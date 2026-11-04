package api

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
)

// limiter enforces the ingest rate limits and daily quotas (ADR 0020). State is in memory: with
// several instances each one enforces the limits on its own share, and quotas restart from zero
// when the process restarts.
type limiter struct {
	mu      sync.Mutex
	buckets map[uuid.UUID]*bucket
	quotas  map[uuid.UUID]*quota
	swept   time.Time
}

// bucket is a token bucket that may go into debt, so a batch larger than the burst passes once the
// bucket is full instead of never.
type bucket struct {
	tokens float64
	at     time.Time
}

type quota struct {
	day           int64 // days since the Unix epoch, UTC
	events, bytes int64
}

// ingestLimits are the limits that apply to one request, resolved from the settings. 0 is unlimited.
type ingestLimits struct {
	tokenRate, projectRate, workspaceRate int64
	burstSeconds                          int64
	wsEvents, wsBytes                     int64
	prEvents, prBytes                     int64
}

func (a *API) ingestLimits(ws, project uuid.UUID) ingestLimits {
	s := a.Settings
	return ingestLimits{
		tokenRate:     s.ProjectInt(ws, project, settings.RatePerToken),
		projectRate:   s.ProjectInt(ws, project, settings.RatePerProject),
		workspaceRate: s.WorkspaceInt(ws, settings.RatePerWorkspace),
		burstSeconds:  s.Int(settings.RateBurstSeconds),
		wsEvents:      s.WorkspaceInt(ws, settings.WorkspaceQuotaEvents),
		wsBytes:       s.WorkspaceInt(ws, settings.WorkspaceQuotaBytes),
		prEvents:      s.ProjectInt(ws, project, settings.ProjectQuotaEvents),
		prBytes:       s.ProjectInt(ws, project, settings.ProjectQuotaBytes),
	}
}

func (l ingestLimits) unlimited() bool {
	return l.tokenRate == 0 && l.projectRate == 0 && l.workspaceRate == 0 &&
		l.wsEvents == 0 && l.wsBytes == 0 && l.prEvents == 0 && l.prBytes == 0
}

// limitError is a refused request: what ran out and how long to wait.
type limitError struct {
	msg        string
	retryAfter time.Duration
}

func (e *limitError) failure() *ingestFailure {
	secs := max(1, int(math.Ceil(e.retryAfter.Seconds())))
	return &ingestFailure{status: http.StatusTooManyRequests, msg: e.msg, retryAfter: strconv.Itoa(secs)}
}

// admit counts n events of the given encoded size against the limits of the token, its project and
// its workspace. It spends nothing when it refuses. A caller that then fails to store the events
// calls release.
func (l *limiter) admit(now time.Time, tokenID, ws, project uuid.UUID, lim ingestLimits, n int, size int64) *limitError {
	if lim.unlimited() {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)

	var spend []*bucket
	var wait time.Duration
	var msg string
	for _, c := range []struct {
		id    uuid.UUID
		rate  int64
		scope string
	}{{tokenID, lim.tokenRate, "token"}, {project, lim.projectRate, "project"}, {ws, lim.workspaceRate, "workspace"}} {
		if c.rate == 0 {
			continue
		}
		capacity := float64(c.rate * lim.burstSeconds)
		b := l.bucketFor(c.id, now, capacity)
		b.refill(now, float64(c.rate), capacity)
		need := math.Min(float64(n), capacity)
		if b.tokens >= need {
			spend = append(spend, b)
			continue
		}
		wait = max(wait, time.Duration((need-b.tokens)/float64(c.rate)*float64(time.Second)))
		if msg == "" {
			msg = fmt.Sprintf("the %s rate limit of %d events per second is reached", c.scope, c.rate)
		}
	}
	if msg != "" {
		return &limitError{msg, wait}
	}

	day := now.Unix() / 86400
	untilMidnight := time.Unix((day+1)*86400, 0).Sub(now)
	for _, c := range []struct {
		id            uuid.UUID
		events, bytes int64
		scope         string
	}{{ws, lim.wsEvents, lim.wsBytes, "workspace"}, {project, lim.prEvents, lim.prBytes, "project"}} {
		q := l.quotaFor(c.id, day)
		if c.events > 0 && q.events+int64(n) > c.events {
			return &limitError{fmt.Sprintf("the %s quota of %d events per day is used up", c.scope, c.events), untilMidnight}
		}
		if c.bytes > 0 && q.bytes+size > c.bytes {
			return &limitError{fmt.Sprintf("the %s quota of %d bytes per day is used up", c.scope, c.bytes), untilMidnight}
		}
	}

	for _, b := range spend {
		b.tokens -= float64(n)
	}
	for _, id := range []uuid.UUID{ws, project} {
		q := l.quotaFor(id, day)
		q.events += int64(n)
		q.bytes += size
	}
	return nil
}

// release gives back the quota of events that were admitted but not stored. Rate tokens stay spent.
func (l *limiter) release(now time.Time, ws, project uuid.UUID, n int, size int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, id := range []uuid.UUID{ws, project} {
		if q := l.quotas[id]; q != nil && q.day == now.Unix()/86400 {
			q.events, q.bytes = max(0, q.events-int64(n)), max(0, q.bytes-size)
		}
	}
}

func (l *limiter) bucketFor(id uuid.UUID, now time.Time, capacity float64) *bucket {
	if l.buckets == nil {
		l.buckets = map[uuid.UUID]*bucket{}
	}
	b := l.buckets[id]
	if b == nil {
		b = &bucket{tokens: capacity, at: now}
		l.buckets[id] = b
	}
	return b
}

func (b *bucket) refill(now time.Time, rate, capacity float64) {
	if dt := now.Sub(b.at).Seconds(); dt > 0 {
		b.tokens = math.Min(capacity, b.tokens+dt*rate)
		b.at = now
	}
}

func (l *limiter) quotaFor(id uuid.UUID, day int64) *quota {
	if l.quotas == nil {
		l.quotas = map[uuid.UUID]*quota{}
	}
	q := l.quotas[id]
	if q == nil || q.day != day {
		q = &quota{day: day}
		l.quotas[id] = q
	}
	return q
}

// sweep drops buckets idle for ten minutes (they would be full again) and quotas of past days.
func (l *limiter) sweep(now time.Time) {
	if now.Sub(l.swept) < time.Minute {
		return
	}
	l.swept = now
	for id, b := range l.buckets {
		if now.Sub(b.at) > 10*time.Minute {
			delete(l.buckets, id)
		}
	}
	for id, q := range l.quotas {
		if q.day != now.Unix()/86400 {
			delete(l.quotas, id)
		}
	}
}
