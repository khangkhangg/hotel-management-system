// Command server runs the Hotel PMS API. "server setup" creates the first organization and owner invite instead.
package main

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/activity"
	"hotelpms/api/internal/alerts"
	"hotelpms/api/internal/app"
	"hotelpms/api/internal/channels"
	"hotelpms/api/internal/config"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/guestbooking"
	"hotelpms/api/internal/guestcare"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/mail"
	"hotelpms/api/internal/notify"
	"hotelpms/api/internal/password"
	"hotelpms/api/internal/payments"
	"hotelpms/api/internal/session"
)

// logMailer prints emails instead of sending them, for a machine with no SMTP server set up.
type logMailer struct{}

func (logMailer) Send(_ context.Context, m mail.Message) error {
	log.Printf("email (SMTP_URL is not set, so it was not sent) to %s: %s\n%s", m.To, m.Subject, m.Text)
	return nil
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.MigrateURL != "" {
		if err := db.Migrate(ctx, cfg.MigrateURL); err != nil {
			log.Fatalf("migrate: %v", err)
		}
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	if len(os.Args) > 1 && os.Args[1] == "setup" {
		if err := runSetup(ctx, pool, os.Args[2:], cfg.BaseURL, time.Now(), os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}

	var sender mail.Sender = logMailer{}
	if cfg.SMTPURL != "" {
		if sender, err = mail.NewSMTP(cfg.SMTPURL, cfg.SMTPFrom); err != nil {
			log.Fatal(err)
		}
	}
	hub, err := notify.NewHub(pool)
	if err != nil {
		log.Fatal(err)
	}
	defer hub.Close()

	var cardOrg uuid.UUID
	var card payments.Provider // nil leaves card payment off until the owner's Stripe keys are configured
	if cfg.StripeSecretKey != "" && cfg.StripeWebhookKey != "" {
		card = payments.NewStripe(cfg.StripeSecretKey, cfg.StripeWebhookKey, "")
		if cardOrg, err = uuid.Parse(cfg.StripeOrgID); err != nil {
			log.Fatal("STRIPE_ORGANIZATION_ID must be the id of the organization that owns the Stripe account")
		}
	}
	var google httpx.Mount
	if cfg.GoogleClientID != "" {
		google = app.GoogleFor(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.BaseURL)
	}
	deps := httpx.Deps{Pool: pool, Mail: sender, Now: time.Now, CookieSecure: cfg.CookieSecure, BaseURL: cfg.BaseURL,
		TrustedProxies: cfg.TrustedProxies, Passwords: password.NewRangeChecker(&http.Client{Timeout: 5 * time.Second})}

	go notify.RunSweeper(ctx, pool, sender, 30*time.Second)
	go alerts.RunSweeper(ctx, pool, time.Minute)
	go guestbooking.RunSweeper(ctx, pool, time.Minute)
	go guestcare.RunSweeper(ctx, pool, sender, cfg.BaseURL, 15*time.Minute)
	go channels.RunSweeper(ctx, pool, channels.Real(os.Getenv), 15*time.Minute)
	go activity.RunNightly(ctx, pool)
	go purgeSessions(ctx, pool)

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	// No WriteTimeout: the notification stream is a response that stays open.
	srv := &http.Server{Addr: addr, Handler: app.NewHandler(deps, hub, google, card, cardOrg), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Printf("listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// purgeSessions removes expired sessions every hour. Lookup never deletes them, so this is what keeps the table small.
func purgeSessions(ctx context.Context, pool *pgxpool.Pool) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			err := db.WithoutOrg(ctx, pool, func(tx pgx.Tx) error {
				_, err := session.Purge(ctx, tx, time.Now())
				return err
			})
			if err != nil {
				log.Printf("purge sessions: %v", err)
			}
		}
	}
}
