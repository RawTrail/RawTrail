import EndpointSecurity
import Foundation
import os.log

private let logger = Logger(subsystem: "io.rawtrail.securityextension", category: "ESFCredentialGuard")

/// CredentialGuardSubscriber is the fifth Endpoint Security client, and the second to authorize: it decides each open of a credential
/// file by an AI agent's process (issue #1431), under the policy the server pushed (issue #1432). An agent's process opening a credential
/// that is not its tool's is reported: in detect mode the open is allowed and reported as `agent_credential_would_block`, in protect mode
/// it is denied and reported as `agent_credential_block`, and off watches nothing. CredentialGuardPolicy holds the decision.
///
/// It subscribes to AUTH_OPEN with target-path muting inverted to the credential files in every home, so the opens it is asked about
/// are those files' and nothing else on the host, a handful a minute. The verdict depends on the process, not the file, so it is
/// never cached.
///
/// It also keeps its own AgentProcessSet from NOTIFY_FORK, EXEC and EXIT, for the reason AgentFileSubscriber does: those arrive in the
/// same ordered stream as the opens, so a shell's child that execs `cat` and opens a key at once is in the set when its open is
/// decided. A set read from the agent-file client would trail by however far that client's queue is behind. FORK and EXIT carry no
/// target path, so the inverted muting passes them; EXEC's target is the binary run, so every exec is selected explicitly.
final class CredentialGuardSubscriber: Sendable {
    // swiftlint:disable:next implicitly_unwrapped_optional
    private nonisolated(unsafe) var client: OpaquePointer!
    private let serializer = EventSerializer()
    nonisolated(unsafe) var onEvent: ((Data) -> Void)?

    /// enforced is what the handler decides each open by: the mode and the credential targets that are muted. Only refresh replaces
    /// it, once the targets it names are muted.
    private let enforced = OSAllocatedUnfairLock(initialState: CredentialGuardEnforcement.none)

    /// processes is the observed agent subtrees. ES calls the handler serially, and only the handler reads or writes the set.
    private nonisolated(unsafe) var processes = AgentProcessSet(capacity: maxProcesses)
    private static let maxProcesses = 4096
    /// refusalsPerLog spaces the log lines a runaway subtree past maxProcesses produces.
    private static let refusalsPerLog = 1_000

    /// queue serializes every change to the muted set, the start and each refresh. `applied` is only read and written on it.
    private let queue = DispatchQueue(label: "io.rawtrail.credentialguard")
    /// applied is the credential files currently muted, and so observed once inversion is on.
    private nonisolated(unsafe) var applied: [WatchedPath] = []
    /// wanted is the policy to enforce: the persisted or built-in one at first, then each one pushed.
    private nonisolated(unsafe) var wanted: CredentialGuardPolicy
    /// started is set once start has cleared the default mutes. A policy pushed before then is only recorded, since a refresh that
    /// muted paths before the clear would leave them recorded as applied and no longer muted.
    private nonisolated(unsafe) var started = false
    private nonisolated(unsafe) var refreshTimer: DispatchSourceTimer?
    /// refreshSeconds is how long an account added to the host goes unguarded at most, as for the other home-scoped clients.
    private static let refreshSeconds = 300

    init(policy: CredentialGuardPolicy) {
        self.wanted = policy
        var rawClient: OpaquePointer?
        let result = es_new_client(&rawClient) { [weak self] client, message in
            self?.handleMessage(client, message)
        }
        guard result == ES_NEW_CLIENT_RESULT_SUCCESS, let rawClient else {
            // Not fatal: the guard adds a detection, and the host's other telemetry must not go with it.
            logger.error("Failed to create credential-guard ES client: \(result.rawValue)")
            return
        }
        self.client = rawClient
    }

