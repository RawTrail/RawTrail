import Darwin

/// Agent-scoped file telemetry (issue #1287): the pure half of AgentFileSubscriber. Which execs start an agent, and the one-second
/// rollup that bounds what reaches the wire; the set of processes the subscriber observes is AgentProcessSet.

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

/// FileOperation is what an agent process did to a path. A connect is to a Unix-domain socket, whose path names the daemon the
/// process asked for work (issue #1355): docker.sock, an ssh-agent's socket, a database's.
enum FileOperation: String, Codable, Sendable {
    case create, modify, rename, delete, connect
}

/// FileActivityPayload is the wire shape of a `file_activity` event: one or more file operations by one process generation, folded
/// by FileActivityRollup. It is a type of its own rather than another `open` or `file_rename`, because those feed detection rules
/// that read `path` as a file, and a rolled-up row names a directory.
struct FileActivityPayload: Codable, Sendable, Equatable {
    var pid: pid_t
    var pidVersion: UInt32
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
    /// How many processes the row stands for, when it folds the rows of several: `pid` and `pidversion` then name the nearest process
    /// they all descend from, which may be one of them. Omitted when one.
    var processCount: Int?

    enum CodingKeys: String, CodingKey {
        case pid, operation, path
        case pidVersion = "pidversion"
        case sourcePath = "source_path"
        case coalescedCount = "coalesced_count"
        case lastTimestampNs = "last_timestamp_ns"
        case rolledUp = "rolled_up"
        case processCount = "process_count"
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
        processCount = try container.decodeIfPresent(Int.self, forKey: .processCount)
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
        try container.encodeIfPresent(processCount, forKey: .processCount)
    }
}

/// FileActivityLoss is how many operations of one agent subtree a window dropped, and when the first and last of them happened.
struct FileActivityLoss: Equatable {
    var operations = 0
    var firstNs = UInt64.max
    var lastNs: UInt64 = 0

    mutating func add(_ row: FileActivityRow) {
        operations += row.count
        firstNs = min(firstNs, row.firstNs)
        lastNs = max(lastNs, row.lastNs)
    }
}

/// FileActivityDroppedPayload is the wire shape of a `file_activity_dropped` event: file operations of an agent subtree that a
/// window could not send within its budget, so an analyst reading the session knows its file activity is incomplete. `pid` and
/// `pidversion` name the subtree's topmost observed process of its session.
struct FileActivityDroppedPayload: Codable, Sendable, Equatable {
    let pid: pid_t
    let pidVersion: UInt32
    let operations: Int
    let lastTimestampNs: UInt64

    enum CodingKeys: String, CodingKey {
        case pid, operations
        case pidVersion = "pidversion"
        case lastTimestampNs = "last_timestamp_ns"
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

/// FileActivityRow is a folded row with the time of its first operation, which becomes the event's timestamp, and the processes whose
/// operations it stands for.
struct FileActivityRow: Sendable, Equatable {
    var payload: FileActivityPayload
    var firstNs: UInt64
    var processes: Set<ProcessKey>
    /// lineage is the processes the row may fold into, the named process's parent first: the live ancestors of its own session the
    /// process had when the row began. Each was running then, and so at the first operation of any row this one takes in, since it was
    /// running before any of its descendants acted: a folded row never names a process that had exited.
    var lineage: [ProcessKey] = []
}

/// FileActivityRollup folds a window of file operations into at most `budget` rows (the spike measured about 6.7k operations for one
/// three-second `npm install`):
///
///   - Repeats of one operation on one path by one process fold into one row with a count, which is all a transcript appended on every
///     message costs.
///   - Past the budget, the deepest rows fold into their parent directory, level by level, until the window fits. A tree of new files
///     becomes a row per directory near its root rather than a row per file.
///   - When rows of distinct processes still exceed the budget, the rows of the deepest processes fold into their parent process, level
///     by level, keeping their paths. A pipeline whose every process closes the output file it inherited becomes one row on the process
///     that started them, with a `process_count`. The directories are then climbed again over the rows that remain.
///   - Only when rows of distinct subtrees, operations or top-level directories still exceed the budget are the rows standing for the
///     fewest operations dropped and counted in `dropped`, so a runaway build cannot flood the server.
///
/// The window holds at most `maxPending` rows between flushes: past it, the window is folded early, so its memory stays bounded however
/// fast a build writes.
struct FileActivityRollup {
    let budget: Int
    let maxPending: Int
    private var rows: [RowKey: FileActivityRow] = [:]
    private(set) var dropped = 0
    /// losses is what the drops cost each agent subtree, keyed by the topmost process of its session a dropped row knew, so the server
    /// can say which session's activity is incomplete rather than only that some was.
    private(set) var losses: [ProcessKey: FileActivityLoss] = [:]

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

