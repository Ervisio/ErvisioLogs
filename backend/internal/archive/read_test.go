package archive

import (
	"strings"
	"testing"
)

func TestRemoteSelectTagsPreservesOldSegments(t *testing.T) {
	old := (ReadSource{SchemaVersion: 1, FileExpr: "legacy"}).RemoteSelect()
	current := (ReadSource{SchemaVersion: 2, FileExpr: "current"}).RemoteSelect()
	if !strings.Contains(old, "CAST([], 'Array(String)') AS tags") {
		t.Fatalf("legacy lacks default: %s", old)
	}
	if !strings.Contains(current, "tags AS tags") {
		t.Fatalf("new tags missing: %s", current)
	}
}
