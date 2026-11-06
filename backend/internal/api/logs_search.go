package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/ingest"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

const (
	searchDefaultLimit = 100
	searchMaxLimit     = 1000
	searchMaxRange     = 31 * 24 * time.Hour
	searchTimeout      = 30 * time.Second
	maxFilters         = 20
)

var attrSegment = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// logsReader authorises reading the logs of a project: a session member with logs:read, or an
// API token with logs:read bound to that project. projectID may be Nil for tokens.
func (a *API) logsReader(w http.ResponseWriter, r *http.Request, projectID uuid.UUID) (store.Project, bool) {
	return a.logsAccess(w, r, projectID, auth.LogsRead)
}

// logsAccess resolves the project a request reads logs from and checks perm: the token scope, or
// the user's workspace role.
func (a *API) logsAccess(w http.ResponseWriter, r *http.Request, projectID uuid.UUID, perm string) (store.Project, bool) {
	if a.Logs == nil {
		httpx.WriteProblem(w, r, http.StatusServiceUnavailable, "log search is not available on this instance")
		return store.Project{}, false
	}
	p := principalOf(r)
	switch {
	case p.token != nil:
		if !hasScope(p.token, perm) {
			httpx.WriteProblem(w, r, http.StatusForbidden, "missing permission "+perm)
			return store.Project{}, false
		}
		if projectID != uuid.Nil && projectID != p.token.ProjectID {
			httpx.WriteProblem(w, r, http.StatusNotFound, "not found")
			return store.Project{}, false
		}
		return store.Project{ID: p.token.ProjectID, WorkspaceID: p.token.WorkspaceID}, true
	case p.user != nil:
		if projectID == uuid.Nil {
			invalid(w, r, "project_id is required")
			return store.Project{}, false
		}
		proj, err := a.Store.GetProject(r.Context(), projectID)
		if err != nil {
			a.storeError(w, r, err)
			return store.Project{}, false
		}
		if _, ok := a.workspaceAccess(w, r, p.user, proj.WorkspaceID, perm); !ok {
			return store.Project{}, false
		}
		return proj, true
	default:
		httpx.WriteProblem(w, r, http.StatusUnauthorized, "authentication required")
		return store.Project{}, false
	}
}

// AttrFilter compares one attribute path with a value.
type AttrFilter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

type searchReq struct {
	ProjectID    uuid.UUID    `json:"project_id"`
	From         *time.Time   `json:"from"`
	To           *time.Time   `json:"to"`
	Query        string       `json:"query"`
	SeverityMin  uint8        `json:"severity_min"`
	Levels       []string     `json:"levels"`
	Streams      []string     `json:"streams"`
	Environments []string     `json:"environments"`
	Events       []string     `json:"events"`
	Tags         []string     `json:"tags"`
	Services     []string     `json:"services"`
	TraceID      string       `json:"trace_id"`
	Filters      []AttrFilter `json:"filters"`
	Limit        int          `json:"limit"`
	Cursor       string       `json:"cursor"`
	IncludeTotal bool         `json:"include_total"`
}

type logItem struct {
	Tags           []string        `json:"tags"`
	Tier           string          `json:"tier"`
	ID             string          `json:"id"`
	Timestamp      time.Time       `json:"timestamp"`
	IngestedAt     time.Time       `json:"ingested_at"`
	Level          string          `json:"level"`
	SeverityNumber uint8           `json:"severity_number"`
	Event          string          `json:"event"`
	Message        string          `json:"message"`
	Stream         string          `json:"stream"`
	Environment    string          `json:"environment"`
	Service        string          `json:"service"`
	Host           string          `json:"host"`
	Resource       string          `json:"resource"`
	TraceID        string          `json:"trace_id"`
	SpanID         string          `json:"span_id"`
	Attributes     json.RawMessage `json:"attributes"`
}

type searchCursor struct {
	T  int64  `json:"t"`
	ID string `json:"id"`
}

type execStats struct {
	ElapsedMs int64  `json:"elapsed_ms"`
	RowsRead  uint64 `json:"rows_read"`
	BytesRead uint64 `json:"bytes_read"`
}

type searchResp struct {
	Items          []logItem         `json:"items"`
	NextCursor     *string           `json:"next_cursor"`
	Total          *uint64           `json:"total,omitempty"`
	LevelCounts    map[string]uint64 `json:"level_counts,omitempty"`
	ExecutionStats execStats         `json:"execution_stats"`
	SearchedTiers  []string          `json:"searched_tiers"`
	Partial        bool              `json:"partial"`
}

