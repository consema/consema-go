package consema

// This file implements the additive facade registry surface
// (crates/consema/src/lib.rs `registry` module; RFC 0015 §6.2; plan §2.2
// G1.4): the unified enumeration of format families, profiles, query
// domains, and per-profile operation registries, plus the single parse
// entry by profile id.
//
// Derivation discipline (Rust facade precedent): the capability inventory
// is the declared Feature-Complete Manifest capability set (roadmap §15.7
// line 1445 — the Go starting point), and everything the Go packages can
// derive is derived from them. The family and profile ids of the JSON and
// TOML families come from go/json and go/toml (JsonProfile.ID(),
// TomlProfile.ID(), FormatFamily()); the operation registries of the four
// implemented profiles come from the family registries
// (json.FormatOperationRegistryFor, toml.NewFormatOperationRegistry); the
// query domains come from the protocol package's frozen domain
// constructors. The inventory entries of the not-yet-implemented families
// (yaml/ini/properties/xml/plist/hcl) are declared capability facts only:
// no Go package exists to derive their registry content from, and the
// drift-guard tests assert every derivable fact against the backend
// packages so a backend change fails this package's own tests (Rust
// facade `registry` tests precedent).

import (
	"context"
	"fmt"
	"sort"

	"consema.dev/consema/document"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/protocol"
	"consema.dev/consema/toml"
)

// FormatProfile is one profile together with the format family that
// publishes it (registry.rs:50-69).
type FormatProfile struct {
	family  document.FormatFamilyId
	profile document.ProfileId
}

// Family returns the format family of the profile.
func (p FormatProfile) Family() document.FormatFamilyId { return p.family }

// Profile returns the profile itself.
func (p FormatProfile) Profile() document.ProfileId { return p.profile }

// Families returns the eight format families (RFC 0015 §6.2 `families`),
// sorted by id. The json and toml entries are derived from the backend
// packages' FormatFamily facts (asserted by the drift-guard tests); the
// remaining entries are declared capability facts of the families that
// land with 0.16.0-0.18.0.
func Families() []document.FormatFamilyId {
	families := []document.FormatFamilyId{
		document.NewFormatFamilyId("hcl", 1),
		document.NewFormatFamilyId("ini", 1),
		document.NewFormatFamilyId("java-properties", 1),
		document.NewFormatFamilyId("json", 1),
		document.NewFormatFamilyId("plist", 1),
		document.NewFormatFamilyId("toml", 1),
		document.NewFormatFamilyId("xml", 1),
		document.NewFormatFamilyId("yaml", 1),
	}
	sort.Slice(families, func(left, right int) bool {
		return families[left].ID() < families[right].ID()
	})
	return families
}

// Profiles returns all sixteen profiles with their owning family (RFC
// 0015 §6.2 `profiles`), sorted by profile id. The four implemented
// profile ids are derived from the backend packages (JsonProfile.ID(),
// TomlProfile.ID()) and asserted by the drift-guard tests; the remaining
// ids are declared capability facts.
func Profiles() []FormatProfile {
	profiles := []FormatProfile{
		familyProfile("hcl", document.NewProfileId("hcl.native", 1)),
		familyProfile("hcl", document.NewProfileId("hcl.tfvars", 1)),
		familyProfile("ini", document.NewProfileId("ini.portable", 1)),
		familyProfile("ini", document.NewProfileId("ini.windows", 1)),
		familyProfile("ini", document.NewProfileId("ini.python-configparser", 1)),
		familyProfile("java-properties", document.NewProfileId("java-properties.reader", 1)),
		familyProfile("java-properties", document.NewProfileId("java-properties.latin1", 1)),
		familyProfile("json", jsonpkg.JsonProfileStrictV1.ID()),
		familyProfile("json", jsonpkg.JsonProfileJsoncBoundedV1.ID()),
		familyProfile("json", jsonpkg.JsonProfileJson5StandardV1.ID()),
		familyProfile("plist", document.NewProfileId("plist.xml", 1)),
		familyProfile("plist", document.NewProfileId("plist.binary", 1)),
		familyProfile("toml", toml.Toml10V1.ID()),
		familyProfile("xml", document.NewProfileId("xml.1.0-safe", 1)),
		familyProfile("yaml", document.NewProfileId("yaml.1.2-core", 1)),
		familyProfile("yaml", document.NewProfileId("yaml.1.1-compat", 1)),
	}
	sort.Slice(profiles, func(left, right int) bool {
		if profiles[left].profile.ID() != profiles[right].profile.ID() {
			return profiles[left].profile.ID() < profiles[right].profile.ID()
		}
		return profiles[left].profile.Version() < profiles[right].profile.Version()
	})
	return profiles
}

