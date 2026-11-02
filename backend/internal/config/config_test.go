package config

import (
	"errors"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func noFiles(string) ([]byte, error) { return nil, errors.New("no such file") }

func valid() map[string]string {
	return map[string]string{
		"ERVISIO_LOGS_POSTGRES_DSN":        "postgres://u:p@pg:5432/db?sslmode=disable",
		"ERVISIO_LOGS_CLICKHOUSE_PASSWORD": "s3cret",
	}
}

func TestLoadDefaults(t *testing.T) {
	c, err := load(env(valid()), noFiles)
	if err != nil {
		t.Fatal(err)
	}
	if c.Env != EnvDevelopment || c.HTTPAddr != ":8080" || c.MaxBodyBytes != 1<<20 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestLoadRequiresSecrets(t *testing.T) {
	_, err := load(env(nil), noFiles)
	if err == nil || !strings.Contains(err.Error(), "POSTGRES_DSN") || !strings.Contains(err.Error(), "CLICKHOUSE_PASSWORD") {
		t.Fatalf("expected missing-secret errors, got %v", err)
	}
}

func TestSecretFromFile(t *testing.T) {
	m := valid()
	delete(m, "ERVISIO_LOGS_CLICKHOUSE_PASSWORD")
	m["ERVISIO_LOGS_CLICKHOUSE_PASSWORD_FILE"] = "/run/secrets/ch"
	c, err := load(env(m), func(p string) ([]byte, error) { return []byte("fromfile\n"), nil })
	if err != nil || c.ClickHouse.Password != "fromfile" {
		t.Fatalf("got %q, %v", c.ClickHouse.Password, err)
	}
}

func TestSecretAndFileMutuallyExclusive(t *testing.T) {
	m := valid()
	m["ERVISIO_LOGS_CLICKHOUSE_PASSWORD_FILE"] = "/x"
	_, err := load(env(m), func(string) ([]byte, error) { return []byte("x"), nil })
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("got %v", err)
	}
}

func TestProductionRefusesWeakConfig(t *testing.T) {
	m := valid()
	m["ERVISIO_LOGS_ENV"] = "production"
	m["ERVISIO_LOGS_CLICKHOUSE_PASSWORD"] = "change_me"
	_, err := load(env(m), noFiles)
	if err == nil || !strings.Contains(err.Error(), "sslmode=disable") || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("got %v", err)
	}
}

func TestInvalidValues(t *testing.T) {
	m := valid()
	m["ERVISIO_LOGS_ENV"] = "staging"
	m["ERVISIO_LOGS_LOG_LEVEL"] = "loud"
	m["ERVISIO_LOGS_SHUTDOWN_TIMEOUT"] = "-1s"
	m["ERVISIO_LOGS_MAX_BODY_BYTES"] = "0"
	_, err := load(env(m), noFiles)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"ENV must be", "LOG_LEVEL", "SHUTDOWN_TIMEOUT", "MAX_BODY_BYTES"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

func TestRedactedHasNoSecrets(t *testing.T) {
	c, _ := load(env(valid()), noFiles)
	for k, v := range c.Redacted() {
		if s, ok := v.(string); ok && (strings.Contains(s, "s3cret") || strings.Contains(s, "u:p")) {
			t.Fatalf("secret leaked in %s", k)
		}
	}
}
