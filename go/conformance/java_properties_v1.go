package conformance

// The `consema.java-properties.conformance@1` suite runner
// (crates/consema-conformance/src/properties_v1.rs). The 0.16.0 milestone
// (G2.3) implements the full Java Properties surface: both exact profiles
// with explicit encoding selection, lossless natural/logical lines, exact
// Java UTF-16 strings with unpaired-surrogate preservation, recovery with
// stable diagnostics, native and lossless-syntax query domains, best-exact
// EntryMapping and explicit unique-Object projection, canonical
// Reader/Latin-1 materialization with exact closure, the five frozen
// structural edits with atomic patch/proof artifacts, and the resource
// limit matrices. All 22 published cases are executed; the vector files
// are the authority and the runner embeds no expectation literals.

import (
	"context"
	"encoding/hex"
	"math/big"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/properties"
	"consema.dev/consema/protocol"
)

// runJavaPropertiesV1 executes the embedded `consema.java-properties.conformance@1`
// suite.
func runJavaPropertiesV1(runner *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		switch vector.ID {
		case "formation.reader-lines-escapes-duplicates":
			runPropertiesFormationReader(vector, report)
		case "formation.empty-blank-comment-empty-key":
			runPropertiesFormationBasicMatrix(vector, report)
		case "formation.mixed-line-terminators":
			runPropertiesFormationTerminators(vector, report)
		case "formation.continuation-and-backslash-parity":
			runPropertiesFormationContinuations(vector, report)
		case "formation.escape-and-java-utf16-matrix":
			runPropertiesFormationJavaStrings(vector, report)
		case "formation.malformed-unicode-recovery-matrix":
			runPropertiesFormationRecoveryMatrix(vector, report)
		case "formation.reader-explicit-encodings":
			runPropertiesFormationReaderEncodings(vector, report)
		case "formation.latin1-byte-and-bom-content":
			runPropertiesFormationLatin1(vector, report)
		case "formation.recovery-never-publishes-partial-operation":
			runPropertiesRecoveredIsAtomic(vector, report)
		case "formation.malformed-escape-in-key":
			runPropertiesMalformedEscapeInKey(vector, report)
		case "formation.invalid-encoding-sequence", "formation.bom-conflict":
			runPropertiesFatalEncoding(vector, report)
		case "query.native-duplicates-and-escape-ownership":
			runPropertiesNativeQuery(vector, report)
		case "query.logical-and-syntax-order":
			runPropertiesLogicalSyntaxQuery(vector, report)
		case "query.validation-limit-cancellation":
			runPropertiesQueryFailures(vector, report)
		case "projection.exact-duplicates-and-fragments":
			runPropertiesProjectionExact(vector, report)
		case "projection.unpaired-and-recovered-atomic-failure":
			runPropertiesProjectionFailures(vector, report)
		case "projection.explicit-jdk-table-collapse":
			runPropertiesProjectionCollapse(vector, report)
		case "materialization.canonical-styles-encodings-and-closure":
			runPropertiesMaterializationStyles(vector, report)
		case "materialization.atomic-failures-and-limits":
			runPropertiesMaterializationLimits(vector, report)
		case "edit.all-five-operations":
			runPropertiesEditAllOperations(vector, report)
		case "edit.dry-run-patch-proof-conflict-atomicity":
			runPropertiesEditAuditArtifacts(vector, report)
		case "resource.formation-limit-matrix":
			runPropertiesFormationLimits(vector, report)
		case "resource.projection-limit-matrix":
			runPropertiesProjectionLimits(vector, report)
		case "registry.frozen-five-operation-surface":
			runPropertiesOperationRegistry(vector, report)
		default:
			report.Failed = append(report.Failed, CaseFailure{
				ID:      vector.ID,
				Message: "runner does not recognize published Java Properties case",
			})
		}
	}
	return report
}

// propertiesProfile resolves one vector profile identifier.
func propertiesProfile(vector *caseData) (properties.PropertiesProfile, string) {
	profile, ok := stringField(vector.Input, "profile")
	if !ok {
		return properties.PropertiesProfile{}, "missing input.profile"
	}
	switch profile {
	case "java-properties.reader@1":
		return properties.PropertiesReaderV1, ""
	case "java-properties.latin1@1":
		return properties.PropertiesLatin1V1, ""
	}
	return properties.PropertiesProfile{}, "unknown Java Properties profile " + profile
}

// propertiesSourceEncoding resolves one vector source-encoding name.
func propertiesSourceEncoding(name string) (document.SourceEncoding, bool) {
	switch name {
	case "Utf8":
		return document.Utf8Encoding(), true
	case "Utf16Le":
		return document.Utf16LeEncoding(), true
	case "Utf16Be":
		return document.Utf16BeEncoding(), true
	case "Latin1":
		return document.Latin1Encoding(), true
	}
	if strings.HasPrefix(name, "WindowsCodePage(") && strings.HasSuffix(name, ")") {
		number, ok := parseU16(name[len("WindowsCodePage(") : len(name)-1])
		if !ok {
			return document.SourceEncoding{}, false
		}
		page, ok := document.WindowsCodePageFromNumber(number)
		if !ok {
			return document.SourceEncoding{}, false
		}
		return document.WindowsCodePageEncoding(page), true
	}
	return document.SourceEncoding{}, false
}

func parseU16(text string) (uint16, bool) {
	if text == "" || len(text) > 5 {
		return 0, false
	}
	var value uint16
	for index := 0; index < len(text); index++ {
		if text[index] < '0' || text[index] > '9' {
			return 0, false
		}
		value = value*10 + uint16(text[index]-'0')
	}
	return value, true
}

// propertiesParseCase forms one vector source under the vector profile.
func propertiesParseCase(vector *caseData) (*properties.Document, string) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		return nil, "missing input.source"
	}
	profile, message := propertiesProfile(vector)
	if message != "" {
		return nil, message
	}
	if profile == properties.PropertiesLatin1V1 {
		document, failure := properties.ParseLatin1([]byte(source),
			properties.DefaultPropertiesParseLimits())
		if failure != nil {
			return nil, "Properties formation failed: " + failure.Code()
		}
		return document, ""
	}
	return propertiesParseReaderText(source)
}

// propertiesParseReaderText parses one vector source as Reader UTF-8.
func propertiesParseReaderText(source string) (*properties.Document, string) {
	document, failure := properties.ParseReader([]byte(source), document.Utf8Encoding(),
		properties.DefaultPropertiesParseLimits())
	if failure != nil {
		return nil, "Properties formation failed: " + failure.Code()
	}
	return document, ""
}

