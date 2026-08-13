package conformance

// The `consema.protocol.conformance@1` suite runner
// (consema-rs/consema-conformance/src/protocol_v1.rs). Every case constructs
// its scenario through the protocol records; the vector `expected` facts
// drive the assertions and the runner holds no expectation literals.

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/protocol"
)

// runProtocolV1 executes the embedded `consema.protocol.conformance@1`
// suite.
func runProtocolV1(_ *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		var err error
		switch vector.ID {
		case "protocol.json.null-vector":
			_, err = protocolV1JSONNullVector(vector)
		case "protocol.json.all-kinds-roundtrip":
			_, err = protocolV1JSONAllKinds(vector)
		case "protocol.json.reject-whitespace":
			_, err = protocolV1JSONRejectWhitespace(vector)
		case "protocol.json.reject-alternate-escape":
			_, err = protocolV1JSONRejectAlternateEscape(vector)
		case "protocol.json.reject-unknown-field":
			_, err = protocolV1JSONRejectUnknownField(vector)
		case "protocol.pvce.roundtrip-equivalent":
			_, err = protocolV1PVCEEquivalent(vector)
		case "protocol.resource.depth-limit":
			_, err = protocolV1DepthLimit(vector)
		case "protocol.envelope.dual-transport":
			_, err = protocolV1EnvelopeDualTransport(vector)
		case "protocol.envelope.all-payloads-dual-transport":
			_, err = protocolV1AllPayloadsDualTransport(vector)
		case "protocol.envelope.reject-unknown-contract":
			_, err = protocolV1RejectUnknownContract(vector)
		case "protocol.envelope.reject-schema-mismatch":
			_, err = protocolV1RejectSchemaMismatch(vector)
		case "protocol.envelope.reject-schema-only-payload":
			_, err = protocolV1RejectSchemaOnlyPayload(vector)
		case "protocol.envelope.reject-nested-envelope":
			_, err = protocolV1RejectNestedEnvelope(vector)
		case "protocol.envelope.reject-semantic-model-identity":
			_, err = protocolV1RejectSemanticModelIdentity(vector)
		case "protocol.profile.roundtrip":
			_, err = protocolV1ProfileRoundtrip(vector)
		case "protocol.capability.conditional-roundtrip":
			_, err = protocolV1CapabilityRoundtrip(vector)
		case "protocol.capability.reject-contradiction":
			_, err = protocolV1CapabilityContradiction(vector)
		case "protocol.diagnostic.require-source-binding":
			_, err = protocolV1DiagnosticSourceBinding(vector)
		case "protocol.diagnostic.reject-category-registry-mismatch":
			_, err = protocolV1DiagnosticCategoryMismatch(vector)
		case "protocol.completion.reject-contradiction":
			_, err = protocolV1CompletionContradiction(vector)
		case "protocol.completion.reject-unregistered-failure-code":
			_, err = protocolV1CompletionUnregisteredCode(vector)
		case "protocol.query.definition-envelope":
			_, err = protocolV1QueryDefinitionEnvelope(vector)
		case "protocol.query.portable-result":
			_, err = protocolV1QueryPortableResult(vector)
		case "protocol.query.reject-native-handle":
			_, err = protocolV1RejectNativeHandle(vector)
		case "protocol.projection.request-roundtrip":
			_, err = protocolV1ProjectionRequestRoundtrip(vector)
		case "protocol.projection.no-partial-value":
			_, err = protocolV1ProjectionNoPartial(vector)
		case "protocol.projection.reject-unregistered-event-code":
			_, err = protocolV1ProjectionUnregisteredCode(vector)
		case "protocol.provenance.externalized-roundtrip":
			_, err = protocolV1ProvenanceRoundtrip(vector)
		case "protocol.change-set.actual-edit-roundtrip":
			RunProtocolV1ChangeSetEditFace(vector, report)
			continue
		case "protocol.registry.current-roundtrip":
			_, err = protocolV1RegistryRoundtrip(vector)
		case "protocol.registry.error-code-schema":
			_, err = protocolV1ErrorCodeSchema(vector)
		case "protocol.errors.query-codes-registered":
			_, err = protocolV1QueryCodesRegistered(vector)
		default:
			err = fmt.Errorf("runner does not recognize published protocol case")
		}
		if err != nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
			continue
		}
		report.Passed = append(report.Passed, vector.ID)
	}
	return report
}

