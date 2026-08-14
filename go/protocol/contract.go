package protocol

import (
	"sort"
	"strings"

	"consema.dev/consema/core"
)

// This file implements the frozen contract registry and the common protocol
// envelope (consema-rs/consema-protocol/src/contract.rs). CONTRACTS_V7 pins the
// semantic-model v7 set of 41 contracts; earlier versions pin their frozen
// subsets (16/18/25/25/30/38).

// ContractStability is the compatibility status of one frozen contract.
type ContractStability uint8

const (
	// StabilityStable is a normative public contract for the current
	// semantic model.
	StabilityStable ContractStability = iota
	// StabilityTransport is a transport-only contract; still immutable
	// within its version.
	StabilityTransport
)

// String returns the canonical stability spelling.
func (s ContractStability) String() string {
	switch s {
	case StabilityStable:
		return "Stable"
	case StabilityTransport:
		return "Transport"
	}
	return "Stable"
}

// ParseContractStability parses one canonical stability spelling.
func ParseContractStability(name string) (ContractStability, error) {
	switch name {
	case "Stable":
		return StabilityStable, nil
	case "Transport":
		return StabilityTransport, nil
	}
	return StabilityStable, invalid("$.stability", "unknown contract stability")
}

// ContractId is a stable versioned protocol contract identifier.
type ContractId struct {
	id      string
	version uint32
}

// NewContractId validates and creates an identifier: the version must be
// non-zero and the id must be a dotted lowercase identifier of at most 255
// bytes whose segments start with a lowercase letter
// (consema-rs/consema-protocol/src/contract.rs).
func NewContractId(id string, version uint32) (*ContractId, error) {
	if version == 0 {
		return nil, invalid("$.contract.version", "version must be non-zero")
	}
	if err := validateIdentifier(id, "$.contract.id"); err != nil {
		return nil, err
	}
	return &ContractId{id: id, version: version}, nil
}

// ID returns the namespaced contract ID without the version suffix.
func (c *ContractId) ID() string { return c.id }

// Version returns the immutable contract version.
func (c *ContractId) Version() uint32 { return c.version }

// Schema returns the canonical `id@version` schema discriminator.
func (c *ContractId) Schema() string {
	return c.id + "@" + uint32String(c.version)
}

// Compare orders contract ids by (id, version).
func (c *ContractId) Compare(other *ContractId) int {
	if c.id != other.id {
		return strings.Compare(c.id, other.id)
	}
	if c.version < other.version {
		return -1
	}
	if c.version > other.version {
		return 1
	}
	return 0
}

// Equal reports whether two contract ids are identical.
func (c *ContractId) Equal(other *ContractId) bool {
	return c.id == other.id && c.version == other.version
}

func uint32String(value uint32) string {
	// strconv-free to keep the dependency surface visible; the standard
	// library is the only import domain, and strconv is part of it.
	var digits [10]byte
	index := len(digits)
	number := uint64(value)
	if number == 0 {
		return "0"
	}
	for number > 0 {
		index--
		digits[index] = byte('0' + number%10)
		number /= 10
	}
	return string(digits[index:])
}

// validateIdentifier enforces the strict dotted identifier rule of the
// contract registry (contract.rs): at most 255 bytes, at least two
// segments, each segment starting with a lowercase letter and continuing
// with lowercase letters, digits, or dashes.
func validateIdentifier(identifier, path string) error {
	if len(identifier) > 255 || !strings.Contains(identifier, ".") {
		return invalid(path, "identifier must contain multiple segments and be at most 255 bytes")
	}
	for _, segment := range strings.Split(identifier, ".") {
		if segment == "" {
			return invalid(path, "identifier contains an invalid segment")
		}
		if !isLower(segment[0]) {
			return invalid(path, "identifier contains an invalid segment")
		}
		for index := 1; index < len(segment); index++ {
			byte := segment[index]
			if !isLower(byte) && (byte < '0' || byte > '9') && byte != '-' {
				return invalid(path, "identifier contains an invalid segment")
			}
		}
	}
	return nil
}

func isLower(byte byte) bool { return byte >= 'a' && byte <= 'z' }

