// Package app assembles the API: every area's routes behind one router. Both the server and the end-to-end server use it.
package app

import (
	"github.com/google/uuid"
	"net/http"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/activity"
	"hotelpms/api/internal/alerts"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/chain"
	"hotelpms/api/internal/extras"
	"hotelpms/api/internal/folio"
	"hotelpms/api/internal/group"
	"hotelpms/api/internal/guestbooking"
	"hotelpms/api/internal/housekeeping"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/notes"
	"hotelpms/api/internal/notify"
	"hotelpms/api/internal/payments"
	"hotelpms/api/internal/reports"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/signin"
	"hotelpms/api/internal/staff"
)

// NewHandler mounts every area of the API. google is nil when Google sign-in is not configured.
func NewHandler(d httpx.Deps, hub *notify.Hub, google httpx.Mount, card payments.Provider, cardOrg uuid.UUID) http.Handler {
	mounts := []httpx.Mount{signin.Mount, access.Mount, staff.Mount, activity.Mount, notify.Mount(hub), rooms.Mount, chain.Mount, extras.Mount, group.Mount, reports.Mount, booking.Mount, folio.Mount, housekeeping.Mount, alerts.Mount, notes.Mount(hub), guestbooking.Mount(card, cardOrg)}
	if google != nil {
		mounts = append(mounts, google)
	}
	return httpx.NewRouter(d, mounts...)
}

// GoogleFor returns the Google sign-in routes for the given credentials. baseURL is the address people use.
func GoogleFor(id, secret, baseURL string) httpx.Mount {
	return signin.MountGoogle(signin.NewGoogle(signin.GoogleConfig{ClientID: id, ClientSecret: secret, RedirectURL: baseURL + "/api/sign-in/google/callback"}))
}
