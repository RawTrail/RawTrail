import Darwin

/// Agent-scoped file telemetry (issue #1287): the pure half of AgentFileSubscriber. Which execs start an agent, the set of processes
/// the subscriber observes, and the one-second rollup that bounds what reaches the wire.

/// The Claude Code binary's signing identifier, read on edr-dev from Claude Code 2.1.296. Its own Read, Edit and Write tools run inside
/// this process, so it is observed itself, not only through the shells it starts. The team is not checked: ES reports none for it on a
/// host with SIP off, and a binary copying the identifier only gets itself observed, which hides nothing.
let claudeCodeSigningID = "com.anthropic.claude-code"

/// isAgentRoot reports whether an exec starts an agent subtree the subscriber observes: the Claude Code binary, or any process carrying a
/// session id, which covers a session's processes when the subscriber started after their agent did.
func isAgentRoot(codeSigning: CodeSigning?, env: [String: String]?) -> Bool {
    codeSigning?.signingID == claudeCodeSigningID || env?["CLAUDE_CODE_SESSION_ID"] != nil
}

/// isAgentFilePath reports whether an operation on path is file activity worth sending. Device nodes are not: every command a shell
/// runs writes to its terminal, and closing `/dev/ttys003` on exit would otherwise report a modified file for each one.
func isAgentFilePath(_ path: String) -> Bool {
    !path.isEmpty && !path.hasPrefix("/dev/")
}

/// ProcessKey is a process generation: a pid and the pidversion its audit token carries, which exec bumps.
struct ProcessKey: Hashable, Sendable {
    let pid: pid_t
    let pidVersion: UInt32
}

/// AgentProcessSet is the set of process generations the subscriber observes, kept from the fork, exec and exit events of the same
/// client that delivers the file events, so a child is in the set before its first file event arrives.
///
/// It holds at most `capacity` generations. A subtree that forks past it, a runaway build, leaves the excess unobserved rather than
/// growing without bound, and each refusal is counted in `refused`.
struct AgentProcessSet {
    private(set) var tracked: Set<ProcessKey> = []
    private(set) var refused = 0
    let capacity: Int

    init(capacity: Int) {
        self.capacity = capacity
    }

    var count: Int { tracked.count }

    func contains(_ key: ProcessKey) -> Bool {
        tracked.contains(key)
    }

    /// fork observes the child of an observed parent. A fork by a root starts observing the parent too: a Claude Code process that
    /// exec'd before the subscriber started was never seen as a root, and its helpers (`rg`, `git`) carry no session id of their own.
    mutating func fork(parent: ProcessKey, child: ProcessKey, root: Bool) {
        if root, !contains(parent) {
            insert(parent)
        }
        if contains(parent) {
            insert(child)
        }
    }

    /// exec moves an observed process to the generation its exec created, or starts observing a root.
    mutating func exec(from old: ProcessKey, to new: ProcessKey, root: Bool) {
        if tracked.remove(old) != nil || root {
            insert(new)
        }
    }

    /// exit stops observing a process.
    mutating func exit(_ key: ProcessKey) {
        tracked.remove(key)
    }

    private mutating func insert(_ key: ProcessKey) {
        guard tracked.count < capacity else {
            refused += 1
            return
        }
        tracked.insert(key)
    }
}

/// FileOperation is what an agent process did to a path. A connect is to a Unix-domain socket, whose path names the daemon the
/// process asked for work (issue #1355): docker.sock, an ssh-agent's socket, a database's.
enum FileOperation: String, Codable, Sendable {
    case create, modify, rename, delete, connect
}

/// FileActivityPayload is the wire shape of a `file_activity` event: one or more file operations by one process generation, folded
/// by FileActivityRollup. It is a type of its own rather than another `open` or `file_rename`, because those feed detection rules
/// that read `path` as a file, and a rolled-up row names a directory.
struct FileActivityPayload: Codable, Sendable, Equatable {
    let pid: pid_t
    let pidVersion: UInt32
    let operation: FileOperation
    /// The file operated on, or, when rolledUp, the directory whose contents the counts cover.
    var path: String
    /// Where the renamed file came from, which every rename the row folds shares. Omitted for every other operation, for a row folding
    /// renames from different sources, and for a rolled-up rename.
    var sourcePath: String?
    /// How many operations the row stands for. Omitted when one.
    var coalescedCount: Int?
    /// The latest operation's time; the envelope timestamp is the earliest. Omitted when the row stands for one operation.
    var lastTimestampNs: UInt64?
    /// Set when `path` is a directory the row folds the paths under into. Omitted from the wire when false.
    var rolledUp = false

    enum CodingKeys: String, CodingKey {
        case pid, operation, path
        case pidVersion = "pidversion"
        case sourcePath = "source_path"
        case coalescedCount = "coalesced_count"
        case lastTimestampNs = "last_timestamp_ns"
        case rolledUp = "rolled_up"
    }
}

extension FileActivityPayload {
    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        pid = try container.decode(pid_t.self, forKey: .pid)
        pidVersion = try container.decode(UInt32.self, forKey: .pidVersion)
        operation = try container.decode(FileOperation.self, forKey: .operation)
        path = try container.decode(String.self, forKey: .path)
        sourcePath = try container.decodeIfPresent(String.self, forKey: .sourcePath)
        coalescedCount = try container.decodeIfPresent(Int.self, forKey: .coalescedCount)
        lastTimestampNs = try container.decodeIfPresent(UInt64.self, forKey: .lastTimestampNs)
        rolledUp = try container.decodeIfPresent(Bool.self, forKey: .rolledUp) ?? false
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(pid, forKey: .pid)
        try container.encode(pidVersion, forKey: .pidVersion)
        try container.encode(operation, forKey: .operation)
        try container.encode(path, forKey: .path)
        try container.encodeIfPresent(sourcePath, forKey: .sourcePath)
        try container.encodeIfPresent(coalescedCount, forKey: .coalescedCount)
        try container.encodeIfPresent(lastTimestampNs, forKey: .lastTimestampNs)
        if rolledUp {
            try container.encode(true, forKey: .rolledUp)
        }
    }
}

