package wal

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func open(t *testing.T, dir string, o Options) *Segmented {
	t.Helper()
	o.Dir = dir
	l, err := Open(o)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return l
}

func mustAppend(t *testing.T, l Log, s string) uint64 {
	t.Helper()
	seq, err := l.Append(context.Background(), []byte(s))
	if err != nil {
		t.Fatalf("append %q: %v", s, err)
	}
	return seq
}

func readAll(t *testing.T, l Log, from uint64) []Record {
	t.Helper()
	var out []Record
	for {
		rs, err := l.ReadFrom(from, 100, 1<<20)
		if err != nil {
			t.Fatalf("read from %d: %v", from, err)
		}
		if len(rs) == 0 {
			return out
		}
		out = append(out, rs...)
		from = rs[len(rs)-1].Seq + 1
	}
}

func TestAppendReadCommitReopen(t *testing.T) {
	dir := t.TempDir()
	l := open(t, dir, Options{SegmentBytes: 64})
	for i := 1; i <= 10; i++ {
		if seq := mustAppend(t, l, fmt.Sprintf("rec-%02d", i)); seq != uint64(i) {
			t.Fatalf("seq %d, want %d", seq, i)
		}
	}
	rs := readAll(t, l, 1)
	if len(rs) != 10 || string(rs[9].Data) != "rec-10" {
		t.Fatalf("read %d records, last %q", len(rs), rs[len(rs)-1].Data)
	}
	if l.Stats().Segments < 3 {
		t.Fatalf("expected rotation, got %d segments", l.Stats().Segments)
	}
	if err := l.Commit(6); err != nil {
		t.Fatal(err)
	}
	before := l.Stats()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	l = open(t, dir, Options{SegmentBytes: 64})
	defer l.Close()
	if l.Checkpoint() != 6 {
		t.Fatalf("checkpoint %d after reopen, want 6", l.Checkpoint())
	}
	rs = readAll(t, l, l.Checkpoint())
	if len(rs) != 5 || rs[0].Seq != 6 || string(rs[0].Data) != "rec-06" {
		t.Fatalf("replay after reopen: %d records, first %+v", len(rs), rs)
	}
	if got := l.Stats(); got.Bytes != before.Bytes || got.Segments != before.Segments {
		t.Fatalf("stats changed across reopen: %+v vs %+v", got, before)
	}
	if seq := mustAppend(t, l, "rec-11"); seq != 11 {
		t.Fatalf("numbering after reopen: %d", seq)
	}
}

