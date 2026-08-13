package protocol

import (
	"sort"
	"strings"

	"consema.dev/consema/core"
)

// This file implements the transferable Profile and Capability registry
// records plus the registry manifest (consema-rs/consema-protocol/src/registry.rs
// and registry_manifest.rs).

// ProfileReference is a versioned reference to a Profile, whose ID may
// contain numeric segments (registry.rs:14-46).
type ProfileReference struct {
	id      string
	version uint32
}

// NewProfileReference validates and creates a profile reference.
func NewProfileReference(id string, version uint32) (*ProfileReference, error) {
	if err := validateNamespace(id, true, "$.profile.id"); err != nil {
		return nil, err
	}
	if version == 0 {
		return nil, invalid("$.profile.version", "version must be non-zero")
	}
	return &ProfileReference{id: id, version: version}, nil
}

// ID returns the profile namespace.
func (r *ProfileReference) ID() string { return r.id }

// Version returns the immutable version.
func (r *ProfileReference) Version() uint32 { return r.version }

// ProfileDescriptor is an immutable language profile registry descriptor
// (registry.rs:48-250).
type ProfileDescriptor struct {
	formatFamilyID       string
	formatFamilyVersion  uint32
	profileID            string
	profileVersion       uint32
	baseProfile          *ProfileReference
	differences          []string
	requiredCapabilities []*CapabilityId
}

// NewProfileDescriptor creates a normalized descriptor and rejects malformed
// or duplicate facts (registry.rs:60-114).
func NewProfileDescriptor(formatFamilyID string, formatFamilyVersion uint32,
	profileID string, profileVersion uint32, baseProfile *ProfileReference,
	differences []string, requiredCapabilities []*CapabilityId) (*ProfileDescriptor, error) {
	if err := validateNamespace(formatFamilyID, false, "$.format_family_id"); err != nil {
		return nil, err
	}
	if err := validateNamespace(profileID, true, "$.profile_id"); err != nil {
		return nil, err
	}
	if formatFamilyVersion == 0 || profileVersion == 0 {
		return nil, invalid("$", "family and profile versions must be non-zero")
	}
	for _, difference := range differences {
		if err := validateNamespace(difference, true, "$.differences"); err != nil {
			return nil, err
		}
	}
	for _, capability := range requiredCapabilities {
		if _, err := NewContractId(capability.Namespace(), capability.Version()); err != nil {
			return nil, err
		}
	}
	sortedDifferences := append([]string(nil), differences...)
	sort.Strings(sortedDifferences)
	for index := 1; index < len(sortedDifferences); index++ {
		if sortedDifferences[index-1] == sortedDifferences[index] {
			return nil, invalid("$.differences", "difference IDs must be unique")
		}
	}
	sortedCapabilities := append([]*CapabilityId(nil), requiredCapabilities...)
	sort.Slice(sortedCapabilities, func(i, j int) bool {
		return sortedCapabilities[i].Compare(sortedCapabilities[j]) < 0
	})
	for index := 1; index < len(sortedCapabilities); index++ {
		if sortedCapabilities[index-1].Compare(sortedCapabilities[index]) == 0 {
			return nil, invalid("$.required_capabilities", "capability IDs must be unique")
		}
	}
	return &ProfileDescriptor{
		formatFamilyID:       formatFamilyID,
		formatFamilyVersion:  formatFamilyVersion,
		profileID:            profileID,
		profileVersion:       profileVersion,
		baseProfile:          baseProfile,
		differences:          sortedDifferences,
		requiredCapabilities: sortedCapabilities,
	}, nil
}

// FormatFamilyID returns the format-family namespace.
func (d *ProfileDescriptor) FormatFamilyID() string { return d.formatFamilyID }

// FormatFamilyVersion returns the format-family contract version.
func (d *ProfileDescriptor) FormatFamilyVersion() uint32 { return d.formatFamilyVersion }

// ProfileID returns the profile namespace.
func (d *ProfileDescriptor) ProfileID() string { return d.profileID }

// ProfileVersion returns the profile version.
func (d *ProfileDescriptor) ProfileVersion() uint32 { return d.profileVersion }

