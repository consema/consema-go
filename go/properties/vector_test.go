package properties

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file drives all 25 published cases of the shared
// `consema.java-properties.conformance@1` vector suite through the public
// package API (https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §4.2; the conformance runner
// in go/conformance/java_properties_v1.go executes the same facts through
// the same API). The vector file is the authority; no expectation literal
// lives here.

// vectorRoot resolves the repository conformance directory from the
// package test working directory.
func vectorRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "conformance", "vectors"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func loadPropertiesVector(t *testing.T) map[string]map[string]interface{} {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(vectorRoot(t), "java-properties-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var suite struct {
		Cases []struct {
			ID         string                 `json:"id"`
			Capability string                 `json:"capability"`
			Input      map[string]interface{} `json:"input"`
			Expected   map[string]interface{} `json:"expected"`
		} `json:"cases"`
	}
	if err := decodeVectorJSON(raw, &suite); err != nil {
		t.Fatal(err)
	}
	if len(suite.Cases) != 25 {
		t.Fatalf("case count %d != 25", len(suite.Cases))
	}
	cases := make(map[string]map[string]interface{}, len(suite.Cases))
	for _, vector := range suite.Cases {
		entry := make(map[string]interface{}, 2)
		entry["input"] = vector.Input
		entry["expected"] = vector.Expected
		cases[vector.ID] = entry
	}
	return cases
}

// decodeVectorJSON decodes the strict vector JSON without float
// round-trips (integers stay exact).
func decodeVectorJSON(raw []byte, target interface{}) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var decoded interface{}
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	converted, err := exactVectorJSON(decoded)
	if err != nil {
		return err
	}
	reEncoded, err := json.Marshal(converted)
	if err != nil {
		return err
	}
	return json.Unmarshal(reEncoded, target)
}

// exactVectorJSON converts json.Number leaves into int64 or float64.
func exactVectorJSON(value interface{}) (interface{}, error) {
	switch item := value.(type) {
	case json.Number:
		if integer, err := item.Int64(); err == nil {
			return integer, nil
		}
		number, err := item.Float64()
		if err != nil {
			return nil, err
		}
		return number, nil
	case []interface{}:
		items := make([]interface{}, 0, len(item))
		for _, element := range item {
			converted, err := exactVectorJSON(element)
			if err != nil {
				return nil, err
			}
			items = append(items, converted)
		}
		return items, nil
	case map[string]interface{}:
		entries := make(map[string]interface{}, len(item))
		for key, element := range item {
			converted, err := exactVectorJSON(element)
			if err != nil {
				return nil, err
			}
			entries[key] = converted
		}
		return entries, nil
	}
	return value, nil
}

func TestPropertiesVectorsAllTwentyFive(t *testing.T) {
	cases := loadPropertiesVector(t)
	for id, vector := range cases {
		t.Run(id, func(t *testing.T) {
			input := vector["input"].(map[string]interface{})
			expected := vector["expected"].(map[string]interface{})
			runPropertiesVectorCase(t, id, input, expected)
		})
	}
}

func runPropertiesVectorCase(t *testing.T, id string,
	input, expected map[string]interface{}) {
	t.Helper()
	switch id {
	case "formation.reader-lines-escapes-duplicates":
		vectorFormationReader(t, input, expected)
	case "formation.empty-blank-comment-empty-key":
		vectorFormationBasicMatrix(t, input, expected)
	case "formation.mixed-line-terminators":
		vectorFormationTerminators(t, input, expected)
	case "formation.continuation-and-backslash-parity":
		vectorFormationContinuations(t, input, expected)
	case "formation.escape-and-java-utf16-matrix":
		vectorFormationJavaStrings(t, input, expected)
	case "formation.malformed-unicode-recovery-matrix":
		vectorFormationRecoveryMatrix(t, input, expected)
	case "formation.reader-explicit-encodings":
		vectorFormationReaderEncodings(t, input, expected)
	case "formation.latin1-byte-and-bom-content":
		vectorFormationLatin1(t, input, expected)
	case "formation.recovery-never-publishes-partial-operation":
		vectorRecoveredIsAtomic(t, input, expected)
	case "formation.malformed-escape-in-key":
		vectorFormationMalformedEscapeInKey(t, input, expected)
	case "formation.invalid-encoding-sequence", "formation.bom-conflict":
		vectorFormationFatalEncoding(t, input, expected)
	case "query.native-duplicates-and-escape-ownership":
		vectorNativeQuery(t, input, expected)
	case "query.logical-and-syntax-order":
		vectorLogicalSyntaxQuery(t, input, expected)
	case "query.validation-limit-cancellation":
		vectorQueryFailures(t, input, expected)
	case "projection.exact-duplicates-and-fragments":
		vectorProjectionExact(t, input, expected)
	case "projection.unpaired-and-recovered-atomic-failure":
		vectorProjectionFailures(t, input, expected)
	case "projection.explicit-jdk-table-collapse":
		vectorProjectionCollapse(t, input, expected)
	case "materialization.canonical-styles-encodings-and-closure":
		vectorMaterializationStyles(t, input, expected)
	case "materialization.atomic-failures-and-limits":
		vectorMaterializationLimits(t, input, expected)
	case "edit.all-five-operations":
		vectorEditAllOperations(t, input, expected)
	case "edit.dry-run-patch-proof-conflict-atomicity":
		vectorEditAuditArtifacts(t, input, expected)
	case "resource.formation-limit-matrix":
		vectorFormationLimits(t, input, expected)
	case "resource.projection-limit-matrix":
		vectorProjectionLimits(t, input, expected)
	case "registry.frozen-five-operation-surface":
		vectorOperationRegistry(t, input, expected)
	default:
		t.Fatalf("runner does not recognize published Properties case %s", id)
	}
}

// -- vector value helpers ------------------------------------------------

func vString(t *testing.T, object map[string]interface{}, name string) string {
	t.Helper()
	value, ok := object[name].(string)
	if !ok {
		t.Fatalf("missing or non-String field %s", name)
	}
	return value
}

func vInt(t *testing.T, object map[string]interface{}, name string) uint64 {
	t.Helper()
	value, ok := object[name].(int64)
	if !ok {
		number, ok := object[name].(float64)
		if !ok {
			t.Fatalf("missing or non-Integer field %s", name)
		}
		return uint64(number)
	}
	return uint64(value)
}

func vBool(t *testing.T, object map[string]interface{}, name string) bool {
	t.Helper()
	value, ok := object[name].(bool)
	if !ok {
		t.Fatalf("missing or non-Boolean field %s", name)
	}
	return value
}

func vStrings(t *testing.T, object map[string]interface{}, name string) []string {
	t.Helper()
	items, ok := object[name].([]interface{})
	if !ok {
		t.Fatalf("missing or non-Sequence field %s", name)
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("field %s item is not String", name)
		}
		values = append(values, text)
	}
	return values
}

