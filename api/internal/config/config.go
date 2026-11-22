// Package config reads the server's settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL        string
	BaseURL            string
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
		SMTPURL:            os.Getenv("SMTP_URL"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		CookieSecure:       true,
	}
	if c.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
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
