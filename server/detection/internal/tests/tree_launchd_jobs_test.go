//go:build integration

// Launchd job definitions (issue #1341): a `launchctl load|bootstrap` exec carries the jobs the agent read from its plists, the graph
// builder stores them on the row, and the tree links the command to the jobs launchd starts by their labels.

package tests

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fleetdm/edr/server/detection/api"
	"github.com/fleetdm/edr/server/detection/internal/graph"
)

const jobsSession = "f4b1b99e-0000-4000-8000-0000000013a1"

// spec:server-process-graph-builder/launchd-job-definitions-on-the-row/an-exec-stores-the-jobs-it-loaded
// spec:server-rest-api/launches-by-job-definition/a-load-links-each-job-it-defined
func TestLaunchdJobs_StoredAndLinked(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	ctx := t.Context()
	host := "launchd-jobs-" + t.Name()
	env := `"env":{"CLAUDE_CODE_SESSION_ID":"` + jobsSession + `","CLAUDE_PID":"30995","AI_AGENT":"claude-code_2-1-300_agent"}`
	jobs := `"launchd_jobs":[{"plist_path":"/Users/v/exp/load.plist","label":"com.fleetdm.qa1341.load",` +
		`"program_arguments":["/bin/sleep","71"]},` +
		`{"plist_path":"/Users/v/exp/two.plist","label":"com.fleetdm.qa1341.two","program":"/bin/sleep"}]`
	evt := func(id, typ string, ms int64, payload string) api.Event {
		return api.Event{EventID: id, HostID: host, TimestampNs: at(float64(ms) / 1000), IngestedAtNs: at(float64(ms)/1000) + 1, EventType: typ,
			Payload: json.RawMessage(payload)}
	}
	// launchdJob is a launchd job start as the extension reports it: xpcproxy forked by launchd and exec'd naming the label, then
	// re-exec'd into the program, run as the job's user.
	launchdJob := func(id string, pid int, ms int64, label string) []api.Event {
		p := strconv.Itoa(pid)
		return []api.Event{
			evt(id+"-f", "fork", ms, `{"child_pid":`+p+`,"parent_pid":1}`),
			evt(id+"-x", "exec", ms+1, `{"pid":`+p+`,"ppid":1,"path":"/usr/libexec/xpcproxy","args":["xpcproxy","`+label+`"],"cwd":"/",`+
				`"uid":0,"gid":0,"original_ppid":1}`),
			evt(id+"-p", "exec", ms+2, `{"pid":`+p+`,"ppid":1,"path":"/bin/sleep","args":["/bin/sleep","71"],"cwd":"/",`+
				`"uid":501,"gid":20,"original_ppid":1}`),
		}
	}
	events := []api.Event{
		evt("shell-f", "fork", 1000, `{"child_pid":31157,"parent_pid":30995}`),
		evt("shell-x", "exec", 1001, `{"pid":31157,"ppid":30995,"path":"/bin/zsh","args":["/bin/zsh","-c","eval 'launchctl load x'"],`+
			`"cwd":"/Users/v/exp","uid":501,"gid":20,`+env+`}`),
		evt("load-f", "fork", 1010, `{"child_pid":31158,"parent_pid":31157}`),
		evt("load-x", "exec", 1011, `{"pid":31158,"ppid":31157,"path":"/bin/launchctl","args":["launchctl","load","load.plist","two.plist"],`+
			`"cwd":"/Users/v/exp","uid":501,"gid":20,`+env+`,`+jobs+`}`),
	}
	events = append(events, launchdJob("job-decoy", 31160, 1012, "com.fleetdm.unrelated")...)
	events = append(events, launchdJob("job-load", 31159, 1013, "com.fleetdm.qa1341.load")...)
	// The first job started again (a KeepAlive restart) before the second started: it must not use up the second's launch.
	events = append(events, launchdJob("job-load-again", 31162, 1013, "com.fleetdm.qa1341.load")...)
	events = append(events, launchdJob("job-two", 31161, 1014, "com.fleetdm.qa1341.two")...)
	require.NoError(t, graph.NewBuilder(store, discardLogger()).ProcessBatch(ctx, events))

	var stored api.NullRawJSON
	require.NoError(t, db.GetContext(ctx, &stored, "SELECT launchd_jobs FROM processes WHERE host_id = ? AND pid = 31158", host))
	var got []map[string]any
	require.NoError(t, json.Unmarshal(stored, &got))
	require.Len(t, got, 2, "the exec's job definitions are on its row")
	assert.Equal(t, "com.fleetdm.qa1341.load", got[0]["label"])
	assert.Equal(t, []any{"/bin/sleep", "71"}, got[0]["program_arguments"])
	var unrelated api.NullRawJSON
	require.NoError(t, db.GetContext(ctx, &unrelated, "SELECT launchd_jobs FROM processes WHERE host_id = ? AND pid = 31157", host))
	assert.Empty(t, unrelated, "any other exec stores none")

	res, err := graph.NewQuery(store).BuildScopedTree(ctx, host, api.TreeScope{AgentSessionID: jobsSession}, 2000, true, 0)
	require.NoError(t, err)
	byID := map[int64]api.Process{}
	var walk func([]api.ProcessNode)
	walk = func(nodes []api.ProcessNode) {
		for _, n := range nodes {
			byID[n.ID] = n.Process
			walk(n.Children)
		}
	}
	walk(res.Roots)
	var linkedPIDs []int
	for _, l := range res.Links {
		require.Equal(t, api.ProcessLinkInferredLaunch, l.Kind)
		assert.Equal(t, "/bin/launchctl", byID[l.From].Path)
		assert.Equal(t, "/bin/sleep", byID[l.To].Path, "the program, not launchd's xpcproxy")
		linkedPIDs = append(linkedPIDs, byID[l.To].PID)
	}
	assert.ElementsMatch(t, []int{31159, 31161}, linkedPIDs,
		"each job the load defined, once: not the unrelated job started between them, and not the first job's restart in place of the second")
}
