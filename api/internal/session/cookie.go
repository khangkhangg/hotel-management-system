package session

import "net/http"

const (
	cookieName       = "sid"
	deviceCookieName = "td"
)

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

// ClearCookie removes the session cookie from the browser.
func ClearCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}

// SetDeviceCookie remembers a trusted device for the same 30 days the server honours it.
func SetDeviceCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: deviceCookieName, Value: token, Path: "/", MaxAge: int(deviceTrust.Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}

func ReadDeviceCookie(r *http.Request) string {
	c, err := r.Cookie(deviceCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func ClearDeviceCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: deviceCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}