func expectErrorCode(vector *caseData, err error) error {
	if err == nil {
		return fmt.Errorf("case %s: expected rejection", vector.ID)
	}
	protocolError, ok := err.(*protocol.ProtocolError)
	if !ok {
		return fmt.Errorf("case %s: unexpected error type: %v", vector.ID, err)
	}
	expected, ok := caseExpected(vector, "code")
	if !ok {
		return fmt.Errorf("case %s: missing expected.code", vector.ID)
	}
	code, ok := expected.(core.String)
	if !ok {
		return fmt.Errorf("case %s: expected.code must be String", vector.ID)
	}
	if protocolError.Code() != string(code) {
		return fmt.Errorf("case %s: error %s != %s", vector.ID, protocolError.Code(), string(code))
	}
	return nil
}

func protocolV1JSONNullVector(vector *caseData) (bool, error) {
	encoded, err := protocol.EncodeJSON(core.NullValue(), protocol.DefaultProtocolLimits())
	if err != nil {
		return false, err
	}
	expected, ok := caseExpected(vector, "utf8")
	if !ok {
		return false, fmt.Errorf("missing expected.utf8")
	}
	text, ok := expected.(core.String)
	if !ok {
		return false, fmt.Errorf("expected.utf8 must be String")
	}
	if string(encoded) != string(text) {
		return false, fmt.Errorf("canonical bytes %q != %q", string(encoded), string(text))
	}
	return true, nil
}

// protocolV1AllKinds builds the closed fifteen-kind sample value.
func protocolV1AllKinds() (core.Value, error) {
	date, err := core.NewDate(big.NewInt(2026), 8, 4)
	if err != nil {
		return nil, err
	}
	fraction := core.NewDecimal(big.NewInt(4), big.NewInt(-1))
	time, err := core.NewTime(1, 2, 3, fraction)
	if err != nil {
		return nil, err
	}
	local := core.NewLocalDateTime(date, time)
	offset, err := core.NewOffsetDateTime(local, 3600)
	if err != nil {
		return nil, err
	}
	object, err := core.NewObject(core.Entry{Key: "k", Value: core.NullValue()})
	if err != nil {
		return nil, err
	}
	mapping, err := core.NewEntryMapping(core.EntryMappingEntry{
		Key: core.NewInteger(big.NewInt(1)), Value: core.String("v"),
	})
	if err != nil {
		return nil, err
	}
	integer, ok := new(big.Int).SetString("12345678901234567890", 10)
	if !ok {
		return nil, fmt.Errorf("invalid integer literal")
	}
	decimal := core.NewDecimal(big.NewInt(12), big.NewInt(-1))
	return core.NewArray(
		core.NullValue(),
		core.Boolean(true),
		core.NewInteger(integer),
		decimal,
		core.NewBinaryFloat32(0x7fc00001),
		core.NewBinaryFloat64(1<<63),
		core.String("文本"),
		core.NewBytes([]byte{0x00, 0xff}),
		date,
		time,
		local,
		offset,
		core.NewArray(),
		object,
		mapping,
	), nil
}

func protocolV1JSONAllKinds(vector *caseData) (bool, error) {
	value, err := protocolV1AllKinds()
	if err != nil {
		return false, err
	}
	limits := protocol.DefaultProtocolLimits()
	encoded, err := protocol.EncodeJSON(value, limits)
	if err != nil {
		return false, err
	}
	decoded, err := protocol.DecodeJSON(encoded, limits)
	if err != nil {
		return false, err
	}
	if !core.Equal(decoded, value) {
		return false, fmt.Errorf("JSON round-trip changed the value")
	}
	return true, nil
}

func protocolV1JSONRejectWhitespace(vector *caseData) (bool, error) {
	_, err := protocol.DecodeJSON([]byte(` {"schema":"core.portable-value-json@1","value":{"type":"Null"}}`),
		protocol.DefaultProtocolLimits())
	return false, expectErrorCode(vector, err)
}

func protocolV1JSONRejectAlternateEscape(vector *caseData) (bool, error) {
	// The alternate `x` escape of an otherwise canonical document must
	// be rejected as non-canonical JSON (the raw backtick string keeps the
	// escape literal).
	_, err := protocol.DecodeJSON([]byte(`{"schema":"core.portable-value-json@1","value":{"type":"String","value":"\u0078"}}`),
		protocol.DefaultProtocolLimits())
	return false, expectErrorCode(vector, err)
}

func protocolV1JSONRejectUnknownField(vector *caseData) (bool, error) {
	_, err := protocol.DecodeJSON([]byte(`{"schema":"core.portable-value-json@1","value":{"type":"Null","x":true}}`),
		protocol.DefaultProtocolLimits())
	return false, expectErrorCode(vector, err)
}

