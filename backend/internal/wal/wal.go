// Package wal is the durable ingest queue (ADR 0003, decision D002).
//
// A batch is acknowledged only after Append returns, and Append returns only after the record is
// written and fsynced. Records live in append-only segment files. Each record is framed as
//
//	length uint32 | crc32c(seq, payload) uint32 | seq uint64 | payload
//
// all little endian. A checkpoint file holds the next sequence number the consumer still needs;
// segments wholly below it are deleted. The total size of all segments is capped by a disk budget,
// and Append returns ErrFull when a record does not fit, so the HTTP layer can answer 503.
package wal

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	headerSize = 16
	// MaxRecordBytes caps one record; ingest batches are far smaller (body limit).
	MaxRecordBytes = 16 << 20
	segmentExt     = ".wal"
	checkpointName = "checkpoint"
)

var (
	// ErrFull means the record does not fit in the disk budget. Retry later.
	ErrFull = errors.New("wal: disk budget exhausted")
	// ErrClosed means the log was closed.
	ErrClosed = errors.New("wal: closed")
	// ErrTooLarge means the record exceeds MaxRecordBytes.
	ErrTooLarge = errors.New("wal: record too large")
	// ErrCorrupt means a sealed segment failed validation. The log refuses to open rather than
	// silently drop acknowledged records.
	ErrCorrupt = errors.New("wal: corrupt segment")
)

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// Record is one durable entry.
type Record struct {
	Seq  uint64
	Data []byte
}

// Log is the contract the ingest pipeline depends on. Segmented is the only implementation.
type Log interface {
	// Append stores data durably and returns its sequence number.
	Append(ctx context.Context, data []byte) (uint64, error)
	// ReadFrom returns up to max durable records starting at seq (fewer if maxBytes is reached).
	ReadFrom(seq uint64, max, maxBytes int) ([]Record, error)
	// Commit records that every record below seq has been processed; their segments may go.
	Commit(seq uint64) error
	// Checkpoint is the first sequence number not yet committed.
	Checkpoint() uint64
	// Notify returns a channel closed on the next successful Append.
	Notify() <-chan struct{}
	Stats() Stats
	Close() error
}

// Options configure a Segmented log.
type Options struct {
	Dir string
	// SegmentBytes rotates the active segment once it would exceed this size. Default 64 MiB.
	SegmentBytes int64
	// MaxBytes is the disk budget for all segments. Default 1 GiB.
	MaxBytes int64
	// MaxGroup caps how many concurrent appends share one fsync. Default 256.
	MaxGroup int
}

// Stats are exposed as metrics.
type Stats struct {
	Bytes          int64
	MaxBytes       int64
	Segments       int
	NextSeq        uint64
	Checkpoint     uint64
	Appends        uint64
	Syncs          uint64
	FullRejections uint64
	// TruncatedBytes is how much torn tail recovery cut from the last segment at open.
	TruncatedBytes int64
}

type segment struct {
	first uint64
	count uint64
	size  int64
	path  string
}

func (s segment) end() uint64 { return s.first + s.count }

type appendReq struct {
	data []byte
	done chan appendRes
}

type appendRes struct {
	seq uint64
	err error
}

// Segmented is a segmented, fsynced, append-only log.
type Segmented struct {
	opt Options

	mu       sync.Mutex
	segs     []segment // ascending; the last one is active
	active   *os.File
	nextSeq  uint64
	ckpt     uint64
	bytes    int64
	notify   chan struct{}
	failed   error
	closed   bool
	stats    Stats
	reqs     chan appendReq
	stopped  chan struct{}
	stopOnce sync.Once

	readMu sync.Mutex
	rd     *cursor
}

type cursor struct {
	seq  uint64 // sequence number of the next record at off
	seg  uint64 // first seq of the open segment
	f    *os.File
	off  int64
	path string
}

