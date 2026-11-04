//go:build integration

package api

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/archive"
)

func TestAdminHealth(t *testing.T) {
	e := newWalEnv(t)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	expect(t, alice.get("/admin/health"), 403)

	h := expect(t, admin.get("/admin/health"), 200).JSON()
	pg := h["postgres"].(map[string]any)
	ch := h["clickhouse"].(map[string]any)
	if pg["ok"] != true || ch["ok"] != false || h["status"] != "degraded" {
		t.Fatalf("health %v", h)
	}
	wal := h["wal"].(map[string]any)
	if wal["max_bytes"].(float64) <= 0 || wal["pending_records"].(float64) != 0 {
		t.Fatalf("wal %v", wal)
	}
	up := h["upload"].(map[string]any)
	if up["waiting"].(map[string]any)["count"] != float64(0) || up["last_verified_at"] != nil {
		t.Fatalf("upload %v", up)
	}
	if a := h["archive"].(map[string]any); a["enabled"] != false || a["provider"] != nil {
		t.Fatalf("archive %v", a)
	}
}

func TestPurgePermissionAndConfirm(t *testing.T) {
	e := newWalEnv(t)
	admin := e.setupAdmin()
	alice, dev := e.newUser(admin, "alice"), e.newUser(admin, "dev")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Server"}), 201), "id")
	p := expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "RP"}), 201)
	proj, slug := id(p, "id"), id(p, "slug")
	expect(t, alice.post("/workspaces/"+ws+"/members", map[string]string{"email": "dev@example.com", "role": "developer"}), 201)

	// A developer holds logs:read but not logs:delete.
	path := "/projects/" + proj + "/purges"
	expect(t, dev.post(path, map[string]any{"confirm": slug}), 403)
	expect(t, dev.get(path), 403)

	expect(t, alice.post(path, map[string]any{"confirm": "wrong"}), 422)
	expect(t, alice.post(path, map[string]any{"confirm": slug, "from": "2026-02-01T00:00:00Z", "to": "2026-01-01T00:00:00Z"}), 422)
	expect(t, alice.post(path, map[string]any{"confirm": slug, "nope": 1}), 400)
	// Valid, but this process has no archiver to run the job.
	expect(t, alice.post(path, map[string]any{"confirm": slug}), 503)
	expect(t, alice.get(path+"/"+proj), 404)
}

// TestPurgeHotRows runs a purge against a real ClickHouse through the worker.
func TestPurgeHotRows(t *testing.T) {
	e := newLogsEnv(t)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Server"}), 201), "id")
	p := expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "RP"}), 201)
	proj, slug := id(p, "id"), id(p, "slug")
	expect(t, alice.post("/projects/"+proj+"/streams", map[string]string{"name": "economy"}), 201)
	tok := expect(t, alice.post("/tokens", map[string]any{"project_id": proj, "name": "srv", "scopes": []string{"logs:ingest", "logs:read"}}), 201).JSON()["token"].(string)

	arch := &archive.Archiver{Store: e.store, CH: e.ch, Log: slog.New(slog.NewTextHandler(testWriter{t}, nil))}
	e.api.Storage = &Storage{Archiver: arch}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); arch.RunPurges(ctx) }()
	t.Cleanup(func() { cancel(); <-done })

	ts := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	expect(t, alice.bearerJSON("POST", "/ingest/batch", tok, map[string]any{"events": []map[string]any{
		{"timestamp": ts, "message": "keep"},
		{"timestamp": ts, "message": "drop 1", "stream": "economy"},
		{"timestamp": ts, "message": "drop 2", "stream": "economy"},
	}}), 202)
	waitSearch(t, alice, tok, map[string]any{}, 3)

	h := expect(t, admin.get("/admin/health"), 200).JSON()
	if h["status"] != "ok" || h["clickhouse"].(map[string]any)["ok"] != true || h["upload"].(map[string]any)["oldest_unarchived_event_at"] == nil {
		t.Fatalf("health %v", h)
	}

	job := expect(t, alice.post("/projects/"+proj+"/purges", map[string]any{"confirm": slug, "stream": "economy"}), 202)
	jobID := id(job, "id")
	deadline := time.Now().Add(30 * time.Second)
	for {
		j := expect(t, alice.get("/projects/"+proj+"/purges/"+jobID), 200).JSON()
		if j["state"] == "done" {
			if j["rows_deleted"] != float64(2) {
				t.Fatalf("job %v", j)
			}
			break
		}
		if j["state"] == "failed" || time.Now().After(deadline) {
			t.Fatalf("job did not finish: %v", j)
		}
		time.Sleep(250 * time.Millisecond)
	}
	got := waitSearch(t, alice, tok, map[string]any{}, 1)
	if m := messages(got); m[0] != "keep" {
		t.Fatalf("left %v", m)
	}
	if n := len(expect(t, alice.get("/projects/"+proj+"/purges"), 200).JSON()["items"].([]any)); n != 1 {
		t.Fatalf("%d jobs listed", n)
	}
	var audited int
	if err := e.queryRow(`SELECT count(*) FROM audit_events WHERE action = 'logs.purge'`).Scan(&audited); err != nil || audited != 1 {
		t.Fatalf("audit rows %d err %v", audited, err)
	}
}
