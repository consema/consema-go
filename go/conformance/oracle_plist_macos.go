package conformance

// The `consema.plist.macos-differential@1` oracle runner (0.17.0 milestone
// G3.3; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §2.4, §4.2, §4.4). The runner
// consumes the frozen facts of
// `conformance/oracles/plist-macos-v1/manifest.json` — the per-case input
// fixture, the Foundation-recorded lint / detected-format / convert /
// values outcomes, and the D-1..D-21 divergence inventory (RFC 0013 §13) —
// and applies the same inputs to the Go plist implementation. The manifest
// is the authority: it is read from its repository path, never embedded,
// and every recorded Foundation fact is compared against the observed Go
// behavior (accept/reject, native value facts, diagnostic classification).
//
// Mapping (manifest `comparison.consema_counterpart`): Consema Complete
// maps to lint ok; the plist profile is caller-selected (RFC 0013 §3), so
// `detected_format` selects the formation profile and the other profile
// must never form a complete document; the convert outcomes map to the
// cross-representation conversion (`ConvertTo`, RFC 0013 §7) or the
// canonical materialization of the same representation (RFC 0013 §10) —
// the runner never compares Consema materialization bytes, which are pinned
// by the conformance vectors (`shape_level` note); the values outcome maps
// to deterministic native value facts whose native model equals every
// successfully converted output (the Foundation oracle checks the same
// cross-file value consistency with `plutil -p`).
//
// Exclusion discipline (plan §4.4; oracle `policy`): a case annotated with
// a D-id in the manifest is a documented skip — the divergence record
// carries the exclusion id and the inventory's own reason, and the
// exclusion's Consema-side claims (RFC 0013) are still verified. A skip is
// never silent. The fixture set contains only Complete-under-Consema
// inputs, and no untracked allowlist exists: a case, fixture, or outcome
// not covered by the inventory fails the run.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"consema.dev/consema/document"
	"consema.dev/consema/plist"
)

// The frozen oracle pins: the suite identifier and the case count of the
// pinned manifest. A manifest change must update these pins deliberately
// (vector case-count discipline, plan §4.3).
const (
	plistMacosSuiteID       = "consema.plist.macos-differential@1"
	plistMacosExpectedCases = 7
)

// The stable classification codes the runner asserts (frozen registered
// codes, pinned by the plist-v1 vectors; RFC 0013 §5.1, §7).
const (
	plistConversionInexpressible = "plist.conversion.inexpressible@1"
	plistBinaryHeaderCode        = "plist.binary.header@1"
)

// plistMacosManifest is the frozen plist-macos-v1 oracle manifest.
type plistMacosManifest struct {
	Suite      string                   `json:"suite"`
	Cases      []plistMacosManifestCase `json:"cases"`
	Exclusions plistMacosExclusions     `json:"exclusions"`
}

// plistMacosExclusions is the divergence inventory container.
type plistMacosExclusions struct {
	DivergenceInventory []plistMacosExclusion `json:"divergence_inventory"`
}

// plistMacosExclusion is one D-id divergence inventory entry.
type plistMacosExclusion struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Oracle  string `json:"oracle"`
	Consema string `json:"consema"`
	Kind    string `json:"kind"`
}

// plistMacosManifestCase is one manifest case.
type plistMacosManifestCase struct {
	ID          string             `json:"id"`
	Input       string             `json:"input"`
	InputSHA256 string             `json:"input_sha256"`
	Divergence  *string            `json:"divergence"`
	Expected    plistMacosExpected `json:"expected"`
}

// plistMacosExpected is the Foundation-recorded outcome of one case.
type plistMacosExpected struct {
	Lint           string            `json:"lint"`
	DetectedFormat string            `json:"detected_format"`
	Convert        plistMacosConvert `json:"convert"`
	Values         string            `json:"values"`
}

// plistMacosConvert is the recorded per-direction convert outcome.
type plistMacosConvert struct {
	ToXML1    string `json:"to_xml1"`
	ToBinary1 string `json:"to_binary1"`
}

// OraclePlistMacOSLeg is one compared leg of one oracle case: the
// Foundation-recorded fact versus the observed Go behavior.
type OraclePlistMacOSLeg struct {
	// Leg is the compared leg: "formation", "format",
	// "convert.to_xml1", "convert.to_binary1", or "values".
	Leg string
	// Foundation is the manifest-recorded Foundation fact.
	Foundation string
	// Observed is the observed Go behavior.
	Observed string
	// Status is "passed", "skipped" (documented divergence), or
	// "failed".
	Status string
}