// BaseProfile returns the optional immutable base profile.
func (d *ProfileDescriptor) BaseProfile() *ProfileReference { return d.baseProfile }

// Differences returns the sorted stable difference identifiers.
func (d *ProfileDescriptor) Differences() []string {
	return append([]string(nil), d.differences...)
}

// RequiredCapabilities returns the sorted required capabilities.
func (d *ProfileDescriptor) RequiredCapabilities() []*CapabilityId {
	return append([]*CapabilityId(nil), d.requiredCapabilities...)
}

// ToValue encodes `core.profile-descriptor@1` (registry.rs:158-201).
func (d *ProfileDescriptor) ToValue() (core.Value, error) {
	differences := make([]core.Value, 0, len(d.differences))
	for _, difference := range d.differences {
		differences = append(differences, core.String(difference))
	}
	capabilities := make([]core.Value, 0, len(d.requiredCapabilities))
	for _, capability := range d.requiredCapabilities {
		capabilities = append(capabilities, referenceValue(capability.Namespace(), capability.Version()))
	}
	baseProfile := core.NullValue()
	if d.baseProfile != nil {
		baseProfile = referenceValue(d.baseProfile.id, d.baseProfile.version)
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.profile-descriptor@1")},
		core.Entry{Key: "format_family_id", Value: core.String(d.formatFamilyID)},
		core.Entry{Key: "format_family_version", Value: integerValue(uint64(d.formatFamilyVersion))},
		core.Entry{Key: "profile_id", Value: core.String(d.profileID)},
		core.Entry{Key: "profile_version", Value: integerValue(uint64(d.profileVersion))},
		core.Entry{Key: "base_profile", Value: baseProfile},
		core.Entry{Key: "differences", Value: core.NewArray(differences...)},
		core.Entry{Key: "required_capabilities", Value: core.NewArray(capabilities...)},
	)
}

// FromValue strictly decodes `core.profile-descriptor@1` (registry.rs:203-249).
func (d *ProfileDescriptor) FromValue(value core.Value) (*ProfileDescriptor, error) {
	fields, err := schemaFields(value, "core.profile-descriptor@1",
		[]string{"schema", "format_family_id", "format_family_version", "profile_id",
			"profile_version", "base_profile", "differences", "required_capabilities"}, "$")
	if err != nil {
		return nil, err
	}
	formatFamilyID, err := stringOf(fields[1], "$.format_family_id")
	if err != nil {
		return nil, err
	}
	formatFamilyVersion, err := unsigned32(fields[2], "$.format_family_version")
	if err != nil {
		return nil, err
	}
	profileID, err := stringOf(fields[3], "$.profile_id")
	if err != nil {
		return nil, err
	}
	profileVersion, err := unsigned32(fields[4], "$.profile_version")
	if err != nil {
		return nil, err
	}
	var baseProfile *ProfileReference
	if _, isNull := fields[5].(core.Null); !isNull {
		baseProfile, err = parseProfileReference(fields[5], "$.base_profile")
		if err != nil {
			return nil, err
		}
	}
	differenceValues, err := sequenceOf(fields[6], "$.differences")
	if err != nil {
		return nil, err
	}
	differences := make([]string, 0, len(differenceValues))
	for index, item := range differenceValues {
		text, err := stringOf(item, "$.differences["+uint32String(uint32(index))+"]")
		if err != nil {
			return nil, err
		}
		differences = append(differences, text)
	}
	capabilityValues, err := sequenceOf(fields[7], "$.required_capabilities")
	if err != nil {
		return nil, err
	}
	capabilities := make([]*CapabilityId, 0, len(capabilityValues))
	for index, item := range capabilityValues {
		path := "$.required_capabilities[" + uint32String(uint32(index)) + "]"
		contract, err := parseContractReference(item, path)
		if err != nil {
			return nil, err
		}
		capabilities = append(capabilities, NewCapabilityId(contract.ID(), contract.Version()))
	}
	return NewProfileDescriptor(formatFamilyID, formatFamilyVersion, profileID,
		profileVersion, baseProfile, differences, capabilities)
}

