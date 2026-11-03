package archive

import (
	"time"

	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

// Retention is the effective policy of one stream after inheritance.
type Retention struct {
	// LocalAge: covered hot rows older than this are evicted from ClickHouse.
	LocalAge time.Duration
	// GlobalAge: data older than this is deleted everywhere.
	GlobalAge time.Duration
}

// Budget is the instance-wide cache budget (ADR 0004).
type Budget struct {
	MaxBytes int64
	HighPct  int
	LowPct   int
}

// Policies resolves retention by precedence stream > project > workspace > instance, field by
// field: a stream override that sets only local_age_hours still inherits global_age_hours.
type Policies struct {
	byScope map[string]map[uuid.UUID]store.RetentionPolicy
}

func NewPolicies(rows []store.RetentionPolicy) Policies {
	p := Policies{byScope: map[string]map[uuid.UUID]store.RetentionPolicy{}}
	for _, r := range rows {
		if p.byScope[r.ScopeType] == nil {
			p.byScope[r.ScopeType] = map[uuid.UUID]store.RetentionPolicy{}
		}
		p.byScope[r.ScopeType][r.ScopeID] = r
	}
	return p
}

// Instance-level fallbacks for a database without the seeded row; the migration always seeds it.
const (
	defaultLocalHours  = 72
	defaultGlobalHours = 336
	defaultCacheBytes  = 20 << 30
	defaultHighPct     = 90
	defaultLowPct      = 75
)

func (p Policies) chain(ws, proj, stream uuid.UUID) []store.RetentionPolicy {
	var out []store.RetentionPolicy
	for _, s := range []struct {
		typ string
		id  uuid.UUID
	}{{"stream", stream}, {"project", proj}, {"workspace", ws}, {"instance", store.InstanceScope}} {
		if r, ok := p.byScope[s.typ][s.id]; ok {
			out = append(out, r)
		}
	}
	return out
}

// For returns the effective retention of a stream. If inheritance produces a global age shorter
// than the local age (each row is valid on its own, the mix is not), local is capped at global:
// data never outlives its global retention.
func (p Policies) For(ws, proj, stream uuid.UUID) Retention {
	local, global := int32(defaultLocalHours), int32(defaultGlobalHours)
	gotLocal, gotGlobal := false, false
	for _, r := range p.chain(ws, proj, stream) {
		if !gotLocal && r.LocalAgeHours != nil {
			local, gotLocal = *r.LocalAgeHours, true
		}
		if !gotGlobal && r.GlobalAgeHours != nil {
			global, gotGlobal = *r.GlobalAgeHours, true
		}
	}
	if local > global {
		local = global
	}
	return Retention{LocalAge: time.Duration(local) * time.Hour, GlobalAge: time.Duration(global) * time.Hour}
}

// Budget returns the instance cache budget.
func (p Policies) Budget() Budget {
	b := Budget{MaxBytes: defaultCacheBytes, HighPct: defaultHighPct, LowPct: defaultLowPct}
	if r, ok := p.byScope["instance"][store.InstanceScope]; ok {
		if r.CacheMaxBytes != nil {
			b.MaxBytes = *r.CacheMaxBytes
		}
		if r.HighPct != nil {
			b.HighPct = int(*r.HighPct)
		}
		if r.LowPct != nil {
			b.LowPct = int(*r.LowPct)
		}
	}
	return b
}

// High and Low are the watermarks in bytes.
func (b Budget) High() int64 { return b.MaxBytes / 100 * int64(b.HighPct) }
func (b Budget) Low() int64  { return b.MaxBytes / 100 * int64(b.LowPct) }
