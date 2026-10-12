package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fleetdm/edr/server/detection/api"
	"github.com/fleetdm/edr/server/detection/internal/mysql"
)

// The overlay's exit binding must close the row the store's UPDATE closes (issue #1280); TestStore_UpdateProcessExit_ByGeneration
// pins the same table against MySQL. Every fold runs through the overlay, so a drift here is the production behavior.
func TestBatchUpdateProcessExit_ByGeneration(t *testing.T) {
	t.Parallel()
	type seed struct {
		name   string
		ver    *uint32
		forkNs int64
	}
	unversioned := seed{"unversioned", nil, 250}
	older := seed{"older", new(uint32(7300)), 200}
	exact := seed{"exact", new(uint32(7350)), 150}
	newer := seed{"newer", new(uint32(7400)), 300}
	cases := []struct {
		name    string
		rows    []seed
		exitVer *uint32
		closed  string
	}{
		{"the exact generation beats an older row that forked later", []seed{older, exact, newer}, new(uint32(7350)), "exact"},
		{"an older row when the exact one is missing", []seed{older, newer}, new(uint32(7350)), "older"},
		{"an older row beats an unversioned one", []seed{unversioned, older}, new(uint32(7350)), "older"},
		{"an unversioned row when it is the only one", []seed{unversioned}, new(uint32(7350)), "unversioned"},
		{"never a newer generation", []seed{newer}, new(uint32(7350)), ""},
		{"an exit without a generation closes the newest fork", []seed{older, newer}, nil, "newer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &batchSession{byKey: map[mysql.HostPID][]*procRow{}, nextProv: -1}
			byName := map[string]*procRow{}
			for i, r := range tc.rows {
				row := &procRow{seq: int64(i + 1), loaded: true, creIdx: -1, prevNew: -1,
					proc: api.Process{ID: int64(i + 1), HostID: "h", PID: 30, ForkTimeNs: r.forkNs, PIDVersion: r.ver}}
				s.rows = append(s.rows, row)
				s.byKey[mysql.HostPID{HostID: "h", PID: 30}] = append(s.byKey[mysql.HostPID{HostID: "h", PID: 30}], row)
				byName[r.name] = row
			}
			affected, err := s.UpdateProcessExit(t.Context(), mysql.ProcessExitUpdate{
				HostID: "h", PID: 30, ExitTimeNs: 400, ExitIngestedAtNs: 401, ExitEventID: "exit-gen", PIDVersion: tc.exitVer,
			})
			require.NoError(t, err)
			assert.Equal(t, map[bool]int64{true: 1, false: 0}[tc.closed != ""], affected)
			for name, row := range byName {
				assert.Equal(t, name == tc.closed, row.proc.ExitTimeNs != nil, "row %s", name)
			}
		})
	}
}