// propertiesExecutableNative builds one validated native-domain executable.
func propertiesExecutableNative(expression *protocol.QueryExpression) (*protocol.ExecutableQuery, string) {
	definition := protocol.NewQueryDefinition(protocol.DomainJavaPropertiesNativeV1()).
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

// propertiesExecutableSyntax builds one validated syntax-domain executable.
func propertiesExecutableSyntax(expression *protocol.QueryExpression) (*protocol.ExecutableQuery, string) {
	definition := protocol.NewQueryDefinition(protocol.DomainJavaPropertiesLosslessSyntaxV1()).
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

func runPropertiesFormationReader(vector *caseData, report *SuiteReport) {
	doc, message := propertiesParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	formation, ok := stringField(vector.Expected, "formation")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.formation"})
		return
	}
	if doc.FormationStatus().String() != formation ||
		uint64(len(doc.NaturalLines())) != expectedInt(vector, report, "natural_lines") ||
		uint64(len(doc.LogicalLines())) != expectedInt(vector, report, "logical_lines") ||
		uint64(len(doc.Comments())) != expectedInt(vector, report, "comments") ||
		uint64(len(doc.Properties())) != expectedInt(vector, report, "properties") ||
		uint64(len(doc.Escapes())) != expectedInt(vector, report, "escapes") {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "Reader formation counts differed"})
		return
	}
	keys, ok := propertiesUnicodeKeys(doc)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "property key is not well-formed Unicode"})
		return
	}
	values, ok := propertiesUnicodeValues(doc)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "property value is not well-formed Unicode"})
		return
	}
	states := make([]string, 0, len(doc.Properties()))
	for _, property := range doc.Properties() {
		states = append(states, string(property.ValueState()))
	}
	duplicateGroup := false
	if len(doc.Properties()) > 2 {
		first := doc.Properties()[1].DuplicateGroup()
		second := doc.Properties()[2].DuplicateGroup()
		duplicateGroup = first != nil && second != nil && *first == *second
	}
	coverage, ok := propertiesExactCoverage(doc)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "syntax coverage facts are inconsistent"})
		return
	}
	if !stringSlicesEqual(keys, expectedStrings(vector, report, "keys")) ||
		!stringSlicesEqual(values, expectedStrings(vector, report, "values")) ||
		!stringSlicesEqual(states, expectedStrings(vector, report, "states")) ||
		duplicateGroup != expectedBool(vector, report, "duplicate_group") ||
		coverage != expectedBool(vector, report, "exact_coverage") {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "Reader formation facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPropertiesFormationBasicMatrix(vector *caseData, report *SuiteReport) {
	samples, ok := sequenceField(vector.Input, "samples")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.samples"})
		return
	}
	formations, ok := sequenceField(vector.Expected, "formations")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.formations"})
		return
	}
	propertiesCounts, ok := sequenceField(vector.Expected, "properties")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.properties"})
		return
	}
	comments, ok := sequenceField(vector.Expected, "comments")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.comments"})
		return
	}
	if len(samples) != len(formations) || len(samples) != len(propertiesCounts) ||
		len(samples) != len(comments) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "basic formation vector lengths differ"})
		return
	}
	for index := range samples {
		source, ok := samples[index].(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "sample must be String"})
			return
		}
		doc, message := propertiesParseReaderText(string(source))
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "basic formation sample " + itoaYaml(index) + ": " + message})
			return
		}
		expectedFormation, ok := formations[index].(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "formations must be String"})
			return
		}
		expectedProperties, ok := propertiesCounts[index].(core.Integer)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "properties counts must be Integer"})
			return
		}
		expectedComments, ok := comments[index].(core.Integer)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "comments counts must be Integer"})
			return
		}
		coverage, _ := propertiesExactCoverage(doc)
		if doc.FormationStatus().String() != string(expectedFormation) ||
			uint64(len(doc.Properties())) != expectedProperties.Int().Uint64() ||
			uint64(len(doc.Comments())) != expectedComments.Int().Uint64() ||
			!coverage {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "basic formation sample " + itoaYaml(index) + " differed"})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPropertiesFormationTerminators(vector *caseData, report *SuiteReport) {
	doc, message := propertiesParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	raw := doc.Render()
	terminators := make([]string, 0, len(doc.NaturalLines()))
	for _, line := range doc.NaturalLines() {
		breakSpan := line.LineBreakSpan()
		if breakSpan == nil {
			terminators = append(terminators, "Eof")
			continue
		}
		bytes := raw[breakSpan.StartByte():breakSpan.EndByte()]
		switch string(bytes) {
		case "\n":
			terminators = append(terminators, "Lf")
		case "\r":
			terminators = append(terminators, "Cr")
		case "\r\n":
			terminators = append(terminators, "CrLf")
		default:
			terminators = append(terminators, "Other")
		}
	}
	coverage, _ := propertiesExactCoverage(doc)
	if uint64(len(doc.NaturalLines())) != expectedInt(vector, report, "natural_lines") ||
		uint64(len(doc.LogicalLines())) != expectedInt(vector, report, "logical_lines") ||
		uint64(len(doc.Properties())) != expectedInt(vector, report, "properties") ||
		!stringSlicesEqual(terminators, expectedStrings(vector, report, "terminators")) ||
		coverage != expectedBool(vector, report, "exact_coverage") {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "line terminator facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPropertiesFormationContinuations(vector *caseData, report *SuiteReport) {
	samples, ok := sequenceField(vector.Input, "samples")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.samples"})
		return
	}
	for index := range samples {
		sample, ok := samples[index].(*core.Object)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "sample must be Object"})
			return
		}
		sourceValue, ok := sample.Get("source")
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
		doc, message := propertiesParseReaderText(string(source))
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "continuation sample " + itoaYaml(index) + ": " + message})
			return
		}
		propertiesCount := len(doc.Properties())
		if propertiesCount == 0 {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "continuation sample " + itoaYaml(index) + " has no property"})
			return
		}
		valueHex := hex.EncodeToString(doc.Properties()[0].Value().Utf16beBytes())
		naturalLines, ok := sample.Get("natural_lines")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "sample.natural_lines missing"})
			return
		}
		logicalLines, ok := sample.Get("logical_lines")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "sample.logical_lines missing"})
			return
		}
		valueHexExpected, ok := sample.Get("value_hex")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "sample.value_hex missing"})
			return
		}
		expectedHex, ok := valueHexExpected.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "sample.value_hex must be String"})
			return
		}
		coverage, _ := propertiesExactCoverage(doc)
		if doc.FormationStatus().String() != "Complete" ||
			valueHex != string(expectedHex) ||
			uint64(len(doc.NaturalLines())) != naturalLines.(core.Integer).Int().Uint64() ||
			uint64(len(doc.LogicalLines())) != logicalLines.(core.Integer).Int().Uint64() ||
			!coverage {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "continuation/backslash sample " + itoaYaml(index) + " differed"})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPropertiesFormationJavaStrings(vector *caseData, report *SuiteReport) {
	doc, message := propertiesParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	values := make([]string, 0, len(doc.Properties()))
	statuses := make([]string, 0, len(doc.Properties()))
	for _, property := range doc.Properties() {
		values = append(values, hex.EncodeToString(property.Value().Utf16beBytes()))
		statuses = append(statuses, string(property.Value().Status()))
	}
	escapeKinds := make([]string, 0, len(doc.Escapes()))
	for _, escape := range doc.Escapes() {
		escapeKinds = append(escapeKinds, string(escape.Kind()))
	}
	if !stringSlicesEqual(values, expectedStrings(vector, report, "value_utf16be_hex")) ||
		!stringSlicesEqual(statuses, expectedStrings(vector, report, "statuses")) ||
		!stringSlicesEqual(escapeKinds, expectedStrings(vector, report, "escape_kinds")) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "Java UTF-16/escape facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPropertiesFormationRecoveryMatrix(vector *caseData, report *SuiteReport) {
	samples, ok := sequenceField(vector.Input, "samples")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.samples"})
		return
	}
	formations, ok := sequenceField(vector.Expected, "formations")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.formations"})
		return
	}
	propertyCounts, ok := sequenceField(vector.Expected, "property_counts")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.property_counts"})
		return
	}
	errorCounts, ok := sequenceField(vector.Expected, "error_counts")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.error_counts"})
		return
	}
	code, ok := stringField(vector.Expected, "code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.code"})
		return
	}
	for index := range samples {
		source, ok := samples[index].(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "sample must be String"})
			return
		}
		doc, message := propertiesParseReaderText(string(source))
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "malformed Unicode sample " + itoaYaml(index) + ": " + message})
			return
		}
		expectedFormation, ok := formations[index].(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "formations must be String"})
			return
		}
		expectedProperties, ok := propertyCounts[index].(core.Integer)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "property_counts must be Integer"})
			return
		}
		expectedErrors, ok := errorCounts[index].(core.Integer)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "error_counts must be Integer"})
			return
		}
		errorCodeOK := true
		if len(doc.ErrorLines()) > 0 {
			errorCodeOK = doc.ErrorLines()[0].Code() == code
		}
		if doc.FormationStatus().String() != string(expectedFormation) ||
			uint64(len(doc.Properties())) != expectedProperties.Int().Uint64() ||
			uint64(len(doc.ErrorLines())) != expectedErrors.Int().Uint64() ||
			!errorCodeOK {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "malformed Unicode sample " + itoaYaml(index) + " differed"})
			return
		}
		if index+1 == len(samples) {
			if len(doc.Properties()) == 0 {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "uppercase U sample has no property"})
				return
			}
			value, err := doc.Properties()[0].Value().ToUnicode()
			if err != nil {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "uppercase U sample value is not Unicode"})
				return
			}
			uppercaseValue, ok := stringField(vector.Expected, "uppercase_u_value")
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "missing expected.uppercase_u_value"})
				return
			}
			if value != uppercaseValue {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "uppercase U behavior differed"})
				return
			}
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

