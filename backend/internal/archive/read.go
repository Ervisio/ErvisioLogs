package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

// ReadSource is a cached segment that ClickHouse can read with file().
type ReadSource struct {
	SegmentID uuid.UUID
	// FileExpr is a file(...) table function over the cached copy. It is built from the
	// configured ClickHouse path and the segment id only, never from request input.
	FileExpr string
}

// remoteSchema is the Parquet layout of SchemaVersion 1 (see exportSQL), given explicitly so
// ClickHouse does not infer types from each file.
const remoteSchema = `workspace_id String, project_id String, environment_id String, stream_id String,
	event_id String, timestamp DateTime64(3, 'UTC'), ingested_at DateTime64(3, 'UTC'), inserted_at DateTime64(3, 'UTC'),
	severity_number UInt8, severity_text String, event_name String, message String, source_service String,
	source_host String, source_resource String, trace_id String, span_id String, attributes String, schema_version UInt16`

// RemoteSelect turns one cached segment into rows with the hot table's column types, so the
// search WHERE clause applies unchanged to both tiers.
func (s ReadSource) RemoteSelect() string {
	return `SELECT toUUID(workspace_id) AS workspace_id, toUUID(project_id) AS project_id,
		toUUID(environment_id) AS environment_id, toUUID(stream_id) AS stream_id, event_id, timestamp, ingested_at,
		severity_number, severity_text, event_name, message, source_service, source_host, source_resource,
		trace_id, span_id, CAST(attributes, 'JSON') AS attributes FROM ` + s.FileExpr
}

// ReadSources returns the evicted segments of a search and makes sure each has a local copy,
// downloading and verifying the ones that are missing. A segment that cannot be fetched is left
// out and counted in missing, so the caller can mark the result partial.
func (a *Archiver) ReadSources(ctx context.Context, ws, proj uuid.UUID, streams []uuid.UUID, from, to time.Time) (srcs []ReadSource, missing int, err error) {
	segs, err := a.Store.ReadableSegments(ctx, ws, proj, streams, from, to, true)
	if err != nil {
		return nil, 0, err
	}
	for _, seg := range segs {
		if seg.MaxEventTime != nil && seg.MaxEventTime.Before(from) || seg.MinEventTime != nil && !seg.MinEventTime.Before(to) {
			continue
		}
		if err := a.ensureCached(ctx, seg); err != nil {
			if ctx.Err() != nil {
				return nil, 0, ctx.Err()
			}
			a.Log.Warn("archive segment unavailable", "segment", seg.ID, "err", err)
			missing++
			continue
		}
		srcs = append(srcs, ReadSource{
			SegmentID: seg.ID,
			FileExpr: fmt.Sprintf("file('%s/segments/%s.parquet', 'Parquet', '%s')",
				a.Cfg.ClickHousePath, seg.ID, strings.ReplaceAll(remoteSchema, "'", "''")),
		})
	}
	return srcs, missing, nil
}

// ensureCached downloads a segment into the cache unless it is there, and touches its mtime so
// the budget's LRU keeps segments people read.
func (a *Archiver) ensureCached(ctx context.Context, seg store.Segment) error {
	path := a.SegmentPath(seg.ID)
	now := time.Now()
	if err := os.Chtimes(path, now, now); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	p, err := a.provider(ctx, seg.ProviderID)
	if err != nil {
		return err
	}
	rc, err := p.Get(ctx, seg.ObjectKey)
	if err != nil {
		return err
	}
	defer rc.Close()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), rc)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("download %s: %w", seg.ObjectKey, err)
	}
	if seg.Bytes != nil && n != *seg.Bytes || !bytes.Equal(h.Sum(nil), seg.Checksum) {
		return fmt.Errorf("%w: downloaded %s does not match the catalog", ErrVerify, seg.ObjectKey)
	}
	// ClickHouse runs as another user and reads the shared mount.
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
