package ingest

import (
	"os"
	"path/filepath"
	"testing"
)

// Two spools must never share a dedup token for the same sequence range: both start at 1, and
// ClickHouse would drop the second spool's data as a copy of the first's.
func TestSpoolIDIsStableAndUnique(t *testing.T) {
	d1, d2 := t.TempDir(), t.TempDir()
	a, err := loadSpoolID(d1)
	if err != nil {
		t.Fatal(err)
	}
	again, err := loadSpoolID(d1)
	if err != nil || again != a {
		t.Fatalf("reload: %q, %v; want %q", again, err, a)
	}
	b, err := loadSpoolID(d2)
	if err != nil {
		t.Fatal(err)
	}
	w1, w2 := &Writer{spoolID: a}, &Writer{spoolID: b}
	if w1.tokenFor(1, 3) == w2.tokenFor(1, 3) {
		t.Fatal("two spools produced the same token")
	}
	if w1.tokenFor(1, 3) == w1.tokenFor(1, 4) {
		t.Fatal("two ranges produced the same token")
	}

	if err := os.WriteFile(filepath.Join(d1, spoolIDName), []byte("short\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSpoolID(d1); err == nil {
		t.Fatal("damaged spool id accepted")
	}
}
