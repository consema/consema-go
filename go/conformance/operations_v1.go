package conformance

// The `consema.operations.conformance@1` suite runner. The JSON-face cases
// (16) execute since 0.15.0 G1.2 (go/json), the TOML-face cases (13) since
// 0.15.0 G1.3 (go/toml), the convert cases belong to the root package
// (G1.4), and the registry/protocol data cases (semantic-model v3) flip
// with 0.18.0 G5.3. The whole 35-case suite executes; no documented skips
// remain (RFC 0016 §7: documented skip = success, never silent).

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"consema.dev/consema/core"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/protocol"
)

func runOperationsV1(_ *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		switch vector.ID {
		case "operations.v1.registry-v3":
			runOperationsRegistryV3Face(vector, report)
		case "operations.v1.protocol-v3-dual-transport":
			runOperationsProtocolV3Face(vector, report)
		case "operations.v1.materialize-json-compact",
			"operations.v1.materialize-json-pretty-crlf",
			"operations.v1.materialize-json-entry-mapping-duplicates",
			"operations.v1.materialize-json-nonstring-key-rejected",
			"operations.v1.materialize-json-float-rejected",
			"operations.v1.materialize-json-output-limit",
			"operations.v1.materialization-depth-limit",
			"operations.v1.json-object-insert",
			"operations.v1.json-object-remove-duplicate",
			"operations.v1.json-array-remove",
			"operations.v1.json-conflict-atomic",
			"operations.v1.json-dry-run-proof-patch",
			"operations.v1.json-structural-matrix",
			"operations.v1.json-conflict-matrix",
			"operations.v1.materialization-security-matrix",
			"operations.v1.untouched-proof-tamper":
			RunOperationsJSONFace(vector, report)
		case "operations.v1.operation-registry",
			"operations.v1.materialize-toml-native",
			"operations.v1.materialize-toml-explicit-mapping",
			"operations.v1.materialize-toml-implicit-mapping-rejected",
			"operations.v1.materialize-toml-null-rejected",
			"operations.v1.materialize-toml-output-limit",
			"operations.v1.toml-root-insert",
			"operations.v1.toml-inline-rename",
			"operations.v1.toml-array-remove",
			"operations.v1.toml-conflict-atomic",
			"operations.v1.toml-dry-run-proof-patch",
			"operations.v1.toml-structural-matrix",
			"operations.v1.toml-conflict-matrix":
			RunOperationsTOMLFace(vector, report)
		case "operations.v1.convert-json-to-toml-exact",
			"operations.v1.convert-toml-to-json-exact",
			"operations.v1.convert-duplicate-json-to-toml-fails",
			"operations.v1.convert-transformed-report":
			RunOperationsConvertFace(vector, report)
		default:
			report.Failed = append(report.Failed, CaseFailure{
				ID:      vector.ID,
				Message: "runner does not recognize published operations v1 case",
			})
		}
	}
	return report
}