// runPropertiesMalformedEscapeInKey executes one
// `formation.malformed-escape-in-key` case: a malformed `\uXXXX` escape in
// the KEY position recovers the logical line without a partial property and
// the error line carries the family parse code (parser.rs:626-666).
func runPropertiesMalformedEscapeInKey(vector *caseData, report *SuiteReport) {
	doc, message := propertiesParseCase(vector)
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
	expectedProperties, ok := integerField(vector.Expected, "properties")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.properties"})
		return
	}
	expectedErrors, ok := integerField(vector.Expected, "error_lines")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.error_lines"})
		return
	}
	expectedCode, ok := stringField(vector.Expected, "code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.code"})
		return
	}
	errorLines := doc.ErrorLines()
	if doc.FormationStatus().String() != expectedFormation ||
		uint64(len(doc.Properties())) != expectedProperties ||
		uint64(len(errorLines)) != expectedErrors ||
		len(errorLines) == 0 || errorLines[0].Code() != expectedCode {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "malformed escape in key facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// runPropertiesFatalEncoding executes one fatal encoding failure of the
// Reader profile: bytes that cannot be decoded under the explicit encoding
// (`core.source.invalid-sequence@1`) or a BOM that contradicts it
// (`core.source.encoding-conflict@1`) fail the whole parse before any
// document forms (parser.rs:24-33).
func runPropertiesFatalEncoding(vector *caseData, report *SuiteReport) {
	encodingName, ok := stringField(vector.Input, "encoding")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.encoding"})
		return
	}
	encoding, ok := propertiesSourceEncoding(encodingName)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "unknown source encoding"})
		return
	}
	hexText, ok := stringField(vector.Input, "source_hex")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.source_hex"})
		return
	}
	raw, err := hex.DecodeString(hexText)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "invalid source hex"})
		return
	}
	_, failure := properties.ParseReader(raw, encoding,
		properties.DefaultPropertiesParseLimits())
	if failure == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "parse must fail fatally"})
		return
	}
	expectedCode, ok := stringField(vector.Expected, "code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.code"})
		return
	}
	diagnostics := failure.Diagnostics()
	if len(diagnostics) == 0 || diagnostics[0].Code != expectedCode {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "fatal encoding code differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPropertiesFormationReaderEncodings(vector *caseData, report *SuiteReport) {
	samples, ok := sequenceField(vector.Input, "samples")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.samples"})
		return
	}
	for index := range samples {
		sample, ok := samples[index].(*core.Object)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "encoding sample must be Object"})
			return
		}
		encodingName, ok := sample.Get("encoding")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "sample.encoding missing"})
			return
		}
		encoding, ok := propertiesSourceEncoding(string(encodingName.(core.String)))
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "unknown source encoding"})
			return
		}
		hexText, ok := sample.Get("source_hex")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "sample.source_hex missing"})
			return
		}
		raw, err := hex.DecodeString(string(hexText.(core.String)))
		if err != nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "invalid source hex"})
			return
		}
		doc, failure := properties.ParseReader(raw, encoding,
			properties.DefaultPropertiesParseLimits())
		if failure != nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "Reader encoding sample " + itoaYaml(index) + " failed: " + failure.Code()})
			return
		}
		key, ok := sample.Get("key")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "sample.key missing"})
			return
		}
		value, ok := sample.Get("value")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "sample.value missing"})
			return
		}
		bom, ok := sample.Get("bom")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "sample.bom missing"})
			return
		}
		coverage, _ := propertiesExactCoverage(doc)
		if len(doc.Properties()) == 0 {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "encoding sample has no property"})
			return
		}
		keyText, keyErr := doc.Properties()[0].Key().ToUnicode()
		valueText, valueErr := doc.Properties()[0].Value().ToUnicode()
		if doc.FormationStatus().String() != "Complete" ||
			string(doc.Render()) != string(raw) ||
			keyErr != nil || valueErr != nil ||
			keyText != string(key.(core.String)) ||
			valueText != string(value.(core.String)) ||
			propertiesBomName(doc) != string(bom.(core.String)) ||
			!coverage {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "Reader encoding sample " + itoaYaml(index) + " differed"})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

// propertiesBomName mirrors the Rust `format!("{:?}", bom)` fact.
func propertiesBomName(doc *properties.Document) string {
	bom := doc.Source().EncodingFacts().Bom()
	if bom == nil {
		return "None"
	}
	return "Some(" + string(*bom) + ")"
}

