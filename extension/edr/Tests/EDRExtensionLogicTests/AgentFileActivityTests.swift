// Issue #1287: the agent-file client observes an AI agent's subtree for file activity. These tests pin its pure half: which execs
// start an agent, the observed set's lifecycle (add on fork, re-key on exec, release on exit), and the rollup that bounds a window.
// The Endpoint Security wiring (the subscriptions and the handler's filter against the set) is exercised on the VM.

import Foundation
@testable import EDRExtensionLogic
import XCTest

final class AgentFileActivityTests: XCTestCase {
    private let claude = ProcessKey(pid: 2736, pidVersion: 5391)
    private let shell = ProcessKey(pid: 2801, pidVersion: 5400)

    // spec:endpoint-event-collection/agent-scoped-file-activity/an-agent-process-is-observed-from-its-exec
    func testAgentRoots() {
        struct RootCase {
            let name: String
            let signing: CodeSigning?
            let env: [String: String]?
            let root: Bool
        }
        // edr-dev reports the Claude Code binary with no team and as a platform binary (SIP off), so the identifier alone decides.
        let claudeSigning = CodeSigning(teamID: "", signingID: claudeCodeSigningID, flags: 0, isPlatformBinary: true)
        let otherSigning = CodeSigning(teamID: "ABCDE12345", signingID: "com.example.tool", flags: 0, isPlatformBinary: false)
        let cases = [
            RootCase(name: "the Claude Code binary", signing: claudeSigning, env: nil, root: true),
            RootCase(name: "a session's process", signing: nil, env: ["CLAUDE_CODE_SESSION_ID": "0b7c8e5e", "CLAUDECODE": "1"], root: true),
            RootCase(name: "a harness helper without a session id", signing: nil, env: ["AI_AGENT": "claude-code_2-1-296_harness"], root: false),
            RootCase(name: "an unrelated process", signing: otherSigning, env: nil, root: false)
        ]
        for testCase in cases {
            XCTAssertEqual(isAgentRoot(codeSigning: testCase.signing, env: testCase.env), testCase.root, testCase.name)
        }
    }

    // spec:endpoint-event-collection/agent-scoped-file-activity/the-observed-set-follows-the-subtree
    // spec:endpoint-event-collection/agent-scoped-file-activity/a-process-outside-an-agent-subtree-is-not-observed
    func testProcessSetLifecycle() {
        var set = AgentProcessSet(capacity: 8)
        let launcher = ProcessKey(pid: claude.pid, pidVersion: claude.pidVersion - 1)
        set.exec(from: launcher, to: claude, root: true)
        XCTAssertTrue(set.contains(claude), "a root is observed from its exec")
        XCTAssertFalse(set.contains(launcher), "but not the process it exec'd from")

        set.fork(parent: claude, child: shell)
        XCTAssertTrue(set.contains(shell), "an observed parent's child is observed from the fork")
        let stranger = ProcessKey(pid: 900, pidVersion: 1)
        let strangerChild = ProcessKey(pid: 901, pidVersion: 1)
        set.fork(parent: stranger, child: strangerChild)
        XCTAssertFalse(set.contains(strangerChild), "an unobserved parent's is not")
        set.exec(from: strangerChild, to: ProcessKey(pid: 901, pidVersion: 2), root: false)
        XCTAssertEqual(set.count, 2, "an unobserved exec that is no root changes nothing")

        let execd = ProcessKey(pid: shell.pid, pidVersion: shell.pidVersion + 1)
        set.exec(from: shell, to: execd, root: false)
        XCTAssertFalse(set.contains(shell), "exec retires the old generation")
        XCTAssertTrue(set.contains(execd), "and observes the new one")

        set.exit(execd)
        XCTAssertFalse(set.contains(execd), "exit releases the generation")
        XCTAssertEqual(set.count, 1)
    }

    // spec:endpoint-event-collection/agent-scoped-file-activity/the-observed-set-follows-the-subtree
    func testProcessSetCapacity() {
        var set = AgentProcessSet(capacity: 2)
        set.exec(from: ProcessKey(pid: claude.pid, pidVersion: 1), to: claude, root: true)
        set.fork(parent: claude, child: shell)
        let third = ProcessKey(pid: 3000, pidVersion: 1)
        set.fork(parent: claude, child: third)
        XCTAssertFalse(set.contains(third), "a fork past the capacity is not observed")
        XCTAssertEqual(set.refused, 1)
        // An exec frees the old slot as it takes the new one, so a full set still follows its processes' execs.
        let execd = ProcessKey(pid: shell.pid, pidVersion: shell.pidVersion + 1)
        set.exec(from: shell, to: execd, root: false)
        XCTAssertTrue(set.contains(execd))
        set.exit(execd)
        set.fork(parent: claude, child: third)
        XCTAssertTrue(set.contains(third), "an exit makes room again")
    }

