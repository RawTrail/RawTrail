//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/catchup"
	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
	rulesapi "github.com/rawtrail/rawtrail/server/rules/api"
	rulesbootstrap "github.com/rawtrail/rawtrail/server/rules/bootstrap"
	"github.com/rawtrail/rawtrail/server/rules/internal/credentialguard"
)

const credentialGuardRoute = "/api/v1/detection-config/credential-guard"

type replaceCredentialGuardBody struct {
	Policy       rulesapi.CredentialGuardPolicy `json:"policy"`
	FanoutHosts  int                            `json:"fanout_hosts"`
	FanoutFailed int                            `json:"fanout_failed"`
}

func (r *appControlRig) credentialGuard(t *testing.T) rulesapi.CredentialGuardPolicy {
	t.Helper()
	resp := r.do(t, http.MethodGet, credentialGuardRoute, nil)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body rulesapi.CredentialGuardPolicy
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return body
}

func (r *appControlRig) putCredentialGuard(t *testing.T, body map[string]any) *http.Response {
	t.Helper()
	return r.do(t, http.MethodPut, credentialGuardRoute, body)
}

// workKeys is an operator's edit of the default policy: protect mode, a second SSH key prefix, and Homebrew's signed gh as an
// owner of the GitHub CLI token beside GitHub's own.
func workKeys() map[string]any {
	return map[string]any{
		"mode": "protect",
		"credentials": []map[string]any{
			{"kind": "ssh_private_key", "path": ".ssh/id_", "match": "prefix", "owners": []map[string]any{{"signing_id": "com.apple.ssh"}}},
			{"kind": "ssh_private_key", "path": ".ssh/work_", "match": "prefix", "owners": []map[string]any{{"signing_id": "com.apple.ssh"}}},
			{"kind": "github_cli_token", "path": ".config/gh/hosts.yml", "match": "literal",
				"owners": []map[string]any{{"team_id": "VEKTX9H2N7"}, {"team_id": "ABCDE12345", "signing_id": "gh"}}},
		},
		"reason": "work keys and our signed gh",
	}
}

// withoutGuardAuthor drops who changed the policy and when, which a test compares separately.
func withoutGuardAuthor(p rulesapi.CredentialGuardPolicy) rulesapi.CredentialGuardPolicy {
	p.UpdatedAt, p.UpdatedBy, p.UpdatedByLabel = nil, "", ""
	return p
}

// spec:server-admin-surface/the-credential-guard-is-configured-over-the-api/the-guard-detects-over-its-built-in-credentials-until-changed
func TestCredentialGuardREST_DetectsOverTheBuiltInsUntilChanged(t *testing.T) {
	t.Parallel()
	r := newAppControlRig(t, []string{"host-a"})
	assert.Equal(t, rulesapi.DefaultCredentialGuard(), r.credentialGuard(t))
}

