//go:build integration

package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/notify"
	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
)

// receiver records the webhook deliveries it gets.
type receiver struct {
	mu   sync.Mutex
	got  []receivedHook
	srv  *httptest.Server
	fail bool
}

type receivedHook struct {
	header http.Header
	body   []byte
}

func newReceiver(t *testing.T) *receiver {
	rc := &receiver{}
	rc.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rc.mu.Lock()
		rc.got = append(rc.got, receivedHook{r.Header.Clone(), b})
		fail := rc.fail
		rc.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(rc.srv.Close)
	return rc
}

func (rc *receiver) all() []receivedHook {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return append([]receivedHook(nil), rc.got...)
}

// newIntegrationsEnv is a logs env with alerts on; webhooks may target loopback for the receiver.
func newIntegrationsEnv(t *testing.T) *logsEnv {
	e := newLogsEnv(t)
	e.api.Integrations = &Integrations{Notify: notify.NewClient(func() bool { return true })}
	ctx := context.Background()
	for key, v := range map[string]string{settings.WebhookAllowPrivate: "true", settings.ExportMaxRows: "5"} {
		if _, err := e.api.Settings.Set(ctx, settings.Scope{}, key, json.RawMessage(v), nil); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func TestSavedQueries(t *testing.T) {
	e := newIntegrationsEnv(t)
	admin := e.setupAdmin()
	alice, bob, mallory := e.newUser(admin, "alice"), e.newUser(admin, "bob"), e.newUser(admin, "mallory")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Server"}), 201), "id")
	expect(t, alice.post("/workspaces/"+ws+"/members", map[string]string{"email": "bob@example.com", "role": "viewer"}), 201)
	proj := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "RP"}), 201), "id")
	expect(t, alice.post("/projects/"+proj+"/streams", map[string]string{"name": "economy"}), 201)

	// The filter is checked like a search: unknown streams, bad ranges and unknown fields fail.
	expect(t, alice.post("/queries", map[string]any{"project_id": proj, "name": "x", "query": map[string]any{"streams": []string{"nope"}}}), 422)
	expect(t, alice.post("/queries", map[string]any{"project_id": proj, "name": "x", "query": map[string]any{"range": "1y"}}), 422)
	expect(t, alice.post("/queries", map[string]any{"project_id": proj, "name": "x", "query": map[string]any{"limit": 5}}), 400)
	expect(t, alice.post("/queries", map[string]any{"project_id": proj, "name": "", "query": map[string]any{}}), 422)

	q := expect(t, alice.post("/queries", map[string]any{"project_id": proj, "name": "Errors",
		"query": map[string]any{"range": "24h", "levels": []string{"ERROR"}, "streams": []string{"economy"}}}), 201).JSON()
	qid := q["id"].(string)

	// Any member reads; only the creator or projects:write edits; outsiders see nothing.
	if items := expect(t, bob.get("/queries?project_id="+proj), 200).JSON()["items"].([]any); len(items) != 1 {
		t.Fatalf("bob sees %d queries", len(items))
	}
	expect(t, bob.get("/queries/"+qid), 200)
	expect(t, bob.put("/queries/"+qid, map[string]any{"project_id": proj, "name": "mine", "query": map[string]any{}}), 403)
	expect(t, bob.delete("/queries/"+qid, nil), 403)
	expect(t, mallory.get("/queries/"+qid), 404)
	expect(t, mallory.get("/queries?project_id="+proj), 404)

	up := expect(t, alice.put("/queries/"+qid, map[string]any{"project_id": proj, "name": "Errors 1h", "query": map[string]any{"range": "1h"}}), 200).JSON()
	if up["name"] != "Errors 1h" {
		t.Fatalf("updated %v", up)
	}
	expect(t, alice.delete("/queries/"+qid, nil), 204)
	expect(t, alice.get("/queries/"+qid), 404)
}

