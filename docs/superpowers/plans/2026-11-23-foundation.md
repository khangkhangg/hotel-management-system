# Foundation (milestone 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Staff can be invited, sign in with two steps, see only what their role allows in a responsive app shell, and every action is recorded in a tamper-evident activity trail.

**Architecture:** One Go API with PostgreSQL as the only store. Every request runs in a transaction that sets the organization for row-level security, carries a trace ID, and writes its audit row in the same transaction as the change. A SvelteKit web app talks to the API over JSON and receives live notifications over server-sent events.

**Tech Stack:** Go 1.23+, PostgreSQL 16+, `pgx/v5`, `sqlc`, `goose` migrations, River job queue, `coreos/go-oidc`, `golang.org/x/oauth2`, `golang.org/x/crypto/argon2`. SvelteKit 2, Svelte 5, TypeScript, plain CSS driven by design tokens (no CSS framework), Bits UI, Lucide, Paraglide JS, Vitest, Playwright, axe.

**Spec:** [Design doc](../specs/2026-11-23-hotel-pms-design.md), and specs [01 Roles and access](../../specs/01-roles-and-access.md), [02 Sign-in and security](../../specs/02-sign-in-and-security.md), [03 Data model](../../specs/03-data-model.md), [08 Branding and interface](../../specs/08-branding-and-interface.md), [10 Notifications and activity trail](../../specs/10-notifications-and-activity-trail.md). The mockup canvas is https://claude.ai/artifact/XLfV5xcXb79z6Wy6HymYiU.

## Not in this plan

These belong to later milestone plans: rooms, rates, reservations, the calendar, payments, channels, guest site, alerts and checks (spec 09), web push, the daily summary email, team notes, and hotel branding settings. The shell shows a placeholder home screen per role.

## Global Constraints

- Primary keys are UUID version 7. Times are stored in UTC.
- Every table has `organization_id` except `app_user`, `session`, `otp_challenge` and `trusted_device`.
- Row-level security limits each request to its own organization.
- Passwords: minimum 12 characters, Argon2id, checked against known breached passwords.
- Email code: 6 digits, valid 10 minutes, usable once, stored as a hash, cancelled after 5 wrong attempts, at most 5 codes per user per hour.
- Trusted device: 30 days.
- Session: ends after 12 hours without activity and after 7 days in any case. Cookie is `HttpOnly`, `Secure`, `SameSite=Lax`.
- Invite link: valid 7 days, usable once. Password reset link: valid 30 minutes, usable once.
- Rate limits: 10 sign-in attempts per account per 15 minutes then a 15-minute lock, 50 per IP address per 15 minutes, 3 password reset emails per account per hour.
- Sign-in answers the same way for a known and an unknown email.
- The audit table rejects `UPDATE` and `DELETE` from the application's database role.
- Roles are exactly: `owner`, `manager`, `front_desk`, `revenue`, `housekeeping`.
- Styling: no CSS framework. Every colour, font, size, space, radius, border, shadow, duration, easing, breakpoint and layer is a named design token in `web/src/lib/tokens.css`. Components use `var(--token)` only and contain no hex colour, no `px` value for spacing, radius or type, no duration and no `z-index` number. A test enforces this.
- Design token values: font IBM Plex Sans; sizes 12, 14, 16, 20, 28 px; weights 400, 500, 600; spacing 4, 8, 12, 16, 24, 32 px; radius 4, 8, 12 px and full; control height 44 px; page `#F4F5F3`; surface `#FFFFFF`; borders `#E2E5E1` and `#8B9590`; text `#17201D` and `#56615C`; accent `#1F5F4F`.
- Breakpoints: phone under 640 px, tablet 640 to 1023 px, desktop 1024 px and up. Sidebar 240 px, rail 68 px.
- Motion: 200 ms screen change, 150 ms colour change, easing `cubic-bezier(0.2, 0, 0, 1)`, reduced motion respected.
- Interface text is sentence case, in Paraglide message files, never glued from pieces.
- `npx @enmera/ai-design-check` reports 0 errors and 0 warnings.
- Layout uses CSS logical properties only (`margin-inline-start`, not `margin-left`).

## Review Focus

1. **The code email fails to send.** The user sees "We could not send the code. Try again." and the failed attempt does not count toward the 5 per hour. Test in Task 5.
2. **The same email in different forms.** `Mai@Hotel.com `, `mai@hotel.com` and `MAI@HOTEL.COM` are one account at invite, sign-in and Google sign-in. Test in Task 2.
3. **The same code or invite submitted twice at once.** Exactly one request succeeds, and one session or one membership is created. Tests in Tasks 5 and 9.
4. **A user with no active membership, or with memberships in two organizations, signs in.** The first sees "Your account has no access. Ask your manager." The second picks an organization before any data loads. Tests in Tasks 6 and 14.
5. **A session expires while a form is open.** The save returns 401, the web app keeps the typed values, shows the sign-in screen, and returns to the form after sign-in. Test in Task 14.

---

## File structure

```
api/
  go.mod                              module hotelpms/api
  cmd/server/main.go                  wires config, db, router, jobs
  migrations/                         goose SQL files, one per task that needs one
  internal/config/config.go           reads environment variables
  internal/db/db.go                   pool, WithOrg transaction helper
  internal/db/testdb/testdb.go        fresh migrated database per test
  internal/ids/ids.go                 UUID v7
  internal/tenant/                    organizations, properties
  internal/account/                   users, email normalising
  internal/password/                  hashing, policy, breach check
  internal/otp/                       email codes
  internal/session/                   sessions, trusted devices, cookies
  internal/signin/                    the two-step flow, rate limits, Google
  internal/access/                    roles, permission matrix, middleware
  internal/staff/                     invites and user management
  internal/audit/                     activity trail, hash chain
  internal/notify/                    notifications, delivery, SSE
  internal/mail/                      Sender interface, SMTP sender, fake
  internal/httpx/                     router, JSON errors, CSRF, trace ID
web/
  src/lib/tokens.css                  design tokens as CSS variables
  src/lib/ui/                         shared components, one file each
  src/lib/api.ts                      fetch wrapper, 401 handling
  src/lib/session.ts                  current user, role, view-as
  src/routes/(auth)/                  sign-in, code, invite, reset
  src/routes/(app)/+layout.svelte     app shell
  src/routes/(app)/staff/             user management
  src/routes/(app)/activity/          activity trail
  messages/en.json                    interface text
  e2e/                                Playwright tests
.github/workflows/ci.yml
```

---

### Task 1: Repository and toolchain

**Files:**
- Create: `.gitignore`, `README.md`, `api/go.mod`, `api/cmd/server/main.go`, `api/internal/config/config.go`, `api/internal/httpx/router.go`, `web/` (SvelteKit scaffold), `.github/workflows/ci.yml`
- Test: `api/internal/httpx/router_test.go`

**Interfaces:**
- Produces: `config.Load() (Config, error)` with fields `DatabaseURL, BaseURL, SMTPURL, GoogleClientID, GoogleClientSecret, CookieSecure`. `httpx.NewRouter(deps Deps) http.Handler`. `GET /api/health` returns `{"status":"ok"}`.

- [ ] **Step 1: Install Go 1.23+ and PostgreSQL 16+ on the machine.** Neither is installed on the owner's Windows PC as of 2026-11-23. Run `go version` and `psql --version` to confirm.
- [ ] **Step 2: Run `git init` in the project root and add the remote** `https://github.com/khangkhangg/hotel-management-system`. Do not push until the owner says so.
- [ ] **Step 3: Write the failing test**

