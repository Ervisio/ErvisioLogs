package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
)

const (
	exportTimeout    = 5 * time.Minute
	exportFlushEvery = 1000
)

// exportReq is a search filter plus the output format. Limit and cursor do not apply: an
// export streams every match up to the instance cap.
type exportReq struct {
	searchReq
	Format string `json:"format"`
}

var csvHeader = []string{"timestamp", "level", "severity_number", "event", "message", "stream", "environment",
	"service", "host", "resource", "trace_id", "span_id", "attributes", "id", "ingested_at"}

// exportLogs streams the events matching a search as NDJSON or CSV, newest first (ADR 0010).
// When the instance cap cuts the export short, the X-Export-Truncated trailer says so.
func (a *API) exportLogs(w http.ResponseWriter, r *http.Request) {
	var req exportReq
	if !decode(w, r, &req) {
		return
	}
	if req.Cursor != "" || req.Limit != 0 {
		invalid(w, r, "an export takes no limit or cursor")
		return
	}
	if req.Format == "" {
		req.Format = "ndjson"
	}
	if req.Format != "ndjson" && req.Format != "csv" {
		invalid(w, r, "format must be ndjson or csv")
		return
	}
	// logs:export is enough on its own: an export-only token needs no logs:read.
	proj, ok := a.logsAccess(w, r, req.ProjectID, auth.LogsExport)
	if !ok {
		return
	}
	cat, err := a.projectCatalog(r.Context(), proj.WorkspaceID, proj.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	req.ProjectID = proj.ID
	q, err := buildSearch(&req.searchReq, proj, cat, a.now().UTC())
	if err != nil {
		invalid(w, r, err.Error())
		return
	}
	maxRows := int(a.Settings.Int(settings.ExportMaxRows))

	ctx := clickhouse.Context(r.Context(), clickhouse.WithSettings(clickhouse.Settings{
		"max_execution_time": int(exportTimeout.Seconds()), "readonly": 2}))
	ctx, cancel := context.WithTimeout(ctx, exportTimeout+10*time.Second)
	defer cancel()
	source, tiers, partial, err := a.searchSource(r.Context(), proj, q)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	sql := "SELECT " + searchCols + " FROM " + source + " WHERE " + q.sql() +
		" ORDER BY timestamp DESC, event_id DESC LIMIT 1 BY event_id LIMIT " + strconv.Itoa(maxRows+1)
	rs, err := a.Logs.CH.Query(ctx, sql, q.args...)
	if err != nil {
		a.searchFailed(w, r, err)
		return
	}
	defer rs.Close()

	ext, ctype := "ndjson", "application/x-ndjson"
	if req.Format == "csv" {
		ext, ctype = "csv", "text/csv; charset=utf-8"
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="logs-%s-%s.%s"`,
		proj.ID.String()[:8], a.now().UTC().Format("20060102T150405Z"), ext))
	h.Set("Cache-Control", "no-store")
	h.Set("X-Searched-Tiers", joinTiers(tiers))
	if partial {
		h.Set("X-Search-Partial", "true")
	}
	h.Set("Trailer", "X-Export-Truncated, X-Export-Error")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	var cw *csv.Writer
	enc := json.NewEncoder(w)
	if req.Format == "csv" {
		cw = csv.NewWriter(w)
		_ = cw.Write(csvHeader)
	}
	written, truncated := 0, false
	var streamErr error
	for rs.Next() {
		if written == maxRows {
			truncated = true
			break
		}
		var it logItem
		var sid, eid uuid.UUID
		var attrs string
		if err := rs.Scan(&it.ID, &it.Timestamp, &it.IngestedAt, &it.Level, &it.SeverityNumber, &it.Event, &it.Message,
			&sid, &eid, &it.Service, &it.Host, &it.Resource, &it.TraceID, &it.SpanID, &attrs); err != nil {
			streamErr = err
			break
		}
		it.Timestamp, it.IngestedAt = it.Timestamp.UTC(), it.IngestedAt.UTC()
		it.Stream, it.Environment = cat.streamName[sid], cat.envName[eid]
		it.Attributes = json.RawMessage(attrs)
		if cw != nil {
			err = cw.Write(csvRow(it))
		} else {
			err = enc.Encode(it)
		}
		if err != nil {
			streamErr = err // the client went away
			break
		}
		written++
		if written%exportFlushEvery == 0 {
			if cw != nil {
				cw.Flush()
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
	if streamErr == nil {
		streamErr = rs.Err()
	}
	if cw != nil {
		cw.Flush()
	}
	if truncated {
		h.Set("X-Export-Truncated", "true")
	}
	if streamErr != nil && r.Context().Err() == nil {
		a.Log.Warn("export stopped early", "project_id", proj.ID, "rows", written, "error", streamErr)
		h.Set("X-Export-Error", "the export stopped before the end")
	}
	detail := map[string]any{"project_id": proj.ID, "format": req.Format, "rows": written, "truncated": truncated}
	if streamErr != nil {
		detail["incomplete"] = true
	}
	a.audit(r.WithContext(context.WithoutCancel(r.Context())), &proj.WorkspaceID, "logs.export", "project", proj.ID.String(), detail)
}

func joinTiers(t []string) string {
	s := ""
	for i, v := range t {
		if i > 0 {
			s += ","
		}
		s += v
	}
	return s
}

// csvRow renders an event. Cells a spreadsheet would read as a formula get a leading quote.
func csvRow(it logItem) []string {
	row := []string{it.Timestamp.Format(time.RFC3339Nano), it.Level, strconv.Itoa(int(it.SeverityNumber)), it.Event,
		it.Message, it.Stream, it.Environment, it.Service, it.Host, it.Resource, it.TraceID, it.SpanID,
		string(it.Attributes), it.ID, it.IngestedAt.Format(time.RFC3339Nano)}
	for i, c := range row {
		row[i] = csvSafe(c)
	}
	return row
}

func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}
