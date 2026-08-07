package conformance

import (
	"path/filepath"
	"testing"
)

// repositoryRunner builds the runner over the repository vectors using the
// repo-relative layout (docs/go-implementation-plan.md §4.3: go test uses
// repository-relative paths; the CLI takes explicit paths). The package
// directory is go/conformance, so the repository root is two levels up.
func repositoryRunner(t *testing.T) *Runner {
	t.Helper()
	repoRoot := filepath.Join("..", "..")
	return &Runner{
		VectorsDir:   filepath.Join(repoRoot, "conformance", "vectors"),
		FixturesDir:  filepath.Join(repoRoot, "conformance", "fixtures"),
		ManifestPath: filepath.Join(repoRoot, "docs", "fc-manifest-0.13.0.json"),
	}
}

// TestRunIsConformant pins the milestone gate: every applicable case of the
// 0.14.0 capability surface passes, the remaining cases are documented
// skips, every count assertion holds, and the aggregate digest matches the
// Feature-Complete Manifest.
func TestRunIsConformant(t *testing.T) {
	report, err := repositoryRunner(t).Run()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Digest.OK {
		t.Fatalf("aggregate digest mismatch: computed %s, recorded %s (%d suites, %d cases)",
			report.Digest.Computed, report.Digest.Recorded, report.Digest.Suites, report.Digest.Cases)
	}
	if report.Total != 508 {
		t.Fatalf("case inventory %d != 508", report.Total)
	}
	for _, suite := range report.Suites {
		if !suite.Conformant() {
			t.Errorf("suite %s is not conformant: %d failed", suite.Suite, len(suite.Failed))
		}
		for _, failure := range suite.Failed {
			t.Logf("suite %s failure: %s: %s", suite.Suite, failure.ID, failure.Message)
		}
		for _, skip := range suite.Skipped {
			if skip.Capability == "" || skip.Reason == "" {
				t.Errorf("suite %s skip %s lacks capability or reason", suite.Suite, skip.ID)
			}
		}
	}
	if !report.Conformant() {
		t.Fatalf("run is not conformant: %d passed, %d skipped, %d failed",
			report.Passed, report.Skipped, report.Failed)
	}
}

// TestApplicableSuiteCounts pins the per-suite applicable surface of the
// current milestone (0.15.0 G1.1 flips source-v1, G1.2 flips the JSON
// faces, G1.3 flips the TOML faces; docs/go-implementation-plan.md §4.2).
func TestApplicableSuiteCounts(t *testing.T) {
	report, err := repositoryRunner(t).Run()
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string][3]int{
		"consema.conformance@1":                   {30, 0, 0},
		"consema.toml.conformance@1":              {18, 0, 0},
		"consema.protocol.conformance@1":          {32, 0, 0},
		"consema.source.conformance@1":            {28, 0, 0},
		"consema.syntax-query.conformance@1":      {19, 0, 0},
		"consema.protocol.conformance@2":          {11, 0, 0},
		"consema.operations.conformance@1":        {33, 2, 0},
		"consema.json-family.conformance@2":       {33, 0, 0},
		"consema.portable-graph.conformance@1":    {10, 0, 0},
		"consema.semantic-model-v5.conformance@1": {22, 0, 0},
		"consema.yaml.conformance@1":              {27, 0, 0},
		"consema.semantic-model-v6.conformance@1": {25, 0, 0},
		"consema.ini.conformance@1":               {20, 0, 0},
		"consema.java-properties.conformance@1":   {22, 0, 0},
		"consema.xml-1-0-safe.conformance@1":      {34, 0, 0},
		"consema.plist.conformance@1":             {45, 0, 0},
		"consema.hcl.conformance@1":               {57, 0, 0},
		"consema.cli.conformance@1":               {40, 0, 0},
	}
	for _, suite := range report.Suites {
		expectedCounts, ok := expected[suite.Suite]
		if !ok {
			t.Errorf("unexpected suite %s", suite.Suite)
			continue
		}
		passed, skipped, failed := len(suite.Passed), len(suite.Skipped), len(suite.Failed)
		if passed != expectedCounts[0] || skipped != expectedCounts[1] || failed != expectedCounts[2] {
			t.Errorf("suite %s: %d passed, %d skipped, %d failed; want %v",
				suite.Suite, passed, skipped, failed, expectedCounts)
		}
	}
}

// TestDigestAlgorithmMatchesManifest pins the §4.5 aggregate algorithm
// against the Feature-Complete Manifest record.
func TestDigestAlgorithmMatchesManifest(t *testing.T) {
	digest, err := repositoryRunner(t).VerifyVectorsDigest()
	if err != nil {
		t.Fatal(err)
	}
	const recorded = "e3d6578858fa1fdcab0c19ee0094cd246923dca76e9be4679aabf86b482b68c8"
	if digest.Recorded != recorded {
		t.Fatalf("manifest record changed: %s", digest.Recorded)
	}
	if digest.Computed != recorded {
		t.Fatalf("aggregate digest %s != %s", digest.Computed, recorded)
	}
	if digest.Suites != 18 || digest.Cases != 508 {
		t.Fatalf("inventory %d suites / %d cases != 18 / 508", digest.Suites, digest.Cases)
	}
}
