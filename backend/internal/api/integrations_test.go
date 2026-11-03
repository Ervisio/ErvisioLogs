package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestTransition(t *testing.T) {
	for _, c := range []struct{ prev, next, typ, status string }{
		{"ok", "ok", "", ""},
		{"ok", "firing", "alert.firing", "firing"},
		{"firing", "firing", "", ""},
		{"firing", "ok", "alert.resolved", "resolved"},
		{"ok", "error", "alert.error", "error"},
		{"firing", "error", "alert.error", "error"},
		{"error", "ok", "", ""},
		{"error", "firing", "alert.firing", "firing"},
	} {
		typ, status := transition(c.prev, c.next)
		if typ != c.typ || status != c.status {
			t.Errorf("%s -> %s = %q %q, want %q %q", c.prev, c.next, typ, status, c.typ, c.status)
		}
	}
}

func TestCSVSafe(t *testing.T) {
	for in, want := range map[string]string{
		"":             "",
		"hello":        "hello",
		"=SUM(A1:A2)":  "'=SUM(A1:A2)",
		"+1":           "'+1",
		"-1":           "'-1",
		"@cmd":         "'@cmd",
		"\tx":          "'\tx",
		"\rx":          "'\rx",
		"a=b":          "a=b",
		"2026-01-01Z ": "2026-01-01Z ",
	} {
		if got := csvSafe(in); got != want {
			t.Errorf("csvSafe(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseRange(t *testing.T) {
	for in, want := range map[string]time.Duration{"15m": 15 * time.Minute, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour} {
		got, err := parseRange(in)
		if err != nil || got != want {
			t.Errorf("parseRange(%q) = %v %v, want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0m", "-1h", "abc", "1y"} {
		if _, err := parseRange(in); err == nil {
			t.Errorf("parseRange(%q) accepted", in)
		}
	}
}

func TestOTLPLevel(t *testing.T) {
	for _, c := range []struct {
		n    int32
		text string
		want string
	}{
		{1, "", "TRACE"}, {4, "", "TRACE"}, {5, "", "DEBUG"}, {9, "", "INFO"}, {13, "", "WARN"},
		{16, "", "WARN"}, {17, "", "ERROR"}, {21, "", "FATAL"}, {24, "", "FATAL"},
		{0, "error", "ERROR"}, {0, " Warn ", "WARN"}, {0, "", "INFO"}, {0, "loud", "INFO"}, {99, "", "INFO"},
	} {
		if got := otlpLevel(c.n, c.text); got != c.want {
			t.Errorf("otlpLevel(%d, %q) = %q, want %q", c.n, c.text, got, c.want)
		}
	}
}

func TestPutAttr(t *testing.T) {
	m := map[string]any{}
	putAttr(m, "http.request.method", "GET")
	putAttr(m, "http.status", 200)
	putAttr(m, "bad key!", 1)
	putAttr(m, "a", 1)
	putAttr(m, "a.b", 2) // a is a scalar: stored flattened
	putAttr(m, "x.y", 1)
	putAttr(m, "x", "v") // x is an object: the scalar goes to _value
	putAttr(m, "1.2.3.4.5.6.7.8", true)
	got, _ := json.Marshal(m)
	want := `{"1":{"2":{"3":{"4":{"5_6_7_8":true}}}},"a":1,"a_b":2,"bad_key_":1,"http":{"request":{"method":"GET"},"status":200},"x":{"_value":"v","y":1}}`
	if string(got) != want {
		t.Fatalf("putAttr\n got %s\nwant %s", got, want)
	}
}

func TestOTLPEventMapping(t *testing.T) {
	res := otlpResource{attrs: []otlpKV{{"service.name", "api"}, {"host.name", "node-1"}, {"ervisio.stream", "economy"},
		{"cloud.region", "eu"}}}
	sc := otlpScope{name: "lib", version: "1.2"}
	rec := otlpRecord{timeNano: 1_700_000_000_123_456_789, severity: 17, body: "boom",
		attrs:   []otlpKV{{"event.name", "player.join"}, {"ervisio.environment", "prod"}, {"player.id", "42"}},
		traceID: "0102030405060708090a0b0c0d0e0f10", spanID: "0102030405060708"}
	ev := otlpEvent(res, sc, rec)
	if ev.Service != "api" || ev.Host != "node-1" || ev.Stream != "economy" || ev.Environment != "prod" ||
		ev.Event != "player.join" || ev.Level != "ERROR" || ev.Message != "boom" ||
		ev.Timestamp != "2023-11-14T22:13:20.123456789Z" || ev.TraceID != rec.traceID || ev.SpanID != rec.spanID {
		t.Fatalf("mapped %+v", ev)
	}
	var attrs map[string]any
	if err := json.Unmarshal(ev.Attributes, &attrs); err != nil {
		t.Fatal(err)
	}
	want := `{"player":{"id":"42"},"resource":{"cloud":{"region":"eu"}},"scope":{"name":"lib","version":"1.2"}}`
	if got, _ := json.Marshal(attrs); string(got) != want {
		t.Fatalf("attributes\n got %s\nwant %s", got, want)
	}

	// An event name the index cannot hold is kept as an attribute.
	ev = otlpEvent(otlpResource{}, otlpScope{}, otlpRecord{eventName: "has space", observedNano: 1})
	if ev.Event != "" || !strings.Contains(string(ev.Attributes), `"event_name":"has space"`) || ev.Timestamp == "" {
		t.Fatalf("bad event name mapped to %+v", ev)
	}
}

func TestOTLPDecodeProtoAndJSON(t *testing.T) {
	str := func(s string) *commonpb.AnyValue {
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
	}
	msg := &logspb.LogsData{ResourceLogs: []*logspb.ResourceLogs{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{Key: "service.name", Value: str("api")}}},
		ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{{
			TimeUnixNano: 1, SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_WARN, Body: str("hi"),
			TraceId: make([]byte, 16), // all zero: no trace
		}}}},
	}}}
	b, err := proto.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	pr, err := decodeOTLPProto(b)
	if err != nil {
		t.Fatal(err)
	}
	js := `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"api"}}]},
		"scopeLogs":[{"logRecords":[{"timeUnixNano":"1","severityNumber":13,"body":{"stringValue":"hi"},
		"traceId":"00000000000000000000000000000000","unknownField":true}]}]}]}`
	jr, err := decodeOTLPJSON([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	for name, rs := range map[string][]otlpResource{"proto": pr, "json": jr} {
		if len(rs) != 1 || len(rs[0].scopes) != 1 || len(rs[0].scopes[0].records) != 1 {
			t.Fatalf("%s: decoded %+v", name, rs)
		}
		ev := otlpEvent(rs[0], rs[0].scopes[0], rs[0].scopes[0].records[0])
		if ev.Service != "api" || ev.Level != "WARN" || ev.Message != "hi" || ev.TraceID != "" {
			t.Fatalf("%s: mapped %+v", name, ev)
		}
	}
}

func TestOTLPResponses(t *testing.T) {
	if len(otlpResponseProto(0, "")) != 0 || string(otlpResponseJSON(0, "")) != "{}" {
		t.Fatal("full success must be the empty message")
	}
	b := otlpResponseProto(3, "bad")
	num, typ, n := protowire.ConsumeTag(b)
	if num != 1 || typ != protowire.BytesType || n < 0 {
		t.Fatalf("partial_success tag %d %d", num, typ)
	}
	inner, m := protowire.ConsumeBytes(b[n:])
	if m < 0 {
		t.Fatal("partial_success not length-delimited")
	}
	num, _, n = protowire.ConsumeTag(inner)
	v, _ := protowire.ConsumeVarint(inner[n:])
	if num != 1 || v != 3 {
		t.Fatalf("rejected_log_records %d=%d", num, v)
	}
	if got := string(otlpResponseJSON(3, "bad")); got != `{"partialSuccess":{"rejectedLogRecords":"3","errorMessage":"bad"}}` {
		t.Fatalf("json response %s", got)
	}
}