// query is a WHERE clause with positional arguments.
type query struct {
	where []string
	args  []any
	// The range and streams also pick the archive segments to read.
	from, to time.Time
	streams  []uuid.UUID
}

func (q *query) add(cond string, args ...any) {
	q.where = append(q.where, cond)
	q.args = append(q.args, args...)
}

func (q *query) sql() string { return strings.Join(q.where, " AND ") }

// attrExpr turns "a.b" (or "attributes.a.b") into a ClickHouse subcolumn reference. The
// "attributes." prefix is optional; to reach a top-level key named "attributes", write
// "attributes.attributes...". Each segment is checked against the same alphabet ingest allows
// for keys, and quoted with backticks, which that alphabet cannot contain.
func attrExpr(field string) (string, error) {
	rest := strings.TrimPrefix(field, "attributes.")
	if rest == "" {
		return "", fmt.Errorf("filter field is empty")
	}
	segs := strings.Split(rest, ".")
	if len(segs) > ingest.MaxAttrDepth {
		return "", fmt.Errorf("filter field is deeper than %d levels", ingest.MaxAttrDepth)
	}
	var b strings.Builder
	b.WriteString("attributes")
	for _, s := range segs {
		if !attrSegment.MatchString(s) {
			return "", fmt.Errorf("filter field segment %q is not valid", s)
		}
		b.WriteString(".`" + s + "`")
	}
	return b.String(), nil
}

// buildSearch validates req and returns the WHERE clause. cat maps names to IDs.
func buildSearch(req *searchReq, proj store.Project, cat *catalogEntry, now time.Time) (*query, error) {
	to := now
	if req.To != nil {
		to = req.To.UTC()
	}
	from := to.Add(-24 * time.Hour)
	if req.From != nil {
		from = req.From.UTC()
	}
	if !from.Before(to) {
		return nil, errors.New("from must be before to")
	}
	if to.Sub(from) > searchMaxRange {
		return nil, fmt.Errorf("the time range is longer than %d days", int(searchMaxRange.Hours()/24))
	}
	q := &query{from: from, to: to}
	q.add("workspace_id = ?", proj.WorkspaceID)
	q.add("project_id = ?", proj.ID)
	q.add("timestamp >= fromUnixTimestamp64Milli(?, 'UTC')", from.UnixMilli())
	q.add("timestamp < fromUnixTimestamp64Milli(?, 'UTC')", to.UnixMilli())

	if len(req.Streams) > 0 {
		ids := make([]any, 0, len(req.Streams))
		for _, n := range req.Streams {
			id, ok := cat.streams[n]
			if !ok {
				return nil, fmt.Errorf("stream %q does not exist in this project", n)
			}
			ids = append(ids, id)
			q.streams = append(q.streams, id)
		}
		q.add("stream_id IN ?", clickhouse.GroupSet{Value: ids})
	}
	if len(req.Environments) > 0 {
		ids := make([]any, 0, len(req.Environments))
		for _, n := range req.Environments {
			id, ok := cat.envs[n]
			if !ok {
				return nil, fmt.Errorf("environment %q does not exist in this project", n)
			}
			ids = append(ids, id)
		}
		q.add("environment_id IN ?", clickhouse.GroupSet{Value: ids})
	}
	if req.SeverityMin > 0 {
		q.add("severity_number >= ?", req.SeverityMin)
	}
	if len(req.Levels) > 0 {
		lv := make([]any, 0, len(req.Levels))
		for _, l := range req.Levels {
			_, text, ok := ingest.SeverityNumber(l)
			if !ok {
				return nil, fmt.Errorf("level %q is not known", l)
			}
			lv = append(lv, text)
		}
		q.add("severity_text IN ?", clickhouse.GroupSet{Value: lv})
	}
	if err := listFilter(q, "event_name", req.Events); err != nil {
		return nil, err
	}
	if err := listFilter(q, "source_service", req.Services); err != nil {
		return nil, err
	}
	if err := ingest.ValidateTags(req.Tags); err != nil {
		return nil, err
	}
	for _, tag := range req.Tags {
		q.add("has(tags, ?)", tag)
	}
	if req.TraceID != "" {
		q.add("trace_id = ?", strings.ToLower(req.TraceID))
	}
	if s := strings.TrimSpace(req.Query); s != "" {
		if len(s) > 1024 {
			return nil, errors.New("query is longer than 1024 characters")
		}
		q.add("positionCaseInsensitiveUTF8(message, ?) > 0", s)
	}
	if len(req.Filters) > maxFilters {
		return nil, fmt.Errorf("at most %d filters", maxFilters)
	}
	for _, f := range req.Filters {
		expr, err := attrExpr(f.Field)
		if err != nil {
			return nil, err
		}
		if len(f.Value) > 1024 {
			return nil, errors.New("filter value is longer than 1024 characters")
		}
		switch f.Op {
		case "eq":
			q.add("toString("+expr+") = ?", f.Value)
		case "neq":
			q.add("toString("+expr+") != ?", f.Value)
		case "contains":
			q.add("positionCaseInsensitiveUTF8(toString("+expr+"), ?) > 0", f.Value)
		case "exists":
			// A path to an object ("player" when only "player.id" is stored) reads as NULL, so
			// look at the stored paths instead. ClickHouse stores no path for a null value.
			path := strings.TrimPrefix(f.Field, "attributes.")
			q.add("arrayExists(p -> p = ? OR startsWith(p, ?), JSONAllPaths(attributes))", path, path+".")
		case "gt", "gte", "lt", "lte":
			n, err := strconv.ParseFloat(f.Value, 64)
			if err != nil {
				return nil, fmt.Errorf("filter %s needs a number", f.Op)
			}
			op := map[string]string{"gt": ">", "gte": ">=", "lt": "<", "lte": "<="}[f.Op]
			q.add("toFloat64OrNull(toString("+expr+")) "+op+" ?", n)
		default:
			return nil, fmt.Errorf("filter op must be eq, neq, contains, exists, gt, gte, lt or lte")
		}
	}
	return q, nil
}

