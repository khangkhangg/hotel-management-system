package cancelrisk

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
)

// chance is the share of reservations at each level that is expected to cancel. These are rough rules of thumb that a
// hotel should check against its own history. They are only used to suggest, and nothing sells above the stock.
var chance = map[string]float64{"low": 0.05, "medium": 0.15, "high": 0.30}

// Hint says that, on a full night, enough cancellations are expected that the hotel could take a booking more.
type Hint struct {
	Date           string  `json:"date"`
	RoomType       string  `json:"roomType"`
	Rooms          int     `json:"rooms"`
	Booked         int     `json:"booked"`
	Expected       float64 `json:"expected"` // cancellations expected on the night
	SuggestedExtra int     `json:"suggestedExtra"`
}

// Hints looks at nights that are at least 80 percent full and adds up the expected cancellations. It suggests at most a tenth
// of the rooms of the type, and at least one when a whole cancellation is expected. The hotel decides what to do.
func Hints(ctx context.Context, tx pgx.Tx, prop uuid.UUID, today time.Time, days int) ([]Hint, error) {
	items, err := Upcoming(ctx, tx, prop, today, today.AddDate(0, 0, days))
	if err != nil {
		return nil, err
	}
	level := map[uuid.UUID]string{}
	for _, it := range items {
		level[it.ReservationID] = it.Level
	}
	type key struct {
		date string
		rt   uuid.UUID
	}
	expected := map[key]float64{}
	names := map[uuid.UUID]string{}
	rows, err := tx.Query(ctx, `select to_char(n.date, 'YYYY-MM-DD'), rr.room_type_id, t.name, r.id
		from reservation_night n join reservation_room rr on rr.id = n.reservation_room_id and rr.active
		join reservation r on r.id = rr.reservation_id join room_type t on t.id = rr.room_type_id
		where r.property_id = $1 and r.status in ('pending', 'confirmed') and n.date >= $2::date and n.date < $2::date + $3::int`, prop, today, days)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var date, name string
		var rt, res uuid.UUID
		if err := rows.Scan(&date, &rt, &name, &res); err != nil {
			rows.Close()
			return nil, err
		}
		names[rt] = name
		l := level[res]
		if l == "" {
			l = "low"
		}
		expected[key{date, rt}] += chance[l]
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	inv, err := tx.Query(ctx, `select to_char(i.date, 'YYYY-MM-DD'), i.room_type_id, i.total, i.sold + i.blocked
		from inventory i join room_type t on t.id = i.room_type_id and t.property_id = $1
		where i.date >= $2::date and i.date < $2::date + $3::int and i.total > 0 order by i.date, t.name`, prop, today, days)
	if err != nil {
		return nil, err
	}
	defer inv.Close()
	out := []Hint{}
	for inv.Next() {
		var date string
		var rt uuid.UUID
		var total, taken int
		if err := inv.Scan(&date, &rt, &total, &taken); err != nil {
			return nil, err
		}
		exp := expected[key{date, rt}]
		whole := int(math.Floor(exp + 1e-9))
		if taken*100 < total*80 || whole < 1 {
			continue
		}
		out = append(out, Hint{Date: date, RoomType: names[rt], Rooms: total, Booked: taken, Expected: math.Round(exp*100) / 100, SuggestedExtra: min(whole, max(1, total/10))})
	}
	return out, inv.Err()
}

func (h *handler) hints(w http.ResponseWriter, r *http.Request) {
	a := access.ActorFrom(r.Context())
	if a.PropertyID == uuid.Nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Choose a property first.")
		return
	}
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days < 1 || days > 365 {
		days = 30
	}
	var out []Hint
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		today, _, e := booking.HotelToday(r.Context(), tx, a.OrgID, a.PropertyID, h.Clock())
		if e != nil {
			return e
		}
		out, e = Hints(r.Context(), tx, a.PropertyID, today, days)
		return e
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