// Open recovers or creates a log in opt.Dir.
func Open(opt Options) (*Segmented, error) {
	if opt.Dir == "" {
		return nil, errors.New("wal: Dir is required")
	}
	if opt.SegmentBytes <= 0 {
		opt.SegmentBytes = 64 << 20
	}
	if opt.MaxBytes <= 0 {
		opt.MaxBytes = 1 << 30
	}
	if opt.MaxGroup <= 0 {
		opt.MaxGroup = 256
	}
	if opt.SegmentBytes > opt.MaxBytes {
		opt.SegmentBytes = opt.MaxBytes
	}
	if err := os.MkdirAll(opt.Dir, 0o700); err != nil {
		return nil, err
	}
	l := &Segmented{opt: opt, notify: make(chan struct{}), reqs: make(chan appendReq), stopped: make(chan struct{})}
	if err := l.recover(); err != nil {
		return nil, err
	}
	go l.writer()
	return l, nil
}

func segName(first uint64) string { return fmt.Sprintf("%020d%s", first, segmentExt) }

func (l *Segmented) recover() error {
	entries, err := os.ReadDir(l.opt.Dir)
	if err != nil {
		return err
	}
	var firsts []uint64
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, segmentExt) {
			continue
		}
		v, err := strconv.ParseUint(strings.TrimSuffix(n, segmentExt), 10, 64)
		if err != nil {
			return fmt.Errorf("wal: unexpected file %s", n)
		}
		firsts = append(firsts, v)
	}
	sort.Slice(firsts, func(i, j int) bool { return firsts[i] < firsts[j] })

	ckpt, haveCkpt, err := readCheckpoint(filepath.Join(l.opt.Dir, checkpointName))
	if err != nil {
		return err
	}
	for i, first := range firsts {
		p := filepath.Join(l.opt.Dir, segName(first))
		last := i == len(firsts)-1
		seg, validEnd, fileSize, err := scanSegment(p, first)
		if err != nil {
			return err
		}
		if validEnd != fileSize {
			if !last {
				return fmt.Errorf("%w: %s has %d invalid bytes", ErrCorrupt, p, fileSize-validEnd)
			}
			// Torn tail from a crash during an unacknowledged write: cut it off.
			if err := os.Truncate(p, validEnd); err != nil {
				return err
			}
			l.stats.TruncatedBytes = fileSize - validEnd
		}
		if n := len(l.segs); n > 0 && l.segs[n-1].end() != seg.first {
			return fmt.Errorf("%w: gap between %s and previous segment", ErrCorrupt, p)
		}
		l.segs = append(l.segs, seg)
	}
	l.nextSeq = 1
	if n := len(l.segs); n > 0 {
		l.nextSeq = l.segs[n-1].end()
		if !haveCkpt {
			ckpt = l.segs[0].first
		}
	}
	if !haveCkpt && len(l.segs) == 0 {
		ckpt = 1
	}
	if ckpt > l.nextSeq {
		// Everything was consumed and the segments are gone: continue numbering after the checkpoint.
		l.nextSeq = ckpt
	}
	l.ckpt = ckpt
	for _, s := range l.segs {
		l.bytes += s.size
	}
	if err := l.dropConsumedLocked(); err != nil {
		return err
	}
	if len(l.segs) == 0 || l.segs[len(l.segs)-1].end() != l.nextSeq {
		if err := l.newSegmentLocked(); err != nil {
			return err
		}
	} else {
		f, err := os.OpenFile(l.segs[len(l.segs)-1].path, os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		l.active = f
	}
	return nil
}

// scanSegment validates records and returns the segment, the offset after the last valid record
// and the file size.
func scanSegment(p string, first uint64) (segment, int64, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return segment{}, 0, 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return segment{}, 0, 0, err
	}
	seg := segment{first: first, path: p}
	var off int64
	r := &offsetReader{f: f}
	for {
		_, n, err := readFrame(r, first+seg.count)
		if err != nil {
			break // EOF, torn or invalid: the caller decides
		}
		off += n
		seg.count++
	}
	seg.size = off
	return seg, off, st.Size(), nil
}

