package httpx

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/db"
	"hotelpms/api/internal/session"
)

type sessionKey struct{}

// SessionFrom returns the session that RequireSession or RequirePending put in the request context.
func SessionFrom(ctx context.Context) session.Session {
	s, _ := ctx.Value(sessionKey{}).(session.Session)
	return s
}

// Wrap lets a middleware-wrapped handler sit in a Methods map.
func Wrap(h http.Handler) http.HandlerFunc { return h.ServeHTTP }

func Unauthenticated(w http.ResponseWriter) {
	Error(w, http.StatusUnauthorized, "unauthenticated", "Sign in to continue.")
}

func InternalError(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	Error(w, http.StatusInternalServerError, "internal", "Something failed on our side. Try again.")
}

// require runs the next handler only for a live session at the given stage.
func (d Deps) require(stage session.Stage, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := session.ReadCookie(r)
		if id == "" {
			Unauthenticated(w)
			return
		}
		var s session.Session
		err := db.WithoutOrg(r.Context(), d.Pool, func(tx pgx.Tx) (e error) {
			s, e = session.Lookup(r.Context(), tx, id, d.Clock())
			return
		})
		switch {
		case errors.Is(err, session.ErrNotFound), errors.Is(err, session.ErrExpired):
			Unauthenticated(w)
			return
		case err != nil:
			InternalError(w, err)
			return
		case s.Stage != stage:
			Unauthenticated(w)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, s)))
	})
}

// RequireSession lets through only people who finished both sign-in steps.
func (d Deps) RequireSession(next http.Handler) http.Handler {
	return d.require(session.StageFull, next)
}

// RequirePending lets through only people who passed the first step and still owe the email code.
func (d Deps) RequirePending(next http.Handler) http.Handler {
	return d.require(session.StagePending, next)
}