// runOperationsRegistryV3Face executes `operations.v1.registry-v3`
// (operations_v1.rs registry_v3): the v1/v2/v3 registry manifests carry
// their pinned contract and error-code counts, the v3 semantic-model
// version is 3, and the v3 manifest round-trips through its record codec
// unchanged.
func runOperationsRegistryV3Face(vector *caseData, report *SuiteReport) {
	fail := func(message string) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
	}
	v1, err := protocol.NewRegistryManifest(1,
		protocol.NewContractRegistry(protocol.RegistryV1),
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV1))
	if err != nil {
		fail(fmt.Sprintf("v1 manifest: %v", err))
		return
	}
	v2, err := protocol.NewRegistryManifest(2,
		protocol.NewContractRegistry(protocol.RegistryV2),
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV2))
	if err != nil {
		fail(fmt.Sprintf("v2 manifest: %v", err))
		return
	}
	v3, err := protocol.NewRegistryManifest(3,
		protocol.NewContractRegistry(protocol.RegistryV3),
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV3))
	if err != nil {
		fail(fmt.Sprintf("v3 manifest: %v", err))
		return
	}
	contractCount, ok := integerField(vector.Expected, "contract_count")
	if !ok {
		fail("missing expected.contract_count")
		return
	}
	errorCodeCount, ok := integerField(vector.Expected, "error_code_count")
	if !ok {
		fail("missing expected.error_code_count")
		return
	}
	v1ContractCount, ok := integerField(vector.Expected, "v1_contract_count")
	if !ok {
		fail("missing expected.v1_contract_count")
		return
	}
	v1ErrorCodeCount, ok := integerField(vector.Expected, "v1_error_code_count")
	if !ok {
		fail("missing expected.v1_error_code_count")
		return
	}
	v2ContractCount, ok := integerField(vector.Expected, "v2_contract_count")
	if !ok {
		fail("missing expected.v2_contract_count")
		return
	}
	v2ErrorCodeCount, ok := integerField(vector.Expected, "v2_error_code_count")
	if !ok {
		fail("missing expected.v2_error_code_count")
		return
	}
	value, err := v3.ToValue()
	if err != nil {
		fail(fmt.Sprintf("v3 manifest encode: %v", err))
		return
	}
	roundtripped, err := (&protocol.RegistryManifest{}).FromValue(value)
	if err != nil {
		fail(fmt.Sprintf("v3 manifest round-trip: %v", err))
		return
	}
	if v3.SemanticModel().Version() != 3 ||
		uint64(len(v3.Contracts())) != contractCount ||
		uint64(len(v3.ErrorCodes())) != errorCodeCount ||
		uint64(len(v1.Contracts())) != v1ContractCount ||
		uint64(len(v1.ErrorCodes())) != v1ErrorCodeCount ||
		uint64(len(v2.Contracts())) != v2ContractCount ||
		uint64(len(v2.ErrorCodes())) != v2ErrorCodeCount ||
		!registryManifestsEqual(v3, roundtripped) {
		fail("registry manifest facts did not match")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// registryManifestsEqual compares the decoded manifest against the
// constructed one field by field (the Rust `==` on RegistryManifest).
func registryManifestsEqual(a, b *protocol.RegistryManifest) bool {
	if !a.SemanticModel().Equal(b.SemanticModel()) {
		return false
	}
	aContracts, bContracts := a.Contracts(), b.Contracts()
	if len(aContracts) != len(bContracts) {
		return false
	}
	for index := range aContracts {
		if !aContracts[index].Contract.Equal(bContracts[index].Contract) ||
			aContracts[index].Stability != bContracts[index].Stability {
			return false
		}
	}
	aCodes, bCodes := a.ErrorCodes(), b.ErrorCodes()
	if len(aCodes) != len(bCodes) {
		return false
	}
	for index := range aCodes {
		if aCodes[index] != bCodes[index] {
			return false
		}
	}
	return true
}

// runOperationsProtocolV3Face executes
// `operations.v1.protocol-v3-dual-transport` (operations_v1.rs
// protocol_v3): the seven v3-registry payloads close over the JSON and
// PVCE/1 transports under the semantic-model v3 contract registry.
func runOperationsProtocolV3Face(vector *caseData, report *SuiteReport) {
	fail := func(message string) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
	}
	newPayloadCount, ok := integerField(vector.Expected, "new_payload_count")
	if !ok {
		fail("missing expected.new_payload_count")
		return
	}
	jsonEqual, ok := booleanField(vector.Expected, "json_equal")
	if !ok {
		fail("missing expected.json_equal")
		return
	}
	pvceEqual, ok := booleanField(vector.Expected, "pvce_equal")
	if !ok {
		fail("missing expected.pvce_equal")
		return
	}
	payloads := []struct {
		id      string
		payload core.Value
	}{
		{"core.conversion-report", conversionReportPayload()},
		{"core.edit-plan", editPlanPayload()},
		{"core.format-operation-registry", formatOperationRegistryPayload()},
		{"core.materialization-provenance-map", materializationProvenanceMapPayload()},
		{"core.materialization-report", materializationReportPayload()},
		{"core.materialization-result", materializationResultPayload()},
	}
	requestPayload, err := materializationRequestPayload()
	if err != nil {
		fail(fmt.Sprintf("materialization request: %v", err))
		return
	}
	payloads = append(payloads, struct {
		id      string
		payload core.Value
	}{"core.materialization-request", requestPayload})
	registry := protocol.NewContractRegistry(protocol.RegistryV3)
	limits := protocol.DefaultProtocolLimits()
	jsonClosed, pvceClosed := true, true
	for _, payload := range payloads {
		contract, err := protocol.NewContractId(payload.id, 1)
		if err != nil {
			fail(fmt.Sprintf("%s contract: %v", payload.id, err))
			return
		}
		message, err := protocol.NewProtocolMessage(contract, payload.payload, registry)
		if err != nil {
			fail(fmt.Sprintf("%s envelope: %v", payload.id, err))
			return
		}
		jsonBytes, err := message.ToJSON(limits)
		if err != nil {
			fail(fmt.Sprintf("%s JSON encode: %v", payload.id, err))
			return
		}
		decodedJSON, err := message.FromJSON(jsonBytes, limits, registry)
		if err != nil {
			fail(fmt.Sprintf("%s JSON decode: %v", payload.id, err))
			return
		}
		jsonClosed = jsonClosed && core.Equal(decodedJSON.Payload(), message.Payload())
		pvceBytes, err := message.ToPVCE(limits)
		if err != nil {
			fail(fmt.Sprintf("%s PVCE encode: %v", payload.id, err))
			return
		}
		decodedPVCE, err := message.FromPVCE(pvceBytes, limits, registry)
		if err != nil {
			fail(fmt.Sprintf("%s PVCE decode: %v", payload.id, err))
			return
		}
		pvceClosed = pvceClosed && core.Equal(decodedPVCE.Payload(), message.Payload())
	}
	if uint64(len(payloads)) != newPayloadCount || jsonClosed != jsonEqual || pvceClosed != pvceEqual {
		fail("protocol dual transport did not close")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// profileReferenceValue builds the canonical {id, version} reference of
// one profile (operations_v1.rs profile_value).
func profileReferenceValue(id string, version uint32) core.Value {
	value, _ := core.NewObject(
		core.Entry{Key: "id", Value: core.String(id)},
		core.Entry{Key: "version", Value: core.NewInteger(big.NewInt(int64(version)))},
	)
	return value
}

// digestOf builds the {algorithm, hex} record of the sha256 digest of one
// byte slice (operations_v1.rs ContentDigest::of).
func digestOf(bytes []byte) core.Value {
	sum := sha256.Sum256(bytes)
	value, _ := core.NewObject(
		core.Entry{Key: "algorithm", Value: core.String("sha256")},
		core.Entry{Key: "hex", Value: core.String(hex.EncodeToString(sum[:]))},
	)
	return value
}

// emptyReportValue builds one empty `core.materialization-report@1` record
// (materialization.rs to_value of the default report).
func emptyReportValue() core.Value {
	value, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.materialization-report@1")},
		core.Entry{Key: "events", Value: core.NewArray()},
	)
	return value
}

// conversionReportPayload builds the exact two-stage conversion report
// (conversion.rs): Exact projection over an empty projection
// report, Exact materialization over an empty materialization report.
func conversionReportPayload() core.Value {
	projectionReport, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.projection-report@1")},
		core.Entry{Key: "events", Value: core.NewArray()},
	)
	value, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.conversion-report@1")},
		core.Entry{Key: "source_profile", Value: profileReferenceValue("toml.1.0", 1)},
		core.Entry{Key: "target_profile", Value: profileReferenceValue("json.strict", 1)},
		core.Entry{Key: "projection_fidelity", Value: core.String("Exact")},
		core.Entry{Key: "projection_report", Value: projectionReport},
		core.Entry{Key: "materialization_fidelity", Value: core.String("Exact")},
		core.Entry{Key: "materialization_report", Value: emptyReportValue()},
		core.Entry{Key: "overall_fidelity", Value: core.String("Exact")},
	)
	return value
}

