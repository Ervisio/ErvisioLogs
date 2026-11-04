package api

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

type specOperation struct {
	OperationID string         `yaml:"operationId"`
	RequestBody map[string]any `yaml:"requestBody"`
	Responses   map[string]any `yaml:"responses"`
}

// loadOperations returns the operations of the specification by operationId.
func loadOperations(t *testing.T) map[string]specOperation {
	t.Helper()
	raw, err := os.ReadFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	ops := map[string]specOperation{}
	for _, item := range spec.Paths {
		for _, method := range []string{"get", "post", "patch", "put", "delete"} {
			node, ok := item[method]
			if !ok {
				continue
			}
			var op specOperation
			if err := node.Decode(&op); err != nil {
				t.Fatal(err)
			}
			ops[op.OperationID] = op
		}
	}
	return ops
}

// decode answers 400 and 413 for any JSON body, so every operation that has one documents them.
func TestSpecDocumentsBodyErrors(t *testing.T) {
	seen := 0
	for name, op := range loadOperations(t) {
		if op.RequestBody == nil {
			continue
		}
		seen++
		for _, code := range []string{"400", "413"} {
			if _, ok := op.Responses[code]; !ok {
				t.Errorf("%s does not document %s", name, code)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no operation with a request body found")
	}
}

func TestSpecDocumentsSpecificResponses(t *testing.T) {
	ops := loadOperations(t)
	for name, codes := range map[string][]string{
		"register":           {"503"},
		"login":              {"503"},
		"adminCreateUser":    {"503"},
		"changePassword":     {"403", "409", "429", "503"},
		"adminResetPassword": {"503"},
		"adminListUsers":     {"400"},
		"adminDeleteUser":    {"409", "422"},
		"listAudit":          {"400"},
		"adminListAudit":     {"200", "400", "401", "403"},
		"oauthCallback":      {"429"},
	} {
		op, ok := ops[name]
		if !ok {
			t.Errorf("%s is missing", name)
			continue
		}
		for _, code := range codes {
			if _, ok := op.Responses[code]; !ok {
				t.Errorf("%s does not document %s", name, code)
			}
		}
	}
}
