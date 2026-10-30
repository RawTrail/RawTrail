// The credential guard's pure half (issue #1431): which files it guards in which homes, which tools own each credential, and the
// decision for a process opening one.

@testable import EDRExtensionLogic
import XCTest

final class CredentialGuardTests: XCTestCase {
    private static func platform(_ signingID: String) -> CodeSigning {
        CodeSigning(teamID: "", signingID: signingID, flags: 0, isPlatformBinary: true)
    }

    private static func developerID(_ teamID: String, _ signingID: String) -> CodeSigning {
        CodeSigning(teamID: teamID, signingID: signingID, flags: 0, isPlatformBinary: false)
    }

    private static let ssh = platform("com.apple.ssh")
    private static let cat = platform("com.apple.cat")
    private static let claude = developerID("Q6L2SF6YDW", "com.anthropic.claude-code")
    private static let awsCLI = developerID("94KV3E626L", "30bd15-aws")
    private static let gitRemoteHTTP = developerID("59GAB85EFG", "com.apple.git-remote-http")
    /// adHocSSH is a binary an agent could build and sign itself, carrying ssh's identifier.
    private static let adHocSSH = CodeSigning(teamID: "", signingID: "com.apple.ssh", flags: 0x2, isPlatformBinary: false)
    /// homebrewGH is `gh` as Homebrew ships it: linker-signed ad hoc, with the Go linker's default identifier.
    private static let homebrewGH = CodeSigning(teamID: "", signingID: "a.out", flags: 0x2_0002, isPlatformBinary: false)

    private struct Case {
        let name: String
        let path: String
        let opener: CodeSigning?
        let inAgentProcessSet: Bool
        let mode: CredentialGuardMode
        let want: CredentialGuardDecision
    }