// CapabilityId is a stable namespaced capability contract
// (consema-core capability.rs:7-28).
type CapabilityId struct {
	namespace string
	version   uint32
}

// NewCapabilityId creates a capability identifier.
func NewCapabilityId(namespace string, version uint32) *CapabilityId {
	return &CapabilityId{namespace: namespace, version: version}
}

// Namespace returns the namespaced identifier without the version suffix.
func (c *CapabilityId) Namespace() string { return c.namespace }

// Version returns the immutable contract version.
func (c *CapabilityId) Version() uint32 { return c.version }

// Compare orders capability ids by (namespace, version).
func (c *CapabilityId) Compare(other *CapabilityId) int {
	if c.namespace != other.namespace {
		return strings.Compare(c.namespace, other.namespace)
	}
	if c.version < other.version {
		return -1
	}
	if c.version > other.version {
		return 1
	}
	return 0
}

// Equal reports whether two capability ids are identical.
func (c *CapabilityId) Equal(other *CapabilityId) bool {
	return c.namespace == other.namespace && c.version == other.version
}

// CapabilitySet is a deterministic set of capabilities available to an
// operation (consema-core capability.rs:59-96).
type CapabilitySet struct {
	capabilities map[string]*CapabilityId
}

// NewCapabilitySet returns an empty set.
func NewCapabilitySet() *CapabilitySet {
	return &CapabilitySet{capabilities: make(map[string]*CapabilityId)}
}

// Insert adds a capability and reports whether it was newly added.
func (s *CapabilitySet) Insert(capability *CapabilityId) bool {
	key := capability.namespace + "@" + uint32String(capability.version)
	if _, exists := s.capabilities[key]; exists {
		return false
	}
	s.capabilities[key] = capability
	return true
}

// Contains reports whether a capability is available.
func (s *CapabilitySet) Contains(capability *CapabilityId) bool {
	key := capability.namespace + "@" + uint32String(capability.version)
	_, exists := s.capabilities[key]
	return exists
}

// Iterate visits the capabilities in stable identifier order.
func (s *CapabilitySet) Iterate(visit func(*CapabilityId)) {
	keys := make([]string, 0, len(s.capabilities))
	for key := range s.capabilities {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		visit(s.capabilities[key])
	}
}

// ImplementationSupport is the declared support state of one capability
// (consema-core capability.rs:30-43).
type ImplementationSupport struct {
	// Kind is the closed support kind.
	Kind SupportKind
	// Preconditions are the machine-readable preconditions of Conditional
	// support, in sorted key order.
	Preconditions []Precondition
}

// SupportKind is the closed support kind.
type SupportKind uint8

const (
	// SupportConformant promises the whole contract.
	SupportConformant SupportKind = iota
	// SupportConditional depends on machine-readable preconditions.
	SupportConditional
	// SupportUnsupported is unavailable.
	SupportUnsupported
)

// Precondition is one machine-readable conditional-support precondition.
type Precondition struct {
	// Key is the precondition name.
	Key string
	// Value is the precondition value.
	Value string
}

// VerificationStatus is how capability support was verified
// (consema-core capability.rs:45-56).
type VerificationStatus string

const (
	// VerificationVerified was verified against the named conformance suite.
	VerificationVerified VerificationStatus = "Verified"
	// VerificationSelfDeclared was declared by the implementation.
	VerificationSelfDeclared VerificationStatus = "SelfDeclared"
	// VerificationUnverified was not verified.
	VerificationUnverified VerificationStatus = "Unverified"
)

// ParseVerificationStatus parses one canonical verification spelling.
func ParseVerificationStatus(name string) (VerificationStatus, error) {
	switch name {
	case "Verified":
		return VerificationVerified, nil
	case "SelfDeclared":
		return VerificationSelfDeclared, nil
	case "Unverified":
		return VerificationUnverified, nil
	}
	return "", invalid("$.verification", "unknown verification status")
}

// String returns the canonical verification spelling.
func (s VerificationStatus) String() string { return string(s) }

