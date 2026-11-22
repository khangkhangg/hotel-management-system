package signin

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/account"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/otp"
	"hotelpms/api/internal/session"
)

const (
	stateCookie   = "gstate"
	stateLifetime = 10 * time.Minute
)

type googleHandler struct {
	*handler
	v IDTokenVerifier
}

// MountGoogle adds "Continue with Google". Only people who were invited can sign in this way:
// the Google account's verified email must match an existing user. The email code is still required.
func MountGoogle(v IDTokenVerifier) httpx.Mount {
	return func(mux *http.ServeMux, d httpx.Deps) {
		g := &googleHandler{handler: &handler{Deps: d}, v: v}
		get := func(f http.HandlerFunc) http.HandlerFunc {
			return httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: f})
		}
		mux.HandleFunc("/api/sign-in/google/start", get(g.start))
		mux.HandleFunc("/api/sign-in/google/callback", get(g.callback))
	}
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashOf(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

func (g *googleHandler) start(w http.ResponseWriter, r *http.Request) {
	state, err1 := randomToken()
	verifier, err2 := randomToken()
	if err := errors.Join(err1, err2); err != nil {
		httpx.InternalError(w, err)
		return
	}
	ctx, now := r.Context(), g.Clock()
	err := db.WithoutOrg(ctx, g.Pool, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `insert into oauth_state (state_hash, verifier, expires_at) values ($1, $2, $3)`, hashOf(state), verifier, now.Add(stateLifetime))
		return e
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: state, Path: "/", MaxAge: int(stateLifetime.Seconds()), HttpOnly: true, Secure: g.CookieSecure, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, g.v.AuthURL(state, base64.RawURLEncoding.EncodeToString(hashOf(verifier))), http.StatusFound)
}

func (g *googleHandler) callback(w http.ResponseWriter, r *http.Request) {
	ctx, now := r.Context(), g.Clock()
	fail := func(reason string) { http.Redirect(w, r, "/sign-in?error="+reason, http.StatusFound) }

	q := r.URL.Query()
	code, state := q.Get("code"), q.Get("state")
	c, cookieErr := r.Cookie(stateCookie)
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: g.CookieSecure, SameSite: http.SameSiteLaxMode})
	if q.Get("error") != "" || code == "" || state == "" || cookieErr != nil || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		fail("google_failed")
		return
	}

	var verifier string
	err := db.WithoutOrg(ctx, g.Pool, func(tx pgx.Tx) error {
		// Deleting makes the state single-use even if two requests race.
		return tx.QueryRow(ctx, `delete from oauth_state where state_hash = $1 and expires_at > $2 returning verifier`, hashOf(state), now).Scan(&verifier)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		fail("google_failed")
		return
	}
	if err != nil {
		httpx.InternalError(w, err)
		return
	}

	identity, err := g.v.Exchange(ctx, code, verifier)
	if err != nil {
		fail("google_failed")
		return
	}
	if !identity.EmailVerified {
		fail("google_not_invited")
		return
	}

	var user account.User
	var allowed bool
	err = db.WithoutOrg(ctx, g.Pool, func(tx pgx.Tx) error {
		u, e := account.ByEmail(ctx, tx, identity.Email)
		if errors.Is(e, account.ErrNotFound) {
			return nil
		}
		if e != nil || u.Status != "active" {
			return e
		}
		// Bind the Google account on first use. A different Google account with the same email matches no row.
		tag, e := tx.Exec(ctx, `update app_user set google_subject = $2 where id = $1 and (google_subject is null or google_subject = $2)`, u.ID, identity.Subject)
		user, allowed = u, e == nil && tag.RowsAffected() == 1
		return e
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	if !allowed {
		fail("google_not_invited")
		return
	}

	sess, trusted, err := g.begin(r, user)
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	session.SetCookie(w, sess.ID, g.CookieSecure)
	if trusted {
		orgs, err := g.settle(r, user, sess)
		if err != nil {
			httpx.InternalError(w, err)
			return
		}
		if len(orgs) > 1 {
			http.Redirect(w, r, "/sign-in/organization", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	switch err := g.issueCode(r, user); {
	case err == nil:
		http.Redirect(w, r, "/sign-in/code", http.StatusFound)
	case errors.Is(err, otp.ErrTooMany):
		fail("too_many_codes")
	default:
		fail("email_failed")
	}
}
