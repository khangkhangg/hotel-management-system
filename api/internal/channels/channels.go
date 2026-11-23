package channels

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
)

const (
	// Horizon is how many nights ahead are pushed.
	Horizon = 60
	// pullOverlap makes each pull start a little before the last one, so a booking made while it ran is not missed.
	pullOverlap = 10 * time.Minute
	// reconcileWindow is how far back the nightly check reads.
	reconcileWindow = 14 * 24 * time.Hour
)

// modes says which providers each way of connecting may use.
var modes = map[string][]string{
	"channel_manager": {"channex", "beds24", "cm_generic", "fake"},
	"direct":          {"agoda", "booking_com", "expedia", "google", "fake"},
}

type handler struct {
	httpx.Deps
	factory Factory
}

// Mount adds the channel routes for the people who manage channels.
func Mount(factory Factory) httpx.Mount {
	return func(mux *http.ServeMux, d httpx.Deps) {
		h := &handler{Deps: d, factory: factory}
		wrap := func(f http.HandlerFunc) http.HandlerFunc {
			return httpx.Wrap(access.RequireAny(d, access.ChannelsManage)(f))
		}
		mux.HandleFunc("/api/channel", httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: wrap(h.get), http.MethodPut: wrap(h.put)}))
		mux.HandleFunc("/api/channel/map", httpx.Methods(map[string]http.HandlerFunc{http.MethodPut: wrap(h.putMap)}))
		mux.HandleFunc("/api/channel/sync", httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: wrap(h.syncNow)}))
		mux.HandleFunc("/api/channel/providers", httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: wrap(h.providers)}))
		mux.HandleFunc("/api/channel/test", httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: wrap(h.test)}))
		mux.HandleFunc("/api/channel/remote", httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: wrap(h.remote)}))
		mux.HandleFunc("/api/channel/log", httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: wrap(h.log)}))
	}
}

func scope(w http.ResponseWriter, r *http.Request) (access.Actor, bool) {
	a := access.ActorFrom(r.Context())
	if a.PropertyID == uuid.Nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Choose a property first.")
		return a, false
	}
	return a, true
}

func internal(w http.ResponseWriter) {
	httpx.Error(w, http.StatusInternalServerError, "internal", "Something went wrong.")
}

type connectionOut struct {
	Mode            string `json:"mode"`
	Provider        string `json:"provider"`
	ExternalHotelID string `json:"externalHotelId"`
	BaseURL         string `json:"baseUrl"`
	HasSecret       bool   `json:"hasSecret"` // the name of the setting is not shown, only whether one is named
	Enabled         bool   `json:"enabled"`
}

