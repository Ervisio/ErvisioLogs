//go:build integration

package ingest

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
	"github.com/Ervisio/ErvisioLogs/backend/internal/db"
	"github.com/Ervisio/ErvisioLogs/backend/internal/migrate"
	"github.com/Ervisio/ErvisioLogs/backend/internal/wal"
	"github.com/Ervisio/ErvisioLogs/backend/migrations"
)

// openCH connects to the local ClickHouse of the dev stack and brings its schema up to date.
func openCH(t *testing.T) driver.Conn {
	t.Helper()
	addr := os.Getenv("ERVISIO_LOGS_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("ERVISIO_LOGS_CLICKHOUSE_ADDR not set")
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") && !strings.HasPrefix(addr, "localhost:") {
		t.Fatal("refusing to write to a non-local ClickHouse")
	}
	ch, err := db.OpenClickHouse(config.ClickHouse{Addr: addr, Database: os.Getenv("ERVISIO_LOGS_CLICKHOUSE_DATABASE"),
		User: os.Getenv("ERVISIO_LOGS_CLICKHOUSE_USER"), Password: os.Getenv("ERVISIO_LOGS_CLICKHOUSE_PASSWORD")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ch.Close() })
	ms, err := migrate.Load(migrations.FS, migrations.ClickHouseDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Up(context.Background(), migrate.ClickHouse{Conn: ch}, ms); err != nil {
		t.Fatal(err)
	}
	return ch
}

func countProject(t *testing.T, ch driver.Conn, project uuid.UUID) uint64 {
	t.Helper()
	var n uint64
	if err := ch.QueryRow(context.Background(), "SELECT count() FROM events WHERE project_id = ?", project).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func testEvents(project uuid.UUID, n int) []Event {
	now := time.Now().UTC().Truncate(time.Millisecond)
	out := make([]Event, n)
	for i := range out {
		out[i] = Event{WorkspaceID: uuid.New(), ProjectID: project, EnvironmentID: uuid.New(), StreamID: uuid.New(),
			EventID: NewEventID(), Timestamp: now, IngestedAt: now, SeverityNumber: 9, SeverityText: "INFO",
			Message: "hello", Attributes: []byte(`{"player":{"id":7}}`), SchemaVersion: SchemaVersion}
	}
	return out
}

// A process that inserted a group and died before committing the WAL re-sends the group on start.
// ClickHouse must drop the copy because the deduplication token is the same.
func TestWriterResendAfterCrashIsDeduplicated(t *testing.T) {
	ch := openCH(t)
	dir := t.TempDir()
	project := uuid.New()

	log1, err := wal.Open(wal.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		rec, err := EncodeRecord(testEvents(project, 4))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := log1.Append(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	recs, err := log1.ReadFrom(log1.Checkpoint(), 100, 1<<30)
	if err != nil || len(recs) != 3 {
		t.Fatalf("read %d records: %v", len(recs), err)
	}

	// The first process: in-flight file, insert, then a crash before Commit.
	w1 := &Writer{Log: log1, CH: ch, Dir: dir, Logger: slog.Default()}
	w1.defaults()
	if w1.spoolID, err = loadSpoolID(dir); err != nil {
		t.Fatal(err)
	}
	first, last := recs[0].Seq, recs[len(recs)-1].Seq
	if err := w1.writeInflight(first, last); err != nil {
		t.Fatal(err)
	}
	body, _, _ := w1.encode(recs)
	if err := w1.insert(ctx, body, w1.tokenFor(first, last)); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if n := countProject(t, ch, project); n != 12 {
		t.Fatalf("after first insert: %d rows, want 12", n)
	}
	if err := log1.Close(); err != nil {
		t.Fatal(err)
	}

	// The restarted process.
	log2, err := wal.Open(wal.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer log2.Close()
	if log2.Checkpoint() != first {
		t.Fatalf("checkpoint %d after restart, want %d", log2.Checkpoint(), first)
	}
	hub := NewHub(100)
	w2 := &Writer{Log: log2, CH: ch, Dir: dir, Hub: hub, Logger: slog.Default()}
	rctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- w2.Run(rctx) }()
	deadline := time.Now().Add(15 * time.Second)
	for log2.Checkpoint() <= last && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if log2.Checkpoint() != last+1 {
		t.Fatalf("checkpoint %d, want %d", log2.Checkpoint(), last+1)
	}
	if n := countProject(t, ch, project); n != 12 {
		t.Fatalf("after re-send: %d rows, want 12 (the copy was not deduplicated)", n)
	}
	if _, err := os.Stat(dir + "/" + inflightName); !os.IsNotExist(err) {
		t.Fatalf("in-flight file still present: %v", err)
	}
	if w2.Stats.EventsWritten.Load() != 12 {
		t.Fatalf("events written %d", w2.Stats.EventsWritten.Load())
	}
}

// New records after a restart go through the normal loop and keep their attributes intact.
func TestWriterStoresEventsWithAttributes(t *testing.T) {
	ch := openCH(t)
	dir := t.TempDir()
	project := uuid.New()
	log, err := wal.Open(wal.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	w := &Writer{Log: log, CH: ch, Dir: dir, Logger: slog.Default(), Linger: 10 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	rec, _ := EncodeRecord(testEvents(project, 2))
	if _, err := log.Append(ctx, rec); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for countProject(t, ch, project) < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := ch.QueryRow(context.Background(),
		"SELECT toInt64OrZero(toString(attributes.player.id)) FROM events WHERE project_id = ? LIMIT 1", project).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id != 7 {
		t.Fatalf("attributes.player.id = %d", id)
	}
}