```go
func TestHealth(t *testing.T) {
    rec := httptest.NewRecorder()
    httpx.NewRouter(httpx.Deps{}).ServeHTTP(rec, httptest.NewRequest("GET", "/api/health", nil))
    if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
        t.Fatalf("got %d %s", rec.Code, rec.Body.String())
    }
}
```

- [ ] **Step 4: Run `go test ./...` in `api/`.** Expected: FAIL, `httpx` undefined.
- [ ] **Step 5: Implement `config.Load`, `httpx.NewRouter` and `cmd/server/main.go`** using `net/http` with Go 1.22 pattern routes.
- [ ] **Step 6: Scaffold the web app** with `npx sv create web` (SvelteKit minimal, TypeScript), then add Bits UI, `lucide-svelte`, Paraglide JS, Vitest and Playwright.
- [ ] **Step 7: Write `ci.yml`** with three jobs: `go test ./...` against a PostgreSQL 16 service, `npm run check && npm test` in `web/`, and `npx @enmera/ai-design-check web/src`.
- [ ] **Step 8: Run `go test ./...` and `npm run check`.** Expected: PASS.
- [ ] **Step 9: Commit** with message `chore: project skeleton`.

### Task 2: Database base, tenancy and row-level security

**Files:**
- Create: `api/migrations/0001_tenancy.sql`, `api/internal/db/db.go`, `api/internal/db/testdb/testdb.go`, `api/internal/ids/ids.go`, `api/internal/tenant/tenant.go`, `api/internal/account/account.go`
- Test: `api/internal/db/db_test.go`, `api/internal/account/account_test.go`

**Interfaces:**
- Produces:
  - `ids.New() uuid.UUID` (version 7)
  - `testdb.New(t *testing.T) *pgxpool.Pool` creates a fresh database, runs all migrations, connects as the application role `app`, and drops it at test end
  - `db.WithOrg(ctx context.Context, pool *pgxpool.Pool, orgID uuid.UUID, fn func(pgx.Tx) error) error` runs `fn` in a transaction after `SET LOCAL app.org_id`
  - `db.WithoutOrg(ctx, pool, fn)` for the person-level tables
  - `tenant.CreateOrganization(ctx, tx, name string) (uuid.UUID, error)`, `tenant.CreateProperty(ctx, tx, orgID uuid.UUID, p PropertyInput) (uuid.UUID, error)`
  - `account.NormalizeEmail(s string) string`, `account.Create(ctx, tx, email, name string) (uuid.UUID, error)`, `account.ByEmail(ctx, tx, email string) (User, error)`, `account.ErrNotFound`
- Tables: `organization`, `property`, `app_user`, `membership` as in spec 03. `app_user.email` is unique and stored normalised. `membership` is unique on `(user_id, property_id)` and has `role` checked against the 5 roles.
- Two database roles: `migrator` owns the tables, `app` has only the rights the API needs.

- [ ] **Step 1: Write the failing tests**

```go
func TestRLSBlocksOtherOrganization(t *testing.T) {
    pool := testdb.New(t)
    a, b := seedOrg(t, pool, "A"), seedOrg(t, pool, "B")
    seedProperty(t, pool, b, "B hotel")
    var n int
    db.WithOrg(ctx, pool, a, func(tx pgx.Tx) error {
        return tx.QueryRow(ctx, `select count(*) from property`).Scan(&n)
    })
    if n != 0 { t.Fatalf("org A saw %d properties of org B", n) }
}

func TestNormalizeEmail(t *testing.T) {
    for _, in := range []string{" Mai@Hotel.com ", "MAI@HOTEL.COM", "mai@hotel.com"} {
        if got := account.NormalizeEmail(in); got != "mai@hotel.com" { t.Fatalf("%q -> %q", in, got) }
    }
}

func TestCreateRejectsSameEmailInAnotherForm(t *testing.T) {
    pool := testdb.New(t)
    mustCreate(t, pool, "mai@hotel.com")
    if _, err := create(pool, " MAI@Hotel.com"); err == nil { t.Fatal("duplicate accepted") }
}
```

- [ ] **Step 2: Run `go test ./internal/db/... ./internal/account/...`.** Expected: FAIL, packages undefined.
- [ ] **Step 3: Write `0001_tenancy.sql`.** Enable RLS on `property` and `membership` with policy `organization_id = current_setting('app.org_id')::uuid`. Force RLS for the `app` role.
- [ ] **Step 4: Implement the functions in the Interfaces block.** `NormalizeEmail` trims and lowercases. It does not strip dots or plus tags.
- [ ] **Step 5: Run the tests.** Expected: PASS.
- [ ] **Step 6: Commit** with message `feat: tenancy tables and row-level security`.

### Task 3: Passwords

**Files:**
- Create: `api/internal/password/password.go`, `api/internal/password/breach.go`
- Test: `api/internal/password/password_test.go`

**Interfaces:**
- Produces:
  - `password.Hash(plain string) (string, error)` returns a PHC-format Argon2id string
  - `password.Verify(hash, plain string) bool`
  - `type BreachChecker interface { Breached(ctx context.Context, plain string) (bool, error) }`
  - `password.Check(ctx context.Context, bc BreachChecker, plain string) error` returning `ErrTooShort` or `ErrBreached`
  - `password.NewRangeChecker(client *http.Client) BreachChecker` sends only the first 5 characters of the SHA-1 hash to the Have I Been Pwned range API

- [ ] **Step 1: Write the failing tests**

```go
func TestCheckPolicy(t *testing.T) {
    fake := fakeChecker{breached: map[string]bool{"correct horse battery": true}}
    if err := password.Check(ctx, fake, "short"); !errors.Is(err, password.ErrTooShort) { t.Fatal(err) }
    if err := password.Check(ctx, fake, "elevenchars"); !errors.Is(err, password.ErrTooShort) { t.Fatal(err) }
    if err := password.Check(ctx, fake, "correct horse battery"); !errors.Is(err, password.ErrBreached) { t.Fatal(err) }
    if err := password.Check(ctx, fake, "twelve chars ok"); err != nil { t.Fatal(err) }
}

func TestHashVerify(t *testing.T) {
    h, _ := password.Hash("twelve chars ok")
    if !strings.HasPrefix(h, "$argon2id$") || !password.Verify(h, "twelve chars ok") || password.Verify(h, "wrong") { t.Fatal(h) }
}

func TestBreachCheckerDownDoesNotBlock(t *testing.T) {
    if err := password.Check(ctx, fakeChecker{err: errors.New("timeout")}, "twelve chars ok"); err != nil { t.Fatal(err) }
}
```

- [ ] **Step 2: Run `go test ./internal/password/...`.** Expected: FAIL.
- [ ] **Step 3: Implement.** Argon2id parameters: memory 64 MiB, 3 iterations, 2 threads, 16-byte salt, 32-byte key. Length is counted in Unicode characters, not bytes. A breach-check error is logged and treated as "not breached".
- [ ] **Step 4: Run the tests.** Expected: PASS.
- [ ] **Step 5: Commit** with message `feat: password hashing and policy`.

### Task 4: Sessions and trusted devices

**Files:**
- Create: `api/migrations/0002_sessions.sql`, `api/internal/session/session.go`, `api/internal/session/cookie.go`
- Test: `api/internal/session/session_test.go`