// CapabilityDeclaration is one implementation's support and verification
// claim for a capability (registry.rs:252-439).
type CapabilityDeclaration struct {
	capability   *CapabilityId
	support      ImplementationSupport
	verification VerificationStatus
	suiteID      *string
}

// NewCapabilityDeclaration validates the cross-field support and
// verification invariants (registry.rs:262-315).
func NewCapabilityDeclaration(capability *CapabilityId, support ImplementationSupport,
	verification VerificationStatus, suiteID *string) (*CapabilityDeclaration, error) {
	if _, err := NewContractId(capability.Namespace(), capability.Version()); err != nil {
		return nil, err
	}
	if support.Kind == SupportConditional && len(support.Preconditions) == 0 {
		return nil, invalid("$.preconditions", "Conditional support requires preconditions")
	}
	if support.Kind != SupportConditional && len(support.Preconditions) != 0 {
		return nil, invalid("$.preconditions", "only Conditional support may carry preconditions")
	}
	seen := make(map[string]struct{})
	for _, precondition := range support.Preconditions {
		if _, exists := seen[precondition.Key]; exists {
			return nil, invalid("$.preconditions", "precondition keys must be unique")
		}
		seen[precondition.Key] = struct{}{}
	}
	if verification == VerificationVerified {
		if suiteID == nil {
			return nil, invalid("$.suite_id", "Verified requires a suite ID")
		}
		if err := validateNamespace(*suiteID, true, "$.suite_id"); err != nil {
			return nil, err
		}
	} else if suiteID != nil {
		return nil, invalid("$.suite_id", "only Verified may name a suite")
	}
	return &CapabilityDeclaration{
		capability:   capability,
		support:      support,
		verification: verification,
		suiteID:      suiteID,
	}, nil
}

// Capability returns the capability contract.
func (c *CapabilityDeclaration) Capability() *CapabilityId { return c.capability }

// Support returns the declared implementation support.
func (c *CapabilityDeclaration) Support() ImplementationSupport { return c.support }

// Verification returns the verification status.
func (c *CapabilityDeclaration) Verification() VerificationStatus { return c.verification }

// SuiteID returns the conformance suite used for Verified status.
func (c *CapabilityDeclaration) SuiteID() *string { return c.suiteID }

// ToValue encodes `core.capability-declaration@1` (registry.rs:341-379).
func (c *CapabilityDeclaration) ToValue() (core.Value, error) {
	supportName := "Conformant"
	preconditions := make(map[string]string)
	switch c.support.Kind {
	case SupportConditional:
		supportName = "Conditional"
		for _, precondition := range c.support.Preconditions {
			preconditions[precondition.Key] = precondition.Value
		}
	case SupportUnsupported:
		supportName = "Unsupported"
	}
	preconditionObject, err := stringMapObject(preconditions)
	if err != nil {
		return nil, err
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.capability-declaration@1")},
		core.Entry{Key: "capability_id", Value: core.String(c.capability.Namespace())},
		core.Entry{Key: "capability_version", Value: integerValue(uint64(c.capability.Version()))},
		core.Entry{Key: "support", Value: core.String(supportName)},
		core.Entry{Key: "preconditions", Value: preconditionObject},
		core.Entry{Key: "verification", Value: core.String(c.verification.String())},
		core.Entry{Key: "suite_id", Value: nullableString(c.suiteID)},
	)
}