func vInts(t *testing.T, object map[string]interface{}, name string) []uint64 {
	t.Helper()
	items, ok := object[name].([]interface{})
	if !ok {
		t.Fatalf("missing or non-Sequence field %s", name)
	}
	values := make([]uint64, 0, len(items))
	for _, item := range items {
		switch number := item.(type) {
		case int64:
			values = append(values, uint64(number))
		case float64:
			values = append(values, uint64(number))
		default:
			t.Fatalf("field %s item is not Integer", name)
		}
	}
	return values
}

func vPairs(t *testing.T, object map[string]interface{}, name string) [][2]string {
	t.Helper()
	items, ok := object[name].([]interface{})
	if !ok {
		t.Fatalf("missing or non-Sequence field %s", name)
	}
	pairs := make([][2]string, 0, len(items))
	for _, item := range items {
		pair, ok := item.([]interface{})
		if !ok || len(pair) != 2 {
			t.Fatalf("field %s pair is not a two-item Sequence", name)
		}
		key, ok := pair[0].(string)
		if !ok {
			t.Fatalf("field %s pair key is not String", name)
		}
		value, ok := pair[1].(string)
		if !ok {
			t.Fatalf("field %s pair value is not String", name)
		}
		pairs = append(pairs, [2]string{key, value})
	}
	return pairs
}

func vInputStrings(t *testing.T, input map[string]interface{}, name string) []string {
	t.Helper()
	return vStrings(t, input, name)
}

func vDecodeHex(t *testing.T, text string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(text)
	if err != nil {
		t.Fatalf("invalid hex %q", text)
	}
	return raw
}

func vHex(raw []byte) string { return hex.EncodeToString(raw) }

func vectorParseReader(t *testing.T, source string) *Document {
	t.Helper()
	doc, failure := ParseReader([]byte(source), document.Utf8Encoding(),
		DefaultPropertiesParseLimits())
	if failure != nil {
		t.Fatalf("formation failed: %s", failure.Code())
	}
	return doc
}

func vectorParseCase(t *testing.T, input map[string]interface{}) *Document {
	t.Helper()
	profile, ok := input["profile"].(string)
	if !ok {
		t.Fatalf("missing input.profile")
	}
	source := vString(t, input, "source")
	if profile == "java-properties.latin1@1" {
		doc, failure := ParseLatin1([]byte(source), DefaultPropertiesParseLimits())
		if failure != nil {
			t.Fatalf("formation failed: %s", failure.Code())
		}
		return doc
	}
	return vectorParseReader(t, source)
}

