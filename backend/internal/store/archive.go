package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---------- storage providers ----------

type StorageProvider struct {
	ID            uuid.UUID       `json:"id"`
	Name          string          `json:"name"`
	Type          string          `json:"type"`
	Config        json.RawMessage `json:"config"`
	SecretFile    *string         `json:"secret_file"`
	Active        bool            `json:"active"`
	Status        string          `json:"status"`
	LastError     *string         `json:"last_error"`
	LastCheckedAt *time.Time      `json:"last_checked_at"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// ErrProviderInUse means segments still point at the provider, so it cannot be removed.
var ErrProviderInUse = errors.New("provider still holds archive segments")

const providerCols = `id, name, type, config, secret_file, active, status, last_error, last_checked_at, created_at, updated_at`

func scanProvider(r pgx.Row) (StorageProvider, error) {
	var p StorageProvider
	err := r.Scan(&p.ID, &p.Name, &p.Type, &p.Config, &p.SecretFile, &p.Active, &p.Status, &p.LastError, &p.LastCheckedAt, &p.CreatedAt, &p.UpdatedAt)
	return p, mapErr(err)
}

func (s *Store) CreateProvider(ctx context.Context, name, typ string, config json.RawMessage, secretFile *string) (StorageProvider, error) {
	return scanProvider(s.pool.QueryRow(ctx, `INSERT INTO storage_providers (id, name, type, config, secret_file)
		VALUES ($1, $2, $3, $4, $5) RETURNING `+providerCols, newID(), name, typ, config, secretFile))
}

// UpdateProvider replaces the settings and resets the health status, since the old result no
// longer describes the new settings.
func (s *Store) UpdateProvider(ctx context.Context, id uuid.UUID, name string, config json.RawMessage, secretFile *string) (StorageProvider, error) {
	return scanProvider(s.pool.QueryRow(ctx, `UPDATE storage_providers
		SET name = $2, config = $3, secret_file = $4, status = 'unknown', last_error = NULL, last_checked_at = NULL, updated_at = now()
		WHERE id = $1 RETURNING `+providerCols, id, name, config, secretFile))
}

func (s *Store) ProviderByID(ctx context.Context, id uuid.UUID) (StorageProvider, error) {
	return scanProvider(s.pool.QueryRow(ctx, `SELECT `+providerCols+` FROM storage_providers WHERE id = $1`, id))
}

// ActiveProvider returns the selected archive provider, or ErrNotFound when there is none.
func (s *Store) ActiveProvider(ctx context.Context) (StorageProvider, error) {
	return scanProvider(s.pool.QueryRow(ctx, `SELECT `+providerCols+` FROM storage_providers WHERE active`))
}

func (s *Store) ListProviders(ctx context.Context) ([]StorageProvider, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+providerCols+` FROM storage_providers ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StorageProvider{}
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetActiveProvider selects id as the archive target (nil deselects every provider). Segments
// already written stay readable from their own provider.
func (s *Store) SetActiveProvider(ctx context.Context, id *uuid.UUID) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE storage_providers SET active = false, updated_at = now() WHERE active`); err != nil {
			return err
		}
		if id == nil {
			return nil
		}
		tag, err := tx.Exec(ctx, `UPDATE storage_providers SET active = true, updated_at = now() WHERE id = $1`, *id)
		if err != nil {
			return mapErr(err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (s *Store) SetProviderStatus(ctx context.Context, id uuid.UUID, healthErr error) error {
	status, msg := "ok", (*string)(nil)
	if healthErr != nil {
		status = "error"
		m := healthErr.Error()
		if len(m) > 500 {
			m = m[:500]
		}
		msg = &m
	}
	_, err := s.pool.Exec(ctx, `UPDATE storage_providers SET status = $2, last_error = $3, last_checked_at = now() WHERE id = $1`, id, status, msg)
	return err
}

// DeleteProvider removes a provider that no segment references, tombstones included.
func (s *Store) DeleteProvider(ctx context.Context, id uuid.UUID) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM archive_segments WHERE provider_id = $1)`, id).Scan(&used); err != nil {
			return err
		}
		if used {
			return ErrProviderInUse
		}
		tag, err := tx.Exec(ctx, `DELETE FROM storage_providers WHERE id = $1`, id)
		if err != nil {
			return mapErr(err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ---------- retention policies ----------

// InstanceScope is the scope_id of the instance-wide policy row.
var InstanceScope = uuid.Nil

type RetentionPolicy struct {
	ScopeType      string     `json:"scope_type"`
	ScopeID        uuid.UUID  `json:"scope_id"`
	LocalAgeHours  *int32     `json:"local_age_hours"`
	GlobalAgeHours *int32     `json:"global_age_hours"`
	CacheMaxBytes  *int64     `json:"cache_max_bytes,omitempty"`
	HighPct        *int16     `json:"high_pct,omitempty"`
	LowPct         *int16     `json:"low_pct,omitempty"`
	UpdatedBy      *uuid.UUID `json:"updated_by"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

const retentionCols = `scope_type, scope_id, local_age_hours, global_age_hours, cache_max_bytes, high_pct, low_pct, updated_by, updated_at`

func scanRetention(r pgx.Row) (RetentionPolicy, error) {
	var p RetentionPolicy
	err := r.Scan(&p.ScopeType, &p.ScopeID, &p.LocalAgeHours, &p.GlobalAgeHours, &p.CacheMaxBytes, &p.HighPct, &p.LowPct, &p.UpdatedBy, &p.UpdatedAt)
	return p, mapErr(err)
}

// RetentionPolicies returns every policy row. There is one per configured scope, so the set stays
// small enough to resolve in memory.
func (s *Store) RetentionPolicies(ctx context.Context) ([]RetentionPolicy, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+retentionCols+` FROM retention_policies`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RetentionPolicy{}
	for rows.Next() {
		p, err := scanRetention(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PutRetentionPolicy inserts or replaces the row of one scope. The table checks enforce
// global >= local within the row; callers check it against the resolved, inherited values.
func (s *Store) PutRetentionPolicy(ctx context.Context, p RetentionPolicy) (RetentionPolicy, error) {
	return scanRetention(s.pool.QueryRow(ctx, `INSERT INTO retention_policies
		(scope_type, scope_id, local_age_hours, global_age_hours, cache_max_bytes, high_pct, low_pct, updated_by, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		ON CONFLICT (scope_type, scope_id) DO UPDATE SET local_age_hours = EXCLUDED.local_age_hours,
			global_age_hours = EXCLUDED.global_age_hours, cache_max_bytes = EXCLUDED.cache_max_bytes,
			high_pct = EXCLUDED.high_pct, low_pct = EXCLUDED.low_pct, updated_by = EXCLUDED.updated_by, updated_at = now()
		RETURNING `+retentionCols,
		p.ScopeType, p.ScopeID, p.LocalAgeHours, p.GlobalAgeHours, p.CacheMaxBytes, p.HighPct, p.LowPct, p.UpdatedBy))
}

// DeleteRetentionPolicy removes an override so the scope inherits again. The instance row stays.
func (s *Store) DeleteRetentionPolicy(ctx context.Context, scopeType string, scopeID uuid.UUID) error {
	if scopeType == "instance" {
		return ErrForbidden
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM retention_policies WHERE scope_type = $1 AND scope_id = $2`, scopeType, scopeID)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- archive segments ----------

// Segment states (ADR 0008).
const (
	SegStaging       = "STAGING"
	SegUploading     = "UPLOADING"
	SegVerified      = "VERIFIED"
	SegCataloged     = "CATALOGED"
	SegHotEvictable  = "HOT_EVICTABLE"
	SegRemoteExpired = "REMOTE_EXPIRED"
	SegDeleting      = "DELETING"
	SegDeleted       = "DELETED"
	SegFailed        = "FAILED"
)

type Segment struct {
	ID            uuid.UUID
	WorkspaceID   uuid.UUID
	ProjectID     uuid.UUID
	StreamID      uuid.UUID
	Day           time.Time
	Generation    int32
	ProviderID    uuid.UUID
	ObjectKey     string
	State         string
	CutoffFrom    time.Time
	CutoffTo      time.Time
	MinEventTime  *time.Time
	MaxEventTime  *time.Time
	Checksum      []byte
	Bytes         *int64
	RowCount      *int64
	SchemaVersion int32
	Attempts      int32
	NextAttemptAt *time.Time
	LastError     *string
	CatalogedAt   *time.Time
	HotEvictedAt  *time.Time
	DeletedAt     *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

const segmentCols = `id, workspace_id, project_id, stream_id, day, generation, provider_id, object_key, state,
	cutoff_from, cutoff_to, min_event_time, max_event_time, checksum_sha256, bytes, row_count, schema_version,
	attempts, next_attempt_at, last_error, cataloged_at, hot_evicted_at, deleted_at, created_at, updated_at`

func scanSegment(r pgx.Row) (Segment, error) {
	var g Segment
	err := r.Scan(&g.ID, &g.WorkspaceID, &g.ProjectID, &g.StreamID, &g.Day, &g.Generation, &g.ProviderID, &g.ObjectKey, &g.State,
		&g.CutoffFrom, &g.CutoffTo, &g.MinEventTime, &g.MaxEventTime, &g.Checksum, &g.Bytes, &g.RowCount, &g.SchemaVersion,
		&g.Attempts, &g.NextAttemptAt, &g.LastError, &g.CatalogedAt, &g.HotEvictedAt, &g.DeletedAt, &g.CreatedAt, &g.UpdatedAt)
	if err == nil {
		g.Day = g.Day.UTC()
	}
	return g, mapErr(err)
}

func collectSegments(rows pgx.Rows, err error) ([]Segment, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Segment{}
	for rows.Next() {
		g, err := scanSegment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ErrSegmentBusy means a generation of the stream-day is still in progress, or the day is
// tombstoned, so no new generation may start.
var ErrSegmentBusy = errors.New("stream-day has a segment in progress or is deleted")

// NewSegment is what the archiver knows when it starts a generation.
type NewSegment struct {
	WorkspaceID, ProjectID, StreamID uuid.UUID
	Day                              time.Time
	ProviderID                       uuid.UUID
	CutoffTo                         time.Time
	SchemaVersion                    int32
	// KeyFor builds the final object key from the new segment's id and generation.
	KeyFor func(id uuid.UUID, generation int32) string
}

// StartSegment opens the next generation of a stream-day in STAGING. Its range starts where the
// cataloged generations end (cutoff_from = their max cutoff_to, or the epoch for the first one),
// so cataloged generations always cover one contiguous range. FAILED generations do not count:
// their rows are picked up again by the next one.
func (s *Store) StartSegment(ctx context.Context, n NewSegment) (Segment, error) {
	var out Segment
	err := s.tx(ctx, func(tx pgx.Tx) error {
		// Serialize generation numbering per stream-day.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text || $2::text || $3::text, 0))`,
			n.StreamID, n.ProjectID, n.Day.Format(time.DateOnly)); err != nil {
			return err
		}
		var busy bool
		var maxGen int32
		var from *time.Time
		err := tx.QueryRow(ctx, `SELECT
				COALESCE(bool_or(state IN ('STAGING','UPLOADING','VERIFIED','REMOTE_EXPIRED','DELETING','DELETED')), false),
				COALESCE(max(generation), 0),
				max(cutoff_to) FILTER (WHERE state IN ('CATALOGED','HOT_EVICTABLE'))
			FROM archive_segments WHERE workspace_id = $1 AND project_id = $2 AND stream_id = $3 AND day = $4`,
			n.WorkspaceID, n.ProjectID, n.StreamID, n.Day).Scan(&busy, &maxGen, &from)
		if err != nil {
			return err
		}
		if busy {
			return ErrSegmentBusy
		}
		cutoffFrom := time.Unix(0, 0).UTC()
		if from != nil {
			cutoffFrom = *from
		}
		if !n.CutoffTo.After(cutoffFrom) {
			return ErrSegmentBusy
		}
		id, gen := newID(), maxGen+1
		out, err = scanSegment(tx.QueryRow(ctx, `INSERT INTO archive_segments
			(id, workspace_id, project_id, stream_id, day, generation, provider_id, object_key, state, cutoff_from, cutoff_to, schema_version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'STAGING', $9, $10, $11) RETURNING `+segmentCols,
			id, n.WorkspaceID, n.ProjectID, n.StreamID, n.Day, gen, n.ProviderID, n.KeyFor(id, gen), cutoffFrom, n.CutoffTo, n.SchemaVersion))
		return err
	})
	return out, err
}

// SegmentStats are filled in once the Parquet file is written.
type SegmentStats struct {
	Checksum     []byte
	Bytes        int64
	RowCount     int64
	MinEventTime *time.Time
	MaxEventTime *time.Time
}

// AdvanceSegment moves a segment from one state to the next, only if it is still in from, and
// records stats when given. It returns ErrConflict when another worker moved it first.
func (s *Store) AdvanceSegment(ctx context.Context, id uuid.UUID, from, to string, stats *SegmentStats) error {
	var st SegmentStats
	has := stats != nil
	if has {
		st = *stats
	}
	tag, err := s.pool.Exec(ctx, `UPDATE archive_segments SET state = $3, last_error = NULL, attempts = 0, next_attempt_at = NULL,
			checksum_sha256 = CASE WHEN $4 THEN $5 ELSE checksum_sha256 END,
			bytes = CASE WHEN $4 THEN $6 ELSE bytes END,
			row_count = CASE WHEN $4 THEN $7 ELSE row_count END,
			min_event_time = CASE WHEN $4 THEN $8 ELSE min_event_time END,
			max_event_time = CASE WHEN $4 THEN $9 ELSE max_event_time END,
			cataloged_at = CASE WHEN $3 = 'CATALOGED' THEN now() ELSE cataloged_at END,
			deleted_at = CASE WHEN $3 = 'DELETED' THEN now() ELSE deleted_at END,
			updated_at = now()
		WHERE id = $1 AND state = $2`,
		id, from, to, has, st.Checksum, st.Bytes, st.RowCount, st.MinEventTime, st.MaxEventTime)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

// MaxCutoff is the largest cutoff any segment was given, or the zero time. The cutoffs come from
// the ClickHouse clock, so a ClickHouse clock below this value has jumped backwards (ADR 0008).
func (s *Store) MaxCutoff(ctx context.Context) (time.Time, error) {
	var t *time.Time
	if err := s.pool.QueryRow(ctx, `SELECT max(cutoff_to) FROM archive_segments `).Scan(&t); err != nil {
		return time.Time{}, err
	}
	if t == nil {
		return time.Time{}, nil
	}
	return *t, nil
}

// archiverLockKey is the session advisory lock that keeps a single archiver running per instance.
const archiverLockKey = 0x45525641524348 // "ERVARCH"

// WithArchiverLock runs fn while holding the instance-wide archiver lock. It returns false
// without running fn when another process holds the lock.
func (s *Store) WithArchiverLock(ctx context.Context, fn func(context.Context) error) (bool, error) {
	return s.withAdvisoryLock(ctx, archiverLockKey, fn)
}

// alertLockKey keeps a single alert evaluator running per instance.
const alertLockKey = 0x4552564c414c5254 // "ERVLALRT"

func (s *Store) withAdvisoryLock(ctx context.Context, key int64, fn func(context.Context) error) (bool, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&got); err != nil {
		return false, err
	}
	if !got {
		return false, nil
	}
	defer func() {
		// A fresh context: the caller's may be cancelled, and a lock left on a pooled
		// connection would block every later pass.
		uctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(uctx, `SELECT pg_advisory_unlock($1)`, key); err != nil {
			conn.Conn().Close(uctx)
		}
	}()
	return true, fn(ctx)
}

// FailSegmentAttempt records an error and schedules a retry, or marks the segment FAILED once
// maxAttempts is reached. It returns the new state.
func (s *Store) FailSegmentAttempt(ctx context.Context, id uuid.UUID, cause error, retryAt time.Time, maxAttempts int32) (string, error) {
	msg := cause.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	var state string
	err := s.pool.QueryRow(ctx, `UPDATE archive_segments SET attempts = attempts + 1, last_error = $2, updated_at = now(),
			next_attempt_at = $3,
			state = CASE WHEN attempts + 1 >= $4 AND state IN ('STAGING','UPLOADING','VERIFIED') THEN 'FAILED' ELSE state END
		WHERE id = $1 RETURNING state`, id, msg, retryAt, maxAttempts).Scan(&state)
	return state, mapErr(err)
}

// SegmentsDue returns in-progress segments whose retry time has come, oldest first.
func (s *Store) SegmentsDue(ctx context.Context, states []string, now time.Time, limit int) ([]Segment, error) {
	return collectSegments(s.pool.Query(ctx, `SELECT `+segmentCols+` FROM archive_segments
		WHERE state = ANY($1) AND (next_attempt_at IS NULL OR next_attempt_at <= $2)
		ORDER BY created_at LIMIT $3`, states, now, limit))
}

func (s *Store) SegmentByID(ctx context.Context, id uuid.UUID) (Segment, error) {
	return scanSegment(s.pool.QueryRow(ctx, `SELECT `+segmentCols+` FROM archive_segments WHERE id = $1`, id))
}

// StreamDaySegments returns every generation of a stream-day, tombstones included.
func (s *Store) StreamDaySegments(ctx context.Context, workspaceID, projectID, streamID uuid.UUID, day time.Time) ([]Segment, error) {
	return collectSegments(s.pool.Query(ctx, `SELECT `+segmentCols+` FROM archive_segments
		WHERE workspace_id = $1 AND project_id = $2 AND stream_id = $3 AND day = $4 ORDER BY generation`,
		workspaceID, projectID, streamID, day))
}

// ReadableSegments returns the cataloged segments of a project whose day overlaps [from, to],
// optionally limited to some streams. evictedOnly limits them to days the hot tier no longer
// holds; the rest are still answered from ClickHouse.
func (s *Store) ReadableSegments(ctx context.Context, workspaceID, projectID uuid.UUID, streams []uuid.UUID, from, to time.Time, evictedOnly bool) ([]Segment, error) {
	return collectSegments(s.pool.Query(ctx, `SELECT `+segmentCols+` FROM archive_segments
		WHERE workspace_id = $1 AND project_id = $2 AND state IN ('CATALOGED','HOT_EVICTABLE')
			AND day BETWEEN $3::date AND $4::date
			AND (COALESCE(cardinality($5::uuid[]), 0) = 0 OR stream_id = ANY($5))
			AND (NOT $6 OR hot_evicted_at IS NOT NULL)
		ORDER BY day DESC, stream_id, generation`,
		workspaceID, projectID, from.UTC(), to.UTC(), streams, evictedOnly))
}

// Coverage is how far the archive covers one stream-day: every hot row with inserted_at before
// CutoffTo is in a cataloged segment.
type Coverage struct {
	WorkspaceID, ProjectID, StreamID uuid.UUID
	Day                              time.Time
	CutoffTo                         time.Time
	Bytes                            int64
	Evicted                          bool
	// LastStartedAt is when the newest generation of the stream-day was created, in any state.
	LastStartedAt time.Time
}

// Coverages returns the covered stream-days with day < before, oldest first.
func (s *Store) Coverages(ctx context.Context, before time.Time) ([]Coverage, error) {
	rows, err := s.pool.Query(ctx, `SELECT workspace_id, project_id, stream_id, day,
			max(cutoff_to) FILTER (WHERE state IN ('CATALOGED','HOT_EVICTABLE')),
			COALESCE(sum(bytes) FILTER (WHERE state IN ('CATALOGED','HOT_EVICTABLE')), 0)::bigint,
			COALESCE(bool_and(hot_evicted_at IS NOT NULL) FILTER (WHERE state IN ('CATALOGED','HOT_EVICTABLE')), false),
			max(created_at)
		FROM archive_segments WHERE day < $1::date
		GROUP BY workspace_id, project_id, stream_id, day
		HAVING bool_or(state IN ('CATALOGED','HOT_EVICTABLE'))
		ORDER BY day, workspace_id, project_id, stream_id`, before.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Coverage{}
	for rows.Next() {
		var c Coverage
		if err := rows.Scan(&c.WorkspaceID, &c.ProjectID, &c.StreamID, &c.Day, &c.CutoffTo, &c.Bytes, &c.Evicted, &c.LastStartedAt); err != nil {
			return nil, err
		}
		c.Day = c.Day.UTC()
		out = append(out, c)
	}
	return out, rows.Err()
}

// MarkHotEvicted records that ClickHouse no longer holds the covered rows of a stream-day, so
// searches must read its segments.
func (s *Store) MarkHotEvicted(ctx context.Context, workspaceID, projectID, streamID uuid.UUID, day time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE archive_segments SET state = 'HOT_EVICTABLE', hot_evicted_at = COALESCE(hot_evicted_at, now()), updated_at = now()
		WHERE workspace_id = $1 AND project_id = $2 AND stream_id = $3 AND day = $4 AND state IN ('CATALOGED','HOT_EVICTABLE')`,
		workspaceID, projectID, streamID, day)
	return err
}

// ExpireSegments moves every readable segment of a stream-day to REMOTE_EXPIRED, so searches
// stop reading it before the remote delete starts.
func (s *Store) ExpireSegments(ctx context.Context, workspaceID, projectID, streamID uuid.UUID, day time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE archive_segments SET state = 'REMOTE_EXPIRED', next_attempt_at = NULL, attempts = 0, updated_at = now()
		WHERE workspace_id = $1 AND project_id = $2 AND stream_id = $3 AND day = $4 AND state IN ('CATALOGED','HOT_EVICTABLE')`,
		workspaceID, projectID, streamID, day)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ArchivedKeys reports, for orphan cleanup, whether each object key of a provider is in the
// catalog in a state that still owns it.
func (s *Store) ArchivedKeys(ctx context.Context, providerID uuid.UUID, keys []string) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT object_key FROM archive_segments
		WHERE provider_id = $1 AND object_key = ANY($2) AND state <> 'DELETED'`, providerID, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bool, len(keys))
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// ArchiveSummary counts segments by state, for the admin storage page and metrics.
func (s *Store) ArchiveSummary(ctx context.Context) (map[string]struct{ Count, Bytes int64 }, error) {
	rows, err := s.pool.Query(ctx, `SELECT state, count(*), COALESCE(sum(bytes), 0)::bigint FROM archive_segments GROUP BY state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{ Count, Bytes int64 }{}
	for rows.Next() {
		var st string
		var v struct{ Count, Bytes int64 }
		if err := rows.Scan(&st, &v.Count, &v.Bytes); err != nil {
			return nil, err
		}
		out[st] = v
	}
	return out, rows.Err()
}

// LastCatalogedAt is when a segment last passed verification and became readable; nil when none did.
func (s *Store) LastCatalogedAt(ctx context.Context) (*time.Time, error) {
	var t *time.Time
	err := s.pool.QueryRow(ctx, `SELECT max(cataloged_at) FROM archive_segments`).Scan(&t)
	return t, err
}

// ProviderInUse reports whether any segment, tombstones included, points at the provider.
func (s *Store) ProviderInUse(ctx context.Context, id uuid.UUID) (bool, error) {
	var used bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM archive_segments WHERE provider_id = $1)`, id).Scan(&used)
	return used, err
}

// RetentionScopeExists reports whether the workspace, project or stream a policy names exists.
func (s *Store) RetentionScopeExists(ctx context.Context, scopeType string, id uuid.UUID) (bool, error) {
	var table string
	switch scopeType {
	case "instance":
		return id == InstanceScope, nil
	case "workspace":
		table = "workspaces"
	case "project":
		table = "projects"
	case "stream":
		table = "streams"
	default:
		return false, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+table+` WHERE id = $1)`, id).Scan(&ok)
	return ok, err
}

// PendingSegmentIDs returns the ids among ids whose segment may still promote its temporary
// object (STAGING, UPLOADING or VERIFIED), so orphan cleanup leaves that object alone.
func (s *Store) PendingSegmentIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM archive_segments
		WHERE id = ANY($1) AND state IN ('STAGING','UPLOADING','VERIFIED')`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[uuid.UUID]bool, len(ids))
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
