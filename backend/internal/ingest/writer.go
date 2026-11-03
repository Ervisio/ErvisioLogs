package ingest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/Ervisio/ErvisioLogs/backend/internal/wal"
)

// Writer moves WAL records into ClickHouse. One insert covers a group of consecutive records. Before
// the insert, the writer stores the group's sequence range in the spool directory, so a restart
// re-sends the same range with the same insert_deduplication_token and ClickHouse drops the copy.
type Writer struct {
	Log        wal.Log
	CH         driver.Conn
	Dir        string // spool directory, for the in-flight file
	Hub        *Hub   // optional: receives events after they are stored
	Logger     *slog.Logger
	MaxRecords int           // records per insert, default 256
	MaxBytes   int           // WAL bytes per insert, default 16 MiB
	Linger     time.Duration // wait for more records when a group is small, default 200 ms
	RetryMax   time.Duration // longest back-off between failed inserts, default 30 s
	Stats      WriterStats

	spoolID string // random, created with the spool directory; part of every dedup token
}

// WriterStats are read by /metrics.
type WriterStats struct {
	Inserts       atomic.Uint64
	InsertErrors  atomic.Uint64
	EventsWritten atomic.Uint64
	BadRecords    atomic.Uint64
	LastInsertMs  atomic.Int64
	LagSeconds    atomic.Int64 // age of the oldest event not yet in ClickHouse, sampled per group
}

const (
	inflightName = "inflight"
	spoolIDName  = "spool-id"
)

func (w *Writer) defaults() {
	if w.MaxRecords <= 0 {
		w.MaxRecords = 256
	}
	if w.MaxBytes <= 0 {
		w.MaxBytes = 16 << 20
	}
	if w.Linger <= 0 {
		w.Linger = 200 * time.Millisecond
	}
	if w.RetryMax <= 0 {
		w.RetryMax = 30 * time.Second
	}
	if w.Logger == nil {
		w.Logger = slog.Default()
	}
}

// Run blocks until ctx ends. It returns nil on cancellation.
func (w *Writer) Run(ctx context.Context) error {
	w.defaults()
	id, err := loadSpoolID(w.Dir)
	if err != nil {
		return fmt.Errorf("spool id: %w", err)
	}
	w.spoolID = id
	// Finish a group a previous process started.
	if first, last, ok := w.readInflight(); ok && first == w.Log.Checkpoint() {
		recs, err := w.Log.ReadFrom(first, int(last-first+1), 1<<31-1)
		if err != nil {
			return err
		}
		if len(recs) > 0 && recs[len(recs)-1].Seq == last {
			w.Logger.Info("re-sending in-flight WAL group", "first", first, "last", last)
			if err := w.flush(ctx, recs); err != nil {
				return nilOnCancel(ctx, err)
			}
		}
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		recs, err := w.Log.ReadFrom(w.Log.Checkpoint(), w.MaxRecords, w.MaxBytes)
		if err != nil {
			return fmt.Errorf("read wal: %w", err)
		}
		if len(recs) == 0 {
			wal.WaitTimeout(ctx, w.Log.Notify(), time.Second)
			continue
		}
		if len(recs) < w.MaxRecords {
			// Small group: give concurrent requests a moment so ClickHouse gets fewer, larger parts.
			wal.WaitTimeout(ctx, nil, w.Linger)
			if more, err := w.Log.ReadFrom(w.Log.Checkpoint(), w.MaxRecords, w.MaxBytes); err == nil && len(more) > len(recs) {
				recs = more
			}
		}
		if err := w.flush(ctx, recs); err != nil {
			return nilOnCancel(ctx, err)
		}
	}
}

