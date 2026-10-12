// Issue #1277: the exec and fork payloads carry the lineage identity that lets the server follow a process back to its creator
// after reparenting and group processes by session and process group. These tests pin the wire keys and values, that every
// field is omitted when unknown (so an older agent's wire shape is unchanged), that the flat encoding round-trips, and the
// timeval conversion behind start_time_ns. Reading the fields out of a live es_process_t is exercised on the VM.

import Darwin
import Foundation
@testable import EDRExtensionLogic
import XCTest

final class ProcessLineageTests: XCTestCase {
    private let encoder: JSONEncoder = {
        let encoder = JSONEncoder()
        encoder.outputFormatting = .sortedKeys
        return encoder
    }()

    private let decoder = JSONDecoder()

    private let lineageKeys = [
        "original_ppid", "parent_pidversion", "responsible_pid", "responsible_pidversion", "session_id", "group_id", "tty",
        "start_time_ns"
    ]

    private func execPayload(lineage: ProcessLineage, ppid: pid_t = 1, cwd: String = "/Users/dev/project") -> ExecPayload {
        ExecPayload(
            pid: 4242, ppid: ppid, path: "/bin/sleep", args: ["/bin/sleep", "74"], cwd: cwd, uid: 501, gid: 20,
            codeSigning: nil, sha256: nil, pidVersion: 5445, lineage: lineage
        )
    }

