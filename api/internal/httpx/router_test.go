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
	rec := httptest.NewRecorder()
	httpx.NewRouter(httpx.Deps{}).ServeHTTP(rec, httptest.NewRequest("POST", "/api/health", nil))
	if rec.Code != 405 {
		t.Fatalf("got %d, want 405", rec.Code)
	}
}
