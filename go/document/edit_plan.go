package document

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"consema.dev/consema/protocol"
)

// EditPlanSourceId is the caller-stable source identity of a transferable
// edit plan (consema-document edit_plan.rs:12-31).
type EditPlanSourceId struct {
	value string
}

// NewEditPlanSourceId validates one non-empty bounded external source
// identity (consema-document edit_plan.rs:16-24).
func NewEditPlanSourceId(value string) (*EditPlanSourceId, error) {
	if value == "" || len(value) > 1024 {
		return nil, &EditPlanError{Kind: EditPlanErrorInvalidSourceId}
	}
	return &EditPlanSourceId{value: value}, nil
}

// String returns the exact caller-stable source identity.
func (s *EditPlanSourceId) String() string { return s.value }

// EditPlanErrorKind classifies an edit-plan construction failure
// (consema-document edit_plan.rs:199-211 EditPlanError).
type EditPlanErrorKind uint8

// The closed plan-construction failure classes.
const (
	// EditPlanErrorInvalidSourceId: the source identity is empty or
	// exceeds the frozen bound.
	EditPlanErrorInvalidSourceId EditPlanErrorKind = iota
	// EditPlanErrorOperationMetadataMismatch: operation ordering disagrees
	// with the exact SourcePatch metadata.
	EditPlanErrorOperationMetadataMismatch
)

// EditPlanError is a typed edit-plan construction failure. It implements
// error and the RFC 0016 §6 Code() contract with the generic
// invalid-input code.
type EditPlanError struct {
	// Kind identifies the failure.
	Kind EditPlanErrorKind
	// Index is the zero-based operation position of an
	// EditPlanErrorOperationMetadataMismatch failure.
	Index int
}

// Error implements error; the text is human presentation only.
func (e *EditPlanError) Error() string {
	switch e.Kind {
	case EditPlanErrorInvalidSourceId:
		return "edit plan: invalid source id"
	case EditPlanErrorOperationMetadataMismatch:
		return fmt.Sprintf("edit plan: operation metadata mismatch at %d", e.Index)
	}
	return "edit plan: failure"
}

// Code returns the registered invalid-input code (RFC 0016 §6).
func (e *EditPlanError) Code() string { return "core.protocol.invalid-value@1" }

// EditPlan is the fully validated dry-run plan; possessing it does not
// authorize a write (consema-document edit_plan.rs:72-197; RFC 0015 §8.1
// read-only precedent; RFC 0016 §5.3).
type EditPlan struct {
	sourceID   string
	profile    ProfileId
	operations []*protocol.EditOperationSummary
	patch      *SourcePatch
	report     []*protocol.Diagnostic
}

// NewEditPlan closes a plan only when its ordered operation metadata
// matches its exact patch (consema-document edit_plan.rs:83-121).
func NewEditPlan(sourceID string, profile ProfileId,
	operations []*protocol.EditOperationSummary, patch *SourcePatch,
	report []*protocol.Diagnostic) (*EditPlan, error) {
	if sourceID == "" || len(sourceID) > 1024 {
		return nil, &EditPlanError{Kind: EditPlanErrorInvalidSourceId}
	}
	metadata := patch.Metadata()
	for index, operation := range operations {
		key := fmt.Sprintf("operation.%d", index)
		expected := operation.Operation.ID() + "@" +
			strconv.FormatUint(uint64(operation.Operation.Version()), 10)
		if metadata[key] != expected {
			return nil, &EditPlanError{Kind: EditPlanErrorOperationMetadataMismatch, Index: index}
		}
	}
	operationKeys := 0
	for key := range metadata {
		if strings.HasPrefix(key, "operation.") {
			operationKeys++
		}
	}
	if operationKeys != len(operations) {
		return nil, &EditPlanError{Kind: EditPlanErrorOperationMetadataMismatch,
			Index: len(operations)}
	}
	return &EditPlan{
		sourceID:   sourceID,
		profile:    profile,
		operations: append([]*protocol.EditOperationSummary(nil), operations...),
		patch:      patch,
		report:     append([]*protocol.Diagnostic(nil), report...),
	}, nil
}

// SourceID returns the caller-stable source identity.
func (p *EditPlan) SourceID() string { return p.sourceID }

// TargetProfile returns the exact target profile.
func (p *EditPlan) TargetProfile() ProfileId { return p.profile }

// Profile returns the exact target profile; it is the same fact as
// TargetProfile under the naming of the TOML family.
func (p *EditPlan) Profile() ProfileId { return p.profile }

// Operations returns the ordered safe operation summaries. The returned
// slice is a copy.
func (p *EditPlan) Operations() []*protocol.EditOperationSummary {
	return append([]*protocol.EditOperationSummary(nil), p.operations...)
}

// Replacements returns the exact patch replacements.
func (p *EditPlan) Replacements() []SourceReplacement { return p.patch.Replacements() }

// TargetDigest returns the exact patch target digest.
func (p *EditPlan) TargetDigest() ContentDigest { return p.patch.TargetDigest() }

// SourcePatch returns the exact patch facts.
func (p *EditPlan) SourcePatch() *SourcePatch { return p.patch }

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
		builder.WriteString(strconv.FormatUint(uint64(operation.Operation.Version()), 10))
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

// String renders the plan with every replacement payload, honoring the
// redaction flags (the redacted debug facts must not leak raw values).
// The text is human presentation only and never participates in
// conformance comparison.
func (p *EditPlan) String() string {
	var output strings.Builder
	output.WriteString("EditPlan{source:")
	output.WriteString(p.sourceID)
	output.WriteString(" profile:")
	output.WriteString(p.profile.ID())
	output.WriteString("@")
	output.WriteString(strconv.FormatUint(uint64(p.profile.Version()), 10))
	output.WriteString(" operations:[")
	for index, operation := range p.operations {
		if index != 0 {
			output.WriteString(", ")
		}
		output.WriteString(operation.Operation.ID())
		output.WriteString("{")
		names := make([]string, 0, len(operation.Summary))
		for name := range operation.Summary {
			names = append(names, name)
		}
		sort.Strings(names)
		for position, name := range names {
			if position != 0 {
				output.WriteString(", ")
			}
			output.WriteString(name)
			output.WriteString("=")
			output.WriteString(operation.Summary[name])
		}
		output.WriteString("}")
	}
	output.WriteString("] patch:{")
	for index, replacement := range p.patch.Replacements() {
		if index != 0 {
			output.WriteString(", ")
		}
		output.WriteString("[")
		output.WriteString(strconv.Itoa(replacement.OldStart()))
		output.WriteString("..")
		output.WriteString(strconv.Itoa(replacement.OldEnd()))
		output.WriteString("] original=")
		if replacement.RedactOriginal() {
			output.WriteString("[redacted]")
		} else {
			output.WriteString(string(replacement.Original()))
		}
		output.WriteString(" replacement=")
		if replacement.RedactReplacement() {
			output.WriteString("[redacted]")
		} else {
			output.WriteString(string(replacement.Replacement()))
		}
	}
	output.WriteString("}}")
	return output.String()
}
