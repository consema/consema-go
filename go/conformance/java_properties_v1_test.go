package conformance

import (
	"path/filepath"
	"testing"
)

// TestJavaPropertiesV1SuitePins the G2.3 gate: the data-driven runner
// executes all 25 published java-properties-v1 cases with zero skips and
// zero failures (docs/go-implementation-plan.md §4.2; RFC 0016 §7).
func TestJavaPropertiesV1SuitePins(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	runner := &Runner{
		VectorsDir:   filepath.Join(repoRoot, "conformance", "vectors"),
		FixturesDir:  filepath.Join(repoRoot, "conformance", "fixtures"),
		ManifestPath: filepath.Join(repoRoot, "docs", "fc-manifest-0.13.0.json"),
	}
	data, loadError := runner.loadSuite(suiteDefinition{
		File: "java-properties-v1.json", SuiteID: "consema.java-properties.conformance@1",
		ExpectedCases: 25,
	})
	if loadError != "" {
		t.Fatal(loadError)
	}
	if data.Suite != "consema.java-properties.conformance@1" {
		t.Fatalf("suite %s", data.Suite)
	}
	if len(data.Cases) != 25 {
		t.Fatalf("case count %d != 25", len(data.Cases))
	}
	report := runJavaPropertiesV1(runner, data)
	if len(report.Passed) != 25 {
		for _, failure := range report.Failed {
			t.Errorf("case %s: %s", failure.ID, failure.Message)
		}
		t.Fatalf("java-properties-v1: %d passed, %d skipped, %d failed; want 25 passed",
			len(report.Passed), len(report.Skipped), len(report.Failed))
	}
}
