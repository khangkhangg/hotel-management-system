package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/account"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/staff"
	"hotelpms/api/internal/tenant"
)

var (
	currencyRE = regexp.MustCompile(`^[A-Z]{3}$`)
	slugRE     = regexp.MustCompile(`[^a-z0-9]+`)
)

// runSetup creates the first organization, its first property and an invite for its owner, in one transaction,
// and prints the invite link once. Running it again for the same owner email is refused.
func runSetup(ctx context.Context, pool *pgxpool.Pool, args []string, baseURL string, now time.Time, out io.Writer) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	org := fs.String("org", "", "organization name")
	property := fs.String("property", "", "first property name")
	zone := fs.String("timezone", "UTC", "property time zone, such as Asia/Ho_Chi_Minh")
	currency := fs.String("currency", "USD", "property currency code")
	owner := fs.String("owner-email", "", "email address of the first owner")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w (flags: --org --property --timezone --currency --owner-email)", err)
	}
	email := account.NormalizeEmail(*owner)
	if _, err := mail.ParseAddress(email); err != nil || strings.ContainsAny(email, " <>") {
		return errors.New("--owner-email must be an email address")
	}
	if strings.TrimSpace(*org) == "" || strings.TrimSpace(*property) == "" {
		return errors.New("--org and --property are required")
	}
	if _, err := time.LoadLocation(*zone); err != nil {
		return fmt.Errorf("--timezone %q is not a time zone name", *zone)
	}
	if !currencyRE.MatchString(*currency) {
		return errors.New("--currency must be a three-letter code such as USD")
	}
	slug := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(*property), "-"), "-")
	if slug == "" {
		return errors.New("--property must contain letters or digits")
	}

	var token string
	err := db.WithoutOrg(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `insert into setup_owner (email, created_at) values ($1, $2)`, email, now); err != nil {
			return err
		}
		orgID, err := tenant.CreateOrganization(ctx, tx, strings.TrimSpace(*org))
		if err != nil {
			return err
		}
		if _, err := tenant.CreateProperty(ctx, tx, orgID, tenant.PropertyInput{
			Name: strings.TrimSpace(*property), Slug: slug, TimeZone: *zone, Currency: *currency, DefaultLocale: "en"}); err != nil {
			return err
		}
		token, err = staff.NewOwnerInvite(ctx, tx, orgID, email, now)
		return err
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "setup_owner_pkey" {
		return fmt.Errorf("setup has already run for %s; use the invite link it printed, or invite the owner from the app", email)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Created %s with its first property.\nGive the owner this link. It works once, for 7 days, and is not shown again:\n\n  %s/invite/%s\n", *org, strings.TrimRight(baseURL, "/"), token)
	return err
}
