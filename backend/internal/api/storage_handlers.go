package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/archive"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

// Storage is the archive administration of the instance (ADR 0004, ADR 0008).
type Storage struct {
	// Archiver is nil when the archiver is off. Providers and policies can still be set.
	Archiver *archive.Archiver
	// SecretsDir holds provider secret files. The API opens them only to test a provider and
	// never returns their content.
	SecretsDir string
}

// maxRetentionHours caps a retention age at ten years.
const maxRetentionHours = 24 * 3660

type storageUsage struct {
	HotBytes     int64 `json:"hot_bytes"`
	CacheBytes   int64 `json:"cache_bytes"`
	MaxBytes     int64 `json:"max_bytes"`
	HighPct      int   `json:"high_pct"`
	LowPct       int   `json:"low_pct"`
	OverCapacity bool  `json:"over_capacity"`
}

type segmentTotals struct {
	Count int64 `json:"count"`
	Bytes int64 `json:"bytes"`
}

func (a *API) archiver() *archive.Archiver {
	if a.Storage == nil {
		return nil
	}
	return a.Storage.Archiver
}

func (a *API) adminGetStorage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	ctx := r.Context()
	provs, err := a.Store.ListProviders(ctx)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	sum, err := a.Store.ArchiveSummary(ctx)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	segs := map[string]segmentTotals{}
	for st, v := range sum {
		segs[st] = segmentTotals{v.Count, v.Bytes}
	}
	rows, err := a.Store.RetentionPolicies(ctx)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	b := archive.NewPolicies(rows).Budget()
	out := map[string]any{"providers": provs, "segments": segs, "archiver_enabled": a.archiver() != nil}
	if arch := a.archiver(); arch != nil {
		u := storageUsage{MaxBytes: b.MaxBytes, HighPct: b.HighPct, LowPct: b.LowPct, OverCapacity: arch.OverCapacity()}
		if u.HotBytes, u.CacheBytes, err = arch.Usage(ctx); err != nil {
			// The page still shows providers and segments when ClickHouse is down.
			a.Log.Warn("storage usage unavailable", "error", err)
		} else {
			out["usage"] = u
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type providerReq struct {
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Config     json.RawMessage `json:"config"`
	SecretFile *string         `json:"secret_file"`
}

// checkProvider opens the provider and runs its health check. cfgErr means the settings are
// invalid. health is the check's result, stored as the provider status.
func (a *API) checkProvider(ctx context.Context, typ string, cfg json.RawMessage, secretFile *string) (cfgErr, health error) {
	secret, dir := "", ""
	if secretFile != nil {
		secret = *secretFile
	}
	if a.Storage != nil {
		dir = a.Storage.SecretsDir
	}
	p, err := archive.Open(typ, cfg, secret, dir)
	if err != nil {
		return err, nil
	}
	defer p.Close()
	hctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return nil, p.Health(hctx)
}

func validProviderReq(req *providerReq) string {
	switch {
	case len(req.Name) < 1 || len(req.Name) > 100:
		return "name must be 1 to 100 characters"
	case req.Type != archive.TypeS3 && req.Type != archive.TypeSFTP && req.Type != archive.TypeLocal:
		return "type must be s3, sftp or local"
	case req.SecretFile != nil && !archive.ValidSecretFile(*req.SecretFile):
		return "secret_file must be a plain file name inside the secrets directory"
	}
	if len(req.Config) == 0 || string(req.Config) == "null" {
		req.Config = json.RawMessage("{}")
	}
	return ""
}

func (a *API) adminCreateProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	var req providerReq
	if !decode(w, r, &req) {
		return
	}
	if msg := validProviderReq(&req); msg != "" {
		invalid(w, r, msg)
		return
	}
	cfgErr, health := a.checkProvider(r.Context(), req.Type, req.Config, req.SecretFile)
	if cfgErr != nil {
		invalid(w, r, cfgErr.Error())
		return
	}
	p, err := a.Store.CreateProvider(r.Context(), req.Name, req.Type, req.Config, req.SecretFile)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	if err := a.Store.SetProviderStatus(r.Context(), p.ID, health); err != nil {
		a.internalError(w, r, err)
		return
	}
	a.audit(r, nil, "storage.provider.create", "storage_provider", p.ID.String(), map[string]any{"name": p.Name, "type": p.Type})
	a.writeProvider(w, r, p.ID, http.StatusCreated)
}

func (a *API) writeProvider(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int) {
	p, err := a.Store.ProviderByID(r.Context(), id)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, status, p)
}

