package graph

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// forestEdges flattens a forest into child id -> parent id, with 0 for a root, and fails the test when a row appears twice.
func forestEdges(t require.TestingT, forest []api.ProcessNode) map[int64]int64 {
	edges := map[int64]int64{}
	var walk func(parent int64, nodes []api.ProcessNode)
	walk = func(parent int64, nodes []api.ProcessNode) {
		for _, n := range nodes {
			_, dup := edges[n.ID]
			require.False(t, dup, "row %d appears twice in the forest", n.ID)
			edges[n.ID] = parent
			walk(n.ID, n.Children)
		}
	}
	walk(0, forest)
	return edges
}

// Each link rule against a page where pid 500 is held by a process's creator and by a later process that reused the number, which
// the newest-row-per-number rule picks.
//
// spec:server-rest-api/process-forest-links-by-creator/the-creator-row-beats-a-reused-pid
// spec:server-rest-api/process-forest-links-by-creator/a-creator-outside-the-result-leaves-the-process-a-root
// spec:server-rest-api/process-forest-links-by-creator/an-exact-generation-beats-a-reused-pid
func TestBuildForest_LinksByCreator(t *testing.T) {
	t.Parallel()
	creator := api.Process{ID: 10, PID: 500, PPID: 1, ForkTimeNs: 100, PIDVersion: new(uint32(7301))}
	reuser := api.Process{ID: 30, PID: 500, PPID: 1, ForkTimeNs: 900, PIDVersion: new(uint32(7351))}
	cases := []struct {
		name   string
		page   []api.Process
		child  api.Process
		parent int64
	}{
		{
			name:   "the creator row beats a reused pid",
			page:   []api.Process{creator, reuser},
			child:  api.Process{ID: 20, PID: 600, PPID: 500, ForkTimeNs: 950, ParentProcessID: new(int64(10))},
			parent: 10,
		},
		{
			name:   "a creator outside the result leaves the process a root",
			page:   []api.Process{reuser},
			child:  api.Process{ID: 20, PID: 600, PPID: 500, ForkTimeNs: 950, ParentProcessID: new(int64(10))},
			parent: 0,
		},
		{
			name:   "an exact generation beats a reused pid",
			page:   []api.Process{creator, reuser},
			child:  api.Process{ID: 20, PID: 600, PPID: 500, ForkTimeNs: 950, ParentPIDVersion: new(uint32(7301))},
			parent: 10,
		},
		{
			name:   "a generation not on the page falls back to the newest row holding the pid",
			page:   []api.Process{creator, reuser},
			child:  api.Process{ID: 20, PID: 600, PPID: 500, ForkTimeNs: 950, ParentPIDVersion: new(uint32(7300))},
			parent: 30,
		},
		{
			name: "an identity held by a process that exited before the fork falls back to the newest row holding the pid",
			page: []api.Process{
				{ID: 5, PID: 500, PPID: 1, ForkTimeNs: 50, ExitTimeNs: new(int64(60)), PIDVersion: new(uint32(7301))},
				reuser,
			},
			child:  api.Process{ID: 20, PID: 600, PPID: 500, ForkTimeNs: 950, ParentPIDVersion: new(uint32(7301))},
			parent: 30,
		},
		{
			name: "of two rows sharing an identity across a reboot, the one alive at the fork",
			page: []api.Process{
				creator,
				{ID: 40, PID: 500, PPID: 1, ForkTimeNs: 2000, PIDVersion: new(uint32(7301))},
			},
			child:  api.Process{ID: 20, PID: 600, PPID: 500, ForkTimeNs: 950, ParentPIDVersion: new(uint32(7301))},
			parent: 10,
		},
		{
			name: "of two open rows sharing an identity, the later fork",
			page: []api.Process{
				creator,
				{ID: 45, PID: 500, PPID: 1, ForkTimeNs: 800, PIDVersion: new(uint32(7301))},
			},
			child:  api.Process{ID: 20, PID: 600, PPID: 500, ForkTimeNs: 950, ParentPIDVersion: new(uint32(7301))},
			parent: 45,
		},
		{
			name:   "a row without lineage keeps the newest row holding the pid",
			page:   []api.Process{creator, reuser},
			child:  api.Process{ID: 20, PID: 600, PPID: 500, ForkTimeNs: 950},
			parent: 30,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			edges := forestEdges(t, buildForest(append(tc.page, tc.child), nil))
			assert.Equal(t, tc.parent, edges[20])
		})
	}

	t.Run("an edge the caller resolved still wins", func(t *testing.T) {
		t.Parallel()
		child := api.Process{ID: 20, PID: 600, PPID: 500, ForkTimeNs: 950, ParentProcessID: new(int64(10))}
		edges := forestEdges(t, buildForest([]api.Process{creator, reuser, child}, map[int64]int64{20: 30}))
		assert.Equal(t, int64(30), edges[20])
	})
}

// Property: for any page whose rows link by exact identity, the forest holds every row exactly once and hangs each under the row it
// named, or at the top when that row is not on the page. The page draws pid reuse freely, with a decoy ppid on every linked row, so
// the newest-row-per-number rule would answer differently whenever it got the chance.
//
// Links point at lower ids that forked earlier, as the graph builder's do: a creator's row exists, and had forked, before the link to
// it is resolved.
func TestBuildForest_ExactEdgesProperty(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 25).Draw(rt, "n")
		page := make([]api.Process, 0, n)
		want := map[int64]int64{}
		for i := range n {
			id := int64(i + 1)
			is := strconv.Itoa(i)
			p := api.Process{
				ID: id, PID: rapid.IntRange(500, 504).Draw(rt, "pid"+is), ForkTimeNs: int64(i) * 10,
				PIDVersion: new(uint32(7000 + i)),
			}
			switch kind := rapid.SampledFrom([]string{"creator", "off-page", "identity", "root"}).Draw(rt, "kind"+is); {
			case kind == "root" || i == 0:
				// ppid 1 is never on the page, so nothing claims the row as a child.
				p.PPID = 1
			case kind == "creator":
				target := page[rapid.IntRange(0, i-1).Draw(rt, "target"+is)]
				p.ParentProcessID = &target.ID
				p.PPID = rapid.IntRange(500, 504).Draw(rt, "decoy"+is)
				want[id] = target.ID
			case kind == "off-page":
				p.ParentProcessID = new(int64(1000 + i))
				p.PPID = rapid.IntRange(500, 504).Draw(rt, "decoy"+is)
			case kind == "identity":
				target := page[rapid.IntRange(0, i-1).Draw(rt, "target"+is)]
				p.PPID, p.ParentPIDVersion = target.PID, target.PIDVersion
				want[id] = target.ID
			}
			page = append(page, p)
		}
		edges := forestEdges(rt, buildForest(page, nil))
		require.Len(rt, edges, n, "every row appears in the forest")
		for _, p := range page {
			assert.Equal(rt, want[p.ID], edges[p.ID], "row %d", p.ID)
		}
	})
}
