package conformance

// The `consema.ini.conformance@1` suite runner
// (consema-rs/crates/consema-conformance/src/ini_v1.rs). The 0.16.0 milestone (G2.2)
// implements the full INI family surface: the three explicit profiles
// (portable, Windows, Python ConfigParser), lossless physical/logical line
// facts, native and syntax queries, exact EntryMapping and explicit Object
// projections with provenance, all three canonical materialization styles,
// the eight versioned edit operations with dry-run/patch/proof audit
// artifacts, resource limits, and the frozen operation registry. All 20
// published cases are executed; the vector file is the authority and the
// runner embeds no expectation literals.

import (
	"context"
	"encoding/hex"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/ini"
	"consema.dev/consema/protocol"
)

// runIniV1 executes the embedded `consema.ini.conformance@1` suite.
func runIniV1(runner *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		switch vector.ID {
		case "formation.portable-lossless":
			runIniPortableLossless(vector, report)
		case "formation.profile-counterexample-matrix":
			runIniProfileCounterexamples(vector, report)
		case "formation.windows-utf16-case-and-quote":
			runIniWindowsUtf16(vector, report)
		case "formation.windows-explicit-code-page":
			runIniWindowsCodePage(vector, report)
		case "formation.python-default-continuation-raw":
			runIniPythonMultiline(vector, report)
		case "formation.python-unicode16-optionxform":
			runIniPythonOptionxform(vector, report)
		case "formation.recovery-never-fabricates-entry":
			runIniRecoveredAtomic(vector, report)
		case "query.native-order-and-profile-equivalence":
			runIniNativeQuery(vector, report)
		case "query.syntax-decoded-structure-order":
			runIniSyntaxQuery(vector, report)
		case "query.validation-limit-cancellation":
			runIniQueryFailures(vector, report)
		case "projection.exact-duplicate-entry-mapping":
			runIniProjectionExact(vector, report)
		case "projection.explicit-object-collapse":
			runIniProjectionCollapse(vector, report)
		case "projection.fragmented-value-provenance":
			runIniProjectionFragments(vector, report)
		case "materialization.all-canonical-styles":
			runIniMaterializationStyles(vector, report)
		case "materialization.atomic-failures-and-limits":
			runIniMaterializationLimits(vector, report)
		case "edit.all-eight-operations":
			runIniEditAllOperations(vector, report)
		case "edit.dry-run-patch-proof-and-atomic-failure":
			runIniEditAuditArtifacts(vector, report)
		case "resource.formation-limit-matrix":
			runIniFormationLimits(vector, report)
		case "resource.projection-limit-matrix":
			runIniProjectionLimits(vector, report)
		case "registry.frozen-eight-operation-surface":
			runIniOperationRegistry(vector, report)
		default:
			report.Failed = append(report.Failed, CaseFailure{
				ID:      vector.ID,
				Message: "runner does not recognize published INI case",
			})
		}
	}
	return report
}

// iniProfile resolves one vector profile identifier.
func iniProfile(value core.Value) (ini.IniProfile, string) {
	text, ok := value.(core.String)
	if !ok {
		return ini.IniProfile{}, "profile must be String"
	}
	switch string(text) {
	case "ini.portable@1":
		return ini.PortableV1, ""
	case "ini.windows@1":
		return ini.WindowsV1, ""
	case "ini.python-configparser@1":
		return ini.PythonConfigParserV1, ""
	}
	return ini.IniProfile{}, "unknown INI profile " + string(text)
}

// iniParseCase forms one vector source under the vector profile.
func iniParseCase(vector *caseData) (*ini.Document, string) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		return nil, "missing input.source"
	}
	profile, message := iniProfileField(vector)
	if message != "" {
		return nil, message
	}
	return iniParseText(profile, source)
}

// iniProfileField reads the vector profile field.
func iniProfileField(vector *caseData) (ini.IniProfile, string) {
	profile, ok := stringField(vector.Input, "profile")
	if !ok {
		return ini.IniProfile{}, "missing input.profile"
	}
	return iniProfile(core.String(profile))
}

// iniParseText parses one source under one profile with the default
// selection and limits.
func iniParseText(profile ini.IniProfile, source string) (*ini.Document, string) {
	doc, failure := ini.Parse([]byte(source), profile, ini.IniEncodingProfileDefault(),
		ini.DefaultIniParseLimits())
	if failure != nil {
		return nil, "INI formation failed: " + failure.Diagnostics()[0].Code
	}
	return doc, ""
}

// iniExactCoverage reports the exhaustive source coverage fact.
func iniExactCoverage(doc *ini.Document) bool {
	if doc.Source().IsEmpty() {
		return len(doc.LosslessStructuralIndex().Pieces()) == 0
	}
	pieces := doc.LosslessStructuralIndex().Pieces()
	kinds := doc.LosslessSyntaxKinds()
	if len(pieces) != len(kinds) || len(pieces) == 0 {
		return false
	}
	if pieces[0].Span().StartByte() != 0 ||
		pieces[len(pieces)-1].Span().EndByte() != doc.Source().Len() {
		return false
	}
	for index := 1; index < len(pieces); index++ {
		if pieces[index-1].Span().EndByte() != pieces[index].Span().StartByte() {
			return false
		}
	}
	return true
}

// iniValueStates renders the ordered value states.
func iniValueStates(doc *ini.Document) []string {
	states := make([]string, 0, len(doc.Entries()))
	for _, entry := range doc.Entries() {
		states = append(states, entry.ValueState().AsStr())
	}
	return states
}

// iniNames renders the ordered section names.
func iniSectionNames(doc *ini.Document) []string {
	names := make([]string, 0, len(doc.Sections()))
	for _, section := range doc.Sections() {
		names = append(names, section.Name())
	}
	return names
}

// iniEntryKeys renders the ordered entry keys.
func iniEntryKeys(doc *ini.Document) []string {
	keys := make([]string, 0, len(doc.Entries()))
	for _, entry := range doc.Entries() {
		keys = append(keys, entry.Key())
	}
	return keys
}

// iniEntryValues renders the ordered entry values.
func iniEntryValues(doc *ini.Document) []string {
	values := make([]string, 0, len(doc.Entries()))
	for _, entry := range doc.Entries() {
		values = append(values, entry.Value())
	}
	return values
}

// iniEntryComparisonKeys renders the ordered comparison keys.
func iniEntryComparisonKeys(doc *ini.Document) []string {
	keys := make([]string, 0, len(doc.Entries()))
	for _, entry := range doc.Entries() {
		keys = append(keys, entry.ComparisonKey())
	}
	return keys
}

