// Package guestbooking is the public side of booking: a guest with no sign-in finds a hotel by its address, asks for a price,
// books and pays (spec 05 and 06). The owner decides which payment types are offered.
package guestbooking

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/folio"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
	"hotelpms/api/internal/payments"
)

// CardHold is how long a room waits for a card payment (spec 05).
const CardHold = 15 * time.Minute

var kinds = []string{"hotel", "bank_transfer", "card"}

var (
	errTypeOff  = errors.New("guestbooking: that payment type is not offered")
	errNeedOne  = errors.New("guestbooking: at least one payment type must stay on")
	errRollback = errors.New("guestbooking: roll back")
)

type handler struct {
	httpx.Deps
	card payments.Provider // nil when no card provider is configured
}

// Mount registers the routes. card is the provider behind the "card" payment type; nil switches card payment off everywhere.
func Mount(card payments.Provider) httpx.Mount {
	return func(mux *http.ServeMux, d httpx.Deps) {
		h := &handler{d, card}
		owner := func(f http.HandlerFunc, method string) http.HandlerFunc {
			return httpx.Methods(map[string]http.HandlerFunc{method: httpx.Wrap(access.Require(d, access.PaymentsConfigure)(f))})
		}
		public := func(f http.HandlerFunc, method string) http.HandlerFunc {
			return httpx.Methods(map[string]http.HandlerFunc{method: f})
		}
		mux.HandleFunc("/api/payment-types", owner(h.listTypes, http.MethodGet))
		mux.HandleFunc("/api/payment-types/{kind}", owner(h.putType, http.MethodPut))
		mux.HandleFunc("/api/public/{slug}", public(h.info, http.MethodGet))
		mux.HandleFunc("/api/public/{slug}/quote", public(h.quote, http.MethodPost))
		mux.HandleFunc("/api/public/{slug}/bookings", public(h.create, http.MethodPost))
		mux.HandleFunc("/api/webhooks/{provider}", public(h.webhook, http.MethodPost))
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	dec.DisallowUnknownFields() // a card number sent by mistake is refused, never stored
	if err := dec.Decode(dst); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Check the details and try again.")
		return false
	}
	return true
}

func scope(w http.ResponseWriter, r *http.Request) (access.Actor, bool) {
	a := access.ActorFrom(r.Context())
	if a.PropertyID == uuid.Nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Choose a property first.")
		return a, false
	}
	return a, true
}

type typeOut struct {
	Kind        string `json:"kind"`
	Enabled     bool   `json:"enabled"`
	HoldHours   int    `json:"holdHours"`
	BankDetails string `json:"bankDetails"`
}

func (h *handler) listTypes(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	out := []typeOut{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		have := map[string]typeOut{}
		rows, err := tx.Query(r.Context(), `select kind, enabled, hold_hours, bank_details from payment_type where property_id = $1`, a.PropertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t typeOut
			if err := rows.Scan(&t.Kind, &t.Enabled, &t.HoldHours, &t.BankDetails); err != nil {
				return err
			}
			have[t.Kind] = t
		}
		for _, k := range kinds {
			t, ok := have[k]
			if !ok {
				t = typeOut{Kind: k, HoldHours: 48}
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *handler) putType(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	kind := r.PathValue("kind")
	var in struct {
		Enabled     bool   `json:"enabled"`
		HoldHours   int    `json:"holdHours"`
		BankDetails string `json:"bankDetails"`
	}
	if !slices.Contains(kinds, kind) || !readJSON(w, r, &in) || in.HoldHours < 1 || in.HoldHours > 336 || len(in.BankDetails) > 1000 {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Check the payment type and its hours.")
		return
	}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(),
			`insert into payment_type (property_id, kind, organization_id, enabled, hold_hours, bank_details) values ($1, $2, $3, $4, $5, $6)
			 on conflict (property_id, kind) do update set enabled = $4, hold_hours = $5, bank_details = $6`,
			a.PropertyID, kind, a.OrgID, in.Enabled, in.HoldHours, strings.TrimSpace(in.BankDetails)); err != nil {
			return err
		}
		var on int
		if err := tx.QueryRow(r.Context(), `select count(*) from payment_type where property_id = $1 and enabled`, a.PropertyID).Scan(&on); err != nil {
			return err
		}
		if on == 0 {
			return errNeedOne
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "payment_type.updated", TargetType: "property", TargetID: a.PropertyID, TargetLabel: kind, After: map[string]any{"enabled": in.Enabled, "holdHours": in.HoldHours}})
	})
	switch {
	case errors.Is(err, errNeedOne):
		httpx.Error(w, http.StatusConflict, "need_one", "At least one payment type must stay on for the booking page.")
	case err != nil:
		httpx.InternalError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// resolve turns the public address into an organization and property, or answers 404.
func (h *handler) resolve(w http.ResponseWriter, r *http.Request) (org, prop uuid.UUID, ok bool) {
	err := h.Pool.QueryRow(r.Context(), `select organization_id, property_id from property_by_slug($1)`, r.PathValue("slug")).Scan(&org, &prop)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
		return org, prop, false
	}
	if err != nil {
		httpx.InternalError(w, err)
		return org, prop, false
	}
	return org, prop, true
}

