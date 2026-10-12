import Darwin

/// FileRenamePayload is the wire shape of a `file_rename` event: a rename where either path falls inside the sensitive
/// target set the file-tamper client watches.
///
/// It is a separate payload from OpenPayload rather than an extra field on it because a rename is the only watched
/// operation carrying TWO paths, and because Sigma already models it: the `file_rename` category reads SourceFilename
/// and TargetFilename, so the FIELDS a rule reads over these events are Sigma's own rather than ones only we supply.
///
/// That is a claim about fields, not about a rule's exported portability. A rule combining this event type with another
/// exports as `portable: mapped` regardless, because Sigma permits one logsource category per rule and no external engine
/// would route both. sudoers_tamper is exactly that case. An earlier version of this comment said such a rule "stays
/// portable standard Sigma", which stopped being true when the exporter learned to check the logsource.
///
/// Field names are the server's: `server/rules/internal/sigmabind` binds `source_path` to SourceFilename and
/// `path` to TargetFilename.
struct FileRenamePayload: Codable, Sendable {
    let pid: pid_t
    /// Kernel PID generation of the acting process (issue #1279), so the event joins the exact process row rather than any
    /// process that held the pid. Omitted when unknown.
    var pidVersion: UInt32?
    /// Where the file came from. Required, and the reason this event exists: a rename can make a file become sudo
    /// policy without any write to the destination, and the source is what separates a promotion out of /tmp from a
    /// move within the watched directory.
    let sourcePath: String
    /// Where the file landed. Named `path` on the wire to match every other file event's target field.
    let path: String

    enum CodingKeys: String, CodingKey {
        case pid
        case pidVersion = "pidversion"
        case sourcePath = "source_path"
        case path
    }
}

/// FileTruncatePayload is the wire shape of a `file_truncate` event: the contents of a watched file were discarded while the
/// file itself remains.
///
/// One payload for two kernel paths that reach the same outcome. `truncate(2)` and `ftruncate(2)` surface as
/// NOTIFY_TRUNCATE; a shell redirect and most language runtimes instead `open(2)` with O_TRUNC, which emits no truncate event
/// at all and is the common case. A rule cares that the policy was emptied, not which syscall emptied it, so the distinction
/// stays out of the wire (#934).
struct FileTruncatePayload: Codable, Sendable {
    let pid: pid_t
    /// Kernel PID generation of the acting process (issue #1279), so the event joins the exact process row rather than any
    /// process that held the pid. Omitted when unknown.
    var pidVersion: UInt32?
    let path: String

    enum CodingKeys: String, CodingKey {
        case pid, path
        case pidVersion = "pidversion"
    }
}

/// FileDeletePayload is the wire shape of a `file_delete` event: a watched file was unlinked.
///
/// Separate from truncation because the outcomes differ in a way an analyst acts on: a truncated sudoers file still exists and
/// still parses, as an empty policy, while a deleted one is gone. Sigma models deletion as its own `file_delete` category, so
/// keeping them apart also keeps a rule over these events readable as standard Sigma.
struct FileDeletePayload: Codable, Sendable {
    let pid: pid_t
    /// Kernel PID generation of the acting process (issue #1279), so the event joins the exact process row rather than any
    /// process that held the pid. Omitted when unknown.
    var pidVersion: UInt32?
    let path: String

    enum CodingKeys: String, CodingKey {
        case pid, path
        case pidVersion = "pidversion"
    }
}