**Interfaces:**
- Consumes: `testdb.New`, `ids.New`, `account.Create`
- Produces:
  - `type Session struct { ID string; UserID uuid.UUID; Stage Stage; OrgID *uuid.UUID; ViewAs string; CreatedAt, LastSeenAt time.Time }` with `Stage` one of `StagePending` (first step passed) or `StageFull`
  - `session.Create(ctx, tx, userID uuid.UUID, stage Stage, device string, now time.Time) (Session, error)`
  - `session.Lookup(ctx, tx, id string, now time.Time) (Session, error)` returns `ErrExpired` after 12 hours idle or 7 days total, and updates `last_seen_at`
  - `session.Promote(ctx, tx, oldID string, now time.Time) (Session, error)` deletes the pending session and returns a new full one with a new ID
  - `session.EndAll(ctx, tx, userID uuid.UUID) error`
  - `session.TrustDevice(ctx, tx, userID uuid.UUID, now time.Time) (token string, err error)`, `session.IsTrusted(ctx, tx, userID uuid.UUID, token string, now time.Time) bool`, `session.ForgetDevices(ctx, tx, userID uuid.UUID) error`
  - `session.SetCookie(w http.ResponseWriter, id string, secure bool)`, `session.ReadCookie(r *http.Request) string`. Cookie name `sid`.
- Session IDs and device tokens are 32 random bytes, base64url. Only a SHA-256 hash is stored.

- [ ] **Step 1: Write the failing tests**

```go
func TestSessionExpiry(t *testing.T) {
    pool := testdb.New(t); u := seedUser(t, pool)
    t0 := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
    s := mustCreate(t, pool, u, session.StageFull, t0)
    if _, err := lookup(pool, s.ID, t0.Add(11*time.Hour+59*time.Minute)); err != nil { t.Fatal("ended early") }
    if _, err := lookup(pool, s.ID, t0.Add(24*time.Hour)); !errors.Is(err, session.ErrExpired) { t.Fatal("idle limit") }
    s2 := mustCreate(t, pool, u, session.StageFull, t0)
    for h := 1; h <= 7*24; h += 6 { lookup(pool, s2.ID, t0.Add(time.Duration(h)*time.Hour)) }
    if _, err := lookup(pool, s2.ID, t0.Add(7*24*time.Hour+time.Minute)); !errors.Is(err, session.ErrExpired) { t.Fatal("7 day limit") }
}

func TestPromoteIssuesNewID(t *testing.T) {
    pool := testdb.New(t); u := seedUser(t, pool)
    p := mustCreate(t, pool, u, session.StagePending, now)
    f := mustPromote(t, pool, p.ID)
    if f.ID == p.ID || f.Stage != session.StageFull { t.Fatal("not rotated") }
    if _, err := lookup(pool, p.ID, now); err == nil { t.Fatal("old id still works") }
}

func TestTrustedDeviceLasts30Days(t *testing.T) {
    pool := testdb.New(t); u := seedUser(t, pool)
    tok := mustTrust(t, pool, u, now)
    if !trusted(pool, u, tok, now.Add(29*24*time.Hour)) || trusted(pool, u, tok, now.Add(31*24*time.Hour)) { t.Fatal("30 days") }
}

func TestCookieAttributes(t *testing.T) {
    rec := httptest.NewRecorder(); session.SetCookie(rec, "abc", true)
    c := rec.Result().Cookies()[0]
    if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" { t.Fatalf("%+v", c) }
}
```

- [ ] **Step 2: Run `go test ./internal/session/...`.** Expected: FAIL.
- [ ] **Step 3: Write the migration** for `session` and `trusted_device`, then implement the Interfaces block.
- [ ] **Step 4: Run the tests.** Expected: PASS.
- [ ] **Step 5: Commit** with message `feat: sessions and trusted devices`.

### Task 5: Email codes

**Files:**
- Create: `api/migrations/0003_otp.sql`, `api/internal/otp/otp.go`, `api/internal/mail/mail.go`, `api/internal/mail/fake.go`, `api/internal/mail/smtp.go`
- Test: `api/internal/otp/otp_test.go`

**Interfaces:**
- Consumes: `testdb.New`, `account.User`
- Produces:
  - `type mail.Sender interface { Send(ctx context.Context, m mail.Message) error }`, `mail.Message{To, Subject, Text string}`, `mail.NewFake() *mail.Fake` with `Sent() []Message` and `FailNext()`
  - `otp.Issue(ctx, pool, sender mail.Sender, u account.User, where otp.Where, now time.Time) error` returns `ErrTooMany` or `ErrSendFailed`. `Where{Browser, Place string}` goes into the email.
  - `otp.Verify(ctx, pool, userID uuid.UUID, code string, now time.Time) error` returns `ErrWrong`, `ErrExpired` or `ErrCancelled`

- [ ] **Step 1: Write the failing tests**

```go
func TestCodeLifecycle(t *testing.T) {
    pool := testdb.New(t); u := seedUser(t, pool); m := mail.NewFake()
    must(t, otp.Issue(ctx, pool, m, u, where, now))
    code := sixDigits(t, m.Sent()[0].Text)
    if err := otp.Verify(ctx, pool, u.ID, code, now.Add(10*time.Minute+time.Second)); !errors.Is(err, otp.ErrExpired) { t.Fatal(err) }
    must(t, otp.Issue(ctx, pool, m, u, where, now))
    code = sixDigits(t, m.Sent()[1].Text)
    must(t, otp.Verify(ctx, pool, u.ID, code, now.Add(time.Minute)))
    if err := otp.Verify(ctx, pool, u.ID, code, now.Add(2*time.Minute)); err == nil { t.Fatal("code reused") }
}

func TestFiveWrongAttemptsCancel(t *testing.T) {
    pool := testdb.New(t); u := seedUser(t, pool); m := mail.NewFake()
    must(t, otp.Issue(ctx, pool, m, u, where, now)); good := sixDigits(t, m.Sent()[0].Text)
    for i := 0; i < 5; i++ { otp.Verify(ctx, pool, u.ID, "000000", now) }
    if err := otp.Verify(ctx, pool, u.ID, good, now); !errors.Is(err, otp.ErrCancelled) { t.Fatal(err) }
}

func TestFivePerHour(t *testing.T) {
    pool := testdb.New(t); u := seedUser(t, pool); m := mail.NewFake()
    for i := 0; i < 5; i++ { must(t, otp.Issue(ctx, pool, m, u, where, now)) }
    if err := otp.Issue(ctx, pool, m, u, where, now); !errors.Is(err, otp.ErrTooMany) { t.Fatal(err) }
    must(t, otp.Issue(ctx, pool, m, u, where, now.Add(61*time.Minute)))
}

func TestSendFailureDoesNotCount(t *testing.T) {
    pool := testdb.New(t); u := seedUser(t, pool); m := mail.NewFake(); m.FailNext()
    if err := otp.Issue(ctx, pool, m, u, where, now); !errors.Is(err, otp.ErrSendFailed) { t.Fatal(err) }
    for i := 0; i < 5; i++ { must(t, otp.Issue(ctx, pool, m, u, where, now)) }
}

func TestSameCodeTwiceAtOnce(t *testing.T) {
    pool := testdb.New(t); u := seedUser(t, pool); m := mail.NewFake()
    must(t, otp.Issue(ctx, pool, m, u, where, now)); code := sixDigits(t, m.Sent()[0].Text)
    ok := runConcurrently(2, func() error { return otp.Verify(ctx, pool, u.ID, code, now) })
    if ok != 1 { t.Fatalf("%d requests succeeded, want 1", ok) }
}

func TestCodeIsStoredHashedAndEmailHasNoLink(t *testing.T) {
    pool := testdb.New(t); u := seedUser(t, pool); m := mail.NewFake()
    must(t, otp.Issue(ctx, pool, m, u, where, now)); code := sixDigits(t, m.Sent()[0].Text)
    if rowContains(t, pool, "otp_challenge", code) { t.Fatal("plain code stored") }
    if strings.Contains(m.Sent()[0].Text, "http") { t.Fatal("email has a link") }
}
```

