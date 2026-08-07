package conformance

import (
	"path/filepath"
	"testing"
)

// repositoryOracle derives the plist-macos-v1 oracle paths from the
// repo-relative layout (the package directory is go/conformance, so the
// repository root is two levels up; docs/go-implementation-plan.md §4.3).
func repositoryOracle(t *testing.T) (manifestPath, repoRoot string) {
	t.Helper()
	repoRoot = filepath.Join("..", "..")
	return filepath.Join(repoRoot, "conformance", "oracles", "plist-macos-v1", "manifest.json"),
		repoRoot
}

// TestPlistMacOSFoundationDifferential pins the 0.17.0 G3.3 gate
// (docs/go-implementation-plan.md §2.4, §6): the Go plist implementation
// reproduces the frozen Foundation facts of the plist-macos-v1 manifest —
// accept/reject, format detection, convert outcomes, and native value
// consistency. The four cases without a divergence annotation must be fully
// asserted; the three exclusion cases (D-2, D-20, D-21) appear as
// documented skips carrying the inventory id and reason, never silently.
func TestPlistMacOSFoundationDifferential(t *testing.T) {
	manifestPath, repoRoot := repositoryOracle(t)
	report, err := RunPlistMacOSOracle(manifestPath, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if report.Suite != "consema.plist.macos-differential@1" {
		t.Errorf("suite %s != consema.plist.macos-differential@1", report.Suite)
	}
	if !report.CountAsserted() {
		t.Errorf("case count assertion failed: %d cases, want %d",
			len(report.Cases), report.ExpectedCases)
	}
	for _, failure := range report.Failed {
		t.Errorf("failure: %s: %s", failure.ID, failure.Message)
	}
	// Every case carries the full five-leg comparison table.
	for _, caseReport := range report.Cases {
		if len(caseReport.Legs) != 5 {
			t.Errorf("case %s has %d legs, want 5", caseReport.ID, len(caseReport.Legs))
		}
		for _, leg := range caseReport.Legs {
			if leg.Foundation == "" {
				t.Errorf("case %s leg %s lacks the recorded Foundation fact",
					caseReport.ID, leg.Leg)
			}
			if leg.Status == "failed" {
				t.Errorf("case %s leg %s failed: %s", caseReport.ID, leg.Leg, leg.Observed)
			}
		}
	}
	// Divergence discipline: non-exclusion cases carry no skip; exclusion
	// cases carry exactly one documented skip with the inventory id and a
	// reason.
	seenExclusions := map[string]string{}
	for _, skip := range report.Skipped {
		if skip.ExclusionID == "" || skip.Reason == "" {
			t.Errorf("case %s skip lacks exclusion id or reason", skip.CaseID)
		}
		seenExclusions[skip.ExclusionID] = skip.CaseID
	}
	for _, caseReport := range report.Cases {
		skips := 0
		for _, skip := range report.Skipped {
			if skip.CaseID == caseReport.ID {
				skips++
			}
		}
		if caseReport.Divergence == "" && skips != 0 {
			t.Errorf("case %s has %d skip records but no divergence annotation", caseReport.ID, skips)
		}
		if caseReport.Divergence != "" {
			if skips != 1 {
				t.Errorf("case %s has %d skip records, want 1", caseReport.ID, skips)
			}
			for _, skip := range report.Skipped {
				if skip.CaseID == caseReport.ID && skip.ExclusionID != caseReport.Divergence {
					t.Errorf("case %s skip id %s != annotation %s",
						caseReport.ID, skip.ExclusionID, caseReport.Divergence)
				}
			}
		}
	}
	// Frozen per-case shape: 7 cases, 35 legs, 34 passed assertions, 1
	// skipped leg (the D-20 to_xml1 leg), 3 documented divergence skips.
	if len(report.Cases) != 7 {
		t.Errorf("case count %d != 7", len(report.Cases))
	}
	if report.Passed != 34 || report.SkippedLegs != 1 {
		t.Errorf("legs: %d passed, %d skipped; want 34 passed, 1 skipped",
			report.Passed, report.SkippedLegs)
	}
	if len(report.Skipped) != 3 {
		t.Errorf("documented divergence skips %d != 3", len(report.Skipped))
	}
	for _, id := range []string{"D-2", "D-20", "D-21"} {
		if _, ok := seenExclusions[id]; !ok {
			t.Errorf("exclusion %s is not documented", id)
		}
	}
	if !report.Conformant() {
		t.Fatalf("plist-macos differential is not conformant: %d passed legs, "+
			"%d skipped legs, %d failures", report.Passed, report.SkippedLegs, len(report.Failed))
	}
}

// TestPlistMacOSFoundationCaseTable pins the per-case comparison table:
// case ids, profile selection, and the recorded Foundation facts each leg
// compares against. The manifest is the authority, so the table only pins
// the manifest's own recorded facts (no expectation literals).
func TestPlistMacOSFoundationCaseTable(t *testing.T) {
	manifestPath, repoRoot := repositoryOracle(t)
	report, err := RunPlistMacOSOracle(manifestPath, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Cases) != 7 {
		t.Fatalf("case count %d != 7", len(report.Cases))
	}
	// The manifest case inventory: id, divergence annotation, detected
	// format, and the recorded convert outcomes.
	cases := []struct {
		id         string
		divergence string
		format     string
		toXML1     string
		toBinary1  string
	}{
		{"xml-info-plist", "", "xml1", "ok", "ok"},
		{"xml-archiver-sample", "", "xml1", "ok", "ok"},
		{"xml-preferences", "", "xml1", "ok", "ok"},
		{"xml-repeated-keys", "D-2", "xml1", "ok", "ok"},
		{"binary-preferences", "", "binary1", "ok", "ok"},
		{"binary-archiver-sample", "D-21", "binary1", "error", "ok"},
		{"binary-shared-refs", "D-20", "binary1", "ok", "ok"},
	}
	for index, want := range cases {
		caseReport := report.Cases[index]
		if caseReport.ID != want.id {
			t.Errorf("case %d id %s != %s", index, caseReport.ID, want.id)
		}
		if caseReport.Divergence != want.divergence {
			t.Errorf("case %s divergence %q != %q", caseReport.ID,
				caseReport.Divergence, want.divergence)
		}
		for _, leg := range caseReport.Legs {
			switch leg.Leg {
			case "format":
				if leg.Foundation != "detected_format="+want.format {
					t.Errorf("case %s format fact %q", caseReport.ID, leg.Foundation)
				}
			case "convert.to_xml1":
				if leg.Foundation != "to_xml1="+want.toXML1 {
					t.Errorf("case %s to_xml1 fact %q", caseReport.ID, leg.Foundation)
				}
			case "convert.to_binary1":
				if leg.Foundation != "to_binary1="+want.toBinary1 {
					t.Errorf("case %s to_binary1 fact %q", caseReport.ID, leg.Foundation)
				}
			case "formation":
				if leg.Foundation != "lint=ok" {
					t.Errorf("case %s lint fact %q", caseReport.ID, leg.Foundation)
				}
			case "values":
				if leg.Foundation != "values=ok" {
					t.Errorf("case %s values fact %q", caseReport.ID, leg.Foundation)
				}
			}
		}
	}
	// The three divergence skips must carry the inventory's own reason text
	// (never invented by the runner): each reason names the D-id and the
	// Consema contract.
	reasons := map[string]string{}
	for _, skip := range report.Skipped {
		reasons[skip.ExclusionID] = skip.Reason
	}
	for id, reason := range reasons {
		if reason == "" || reason == id {
			t.Errorf("exclusion %s lacks the inventory reason", id)
		}
	}
}