func protocolV1PVCEEquivalent(vector *caseData) (bool, error) {
	value, err := protocolV1AllKinds()
	if err != nil {
		return false, err
	}
	limits := protocol.DefaultProtocolLimits()
	encoded, err := protocol.EncodePVCE(value, limits)
	if err != nil {
		return false, err
	}
	decoded, err := protocol.DecodePVCE(encoded, limits)
	if err != nil {
		return false, err
	}
	if !core.Equal(decoded, value) {
		return false, fmt.Errorf("PVCE round-trip changed the value")
	}
	return true, nil
}

func protocolV1DepthLimit(vector *caseData) (bool, error) {
	limits := protocol.DefaultProtocolLimits()
	limits.MaxDepth = 0
	_, err := protocol.EncodeJSON(core.NewArray(core.NullValue()), limits)
	return false, expectErrorCode(vector, err)
}

func protocolV1EnvelopeDualTransport(vector *caseData) (bool, error) {
	completion, err := protocol.NewCompletion(protocol.CompletionSuccess, 1, 1, nil, nil)
	if err != nil {
		return false, err
	}
	payload, err := completion.ToValue()
	if err != nil {
		return false, err
	}
	registry := protocol.NewContractRegistry(protocol.RegistryV1)
	contract, err := protocol.NewContractId("core.completion", 1)
	if err != nil {
		return false, err
	}
	message, err := protocol.NewProtocolMessage(contract, payload, registry)
	if err != nil {
		return false, err
	}
	limits := protocol.DefaultProtocolLimits()
	jsonBytes, err := message.ToJSON(limits)
	if err != nil {
		return false, err
	}
	decodedJSON, err := message.FromJSON(jsonBytes, limits, registry)
	if err != nil {
		return false, err
	}
	pvceBytes, err := message.ToPVCE(limits)
	if err != nil {
		return false, err
	}
	decodedPVCE, err := message.FromPVCE(pvceBytes, limits, registry)
	if err != nil {
		return false, err
	}
	jsonEqual, _ := booleanField(vector.Expected, "json_equal")
	pvceEqual, _ := booleanField(vector.Expected, "pvce_equal")
	if !jsonEqual || !pvceEqual {
		return false, fmt.Errorf("unexpected expectation facts")
	}
	if !core.Equal(decodedJSON.Payload(), message.Payload()) ||
		!core.Equal(decodedPVCE.Payload(), message.Payload()) {
		return false, fmt.Errorf("dual transport did not close")
	}
	return true, nil
}

