package folio

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
	"hotelpms/api/internal/payments"
)

type handler struct {
	httpx.Deps
	card payments.Provider // nil when no card provider is configured
}

func Mount(mux *http.ServeMux, d httpx.Deps) { mount(mux, d, nil) }

// MountWith adds the folio routes for a server that takes card payments, so card refunds go to the provider.
func MountWith(card payments.Provider) httpx.Mount {
	return func(mux *http.ServeMux, d httpx.Deps) { mount(mux, d, card) }
}

// pay sends a refund to the card provider, or is nil when there is none.
func (h *handler) pay() Pay {
	if h.card == nil {
		return nil
	}
	return func(ctx context.Context, ref string, amount int64, key string) error {
		_, err := h.card.RefundPayment(ctx, ref, amount, key)
		return err
	}
}

func mount(mux *http.ServeMux, d httpx.Deps, card payments.Provider) {
	h := &handler{d, card}
	route := func(methods map[string]http.HandlerFunc, perms map[string][]access.Permission) http.HandlerFunc {
		m := map[string]http.HandlerFunc{}
		for method, f := range methods {
			m[method] = httpx.Wrap(access.RequireAny(d, perms[method]...)(f))
		}
		return httpx.Methods(m)
	}
	one := func(method string, f http.HandlerFunc, ps ...access.Permission) http.HandlerFunc {
		return route(map[string]http.HandlerFunc{method: f}, map[string][]access.Permission{method: ps})
	}
	mux.HandleFunc("/api/reservations/{id}/folio", one(http.MethodGet, h.get, access.ReservationsView, access.PaymentsTake))
	mux.HandleFunc("/api/folios/{id}/lines", one(http.MethodPost, h.addLine, access.ReservationsEdit, access.PaymentsTake))
	mux.HandleFunc("/api/folios/{id}/payments", one(http.MethodPost, h.addPayment, access.PaymentsTake))
	mux.HandleFunc("/api/payments/{id}/refunds", one(http.MethodPost, h.refund, access.RefundsRequest))
	mux.HandleFunc("/api/refunds/{id}/approve", one(http.MethodPost, h.decide(true), access.RefundsApprove))
	mux.HandleFunc("/api/refunds/{id}/reject", one(http.MethodPost, h.decide(false), access.RefundsApprove))
	mux.HandleFunc("/api/payments", one(http.MethodGet, h.listPayments, access.PaymentsTake))
	mux.HandleFunc("/api/tax-rules", route(
		map[string]http.HandlerFunc{http.MethodGet: h.listTax, http.MethodPost: h.createTax},
		map[string][]access.Permission{http.MethodGet: {access.ReservationsView, access.PaymentsConfigure}, http.MethodPost: {access.PaymentsConfigure}}))
	mux.HandleFunc("/api/refund-limits", route(
		map[string]http.HandlerFunc{http.MethodGet: h.getLimits, http.MethodPut: h.putLimits},
		map[string][]access.Permission{http.MethodGet: {access.RefundsRequest}, http.MethodPut: {access.PaymentsConfigure}}))
}

func bad(w http.ResponseWriter, msg string) {
	httpx.Error(w, http.StatusBadRequest, "bad_request", msg)
}

func scope(w http.ResponseWriter, r *http.Request) (access.Actor, bool) {
	a := access.ActorFrom(r.Context())
	if a.PropertyID == uuid.Nil {
		bad(w, "Choose a property first.")
		return a, false
	}
	return a, true
}

func fail(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, ErrNotFound), errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
	case errors.Is(err, ErrBad):
		bad(w, "Check the amount, the method and the text.")
	case errors.Is(err, ErrRefundTooLarge):
		httpx.Error(w, http.StatusConflict, "refund_too_large", "That is more than what is left of the payment.")
	case errors.Is(err, ErrProviderRefused):
		httpx.Error(w, http.StatusBadGateway, "provider_refused", "The card provider did not give the money back. Nothing was changed.")
	case errors.Is(err, ErrAlreadyDecided):
		httpx.Error(w, http.StatusConflict, "already_decided", "That refund was already decided.")
	default:
		httpx.InternalError(w, err)
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
		return id, false
	}
	return id, true
}

type lineOut struct {
	ID          uuid.UUID `json:"id"`
	Kind        string    `json:"kind"`
	Description string    `json:"description"`
	Quantity    int       `json:"quantity"`
	UnitPrice   int64     `json:"unitPrice"`
	Amount      int64     `json:"amount"`
	TaxIncluded int64     `json:"taxIncluded"`
}

