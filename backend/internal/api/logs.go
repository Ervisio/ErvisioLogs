package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/archive"
	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/ingest"
	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
	"github.com/Ervisio/ErvisioLogs/backend/internal/wal"
)

// Logs holds the dependencies of the ingest, search and tail handlers. When API.Logs is nil those
// routes answer 503.
type Logs struct {
	WAL    wal.Log
	CH     driver.Conn
	Hub    *ingest.Hub
	Writer *ingest.Writer
	// MaxTailSubscribers caps open live-tail connections on this instance (default 500).
	MaxTailSubscribers int
	// OverCapacity, when set, reports that storage is above its cap with nothing left to evict
	// (ADR 0004). Ingest then answers 503 instead of accepting data it cannot keep.
	OverCapacity func() bool
	// Archive, when set, adds evicted archive segments to searches (ADR 0008).
	Archive ArchiveReader
}

// ArchiveReader lists the cached archive segments a search must read.
type ArchiveReader interface {
	ReadSources(ctx context.Context, ws, proj uuid.UUID, streams []uuid.UUID, from, to time.Time) ([]archive.ReadSource, int, error)
}

// maxDecodedIngest caps a gzip-decoded ingest body; the compressed size is capped by the router.
const maxDecodedIngest = 16 << 20

// ---------- stream and environment names ----------

// catalog caches the stream and environment names of each project for catalogTTL. Handlers that
// create or delete streams and environments drop the project's entry, so on this instance changes
// are visible at once; other instances see them within catalogTTL.
type catalog struct {
	mu      sync.Mutex
	entries map[uuid.UUID]*catalogEntry
}

type catalogEntry struct {
	loaded              time.Time
	streams, envs       map[string]uuid.UUID
	streamName, envName map[uuid.UUID]string
}

const catalogTTL = 30 * time.Second

func (a *API) projectCatalog(ctx context.Context, ws, project uuid.UUID) (*catalogEntry, error) {
	a.catalog.mu.Lock()
	if a.catalog.entries == nil {
		a.catalog.entries = map[uuid.UUID]*catalogEntry{}
	}
	e := a.catalog.entries[project]
	a.catalog.mu.Unlock()
	if e != nil && a.now().Sub(e.loaded) < catalogTTL {
		return e, nil
	}
	ss, err := a.Store.ListStreams(ctx, ws, project)
	if err != nil {
		return nil, err
	}
	es, err := a.Store.ListEnvironments(ctx, ws, project)
	if err != nil {
		return nil, err
	}
	e = &catalogEntry{loaded: a.now(), streams: map[string]uuid.UUID{}, envs: map[string]uuid.UUID{},
		streamName: map[uuid.UUID]string{}, envName: map[uuid.UUID]string{}}
	for _, s := range ss {
		e.streams[s.Name], e.streamName[s.ID] = s.ID, s.Name
	}
	for _, v := range es {
		e.envs[v.Name], e.envName[v.ID] = v.ID, v.Name
	}
	a.catalog.mu.Lock()
	a.catalog.entries[project] = e
	a.catalog.mu.Unlock()
	return e, nil
}

func (a *API) forgetCatalog(project uuid.UUID) {
	a.catalog.mu.Lock()
	delete(a.catalog.entries, project)
	a.catalog.mu.Unlock()
}

func (e *catalogEntry) Stream(n string) (uuid.UUID, bool, error) {
	id, ok := e.streams[n]
	return id, ok, nil
}

func (e *catalogEntry) Environment(n string) (uuid.UUID, bool, error) {
	id, ok := e.envs[n]
	return id, ok, nil
}

var errStreamLimit = errors.New("the project has reached its stream limit")

// streamLimit reports whether the project may not get another stream.
func (a *API) streamLimit(ws, project uuid.UUID, have int) bool {
	max := a.Settings.ProjectInt(ws, project, settings.MaxStreams)
	return max > 0 && int64(have) >= max
}

// streamCreator resolves the streams of one ingest request and creates the missing ones when the
// project allows it (ADR 0018). The limit is checked per request, so concurrent requests can pass it
// by a few streams.
type streamCreator struct {
	a           *API
	r           *http.Request
	ws, project uuid.UUID
	cat         *catalogEntry
	created     map[string]uuid.UUID
}

func (c *streamCreator) Environment(n string) (uuid.UUID, bool, error) { return c.cat.Environment(n) }