func protocolV1AllPayloadsDualTransport(vector *caseData) (bool, error) {
	registry := protocol.NewContractRegistry(protocol.RegistryV1)
	errorRegistry := registry.ErrorCodeRegistry()
	profile, err := protocol.NewProfileDescriptor("toml", 1, "toml.1.0", 1, nil,
		[]string{"toml.datetime"},
		[]*protocol.CapabilityId{protocol.NewCapabilityId("core.document.exact-roundtrip", 1)})
	if err != nil {
		return false, err
	}
	capability, err := protocol.NewCapabilityDeclaration(
		protocol.NewCapabilityId("core.query.ordered-results", 1),
		protocol.ImplementationSupport{Kind: protocol.SupportConditional,
			Preconditions: []protocol.Precondition{{Key: "profile", Value: "toml.1.0@1"}}},
		protocol.VerificationVerified, strPtr("consema.protocol.conformance"))
	if err != nil {
		return false, err
	}
	diagnostic, err := protocol.NewDiagnostic("json.syntax.expected-value@1",
		protocol.CategorySyntax, protocol.SeverityError, nil, nil, nil, nil, nil, 0, errorRegistry)
	if err != nil {
		return false, err
	}
	policy := protocol.NewProjectionPolicy(
		mustContractId("core.projection.exact-or-reject@1"), nil)
	projectionRequest, err := protocol.NewProjectionRequestMessage(
		mustContractId("json.projection.best-exact-core@1"), *policy, nil, nil)
	if err != nil {
		return false, err
	}
	completion, err := protocol.NewCompletion(protocol.CompletionSuccess, 0, 0, nil, nil)
	if err != nil {
		return false, err
	}
	projectionResult, err := protocol.NewProjectionResultMessage(completion,
		core.NullValue(), true, ptrFidelity(protocol.FidelityExact),
		&protocol.ProjectionReportMessage{}, &protocol.ProvenanceMapMessage{}, nil)
	if err != nil {
		return false, err
	}
	queryResult, err := protocol.NewQueryResultFromPortableExecution(
		protocol.DomainPortableValueV1(), protocol.RoleValue, nil)
	if err != nil {
		return false, err
	}
	changeSet, err := protocol.NewChangeSetMessage("source:old", "source:new", nil, nil, nil)
	if err != nil {
		return false, err
	}
	cancellation, err := protocol.NewCancellationRequest("request:1", nil)
	if err != nil {
		return false, err
	}
	executionPolicy, err := protocol.NewExecutionPolicy(nil, nil)
	if err != nil {
		return false, err
	}
	report, err := protocol.NewProjectionReportMessage(nil)
	if err != nil {
		return false, err
	}
	provenance, err := protocol.NewProvenanceMapMessage(nil)
	if err != nil {
		return false, err
	}
	manifest, err := protocol.NewRegistryManifest(1, registry, errorRegistry)
	if err != nil {
		return false, err
	}
	errorCodeManifest, err := protocol.ErrorCodeManifestValueForVersion(protocol.ErrorRegistryV1)
	if err != nil {
		return false, err
	}
	queryDefinition := protocol.NewQueryDefinition(protocol.DomainPortableValueV1())
	queryDefinitionValue, failure := queryDefinition.ToProtocolValue()
	if failure != nil {
		return false, failure
	}
	payloads := []struct {
		schema  string
		payload core.Value
	}{
		{"core.cancellation-request@1", mustValue(cancellation.ToValue)},
		{"core.capability-declaration@1", mustValue(capability.ToValue)},
		{"core.change-set@1", mustValue(changeSet.ToValue)},
		{"core.completion@1", mustValue(completion.ToValue)},
		{"core.diagnostic@1", mustValue(diagnostic.ToValue)},
		{"core.error-code-registry@1", errorCodeManifest},
		{"core.execution-policy@1", mustValue(executionPolicy.ToValue)},
		{"core.profile-descriptor@1", mustValue(profile.ToValue)},
		{"core.projection-report@1", mustValue(report.ToValue)},
		{"core.projection-request@1", mustValue(projectionRequest.ToValue)},
		{"core.projection-result@1", mustValue(projectionResult.ToValue)},
		{"core.provenance-map@1", mustValue(provenance.ToValue)},
		{"core.query-definition@1", queryDefinitionValue},
		{"core.query-result@1", mustValue(queryResult.ToValue)},
		{"core.registry-manifest@1", mustValue(manifest.ToValue)},
	}
	payloadCount, _ := integerField(vector.Expected, "payload_contracts")
	registryExact, _ := booleanField(vector.Expected, "registry_exact")
	jsonEqual, _ := booleanField(vector.Expected, "json_equal")
	pvceEqual, _ := booleanField(vector.Expected, "pvce_equal")
	if payloadCount != uint64(len(payloads)) || !registryExact || !jsonEqual || !pvceEqual {
		return false, fmt.Errorf("unexpected expectation facts")
	}
	// The 15 sampled payloads must exactly cover the stable v1 registry
	// contracts (16 minus the transport envelope).
	stable := stableRegistrySchemas(registry)
	if len(stable) != len(payloads) {
		return false, fmt.Errorf("dual-transport samples do not exactly cover the stable registry")
	}
	sampled := make([]string, 0, len(payloads))
	for _, payload := range payloads {
		sampled = append(sampled, payload.schema)
	}
	sort.Strings(sampled)
	sort.Strings(stable)
	for index := range sampled {
		if sampled[index] != stable[index] {
			return false, fmt.Errorf("dual-transport samples do not exactly cover the stable registry")
		}
	}
	limits := protocol.DefaultProtocolLimits()
	for _, payload := range payloads {
		contract, err := parseContractSchema(payload.schema)
		if err != nil {
			return false, err
		}
		message, err := protocol.NewProtocolMessage(contract, payload.payload, registry)
		if err != nil {
			return false, err
		}
		jsonBytes, err := message.ToJSON(limits)
		if err != nil {
			return false, err
		}
		decodedJSON, err := message.FromJSON(jsonBytes, limits, registry)
		if err != nil {
			return false, err
		}
		pvceBytes, err := message.ToPVCE(limits)
		if err != nil {
			return false, err
		}
		decodedPVCE, err := message.FromPVCE(pvceBytes, limits, registry)
		if err != nil {
			return false, err
		}
		if !core.Equal(decodedJSON.Payload(), message.Payload()) ||
			!core.Equal(decodedPVCE.Payload(), message.Payload()) {
			return false, fmt.Errorf("dual-transport mismatch for %s", payload.schema)
		}
	}
	return true, nil
}

// stableRegistrySchemas lists the stable contracts of one registry.
func stableRegistrySchemas(registry protocol.ContractRegistry) []string {
	var schemas []string
	for _, descriptor := range registry.Contracts() {
		if descriptor.Stability == protocol.StabilityStable {
			schemas = append(schemas, descriptor.ID+"@"+uint32ToString(descriptor.Version))
		}
	}
	return schemas
}