// iniSourceEncodingName mirrors the Rust source_encoding_name facts.
func iniSourceEncodingName(encoding document.SourceEncoding) string {
	switch encoding.Kind() {
	case document.EncodingUtf8:
		return "Utf8"
	case document.EncodingUtf16Le:
		return "Utf16Le"
	case document.EncodingUtf16Be:
		return "Utf16Be"
	case document.EncodingLatin1:
		return "Latin1"
	case document.EncodingBinary:
		return "Binary"
	case document.EncodingWindowsCodePage:
		if page, ok := encoding.WindowsCodePage(); ok {
			return "WindowsCodePage(" + itoaUint16(page.Number()) + ")"
		}
		return "WindowsCodePage"
	}
	return "Unknown"
}

// iniQuoteStyleName renders the stable quote style.
func iniQuoteStyleName(style ini.IniQuoteStyle) string { return style.AsStr() }

// iniFidelityName renders the stable fidelity.
func iniFidelityName(fidelity ini.Fidelity) string { return string(fidelity) }

// iniTerminalName renders the stable cursor terminal state.
func iniTerminalName(terminal string) string { return terminal }

func runIniPortableLossless(vector *caseData, report *SuiteReport) {
	doc, message := iniParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	expectedFormation, ok := stringField(vector.Expected, "formation")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.formation"})
		return
	}
	physicalLines, ok := integerField(vector.Expected, "physical_lines")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.physical_lines"})
		return
	}
	logicalLines, ok := integerField(vector.Expected, "logical_lines")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.logical_lines"})
		return
	}
	expectedSections, ok := sequenceField(vector.Expected, "section_names")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.section_names"})
		return
	}
	expectedKeys, ok := sequenceField(vector.Expected, "keys")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.keys"})
		return
	}
	expectedValues, ok := sequenceField(vector.Expected, "values")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.values"})
		return
	}
	expectedStates, ok := sequenceField(vector.Expected, "value_states")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.value_states"})
		return
	}
	exactCoverage, ok := booleanField(vector.Expected, "exact_coverage")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.exact_coverage"})
		return
	}
	if doc.FormationStatus().String() != expectedFormation ||
		uint64(len(doc.PhysicalLines())) != physicalLines ||
		uint64(len(doc.LogicalLines())) != logicalLines ||
		!stringSequenceEqual(iniSectionNames(doc), expectedSections) ||
		!stringSequenceEqual(iniEntryKeys(doc), expectedKeys) ||
		!stringSequenceEqual(iniEntryValues(doc), expectedValues) ||
		!stringSequenceEqual(iniValueStates(doc), expectedStates) ||
		iniExactCoverage(doc) != exactCoverage {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "portable document facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runIniProfileCounterexamples(vector *caseData, report *SuiteReport) {
	samples, ok := sequenceField(vector.Input, "samples")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.samples"})
		return
	}
	profiles := []struct {
		profile ini.IniProfile
		name    string
	}{
		{ini.PortableV1, "portable"},
		{ini.WindowsV1, "windows"},
		{ini.PythonConfigParserV1, "python"},
	}
	for _, entry := range profiles {
		expected, ok := sequenceField(vector.Expected, entry.name)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "missing expected." + entry.name})
			return
		}
		if len(expected) != len(samples) {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "expected." + entry.name + " length differed"})
			return
		}
		for index, sample := range samples {
			sampleObject, ok := sample.(*core.Object)
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "sample must be Object"})
				return
			}
			sourceValue, ok := sampleObject.Get("source")
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "sample.source missing"})
				return
			}
			source, ok := sourceValue.(core.String)
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "sample.source must be String"})
				return
			}
			actual := "Fatal"
			doc, failure := ini.Parse([]byte(source), entry.profile,
				ini.IniEncodingProfileDefault(), ini.DefaultIniParseLimits())
			if failure == nil {
				actual = doc.FormationStatus().String()
			}
			expectedText, ok := expected[index].(core.String)
			if !ok || string(expectedText) != actual {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: entry.name + " counterexample matrix differed"})
				return
			}
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runIniWindowsUtf16(vector *caseData, report *SuiteReport) {
	raw, message := iniHexInput(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	profile, message := iniProfileField(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	doc, failure := ini.Parse(raw, profile, ini.IniEncodingProfileDefault(),
		ini.DefaultIniParseLimits())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "INI formation failed: " + failure.Diagnostics()[0].Code})
		return
	}
	expectedEncoding, ok := stringField(vector.Expected, "encoding")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.encoding"})
		return
	}
	expectedSections, ok := sequenceField(vector.Expected, "section_names")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.section_names"})
		return
	}
	comparisonSection, ok := stringField(vector.Expected, "comparison_section")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.comparison_section"})
		return
	}
	expectedKeys, ok := sequenceField(vector.Expected, "keys")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.keys"})
		return
	}
	comparisonKey, ok := stringField(vector.Expected, "comparison_key")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.comparison_key"})
		return
	}
	expectedValues, ok := sequenceField(vector.Expected, "values")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.values"})
		return
	}
	quoteStyle, ok := stringField(vector.Expected, "quote_style")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.quote_style"})
		return
	}
	caseCollisionCode, ok := stringField(vector.Expected, "case_collision_code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.case_collision_code"})
		return
	}
	exactCoverage, ok := booleanField(vector.Expected, "exact_coverage")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.exact_coverage"})
		return
	}
	sections := doc.Sections()
	entries := doc.Entries()
	hasCode := false
	for _, item := range doc.Diagnostics() {
		if item.Code == caseCollisionCode {
			hasCode = true
			break
		}
	}
	if iniSourceEncodingName(doc.Source().EncodingFacts().Selected()) != expectedEncoding ||
		!stringSequenceEqual(iniSectionNames(doc), expectedSections) ||
		sections[0].ComparisonName() != comparisonSection ||
		!stringSequenceEqual(iniEntryKeys(doc), expectedKeys) ||
		entries[0].ComparisonKey() != comparisonKey ||
		!stringSequenceEqual(iniEntryValues(doc), expectedValues) ||
		iniQuoteStyleName(entries[0].QuoteStyle()) != quoteStyle ||
		!iniSameGroup(sections[0].DuplicateGroup(), sections[1].DuplicateGroup()) ||
		!iniSameGroup(entries[0].DuplicateGroup(), entries[1].DuplicateGroup()) ||
		!hasCode ||
		iniExactCoverage(doc) != exactCoverage {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "Windows UTF-16 facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// iniSameGroup compares two group identities by value.
func iniSameGroup(left, right *uint32) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func runIniWindowsCodePage(vector *caseData, report *SuiteReport) {
	raw, message := iniHexInput(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	number, ok := integerField(vector.Input, "code_page")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.code_page"})
		return
	}
	if number > 65535 {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "code page out of range"})
		return
	}
	codePage, admitted := document.WindowsCodePageFromNumber(uint16(number))
	if !admitted {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "unsupported vector code page"})
		return
	}
	profile, message := iniProfileField(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	doc, failure := ini.Parse(raw, profile,
		ini.IniEncodingExplicit(document.WindowsCodePageEncoding(codePage)),
		ini.DefaultIniParseLimits())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "INI formation failed: " + failure.Diagnostics()[0].Code})
		return
	}
	expectedValue, ok := stringField(vector.Expected, "value")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.value"})
		return
	}
	expectedEncoding, ok := stringField(vector.Expected, "encoding")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.encoding"})
		return
	}
	bomPolicy, ok := stringField(vector.Expected, "bom_policy")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.bom_policy"})
		return
	}
	exactCoverage, ok := booleanField(vector.Expected, "exact_coverage")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.exact_coverage"})
		return
	}
	if doc.Entries()[0].Value() != expectedValue ||
		iniSourceEncodingName(doc.Source().EncodingFacts().Selected()) != expectedEncoding ||
		string(doc.Source().EncodingFacts().BomPolicy()) != bomPolicy ||
		iniExactCoverage(doc) != exactCoverage {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "Windows code-page facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runIniPythonMultiline(vector *caseData, report *SuiteReport) {
	doc, message := iniParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	expectedFormation, ok := stringField(vector.Expected, "formation")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.formation"})
		return
	}
	defaultSection, ok := booleanField(vector.Expected, "default_section")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.default_section"})
		return
	}
	expectedKeys, ok := sequenceField(vector.Expected, "comparison_keys")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.comparison_keys"})
		return
	}
	expectedValues, ok := sequenceField(vector.Expected, "values")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.values"})
		return
	}
	continuationLines, ok := integerField(vector.Expected, "continuation_physical_lines")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.continuation_physical_lines"})
		return
	}
	exactCoverage, ok := booleanField(vector.Expected, "exact_coverage")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.exact_coverage"})
		return
	}
	sections := doc.Sections()
	continued, ok := doc.LogicalLine(doc.Entries()[1].LogicalLine())
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "continued logical line missing"})
		return
	}
	if doc.FormationStatus().String() != expectedFormation ||
		sections[0].IsDefault() != defaultSection ||
		!stringSequenceEqual(iniEntryComparisonKeys(doc), expectedKeys) ||
		!stringSequenceEqual(iniEntryValues(doc), expectedValues) ||
		uint64(len(continued.PhysicalLines())) != continuationLines ||
		iniExactCoverage(doc) != exactCoverage {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "Python raw/default/continuation facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runIniPythonOptionxform(vector *caseData, report *SuiteReport) {
	doc, message := iniParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	expectedFormation, ok := stringField(vector.Expected, "formation")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.formation"})
		return
	}
	expectedKeys, ok := sequenceField(vector.Expected, "comparison_keys")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.comparison_keys"})
		return
	}
	duplicateGroup, ok := booleanField(vector.Expected, "duplicate_group")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.duplicate_group"})
		return
	}
	code, ok := stringField(vector.Expected, "code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.code"})
		return
	}
	entries := doc.Entries()
	hasCode := false
	for _, item := range doc.Diagnostics() {
		if item.Code == code {
			hasCode = true
			break
		}
	}
	if doc.FormationStatus().String() != expectedFormation ||
		!stringSequenceEqual(iniEntryComparisonKeys(doc), expectedKeys) ||
		(entries[0].DuplicateGroup() != nil) != duplicateGroup ||
		!iniSameGroup(entries[0].DuplicateGroup(), entries[1].DuplicateGroup()) ||
		!hasCode {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "Unicode 16 optionxform facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runIniRecoveredAtomic(vector *caseData, report *SuiteReport) {
	doc, message := iniParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	expectedFormation, ok := stringField(vector.Expected, "formation")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.formation"})
		return
	}
	entries, ok := integerField(vector.Expected, "entries")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.entries"})
		return
	}
	errorLines, ok := integerField(vector.Expected, "error_lines")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.error_lines"})
		return
	}
	code, ok := stringField(vector.Expected, "code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.code"})
		return
	}
	projectionCode, ok := stringField(vector.Expected, "projection_code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.projection_code"})
		return
	}
	editCode, ok := stringField(vector.Expected, "edit_code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.edit_code"})
		return
	}
	projected := doc.Project(ini.BestExactEntryMappingV1())
	projectionDiagnostic := ""
	if projected.Failed != nil && len(projected.Failed.Diagnostics) > 0 {
		projectionDiagnostic = projected.Failed.Diagnostics[0].Code
	}
	transaction := ini.NewEditTransactionBuilder(doc).Build()
	editFailure := ""
	if _, failure := doc.Commit(transaction); failure != nil {
		editFailure = failure.Code()
	}
	if doc.FormationStatus().String() != expectedFormation ||
		uint64(len(doc.Entries())) != entries ||
		uint64(len(doc.ErrorLines())) != errorLines ||
		doc.ErrorLines()[0].Code() != code ||
		projectionDiagnostic != projectionCode ||
		editFailure != editCode {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "recovered document exposed partial semantics"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// iniNativeExecutable binds one validated INI native query expression.
func iniNativeExecutable(expression *protocol.QueryExpression) (*protocol.ExecutableQuery, string) {
	definition := protocol.NewQueryDefinition(protocol.DomainININativeV1()).
		WithExpression(expression)
	validated, failure := definition.Validate()
	if failure != nil {
		return nil, "validation: " + failure.Code()
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	bound, failure := validated.Bind(capabilities)
	if failure != nil {
		return nil, "binding: " + failure.Code()
	}
	return bound, ""
}

func runIniNativeQuery(vector *caseData, report *SuiteReport) {
	doc, message := iniParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	sectionName, ok := stringField(vector.Input, "section_name")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.section_name"})
		return
	}
	comparison, ok := stringField(vector.Input, "comparison")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.comparison"})
		return
	}
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("ini.document-sections", 1)).
		Then(protocol.NewOperatorCall("ini.section-name-equals", 1).
			WithArgument("name", core.String(sectionName)).
			WithArgument("comparison", core.String(comparison))).
		Then(protocol.NewOperatorCall("ini.section-entries", 1))
	executable, message := iniNativeExecutable(expression)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	matches, failure := ini.ExecuteIniQuery(iniRunContext, executable, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "query: " + failure.Code()})
		return
	}
	keys := make([]string, 0, len(matches))
	roles := make([]string, 0, len(matches))
	allDuplicated := true
	for index := range matches {
		if matches[index].Kind != ini.IniMatchEntry {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "native query returned non-entry"})
			return
		}
		keys = append(keys, matches[index].Key)
		roles = append(roles, "IniEntry")
		if matches[index].DuplicateGroup == nil {
			allDuplicated = false
		}
	}
	expectedKeys, ok := sequenceField(vector.Expected, "keys")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.keys"})
		return
	}
	expectedRoles, ok := sequenceField(vector.Expected, "roles")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.roles"})
		return
	}
	duplicateGroup, ok := booleanField(vector.Expected, "duplicate_group")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.duplicate_group"})
		return
	}
	terminal, ok := stringField(vector.Expected, "terminal")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.terminal"})
		return
	}
	if !stringSequenceEqual(keys, expectedKeys) ||
		!stringSequenceEqual(roles, expectedRoles) ||
		allDuplicated != duplicateGroup ||
		iniTerminalName("Completed") != terminal {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "native query order or profile comparison differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runIniSyntaxQuery(vector *caseData, report *SuiteReport) {
	doc, message := iniParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	text, ok := stringField(vector.Input, "text")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.text"})
		return
	}
	kind, ok := stringField(vector.Input, "kind")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.kind"})
		return
	}
	textExpression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("ini.syntax-text-equals", 1).
			WithArgument("text", core.String(text)))
	kindExpression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("ini.syntax-kind-is", 1).
			WithArgument("kind", core.String(kind)))
	expression := &protocol.QueryExpression{Kind: protocol.ExpressionStructureOrderMerge,
		Branches: []*protocol.QueryExpression{textExpression, kindExpression}}
	definition := protocol.NewQueryDefinition(protocol.DomainINILosslessSyntaxV1()).
		WithExpression(expression)
	validated, failure := definition.Validate()
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "validation: " + failure.Code()})
		return
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, failure := validated.Bind(capabilities)
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "binding: " + failure.Code()})
		return
	}
	matches, failure := ini.ExecuteIniSyntaxQuery(iniRunContext, executable, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "query: " + failure.Code()})
		return
	}
	expectedKinds, ok := sequenceField(vector.Expected, "kinds")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.kinds"})
		return
	}
	increasing, ok := booleanField(vector.Expected, "strictly_increasing_ordinals")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.strictly_increasing_ordinals"})
		return
	}
	role, ok := stringField(vector.Expected, "role")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.role"})
		return
	}
	kinds := make([]string, 0, len(matches))
	for _, match := range matches {
		kinds = append(kinds, match.Kind().AsStr())
	}
	ordinalsIncreasing := true
	for index := 1; index < len(matches); index++ {
		if matches[index-1].Ordinal() >= matches[index].Ordinal() {
			ordinalsIncreasing = false
			break
		}
	}
	roleMatches := true
	for _, match := range matches {
		if string(match.NodeRef().Role()) != role {
			roleMatches = false
			break
		}
	}
	if !stringSequenceEqual(kinds, expectedKinds) ||
		ordinalsIncreasing != increasing || !roleMatches {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "syntax query decoded ordering differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runIniQueryFailures(vector *caseData, report *SuiteReport) {
	invalid := protocol.NewQueryDefinition(protocol.DomainININativeV1()).
		WithExpression((&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
			Then(protocol.NewOperatorCall("ini.section-name-equals", 1).
				WithArgument("name", core.String("S")).
				WithArgument("comparison", core.String("OriginalExact"))))
	_, invalidFailure := invalid.Validate()
	invalidComposition := invalidFailure != nil &&
		invalidFailure.Kind == protocol.FailureInvalidOperatorComposition
	doc, message := iniParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	executable, message := iniNativeExecutable((&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("ini.all-entries", 1)))
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	maxResults, ok := integerField(vector.Input, "max_results")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.max_results"})
		return
	}
	limits := protocol.DefaultQueryLimits()
	limits.MaxSteps = 100
	limits.MaxResults = int(maxResults)
	limitFailure := ""
	if _, failure := ini.ExecuteIniQuery(iniRunContext, executable, doc, limits); failure != nil {
		limitFailure = failure.Code()
	} else {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "vector requires a query result limit"})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cursor, failure := ini.ExecuteIniQueryCursor(ctx, executable, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "cursor: " + failure.Code()})
		return
	}
	_, first := cursor.Next()
	cancel()
	_, exhausted := cursor.Next()
	terminal := cursor.TerminalState()
	invalidCompositionExpected, ok := booleanField(vector.Expected, "invalid_composition")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.invalid_composition"})
		return
	}
	limitCode, ok := stringField(vector.Expected, "limit_code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.limit_code"})
		return
	}
	firstYielded, ok := booleanField(vector.Expected, "first_yielded")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.first_yielded"})
		return
	}
	terminalExpected, ok := stringField(vector.Expected, "terminal")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.terminal"})
		return
	}
	if invalidComposition != invalidCompositionExpected ||
		limitFailure != limitCode ||
		first != firstYielded ||
		exhausted ||
		terminal != terminalExpected {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "query validation, limit, or cancellation differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runIniProjectionExact(vector *caseData, report *SuiteReport) {
	doc, message := iniParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	projected := doc.Project(ini.BestExactEntryMappingV1())
	if projected.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "exact projection failed"})
		return
	}
	outer, ok := projected.Complete.Value.(*core.EntryMapping)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "outer EntryMapping missing"})
		return
	}
	sectionKeys := make([]string, 0, outer.Len())
	firstEntryKeys := []string{}
	for _, sectionEntry := range outer.Entries() {
		key, ok := sectionEntry.Key.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "section key must be String"})
			return
		}
		sectionKeys = append(sectionKeys, string(key))
	}
	inner, ok := outer.Entries()[0].Value.(*core.EntryMapping)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "inner EntryMapping missing"})
		return
	}
	for _, entry := range inner.Entries() {
		key, ok := entry.Key.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "entry key must be String"})
			return
		}
		firstEntryKeys = append(firstEntryKeys, string(key))
	}
	associationProvenance := false
	for _, item := range projected.Complete.Provenance.Entries() {
		if item.Projected.Kind == "Association" {
			associationProvenance = true
			break
		}
	}
	fidelity, ok := stringField(vector.Expected, "fidelity")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.fidelity"})
		return
	}
	expectedSections, ok := sequenceField(vector.Expected, "section_keys")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.section_keys"})
		return
	}
	expectedEntries, ok := sequenceField(vector.Expected, "first_entry_keys")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.first_entry_keys"})
		return
	}
	events, ok := integerField(vector.Expected, "events")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.events"})
		return
	}
	provenanceExpected, ok := booleanField(vector.Expected, "association_provenance")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.association_provenance"})
		return
	}
	if iniFidelityName(projected.Complete.Fidelity) != fidelity ||
		!stringSequenceEqual(sectionKeys, expectedSections) ||
		!stringSequenceEqual(firstEntryKeys, expectedEntries) ||
		uint64(len(projected.Complete.Report.Events())) != events ||
		associationProvenance != provenanceExpected {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "exact projection facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// iniComparison resolves one vector comparison mode.
func iniComparison(name string) (ini.NameComparison, bool) {
	switch name {
	case "OriginalExact":
		return ini.ComparisonOriginalExact, true
	case "ProfileEquivalent":
		return ini.ComparisonProfileEquivalent, true
	}
	return "", false
}

func runIniProjectionCollapse(vector *caseData, report *SuiteReport) {
	doc, message := iniParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	comparisonName, ok := stringField(vector.Input, "comparison")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.comparison"})
		return
	}
	comparison, ok := iniComparison(comparisonName)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "unknown comparison " + comparisonName})
		return
	}
	rejected := doc.Project(ini.RequireObjectV1(comparison, ini.CollisionPolicyReject)).Failed != nil
	first := doc.Project(ini.RequireObjectV1(comparison, ini.CollisionPolicyFirst))
	last := doc.Project(ini.RequireObjectV1(comparison, ini.CollisionPolicyLast))
	if first.Complete == nil || last.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "explicit collapse failed"})
		return
	}
	firstSection, firstKey, firstValue, ok := iniObjectTriplet(first.Complete.Value)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "first projected Object shape differed"})
		return
	}
	lastSection, lastKey, lastValue, ok := iniObjectTriplet(last.Complete.Value)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "last projected Object shape differed"})
		return
	}
	collapsedProvenance := false
	for _, item := range first.Complete.Provenance.Entries() {
		for _, origin := range item.Origins {
			if origin.Relation == ini.RelationCollapsed {
				collapsedProvenance = true
				break
			}
		}
	}
	rejectsExpected, ok := booleanField(vector.Expected, "rejects")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.rejects"})
		return
	}
	firstFidelity, ok := stringField(vector.Expected, "first_fidelity")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.first_fidelity"})
		return
	}
	firstEvents, ok := integerField(vector.Expected, "first_events")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.first_events"})
		return
	}
	firstSectionExpected, ok := stringField(vector.Expected, "first_section")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.first_section"})
		return
	}
	firstKeyExpected, ok := stringField(vector.Expected, "first_key")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.first_key"})
		return
	}
	firstValueExpected, ok := stringField(vector.Expected, "first_value")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.first_value"})
		return
	}
	lastSectionExpected, ok := stringField(vector.Expected, "last_section")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.last_section"})
		return
	}
	lastKeyExpected, ok := stringField(vector.Expected, "last_key")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.last_key"})
		return
	}
	lastValueExpected, ok := stringField(vector.Expected, "last_value")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.last_value"})
		return
	}
	collapsedExpected, ok := booleanField(vector.Expected, "collapsed_provenance")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.collapsed_provenance"})
		return
	}
	if rejected != rejectsExpected ||
		iniFidelityName(first.Complete.Fidelity) != firstFidelity ||
		uint64(len(first.Complete.Report.Events())) != firstEvents ||
		firstSection != firstSectionExpected || firstKey != firstKeyExpected ||
		firstValue != firstValueExpected ||
		lastSection != lastSectionExpected || lastKey != lastKeyExpected ||
		lastValue != lastValueExpected ||
		collapsedProvenance != collapsedExpected {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "explicit Object collapse facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// iniObjectTriplet reads the first section/entry/value of one projected
// Object.
func iniObjectTriplet(value core.Value) (string, string, string, bool) {
	sections, ok := value.(*core.Object)
	if !ok || sections.Len() == 0 {
		return "", "", "", false
	}
	section := sections.Entries()[0]
	entries, ok := section.Value.(*core.Object)
	if !ok || entries.Len() == 0 {
		return "", "", "", false
	}
	entry := entries.Entries()[0]
	text, ok := entry.Value.(core.String)
	if !ok {
		return "", "", "", false
	}
	return section.Key, entry.Key, string(text), true
}

func runIniProjectionFragments(vector *caseData, report *SuiteReport) {
	pythonSource, ok := stringField(vector.Input, "python_source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.python_source"})
		return
	}
	windowsSource, ok := stringField(vector.Input, "windows_source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.windows_source"})
		return
	}
	python, message := iniParseText(ini.PythonConfigParserV1, pythonSource)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	windows, message := iniParseText(ini.WindowsV1, windowsSource)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	pythonProjected := python.Project(ini.BestExactEntryMappingV1())
	windowsProjected := windows.Project(ini.BestExactEntryMappingV1())
	if pythonProjected.Complete == nil || windowsProjected.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "fragment projection failed"})
		return
	}
	continuation, ok := stringField(vector.Expected, "continuation_relation")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.continuation_relation"})
		return
	}
	quote, ok := stringField(vector.Expected, "quote_relation")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.quote_relation"})
		return
	}
	if !iniRelationPresent(pythonProjected.Complete.Provenance, ini.RelationContinuationFragment) ||
		!iniRelationPresent(windowsProjected.Complete.Provenance, ini.RelationQuoteDerived) ||
		continuation != "ContinuationFragment" || quote != "QuoteDerived" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "fragmented provenance was incomplete"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// iniRelationPresent reports whether one provenance relation occurs.
