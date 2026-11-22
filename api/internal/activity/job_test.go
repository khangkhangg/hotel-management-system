package activity_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/activity"
	"hotelpms/api/internal/db"
)

func (w *world) alerts(user uuid.UUID, org uuid.UUID) (n int, level string) {
	w.t.Helper()
	must(w.t, db.WithOrg(ctx, w.pool, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select count(*), coalesce(max(level), '') from notification where user_id = $1 and key = $2`, user, "trail-broken:"+org.String()).Scan(&n, &level)
	}))
	return
}

func (w *world) tamper(org uuid.UUID) {
	w.t.Helper()
	cfg := w.pool.Config().ConnConfig.Copy()
	cfg.User, cfg.Password = "migrator", "test-only-password"
	c, err := pgx.ConnectConfig(ctx, cfg)
	must(w.t, err)
	defer c.Close(ctx)
	_, err = c.Exec(ctx, `update audit_log set reason = 'edited later' where organization_id = $1 and seq = 2`, org)
	must(w.t, err)
}

func TestBrokenChainAlertsOwners(t *testing.T) {
	w := newWorld(t)
	owner := w.member(w.orgA, nil, "owner")
	mgr := w.member(w.orgA, &w.a1, "manager")
	for i := 0; i < 3; i++ {
		w.log(w.orgA, w.a1, owner.id, "owner", "user.invited", "user", uuid.New())
	}
	w.tamper(w.orgA)
	must(t, activity.VerifyAll(ctx, w.pool, time.Now()))
	if n, level := w.alerts(owner.id, w.orgA); n != 1 || level != "urgent" {
		t.Fatalf("owner: %d alerts, level %q", n, level)
	}
	if n, _ := w.alerts(mgr.id, w.orgA); n != 0 {
		t.Fatalf("manager got %d alerts", n)
	}
	must(t, activity.VerifyAll(ctx, w.pool, time.Now()))
	if n, _ := w.alerts(owner.id, w.orgA); n != 1 {
		t.Fatalf("a second night made %d notifications, want the same one raised", n)
	}
}

func TestCleanChainIsSilent(t *testing.T) {
	w := newWorld(t)
	owner := w.member(w.orgA, nil, "owner")
	ownerB := w.member(w.orgB, nil, "owner")
	for i := 0; i < 3; i++ {
		w.log(w.orgA, w.a1, owner.id, "owner", "user.invited", "user", uuid.New())
	}
	w.log(w.orgB, w.b1, ownerB.id, "owner", "user.invited", "user", uuid.New())
	must(t, activity.VerifyAll(ctx, w.pool, time.Now()))
	if n, _ := w.alerts(owner.id, w.orgA); n != 0 {
		t.Fatal("an intact chain raised an alert")
	}
}

func TestOnlyTheBrokenOrganizationIsReported(t *testing.T) {
	w := newWorld(t)
	ownerA, ownerB := w.member(w.orgA, nil, "owner"), w.member(w.orgB, nil, "owner")
	for i := 0; i < 3; i++ {
		w.log(w.orgA, w.a1, ownerA.id, "owner", "user.invited", "user", uuid.New())
		w.log(w.orgB, w.b1, ownerB.id, "owner", "user.invited", "user", uuid.New())
	}
	w.tamper(w.orgB)
	must(t, activity.VerifyAll(ctx, w.pool, time.Now()))
	if n, _ := w.alerts(ownerB.id, w.orgB); n != 1 {
		t.Fatalf("organization B: %d", n)
	}
	if n, _ := w.alerts(ownerA.id, w.orgA); n != 0 {
		t.Fatalf("organization A: %d", n)
	}
}

func TestNextRunIsThreeInTheMorningUTC(t *testing.T) {
	for _, c := range []struct{ now, want string }{
		{"2026-11-23T02:59:00Z", "2026-11-23T03:00:00Z"},
		{"2026-11-23T03:00:00Z", "2026-11-24T03:00:00Z"},
		{"2026-11-23T17:30:00+07:00", "2026-11-24T03:00:00Z"},
	} {
		now, _ := time.Parse(time.RFC3339, c.now)
		if got := activity.NextRun(now).Format(time.RFC3339); got != c.want {
			t.Fatalf("%s: %s, want %s", c.now, got, c.want)
		}
	}
}
