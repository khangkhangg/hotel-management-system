package alerts

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/folio"
)

// registry lists every check of this milestone. Checks that need channels or booking history come with later milestones.
var registry = []check{
	{kind: "unassigned_arrival", role: access.FrontDesk, find: unassignedArrival},
	{kind: "room_not_ready", role: access.FrontDesk, defaults: map[string]float64{"checkInHour": 14, "leadHours": 2}, find: roomNotReady},
	{kind: "unpaid_arrival", role: access.FrontDesk, find: unpaidArrival},
	{kind: "unpaid_departure", role: access.FrontDesk, find: unpaidDeparture},
	{kind: "late_checkout", role: access.FrontDesk, defaults: map[string]float64{"checkOutHour": 11, "graceHours": 1}, find: lateCheckout},
	{kind: "likely_no_show", role: access.FrontDesk, defaults: map[string]float64{"fromHour": 22, "cutoffHour": 23}, find: likelyNoShow},
	{kind: "out_of_order_clash", role: access.Manager, find: outOfOrderClash},
	{kind: "repeat_issue", role: access.Manager, defaults: map[string]float64{"tickets": 3, "days": 30}, find: repeatIssue},
	{kind: "returning_guest", role: access.FrontDesk, find: returningGuest},
	{kind: "refund_waiting", role: access.Manager, find: refundWaiting},
	{kind: "paid_late", role: access.Manager, find: paidLate},
	{kind: "booking_review", role: access.Manager, find: bookingReview},
	{kind: "channel_trouble", role: access.Manager, defaults: map[string]float64{"staleHours": 3}, find: channelTrouble},
	{kind: "tag_not_answered", role: access.FrontDesk, defaults: map[string]float64{"hours": 4}, find: tagNotAnswered},
}

// Kinds names every check, for the settings screen.
func Kinds() []string {
	out := make([]string, len(registry))
	for i, c := range registry {
		out[i] = c.kind
	}
	return out
}

// zeroDecimal lists the currencies whose smallest unit is the whole unit. The web uses the same list.
var zeroDecimal = map[string]bool{"VND": true, "JPY": true, "KRW": true, "CLP": true, "IDR": true}

func money(amount int64, currency string) string {
	if zeroDecimal[currency] {
		return fmt.Sprintf("%d %s", amount, currency)
	}
	return fmt.Sprintf("%d.%02d %s", amount/100, amount%100, currency)
}

func clock(hour float64) string {
	return fmt.Sprintf("%02d:%02d", int(hour), int((hour-float64(int(hour)))*60))
}

type arrival struct {
	id, roomID, roomType uuid.UUID
	code, guest, status  string
	roomNumber           *string
	roomStatus           *string
}

