package xml

// This file implements the XML edit record aliases and the format
// operation registry. The shared edit records (ChangeSet, EditPlan,
// SourceEdit, NodeMapping, AssociationPlacement, UntouchedByteProof,
// LosslessStructuralIndex) live in go/document (consema-document
// change_set.rs, edit_plan.rs, untouched_proof.rs); this package aliases
// them and keeps the format-local names of the XML public surface.

import (
	"sort"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

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

// UntouchedByteProof is the immutable evidence for every byte outside one
// exact replacement plan (document.UntouchedByteProof).
type UntouchedByteProof = document.UntouchedByteProof

// FormatOperationRegistry is the validated operation registry for one
// exact XML profile (operation_registry.rs:11-14).
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
// Every profile publishes the identical surface
// (operation_registry.rs:11-14); the descriptors are ordered by their
// stable operation identifier, exactly as the reference registry sorts
// them.
func FormatOperationRegistryFor(profile XmlProfile) *FormatOperationRegistry {
	descriptors := operationDescriptors()
	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].operation.ID() < descriptors[j].operation.ID()
	})
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

// operationDescriptors builds the frozen eight-operation surface
// (operation_registry.rs:16-75).
func operationDescriptors() []FormatOperationDescriptor {
	stringArgument := func(name string) OperationArgumentDescriptor {
		return OperationArgumentDescriptor{Name: name, Kind: "String", Required: true}
	}
	placementArgument := func(name string) OperationArgumentDescriptor {
		return OperationArgumentDescriptor{Name: name, Kind: "Placement", Required: true}
	}
	return []FormatOperationDescriptor{
		{
			operation:  protocol.NewFormatOperationId("xml.edit.replace-text", 1),
			targetRole: "xml.text@1",
			arguments:  []OperationArgumentDescriptor{stringArgument("text")},
			support:    "Supported",
		},
		{
			operation:  protocol.NewFormatOperationId("xml.edit.insert-attribute", 1),
			targetRole: "xml.element@1",
			arguments: []OperationArgumentDescriptor{
				stringArgument("name"), stringArgument("value"), placementArgument("placement"),
			},
			support: "Supported",
		},
		{
			operation:  protocol.NewFormatOperationId("xml.edit.remove-attribute", 1),
			targetRole: "xml.attribute@1",
			support:    "Supported",
		},
		{
			operation:  protocol.NewFormatOperationId("xml.edit.rename-attribute", 1),
			targetRole: "xml.attribute@1",
			arguments:  []OperationArgumentDescriptor{stringArgument("name")},
			support:    "Supported",
		},
		{
			operation:  protocol.NewFormatOperationId("xml.edit.set-attribute-value", 1),
			targetRole: "xml.attribute@1",
			arguments:  []OperationArgumentDescriptor{stringArgument("value")},
			support:    "Supported",
		},
		{
			operation:  protocol.NewFormatOperationId("xml.edit.insert-element", 1),
			targetRole: "xml.element@1",
			arguments: []OperationArgumentDescriptor{
				stringArgument("name"), stringArgument("content"), placementArgument("placement"),
			},
			support: "Supported",
		},
		{
			operation:  protocol.NewFormatOperationId("xml.edit.remove-element", 1),
			targetRole: "xml.element@1",
			support:    "Supported",
		},
		{
			operation:  protocol.NewFormatOperationId("xml.edit.rename-element", 1),
			targetRole: "xml.element@1",
			arguments:  []OperationArgumentDescriptor{stringArgument("name")},
			support:    "Supported",
		},
	}
}

// ID returns the stable operation identifier with its version suffix.
func (d FormatOperationDescriptor) ID() string {
	return d.operation.ID() + "@" + itoa(int(d.operation.Version()))
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

// CreateUntouchedByteProof creates a proof only when the replacements
// exactly produce the supplied target snapshot.
func CreateUntouchedByteProof(base, target *document.SourceSnapshot,
	replacements []document.SourceReplacement) (*UntouchedByteProof, *document.UntouchedByteProofError) {
	proof, err := document.CreateUntouchedByteProof(base, target, replacements)
	if err != nil {
		return nil, err.(*document.UntouchedByteProofError)
	}
	return &proof, nil
}

// IsUntouchedProofError reports whether one error is a proof failure.
func IsUntouchedProofError(err error) bool {
	return document.IsUntouchedProofError(err)
}
