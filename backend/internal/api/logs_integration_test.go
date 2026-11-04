//go:build integration

package api

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
	"github.com/Ervisio/ErvisioLogs/backend/internal/db"
	"github.com/Ervisio/ErvisioLogs/backend/internal/ingest"
	"github.com/Ervisio/ErvisioLogs/backend/internal/migrate"
	"github.com/Ervisio/ErvisioLogs/backend/internal/wal"
	"github.com/Ervisio/ErvisioLogs/backend/migrations"
)

type logsEnv struct {
	*env
	ch     driver.Conn
	writer *ingest.Writer
}

// newLogsEnv is newEnv plus a migrated ClickHouse, a WAL in a temp dir and a running writer.
func newLogsEnv(t *testing.T) *logsEnv {
	t.Helper()
	addr := os.Getenv("ERVISIO_LOGS_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("ERVISIO_LOGS_CLICKHOUSE_ADDR not set")
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") && !strings.HasPrefix(addr, "localhost:") {
		t.Fatal("refusing to use a non-local ClickHouse")
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
	dir := t.TempDir()
	log, err := wal.Open(wal.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	hub := ingest.NewHub(1000)
	w := &ingest.Writer{Log: log, CH: ch, Dir: dir, Hub: hub, Linger: 10 * time.Millisecond,
		Logger: slog.New(slog.NewTextHandler(testWriter{t}, nil))}
	le := &logsEnv{ch: ch, writer: w}
	le.env = newEnv(t, nil, nil, func(a *API) {
		a.Logs = &Logs{WAL: log, CH: ch, Hub: hub, Writer: w}
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := w.Run(ctx); err != nil {
			t.Errorf("writer stopped: %v", err)
		}
	}()
	t.Cleanup(func() { cancel(); <-done; log.Close() })
	return le
}

func (c *client) bearerJSON(method, path, tok string, body any) resp {
	return c.do(method, path, body, map[string]string{"Authorization": "Bearer " + tok})
}

// waitSearch repeats a search until it returns want items (the writer is asynchronous).
func waitSearch(t *testing.T, c *client, tok string, body map[string]any, want int) map[string]any {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		r := expect(t, c.bearerJSON("POST", "/logs/search", tok, body), 200).JSON()
		items, _ := r["items"].([]any)
		if len(items) == want || time.Now().After(deadline) {
			if len(items) != want {
				t.Fatalf("search %v: %d items, want %d: %v", body, len(items), want, r)
			}
			return r
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func messages(r map[string]any) []string {
	var out []string
	for _, it := range r["items"].([]any) {
		out = append(out, it.(map[string]any)["message"].(string))
	}
	return out
}

func TestIngestSearchAndTail(t *testing.T) {
	e := newLogsEnv(t)
	admin := e.setupAdmin()
	alice, mallory := e.newUser(admin, "alice"), e.newUser(admin, "mallory")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Server"}), 201), "id")
	proj := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "RP"}), 201), "id")
	expect(t, alice.post("/projects/"+proj+"/streams", map[string]string{"name": "economy"}), 201)
	both := expect(t, alice.post("/tokens", map[string]any{"project_id": proj, "name": "srv", "scopes": []string{"logs:ingest", "logs:read"}}), 201).JSON()["token"].(string)
	readOnly := expect(t, alice.post("/tokens", map[string]any{"project_id": proj, "name": "ro", "scopes": []string{"logs:read"}}), 201).JSON()["token"].(string)

	// Open a live tail before ingesting.
	tailReq, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/logs/tail?levels=ERROR", nil)
	tailReq.Header.Set("Authorization", "Bearer "+both)
	tctx, stopTail := context.WithCancel(context.Background())
	defer stopTail()
	tailRes, err := http.DefaultClient.Do(tailReq.WithContext(tctx))
	if err != nil {
		t.Fatal(err)
	}
	defer tailRes.Body.Close()
	if tailRes.StatusCode != 200 || tailRes.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("tail status %d type %q", tailRes.StatusCode, tailRes.Header.Get("Content-Type"))
	}
	frames := make(chan string, 10)
	go func() {
		sc := bufio.NewScanner(tailRes.Body)
		var cur strings.Builder
		for sc.Scan() {
			if sc.Text() == "" {
				frames <- cur.String()
				cur.Reset()
				continue
			}
			cur.WriteString(sc.Text() + "\n")
		}
		close(frames)
	}()

	// Unknown streams must be rejected, not created.
	expect(t, alice.put("/projects/"+proj+"/settings/ingest.auto_create_streams", map[string]any{"value": false}), 200)

	// Ingest: permissions first.
	expect(t, alice.post("/ingest", map[string]any{"message": "x"}), 401)
	expect(t, alice.bearerJSON("POST", "/ingest", readOnly, map[string]any{"message": "x"}), 403)

	ts := time.Now().UTC().Add(-time.Minute)
	batch := map[string]any{"environment": "production", "events": []map[string]any{
		{"id": "evt-a", "timestamp": ts.Format(time.RFC3339Nano), "level": "info", "event": "player.join", "message": "Mario joined",
			"attributes": map[string]any{"player": map[string]any{"license": "lic:1", "money": 500}}},
		{"id": "evt-b", "timestamp": ts.Add(time.Second).Format(time.RFC3339Nano), "level": "error", "stream": "economy",
			"event": "bank.fail", "message": "Transfer failed", "service": "bank",
			"attributes": map[string]any{"player": map[string]any{"license": "lic:2", "money": 20}}},
		{"level": "loud", "message": "bad level"},
		{"stream": "nope", "message": "bad stream"},
	}}
	r := expect(t, alice.bearerJSON("POST", "/ingest/batch", both, batch), 202).JSON()
	if r["accepted"].(float64) != 2 || r["rejected"].(float64) != 2 || r["status"] != "durably_queued" {
		t.Fatalf("batch result %v", r)
	}
	errs := r["errors"].([]any)
	if errs[0].(map[string]any)["index"].(float64) != 2 || errs[1].(map[string]any)["index"].(float64) != 3 {
		t.Fatalf("rejections %v", errs)
	}
	// Only invalid events: 422 with the same body shape.
	r = expect(t, alice.bearerJSON("POST", "/ingest/batch", both, map[string]any{"events": []map[string]any{{"level": "nope"}}}), 422).JSON()
	if r["status"] != "rejected" {
		t.Fatalf("all-invalid result %v", r)
	}
	expect(t, alice.bearerJSON("POST", "/ingest/batch", both, map[string]any{"events": []any{}}), 422)
	expect(t, alice.bearerJSON("POST", "/ingest", both, map[string]any{"message": "x", "unknown": 1}), 400)

	// A single gzip event, and a resend of evt-a that must not show up twice.
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte(`{"id":"evt-c","timestamp":"` + ts.Add(2*time.Second).Format(time.RFC3339Nano) + `","message":"Compressed hello","environment":"production"}`))
	_ = zw.Close()
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/ingest", &gz)
	req.Header.Set("Authorization", "Bearer "+both)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 202 {
		t.Fatalf("gzip ingest %d: %s", res.StatusCode, b)
	}
	expect(t, alice.bearerJSON("POST", "/ingest", both, batch["events"].([]map[string]any)[0]), 202)

	// Search: newest first, deduplicated by id.
	all := waitSearch(t, alice, both, map[string]any{"include_total": true}, 3)
	if got := strings.Join(messages(all), "|"); got != "Compressed hello|Transfer failed|Mario joined" {
		t.Fatalf("order %q", got)
	}
	if all["total"].(float64) != 3 {
		t.Fatalf("total %v", all["total"])
	}
	lc := all["level_counts"].(map[string]any)
	if lc["INFO"].(float64) != 2 || lc["ERROR"].(float64) != 1 {
		t.Fatalf("level counts %v", lc)
	}
	first := all["items"].([]any)[1].(map[string]any)
	if first["stream"] != "economy" || first["environment"] != "production" || first["service"] != "bank" ||
		first["attributes"].(map[string]any)["player"].(map[string]any)["license"] != "lic:2" {
		t.Fatalf("item %v", first)
	}

	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{"level", map[string]any{"levels": []string{"ERROR"}}, "Transfer failed"},
		{"severity", map[string]any{"severity_min": 13}, "Transfer failed"},
		{"stream", map[string]any{"streams": []string{"economy"}}, "Transfer failed"},
		{"event", map[string]any{"events": []string{"player.join"}}, "Mario joined"},
		{"service", map[string]any{"services": []string{"bank"}}, "Transfer failed"},
		{"query", map[string]any{"query": "MARIO"}, "Mario joined"},
		{"attr eq", map[string]any{"filters": []map[string]string{{"field": "player.license", "op": "eq", "value": "lic:1"}}}, "Mario joined"},
		{"attr neq", map[string]any{"filters": []map[string]string{{"field": "player.license", "op": "neq", "value": "lic:1"}, {"field": "player", "op": "exists"}}}, "Transfer failed"},
		{"attr contains", map[string]any{"filters": []map[string]string{{"field": "player.license", "op": "contains", "value": ":2"}}}, "Transfer failed"},
		{"attr gt", map[string]any{"filters": []map[string]string{{"field": "player.money", "op": "gt", "value": "100"}}}, "Mario joined"},
		{"attr lte", map[string]any{"filters": []map[string]string{{"field": "player.money", "op": "lte", "value": "20"}}}, "Transfer failed"},
	}
	for _, c := range cases {
		got := waitSearch(t, alice, both, c.body, 1)
		if m := messages(got); m[0] != c.want {
			t.Errorf("%s: got %v, want %s", c.name, m, c.want)
		}
	}
	none := expect(t, alice.bearerJSON("POST", "/logs/search", both, map[string]any{
		"filters": []map[string]string{{"field": "missing.path", "op": "exists"}}}), 200).JSON()
	if len(none["items"].([]any)) != 0 {
		t.Fatalf("missing path matched %v", none)
	}

	// Cursor pagination.
	p1 := expect(t, alice.bearerJSON("POST", "/logs/search", both, map[string]any{"limit": 2}), 200).JSON()
	cur, _ := p1["next_cursor"].(string)
	if len(p1["items"].([]any)) != 2 || cur == "" {
		t.Fatalf("page 1 %v", p1)
	}
	p2 := expect(t, alice.bearerJSON("POST", "/logs/search", both, map[string]any{"limit": 2, "cursor": cur}), 200).JSON()
	if m := messages(p2); len(m) != 1 || m[0] != "Mario joined" || p2["next_cursor"] != nil {
		t.Fatalf("page 2 %v", p2)
	}
	expect(t, alice.bearerJSON("POST", "/logs/search", both, map[string]any{"cursor": "garbage"}), 400)
	expect(t, alice.bearerJSON("POST", "/logs/search", both, map[string]any{"filters": []map[string]string{{"field": "a`b", "op": "eq", "value": "1"}}}), 422)
	expect(t, alice.bearerJSON("POST", "/logs/search", both, map[string]any{"from": ts.Add(-40 * 24 * time.Hour), "to": ts}), 422)

	// Sessions: the member reads, a stranger gets 404, a session cannot ingest.
	sess := expect(t, alice.post("/logs/search", map[string]any{"project_id": proj}), 200).JSON()
	if len(sess["items"].([]any)) != 3 {
		t.Fatalf("session search %v", sess)
	}
	expect(t, alice.post("/logs/search", map[string]any{}), 422)
	expect(t, mallory.post("/logs/search", map[string]any{"project_id": proj}), 404)
	expect(t, alice.post("/ingest", map[string]any{"message": "from browser"}), 401)

	// The tail saw only the ERROR event (filter levels=ERROR).
	timeout := time.After(10 * time.Second)
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatal("tail closed early")
			}
			if !strings.Contains(f, "event: log") {
				continue
			}
			var item map[string]any
			for _, line := range strings.Split(f, "\n") {
				if d, ok := strings.CutPrefix(line, "data: "); ok {
					_ = json.Unmarshal([]byte(d), &item)
				}
			}
			if item["message"] != "Transfer failed" || item["level"] != "ERROR" {
				t.Fatalf("tail delivered %v", item)
			}
			return
		case <-timeout:
			t.Fatal("tail delivered nothing")
		}
	}
}