// OraclePlistMacOSSkipped is one documented divergence skip (never silent;
// plan §4.4): the exclusion id and the inventory's own reason.
type OraclePlistMacOSSkipped struct {
	// CaseID is the manifest case identifier.
	CaseID string
	// Leg is the compared leg the divergence applies to.
	Leg string
	// ExclusionID is the D-id from the manifest divergence inventory.
	ExclusionID string
	// Reason is the inventory's own divergence statement.
	Reason string
}

// OraclePlistMacOSCase is the per-case Go-versus-Foundation comparison
// table.
type OraclePlistMacOSCase struct {
	// ID is the manifest case identifier.
	ID string
	// Divergence is the D-id annotation; empty when the case has none.
	Divergence string
	// Legs are the compared legs in run order.
	Legs []OraclePlistMacOSLeg
}

// OraclePlistMacOSReport is the complete differential run result.
type OraclePlistMacOSReport struct {
	// Suite is the suite identifier read from the manifest.
	Suite string
	// ManifestPath is the manifest file the facts were read from.
	ManifestPath string
	// Cases are the per-case comparison tables in manifest order.
	Cases []*OraclePlistMacOSCase
	// Skipped are the documented divergence skips (success; never
	// silent).
	Skipped []OraclePlistMacOSSkipped
	// Passed is the number of passed legs.
	Passed int
	// SkippedLegs is the number of legs excluded by a documented
	// divergence.
	SkippedLegs int
	// Failed are the stable case identifiers and failure descriptions.
	Failed []CaseFailure
	// ExpectedCases is the frozen case count assertion.
	ExpectedCases int
}

// CountAsserted reports whether the frozen case count matched the manifest
// (a manifest case change must update the runner deliberately).
func (r *OraclePlistMacOSReport) CountAsserted() bool {
	return r.ExpectedCases == len(r.Cases)
}

// Conformant reports whether every executed leg passed and the count
// assertion held; documented divergence skips count as success.
func (r *OraclePlistMacOSReport) Conformant() bool {
	return len(r.Failed) == 0 && r.CountAsserted()
}

// RunPlistMacOSOracle executes the plist-macos-v1 differential over the Go
// plist implementation (0.17.0 G3.3). manifestPath points at the frozen
// oracle manifest and repoRoot at the repository root the manifest inputs
// are relative to; the manifest facts are the authority and the runner
// embeds no copy of them. The Foundation facts are frozen records, so no
// pinned-macOS host is required.
func RunPlistMacOSOracle(manifestPath, repoRoot string) (*OraclePlistMacOSReport, error) {
	bytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("plist-macos oracle manifest: %w", err)
	}
	var manifest plistMacosManifest
	if err := json.Unmarshal(bytes, &manifest); err != nil {
		return nil, fmt.Errorf("plist-macos oracle manifest is not strict JSON: %w", err)
	}
	report := &OraclePlistMacOSReport{
		Suite:         manifest.Suite,
		ManifestPath:  manifestPath,
		ExpectedCases: plistMacosExpectedCases,
	}
	state := &oracleState{report: report, manifest: &manifest, repoRoot: repoRoot}
	state.run()
	return report, nil
}

// oracleState is the shared run state of one differential execution.
type oracleState struct {
	report     *OraclePlistMacOSReport
	manifest   *plistMacosManifest
	repoRoot   string
	exclusions map[string]plistMacosExclusion
}

