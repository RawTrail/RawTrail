import Foundation

/// The credential guard (issue #1431): the pure half of CredentialGuardSubscriber. Which files hold a credential, which tools each
/// one belongs to, and the decision for an AI agent's process opening one.
///
/// The rule is that inside an agent session a credential file is read only by the tool it belongs to: `git push` runs `ssh`, which
/// reads the key, and stays quiet, while `cat ~/.ssh/id_ed25519`, a Python `open()` or the agent's own Read tool is what a prompt
/// injection makes an agent do. Nothing outside an agent session is ever decided against.
///
/// Pure Foundation, with no EndpointSecurity import, so the decision is unit-testable; CredentialGuardSubscriber applies it.

/// CredentialKind is what a protected file holds, as it is spelled on the wire.
enum CredentialKind: String, Codable, Sendable, CaseIterable {
    case sshPrivateKey = "ssh_private_key"
    case awsCredentials = "aws_credentials"
    case githubCLIToken = "github_cli_token"
    case netrc
    case dockerConfig = "docker_config"
    case kubeConfig = "kube_config"
}

/// CredentialOwner is a tool a credential belongs to, matched by signing identity. With no team it is an Apple platform binary of
/// that signing identifier, which only Apple can sign on a host with SIP on. With a team it is that Developer ID team, and when a
/// signing identifier is given as well, that one binary of the team's.
///
/// Signing identity is the only identity that holds: a path or a name is whatever the agent wrote, and an ad-hoc signature can
/// carry any identifier. So an ad-hoc build of an owner tool, as Homebrew ships `gh` (`Identifier=a.out`), is not an owner.
struct CredentialOwner: Equatable, Sendable, Decodable {
    let signingID: String?
    let teamID: String?

    enum CodingKeys: String, CodingKey {
        case signingID = "signing_id"
        case teamID = "team_id"
    }

    static func platform(_ signingID: String) -> CredentialOwner {
        CredentialOwner(signingID: signingID, teamID: nil)
    }

    static func team(_ teamID: String, signingID: String? = nil) -> CredentialOwner {
        CredentialOwner(signingID: signingID, teamID: teamID)
    }

    func matches(_ signing: CodeSigning?) -> Bool {
        guard let signing else {
            return false
        }
        guard let teamID else {
            return signing.isPlatformBinary && signing.signingID == signingID
        }
        return signing.teamID == teamID && (signingID == nil || signing.signingID == signingID)
    }
}

/// ProtectedCredential is one credential file the guard watches in every home, and the tools allowed to read it.
struct ProtectedCredential: Equatable, Sendable {
    let kind: CredentialKind
    /// homePath is the file below a home directory, without a leading "/". A prefix entry covers every name in its directory starting
    /// with it, the name itself included.
    let homePath: String
    let match: WatchedPathMatch
    let owners: [CredentialOwner]
}

/// CredentialGuardMode mirrors application control's enforcement: detect reports what protect would deny, protect denies it, and off
/// watches nothing. The raw values are the server's spelling.
enum CredentialGuardMode: String, Sendable {
    case detect
    case protect
    case off
}

/// CredentialGuardPolicy is what the guard enforces: its mode and the credentials it protects. The server pushes it (issue #1432);
/// until it does, builtIn is in force.
struct CredentialGuardPolicy: Equatable, Sendable {
    let mode: CredentialGuardMode
    let credentials: [ProtectedCredential]

    static let builtIn = CredentialGuardPolicy(mode: .detect, credentials: CredentialGuard.builtIn)

    /// targets is every path the guard mutes, and so observes once target-path muting is inverted, with the credential each guards:
    /// each credential in each home, in both spellings of a firmlinked root (root's home is under /var), in a stable order, and none when
    /// the guard is off. The homes are distinct, as WatchedPaths.homeDirectories returns them, so the targets are too. A spelling
    /// es_mute_path could not take, which only a very long home makes of a path the decode accepted, is left out.
    func targets(homes: [String]) -> [CredentialTarget] {
        guard mode != .off else {
            return []
        }
        var out: [CredentialTarget] = []
        for home in homes {
            for credential in credentials {
                for spelling in WatchedPaths.spellings(of: home + "/" + credential.homePath)
                where WatchedPaths.isAcceptable(spelling, .literal) {
                    out.append(CredentialTarget(watched: WatchedPath(path: spelling, match: credential.match), credential: credential))
                }
            }
        }
        return out
    }

    /// enforcement is the policy as it is enforced over homes: its mode, and every target muted.
    func enforcement(homes: [String]) -> CredentialGuardEnforcement {
        CredentialGuardEnforcement(mode: mode, targets: targets(homes: homes))
    }
}