    private func jsonObject(_ data: Data) throws -> [String: Any] {
        try XCTUnwrap(try JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    // MARK: Exec

    // spec:endpoint-event-collection/process-lineage-identity-capture/a-daemonized-process-still-names-its-creator
    //
    // The shape edr-dev produced for a double-forked daemon (#1276): ppid is launchd, original_ppid is the intermediate
    // parent, and the current parent's generation is launchd's. The keys sit at the top level of the payload, not nested.
    func testDaemonizedExecCarriesItsCreatorBesidePpid() throws {
        let lineage = ProcessLineage(originalPpid: 2315, parentPidVersion: 7)
        let object = try jsonObject(try encoder.encode(execPayload(lineage: lineage, ppid: 1)))

        XCTAssertEqual(object["ppid"] as? Int, 1)
        XCTAssertEqual(object["original_ppid"] as? Int, 2315)
        XCTAssertEqual(object["parent_pidversion"] as? Int, 7)
        XCTAssertNil(object["lineage"], "lineage must be encoded flat, not as a nested object")
    }

    // spec:endpoint-event-collection/process-lineage-identity-capture/an-exec-carries-its-grouping-identity
    func testExecCarriesEveryGroupingField() throws {
        let lineage = ProcessLineage(
            originalPpid: 892, parentPidVersion: 3, responsiblePid: 2736, responsiblePidVersion: 11, sessionID: 99973,
            groupID: 1945, tty: "/dev/ttys004", startTimeNs: 1_791_030_436_491_552_000
        )
        let object = try jsonObject(try encoder.encode(execPayload(lineage: lineage)))

        XCTAssertEqual(object["responsible_pid"] as? Int, 2736)
        XCTAssertEqual(object["responsible_pidversion"] as? Int, 11)
        XCTAssertEqual(object["session_id"] as? Int, 99973)
        XCTAssertEqual(object["group_id"] as? Int, 1945)
        XCTAssertEqual(object["tty"] as? String, "/dev/ttys004")
        XCTAssertEqual((object["start_time_ns"] as? NSNumber)?.uint64Value, 1_791_030_436_491_552_000)
        XCTAssertEqual(object["cwd"] as? String, "/Users/dev/project")
    }

    // A process with no controlling terminal (an agent's non-interactive shell) omits tty rather than sending an empty string.
    func testExecWithoutTerminalOmitsTty() throws {
        let object = try jsonObject(try encoder.encode(execPayload(lineage: ProcessLineage(sessionID: 1, groupID: 1))))
        XCTAssertNil(object["tty"])
        XCTAssertEqual(object["group_id"] as? Int, 1)
    }

    // spec:endpoint-event-collection/process-lineage-identity-capture/an-event-without-lineage-keeps-its-shape
    //
    // Built without lineage, the payload carries none of the keys, so its bytes match what the extension sent before #1277.
    func testExecWithoutLineageEmitsNoLineageKey() throws {
        let object = try jsonObject(try encoder.encode(execPayload(lineage: ProcessLineage())))
        for key in lineageKeys {
            XCTAssertNil(object[key], "\(key) must be omitted when unknown")
        }
        XCTAssertEqual(
            Set(object.keys), ["pid", "ppid", "path", "args", "cwd", "uid", "gid", "pidversion"],
            "an exec without lineage must keep the pre-#1277 key set"
        )
    }

    // Decode(encode(x)) == x across random lineage values, each field independently present or absent. The flat encoding shares
    // one keyed container with the rest of the payload, so this is what catches a key collision or a field the decoder misses.
    func testLineageRoundTripsThroughTheExecPayload() throws {
        var rng = SystemRandomNumberGenerator()
        func maybe<T>(_ value: @autoclosure () -> T) -> T? { Bool.random(using: &rng) ? value() : nil }

        for _ in 0..<500 {
            let lineage = ProcessLineage(
                originalPpid: maybe(pid_t.random(in: 0...99_999, using: &rng)),
                parentPidVersion: maybe(UInt32.random(in: 0...UInt32.max, using: &rng)),
                responsiblePid: maybe(pid_t.random(in: 0...99_999, using: &rng)),
                responsiblePidVersion: maybe(UInt32.random(in: 0...UInt32.max, using: &rng)),
                sessionID: maybe(pid_t.random(in: 0...99_999, using: &rng)),
                groupID: maybe(pid_t.random(in: 0...99_999, using: &rng)),
                tty: maybe("/dev/ttys\(Int.random(in: 0...999, using: &rng))"),
                startTimeNs: maybe(UInt64.random(in: 0...UInt64(Int64.max), using: &rng))
            )
            let decoded = try decoder.decode(ExecPayload.self, from: try encoder.encode(execPayload(lineage: lineage)))
            XCTAssertEqual(decoded.lineage, lineage)
            XCTAssertEqual(decoded.ppid, 1)
            XCTAssertEqual(decoded.pidVersion, 5445)
        }
    }

    // MARK: Snapshot

    // spec:endpoint-event-collection/process-lineage-identity-capture/a-snapshot-row-carries-what-the-process-table-knows
    //
    // The snapshot pass fills only what kinfo_proc supplies; this pins the shape it builds (ProcessSnapshotEnumerator) so the
    // fields it cannot know stay off the wire instead of arriving as zeros.
    func testSnapshotRowCarriesGroupAndStartTimeOnly() throws {
        let started = timeval(tv_sec: 1_791_030_436, tv_usec: 491_552)
        let payload = ExecPayload(
            pid: 300, ppid: 1, path: "/Applications/Slack.app/Contents/MacOS/Slack", args: [], cwd: "", uid: 501, gid: 20,
            codeSigning: nil, sha256: nil,
            lineage: ProcessLineage(groupID: 300, startTimeNs: unixNanoseconds(started)),
            snapshot: true
        )
        let object = try jsonObject(try encoder.encode(payload))

        XCTAssertEqual(object["group_id"] as? Int, 300)
        XCTAssertEqual((object["start_time_ns"] as? NSNumber)?.uint64Value, 1_791_030_436_491_552_000)
        for key in lineageKeys where key != "group_id" && key != "start_time_ns" {
            XCTAssertNil(object[key], "a snapshot row cannot know \(key), so it must be omitted")
        }
    }

    // MARK: Fork

    // spec:endpoint-event-collection/process-lineage-identity-capture/a-fork-names-the-generation-of-its-creator
    func testForkCarriesTheCreatorsGeneration() throws {
        let encoded = try encoder.encode(ForkPayload(childPid: 2316, parentPid: 2315, pidVersion: 5444, parentPidVersion: 5443))
        let object = try jsonObject(encoded)

        XCTAssertEqual(object["parent_pid"] as? Int, 2315)
        XCTAssertEqual(object["parent_pidversion"] as? Int, 5443)
        XCTAssertEqual(object["pidversion"] as? Int, 5444, "the child's own generation must stay under pidversion")
        XCTAssertEqual(try decoder.decode(ForkPayload.self, from: encoded).parentPidVersion, 5443)

        let without = try jsonObject(try encoder.encode(ForkPayload(childPid: 2316, parentPid: 2315, pidVersion: 5444)))
        XCTAssertNil(without["parent_pidversion"])
    }

    // MARK: timeval

    func testUnixNanosecondsKeepsMicrosecondPrecision() {
        XCTAssertEqual(unixNanoseconds(timeval(tv_sec: 0, tv_usec: 0)), 0)
        XCTAssertEqual(unixNanoseconds(timeval(tv_sec: 0, tv_usec: 1)), 1_000)
        XCTAssertEqual(unixNanoseconds(timeval(tv_sec: 1, tv_usec: 999_999)), 1_999_999_000)
        XCTAssertEqual(unixNanoseconds(timeval(tv_sec: 1_791_030_436, tv_usec: 491_552)), 1_791_030_436_491_552_000)
    }
}
