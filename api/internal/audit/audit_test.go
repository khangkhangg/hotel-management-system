package audit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/account"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/db/testdb"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/tenant"
)

var ctx = context.Background()

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type fixture struct {
	org, property, user uuid.UUID
}

func seedOrg(t *testing.T, pool *pgxpool.Pool, name string) fixture {
	t.Helper()
	var f fixture
	must(t, db.WithoutOrg(ctx, pool, func(tx pgx.Tx) (e error) {
		f.org, e = tenant.CreateOrganization(ctx, tx, name)
		return
	}))
	must(t, db.WithOrg(ctx, pool, f.org, func(tx pgx.Tx) (e error) {
		f.property, e = tenant.CreateProperty(ctx, tx, f.org, tenant.PropertyInput{Name: "p", Slug: name + "-p", TimeZone: "UTC", Currency: "USD", DefaultLocale: "en"})
		return
	}))
	must(t, db.WithoutOrg(ctx, pool, func(tx pgx.Tx) (e error) {
		f.user, e = account.Create(ctx, tx, name+"@hotel.com", "Person")
		return
	}))
	return f
}

func (f fixture) who(role string) audit.Who {
	return audit.Who{UserID: f.user, OrgID: f.org, PropertyID: f.property, Role: role}
}

func record(t *testing.T, pool *pgxpool.Pool, f fixture, e audit.Entry) {
	t.Helper()
	c := audit.WithWho(ctx, f.who("manager"))
	must(t, db.WithOrg(ctx, pool, f.org, func(tx pgx.Tx) error { return audit.Record(c, tx, e) }))
}

func seedEntry(t *testing.T, pool *pgxpool.Pool) fixture {
	t.Helper()
	f := seedOrg(t, pool, "a")
	record(t, pool, f, audit.Entry{Action: "user.invited", TargetType: "user", TargetID: f.user})
	return f
}

func verify(pool *pgxpool.Pool, f fixture) (at *uuid.UUID, err error) {
	err = db.WithOrg(ctx, pool, f.org, func(tx pgx.Tx) (e error) {
		at, e = audit.VerifyChain(ctx, tx, f.org)
		return
	})
	return
}

// asMigrator runs a statement as the table owner, which is how someone with database access would tamper.
func asMigrator(t *testing.T, pool *pgxpool.Pool, q string, args ...any) {
	t.Helper()
	cfg := pool.Config().ConnConfig.Copy()
	cfg.User, cfg.Password = "migrator", "test-only-password"
	c, err := pgx.ConnectConfig(ctx, cfg)
	must(t, err)
	defer c.Close(ctx)
	_, err = c.Exec(ctx, q, args...)
	must(t, err)
}

func TestAppRoleCannotChangeTrail(t *testing.T) {
	pool := testdb.New(t)
	f := seedEntry(t, pool)
	for _, q := range []string{`update audit_log set action = 'x'`, `delete from audit_log`} {
		err := db.WithOrg(ctx, pool, f.org, func(tx pgx.Tx) error { _, e := tx.Exec(ctx, q); return e })
		if err == nil {
			t.Fatalf("%q was allowed", q)
		}
	}
}

