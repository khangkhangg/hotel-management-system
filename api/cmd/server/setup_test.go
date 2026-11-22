package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/db/testdb"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/mail"
	"hotelpms/api/internal/staff"
)

var ctx = context.Background()

var linkRE = regexp.MustCompile(`https://app\.example\.com/invite/([A-Za-z0-9_-]+)`)

func args(extra ...string) []string {
	base := []string{"--org", "Sample Hotel", "--property", "Sample Hotel", "--timezone", "Asia/Ho_Chi_Minh", "--currency", "USD", "--owner-email", "owner@example.com"}
	return append(base, extra...)
}

// count runs a query as the database owner of the cluster (outside row-level security) to see what exists.
func count(t *testing.T, pool *pgxpool.Pool, q string) int {
	t.Helper()
	get := func(k, d string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return d
	}
	url := fmt.Sprintf("postgres://%s:%s@%s:%s/%s", get("PGUSER", "postgres"), os.Getenv("PGPASSWORD"), get("PGHOST", "127.0.0.1"), get("PGPORT", "5432"), pool.Config().ConnConfig.Database)
	c, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)
	var n int
	if err := c.QueryRow(ctx, q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSetupCreatesOwnerInvite(t *testing.T) {
	pool := testdb.New(t)
	var out bytes.Buffer
	if err := runSetup(ctx, pool, args(), "https://app.example.com", time.Now(), &out); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, `select count(*) from organization`); n != 1 {
		t.Fatalf("%d organizations", n)
	}
	if n := count(t, pool, `select count(*) from property where time_zone = 'Asia/Ho_Chi_Minh' and currency = 'USD'`); n != 1 {
		t.Fatalf("%d properties", n)
	}
	if n := count(t, pool, `select count(*) from invite where role = 'owner' and property_id is null and email = 'owner@example.com' and invited_by is null`); n != 1 {
		t.Fatalf("%d owner invites", n)
	}
	m := linkRE.FindAllStringSubmatch(out.String(), -1)
	if len(m) != 1 {
		t.Fatalf("the invite link is printed %d times: %q", len(m), out.String())
	}
	// The printed link works: it signs the owner up.
	srv := httptest.NewServer(httpx.NewRouter(httpx.Deps{Pool: pool, Mail: mail.NewFake(), Now: time.Now, BaseURL: "https://app.example.com"}, staff.Mount))
	defer srv.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/api/invites/"+m[0][1]+"/accept", strings.NewReader(`{"name":"Owner","password":"twelve chars ok"}`))
	req.Header.Set("X-CSRF-Token", "t")
	req.AddCookie(&http.Cookie{Name: "csrf", Value: "t"})
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("accepting the printed link: %v %v", res, err)
	}
	if n := count(t, pool, `select count(*) from membership where role = 'owner' and property_id is null`); n != 1 {
		t.Fatalf("%d owner memberships", n)
	}
}

func TestSetupRefusesSecondRunForSameEmail(t *testing.T) {
	pool := testdb.New(t)
	if err := runSetup(ctx, pool, args(), "https://app.example.com", time.Now(), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := runSetup(ctx, pool, args("--org", "Another Hotel"), "https://app.example.com", time.Now(), &out)
	if err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("second run: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("a link was printed on a refused run: %q", out.String())
	}
	if n := count(t, pool, `select count(*) from organization`); n != 1 {
		t.Fatalf("%d organizations after a refused run", n)
	}
}

func TestSetupRefusesBadInputAndCreatesNothing(t *testing.T) {
	pool := testdb.New(t)
	for _, bad := range [][]string{
		{"--org", ""},
		{"--property", ""},
		{"--timezone", "Mars/Olympus"},
		{"--currency", "dollars"},
		{"--owner-email", "not-an-email"},
	} {
		if err := runSetup(ctx, pool, args(bad...), "https://app.example.com", time.Now(), &bytes.Buffer{}); err == nil {
			t.Fatalf("%v was accepted", bad)
		}
	}
	if err := runSetup(ctx, pool, []string{"--bogus"}, "https://app.example.com", time.Now(), &bytes.Buffer{}); err == nil {
		t.Fatal("an unknown flag was accepted")
	}
	if n := count(t, pool, `select count(*) from organization`); n != 0 {
		t.Fatalf("%d organizations created from bad input", n)
	}
}