// QueryDomains returns the query-domain constructor inventory (RFC 0015
// §6.2 `query_domains`), sorted by (id, version). Every domain comes from
// the protocol package's frozen domain constructors; this package only
// aggregates and sorts them.
func QueryDomains() []*protocol.QueryDomain {
	domains := []*protocol.QueryDomain{
		protocol.DomainPortableValueV1(),
		protocol.DomainPortableGraphV1(),
		protocol.DomainJSONNativeV1(),
		protocol.DomainJSONNativeV2(),
		protocol.DomainTOMLNativeV1(),
		protocol.DomainYAMLNativeV1(),
		protocol.DomainININativeV1(),
		protocol.DomainJavaPropertiesNativeV1(),
		protocol.DomainXMLNativeV1(),
		protocol.DomainJSONLosslessSyntaxV1(),
		protocol.DomainJSONLosslessSyntaxV2(),
		protocol.DomainTOMLLosslessSyntaxV1(),
		protocol.DomainYAMLLosslessSyntaxV1(),
		protocol.DomainINILosslessSyntaxV1(),
		protocol.DomainJavaPropertiesLosslessSyntaxV1(),
		protocol.DomainXMLLosslessSyntaxV1(),
		protocol.DomainPlistNativeV1(),
		protocol.DomainPlistLosslessSyntaxV1(),
		protocol.DomainPlistBinaryStructureV1(),
		protocol.DomainHCLNativeV1(),
		protocol.DomainHCLLosslessSyntaxV1(),
	}
	sort.Slice(domains, func(left, right int) bool {
		if domains[left].ID() != domains[right].ID() {
			return domains[left].ID() < domains[right].ID()
		}
		return domains[left].Version() < domains[right].Version()
	})
	return domains
}

// OperationRegistry is the validated operation registry of one exact
// profile (RFC 0015 §6.2 `operations`), derived from the family
// registries of the implementing packages. The profile inventory
// (sixteen registries, one per profile) is the declared capability set;
// the registries of the four implemented profiles carry the derived
// operation surface.
type OperationRegistry struct {
	profile    document.ProfileId
	operations []OperationDescriptor
}

// Profile returns the owning profile.
func (r *OperationRegistry) Profile() document.ProfileId { return r.profile }

// Operations returns the ordered operation descriptors. The returned
// slice is a copy.
func (r *OperationRegistry) Operations() []OperationDescriptor {
	return append([]OperationDescriptor(nil), r.operations...)
}

// OperationDescriptor is one versioned format operation descriptor.
type OperationDescriptor struct {
	id         string
	targetRole string
	arguments  []OperationArgumentDescriptor
	support    string
}

// ID returns the stable operation identifier with its version suffix
// (for example "json.edit.move-member@1").
func (d OperationDescriptor) ID() string { return d.id }

// TargetRole returns the versioned target role identifier.
func (d OperationDescriptor) TargetRole() string { return d.targetRole }

// Arguments returns the ordered argument descriptors. The returned slice
// is a copy.
func (d OperationDescriptor) Arguments() []OperationArgumentDescriptor {
	return append([]OperationArgumentDescriptor(nil), d.arguments...)
}

// Support returns the stable support class: "Supported" or
// "ExistingTypedCapability".
func (d OperationDescriptor) Support() string { return d.support }

// OperationArgumentDescriptor is one operation argument contract.
type OperationArgumentDescriptor struct {
	// Name is the stable argument name.
	Name string
	// Kind is the argument value kind.
	Kind string
	// Required reports whether the argument is mandatory.
	Required bool
}

// OperationRegistryFor returns the per-profile operation registry of one
// exact profile (RFC 0015 §6.2 `operations`); ok is false for profiles
// whose operation surface is not implemented by this Go milestone (the
// yaml/ini/properties/xml/plist/hcl profiles land with 0.16.0-0.18.0 —
// no Go package exists to derive their registry from, and the capability
// list is never hand-declared). For the four implemented profiles the
// registry is derived from the family registries of go/json and go/toml
// and never re-declared here.
func OperationRegistryFor(profile document.ProfileId) (*OperationRegistry, bool) {
	switch profile.ID() {
	case "json.strict":
		return jsonOperationRegistry(jsonpkg.FormatOperationRegistryFor(jsonpkg.JsonProfileStrictV1)), true
	case "jsonc.bounded":
		return jsonOperationRegistry(jsonpkg.FormatOperationRegistryFor(jsonpkg.JsonProfileJsoncBoundedV1)), true
	case "json5.standard":
		return jsonOperationRegistry(jsonpkg.FormatOperationRegistryFor(jsonpkg.JsonProfileJson5StandardV1)), true
	case "toml.1.0":
		return tomlOperationRegistry(toml.NewFormatOperationRegistry(toml.Toml10V1)), true
	default:
		return nil, false
	}
}