type offsetReader struct {
	f   *os.File
	off int64
}

func (r *offsetReader) Read(p []byte) (int, error) {
	n, err := r.f.ReadAt(p, r.off)
	r.off += int64(n)
	if err == io.EOF && n == len(p) {
		err = nil
	}
	return n, err
}

var errFrame = errors.New("wal: invalid frame")

// readFrame reads one frame and checks it carries wantSeq.
func readFrame(r io.Reader, wantSeq uint64) ([]byte, int64, error) {
	var h [headerSize]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return nil, 0, err
	}
	n := binary.LittleEndian.Uint32(h[0:4])
	sum := binary.LittleEndian.Uint32(h[4:8])
	seq := binary.LittleEndian.Uint64(h[8:16])
	if n > MaxRecordBytes || seq != wantSeq {
		return nil, 0, errFrame
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, 0, err
	}
	if frameCRC(seq, data) != sum {
		return nil, 0, errFrame
	}
	return data, headerSize + int64(n), nil
}

func frameCRC(seq uint64, data []byte) uint32 {
	var s [8]byte
	binary.LittleEndian.PutUint64(s[:], seq)
	c := crc32.Update(0, castagnoli, s[:])
	return crc32.Update(c, castagnoli, data)
}

func appendFrame(buf []byte, seq uint64, data []byte) []byte {
	var h [headerSize]byte
	binary.LittleEndian.PutUint32(h[0:4], uint32(len(data)))
	binary.LittleEndian.PutUint32(h[4:8], frameCRC(seq, data))
	binary.LittleEndian.PutUint64(h[8:16], seq)
	return append(append(buf, h[:]...), data...)
}

func (l *Segmented) newSegmentLocked() error {
	if l.active != nil {
		if err := l.active.Close(); err != nil {
			return err
		}
		l.active = nil
	}
	p := filepath.Join(l.opt.Dir, segName(l.nextSeq))
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := syncDir(l.opt.Dir); err != nil {
		f.Close()
		return err
	}
	l.active = f
	l.segs = append(l.segs, segment{first: l.nextSeq, path: p})
	return nil
}

// Append implements Log.
func (l *Segmented) Append(ctx context.Context, data []byte) (uint64, error) {
	if len(data) > MaxRecordBytes {
		return 0, ErrTooLarge
	}
	req := appendReq{data: data, done: make(chan appendRes, 1)}
	select {
	case l.reqs <- req:
	case <-l.stopped:
		return 0, ErrClosed
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	// Once handed over, wait for the outcome even if ctx ends: the caller must know whether the
	// record became durable.
	res := <-req.done
	return res.seq, res.err
}

// writer serialises appends and shares one fsync among the requests waiting at that moment.
func (l *Segmented) writer() {
	for {
		var group []appendReq
		select {
		case r := <-l.reqs:
			group = append(group, r)
		case <-l.stopped:
			return
		}
	drain:
		for len(group) < l.opt.MaxGroup {
			select {
			case r := <-l.reqs:
				group = append(group, r)
			default:
				break drain
			}
		}
		l.writeGroup(group)
	}
}

func (l *Segmented) writeGroup(group []appendReq) {
	l.mu.Lock()
	defer l.mu.Unlock()
	reply := func(r appendReq, seq uint64, err error) { r.done <- appendRes{seq, err} }
	if l.failed != nil {
		for _, r := range group {
			reply(r, 0, l.failed)
		}
		return
	}
	var (
		buf      []byte
		accepted []appendReq
		seqs     []uint64
		wrote    bool
	)
	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		if err := l.writeSync(buf); err != nil {
			for _, r := range accepted {
				reply(r, 0, err)
			}
			buf, accepted, seqs = nil, nil, nil
			return err
		}
		cur := &l.segs[len(l.segs)-1]
		cur.size += int64(len(buf))
		cur.count += uint64(len(accepted))
		l.bytes += int64(len(buf))
		l.nextSeq += uint64(len(accepted))
		l.stats.Appends += uint64(len(accepted))
		for i, r := range accepted {
			reply(r, seqs[i], nil)
		}
		buf, accepted, seqs = nil, nil, nil
		wrote = true
		return nil
	}
	for _, r := range group {
		frame := int64(headerSize + len(r.data))
		if l.bytes+int64(len(buf))+frame > l.opt.MaxBytes {
			l.stats.FullRejections++
			reply(r, 0, ErrFull)
			continue
		}
		cur := l.segs[len(l.segs)-1]
		if cur.size+int64(len(buf))+frame > l.opt.SegmentBytes && cur.size+int64(len(buf)) > 0 {
			if err := flush(); err != nil {
				reply(r, 0, err)
				continue
			}
			if err := l.newSegmentLocked(); err != nil {
				l.failed = fmt.Errorf("wal: rotate: %w", err)
				reply(r, 0, l.failed)
				continue
			}
		}
		seq := l.nextSeq + uint64(len(accepted))
		buf = appendFrame(buf, seq, r.data)
		accepted = append(accepted, r)
		seqs = append(seqs, seq)
	}
	_ = flush()
	if wrote {
		old := l.notify
		l.notify = make(chan struct{})
		close(old)
	}
}

