package breakglass

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jmoiron/sqlx"

	"github.com/rawtrail/rawtrail/server/sqlhelpers"
)

// ErrChallengeReused is returned when a WebAuthn challenge that already finished a ceremony is presented again (issue #1618).
var ErrChallengeReused = errors.New("breakglass: challenge already used")

// ErrChallengeExpired is returned for a challenge past its expiry, or one that carries none, as a cookie issued before the server
// enforced challenge timeouts does: the engine skips its own check for a zero expiry, so it is refused here.
var ErrChallengeExpired = errors.New("breakglass: challenge expired")

// consumedChallenges records each WebAuthn challenge that finishes a ceremony, so a challenge is accepted once. The challenge rides a
// signed cookie with no server-side row, so without this a captured challenge and assertion would be accepted again until the challenge
// expires. The table lives in MySQL so every replica refuses a repeat (ADR-0010).
//
// Expiry is judged by one clock, the database's UTC_TIMESTAMP(6), against expiries stored as UTC in a DATETIME column, so neither the
// session's nor any replica's time zone or clock enters it. A challenge is recorded only while it is unexpired by that clock. The sweep
// keeps each record for sweepGrace past its challenge's expiry, so a record cannot vanish while a consume that judged its challenge
// unexpired is still in flight: such a statement would have to stall longer than sweepGrace, far beyond InnoDB's lock wait timeout.
// sweepGrace is how long past its challenge's expiry a record is kept. It bounds the longest a consume statement may take between
// judging a challenge unexpired and inserting its record, so it must exceed innodb_lock_wait_timeout (50 seconds by default) by a wide
// margin.
const sweepGrace = time.Hour

type consumedChallenges struct {
	db *sqlx.DB
}

func newConsumedChallenges(db *sqlx.DB) *consumedChallenges {
	return &consumedChallenges{db: db}
}

// consume records sd's challenge as used. It returns ErrChallengeReused if it already was, and ErrChallengeExpired if the challenge
// carries no expiry or is past it. The record is kept until the challenge's own expiry.
func (c *consumedChallenges) consume(ctx context.Context, sd webauthn.SessionData) error {
	// The insert's condition refuses a zero expiry with the expired ones: it is never after UTC_TIMESTAMP(6).
	digest := sha256.Sum256([]byte(sd.Challenge))
	expires := sd.Expires.UTC()
	res, err := c.db.ExecContext(ctx, `
		INSERT INTO breakglass_consumed_challenges (challenge_hash, expires_at)
		SELECT ?, ? FROM DUAL WHERE ? > UTC_TIMESTAMP(6)`, digest[:], expires, expires)
	switch {
	case sqlhelpers.IsDuplicateKeyErr(err):
		return ErrChallengeReused
	case err != nil:
		return fmt.Errorf("breakglass: consume challenge: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrChallengeExpired
	}
	return nil
}

// cleanupExpired deletes the records of challenges expired more than sweepGrace ago by the database's clock and returns how many it
// removed.
func (c *consumedChallenges) cleanupExpired(ctx context.Context) (int64, error) {
	res, err := c.db.ExecContext(ctx,
		`DELETE FROM breakglass_consumed_challenges WHERE expires_at <= DATE_SUB(UTC_TIMESTAMP(6), INTERVAL ? MICROSECOND)`,
		sweepGrace.Microseconds())
	if err != nil {
		return 0, fmt.Errorf("breakglass: clean up consumed challenges: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
