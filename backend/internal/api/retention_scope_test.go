package api

import (
	"testing"

	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
	"github.com/google/uuid"
)

func TestVisibleRetentionPolicies(t *testing.T) {
	workspace, project, sibling, outsider := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	cache, high, low := int64(1<<20), int16(90), int16(70)
	actor := uuid.New()
	rows := []store.RetentionPolicy{
		{ScopeType: "instance", ScopeID: uuid.Nil, CacheMaxBytes: &cache, HighPct: &high, LowPct: &low, UpdatedBy: &actor},
		{ScopeType: "workspace", ScopeID: workspace, UpdatedBy: &actor},
		{ScopeType: "project", ScopeID: project, UpdatedBy: &actor},
		{ScopeType: "project", ScopeID: sibling},
		{ScopeType: "workspace", ScopeID: outsider},
		{ScopeType: "stream", ScopeID: uuid.New()},
	}
	for _, test := range []struct {
		name    string
		project uuid.UUID
		want    int
	}{{"workspace", uuid.Nil, 2}, {"project", project, 3}} {
		t.Run(test.name, func(t *testing.T) {
			visible := visibleRetentionPolicies(rows, workspace, test.project)
			if len(visible) != test.want {
				t.Fatalf("got %d policies, want %d", len(visible), test.want)
			}
			for _, policy := range visible {
				if policy.ScopeID == outsider || policy.ScopeID == sibling {
					t.Fatalf("unrelated policy leaked: %v", policy)
				}
				if policy.CacheMaxBytes != nil || policy.HighPct != nil || policy.LowPct != nil || policy.UpdatedBy != nil {
					t.Fatalf("instance-only metadata leaked: %v", policy)
				}
			}
		})
	}
	if rows[0].CacheMaxBytes == nil || rows[0].UpdatedBy == nil {
		t.Fatal("redaction mutated source rows")
	}
	if visible := visibleRetentionPolicies(nil, workspace, project); visible == nil {
		t.Fatal("empty response must remain an array")
	}
}
