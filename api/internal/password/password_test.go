package password_test

import (
	"context"
	"crypto/sha1" //nolint:gosec // the breach list is keyed by SHA-1; it is not used for security here
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"hotelpms/api/internal/password"
)

var ctx = context.Background()

type fakeChecker struct {
	breached map[string]bool
	err      error
}

func (f fakeChecker) Breached(_ context.Context, plain string) (bool, error) {
	return f.breached[plain], f.err
}

func TestCheckPolicy(t *testing.T) {
	fake := fakeChecker{breached: map[string]bool{"correct horse battery": true}}
	if err := password.Check(ctx, fake, "short"); !errors.Is(err, password.ErrTooShort) {
		t.Fatal(err)
	}
	if err := password.Check(ctx, fake, "elevenchars"); !errors.Is(err, password.ErrTooShort) {
		t.Fatal(err)
	}
	if err := password.Check(ctx, fake, "correct horse battery"); !errors.Is(err, password.ErrBreached) {
		t.Fatal(err)
	}
	if err := password.Check(ctx, fake, "twelve chars ok"); err != nil {
		t.Fatal(err)
	}
}

func TestLengthIsCountedInCharactersNotBytes(t *testing.T) {
	fake := fakeChecker{}
	if err := password.Check(ctx, fake, strings.Repeat("ñ", 11)); !errors.Is(err, password.ErrTooShort) {
		t.Fatalf("11 characters (22 bytes) accepted: %v", err)
	}
	if err := password.Check(ctx, fake, strings.Repeat("ñ", 12)); err != nil {
		t.Fatalf("12 characters rejected: %v", err)
	}
}

func TestNoCharacterMixRulesAreForced(t *testing.T) {
	if err := password.Check(ctx, fakeChecker{}, "aaaaaaaaaaaa"); err != nil {
		t.Fatalf("a long single-class password was rejected by a composition rule: %v", err)
	}
}

func TestBreachCheckerDownDoesNotBlock(t *testing.T) {
	if err := password.Check(ctx, fakeChecker{err: errors.New("timeout")}, "twelve chars ok"); err != nil {
		t.Fatal(err)
	}
}

func TestHashVerify(t *testing.T) {
	h, err := password.Hash("twelve chars ok")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$") {
		t.Fatal(h)
	}
	if !password.Verify(h, "twelve chars ok") {
		t.Fatal("right password refused")
	}
	if password.Verify(h, "wrong") || password.Verify(h, "twelve chars ok ") {
		t.Fatal("wrong password accepted")
	}
}

func TestHashUsesTheSpecifiedParameters(t *testing.T) {
	h, _ := password.Hash("twelve chars ok")
	if !strings.Contains(h, "$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("parameters not 64 MiB, 3 passes, 2 threads: %s", h)
	}
}

func TestEachHashHasItsOwnSalt(t *testing.T) {
	a, _ := password.Hash("twelve chars ok")
	b, _ := password.Hash("twelve chars ok")
	if a == b {
		t.Fatal("two hashes of one password are identical")
	}
}

func TestVerifyRejectsMalformedHashesWithoutPanicking(t *testing.T) {
	for _, bad := range []string{"", "plain text", "$argon2id$", "$argon2id$v=19$m=65536,t=3,p=2$AAAA", "$bcrypt$x$y$z$w", "$argon2id$v=19$m=x,t=3,p=2$AAAA$BBBB"} {
		if password.Verify(bad, "twelve chars ok") {
			t.Errorf("%q verified", bad)
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}

func sha1Hex(s string) string {
	sum := sha1.Sum([]byte(s)) //nolint:gosec
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func TestRangeCheckerSendsOnlyTheFirstFiveHashCharacters(t *testing.T) {
	plain := "twelve chars ok"
	full := sha1Hex(plain)
	var gotURL string
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return reply(200, full[5:]+":42\r\n0018A45C4D1DEF81644B54AB7F969B88D65:1\r\n"), nil
	})}
	breached, err := password.NewRangeChecker(client).Breached(ctx, plain)
	if err != nil || !breached {
		t.Fatalf("breached=%v err=%v", breached, err)
	}
	if !strings.HasSuffix(gotURL, "/range/"+full[:5]) {
		t.Fatalf("url %q should end in /range/%s", gotURL, full[:5])
	}
	if strings.Contains(gotURL, full[5:]) || strings.Contains(gotURL, plain) {
		t.Fatalf("the url leaks more than the 5 character prefix: %s", gotURL)
	}
}

func TestRangeCheckerNotFoundAndPadding(t *testing.T) {
	plain := "twelve chars ok"
	full := sha1Hex(plain)
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		// A padding entry for this very suffix with a count of 0 means "not in the list".
		return reply(200, full[5:]+":0\r\nABCDEF0123456789ABCDEF0123456789ABC:7\r\n"), nil
	})}
	breached, err := password.NewRangeChecker(client).Breached(ctx, plain)
	if err != nil || breached {
		t.Fatalf("breached=%v err=%v", breached, err)
	}
}

func TestRangeCheckerReportsServerErrors(t *testing.T) {
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) { return reply(503, ""), nil })}
	if _, err := password.NewRangeChecker(client).Breached(ctx, "twelve chars ok"); err == nil {
		t.Fatal("a 503 was treated as an answer")
	}
}