func TestChannelsAndAlerts(t *testing.T) {
	e := newIntegrationsEnv(t)
	rc := newReceiver(t)
	admin := e.setupAdmin()
	alice, viewer, mallory := e.newUser(admin, "alice"), e.newUser(admin, "vic"), e.newUser(admin, "mallory")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Server"}), 201), "id")
	expect(t, alice.post("/workspaces/"+ws+"/members", map[string]string{"email": "vic@example.com", "role": "viewer"}), 201)
	proj := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "RP"}), 201), "id")
	tok := expect(t, alice.post("/tokens", map[string]any{"project_id": proj, "name": "srv", "scopes": []string{"logs:ingest", "logs:read"}}), 201).JSON()["token"].(string)

	// Channels: alerts:manage to change, URL never echoed, secret shown once.
	expect(t, viewer.post("/notification-channels", map[string]any{"workspace_id": ws, "name": "hook", "url": rc.srv.URL}), 403)
	expect(t, alice.post("/notification-channels", map[string]any{"workspace_id": ws, "name": "hook", "url": "ftp://x.example"}), 422)
	expect(t, alice.post("/notification-channels", map[string]any{"workspace_id": ws, "name": "hook", "url": rc.srv.URL, "format": "teams"}), 422)
	ch := expect(t, alice.post("/notification-channels", map[string]any{"workspace_id": ws, "name": "hook", "url": rc.srv.URL + "/secret-path"}), 201)
	if has(ch.Body, "secret-path") {
		t.Fatalf("create echoed the URL path: %s", ch.Body)
	}
	cj := ch.JSON()
	cid, secret := cj["id"].(string), cj["secret"].(string)
	if !strings.HasPrefix(secret, "whsec_") {
		t.Fatalf("secret %q", secret)
	}
	got := expect(t, viewer.get("/notification-channels/"+cid), 200)
	if has(got.Body, "whsec_") || has(got.Body, "secret-path") {
		t.Fatalf("get leaked the secret or URL: %s", got.Body)
	}
	expect(t, mallory.get("/notification-channels/"+cid), 404)
	expect(t, mallory.get("/notification-channels?workspace_id="+ws), 404)

	// Test delivery is signed with the shown secret.
	if r := expect(t, alice.post("/notification-channels/"+cid+"/test", nil), 200).JSON(); r["ok"] != true {
		t.Fatalf("test delivery %v", r)
	}
	key, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil {
		t.Fatal(err)
	}
	hooks := rc.all()
	if len(hooks) != 1 {
		t.Fatalf("%d deliveries", len(hooks))
	}
	h := hooks[0]
	if !notify.Verify(key, h.header.Get("X-Ervisio-Timestamp"), h.header.Get("X-Ervisio-Signature"), h.body, time.Now(), 5*time.Minute) {
		t.Fatal("test delivery signature does not verify with the shown secret")
	}

	// Rotation invalidates the old secret.
	rot := expect(t, alice.post("/notification-channels/"+cid+"/rotate-secret", nil), 200).JSON()
	if rot["secret"] == secret || rot["secret"] == nil {
		t.Fatal("rotation kept the secret")
	}

	// Rules validate their spec and their channels.
	bad := []map[string]any{
		{"project_id": proj, "name": "r", "query": map[string]any{}, "window_seconds": 10, "threshold": 0},
		{"project_id": proj, "name": "r", "query": map[string]any{}, "window_seconds": 60, "threshold": -1},
		{"project_id": proj, "name": "r", "query": map[string]any{}, "window_seconds": 60, "threshold": 0, "interval_seconds": 5},
		{"project_id": proj, "name": "r", "query": map[string]any{}, "window_seconds": 60, "threshold": 0, "channel_ids": []string{proj}},
	}
	for _, b := range bad {
		expect(t, alice.post("/alerts", b), 422)
	}
	expect(t, viewer.post("/alerts", map[string]any{"project_id": proj, "name": "r", "query": map[string]any{}, "window_seconds": 60, "threshold": 0}), 403)
	rule := expect(t, alice.post("/alerts", map[string]any{"project_id": proj, "name": "Errors", "query": map[string]any{"levels": []string{"ERROR"}},
		"window_seconds": 300, "threshold": 1, "channel_ids": []string{cid}}), 201).JSON()
	rid := rule["id"].(string)
	if rule["state"] != "ok" || rule["enabled"] != true {
		t.Fatalf("new rule %v", rule)
	}

	// Two errors cross the threshold of 1: the rule fires once and notifies.
	for i := 0; i < 2; i++ {
		expect(t, e.newClient().bearerJSON("POST", "/ingest", tok, map[string]any{"level": "ERROR", "message": "boom"}), 202)
	}
	waitSearch(t, e.newClient(), tok, map[string]any{"levels": []string{"ERROR"}}, 2)
	ctx := context.Background()
	e.api.evaluateDue(ctx)
	if r := expect(t, alice.get("/alerts/"+rid), 200).JSON(); r["state"] != "firing" || r["last_value"] != float64(2) {
		t.Fatalf("after evaluation %v", r)
	}
	hooks = rc.all()
	if len(hooks) != 2 {
		t.Fatalf("%d deliveries, want test + firing", len(hooks))
	}
	var p map[string]any
	if err := json.Unmarshal(hooks[1].body, &p); err != nil || p["type"] != "alert.firing" || p["rule_id"] != rid {
		t.Fatalf("firing payload %s", hooks[1].body)
	}
	ev := expect(t, viewer.get("/alerts/"+rid+"/events"), 200).JSON()["items"].([]any)
	if len(ev) != 1 || ev[0].(map[string]any)["status"] != "firing" {
		t.Fatalf("history %v", ev)
	}

	// Staying above the threshold does not notify again; a raised threshold resolves it.
	if _, err := e.pool.Exec(ctx, `UPDATE alert_rules SET next_eval_at = now() WHERE id = $1`, rid); err != nil {
		t.Fatal(err)
	}
	e.api.evaluateDue(ctx)
	if n := len(rc.all()); n != 2 {
		t.Fatalf("%d deliveries after a steady evaluation", n)
	}
	upd := expect(t, alice.put("/alerts/"+rid, map[string]any{"project_id": proj, "name": "Errors", "query": map[string]any{"levels": []string{"ERROR"}},
		"window_seconds": 300, "threshold": 10, "channel_ids": []string{cid}}), 200).JSON()
	if upd["threshold"] != float64(10) {
		t.Fatalf("updated %v", upd)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE alert_rules SET next_eval_at = now() WHERE id = $1`, rid); err != nil {
		t.Fatal(err)
	}
	e.api.evaluateDue(ctx)
	if r := expect(t, alice.get("/alerts/"+rid), 200).JSON(); r["state"] != "ok" {
		t.Fatalf("after raising the threshold %v", r)
	}
	hooks = rc.all()
	if len(hooks) != 3 || !bytes.Contains(hooks[2].body, []byte(`"alert.resolved"`)) {
		t.Fatalf("resolve delivery missing: %d", len(hooks))
	}

	// A failing receiver is recorded on the channel.
	rc.mu.Lock()
	rc.fail = true
	rc.mu.Unlock()
	if r := expect(t, alice.post("/notification-channels/"+cid+"/test", nil), 200).JSON(); r["ok"] != false {
		t.Fatalf("failing test delivery %v", r)
	}
	if c := expect(t, alice.get("/notification-channels/"+cid), 200).JSON(); c["last_status"] != "error" {
		t.Fatalf("channel after failure %v", c)
	}

	expect(t, mallory.get("/alerts/"+rid), 404)
	expect(t, alice.delete("/notification-channels/"+cid, nil), 204)
	expect(t, alice.delete("/alerts/"+rid, nil), 204)
	expect(t, alice.get("/alerts/"+rid), 404)
}

func TestExport(t *testing.T) {
	e := newIntegrationsEnv(t)
	admin := e.setupAdmin()
	alice, viewer := e.newUser(admin, "alice"), e.newUser(admin, "vic")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Server"}), 201), "id")
	expect(t, alice.post("/workspaces/"+ws+"/members", map[string]string{"email": "vic@example.com", "role": "viewer"}), 201)
	proj := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "RP"}), 201), "id")
	tok := expect(t, alice.post("/tokens", map[string]any{"project_id": proj, "name": "srv", "scopes": []string{"logs:ingest", "logs:read"}}), 201).JSON()["token"].(string)
	exp := expect(t, alice.post("/tokens", map[string]any{"project_id": proj, "name": "exp", "scopes": []string{"logs:export"}}), 201).JSON()["token"].(string)

	events := []map[string]any{}
	for i := 0; i < 7; i++ {
		events = append(events, map[string]any{"message": "event", "level": "INFO"})
	}
	events[0]["message"] = "=HYPERLINK(\"http://evil\")"
	expect(t, e.newClient().bearerJSON("POST", "/ingest/batch", tok, map[string]any{"events": events}), 202)
	waitSearch(t, e.newClient(), tok, map[string]any{"limit": 10}, 7)

	// Permission: viewer lacks logs:export; a read-only token too.
	expect(t, viewer.post("/logs/export", map[string]any{"project_id": proj}), 403)
	expect(t, e.newClient().bearerJSON("POST", "/logs/export", tok, map[string]any{}), 403)
	expect(t, alice.post("/logs/export", map[string]any{"project_id": proj, "limit": 3}), 422)
	expect(t, alice.post("/logs/export", map[string]any{"project_id": proj, "format": "xml"}), 422)

	run := func(format string) (*http.Response, []byte) {
		b, _ := json.Marshal(map[string]any{"format": format})
		req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/logs/export", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+exp)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body) // trailers are read after the body
		return res, body
	}

	// NDJSON: capped at 5 rows (ExportMaxRows) and flagged as truncated.
	res, body := run("ndjson")
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("ndjson export %d %s", res.StatusCode, res.Header)
	}
	lines := 0
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		var it map[string]any
		if err := json.Unmarshal(sc.Bytes(), &it); err != nil {
			t.Fatalf("line %d: %v", lines, err)
		}
		lines++
	}
	if lines != 5 || res.Trailer.Get("X-Export-Truncated") != "true" {
		t.Fatalf("ndjson: %d lines, truncated=%q", lines, res.Trailer.Get("X-Export-Truncated"))
	}

	// CSV: header plus rows, formula cells neutralised.
	res, body = run("csv")
	recs, err := csv.NewReader(bytes.NewReader(body)).ReadAll()
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("csv export %d %v", res.StatusCode, err)
	}
	if len(recs) != 6 || recs[0][0] != "timestamp" {
		t.Fatalf("csv rows %d header %v", len(recs), recs[0])
	}
	for _, r := range recs[1:] {
		if strings.HasPrefix(r[4], "=") {
			t.Fatalf("formula cell exported as is: %q", r[4])
		}
	}

	// Exports are audited.
	audit := expect(t, alice.get("/workspaces/"+ws+"/audit"), 200)
	if !has(audit.Body, "logs.export") {
		t.Fatalf("export not audited: %s", audit.Body)
	}
}

func TestOTLPReceiver(t *testing.T) {
	e := newIntegrationsEnv(t)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Server"}), 201), "id")
	proj := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "RP"}), 201), "id")
	expect(t, alice.post("/projects/"+proj+"/streams", map[string]string{"name": "game"}), 201)
	tok := expect(t, alice.post("/tokens", map[string]any{"project_id": proj, "name": "otel", "scopes": []string{"logs:ingest", "logs:read"}}), 201).JSON()["token"].(string)

	post := func(ctype string, body string, hdr map[string]string) (*http.Response, []byte) {
		req, _ := http.NewRequest("POST", e.srv.URL+"/v1/logs", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", ctype)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res, b
	}

	if res, _ := post("text/plain", "{}", nil); res.StatusCode != 415 {
		t.Fatalf("text/plain: %d", res.StatusCode)
	}
	if res, _ := post("application/json", "{not json", nil); res.StatusCode != 400 {
		t.Fatalf("malformed: %d", res.StatusCode)
	}

	now := time.Now().UnixNano()
	body := `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"gateway"}}]},
	 "scopeLogs":[{"scope":{"name":"otel-test"},"logRecords":[
	  {"timeUnixNano":"` + itoa(now) + `","severityNumber":17,"body":{"stringValue":"otlp error"},
	   "attributes":[{"key":"http.status_code","value":{"intValue":"500"}}]},
	  {"timeUnixNano":"` + itoa(now) + `","severityNumber":9,"body":{"stringValue":"bad trace"},"traceId":"zz"}]}]}]}`
	res, out := post("application/json", body, map[string]string{"X-Ervisio-Stream": "game"})
	if res.StatusCode != 200 || !bytes.Contains(out, []byte(`"rejectedLogRecords":"1"`)) {
		t.Fatalf("otlp json: %d %s", res.StatusCode, out)
	}
	r := waitSearch(t, e.newClient(), tok, map[string]any{"services": []string{"gateway"}}, 1)
	it := r["items"].([]any)[0].(map[string]any)
	if it["stream"] != "game" || it["level"] != "ERROR" || it["message"] != "otlp error" {
		t.Fatalf("mapped %v", it)
	}
	attrs, _ := json.Marshal(it["attributes"])
	if !bytes.Contains(attrs, []byte(`"status_code"`)) || !bytes.Contains(attrs, []byte(`"otel-test"`)) {
		t.Fatalf("attributes %s", attrs)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