/// CredentialTarget is one path the guard mutes and the credential it guards.
struct CredentialTarget: Equatable, Sendable {
    let watched: WatchedPath
    let credential: ProtectedCredential

    /// covers reports whether an opened path is this target's: the file itself, or for a prefix a name in its directory that starts
    /// with it and is not a public key, which `id_ed25519.pub` and `id_rsa-cert.pub` are. A directory that happens to start with it
    /// is not covered.
    func covers(_ path: String) -> Bool {
        switch watched.match {
        case .literal:
            return path == watched.path
        case .prefix:
            guard path.hasPrefix(watched.path) else {
                return false
            }
            let rest = path.dropFirst(watched.path.count)
            return !rest.contains("/") && !rest.hasSuffix(".pub")
        }
    }
}

/// CredentialGuardEnforcement is what the guard decides an open by: the mode, and the targets that are muted and so reach it. It is
/// published only once its targets are muted, so a decision never names a credential whose opens are not yet observed.
struct CredentialGuardEnforcement: Sendable {
    let mode: CredentialGuardMode
    let targets: [CredentialTarget]

    /// none is the enforcement before the first refresh, when no open reaches the guard.
    static let none = CredentialGuardEnforcement(mode: .off, targets: [])

    /// credential is the protected credential an opened path is, or nil for any other file the muting delivers. Where two targets
    /// cover the path, as a prefix and a file under it, or `.netrc` and a `.netrc` in a subdirectory, the most specific decides: a
    /// literal file before a prefix, and a longer path before a shorter one.
    func credential(at path: String) -> ProtectedCredential? {
        targets.filter { $0.covers(path) }.max { lhs, rhs in
            (lhs.watched.match == .literal ? 1 : 0, lhs.watched.path.count)
                < (rhs.watched.match == .literal ? 1 : 0, rhs.watched.path.count)
        }?.credential
    }

    /// decide is the verdict on a process opening path. The opener is in an agent session when the agent-process set holds it, or
    /// when it is the Claude Code binary itself, which covers a session started before the extension was, whose process the set never
    /// saw exec. Its own Read tool runs inside that process.
    func decide(path: String, opener signing: CodeSigning?, inAgentProcessSet: Bool) -> CredentialGuardDecision {
        guard mode != .off, let credential = credential(at: path),
              inAgentProcessSet || isAgentRoot(codeSigning: signing, env: nil),
              !credential.owners.contains(where: { $0.matches(signing) }) else {
            return .allow
        }
        return mode == .protect ? .block(credential.kind) : .wouldBlock(credential.kind)
    }
}

/// CredentialGuardUpdate is a decoded `credential_guard.update`: the server's version and epoch for the policy, the policy this
/// extension applies, and how many credentials it skipped.
struct CredentialGuardUpdate: Equatable, Sendable {
    let version: Int64
    /// epoch is the policy's server update time in Unix microseconds, 0 for the policy no operator has changed.
    let epoch: Int64
    let policy: CredentialGuardPolicy
    let skipped: Int

    private struct Document: Decodable {
        let version: Int64
        let epoch: Int64?
        let mode: String
        let credentials: [Entry]
    }

    private struct Entry: Decodable {
        let kind: String
        let path: String
        let match: String
        let owners: [CredentialOwner]?
    }

    /// decode reads a `credential_guard.update` payload. It returns nil, leaving the policy in force, for a payload that is not a policy
    /// document or names a mode this extension does not know: guessing at a newer mode could deny what the operator meant to allow, or
    /// the reverse. Within a valid document, a credential of a kind or match this extension does not know, or at a path it would not
    /// mute, is skipped and counted, so an older extension keeps guarding what it understands. An owner with neither a team nor a
    /// signing identifier would match nothing, and is dropped.
    static func decode(_ data: Data) -> CredentialGuardUpdate? {
        guard let document = try? JSONDecoder().decode(Document.self, from: data),
              let mode = CredentialGuardMode(rawValue: document.mode) else {
            return nil
        }
        var credentials: [ProtectedCredential] = []
        for entry in document.credentials {
            guard let kind = CredentialKind(rawValue: entry.kind), let match = WatchedPathMatch(rawValue: entry.match),
                  isAcceptable(homePath: entry.path) else {
                continue
            }
            let owners = (entry.owners ?? []).filter { $0.teamID != nil || $0.signingID != nil }
            credentials.append(ProtectedCredential(kind: kind, homePath: entry.path, match: match, owners: owners))
        }
        return CredentialGuardUpdate(
            version: document.version, epoch: document.epoch ?? 0, policy: CredentialGuardPolicy(mode: mode, credentials: credentials),
            skipped: document.credentials.count - credentials.count
        )
    }

