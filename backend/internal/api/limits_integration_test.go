//go:build integration

package api

import (
	"strconv"
	"testing"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/ingest"
	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
	"github.com/Ervisio/ErvisioLogs/backend/internal/wal"
)

// newWalEnv is newEnv with a WAL and no ClickHouse: enough to accept events and to run the admin pages.
func newWalEnv(t *testing.T) *env {
	t.Helper()
	log, err := wal.Open(wal.Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	return newEnv(t, nil, nil, func(a *API) { a.Logs = &Logs{WAL: log, Hub: ingest.NewHub(100)} })
}

func ingestOne(c *client, tok string) resp {
	return c.bearerJSON("POST", "/ingest", tok, map[string]any{"message": "x"})
}

func TestIngestRateLimit(t *testing.T) {
	e := newWalEnv(t)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Server"}), 201), "id")
	proj := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "RP"}), 201), "id")
	tok := expect(t, alice.post("/tokens", map[string]any{"project_id": proj, "name": "srv", "scopes": []string{"logs:ingest"}}), 201).JSON()["token"].(string)

	// Unlimited by default.
	for range 5 {
		expect(t, ingestOne(alice, tok), 202)
	}

	expect(t, admin.put("/admin/settings/"+settings.RateBurstSeconds, map[string]any{"value": 1}), 200)
	expect(t, alice.put("/projects/"+proj+"/settings/"+settings.RatePerToken, map[string]any{"value": 2}), 200)
	expect(t, ingestOne(alice, tok), 202)
	expect(t, ingestOne(alice, tok), 202)
	r := expect(t, ingestOne(alice, tok), 429)
	if n, err := strconv.Atoi(r.Header.Get("Retry-After")); err != nil || n < 1 {
		t.Fatalf("Retry-After %q", r.Header.Get("Retry-After"))
	}
	// A batch the bucket cannot hold at all is refused as a whole, and nothing is counted for it.
	r = alice.bearerJSON("POST", "/ingest/batch", tok, map[string]any{"events": []map[string]any{{"message": "a"}, {"message": "b"}}})
	expect(t, r, 429)

	// Back to unlimited.
	expect(t, alice.delete("/projects/"+proj+"/settings/"+settings.RatePerToken, nil), 200)
	expect(t, ingestOne(alice, tok), 202)
}

func TestIngestDailyQuota(t *testing.T) {
	e := newWalEnv(t)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Server"}), 201), "id")
	proj := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "RP"}), 201), "id")
	tok := expect(t, alice.post("/tokens", map[string]any{"project_id": proj, "name": "srv", "scopes": []string{"logs:ingest"}}), 201).JSON()["token"].(string)
	batch := func(n int) resp {
		evs := make([]map[string]any, n)
		for i := range evs {
			evs[i] = map[string]any{"message": "m" + strconv.Itoa(i)}
		}
		return alice.bearerJSON("POST", "/ingest/batch", tok, map[string]any{"events": evs})
	}

	expect(t, alice.put("/projects/"+proj+"/settings/"+settings.ProjectQuotaEvents, map[string]any{"value": 3}), 200)
	expect(t, batch(2), 202)
	// 2 + 2 exceeds 3: refused whole, and the refusal spends nothing.
	r := expect(t, batch(2), 429)
	if n, err := strconv.Atoi(r.Header.Get("Retry-After")); err != nil || n < 1 || time.Duration(n)*time.Second > 24*time.Hour {
		t.Fatalf("Retry-After %q", r.Header.Get("Retry-After"))
	}
	expect(t, ingestOne(alice, tok), 202)
	expect(t, ingestOne(alice, tok), 429)

	// The workspace quota counts every project; 3 events are used so far.
	expect(t, alice.delete("/projects/"+proj+"/settings/"+settings.ProjectQuotaEvents, nil), 200)
	expect(t, alice.put("/workspaces/"+ws+"/settings/"+settings.WorkspaceQuotaEvents, map[string]any{"value": 4}), 200)
	expect(t, ingestOne(alice, tok), 202)
	expect(t, ingestOne(alice, tok), 429)
}
