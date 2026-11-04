package auth

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

var fast = NewHasher(Params{Memory: 8, Time: 1, Threads: 1}, 2)

func TestPasswordRoundTrip(t *testing.T) {
	h, err := fast.Hash(context.Background(), "correct horse battery")
	if err != nil || !strings.HasPrefix(h, "$argon2id$") {
		t.Fatalf("%q %v", h, err)
	}
	if err := fast.Verify(context.Background(), "correct horse battery", h); err != nil {
		t.Fatal(err)
	}
	if err := fast.Verify(context.Background(), "wrong", h); err != ErrMismatch {
		t.Fatalf("got %v", err)
	}
	h2, _ := fast.Hash(context.Background(), "correct horse battery")
	if h == h2 {
		t.Fatal("salt must differ")
	}
}

func TestVerifyRejectsHostileHashes(t *testing.T) {
	for _, bad := range []string{
		"", "plain", "$argon2id$v=19$m=999999999,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=8,t=999,p=1$c2FsdA$aGFzaA", "$argon2i$v=19$m=8,t=1,p=1$c2FsdA$aGFzaA",
	} {
		if err := fast.Verify(context.Background(), "x", bad); err == nil || err == ErrMismatch {
			t.Errorf("%q: expected a format error, got %v", bad, err)
		}
	}
}

func TestPasswordPolicy(t *testing.T) {
	if ValidatePassword("short", "a@b.c", "user") == nil {
		t.Error("short accepted")
	}
	if ValidatePassword("A@B.C-longer", "a@b.c-longer", "u") == nil {
		t.Error("password equal to email accepted")
	}
	if err := ValidatePassword("long enough password", "a@b.c", "user"); err != nil {
		t.Error(err)
	}
}

func TestAPITokenHashing(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	tok, prefix, hash := NewAPIToken(key)
	if !LooksLikeAPIToken(tok) || !strings.HasPrefix(tok, prefix) || len(prefix) >= len(tok) {
		t.Fatalf("bad token %q %q", tok, prefix)
	}
	if string(HashAPIToken(key, tok)) != string(hash) {
		t.Fatal("hash not reproducible")
	}
	if string(HashAPIToken([]byte(strings.Repeat("x", 32)), tok)) == string(hash) {
		t.Fatal("hash must depend on the key")
	}
	other, _, _ := NewAPIToken(key)
	if other == tok {
		t.Fatal("tokens must be unique")
	}
}

func TestAdminTokens(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	tok, prefix, hash := NewAdminToken(key)
	if !LooksLikeAdminToken(tok) || !strings.HasPrefix(prefix, AdminTokenPrefix) || string(HashAPIToken(key, tok)) != string(hash) {
		t.Fatalf("bad admin token %q %q", tok, prefix)
	}
	for range 1000 {
		if tok, _, _ := NewAPIToken(key); LooksLikeAdminToken(tok) {
			t.Fatalf("API token %q looks like an admin token", tok)
		}
	}
	if LooksLikeAdminToken("evl_adm_") || LooksLikeAdminToken("evl_abc") {
		t.Fatal("malformed tokens must not match")
	}
}

func TestSessionSecret(t *testing.T) {
	s, h := NewSessionSecret()
	if string(HashSession(s)) != string(h) || len(s) < 40 {
		t.Fatal("bad session secret")
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(3, time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if !l.Allow("k") {
			t.Fatal("blocked early")
		}
	}
	if l.Allow("k") || !l.Allow("other") {
		t.Fatal("limit wrong")
	}
	now = now.Add(2 * time.Minute)
	if !l.Allow("k") {
		t.Fatal("window did not reset")
	}
}

func TestSystemRoles(t *testing.T) {
	if !NewPermSet(SystemRoles["owner"]).Has(WorkspaceManage) || NewPermSet(SystemRoles["admin"]).Has(WorkspaceManage) {
		t.Fatal("only the owner may manage the workspace")
	}
	if NewPermSet(SystemRoles["viewer"]).Has(LogsIngest) || NewPermSet(SystemRoles["analyst"]).Has(TokensManage) {
		t.Fatal("role too permissive")
	}
}

func TestHasherQueue(t *testing.T) {
	h := NewHasherWithQueue(Params{Memory: 8, Time: 1, Threads: 1}, 1, 1)
	ctx := context.Background()
	release, err := h.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// A waiter occupies the only queue place; the next request is refused at once.
	waiting, cancel := context.WithCancel(ctx)
	defer cancel()
	got := make(chan error, 1)
	go func() {
		rel, err := h.Acquire(waiting)
		if err == nil {
			rel()
		}
		got <- err
	}()
	for len(h.admit) != 2 {
		time.Sleep(time.Millisecond)
	}
	if _, err := h.Hash(ctx, "some password"); err != ErrBusy {
		t.Fatalf("full queue: %v", err)
	}
	// The waiter gives up when its context ends, and frees its place.
	cancel()
	if err := <-got; err != ErrBusy {
		t.Fatalf("cancelled waiter: %v", err)
	}
	short, stop := context.WithTimeout(ctx, 5*time.Millisecond)
	defer stop()
	if err := h.Burn(short, "some password"); err != ErrBusy {
		t.Fatalf("timed out waiter: %v", err)
	}
	// An ended context is refused even with a free slot.
	release()
	if err := h.Burn(short, "some password"); err != ErrBusy {
		t.Fatalf("ended context: %v", err)
	}
	// Releasing makes the slot usable, and no place leaked.
	if _, err := h.Hash(ctx, "some password"); err != nil {
		t.Fatal(err)
	}
	if len(h.admit) != 0 || len(h.sem) != 0 {
		t.Fatalf("leaked: admit=%d sem=%d", len(h.admit), len(h.sem))
	}
}

func TestLimiterBoundsMemory(t *testing.T) {
	l := NewLimiter(2, time.Minute)
	l.maxEntries = 100
	for i := 0; i < 1000; i++ {
		l.Allow(fmt.Sprintf("key-%d", i))
		if len(l.hits) > l.maxEntries {
			t.Fatalf("%d entries", len(l.hits))
		}
	}
	// Expired keys go first: a live counter survives compaction when expired ones suffice.
	now := time.Now()
	l = NewLimiter(1, time.Minute)
	l.maxEntries = 10
	l.now = func() time.Time { return now }
	for i := 0; i < 9; i++ {
		l.Allow(fmt.Sprintf("old-%d", i))
	}
	now = now.Add(2 * time.Minute)
	l.Allow("live")
	l.Allow("new")
	if l.Allow("live") {
		t.Fatal("the live counter was dropped while expired keys remained")
	}
}

func TestLimiterHashesLongKeys(t *testing.T) {
	l := NewLimiter(1, time.Minute)
	long := strings.Repeat("x", 1<<20)
	l.Allow(long)
	for k := range l.hits {
		if len(k) > maxLimiterKey {
			t.Fatalf("key of %d bytes kept", len(k))
		}
	}
	if l.Allow(long) {
		t.Fatal("long keys are not counted")
	}
	if !l.Allow(long + "y") {
		t.Fatal("different long keys collide")
	}
	l.Reset(long)
	if !l.Allow(long) {
		t.Fatal("Reset does not reach long keys")
	}
}

func TestRateKeyIP(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.9":          "203.0.113.9",
		"::ffff:203.0.113.9":   "203.0.113.9",
		"2001:db8:1:2:3:4:5:6": "2001:db8:1:2::/64",
		"2001:db8:1:2::1%eth0": "2001:db8:1:2::/64",
		"not-an-ip":            "not-an-ip",
	} {
		if got := RateKeyIP(in); got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
}
