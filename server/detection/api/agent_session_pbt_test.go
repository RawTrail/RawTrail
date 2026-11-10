package api_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// The sessions list's response shape round-trips through JSON (issue #1283): every field, with the optional pointers both nil and set
// and session strings arbitrary, as the agent supplies them. A changed tag or field type that breaks the wire contract fails here.
func TestAgentSessionResultRoundTripProperty(t *testing.T) {
	t.Parallel()
	optString := func(t *rapid.T, label string) *string {
		if rapid.Bool().Draw(t, label+"?") {
			return new(rapid.String().Draw(t, label))
		}
		return nil
	}
	rapid.Check(t, func(t *rapid.T) {
		rows := rapid.SliceOfN(rapid.Custom(func(t *rapid.T) api.AgentSessionSummary {
			row := api.AgentSessionSummary{
				HostID: rapid.String().Draw(t, "host"), AgentSessionID: rapid.String().Draw(t, "session"),
				AgentName: rapid.String().Draw(t, "agent"), AgentVersion: optString(t, "version"), CWD: optString(t, "cwd"),
				FirstSeenNs: rapid.Int64().Draw(t, "first"), LastSeenNs: rapid.Int64().Draw(t, "last"),
				ProcessCount: rapid.Int64().Draw(t, "processes"), ProcessCountCapped: rapid.Bool().Draw(t, "capped"),
				AlertCount: rapid.Int64().Draw(t, "alerts"),
			}
			if rapid.Bool().Draw(t, "pid?") {
				row.AgentPID = new(rapid.Int().Draw(t, "pid"))
			}
			return row
		}), 0, 4).Draw(t, "rows")
		in := api.AgentSessionResult{Rows: rows, NextCursor: rapid.String().Draw(t, "cursor"), TotalMatched: rapid.Int64().Draw(t, "total")}

		raw, err := json.Marshal(in)
		require.NoError(t, err)
		var out api.AgentSessionResult
		require.NoError(t, json.Unmarshal(raw, &out))
		if len(in.Rows) == 0 {
			// An empty page and a nil one are the same on the wire; the handler sends [] for both.
			in.Rows, out.Rows = nil, nil
		}
		require.Equal(t, in, out)
	})
}

// TestAgentSessionSummary_ProcessCountCappedWire pins the capped flag's wire name, which the UI reads to show a count as "10,000+"
// (issue #1547), and its absence on an exact count. A round trip cannot catch a renamed tag: both directions use the same one.
func TestAgentSessionSummary_ProcessCountCappedWire(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		capped bool
		want   map[string]any
	}{
		{name: "capped count says so", capped: true, want: map[string]any{"process_count": float64(10000), "process_count_capped": true}},
		{name: "exact count omits the flag", capped: false, want: map[string]any{"process_count": float64(10000)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(api.AgentSessionSummary{ProcessCount: 10000, ProcessCountCapped: c.capped})
			require.NoError(t, err)
			var got map[string]any
			require.NoError(t, json.Unmarshal(raw, &got))
			for _, key := range []string{"process_count", "process_count_capped"} {
				want, present := c.want[key]
				if !present {
					require.NotContains(t, got, key)
					continue
				}
				require.Equal(t, want, got[key], key)
			}
		})
	}
}