// offered lists the enabled payment types the guest may use. Card needs a configured provider.
func (h *handler) offered(ctx context.Context, tx pgx.Tx, prop uuid.UUID) ([]string, error) {
	rows, err := tx.Query(ctx, `select kind from payment_type where property_id = $1 and enabled order by kind`, prop)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		if k != "card" || h.card != nil {
			out = append(out, k)
		}
	}
	return out, rows.Err()
}

func (h *handler) info(w http.ResponseWriter, r *http.Request) {
	org, prop, ok := h.resolve(w, r)
	if !ok {
		return
	}
	type roomType struct {
		ID         uuid.UUID `json:"id"`
		Name       string    `json:"name"`
		BaseGuests int       `json:"baseGuests"`
		MaxGuests  int       `json:"maxGuests"`
	}
	type ratePlan struct {
		ID         uuid.UUID `json:"id"`
		RoomTypeID uuid.UUID `json:"roomTypeId"`
		Name       string    `json:"name"`
	}
	out := struct {
		Name          string     `json:"name"`
		Currency      string     `json:"currency"`
		DefaultLocale string     `json:"defaultLocale"`
		Address       string     `json:"address"`
		Payments      []string   `json:"payments"`
		RoomTypes     []roomType `json:"roomTypes"`
		RatePlans     []ratePlan `json:"ratePlans"`
	}{RoomTypes: []roomType{}, RatePlans: []ratePlan{}}
	err := db.WithOrg(r.Context(), h.Pool, org, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := tx.QueryRow(ctx, `select name, currency, default_locale, address from property where id = $1`, prop).Scan(&out.Name, &out.Currency, &out.DefaultLocale, &out.Address); err != nil {
			return err
		}
		var err error
		if out.Payments, err = h.offered(ctx, tx, prop); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `select id, name, base_guests, max_guests from room_type where property_id = $1 order by name`, prop)
		if err != nil {
			return err
		}
		for rows.Next() {
			var t roomType
			if err := rows.Scan(&t.ID, &t.Name, &t.BaseGuests, &t.MaxGuests); err != nil {
				rows.Close()
				return err
			}
			out.RoomTypes = append(out.RoomTypes, t)
		}
		rows.Close()
		rows, err = tx.Query(ctx, `select id, room_type_id, name from rate_plan where property_id = $1 order by name`, prop)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p ratePlan
			if err := rows.Scan(&p.ID, &p.RoomTypeID, &p.Name); err != nil {
				return err
			}
			out.RatePlans = append(out.RatePlans, p)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

type bookIn struct {
	Guest       booking.GuestInput    `json:"guest"`
	Rooms       []booking.RoomRequest `json:"rooms"`
	PaymentKind string                `json:"paymentKind"`
}

type lineOut struct {
	Description string `json:"description"`
	Amount      int64  `json:"amount"`
}

// price books the stay inside tx and reads what the folio says is due, so the figure shown is the figure charged.
func price(ctx context.Context, tx pgx.Tx, org, prop uuid.UUID, in bookIn, now time.Time) (booking.Reservation, uuid.UUID, int64, []lineOut, error) {
	res, err := booking.Create(ctx, tx, org, prop, booking.CreateInput{Guest: in.Guest, Rooms: in.Rooms, Source: "website"}, now)
	if err != nil {
		return res, uuid.Nil, 0, nil, err
	}
	folioID, _, err := folio.Ensure(ctx, tx, org, prop, res.ID, now)
	if err != nil {
		return res, uuid.Nil, 0, nil, err
	}
	due, _, err := folio.Balance(ctx, tx, org, prop, res.ID, now)
	if err != nil {
		return res, uuid.Nil, 0, nil, err
	}
	rows, err := tx.Query(ctx, `select description, quantity * unit_price from folio_line where folio_id = $1 order by created_at, id`, folioID)
	if err != nil {
		return res, uuid.Nil, 0, nil, err
	}
	defer rows.Close()
	lines := []lineOut{}
	for rows.Next() {
		var l lineOut
		if err := rows.Scan(&l.Description, &l.Amount); err != nil {
			return res, uuid.Nil, 0, nil, err
		}
		lines = append(lines, l)
	}
	return res, folioID, due, lines, rows.Err()
}

func bookingError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, booking.ErrSoldOut):
		httpx.Error(w, http.StatusConflict, "sold_out", "The last room was just taken. Choose other dates or another room.")
	case errors.Is(err, booking.ErrStayRule):
		httpx.Error(w, http.StatusConflict, "stay_rule", "The hotel does not take that stay. Try other dates.")
	case errors.Is(err, booking.ErrPast), errors.Is(err, booking.ErrBad):
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Check the dates and guests.")
	case errors.Is(err, booking.ErrNotFound), errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, http.StatusNotFound, "not_found", "That room or rate is not available here.")
	case errors.Is(err, errTypeOff):
		httpx.Error(w, http.StatusConflict, "payment_type_off", "The hotel no longer offers that way to pay. Choose another.")
	default:
		return false
	}
	return true
}

