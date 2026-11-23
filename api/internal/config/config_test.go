package config_test

import (
	"testing"

	"hotelpms/api/internal/config"
)

func set(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{"DATABASE_URL", "BASE_URL", "SMTP_URL", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "COOKIE_SECURE", "MIGRATE_DATABASE_URL", "SMTP_FROM", "TRUSTED_PROXIES"} {
		t.Setenv(k, "")
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestLoadReadsEveryField(t *testing.T) {
	set(t, map[string]string{
		"DATABASE_URL": "postgres://app@localhost/hotel", "BASE_URL": "https://app.example.com",
		"SMTP_URL": "smtp://mail:25", "GOOGLE_CLIENT_ID": "id", "GOOGLE_CLIENT_SECRET": "secret", "COOKIE_SECURE": "false", "MIGRATE_DATABASE_URL": "postgres://migrator@localhost/hotel", "SMTP_FROM": "Hotel <hi@example.com>",
	})
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.DatabaseURL != "postgres://app@localhost/hotel" || c.BaseURL != "https://app.example.com" || c.SMTPURL != "smtp://mail:25" ||
		c.GoogleClientID != "id" || c.GoogleClientSecret != "secret" || c.CookieSecure || c.MigrateURL != "postgres://migrator@localhost/hotel" || c.SMTPFrom != "Hotel <hi@example.com>" {
		t.Fatalf("%+v", c)
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	set(t, nil)
	if _, err := config.Load(); err == nil {
		t.Fatal("missing DATABASE_URL accepted")
	}
}

func TestCookieSecureDefaultsToTrue(t *testing.T) {
	set(t, map[string]string{"DATABASE_URL": "postgres://x"})
	c, err := config.Load()
	if err != nil || !c.CookieSecure {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestCookieSecureRejectsGarbage(t *testing.T) {
	set(t, map[string]string{"DATABASE_URL": "postgres://x", "COOKIE_SECURE": "maybe"})
	if _, err := config.Load(); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestSMTPFromHasADefault(t *testing.T) {
	set(t, map[string]string{"DATABASE_URL": "postgres://app@localhost/hotel"})
	c, err := config.Load()
	if err != nil || c.SMTPFrom == "" {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestTrustedProxiesAreReadAsAddressRanges(t *testing.T) {
	set(t, map[string]string{"DATABASE_URL": "postgres://app@localhost/hotel", "TRUSTED_PROXIES": "10.0.0.0/8, 192.168.1.5"})
	c, err := config.Load()
	if err != nil || len(c.TrustedProxies) != 2 || c.TrustedProxies[1].String() != "192.168.1.5/32" {
		t.Fatalf("%+v %v", c.TrustedProxies, err)
	}
	set(t, map[string]string{"DATABASE_URL": "postgres://app@localhost/hotel", "TRUSTED_PROXIES": "not-an-address"})
	if _, err := config.Load(); err == nil {
		t.Fatal("a bad TRUSTED_PROXIES was accepted")
	}
}
