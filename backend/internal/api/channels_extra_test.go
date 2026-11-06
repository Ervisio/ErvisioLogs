package api

import (
	"encoding/base64"
	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
	"strings"
	"testing"
)

func TestTelegramDestinationIsEncryptedAndValidated(t *testing.T) {
	a := &API{secret: []byte("server-secret")}
	bot := "123456789:abcdefghijklmnopqrstuvwxyz"
	dest, err := a.channelDestination("telegram", "", "", bot, "-123456")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dest, bot) {
		t.Fatal("plaintext bot token persisted")
	}
	_, ciphertext, ok := strings.Cut(strings.TrimPrefix(dest, "telegram:"), ":")
	if !ok {
		t.Fatal(dest)
	}
	sealed, err := base64.RawURLEncoding.DecodeString(ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	got, err := auth.Open(a.secret, sealed, "telegram")
	if err != nil || string(got) != bot {
		t.Fatalf("bad encrypted credentials: %v", err)
	}
	if hint := channelHint(store.NotificationChannel{Type: "telegram", URL: dest}); hint != "telegram:-123456" || strings.Contains(hint, bot) {
		t.Fatal(hint)
	}
	for _, chat := range []string{"../../etc/passwd", "https://attacker.example", "123?token=x", ""} {
		if _, err := a.channelDestination("telegram", "", "", bot, chat); err == nil {
			t.Errorf("accepted %q", chat)
		}
	}
}