    private func record(_ op: FileOperation, _ path: String, at time: UInt64, by process: ProcessKey? = nil,
                        from source: String? = nil) -> FileActivityRecord {
        FileActivityRecord(process: process ?? shell, operation: op, path: path, sourcePath: source, timeNs: time)
    }

    // spec:endpoint-event-collection/agent-scoped-file-activity/repeats-of-one-file-fold-into-one-row
    func testRepeatsFold() {
        var rollup = FileActivityRollup(budget: 8, maxPending: 64)
        let transcript = "/Users/dev/.claude/projects/p/0b7c8e5e.jsonl"
        for time in [100, 300, 200] as [UInt64] {
            rollup.add(record(.modify, transcript, at: time, by: claude))
        }
        rollup.add(record(.rename, "/repo/main.go", at: 150, from: "/repo/.main.go.tmp"))
        let rows = rollup.flush()
        XCTAssertEqual(rows.count, 2)
        let modify = rows[0].payload
        XCTAssertEqual(rows[0].firstNs, 100, "the envelope carries the earliest operation")
        XCTAssertEqual(modify.coalescedCount, 3)
        XCTAssertEqual(modify.lastTimestampNs, 300)
        XCTAssertFalse(modify.rolledUp)
        XCTAssertEqual(modify.path, transcript)
        let rename = rows[1].payload
        XCTAssertEqual(rename.sourcePath, "/repo/.main.go.tmp", "a single rename keeps its source")
        XCTAssertNil(rename.coalescedCount, "a single operation omits the count")
        XCTAssertNil(rename.lastTimestampNs)
        XCTAssertTrue(rollup.flush().isEmpty, "a flush starts an empty window")

        // A rename repeated from one source keeps it; one from another source leaves the row naming none.
        rollup.add(record(.rename, "/repo/a.go", at: 1, from: "/repo/.a.tmp"))
        rollup.add(record(.rename, "/repo/a.go", at: 2, from: "/repo/.a.tmp"))
        rollup.add(record(.rename, "/repo/b.go", at: 3, from: "/repo/.b.tmp"))
        rollup.add(record(.rename, "/repo/b.go", at: 4, from: "/repo/.b2.tmp"))
        let renames = rollup.flush().map(\.payload)
        XCTAssertEqual(renames.map(\.sourcePath), ["/repo/.a.tmp", nil])
        XCTAssertEqual(renames.map(\.coalescedCount), [2, 2])
    }

    // spec:endpoint-event-collection/agent-scoped-file-activity/a-busy-window-rolls-up-into-directories
    func testBusyWindowRollsUp() {
        var rollup = FileActivityRollup(budget: 4, maxPending: 1000)
        for package in ["a", "b", "c"] {
            for file in 0..<20 {
                rollup.add(record(.create, "/repo/node_modules/\(package)/lib/f\(file).js", at: UInt64(file)))
            }
        }
        rollup.add(record(.modify, "/repo/package.json", at: 5))
        let rows = rollup.flush()
        XCTAssertLessThanOrEqual(rows.count, 4)
        let byPath = Dictionary(uniqueKeysWithValues: rows.map { ($0.payload.path, $0.payload) })
        // Folding stops at the deepest level that fits: one row per package's lib directory, and the shallower edit is untouched.
        for package in ["a", "b", "c"] {
            let row = byPath["/repo/node_modules/\(package)/lib"]
            XCTAssertEqual(row?.coalescedCount, 20, package)
            XCTAssertEqual(row?.rolledUp, true, package)
        }
        XCTAssertEqual(byPath["/repo/package.json"]?.rolledUp, false)
        XCTAssertEqual(rollup.dropped, 0)
    }

    // spec:endpoint-event-collection/agent-scoped-file-activity/a-busy-window-rolls-up-into-directories
    func testOverflowDropsTheSmallestRows() {
        var rollup = FileActivityRollup(budget: 2, maxPending: 1000)
        for pid in 1...3 {
            for _ in 0..<pid {
                rollup.add(record(.create, "/tmp", at: 1, by: ProcessKey(pid: pid_t(pid), pidVersion: 1)))
            }
        }
        let rows = rollup.flush()
        XCTAssertEqual(rows.map(\.payload.pid).sorted(), [2, 3], "the rows standing for the most operations are kept")
        XCTAssertEqual(rollup.dropped, 1)
    }

