package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/rules/api"
)

const agentCredentialSession = "6662f20d-55c2-4a1e-9f3b-0c1d2e3f4a5b" //nolint:gosec // G101: an agent session id, not a credential.

// guardEvent is a credential guard report of pid opening path, the shape the extension sends. Each event gets its own ID.
func guardEvent(t *testing.T, pid int, path, kind string) api.Event {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"pid": pid, "pidversion": pid * 10, "path": path, "credential": kind, "flags": 0})
	require.NoError(t, err)
	return api.Event{EventID: fmt.Sprintf("%d:%s", pid, path), HostID: "fixture-host", TimestampNs: 1,
		EventType: "agent_credential_would_block", Payload: raw}
}

// generationReader answers the guard rule's lookup, by pid and the kernel generation its report carries.
type generationReader struct {
	perPIDGraphReader
	byGeneration map[[2]int64]*api.Process
}

func (r *generationReader) GetProcessByPIDVersion(_ context.Context, _ string, pid int, pidversion uint32, _ int64) (*api.Process, error) {
	return r.byGeneration[[2]int64{int64(pid), int64(pidversion)}], nil
}

// agentProcesses are a Claude Code session's processes, by pid and generation (pid*10, as guardEvent reports): two of its
// commands, which carry the session, and the agent itself, which does not. Pid 6991's earlier image, generation 69909, is the
// shell that execed cat, recorded with another session.
func agentProcesses() *generationReader {
	session := agentCredentialSession
	sig := func(team, signingID string, platform bool) api.NullRawJSON {
		raw, _ := json.Marshal(map[string]any{"team_id": team, "signing_id": signingID, "flags": 0, "is_platform_binary": platform})
		return api.NullRawJSON(raw)
	}
	cat := &api.Process{ID: 1, PID: 6991, Path: "/bin/cat", CodeSigning: sig("", "com.apple.cat", true)}
	gh := &api.Process{ID: 2, PID: 7200, Path: "/opt/homebrew/bin/gh", CodeSigning: sig("", "a.out", false)}
	cat.AgentSessionID, gh.AgentSessionID = &session, &session
	other := "another-session"
	shell := &api.Process{ID: 9, PID: 6991, Path: "/bin/zsh", CodeSigning: sig("", "com.apple.zsh", true)}
	shell.AgentSessionID = &other
	agent := &api.Process{ID: 3, PID: 6833, Path: "/Users/victor/.local/share/claude/versions/2.1.302", CodeSigning: sig("", "a.out", false)}
	return &generationReader{
		perPIDGraphReader: perPIDGraphReader{procByPID: map[int]*api.Process{6991: shell, 7200: gh, 6833: agent}},
		byGeneration: map[[2]int64]*api.Process{
			{6991, 69910}: cat, {6991, 69909}: shell, {7200, 72000}: gh, {6833, 68330}: agent,
		},
	}
}

func evaluateAgentCredentialRead(t *testing.T, excl api.ExclusionResolver, events ...api.Event) []api.Finding {
	t.Helper()
	findings, err := (&AgentCredentialRead{Exclusions: excl}).Evaluate(t.Context(), events, agentProcesses())
	require.NoError(t, err)
	return findings
}

// spec:server-detection-rules-engine/an-ai-agent-reading-credentials-is-reported/a-session-reading-a-credential-is-alerted-once-per-kind
func TestAgentCredentialRead_AlertsOncePerSessionAndKind(t *testing.T) {
	t.Parallel()
	findings := evaluateAgentCredentialRead(t, nil,
		guardEvent(t, 6991, "/Users/victor/.ssh/id_ed25519", "ssh_private_key"),
		guardEvent(t, 6991, "/Users/victor/.ssh/id_rsa", "ssh_private_key"),
		guardEvent(t, 7200, "/Users/victor/.config/gh/hosts.yml", "github_cli_token"),
		guardEvent(t, 6833, "/Users/victor/.ssh/id_ed25519", "ssh_private_key"),
	)
	require.Len(t, findings, 4, "each report is a finding; the alert store joins those of one subject")
	subjects := map[string]string{}
	for _, f := range findings {
		subjects[f.EventIDs[0]] = f.Subject
	}
	keySubject := "agentcredential:" + agentCredentialSession + ":ssh_private_key"
	assert.Equal(t, map[string]string{
		"6991:/Users/victor/.ssh/id_ed25519":      keySubject,
		"6991:/Users/victor/.ssh/id_rsa":          keySubject,
		"7200:/Users/victor/.config/gh/hosts.yml": "agentcredential:" + agentCredentialSession + ":github_cli_token",
		"6833:/Users/victor/.ssh/id_ed25519":      "",
	}, subjects, "a session's reads of one kind share a subject; the agent's own process, with no session, keeps its own")
	assert.Equal(t, "An AI agent's /bin/cat tried to read your SSH private key (/Users/victor/.ssh/id_ed25519)",
		findings[0].Description)
	assert.Equal(t, api.SeverityHigh, findings[0].Severity)
	assert.Equal(t, int64(1), findings[0].ProcessID)
}

