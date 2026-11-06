//go:build integration

package api

import (
	"strings"
	"testing"
)

func TestEventCatalogAndFieldRules(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	outsider := e.newUser(admin, "outsider")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Server"}), 201), "id")
	project := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "RP"}), 201), "id")
	base := "/projects/" + project
	expect(t, outsider.get(base+"/event-types"), 404)
	expect(t, alice.post(base+"/event-types", map[string]any{"name": "money.add", "title": "Custom money", "featured_fields": []string{"amount"}}), 200)
	expect(t, alice.post(base+"/event-types/presets/fivem", nil), 200)
	list := expect(t, alice.get(base+"/event-types"), 200).JSON()["items"].([]any)
	found := false
	for _, value := range list {
		event := value.(map[string]any)
		if event["name"] == "money.add" {
			found = true
			if event["title"] != "Custom money" {
				t.Fatal("preset overwrote custom event")
			}
		}
	}
	if !found {
		t.Fatal("custom event disappeared")
	}
	expect(t, alice.patch(base+"/event-types/money.add", map[string]string{"color": "#123abc"}), 200)
	expect(t, alice.patch(base+"/event-types/missing", map[string]string{"title": "missing"}), 404)
	rules := map[string]any{"items": []map[string]any{{"field": "actor.name", "label": "Player", "redact": true, "format": "text"}}}
	expect(t, outsider.put(base+"/field-rules", rules), 404)
	expect(t, alice.put(base+"/field-rules", rules), 200)
	saved := expect(t, alice.get(base+"/field-rules"), 200).JSON()["items"].([]any)
	if len(saved) != 1 || saved[0].(map[string]any)["redact"] != true {
		t.Fatalf("rules not saved: %v", saved)
	}
	expect(t, alice.delete(base+"/event-types/money.add", nil), 204)
	expect(t, alice.delete(base+"/event-types/money.add", nil), 404)
}

func TestTagsDiscoveryAndRedaction(t *testing.T) {
	e := newLogsEnv(t)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Server"}), 201), "id")
	project := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "RP"}), 201), "id")
	base := "/projects/" + project
	expect(t, alice.put(base+"/field-rules", map[string]any{"items": []map[string]any{{"field": "actor.name", "redact": true}}}), 200)
	token := expect(t, alice.post("/tokens", map[string]any{"project_id": project, "name": "ingest", "scopes": []string{"logs:ingest", "logs:read", "logs:export"}}), 201).JSON()["token"].(string)
	expect(t, alice.bearerJSON("POST", "/ingest", token, map[string]any{"event": "money.add", "tags": []string{"economy", "transfer"}, "attributes": map[string]any{"actor": map[string]any{"id": 7, "name": "private-player"}}}), 202)
	result := waitSearch(t, alice, token, map[string]any{"tags": []string{"economy", "transfer"}, "filters": []map[string]string{{"field": "actor.id", "op": "eq", "value": "7"}}}, 1)
	event := result["items"].([]any)[0].(map[string]any)
	actor := event["attributes"].(map[string]any)["actor"].(map[string]any)
	if actor["name"] != "[REDACTED]" {
		t.Fatalf("private data persisted: %v", actor)
	}
	if event["tier"] != "hot" {
		t.Fatalf("expected hot tier: %v", event)
	}
	if len(event["tags"].([]any)) != 2 {
		t.Fatalf("tags lost: %v", event)
	}
	histogram := expect(t, alice.bearerJSON("POST", "/logs/histogram", token, map[string]any{"tags": []string{"economy"}, "interval_seconds": 60}), 200).JSON()["items"].([]any)
	if len(histogram) != 1 || histogram[0].(map[string]any)["count"] != float64(1) {
		t.Fatalf("histogram mismatch %v", histogram)
	}
	waitSearch(t, alice, token, map[string]any{"tags": []string{"missing"}}, 0)
	catalog := expect(t, alice.get(base+"/event-types"), 200).JSON()["items"].([]any)
	if len(catalog) != 1 || catalog[0].(map[string]any)["discovered"] != true {
		t.Fatalf("discovery failed: %v", catalog)
	}
	exported := expect(t, alice.bearerJSON("POST", "/logs/export", token, map[string]any{"tags": []string{"economy"}, "format": "ndjson"}), 200)
	if strings.Contains(string(exported.Body), "private-player") || !strings.Contains(string(exported.Body), `"tags":["economy","transfer"]`) {
		t.Fatalf("export mismatch %s", exported.Body)
	}
}