- [ ] **Step 2: Run `go test ./internal/otp/...`.** Expected: FAIL.
- [ ] **Step 3: Implement.** Generate the code with `crypto/rand`. Issuing a new code cancels the user's earlier unused codes. `Verify` locks the challenge row with `SELECT ... FOR UPDATE` and compares hashes in constant time. A failed send deletes the challenge row.
- [ ] **Step 4: Run the tests.** Expected: PASS.
- [ ] **Step 5: Commit** with message `feat: email codes`.

### Task 6: Two-step sign-in over HTTP

**Files:**
- Create: `api/internal/signin/signin.go`, `api/internal/signin/ratelimit.go`, `api/internal/signin/handlers.go`, `api/internal/httpx/csrf.go`, `api/internal/httpx/errors.go`, `api/migrations/0004_signin_attempts.sql`
- Test: `api/internal/signin/signin_test.go`

**Interfaces:**
- Consumes: `password.Verify`, `otp.Issue`, `otp.Verify`, `session.*`, `account.ByEmail`
- Produces these routes. All bodies are JSON. Errors are `{"error":{"code":"...","message":"..."}}`.
  - `POST /api/sign-in` `{email, password}` → 200 `{"next":"code"}` or `{"next":"done"}` when the device is trusted. Sets a pending or full `sid` cookie. Any failure → 401 `invalid_credentials` with the same body and similar timing.
  - `POST /api/sign-in/code` `{code, trustDevice}` → 200 `{"next":"done"}`, or `{"next":"choose_organization","organizations":[{id,name}]}` when the user has memberships in more than one organization
  - `POST /api/sign-in/code/resend` → 204
  - `POST /api/sign-in/organization` `{organizationId}` → 204
  - `POST /api/sign-out` → 204, `POST /api/sign-out/everywhere` → 204
  - `GET /api/me` → `{user:{id,name,email}, organization:{id,name}, memberships:[{propertyId,propertyName,role}], viewAs}` or 401. A user with no active membership gets 403 `no_access`.
  - `GET /api/csrf` → `{token}`. Every non-GET route requires header `X-CSRF-Token`.
- `httpx.RequireSession(next http.Handler) http.Handler` puts the full `session.Session` in the request context. `httpx.SessionFrom(ctx) session.Session`.

- [ ] **Step 1: Write the failing tests**

```go
func TestPasswordAloneGivesNoAccess(t *testing.T) {
    app := newApp(t); app.seedUser("mai@hotel.com", "twelve chars ok", "front_desk")
    c := app.client(); c.post("/api/sign-in", `{"email":"mai@hotel.com","password":"twelve chars ok"}`).want(200, `"next":"code"`)
    c.get("/api/me").want(401, "")
}

func TestFullSignIn(t *testing.T) {
    app := newApp(t); app.seedUser("mai@hotel.com", "twelve chars ok", "front_desk")
    c := app.client(); c.post("/api/sign-in", `{"email":" Mai@Hotel.com","password":"twelve chars ok"}`)
    c.post("/api/sign-in/code", fmt.Sprintf(`{"code":%q,"trustDevice":false}`, app.lastCode())).want(200, `"next":"done"`)
    c.get("/api/me").want(200, `"role":"front_desk"`)
}

func TestUnknownAndWrongLookTheSame(t *testing.T) {
    app := newApp(t); app.seedUser("mai@hotel.com", "twelve chars ok", "front_desk")
    a := app.client().post("/api/sign-in", `{"email":"mai@hotel.com","password":"wrong wrong wrong"}`)
    b := app.client().post("/api/sign-in", `{"email":"nobody@hotel.com","password":"wrong wrong wrong"}`)
    if a.status != 401 || a.body != b.body { t.Fatalf("%s vs %s", a.body, b.body) }
    if d := absDuration(a.took - b.took); d > 50*time.Millisecond { t.Fatalf("timing differs by %v", d) }
}

func TestTrustedDeviceSkipsCode(t *testing.T) { /* sign in with trustDevice true, sign out, sign in again: next is "done" */ }
func TestLockAfterTenFailures(t *testing.T)   { /* 10 wrong passwords, then the right one returns 401 for 15 minutes */ }
func TestPostWithoutCSRFTokenIsRefused(t *testing.T) { /* 403 csrf */ }
func TestNoMembershipGets403(t *testing.T)    { /* /api/me returns 403 no_access after both steps */ }
func TestTwoOrganizationsMustChoose(t *testing.T) { /* next is "choose_organization" with 2 entries; /api/me is 401 until chosen */ }
```

- [ ] **Step 2: Run `go test ./internal/signin/...`.** Expected: FAIL.
- [ ] **Step 3: Implement.** For an unknown email, run `password.Verify` against a fixed dummy hash so timing matches. Rate-limit counters live in table `signin_attempt(key, at)`. The clock is injected through `Deps.Now func() time.Time`.
- [ ] **Step 4: Run the tests.** Expected: PASS.
- [ ] **Step 5: Commit** with message `feat: two-step sign-in`.

### Task 7: Google sign-in

**Files:**
- Create: `api/internal/signin/google.go`
- Test: `api/internal/signin/google_test.go`

**Interfaces:**
- Consumes: `account.ByEmail`, `session.Create`, `otp.Issue`
- Produces:
  - `type IDTokenVerifier interface { Exchange(ctx context.Context, code, pkceVerifier string) (GoogleIdentity, error) }` with `GoogleIdentity{Subject, Email string; EmailVerified bool}`
  - `GET /api/sign-in/google/start` → 302 to Google with `state`, PKCE `code_challenge` and scopes `openid email`
  - `GET /api/sign-in/google/callback?code&state` → 302 to `/sign-in/code` with a pending session, or to `/sign-in?error=google_not_invited`
- Column `app_user.google_subject` (unique, nullable), added in `api/migrations/0005_google.sql`.

- [ ] **Step 1: Write the failing tests** with a fake `IDTokenVerifier`.

```go
func TestGoogleUnknownEmailIsRefused(t *testing.T) {
    app := newApp(t); app.google.next = signin.GoogleIdentity{Subject: "g1", Email: "stranger@gmail.com", EmailVerified: true}
    loc := app.client().googleRoundTrip()
    if !strings.Contains(loc, "error=google_not_invited") || app.userCount() != 0 { t.Fatal(loc) }
}
func TestGoogleUnverifiedEmailIsRefused(t *testing.T) { /* EmailVerified false → google_not_invited */ }
func TestGoogleStillNeedsTheEmailCode(t *testing.T)   { /* known user → /sign-in/code, /api/me is 401 until the code is entered */ }
func TestGoogleSubjectIsBoundOnFirstUse(t *testing.T) { /* second sign-in with the same email but subject "g2" is refused */ }
func TestGoogleStateMismatchIsRefused(t *testing.T)   { /* callback with a different state → 400 */ }
```

- [ ] **Step 2: Run `go test ./internal/signin/... -run Google`.** Expected: FAIL.
- [ ] **Step 3: Implement** with `coreos/go-oidc` and `golang.org/x/oauth2`. `state` and the PKCE verifier are stored in a short-lived signed cookie.
- [ ] **Step 4: Run the tests.** Expected: PASS.
- [ ] **Step 5: Commit** with message `feat: Google sign-in`.

### Task 8: Roles and permissions

**Files:**
- Create: `api/internal/access/roles.go`, `api/internal/access/matrix.go`, `api/internal/access/middleware.go`
- Test: `api/internal/access/access_test.go`

