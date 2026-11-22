package access

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/session"
)

// Actor is who is acting, in which organization and property, with which role.
// ViewAs is the role an owner is looking at the app as. It changes what is shown, never what is allowed:
// Role is always the person's real role.
type Actor struct {
	UserID     uuid.UUID
	OrgID      uuid.UUID
	PropertyID uuid.UUID // uuid.Nil when the request names no property
	Role       Role
	ViewAs     string
}

type actorKey struct{}

func ActorFrom(ctx context.Context) Actor {
	a, _ := ctx.Value(actorKey{}).(Actor)
	return a
}

// load works out the actor for a request that already has a full session. It answers the error itself
// and returns false when the request cannot go on.
func load(d httpx.Deps, w http.ResponseWriter, r *http.Request) (Actor, bool) {
	ctx, s := r.Context(), httpx.SessionFrom(r.Context())
	if s.OrgID == nil {
		httpx.Error(w, http.StatusUnauthorized, "organization_required", "Choose an organization to continue.")
		return Actor{}, false
	}
	a := Actor{UserID: s.UserID, OrgID: *s.OrgID, ViewAs: s.ViewAs}
	var property *uuid.UUID
	if h := r.Header.Get("X-Property-Id"); h != "" {
		id, err := uuid.Parse(h)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "bad_request", "The property in this request is not valid.")
			return Actor{}, false
		}
		property = &id
		a.PropertyID = id
	}

	var role string
	err := db.WithOrg(ctx, d.Pool, a.OrgID, func(tx pgx.Tx) error {
		if property != nil {
			var n int
			// Row-level security hides other organizations' properties, so this is also the ownership check.
			if err := tx.QueryRow(ctx, `select count(*) from property where id = $1`, *property).Scan(&n); err != nil || n == 0 {
				return err
			}
		}
		// An organization-wide membership (an owner) covers every property; otherwise the property's own one counts.
		return tx.QueryRow(ctx,
			`select role from membership
			 where user_id = $1 and organization_id = $2 and (property_id is null or property_id = $3::uuid)
			 order by property_id nulls first limit 1`, a.UserID, a.OrgID, property).Scan(&role)
	})
	if err != nil && err != pgx.ErrNoRows {
		httpx.InternalError(w, err)
		return Actor{}, false
	}
	if role == "" || !validRole(role) {
		forbidden(w)
		return Actor{}, false
	}
	a.Role = Role(role)
	return a, true
}

func forbidden(w http.ResponseWriter) {
	httpx.Error(w, http.StatusForbidden, "forbidden", "You do not have permission to do that.")
}

// Require lets a request through only when the signed-in person's role at the property named in the
// X-Property-Id header has the permission. With no header, only an organization-wide role (an owner) can qualify.
func Require(d httpx.Deps, p Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return d.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			a, ok := load(d, w, r)
			if !ok {
				return
			}
			if !Can(a.Role, p) {
				forbidden(w)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, a)))
		}))
	}
}

// Mount registers POST /api/view-as, which lets an owner see the app as one of the other four roles.
func Mount(mux *http.ServeMux, d httpx.Deps) {
	mux.HandleFunc("/api/view-as", httpx.Methods(map[string]http.HandlerFunc{
		http.MethodPost: httpx.Wrap(d.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			a, ok := load(d, w, r)
			if !ok {
				return
			}
			if a.Role != Owner {
				forbidden(w)
				return
			}
			var in struct{ Role string }
			if !httpx.Decode(w, r, &in) {
				return
			}
			if in.Role != "" && (in.Role == string(Owner) || !validRole(in.Role)) {
				httpx.Error(w, http.StatusBadRequest, "bad_request", "Choose manager, front desk, revenue or housekeeping, or leave the role empty to go back to the owner view.")
				return
			}
			s := httpx.SessionFrom(r.Context())
			err := db.WithoutOrg(r.Context(), d.Pool, func(tx pgx.Tx) error { return session.SetViewAs(r.Context(), tx, s.ID, in.Role) })
			if err != nil {
				httpx.InternalError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}))),
	}))
}
