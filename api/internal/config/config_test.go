package config_test

import (
	"testing"

	"hotelpms/api/internal/config"
)

func set(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{"DATABASE_URL", "BASE_URL", "SMTP_URL", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "COOKIE_SECURE"} {
		t.Setenv(k, "")
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestLoadReadsEveryField(t *testing.T) {
	set(t, map[string]string{
		"DATABASE_URL": "postgres://app@localhost/hotel", "BASE_URL": "https://app.example.com",
		"SMTP_URL": "smtp://mail:25", "GOOGLE_CLIENT_ID": "id", "GOOGLE_CLIENT_SECRET": "secret", "COOKIE_SECURE": "false",
	})
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.DatabaseURL != "postgres://app@localhost/hotel" || c.BaseURL != "https://app.example.com" || c.SMTPURL != "smtp://mail:25" ||
		c.GoogleClientID != "id" || c.GoogleClientSecret != "secret" || c.CookieSecure {
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