func nilOnCancel(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// flush inserts recs, retrying until it succeeds or ctx ends, then commits the WAL.
func (w *Writer) flush(ctx context.Context, recs []wal.Record) error {
	first, last := recs[0].Seq, recs[len(recs)-1].Seq
	if err := w.writeInflight(first, last); err != nil {
		return err
	}
	body, events, oldest := w.encode(recs)
	if !oldest.IsZero() {
		w.Stats.LagSeconds.Store(int64(time.Since(oldest).Seconds()))
	}
	token := w.tokenFor(first, last)
	backoff := 250 * time.Millisecond
	for len(body) > 0 {
		start := time.Now()
		err := w.insert(ctx, body, token)
		if err == nil {
			w.Stats.Inserts.Add(1)
			w.Stats.EventsWritten.Add(uint64(len(events)))
			w.Stats.LastInsertMs.Store(time.Since(start).Milliseconds())
			break
		}
		w.Stats.InsertErrors.Add(1)
		w.Logger.Warn("clickhouse insert failed, retrying", "first", first, "last", last, "err", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, w.RetryMax)
	}
	if err := w.Log.Commit(last + 1); err != nil {
		return fmt.Errorf("commit wal: %w", err)
	}
	if err := os.Remove(filepath.Join(w.Dir, inflightName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		w.Logger.Warn("remove in-flight file", "err", err)
	}
	w.Stats.LagSeconds.Store(0)
	if w.Hub != nil && len(events) > 0 {
		w.Hub.Publish(events)
	}
	return nil
}

// tokenFor names a WAL group for insert_deduplication_token. A re-send of the same range from the
// same spool carries the same token, so ClickHouse drops the copy. The spool id keeps tokens of
// different spools apart: sequence numbers restart at 1 in a new spool directory and on every
// instance, and a bare range would make ClickHouse discard new data as a duplicate of old data.
func (w *Writer) tokenFor(first, last uint64) string {
	return fmt.Sprintf("%s-%d-%d", w.spoolID, first, last)
}

// loadSpoolID reads the spool's id, creating it on first use.
func loadSpoolID(dir string) (string, error) {
	p := filepath.Join(dir, spoolIDName)
	if b, err := os.ReadFile(p); err == nil {
		if id := string(bytes.TrimSpace(b)); len(id) == 32 {
			return id, nil
		}
		return "", fmt.Errorf("%s is damaged; delete it only if the spool holds no pending records", p)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(raw[:])
	if err := writeFileSync(p, []byte(id+"\n")); err != nil {
		return "", err
	}
	return id, nil
}

// writeFileSync writes p atomically: a temp file, fsync, rename.
func writeFileSync(p string, b []byte) error {
	tmp := p + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (w *Writer) insert(ctx context.Context, body []byte, token string) error {
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"insert_deduplicate":               1,
		"insert_deduplication_token":       token,
		"input_format_skip_unknown_fields": 0,
	}))
	ictx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var q bytes.Buffer
	q.Grow(len(body) + 64)
	q.WriteString("INSERT INTO events FORMAT JSONEachRow\n")
	q.Write(body)
	return w.CH.Exec(ictx, q.String())
}

// chRow is one JSONEachRow line.
type chRow struct {
	Event
	TS       string `json:"timestamp"`
	Ingested string `json:"ingested_at"`
}

const chTime = "2006-01-02 15:04:05.000"

func (w *Writer) encode(recs []wal.Record) ([]byte, []Event, time.Time) {
	var buf bytes.Buffer
	var all []Event
	var oldest time.Time
	for _, r := range recs {
		events, err := DecodeRecord(r.Data)
		if err != nil {
			// A record that passed the CRC but does not decode was written by a buggy build. Skip it
			// so one bad record cannot stall ingestion forever; the counter makes it visible.
			w.Stats.BadRecords.Add(1)
			w.Logger.Error("skipping undecodable WAL record", "seq", r.Seq, "err", err)
			continue
		}
		for _, e := range events {
			line, err := json.Marshal(chRow{Event: e, TS: e.Timestamp.Format(chTime), Ingested: e.IngestedAt.Format(chTime)})
			if err != nil {
				w.Stats.BadRecords.Add(1)
				continue
			}
			buf.Write(line)
			buf.WriteByte('\n')
			if oldest.IsZero() || e.IngestedAt.Before(oldest) {
				oldest = e.IngestedAt
			}
		}
		all = append(all, events...)
	}
	return buf.Bytes(), all, oldest
}

// ---------- in-flight file: first u64 | last u64 | crc32c ----------

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

func (w *Writer) writeInflight(first, last uint64) error {
	var b [20]byte
	binary.LittleEndian.PutUint64(b[0:], first)
	binary.LittleEndian.PutUint64(b[8:], last)
	binary.LittleEndian.PutUint32(b[16:], crc32.Checksum(b[:16], castagnoli))
	return writeFileSync(filepath.Join(w.Dir, inflightName), b[:])
}

func (w *Writer) readInflight() (uint64, uint64, bool) {
	b, err := os.ReadFile(filepath.Join(w.Dir, inflightName))
	if err != nil || len(b) != 20 || crc32.Checksum(b[:16], castagnoli) != binary.LittleEndian.Uint32(b[16:]) {
		return 0, 0, false
	}
	first, last := binary.LittleEndian.Uint64(b[0:]), binary.LittleEndian.Uint64(b[8:])
	return first, last, last >= first
}