**Interfaces:**
- Consumes: `httpx.SessionFrom`, table `membership`
- Produces:
  - `type Role string` with `Owner, Manager, FrontDesk, Revenue, Housekeeping`
  - `type Permission string`, one constant per row of the matrix in spec 01, named in snake case: `reservations.view`, `reservations.edit`, `checkin`, `guests.contact.view`, `payments.take`, `refunds.request`, `refunds.approve`, `rates.edit`, `channels.manage`, `housekeeping.view`, `housekeeping.update`, `extras.manage`, `reports.view`, `reports.revenue.view`, `users.manage`, `owners.manage`, `payments.configure`, `branding.configure`, `audit.view`
  - `access.Can(role Role, p Permission) bool`
  - `access.Require(p Permission) func(http.Handler) http.Handler` reads the property from the `X-Property-Id` header, loads the caller's membership, returns 403 `forbidden` when the role lacks `p`, and puts `access.Actor{UserID, OrgID, PropertyID, Role, ViewAs}` in the context
  - `access.ActorFrom(ctx) Actor`
  - `POST /api/view-as` `{role}` sets `Session.ViewAs`. Only an owner may call it. An empty role clears it.

- [ ] **Step 1: Write the failing test.** The table below is the matrix from spec 01, copied cell by cell.

```go
func TestMatrixMatchesSpec(t *testing.T) {
    yes := map[access.Permission][]access.Role{
        "reservations.view":    {Owner, Manager, FrontDesk, Revenue},
        "reservations.edit":    {Owner, Manager, FrontDesk},
        "checkin":              {Owner, Manager, FrontDesk},
        "guests.contact.view":  {Owner, Manager, FrontDesk},
        "payments.take":        {Owner, Manager, FrontDesk},
        "refunds.request":      {Owner, Manager, FrontDesk},
        "refunds.approve":      {Owner, Manager},
        "rates.edit":           {Owner, Manager, Revenue},
        "channels.manage":      {Owner, Manager, Revenue},
        "housekeeping.view":    {Owner, Manager, FrontDesk, Housekeeping},
        "housekeeping.update":  {Owner, Manager, Housekeeping},
        "extras.manage":        {Owner, Manager},
        "reports.view":         {Owner, Manager},
        "reports.revenue.view": {Owner, Manager, Revenue},
        "users.manage":         {Owner, Manager},
        "owners.manage":        {Owner},
        "payments.configure":   {Owner},
        "branding.configure":   {Owner},
        "audit.view":           {Owner, Manager},
    }
    for p, roles := range yes {
        for _, r := range access.AllRoles {
            if got, want := access.Can(r, p), slices.Contains(roles, r); got != want { t.Errorf("%s %s: got %v want %v", r, p, got, want) }
        }
    }
    if len(yes) != len(access.AllPermissions) { t.Fatal("a permission has no row in this test") }
}

func TestRolePerProperty(t *testing.T)            { /* front_desk at hotel A, manager at hotel B: users.manage is 403 with A's id and 200 with B's */ }
func TestViewAsDoesNotRemoveOwnerRights(t *testing.T) { /* owner with ViewAs "front_desk" still passes Require("payments.configure") */ }
func TestOnlyOwnerCanViewAs(t *testing.T)         { /* manager POST /api/view-as → 403 */ }
func TestPropertyOfAnotherOrganization(t *testing.T) { /* X-Property-Id of another org → 403, and RLS returns no row */ }
```

- [ ] **Step 2: Run `go test ./internal/access/...`.** Expected: FAIL.
- [ ] **Step 3: Implement.** An owner's membership has a null `property_id` and applies to every property of the organization.
- [ ] **Step 4: Run the tests.** Expected: PASS.
- [ ] **Step 5: Commit** with message `feat: roles and permission checks`.

### Task 9: Invites and user management

**Files:**
- Create: `api/migrations/0006_invites.sql`, `api/internal/staff/staff.go`, `api/internal/staff/handlers.go`
- Test: `api/internal/staff/staff_test.go`

**Interfaces:**
- Consumes: `access.Require`, `access.ActorFrom`, `mail.Sender`, `session.EndAll`, `session.ForgetDevices`, `password.Check`, `password.Hash`
- Produces:
  - `POST /api/staff/invites` `{email, propertyId, role}` → 201. Needs `users.manage`. Inviting an owner needs `owners.manage`.
  - `GET /api/invites/{token}` → `{email, organizationName, propertyName, role}` or 404
  - `POST /api/invites/{token}/accept` `{name, password}` → 200, then the normal code step. Accepting through Google uses Task 7 with the invite's email.
  - `GET /api/staff` → `[{id, name, email, role, propertyName, lastSignInAt, status}]` with status `invited`, `active` or `deactivated`
  - `PATCH /api/staff/{id}` `{role?, propertyId?}`, `POST /api/staff/{id}/deactivate`, `POST /api/staff/{id}/reset-sign-in`
  - `POST /api/password-reset` `{email}` → always 204. `POST /api/password-reset/{token}` `{password}` → 204.

- [ ] **Step 1: Write the failing tests**

```go
func TestInviteExpiresAfter7Days(t *testing.T)   { /* GET token at +7d+1m → 404 */ }
func TestInviteWorksOnce(t *testing.T)           { /* accept twice → second is 404 */ }
func TestInviteAcceptedTwiceAtOnce(t *testing.T) { /* 2 concurrent accepts → exactly 1 membership row */ }
func TestManagerCannotInviteOwner(t *testing.T)  { /* 403 */ }
func TestLastOwnerCannotBeRemoved(t *testing.T)  { /* deactivate or demote the only owner → 409 last_owner */ }
func TestDeactivateEndsSessions(t *testing.T)    { /* the user's next /api/me is 401, and sign-in is refused */ }
func TestResetSignInForgetsDevices(t *testing.T) { /* trusted device no longer skips the code */ }
func TestRoleChangeAppliesOnNextRequest(t *testing.T) { /* same session, new role in /api/me */ }
func TestPasswordResetEndsAllSessions(t *testing.T)   { /* token valid 30 minutes, usable once */ }
func TestPasswordResetAnswersSameForUnknownEmail(t *testing.T) { /* 204 and no email sent */ }
func TestThreeResetEmailsPerHour(t *testing.T)   { /* 4th request sends nothing */ }
```

- [ ] **Step 2: Run `go test ./internal/staff/...`.** Expected: FAIL.
- [ ] **Step 3: Implement.** Tokens are 32 random bytes and stored hashed. Accepting locks the invite row. Users are never hard-deleted.
- [ ] **Step 4: Run the tests.** Expected: PASS.
- [ ] **Step 5: Commit** with message `feat: invites and user management`.

### Task 10: Activity trail

**Files:**
- Create: `api/migrations/0007_audit.sql`, `api/internal/audit/audit.go`, `api/internal/audit/chain.go`, `api/internal/audit/handlers.go`, `api/internal/httpx/trace.go`
- Modify: `api/internal/staff/staff.go`, `api/internal/signin/signin.go`, `api/internal/access/middleware.go` (record their actions)
- Test: `api/internal/audit/audit_test.go`

**Interfaces:**
- Consumes: `access.ActorFrom`, `db.WithOrg`
- Produces:
  - `httpx.Trace(next http.Handler) http.Handler` puts a new UUID in the context and in response header `X-Trace-Id`. `httpx.TraceID(ctx) uuid.UUID`.
  - `type audit.Entry struct { Action, TargetType string; TargetID uuid.UUID; Before, After any; Reason string }`
  - `audit.Record(ctx context.Context, tx pgx.Tx, e Entry) error` reads the actor, role view, IP address, device and trace ID from the context
  - `audit.RecordSystem(ctx, tx, orgID uuid.UUID, job string, e Entry) error`
  - `audit.VerifyChain(ctx, tx, orgID uuid.UUID) (brokenAt *uuid.UUID, err error)`
  - `GET /api/activity?user&action&targetType&targetId&from&to&cursor` → `{items:[{id, at, actor:{id,name,role}, viewAs, action, target:{type,id,label}, before, after, reason, device, ip, place, traceId}], nextCursor}`. Needs `audit.view`. A manager sees only their property.
  - `GET /api/activity/target/{type}/{id}` → the same items without `ip` and `place`, for anyone who can read the target
