import Foundation
@testable import EDRExtensionLogic
import XCTest

final class AtomicFileTests: XCTestCase {
    func testWriteCreatesTheDirectoryAndReplacesTheFile() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        addTeardownBlock { try? FileManager.default.removeItem(at: directory) }
        let path = directory.appendingPathComponent("nested/store.json").path

        try AtomicFile.write(Data("first".utf8), toPath: path)
        try AtomicFile.write(Data("second".utf8), toPath: path)

        XCTAssertEqual(try Data(contentsOf: URL(fileURLWithPath: path)), Data("second".utf8))
    }

    // spec:extension-xpc-server/persisted-extension-state-is-readable-only-by-root/a-pushed-document-is-written-owner-only
    func testWriteMakesTheDirectoryAndFileOwnerOnly() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        addTeardownBlock { try? FileManager.default.removeItem(at: directory) }
        // An existing world-readable directory and file, as an install that predates the modes left them.
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true,
                                                attributes: [.posixPermissions: 0o755])
        let path = directory.appendingPathComponent("store.json").path
        XCTAssertTrue(FileManager.default.createFile(atPath: path, contents: Data("old".utf8), attributes: [.posixPermissions: 0o644]))

        try AtomicFile.write(Data("new".utf8), toPath: path)

        XCTAssertEqual(try permissions(of: directory.path), 0o700)
        XCTAssertEqual(try permissions(of: path), 0o600)
        XCTAssertEqual(try Data(contentsOf: URL(fileURLWithPath: path)), Data("new".utf8))
    }

    func testAFailedReplaceThrowsAndLeavesNoTemporaryFile() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        addTeardownBlock { try? FileManager.default.removeItem(at: directory) }
        // A non-empty directory at the destination path makes the rename fail after the temporary file is written.
        let destination = directory.appendingPathComponent("store.json")
        try FileManager.default.createDirectory(at: destination.appendingPathComponent("occupied"), withIntermediateDirectories: true)

        XCTAssertThrowsError(try AtomicFile.write(Data("x".utf8), toPath: destination.path))

        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: directory.path), ["store.json"])
    }

    private func permissions(of path: String) throws -> Int {
        let attributes = try FileManager.default.attributesOfItem(atPath: path)
        return try XCTUnwrap(attributes[.posixPermissions] as? NSNumber).intValue
    }

    func testWriteThrowsWhenTheDirectoryCannotBeCreated() throws {
        let blocker = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        addTeardownBlock { try? FileManager.default.removeItem(at: blocker) }
        try Data("a file where a directory should be".utf8).write(to: blocker)

        XCTAssertThrowsError(try AtomicFile.write(Data("x".utf8), toPath: blocker.appendingPathComponent("store.json").path))
    }
}
