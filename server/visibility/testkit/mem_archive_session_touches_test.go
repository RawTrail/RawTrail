package testkit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fleetdm/edr/server/visibility/api"
)

// TestMemArchive_SessionTouches pins the fake to the ClickHouse read (TestEventArchive_AgentSpans): the first touch by the generation
// of a path named after each session, other paths and other generations left out.
func TestMemArchive_SessionTouches(t *testing.T) {
	t.Parallel()
	const (
		dir    = "/Users/dev/.claude/projects/-Users-dev-qa"
		first  = "4f5b9ea2-30e4-43f2-96e7-aa36898179d5"
		second = "07c2d66d-1b9e-4ae4-871c-bb28533aa98e"
	)
	file := func(id string, ts int64, pv, path string) api.Event {
		return api.Event{EventID: id, HostID: "h1", TimestampNs: ts, IngestedAtNs: ts, EventType: "file_activity",
			Payload: []byte(`{"pid":5709,"pidversion":` + pv + `,"operation":"modify","path":"` + path + `"}`)}
	}
	arch := NewMemArchive()
	require.NoError(t, arch.Insert(context.Background(), []api.Event{
		file("a", 20, "9", dir+"/"+first+".jsonl"),
		file("b", 10, "9", dir+"/"+first+".jsonl"),
		file("c", 49, "9", "/private/tmp/claude-501/-Users-dev-qa/"+second+"/scratchpad"),
		file("d", 50, "9", dir+"/"+second+".jsonl"),
		file("e", 53, "9", dir+"/notes.txt"),
		file("f", 5, "7", dir+"/"+first+".jsonl"),
	}))
	got, err := arch.SessionTouches(context.Background(), "h1", api.ProcessGeneration{PID: 5709, PIDVersion: 9})
	require.NoError(t, err)
	assert.Equal(t, []api.SessionTouch{{SessionID: first, FirstNs: 10}, {SessionID: second, FirstNs: 49}}, got)
}
