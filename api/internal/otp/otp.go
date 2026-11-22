// Package otp issues and checks the 6 digit email code that is the second sign-in step (spec 02).
package otp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/account"
	"hotelpms/api/internal/ids"
	"hotelpms/api/internal/mail"
)

const (
	validFor     = 10 * time.Minute
	maxAttempts  = 5
	maxPerHour   = 5
	codeSubject  = "Your sign-in code"
	hourlyWindow = time.Hour
)

var (
	ErrWrong      = errors.New("otp: wrong code")
	ErrExpired    = errors.New("otp: expired")
	ErrCancelled  = errors.New("otp: cancelled after too many wrong tries")
	ErrTooMany    = errors.New("otp: too many codes this hour")
	ErrSendFailed = errors.New("otp: the email could not be sent")
)

// Where describes the sign-in attempt, for the email. Either field may be empty.
type Where struct{ Browser, Place string }

func hashCode(challengeID uuid.UUID, code string) []byte {
	sum := sha256.Sum256([]byte(challengeID.String() + ":" + code))
	return sum[:]
}

func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// Issue sends a new code and cancels the user's earlier ones. A failed send leaves no trace:
// it does not count toward the hourly limit and the user's previous code stays valid.
func Issue(ctx context.Context, pool *pgxpool.Pool, sender mail.Sender, u account.User, w Where, now time.Time) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Serialise requests for one user so two at once cannot both slip under the limit.
	if _, err := tx.Exec(ctx, `select 1 from app_user where id = $1 for update`, u.ID); err != nil {
		return err
	}
	var recent int
	if err := tx.QueryRow(ctx, `select count(*) from otp_challenge where user_id = $1 and created_at > $2`, u.ID, now.Add(-hourlyWindow)).Scan(&recent); err != nil {
		return err
	}
	if recent >= maxPerHour {
		return ErrTooMany
	}
	code, err := newCode()
	if err != nil {
		return err
	}
	id := ids.New()
	if _, err := tx.Exec(ctx, `update otp_challenge set cancelled_at = $2 where user_id = $1 and used_at is null and cancelled_at is null`, u.ID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `insert into otp_challenge (id, user_id, code_hash, created_at, expires_at) values ($1, $2, $3, $4, $5)`,
		id, u.ID, hashCode(id, code), now, now.Add(validFor)); err != nil {
		return err
	}
	if err := sender.Send(ctx, mail.Message{To: u.Email, Subject: codeSubject, Text: emailText(code, w, now)}); err != nil {
		return fmt.Errorf("%w: %v", ErrSendFailed, err)
	}
	return tx.Commit(ctx)
}

func emailText(code string, w Where, now time.Time) string {
	from := ""
	if w.Browser != "" {
		from = " from " + w.Browser
	}
	if w.Place != "" {
		from += " near " + w.Place
	}
	return fmt.Sprintf("Your sign-in code is %s.\n\nIt works for 10 minutes and only once.\n\n"+
		"Someone entered your password%s at %s UTC. If that was you, enter the code to finish signing in.\n"+
		"If it was not you, do not share this code, and change your password.\n", code, from, now.UTC().Format("15:04"))
}

// Verify checks a code against the user's newest one. A wrong try is counted even though an error is returned.
func Verify(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, code string, now time.Time) error {
	var outcome error
	err := func() error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx) //nolint:errcheck

		var (
			id        uuid.UUID
			hash      []byte
			expires   time.Time
			attempts  int
			used      *time.Time
			cancelled *time.Time
		)
		err = tx.QueryRow(ctx,
			`select id, code_hash, expires_at, attempts, used_at, cancelled_at from otp_challenge
			 where user_id = $1 order by created_at desc, id desc limit 1 for update`, userID).
			Scan(&id, &hash, &expires, &attempts, &used, &cancelled)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			outcome = ErrWrong
			return nil
		case err != nil:
			return err
		case used != nil:
			outcome = ErrWrong
			return nil
		case cancelled != nil || attempts >= maxAttempts:
			outcome = ErrCancelled
			return nil
		case now.After(expires):
			outcome = ErrExpired
			return nil
		}
		if subtle.ConstantTimeCompare(hash, hashCode(id, code)) == 1 {
			if _, err := tx.Exec(ctx, `update otp_challenge set used_at = $2 where id = $1`, id, now); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
		attempts++
		if attempts >= maxAttempts {
			if _, err := tx.Exec(ctx, `update otp_challenge set attempts = $2, cancelled_at = $3 where id = $1`, id, attempts, now); err != nil {
				return err
			}
			outcome = ErrCancelled
		} else {
			if _, err := tx.Exec(ctx, `update otp_challenge set attempts = $2 where id = $1`, id, attempts); err != nil {
				return err
			}
			outcome = ErrWrong
		}
		return tx.Commit(ctx) // the count must persist even though the caller gets an error
	}()
	if err != nil {
		return err
	}
	return outcome
}
