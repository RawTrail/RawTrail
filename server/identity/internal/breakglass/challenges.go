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

// consumedChallenges records each WebAuthn challenge that finishes a ceremony, so a challenge is accepted once. The challenge rides a
// signed cookie with no server-side row, so without this a captured challenge and assertion would be accepted again until the challenge
// expires. The table lives in MySQL so every replica refuses a repeat (ADR-0010).
type consumedChallenges struct {
	db  *sqlx.DB
	now func() time.Time
}

func newConsumedChallenges(db *sqlx.DB) *consumedChallenges {
	return &consumedChallenges{db: db, now: time.Now}
}

// consume records sd's challenge as used and returns ErrChallengeReused if it already was. The row is kept until the challenge's own
// expiry, after which the engine refuses it as expired anyway.
func (c *consumedChallenges) consume(ctx context.Context, sd webauthn.SessionData) error {
	digest := sha256.Sum256([]byte(sd.Challenge))
	expires := sd.Expires
	if expires.IsZero() {
		// The engine enforces timeouts, so it stamps every challenge; a zero expiry still keeps the row for a whole lifetime.
		expires = c.now().Add(ChallengeLifetime)
	}
	_, err := c.db.ExecContext(ctx,
		`INSERT INTO breakglass_consumed_challenges (challenge_hash, expires_at) VALUES (?, ?)`, digest[:], expires.UTC())
	switch {
	case sqlhelpers.IsDuplicateKeyErr(err):
		return ErrChallengeReused
	case err != nil:
		return fmt.Errorf("breakglass: consume challenge: %w", err)
	}
	return nil
}

// cleanupExpired deletes the rows of challenges past their expiry and returns how many it removed.
func (c *consumedChallenges) cleanupExpired(ctx context.Context) (int64, error) {
	res, err := c.db.ExecContext(ctx, `DELETE FROM breakglass_consumed_challenges WHERE expires_at <= ?`, c.now().UTC())
	if err != nil {
		return 0, fmt.Errorf("breakglass: clean up consumed challenges: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
