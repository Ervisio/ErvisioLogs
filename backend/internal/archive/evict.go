package archive

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

// MaintainReport says what the retention and capacity step of one pass did.
type MaintainReport struct {
	HotExpiredDays    int // stream-days whose hot rows went away at global retention
	SegmentsExpired   int // segments moved to REMOTE_EXPIRED
	SegmentsDeleted   int // remote objects deleted
	HotEvictedDays    int // covered stream-days evicted from ClickHouse
	CacheFilesEvicted int
	UsageBytes        int64 // hot bytes plus cache bytes after the step
	OverCapacity      bool
}

type streamDay struct {
	ws, proj, stream uuid.UUID
	day              time.Time
	cutoff           time.Time // eviction only: rows inserted before it are covered
}

// maintain applies retention and the cache budget (ADR 0004, ADR 0008): global expiry of hot
// rows and segments, eviction of covered hot rows past local age, then eviction by size while
// usage is above the high watermark. It needs no active provider: deleting needs only the
// provider each segment was written to.
func (a *Archiver) maintain(ctx context.Context, now time.Time, pol Policies, rep *MaintainReport) error {
	if err := a.expireHot(ctx, now, pol, rep); err != nil {
		return err
	}
	covs, err := a.Store.Coverages(ctx, now.Add(24*time.Hour))
	if err != nil {
		return err
	}
	var expire, evict []streamDay
	for _, c := range covs {
		r := pol.For(c.WorkspaceID, c.ProjectID, c.StreamID)
		end := c.Day.Add(24 * time.Hour)
		sd := streamDay{c.WorkspaceID, c.ProjectID, c.StreamID, c.Day, c.CutoffTo}
		switch {
		case !end.After(now.Add(-r.GlobalAge)):
			expire = append(expire, sd)
		case !c.Evicted && !end.After(now.Add(-r.LocalAge)):
			evict = append(evict, sd)
		}
	}
	for _, sd := range expire {
		n, err := a.Store.ExpireSegments(ctx, sd.ws, sd.proj, sd.stream, sd.day)
		if err != nil {
			return err
		}
		rep.SegmentsExpired += int(n)
	}
	if err := a.deleteExpired(ctx, rep); err != nil {
		return err
	}
	if err := a.evictHot(ctx, evict); err != nil {
		return err
	}
	rep.HotEvictedDays += len(evict)
	return a.enforceBudget(ctx, pol.Budget(), rep)
}

// expireHot deletes every hot row of stream-days past global retention, archived or not.
func (a *Archiver) expireHot(ctx context.Context, now time.Time, pol Policies, rep *MaintainReport) error {
	// No policy keeps data for less than an hour; anything newer cannot be expired.
	rows, err := a.CH.Query(ctx, `SELECT DISTINCT workspace_id, project_id, stream_id, toDate(timestamp) AS d
		FROM events WHERE timestamp < `+ms+` ORDER BY d`, chTime(now.Add(-time.Hour).Truncate(24*time.Hour)))
	if err != nil {
		return fmt.Errorf("hot days: %w", err)
	}
	var del []streamDay
	for rows.Next() {
		var sd streamDay
		if err := rows.Scan(&sd.ws, &sd.proj, &sd.stream, &sd.day); err != nil {
			rows.Close()
			return err
		}
		sd.day = utcDay(sd.day)
		if !sd.day.Add(24 * time.Hour).After(now.Add(-pol.For(sd.ws, sd.proj, sd.stream).GlobalAge)) {
			sd.cutoff = time.Time{} // every row
			del = append(del, sd)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if err := a.deleteHot(ctx, del); err != nil {
		return err
	}
	rep.HotExpiredDays += len(del)
	return nil
}

// evictHot marks the segments as the read source, then deletes the covered hot rows. In that
// order a search between the two steps reads both tiers and deduplicates; in the other order it
// would briefly miss the rows.
func (a *Archiver) evictHot(ctx context.Context, sds []streamDay) error {
	for _, sd := range sds {
		if err := a.Store.MarkHotEvicted(ctx, sd.ws, sd.proj, sd.stream, sd.day); err != nil {
			return err
		}
	}
	return a.deleteHot(ctx, sds)
}

// deleteHot removes hot rows with one synchronous mutation per day partition. A stream-day with
// a cutoff loses only the rows inserted before it (the covered ones); a zero cutoff removes all.
// ALTER DELETE instead of lightweight DELETE, which keeps the bytes until a merge, and instead
// of DROP PARTITION, because a day partition holds every tenant. ADR 0009 (D004) records the
// choice; its benchmark has not been run.
func (a *Archiver) deleteHot(ctx context.Context, sds []streamDay) error {
	byDay := map[time.Time][]streamDay{}
	for _, sd := range sds {
		byDay[sd.day] = append(byDay[sd.day], sd)
	}
	days := make([]time.Time, 0, len(byDay))
	for d := range byDay {
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	mctx := clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"mutations_sync": 1}))
	for _, d := range days {
		conds := make([]string, 0, len(byDay[d]))
		for _, sd := range byDay[d] {
			// Values are UUIDs and formatted times from our own types, never user text.
			c := fmt.Sprintf("(workspace_id = toUUID('%s') AND project_id = toUUID('%s') AND stream_id = toUUID('%s')",
				sd.ws, sd.proj, sd.stream)
			if !sd.cutoff.IsZero() {
				c += fmt.Sprintf(" AND inserted_at < toDateTime64('%s', 3, 'UTC')", chTime(sd.cutoff))
			}
			conds = append(conds, c+")")
		}
		q := fmt.Sprintf("ALTER TABLE events DELETE IN PARTITION %s WHERE toDate(timestamp) = toDate('%s') AND (%s)",
			d.Format("20060102"), d.Format(time.DateOnly), strings.Join(conds, " OR "))
		if err := a.CH.Exec(mctx, q); err != nil {
			return fmt.Errorf("delete hot rows of %s: %w", d.Format(time.DateOnly), err)
		}
	}
	return nil
}

