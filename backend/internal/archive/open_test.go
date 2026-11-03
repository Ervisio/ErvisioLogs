package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpen(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s3.json"), []byte(`{"access_key":"a","secret_key":"b"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cases := []struct {
		typ, cfg, secret, wantErr string
	}{
		{"local", `{"root":` + jsonString(root) + `}`, "", ""},
		{"local", `{"root":"relative"}`, "", "absolute"},
		{"local", `{"root":` + jsonString(root) + `,"rot":"x"}`, "", "unknown field"},
		{"s3", `{"endpoint":"http://127.0.0.1:8333","bucket":"b"}`, "s3.json", ""},
		{"s3", `{"endpoint":"http://127.0.0.1:8333","bucket":"b"}`, "", "secret file"},
		{"s3", `{"endpoint":"http://127.0.0.1:8333","bucket":"b"}`, "../s3.json", "plain file name"},
		{"s3", `{"endpoint":"http://127.0.0.1:8333","bucket":"b"}`, "missing", "cannot read secret file missing"},
		{"s3", `{"endpoint":"ftp://x","bucket":"b"}`, "s3.json", "endpoint"},
		{"s3", `{"endpoint":"http://x","bucket":"b","access_key":"inline"}`, "s3.json", "unknown field"},
		{"sftp", `{"host":"h","user":"u","host_key":"nope"}`, "s3.json", "host_key"},
		{"gcs", `{}`, "", "unknown provider type"},
	}
	for _, c := range cases {
		p, err := Open(c.typ, json.RawMessage(c.cfg), c.secret, dir)
		if c.wantErr == "" {
			if err != nil {
				t.Errorf("%s %s: %v", c.typ, c.cfg, err)
				continue
			}
			p.Close()
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s %s: got %v, want %q", c.typ, c.cfg, err, c.wantErr)
		}
		if err != nil && strings.Contains(err.Error(), `"b"`) {
			t.Errorf("error leaks the secret: %v", err)
		}
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
