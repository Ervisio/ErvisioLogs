//go:build integration

package api

import (
	"testing"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
)

func settingEntry(t *testing.T, r resp, key string) map[string]any {
	t.Helper()
	for _, it := range r.JSON()["items"].([]any) {
		if m := it.(map[string]any); m["key"] == key {
			return m
		}
	}
	t.Fatalf("no %s in %s", key, r.Body)
	return nil
}

func TestSettingsAPI(t *testing.T) {
	e := newEnv(t, nil, nil, func(a *API) {
		a.Settings = settings.New(a.Store, config.Config{Auth: config.Auth{SessionTTL: time.Hour},
			ExportMaxRows: 1000, FromEnv: map[string]bool{"ERVISIO_LOGS_SESSION_TTL": true}})
	})
	admin := e.setupAdmin()
	alice, mallory := e.newUser(admin, "alice"), e.newUser(admin, "mallory")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Server"}), 201), "id")
	proj := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "RP"}), 201), "id")

	// Instance level: admins only; env-backed keys are locked.
	expect(t, alice.get("/admin/settings"), 403)
	ttl := settingEntry(t, expect(t, admin.get("/admin/settings"), 200), settings.SessionTTL)
	if ttl["locked"] != true || ttl["source"] != "env" || ttl["env"] != "ERVISIO_LOGS_SESSION_TTL" {
		t.Fatalf("locked entry %v", ttl)
	}
	expect(t, admin.put("/admin/settings/"+settings.SessionTTL, map[string]any{"value": 7200}), 409)
	expect(t, admin.put("/admin/settings/nope", map[string]any{"value": 1}), 404)
	expect(t, admin.put("/admin/settings/"+settings.MaxStreams, map[string]any{"value": -1}), 422)
	expect(t, admin.put("/admin/settings/"+settings.MaxStreams, map[string]any{"value": 5}), 200)

	// Workspace and project inherit, and only per-project keys exist below the instance.
	m := settingEntry(t, expect(t, alice.get("/projects/"+proj+"/settings"), 200), settings.MaxStreams)
	if m["value"] != float64(5) || m["source"] != "instance" {
		t.Fatalf("inherited from instance %v", m)
	}
	expect(t, alice.put("/workspaces/"+ws+"/settings/"+settings.ExportMaxRows, map[string]any{"value": 5}), 404)
	expect(t, alice.put("/workspaces/"+ws+"/settings/"+settings.MaxStreams, map[string]any{"value": 0}), 200)
	m = settingEntry(t, expect(t, alice.get("/projects/"+proj+"/settings"), 200), settings.MaxStreams)
	if m["value"] != float64(0) || m["source"] != "workspace" {
		t.Fatalf("inherited from workspace %v", m)
	}
	m = expect(t, alice.put("/projects/"+proj+"/settings/"+settings.MaxStreams, map[string]any{"value": 3}), 200).JSON()
	if m["value"] != float64(3) || m["source"] != "project" || m["inherited"] != float64(0) {
		t.Fatalf("project override %v", m)
	}
	m = expect(t, alice.delete("/workspaces/"+ws+"/settings/"+settings.MaxStreams, nil), 200).JSON()
	if m["value"] != float64(5) || m["source"] != "instance" {
		t.Fatalf("workspace reset %v", m)
	}

	// Outsiders see nothing.
	expect(t, mallory.get("/projects/"+proj+"/settings"), 404)
	expect(t, mallory.get("/workspaces/"+ws+"/settings"), 404)
	expect(t, mallory.put("/projects/"+proj+"/settings/"+settings.MaxStreams, map[string]any{"value": 1}), 404)
}
