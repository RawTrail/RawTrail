// Issue #1278: exec events carry the AI agent session markers from the process environment, read through an allowlist matched
// on the full variable name. These tests pin the allowlist match, that a credential sharing the markers' prefix never ships,
// the value cap, and that a process without markers keeps the exec payload's shape. Reading es_exec_env is exercised on the VM.

import Foundation
@testable import EDRExtensionLogic
import XCTest

final class AgentSessionEnvTests: XCTestCase {
    private let sessionID = "0b7c8e5e-3f4a-4c1e-9d2b-6a1f2e3d4c5b"

    // The environment of a Bash tool call's shell, trimmed to the variables that matter here.
    private var agentShellEnvironment: [String] {
        [
            "PATH=/usr/bin:/bin",
            "HOME=/Users/dev",
            "CLAUDE_CODE_SESSION_ID=\(sessionID)",
            "CLAUDE_PID=2736",
            "CLAUDECODE=1",
            "CLAUDE_CODE_CHILD_SESSION=1",
            "CLAUDE_CODE_ENTRYPOINT=cli",
            "CLAUDE_CODE_SESSION_ATTENDED=1",
            "AI_AGENT=claude-code_2-1-296_agent",
            "CLAUDE_CODE_MESSAGING_TOKEN=secret-token-value",
            "ANTHROPIC_API_KEY=sk-ant-secret"
        ]
    }

    private func execPayload(env: [String: String]?) -> ExecPayload {
        ExecPayload(
            pid: 4242, ppid: 2736, path: "/bin/zsh", args: ["/bin/zsh", "-c", "ls"], cwd: "/Users/dev/project", uid: 501, gid: 20,
            codeSigning: nil, sha256: nil, pidVersion: 5445, env: env
        )
    }

    private func jsonObject(_ payload: ExecPayload) throws -> [String: Any] {
        let encoder = JSONEncoder()
        encoder.outputFormatting = .sortedKeys
        return try XCTUnwrap(try JSONSerialization.jsonObject(with: try encoder.encode(payload)) as? [String: Any])
    }

    // spec:endpoint-event-collection/agent-session-markers-capture/an-agent-spawned-process-carries-its-session
    func testAgentShellKeepsEveryAllowlistedMarker() throws {
        let markers = try XCTUnwrap(agentSessionEnv(from: agentShellEnvironment))

        XCTAssertEqual(markers, [
            "CLAUDE_CODE_SESSION_ID": sessionID,
            "CLAUDE_PID": "2736",
            "CLAUDECODE": "1",
            "CLAUDE_CODE_CHILD_SESSION": "1",
            "CLAUDE_CODE_ENTRYPOINT": "cli",
            "CLAUDE_CODE_SESSION_ATTENDED": "1",
            "AI_AGENT": "claude-code_2-1-296_agent"
        ])
        let object = try jsonObject(execPayload(env: markers))
        XCTAssertEqual((object["env"] as? [String: String])?["CLAUDE_CODE_SESSION_ID"], sessionID)
    }

    // spec:endpoint-event-collection/agent-session-markers-capture/a-credential-never-leaves-the-host
    //
    // CLAUDE_CODE_MESSAGING_TOKEN shares the CLAUDE_CODE_ prefix with the session id, so a prefix match would ship it.
    func testCredentialSharingTheMarkerPrefixIsDropped() throws {
        let markers = try XCTUnwrap(agentSessionEnv(from: agentShellEnvironment))
        let wire = try XCTUnwrap(String(data: try JSONEncoder().encode(execPayload(env: markers)), encoding: .utf8))

        XCTAssertNil(markers["CLAUDE_CODE_MESSAGING_TOKEN"])
        for secret in ["CLAUDE_CODE_MESSAGING_TOKEN", "secret-token-value", "ANTHROPIC_API_KEY", "sk-ant-secret"] {
            XCTAssertFalse(wire.contains(secret), "\(secret) must never reach the wire")
        }
    }

    // A name that only starts or ends with an allowlisted one is a different variable.
    func testNameMustMatchInFull() {
        XCTAssertNil(agentSessionEnv(from: ["CLAUDE_CODE_SESSION_ID_EXTRA=x", "XCLAUDECODE=1", "CLAUDE=1", "AI_AGENT_TOKEN=x"]))
    }

    // spec:endpoint-event-collection/agent-session-markers-capture/an-oversized-marker-is-dropped
    func testValueOverTheCapIsDroppedNotTruncated() {
        let atCap = String(repeating: "a", count: agentSessionEnvMaxValueBytes)
        let overCap = String(repeating: "b", count: agentSessionEnvMaxValueBytes + 1)

        let markers = agentSessionEnv(from: ["AI_AGENT=\(atCap)", "CLAUDE_CODE_SESSION_ID=\(overCap)"])

        XCTAssertEqual(markers, ["AI_AGENT": atCap])
    }

    // The cap counts UTF-8 bytes, so a value of few but wide characters cannot slip past it.
    func testCapCountsBytesNotCharacters() {
        let wide = String(repeating: "é", count: agentSessionEnvMaxValueBytes / 2 + 1)
        XCTAssertNil(agentSessionEnv(from: ["AI_AGENT=\(wide)"]))
    }

    // getenv returns the first of two entries with one name, so the marker matches what the process itself reads.
    func testFirstEntryOfARepeatedNameWins() {
        XCTAssertEqual(agentSessionEnv(from: ["CLAUDECODE=1", "CLAUDECODE=0"]), ["CLAUDECODE": "1"])
    }

    // A value may itself contain "="; only the first one separates the name.
    func testValueKeepsLaterEqualsSigns() {
        XCTAssertEqual(agentSessionEnv(from: ["AI_AGENT=a=b", "CLAUDECODE"]), ["AI_AGENT": "a=b"])
    }

    // spec:endpoint-event-collection/agent-session-markers-capture/a-process-without-markers-keeps-its-shape
    func testProcessWithoutMarkersOmitsEnv() throws {
        let markers = agentSessionEnv(from: ["PATH=/usr/bin", "CLAUDE_CODE_MESSAGING_TOKEN=secret-token-value"])
        XCTAssertNil(markers)

        let object = try jsonObject(execPayload(env: markers))
        XCTAssertNil(object["env"])
    }

    func testEnvRoundTrips() throws {
        let original = execPayload(env: ["CLAUDE_CODE_SESSION_ID": sessionID, "CLAUDECODE": "1"])
        let decoded = try JSONDecoder().decode(ExecPayload.self, from: try JSONEncoder().encode(original))
        XCTAssertEqual(decoded.env, original.env)

        let withoutEnv = try JSONDecoder().decode(ExecPayload.self, from: try JSONEncoder().encode(execPayload(env: nil)))
        XCTAssertNil(withoutEnv.env)
    }
}
