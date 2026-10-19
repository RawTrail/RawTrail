import Foundation
import XCTest
@testable import EDRExtensionLogic

/// SelfAllowTests covers the AUTH_EXEC failsafe that allows RawTrail's own binaries before the snapshot is consulted, so no rule
/// can block the EDR itself.
final class SelfAllowTests: XCTestCase {
    private let releaseTeamID = PeerCodeSigningRequirement.teamID

    // spec:extension-application-control/own-binaries-are-self-allowed/the-agent-starts-under-a-block-on-the-release-team
    func testTheAgentIsAllowedUnderABlockOnTheReleaseTeam() {
        let rule = makeRule(ruleType: ApplicationControlRuleType.teamID, identifier: releaseTeamID)
        let snapshot = makeSnapshot(teamIDRules: [releaseTeamID: rule])
        let agentTuple = makeTuple(signingIDPrefixed: "\(releaseTeamID):rawtrail-agent", teamID: releaseTeamID)

        XCTAssertEqual(
            decideAuthExec(tuple: agentTuple, snapshot: snapshot, hashOutcome: .notNeeded),
            .deny(rule: rule, matchedIdentifier: releaseTeamID),
            "the snapshot alone blocks the agent, so only the failsafe keeps it running"
        )
        XCTAssertEqual(
            authExecFastPath(isPlatformBinary: false, teamID: releaseTeamID, signingID: "rawtrail-agent"), .selfAllowed,
            "the agent is allowed before the snapshot is consulted"
        )
    }

    private struct FastPathCase {
        let name: String
        let isPlatformBinary: Bool
        let signingID: String
        let want: AuthExecFastPath
    }

    func testOnlyExecsNeitherAllowCoversReachTheSnapshot() {
        let cases = [
            FastPathCase(name: "platform binary", isPlatformBinary: true, signingID: "com.apple.ls", want: .platformBinary),
            FastPathCase(name: "own binary", isPlatformBinary: false, signingID: "io.rawtrail.securityextension", want: .selfAllowed),
            FastPathCase(name: "the agent's launchd label", isPlatformBinary: false, signingID: "io.rawtrail.agent", want: .snapshot),
            FastPathCase(name: "unrelated tool", isPlatformBinary: false, signingID: "io.rawtrail.unrelated-tool", want: .snapshot)
        ]
        for testCase in cases {
            XCTAssertEqual(
                authExecFastPath(isPlatformBinary: testCase.isPlatformBinary, teamID: releaseTeamID, signingID: testCase.signingID),
                testCase.want, testCase.name
            )
        }
    }

    // spec:extension-application-control/own-binaries-are-self-allowed/another-binary-from-the-release-team-is-not-self-allowed
    func testOtherIdentifiersFromTheReleaseTeamAreNotSelfAllowed() {
        let cases: [(name: String, signingID: String)] = [
            ("the agent's launchd label", "io.rawtrail.agent"),
            ("an unrelated tool", "io.rawtrail.unrelated-tool"),
            ("an unsigned identifier", "")
        ]
        for testCase in cases {
            XCTAssertFalse(isSelfAllowed(teamID: releaseTeamID, signingID: testCase.signingID), testCase.name)
        }
    }

    func testOwnIdentifiersFromAnotherTeamAreNotSelfAllowed() {
        for signingID in selfAllowSigningIDs {
            XCTAssertFalse(isSelfAllowed(teamID: "ABCDEFGHIJ", signingID: signingID), signingID)
            XCTAssertFalse(isSelfAllowed(teamID: "", signingID: signingID), signingID)
        }
    }

    func testTheExtensionsAndHostAppAreSelfAllowed() {
        for signingID in ["io.rawtrail.securityextension", "io.rawtrail.networkextension", "io.rawtrail"] {
            XCTAssertTrue(isSelfAllowed(teamID: releaseTeamID, signingID: signingID), signingID)
        }
    }

    // spec:extension-application-control/own-binaries-are-self-allowed/the-self-allow-entry-is-the-identifier-the-agent-is-signed-with
    func testTheAgentEntryMatchesTheIdentifierTheBuildsSignWith() throws {
        // Read the identifiers from the build scripts themselves rather than from a Swift constant, so a rename in either
        // script without the extension (the #1373 drift) fails here.
        let repoRoot = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()
        let buildScript = try String(contentsOf: repoRoot.appendingPathComponent("packaging/pkg/build.sh"), encoding: .utf8)
        let taskfile = try String(contentsOf: repoRoot.appendingPathComponent("Taskfile.yml"), encoding: .utf8)

        let releaseIdentifier = try XCTUnwrap(firstCapture(#"(?m)^AGENT_SIGNING_IDENTIFIER="([^"]+)""#, in: buildScript))
        let devIdentifier = try XCTUnwrap(firstCapture(#"codesign --force --sign - --identifier (\S+) \{\{\.AGENT_BIN\}\}"#, in: taskfile))

        for (build, identifier) in [("release", releaseIdentifier), ("dev", devIdentifier)] {
            XCTAssertTrue(isSelfAllowed(teamID: releaseTeamID, signingID: identifier), "\(build) build signs the agent as \(identifier)")
        }
    }

    private func firstCapture(_ pattern: String, in text: String) -> String? {
        guard let regex = try? NSRegularExpression(pattern: pattern),
              let match = regex.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)),
              let range = Range(match.range(at: 1), in: text) else {
            return nil
        }
        return String(text[range])
    }
}
