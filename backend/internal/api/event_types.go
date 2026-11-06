package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
	"github.com/go-chi/chi/v5"
)

var eventNamePattern = regexp.MustCompile(`^[A-Za-z0-9._:/-]{1,128}$`)
var eventColorPattern = regexp.MustCompile(`^#[0-9a-f]{6}$`)

func validateEventType(e store.EventType) error {
	if !eventNamePattern.MatchString(e.Name) {
		return fmt.Errorf("event name is invalid")
	}
	for _, v := range []struct {
		s string
		n int
	}{{e.Title, 150}, {e.Template, 2000}, {e.Icon, 64}, {e.Category, 100}} {
		if !utf8.ValidString(v.s) || len(v.s) > v.n {
			return fmt.Errorf("event metadata is invalid or too long")
		}
	}
	if e.Color != "" && !eventColorPattern.MatchString(e.Color) {
		return fmt.Errorf("color must be #rrggbb")
	}
	if len(e.FeaturedFields) > 10 {
		return fmt.Errorf("at most 10 featured fields")
	}
	for _, f := range e.FeaturedFields {
		if _, err := attrExpr(f); err != nil {
			return err
		}
	}
	return nil
}

func (a *API) listEventTypes(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.projectAccess(w, r, auth.LogsRead)
	if !ok {
		return
	}
	out, err := a.Store.ListEventTypes(r.Context(), p.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, out)
}

