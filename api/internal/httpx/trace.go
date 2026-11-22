package httpx

import (
	"context"
	"net"
	"net/http"

	"github.com/google/uuid"
)

type requestInfoKey struct{}

// RequestInfo is what an activity entry records about where a request came from.
type RequestInfo struct {
	TraceID   uuid.UUID
	IP        string
	UserAgent string
}

// Trace gives every request an ID, returns it in X-Trace-Id and makes it, the client address and the
// browser available to code that records activity.
// shortcut: the client address is RemoteAddr, so behind a reverse proxy it is the proxy's, until a trusted-proxy setting exists.
func Trace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ua := uuid.New(), r.UserAgent()
		if len(ua) > 300 {
			ua = ua[:300]
		}
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		w.Header().Set("X-Trace-Id", id.String())
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestInfoKey{}, RequestInfo{TraceID: id, IP: ip, UserAgent: ua})))
	})
}

// Info returns the request's trace data, or the zero value outside a request.
func Info(ctx context.Context) RequestInfo {
	i, _ := ctx.Value(requestInfoKey{}).(RequestInfo)
	return i
}

func TraceID(ctx context.Context) uuid.UUID { return Info(ctx).TraceID }
