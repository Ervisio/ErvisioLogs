package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPublic(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700::1111": true, "::ffff:8.8.8.8": true,
		"127.0.0.1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false,
		"169.254.169.254": false, "100.64.0.1": false, "0.0.0.0": false, "0.1.2.3": false,
		"224.0.0.1": false, "::1": false, "fe80::1": false, "fc00::1": false,
		"::ffff:127.0.0.1": false, "255.255.255.255": false,
	} {
		if got := Public(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Public(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestValidateURL(t *testing.T) {
	ok := []string{"https://hooks.example.com/x", "https://discord.com/api/webhooks/1/abc"}
	bad := []string{"http://hooks.example.com/x", "ftp://example.com/x", "https://127.0.0.1/x",
		"https://localhost/x", "https://user:pw@example.com/x", "https://[::1]/x", "not a url"}
	for _, u := range ok {
		if err := ValidateURL(u, false); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	for _, u := range bad {
		if err := ValidateURL(u, false); err == nil {
			t.Errorf("%s accepted", u)
		}
	}
	if err := ValidateURL("http://127.0.0.1:9000/hook", true); err != nil {
		t.Errorf("allowPrivate: %v", err)
	}
}

func TestRedactURL(t *testing.T) {
	if got := RedactURL("https://discord.com/api/webhooks/1/secret"); got != "https://discord.com/..." {
		t.Errorf("got %q", got)
	}
	if strings.Contains(RedactURL("https://x.example/?token=abc"), "abc") {
		t.Error("query leaked")
	}
}

func TestSignVerify(t *testing.T) {
	key := DeriveKey([]byte("server-secret-server-secret-server"), uuid.New(), 1)
	body := []byte(`{"a":1}`)
	now := time.Unix(1_800_000_000, 0)
	sig := Sign(key, now.Unix(), body)
	if !Verify(key, "1800000000", sig, body, now, time.Minute) {
		t.Fatal("valid signature rejected")
	}
	if Verify(key, "1800000000", sig, []byte(`{"a":2}`), now, time.Minute) {
		t.Fatal("tampered body accepted")
	}
	if Verify(key, "1800000000", sig, body, now.Add(time.Hour), time.Minute) {
		t.Fatal("stale timestamp accepted")
	}
}

func TestDeriveKeyRotates(t *testing.T) {
	id := uuid.New()
	s := []byte("server-secret-server-secret-server")
	if string(DeriveKey(s, id, 1)) == string(DeriveKey(s, id, 2)) {
		t.Fatal("rotation did not change the key")
	}
}

func TestDiscordBodyBlocksMentions(t *testing.T) {
	b, err := Body(FormatDiscord, Payload{Type: "alert.firing", RuleName: "@everyone"})
	if err != nil || !strings.Contains(string(b), `"allowed_mentions":{"parse":[]}`) {
		t.Fatalf("body %s, err %v", b, err)
	}
}

func TestSendSignedAndBlocked(t *testing.T) {
	key := []byte("k")
	var gotSig, gotTS string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig, gotTS = r.Header.Get("X-Ervisio-Signature"), r.Header.Get("X-Ervisio-Timestamp")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	d := Delivery{URL: srv.URL, Format: FormatErvisio, Key: key, Payload: Payload{Type: "test", EventID: uuid.New()}}

	if err := NewClient(func() bool { return true }).Send(context.Background(), d); err != nil {
		t.Fatalf("send: %v", err)
	}
	if !Verify(key, gotTS, gotSig, gotBody, time.Now(), time.Minute) {
		t.Fatal("receiver could not verify the signature")
	}
	// The test server listens on loopback: the default client must refuse it.
	if err := NewClient(func() bool { return false }).Send(context.Background(), d); err == nil {
		t.Fatal("loopback delivery was not blocked")
	}
}

func TestSendNoRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/", http.StatusFound)
	}))
	defer srv.Close()
	err := NewClient(func() bool { return true }).Send(context.Background(), Delivery{URL: srv.URL, Format: FormatSlack, Payload: Payload{Type: "test"}})
	if err == nil || !strings.Contains(err.Error(), "302") {
		t.Fatalf("redirect: %v", err)
	}
}
