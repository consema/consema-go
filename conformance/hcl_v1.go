package conformance

// The `consema.hcl.conformance@1` suite runner, mirroring
// crates/consema-conformance/src/hcl_v1.rs. The 0.18.0 milestone G4.1
// implements the whole HCL surface (hcl.native@1 and hcl.tfvars@1
// formation, hcl.native-semantic-query@1 and hcl.lossless-syntax-query@1,
// hcl.projection.body@1 with the ProjectExpression policy, the
// hcl.canonical-document@1 materialization, the six structural edit
// operations, and the hcl.limit@1 formation limits) through the hcl
// package, so the formation, query, projection, materialization, edit, and
// limit cases execute. The shared vector files are the authority; the
// runner embeds no expectation literals.

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/hcl"
	"consema.dev/consema/protocol"
)

// runHclV1 executes the `consema.hcl.conformance@1` suite.
func runHclV1(_ *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		switch vector.Capability {
		case "hcl.native-formation@1", "hcl.tfvars-formation@1", "hcl.limit@1":
			runHclFormationCase(vector, report)
		case "hcl.query@1":
			runHclQueryCase(vector, report)
		case "hcl.projection@1":
			runHclProjectionCase(vector, report)
		case "hcl.materialization@1":
			runHclMaterializationCase(vector, report)
		case "hcl.edit@1":
			runHclEditCase(vector, report)
		default:
			failHclCase(vector, report, "unknown capability "+vector.Capability)
		}
	}
	return report
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// failHclCase records one failed case.
func failHclCase(vector *caseData, report *SuiteReport, message string) {
	report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
}

// passHclCase records one passed case.
func passHclCase(vector *caseData, report *SuiteReport) {
	report.Passed = append(report.Passed, vector.ID)
}

// hclProfile resolves one vector profile spelling.
func hclProfile(name string) (hcl.HclProfile, bool) {
	switch name {
	case "hcl.native@1":
		return hcl.HclProfileNativeV1, true
	case "hcl.tfvars@1":
		return hcl.HclProfileTfvarsV1, true
	}
	return 0, false
}

// hclProfileOf reads the profile of one vector input or sample.
func hclProfileOf(vector *caseData) (hcl.HclProfile, bool) {
	name, ok := stringField(vector.Input, "profile")
	if !ok {
		return 0, false
	}
	return hclProfile(name)
}

// hclSourceBytes reads the raw source bytes of one vector input or sample:
// `source` UTF-8 text, or `hex` raw bytes (the invalid-UTF-8 fatal sample).
func hclSourceBytes(vector *caseData) ([]byte, bool, string) {
	if hex, ok := stringField(vector.Input, "hex"); ok {
		bytes, err := decodeHexBytes(hex)
		if err != nil {
			return nil, false, err.Error()
		}
		return bytes, true, ""
	}
	source, ok := stringField(vector.Input, "source")
	if !ok {
		return nil, false, "missing input.source"
	}
	return []byte(source), true, ""
}

// decodeHexBytes decodes one lowercase hex string.
func decodeHexBytes(text string) ([]byte, error) {
	if len(text)%2 != 0 {
		return nil, fmt.Errorf("invalid hex")
	}
	bytes := make([]byte, 0, len(text)/2)
	for index := 0; index < len(text); index += 2 {
		high := hexDigitByte(text[index])
		low := hexDigitByte(text[index+1])
		if high < 0 || low < 0 {
			return nil, fmt.Errorf("invalid hex")
		}
		bytes = append(bytes, byte(high)*16+byte(low))
	}
	return bytes, nil
}

func hexDigitByte(character byte) int {
	switch {
	case character >= '0' && character <= '9':
		return int(character - '0')
	case character >= 'a' && character <= 'f':
		return int(character-'a') + 10
	case character >= 'A' && character <= 'F':
		return int(character-'A') + 10
	}
	return -1
}

// hclParseLimits resolves the input.limits overrides into the formation
// contract; absent fields keep the frozen defaults.
func hclParseLimits(vector *caseData) hcl.HclParseLimits {
	limits := hcl.DefaultHclParseLimits()
	overrides, ok := objectField(vector.Input, "limits")
	if !ok {
		return limits
	}
	object, ok := overrides.(*core.Object)
	if !ok {
		return limits
	}
	if common, ok := object.Get("common"); ok {
		if commonObject, ok := common.(*core.Object); ok {
			applyHclLimit(commonObject, "max_source_bytes", &limits.Common.MaxSourceBytes)
			applyHclLimit(commonObject, "max_nesting_depth", &limits.Common.MaxNestingDepth)
			applyHclLimit(commonObject, "max_token_count", &limits.Common.MaxTokenCount)
			applyHclLimit(commonObject, "max_node_count", &limits.Common.MaxNodeCount)
			applyHclLimit(commonObject, "max_diagnostics", &limits.Common.MaxDiagnostics)
		}
	}
	applyHclLimit(object, "max_body_depth", &limits.MaxBodyDepth)
	applyHclLimit(object, "max_expression_depth", &limits.MaxExpressionDepth)
	applyHclLimit(object, "max_template_depth", &limits.MaxTemplateDepth)
	applyHclLimit(object, "max_attribute_count", &limits.MaxAttributeCount)
	applyHclLimit(object, "max_block_count", &limits.MaxBlockCount)
	applyHclLimit(object, "max_label_count", &limits.MaxLabelCount)
	applyHclLimit(object, "max_body_item_count", &limits.MaxBodyItemCount)
	applyHclLimit(object, "max_identifier_len", &limits.MaxIdentifierLen)
	applyHclLimit(object, "max_string_len", &limits.MaxStringLen)
	applyHclLimit(object, "max_number_digits", &limits.MaxNumberDigits)
	applyHclLimit(object, "max_template_len", &limits.MaxTemplateLen)
	applyHclLimit(object, "max_heredoc_lines", &limits.MaxHeredocLines)
	applyHclLimit(object, "max_heredoc_bytes", &limits.MaxHeredocBytes)
	applyHclLimit(object, "max_tuple_elements", &limits.MaxTupleElements)
	applyHclLimit(object, "max_object_entries", &limits.MaxObjectEntries)
	applyHclLimit(object, "max_for_extent", &limits.MaxForExtent)
	applyHclLimit(object, "max_recovery_regions", &limits.MaxRecoveryRegions)
	applyHclLimit(object, "max_error_regions", &limits.MaxErrorRegions)
	applyHclLimit(object, "max_syntax_pieces", &limits.MaxSyntaxPieces)
	return limits
}

func applyHclLimit(object *core.Object, name string, target *int) {
	value, ok := object.Get(name)
	if !ok {
		return
	}
	integer, ok := value.(core.Integer)
	if !ok {
		return
	}
	number := integer.Int()
	if number.IsInt64() && number.Sign() >= 0 && number.IsUint64() {
		*target = int(number.Uint64())
	}
}

// hclFormed is one formation outcome: a formed document or a fatal
// formation failure.
type hclFormed struct {
	document *hcl.Document
	failure  *hcl.FormationFailure
}

func (f *hclFormed) statusName() string {
	if f.failure != nil {
		return "FatalFormationFailure"
	}
	return f.document.FormationStatus().String()
}