type mapOut struct {
	RatePlanID     uuid.UUID `json:"ratePlanId"`
	RoomTypeID     uuid.UUID `json:"roomTypeId"`
	ExternalRoomID string    `json:"externalRoomId"`
	ExternalRateID string    `json:"externalRateId"`
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	out := struct {
		Connection *connectionOut `json:"connection"`
		Maps       []mapOut       `json:"maps"`
	}{Maps: []mapOut{}}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var c connectionOut
		var secret string
		err := tx.QueryRow(r.Context(), `select mode, provider, external_hotel_id, base_url, secret_setting, enabled from channel_connection where property_id = $1`, a.PropertyID).
			Scan(&c.Mode, &c.Provider, &c.ExternalHotelID, &c.BaseURL, &secret, &c.Enabled)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		c.HasSecret = secret != ""
		out.Connection = &c
		rows, err := tx.Query(r.Context(), `select m.rate_plan_id, m.room_type_id, m.external_room_id, m.external_rate_id from channel_map m join channel_connection c on c.id = m.connection_id where c.property_id = $1 order by m.external_room_id`, a.PropertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m mapOut
			if err := rows.Scan(&m.RatePlanID, &m.RoomTypeID, &m.ExternalRoomID, &m.ExternalRateID); err != nil {
				return err
			}
			out.Maps = append(out.Maps, m)
		}
		return rows.Err()
	})
	if err != nil {
		internal(w)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

type connectionIn struct {
	Mode            string `json:"mode"`
	Provider        string `json:"provider"`
	ExternalHotelID string `json:"externalHotelId"`
	BaseURL         string `json:"baseUrl"`
	SecretSetting   string `json:"secretSetting"`
	Enabled         bool   `json:"enabled"`
}

// validBaseURL accepts nothing or an https address of a named host. A staff member must not be able to point the
// server at its own network, so addresses written as IP numbers and local names are refused.
func validBaseURL(s string) bool {
	if s == "" {
		return true
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || len(s) > 300 {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return net.ParseIP(host) == nil && host != "localhost" && !strings.HasSuffix(host, ".local") && !strings.HasSuffix(host, ".internal")
}

func (in *connectionIn) valid() bool {
	ok := false
	for _, p := range modes[in.Mode] {
		ok = ok || p == in.Provider
	}
	return ok && len(in.ExternalHotelID) <= 100 && len(in.SecretSetting) <= 100 && validBaseURL(in.BaseURL)
}

func (h *handler) put(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var in connectionIn
	if !httpx.Decode(w, r, &in) {
		return
	}
	if !in.valid() {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Pick a channel manager or an OTA that goes with the way you connect, and use an https address.")
		return
	}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		// A different provider means different room and rate codes, so the old mapping goes with it.
		if _, err := tx.Exec(r.Context(), `delete from channel_map where connection_id in (select id from channel_connection where property_id = $1 and provider <> $2)`, a.PropertyID, in.Provider); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `insert into channel_connection (id, organization_id, property_id, mode, provider, external_hotel_id, base_url, secret_setting, enabled, created_at)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			on conflict (property_id) do update set mode = excluded.mode, provider = excluded.provider, external_hotel_id = excluded.external_hotel_id,
			  base_url = excluded.base_url, secret_setting = excluded.secret_setting, enabled = excluded.enabled`,
			ids.New(), a.OrgID, a.PropertyID, in.Mode, in.Provider, in.ExternalHotelID, in.BaseURL, in.SecretSetting, in.Enabled, h.Clock()); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "channel.connection_saved", TargetType: "channel", TargetID: a.PropertyID, TargetLabel: in.Provider,
			After: map[string]any{"mode": in.Mode, "provider": in.Provider, "enabled": in.Enabled}})
	})
	if err != nil {
		internal(w)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *handler) putMap(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var in struct {
		RatePlanID     uuid.UUID `json:"ratePlanId"`
		ExternalRoomID string    `json:"externalRoomId"`
		ExternalRateID string    `json:"externalRateId"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.ExternalRoomID, in.ExternalRateID = strings.TrimSpace(in.ExternalRoomID), strings.TrimSpace(in.ExternalRateID)
	if in.ExternalRoomID == "" || len(in.ExternalRoomID) > 100 || len(in.ExternalRateID) > 100 {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Give the room code from the other side.")
		return
	}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `insert into channel_map (connection_id, organization_id, room_type_id, rate_plan_id, external_room_id, external_rate_id)
			select c.id, c.organization_id, p.room_type_id, p.id, $3, $4 from channel_connection c, rate_plan p where c.property_id = $1 and p.id = $2 and p.property_id = $1
			on conflict (connection_id, rate_plan_id) do update set external_room_id = excluded.external_room_id, external_rate_id = excluded.external_rate_id`,
			a.PropertyID, in.RatePlanID, in.ExternalRoomID, in.ExternalRateID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "channel.mapped", TargetType: "rate_plan", TargetID: in.RatePlanID, After: map[string]any{"room": in.ExternalRoomID, "rate": in.ExternalRateID}})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "not_found", "Save the connection first, and choose one of this property's rate plans.")
		return
	}
	if err != nil {
		internal(w)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

type logOut struct {
	Direction string    `json:"direction"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	At        time.Time `json:"at"`
}

func (h *handler) log(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	out := []logOut{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select direction, status, message, created_at from sync_log where property_id = $1 order by created_at desc, id desc limit 200`, a.PropertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var l logOut
			if err := rows.Scan(&l.Direction, &l.Status, &l.Message, &l.At); err != nil {
				return err
			}
			out = append(out, l)
		}
		return rows.Err()
	})
	if err != nil {
		internal(w)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *handler) syncNow(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	res, err := Sync(r.Context(), h.Pool, h.factory, a.OrgID, a.PropertyID, h.Clock())
	switch {
	case errors.Is(err, ErrOff):
		httpx.Error(w, http.StatusConflict, "channel_off", "Switch the connection on first.")
	case errors.Is(err, ErrUnavailable):
		httpx.Error(w, http.StatusConflict, "provider_unavailable", "This provider is not available on this server yet.")
	case err != nil:
		internal(w)
	default:
		httpx.JSON(w, http.StatusOK, res)
	}
}

// Result says what one sync did.
type Result struct {
	Pushed int `json:"pushed"`
	Pulled int `json:"pulled"`
}

// ErrOff means the property has no enabled connection.
var ErrOff = errors.New("channels: connection is off")

type mapping struct {
	roomType, ratePlan uuid.UUID
	room, rate         string
}

type loaded struct {
	id    uuid.UUID
	conn  Connection
	maps  []mapping
	slots []Slot
	since time.Time
	today time.Time
}

func (l *loaded) mapFor(b Booking) (mapping, bool) {
	for _, m := range l.maps {
		if m.room == b.ExternalRoomID && (b.ExternalRateID == "" || m.rate == "" || m.rate == b.ExternalRateID) {
			return m, true
		}
	}
	return mapping{}, false
}

// Sync pushes the property's nights and pulls its bookings. The calls to the other side run outside any
// transaction, and each pulled booking is saved in its own, so one that cannot be kept does not stop the rest.
func Sync(ctx context.Context, pool *pgxpool.Pool, factory Factory, org, prop uuid.UUID, now time.Time) (Result, error) {
	return syncWith(ctx, pool, factory, org, prop, now, false)
}

// Reconcile is the nightly check. It reads the last two weeks again and fixes what the live messages missed:
// a moved stay, a cancellation, or a booking that never arrived.
func Reconcile(ctx context.Context, pool *pgxpool.Pool, factory Factory, org, prop uuid.UUID, now time.Time) (Result, error) {
	return syncWith(ctx, pool, factory, org, prop, now, true)
}

func syncWith(ctx context.Context, pool *pgxpool.Pool, factory Factory, org, prop uuid.UUID, now time.Time, full bool) (Result, error) {
	var l loaded
	err := db.WithOrg(ctx, pool, org, func(tx pgx.Tx) error { return load(ctx, tx, org, prop, now, &l) })
	if err != nil {
		return Result{}, err
	}
	if full {
		l.since = now.Add(-reconcileWindow)
	}
	ad, err := factory(l.conn)
	if err != nil {
		return Result{}, err
	}
	var res Result
	pushErr := ad.PushAvailability(ctx, l.slots)
	if pushErr == nil {
		res.Pushed = len(l.slots)
	}
	bookings, pullErr := ad.PullBookings(ctx, l.since)
	var notes []logLine
	if pushErr != nil {
		notes = append(notes, logLine{"push", "error", "Sending rooms and prices failed: " + pushErr.Error()})
	} else {
		notes = append(notes, logLine{"push", "ok", fmt.Sprintf("Sent %d nights.", len(l.slots))})
	}
	if pullErr != nil {
		notes = append(notes, logLine{"pull", "error", "Getting bookings failed: " + pullErr.Error()})
	}
	var kept []string
	for _, b := range bookings {
		if full && b.Status == "new" {
			b.Status = "modified" // apply then compares the stay with ours instead of skipping a booking it knows
		}
		made, err := apply(ctx, pool, org, prop, &l, b, now)
		switch {
		case err != nil:
			// Not acknowledged, so a channel that hands a booking out until it is saved will hand this one out again.
			notes = append(notes, logLine{"pull", "error", fmt.Sprintf("Booking %s could not be kept: %s. Handle it by hand.", b.ExternalID, reason(err, b))})
			continue
		case made:
			res.Pulled++
			notes = append(notes, logLine{"pull", "ok", fmt.Sprintf("Booking %s (%s) saved.", b.ExternalID, b.Status)})
		}
		if b.AckID != "" {
			kept = append(kept, b.AckID)
		}
	}
	if ak, ok := ad.(Acker); ok && len(kept) > 0 {
		if err := ak.Ack(ctx, kept); err != nil {
			notes = append(notes, logLine{"pull", "error", "Telling the channel which bookings were saved failed: " + err.Error()})
		}
	}
	err = db.WithOrg(ctx, pool, org, func(tx pgx.Tx) error {
		for _, n := range notes {
			if _, err := tx.Exec(ctx, `insert into sync_log (id, organization_id, property_id, connection_id, direction, status, message, created_at) values ($1, $2, $3, $4, $5, $6, $7, $8)`,
				ids.New(), org, prop, l.id, n.direction, n.status, n.message, now); err != nil {
				return err
			}
		}
		return nil
	})
	return res, err
}

type logLine struct{ direction, status, message string }

func reason(err error, b Booking) string {
	switch {
	case b.Problem != "":
		return b.Problem
	case errors.Is(err, booking.ErrSoldOut):
		return "the room is already sold for those nights"
	case errors.Is(err, errNotMapped):
		return "its room is not mapped to one of your rate plans"
	case errors.Is(err, booking.ErrStayRule):
		return "the stay breaks a rate rule"
	case errors.Is(err, booking.ErrPast):
		return "the stay starts in the past"
	default:
		return "an unexpected problem"
	}
}

var errNotMapped = errors.New("channels: room not mapped")

// loadConnection reads the property's connection. With needOn it also insists that the connection is switched on.
func loadConnection(ctx context.Context, tx pgx.Tx, prop uuid.UUID, l *loaded, needOn bool) error {
	var enabled bool
	if err := tx.QueryRow(ctx, `select id, mode, provider, external_hotel_id, base_url, secret_setting, enabled from channel_connection where property_id = $1`, prop).
		Scan(&l.id, &l.conn.Mode, &l.conn.Provider, &l.conn.ExternalHotelID, &l.conn.BaseURL, &l.conn.SecretSetting, &enabled); err != nil {
		return ErrOff
	}
	if needOn && !enabled {
		return ErrOff
	}
	return nil
}

func load(ctx context.Context, tx pgx.Tx, org, prop uuid.UUID, now time.Time, l *loaded) error {
	if err := loadConnection(ctx, tx, prop, l, true); err != nil {
		return err
	}
	today, currency, err := booking.HotelToday(ctx, tx, org, prop, now)
	if err != nil {
		return err
	}
	l.today = today
	rows, err := tx.Query(ctx, `select room_type_id, rate_plan_id, external_room_id, external_rate_id from channel_map where connection_id = $1`, l.id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var m mapping
		if err := rows.Scan(&m.roomType, &m.ratePlan, &m.room, &m.rate); err != nil {
			rows.Close()
			return err
		}
		l.maps = append(l.maps, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, m := range l.maps {
		s, err := tx.Query(ctx, `select to_char(i.date, 'YYYY-MM-DD'), case when r.closed then 0 else greatest(i.total - i.sold - i.blocked, 0) end, r.price
			from inventory i join rate r on r.rate_plan_id = $1 and r.date = i.date
			where i.room_type_id = $2 and i.date >= $3::date and i.date < $3::date + $4::int order by i.date`, m.ratePlan, m.roomType, today, Horizon)
		if err != nil {
			return err
		}
		for s.Next() {
			sl := Slot{ExternalRoomID: m.room, ExternalRateID: m.rate, Currency: currency}
			if err := s.Scan(&sl.Date, &sl.Available, &sl.Price); err != nil {
				s.Close()
				return err
			}
			l.slots = append(l.slots, sl)
		}
		s.Close()
		if err := s.Err(); err != nil {
			return err
		}
	}
	var last *time.Time
	if err := tx.QueryRow(ctx, `select max(created_at) from sync_log where connection_id = $1 and direction = 'pull' and status = 'ok'`, l.id).Scan(&last); err != nil {
		return err
	}
	l.since = now.Add(-24 * time.Hour)
	if last != nil {
		l.since = last.Add(-pullOverlap)
	}
	return nil
}

// apply saves one pulled booking. It reports whether anything changed: a booking already saved changes nothing.
func apply(ctx context.Context, pool *pgxpool.Pool, org, prop uuid.UUID, l *loaded, b Booking, now time.Time) (changed bool, err error) {
	if b.ExternalID == "" || len(b.ExternalID) > 100 {
		return false, booking.ErrBad
	}
	if b.Problem != "" {
		return false, errProblem
	}
	err = db.WithOrg(ctx, pool, org, func(tx pgx.Tx) error {
		var id uuid.UUID
		var status, from, to string
		e := tx.QueryRow(ctx, `select r.id, r.status, coalesce(min(lower(x.stay))::text, ''), coalesce(max(upper(x.stay))::text, '')
			from reservation r left join reservation_room x on x.reservation_id = r.id
			where r.property_id = $1 and r.source_channel = $2 and r.channel_booking_ref = $3 group by r.id order by r.created_at desc limit 1`,
			prop, l.conn.Provider, b.ExternalID).Scan(&id, &status, &from, &to)
		exists := e == nil
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		switch {
		case b.Status == "cancelled":
			if !exists || status == "cancelled" {
				return nil
			}
			if err := booking.Cancel(ctx, tx, org, prop, id, now); err != nil {
				return err
			}
			changed = true
			return nil
		case exists && b.Status != "modified":
			return nil // a booking already saved, or one that was cancelled and must not come back
		case exists && status != "cancelled" && from == b.Arrival && to == b.Departure:
			return nil // a modification that is already in
		case exists && status != "cancelled":
			// A modification moves the stay: free the old nights, then take the new ones. If the new ones are
			// taken, the whole step rolls back and the old stay stays.
			if err := booking.Cancel(ctx, tx, org, prop, id, now); err != nil {
				return err
			}
		}
		m, ok := l.mapFor(b)
		if !ok {
			return errNotMapped
		}
		res, err := booking.Create(ctx, tx, org, prop, booking.CreateInput{
			Guest:  booking.GuestInput{Name: b.GuestName, Email: b.Email},
			Rooms:  []booking.RoomRequest{{RoomTypeID: m.roomType, RatePlanID: m.ratePlan, From: b.Arrival, To: b.Departure, Adults: max(b.Adults, 1), Children: b.Children}},
			Source: l.conn.Provider,
		}, now)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update reservation set channel_booking_ref = $2 where id = $1`, res.ID, b.ExternalID); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed, err
}

var errProblem = errors.New("channels: the booking needs staff")

// RunAll syncs every enabled connection of every organization and says how many it reached. A connection that fails
// is written to its own log and does not stop the others.
func RunAll(ctx context.Context, pool *pgxpool.Pool, factory Factory, now time.Time) (int, error) {
	return runEach(ctx, pool, factory, now, Sync)
}

// RunNightly runs the nightly check on every enabled connection.
func RunNightly(ctx context.Context, pool *pgxpool.Pool, factory Factory, now time.Time) (int, error) {
	return runEach(ctx, pool, factory, now, Reconcile)
}

func runEach(ctx context.Context, pool *pgxpool.Pool, factory Factory, now time.Time, run func(context.Context, *pgxpool.Pool, Factory, uuid.UUID, uuid.UUID, time.Time) (Result, error)) (int, error) {
	type pair struct{ org, prop uuid.UUID }
	var todo []pair
	err := db.WithoutOrg(ctx, pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select organization_id, property_id from all_channel_connections()`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p pair
			if err := rows.Scan(&p.org, &p.prop); err != nil {
				return err
			}
			todo = append(todo, p)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	done := 0
	for _, p := range todo {
		if _, err := run(ctx, pool, factory, p.org, p.prop, now); err == nil {
			done++
		}
	}
	return done, nil
}

// RunSweeper runs RunAll on a timer and RunNightly once a day, until the context ends.
func RunSweeper(ctx context.Context, pool *pgxpool.Pool, factory Factory, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	night := time.NewTicker(24 * time.Hour)
	defer night.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = RunAll(ctx, pool, factory, time.Now())
		case <-night.C:
			_, _ = RunNightly(ctx, pool, factory, time.Now())
		}
	}
}
