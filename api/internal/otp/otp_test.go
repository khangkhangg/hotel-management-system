package otp_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/account"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/db/testdb"
	"hotelpms/api/internal/mail"
	"hotelpms/api/internal/otp"
)

var (
	ctx   = context.Background()
	now   = time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	where = otp.Where{Browser: "Chrome on Windows", Place: "Da Nang, Vietnam"}
	six   = regexp.MustCompile(`\b(\d{6})\b`)
)

func seedUser(t *testing.T, pool *pgxpool.Pool) account.User {
	t.Helper()
	var u account.User
	if err := db.WithoutOrg(ctx, pool, func(tx pgx.Tx) error {
		if _, err := account.Create(ctx, tx, "mai@hotel.com", "Mai Pham"); err != nil {
			return err
		}
		var e error
		u, e = account.ByEmail(ctx, tx, "mai@hotel.com")
		return e
	}); err != nil {
		t.Fatal(err)
	}
	return u
}

func codeOf(t *testing.T, m mail.Message) string {
	t.Helper()
	match := six.FindStringSubmatch(m.Text)
	if match == nil {
		t.Fatalf("no 6 digit code in %q", m.Text)
	}
	return match[1]
}

func issue(t *testing.T, pool *pgxpool.Pool, m *mail.Fake, u account.User, at time.Time) string {
	t.Helper()
	if err := otp.Issue(ctx, pool, m, u, where, at); err != nil {
		t.Fatal(err)
	}
	sent := m.Sent()
	return codeOf(t, sent[len(sent)-1])
}

func TestCodeLifecycle(t *testing.T) {
	pool := testdb.New(t)
	u, m := seedUser(t, pool), mail.NewFake()
	code := issue(t, pool, m, u, now)
	if err := otp.Verify(ctx, pool, u.ID, code, now.Add(10*time.Minute+time.Second)); !errors.Is(err, otp.ErrExpired) {
		t.Fatalf("after 10 minutes: %v", err)
	}
	code = issue(t, pool, m, u, now)
	if err := otp.Verify(ctx, pool, u.ID, code, now.Add(9*time.Minute)); err != nil {
		t.Fatalf("inside 10 minutes: %v", err)
	}
	if err := otp.Verify(ctx, pool, u.ID, code, now.Add(10*time.Minute)); err == nil {
		t.Fatal("a code was used twice")
	}
}

func TestWrongCodeAndNoCode(t *testing.T) {
	pool := testdb.New(t)
	u, m := seedUser(t, pool), mail.NewFake()
	if err := otp.Verify(ctx, pool, u.ID, "123456", now); !errors.Is(err, otp.ErrWrong) {
		t.Fatalf("no code issued: %v", err)
	}
	code := issue(t, pool, m, u, now)
	bad := "000000"
	if code == bad {
		bad = "111111"
	}
	if err := otp.Verify(ctx, pool, u.ID, bad, now); !errors.Is(err, otp.ErrWrong) {
		t.Fatalf("wrong code: %v", err)
	}
	if err := otp.Verify(ctx, pool, u.ID, code, now); err != nil {
		t.Fatalf("a wrong attempt must not spoil the right code: %v", err)
	}
}

