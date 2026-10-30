package catalog

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rawtrail/rawtrail/server/rules/api"
)

// AgentCredentialRead fires when an AI agent's process reads a credential file that is not its tool's (T1552.004, T1552.001): the
// step a prompt injection takes when a poisoned README, issue or tool description tells the agent to read `~/.ssh/id_ed25519` or
// `~/.aws/credentials` and send it out (issue #1432).
//
// The events are the security extension's credential guard's (issue #1431), which sees only opens of the guarded files, only by a
// process in an agent session, and drops an open by the credential's own tool (ssh reading a key for git push) before it is sent.
// The rule names the credential in plain words and raises one alert per session and kind of credential, however many processes of
// the session open it: the agent itself opens a file a command names before it runs the command.
type AgentCredentialRead struct {
	// Exclusions is the per-host false-positive resolver, consulted for the opener's path, team, team-qualified signing identifier
	// and cdhash. Nil excludes nothing.
	Exclusions api.ExclusionResolver
}

// agentCredentialWords is each credential kind the guard reports, by its wire name, as the alert says it.
//
//nolint:gosec // G101: kinds of credential and how an alert names them, not credentials.
var agentCredentialWords = map[string]string{
	"ssh_private_key":  "your SSH private key",
	"aws_credentials":  "your AWS credentials",
	"github_cli_token": "your GitHub CLI token",
	"netrc":            "the logins in your .netrc",
	"docker_config":    "your Docker registry logins",
	"kube_config":      "your Kubernetes credentials",
}

func (r *AgentCredentialRead) ID() string { return "agent_credential_read" }

// AlgorithmName names the evaluator that decides this rule, for the exported rule file.
func (r *AgentCredentialRead) AlgorithmName() string { return "agent_credential_guard_verdict" }

// SupportedExclusionMatchTypes lists what an operator can waive an opener by: its path, its team, its team-qualified signing
// identifier, or its cdhash, as for the browser credential rule.
func (r *AgentCredentialRead) SupportedExclusionMatchTypes() []api.ExclusionMatchType {
	return []api.ExclusionMatchType{
		api.ExclusionMatchPathGlob, api.ExclusionMatchTeamID, api.ExclusionMatchSigningID, api.ExclusionMatchCDHash,
	}
}

// DisplayName is the canonical human-readable name reused by Doc().Title and the finding (issue #519).
func (r *AgentCredentialRead) DisplayName() string { return "AI agent read a credential file" }

// Techniques returns T1552.004 (Unsecured Credentials: Private Keys) and T1552.001 (Unsecured Credentials: Credentials In Files).
func (r *AgentCredentialRead) Techniques() []string { return []string{"T1552.004", "T1552.001"} }

// Doc surfaces the operator-facing description in /api/rules and the generated docs/detection-rules.md.
func (r *AgentCredentialRead) Doc() api.Documentation {
	return api.Documentation{
		Title:   r.DisplayName(),
		Summary: "Flags an AI coding agent's process reading an SSH key, cloud or registry credentials it does not own.",
		Description: "Detects an AI coding agent session reading credential files (T1552.004, T1552.001), the step a prompt " +
			"injection takes before it sends them out: a poisoned README, issue, web page or tool description tells the agent to " +
			"read `~/.ssh/id_*` or `~/.aws/credentials`, and the agent's own deny rules do not stop a shell command.\n\n" +
			"The security extension's credential guard watches SSH private keys, `~/.aws/credentials`, the GitHub CLI's " +
			"`hosts.yml`, `~/.netrc`, `~/.docker/config.json` and `~/.kube/config`, and reports an open only by a process in an " +
			"agent session that is not the credential's own tool, judged by its signature: `ssh` reading a key for `git push` is " +
			"not reported. One alert is raised per session and kind of credential.",
		Severity:   api.SeverityHigh,
		EventTypes: []string{"agent_credential_would_block"},
		FalsePositives: []string{
			"A person asking their agent to work on their SSH or cloud configuration. Confirm with them; the session's prompts say " +
				"what was asked.",
			"A credential tool installed without a vendor signature, such as Homebrew's `gh` or `kubectl`, run by the agent. " +
				"Exclude it by `cdhash` or `path_glob`.",
		},
		Limitations: []string{
			"Only Claude Code sessions are recognized, by the markers it sets in the environment of the commands it runs.",
			"Detect mode only: the read is reported, not prevented.",
			"The agent's own process carries no session marker, so its reads are alerted apart from its commands'.",
			"Needs an agent whose extension runs the credential guard.",
		},
	}
}

func (r *AgentCredentialRead) Evaluate(ctx context.Context, events []api.Event, s api.GraphReader) ([]api.Finding, error) {
	return r.EvaluateScoped(ctx, &api.BatchScope{}, events, s)
}

// CountsMaterializationAbandons declares that every abandon this rule makes is recorded, so its zero is a measurement.
func (r *AgentCredentialRead) CountsMaterializationAbandons() {
	// A marker: the method's existence is the declaration (api.AbandonCounter), and evalEvent records each abandon itself.
}

// EvaluateScoped implements api.ScopedRule.
func (r *AgentCredentialRead) EvaluateScoped(
	ctx context.Context, scope *api.BatchScope, events []api.Event, s api.GraphReader,
) ([]api.Finding, error) {
	return evalEachScopedEvent(ctx, scope, events, s, r.evalEvent)
}

func (r *AgentCredentialRead) evalEvent(
	ctx context.Context, scope *api.BatchScope, evt api.Event, s api.GraphReader,
) (*api.Finding, error) {
	if evt.EventType != "agent_credential_would_block" {
		return nil, nil
	}
	var p struct {
		PID        int    `json:"pid"`
		Path       string `json:"path"`
		Credential string `json:"credential"`
	}
	if err := json.Unmarshal(evt.Payload, &p); err != nil || p.PID <= 0 {
		return nil, nil
	}
	// A kind this server does not name yet, from a newer agent, is still an agent reading a credential.
	words, ok := agentCredentialWords[p.Credential]
	if !ok {
		words = "a credential file"
	}
	proc, err := resolveSubjectProcess(ctx, s, evt, p.PID)
	if err != nil {
		return nil, err
	}
	if proc == nil {
		scope.RecordMaterializationAbandoned(r.ID(), p.PID)
		return nil, nil
	}
	if r.Exclusions != nil && processExcluded(r.Exclusions, r.ID(), api.ExclusionMatchPathGlob, proc, evt.HostID) {
		return nil, nil
	}
	finding := &api.Finding{
		HostID:      evt.HostID,
		RuleID:      r.ID(),
		Severity:    api.SeverityHigh,
		Title:       r.DisplayName(),
		Description: fmt.Sprintf("An AI agent's %s tried to read %s (%s)", proc.Path, words, p.Path),
		ProcessID:   proc.ID,
		EventIDs:    []string{evt.EventID},
	}
	// One alert per session and kind of credential: the session's later reads of it join the first. A process the graph holds no
	// session for (the agent's own) is alerted on its own, by the default per-process subject.
	if id := proc.AgentSessionID; id != nil && *id != "" {
		finding.Subject = "agentcredential:" + *id + ":" + p.Credential
	}
	return finding, nil
}