func TestTailResumeAndAuth(t *testing.T) {
	e := newLogsEnv(t)
	admin := e.setupAdmin()
	alice, mallory := e.newUser(admin, "alice"), e.newUser(admin, "mallory")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "S"}), 201), "id")
	proj := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "P"}), 201), "id")
	tok := expect(t, alice.post("/tokens", map[string]any{"project_id": proj, "name": "t", "scopes": []string{"logs:ingest"}}), 201).JSON()["token"].(string)

	// A stranger's session and an ingest-only token cannot tail; a member's session needs project_id.
	if r := mallory.get("/logs/tail?project_id=" + proj); r.Status != 404 {
		t.Fatalf("stranger tail %d", r.Status)
	}
	if r := alice.bearer("GET", "/logs/tail", tok); r.Status != 403 {
		t.Fatalf("ingest-only tail %d", r.Status)
	}
	if r := alice.get("/logs/tail"); r.Status != 422 {
		t.Fatalf("tail without project %d", r.Status)
	}

	for i := 0; i < 3; i++ {
		expect(t, alice.bearerJSON("POST", "/ingest", tok, map[string]any{"message": "m" + string(rune('0'+i))}), 202)
	}
	deadline := time.Now().Add(15 * time.Second)
	for e.writer.Stats.EventsWritten.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	// Resume after cursor 1 with the session cookie: the backlog holds the two later events.
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/logs/tail?project_id="+proj, nil)
	req.Header.Set("Last-Event-ID", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := alice.http.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	sc := bufio.NewScanner(res.Body)
	var ids []string
	for sc.Scan() && len(ids) < 2 {
		if v, ok := strings.CutPrefix(sc.Text(), "id: "); ok {
			ids = append(ids, v)
		}
	}
	if strings.Join(ids, ",") != "2,3" {
		t.Fatalf("resumed ids %v", ids)
	}
	cancel()

	// A browser resuming after a pause passes the cursor as a query parameter.
	ctxQ, cancelQ := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelQ()
	req, _ = http.NewRequestWithContext(ctxQ, "GET", e.srv.URL+"/api/v1/logs/tail?last_event_id=2&project_id="+proj, nil)
	resQ, err := alice.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resQ.Body.Close()
	sc = bufio.NewScanner(resQ.Body)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "id: "); ok {
			if v != "3" {
				t.Fatalf("query resume id %s, want 3", v)
			}
			break
		}
	}
	cancelQ()

	// A cursor beyond what this instance has seen asks the client to reset.
	req, _ = http.NewRequest("GET", e.srv.URL+"/api/v1/logs/tail?project_id="+proj, nil)
	req.Header.Set("Last-Event-ID", "999999")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	res2, err := alice.http.Do(req.WithContext(ctx2))
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	sc = bufio.NewScanner(res2.Body)
	for sc.Scan() {
		if sc.Text() == "event: reset" {
			return
		}
	}
	t.Fatal("no reset event")
}