func iniRelationPresent(provenance ini.ProvenanceMap, relation ini.ProvenanceRelation) bool {
	for _, entry := range provenance.Entries() {
		for _, origin := range entry.Origins {
			if origin.Relation == relation {
				return true
			}
		}
	}
	return false
}

// iniMaterializationRequest builds one frozen vector materialization
// request.
func iniMaterializationRequest(profile ini.IniProfile) document.MaterializationRequest {
	switch {
	case profile == ini.PortableV1:
		return document.NewMaterializationRequest(
			document.NewProfileId("ini.portable", 1),
			document.NewMaterializationStyleId("ini.portable-canonical", 1))
	case profile == ini.WindowsV1:
		return document.NewMaterializationRequest(
			document.NewProfileId("ini.windows", 1),
			document.NewMaterializationStyleId("ini.windows-canonical", 1)).
			WithEncoding(document.Utf16LeEncoding()).
			WithNewline(document.NewlineCrLf)
	default:
		return document.NewMaterializationRequest(
			document.NewProfileId("ini.python-configparser", 1),
			document.NewMaterializationStyleId("ini.python-configparser-canonical", 1))
	}
}

// iniNestedMapping builds one nested EntryMapping from the vector
// descriptor.
func iniNestedMapping(descriptor core.Value) (core.Value, string) {
	sections, ok := descriptor.(*core.Array)
	if !ok {
		return nil, "mapping descriptor must be Sequence"
	}
	outer := core.NewEntryMappingBuilder()
	for _, section := range sections.Items() {
		fields, ok := section.(*core.Object)
		if !ok {
			return nil, "section descriptor must be Object"
		}
		nameValue, ok := fields.Get("section")
		if !ok {
			return nil, "section.section missing"
		}
		name, ok := nameValue.(core.String)
		if !ok {
			return nil, "section.section must be String"
		}
		entriesValue, ok := fields.Get("entries")
		if !ok {
			return nil, "section.entries missing"
		}
		entries, ok := entriesValue.(*core.Array)
		if !ok {
			return nil, "section.entries must be Sequence"
		}
		inner := core.NewEntryMappingBuilder()
		for _, entry := range entries.Items() {
			pair, ok := entry.(*core.Array)
			if !ok || pair.Len() != 2 {
				return nil, "entry descriptor must contain key and value"
			}
			key, ok := pair.At(0).(core.String)
			if !ok {
				return nil, "entry key must be String"
			}
			value, ok := pair.At(1).(core.String)
			if !ok {
				return nil, "entry value must be String"
			}
			_ = inner.Push(key, value)
		}
		_ = outer.Push(name, inner.Build())
	}
	return outer.Build(), ""
}

