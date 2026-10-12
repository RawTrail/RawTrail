import Foundation

/// AtomicFile is the one way the extensions persist a pushed document: the application-control snapshot, the watched-path set, and the
/// network containment state. Shared so the stores cannot drift apart on how a write survives a crash, on what a missing directory
/// means, or on who may read the result.
enum AtomicFile {
    /// The persisted documents tell a local user what the EDR watches and blocks, and the containment state names the addresses a contained
    /// host may still reach. Only root (the extensions and the agent) reads them, so the directory is owner-only and so is each file.
    static let directoryMode: Int = 0o700
    static let fileMode: mode_t = 0o600

    /// write creates the file's directory if needed and replaces the file with data atomically: it writes a temporary file beside the
    /// destination, flushes it to disk, and renames it over the destination, so a crash mid-write leaves the previous file intact rather
    /// than a torn one, and there is no moment when the destination is missing.
    ///
    /// The temporary file is created owner-only, so the document is never readable by another user, not even between the write and
    /// the rename, whatever the directory's mode. A directory open to group or other is tightened on every write so an install that
    /// predates these modes stops exposing the older files beside it. That step is best effort: failing the write over it would lose
    /// the document across a restart, and the document itself is already protected.
    static func write(_ data: Data, toPath path: String) throws {
        let url = URL(fileURLWithPath: path)
        let directory = url.deletingLastPathComponent()
        let fileManager = FileManager.default
        try fileManager.createDirectory(at: directory, withIntermediateDirectories: true)
        if let mode = (try? fileManager.attributesOfItem(atPath: directory.path))?[.posixPermissions] as? NSNumber,
           mode.intValue & 0o077 != 0 {
            try? fileManager.setAttributes([.posixPermissions: directoryMode], ofItemAtPath: directory.path)
        }

        let temporaryPath = directory.appendingPathComponent(".\(url.lastPathComponent).\(UUID().uuidString).tmp").path
        // O_EXCL with a fresh name: the file is created here with the owner-only mode, never opened as one someone else made.
        let descriptor = open(temporaryPath, O_WRONLY | O_CREAT | O_EXCL | O_CLOEXEC, fileMode)
        guard descriptor >= 0 else { throw posixError() }
        var renamed = false
        defer { if !renamed { unlink(temporaryPath) } }
        let handle = FileHandle(fileDescriptor: descriptor, closeOnDealloc: true)
        try handle.write(contentsOf: data)
        try handle.synchronize()
        try handle.close()
        guard rename(temporaryPath, path) == 0 else { throw posixError() }
        renamed = true
    }

    private static func posixError() -> Error {
        POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
    }
}
