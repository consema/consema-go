package conformance

import (
	"path/filepath"
	"testing"
)

// repositoryRunner builds the runner over the repository vectors using the
// repo-relative layout (https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §4.3: go test uses
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

// repositoryManifestCounts reads the conformance_suite record of the
// repository's provisioned Feature-Complete Manifest (wave-4 R10,
// 2026-08-15): the suite/case counts are derived from the manifest — the
// single re-vendor sync point — instead of hardcoded literals that go
// unnoticed when the inventory legitimately changes.
func repositoryManifestCounts(t *testing.T) (suites, cases int) {
	t.Helper()
	_, suites, cases, err := manifestConformanceSuite(repositoryRunner(t).ManifestPath)
	if err != nil {
		t.Fatalf("cannot read the Feature-Complete Manifest: %v", err)
	}
	return suites, cases
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
	if _, cases := repositoryManifestCounts(t); report.Total != cases {
		t.Fatalf("case inventory %d != manifest conformance_suite cases %d", report.Total, cases)
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
// current milestone: all 18 suites execute with zero documented skips
// (0.14.0-0.19.0 milestones G0.1-G5.6 delivered; the 0.15.0-era per-face
// flip notes of the old header are closed — G054, adversarial audit
// 2026-08-13; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
// §4.2).
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
		"consema.operations.conformance@1":        {35, 0, 0},
		"consema.json-family.conformance@2":       {33, 0, 0},
		"consema.portable-graph.conformance@1":    {10, 0, 0},
		"consema.semantic-model-v5.conformance@1": {22, 0, 0},
		"consema.yaml.conformance@1":              {31, 0, 0},
		"consema.semantic-model-v6.conformance@1": {25, 0, 0},
		"consema.ini.conformance@1":               {20, 0, 0},
		"consema.java-properties.conformance@1":   {25, 0, 0},
		"consema.xml-1-0-safe.conformance@1":      {34, 0, 0},
		"consema.plist.conformance@1":             {49, 0, 0},
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
// against the Feature-Complete Manifest record: the computed aggregate
// over the repository vectors must equal the manifest's recorded value,
// and the computed suite/case counts must equal the manifest's
// conformance_suite record (wave-4 R10, 2026-08-15: the recorded value
// and the counts come from the manifest — the hardcoded digest literal
// was a second re-vendor sync point that reddened on every legitimate
// inventory change).
func TestDigestAlgorithmMatchesManifest(t *testing.T) {
	digest, err := repositoryRunner(t).VerifyVectorsDigest()
	if err != nil {
		t.Fatal(err)
	}
	if !digest.OK {
		t.Fatalf("aggregate digest mismatch: computed %s, recorded %s", digest.Computed, digest.Recorded)
	}
	suites, cases := repositoryManifestCounts(t)
	if digest.Suites != suites || digest.Cases != cases {
		t.Fatalf("inventory %d suites / %d cases != manifest %d / %d",
			digest.Suites, digest.Cases, suites, cases)
	}
}
