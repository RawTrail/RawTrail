/// The process set behind agent-scoped file telemetry (issue #1287): which processes the subscriber observes, with each one's parent
/// and session, so the rollup can fold a fan-out's rows into the process that started it.

/// AgentProcessSet is the set of process generations the subscriber observes, kept from the fork, exec and exit events of the same
/// client that delivers the file events, so a child is in the set before its first file event arrives.
///
/// It holds at most `capacity` generations. A subtree that forks past it, a runaway build, leaves the excess unobserved rather than
/// growing without bound, and each refusal is counted in `refused`.
///
/// Each generation keeps the observed process it was forked from and the agent session it belongs to, so the rollup can fold the rows
/// of a fan-out into the process they all descend from without crossing into another session. A generation an exec created keeps its
/// predecessor's parent, since to the tree it is the same process, and takes the session its exec names, or else its predecessor's.
struct AgentProcessSet {
    /// Node is what the set knows of a generation: its observed parent, nil for the root of a subtree, and its session id, nil until an
    /// exec in its line carries one.
    struct Node: Equatable {
        var parent: ProcessKey?
        var session: String?
    }

    private(set) var tracked: [ProcessKey: Node] = [:]
    private(set) var refused = 0
    let capacity: Int
    /// maxLineage is the most ancestors a lineage names: a fan-out folds at most that many levels up.
    static let maxLineage = 64

    init(capacity: Int) {
        self.capacity = capacity
    }

    var count: Int { tracked.count }

    func contains(_ key: ProcessKey) -> Bool {
        tracked[key] != nil
    }

    /// lineage is the live generations of its own session a process descends from, its parent first. It ends at the first ancestor
    /// that has exited, whose own parent is no longer known and which a row folded later must not name, or that belongs to another
    /// session: Claude Code itself carries no session id, and after `/clear` its next shell starts another session.
    ///
    /// It holds at most `maxLineage` generations, which bounds the walk on the ES callback path however deep a chain of forks goes.
    func lineage(of key: ProcessKey) -> [ProcessKey] {
        guard let node = tracked[key] else {
            return []
        }
        var out: [ProcessKey] = []
        var next = node.parent
        while out.count < Self.maxLineage, let parent = next, let ancestor = tracked[parent], ancestor.session == node.session {
            out.append(parent)
            next = ancestor.parent
        }
        return out
    }

    /// fork observes the child of an observed parent, in the parent's session. A fork by a root starts observing the parent too: a
    /// Claude Code process that exec'd before the subscriber started was never seen as a root, and its helpers (`rg`, `git`) carry no
    /// session id of their own.
    mutating func fork(parent: ProcessKey, child: ProcessKey, root: Bool) {
        if root, !contains(parent) {
            insert(parent, Node())
        }
        if let node = tracked[parent] {
            insert(child, Node(parent: parent, session: node.session))
        }
    }

    /// exec moves an observed process to the generation its exec created, or starts observing a root. session is the session id the
    /// exec carries, if any.
    mutating func exec(from old: ProcessKey, to new: ProcessKey, root: Bool, session: String? = nil) {
        if let node = tracked.removeValue(forKey: old) {
            insert(new, Node(parent: node.parent, session: session ?? node.session))
        } else if root {
            insert(new, Node(session: session))
        }
    }

    /// exit stops observing a process.
    mutating func exit(_ key: ProcessKey) {
        tracked.removeValue(forKey: key)
    }

    private mutating func insert(_ key: ProcessKey, _ node: Node) {
        guard tracked.count < capacity else {
            refused += 1
            return
        }
        tracked[key] = node
    }
}
