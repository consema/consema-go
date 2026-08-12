package conformance

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Shared dual-runner conformance integration hook (milestone 0.19.0 G5.1;
// docs/go-implementation-plan.md §2.6, §4.1, §4.5; roadmap §16.6 line
// 1547).
//
// The orchestrator scripts/go-verify-shared-conformance.ps1 provisions the
// Rust side's shared report (crates/consema-conformance/examples/
// emit_conformance_reports.rs -> <dir>/shared-conformance.json) and runs
// this test with CONSEMA_SHARED_CONFORMANCE_RUST_DIR set to that directory.
// The test executes the Go runner over the same 18 vector suites, converts
// its report to the shared contract, compares the two sides case by case
// with CompareSharedReports, prints the per-suite two-side table and the
// summary line, and writes the Go side's shared report file
// (CONSEMA_SHARED_CONFORMANCE_GO_REPORT) for the orchestrator's own table.
// Without the variable the test skips (documented skip, never silent; the
// orchestrator rejects a skip). CONSEMA_SHARED_CONFORMANCE_STRICT=1 makes
// documented-skip asymmetries blocking ("skip 必须两侧同 skip").
// ---------------------------------------------------------------------------

const (
	sharedConformanceRustDirEnv  = "CONSEMA_SHARED_CONFORMANCE_RUST_DIR"
	sharedConformanceGoReportEnv = "CONSEMA_SHARED_CONFORMANCE_GO_REPORT"
	sharedConformanceStrictEnv   = "CONSEMA_SHARED_CONFORMANCE_STRICT"
	sharedConformanceReportFile  = "shared-conformance.json"
)

// TestSharedConformanceDualRunner executes the Go runner and compares it
// case by case with the Rust runner's shared report.
func TestSharedConformanceDualRunner(t *testing.T) {
	rustDir := os.Getenv(sharedConformanceRustDirEnv)
	if rustDir == "" {
		t.Skipf("%s is not set: run scripts/go-verify-shared-conformance.ps1 to provision the Rust side report", sharedConformanceRustDirEnv)
	}
	rustPath := filepath.Join(rustDir, sharedConformanceReportFile)
	rustBytes, err := os.ReadFile(rustPath)
	if err != nil {
		t.Fatalf("cannot read the Rust shared report %q: %v (run scripts/go-verify-shared-conformance.ps1)", rustPath, err)
	}
	rustShared, err := UnmarshalSharedReport(rustBytes)
	if err != nil {
		t.Fatalf("Rust shared report: %v", err)
	}
	if rustShared.Runner != "rust" {
		t.Fatalf("Rust shared report runner = %q, want rust", rustShared.Runner)
	}

	goReport, err := repositoryRunner(t).Run()
	if err != nil {
		t.Fatal(err)
	}
	if !goReport.Digest.OK {
		t.Fatalf("aggregate digest mismatch: computed %s, recorded %s (%d suites, %d cases)",
			goReport.Digest.Computed, goReport.Digest.Recorded, goReport.Digest.Suites, goReport.Digest.Cases)
	}
	goShared := ToSharedRunReport(goReport)
	if goShared.Runner != "go" {
		t.Fatalf("Go shared report runner = %q, want go", goShared.Runner)
	}

	if out := os.Getenv(sharedConformanceGoReportEnv); out != "" {
		bytes, err := MarshalSharedReport(goShared)
		if err != nil {
			t.Fatalf("cannot marshal the Go shared report: %v", err)
		}
		if err := os.WriteFile(out, bytes, 0o644); err != nil {
			t.Fatalf("cannot write the Go shared report %q: %v", out, err)
		}
	}

	strict := os.Getenv(sharedConformanceStrictEnv) == "1"
	comparison := CompareSharedReports(goShared, rustShared, strict)

	for _, row := range comparison.Rows {
		fmt.Printf("shared conformance suite %s: go %d passed %d skipped %d failed | rust %d passed %d skipped %d failed | agree %d/%d\n",
			row.File, row.GoPassed, row.GoSkipped, row.GoFailed,
			row.RustPassed, row.RustSkipped, row.RustFailed, row.Agree, row.Total)
	}
	for _, disagreement := range comparison.DocumentedSkips {
		fmt.Printf("shared conformance documented-skip %s %s: go=%s rust=%s [%s] %s\n",
			disagreement.File, disagreement.Case,
			disagreement.GoVerdict, disagreement.RustVerdict,
			disagreement.SkipCapability, disagreement.SkipReason)
	}
	for _, disagreement := range comparison.HardMismatches {
		message := disagreement.Detail
		if message == "" {
			message = fmt.Sprintf("go=%s rust=%s", disagreement.GoVerdict, disagreement.RustVerdict)
		}
		if disagreement.SkipCapability != "" {
			message += fmt.Sprintf(" (skip [%s] %s)", disagreement.SkipCapability, disagreement.SkipReason)
		}
		t.Errorf("shared conformance mismatch %s %s: %s", disagreement.File, disagreement.Case, message)
	}
	fmt.Printf("shared conformance: %d/%d cases agree, %d hard mismatches, %d documented-skip asymmetries (%d suites)\n",
		comparison.AgreeCases, comparison.TotalCases,
		len(comparison.HardMismatches), len(comparison.DocumentedSkips), len(comparison.Rows))

	if !goReport.Conformant() {
		t.Errorf("Go runner is not conformant: %d passed, %d skipped, %d failed",
			goReport.Passed, goReport.Skipped, goReport.Failed)
	}
	for _, suite := range rustShared.Suites {
		if len(suite.Failed) != 0 {
			t.Errorf("Rust suite %s is not conformant: %d failed", suite.File, len(suite.Failed))
		}
	}
	if len(goShared.Suites) != 18 || len(rustShared.Suites) != 18 {
		t.Errorf("suite inventories: go %d, rust %d; want 18 both sides",
			len(goShared.Suites), len(rustShared.Suites))
	}
	if goReport.Total != 508 {
		t.Errorf("Go case inventory %d != 508", goReport.Total)
	}
	rustTotal := 0
	for _, suite := range rustShared.Suites {
		rustTotal += len(suite.Passed) + len(suite.Skipped) + len(suite.Failed)
	}
	if rustTotal != 508 {
		t.Errorf("Rust case inventory %d != 508", rustTotal)
	}
}
