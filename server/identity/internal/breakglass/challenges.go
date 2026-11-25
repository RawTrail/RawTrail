package breakglass

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

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
// The record and the sweep judge expiry by one clock, the database's: a challenge is recorded only while it is unexpired by NOW(6),
// and the sweep deletes only records expired by NOW(6). So a replay that validated just before expiry and reaches the insert after the
// sweep removed the original record is refused as expired rather than recorded afresh.
type consumedChallenges struct {
	db *sqlx.DB
}

func newConsumedChallenges(db *sqlx.DB) *consumedChallenges {
	return &consumedChallenges{db: db}
}

// consume records sd's challenge as used. It returns ErrChallengeReused if it already was, and ErrChallengeExpired if the challenge
// carries no expiry or is past it. The record is kept until the challenge's own expiry.
func (c *consumedChallenges) consume(ctx context.Context, sd webauthn.SessionData) error {
	// The insert's condition refuses a zero expiry with the expired ones: it is never after NOW(6).
	digest := sha256.Sum256([]byte(sd.Challenge))
	expires := sd.Expires.UTC()
	res, err := c.db.ExecContext(ctx, `
		INSERT INTO breakglass_consumed_challenges (challenge_hash, expires_at)
		SELECT ?, ? FROM DUAL WHERE ? > NOW(6)`, digest[:], expires, expires)
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

// cleanupExpired deletes the records of challenges past their expiry by the database's clock and returns how many it removed.
func (c *consumedChallenges) cleanupExpired(ctx context.Context) (int64, error) {
	res, err := c.db.ExecContext(ctx, `DELETE FROM breakglass_consumed_challenges WHERE expires_at <= NOW(6)`)
	if err != nil {
		return 0, fmt.Errorf("breakglass: clean up consumed challenges: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
