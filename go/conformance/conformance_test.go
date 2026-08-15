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
// 2026-08-15): the aggregate digest and the total suite/case counts come
// from the manifest — the single re-vendor sync point for the digest and
// the totals — instead of hardcoded literals that go unnoticed when the
// inventory legitimately changes. Wave-5 P2 note: the per-suite counts
// remain frozen in-runner pins (conformance/README.md rule 4; the
// manifest carries only the aggregate totals, not per-suite counts).
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
// current milestone: all 18 suites execute their full frozen case count
// with zero documented skips and zero failures (0.14.0-0.19.0 milestones
// G0.1-G5.7 delivered; the 0.15.0-era per-face flip notes of the old
// header are closed — G054, adversarial audit 2026-08-13;
// https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
// §4.2). Wave-5 P2: the expected triples are derived from the in-runner
// definition table (allSuites — the frozen rule-4 per-suite pins) instead
// of a duplicated literal map, so a legitimate vector re-vendor (case
// added, manifest updated, ExpectedCases bumped) does not redden this
// test — the duplicated literal list was a second re-vendor sync point.
func TestApplicableSuiteCounts(t *testing.T) {
	report, err := repositoryRunner(t).Run()
	if err != nil {
		t.Fatal(err)
	}
	expected := make(map[string][3]int, len(allSuites))
	for _, definition := range allSuites {
		expected[definition.SuiteID] = [3]int{definition.ExpectedCases, 0, 0}
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
