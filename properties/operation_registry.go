package properties

import (
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the Java Properties operation discovery for both
// exact source profiles (consema-properties operation_registry.rs; RFC
// 0010 §13). Both profiles publish the identical frozen five-operation
// surface.

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

// The four argument kinds used by the Properties surface.
const (
	// OperationArgumentPortableValue is a PortableValue argument.
	OperationArgumentPortableValue OperationArgumentKind = "PortableValue"
	// OperationArgumentPlacement is an association placement argument.
	OperationArgumentPlacement OperationArgumentKind = "Placement"
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
	ID *protocol.FormatOperationId
	// TargetRole is the versioned target role identity.
	TargetRole string
	// Arguments are the required arguments.
	Arguments []OperationArgumentDescriptor
	// Support is the operation support classification.
	Support OperationSupport
}

// FormatOperationRegistry is the validated operation registry for one
// exact Java Properties profile (operation_registry.rs:9-14).
type FormatOperationRegistry struct {
	profile    document.ProfileId
	operations []OperationDescriptor
}

// Profile returns the owning profile.
func (r FormatOperationRegistry) Profile() document.ProfileId { return r.profile }

// Operations returns the ordered validated operation descriptors. The
// returned slice is a copy.
func (r FormatOperationRegistry) Operations() []OperationDescriptor {
	return append([]OperationDescriptor(nil), r.operations...)
}

// NewFormatOperationRegistry returns the validated operation registry for
// one exact Java Properties profile. The frozen surface has the five
// independently validated operations of RFC 0010 §13; both profiles share
// the same registry.
func NewFormatOperationRegistry(profile PropertiesProfile) FormatOperationRegistry {
	return FormatOperationRegistry{
		profile: profile.ID(),
		operations: []OperationDescriptor{
			{
				ID:         protocol.NewFormatOperationId("java-properties.edit.insert-property", 1),
				TargetRole: "java-properties.document@1",
				Arguments: []OperationArgumentDescriptor{
					{Name: "key", Kind: OperationArgumentPortableValue},
					{Name: "value", Kind: OperationArgumentPortableValue},
					{Name: "placement", Kind: OperationArgumentPlacement},
				},
				Support: OperationSupportSupported,
			},
			{
				ID:         protocol.NewFormatOperationId("java-properties.edit.remove-property", 1),
				TargetRole: "java-properties.property@1",
				Support:    OperationSupportSupported,
			},
			{
				ID:         protocol.NewFormatOperationId("java-properties.edit.rename-property", 1),
				TargetRole: "java-properties.property@1",
				Arguments: []OperationArgumentDescriptor{
					{Name: "key", Kind: OperationArgumentPortableValue},
				},
				Support: OperationSupportSupported,
			},
			{
				ID:         protocol.NewFormatOperationId("java-properties.edit.replace-literal-value", 1),
				TargetRole: "java-properties.property@1",
				Arguments: []OperationArgumentDescriptor{
					{Name: "literal", Kind: OperationArgumentExactBytes},
				},
				Support: OperationSupportSupported,
			},
			{
				ID:         protocol.NewFormatOperationId("java-properties.edit.replace-semantic-value", 1),
				TargetRole: "java-properties.property@1",
				Arguments: []OperationArgumentDescriptor{
					{Name: "value", Kind: OperationArgumentPortableValue},
				},
				Support: OperationSupportSupported,
			},
		},
	}
}
