package consema

// This file implements the additive facade registry surface
// (https://github.com/consema/consema-rs/blob/main/consema/src/lib.rs,
// inline `registry` module — G077, adversarial audit 2026-08-14: the bare
// "consema-rs/consema/src/lib.rs `registry` module" path does not resolve
// inside this checkout and "registry.rs" does not exist as a separate
// file; RFC 0015 §6.2; plan §2.2
// G1.4): the unified enumeration of format families, profiles, query
// domains, and per-profile operation registries, plus the single parse
// entry by profile id.
//
// Derivation discipline (Rust facade precedent): the capability inventory
// is the declared Feature-Complete Manifest capability set (roadmap §15.7
// line 1451 — "Go 以该 manifest 为起点"; G114 line re-verification
// 2026-08-13), and everything the Go packages can
// derive is derived from them. The family and profile ids come from the
// implementing family packages (JsonProfile.ID(), TomlProfile.ID(),
// FormatFamily(), ...); the operation registries come from the family
// registries (json.FormatOperationRegistryFor,
// toml.NewFormatOperationRegistry, ...); the query domains come from the
// protocol package's frozen domain constructors. All eight families are
// implemented (0.15.0-0.18.0) — the inventory entries are derived from the
// backend facts with drift-guard tests asserting equality, so a backend
// change fails this package's own tests (Rust facade `registry` tests
// precedent; G056, adversarial audit 2026-08-13 — the
// "not-yet-implemented families" wording was stale).

import (
	"context"
	"fmt"
	"sort"

	"consema.dev/consema/document"
	hclpkg "consema.dev/consema/hcl"
	"consema.dev/consema/ini"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/plist"
	"consema.dev/consema/properties"
	"consema.dev/consema/protocol"
	"consema.dev/consema/toml"
	xmlpkg "consema.dev/consema/xml"
	yamlpkg "consema.dev/consema/yaml"
)

// FormatProfile is one profile together with the format family that
// publishes it (the inline `registry` module of
// https://github.com/consema/consema-rs/blob/main/consema/src/lib.rs — G077,
// adversarial audit 2026-08-14: "registry.rs" was a phantom bare-file
// reference; the module is inline in lib.rs).
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
// exact profile (RFC 0015 §6.2 `operations`); ok is false only for
// profile ids outside the sixteen-profile facade surface. Every registry
// is derived from the family registries of the implementing packages and
// never re-declared here.
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
	case "yaml.1.2-core":
		return yamlOperationRegistry(yamlpkg.NewFormatOperationRegistry(yamlpkg.Yaml12CoreV1)), true
	case "yaml.1.1-compat":
		return yamlOperationRegistry(yamlpkg.NewFormatOperationRegistry(yamlpkg.Yaml11CompatV1)), true
	case "ini.portable":
		return iniOperationRegistry(ini.NewFormatOperationRegistry(ini.PortableV1)), true
	case "ini.windows":
		return iniOperationRegistry(ini.NewFormatOperationRegistry(ini.WindowsV1)), true
	case "ini.python-configparser":
		return iniOperationRegistry(ini.NewFormatOperationRegistry(ini.PythonConfigParserV1)), true
	case "java-properties.reader":
		return propertiesOperationRegistry(properties.NewFormatOperationRegistry(properties.PropertiesReaderV1)), true
	case "java-properties.latin1":
		return propertiesOperationRegistry(properties.NewFormatOperationRegistry(properties.PropertiesLatin1V1)), true
	case "xml.1.0-safe":
		return xmlOperationRegistry(xmlpkg.FormatOperationRegistryFor(xmlpkg.XmlProfileSafeV1)), true
	case "plist.xml":
		return plistOperationRegistry(plist.FormatOperationRegistryFor(plist.PlistProfileXmlV1)), true
	case "plist.binary":
		return plistOperationRegistry(plist.FormatOperationRegistryFor(plist.PlistProfileBinaryV1)), true
	case "hcl.native":
		return hclOperationRegistry(hclpkg.FormatOperationRegistryFor(hclpkg.HclProfileNativeV1)), true
	case "hcl.tfvars":
		return hclOperationRegistry(hclpkg.FormatOperationRegistryFor(hclpkg.HclProfileTfvarsV1)), true
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

