package conformance

// The `consema.conformance@1` suite runner (crates/consema-conformance
// src/lib.rs run_v1). The 0.14.0 milestone implements the core/PVCE surface
// (value.*, pvce.*) and the QueryDefinition protocol surface
// (query.reject-role-mismatch, query.protocol-roundtrip); the JSON-family
// cases (parse/query/projection/edit) and the portable query execution
// cases are documented skips until 0.15.0.

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/protocol"
)

// runV1 executes the embedded `consema.conformance@1` suite.
func runV1(_ *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		switch vector.ID {
		case "value.integer-arbitrary-precision":
			runIntegerPrecision(vector, report)
		case "value.decimal-normalization":
			runDecimalNormalization(vector, report)
		case "value.float-signed-zero":
			runFloatSignedZero(vector, report)
		case "pvce.null-vector", "pvce.negative-integer-vector", "pvce.object-vector":
			runPVCEVector(vector, report)
		case "pvce.reject-nonminimal-varint":
			runPVCERejection(vector, report)
		case "pvce.encode-blob-limit":
			runPVCEBlobLimit(vector, report)
		case "query.reject-role-mismatch":
			runQueryRoleMismatch(vector, report)
		case "query.protocol-roundtrip":
			runQueryProtocolRoundtrip(vector, report)
		case "parse.strict-exact-roundtrip", "parse.jsonc-comments-trailing-comma",
			"parse.recovery-missing-close", "parse.duplicate-members",
			"parse.lossless-byte-coverage":
			RunV1JSONFace(vector, report)
		case "query.json-duplicate-order":
			RunV1JSONFace(vector, report)
		case "query.root-result-limit", "query.cursor-failure-terminal":
			RunV1PortableQueryFace(vector, report)
		case "projection.best-exact-duplicate-mapping", "projection.object-reject-duplicates",
			"projection.object-last-wins", "projection.object-key-provenance":
			RunV1JSONFace(vector, report)
		case "edit.scalar-minimal", "edit.preserve-decimal-scale",
			"edit.preserve-exponent-style", "edit.canonical-for-profile",
			"edit.preserve-else-canonical", "edit.preserve-incompatible-rejected",
			"edit.wrong-snapshot":
			RunV1JSONFace(vector, report)
		case "resource.parse-token-limit":
			RunV1JSONFace(vector, report)
		default:
			report.Failed = append(report.Failed, CaseFailure{
				ID:      vector.ID,
				Message: "runner does not recognize published v1 case",
			})
		}
	}
	return report
}

func runIntegerPrecision(vector *caseData, report *SuiteReport) {
	text, ok := caseInput(vector, "decimal")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.decimal"})
		return
	}
	number, ok := text.(core.String)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "input.decimal must be String"})
		return
	}
	expected, ok := caseExpected(vector, "decimal")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.decimal"})
		return
	}
	integer, ok := expected.(core.String)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected.decimal must be String"})
		return
	}
	value, ok := new(big.Int).SetString(string(number), 10)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "input is not a decimal integer"})
		return
	}
	if value.String() != string(integer) {
		report.Failed = append(report.Failed, CaseFailure{
			ID: vector.ID, Message: fmt.Sprintf("integer %s != %s", value.String(), string(integer)),
		})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runDecimalNormalization(vector *caseData, report *SuiteReport) {
	left, ok := caseInput(vector, "left")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.left"})
		return
	}
	right, ok := caseInput(vector, "right")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.right"})
		return
	}
	leftDecimal, err := parseDecimalNumber(left)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
		return
	}
	rightDecimal, err := parseDecimalNumber(right)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
		return
	}
	equal, _ := booleanField(vector.Expected, "strict_equal")
	hashEqual, _ := booleanField(vector.Expected, "strict_hash_equal")
	if !equal || !hashEqual {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected strict equality"})
		return
	}
	if !core.Equal(leftDecimal, rightDecimal) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "decimals are not strictly equal"})
		return
	}
	if core.Hash(leftDecimal) != core.Hash(rightDecimal) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "decimal hashes differ"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// parseDecimalNumber parses one JSON-number spelling ("1.00", "10e-1")
