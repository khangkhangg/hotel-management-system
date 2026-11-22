// Package httpx holds the HTTP plumbing shared by every route: the router, JSON errors, CSRF and trace IDs.
package httpx

import (
	"encoding/json"
	"net/http"
)

// Deps is everything the routes need. It grows as tasks add routes.
type Deps struct{}

func NewRouter(deps Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", Methods(map[string]http.HandlerFunc{
		http.MethodGet: func(w http.ResponseWriter, r *http.Request) {
			JSON(w, http.StatusOK, map[string]string{"status": "ok"})
		},
	}))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
	})
	return mux
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