// editPlanPayload builds the exact dry-run plan (operation.rs):
// an empty operation/replacement/report plan whose empty replacement set
// cannot change the content digest.
func editPlanPayload() core.Value {
	digest := digestOf([]byte("unchanged"))
	value, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.edit-plan@1")},
		core.Entry{Key: "source_id", Value: core.String("source:one")},
		core.Entry{Key: "base_digest", Value: digest},
		core.Entry{Key: "profile", Value: profileReferenceValue("json.strict", 1)},
		core.Entry{Key: "operations", Value: core.NewArray()},
		core.Entry{Key: "replacements", Value: core.NewArray()},
		core.Entry{Key: "target_digest", Value: digest},
		core.Entry{Key: "report", Value: core.NewArray()},
	)
	return value
}

// formatOperationRegistryPayload builds the JSON-family format operation
// registry record (operation.rs) from the same frozen descriptor
// surface the shared operation-registry face checks.
func formatOperationRegistryPayload() core.Value {
	registry := jsonpkg.FormatOperationRegistryFor(jsonpkg.JsonProfileStrictV1)
	profile := registry.Profile()
	operations := make([]core.Value, 0, len(registry.Operations()))
	for _, descriptor := range registry.Operations() {
		operationID, operationVersion := splitVersionedID(descriptor.ID())
		targetRoleID, targetRoleVersion := splitVersionedID(descriptor.TargetRole())
		arguments := make([]core.Value, 0, len(descriptor.Arguments()))
		for _, argument := range descriptor.Arguments() {
			record, _ := core.NewObject(
				core.Entry{Key: "name", Value: core.String(argument.Name)},
				core.Entry{Key: "kind", Value: core.String(argument.Kind)},
				core.Entry{Key: "required", Value: core.Boolean(argument.Required)},
			)
			arguments = append(arguments, record)
		}
		record, _ := core.NewObject(
			core.Entry{Key: "operation", Value: profileReferenceValue(operationID, operationVersion)},
			core.Entry{Key: "target_role", Value: profileReferenceValue(targetRoleID, targetRoleVersion)},
			core.Entry{Key: "arguments", Value: core.NewArray(arguments...)},
			core.Entry{Key: "support", Value: core.String(descriptor.Support())},
		)
		operations = append(operations, record)
	}
	value, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.format-operation-registry@1")},
		core.Entry{Key: "profile", Value: profileReferenceValue(profile.ID(), profile.Version())},
		core.Entry{Key: "operations", Value: core.NewArray(operations...)},
	)
	return value
}