    // spec:endpoint-event-collection/agent-scoped-file-activity/a-busy-window-rolls-up-into-directories
    func testRollupInvariants() {
        var generator = SystemRandomNumberGenerator()
        for _ in 0..<200 {
            let budget = Int.random(in: 1...12, using: &generator)
            let maxPending = Int.random(in: budget...(budget * 4), using: &generator)
            var rollup = FileActivityRollup(budget: budget, maxPending: maxPending)
            var records: [FileActivityRecord] = []
            for _ in 0..<Int.random(in: 0...150, using: &generator) {
                let depth = Int.random(in: 1...5, using: &generator)
                let path = "/" + (0..<depth).map { _ in String(Int.random(in: 0...2, using: &generator)) }.joined(separator: "/")
                let process = ProcessKey(pid: pid_t.random(in: 1...3, using: &generator), pidVersion: 1)
                let op: FileOperation = [.create, .modify, .rename, .delete].randomElement(using: &generator) ?? .create
                records.append(record(op, path, at: UInt64.random(in: 1...1000, using: &generator), by: process, from: "/src"))
            }
            records.forEach { rollup.add($0) }
            let rows = rollup.flush()
            XCTAssertLessThanOrEqual(rows.count, budget, "a flush stays within the budget")
            let sent = rows.reduce(0) { $0 + $1.count }
            XCTAssertEqual(sent + rollup.dropped, records.count, "every operation is sent or counted as dropped")
            for row in rows {
                XCTAssertLessThanOrEqual(row.firstNs, row.lastNs)
                let covered = records.filter { recordRow in
                    recordRow.process.pid == row.payload.pid && recordRow.operation == row.payload.operation &&
                        (recordRow.path == row.payload.path || recordRow.path.hasPrefix(row.payload.path + "/"))
                }
                XCTAssertGreaterThanOrEqual(covered.count, row.count, "a row covers only operations on paths under it")
                // Rows can overlap (a window folded early, then deeper records arrived), so the times are checked as membership.
                XCTAssertTrue(covered.contains { $0.timeNs == row.firstNs }, "the row's first time is a covered operation's")
                XCTAssertTrue(covered.contains { $0.timeNs == row.lastNs }, "the row's last time is a covered operation's")
                if row.payload.rolledUp {
                    XCTAssertNil(row.payload.sourcePath, "a rolled-up row names no single source")
                }
            }
        }
    }

    // spec:endpoint-event-collection/agent-scoped-file-activity/a-busy-window-rolls-up-into-directories
    func testPendingWindowIsBounded() {
        var rollup = FileActivityRollup(budget: 4, maxPending: 16)
        for file in 0..<500 {
            rollup.add(record(.create, "/repo/build/obj\(file % 50)/f\(file).o", at: UInt64(file)))
            XCTAssertLessThanOrEqual(rollup.pendingCount, 16, "the window folds early past its bound")
        }
        XCTAssertEqual(rollup.flush().reduce(0) { $0 + $1.count } + rollup.dropped, 500)
    }

    // spec:endpoint-event-collection/agent-scoped-file-activity/a-terminal-is-not-file-activity
    func testDeviceNodesAreNotFileActivity() {
        XCTAssertFalse(isAgentFilePath("/dev/ttys003"))
        XCTAssertFalse(isAgentFilePath("/dev/null"))
        XCTAssertFalse(isAgentFilePath(""))
        XCTAssertTrue(isAgentFilePath("/Users/dev/repo/main.go"))
        XCTAssertTrue(isAgentFilePath("/Users/dev/devices/notes.md"))
    }

    // spec:endpoint-event-collection/agent-scoped-file-activity/an-agent-process-is-observed-from-its-exec
    func testPayloadWireShape() throws {
        let rolled = FileActivityPayload(
            pid: 2801, pidVersion: 5400, operation: .create, path: "/repo/node_modules/a", coalescedCount: 20,
            lastTimestampNs: 900, rolledUp: true
        )
        let rolledJSON = try JSONSerialization.jsonObject(with: JSONEncoder().encode(rolled)) as? [String: Any]
        XCTAssertEqual(rolledJSON?["pidversion"] as? Int, 5400)
        XCTAssertEqual(rolledJSON?["operation"] as? String, "create")
        XCTAssertEqual(rolledJSON?["coalesced_count"] as? Int, 20)
        XCTAssertEqual(rolledJSON?["last_timestamp_ns"] as? Int, 900)
        XCTAssertEqual(rolledJSON?["rolled_up"] as? Bool, true)
        XCTAssertNil(rolledJSON?["source_path"])

        let single = FileActivityPayload(pid: 1, pidVersion: 2, operation: .rename, path: "/b", sourcePath: "/a")
        let singleJSON = try JSONSerialization.jsonObject(with: JSONEncoder().encode(single)) as? [String: Any]
        XCTAssertEqual(Set(singleJSON?.keys.map { $0 } ?? []), ["pid", "pidversion", "operation", "path", "source_path"],
                       "a single operation carries no count, span or rollup flag")
        XCTAssertEqual(try JSONDecoder().decode(FileActivityPayload.self, from: JSONEncoder().encode(rolled)), rolled)
    }
}
