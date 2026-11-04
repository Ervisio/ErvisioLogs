package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Purge job states.
const (
	PurgeQueued  = "queued"
	PurgeRunning = "running"
	PurgeDone    = "done"
	PurgeFailed  = "failed"
)

// PurgeJob is a manual deletion of a project's logs (ADR 0020). Nil filters match everything.
type PurgeJob struct {
	ID              uuid.UUID  `json:"id"`
	WorkspaceID     uuid.UUID  `json:"workspace_id"`
	ProjectID       uuid.UUID  `json:"project_id"`
	StreamID        *uuid.UUID `json:"stream_id"`
	EnvironmentID   *uuid.UUID `json:"environment_id"`
	From            *time.Time `json:"from"`
	To              *time.Time `json:"to"`
	State           string     `json:"state"`
	RowsDeleted     int64      `json:"rows_deleted"`
	SegmentsRemoved int64      `json:"segments_removed"`
	Error           *string    `json:"error"`
	CreatedBy       *uuid.UUID `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at"`
}

const purgeCols = `id, workspace_id, project_id, stream_id, environment_id, time_from, time_to, state,
	rows_deleted, segments_removed, error, created_by, created_at, started_at, finished_at`

func scanPurge(r pgx.Row) (PurgeJob, error) {
	var j PurgeJob
	err := r.Scan(&j.ID, &j.WorkspaceID, &j.ProjectID, &j.StreamID, &j.EnvironmentID, &j.From, &j.To, &j.State,
		&j.RowsDeleted, &j.SegmentsRemoved, &j.Error, &j.CreatedBy, &j.CreatedAt, &j.StartedAt, &j.FinishedAt)
	return j, mapErr(err)
}

// CreatePurgeJob queues j; the caller sets the project, filters and creator.
func (s *Store) CreatePurgeJob(ctx context.Context, j PurgeJob) (PurgeJob, error) {
	return scanPurge(s.pool.QueryRow(ctx, `INSERT INTO purge_jobs
		(id, workspace_id, project_id, stream_id, environment_id, time_from, time_to, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+purgeCols,
		newID(), j.WorkspaceID, j.ProjectID, j.StreamID, j.EnvironmentID, j.From, j.To, j.CreatedBy))
}

func (s *Store) PurgeJob(ctx context.Context, workspaceID, projectID, id uuid.UUID) (PurgeJob, error) {
	return scanPurge(s.pool.QueryRow(ctx, `SELECT `+purgeCols+` FROM purge_jobs
		WHERE id = $3 AND project_id = $2 AND workspace_id = $1`, workspaceID, projectID, id))
}

// ListPurgeJobs returns the newest jobs of a project first.
func (s *Store) ListPurgeJobs(ctx context.Context, workspaceID, projectID uuid.UUID, limit int) ([]PurgeJob, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+purgeCols+` FROM purge_jobs
		WHERE project_id = $2 AND workspace_id = $1 ORDER BY created_at DESC, id LIMIT $3`, workspaceID, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PurgeJob{}
	for rows.Next() {
		j, err := scanPurge(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// HasOpenPurge reports whether the project already has a queued or running purge.
func (s *Store) HasOpenPurge(ctx context.Context, projectID uuid.UUID) (bool, error) {
	var open bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM purge_jobs WHERE project_id = $1 AND state IN ('queued','running'))`,
		projectID).Scan(&open)
	return open, err
}

// RequeueRunningPurges puts jobs a stopped process left running back in the queue. The caller holds
// the archiver lock, so no live worker owns them.
func (s *Store) RequeueRunningPurges(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE purge_jobs SET state = 'queued', started_at = NULL WHERE state = 'running'`)
	return err
}

// ClaimPurgeJob starts the oldest queued job, or returns ErrNotFound when there is none. Only the
// holder of the archiver lock calls it.
func (s *Store) ClaimPurgeJob(ctx context.Context) (PurgeJob, error) {
	return scanPurge(s.pool.QueryRow(ctx, `UPDATE purge_jobs SET state = 'running', started_at = now()
		WHERE id = (SELECT id FROM purge_jobs WHERE state = 'queued' ORDER BY created_at LIMIT 1)
		RETURNING `+purgeCols))
}

// FinishPurgeJob records the outcome; a non-nil cause marks the job failed.
func (s *Store) FinishPurgeJob(ctx context.Context, id uuid.UUID, rows, segments int64, cause error) error {
	state, msg := PurgeDone, (*string)(nil)
	if cause != nil {
		state = PurgeFailed
		m := cause.Error()
		if len(m) > 500 {
			m = m[:500]
		}
		msg = &m
	}
	_, err := s.pool.Exec(ctx, `UPDATE purge_jobs SET state = $2, rows_deleted = $3, segments_removed = $4, error = $5, finished_at = now()
		WHERE id = $1`, id, state, rows, segments, msg)
	return err
}

// OpenSegments returns the project's segments that are still being written (STAGING, UPLOADING,
// VERIFIED) on days overlapping [from, to]; stream nil means every stream.
func (s *Store) OpenSegments(ctx context.Context, workspaceID, projectID uuid.UUID, stream *uuid.UUID, from, to time.Time) ([]Segment, error) {
	return collectSegments(s.pool.Query(ctx, `SELECT `+segmentCols+` FROM archive_segments
		WHERE workspace_id = $1 AND project_id = $2 AND state IN ('STAGING','UPLOADING','VERIFIED')
			AND day BETWEEN $3::date AND $4::date AND ($5::uuid IS NULL OR stream_id = $5)
		ORDER BY created_at`, workspaceID, projectID, from.UTC(), to.UTC(), stream))
}

// ExpireSegment moves one readable segment to REMOTE_EXPIRED, so searches stop reading it before
// the remote delete starts. It reports false when the segment was no longer readable.
func (s *Store) ExpireSegment(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE archive_segments SET state = 'REMOTE_EXPIRED', next_attempt_at = NULL, attempts = 0, updated_at = now()
		WHERE id = $1 AND state IN ('CATALOGED','HOT_EVICTABLE')`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
