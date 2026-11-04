package archive

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

// purgePoll is how often an idle worker looks for a queued purge.
const purgePoll = 5 * time.Second

// ErrPurgeCutsSegment means the purge would delete only part of an archived segment. Segments are
// immutable Parquet files, so a purge removes them whole or not at all (ADR 0020).
var ErrPurgeCutsSegment = errors.New("purge cuts an archived segment")

// RunPurges runs queued purge jobs until ctx ends. Jobs run under the archiver lock, so a purge never
// overlaps an archive pass that could write the rows it is deleting.
func (a *Archiver) RunPurges(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(purgePoll):
		}
		if _, err := a.Store.WithArchiverLock(ctx, a.drainPurges); err != nil && ctx.Err() == nil {
			a.Log.Error("purge worker failed", "err", err)
		}
	}
}

func (a *Archiver) drainPurges(ctx context.Context) error {
	if err := a.Store.RequeueRunningPurges(ctx); err != nil {
		return err
	}
	for ctx.Err() == nil {
		job, err := a.Store.ClaimPurgeJob(ctx)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		rows, segs, perr := a.runPurge(ctx, job)
		if ctx.Err() != nil {
			return nil // shutting down: the job is requeued by the next worker
		}
		if perr != nil {
			a.Log.Warn("purge failed", "job", job.ID, "project", job.ProjectID, "err", perr)
		} else {
			a.Log.Info("purge done", "job", job.ID, "project", job.ProjectID, "rows", rows, "segments", segs)
		}
		if err := a.Store.FinishPurgeJob(ctx, job.ID, rows, segs, perr); err != nil {
			return err
		}
	}
	return nil
}

// runPurge deletes what the job selects: segments still being written are cancelled, archived
// segments inside the selection are expired and then deleted, and the hot rows are removed.
func (a *Archiver) runPurge(ctx context.Context, job store.PurgeJob) (rows, segments int64, err error) {
	plan, err := a.PlanPurge(ctx, job)
	if err != nil {
		return 0, 0, err
	}
	from, to := purgeDays(job)
	open, err := a.Store.OpenSegments(ctx, job.WorkspaceID, job.ProjectID, job.StreamID, from, to)
	if err != nil {
		return 0, 0, err
	}
	// A segment that finished after the rows are gone would archive them again. Its stream-day is
	// archived again from the remaining hot rows by a later pass.
	for _, seg := range open {
		p, _ := a.provider(ctx, seg.ProviderID)
		a.fail(ctx, p, seg, errPermanent{errors.New("cancelled by a purge")}, 0)
	}
	for _, seg := range plan {
		ok, err := a.Store.ExpireSegment(ctx, seg.ID)
		if err != nil {
			return 0, segments, err
		}
		if ok {
			segments++
		}
	}
	if rows, err = a.purgeHot(ctx, job); err != nil {
		return rows, segments, err
	}
	var rep MaintainReport
	if err := a.deleteExpired(ctx, &rep); err != nil {
		// The data is unreadable already; the next pass retries the remote delete.
		a.Log.Warn("purge: delete expired segments", "err", err)
	}
	return rows, segments, nil
}

// PlanPurge returns the archived segments a job removes, or ErrPurgeCutsSegment when a segment
// holds events both inside and outside the selection. It reads the catalog only.
func (a *Archiver) PlanPurge(ctx context.Context, job store.PurgeJob) ([]store.Segment, error) {
	var streams []uuid.UUID
	if job.StreamID != nil {
		streams = []uuid.UUID{*job.StreamID}
	}
	from, to := purgeDays(job)
	segs, err := a.Store.ReadableSegments(ctx, job.WorkspaceID, job.ProjectID, streams, from, to, false)
	if err != nil {
		return nil, err
	}
	var out []store.Segment
	for _, seg := range segs {
		switch purgeOverlap(seg, job) {
		case overlapInside:
			out = append(out, seg)
		case overlapCut:
			if job.EnvironmentID != nil {
				return nil, fmt.Errorf("%w: archived events of %s cannot be purged by environment", ErrPurgeCutsSegment, seg.Day.Format(time.DateOnly))
			}
			return nil, fmt.Errorf("%w: archived events of %s span the edge of the time range; widen the range to cover them or narrow it to leave them out",
				ErrPurgeCutsSegment, seg.Day.Format(time.DateOnly))
		}
	}
	return out, nil
}

type overlap int

const (
	overlapNone overlap = iota
	overlapInside
	overlapCut
)

