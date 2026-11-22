package signin_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"golang.org/x/oauth2"

	"hotelpms/api/internal/signin"
)

const (
	testIssuer   = "https://accounts.google.test"
	testClientID = "client-id.apps.google.test"
)

type idToken struct {
	Issuer        string `json:"iss"`
	Audience      string `json:"aud"`
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Expiry        int64  `json:"exp"`
	IssuedAt      int64  `json:"iat"`
}

func goodToken() idToken {
	return idToken{Issuer: testIssuer, Audience: testClientID, Subject: "g-sub-1", Email: "mai@hotel.com", EmailVerified: true,
		Expiry: time.Now().Add(time.Hour).Unix(), IssuedAt: time.Now().Unix()}
}

func sign(t *testing.T, key *rsa.PrivateKey, claims idToken) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT"))
	must(t, err)
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	must(t, err)
	return raw
}

// adapter builds the real Google adapter against a local token endpoint that returns idToken.
func adapter(t *testing.T, trust *rsa.PrivateKey, idTokenRaw string, seen *url.Values) *signin.Google {
	t.Helper()
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		must(t, r.ParseForm())
		if seen != nil {
			*seen = r.PostForm
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": idTokenRaw})
	}))
	t.Cleanup(tokenServer.Close)
	return signin.NewGoogle(signin.GoogleConfig{
		ClientID: testClientID, ClientSecret: "secret", RedirectURL: "https://app.example.com/api/sign-in/google/callback",
		Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.test/o/oauth2/v2/auth", TokenURL: tokenServer.URL},
		Issuer:   testIssuer,
		KeySet:   &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&trust.PublicKey}},
	})
}

func newKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	must(t, err)
	return k
}

func TestAdapterAuthURLAsksForOpenIDAndEmailWithPKCE(t *testing.T) {
	g := signin.NewGoogle(signin.GoogleConfig{ClientID: testClientID, ClientSecret: "s", RedirectURL: "https://app.example.com/cb"})
	u, err := url.Parse(g.AuthURL("state-1", "challenge-1"))
	must(t, err)
	q := u.Query()
	want := map[string]string{
		"client_id": testClientID, "redirect_uri": "https://app.example.com/cb", "response_type": "code", "state": "state-1",
		"code_challenge": "challenge-1", "code_challenge_method": "S256",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	scopes := strings.Fields(q.Get("scope"))
	if len(scopes) != 2 || scopes[0] != "openid" || scopes[1] != "email" {
		t.Errorf("scopes %v, want exactly openid and email", scopes)
	}
	if u.Host != "accounts.google.com" {
		t.Errorf("production host is %s", u.Host)
	}
}

func TestAdapterReturnsTheVerifiedIdentityAndSendsThePKCEVerifier(t *testing.T) {
	key := newKey(t)
	var form url.Values
	g := adapter(t, key, sign(t, key, goodToken()), &form)
	id, err := g.Exchange(ctx, "the-code", "the-verifier")
	if err != nil {
		t.Fatal(err)
	}
	if id.Subject != "g-sub-1" || id.Email != "mai@hotel.com" || !id.EmailVerified {
		t.Fatalf("%+v", id)
	}
	if form.Get("code") != "the-code" || form.Get("code_verifier") != "the-verifier" {
		t.Fatalf("the token request did not carry the code and the PKCE verifier: %v", form)
	}
}

func TestAdapterReportsAnUnverifiedEmailWithoutHidingIt(t *testing.T) {
	key := newKey(t)
	claims := goodToken()
	claims.EmailVerified = false
	id, err := adapter(t, key, sign(t, key, claims), nil).Exchange(ctx, "c", "v")
	if err != nil || id.EmailVerified {
		t.Fatalf("%+v %v", id, err)
	}
}

func TestAdapterRejectsABadIDToken(t *testing.T) {
	key := newKey(t)
	for name, mutate := range map[string]func(*idToken){
		"wrong audience": func(c *idToken) { c.Audience = "someone-else" },
		"wrong issuer":   func(c *idToken) { c.Issuer = "https://evil.example" },
		"expired":        func(c *idToken) { c.Expiry = time.Now().Add(-time.Hour).Unix() },
	} {
		claims := goodToken()
		mutate(&claims)
		if _, err := adapter(t, key, sign(t, key, claims), nil).Exchange(ctx, "c", "v"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// Signed by a key Google does not publish.
	if _, err := adapter(t, key, sign(t, newKey(t), goodToken()), nil).Exchange(ctx, "c", "v"); err == nil {
		t.Error("a token signed by an unknown key was accepted")
	}
}

func TestAdapterFailsWhenGoogleReturnsNoIDToken(t *testing.T) {
	key := newKey(t)
	if _, err := adapter(t, key, "", nil).Exchange(ctx, "c", "v"); err == nil {
		t.Fatal("a response without an id_token was accepted")
	}
}