// run executes the fixed manifest validations and every case.
func (state *oracleState) run() {
	if state.manifest.Suite != plistMacosSuiteID {
		state.report.Failed = append(state.report.Failed, CaseFailure{
			ID:      "oracle.suite",
			Message: "unexpected suite " + state.manifest.Suite,
		})
	}
	state.exclusions = map[string]plistMacosExclusion{}
	for _, entry := range state.manifest.Exclusions.DivergenceInventory {
		state.exclusions[entry.ID] = entry
	}
	if !plistMacosInventoryComplete(state.exclusions) {
		state.report.Failed = append(state.report.Failed, CaseFailure{
			ID:      "oracle.inventory",
			Message: "divergence inventory must be exactly D-1..D-21 with name, oracle, consema, and kind",
		})
	}
	if state.report.ExpectedCases != len(state.manifest.Cases) {
		state.report.Failed = append(state.report.Failed, CaseFailure{
			ID: "oracle.count",
			Message: fmt.Sprintf("case count changed: expected %d, found %d",
				state.report.ExpectedCases, len(state.manifest.Cases)),
		})
	}
	seen := map[string]bool{}
	for index := range state.manifest.Cases {
		manifestCase := &state.manifest.Cases[index]
		if seen[manifestCase.ID] {
			state.report.Failed = append(state.report.Failed, CaseFailure{
				ID: manifestCase.ID, Message: "duplicate case id",
			})
			continue
		}
		seen[manifestCase.ID] = true
		state.runCase(manifestCase)
	}
	for _, caseReport := range state.report.Cases {
		for _, leg := range caseReport.Legs {
			switch leg.Status {
			case "passed":
				state.report.Passed++
			case "skipped":
				state.report.SkippedLegs++
			}
		}
	}
}

// runCase validates one manifest case, applies its input fixture to the Go
// plist implementation, and records the compared legs plus the documented
// divergence skip when the case carries a D-id annotation.
func (state *oracleState) runCase(manifestCase *plistMacosManifestCase) {
	caseReport := &OraclePlistMacOSCase{ID: manifestCase.ID}
	if manifestCase.Divergence != nil {
		caseReport.Divergence = *manifestCase.Divergence
	}
	state.report.Cases = append(state.report.Cases, caseReport)
	if message := validatePlistMacOSCase(manifestCase, state); message != "" {
		state.report.Failed = append(state.report.Failed, CaseFailure{
			ID: manifestCase.ID, Message: message,
		})
		return
	}
	source, message := state.readInput(manifestCase)
	if message != "" {
		state.report.Failed = append(state.report.Failed, CaseFailure{
			ID: manifestCase.ID, Message: message,
		})
		return
	}
	run := &plistMacosCaseRun{state: state, caseReport: caseReport, c: manifestCase, source: source}
	run.profile = oraclePlistProfile(manifestCase.Expected.DetectedFormat)
	run.runLegs()
	if manifestCase.Divergence != nil {
		entry, ok := state.exclusions[*manifestCase.Divergence]
		if ok {
			state.report.Skipped = append(state.report.Skipped, OraclePlistMacOSSkipped{
				CaseID:      manifestCase.ID,
				Leg:         oracleDivergenceLeg(*manifestCase.Divergence),
				ExclusionID: entry.ID,
				Reason:      oracleDivergenceReason(entry),
			})
		}
	}
}

// validatePlistMacOSCase validates the frozen case schema and the
// divergence annotation against the inventory (mirroring the pinned-macOS
// wrapper's outcome enumerations).
func validatePlistMacOSCase(manifestCase *plistMacosManifestCase,
	state *oracleState) string {
	expected := &manifestCase.Expected
	if expected.Lint != "ok" && expected.Lint != "error" {
		return "unknown expected lint outcome " + expected.Lint
	}
	if expected.DetectedFormat != "xml1" && expected.DetectedFormat != "binary1" {
		return "unknown expected detected format " + expected.DetectedFormat
	}
	if (expected.Convert.ToXML1 != "ok" && expected.Convert.ToXML1 != "error") ||
		(expected.Convert.ToBinary1 != "ok" && expected.Convert.ToBinary1 != "error") {
		return "unknown expected convert outcome"
	}
	if expected.Values != "ok" {
		return "unknown expected values outcome " + expected.Values
	}
	if manifestCase.Input == "" || manifestCase.InputSHA256 == "" {
		return "case input facts are absent"
	}
	if manifestCase.Divergence != nil {
		if _, ok := state.exclusions[*manifestCase.Divergence]; !ok {
			return "divergence " + *manifestCase.Divergence +
				" is not in the exclusion inventory"
		}
	}
	return ""
}

