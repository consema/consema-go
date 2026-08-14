package ini

import (
	"sort"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the INI operation discovery for every explicit
// family profile (consema-ini operation_registry.rs). The frozen surface
// has eight operations shared by all three profiles: the six mandatory
// structural operations and the two existing typed scalar capabilities.

// OperationSupport is the closed operation support classification.
type OperationSupport string

// The two frozen support levels.
const (
	// OperationSupportSupported is a mandatory structural operation.
	OperationSupportSupported OperationSupport = "Supported"
	// OperationSupportExistingTypedCapability is an existing typed
	// capability exposed through the operation registry.
	OperationSupportExistingTypedCapability OperationSupport = "ExistingTypedCapability"
)

// OperationArgumentKind is the closed operation argument kind.
type OperationArgumentKind string

// The four frozen argument kinds used by the INI surface.
const (
	// OperationArgumentString is a String argument.
	OperationArgumentString OperationArgumentKind = "String"
	// OperationArgumentPlacement is an association placement argument.
	OperationArgumentPlacement OperationArgumentKind = "Placement"
	// OperationArgumentRepresentationPolicy is a representation policy
	// argument.
	OperationArgumentRepresentationPolicy OperationArgumentKind = "RepresentationPolicy"
	// OperationArgumentExactBytes is an exact literal bytes argument.
	OperationArgumentExactBytes OperationArgumentKind = "ExactBytes"
)

// OperationArgumentDescriptor describes one required operation argument.
type OperationArgumentDescriptor struct {
	// Name is the stable argument name.
	Name string
	// Kind is the argument kind.
	Kind OperationArgumentKind
}

// OperationDescriptor is one validated format operation descriptor.
type OperationDescriptor struct {
	// ID is the stable operation identity.
	ID protocol.FormatOperationId
	// TargetRole is the versioned target role identity.
	TargetRole string
	// Arguments are the required arguments.
	Arguments []OperationArgumentDescriptor
	// Support is the operation support classification.
	Support OperationSupport
}

// FormatOperationRegistry is the validated operation registry for one
// exact INI profile (consema-ini operation_registry.rs).
type FormatOperationRegistry struct {
	profile    document.ProfileId
	operations []OperationDescriptor
}

// Profile returns the owning profile.
func (r FormatOperationRegistry) Profile() document.ProfileId { return r.profile }

// Operations returns the ordered validated operation descriptors.
func (r FormatOperationRegistry) Operations() []OperationDescriptor {
	return append([]OperationDescriptor(nil), r.operations...)
}

// NewFormatOperationRegistry returns the validated operation registry for
// one exact INI profile. Every profile publishes the identical frozen
// eight-operation surface: insert-section, remove-section, rename-section,
// insert-entry, remove-entry, rename-entry (six mandatory structural
// operations) and replace-semantic-value, replace-literal-value (two
// existing typed capabilities). Descriptors are published in the canonical
// operation-ID order (consema-document operation_registry.rs).
func NewFormatOperationRegistry(profile IniProfile) FormatOperationRegistry {
	operations := []OperationDescriptor{
		{
			ID:         *protocol.NewFormatOperationId("ini.edit.insert-section", 1),
			TargetRole: "ini.document@1",
			Arguments: []OperationArgumentDescriptor{
				{Name: "name", Kind: OperationArgumentString},
				{Name: "placement", Kind: OperationArgumentPlacement},
			},
			Support: OperationSupportSupported,
		},
		{
			ID:         *protocol.NewFormatOperationId("ini.edit.remove-section", 1),
			TargetRole: "ini.section@1",
			Support:    OperationSupportSupported,
		},
		{
			ID:         *protocol.NewFormatOperationId("ini.edit.rename-section", 1),
			TargetRole: "ini.section@1",
			Arguments: []OperationArgumentDescriptor{
				{Name: "name", Kind: OperationArgumentString},
			},
			Support: OperationSupportSupported,
		},
		{
			ID:         *protocol.NewFormatOperationId("ini.edit.insert-entry", 1),
			TargetRole: "ini.section@1",
			Arguments: []OperationArgumentDescriptor{
				{Name: "key", Kind: OperationArgumentString},
				{Name: "value", Kind: OperationArgumentString},
				{Name: "placement", Kind: OperationArgumentPlacement},
			},
			Support: OperationSupportSupported,
		},
		{
			ID:         *protocol.NewFormatOperationId("ini.edit.remove-entry", 1),
			TargetRole: "ini.entry@1",
			Support:    OperationSupportSupported,
		},
		{
			ID:         *protocol.NewFormatOperationId("ini.edit.rename-entry", 1),
			TargetRole: "ini.entry@1",
			Arguments: []OperationArgumentDescriptor{
				{Name: "key", Kind: OperationArgumentString},
			},
			Support: OperationSupportSupported,
		},
		{
			ID:         *protocol.NewFormatOperationId("ini.edit.replace-semantic-value", 1),
			TargetRole: "ini.entry@1",
			Arguments: []OperationArgumentDescriptor{
				{Name: "value", Kind: OperationArgumentString},
				{Name: "representation_policy", Kind: OperationArgumentRepresentationPolicy},
			},
			Support: OperationSupportExistingTypedCapability,
		},
		{
			ID:         *protocol.NewFormatOperationId("ini.edit.replace-literal-value", 1),
			TargetRole: "ini.entry@1",
			Arguments: []OperationArgumentDescriptor{
				{Name: "literal", Kind: OperationArgumentExactBytes},
			},
			Support: OperationSupportExistingTypedCapability,
		},
	}
	sort.SliceStable(operations, func(i, j int) bool {
		if operations[i].ID.ID() != operations[j].ID.ID() {
			return operations[i].ID.ID() < operations[j].ID.ID()
		}
		return operations[i].ID.Version() < operations[j].ID.Version()
	})
	return FormatOperationRegistry{profile: profile.ID(), operations: operations}
}
