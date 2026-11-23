package httpx

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// RealIP makes r.RemoteAddr the real client address when the request came through a trusted proxy.
// It reads X-Forwarded-For from the right, skipping the proxies' own addresses, because the left part is
// whatever the client claimed. A request from any other peer keeps its own address.
func RealIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	contains := func(a netip.Addr) bool {
		for _, p := range trusted {
			if p.Contains(a) {
				return true
			}
		}
		return false
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(trusted) > 0 {
				host, _, err := net.SplitHostPort(r.RemoteAddr)
				if peer, perr := netip.ParseAddr(host); err == nil && perr == nil && contains(peer) {
					parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
					for i := len(parts) - 1; i >= 0; i-- {
						a, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
						if err != nil {
							break // a value we cannot read ends the trusted part of the chain
						}
						if !contains(a) {
							r.RemoteAddr = net.JoinHostPort(a.String(), "0") // keeps the host:port shape other code expects
							break
						}
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
