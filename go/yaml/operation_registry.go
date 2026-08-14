package yaml

import (
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// OperationSupport is the closed operation support classification
// (consema-document operation_registry.rs).
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

// The seven frozen argument kinds.
const (
	// OperationArgumentString is a String argument.
	OperationArgumentString OperationArgumentKind = "String"
	// OperationArgumentPortableValue is a PortableValue argument.
	OperationArgumentPortableValue OperationArgumentKind = "PortableValue"
	// OperationArgumentPlacement is an association placement argument.
	OperationArgumentPlacement OperationArgumentKind = "Placement"
	// OperationArgumentRepresentationPolicy is a representation policy
	// argument.
	OperationArgumentRepresentationPolicy OperationArgumentKind = "RepresentationPolicy"
	// OperationArgumentExactBytes is an exact literal bytes argument.
	OperationArgumentExactBytes OperationArgumentKind = "ExactBytes"
	// OperationArgumentNodeRef is a snapshot-bound node reference
	// argument.
	OperationArgumentNodeRef OperationArgumentKind = "NodeRef"
)

// OperationArgumentDescriptor describes one required operation argument
// (consema-document operation_registry.rs).
type OperationArgumentDescriptor struct {
	// Name is the stable argument name.
	Name string
	// Kind is the argument kind.
	Kind OperationArgumentKind
}

// OperationDescriptor is one validated format operation descriptor
// (consema-document operation_registry.rs).
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
// exact YAML profile (consema-yaml operation_registry.rs).
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
// one exact YAML profile (consema-yaml operation_registry.rs). The
// frozen surface has eight operations: the six mandatory structural
// operations (insert-alias, insert-mapping-entry, insert-sequence-element,
// remove-mapping-entry, remove-sequence-element, rename-anchor) and the
// two existing typed scalar capabilities (replace-scalar-semantic,
// replace-scalar-literal). Both profiles share the same registry.
func NewFormatOperationRegistry(profile YamlProfile) FormatOperationRegistry {
	return FormatOperationRegistry{
		profile: profile.ID(),
		operations: []OperationDescriptor{
			{
				ID:         *protocol.NewFormatOperationId("yaml.edit.insert-alias", 1),
				TargetRole: "yaml.sequence@1",
				Arguments: []OperationArgumentDescriptor{
					{Name: "anchor", Kind: OperationArgumentNodeRef},
					{Name: "placement", Kind: OperationArgumentPlacement},
				},
				Support: OperationSupportSupported,
			},
			{
				ID:         *protocol.NewFormatOperationId("yaml.edit.insert-mapping-entry", 1),
				TargetRole: "yaml.mapping@1",
				Arguments: []OperationArgumentDescriptor{
					{Name: "key", Kind: OperationArgumentPortableValue},
					{Name: "value", Kind: OperationArgumentPortableValue},
					{Name: "placement", Kind: OperationArgumentPlacement},
				},
				Support: OperationSupportSupported,
			},
			{
				ID:         *protocol.NewFormatOperationId("yaml.edit.insert-sequence-element", 1),
				TargetRole: "yaml.sequence@1",
				Arguments: []OperationArgumentDescriptor{
					{Name: "value", Kind: OperationArgumentPortableValue},
					{Name: "placement", Kind: OperationArgumentPlacement},
				},
				Support: OperationSupportSupported,
			},
			{
				ID:         *protocol.NewFormatOperationId("yaml.edit.remove-mapping-entry", 1),
				TargetRole: "yaml.mapping-entry@1",
				Support:    OperationSupportSupported,
			},
			{
				ID:         *protocol.NewFormatOperationId("yaml.edit.remove-sequence-element", 1),
				TargetRole: "yaml.sequence-element@1",
				Support:    OperationSupportSupported,
			},
			{
				ID:         *protocol.NewFormatOperationId("yaml.edit.rename-anchor", 1),
				TargetRole: "yaml.anchor-definition@1",
				Arguments: []OperationArgumentDescriptor{
					{Name: "name", Kind: OperationArgumentString},
				},
				Support: OperationSupportSupported,
			},
			{
				ID:         *protocol.NewFormatOperationId("yaml.edit.replace-scalar-literal", 1),
				TargetRole: "yaml.scalar@1",
				Arguments: []OperationArgumentDescriptor{
					{Name: "literal", Kind: OperationArgumentExactBytes},
				},
				Support: OperationSupportExistingTypedCapability,
			},
			{
				ID:         *protocol.NewFormatOperationId("yaml.edit.replace-scalar-semantic", 1),
				TargetRole: "yaml.scalar@1",
				Arguments: []OperationArgumentDescriptor{
					{Name: "value", Kind: OperationArgumentPortableValue},
					{Name: "representation_policy", Kind: OperationArgumentRepresentationPolicy},
				},
				Support: OperationSupportExistingTypedCapability,
			},
		},
	}
}
