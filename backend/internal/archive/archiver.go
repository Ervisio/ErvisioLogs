package archive

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

// SchemaVersion is the layout of the Parquet files this archiver writes (column set below).
const SchemaVersion = 1

const (
	// defaultCutoffLag keeps the newest rows out of a segment: an insert that started before the
	// cutoff may still become visible after it (ADR 0008).
	defaultCutoffLag = 2 * time.Minute
	maxAttempts      = 8
	dueBatch         = 100
)

// Archiver moves closed stream-days from ClickHouse to the remote provider (ADR 0008). One pass
// runs at a time per instance, under a Postgres advisory lock.
type Archiver struct {
	Store *store.Store
	CH    driver.Conn
	HTTP  *CHHTTP
	Cfg   config.Archive
	// Settings, when set, overrides the timing of Cfg at every pass (ADR 0018).
	Settings *settings.Service
	Log      *slog.Logger
	// CutoffLag overrides defaultCutoffLag (tests).
	CutoffLag time.Duration

	mu        sync.Mutex
	providers map[uuid.UUID]openProvider
	over      atomic.Bool
	// lastOrphans is when this process last reconciled orphans; zero runs it on the first pass.
	lastOrphans time.Time
}

type openProvider struct {
	updatedAt time.Time
	p         Provider
}

// PassReport says what one pass did, for logs and tests.
type PassReport struct {
	Ran       bool
	Resumed   int
	Started   int
	Cataloged int
	Failed    int
	Skipped   string
	Maintain  MaintainReport
	Orphans   *OrphanReport
}

// timing returns Cfg with the runtime settings applied.
func (a *Archiver) timing() config.Archive {
	c := a.Cfg
	if a.Settings != nil {
		c.Interval = a.Settings.Duration(settings.ArchiveInterval)
		c.Grace = a.Settings.Duration(settings.ArchiveGrace)
		c.LateInterval = a.Settings.Duration(settings.ArchiveLateInterval)
	}
	return c
}

// Run calls RunOnce every interval until ctx ends.
func (a *Archiver) Run(ctx context.Context) {
	for {
		rep, err := a.RunOnce(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			a.Log.Error("archive pass failed", "err", err)
		case rep.Orphans != nil && (rep.Orphans.Deleted > 0 || rep.Orphans.Failed > 0):
			a.Log.Info("archive orphan cleanup", "providers", rep.Orphans.Providers, "listed", rep.Orphans.Listed,
				"deleted", rep.Orphans.Deleted, "failed", rep.Orphans.Failed)
			fallthrough
		case rep.Cataloged > 0 || rep.Failed > 0 || rep.Maintain != (MaintainReport{UsageBytes: rep.Maintain.UsageBytes}):
			m := rep.Maintain
			a.Log.Info("archive pass", "cataloged", rep.Cataloged, "failed", rep.Failed, "resumed", rep.Resumed,
				"hot_evicted_days", m.HotEvictedDays, "hot_expired_days", m.HotExpiredDays, "segments_expired", m.SegmentsExpired,
				"segments_deleted", m.SegmentsDeleted, "cache_files_evicted", m.CacheFilesEvicted, "usage_bytes", m.UsageBytes,
				"over_capacity", m.OverCapacity)
		}
		select {
		case <-ctx.Done():
			a.Close()
			return
		case <-time.After(a.timing().Interval):
		}
	}
}

// Close releases the cached provider connections.
func (a *Archiver) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, op := range a.providers {
		op.p.Close()
		delete(a.providers, id)
	}
}

// RunOnce runs a single pass. Ran is false when another process holds the archiver lock.
func (a *Archiver) RunOnce(ctx context.Context) (PassReport, error) {
	var rep PassReport
	ran, err := a.Store.WithArchiverLock(ctx, func(ctx context.Context) error { return a.pass(ctx, &rep) })
	rep.Ran = ran
	return rep, err
}

