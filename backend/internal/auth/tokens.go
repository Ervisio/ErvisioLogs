package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// NewSessionSecret returns a cookie value and the SHA-256 stored in the database.
func NewSessionSecret() (secret string, hash []byte) {
	secret = randomString(32)
	return secret, HashSession(secret)
}

func HashSession(secret string) []byte {
	h := sha256.Sum256([]byte(secret))
	return h[:]
}

// NewCSRFToken returns a random per-session CSRF token.
func NewCSRFToken() string { return randomString(24) }

// NewRandom returns n random bytes, URL-safe encoded (OAuth state, nonce, PKCE verifier, setup code).
func NewRandom(n int) string { return randomString(n) }

// TokenPrefix marks Ervisio Logs API tokens, which makes leaked ones greppable by secret scanners.
const TokenPrefix = "evl_"

// AdminTokenPrefix marks instance admin tokens (ADR 0019). No API token starts with it.
const AdminTokenPrefix = "evl_adm_"

// NewAPIToken returns the full token (shown once), a display prefix and the keyed hash to store.
func NewAPIToken(key []byte) (token, prefix string, hash []byte) {
	for token = AdminTokenPrefix; strings.HasPrefix(token, AdminTokenPrefix); {
		token = TokenPrefix + randomString(32)
	}
	return token, token[:len(TokenPrefix)+6], HashAPIToken(key, token)
}

// NewAdminToken is NewAPIToken for instance admin tokens.
func NewAdminToken(key []byte) (token, prefix string, hash []byte) {
	token = AdminTokenPrefix + randomString(32)
	return token, token[:len(AdminTokenPrefix)+6], HashAPIToken(key, token)
}

// HashAPIToken is HMAC-SHA256(key, token). The key never leaves the instance, so a database
// dump alone cannot be used to validate guesses.
func HashAPIToken(key []byte, token string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(token))
	return m.Sum(nil)
}

// LooksLikeAPIToken is a cheap pre-check before hitting the database.
func LooksLikeAPIToken(s string) bool {
	return strings.HasPrefix(s, TokenPrefix) && len(s) > len(TokenPrefix)+20 && len(s) < 200
}

func LooksLikeAdminToken(s string) bool {
	return strings.HasPrefix(s, AdminTokenPrefix) && LooksLikeAPIToken(s)
}