// adminUpdateProvider replaces the name, config and secret file. The type never changes. The
// config of a provider that holds segments cannot change either, because the segments' object
// keys would then point into another location. Rotating the secret file stays possible.
func (a *API) adminUpdateProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	var req providerReq
	if !decode(w, r, &req) {
		return
	}
	cur, err := a.Store.ProviderByID(r.Context(), id)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	if req.Type == "" {
		req.Type = cur.Type
	}
	if req.Type != cur.Type {
		invalid(w, r, "the type of a provider cannot change; create a new provider")
		return
	}
	if msg := validProviderReq(&req); msg != "" {
		invalid(w, r, msg)
		return
	}
	if !jsonEqual(req.Config, cur.Config) {
		used, err := a.Store.ProviderInUse(r.Context(), id)
		if err != nil {
			a.internalError(w, r, err)
			return
		}
		if used {
			httpx.WriteProblem(w, r, http.StatusConflict, "this provider holds archive segments, so its location cannot change; create a new provider and make it active")
			return
		}
	}
	cfgErr, health := a.checkProvider(r.Context(), req.Type, req.Config, req.SecretFile)
	if cfgErr != nil {
		invalid(w, r, cfgErr.Error())
		return
	}
	if _, err := a.Store.UpdateProvider(r.Context(), id, req.Name, req.Config, req.SecretFile); err != nil {
		a.storeError(w, r, err)
		return
	}
	if err := a.Store.SetProviderStatus(r.Context(), id, health); err != nil {
		a.internalError(w, r, err)
		return
	}
	a.audit(r, nil, "storage.provider.update", "storage_provider", id.String(), map[string]any{"name": req.Name})
	a.writeProvider(w, r, id, http.StatusOK)
}

func jsonEqual(x, y json.RawMessage) bool {
	var a, b any
	if json.Unmarshal(x, &a) != nil || json.Unmarshal(y, &b) != nil {
		return false
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

func (a *API) adminTestProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	p, err := a.Store.ProviderByID(r.Context(), id)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	cfgErr, health := a.checkProvider(r.Context(), p.Type, p.Config, p.SecretFile)
	if cfgErr != nil {
		health = cfgErr
	}
	if err := a.Store.SetProviderStatus(r.Context(), id, health); err != nil {
		a.internalError(w, r, err)
		return
	}
	a.writeProvider(w, r, id, http.StatusOK)
}

func (a *API) adminDeleteProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	if err := a.Store.DeleteProvider(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrProviderInUse) {
			httpx.WriteProblem(w, r, http.StatusConflict, "this provider holds archive segments; it stays until retention deletes them")
			return
		}
		a.storeError(w, r, err)
		return
	}
	a.audit(r, nil, "storage.provider.delete", "storage_provider", id.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}