/// FileActivityRecord is one file operation as the subscriber observed it.
struct FileActivityRecord: Sendable {
    let process: ProcessKey
    let operation: FileOperation
    let path: String
    var sourcePath: String?
    let timeNs: UInt64
}

/// FileActivityRow is a folded row with the time of its first operation, which becomes the event's timestamp.
struct FileActivityRow: Sendable, Equatable {
    var payload: FileActivityPayload
    var firstNs: UInt64
}

/// FileActivityRollup folds a window of file operations into at most `budget` rows (the spike measured about 6.7k operations for one
/// three-second `npm install`):
///
///   - Repeats of one operation on one path by one process fold into one row with a count, which is all a transcript appended on every
///     message costs.
///   - Past the budget, the deepest rows fold into their parent directory, level by level, until the window fits. A tree of new files
///     becomes a row per directory near its root rather than a row per file.
///   - When rows of distinct processes or operations still exceed the budget at the top level, the rows standing for the fewest
///     operations are dropped and counted in `dropped`, so a runaway build cannot flood the server.
///
/// The window holds at most `maxPending` rows between flushes: past it, the window is folded early, so its memory stays bounded however
/// fast a build writes.
struct FileActivityRollup {
    let budget: Int
    let maxPending: Int
    private var rows: [RowKey: FileActivityRow] = [:]
    private(set) var dropped = 0

    private struct RowKey: Hashable {
        let process: ProcessKey
        let operation: FileOperation
        let path: String
    }

    init(budget: Int, maxPending: Int) {
        self.budget = budget
        self.maxPending = maxPending
    }

    var pendingCount: Int { rows.count }

    mutating func add(_ record: FileActivityRecord) {
        let key = RowKey(process: record.process, operation: record.operation, path: record.path)
        if var row = rows[key] {
            row.merge(count: 1, firstNs: record.timeNs, lastNs: record.timeNs)
            // Two renames to one path from different sources keep neither source: the row no longer names one.
            if row.payload.sourcePath != record.sourcePath {
                row.payload.sourcePath = nil
            }
            rows[key] = row
        } else {
            let payload = FileActivityPayload(
                pid: record.process.pid, pidVersion: record.process.pidVersion, operation: record.operation, path: record.path,
                sourcePath: record.operation == .rename ? record.sourcePath : nil
            )
            rows[key] = FileActivityRow(payload: payload, firstNs: record.timeNs)
        }
        if rows.count > maxPending {
            fold(to: budget)
        }
    }

    /// flush returns the window's rows, folded to the budget and oldest first, and starts an empty window.
    mutating func flush() -> [FileActivityRow] {
        fold(to: budget)
        let out = rows.values.sorted { ($0.firstNs, $0.payload.path) < ($1.firstNs, $1.payload.path) }
        rows = [:]
        return out
    }

    /// fold folds the deepest rows into their parent directories until at most limit remain or every row is at the top level, then
    /// drops the smallest rows past the limit.
    private mutating func fold(to limit: Int) {
        while rows.count > limit {
            let deepest = rows.keys.map { Self.depth($0.path) }.max() ?? 0
            guard deepest > 1 else {
                break
            }
            var folded: [RowKey: FileActivityRow] = [:]
            for (key, row) in rows {
                var next = row
                var nextKey = key
                if Self.depth(key.path) == deepest {
                    let parent = Self.parent(of: key.path)
                    next.payload.path = parent
                    next.payload.sourcePath = nil
                    next.payload.rolledUp = true
                    nextKey = RowKey(process: key.process, operation: key.operation, path: parent)
                }
                if var existing = folded[nextKey] {
                    existing.absorb(next)
                    folded[nextKey] = existing
                } else {
                    folded[nextKey] = next
                }
            }
            rows = folded
        }
        guard rows.count > limit else {
            return
        }
        let kept = rows.sorted { ($0.value.count, $1.value.firstNs) > ($1.value.count, $0.value.firstNs) }.prefix(limit)
        dropped += rows.values.reduce(0) { $0 + $1.count } - kept.reduce(0) { $0 + $1.value.count }
        rows = Dictionary(uniqueKeysWithValues: kept.map { ($0.key, $0.value) })
    }

    /// depth counts a path's components: `/a/b` is 2. The root and a relative path are 0.
    static func depth(_ path: String) -> Int {
        path.split(separator: "/", omittingEmptySubsequences: true).count
    }

    /// parent drops a path's last component.
    static func parent(of path: String) -> String {
        let components = path.split(separator: "/", omittingEmptySubsequences: true).dropLast()
        return "/" + components.joined(separator: "/")
    }
}

extension FileActivityRow {
    /// count is how many operations the row stands for.
    var count: Int { payload.coalescedCount ?? 1 }

    var lastNs: UInt64 { payload.lastTimestampNs ?? firstNs }

    mutating func merge(count added: Int, firstNs addedFirst: UInt64, lastNs addedLast: UInt64) {
        let last = max(lastNs, addedLast)
        firstNs = min(firstNs, addedFirst)
        let total = count + added
        payload.coalescedCount = total
        payload.lastTimestampNs = last
    }

    /// absorb folds another row of the same process, operation and directory into this one.
    mutating func absorb(_ other: FileActivityRow) {
        merge(count: other.count, firstNs: other.firstNs, lastNs: other.lastNs)
        payload.rolledUp = true
        payload.sourcePath = nil
    }
}
