// Package booking holds guests and reservations, and keeps inventory right while they are made and cancelled (spec 03).
package booking

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/ids"
	"hotelpms/api/internal/rooms"
)

var (
	ErrSoldOut          = errors.New("booking: a night is full")
	ErrStayRule         = errors.New("booking: the stay breaks a rate rule")
	ErrPast             = errors.New("booking: the stay starts in the past")
	ErrNotFound         = errors.New("booking: not found")
	ErrBad              = errors.New("booking: bad input")
	ErrAlreadyCancelled = errors.New("booking: already cancelled")
	ErrCannotCancel     = errors.New("booking: this booking cannot be cancelled now")
)

const day = "2006-01-02"

type GuestInput struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Country string `json:"country"`
	Locale  string `json:"locale"`
}

type RoomRequest struct {
	RoomTypeID uuid.UUID `json:"roomTypeId"`
	RatePlanID uuid.UUID `json:"ratePlanId"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	Adults     int       `json:"adults"`
	Children   int       `json:"children"`
}

type CreateInput struct {
	Guest  GuestInput    `json:"guest"`
	Rooms  []RoomRequest `json:"rooms"`
	Source string        `json:"source"`
}

type Reservation struct {
	ID       uuid.UUID `json:"id"`
	Code     string    `json:"code"`
	Total    int64     `json:"total"`
	Currency string    `json:"currency"`
}

// NormalizeEmail lowercases and trims an address, so one person is one guest however it is typed.
func NormalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// HotelToday is the date now in the property's time zone.
func HotelToday(ctx context.Context, tx pgx.Tx, orgID, propertyID uuid.UUID, now time.Time) (today time.Time, currency string, err error) {
	var zone string
	if err = tx.QueryRow(ctx, `select time_zone, currency from property where id = $1`, propertyID).Scan(&zone, &currency); err != nil {
		return
	}
	loc, lerr := time.LoadLocation(zone)
	if lerr != nil {
		loc = time.UTC
	}
	n := now.In(loc)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC), currency, nil
}

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no letters that look like digits

func newCode() string {
	id := ids.New()
	b := make([]byte, 8)
	for i := range b {
		b[i] = codeAlphabet[int(id[8+i%8]^id[i])%len(codeAlphabet)]
	}
	return string(b)
}

type stay struct {
	req      RoomRequest
	from, to time.Time
	nights   []rooms.Night
	total    int64
}

// Create makes a reservation for the guest in one transaction: price each room, check the rate rules, take the
// rooms out of inventory (locked in a fixed order, so two bookings cannot deadlock), then store everything.
// Run it inside db.WithOrg for orgID. Any error rolls the whole booking back.
func Create(ctx context.Context, tx pgx.Tx, orgID, propertyID uuid.UUID, in CreateInput, now time.Time) (Reservation, error) {
	in.Guest.Name = strings.TrimSpace(in.Guest.Name)
	if in.Guest.Name == "" || len(in.Guest.Name) > 200 || len(in.Rooms) < 1 || len(in.Rooms) > 10 {
		return Reservation{}, ErrBad
	}
	today, currency, err := HotelToday(ctx, tx, orgID, propertyID, now)
	if err != nil {
		return Reservation{}, err
	}
	stays := make([]stay, len(in.Rooms))
	for i, r := range in.Rooms {
		s := stay{req: r}
		var e1, e2 error
		s.from, e1 = time.Parse(day, r.From)
		s.to, e2 = time.Parse(day, r.To)
		nights := int(s.to.Sub(s.from).Hours() / 24)
		if e1 != nil || e2 != nil || nights < 1 || nights > 60 || r.Children < 0 {
			return Reservation{}, ErrBad
		}
		if s.from.Before(today) {
			return Reservation{}, ErrPast
		}
		var one int
		if err := tx.QueryRow(ctx, `select 1 from rate_plan where id = $1 and room_type_id = $2 and property_id = $3`, r.RatePlanID, r.RoomTypeID, propertyID).Scan(&one); err != nil {
			return Reservation{}, ErrNotFound
		}
		if s.nights, s.total, err = rooms.Price(ctx, tx, r.RatePlanID, s.from, s.to, r.Adults, r.Children); err != nil {
			return Reservation{}, err
		}
		if err := checkRules(ctx, tx, r.RatePlanID, s.from, s.to); err != nil {
			return Reservation{}, err
		}
		stays[i] = s
	}

	// Take the rooms out of inventory in a fixed order (room type, then date).
	order := append([]stay(nil), stays...)
	sort.Slice(order, func(i, j int) bool {
		if order[i].req.RoomTypeID != order[j].req.RoomTypeID {
			return order[i].req.RoomTypeID.String() < order[j].req.RoomTypeID.String()
		}
		return order[i].from.Before(order[j].from)
	})
	for _, s := range order {
		tag, err := tx.Exec(ctx,
			`with locked as (select date from inventory where room_type_id = $1 and date >= $2 and date < $3 order by date for update)
			 update inventory i set sold = sold + 1 from locked l
			 where i.room_type_id = $1 and i.date = l.date and i.sold + i.blocked < i.total`, s.req.RoomTypeID, s.from, s.to)
		if err != nil {
			return Reservation{}, err
		}
		if int(tag.RowsAffected()) != len(s.nights) {
			return Reservation{}, ErrSoldOut // rolled back by the caller, which also gives back the nights taken so far
		}
	}

	guestID, err := upsertGuest(ctx, tx, orgID, in.Guest, now)
	if err != nil {
		return Reservation{}, err
	}
	var total int64
	for _, s := range stays {
		total += s.total
	}
	source := in.Source
	if source == "" {
		source = "direct"
	}
	res := Reservation{ID: ids.New(), Total: total, Currency: currency}
	for attempt := 0; ; attempt++ {
		res.Code = newCode()
		_, err = tx.Exec(ctx, `savepoint code`)
		if err == nil {
			_, err = tx.Exec(ctx,
				`insert into reservation (id, organization_id, property_id, code, guest_id, source_channel, total, currency, created_at) values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
				res.ID, orgID, propertyID, res.Code, guestID, source, total, currency, now)
		}
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" && attempt < 5 {
			if _, rerr := tx.Exec(ctx, `rollback to savepoint code`); rerr != nil {
				return Reservation{}, rerr
			}
			continue
		}
		if err != nil {
			return Reservation{}, err
		}
		break
	}
	for _, s := range stays {
		roomID := ids.New()
		if _, err := tx.Exec(ctx,
			`insert into reservation_room (id, organization_id, reservation_id, room_type_id, rate_plan_id, stay, adults, children)
			 values ($1, $2, $3, $4, $5, daterange($6, $7, '[)'), $8, $9)`,
			roomID, orgID, res.ID, s.req.RoomTypeID, s.req.RatePlanID, s.from, s.to, s.req.Adults, s.req.Children); err != nil {
			return Reservation{}, err
		}
		for _, n := range s.nights {
			if _, err := tx.Exec(ctx, `insert into reservation_night (reservation_room_id, date, organization_id, price) values ($1, $2, $3, $4)`, roomID, n.Date, orgID, n.Price); err != nil {
				return Reservation{}, err
			}
		}
	}
	return res, audit.Record(ctx, tx, audit.Entry{Action: "reservation.created", TargetType: "reservation", TargetID: res.ID, TargetLabel: res.Code,
		After: map[string]any{"total": total, "currency": currency, "rooms": len(stays), "source": source}})
}

