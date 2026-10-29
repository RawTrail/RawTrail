package clickhouse

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/visibility/api"
)

// A prompt page's cursor is the event search's own token, and only a whole one is read: a token without its time or its event id
// names no prompt, so it is refused rather than read from a guessed position.
func TestDecodePromptCursor(t *testing.T) {
	t.Parallel()
	c, err := DecodePromptCursor(api.EncodeEventCursor(api.EventCursor{TimestampNs: 42, EventID: "evt:1"}))
	require.NoError(t, err)
	assert.Equal(t, api.EventCursor{TimestampNs: 42, EventID: "evt:1"}, c)
	for name, token := range map[string]string{
		"not base64url":    "!!",
		"no separator":     base64.RawURLEncoding.EncodeToString([]byte(`{"e":"evt-1"}`)),
		"no time":          base64.RawURLEncoding.EncodeToString([]byte(":evt-1")),
		"no event id":      base64.RawURLEncoding.EncodeToString([]byte("42:")),
		"a time not a num": base64.RawURLEncoding.EncodeToString([]byte("x:evt-1")),
	} {
		_, err := DecodePromptCursor(token)
		assert.ErrorIs(t, err, api.ErrInvalidPromptCursor, name)
	}
}