func vectorExactCoverage(t *testing.T, doc *Document) bool {
	t.Helper()
	pieces := doc.LosslessStructuralIndex().Pieces()
	if doc.Source().IsEmpty() {
		return len(pieces) == 0
	}
	if len(pieces) == 0 || pieces[0].Span().StartByte() != 0 ||
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

func vectorUnicodeKeys(t *testing.T, doc *Document) []string {
	t.Helper()
	keys := make([]string, 0, len(doc.properties))
	for _, property := range doc.properties {
		key, err := property.key.ToUnicode()
		if err != nil {
			t.Fatalf("property key is not well-formed Unicode")
		}
		keys = append(keys, key)
	}
	return keys
}

func vectorUnicodeValues(t *testing.T, doc *Document) []string {
	t.Helper()
	values := make([]string, 0, len(doc.properties))
	for _, property := range doc.properties {
		value, err := property.value.ToUnicode()
		if err != nil {
			t.Fatalf("property value is not well-formed Unicode")
		}
		values = append(values, value)
	}
	return values
}

func vectorExecutableNative(t *testing.T, expression *protocol.QueryExpression) *protocol.ExecutableQuery {
	t.Helper()
	definition := protocol.NewQueryDefinition(protocol.DomainJavaPropertiesNativeV1()).
		WithExpression(expression)
	validated, failure := definition.Validate()
	if failure != nil {
		t.Fatalf("validation: %s", failure.Code())
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	bound, failure := validated.Bind(capabilities)
	if failure != nil {
		t.Fatalf("binding: %s", failure.Code())
	}
	return bound
}

func vectorExecutableSyntax(t *testing.T, expression *protocol.QueryExpression) *protocol.ExecutableQuery {
	t.Helper()
	definition := protocol.NewQueryDefinition(protocol.DomainJavaPropertiesLosslessSyntaxV1()).
		WithExpression(expression)
	validated, failure := definition.Validate()
	if failure != nil {
		t.Fatalf("validation: %s", failure.Code())
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	bound, failure := validated.Bind(capabilities)
	if failure != nil {
		t.Fatalf("binding: %s", failure.Code())
	}
	return bound
}

// -- formation cases -----------------------------------------------------

func vectorFormationReader(t *testing.T, input, expected map[string]interface{}) {
	doc := vectorParseCase(t, input)
	requireEqual(t, doc.formationStatus.String(), vString(t, expected, "formation"))
	requireEqual(t, len(doc.naturalLines), int(vInt(t, expected, "natural_lines")))
	requireEqual(t, len(doc.logicalLines), int(vInt(t, expected, "logical_lines")))
	requireEqual(t, len(doc.comments), int(vInt(t, expected, "comments")))
	requireEqual(t, len(doc.properties), int(vInt(t, expected, "properties")))
	requireEqual(t, len(doc.escapes), int(vInt(t, expected, "escapes")))
	requireSlices(t, vectorUnicodeKeys(t, doc), vStrings(t, expected, "keys"))
	requireSlices(t, vectorUnicodeValues(t, doc), vStrings(t, expected, "values"))
	states := make([]string, 0, len(doc.properties))
	for _, property := range doc.properties {
		states = append(states, string(property.valueState))
	}
	requireSlices(t, states, vStrings(t, expected, "states"))
	duplicateGroup := false
	if len(doc.properties) > 2 {
		first := doc.properties[1].duplicateGroup
		second := doc.properties[2].duplicateGroup
		duplicateGroup = first != nil && second != nil && *first == *second
	}
	requireEqual(t, duplicateGroup, vBool(t, expected, "duplicate_group"))
	requireEqual(t, vectorExactCoverage(t, doc), vBool(t, expected, "exact_coverage"))
}

func vectorFormationBasicMatrix(t *testing.T, input, expected map[string]interface{}) {
	samples := vInputStrings(t, input, "samples")
	formations := vStrings(t, expected, "formations")
	propertiesCounts := vInts(t, expected, "properties")
	comments := vInts(t, expected, "comments")
	requireEqual(t, len(samples), len(formations))
	requireEqual(t, len(samples), len(propertiesCounts))
	requireEqual(t, len(samples), len(comments))
	for index, source := range samples {
		doc := vectorParseReader(t, source)
		requireEqual(t, doc.formationStatus.String(), formations[index])
		requireEqual(t, len(doc.properties), int(propertiesCounts[index]))
		requireEqual(t, len(doc.comments), int(comments[index]))
		if !vectorExactCoverage(t, doc) {
			t.Fatalf("sample %d coverage", index)
		}
	}
}

func vectorFormationTerminators(t *testing.T, input, expected map[string]interface{}) {
	doc := vectorParseCase(t, input)
	raw := doc.Render()
	terminators := make([]string, 0, len(doc.naturalLines))
	for _, line := range doc.naturalLines {
		breakSpan := line.lineBreakSpan
		if breakSpan == nil {
			terminators = append(terminators, "Eof")
			continue
		}
		switch string(raw[breakSpan.StartByte():breakSpan.EndByte()]) {
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
	requireEqual(t, len(doc.naturalLines), int(vInt(t, expected, "natural_lines")))
	requireEqual(t, len(doc.logicalLines), int(vInt(t, expected, "logical_lines")))
	requireEqual(t, len(doc.properties), int(vInt(t, expected, "properties")))
	requireSlices(t, terminators, vStrings(t, expected, "terminators"))
	requireEqual(t, vectorExactCoverage(t, doc), vBool(t, expected, "exact_coverage"))
}

func vectorFormationContinuations(t *testing.T, input, expected map[string]interface{}) {
	samples, ok := input["samples"].([]interface{})
	if !ok {
		t.Fatalf("missing input.samples")
	}
	for index, item := range samples {
		sample, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("sample must be Object")
		}
		doc := vectorParseReader(t, vString(t, sample, "source"))
		if doc.formationStatus.String() != "Complete" {
			t.Fatalf("sample %d not complete", index)
		}
		if len(doc.properties) == 0 {
			t.Fatalf("sample %d has no property", index)
		}
		requireEqual(t, vHex(doc.properties[0].value.Utf16beBytes()),
			vString(t, sample, "value_hex"))
		requireEqual(t, len(doc.naturalLines), int(vInt(t, sample, "natural_lines")))
		requireEqual(t, len(doc.logicalLines), int(vInt(t, sample, "logical_lines")))
		if !vectorExactCoverage(t, doc) {
			t.Fatalf("sample %d coverage", index)
		}
	}
}

func vectorFormationJavaStrings(t *testing.T, input, expected map[string]interface{}) {
	doc := vectorParseCase(t, input)
	values := make([]string, 0, len(doc.properties))
	statuses := make([]string, 0, len(doc.properties))
	for _, property := range doc.properties {
		values = append(values, vHex(property.value.Utf16beBytes()))
		statuses = append(statuses, string(property.value.status))
	}
	escapeKinds := make([]string, 0, len(doc.escapes))
	for _, escape := range doc.escapes {
		escapeKinds = append(escapeKinds, string(escape.kind))
	}
	requireSlices(t, values, vStrings(t, expected, "value_utf16be_hex"))
	requireSlices(t, statuses, vStrings(t, expected, "statuses"))
	requireSlices(t, escapeKinds, vStrings(t, expected, "escape_kinds"))
}

func vectorFormationRecoveryMatrix(t *testing.T, input, expected map[string]interface{}) {
	samples := vInputStrings(t, input, "samples")
	formations := vStrings(t, expected, "formations")
	propertyCounts := vInts(t, expected, "property_counts")
	errorCounts := vInts(t, expected, "error_counts")
	code := vString(t, expected, "code")
	for index, source := range samples {
		doc := vectorParseReader(t, source)
		requireEqual(t, doc.formationStatus.String(), formations[index])
		requireEqual(t, len(doc.properties), int(propertyCounts[index]))
		requireEqual(t, len(doc.errorLines), int(errorCounts[index]))
		if len(doc.errorLines) > 0 {
			requireEqual(t, doc.errorLines[0].code, code)
		}
		if index+1 == len(samples) {
			value, err := doc.properties[0].value.ToUnicode()
			if err != nil {
				t.Fatalf("uppercase U value is not Unicode")
			}
			requireEqual(t, value, vString(t, expected, "uppercase_u_value"))
		}
	}
}

func vectorSourceEncoding(t *testing.T, name string) document.SourceEncoding {
	t.Helper()
	switch name {
	case "Utf8":
		return document.Utf8Encoding()
	case "Utf16Le":
		return document.Utf16LeEncoding()
	case "Utf16Be":
		return document.Utf16BeEncoding()
	case "Latin1":
		return document.Latin1Encoding()
	}
	if strings.HasPrefix(name, "WindowsCodePage(") && strings.HasSuffix(name, ")") {
		var number uint16
		for _, digit := range name[len("WindowsCodePage(") : len(name)-1] {
			number = number*10 + uint16(digit-'0')
		}
		page, ok := document.WindowsCodePageFromNumber(number)
		if !ok {
			t.Fatalf("unsupported code page %d", number)
		}
		return document.WindowsCodePageEncoding(page)
	}
	t.Fatalf("unknown source encoding %s", name)
	return document.SourceEncoding{}
}

func vectorBomName(t *testing.T, doc *Document) string {
	t.Helper()
	bom := doc.source.EncodingFacts().Bom()
	if bom == nil {
		return "None"
	}
	return "Some(" + string(*bom) + ")"
}

func vectorFormationReaderEncodings(t *testing.T, input, expected map[string]interface{}) {
	samples, ok := input["samples"].([]interface{})
	if !ok {
		t.Fatalf("missing input.samples")
	}
	for index, item := range samples {
		sample, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("sample must be Object")
		}
		raw := vDecodeHex(t, vString(t, sample, "source_hex"))
		doc, failure := ParseReader(raw, vectorSourceEncoding(t, vString(t, sample, "encoding")),
			DefaultPropertiesParseLimits())
		if failure != nil {
			t.Fatalf("sample %d failed: %s", index, failure.Code())
		}
		if doc.formationStatus.String() != "Complete" || string(doc.Render()) != string(raw) {
			t.Fatalf("sample %d identity", index)
		}
		if len(doc.properties) == 0 {
			t.Fatalf("sample %d has no property", index)
		}
		key, keyErr := doc.properties[0].key.ToUnicode()
		value, valueErr := doc.properties[0].value.ToUnicode()
		if keyErr != nil || valueErr != nil {
			t.Fatalf("sample %d value is not Unicode", index)
		}
		requireEqual(t, key, vString(t, sample, "key"))
		requireEqual(t, value, vString(t, sample, "value"))
		requireEqual(t, vectorBomName(t, doc), vString(t, sample, "bom"))
		if !vectorExactCoverage(t, doc) {
			t.Fatalf("sample %d coverage", index)
		}
	}
}

// vectorFormationMalformedEscapeInKey drives formation.malformed-escape-
// in-key: a malformed `\uXXXX` escape in the KEY position recovers the
// logical line without a partial property and the error line carries the
// family parse code (parser.rs).
func vectorFormationMalformedEscapeInKey(t *testing.T, input, expected map[string]interface{}) {
	doc := vectorParseCase(t, input)
	requireEqual(t, doc.formationStatus.String(), vString(t, expected, "formation"))
	requireEqual(t, len(doc.properties), int(vInt(t, expected, "properties")))
	requireEqual(t, len(doc.errorLines), int(vInt(t, expected, "error_lines")))
	if len(doc.errorLines) == 0 {
		t.Fatalf("missing error line")
	}
	requireEqual(t, doc.errorLines[0].code, vString(t, expected, "code"))
}

// vectorFormationFatalEncoding drives formation.invalid-encoding-sequence /
// formation.bom-conflict: bytes that cannot be decoded under the explicit
// Reader encoding (`core.source.invalid-sequence@1`) or a BOM that
// contradicts it (`core.source.encoding-conflict@1`) fail the whole parse
// fatally before any document forms (parser.rs).
func vectorFormationFatalEncoding(t *testing.T, input, expected map[string]interface{}) {
	raw := vDecodeHex(t, vString(t, input, "source_hex"))
	_, failure := ParseReader(raw, vectorSourceEncoding(t, vString(t, input, "encoding")),
		DefaultPropertiesParseLimits())
	if failure == nil {
		t.Fatalf("parse must fail fatally")
	}
	requireEqual(t, failure.Code(), vString(t, expected, "code"))
}

func vectorFormationLatin1(t *testing.T, input, expected map[string]interface{}) {
	raw := vDecodeHex(t, vString(t, input, "source_hex"))
	doc, failure := ParseLatin1(raw, DefaultPropertiesParseLimits())
	if failure != nil {
		t.Fatalf("formation failed: %s", failure.Code())
	}
	if len(doc.properties) == 0 {
		t.Fatalf("no property")
	}
	requireEqual(t, vHex(doc.properties[0].key.Utf16beBytes()),
		vString(t, expected, "key_utf16be_hex"))
	requireEqual(t, vHex(doc.properties[0].value.Utf16beBytes()),
		vString(t, expected, "value_utf16be_hex"))
	requireEqual(t, vectorBomName(t, doc), vString(t, expected, "bom"))
	hasBomKind := false
	for _, kind := range doc.syntaxKinds {
		if kind == SyntaxKindBom {
			hasBomKind = true
			break
		}
	}
	requireEqual(t, hasBomKind, vBool(t, expected, "bom_syntax"))
	requireEqual(t, vectorExactCoverage(t, doc), vBool(t, expected, "exact_coverage"))
}

func vectorRecoveredIsAtomic(t *testing.T, input, expected map[string]interface{}) {
	doc := vectorParseCase(t, input)
	projectionCode := ""
	if result := doc.Project(BestExactEntryMapping()); result.Failed != nil &&
		len(result.Failed.Diagnostics) > 0 {
		projectionCode = result.Failed.Diagnostics[0].Code
	}
	builder := NewEditTransactionBuilder(doc)
	_, editFailure := doc.Commit(builder.Build())
	editCode := ""
	if editFailure != nil {
		editCode = editFailure.Code()
	}
	requireEqual(t, doc.formationStatus.String(), vString(t, expected, "formation"))
	requireSlices(t, vectorUnicodeKeys(t, doc), vStrings(t, expected, "keys"))
	requireEqual(t, len(doc.errorLines), int(vInt(t, expected, "error_lines")))
	if len(doc.errorLines) == 0 {
		t.Fatalf("no error lines")
	}
	requireEqual(t, doc.errorLines[0].code, vString(t, expected, "code"))
	requireEqual(t, projectionCode, vString(t, expected, "projection_code"))
	requireEqual(t, editCode, vString(t, expected, "edit_code"))
}

// -- query cases ---------------------------------------------------------

func vectorNativeQuery(t *testing.T, input, expected map[string]interface{}) {
	doc := vectorParseCase(t, input)
	keyBytes := vDecodeHex(t, vString(t, input, "key_utf16be_hex"))
	take := vInt(t, input, "take")
	duplicates := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.document-properties", 1)).
		Then(protocol.NewOperatorCall("properties.property-key-equals", 1).
			WithArgument("key", core.NewBytes(keyBytes))).
		Then(protocol.NewOperatorCall("core.take", 1).
			WithArgument("count", core.NewInteger(big.NewInt(int64(take))))).
		Then(protocol.NewOperatorCall("properties.duplicate-group", 1))
	duplicateMatches, failure := ExecutePropertiesQuery(context.Background(),
		vectorExecutableNative(t, duplicates), doc, protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("query: %s", failure.Code())
	}
	escapes := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.document-properties", 1)).
		Then(protocol.NewOperatorCall("core.take", 1).
			WithArgument("count", core.NewInteger(big.NewInt(int64(take))))).
		Then(protocol.NewOperatorCall("properties.property-escapes", 1))
	escapeMatches, failure := ExecutePropertiesQuery(context.Background(),
		vectorExecutableNative(t, escapes), doc, protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("query: %s", failure.Code())
	}
	allGrouped := len(duplicateMatches) > 0
	for _, match := range duplicateMatches {
		if match.Kind != PropertiesMatchProperty || match.DuplicateGroup == nil {
			allGrouped = false
			break
		}
	}
	allEscapes := len(escapeMatches) > 0
	for _, match := range escapeMatches {
		if match.Kind != PropertiesMatchEscape {
			allEscapes = false
			break
		}
	}
	requireEqual(t, len(duplicateMatches), int(vInt(t, expected, "duplicate_matches")))
	requireEqual(t, len(escapeMatches), int(vInt(t, expected, "escape_matches")))
	requireEqual(t, allGrouped, vBool(t, expected, "duplicate_group"))
	requireEqual(t, allEscapes, vBool(t, expected, "escape_roles"))
	requireEqual(t, "Completed", vString(t, expected, "terminal"))
}

func vectorLogicalSyntaxQuery(t *testing.T, input, expected map[string]interface{}) {
	logical := vectorParseReader(t, vString(t, input, "logical_source"))
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.logical-lines", 1)).
		Then(protocol.NewOperatorCall("properties.logical-line-natural-lines", 1))
	logicalMatches, failure := ExecutePropertiesQuery(context.Background(),
		vectorExecutableNative(t, expression), logical, protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("query: %s", failure.Code())
	}
	ordinals := make([]uint64, 0, len(logicalMatches))
	for _, match := range logicalMatches {
		if match.Kind != PropertiesMatchNaturalLine {
			t.Fatalf("logical query returned non-natural line")
		}
		ordinals = append(ordinals, uint64(match.Ordinal))
	}
	requireSlicesUint64(t, ordinals, vInts(t, expected, "natural_ordinals"))

	syntax := vectorParseReader(t, vString(t, input, "syntax_source"))
	rawBranch := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.syntax-raw-bytes-equals", 1).
			WithArgument("bytes", core.NewBytes(vDecodeHex(t, vString(t, input, "raw_hex")))))
	textBranch := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.syntax-text-equals", 1).
			WithArgument("text", core.String(vString(t, input, "text"))))
	utf16Branch := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.syntax-utf16be-equals", 1).
			WithArgument("code_units", core.NewBytes(vDecodeHex(t, vString(t, input, "utf16be_hex")))))
	merge := &protocol.QueryExpression{Kind: protocol.ExpressionStructureOrderMerge,
		Branches: []*protocol.QueryExpression{rawBranch, textBranch, utf16Branch}}
	syntaxMatches, failure := ExecutePropertiesSyntaxQuery(context.Background(),
		vectorExecutableSyntax(t, merge), syntax, protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("syntax query: %s", failure.Code())
	}
	kinds := make([]string, 0, len(syntaxMatches))
	increasing := true
	for index, match := range syntaxMatches {
		kinds = append(kinds, match.kind.AsStr())
		if string(match.node.Role()) != "PropertiesSyntaxPiece" {
			t.Fatalf("syntax role is %s", match.node.Role())
		}
		if index > 0 && syntaxMatches[index-1].ordinal >= match.ordinal {
			increasing = false
		}
	}
	requireSlices(t, kinds, vStrings(t, expected, "syntax_kinds"))
	requireEqual(t, increasing, vBool(t, expected, "strictly_increasing_ordinals"))
}

