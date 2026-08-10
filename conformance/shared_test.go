package conformance

import (
	"strings"
	"testing"
)

// Unit tests for the shared dual-runner report hooks (milestone 0.19.0
// G5.1): the RunReport conversion (ToSharedRunReport), the JSON contract
// (Marshal/UnmarshalSharedReport), and the case-id-level comparison core
// (CompareSharedReports). The comparison semantics are pinned here: verdict
// equality per case, skip-skip equality (same case, same capability claim,
// both documented), documented-skip asymmetry handling, and inventory
// divergence detection.

// testSharedReport builds one shared report document with the given suites.
func testSharedReport(runner string, suites ...*SharedSuiteReport) *SharedRunReport {
	return &SharedRunReport{Schema: sharedReportSchema, Runner: runner, Suites: suites}
}

// testSuite builds one shared suite report.
func testSuite(file, suite string, passed []string,
	skipped []SharedSkippedCase, failed []SharedFailedCase) *SharedSuiteReport {
	return &SharedSuiteReport{
		File: file, Suite: suite, Passed: passed, Skipped: skipped, Failed: failed,
	}
}

// testComparison runs one comparison and returns it.
func testComparison(t *testing.T, goSide, rustSide *SharedRunReport, strict bool) *SharedComparison {
	t.Helper()
	return CompareSharedReports(goSide, rustSide, strict)
}

func TestToSharedRunReport(t *testing.T) {
	report := &RunReport{
		Digest: DigestResult{
			OK: true, Computed: "abc", Recorded: "abc", Suites: 1, Cases: 1,
		},
		Suites: []*SuiteReport{
			{
				Suite:         "consema.conformance@1",
				ExpectedCases: 1,
				Passed:        []string{"value.integer-arbitrary-precision"},
				Skipped: []SkipRecord{{
					ID:         "operations.v1.registry-v3",
					Capability: "core.registry-manifest@1",
					Reason:     "not part of the operations capability face",
				}},
				Failed: []CaseFailure{{ID: "suite.parse", Message: "boom"}},
			},
		},
		Total: 3, Passed: 1, Skipped: 1, Failed: 1,
	}
	shared := ToSharedRunReport(report)
	if shared.Schema != sharedReportSchema {
		t.Errorf("schema = %q, want %q", shared.Schema, sharedReportSchema)
	}
	if shared.Runner != "go" {
		t.Errorf("runner = %q, want go", shared.Runner)
	}
	if len(shared.Suites) != 1 {
		t.Fatalf("suites = %d, want 1", len(shared.Suites))
	}
	suite := shared.Suites[0]
	if suite.File != "v1.json" {
		t.Errorf("file = %q, want v1.json (frozen inventory mapping)", suite.File)
	}
	if len(suite.Skipped) != 1 || suite.Skipped[0].Capability != "core.registry-manifest@1" {
		t.Errorf("skip documentation lost: %+v", suite.Skipped)
	}
	if len(suite.Failed) != 1 || suite.Failed[0].Message != "boom" {
		t.Errorf("failure message lost: %+v", suite.Failed)
	}
	if shared.Digest.Cases != 1 || !shared.Digest.OK {
		t.Errorf("digest facts lost: %+v", shared.Digest)
	}
}

func TestSharedReportJSONRoundTrip(t *testing.T) {
	goSide := testSharedReport("go",
		testSuite("v1.json", "consema.conformance@1", []string{"a"}, nil, nil))
	bytes, err := MarshalSharedReport(goSide)
	if err != nil {
		t.Fatal(err)
	}
	text := string(bytes)
	if !strings.Contains(text, `"schema":"consema.shared-conformance@1"`) {
		t.Errorf("schema missing from JSON: %s", text)
	}
	parsed, err := UnmarshalSharedReport(bytes)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Runner != "go" || parsed.Suites[0].File != "v1.json" || parsed.Suites[0].Suite != "consema.conformance@1" {
		t.Errorf("round trip changed the report: %+v", parsed)
	}
	if parsed.Suites[0].Skipped == nil || parsed.Suites[0].Failed == nil {
		t.Errorf("empty lists must normalize to arrays, not nil")
	}

	if _, err := UnmarshalSharedReport([]byte(`{"schema":"other","runner":"go","suites":[]}`)); err == nil {
		t.Error("wrong schema must be rejected")
	}
	if _, err := UnmarshalSharedReport([]byte(`{"schema":"consema.shared-conformance@1","runner":"java","suites":[]}`)); err == nil {
		t.Error("unknown runner must be rejected")
	}
	if _, err := UnmarshalSharedReport([]byte(`not json`)); err == nil {
		t.Error("invalid JSON must be rejected")
	}
}