// validateNamespace enforces the profile/capability namespace rule
// (consema-rs/consema-protocol/src/registry.rs): at most 255 bytes, and
// when requireDot is set at least two segments; every segment starts with a
// lowercase letter (or a digit when not the first segment) and continues
// with lowercase letters, digits, or dashes.
func validateNamespace(identifier string, requireDot bool, path string) error {
	if len(identifier) == 0 || len(identifier) > 255 || (requireDot && !strings.Contains(identifier, ".")) {
		return invalid(path, "invalid namespaced identifier")
	}
	for index, segment := range strings.Split(identifier, ".") {
		if segment == "" {
			return invalid(path, "invalid identifier segment")
		}
		first := segment[0]
		if !isLower(first) && !(index != 0 && first >= '0' && first <= '9') {
			return invalid(path, "invalid identifier segment")
		}
		for offset := 1; offset < len(segment); offset++ {
			byte := segment[offset]
			if !isLower(byte) && (byte < '0' || byte > '9') && byte != '-' {
				return invalid(path, "invalid identifier segment")
			}
		}
	}
	return nil
}

// ContractDescriptor is one static registry record.
type ContractDescriptor struct {
	// ID is the namespaced contract ID.
	ID string
	// Version is the contract version.
	Version uint32
	// Stability is the compatibility classification.
	Stability ContractStability
}

// ContractRegistryVersion selects one frozen semantic-model registry.
type ContractRegistryVersion uint8

const (
	// RegistryV1 is the Consema 0.3 semantic-model v1 registry.
	RegistryV1 ContractRegistryVersion = iota
	// RegistryV2 is the Consema 0.4 semantic-model v2 registry.
	RegistryV2
	// RegistryV3 is the Consema 0.5 semantic-model v3 registry.
	RegistryV3
	// RegistryV4 is the Consema 0.6 semantic-model v4 registry.
	RegistryV4
	// RegistryV5 is the Consema 0.7 semantic-model v5 registry.
	RegistryV5
	// RegistryV6 is the Consema 0.8 semantic-model v6 registry.
	RegistryV6
	// RegistryV7 is the Consema 0.12 semantic-model v7 registry (CLI
	// machine payloads).
	RegistryV7
)

// ContractRegistry is a closed, explicitly versioned contract registry.
type ContractRegistry struct {
	version ContractRegistryVersion
}

// NewContractRegistry returns the registry for one frozen semantic-model
// version.
func NewContractRegistry(version ContractRegistryVersion) ContractRegistry {
	return ContractRegistry{version: version}
}

// DefaultContractRegistry returns the semantic-model v1 registry (the Rust
// Default).
func DefaultContractRegistry() ContractRegistry {
	return NewContractRegistry(RegistryV1)
}

// Version reports the semantic-model version of the registry.
func (r ContractRegistry) Version() ContractRegistryVersion { return r.version }

// Contracts returns the sorted immutable descriptors.
func (r ContractRegistry) Contracts() []ContractDescriptor {
	return append([]ContractDescriptor(nil), contractsV7(r.version)...)
}

// Recognizes reports whether an exact ID/version pair is registered.
func (r ContractRegistry) Recognizes(contract *ContractId) bool {
	return r.descriptor(contract) != nil
}

// Descriptor returns the exact registered descriptor for the contract.
func (r ContractRegistry) Descriptor(contract *ContractId) *ContractDescriptor {
	return r.descriptor(contract)
}

func (r ContractRegistry) descriptor(contract *ContractId) *ContractDescriptor {
	records := contractsV7(r.version)
	index := sort.Search(len(records), func(i int) bool {
		candidate := records[i]
		return candidate.ID > contract.id || (candidate.ID == contract.id && candidate.Version >= contract.version)
	})
	if index < len(records) {
		candidate := records[index]
		if candidate.ID == contract.id && candidate.Version == contract.version {
			return &candidate
		}
	}
	return nil
}

// ErrorCodeRegistry returns the error-code registry for the same semantic
// model version.
func (r ContractRegistry) ErrorCodeRegistry() ErrorCodeRegistry {
	return NewErrorCodeRegistry(ErrorRegistryVersion(r.version))
}

