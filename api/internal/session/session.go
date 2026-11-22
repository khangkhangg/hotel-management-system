// Package session keeps people signed in. A session is "pending" after the first sign-in step and
// "full" after the email code. Ids and device tokens are random; only their SHA-256 hash is stored.
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Stage string

const (
	StagePending Stage = "pending"
	StageFull    Stage = "full"
)

const (
	idleLimit     = 12 * time.Hour
	absoluteLimit = 7 * 24 * time.Hour
	pendingLimit  = 15 * time.Minute // the email code lives 10 minutes; the pending session outlives it slightly
	deviceTrust   = 30 * 24 * time.Hour
)

var (
	ErrNotFound = errors.New("session: not found")
	ErrExpired  = errors.New("session: expired")
)

type Session struct {
	ID         string // the raw id; exists only in memory and in the cookie
	UserID     uuid.UUID
	Stage      Stage
	OrgID      *uuid.UUID
	ViewAs     string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

func newToken() (token string, hash []byte, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashOf(token), nil
}

func hashOf(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func expired(stage Stage, created, lastSeen, now time.Time) bool {
	idle := idleLimit
	if stage == StagePending {
		idle = pendingLimit
	}
	return now.Sub(lastSeen) > idle || now.Sub(created) > absoluteLimit
}

func Create(ctx context.Context, tx pgx.Tx, userID uuid.UUID, stage Stage, device string, now time.Time) (Session, error) {
	id, hash, err := newToken()
	if err != nil {
		return Session{}, err
	}
	_, err = tx.Exec(ctx,
		`insert into session (id_hash, user_id, stage, device_label, created_at, last_seen_at) values ($1, $2, $3, $4, $5, $5)`,
		hash, userID, string(stage), device, now)
	return Session{ID: id, UserID: userID, Stage: stage, CreatedAt: now, LastSeenAt: now}, err
}

// Lookup finds a live session and records that it was just used. It never deletes: a caller that gets
// ErrExpired usually rolls back its transaction, so deletion is left to Purge.
func Lookup(ctx context.Context, tx pgx.Tx, id string, now time.Time) (Session, error) {
	s := Session{ID: id}
	var stage string
	err := tx.QueryRow(ctx,
		`select user_id, stage, org_id, view_as, created_at, last_seen_at from session where id_hash = $1 for update`, hashOf(id)).
		Scan(&s.UserID, &stage, &s.OrgID, &s.ViewAs, &s.CreatedAt, &s.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	s.Stage = Stage(stage)
	if expired(s.Stage, s.CreatedAt, s.LastSeenAt, now) {
		return Session{}, ErrExpired
	}
	if _, err := tx.Exec(ctx, `update session set last_seen_at = $2 where id_hash = $1`, hashOf(id), now); err != nil {
		return Session{}, err
	}
	s.LastSeenAt = now
	return s, nil
}

// Promote turns a pending session into a full one with a new id. It succeeds once per pending session.
func Promote(ctx context.Context, tx pgx.Tx, oldID string, now time.Time) (Session, error) {
	var userID uuid.UUID
	var orgID *uuid.UUID
	var device string
	var createdAt time.Time
	err := tx.QueryRow(ctx,
		`delete from session where id_hash = $1 and stage = 'pending' returning user_id, org_id, device_label, created_at`, hashOf(oldID)).
		Scan(&userID, &orgID, &device, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	s, err := Create(ctx, tx, userID, StageFull, device, now)
	s.OrgID = orgID
	return s, err
}

// Purge deletes every expired session and returns how many. A periodic job calls it.
func Purge(ctx context.Context, tx pgx.Tx, now time.Time) (int64, error) {
	tag, err := tx.Exec(ctx,
		`delete from session where (stage = 'pending' and last_seen_at < $1::timestamptz - $2::interval)
		   or (stage = 'full' and last_seen_at < $1::timestamptz - $3::interval)
		   or created_at < $1::timestamptz - $4::interval`,
		now, pendingLimit.String(), idleLimit.String(), absoluteLimit.String())
	return tag.RowsAffected(), err
}

// SetOrganization records which organization a signed-in person is working in.
func SetOrganization(ctx context.Context, tx pgx.Tx, id string, orgID uuid.UUID) error {
	tag, err := tx.Exec(ctx, `update session set org_id = $2 where id_hash = $1`, hashOf(id), orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// End deletes one session. Ending a session that does not exist is not an error.
func End(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `delete from session where id_hash = $1`, hashOf(id))
	return err
}

func EndAll(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	_, err := tx.Exec(ctx, `delete from session where user_id = $1`, userID)
	return err
}

// TrustDevice returns a token to put in a long-lived cookie. While it is valid, the email code is skipped.
func TrustDevice(ctx context.Context, tx pgx.Tx, userID uuid.UUID, now time.Time) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `insert into trusted_device (token_hash, user_id, expires_at, created_at) values ($1, $2, $3, $4)`,
		hash, userID, now.Add(deviceTrust), now)
	return token, err
}

func IsTrusted(ctx context.Context, tx pgx.Tx, userID uuid.UUID, token string, now time.Time) bool {
	if token == "" {
		return false
	}
	var ok bool
	err := tx.QueryRow(ctx, `select exists (select 1 from trusted_device where token_hash = $1 and user_id = $2 and expires_at > $3)`,
		hashOf(token), userID, now).Scan(&ok)
	return err == nil && ok
}

func ForgetDevices(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	_, err := tx.Exec(ctx, `delete from trusted_device where user_id = $1`, userID)
	return err
}