func runPropertiesFormationLatin1(vector *caseData, report *SuiteReport) {
	hexText, ok := stringField(vector.Input, "source_hex")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.source_hex"})
		return
	}
	raw, err := hex.DecodeString(hexText)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "invalid source hex"})
		return
	}
	doc, failure := properties.ParseLatin1(raw, properties.DefaultPropertiesParseLimits())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "Latin-1 formation failed: " + failure.Code()})
		return
	}
	if len(doc.Properties()) == 0 {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "Latin-1 document has no property"})
		return
	}
	keyHex := hex.EncodeToString(doc.Properties()[0].Key().Utf16beBytes())
	valueHex := hex.EncodeToString(doc.Properties()[0].Value().Utf16beBytes())
	hasBomKind := false
	for _, kind := range doc.LosslessSyntaxKinds() {
		if kind == properties.SyntaxKindBom {
			hasBomKind = true
			break
		}
	}
	coverage, _ := propertiesExactCoverage(doc)
	if keyHex != expectedString(vector, report, "key_utf16be_hex") ||
		valueHex != expectedString(vector, report, "value_utf16be_hex") ||
		propertiesBomName(doc) != expectedString(vector, report, "bom") ||
		hasBomKind != expectedBool(vector, report, "bom_syntax") ||
		coverage != expectedBool(vector, report, "exact_coverage") {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "Latin-1 byte/BOM-content facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPropertiesRecoveredIsAtomic(vector *caseData, report *SuiteReport) {
	doc, message := propertiesParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	projectionCode := ""
	if result := doc.Project(properties.BestExactEntryMapping()); result.Failed != nil &&
		len(result.Failed.Diagnostics) > 0 {
		projectionCode = result.Failed.Diagnostics[0].Code
	}
	builder := properties.NewEditTransactionBuilder(doc)
	_, editFailure := doc.Commit(builder.Build())
	editCode := ""
	if editFailure != nil {
		editCode = editFailure.Code()
	}
	keys, ok := propertiesUnicodeKeys(doc)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "property key is not well-formed Unicode"})
		return
	}
	if doc.FormationStatus().String() != expectedString(vector, report, "formation") ||
		!stringSlicesEqual(keys, expectedStrings(vector, report, "keys")) ||
		uint64(len(doc.ErrorLines())) != expectedInt(vector, report, "error_lines") ||
		len(doc.ErrorLines()) == 0 ||
		doc.ErrorLines()[0].Code() != expectedString(vector, report, "code") ||
		projectionCode != expectedString(vector, report, "projection_code") ||
		editCode != expectedString(vector, report, "edit_code") {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "recovered Properties document exposed a partial operation result"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPropertiesNativeQuery(vector *caseData, report *SuiteReport) {
	doc, message := propertiesParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	keyHex, ok := stringField(vector.Input, "key_utf16be_hex")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.key_utf16be_hex"})
		return
	}
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "invalid key hex"})
		return
	}
	take, ok := integerField(vector.Input, "take")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.take"})
		return
	}
	duplicates := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.document-properties", 1)).
		Then(protocol.NewOperatorCall("properties.property-key-equals", 1).
			WithArgument("key", core.NewBytes(keyBytes))).
		Then(protocol.NewOperatorCall("core.take", 1).
			WithArgument("count", core.NewInteger(big.NewInt(int64(take))))).
		Then(protocol.NewOperatorCall("properties.duplicate-group", 1))
	executable, message := propertiesExecutableNative(duplicates)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	duplicateMatches, failure := properties.ExecutePropertiesQuery(contextBackground(),
		executable, doc, protocol.DefaultQueryLimits())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "duplicate query: " + failure.Code()})
		return
	}
	escapes := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.document-properties", 1)).
		Then(protocol.NewOperatorCall("core.take", 1).
			WithArgument("count", core.NewInteger(big.NewInt(int64(take))))).
		Then(protocol.NewOperatorCall("properties.property-escapes", 1))
	executable, message = propertiesExecutableNative(escapes)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	escapeMatches, failure := properties.ExecutePropertiesQuery(contextBackground(),
		executable, doc, protocol.DefaultQueryLimits())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "escape query: " + failure.Code()})
		return
	}
	allGrouped := len(duplicateMatches) > 0
	for _, match := range duplicateMatches {
		if match.Kind != properties.PropertiesMatchProperty ||
			match.DuplicateGroup == nil {
			allGrouped = false
			break
		}
	}
	allEscapes := len(escapeMatches) > 0
	for _, match := range escapeMatches {
		if match.Kind != properties.PropertiesMatchEscape {
			allEscapes = false
			break
		}
	}
	if uint64(len(duplicateMatches)) != expectedInt(vector, report, "duplicate_matches") ||
		uint64(len(escapeMatches)) != expectedInt(vector, report, "escape_matches") ||
		allGrouped != expectedBool(vector, report, "duplicate_group") ||
		allEscapes != expectedBool(vector, report, "escape_roles") ||
		"Completed" != expectedString(vector, report, "terminal") {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "native duplicate/escape query facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPropertiesLogicalSyntaxQuery(vector *caseData, report *SuiteReport) {
	logicalSource, ok := stringField(vector.Input, "logical_source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.logical_source"})
		return
	}
	logical, message := propertiesParseReaderText(logicalSource)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.logical-lines", 1)).
		Then(protocol.NewOperatorCall("properties.logical-line-natural-lines", 1))
	executable, message := propertiesExecutableNative(expression)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	logicalMatches, failure := properties.ExecutePropertiesQuery(contextBackground(),
		executable, logical, protocol.DefaultQueryLimits())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "logical query: " + failure.Code()})
		return
	}
	ordinals := make([]uint64, 0, len(logicalMatches))
	for _, match := range logicalMatches {
		if match.Kind != properties.PropertiesMatchNaturalLine {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "logical query returned non-natural line"})
			return
		}
		ordinals = append(ordinals, uint64(match.Ordinal))
	}

	syntaxSource, ok := stringField(vector.Input, "syntax_source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.syntax_source"})
		return
	}
	syntax, message := propertiesParseReaderText(syntaxSource)
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
	rawHex, ok := stringField(vector.Input, "raw_hex")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.raw_hex"})
		return
	}
	rawBytes, err := hex.DecodeString(rawHex)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "invalid raw hex"})
		return
	}
	utf16Hex, ok := stringField(vector.Input, "utf16be_hex")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.utf16be_hex"})
		return
	}
	utf16Bytes, err := hex.DecodeString(utf16Hex)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "invalid utf16be hex"})
		return
	}
	rawBranch := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.syntax-raw-bytes-equals", 1).
			WithArgument("bytes", core.NewBytes(rawBytes)))
	textBranch := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.syntax-text-equals", 1).
			WithArgument("text", core.String(text)))
	utf16Branch := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.syntax-utf16be-equals", 1).
			WithArgument("code_units", core.NewBytes(utf16Bytes)))
	merge := &protocol.QueryExpression{Kind: protocol.ExpressionStructureOrderMerge,
		Branches: []*protocol.QueryExpression{rawBranch, textBranch, utf16Branch}}
	syntaxExecutable, message := propertiesExecutableSyntax(merge)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	syntaxMatches, failure := properties.ExecutePropertiesSyntaxQuery(contextBackground(),
		syntaxExecutable, syntax, protocol.DefaultQueryLimits())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "syntax query: " + failure.Code()})
		return
	}
	kinds := make([]string, 0, len(syntaxMatches))
	increasing := true
	allRoles := true
	for index, match := range syntaxMatches {
		kinds = append(kinds, match.Kind().AsStr())
		if string(match.NodeRef().Role()) != "PropertiesSyntaxPiece" {
			allRoles = false
		}
		if index > 0 && syntaxMatches[index-1].Ordinal() >= match.Ordinal() {
			increasing = false
		}
	}
	if !uint64SlicesEqual(ordinals, expectedInts(vector, report, "natural_ordinals")) ||
		!stringSlicesEqual(kinds, expectedStrings(vector, report, "syntax_kinds")) ||
		!allRoles ||
		increasing != expectedBool(vector, report, "strictly_increasing_ordinals") {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "logical/syntax query facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPropertiesQueryFailures(vector *caseData, report *SuiteReport) {
	invalid := protocol.NewQueryDefinition(protocol.DomainJavaPropertiesNativeV1()).
		WithExpression((&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
			Then(protocol.NewOperatorCall("properties.document-properties", 1)).
			Then(protocol.NewOperatorCall("properties.property-key-equals", 1).
				WithArgument("key", core.NewBytes([]byte{0}))))
	_, validationFailure := invalid.Validate()
	invalidArgument := ""
	if validationFailure != nil && validationFailure.Argument != "" {
		invalidArgument = validationFailure.Argument
	}
	doc, message := propertiesParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	all := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.document-properties", 1))
	executable, message := propertiesExecutableNative(all)
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
	_, queryFailure := properties.ExecutePropertiesQuery(contextBackground(),
		executable, doc, limits)
	limitCode := ""
	if queryFailure != nil {
		limitCode = queryFailure.Code()
	}
	cursorContext, cancel := context.WithCancel(contextBackground())
	defer cancel()
	cursor, cursorFailure := properties.ExecutePropertiesQueryCursor(cursorContext,
		executable, doc, protocol.DefaultQueryLimits())
	if cursorFailure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "cursor: " + cursorFailure.Code()})
		return
	}
	firstYielded := cursor.Next() != nil
	cancel()
	exhausted := cursor.Next() == nil
	terminal := ""
	if state := cursor.TerminalState(); state != nil {
		terminal = string(*state)
	}
	if invalidArgument != expectedString(vector, report, "invalid_argument") ||
		limitCode != expectedString(vector, report, "limit_code") ||
		firstYielded != expectedBool(vector, report, "first_yielded") ||
		!exhausted ||
		terminal != expectedString(vector, report, "terminal") {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "query validation, limit, or cancellation differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPropertiesProjectionExact(vector *caseData, report *SuiteReport) {
	doc, message := propertiesParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	result := doc.Project(properties.BestExactEntryMapping())
	if result.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "exact Properties projection failed"})
		return
	}
	mapping, ok := result.Complete.Value.(*core.EntryMapping)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "exact projection did not produce EntryMapping"})
		return
	}
	keys := make([]string, 0, mapping.Len())
	values := make([]string, 0, mapping.Len())
	for _, entry := range mapping.Entries() {
		key, ok := entry.Key.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "projected key is not String"})
			return
		}
		value, ok := entry.Value.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "projected value is not String"})
			return
		}
		keys = append(keys, string(key))
		values = append(values, string(value))
	}
	escape := propertiesRelationPresent(result.Complete, properties.ProvenanceEscapeDerived)
	twoValueFragments := false
	for _, entry := range result.Complete.Provenance.Entries() {
		count := 0
		for _, origin := range entry.Origins {
			if origin.Relation == properties.ProvenanceValueFragment {
				count++
			}
		}
		if count == 2 {
			twoValueFragments = true
			break
		}
	}
	association := false
	for _, entry := range result.Complete.Provenance.Entries() {
		if entry.Projected.Kind == "Association" {
			association = true
			break
		}
	}
	if result.Complete.Fidelity.String() != expectedString(vector, report, "fidelity") ||
		!stringSlicesEqual(keys, expectedStrings(vector, report, "keys")) ||
		!stringSlicesEqual(values, expectedStrings(vector, report, "values")) ||
		uint64(len(result.Complete.Report.Events())) != expectedInt(vector, report, "events") ||
		escape != expectedBool(vector, report, "escape_provenance") ||
		twoValueFragments != expectedBool(vector, report, "two_value_fragments") ||
		association != expectedBool(vector, report, "association_provenance") {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "exact Properties projection facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func propertiesRelationPresent(complete *properties.CompleteProjection,
	relation properties.ProvenanceRelation) bool {
	for _, entry := range complete.Provenance.Entries() {
		for _, origin := range entry.Origins {
			if origin.Relation == relation {
				return true
			}
		}
	}
	return false
}

