import Darwin

/// Lineage identity of an exec'd process (issue #1277): the fields that let the server group processes and follow them
/// across reparenting. Every field is optional and the encoder omits a nil one, so a payload built without lineage (an
/// older agent, or a snapshot row that cannot know a field) keeps its existing wire shape.
///
/// Encoded flat into the exec payload rather than nested, so these keys sit beside `ppid` and `pidversion` like the
/// identity fields they extend.
struct ProcessLineage: Codable, Sendable, Equatable {
    /// Parent pid at process creation. ESF keeps it constant when the process is reparented, so a double-forked daemon
    /// whose `ppid` became 1 (launchd) still names the process that created it. A pid only: the creator's generation is on
    /// the fork event's `parent_pidversion`.
    var originalPpid: pid_t?
    /// Kernel PID generation of the CURRENT parent, from the same audit token as the payload's `ppid`. That parent is
    /// launchd for any daemonized process, so this pins `ppid` exactly; it is not the creator.
    var parentPidVersion: UInt32?
    /// The process macOS holds responsible for this one. For a launchd-spawned XPC service it is the app that requested
    /// the service. Every process started from a terminal shares the terminal app as its responsible process, so this
    /// cannot tell two processes in the same terminal apart.
    var responsiblePid: pid_t?
    var responsiblePidVersion: UInt32?
    /// POSIX session and process group. A process group survives daemonizing by double fork, `nohup`, or `disown`, and
    /// only `setsid` replaces it.
    var sessionID: pid_t?
    var groupID: pid_t?
    /// Controlling terminal device path. Absent for a process with no controlling terminal.
    var tty: String?
    /// When the process was created (its fork), in Unix nanoseconds. Unchanged by exec.
    var startTimeNs: UInt64?

    enum CodingKeys: String, CodingKey {
        case originalPpid = "original_ppid"
        case parentPidVersion = "parent_pidversion"
        case responsiblePid = "responsible_pid"
        case responsiblePidVersion = "responsible_pidversion"
        case sessionID = "session_id"
        case groupID = "group_id"
        case tty
        case startTimeNs = "start_time_ns"
    }
}
