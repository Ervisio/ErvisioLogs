package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/ingest"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

func TestTailAttributeAndTagFilters(t *testing.T) {
	e := ingest.Event{Tags: []string{"economy", "trade"}, Attributes: json.RawMessage(`{"actor":{"id":7,"name":"Alice"},"amount":42,"nil":null}`)}
	cases := []struct {
		filter AttrFilter
		want   bool
	}{
		{AttrFilter{"actor.id", "eq", "7"}, true}, {AttrFilter{"actor.name", "contains", "LIC"}, true},
		{AttrFilter{"amount", "gte", "42"}, true}, {AttrFilter{"amount", "gt", "42"}, false},
		{AttrFilter{"actor", "exists", ""}, true}, {AttrFilter{"nil", "exists", ""}, false},
		{AttrFilter{"missing", "neq", "x"}, false},
	}
	for _, tc := range cases {
		f := tailFilter{tags: []string{"economy"}, filters: []AttrFilter{tc.filter}}
		if got := f.match(&e); got != tc.want {
			t.Errorf("%+v = %v", tc.filter, got)
		}
	}
	if (&tailFilter{tags: []string{"unknown"}}).match(&e) {
		t.Fatal("matched missing tag")
	}
}

func TestRedactAttributesBeforeDurability(t *testing.T) {
	raw := json.RawMessage(`{"actor":{"id":7,"name":"Alice"},"secret":"private","visible":true,"large_id":9007199254740993}`)
	rules := []store.FieldRule{{Field: "attributes.actor.name", Redact: true}, {Field: "secret", Redact: true}, {Field: "missing.path", Redact: true}}
	out, err := redactAttributes(raw, rules)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "Alice") || strings.Contains(string(out), "private") || !strings.Contains(string(out), `"id":7`) || !strings.Contains(string(out), `9007199254740993`) {
		t.Fatalf("bad redaction %s", out)
	}
	if err := validateFieldRules([]store.FieldRule{{Field: "actor.name"}, {Field: "attributes.actor.name"}}); err == nil {
		t.Fatal("duplicate alias accepted")
	}
	if err := validateFieldRules([]store.FieldRule{{Field: "secret", Format: "html"}}); err == nil {
		t.Fatal("invalid format accepted")
	}
}

func TestSearchTagsAreBoundAndValidated(t *testing.T) {
	q, err := buildSearch(&searchReq{Tags: []string{"economy"}}, store.Project{}, &catalogEntry{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q.sql(), "has(tags, ?)") || q.args[len(q.args)-1] != "economy" {
		t.Fatalf("unbound tag: %+v", q)
	}
	if _, err := buildSearch(&searchReq{Tags: []string{"UPPER"}}, store.Project{}, &catalogEntry{}, time.Now()); err == nil {
		t.Fatal("invalid tag accepted")
	}
}