func runIniMaterializationStyles(vector *caseData, report *SuiteReport) {
	profiles := []struct {
		field    string
		profile  ini.IniProfile
		expected string
		encoding document.SourceEncoding
	}{
		{"portable", ini.PortableV1, "", document.Utf8Encoding()},
		{"windows", ini.WindowsV1, "", document.Utf16LeEncoding()},
		{"python", ini.PythonConfigParserV1, "", document.Utf8Encoding()},
	}
	for index := range profiles {
		entry := &profiles[index]
		var ok bool
		// The Windows and Python canonical expectations are the decoded
		// text (the vector publishes windows_decoded and python_decoded,
		// not source spellings).
		field := entry.field + "_source"
		if entry.field == "windows" || entry.field == "python" {
			field = entry.field + "_decoded"
		}
		entry.expected, ok = stringField(vector.Expected, field)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "missing expected." + field})
			return
		}
	}
	exactFidelity, ok := booleanField(vector.Expected, "exact_fidelity")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.exact_fidelity"})
		return
	}
	closureExpected, ok := booleanField(vector.Expected, "closure")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.closure"})
		return
	}
	for _, entry := range profiles {
		input, ok := caseInput(vector, entry.field)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "missing input." + entry.field})
			return
		}
		value, message := iniNestedMapping(input)
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		result := ini.Materialize(value, iniMaterializationRequest(entry.profile))
		if result.Complete == nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: entry.field + " materialization failed: " + result.Failed.Failure.Code()})
			return
		}
		decoded, _ := result.Complete.Document.Source().DecodedText()
		projected := result.Complete.Document.Project(ini.BestExactEntryMappingV1())
		closure := projected.Complete != nil && core.Equal(projected.Complete.Value, value)
		if decoded != entry.expected ||
			!result.Complete.Document.Source().EncodingFacts().Selected().Equal(entry.encoding) ||
			(result.Complete.Fidelity == ini.MaterializationFidelityExact) != exactFidelity ||
			closure != closureExpected {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: entry.field + " canonical materialization differed"})
			return
		}
	}
	windowsEncoding, ok := stringField(vector.Expected, "windows_encoding")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.windows_encoding"})
		return
	}
	if windowsEncoding != "Utf16Le" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "Windows encoding expectation is not canonical"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runIniMaterializationLimits(vector *caseData, report *SuiteReport) {
	scalarResult := ini.Materialize(core.String("x"), iniMaterializationRequest(ini.PortableV1))
	if scalarResult.Complete != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "scalar materialized"})
		return
	}
	scalarCode := scalarResult.Failed.Failure.Code()
	valueInput, ok := caseInput(vector, "value")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.value"})
		return
	}
	value, message := iniNestedMapping(valueInput)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	names, ok := sequenceField(vector.Input, "limit_names")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.limit_names"})
		return
	}
	expected, ok := sequenceField(vector.Expected, "limit_outcomes")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.limit_outcomes"})
		return
	}
	if len(names) != len(expected) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "materialization limit vector lengths differ"})
		return
	}
	limitCode, ok := stringField(vector.Expected, "limit_code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.limit_code"})
		return
	}
	scalarCodeExpected, ok := stringField(vector.Expected, "scalar_code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.scalar_code"})
		return
	}
	var outcomes []string
	for _, nameValue := range names {
		name, ok := nameValue.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "limit_names must be String"})
			return
		}
		limits := document.DefaultMaterializationLimits()
		switch string(name) {
		case "max_input_nodes":
			limits.MaxInputNodes = 1
		case "max_output_bytes":
			limits.MaxOutputBytes = 2
		case "max_depth":
			limits.MaxDepth = 0
		case "max_report_entries":
			limits.MaxReportEntries = 0
		case "max_provenance_entries":
			limits.MaxProvenanceEntries = 1
		default:
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "unknown materialization limit " + string(name)})
			return
		}
		result := ini.Materialize(value, iniMaterializationRequest(ini.PortableV1).
			WithLimits(limits))
		if result.Complete != nil {
			outcomes = append(outcomes, "Complete")
			continue
		}
		if result.Failed.Failure.Code() != limitCode {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: string(name) + " returned wrong failure code"})
			return
		}
		outcomes = append(outcomes, "Failed")
	}
	expectedOutcomes := make([]string, 0, len(expected))
	for _, item := range expected {
		text, ok := item.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "limit_outcomes must be String"})
			return
		}
		expectedOutcomes = append(expectedOutcomes, string(text))
	}
	if scalarCode != scalarCodeExpected || !stringSliceEqual(outcomes, expectedOutcomes) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "materialization atomic limit outcomes differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// iniCollectEdit commits one transaction and records the output and edit