// deleteExpired drives REMOTE_EXPIRED and DELETING segments to DELETED. The row stays as a
// tombstone. Failures retry with backoff and never mark the segment FAILED: the data is already
// unreadable, only the remote bytes are left.
func (a *Archiver) deleteExpired(ctx context.Context, rep *MaintainReport) error {
	segs, err := a.Store.SegmentsDue(ctx, []string{store.SegRemoteExpired, store.SegDeleting}, time.Now(), dueBatch)
	if err != nil {
		return err
	}
	for _, seg := range segs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := a.deleteSegment(ctx, seg)
		switch {
		case err == nil:
			rep.SegmentsDeleted++
		case errors.Is(err, store.ErrConflict), ctx.Err() != nil:
		default:
			a.Log.Warn("segment delete failed", "segment", seg.ID, "err", err)
			if _, ferr := a.Store.FailSegmentAttempt(ctx, seg.ID, err, time.Now().Add(backoff(seg.Attempts)), math.MaxInt32); ferr != nil {
				return ferr
			}
		}
	}
	return nil
}

func (a *Archiver) deleteSegment(ctx context.Context, seg store.Segment) error {
	if seg.State == store.SegRemoteExpired {
		if err := a.Store.AdvanceSegment(ctx, seg.ID, store.SegRemoteExpired, store.SegDeleting, nil); err != nil {
			return err
		}
	}
	p, err := a.provider(ctx, seg.ProviderID)
	if err != nil {
		return err
	}
	if err := p.Delete(ctx, seg.ObjectKey); err != nil {
		return err
	}
	if err := os.Remove(a.SegmentPath(seg.ID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return a.Store.AdvanceSegment(ctx, seg.ID, store.SegDeleting, store.SegDeleted, nil)
}

// HotBytes is the on-disk size of the active parts of the events table.
func (a *Archiver) HotBytes(ctx context.Context) (int64, error) {
	var n uint64
	err := a.CH.QueryRow(ctx, `SELECT toUInt64(sum(bytes_on_disk)) FROM system.parts
		WHERE database = currentDatabase() AND table = 'events' AND active`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("hot bytes: %w", err)
	}
	return int64(n), nil
}

type cacheFile struct {
	path  string
	size  int64
	mtime time.Time
}

// cacheFiles lists the cached segments, least recently used first. Readers touch the mtime.
func (a *Archiver) cacheFiles() ([]cacheFile, int64, error) {
	dir := filepath.Join(a.Cfg.CacheDir, "segments")
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var out []cacheFile
	var total int64
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".parquet") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // removed meanwhile
		}
		out = append(out, cacheFile{filepath.Join(dir, e.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mtime.Before(out[j].mtime) })
	return out, total, nil
}

// enforceBudget keeps hot bytes plus cache bytes under the budget. Above the high watermark it
// drops cached segments (they can be downloaded again), then evicts the oldest covered hot days,
// until usage is under the low watermark. Uncovered rows are never evicted; if they alone exceed
// the cap, the instance is over capacity and ingest answers 503.
func (a *Archiver) enforceBudget(ctx context.Context, b Budget, rep *MaintainReport) error {
	usage := func() (int64, []cacheFile, error) {
		hot, err := a.HotBytes(ctx)
		if err != nil {
			return 0, nil, err
		}
		files, cache, err := a.cacheFiles()
		return hot + cache, files, err
	}
	u, files, err := usage()
	if err != nil {
		return err
	}
	if u > b.High() {
		for _, f := range files {
			if u <= b.Low() {
				break
			}
			if err := os.Remove(f.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			u -= f.size
			rep.CacheFilesEvicted++
		}
		for u > b.Low() {
			day, err := a.oldestEvictable(ctx)
			if err != nil {
				return err
			}
			if len(day) == 0 {
				break
			}
			if err := a.evictHot(ctx, day); err != nil {
				return err
			}
			rep.HotEvictedDays += len(day)
			if u, _, err = usage(); err != nil {
				return err
			}
		}
	}
	rep.UsageBytes = u
	rep.OverCapacity = u > b.MaxBytes
	a.over.Store(rep.OverCapacity)
	return nil
}

// oldestEvictable returns the covered, not yet evicted stream-days of the oldest such day.
func (a *Archiver) oldestEvictable(ctx context.Context) ([]streamDay, error) {
	covs, err := a.Store.Coverages(ctx, time.Now().Add(24*time.Hour))
	if err != nil {
		return nil, err
	}
	var out []streamDay
	for _, c := range covs { // oldest day first
		if c.Evicted {
			continue
		}
		if len(out) > 0 && !c.Day.Equal(out[0].day) {
			break
		}
		out = append(out, streamDay{c.WorkspaceID, c.ProjectID, c.StreamID, c.Day, c.CutoffTo})
	}
	return out, nil
}

// OverCapacity reports whether the last pass found usage above the cache cap with nothing left
// to evict. Ingest answers 503 while it is true.
func (a *Archiver) OverCapacity() bool { return a.over.Load() }

func utcDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// Usage returns the hot bytes and the cache bytes that the budget counts.
func (a *Archiver) Usage(ctx context.Context) (hot, cache int64, err error) {
	if hot, err = a.HotBytes(ctx); err != nil {
		return 0, 0, err
	}
	_, cache, err = a.cacheFiles()
	return hot, cache, err
}
