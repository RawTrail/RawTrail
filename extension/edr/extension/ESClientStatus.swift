import Foundation

/// ESClientStatus records why macOS refused the extension an Endpoint Security client (issue #1436), so the agent can say why the
/// extension is not running instead of only that it is not. The extension exits when its client is refused, and launchd starts it again,
/// so the record is the one thing that outlives the refusal. The commonest refusal, `not_permitted`, is a missing Full Disk Access grant.
///
/// The agent reads the file; the two share no code, so its shape is pinned on both sides: `{"refused": "<reason>", "at": "<RFC 3339>"}`.
enum ESClientStatus {
    static let defaultPath = "/var/db/io.rawtrail/es-client-status.json"

    /// The reasons es_new_client gives, as the record spells them. ESFSubscriber maps the EndpointSecurity enum onto these.
    static let notPermitted = "not_permitted"
    static let notEntitled = "not_entitled"
    static let notPrivileged = "not_privileged"
    static let tooManyClients = "too_many_clients"
    static let invalidArgument = "invalid_argument"
    static let internalError = "internal"

    struct Record: Codable, Equatable {
        let refused: String
        let at: String
    }

    /// record writes that the client was refused for reason, at now. Failing to write is logged by the caller's exit path only: the
    /// extension is about to exit either way, and the agent then reports it as not activated, as it did before this record existed.
    static func record(_ reason: String, at now: Date = Date(), path: String = defaultPath) throws {
        let formatter = ISO8601DateFormatter()
        let data = try JSONEncoder().encode(Record(refused: reason, at: formatter.string(from: now)))
        try FileManager.default.createDirectory(atPath: (path as NSString).deletingLastPathComponent, withIntermediateDirectories: true)
        try AtomicFile.write(data, toPath: path)
    }

    /// clear removes the record once a client was granted: a refusal that has since been fixed must not be reported.
    static func clear(path: String = defaultPath) {
        try? FileManager.default.removeItem(atPath: path)
    }
}
