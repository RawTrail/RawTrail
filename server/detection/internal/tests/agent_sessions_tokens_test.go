//go:build integration

// The sessions list's token totals (issue #1427): each row's tokens, read from the event archive in one read over the page.

package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/mysql"
	detectiontestkit "github.com/rawtrail/rawtrail/server/detection/testkit"
	"github.com/rawtrail/rawtrail/server/testdb/full"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// failingTokens is an archive whose token read fails, as an unreachable ClickHouse does.
type failingTokens struct{ visibilityapi.EventArchive }

func (failingTokens) AgentSessionTokens(context.Context, []visibilityapi.AgentSessionRef) (map[visibilityapi.AgentSessionRef]int64, error) {
	return nil, errors.New("clickhouse unreachable")
}

// stalledTokens is an archive whose token read never returns until its context ends, as a stalled ClickHouse does.
type stalledTokens struct{ visibilityapi.EventArchive }

func (stalledTokens) AgentSessionTokens(
	ctx context.Context, _ []visibilityapi.AgentSessionRef,
) (map[visibilityapi.AgentSessionRef]int64, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// spec:server-rest-api/sessions-list-token-total/each-session-is-listed-with-its-tokens
func TestAgentSessionsList_Tokens(t *testing.T) {
	t.Parallel()
	db := full.Open(t)
	archive := detectiontestkit.NewMemArchive()
	host := "tokens-list"
	require.NoError(t, archive.Insert(t.Context(), rewriteHost([]api.Event{
		usage("t-one", 1000, sessionOne, "auto", ""),
		usage("t-one-b", 2000, sessionOne, "bypassPermissions", ""),
	}, host)))
	store, err := mysql.New(db, archive, nil)
	require.NoError(t, err)
	seedSessionRow(t, db, host, sessionOne, "claude-code", "/Users/v/project", 100, 900)
	seedSessionRow(t, db, host, sessionTwo, "claude-code", "/Users/v/other", 200, 800)

	t.Run("each session carries its tokens, and one that sent no usage none", func(t *testing.T) {
		t.Parallel()
		page, err := store.ListAgentSessions(t.Context(), api.AgentSessionFilter{HostID: host}, "", 10)
		require.NoError(t, err)
		tokens := map[string]*int64{}
		for _, r := range page.Rows {
			tokens[r.AgentSessionID] = r.Tokens
		}
		// Each usage() message is one input and one output token.
		assert.Equal(t, map[string]*int64{sessionOne: new(int64(4)), sessionTwo: nil}, tokens)
	})

	for name, unreadable := range map[string]visibilityapi.EventArchive{
		"an archive that fails": failingTokens{archive}, "an archive that stalls": stalledTokens{archive},
	} {
		t.Run(name+" leaves the totals out and still lists the sessions", func(t *testing.T) {
			t.Parallel()
			degraded, err := mysql.New(db, unreadable, nil)
			require.NoError(t, err)
			start := time.Now()
			page, err := degraded.ListAgentSessions(t.Context(), api.AgentSessionFilter{HostID: host}, "", 10)
			require.NoError(t, err)
			assert.Less(t, time.Since(start), 10*time.Second, "within the token read's bound, well inside the write timeout")
			require.Len(t, page.Rows, 2)
			for _, r := range page.Rows {
				assert.Nil(t, r.Tokens, r.AgentSessionID)
			}
		})
	}
}
