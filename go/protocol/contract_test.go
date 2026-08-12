package protocol

import (
	"testing"

	"consema.dev/consema/core"
)

// The frozen per-version contract counts (contract.rs:696-702).
var contractVersionCounts = map[ContractRegistryVersion]int{
	RegistryV1: 16,
	RegistryV2: 18,
	RegistryV3: 25,
	RegistryV4: 25,
	RegistryV5: 30,
	RegistryV6: 38,
	RegistryV7: 41,
}

func allContractVersions() []ContractRegistryVersion {
	return []ContractRegistryVersion{RegistryV1, RegistryV2, RegistryV3, RegistryV4, RegistryV5, RegistryV6, RegistryV7}
}

func TestContractRegistryCountsAndSortedness(t *testing.T) {
	for _, version := range allContractVersions() {
		registry := NewContractRegistry(version)
		contracts := registry.Contracts()
		if len(contracts) != contractVersionCounts[version] {
			t.Errorf("v%d contract count = %d, want %d", version+1, len(contracts), contractVersionCounts[version])
		}
		for index := 1; index < len(contracts); index++ {
			previous := contracts[index-1]
			current := contracts[index]
			if previous.ID > current.ID || (previous.ID == current.ID && previous.Version >= current.Version) {
				t.Errorf("v%d contracts not strictly sorted at %d: %s@%d then %s@%d",
					version+1, index, previous.ID, previous.Version, current.ID, current.Version)
			}
		}
	}
}

func TestContractRegistrySupersetsAndBoundaries(t *testing.T) {
	// v5 ⊂ v6 ⊂ v7 (contract.rs:703-716).
	for _, descriptor := range NewContractRegistry(RegistryV5).Contracts() {
		contract, err := NewContractId(descriptor.ID, descriptor.Version)
		if err != nil {
			t.Fatal(err)
		}
		if !NewContractRegistry(RegistryV6).Recognizes(contract) {
			t.Errorf("v6 lost %s@%d", descriptor.ID, descriptor.Version)
		}
	}
	for _, descriptor := range NewContractRegistry(RegistryV6).Contracts() {
		contract, err := NewContractId(descriptor.ID, descriptor.Version)
		if err != nil {
			t.Fatal(err)
		}
		if !NewContractRegistry(RegistryV7).Recognizes(contract) {
			t.Errorf("v7 lost %s@%d", descriptor.ID, descriptor.Version)
		}
	}
	// v3 == v4.
	if len(NewContractRegistry(RegistryV4).Contracts()) != len(NewContractRegistry(RegistryV3).Contracts()) {
		t.Error("v4 contract count differs from v3")
	}
	// The v7 CLI records are not in v6.
	for _, id := range []string{"core.cli-output", "core.batch-plan", "core.batch-result"} {
		contract, err := NewContractId(id, 1)
		if err != nil {
			t.Fatal(err)
		}
		if NewContractRegistry(RegistryV6).Recognizes(contract) {
			t.Errorf("v6 recognizes %s", id)
		}
		if !NewContractRegistry(RegistryV7).Recognizes(contract) {
			t.Errorf("v7 lost %s", id)
		}
	}
	// Version boundaries.
	snapshot, _ := NewContractId("core.source-snapshot", 1)
	if NewContractRegistry(RegistryV1).Recognizes(snapshot) {
		t.Error("v1 recognizes core.source-snapshot@1")
	}
	if !NewContractRegistry(RegistryV2).Recognizes(snapshot) {
		t.Error("v2 lost core.source-snapshot@1")
	}
	graph, _ := NewContractId("core.portable-graph", 1)
	if NewContractRegistry(RegistryV4).Recognizes(graph) {
		t.Error("v4 recognizes core.portable-graph@1")
	}
	if !NewContractRegistry(RegistryV5).Recognizes(graph) {
		t.Error("v5 lost core.portable-graph@1")
	}
	// The transport envelope is registered but never a nested payload.
	envelope, _ := NewContractId("core.protocol-message", 1)
	if !NewContractRegistry(RegistryV1).Recognizes(envelope) {
		t.Error("v1 lost core.protocol-message@1")
	}
}

func TestContractIdentifierStrictness(t *testing.T) {
	if _, err := NewContractId("Core.Bad", 1); err == nil {
		t.Error("uppercase identifier accepted")
	}
	if _, err := NewContractId("core.bad", 0); err == nil {
		t.Error("zero version accepted")
	}
	if _, err := NewContractId("core", 1); err == nil {
		t.Error("single-segment identifier accepted")
	}
	if _, err := NewContractId("core.bad-1.2nd", 1); err == nil {
		t.Error("digit-leading segment accepted")
	}
	if _, err := NewContractId("coreok1", 1); err == nil {
		t.Error("missing dot accepted")
	}
	if _, err := NewContractId("core.good-name1", 2); err != nil {
		t.Errorf("valid identifier rejected: %v", err)
	}
	// The profile/capability namespace allows digit-leading non-first
	// segments (registry.rs:475-498).
	reference, err := NewProfileReference("toml.1-0", 1)
	if err != nil {
		t.Errorf("valid profile reference rejected: %v", err)
	}
	if reference.ID() != "toml.1-0" || reference.Version() != 1 {
		t.Error("profile reference fields wrong")
	}
	if _, err := NewProfileReference("toml", 1); err == nil {
		t.Error("single-segment profile accepted")
	}
	if _, err := NewProfileReference("toml.1-0", 0); err == nil {
		t.Error("zero profile version accepted")
	}
}

