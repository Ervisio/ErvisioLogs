package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"sync"
	"time"
)

const (
	// maxLimiterKey is the longest key kept verbatim. Longer ones are stored as a hash, so a client
	// sending huge identifiers cannot make the limiter hold them in memory.
	maxLimiterKey = 128
	// defaultLimiterEntries caps the keys one limiter tracks.
	defaultLimiterEntries = 100_000
)

// Limiter is a fixed-window in-memory rate limiter. It protects login and registration against
// brute force on a single node; a multi-node deployment needs a shared limiter (P7).
//
// Memory is bounded: past the entry cap it drops expired keys and then arbitrary ones. A flood of
// distinct keys can therefore forget a counter early, which is preferred to refusing every new client.
type Limiter struct {
	mu         sync.Mutex
	max        int
	window     time.Duration
	now        func() time.Time
	hits       map[string]*bucket
	maxEntries int
}

type bucket struct {
	count int
	reset time.Time
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, now: time.Now, hits: map[string]*bucket{}, maxEntries: defaultLimiterEntries}
}

func limiterKey(key string) string {
	if len(key) <= maxLimiterKey {
		return key
	}
	sum := sha256.Sum256([]byte(key))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Allow records an attempt and reports whether it is within the limit.
func (l *Limiter) Allow(key string) bool {
	key = limiterKey(key)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b := l.hits[key]
	if b == nil && len(l.hits) >= l.maxEntries {
		l.compact(now)
	}
	if b == nil || now.After(b.reset) {
		b = &bucket{reset: now.Add(l.window)}
		l.hits[key] = b
	}
	b.count++
	return b.count <= l.max
}

// compact frees at least a tenth of the capacity, so the full scan is paid once per many inserts.
func (l *Limiter) compact(now time.Time) {
	for k, b := range l.hits {
		if now.After(b.reset) {
			delete(l.hits, k)
		}
	}
	target := l.maxEntries - l.maxEntries/10 - 1
	for k := range l.hits {
		if len(l.hits) <= target {
			break
		}
		delete(l.hits, k)
	}
}

// Reset clears a key (e.g. after a successful login).
func (l *Limiter) Reset(key string) {
	key = limiterKey(key)
	l.mu.Lock()
	delete(l.hits, key)
	l.mu.Unlock()
}

// RateKeyIP returns the rate limit key of a client address. An IPv6 client usually controls a whole
// /64, so all of it counts as one client. Anything that is not an address is returned unchanged.
func RateKeyIP(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap().WithZone("")
	if addr.Is4() {
		return addr.String()
	}
	return netip.PrefixFrom(addr, 64).Masked().String()
}
