import Foundation

/// AtomicFile is the one way the extensions persist a pushed document: the application-control snapshot, the watched-path set, and the
/// network containment state. Shared so the stores cannot drift apart on how a write survives a crash, on what a missing directory
/// means, or on who may read the result.
enum AtomicFile {
    /// The persisted documents tell a local user what the EDR watches and blocks, and the containment state names the addresses a contained
    /// host may still reach. Only root (the extensions and the agent) reads them, so the directory is owner-only and so is each file.
    static let directoryMode: Int = 0o700
    static let fileMode: Int = 0o600

    /// write creates the file's directory if needed and replaces the file with data atomically. Data.write(to:options: .atomic) writes
    /// a temporary file and renames it over the destination, so a crash mid-write leaves the previous file intact rather than a torn
    /// one, and there is no moment when the destination is missing.
    ///
    /// A directory open to group or other is tightened on every write, not only on creation, so an install that predates these modes is
    /// fixed by its next write. That step is best effort: a directory left listable exposes file names, not contents, and failing the
    /// write over it would lose the document across a restart. The file's own mode is not best effort. The new file takes the umask's
    /// mode until its mode is set just after the rename, and an owner-only directory keeps other users out of it for that moment.
    static func write(_ data: Data, toPath path: String) throws {
        let url = URL(fileURLWithPath: path)
        let directory = url.deletingLastPathComponent()
        let fileManager = FileManager.default
        try fileManager.createDirectory(at: directory, withIntermediateDirectories: true)
        if let mode = (try? fileManager.attributesOfItem(atPath: directory.path))?[.posixPermissions] as? NSNumber,
           mode.intValue & 0o077 != 0 {
            try? fileManager.setAttributes([.posixPermissions: directoryMode], ofItemAtPath: directory.path)
        }
        try data.write(to: url, options: .atomic)
        try fileManager.setAttributes([.posixPermissions: fileMode], ofItemAtPath: path)
    }
}