// into its canonical coefficient × 10^exponent decimal.
func parseDecimalNumber(value core.Value) (core.Decimal, error) {
	text, ok := value.(core.String)
	if !ok {
		return core.Decimal{}, fmt.Errorf("decimal input must be String")
	}
	source := string(text)
	coefficientText := source
	exponent := big.NewInt(0)
	if index := strings.IndexAny(source, "eE"); index >= 0 {
		exponentText := source[index+1:]
		coefficientText = source[:index]
		parsed, ok := new(big.Int).SetString(exponentText, 10)
		if !ok {
			return core.Decimal{}, fmt.Errorf("invalid decimal exponent %q", exponentText)
		}
		exponent = parsed
	}
	scale := big.NewInt(0)
	if index := strings.IndexByte(coefficientText, '.'); index >= 0 {
		fraction := coefficientText[index+1:]
		coefficientText = coefficientText[:index] + fraction
		scale = big.NewInt(int64(-len(fraction)))
	}
	if coefficientText == "" || coefficientText == "-" || coefficientText == "+" {
		return core.Decimal{}, fmt.Errorf("invalid decimal coefficient %q", coefficientText)
	}
	coefficient, ok := new(big.Int).SetString(coefficientText, 10)
	if !ok {
		return core.Decimal{}, fmt.Errorf("invalid decimal coefficient %q", coefficientText)
	}
	exponent.Add(exponent, scale)
	return core.NewDecimal(coefficient, exponent), nil
}

func runFloatSignedZero(vector *caseData, report *SuiteReport) {
	positive, ok := caseInput(vector, "positive_bits")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.positive_bits"})
		return
	}
	negative, ok := caseInput(vector, "negative_bits")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.negative_bits"})
		return
	}
	positiveBits, err := parseHex64(positive)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
		return
	}
	negativeBits, err := parseHex64(negative)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
		return
	}
	strictEqual, _ := booleanField(vector.Expected, "strict_equal")
	if core.Equal(core.NewBinaryFloat64(positiveBits), core.NewBinaryFloat64(negativeBits)) != strictEqual {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "signed-zero strict equality differs"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func parseHex64(value core.Value) (uint64, error) {
	text, ok := value.(core.String)
	if !ok {
		return 0, fmt.Errorf("hex input must be String")
	}
	decoded, err := hex.DecodeString(string(text))
	if err != nil || len(decoded) != 8 {
		return 0, fmt.Errorf("expected 8 hex bytes")
	}
	var bits uint64
	for _, byte := range decoded {
		bits = bits<<8 | uint64(byte)
	}
	return bits, nil
}

// pvceValueFromInput builds the PVCE-encodable value from the compact
// vector descriptors ("Null", booleans, {"integer": "..."},
// {"decimal": "..."}, {"string": "..."}, {"sequence": [...]},
// {"object": {...}}, and bare object descriptors).
func pvceValueFromInput(input core.Value) (core.Value, error) {
	switch value := input.(type) {
	case core.String:
		if string(value) == "Null" {
			return core.NullValue(), nil
		}
		return value, nil
	case core.Boolean:
		return value, nil
	case core.Integer:
		return value, nil
	case *core.Object:
		if integer, ok := value.Get("integer"); ok {
			text, ok := integer.(core.String)
			if !ok {
				return nil, fmt.Errorf("integer descriptor must be String")
			}
			number, ok := new(big.Int).SetString(string(text), 10)
			if !ok {
				return nil, fmt.Errorf("invalid integer descriptor %q", string(text))
			}
			return core.NewInteger(number), nil
		}
		if decimal, ok := value.Get("decimal"); ok {
			return parseDecimalNumber(decimal)
		}
		if stringValue, ok := value.Get("string"); ok {
			text, ok := stringValue.(core.String)
			if !ok {
				return nil, fmt.Errorf("string descriptor must be String")
			}
			return text, nil
		}
		if sequence, ok := value.Get("sequence"); ok {
			items, ok := sequence.(*core.Array)
			if !ok {
				return nil, fmt.Errorf("sequence descriptor must be Sequence")
			}
			values := make([]core.Value, 0, items.Len())
			for _, item := range items.Items() {
				decoded, err := pvceValueFromInput(item)
				if err != nil {
					return nil, err
				}
				values = append(values, decoded)
			}
			return core.NewArray(values...), nil
		}
		if object, ok := value.Get("object"); ok {
			members, ok := object.(*core.Object)
			if !ok {
				return nil, fmt.Errorf("object descriptor must be Object")
			}
			return objectFromEntries(members)
		}
		// Bare object descriptor without a wrapping key.
		return objectFromEntries(value)
	}
	return nil, fmt.Errorf("unrepresentable input descriptor")
}

// objectFromEntries recursively decodes every member through the descriptor
// grammar.
func objectFromEntries(object *core.Object) (core.Value, error) {
	entries := make([]core.Entry, 0, object.Len())
	for _, entry := range object.Entries() {
		decoded, err := pvceValueFromInput(entry.Value)
		if err != nil {
			return nil, err
		}
		entries = append(entries, core.Entry{Key: entry.Key, Value: decoded})
	}
	return core.NewObject(entries...)
}

func runPVCEVector(vector *caseData, report *SuiteReport) {
	var value core.Value
	var err error
	switch vector.ID {
	case "pvce.null-vector":
		value = core.NullValue()
	case "pvce.negative-integer-vector":
		text, ok := caseInput(vector, "integer")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.integer"})
			return
		}
		number, ok := text.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "input.integer must be String"})
			return
		}
		integer, ok := new(big.Int).SetString(string(number), 10)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "invalid integer"})
			return
		}
		value = core.NewInteger(integer)
	case "pvce.object-vector":
		object, ok := caseInput(vector, "object")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.object"})
			return
		}
		value, err = pvceValueFromInput(object)
		if err != nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
			return
		}
	}
	expected, ok := caseExpected(vector, "hex")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.hex"})
		return
	}
	expectedHex, ok := expected.(core.String)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected.hex must be String"})
		return
	}
	encoded, err := core.EncodePVCE(value)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
		return
	}
	if hex.EncodeToString(encoded) != string(expectedHex) {
		report.Failed = append(report.Failed, CaseFailure{
			ID: vector.ID, Message: fmt.Sprintf("pvce hex %s != %s",
				hex.EncodeToString(encoded), string(expectedHex)),
		})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPVCERejection(vector *caseData, report *SuiteReport) {
	text, ok := caseInput(vector, "hex")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.hex"})
		return
	}
	hexText, ok := text.(core.String)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "input.hex must be String"})
		return
	}
	bytes, err := hex.DecodeString(string(hexText))
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "invalid hex"})
		return
	}
	_, err = core.DecodePVCE(bytes, core.DefaultDecodeLimits())
	if err == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "published rejection vector must fail"})
		return
	}
	pvceError, ok := err.(*core.PVCEError)
	if !ok || pvceError.Kind != core.ErrNonCanonicalVarint {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected NonCanonicalVarint"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPVCEBlobLimit(vector *caseData, report *SuiteReport) {
	valueValue, ok := caseInput(vector, "value")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.value"})
		return
	}
	value, err := pvceValueFromInput(valueValue)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
		return
	}
	limit, ok := integerFieldValue(vector.Input, "max_blob_bytes")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.max_blob_bytes"})
		return
	}
	limits := core.DefaultEncodeLimits()
	limits.MaxBlobBytes = int(limit)
	_, err = core.EncodePVCEBounded(value, limits)
	if err == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "encode must exceed the blob limit"})
		return
	}
	pvceError, ok := err.(*core.PVCEError)
	if !ok || pvceError.Kind != core.ErrResourceLimit || pvceError.Field != "blob-bytes" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected ResourceLimit blob-bytes"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func integerFieldValue(value core.Value, name string) (uint64, bool) {
	field, ok := objectField(value, name)
	if !ok {
		return 0, false
	}
	integer, ok := field.(core.Integer)
	if !ok {
		return 0, false
	}
	number := integer.Int()
	if number.Sign() < 0 || number.BitLen() > 64 {
		return 0, false
	}
	return number.Uint64(), true
}

