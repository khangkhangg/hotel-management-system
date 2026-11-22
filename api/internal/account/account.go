// Package account stores people. A person belongs to no organization; memberships say where they work.
package account

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/ids"
)

var ErrNotFound = errors.New("account: not found")

type User struct {
	ID     uuid.UUID
	Email  string
	Name   string
	Status string
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
	err := tx.QueryRow(ctx, `select id, email, name, status from app_user where email = $1`, NormalizeEmail(email)).
		Scan(&u.ID, &u.Email, &u.Name, &u.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}
