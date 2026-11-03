//go:build integration

package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
	"github.com/Ervisio/ErvisioLogs/backend/internal/db"
	"github.com/Ervisio/ErvisioLogs/backend/internal/migrate"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
	"github.com/Ervisio/ErvisioLogs/backend/migrations"
)

func newStore(t *testing.T) (*store.Store, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("ERVISIO_LOGS_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ERVISIO_LOGS_POSTGRES_DSN not set")
	}
	if !strings.Contains(dsn, "@127.0.0.1:") && !strings.Contains(dsn, "@localhost:") {
		t.Fatal("refusing to reset a non-local database")
	}
	ctx := context.Background()
	pool, err := db.OpenPostgres(ctx, config.Postgres{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	ms, err := migrate.Load(migrations.FS, migrations.PostgresDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Up(ctx, migrate.Postgres{Pool: pool}, ms); err != nil {
		t.Fatal(err)
	}
	return store.New(pool), pool
}

func TestProvidersCatalog(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if _, err := s.ActiveProvider(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("no provider yet: %v", err)
	}
	secret := "s3.json"
	a, err := s.CreateProvider(ctx, "primary", "s3", json.RawMessage(`{"bucket":"b"}`), &secret)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateProvider(ctx, "box", "sftp", json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	bad := "../x"
	if _, err := s.CreateProvider(ctx, "bad", "s3", nil, &bad); err == nil {
		t.Fatal("a path as secret_file must be rejected")
	}
	if err := s.SetActiveProvider(ctx, &a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetActiveProvider(ctx, &b.ID); err != nil {
		t.Fatal(err)
	}
	act, err := s.ActiveProvider(ctx)
	if err != nil || act.ID != b.ID {
		t.Fatalf("active = %v, %v; want box", act.ID, err)
	}
	missing := uuid.New()
	if err := s.SetActiveProvider(ctx, &missing); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown provider: %v", err)
	}
	if act, _ := s.ActiveProvider(ctx); act.ID != b.ID {
		t.Fatal("a failed switch must keep the previous provider active")
	}

	if err := s.SetProviderStatus(ctx, a.ID, errors.New("dial tcp: refused")); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ProviderByID(ctx, a.ID)
	if got.Status != "error" || got.LastError == nil || got.LastCheckedAt == nil {
		t.Fatalf("status not recorded: %+v", got)
	}
	got, err = s.UpdateProvider(ctx, a.ID, "primary-2", json.RawMessage(`{"bucket":"c"}`), &secret)
	if err != nil || got.Status != "unknown" || got.LastError != nil {
		t.Fatalf("update must reset health: %+v %v", got, err)
	}

	// A provider that holds segments cannot be removed.
	if _, err := s.StartSegment(ctx, newSeg(a.ID, day(0), time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProvider(ctx, a.ID); !errors.Is(err, store.ErrProviderInUse) {
		t.Fatalf("delete in use: %v", err)
	}
	if err := s.DeleteProvider(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProvider(ctx, b.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestRetentionPolicies(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	ps, err := s.RetentionPolicies(ctx)
	if err != nil || len(ps) != 1 || ps[0].ScopeType != "instance" || *ps[0].LocalAgeHours != 72 || *ps[0].GlobalAgeHours != 336 {
		t.Fatalf("instance default missing: %+v %v", ps, err)
	}
	proj := uuid.New()
	l, g := int32(24), int32(48)
	if _, err := s.PutRetentionPolicy(ctx, store.RetentionPolicy{ScopeType: "project", ScopeID: proj, LocalAgeHours: &l, GlobalAgeHours: &g}); err != nil {
		t.Fatal(err)
	}
	g2 := int32(12)
	if _, err := s.PutRetentionPolicy(ctx, store.RetentionPolicy{ScopeType: "project", ScopeID: proj, LocalAgeHours: &l, GlobalAgeHours: &g2}); err == nil {
		t.Fatal("global shorter than local must be rejected")
	}
	var cache int64 = 1 << 30
	if _, err := s.PutRetentionPolicy(ctx, store.RetentionPolicy{ScopeType: "project", ScopeID: proj, CacheMaxBytes: &cache}); err == nil {
		t.Fatal("cache size only applies to the instance row")
	}
	if err := s.DeleteRetentionPolicy(ctx, "instance", store.InstanceScope); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("instance row must stay: %v", err)
	}
	if err := s.DeleteRetentionPolicy(ctx, "project", proj); err != nil {
		t.Fatal(err)
	}
	if ps, _ := s.RetentionPolicies(ctx); len(ps) != 1 {
		t.Fatalf("override not removed: %d rows", len(ps))
	}
}

var (
	wsID, projID, streamID = uuid.New(), uuid.New(), uuid.New()
)

func day(offset int) time.Time {
	return time.Date(2026, 10, 1+offset, 0, 0, 0, 0, time.UTC)
}

func newSeg(provider uuid.UUID, d time.Time, cutoff time.Time) store.NewSegment {
	return store.NewSegment{WorkspaceID: wsID, ProjectID: projID, StreamID: streamID, Day: d, ProviderID: provider,
		CutoffTo: cutoff, SchemaVersion: 1,
		KeyFor: func(id uuid.UUID, gen int32) string {
			return d.Format("2006/01/02") + "/g" + string(rune('0'+gen)) + "-" + id.String()
		}}
}

func TestSegmentLifecycle(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	p, err := s.CreateProvider(ctx, "local", "local", json.RawMessage(`{"root":"/tmp/x"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	d := day(0)
	t1 := time.Date(2026, 10, 2, 1, 0, 0, 0, time.UTC)

	g1, err := s.StartSegment(ctx, newSeg(p.ID, d, t1))
	if err != nil {
		t.Fatal(err)
	}
	if g1.Generation != 1 || g1.State != store.SegStaging || !g1.CutoffFrom.Equal(time.Unix(0, 0)) || !g1.Day.Equal(d) {
		t.Fatalf("first generation: %+v", g1)
	}
	if !strings.Contains(g1.ObjectKey, g1.ID.String()) {
		t.Fatalf("key %q must carry the segment id", g1.ObjectKey)
	}
	// One generation in progress blocks the next.
	if _, err := s.StartSegment(ctx, newSeg(p.ID, d, t1.Add(time.Hour))); !errors.Is(err, store.ErrSegmentBusy) {
		t.Fatalf("second start while staging: %v", err)
	}

	// Coverage stays empty until the generation is cataloged.
	if cs, _ := s.Coverages(ctx, day(5)); len(cs) != 0 {
		t.Fatalf("staging must not count as coverage: %+v", cs)
	}
	sum := make([]byte, 32)
	minT, maxT := d.Add(time.Hour), d.Add(23*time.Hour)
	if err := s.AdvanceSegment(ctx, g1.ID, store.SegStaging, store.SegUploading,
		&store.SegmentStats{Checksum: sum, Bytes: 1000, RowCount: 10, MinEventTime: &minT, MaxEventTime: &maxT}); err != nil {
		t.Fatal(err)
	}
	if err := s.AdvanceSegment(ctx, g1.ID, store.SegStaging, store.SegUploading, nil); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale transition must conflict: %v", err)
	}
	for _, step := range [][2]string{{store.SegUploading, store.SegVerified}, {store.SegVerified, store.SegCataloged}} {
		if err := s.AdvanceSegment(ctx, g1.ID, step[0], step[1], nil); err != nil {
			t.Fatal(step, err)
		}
	}
	g1, _ = s.SegmentByID(ctx, g1.ID)
	if g1.CatalogedAt == nil || *g1.RowCount != 10 {
		t.Fatalf("cataloged segment: %+v", g1)
	}

	// Late rows: generation 2 starts where generation 1 ended.
	t2 := t1.Add(7 * time.Hour)
	g2, err := s.StartSegment(ctx, newSeg(p.ID, d, t2))
	if err != nil {
		t.Fatal(err)
	}
	if g2.Generation != 2 || !g2.CutoffFrom.Equal(t1) {
		t.Fatalf("second generation: gen %d from %v, want 2 from %v", g2.Generation, g2.CutoffFrom, t1)
	}
	// It fails for good; generation 3 starts again from generation 1's end.
	for i := 0; i < 3; i++ {
		st, err := s.FailSegmentAttempt(ctx, g2.ID, errors.New("upload refused"), time.Now().Add(-time.Second), 3)
		if err != nil {
			t.Fatal(err)
		}
		if want := map[bool]string{true: store.SegFailed, false: store.SegStaging}[i == 2]; st != want {
			t.Fatalf("attempt %d: state %s, want %s", i+1, st, want)
		}
	}
	g3, err := s.StartSegment(ctx, newSeg(p.ID, d, t2))
	if err != nil {
		t.Fatal(err)
	}
	if g3.Generation != 3 || !g3.CutoffFrom.Equal(t1) {
		t.Fatalf("after a failure: gen %d from %v", g3.Generation, g3.CutoffFrom)
	}
	// A cutoff that does not move forward cannot start a generation.
	if err := s.AdvanceSegment(ctx, g3.ID, store.SegStaging, store.SegCataloged, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartSegment(ctx, newSeg(p.ID, d, t2)); !errors.Is(err, store.ErrSegmentBusy) {
		t.Fatalf("empty range: %v", err)
	}

	// Work queue: only in-progress, due segments.
	other, err := s.StartSegment(ctx, newSeg(p.ID, day(1), t2))
	if err != nil {
		t.Fatal(err)
	}
	due, err := s.SegmentsDue(ctx, []string{store.SegStaging, store.SegUploading, store.SegVerified}, time.Now(), 10)
	if err != nil || len(due) != 1 || due[0].ID != other.ID {
		t.Fatalf("due = %+v, %v", due, err)
	}
	if _, err := s.FailSegmentAttempt(ctx, other.ID, errors.New("x"), time.Now().Add(time.Hour), 5); err != nil {
		t.Fatal(err)
	}
	if due, _ := s.SegmentsDue(ctx, []string{store.SegStaging}, time.Now(), 10); len(due) != 0 {
		t.Fatal("a segment waiting for its retry time must not be due")
	}

	// Coverage of day 0 is the end of the cataloged range.
	cs, err := s.Coverages(ctx, day(5))
	if err != nil || len(cs) != 1 || !cs[0].CutoffTo.Equal(t2) || cs[0].Evicted || cs[0].Bytes != 1000 {
		t.Fatalf("coverage = %+v, %v", cs, err)
	}
	if cs, _ := s.Coverages(ctx, d); len(cs) != 0 {
		t.Fatal("before is exclusive")
	}

	// Readable segments: all cataloged ones, or only the evicted days.
	rs, err := s.ReadableSegments(ctx, wsID, projID, nil, d, d.Add(time.Hour), false)
	if err != nil || len(rs) != 2 || rs[0].Generation != 1 || rs[1].Generation != 3 {
		t.Fatalf("readable = %+v, %v", rs, err)
	}
	if rs, _ := s.ReadableSegments(ctx, wsID, projID, nil, d, d, true); len(rs) != 0 {
		t.Fatal("nothing is evicted yet")
	}
	if rs, _ := s.ReadableSegments(ctx, wsID, projID, []uuid.UUID{uuid.New()}, d, d, false); len(rs) != 0 {
		t.Fatal("stream filter ignored")
	}
	if rs, _ := s.ReadableSegments(ctx, uuid.New(), projID, nil, d, d, false); len(rs) != 0 {
		t.Fatal("another workspace must see nothing")
	}
	if err := s.MarkHotEvicted(ctx, wsID, projID, streamID, d); err != nil {
		t.Fatal(err)
	}
	if rs, _ := s.ReadableSegments(ctx, wsID, projID, nil, d, d, true); len(rs) != 2 || rs[0].State != store.SegHotEvictable {
		t.Fatalf("evicted readable = %+v", rs)
	}
	if cs, _ := s.Coverages(ctx, day(5)); !cs[0].Evicted {
		t.Fatal("coverage must report the eviction")
	}

	// Expiry hides the day from searches and blocks new generations.
	n, err := s.ExpireSegments(ctx, wsID, projID, streamID, d)
	if err != nil || n != 2 {
		t.Fatalf("expired %d, %v", n, err)
	}
	if rs, _ := s.ReadableSegments(ctx, wsID, projID, nil, d, d, false); len(rs) != 0 {
		t.Fatal("expired segments must not be read")
	}
	if _, err := s.StartSegment(ctx, newSeg(p.ID, d, t2.Add(time.Hour))); !errors.Is(err, store.ErrSegmentBusy) {
		t.Fatalf("expired day must not be archived again: %v", err)
	}
	for _, step := range [][2]string{{store.SegRemoteExpired, store.SegDeleting}, {store.SegDeleting, store.SegDeleted}} {
		if err := s.AdvanceSegment(ctx, g1.ID, step[0], step[1], nil); err != nil {
			t.Fatal(err)
		}
	}
	g1, _ = s.SegmentByID(ctx, g1.ID)
	if g1.DeletedAt == nil {
		t.Fatal("deleted_at not set")
	}
	keys, err := s.ArchivedKeys(ctx, p.ID, []string{g1.ObjectKey, g3.ObjectKey, "v1/stray.parquet"})
	if err != nil || keys[g1.ObjectKey] || !keys[g3.ObjectKey] || keys["v1/stray.parquet"] {
		t.Fatalf("archived keys = %v, %v", keys, err)
	}
	sum2, err := s.ArchiveSummary(ctx)
	if err != nil || sum2[store.SegDeleted].Count != 1 || sum2[store.SegFailed].Count != 1 {
		t.Fatalf("summary = %v, %v", sum2, err)
	}
}
