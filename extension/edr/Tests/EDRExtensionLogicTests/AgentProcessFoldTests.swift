// The rollup's fold by process: past the budget, rows of one operation on one path fold into the nearest observed process their
// processes all descend from, so a fan-out's jobs become one row on the process that started them rather than rows dropped.

import Foundation
@testable import EDRExtensionLogic
import XCTest

final class AgentProcessFoldTests: XCTestCase {
    private let claude = ProcessKey(pid: 2736, pidVersion: 5391)
    private let shell = ProcessKey(pid: 2801, pidVersion: 5400)

    private func record(_ op: FileOperation, _ path: String, at time: UInt64, by process: ProcessKey) -> FileActivityRecord {
        FileActivityRecord(process: process, operation: op, path: path, sourcePath: nil, timeNs: time)
    }

    // spec:endpoint-event-collection/file-activity-rows-fold-by-process/a-fan-out-folds-into-the-process-that-started-it
    func testProcessSetKeepsLineage() {
        var set = AgentProcessSet(capacity: 8)
        set.exec(from: ProcessKey(pid: claude.pid, pidVersion: 1), to: claude, root: true)
        set.fork(parent: claude, child: shell, root: false)
        let execd = ProcessKey(pid: shell.pid, pidVersion: shell.pidVersion + 1)
        set.exec(from: shell, to: execd, root: false)
        let child = ProcessKey(pid: 3100, pidVersion: 1)
        set.fork(parent: execd, child: child, root: false)
        XCTAssertEqual(set.lineage(of: claude), [], "a root descends from no observed process")
        XCTAssertEqual(set.lineage(of: execd), [claude], "an exec keeps its predecessor's parent")
        XCTAssertEqual(set.lineage(of: child), [execd, claude])
        set.exit(execd)
        XCTAssertEqual(set.lineage(of: child), [execd], "a parent that has exited ends the lineage")
        XCTAssertEqual(set.lineage(of: ProcessKey(pid: 9, pidVersion: 9)), [], "an unobserved process has none")
    }

    /// Fanout is an agent session's pipeline as edr-dev ran it: Claude Code starts a shell, which starts `xargs`, which forks a child
    /// per job; each job closes the output file it inherited, on stdout and on stderr.
    private struct Fanout {
        let shellKey = ProcessKey(pid: 2801, pidVersion: 5400)
        let xargs = ProcessKey(pid: 2900, pidVersion: 5500)
        let root: ProcessKey
        var lineages: [ProcessKey: [ProcessKey]] = [:]

        init(root: ProcessKey, jobs: Int, firstPid: pid_t) {
            self.root = root
            lineages[shellKey] = [root]
            lineages[xargs] = [shellKey, root]
            for job in 0..<jobs {
                lineages[ProcessKey(pid: firstPid + pid_t(job), pidVersion: 1)] = [xargs, shellKey, root]
            }
        }

        var jobs: [ProcessKey] { lineages.keys.filter { lineages[$0]?.first == xargs }.sorted { $0.pid < $1.pid } }
    }

    private func add(_ record: FileActivityRecord, to rollup: inout FileActivityRollup, lineages: [ProcessKey: [ProcessKey]]) {
        rollup.add(record) { lineages[record.process] ?? [] }
    }

    // spec:endpoint-event-collection/file-activity-rows-fold-by-process/a-fan-out-folds-into-the-process-that-started-it
    func testAFanOutFoldsIntoTheProcessThatStartedIt() throws {
        var rollup = FileActivityRollup(budget: 64, maxPending: 4096)
        let fanout = Fanout(root: claude, jobs: 80, firstPid: 4000)
        let output = "/private/tmp/claude-501/-Users-dev-app/0b7c8e5e/tasks/b1.output"
        for job in fanout.jobs {
            for _ in 0..<2 {
                add(record(.modify, output, at: UInt64(job.pid), by: job), to: &rollup, lineages: fanout.lineages)
            }
        }
        // The session itself writes its transcript, and the shell three files in one directory: rows the fold has no need to touch.
        let transcript = "/Users/dev/.claude/projects/p/0b7c8e5e.jsonl"
        add(record(.modify, transcript, at: 1, by: claude), to: &rollup, lineages: fanout.lineages)
        for name in ["a", "b", "c"] {
            add(record(.create, "/repo/build/\(name).o", at: 2, by: fanout.shellKey), to: &rollup, lineages: fanout.lineages)
        }
        let rows = rollup.flush()
        XCTAssertEqual(rollup.dropped, 0, "nothing is dropped")
        XCTAssertLessThanOrEqual(rows.count, 64)
        let folded = try XCTUnwrap(rows.first { $0.payload.path == output })
        XCTAssertEqual(rows.filter { $0.payload.path == output }.count, 1, "the jobs' rows fold into one")
        XCTAssertEqual(ProcessKey(pid: folded.payload.pid, pidVersion: folded.payload.pidVersion), fanout.xargs,
                       "it names the process the jobs all descend from")
        XCTAssertEqual(folded.payload.processCount, 80)
        XCTAssertEqual(folded.payload.coalescedCount, 160)
        XCTAssertFalse(folded.payload.rolledUp, "it keeps the file's path")
        XCTAssertEqual(folded.firstNs, 4000)
        XCTAssertEqual(folded.lastNs, 4079)
        let session = try XCTUnwrap(rows.first { $0.payload.path == transcript }?.payload)
        XCTAssertEqual(session.pid, claude.pid, "a row that folds with no other keeps its process")
        XCTAssertNil(session.processCount)
        XCTAssertEqual(rows.filter { $0.payload.path.hasPrefix("/repo/build/") }.count, 3, "and its path")
    }

