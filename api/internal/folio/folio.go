// Package folio holds what a guest owes and has paid: lines, tax, payments recorded by hand, and refunds (spec 06).
package folio

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/ids"
)

var (
	ErrNotFound       = errors.New("folio: not found")
	ErrBad            = errors.New("folio: bad input")
	ErrRefundTooLarge = errors.New("folio: a refund cannot exceed what is left of the payment")
	ErrAlreadyDecided = errors.New("folio: the refund was already decided")
)

// DefaultFrontDeskRefundLimit is what a front desk user may refund without approval until the owner sets another amount.
const DefaultFrontDeskRefundLimit int64 = 5000

type taxRule struct {
	name      string
	p100      int64 // the percent in hundredths, so 10 percent is 1000
	inclusive bool
}

// Ensure returns the reservation's folio, creating it if needed, with its automatic lines up to date.
func Ensure(ctx context.Context, tx pgx.Tx, orgID, propertyID, reservationID uuid.UUID, now time.Time) (folioID uuid.UUID, currency string, err error) {
	if err = tx.QueryRow(ctx, `select currency from reservation where id = $1 and property_id = $2`, reservationID, propertyID).Scan(&currency); err != nil {
		return uuid.Nil, "", ErrNotFound
	}
	id := ids.New()
	if _, err = tx.Exec(ctx, `insert into folio (id, organization_id, property_id, reservation_id, currency, created_at) values ($1, $2, $3, $4, $5, $6) on conflict (reservation_id) do nothing`,
		id, orgID, propertyID, reservationID, currency, now); err != nil {
		return
	}
	if err = tx.QueryRow(ctx, `select id from folio where reservation_id = $1`, reservationID).Scan(&folioID); err != nil {
		return
	}
	return folioID, currency, sync(ctx, tx, orgID, propertyID, folioID, reservationID, now)
}