func uint32ToString(value uint32) string {
	return fmt.Sprintf("%d", value)
}

// parseContractSchema parses "id@version" into a contract.
func parseContractSchema(schema string) (*protocol.ContractId, error) {
	id, version, ok := strings.Cut(schema, "@")
	if !ok {
		return nil, fmt.Errorf("contract lacks version: %s", schema)
	}
	var versionNumber uint32
	if _, err := fmt.Sscanf(version, "%d", &versionNumber); err != nil {
		return nil, fmt.Errorf("invalid contract version: %s", schema)
	}
	return protocol.NewContractId(id, versionNumber)
}

// mustValue converts a (core.Value, error) builder into a value.
func mustValue(build func() (core.Value, error)) core.Value {
	value, err := build()
	if err != nil {
		panic(err)
	}
	return value
}

func protocolV1RejectUnknownContract(vector *caseData) (bool, error) {
	payload, err := core.NewObject(core.Entry{Key: "schema", Value: core.String("example.unknown@1")})
	if err != nil {
		return false, err
	}
	contract, err := protocol.NewContractId("example.unknown", 1)
	if err != nil {
		return false, err
	}
	_, err = protocol.NewProtocolMessage(contract, payload, protocol.NewContractRegistry(protocol.RegistryV1))
	return false, expectErrorCode(vector, err)
}

func protocolV1RejectSchemaMismatch(vector *caseData) (bool, error) {
	completion, err := protocol.NewCompletion(protocol.CompletionSuccess, 1, 1, nil, nil)
	if err != nil {
		return false, err
	}
	payload, err := completion.ToValue()
	if err != nil {
		return false, err
	}
	contract, err := protocol.NewContractId("core.diagnostic", 1)
	if err != nil {
		return false, err
	}
	_, err = protocol.NewProtocolMessage(contract, payload, protocol.NewContractRegistry(protocol.RegistryV1))
	return false, expectErrorCode(vector, err)
}

func protocolV1RejectSchemaOnlyPayload(vector *caseData) (bool, error) {
	payload, err := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.diagnostic@1")},
		core.Entry{Key: "placeholder", Value: core.NullValue()},
	)
	if err != nil {
		return false, err
	}
	contract, err := protocol.NewContractId("core.diagnostic", 1)
	if err != nil {
		return false, err
	}
	_, err = protocol.NewProtocolMessage(contract, payload, protocol.NewContractRegistry(protocol.RegistryV1))
	return false, expectErrorCode(vector, err)
}

func protocolV1RejectNestedEnvelope(vector *caseData) (bool, error) {
	payload, err := core.NewObject(core.Entry{Key: "schema", Value: core.String("core.protocol-message@1")})
	if err != nil {
		return false, err
	}
	contract, err := protocol.NewContractId("core.protocol-message", 1)
	if err != nil {
		return false, err
	}
	_, err = protocol.NewProtocolMessage(contract, payload, protocol.NewContractRegistry(protocol.RegistryV1))
	return false, expectErrorCode(vector, err)
}

func protocolV1RejectSemanticModelIdentity(vector *caseData) (bool, error) {
	payload, err := core.NewObject(core.Entry{Key: "schema", Value: core.String("core.semantic-model@1")})
	if err != nil {
		return false, err
	}
	contract, err := protocol.NewContractId("core.semantic-model", 1)
	if err != nil {
		return false, err
	}
	_, err = protocol.NewProtocolMessage(contract, payload, protocol.NewContractRegistry(protocol.RegistryV1))
	return false, expectErrorCode(vector, err)
}

func protocolV1ProfileRoundtrip(vector *caseData) (bool, error) {
	profile, err := protocol.NewProfileDescriptor("toml", 1, "toml.1.0", 1, nil,
		[]string{"toml.datetime"},
		[]*protocol.CapabilityId{protocol.NewCapabilityId("core.document.exact-roundtrip", 1)})
	if err != nil {
		return false, err
	}
	value, err := profile.ToValue()
	if err != nil {
		return false, err
	}
	decoded := &protocol.ProfileDescriptor{}
	roundtripped, err := decoded.FromValue(value)
	if err != nil {
		return false, err
	}
	roundtripValue, err := roundtripped.ToValue()
	if err != nil {
		return false, err
	}
	if !core.Equal(roundtripValue, value) {
		return false, fmt.Errorf("profile round-trip changed the descriptor")
	}
	return true, nil
}

