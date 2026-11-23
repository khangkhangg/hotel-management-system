package staff_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/db"
)

type propertyRow struct{ ID, Name, Currency string }

func TestPropertiesListsWhereThePersonWorks(t *testing.T) {
	w := newWorld(t)
	owner := w.member("linh@hotel.com", w.orgA, nil, "owner")
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	w.member("other@hotel.com", w.orgB, nil, "owner")

	var got []propertyRow
	jsonOf(t, w.do(owner, "GET", "/api/properties", nil, "").want(t, 200), &got)
	if len(got) != 2 || got[0].Currency != "USD" || got[0].Name != "a1" || got[1].Name != "a2" || got[0].ID != w.a1.String() {
		t.Fatalf("owner sees %+v", got)
	}
	got = nil
	jsonOf(t, w.do(mgr, "GET", "/api/properties", nil, "").want(t, 200), &got)
	if len(got) != 1 || got[0].ID != w.a1.String() {
		t.Fatalf("manager sees %+v", got)
	}
}

func TestPropertiesNeedsASession(t *testing.T) {
	w := newWorld(t)
	w.do(nil, "GET", "/api/properties", nil, "").want(t, 401)
}

func TestADeactivatedMembershipDropsItsProperty(t *testing.T) {
	w := newWorld(t)
	owner := w.member("linh@hotel.com", w.orgA, nil, "owner")
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	w.do(owner, "POST", "/api/staff/"+mgr.id.String()+"/deactivate", ptr(w.a1), "").want(t, 204)
	// Their session ended with the deactivation, so they get no list at all.
	w.do(mgr, "GET", "/api/properties", nil, "").want(t, 401)
}

func TestEachPropertyReportsItsOwnToday(t *testing.T) {
	w := newWorld(t)
	w.advance(11 * time.Hour) // 20:00 UTC on the 23rd is already the 24th in Ho Chi Minh City
	owner := w.member("linh@hotel.com", w.orgA, nil, "owner")
	must(t, db.WithOrg(ctx, w.pool, w.orgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update property set time_zone = 'Asia/Ho_Chi_Minh' where id = $1`, w.a2)
		return err
	}))
	var got []struct{ ID, Name, Today string }
	jsonOf(t, w.do(owner, "GET", "/api/properties", nil, "").want(t, 200), &got)
	if len(got) != 2 || got[0].Today != "2026-11-23" || got[1].Today != "2026-11-24" {
		t.Fatalf("%+v", got)
	}
}