func (a *Archiver) pass(ctx context.Context, rep *PassReport) error {
	now, err := a.chNow(ctx)
	if err != nil {
		return err
	}
	// Cutoffs come from the ClickHouse clock. If it went back, a new cutoff could fall inside a
	// range that is already cataloged and rows would be archived twice or skipped.
	maxCut, err := a.Store.MaxCutoff(ctx)
	if err != nil {
		return err
	}
	if now.Before(maxCut) {
		return fmt.Errorf("clickhouse clock %s is behind the last archive cutoff %s; archiving paused",
			now.Format(time.RFC3339), maxCut.Format(time.RFC3339))
	}

	due, err := a.Store.SegmentsDue(ctx, []string{store.SegStaging, store.SegUploading, store.SegVerified}, time.Now(), dueBatch)
	if err != nil {
		return err
	}
	for _, seg := range due {
		rep.Resumed++
		a.count(rep, a.drive(ctx, seg))
	}

	rows, err := a.Store.RetentionPolicies(ctx)
	if err != nil {
		return err
	}
	pol := NewPolicies(rows)
	active, err := a.Store.ActiveProvider(ctx)
	if errors.Is(err, store.ErrNotFound) {
		// Without a provider nothing new is archived, but retention and the budget still apply.
		rep.Skipped = "no active provider"
		if err := a.maintain(ctx, now, pol, &rep.Maintain); err != nil {
			return err
		}
		return a.orphans(ctx, rep)
	}
	if err != nil {
		return err
	}
	cands, err := a.candidates(ctx, now, pol)
	if err != nil {
		return err
	}
	lag := a.CutoffLag
	if lag <= 0 {
		lag = defaultCutoffLag
	}
	cutoff := now.Add(-lag)
	for _, c := range cands {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		seg, err := a.Store.StartSegment(ctx, store.NewSegment{
			WorkspaceID: c.ws, ProjectID: c.proj, StreamID: c.stream, Day: c.day,
			ProviderID: active.ID, CutoffTo: cutoff, SchemaVersion: SchemaVersion,
			KeyFor: func(id uuid.UUID, gen int32) string { return ObjectKey(c.ws, c.proj, c.stream, c.day, gen, id) },
		})
		if errors.Is(err, store.ErrSegmentBusy) {
			continue
		}
		if err != nil {
			return err
		}
		rep.Started++
		a.count(rep, a.drive(ctx, seg))
	}
	if err := a.maintain(ctx, now, pol, &rep.Maintain); err != nil {
		return err
	}
	return a.orphans(ctx, rep)
}

// orphans runs ReconcileOrphans at most once per orphanEvery. Object ages come from the
// provider's clock, so the wall clock is the reference here, not ClickHouse.
func (a *Archiver) orphans(ctx context.Context, rep *PassReport) error {
	if !a.lastOrphans.IsZero() && time.Since(a.lastOrphans) < orphanEvery {
		return nil
	}
	o, err := a.ReconcileOrphans(ctx, time.Now())
	if err != nil {
		return err
	}
	a.lastOrphans = time.Now()
	rep.Orphans = &o
	return nil
}

func (a *Archiver) count(rep *PassReport, state string) {
	switch state {
	case store.SegCataloged:
		rep.Cataloged++
	case store.SegFailed:
		rep.Failed++
	}
}

func (a *Archiver) chNow(ctx context.Context) (time.Time, error) {
	var now time.Time
	if err := a.CH.QueryRow(ctx, "SELECT now64(3)").Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("clickhouse clock: %w", err)
	}
	return now.UTC(), nil
}

// ObjectKey is the final key of a segment (ADR 0008).
func ObjectKey(ws, proj, stream uuid.UUID, day time.Time, gen int32, id uuid.UUID) string {
	return fmt.Sprintf("v1/%s/%s/%s/%s/g%d-%s.parquet", ws, proj, stream, day.UTC().Format("2006/01/02"), gen, id)
}

func tmpKey(id uuid.UUID) string { return "tmp/" + id.String() + ".parquet" }

func (a *Archiver) stagingPath(id uuid.UUID) string {
	return filepath.Join(a.Cfg.CacheDir, "staging", id.String()+".parquet")
}

// SegmentPath is where a cataloged segment lives in the local cache.
func (a *Archiver) SegmentPath(id uuid.UUID) string {
	return filepath.Join(a.Cfg.CacheDir, "segments", id.String()+".parquet")
}

type candidate struct {
	ws, proj, stream uuid.UUID
	day              time.Time
}