func (f *hclFormed) hasCode(code string) bool {
	if f.failure != nil {
		for _, diagnostic := range f.failure.Diagnostics() {
			if diagnostic.Code == code {
				return true
			}
		}
		return false
	}
	for _, diagnostic := range f.document.Diagnostics() {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func (f *hclFormed) documentOrFail() (*hcl.Document, string) {
	if f.failure != nil {
		return nil, "formation failed"
	}
	return f.document, ""
}

// hclFormValue forms one vector value document.
func hclFormValue(vector *caseData) (*hclFormed, string) {
	profile, ok := hclProfileOf(vector)
	if !ok {
		return nil, "missing or unknown profile"
	}
	bytes, ok, message := hclSourceBytes(vector)
	if !ok {
		return nil, message
	}
	document, failure := hcl.Parse(context.Background(), bytes, profile,
		hcl.HclEncodingSelectionProfileDefault(), hclParseLimits(vector))
	if failure != nil {
		return &hclFormed{failure: failure}, ""
	}
	return &hclFormed{document: document}, ""
}

// hclFormSample forms one sample document; a sample without its own
// source/profile facts inherits the case-level input facts.
func hclFormSample(vector *caseData, sample core.Value) (*hclFormed, string) {
	profileValue, ok := objectField(sample, "profile")
	var profile hcl.HclProfile
	if ok {
		name, ok := profileValue.(core.String)
		if !ok {
			return nil, "sample profile must be a string"
		}
		resolved, ok := hclProfile(string(name))
		if !ok {
			return nil, "unknown sample profile"
		}
		profile = resolved
	} else {
		resolved, ok := hclProfileOf(vector)
		if !ok {
			return nil, "missing profile"
		}
		profile = resolved
	}
	var bytes []byte
	if hexValue, ok := objectField(sample, "hex"); ok {
		hex, ok := hexValue.(core.String)
		if !ok {
			return nil, "sample hex must be a string"
		}
		decoded, err := decodeHexBytes(string(hex))
		if err != nil {
			return nil, "invalid sample hex"
		}
		bytes = decoded
	} else if sourceValue, ok := objectField(sample, "source"); ok {
		source, ok := sourceValue.(core.String)
		if !ok {
			return nil, "sample source must be a string"
		}
		bytes = []byte(source)
	} else {
		var ok bool
		var message string
		bytes, ok, message = hclSourceBytes(vector)
		if !ok {
			return nil, message
		}
	}
	document, failure := hcl.Parse(context.Background(), bytes, profile,
		hcl.HclEncodingSelectionProfileDefault(), hclParseLimits(vector))
	if failure != nil {
		return &hclFormed{failure: failure}, ""
	}
	return &hclFormed{document: document}, ""
}

// hclExpectedStrings reads one expected string sequence.
func hclExpectedStrings(vector *caseData, name string) ([]string, bool) {
	values, ok := sequenceField(vector.Expected, name)
	if !ok {
		return nil, false
	}
	strings := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(core.String)
		if !ok {
			return nil, false
		}
		strings = append(strings, string(text))
	}
	return strings, true
}

// hclExpectedString reads one expected string field.
func hclExpectedString(vector *caseData, name string) (string, bool) {
	return stringField(vector.Expected, name)
}

// hclHasExpectedField reports whether one expected field is present.
func hclHasExpectedField(vector *caseData, name string) bool {
	_, ok := objectField(vector.Expected, name)
	return ok
}

// ---------------------------------------------------------------------------
// Formation
// ---------------------------------------------------------------------------

func runHclFormationCase(vector *caseData, report *SuiteReport) {
	if samples, ok := sequenceField(vector.Input, "samples"); ok {
		runHclFormationSamples(vector, samples, report)
		return
	}
	formed, message := hclFormValue(vector)
	if message != "" {
		failHclCase(vector, report, message)
		return
	}
	assertHclExpectedStatus(vector, formed, report)
	if formed.statusName() == "Complete" {
		document, _ := formed.documentOrFail()
		if render, ok := hclExpectedString(vector, "render"); ok {
			if string(document.Render()) != render {
				failHclCase(vector, report, fmt.Sprintf("render %q != %q",
					string(document.Render()), render))
				return
			}
		}
	}
	passHclCase(vector, report)
}

// assertHclExpectedStatus asserts the expected.status and optional
// expected.diagnostic facts.
func assertHclExpectedStatus(vector *caseData, formed *hclFormed, report *SuiteReport) bool {
	if status, ok := hclExpectedString(vector, "status"); ok {
		if formed.statusName() != status {
			failHclCase(vector, report, fmt.Sprintf("status %s != %s",
				formed.statusName(), status))
			return false
		}
	}
	if diagnostic, ok := hclExpectedString(vector, "diagnostic"); ok {
		if !formed.hasCode(diagnostic) {
			failHclCase(vector, report, "diagnostic "+diagnostic+" not found")
			return false
		}
	}
	return true
}

func runHclFormationSamples(vector *caseData, samples []core.Value, report *SuiteReport) {
	statuses, ok := sequenceField(vector.Expected, "statuses")
	if !ok {
		failHclCase(vector, report, "missing expected.statuses")
		return
	}
	diagnostics, ok := sequenceField(vector.Expected, "diagnostics")
	if !ok {
		failHclCase(vector, report, "missing expected.diagnostics")
		return
	}
	if len(samples) != len(statuses) || len(samples) != len(diagnostics) {
		failHclCase(vector, report, "status/diagnostic count mismatch")
		return
	}
	canonicalValues, hasCanonical := sequenceField(vector.Expected, "canonical_values")
	provenNames, hasProven := sequenceField(vector.Expected, "proven_attribute_names")
	for index, sample := range samples {
		formed, message := hclFormSample(vector, sample)
		if message != "" {
			failHclCase(vector, report, fmt.Sprintf("sample %d: %s", index, message))
			return
		}
		statusValue, ok := statuses[index].(core.String)
		if !ok {
			failHclCase(vector, report, "status must be a string")
			return
		}
		status := string(statusValue)
		if formed.statusName() != status {
			failHclCase(vector, report, fmt.Sprintf("sample %d status %s != %s",
				index, formed.statusName(), status))
			return
		}
		if codeValue, ok := diagnostics[index].(core.String); ok {
			if !formed.hasCode(string(codeValue)) {
				failHclCase(vector, report, fmt.Sprintf("sample %d diagnostic %s not found",
					index, string(codeValue)))
				return
			}
		}
		if status == "Complete" && hasCanonical {
			if canonicalValues[index].Kind() != core.KindNull {
				document, _ := formed.documentOrFail()
				if message := assertHclCanonicalValue(document, canonicalValues[index]); message != "" {
					failHclCase(vector, report, fmt.Sprintf("sample %d: %s", index, message))
					return
				}
			}
		}
		if hasProven {
			if namesValue, ok := provenNames[index].(*core.Array); ok {
				document, _ := formed.documentOrFail()
				var actual []string
				for _, item := range document.RootBody().Items() {
					if attribute := item.AsAttribute(); attribute != nil {
						actual = append(actual, attribute.Name())
					}
				}
				expected := make([]string, 0, len(namesValue.Items()))
				for _, name := range namesValue.Items() {
					text, ok := name.(core.String)
					if !ok {
						failHclCase(vector, report, "proven name must be a string")
						return
					}
					expected = append(expected, string(text))
				}
				if strings.Join(actual, "\x00") != strings.Join(expected, "\x00") {
					failHclCase(vector, report, fmt.Sprintf("sample %d attribute names %v != %v",
						index, actual, expected))
					return
				}
			}
		}
	}
	passHclCase(vector, report)
}

// assertHclCanonicalValue asserts the canonical decimal value of the first
// attribute expression against one expected numeric fact (RFC 0014 §6).
func assertHclCanonicalValue(document *hcl.Document, expected core.Value) string {
	items := document.RootBody().Items()
	if len(items) == 0 {
		return "no attribute to canonicalize"
	}
	attribute := items[0].AsAttribute()
	if attribute == nil {
		return "no attribute to canonicalize"
	}
	literal, err := hcl.LiteralValue(attribute.Expression())
	if err != nil {
		return "expression is not literal-complete"
	}
	if literal.IsInteger() {
		actual, ok := new(big.Int).SetString(literal.Text(), 10)
		if !ok {
			return "integer canonical value is not numeric"
		}
		expected, ok := expected.(core.Integer)
		if !ok {
			return "expected an integer canonical value"
		}
		if actual.Cmp(expected.Int()) != 0 {
			return "integer canonical value mismatch"
		}
		return ""
	}
	if literal.IsDecimal() {
		actual, ok := decimalToFloat64(literal.Text())
		if !ok {
			return "canonical is not numeric"
		}
		expectedValue, ok := expectedFloat64(expected)
		if !ok {
			return "expected a real canonical value"
		}
		if mathFloat64Bits(actual) != mathFloat64Bits(expectedValue) {
			return "real canonical value mismatch"
		}
		return ""
	}
	return "unexpected literal kind"
}

// decimalToFloat64 converts one exact canonical decimal spelling to its
// double value.
func decimalToFloat64(text string) (float64, bool) {
	negative := false
	magnitude := text
	if strings.HasPrefix(magnitude, "-") {
		negative = true
		magnitude = magnitude[1:]
	}
	exponent := big.NewInt(0)
	digits := magnitude
	if index := strings.IndexByte(magnitude, '.'); index >= 0 {
		fraction := magnitude[index+1:]
		digits = magnitude[:index] + fraction
		exponent = big.NewInt(-int64(len(fraction)))
	}
	coefficient, ok := new(big.Int).SetString(digits, 10)
	if !ok || !exponent.IsInt64() || !coefficient.IsInt64() {
		return 0, false
	}
	value := float64(coefficient.Int64())
	exp := exponent.Int64()
	if exp > 0 {
		value *= mathPow10(exp)
	} else if exp < 0 {
		value /= mathPow10(-exp)
	}
	if negative {
		value = -value
	}
	return value, true
}

func mathPow10(power int64) float64 {
	result := 1.0
	base := 10.0
	for power > 0 {
		if power&1 == 1 {
			result *= base
		}
		base *= base
		power >>= 1
	}
	return result
}

func mathFloat64Bits(value float64) uint64 {
	return math.Float64bits(value)
}

// decimalValueToFloat64 converts one exact core.Decimal to its double
// value; the second result is false when the coefficient or exponent
// exceeds the exact int64 range.
func decimalValueToFloat64(decimal *core.Decimal) (float64, bool) {
	coefficient := decimal.Coefficient()
	exponent := decimal.Exponent()
	if !coefficient.IsInt64() || !exponent.IsInt64() {
		return 0, false
	}
	value := float64(coefficient.Int64())
	exp := exponent.Int64()
	if exp > 0 {
		value *= mathPow10(exp)
	} else if exp < 0 {
		value /= mathPow10(-exp)
	}
	return value, true
}

// expectedFloat64 resolves one expected numeric fact (decimal or binary
// float) to its double value.
func expectedFloat64(value core.Value) (float64, bool) {
	switch item := value.(type) {
	case core.BinaryFloat64:
		return math.Float64frombits(uint64(item)), true
	case core.BinaryFloat32:
		return float64(math.Float32frombits(uint32(item))), true
	case core.Decimal:
		return decimalValueToFloat64(&item)
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// Query
// ---------------------------------------------------------------------------

// hclQueryFailureCode maps one query failure kind onto its vector spelling.
func hclQueryFailureCode(failure *protocol.QueryFailure) string {
	switch failure.Kind {
	case protocol.FailureDomainMismatch:
		return "hcl.query.domain-mismatch@1"
	case protocol.FailureUnknownOperator:
		return "hcl.query.unknown-operator@1"
	case protocol.FailureWrongArgumentType:
		return "hcl.query.wrong-argument-type@1"
	case protocol.FailureInvalidArgument:
		return "hcl.query.invalid-argument@1"
	case protocol.FailureInvalidOperatorComposition:
		return "hcl.query.invalid-composition@1"
	case protocol.FailureMissingCapability:
		return "hcl.query.missing-capability@1"
	case protocol.FailureRequiredTypeMismatch:
		return "hcl.query.type-mismatch@1"
	case protocol.FailureCardinalityViolation:
		return "hcl.query.cardinality-violation@1"
	case protocol.FailureResourceLimit:
		return "hcl.query.resource-limit@1"
	case protocol.FailureCancelled:
		return "hcl.query.cancelled@1"
	case protocol.FailureTargetUnavailable:
		return "hcl.query.non-literal@1"
	}
	return "hcl.query.invalid-argument@1"
}

// hclBuildFilters builds the frozen operator vocabulary from one vector
// filter list.
func hclBuildFilters(filters []core.Value) ([]*protocol.OperatorCall, string) {
	calls := make([]*protocol.OperatorCall, 0, len(filters))
	for _, filter := range filters {
		operatorValue, ok := objectField(filter, "operator")
		if !ok {
			return nil, "missing filter.operator"
		}
		operator, ok := operatorValue.(core.String)
		if !ok {
			return nil, "filter.operator must be a string"
		}
		name, version, ok := splitOperatorID(string(operator))
		if !ok {
			return nil, "operator lacks version: " + string(operator)
		}
		call := protocol.NewOperatorCall(name, version)
		if argumentValue, ok := objectField(filter, "argument"); ok {
			argument, ok := argumentValue.(core.String)
			if !ok {
				return nil, "filter.argument must be a string"
			}
			call = call.WithArgument(hclArgumentName(name), core.String(argument))
		}
		calls = append(calls, call)
	}
	return calls, ""
}

// splitOperatorID splits one versioned operator spelling.
func splitOperatorID(operator string) (string, uint32, bool) {
	index := strings.LastIndexByte(operator, '@')
	if index < 0 {
		return "", 0, false
	}
	var version uint32
	for _, digit := range operator[index+1:] {
		if digit < '0' || digit > '9' {
			return "", 0, false
		}
		version = version*10 + uint32(digit-'0')
	}
	return operator[:index], version, true
}

// hclArgumentName resolves the argument name of one operator.
func hclArgumentName(operator string) string {
	switch operator {
	case "hcl.attribute-name-equals":
		return "name"
	case "hcl.attribute-literal-value":
		return "accessor"
	case "hcl.body-block-type-equals", "hcl.block-type-equals":
		return "type"
	case "hcl.block-label-equals":
		return "label"
	case "hcl.expression-kind-is", "hcl.syntax-kind-is":
		return "kind"
	case "hcl.syntax-text-equals":
		return "text"
	}
	return "argument"
}

// hclBindQuery validates and binds one query definition.
func hclBindQuery(domain *protocol.QueryDomain,
	expression *protocol.QueryExpression) (*protocol.ExecutableQuery, string) {
	validated, failure := protocol.NewQueryDefinition(domain).WithExpression(expression).Validate()
	if failure != nil {
		return nil, "validate: " + failure.Error()
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, failure := validated.Bind(capabilities)
	if failure != nil {
		return nil, "bind: " + failure.Error()
	}
	return executable, ""
}

// hclBuildQueryExpression chains one filter list into one apply chain.
func hclBuildQueryExpression(calls []*protocol.OperatorCall) *protocol.QueryExpression {
	expression := &protocol.QueryExpression{Kind: protocol.ExpressionInput}
	for _, call := range calls {
		expression = expression.Then(call)
	}
	return expression
}

func runHclQueryCase(vector *caseData, report *SuiteReport) {
	domainName, ok := stringField(vector.Input, "domain")
	if !ok {
		failHclCase(vector, report, "missing input.domain")
		return
	}
	switch domainName {
	case "hcl.native-semantic-query@1":
		runHclNativeQueryCase(vector, report)
	case "hcl.lossless-syntax-query@1":
		runHclSyntaxQueryCase(vector, report)
	default:
		failHclCase(vector, report, "unknown query domain "+domainName)
	}
}

// hclExpressionFacts derives the kind, text, and literal facts of one
// expression match.
func hclExpressionFacts(doc *hcl.Document, match *hcl.HclMatch) (string, string, bool) {
	expression := match.Expression
	kind := expression.Kind().Name().AsStr()
	decoded, _ := doc.Source().DecodedText()
	text := expression.Text(decoded)
	literal := hcl.IsLiteralComplete(expression)
	return kind, text, literal
}

// hclAssertExpressionMatch compares one expression match against its
// {kind, text, literal} expectation.
func hclAssertExpressionMatch(document *hcl.Document, actual *hcl.HclMatch,
	expected core.Value) string {
	kind, text, literal := hclExpressionFacts(document, actual)
	if expectedKind, ok := stringField(expected, "kind"); ok && kind != expectedKind {
		return fmt.Sprintf("kind %s != %s", kind, expectedKind)
	}
	if expectedText, ok := stringField(expected, "text"); ok && text != expectedText {
		return fmt.Sprintf("text %q != %q", text, expectedText)
	}
	if expectedLiteral, ok := booleanField(expected, "literal"); ok && literal != expectedLiteral {
		return fmt.Sprintf("literal %v != %v", literal, expectedLiteral)
	}
	return ""
}

func runHclNativeQueryCase(vector *caseData, report *SuiteReport) {
	if samples, ok := sequenceField(vector.Input, "samples"); ok {
		runHclNativeQuerySamples(vector, samples, report)
		return
	}
	formed, message := hclFormValue(vector)
	if message != "" {
		failHclCase(vector, report, message)
		return
	}
	doc, message := formed.documentOrFail()
	if message != "" {
		failHclCase(vector, report, message)
		return
	}
	// An expected.error_regions case queries a Recovered document: the
	// `hcl.error-regions@1` operator exposes its ordered error regions as
	// document-level facts (RFC 0014 §3, §7.1).
	_, expectsErrorRegions := sequenceField(vector.Expected, "error_regions")
	if doc.FormationStatus() != document.FormationStatusComplete && !expectsErrorRegions {
		failHclCase(vector, report, "native-query input must form completely")
		return
	}
	filters, ok := sequenceField(vector.Input, "filters")
	if !ok {
		failHclCase(vector, report, "missing input.filters")
		return
	}
	calls, message := hclBuildFilters(filters)
	if message != "" {
		failHclCase(vector, report, message)
		return
	}
	executable, message := hclBindQuery(protocol.DomainHCLNativeV1(),
		hclBuildQueryExpression(calls))
	if message != "" {
		failHclCase(vector, report, message)
		return
	}
	matches, queryFailure := hcl.ExecuteHCLNativeQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if queryFailure != nil {
		failHclCase(vector, report, "execute: "+queryFailure.Error())
		return
	}
	terminal, ok := hclExpectedString(vector, "terminal")
	if !ok {
		failHclCase(vector, report, "missing expected.terminal")
		return
	}
	if terminal != "Completed" {
		failHclCase(vector, report, fmt.Sprintf("terminal Completed != %s", terminal))
		return
	}
	if expectedMatches, ok := sequenceField(vector.Expected, "matches"); ok {
		if len(matches) != len(expectedMatches) {
			failHclCase(vector, report, fmt.Sprintf("match count %d != %d",
				len(matches), len(expectedMatches)))
			return
		}
		for index, expectedMatch := range expectedMatches {
			if message := hclAssertExpressionMatch(doc, &matches[index], expectedMatch); message != "" {
				failHclCase(vector, report, message)
				return
			}
		}
	}
	if expectedRegions, ok := sequenceField(vector.Expected, "error_regions"); ok {
		type regionFact struct {
			code     string
			position int
		}
		var regions []regionFact
		for _, match := range matches {
			if match.Kind == hcl.HclMatchErrorRegion {
				regions = append(regions, regionFact{code: match.Region.Code(), position: match.Position})
			}
		}
		if len(regions) != len(expectedRegions) {
			failHclCase(vector, report, fmt.Sprintf("error region count %d != %d",
				len(regions), len(expectedRegions)))
			return
		}
		for index, expectedRegion := range expectedRegions {
			if expectedCode, ok := stringField(expectedRegion, "code"); ok && regions[index].code != expectedCode {
				failHclCase(vector, report, fmt.Sprintf("error region code %s != %s",
					regions[index].code, expectedCode))
				return
			}
			if expectedPosition, ok := integerField(expectedRegion, "position"); ok &&
				uint64(regions[index].position) != expectedPosition {
				failHclCase(vector, report, fmt.Sprintf("error region position %d != %d",
					regions[index].position, expectedPosition))
				return
			}
		}
	}
	passHclCase(vector, report)
}

func runHclNativeQuerySamples(vector *caseData, samples []core.Value, report *SuiteReport) {
	terminals, ok := sequenceField(vector.Expected, "terminals")
	if !ok {
		failHclCase(vector, report, "missing expected.terminals")
		return
	}
	if len(samples) != len(terminals) {
		failHclCase(vector, report, "terminal count mismatch")
		return
	}
	codes, hasCodes := sequenceField(vector.Expected, "codes")
	integerMatches, hasIntegers := sequenceField(vector.Expected, "integer_matches")
	booleanMatches, hasBooleans := sequenceField(vector.Expected, "boolean_matches")
	labelMatches, hasLabels := sequenceField(vector.Expected, "label_matches")
	nestedMatches, hasNested := sequenceField(vector.Expected, "nested_matches")
	for index, sample := range samples {
		formed, message := hclFormSample(vector, sample)
		if message != "" {
			failHclCase(vector, report, message)
			return
		}
		doc, message := formed.documentOrFail()
		if message != "" {
			failHclCase(vector, report, message)
			return
		}
		if doc.FormationStatus() != document.FormationStatusComplete {
			failHclCase(vector, report, "native-query input must form completely")
			return
		}
		filtersValue, ok := objectField(sample, "filters")
		if !ok {
			failHclCase(vector, report, "missing sample filters")
			return
		}
		filters, ok := filtersValue.(*core.Array)
		if !ok {
			failHclCase(vector, report, "sample filters must be a sequence")
			return
		}
		lastOperator := ""
		if len(filters.Items()) > 0 {
			lastOperator, _ = stringField(filters.Items()[len(filters.Items())-1], "operator")
		}
		calls, message := hclBuildFilters(filters.Items())
		if message != "" {
			failHclCase(vector, report, message)
			return
		}
		terminalValue, ok := terminals[index].(core.String)
		if !ok {
			failHclCase(vector, report, "terminal must be a string")
			return
		}
		terminal := string(terminalValue)
		switch terminal {
		case "Completed":
			executable, message := hclBindQuery(protocol.DomainHCLNativeV1(),
				hclBuildQueryExpression(calls))
			if message != "" {
				failHclCase(vector, report, message)
				return
			}
			matches, queryFailure := hcl.ExecuteHCLNativeQuery(context.Background(), executable,
				doc, protocol.DefaultQueryLimits())
			if queryFailure != nil {
				failHclCase(vector, report, "execute: "+queryFailure.Error())
				return
			}
			switch {
			case lastOperator == "hcl.attribute-literal-value" && sampleAccessor(sample) == "as-integer" && hasIntegers:
				if message := hclAssertIntegerMatches(doc, matches, integerMatches); message != "" {
					failHclCase(vector, report, message)
					return
				}
			case lastOperator == "hcl.attribute-literal-value" && sampleAccessor(sample) == "as-boolean-is" && hasBooleans:
				if message := hclAssertBooleanMatches(doc, matches, booleanMatches); message != "" {
					failHclCase(vector, report, message)
					return
				}
			case lastOperator == "hcl.block-label-equals" && hasLabels:
				if message := hclAssertLabelMatches(matches, labelMatches); message != "" {
					failHclCase(vector, report, message)
					return
				}
			case lastOperator == "hcl.expression-text" && hasNested:
				if message := hclAssertNestedMatches(doc, matches, nestedMatches); message != "" {
					failHclCase(vector, report, message)
					return
				}
			}
		case "Failed":
			executable, message := hclBindQuery(protocol.DomainHCLNativeV1(),
				hclBuildQueryExpression(calls))
			if message != "" {
				failHclCase(vector, report, message)
				return
			}
			_, queryFailure := hcl.ExecuteHCLNativeQuery(context.Background(), executable,
				doc, protocol.DefaultQueryLimits())
			if queryFailure == nil {
				failHclCase(vector, report, "execution must fail")
				return
			}
			if !hasCodes {
				failHclCase(vector, report, "missing expected.codes")
				return
			}
			expectedCodeValue, ok := codes[index].(core.String)
			if !ok {
				failHclCase(vector, report, "expected code must be a string")
				return
			}
			if hclQueryFailureCode(queryFailure) != string(expectedCodeValue) {
				failHclCase(vector, report, fmt.Sprintf("query failure %s != %s",
					hclQueryFailureCode(queryFailure), string(expectedCodeValue)))
				return
			}
		default:
			failHclCase(vector, report, "unknown terminal "+terminal)
			return
		}
	}
	passHclCase(vector, report)
}

func sampleAccessor(sample core.Value) string {
	filters, ok := objectField(sample, "filters")
	if !ok {
		return ""
	}
	array, ok := filters.(*core.Array)
	if !ok || len(array.Items()) == 0 {
		return ""
	}
	last := array.Items()[len(array.Items())-1]
	argument, ok := stringField(last, "argument")
	if !ok {
		return ""
	}
	return argument
}

// hclAssertIntegerMatches asserts typed integer literal matches against
// {kind, value} facts.
func hclAssertIntegerMatches(document *hcl.Document, matches []hcl.HclMatch,
	expectedMatches []core.Value) string {
	if len(matches) != len(expectedMatches) {
		return fmt.Sprintf("integer match count %d != %d", len(matches), len(expectedMatches))
	}
	for index, expectedMatch := range expectedMatches {
		expectedKind, ok := stringField(expectedMatch, "kind")
		if !ok || expectedKind != "integer" {
			return "missing expected match kind"
		}
		value, err := hcl.LiteralValue(matches[index].Expression)
		if err != nil || !value.IsInteger() {
			return "match is not an integer literal"
		}
		actualValue, ok := new(big.Int).SetString(value.Text(), 10)
		if !ok {
			return "integer canonical value is not numeric"
		}
		expectedValue, ok := expectedMatch.(core.Integer)
		if !ok {
			return "missing expected integer value"
		}
		if actualValue.Cmp(expectedValue.Int()) != 0 {
			return "integer literal value mismatch"
		}
	}
	return ""
}

// hclAssertBooleanMatches asserts typed boolean literal matches against
// {kind, value} facts.
func hclAssertBooleanMatches(document *hcl.Document, matches []hcl.HclMatch,
	expectedMatches []core.Value) string {
	if len(matches) != len(expectedMatches) {
		return fmt.Sprintf("boolean match count %d != %d", len(matches), len(expectedMatches))
	}
	for index, expectedMatch := range expectedMatches {
		expectedKind, ok := stringField(expectedMatch, "kind")
		if !ok || expectedKind != "boolean" {
			return "missing expected match kind"
		}
		value, err := hcl.LiteralValue(matches[index].Expression)
		if err != nil || !value.IsBoolean() {
			return "match is not a boolean literal"
		}
		expectedValue, ok := booleanField(expectedMatch, "value")
		if !ok {
			return "missing expected boolean value"
		}
		if value.Boolean() != expectedValue {
			return "boolean literal value mismatch"
		}
	}
	return ""
}

// hclAssertLabelMatches asserts block-label matches against {text, quoted}
// facts.
func hclAssertLabelMatches(matches []hcl.HclMatch, expectedMatches []core.Value) string {
	if len(matches) != len(expectedMatches) {
		return fmt.Sprintf("label match count %d != %d", len(matches), len(expectedMatches))
	}
	for index, expectedMatch := range expectedMatches {
		match := &matches[index]
		if match.Kind != hcl.HclMatchBlockLabel {
			return "match is not a block label"
		}
		if expectedText, ok := stringField(expectedMatch, "text"); ok && match.Label.Text() != expectedText {
			return fmt.Sprintf("label text %q != %q", match.Label.Text(), expectedText)
		}
		if expectedQuoted, ok := booleanField(expectedMatch, "quoted"); ok &&
			match.Label.Quoted() != expectedQuoted {
			return fmt.Sprintf("label quoted %v != %v", match.Label.Quoted(), expectedQuoted)
		}
	}
	return ""
}

// hclAssertNestedMatches asserts expression matches against {kind, text}
// facts.
func hclAssertNestedMatches(document *hcl.Document, matches []hcl.HclMatch,
	expectedMatches []core.Value) string {
	if len(matches) != len(expectedMatches) {
		return fmt.Sprintf("nested match count %d != %d", len(matches), len(expectedMatches))
	}
	for index, expectedMatch := range expectedMatches {
		kind, text, _ := hclExpressionFacts(document, &matches[index])
		if expectedKind, ok := stringField(expectedMatch, "kind"); ok && kind != expectedKind {
			return fmt.Sprintf("kind %s != %s", kind, expectedKind)
		}
		if expectedText, ok := stringField(expectedMatch, "text"); ok && text != expectedText {
			return fmt.Sprintf("text %q != %q", text, expectedText)
		}
	}
	return ""
}

func runHclSyntaxQueryCase(vector *caseData, report *SuiteReport) {
	formed, message := hclFormValue(vector)
	if message != "" {
		failHclCase(vector, report, message)
		return
	}
	doc, message := formed.documentOrFail()
	if message != "" {
		failHclCase(vector, report, message)
		return
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		failHclCase(vector, report, "syntax-query input must form completely")
		return
	}
	samples, ok := sequenceField(vector.Input, "samples")
	if !ok {
		failHclCase(vector, report, "missing input.samples")
		return
	}
	terminals, ok := sequenceField(vector.Expected, "terminals")
	if !ok {
		failHclCase(vector, report, "missing expected.terminals")
		return
	}
	if len(samples) != len(terminals) {
		failHclCase(vector, report, "terminal count mismatch")
		return
	}
	matchesSets, ok := sequenceField(vector.Expected, "matches")
	if !ok {
		failHclCase(vector, report, "missing expected.matches")
		return
	}
	if len(samples) != len(matchesSets) {
		failHclCase(vector, report, "match count mismatch")
		return
	}
	decoded, _ := doc.Source().DecodedText()
	for index, sample := range samples {
		filtersValue, ok := objectField(sample, "filters")
		if !ok {
			failHclCase(vector, report, "missing sample filters")
			return
		}
		filters, ok := filtersValue.(*core.Array)
		if !ok {
			failHclCase(vector, report, "sample filters must be a sequence")
			return
		}
		calls, message := hclBuildFilters(filters.Items())
		if message != "" {
			failHclCase(vector, report, message)
			return
		}
		executable, message := hclBindQuery(protocol.DomainHCLLosslessSyntaxV1(),
			hclBuildQueryExpression(calls))
		if message != "" {
			failHclCase(vector, report, message)
			return
		}
		matches, queryFailure := hcl.ExecuteHCLSyntaxQuery(context.Background(), executable,
			doc, protocol.DefaultQueryLimits())
		if queryFailure != nil {
			failHclCase(vector, report, "execute: "+queryFailure.Error())
			return
		}
		terminalValue, ok := terminals[index].(core.String)
		if !ok {
			failHclCase(vector, report, "terminal must be a string")
			return
		}
		if string(terminalValue) != "Completed" {
			failHclCase(vector, report, "unexpected terminal "+string(terminalValue))
			return
		}
		expectedMatchesValue, ok := matchesSets[index].(*core.Array)
		if !ok {
			failHclCase(vector, report, "expected matches must be a sequence")
			return
		}
		expectedMatches := expectedMatchesValue.Items()
		if len(matches) != len(expectedMatches) {
			failHclCase(vector, report, fmt.Sprintf("syntax match count %d != %d",
				len(matches), len(expectedMatches)))
			return
		}
		for matchIndex, expectedMatch := range expectedMatches {
			expectedKind, ok := stringField(expectedMatch, "kind")
			if !ok {
				failHclCase(vector, report, "missing expected match kind")
				return
			}
			if matches[matchIndex].Kind().AsStr() != expectedKind {
				failHclCase(vector, report, fmt.Sprintf("kind %s != %s",
					matches[matchIndex].Kind().AsStr(), expectedKind))
				return
			}
			if expectedText, ok := stringField(expectedMatch, "text"); ok {
				span := matches[matchIndex].Span()
				actualText := decoded[span.StartByte():span.EndByte()]
				if actualText != expectedText {
					failHclCase(vector, report, fmt.Sprintf("text %q != %q", actualText, expectedText))
					return
				}
			}
			if expectedOrdinal, ok := integerField(expectedMatch, "ordinal"); ok &&
				uint64(matches[matchIndex].Ordinal()) != expectedOrdinal {
				failHclCase(vector, report, fmt.Sprintf("ordinal %d != %d",
					matches[matchIndex].Ordinal(), expectedOrdinal))
				return
			}
		}
	}
	passHclCase(vector, report)
}

// ---------------------------------------------------------------------------
// Projection
// ---------------------------------------------------------------------------

// hclProjectionRequest resolves the projection target and policy of one
// vector input.
func hclProjectionRequest(vector *caseData) (hcl.ProjectionRequest, string) {
	target, ok := stringField(vector.Input, "target")
	if !ok {
		target = "hcl.projection.body@1"
	}
	if target != "hcl.projection.body@1" {
		return hcl.ProjectionRequest{}, "unknown projection target " + target
	}
	if policy, ok := stringField(vector.Input, "policy"); ok {
		if policy != "ProjectExpression" {
			return hcl.ProjectionRequest{}, "unknown projection policy " + policy
		}
		return hcl.ProjectionRequestBodyWithExpressionPolicy(
			hcl.ExpressionPolicyProjectExpression), ""
	}
	return hcl.ProjectionRequestBody(), ""
}

func runHclProjectionCase(vector *caseData, report *SuiteReport) {
	if samples, ok := sequenceField(vector.Input, "samples"); ok {
		runHclProjectionSamples(vector, samples, report)
		return
	}
	formed, message := hclFormValue(vector)
	if message != "" {
		failHclCase(vector, report, message)
		return
	}
	doc, message := formed.documentOrFail()
	if message != "" {
		failHclCase(vector, report, message)
		return
	}
	request, message := hclProjectionRequest(vector)
	if message != "" {
		failHclCase(vector, report, message)
		return
	}
	result := doc.Project(request)
	if failure, ok := hclExpectedString(vector, "failure"); ok {
		if result.Failed == nil {
			failHclCase(vector, report, "projection must fail")
			return
		}
		code := result.Failed.Diagnostics[0].Code
		if code != failure {
			failHclCase(vector, report, fmt.Sprintf("failure code %s != %s", code, failure))
			return
		}
		passHclCase(vector, report)
		return
	}
	if result.Complete == nil {
		failHclCase(vector, report, "projection must complete")
		return
	}
	record, ok := hclExpectedString(vector, "record")
	if !ok {
		failHclCase(vector, report, "missing expected.record")
		return
	}
	actualRecord, ok := stringField(result.Complete.Value, "record")
	if !ok {
		failHclCase(vector, report, "missing record member")
		return
	}
	if actualRecord != record {
		failHclCase(vector, report, fmt.Sprintf("record %s != %s", actualRecord, record))
		return
	}
	if expectedAttributes, ok := sequenceField(vector.Expected, "attributes"); ok {
		if message := hclAssertProjectedAttributes(result.Complete.Value, expectedAttributes); message != "" {
			failHclCase(vector, report, message)
			return
		}
	}
	if expectedBlocks, ok := sequenceField(vector.Expected, "blocks"); ok {
		if message := hclAssertProjectedBlocks(result.Complete.Value, expectedBlocks); message != "" {
			failHclCase(vector, report, message)
			return
		}
	}
	if transformed, ok := integerField(vector.Expected, "transformed_events"); ok {
		events := 0
		for _, event := range result.Complete.Report.Events() {
			if event.Kind == hcl.ProjectionEventExpressionSubstituted {
				events++
			}
		}
		if uint64(events) != transformed {
			failHclCase(vector, report, fmt.Sprintf("transformed events %d != %d", events, transformed))
			return
		}
	}
	if provenance, ok := booleanField(vector.Expected, "event_provenance"); ok {
		nonEmpty := len(result.Complete.Provenance.Entries()) > 0
		if provenance != nonEmpty {
			failHclCase(vector, report, "event provenance mismatch")
			return
		}
	}
	for _, name := range []string{"attribute_order_preserved", "duplicate_keys_preserved", "canonical_decimal"} {
		if declared, ok := booleanField(vector.Expected, name); ok && !declared {
			failHclCase(vector, report, "declared projection flag "+name+" is false")
			return
		}
	}
	passHclCase(vector, report)
}

// hclProjectedItems reads the projected `hcl.body@1` record's ordered item
// sequence.
func hclProjectedItems(projected core.Value) ([]core.Value, string) {
	items, ok := sequenceField(projected, "items")
	if !ok {
		return nil, "missing projected items"
	}
	return items, ""
}

// hclItemKind reads the kind member of one projected item.
func hclItemKind(item core.Value) (string, bool) {
	return stringField(item, "kind")
}

// hclAssertProjectedAttributes asserts the attribute items of the
// projected record against the ordered expected attribute facts.
func hclAssertProjectedAttributes(projected core.Value, expectedAttributes []core.Value) string {
	items, message := hclProjectedItems(projected)
	if message != "" {
		return message
	}
	var attributes []core.Value
	for _, item := range items {
		if kind, ok := hclItemKind(item); ok && kind == "attribute" {
			attributes = append(attributes, item)
		}
	}
	if len(attributes) != len(expectedAttributes) {
		return fmt.Sprintf("attribute count %d != %d", len(attributes), len(expectedAttributes))
	}
	for index, expectedAttribute := range expectedAttributes {
		expectedName, ok := stringField(expectedAttribute, "name")
		if !ok {
			return "missing expected attribute name"
		}
		actualName, ok := stringField(attributes[index], "name")
		if !ok {
			return "missing projected attribute name"
		}
		if actualName != expectedName {
			return fmt.Sprintf("attribute name %s != %s", actualName, expectedName)
		}
		value, ok := objectField(attributes[index], "value")
		if !ok {
			return "missing projected value"
		}
		if message := hclAssertProjectedValue(value, expectedAttribute); message != "" {
			return message
		}
	}
	return ""
}

// hclAssertProjectedBlocks asserts the block items of the projected
// record.
func hclAssertProjectedBlocks(projected core.Value, expectedBlocks []core.Value) string {
	items, message := hclProjectedItems(projected)
	if message != "" {
		return message
	}
	var blocks []core.Value
	for _, item := range items {
		if kind, ok := hclItemKind(item); ok && kind == "block" {
			blocks = append(blocks, item)
		}
	}
	if len(blocks) != len(expectedBlocks) {
		return fmt.Sprintf("block count %d != %d", len(blocks), len(expectedBlocks))
	}
	for index, expectedBlock := range expectedBlocks {
		if expectedType, ok := stringField(expectedBlock, "type"); ok {
			actualType, ok := stringField(blocks[index], "type")
			if !ok {
				return "missing projected block type"
			}
			if actualType != expectedType {
				return fmt.Sprintf("block type %s != %s", actualType, expectedType)
			}
		}
		if expectedLabels, ok := sequenceField(expectedBlock, "labels"); ok {
			actualLabelsValue, ok := objectField(blocks[index], "labels")
			if !ok {
				return "missing projected block labels"
			}
			actualLabels, ok := actualLabelsValue.(*core.Array)
			if !ok {
				return "projected block labels must be a sequence"
			}
			if len(actualLabels.Items()) != len(expectedLabels) {
				return fmt.Sprintf("label count %d != %d", len(actualLabels.Items()), len(expectedLabels))
			}
			for labelIndex, expectedLabel := range expectedLabels {
				expectedText, ok := expectedLabel.(core.String)
				if !ok {
					return "expected label must be a string"
				}
				actualText, ok := actualLabels.Items()[labelIndex].(core.String)
				if !ok {
					return "projected label must be a string"
				}
				if actualText != expectedText {
					return fmt.Sprintf("label %s != %s", string(actualText), string(expectedText))
				}
			}
		}
	}
	return ""
}

// hclAssertProjectedValue asserts one projected value against its
// {kind, ...} expectation.
func hclAssertProjectedValue(actual core.Value, expected core.Value) string {
	kind, ok := stringField(expected, "kind")
	if !ok {
		return "missing expected value kind"
	}
	switch kind {
	case "string":
		text, ok := stringField(expected, "text")
		if !ok {
			return "missing expected text"
		}
		actualText, ok := actual.(core.String)
		if !ok || string(actualText) != text {
			return "projected string mismatch"
		}
	case "integer":
		expectedValue, ok := objectFieldValue(expected, "value").(core.Integer)
		if !ok {
			return "missing expected integer"
		}
		actualValue, ok := actual.(core.Integer)
		if !ok {
			return "projected value is not an integer"
		}
		if actualValue.Int().Cmp(expectedValue.Int()) != 0 {
			return "projected integer mismatch"
		}
	case "real":
		expectedValue, ok := expectedFloat64(objectFieldValue(expected, "value"))
		if !ok {
			return "missing expected real"
		}
		actualValue, ok := expectedFloat64(actual)
		if !ok {
			return "projected value is not a real"
		}
		if mathFloat64Bits(actualValue) != mathFloat64Bits(expectedValue) {
			return "projected real mismatch"
		}
	case "boolean":
		expectedValue, ok := booleanField(expected, "value")
		if !ok {
			return "missing expected boolean"
		}
		actualValue, ok := actual.(core.Boolean)
		if !ok || bool(actualValue) != expectedValue {
			return "projected boolean mismatch"
		}
	case "null":
		if actual.Kind() != core.KindNull {
			return "projected value is not null"
		}
	case "tuple":
		elements, ok := sequenceField(expected, "elements")
		if !ok {
			return "missing expected elements"
		}
		actualElements, ok := actual.(*core.Array)
		if !ok {
			return "projected value is not a tuple"
		}
		if len(actualElements.Items()) != len(elements) {
			return fmt.Sprintf("tuple count %d != %d", len(actualElements.Items()), len(elements))
		}
		for index, expectedElement := range elements {
			if message := hclAssertProjectedElement(actualElements.Items()[index], expectedElement); message != "" {
				return message
			}
		}
	case "object":
		entries, ok := sequenceField(expected, "entries")
		if !ok {
			return "missing expected entries"
		}
		actualEntries, ok := actual.(*core.EntryMapping)
		if !ok {
			return "projected value is not an object"
		}
		if actualEntries.Len() != len(entries) {
			return fmt.Sprintf("object count %d != %d", actualEntries.Len(), len(entries))
		}
		for index, expectedEntry := range entries {
			pair, ok := expectedEntry.(*core.Array)
			if !ok || len(pair.Items()) != 2 {
				return "expected object entry must be a pair"
			}
			expectedKey, ok := pair.Items()[0].(core.String)
			if !ok {
				return "expected object key must be a string"
			}
			actualKey, ok := actualEntries.Entries()[index].Key.(core.String)
			if !ok {
				return "projected object key is not a string"
			}
			if actualKey != expectedKey {
				return fmt.Sprintf("object key %s != %s", string(actualKey), string(expectedKey))
			}
			if message := hclAssertProjectedElement(actualEntries.Entries()[index].Value,
				pair.Items()[1]); message != "" {
				return message
			}
		}
	case "expression":
		expectedExpression, ok := objectField(expected, "expression")
		if !ok {
			return "missing expected expression record"
		}
		actualRecord, ok := stringField(actual, "record")
		if !ok {
			return "missing expression record member"
		}
		expectedRecord, ok := stringField(expectedExpression, "record")
		if !ok {
			return "missing expected expression record id"
		}
		if actualRecord != expectedRecord {
			return fmt.Sprintf("expression record %s != %s", actualRecord, expectedRecord)
		}
		actualKind, ok := stringField(actual, "kind")
		if !ok {
			return "missing expression kind member"
		}
		expectedKind, ok := stringField(expectedExpression, "kind")
		if !ok {
			return "missing expected expression kind"
		}
		if actualKind != expectedKind {
			return fmt.Sprintf("expression kind %s != %s", actualKind, expectedKind)
		}
		actualText, ok := stringField(actual, "text")
		if !ok {
			return "missing expression text member"
		}
		expectedText, ok := stringField(expectedExpression, "text")
		if !ok {
			return "missing expected expression text"
		}
		if actualText != expectedText {
			return fmt.Sprintf("expression text %q != %q", actualText, expectedText)
		}
	default:
		return "unknown projected value kind " + kind
	}
	return ""
}

// objectFieldValue reads one named field as a value (never failing).
func objectFieldValue(object core.Value, name string) core.Value {
	value, _ := objectField(object, name)
	return value
}

// hclAssertProjectedElement asserts one tuple element or object value: a
// scalar, or a nested {kind, ...} descriptor.
func hclAssertProjectedElement(actual core.Value, expected core.Value) string {
	if text, ok := expected.(core.String); ok {
		actualText, ok := actual.(core.String)
		if !ok || actualText != text {
			return "projected element string mismatch"
		}
		return ""
	}
	if integer, ok := expected.(core.Integer); ok {
		actualInteger, ok := actual.(core.Integer)
		if !ok || actualInteger.Int().Cmp(integer.Int()) != 0 {
			return "projected element integer mismatch"
		}
		return ""
	}
	if boolean, ok := expected.(core.Boolean); ok {
		actualBoolean, ok := actual.(core.Boolean)
		if !ok || actualBoolean != boolean {
			return "projected element boolean mismatch"
		}
		return ""
	}
	if expectedReal, ok := expectedFloat64(expected); ok {
		actualReal, ok := expectedFloat64(actual)
		if !ok {
			return "projected element is not a real"
		}
		if mathFloat64Bits(actualReal) != mathFloat64Bits(expectedReal) {
			return "projected element real mismatch"
		}
		return ""
	}
	if _, ok := objectField(expected, "kind"); ok {
		return hclAssertProjectedValue(actual, expected)
	}
	return "unsupported expected element"
}

func runHclProjectionSamples(vector *caseData, samples []core.Value, report *SuiteReport) {
	codes, hasCodes := sequenceField(vector.Expected, "codes")
	literals, hasLiterals := sequenceField(vector.Expected, "literals")
	for index, sample := range samples {
		formed, message := hclFormSample(vector, sample)
		if message != "" {
			failHclCase(vector, report, message)
			return
		}
		doc, message := formed.documentOrFail()
		if message != "" {
			failHclCase(vector, report, message)
			return
		}
		request, message := hclProjectionRequest(vector)
		if message != "" {
			failHclCase(vector, report, message)
			return
		}
		result := doc.Project(request)
		if hasCodes {
			if expectedCodeValue, ok := codes[index].(core.String); ok {
				if result.Failed == nil {
					failHclCase(vector, report, "projection must fail")
					return
				}
				code := result.Failed.Diagnostics[0].Code
				if code != string(expectedCodeValue) {
					failHclCase(vector, report, fmt.Sprintf("projection code %s != %s",
						code, string(expectedCodeValue)))
					return
				}
			}
		}
		if hasLiterals {
			expectedLiteralValue, ok := literals[index].(core.Boolean)
			if !ok {
				failHclCase(vector, report, "expected literal must be a boolean")
				return
			}
			completed := result.Complete != nil
			if completed != bool(expectedLiteralValue) {
				failHclCase(vector, report, fmt.Sprintf("sample %d projection completion %v != %v",
					index, completed, bool(expectedLiteralValue)))
				return
			}
		}
	}
	passHclCase(vector, report)
}

// ---------------------------------------------------------------------------
// Materialization
// ---------------------------------------------------------------------------

// hclMaterializationRequest builds the request of one vector input.
func hclMaterializationRequest(vector *caseData) (document.MaterializationRequest, string) {
	style, ok := stringField(vector.Input, "style")
	if !ok {
		return document.MaterializationRequest{}, "missing input.style"
	}
	profileName, ok := stringField(vector.Input, "profile")
	if !ok {
		return document.MaterializationRequest{}, "missing input.profile"
	}
	profile, ok := hclProfile(profileName)
	if !ok {
		return document.MaterializationRequest{}, "unknown profile " + profileName
	}
	switch style {
	case "hcl.canonical-document@1":
		return document.NewMaterializationRequest(profile.ID(),
			document.NewMaterializationStyleId("hcl.canonical-document", 1)), ""
	default:
		return document.MaterializationRequest{}, "unknown materialization style " + style
	}
}

// hclMaterializationFailureCode maps one materialization failure onto its
// vector spelling.
func hclMaterializationFailureCode(failure *hcl.MaterializationFailure) string {
	switch failure.Kind {
	case hcl.MaterializationFailureInvalidRequest:
		return "invalid-record"
	case hcl.MaterializationFailureUnsupportedProfile:
		return "unsupported-profile"
	case hcl.MaterializationFailureUnsupportedStyle:
		return "unsupported-style"
	case hcl.MaterializationFailureUnsupportedEncoding:
		return "unsupported-encoding"
	case hcl.MaterializationFailureUnsupportedNewline:
		return "unsupported-newline"
	case hcl.MaterializationFailureUnrepresentable:
		return "hcl.materialization.unrepresentable@1"
	case hcl.MaterializationFailureResourceLimit:
		return "hcl.materialization.resource-limit@1"
	case hcl.MaterializationFailureFormationFailed:
		return "formation-failed"
	}
	return "formation-failed"
}

func runHclMaterializationCase(vector *caseData, report *SuiteReport) {
	if samples, ok := sequenceField(vector.Input, "samples"); ok {
		runHclMaterializationSamples(vector, samples, report)
		return
	}
	request, message := hclMaterializationRequest(vector)
	if message != "" {
		failHclCase(vector, report, message)
		return
	}
	record, ok := objectField(vector.Input, "record")
	if !ok {
		failHclCase(vector, report, "missing input.record")
		return
	}
	if failure, ok := hclExpectedString(vector, "failure"); ok {
		result := hcl.Materialize(record, request)
		if result.Failed == nil {
			failHclCase(vector, report, "materialization must fail")
			return
		}
		if hclMaterializationFailureCode(result.Failed.Failure) != failure {
			failHclCase(vector, report, fmt.Sprintf("failure %s != %s",
				hclMaterializationFailureCode(result.Failed.Failure), failure))
			return
		}
		passHclCase(vector, report)
		return
	}
	complete, message := hclCompleteMaterialization(record, request)
	if message != "" {
		failHclCase(vector, report, message)
		return
	}
	if render, ok := hclExpectedString(vector, "render"); ok {
		actual := string(complete.Document.Render())
		if actual != render {
			failHclCase(vector, report, fmt.Sprintf("render %q != %q", actual, render))
			return
		}
	}
	if closure, ok := booleanField(vector.Expected, "closure"); ok && closure {
		if complete.Document.FormationStatus() != document.FormationStatusComplete {
			failHclCase(vector, report, "materialized document must be complete")
			return
		}
	}
	if fingerprintMatch, ok := booleanField(vector.Expected, "fingerprint_match"); ok && fingerprintMatch {
		if message := hclAssertFingerprintMatch(complete, record); message != "" {
			failHclCase(vector, report, message)
			return
		}
	}
	passHclCase(vector, report)
}

// hclCompleteMaterialization runs one complete materialization.
func hclCompleteMaterialization(record core.Value,
	request document.MaterializationRequest) (*hcl.CompleteMaterialization, string) {
	result := hcl.Materialize(record, request)
	if result.Complete == nil {
		return nil, "materialization failed: " + result.Failed.Failure.Error()
	}
	return result.Complete, ""
}

// hclAssertFingerprintMatch asserts that every `hcl.expression@1` record
// of the input record is reproduced by the re-projection of the
// materialized document.
func hclAssertFingerprintMatch(complete *hcl.CompleteMaterialization, record core.Value) string {
	request := hcl.ProjectionRequestBodyWithExpressionPolicy(hcl.ExpressionPolicyProjectExpression)
	result := complete.Document.Project(request)
	if result.Complete == nil {
		return "materialized document must re-project"
	}
	projection := result.Complete
	items, ok := sequenceField(record, "items")
	if !ok {
		return "missing record items"
	}
	projectedItems, message := hclProjectedItems(projection.Value)
	if message != "" {
		return message
	}
	var projectedAttributes []core.Value
	for _, item := range projectedItems {
		if kind, ok := hclItemKind(item); ok && kind == "attribute" {
			projectedAttributes = append(projectedAttributes, item)
		}
	}
	for _, item := range items {
		kind, ok := hclItemKind(item)
		if !ok || kind != "attribute" {
			continue
		}
		value, ok := objectField(item, "value")
		if !ok {
			continue
		}
		valueKind, ok := stringField(value, "kind")
		if !ok || valueKind != "expression" {
			continue
		}
		name, ok := stringField(item, "name")
		if !ok {
			return "missing attribute name"
		}
		expectedExpression, ok := objectField(value, "expression")
		if !ok {
			return "missing expression record"
		}
		var projected core.Value
		for _, candidate := range projectedAttributes {
			candidateName, _ := stringField(candidate, "name")
			if candidateName == name {
				projected = candidate
				break
			}
		}
		if projected == nil {
			return "projected attribute " + name + " not found"
		}
		projectedValue, ok := objectField(projected, "value")
		if !ok {
			return "missing projected value"
		}
		actualKind, ok := stringField(projectedValue, "kind")
		if !ok {
			return "missing projected expression kind"
		}
		expectedKind, ok := stringField(expectedExpression, "kind")
		if !ok {
			return "missing expected expression kind"
		}
		if actualKind != expectedKind {
			return fmt.Sprintf("expression kind %s != %s", actualKind, expectedKind)
		}
		actualText, ok := stringField(projectedValue, "text")
		if !ok {
			return "missing projected expression text"
		}
		expectedText, ok := stringField(expectedExpression, "text")
		if !ok {
			return "missing expected expression text"
		}
		if actualText != expectedText {
			return fmt.Sprintf("expression text %q != %q", actualText, expectedText)
		}
		actualRecord, ok := stringField(projectedValue, "record")
		if !ok {
			return "missing projected expression record"
		}
		expectedRecord, ok := stringField(expectedExpression, "record")
		if !ok {
			return "missing expected expression record"
		}
		if actualRecord != expectedRecord {
			return fmt.Sprintf("expression record %s != %s", actualRecord, expectedRecord)
		}
	}
	return ""
}

func runHclMaterializationSamples(vector *caseData, samples []core.Value, report *SuiteReport) {
	renders, hasRenders := sequenceField(vector.Expected, "renders")
	codes, hasCodes := sequenceField(vector.Expected, "codes")
	closure, _ := booleanField(vector.Expected, "closure")
	if !hasRenders && !hasCodes {
		failHclCase(vector, report, "missing expected.codes")
		return
	}
	expectedLength := len(codes)
	if hasRenders {
		expectedLength = len(renders)
	}
	if len(samples) != expectedLength {
		failHclCase(vector, report, "render/code count mismatch")
		return
	}
	for index, sample := range samples {
		style, ok := stringField(sample, "style")
		if !ok {
			style, ok = stringField(vector.Input, "style")
			if !ok {
				failHclCase(vector, report, "missing sample style")
				return
			}
		}
		profileValue, ok := objectField(sample, "profile")
		if !ok {
			profileValue, ok = objectField(vector.Input, "profile")
			if !ok {
				failHclCase(vector, report, "missing sample profile")
				return
			}
		}
		request, message := hclMaterializationRequestFor(style, profileValue)
		if message != "" {
			failHclCase(vector, report, message)
			return
		}
		record, ok := objectField(sample, "record")
		if !ok {
			failHclCase(vector, report, "missing sample record")
			return
		}
		result := hcl.Materialize(record, request)
		if result.Complete != nil {
			if hasRenders {
				expectedRender, ok := renders[index].(core.String)
				if !ok {
					failHclCase(vector, report, "expected render must be a string")
					return
				}
				actual := string(result.Complete.Document.Render())
				if actual != string(expectedRender) {
					failHclCase(vector, report, fmt.Sprintf("render %q != %q", actual, string(expectedRender)))
					return
				}
			} else if hasCodes {
				if _, ok := codes[index].(core.String); ok {
					failHclCase(vector, report, "materialization must fail")
					return
				}
			}
			if closure {
				if result.Complete.Document.FormationStatus() != document.FormationStatusComplete {
					failHclCase(vector, report, "materialized document must be complete")
					return
				}
			}
		} else {
			if !hasCodes {
				failHclCase(vector, report, "materialization must complete")
				return
			}
			expectedCode, ok := codes[index].(core.String)
			if !ok {
				failHclCase(vector, report, "expected code must be a string")
				return
			}
			if hclMaterializationFailureCode(result.Failed.Failure) != string(expectedCode) {
				failHclCase(vector, report, fmt.Sprintf("materialization failure %s != %s",
					hclMaterializationFailureCode(result.Failed.Failure), string(expectedCode)))
				return
			}
		}
	}
	passHclCase(vector, report)
}

// hclMaterializationRequestFor builds the request for one sample.
func hclMaterializationRequestFor(style string, profileValue core.Value) (document.MaterializationRequest, string) {
	profileName, ok := profileValue.(core.String)
	if !ok {
		return document.MaterializationRequest{}, "profile must be a string"
	}
	profile, ok := hclProfile(string(profileName))
	if !ok {
		return document.MaterializationRequest{}, "unknown profile " + string(profileName)
	}
	if style != "hcl.canonical-document@1" {
		return document.MaterializationRequest{}, "unknown materialization style " + style
	}
	return document.NewMaterializationRequest(profile.ID(),
		document.NewMaterializationStyleId("hcl.canonical-document", 1)), ""
}

// ---------------------------------------------------------------------------
// Edit
// ---------------------------------------------------------------------------

// hclEditBodyPath reads the body path of one operation.
func hclEditBodyPath(operation core.Value) (hcl.BodyPath, string) {
	body, ok := stringField(operation, "body")
	if !ok || body == "root" {
		return hcl.BodyPathRoot(), ""
	}
	return hcl.BodyPath{}, "unknown body path " + body
}

// hclEditPlacement reads the placement of one operation.
func hclEditPlacement(operation core.Value) (hcl.BodyPlacement, string) {
	switch placement, ok := stringField(operation, "placement"); {
	case !ok || placement == "Last":
		return hcl.BodyPlacementLast, ""
	case placement == "First":
		return hcl.BodyPlacementFirst, ""
	default:
		return 0, "unknown placement " + placement
	}
}

// hclEditValue reads one typed edit value.
func hclEditValue(value core.Value) (hcl.EditValue, string) {
	kind, ok := stringField(value, "kind")
	if !ok {
		return hcl.EditValue{}, "missing value kind"
	}
	switch kind {
	case "string":
		text, ok := stringField(value, "text")
		if !ok {
			return hcl.EditValue{}, "missing text"
		}
		return hcl.EditValueStringV(text), ""
	case "integer":
		payload, ok := objectField(value, "value")
		if !ok {
			return hcl.EditValue{}, "missing integer value"
		}
		integer, ok := payload.(core.Integer)
		if !ok || !integer.Int().IsInt64() {
			return hcl.EditValue{}, "missing integer value"
		}
		return hcl.EditValueIntegerV(integer.Int().Int64()), ""
	case "real":
		payload, ok := objectField(value, "value")
		if !ok {
			return hcl.EditValue{}, "missing real value"
		}
		real, ok := expectedFloat64(payload)
		if !ok {
			return hcl.EditValue{}, "missing real value"
		}
		return hcl.EditValueRealV(real), ""
	case "boolean":
		payload, ok := objectField(value, "value")
		if !ok {
			return hcl.EditValue{}, "missing boolean value"
		}
		boolean, ok := payload.(core.Boolean)
		if !ok {
			return hcl.EditValue{}, "missing boolean value"
		}
		return hcl.EditValueBooleanV(bool(boolean)), ""
	case "null":
		return hcl.EditValueNullV(), ""
	case "tuple":
		elementsValue, ok := objectField(value, "elements")
		if !ok {
			return hcl.EditValue{}, "missing tuple elements"
		}
		elements, ok := elementsValue.(*core.Array)
		if !ok {
			return hcl.EditValue{}, "tuple elements must be a sequence"
		}
		var values []hcl.EditValue
		for _, element := range elements.Items() {
			value, message := hclEditValue(element)
			if message != "" {
				return hcl.EditValue{}, message
			}
			values = append(values, value)
		}
		return hcl.EditValueTupleV(values), ""
	case "object":
		entriesValue, ok := objectField(value, "entries")
		if !ok {
			return hcl.EditValue{}, "missing object entries"
		}
		entries, ok := entriesValue.(*core.Array)
		if !ok {
			return hcl.EditValue{}, "object entries must be a sequence"
		}
		var values []hcl.EditObjectEntry
		for _, entry := range entries.Items() {
			pair, ok := entry.(*core.Array)
			if !ok || len(pair.Items()) != 2 {
				return hcl.EditValue{}, "entry must be a pair"
			}
			keyText, ok := pair.Items()[0].(core.String)
			if !ok {
				return hcl.EditValue{}, "entry key must be a string"
			}
			key := hcl.EditKeyIdentifierV(string(keyText))
			if number, ok := parseInt64Value(pair.Items()[0]); ok {
				key = hcl.EditKeyNumberV(number)
			}
			entryValue, message := hclEditValue(pair.Items()[1])
			if message != "" {
				return hcl.EditValue{}, message
			}
			values = append(values, hcl.EditObjectEntry{Key: key, Value: entryValue})
		}
		return hcl.EditValueObjectV(values), ""
	case "expression":
		expression, ok := objectField(value, "expression")
		if !ok {
			return hcl.EditValue{}, "missing expression record"
		}
		kind, ok := stringField(expression, "kind")
		if !ok {
			return hcl.EditValue{}, "missing expression kind"
		}
		text, ok := stringField(expression, "text")
		if !ok {
			return hcl.EditValue{}, "missing expression text"
		}
		return hcl.EditValueExpressionV(kind, text), ""
	}
	return hcl.EditValue{}, "unknown value kind " + kind
}

// parseInt64Value reports whether one value is an exact integer and
// returns it.
func parseInt64Value(value core.Value) (int64, bool) {
	integer, ok := value.(core.Integer)
	if !ok || !integer.Int().IsInt64() {
		return 0, false
	}
	return integer.Int().Int64(), true
}

// hclEditBlockNodeRef reads one block node ref.
func hclEditBlockNodeRef(operation core.Value) (hcl.EditNodeRef, string) {
	nodeRefValue, ok := objectField(operation, "node_ref")
	if !ok {
		return hcl.EditNodeRef{}, "missing node_ref"
	}
	blockType, ok := stringField(nodeRefValue, "type")
	if !ok {
		return hcl.EditNodeRef{}, "missing node_ref type"
	}
	labelsValue, ok := objectField(nodeRefValue, "labels")
	if !ok {
		return hcl.EditNodeRef{}, "missing node_ref labels"
	}
	labels, ok := labelsValue.(*core.Array)
	if !ok {
		return hcl.EditNodeRef{}, "node_ref labels must be a sequence"
	}
	var labelTexts []string
	for _, label := range labels.Items() {
		text, ok := label.(core.String)
		if !ok {
			return hcl.EditNodeRef{}, "node_ref label must be a string"
		}
		labelTexts = append(labelTexts, string(text))
	}
	return hcl.EditNodeRef{Body: hcl.BodyPathRoot(), BlockType: blockType,
		Labels: labelTexts, IsBlock: true}, ""
}

// hclBuildTransaction builds one transaction from one operation list.
func hclBuildTransaction(document *hcl.Document, operations []core.Value) (*hcl.EditTransactionBuilder, string) {
	builder := hcl.NewEditTransactionBuilder(document)
	for _, operation := range operations {
		op, ok := stringField(operation, "op")
		if !ok {
			return nil, "missing op"
		}
		switch op {
		case "hcl.edit.set-attribute-value@1":
			body, message := hclEditBodyPath(operation)
			if message != "" {
				return nil, message
			}
			attribute, ok := stringField(operation, "attribute")
			if !ok {
				return nil, "missing attribute"
			}
			valueValue, ok := objectField(operation, "value")
			if !ok {
				return nil, "missing value"
			}
			value, message := hclEditValue(valueValue)
			if message != "" {
				return nil, message
			}
			builder.SetAttributeValue(body, attribute, value)
		case "hcl.edit.insert-attribute@1":
			body, message := hclEditBodyPath(operation)
			if message != "" {
				return nil, message
			}
			name, ok := stringField(operation, "name")
			if !ok {
				return nil, "missing name"
			}
			valueValue, ok := objectField(operation, "value")
			if !ok {
				return nil, "missing value"
			}
			value, message := hclEditValue(valueValue)
			if message != "" {
				return nil, message
			}
			placement, message := hclEditPlacement(operation)
			if message != "" {
				return nil, message
			}
			builder.InsertAttribute(body, name, value, placement)
		case "hcl.edit.remove-attribute@1":
			body, message := hclEditBodyPath(operation)
			if message != "" {
				return nil, message
			}
			attribute, ok := stringField(operation, "attribute")
			if !ok {
				return nil, "missing attribute"
			}
			builder.RemoveAttribute(body, attribute)
		case "hcl.edit.rename-attribute@1":
			body, message := hclEditBodyPath(operation)
			if message != "" {
				return nil, message
			}
			attribute, ok := stringField(operation, "attribute")
			if !ok {
				return nil, "missing attribute"
			}
			name, ok := stringField(operation, "name")
			if !ok {
				return nil, "missing name"
			}
			builder.RenameAttribute(body, attribute, name)
		case "hcl.edit.insert-block@1":
			body, message := hclEditBodyPath(operation)
			if message != "" {
				return nil, message
			}
			blockType, ok := stringField(operation, "type")
			if !ok {
				return nil, "missing block type"
			}
			labelsValue, ok := objectField(operation, "labels")
			if !ok {
				return nil, "missing block labels"
			}
			labels, ok := labelsValue.(*core.Array)
			if !ok {
				return nil, "block labels must be a sequence"
			}
			var labelTexts []string
			for _, label := range labels.Items() {
				text, ok := label.(core.String)
				if !ok {
					return nil, "block label must be a string"
				}
				labelTexts = append(labelTexts, string(text))
			}
			attributesValue, ok := objectField(operation, "attributes")
			if !ok {
				return nil, "missing block attributes"
			}
			attributes, ok := attributesValue.(*core.Array)
			if !ok {
				return nil, "block attributes must be a sequence"
			}
			var typed []hcl.EditBlockAttribute
			for _, attribute := range attributes.Items() {
				name, ok := stringField(attribute, "name")
				if !ok {
					return nil, "missing block attribute name"
				}
				valueValue, ok := objectField(attribute, "value")
				if !ok {
					return nil, "missing attribute value"
				}
				value, message := hclEditValue(valueValue)
				if message != "" {
					return nil, message
				}
				typed = append(typed, hcl.EditBlockAttribute{Name: name, Value: value})
			}
			placement, message := hclEditPlacement(operation)
			if message != "" {
				return nil, message
			}
			builder.InsertBlock(body, blockType, labelTexts, typed, placement)
		case "hcl.edit.remove-block@1":
			nodeRef, message := hclEditBlockNodeRef(operation)
			if message != "" {
				return nil, message
			}
			builder.RemoveBlock(hcl.BodyPathRoot(), nodeRef.BlockType, nodeRef.Labels, 0)
		default:
			return nil, "unknown edit op " + op
		}
	}
	return builder, ""
}

// hclReparse reparses one committed document under its own profile.
func hclReparse(document *hcl.Document) (*hcl.Document, string) {
	profile := hcl.HclProfileNativeV1
	if document.Profile().ID() == "hcl.tfvars" {
		profile = hcl.HclProfileTfvarsV1
	}
	formed, failure := hcl.Parse(context.Background(), document.Render(), profile,
		hcl.HclEncodingSelectionProfileDefault(), hcl.DefaultHclParseLimits())
	if failure != nil {
		return nil, "reparse: " + failure.Error()
	}
	return formed, ""
}

// hclAllLabelsQuoted reports whether every block label of one native body
// tree is quoted.
func hclAllLabelsQuoted(body *hcl.HclBody) bool {
	for _, item := range body.Items() {
		if attribute := item.AsAttribute(); attribute != nil {
			continue
		}
		block := item.AsBlock()
		for i := range block.Labels() {
			if !block.Labels()[i].Quoted() {
				return false
			}
		}
		if !hclAllLabelsQuoted(block.Body()) {
			return false
		}
	}
	return true
}

// hclReplacementSetsEqual compares two replacement sets field by field.
func hclReplacementSetsEqual(left, right []document.SourceReplacement) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].OldStart() != right[index].OldStart() ||
			left[index].OldEnd() != right[index].OldEnd() ||
			string(left[index].Original()) != string(right[index].Original()) ||
			string(left[index].Replacement()) != string(right[index].Replacement()) {
			return false
		}
	}
	return true
}

func runHclEditCase(vector *caseData, report *SuiteReport) {
	if samples, ok := sequenceField(vector.Input, "samples"); ok {
		runHclEditConflicts(vector, samples, report)
		return
	}
	formed, message := hclFormValue(vector)
	if message != "" {
		failHclCase(vector, report, message)
		return
	}
	doc, message := formed.documentOrFail()
	if message != "" {
		failHclCase(vector, report, message)
		return
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		failHclCase(vector, report, "edit input must form completely")
		return
	}
	operations, ok := sequenceField(vector.Input, "operations")
	if !ok {
		failHclCase(vector, report, "missing input.operations")
		return
	}
	builder, message := hclBuildTransaction(doc, operations)
	if message != "" {
		failHclCase(vector, report, message)
		return
	}
	transaction := builder.Build()
	commit, failure := doc.Commit(transaction)
	if failure != nil {
		failHclCase(vector, report, failure.Error())
		return
	}
	if message := hclAssertEditFacts(doc, transaction, commit, vector); message != "" {
		failHclCase(vector, report, message)
		return
	}
	passHclCase(vector, report)
}

// hclAssertEditFacts asserts the vector facts of one committed edit
// against its base document.
func hclAssertEditFacts(base *hcl.Document, transaction *hcl.EditTransaction,
	commit *hcl.EditCommit, vector *caseData) string {
	committed := commit.Document
	if committed.FormationStatus() != document.FormationStatusComplete {
		return "committed document must be complete"
	}
	if render, ok := hclExpectedString(vector, "render"); ok {
		actual := string(committed.Render())
		if actual != render {
			return fmt.Sprintf("render %q != %q", actual, render)
		}
	}
	if reparseClosure, ok := booleanField(vector.Expected, "reparse_closure"); ok && reparseClosure {
		reparsed, message := hclReparse(committed)
		if message != "" {
			return message
		}
		if reparsed.FormationStatus() != document.FormationStatusComplete {
			return "committed document must reparse completely"
		}
	}
	if untouched, ok := booleanField(vector.Expected, "untouched_byte_proof"); ok && untouched {
		if err := commit.UntouchedProof.Verify(base.Source(), committed.Source(),
			commit.SourcePatch.Replacements()); err != nil {
			return "untouched proof: " + err.Error()
		}
	}
	if patchReplays, ok := booleanField(vector.Expected, "patch_replays"); ok && patchReplays {
		replay, err := commit.SourcePatch.Apply(base.Source(), document.DefaultSourcePatchLimits())
		if err != nil {
			return "patch apply: " + err.Error()
		}
		if string(replay.Bytes()) != string(committed.Render()) {
			return "patch does not replay"
		}
	}
	if labelsQuoted, ok := booleanField(vector.Expected, "labels_always_quoted"); ok && labelsQuoted {
		if !hclAllLabelsQuoted(committed.Document().Body()) {
			return "a block label is not quoted"
		}
	}
	if dryRun, ok := booleanField(vector.Expected, "dry_run_equivalent"); ok && dryRun {
		plan, failure := base.DryRun(transaction, "hcl-conformance")
		if failure != nil {
			return "dry run: " + failure.Error()
		}
		if !hclReplacementSetsEqual(plan.Replacements(), commit.SourcePatch.Replacements()) {
			return "dry-run replacement set differs from the committed replacement set"
		}
	}
	return ""
}

func runHclEditConflicts(vector *caseData, samples []core.Value, report *SuiteReport) {
	codes, ok := sequenceField(vector.Expected, "codes")
	if !ok {
		failHclCase(vector, report, "missing expected.codes")
		return
	}
	baseUnchanged, _ := booleanField(vector.Expected, "base_unchanged")
	if len(samples) != len(codes) {
		failHclCase(vector, report, "code count mismatch")
		return
	}
	for index, sample := range samples {
		formed, message := hclFormSample(vector, sample)
		if message != "" {
			failHclCase(vector, report, message)
			return
		}
		doc, message := formed.documentOrFail()
		if message != "" {
			failHclCase(vector, report, message)
			return
		}
		operationsValue, ok := objectField(sample, "operations")
		if !ok {
			failHclCase(vector, report, "missing operations")
			return
		}
		operations, ok := operationsValue.(*core.Array)
		if !ok {
			failHclCase(vector, report, "operations must be a sequence")
			return
		}
		var transaction *hcl.EditTransaction
		if wrongSourceValue, ok := objectField(sample, "wrong_source"); ok {
			// The transaction is bound to another document's snapshot.
			wrongVector := &caseData{Input: wrongSourceValue}
			wrongFormed, message := hclFormValue(wrongVector)
			if message != "" {
				failHclCase(vector, report, message)
				return
			}
			other, message := wrongFormed.documentOrFail()
			if message != "" {
				failHclCase(vector, report, message)
				return
			}
			builder, message := hclBuildTransaction(other, operations.Items())
			if message != "" {
				failHclCase(vector, report, message)
				return
			}
			transaction = builder.Build()
		} else {
			builder, message := hclBuildTransaction(doc, operations.Items())
			if message != "" {
				failHclCase(vector, report, message)
				return
			}
			transaction = builder.Build()
		}
		_, failure := doc.Commit(transaction)
		if failure == nil {
			failHclCase(vector, report, "edit must fail")
			return
		}
		expectedCodeValue, ok := codes[index].(core.String)
		if !ok {
			failHclCase(vector, report, "expected code must be a string")
			return
		}
		if failure.Code() != string(expectedCodeValue) {
			failHclCase(vector, report, fmt.Sprintf("edit failure %s != %s",
				failure.Code(), string(expectedCodeValue)))
			return
		}
		if baseUnchanged {
			if string(doc.Render()) != string(doc.Source().Bytes()) {
				failHclCase(vector, report, "base document changed")
				return
			}
		}
	}
	passHclCase(vector, report)
}