    /// add folds one operation into the window. lineage is read when the operation starts a row, for the live processes of its session
    /// the process descends from, parent first, at most `AgentProcessSet.maxLineage` of them: the window's memory and every fold's work
    /// stay bounded by its rows.
    mutating func add(_ record: FileActivityRecord, lineage: () -> [ProcessKey] = { [] }) {
        let key = RowKey(process: record.process, operation: record.operation, path: record.path)
        if var row = rows[key] {
            row.merge(count: 1, firstNs: record.timeNs, lastNs: record.timeNs)
            // A process's own operation can join the row its children's folded into.
            row.processes.insert(record.process)
            row.payload.processCount = row.processes.count > 1 ? row.processes.count : nil
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
            rows[key] = FileActivityRow(payload: payload, firstNs: record.timeNs, processes: [record.process], lineage: lineage())
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

    /// Group is a row as a climb moves it, with what it was folded from: the first row as it stood before the climb, how many rows it
    /// took in, and the steps every one of them shares: the leading path components, or the processes of their lineages.
    private struct Group<Step: Equatable> {
        var row: FileActivityRow
        let first: FileActivityRow
        var members = 1
        var common: [Step]

        mutating func take(_ other: Group, sharing shared: [Step]) {
            row.join(other.row)
            members += other.members
            common = shared
        }
    }

    /// fold folds the window until at most limit rows remain, by process and by directory in turn, and drops the smallest rows past
    /// the limit only when neither climb folds anything more.
    ///
    /// Processes go first: folding the jobs of a fan-out keeps every path and loses only which sibling did what, which the row's
    /// `process_count` still says, while folding into a directory loses the names of the files. Each climb can make the other's next
    /// pass fold more, so they alternate until the window fits or a round changes nothing.
    private mutating func fold(to limit: Int) {
        var before = Int.max
        while rows.count > limit, rows.count < before {
            before = rows.count
            climbProcesses(to: limit)
            if rows.count > limit {
                climbPaths(to: limit)
            }
        }
        guard rows.count > limit else {
            return
        }
        let ranked = rows.sorted { ($0.value.count, $1.value.firstNs) > ($1.value.count, $0.value.firstNs) }
        for (key, row) in ranked.dropFirst(limit) {
            dropped += row.count
            losses[row.lineage.last ?? key.process, default: FileActivityLoss()].add(row)
        }
        rows = Dictionary(uniqueKeysWithValues: ranked.prefix(limit).map { ($0.key, $0.value) })
    }

    /// climbPaths folds the deepest rows into their parent directories until at most limit remain or every row is at the top level.
    ///
    /// Rows of different processes never merge here, so climbing a level can leave the count where it was: a pipeline whose every
    /// process closes the one output file it inherited climbs to `/private` without folding anything. The climb is therefore only a
    /// grouping, and each group settles on the path its rows share: a row that took in no other keeps its own.
    private mutating func climbPaths(to limit: Int) {
        var groups = rows.mapValues { Group(row: $0, first: $0, common: Self.components($0.payload.path)) }
        while groups.count > limit {
            let deepest = groups.keys.map { Self.depth($0.path) }.max() ?? 0
            guard deepest > 1 else {
                break
            }
            var folded: [RowKey: Group<Substring>] = [:]
            for (key, group) in groups {
                var next = group
                var nextKey = key
                if Self.depth(key.path) == deepest {
                    let parent = Self.parent(of: key.path)
                    next.row.payload.path = parent
                    next.row.payload.sourcePath = nil
                    next.row.payload.rolledUp = true
                    nextKey = RowKey(process: key.process, operation: key.operation, path: parent)
                }
                if var existing = folded[nextKey] {
                    existing.take(next, sharing: Array(zip(existing.common, next.common).prefix { $0 == $1 }.map(\.0)))
                    folded[nextKey] = existing
                } else {
                    folded[nextKey] = next
                }
            }
            groups = folded
        }
        settle(groups.values.map { group in
            guard group.members > 1 else {
                return group.first
            }
            var out = group.row
            out.payload.path = "/" + group.common.joined(separator: "/")
            out.payload.rolledUp = true
            out.payload.sourcePath = nil
            return out
        })
    }

    /// climbProcesses folds the rows of the deepest processes into their parent process, keeping their paths, until at most limit
    /// remain or no row has a lineage left to climb. Each row climbs its own lineage, so it can reach only processes that were running
    /// when its rows began, and at most `AgentProcessSet.maxLineage` levels. As with directories, each group settles on the nearest
    /// process its rows all descend from, and a row that took in no other keeps its own process.
    private mutating func climbProcesses(to limit: Int) {
        // above is what a group can still climb to; common, for settling, is the processes every row it took in shares, the one it
        // names first. Groups that meet keep the processes in both: lineages read at different times differ past an ancestor that
        // exited between them, and the process they meet at is in both.
        var groups = Dictionary(uniqueKeysWithValues: rows.map { key, row in
            (key, (group: Group(row: row, first: row, common: [key.process] + row.lineage), above: row.lineage))
        })
        let depth = Self.depths(of: rows)
        while groups.count > limit {
            let climbing = groups.filter { !$0.value.above.isEmpty }
            guard let deepest = climbing.keys.map({ depth($0.process) }).max() else {
                break
            }
            var folded: [RowKey: (group: Group<ProcessKey>, above: [ProcessKey])] = [:]
            for (key, entry) in groups {
                var next = entry
                var nextKey = key
                if !entry.above.isEmpty, depth(key.process) == deepest {
                    nextKey = RowKey(process: entry.above[0], operation: key.operation, path: key.path)
                    next.above.removeFirst()
                }
                if var existing = folded[nextKey] {
                    let theirs = Set(next.group.common)
                    existing.group.take(next.group, sharing: existing.group.common.filter { theirs.contains($0) })
                    let above = Set(next.above)
                    existing.above = existing.above.filter { above.contains($0) }
                    folded[nextKey] = existing
                } else {
                    folded[nextKey] = next
                }
            }
            groups = folded
        }
        settle(groups.map { key, entry in
            let group = entry.group
            guard group.members > 1 else {
                return group.first
            }
            let ancestor = group.common.first ?? key.process
            var out = group.row
            out.payload.pid = ancestor.pid
            out.payload.pidVersion = ancestor.pidVersion
            out.lineage = Array(group.common.dropFirst())
            return out
        })
    }

    /// depths reads how deep in its tree each process the rows name is, from their lineages, so rows climb in step and siblings meet
    /// at their parent even when an exit cut one's lineage short. A process's parent is the same in every lineage that names it, so the
    /// links form a tree; depth is counted to the top of what the rows know of it, at most `AgentProcessSet.maxLineage`.
    private static func depths(of rows: [RowKey: FileActivityRow]) -> (ProcessKey) -> Int {
        var parent: [ProcessKey: ProcessKey] = [:]
        for (key, row) in rows {
            for (child, up) in zip([key.process] + row.lineage, row.lineage) where parent[child] == nil {
                parent[child] = up
            }
        }
        var known: [ProcessKey: Int] = [:]
        return { process in
            // Walk up to a process whose depth is known, the top of the tree, or the cap, then count back down.
            var path: [ProcessKey] = []
            var start = 0
            var at = process
            while known[at] == nil {
                path.append(at)
                guard let up = parent[at] else {
                    break
                }
                guard path.count < AgentProcessSet.maxLineage else {
                    start = AgentProcessSet.maxLineage
                    break
                }
                at = up
            }
            if let found = known[at], !path.isEmpty {
                start = found + 1
            }
            for (offset, step) in path.reversed().enumerated() {
                known[step] = min(start + offset, AgentProcessSet.maxLineage)
            }
            return known[process] ?? 0
        }
    }

    /// settle keys the window again by the process and path each row now names, so a later operation on that path joins it.
    private mutating func settle(_ settled: [FileActivityRow]) {
        rows = Dictionary(settled.map { row in
            (RowKey(process: ProcessKey(pid: row.payload.pid, pidVersion: row.payload.pidVersion), operation: row.payload.operation,
                    path: row.payload.path), row)
        }, uniquingKeysWith: { kept, other in
            var merged = kept
            merged.join(other)
            return merged
        })
    }

    /// depth counts a path's components: `/a/b` is 2. The root and a relative path are 0.
    static func depth(_ path: String) -> Int {
        components(path).count
    }

    static func components(_ path: String) -> [Substring] {
        path.split(separator: "/", omittingEmptySubsequences: true)
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

    /// join folds another row of the same operation into this one: its operations, its processes, and whether it named a directory.
    /// The source survives only when both name the same one.
    mutating func join(_ other: FileActivityRow) {
        merge(count: other.count, firstNs: other.firstNs, lastNs: other.lastNs)
        processes.formUnion(other.processes)
        payload.processCount = processes.count > 1 ? processes.count : nil
        payload.rolledUp = payload.rolledUp || other.payload.rolledUp
        if payload.sourcePath != other.payload.sourcePath {
            payload.sourcePath = nil
        }
    }
}
