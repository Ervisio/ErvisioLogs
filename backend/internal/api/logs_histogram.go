package api

import (
	"context"
	"net/http"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
)

type histogramRequest struct {
	searchReq
	IntervalSeconds uint32 `json:"interval_seconds"`
}

type histogramBucket struct {
	Timestamp time.Time `json:"timestamp"`
	Count     uint64    `json:"count"`
}

func (a *API) histogramLogs(w http.ResponseWriter, r *http.Request) {
	var req histogramRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Cursor != "" || req.Limit != 0 {
		invalid(w, r, "a histogram takes no cursor or limit")
		return
	}
	if req.IntervalSeconds == 0 {
		req.IntervalSeconds = 300
	}
	switch req.IntervalSeconds {
	case 60, 300, 900, 3600, 21600, 86400:
	default:
		invalid(w, r, "interval_seconds must be 60, 300, 900, 3600, 21600 or 86400")
		return
	}
	project, ok := a.logsReader(w, r, req.ProjectID)
	if !ok {
		return
	}
	catalog, err := a.projectCatalog(r.Context(), project.WorkspaceID, project.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	q, err := buildSearch(&req.searchReq, project, catalog, a.now().UTC())
	if err != nil {
		invalid(w, r, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), searchTimeout+5*time.Second)
	defer cancel()
	source, tiers, partial, err := a.searchSource(ctx, project, q)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"max_execution_time": int(searchTimeout.Seconds()), "readonly": 2}))
	args := append([]any{req.IntervalSeconds}, q.args...)
	rows, err := a.Logs.CH.Query(ctx, "SELECT toStartOfInterval(timestamp, toIntervalSecond(?), 'UTC') AS bucket, uniqExact(event_id) FROM "+source+" WHERE "+q.sql()+" GROUP BY bucket ORDER BY bucket", args...)
	if err != nil {
		a.searchFailed(w, r, err)
		return
	}
	defer rows.Close()
	buckets := []histogramBucket{}
	for rows.Next() {
		var bucket histogramBucket
		if err := rows.Scan(&bucket.Timestamp, &bucket.Count); err != nil {
			a.searchFailed(w, r, err)
			return
		}
		bucket.Timestamp = bucket.Timestamp.UTC()
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		a.searchFailed(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": buckets, "partial": partial, "searched_tiers": tiers})
}
