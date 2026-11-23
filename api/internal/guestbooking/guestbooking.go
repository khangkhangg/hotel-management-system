// Package guestbooking is the public side of booking: a guest with no sign-in finds a hotel by its address, asks for a price,
// books and pays (spec 05 and 06). The owner decides which payment types are offered.
package guestbooking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	netmail "net/mail"
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
	"hotelpms/api/internal/extras"
	"hotelpms/api/internal/folio"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
	"hotelpms/api/internal/mail"
	"hotelpms/api/internal/payments"
	"hotelpms/api/internal/promo"
	"hotelpms/api/internal/ratelimit"
	"hotelpms/api/internal/risk"
	"hotelpms/api/internal/siteprofile"
)

// CardHold is how long a room waits for a card payment (spec 05).
const CardHold = 30 * time.Minute // Stripe will not expire a payment page sooner

var kinds = []string{"hotel", "bank_transfer", "card"}

var (
	errTypeOff  = errors.New("guestbooking: that payment type is not offered")
	errNeedOne  = errors.New("guestbooking: at least one payment type must stay on")
	errRollback = errors.New("guestbooking: roll back")
)

type handler struct {
	httpx.Deps
	card    payments.Provider // nil when no card provider is configured
	cardOrg uuid.UUID         // the one organization whose Stripe account the keys belong to
}

func (h *handler) throttled(w http.ResponseWriter, r *http.Request, what string, max int, window time.Duration) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if ratelimit.Allow(r.Context(), h.Pool, what+":"+ip, h.Clock(), max, window) {
		return false
	}
	httpx.Error(w, http.StatusTooManyRequests, "slow_down", "Too many requests. Wait a few minutes and try again.")
	return true
}

// cardCurrencies are the currencies whose Stripe amounts were checked against our minor units.
var cardCurrencies = []string{"USD", "EUR", "GBP", "VND", "JPY", "KRW", "IDR", "SGD", "THB", "AUD", "CAD"}

