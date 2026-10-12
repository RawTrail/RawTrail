// Issue #1279: exit and file events carry the acting process's pidversion, so the server can join them to the exact process
// row instead of any process that held the pid. These tests pin the wire key on each payload, its omission when unknown, and
// the round trip. Reading the value from a live ES message is exercised on the VM.

import Foundation
@testable import EDRExtensionLogic
import XCTest

final class ActorPidVersionTests: XCTestCase {
    private func jsonObject<Payload: Encodable>(_ payload: Payload) throws -> [String: Any] {
        try XCTUnwrap(try JSONSerialization.jsonObject(with: try JSONEncoder().encode(payload)) as? [String: Any])
    }

    // spec:endpoint-event-collection/exit-and-file-event-process-generation/an-exit-names-the-generation-that-exited
    func testExitCarriesPidVersion() throws {
        let object = try jsonObject(ExitPayload(pid: 4242, pidVersion: 5445, exitCode: 0))

        XCTAssertEqual(object["pidversion"] as? Int, 5445)
        XCTAssertEqual(object["exit_code"] as? Int, 0)
    }

    // spec:endpoint-event-collection/exit-and-file-event-process-generation/a-file-event-names-the-generation-that-acted
    func testEveryFilePayloadCarriesPidVersion() throws {
        let payloads: [(String, [String: Any])] = [
            ("open", try jsonObject(OpenPayload(pid: 4242, pidVersion: 5445, path: "/etc/sudoers", flags: 1))),
            ("file_rename", try jsonObject(FileRenamePayload(pid: 4242, pidVersion: 5445, sourcePath: "/tmp/x", path: "/etc/sudoers"))),
            ("file_truncate", try jsonObject(FileTruncatePayload(pid: 4242, pidVersion: 5445, path: "/etc/sudoers"))),
            ("file_delete", try jsonObject(FileDeletePayload(pid: 4242, pidVersion: 5445, path: "/etc/sudoers")))
        ]
        for (name, object) in payloads {
            XCTAssertEqual(object["pidversion"] as? Int, 5445, name)
            XCTAssertEqual(object["pid"] as? Int, 4242, name)
        }
    }

    // spec:endpoint-event-collection/exit-and-file-event-process-generation/a-payload-without-a-generation-keeps-its-shape
    func testPayloadWithoutPidVersionOmitsTheKey() throws {
        let objects = [
            try jsonObject(ExitPayload(pid: 1, exitCode: 0)),
            try jsonObject(OpenPayload(pid: 1, path: "/etc/sudoers", flags: 1)),
            try jsonObject(FileRenamePayload(pid: 1, sourcePath: "/tmp/x", path: "/etc/sudoers")),
            try jsonObject(FileTruncatePayload(pid: 1, path: "/etc/sudoers")),
            try jsonObject(FileDeletePayload(pid: 1, path: "/etc/sudoers"))
        ]
        for object in objects {
            XCTAssertNil(object["pidversion"])
        }
    }

    // The full UInt32 range survives, including values above Int32.max that a signed decode would reject.
    func testPidVersionRoundTripsAcrossTheFullRange() throws {
        for value in [UInt32(0), 1, UInt32(Int32.max) + 1, .max] {
            let exit = ExitPayload(pid: 1, pidVersion: value, exitCode: 0)
            XCTAssertEqual(try JSONDecoder().decode(ExitPayload.self, from: try JSONEncoder().encode(exit)).pidVersion, value)
            let delete = FileDeletePayload(pid: 1, pidVersion: value, path: "/x")
            XCTAssertEqual(try JSONDecoder().decode(FileDeletePayload.self, from: try JSONEncoder().encode(delete)).pidVersion, value)
        }
    }
}
