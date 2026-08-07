package json

import (
	"fmt"
	"strings"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the JSON-local edit records: the change set, the
// transferable dry-run plan, and the format operation registry
// (consema-document change.rs, edit_plan.rs, operation_registry.rs).
// go/document does not yet carry these records; they live here until the
// document milestone that absorbs them.

// SourceEdit is one exact source edit between the old and new snapshots
// (consema-document ChangeSet source edits).
type SourceEdit struct {
	// OldSpan is the exact old-source range.
	OldSpan document.Span
	// NewSpan is the exact new-source range of the replacement bytes.
	NewSpan document.Span
	// Replacement is the exact replacement bytes.
	Replacement []byte
}

// NodeMapping is one old-to-new node mapping fact
// (consema-document ChangeSet node mappings).
type NodeMapping struct {
	// Old is the old node identity.
	Old document.NodeRef
	// New is the new node identity when one exists.
	New *document.NodeRef
	// Status is the mapping topology/status.
	Status protocol.NodeMappingStatus
	// Reason is the stable reason for non-trivial or unresolved mapping.
	Reason *string
}

// ChangeSet is the complete old-to-new change facts of one edit commit
// (RFC 0016 §5.3; consema-document ChangeSet).
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
// authorize a write (RFC 0015 §8.1 read-only precedent; consema-document
// edit_plan.rs).
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
		expected := operation.Operation.ID() + "@" + uint64String(uint64(operation.Operation.Version()))
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

// WithAllReplacementsRedacted marks every replacement payload for redacted
// review/debug presentation. Exact bytes remain present for digest and
// original-byte precondition checks.
func (p *EditPlan) WithAllReplacementsRedacted(redactOriginal, redactReplacement bool) (*EditPlan, error) {
	redacted, err := p.patch.WithAllReplacementsRedacted(redactOriginal, redactReplacement)
	if err != nil {
		return nil, err
	}
	return &EditPlan{
		sourceID:   p.sourceID,
		profile:    p.profile,
		operations: p.operations,
		patch:      redacted,
		report:     p.report,
	}, nil
}

// DebugString renders the plan for review with the redaction flags
// honored: replacement payloads marked redacted render as "<redacted>".
// The text is human presentation only and never participates in
// conformance comparison.
func (p *EditPlan) DebugString() string {
	var builder strings.Builder
	builder.WriteString("EditPlan { source_id: ")
	builder.WriteString(p.sourceID)
	builder.WriteString(", operations: [")
	for index, operation := range p.operations {
		if index > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(operation.Operation.ID())
		builder.WriteString("@")
		builder.WriteString(uint64String(uint64(operation.Operation.Version())))
	}
	builder.WriteString("], replacements: [")
	replacements := p.patch.Replacements()
	for index, replacement := range replacements {
		if index > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString("SourceReplacement { original: ")
		appendRedactedBytes(&builder, replacement.Original(), replacement.RedactOriginal())
		builder.WriteString(", replacement: ")
		appendRedactedBytes(&builder, replacement.Replacement(), replacement.RedactReplacement())
		builder.WriteString(" }")
	}
	builder.WriteString("] }")
	return builder.String()
}

// appendRedactedBytes renders one payload honoring its redaction flag.
func appendRedactedBytes(builder *strings.Builder, bytes []byte, redacted bool) {
	if redacted {
		builder.WriteString("<redacted>")
		return
	}
	builder.WriteString(fmt.Sprintf("%q", string(bytes)))
}

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
// Every profile publishes the identical surface (operation_registry.rs:11-14).
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
// (operation_registry.rs:16-80).
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