func TestAgentCredentialRead_NamesEachCredentialInPlainWords(t *testing.T) {
	t.Parallel()
	for kind, words := range map[string]string{ //nolint:gosec // G101: kinds of credential and their names, not credentials.
		"ssh_private_key": "your SSH private key", "aws_credentials": "your AWS credentials",
		"github_cli_token": "your GitHub CLI token", "netrc": "the logins in your .netrc",
		"docker_config": "your Docker registry logins", "kube_config": "your Kubernetes credentials",
		"a_kind_from_a_newer_agent": "a credential file",
	} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			findings := evaluateAgentCredentialRead(t, nil, guardEvent(t, 6991, "/Users/victor/f", kind))
			require.Len(t, findings, 1)
			assert.Contains(t, findings[0].Description, "tried to read "+words+" (/Users/victor/f)")
		})
	}
}

func TestAgentCredentialRead_AnOpenerIsWaivedBySignerOrPath(t *testing.T) {
	t.Parallel()
	rule := "agent_credential_read"
	for _, tc := range []struct {
		name  string
		excl  fakeExcl
		fires bool
	}{
		{"by path", fakeExcl{ruleID: rule, matchType: api.ExclusionMatchPathGlob, value: "/opt/homebrew/bin/*"}, false},
		{"another rule's exclusion", fakeExcl{ruleID: "suspicious_exec", matchType: api.ExclusionMatchPathGlob,
			value: "/opt/homebrew/bin/*"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			findings := evaluateAgentCredentialRead(t, &fakeExclusions{entries: []fakeExcl{tc.excl}},
				guardEvent(t, 7200, "/Users/victor/.config/gh/hosts.yml", "github_cli_token"))
			assert.Equal(t, tc.fires, len(findings) == 1)
		})
	}
}

func TestAgentCredentialRead_IgnoresWhatIsNotAReport(t *testing.T) {
	t.Parallel()
	open := guardEvent(t, 6991, "/Users/victor/.ssh/id_ed25519", "ssh_private_key")
	open.EventType = "open"
	unknownProcess := guardEvent(t, 4242, "/Users/victor/.ssh/id_ed25519", "ssh_private_key")
	assert.Empty(t, evaluateAgentCredentialRead(t, nil, open, unknownProcess),
		"another event type, and a long-ingested report whose process the graph never recorded, raise nothing")
}

// The opener is the generation the report names, never the pid's image at the report's time: the report can arrive before the
// exec that started it is recorded, when a lookup by pid and time finds the shell that execed it.
//
// spec:server-detection-rules-engine/an-ai-agent-reading-credentials-is-reported/a-report-is-the-generation-it-names
func TestAgentCredentialRead_ResolvesTheGenerationTheReportNames(t *testing.T) {
	t.Parallel()
	findings := evaluateAgentCredentialRead(t, nil, guardEvent(t, 6991, "/Users/victor/.ssh/id_ed25519", "ssh_private_key"))
	require.Len(t, findings, 1)
	assert.Equal(t, int64(1), findings[0].ProcessID, "cat's generation, not the shell's")
	assert.Contains(t, findings[0].Subject, agentCredentialSession)

	early := guardEvent(t, 6991, "/Users/victor/.ssh/id_ed25519", "ssh_private_key")
	early.EventID = "early"
	early.Payload = json.RawMessage(`{"pid":6991,"pidversion":69911,"path":"/Users/victor/.ssh/id_ed25519","credential":"ssh_private_key"}`)
	early.IngestedAtNs = time.Now().UnixNano()
	_, err := (&AgentCredentialRead{}).Evaluate(t.Context(), []api.Event{early}, agentProcesses())
	assert.ErrorIs(t, err, api.ErrProcessNotYetMaterialized, "a generation not recorded yet is retried, not guessed")
}

// A session id at the 256-byte marker limit still makes a subject the alert column holds.
func TestAgentCredentialRead_ALongSessionIDStillFitsTheSubject(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("s", 256)
	reader := agentProcesses()
	cat, ok := reader.byGeneration[[2]int64{6991, 69910}]
	require.True(t, ok)
	cat.AgentSessionID = &long
	findings, err := (&AgentCredentialRead{}).Evaluate(t.Context(),
		[]api.Event{guardEvent(t, 6991, "/Users/victor/.ssh/id_ed25519", "ssh_private_key")}, reader)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.LessOrEqual(t, len(findings[0].Subject), subjectColumnLimit)
	assert.True(t, strings.HasPrefix(findings[0].Subject, "agentcredential:sha256:"))
}
