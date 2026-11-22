// Package tenant creates organizations, properties and the memberships that give people their roles.
package tenant

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/ids"
)

type PropertyInput struct {
	Name          string
	Slug          string
	TimeZone      string
	Currency      string
	DefaultLocale string
	Address       string
}

// CreateOrganization makes a new organization. The row-level policy only lets a transaction write the
// organization it is working in, so the new id becomes this transaction's organization.
func CreateOrganization(ctx context.Context, tx pgx.Tx, name string) (uuid.UUID, error) {
	id := ids.New()
	if _, err := tx.Exec(ctx, `select set_config('app.org_id', $1, true)`, id.String()); err != nil {
		return uuid.Nil, err
	}
	_, err := tx.Exec(ctx, `insert into organization (id, name) values ($1, $2)`, id, name)
	return id, err
}

// CreateProperty must run in a transaction opened with db.WithOrg for the same organization.
func CreateProperty(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, p PropertyInput) (uuid.UUID, error) {
	id := ids.New()
	_, err := tx.Exec(ctx,
		`insert into property (id, organization_id, name, slug, time_zone, currency, default_locale, address)
		 values ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, orgID, p.Name, p.Slug, p.TimeZone, p.Currency, p.DefaultLocale, p.Address)
	return id, err
}

// AddMembership gives a user a role at one property, or organization-wide when propertyID is nil.
func AddMembership(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID, propertyID *uuid.UUID, role string) error {
	_, err := tx.Exec(ctx,
		`insert into membership (id, organization_id, user_id, property_id, role) values ($1, $2, $3, $4, $5)`,
		ids.New(), orgID, userID, propertyID, role)
	return err
}
