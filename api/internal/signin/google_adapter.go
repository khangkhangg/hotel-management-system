package signin

import (
	"context"
	"errors"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// GoogleIdentity is what we trust from Google after the sign-in: who the account is and whether
// Google has verified the email address.
type GoogleIdentity struct {
	Subject       string
	Email         string
	EmailVerified bool
}

// IDTokenVerifier is the part of Google the sign-in handlers need. Tests replace it with a fake.
type IDTokenVerifier interface {
	// AuthURL is the Google page to send the person to. challenge is the PKCE S256 challenge.
	AuthURL(state, challenge string) string
	// Exchange trades the code for an ID token, checks its signature, issuer, audience and expiry,
	// and returns the identity inside it.
	Exchange(ctx context.Context, code, pkceVerifier string) (GoogleIdentity, error)
}

// GoogleConfig holds the app's Google credentials. Endpoint, Issuer and KeySet are for tests;
// leave them empty to use Google's production values.
type GoogleConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string

	Endpoint oauth2.Endpoint
	Issuer   string
	KeySet   oidc.KeySet
}

type Google struct {
	cfg      oauth2.Config
	verifier *oidc.IDTokenVerifier
}

func NewGoogle(c GoogleConfig) *Google {
	if c.Endpoint.AuthURL == "" {
		c.Endpoint = google.Endpoint
	}
	if c.Issuer == "" {
		c.Issuer = "https://accounts.google.com"
	}
	if c.KeySet == nil {
		c.KeySet = oidc.NewRemoteKeySet(context.Background(), "https://www.googleapis.com/oauth2/v3/certs")
	}
	return &Google{
		cfg: oauth2.Config{
			ClientID: c.ClientID, ClientSecret: c.ClientSecret, RedirectURL: c.RedirectURL,
			Endpoint: c.Endpoint, Scopes: []string{oidc.ScopeOpenID, "email"},
		},
		verifier: oidc.NewVerifier(c.Issuer, c.KeySet, &oidc.Config{ClientID: c.ClientID}),
	}
}

func (g *Google) AuthURL(state, challenge string) string {
	return g.cfg.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"))
}

func (g *Google) Exchange(ctx context.Context, code, pkceVerifier string) (GoogleIdentity, error) {
	tok, err := g.cfg.Exchange(ctx, code, oauth2.VerifierOption(pkceVerifier))
	if err != nil {
		return GoogleIdentity{}, err
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return GoogleIdentity{}, errors.New("google: the response has no id_token")
	}
	idt, err := g.verifier.Verify(ctx, raw)
	if err != nil {
		return GoogleIdentity{}, err
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := idt.Claims(&claims); err != nil {
		return GoogleIdentity{}, err
	}
	return GoogleIdentity{Subject: idt.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified}, nil
}
