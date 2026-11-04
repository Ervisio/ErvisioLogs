package api

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

func TestAttrExpr(t *testing.T) {
	ok := map[string]string{
		"player.license":            "attributes.`player`.`license`",
		"attributes.player.license": "attributes.`player`.`license`",
		"a_b-c":                     "attributes.`a_b-c`",
	}
	for in, want := range ok {
		got, err := attrExpr(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "attributes.", "a..b", "a.`b", "a b", "a.b'", strings.Repeat("a.", 9) + "a"} {
		if got, err := attrExpr(in); err == nil {
			t.Errorf("%q: accepted as %q", in, got)
		}
	}
}

func TestBuildSearch(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	proj := store.Project{ID: uuid.New(), WorkspaceID: uuid.New()}
	sid := uuid.New()
	cat := &catalogEntry{streams: map[string]uuid.UUID{"economy": sid}, envs: map[string]uuid.UUID{}}

	q, err := buildSearch(&searchReq{Streams: []string{"economy"}, Levels: []string{"warning"},
		Filters: []AttrFilter{{Field: "player", Op: "exists"}, {Field: "player.money", Op: "gt", Value: "10"}}}, proj, cat, now)
	if err != nil {
		t.Fatal(err)
	}
	sql := q.sql()
	for _, want := range []string{"workspace_id = ?", "project_id = ?", "stream_id IN ?", "severity_text IN ?",
		"JSONAllPaths(attributes)", "toFloat64OrNull(toString(attributes.`player`.`money`)) > ?"} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL lacks %q: %s", want, sql)
		}
	}
	if strings.Count(sql, "?") != len(q.args) {
		t.Fatalf("%d placeholders, %d args", strings.Count(sql, "?"), len(q.args))
	}
	if q.args[0] != proj.WorkspaceID || q.args[1] != proj.ID {
		t.Fatalf("tenant args %v", q.args[:2])
	}
	if from, to := q.args[2].(int64), q.args[3].(int64); to-from != (24*time.Hour).Milliseconds() || to != now.UnixMilli() {
		t.Fatalf("default range %d..%d", from, to)
	}

	long := now.Add(-32 * 24 * time.Hour)
	later := now.Add(time.Hour)
	bad := map[string]*searchReq{
		"unknown stream": {Streams: []string{"nope"}},
		"unknown level":  {Levels: []string{"LOUD"}},
		"range":          {From: &long},
		"inverted":       {From: &later},
		"op":             {Filters: []AttrFilter{{Field: "a", Op: "like"}}},
		"number":         {Filters: []AttrFilter{{Field: "a", Op: "gt", Value: "ten"}}},
		"value":          {Filters: []AttrFilter{{Field: "a", Op: "eq", Value: strings.Repeat("x", 1025)}}},
	}
	for name, req := range bad {
		if _, err := buildSearch(req, proj, cat, now); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSearchCursorRoundTrip(t *testing.T) {
	c := searchCursor{T: 1762171200000, ID: "evt-a"}
	got, err := decodeCursor(encodeCursor(c))
	if err != nil || got != c {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := decodeCursor("!!"); err == nil {
		t.Fatal("bad cursor accepted")
	}
}

func TestMatchesSearchLevel(t *testing.T) {
	for _, tc := range []struct {
		name     string
		req      searchReq
		level    string
		severity uint8
		want     bool
	}{
		{"unfiltered", searchReq{}, "INFO", 9, true},
		{"alias", searchReq{Levels: []string{"warning"}}, "WARN", 13, true},
		{"other level", searchReq{Levels: []string{"ERROR"}}, "INFO", 9, false},
		{"minimum", searchReq{SeverityMin: 13}, "INFO", 9, false},
		{"inclusive minimum", searchReq{SeverityMin: 13}, "WARN", 13, true},
		{"combined", searchReq{SeverityMin: 17, Levels: []string{"WARN"}}, "WARN", 13, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesSearchLevel(&tc.req, tc.level, tc.severity); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