func TestCompareSharedReportsAgree(t *testing.T) {
	goSide := testSharedReport("go",
		testSuite("v1.json", "consema.conformance@1", []string{"a", "b"}, nil, nil),
		testSuite("toml-v1.json", "consema.toml.conformance@1", []string{"c"}, nil, nil))
	rustSide := testSharedReport("rust",
		testSuite("v1.json", "consema.conformance@1", []string{"a", "b"}, nil, nil),
		testSuite("toml-v1.json", "consema.toml.conformance@1", []string{"c"}, nil, nil))
	comparison := testComparison(t, goSide, rustSide, false)
	if comparison.TotalCases != 3 || comparison.AgreeCases != 3 {
		t.Errorf("agree = %d/%d, want 3/3", comparison.AgreeCases, comparison.TotalCases)
	}
	if len(comparison.HardMismatches) != 0 || len(comparison.DocumentedSkips) != 0 {
		t.Errorf("unexpected disagreements: %+v %+v", comparison.HardMismatches, comparison.DocumentedSkips)
	}
	if len(comparison.Rows) != 2 {
		t.Errorf("rows = %d, want 2", len(comparison.Rows))
	}
}

func TestCompareSharedReportsVerdictMismatches(t *testing.T) {
	cases := []struct {
		name       string
		goPassed   []string
		goFailed   []string
		rustPassed []string
		rustFailed []string
		wantHard   int
	}{
		{name: "go passes, rust fails", goPassed: []string{"a"}, rustFailed: []string{"a"}, wantHard: 1},
		{name: "go fails, rust passes", goFailed: []string{"a"}, rustPassed: []string{"a"}, wantHard: 1},
		{name: "go fails, rust fails (verdict agreement)", goFailed: []string{"a"}, rustFailed: []string{"a"}, wantHard: 0},
		{name: "both pass", goPassed: []string{"a"}, rustPassed: []string{"a"}, wantHard: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			goSide := testSharedReport("go",
				testSuite("v1.json", "consema.conformance@1", tc.goPassed, nil,
					testFailed(tc.goFailed...)))
			rustSide := testSharedReport("rust",
				testSuite("v1.json", "consema.conformance@1", tc.rustPassed, nil,
					testFailed(tc.rustFailed...)))
			comparison := testComparison(t, goSide, rustSide, false)
			if len(comparison.HardMismatches) != tc.wantHard {
				t.Errorf("hard mismatches = %d, want %d (%+v)",
					len(comparison.HardMismatches), tc.wantHard, comparison.HardMismatches)
			}
		})
	}
}

func TestCompareSharedReportsDocumentedSkipAsymmetry(t *testing.T) {
	skip := SharedSkippedCase{ID: "operations.v1.registry-v3",
		Capability: "core.registry-manifest@1", Reason: "not part of the operations capability face"}

	// A documented Go skip with a Rust pass is a non-blocking asymmetry...
	goSide := testSharedReport("go",
		testSuite("operations-v1.json", "consema.operations.conformance@1",
			[]string{"a"}, []SharedSkippedCase{skip}, nil))
	rustSide := testSharedReport("rust",
		testSuite("operations-v1.json", "consema.operations.conformance@1",
			[]string{"a", "operations.v1.registry-v3"}, nil, nil))
	comparison := testComparison(t, goSide, rustSide, false)
	if len(comparison.DocumentedSkips) != 1 {
		t.Fatalf("documented-skips = %d, want 1", len(comparison.DocumentedSkips))
	}
	if len(comparison.HardMismatches) != 0 {
		t.Errorf("hard mismatches = %d, want 0", len(comparison.HardMismatches))
	}
	if comparison.DocumentedSkips[0].Case != "operations.v1.registry-v3" ||
		comparison.DocumentedSkips[0].SkipCapability != "core.registry-manifest@1" ||
		comparison.DocumentedSkips[0].GoVerdict != VerdictSkipped ||
		comparison.DocumentedSkips[0].RustVerdict != VerdictPassed {
		t.Errorf("asymmetry facts wrong: %+v", comparison.DocumentedSkips[0])
	}

	// ...and blocking under strict skip semantics.
	strict := testComparison(t, goSide, rustSide, true)
	if len(strict.HardMismatches) != 1 || len(strict.DocumentedSkips) != 0 {
		t.Errorf("strict mode: hard = %d, documented = %d; want 1/0",
			len(strict.HardMismatches), len(strict.DocumentedSkips))
	}

	// A documented Rust skip with a Go pass is the mirrored asymmetry.
	rustSkipSide := testSharedReport("rust",
		testSuite("v1.json", "consema.conformance@1",
			[]string{"a"}, []SharedSkippedCase{{ID: "b", Capability: "cap", Reason: "why"}}, nil))
	goPassSide := testSharedReport("go",
		testSuite("v1.json", "consema.conformance@1", []string{"a", "b"}, nil, nil))
	mirrored := testComparison(t, goPassSide, rustSkipSide, false)
	if len(mirrored.DocumentedSkips) != 1 || mirrored.DocumentedSkips[0].RustVerdict != VerdictSkipped {
		t.Errorf("mirrored asymmetry not detected: %+v", mirrored.DocumentedSkips)
	}

	// An undocumented skip is a hard mismatch, never tolerated.
	undocumented := testSharedReport("go",
		testSuite("v1.json", "consema.conformance@1",
			[]string{"a"}, []SharedSkippedCase{{ID: "b"}}, nil))
	hard := testComparison(t, undocumented, goPassSide, false)
	if len(hard.HardMismatches) != 1 || hard.HardMismatches[0].Case != "b" {
		t.Errorf("undocumented skip not a hard mismatch: %+v", hard.HardMismatches)
	}

	// A skip against a fail is a hard mismatch.
	skipVsFail := testComparison(t, undocumented,
		testSharedReport("rust",
			testSuite("v1.json", "consema.conformance@1",
				[]string{"a"}, nil, testFailed("b"))), false)
	if len(skipVsFail.HardMismatches) != 1 || skipVsFail.HardMismatches[0].Case != "b" {
		t.Errorf("skip vs fail not a hard mismatch: %+v", skipVsFail.HardMismatches)
	}
}