func listFilter(q *query, col string, vals []string) error {
	if len(vals) == 0 {
		return nil
	}
	if len(vals) > 100 {
		return fmt.Errorf("at most 100 values for %s", col)
	}
	vs := make([]any, len(vals))
	for i, v := range vals {
		vs[i] = v
	}
	q.add(col+" IN ?", clickhouse.GroupSet{Value: vs})
	return nil
}

const searchCols = `event_id, timestamp, ingested_at, severity_text, severity_number, event_name, message,
	stream_id, environment_id, source_service, source_host, source_resource, trace_id, span_id,
	toJSONString(attributes), tags, tier`

// hotUnionCols are the events columns in the order archive.ReadSource.RemoteSelect returns them.
const hotUnionCols = `workspace_id, project_id, environment_id, stream_id, event_id, timestamp, ingested_at,
	severity_number, severity_text, event_name, message, source_service, source_host, source_resource,
	trace_id, span_id, attributes, tags`

func (a *API) searchLogs(w http.ResponseWriter, r *http.Request) {
	var req searchReq
	if !decode(w, r, &req) {
		return
	}
	proj, ok := a.logsReader(w, r, req.ProjectID)
	if !ok {
		return
	}
	cat, err := a.projectCatalog(r.Context(), proj.WorkspaceID, proj.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	q, err := buildSearch(&req, proj, cat, a.now().UTC())
	if err != nil {
		invalid(w, r, err.Error())
		return
	}
	limit := req.Limit
	if limit <= 0 {
		limit = searchDefaultLimit
	}
	if limit > searchMaxLimit {
		limit = searchMaxLimit
	}

	var rows, bytesRead atomic.Uint64
	ctx := clickhouse.Context(r.Context(),
		clickhouse.WithProgress(func(p *clickhouse.Progress) { rows.Add(p.Rows); bytesRead.Add(p.Bytes) }),
		clickhouse.WithSettings(clickhouse.Settings{"max_execution_time": int(searchTimeout.Seconds()), "readonly": 2}))
	ctx, cancel := context.WithTimeout(ctx, searchTimeout+5*time.Second)
	defer cancel()
	start := time.Now()

	resp := searchResp{Items: []logItem{}}
	source, tiers, partial, err := a.searchSource(r.Context(), proj, q)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	resp.SearchedTiers, resp.Partial = tiers, partial
	if req.IncludeTotal {
		// Counted on the filter without the cursor, so the number stays the same while paging.
		// Duplicates left by a retried insert are counted once.
		crow, err := a.Logs.CH.Query(ctx, "SELECT severity_text, uniqExact(event_id) FROM "+source+" WHERE "+q.sql()+" GROUP BY severity_text", q.args...)
		if err != nil {
			a.searchFailed(w, r, err)
			return
		}
		var total uint64
		resp.LevelCounts = map[string]uint64{}
		for crow.Next() {
			var lv string
			var n uint64
			if err := crow.Scan(&lv, &n); err != nil {
				crow.Close()
				a.searchFailed(w, r, err)
				return
			}
			resp.LevelCounts[lv] = n
			total += n
		}
		crow.Close()
		if err := crow.Err(); err != nil {
			a.searchFailed(w, r, err)
			return
		}
		resp.Total = &total
	}

	if req.Cursor != "" {
		c, err := decodeCursor(req.Cursor)
		if err != nil {
			httpx.WriteProblem(w, r, http.StatusBadRequest, "invalid cursor")
			return
		}
		q.add("(timestamp < fromUnixTimestamp64Milli(?, 'UTC') OR (timestamp = fromUnixTimestamp64Milli(?, 'UTC') AND event_id < ?))", c.T, c.T, c.ID)
	}
	sql := "SELECT " + searchCols + " FROM " + source + " WHERE " + q.sql() +
		" ORDER BY timestamp DESC, event_id DESC, tier DESC LIMIT 1 BY event_id LIMIT " + strconv.Itoa(limit+1)
	rs, err := a.Logs.CH.Query(ctx, sql, q.args...)
	if err != nil {
		a.searchFailed(w, r, err)
		return
	}
	defer rs.Close()
	for rs.Next() {
		var it logItem
		var sid, eid uuid.UUID
		var attrs string
		if err := rs.Scan(&it.ID, &it.Timestamp, &it.IngestedAt, &it.Level, &it.SeverityNumber, &it.Event, &it.Message,
			&sid, &eid, &it.Service, &it.Host, &it.Resource, &it.TraceID, &it.SpanID, &attrs, &it.Tags, &it.Tier); err != nil {
			a.searchFailed(w, r, err)
			return
		}
		it.Timestamp, it.IngestedAt = it.Timestamp.UTC(), it.IngestedAt.UTC()
		it.Stream, it.Environment = cat.streamName[sid], cat.envName[eid]
		it.Attributes = json.RawMessage(attrs)
		resp.Items = append(resp.Items, it)
	}
	if err := rs.Err(); err != nil {
		a.searchFailed(w, r, err)
		return
	}
	if len(resp.Items) > limit {
		resp.Items = resp.Items[:limit]
		last := resp.Items[limit-1]
		c := encodeCursor(searchCursor{T: last.Timestamp.UnixMilli(), ID: last.ID})
		resp.NextCursor = &c
	}
	resp.ExecutionStats = execStats{ElapsedMs: time.Since(start).Milliseconds(), RowsRead: rows.Load(), BytesRead: bytesRead.Load()}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// searchSource returns the FROM expression for a search: the hot table alone, or the hot table
// plus the cached archive segments of the range. partial reports segments that could not be read.
func (a *API) searchSource(ctx context.Context, proj store.Project, q *query) (source string, tiers []string, partial bool, err error) {
	source, tiers = "(SELECT *, 'hot' AS tier FROM events)", []string{"hot"}
	if a.Logs.Archive == nil {
		return source, tiers, false, nil
	}
	srcs, missing, err := a.Logs.Archive.ReadSources(ctx, proj.WorkspaceID, proj.ID, q.streams, q.from, q.to)
	if err != nil {
		return "", nil, false, err
	}
	if len(srcs) > 0 || missing > 0 {
		tiers = append(tiers, "archive")
	}
	if len(srcs) > 0 {
		// Evicted segments still read alongside the hot table: a late row or the moment
		// between marking and deleting leaves copies in both, and LIMIT 1 BY event_id and
		// uniqExact(event_id) count each event once.
		parts := make([]string, 0, len(srcs)+1)
		parts = append(parts, "SELECT "+hotUnionCols+", 'hot' AS tier FROM events")
		for _, s := range srcs {
			parts = append(parts, s.RemoteSelect())
		}
		source = "(" + strings.Join(parts, " UNION ALL ") + ")"
	}
	return source, tiers, missing > 0, nil
}

func (a *API) searchFailed(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "TIMEOUT_EXCEEDED") {
		httpx.WriteProblem(w, r, http.StatusGatewayTimeout, "the search took too long: narrow the time range or add filters")
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	a.Log.Error("search failed", "request_id", httpx.RequestID(r.Context()), "error", err)
	httpx.WriteProblem(w, r, http.StatusServiceUnavailable, "log storage is unavailable")
}

func encodeCursor(c searchCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (searchCursor, error) {
	var c searchCursor
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) > 512 {
		return c, errors.New("bad cursor")
	}
	if err := json.Unmarshal(b, &c); err != nil || c.ID == "" {
		return c, errors.New("bad cursor")
	}
	return c, nil
}