// writeSync appends buf to the active segment and fsyncs. On failure (disk full) it cuts the file
// back to its previous size, so a later append starts on a record boundary.
func (l *Segmented) writeSync(buf []byte) error {
	cur := l.segs[len(l.segs)-1]
	_, werr := l.active.Write(buf)
	if werr == nil {
		werr = l.active.Sync()
		l.stats.Syncs++
		if werr == nil {
			return nil
		}
	}
	if err := l.active.Truncate(cur.size); err != nil {
		l.failed = fmt.Errorf("wal: write failed (%v) and truncate failed: %w", werr, err)
		return l.failed
	}
	if err := l.active.Sync(); err != nil {
		l.failed = fmt.Errorf("wal: write failed (%v) and sync after truncate failed: %w", werr, err)
		return l.failed
	}
	return fmt.Errorf("wal: append: %w", werr)
}

// ReadFrom implements Log. It only returns records that Append already acknowledged.
func (l *Segmented) ReadFrom(seq uint64, max, maxBytes int) ([]Record, error) {
	l.readMu.Lock()
	defer l.readMu.Unlock()
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, ErrClosed
	}
	end := l.nextSeq
	segs := append([]segment(nil), l.segs...)
	l.mu.Unlock()
	if seq >= end || max <= 0 {
		return nil, nil
	}
	if len(segs) == 0 || seq < segs[0].first {
		return nil, fmt.Errorf("wal: seq %d already removed", seq)
	}
	var out []Record
	size := 0
	for seq < end && len(out) < max && (size < maxBytes || len(out) == 0) {
		si := sort.Search(len(segs), func(i int) bool { return segs[i].end() > seq })
		if si == len(segs) {
			break
		}
		s := segs[si]
		if err := l.position(s, seq); err != nil {
			return out, err
		}
		data, n, err := readFrame(&offsetReader{f: l.rd.f, off: l.rd.off}, seq)
		if err != nil {
			return out, fmt.Errorf("wal: read seq %d: %w", seq, err)
		}
		l.rd.off += n
		l.rd.seq++
		out = append(out, Record{Seq: seq, Data: data})
		size += len(data)
		seq++
	}
	return out, nil
}

