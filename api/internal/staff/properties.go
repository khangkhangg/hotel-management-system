package staff

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
)

// properties lists the properties the signed-in person works at: every property of the organization for an owner,
// otherwise the ones they hold an active membership at. The app uses it to choose which property a screen is about.
func (h *handler) properties(w http.ResponseWriter, r *http.Request) {
	ctx, s := r.Context(), httpx.SessionFrom(r.Context())
	if s.OrgID == nil {
		httpx.Error(w, http.StatusUnauthorized, "organization_required", "Choose an organization to continue.")
		return
	}
	type row struct {
		ID       uuid.UUID `json:"id"`
		Name     string    `json:"name"`
		Currency string    `json:"currency"`
		Today    string    `json:"today"` // the date there right now: a hotel day is a date in the property's time zone
	}
	out := []row{}
	err := db.WithOrg(ctx, h.Pool, *s.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`select p.id, p.name, p.currency, to_char(($2::timestamptz at time zone p.time_zone)::date, 'YYYY-MM-DD') from property p
			 where exists (select 1 from membership m where m.user_id = $1 and m.organization_id = p.organization_id and m.status = 'active'
			               and (m.property_id is null or m.property_id = p.id))
			 order by p.name`, s.UserID, h.Clock())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p row
			if err := rows.Scan(&p.ID, &p.Name, &p.Currency, &p.Today); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