// candidates lists the stream-days that need a new generation: closed for longer than the grace
// period, not past global retention, and holding rows inserted after the covered range.
func (a *Archiver) candidates(ctx context.Context, now time.Time, pol Policies) ([]candidate, error) {
	timing := a.timing()
	before := now.Add(-timing.Grace).Truncate(24 * time.Hour)
	covs, err := a.Store.Coverages(ctx, before)
	if err != nil {
		return nil, err
	}
	type key struct {
		ws, proj, stream uuid.UUID
		day              string
	}
	cov := make(map[key]store.Coverage, len(covs))
	for _, c := range covs {
		cov[key{c.WorkspaceID, c.ProjectID, c.StreamID, c.Day.UTC().Format(time.DateOnly)}] = c
	}
	rows, err := a.CH.Query(ctx, `SELECT workspace_id, project_id, stream_id, toDate(timestamp) AS d, max(inserted_at)
		FROM events WHERE timestamp < `+ms+` GROUP BY workspace_id, project_id, stream_id, d ORDER BY d, workspace_id, project_id, stream_id`, chTime(before))
	if err != nil {
		return nil, fmt.Errorf("archive candidates: %w", err)
	}
	defer rows.Close()
	var out []candidate
	for rows.Next() {
		var c candidate
		var maxIns time.Time
		if err := rows.Scan(&c.ws, &c.proj, &c.stream, &c.day, &maxIns); err != nil {
			return nil, err
		}
		c.day = utcDay(c.day)
		// The whole day is past global retention: its data goes away, there is nothing to keep.
		if !c.day.Add(24 * time.Hour).After(now.Add(-pol.For(c.ws, c.proj, c.stream).GlobalAge)) {
			continue
		}
		if cv, ok := cov[key{c.ws, c.proj, c.stream, c.day.Format(time.DateOnly)}]; ok {
			if maxIns.Before(cv.CutoffTo) {
				continue // fully covered
			}
			if now.Sub(cv.LastStartedAt) < timing.LateInterval {
				continue // late rows wait for the next late generation
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// drive moves a segment as far along the state machine as it can and returns its final state.
// Errors are recorded on the segment, not returned: one bad segment must not stop the pass.
func (a *Archiver) drive(ctx context.Context, seg store.Segment) string {
	p, err := a.provider(ctx, seg.ProviderID)
	if err == nil {
		err = a.step(ctx, p, &seg)
	}
	if err == nil {
		return seg.State
	}
	if errors.Is(err, store.ErrConflict) || ctx.Err() != nil {
		return seg.State // another worker moved it, or we are shutting down
	}
	return a.fail(ctx, p, seg, err, maxAttempts)
}

// errPermanent marks errors that retrying cannot fix.
type errPermanent struct{ error }

func (a *Archiver) fail(ctx context.Context, p Provider, seg store.Segment, cause error, maxAtt int32) string {
	var perm errPermanent
	if errors.As(cause, &perm) {
		maxAtt = 0
	}
	retry := time.Now().Add(backoff(seg.Attempts))
	state, err := a.Store.FailSegmentAttempt(ctx, seg.ID, cause, retry, maxAtt)
	if err != nil {
		a.Log.Error("record archive failure", "segment", seg.ID, "err", err, "cause", cause)
		return seg.State
	}
	a.Log.Warn("archive segment attempt failed", "segment", seg.ID, "state", state, "attempt", seg.Attempts+1, "err", cause)
	if state == store.SegFailed {
		if p != nil {
			if err := p.Delete(ctx, tmpKey(seg.ID)); err != nil {
				a.Log.Warn("delete temporary object", "segment", seg.ID, "err", err)
			}
		}
		os.Remove(a.stagingPath(seg.ID))
	}
	return state
}

func backoff(attempts int32) time.Duration {
	d := time.Minute << min(attempts, 6)
	return min(d, time.Hour)
}

func (a *Archiver) step(ctx context.Context, p Provider, seg *store.Segment) error {
	if seg.State == store.SegStaging {
		st, err := a.export(ctx, *seg)
		if err != nil {
			return err
		}
		if st.RowCount == 0 {
			// Nothing in range: catalog the generation as empty so its range counts as covered.
			if err := a.Store.AdvanceSegment(ctx, seg.ID, store.SegStaging, store.SegCataloged, &st); err != nil {
				return err
			}
			seg.State = store.SegCataloged
			return nil
		}
		if err := a.Store.AdvanceSegment(ctx, seg.ID, store.SegStaging, store.SegUploading, &st); err != nil {
			return err
		}
		seg.State, seg.Checksum, seg.Bytes, seg.RowCount = store.SegUploading, st.Checksum, &st.Bytes, &st.RowCount
	}
	if seg.State == store.SegUploading {
		if err := a.upload(ctx, p, *seg); err != nil {
			return err
		}
		if err := a.Store.AdvanceSegment(ctx, seg.ID, store.SegUploading, store.SegVerified, nil); err != nil {
			return err
		}
		seg.State = store.SegVerified
	}
	if seg.State == store.SegVerified {
		if seg.Bytes == nil {
			return errPermanent{fmt.Errorf("segment %s is VERIFIED without size", seg.ID)}
		}
		if err := p.Promote(ctx, tmpKey(seg.ID), seg.ObjectKey); err != nil {
			// A retry after a crash between Promote and the catalog update finds tmp gone and
			// the final object in place; the check below decides.
			if verr := Verify(ctx, p, seg.ObjectKey, *seg.Bytes, seg.Checksum); verr != nil {
				return fmt.Errorf("promote: %w", err)
			}
		} else if err := Verify(ctx, p, seg.ObjectKey, *seg.Bytes, seg.Checksum); err != nil {
			return err
		}
		if err := a.Store.AdvanceSegment(ctx, seg.ID, store.SegVerified, store.SegCataloged, nil); err != nil {
			return err
		}
		seg.State = store.SegCataloged
		a.keepInCache(seg.ID)
	}
	return nil
}

// keepInCache moves the staged file into the segment cache: the newest segments are the likeliest
// to be read. Failure only costs a download later.
func (a *Archiver) keepInCache(id uuid.UUID) {
	dst := a.SegmentPath(id)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err == nil {
		if err := os.Rename(a.stagingPath(id), dst); err == nil {
			return
		}
	}
	os.Remove(a.stagingPath(id))
}

func (a *Archiver) upload(ctx context.Context, p Provider, seg store.Segment) error {
	if seg.Bytes == nil || seg.Checksum == nil {
		return errPermanent{fmt.Errorf("segment %s is UPLOADING without stats", seg.ID)}
	}
	f, err := os.Open(a.stagingPath(seg.ID))
	if errors.Is(err, os.ErrNotExist) {
		// The staged export is gone (lost cache volume). Its rows are still in ClickHouse: the
		// next generation picks them up once this one is FAILED.
		return errPermanent{fmt.Errorf("staged export missing: %w", err)}
	}
	if err != nil {
		return err
	}
	defer f.Close()
	if n, sum, err := hashReader(f); err != nil {
		return err
	} else if n != *seg.Bytes || string(sum) != string(seg.Checksum) {
		return errPermanent{errors.New("staged export changed since it was written")}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := p.Put(ctx, tmpKey(seg.ID), f, *seg.Bytes, seg.Checksum); err != nil {
		return err
	}
	return Verify(ctx, p, tmpKey(seg.ID), *seg.Bytes, seg.Checksum)
}

func hashReader(r io.Reader) (int64, []byte, error) {
	h := sha256.New()
	n, err := io.Copy(h, r)
	return n, h.Sum(nil), err
}

// export writes the segment's rows to its staging file and returns the stats to catalog.
func (a *Archiver) export(ctx context.Context, seg store.Segment) (store.SegmentStats, error) {
	dayEnd := seg.Day.Add(24 * time.Hour)
	var (
		n          uint64
		minT, maxT time.Time
	)
	if err := a.CH.QueryRow(ctx, `SELECT count(), min(timestamp), max(timestamp) FROM events
		WHERE workspace_id = toUUID(?) AND project_id = toUUID(?) AND stream_id = toUUID(?)
		  AND timestamp >= `+ms+` AND timestamp < `+ms+` AND inserted_at >= `+ms+` AND inserted_at < `+ms,
		seg.WorkspaceID.String(), seg.ProjectID.String(), seg.StreamID.String(),
		chTime(seg.Day), chTime(dayEnd), chTime(seg.CutoffFrom), chTime(seg.CutoffTo)).Scan(&n, &minT, &maxT); err != nil {
		return store.SegmentStats{}, fmt.Errorf("count segment rows: %w", err)
	}
	if n == 0 {
		return store.SegmentStats{}, nil
	}

	path := a.stagingPath(seg.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return store.SegmentStats{}, err
	}
	// 0644: ClickHouse runs as another user and reads the file through the shared mount.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return store.SegmentStats{}, err
	}
	h := sha256.New()
	cw := &countWriter{}
	err = a.HTTP.Query(ctx, exportSQL, map[string]string{
		"ws": seg.WorkspaceID.String(), "proj": seg.ProjectID.String(), "stream": seg.StreamID.String(),
		"day_from": chTime(seg.Day), "day_to": chTime(dayEnd),
		"ins_from": chTime(seg.CutoffFrom), "ins_to": chTime(seg.CutoffTo),
	}, map[string]string{
		"output_format_parquet_compression_method": "zstd",
		"output_format_parquet_string_as_string":   "1",
	}, io.MultiWriter(f, h, cw))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return store.SegmentStats{}, fmt.Errorf("export segment: %w", err)
	}

	// Read the file back through ClickHouse: this proves the Parquet is readable and that the
	// shared mount works, which the reader depends on.
	var got uint64
	if err := a.CH.QueryRow(ctx, fmt.Sprintf("SELECT count() FROM file('%s/staging/%s.parquet', 'Parquet', 'event_id String')",
		a.Cfg.ClickHousePath, seg.ID)).Scan(&got); err != nil {
		return store.SegmentStats{}, fmt.Errorf("read back export: %w", err)
	}
	if got != n {
		os.Remove(path)
		return store.SegmentStats{}, fmt.Errorf("export has %d rows, expected %d", got, n)
	}
	minT, maxT = minT.UTC(), maxT.UTC()
	return store.SegmentStats{Checksum: h.Sum(nil), Bytes: cw.n, RowCount: int64(n), MinEventTime: &minT, MaxEventTime: &maxT}, nil
}

// exportSQL is the Parquet layout of SchemaVersion 1. Ids are strings and attributes a JSON
// string so any Parquet reader can open the file without ClickHouse types. The WHERE clause names
// e.<column>: the SELECT aliases reuse the column names and would otherwise replace them there.
const exportSQL = `SELECT
	toString(workspace_id) AS workspace_id, toString(project_id) AS project_id,
	toString(environment_id) AS environment_id, toString(stream_id) AS stream_id,
	event_id, timestamp, ingested_at, inserted_at,
	severity_number, toString(severity_text) AS severity_text, toString(event_name) AS event_name,
	message, toString(source_service) AS source_service, toString(source_host) AS source_host,
	source_resource, trace_id, span_id, toJSONString(attributes) AS attributes, schema_version
FROM events AS e
WHERE e.workspace_id = {ws:UUID} AND e.project_id = {proj:UUID} AND e.stream_id = {stream:UUID}
  AND e.timestamp >= {day_from:DateTime64(3, 'UTC')} AND e.timestamp < {day_to:DateTime64(3, 'UTC')}
  AND e.inserted_at >= {ins_from:DateTime64(3, 'UTC')} AND e.inserted_at < {ins_to:DateTime64(3, 'UTC')}
ORDER BY timestamp, event_id
FORMAT Parquet`

// chTime formats a time for a DateTime64(3, 'UTC') parameter. The native driver binds a
// time.Time with second precision, which would move a cutoff and drop rows, so times always
// travel as explicit millisecond strings.
func chTime(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05.000") }

// ms is the native-protocol placeholder for a chTime value.
const ms = "toDateTime64(?, 3, 'UTC')"

type countWriter struct{ n int64 }

func (c *countWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// provider returns an open provider for id, reopening it when its settings changed.
func (a *Archiver) provider(ctx context.Context, id uuid.UUID) (Provider, error) {
	sp, err := a.Store.ProviderByID(ctx, id)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if op, ok := a.providers[id]; ok {
		if op.updatedAt.Equal(sp.UpdatedAt) {
			return op.p, nil
		}
		op.p.Close()
		delete(a.providers, id)
	}
	secret := ""
	if sp.SecretFile != nil {
		secret = *sp.SecretFile
	}
	p, err := Open(sp.Type, sp.Config, secret, a.Cfg.SecretsDir)
	if err != nil {
		return nil, err
	}
	if a.providers == nil {
		a.providers = map[uuid.UUID]openProvider{}
	}
	a.providers[id] = openProvider{updatedAt: sp.UpdatedAt, p: p}
	return p, nil
}