// contractsV7 returns the frozen contract records of one semantic-model
// version. The lists are transcribed from the Rust CONTRACTS_V1..V7
// (consema-rs/consema-protocol/src/contract.rs) and are strictly sorted
// by (id, version); the test battery re-pins the counts
// (16/18/25/25/30/38/41) and sortedness.
func contractsV7(version ContractRegistryVersion) []ContractDescriptor {
	switch version {
	case RegistryV1:
		return contractsV1
	case RegistryV2:
		return contractsV2
	case RegistryV3, RegistryV4:
		return contractsV3
	case RegistryV5:
		return contractsV5
	case RegistryV6:
		return contractsV6
	case RegistryV7:
		return contractsV7List
	}
	return nil
}

func contract(id string, version uint32, stability ContractStability) ContractDescriptor {
	return ContractDescriptor{ID: id, Version: version, Stability: stability}
}

func stable(id string) ContractDescriptor { return contract(id, 1, StabilityStable) }
func transport(id string) ContractDescriptor {
	return contract(id, 1, StabilityTransport)
}

var contractsV1 = []ContractDescriptor{
	stable("core.cancellation-request"),
	stable("core.capability-declaration"),
	stable("core.change-set"),
	stable("core.completion"),
	stable("core.diagnostic"),
	stable("core.error-code-registry"),
	stable("core.execution-policy"),
	stable("core.profile-descriptor"),
	stable("core.projection-report"),
	stable("core.projection-request"),
	stable("core.projection-result"),
	transport("core.protocol-message"),
	stable("core.provenance-map"),
	stable("core.query-definition"),
	stable("core.query-result"),
	stable("core.registry-manifest"),
}

var contractsV2 = []ContractDescriptor{
	stable("core.cancellation-request"),
	stable("core.capability-declaration"),
	stable("core.change-set"),
	stable("core.completion"),
	stable("core.diagnostic"),
	stable("core.error-code-registry"),
	stable("core.execution-policy"),
	stable("core.profile-descriptor"),
	stable("core.projection-report"),
	stable("core.projection-request"),
	stable("core.projection-result"),
	transport("core.protocol-message"),
	stable("core.provenance-map"),
	stable("core.query-definition"),
	stable("core.query-result"),
	stable("core.registry-manifest"),
	stable("core.source-patch"),
	stable("core.source-snapshot"),
}

var contractsV3 = []ContractDescriptor{
	stable("core.cancellation-request"),
	stable("core.capability-declaration"),
	stable("core.change-set"),
	stable("core.completion"),
	stable("core.conversion-report"),
	stable("core.diagnostic"),
	stable("core.edit-plan"),
	stable("core.error-code-registry"),
	stable("core.execution-policy"),
	stable("core.format-operation-registry"),
	stable("core.materialization-provenance-map"),
	stable("core.materialization-report"),
	stable("core.materialization-request"),
	stable("core.materialization-result"),
	stable("core.profile-descriptor"),
	stable("core.projection-report"),
	stable("core.projection-request"),
	stable("core.projection-result"),
	transport("core.protocol-message"),
	stable("core.provenance-map"),
	stable("core.query-definition"),
	stable("core.query-result"),
	stable("core.registry-manifest"),
	stable("core.source-patch"),
	stable("core.source-snapshot"),
}

var contractsV5 = []ContractDescriptor{
	stable("core.cancellation-request"),
	stable("core.capability-declaration"),
	stable("core.change-set"),
	stable("core.completion"),
	stable("core.conversion-report"),
	stable("core.diagnostic"),
	stable("core.edit-plan"),
	stable("core.error-code-registry"),
	stable("core.execution-policy"),
	stable("core.format-operation-registry"),
	stable("core.graph-projection-result"),
	stable("core.graph-provenance-map"),
	stable("core.graph-query-result"),
	stable("core.materialization-provenance-map"),
	stable("core.materialization-report"),
	stable("core.materialization-request"),
	stable("core.materialization-result"),
	stable("core.portable-graph"),
	stable("core.profile-descriptor"),
	stable("core.projection-report"),
	stable("core.projection-request"),
	stable("core.projection-result"),
	transport("core.protocol-message"),
	stable("core.provenance-map"),
	stable("core.query-definition"),
	stable("core.query-result"),
	stable("core.registry-manifest"),
	stable("core.source-patch"),
	stable("core.source-snapshot"),
	stable("core.yaml-query-result"),
}

