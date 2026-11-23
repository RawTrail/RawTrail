package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// The session a rule resolved for a finding reaches the alert, which is what keeps an alert anchored on the agent's own process in
// its session's views (issue #1589).
func TestAlertFromFinding_CarriesTheResolvedSession(t *testing.T) {
	t.Parallel()
	f := api.Finding{HostID: "h", RuleID: "agent_credential_read", ProcessID: 3, Subject: "s", AgentSessionID: "session-1"}
	assert.Equal(t, "session-1", alertFromFinding(f, nil, "", api.AlertDispositionAlert).AgentSessionID)
}
