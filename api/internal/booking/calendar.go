package booking

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/rooms"
)

// calendar returns what the calendar screen draws: the property's rooms, and every active stay that touches the range,
// with the room it holds (or none, for a booking that still needs a room number).
func (h *handler) calendar(w http.ResponseWriter, r *http.Request) {
	a, ok := property(w, r)
	if !ok {
		return
	}
	from, e1 := time.Parse(day, r.URL.Query().Get("from"))
	to, e2 := time.Parse(day, r.URL.Query().Get("to"))
	if e1 != nil || e2 != nil || !to.After(from) || to.Sub(from) > 62*24*time.Hour {
		bad(w, "bad_request", "Give a first date and a later last date within 62 days.")
		return
	}
	type roomRow struct {
		ID           uuid.UUID `json:"id"`
		Number       string    `json:"number"`
		Floor        int       `json:"floor"`
		Status       string    `json:"status"`
		RoomTypeID   uuid.UUID `json:"roomTypeId"`
		RoomTypeName string    `json:"roomTypeName"`
	}
	type stay struct {
		ID            uuid.UUID  `json:"id"`
		ReservationID uuid.UUID  `json:"reservationId"`
		Code          string     `json:"code"`
		Status        string     `json:"status"`
		GuestName     string     `json:"guestName"`
		RoomID        *uuid.UUID `json:"roomId"`
		RoomTypeID    uuid.UUID  `json:"roomTypeId"`
		RoomTypeName  string     `json:"roomTypeName"`
		From          string     `json:"from"`
		To            string     `json:"to"`
		Adults        int        `json:"adults"`
		Children      int        `json:"children"`
		Total         int64      `json:"total"`
		Currency      string     `json:"currency"`
		Source        string     `json:"source"`
	}
	out := struct {
		Rooms []roomRow `json:"rooms"`
		Stays []stay    `json:"stays"`
	}{Rooms: []roomRow{}, Stays: []stay{}}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(),
			`select rm.id, rm.number, rm.floor, rm.status, t.id, t.name from room rm join room_type t on t.id = rm.room_type_id where rm.property_id = $1 order by t.name, rm.number`, a.PropertyID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var x roomRow
			if err := rows.Scan(&x.ID, &x.Number, &x.Floor, &x.Status, &x.RoomTypeID, &x.RoomTypeName); err != nil {
				rows.Close()
				return err
			}
			out.Rooms = append(out.Rooms, x)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		st, err := tx.Query(r.Context(),
			`select rr.id, r.id, r.code, r.status, g.name, rr.room_id, t.id, t.name, to_char(lower(rr.stay), 'YYYY-MM-DD'), to_char(upper(rr.stay), 'YYYY-MM-DD'),
			        rr.adults, rr.children, r.total, r.currency, r.source_channel
			 from reservation_room rr join reservation r on r.id = rr.reservation_id join guest g on g.id = r.guest_id join room_type t on t.id = rr.room_type_id
			 where r.property_id = $1 and rr.active and rr.stay && daterange($2, $3, '[)') order by lower(rr.stay), r.code`, a.PropertyID, from, to)
		if err != nil {
			return err
		}
		defer st.Close()
		for st.Next() {
			var s stay
			if err := st.Scan(&s.ID, &s.ReservationID, &s.Code, &s.Status, &s.GuestName, &s.RoomID, &s.RoomTypeID, &s.RoomTypeName, &s.From, &s.To, &s.Adults, &s.Children, &s.Total, &s.Currency, &s.Source); err != nil {
				return err
			}
			out.Stays = append(out.Stays, s)
		}
		return st.Err()
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusOK, out)
	}
}

// quote prices a stay and says whether every night still has a room, so the booking form can show it before saving.
func (h *handler) quote(w http.ResponseWriter, r *http.Request) {
	a, ok := property(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	roomType, e1 := uuid.Parse(q.Get("roomTypeId"))
	plan, e2 := uuid.Parse(q.Get("ratePlanId"))
	from, e3 := time.Parse(day, q.Get("from"))
	to, e4 := time.Parse(day, q.Get("to"))
	adults, children := atoi(q.Get("adults")), atoi(q.Get("children"))
	nights := int(to.Sub(from).Hours() / 24)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || nights < 1 || nights > 60 {
		bad(w, "bad_request", "Give a room type, a rate plan and dates of 1 to 60 nights.")
		return
	}
	out := map[string]any{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var one int
		if err := tx.QueryRow(r.Context(), `select 1 from rate_plan where id = $1 and room_type_id = $2 and property_id = $3`, plan, roomType, a.PropertyID).Scan(&one); err != nil {
			return ErrNotFound
		}
		list, total, err := rooms.Price(r.Context(), tx, plan, from, to, adults, children)
		if err != nil {
			return err
		}
		var free int
		if err := tx.QueryRow(r.Context(), `select count(*) from inventory where room_type_id = $1 and date >= $2 and date < $3 and sold + blocked < total`, roomType, from, to).Scan(&free); err != nil {
			return err
		}
		_, currency, err := HotelToday(r.Context(), tx, a.OrgID, a.PropertyID, h.Clock())
		if err != nil {
			return err
		}
		prices := make([]int64, len(list))
		for i, n := range list {
			prices[i] = n.Price
		}
		out = map[string]any{"nights": len(list), "total": total, "currency": currency, "prices": prices, "available": free == nights}
		return nil
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusOK, out)
	}
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' || n > 1000 {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}
