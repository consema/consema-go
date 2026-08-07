package hcl

import (
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the HCL operation discovery for the frozen
// `hcl.native@1` and `hcl.tfvars@1` profiles (RFC 0014 §10).
//
// `hcl.native@1` publishes all six structural operations; `hcl.tfvars@1`
// publishes the four attribute operations only, because the tfvars
// restriction admits no block (RFC 0014 §5, §10).

// FormatOperationRegistry is the validated operation registry for one
// exact HCL profile (consema-hcl operation_registry.rs).
type FormatOperationRegistry struct {
	profile    document.ProfileId
	operations []FormatOperationDescriptor
}

// FormatOperationDescriptor is one versioned format operation descriptor.
type FormatOperationDescriptor struct {
	operation  *protocol.FormatOperationId
	targetRole string
	arguments  []OperationArgumentDescriptor
	support    string
}

// OperationArgumentDescriptor is one operation argument contract.
type OperationArgumentDescriptor struct {
	// Name is the argument name.
	Name string
	// Kind is the argument value kind.
	Kind string
	// Required reports whether the argument is mandatory.
	Required bool
}

// FormatOperationRegistryFor returns the validated operation registry for
// one exact HCL profile (operation_registry.rs).
func FormatOperationRegistryFor(profile HclProfile) *FormatOperationRegistry {
	descriptors := tfvarsDescriptors()
	if profile == HclProfileNativeV1 {
		descriptors = append(descriptors,
			operationDescriptor("hcl.edit.insert-block", "hcl.body",
				[]OperationArgumentDescriptor{
					operationArgument("type", "String"),
					operationArgument("labels", "String"),
					operationArgument("attributes", "PortableValue"),
					operationArgument("placement", "Placement"),
				}),
			operationDescriptor("hcl.edit.remove-block", "hcl.block", nil))
	}
	return &FormatOperationRegistry{
		profile:    profile.ID(),
		operations: descriptors,
	}
}

// Profile returns the owning profile.
func (r *FormatOperationRegistry) Profile() document.ProfileId { return r.profile }

// Operations returns the ordered descriptors. The returned slice is a
// copy.
func (r *FormatOperationRegistry) Operations() []FormatOperationDescriptor {
	return append([]FormatOperationDescriptor(nil), r.operations...)
}

// tfvarsDescriptors builds the attribute-only surface of `hcl.tfvars@1`.
func tfvarsDescriptors() []FormatOperationDescriptor {
	return []FormatOperationDescriptor{
		operationDescriptor("hcl.edit.insert-attribute", "hcl.body",
			[]OperationArgumentDescriptor{
				operationArgument("name", "String"),
				operationArgument("value", "PortableValue"),
				operationArgument("placement", "Placement"),
			}),
		operationDescriptor("hcl.edit.remove-attribute", "hcl.attribute", nil),
		operationDescriptor("hcl.edit.rename-attribute", "hcl.attribute",
			[]OperationArgumentDescriptor{operationArgument("name", "String")}),
		operationDescriptor("hcl.edit.set-attribute-value", "hcl.attribute",
			[]OperationArgumentDescriptor{operationArgument("value", "PortableValue")}),
	}
}

func operationDescriptor(id, targetRole string,
	arguments []OperationArgumentDescriptor) FormatOperationDescriptor {
	return FormatOperationDescriptor{
		operation:  protocol.NewFormatOperationId(id, 1),
		targetRole: targetRole,
		arguments:  arguments,
		support:    "Supported",
	}
}

func operationArgument(name, kind string) OperationArgumentDescriptor {
	return OperationArgumentDescriptor{Name: name, Kind: kind, Required: true}
}

// ID returns the stable operation identifier with its version suffix.
func (d FormatOperationDescriptor) ID() string {
	return d.operation.ID() + "@" + uint64String(uint64(d.operation.Version()))
}

// TargetRole returns the target role identifier.
func (d FormatOperationDescriptor) TargetRole() string { return d.targetRole }

// Arguments returns the ordered argument descriptors. The returned slice
// is a copy.
func (d FormatOperationDescriptor) Arguments() []OperationArgumentDescriptor {
	return append([]OperationArgumentDescriptor(nil), d.arguments...)
}

// Support returns the stable support class: "Supported" or
// "ExistingTypedCapability".
func (d FormatOperationDescriptor) Support() string { return d.support }