// checkRules applies the rate plan's sales rules to a stay: a closed night, minimum nights, closed to arrival
// and closed to departure.
func checkRules(ctx context.Context, tx pgx.Tx, ratePlanID uuid.UUID, from, to time.Time) error {
	rows, err := tx.Query(ctx, `select date, min_nights, closed, closed_to_arrival, closed_to_departure from rate where rate_plan_id = $1 and date >= $2 and date <= $3`, ratePlanID, from, to)
	if err != nil {
		return err
	}
	defer rows.Close()
	nights := int(to.Sub(from).Hours() / 24)
	for rows.Next() {
		var d time.Time
		var minNights int
		var closed, cta, ctd bool
		if err := rows.Scan(&d, &minNights, &closed, &cta, &ctd); err != nil {
			return err
		}
		switch {
		case d.Before(to) && closed:
			return ErrStayRule
		case d.Equal(from) && (cta || nights < minNights):
			return ErrStayRule
		case d.Equal(to) && ctd:
			return ErrStayRule
		}
	}
	return rows.Err()
}

func upsertGuest(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, g GuestInput, now time.Time) (uuid.UUID, error) {
	email := NormalizeEmail(g.Email)
	locale := g.Locale
	if locale == "" {
		locale = "en"
	}
	id := ids.New()
	if email == "" {
		_, err := tx.Exec(ctx, `insert into guest (id, organization_id, name, phone, country, locale, created_at) values ($1, $2, $3, $4, $5, $6, $7)`,
			id, orgID, g.Name, g.Phone, g.Country, locale, now)
		return id, err
	}
	err := tx.QueryRow(ctx,
		`insert into guest (id, organization_id, name, email, phone, country, locale, created_at) values ($1, $2, $3, $4, $5, $6, $7, $8)
		 on conflict (organization_id, email) where email is not null
		 do update set phone = coalesce(nullif(excluded.phone, ''), guest.phone), country = coalesce(nullif(excluded.country, ''), guest.country)
		 returning id`, id, orgID, g.Name, email, g.Phone, g.Country, locale, now).Scan(&id)
	return id, err
}

// Cancel gives the rooms back to inventory and marks the reservation cancelled. A second cancel changes nothing.
func Cancel(ctx context.Context, tx pgx.Tx, orgID, propertyID, id uuid.UUID, now time.Time) error {
	var status, code string
	if err := tx.QueryRow(ctx, `select status, code from reservation where id = $1 and property_id = $2 for update`, id, propertyID).Scan(&status, &code); err != nil {
		return ErrNotFound
	}
	switch status {
	case "cancelled":
		return ErrAlreadyCancelled
	case "checked_in", "checked_out", "no_show":
		return ErrCannotCancel
	}
	if _, err := tx.Exec(ctx,
		`with active as (select room_type_id, stay from reservation_room where reservation_id = $1 and active)
		 update inventory i set sold = sold - 1 from active a
		 where i.room_type_id = a.room_type_id and i.date >= lower(a.stay) and i.date < upper(a.stay)`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `update reservation_room set active = false where reservation_id = $1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `update reservation set status = 'cancelled', cancelled_at = $2 where id = $1`, id, now); err != nil {
		return err
	}
	return audit.Record(ctx, tx, audit.Entry{Action: "reservation.cancelled", TargetType: "reservation", TargetID: id, TargetLabel: code,
		Before: map[string]string{"status": status}, After: map[string]string{"status": "cancelled"}})
}
