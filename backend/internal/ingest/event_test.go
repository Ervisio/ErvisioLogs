package ingest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeRes struct{ streams, envs map[string]uuid.UUID }

func (f fakeRes) Stream(n string) (uuid.UUID, bool, error) {
	id, ok := f.streams[n]
	return id, ok, nil
}
func (f fakeRes) Environment(n string) (uuid.UUID, bool, error) {
	id, ok := f.envs[n]
	return id, ok, nil
}

var (
	now   = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	val   = Validator{MaxFutureSkew: 5 * time.Minute, MaxPastAge: 168 * time.Hour, Now: func() time.Time { return now }}
	res   = fakeRes{streams: map[string]uuid.UUID{"default": uuid.New(), "economy": uuid.New()}, envs: map[string]uuid.UUID{"prod": uuid.New()}}
	ws, p = uuid.New(), uuid.New()
)

func TestNormalizeDefaults(t *testing.T) {
	e, err := val.Normalize(InEvent{Message: "hi"}, "", "", res, ws, p)
	if err != nil {
		t.Fatal(err)
	}
	if e.Stream != "default" || e.StreamID != res.streams["default"] {
		t.Fatalf("stream = %q", e.Stream)
	}
	if e.SeverityNumber != 9 || e.SeverityText != "INFO" {
		t.Fatalf("severity = %d %s", e.SeverityNumber, e.SeverityText)
	}
	if !e.Timestamp.Equal(now) || !strings.HasPrefix(e.EventID, "evt_") || string(e.Attributes) != "{}" {
		t.Fatalf("got %+v", e)
	}
	if e.EnvironmentID != uuid.Nil {
		t.Fatal("environment should be empty")
	}
}

func TestNormalizeFull(t *testing.T) {
	in := InEvent{ID: "abc-1", Stream: "economy", Timestamp: "2026-11-03T11:59:00.123456Z", Level: "warning",
		Event: "player.purchase", Message: "m", TraceID: "ABCDEF", Attributes: json.RawMessage(`{ "player": {"id": 7}, "tags":["a"] }`)}
	e, err := val.Normalize(in, "default", "prod", res, ws, p)
	if err != nil {
		t.Fatal(err)
	}
	if e.Stream != "economy" || e.Environment != "prod" || e.SeverityText != "WARN" || e.SeverityNumber != 13 {
		t.Fatalf("got %+v", e)
	}
	if e.Timestamp.Nanosecond() != 123_000_000 || e.TraceID != "abcdef" {
		t.Fatalf("ts=%v trace=%s", e.Timestamp, e.TraceID)
	}
	if string(e.Attributes) != `{"player":{"id":7},"tags":["a"]}` {
		t.Fatalf("attrs = %s", e.Attributes)
	}
}

func TestNormalizeRejects(t *testing.T) {
	deep := `{"a":` + strings.Repeat(`{"a":`, 8) + `1` + strings.Repeat(`}`, 9)
	many := map[string]int{}
	for i := 0; i < MaxAttrKeys+1; i++ {
		many["k"+strings.Repeat("x", i%5)+string(rune('a'+i%26))+uuid.NewString()[:4]] = i
	}
	manyJSON, _ := json.Marshal(many)
	cases := map[string]InEvent{
		"bad id":         {ID: "a b"},
		"long id":        {ID: strings.Repeat("a", MaxIDLen+1)},
		"unknown stream": {Stream: "nope"},
		"bad stream":     {Stream: "Bad Stream"},
		"unknown env":    {Environment: "staging"},
		"bad level":      {Level: "LOUD"},
		"bad timestamp":  {Timestamp: "yesterday"},
		"future":         {Timestamp: now.Add(10 * time.Minute).Format(time.RFC3339)},
		"too old":        {Timestamp: now.Add(-200 * time.Hour).Format(time.RFC3339)},
		"big message":    {Message: strings.Repeat("x", MaxMessageBytes+1)},
		"bad event name": {Event: "has space"},
		"attrs array":    {Attributes: json.RawMessage(`[1]`)},
		"attrs dot key":  {Attributes: json.RawMessage(`{"a.b":1}`)},
		"attrs deep":     {Attributes: json.RawMessage(deep)},
		"attrs many":     {Attributes: manyJSON},
		"attrs invalid":  {Attributes: json.RawMessage(`{"a":}`)},
		"trace not hex":  {TraceID: "zz"},
		"long host":      {Host: strings.Repeat("h", MaxSourceFieldLen+1)},
	}
	for name, in := range cases {
		if _, err := val.Normalize(in, "", "", res, ws, p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRecordRoundTrip(t *testing.T) {
	e, err := val.Normalize(InEvent{ID: "x1", Environment: "prod", Attributes: json.RawMessage(`{"n":1.5}`)}, "", "", res, ws, p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := EncodeRecord([]Event{e})
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeRecord(b)
	if err != nil || len(got) != 1 {
		t.Fatal(err)
	}
	g := got[0]
	if g.EventID != "x1" || g.Stream != "default" || g.Environment != "prod" || !g.Timestamp.Equal(e.Timestamp) ||
		!g.IngestedAt.Equal(e.IngestedAt) || string(g.Attributes) != `{"n":1.5}` || g.StreamID != e.StreamID {
		t.Fatalf("round trip: %+v", g)
	}
}

// A zero Validator must not reject everything: MaxPastAge falls back to the default, and a zero
// MaxFutureSkew refuses only future timestamps.
func TestValidatorZeroLimits(t *testing.T) {
	zero := Validator{Now: func() time.Time { return now }}
	recent := InEvent{Timestamp: now.Add(-time.Hour).Format(time.RFC3339)}
	if _, err := zero.Normalize(recent, "", "", res, ws, p); err != nil {
		t.Fatalf("recent event rejected: %v", err)
	}
	old := InEvent{Timestamp: now.Add(-DefaultMaxPastAge - time.Hour).Format(time.RFC3339)}
	if _, err := zero.Normalize(old, "", "", res, ws, p); err == nil {
		t.Fatal("event older than the default accepted")
	}
	future := InEvent{Timestamp: now.Add(time.Second).Format(time.RFC3339)}
	if _, err := zero.Normalize(future, "", "", res, ws, p); err == nil {
		t.Fatal("future event accepted with zero skew")
	}
}