type refundOut struct {
	ID        uuid.UUID `json:"id"`
	Amount    int64     `json:"amount"`
	Reason    string    `json:"reason"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type paymentOut struct {
	ID        uuid.UUID   `json:"id"`
	Method    string      `json:"method"`
	Amount    int64       `json:"amount"`
	Note      string      `json:"note"`
	Status    string      `json:"status"`
	CreatedAt time.Time   `json:"createdAt"`
	Refunds   []refundOut `json:"refunds"`
}

type viewOut struct {
	FolioID  uuid.UUID    `json:"folioId"`
	Currency string       `json:"currency"`
	Lines    []lineOut    `json:"lines"`
	Payments []paymentOut `json:"payments"`
	Total    int64        `json:"total"`
	Paid     int64        `json:"paid"`
	Refunded int64        `json:"refunded"`
	Balance  int64        `json:"balance"`
}

// get returns the folio. The total is always the sum of its lines; nothing here is stored as a total.
func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	res, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	out := viewOut{Lines: []lineOut{}, Payments: []paymentOut{}}
	err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var err error
		if out.FolioID, out.Currency, err = Ensure(ctx, tx, a.OrgID, a.PropertyID, res, h.Clock()); err != nil {
			return err
		}
		lines, err := tx.Query(ctx, `select id, kind, description, quantity, unit_price, quantity * unit_price, tax_included from folio_line where folio_id = $1 order by auto desc, night nulls last, created_at, id`, out.FolioID)
		if err != nil {
			return err
		}
		for lines.Next() {
			var l lineOut
			if err := lines.Scan(&l.ID, &l.Kind, &l.Description, &l.Quantity, &l.UnitPrice, &l.Amount, &l.TaxIncluded); err != nil {
				lines.Close()
				return err
			}
			out.Total += l.Amount
			out.Lines = append(out.Lines, l)
		}
		lines.Close()
		if err := lines.Err(); err != nil {
			return err
		}
		pays, err := tx.Query(ctx, `select id, method, amount, note, status, created_at from payment where folio_id = $1 order by created_at, id`, out.FolioID)
		if err != nil {
			return err
		}
		for pays.Next() {
			p := paymentOut{Refunds: []refundOut{}}
			if err := pays.Scan(&p.ID, &p.Method, &p.Amount, &p.Note, &p.Status, &p.CreatedAt); err != nil {
				pays.Close()
				return err
			}
			out.Paid += p.Amount
			out.Payments = append(out.Payments, p)
		}
		pays.Close()
		if err := pays.Err(); err != nil {
			return err
		}
		for i := range out.Payments {
			rs, err := tx.Query(ctx, `select id, amount, reason, status, created_at from refund where payment_id = $1 order by created_at, id`, out.Payments[i].ID)
			if err != nil {
				return err
			}
			for rs.Next() {
				var x refundOut
				if err := rs.Scan(&x.ID, &x.Amount, &x.Reason, &x.Status, &x.CreatedAt); err != nil {
					rs.Close()
					return err
				}
				if x.Status == "done" {
					out.Refunded += x.Amount
				}
				out.Payments[i].Refunds = append(out.Payments[i].Refunds, x)
			}
			rs.Close()
		}
		out.Balance = out.Total - out.Paid + out.Refunded
		return nil
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusOK, out)
	}
}

func (h *handler) addLine(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Kind, Description string
		Quantity          int
		UnitPrice         int64 `json:"unitPrice"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	var line uuid.UUID
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) (e error) {
		line, e = AddLine(r.Context(), tx, a.OrgID, a.PropertyID, id, a.UserID, in.Kind, in.Description, in.Quantity, in.UnitPrice, h.Clock())
		return
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusCreated, map[string]any{"id": line})
	}
}

func (h *handler) addPayment(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Method string
		Amount int64
		Note   string
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	var pay uuid.UUID
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) (e error) {
		pay, e = AddPayment(r.Context(), tx, a.OrgID, a.PropertyID, id, a.UserID, in.Method, in.Amount, in.Note, h.Clock())
		return
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusCreated, map[string]any{"id": pay})
	}
}

func (h *handler) refund(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Amount int64
		Reason string
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	var refund uuid.UUID
	var status string
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) (e error) {
		refund, status, e = RefundVia(r.Context(), tx, a.OrgID, a.PropertyID, id, a.UserID, a.Role, in.Amount, in.Reason, h.Clock(), h.pay())
		return
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusCreated, map[string]any{"id": refund, "status": status})
	}
}

func (h *handler) decide(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, ok := scope(w, r)
		if !ok {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
			return DecideVia(r.Context(), tx, a.PropertyID, id, a.UserID, approve, h.Clock(), h.pay())
		})
		if !fail(w, err) {
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

func (h *handler) listTax(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	type row struct {
		ID        uuid.UUID `json:"id"`
		Name      string    `json:"name"`
		Percent   float64   `json:"percent"`
		Inclusive bool      `json:"inclusive"`
		AppliesTo string    `json:"appliesTo"`
	}
	out := []row{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select id, name, percent::float8, inclusive, applies_to from tax_rule where property_id = $1 order by name`, a.PropertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x row
			if err := rows.Scan(&x.ID, &x.Name, &x.Percent, &x.Inclusive, &x.AppliesTo); err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusOK, out)
	}
}

func (h *handler) createTax(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var in struct {
		Name      string
		Percent   float64
		Inclusive bool
		AppliesTo string `json:"appliesTo"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.AppliesTo == "" {
		in.AppliesTo = "all"
	}
	if in.Name == "" || len(in.Name) > 100 || in.Percent < 0 || in.Percent > 100 || (in.AppliesTo != "all" && in.AppliesTo != "room" && in.AppliesTo != "extra") {
		bad(w, "Give a name and a percent from 0 to 100.")
		return
	}
	id := ids.New()
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `insert into tax_rule (id, organization_id, property_id, name, percent, inclusive, applies_to) values ($1, $2, $3, $4, $5, $6, $7)`,
			id, a.OrgID, a.PropertyID, in.Name, in.Percent, in.Inclusive, in.AppliesTo); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "tax_rule.created", TargetType: "tax_rule", TargetID: id, TargetLabel: in.Name, After: in})
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusCreated, map[string]any{"id": id})
	}
}

