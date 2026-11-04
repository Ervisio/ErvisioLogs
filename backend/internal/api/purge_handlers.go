package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/archive"
	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

const purgeListLimit = 50

// createPurge queues a manual purge of a project's logs (ADR 0020). Deleting hot rows and archived
// objects can take minutes, so the request validates, records the job and answers 202; the archiver
// process runs it and the job row carries the outcome.
func (a *API) createPurge(w http.ResponseWriter, r *http.Request) {
	p, u, ok := a.projectAccess(w, r, auth.LogsDelete)
	if !ok {
		return
	}
	var req struct {
		Confirm     string     `json:"confirm"`
		Stream      string     `json:"stream"`
		Environment string     `json:"environment"`
		From        *time.Time `json:"from"`
		To          *time.Time `json:"to"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Confirm != p.Slug {
		invalid(w, r, "confirm must equal the project slug")
		return
	}
	// ClickHouse stores milliseconds; cut here so the catalog check and the delete see the same range.
	job := store.PurgeJob{WorkspaceID: p.WorkspaceID, ProjectID: p.ID, CreatedBy: &u.ID}
	if req.From != nil {
		job.From = ptrTo(req.From.UTC().Truncate(time.Millisecond))
	}
	if req.To != nil {
		job.To = ptrTo(req.To.UTC().Truncate(time.Millisecond))
	}
	if job.From != nil && job.To != nil && !job.From.Before(*job.To) {
		invalid(w, r, "from must be before to")
		return
	}
	arch := a.archiver()
	if arch == nil {
		httpx.WriteProblem(w, r, http.StatusServiceUnavailable, "purges run in the archiver, which is off in this process")
		return
	}
	cat, err := a.projectCatalog(r.Context(), p.WorkspaceID, p.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if req.Stream != "" {
		id, found, _ := cat.Stream(req.Stream)
		if !found {
			invalid(w, r, "unknown stream")
			return
		}
		job.StreamID = &id
	}
	if req.Environment != "" {
		id, found, _ := cat.Environment(req.Environment)
		if !found {
			invalid(w, r, "unknown environment")
			return
		}
		job.EnvironmentID = &id
	}
	if open, err := a.Store.HasOpenPurge(r.Context(), p.ID); err != nil {
		a.internalError(w, r, err)
		return
	} else if open {
		httpx.WriteProblem(w, r, http.StatusConflict, "a purge is already queued or running for this project")
		return
	}
	if _, err := arch.PlanPurge(r.Context(), job); errors.Is(err, archive.ErrPurgeCutsSegment) {
		invalid(w, r, err.Error())
		return
	} else if err != nil {
		a.internalError(w, r, err)
		return
	}
	created, err := a.Store.CreatePurgeJob(r.Context(), job)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &p.WorkspaceID, "logs.purge", "project", p.ID.String(), map[string]any{
		"job": created.ID, "stream": req.Stream, "environment": req.Environment, "from": created.From, "to": created.To})
	w.Header().Set("Location", "/api/v1/projects/"+p.ID.String()+"/purges/"+created.ID.String())
	httpx.WriteJSON(w, http.StatusAccepted, created)
}

func (a *API) listPurges(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.projectAccess(w, r, auth.LogsDelete)
	if !ok {
		return
	}
	jobs, err := a.Store.ListPurgeJobs(r.Context(), p.WorkspaceID, p.ID, purgeListLimit)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, jobs)
}

func (a *API) getPurge(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.projectAccess(w, r, auth.LogsDelete)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "jobId")
	if !ok {
		return
	}
	job, err := a.Store.PurgeJob(r.Context(), p.WorkspaceID, p.ID, id)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, job)
}

func ptrTo[T any](v T) *T { return &v }