// spec:server-admin-surface/the-credential-guard-is-configured-over-the-api/a-change-is-stored-pushed-and-audited
func TestCredentialGuardREST_StoresPushesAndAuditsAChange(t *testing.T) {
	t.Parallel()
	hosts := []string{"host-c", "host-a", "host-b"}
	r := newAppControlRig(t, hosts)

	body := workKeys()
	body["expected_version"] = 0
	resp := r.putCredentialGuard(t, body)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var result replaceCredentialGuardBody
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, int64(1), result.Policy.Version)
	assert.Equal(t, "protect", result.Policy.Mode)
	assert.Equal(t, 3, result.FanoutHosts)

	stored := r.credentialGuard(t)
	require.Len(t, stored.Credentials, 3)
	assert.Equal(t, ".ssh/work_", stored.Credentials[1].Path)
	assert.False(t, stored.Credentials[1].Default, "an operator's entry is not a built-in")
	require.NotNil(t, stored.UpdatedAt)

	// Written out rather than built from the payload type, so the wire shape the agent forwards is what this pins.
	wantPayload := fmt.Sprintf(`{"version":1,"epoch":%d,"mode":"protect","credentials":[`+
		`{"kind":"ssh_private_key","path":".ssh/id_","match":"prefix","owners":[{"signing_id":"com.apple.ssh"}]},`+
		`{"kind":"ssh_private_key","path":".ssh/work_","match":"prefix","owners":[{"signing_id":"com.apple.ssh"}]},`+
		`{"kind":"github_cli_token","path":".config/gh/hosts.yml","match":"literal",`+
		`"owners":[{"team_id":"VEKTX9H2N7"},{"team_id":"ABCDE12345","signing_id":"gh"}]}]}`, stored.UpdatedAt.UnixMicro())
	commands := r.inserter.snapshot()
	require.Len(t, commands, 3)
	for _, c := range commands {
		assert.Equal(t, rulesapi.CommandTypeSetCredentialGuard, c.Type)
		assert.JSONEq(t, wantPayload, string(c.Payload))
	}

	e := r.auditRows(t, 1)[0]
	assert.Equal(t, identityapi.AuditDetectionConfigCredentialGuardUpdate, e.Action)
	assert.Equal(t, "credential_guard", e.TargetType)
	assert.Equal(t, "1", e.TargetID)
	assert.Equal(t, "work keys and our signed gh", e.Payload["reason"])
	assert.Equal(t, "protect", e.Payload["mode"])
	assert.Equal(t, "detect", e.Payload["previous_mode"])
	assert.Len(t, e.Payload["credentials"], 3)
	assert.Len(t, e.Payload["previous_credentials"], 6, "the built-ins it replaced")
	assert.EqualValues(t, 3, e.Payload["fanout_hosts"])

	// Off, protecting nothing, is a change like any other.
	off := r.putCredentialGuard(t, map[string]any{"mode": "off", "credentials": []any{}, "reason": "pause the guard"})
	off.Body.Close()
	require.Equal(t, http.StatusOK, off.StatusCode)
	assert.Equal(t, rulesapi.CredentialGuardPolicy{Version: 2, Mode: "off", Credentials: []rulesapi.ProtectedCredential{}},
		withoutGuardAuthor(r.credentialGuard(t)))
}

// spec:server-admin-surface/the-credential-guard-is-configured-over-the-api/an-invalid-change-is-refused
func TestCredentialGuardREST_RefusesAnInvalidChange(t *testing.T) {
	t.Parallel()
	r := newAppControlRig(t, []string{"host-a"})
	valid := func(edit func(map[string]any)) map[string]any {
		b := workKeys()
		edit(b)
		return b
	}
	cases := []struct {
		name string
		body map[string]any
	}{
		// A misspelled field must not read as an empty list and stop the guard protecting anything.
		{"no credentials", valid(func(b map[string]any) { delete(b, "credentials") })},
		{"blank reason", valid(func(b map[string]any) { b["reason"] = " " })},
		{"unknown mode", valid(func(b map[string]any) { b["mode"] = "block" })},
		{"a path that leaves the home", valid(func(b map[string]any) {
			b["credentials"] = []map[string]any{{"kind": "netrc", "path": "../../etc/passwd", "match": "literal", "owners": []any{}}}
		})},
		{"an absolute path", valid(func(b map[string]any) {
			b["credentials"] = []map[string]any{{"kind": "netrc", "path": "/Users/a/.netrc", "match": "literal", "owners": []any{}}}
		})},
		{"an unknown kind", valid(func(b map[string]any) {
			b["credentials"] = []map[string]any{{"kind": "browser_cookies", "path": ".cookies", "match": "literal", "owners": []any{}}}
		})},
		{"an owner with no identity", valid(func(b map[string]any) {
			b["credentials"] = []map[string]any{{"kind": "netrc", "path": ".netrc", "match": "literal", "owners": []map[string]any{{}}}}
		})},
		{"a malformed team", valid(func(b map[string]any) {
			b["credentials"] = []map[string]any{{"kind": "netrc", "path": ".netrc", "match": "literal",
				"owners": []map[string]any{{"team_id": "short"}}}}
		})},
		{"a path listed twice", valid(func(b map[string]any) {
			entry := map[string]any{"kind": "netrc", "path": ".netrc", "match": "literal", "owners": []any{}}
			b["credentials"] = []map[string]any{entry, entry}
		})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := r.putCredentialGuard(t, tc.body)
			resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	}
	assert.Equal(t, rulesapi.DefaultCredentialGuard(), r.credentialGuard(t))
	assert.Empty(t, r.inserter.snapshot())
}

// spec:server-admin-surface/the-credential-guard-is-configured-over-the-api/a-change-to-a-policy-that-moved-on-is-refused
func TestCredentialGuardREST_RefusesAChangeToAPolicyThatMovedOn(t *testing.T) {
	t.Parallel()
	r := newAppControlRig(t, []string{"host-a"})
	first := r.putCredentialGuard(t, workKeys())
	first.Body.Close()
	require.Equal(t, http.StatusOK, first.StatusCode)

	stale := workKeys()
	stale["mode"], stale["expected_version"] = "off", 0
	resp := r.putCredentialGuard(t, stale)
	resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, "protect", r.credentialGuard(t).Mode)
	assert.Len(t, r.inserter.snapshot(), 1, "the refused change queued nothing")
}

