package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// CommandTypeSetCredentialGuard is the command that carries the credential guard's policy to a host (issue #1432): its mode and the
// credential files it protects. The agent forwards it to the security extension, which applies it. Stable wire string; renaming it
// breaks every deployed agent.
const CommandTypeSetCredentialGuard = "set_credential_guard" //nolint:gosec // G101: a command type name, not a credential.

// Credential guard modes. Detect reports an AI agent's read of a credential file that is not its tool's, protect denies it, and off
// watches nothing.
const (
	CredentialGuardDetect  = "detect"
	CredentialGuardProtect = "protect"
	CredentialGuardOff     = "off"
)

// CredentialKinds are the kinds of credential the guard protects, as the extension and the agent_credential_read rule spell them.
var CredentialKinds = []string{"ssh_private_key", "aws_credentials", "github_cli_token", "netrc", "docker_config", "kube_config"}

// CredentialOwner is a tool a credential belongs to, by signing identity: with no team, an Apple platform binary of SigningID;
// with a team, that Developer ID team, and when SigningID is given as well, that one binary of the team's. A path or an ad hoc
// signature can be anything the agent wrote, so neither makes an owner.
type CredentialOwner struct {
	TeamID    string `json:"team_id,omitempty"`
	SigningID string `json:"signing_id,omitempty"`
}

// ProtectedCredential is one credential file the guard watches in every home, and the tools allowed to read it. Path is the file
// below a home, without a leading "/"; with Match "prefix" it covers every file in its directory whose name starts with it, a
// public key (".pub") excepted. Default marks the built-in entries the server seeds, which an operator can edit or remove.
type ProtectedCredential struct {
	Kind    string            `json:"kind"`
	Path    string            `json:"path"`
	Match   string            `json:"match"`
	Owners  []CredentialOwner `json:"owners"`
	Default bool              `json:"default,omitempty"`
}

// CredentialGuardPolicy is the credential guard's fleet-wide policy (issue #1432).
type CredentialGuardPolicy struct {
	// Version starts at 0, the policy no operator has changed, and advances by one on every change.
	Version     int64                 `json:"version"`
	Mode        string                `json:"mode"`
	Credentials []ProtectedCredential `json:"credentials"`
	// UpdatedAt and UpdatedBy are zero until the first change; UpdatedByLabel is resolved at read time and never stored.
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
	UpdatedBy      string     `json:"updated_by,omitempty"`
	UpdatedByLabel string     `json:"updated_by_label,omitempty"`
}

// SetCredentialGuardPayload is the set_credential_guard payload. Commands can arrive out of order, so the extension applies one only
// when its (Epoch, Version) is ahead of the last it applied; Epoch is the policy's update time in microseconds, 0 with Version 0.
type SetCredentialGuardPayload struct {
	Version     int64                 `json:"version"`
	Epoch       int64                 `json:"epoch"`
	Mode        string                `json:"mode"`
	Credentials []ProtectedCredential `json:"credentials"`
}

// sshKeyOwners are OpenSSH's own tools, which git, scp and rsync reach keys through.
var sshKeyOwners = []CredentialOwner{
	{SigningID: "com.apple.ssh"}, {SigningID: "com.apple.ssh-agent"}, {SigningID: "com.apple.ssh-add"}, {SigningID: "com.apple.ssh-keygen"},
}

// dockerTeam signs the Docker CLI, its credential helpers and the kubectl Docker Desktop installs.
const dockerTeam = "9BNSXJN65R"

// DefaultCredentialGuard is the policy before any change: detect mode over the credentials the extension protects built in, each
// read on a macOS 26 host from its tool as the vendor ships it. It mirrors the extension's CredentialGuard.builtIn.
func DefaultCredentialGuard() CredentialGuardPolicy {
	return CredentialGuardPolicy{Mode: CredentialGuardDetect, Credentials: []ProtectedCredential{
		{Kind: "ssh_private_key", Path: ".ssh/id_", Match: "prefix", Owners: slices.Clone(sshKeyOwners), Default: true},
		{Kind: "aws_credentials", Path: ".aws/credentials", Match: "literal", Owners: []CredentialOwner{{TeamID: "94KV3E626L"}}, Default: true},
		{Kind: "github_cli_token", Path: ".config/gh/hosts.yml", Match: "literal", Owners: []CredentialOwner{{TeamID: "VEKTX9H2N7"}},
			Default: true},
		{Kind: "netrc", Path: ".netrc", Match: "literal", Owners: []CredentialOwner{
			{SigningID: "com.apple.curl"}, {TeamID: "59GAB85EFG", SigningID: "com.apple.git-remote-http"},
		}, Default: true},
		{Kind: "docker_config", Path: ".docker/config.json", Match: "literal", Owners: []CredentialOwner{{TeamID: dockerTeam}}, Default: true},
		{Kind: "kube_config", Path: ".kube/config", Match: "literal", Owners: []CredentialOwner{{TeamID: dockerTeam}}, Default: true},
	}}
}