- Action names recorded in this milestone: `signin.succeeded`, `signin.failed`, `signout`, `user.invited`, `user.joined`, `user.role_changed`, `user.deactivated`, `user.signin_reset`, `password.changed`, `view_as.changed`.

- [ ] **Step 1: Write the failing tests**

```go
func TestAppRoleCannotChangeTrail(t *testing.T) {
    pool := testdb.New(t); seedEntry(t, pool)
    for _, q := range []string{`update audit_log set action = 'x'`, `delete from audit_log`} {
        if _, err := pool.Exec(ctx, q); err == nil { t.Fatalf("%q was allowed", q) }
    }
}

func TestChainDetectsTampering(t *testing.T) {
    pool := testdb.New(t); org := seedOrg(t, pool, "A")
    for i := 0; i < 3; i++ { seedEntryFor(t, pool, org) }
    if at, _ := verify(pool, org); at != nil { t.Fatal("clean chain reported broken") }
    asMigrator(t, pool, `update audit_log set after = '{"role":"owner"}' where seq = 2`)
    if at, _ := verify(pool, org); at == nil { t.Fatal("tampering not found") }
}

func TestEntryCarriesActorViewAsAndTrace(t *testing.T) { /* owner with ViewAs front_desk changes a role: row has actor owner, view_as front_desk, the request's trace id, before and after */ }
func TestSecretsNeverReachTheTrail(t *testing.T)       { /* full sign-in and password change: no row contains the password, the code or a token */ }
func TestManagerSeesOnlyOwnProperty(t *testing.T)      { /* /api/activity as manager of A has no rows of B */ }
func TestFrontDeskSeesNoIPAddress(t *testing.T)        { /* /api/activity → 403; target history has no "ip" key */ }
func TestEveryWriteRouteRecordsOneRow(t *testing.T)    { /* walk the router's non-GET routes with valid input; each adds exactly 1 audit row */ }
func TestTwoWritesAtOnceKeepOneChain(t *testing.T)     { /* 20 concurrent Record calls: seq is 1..20 with no gap and VerifyChain is clean */ }
```

- [ ] **Step 2: Run `go test ./internal/audit/...`.** Expected: FAIL.
- [ ] **Step 3: Write the migration.** `audit_log` has `seq bigint` per organization, `prev_hash`, `row_hash`. Grant the `app` role `INSERT` and `SELECT` only.
- [ ] **Step 4: Implement.** `Record` takes an advisory lock on the organization, reads the last `row_hash`, and stores `row_hash = sha256(prev_hash || canonical JSON of the row)`. `Before` and `After` pass through a redactor that drops keys named `password`, `code`, `token`, `secret`.
- [ ] **Step 5: Add `audit.Record` calls** to the handlers of Tasks 6 to 9, inside their existing transactions.
- [ ] **Step 6: Run `go test ./...`.** Expected: PASS.
- [ ] **Step 7: Commit** with message `feat: tamper-evident activity trail`.

### Task 11: Notifications core

**Files:**
- Create: `api/migrations/0008_notifications.sql`, `api/internal/notify/notify.go`, `api/internal/notify/deliver.go`, `api/internal/notify/sse.go`, `api/internal/notify/handlers.go`
- Test: `api/internal/notify/notify_test.go`

**Interfaces:**
- Consumes: `mail.Sender`, `access.ActorFrom`, River client
- Produces:
  - `type Level string` with `Urgent, ActionNeeded, Info`
  - `type notify.Input struct { OrgID, PropertyID uuid.UUID; ToRole access.Role; ToUser *uuid.UUID; Level Level; Key, Title, Body, ActionURL string }`
  - `notify.Send(ctx context.Context, tx pgx.Tx, in Input) error`. The same `Key` for the same recipient updates the open notification and raises its `count`, and creates no new row.
  - `notify.Resolve(ctx, tx, orgID uuid.UUID, key string, by uuid.UUID) error`
  - River job `notify.EscalateArgs{Key}`: an unopened `Urgent` notification goes to managers after 10 minutes and to owners after 30 minutes
  - `GET /api/notifications` → `{unread, items:[{id, level, title, body, actionUrl, count, at, openedAt, resolvedAt, resolvedBy}]}`
  - `POST /api/notifications/{id}/open`, `POST /api/notifications/open-all`
  - `GET /api/notifications/stream` (server-sent events, event name `notification`)
  - `GET /api/notification-preferences`, `PUT /api/notification-preferences` `{quietFrom, quietTo, email:{urgent,actionNeeded}}`
- Delivery rules: `Urgent` goes in-app and by email and ignores quiet hours. `ActionNeeded` goes in-app, and by email if the user chose so. `Info` goes in-app only.

- [ ] **Step 1: Write the failing tests**

```go
func TestSameKeyUpdatesOneNotification(t *testing.T) {
    app := newApp(t); mgr := app.seedUser("m@hotel.com", "", "manager")
    for i := 0; i < 20; i++ { app.send(notify.Input{ToRole: access.Manager, Level: notify.Urgent, Key: "channel-down:expedia", Title: "Expedia is not confirming updates"}) }
    got := app.as(mgr).notifications()
    if len(got.Items) != 1 || got.Items[0].Count != 20 { t.Fatalf("%+v", got) }
    if n := len(app.mail.Sent()); n != 1 { t.Fatalf("%d emails, want 1", n) }
}

func TestOnlyTargetRoleAtThatProperty(t *testing.T) { /* manager of hotel B and front desk of hotel A receive nothing */ }
func TestEscalation(t *testing.T)                   { /* front desk alert unopened: manager gets it at +10m, owner at +30m; opening at +5m stops both */ }
func TestQuietHours(t *testing.T)                   { /* 23:00 with quiet 22:00–07:00: action-needed email is held until 07:00, urgent email is sent at once */ }
func TestResolveMarksEveryRecipient(t *testing.T)   { /* resolvedAt and resolvedBy set for all holders of the key */ }
func TestStreamDeliversWithoutReload(t *testing.T)  { /* open the SSE stream, Send, read one "notification" event within 2 s */ }
func TestStreamIsPerUser(t *testing.T)              { /* user B's stream receives nothing for a notification sent to user A */ }
```

- [ ] **Step 2: Run `go test ./internal/notify/...`.** Expected: FAIL.
- [ ] **Step 3: Implement.** Use PostgreSQL `LISTEN/NOTIFY` on channel `notifications` to wake the SSE handlers. Quiet hours are evaluated in the property's time zone.
- [ ] **Step 4: Run the tests.** Expected: PASS.
- [ ] **Step 5: Commit** with message `feat: notifications with dedupe, escalation and live stream`.

### Task 12: Design tokens and shared components

**Files:**
- Create: `web/src/lib/tokens.css`, `web/src/lib/ui/Button.svelte`, `Field.svelte`, `Segmented.svelte`, `StatusBadge.svelte`, `StatTile.svelte`, `SectionTitle.svelte`, `PageHeader.svelte`, `Card.svelte`, `Icon.svelte`, `HoverCard.svelte`, `AlertList.svelte`, `web/src/routes/dev/components/+page.svelte`
- Test: `web/src/lib/ui/ui.test.ts`, `web/src/lib/tokens.test.ts`