    // spec:extension-credential-guard/agents-read-credentials-only-through-their-tools/an-agent-reading-an-ssh-key-is-reported
    // spec:extension-credential-guard/agents-read-credentials-only-through-their-tools/a-push-over-ssh-is-not-reported
    // spec:extension-credential-guard/agents-read-credentials-only-through-their-tools/a-terminal-read-is-not-reported
    // spec:extension-credential-guard/agents-read-credentials-only-through-their-tools/an-ad-hoc-owner-identifier-is-not-an-owner
    // spec:extension-credential-guard/agents-read-credentials-only-through-their-tools/claude-code-is-an-agent-before-its-exec-is-seen
    // spec:extension-credential-guard/the-guard-fails-open/a-process-outside-an-agent-session-is-never-refused
    func testDecisionOverPathProcessOwnerAndMode() {
        let key = "/Users/alice/.ssh/id_ed25519"
        let aws = "/Users/alice/.aws/credentials"
        let cases: [Case] = [
            Case(name: "agent cat of a key, detect", path: key, opener: Self.cat, inAgentProcessSet: true, mode: .detect,
                 want: .wouldBlock(.sshPrivateKey)),
            Case(name: "agent cat of a key, protect", path: key, opener: Self.cat, inAgentProcessSet: true, mode: .protect,
                 want: .block(.sshPrivateKey)),
            Case(name: "agent ssh reads its key", path: key, opener: Self.ssh, inAgentProcessSet: true, mode: .protect, want: .allow),
            Case(name: "agent ssh-agent reads a key", path: "/Users/alice/.ssh/id_rsa", opener: Self.platform("com.apple.ssh-agent"),
                 inAgentProcessSet: true, mode: .protect, want: .allow),
            Case(name: "terminal cat of a key, detect", path: key, opener: Self.cat, inAgentProcessSet: false, mode: .detect,
                 want: .allow),
            Case(name: "terminal cat of a key, protect", path: key, opener: Self.cat, inAgentProcessSet: false, mode: .protect,
                 want: .allow),
            Case(name: "unsigned agent process", path: key, opener: nil, inAgentProcessSet: true, mode: .detect,
                 want: .wouldBlock(.sshPrivateKey)),
            Case(name: "ad-hoc binary with ssh's identifier", path: key, opener: Self.adHocSSH, inAgentProcessSet: true, mode: .detect,
                 want: .wouldBlock(.sshPrivateKey)),
            Case(name: "ssh is not an owner of AWS credentials", path: aws, opener: Self.ssh, inAgentProcessSet: true, mode: .detect,
                 want: .wouldBlock(.awsCredentials)),
            Case(name: "agent aws CLI reads its credentials", path: aws, opener: Self.awsCLI, inAgentProcessSet: true, mode: .protect,
                 want: .allow),
            Case(name: "Claude Code's Read tool outside the set", path: aws, opener: Self.claude, inAgentProcessSet: false,
                 mode: .detect, want: .wouldBlock(.awsCredentials)),
            Case(name: "agent gh as GitHub ships it", path: "/Users/alice/.config/gh/hosts.yml",
                 opener: Self.developerID("VEKTX9H2N7", "gh"), inAgentProcessSet: true, mode: .protect, want: .allow),
            Case(name: "agent gh as Homebrew builds it", path: "/Users/alice/.config/gh/hosts.yml", opener: Self.homebrewGH,
                 inAgentProcessSet: true, mode: .detect, want: .wouldBlock(.githubCLIToken)),
            Case(name: "agent curl reads .netrc", path: "/Users/alice/.netrc", opener: Self.platform("com.apple.curl"),
                 inAgentProcessSet: true, mode: .protect, want: .allow),
            Case(name: "agent git over HTTPS reads .netrc", path: "/Users/alice/.netrc", opener: Self.gitRemoteHTTP,
                 inAgentProcessSet: true, mode: .protect, want: .allow),
            Case(name: "another binary of git's team is not an owner of .netrc", path: "/Users/alice/.netrc",
                 opener: Self.developerID("59GAB85EFG", "com.apple.dt.xcodebuild"), inAgentProcessSet: true, mode: .detect,
                 want: .wouldBlock(.netrc)),
            Case(name: "agent docker reads its config", path: "/Users/alice/.docker/config.json",
                 opener: Self.developerID("9BNSXJN65R", "docker"), inAgentProcessSet: true, mode: .protect, want: .allow),
            Case(name: "agent python reads the kube config", path: "/var/root/.kube/config",
                 opener: Self.platform("com.apple.python3"), inAgentProcessSet: true, mode: .detect, want: .wouldBlock(.kubeConfig)),
            Case(name: "agent cat of a public key", path: "/Users/alice/.ssh/id_ed25519.pub", opener: Self.cat, inAgentProcessSet: true,
                 mode: .protect, want: .allow),
            Case(name: "agent cat of the AWS config", path: "/Users/alice/.aws/config", opener: Self.cat, inAgentProcessSet: true,
                 mode: .protect, want: .allow)
        ]
        for tc in cases {
            let got = CredentialGuard.decide(path: tc.path, opener: tc.opener, inAgentProcessSet: tc.inAgentProcessSet, mode: tc.mode)
            XCTAssertEqual(got, tc.want, tc.name)
            let refused = tc.mode == .protect && tc.want != .allow
            XCTAssertEqual(got.deniesOpen, refused, "\(tc.name): only a protect-mode block refuses the open")
        }
    }

    // spec:extension-credential-guard/credential-files-are-guarded-in-every-home/public-keys-and-ssh-config-are-not-credentials
    func testOnlyPrivateKeysUnderTheSSHPrefixAreCredentials() {
        let cases: [(path: String, want: CredentialKind?)] = [
            ("/Users/alice/.ssh/id_ed25519", .sshPrivateKey),
            ("/Users/alice/.ssh/id_rsa", .sshPrivateKey),
            ("/Users/alice/.ssh/id_ecdsa_sk", .sshPrivateKey),
            ("/Users/alice/.ssh/id_ed25519.pub", nil),
            ("/Users/alice/.ssh/id_rsa-cert.pub", nil),
            ("/Users/alice/.ssh/config", nil),
            ("/Users/alice/.ssh/known_hosts", nil),
            ("/Users/alice/.ssh/id_", nil),
            ("/Users/alice/.ssh/id_dir/inner", nil),
            ("/Users/alice/.aws/credentials", .awsCredentials),
            ("/Users/alice/.aws/credentials.bak", nil),
            ("/Users/alice/.netrc", .netrc),
            ("/private/var/root/.docker/config.json", .dockerConfig)
        ]
        for tc in cases {
            XCTAssertEqual(CredentialGuard.credential(at: tc.path)?.kind, tc.want, tc.path)
        }
    }

