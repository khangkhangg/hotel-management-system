package httpx

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

const (
	csrfCookie = "csrf"
	csrfHeader = "X-CSRF-Token"
)

// issueCSRF returns the CSRF token and sets it as a cookie. A browser that already has the cookie
// gets the same token back, so a second open tab keeps working.
func (d Deps) issueCSRF(w http.ResponseWriter, r *http.Request) {
	token := ""
	if c, err := r.Cookie(csrfCookie); err == nil && len(c.Value) >= 32 {
		token = c.Value
	} else {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			Error(w, http.StatusInternalServerError, "internal", "Something failed on our side. Try again.")
			return
		}
		token = base64.RawURLEncoding.EncodeToString(raw)
	}
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Value: token, Path: "/", HttpOnly: true, Secure: d.CookieSecure, SameSite: http.SameSiteLaxMode})
	JSON(w, http.StatusOK, map[string]string{"token": token})
}

// csrf refuses any request that changes data unless its X-CSRF-Token header matches the csrf cookie.
// A page on another site can make the browser send the cookie but cannot read it, so it cannot send the header.
func (d Deps) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A payment provider cannot hold our cookie; its webhooks prove themselves with a signature instead.
		if strings.HasPrefix(r.URL.Path, "/api/webhooks/") {
			next.ServeHTTP(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(csrfCookie)
		header := r.Header.Get(csrfHeader)
		if err != nil || header == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(header)) != 1 {
			Error(w, http.StatusForbidden, "csrf", "Your session could not be verified. Reload the page and try again.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
