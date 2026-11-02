package migrate

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoad(t *testing.T) {
	fsys := fstest.MapFS{
		"m/0001_init.up.sql":   {Data: []byte("CREATE TABLE a (x int);")},
		"m/0001_init.down.sql": {Data: []byte("DROP TABLE a;")},
		"m/0002_more.up.sql":   {Data: []byte("SELECT 1;")},
		"m/.gitkeep":           {},
	}
	ms, err := Load(fsys, "m")
	if err != nil || len(ms) != 2 || ms[0].Down == "" || ms[1].Down != "" {
		t.Fatalf("got %+v, %v", ms, err)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]fstest.MapFS{
		"bad name": {"m/init.sql": {Data: []byte("x")}},
		"gap":      {"m/0001_a.up.sql": {Data: []byte("x")}, "m/0003_c.up.sql": {Data: []byte("x")}},
		"no up":    {"m/0001_a.down.sql": {Data: []byte("x")}},
	}
	for name, f := range cases {
		if _, err := Load(f, "m"); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestSplitStatements(t *testing.T) {
	got := SplitStatements("-- c\nCREATE TABLE a (\n x int\n);\n\n-- d\nINSERT INTO a VALUES (1);\nSELECT 2")
	want := []string{"CREATE TABLE a (\n x int\n)", "INSERT INTO a VALUES (1)", "SELECT 2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

// fake is an in-memory Driver.
type fake struct {
	rec     []Record
	applied []int
	failOn  int
}

func (f *fake) Lock(context.Context) (func(), error) { return func() {}, nil }
func (f *fake) Init(context.Context) error           { return nil }
func (f *fake) Applied(context.Context) ([]Record, error) {
	return append([]Record(nil), f.rec...), nil
}
func (f *fake) Apply(_ context.Context, m Migration) error {
	if m.Version == f.failOn {
		return errors.New("boom")
	}
	f.rec = append(f.rec, Record{m.Version, m.Name, m.Checksum()})
	f.applied = append(f.applied, m.Version)
	return nil
}
func (f *fake) Revert(_ context.Context, m Migration) error {
	f.rec = f.rec[:len(f.rec)-1]
	return nil
}

func set() []Migration {
	return []Migration{
		{1, "a", "CREATE a;", "DROP a;"},
		{2, "b", "CREATE b;", "DROP b;"},
		{3, "c", "CREATE c;", ""},
	}
}

func TestUpIsIdempotent(t *testing.T) {
	f, ctx := &fake{}, context.Background()
	if n, err := Up(ctx, f, set()); n != 3 || err != nil {
		t.Fatalf("first: %d %v", n, err)
	}
	if n, err := Up(ctx, f, set()); n != 0 || err != nil {
		t.Fatalf("second: %d %v", n, err)
	}
}

func TestUpStopsOnFailureAndResumes(t *testing.T) {
	f, ctx := &fake{failOn: 2}, context.Background()
	if n, err := Up(ctx, f, set()); n != 1 || err == nil {
		t.Fatalf("got %d %v", n, err)
	}
	f.failOn = 0
	if n, err := Up(ctx, f, set()); n != 2 || err != nil {
		t.Fatalf("resume: %d %v", n, err)
	}
}

func TestChecksumTamperDetected(t *testing.T) {
	f, ctx := &fake{}, context.Background()
	_, _ = Up(ctx, f, set())
	ms := set()
	ms[0].Up = "CREATE a_changed;"
	if _, err := Check(ctx, f, ms); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("got %v", err)
	}
}

func TestDowngradeDetected(t *testing.T) {
	f, ctx := &fake{}, context.Background()
	_, _ = Up(ctx, f, set())
	if _, err := Check(ctx, f, set()[:2]); err == nil {
		t.Fatal("expected unknown-version error")
	}
}

func TestDownRefusesIrreversible(t *testing.T) {
	f, ctx := &fake{}, context.Background()
	_, _ = Up(ctx, f, set())
	n, err := Down(ctx, f, set(), 2)
	if n != 0 || err == nil || !strings.Contains(err.Error(), "irreversible") {
		t.Fatalf("got %d %v", n, err)
	}
	f.rec = f.rec[:2] // pretend 0003 was never applied
	if n, err := Down(ctx, f, set(), 2); n != 2 || err != nil {
		t.Fatalf("got %d %v", n, err)
	}
}