// quote prices a stay and answers whether it is sold out. It books inside a transaction it then rolls back, so it can never differ from a booking.
// shortcut: this locks the inventory rows for a moment per quote, fine for one hotel, replace with a read-only price when traffic grows.
func (h *handler) quote(w http.ResponseWriter, r *http.Request) {
	org, prop, ok := h.resolve(w, r)
	if !ok {
		return
	}
	var in bookIn
	if !readJSON(w, r, &in) {
		return
	}
	in.Guest = booking.GuestInput{Name: "Quote"}
	out := struct {
		Total    int64     `json:"total"`
		Currency string    `json:"currency"`
		SoldOut  bool      `json:"soldOut"`
		Lines    []lineOut `json:"lines"`
	}{Lines: []lineOut{}}
	err := db.WithOrg(r.Context(), h.Pool, org, func(tx pgx.Tx) error {
		res, _, due, lines, err := price(r.Context(), tx, org, prop, in, h.Clock())
		if err != nil {
			return err
		}
		out.Total, out.Currency, out.Lines = due, res.Currency, lines
		return errRollback
	})
	if errors.Is(err, booking.ErrSoldOut) {
		out.SoldOut = true
	} else if !errors.Is(err, errRollback) {
		if !bookingError(w, err) {
			httpx.InternalError(w, err)
		}
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

type madeOut struct {
	Code        string    `json:"code"`
	Total       int64     `json:"total"`
	Currency    string    `json:"currency"`
	Status      string    `json:"status"`
	CheckoutURL string    `json:"checkoutUrl,omitempty"`
	BankDetails string    `json:"bankDetails,omitempty"`
	HoldUntil   time.Time `json:"holdUntil,omitzero"`
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	org, prop, ok := h.resolve(w, r)
	if !ok {
		return
	}
	var in bookIn
	if !readJSON(w, r, &in) {
		return
	}
	if !slices.Contains(kinds, in.PaymentKind) {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Choose how you will pay.")
		return
	}
	now := h.Clock()
	var out madeOut
	var resID uuid.UUID
	err := db.WithOrg(r.Context(), h.Pool, org, func(tx pgx.Tx) error {
		ctx := r.Context()
		on, err := h.offered(ctx, tx, prop)
		if err != nil {
			return err
		}
		if !slices.Contains(on, in.PaymentKind) {
			return errTypeOff
		}
		res, _, due, _, err := price(ctx, tx, org, prop, in, now)
		if err != nil {
			return err
		}
		out = madeOut{Code: res.Code, Total: due, Currency: res.Currency, Status: "confirmed"}
		resID = res.ID
		var hold *time.Time
		switch in.PaymentKind {
		case "card":
			t := now.Add(CardHold)
			hold, out.Status = &t, "pending"
		case "bank_transfer":
			var hours int
			if err := tx.QueryRow(ctx, `select hold_hours, bank_details from payment_type where property_id = $1 and kind = 'bank_transfer'`, prop).Scan(&hours, &out.BankDetails); err != nil {
				return err
			}
			t := now.Add(time.Duration(hours) * time.Hour)
			hold, out.Status = &t, "pending"
		}
		if hold != nil {
			out.HoldUntil = *hold
		}
		_, err = tx.Exec(ctx, `update reservation set status = $2, payment_kind = $3, hold_until = $4 where id = $1`, res.ID, out.Status, in.PaymentKind, hold)
		return err
	})
	if bookingError(w, err) {
		return
	}
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	if in.PaymentKind == "card" {
		c, err := h.card.CreateCheckout(r.Context(), payments.CheckoutRequest{
			Reference: org.String() + ":" + out.Code, Amount: out.Total, Currency: out.Currency, Description: "Booking " + out.Code,
			SuccessURL: h.BaseURL + "/book/done?code=" + out.Code, CancelURL: h.BaseURL + "/book/cancelled?code=" + out.Code,
			Key: "booking-" + resID.String(),
		})
		if err != nil {
			// The room stays held until the hold runs out; the guest can try again.
			httpx.Error(w, http.StatusBadGateway, "provider_unavailable", "The payment page could not be opened. Try again in a moment.")
			return
		}
		out.CheckoutURL = c.URL
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// webhook records a payment the provider reports. The signature is the only proof, so nothing is read before it is checked.
func (h *handler) webhook(w http.ResponseWriter, r *http.Request) {
	if h.card == nil || r.PathValue("provider") != h.card.Name() {
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
		return
	}
	now := h.Clock()
	ev, err := h.card.ParseWebhook(r, now)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_signature", "The signature could not be verified.")
		return
	}
	orgText, code, found := strings.Cut(ev.Reference, ":")
	org, perr := uuid.Parse(orgText)
	if ev.Type != payments.Paid || !found || perr != nil {
		w.WriteHeader(http.StatusNoContent) // nothing for us to do, and an error would only make the provider retry
		return
	}
	err = db.WithOrg(r.Context(), h.Pool, org, func(tx pgx.Tx) error {
		ctx := r.Context()
		tag, err := tx.Exec(ctx, `insert into payment_event (provider, event_id, organization_id, received_at) values ($1, $2, $3, $4) on conflict do nothing`, h.card.Name(), ev.ID, org, now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil // the provider sent this event before
		}
		var id, prop uuid.UUID
		var status string
		if err := tx.QueryRow(ctx, `select id, property_id, status from reservation where code = $1 for update`, code).Scan(&id, &prop, &status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
		folioID, _, err := folio.Ensure(ctx, tx, org, prop, id, now)
		if err != nil {
			return err
		}
		payID := ids.New()
		if _, err := tx.Exec(ctx,
			`insert into payment (id, organization_id, folio_id, method, amount, note, created_at, provider, provider_ref) values ($1, $2, $3, 'card_online', $4, '', $5, $6, $7)`,
			payID, org, folioID, ev.Amount, now, h.card.Name(), ev.PaymentRef); err != nil {
			return err
		}
		entry := audit.Entry{Action: "payment.recorded", TargetType: "folio", TargetID: folioID, TargetLabel: "card_online", After: map[string]any{"paymentId": payID, "amount": ev.Amount}}
		switch status {
		case "pending":
			// shortcut: any paid event confirms; a part payment is not told apart from a full one, replace when deposits are added.
			if _, err := tx.Exec(ctx, `update reservation set status = 'confirmed', hold_until = null where id = $1`, id); err != nil {
				return err
			}
		case "cancelled":
			// The hold ran out before the guest paid. The money is on the folio and staff must decide: restore the booking or refund.
			if _, err := tx.Exec(ctx, `update reservation set paid_late = true where id = $1`, id); err != nil {
				return err
			}
			entry.Action = "payment.late"
		}
		return audit.RecordSystem(ctx, tx, org, "payments", entry)
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ExpireHolds cancels pending bookings whose hold has run out and gives their rooms back. It returns how many it cancelled.
func ExpireHolds(ctx context.Context, pool *pgxpool.Pool, now time.Time) (int, error) {
	rows, err := pool.Query(ctx, `select organization_id, property_id from all_properties()`)
	if err != nil {
		return 0, err
	}
	type scope struct{ org, prop uuid.UUID }
	var all []scope
	for rows.Next() {
		var s scope
		if err := rows.Scan(&s.org, &s.prop); err != nil {
			rows.Close()
			return 0, err
		}
		all = append(all, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, s := range all {
		err := db.WithOrg(ctx, pool, s.org, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `select id from reservation where property_id = $1 and status = 'pending' and hold_until <= $2 order by hold_until for update`, s.prop, now)
			if err != nil {
				return err
			}
			var due []uuid.UUID
			for rows.Next() {
				var id uuid.UUID
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				due = append(due, id)
			}
			rows.Close()
			for _, id := range due {
				if err := booking.Cancel(ctx, tx, s.org, s.prop, id, now); err != nil {
					return err
				}
				n++
			}
			return rows.Err()
		})
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// RunSweeper releases expired holds every interval until ctx ends.
func RunSweeper(ctx context.Context, pool *pgxpool.Pool, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = ExpireHolds(ctx, pool, time.Now())
		}
	}
}