    /// isAcceptable is the server's rule for a path below a home (validateHomePath in server/rules/api), held again here for the reason
    /// WatchedPaths.isAcceptable gives: relative, and judged as the file it names in a home at the top of the filesystem, so it has no
    /// empty, `.` or `..` segment, no control character, and does not end in "/". A prefix credential names the start of a file name,
    /// not a directory, so it is judged the same way.
    static func isAcceptable(homePath: String) -> Bool {
        !homePath.hasPrefix("/") && WatchedPaths.isAcceptable(WatchedPaths.homePrefix + homePath, .literal)
    }

    /// supersedes reports whether this update should replace `current`, the last one accepted, in PushOrder, as a watched-path set
    /// does (WatchedPathsUpdate.supersedes).
    func supersedes(_ current: CredentialGuardUpdate?) -> Bool {
        guard let current else { return true }
        return PushOrder(epoch: epoch, version: version) > PushOrder(epoch: current.epoch, version: current.version)
    }
}

/// CredentialGuardDecision is the guard's verdict on one open.
enum CredentialGuardDecision: Equatable, Sendable {
    /// allow lets the open through and reports nothing: not a credential, not an agent's process, or the credential's own tool.
    case allow
    /// wouldBlock lets an agent's read of a credential through and reports it, which is detect mode.
    case wouldBlock(CredentialKind)
    /// block denies an agent's read of a credential and reports it, which is protect mode.
    case block(CredentialKind)

    /// report is the event the decision is reported as, and the credential it names, or nil for an open that is not reported. denied
    /// is whether the kernel took the refusal: a block it did not take is reported as a read that may have gone through, so an alert
    /// never says the agent was stopped when it might not have been.
    func report(denied: Bool) -> (eventType: String, credential: CredentialKind)? {
        switch self {
        case .allow:
            return nil
        case let .wouldBlock(kind):
            return ("agent_credential_would_block", kind)
        case let .block(kind):
            return (denied ? "agent_credential_block" : "agent_credential_would_block", kind)
        }
    }

    /// deniesOpen is whether the kernel is told to refuse the open.
    var deniesOpen: Bool {
        if case .block = self {
            return true
        }
        return false
    }
}

enum CredentialGuard {
    /// sshKeyOwners are OpenSSH's own tools, which `git`, `scp` and `rsync` reach keys through.
    static let sshKeyOwners: [CredentialOwner] = [
        .platform("com.apple.ssh"), .platform("com.apple.ssh-agent"), .platform("com.apple.ssh-add"), .platform("com.apple.ssh-keygen")
    ]

    /// dockerTeam signs the Docker CLI, its credential helpers and the `kubectl` Docker Desktop installs.
    static let dockerTeam = "9BNSXJN65R"

    /// builtIn is every credential the guard watches, read on a macOS 26 host from each tool as its vendor ships it: the AWS CLI
    /// installer, GitHub's release of `gh`, Docker Desktop, and Apple's `curl` and Xcode `git`, whose HTTP transport reads `.netrc`.
    static let builtIn: [ProtectedCredential] = [
        ProtectedCredential(kind: .sshPrivateKey, homePath: ".ssh/id_", match: .prefix, owners: sshKeyOwners),
        ProtectedCredential(kind: .awsCredentials, homePath: ".aws/credentials", match: .literal, owners: [.team("94KV3E626L")]),
        ProtectedCredential(kind: .githubCLIToken, homePath: ".config/gh/hosts.yml", match: .literal, owners: [.team("VEKTX9H2N7")]),
        ProtectedCredential(
            kind: .netrc, homePath: ".netrc", match: .literal,
            owners: [.platform("com.apple.curl"), .team("59GAB85EFG", signingID: "com.apple.git-remote-http")]
        ),
        ProtectedCredential(kind: .dockerConfig, homePath: ".docker/config.json", match: .literal, owners: [.team(dockerTeam)]),
        ProtectedCredential(kind: .kubeConfig, homePath: ".kube/config", match: .literal, owners: [.team(dockerTeam)])
    ]
}

/// AgentCredentialAccessPayload is the wire shape of an `agent_credential_would_block` event: an AI agent's process opened a
/// credential file that is not its tool's. The process is named by its generation, which joins it to the exec row carrying its path
/// and signing.
struct AgentCredentialAccessPayload: Codable, Sendable, Equatable {
    let pid: pid_t
    let pidVersion: UInt32
    let path: String
    let credential: CredentialKind
    /// flags is the open's access mode as open(2) spells it: O_RDONLY, O_WRONLY or O_RDWR.
    let flags: Int

    enum CodingKeys: String, CodingKey {
        case pid, path, credential, flags
        case pidVersion = "pidversion"
    }
}
