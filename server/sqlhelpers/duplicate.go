package sqlhelpers

import (
	"errors"

	"github.com/go-sql-driver/mysql"
)

// mysqlErrDupEntry is MySQL error 1062: "Duplicate entry for key".
const mysqlErrDupEntry = 1062

// IsDuplicateKeyErr reports whether err wraps a MySQL duplicate-key error (1062), which an INSERT against a primary or unique key
// returns when the row is already there.
func IsDuplicateKeyErr(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlErrDupEntry
}