// yamlOperationRegistry derives the root registry view from the YAML
// family registry without re-declaring the operation surface. The YAML
// argument descriptors carry no required flag; every argument of the
// frozen YAML surface is required.
func yamlOperationRegistry(registry yamlpkg.FormatOperationRegistry) *OperationRegistry {
	descriptors := make([]OperationDescriptor, 0, 8)
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

// iniOperationRegistry derives the root registry view from the INI family
// registry without re-declaring the operation surface. The INI argument
// descriptors carry no required flag; every argument of the frozen INI
// surface is required.
func iniOperationRegistry(registry ini.FormatOperationRegistry) *OperationRegistry {
	descriptors := make([]OperationDescriptor, 0, 8)
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

// propertiesOperationRegistry derives the root registry view from the
// Java Properties family registry without re-declaring the operation
// surface. The Properties argument descriptors carry no required flag;
// every argument of the frozen surface is required.
func propertiesOperationRegistry(registry properties.FormatOperationRegistry) *OperationRegistry {
	descriptors := make([]OperationDescriptor, 0, 5)
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

// xmlOperationRegistry derives the root registry view from the XML family
// registry without re-declaring the operation surface.
func xmlOperationRegistry(registry *xmlpkg.FormatOperationRegistry) *OperationRegistry {
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

// plistOperationRegistry derives the root registry view from the plist
// family registry without re-declaring the operation surface.
func plistOperationRegistry(registry *plist.FormatOperationRegistry) *OperationRegistry {
	descriptors := make([]OperationDescriptor, 0, 6)
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

// hclOperationRegistry derives the root registry view from the HCL family
// registry without re-declaring the operation surface.
func hclOperationRegistry(registry *hclpkg.FormatOperationRegistry) *OperationRegistry {
	descriptors := make([]OperationDescriptor, 0, 6)
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

// ParseDocument parses one snapshot under an exact profile id through the
// single facade parse entry (registry.rs parse_document; RFC 0015 §7.1
// cli.parse-facts@1). The per-format encoding selection and limits use
// the frozen profile defaults; the properties reader profile uses an
// explicit UTF-8 selection because its contract has no profile default.
// Unknown profile ids fail with the typed ProfileError carrying the same
// code as the Rust facade's unknown-profile failure.
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
	case "yaml.1.2-core":
		return parseYAMLResult(source, yamlpkg.Yaml12CoreV1)
	case "yaml.1.1-compat":
		return parseYAMLResult(source, yamlpkg.Yaml11CompatV1)
	case "ini.portable":
		return parseINIResult(source, ini.PortableV1)
	case "ini.windows":
		return parseINIResult(source, ini.WindowsV1)
	case "ini.python-configparser":
		return parseINIResult(source, ini.PythonConfigParserV1)
	case "java-properties.reader":
		document, failure := ParseProperties(source, properties.PropertiesReaderV1,
			properties.ReaderEncodingSelection(document.Utf8Encoding()),
			properties.DefaultPropertiesParseLimits())
		if failure != nil {
			return nil, failure
		}
		return document, nil
	case "java-properties.latin1":
		document, failure := ParseProperties(source, properties.PropertiesLatin1V1,
			properties.Latin1EncodingSelection(), properties.DefaultPropertiesParseLimits())
		if failure != nil {
			return nil, failure
		}
		return document, nil
	case "xml.1.0-safe":
		document, failure := ParseXML(ctx, source, xmlpkg.XmlProfileSafeV1,
			xmlpkg.XmlEncodingProfileDefault(), xmlpkg.DefaultXmlParseLimits())
		if failure != nil {
			return nil, failure
		}
		return document, nil
	case "plist.xml":
		return parsePlistResult(source, plist.PlistProfileXmlV1)
	case "plist.binary":
		return parsePlistResult(source, plist.PlistProfileBinaryV1)
	case "hcl.native":
		document, failure := ParseHCL(ctx, source, hclpkg.HclProfileNativeV1,
			hclpkg.HclEncodingSelectionProfileDefault(), hclpkg.DefaultHclParseLimits())
		if failure != nil {
			return nil, failure
		}
		return document, nil
	case "hcl.tfvars":
		document, failure := ParseHCL(ctx, source, hclpkg.HclProfileTfvarsV1,
			hclpkg.HclEncodingSelectionProfileDefault(), hclpkg.DefaultHclParseLimits())
		if failure != nil {
			return nil, failure
		}
		return document, nil
	default:
		return nil, &ProfileError{Profile: profile}
	}
}

// parseYAMLResult unpacks the typed formation failure so a successful
// parse never leaks a typed-nil failure into the error interface.
func parseYAMLResult(source []byte, profile yamlpkg.YamlProfile) (*Document, error) {
	document, failure := ParseYAML(source, profile, document.DefaultParseLimits())
	if failure != nil {
		return nil, failure
	}
	return document, nil
}

// parseINIResult unpacks the typed formation failure so a successful
// parse never leaks a typed-nil failure into the error interface.
func parseINIResult(source []byte, profile ini.IniProfile) (*Document, error) {
	document, failure := ParseINI(source, profile, ini.IniEncodingProfileDefault(),
		ini.DefaultIniParseLimits())
	if failure != nil {
		return nil, failure
	}
	return document, nil
}

// parsePlistResult unpacks the typed formation failure so a successful
// parse never leaks a typed-nil failure into the error interface.
func parsePlistResult(source []byte, profile plist.PlistProfile) (*Document, error) {
	document, failure := ParsePlist(source, profile, plist.PlistEncodingProfileDefault(),
		plist.DefaultPlistParseLimits())
	if failure != nil {
		return nil, failure
	}
	return document, nil
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
