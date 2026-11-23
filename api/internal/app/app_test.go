package app_test

import (
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hotelpms/api/internal/app"
	"hotelpms/api/internal/db/testdb"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/mail"
	"hotelpms/api/internal/notify"
)

func TestEveryAreaIsMountedAndNeedsASession(t *testing.T) {
	pool := testdb.New(t)
	hub, err := notify.NewHub(pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Close)
	h := app.NewHandler(httpx.Deps{Pool: pool, Mail: mail.NewFake(), Now: time.Now}, hub, nil, nil, uuid.Nil, nil)

	get := func(method, path string) int {
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := get("GET", "/api/health"); got != 200 {
		t.Fatalf("health: %d", got)
	}
	for _, path := range []string{"/api/me", "/api/staff", "/api/activity", "/api/notifications", "/api/notification-preferences"} {
		if got := get("GET", path); got != 401 {
			t.Fatalf("%s without a session: %d, want 401", path, got)
		}
	}
	// Public routes are reachable; they answer 403 only because this request has no CSRF token.
	for _, path := range []string{"/api/sign-in", "/api/password-reset"} {
		if got := get("POST", path); got != 403 {
			t.Fatalf("POST %s: %d, want 403 (CSRF)", path, got)
		}
	}
	if got := get("GET", "/api/sign-in/google/start"); got != 404 {
		t.Fatalf("Google sign-in is mounted without Google settings: %d", got)
	}
}

func TestGoogleSignInIsMountedWhenConfigured(t *testing.T) {
	pool := testdb.New(t)
	hub, _ := notify.NewHub(pool)
	t.Cleanup(hub.Close)
	h := app.NewHandler(httpx.Deps{Pool: pool, Mail: mail.NewFake(), Now: time.Now}, hub, app.GoogleFor("id", "secret", "https://app.example.com"), nil, uuid.Nil, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/sign-in/google/start", nil))
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "accounts.google.com") {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Location"))
	}
}
