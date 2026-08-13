// Orchestration report hooks for the shared dual-runner conformance
// orchestration (milestone 0.19.0 G5.1; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §2.6,
// §4.1, §4.5; roadmap §16.6 line 1547).
//
// The Go runner and the Rust runner (consema-rs/consema-conformance, driven by
// the auxiliary example emit_conformance_reports.rs) each execute the same
// 18 vector suites. This file defines the shared machine-readable report
// contract ("consema.shared-conformance@1") both sides emit, the Go-side
// conversion from RunReport, and the case-id-level comparison core
// (CompareSharedReports). The orchestrator
// (scripts/go-verify-shared-conformance.ps1) provisions the Rust side's
// report file, runs the integration hook test (shared_run_test.go), and
// prints the per-suite two-side table from the two report files.
//
// Comparison semantics (§4.1; RFC 0016 §7): for each case id the two sides
// must reach the same verdict — passed/passed, skipped/skipped (same case
// id, same capability claim, both documented), or failed/failed. A case
// skipped on exactly one side with the required documentation (capability
// and reason) while the other side passes it is reported as a documented-
// skip asymmetry — never silent, and blocking when strictSkips is set. A
// case skipped without that documentation is a hard mismatch (the never-
// silent skip discipline of §4.3). A suite or case present on exactly one
// side, and a per-file suite identifier disagreement, are hard mismatches.
package conformance

import (
	"encoding/json"
	"fmt"
	"sort"
)

// SharedSkippedCase is one documented skip in the shared report contract
// (the Go runner's SkipRecord; RFC 0016 §7: capability and reason, never
// silent).
type SharedSkippedCase struct {
	// ID is the stable case identifier.
	ID string `json:"id"`
	// Capability is the declared mandatory capability.
	Capability string `json:"capability"`
	// Reason explains why the capability is not implemented.
	Reason string `json:"reason"`
}

// SharedFailedCase is one failed case in the shared report contract.
type SharedFailedCase struct {
	// ID is the stable case identifier.
	ID string `json:"id"`
	// Message is the failure description.
	Message string `json:"message"`
}

// SharedSuiteReport is one vector suite's case-level verdicts in the shared
// report contract. The Rust side emits the identical shape; its runner has
// no skip concept, so its skipped list is always empty.
type SharedSuiteReport struct {
	// File is the vector file basename (the inventory key of §4.2).
	File string `json:"file"`
	// Suite is the suite identifier read from the vector file.
	Suite string `json:"suite"`
	// Passed are the stable passing case IDs.
	Passed []string `json:"passed"`
	// Skipped are the documented skips.
	Skipped []SharedSkippedCase `json:"skipped"`
	// Failed are the failed cases.
	Failed []SharedFailedCase `json:"failed"`
}

// SharedDigest is the aggregate vector digest fact of the shared report
// contract (§4.5; fc-manifest conformance_suite). Only the Go runner
// verifies the digest against the manifest; the Rust side embeds its
// vectors and carries no digest facts (the orchestrator verifies the
// inventory independently).
type SharedDigest struct {
	// OK reports whether the computed aggregate matches the manifest.
	OK bool `json:"ok"`
	// Computed is the computed aggregate sha256.
	Computed string `json:"computed"`
	// Recorded is the manifest-recorded aggregate sha256.
	Recorded string `json:"recorded"`
	// Suites is the counted vector file count.
	Suites int `json:"suites"`
	// Cases is the counted total case count.
	Cases int `json:"cases"`
}

// SharedRunReport is the complete shared report contract document.
type SharedRunReport struct {
	// Schema is the frozen contract identifier.
	Schema string `json:"schema"`
	// Runner identifies the emitting runner: "go" or "rust".
	Runner string `json:"runner"`
	// Digest is the aggregate digest fact (Go side only).
	Digest SharedDigest `json:"digest,omitempty"`
	// Suites are the per-suite reports in the frozen inventory order.
	Suites []*SharedSuiteReport `json:"suites"`
}

// sharedReportSchema is the frozen shared report contract identifier.
const sharedReportSchema = "consema.shared-conformance@1"