// purgeOverlap says whether every event of seg is in the job's selection, none is, or some are.
// Segments have no environment column to filter on, so an environment filter never selects one whole.
func purgeOverlap(seg store.Segment, job store.PurgeJob) overlap {
	if seg.RowCount != nil && *seg.RowCount == 0 {
		return overlapNone
	}
	lo, hi := seg.Day, seg.Day.Add(24*time.Hour-time.Millisecond)
	if seg.MinEventTime != nil && seg.MaxEventTime != nil {
		lo, hi = *seg.MinEventTime, *seg.MaxEventTime
	}
	if (job.To != nil && !lo.Before(*job.To)) || (job.From != nil && hi.Before(*job.From)) {
		return overlapNone
	}
	if job.EnvironmentID != nil {
		return overlapCut
	}
	if (job.From == nil || !lo.Before(*job.From)) && (job.To == nil || hi.Before(*job.To)) {
		return overlapInside
	}
	return overlapCut
}

// purgeDays is the day range of a job for catalog queries, unbounded sides widened.
func purgeDays(job store.PurgeJob) (from, to time.Time) {
	from, to = time.Unix(0, 0).UTC(), time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	if job.From != nil {
		from = *job.From
	}
	if job.To != nil {
		to = *job.To
	}
	return from, to
}

// purgeHot deletes the job's rows with one synchronous ALTER DELETE per day partition, like
// deleteHot and for the same reasons (ADR 0009): never DROP PARTITION, because a partition holds
// every tenant. It returns the number of rows that matched.
func (a *Archiver) purgeHot(ctx context.Context, job store.PurgeJob) (int64, error) {
	// Values are UUIDs and formatted times, never user text.
	where := fmt.Sprintf("workspace_id = toUUID('%s') AND project_id = toUUID('%s')", job.WorkspaceID, job.ProjectID)
	if job.StreamID != nil {
		where += fmt.Sprintf(" AND stream_id = toUUID('%s')", *job.StreamID)
	}
	if job.EnvironmentID != nil {
		where += fmt.Sprintf(" AND environment_id = toUUID('%s')", *job.EnvironmentID)
	}
	if job.From != nil {
		where += fmt.Sprintf(" AND timestamp >= toDateTime64('%s', 3, 'UTC')", chTime(*job.From))
	}
	if job.To != nil {
		where += fmt.Sprintf(" AND timestamp < toDateTime64('%s', 3, 'UTC')", chTime(*job.To))
	}
	rows, err := a.CH.Query(ctx, "SELECT toDate(timestamp) AS d, count() FROM events WHERE "+where+" GROUP BY d ORDER BY d")
	if err != nil {
		return 0, fmt.Errorf("purge: list days: %w", err)
	}
	type dayCount struct {
		day time.Time
		n   uint64
	}
	var days []dayCount
	for rows.Next() {
		var d dayCount
		if err := rows.Scan(&d.day, &d.n); err != nil {
			rows.Close()
			return 0, err
		}
		d.day = utcDay(d.day)
		days = append(days, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	mctx := clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"mutations_sync": 1}))
	var total int64
	for _, d := range days {
		q := fmt.Sprintf("ALTER TABLE events DELETE IN PARTITION %s WHERE toDate(timestamp) = toDate('%s') AND %s",
			d.day.Format("20060102"), d.day.Format(time.DateOnly), where)
		if err := a.CH.Exec(mctx, q); err != nil {
			return total, fmt.Errorf("purge: delete rows of %s: %w", d.day.Format(time.DateOnly), err)
		}
		total += int64(d.n)
	}
	return total, nil
}

// OldestUnarchived is the earliest event time among stream-days that hold hot rows no cataloged
// segment covers yet. It is false when everything hot is archived. A day with late rows counts from
// its first event, so the value can be earlier than the oldest unarchived row.
func (a *Archiver) OldestUnarchived(ctx context.Context) (time.Time, bool, error) {
	covs, err := a.Store.Coverages(ctx, time.Now().Add(48*time.Hour))
	if err != nil {
		return time.Time{}, false, err
	}
	type key struct {
		ws, proj, stream uuid.UUID
		day              string
	}
	cov := make(map[key]time.Time, len(covs))
	for _, c := range covs {
		cov[key{c.WorkspaceID, c.ProjectID, c.StreamID, c.Day.Format(time.DateOnly)}] = c.CutoffTo
	}
	rows, err := a.CH.Query(ctx, `SELECT workspace_id, project_id, stream_id, toDate(timestamp) AS d, min(timestamp), max(inserted_at)
		FROM events GROUP BY workspace_id, project_id, stream_id, d`)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("oldest unarchived: %w", err)
	}
	defer rows.Close()
	var oldest time.Time
	for rows.Next() {
		var k key
		var day, minTS, maxIns time.Time
		if err := rows.Scan(&k.ws, &k.proj, &k.stream, &day, &minTS, &maxIns); err != nil {
			return time.Time{}, false, err
		}
		k.day = utcDay(day).Format(time.DateOnly)
		if cutoff, ok := cov[k]; ok && maxIns.Before(cutoff) {
			continue
		}
		if oldest.IsZero() || minTS.Before(oldest) {
			oldest = minTS
		}
	}
	return oldest.UTC(), !oldest.IsZero(), rows.Err()
}
