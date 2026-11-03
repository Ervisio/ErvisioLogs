package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/ingest"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
	"github.com/Ervisio/ErvisioLogs/backend/internal/wal"
)

// Logs holds the dependencies of the ingest, search and tail handlers. When API.Logs is nil those
// routes answer 503.
type Logs struct {
	WAL       wal.Log
	CH        driver.Conn
	Hub       *ingest.Hub
	Writer    *ingest.Writer
	Validator ingest.Validator
	// MaxTailSubscribers caps open live-tail connections on this instance (default 500).
	MaxTailSubscribers int
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
	cat, err := a.projectCatalog(r.Context(), t.WorkspaceID, t.ProjectID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	v := a.Logs.Validator
	if v.Now == nil {
		v.Now = a.now
	}
	res := ingestResult{RequestID: httpx.RequestID(r.Context()), Errors: []ingest.Rejection{}}
	accepted := make([]ingest.Event, 0, len(in))
	for i, ev := range in {
		e, err := v.Normalize(ev, defStream, defEnv, cat, t.WorkspaceID, t.ProjectID)
		if err != nil {
			res.Errors = append(res.Errors, ingest.Rejection{Index: i, Reason: err.Error()})
			continue
		}
		accepted = append(accepted, e)
	}
	res.Accepted, res.Rejected = len(accepted), len(res.Errors)
	if len(accepted) == 0 {
		res.Status = "rejected"
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, res)
		return
	}
	rec, err := ingest.EncodeRecord(accepted)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if _, err := a.Logs.WAL.Append(r.Context(), rec); err != nil {
		switch {
		case errors.Is(err, wal.ErrFull):
			w.Header().Set("Retry-After", "5")
			httpx.WriteProblem(w, r, http.StatusServiceUnavailable, "the ingest queue is full, retry later")
		case errors.Is(err, wal.ErrTooLarge):
			httpx.WriteProblem(w, r, http.StatusRequestEntityTooLarge, "batch too large")
		case errors.Is(err, context.Canceled):
			// The client went away; nothing was acknowledged. The record may still be stored.
		default:
			a.Log.Error("wal append failed", "request_id", res.RequestID, "error", err)
			w.Header().Set("Retry-After", "5")
			httpx.WriteProblem(w, r, http.StatusServiceUnavailable, "ingestion is temporarily unavailable")
		}
		return
	}
	res.Status = "durably_queued"
	httpx.WriteJSON(w, http.StatusAccepted, res)
}
