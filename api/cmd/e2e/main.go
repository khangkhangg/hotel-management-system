// Command e2e runs the API against a fresh throwaway database with a fake mail sender, for the browser tests.
// It adds /api/test/* routes that read the emails the fake sender "sent". Never deploy it.
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/account"
	"hotelpms/api/internal/app"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/db/testdb"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/mail"
	"hotelpms/api/internal/notify"
	"hotelpms/api/internal/password"
	"hotelpms/api/internal/staff"
	"hotelpms/api/internal/tenant"
)

const (
	hotelPassword = "twelve chars ok"
	baseURL       = "http://127.0.0.1:5173"
)

var (
	codeRE = regexp.MustCompile(`\b\d{6}\b`)
	linkRE = regexp.MustCompile(baseURL + `(/(?:invite|reset)/[A-Za-z0-9_-]+)`)
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	pool, cleanup, err := testdb.Create(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()
	org, property := seed(ctx, pool)
	sender := mail.NewFake()
	hub, err := notify.NewHub(pool)
	if err != nil {
		log.Fatal(err)
	}
	defer hub.Close()
	deps := httpx.Deps{Pool: pool, Mail: sender, Now: time.Now, BaseURL: baseURL}

	mux := http.NewServeMux()
	mux.Handle("/", app.NewHandler(deps, hub, nil))
	mux.HandleFunc("/api/test/last-code", lastMatch(sender, codeRE, 0))
	mux.HandleFunc("/api/test/last-link", lastMatch(sender, linkRE, 1))
	mux.HandleFunc("/api/test/owner-invite", func(w http.ResponseWriter, r *http.Request) {
		var token string
		err := db.WithOrg(r.Context(), pool, org, func(tx pgx.Tx) (e error) {
			token, e = staff.NewOwnerInvite(r.Context(), tx, org, r.URL.Query().Get("email"), time.Now())
			return
		})
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"path": "/invite/" + token, "propertyId": property})
	})
	srv := &http.Server{Addr: "127.0.0.1:8080", Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { <-ctx.Done(); _ = srv.Close() }()
	log.Printf("e2e API on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// lastMatch answers with the first (or group) match of re in the latest email sent to ?email=.
func lastMatch(sender *mail.Fake, re *regexp.Regexp, group int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		to := strings.ToLower(r.URL.Query().Get("email"))
		sent := sender.Sent()
		for i := len(sent) - 1; i >= 0; i-- {
			if strings.ToLower(sent[i].To) != to {
				continue
			}
			if m := re.FindStringSubmatch(sent[i].Text); m != nil {
				_, _ = w.Write([]byte(m[group]))
				return
			}
		}
		http.Error(w, "no matching email", http.StatusNotFound)
	}
}

// seed creates one hotel with a front desk person, a manager and an owner, all with the same password.
func seed(ctx context.Context, pool *pgxpool.Pool) (org, property uuid.UUID) {
	hash, err := password.Hash(hotelPassword)
	if err != nil {
		log.Fatal(err)
	}
	must := func(err error) {
		if err != nil {
			log.Fatal(err)
		}
	}
	must(db.WithoutOrg(ctx, pool, func(tx pgx.Tx) (e error) { org, e = tenant.CreateOrganization(ctx, tx, "Sample Hotel"); return }))
	must(db.WithOrg(ctx, pool, org, func(tx pgx.Tx) (e error) {
		property, e = tenant.CreateProperty(ctx, tx, org, tenant.PropertyInput{Name: "Main hotel", Slug: "main-hotel", TimeZone: "Asia/Ho_Chi_Minh", Currency: "USD", DefaultLocale: "en"})
		return
	}))
	for _, p := range []struct{ email, name, role string }{
		{"mai@hotel.com", "Mai Pham", "front_desk"},
		{"daniel@hotel.com", "Daniel Tran", "manager"},
		{"lan@hotel.com", "Lan Vo", "front_desk"},
		{"owner@hotel.com", "Linh Nguyen", "owner"},
	} {
		var id uuid.UUID
		must(db.WithoutOrg(ctx, pool, func(tx pgx.Tx) (e error) {
			if id, e = account.Create(ctx, tx, p.email, p.name); e != nil {
				return
			}
			return account.SetPassword(ctx, tx, id, hash)
		}))
		var prop *uuid.UUID
		if p.role != "owner" {
			prop = &property
		}
		must(db.WithOrg(ctx, pool, org, func(tx pgx.Tx) error { return tenant.AddMembership(ctx, tx, org, id, prop, p.role) }))
	}
	return org, property
}
