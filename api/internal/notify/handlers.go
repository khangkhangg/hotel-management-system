package notify

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
)

type handler struct {
	httpx.Deps
	hub *Hub
}

// Mount returns the routes for notifications; hub feeds the live stream.
func Mount(hub *Hub) httpx.Mount {
	return func(mux *http.ServeMux, d httpx.Deps) {
		h := &handler{d, hub}
		route := func(method string, f http.HandlerFunc) http.HandlerFunc {
			return httpx.Methods(map[string]http.HandlerFunc{method: httpx.Wrap(d.RequireSession(f))})
		}
		mux.HandleFunc("/api/notifications", route(http.MethodGet, h.listNotifications))
		mux.HandleFunc("/api/notifications/stream", route(http.MethodGet, h.stream))
		mux.HandleFunc("/api/notifications/open-all", route(http.MethodPost, h.openAll))
		mux.HandleFunc("/api/notifications/{id}/open", route(http.MethodPost, h.open))
		mux.HandleFunc("/api/notification-preferences", httpx.Methods(map[string]http.HandlerFunc{
			http.MethodGet: httpx.Wrap(d.RequireSession(http.HandlerFunc(h.getPreferences))),
			http.MethodPut: httpx.Wrap(d.RequireSession(http.HandlerFunc(h.putPreferences))),
		}))
	}
}

type item struct {
	ID         uuid.UUID  `json:"id"`
	Level      Level      `json:"level"`
	Key        string     `json:"key"`
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	ActionURL  string     `json:"actionURL"`
	Count      int        `json:"count"`
	OpenedAt   *time.Time `json:"openedAt"`
	ResolvedAt *time.Time `json:"resolvedAt"`
	ResolvedBy *uuid.UUID `json:"resolvedBy"`
}

type listing struct {
	Unread int    `json:"unread"`
	Items  []item `json:"items"`
}

const listLimit = 50

func (h *handler) list(ctx context.Context, org, user uuid.UUID) (listing, error) {
	out := listing{Items: []item{}}
	err := db.WithOrg(ctx, h.Pool, org, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `select count(*) from notification where user_id = $1 and opened_at is null and resolved_at is null`, user).Scan(&out.Unread); err != nil {
			return err
		}
		rows, err := tx.Query(ctx,
			`select id, level, key, title, body, action_url, count, opened_at, resolved_at, resolved_by from notification
			 where user_id = $1 order by updated_at desc, id desc limit $2`, user, listLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.ID, &it.Level, &it.Key, &it.Title, &it.Body, &it.ActionURL, &it.Count, &it.OpenedAt, &it.ResolvedAt, &it.ResolvedBy); err != nil {
				return err
			}
			out.Items = append(out.Items, it)
		}
		return rows.Err()
	})
	return out, err
}

func noOrganization(w http.ResponseWriter) {
	httpx.Error(w, http.StatusUnauthorized, "organization_required", "Choose an organization to continue.")
}

func (h *handler) listNotifications(w http.ResponseWriter, r *http.Request) {
	s := httpx.SessionFrom(r.Context())
	if s.OrgID == nil {
		noOrganization(w)
		return
	}
	l, err := h.list(r.Context(), *s.OrgID, s.UserID)
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, l)
}

func (h *handler) open(w http.ResponseWriter, r *http.Request) {
	s := httpx.SessionFrom(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if s.OrgID == nil || err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
		return
	}
	var found bool
	err = db.WithOrg(r.Context(), h.Pool, *s.OrgID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `update notification set opened_at = coalesce(opened_at, $3) where id = $1 and user_id = $2`, id, s.UserID, h.Clock())
		found = tag.RowsAffected() == 1
		return err
	})
	switch {
	case err != nil:
		httpx.InternalError(w, err)
	case !found:
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *handler) openAll(w http.ResponseWriter, r *http.Request) {
	s := httpx.SessionFrom(r.Context())
	if s.OrgID == nil {
		noOrganization(w)
		return
	}
	err := db.WithOrg(r.Context(), h.Pool, *s.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `update notification set opened_at = $2 where user_id = $1 and opened_at is null`, s.UserID, h.Clock())
		return err
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type preferences struct {
	QuietFrom *string `json:"quietFrom"`
	QuietTo   *string `json:"quietTo"`
	Email     struct {
		Urgent       bool `json:"urgent"`
		ActionNeeded bool `json:"actionNeeded"`
	} `json:"email"`
}

func (h *handler) getPreferences(w http.ResponseWriter, r *http.Request) {
	s := httpx.SessionFrom(r.Context())
	if s.OrgID == nil {
		noOrganization(w)
		return
	}
	var out preferences
	err := db.WithOrg(r.Context(), h.Pool, *s.OrgID, func(tx pgx.Tx) error {
		p, err := preferencesOf(r.Context(), tx, s.UserID)
		out.QuietFrom, out.QuietTo, out.Email.Urgent, out.Email.ActionNeeded = p.QuietFrom, p.QuietTo, p.EmailUrgent, p.EmailActionNeeded
		return err
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// validClock reports whether s is a time of day written "HH:MM".
func validClock(s string) bool {
	_, err := time.Parse("15:04", s)
	return err == nil && len(s) == 5
}

func (h *handler) putPreferences(w http.ResponseWriter, r *http.Request) {
	var in preferences
	if !httpx.Decode(w, r, &in) {
		return
	}
	s := httpx.SessionFrom(r.Context())
	if s.OrgID == nil {
		noOrganization(w)
		return
	}
	bothSet, noneSet := in.QuietFrom != nil && in.QuietTo != nil, in.QuietFrom == nil && in.QuietTo == nil
	if !(noneSet || (bothSet && validClock(*in.QuietFrom) && validClock(*in.QuietTo))) {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Give quiet hours as two times like 22:00 and 07:00, or leave both empty.")
		return
	}
	err := db.WithOrg(r.Context(), h.Pool, *s.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(),
			`insert into notification_preference (user_id, quiet_from, quiet_to, email_urgent, email_action_needed) values ($1, $2::time, $3::time, $4, $5)
			 on conflict (user_id) do update set quiet_from = excluded.quiet_from, quiet_to = excluded.quiet_to,
			   email_urgent = excluded.email_urgent, email_action_needed = excluded.email_action_needed`,
			s.UserID, in.QuietFrom, in.QuietTo, in.Email.Urgent, in.Email.ActionNeeded)
		return err
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