**Interfaces:**
- Produces these props. The mockup canvas page "Screens and components" shows each one.
  - `Button`: `variant: 'primary' | 'secondary' | 'quiet'`, `icon?: IconName`, `iconOnly?: boolean`, `block?: boolean`, `disabled?: boolean`, `href?: string`, `onclick?`, default slot for the label
  - `Field`: `label: string`, `type`, `value` (bindable), `placeholder?`, `autocomplete?`, `error?: string`
  - `Segmented`: `options: {id: string, label: string}[]`, `value` (bindable)
  - `StatusBadge`: `tone: 'success' | 'warning' | 'danger' | 'info' | 'neutral'`, slot
  - `StatTile`: `label`, `value`, `unit?`, `note?`, `tone?: 'plain' | 'up' | 'down'`
  - `SectionTitle`: `title`, `note?`. `PageHeader`: `title`, `note?`, slots `secondary` and `primary`.
  - `HoverCard`: `title`, `badge?`, `tone?`, `lines: {label, value}[]`, `note?`, slot `trigger`. Opens after 300 ms hover or on focus, closes on Escape, sets `aria-describedby`.
  - `AlertList`: `items: {id, level: 'urgent' | 'today', title, reason, action}[]`, `onact(id)`, `ondismiss(id)`
- `tokens.css` defines every value in Global Constraints as a CSS custom property in two layers: raw palette and scale values, then meaning tokens such as `--color-action`, `--color-text-muted`, `--color-surface`, `--color-border`, `--color-status-success-bg`, `--space-4`, `--radius-control`, `--font-size-body`, `--control-height`, `--duration-screen`, `--ease-standard`, `--layer-popover`. Components use meaning tokens only. A hotel brand colour overrides `--color-action` and the tokens derived from it on the page root, and nothing else.

- [ ] **Step 1: Write the failing tests** with `@testing-library/svelte`.

```ts
test('icon-only button has an accessible name', () => {
  render(Button, { icon: 'close', iconOnly: true, children: text('Close chat') });
  expect(screen.getByRole('button', { name: 'Close chat' })).toBeInTheDocument();
});
test('button is 44 px high and uses the action token', () => { /* computed height 44px; primary background is var(--color-action) */ });
test('segmented control marks the chosen option', async () => { /* click "Revenue": aria-pressed true on it, false on the others, bound value 'revenue' */ });
test('field shows its error and links it to the input', () => { /* aria-invalid true, aria-describedby points at the message */ });
test('hover card opens on focus and closes on Escape', async () => { /* role tooltip appears, then is gone */ });
test('hover card does not open before 300 ms of hover', async () => { /* fake timers: absent at 299 ms, present at 300 ms */ });
test('alert list calls onact with the id and removes the row', async () => { /* 2 rows → click action on 'a' → 1 row, onact('a') */ });
test('empty alert list says nothing needs attention', () => { /* text "Nothing needs attention" */ });

// tokens.test.ts
test('no component file holds a literal design value', () => {
  /* read every .svelte and .css under src except lib/tokens.css; fail on: a hex colour, rgb( or hsl(, a px value on padding, margin, gap, border-radius, font-size or line-height, a ms or s duration, a z-index number, a font-family name */
});
test('every var(--x) used in a component is defined in tokens.css', () => { /* collect var(--name) from component files; each name must be declared in tokens.css */ });
test('no meaning token points at another meaning token more than 2 deep', () => { /* keeps the palette to meaning mapping readable */ });
test('a brand override changes the action colour and no status colour', () => { /* set --color-action on the root: computed action changes; --color-status-success-bg, -warning-bg, -danger-bg, -info-bg are unchanged */ });
test('every token the spec lists exists', () => { /* the values in Global Constraints: sizes 12,14,16,20,28; spacing 4,8,12,16,24,32; radius 4,8,12; control height 44; the 5 breakpoint-related sizes */ });
```

- [ ] **Step 2: Run `npm test` in `web/`.** Expected: FAIL.
- [ ] **Step 3: Implement the components.** Build `Segmented` and `HoverCard` on Bits UI primitives. Write each component's CSS in its own `<style>` block using `var(--token)` only, with no literal colour, size, duration or layer. Use CSS logical properties. Hover styles sit inside `@media (hover: hover)`.
- [ ] **Step 4: Build `/dev/components`**, a page that shows every component in every variant. It is the target for the design checker and for visual review.
- [ ] **Step 5: Run `npm test` and `npx @enmera/ai-design-check web/src`.** Expected: tests PASS, 0 errors and 0 warnings.
- [ ] **Step 6: Commit** with message `feat: design tokens and shared components`.

### Task 13: Sign-in screens

**Files:**
- Create: `web/src/lib/api.ts`, `web/src/routes/(auth)/+layout.svelte`, `web/src/routes/(auth)/sign-in/+page.svelte`, `sign-in/code/+page.svelte`, `sign-in/organization/+page.svelte`, `invite/[token]/+page.svelte`, `reset/+page.svelte`, `reset/[token]/+page.svelte`, `web/messages/en.json`
- Test: `web/e2e/sign-in.spec.ts`

**Interfaces:**
- Consumes: the routes of Tasks 6, 7 and 9; `Button`, `Field`, `Card` from Task 12
- Produces: `api.get<T>(path): Promise<T>`, `api.post<T>(path, body): Promise<T>`. Both send the CSRF token and the current `X-Property-Id`, and throw `ApiError{status, code, message}`.
- Exact copy, all in `en.json`:
  - Sign-in title: "Sign in". Button: "Sign in". Google button: "Continue with Google". Link: "Forgot your password?"
  - Wrong details: "The email or password is not right."
  - Code title: "Enter the code we emailed you". Help: "We sent a 6-digit code to {email}. It works for 10 minutes." Checkbox: "Trust this device for 30 days". Link: "Send a new code".
  - Wrong code: "That code is not right. Check the email and try again." Expired: "That code has expired. We can send a new one." Cancelled: "Too many wrong tries. We can send a new code." Too many: "You asked for 5 codes this hour. Try again later." Send failed: "We could not send the code. Try again."
  - No access: "Your account has no access. Ask your manager."
  - Google not invited: "This Google account has not been invited. Ask your manager for an invite."

- [ ] **Step 1: Write the failing Playwright tests** against the API started with the fake mail sender and a test-only route `GET /api/test/last-code?email=`.

```ts
test('sign in with password and code', async ({ page }) => {
  await page.goto('/sign-in');
  await page.getByLabel('Email').fill('mai@hotel.com');
  await page.getByLabel('Password').fill('twelve chars ok');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: 'Enter the code we emailed you' })).toBeVisible();
  await page.getByLabel('Code').fill(await lastCode('mai@hotel.com'));
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page).toHaveURL('/');
});
test('wrong password shows the message and keeps the email', async ({ page }) => { /* "The email or password is not right." and the email field still filled */ });
test('wrong code shows the message', async ({ page }) => { /* fill 000000 */ });
test('invite link sets a password then asks for the code', async ({ page }) => { /* /invite/{token} */ });
test('expired invite says so and offers no form', async ({ page }) => { /* "This invite has expired. Ask your manager for a new one." */ });
for (const width of [390, 744, 1280]) test(`sign-in has no sideways scroll at ${width}`, async ({ page }) => { /* scrollWidth === clientWidth */ });
test('sign-in pages pass axe', async ({ page }) => { /* 0 violations on each auth page */ });
```

- [ ] **Step 2: Run `npx playwright test e2e/sign-in.spec.ts`.** Expected: FAIL.
- [ ] **Step 3: Implement the pages.** The code input has `inputmode="numeric"`, `autocomplete="one-time-code"` and a 16 px font. Each page has one filled primary button.
- [ ] **Step 4: Run the tests.** Expected: PASS.
- [ ] **Step 5: Commit** with message `feat: sign-in, code, invite and reset screens`.

