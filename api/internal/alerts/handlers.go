package alerts

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
)

type handler struct{ httpx.Deps }

func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	// Every role has at least one of these, so any signed-in staff member may open the alerts routes;
	// what they see is limited to alerts meant for their role.
	anyRole := func(f http.HandlerFunc) http.HandlerFunc {
		return httpx.Wrap(access.RequireAny(d, access.ReservationsView, access.HousekeepingView, access.RatesEdit)(f))
	}
	mux.HandleFunc("/api/alerts", httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: anyRole(h.list)}))
	mux.HandleFunc("/api/alerts/{id}/apply", httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: anyRole(h.apply)}))
	mux.HandleFunc("/api/alerts/{id}/dismiss", httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: anyRole(h.dismiss)}))
	mux.HandleFunc("/api/alerts/{id}/snooze", httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: anyRole(h.snooze)}))
	mux.HandleFunc("/api/check-settings", httpx.Methods(map[string]http.HandlerFunc{
		http.MethodGet: httpx.Wrap(access.Require(d, access.UsersManage)(http.HandlerFunc(h.getSettings))),
		http.MethodPut: httpx.Wrap(access.Require(d, access.UsersManage)(http.HandlerFunc(h.putSettings))),
	}))
}

func bad(w http.ResponseWriter, msg string) {
	httpx.Error(w, http.StatusBadRequest, "bad_request", msg)
}

func notFound(w http.ResponseWriter) {
	httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
}

func scopeOf(w http.ResponseWriter, r *http.Request) (access.Actor, bool) {
	a := access.ActorFrom(r.Context())
	if a.PropertyID == uuid.Nil {
		bad(w, "Choose a property first.")
		return a, false
	}
	return a, true
}

type alertOut struct {
	ID          uuid.UUID       `json:"id"`
	CheckKind   string          `json:"checkKind"`
	SubjectType string          `json:"subjectType"`
	SubjectID   uuid.UUID       `json:"subjectId"`
	Severity    string          `json:"severity"`
	Reason      json.RawMessage `json:"reason"`
	Action      json.RawMessage `json:"action"`
	Status      string          `json:"status"`
	TargetRole  string          `json:"targetRole"`
	OpenedAt    time.Time       `json:"openedAt"`
}

// visible is the rule for who may see an alert: the role it is meant for, and owners, who work across every role.
const visible = `(a.target_role = $2 or $2 = 'owner')`

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	a, ok := scopeOf(w, r)
	if !ok {
		return
	}
	out := []alertOut{}
	var lastRun *time.Time
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(),
			`select a.id, a.check_kind, a.subject_type, a.subject_id, a.severity, a.reason, a.suggested_action, a.status, a.target_role, a.opened_at from alert a
			 where a.property_id = $1 and a.status = 'open' and `+visible+`
			 order by case a.severity when 'urgent' then 0 when 'warning' then 1 else 2 end, a.opened_at`, a.PropertyID, string(a.Role))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x alertOut
			if err := rows.Scan(&x.ID, &x.CheckKind, &x.SubjectType, &x.SubjectID, &x.Severity, &x.Reason, &x.Action, &x.Status, &x.TargetRole, &x.OpenedAt); err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	_ = lastRun
	httpx.JSON(w, http.StatusOK, out)
}

// load reads one alert the person may see, locking it. Anything else is "not found".
func load(r *http.Request, tx pgx.Tx, a access.Actor, id uuid.UUID) (status string, action *Action, kind string, err error) {
	var raw []byte
	err = tx.QueryRow(r.Context(),
		`select a.status, a.suggested_action, a.check_kind from alert a where a.id = $1 and a.property_id = $3 and `+visible+` for update`, id, string(a.Role), a.PropertyID).Scan(&status, &raw, &kind)
	if err != nil {
		return "", nil, "", errNotFound
	}
	if raw != nil {
		var act Action
		if json.Unmarshal(raw, &act) == nil {
			action = &act
		}
	}
	return
}

var (
	errNotFound = errors.New("alerts: not found")
	errClosed   = errors.New("alerts: already closed")
	errNoAction = errors.New("alerts: this alert has no action to apply")
)

func (h *handler) answer(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, errNotFound), errors.Is(err, pgx.ErrNoRows):
		notFound(w)
	case errors.Is(err, errClosed):
		httpx.Error(w, http.StatusConflict, "already_closed", "That alert is already closed.")
	case errors.Is(err, errNoAction):
		bad(w, "That alert has no button to press; open the screen it points to.")
	case errors.Is(err, booking.ErrRoomTaken), errors.Is(err, booking.ErrRoomNotReady), errors.Is(err, booking.ErrBadStatus), errors.Is(err, booking.ErrTooEarly), errors.Is(err, booking.ErrNotConfirmed):
		httpx.Error(w, http.StatusConflict, "action_not_possible", "The situation changed, so that fix no longer works. Check the booking.")
	default:
		httpx.InternalError(w, err)
	}
}

func (h *handler) apply(w http.ResponseWriter, r *http.Request) {
	a, ok := scopeOf(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		notFound(w)
		return
	}
	ctx := r.Context()
	err = db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		status, action, kind, err := load(r, tx, a, id)
		if err != nil {
			return err
		}
		if status != "open" {
			return errClosed
		}
		if action == nil || action.Kind == "open" {
			return errNoAction
		}
		reservation, _ := uuid.Parse(str(action.Params["reservationId"]))
		switch action.Kind {
		case "assign_room":
			room, _ := uuid.Parse(str(action.Params["roomId"]))
			err = booking.Assign(ctx, tx, a.PropertyID, reservation, room, nil, false, "")
		case "no_show":
			err = booking.NoShow(ctx, tx, a.OrgID, a.PropertyID, reservation, h.Clock())
		default:
			err = errNoAction
		}
		if err != nil {
			return err
		}
		if err := closeAlert(ctx, tx, a.OrgID, id, "accepted", "applied", &a.UserID, h.Clock()); err != nil {
			return err
		}
		// The trail row names the alert that caused the change.
		return audit.Record(ctx, tx, audit.Entry{Action: "alert.accepted", TargetType: "alert", TargetID: id, TargetLabel: kind,
			After: map[string]any{"alertId": id, "action": action.Kind, "changes": action.Changes}})
	})
	h.answer(w, err)
}

