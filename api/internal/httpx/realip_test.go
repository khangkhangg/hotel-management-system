package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"hotelpms/api/internal/httpx"
)

func seen(t *testing.T, trusted string, remote, forwarded string) string {
	t.Helper()
	var got string
	var prefixes []netip.Prefix
	if trusted != "" {
		prefixes = []netip.Prefix{netip.MustParsePrefix(trusted)}
	}
	h := httpx.RealIP(prefixes)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.RemoteAddr }))
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = remote
	if forwarded != "" {
		req.Header.Set("X-Forwarded-For", forwarded)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

func TestTheClientAddressComesFromAProxyOnlyWhenTrusted(t *testing.T) {
	if got := seen(t, "10.0.0.0/8", "10.0.0.1:4000", "203.0.113.9"); got != "203.0.113.9:0" {
		t.Fatalf("trusted proxy: %q", got)
	}
	if got := seen(t, "10.0.0.0/8", "198.51.100.7:4000", "203.0.113.9"); got != "198.51.100.7:4000" {
		t.Fatalf("an untrusted peer chose its own address: %q", got)
	}
	if got := seen(t, "", "10.0.0.1:4000", "203.0.113.9"); got != "10.0.0.1:4000" {
		t.Fatalf("no trusted proxies configured: %q", got)
	}
}

func TestTheRightMostUntrustedAddressWins(t *testing.T) {
	// The left part is whatever the client claimed; only what the proxies appended can be believed.
	if got := seen(t, "10.0.0.0/8", "10.0.0.1:4000", "1.2.3.4, 203.0.113.9, 10.0.0.2"); got != "203.0.113.9:0" {
		t.Fatalf("%q", got)
	}
	if got := seen(t, "10.0.0.0/8", "10.0.0.1:4000", "not an address"); got != "10.0.0.1:4000" {
		t.Fatalf("a bad header changed the address: %q", got)
	}
	if got := seen(t, "10.0.0.0/8", "10.0.0.1:4000", ""); got != "10.0.0.1:4000" {
		t.Fatalf("no header: %q", got)
	}
}
