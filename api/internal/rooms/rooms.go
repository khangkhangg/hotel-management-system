// Package rooms holds a property's room types, rooms, rate plans, rates and the inventory per night (spec 03).
package rooms

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrNoRate        = errors.New("rooms: a night has no rate")
	ErrTooManyGuests = errors.New("rooms: more guests than the room holds")
	ErrNoAdult       = errors.New("rooms: a room needs at least one adult")
)

// Night is one night's price.
type Night struct {
	Date  time.Time
	Price int64
}

// Price returns the price of each night from (inclusive) to (exclusive) and their total, for the guests given.
// A room price covers up to the room type's base guests; each guest above that adds the room type's extra price
// per night, up to its maximum. Adults fill the base places first.
func Price(ctx context.Context, tx pgx.Tx, ratePlanID uuid.UUID, from, to time.Time, adults, children int) ([]Night, int64, error) {
	var base, max int
	var extraAdult, extraChild int64
	err := tx.QueryRow(ctx,
		`select t.base_guests, t.max_guests, t.extra_adult_price, t.extra_child_price
		 from rate_plan p join room_type t on t.id = p.room_type_id where p.id = $1`, ratePlanID).Scan(&base, &max, &extraAdult, &extraChild)
	if err != nil {
		return nil, 0, err
	}
	if adults < 1 {
		return nil, 0, ErrNoAdult
	}
	if adults+children > max {
		return nil, 0, ErrTooManyGuests
	}
	extraAdults := max0(adults - base)
	extraChildren := max0(children - max0(base-adults))
	extra := int64(extraAdults)*extraAdult + int64(extraChildren)*extraChild

	rows, err := tx.Query(ctx, `select date, price from rate where rate_plan_id = $1 and date >= $2 and date < $3 order by date`, ratePlanID, from, to)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var nights []Night
	var total int64
	for rows.Next() {
		var n Night
		if err := rows.Scan(&n.Date, &n.Price); err != nil {
			return nil, 0, err
		}
		n.Price += extra
		total += n.Price
		nights = append(nights, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if want := int(to.Sub(from).Hours() / 24); len(nights) != want || want < 1 {
		return nil, 0, ErrNoRate
	}
	return nights, total, nil
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// EnsureInventory makes sure each night from today for a year has an inventory row for the room type, and that
// each one counts the rooms the type has now.
func EnsureInventory(ctx context.Context, tx pgx.Tx, orgID, roomTypeID uuid.UUID, today time.Time) error {
	if _, err := tx.Exec(ctx,
		`insert into inventory (organization_id, room_type_id, date, total)
		 select $1, $2, d::date, 0 from generate_series($3::date, $3::date + 400, interval '1 day') d
		 on conflict do nothing`, orgID, roomTypeID, today); err != nil {
		return err
	}
	_, err := tx.Exec(ctx,
		`update inventory set total = (select count(*) from room where room_type_id = $1 and status <> 'out_of_order')
		 where room_type_id = $1 and date >= $2`, roomTypeID, today)
	return err
}