// arrivals returns the bookings arriving today with the given status, one row per reservation.
func arrivals(ctx context.Context, tx pgx.Tx, sc scope, status string) ([]arrival, error) {
	rows, err := tx.Query(ctx,
		`select r.id, r.code, g.name, r.status, (array_agg(rr.room_type_id order by rr.room_id is not null, rr.id))[1], (array_agg(rr.room_id order by rr.id))[1],
		        (array_agg(rm.number order by rr.id))[1], (array_agg(rm.status order by rr.id))[1],
		        bool_or(rr.room_id is null)
		 from reservation r join guest g on g.id = r.guest_id
		 join reservation_room rr on rr.reservation_id = r.id and rr.active
		 left join room rm on rm.id = rr.room_id
		 where r.property_id = $1 and r.status = $2 and lower(rr.stay) = $3
		 group by r.id, g.name order by r.code`, sc.propertyID, status, sc.today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []arrival
	for rows.Next() {
		var a arrival
		var typ uuid.UUID
		var room *uuid.UUID
		var anyUnassigned bool
		if err := rows.Scan(&a.id, &a.code, &a.guest, &a.status, &typ, &room, &a.roomNumber, &a.roomStatus, &anyUnassigned); err != nil {
			return nil, err
		}
		a.roomType = typ
		if room != nil {
			a.roomID = *room
		}
		if anyUnassigned {
			a.roomID = uuid.Nil
			a.roomNumber, a.roomStatus = nil, nil
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// freeRoom finds the best room of the booked type for the stay: clean first, nothing else holding those nights.
func freeRoom(ctx context.Context, tx pgx.Tx, sc scope, reservationID, roomType uuid.UUID) (id uuid.UUID, number string, ok bool, err error) {
	err = tx.QueryRow(ctx,
		`select rm.id, rm.number from room rm, (select stay from reservation_room where reservation_id = $2 and active order by id limit 1) s
		 where rm.property_id = $1 and rm.room_type_id = $3 and rm.status in ('clean', 'inspect')
		   and not exists (select 1 from reservation_room x where x.room_id = rm.id and x.active and x.stay && s.stay)
		   and not exists (select 1 from maintenance_ticket t where t.room_id = rm.id and t.status <> 'done' and t.blocks_from is not null and daterange(t.blocks_from, t.blocks_to, '[)') && s.stay)
		 order by rm.status = 'clean' desc, rm.number limit 1`, sc.propertyID, reservationID, roomType).Scan(&id, &number)
	if err == pgx.ErrNoRows {
		return uuid.Nil, "", false, nil
	}
	return id, number, err == nil, err
}

func assignAction(ctx context.Context, tx pgx.Tx, sc scope, a arrival) (*Action, error) {
	id, number, ok, err := freeRoom(ctx, tx, sc, a.id, a.roomType)
	if err != nil || !ok {
		return nil, err
	}
	return &Action{Kind: "assign_room", Label: "Assign room " + number,
		Changes: []string{fmt.Sprintf("Booking %s gets room %s", a.code, number)},
		Params:  map[string]any{"reservationId": a.id, "roomId": id, "roomNumber": number}}, nil
}

func unassignedArrival(ctx context.Context, tx pgx.Tx, sc scope) ([]Finding, error) {
	list, err := arrivals(ctx, tx, sc, "confirmed")
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, a := range list {
		if a.roomID != uuid.Nil {
			continue
		}
		act, err := assignAction(ctx, tx, sc, a)
		if err != nil {
			return nil, err
		}
		out = append(out, Finding{SubjectType: "reservation", Subject: a.id, Severity: "warning",
			Text: fmt.Sprintf("%s arrives today and has no room number.", a.guest), Facts: map[string]any{"code": a.code}, Action: act})
	}
	return out, nil
}

func roomNotReady(ctx context.Context, tx pgx.Tx, sc scope) ([]Finding, error) {
	if sc.hour < sc.th["checkInHour"]-sc.th["leadHours"] {
		return nil, nil
	}
	list, err := arrivals(ctx, tx, sc, "confirmed")
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, a := range list {
		if a.roomID == uuid.Nil || a.roomStatus == nil || *a.roomStatus == "clean" {
			continue
		}
		act, err := assignAction(ctx, tx, sc, a)
		if err != nil {
			return nil, err
		}
		if act != nil {
			act.Label = "Swap to room " + act.Params["roomNumber"].(string)
		}
		out = append(out, Finding{SubjectType: "reservation", Subject: a.id, Severity: "warning",
			Text:  fmt.Sprintf("%s arrives at %s and room %s is %s.", a.guest, clock(sc.th["checkInHour"]), *a.roomNumber, *a.roomStatus),
			Facts: map[string]any{"room": *a.roomNumber, "roomStatus": *a.roomStatus, "checkInHour": sc.th["checkInHour"]}, Action: act})
	}
	return out, nil
}

func openAction(label, path string) *Action {
	return &Action{Kind: "open", Label: label, Params: map[string]any{"path": path}}
}

func unpaidArrival(ctx context.Context, tx pgx.Tx, sc scope) ([]Finding, error) {
	list, err := arrivals(ctx, tx, sc, "confirmed")
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, a := range list {
		balance, cur, err := folio.Balance(ctx, tx, sc.orgID, sc.propertyID, a.id, sc.now)
		if err != nil {
			return nil, err
		}
		if balance > 0 {
			out = append(out, Finding{SubjectType: "reservation", Subject: a.id, Severity: "warning",
				Text: fmt.Sprintf("%s arrives today with %s still to pay.", a.guest, money(balance, cur)), Facts: map[string]any{"balance": balance, "currency": cur},
				Action: openAction("Take payment at check-in", "/arrivals")})
		}
	}
	return out, nil
}

// departures returns checked-in bookings whose last night ends today.
func departures(ctx context.Context, tx pgx.Tx, sc scope) ([]arrival, error) {
	rows, err := tx.Query(ctx,
		`select r.id, r.code, g.name, (array_agg(rr.room_id order by rr.id))[1], (array_agg(rm.number order by rr.id))[1]
		 from reservation r join guest g on g.id = r.guest_id
		 join reservation_room rr on rr.reservation_id = r.id and rr.active left join room rm on rm.id = rr.room_id
		 where r.property_id = $1 and r.status = 'checked_in' group by r.id, g.name having max(upper(rr.stay)) = $2 order by r.code`, sc.propertyID, sc.today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []arrival
	for rows.Next() {
		var a arrival
		var room *uuid.UUID
		if err := rows.Scan(&a.id, &a.code, &a.guest, &room, &a.roomNumber); err != nil {
			return nil, err
		}
		if room != nil {
			a.roomID = *room
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func unpaidDeparture(ctx context.Context, tx pgx.Tx, sc scope) ([]Finding, error) {
	list, err := departures(ctx, tx, sc)
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, a := range list {
		balance, cur, err := folio.Balance(ctx, tx, sc.orgID, sc.propertyID, a.id, sc.now)
		if err != nil {
			return nil, err
		}
		if balance > 0 {
			out = append(out, Finding{SubjectType: "reservation", Subject: a.id, Severity: "warning",
				Text: fmt.Sprintf("%s checks out today with %s unpaid.", a.guest, money(balance, cur)), Facts: map[string]any{"balance": balance, "currency": cur},
				Action: openAction("Take payment before checkout", "/arrivals")})
		}
	}
	return out, nil
}

func lateCheckout(ctx context.Context, tx pgx.Tx, sc scope) ([]Finding, error) {
	if sc.hour < sc.th["checkOutHour"]+sc.th["graceHours"] {
		return nil, nil
	}
	list, err := departures(ctx, tx, sc)
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, a := range list {
		if a.roomID == uuid.Nil {
			continue
		}
		var next int
		if err := tx.QueryRow(ctx, `select count(*) from reservation_room x where x.room_id = $1 and x.active and lower(x.stay) = $2 and x.reservation_id <> $3`, a.roomID, sc.today, a.id).Scan(&next); err != nil {
			return nil, err
		}
		if next > 0 {
			out = append(out, Finding{SubjectType: "reservation", Subject: a.id, Severity: "warning",
				Text:  fmt.Sprintf("%s is still in room %s after %s, and another guest arrives there today.", a.guest, *a.roomNumber, clock(sc.th["checkOutHour"])),
				Facts: map[string]any{"room": *a.roomNumber}, Action: openAction("Call the room or charge late checkout", "/arrivals")})
		}
	}
	return out, nil
}

func likelyNoShow(ctx context.Context, tx pgx.Tx, sc scope) ([]Finding, error) {
	if sc.hour < sc.th["fromHour"] {
		return nil, nil
	}
	list, err := arrivals(ctx, tx, sc, "confirmed")
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, a := range list {
		balance, _, err := folio.Balance(ctx, tx, sc.orgID, sc.propertyID, a.id, sc.now)
		if err != nil {
			return nil, err
		}
		if balance <= 0 {
			continue // a guest who has paid is not a likely no-show
		}
		act := openAction("Contact the guest", "/arrivals")
		if sc.hour >= sc.th["cutoffHour"] {
			act = &Action{Kind: "no_show", Label: "Mark as no-show", Changes: []string{fmt.Sprintf("Booking %s becomes a no-show and its room goes back on sale", a.code)},
				Params: map[string]any{"reservationId": a.id}}
		}
		out = append(out, Finding{SubjectType: "reservation", Subject: a.id, Severity: "info",
			Text: fmt.Sprintf("%s has not arrived by %s and has not paid.", a.guest, clock(sc.th["fromHour"])), Facts: map[string]any{"code": a.code}, Action: act})
	}
	return out, nil
}

func outOfOrderClash(ctx context.Context, tx pgx.Tx, sc scope) ([]Finding, error) {
	rows, err := tx.Query(ctx,
		`select t.id, rm.number, r.code, g.name, to_char(t.blocks_from, 'YYYY-MM-DD'), to_char(t.blocks_to, 'YYYY-MM-DD')
		 from maintenance_ticket t join room rm on rm.id = t.room_id
		 join reservation_room rr on rr.room_id = t.room_id and rr.active and rr.stay && daterange(t.blocks_from, t.blocks_to, '[)')
		 join reservation r on r.id = rr.reservation_id join guest g on g.id = r.guest_id
		 where t.property_id = $1 and t.status <> 'done' and t.blocks_from is not null and r.status in ('confirmed', 'checked_in')
		 order by t.id, r.code`, sc.propertyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[uuid.UUID]bool{}
	var out []Finding
	for rows.Next() {
		var id uuid.UUID
		var room, code, guest, from, to string
		if err := rows.Scan(&id, &room, &code, &guest, &from, &to); err != nil {
			return nil, err
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, Finding{SubjectType: "maintenance_ticket", Subject: id, Severity: "urgent",
			Text:  fmt.Sprintf("Room %s is out of order from %s to %s, but %s's booking %s holds it.", room, from, to, guest, code),
			Facts: map[string]any{"room": room, "from": from, "to": to, "code": code}, Action: openAction("Move the booking or change the block", "/issues")})
	}
	return out, rows.Err()
}

func repeatIssue(ctx context.Context, tx pgx.Tx, sc scope) ([]Finding, error) {
	rows, err := tx.Query(ctx,
		`select rm.id, rm.number, count(*) from maintenance_ticket t join room rm on rm.id = t.room_id
		 where t.property_id = $1 and t.created_at >= $2 group by rm.id having count(*) >= $3 order by rm.number`,
		sc.propertyID, sc.now.Add(-time.Duration(sc.th["days"])*24*time.Hour), int(sc.th["tickets"]))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Finding
	for rows.Next() {
		var id uuid.UUID
		var number string
		var n int
		if err := rows.Scan(&id, &number, &n); err != nil {
			return nil, err
		}
		out = append(out, Finding{SubjectType: "room", Subject: id, Severity: "info",
			Text: fmt.Sprintf("Room %s has had %d maintenance tickets in %d days.", number, n, int(sc.th["days"])), Facts: map[string]any{"room": number, "tickets": n},
			Action: openAction("Open the room history", "/issues")})
	}
	return out, rows.Err()
}

func returningGuest(ctx context.Context, tx pgx.Tx, sc scope) ([]Finding, error) {
	list, err := arrivals(ctx, tx, sc, "confirmed")
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, a := range list {
		var last *string
		if err := tx.QueryRow(ctx,
			`select to_char(max(upper(rr.stay)), 'YYYY-MM-DD') from reservation r join reservation_room rr on rr.reservation_id = r.id
			 where r.guest_id = (select guest_id from reservation where id = $1) and r.id <> $1 and r.status = 'checked_out'`, a.id).Scan(&last); err != nil {
			return nil, err
		}
		if last != nil {
			out = append(out, Finding{SubjectType: "reservation", Subject: a.id, Severity: "info",
				Text: fmt.Sprintf("%s has stayed before. The last stay ended %s.", a.guest, *last), Facts: map[string]any{"lastStay": *last}})
		}
	}
	return out, nil
}

func refundWaiting(ctx context.Context, tx pgx.Tx, sc scope) ([]Finding, error) {
	rows, err := tx.Query(ctx,
		`select r.id, r.amount, f.currency, r.reason from refund r join payment p on p.id = r.payment_id join folio f on f.id = p.folio_id
		 where f.property_id = $1 and r.status = 'pending' order by r.created_at`, sc.propertyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Finding
	for rows.Next() {
		var id uuid.UUID
		var amount int64
		var cur, reason string
		if err := rows.Scan(&id, &amount, &cur, &reason); err != nil {
			return nil, err
		}
		out = append(out, Finding{SubjectType: "refund", Subject: id, Severity: "warning",
			Text: fmt.Sprintf("A refund of %s waits for approval: %s.", money(amount, cur), reason), Facts: map[string]any{"amount": amount, "currency": cur},
			Action: openAction("Review the refund", "/payments")})
	}
	return out, rows.Err()
}

// tagNotAnswered reminds the author of a note whose tag nobody has answered. It goes to the author's role.
func tagNotAnswered(ctx context.Context, tx pgx.Tx, sc scope) ([]Finding, error) {
	rows, err := tx.Query(ctx,
		`select n.id, u.name, left(n.body, 60), coalesce((select m.role from membership m where m.user_id = n.author_id and m.organization_id = n.organization_id and (m.property_id = n.property_id or m.property_id is null) limit 1), 'front_desk'),
		        (select count(*) from note_mention x where x.note_id = n.id and x.done_at is null)
		 from note n join app_user u on u.id = n.author_id
		 where n.property_id = $1 and n.removed_at is null and n.created_at <= $2
		   and exists (select 1 from note_mention x where x.note_id = n.id and x.done_at is null)
		 order by n.created_at`, sc.propertyID, sc.now.Add(-time.Duration(sc.th["hours"])*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Finding
	for rows.Next() {
		var id uuid.UUID
		var author, snippet, role string
		var open int
		if err := rows.Scan(&id, &author, &snippet, &role, &open); err != nil {
			return nil, err
		}
		out = append(out, Finding{SubjectType: "note", Subject: id, Severity: "info", Role: access.Role(role),
			Text:  fmt.Sprintf("%d tag(s) from %s's note have no answer after %d hours: %q", open, author, int(sc.th["hours"]), snippet),
			Facts: map[string]any{"open": open}, Action: openAction("Open the booking", "/calendar")})
	}
	return out, rows.Err()
}

// paidLate finds guest payments that arrived after the hold ran out, or that did not cover the booking.
// The alert stays until the booking is confirmed or the money has gone back.
func paidLate(ctx context.Context, tx pgx.Tx, sc scope) ([]Finding, error) {
	rows, err := tx.Query(ctx, `select r.id, r.code, r.status, r.currency,
		coalesce((select sum(p.amount) from payment p where p.folio_id = fo.id), 0)
		- coalesce((select sum(x.amount) from refund x join payment p on p.id = x.payment_id where p.folio_id = fo.id and x.status <> 'rejected'), 0)
		from reservation r join folio fo on fo.reservation_id = r.id
		where r.property_id = $1 and r.paid_late and r.status in ('pending', 'cancelled') order by r.created_at`, sc.propertyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Finding
	for rows.Next() {
		var id uuid.UUID
		var code, status, cur string
		var held int64
		if err := rows.Scan(&id, &code, &status, &cur, &held); err != nil {
			return nil, err
		}
		if held <= 0 {
			continue
		}
		text := fmt.Sprintf("Booking %s was paid late and is cancelled. %s is held.", code, money(held, cur))
		if status == "pending" {
			text = fmt.Sprintf("Booking %s was paid, but not the full amount. %s is held.", code, money(held, cur))
		}
		out = append(out, Finding{SubjectType: "reservation", Subject: id, Severity: "warning", Text: text,
			Facts:  map[string]any{"held": held, "currency": cur, "status": status},
			Action: openAction("Open the booking", "/calendar")})
	}
	return out, rows.Err()
}

// channelTrouble finds a switched-on channel connection whose last send failed, or that has not sent anything
// for a few hours. Rooms and prices on the booking sites are then out of date, which is how overbookings start.
func channelTrouble(ctx context.Context, tx pgx.Tx, sc scope) ([]Finding, error) {
	rows, err := tx.Query(ctx, `select c.id, c.provider,
		coalesce((select max(s.created_at) from sync_log s where s.connection_id = c.id and s.direction = 'push' and s.status = 'ok'), c.created_at),
		coalesce((select s.status from sync_log s where s.connection_id = c.id and s.direction = 'push' order by s.created_at desc, s.id desc limit 1), '')
		from channel_connection c where c.property_id = $1 and c.enabled`, sc.propertyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Finding
	for rows.Next() {
		var id uuid.UUID
		var provider, last string
		var lastOK time.Time
		if err := rows.Scan(&id, &provider, &lastOK, &last); err != nil {
			return nil, err
		}
		stale := sc.now.Sub(lastOK) > time.Duration(sc.th["staleHours"])*time.Hour
		if last != "error" && !stale {
			continue
		}
		text := "The last send of rooms and prices to your booking sites failed. Their rooms and prices may be out of date."
		if last != "error" {
			text = fmt.Sprintf("Nothing has been sent to your booking sites for %d hours. Their rooms and prices may be out of date.", int(sc.now.Sub(lastOK).Hours()))
		}
		out = append(out, Finding{SubjectType: "channel", Subject: id, Severity: "warning", Text: text,
			Facts: map[string]any{"provider": provider}, Action: openAction("Open the sync log", "/sync-log")})
	}
	return out, rows.Err()
}

// bookingReview finds guest bookings whose fraud score is high and that no manager has decided yet.
func bookingReview(ctx context.Context, tx pgx.Tx, sc scope) ([]Finding, error) {
	rows, err := tx.Query(ctx, `select b.reservation_id, r.code, b.score from booking_risk b join reservation r on r.id = b.reservation_id
		where b.property_id = $1 and b.status = 'review' order by b.created_at`, sc.propertyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Finding
	for rows.Next() {
		var id uuid.UUID
		var code string
		var score int
		if err := rows.Scan(&id, &code, &score); err != nil {
			return nil, err
		}
		out = append(out, Finding{SubjectType: "reservation", Subject: id, Severity: "warning",
			Text:   fmt.Sprintf("Booking %s looks risky (score %d). Check it, then approve or decline it.", code, score),
			Facts:  map[string]any{"score": score},
			Action: openAction("Open the review queue", "/risk")})
	}
	return out, rows.Err()
}
