package sqlhelpers

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
)

func TestIsDuplicateKeyErr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"a duplicate entry", &mysql.MySQLError{Number: 1062}, true},
		{"a wrapped duplicate entry", fmt.Errorf("insert: %w", &mysql.MySQLError{Number: 1062}), true},
		{"a deadlock", &mysql.MySQLError{Number: 1213}, false},
		{"another error", errors.New("connection refused"), false},
		{"no error", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsDuplicateKeyErr(tc.err))
		})
	}
}
