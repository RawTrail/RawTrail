import EndpointSecurity
import Foundation
import os.log

private let logger = Logger(subsystem: "io.rawtrail.securityextension", category: "ESFAgentFiles")

/// AgentFileSubscriber is the fourth, NOTIFY-only Endpoint Security client: full file activity (create, modify, rename, delete) for an
/// AI agent and every process it starts, and for nothing else on the host (issue #1287).
///
/// It filters in its handler rather than through process-mute inversion, which the spike (#1276) proposed and edr-dev disproved. A
/// muted set has to be updated from a FORK or EXEC handler, and a shell's child forks, execs and writes within a millisecond, before
/// that handler runs; exec bumps the pidversion, so the token muted late is already dead and the child's whole life goes unseen. Here
/// FORK, EXEC and EXIT arrive in the same ordered stream as the file events, so a child is in AgentProcessSet before its first file
/// event is handled. The cost is that every close on the host reaches the handler, about 520 a second on idle edr-dev, each settled by
/// one set lookup.
///
/// Roots are execs isAgentRoot accepts: the Claude Code binary, or a process carrying a session id. The file events are CREATE, RENAME,
/// UNLINK and CLOSE with `modified` set; WRITE and OPEN are left out, because the spike measured them as the bulk of an install's volume
/// while saying nothing a modified close does not. UIPC_CONNECT adds the Unix-domain sockets the subtree connects to (issue #1355):
/// the request to a running daemon, kernel-attributed to the process that made it, and never the daemon's own work. Operations are
/// folded by FileActivityRollup and sent once a second as `file_activity` events.
final class AgentFileSubscriber: Sendable {
    // swiftlint:disable:next implicitly_unwrapped_optional
    private nonisolated(unsafe) var client: OpaquePointer!
    private let serializer = EventSerializer()
    nonisolated(unsafe) var onEvent: ((Data) -> Void)?

    /// queue guards the process set and the rollup's open window. The ES callbacks hold it for a lookup or an add, and the flush only
    /// long enough to swap the window out, so folding and serializing never delay delivery of the host's other events.
    private let queue = DispatchQueue(label: "io.rawtrail.agentfiles")
    /// flushQueue runs the flush timer: folding, serializing and sending a window taken off queue.
    private let flushQueue = DispatchQueue(label: "io.rawtrail.agentfiles.flush")
    private nonisolated(unsafe) var processes = AgentProcessSet(capacity: maxProcesses)
    private nonisolated(unsafe) var rollup = FileActivityRollup(budget: rowsPerFlush, maxPending: maxPendingRows)
    private nonisolated(unsafe) var flushTimer: DispatchSourceTimer?
    /// reportedRefusals is the refusal total already logged, so each flush logs only what is new. Read and written on flushQueue.
    private nonisolated(unsafe) var reportedRefusals = 0

    /// maxProcesses bounds the observed set. An agent subtree is a handful of shells and tools; thousands at once is a runaway build,
    /// whose excess goes unobserved rather than growing the set.
    private static let maxProcesses = 4096
    /// rowsPerFlush is the most `file_activity` events one flush sends, so at most this many a second.
    private static let rowsPerFlush = 64
    /// maxPendingRows bounds the window between flushes; past it the window is folded early.
    private static let maxPendingRows = 4096
    private static let flushInterval: DispatchTimeInterval = .seconds(1)

    init() {
        var rawClient: OpaquePointer?
        let result = es_new_client(&rawClient) { [weak self] _, message in
            self?.handleMessage(message)
        }
        guard result == ES_NEW_CLIENT_RESULT_SUCCESS, let rawClient else {
            // Not fatal: agent file activity adds visibility, and the host's other telemetry must not go with it.
            logger.error("Failed to create agent-file ES client: \(result.rawValue)")
            return
        }
        self.client = rawClient
    }

    func start() {
        guard client != nil else {
            return
        }
        let events: [es_event_type_t] = [
            ES_EVENT_TYPE_NOTIFY_FORK,
            ES_EVENT_TYPE_NOTIFY_EXEC,
            ES_EVENT_TYPE_NOTIFY_EXIT,
            ES_EVENT_TYPE_NOTIFY_CREATE,
            ES_EVENT_TYPE_NOTIFY_RENAME,
            ES_EVENT_TYPE_NOTIFY_UNLINK,
            ES_EVENT_TYPE_NOTIFY_CLOSE,
            ES_EVENT_TYPE_NOTIFY_UIPC_CONNECT
        ]
        guard es_subscribe(client, events, UInt32(events.count)) == ES_RETURN_SUCCESS else {
            logger.error("agent-file subscribe failed")
            return
        }
        let timer = DispatchSource.makeTimerSource(queue: flushQueue)
        timer.schedule(deadline: .now() + Self.flushInterval, repeating: Self.flushInterval)
        timer.setEventHandler { [weak self] in self?.flush() }
        timer.resume()
        flushTimer = timer
        logger.info("agent-file client active: FORK/EXEC/EXIT/CREATE/RENAME/UNLINK/CLOSE/UIPC_CONNECT, filtered to agent subtrees")
    }

    private func handleMessage(_ message: UnsafePointer<es_message_t>) {
        let msg = message.pointee
        if let change = AgentProcessSet.Change(of: msg) {
            queue.sync { processes.apply(change) }
            return
        }
        // Most closes and socket connects on the host belong to no agent; the set lookup settles them before any path is read.
        let actor = ProcessKey(msg.process.pointee.audit_token)
        guard queue.sync(execute: { processes.contains(actor) }), let record = Self.record(of: msg) else {
            return
        }
        queue.sync { rollup.add(record) }
    }