// sync rebuilds the automatic room and tax lines from the reservation's nights. Lines people added stay as they are.
func sync(ctx context.Context, tx pgx.Tx, orgID, propertyID, folioID, reservationID uuid.UUID, now time.Time) error {
	if _, err := tx.Exec(ctx, `delete from folio_line where folio_id = $1 and auto`, folioID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `select name, (percent * 100)::bigint, inclusive from tax_rule where property_id = $1 and applies_to in ('all', 'room') order by name`, propertyID)
	if err != nil {
		return err
	}
	var rules []taxRule
	var inclusiveP100 int64
	for rows.Next() {
		var r taxRule
		if err := rows.Scan(&r.name, &r.p100, &r.inclusive); err != nil {
			rows.Close()
			return err
		}
		if r.inclusive {
			inclusiveP100 += r.p100
		}
		rules = append(rules, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	nights, err := tx.Query(ctx,
		`select n.date, n.price, t.name from reservation_night n
		 join reservation_room rr on rr.id = n.reservation_room_id join room_type t on t.id = rr.room_type_id
		 where rr.reservation_id = $1 order by n.date, rr.id`, reservationID)
	if err != nil {
		return err
	}
	type night struct {
		date  time.Time
		price int64
		name  string
	}
	var list []night
	for nights.Next() {
		var n night
		if err := nights.Scan(&n.date, &n.price, &n.name); err != nil {
			nights.Close()
			return err
		}
		list = append(list, n)
	}
	nights.Close()
	if err := nights.Err(); err != nil {
		return err
	}
	var roomTotal int64
	for _, n := range list {
		included := int64(0)
		if inclusiveP100 > 0 {
			net := (n.price*10000*2 + (10000 + inclusiveP100)) / (2 * (10000 + inclusiveP100)) // price without tax, rounded
			included = n.price - net
		}
		if _, err := tx.Exec(ctx,
			`insert into folio_line (id, organization_id, folio_id, kind, description, unit_price, tax_included, auto, night, created_at) values ($1, $2, $3, 'room', $4, $5, $6, true, $7, $8)`,
			ids.New(), orgID, folioID, fmt.Sprintf("%s, night of %s", n.name, n.date.Format("2006-01-02")), n.price, included, n.date, now); err != nil {
			return err
		}
		roomTotal += n.price
	}
	for _, r := range rules {
		if r.inclusive || roomTotal == 0 {
			continue
		}
		amount := (roomTotal*r.p100*2 + 10000) / 20000 // rounded to the nearest unit
		if _, err := tx.Exec(ctx,
			`insert into folio_line (id, organization_id, folio_id, kind, description, unit_price, auto, created_at) values ($1, $2, $3, 'tax', $4, $5, true, $6)`,
			ids.New(), orgID, folioID, fmt.Sprintf("%s %s%%", r.name, trimPercent(r.p100)), amount, now); err != nil {
			return err
		}
	}
	return nil
}

func trimPercent(p100 int64) string {
	s := fmt.Sprintf("%d.%02d", p100/100, p100%100)
	return strings.TrimSuffix(strings.TrimSuffix(s, "0"), ".0")
}

// AddLine adds an extra charge or an adjustment (which may be a discount) to a folio.
func AddLine(ctx context.Context, tx pgx.Tx, orgID, propertyID, folioID, by uuid.UUID, kind, description string, quantity int, unitPrice int64, now time.Time) (uuid.UUID, error) {
	description = strings.TrimSpace(description)
	if (kind != "extra" && kind != "adjustment") || description == "" || len(description) > 200 || quantity < 1 || unitPrice == 0 || (kind == "extra" && unitPrice < 0) {
		return uuid.Nil, ErrBad
	}
	var one int
	if err := tx.QueryRow(ctx, `select 1 from folio where id = $1 and property_id = $2`, folioID, propertyID).Scan(&one); err != nil {
		return uuid.Nil, ErrNotFound
	}
	id := ids.New()
	if _, err := tx.Exec(ctx, `insert into folio_line (id, organization_id, folio_id, kind, description, quantity, unit_price, posted_by, created_at) values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		id, orgID, folioID, kind, description, quantity, unitPrice, by, now); err != nil {
		return uuid.Nil, err
	}
	return id, audit.Record(ctx, tx, audit.Entry{Action: "folio.line_added", TargetType: "folio", TargetID: folioID, TargetLabel: description,
		After: map[string]any{"kind": kind, "quantity": quantity, "unitPrice": unitPrice}})
}

var methods = map[string]bool{"cash": true, "card_terminal": true, "bank_transfer": true, "other": true}

// AddPayment records money taken by hand at the desk.
func AddPayment(ctx context.Context, tx pgx.Tx, orgID, propertyID, folioID, by uuid.UUID, method string, amount int64, note string, now time.Time) (uuid.UUID, error) {
	if !methods[method] || amount < 1 || len(note) > 500 {
		return uuid.Nil, ErrBad
	}
	var one int
	if err := tx.QueryRow(ctx, `select 1 from folio where id = $1 and property_id = $2`, folioID, propertyID).Scan(&one); err != nil {
		return uuid.Nil, ErrNotFound
	}
	id := ids.New()
	if _, err := tx.Exec(ctx, `insert into payment (id, organization_id, folio_id, method, amount, note, taken_by, created_at) values ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, orgID, folioID, method, amount, strings.TrimSpace(note), by, now); err != nil {
		return uuid.Nil, err
	}
	return id, audit.Record(ctx, tx, audit.Entry{Action: "payment.recorded", TargetType: "folio", TargetID: folioID, TargetLabel: method,
		After: map[string]any{"paymentId": id, "method": method, "amount": amount}})
}

// RefundLimit is how much the role may refund without approval at the property.
func RefundLimit(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, role access.Role) (int64, error) {
	if access.Can(role, access.RefundsApprove) {
		return 1<<62 - 1, nil
	}
	var amount int64
	err := tx.QueryRow(ctx, `select amount from refund_limit where property_id = $1 and role = $2`, propertyID, string(role)).Scan(&amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultFrontDeskRefundLimit, nil
	}
	return amount, err
}

// Refund asks to give money back on a payment. It is done at once when the person may approve it or it is within
// their limit; otherwise it waits for a manager or owner. A waiting refund already holds its share of the payment.
func Refund(ctx context.Context, tx pgx.Tx, orgID, propertyID, paymentID, by uuid.UUID, role access.Role, amount int64, reason string, now time.Time) (id uuid.UUID, status string, err error) {
	reason = strings.TrimSpace(reason)
	if amount < 1 || reason == "" || len(reason) > 500 {
		return uuid.Nil, "", ErrBad
	}
	var paid, taken int64
	if err = tx.QueryRow(ctx,
		`select p.amount, coalesce((select sum(amount) from refund where payment_id = p.id and status in ('done', 'pending')), 0)
		 from payment p join folio f on f.id = p.folio_id where p.id = $1 and f.property_id = $2 for update of p`, paymentID, propertyID).Scan(&paid, &taken); err != nil {
		return uuid.Nil, "", ErrNotFound
	}
	if amount > paid-taken {
		return uuid.Nil, "", ErrRefundTooLarge
	}
	limit, err := RefundLimit(ctx, tx, propertyID, role)
	if err != nil {
		return uuid.Nil, "", err
	}
	status = "pending"
	if amount <= limit {
		status = "done"
	}
	id = ids.New()
	var decided *time.Time
	var approver *uuid.UUID
	if status == "done" {
		decided, approver = &now, &by
	}
	if _, err = tx.Exec(ctx, `insert into refund (id, organization_id, payment_id, amount, reason, status, requested_by, approved_by, created_at, decided_at) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		id, orgID, paymentID, amount, reason, status, by, approver, now, decided); err != nil {
		return uuid.Nil, "", err
	}
	if status == "done" {
		if err = refreshPayment(ctx, tx, paymentID); err != nil {
			return uuid.Nil, "", err
		}
	}
	return id, status, audit.Record(ctx, tx, audit.Entry{Action: "refund." + status, TargetType: "payment", TargetID: paymentID, Reason: reason,
		After: map[string]any{"refundId": id, "amount": amount, "status": status}})
}

// Decide approves or rejects a waiting refund. Only people with refund approval rights call it.
func Decide(ctx context.Context, tx pgx.Tx, propertyID, refundID, by uuid.UUID, approve bool, now time.Time) error {
	var status string
	var paymentID uuid.UUID
	var amount int64
	if err := tx.QueryRow(ctx,
		`select r.status, r.payment_id, r.amount from refund r join payment p on p.id = r.payment_id join folio f on f.id = p.folio_id
		 where r.id = $1 and f.property_id = $2 for update of r`, refundID, propertyID).Scan(&status, &paymentID, &amount); err != nil {
		return ErrNotFound
	}
	if status != "pending" {
		return ErrAlreadyDecided
	}
	next := "rejected"
	if approve {
		next = "done"
	}
	if _, err := tx.Exec(ctx, `update refund set status = $2, approved_by = $3, decided_at = $4 where id = $1`, refundID, next, by, now); err != nil {
		return err
	}
	if approve {
		if err := refreshPayment(ctx, tx, paymentID); err != nil {
			return err
		}
	}
	action := "refund.rejected"
	if approve {
		action = "refund.approved"
	}
	return audit.Record(ctx, tx, audit.Entry{Action: action, TargetType: "payment", TargetID: paymentID, After: map[string]any{"refundId": refundID, "amount": amount}})
}

func refreshPayment(ctx context.Context, tx pgx.Tx, paymentID uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`update payment p set status = case
		   when coalesce((select sum(amount) from refund where payment_id = p.id and status = 'done'), 0) = 0 then 'paid'
		   when coalesce((select sum(amount) from refund where payment_id = p.id and status = 'done'), 0) >= p.amount then 'refunded'
		   else 'partly_refunded' end where p.id = $1`, paymentID)
	return err
}
