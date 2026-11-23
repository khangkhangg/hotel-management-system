package app_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/app"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/notify"
	"hotelpms/api/internal/payments"
	"hotelpms/api/internal/testkit"
)

// route is one endpoint and the permission that opens it. A person with any of the permissions may use it.
type route struct {
	method, path string
	any          []access.Permission
}

var nobody = uuid.Nil.String()

// matrix lists a route from each area. The full set of roles is tried on each one, so a role that gains or loses access
// by accident shows up here as a failing line with the role and the route in it.
var matrix = []route{
	// reservations and guests
	{"GET", "/api/reservations", []access.Permission{access.ReservationsView}},
	{"POST", "/api/reservations", []access.Permission{access.ReservationsEdit}},
	{"GET", "/api/guests", []access.Permission{access.ReservationsView}},
	{"GET", "/api/cancel-risk", []access.Permission{access.ReservationsView}},
	{"GET", "/api/messages", []access.Permission{access.ReservationsView}},
	{"GET", "/api/reservations/" + nobody + "/messages", []access.Permission{access.ReservationsView}},
	{"POST", "/api/reservations/" + nobody + "/messages", []access.Permission{access.ReservationsEdit}},
	{"GET", "/api/waitlist", []access.Permission{access.ReservationsView}},
	// money
	{"GET", "/api/payments", []access.Permission{access.PaymentsTake}},
	{"POST", "/api/payments/" + nobody + "/refunds", []access.Permission{access.RefundsRequest}},
	{"POST", "/api/refunds/" + nobody + "/approve", []access.Permission{access.RefundsApprove}},
	{"GET", "/api/refund-limits", []access.Permission{access.RefundsRequest}},
	{"PUT", "/api/refund-limits", []access.Permission{access.PaymentsConfigure}},
	{"GET", "/api/payment-types", []access.Permission{access.PaymentsConfigure}},
	{"GET", "/api/payouts?from=2026-11-01&to=2026-11-30", []access.Permission{access.ReportsView}},
	{"GET", "/api/reports/reservations.csv?from=2026-11-01&to=2026-11-30", []access.Permission{access.ReportsView}},
	// rates, yield and pricing
	{"GET", "/api/yield-rules", []access.Permission{access.RatesEdit}},
	{"POST", "/api/yield-rules", []access.Permission{access.RatesEdit}},
	{"POST", "/api/channel/yield-preview", []access.Permission{access.ChannelsManage}},
	{"GET", "/api/promo-codes", []access.Permission{access.RatesEdit}},
	{"GET", "/api/price-suggestions", []access.Permission{access.RatesEdit}},
	{"POST", "/api/price-suggestions/apply", []access.Permission{access.RatesEdit}},
	{"GET", "/api/price-limits", []access.Permission{access.RatesEdit}},
	{"GET", "/api/competitor-rates?from=2026-11-01&to=2026-11-30", []access.Permission{access.RatesEdit}},
	{"GET", "/api/overbooking-hints", []access.Permission{access.RatesEdit}},
	// channels
	{"GET", "/api/channel", []access.Permission{access.ChannelsManage}},
	{"POST", "/api/channel/sync", []access.Permission{access.ChannelsManage}},
	{"GET", "/api/channel/parity", []access.Permission{access.ChannelsManage}},
	{"GET", "/api/channel/providers", []access.Permission{access.ChannelsManage}},
	{"GET", "/api/channel/log", []access.Permission{access.ChannelsManage}},
	// the guest site and what guests say
	{"GET", "/api/site", []access.Permission{access.BrandingConfigure}},
	{"PUT", "/api/site/brand", []access.Permission{access.BrandingConfigure}},
	{"PUT", "/api/site/amenities", []access.Permission{access.BrandingConfigure}},
	{"POST", "/api/site/photos", []access.Permission{access.BrandingConfigure}},
	{"GET", "/api/reviews", []access.Permission{access.RefundsApprove}},
	{"POST", "/api/reviews/" + nobody + "/hide", []access.Permission{access.RefundsApprove}},
	{"GET", "/api/risk", []access.Permission{access.RefundsApprove}},
	{"POST", "/api/risk/" + nobody + "/approve", []access.Permission{access.RefundsApprove}},
	// housekeeping
	{"GET", "/api/lost-items", []access.Permission{access.HousekeepingView}},
	{"POST", "/api/lost-items", []access.Permission{access.HousekeepingUpdate}},
	{"PUT", "/api/lost-items/" + nobody, []access.Permission{access.HousekeepingUpdate}},
	// extras: the catalog is for the people who sell them as well as those who manage them
	{"POST", "/api/extras", []access.Permission{access.ExtrasManage}},
	{"POST", "/api/reservations/" + nobody + "/extras", []access.Permission{access.ReservationsEdit, access.PaymentsTake}},
}

func TestEveryRoleIsAllowedOrRefusedOnEachRouteExactlyAsThePermissionTableSays(t *testing.T) {
	w := testkit.NewWith(t, func(w *testkit.World) []httpx.Mount {
		hub, err := notify.NewHub(w.Pool)
		testkit.Must(t, err)
		t.Cleanup(hub.Close)
		return app.Mounts(hub, nil, payments.NewFake(), w.OrgA, nil)
	})
	people := map[access.Role]*testkit.Person{}
	for _, role := range access.AllRoles {
		if role == access.Owner {
			people[role] = w.Member(w.OrgA, nil, string(role))
			continue
		}
		people[role] = w.Member(w.OrgA, &w.A1, string(role))
	}
	for _, r := range matrix {
		for _, role := range access.AllRoles {
			want := false
			for _, p := range r.any {
				want = want || access.Can(role, p)
			}
			got := people[role].Do(r.method, r.path, &w.A1, `{}`).Status
			switch {
			case !want && got != 403:
				t.Errorf("%s %s as %s: status %d, want 403", r.method, r.path, role, got)
			case want && (got == 403 || got == 401):
				t.Errorf("%s %s as %s: status %d, but the role has %v", r.method, r.path, role, got, r.any)
			}
		}
	}
	// Someone who is not signed in is turned away from all of them.
	for _, r := range matrix {
		if got := w.Anon(r.method, r.path, `{}`).Status; got != 401 && got != 403 {
			t.Errorf("%s %s without a session: status %d", r.method, r.path, got)
		}
	}
	t.Log(fmt.Sprintf("%d routes, %d roles", len(matrix), len(access.AllRoles)))
}

func TestAPersonFromAnotherOrganizationGetsNothingFromAnyRoute(t *testing.T) {
	w := testkit.NewWith(t, func(w *testkit.World) []httpx.Mount {
		hub, err := notify.NewHub(w.Pool)
		testkit.Must(t, err)
		t.Cleanup(hub.Close)
		return app.Mounts(hub, nil, payments.NewFake(), w.OrgA, nil)
	})
	stranger := w.Member(w.OrgB, nil, "owner")
	for _, r := range matrix {
		got := stranger.Do(r.method, r.path, &w.A1, `{}`).Status
		if got == 200 || got == 201 || got == 204 {
			t.Errorf("%s %s: an owner of another organization got status %d on this organization's property", r.method, r.path, got)
		}
	}
}
