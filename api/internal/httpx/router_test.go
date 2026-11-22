package httpx_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"hotelpms/api/internal/httpx"
)

func TestHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.NewRouter(httpx.Deps{}).ServeHTTP(rec, httptest.NewRequest("GET", "/api/health", nil))
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}

func TestUnknownRouteIsJSON404(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.NewRouter(httpx.Deps{}).ServeHTTP(rec, httptest.NewRequest("GET", "/api/nope", nil))
	if rec.Code != 404 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestHealthRefusesPost(t *testing.T) {
	h := httpx.NewRouter(httpx.Deps{})
	token, cookie := fetchToken(t, h) // the CSRF check runs first, so send a valid token to reach the method check
	r := httptest.NewRequest("POST", "/api/health", nil)
	r.Header.Set("X-CSRF-Token", token)
	r.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != 405 || rec.Header().Get("Allow") != "GET" {
		t.Fatalf("got %d allow=%q, want 405 and Allow: GET", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestHealthPostWithoutATokenIsRefusedByCSRFFirst(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.NewRouter(httpx.Deps{}).ServeHTTP(rec, httptest.NewRequest("POST", "/api/health", nil))
	if rec.Code != 403 {
		t.Fatalf("got %d, want 403", rec.Code)
	}
}

func TestEveryResponseCarriesATraceID(t *testing.T) {
	for _, path := range []string{"/api/health", "/api/nope"} {
		rec := httptest.NewRecorder()
		httpx.NewRouter(httpx.Deps{}).ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Header().Get("X-Trace-Id") == "" {
			t.Fatalf("%s has no X-Trace-Id", path)
		}
	}
}