var contractsV6 = []ContractDescriptor{
	stable("core.cancellation-request"),
	stable("core.capability-declaration"),
	stable("core.change-set"),
	stable("core.completion"),
	stable("core.conversion-report"),
	stable("core.diagnostic"),
	stable("core.edit-plan"),
	stable("core.error-code-registry"),
	stable("core.execution-policy"),
	stable("core.format-operation-registry"),
	stable("core.graph-projection-result"),
	stable("core.graph-provenance-map"),
	stable("core.graph-query-result"),
	stable("core.ini-query-result"),
	stable("core.java-properties-query-result"),
	stable("core.java-utf16-string"),
	stable("core.materialization-provenance-map"),
	stable("core.materialization-report"),
	stable("core.materialization-request"),
	contract("core.materialization-request", 2, StabilityStable),
	stable("core.materialization-result"),
	contract("core.materialization-result", 2, StabilityStable),
	stable("core.portable-graph"),
	stable("core.profile-descriptor"),
	stable("core.projection-report"),
	stable("core.projection-request"),
	stable("core.projection-result"),
	transport("core.protocol-message"),
	stable("core.provenance-map"),
	stable("core.query-definition"),
	stable("core.query-result"),
	stable("core.registry-manifest"),
	stable("core.source-encoding"),
	stable("core.source-patch"),
	contract("core.source-patch", 2, StabilityStable),
	stable("core.source-snapshot"),
	contract("core.source-snapshot", 2, StabilityStable),
	stable("core.yaml-query-result"),
}

var contractsV7List = []ContractDescriptor{
	stable("core.batch-plan"),
	stable("core.batch-result"),
	stable("core.cancellation-request"),
	stable("core.capability-declaration"),
	stable("core.change-set"),
	stable("core.cli-output"),
	stable("core.completion"),
	stable("core.conversion-report"),
	stable("core.diagnostic"),
	stable("core.edit-plan"),
	stable("core.error-code-registry"),
	stable("core.execution-policy"),
	stable("core.format-operation-registry"),
	stable("core.graph-projection-result"),
	stable("core.graph-provenance-map"),
	stable("core.graph-query-result"),
	stable("core.ini-query-result"),
	stable("core.java-properties-query-result"),
	stable("core.java-utf16-string"),
	stable("core.materialization-provenance-map"),
	stable("core.materialization-report"),
	stable("core.materialization-request"),
	contract("core.materialization-request", 2, StabilityStable),
	stable("core.materialization-result"),
	contract("core.materialization-result", 2, StabilityStable),
	stable("core.portable-graph"),
	stable("core.profile-descriptor"),
	stable("core.projection-report"),
	stable("core.projection-request"),
	stable("core.projection-result"),
	transport("core.protocol-message"),
	stable("core.provenance-map"),
	stable("core.query-definition"),
	stable("core.query-result"),
	stable("core.registry-manifest"),
	stable("core.source-encoding"),
	stable("core.source-patch"),
	contract("core.source-patch", 2, StabilityStable),
	stable("core.source-snapshot"),
	contract("core.source-snapshot", 2, StabilityStable),
	stable("core.yaml-query-result"),
}

// ProtocolMessage is one validated protocol payload in the common envelope
// (consema-rs/consema-protocol/src/contract.rs).
type ProtocolMessage struct {
	contract *ContractId
	payload  core.Value
}

// NewProtocolMessage validates a recognized contract, rejects transport
// envelopes as nested payload contracts, checks the payload schema
// discriminator, and applies the registered-payload validation of the
// contracts whose Go message types exist in this package.
//
// Documented scope note: the Rust side additionally dispatches every
// registered contract to its full record decoder
// (consema-rs/consema-protocol/src/payload.rs). The Go package implements those
// record types in stages: core.cli-output@1, core.batch-plan@1,
// core.batch-result@1, core.diagnostic@1, core.query-definition@1,
// core.capability-declaration@1, core.profile-descriptor@1,
// core.error-code-registry@1, and core.registry-manifest@1 are validated in
// full here; the remaining contracts are validated to the schema
// discriminator level until their owning milestone ships the record type
// (documented reachable-code difference; the shared vectors exercise only
// the implemented records).
func NewProtocolMessage(contract *ContractId, payload core.Value, registry ContractRegistry) (*ProtocolMessage, error) {
	descriptor := registry.descriptor(contract)
	if descriptor == nil {
		return nil, protocolError(KindUnknownContract, "$.contract", contract.Schema())
	}
	if descriptor.Stability == StabilityTransport {
		return nil, protocolError(KindInvalidValue, "$.contract", "transport envelopes cannot be nested as payload contracts")
	}
	if err := validateContractPayloadSchema(payload, contract); err != nil {
		return nil, err
	}
	if err := validateRegisteredPayload(contract, payload, registry); err != nil {
		return nil, err
	}
	return &ProtocolMessage{contract: contract, payload: payload}, nil
}

