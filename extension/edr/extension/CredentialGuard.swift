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
struct CredentialOwner: Equatable, Sendable {
    let signingID: String?
    let teamID: String?

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
struct ProtectedCredential: Sendable {
    let kind: CredentialKind
    /// homePath is the file below a home directory, without a leading "/". A prefix entry covers every name starting with it.
    let homePath: String
    let match: WatchedPathMatch
    let owners: [CredentialOwner]

    /// covers reports whether an opened path is this credential: the file itself, or for a prefix entry a name in its directory that
    /// starts with it and is not a public key, which `id_ed25519.pub` and `id_rsa-cert.pub` are.
    func covers(_ path: String) -> Bool {
        let needle = "/" + homePath
        switch match {
        case .literal:
            return path.hasSuffix(needle)
        case .prefix:
            guard let range = path.range(of: needle, options: .backwards) else {
                return false
            }
            let rest = path[range.upperBound...]
            return !rest.isEmpty && !rest.contains("/") && !rest.hasSuffix(".pub")
        }
    }
}

/// CredentialGuardMode mirrors application control's enforcement: detect reports what protect would deny, and protect denies it.
enum CredentialGuardMode: Sendable {
    case detect
    case protect
}

/// CredentialGuardDecision is the guard's verdict on one open.
enum CredentialGuardDecision: Equatable, Sendable {
    /// allow lets the open through and reports nothing: not a credential, not an agent's process, or the credential's own tool.
    case allow
    /// wouldBlock lets an agent's read of a credential through and reports it, which is detect mode.
    case wouldBlock(CredentialKind)
    /// block denies an agent's read of a credential and reports it, which is protect mode.
    case block(CredentialKind)

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

    /// targets is every path the guard mutes, and so observes once target-path muting is inverted: each credential in each home, in
    /// both spellings of a firmlinked root (root's home is under /var), in a stable order. The homes are distinct, as
    /// WatchedPaths.homeDirectories returns them, so the targets are too.
    static func targets(homes: [String]) -> [WatchedPath] {
        homes.flatMap { home in
            builtIn.flatMap { credential in
                WatchedPaths.spellings(of: home + "/" + credential.homePath).map { WatchedPath(path: $0, match: credential.match) }
            }
        }
    }

    /// credential is the protected credential an opened path is, or nil for any other file the muting delivers: a public key under
    /// the `id_` prefix, or a directory that happens to start with it.
    static func credential(at path: String) -> ProtectedCredential? {
        builtIn.first { $0.covers(path) }
    }

    /// decide is the verdict on a process opening path. The opener is in an agent session when the agent-process set holds it, or
    /// when it is the Claude Code binary itself, which covers a session started before the extension was, whose process the set never
    /// saw exec. Its own Read tool runs inside that process.
    static func decide(
        path: String, opener signing: CodeSigning?, inAgentProcessSet: Bool, mode: CredentialGuardMode
    ) -> CredentialGuardDecision {
        guard let credential = credential(at: path),
              inAgentProcessSet || isAgentRoot(codeSigning: signing, env: nil),
              !credential.owners.contains(where: { $0.matches(signing) }) else {
            return .allow
        }
        switch mode {
        case .detect:
            return .wouldBlock(credential.kind)
        case .protect:
            return .block(credential.kind)
        }
    }
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