// ToSharedRunReport converts one Go run report into the shared report
// contract. The per-suite vector file basenames come from the frozen
// inventory (§4.2).
func ToSharedRunReport(report *RunReport) *SharedRunReport {
	fileBySuite := make(map[string]string, len(allSuites))
	for _, definition := range allSuites {
		fileBySuite[definition.SuiteID] = definition.File
	}
	shared := &SharedRunReport{
		Schema: sharedReportSchema,
		Runner: "go",
		Digest: SharedDigest{
			OK:       report.Digest.OK,
			Computed: report.Digest.Computed,
			Recorded: report.Digest.Recorded,
			Suites:   report.Digest.Suites,
			Cases:    report.Digest.Cases,
		},
		Suites: make([]*SharedSuiteReport, 0, len(report.Suites)),
	}
	for _, suite := range report.Suites {
		skipped := make([]SharedSkippedCase, 0, len(suite.Skipped))
		for _, skip := range suite.Skipped {
			skipped = append(skipped, SharedSkippedCase{
				ID:         skip.ID,
				Capability: skip.Capability,
				Reason:     skip.Reason,
			})
		}
		failed := make([]SharedFailedCase, 0, len(suite.Failed))
		for _, failure := range suite.Failed {
			failed = append(failed, SharedFailedCase{ID: failure.ID, Message: failure.Message})
		}
		shared.Suites = append(shared.Suites, &SharedSuiteReport{
			File:    fileBySuite[suite.Suite],
			Suite:   suite.Suite,
			Passed:  suite.Passed,
			Skipped: skipped,
			Failed:  failed,
		})
	}
	return shared
}

// MarshalSharedReport renders one shared report contract document as JSON.
func MarshalSharedReport(shared *SharedRunReport) ([]byte, error) {
	return json.Marshal(shared)
}

// UnmarshalSharedReport parses one shared report contract document and
// validates its frozen schema identifier and runner label.
func UnmarshalSharedReport(bytes []byte) (*SharedRunReport, error) {
	var shared SharedRunReport
	if err := json.Unmarshal(bytes, &shared); err != nil {
		return nil, fmt.Errorf("shared report is not valid JSON: %w", err)
	}
	if shared.Schema != sharedReportSchema {
		return nil, fmt.Errorf("shared report schema %q != %q", shared.Schema, sharedReportSchema)
	}
	if shared.Runner != "go" && shared.Runner != "rust" {
		return nil, fmt.Errorf("shared report runner %q is neither go nor rust", shared.Runner)
	}
	for _, suite := range shared.Suites {
		if suite.Passed == nil {
			suite.Passed = []string{}
		}
		if suite.Skipped == nil {
			suite.Skipped = []SharedSkippedCase{}
		}
		if suite.Failed == nil {
			suite.Failed = []SharedFailedCase{}
		}
	}
	return &shared, nil
}

// SharedCaseVerdict is one runner's verdict for one case.
type SharedCaseVerdict string

const (
	// VerdictPassed means the case executed and passed.
	VerdictPassed SharedCaseVerdict = "passed"
	// VerdictSkipped means the case is a documented skip.
	VerdictSkipped SharedCaseVerdict = "skipped"
	// VerdictFailed means the case failed.
	VerdictFailed SharedCaseVerdict = "failed"
	// VerdictAbsent means the side ran no such case (inventory divergence).
	VerdictAbsent SharedCaseVerdict = "absent"
)

// DisagreementKind classifies one case-level disagreement.
type DisagreementKind string

const (
	// DisagreementMismatch is a hard disagreement: the two sides judge the
	// same case differently (passed vs failed, skipped vs failed, or a skip
	// without the required documentation), or a suite or case exists on one
	// side only, or the per-file suite identifiers disagree.
	DisagreementMismatch DisagreementKind = "mismatch"
	// DisagreementDocumentedSkip is a documented-skip asymmetry: one side
	// skips the case with the required documentation (RFC 0016 §7) while
	// the other side passes it. Never silent; blocking only under strict
	// skip semantics.
	DisagreementDocumentedSkip DisagreementKind = "documented-skip"
)