// count.
func iniCollectEdit(doc *ini.Document, builder *ini.EditTransactionBuilder,
	outputs *[]string, editCounts *[]int) string {
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		return "edit failed: " + failure.Code()
	}
	*outputs = append(*outputs, string(commit.Document.Render()))
	*editCounts = append(*editCounts, len(commit.ChangeSet.SourceEdits()))
	return ""
}

func runIniEditAllOperations(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.source"})
		return
	}
	profile, message := iniProfileField(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	expected, ok := sequenceField(vector.Expected, "outputs")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.outputs"})
		return
	}
	semanticValue, ok := stringField(vector.Input, "semantic_value")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.semantic_value"})
		return
	}
	literalValue, ok := stringField(vector.Input, "literal_value")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.literal_value"})
		return
	}
	newSection, ok := stringField(vector.Input, "new_section")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.new_section"})
		return
	}
	renamedSection, ok := stringField(vector.Input, "renamed_section")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.renamed_section"})
		return
	}
	newKey, ok := stringField(vector.Input, "new_key")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.new_key"})
		return
	}
	newValue, ok := stringField(vector.Input, "new_value")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.new_value"})
		return
	}
	renamedKey, ok := stringField(vector.Input, "renamed_key")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.renamed_key"})
		return
	}
	var outputs []string
	var editCounts []int

	doc, message := iniParseText(profile, source)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	builder := ini.NewEditTransactionBuilder(doc)
	builder.SemanticValue(doc.Entries()[0].NodeRef(), semanticValue,
		ini.RepresentationPolicyCanonicalForProfile)
	if message := iniCollectEdit(doc, builder, &outputs, &editCounts); message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}

	doc, message = iniParseText(profile, source)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	builder = ini.NewEditTransactionBuilder(doc)
	builder.LiteralValue(doc.Entries()[0].NodeRef(), []byte(literalValue))
	if message := iniCollectEdit(doc, builder, &outputs, &editCounts); message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}

	doc, message = iniParseText(profile, source)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	builder = ini.NewEditTransactionBuilder(doc)
	builder.InsertSection(doc.NodeRef(), newSection, ini.PlacementEnd())
	if message := iniCollectEdit(doc, builder, &outputs, &editCounts); message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}

	doc, message = iniParseText(profile, source)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	builder = ini.NewEditTransactionBuilder(doc)
	builder.RemoveSection(doc.Sections()[0].NodeRef())
	if message := iniCollectEdit(doc, builder, &outputs, &editCounts); message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}

	doc, message = iniParseText(profile, source)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	builder = ini.NewEditTransactionBuilder(doc)
	builder.RenameSection(doc.Sections()[0].NodeRef(), renamedSection)
	if message := iniCollectEdit(doc, builder, &outputs, &editCounts); message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}

	doc, message = iniParseText(profile, source)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	builder = ini.NewEditTransactionBuilder(doc)
	builder.InsertEntry(doc.Sections()[0].NodeRef(), newKey, newValue, ini.PlacementEnd())
	if message := iniCollectEdit(doc, builder, &outputs, &editCounts); message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}

	doc, message = iniParseText(profile, source)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	builder = ini.NewEditTransactionBuilder(doc)
	builder.RemoveEntry(doc.Entries()[0].NodeRef())
	if message := iniCollectEdit(doc, builder, &outputs, &editCounts); message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}

	doc, message = iniParseText(profile, source)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	builder = ini.NewEditTransactionBuilder(doc)
	builder.RenameEntry(doc.Entries()[0].NodeRef(), renamedKey)
	if message := iniCollectEdit(doc, builder, &outputs, &editCounts); message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}

	expectedOutputs := make([]string, 0, len(expected))
	for _, item := range expected {
		text, ok := item.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "expected.outputs must be String"})
			return
		}
		expectedOutputs = append(expectedOutputs, string(text))
	}
	oneEditEach, ok := booleanField(vector.Expected, "one_source_edit_each")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.one_source_edit_each"})
		return
	}
	allSingle := len(editCounts) == len(outputs)
	for _, count := range editCounts {
		if count != 1 {
			allSingle = false
			break
		}
	}
	if !stringSliceEqual(outputs, expectedOutputs) || allSingle != oneEditEach {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "eight edit outputs differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runIniEditAuditArtifacts(vector *caseData, report *SuiteReport) {
	doc, message := iniParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	value, ok := stringField(vector.Input, "value")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.value"})
		return
	}
	sourceID, ok := stringField(vector.Input, "source_id")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.source_id"})
		return
	}
	wrongSource, ok := stringField(vector.Input, "wrong_source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.wrong_source"})
		return
	}
	builder := ini.NewEditTransactionBuilder(doc)
	builder.SemanticValue(doc.Entries()[0].NodeRef(), value,
		ini.RepresentationPolicyCanonicalForProfile)
	transaction := builder.Build()
	plan, failure := doc.DryRun(transaction, sourceID)
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "dry run: " + failure.Code()})
		return
	}
	commit, failure := doc.Commit(transaction)
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "commit: " + failure.Code()})
		return
	}
	replayed, err := commit.SourcePatch.Apply(doc.Source(), document.DefaultSourcePatchLimits())
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "patch replay failed"})
		return
	}
	proofErr := commit.UntouchedProof.Verify(doc.Source(), commit.Document.Source(),
		commit.SourcePatch.Replacements())

	other, message := iniParseText(iniProfileFromCase(vector), wrongSource)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	wrong := ini.NewEditTransactionBuilder(doc)
	wrong.LiteralValue(other.Entries()[0].NodeRef(), []byte("new"))
	_, wrongFailure := doc.Commit(wrong.Build())
	if wrongFailure == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "wrong snapshot must fail"})
		return
	}
	expectedSource, ok := stringField(vector.Expected, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.source"})
		return
	}
	dryRunEquals, ok := booleanField(vector.Expected, "dry_run_equals_commit")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.dry_run_equals_commit"})
		return
	}
	patchReplays, ok := booleanField(vector.Expected, "patch_replays")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.patch_replays"})
		return
	}
	proofVerifies, ok := booleanField(vector.Expected, "proof_verifies")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.proof_verifies"})
		return
	}
	wrongSnapshotCode, ok := stringField(vector.Expected, "wrong_snapshot_code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.wrong_snapshot_code"})
		return
	}
	baseUnchanged, ok := booleanField(vector.Expected, "base_unchanged")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.base_unchanged"})
		return
	}
	sourceText, _ := stringField(vector.Input, "source")
	if string(commit.Document.Render()) != expectedSource ||
		(plan.SourcePatch().TargetDigest().Equal(commit.SourcePatch.TargetDigest()) &&
			plan.SourcePatch().BaseDigest().Equal(commit.SourcePatch.BaseDigest())) != dryRunEquals ||
		(string(replayed.Bytes()) == string(commit.Document.Render())) != patchReplays ||
		(proofErr == nil) != proofVerifies ||
		wrongFailure.Code() != wrongSnapshotCode ||
		(string(doc.Render()) == sourceText) != baseUnchanged {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "edit plan, patch, proof, or atomic failure differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// iniProfileFromCase resolves the profile for an input that carries one.
func iniProfileFromCase(vector *caseData) ini.IniProfile {
	profile, _ := iniProfileField(vector)
	return profile
}

// iniSetParseLimit applies one named limit to the parse limits.
func iniSetParseLimit(limits *ini.IniParseLimits, name string, value uint64) bool {
	switch name {
	case "max_source_bytes":
		limits.Common.MaxSourceBytes = int(value)
	case "max_nesting_depth":
		limits.Common.MaxNestingDepth = int(value)
	case "max_token_count":
		limits.Common.MaxTokenCount = int(value)
	case "max_node_count":
		limits.Common.MaxNodeCount = int(value)
	case "max_diagnostics":
		limits.Common.MaxDiagnostics = int(value)
	case "max_decoded_utf8_bytes":
		limits.MaxDecodedUTF8Bytes = int(value)
	case "max_decoded_scalars":
		limits.MaxDecodedScalars = int(value)
	case "max_physical_lines":
		limits.MaxPhysicalLines = int(value)
	case "max_physical_line_bytes":
		limits.MaxPhysicalLineBytes = int(value)
	case "max_physical_line_scalars":
		limits.MaxPhysicalLineScalars = int(value)
	case "max_logical_lines":
		limits.MaxLogicalLines = int(value)
	case "max_logical_line_bytes":
		limits.MaxLogicalLineBytes = int(value)
	case "max_logical_line_scalars":
		limits.MaxLogicalLineScalars = int(value)
	case "max_continuation_lines":
		limits.MaxContinuationLines = int(value)
	case "max_sections":
		limits.MaxSections = int(value)
	case "max_entries":
		limits.MaxEntries = int(value)
	case "max_duplicate_group_members":
		limits.MaxDuplicateGroupMembers = int(value)
	case "max_recovery_regions":
		limits.MaxRecoveryRegions = int(value)
	default:
		return false
	}
	return true
}

// iniProfileName resolves one vector profile spelling.
func iniProfileName(name string) (ini.IniProfile, bool) {
	switch name {
	case "ini.portable@1":
		return ini.PortableV1, true
	case "ini.windows@1":
		return ini.WindowsV1, true
	case "ini.python-configparser@1":
		return ini.PythonConfigParserV1, true
	}
	return ini.IniProfile{}, false
}

func runIniFormationLimits(vector *caseData, report *SuiteReport) {
	descriptors, ok := sequenceField(vector.Input, "limits")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.limits"})
		return
	}
	fatal := 0
	for _, descriptor := range descriptors {
		fields, ok := descriptor.(*core.Object)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "limit descriptor must be Object"})
			return
		}
		nameValue, ok := fields.Get("name")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "limit descriptor.name missing"})
			return
		}
		name, ok := nameValue.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "limit descriptor.name must be String"})
			return
		}
		profileValue, ok := fields.Get("profile")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "limit descriptor.profile missing"})
			return
		}
		profileText, ok := profileValue.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "limit descriptor.profile must be String"})
			return
		}
		profile, ok := iniProfileName(string(profileText))
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "unknown INI profile " + string(profileText)})
			return
		}
		sourceValue, ok := fields.Get("source")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "limit descriptor.source missing"})
			return
		}
		source, ok := sourceValue.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "limit descriptor.source must be String"})
			return
		}
		valueValue, ok := fields.Get("value")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "limit descriptor.value missing"})
			return
		}
		value, ok := valueValue.(core.Integer)
		if !ok || value.Int().Sign() < 0 {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "limit descriptor.value must be a non-negative Integer"})
			return
		}
		limits := ini.DefaultIniParseLimits()
		if !iniSetParseLimit(&limits, string(name), value.Int().Uint64()) {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "unknown INI parse limit " + string(name)})
			return
		}
		_, failure := ini.Parse([]byte(source), profile, ini.IniEncodingProfileDefault(), limits)
		if failure != nil {
			fatal++
		}
	}
	fatalCount, ok := integerField(vector.Expected, "fatal_count")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.fatal_count"})
		return
	}
	noPartialDocuments, ok := booleanField(vector.Expected, "no_partial_documents")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.no_partial_documents"})
		return
	}
	if uint64(fatal) != fatalCount || !noPartialDocuments {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "formation limit outcomes differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runIniProjectionLimits(vector *caseData, report *SuiteReport) {
	doc, message := iniParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	names, ok := sequenceField(vector.Input, "limits")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.limits"})
		return
	}
	code, ok := stringField(vector.Expected, "code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.code"})
		return
	}
	failedCount := 0
	for _, nameValue := range names {
		name, ok := nameValue.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "limits must be String"})
			return
		}
		limits := ini.DefaultProjectionLimits()
		switch string(name) {
		case "max_source_associations":
			limits.MaxSourceAssociations = 1
		case "max_value_nodes":
			limits.MaxValueNodes = 1
		case "max_provenance_units":
			limits.MaxProvenanceUnits = 1
		default:
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "unknown projection limit " + string(name)})
			return
		}
		projected := doc.Project(ini.BestExactEntryMappingV1().WithLimits(limits))
		if projected.Failed == nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "projection limit " + string(name) + " did not fail"})
			return
		}
		if len(projected.Failed.Diagnostics) > 0 &&
			projected.Failed.Diagnostics[0].Code == code {
			failedCount++
		}
	}
	expectedCount, ok := integerField(vector.Expected, "failed_count")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.failed_count"})
		return
	}
	if uint64(failedCount) != expectedCount {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "projection failed count differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runIniOperationRegistry(vector *caseData, report *SuiteReport) {
	expected, ok := sequenceField(vector.Expected, "operations")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.operations"})
		return
	}
	directStructural, ok := integerField(vector.Expected, "direct_structural")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.direct_structural"})
		return
	}
	profiles, ok := sequenceField(vector.Input, "profiles")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.profiles"})
		return
	}
	for _, profileValue := range profiles {
		profileText, ok := profileValue.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "profiles must be String"})
			return
		}
		profile, ok := iniProfileName(string(profileText))
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "unknown INI profile " + string(profileText)})
			return
		}
		registry := ini.NewFormatOperationRegistry(profile)
		operations := make([]string, 0, len(registry.Operations()))
		direct := 0
		for _, descriptor := range registry.Operations() {
			operations = append(operations, descriptor.ID.ID()+"@"+itoaUint32(descriptor.ID.Version()))
			if descriptor.Support == ini.OperationSupportSupported {
				direct++
			}
		}
		expectedOperations := make([]string, 0, len(expected))
		for _, item := range expected {
			text, ok := item.(core.String)
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "expected.operations must be String"})
				return
			}
			expectedOperations = append(expectedOperations, string(text))
		}
		if !stringSliceEqual(operations, expectedOperations) ||
			uint64(direct) != directStructural {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "operation registry differed for " + string(profileText)})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

// iniHexInput decodes one vector source_hex input.
func iniHexInput(vector *caseData) ([]byte, string) {
	text, ok := stringField(vector.Input, "source_hex")
	if !ok {
		return nil, "missing input.source_hex"
	}
	raw, err := hex.DecodeString(text)
	if err != nil {
		return nil, "invalid source hex"
	}
	return raw, ""
}

// stringSequenceEqual compares one string slice to a vector Sequence.
func stringSequenceEqual(actual []string, expected []core.Value) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		text, ok := expected[index].(core.String)
		if !ok || string(text) != actual[index] {
			return false
		}
	}
	return true
}

// stringSliceEqual compares two string slices.
func stringSliceEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// itoaUint16 renders one unsigned 16-bit integer.
func itoaUint16(value uint16) string { return itoaUint64(uint64(value)) }

// itoaUint32 renders one unsigned 32-bit integer.
func itoaUint32(value uint32) string { return itoaUint64(uint64(value)) }

// itoaUint64 renders one unsigned integer.
func itoaUint64(value uint64) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

// iniRunContext is the runner cancellation context shared with the other
// suite runners (contextBackground in toml_v1.go).
var iniRunContext = context.Background()
