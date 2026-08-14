package document

import "consema.dev/consema/protocol"

// SourceEdit is one exact source edit between the old and new snapshots
// (consema-document lib.rs SourceEdit).
type SourceEdit struct {
	// OldSpan is the exact old-source range.
	OldSpan Span
	// NewSpan is the exact new-source range of the replacement bytes.
	NewSpan Span
	// Replacement is the exact replacement bytes.
	Replacement []byte
}

// NodeMapping is one explicit old-to-new node mapping fact
// (consema-document lib.rs NodeMapping).
type NodeMapping struct {
	// Old is the old node identity.
	Old NodeRef
	// New is the new node identity when one exists.
	New *NodeRef
	// Status is the mapping topology/status; the six frozen
	// protocol.NodeMappingStatus values (Preserved, Replaced, Deleted,
	// Split, Merged, Unmapped) mirror consema-document NodeMappingStatus.
	Status protocol.NodeMappingStatus
	// Reason is the stable reason for non-trivial or unresolved mapping.
	Reason *string
}

// ChangeSet is the complete old-to-new change facts of one edit commit
// (consema-document lib.rs ChangeSet; RFC 0016 §5.3).
type ChangeSet struct {
	base        SnapshotIdentity
	target      SnapshotIdentity
	sourceEdits []SourceEdit
	mappings    []NodeMapping
	diagnostics []*protocol.Diagnostic
}

// NewChangeSet completes a change set from already ordered validated
// facts (consema-document ChangeSet::new). The supplied slices are
// copied; the completed change set is logically immutable.
func NewChangeSet(base, target SnapshotIdentity, sourceEdits []SourceEdit,
	mappings []NodeMapping, diagnostics []*protocol.Diagnostic) ChangeSet {
	return ChangeSet{
		base:        base,
		target:      target,
		sourceEdits: append([]SourceEdit(nil), sourceEdits...),
		mappings:    append([]NodeMapping(nil), mappings...),
		diagnostics: append([]*protocol.Diagnostic(nil), diagnostics...),
	}
}

// BaseSnapshot returns the base snapshot identity.
func (c *ChangeSet) BaseSnapshot() SnapshotIdentity { return c.base }

// TargetSnapshot returns the target snapshot identity.
func (c *ChangeSet) TargetSnapshot() SnapshotIdentity { return c.target }

// OldSnapshot returns the base snapshot identity; it is the same fact as
// BaseSnapshot under the old-to-new vocabulary of the Rust reference.
func (c *ChangeSet) OldSnapshot() SnapshotIdentity { return c.base }

// NewSnapshot returns the target snapshot identity; it is the same fact
// as TargetSnapshot under the old-to-new vocabulary of the Rust
// reference.
func (c *ChangeSet) NewSnapshot() SnapshotIdentity { return c.target }

// SourceEdits returns the ordered source edits. The returned slice is a
// copy.
func (c *ChangeSet) SourceEdits() []SourceEdit { return append([]SourceEdit(nil), c.sourceEdits...) }

// NodeMappings returns the ordered node mappings. The returned slice is a
// copy.
func (c *ChangeSet) NodeMappings() []NodeMapping { return append([]NodeMapping(nil), c.mappings...) }

// Diagnostics returns the ordered commit diagnostics. The returned slice
// is a copy.
func (c *ChangeSet) Diagnostics() []*protocol.Diagnostic {
	return append([]*protocol.Diagnostic(nil), c.diagnostics...)
}