// readInput reads one fixture within the repository and verifies its pinned
// digest.
func (state *oracleState) readInput(manifestCase *plistMacosManifestCase) ([]byte, string) {
	root := filepath.Clean(state.repoRoot) + string(filepath.Separator)
	input := filepath.Clean(filepath.Join(state.repoRoot, manifestCase.Input))
	if !strings.HasPrefix(input, root) {
		return nil, "case input escapes the repository: " + manifestCase.Input
	}
	bytes, err := os.ReadFile(input)
	if err != nil {
		return nil, "case input is unreadable: " + manifestCase.Input
	}
	digest := sha256.Sum256(bytes)
	if hex.EncodeToString(digest[:]) != manifestCase.InputSHA256 {
		return nil, "case input digest mismatch: expected " +
			manifestCase.InputSHA256 + ", got " + hex.EncodeToString(digest[:])
	}
	return bytes, ""
}

// plistMacosInventoryComplete reports whether the divergence inventory is
// exactly the frozen D-1..D-21 set with complete entries (RFC 0013 §13; no
// untracked allowlist).
func plistMacosInventoryComplete(entries map[string]plistMacosExclusion) bool {
	for id := 1; id <= 21; id++ {
		entry, ok := entries[fmt.Sprintf("D-%d", id)]
		if !ok || entry.Name == "" || entry.Oracle == "" || entry.Consema == "" || entry.Kind == "" {
			return false
		}
	}
	return len(entries) == 21
}

// oraclePlistProfile resolves the formation profile of one detected format.
func oraclePlistProfile(detected string) plist.PlistProfile {
	if detected == "binary1" {
		return plist.PlistProfileBinaryV1
	}
	return plist.PlistProfileXmlV1
}

// oracleDivergenceLeg is the compared leg the divergence annotation applies
// to: the leg whose Foundation fact is documented as divergent for this
// case.
func oracleDivergenceLeg(id string) string {
	switch id {
	case "D-20", "D-21":
		return "convert.to_xml1"
	}
	return "values"
}

// oracleDivergenceReason is the inventory's own divergence statement; the
// reason is never invented by the runner.
func oracleDivergenceReason(entry plistMacosExclusion) string {
	return entry.Name + ". " + entry.Oracle + " Consema: " + entry.Consema
}

// oraclePlistParse forms one document under one profile with the frozen
// default limits.
func oraclePlistParse(bytes []byte, profile plist.PlistProfile) (*plist.Document, *plist.FormationFailure) {
	return plist.Parse(bytes, profile, plist.PlistEncodingProfileDefault(),
		plist.DefaultPlistParseLimits())
}