    func start() {
        guard client != nil else {
            return
        }
        // The order the SDK requires of a client that inverts target-path muting: clear the default mute set, mute the targets and
        // invert while the client has no AUTH subscription, and only then subscribe.
        guard es_unmute_all_target_paths(client) == ES_RETURN_SUCCESS else {
            logger.error("credential-guard client could not clear the default target-path mutes; the client stays unsubscribed")
            return
        }
        queue.sync {
            started = true
            refresh()
        }
        let exec = [ES_EVENT_TYPE_NOTIFY_EXEC]
        guard es_mute_path_events(client, "/", ES_MUTE_PATH_TYPE_TARGET_PREFIX, exec, exec.count) == ES_RETURN_SUCCESS,
              es_invert_muting(client, ES_MUTE_INVERSION_TYPE_TARGET_PATH) == ES_RETURN_SUCCESS else {
            logger.error("credential-guard target-path muting could not be set up; the client stays unsubscribed")
            return
        }
        let events: [es_event_type_t] = [
            ES_EVENT_TYPE_NOTIFY_FORK,
            ES_EVENT_TYPE_NOTIFY_EXEC,
            ES_EVENT_TYPE_NOTIFY_EXIT,
            ES_EVENT_TYPE_AUTH_OPEN
        ]
        guard es_subscribe(client, events, UInt32(events.count)) == ES_RETURN_SUCCESS else {
            logger.error("credential-guard subscribe failed")
            return
        }
        let timer = DispatchSource.makeTimerSource(queue: queue)
        timer.schedule(deadline: .now() + .seconds(Self.refreshSeconds), repeating: .seconds(Self.refreshSeconds))
        timer.setEventHandler { [weak self] in self?.refresh() }
        timer.resume()
        refreshTimer = timer
        let summary = "credential-guard client active in \(enforced.withLock { $0.mode }.rawValue) mode: \(appliedCount) paths guarded"
        logger.info("\(summary, privacy: .public)")
    }

    /// apply puts a pushed policy in force: the muted set follows its credentials, so a credential the server stopped protecting stops
    /// reaching the guard and one it added starts to, and opens are decided by it once its credentials are muted. Off unmutes every
    /// credential. Before start the policy is only recorded, and start enforces it.
    func apply(_ next: CredentialGuardPolicy) {
        queue.async { [weak self] in
            guard let self else {
                return
            }
            wanted = next
            if started {
                refresh()
            }
        }
    }

    private var appliedCount: Int {
        queue.sync { applied.count }
    }

    /// refresh brings the muted set and the enforcement to the wanted policy over the homes on the host now. It mutes first, then
    /// publishes the enforcement over the targets that are muted, and unmutes only when every mute succeeded, the discipline
    /// FileTamperSubscriber.reconcile follows: a credential newly protected is decided against only once its opens reach the guard,
    /// and one no longer protected is allowed before it stops reaching it. A mute that fails is not recorded, so the next refresh
    /// tries it again, and an account walk that fails keeps every path already guarded. Runs on queue.
    private func refresh() {
        guard let homes = WatchedPaths.homeDirectories() else {
            logger.error("credential-guard client could not read the accounts; keeping \(self.applied.count) guarded paths")
            return
        }
        let next = wanted.enforcement(homes: homes)
        let (mute, unmute) = WatchedPaths.changes(from: applied, to: next.targets.map(\.watched))
        var failed = 0
        for target in mute {
            guard es_mute_path(client, target.path, Self.muteType(target.match)) == ES_RETURN_SUCCESS else {
                failed += 1
                continue
            }
            applied.append(target)
        }
        let muted = Set(applied)
        enforced.withLock { $0 = CredentialGuardEnforcement(mode: next.mode, targets: next.targets.filter { muted.contains($0.watched) }) }
        guard failed == 0 else {
            logger.error("credential-guard client could not mute \(failed, privacy: .public) credential paths; nothing unmuted")
            return
        }
        for target in unmute where es_unmute_path(client, target.path, Self.muteType(target.match)) == ES_RETURN_SUCCESS {
            // Still muted, so still guarded, when the unmute fails: it stays applied and the next refresh tries again.
            applied.removeAll { $0 == target }
        }
    }

