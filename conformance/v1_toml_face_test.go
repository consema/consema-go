package conformance

// The G1.3 self-test for the TOML face handlers. The shared runner files
// (syntax_query_v1.go, operations_v1.go) are merged by the milestone
// coordinator; until then these tests drive the exported handlers directly
// over the published vector cases, mirroring the Rust suite execution.

import (
	"testing"
)

// loadSuiteFile loads one vector file through the runner loader.
func loadSuiteFile(t *testing.T, runner *Runner, name string) *suiteData {
	t.Helper()
	data, message := runner.loadSuite(suiteDefinition{File: name})
	if message != "" {
		t.Fatalf("load %s: %s", name, message)
	}
	return data
}

// TestSyntaxQueryTomlFaceExecutesAllTomlCases runs the eight
// `syntax.toml.*` cases through the exported handler.
func TestSyntaxQueryTomlFaceExecutesAllTomlCases(t *testing.T) {
	runner := repositoryRunner(t)
	data := loadSuiteFile(t, runner, "syntax-query-v1.json")
	report := &SuiteReport{}
	executed := 0
	for index := range data.Cases {
		vector := &data.Cases[index]
		if len(vector.ID) < len("syntax.toml.") || vector.ID[:len("syntax.toml.")] != "syntax.toml." {
			continue
		}
		RunSyntaxQueryTomlFace(vector, report)
		executed++
	}
	if executed != 8 {
		t.Fatalf("executed %d toml syntax-query cases, want 8", executed)
	}
	if len(report.Failed) != 0 {
		t.Fatalf("toml syntax-query face failed: %+v", report.Failed)
	}
	if len(report.Passed) != 8 {
		t.Fatalf("toml syntax-query face passed %d, want 8", len(report.Passed))
	}
}

// TestOperationsTomlFaceExecutesAllTomlCases runs all thirteen TOML
// operation cases through the exported handler (the operation-registry
// case compares the JSON and TOML registries).
func TestOperationsTomlFaceExecutesAllTomlCases(t *testing.T) {
	runner := repositoryRunner(t)
	data := loadSuiteFile(t, runner, "operations-v1.json")
	report := &SuiteReport{}
	executed := 0
	for index := range data.Cases {
		vector := &data.Cases[index]
		switch {
		case vector.ID == "operations.v1.operation-registry",
			vector.ID == "operations.v1.materialize-toml-native",
			vector.ID == "operations.v1.materialize-toml-explicit-mapping",
			vector.ID == "operations.v1.materialize-toml-implicit-mapping-rejected",
			vector.ID == "operations.v1.materialize-toml-null-rejected",
			vector.ID == "operations.v1.materialize-toml-output-limit",
			vector.ID == "operations.v1.toml-root-insert",
			vector.ID == "operations.v1.toml-inline-rename",
			vector.ID == "operations.v1.toml-array-remove",
			vector.ID == "operations.v1.toml-conflict-atomic",
			vector.ID == "operations.v1.toml-dry-run-proof-patch",
			vector.ID == "operations.v1.toml-structural-matrix",
			vector.ID == "operations.v1.toml-conflict-matrix":
			RunOperationsTOMLFace(vector, report)
			executed++
		}
	}
	if executed != 13 {
		t.Fatalf("executed %d toml operation cases, want 13", executed)
	}
	if len(report.Failed) != 0 {
		t.Fatalf("toml operations face failed: %+v", report.Failed)
	}
	if len(report.Passed) != 13 {
		t.Fatalf("toml operations face passed %d, want 13", len(report.Passed))
	}
}

// TestV1JSONHasNoTomlFace verifies that v1.json carries no TOML-facing
// cases (the v1.go face hook is a no-op by construction).
func TestV1JSONHasNoTomlFace(t *testing.T) {
	runner := repositoryRunner(t)
	data := loadSuiteFile(t, runner, "v1.json")
	for index := range data.Cases {
		vector := &data.Cases[index]
		if len(vector.ID) >= len("toml.") && vector.ID[:len("toml.")] == "toml." {
			t.Fatalf("unexpected TOML case %s in v1.json", vector.ID)
		}
	}
}

// TestTomlSuiteRunner executes the full toml-v1 suite through the runner.
func TestTomlSuiteRunner(t *testing.T) {
	runner := repositoryRunner(t)
	report := runner.runSuite(suiteDefinition{
		File: "toml-v1.json", SuiteID: "consema.toml.conformance@1", ExpectedCases: 18,
		Run: runTomlV1,
	})
	if !report.Conformant() {
		t.Fatalf("toml suite is not conformant: %+v", report.Failed)
	}
	if len(report.Passed) != 18 || len(report.Skipped) != 0 {
		t.Fatalf("toml suite passed %d, skipped %d; want 18/0", len(report.Passed), len(report.Skipped))
	}
}