func (a *API) saveEventType(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.projectAccess(w, r, auth.ProjectsWrite)
	if !ok {
		return
	}
	var e store.EventType
	if r.Method == http.MethodPatch {
		name, err := url.PathUnescape(chi.URLParam(r, "eventName"))
		if err != nil {
			invalid(w, r, "event name is invalid")
			return
		}
		e, err = a.Store.GetEventType(r.Context(), p.ID, name)
		if err != nil {
			a.storeError(w, r, err)
			return
		}
		var patch struct {
			Title          *string   `json:"title"`
			Template       *string   `json:"template"`
			Icon           *string   `json:"icon"`
			Category       *string   `json:"category"`
			Color          *string   `json:"color"`
			FeaturedFields *[]string `json:"featured_fields"`
		}
		if !decode(w, r, &patch) {
			return
		}
		if patch.Title != nil {
			e.Title = *patch.Title
		}
		if patch.Template != nil {
			e.Template = *patch.Template
		}
		if patch.Icon != nil {
			e.Icon = *patch.Icon
		}
		if patch.Category != nil {
			e.Category = *patch.Category
		}
		if patch.Color != nil {
			e.Color = *patch.Color
		}
		if patch.FeaturedFields != nil {
			e.FeaturedFields = *patch.FeaturedFields
		}
	} else {
		var req struct {
			Name           string   `json:"name"`
			Title          string   `json:"title"`
			Template       string   `json:"template"`
			Icon           string   `json:"icon"`
			Category       string   `json:"category"`
			Color          string   `json:"color"`
			FeaturedFields []string `json:"featured_fields"`
		}
		if !decode(w, r, &req) {
			return
		}
		e = store.EventType{Name: req.Name, Title: req.Title, Template: req.Template, Icon: req.Icon, Category: req.Category, Color: req.Color, FeaturedFields: req.FeaturedFields}
	}
	if err := validateEventType(e); err != nil {
		invalid(w, r, err.Error())
		return
	}
	e.Discovered = false
	out, err := a.Store.SaveEventType(r.Context(), p.ID, e)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &p.WorkspaceID, "event_type.save", "event_type", e.Name, nil)
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) deleteEventType(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.projectAccess(w, r, auth.ProjectsWrite)
	if !ok {
		return
	}
	name, err := url.PathUnescape(chi.URLParam(r, "eventName"))
	if err != nil {
		invalid(w, r, "event name is invalid")
		return
	}
	if err := a.Store.DeleteEventType(r.Context(), p.ID, name); err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &p.WorkspaceID, "event_type.delete", "event_type", name, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) fieldRules(w http.ResponseWriter, r *http.Request) {
	perm := auth.LogsRead
	if r.Method == http.MethodPut {
		perm = auth.ProjectsWrite
	}
	p, _, ok := a.projectAccess(w, r, perm)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		out, err := a.Store.FieldRules(r.Context(), p.ID)
		if err != nil {
			a.internalError(w, r, err)
			return
		}
		items(w, out)
		return
	}
	var req struct {
		Items []store.FieldRule `json:"items"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Items == nil {
		req.Items = []store.FieldRule{}
	}
	if err := validateFieldRules(req.Items); err != nil {
		invalid(w, r, err.Error())
		return
	}
	if err := a.Store.SaveFieldRules(r.Context(), p.ID, req.Items); err != nil {
		a.internalError(w, r, err)
		return
	}
	a.audit(r, &p.WorkspaceID, "field_rules.save", "project", p.ID.String(), nil)
	items(w, req.Items)
}

func validateFieldRules(rules []store.FieldRule) error {
	if len(rules) > 100 {
		return fmt.Errorf("at most 100 field rules")
	}
	seen := map[string]bool{}
	for _, rule := range rules {
		if _, err := attrExpr(rule.Field); err != nil {
			return err
		}
		path := strings.TrimPrefix(rule.Field, "attributes.")
		if seen[path] {
			return fmt.Errorf("duplicate field rule")
		}
		seen[path] = true
		if len(rule.Label) > 100 || !utf8.ValidString(rule.Label) {
			return fmt.Errorf("field label must be valid UTF-8 up to 100 bytes")
		}
		switch rule.Format {
		case "", "text", "number", "currency", "datetime", "json":
		default:
			return fmt.Errorf("unknown field format")
		}
	}
	return nil
}

func redactAttributes(raw json.RawMessage, rules []store.FieldRule) (json.RawMessage, error) {
	hasRedaction := false
	for _, rule := range rules {
		if rule.Redact {
			hasRedaction = true
			break
		}
	}
	if !hasRedaction {
		return raw, nil
	}
	var obj map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&obj); err != nil {
		return nil, err
	}
	for _, rule := range rules {
		if !rule.Redact {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(rule.Field, "attributes."), ".")
		node := obj
		for i, key := range parts {
			if i == len(parts)-1 {
				if _, ok := node[key]; ok {
					node[key] = "[REDACTED]"
				}
				break
			}
			next, ok := node[key].(map[string]any)
			if !ok {
				break
			}
			node = next
		}
	}
	return json.Marshal(obj)
}

func (a *API) fiveMEventPreset(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.projectAccess(w, r, auth.ProjectsWrite)
	if !ok {
		return
	}
	presets := []store.EventType{
		{Name: "player.join", Title: "Player joined", Template: "Player {actor.id} joined the server", Icon: "user-plus", Category: "players", Color: "#22c55e", FeaturedFields: []string{"actor.id"}},
		{Name: "player.leave", Title: "Player left", Template: "Player {actor.id} left: {reason}", Icon: "user-minus", Category: "players", Color: "#64748b", FeaturedFields: []string{"actor.id", "reason"}},
		{Name: "money.add", Title: "Money received", Template: "Player {actor.id} received {amount} ({account})", Icon: "wallet", Category: "economy", Color: "#22c55e", FeaturedFields: []string{"amount", "account"}},
		{Name: "inventory.add", Title: "Item received", Template: "Player {actor.id} received {count} × {item}", Icon: "package", Category: "inventory", Color: "#3b82f6", FeaturedFields: []string{"item", "count"}},
		{Name: "money.remove", Title: "Money removed", Template: "Player {actor.id} spent {amount} ({account})", Icon: "wallet", Category: "economy", Color: "#f59e0b", FeaturedFields: []string{"amount", "account"}},
		{Name: "money.set", Title: "Balance set", Template: "Player {actor.id} balance set to {amount} ({account})", Icon: "wallet", Category: "economy", Color: "#3b82f6", FeaturedFields: []string{"amount", "account"}},
		{Name: "money.transfer", Title: "Money transferred", Template: "Player {actor.id} transferred {amount} to player {target.id}", Icon: "arrow-right-left", Category: "economy", Color: "#22c55e", FeaturedFields: []string{"amount", "account"}},
		{Name: "job.change", Title: "Job changed", Template: "Player {actor.id} changed job to {job}", Icon: "briefcase", Category: "players", Color: "#3b82f6", FeaturedFields: []string{"job", "job_grade"}},
		{Name: "inventory.remove", Title: "Item removed", Template: "Player {actor.id} lost {count} × {item}", Icon: "package", Category: "inventory", Color: "#f59e0b", FeaturedFields: []string{"item", "count"}},
		{Name: "inventory.move", Title: "Item moved", Template: "Player {actor.id} moved {count} × {item}", Icon: "package", Category: "inventory", Color: "#3b82f6", FeaturedFields: []string{"item", "count"}},
		{Name: "inventory.buy", Title: "Item purchased", Template: "Player {actor.id} bought {count} × {item}", Icon: "shopping-cart", Category: "inventory", Color: "#22c55e", FeaturedFields: []string{"item", "count", "price"}},
		{Name: "inventory.create", Title: "Item created", Template: "Created {count} × {item}", Icon: "package-plus", Category: "inventory", Color: "#64748b", FeaturedFields: []string{"item", "count"}},
		{Name: "moderation.kick", Title: "Player kicked", Template: "Player {target.id} kicked: {reason}", Icon: "shield", Category: "moderation", Color: "#f59e0b", FeaturedFields: []string{"reason"}},
		{Name: "moderation.warn", Title: "Player warned", Template: "Player {target.id} warned: {reason}", Icon: "shield", Category: "moderation", Color: "#f59e0b", FeaturedFields: []string{"reason"}},
		{Name: "ervisio_logs.started", Title: "Log forwarding started", Template: "Log forwarding started ({version})", Icon: "activity", Category: "system", Color: "#3b82f6", FeaturedFields: []string{"version", "environment"}},
		{Name: "moderation.ban", Title: "Player banned", Template: "Admin {actor.id} banned player {target.id}: {reason}", Icon: "shield", Category: "moderation", Color: "#ef4444", FeaturedFields: []string{"reason"}},
	}
	if err := a.Store.ApplyEventPreset(r.Context(), p.ID, presets); err != nil {
		a.internalError(w, r, err)
		return
	}
	a.audit(r, &p.WorkspaceID, "event_type.preset", "project", p.ID.String(), map[string]any{"preset": "fivem"})
	out, err := a.Store.ListEventTypes(r.Context(), p.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, out)
}
