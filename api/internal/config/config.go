// Package config reads the server's settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL        string
	BaseURL            string
	MigrateURL         string         // an owner-role connection, used only to run migrations
	TrustedProxies     []netip.Prefix // proxies whose X-Forwarded-For is believed (TRUSTED_PROXIES, comma separated)
	SMTPFrom           string
	SMTPURL            string
	GoogleClientID     string
	GoogleClientSecret string
	CookieSecure       bool
}

// Load reads the environment. Only DATABASE_URL is required. Cookies are Secure unless COOKIE_SECURE says otherwise.
func Load() (Config, error) {
	c := Config{
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		BaseURL:            os.Getenv("BASE_URL"),
		MigrateURL:         os.Getenv("MIGRATE_DATABASE_URL"),
		SMTPFrom:           os.Getenv("SMTP_FROM"),
		SMTPURL:            os.Getenv("SMTP_URL"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		CookieSecure:       true,
	}
	if c.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if c.SMTPFrom == "" {
		c.SMTPFrom = "Hotel PMS <no-reply@localhost>"
	}
	for _, part := range strings.Split(os.Getenv("TRUSTED_PROXIES"), ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		p, err := netip.ParsePrefix(part)
		if err != nil {
			a, aerr := netip.ParseAddr(part)
			if aerr != nil {
				return Config{}, fmt.Errorf("TRUSTED_PROXIES must list addresses or ranges, got %q", part)
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		c.TrustedProxies = append(c.TrustedProxies, p)
	}
	if v := os.Getenv("COOKIE_SECURE"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("COOKIE_SECURE must be true or false, got %q", v)
		}
		c.CookieSecure = b
	}
	return c, nil
}
