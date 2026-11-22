package session

import "net/http"

const cookieName = "sid"

// SetCookie sends the session id. It is a browser-session cookie: the server enforces the real limits.
func SetCookie(w http.ResponseWriter, id string, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: id, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}

// ReadCookie returns the session id, or "" when there is none.
func ReadCookie(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