// spec:server-admin-surface/hosts-that-miss-the-credential-guard-push-get-it/a-host-is-sent-the-policy-it-missed
func TestCredentialGuardConverge_QueuesThePolicyForHostsThatMissedIt(t *testing.T) {
	t.Parallel()
	r := newAppControlRig(t, []string{"host-a"})
	history := &fakeCommandHistory{latest: map[string]rulesapi.HostCommand{}}
	enrolled := []rulesapi.HostEnrollment{
		{HostID: "host-a", EnrolledAt: time.Now().Add(-48 * time.Hour)},
		{HostID: "late-host", EnrolledAt: time.Now().Add(-time.Hour)},
	}
	converger := credentialguard.NewConverger(credentialguard.NewStore(r.db), history.insert,
		func(context.Context) ([]rulesapi.HostEnrollment, error) { return enrolled, nil }, history.list, slog.Default())

	// Before any change, every host is sent the default policy, so it learns the fleet's mode and list.
	queued, err := converger.Converge(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, queued)
	assert.ElementsMatch(t, []string{"host-a", "late-host"}, history.takeQueued())
	var defaults rulesapi.SetCredentialGuardPayload
	require.NoError(t, json.Unmarshal(history.latest["late-host"].Payload, &defaults))
	assert.Equal(t, "detect", defaults.Mode)
	assert.Len(t, defaults.Credentials, 6)
	queued, err = converger.Converge(t.Context())
	require.NoError(t, err)
	assert.Zero(t, queued, "hosts whose policy is pending are not sent it again")

	put := r.putCredentialGuard(t, workKeys())
	put.Body.Close()
	require.Equal(t, http.StatusOK, put.StatusCode)
	for _, c := range r.inserter.snapshot() {
		history.latest[c.HostID] = rulesapi.HostCommand{Payload: c.Payload, Status: catchup.StatusCompleted, CreatedAt: time.Now()}
	}

	queued, err = converger.Converge(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, queued)
	assert.Equal(t, []string{"late-host"}, history.takeQueued())
	var sent rulesapi.SetCredentialGuardPayload
	require.NoError(t, json.Unmarshal(history.latest["late-host"].Payload, &sent))
	stored := r.credentialGuard(t)
	assert.Equal(t, rulesapi.SetCredentialGuardPayload{Version: stored.Version, Epoch: stored.UpdatedAt.UnixMicro(), Mode: "protect",
		Credentials: stored.Credentials}, sent, "a caught-up host gets the same policy, epoch included, as the push")
}

// The catch-up runs as one of the rules context's loops, wired from the same dependencies as the agent settings'.
func TestCredentialGuardConverge_RunsAsARulesContextLoop(t *testing.T) {
	t.Parallel()
	history := newTypedCommandHistory()
	r := newAppControlRig(t, []string{"host-a"}, func(d *rulesbootstrap.Deps) {
		d.CommandBatchInserter = history.insert
		d.HostEnrollments = func(context.Context) ([]rulesapi.HostEnrollment, error) {
			return []rulesapi.HostEnrollment{{HostID: "late-host", EnrolledAt: time.Now().Add(-time.Hour)}}, nil
		}
		d.HostLatestCommands = history.list
		d.WatchedPathConvergeInterval = 20 * time.Millisecond
	})
	put := r.putCredentialGuard(t, workKeys())
	put.Body.Close()
	require.Equal(t, http.StatusOK, put.StatusCode)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { r.rules.Run(ctx); close(done) }()
	require.Eventually(t, func() bool {
		c, ok := history.get(rulesapi.CommandTypeSetCredentialGuard, "late-host")
		var sent rulesapi.SetCredentialGuardPayload
		return ok && json.Unmarshal(c.Payload, &sent) == nil && sent.Version == 1 && sent.Mode == "protect"
	}, 5*time.Second, 20*time.Millisecond)
	cancel()
	<-done
}