func TestCommitDeletesConsumedSegments(t *testing.T) {
	dir := t.TempDir()
	l := open(t, dir, Options{SegmentBytes: 40})
	defer l.Close()
	for i := 0; i < 20; i++ {
		mustAppend(t, l, "0123456789")
	}
	readAll(t, l, 1)
	if err := l.Commit(21); err != nil {
		t.Fatal(err)
	}
	st := l.Stats()
	if st.Segments != 1 {
		t.Fatalf("%d segments left after full commit, want only the active one", st.Segments)
	}
	if _, err := l.ReadFrom(1, 10, 1<<20); err == nil {
		t.Fatal("reading a deleted range must fail")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"+segmentExt))
	if len(files) != 1 {
		t.Fatalf("%d segment files on disk", len(files))
	}
}

func TestFullyConsumedLogKeepsNumberingAfterReopen(t *testing.T) {
	dir := t.TempDir()
	l := open(t, dir, Options{SegmentBytes: 30})
	for i := 0; i < 5; i++ {
		mustAppend(t, l, "abcdefghij")
	}
	if err := l.Commit(6); err != nil {
		t.Fatal(err)
	}
	l.Close()
	l = open(t, dir, Options{SegmentBytes: 30})
	defer l.Close()
	if seq := mustAppend(t, l, "next"); seq != 6 {
		t.Fatalf("seq %d after reopen of a consumed log, want 6 (sequence numbers must never repeat)", seq)
	}
}

func TestTornTailIsTruncated(t *testing.T) {
	for _, tc := range []struct {
		name string
		tail func(full []byte) []byte
	}{
		{"partial header", func([]byte) []byte { return []byte{1, 2, 3} }},
		{"partial payload", func(f []byte) []byte { return f[:len(f)-2] }},
		{"bad checksum", func(f []byte) []byte { c := bytes.Clone(f); c[len(c)-1] ^= 0xff; return c }},
		{"zeroed block", func(f []byte) []byte { return make([]byte, len(f)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			l := open(t, dir, Options{})
			mustAppend(t, l, "one")
			mustAppend(t, l, "two")
			l.Close()
			seg := filepath.Join(dir, segName(1))
			f, _ := os.OpenFile(seg, os.O_WRONLY|os.O_APPEND, 0)
			f.Write(tc.tail(appendFrame(nil, 3, []byte("three-unacked"))))
			f.Close()

			l = open(t, dir, Options{})
			defer l.Close()
			if l.Stats().TruncatedBytes == 0 {
				t.Fatal("torn tail not reported")
			}
			rs := readAll(t, l, 1)
			if len(rs) != 2 || string(rs[1].Data) != "two" {
				t.Fatalf("acknowledged records damaged: %+v", rs)
			}
			if seq := mustAppend(t, l, "three"); seq != 3 {
				t.Fatalf("seq after truncation %d", seq)
			}
			if rs := readAll(t, l, 3); len(rs) != 1 || string(rs[0].Data) != "three" {
				t.Fatalf("append after truncation unreadable: %+v", rs)
			}
		})
	}
}

func TestCorruptSealedSegmentRefusesToOpen(t *testing.T) {
	dir := t.TempDir()
	l := open(t, dir, Options{SegmentBytes: 30})
	for i := 0; i < 4; i++ {
		mustAppend(t, l, "abcdefghij")
	}
	l.Close()
	seg := filepath.Join(dir, segName(1))
	b, _ := os.ReadFile(seg)
	b[headerSize+2] ^= 0xff
	os.WriteFile(seg, b, 0o600)
	if _, err := Open(Options{Dir: dir, SegmentBytes: 30}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("got %v, want ErrCorrupt", err)
	}
}

func TestDamagedCheckpointRefusesToOpen(t *testing.T) {
	dir := t.TempDir()
	l := open(t, dir, Options{})
	mustAppend(t, l, "x")
	l.Commit(2)
	l.Close()
	os.WriteFile(filepath.Join(dir, checkpointName), []byte("garbage"), 0o600)
	if _, err := Open(Options{Dir: dir}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("got %v, want ErrCorrupt", err)
	}
}

func TestDiskBudget(t *testing.T) {
	dir := t.TempDir()
	rec := strings.Repeat("x", 84) // 100 bytes framed
	l := open(t, dir, Options{SegmentBytes: 200, MaxBytes: 500})
	defer l.Close()
	for i := 0; i < 5; i++ {
		mustAppend(t, l, rec)
	}
	if _, err := l.Append(context.Background(), []byte(rec)); !errors.Is(err, ErrFull) {
		t.Fatalf("got %v, want ErrFull", err)
	}
	if l.Stats().Bytes > 500 {
		t.Fatalf("budget exceeded: %d", l.Stats().Bytes)
	}
	// Consuming frees sealed segments, and appends work again.
	readAll(t, l, 1)
	if err := l.Commit(5); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(context.Background(), []byte(rec)); err != nil {
		t.Fatalf("append after commit: %v", err)
	}
	if _, err := l.Append(context.Background(), make([]byte, MaxRecordBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("got %v, want ErrTooLarge", err)
	}
}

func TestConcurrentAppendsShareFsync(t *testing.T) {
	dir := t.TempDir()
	l := open(t, dir, Options{SegmentBytes: 4096})
	defer l.Close()
	const n = 400
	var wg sync.WaitGroup
	seen := make([]bool, n+1)
	var mu sync.Mutex
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			seq, err := l.Append(context.Background(), []byte(strconv.Itoa(i)))
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			if seen[seq] {
				t.Errorf("duplicate seq %d", seq)
			}
			seen[seq] = true
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	rs := readAll(t, l, 1)
	if len(rs) != n {
		t.Fatalf("read %d records, want %d", len(rs), n)
	}
	st := l.Stats()
	t.Logf("%d appends, %d fsyncs", st.Appends, st.Syncs)
	if st.Syncs >= st.Appends {
		t.Logf("no group commit happened in this run (scheduling dependent)")
	}
}

func TestNotify(t *testing.T) {
	l := open(t, t.TempDir(), Options{})
	defer l.Close()
	ch := l.Notify()
	errc := make(chan error, 1)
	go func() {
		_, err := l.Append(context.Background(), []byte("x"))
		errc <- err
	}()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("notify not closed after append")
	}
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
}

func TestClosed(t *testing.T) {
	l := open(t, t.TempDir(), Options{})
	l.Close()
	if _, err := l.Append(context.Background(), []byte("x")); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
}

// TestKillDuringAppends runs a child process that appends and prints each acknowledged sequence
// number, kills it without warning, and checks every acknowledged record survives with the right
// content and order. This is the "kill -9" check from ADR 0003.
func TestKillDuringAppends(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a process")
	}
	dir := t.TempDir()
	for round := 0; round < 3; round++ {
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperAppender")
		cmd.Env = append(os.Environ(), "WAL_HELPER_DIR="+dir)
		out, _ := cmd.StdoutPipe()
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		acked := map[uint64]string{}
		sc := bufio.NewScanner(out)
		for sc.Scan() && len(acked) < 300 {
			seq, payload, ok := strings.Cut(sc.Text(), " ")
			if !ok {
				continue
			}
			n, _ := strconv.ParseUint(seq, 10, 64)
			acked[n] = payload
		}
		if err := cmd.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		cmd.Wait()
		if len(acked) == 0 {
			t.Fatal("helper acknowledged nothing")
		}

		l := open(t, dir, Options{SegmentBytes: 2048})
		got := map[uint64]string{}
		var last uint64
		for _, r := range readAll(t, l, 1) {
			if r.Seq != last+1 {
				t.Fatalf("round %d: replay out of order: %d after %d", round, r.Seq, last)
			}
			last = r.Seq
			got[r.Seq] = string(r.Data)
		}
		for seq, want := range acked {
			if got[seq] != want {
				t.Fatalf("round %d: acknowledged seq %d lost or changed: %q vs %q", round, seq, got[seq], want)
			}
		}
		t.Logf("round %d: %d acknowledged, %d on disk, %d torn bytes cut", round, len(acked), len(got), l.Stats().TruncatedBytes)
		l.Close()
	}
}

func TestHelperAppender(t *testing.T) {
	dir := os.Getenv("WAL_HELPER_DIR")
	if dir == "" {
		t.Skip("helper process only")
	}
	l, err := Open(Options{Dir: dir, SegmentBytes: 2048})
	if err != nil {
		fmt.Println("open error", err)
		os.Exit(2)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	w := bufio.NewWriter(os.Stdout)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; ; i++ {
				p := fmt.Sprintf("g%d-i%d-%s", g, i, strings.Repeat("p", i%50))
				seq, err := l.Append(context.Background(), []byte(p))
				if err != nil {
					return
				}
				mu.Lock()
				fmt.Fprintf(w, "%d %s\n", seq, p)
				w.Flush()
				mu.Unlock()
			}
		}(g)
	}
	wg.Wait()
}
