//go:build integration

package archive

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

// Orphan cleanup removes old objects nobody owns and keeps everything the catalog points at,
// everything recent, and every file outside the archive layout.
func TestReconcileOrphans(t *testing.T) {
	a, p, _ := archiverEnv(t)
	ctx := context.Background()
	lp := p.(*LocalFS)
	active, err := a.Store.ActiveProvider(ctx)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	put := func(key string, age time.Time) {
		t.Helper()
		fp := filepath.Join(lp.root, filepath.FromSlash(key))
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fp, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(fp, age, age); err != nil {
			t.Fatal(err)
		}
	}
	exists := func(key string) bool {
		_, err := os.Stat(filepath.Join(lp.root, filepath.FromSlash(key)))
		return err == nil
	}

	ws, proj, stream := uuid.New(), uuid.New(), uuid.New()
	day := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -5)
	seg, err := a.Store.StartSegment(ctx, store.NewSegment{WorkspaceID: ws, ProjectID: proj, StreamID: stream, Day: day,
		ProviderID: active.ID, CutoffTo: time.Now().UTC(), SchemaVersion: SchemaVersion,
		KeyFor: func(id uuid.UUID, g int32) string { return ObjectKey(ws, proj, stream, day, g, id) }})
	if err != nil {
		t.Fatal(err)
	}
	owned := seg.ObjectKey
	pendingTmp := tmpKey(seg.ID)
	orphan := ObjectKey(ws, proj, stream, day, 9, uuid.New())
	recent := ObjectKey(ws, proj, stream, day, 10, uuid.New())
	deadTmp := tmpKey(uuid.New())
	foreign := "backups/notes.txt"
	odd := "v1/README"
	for _, k := range []string{owned, pendingTmp, orphan, deadTmp, foreign, odd} {
		put(k, old)
	}
	put(recent, time.Now())

	rep, err := a.ReconcileOrphans(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Providers != 1 || rep.Deleted != 2 || rep.Failed != 0 || rep.Listed != 7 {
		t.Fatalf("report %+v", rep)
	}
	for k, want := range map[string]bool{owned: true, pendingTmp: true, recent: true, foreign: true, odd: true, orphan: false, deadTmp: false} {
		if exists(k) != want {
			t.Errorf("%s exists=%v, want %v", k, !want, want)
		}
	}

	// A segment that failed for good can no longer promote its temporary object.
	if _, err := a.Store.FailSegmentAttempt(ctx, seg.ID, bytes.ErrTooLarge, time.Now(), 1); err != nil {
		t.Fatal(err)
	}
	if rep, err = a.ReconcileOrphans(ctx, time.Now()); err != nil || rep.Deleted != 1 {
		t.Fatalf("after failure: %+v %v", rep, err)
	}
	if exists(pendingTmp) || !exists(owned) {
		t.Fatal("failed segment: temporary object must go, the cataloged key row still owns the final key")
	}

	// The pass runs reconciliation once, then waits a day.
	r1, err := a.RunOnce(ctx)
	if err != nil || r1.Orphans == nil {
		t.Fatalf("first pass %+v %v", r1, err)
	}
	r2, err := a.RunOnce(ctx)
	if err != nil || r2.Orphans != nil {
		t.Fatalf("second pass %+v %v", r2, err)
	}
}

func TestPartFileNames(t *testing.T) {
	for k, want := range map[string]bool{
		"v1/a/b/.g1-x.parquet.part-abc": true,
		"tmp/.x.parquet.part-1":         true,
		"v1/a/g1-x.parquet":             false,
		"v1/a/.hidden":                  false,
	} {
		if isPartFile(k) != want {
			t.Errorf("isPartFile(%q) = %v", k, !want)
		}
	}
	if _, ok := tmpID("tmp/" + uuid.NewString() + ".parquet"); !ok {
		t.Error("tmpID rejects a real temporary key")
	}
	if _, ok := tmpID("tmp/../x.parquet"); ok {
		t.Error("tmpID accepts a bad key")
	}
}