func str(v any) string { s, _ := v.(string); return s }

func (h *handler) dismiss(w http.ResponseWriter, r *http.Request) {
	a, ok := scopeOf(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		notFound(w)
		return
	}
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		status, _, kind, err := load(r, tx, a, id)
		if err != nil {
			return err
		}
		if status != "open" && status != "snoozed" {
			return errClosed
		}
		if err := closeAlert(r.Context(), tx, a.OrgID, id, "dismissed", "dismissed", &a.UserID, h.Clock()); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "alert.dismissed", TargetType: "alert", TargetID: id, TargetLabel: kind})
	})
	h.answer(w, err)
}

func (h *handler) snooze(w http.ResponseWriter, r *http.Request) {
	a, ok := scopeOf(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		notFound(w)
		return
	}
	var in struct{ Hours int }
	if !httpx.Decode(w, r, &in) {
		return
	}
	if in.Hours < 1 || in.Hours > 24*7 {
		bad(w, "Snooze for 1 hour up to 7 days.")
		return
	}
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		status, _, kind, err := load(r, tx, a, id)
		if err != nil {
			return err
		}
		if status != "open" {
			return errClosed
		}
		if _, err := tx.Exec(r.Context(), `update alert set status = 'snoozed', snoozed_until = $2 where id = $1`, id, h.Clock().Add(time.Duration(in.Hours)*time.Hour)); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "alert.snoozed", TargetType: "alert", TargetID: id, TargetLabel: kind, After: map[string]int{"hours": in.Hours}})
	})
	h.answer(w, err)
}

type settingOut struct {
	CheckKind  string             `json:"checkKind"`
	Enabled    bool               `json:"enabled"`
	Thresholds map[string]float64 `json:"thresholds"`
	Last30Days map[string]int     `json:"last30Days"` // alerts by how they ended: opened, accepted, dismissed, closed
	Noisy      bool               `json:"noisy"`      // staff dismiss more than half of its alerts
}

func (h *handler) getSettings(w http.ResponseWriter, r *http.Request) {
	a, ok := scopeOf(w, r)
	if !ok {
		return
	}
	out := []settingOut{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		for _, c := range registry {
			enabled, th, err := settings(r.Context(), tx, a.PropertyID, c)
			if err != nil {
				return err
			}
			s := settingOut{CheckKind: c.kind, Enabled: enabled, Thresholds: th, Last30Days: map[string]int{}}
			rows, err := tx.Query(r.Context(), `select status, count(*) from alert where property_id = $1 and check_kind = $2 and opened_at >= $3 group by status`, a.PropertyID, c.kind, h.Clock().Add(-30*24*time.Hour))
			if err != nil {
				return err
			}
			total := 0
			for rows.Next() {
				var status string
				var n int
				if err := rows.Scan(&status, &n); err != nil {
					rows.Close()
					return err
				}
				s.Last30Days[status] = n
				total += n
			}
			rows.Close()
			s.Last30Days["opened"] = total
			s.Noisy = total >= 4 && s.Last30Days["dismissed"]*2 > total
			out = append(out, s)
		}
		return nil
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// putSettings switches a check on or off and changes its numbers. Turning a check off closes its open alerts.
func (h *handler) putSettings(w http.ResponseWriter, r *http.Request) {
	a, ok := scopeOf(w, r)
	if !ok {
		return
	}
	var in struct {
		CheckKind  string             `json:"checkKind"`
		Enabled    bool               `json:"enabled"`
		Thresholds map[string]float64 `json:"thresholds"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	var c *check
	for i := range registry {
		if registry[i].kind == in.CheckKind {
			c = &registry[i]
		}
	}
	if c == nil {
		bad(w, "That check does not exist.")
		return
	}
	for k, v := range in.Thresholds {
		if _, known := c.defaults[k]; !known || v < 0 || v > 1000 {
			bad(w, "That number does not belong to this check.")
			return
		}
	}
	raw, _ := json.Marshal(in.Thresholds)
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(),
			`insert into check_setting (property_id, organization_id, check_kind, enabled, thresholds) values ($1, $2, $3, $4, $5)
			 on conflict (property_id, check_kind) do update set enabled = excluded.enabled, thresholds = excluded.thresholds`,
			a.PropertyID, a.OrgID, in.CheckKind, in.Enabled, string(raw)); err != nil {
			return err
		}
		if !in.Enabled {
			rows, err := tx.Query(r.Context(), `select id from alert where property_id = $1 and check_kind = $2 and status in ('open', 'snoozed')`, a.PropertyID, in.CheckKind)
			if err != nil {
				return err
			}
			var open []uuid.UUID
			for rows.Next() {
				var id uuid.UUID
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				open = append(open, id)
			}
			rows.Close()
			for _, id := range open {
				if err := closeAlert(r.Context(), tx, a.OrgID, id, "closed", "check_off", &a.UserID, h.Clock()); err != nil {
					return err
				}
			}
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "check_setting.changed", TargetType: "property", TargetID: a.PropertyID, TargetLabel: in.CheckKind,
			After: map[string]any{"enabled": in.Enabled, "thresholds": in.Thresholds}})
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