func runPropertiesProjectionFailures(vector *caseData, report *SuiteReport) {
	unpairedSource, ok := stringField(vector.Input, "unpaired_source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.unpaired_source"})
		return
	}
	unpaired, message := propertiesParseReaderText(unpairedSource)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	unpairedResult := unpaired.Project(properties.BestExactEntryMapping())
	if unpairedResult.Complete != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "unpaired surrogate projection completed"})
		return
	}
	recoveredSource, ok := stringField(vector.Input, "recovered_source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.recovered_source"})
		return
	}
	recovered, message := propertiesParseReaderText(recoveredSource)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	recoveredResult := recovered.Project(properties.BestExactEntryMapping())
	if recoveredResult.Complete != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "recovered projection completed"})
		return
	}
	unpairedCode := ""
	unpairedStart := uint64(0)
	if len(unpairedResult.Failed.Diagnostics) > 0 {
		unpairedCode = unpairedResult.Failed.Diagnostics[0].Code
		if primary := unpairedResult.Failed.Diagnostics[0].Primary; primary != nil {
			unpairedStart = primary.StartByte
		}
	}
	recoveredCode := ""
	if len(recoveredResult.Failed.Diagnostics) > 0 {
		recoveredCode = recoveredResult.Failed.Diagnostics[0].Code
	}
	expectedStart, ok := integerField(vector.Expected, "unpaired_start_byte")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.unpaired_start_byte"})
		return
	}
	emptyReports := len(unpairedResult.Failed.Report.Events()) == 0 &&
		len(recoveredResult.Failed.Report.Events()) == 0
	if unpairedCode != expectedString(vector, report, "unpaired_code") ||
		unpairedStart != expectedStart ||
		recoveredCode != expectedString(vector, report, "recovered_code") ||
		emptyReports != expectedBool(vector, report, "empty_reports") {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "unpaired/recovered projection atomic failure differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPropertiesProjectionCollapse(vector *caseData, report *SuiteReport) {
	doc, message := propertiesParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	uniqueResult := doc.Project(properties.RequireObject(properties.DuplicatePolicyRequireUnique))
	if uniqueResult.Complete != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "unique projection accepted duplicates"})
		return
	}
	uniqueCode := ""
	if len(uniqueResult.Failed.Diagnostics) > 0 {
		uniqueCode = uniqueResult.Failed.Diagnostics[0].Code
	}
	firstResult := doc.Project(properties.RequireObject(properties.DuplicatePolicyFirstWins))
	if firstResult.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "FirstWins projection failed"})
		return
	}
	lastResult := doc.Project(properties.RequireObject(properties.DuplicatePolicyLastWinsJdkTable))
	if lastResult.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "LastWinsJdkTable projection failed"})
		return
	}
	firstPairs, ok := propertiesObjectPairs(firstResult.Complete.Value)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "FirstWins projection did not produce Object"})
		return
	}
	lastPairs, ok := propertiesObjectPairs(lastResult.Complete.Value)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "LastWinsJdkTable projection did not produce Object"})
		return
	}
	firstEvents := firstResult.Complete.Report.Events()
	eventCode := ""
	if len(firstEvents) > 0 {
		eventCode = firstEvents[0].Code
	}
	collapsed := propertiesRelationPresent(firstResult.Complete, properties.ProvenanceCollapsed)
	if uniqueCode != expectedString(vector, report, "unique_code") ||
		firstResult.Complete.Fidelity.String() != expectedString(vector, report, "first_fidelity") ||
		uint64(len(firstEvents)) != expectedInt(vector, report, "events") ||
		eventCode != expectedString(vector, report, "event_code") ||
		!stringPairSlicesEqual(firstPairs, expectedPairs(vector, report, "first_entries")) ||
		!stringPairSlicesEqual(lastPairs, expectedPairs(vector, report, "last_entries")) ||
		collapsed != expectedBool(vector, report, "collapsed_provenance") {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "explicit JDK table collapse differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// propertiesObjectPairs reads one projected Object into ordered pairs.
func propertiesObjectPairs(value core.Value) ([][2]string, bool) {
	object, ok := value.(*core.Object)
	if !ok {
		return nil, false
	}
	pairs := make([][2]string, 0, len(object.Entries()))
	for _, entry := range object.Entries() {
		value, ok := entry.Value.(core.String)
		if !ok {
			return nil, false
		}
		pairs = append(pairs, [2]string{entry.Key, string(value)})
	}
	return pairs, true
}

// propertiesMaterializationRequest builds the frozen vector
// materialization request for one profile.
func propertiesMaterializationRequest(profile properties.PropertiesProfile) document.MaterializationRequest {
	if profile == properties.PropertiesLatin1V1 {
		return document.NewMaterializationRequest(
			document.NewProfileId("java-properties.latin1", 1),
			document.NewMaterializationStyleId("java-properties.latin1-canonical", 1)).
			WithEncoding(document.Latin1Encoding())
	}
	return document.NewMaterializationRequest(
		document.NewProfileId("java-properties.reader", 1),
		document.NewMaterializationStyleId("java-properties.reader-canonical", 1))
}

// propertiesFlatMapping builds one EntryMapping of String pairs from a
// vector descriptor.
func propertiesFlatMapping(descriptor core.Value) (*core.EntryMapping, string) {
	items, ok := descriptor.(*core.Array)
	if !ok {
		return nil, "mapping descriptor must be Sequence"
	}
	pairs := make([]core.EntryMappingEntry, 0, len(items.Items()))
	for _, item := range items.Items() {
		pair, ok := item.(*core.Array)
		if !ok || len(pair.Items()) != 2 {
			return nil, "mapping entry must be a two-item Sequence"
		}
		key, ok := pair.Items()[0].(core.String)
		if !ok {
			return nil, "mapping key must be String"
		}
		value, ok := pair.Items()[1].(core.String)
		if !ok {
			return nil, "mapping value must be String"
		}
		pairs = append(pairs, core.EntryMappingEntry{Key: key, Value: value})
	}
	mapping, err := core.NewEntryMapping(pairs...)
	if err != nil {
		return nil, "EntryMapping construction failed"
	}
	return mapping, ""
}

func runPropertiesMaterializationStyles(vector *caseData, report *SuiteReport) {
	readerDescriptor, ok := caseInput(vector, "reader")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.reader"})
		return
	}
	readerValue, message := propertiesFlatMapping(readerDescriptor)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	readerResult := properties.Materialize(readerValue,
		propertiesMaterializationRequest(properties.PropertiesReaderV1))
	if readerResult.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "Reader materialization failed: " + readerResult.Failed.Failure.Code()})
		return
	}
	latinDescriptor, ok := caseInput(vector, "latin1")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.latin1"})
		return
	}
	latinValue, message := propertiesFlatMapping(latinDescriptor)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	latinResult := properties.Materialize(latinValue,
		propertiesMaterializationRequest(properties.PropertiesLatin1V1).
			WithNewline(document.NewlineCrLf))
	if latinResult.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "Latin-1 materialization failed: " + latinResult.Failed.Failure.Code()})
		return
	}
	utf16Descriptor, ok := caseInput(vector, "utf16be")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.utf16be"})
		return
	}
	utf16Value, message := propertiesFlatMapping(utf16Descriptor)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	utf16Result := properties.Materialize(utf16Value,
		propertiesMaterializationRequest(properties.PropertiesReaderV1).
			WithEncoding(document.Utf16BeEncoding()).
			WithNewline(document.NewlineCrLf))
	if utf16Result.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "UTF-16BE Reader materialization failed: " + utf16Result.Failed.Failure.Code()})
		return
	}
	cpDescriptor, ok := caseInput(vector, "cp1252")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.cp1252"})
		return
	}
	cpValue, message := propertiesFlatMapping(cpDescriptor)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	cpPage, ok := document.WindowsCodePageFromNumber(1252)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "CP1252 unavailable"})
		return
	}
	cpResult := properties.Materialize(cpValue,
		propertiesMaterializationRequest(properties.PropertiesReaderV1).
			WithEncoding(document.WindowsCodePageEncoding(cpPage)))
	if cpResult.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "CP1252 Reader materialization failed: " + cpResult.Failed.Failure.Code()})
		return
	}
	closure := true
	for _, pair := range [][2]interface{}{
		{readerResult.Complete.Document, readerValue},
		{latinResult.Complete.Document, latinValue},
		{utf16Result.Complete.Document, utf16Value},
		{cpResult.Complete.Document, cpValue},
	} {
		doc := pair[0].(*properties.Document)
		input := pair[1].(*core.EntryMapping)
		projected := doc.Project(properties.BestExactEntryMapping())
		if projected.Complete == nil || !core.Equal(projected.Complete.Value, input) {
			closure = false
			break
		}
	}
	utf16Text, _ := utf16Result.Complete.Document.Source().DecodedText()
	exactFidelity := readerResult.Complete.Fidelity == properties.MaterializationFidelityExact &&
		latinResult.Complete.Fidelity == properties.MaterializationFidelityExact &&
		utf16Result.Complete.Fidelity == properties.MaterializationFidelityExact &&
		cpResult.Complete.Fidelity == properties.MaterializationFidelityExact
	if string(readerResult.Complete.Document.Render()) != expectedString(vector, report, "reader_source") ||
		string(latinResult.Complete.Document.Render()) != expectedString(vector, report, "latin1_source") ||
		utf16Text != expectedString(vector, report, "utf16be_decoded") ||
		hex.EncodeToString(cpResult.Complete.Document.Render()) != expectedString(vector, report, "cp1252_hex") ||
		exactFidelity != expectedBool(vector, report, "exact_fidelity") ||
		closure != expectedBool(vector, report, "closure") {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "canonical materialization differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPropertiesMaterializationLimits(vector *caseData, report *SuiteReport) {
	scalarResult := properties.Materialize(core.String("scalar"),
		propertiesMaterializationRequest(properties.PropertiesReaderV1))
	scalarCode := ""
	if scalarResult.Failed != nil {
		scalarCode = scalarResult.Failed.Failure.Code()
	}
	valueDescriptor, ok := caseInput(vector, "value")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.value"})
		return
	}
	value, message := propertiesFlatMapping(valueDescriptor)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	encodingResult := properties.Materialize(value,
		propertiesMaterializationRequest(properties.PropertiesLatin1V1).
			WithEncoding(document.Utf8Encoding()))
	encodingCode := ""
	if encodingResult.Failed != nil {
		encodingCode = encodingResult.Failed.Failure.Code()
	}
	names, ok := sequenceField(vector.Input, "limit_names")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.limit_names"})
		return
	}
	outcomes := make([]string, 0, len(names))
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
		result := properties.Materialize(value,
			propertiesMaterializationRequest(properties.PropertiesReaderV1).WithLimits(limits))
		if result.Complete != nil {
			outcomes = append(outcomes, "Complete")
			continue
		}
		if result.Failed.Failure.Code() != expectedString(vector, report, "limit_code") {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: string(name) + " returned wrong failure code"})
			return
		}
		outcomes = append(outcomes, "Failed")
	}
	if scalarCode != expectedString(vector, report, "scalar_code") ||
		encodingCode != expectedString(vector, report, "encoding_code") ||
		!stringSlicesEqual(outcomes, expectedStrings(vector, report, "limit_outcomes")) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "materialization failure outcomes differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPropertiesEditAllOperations(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.source"})
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
	editCounts := make([]int, 0, 5)

	doc, message := propertiesParseReaderText(source)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	builder := properties.NewEditTransactionBuilder(doc)
	builder.SemanticValue(doc.Properties()[0].NodeRef(),
		properties.NewJavaStringFromUnicode(semanticValue))
	if !propertiesCollectEdit(doc, builder, &outputs, &editCounts) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "semantic edit failed"})
		return
	}

	doc, message = propertiesParseReaderText(source)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	builder = properties.NewEditTransactionBuilder(doc)
	builder.LiteralValue(doc.Properties()[0].NodeRef(), []byte(literalValue))
	if !propertiesCollectEdit(doc, builder, &outputs, &editCounts) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "literal edit failed"})
		return
	}

	doc, message = propertiesParseReaderText(source)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	builder = properties.NewEditTransactionBuilder(doc)
	builder.InsertProperty(doc.NodeRef(), properties.NewJavaStringFromUnicode(newKey),
		properties.NewJavaStringFromUnicode(newValue), properties.PlacementEnd())
	if !propertiesCollectEdit(doc, builder, &outputs, &editCounts) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "insert edit failed"})
		return
	}

	doc, message = propertiesParseReaderText(source)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	builder = properties.NewEditTransactionBuilder(doc)
	builder.RemoveProperty(doc.Properties()[0].NodeRef())
	if !propertiesCollectEdit(doc, builder, &outputs, &editCounts) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "remove edit failed"})
		return
	}

	doc, message = propertiesParseReaderText(source)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	builder = properties.NewEditTransactionBuilder(doc)
	builder.RenameProperty(doc.Properties()[0].NodeRef(),
		properties.NewJavaStringFromUnicode(renamedKey))
	if !propertiesCollectEdit(doc, builder, &outputs, &editCounts) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "rename edit failed"})
		return
	}

	oneEditEach := len(editCounts) == 5
	for _, count := range editCounts {
		if count != 1 {
			oneEditEach = false
			break
		}
	}
	if !stringSlicesEqual(outputs, expectedStrings(vector, report, "outputs")) ||
		oneEditEach != expectedBool(vector, report, "one_source_edit_each") {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "five edit outputs differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func propertiesCollectEdit(doc *properties.Document, builder *properties.EditTransactionBuilder,
	outputs *[]string, editCounts *[]int) bool {
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		return false
	}
	*outputs = append(*outputs, string(commit.Document.Render()))
	*editCounts = append(*editCounts, len(commit.ChangeSet.SourceEdits()))
	return true
}

