package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/ingest"
)

const (
	tailHeartbeat      = 15 * time.Second
	defaultMaxTailSubs = 500
)

// tailFilter is the subset of search filters the live tail applies in memory.
type tailFilter struct {
	streams, envs map[uuid.UUID]bool
	levels        map[string]bool
	events        map[string]bool
	severityMin   uint8
	query         string
}

func (f *tailFilter) match(e *ingest.Event) bool {
	if f.streams != nil && !f.streams[e.StreamID] {
		return false
	}
	if f.envs != nil && !f.envs[e.EnvironmentID] {
		return false
	}
	if f.levels != nil && !f.levels[e.SeverityText] {
		return false
	}
	if f.events != nil && !f.events[e.EventName] {
		return false
	}
	if e.SeverityNumber < f.severityMin {
		return false
	}
	return f.query == "" || strings.Contains(strings.ToLower(e.Message), f.query)
}

func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// tailLogs streams new events of a project as Server-Sent Events. Query parameters: project_id,
// streams, environments, levels, events (comma separated), severity_min, query. Browsers authenticate
// with the session cookie (EventSource sends it); other clients send a Bearer token with logs:read.
// The stream resumes from Last-Event-ID while the event is still in this instance's memory, and
// sends a "reset" event when it is not, so the client knows it missed events.
func (a *API) tailLogs(w http.ResponseWriter, r *http.Request) {
	qv := r.URL.Query()
	var pid uuid.UUID
	if s := qv.Get("project_id"); s != "" {
		var err error
		if pid, err = uuid.Parse(s); err != nil {
			invalid(w, r, "project_id is not a valid id")
			return
		}
	}
	proj, ok := a.logsReader(w, r, pid)
	if !ok {
		return
	}
	cat, err := a.projectCatalog(r.Context(), proj.WorkspaceID, proj.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	f := tailFilter{query: strings.ToLower(strings.TrimSpace(qv.Get("query")))}
	if names := splitList(qv.Get("streams")); names != nil {
		f.streams = map[uuid.UUID]bool{}
		for _, n := range names {
			id, ok := cat.streams[n]
			if !ok {
				invalid(w, r, fmt.Sprintf("stream %q does not exist in this project", n))
				return
			}
			f.streams[id] = true
		}
	}
	if names := splitList(qv.Get("environments")); names != nil {
		f.envs = map[uuid.UUID]bool{}
		for _, n := range names {
			id, ok := cat.envs[n]
			if !ok {
				invalid(w, r, fmt.Sprintf("environment %q does not exist in this project", n))
				return
			}
			f.envs[id] = true
		}
	}
	if names := splitList(qv.Get("levels")); names != nil {
		f.levels = map[string]bool{}
		for _, n := range names {
			_, text, ok := ingest.SeverityNumber(n)
			if !ok {
				invalid(w, r, fmt.Sprintf("level %q is not known", n))
				return
			}
			f.levels[text] = true
		}
	}
	if names := splitList(qv.Get("events")); names != nil {
		f.events = map[string]bool{}
		for _, n := range names {
			f.events[n] = true
		}
	}
	if s := qv.Get("severity_min"); s != "" {
		n, err := strconv.ParseUint(s, 10, 8)
		if err != nil {
			invalid(w, r, "severity_min must be a number from 0 to 255")
			return
		}
		f.severityMin = uint8(n)
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.WriteProblem(w, r, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	limit := a.Logs.MaxTailSubscribers
	if limit <= 0 {
		limit = defaultMaxTailSubs
	}
	if a.tailCount.Add(1) > int64(limit) {
		a.tailCount.Add(-1)
		w.Header().Set("Retry-After", "10")
		httpx.WriteProblem(w, r, http.StatusServiceUnavailable, "too many live tail connections, retry later")
		return
	}
	defer a.tailCount.Add(-1)

	// A browser that pauses and resumes opens a new EventSource, which cannot set Last-Event-ID,
	// so the same cursor is accepted as the last_event_id query parameter.
	var after uint64
	s := r.Header.Get("Last-Event-ID")
	if s == "" {
		s = qv.Get("last_event_id")
	}
	if s != "" {
		after, _ = strconv.ParseUint(s, 10, 64)
	}
	sub, backlog, resumed := a.Logs.Hub.Subscribe(proj.ID, after)
	defer a.Logs.Hub.Unsubscribe(sub)

	// The server's ReadTimeout would otherwise end the stream after 30 seconds.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 3000\n\n")
	if after != 0 && !resumed {
		fmt.Fprint(w, "event: reset\ndata: {\"reason\":\"events since your last event id are no longer in memory\"}\n\n")
	}
	send := func(te ingest.TailEvent) bool {
		if !f.match(&te.Event) {
			return true
		}
		b, err := json.Marshal(tailItem(&te.Event, cat))
		if err != nil {
			return true
		}
		_, err = fmt.Fprintf(w, "id: %d\nevent: log\ndata: %s\n\n", te.Cursor, b)
		return err == nil
	}
	for _, te := range backlog {
		if !send(te) {
			return
		}
	}
	flusher.Flush()

	beat := time.NewTicker(tailHeartbeat)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-beat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case te, open := <-sub.C:
			if !open {
				// The hub dropped this client for falling behind; it reconnects with Last-Event-ID.
				fmt.Fprint(w, "event: overflow\ndata: {\"reason\":\"the client did not keep up\"}\n\n")
				flusher.Flush()
				return
			}
			if !send(te) {
				return
			}
			// Drain what is already queued before flushing, so bursts cost one flush.
			for drained := false; !drained; {
				select {
				case te, open := <-sub.C:
					if !open || !send(te) {
						flusher.Flush()
						return
					}
				default:
					drained = true
				}
			}
			flusher.Flush()
		}
	}
}

func tailItem(e *ingest.Event, cat *catalogEntry) logItem {
	attrs := e.Attributes
	if len(attrs) == 0 {
		attrs = json.RawMessage("{}")
	}
	stream := e.Stream
	if stream == "" {
		stream = cat.streamName[e.StreamID]
	}
	env := e.Environment
	if env == "" {
		env = cat.envName[e.EnvironmentID]
	}
	return logItem{ID: e.EventID, Timestamp: e.Timestamp, IngestedAt: e.IngestedAt, Level: e.SeverityText,
		SeverityNumber: e.SeverityNumber, Event: e.EventName, Message: e.Message, Stream: stream, Environment: env,
		Service: e.SourceService, Host: e.SourceHost, Resource: e.SourceResource, TraceID: e.TraceID, SpanID: e.SpanID,
		Attributes: attrs, Tier: "hot"}
}
