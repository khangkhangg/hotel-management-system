package channels

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
)

// slotMeta is what the engine knew about a night before the yield rules changed it.
type slotMeta struct {
	roomType uuid.UUID
	direct   int64 // the price on the hotel's own booking page
}

// ParityRow is a night where the booking sites do not show the price of the hotel's own booking page.
type ParityRow struct {
	Date     string `json:"date"`
	RoomType string `json:"roomType"`
	Direct   int64  `json:"direct"`
	Channel  int64  `json:"channel"`
	Status   string `json:"status"` // channel_lower: the site undercuts the booking page, channel_higher: the page is cheaper
}

// Parity compares what the channel would be sent with the price of the hotel's own booking page, for the pushed nights.
// A hotel with no switched-on connection has nothing to compare.
func Parity(ctx context.Context, tx pgx.Tx, org, prop uuid.UUID, now time.Time) ([]ParityRow, error) {
	var l loaded
	if err := load(ctx, tx, org, prop, now, &l); err != nil {
		if errors.Is(err, ErrOff) || errors.Is(err, pgx.ErrNoRows) {
			return []ParityRow{}, nil
		}
		return nil, err
	}
	names := map[uuid.UUID]string{}
	rows, err := tx.Query(ctx, `select id, name from room_type where property_id = $1`, prop)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		names[id] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []ParityRow{}
	for i, s := range l.slots {
		m := l.meta[i]
		if s.Price == m.direct {
			continue
		}
		status := "channel_higher"
		if s.Price < m.direct {
			status = "channel_lower"
		}
		out = append(out, ParityRow{Date: s.Date, RoomType: names[m.roomType], Direct: m.direct, Channel: s.Price, Status: status})
	}
	return out, nil
}

func (h *handler) parity(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var out []ParityRow
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var e error
		out, e = Parity(r.Context(), tx, a.OrgID, a.PropertyID, h.Clock())
		return e
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
