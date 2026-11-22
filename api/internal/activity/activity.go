// Package activity serves the activity trail (spec 06): who did what, filtered and paged.
package activity

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
)

const (
	defaultLimit = 50
	maxLimit     = 100
)

// historyPermissions says who may read the history of each kind of target: the people who can read the target itself.
var historyPermissions = map[string]access.Permission{
	"user":    access.UsersManage,
	"booking": access.ReservationsView,
}

type handler struct{ httpx.Deps }

func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	get := func(p access.Permission, f http.HandlerFunc) http.HandlerFunc {
		return httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: httpx.Wrap(access.Require(d, p)(f))})
	}
	mux.HandleFunc("/api/activity", get(access.AuditView, h.list))
	for kind, p := range historyPermissions {
		mux.HandleFunc("/api/activity/target/"+kind+"/{id}", get(p, h.history(kind)))
	}
}

type item struct {
	ID      uuid.UUID `json:"id"`
	At      time.Time `json:"at"`
	Actor   actor     `json:"actor"`
	ViewAs  string    `json:"viewAs"`
	Action  string    `json:"action"`
	Target  target    `json:"target"`
	Before  rawJSON   `json:"before"`
	After   rawJSON   `json:"after"`
	Reason  string    `json:"reason"`
	Device  string    `json:"device"`
	IP      *string   `json:"ip,omitempty"`
	Place   *string   `json:"place,omitempty"`
	TraceID *string   `json:"traceId"`
}

type actor struct {
	ID   *uuid.UUID `json:"id"`
	Name string     `json:"name"`
	Role string     `json:"role"`
}

type target struct {
	Type  string     `json:"type"`
	ID    *uuid.UUID `json:"id"`
	Label string     `json:"label"`
}

// rawJSON is stored JSON that is sent out as it is, and null when there is none.
type rawJSON []byte

func (r rawJSON) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}
	return r, nil
}

type filter struct {
	user       *uuid.UUID
	action     string
	targetType string
	targetID   *uuid.UUID
	from, to   *time.Time
	cursor     int64
	limit      int
}

func parseFilter(r *http.Request) (f filter, ok bool) {
	q := r.URL.Query()
	f.limit = defaultLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return f, false
		}
		f.limit = min(n, maxLimit)
	}
	if v := q.Get("cursor"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			return f, false
		}
		f.cursor = n
	}
	for _, p := range []struct {
		key string
		dst **uuid.UUID
	}{{"user", &f.user}, {"targetId", &f.targetID}} {
		if v := q.Get(p.key); v != "" {
			id, err := uuid.Parse(v)
			if err != nil {
				return f, false
			}
			*p.dst = &id
		}
	}
	for _, p := range []struct {
		key string
		dst **time.Time
	}{{"from", &f.from}, {"to", &f.to}} {
		if v := q.Get(p.key); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return f, false
			}
			*p.dst = &t
		}
	}
	f.action, f.targetType = q.Get("action"), q.Get("targetType")
	return f, true
}

func bad(w http.ResponseWriter) {
	httpx.Error(w, http.StatusBadRequest, "bad_request", "A filter in the request is not valid.")
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	f, ok := parseFilter(r)
	if !ok {
		bad(w)
		return
	}
	h.serve(w, r, f, true)
}

func (h *handler) history(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, ok := parseFilter(r)
		id, err := uuid.Parse(r.PathValue("id"))
		if !ok || err != nil {
			bad(w)
			return
		}
		f.targetType, f.targetID = kind, &id
		h.serve(w, r, f, false)
	}
}

// serve answers one page, newest first. A manager (or anyone who is not an owner) sees only their own property's entries.
// withWhere adds the client address and place, which only people who can open the whole trail get.
func (h *handler) serve(w http.ResponseWriter, r *http.Request, f filter, withWhere bool) {
	ctx, a := r.Context(), access.ActorFrom(r.Context())
	var property *uuid.UUID
	if a.Role != access.Owner {
		property = &a.PropertyID
	}
	var items []item
	var seqs []int64
	err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`select a.seq, a.id, a.occurred_at, a.actor_user_id, coalesce(u.name, ''), a.actor_role, a.view_as, a.action, a.target_type, a.target_id,
			        a.target_label, a.before::text, a.after::text, a.reason, a.device, a.ip, a.place, a.trace_id
			 from audit_log a left join app_user u on u.id = a.actor_user_id
			 where a.organization_id = $1
			   and ($2::uuid is null or a.property_id = $2)
			   and ($3::uuid is null or a.actor_user_id = $3)
			   and ($4 = '' or a.action = $4)
			   and ($5 = '' or a.target_type = $5)
			   and ($6::uuid is null or a.target_id = $6)
			   and ($7::timestamptz is null or a.occurred_at >= $7)
			   and ($8::timestamptz is null or a.occurred_at < $8)
			   and ($9::bigint = 0 or a.seq < $9)
			 order by a.seq desc limit $10`,
			a.OrgID, property, f.user, f.action, f.targetType, f.targetID, f.from, f.to, f.cursor, f.limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var it item
			var seq int64
			var before, after *string
			var ip, place string
			var trace *uuid.UUID
			if err := rows.Scan(&seq, &it.ID, &it.At, &it.Actor.ID, &it.Actor.Name, &it.Actor.Role, &it.ViewAs, &it.Action, &it.Target.Type, &it.Target.ID,
				&it.Target.Label, &before, &after, &it.Reason, &it.Device, &ip, &place, &trace); err != nil {
				return err
			}
			if before != nil {
				it.Before = rawJSON(*before)
			}
			if after != nil {
				it.After = rawJSON(*after)
			}
			if trace != nil {
				s := trace.String()
				it.TraceID = &s
			}
			if withWhere {
				it.IP, it.Place = &ip, &place
			}
			items, seqs = append(items, it), append(seqs, seq)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	var next *string
	if len(items) > f.limit {
		items = items[:f.limit]
		s := strconv.FormatInt(seqs[f.limit-1], 10)
		next = &s
	}
	if items == nil {
		items = []item{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}