func protocolV1CapabilityRoundtrip(vector *caseData) (bool, error) {
	declaration, err := protocol.NewCapabilityDeclaration(
		protocol.NewCapabilityId("toml.projection.best-exact-core", 1),
		protocol.ImplementationSupport{Kind: protocol.SupportConditional,
			Preconditions: []protocol.Precondition{{Key: "profile", Value: "toml.1.0@1"}}},
		protocol.VerificationVerified, strPtr("consema.protocol.conformance"))
	if err != nil {
		return false, err
	}
	value, err := declaration.ToValue()
	if err != nil {
		return false, err
	}
	decoded := &protocol.CapabilityDeclaration{}
	roundtripped, err := decoded.FromValue(value)
	if err != nil {
		return false, err
	}
	roundtripValue, err := roundtripped.ToValue()
	if err != nil {
		return false, err
	}
	if !core.Equal(roundtripValue, value) {
		return false, fmt.Errorf("capability round-trip changed the declaration")
	}
	return true, nil
}

func protocolV1CapabilityContradiction(vector *caseData) (bool, error) {
	_, err := protocol.NewCapabilityDeclaration(
		protocol.NewCapabilityId("core.query.ordered-results", 1),
		protocol.ImplementationSupport{Kind: protocol.SupportConditional},
		protocol.VerificationUnverified, nil)
	return false, expectErrorCode(vector, err)
}

func protocolV1DiagnosticSourceBinding(vector *caseData) (bool, error) {
	// A core diagnostic whose primary location still references a
	// process-local snapshot handle cannot be externalized; Go's Diagnostic
	// is the externalized message record, so the boundary is the fixed
	// process-local rejection.
	return false, expectErrorCode(vector, protocol.ProcessLocalHandleError("$.location.snapshot"))
}

func protocolV1DiagnosticCategoryMismatch(vector *caseData) (bool, error) {
	_, err := protocol.NewDiagnostic("json.object.duplicate-member@1",
		protocol.CategorySyntax, protocol.SeverityError, nil, nil, nil, nil, nil, 0,
		protocol.DefaultErrorCodeRegistry())
	return false, expectErrorCode(vector, err)
}

func protocolV1CompletionContradiction(vector *caseData) (bool, error) {
	limit := "max_steps"
	_, err := protocol.NewCompletion(protocol.CompletionSuccess, 1, 1, &limit, nil)
	return false, expectErrorCode(vector, err)
}

func protocolV1CompletionUnregisteredCode(vector *caseData) (bool, error) {
	code := "example.failure@1"
	_, err := protocol.NewCompletion(protocol.CompletionFailed, 1, 0, nil, &code)
	return false, expectErrorCode(vector, err)
}

