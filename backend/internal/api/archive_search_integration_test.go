//go:build integration

package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/archive"
	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

// Archived events stay searchable after the archiver evicts them from ClickHouse, the search
// reports the archive tier, and a segment that cannot be fetched makes the result partial.
func TestSearchReadsArchive(t *testing.T) {
	cacheDir, httpURL := os.Getenv("ERVISIO_LOGS_ARCHIVE_CACHE_DIR"), os.Getenv("ERVISIO_LOGS_CLICKHOUSE_HTTP_URL")
	if cacheDir == "" || httpURL == "" {
		t.Fatal("needs ERVISIO_LOGS_ARCHIVE_CACHE_DIR and ERVISIO_LOGS_CLICKHOUSE_HTTP_URL (make test-integration)")
	}
	e := newLogsEnv(t)
	ctx := context.Background()
	// The archiver scans the whole table, and rows from other suites name workspaces that the
	// wiped schema no longer has.
	if err := e.ch.Exec(ctx, "TRUNCATE TABLE events"); err != nil {
		t.Fatal(err)
	}
	admin := e.setupAdmin()
	ws := id(expect(t, admin.post("/workspaces", map[string]string{"name": "Server"}), 201), "id")
	proj := id(expect(t, admin.post("/projects", map[string]string{"workspace_id": ws, "name": "RP"}), 201), "id")
	tok := expect(t, admin.post("/tokens", map[string]any{"project_id": proj, "name": "srv", "scopes": []string{"logs:ingest", "logs:read"}}), 201).JSON()["token"].(string)

	ts := time.Now().UTC().Add(-48 * time.Hour).Truncate(24 * time.Hour).Add(10 * time.Hour)
	var events []map[string]any
	for i, lic := range []string{"lic:1", "lic:2", "lic:1"} {
		events = append(events, map[string]any{"id": "arch-" + string(rune('a'+i)), "timestamp": ts.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano),
			"level": "warn", "event": "bank.transfer", "message": "transfer " + lic, "attributes": map[string]any{"player": map[string]any{"license": lic}}})
	}
	expect(t, e.newClient().bearerJSON("POST", "/ingest/batch", tok, map[string]any{"events": events}), 202)
	query := map[string]any{"project_id": proj, "from": ts.Add(-time.Hour).Format(time.RFC3339), "to": ts.Add(time.Hour).Format(time.RFC3339),
		"filters": []map[string]any{{"field": "attributes.player.license", "op": "eq", "value": "lic:1"}}, "include_total": true}
	before := waitSearch(t, admin, tok, query, 2)

	// A real archiver against the same stack, keeping one day locally.
	st := store.New(e.pool)
	pcfg, _ := json.Marshal(archive.LocalConfig{Root: t.TempDir()})
	sp, err := st.CreateProvider(ctx, "local test", "local", pcfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetActiveProvider(ctx, &sp.ID); err != nil {
		t.Fatal(err)
	}
	local, global, budget := int32(24), int32(24*30), int64(1<<40)
	high, low := int16(90), int16(75)
	if _, err := st.PutRetentionPolicy(ctx, store.RetentionPolicy{ScopeType: "instance", ScopeID: store.InstanceScope,
		LocalAgeHours: &local, GlobalAgeHours: &global, CacheMaxBytes: &budget, HighPct: &high, LowPct: &low}); err != nil {
		t.Fatal(err)
	}
	arch := &archive.Archiver{Store: st, CH: e.ch,
		HTTP: archive.NewCHHTTP(httpURL, os.Getenv("ERVISIO_LOGS_CLICKHOUSE_DATABASE"), os.Getenv("ERVISIO_LOGS_CLICKHOUSE_USER"), os.Getenv("ERVISIO_LOGS_CLICKHOUSE_PASSWORD")),
		Cfg:  config.Archive{CacheDir: cacheDir, ClickHousePath: "ervisio-archive", Grace: time.Hour},
		Log:  slog.New(slog.NewTextHandler(testWriter{t}, nil)), CutoffLag: time.Millisecond}
	t.Cleanup(arch.Close)
	rep, err := arch.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Maintain.HotEvictedDays != 1 {
		t.Fatalf("pass %+v: want one evicted stream-day", rep)
	}
	var hot uint64
	if err := e.ch.QueryRow(ctx, "SELECT count() FROM events").Scan(&hot); err != nil || hot != 0 {
		t.Fatalf("hot rows after eviction: %d %v", hot, err)
	}

	// Without the archive tier the events are gone; with it they come back, filtered by a JSON
	// path, with the same ids and the same total.
	e.api.Logs.Archive = nil
	waitSearch(t, admin, tok, query, 0)
	e.api.Logs.Archive = arch
	// Drop the cached copy so the search downloads and verifies the segment.
	files, _ := filepath.Glob(filepath.Join(cacheDir, "segments", "*.parquet"))
	for _, f := range files {
		os.Remove(f)
	}
	after := waitSearch(t, admin, tok, query, 2)
	if got, want := messages(after), messages(before); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("archive search %v, hot search %v", got, want)
	}
	if after["total"].(float64) != 2 || after["partial"] != false {
		t.Fatalf("archive search total/partial: %v", after)
	}
	for _, item := range after["items"].([]any) {
		if item.(map[string]any)["tier"] != "archive" {
			t.Fatalf("archive row tier: %v", item)
		}
	}
	tiers := after["searched_tiers"].([]any)
	if len(tiers) != 2 || tiers[0] != "hot" || tiers[1] != "archive" {
		t.Fatalf("tiers %v", tiers)
	}
	if files, _ := filepath.Glob(filepath.Join(cacheDir, "segments", "*.parquet")); len(files) != 1 {
		t.Fatalf("cache after search: %v", files)
	}

	// A late duplicate of an archived event in the hot table is returned once.
	expect(t, e.newClient().bearerJSON("POST", "/ingest/batch", tok, map[string]any{"events": events[:1]}), 202)
	e.api.Logs.Archive = nil
	waitSearch(t, admin, tok, query, 1)
	e.api.Logs.Archive = arch
	r := waitSearch(t, admin, tok, query, 2)
	if r["total"].(float64) != 2 {
		t.Fatalf("duplicate counted: %v", r)
	}
	for _, raw := range r["items"].([]any) {
		item := raw.(map[string]any)
		want := "archive"
		if item["id"] == "arch-a" {
			want = "hot"
		}
		if item["tier"] != want {
			t.Fatalf("mixed source row tier: %v, want %s", item, want)
		}
	}

	// The object disappears from the provider and the cache: the search answers without it and says so.
	if files, _ := filepath.Glob(filepath.Join(cacheDir, "segments", "*.parquet")); len(files) == 1 {
		os.Remove(files[0])
	}
	if err := e.exec(`UPDATE archive_segments SET object_key = 'missing/' || object_key`); err != nil {
		t.Fatal(err)
	}
	r = expect(t, admin.bearerJSON("POST", "/logs/search", tok, query), 200).JSON()
	if r["partial"] != true {
		t.Fatalf("missing segment not reported: %v", r)
	}
}
