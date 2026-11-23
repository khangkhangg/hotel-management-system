package access

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// OrgWide says whether the person has a membership that covers the whole organization, such as an owner.
// The web app names the property it is on in every request, so a role at that property is not enough to see across hotels.
func OrgWide(ctx context.Context, tx pgx.Tx, a Actor) (bool, error) {
	var wide bool
	err := tx.QueryRow(ctx, `select exists (select 1 from membership where user_id = $1 and organization_id = $2 and status = 'active' and property_id is null)`, a.UserID, a.OrgID).Scan(&wide)
	return wide, err
}