// FromValue strictly decodes `core.capability-declaration@1`
// (registry.rs:381-438).
func (c *CapabilityDeclaration) FromValue(value core.Value) (*CapabilityDeclaration, error) {
	fields, err := schemaFields(value, "core.capability-declaration@1",
		[]string{"schema", "capability_id", "capability_version", "support",
			"preconditions", "verification", "suite_id"}, "$")
	if err != nil {
		return nil, err
	}
	namespace, err := stringOf(fields[1], "$.capability_id")
	if err != nil {
		return nil, err
	}
	version, err := unsigned32(fields[2], "$.capability_version")
	if err != nil {
		return nil, err
	}
	preconditionMap, err := stringMapFromObject(fields[4], "$.preconditions")
	if err != nil {
		return nil, err
	}
	preconditionKeys := make([]string, 0, len(preconditionMap))
	for key := range preconditionMap {
		preconditionKeys = append(preconditionKeys, key)
	}
	sort.Strings(preconditionKeys)
	preconditions := make([]Precondition, 0, len(preconditionKeys))
	for _, key := range preconditionKeys {
		preconditions = append(preconditions, Precondition{Key: key, Value: preconditionMap[key]})
	}
	supportName, err := stringOf(fields[3], "$.support")
	if err != nil {
		return nil, err
	}
	var support ImplementationSupport
	switch {
	case supportName == "Conformant" && len(preconditions) == 0:
		support = ImplementationSupport{Kind: SupportConformant}
	case supportName == "Conditional":
		support = ImplementationSupport{Kind: SupportConditional, Preconditions: preconditions}
	case supportName == "Unsupported" && len(preconditions) == 0:
		support = ImplementationSupport{Kind: SupportUnsupported}
	default:
		return nil, invalid("$.support", "invalid support/preconditions combination")
	}
	verificationName, err := stringOf(fields[5], "$.verification")
	if err != nil {
		return nil, err
	}
	verification, err := ParseVerificationStatus(verificationName)
	if err != nil {
		return nil, err
	}
	suiteID, err := optionalString(fields[6], "$.suite_id")
	if err != nil {
		return nil, err
	}
	return NewCapabilityDeclaration(NewCapabilityId(namespace, version), support, verification, suiteID)
}

// RegistryManifest is the `core.registry-manifest@1` record of one
// semantic-model contract set (registry_manifest.rs:30-282).
type RegistryManifest struct {
	semanticModel *ContractId
	contracts     []ContractManifestEntry
	errorCodes    []ErrorCodeManifestEntry
}

// ContractManifestEntry is one owned contract entry.
type ContractManifestEntry struct {
	// Contract is the contract identity.
	Contract *ContractId
	// Stability is the compatibility classification.
	Stability ContractStability
}

// ErrorCodeManifestEntry is one owned error-code entry.
type ErrorCodeManifestEntry struct {
	// Code is the full code including version.
	Code string
	// Category is the semantic category.
	Category DiagnosticCategory
	// Introduced is the first release containing the code.
	Introduced string
	// Description is the human-facing description.
	Description string
}

// NewRegistryManifest builds a manifest from one semantic-model version,
// mirroring the Rust version constructors.
func NewRegistryManifest(semanticModelVersion uint32, contractRegistry ContractRegistry,
	errorCodeRegistry ErrorCodeRegistry) (*RegistryManifest, error) {
	contracts := make([]ContractManifestEntry, 0, len(contractRegistry.Contracts()))
	for _, descriptor := range contractRegistry.Contracts() {
		contract, err := NewContractId(descriptor.ID, descriptor.Version)
		if err != nil {
			return nil, err
		}
		contracts = append(contracts, ContractManifestEntry{Contract: contract, Stability: descriptor.Stability})
	}
	codes := make([]ErrorCodeManifestEntry, 0, len(errorCodeRegistry.Codes()))
	for _, descriptor := range errorCodeRegistry.Codes() {
		codes = append(codes, ErrorCodeManifestEntry{
			Code:        descriptor.Code,
			Category:    descriptor.Category,
			Introduced:  descriptor.Introduced,
			Description: descriptor.Description,
		})
	}
	semanticModel, err := NewContractId("core.semantic-model", semanticModelVersion)
	if err != nil {
		return nil, err
	}
	return &RegistryManifest{semanticModel: semanticModel, contracts: contracts, errorCodes: codes}, nil
}