// Contract returns the exact contract.
func (m *ProtocolMessage) Contract() *ContractId { return m.contract }

// Payload returns the validated payload.
func (m *ProtocolMessage) Payload() core.Value { return m.payload }

// ToValue encodes the fixed `core.protocol-message@1` envelope as a
// PortableValue tree.
func (m *ProtocolMessage) ToValue() (core.Value, error) {
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.protocol-message@1")},
		core.Entry{Key: "contract_id", Value: core.String(m.contract.id)},
		core.Entry{Key: "contract_version", Value: integerValue(uint64(m.contract.version))},
		core.Entry{Key: "payload", Value: m.payload},
	)
}

// FromValue strictly decodes the envelope and validates the selected payload
// contract.
func (m *ProtocolMessage) FromValue(value core.Value, registry ContractRegistry) (*ProtocolMessage, error) {
	fields, err := schemaFields(value, "core.protocol-message@1",
		[]string{"schema", "contract_id", "contract_version", "payload"}, "$")
	if err != nil {
		return nil, err
	}
	id, err := stringOf(fields[1], "$.contract_id")
	if err != nil {
		return nil, err
	}
	version, err := unsigned32(fields[2], "$.contract_version")
	if err != nil {
		return nil, err
	}
	contract, err := NewContractId(id, version)
	if err != nil {
		return nil, err
	}
	return NewProtocolMessage(contract, fields[3], registry)
}

// ToJSON encodes the envelope through canonical tagged JSON.
func (m *ProtocolMessage) ToJSON(limits ProtocolLimits) ([]byte, error) {
	value, err := m.ToValue()
	if err != nil {
		return nil, err
	}
	return EncodeJSON(value, limits)
}

// FromJSON decodes canonical tagged JSON and validates the registry
// contract.
func (m *ProtocolMessage) FromJSON(bytes []byte, limits ProtocolLimits, registry ContractRegistry) (*ProtocolMessage, error) {
	value, err := DecodeJSON(bytes, limits)
	if err != nil {
		return nil, err
	}
	return m.FromValue(value, registry)
}

// ToPVCE encodes the envelope through canonical PVCE/1.
func (m *ProtocolMessage) ToPVCE(limits ProtocolLimits) ([]byte, error) {
	value, err := m.ToValue()
	if err != nil {
		return nil, err
	}
	return EncodePVCE(value, limits)
}

// FromPVCE decodes canonical PVCE/1 and validates the registry contract.
func (m *ProtocolMessage) FromPVCE(bytes []byte, limits ProtocolLimits, registry ContractRegistry) (*ProtocolMessage, error) {
	value, err := DecodePVCE(bytes, limits)
	if err != nil {
		return nil, err
	}
	return m.FromValue(value, registry)
}

// validateContractPayloadSchema requires the payload to be an Object whose
// first field is "schema" carrying the exact contract schema
// (contract.rs).
func validateContractPayloadSchema(payload core.Value, contract *ContractId) error {
	object, ok := payload.(*core.Object)
	if !ok {
		return protocolError(KindWrongType, "$.payload", "payload must be an Object")
	}
	entries := object.Entries()
	if len(entries) == 0 {
		return protocolError(KindMissingField, "$.payload.schema", "payload schema is absent")
	}
	if entries[0].Key != "schema" {
		return protocolError(KindSchemaMismatch, "$.payload", "schema must be the first field")
	}
	observed, err := stringOf(entries[0].Value, "$.payload.schema")
	if err != nil {
		return err
	}
	if observed != contract.Schema() {
		return protocolError(KindSchemaMismatch, "$.payload.schema", "expected "+contract.Schema())
	}
	return nil
}