func (h *handler) getLimits(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var limit int64
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) (e error) {
		limit, e = RefundLimit(r.Context(), tx, a.PropertyID, access.FrontDesk)
		return
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusOK, map[string]int64{"front_desk": limit})
	}
}

func (h *handler) putLimits(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var in map[string]int64
	if !httpx.Decode(w, r, &in) {
		return
	}
	if len(in) == 0 {
		bad(w, "Give a role and an amount.")
		return
	}
	for role, amount := range in {
		if role != "front_desk" || amount < 0 {
			bad(w, "Only the front desk has a limit, and it cannot be below zero.")
			return
		}
	}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		for role, amount := range in {
			if _, err := tx.Exec(r.Context(),
				`insert into refund_limit (property_id, organization_id, role, amount) values ($1, $2, $3, $4)
				 on conflict (property_id, role) do update set amount = excluded.amount`, a.PropertyID, a.OrgID, role, amount); err != nil {
				return err
			}
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "refund_limit.changed", TargetType: "property", TargetID: a.PropertyID, After: in})
	})
	if !fail(w, err) {
		w.WriteHeader(http.StatusNoContent)
	}
}

// listPayments shows what was taken between two dates (the hotel day starts at midnight UTC here), with who paid,
// and the refunds still waiting for approval. The front desk uses it to see the day's takings.
func (h *handler) listPayments(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	from, e1 := time.Parse("2006-01-02", r.URL.Query().Get("from"))
	to, e2 := time.Parse("2006-01-02", r.URL.Query().Get("to"))
	if e1 != nil || e2 != nil || !to.After(from) || to.Sub(from) > 93*24*time.Hour {
		bad(w, "Give a first date and a later last date within 93 days.")
		return
	}
	type pay struct {
		ID            uuid.UUID `json:"id"`
		ReservationID uuid.UUID `json:"reservationId"`
		Code          string    `json:"code"`
		GuestName     string    `json:"guestName"`
		Method        string    `json:"method"`
		Amount        int64     `json:"amount"`
		Status        string    `json:"status"`
		Currency      string    `json:"currency"`
		CreatedAt     time.Time `json:"createdAt"`
	}
	type waiting struct {
		ID            uuid.UUID `json:"id"`
		ReservationID uuid.UUID `json:"reservationId"`
		Code          string    `json:"code"`
		GuestName     string    `json:"guestName"`
		Amount        int64     `json:"amount"`
		Reason        string    `json:"reason"`
		Currency      string    `json:"currency"`
	}
	out := struct {
		Payments       []pay            `json:"payments"`
		PendingRefunds []waiting        `json:"pendingRefunds"`
		Totals         map[string]int64 `json:"totals"`
	}{Payments: []pay{}, PendingRefunds: []waiting{}, Totals: map[string]int64{}}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(),
			`select p.id, r.id, r.code, g.name, p.method, p.amount, p.status, f.currency, p.created_at
			 from payment p join folio f on f.id = p.folio_id join reservation r on r.id = f.reservation_id join guest g on g.id = r.guest_id
			 join property pr on pr.id = f.property_id
			 where f.property_id = $1
			   and p.created_at >= ($2::date::timestamp at time zone pr.time_zone) and p.created_at < ($3::date::timestamp at time zone pr.time_zone)
			 order by p.created_at`, a.PropertyID, from, to)
		if err != nil {
			return err
		}
		for rows.Next() {
			var x pay
			if err := rows.Scan(&x.ID, &x.ReservationID, &x.Code, &x.GuestName, &x.Method, &x.Amount, &x.Status, &x.Currency, &x.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			out.Totals[x.Method] += x.Amount
			out.Payments = append(out.Payments, x)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		rs, err := tx.Query(r.Context(),
			`select rf.id, r.id, r.code, g.name, rf.amount, rf.reason, f.currency
			 from refund rf join payment p on p.id = rf.payment_id join folio f on f.id = p.folio_id join reservation r on r.id = f.reservation_id join guest g on g.id = r.guest_id
			 where f.property_id = $1 and rf.status = 'pending' order by rf.created_at`, a.PropertyID)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var x waiting
			if err := rs.Scan(&x.ID, &x.ReservationID, &x.Code, &x.GuestName, &x.Amount, &x.Reason, &x.Currency); err != nil {
				return err
			}
			out.PendingRefunds = append(out.PendingRefunds, x)
		}
		return rs.Err()
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusOK, out)
	}
}