func (c *streamCreator) Stream(n string) (uuid.UUID, bool, error) {
	if id, ok := c.cat.streams[n]; ok {
		return id, true, nil
	}
	if id, ok := c.created[n]; ok {
		return id, true, nil
	}
	if !c.a.Settings.ProjectBool(c.ws, c.project, settings.AutoCreateStreams) {
		return uuid.Nil, false, nil
	}
	if c.a.streamLimit(c.ws, c.project, len(c.cat.streams)+len(c.created)) {
		return uuid.Nil, false, fmt.Errorf("stream %q does not exist and %w", n, errStreamLimit)
	}
	ctx := c.r.Context()
	c.a.forgetCatalog(c.project)
	s, err := c.a.Store.CreateStream(ctx, c.ws, c.project, n, store.Meta{})
	if errors.Is(err, store.ErrConflict) {
		// Another request created it first.
		if cat, err := c.a.projectCatalog(ctx, c.ws, c.project); err == nil {
			if id, ok := cat.streams[n]; ok {
				c.cat = cat
				return id, true, nil
			}
		}
	}
	if err != nil {
		c.a.Log.Error("creating stream failed", "request_id", httpx.RequestID(ctx), "stream", n, "error", err)
		return uuid.Nil, false, fmt.Errorf("stream %q could not be created, retry later", n)
	}
	if c.created == nil {
		c.created = map[string]uuid.UUID{}
	}
	c.created[n] = s.ID
	c.a.audit(c.r, &c.ws, "stream.auto_create", "stream", s.ID.String(), map[string]any{"name": n})
	return s.ID, true, nil
}

// ---------- ingest ----------

type ingestResult struct {
	RequestID string             `json:"request_id"`
	Accepted  int                `json:"accepted"`
	Rejected  int                `json:"rejected"`
	Status    string             `json:"status"`
	Errors    []ingest.Rejection `json:"errors"`
}

// ingestToken requires an API token with logs:ingest. Ingest is for machines: a browser session
// cannot ingest.
func (a *API) ingestToken(w http.ResponseWriter, r *http.Request) (*store.APIToken, bool) {
	if a.Logs == nil {
		httpx.WriteProblem(w, r, http.StatusServiceUnavailable, "ingestion is not available on this instance")
		return nil, false
	}
	t := principalOf(r).token
	if t == nil {
		httpx.WriteProblem(w, r, http.StatusUnauthorized, "an API token with logs:ingest is required")
		return nil, false
	}
	if !hasScope(t, auth.LogsIngest) {
		httpx.WriteProblem(w, r, http.StatusForbidden, "missing permission "+auth.LogsIngest)
		return nil, false
	}
	if a.Logs.OverCapacity != nil && a.Logs.OverCapacity() {
		w.Header().Set("Retry-After", "60")
		httpx.WriteProblem(w, r, http.StatusServiceUnavailable, "storage is full: raise the cache budget or shorten retention")
		return nil, false
	}
	return t, true
}

func hasScope(t *store.APIToken, scope string) bool {
	for _, s := range t.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// readIngestBody returns the body, gunzipped when Content-Encoding says gzip.
func readIngestBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	var src io.Reader = r.Body
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			httpx.WriteProblem(w, r, http.StatusBadRequest, "body is not valid gzip")
			return nil, false
		}
		defer zr.Close()
		src = zr
	default:
		httpx.WriteProblem(w, r, http.StatusUnsupportedMediaType, "Content-Encoding must be gzip or absent")
		return nil, false
	}
	b, err := io.ReadAll(io.LimitReader(src, maxDecodedIngest+1))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.WriteProblem(w, r, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			httpx.WriteProblem(w, r, http.StatusBadRequest, "could not read the request body")
		}
		return nil, false
	}
	if len(b) > maxDecodedIngest {
		httpx.WriteProblem(w, r, http.StatusRequestEntityTooLarge, "decompressed body too large")
		return nil, false
	}
	return b, true
}

func strictJSON(b []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("unexpected data after JSON body")
	}
	return nil
}

func (a *API) ingestOne(w http.ResponseWriter, r *http.Request) {
	t, ok := a.ingestToken(w, r)
	if !ok {
		return
	}
	b, ok := readIngestBody(w, r)
	if !ok {
		return
	}
	var ev ingest.InEvent
	if err := strictJSON(b, &ev); err != nil {
		httpx.WriteProblem(w, r, http.StatusBadRequest, "malformed JSON body: "+err.Error())
		return
	}
	a.acceptEvents(w, r, t, []ingest.InEvent{ev}, "", "")
}

func (a *API) ingestBatch(w http.ResponseWriter, r *http.Request) {
	t, ok := a.ingestToken(w, r)
	if !ok {
		return
	}
	b, ok := readIngestBody(w, r)
	if !ok {
		return
	}
	var batch ingest.InBatch
	if err := strictJSON(b, &batch); err != nil {
		httpx.WriteProblem(w, r, http.StatusBadRequest, "malformed JSON body: "+err.Error())
		return
	}
	if len(batch.Events) == 0 {
		invalid(w, r, "events must hold at least one event")
		return
	}
	if len(batch.Events) > ingest.MaxBatchEvents {
		invalid(w, r, "a batch holds at most "+strconv.Itoa(ingest.MaxBatchEvents)+" events")
		return
	}
	a.acceptEvents(w, r, t, batch.Events, batch.Stream, batch.Environment)
}

// acceptEvents validates the events, appends the valid ones to the WAL as one record and answers
// 202 only after the WAL reports the record durable.
func (a *API) acceptEvents(w http.ResponseWriter, r *http.Request, t *store.APIToken, in []ingest.InEvent, defStream, defEnv string) {
	res, f := a.queueEvents(r, t, in, defStream, defEnv)
	if f != nil {
		f.write(w, r)
		return
	}
	if res.Accepted == 0 {
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, res)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, res)
}