// SharedCaseDisagreement is one case-level disagreement between the two
// runners.
type SharedCaseDisagreement struct {
	// File is the vector file basename.
	File string
	// Case is the case identifier; empty for suite-level disagreements.
	Case string
	// GoVerdict and RustVerdict are the two sides' verdicts (VerdictAbsent
	// when the side ran no such case or suite).
	GoVerdict   SharedCaseVerdict
	RustVerdict SharedCaseVerdict
	// Kind classifies the disagreement.
	Kind DisagreementKind
	// Detail is the human-readable explanation (suite-level disagreements
	// and inventory divergences).
	Detail string
	// SkipCapability and SkipReason carry the documented skip's facts when
	// the disagreement involves a skip.
	SkipCapability string
	SkipReason     string
}

// SharedSuiteRow is the per-suite two-side summary of one comparison.
type SharedSuiteRow struct {
	// File is the vector file basename.
	File string
	// GoSuite and RustSuite are the two sides' suite identifiers.
	GoSuite   string
	RustSuite string
	// GoPassed, GoSkipped, GoFailed and the Rust equivalents are the
	// per-side counts.
	GoPassed, GoSkipped, GoFailed       int
	RustPassed, RustSkipped, RustFailed int
	// Agree is the number of compared cases whose verdicts match; Total is
	// the number of compared cases (the union of both sides' case sets).
	Agree, Total int
	// Disagreements lists this suite's case-level disagreements.
	Disagreements []SharedCaseDisagreement
}

// SharedComparison is the complete case-id-level comparison of the two
// runners' shared reports.
type SharedComparison struct {
	// Rows are the per-suite comparisons.
	Rows []SharedSuiteRow
	// TotalCases is the number of compared cases across suites.
	TotalCases int
	// AgreeCases is the number of cases whose verdicts match.
	AgreeCases int
	// HardMismatches are the blocking disagreements.
	HardMismatches []SharedCaseDisagreement
	// DocumentedSkips are the documented-skip asymmetries (blocking only
	// under strict skip semantics).
	DocumentedSkips []SharedCaseDisagreement
	// GoPassed, GoSkipped, GoFailed and the Rust equivalents are the
	// totals across suites.
	GoPassed, GoSkipped, GoFailed       int
	RustPassed, RustSkipped, RustFailed int
}