func runQueryRoleMismatch(vector *caseData, report *SuiteReport) {
	// The expression is built from the vector input.pipeline, exactly like
	// the Rust runner's pipeline helper (consema-conformance/src/lib.rs:
	// 564-582): the last operator of the pipeline must be a role mismatch on
	// the portable-value domain.
	pipelineValue, ok := caseInput(vector, "pipeline")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.pipeline"})
		return
	}
	pipeline, ok := pipelineValue.(*core.Array)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "input.pipeline must be Sequence"})
		return
	}
	expression := &protocol.QueryExpression{Kind: protocol.ExpressionInput}
	for _, item := range pipeline.Items() {
		text, ok := item.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "pipeline descriptor must be String"})
			return
		}
		id, version, ok := strings.Cut(string(text), "@")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{
				ID: vector.ID, Message: "pipeline descriptor lacks version: " + string(text),
			})
			return
		}
		var versionNumber uint64
		if _, err := fmt.Sscanf(version, "%d", &versionNumber); err != nil {
			report.Failed = append(report.Failed, CaseFailure{
				ID: vector.ID, Message: "invalid pipeline version: " + string(text),
			})
			return
		}
		expression = expression.Then(protocol.NewOperatorCall(id, uint32(versionNumber)))
	}
	definition := protocol.NewQueryDefinition(protocol.DomainPortableValueV1()).WithExpression(expression)
	_, failure := definition.Validate()
	if failure == nil || failure.Kind != protocol.FailureInvalidOperatorComposition {
		report.Failed = append(report.Failed, CaseFailure{
			ID: vector.ID, Message: "expected InvalidOperatorComposition",
		})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runQueryProtocolRoundtrip(vector *caseData, report *SuiteReport) {
	domainText, ok := caseInput(vector, "domain")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.domain"})
		return
	}
	if string(domainText.(core.String)) != "core.portable-value-query@1" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "unknown domain"})
		return
	}
	operatorText, ok := caseInput(vector, "operator")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.operator"})
		return
	}
	operator, ok := operatorText.(core.String)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "input.operator must be String"})
		return
	}
	selectionText, ok := caseInput(vector, "selection")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.selection"})
		return
	}
	selection, ok := selectionText.(core.String)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "input.selection must be String"})
		return
	}
	id, version, ok := strings.Cut(string(operator), "@")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "operator lacks version"})
		return
	}
	var versionNumber uint64
	if _, err := fmt.Sscanf(version, "%d", &versionNumber); err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "invalid operator version"})
		return
	}
	var selectionKind protocol.QuerySelection
	switch string(selection) {
	case "All":
		selectionKind = protocol.SelectionAll
	case "First":
		selectionKind = protocol.SelectionFirst
	case "Last":
		selectionKind = protocol.SelectionLast
	case "ZeroOrOne":
		selectionKind = protocol.SelectionZeroOrOne
	case "RequireOne":
		selectionKind = protocol.SelectionRequireOne
	default:
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "unknown selection"})
		return
	}
	definition := protocol.NewQueryDefinition(protocol.DomainPortableValueV1()).
		WithExpression((&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
			Then(protocol.NewOperatorCall(id, uint32(versionNumber)))).
		WithSelection(selectionKind)
	value, failure := definition.ToProtocolValue()
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: failure.Error()})
		return
	}
	decoded := &protocol.QueryDefinition{}
	roundtripped, failure := decoded.FromProtocolValue(value)
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: failure.Error()})
		return
	}
	before, err := core.EncodePVCE(value)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
		return
	}
	roundtripValue, failure := roundtripped.ToProtocolValue()
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: failure.Error()})
		return
	}
	after, err := core.EncodePVCE(roundtripValue)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
		return
	}
	roundtripEqual, _ := booleanField(vector.Expected, "roundtrip_equal")
	unknownField, _ := stringField(vector.Expected, "unknown_field")
	if !roundtripEqual || unknownField != "Reject" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "unexpected expectation facts"})
		return
	}
	if string(before) != string(after) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "protocol value is not round-trip stable"})
		return
	}
	// The envelope path preserves the definition through the protocol
	// message record.
	registry := protocol.NewContractRegistry(protocol.RegistryV1)
	contract, err := protocol.NewContractId("core.query-definition", 1)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
		return
	}
	message, err := protocol.NewProtocolMessage(contract, value, registry)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
		return
	}
	envelopeValue, err := message.ToValue()
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
		return
	}
	envelope := &protocol.ProtocolMessage{}
	decodedEnvelope, err := envelope.FromValue(envelopeValue, registry)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
		return
	}
	decodedDefinition := &protocol.QueryDefinition{}
	_, failure = decodedDefinition.FromProtocolValue(decodedEnvelope.Payload())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: failure.Error()})
		return
	}
	// An unknown field appended to the protocol value is rejected.
	object, ok := value.(*core.Object)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "protocol value must be Object"})
		return
	}
	entries := append([]core.Entry(nil), object.Entries()...)
	entries = append(entries, core.Entry{Key: "unknown", Value: core.NullValue()})
	invalid, err := core.NewObject(entries...)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
		return
	}
	rejecting := &protocol.QueryDefinition{}
	if _, failure := rejecting.FromProtocolValue(invalid); failure == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "unknown field must be rejected"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func skipJSONFamily(vector *caseData, report *SuiteReport) {
	report.Skipped = append(report.Skipped, SkipRecord{
		ID: vector.ID, Capability: vector.Capability,
		Reason: "JSON family formation/query surface lands with 0.15.0 (G1.2)",
	})
}

func skipQueryExecution(vector *caseData, report *SuiteReport) {
	report.Skipped = append(report.Skipped, SkipRecord{
		ID: vector.ID, Capability: vector.Capability,
		Reason: "portable-value query execution lands with the family packages (0.15.0+)",
	})
}

func skipJSONProjection(vector *caseData, report *SuiteReport) {
	report.Skipped = append(report.Skipped, SkipRecord{
		ID: vector.ID, Capability: vector.Capability,
		Reason: "JSON projection surface lands with 0.15.0 (G1.2)",
	})
}

func skipJSONEdit(vector *caseData, report *SuiteReport) {
	report.Skipped = append(report.Skipped, SkipRecord{
		ID: vector.ID, Capability: vector.Capability,
		Reason: "JSON edit surface lands with 0.15.0 (G1.2)",
	})
}
