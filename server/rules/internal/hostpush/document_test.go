package hostpush

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rawtrail/rawtrail/internal/versionedset"
)

// A missing dependency is a wiring bug, refused when the server is built rather than at the first push.
func TestNewDocumentAndConverger_PanicOnAMissingDependency(t *testing.T) {
	t.Parallel()
	assert.PanicsWithValue(t, "hostpush.NewDocument: db must not be nil", func() {
		NewDocument[int](nil, versionedset.Table{}, "outbox", nil, nil)
	})
	assert.PanicsWithValue(t, "hostpush.NewConverger: settings: every dependency but the logger is required", func() {
		NewConverger(ConvergerConfig[int]{Subject: "settings"})
	})
}