// adminSetActiveProvider picks the provider that receives new segments, after a passing health
// check. A null provider_id stops archiving; hot data then stays until global retention.
func (a *API) adminSetActiveProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	var req struct {
		ProviderID *uuid.UUID `json:"provider_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.ProviderID != nil {
		p, err := a.Store.ProviderByID(r.Context(), *req.ProviderID)
		if err != nil {
			a.storeError(w, r, err)
			return
		}
		cfgErr, health := a.checkProvider(r.Context(), p.Type, p.Config, p.SecretFile)
		if cfgErr != nil {
			health = cfgErr
		}
		if err := a.Store.SetProviderStatus(r.Context(), p.ID, health); err != nil {
			a.internalError(w, r, err)
			return
		}
		if health != nil {
			invalid(w, r, "the provider failed its health check: "+health.Error())
			return
		}
	}
	if err := a.Store.SetActiveProvider(r.Context(), req.ProviderID); err != nil {
		a.storeError(w, r, err)
		return
	}
	target := ""
	if req.ProviderID != nil {
		target = req.ProviderID.String()
	}
	a.audit(r, nil, "storage.provider.activate", "storage_provider", target, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- retention ----------

func (a *API) adminListRetention(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	rows, err := a.Store.RetentionPolicies(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if rows == nil {
		rows = []store.RetentionPolicy{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": rows})
}

type retentionReq struct {
	LocalAgeHours  *int32 `json:"local_age_hours"`
	GlobalAgeHours *int32 `json:"global_age_hours"`
	CacheMaxBytes  *int64 `json:"cache_max_bytes"`
	HighPct        *int16 `json:"high_pct"`
	LowPct         *int16 `json:"low_pct"`
}

func validRetention(scopeType string, req retentionReq) string {
	for _, h := range []*int32{req.LocalAgeHours, req.GlobalAgeHours} {
		if h != nil && (*h < 1 || *h > maxRetentionHours) {
			return "retention ages must be between 1 hour and 10 years"
		}
	}
	if req.LocalAgeHours != nil && req.GlobalAgeHours != nil && *req.GlobalAgeHours < *req.LocalAgeHours {
		return "global_age_hours must be at least local_age_hours"
	}
	if scopeType != "instance" {
		if req.CacheMaxBytes != nil || req.HighPct != nil || req.LowPct != nil {
			return "the cache budget is set on the instance policy only"
		}
		if req.LocalAgeHours == nil && req.GlobalAgeHours == nil {
			return "an override needs local_age_hours or global_age_hours"
		}
		return ""
	}
	if req.LocalAgeHours == nil || req.GlobalAgeHours == nil || req.CacheMaxBytes == nil || req.HighPct == nil || req.LowPct == nil {
		return "the instance policy needs every field"
	}
	if *req.CacheMaxBytes < 1<<20 {
		return "cache_max_bytes must be at least 1 MiB"
	}
	if *req.HighPct < 1 || *req.HighPct > 100 || *req.LowPct < 1 || *req.LowPct >= *req.HighPct {
		return "watermarks need 1 <= low_pct < high_pct <= 100"
	}
	return ""
}

func retentionScope(w http.ResponseWriter, r *http.Request) (string, uuid.UUID, bool) {
	scopeType := chi.URLParam(r, "scopeType")
	scopeID, err := uuid.Parse(chi.URLParam(r, "scopeId"))
	switch scopeType {
	case "instance", "workspace", "project", "stream":
	default:
		err = errors.New("unknown scope")
	}
	if err != nil {
		httpx.WriteProblem(w, r, http.StatusNotFound, "not found")
		return "", uuid.Nil, false
	}
	return scopeType, scopeID, true
}

func (a *API) adminPutRetention(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	scopeType, scopeID, ok := retentionScope(w, r)
	if !ok {
		return
	}
	var req retentionReq
	if !decode(w, r, &req) {
		return
	}
	exists, err := a.Store.RetentionScopeExists(r.Context(), scopeType, scopeID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if !exists {
		httpx.WriteProblem(w, r, http.StatusNotFound, "not found")
		return
	}
	if msg := validRetention(scopeType, req); msg != "" {
		invalid(w, r, msg)
		return
	}
	p, err := a.Store.PutRetentionPolicy(r.Context(), store.RetentionPolicy{ScopeType: scopeType, ScopeID: scopeID,
		LocalAgeHours: req.LocalAgeHours, GlobalAgeHours: req.GlobalAgeHours, CacheMaxBytes: req.CacheMaxBytes,
		HighPct: req.HighPct, LowPct: req.LowPct, UpdatedBy: &admin.ID})
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, nil, "retention.update", scopeType, scopeID.String(), map[string]any{
		"local_age_hours": req.LocalAgeHours, "global_age_hours": req.GlobalAgeHours})
	httpx.WriteJSON(w, http.StatusOK, p)
}

func (a *API) adminDeleteRetention(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	scopeType, scopeID, ok := retentionScope(w, r)
	if !ok {
		return
	}
	if err := a.Store.DeleteRetentionPolicy(r.Context(), scopeType, scopeID); err != nil {
		if errors.Is(err, store.ErrForbidden) {
			invalid(w, r, "the instance policy cannot be removed; change it instead")
			return
		}
		a.storeError(w, r, err)
		return
	}
	a.audit(r, nil, "retention.delete", scopeType, scopeID.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}