func vectorQueryFailures(t *testing.T, input, expected map[string]interface{}) {
	invalid := protocol.NewQueryDefinition(protocol.DomainJavaPropertiesNativeV1()).
		WithExpression((&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
			Then(protocol.NewOperatorCall("properties.document-properties", 1)).
			Then(protocol.NewOperatorCall("properties.property-key-equals", 1).
				WithArgument("key", core.NewBytes([]byte{0}))))
	_, validationFailure := invalid.Validate()
	invalidArgument := ""
	if validationFailure != nil {
		invalidArgument = validationFailure.Argument
	}
	doc := vectorParseCase(t, input)
	all := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.document-properties", 1))
	executable := vectorExecutableNative(t, all)
	limits := protocol.DefaultQueryLimits()
	limits.MaxSteps = 100
	limits.MaxResults = int(vInt(t, input, "max_results"))
	_, queryFailure := ExecutePropertiesQuery(context.Background(), executable, doc, limits)
	limitCode := ""
	if queryFailure != nil {
		limitCode = queryFailure.Code()
	}
	cursorContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	cursor, cursorFailure := ExecutePropertiesQueryCursor(cursorContext, executable, doc,
		protocol.DefaultQueryLimits())
	if cursorFailure != nil {
		t.Fatalf("cursor: %s", cursorFailure.Code())
	}
	firstYielded := cursor.Next() != nil
	cancel()
	exhausted := cursor.Next() == nil
	terminal := ""
	if state := cursor.TerminalState(); state != nil {
		terminal = string(*state)
	}
	requireEqual(t, invalidArgument, vString(t, expected, "invalid_argument"))
	requireEqual(t, limitCode, vString(t, expected, "limit_code"))
	requireEqual(t, firstYielded, vBool(t, expected, "first_yielded"))
	if !exhausted {
		t.Fatalf("cursor not exhausted after cancellation")
	}
	requireEqual(t, terminal, vString(t, expected, "terminal"))
}

// -- projection cases ----------------------------------------------------

func vectorRelationPresent(complete *CompleteProjection, relation ProvenanceRelation) bool {
	for _, entry := range complete.Provenance.Entries() {
		for _, origin := range entry.Origins {
			if origin.Relation == relation {
				return true
			}
		}
	}
	return false
}

func vectorProjectionExact(t *testing.T, input, expected map[string]interface{}) {
	doc := vectorParseCase(t, input)
	result := doc.Project(BestExactEntryMapping())
	if result.Complete == nil {
		t.Fatalf("exact projection failed")
	}
	mapping, ok := result.Complete.Value.(*core.EntryMapping)
	if !ok {
		t.Fatalf("exact projection did not produce EntryMapping")
	}
	keys := make([]string, 0, mapping.Len())
	values := make([]string, 0, mapping.Len())
	for _, entry := range mapping.Entries() {
		key, ok := entry.Key.(core.String)
		if !ok {
			t.Fatalf("projected key is not String")
		}
		value, ok := entry.Value.(core.String)
		if !ok {
			t.Fatalf("projected value is not String")
		}
		keys = append(keys, string(key))
		values = append(values, string(value))
	}
	twoValueFragments := false
	for _, entry := range result.Complete.Provenance.Entries() {
		count := 0
		for _, origin := range entry.Origins {
			if origin.Relation == ProvenanceValueFragment {
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
	requireEqual(t, result.Complete.Fidelity.String(), vString(t, expected, "fidelity"))
	requireSlices(t, keys, vStrings(t, expected, "keys"))
	requireSlices(t, values, vStrings(t, expected, "values"))
	requireEqual(t, len(result.Complete.Report.Events()), int(vInt(t, expected, "events")))
	requireEqual(t, vectorRelationPresent(result.Complete, ProvenanceEscapeDerived),
		vBool(t, expected, "escape_provenance"))
	requireEqual(t, twoValueFragments, vBool(t, expected, "two_value_fragments"))
	requireEqual(t, association, vBool(t, expected, "association_provenance"))
}

func vectorProjectionFailures(t *testing.T, input, expected map[string]interface{}) {
	unpaired := vectorParseReader(t, vString(t, input, "unpaired_source"))
	unpairedResult := unpaired.Project(BestExactEntryMapping())
	if unpairedResult.Complete != nil {
		t.Fatalf("unpaired surrogate projection completed")
	}
	recovered := vectorParseReader(t, vString(t, input, "recovered_source"))
	recoveredResult := recovered.Project(BestExactEntryMapping())
	if recoveredResult.Complete != nil {
		t.Fatalf("recovered projection completed")
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
	emptyReports := len(unpairedResult.Failed.Report.Events()) == 0 &&
		len(recoveredResult.Failed.Report.Events()) == 0
	requireEqual(t, unpairedCode, vString(t, expected, "unpaired_code"))
	requireEqual(t, unpairedStart, vInt(t, expected, "unpaired_start_byte"))
	requireEqual(t, recoveredCode, vString(t, expected, "recovered_code"))
	requireEqual(t, emptyReports, vBool(t, expected, "empty_reports"))
}

func vectorObjectPairs(t *testing.T, value core.Value) [][2]string {
	t.Helper()
	object, ok := value.(*core.Object)
	if !ok {
		t.Fatalf("projected Object missing")
	}
	pairs := make([][2]string, 0, len(object.Entries()))
	for _, entry := range object.Entries() {
		value, ok := entry.Value.(core.String)
		if !ok {
			t.Fatalf("projected Object value not String")
		}
		pairs = append(pairs, [2]string{entry.Key, string(value)})
	}
	return pairs
}

func vectorProjectionCollapse(t *testing.T, input, expected map[string]interface{}) {
	doc := vectorParseCase(t, input)
	uniqueResult := doc.Project(RequireObject(DuplicatePolicyRequireUnique))
	if uniqueResult.Complete != nil {
		t.Fatalf("unique projection accepted duplicates")
	}
	uniqueCode := ""
	if len(uniqueResult.Failed.Diagnostics) > 0 {
		uniqueCode = uniqueResult.Failed.Diagnostics[0].Code
	}
	firstResult := doc.Project(RequireObject(DuplicatePolicyFirstWins))
	if firstResult.Complete == nil {
		t.Fatalf("FirstWins projection failed")
	}
	lastResult := doc.Project(RequireObject(DuplicatePolicyLastWinsJdkTable))
	if lastResult.Complete == nil {
		t.Fatalf("LastWinsJdkTable projection failed")
	}
	firstPairs := vectorObjectPairs(t, firstResult.Complete.Value)
	lastPairs := vectorObjectPairs(t, lastResult.Complete.Value)
	firstEvents := firstResult.Complete.Report.Events()
	eventCode := ""
	if len(firstEvents) > 0 {
		eventCode = firstEvents[0].Code
	}
	requireEqual(t, uniqueCode, vString(t, expected, "unique_code"))
	requireEqual(t, firstResult.Complete.Fidelity.String(), vString(t, expected, "first_fidelity"))
	requireEqual(t, len(firstEvents), int(vInt(t, expected, "events")))
	requireEqual(t, eventCode, vString(t, expected, "event_code"))
	requirePairs(t, firstPairs, vPairs(t, expected, "first_entries"))
	requirePairs(t, lastPairs, vPairs(t, expected, "last_entries"))
	requireEqual(t, vectorRelationPresent(firstResult.Complete, ProvenanceCollapsed),
		vBool(t, expected, "collapsed_provenance"))
}

// -- materialization cases -----------------------------------------------

func vectorMaterializationRequest(profile PropertiesProfile) document.MaterializationRequest {
	if profile == PropertiesLatin1V1 {
		return document.NewMaterializationRequest(
			document.NewProfileId("java-properties.latin1", 1),
			document.NewMaterializationStyleId("java-properties.latin1-canonical", 1)).
			WithEncoding(document.Latin1Encoding())
	}
	return document.NewMaterializationRequest(
		document.NewProfileId("java-properties.reader", 1),
		document.NewMaterializationStyleId("java-properties.reader-canonical", 1))
}

func vectorFlatMapping(t *testing.T, descriptor interface{}) *core.EntryMapping {
	t.Helper()
	items, ok := descriptor.([]interface{})
	if !ok {
		t.Fatalf("mapping descriptor must be Sequence")
	}
	pairs := make([]core.EntryMappingEntry, 0, len(items))
	for _, item := range items {
		pair, ok := item.([]interface{})
		if !ok || len(pair) != 2 {
			t.Fatalf("mapping entry must be a two-item Sequence")
		}
		key, ok := pair[0].(string)
		if !ok {
			t.Fatalf("mapping key must be String")
		}
		value, ok := pair[1].(string)
		if !ok {
			t.Fatalf("mapping value must be String")
		}
		pairs = append(pairs, core.EntryMappingEntry{Key: core.String(key), Value: core.String(value)})
	}
	mapping, err := core.NewEntryMapping(pairs...)
	if err != nil {
		t.Fatalf("EntryMapping construction failed")
	}
	return mapping
}

func vectorMaterializationStyles(t *testing.T, input, expected map[string]interface{}) {
	readerValue := vectorFlatMapping(t, input["reader"])
	readerResult := Materialize(readerValue, vectorMaterializationRequest(PropertiesReaderV1))
	if readerResult.Complete == nil {
		t.Fatalf("Reader materialization failed: %s", readerResult.Failed.Failure.Code())
	}
	latinValue := vectorFlatMapping(t, input["latin1"])
	latinResult := Materialize(latinValue,
		vectorMaterializationRequest(PropertiesLatin1V1).WithNewline(document.NewlineCrLf))
	if latinResult.Complete == nil {
		t.Fatalf("Latin-1 materialization failed: %s", latinResult.Failed.Failure.Code())
	}
	utf16Value := vectorFlatMapping(t, input["utf16be"])
	utf16Result := Materialize(utf16Value,
		vectorMaterializationRequest(PropertiesReaderV1).
			WithEncoding(document.Utf16BeEncoding()).
			WithNewline(document.NewlineCrLf))
	if utf16Result.Complete == nil {
		t.Fatalf("UTF-16BE Reader materialization failed: %s", utf16Result.Failed.Failure.Code())
	}
	cpValue := vectorFlatMapping(t, input["cp1252"])
	cpPage, ok := document.WindowsCodePageFromNumber(1252)
	if !ok {
		t.Fatalf("CP1252 unavailable")
	}
	cpResult := Materialize(cpValue,
		vectorMaterializationRequest(PropertiesReaderV1).
			WithEncoding(document.WindowsCodePageEncoding(cpPage)))
	if cpResult.Complete == nil {
		t.Fatalf("CP1252 Reader materialization failed: %s", cpResult.Failed.Failure.Code())
	}
	closure := true
	for _, pair := range []struct {
		doc   *Document
		input *core.EntryMapping
	}{
		{readerResult.Complete.Document, readerValue},
		{latinResult.Complete.Document, latinValue},
		{utf16Result.Complete.Document, utf16Value},
		{cpResult.Complete.Document, cpValue},
	} {
		projected := pair.doc.Project(BestExactEntryMapping())
		if projected.Complete == nil || !core.Equal(projected.Complete.Value, pair.input) {
			closure = false
			break
		}
	}
	utf16Text, _ := utf16Result.Complete.Document.Source().DecodedText()
	requireEqual(t, string(readerResult.Complete.Document.Render()),
		vString(t, expected, "reader_source"))
	requireEqual(t, string(latinResult.Complete.Document.Render()),
		vString(t, expected, "latin1_source"))
	requireEqual(t, utf16Text, vString(t, expected, "utf16be_decoded"))
	requireEqual(t, vHex(cpResult.Complete.Document.Render()), vString(t, expected, "cp1252_hex"))
	exactFidelity := readerResult.Complete.Fidelity == MaterializationFidelityExact &&
		latinResult.Complete.Fidelity == MaterializationFidelityExact &&
		utf16Result.Complete.Fidelity == MaterializationFidelityExact &&
		cpResult.Complete.Fidelity == MaterializationFidelityExact
	requireEqual(t, exactFidelity, vBool(t, expected, "exact_fidelity"))
	requireEqual(t, closure, vBool(t, expected, "closure"))
}

func vectorMaterializationLimits(t *testing.T, input, expected map[string]interface{}) {
	scalarResult := Materialize(core.String("scalar"),
		vectorMaterializationRequest(PropertiesReaderV1))
	scalarCode := ""
	if scalarResult.Failed != nil {
		scalarCode = scalarResult.Failed.Failure.Code()
	}
	value := vectorFlatMapping(t, input["value"])
	encodingResult := Materialize(value,
		vectorMaterializationRequest(PropertiesLatin1V1).WithEncoding(document.Utf8Encoding()))
	encodingCode := ""
	if encodingResult.Failed != nil {
		encodingCode = encodingResult.Failed.Failure.Code()
	}
	names := vStrings(t, input, "limit_names")
	outcomes := make([]string, 0, len(names))
	for _, name := range names {
		limits := document.DefaultMaterializationLimits()
		switch name {
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
			t.Fatalf("unknown materialization limit %s", name)
		}
		result := Materialize(value,
			vectorMaterializationRequest(PropertiesReaderV1).WithLimits(limits))
		if result.Complete != nil {
			outcomes = append(outcomes, "Complete")
			continue
		}
		if result.Failed.Failure.Code() != vString(t, expected, "limit_code") {
			t.Fatalf("%s returned wrong failure code %s", name, result.Failed.Failure.Code())
		}
		outcomes = append(outcomes, "Failed")
	}
	requireEqual(t, scalarCode, vString(t, expected, "scalar_code"))
	requireEqual(t, encodingCode, vString(t, expected, "encoding_code"))
	requireSlices(t, outcomes, vStrings(t, expected, "limit_outcomes"))
}

// -- edit cases ----------------------------------------------------------

func vectorEditAllOperations(t *testing.T, input, expected map[string]interface{}) {
	source := vString(t, input, "source")
	var outputs []string
	editCounts := make([]int, 0, 5)

	doc := vectorParseReader(t, source)
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticValue(doc.properties[0].node,
		NewJavaStringFromUnicode(vString(t, input, "semantic_value")))
	vectorCollectEdit(t, doc, builder, &outputs, &editCounts)

	doc = vectorParseReader(t, source)
	builder = NewEditTransactionBuilder(doc)
	builder.LiteralValue(doc.properties[0].node, []byte(vString(t, input, "literal_value")))
	vectorCollectEdit(t, doc, builder, &outputs, &editCounts)

	doc = vectorParseReader(t, source)
	builder = NewEditTransactionBuilder(doc)
	builder.InsertProperty(doc.NodeRef(), NewJavaStringFromUnicode(vString(t, input, "new_key")),
		NewJavaStringFromUnicode(vString(t, input, "new_value")), PlacementEnd())
	vectorCollectEdit(t, doc, builder, &outputs, &editCounts)

	doc = vectorParseReader(t, source)
	builder = NewEditTransactionBuilder(doc)
	builder.RemoveProperty(doc.properties[0].node)
	vectorCollectEdit(t, doc, builder, &outputs, &editCounts)

	doc = vectorParseReader(t, source)
	builder = NewEditTransactionBuilder(doc)
	builder.RenameProperty(doc.properties[0].node,
		NewJavaStringFromUnicode(vString(t, input, "renamed_key")))
	vectorCollectEdit(t, doc, builder, &outputs, &editCounts)

	oneEditEach := len(editCounts) == 5
	for _, count := range editCounts {
		if count != 1 {
			oneEditEach = false
			break
		}
	}
	requireSlices(t, outputs, vStrings(t, expected, "outputs"))
	requireEqual(t, oneEditEach, vBool(t, expected, "one_source_edit_each"))
}

func vectorCollectEdit(t *testing.T, doc *Document, builder *EditTransactionBuilder,
	outputs *[]string, editCounts *[]int) {
	t.Helper()
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("edit failed: %s", failure.Name())
	}
	*outputs = append(*outputs, string(commit.Document.Render()))
	*editCounts = append(*editCounts, len(commit.ChangeSet.SourceEdits()))
}

func vectorEditAuditArtifacts(t *testing.T, input, expected map[string]interface{}) {
	doc := vectorParseCase(t, input)
	first := doc.properties[0].node
	second := doc.properties[1].node
	builder := NewEditTransactionBuilder(doc)
	builder.RenameProperty(first, NewJavaStringFromUnicode(vString(t, input, "rename")))
	builder.SemanticValue(second, NewJavaStringFromUnicode(vString(t, input, "value")))
	transaction := builder.Build()
	plan, planFailure := doc.DryRun(transaction, vString(t, input, "source_id"))
	if planFailure != nil {
		t.Fatalf("dry run: %s", planFailure.Name())
	}
	commit, commitFailure := doc.Commit(transaction)
	if commitFailure != nil {
		t.Fatalf("commit: %s", commitFailure.Name())
	}
	replay, replayErr := commit.SourcePatch.Apply(doc.Source(), document.DefaultSourcePatchLimits())
	if replayErr != nil {
		t.Fatalf("patch replay failed: %s", replayErr.Error())
	}
	proofErr := commit.UntouchedProof.Verify(doc.Source(), commit.Document.Source(),
		commit.SourcePatch.Replacements())
	conflict := NewEditTransactionBuilder(doc)
	conflict.SemanticValue(first, NewJavaStringFromUnicode("x"))
	conflict.RenameProperty(first, NewJavaStringFromUnicode("renamed"))
	_, conflictFailure := doc.Commit(conflict.Build())
	conflictCode := ""
	if conflictFailure != nil {
		conflictCode = conflictFailure.Code()
	}
	source := vString(t, input, "source")
	requireEqual(t, string(commit.Document.Render()), vString(t, expected, "source"))
	requireEqual(t, len(commit.ChangeSet.SourceEdits()), int(vInt(t, expected, "edit_count")))
	requireEqual(t, len(plan.Operations()), int(vInt(t, expected, "dry_run_operations")))
	requireEqual(t, string(replay.Bytes()) == string(commit.Document.Render()),
		vBool(t, expected, "patch_replays"))
	requireEqual(t, proofErr == nil, vBool(t, expected, "proof_verifies"))
	requireEqual(t, conflictCode, vString(t, expected, "conflict_code"))
	requireEqual(t, string(doc.Render()) == source, vBool(t, expected, "base_unchanged"))
}

// -- resource cases ------------------------------------------------------

func vectorSetParseLimit(t *testing.T, limits *PropertiesParseLimits, name string, value int64) {
	t.Helper()
	set := func(target *int) {
		*target = int(value)
	}
	switch name {
	case "max_source_bytes":
		set(&limits.Common.MaxSourceBytes)
	case "max_nesting_depth":
		set(&limits.Common.MaxNestingDepth)
	case "max_token_count":
		set(&limits.Common.MaxTokenCount)
	case "max_node_count":
		set(&limits.Common.MaxNodeCount)
	case "max_diagnostics":
		set(&limits.Common.MaxDiagnostics)
	case "max_decoded_utf8_bytes":
		set(&limits.MaxDecodedUTF8Bytes)
	case "max_decoded_scalars":
		set(&limits.MaxDecodedScalars)
	case "max_natural_lines":
		set(&limits.MaxNaturalLines)
	case "max_natural_line_bytes":
		set(&limits.MaxNaturalLineBytes)
	case "max_natural_line_scalars":
		set(&limits.MaxNaturalLineScalars)
	case "max_logical_lines":
		set(&limits.MaxLogicalLines)
	case "max_logical_line_natural_lines":
		set(&limits.MaxLogicalLineNaturalLines)
	case "max_logical_line_scalars":
		set(&limits.MaxLogicalLineScalars)
	case "max_properties":
		set(&limits.MaxProperties)
	case "max_comments":
		set(&limits.MaxComments)
	case "max_escapes":
		set(&limits.MaxEscapes)
	case "max_unicode_escapes":
		set(&limits.MaxUnicodeEscapes)
	case "max_java_code_units_per_string":
		set(&limits.MaxJavaCodeUnitsPerString)
	case "max_total_java_code_units":
		set(&limits.MaxTotalJavaCodeUnits)
	case "max_duplicate_group_members":
		set(&limits.MaxDuplicateGroupMembers)
	case "max_recovery_regions":
		set(&limits.MaxRecoveryRegions)
	default:
		t.Fatalf("unknown Properties parse limit %s", name)
	}
}

func vectorFormationLimits(t *testing.T, input, expected map[string]interface{}) {
	descriptors, ok := input["limits"].([]interface{})
	if !ok {
		t.Fatalf("missing input.limits")
	}
	fatal := 0
	for _, item := range descriptors {
		descriptor, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("limit descriptor must be Object")
		}
		name := vString(t, descriptor, "name")
		source := vString(t, descriptor, "source")
		value := vInt(t, descriptor, "value")
		limits := DefaultPropertiesParseLimits()
		vectorSetParseLimit(t, &limits, name, int64(value))
		_, failure := ParseReader([]byte(source), document.Utf8Encoding(), limits)
		if failure != nil {
			fatal++
		}
	}
	requireEqual(t, fatal, int(vInt(t, expected, "fatal_count")))
	if !vBool(t, expected, "no_partial_documents") {
		t.Fatalf("vector requires no partial documents")
	}
}

func vectorProjectionLimits(t *testing.T, input, expected map[string]interface{}) {
	doc := vectorParseCase(t, input)
	names := vStrings(t, input, "limits")
	failedCount := 0
	for _, name := range names {
		limits := DefaultProjectionLimits()
		switch name {
		case "max_source_associations":
			limits.MaxSourceAssociations = 0
		case "max_value_nodes":
			limits.MaxValueNodes = 1
		case "max_provenance_units":
			limits.MaxProvenanceUnits = 1
		default:
			t.Fatalf("unknown projection limit %s", name)
		}
		result := doc.Project(BestExactEntryMapping().WithLimits(limits))
		if result.Complete != nil {
			t.Fatalf("projection limit %s did not fail", name)
		}
		if len(result.Failed.Diagnostics) > 0 &&
			result.Failed.Diagnostics[0].Code == vString(t, expected, "code") {
			failedCount++
		}
	}
	duplicate := vectorParseReader(t, vString(t, input, "duplicate_source"))
	reportLimits := DefaultProjectionLimits()
	reportLimits.MaxReportEntries = 0
	duplicateResult := duplicate.Project(RequireObject(DuplicatePolicyFirstWins).
		WithLimits(reportLimits))
	if duplicateResult.Complete != nil {
		t.Fatalf("report limit did not fail")
	}
	if len(duplicateResult.Failed.Diagnostics) > 0 &&
		duplicateResult.Failed.Diagnostics[0].Code == vString(t, expected, "code") {
		failedCount++
	}
	requireEqual(t, failedCount, int(vInt(t, expected, "failed_count")))
}

func vectorOperationRegistry(t *testing.T, input, expected map[string]interface{}) {
	profiles := vStrings(t, input, "profiles")
	expectedOperations := vStrings(t, expected, "operations")
	expectedSupported := vInt(t, expected, "supported")
	for _, name := range profiles {
		var profile PropertiesProfile
		switch name {
		case "java-properties.reader@1":
			profile = PropertiesReaderV1
		case "java-properties.latin1@1":
			profile = PropertiesLatin1V1
		default:
			t.Fatalf("unknown profile %s", name)
		}
		registry := NewFormatOperationRegistry(profile)
		operations := make([]string, 0, len(registry.Operations()))
		supported := 0
		for _, descriptor := range registry.Operations() {
			operations = append(operations,
				descriptor.ID.ID()+"@"+uint64String(uint64(descriptor.ID.Version())))
			if descriptor.Support == OperationSupportSupported {
				supported++
			}
		}
		requireSlices(t, operations, expectedOperations)
		requireEqual(t, supported, int(expectedSupported))
	}
}

// -- assertion helpers ---------------------------------------------------

func requireEqual(t *testing.T, actual, expected interface{}) {
	t.Helper()
	if actual != expected {
		t.Fatalf("got %v, want %v", actual, expected)
	}
}

func requireSlices(t *testing.T, actual, expected []string) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("length %d != %d: got %v, want %v", len(actual), len(expected), actual, expected)
	}
	for index := range actual {
		if actual[index] != expected[index] {
			t.Fatalf("index %d: got %q, want %q", index, actual[index], expected[index])
		}
	}
}

func requireSlicesUint64(t *testing.T, actual, expected []uint64) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("length %d != %d", len(actual), len(expected))
	}
	for index := range actual {
		if actual[index] != expected[index] {
			t.Fatalf("index %d: got %d, want %d", index, actual[index], expected[index])
		}
	}
}

func requirePairs(t *testing.T, actual, expected [][2]string) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("length %d != %d", len(actual), len(expected))
	}
	for index := range actual {
		if actual[index] != expected[index] {
			t.Fatalf("index %d: got %v, want %v", index, actual[index], expected[index])
		}
	}
}