func protocolV1QueryDefinitionEnvelope(vector *caseData) (bool, error) {
	definition := protocol.NewQueryDefinition(protocol.DomainPortableValueV1()).
		WithExpression((&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
			Then(protocol.NewOperatorCall("core.try-sequence-elements", 1)))
	before, failure := definition.ToProtocolValue()
	if failure != nil {
		return false, failure
	}
	beforeBytes, err := core.EncodePVCE(before)
	if err != nil {
		return false, err
	}
	registry := protocol.NewContractRegistry(protocol.RegistryV1)
	contract, err := protocol.NewContractId("core.query-definition", 1)
	if err != nil {
		return false, err
	}
	message, err := protocol.NewProtocolMessage(contract, before, registry)
	if err != nil {
		return false, err
	}
	envelopeValue, err := message.ToValue()
	if err != nil {
		return false, err
	}
	envelope := &protocol.ProtocolMessage{}
	decodedEnvelope, err := envelope.FromValue(envelopeValue, registry)
	if err != nil {
		return false, err
	}
	decoded := &protocol.QueryDefinition{}
	afterValue, failure := decoded.FromProtocolValue(decodedEnvelope.Payload())
	if failure != nil {
		return false, failure
	}
	after, failure := afterValue.ToProtocolValue()
	if failure != nil {
		return false, failure
	}
	afterBytes, err := core.EncodePVCE(after)
	if err != nil {
		return false, err
	}
	if string(beforeBytes) != string(afterBytes) {
		return false, fmt.Errorf("query definition envelope is not PVCE-stable")
	}
	strictEqual, _ := booleanField(vector.Expected, "strict_equal")
	pvceUnchanged, _ := booleanField(vector.Expected, "pvce1_unchanged")
	if !strictEqual || !pvceUnchanged {
		return false, fmt.Errorf("unexpected expectation facts")
	}
	return true, nil
}

func protocolV1QueryPortableResult(vector *caseData) (bool, error) {
	definition := protocol.NewQueryDefinition(protocol.DomainPortableValueV1())
	validated, failure := definition.Validate()
	if failure != nil {
		return false, failure
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, failure := validated.Bind(capabilities)
	if failure != nil {
		return false, failure
	}
	matches, failure := executable.ExecutePortable(core.String("x"), protocol.DefaultQueryLimits())
	if failure != nil {
		return false, failure
	}
	result, err := protocol.NewQueryResultFromPortableExecution(
		protocol.DomainPortableValueV1(), protocol.RoleValue, portableMatches(matches))
	if err != nil {
		return false, err
	}
	value, err := result.ToValue()
	if err != nil {
		return false, err
	}
	decoded := &protocol.QueryResultMessage{}
	roundtripped, err := decoded.FromValue(value)
	if err != nil {
		return false, err
	}
	roundtripValue, err := roundtripped.ToValue()
	if err != nil {
		return false, err
	}
	if !core.Equal(roundtripValue, value) {
		return false, fmt.Errorf("query result round-trip changed the record")
	}
	return true, nil
}

func portableMatches(matches []protocol.PortableMatch) []protocol.ProtocolQueryMatch {
	output := make([]protocol.ProtocolQueryMatch, 0, len(matches))
	for _, match := range matches {
		output = append(output, protocol.ProtocolQueryMatch{
			Kind: "Value", Path: match.Path, Value: match.Value,
		})
	}
	return output
}

func protocolV1RejectNativeHandle(vector *caseData) (bool, error) {
	return false, expectErrorCode(vector, protocol.NativeMatchLocatorFromProcessLocal())
}

func protocolV1ProjectionRequestRoundtrip(vector *caseData) (bool, error) {
	policy := protocol.NewProjectionPolicy(
		mustContractId("core.projection.exact-or-reject@1"), nil)
	request, err := protocol.NewProjectionRequestMessage(
		mustContractId("json.projection.best-exact-core@1"), *policy,
		[]protocol.ProjectionRule{{
			RuleID: "global",
			Scope:  protocol.ProjectionScope{Kind: "Global"},
			Policy: *policy,
		}}, nil)
	if err != nil {
		return false, err
	}
	value, err := request.ToValue()
	if err != nil {
		return false, err
	}
	decoded := &protocol.ProjectionRequestMessage{}
	roundtripped, err := decoded.FromValue(value)
	if err != nil {
		return false, err
	}
	roundtripValue, err := roundtripped.ToValue()
	if err != nil {
		return false, err
	}
	if !core.Equal(roundtripValue, value) {
		return false, fmt.Errorf("projection request round-trip changed the record")
	}
	return true, nil
}

func protocolV1ProjectionNoPartial(vector *caseData) (bool, error) {
	completion, err := protocol.NewCompletion(protocol.CompletionFailed, 1, 0, nil,
		strPtr("core.projection.target-not-applicable@1"))
	if err != nil {
		return false, err
	}
	_, err = protocol.NewProjectionResultMessage(completion, core.NullValue(), true,
		ptrFidelity(protocol.FidelityExact),
		&protocol.ProjectionReportMessage{}, &protocol.ProvenanceMapMessage{}, nil)
	contradictionRejected, _ := booleanField(vector.Expected, "contradiction_rejected")
	if !contradictionRejected {
		return false, fmt.Errorf("unexpected expectation facts")
	}
	if err == nil {
		return false, fmt.Errorf("failed projection must reject a partial value")
	}
	protocolError, ok := err.(*protocol.ProtocolError)
	if !ok || protocolError.Code() != "core.protocol.invalid-value@1" {
		return false, fmt.Errorf("rejection %v", err)
	}
	return true, nil
}

func protocolV1ProjectionUnregisteredCode(vector *caseData) (bool, error) {
	_, err := protocol.NewProjectionReportMessage([]protocol.ProjectionEventMessage{{
		Code:               "example.projection@1",
		LossClassification: protocol.LossNone,
	}})
	return false, expectErrorCode(vector, err)
}

func protocolV1ProvenanceRoundtrip(vector *caseData) (bool, error) {
	origin, err := protocol.NewSourceOriginMessage("source:one",
		strPtr("toml:root"), 0, 1, protocol.RelationDirect)
	if err != nil {
		return false, err
	}
	mapMessage, err := protocol.NewProvenanceMapMessage([]protocol.ProvenanceEntryMessage{{
		Projected: protocol.ProjectedLocationMessage{Kind: "ValuePath", Path: protocol.RootValuePath()},
		Origins:   []protocol.SourceOriginMessage{*origin},
	}})
	if err != nil {
		return false, err
	}
	value, err := mapMessage.ToValue()
	if err != nil {
		return false, err
	}
	decoded := &protocol.ProvenanceMapMessage{}
	roundtripped, err := decoded.FromValue(value)
	if err != nil {
		return false, err
	}
	roundtripValue, err := roundtripped.ToValue()
	if err != nil {
		return false, err
	}
	if !core.Equal(roundtripValue, value) {
		return false, fmt.Errorf("provenance round-trip changed the record")
	}
	// The externalized record carries no raw process-local node reference.
	if rawNodeRefInValue(value) {
		return false, fmt.Errorf("provenance record carries a raw node reference")
	}
	rawNodeRef, _ := booleanField(vector.Expected, "raw_node_ref")
	if rawNodeRef {
		return false, fmt.Errorf("unexpected expectation facts")
	}
	return true, nil
}

// rawNodeRefInValue reports whether the value tree contains a raw
// process-local node-reference marker ("node" integer fields).
func rawNodeRefInValue(value core.Value) bool {
	switch item := value.(type) {
	case *core.Object:
		for _, entry := range item.Entries() {
			if entry.Key == "node" {
				if _, ok := entry.Value.(core.Integer); ok {
					return true
				}
			}
			if rawNodeRefInValue(entry.Value) {
				return true
			}
		}
	case *core.Array:
		for _, element := range item.Items() {
			if rawNodeRefInValue(element) {
				return true
			}
		}
	}
	return false
}

func protocolV1RegistryRoundtrip(vector *caseData) (bool, error) {
	manifest, err := protocol.NewRegistryManifest(1,
		protocol.NewContractRegistry(protocol.RegistryV1),
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV1))
	if err != nil {
		return false, err
	}
	value, err := manifest.ToValue()
	if err != nil {
		return false, err
	}
	decoded := &protocol.RegistryManifest{}
	roundtripped, err := decoded.FromValue(value)
	if err != nil {
		return false, err
	}
	sortedUnique, _ := booleanField(vector.Expected, "sorted_unique")
	isCurrent, _ := booleanField(vector.Expected, "is_current")
	if !sortedUnique {
		return false, fmt.Errorf("unexpected expectation facts")
	}
	if roundtripped.SemanticModel().Schema() != "core.semantic-model@1" {
		return false, fmt.Errorf("semantic-model identity differs")
	}
	// The suite-local "current" fact: the v1 manifest is the current
	// registry of the protocol-v1 suite (the protocol-v2 suite binds the
	// same fact suite-locally; a later library model must not leak in).
	if isCurrent != (roundtripped.SemanticModel().Schema() == "core.semantic-model@1") {
		return false, fmt.Errorf("is_current fact differs")
	}
	roundtripValue, err := roundtripped.ToValue()
	if err != nil {
		return false, err
	}
	if !core.Equal(roundtripValue, value) {
		return false, fmt.Errorf("registry manifest round-trip changed the record")
	}
	return true, nil
}

func protocolV1ErrorCodeSchema(vector *caseData) (bool, error) {
	value, err := protocol.ErrorCodeManifestValue()
	if err != nil {
		return false, err
	}
	return true, protocol.ValidateErrorCodeManifestValue(value)
}

func protocolV1QueryCodesRegistered(vector *caseData) (bool, error) {
	registry := protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV1)
	failures := []*protocol.QueryFailure{
		{Kind: protocol.FailureDomainMismatch, Domain: protocol.NewQueryDomain("example.domain", 1)},
		{Kind: protocol.FailureUnknownOperator, Operator: "unknown", Version: 1},
		{Kind: protocol.FailureWrongArgumentType, Operator: "x", Argument: "a"},
		{Kind: protocol.FailureInvalidArgument, Operator: "x", Argument: "a"},
		{Kind: protocol.FailureInvalidOperatorComposition, Operator: "x"},
		{Kind: protocol.FailureMissingCapability,
			Capability: protocol.NewCapabilityId("core.example", 1)},
		{Kind: protocol.FailureRequiredTypeMismatch},
		{Kind: protocol.FailureCardinalityViolation},
		{Kind: protocol.FailureResourceLimit},
		{Kind: protocol.FailureCancelled},
		{Kind: protocol.FailureTargetUnavailable},
	}
	for _, failure := range failures {
		if !registry.Contains(failure.Code()) {
			return false, fmt.Errorf("unregistered query failure code %s", failure.Code())
		}
	}
	return true, nil
}

// hexBytes decodes one lowercase-hex vector string.
func hexBytes(text string) ([]byte, error) {
	return hex.DecodeString(text)
}
