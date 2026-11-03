//go:build integration

package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

func (c *client) put(path string, b any) resp { return c.do("PUT", path, b, nil) }

// The admin storage API manages providers and retention, refuses changes that would strand
// archived segments, and is closed to everyone but the instance administrator.
func TestStorageAdmin(t *testing.T) {
	secrets := t.TempDir()
	e := newEnv(t, nil, nil, func(a *API) { a.Storage = &Storage{SecretsDir: secrets} })
	admin := e.setupAdmin()
	user := e.newUser(admin, "member")
	ctx := context.Background()

	for _, r := range []resp{user.get("/admin/storage"), user.get("/admin/retention"),
		user.post("/admin/storage/providers", map[string]any{"name": "x", "type": "local", "config": map[string]any{"root": t.TempDir()}})} {
		expect(t, r, 403)
	}

	// Invalid settings never reach the database.
	expect(t, admin.post("/admin/storage/providers", map[string]any{"name": "rel", "type": "local", "config": map[string]any{"root": "relative/dir"}}), 422)
	expect(t, admin.post("/admin/storage/providers", map[string]any{"name": "typo", "type": "local", "config": map[string]any{"rooot": "/x"}}), 422)
	expect(t, admin.post("/admin/storage/providers", map[string]any{"name": "ftp", "type": "ftp"}), 422)
	expect(t, admin.post("/admin/storage/providers", map[string]any{"name": "esc", "type": "s3", "secret_file": "../etc/passwd"}), 422)

	root := t.TempDir()
	p := expect(t, admin.post("/admin/storage/providers", map[string]any{"name": "disk", "type": "local", "config": map[string]any{"root": root}}), 201).JSON()
	pid := p["id"].(string)
	if p["status"] != "ok" || p["active"] != false {
		t.Fatalf("new provider %v", p)
	}

	// A root that turns into a file fails the health check, so it cannot become active.
	blocked := filepath.Join(t.TempDir(), "blocked")
	bp := id(expect(t, admin.post("/admin/storage/providers", map[string]any{"name": "blocked", "type": "local", "config": map[string]any{"root": blocked}}), 201), "id")
	if err := os.RemoveAll(blocked); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := expect(t, admin.post("/admin/storage/providers/"+bp+"/test", nil), 200).JSON(); got["status"] != "error" || got["last_error"] == nil {
		t.Fatalf("broken provider status %v", got)
	}
	expect(t, admin.put("/admin/storage/active", map[string]any{"provider_id": bp}), 422)
	expect(t, admin.put("/admin/storage/active", map[string]any{"provider_id": pid}), 204)

	ov := expect(t, admin.get("/admin/storage"), 200).JSON()
	provs := ov["providers"].([]any)
	if len(provs) != 2 || ov["archiver_enabled"] != false {
		t.Fatalf("overview %v", ov)
	}
	for _, x := range provs {
		m := x.(map[string]any)
		if (m["id"] == pid) != (m["active"] == true) {
			t.Fatalf("active flag wrong: %v", m)
		}
	}

	// Once a segment points at the provider, its location and existence are fixed.
	ws := id(expect(t, admin.post("/workspaces", map[string]string{"name": "Server"}), 201), "id")
	proj := id(expect(t, admin.post("/projects", map[string]string{"workspace_id": ws, "name": "RP"}), 201), "id")
	var stream uuid.UUID
	if err := e.queryRow(`SELECT id FROM streams WHERE project_id = $1 LIMIT 1`, proj).Scan(&stream); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.StartSegment(ctx, store.NewSegment{WorkspaceID: uuid.MustParse(ws), ProjectID: uuid.MustParse(proj), StreamID: stream,
		Day: time.Now().UTC().Truncate(24 * time.Hour), ProviderID: uuid.MustParse(pid), CutoffTo: time.Now().UTC(), SchemaVersion: 1,
		KeyFor: func(id uuid.UUID, g int32) string { return "segments/" + id.String() }}); err != nil {
		t.Fatal(err)
	}
	expect(t, admin.put("/admin/storage/providers/"+pid, map[string]any{"name": "disk", "config": map[string]any{"root": t.TempDir()}}), 409)
	expect(t, admin.put("/admin/storage/providers/"+pid, map[string]any{"name": "disk", "type": "s3", "config": map[string]any{"root": root}}), 422)
	if got := expect(t, admin.put("/admin/storage/providers/"+pid, map[string]any{"name": "main disk", "config": map[string]any{"root": root}}), 200).JSON(); got["name"] != "main disk" {
		t.Fatalf("rename %v", got)
	}
	expect(t, admin.delete("/admin/storage/providers/"+pid, nil), 409)
	expect(t, admin.delete("/admin/storage/providers/"+bp, nil), 204)
	expect(t, admin.delete("/admin/storage/providers/"+bp, nil), 404)
	if segs := expect(t, admin.get("/admin/storage"), 200).JSON()["segments"].(map[string]any); segs[store.SegStaging] == nil {
		t.Fatalf("segment totals %v", segs)
	}

	// Retention: the instance row needs every field, overrides only ages.
	inst := "/admin/retention/instance/" + uuid.Nil.String()
	expect(t, admin.put(inst, map[string]any{"local_age_hours": 24}), 422)
	full := map[string]any{"local_age_hours": 48, "global_age_hours": 24, "cache_max_bytes": 1 << 30, "high_pct": 90, "low_pct": 75}
	expect(t, admin.put(inst, full), 422) // global < local
	full["global_age_hours"] = 24 * 90
	expect(t, admin.put(inst, full), 200)
	full["low_pct"] = 95
	expect(t, admin.put(inst, full), 422)
	expect(t, admin.put("/admin/retention/instance/"+uuid.NewString(), map[string]any{"local_age_hours": 24}), 404)
	expect(t, admin.put("/admin/retention/project/"+uuid.NewString(), map[string]any{"local_age_hours": 24}), 404)
	expect(t, admin.put("/admin/retention/galaxy/"+proj, map[string]any{"local_age_hours": 24}), 404)
	expect(t, admin.put("/admin/retention/project/"+proj, map[string]any{"local_age_hours": 24, "cache_max_bytes": 1 << 30}), 422)
	expect(t, admin.put("/admin/retention/project/"+proj, map[string]any{}), 422)
	if got := expect(t, admin.put("/admin/retention/project/"+proj, map[string]any{"global_age_hours": 24 * 7}), 200).JSON(); got["global_age_hours"] != float64(24*7) {
		t.Fatalf("override %v", got)
	}
	if items := expect(t, admin.get("/admin/retention"), 200).JSON()["items"].([]any); len(items) != 2 {
		t.Fatalf("policies %v", items)
	}
	expect(t, admin.delete(inst, nil), 422)
	expect(t, admin.delete("/admin/retention/project/"+proj, nil), 204)
	expect(t, admin.delete("/admin/retention/project/"+proj, nil), 404)

	var n int
	if err := e.queryRow(`SELECT count(*) FROM audit_events WHERE action LIKE 'storage.%' OR action LIKE 'retention.%'`).Scan(&n); err != nil || n < 6 {
		t.Fatalf("audit rows %d %v", n, err)
	}
}
