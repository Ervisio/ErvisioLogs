//go:build integration

package archive

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
	"github.com/Ervisio/ErvisioLogs/backend/internal/db"
	"github.com/Ervisio/ErvisioLogs/backend/internal/migrate"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
	"github.com/Ervisio/ErvisioLogs/backend/migrations"
)

// archiverEnv connects to the dev stack, wipes it and returns an archiver writing to a local
// provider. ERVISIO_LOGS_ARCHIVE_CACHE_DIR must be the host directory that the dev ClickHouse
// mounts at user_files/ervisio-archive (deploy/compose.dev.yaml).
func archiverEnv(t *testing.T) (*Archiver, Provider, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	dsn, addr := os.Getenv("ERVISIO_LOGS_POSTGRES_DSN"), os.Getenv("ERVISIO_LOGS_CLICKHOUSE_ADDR")
	cacheDir, httpURL := os.Getenv("ERVISIO_LOGS_ARCHIVE_CACHE_DIR"), os.Getenv("ERVISIO_LOGS_CLICKHOUSE_HTTP_URL")
	if dsn == "" || addr == "" || cacheDir == "" || httpURL == "" {
		t.Fatal("needs ERVISIO_LOGS_POSTGRES_DSN, _CLICKHOUSE_ADDR, _CLICKHOUSE_HTTP_URL and _ARCHIVE_CACHE_DIR (make test-integration)")
	}
	if !strings.Contains(dsn, "@127.0.0.1:") || !strings.HasPrefix(addr, "127.0.0.1:") || !strings.HasPrefix(httpURL, "http://127.0.0.1:") {
		t.Fatal("refusing to wipe a non-local stack")
	}
	pool, err := db.OpenPostgres(ctx, config.Postgres{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	pms, err := migrate.Load(migrations.FS, migrations.PostgresDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Up(ctx, migrate.Postgres{Pool: pool}, pms); err != nil {
		t.Fatal(err)
	}
	chc := config.ClickHouse{Addr: addr, Database: os.Getenv("ERVISIO_LOGS_CLICKHOUSE_DATABASE"),
		User: os.Getenv("ERVISIO_LOGS_CLICKHOUSE_USER"), Password: os.Getenv("ERVISIO_LOGS_CLICKHOUSE_PASSWORD")}
	ch, err := db.OpenClickHouse(chc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ch.Close() })
	cms, err := migrate.Load(migrations.FS, migrations.ClickHouseDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Up(ctx, migrate.ClickHouse{Conn: ch}, cms); err != nil {
		t.Fatal(err)
	}
	// Other suites leave rows behind; the archiver scans the whole table.
	if err := ch.Exec(ctx, "TRUNCATE TABLE events"); err != nil {
		t.Fatal(err)
	}

	st := store.New(pool)
	cfg, _ := json.Marshal(LocalConfig{Root: t.TempDir()})
	sp, err := st.CreateProvider(ctx, "local test", "local", cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetActiveProvider(ctx, &sp.ID); err != nil {
		t.Fatal(err)
	}
	p, err := Open("local", cfg, "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	a := &Archiver{
		Store: st, CH: ch,
		HTTP:      NewCHHTTP(httpURL, chc.Database, chc.User, chc.Password),
		Cfg:       config.Archive{CacheDir: cacheDir, ClickHousePath: "ervisio-archive", Grace: time.Hour, LateInterval: 0},
		Log:       slog.New(slog.NewTextHandler(os.Stderr, nil)),
		CutoffLag: time.Millisecond,
	}
	t.Cleanup(a.Close)
	return a, p, pool
}

// insertRows writes n events for one stream at day + offset + i ms, through the server default
// for inserted_at like the real writer.
func insertRows(t *testing.T, ch driver.Conn, ws, proj, stream uuid.UUID, at time.Time, n int, tag string) {
	t.Helper()
	err := ch.Exec(context.Background(), `INSERT INTO events (workspace_id, project_id, environment_id, stream_id, event_id,
			timestamp, severity_number, severity_text, event_name, message, source_service, source_host, source_resource,
			trace_id, span_id, attributes)
		SELECT toUUID(?), toUUID(?), toUUID(?), toUUID(?), concat(?, toString(number)),
			toDateTime64(?, 3, 'UTC') + toIntervalMillisecond(number), 9, 'info', 'test',
			concat('message ', toString(number)), 'svc', 'host', '', '', '', ?
		FROM numbers(?)`, ws.String(), proj.String(), uuid.Nil.String(), stream.String(), tag+"-", chTime(at), `{"tag":"`+tag+`"}`, n)
	if err != nil {
		t.Fatal(err)
	}
}

func cataloged(t *testing.T, a *Archiver, ws, proj, stream uuid.UUID, day time.Time) []store.Segment {
	t.Helper()
	segs, err := a.Store.StreamDaySegments(context.Background(), ws, proj, stream, day)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Segment
	for _, s := range segs {
		if s.State == store.SegCataloged {
			out = append(out, s)
		}
	}
	return out
}

// setRetention replaces the instance policy.
func setRetention(t *testing.T, a *Archiver, local, global int32, cacheBytes int64) {
	t.Helper()
	high, low := int16(90), int16(75)
	if _, err := a.Store.PutRetentionPolicy(context.Background(), store.RetentionPolicy{ScopeType: "instance", ScopeID: store.InstanceScope,
		LocalAgeHours: &local, GlobalAgeHours: &global, CacheMaxBytes: &cacheBytes, HighPct: &high, LowPct: &low}); err != nil {
		t.Fatal(err)
	}
}

func hotCount(t *testing.T, a *Archiver, stream uuid.UUID, day time.Time) uint64 {
	t.Helper()
	var n uint64
	if err := a.CH.QueryRow(context.Background(), `SELECT count() FROM events WHERE stream_id = toUUID(?) AND toDate(timestamp) = toDate(?)`,
		stream.String(), day.Format(time.DateOnly)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestArchiverPass(t *testing.T) {
	a, p, pool := archiverEnv(t)
	ctx := context.Background()
	// Keep every test day inside local retention: this test is about archiving only.
	setRetention(t, a, 1000, 2000, 1<<40)
	now, err := a.chNow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	today := now.Truncate(24 * time.Hour)
	dayA, dayB := today.AddDate(0, 0, -3), today.AddDate(0, 0, -2)
	ws, proj, s1, s2 := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	insertRows(t, a.CH, ws, proj, s1, dayA.Add(time.Hour), 50, "a")
	insertRows(t, a.CH, ws, proj, s1, dayB.Add(time.Hour), 10, "b")
	insertRows(t, a.CH, ws, proj, s2, dayA.Add(2*time.Hour), 7, "c")
	// Today is still open: never archived.
	insertRows(t, a.CH, ws, proj, s1, now.Add(-time.Minute), 5, "today")
	time.Sleep(20 * time.Millisecond)

	rep, err := a.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Ran || rep.Started != 3 || rep.Cataloged != 3 || rep.Failed != 0 {
		t.Fatalf("first pass: %+v", rep)
	}
	for _, c := range []struct {
		stream uuid.UUID
		day    time.Time
		rows   int64
	}{{s1, dayA, 50}, {s1, dayB, 10}, {s2, dayA, 7}} {
		segs := cataloged(t, a, ws, proj, c.stream, c.day)
		if len(segs) != 1 {
			t.Fatalf("%s %s: %d cataloged segments", c.stream, c.day, len(segs))
		}
		g := segs[0]
		if *g.RowCount != c.rows || g.Generation != 1 || g.CutoffFrom.Unix() != 0 || g.MinEventTime == nil {
			t.Fatalf("segment %+v", g)
		}
		if g.ObjectKey != ObjectKey(ws, proj, c.stream, c.day, 1, g.ID) {
			t.Fatalf("key %s", g.ObjectKey)
		}
		if err := Verify(ctx, p, g.ObjectKey, *g.Bytes, g.Checksum); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Stat(ctx, tmpKey(g.ID)); !errors.Is(err, ErrNotExist) {
			t.Fatalf("tmp object left behind: %v", err)
		}
		if _, err := os.Stat(a.SegmentPath(g.ID)); err != nil {
			t.Fatalf("segment not kept in cache: %v", err)
		}
		if _, err := os.Stat(a.stagingPath(g.ID)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("staging file left behind: %v", err)
		}
	}

	// ClickHouse reads the cached file with the documented layout.
	g := cataloged(t, a, ws, proj, s1, dayA)[0]
	var n uint64
	var tag, gotWS string
	if err := a.CH.QueryRow(ctx, "SELECT count(), any(JSONExtractString(attributes, 'tag')), any(workspace_id) FROM file('ervisio-archive/segments/"+
		g.ID.String()+".parquet', 'Parquet')").Scan(&n, &tag, &gotWS); err != nil {
		t.Fatal(err)
	}
	if n != 50 || tag != "a" || gotWS != ws.String() {
		t.Fatalf("parquet read back: %d %q %q", n, tag, gotWS)
	}

	// Nothing new: nothing to do.
	if rep, err := a.RunOnce(ctx); err != nil || rep.Started != 0 {
		t.Fatalf("idle pass: %+v %v", rep, err)
	}

	// Late rows for a covered day start generation 2 over the following range only.
	insertRows(t, a.CH, ws, proj, s1, dayA.Add(5*time.Hour), 3, "late")
	time.Sleep(20 * time.Millisecond)
	if rep, err := a.RunOnce(ctx); err != nil || rep.Started != 1 || rep.Cataloged != 1 {
		t.Fatalf("late pass: %+v %v", rep, err)
	}
	segs := cataloged(t, a, ws, proj, s1, dayA)
	if len(segs) != 2 || segs[1].Generation != 2 || *segs[1].RowCount != 3 || !segs[1].CutoffFrom.Equal(segs[0].CutoffTo) {
		t.Fatalf("late generation: %+v", segs)
	}

	// The late interval throttles further generations.
	a.Cfg.LateInterval = time.Hour
	insertRows(t, a.CH, ws, proj, s1, dayA.Add(6*time.Hour), 2, "later")
	time.Sleep(20 * time.Millisecond)
	if rep, err := a.RunOnce(ctx); err != nil || rep.Started != 0 {
		t.Fatalf("throttled pass: %+v %v", rep, err)
	}
	a.Cfg.LateInterval = 0

	// A crash after STAGING: the pass resumes the segment.
	dayC := today.AddDate(0, 0, -4)
	insertRows(t, a.CH, ws, proj, s2, dayC, 4, "resume")
	s1Day := cataloged(t, a, ws, proj, s1, dayA)
	// Catch the "later" rows first, so the next pass only has the cases below.
	if _, err := a.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got := cataloged(t, a, ws, proj, s1, dayA); len(got) != len(s1Day)+1 {
		t.Fatalf("later rows not archived: %d", len(got))
	}
	if got := cataloged(t, a, ws, proj, s2, dayC); len(got) != 1 || *got[0].RowCount != 4 {
		t.Fatalf("day C: %+v", got)
	}

	dayD := today.AddDate(0, 0, -5)
	insertRows(t, a.CH, ws, proj, s2, dayD, 6, "crash")
	time.Sleep(20 * time.Millisecond)
	cut := now.Add(time.Hour) // after every row above
	active, err := a.Store.ActiveProvider(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(day time.Time) store.Segment {
		seg, err := a.Store.StartSegment(ctx, store.NewSegment{WorkspaceID: ws, ProjectID: proj, StreamID: s2, Day: day,
			ProviderID: active.ID, CutoffTo: cut, SchemaVersion: SchemaVersion,
			KeyFor: func(id uuid.UUID, gen int32) string { return ObjectKey(ws, proj, s2, day, gen, id) }})
		if err != nil {
			t.Fatal(err)
		}
		return seg
	}
	staged := mk(dayD)
	// UPLOADING whose staged file is gone (lost cache): FAILED at once, and the same pass starts
	// a new generation that picks the rows up again.
	dayE := today.AddDate(0, 0, -6)
	insertRows(t, a.CH, ws, proj, s2, dayE, 8, "lost")
	lost := mk(dayE)
	if err := a.Store.AdvanceSegment(ctx, lost.ID, store.SegStaging, store.SegUploading,
		&store.SegmentStats{Checksum: make([]byte, 32), Bytes: 10, RowCount: 8}); err != nil {
		t.Fatal(err)
	}
	// The clock guard: cut is in the future of the ClickHouse clock.
	if _, err := a.RunOnce(ctx); err == nil || !strings.Contains(err.Error(), "clock") {
		t.Fatalf("clock guard: %v", err)
	}
	// Pull the two hand-made cutoffs back into the past, as a real crash would have left them.
	if _, err := pool.Exec(ctx, `UPDATE archive_segments SET cutoff_to = now() WHERE id = ANY($1)`,
		[]uuid.UUID{staged.ID, lost.ID}); err != nil {
		t.Fatal(err)
	}
	rep, err = a.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Resumed != 2 || rep.Failed != 1 {
		t.Fatalf("resume pass: %+v", rep)
	}
	if got := cataloged(t, a, ws, proj, s2, dayD); len(got) != 1 || got[0].ID != staged.ID || *got[0].RowCount != 6 {
		t.Fatalf("resumed segment: %+v", got)
	}
	f, err := a.Store.SegmentByID(ctx, lost.ID)
	if err != nil || f.State != store.SegFailed || f.LastError == nil {
		t.Fatalf("lost segment: %+v %v", f, err)
	}
	if got := cataloged(t, a, ws, proj, s2, dayE); len(got) != 1 || got[0].Generation != 2 || *got[0].RowCount != 8 {
		t.Fatalf("regenerated segment: %+v", got)
	}
}

// TestArchiverRetention runs the 3 day local / 14 day global policy on a simulated history:
// expiry everywhere past 14 days, hot eviction of covered rows past 3 days, uncovered late rows
// kept, and the cache budget with its 503 flag.
func TestArchiverRetention(t *testing.T) {
	a, p, _ := archiverEnv(t)
	ctx := context.Background()
	now, err := a.chNow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	today := now.Truncate(24 * time.Hour)
	recent, old, expired := today.AddDate(0, 0, -1), today.AddDate(0, 0, -5), today.AddDate(0, 0, -20)
	ws, proj, s1 := uuid.New(), uuid.New(), uuid.New()
	insertRows(t, a.CH, ws, proj, s1, recent.Add(time.Hour), 11, "recent")
	insertRows(t, a.CH, ws, proj, s1, old.Add(time.Hour), 12, "old")
	insertRows(t, a.CH, ws, proj, s1, expired.Add(time.Hour), 13, "expired")
	insertRows(t, a.CH, ws, proj, s1, now.Add(-time.Minute), 5, "today")
	time.Sleep(20 * time.Millisecond)

	// Archive the whole history first, as if the policy had been longer until now.
	setRetention(t, a, 1000, 2000, 1<<40)
	if rep, err := a.RunOnce(ctx); err != nil || rep.Cataloged != 3 || rep.Maintain.HotEvictedDays != 0 {
		t.Fatalf("archive pass: %+v %v", rep, err)
	}
	expSeg := cataloged(t, a, ws, proj, s1, expired)[0]
	oldSeg := cataloged(t, a, ws, proj, s1, old)[0]

	// A late row for the old day, not yet archived (late interval), must survive eviction.
	insertRows(t, a.CH, ws, proj, s1, old.Add(3*time.Hour), 1, "late")
	a.Cfg.LateInterval = time.Hour
	setRetention(t, a, 72, 336, 1<<40)
	rep, err := a.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m := rep.Maintain
	if rep.Started != 0 || m.HotExpiredDays != 1 || m.SegmentsExpired != 1 || m.SegmentsDeleted != 1 || m.HotEvictedDays != 1 || m.OverCapacity {
		t.Fatalf("retention pass: %+v", rep)
	}
	if n := hotCount(t, a, s1, expired); n != 0 {
		t.Fatalf("expired day still has %d hot rows", n)
	}
	if n := hotCount(t, a, s1, old); n != 1 {
		t.Fatalf("old day: %d hot rows, want only the uncovered late row", n)
	}
	if n := hotCount(t, a, s1, recent); n != 11 {
		t.Fatalf("recent day: %d hot rows", n)
	}
	if g, err := a.Store.SegmentByID(ctx, expSeg.ID); err != nil || g.State != store.SegDeleted || g.DeletedAt == nil {
		t.Fatalf("expired segment: %+v %v", g, err)
	}
	if _, err := p.Stat(ctx, expSeg.ObjectKey); !errors.Is(err, ErrNotExist) {
		t.Fatalf("expired object still on the provider: %v", err)
	}
	if _, err := os.Stat(a.SegmentPath(expSeg.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired segment still cached: %v", err)
	}
	if g, err := a.Store.SegmentByID(ctx, oldSeg.ID); err != nil || g.State != store.SegHotEvictable || g.HotEvictedAt == nil {
		t.Fatalf("evicted segment: %+v %v", g, err)
	}
	// The tombstone keeps the expired day from being archived again.
	insertRows(t, a.CH, ws, proj, s1, expired.Add(2*time.Hour), 2, "zombie")
	if rep, err := a.RunOnce(ctx); err != nil || rep.Started != 0 || rep.Maintain.HotExpiredDays != 1 {
		t.Fatalf("zombie pass: %+v %v", rep, err)
	}

	// A tiny budget: the cached files go, then the covered recent day, and the uncovered rows
	// that are left still exceed the cap, so ingest must refuse.
	setRetention(t, a, 72, 336, 1)
	rep, err = a.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m = rep.Maintain
	if m.CacheFilesEvicted == 0 || m.HotEvictedDays != 1 || !m.OverCapacity || !a.OverCapacity() {
		t.Fatalf("budget pass: %+v", rep)
	}
	if n := hotCount(t, a, s1, recent); n != 0 {
		t.Fatalf("recent day after budget eviction: %d hot rows", n)
	}
	if files, _, err := a.cacheFiles(); err != nil || len(files) != 0 {
		t.Fatalf("cache after budget eviction: %d %v", len(files), err)
	}
	setRetention(t, a, 72, 336, 1<<40)
	if _, err := a.RunOnce(ctx); err != nil || a.OverCapacity() {
		t.Fatalf("capacity flag not cleared: %v", err)
	}
}