    /// record reads a file event into a FileActivityRecord, or nil for a close that changed nothing and for a path not worth sending.
    private static func record(of msg: es_message_t) -> FileActivityRecord? {
        guard let observed = operation(of: msg), isAgentFilePath(observed.path) else {
            return nil
        }
        return FileActivityRecord(
            process: ProcessKey(msg.process.pointee.audit_token), operation: observed.operation, path: observed.path,
            sourcePath: observed.sourcePath, timeNs: kernelEventTimeNs(msg.time)
        )
    }

    /// operation reads what a file event did and to which path, with a rename's source.
    private struct ObservedOperation {
        let operation: FileOperation
        let path: String
        var sourcePath: String?
    }

    private static func operation(of msg: es_message_t) -> ObservedOperation? {
        switch msg.event_type {
        case ES_EVENT_TYPE_NOTIFY_CREATE:
            let create = msg.event.create
            let path = destination(create.destination_type, dir: create.destination.new_path.dir,
                                   filename: create.destination.new_path.filename, existing: create.destination.existing_file)
            return path.map { ObservedOperation(operation: .create, path: $0) }
        case ES_EVENT_TYPE_NOTIFY_RENAME:
            let rename = msg.event.rename
            let path = destination(rename.destination_type, dir: rename.destination.new_path.dir,
                                   filename: rename.destination.new_path.filename, existing: rename.destination.existing_file)
            return path.map { ObservedOperation(operation: .rename, path: $0, sourcePath: esTokenString(rename.source.pointee.path)) }
        case ES_EVENT_TYPE_NOTIFY_UNLINK:
            return ObservedOperation(operation: .delete, path: esTokenString(msg.event.unlink.target.pointee.path))
        case ES_EVENT_TYPE_NOTIFY_CLOSE:
            guard msg.event.close.modified else {
                return nil
            }
            return ObservedOperation(operation: .modify, path: esTokenString(msg.event.close.target.pointee.path))
        case ES_EVENT_TYPE_NOTIFY_UIPC_CONNECT:
            return ObservedOperation(operation: .connect, path: esTokenString(msg.event.uipc_connect.file.pointee.path))
        default:
            return nil
        }
    }

    /// destination reads a create's or a rename's destination, which is a new path or a file that already existed.
    private static func destination(
        _ type: es_destination_type_t, dir: UnsafeMutablePointer<es_file_t>, filename: es_string_token_t,
        existing: UnsafeMutablePointer<es_file_t>
    ) -> String? {
        switch type {
        case ES_DESTINATION_TYPE_NEW_PATH:
            return esNewPath(dir, filename)
        case ES_DESTINATION_TYPE_EXISTING_FILE:
            return esTokenString(existing.pointee.path)
        default:
            return nil
        }
    }

    /// flush takes the open window off queue, then folds it and sends its rows. Runs on flushQueue, from the timer.
    private func flush() {
        let (taken, refused, observed) = queue.sync {
            let window = rollup
            rollup = FileActivityRollup(budget: Self.rowsPerFlush, maxPending: Self.maxPendingRows)
            return (window, processes.refused, processes.count)
        }
        var window = taken
        for row in window.flush() {
            if let data = serializer.serialize(eventType: "file_activity", payload: row.payload, kernelTimeNs: row.firstNs) {
                onEvent?(data)
            }
        }
        if window.dropped > 0 || refused > reportedRefusals {
            let summary = "agent-file client over budget: \(window.dropped) operations dropped, " +
                "\(refused - reportedRefusals) processes unobserved (\(observed) observed)"
            logger.error("\(summary, privacy: .public)")
            reportedRefusals = refused
        }
    }

    func stop() {
        flushQueue.sync { flushTimer?.cancel() }
        es_unsubscribe_all(client)
        es_delete_client(client)
    }
}

extension ProcessKey {
    init(_ token: audit_token_t) {
        self.init(pid: audit_token_to_pid(token), pidVersion: UInt32(bitPattern: audit_token_to_pidversion(token)))
    }
}

/// The ES half of AgentProcessSet, shared by the clients that keep one from their own fork, exec and exit stream: this one and the
/// credential guard. Each keeps its own set, because a set fed by another client's stream would trail the events it is asked about.
extension AgentProcessSet {
    /// Change is what one FORK, EXEC or EXIT does to the set, read from the message while it is valid.
    enum Change {
        case fork(parent: ProcessKey, child: ProcessKey, root: Bool)
        case exec(from: ProcessKey, to: ProcessKey, root: Bool)
        case exit(ProcessKey)

        /// init reads the change a message makes, or is nil for a message that is not a FORK, EXEC or EXIT.
        init?(of msg: es_message_t) {
            let actor = ProcessKey(msg.process.pointee.audit_token)
            switch msg.event_type {
            case ES_EVENT_TYPE_NOTIFY_FORK:
                let root = isAgentRoot(codeSigning: ESFSubscriber.extractCodeSigning(from: msg.process.pointee), env: nil)
                self = .fork(parent: actor, child: ProcessKey(msg.event.fork.child.pointee.audit_token), root: root)
            case ES_EVENT_TYPE_NOTIFY_EXEC:
                var exec = msg.event.exec
                let target = exec.target.pointee
                let root = isAgentRoot(
                    codeSigning: ESFSubscriber.extractCodeSigning(from: target), env: ESFSubscriber.sessionMarkers(of: &exec)
                )
                self = .exec(from: actor, to: ProcessKey(target.audit_token), root: root)
            case ES_EVENT_TYPE_NOTIFY_EXIT:
                self = .exit(actor)
            default:
                return nil
            }
        }
    }

    mutating func apply(_ change: Change) {
        switch change {
        case let .fork(parent, child, root):
            fork(parent: parent, child: child, root: root)
        case let .exec(old, new, root):
            exec(from: old, to: new, root: root)
        case let .exit(key):
            exit(key)
        }
    }
}
