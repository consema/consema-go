package ini

import (
	"fmt"
	"strings"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the INI edit records: the change set and the
// transferable dry-run plan (consema-document change_set.rs and
// edit_plan.rs; the same record shapes as go/json/change_set.go).

// SourceEdit is one exact source edit between the old and new snapshots.
type SourceEdit struct {
	// OldSpan is the exact old-source range.
	OldSpan document.Span
	// NewSpan is the exact new-source range of the replacement bytes.
	NewSpan document.Span
	// Replacement is the exact replacement bytes.
	Replacement []byte
}

// NodeMappingStatus is the closed old-to-new node mapping status.
type NodeMappingStatus string

// The three frozen mapping statuses.
const (
	// NodeMappingReplaced maps the old node to a reparsed result node.
	NodeMappingReplaced NodeMappingStatus = "Replaced"
	// NodeMappingDeleted reports the old node was deleted.
	NodeMappingDeleted NodeMappingStatus = "Deleted"
	// NodeMappingUnmapped reports the old node has no result identity.
	NodeMappingUnmapped NodeMappingStatus = "Unmapped"
)

// NodeMapping is one old-to-new node identity fact.
type NodeMapping struct {
	// Old is the base identity.
	Old document.NodeRef
	// New is the reparsed result identity when one exists.
	New *document.NodeRef
	// Status is the mapping status.
	Status NodeMappingStatus
	// Reason is the stable reason for non-trivial or unresolved mapping.
	Reason *string
}

// ChangeSet is the complete old-to-new change facts of one edit commit.
type ChangeSet struct {
	base        document.SnapshotIdentity
	target      document.SnapshotIdentity
	sourceEdits []SourceEdit
	mappings    []NodeMapping
	diagnostics []*protocol.Diagnostic
}

// BaseSnapshot returns the base snapshot identity.
func (c *ChangeSet) BaseSnapshot() document.SnapshotIdentity { return c.base }

// TargetSnapshot returns the target snapshot identity.
func (c *ChangeSet) TargetSnapshot() document.SnapshotIdentity { return c.target }

// SourceEdits returns the ordered source edits. The returned slice is a
// copy.
func (c *ChangeSet) SourceEdits() []SourceEdit {
	return append([]SourceEdit(nil), c.sourceEdits...)
}

// NodeMappings returns the ordered node mappings. The returned slice is a
// copy.
func (c *ChangeSet) NodeMappings() []NodeMapping {
	return append([]NodeMapping(nil), c.mappings...)
}

// Diagnostics returns the ordered commit diagnostics. The returned slice
// is a copy.
func (c *ChangeSet) Diagnostics() []*protocol.Diagnostic {
	return append([]*protocol.Diagnostic(nil), c.diagnostics...)
}

// EditPlan is the fully validated dry-run plan; possessing it does not
// authorize a write (RFC 0015 §8.1 read-only precedent).
type EditPlan struct {
	sourceID   string
	profile    document.ProfileId
	operations []*protocol.EditOperationSummary
	patch      *document.SourcePatch
	report     []*protocol.Diagnostic
}

// newEditPlan closes a plan only when its ordered operation metadata
// matches its exact patch (edit_plan.rs:79-117).
func newEditPlan(sourceID string, profile document.ProfileId,
	operations []*protocol.EditOperationSummary, patch *document.SourcePatch,
	report []*protocol.Diagnostic) (*EditPlan, error) {
	if sourceID == "" || len(sourceID) > 1024 {
		return nil, fmt.Errorf("edit plan: invalid source id")
	}
	metadata := patch.Metadata()
	for index, operation := range operations {
		key := fmt.Sprintf("operation.%d", index)
		expected := operation.Operation.ID() + "@" + u32String(uint32(operation.Operation.Version()))
		if metadata[key] != expected {
			return nil, fmt.Errorf("edit plan: operation metadata mismatch at %d", index)
		}
	}
	operationKeys := 0
	for key := range metadata {
		if strings.HasPrefix(key, "operation.") {
			operationKeys++
		}
	}
	if operationKeys != len(operations) {
		return nil, fmt.Errorf("edit plan: operation metadata count mismatch")
	}
	return &EditPlan{
		sourceID:   sourceID,
		profile:    profile,
		operations: operations,
		patch:      patch,
		report:     report,
	}, nil
}

// SourceID returns the caller-stable source identity.
func (p *EditPlan) SourceID() string { return p.sourceID }

// TargetProfile returns the exact target profile.
func (p *EditPlan) TargetProfile() document.ProfileId { return p.profile }

// Operations returns the ordered safe operation summaries. The returned
// slice is a copy.
func (p *EditPlan) Operations() []*protocol.EditOperationSummary {
	return append([]*protocol.EditOperationSummary(nil), p.operations...)
}

// Replacements returns the exact patch replacements.
func (p *EditPlan) Replacements() []document.SourceReplacement {
	return p.patch.Replacements()
}

// TargetDigest returns the exact patch target digest.
func (p *EditPlan) TargetDigest() document.ContentDigest {
	return p.patch.TargetDigest()
}

// SourcePatch returns the exact patch facts.
func (p *EditPlan) SourcePatch() *document.SourcePatch { return p.patch }

// Diagnostics returns the ordered plan diagnostics. The returned slice is
// a copy.
func (p *EditPlan) Diagnostics() []*protocol.Diagnostic {
	return append([]*protocol.Diagnostic(nil), p.report...)
}
