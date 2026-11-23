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
	"hotelpms/api/internal/alerts"
	"hotelpms/api/internal/app"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/db/testdb"
	"hotelpms/api/internal/guestbooking"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/mail"
	"hotelpms/api/internal/notify"
	"hotelpms/api/internal/password"
	"hotelpms/api/internal/payments"
	"hotelpms/api/internal/session"
	"hotelpms/api/internal/staff"
	"hotelpms/api/internal/tenant"
	"hotelpms/api/internal/translate"
)

const (
	hotelPassword = "twelve chars ok"
	baseURL       = "http://127.0.0.1:5173"
)

var (
	codeRE = regexp.MustCompile(`\b\d{6}\b`)
	linkRE = regexp.MustCompile(baseURL + `(/(?:invite|reset)/[A-Za-z0-9_-]+|/book/[a-z0-9-]+/manage\?token=[A-Za-z0-9._-]+)`)
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
	seedHotel(ctx, pool, org, property)
	if err := alerts.Run(ctx, pool, time.Now()); err != nil {
		log.Fatal(err)
	}
	sender := mail.NewFake()
	hub, err := notify.NewHub(pool)
	if err != nil {
		log.Fatal(err)
	}
	defer hub.Close()
	deps := httpx.Deps{Pool: pool, Mail: sender, Now: time.Now, BaseURL: baseURL}

	go guestbooking.RunSweeper(ctx, pool, 2*time.Second)
	go alerts.RunSweeper(ctx, pool, 2*time.Second) // the browser tests do not wait a minute for an alert
	mux := http.NewServeMux()
	mux.Handle("/", app.NewHandler(deps, hub, nil, payments.NewFake(), uuid.Nil, translate.NewFake()))
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
	// Test helpers: sign a person in without the code step, expire their sessions, send them a notification.
	mux.HandleFunc("/api/test/login", func(w http.ResponseWriter, r *http.Request) {
		err := db.WithoutOrg(r.Context(), pool, func(tx pgx.Tx) error {
			u, err := account.ByEmail(r.Context(), tx, r.URL.Query().Get("email"))
			if err != nil {
				return err
			}
			s, err := session.Create(r.Context(), tx, u.ID, session.StageFull, "e2e", time.Now())
			if err != nil {
				return err
			}
			if err := session.SetOrganization(r.Context(), tx, s.ID, org); err != nil {
				return err
			}
			session.SetCookie(w, s.ID, false)
			return nil
		})
		reply(w, err)
	})
	mux.HandleFunc("/api/test/expire-session", func(w http.ResponseWriter, r *http.Request) {
		err := db.WithoutOrg(r.Context(), pool, func(tx pgx.Tx) error {
			u, err := account.ByEmail(r.Context(), tx, r.URL.Query().Get("email"))
			if err != nil {
				return err
			}
			return session.EndAll(r.Context(), tx, u.ID)
		})
		reply(w, err)
	})
	mux.HandleFunc("/api/test/notify", func(w http.ResponseWriter, r *http.Request) {
		var id uuid.UUID
		err := db.WithoutOrg(r.Context(), pool, func(tx pgx.Tx) error {
			u, err := account.ByEmail(r.Context(), tx, r.URL.Query().Get("email"))
			id = u.ID
			return err
		})
		if err == nil {
			err = db.WithOrg(r.Context(), pool, org, func(tx pgx.Tx) error {
				return notify.Send(r.Context(), tx, notify.Input{OrgID: org, PropertyID: property, ToUser: &id, Level: notify.ActionNeeded,
					Key: "e2e:" + uuid.NewString(), Title: "Refund needs approval", Body: "A refund of $40 is waiting.", Now: time.Now()})
			})
		}
		reply(w, err)
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
		{"tuan@hotel.com", "Tuan Le", "housekeeping"},
		{"rita@hotel.com", "Rita Ho", "revenue"},
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

	// A person whose access was taken away: signed in, but with no membership.
	must(db.WithoutOrg(ctx, pool, func(tx pgx.Tx) error {
		id, err := account.Create(ctx, tx, "nobody@hotel.com", "No Access")
		if err != nil {
			return err
		}
		return account.SetPassword(ctx, tx, id, hash)
	}))

	// One person who works in two organizations, so sign-in has to ask which one.
	var second, secondProperty, multi uuid.UUID
	must(db.WithoutOrg(ctx, pool, func(tx pgx.Tx) (e error) { second, e = tenant.CreateOrganization(ctx, tx, "Second Hotel"); return }))
	must(db.WithOrg(ctx, pool, second, func(tx pgx.Tx) (e error) {
		secondProperty, e = tenant.CreateProperty(ctx, tx, second, tenant.PropertyInput{Name: "Harbour house", Slug: "harbour-house", TimeZone: "UTC", Currency: "USD", DefaultLocale: "en"})
		return
	}))
	must(db.WithoutOrg(ctx, pool, func(tx pgx.Tx) (e error) {
		if multi, e = account.Create(ctx, tx, "multi@hotel.com", "Minh Do"); e != nil {
			return
		}
		return account.SetPassword(ctx, tx, multi, hash)
	}))
	must(db.WithOrg(ctx, pool, org, func(tx pgx.Tx) error { return tenant.AddMembership(ctx, tx, org, multi, &property, "front_desk") }))
	must(db.WithOrg(ctx, pool, second, func(tx pgx.Tx) error { return tenant.AddMembership(ctx, tx, second, multi, &secondProperty, "manager") }))
	return org, property
}

func reply(w http.ResponseWriter, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