// Bounds on a policy, so one fits a command row and the extension's mute set stays small.
const (
	maxProtectedCredentials = 64
	maxCredentialOwners     = 32
	maxCredentialPathBytes  = 512
	maxSigningIDBytes       = 255
)

// MaxCredentialGuardBytes bounds the credentials as the server encodes them, which is what a host is sent. The fan-out repeats the
// payload on every row of a batched insert of up to 256 hosts, so this keeps one statement near 2 MiB, inside the 4 MiB
// max_allowed_packet the server is designed to work under, as MaxWatchedPathSetBytes does. The built-in list is about 1 KiB.
const MaxCredentialGuardBytes = 8 * 1024

// teamIDPattern is an Apple Developer team identifier.
var teamIDPattern = regexp.MustCompile(`^[A-Z0-9]{10}$`)

// ValidateCredentialGuard checks a policy an operator submitted: a known mode, and credentials of known kinds at relative paths below
// a home that stay below it, each owner named by a team identifier, a signing identifier, or both. It returns the first problem.
func ValidateCredentialGuard(mode string, credentials []ProtectedCredential) error {
	if !slices.Contains([]string{CredentialGuardDetect, CredentialGuardProtect, CredentialGuardOff}, mode) {
		return fmt.Errorf("mode must be %q, %q or %q", CredentialGuardDetect, CredentialGuardProtect, CredentialGuardOff)
	}
	if len(credentials) > maxProtectedCredentials {
		return fmt.Errorf("at most %d credentials", maxProtectedCredentials)
	}
	// Encoding a slice of plain structs cannot fail.
	if encoded, _ := json.Marshal(credentials); len(encoded) > MaxCredentialGuardBytes {
		return fmt.Errorf("the credentials encode to %d bytes, at most %d", len(encoded), MaxCredentialGuardBytes)
	}
	seen := map[string]bool{}
	for i, c := range credentials {
		if err := validateProtectedCredential(c); err != nil {
			return fmt.Errorf("credentials[%d]: %w", i, err)
		}
		if seen[c.Path] {
			return fmt.Errorf("credentials[%d]: %q is listed twice", i, c.Path)
		}
		seen[c.Path] = true
	}
	return nil
}

func validateProtectedCredential(c ProtectedCredential) error {
	if !slices.Contains(CredentialKinds, c.Kind) {
		return fmt.Errorf("kind must be one of %s", strings.Join(CredentialKinds, ", "))
	}
	if c.Match != "literal" && c.Match != "prefix" {
		return errors.New(`match must be "literal" or "prefix"`)
	}
	if err := validateHomePath(c.Path); err != nil {
		return err
	}
	if c.Match == "prefix" && !strings.Contains(c.Path, "/") {
		// Endpoint Security delivers every open under a muted prefix, at any depth: ".c" would send all of ~/.config and ~/.cache.
		return errors.New("a prefix must lie in a directory below the home, as .ssh/id_ does")
	}
	if len(c.Owners) > maxCredentialOwners {
		return fmt.Errorf("at most %d owners", maxCredentialOwners)
	}
	for j, o := range c.Owners {
		switch {
		case o.TeamID == "" && o.SigningID == "":
			return fmt.Errorf("owners[%d]: a team_id, a signing_id or both is required", j)
		case o.TeamID != "" && !teamIDPattern.MatchString(o.TeamID):
			return fmt.Errorf("owners[%d]: team_id must be ten capital letters and digits", j)
		case len(o.SigningID) > maxSigningIDBytes:
			return fmt.Errorf("owners[%d]: signing_id is longer than %d bytes", j, maxSigningIDBytes)
		case hasControlCharacter(o.SigningID):
			return fmt.Errorf("owners[%d]: signing_id must not hold control characters", j)
		}
	}
	return nil
}

// validateHomePath checks a path below a home: relative, without "." or ".." segments or empty ones, so it cannot leave the home.
func validateHomePath(path string) error {
	if path == "" || len(path) > maxCredentialPathBytes {
		return fmt.Errorf("path must be 1 to %d bytes", maxCredentialPathBytes)
	}
	if hasControlCharacter(path) {
		// A NUL would cut the path the extension mutes short, and no file name holds a control character a person typed.
		return errors.New("path must not hold control characters")
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "~") {
		return errors.New("path is below a home: write .aws/credentials, not ~/.aws/credentials")
	}
	for segment := range strings.SplitSeq(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return errors.New("path must not hold empty, . or .. segments")
		}
	}
	return nil
}

// hasControlCharacter reports whether s holds an ASCII control character, NUL included, which would truncate the C string the
// extension hands the kernel.
func hasControlCharacter(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f })
}