func TestChainDetectsTampering(t *testing.T) {
	pool := testdb.New(t)
	f := seedOrg(t, pool, "a")
	for i := 0; i < 3; i++ {
		record(t, pool, f, audit.Entry{Action: "user.role_changed", TargetType: "user", TargetID: f.user, After: map[string]any{"role": "manager"}})
	}
	if at, _ := verify(pool, f); at != nil {
		t.Fatal("a clean chain was reported broken")
	}
	asMigrator(t, pool, `update audit_log set after = '{"role":"owner"}' where seq = 2`)
	at, err := verify(pool, f)
	must(t, err)
	if at == nil {
		t.Fatal("tampering was not found")
	}
	var second uuid.UUID
	must(t, db.WithOrg(ctx, pool, f.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select id from audit_log where seq = 2`).Scan(&second)
	}))
	if *at != second {
		t.Fatalf("broken at %v, want the second entry %v", *at, second)
	}
}

func TestDeletingARowIsDetected(t *testing.T) {
	pool := testdb.New(t)
	f := seedOrg(t, pool, "a")
	for i := 0; i < 3; i++ {
		record(t, pool, f, audit.Entry{Action: "user.invited", TargetType: "user", TargetID: f.user})
	}
	asMigrator(t, pool, `delete from audit_log where seq = 2`)
	if at, _ := verify(pool, f); at == nil {
		t.Fatal("a removed entry was not found")
	}
}

func TestEntryCarriesActorViewAsAndTrace(t *testing.T) {
	pool := testdb.New(t)
	f := seedOrg(t, pool, "a")
	who := f.who("owner")
	who.ViewAs = "front_desk"
	var trace uuid.UUID
	h := httpx.Trace(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trace = httpx.TraceID(r.Context())
		c := audit.WithWho(r.Context(), who)
		err := db.WithOrg(c, pool, f.org, func(tx pgx.Tx) error {
			return audit.Record(c, tx, audit.Entry{Action: "user.role_changed", TargetType: "user", TargetID: f.user, TargetLabel: "Person",
				Before: map[string]any{"role": "front_desk"}, After: map[string]any{"role": "manager"}, Reason: "promotion"})
		})
		must(t, err)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) Safari/605.1.15")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if trace == uuid.Nil || rec.Header().Get("X-Trace-Id") != trace.String() {
		t.Fatalf("trace %v, header %q", trace, rec.Header().Get("X-Trace-Id"))
	}
	var role, viewAs, ip, device, before, after, reason, label string
	var gotTrace, actor uuid.UUID
	must(t, db.WithOrg(ctx, pool, f.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select actor_user_id, actor_role, view_as, ip, device, trace_id, before::text, after::text, reason, target_label from audit_log`).
			Scan(&actor, &role, &viewAs, &ip, &device, &gotTrace, &before, &after, &reason, &label)
	}))
	if actor != f.user || role != "owner" || viewAs != "front_desk" || gotTrace != trace || ip != "203.0.113.9" || device == "" ||
		!strings.Contains(before, "front_desk") || !strings.Contains(after, "manager") || reason != "promotion" || label != "Person" {
		t.Fatalf("row: %v %s %s %v ip=%s device=%q %s %s %s %s", actor, role, viewAs, gotTrace, ip, device, before, after, reason, label)
	}
}

func TestSecretsNeverReachTheTrail(t *testing.T) {
	pool := testdb.New(t)
	f := seedOrg(t, pool, "a")
	record(t, pool, f, audit.Entry{Action: "password.changed", TargetType: "user", TargetID: f.user,
		Before: map[string]any{"password": "hunter2-hunter2", "name": "keep"},
		After:  map[string]any{"code": "123456", "nested": map[string]any{"token": "tok-abc", "secret": "s3", "ok": 1}, "list": []any{map[string]any{"password": "p9"}}}})
	var all string
	must(t, db.WithOrg(ctx, pool, f.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select before::text || after::text from audit_log`).Scan(&all)
	}))
	for _, secret := range []string{"hunter2", "123456", "tok-abc", "s3", "p9"} {
		if strings.Contains(all, secret) {
			t.Fatalf("%q reached the trail: %s", secret, all)
		}
	}
	if !strings.Contains(all, "keep") {
		t.Fatalf("ordinary values were dropped: %s", all)
	}
	if at, _ := verify(pool, f); at != nil {
		t.Fatal("chain broken after redaction")
	}
}

func TestTwoWritesAtOnceKeepOneChain(t *testing.T) {
	pool := testdb.New(t)
	f := seedOrg(t, pool, "a")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			record(t, pool, f, audit.Entry{Action: "user.invited", TargetType: "user", TargetID: f.user})
		}()
	}
	wg.Wait()
	var n, min, max int
	must(t, db.WithOrg(ctx, pool, f.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select count(*), min(seq), max(seq) from audit_log`).Scan(&n, &min, &max)
	}))
	if n != 20 || min != 1 || max != 20 {
		t.Fatalf("count %d, seq %d..%d", n, min, max)
	}
	if at, err := verify(pool, f); at != nil || err != nil {
		t.Fatalf("chain: %v %v", at, err)
	}
}