// ValidateRegistryManifest validates a manifest's sorted, unique, versioned
// records (registry_manifest.rs:119-151).
func ValidateRegistryManifest(semanticModel *ContractId, contracts []ContractManifestEntry,
	errorCodes []ErrorCodeManifestEntry) (*RegistryManifest, error) {
	for index := 1; index < len(contracts); index++ {
		if contracts[index-1].Contract.Compare(contracts[index].Contract) >= 0 {
			return nil, invalid("$", "manifest records must be sorted and unique")
		}
	}
	for index := 1; index < len(errorCodes); index++ {
		if errorCodes[index-1].Code >= errorCodes[index].Code {
			return nil, invalid("$", "manifest records must be sorted and unique")
		}
	}
	for _, entry := range errorCodes {
		if err := validateVersionedCode(entry.Code, "$.error_codes.code"); err != nil {
			return nil, err
		}
		if entry.Introduced == "" || entry.Description == "" {
			return nil, invalid("$.error_codes", "error-code metadata cannot be empty")
		}
	}
	return &RegistryManifest{semanticModel: semanticModel, contracts: contracts, errorCodes: errorCodes}, nil
}

// SemanticModel returns the semantic model ID/version.
func (m *RegistryManifest) SemanticModel() *ContractId { return m.semanticModel }

// Contracts returns the sorted contract records.
func (m *RegistryManifest) Contracts() []ContractManifestEntry {
	return append([]ContractManifestEntry(nil), m.contracts...)
}

// ErrorCodes returns the sorted error-code records.
func (m *RegistryManifest) ErrorCodes() []ErrorCodeManifestEntry {
	return append([]ErrorCodeManifestEntry(nil), m.errorCodes...)
}

// IsCurrent reports whether this manifest exactly equals the built-in
// current (v7) contract set.
func (m *RegistryManifest) IsCurrent() bool {
	current, err := NewRegistryManifest(7, NewContractRegistry(RegistryV7), NewErrorCodeRegistry(ErrorRegistryV7))
	if err != nil {
		return false
	}
	if !m.semanticModel.Equal(current.semanticModel) {
		return false
	}
	if len(m.contracts) != len(current.contracts) || len(m.errorCodes) != len(current.errorCodes) {
		return false
	}
	for index := range m.contracts {
		if !m.contracts[index].Contract.Equal(current.contracts[index].Contract) ||
			m.contracts[index].Stability != current.contracts[index].Stability {
			return false
		}
	}
	for index := range m.errorCodes {
		if m.errorCodes[index] != current.errorCodes[index] {
			return false
		}
	}
	return true
}

// ToValue encodes `core.registry-manifest@1` (registry_manifest.rs:177-230).
func (m *RegistryManifest) ToValue() (core.Value, error) {
	contracts := make([]core.Value, 0, len(m.contracts))
	for _, entry := range m.contracts {
		contract, err := core.NewObject(
			core.Entry{Key: "id", Value: core.String(entry.Contract.ID())},
			core.Entry{Key: "version", Value: integerValue(uint64(entry.Contract.Version()))},
			core.Entry{Key: "stability", Value: core.String(entry.Stability.String())},
		)
		if err != nil {
			return nil, err
		}
		contracts = append(contracts, contract)
	}
	errorCodeValues := make([]core.Value, 0, len(m.errorCodes))
	for _, entry := range m.errorCodes {
		record, err := core.NewObject(
			core.Entry{Key: "code", Value: core.String(entry.Code)},
			core.Entry{Key: "category", Value: core.String(entry.Category.String())},
			core.Entry{Key: "introduced", Value: core.String(entry.Introduced)},
			core.Entry{Key: "stability", Value: core.String("Stable")},
			core.Entry{Key: "description", Value: core.String(entry.Description)},
		)
		if err != nil {
			return nil, err
		}
		errorCodeValues = append(errorCodeValues, record)
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.registry-manifest@1")},
		core.Entry{Key: "semantic_model", Value: referenceValue(m.semanticModel.ID(), m.semanticModel.Version())},
		core.Entry{Key: "contracts", Value: core.NewArray(contracts...)},
		core.Entry{Key: "error_codes", Value: core.NewArray(errorCodeValues...)},
	)
}

