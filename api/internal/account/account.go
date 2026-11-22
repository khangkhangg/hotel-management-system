// Package account stores people. A person belongs to no organization; memberships say where they work.
package account

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/ids"
)

var ErrNotFound = errors.New("account: not found")

type User struct {
	ID           uuid.UUID
	Email        string
	Name         string
	Status       string
	PasswordHash string // empty when the person has not set a password (for example, Google sign-in only)
}

// NormalizeEmail trims and lower-cases. It does not strip dots or plus tags, which belong to the address.
func NormalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func Create(ctx context.Context, tx pgx.Tx, email, name string) (uuid.UUID, error) {
	id := ids.New()
	_, err := tx.Exec(ctx, `insert into app_user (id, email, name) values ($1, $2, $3)`, id, NormalizeEmail(email), name)
	return id, err
}

func ByEmail(ctx context.Context, tx pgx.Tx, email string) (User, error) {
	var u User
	err := tx.QueryRow(ctx, `select id, email, name, status, coalesce(password_hash, '') from app_user where email = $1`, NormalizeEmail(email)).
		Scan(&u.ID, &u.Email, &u.Name, &u.Status, &u.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func ByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (User, error) {
	var u User
	err := tx.QueryRow(ctx, `select id, email, name, status, coalesce(password_hash, '') from app_user where id = $1`, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.Status, &u.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// SetPassword stores an already hashed password.
func SetPassword(ctx context.Context, tx pgx.Tx, id uuid.UUID, hash string) error {
	tag, err := tx.Exec(ctx, `update app_user set password_hash = $2 where id = $1`, id, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func TouchSignIn(ctx context.Context, tx pgx.Tx, id uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `update app_user set last_sign_in_at = $2 where id = $1`, id, at)
	return err
}