func completionPayload() (core.Value, error) {
	completion, err := NewCompletion(CompletionSuccess, 1, 1, nil, nil)
	if err != nil {
		return nil, err
	}
	return completion.ToValue()
}

func TestProtocolMessageEnvelopeRoundTripsBothTransports(t *testing.T) {
	registry := NewContractRegistry(RegistryV1)
	payload, err := completionPayload()
	if err != nil {
		t.Fatal(err)
	}
	contract, err := NewContractId("core.completion", 1)
	if err != nil {
		t.Fatal(err)
	}
	message, err := NewProtocolMessage(contract, payload, registry)
	if err != nil {
		t.Fatal(err)
	}
	limits := DefaultProtocolLimits()
	jsonBytes, err := message.ToJSON(limits)
	if err != nil {
		t.Fatal(err)
	}
	decodedJSON, err := message.FromJSON(jsonBytes, limits, registry)
	if err != nil {
		t.Fatal(err)
	}
	if decodedJSON.Contract().Schema() != "core.completion@1" {
		t.Error("JSON round-trip lost the contract")
	}
	pvceBytes, err := message.ToPVCE(limits)
	if err != nil {
		t.Fatal(err)
	}
	decodedPVCE, err := message.FromPVCE(pvceBytes, limits, registry)
	if err != nil {
		t.Fatal(err)
	}
	if decodedPVCE.Contract().Schema() != "core.completion@1" {
		t.Error("PVCE round-trip lost the contract")
	}
}

func TestProtocolMessageRejectionRules(t *testing.T) {
	registry := NewContractRegistry(RegistryV1)
	limits := DefaultProtocolLimits()

	// Unknown contract (protocol.envelope.reject-unknown-contract).
	unknown, _ := NewContractId("example.unknown", 1)
	payload, _ := core.NewObject(core.Entry{Key: "schema", Value: core.String("example.unknown@1")})
	_, err := NewProtocolMessage(unknown, payload, registry)
	if err == nil || protocolCode(err) != "core.protocol.unknown-contract@1" {
		t.Errorf("unknown contract: got %v", err)
	}

	// Schema mismatch (protocol.envelope.reject-schema-mismatch).
	diagnostic, _ := NewContractId("core.diagnostic", 1)
	wrongPayload, _ := core.NewObject(core.Entry{Key: "schema", Value: core.String("core.completion@1")})
	_, err = NewProtocolMessage(diagnostic, wrongPayload, registry)
	if err == nil || protocolCode(err) != "core.protocol.schema-mismatch@1" {
		t.Errorf("schema mismatch: got %v", err)
	}

	// A matching schema does not bypass full payload validation
	// (protocol.envelope.reject-schema-only-payload; the Rust test
	// contract.rs:650-663): the diagnostic schema is present but an
	// undeclared field is rejected.
	emptyPayload, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.diagnostic@1")},
		core.Entry{Key: "placeholder", Value: core.NullValue()},
	)
	_, err = NewProtocolMessage(diagnostic, emptyPayload, registry)
	if err == nil || protocolCode(err) != "core.protocol.unknown-field@1" {
		t.Errorf("schema-only payload: got %v", err)
	}

	// The transport envelope is not a nested payload contract
	// (protocol.envelope.reject-nested-envelope).
	envelope, _ := NewContractId("core.protocol-message", 1)
	envelopePayload, _ := core.NewObject(core.Entry{Key: "schema", Value: core.String("core.protocol-message@1")})
	_, err = NewProtocolMessage(envelope, envelopePayload, registry)
	if err == nil || protocolCode(err) != "core.protocol.invalid-value@1" {
		t.Errorf("nested envelope: got %v", err)
	}

	// The semantic-model manifest identity is not a registered contract
	// (protocol.envelope.reject-semantic-model-identity).
	semanticModel, _ := NewContractId("core.semantic-model", 7)
	manifestPayload, _ := core.NewObject(core.Entry{Key: "schema", Value: core.String("core.semantic-model@7")})
	_, err = NewProtocolMessage(semanticModel, manifestPayload, NewContractRegistry(RegistryV7))
	if err == nil || protocolCode(err) != "core.protocol.unknown-contract@1" {
		t.Errorf("semantic-model identity: got %v", err)
	}

	// Non-canonical transport JSON is rejected (protocol.json.reject-whitespace).
	nullVector := `{"schema":"core.portable-value-json@1","value":{"type":"Null"}}`
	_, err = DecodeJSON(append([]byte(" "), nullVector...), limits)
	if err == nil || protocolCode(err) != "core.protocol.non-canonical-json@1" {
		t.Errorf("reject-whitespace: got %v", err)
	}
}

func TestContractRegistryErrorCodeRegistryPairing(t *testing.T) {
	contracts := NewContractRegistry(RegistryV7)
	codes := contracts.ErrorCodeRegistry()
	if codes.Version() != ErrorRegistryV7 {
		t.Error("v7 contract registry paired with wrong error registry")
	}
	if len(codes.Codes()) != 187 {
		t.Errorf("paired error registry has %d codes, want 187", len(codes.Codes()))
	}
}