// Mount registers the routes. card is the provider behind the "card" payment type, and cardOrg the organization its account belongs to;
// card payment is offered to no other organization, so one hotel's guests can never pay into another hotel's account.
func Mount(card payments.Provider, cardOrg uuid.UUID) httpx.Mount {
	return func(mux *http.ServeMux, d httpx.Deps) {
		h := &handler{Deps: d, card: card, cardOrg: cardOrg}
		owner := func(f http.HandlerFunc, method string) http.HandlerFunc {
			return httpx.Methods(map[string]http.HandlerFunc{method: httpx.Wrap(access.Require(d, access.PaymentsConfigure)(f))})
		}
		public := func(f http.HandlerFunc, method string) http.HandlerFunc {
			return httpx.Methods(map[string]http.HandlerFunc{method: f})
		}
		mux.HandleFunc("/api/payment-types", owner(h.listTypes, http.MethodGet))
		mux.HandleFunc("/api/payment-types/{kind}", owner(h.putType, http.MethodPut))
		mux.HandleFunc("/api/public/{slug}", public(h.info, http.MethodGet))
		mux.HandleFunc("/api/public/{slug}/photos/{id}", public(h.photo, http.MethodGet))
		mux.HandleFunc("/api/public/{slug}/quote", public(h.quote, http.MethodPost))
		mux.HandleFunc("/api/public/{slug}/bookings", public(h.create, http.MethodPost))
		mux.HandleFunc("/api/public/{slug}/bookings/{code}", public(h.status, http.MethodGet))
		h.mountManage(mux)
		mux.HandleFunc("/api/reservations/{id}/confirm-payment", httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: httpx.Wrap(access.Require(d, access.PaymentsTake)(http.HandlerFunc(h.confirmPayment)))}))
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
func (h *handler) offered(ctx context.Context, tx pgx.Tx, org, prop uuid.UUID) ([]string, error) {
	var currency string
	if err := tx.QueryRow(ctx, `select currency from property where id = $1`, prop).Scan(&currency); err != nil {
		return nil, err
	}
	cardOK := h.card != nil && org == h.cardOrg && slices.Contains(cardCurrencies, currency)
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
		if k != "card" || cardOK {
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
		Name          string             `json:"name"`
		Currency      string             `json:"currency"`
		DefaultLocale string             `json:"defaultLocale"`
		Address       string             `json:"address"`
		Payments      []string           `json:"payments"`
		RoomTypes     []roomType         `json:"roomTypes"`
		RatePlans     []ratePlan         `json:"ratePlans"`
		Extras        []extraOut         `json:"extras"`
		Site          siteprofile.Public `json:"site"`
	}{RoomTypes: []roomType{}, RatePlans: []ratePlan{}, Extras: []extraOut{}}
	err := db.WithOrg(r.Context(), h.Pool, org, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := tx.QueryRow(ctx, `select name, currency, default_locale, address from property where id = $1`, prop).Scan(&out.Name, &out.Currency, &out.DefaultLocale, &out.Address); err != nil {
			return err
		}
		var err error
		if out.Site, err = siteprofile.LoadPublic(ctx, tx, prop); err != nil {
			return err
		}
		if out.Payments, err = h.offered(ctx, tx, org, prop); err != nil {
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
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		xs, err := tx.Query(ctx, `select id, name, kind, unit, price from extra where property_id = $1 and active order by name`, prop)
		if err != nil {
			return err
		}
		defer xs.Close()
		for xs.Next() {
			var x extraOut
			if err := xs.Scan(&x.ID, &x.Name, &x.Kind, &x.Unit, &x.Price); err != nil {
				return err
			}
			out.Extras = append(out.Extras, x)
		}
		return xs.Err()
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

type extraReq struct {
	ID       uuid.UUID `json:"id"`
	Quantity int       `json:"quantity"`
}

type extraOut struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Kind  string    `json:"kind"`
	Unit  string    `json:"unit"`
	Price int64     `json:"price"`
}

type bookIn struct {
	Guest       booking.GuestInput    `json:"guest"`
	Rooms       []booking.RoomRequest `json:"rooms"`
	PaymentKind string                `json:"paymentKind"`
	Key         string                `json:"key"`
	Extras      []extraReq            `json:"extras"`
	Promo       string                `json:"promo"`
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
	if strings.TrimSpace(in.Promo) != "" {
		nights, arrival := 0, time.Time{}
		for _, rm := range in.Rooms {
			from, e1 := time.Parse("2006-01-02", rm.From)
			to, e2 := time.Parse("2006-01-02", rm.To)
			if e1 == nil && e2 == nil {
				nights = max(nights, int(to.Sub(from).Hours()/24))
				if arrival.IsZero() || from.Before(arrival) {
					arrival = from
				}
			}
		}
		if err := promo.Apply(ctx, tx, org, prop, res.ID, in.Promo, res.Total, nights, arrival, now); err != nil {
			return res, uuid.Nil, 0, nil, err
		}
	}
	for _, x := range in.Extras {
		if _, err := extras.Add(ctx, tx, org, prop, res.ID, x.ID, x.Quantity, uuid.Nil, now); err != nil {
			return res, uuid.Nil, 0, nil, err
		}
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
	case errors.Is(err, booking.ErrPast), errors.Is(err, booking.ErrBad), errors.Is(err, folio.ErrBad):
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Check the dates and guests.")
	case errors.Is(err, booking.ErrNotFound), errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, http.StatusNotFound, "not_found", "That room or rate is not available here.")
	case errors.Is(err, promo.ErrInvalid):
		httpx.Error(w, http.StatusConflict, "promo_invalid", "That promo code does not work for these dates.")
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
	if !ok || h.throttled(w, r, "quote", 30, time.Minute) {
		return
	}
	var in bookIn
	if !readJSON(w, r, &in) {
		return
	}
	if !extrasOK(in) {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Check the extras and try again.")
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
	if !ok || h.throttled(w, r, "book", 5, 10*time.Minute) {
		return
	}
	var in bookIn
	if !readJSON(w, r, &in) {
		return
	}
	if !slices.Contains(kinds, in.PaymentKind) || !sane(in) {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Check your name, email and the dates, then try again.")
		return
	}
	now := h.Clock()
	var out madeOut
	var resID uuid.UUID
	var replay bool
	var assessed risk.Result
	err := db.WithOrg(r.Context(), h.Pool, org, func(tx pgx.Tx) error {
		ctx := r.Context()
		on, err := h.offered(ctx, tx, org, prop)
		if err != nil {
			return err
		}
		if !slices.Contains(on, in.PaymentKind) {
			return errTypeOff
		}
		if in.Key != "" {
			// The lock makes a double click wait for the first request, so it finds that booking below.
			if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, org.String()+in.Key); err != nil {
				return err
			}
			var code, status, cur string
			var total int64
			var hu *time.Time
			err := tx.QueryRow(ctx, `select id, code, status, currency, total, hold_until from reservation where property_id = $2 and channel_booking_ref = $1 and source_channel = 'website'`, in.Key, prop).Scan(&resID, &code, &status, &cur, &total, &hu)
			if err == nil { // shortcut: the replay leaves out bank details, the guest has them by email
				out = madeOut{Code: code, Total: total, Currency: cur, Status: status}
				if hu != nil {
					out.HoldUntil = *hu
				}
				replay = true
				return nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		res, _, due, _, err := price(ctx, tx, org, prop, in, now)
		if err != nil {
			return err
		}
		out = madeOut{Code: res.Code, Total: due, Currency: res.Currency, Status: "confirmed"}
		resID = res.ID
		if assessed, err = h.assess(ctx, tx, org, prop, res.ID, in, r, now); err != nil {
			return err
		}
		if in.Key != "" {
			if _, err := tx.Exec(ctx, `update reservation set channel_booking_ref = $2 where id = $1`, resID, in.Key); err != nil {
				return err
			}
		}
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
	if in.PaymentKind == "card" && out.Status == "pending" {
		c, err := h.card.CreateCheckout(r.Context(), payments.CheckoutRequest{
			Reference: org.String() + ":" + out.Code, Amount: out.Total, Currency: out.Currency, Description: "Booking " + out.Code,
			SuccessURL: h.BaseURL + "/book/" + r.PathValue("slug") + "?done=" + out.Code, CancelURL: h.BaseURL + "/book/" + r.PathValue("slug"),
			Key: "booking-" + resID.String(), ExpiresAt: out.HoldUntil, ThreeDS: assessed.Level != "low",
		})
		if err != nil {
			// Give the room back at once, so the guest's retry is not turned away by their own hold.
			_ = db.WithOrg(r.Context(), h.Pool, org, func(tx pgx.Tx) error { return booking.Cancel(r.Context(), tx, org, prop, resID, now) })
			httpx.Error(w, http.StatusBadGateway, "provider_unavailable", "The payment page could not be opened. Try again in a moment.")
			return
		}
		out.CheckoutURL = c.URL
	} else if !replay {
		h.mailGuest(in.Guest.Email, out, out.BankDetails)
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// sane refuses bookings no real guest would send: a bad email, oversized text, or a stay no hotel takes online.
func sane(in bookIn) bool {
	a, err := netmail.ParseAddress(in.Guest.Email)
	if err != nil || a.Address != in.Guest.Email || len(in.Guest.Email) > 200 || len(in.Guest.Phone) > 50 || len(in.Guest.Name) > 200 || len(in.Guest.Country) > 100 {
		return false
	}
	if len(in.Key) > 64 || len(in.Promo) > 20 || !extrasOK(in) {
		return false
	}
	if len(in.Rooms) < 1 || len(in.Rooms) > 4 {
		return false
	}
	for _, r := range in.Rooms {
		from, e1 := time.Parse("2006-01-02", r.From)
		to, e2 := time.Parse("2006-01-02", r.To)
		if e1 != nil || e2 != nil || to.Sub(from) > 30*24*time.Hour || r.Adults < 1 || r.Adults > 10 || r.Children < 0 || r.Children > 10 {
			return false
		}
	}
	return true
}

// mailGuest sends the confirmation after the booking is saved. A failed send never undoes a booking.
func (h *handler) mailGuest(to string, m madeOut, bank string) {
	text := fmt.Sprintf("Your booking code is %s.\nStatus: %s.\nTotal: %d %s (minor units).\n", m.Code, m.Status, m.Total, m.Currency)
	if bank != "" {
		text += "Send the transfer to:\n" + bank + "\n"
	}
	if !m.HoldUntil.IsZero() {
		text += "We hold your room until " + m.HoldUntil.UTC().Format("2006-01-02 15:04 MST") + ".\n"
	}
	if h.Mail != nil {
		h.Spawn(func() {
			_ = h.Mail.Send(context.Background(), mail.Message{To: to, Subject: "Your booking " + m.Code, Text: text})
		})
	}
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
	var confirmed *madeOut
	var guestEmail string
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
		folioID, currency, err := folio.Ensure(ctx, tx, org, prop, id, now)
		if err != nil {
			return err
		}
		due, _, err := folio.Balance(ctx, tx, org, prop, id, now)
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
			if !strings.EqualFold(ev.Currency, currency) || ev.Amount < due {
				// Money arrived but does not cover the booking: keep it pending and tell staff instead of confirming.
				if _, err := tx.Exec(ctx, `update reservation set paid_late = true where id = $1`, id); err != nil {
					return err
				}
				entry.Action = "payment.short"
				break
			}
			if _, err := tx.Exec(ctx, `update reservation set status = 'confirmed', hold_until = null where id = $1`, id); err != nil {
				return err
			}
			confirmed = &madeOut{Code: code, Total: due, Currency: currency, Status: "confirmed"}
			if err := tx.QueryRow(ctx, `select g.email from reservation r join guest g on g.id = r.guest_id where r.id = $1`, id).Scan(&guestEmail); err != nil {
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
	if confirmed != nil && guestEmail != "" {
		h.mailGuest(guestEmail, *confirmed, "")
	}
	w.WriteHeader(http.StatusNoContent)
}

// status tells a guest where their booking stands, by the code they were given. It holds no personal data.
func (h *handler) status(w http.ResponseWriter, r *http.Request) {
	org, _, ok := h.resolve(w, r)
	if !ok || h.throttled(w, r, "status", 60, time.Minute) {
		return
	}
	var out struct {
		Code      string    `json:"code"`
		Status    string    `json:"status"`
		Currency  string    `json:"currency"`
		HoldUntil time.Time `json:"holdUntil,omitzero"`
	}
	err := db.WithOrg(r.Context(), h.Pool, org, func(tx pgx.Tx) error {
		var hold *time.Time
		err := tx.QueryRow(r.Context(), `select r.code, r.status, r.currency, r.hold_until from reservation r join property p on p.id = r.property_id where r.code = $1 and p.slug = $2`, r.PathValue("code"), r.PathValue("slug")).Scan(&out.Code, &out.Status, &out.Currency, &hold)
		if hold != nil {
			out.HoldUntil = *hold
		}
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "not_found", "There is no booking with that code.")
		return
	}
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

var errNotPending = errors.New("guestbooking: nothing is waiting for a transfer")

// confirmPayment lets staff mark a bank transfer as received: the payment is recorded and the room is kept.
func (h *handler) confirmPayment(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
		return
	}
	now := h.Clock()
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		ctx := r.Context()
		var status string
		var kind *string
		var code string
		if err := tx.QueryRow(ctx, `select status, payment_kind, code from reservation where id = $1 and property_id = $2 for update`, id, a.PropertyID).Scan(&status, &kind, &code); err != nil {
			return err
		}
		if status != "pending" || kind == nil || *kind != "bank_transfer" {
			return errNotPending
		}
		folioID, _, err := folio.Ensure(ctx, tx, a.OrgID, a.PropertyID, id, now)
		if err != nil {
			return err
		}
		due, _, err := folio.Balance(ctx, tx, a.OrgID, a.PropertyID, id, now)
		if err != nil {
			return err
		}
		if due > 0 {
			if _, err := tx.Exec(ctx, `insert into payment (id, organization_id, folio_id, method, amount, note, created_at) values ($1, $2, $3, 'bank_transfer', $4, '', $5)`, ids.New(), a.OrgID, folioID, due, now); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `update reservation set status = 'confirmed', hold_until = null where id = $1`, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Action: "payment.recorded", TargetType: "reservation", TargetID: id, TargetLabel: code, After: map[string]any{"method": "bank_transfer", "amount": due}})
	})
	switch {
	case errors.Is(err, errNotPending):
		httpx.Error(w, http.StatusConflict, "not_pending", "No bank transfer is waiting on this booking.")
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
	case err != nil:
		httpx.InternalError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
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
	var failed []error
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
			failed = append(failed, err) // one property's trouble must not keep the others' rooms held
		}
	}
	return n, errors.Join(failed...)
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

// extrasOK limits what a guest can ask for: at most 10 extras, 1 to 10 of each.
func extrasOK(in bookIn) bool {
	if len(in.Extras) > 10 {
		return false
	}
	for _, x := range in.Extras {
		if x.Quantity < 1 || x.Quantity > 10 {
			return false
		}
	}
	return true
}

// photo serves one picture of the hotel. The type was checked when the picture was saved, and the headers keep
// the browser from guessing another one.
func (h *handler) photo(w http.ResponseWriter, r *http.Request) {
	org, prop, ok := h.resolve(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
		return
	}
	var ctype string
	var data []byte
	err = db.WithOrg(r.Context(), h.Pool, org, func(tx pgx.Tx) error {
		var e error
		ctype, data, e = siteprofile.PhotoData(r.Context(), tx, prop, id)
		return e
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
		return
	}
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(data)
}

// assess scores the fraud risk of a new booking and keeps the score. A booking is never turned away because of it.
// The caller asks a risky card for a 3-D Secure check, and a high score waits in the managers' queue.
func (h *handler) assess(ctx context.Context, tx pgx.Tx, org, prop, res uuid.UUID, in bookIn, r *http.Request, now time.Time) (risk.Result, error) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	ipHash, mailHash := risk.Hash(ip), risk.Hash(in.Guest.Email)
	fromAddr, forEmail, err := risk.Counts(ctx, tx, prop, ipHash, mailHash, now)
	if err != nil {
		return risk.Result{}, err
	}
	nights, days := 0, 1<<20
	for _, rm := range in.Rooms {
		from, e1 := time.Parse("2006-01-02", rm.From)
		to, e2 := time.Parse("2006-01-02", rm.To)
		if e1 != nil || e2 != nil {
			continue
		}
		nights = max(nights, int(to.Sub(from).Hours()/24))
		days = min(days, int(from.Sub(now.UTC().Truncate(24*time.Hour)).Hours()/24))
	}
	out := risk.Score(risk.Signals{Email: in.Guest.Email, Name: in.Guest.Name, Nights: nights, DaysToArrival: days, Card: in.PaymentKind == "card",
		FromAddressLastHour: fromAddr, ForEmailLastDay: forEmail})
	return out, risk.Record(ctx, tx, org, prop, res, ipHash, mailHash, out, now)
}
