// Package migrate is a small, dependency-free SQL migration runner shared by PostgreSQL and ClickHouse.
//
// Migrations are files named NNNN_name.up.sql and (optionally) NNNN_name.down.sql.
// A migration without a down file is irreversible and Down refuses to cross it.
// Applied migrations are recorded with a checksum; editing an applied migration is an error.
package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Migration is one versioned schema change.
type Migration struct {
	Version  int
	Name     string
	Up, Down string
}

// Checksum identifies the exact up script.
func (m Migration) Checksum() string {
	s := sha256.Sum256([]byte(m.Up))
	return hex.EncodeToString(s[:])
}

// Record is a migration stored as applied.
type Record struct {
	Version  int
	Name     string
	Checksum string
}

// Driver abstracts a target database.
type Driver interface {
	// Lock serialises concurrent runners; the returned func releases it.
	Lock(ctx context.Context) (func(), error)
	Init(ctx context.Context) error
	Applied(ctx context.Context) ([]Record, error)
	Apply(ctx context.Context, m Migration) error
	Revert(ctx context.Context, m Migration) error
}

var fileRe = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.(up|down)\.sql$`)

// Load reads migrations from dir inside fsys.
func Load(fsys fs.FS, dir string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	byVersion := map[int]*Migration{}
	for _, e := range entries {
		if e.IsDir() || e.Name() == ".gitkeep" {
			continue
		}
		g := fileRe.FindStringSubmatch(e.Name())
		if g == nil {
			return nil, fmt.Errorf("migrate: unexpected file name %q (want NNNN_name.up.sql)", e.Name())
		}
		v, _ := strconv.Atoi(g[1])
		b, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		m := byVersion[v]
		if m == nil {
			m = &Migration{Version: v, Name: g[2]}
			byVersion[v] = m
		} else if m.Name != g[2] {
			return nil, fmt.Errorf("migrate: version %04d has conflicting names %q and %q", v, m.Name, g[2])
		}
		if g[3] == "up" {
			m.Up = string(b)
		} else {
			m.Down = string(b)
		}
	}
	out := make([]Migration, 0, len(byVersion))
	for _, m := range byVersion {
		if strings.TrimSpace(m.Up) == "" {
			return nil, fmt.Errorf("migrate: version %04d has no up script", m.Version)
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i, m := range out {
		if m.Version != i+1 {
			return nil, fmt.Errorf("migrate: versions must be contiguous from 0001, found gap before %04d", m.Version)
		}
	}
	return out, nil
}

// Status compares the known migrations with those applied.
type Status struct {
	Applied []Record
	Pending []Migration
}

func status(ctx context.Context, d Driver, ms []Migration) (Status, error) {
	if err := d.Init(ctx); err != nil {
		return Status{}, err
	}
	return Verify(ctx, d, ms)
}

// Verify compares applied records with the known migrations without writing anything.
// A missing bookkeeping table surfaces as an error from the driver.
func Verify(ctx context.Context, d Driver, ms []Migration) (Status, error) {
	applied, err := d.Applied(ctx)
	if err != nil {
		return Status{}, err
	}
	applied, err = dedupeRecords(applied)
	if err != nil {
		return Status{}, err
	}
	for i, r := range applied {
		if i >= len(ms) {
			return Status{}, fmt.Errorf("migrate: database has version %04d unknown to this binary (downgrade?)", r.Version)
		}
		if r.Version != ms[i].Version {
			return Status{}, fmt.Errorf("migrate: applied history is not a prefix of known migrations at %04d", r.Version)
		}
		if r.Checksum != ms[i].Checksum() {
			return Status{}, fmt.Errorf("migrate: migration %04d_%s was modified after being applied", r.Version, r.Name)
		}
	}
	return Status{Applied: applied, Pending: ms[len(applied):]}, nil
}

// dedupeRecords sorts records by version and collapses identical duplicates. ClickHouse has no unique
// constraint, so concurrent runners can record the same migration twice; since its scripts must be
// idempotent that is harmless. Two different records for one version are real corruption, and an error.
func dedupeRecords(in []Record) ([]Record, error) {
	sorted := append([]Record(nil), in...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })
	out := sorted[:0]
	for _, r := range sorted {
		if n := len(out); n > 0 && out[n-1].Version == r.Version {
			if out[n-1] != r {
				return nil, fmt.Errorf("migrate: conflicting records for version %04d (%s, %s)", r.Version, out[n-1].Name, r.Name)
			}
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// Check reports the migration state without changing the schema (apart from the bookkeeping table).
func Check(ctx context.Context, d Driver, ms []Migration) (Status, error) {
	unlock, err := d.Lock(ctx)
	if err != nil {
		return Status{}, err
	}
	defer unlock()
	return status(ctx, d, ms)
}

// Up applies all pending migrations in order and returns how many ran.
func Up(ctx context.Context, d Driver, ms []Migration) (int, error) {
	unlock, err := d.Lock(ctx)
	if err != nil {
		return 0, err
	}
	defer unlock()
	st, err := status(ctx, d, ms)
	if err != nil {
		return 0, err
	}
	for i, m := range st.Pending {
		if err := d.Apply(ctx, m); err != nil {
			return i, fmt.Errorf("migrate: apply %04d_%s: %w", m.Version, m.Name, err)
		}
	}
	return len(st.Pending), nil
}

// Down reverts the last steps applied migrations.
func Down(ctx context.Context, d Driver, ms []Migration, steps int) (int, error) {
	if steps < 1 {
		return 0, errors.New("migrate: steps must be >= 1")
	}
	unlock, err := d.Lock(ctx)
	if err != nil {
		return 0, err
	}
	defer unlock()
	st, err := status(ctx, d, ms)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := len(st.Applied) - 1; i >= 0 && n < steps; i-- {
		m := ms[i]
		if strings.TrimSpace(m.Down) == "" {
			return n, fmt.Errorf("migrate: %04d_%s is irreversible (no down script)", m.Version, m.Name)
		}
		if err := d.Revert(ctx, m); err != nil {
			return n, fmt.Errorf("migrate: revert %04d_%s: %w", m.Version, m.Name, err)
		}
		n++
	}
	return n, nil
}

// SplitStatements splits a script on semicolons that end a line, ignoring "--" comments (whole-line or
// trailing). It is intentionally simple: migration scripts must not contain ";" at the end of a line
// inside a string literal.
func SplitStatements(script string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		s := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(cur.String()), ";"))
		if s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for _, line := range strings.Split(script, "\n") {
		line = stripLineComment(line)
		if strings.TrimSpace(line) == "" {
			continue
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
		if strings.HasSuffix(strings.TrimSpace(line), ";") {
			flush()
		}
	}
	flush()
	return out
}

// stripLineComment removes a "--" comment from one line, leaving "--" inside quotes alone.
func stripLineComment(line string) string {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"' || c == '`':
			quote = c
		case c == '-' && i+1 < len(line) && line[i+1] == '-':
			return line[:i]
		}
	}
	return line
}