    private static func muteType(_ match: WatchedPathMatch) -> es_mute_path_type_t {
        switch match {
        case .literal:
            return ES_MUTE_PATH_TYPE_TARGET_LITERAL
        case .prefix:
            return ES_MUTE_PATH_TYPE_TARGET_PREFIX
        }
    }

    private func handleMessage(_ client: OpaquePointer, _ message: UnsafePointer<es_message_t>) {
        let msg = message.pointee
        if let change = AgentProcessSet.Change(of: msg) {
            let refusedBefore = processes.refused
            processes.apply(change)
            if processes.refused > refusedBefore, processes.refused % Self.refusalsPerLog == 1 {
                // A process the full set refused is unguarded, so say so: on the first refusal and every refusalsPerLog after it.
                let summary = "credential-guard process set full: \(processes.refused) processes unguarded (\(processes.count) observed)"
                logger.error("\(summary, privacy: .public)")
            }
            return
        }
        guard msg.event_type == ES_EVENT_TYPE_AUTH_OPEN else {
            return
        }
        // The decision is a few string compares and one set lookup, made and answered before anything is serialized, so the open
        // waits on nothing else. Any open the guard does not decide against is allowed.
        let opener = msg.process.pointee
        let path = esTokenString(msg.event.open.file.pointee.path)
        let key = ProcessKey(opener.audit_token)
        let decision = enforced.withLock { $0 }.decide(
            path: path, opener: ESFSubscriber.extractCodeSigning(from: opener), inAgentProcessSet: processes.contains(key)
        )
        // An allowed open is answered with the access it asked for, in the kernel's FREAD and FWRITE flags as the open reports them.
        let requested = UInt32(bitPattern: msg.event.open.fflag)
        let answered = es_respond_flags_result(client, message, decision.deniesOpen ? 0 : requested, false)
        // The time from the open to its answer, logged for every open so the guard's cost to the tools it allows is measurable too.
        let verdict: String
        switch decision {
        case .allow: verdict = "allow"
        case .wouldBlock: verdict = "allow, would block"
        case .block: verdict = "deny"
        }
        let latency = "credential-guard answered pid=\(key.pid) in \(Self.microseconds(since: msg.mach_time))us: \(verdict)"
        logger.debug("\(latency, privacy: .public)")
        if answered != ES_RESPOND_RESULT_SUCCESS {
            logger.error("credential-guard could not answer an open: \(answered.rawValue, privacy: .public)")
        }
        guard let (eventType, credential) = decision.report(denied: answered == ES_RESPOND_RESULT_SUCCESS) else {
            return
        }
        let payload = AgentCredentialAccessPayload(
            pid: key.pid, pidVersion: key.pidVersion, path: path, credential: credential,
            flags: CredentialStores.openFlags(fflag: msg.event.open.fflag)
        )
        let reported = "credential-guard \(verdict) pid=\(key.pid) credential=\(credential.rawValue)"
        logger.info("\(reported, privacy: .public)")
        let kernelTimeNs = kernelEventTimeNs(msg.time)
        if let data = serializer.serialize(eventType: eventType, payload: payload, kernelTimeNs: kernelTimeNs) {
            onEvent?(data)
        }
    }

    /// microseconds is the time from a message's creation to now, which for an AUTH message is how long the open waited on the guard.
    private static func microseconds(since machTime: UInt64) -> UInt64 {
        let elapsed = mach_absolute_time() - machTime
        return elapsed * UInt64(timebase.numer) / UInt64(timebase.denom) / nanosecondsPerMicrosecond
    }

    private static let nanosecondsPerMicrosecond: UInt64 = 1_000

    private static let timebase: mach_timebase_info_data_t = {
        var info = mach_timebase_info_data_t()
        mach_timebase_info(&info)
        return info
    }()

    func stop() {
        guard client != nil else {
            return
        }
        queue.sync { refreshTimer?.cancel() }
        es_unsubscribe_all(client)
        es_delete_client(client)
    }
}
