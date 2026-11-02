package oauth

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestDiscordAuthURLAlwaysAsksForConsent(t *testing.T) {
	raw, err := NewDiscord("id", "secret", "https://app.test/cb").AuthURL(context.Background(), "st", "", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Has("prompt") {
		t.Fatalf("prompt must not be set: %s", raw)
	}
	if q.Get("state") != "st" || q.Get("code_challenge") != Challenge("verifier") || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization URL: %s", raw)
	}
}

func TestDiscordUsesVersionedAPI(t *testing.T) {
	const base = "https://discord.com/api/v10"
	if defaultDiscord.API != base || !strings.HasPrefix(defaultDiscord.Token, base+"/") {
		t.Fatalf("endpoints: %+v", defaultDiscord)
	}
}
