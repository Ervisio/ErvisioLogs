package api

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
)

// MetricsHandler serves Prometheus text metrics. With a token, scrapers must send it as a Bearer
// token. Without one, the endpoint answers only when open is true (development mode).
func (a *API) MetricsHandler(token string, open bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token == "" && !open {
			httpx.WriteProblem(w, r, http.StatusNotFound, "not found")
			return
		}
		if token != "" {
			got, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				httpx.WriteProblem(w, r, http.StatusUnauthorized, "invalid metrics token")
				return
			}
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		var b strings.Builder
		metric := func(name, typ, help string, v any) {
			fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n%s %v\n", name, help, name, typ, name, v)
		}
		if l := a.Logs; l != nil {
			s := l.WAL.Stats()
			metric("ervisio_wal_bytes", "gauge", "Bytes held by the ingest WAL.", s.Bytes)
			metric("ervisio_wal_max_bytes", "gauge", "Disk budget of the ingest WAL.", s.MaxBytes)
			metric("ervisio_wal_segments", "gauge", "WAL segment files.", s.Segments)
			metric("ervisio_wal_pending_records", "gauge", "WAL records not yet written to ClickHouse.", s.NextSeq-s.Checkpoint)
			metric("ervisio_wal_appends_total", "counter", "Records appended to the WAL.", s.Appends)
			metric("ervisio_wal_fsyncs_total", "counter", "WAL fsync calls.", s.Syncs)
			metric("ervisio_wal_full_rejections_total", "counter", "Appends refused because the WAL was full.", s.FullRejections)
			if wr := l.Writer; wr != nil {
				metric("ervisio_ingest_inserts_total", "counter", "ClickHouse inserts.", wr.Stats.Inserts.Load())
				metric("ervisio_ingest_insert_errors_total", "counter", "Failed ClickHouse inserts (retried).", wr.Stats.InsertErrors.Load())
				metric("ervisio_ingest_events_written_total", "counter", "Events written to ClickHouse.", wr.Stats.EventsWritten.Load())
				metric("ervisio_ingest_bad_records_total", "counter", "WAL records skipped because they did not decode.", wr.Stats.BadRecords.Load())
				metric("ervisio_ingest_last_insert_ms", "gauge", "Duration of the last ClickHouse insert.", wr.Stats.LastInsertMs.Load())
				metric("ervisio_ingest_lag_seconds", "gauge", "Age of the oldest event waiting for ClickHouse.", wr.Stats.LagSeconds.Load())
			}
			if l.Hub != nil {
				metric("ervisio_tail_subscribers", "gauge", "Open live tail connections.", l.Hub.Subscribers())
			}
		}
		_, _ = w.Write([]byte(b.String()))
	}
}
