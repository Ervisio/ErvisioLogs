package api

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

const (
	healthCheckTimeout = 3 * time.Second
	// The oldest-unarchived query groups the whole hot table, so a polling dashboard reuses its answer.
	unarchivedTTL = 30 * time.Second
)

type healthCheck struct {
	OK    bool    `json:"ok"`
	Error *string `json:"error"`
}

type healthWAL struct {
	Bytes          int64 `json:"bytes"`
	MaxBytes       int64 `json:"max_bytes"`
	Segments       int   `json:"segments"`
	PendingRecords int64 `json:"pending_records"`
	LagSeconds     int64 `json:"lag_seconds"`
}

type healthSegments struct {
	Count int64 `json:"count"`
	Bytes int64 `json:"bytes"`
}

type healthProvider struct {
	Name          string     `json:"name"`
	Type          string     `json:"type"`
	Status        string     `json:"status"`
	LastError     *string    `json:"last_error"`
	LastCheckedAt *time.Time `json:"last_checked_at"`
}

type healthArchive struct {
	Enabled  bool            `json:"enabled"`
	Provider *healthProvider `json:"provider"`
}

type healthUpload struct {
	Waiting                 healthSegments `json:"waiting"`
	Failed                  healthSegments `json:"failed"`
	LastVerifiedAt          *time.Time     `json:"last_verified_at"`
	OldestUnarchivedEventAt *time.Time     `json:"oldest_unarchived_event_at"`
}

// unarchivedCache holds the last OldestUnarchived answer.
type unarchivedCache struct {
	mu  sync.Mutex
	at  time.Time
	val *time.Time
}

func (c *unarchivedCache) get(ctx context.Context, load func(context.Context) (*time.Time, error)) (*time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < unarchivedTTL {
		return c.val, nil
	}
	v, err := load(ctx)
	if err != nil {
		return nil, err
	}
	c.at, c.val = time.Now(), v
	return v, nil
}

func checkOf(err error) healthCheck {
	if err == nil {
		return healthCheck{OK: true}
	}
	msg := err.Error()
	return healthCheck{Error: &msg}
}

func probe(ctx context.Context, ping func(context.Context) error) healthCheck {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()
	return checkOf(ping(ctx))
}

// adminHealth reports the state of the ingest and archive pipeline from state the process already
// keeps; the only queries are small ones against PostgreSQL and one cached ClickHouse scan.
func (a *API) adminHealth(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminAccess(w, r, auth.AdminRead); !ok {
		return
	}
	ctx := r.Context()
	out := struct {
		Status     string        `json:"status"`
		WAL        *healthWAL    `json:"wal"`
		Upload     healthUpload  `json:"upload"`
		Archive    healthArchive `json:"archive"`
		ClickHouse healthCheck   `json:"clickhouse"`
		Postgres   healthCheck   `json:"postgres"`
	}{Status: "ok"}

	out.Postgres = probe(ctx, a.Store.Ping)
	out.ClickHouse = checkOf(errors.New("not configured"))
	if l := a.Logs; l != nil && l.WAL != nil {
		s := l.WAL.Stats()
		out.WAL = &healthWAL{Bytes: s.Bytes, MaxBytes: s.MaxBytes, Segments: s.Segments, PendingRecords: int64(s.NextSeq - s.Checkpoint)}
		if l.Writer != nil {
			out.WAL.LagSeconds = l.Writer.Stats.LagSeconds.Load()
		}
	}
	if l := a.Logs; l != nil && l.CH != nil {
		out.ClickHouse = probe(ctx, l.CH.Ping)
	}

	out.Archive.Enabled = a.archiver() != nil
	if out.Postgres.OK {
		if a.archiveHealth(ctx, &out.Upload, &out.Archive, out.ClickHouse.OK) {
			out.Status = "degraded"
		}
	}
	if !out.Postgres.OK || !out.ClickHouse.OK {
		out.Status = "degraded"
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// archiveHealth fills the upload queue and the active destination. It reports whether either is
// unhealthy. A failing source leaves its fields empty and is logged: the rest of the page still helps.
func (a *API) archiveHealth(ctx context.Context, up *healthUpload, ar *healthArchive, chOK bool) (degraded bool) {
	sum, err := a.Store.ArchiveSummary(ctx)
	if err != nil {
		a.Log.Warn("health: archive summary", "error", err)
	}
	for _, st := range []string{store.SegStaging, store.SegUploading, store.SegVerified} {
		up.Waiting.Count += sum[st].Count
		up.Waiting.Bytes += sum[st].Bytes
	}
	up.Failed = healthSegments{sum[store.SegFailed].Count, sum[store.SegFailed].Bytes}
	degraded = up.Failed.Count > 0
	if up.LastVerifiedAt, err = a.Store.LastCatalogedAt(ctx); err != nil {
		a.Log.Warn("health: last cataloged", "error", err)
	}
	switch p, err := a.Store.ActiveProvider(ctx); {
	case err == nil:
		ar.Provider = &healthProvider{p.Name, p.Type, p.Status, p.LastError, p.LastCheckedAt}
		degraded = degraded || p.Status == "error"
	case !errors.Is(err, store.ErrNotFound):
		a.Log.Warn("health: active provider", "error", err)
	}
	if arch := a.archiver(); arch != nil && chOK {
		up.OldestUnarchivedEventAt, err = a.unarchived.get(ctx, func(ctx context.Context) (*time.Time, error) {
			t, ok, err := arch.OldestUnarchived(ctx)
			if err != nil || !ok {
				return nil, err
			}
			return &t, nil
		})
		if err != nil {
			a.Log.Warn("health: oldest unarchived", "error", err)
		}
	}
	return degraded
}
