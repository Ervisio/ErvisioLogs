// Package oauth implements the external sign-in providers: Google and AWS Cognito (OpenID
// Connect, ID token verified for signature, issuer, audience, expiry and nonce) and Discord
// (plain OAuth 2.0). All flows use the authorization code grant with PKCE (S256).
package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Profile is the verified identity returned by a provider.
type Profile struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

// ErrUnverifiedEmail means the provider did not vouch for the email address. Such identities
// are refused: linking or creating accounts by an unverified email would allow takeover.
var ErrUnverifiedEmail = errors.New("the provider has not verified this email address")

// Provider is one configured sign-in method.
type Provider interface {
	Name() string
	AuthURL(ctx context.Context, state, nonce, verifier string) (string, error)
	Exchange(ctx context.Context, code, verifier, nonce string) (Profile, error)
}

// Challenge derives the S256 PKCE code challenge from a verifier.
func Challenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// ---------- OpenID Connect (Google, Cognito) ----------

type oidcProvider struct {
	name     string
	issuer   string
	conf     oauth2.Config
	mu       sync.Mutex
	provider *oidc.Provider
}

// NewGoogle returns the Google OpenID Connect provider.
func NewGoogle(clientID, secret, redirect string) Provider {
	return NewOIDC("google", "https://accounts.google.com", clientID, secret, redirect)
}

// NewCognito returns an AWS Cognito user pool provider. issuer looks like
// https://cognito-idp.<region>.amazonaws.com/<pool id>. The pool needs a hosted UI domain, an app
// client with a secret, the authorization code grant and the openid, email and profile scopes.
func NewCognito(issuer, clientID, secret, redirect string) Provider {
	return NewOIDC("cognito", issuer, clientID, secret, redirect)
}

// NewOIDC returns a generic OpenID Connect provider (also used by tests against a fake issuer).
func NewOIDC(name, issuer, clientID, secret, redirect string) Provider {
	return &oidcProvider{name: name, issuer: issuer, conf: oauth2.Config{
		ClientID: clientID, ClientSecret: secret, RedirectURL: redirect,
		Scopes: []string{oidc.ScopeOpenID, "email", "profile"},
	}}
}

func (p *oidcProvider) Name() string { return p.name }

// discover performs OIDC discovery lazily, so a provider outage at start-up does not stop the
// server and a later request retries.
func (p *oidcProvider) discover(ctx context.Context) (*oidc.Provider, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.provider != nil {
		return p.provider, nil
	}
	prov, err := oidc.NewProvider(oidc.ClientContext(ctx, httpClient), p.issuer)
	if err != nil {
		return nil, fmt.Errorf("%s discovery: %w", p.name, err)
	}
	p.provider = prov
	p.conf.Endpoint = prov.Endpoint()
	return prov, nil
}

func (p *oidcProvider) AuthURL(ctx context.Context, state, nonce, verifier string) (string, error) {
	if _, err := p.discover(ctx); err != nil {
		return "", err
	}
	return p.conf.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.SetAuthURLParam("code_challenge", Challenge(verifier)),
		oauth2.SetAuthURLParam("code_challenge_method", "S256")), nil
}

func (p *oidcProvider) Exchange(ctx context.Context, code, verifier, nonce string) (Profile, error) {
	prov, err := p.discover(ctx)
	if err != nil {
		return Profile{}, err
	}
	ctx = oidc.ClientContext(ctx, httpClient)
	tok, err := p.conf.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		return Profile{}, fmt.Errorf("%s token exchange: %w", p.name, err)
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return Profile{}, errors.New("no id_token in token response")
	}
	// Verify checks signature (provider JWKS), issuer, audience (client id) and expiry.
	idt, err := prov.Verifier(&oidc.Config{ClientID: p.conf.ClientID}).Verify(ctx, raw)
	if err != nil {
		return Profile{}, fmt.Errorf("%s id_token: %w", p.name, err)
	}
	if idt.Nonce != nonce {
		return Profile{}, errors.New("id_token nonce mismatch")
	}
	var claims struct {
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"` // bool, or "true" as some Cognito setups emit
		Name          string `json:"name"`
	}
	if err := idt.Claims(&claims); err != nil {
		return Profile{}, err
	}
	if claims.Subject == "" || claims.Email == "" {
		return Profile{}, errors.New("id_token lacks sub or email (is the email scope granted?)")
	}
	pr := Profile{Subject: claims.Subject, Email: strings.ToLower(claims.Email), Name: claims.Name}
	switch v := claims.EmailVerified.(type) {
	case bool:
		pr.EmailVerified = v
	case string:
		pr.EmailVerified = v == "true"
	}
	if !pr.EmailVerified {
		return Profile{}, ErrUnverifiedEmail
	}
	return pr, nil
}

// ---------- Discord (OAuth 2.0) ----------

// DiscordEndpoints can be overridden in tests.
type DiscordEndpoints struct{ Auth, Token, API string }

var defaultDiscord = DiscordEndpoints{
	Auth:  "https://discord.com/oauth2/authorize",
	Token: "https://discord.com/api/v10/oauth2/token",
	API:   "https://discord.com/api/v10",
}

type discord struct {
	conf oauth2.Config
	api  string
}

func NewDiscord(clientID, secret, redirect string) Provider {
	return NewDiscordWith(clientID, secret, redirect, defaultDiscord)
}

func NewDiscordWith(clientID, secret, redirect string, e DiscordEndpoints) Provider {
	return &discord{api: e.API, conf: oauth2.Config{
		ClientID: clientID, ClientSecret: secret, RedirectURL: redirect,
		Endpoint: oauth2.Endpoint{AuthURL: e.Auth, TokenURL: e.Token, AuthStyle: oauth2.AuthStyleInParams},
		Scopes:   []string{"identify", "email"},
	}}
}

func (d *discord) Name() string { return "discord" }

func (d *discord) AuthURL(_ context.Context, state, _ string, verifier string) (string, error) {
	return d.conf.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", Challenge(verifier)),
		oauth2.SetAuthURLParam("code_challenge_method", "S256")), nil
}

func (d *discord) Exchange(ctx context.Context, code, verifier, _ string) (Profile, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)
	tok, err := d.conf.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		return Profile{}, fmt.Errorf("discord token exchange: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(d.api, "/")+"/users/@me", nil)
	if err != nil {
		return Profile{}, err
	}
	tok.SetAuthHeader(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		return Profile{}, fmt.Errorf("discord user: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Profile{}, fmt.Errorf("discord user: status %d", resp.StatusCode)
	}
	var u struct {
		ID         string `json:"id"`
		Username   string `json:"username"`
		GlobalName string `json:"global_name"`
		Email      string `json:"email"`
		Verified   bool   `json:"verified"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&u); err != nil {
		return Profile{}, err
	}
	if u.ID == "" || u.Email == "" {
		return Profile{}, errors.New("discord profile lacks id or email (is the email scope granted?)")
	}
	if !u.Verified {
		return Profile{}, ErrUnverifiedEmail
	}
	name := u.GlobalName
	if name == "" {
		name = u.Username
	}
	return Profile{Subject: u.ID, Email: strings.ToLower(u.Email), EmailVerified: true, Name: name}, nil
}

// RedirectURL builds the callback URL registered with the provider.
func RedirectURL(publicURL, provider string) string {
	return strings.TrimRight(publicURL, "/") + "/api/v1/auth/oauth/" + url.PathEscape(provider) + "/callback"
}