func runPropertiesEditAuditArtifacts(vector *caseData, report *SuiteReport) {
	doc, message := propertiesParseCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	rename, ok := stringField(vector.Input, "rename")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.rename"})
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
	first := doc.Properties()[0].NodeRef()
	second := doc.Properties()[1].NodeRef()
	builder := properties.NewEditTransactionBuilder(doc)
	builder.RenameProperty(first, properties.NewJavaStringFromUnicode(rename))
	builder.SemanticValue(second, properties.NewJavaStringFromUnicode(value))
	transaction := builder.Build()
	plan, planFailure := doc.DryRun(transaction, sourceID)
	if planFailure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "dry run: " + planFailure.Name()})
		return
	}
	commit, commitFailure := doc.Commit(transaction)
	if commitFailure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "commit: " + commitFailure.Name()})
		return
	}
	replay, replayErr := commit.SourcePatch.Apply(doc.Source(), document.DefaultSourcePatchLimits())
	if replayErr != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "patch replay failed"})
		return
	}
	proofErr := commit.UntouchedProof.Verify(doc.Source(), commit.Document.Source(),
		commit.SourcePatch.Replacements())

	conflict := properties.NewEditTransactionBuilder(doc)
	conflict.SemanticValue(first, properties.NewJavaStringFromUnicode("x"))
	conflict.RenameProperty(first, properties.NewJavaStringFromUnicode("renamed"))
	_, conflictFailure := doc.Commit(conflict.Build())
	conflictCode := ""
	if conflictFailure != nil {
		conflictCode = conflictFailure.Code()
	}
	source, _ := stringField(vector.Input, "source")
	if string(commit.Document.Render()) != expectedString(vector, report, "source") ||
		uint64(len(commit.ChangeSet.SourceEdits())) != expectedInt(vector, report, "edit_count") ||
		uint64(len(plan.Operations())) != expectedInt(vector, report, "dry_run_operations") ||
		(string(replay.Bytes()) == string(commit.Document.Render())) !=
			expectedBool(vector, report, "patch_replays") ||
		(proofErr == nil) != expectedBool(vector, report, "proof_verifies") ||
		conflictCode != expectedString(vector, report, "conflict_code") ||
		(string(doc.Render()) == source) != expectedBool(vector, report, "base_unchanged") {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "edit patch/proof/conflict atomicity differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPropertiesFormationLimits(vector *caseData, report *SuiteReport) {
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
		sourceValue, ok := fields.Get("source")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "limit descriptor.source missing"})
			return
		}
		valueValue, ok := fields.Get("value")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "limit descriptor.value missing"})
			return
		}
		limits := properties.DefaultPropertiesParseLimits()
		if !propertiesSetParseLimit(&limits, string(nameValue.(core.String)),
			valueValue.(core.Integer).Int().Int64()) {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "unknown Properties parse limit " + string(nameValue.(core.String))})
			return
		}
		_, failure := properties.ParseReader([]byte(string(sourceValue.(core.String))),
			document.Utf8Encoding(), limits)
		if failure != nil {
			fatal++
		}
	}
	if uint64(fatal) != expectedInt(vector, report, "fatal_count") ||
		!expectedBool(vector, report, "no_partial_documents") {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "formation limit outcomes differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// propertiesSetParseLimit applies one vector limit name.
func propertiesSetParseLimit(limits *properties.PropertiesParseLimits, name string, value int64) bool {
	set := func(target *int) bool {
		if value < 0 || value > int64(^uint(0)>>1) {
			return false
		}
		*target = int(value)
		return true
	}
	switch name {
	case "max_source_bytes":
		return set(&limits.Common.MaxSourceBytes)
	case "max_nesting_depth":
		return set(&limits.Common.MaxNestingDepth)
	case "max_token_count":
		return set(&limits.Common.MaxTokenCount)
	case "max_node_count":
		return set(&limits.Common.MaxNodeCount)
	case "max_diagnostics":
		return set(&limits.Common.MaxDiagnostics)
	case "max_decoded_utf8_bytes":
		return set(&limits.MaxDecodedUTF8Bytes)
	case "max_decoded_scalars":
		return set(&limits.MaxDecodedScalars)
	case "max_natural_lines":
		return set(&limits.MaxNaturalLines)
	case "max_natural_line_bytes":
		return set(&limits.MaxNaturalLineBytes)
	case "max_natural_line_scalars":
		return set(&limits.MaxNaturalLineScalars)
	case "max_logical_lines":
		return set(&limits.MaxLogicalLines)
	case "max_logical_line_natural_lines":
		return set(&limits.MaxLogicalLineNaturalLines)
	case "max_logical_line_scalars":
		return set(&limits.MaxLogicalLineScalars)
	case "max_properties":
		return set(&limits.MaxProperties)
	case "max_comments":
		return set(&limits.MaxComments)
	case "max_escapes":
		return set(&limits.MaxEscapes)
	case "max_unicode_escapes":
		return set(&limits.MaxUnicodeEscapes)
	case "max_java_code_units_per_string":
		return set(&limits.MaxJavaCodeUnitsPerString)
	case "max_total_java_code_units":
		return set(&limits.MaxTotalJavaCodeUnits)
	case "max_duplicate_group_members":
		return set(&limits.MaxDuplicateGroupMembers)
	case "max_recovery_regions":
		return set(&limits.MaxRecoveryRegions)
	}
	return false
}

func runPropertiesProjectionLimits(vector *caseData, report *SuiteReport) {
	doc, message := propertiesParseCase(vector)
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
	failedCount := 0
	for _, nameValue := range names {
		name, ok := nameValue.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "limits must be String"})
			return
		}
		limits := properties.DefaultProjectionLimits()
		switch string(name) {
		case "max_source_associations":
			limits.MaxSourceAssociations = 0
		case "max_value_nodes":
			limits.MaxValueNodes = 1
		case "max_provenance_units":
			limits.MaxProvenanceUnits = 1
		default:
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "unknown projection limit " + string(name)})
			return
		}
		result := doc.Project(properties.BestExactEntryMapping().WithLimits(limits))
		if result.Complete != nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "projection limit " + string(name) + " did not fail"})
			return
		}
		if len(result.Failed.Diagnostics) > 0 &&
			result.Failed.Diagnostics[0].Code == expectedString(vector, report, "code") {
			failedCount++
		}
	}
	duplicateSource, ok := stringField(vector.Input, "duplicate_source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.duplicate_source"})
		return
	}
	duplicate, message := propertiesParseReaderText(duplicateSource)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	reportLimits := properties.DefaultProjectionLimits()
	reportLimits.MaxReportEntries = 0
	duplicateResult := duplicate.Project(properties.RequireObject(properties.DuplicatePolicyFirstWins).
		WithLimits(reportLimits))
	if duplicateResult.Complete != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "report limit did not fail"})
		return
	}
	if len(duplicateResult.Failed.Diagnostics) > 0 &&
		duplicateResult.Failed.Diagnostics[0].Code == expectedString(vector, report, "code") {
		failedCount++
	}
	if uint64(failedCount) != expectedInt(vector, report, "failed_count") {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "projection limit failed count was " + itoaYaml(failedCount)})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPropertiesOperationRegistry(vector *caseData, report *SuiteReport) {
	profiles, ok := sequenceField(vector.Input, "profiles")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.profiles"})
		return
	}
	for _, profileValue := range profiles {
		name, ok := profileValue.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "profiles must be String"})
			return
		}
		profile, ok := propertiesProfileFromName(string(name))
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "unknown Java Properties profile " + string(name)})
			return
		}
		registry := properties.NewFormatOperationRegistry(profile)
		operations := make([]string, 0, len(registry.Operations()))
		supported := 0
		for _, descriptor := range registry.Operations() {
			operations = append(operations, descriptor.ID.ID()+"@"+propertiesItoaU64(uint64(descriptor.ID.Version())))
			if descriptor.Support == properties.OperationSupportSupported {
				supported++
			}
		}
		if !stringSlicesEqual(operations, expectedStrings(vector, report, "operations")) ||
			uint64(supported) != expectedInt(vector, report, "supported") {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "operation registry differed for " + string(name)})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