func TestCompareSharedReportsSkipEquality(t *testing.T) {
	documented := []SharedSkippedCase{{ID: "x", Capability: "cap", Reason: "why"}}

	// Both sides skip the same case with the same documentation: agree.
	goSide := testSharedReport("go",
		testSuite("v1.json", "consema.conformance@1", nil, documented, nil))
	rustSide := testSharedReport("rust",
		testSuite("v1.json", "consema.conformance@1", nil, documented, nil))
	comparison := testComparison(t, goSide, rustSide, false)
	if comparison.AgreeCases != 1 || len(comparison.HardMismatches) != 0 {
		t.Errorf("same skip both sides: agree = %d, hard = %d; want 1/0",
			comparison.AgreeCases, len(comparison.HardMismatches))
	}

	// Same case, different capability claims: hard mismatch.
	rustOtherCap := testSharedReport("rust",
		testSuite("v1.json", "consema.conformance@1", nil,
			[]SharedSkippedCase{{ID: "x", Capability: "other", Reason: "why"}}, nil))
	different := testComparison(t, goSide, rustOtherCap, false)
	if len(different.HardMismatches) != 1 {
		t.Errorf("different skip capabilities not a hard mismatch: %+v", different.HardMismatches)
	}

	// Same case, one side's skip lacks the reason: hard mismatch.
	rustPartial := testSharedReport("rust",
		testSuite("v1.json", "consema.conformance@1", nil,
			[]SharedSkippedCase{{ID: "x", Capability: "cap"}}, nil))
	partial := testComparison(t, goSide, rustPartial, false)
	if len(partial.HardMismatches) != 1 {
		t.Errorf("partial skip documentation not a hard mismatch: %+v", partial.HardMismatches)
	}
}

func TestCompareSharedReportsInventory(t *testing.T) {
	// A case present on one side only is a hard mismatch.
	goSide := testSharedReport("go",
		testSuite("v1.json", "consema.conformance@1", []string{"a", "b"}, nil, nil))
	rustSide := testSharedReport("rust",
		testSuite("v1.json", "consema.conformance@1", []string{"a"}, nil, nil))
	comparison := testComparison(t, goSide, rustSide, false)
	if len(comparison.HardMismatches) != 1 || comparison.HardMismatches[0].Case != "b" {
		t.Errorf("case on one side only not detected: %+v", comparison.HardMismatches)
	}

	// A suite missing on one side is a hard mismatch (both directions: go
	// only has v1.json, rust only has toml-v1.json).
	rustMissingSuite := testSharedReport("rust",
		testSuite("toml-v1.json", "consema.toml.conformance@1", []string{"c"}, nil, nil))
	missing := testComparison(t, goSide, rustMissingSuite, false)
	if len(missing.HardMismatches) != 2 || missing.HardMismatches[0].Case != "" {
		t.Errorf("suite on one side only not detected: %+v", missing.HardMismatches)
	}

	// The per-file suite identifier must agree.
	rustWrongSuite := testSharedReport("rust",
		testSuite("v1.json", "consema.other@1", []string{"a", "b"}, nil, nil))
	wrongSuite := testComparison(t, goSide, rustWrongSuite, false)
	if len(wrongSuite.HardMismatches) != 1 || wrongSuite.HardMismatches[0].Detail == "" {
		t.Errorf("suite identifier mismatch not detected: %+v", wrongSuite.HardMismatches)
	}
}

// testFailed converts case ids into failed-case records.
func testFailed(ids ...string) []SharedFailedCase {
	failed := make([]SharedFailedCase, 0, len(ids))
	for _, id := range ids {
		failed = append(failed, SharedFailedCase{ID: id, Message: "expected behavior did not match"})
	}
	return failed
}