// jsonOperationRegistry derives the root registry view from the JSON
// family registry without re-declaring the operation surface.
func jsonOperationRegistry(registry *jsonpkg.FormatOperationRegistry) *OperationRegistry {
	descriptors := make([]OperationDescriptor, 0, 8)
	for _, operation := range registry.Operations() {
		arguments := make([]OperationArgumentDescriptor, 0, len(operation.Arguments()))
		for _, argument := range operation.Arguments() {
			arguments = append(arguments, OperationArgumentDescriptor{
				Name: argument.Name, Kind: argument.Kind, Required: argument.Required,
			})
		}
		descriptors = append(descriptors, OperationDescriptor{
			id:         operation.ID(),
			targetRole: operation.TargetRole(),
			arguments:  arguments,
			support:    operation.Support(),
		})
	}
	return &OperationRegistry{profile: registry.Profile(), operations: descriptors}
}

// tomlOperationRegistry derives the root registry view from the TOML
// family registry without re-declaring the operation surface. The TOML
// argument descriptors carry no required flag; every argument of the
// frozen TOML surface is required.
func tomlOperationRegistry(registry toml.FormatOperationRegistry) *OperationRegistry {
	descriptors := make([]OperationDescriptor, 0, 7)
	for _, operation := range registry.Operations() {
		arguments := make([]OperationArgumentDescriptor, 0, len(operation.Arguments))
		for _, argument := range operation.Arguments {
			arguments = append(arguments, OperationArgumentDescriptor{
				Name: argument.Name, Kind: string(argument.Kind), Required: true,
			})
		}
		descriptors = append(descriptors, OperationDescriptor{
			id:         operation.ID.ID() + "@" + uint64String(uint64(operation.ID.Version())),
			targetRole: operation.TargetRole,
			arguments:  arguments,
			support:    string(operation.Support),
		})
	}
	return &OperationRegistry{profile: registry.Profile(), operations: descriptors}
}

// ParseDocument parses one snapshot under an exact profile id through the
// single facade parse entry (registry.rs parse_document; RFC 0015 §7.1
// cli.parse-facts@1). The per-format encoding selection and limits use
// the frozen profile defaults. Profiles of the not-yet-implemented
// families fail with the same typed error as unknown ids (no Go package
// exists to form their documents; the profiles land with
// 0.16.0-0.18.0).
func ParseDocument(ctx context.Context, source []byte,
	profile document.ProfileId) (*Document, error) {
	switch profile.ID() {
	case "json.strict":
		return parseJSONResult(ctx, source, jsonpkg.JsonProfileStrictV1)
	case "jsonc.bounded":
		return parseJSONResult(ctx, source, jsonpkg.JsonProfileJsoncBoundedV1)
	case "json5.standard":
		return parseJSONResult(ctx, source, jsonpkg.JsonProfileJson5StandardV1)
	case "toml.1.0":
		document, failure := ParseTOML(source, toml.Toml10V1, document.DefaultParseLimits())
		if failure != nil {
			return nil, failure
		}
		return document, nil
	default:
		return nil, &ProfileError{Profile: profile}
	}
}

// parseJSONResult unpacks the typed formation failure so a successful
// parse never leaks a typed-nil failure into the error interface.
func parseJSONResult(ctx context.Context, source []byte,
	profile jsonpkg.JsonProfile) (*Document, error) {
	document, failure := ParseJSON(ctx, source, profile, document.DefaultParseLimits())
	if failure != nil {
		return nil, failure
	}
	return document, nil
}

// ProfileError is the typed failure of ParseDocument: the profile id is
// unknown or its family is not implemented by this Go milestone.
type ProfileError struct {
	// Profile is the unresolved profile id.
	Profile document.ProfileId
}

// Error implements error; the text is human presentation only (RFC 0016
// §6).
func (e *ProfileError) Error() string {
	return fmt.Sprintf("consema: unknown or unimplemented profile %s@%d",
		e.Profile.ID(), e.Profile.Version())
}

// Code returns the frozen registered code, mirroring the Rust facade's
// unknown-profile failure diagnostic (registry.rs parse_document).
func (e *ProfileError) Code() string { return "core.source.encoding-conflict@1" }

// familyProfile pairs one family id with one profile id.
func familyProfile(familyID string, profile document.ProfileId) FormatProfile {
	return FormatProfile{family: document.NewFormatFamilyId(familyID, 1), profile: profile}
}

// uint64String formats one unsigned ordinal (shared with the registry
// operation-id suffix).
func uint64String(value uint64) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