// FromValue strictly decodes `core.registry-manifest@1`
// (registry_manifest.rs:232-281).
func (m *RegistryManifest) FromValue(value core.Value) (*RegistryManifest, error) {
	fields, err := schemaFields(value, "core.registry-manifest@1",
		[]string{"schema", "semantic_model", "contracts", "error_codes"}, "$")
	if err != nil {
		return nil, err
	}
	semanticModel, err := parseContractReference(fields[1], "$.semantic_model")
	if err != nil {
		return nil, err
	}
	contractValues, err := sequenceOf(fields[2], "$.contracts")
	if err != nil {
		return nil, err
	}
	contracts := make([]ContractManifestEntry, 0, len(contractValues))
	for index, item := range contractValues {
		path := "$.contracts[" + uint32String(uint32(index)) + "]"
		entry, err := exactFields(item, []string{"id", "version", "stability"}, path)
		if err != nil {
			return nil, err
		}
		id, err := stringOf(entry[0], path+".id")
		if err != nil {
			return nil, err
		}
		version, err := unsigned32(entry[1], path+".version")
		if err != nil {
			return nil, err
		}
		contract, err := NewContractId(id, version)
		if err != nil {
			return nil, err
		}
		stabilityName, err := stringOf(entry[2], path+".stability")
		if err != nil {
			return nil, err
		}
		stability, err := ParseContractStability(stabilityName)
		if err != nil {
			return nil, err
		}
		contracts = append(contracts, ContractManifestEntry{Contract: contract, Stability: stability})
	}
	codeValues, err := sequenceOf(fields[3], "$.error_codes")
	if err != nil {
		return nil, err
	}
	codes := make([]ErrorCodeManifestEntry, 0, len(codeValues))
	for index, item := range codeValues {
		path := "$.error_codes[" + uint32String(uint32(index)) + "]"
		entry, err := exactFields(item, []string{"code", "category", "introduced", "stability", "description"}, path)
		if err != nil {
			return nil, err
		}
		code, err := stringOf(entry[0], path+".code")
		if err != nil {
			return nil, err
		}
		categoryText, err := stringOf(entry[1], path+".category")
		if err != nil {
			return nil, err
		}
		category, err := ParseDiagnosticCategory(categoryText)
		if err != nil {
			return nil, err
		}
		introduced, err := stringOf(entry[2], path+".introduced")
		if err != nil {
			return nil, err
		}
		stability, err := stringOf(entry[3], path+".stability")
		if err != nil {
			return nil, err
		}
		if stability != "Stable" {
			return nil, invalid(path+".stability", "unknown error-code stability")
		}
		description, err := stringOf(entry[4], path+".description")
		if err != nil {
			return nil, err
		}
		codes = append(codes, ErrorCodeManifestEntry{
			Code: code, Category: category, Introduced: introduced, Description: description,
		})
	}
	return ValidateRegistryManifest(semanticModel, contracts, codes)
}

// referenceValue builds the {"id","version"} reference record.
func referenceValue(id string, version uint32) core.Value {
	return valueObject(
		core.Entry{Key: "id", Value: core.String(id)},
		core.Entry{Key: "version", Value: integerValue(uint64(version))},
	)
}

// parseContractReference strictly decodes a {"id","version"} reference
// (registry_manifest.rs:284-290).
func parseContractReference(value core.Value, path string) (*ContractId, error) {
	fields, err := exactFields(value, []string{"id", "version"}, path)
	if err != nil {
		return nil, err
	}
	id, err := stringOf(fields[0], path+".id")
	if err != nil {
		return nil, err
	}
	version, err := unsigned32(fields[1], path+".version")
	if err != nil {
		return nil, err
	}
	return NewContractId(id, version)
}

// parseProfileReference strictly decodes a profile reference
// (registry.rs:459-465).
func parseProfileReference(value core.Value, path string) (*ProfileReference, error) {
	fields, err := exactFields(value, []string{"id", "version"}, path)
	if err != nil {
		return nil, err
	}
	id, err := stringOf(fields[0], path+".id")
	if err != nil {
		return nil, err
	}
	version, err := unsigned32(fields[1], path+".version")
	if err != nil {
		return nil, err
	}
	return NewProfileReference(id, version)
}

// valueObject builds an object from entries that are statically unique.
func valueObject(entries ...core.Entry) core.Value {
	object, err := core.NewObject(entries...)
	if err != nil {
		panic("protocol: static object construction cannot fail")
	}
	return object
}
