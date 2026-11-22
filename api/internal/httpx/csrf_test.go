package httpx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hotelpms/api/internal/httpx"
)

// router with one write route, so the tests can see whether the CSRF check lets a request through.
func csrfRouter() http.Handler {
	return httpx.NewRouter(httpx.Deps{}, func(mux *http.ServeMux, d httpx.Deps) {
		mux.HandleFunc("/api/echo", httpx.Methods(map[string]http.HandlerFunc{
			http.MethodGet:  func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) },
			http.MethodPost: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) },
		}))
	})
}

func fetchToken(t *testing.T, h http.Handler) (token string, cookie *http.Cookie) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/csrf", nil))
	var body struct{ Token string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Token == "" || rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "csrf" {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value != body.Token {
		t.Fatalf("the csrf cookie does not hold the token: %+v", cookie)
	}
	return body.Token, cookie
}

func post(h http.Handler, token string, cookie *http.Cookie) int {
	r := httptest.NewRequest("POST", "/api/echo", strings.NewReader("{}"))
	if token != "" {
		r.Header.Set("X-CSRF-Token", token)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Code
}

func TestCSRFTokenWithMatchingCookieIsAccepted(t *testing.T) {
	h := csrfRouter()
	token, cookie := fetchToken(t, h)
	if code := post(h, token, cookie); code != 204 {
		t.Fatalf("got %d", code)
	}
}

func TestCSRFPostWithoutTokenIsRefused(t *testing.T) {
	h := csrfRouter()
	_, cookie := fetchToken(t, h)
	if code := post(h, "", cookie); code != 403 {
		t.Fatalf("no header: %d", code)
	}
	if code := post(h, "anything", nil); code != 403 {
		t.Fatalf("no cookie: %d", code)
	}
}

func TestCSRFMismatchedTokenIsRefused(t *testing.T) {
	h := csrfRouter()
	_, cookie := fetchToken(t, h)
	if code := post(h, "some-other-token", cookie); code != 403 {
		t.Fatalf("got %d", code)
	}
}

func TestCSRFRefusalIsAJSONError(t *testing.T) {
	h := csrfRouter()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/echo", strings.NewReader("{}")))
	var body struct{ Error struct{ Code string } }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code != "csrf" {
		t.Fatalf("%s", rec.Body.String())
	}
}

func TestCSRFSafeMethodsNeedNoToken(t *testing.T) {
	h := csrfRouter()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/echo", nil))
	if rec.Code != 204 {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestCSRFTokenIsReusedWhileTheCookieExists(t *testing.T) {
	h := csrfRouter()
	token, cookie := fetchToken(t, h)
	r := httptest.NewRequest("GET", "/api/csrf", nil)
	r.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if !strings.Contains(rec.Body.String(), token) {
		t.Fatal("a second call issued a different token, which would break a second open tab")
	}
}

func TestCSRFCookieAttributes(t *testing.T) {
	h := httpx.NewRouter(httpx.Deps{CookieSecure: true})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/csrf", nil))
	c := rec.Result().Cookies()[0]
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Fatalf("%+v", c)
	}
}