func TestStreamAutoCreate(t *testing.T) {
	e := newLogsEnv(t)
	alice := e.newUser(e.setupAdmin(), "alice")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Server"}), 201), "id")
	proj := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "RP"}), 201), "id")
	tok := expect(t, alice.post("/tokens", map[string]any{"project_id": proj, "name": "srv", "scopes": []string{"logs:ingest"}}), 201).JSON()["token"].(string)
	// The project starts with the "default" stream; one more fits under a limit of two.
	expect(t, alice.put("/projects/"+proj+"/settings/ingest.max_streams", map[string]any{"value": 2}), 200)

	r := expect(t, alice.bearerJSON("POST", "/ingest/batch", tok, map[string]any{"events": []map[string]any{
		{"stream": "economy", "message": "a"},
		{"stream": "economy", "message": "b"},
		{"stream": "inventory", "message": "c"},
	}}), 202).JSON()
	errs := r["errors"].([]any)
	if r["accepted"].(float64) != 2 || len(errs) != 1 || !strings.Contains(errs[0].(map[string]any)["reason"].(string), "stream limit") {
		t.Fatalf("batch result %v", r)
	}
	streams := expect(t, alice.get("/projects/"+proj+"/streams"), 200).JSON()["items"].([]any)
	if len(streams) != 2 {
		t.Fatalf("streams %v", streams)
	}
	// The limit holds for manual creation too, and 0 lifts it.
	expect(t, alice.post("/projects/"+proj+"/streams", map[string]string{"name": "inventory"}), 409)
	expect(t, alice.put("/projects/"+proj+"/settings/ingest.max_streams", map[string]any{"value": 0}), 200)
	expect(t, alice.post("/projects/"+proj+"/streams", map[string]string{"name": "inventory"}), 201)
}
