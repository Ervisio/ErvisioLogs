package api

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLimiterRate(t *testing.T) {
	var l limiter
	tok, ws, pr := uuid.New(), uuid.New(), uuid.New()
	now := time.Unix(1_700_000_000, 0)
	lim := ingestLimits{tokenRate: 10, burstSeconds: 2} // capacity 20

	if err := l.admit(now, tok, ws, pr, lim, 20, 100); err != nil {
		t.Fatalf("a full bucket must admit its burst: %v", err)
	}
	err := l.admit(now, tok, ws, pr, lim, 5, 100)
	if err == nil {
		t.Fatal("an empty bucket must refuse")
	}
	if got := err.failure(); got.status != 429 || got.retryAfter != "1" {
		t.Fatalf("got %d retry-after %q", got.status, got.retryAfter)
	}
	if err := l.admit(now.Add(time.Second), tok, ws, pr, lim, 5, 100); err != nil {
		t.Fatalf("10 tokens refill after a second: %v", err)
	}
	// Another token has its own bucket.
	if err := l.admit(now, uuid.New(), ws, pr, lim, 20, 100); err != nil {
		t.Fatalf("buckets are per token: %v", err)
	}
}

func TestLimiterBatchLargerThanBurst(t *testing.T) {
	var l limiter
	tok, ws, pr := uuid.New(), uuid.New(), uuid.New()
	now := time.Unix(1_700_000_000, 0)
	lim := ingestLimits{projectRate: 10, burstSeconds: 1} // capacity 10

	if err := l.admit(now, tok, ws, pr, lim, 50, 1); err != nil {
		t.Fatalf("a full bucket admits a batch above the burst: %v", err)
	}
	err := l.admit(now.Add(time.Second), tok, ws, pr, lim, 50, 1)
	if err == nil {
		t.Fatal("the bucket is in debt")
	}
	if err := l.admit(now.Add(6*time.Second), tok, ws, pr, lim, 50, 1); err != nil {
		t.Fatalf("the debt of 40 clears in 4 s and the bucket refills: %v", err)
	}
}

func TestLimiterRefusalSpendsNothing(t *testing.T) {
	var l limiter
	tok, ws, pr := uuid.New(), uuid.New(), uuid.New()
	now := time.Unix(1_700_000_000, 0)
	lim := ingestLimits{tokenRate: 100, burstSeconds: 1, wsEvents: 10}

	if err := l.admit(now, tok, ws, pr, lim, 11, 1); err == nil {
		t.Fatal("quota must refuse a batch that does not fit")
	}
	if err := l.admit(now, tok, ws, pr, lim, 10, 1); err != nil {
		t.Fatalf("the refused batch must not have spent tokens or quota: %v", err)
	}
}

func TestLimiterQuota(t *testing.T) {
	var l limiter
	tok, ws, pr := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, 11, 4, 23, 0, 0, 0, time.UTC)
	lim := ingestLimits{prEvents: 100, wsBytes: 1000}

	if err := l.admit(now, tok, ws, pr, lim, 60, 600); err != nil {
		t.Fatal(err)
	}
	err := l.admit(now, tok, ws, pr, lim, 50, 100)
	if err == nil || err.retryAfter != time.Hour {
		t.Fatalf("project events quota must refuse until midnight: %+v", err)
	}
	if err := l.admit(now, tok, ws, pr, lim, 10, 500); err == nil {
		t.Fatal("workspace bytes quota must refuse")
	}
	l.release(now, ws, pr, 60, 600)
	if err := l.admit(now, tok, ws, pr, lim, 100, 1000); err != nil {
		t.Fatalf("released quota is available again: %v", err)
	}
	if err := l.admit(now.Add(time.Hour), tok, ws, pr, lim, 100, 1000); err != nil {
		t.Fatalf("the quota resets at UTC midnight: %v", err)
	}
}

func TestLimiterUnlimitedByDefault(t *testing.T) {
	var l limiter
	if err := l.admit(time.Now(), uuid.New(), uuid.New(), uuid.New(), ingestLimits{burstSeconds: 5}, 1_000_000, 1<<40); err != nil {
		t.Fatal(err)
	}
	if len(l.buckets)+len(l.quotas) != 0 {
		t.Fatal("unlimited traffic must not allocate state")
	}
}
