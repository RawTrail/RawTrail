package api_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/rawtrail/rawtrail/server/visibility/api"
)

// The digest's file counts go to the UI under these names, which the Summary and the shared image read.
//
// spec:server-rest-api/agent-session-digest/the-digest-counts-the-files-the-session-changed
func TestFileDigest_WireShape(t *testing.T) {
	t.Parallel()
	got, err := json.Marshal(api.FileDigest{Changed: 12, OutsideProject: 3, Temporary: 4, AgentFiles: 40})
	require.NoError(t, err)
	assert.JSONEq(t, `{"changed":12,"outside_project":3,"temporary":4,"agent_files":40}`, string(got))
}

func TestFileDigest_RoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		in := api.FileDigest{
			Changed: rapid.Int64().Draw(t, "changed"), OutsideProject: rapid.Int64().Draw(t, "outside"),
			Temporary: rapid.Int64().Draw(t, "temporary"), AgentFiles: rapid.Int64().Draw(t, "agent"),
		}
		raw, err := json.Marshal(in)
		require.NoError(t, err)
		var out api.FileDigest
		require.NoError(t, json.Unmarshal(raw, &out))
		assert.Equal(t, in, out)
	})
}