func TestFiveWrongAttemptsCancel(t *testing.T) {
	pool := testdb.New(t)
	u, m := seedUser(t, pool), mail.NewFake()
	good := issue(t, pool, m, u, now)
	bad := "000000"
	if good == bad {
		bad = "111111"
	}
	for i := 0; i < 5; i++ {
		if err := otp.Verify(ctx, pool, u.ID, bad, now); !errors.Is(err, otp.ErrWrong) && !errors.Is(err, otp.ErrCancelled) {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	if err := otp.Verify(ctx, pool, u.ID, good, now); !errors.Is(err, otp.ErrCancelled) {
		t.Fatalf("the right code worked after 5 wrong tries: %v", err)
	}
}

func TestFourWrongAttemptsDoNotCancel(t *testing.T) {
	pool := testdb.New(t)
	u, m := seedUser(t, pool), mail.NewFake()
	good := issue(t, pool, m, u, now)
	bad := "000000"
	if good == bad {
		bad = "111111"
	}
	for i := 0; i < 4; i++ {
		otp.Verify(ctx, pool, u.ID, bad, now)
	}
	if err := otp.Verify(ctx, pool, u.ID, good, now); err != nil {
		t.Fatalf("cancelled one attempt early: %v", err)
	}
}

func TestFivePerHour(t *testing.T) {
	pool := testdb.New(t)
	u, m := seedUser(t, pool), mail.NewFake()
	for i := 0; i < 5; i++ {
		issue(t, pool, m, u, now)
	}
	if err := otp.Issue(ctx, pool, m, u, where, now); !errors.Is(err, otp.ErrTooMany) {
		t.Fatalf("6th code in an hour: %v", err)
	}
	if len(m.Sent()) != 5 {
		t.Fatalf("%d emails sent, want 5", len(m.Sent()))
	}
	issue(t, pool, m, u, now.Add(61*time.Minute))
}

func TestSendFailureDoesNotCount(t *testing.T) {
	pool := testdb.New(t)
	u, m := seedUser(t, pool), mail.NewFake()
	m.FailNext()
	if err := otp.Issue(ctx, pool, m, u, where, now); !errors.Is(err, otp.ErrSendFailed) {
		t.Fatalf("got %v", err)
	}
	for i := 0; i < 5; i++ {
		issue(t, pool, m, u, now)
	}
}

func TestSendFailureKeepsThePreviousCodeValid(t *testing.T) {
	pool := testdb.New(t)
	u, m := seedUser(t, pool), mail.NewFake()
	first := issue(t, pool, m, u, now)
	m.FailNext()
	if err := otp.Issue(ctx, pool, m, u, where, now); !errors.Is(err, otp.ErrSendFailed) {
		t.Fatal(err)
	}
	if err := otp.Verify(ctx, pool, u.ID, first, now); err != nil {
		t.Fatalf("a failed resend cancelled the code the user already had: %v", err)
	}
}

func TestNewCodeCancelsTheOlderOne(t *testing.T) {
	pool := testdb.New(t)
	u, m := seedUser(t, pool), mail.NewFake()
	first := issue(t, pool, m, u, now)
	second := issue(t, pool, m, u, now.Add(time.Minute))
	if first != second {
		if err := otp.Verify(ctx, pool, u.ID, first, now.Add(2*time.Minute)); err == nil {
			t.Fatal("the older code still works after a newer one was sent")
		}
	}
	if err := otp.Verify(ctx, pool, u.ID, second, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("the newest code: %v", err)
	}
}

func TestSameCodeTwiceAtOnceSucceedsOnce(t *testing.T) {
	pool := testdb.New(t)
	u, m := seedUser(t, pool), mail.NewFake()
	code := issue(t, pool, m, u, now)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if otp.Verify(ctx, pool, u.ID, code, now) == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 {
		t.Fatalf("%d requests used the same code, want 1", ok)
	}
}

func TestCodeIsStoredHashedAndEmailHasNoLink(t *testing.T) {
	pool := testdb.New(t)
	u, m := seedUser(t, pool), mail.NewFake()
	code := issue(t, pool, m, u, now)
	var dump string
	if err := db.WithoutOrg(ctx, pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select coalesce(string_agg(row_to_json(c)::text, ' '), '') from otp_challenge c`).Scan(&dump)
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dump, code) {
		t.Fatal("the plain code is stored")
	}
	if strings.Contains(m.Sent()[0].Text, "http") {
		t.Fatal("the email carries a link")
	}
}

func TestEmailNamesTheTimeBrowserAndPlace(t *testing.T) {
	pool := testdb.New(t)
	u, m := seedUser(t, pool), mail.NewFake()
	issue(t, pool, m, u, now)
	msg := m.Sent()[0]
	for _, want := range []string{"10 minutes", "Chrome on Windows", "Da Nang, Vietnam", "09:00"} {
		if !strings.Contains(msg.Text, want) {
			t.Errorf("the email does not say %q:\n%s", want, msg.Text)
		}
	}
	if msg.To != "mai@hotel.com" || msg.Subject == "" {
		t.Fatalf("%+v", msg)
	}
}

func TestCodesAreSixDigitsIncludingLeadingZeros(t *testing.T) {
	pool := testdb.New(t)
	m := mail.NewFake()
	var u account.User
	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		var uid uuid.UUID
		db.WithoutOrg(ctx, pool, func(tx pgx.Tx) (e error) {
			uid, e = account.Create(ctx, tx, strings.Repeat("a", i+1)+"@hotel.com", "x")
			u = account.User{ID: uid, Email: strings.Repeat("a", i+1) + "@hotel.com"}
			return
		})
		seen[issue(t, pool, m, u, now)] = true
	}
	if len(seen) < 30 {
		t.Fatalf("only %d different codes in 40: not random", len(seen))
	}
}
