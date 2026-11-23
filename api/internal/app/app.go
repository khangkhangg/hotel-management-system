// Package app assembles the API: every area's routes behind one router. Both the server and the end-to-end server use it.
package app

import (
	"github.com/google/uuid"
	"net/http"
	"os"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/activity"
	"hotelpms/api/internal/alerts"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/cancelrisk"
	"hotelpms/api/internal/chain"
	"hotelpms/api/internal/channels"
	"hotelpms/api/internal/extras"
	"hotelpms/api/internal/folio"
	"hotelpms/api/internal/group"
	"hotelpms/api/internal/guestbooking"
	"hotelpms/api/internal/guestcare"
	"hotelpms/api/internal/housekeeping"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/lostfound"
	"hotelpms/api/internal/notes"
	"hotelpms/api/internal/notify"
	"hotelpms/api/internal/payments"
	"hotelpms/api/internal/payouts"
	"hotelpms/api/internal/pricing"
	"hotelpms/api/internal/promo"
	"hotelpms/api/internal/reports"
	"hotelpms/api/internal/risk"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/signin"
	"hotelpms/api/internal/siteprofile"
	"hotelpms/api/internal/staff"
	"hotelpms/api/internal/translate"
	"hotelpms/api/internal/yieldrules"
)

// Mounts lists every area of the API. google is nil when Google sign-in is not configured.
func Mounts(hub *notify.Hub, google httpx.Mount, card payments.Provider, cardOrg uuid.UUID, tr translate.Translator) []httpx.Mount {
	mounts := []httpx.Mount{signin.Mount, access.Mount, staff.Mount, activity.Mount, notify.Mount(hub), rooms.Mount, chain.Mount, extras.Mount, siteprofile.MountWith(tr), yieldrules.Mount, risk.Mount, cancelrisk.Mount, promo.Mount, pricing.Mount, guestcare.Mount, lostfound.Mount, payouts.Mount, group.Mount, reports.Mount, booking.Mount, folio.MountWith(card), housekeeping.Mount, alerts.Mount, notes.Mount(hub), guestbooking.Mount(card, cardOrg), channels.Mount(channels.Real(os.Getenv))}
	if google != nil {
		mounts = append(mounts, google)
	}
	return mounts
}

// NewHandler mounts every area of the API. google is nil when Google sign-in is not configured.
func NewHandler(d httpx.Deps, hub *notify.Hub, google httpx.Mount, card payments.Provider, cardOrg uuid.UUID, tr translate.Translator) http.Handler {
	return httpx.NewRouter(d, Mounts(hub, google, card, cardOrg, tr)...)
}

// GoogleFor returns the Google sign-in routes for the given credentials. baseURL is the address people use.
func GoogleFor(id, secret, baseURL string) httpx.Mount {
	return signin.MountGoogle(signin.NewGoogle(signin.GoogleConfig{ClientID: id, ClientSecret: secret, RedirectURL: baseURL + "/api/sign-in/google/callback"}))
}