// propertiesProfileFromName resolves one exact profile identifier.
func propertiesProfileFromName(name string) (properties.PropertiesProfile, bool) {
	switch name {
	case "java-properties.reader@1":
		return properties.PropertiesReaderV1, true
	case "java-properties.latin1@1":
		return properties.PropertiesLatin1V1, true
	}
	return properties.PropertiesProfile{}, false
}

// propertiesExactCoverage verifies exhaustive contiguous syntax coverage.
func propertiesExactCoverage(doc *properties.Document) (bool, bool) {
	pieces := doc.LosslessStructuralIndex().Pieces()
	kinds := doc.LosslessSyntaxKinds()
	if doc.Source().IsEmpty() {
		return len(pieces) == 0, len(pieces) == len(kinds)
	}
	if len(pieces) != len(kinds) || len(pieces) == 0 ||
		pieces[0].Span().StartByte() != 0 ||
		pieces[len(pieces)-1].Span().EndByte() != doc.Source().Len() {
		return false, len(pieces) == len(kinds)
	}
	for index := 1; index < len(pieces); index++ {
		if pieces[index-1].Span().EndByte() != pieces[index].Span().StartByte() {
			return false, true
		}
	}
	return true, true
}

func propertiesUnicodeKeys(doc *properties.Document) ([]string, bool) {
	keys := make([]string, 0, len(doc.Properties()))
	for _, property := range doc.Properties() {
		key, err := property.Key().ToUnicode()
		if err != nil {
			return nil, false
		}
		keys = append(keys, key)
	}
	return keys, true
}

