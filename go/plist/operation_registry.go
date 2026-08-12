package plist

// This file implements the plist operation discovery for the frozen
// `plist.xml@1` and `plist.binary@1` profiles (consema-plist
// operation_registry.rs; RFC 0013 §11). Both profiles publish the same six
// snapshot-bound structural operations, independently typed per profile.

import "consema.dev/consema/document"

// FormatOperationRegistry is the validated operation registry for one exact
// plist profile.
type FormatOperationRegistry struct {
	profile    document.ProfileId
	operations []FormatOperationDescriptor
}

// FormatOperationDescriptor is one versioned format operation descriptor.
type FormatOperationDescriptor struct {
	operation  string
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
// Every profile publishes the identical six-operation surface (RFC 0013
// §11).
func FormatOperationRegistryFor(profile PlistProfile) *FormatOperationRegistry {
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

// operationDescriptors builds the frozen six-operation surface
// (operation_registry.rs:20-83).
func operationDescriptors() []FormatOperationDescriptor {
	stringArgument := func(name string) OperationArgumentDescriptor {
		return OperationArgumentDescriptor{Name: name, Kind: "String", Required: true}
	}
	valueArgument := func(name string) OperationArgumentDescriptor {
		return OperationArgumentDescriptor{Name: name, Kind: "PortableValue", Required: true}
	}
	return []FormatOperationDescriptor{
		{
			operation:  "plist.edit.set-value@1",
			targetRole: "plist.value@1",
			arguments: []OperationArgumentDescriptor{
				{Name: "path", Kind: "NodeRef", Required: true}, valueArgument("value"),
			},
			support: "Supported",
		},
		{
			operation:  "plist.edit.insert-dict-entry@1",
			targetRole: "plist.value@1",
			arguments: []OperationArgumentDescriptor{
				{Name: "path", Kind: "NodeRef", Required: true}, stringArgument("key"),
				valueArgument("value"), {Name: "placement", Kind: "Placement", Required: true},
			},
			support: "Supported",
		},
		{
			operation:  "plist.edit.remove-dict-entry@1",
			targetRole: "plist.dict-entry@1",
			arguments: []OperationArgumentDescriptor{
				{Name: "path", Kind: "NodeRef", Required: true}, stringArgument("key"),
				{Name: "occurrence", Kind: "NodeRef", Required: true},
			},
			support: "Supported",
		},
		{
			operation:  "plist.edit.rename-dict-key@1",
			targetRole: "plist.dict-entry@1",
			arguments: []OperationArgumentDescriptor{
				{Name: "path", Kind: "NodeRef", Required: true}, stringArgument("from"),
				{Name: "occurrence", Kind: "NodeRef", Required: true}, stringArgument("to"),
			},
			support: "Supported",
		},
		{
			operation:  "plist.edit.insert-array-element@1",
			targetRole: "plist.value@1",
			arguments: []OperationArgumentDescriptor{
				{Name: "path", Kind: "NodeRef", Required: true},
				{Name: "index", Kind: "NodeRef", Required: true}, valueArgument("value"),
			},
			support: "Supported",
		},
		{
			operation:  "plist.edit.remove-array-element@1",
			targetRole: "plist.array-element@1",
			arguments: []OperationArgumentDescriptor{
				{Name: "path", Kind: "NodeRef", Required: true},
				{Name: "index", Kind: "NodeRef", Required: true},
			},
			support: "Supported",
		},
	}
}

// ID returns the stable operation identifier with its version suffix.
func (d FormatOperationDescriptor) ID() string { return d.operation }

// TargetRole returns the target role identifier.
func (d FormatOperationDescriptor) TargetRole() string { return d.targetRole }

// Arguments returns the ordered argument descriptors. The returned slice
// is a copy.
func (d FormatOperationDescriptor) Arguments() []OperationArgumentDescriptor {
	return append([]OperationArgumentDescriptor(nil), d.arguments...)
}

// Support returns the stable support class.
func (d FormatOperationDescriptor) Support() string { return d.support }
