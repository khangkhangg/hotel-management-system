// Package httpx holds the HTTP plumbing shared by every route: the router, JSON errors, CSRF and sessions.
package httpx

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/mail"
	"hotelpms/api/internal/password"
)

// Deps is everything routes need. It grows as tasks add routes.
type Deps struct {
	Pool         *pgxpool.Pool
	Mail         mail.Sender
	Now          func() time.Time // injected so tests control time
	CookieSecure bool
	BaseURL      string                 // the address people use, for links in emails
	Passwords    password.BreachChecker // checks new passwords against breach lists; nil skips the check
}

// Clock returns the current time from Now, or the real time when Now is not set.
func (d Deps) Clock() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Mount registers a package's routes. Packages that import httpx provide one, so httpx never imports them.
type Mount func(mux *http.ServeMux, d Deps)

func NewRouter(d Deps, mounts ...Mount) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", Methods(map[string]http.HandlerFunc{
		http.MethodGet: func(w http.ResponseWriter, r *http.Request) {
			JSON(w, http.StatusOK, map[string]string{"status": "ok"})
		},
	}))
	mux.HandleFunc("/api/csrf", Methods(map[string]http.HandlerFunc{http.MethodGet: d.issueCSRF}))
	for _, mount := range mounts {
		mount(mux, d)
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
	})
	return d.csrf(mux)
}

func JSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// Error writes {"error":{"code":"...","message":"..."}}, the shape every route uses.
func Error(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// Decode reads a small JSON body into dst. It answers 400 itself and returns false when the body is bad.
func Decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		Error(w, http.StatusBadRequest, "bad_request", "The request could not be read.")
		return false
	}
	return true
}

// Methods dispatches by HTTP method and answers any other method with 405 and an Allow header.
// A route registered for all methods plus this helper keeps wrong-method calls from falling into the 404 handler.
func Methods(handlers map[string]http.HandlerFunc) http.HandlerFunc {
	allow := ""
	for m := range handlers {
		if allow != "" {
			allow += ", "
		}
		allow += m
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if h, ok := handlers[r.Method]; ok {
			h(w, r)
			return
		}
		w.Header().Set("Allow", allow)
		Error(w, http.StatusMethodNotAllowed, "method_not_allowed", "This address does not accept "+r.Method+" requests.")
	}
}
