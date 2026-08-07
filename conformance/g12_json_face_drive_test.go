package conformance

// The 0.15.0 G1.2 JSON-face acceptance pin. The shared runner files
// (v1.go, syntax_query_v1.go, operations_v1.go) are rewired by the main
// agent at merge time; this test drives the exported face handlers
// directly with the vector data so the G1.2 acceptance is pinned
// independently of the shared-file wiring. The suite-level counts still
// come from the shared files and conformance_test.go.

import (
	"testing"
)

func faceVectorCase(t *testing.T, suite string, id string) *caseData {
	t.Helper()
	runner := repositoryRunner(t)
	data, loadError := runner.loadSuite(suiteDefinition{
		File: suite, SuiteID: "consema.conformance@1", ExpectedCases: 0,
	})
	if loadError != "" {
		t.Fatalf("load %s: %s", suite, loadError)
	}
	for index := range data.Cases {
		if data.Cases[index].ID == id {
			return &data.Cases[index]
		}
	}
	t.Fatalf("case %s not found in %s", id, suite)
	return nil
}

func TestG12V1JSONFace(t *testing.T) {
	report := &SuiteReport{}
	for _, id := range []string{
		"parse.strict-exact-roundtrip", "parse.jsonc-comments-trailing-comma",
		"parse.recovery-missing-close", "parse.duplicate-members",
		"parse.lossless-byte-coverage", "query.json-duplicate-order",
		"projection.best-exact-duplicate-mapping", "projection.object-reject-duplicates",
		"projection.object-last-wins", "edit.scalar-minimal",
		"edit.preserve-decimal-scale", "edit.preserve-exponent-style",
		"edit.canonical-for-profile", "edit.preserve-else-canonical",
		"edit.preserve-incompatible-rejected", "projection.object-key-provenance",
		"edit.wrong-snapshot", "resource.parse-token-limit",
	} {
		RunV1JSONFace(faceVectorCase(t, "v1.json", id), report)
	}
	if len(report.Failed) != 0 {
		t.Fatalf("v1 JSON face failures: %v", report.Failed)
	}
	if len(report.Passed) != 18 {
		t.Fatalf("v1 JSON face passed %d != 18", len(report.Passed))
	}
}

func TestG12SyntaxQueryJSONFace(t *testing.T) {
	report := &SuiteReport{}
	for _, id := range []string{
		"syntax.json.kind-text-order", "syntax.json.kind-string",
		"syntax.json.text-equals", "syntax.json.selection-first",
		"syntax.json.selection-last", "syntax.json.result-limit",
		"syntax.json.cancelled", "syntax.json.reject-invalid-kind",
	} {
		RunSyntaxQueryJSONFace(faceVectorCase(t, "syntax-query-v1.json", id), report)
	}
	if len(report.Failed) != 0 {
		t.Fatalf("syntax-query JSON face failures: %v", report.Failed)
	}
	if len(report.Passed) != 8 {
		t.Fatalf("syntax-query JSON face passed %d != 8", len(report.Passed))
	}
}

func TestG12OperationsJSONFace(t *testing.T) {
	report := &SuiteReport{}
	for _, id := range []string{
		"operations.v1.materialize-json-compact",
		"operations.v1.materialize-json-pretty-crlf",
		"operations.v1.materialize-json-entry-mapping-duplicates",
		"operations.v1.materialize-json-nonstring-key-rejected",
		"operations.v1.materialize-json-float-rejected",
		"operations.v1.materialize-json-output-limit",
		"operations.v1.materialization-depth-limit",
		"operations.v1.json-object-insert",
		"operations.v1.json-object-remove-duplicate",
		"operations.v1.json-array-remove",
		"operations.v1.json-conflict-atomic",
		"operations.v1.json-dry-run-proof-patch",
		"operations.v1.json-structural-matrix",
		"operations.v1.json-conflict-matrix",
		"operations.v1.materialization-security-matrix",
		"operations.v1.untouched-proof-tamper",
	} {
		RunOperationsJSONFace(faceVectorCase(t, "operations-v1.json", id), report)
	}
	if len(report.Failed) != 0 {
		t.Fatalf("operations JSON face failures: %v", report.Failed)
	}
	if len(report.Passed) != 16 {
		t.Fatalf("operations JSON face passed %d != 16", len(report.Passed))
	}
}

func TestG12JSONFamilyV2Suite(t *testing.T) {
	runner := repositoryRunner(t)
	data, loadError := runner.loadSuite(suiteDefinition{
		File: "json-family-v2.json", SuiteID: "consema.json-family.conformance@2",
		ExpectedCases: 33,
	})
	if loadError != "" {
		t.Fatalf("load: %s", loadError)
	}
	report := runJsonFamilyV2(runner, data)
	if len(report.Failed) != 0 {
		t.Fatalf("json-family-v2 failures: %v", report.Failed)
	}
	if len(report.Passed) != 30 || len(report.Skipped) != 3 {
		t.Fatalf("json-family-v2 passed %d skipped %d; want 30/3",
			len(report.Passed), len(report.Skipped))
	}
}