// position moves the read cursor to seq inside s, reusing the open file when possible.
func (l *Segmented) position(s segment, seq uint64) error {
	if l.rd != nil && l.rd.seg == s.first && l.rd.seq == seq {
		return nil
	}
	if l.rd == nil || l.rd.seg != s.first {
		l.closeCursor()
		f, err := os.Open(s.path)
		if err != nil {
			return err
		}
		l.rd = &cursor{seq: s.first, seg: s.first, f: f, path: s.path}
	}
	if l.rd.seq > seq {
		l.rd.seq, l.rd.off = s.first, 0
	}
	for l.rd.seq < seq {
		_, n, err := readFrame(&offsetReader{f: l.rd.f, off: l.rd.off}, l.rd.seq)
		if err != nil {
			return err
		}
		l.rd.off += n
		l.rd.seq++
	}
	return nil
}

func (l *Segmented) closeCursor() {
	if l.rd != nil {
		l.rd.f.Close()
		l.rd = nil
	}
}

// Commit implements Log.
func (l *Segmented) Commit(seq uint64) error {
	l.readMu.Lock()
	defer l.readMu.Unlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrClosed
	}
	if seq <= l.ckpt {
		return nil
	}
	if seq > l.nextSeq {
		return fmt.Errorf("wal: commit %d beyond next sequence %d", seq, l.nextSeq)
	}
	if err := writeCheckpoint(l.opt.Dir, seq); err != nil {
		return err
	}
	l.ckpt = seq
	return l.dropConsumedLocked()
}

// dropConsumedLocked removes sealed segments whose records are all below the checkpoint.
func (l *Segmented) dropConsumedLocked() error {
	keep := l.segs[:0]
	var firstErr error
	for i, s := range l.segs {
		active := i == len(l.segs)-1 && l.active != nil
		if !active && s.end() <= l.ckpt {
			if l.rd != nil && l.rd.seg == s.first {
				l.closeCursor()
			}
			if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				if firstErr == nil {
					firstErr = err
				}
				keep = append(keep, s)
				continue
			}
			l.bytes -= s.size
			continue
		}
		keep = append(keep, s)
	}
	l.segs = keep
	return firstErr
}

// Checkpoint implements Log.
func (l *Segmented) Checkpoint() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ckpt
}

// Notify implements Log.
func (l *Segmented) Notify() <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.notify
}

// Stats implements Log.
func (l *Segmented) Stats() Stats {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.stats
	s.Bytes, s.MaxBytes, s.Segments, s.NextSeq, s.Checkpoint = l.bytes, l.opt.MaxBytes, len(l.segs), l.nextSeq, l.ckpt
	return s
}

// Close stops the writer. Appends already acknowledged stay durable.
func (l *Segmented) Close() error {
	l.stopOnce.Do(func() { close(l.stopped) })
	l.readMu.Lock()
	defer l.readMu.Unlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	l.closeCursor()
	if l.active != nil {
		err := l.active.Close()
		l.active = nil
		return err
	}
	return nil
}

// ---------- checkpoint file: seq uint64 | crc32c uint32 ----------

func readCheckpoint(p string) (uint64, bool, error) {
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if len(b) != 12 || crc32.Checksum(b[:8], castagnoli) != binary.LittleEndian.Uint32(b[8:]) {
		return 0, false, fmt.Errorf("%w: checkpoint file damaged", ErrCorrupt)
	}
	return binary.LittleEndian.Uint64(b[:8]), true, nil
}

func writeCheckpoint(dir string, seq uint64) error {
	var b [12]byte
	binary.LittleEndian.PutUint64(b[:8], seq)
	binary.LittleEndian.PutUint32(b[8:], crc32.Checksum(b[:8], castagnoli))
	tmp := filepath.Join(dir, checkpointName+".tmp")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b[:]); err != nil {
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
	if err := os.Rename(tmp, filepath.Join(dir, checkpointName)); err != nil {
		return err
	}
	return syncDir(dir)
}

// WaitTimeout is a helper for consumers: it waits for ch or d, whichever comes first.
func WaitTimeout(ctx context.Context, ch <-chan struct{}, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ch:
	case <-t.C:
	case <-ctx.Done():
	}
}
