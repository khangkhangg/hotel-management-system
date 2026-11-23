package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/account"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/ids"
	"hotelpms/api/internal/rooms"
)

// seedHotel gives the sample hotel two room types, six rooms, prices for the next 200 days and two bookings,
// dated from today so the browser tests always find them on the calendar.
func seedHotel(ctx context.Context, pool *pgxpool.Pool, org, property uuid.UUID) {
	must := func(err error) {
		if err != nil {
			log.Fatal(err)
		}
	}
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	var owner uuid.UUID
	must(db.WithoutOrg(ctx, pool, func(tx pgx.Tx) error {
		u, err := account.ByEmail(ctx, tx, "owner@hotel.com")
		owner = u.ID
		return err
	}))
	who := audit.WithWho(ctx, audit.Who{UserID: owner, OrgID: org, PropertyID: property, Role: "owner"})

	type plan struct{ typeID, planID uuid.UUID }
	var plans []plan
	must(db.WithOrg(ctx, pool, org, func(tx pgx.Tx) error {
		for _, t := range []struct {
			name    string
			base    int
			max     int
			price   int64
			numbers []string
			floor   int
		}{
			{"Deluxe", 2, 3, 12000, []string{"101", "102", "103", "104"}, 1},
			{"Suite", 2, 4, 30000, []string{"201", "202"}, 2},
		} {
			typeID, planID := ids.New(), ids.New()
			if _, err := tx.Exec(ctx, `insert into room_type (id, organization_id, property_id, name, base_guests, max_guests, extra_adult_price, extra_child_price, created_at) values ($1, $2, $3, $4, $5, $6, 2000, 1000, $7)`,
				typeID, org, property, t.name, t.base, t.max, now); err != nil {
				return err
			}
			for _, n := range t.numbers {
				if _, err := tx.Exec(ctx, `insert into room (id, organization_id, property_id, room_type_id, number, floor, created_at) values ($1, $2, $3, $4, $5, $6, $7)`, ids.New(), org, property, typeID, n, t.floor, now); err != nil {
					return err
				}
			}
			if err := rooms.EnsureInventory(ctx, tx, org, typeID, today); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `insert into payment_type (property_id, kind, organization_id, enabled, bank_details) select $1, k, $2, true, 'IBAN VN00 0000' from unnest(array['hotel', 'bank_transfer', 'card']) k on conflict do nothing`, property, org); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `insert into rate_plan (id, organization_id, property_id, room_type_id, name, free_cancel_until_days, late_cancel_penalty, created_at) values ($1, $2, $3, $4, 'Flexible', 2, 'first_night', $5)`, planID, org, property, typeID, now); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`insert into rate (rate_plan_id, date, organization_id, price) select $1, d::date, $2, $3 from generate_series($4::date, $4::date + 200, interval '1 day') d`, planID, org, t.price, today); err != nil {
				return err
			}
			plans = append(plans, plan{typeID, planID})
		}
		return nil
	}))
	book := func(name, email string, from, to time.Time) {
		must(db.WithOrg(who, pool, org, func(tx pgx.Tx) error {
			_, err := booking.Create(who, tx, org, property, booking.CreateInput{
				Guest: booking.GuestInput{Name: name, Email: email, Phone: "+84 90 000 0000"},
				Rooms: []booking.RoomRequest{{RoomTypeID: plans[0].typeID, RatePlanID: plans[0].planID, From: from.Format("2006-01-02"), To: to.Format("2006-01-02"), Adults: 2}},
			}, now)
			return err
		}))
	}
	book("Ana Reyes", "ana@example.com", today, today.AddDate(0, 0, 2))
	book("Ben Carter", "ben@example.com", today.AddDate(0, 0, 3), today.AddDate(0, 0, 5))
	fmt.Println("seeded the sample hotel")
}
