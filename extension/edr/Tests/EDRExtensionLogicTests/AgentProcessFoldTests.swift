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
        XCTAssertEqual(set.lineage(of: child), [], "a parent that has exited is not named: a row folded later would outlive it")
        XCTAssertEqual(set.lineage(of: ProcessKey(pid: 9, pidVersion: 9)), [], "an unobserved process has none")
    }

    // spec:endpoint-event-collection/file-activity-rows-fold-by-process/two-sessions-never-share-a-row
    func testLineageStaysInItsSession() {
        // Claude Code carries no session id; after /clear its next shell starts another session in the same process.
        var set = AgentProcessSet(capacity: 16)
        set.exec(from: ProcessKey(pid: claude.pid, pidVersion: 1), to: claude, root: true)
        var jobs: [String: ProcessKey] = [:]
        for (index, session) in ["0b7c8e5e", "9f3a1d22"].enumerated() {
            let forked = ProcessKey(pid: 3000 + pid_t(index), pidVersion: 1)
            set.fork(parent: claude, child: forked, root: true)
            let sessionShell = ProcessKey(pid: forked.pid, pidVersion: 2)
            set.exec(from: forked, to: sessionShell, root: true, session: session)
            let job = ProcessKey(pid: 4000 + pid_t(index), pidVersion: 1)
            set.fork(parent: sessionShell, child: job, root: false)
            let execd = ProcessKey(pid: job.pid, pidVersion: 2)
            set.exec(from: job, to: execd, root: true, session: session)
            XCTAssertEqual(set.lineage(of: execd), [sessionShell], "a job's lineage keeps to its session, and an exec keeps the parent")
            XCTAssertEqual(set.lineage(of: sessionShell), [], "it ends before Claude Code, which belongs to no session")
            jobs[session] = execd
        }
        XCTAssertEqual(set.tracked[jobs["0b7c8e5e"] ?? claude]?.session, "0b7c8e5e")
    }

    // A parent that exits between two siblings' first writes leaves their lineages disagreeing: the first read [P, R], the second only
    // [P]. Their rows still meet at P, and the fold must keep both rather than settle on one.
    func testDisagreeingLineagesLoseNothing() {
        var rollup = FileActivityRollup(budget: 1, maxPending: 4096)
        let root = ProcessKey(pid: 10, pidVersion: 1)
        let parent = ProcessKey(pid: 20, pidVersion: 1)
        let early = ProcessKey(pid: 30, pidVersion: 1)
        let late = ProcessKey(pid: 31, pidVersion: 1)
        let lineages = [early: [parent, root], late: [parent]]
        for (process, time) in [(early, UInt64(1)), (late, 2), (late, 3)] {
            add(record(.modify, "/tmp/out.log", at: time, by: process), to: &rollup, lineages: lineages)
        }
        let rows = rollup.flush()
        XCTAssertEqual(rollup.dropped, 0)
        XCTAssertEqual(rows.map(\.count), [3], "every operation is in the one row")
        XCTAssertEqual(rows.first?.payload.processCount, 2)
        XCTAssertEqual(rows.first?.payload.pid, parent.pid, "named for the parent both lineages share")
    }

    // The fold's work is bounded on the ES callback path: a lineage names at most maxLineage ancestors, a row reads its lineage once
    // when it begins, and a chain of forks far deeper than that still folds with every operation counted.
    func testLineageBookkeepingIsBounded() {
        var set = AgentProcessSet(capacity: 512)
        var parent = claude
        set.exec(from: ProcessKey(pid: claude.pid, pidVersion: 1), to: claude, root: true)
        var chain: [ProcessKey] = []
        for depth in 1...300 {
            let child = ProcessKey(pid: pid_t(5000 + depth), pidVersion: 1)
            set.fork(parent: parent, child: child, root: false)
            chain.append(child)
            parent = child
        }
        XCTAssertEqual(set.lineage(of: parent).count, AgentProcessSet.maxLineage)

        var rollup = FileActivityRollup(budget: 4, maxPending: 4096)
        var reads = 0
        for process in chain {
            for _ in 0..<2 {
                rollup.add(record(.modify, "/tmp/chain/\(process.pid)", at: 1, by: process)) {
                    reads += 1
                    return set.lineage(of: process)
                }
            }
        }
        XCTAssertEqual(reads, chain.count, "a row reads its lineage when it begins, not on every operation")
        let rows = rollup.flush()
        XCTAssertLessThanOrEqual(rows.count, 4)
        XCTAssertEqual(rows.reduce(0) { $0 + $1.count } + rollup.dropped, 600)
    }

    // A row reads its lineage when it begins, so one begun after an ancestor exited cannot fold into it, though the same processes'
    // earlier rows did: a folded row never names a process that was gone at its first operation.
    func testARowBegunAfterAnAncestorExitedDoesNotNameIt() {
        var set = AgentProcessSet(capacity: 16)
        let root = ProcessKey(pid: 10, pidVersion: 1)
        set.exec(from: ProcessKey(pid: 10, pidVersion: 0), to: root, root: true)
        let jobs = [ProcessKey(pid: 20, pidVersion: 1), ProcessKey(pid: 21, pidVersion: 1)]
        jobs.forEach { set.fork(parent: root, child: $0, root: false) }
        var rollup = FileActivityRollup(budget: 2, maxPending: 4096)
        for job in jobs {
            rollup.add(record(.modify, "/tmp/seed.log", at: 1, by: job)) { set.lineage(of: job) }
        }
        set.exit(root)
        for job in jobs {
            rollup.add(record(.modify, "/tmp/later/out.log", at: 5, by: job)) { set.lineage(of: job) }
        }
        let rows = rollup.flush()
        let seed = rows.first { $0.payload.path == "/tmp/seed.log" }?.payload
        XCTAssertEqual(seed?.pid, root.pid, "rows begun while the parent ran fold into it")
        XCTAssertEqual(seed?.processCount, 2)
        XCTAssertFalse(rows.contains { $0.payload.path.hasPrefix("/tmp/later") && $0.payload.pid == root.pid },
                       "rows begun after it exited do not")
        XCTAssertEqual(rows.reduce(0) { $0 + $1.count } + rollup.dropped, 4)
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

    /// randomForest makes processes that are each a root or the child of one made before it, with their lineages.
    private func randomForest(using generator: inout SystemRandomNumberGenerator) -> ([ProcessKey], [ProcessKey: [ProcessKey]]) {
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
        return (processes, lineages)
    }

    // spec:endpoint-event-collection/file-activity-rows-fold-by-process/a-fan-out-folds-into-the-process-that-started-it
    // spec:endpoint-event-collection/file-activity-rows-fold-by-process/two-sessions-never-share-a-row
    func testProcessFoldInvariants() {
        var generator = SystemRandomNumberGenerator()
        for _ in 0..<200 {
            let (processes, lineages) = randomForest(using: &generator)
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
            // An ancestor that exits before a process's first write cuts the lineage the rollup reads for it.
            let truncated = Bool.random(using: &generator)
            let read = lineages.mapValues { lineage in
                truncated && Bool.random(using: &generator) ? Array(lineage.prefix(Int.random(in: 0...lineage.count, using: &generator)))
                    : lineage
            }
            records.forEach { add($0, to: &rollup, lineages: read) }
            let rows = rollup.flush()
            XCTAssertLessThanOrEqual(rows.count, budget)
            let sent = rows.reduce(0) { $0 + $1.count }
            XCTAssertEqual(sent + rollup.dropped, records.count, "every operation is sent or counted as dropped")
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
                    guard !truncated else {
                        continue
                    }
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