// plistMacosHasDiagnostic reports whether one document carries the given
// diagnostic code.
func plistMacosHasDiagnostic(doc *plist.Document, code string) bool {
	if doc == nil {
		return false
	}
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

// plistMacosCaseRun is the execution state of one case.
type plistMacosCaseRun struct {
	state      *oracleState
	caseReport *OraclePlistMacOSCase
	c          *plistMacosManifestCase
	source     []byte
	profile    plist.PlistProfile
	doc        *plist.Document
	xmlOut     *plist.Document
	binaryOut  *plist.Document
}

// runLegs executes the five compared legs in run order.
func (run *plistMacosCaseRun) runLegs() {
	run.formationLeg()
	run.formatLeg()
	run.toXMLLeg()
	run.toBinaryLeg()
	run.valuesLeg()
}

// record appends one leg result and its failure, when failed.
func (run *plistMacosCaseRun) record(leg, foundation, observed, status string) {
	result := OraclePlistMacOSLeg{Leg: leg, Foundation: foundation,
		Observed: observed, Status: status}
	if status == "failed" {
		run.state.report.Failed = append(run.state.report.Failed, CaseFailure{
			ID: run.c.ID, Message: leg + ": " + observed,
		})
	}
	run.caseReport.Legs = append(run.caseReport.Legs, result)
}

// formationLeg compares the lint outcome: Consema Complete maps to lint ok
// (manifest comparison.consema_counterpart); a lint error maps to any
// non-complete formation.
func (run *plistMacosCaseRun) formationLeg() {
	foundation := "lint=" + run.c.Expected.Lint
	doc, failure := oraclePlistParse(run.source, run.profile)
	if failure == nil {
		run.doc = doc
	}
	ok := false
	observed := "formation failure"
	switch run.c.Expected.Lint {
	case "ok":
		ok = failure == nil && doc.FormationStatus() == document.FormationStatusComplete &&
			doc.NativeDocument() != nil
		if ok {
			observed = "status=Complete, native model provable"
		} else if failure == nil {
			observed = "status=" + doc.FormationStatus().String() + ", no native model"
		}
	case "error":
		ok = failure != nil || doc.FormationStatus() != document.FormationStatusComplete
		if failure == nil {
			observed = "status=" + doc.FormationStatus().String()
		}
	}
	run.record("formation", foundation, observed, verdict(ok))
}

// formatLeg compares the detected-format fact: the detected profile is the
// only profile under which the fixture forms a complete document. The
// cross-profile rejection is classified: an XML fixture under the binary
// profile is Recovered with the header diagnostic (RFC 0013 §5.1) and no
// native model; a binary fixture under the XML profile fails formation
// fatally.
func (run *plistMacosCaseRun) formatLeg() {
	foundation := "detected_format=" + run.c.Expected.DetectedFormat
	other := plist.PlistProfileBinaryV1
	if run.profile == plist.PlistProfileBinaryV1 {
		other = plist.PlistProfileXmlV1
	}
	cross, failure := oraclePlistParse(run.source, other)
	ok := false
	observed := "other-profile parse unexpected"
	if run.profile == plist.PlistProfileXmlV1 {
		ok = failure == nil &&
			cross.FormationStatus() != document.FormationStatusComplete &&
			cross.NativeDocument() == nil &&
			plistMacosHasDiagnostic(cross, plistBinaryHeaderCode)
		if failure == nil {
			observed = "other-profile (binary) parse " + cross.FormationStatus().String() +
				" with " + plistBinaryHeaderCode + ", no native model"
		}
	} else {
		ok = failure != nil
		observed = "other-profile (xml) parse fails (FatalFormationFailure)"
	}
	run.record("format", foundation, observed, verdict(ok))
}

// toXMLLeg compares the to_xml1 convert outcome. For an XML source the
// counterpart is the canonical XML materialization (RFC 0013 §10); for a
// binary source it is the cross-representation conversion (RFC 0013 §7).
// The D-20 annotation documents the conversion leg where Foundation
// succeeds and Consema refuses by design; the D-21 annotation records the
// equivalent outcome where both oracles refuse.
func (run *plistMacosCaseRun) toXMLLeg() {
	foundation := "to_xml1=" + run.c.Expected.Convert.ToXML1
	if run.profile == plist.PlistProfileXmlV1 {
		outcome, target, observed := run.materializeXML()
		run.record("convert.to_xml1", foundation, observed,
			verdict(outcome == (run.c.Expected.Convert.ToXML1 == "ok")))
		if outcome {
			run.xmlOut = target
		}
		return
	}
	if run.c.Divergence != nil && *run.c.Divergence == "D-20" &&
		run.c.Expected.Convert.ToXML1 == "ok" {
		// Documented skip (D-20): Foundation converts the shared reference
		// silently, duplicating the shared object in XML; Consema refuses
		// atomically by design because shared object identity is a
		// binary-only fact (RFC 0013 §7 hard gate 3). The exclusion's
		// Consema-side claim is verified: the conversion fails with the
		// frozen inexpressible code and returns no target document.
		code := run.convertToXMLFailureCode()
		ok := code == plistConversionInexpressible
		observed := "refused atomically (" + code + ")"
		status := "skipped"
		if !ok {
			observed = "conversion outcome contradicts the D-20 record: " + code
			status = "failed"
		}
		run.record("convert.to_xml1", foundation, observed, status)
		return
	}
	converted, failure := run.convertToXML()
	if run.c.Expected.Convert.ToXML1 == "ok" {
		ok := false
		observed := "conversion refused"
		if failure != nil {
			observed = "conversion refused (" + failure.Code() + ")"
		} else if converted.Document().FormationStatus() == document.FormationStatusComplete &&
			converted.Document().NativeDocument() != nil &&
			run.doc.NativeDocument().Equal(converted.Document().NativeDocument()) {
			ok = true
			observed = "converted to plist.xml@1, complete, native model equal"
			run.xmlOut = converted.Document()
		} else {
			observed = "converted document is not complete or native model differs"
		}
		run.record("convert.to_xml1", foundation, observed, verdict(ok))
		return
	}
	// Recorded error: both oracles refuse the conversion (D-21 records the
	// equivalent outcome; the Consema mechanism is the UID
	// XML-inexpressibility hard gate 3 of RFC 0013 §7).
	code := run.convertToXMLFailureCode()
	ok := code == plistConversionInexpressible
	observed := "refused atomically (" + code + ")"
	if !ok {
		observed = "conversion outcome contradicts the recorded refusal: " + code
	}
	run.record("convert.to_xml1", foundation, observed, verdict(ok))
}

// toBinaryLeg compares the to_binary1 convert outcome. For a binary source
// the counterpart is the canonical binary materialization (RFC 0013 §10,
// UIDs preserved); for an XML source it is the cross-representation
// conversion (RFC 0013 §7).
func (run *plistMacosCaseRun) toBinaryLeg() {
	foundation := "to_binary1=" + run.c.Expected.Convert.ToBinary1
	if run.profile == plist.PlistProfileBinaryV1 {
		outcome, target, observed := run.materializeBinary()
		run.record("convert.to_binary1", foundation, observed,
			verdict(outcome == (run.c.Expected.Convert.ToBinary1 == "ok")))
		if outcome {
			run.binaryOut = target
		}
		return
	}
	converted, failure := run.convertToBinary()
	ok := false
	observed := "conversion refused"
	if failure != nil {
		observed = "conversion refused (" + failure.Code() + ")"
	} else if converted.Document().FormationStatus() == document.FormationStatusComplete &&
		converted.Document().NativeDocument() != nil &&
		run.doc.NativeDocument().Equal(converted.Document().NativeDocument()) {
		ok = true
		observed = "converted to plist.binary@1, complete, native model equal"
		run.binaryOut = converted.Document()
	} else {
		observed = "converted document is not complete or native model differs"
	}
	run.record("convert.to_binary1", foundation, observed,
		verdict(ok == (run.c.Expected.Convert.ToBinary1 == "ok")))
}

// valuesLeg compares the values outcome: the native value facts are
// deterministic (the same input forms the identical native model), which is
// the Go counterpart of the Foundation cross-file `plutil -p` consistency
// the oracle verifies. For the D-2 annotated case the value-content facts
// are documented as divergent (Foundation silent last-wins versus Consema
// ordered preservation), so the leg additionally verifies the exclusion's
// Consema-side claim: duplicate keys are preserved, never collapsed (RFC
// 0013 §4.4, §5.9).
func (run *plistMacosCaseRun) valuesLeg() {
	foundation := "values=" + run.c.Expected.Values
	if run.doc == nil || run.doc.FormationStatus() != document.FormationStatusComplete {
		run.record("values", foundation, "input did not form a complete document", "failed")
		return
	}
	again, failure := oraclePlistParse(run.source, run.profile)
	ok := failure == nil && again.FormationStatus() == document.FormationStatusComplete &&
		again.NativeDocument() != nil && run.doc.NativeDocument().Equal(again.NativeDocument())
	observed := "deterministic parse"
	if ok && run.c.Divergence != nil && *run.c.Divergence == "D-2" {
		groups := oracleDuplicateKeyGroups(run.doc)
		ok = groups >= 1
		if groups == 1 {
			observed = "deterministic parse; duplicate keys preserved (1 group)"
		} else {
			observed = fmt.Sprintf("deterministic parse; duplicate keys preserved (%d groups)", groups)
		}
	}
	run.record("values", foundation, observed, verdict(ok))
}

// convertToXML converts one complete binary document to XML; nil when the
// input is not a complete document (the caller then records the failure).
func (run *plistMacosCaseRun) convertToXML() (*plist.ConvertedDocument, *plist.ConversionFailure) {
	if run.doc == nil || run.doc.FormationStatus() != document.FormationStatusComplete {
		return nil, nil
	}
	converted, failure := run.doc.ConvertTo(plist.PlistProfileXmlV1, plist.DefaultPlistParseLimits())
	if failure != nil {
		return nil, failure
	}
	return converted, nil
}

// convertToXMLFailureCode returns the conversion refusal code; "" when the
// input is not convertible or the conversion succeeds.
func (run *plistMacosCaseRun) convertToXMLFailureCode() string {
	_, failure := run.convertToXML()
	if failure == nil {
		return ""
	}
	return failure.Code()
}

// convertToBinary converts one complete XML document to binary; nil when
// the input is not a complete document.
func (run *plistMacosCaseRun) convertToBinary() (*plist.ConvertedDocument, *plist.ConversionFailure) {
	if run.doc == nil || run.doc.FormationStatus() != document.FormationStatusComplete {
		return nil, nil
	}
	converted, failure := run.doc.ConvertTo(plist.PlistProfileBinaryV1, plist.DefaultPlistParseLimits())
	if failure != nil {
		return nil, failure
	}
	return converted, nil
}

// materializeXML materializes the projected value tree as the canonical
// `plist.xml-canonical@1` document (RFC 0013 §10.1), the same-
// representation counterpart of `plutil -convert xml1`.
func (run *plistMacosCaseRun) materializeXML() (bool, *plist.Document, string) {
	if run.doc == nil || run.doc.FormationStatus() != document.FormationStatusComplete {
		return false, nil, "input did not form a complete document"
	}
	projection := plist.Project(run.doc, plist.NewProjectionRequestValueTree())
	if projection.Failed != nil {
		return false, nil, "value-tree projection failed (" +
			projection.Failed.Diagnostics[0].Code + ")"
	}
	request := document.NewMaterializationRequest(
		document.NewProfileId("plist.xml", 1),
		document.NewMaterializationStyleId("plist.xml-canonical", 1))
	result := plist.Materialize(projection.Complete.Value, request)
	if result.Failed != nil {
		return false, nil, "canonical materialization refused (" +
			result.Failed.Failure.Code() + ")"
	}
	return run.closureOK(result.Complete.Document, "materialized plist.xml-canonical@1")
}

// materializeBinary materializes the projected value tree as the canonical
// `plist.binary-canonical@1` document (RFC 0013 §10.2), the same-
// representation counterpart of `plutil -convert binary1`. UIDs are
// binary-only facts (RFC 0013 §5.8), so the projection uses the explicit
// UID-include policy.
func (run *plistMacosCaseRun) materializeBinary() (bool, *plist.Document, string) {
	if run.doc == nil || run.doc.FormationStatus() != document.FormationStatusComplete {
		return false, nil, "input did not form a complete document"
	}
	projection := plist.Project(run.doc,
		plist.NewProjectionRequestValueTreeWithUID(plist.UIDPolicyInclude))
	if projection.Failed != nil {
		return false, nil, "value-tree projection failed (" +
			projection.Failed.Diagnostics[0].Code + ")"
	}
	request := document.NewMaterializationRequest(
		document.NewProfileId("plist.binary", 1),
		document.NewMaterializationStyleId("plist.binary-canonical", 1),
	).WithEncoding(document.BinaryEncoding()).WithNewline(document.NewlineNone)
	result := plist.Materialize(projection.Complete.Value, request)
	if result.Failed != nil {
		return false, nil, "canonical materialization refused (" +
			result.Failed.Failure.Code() + ")"
	}
	return run.closureOK(result.Complete.Document, "materialized plist.binary-canonical@1")
}

// closureOK verifies the materialization closure (RFC 0013 §10.3): the
// target document is Complete and its native model equals the source native
// model.
func (run *plistMacosCaseRun) closureOK(target *plist.Document, observed string) (bool, *plist.Document, string) {
	if target.FormationStatus() != document.FormationStatusComplete ||
		target.NativeDocument() == nil ||
		!run.doc.NativeDocument().Equal(target.NativeDocument()) {
		return false, nil, "materialized document is not complete or native model differs"
	}
	return true, target, observed + ", complete, native model equal"
}

// oracleDuplicateKeyGroups counts the root dictionary keys with more than
// one association (RFC 0013 §4.4, §5.9 duplicate-key groups).
func oracleDuplicateKeyGroups(doc *plist.Document) int {
	dict, ok := doc.NativeDocument().RootValue().AsDict()
	if !ok {
		return 0
	}
	groups := 0
	var counted []plist.PlistKey
	for _, entry := range dict.Entries() {
		alreadyCounted := false
		for _, key := range counted {
			if key.Equal(entry.Key()) {
				alreadyCounted = true
				break
			}
		}
		if alreadyCounted {
			continue
		}
		if len(dict.PositionsOfKey(entry.Key())) > 1 {
			groups++
		}
		counted = append(counted, entry.Key())
	}
	return groups
}

// verdict maps one comparison result to the leg status.
func verdict(ok bool) string {
	if ok {
		return "passed"
	}
	return "failed"
}