    // spec:endpoint-event-collection/file-activity-rows-fold-by-process/two-sessions-never-share-a-row
    func testTwoSessionsNeverShareARow() {
        var rollup = FileActivityRollup(budget: 4, maxPending: 4096)
        let otherRoot = ProcessKey(pid: 7000, pidVersion: 1)
        var lineages: [ProcessKey: [ProcessKey]] = [:]
        let output = "/private/tmp/shared.log"
        for (root, firstPid) in [(claude, pid_t(4000)), (otherRoot, 5000)] {
            for job in 0..<10 {
                let key = ProcessKey(pid: firstPid + pid_t(job), pidVersion: 1)
                lineages[key] = [root]
                add(record(.modify, output, at: 1, by: key), to: &rollup, lineages: lineages)
            }
        }
        let rows = rollup.flush()
        XCTAssertEqual(rows.map(\.payload.pid).sorted(), [claude.pid, otherRoot.pid], "each session's jobs fold into its own root")
        XCTAssertEqual(rows.map(\.payload.processCount), [10, 10])
        XCTAssertEqual(rows.first { $0.payload.pid == claude.pid }?.processes.allSatisfy { $0.pid < 5000 }, true)
    }

    // spec:endpoint-event-collection/file-activity-rows-fold-by-process/a-fan-out-folds-into-the-process-that-started-it
    // spec:endpoint-event-collection/file-activity-rows-fold-by-process/two-sessions-never-share-a-row
    func testProcessFoldInvariants() {
        var generator = SystemRandomNumberGenerator()
        for _ in 0..<200 {
            // A random forest: each process is a root, or the child of one made before it.
            var lineages: [ProcessKey: [ProcessKey]] = [:]
            var processes: [ProcessKey] = []
            for pid in 1...Int.random(in: 1...40, using: &generator) {
                let key = ProcessKey(pid: pid_t(pid), pidVersion: 1)
                if let parent = processes.randomElement(using: &generator), Bool.random(using: &generator) || pid > 3 {
                    lineages[key] = [parent] + (lineages[parent] ?? [])
                } else {
                    lineages[key] = []
                }
                processes.append(key)
            }
            let budget = Int.random(in: 1...12, using: &generator)
            var rollup = FileActivityRollup(budget: budget, maxPending: Int.random(in: budget...(budget * 4), using: &generator))
            var records: [FileActivityRecord] = []
            for _ in 0..<Int.random(in: 0...200, using: &generator) {
                let path = "/" + (0..<Int.random(in: 1...4, using: &generator)).map { _ in
                    String(Int.random(in: 0...2, using: &generator))
                }.joined(separator: "/")
                let op: FileOperation = [.create, .modify, .delete].randomElement(using: &generator) ?? .create
                let process = processes.randomElement(using: &generator) ?? claude
                records.append(record(op, path, at: UInt64.random(in: 1...1000, using: &generator), by: process))
            }
            records.forEach { add($0, to: &rollup, lineages: lineages) }
            let rows = rollup.flush()
            XCTAssertLessThanOrEqual(rows.count, budget)
            XCTAssertEqual(rows.reduce(0) { $0 + $1.count } + rollup.dropped, records.count, "every operation is sent or counted as dropped")
            for row in rows {
                let named = ProcessKey(pid: row.payload.pid, pidVersion: row.payload.pidVersion)
                let covered = records.filter { covered in
                    covered.operation == row.payload.operation && row.processes.contains(covered.process) &&
                        (covered.path == row.payload.path || covered.path.hasPrefix(row.payload.path + "/"))
                }
                XCTAssertGreaterThanOrEqual(covered.count, row.count, "a row covers only its processes' operations on paths under it")
                for member in row.processes {
                    XCTAssertTrue(member == named || lineages[member]?.contains(named) == true,
                                  "every process a row stands for is the process it names or descends from it")
                }
                XCTAssertEqual(row.payload.processCount, row.processes.count > 1 ? row.processes.count : nil)
                if row.processes.count > 1 {
                    let roots = Set(row.processes.map { lineages[$0]?.last ?? $0 })
                    XCTAssertEqual(roots.count, 1, "a row never folds two subtrees together")
                    let shared = row.processes.map { [$0] + (lineages[$0] ?? []) }
                    let deeper = shared[0].prefix { $0 != named }
                    XCTAssertFalse(deeper.contains { ancestor in shared.allSatisfy { $0.contains(ancestor) } },
                                   "it names the nearest process they all descend from")
                } else {
                    XCTAssertEqual(row.processes, [named], "a row that folded no other process keeps its own")
                }
            }
        }
    }
}