// splitVersionedID splits one "namespace@version" spelling at its last
// "@" into the id and version parts (the ids never contain "@").
func splitVersionedID(text string) (string, uint32) {
	index := strings.LastIndex(text, "@")
	if index < 0 {
		return text, 1
	}
	var version uint64
	_, err := fmt.Sscanf(text[index+1:], "%d", &version)
	if err != nil {
		return text, 1
	}
	return text[:index], uint32(version)
}

// materializationProvenanceMapPayload builds the empty default
// provenance map (materialization.rs).
func materializationProvenanceMapPayload() core.Value {
	value, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.materialization-provenance-map@1")},
		core.Entry{Key: "entries", Value: core.NewArray()},
	)
	return value
}

// materializationReportPayload builds the empty default materialization
// report (materialization.rs).
func materializationReportPayload() core.Value {
	return emptyReportValue()
}

// materializationRequestPayload builds the v1 request for the
// json.canonical-compact style with no newline policy
// (operations_v1.rs json_request).
func materializationRequestPayload() (core.Value, error) {
	reference, err := protocol.NewProfileReference("json.strict", 1)
	if err != nil {
		return nil, err
	}
	request, err := protocol.NewMaterializationRequest(*reference, "json.canonical-compact", 1)
	if err != nil {
		return nil, err
	}
	request = request.WithNewline("None")
	return protocol.NewMaterializationRequestMessageV1FromRequest(request).ToValue()
}

// materializationResultPayload builds the explicitly failed result
// (materialization.rs and 719-733): UnsupportedStyle against an
// empty report and no analyzed input paths.
func materializationResultPayload() core.Value {
	failure, _ := core.NewObject(
		core.Entry{Key: "kind", Value: core.String("UnsupportedStyle")},
		core.Entry{Key: "code", Value: core.String("core.materialization.unsupported-style@1")},
	)
	outcome, _ := core.NewObject(
		core.Entry{Key: "kind", Value: core.String("Failed")},
		core.Entry{Key: "failure", Value: failure},
		core.Entry{Key: "report", Value: emptyReportValue()},
		core.Entry{Key: "analyzed_input_paths", Value: core.NewArray()},
	)
	value, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.materialization-result@1")},
		core.Entry{Key: "target_profile", Value: profileReferenceValue("json.strict", 1)},
		core.Entry{Key: "outcome", Value: outcome},
	)
	return value
}