// ingestFailure is an answer that is not an ingest result: the WAL refused the record or the
// catalog could not be read. A nil status means the client went away and nothing is written.
type ingestFailure struct {
	status     int
	msg        string
	retryAfter string
	err        error // internal error, logged
}

func (f *ingestFailure) write(w http.ResponseWriter, r *http.Request) {
	if f.status == 0 {
		return
	}
	if f.retryAfter != "" {
		w.Header().Set("Retry-After", f.retryAfter)
	}
	httpx.WriteProblem(w, r, f.status, f.msg)
}

// queueEvents validates in and appends the accepted events to the WAL. res.Accepted == 0 means
// every event was rejected and nothing was written.
func (a *API) queueEvents(r *http.Request, t *store.APIToken, in []ingest.InEvent, defStream, defEnv string) (ingestResult, *ingestFailure) {
	res := ingestResult{RequestID: httpx.RequestID(r.Context()), Errors: []ingest.Rejection{}}
	cat, err := a.projectCatalog(r.Context(), t.WorkspaceID, t.ProjectID)
	if err != nil {
		a.Log.Error("request failed", "request_id", res.RequestID, "path", r.URL.Path, "error", err)
		return res, &ingestFailure{status: http.StatusInternalServerError, msg: "internal error"}
	}
	v := ingest.Validator{MaxFutureSkew: a.Settings.Duration(settings.IngestMaxFutureSkew),
		MaxPastAge: a.Settings.Duration(settings.IngestMaxPastAge), Now: a.now}
	streams := &streamCreator{a: a, r: r, ws: t.WorkspaceID, project: t.ProjectID, cat: cat}
	rules, err := a.Store.FieldRules(r.Context(), t.ProjectID)
	if err != nil {
		a.Log.Error("ingest rules unavailable", "request_id", res.RequestID, "error", err)
		return res, &ingestFailure{status: http.StatusServiceUnavailable, msg: "ingestion rules are unavailable", retryAfter: "5"}
	}
	accepted := make([]ingest.Event, 0, len(in))
	for i, ev := range in {
		e, err := v.Normalize(ev, defStream, defEnv, streams, t.WorkspaceID, t.ProjectID)
		if err != nil {
			res.Errors = append(res.Errors, ingest.Rejection{Index: i, Reason: err.Error()})
			continue
		}
		e.Attributes, err = redactAttributes(e.Attributes, rules)
		if err != nil {
			return res, &ingestFailure{status: http.StatusInternalServerError, msg: "internal error"}
		}
		accepted = append(accepted, e)
	}
	res.Accepted, res.Rejected = len(accepted), len(res.Errors)
	if len(accepted) == 0 {
		res.Status = "rejected"
		return res, nil
	}
	names := make([]string, 0, len(accepted))
	seenNames := map[string]bool{}
	for _, e := range accepted {
		if e.EventName != "" && !seenNames[e.EventName] {
			names = append(names, e.EventName)
			seenNames[e.EventName] = true
		}
	}
	if err := a.Store.DiscoverEventTypes(r.Context(), t.ProjectID, names); err != nil {
		a.Log.Error("event discovery failed", "request_id", res.RequestID, "error", err)
		return res, &ingestFailure{status: http.StatusServiceUnavailable, msg: "event catalog is unavailable", retryAfter: "5"}
	}
	rec, err := ingest.EncodeRecord(accepted)
	if err != nil {
		a.Log.Error("request failed", "request_id", res.RequestID, "path", r.URL.Path, "error", err)
		return res, &ingestFailure{status: http.StatusInternalServerError, msg: "internal error"}
	}
	size := int64(len(rec))
	lim := a.ingestLimits(t.WorkspaceID, t.ProjectID)
	if le := a.limits.admit(a.now(), t.ID, t.WorkspaceID, t.ProjectID, lim, len(accepted), size); le != nil {
		return res, le.failure()
	}
	if _, err := a.Logs.WAL.Append(r.Context(), rec); err != nil {
		a.limits.release(a.now(), t.WorkspaceID, t.ProjectID, len(accepted), size)
		switch {
		case errors.Is(err, wal.ErrFull):
			return res, &ingestFailure{status: http.StatusServiceUnavailable, msg: "the ingest queue is full, retry later", retryAfter: "5"}
		case errors.Is(err, wal.ErrTooLarge):
			return res, &ingestFailure{status: http.StatusRequestEntityTooLarge, msg: "batch too large"}
		case errors.Is(err, context.Canceled):
			// The client went away; nothing was acknowledged. The record may still be stored.
			return res, &ingestFailure{}
		default:
			a.Log.Error("wal append failed", "request_id", res.RequestID, "error", err)
			return res, &ingestFailure{status: http.StatusServiceUnavailable, msg: "ingestion is temporarily unavailable", retryAfter: "5"}
		}
	}
	res.Status = "durably_queued"
	return res, nil
}
