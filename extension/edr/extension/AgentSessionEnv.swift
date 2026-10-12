/// AI agent session markers read from an exec'd process's environment (issue #1278).
///
/// Claude Code exports its session id into the environment of every process it spawns, and that id is the name of the
/// session's transcript on disk. The environment is inherited, so one exec attributes a whole subtree to a named agent
/// session, including processes that were reparented to launchd and that lineage alone loses.
///
/// Keys are matched on the FULL name and never on a prefix. An agent's environment routinely holds live credentials:
/// `CLAUDE_CODE_MESSAGING_TOKEN` is a secret that shares the `CLAUDE_CODE_` prefix with the session id, so a prefix match
/// would ship it on every exec. Adding a key is a deliberate act: every entry must be a marker with no secret value,
/// observed on a real agent process rather than taken from a vendor's documentation.
let agentSessionEnvAllowlist: Set<String> = [
    // Claude Code, each observed on a live session's subprocesses (Claude Code 2.1.296, macOS 26).
    "CLAUDE_CODE_SESSION_ID",       // session UUID; equals the transcript filename under ~/.claude/projects/<dir>/
    "CLAUDE_PID",                   // pid of the owning `claude` process
    "CLAUDECODE",                   // "1" in every Claude Code subprocess
    "CLAUDE_CODE_CHILD_SESSION",    // "1" in every Claude Code subprocess, including a top-level session's
    "CLAUDE_CODE_ENTRYPOINT",       // how the session was started, e.g. "cli"
    "CLAUDE_CODE_SESSION_ATTENDED", // whether the session reports a human present
    // Product, version and role: "claude-code_2-1-296_agent" in a tool call's processes, "..._harness" in Claude Code's own
    // helpers (git, rg), which carry no session id. Whether other agents set it is not yet observed (#1290).
    "AI_AGENT"
]

/// Longest marker value captured, in UTF-8 bytes. A longer value is dropped, not truncated: a truncated session id would
/// join the process to the wrong session. Every observed marker is under 64 bytes.
let agentSessionEnvMaxValueBytes = 256

/// Longest allowlisted name, in bytes. An entry whose name is longer cannot match, so it is skipped without building a string.
private let agentSessionEnvMaxNameBytes = agentSessionEnvAllowlist.map(\.utf8.count).max() ?? 0

/// The allowlisted markers among `entries`, which are the raw `NAME=value` bytes `es_exec_env` returns, or nil when there are
/// none, so a process without markers keeps the exec payload's existing shape. Strings are built only for a short name and an
/// accepted value, so the rest of the environment, credentials included, is never copied. When a name repeats, the first
/// entry wins, as it does for `getenv`, even when that first value is dropped for its length or encoding.
func agentSessionEnv<Entries: Sequence>(from entries: Entries) -> [String: String]?
where Entries.Element: Collection, Entries.Element.Element == UInt8 {
    var markers: [String: String] = [:]
    var seen: Set<String> = []
    for entry in entries {
        guard let separator = entry.firstIndex(of: UInt8(ascii: "=")),
              entry.distance(from: entry.startIndex, to: separator) <= agentSessionEnvMaxNameBytes,
              let name = String(bytes: entry[..<separator], encoding: .utf8),
              agentSessionEnvAllowlist.contains(name),
              seen.insert(name).inserted else { continue }
        let valueBytes = entry[entry.index(after: separator)...]
        guard valueBytes.count <= agentSessionEnvMaxValueBytes, let value = String(bytes: valueBytes, encoding: .utf8) else {
            continue
        }
        markers[name] = value
    }
    return markers.isEmpty ? nil : markers
}