func propertiesUnicodeValues(doc *properties.Document) ([]string, bool) {
	values := make([]string, 0, len(doc.Properties()))
	for _, property := range doc.Properties() {
		value, err := property.Value().ToUnicode()
		if err != nil {
			return nil, false
		}
		values = append(values, value)
	}
	return values, true
}

func expectedString(vector *caseData, report *SuiteReport, name string) string {
	value, ok := stringField(vector.Expected, name)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected." + name})
		return ""
	}
	return value
}

func expectedInt(vector *caseData, report *SuiteReport, name string) uint64 {
	value, ok := integerField(vector.Expected, name)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected." + name})
		return 0
	}
	return value
}

func expectedBool(vector *caseData, report *SuiteReport, name string) bool {
	value, ok := booleanField(vector.Expected, name)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected." + name})
		return false
	}
	return value
}

func expectedStrings(vector *caseData, report *SuiteReport, name string) []string {
	items, ok := sequenceField(vector.Expected, name)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected." + name})
		return nil
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "expected." + name + " must be String"})
			return nil
		}
		values = append(values, string(text))
	}
	return values
}

func expectedInts(vector *caseData, report *SuiteReport, name string) []uint64 {
	items, ok := sequenceField(vector.Expected, name)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected." + name})
		return nil
	}
	values := make([]uint64, 0, len(items))
	for _, item := range items {
		integer, ok := item.(core.Integer)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "expected." + name + " must be Integer"})
			return nil
		}
		values = append(values, integer.Int().Uint64())
	}
	return values
}

func expectedPairs(vector *caseData, report *SuiteReport, name string) [][2]string {
	items, ok := sequenceField(vector.Expected, name)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected." + name})
		return nil
	}
	pairs := make([][2]string, 0, len(items))
	for _, item := range items {
		pair, ok := item.(*core.Array)
		if !ok || len(pair.Items()) != 2 {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "expected." + name + " pair must be a two-item Sequence"})
			return nil
		}
		key, ok := pair.Items()[0].(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "expected pair key must be String"})
			return nil
		}
		value, ok := pair.Items()[1].(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "expected pair value must be String"})
			return nil
		}
		pairs = append(pairs, [2]string{string(key), string(value)})
	}
	return pairs
}

func stringSlicesEqual(left, right []string) bool {
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

func uint64SlicesEqual(left, right []uint64) bool {
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

func stringPairSlicesEqual(left, right [][2]string) bool {
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

// propertiesItoaU64 renders one unsigned integer without imports.
func propertiesItoaU64(value uint64) string {
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
