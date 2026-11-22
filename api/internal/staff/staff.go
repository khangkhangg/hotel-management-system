// Package staff covers inviting people, managing them, and resetting passwords (spec 01 and 02).
package staff

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/password"
)

type handler struct{ httpx.Deps }

func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	users := func(f http.HandlerFunc) http.HandlerFunc {
		return httpx.Wrap(access.Require(d, access.UsersManage)(f))
	}
	m := func(method string, f http.HandlerFunc) http.HandlerFunc {
		return httpx.Methods(map[string]http.HandlerFunc{method: f})
	}
	mux.HandleFunc("/api/staff/invites", m(http.MethodPost, users(h.createInvite)))
	mux.HandleFunc("/api/staff", m(http.MethodGet, users(h.list)))
	mux.HandleFunc("/api/staff/{id}", m(http.MethodPatch, users(h.patch)))
	mux.HandleFunc("/api/staff/{id}/deactivate", m(http.MethodPost, users(h.setStatus("deactivated"))))
	mux.HandleFunc("/api/staff/{id}/reactivate", m(http.MethodPost, users(h.setStatus("active"))))
	mux.HandleFunc("/api/staff/{id}/reset-sign-in", m(http.MethodPost, users(h.resetSignIn)))
	mux.HandleFunc("/api/properties", m(http.MethodGet, httpx.Wrap(d.RequireSession(http.HandlerFunc(h.properties)))))
	mux.HandleFunc("/api/invites/{token}", m(http.MethodGet, h.previewInvite))
	mux.HandleFunc("/api/invites/{token}/accept", m(http.MethodPost, h.acceptInvite))
	mux.HandleFunc("/api/password-reset", m(http.MethodPost, h.requestReset))
	mux.HandleFunc("/api/password-reset/{token}", m(http.MethodPost, h.useReset))
}

// newToken returns a random link token and the hash that is stored in its place.
func newToken() (token string, hash []byte, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashOf(token), nil
}

func hashOf(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func validRole(s string) bool { return slices.Contains(access.AllRoles, access.Role(s)) }

func roleWords(role string) string { return strings.ReplaceAll(role, "_", " ") }

func notFound(w http.ResponseWriter) {
	httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
}

func forbidden(w http.ResponseWriter) {
	httpx.Error(w, http.StatusForbidden, "forbidden", "You do not have permission to do that.")
}

func badRequest(w http.ResponseWriter, message string) {
	httpx.Error(w, http.StatusBadRequest, "bad_request", message)
}

// passwordProblem applies the password policy. It returns nil when the password is acceptable, otherwise a
// function that writes the matching error. Returning a function lets a caller refuse the input after rolling back.
func (h *handler) passwordProblem(ctx context.Context, plain string) func(http.ResponseWriter) {
	var checker password.BreachChecker = noBreachList{}
	if h.Passwords != nil {
		checker = h.Passwords
	}
	switch err := password.Check(ctx, checker, plain); err {
	case nil:
		return nil
	case password.ErrTooShort:
		return func(w http.ResponseWriter) {
			httpx.Error(w, http.StatusBadRequest, "password_too_short", "Use at least 12 characters.")
		}
	case password.ErrBreached:
		return func(w http.ResponseWriter) {
			httpx.Error(w, http.StatusBadRequest, "password_breached", "That password has appeared in a data breach. Choose another.")
		}
	default:
		return func(w http.ResponseWriter) { httpx.InternalError(w, err) }
	}
}

type noBreachList struct{}

func (noBreachList) Breached(context.Context, string) (bool, error) { return false, nil }

// setOrg makes the transaction work inside an organization. It is for code that learns the organization
// from a trusted row (an invite) rather than from a session.
func setOrg(ctx context.Context, tx pgx.Tx, org uuid.UUID) error {
	_, err := tx.Exec(ctx, `select set_config('app.org_id', $1, true)`, org.String())
	return err
}