func TestChainsAreIndependentPerOrganization(t *testing.T) {
	pool := testdb.New(t)
	a, b := seedOrg(t, pool, "a"), seedOrg(t, pool, "b")
	record(t, pool, a, audit.Entry{Action: "user.invited", TargetType: "user", TargetID: a.user})
	record(t, pool, b, audit.Entry{Action: "user.invited", TargetType: "user", TargetID: b.user})
	var seqB int
	must(t, db.WithOrg(ctx, pool, b.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select seq from audit_log`).Scan(&seqB)
	}))
	if seqB != 1 {
		t.Fatalf("second organization starts at seq %d, want 1", seqB)
	}
}

func TestSystemEntryHasNoPersonButNamesTheJob(t *testing.T) {
	pool := testdb.New(t)
	f := seedOrg(t, pool, "a")
	must(t, db.WithOrg(ctx, pool, f.org, func(tx pgx.Tx) error {
		return audit.RecordSystem(ctx, tx, f.org, "nightly-chain-check", audit.Entry{Action: "chain.verified", TargetType: "organization", TargetID: f.org})
	}))
	var actor *uuid.UUID
	var role string
	must(t, db.WithOrg(ctx, pool, f.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select actor_user_id, actor_role from audit_log`).Scan(&actor, &role)
	}))
	if actor != nil || role != "system:nightly-chain-check" {
		t.Fatalf("actor %v role %q", actor, role)
	}
}

func TestEntryWithoutActorOrActionIsRefused(t *testing.T) {
	pool := testdb.New(t)
	f := seedOrg(t, pool, "a")
	err := db.WithOrg(ctx, pool, f.org, func(tx pgx.Tx) error {
		return audit.Record(ctx, tx, audit.Entry{Action: "user.invited", TargetType: "user"}) // no Who in the context
	})
	if err == nil {
		t.Fatal("an entry with no actor was accepted")
	}
	err = db.WithOrg(audit.WithWho(ctx, f.who("owner")), pool, f.org, func(tx pgx.Tx) error {
		return audit.Record(audit.WithWho(ctx, f.who("owner")), tx, audit.Entry{TargetType: "user"})
	})
	if err == nil {
		t.Fatal("an entry with no action was accepted")
	}
}

func TestRecordForUserWritesIntoEachOrganizationTheyBelongTo(t *testing.T) {
	pool := testdb.New(t)
	a, b := seedOrg(t, pool, "a"), seedOrg(t, pool, "b")
	for _, org := range []fixture{a, b} {
		prop := org.property
		must(t, db.WithOrg(ctx, pool, org.org, func(tx pgx.Tx) error {
			return tenant.AddMembership(ctx, tx, org.org, a.user, &prop, "front_desk")
		}))
	}
	must(t, db.WithoutOrg(ctx, pool, func(tx pgx.Tx) error {
		return audit.RecordForUser(ctx, tx, a.user, audit.Entry{Action: "signin.failed", TargetType: "user", TargetID: a.user})
	}))
	for _, org := range []fixture{a, b} {
		var n int
		var role string
		must(t, db.WithOrg(ctx, pool, org.org, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `select count(*), coalesce(max(actor_role), '') from audit_log where actor_user_id = $1`, a.user).Scan(&n, &role)
		}))
		if n != 1 || role != "front_desk" {
			t.Fatalf("org %v: %d entries, role %q", org.org, n, role)
		}
		if at, _ := verify(pool, org); at != nil {
			t.Fatal("chain broken")
		}
	}
}
