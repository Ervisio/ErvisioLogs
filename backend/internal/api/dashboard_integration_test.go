//go:build integration

package api

import (
	"github.com/Ervisio/ErvisioLogs/backend/internal/notify"
	"testing"
)

func TestDashboardPanelsPersistScopeAndAggregateAllMatches(t *testing.T) {
	e := newLogsEnv(t)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	viewer := e.newUser(admin, "viewer")
	outsider := e.newUser(admin, "outsider")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Insights"}), 201), "id")
	expect(t, alice.post("/workspaces/"+ws+"/members", map[string]string{"email": "viewer@example.com", "role": "viewer"}), 201)
	project := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "Events"}), 201), "id")
	token := id(expect(t, alice.post("/tokens", map[string]any{"project_id": project, "name": "ingest", "scopes": []string{"logs:ingest", "logs:read"}}), 201), "token")
	expect(t, alice.bearerJSON("POST", "/ingest/batch", token, map[string]any{"events": []map[string]any{{"id": "panel-a", "message": "one", "level": "error"}, {"id": "panel-b", "message": "two", "level": "info"}, {"id": "panel-c", "message": "three", "level": "error"}}}), 202)
	waitSearch(t, alice, token, map[string]any{"project_id": project}, 3)
	saved := id(expect(t, alice.post("/queries", map[string]any{"project_id": project, "name": "Errors", "query": map[string]any{"range": "24h", "levels": []string{"ERROR"}}}), 201), "id")
	for _, typ := range []string{"count", "timeseries", "top"} {
		panel := expect(t, alice.post("/dashboard-panels", map[string]any{"project_id": project, "saved_query_id": saved, "name": typ, "type": typ, "field": "level"}), 201)
		pid := id(panel, "id")
		data := expect(t, viewer.get("/dashboard-panels/"+pid+"/data"), 200).JSON()["items"].([]any)
		if len(data) != 1 || data[0].(map[string]any)["value"] != float64(2) {
			t.Fatalf("%s aggregate: %v", typ, data)
		}
		expect(t, outsider.get("/dashboard-panels/"+pid+"/data"), 404)
		expect(t, viewer.delete("/dashboard-panels/"+pid, nil), 403)
	}
	if got := expect(t, alice.get("/dashboard-panels?project_id="+project), 200).JSON()["items"].([]any); len(got) != 3 {
		t.Fatal("panels not persisted")
	}
	usage := expect(t, alice.get("/usage?project_id="+project), 200).JSON()
	if usage["events"] != float64(3) || usage["bytes"].(float64) <= 0 {
		t.Fatalf("usage: %v", usage)
	}
	expect(t, outsider.get("/usage?project_id="+project), 404)
	expect(t, alice.get("/usage?project_id="+project+"&range=40d"), 422)
}
func TestEmailTelegramChannelSecretsAndDeliveryHistory(t *testing.T) {
	e, fm, admin := newMailEnv(t)
	e.api.Integrations = &Integrations{Notify: notify.NewClient(func() bool { return true })}
	enableMail(t, admin)
	ws := id(expect(t, admin.post("/workspaces", map[string]string{"name": "Notifications"}), 201), "id")
	channel := expect(t, admin.post("/notification-channels", map[string]string{"workspace_id": ws, "name": "Email", "type": "email", "recipient": "ops@example.com"}), 201)
	cid := id(channel, "id")
	result := expect(t, admin.post("/notification-channels/"+cid+"/test", nil), 200).JSON()
	if result["ok"] != true {
		t.Fatal(result)
	}
	select {
	case message := <-fm.sent:
		if message.To != "ops@example.com" {
			t.Fatal(message)
		}
	default:
		t.Fatal("no email sent")
	}
	deliveries := expect(t, admin.get("/notification-channels/"+cid+"/deliveries"), 200).JSON()["items"].([]any)
	if len(deliveries) != 1 || deliveries[0].(map[string]any)["status"] != "ok" {
		t.Fatal(deliveries)
	}
	bot := "123456789:abcdefghijklmnopqrstuvwxyz"
	telegram := expect(t, admin.post("/notification-channels", map[string]string{"workspace_id": ws, "name": "Telegram", "type": "telegram", "bot_token": bot, "chat_id": "-123456"}), 201)
	if has(telegram.Body, bot) || has(telegram.Body, "whsec_") {
		t.Fatal("channel response exposed credentials")
	}
	var stored string
	if err := e.queryRow(`SELECT url FROM notification_channels WHERE id=$1`, id(telegram, "id")).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == bot || len(stored) < 50 {
		t.Fatal("Telegram token was not encrypted")
	}
	expect(t, admin.post("/notification-channels/"+id(telegram, "id")+"/rotate-secret", nil), 422)
	expect(t, admin.put("/workspaces/"+ws+"/system-alerts", map[string]any{"channel_ids": []string{cid}}), 200)
	expect(t, admin.get("/workspaces/"+ws+"/system-alerts"), 200)
}
