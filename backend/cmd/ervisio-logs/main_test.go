package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestMigrateDownRequiresStore(t *testing.T) {
	// run must refuse before loading configuration or touching a database.
	t.Setenv("ERVISIO_LOGS_POSTGRES_DSN", "")
	err := run([]string{"migrate", "down"})
	if err == nil || !strings.Contains(err.Error(), "--store") {
		t.Fatalf("got %v", err)
	}
}

func TestParseMigrateArgs(t *testing.T) {
	valid := []struct {
		sub    string
		args   []string
		stores []string
		steps  int
	}{
		{"down", []string{"--store", "postgres"}, []string{"postgres"}, 1},
		{"down", []string{"--store=clickhouse", "-n", "2"}, []string{"clickhouse"}, 2},
		{"up", nil, []string{"postgres", "clickhouse"}, 1},
		{"up", []string{"--store", "clickhouse"}, []string{"clickhouse"}, 1},
		{"status", nil, []string{"postgres", "clickhouse"}, 1},
	}
	for _, c := range valid {
		o, err := parseMigrateArgs(c.sub, c.args)
		if err != nil {
			t.Errorf("%s %v: %v", c.sub, c.args, err)
			continue
		}
		if !reflect.DeepEqual(o.stores(), c.stores) || o.steps != c.steps {
			t.Errorf("%s %v: got %+v", c.sub, c.args, o)
		}
	}
	invalid := []struct {
		sub  string
		args []string
	}{
		{"down", nil},
		{"down", []string{"-n", "2"}},
		{"down", []string{"--store", "mysql"}},
		{"down", []string{"--store", "postgres", "-n", "0"}},
		{"down", []string{"--store", "postgres", "extra"}},
		{"up", []string{"--store", "mysql"}},
		{"sideways", nil},
	}
	for _, c := range invalid {
		if _, err := parseMigrateArgs(c.sub, c.args); err == nil {
			t.Errorf("%s %v: expected error", c.sub, c.args)
		}
	}
}