    // spec:extension-credential-guard/credential-files-are-guarded-in-every-home/every-home-s-credential-files-are-guarded
    func testTargetsAreEveryCredentialInEveryHome() {
        let targets = CredentialGuard.targets(homes: ["/Users/alice", "/var/root"])
        let homePaths = [".ssh/id_", ".aws/credentials", ".config/gh/hosts.yml", ".netrc", ".docker/config.json", ".kube/config"]
        var expected: [WatchedPath] = []
        for home in ["/Users/alice", "/private/var/root", "/var/root"] {
            for homePath in homePaths {
                expected.append(WatchedPath(path: home + "/" + homePath, match: homePath == ".ssh/id_" ? .prefix : .literal))
            }
        }
        XCTAssertEqual(Set(targets), Set(expected))
        XCTAssertEqual(targets.count, expected.count)
        XCTAssertEqual(Set(CredentialKind.allCases), Set(CredentialGuard.builtIn.map(\.kind)), "every credential kind is guarded")
    }

    func testTargetsAreEmptyWithNoHomes() {
        XCTAssertEqual(CredentialGuard.targets(homes: []), [])
    }

    // An owner without a team must be a platform binary; one with a team must carry it, and its signing identifier when named.
    func testOwnerMatching() {
        struct OwnerCase {
            let name: String
            let owner: CredentialOwner
            let signing: CodeSigning?
            let want: Bool

            init(_ name: String, _ owner: CredentialOwner, _ signing: CodeSigning?, _ want: Bool) {
                self.name = name
                self.owner = owner
                self.signing = signing
                self.want = want
            }
        }
        let gitRemoteHTTPOwner = CredentialOwner.team("59GAB85EFG", signingID: "com.apple.git-remote-http")
        let xcodebuild = Self.developerID("59GAB85EFG", "com.apple.dt.xcodebuild")
        let cases: [OwnerCase] = [
            OwnerCase("platform owner, platform binary", .platform("com.apple.ssh"), Self.ssh, true),
            OwnerCase("platform owner, other identifier", .platform("com.apple.ssh"), Self.cat, false),
            OwnerCase("platform owner, not a platform binary", .platform("com.apple.ssh"), Self.adHocSSH, false),
            OwnerCase("platform owner, unsigned", .platform("com.apple.ssh"), nil, false),
            OwnerCase("team owner, any binary of the team", .team("94KV3E626L"), Self.awsCLI, true),
            OwnerCase("team owner, other team", .team("94KV3E626L"), Self.claude, false),
            OwnerCase("team owner, no team", .team("94KV3E626L"), Self.cat, false),
            OwnerCase("team and identifier, both match", gitRemoteHTTPOwner, Self.gitRemoteHTTP, true),
            OwnerCase("team and identifier, other identifier", gitRemoteHTTPOwner, xcodebuild, false)
        ]
        for tc in cases {
            XCTAssertEqual(tc.owner.matches(tc.signing), tc.want, tc.name)
        }
    }

    // MARK: Wire

    private let encoder = JSONEncoder()
    private let decoder = JSONDecoder()

    // spec:extension-credential-guard/credential-would-block-event/the-payload-round-trips-and-validates
    //
    // Decode(encode(x)) == x across random payloads, and the encoding carries exactly the five keys schema/events.json declares.
    func testWouldBlockPayloadRoundTrips() throws {
        var rng = SystemRandomNumberGenerator()
        let flags = [Int(O_RDONLY), Int(O_WRONLY), Int(O_RDWR)]
        for _ in 0..<500 {
            let payload = AgentCredentialAccessPayload(
                pid: pid_t.random(in: 0...99_999, using: &rng), pidVersion: UInt32.random(in: 0...UInt32.max, using: &rng),
                path: "/Users/u\(Int.random(in: 0...999, using: &rng))/.ssh/id_\(UInt64.random(in: 0...UInt64.max, using: &rng))",
                credential: CredentialKind.allCases.randomElement(using: &rng) ?? .netrc,
                flags: flags.randomElement(using: &rng) ?? Int(O_RDONLY)
            )
            let data = try encoder.encode(payload)
            XCTAssertEqual(try decoder.decode(AgentCredentialAccessPayload.self, from: data), payload)
            let object = try XCTUnwrap(try JSONSerialization.jsonObject(with: data) as? [String: Any])
            XCTAssertEqual(Set(object.keys), ["pid", "pidversion", "path", "credential", "flags"])
        }
    }

    func testCredentialKindsKeepTheirWireNames() {
        XCTAssertEqual(CredentialKind.allCases.map(\.rawValue), [
            "ssh_private_key", "aws_credentials", "github_cli_token", "netrc", "docker_config", "kube_config"
        ])
    }
}