// CompareSharedReports compares the two runners' shared reports case by
// case (§4.1: the same vectors, both sides, case-id-level agreement). The
// comparison keys suites by vector file basename and requires identical
// inventories: a suite or case present on exactly one side is a hard
// mismatch, and the per-file suite identifier must agree. strictSkips makes
// documented-skip asymmetries blocking.
func CompareSharedReports(goSide, rustSide *SharedRunReport, strictSkips bool) *SharedComparison {
	goByFile := suiteIndex(goSide)
	rustByFile := suiteIndex(rustSide)
	files := make([]string, 0, len(goByFile))
	for file := range goByFile {
		files = append(files, file)
	}
	for file := range rustByFile {
		if _, ok := goByFile[file]; !ok {
			files = append(files, file)
		}
	}
	sort.Strings(files)

	comparison := &SharedComparison{}
	for _, file := range files {
		goSuite := goByFile[file]
		rustSuite := rustByFile[file]
		row := SharedSuiteRow{
			File:      file,
			GoSuite:   suiteID(goSuite),
			RustSuite: suiteID(rustSuite),
		}
		var suiteDisagreements []SharedCaseDisagreement

		// Suite-level inventory and identifier checks.
		switch {
		case goSuite == nil:
			disagreement := suiteLevelDisagreement(file, "suite present only on the rust side",
				VerdictAbsent, VerdictPassed)
			suiteDisagreements = append(suiteDisagreements, disagreement)
			comparison.HardMismatches = append(comparison.HardMismatches, disagreement)
			comparison.Rows = append(comparison.Rows, row)
			continue
		case rustSuite == nil:
			disagreement := suiteLevelDisagreement(file, "suite present only on the go side",
				VerdictPassed, VerdictAbsent)
			suiteDisagreements = append(suiteDisagreements, disagreement)
			comparison.HardMismatches = append(comparison.HardMismatches, disagreement)
			comparison.Rows = append(comparison.Rows, row)
			continue
		case goSuite.Suite != rustSuite.Suite:
			disagreement := suiteLevelDisagreement(file,
				fmt.Sprintf("suite identifier mismatch: go %q vs rust %q", goSuite.Suite, rustSuite.Suite),
				VerdictPassed, VerdictPassed)
			suiteDisagreements = append(suiteDisagreements, disagreement)
			comparison.HardMismatches = append(comparison.HardMismatches, disagreement)
		}
		row.GoPassed, row.GoSkipped, row.GoFailed = suiteCounts(goSuite)
		row.RustPassed, row.RustSkipped, row.RustFailed = suiteCounts(rustSuite)
		comparison.GoPassed += row.GoPassed
		comparison.GoSkipped += row.GoSkipped
		comparison.GoFailed += row.GoFailed
		comparison.RustPassed += row.RustPassed
		comparison.RustSkipped += row.RustSkipped
		comparison.RustFailed += row.RustFailed

		goVerdicts := verdictIndex(goSuite)
		rustVerdicts := verdictIndex(rustSuite)
		ids := make([]string, 0, len(goVerdicts))
		for id := range goVerdicts {
			ids = append(ids, id)
		}
		for id := range rustVerdicts {
			if _, ok := goVerdicts[id]; !ok {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		row.Total = len(ids)
		comparison.TotalCases += len(ids)
		for _, id := range ids {
			goVerdict := goVerdicts[id]
			rustVerdict := rustVerdicts[id]
			disagreement := classifyCaseDisagreement(file, id, goVerdict, rustVerdict, goSuite, rustSuite, strictSkips)
			if disagreement == nil {
				row.Agree++
				comparison.AgreeCases++
				continue
			}
			suiteDisagreements = append(suiteDisagreements, *disagreement)
			switch disagreement.Kind {
			case DisagreementDocumentedSkip:
				comparison.DocumentedSkips = append(comparison.DocumentedSkips, *disagreement)
			default:
				comparison.HardMismatches = append(comparison.HardMismatches, *disagreement)
			}
		}
		row.Disagreements = suiteDisagreements
		comparison.Rows = append(comparison.Rows, row)
	}
	return comparison
}

// classifyCaseDisagreement judges one case against the two sides' verdicts.
// It returns nil when the verdicts agree, and the disagreement record
// otherwise.
func classifyCaseDisagreement(file, id string, goVerdict, rustVerdict SharedCaseVerdict,
	goSuite, rustSuite *SharedSuiteReport, strictSkips bool) *SharedCaseDisagreement {
	if goVerdict == VerdictAbsent {
		return &SharedCaseDisagreement{
			File: file, Case: id, GoVerdict: VerdictAbsent, RustVerdict: rustVerdict,
			Kind: DisagreementMismatch, Detail: "case present only on the rust side",
		}
	}
	if rustVerdict == VerdictAbsent {
		return &SharedCaseDisagreement{
			File: file, Case: id, GoVerdict: goVerdict, RustVerdict: VerdictAbsent,
			Kind: DisagreementMismatch, Detail: "case present only on the go side",
		}
	}
	if goVerdict == rustVerdict {
		if goVerdict != VerdictSkipped {
			return nil
		}
		// Both sides skip the same case: the skip must be the same skip —
		// documented on both sides and carrying the same capability claim.
		goSkip, goFound := findSkip(goSuite, id)
		rustSkip, rustFound := findSkip(rustSuite, id)
		if !documented(goSkip, goFound) || !documented(rustSkip, rustFound) ||
			goSkip.Capability != rustSkip.Capability {
			return &SharedCaseDisagreement{
				File: file, Case: id, GoVerdict: goVerdict, RustVerdict: rustVerdict,
				Kind: DisagreementMismatch, Detail: "skip not documented identically on both sides",
			}
		}
		return nil
	}
	// One side skips, the other passes: documented-skip asymmetry when the
	// skipping side carries the required documentation, hard mismatch
	// otherwise (and under strict skip semantics).
	if (goVerdict == VerdictSkipped && rustVerdict == VerdictPassed) ||
		(goVerdict == VerdictPassed && rustVerdict == VerdictSkipped) {
		skip, skipFound := findSkip(goSuite, id)
		skipSide := "go"
		if rustVerdict == VerdictSkipped {
			skip, skipFound = findSkip(rustSuite, id)
			skipSide = "rust"
		}
		kind := DisagreementMismatch
		detail := fmt.Sprintf("undocumented skip on the %s side", skipSide)
		if documented(skip, skipFound) {
			kind = DisagreementDocumentedSkip
			detail = fmt.Sprintf("documented skip on the %s side", skipSide)
		}
		disagreement := &SharedCaseDisagreement{
			File: file, Case: id, GoVerdict: goVerdict, RustVerdict: rustVerdict,
			Kind: kind, Detail: detail,
			SkipCapability: skip.Capability, SkipReason: skip.Reason,
		}
		if kind == DisagreementDocumentedSkip && strictSkips {
			// Strict skip semantics: a skip on one side must be a skip on
			// the other side too ("skip 必须两侧同 skip").
			disagreement.Detail = "documented skip on one side only (strict mode)"
			disagreement.Kind = DisagreementMismatch
		}
		return disagreement
	}
	// The remaining combinations are pass vs fail and fail vs skip.
	return &SharedCaseDisagreement{
		File: file, Case: id, GoVerdict: goVerdict, RustVerdict: rustVerdict,
		Kind: DisagreementMismatch, Detail: fmt.Sprintf("go=%s rust=%s", goVerdict, rustVerdict),
	}
}

// suiteLevelDisagreement builds one suite-level disagreement record.
func suiteLevelDisagreement(file, detail string, goVerdict, rustVerdict SharedCaseVerdict) SharedCaseDisagreement {
	return SharedCaseDisagreement{
		File: file, Case: "", GoVerdict: goVerdict, RustVerdict: rustVerdict,
		Kind: DisagreementMismatch, Detail: detail,
	}
}

// suiteID returns one suite's identifier, empty when absent.
func suiteID(suite *SharedSuiteReport) string {
	if suite == nil {
		return ""
	}
	return suite.Suite
}

// suiteCounts returns one suite's passed/skipped/failed counts.
func suiteCounts(suite *SharedSuiteReport) (passed, skipped, failed int) {
	return len(suite.Passed), len(suite.Skipped), len(suite.Failed)
}

// suiteIndex maps vector file basenames to shared suite reports.
func suiteIndex(report *SharedRunReport) map[string]*SharedSuiteReport {
	index := make(map[string]*SharedSuiteReport, len(report.Suites))
	for _, suite := range report.Suites {
		index[suite.File] = suite
	}
	return index
}

// verdictIndex maps each case id of one shared suite to its verdict.
func verdictIndex(suite *SharedSuiteReport) map[string]SharedCaseVerdict {
	index := make(map[string]SharedCaseVerdict,
		len(suite.Passed)+len(suite.Skipped)+len(suite.Failed))
	for _, id := range suite.Passed {
		index[id] = VerdictPassed
	}
	for _, skip := range suite.Skipped {
		index[skip.ID] = VerdictSkipped
	}
	for _, failure := range suite.Failed {
		index[failure.ID] = VerdictFailed
	}
	return index
}

// findSkip returns the skip record of one case id.
func findSkip(suite *SharedSuiteReport, id string) (SharedSkippedCase, bool) {
	for _, skip := range suite.Skipped {
		if skip.ID == id {
			return skip, true
		}
	}
	return SharedSkippedCase{}, false
}

// documented reports whether a skip record carries the RFC 0016 §7
// documentation (capability and reason, never silent).
func documented(skip SharedSkippedCase, found bool) bool {
	return found && skip.Capability != "" && skip.Reason != ""
}
