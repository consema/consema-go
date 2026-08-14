package json

import (
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the JSON-family edit record aliases and the
// format operation registry. The shared edit records (ChangeSet,
// EditPlan, SourceEdit, NodeMapping, AssociationPlacement,
// UntouchedByteProof, LosslessStructuralIndex) live in go/document
// (consema-document change_set.rs, edit_plan.rs, untouched_proof.rs);
// this package aliases them and keeps the format-local names that the
// JSON public surface has used since 0.15.0.

// SourceEdit is one exact source edit between the old and new snapshots
// (document.SourceEdit; consema-document ChangeSet source edits).
type SourceEdit = document.SourceEdit

// NodeMapping is one old-to-new node mapping fact (document.NodeMapping;
// consema-document ChangeSet node mappings).
type NodeMapping = document.NodeMapping

// ChangeSet is the complete old-to-new change facts of one edit commit
// (document.ChangeSet; RFC 0016 §5.3; consema-document ChangeSet).
type ChangeSet = document.ChangeSet

// EditPlan is the fully validated dry-run plan; possessing it does not
// authorize a write (document.EditPlan; RFC 0015 §8.1 read-only
// precedent; consema-document edit_plan.rs).
type EditPlan = document.EditPlan

// FormatOperationRegistry is the validated operation registry for one
// exact JSON-family profile (consema-json operation_registry.rs).
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

// FormatOperationRegistryFor returns the frozen registry for one profile.
// Every profile publishes the identical surface (operation_registry.rs).
func FormatOperationRegistryFor(profile JsonProfile) *FormatOperationRegistry {
	return &FormatOperationRegistry{
		profile:    profile.ID(),
		operations: operationDescriptors(),
	}
}

// Profile returns the owning profile.
func (r *FormatOperationRegistry) Profile() document.ProfileId { return r.profile }

// Operations returns the ordered descriptors. The returned slice is a
// copy.
func (r *FormatOperationRegistry) Operations() []FormatOperationDescriptor {
	return append([]FormatOperationDescriptor(nil), r.operations...)
}

// operationDescriptors builds the frozen eight-operation surface
// (operation_registry.rs).
func operationDescriptors() []FormatOperationDescriptor {
	stringArgument := func(name string) OperationArgumentDescriptor {
		return OperationArgumentDescriptor{Name: name, Kind: "String", Required: true}
	}
	valueArgument := func(name string) OperationArgumentDescriptor {
		return OperationArgumentDescriptor{Name: name, Kind: "PortableValue", Required: true}
	}
	placementArgument := func(name string) OperationArgumentDescriptor {
		return OperationArgumentDescriptor{Name: name, Kind: "Placement", Required: true}
	}
	return []FormatOperationDescriptor{
		{
			operation:  protocol.NewFormatOperationId("json.edit.insert-member", 1),
			targetRole: "json.object@1",
			arguments: []OperationArgumentDescriptor{
				stringArgument("name"), valueArgument("value"), placementArgument("placement"),
			},
			support: "Supported",
		},
		{
			operation:  protocol.NewFormatOperationId("json.edit.remove-member", 1),
			targetRole: "json.object-member@1",
			support:    "Supported",
		},
		{
			operation:  protocol.NewFormatOperationId("json.edit.move-member", 1),
			targetRole: "json.object-member@1",
			arguments:  []OperationArgumentDescriptor{placementArgument("placement")},
			support:    "Supported",
		},
		{
			operation:  protocol.NewFormatOperationId("json.edit.rename-member", 1),
			targetRole: "json.object-member@1",
			arguments:  []OperationArgumentDescriptor{stringArgument("name")},
			support:    "Supported",
		},
		{
			operation:  protocol.NewFormatOperationId("json.edit.insert-array-element", 1),
			targetRole: "json.array@1",
			arguments: []OperationArgumentDescriptor{
				valueArgument("value"), placementArgument("placement"),
			},
			support: "Supported",
		},
		{
			operation:  protocol.NewFormatOperationId("json.edit.remove-array-element", 1),
			targetRole: "json.array-element@1",
			support:    "Supported",
		},
		{
			operation:  protocol.NewFormatOperationId("json.edit.replace-scalar-semantic", 1),
			targetRole: "json.scalar@1",
			arguments: []OperationArgumentDescriptor{
				valueArgument("value"),
				{Name: "representation_policy", Kind: "RepresentationPolicy", Required: true},
			},
			support: "ExistingTypedCapability",
		},
		{
			operation:  protocol.NewFormatOperationId("json.edit.replace-scalar-literal", 1),
			targetRole: "json.scalar@1",
			arguments: []OperationArgumentDescriptor{
				{Name: "literal", Kind: "ExactBytes", Required: true},
			},
			support: "ExistingTypedCapability",
		},
	}
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
