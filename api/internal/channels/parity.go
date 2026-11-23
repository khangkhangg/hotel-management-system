package channels

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/yieldrules"
)

// slotMeta is what the engine knew about a night before the yield rules changed it.
type slotMeta struct {
	roomType uuid.UUID
	direct   int64 // the price on the hotel's own booking page
	avail    int   // the rooms left before any yield rule
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

// PreviewRow is a night that a rule being tried out would change, with the state of that night now and with the rule.
type PreviewRow struct {
	Date            string `json:"date"`
	RoomType        string `json:"roomType"`
	AvailableBefore int    `json:"availableBefore"`
	AvailableAfter  int    `json:"availableAfter"`
	PriceBefore     int64  `json:"priceBefore"`
	PriceAfter      int64  `json:"priceAfter"`
}

// Preview says which nights the candidate rule would change on top of the rules already in force. It saves nothing.
func Preview(ctx context.Context, tx pgx.Tx, org, prop uuid.UUID, now time.Time, candidate yieldrules.Rule) ([]PreviewRow, error) {
	var before, after loaded
	after.extra = []yieldrules.Rule{candidate}
	for _, l := range []*loaded{&before, &after} {
		if err := load(ctx, tx, org, prop, now, l); err != nil {
			if errors.Is(err, ErrOff) || errors.Is(err, pgx.ErrNoRows) {
				return []PreviewRow{}, nil
			}
			return nil, err
		}
	}
	names := map[uuid.UUID]string{}
	rows, err := tx.Query(ctx, `select id, name from room_type where property_id = $1`, prop)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		names[id] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []PreviewRow{}
	for i, b := range before.slots {
		a := after.slots[i]
		if a.Available == b.Available && a.Price == b.Price {
			continue
		}
		out = append(out, PreviewRow{Date: b.Date, RoomType: names[before.meta[i].roomType], AvailableBefore: b.Available, AvailableAfter: a.Available, PriceBefore: b.Price, PriceAfter: a.Price})
	}
	return out, nil
}

func (h *handler) preview(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var rule yieldrules.Rule
	d := json.NewDecoder(io.LimitReader(r.Body, 8<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(&rule); err != nil || !rule.Valid() {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Check the rule and try again.")
		return
	}
	var out []PreviewRow
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var e error
		out, e = Preview(r.Context(), tx, a.OrgID, a.PropertyID, h.Clock(), rule)
		return e
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