### Task 14: App shell, staff screen and activity screen

**Files:**
- Create: `web/src/lib/session.ts`, `web/src/lib/nav.ts`, `web/src/routes/(app)/+layout.ts`, `web/src/routes/(app)/+layout.svelte`, `web/src/lib/shell/Sidebar.svelte`, `TopBar.svelte`, `BottomBar.svelte`, `NotificationBell.svelte`, `web/src/routes/(app)/+page.svelte`, `web/src/routes/(app)/staff/+page.svelte`, `web/src/routes/(app)/activity/+page.svelte`
- Test: `web/e2e/shell.spec.ts`, `web/src/lib/nav.test.ts`

**Interfaces:**
- Consumes: `GET /api/me`, `POST /api/view-as`, the staff routes of Task 9, the activity routes of Task 10, the notification routes of Task 11, all Task 12 components
- Produces:
  - `nav.itemsFor(role: Role): {id, label, icon, href}[]`. Owner: Overview, Reports, Payouts, Staff, Activity. Manager: Overview, Staff, Activity. Front desk: Calendar, Messages, Arrivals, Guests, Payments. Revenue: Rates, Rate plans, Channels, Sync log. Housekeeping: Rooms, Issues, Lost and found. In this milestone only Staff and Activity lead to real screens, and the others open a placeholder that names the milestone that builds them.
  - `session.current` store with `{user, organization, memberships, activeProperty, role, viewAs}`. `session.effectiveRole` is `viewAs` when set, otherwise `role`.
- Behaviour:
  - Desktop: sidebar 240 px that collapses to 68 px. Tablet: 68 px rail that expands. Phone: top bar and bottom tab bar with up to 5 items. The sidebar state is remembered per user in `localStorage`.
  - The "Viewing as" segmented control is rendered only for owners. While a view is active, a banner reads "Viewing as {role}" with a "Back to owner view" button.
  - The bell shows the unread count, opens a panel, and updates from the SSE stream.
  - A 401 from any API call stores the current path and unsaved form values in `sessionStorage`, opens `/sign-in`, and returns after sign-in.
  - A user in two organizations sees `/sign-in/organization` before the shell.
  - Changing screen fades the content in and raises it 8 px over 200 ms.

- [ ] **Step 1: Write the failing tests**

```ts
// nav.test.ts
test('each role gets exactly its menu', () => {
  expect(itemsFor('housekeeping').map(i => i.label)).toEqual(['Rooms', 'Issues', 'Lost and found']);
  expect(itemsFor('front_desk').map(i => i.label)).not.toContain('Staff');
  expect(itemsFor('owner').map(i => i.label)).toContain('Activity');
});

// shell.spec.ts
test('front desk does not see Staff, and /staff is refused', async ({ page }) => { /* no link; direct visit shows "You do not have access to this page." */ });
test('owner switches view and the banner returns them', async ({ page }) => { /* choose Front desk: menu changes, banner shows; "Back to owner view" restores */ });
test('manager has no Viewing as control', async ({ page }) => { /* not in the DOM */ });
test('layout per width', async ({ page }) => { /* 1280: sidebar 240 px. 744: rail 68 px. 390: no sidebar, bottom bar with labels */ });
test('sidebar collapse is remembered after reload', async ({ page }) => {});
test('bell count rises without reload', async ({ page }) => { /* trigger a test notification; count goes 0 → 1 within 3 s */ });
test('owner invites a user and sees them as invited', async ({ page }) => { /* Staff → "Invite user" → row with status Invited */ });
test('deactivating a user needs a confirm and updates the row', async ({ page }) => {});
test('activity lists the invite with actor, time and before/after', async ({ page }) => {});
test('expired session keeps the typed invite email', async ({ page }) => { /* expire the session server-side, submit, sign in again, the email is still in the field */ });
test('user in two organizations must pick one', async ({ page }) => {});
test('reduced motion removes the screen animation', async ({ page }) => { /* emulateMedia reducedMotion: animation-duration ≤ 1ms */ });
for (const width of [390, 744, 1280]) test(`shell, staff and activity pass axe and do not scroll sideways at ${width}`, async ({ page }) => {});
```

- [ ] **Step 2: Run `npm test` and `npx playwright test e2e/shell.spec.ts`.** Expected: FAIL.
- [ ] **Step 3: Implement.** The staff table scrolls inside its own box on narrow screens. Each list has an empty state with the next step: "No one has been invited yet. Invite your first user."
- [ ] **Step 4: Run all web tests and `npx @enmera/ai-design-check web/src`.** Expected: PASS, 0 errors and 0 warnings.
- [ ] **Step 5: Look at the shell, Staff and Activity at 390, 744 and 1280 px** next to the mockup canvas, and answer the four questions of spec 08 for each area. Record the answers in the pull request description.
- [ ] **Step 6: Commit** with message `feat: app shell, staff and activity screens`.

### Task 15: Nightly chain check and first-owner setup

**Files:**
- Create: `api/internal/audit/job.go`, `api/cmd/server/setup.go`
- Modify: `api/cmd/server/main.go`
- Test: `api/internal/audit/job_test.go`, `api/cmd/server/setup_test.go`

**Interfaces:**
- Consumes: `audit.VerifyChain`, `notify.Send`, `tenant.CreateOrganization`, `tenant.CreateProperty`, `account.Create`, `staff` invite functions
- Produces:
  - River periodic job `audit.VerifyAllArgs{}` at 03:00 UTC. A broken chain sends an `Urgent` notification with key `trail-broken:{orgID}` to owners.
  - Command `server setup --org "Sample Hotel" --property "Sample Hotel" --timezone "Asia/Ho_Chi_Minh" --currency USD --owner-email owner@example.com` creates the organization, the property and an owner invite, and prints the invite link once.

- [ ] **Step 1: Write the failing tests**

```go
func TestBrokenChainAlertsOwners(t *testing.T)  { /* tamper as migrator, run the job: owner has 1 urgent notification, manager has 0 */ }
func TestCleanChainIsSilent(t *testing.T)       { /* run the job: no notification */ }
func TestSetupCreatesOwnerInvite(t *testing.T)  { /* 1 organization, 1 property, 1 invite with role owner; the printed link accepts */ }
func TestSetupRefusesSecondRunForSameEmail(t *testing.T) { /* clear error, nothing created */ }
```

- [ ] **Step 2: Run `go test ./...`.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run `go test ./...`, `npm test` and `npx playwright test`.** Expected: PASS.
- [ ] **Step 5: Commit** with message `feat: nightly trail check and first-owner setup`.

---

## Spec coverage

| Spec section | Task |
| --- | --- |
| 01 Roles, permission matrix | 8 |
| 01 Owner role switching | 8, 10, 14 |
| 01 User management | 9, 14 |
| 02 Passwords | 3, 9 |
| 02 Google sign-in | 7 |
| 02 Email codes, trusted device | 4, 5, 6 |
| 02 Sessions, CSRF | 4, 6 |
| 02 Rate limits | 5, 6, 9 |
| 02 Audit log | 10 |
| 03 Table rules, tenancy tables, row-level security | 2 |
| 08 Tokens, components, breakpoints, motion, quality gate | 12, 13, 14 |
| 10 Notifications: levels, dedupe, escalation, quiet hours, live update | 11, 14 |
| 10 Activity trail: fields, trace ID, hash chain, who reads it | 10, 14, 15 |

Left for later plans, by design: guest access and data-protection items in spec 02 that need bookings and uploads, the security checks in spec 10 that need alerts (spec 09), web push, the daily summary, undo, and shift handover.
