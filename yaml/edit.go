package yaml

import (
	"fmt"
	"sort"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the typed YAML edit operations and the atomic
// commit pipeline (consema-yaml edit.rs; RFC 0007 §12). An edit replaces
// only the bytes its operations own; every other byte is covered by the
// untouched-byte proof, and a failure never modifies the base document.
// Anchor-safe rules are enforced: renaming updates dependent aliases in
// one transaction, removing an anchored definition with remaining aliases
// is rejected, removing an alias never removes its target, and inserting
// an alias requires an earlier visible anchor in the same document.

// RepresentationPolicy is the explicit semantic scalar representation
// policy (edit.rs:17-28).
type RepresentationPolicy uint8

// The four frozen policies.
const (
	// RepresentationPolicyExactLiteral requires the caller to use a
	// literal scalar replacement; semantic replacement rejects it.
	RepresentationPolicyExactLiteral RepresentationPolicy = iota
	// RepresentationPolicyPreserveCompatible retains the target scalar
	// category and presentation style or fails.
	RepresentationPolicyPreserveCompatible
	// RepresentationPolicyCanonicalForProfile uses the frozen canonical
	// YAML scalar representation.
	RepresentationPolicyCanonicalForProfile
	// RepresentationPolicyPreserveElseCanonical preserves when compatible,
	// otherwise reports a canonical fallback.
	RepresentationPolicyPreserveElseCanonical
)

// ScalarReplacementKind is the closed scalar replacement category.
type ScalarReplacementKind uint8

// The two frozen categories.
const (
	// ScalarReplacementSemantic replaces by a public semantic value under
	// an explicit representation policy.
	ScalarReplacementSemantic ScalarReplacementKind = iota
	// ScalarReplacementLiteral replaces by exact candidate literal bytes
	// after full profile validation.
	ScalarReplacementLiteral
)

// ScalarReplacement is one scalar operation bound to the transaction's
// base snapshot (edit.rs:30-57).
type ScalarReplacement struct {
	// Kind is the closed replacement category.
	Kind ScalarReplacementKind
	// Target is the exact target NodeRef.
	Target document.NodeRef
	// Value is the new complete core scalar (Semantic).
	Value core.Value
	// Policy is the representation contract (Semantic).
	Policy RepresentationPolicy
	// Literal is the exact candidate bytes (Literal).
	Literal []byte
}

// EditOperationKind is the closed YAML edit operation category
// (edit.rs:59-108).
type EditOperationKind uint8

// The eight frozen operation categories.
const (
	// EditOperationReplaceScalar replaces an existing scalar semantically
	// or literally.
	EditOperationReplaceScalar EditOperationKind = iota
	// EditOperationRenameAnchor renames one anchor definition and its
	// dependent aliases.
	EditOperationRenameAnchor
	// EditOperationInsertMappingEntry inserts one complete mapping
	// association.
	EditOperationInsertMappingEntry
	// EditOperationRemoveMappingEntry removes one exact mapping
	// association identity.
	EditOperationRemoveMappingEntry
	// EditOperationInsertSequenceElement inserts one complete sequence
	// element.
	EditOperationInsertSequenceElement
	// EditOperationRemoveSequenceElement removes one exact sequence element
	// identity.
	EditOperationRemoveSequenceElement
	// EditOperationInsertAlias inserts one alias sequence association.
	EditOperationInsertAlias
)

// EditOperation is one typed YAML edit operation bound to an immutable
// base snapshot (edit.rs:59-108). Only the fields of the declared Kind
// are meaningful.
type EditOperation struct {
	// Kind is the closed operation category.
	Kind EditOperationKind
	// Target is the exact target NodeRef (ReplaceScalar, RenameAnchor,
	// RemoveMappingEntry, RemoveSequenceElement).
	Target document.NodeRef
	// Scalar is the scalar replacement (ReplaceScalar).
	Scalar ScalarReplacement
	// Mapping is the exact mapping value target (InsertMappingEntry,
	// InsertSequenceElement, InsertAlias).
	Mapping document.NodeRef
	// Name is the new anchor name (RenameAnchor).
	Name string
	// Key is the inserted key value (InsertMappingEntry).
	Key core.Value
	// Value is the inserted value (InsertMappingEntry,
	// InsertSequenceElement).
	Value core.Value
	// Anchor is the anchor-definition target (InsertAlias).
	Anchor document.NodeRef
	// Placement is the explicit association placement.
	Placement AssociationPlacement
}

// AssociationPlacement is the explicit association placement of one
// structural edit (edit.rs AssociationPlacement).
type AssociationPlacement struct {
	// Kind is "Start", "End", "Before", or "After".
	Kind string
	// Anchor is the association identity of Before/After placements.
	Anchor document.NodeRef
}

// PlacementStart is the start placement.
func PlacementStart() AssociationPlacement { return AssociationPlacement{Kind: "Start"} }

// PlacementEnd is the end placement.
func PlacementEnd() AssociationPlacement { return AssociationPlacement{Kind: "End"} }

// PlacementBefore places the new association before one exact association.
func PlacementBefore(anchor document.NodeRef) AssociationPlacement {
	return AssociationPlacement{Kind: "Before", Anchor: anchor}
}

// PlacementAfter places the new association after one exact association.
func PlacementAfter(anchor document.NodeRef) AssociationPlacement {
	return AssociationPlacement{Kind: "After", Anchor: anchor}
}

// EditTransaction is the immutable transaction; every operation resolves
// against one base snapshot (edit.rs:110-129).
type EditTransaction struct {
	base       document.SnapshotIdentity
	operations []EditOperation
}

// BaseSnapshot returns the base snapshot identity.
func (t *EditTransaction) BaseSnapshot() document.SnapshotIdentity { return t.base }

// Operations returns the ordered declared operations. The returned slice
// is a copy.
func (t *EditTransaction) Operations() []EditOperation {
	return append([]EditOperation(nil), t.operations...)
}

// EditTransactionBuilder is a builder that is not a committed edit
// (edit.rs:131-243).
type EditTransactionBuilder struct {
	base       document.SnapshotIdentity
	operations []EditOperation
}

// NewEditTransactionBuilder binds a new transaction to one immutable base
// document.
func NewEditTransactionBuilder(document *Document) *EditTransactionBuilder {
	return &EditTransactionBuilder{base: document.SnapshotIdentity()}
}

// SemanticScalar adds a semantic scalar replacement.
func (b *EditTransactionBuilder) SemanticScalar(target document.NodeRef, value core.Value,
	policy RepresentationPolicy) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind:   EditOperationReplaceScalar,
		Target: target,
		Scalar: ScalarReplacement{Kind: ScalarReplacementSemantic, Target: target,
			Value: value, Policy: policy},
	})
	return b
}

// LiteralScalar adds an exact literal scalar replacement.
func (b *EditTransactionBuilder) LiteralScalar(target document.NodeRef,
	literal []byte) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind:   EditOperationReplaceScalar,
		Target: target,
		Scalar: ScalarReplacement{Kind: ScalarReplacementLiteral, Target: target,
			Literal: append([]byte(nil), literal...)},
	})
	return b
}

// RenameAnchor adds one anchor rename that also updates its dependent
// aliases.
func (b *EditTransactionBuilder) RenameAnchor(target document.NodeRef,
	name string) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRenameAnchor, Target: target, Name: name,
	})
	return b
}

// InsertMappingEntry adds one mapping association insertion with an
// arbitrary key value.
func (b *EditTransactionBuilder) InsertMappingEntry(mapping document.NodeRef,
	key, value core.Value, placement AssociationPlacement) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationInsertMappingEntry, Mapping: mapping, Key: key, Value: value,
		Placement: placement,
	})
	return b
}

// RemoveMappingEntry adds one exact mapping association removal.
func (b *EditTransactionBuilder) RemoveMappingEntry(target document.NodeRef) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRemoveMappingEntry, Target: target,
	})
	return b
}

// InsertSequenceElement adds one sequence element insertion.
func (b *EditTransactionBuilder) InsertSequenceElement(sequence document.NodeRef,
	value core.Value, placement AssociationPlacement) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationInsertSequenceElement, Mapping: sequence, Value: value,
		Placement: placement,
	})
	return b
}

// RemoveSequenceElement adds one exact sequence element removal.
func (b *EditTransactionBuilder) RemoveSequenceElement(target document.NodeRef) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRemoveSequenceElement, Target: target,
	})
	return b
}

// InsertAlias adds one alias sequence association requiring an earlier
// visible anchor.
func (b *EditTransactionBuilder) InsertAlias(sequence, anchor document.NodeRef,
	placement AssociationPlacement) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationInsertAlias, Mapping: sequence, Anchor: anchor,
		Placement: placement,
	})
	return b
}

// Build completes the immutable request; target validation happens
// atomically at commit (edit.rs:236-242).
func (b *EditTransactionBuilder) Build() *EditTransaction {
	return &EditTransaction{
		base:       b.base,
		operations: append([]EditOperation(nil), b.operations...),
	}
}

// EditCommit is the atomic edit success (edit.rs:245-256).
type EditCommit struct {
	// Document is the new immutable document.
	Document *Document
	// ChangeSet carries the complete old-to-new change facts.
	ChangeSet ChangeSet
	// SourcePatch carries the portable exact raw-byte application facts.
	SourcePatch *document.SourcePatch
	// UntouchedProof carries verifiable evidence for every byte outside
	// the replacement set.
	UntouchedProof UntouchedByteProof
}

// ChangeSet is the complete old-to-new change facts (consema-document
// change_set.rs).
type ChangeSet struct {
	oldSnapshot document.SnapshotIdentity
	newSnapshot document.SnapshotIdentity
	sourceEdits []SourceEdit
	mappings    []NodeMapping
	diagnostics []*protocol.Diagnostic
}

// OldSnapshot returns the base snapshot identity.
func (c ChangeSet) OldSnapshot() document.SnapshotIdentity { return c.oldSnapshot }

// NewSnapshot returns the result snapshot identity.
func (c ChangeSet) NewSnapshot() document.SnapshotIdentity { return c.newSnapshot }

// SourceEdits returns the ordered source edits.
func (c ChangeSet) SourceEdits() []SourceEdit { return append([]SourceEdit(nil), c.sourceEdits...) }

// NodeMappings returns the old-to-new node mappings.
func (c ChangeSet) NodeMappings() []NodeMapping { return append([]NodeMapping(nil), c.mappings...) }

// Diagnostics returns the ordered edit diagnostics.
func (c ChangeSet) Diagnostics() []*protocol.Diagnostic {
	return append([]*protocol.Diagnostic(nil), c.diagnostics...)
}

// SourceEdit is one raw-byte source edit (consema-document source_patch.rs
// SourceEdit).
type SourceEdit struct {
	// OldSpan is the replaced base span.
	OldSpan document.Span
	// NewSpan is the replacement span in the result snapshot.
	NewSpan document.Span
	// Replacement is the exact new bytes.
	Replacement []byte
}

// NodeMappingStatus is the closed node-mapping status.
type NodeMappingStatus string

// The three frozen node-mapping statuses.
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

// EditFailureKind is the stable edit validation or commit failure category
// (edit.rs:258-299).
type EditFailureKind uint8

// The closed edit failure categories.
const (
	// EditFailureWrongSnapshot: the transaction or target belongs to
	// another snapshot.
	EditFailureWrongSnapshot EditFailureKind = iota
	// EditFailureWrongRole: the target role does not match the selected
	// operation.
	EditFailureWrongRole
	// EditFailureTargetNotFound: the target identity is not present in the
	// base document.
	EditFailureTargetNotFound
	// EditFailureIncompleteTarget: the target is not one complete editable
	// scalar or anchor occurrence.
	EditFailureIncompleteTarget
	// EditFailureUnsupportedSemanticValue: the public value cannot be
	// represented as a YAML scalar.
	EditFailureUnsupportedSemanticValue
	// EditFailureInvalidLiteral: the exact candidate is not one complete
	// scalar literal.
	EditFailureInvalidLiteral
	// EditFailureRepresentationIncompatible: PreserveCompatible could not
	// retain category and presentation style.
	EditFailureRepresentationIncompatible
	// EditFailureExactLiteralRequiresLiteralOperation: ExactLiteral was
	// requested without an exact literal operation.
	EditFailureExactLiteralRequiresLiteralOperation
	// EditFailureInvalidAnchorName: the new anchor name is not accepted as
	// one exact anchor property.
	EditFailureInvalidAnchorName
	// EditFailureInvalidPlacement: the placement anchor is from another
	// container or has the wrong association role.
	EditFailureInvalidPlacement
	// EditFailureAnchorNotVisible: the inserted alias target is not the
	// last visible definition of its name.
	EditFailureAnchorNotVisible
	// EditFailureAnchorDependency: a removal would leave an alias whose
	// anchor definition no longer exists.
	EditFailureAnchorDependency
	// EditFailureUnsupportedInsertedValue: the portable input cannot be
	// represented exactly by the YAML value materializer.
	EditFailureUnsupportedInsertedValue
	// EditFailureStructuralContainerConflict: more than one structural
	// mutation targets the same base container.
	EditFailureStructuralContainerConflict
	// EditFailureDuplicateTarget: more than one operation names the same
	// destructive target.
	EditFailureDuplicateTarget
	// EditFailureOverlappingOwnership: prepared source ownership intervals
	// overlap or reuse one insertion point.
	EditFailureOverlappingOwnership
	// EditFailureAncestorDescendantConflict: one operation edits an
	// ancestor/descendant region of another operation.
	EditFailureAncestorDescendantConflict
	// EditFailureResourceLimit: a configured edit or output bound was
	// exceeded.
	EditFailureResourceLimit
	// EditFailureNewDocumentFormationFailed: the replacement bytes did not
	// form the promised YAML document and topology.
	EditFailureNewDocumentFormationFailed
)

// EditFailure is the typed edit failure. It implements error and the
// RFC 0016 §6 Code() contract with the frozen registered codes.
type EditFailure struct {
	// Kind identifies the failure.
	Kind EditFailureKind
	// ValueKind is the offending PortableValue kind of unsupported-value
	// failures.
	ValueKind core.Kind
	// LimitName is the stable limit name of a ResourceLimit.
	LimitName string
}

// Error implements error.
func (e *EditFailure) Error() string {
	switch e.Kind {
	case EditFailureWrongSnapshot:
		return "yaml: edit transaction or target belongs to another snapshot"
	case EditFailureWrongRole:
		return "yaml: edit target has the wrong structural role"
	case EditFailureTargetNotFound:
		return "yaml: edit target or placement anchor was not found"
	case EditFailureIncompleteTarget:
		return "yaml: edit target is not a complete editable occurrence"
	case EditFailureUnsupportedSemanticValue:
		return "yaml: portable kind " + e.ValueKind.String() + " is not a YAML scalar"
	case EditFailureInvalidLiteral:
		return "yaml: edit literal is invalid for the target profile"
	case EditFailureRepresentationIncompatible:
		return "yaml: representation policy cannot preserve the target category"
	case EditFailureExactLiteralRequiresLiteralOperation:
		return "yaml: exact literal policy requires a literal operation"
	case EditFailureInvalidAnchorName:
		return "yaml: edit anchor name is not accepted"
	case EditFailureInvalidPlacement:
		return "yaml: edit placement anchor is invalid"
	case EditFailureAnchorNotVisible:
		return "yaml: inserted alias anchor is not visible"
	case EditFailureAnchorDependency:
		return "yaml: removal would orphan a dependent alias"
	case EditFailureUnsupportedInsertedValue:
		return "yaml: portable kind " + e.ValueKind.String() + " is not representable"
	case EditFailureStructuralContainerConflict:
		return "yaml: more than one structural mutation targets one container"
	case EditFailureDuplicateTarget:
		return "yaml: more than one operation names the same target"
	case EditFailureOverlappingOwnership:
		return "yaml: prepared source ownership intervals overlap"
	case EditFailureAncestorDescendantConflict:
		return "yaml: an edit targets an ancestor/descendant region"
	case EditFailureResourceLimit:
		return "yaml: edit limit " + e.LimitName + " reached"
	case EditFailureNewDocumentFormationFailed:
		return "yaml: replacement document could not be formed"
	}
	return "yaml: edit failure"
}

// Name returns the stable failure name (the Rust variant spelling used by
// the conformance vectors).
func (e *EditFailure) Name() string {
	switch e.Kind {
	case EditFailureWrongSnapshot:
		return "WrongSnapshot"
	case EditFailureWrongRole:
		return "WrongRole"
	case EditFailureTargetNotFound:
		return "TargetNotFound"
	case EditFailureIncompleteTarget:
		return "IncompleteTarget"
	case EditFailureUnsupportedSemanticValue:
		return "UnsupportedSemanticValue"
	case EditFailureInvalidLiteral:
		return "InvalidLiteral"
	case EditFailureRepresentationIncompatible:
		return "RepresentationIncompatible"
	case EditFailureExactLiteralRequiresLiteralOperation:
		return "ExactLiteralRequiresLiteralOperation"
	case EditFailureInvalidAnchorName:
		return "InvalidAnchorName"
	case EditFailureInvalidPlacement:
		return "InvalidPlacement"
	case EditFailureAnchorNotVisible:
		return "AnchorNotVisible"
	case EditFailureAnchorDependency:
		return "AnchorDependency"
	case EditFailureUnsupportedInsertedValue:
		return "UnsupportedInsertedValue"
	case EditFailureStructuralContainerConflict:
		return "StructuralContainerConflict"
	case EditFailureDuplicateTarget:
		return "DuplicateTarget"
	case EditFailureOverlappingOwnership:
		return "OverlappingOwnership"
	case EditFailureAncestorDescendantConflict:
		return "AncestorDescendantConflict"
	case EditFailureResourceLimit:
		return "ResourceLimit"
	case EditFailureNewDocumentFormationFailed:
		return "NewDocumentFormationFailed"
	}
	return "EditFailure"
}

// Code returns the frozen registered code for the failure.
func (e *EditFailure) Code() string {
	switch e.Kind {
	case EditFailureWrongSnapshot:
		return "core.edit.wrong-snapshot@1"
	case EditFailureWrongRole:
		return "core.edit.wrong-role@1"
	case EditFailureTargetNotFound:
		return "core.edit.target-not-found@1"
	case EditFailureIncompleteTarget:
		return "core.edit.incomplete-target@1"
	case EditFailureUnsupportedSemanticValue, EditFailureUnsupportedInsertedValue:
		return "core.edit.unsupported-value@1"
	case EditFailureInvalidLiteral:
		return "core.edit.invalid-literal@1"
	case EditFailureRepresentationIncompatible:
		return "core.edit.representation-incompatible@1"
	case EditFailureExactLiteralRequiresLiteralOperation:
		return "core.edit.exact-literal-requires-literal@1"
	case EditFailureInvalidAnchorName:
		return "yaml.edit.invalid-anchor-name@1"
	case EditFailureInvalidPlacement:
		return "yaml.edit.invalid-placement@1"
	case EditFailureAnchorNotVisible:
		return "yaml.edit.anchor-not-visible@1"
	case EditFailureAnchorDependency:
		return "yaml.edit.anchor-dependency@1"
	case EditFailureStructuralContainerConflict:
		return "yaml.edit.structural-container-conflict@1"
	case EditFailureDuplicateTarget, EditFailureOverlappingOwnership,
		EditFailureAncestorDescendantConflict:
		return "core.edit.conflicting-edits@1"
	case EditFailureResourceLimit:
		return "core.edit.resource-limit@1"
	case EditFailureNewDocumentFormationFailed:
		return "core.edit.formation-failed@1"
	}
	return "core.edit.conflicting-edits@1"
}

// preparedEdit is one byte-level replacement with its optional node
// mapping plan (edit.rs PreparedEdit).
type preparedEdit struct {
	oldSpan      document.Span
	replacement  []byte
	mappingOld   document.NodeRef
	mappingPlan  mappingPlanKind
	mappingIndex int
}

// mappingPlanKind is the closed node-mapping plan.
type mappingPlanKind uint8

// The frozen mapping plans.
const (
	mappingPlanNone mappingPlanKind = iota
	mappingPlanNode
	mappingPlanAnchor
	mappingPlanAlias
	mappingPlanRemoved
)

// Commit atomically commits scalar, structural, and anchor operations. On
// failure the base document remains unchanged (edit.rs:301-451).
func (d *Document) Commit(tx *EditTransaction) (*EditCommit, *EditFailure) {
	if tx.base != d.SnapshotIdentity() {
		return nil, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if failure := d.validateDependencies(tx); failure != nil {
		return nil, failure
	}
	var diagnostics []*protocol.Diagnostic
	prepared := make([]preparedEdit, 0, len(tx.operations))
	for index := range tx.operations {
		edits, failure := d.prepareOperation(&tx.operations[index], &diagnostics)
		if failure != nil {
			return nil, failure
		}
		prepared = append(prepared, edits...)
	}
	sort.SliceStable(prepared, func(i, j int) bool {
		if prepared[i].oldSpan.StartByte() != prepared[j].oldSpan.StartByte() {
			return prepared[i].oldSpan.StartByte() < prepared[j].oldSpan.StartByte()
		}
		return prepared[i].oldSpan.EndByte() < prepared[j].oldSpan.EndByte()
	})
	for index := 1; index < len(prepared); index++ {
		left, right := prepared[index-1], prepared[index]
		if !left.oldSpan.IsEmpty() && !right.oldSpan.IsEmpty() &&
			(left.oldSpan.EndByte() > right.oldSpan.StartByte() ||
				left.oldSpan == right.oldSpan) {
			return nil, &EditFailure{Kind: EditFailureAncestorDescendantConflict}
		}
		if left.oldSpan == right.oldSpan ||
			(left.oldSpan.IsEmpty() && right.oldSpan.IsEmpty() &&
				left.oldSpan.StartByte() == right.oldSpan.StartByte()) {
			return nil, &EditFailure{Kind: EditFailureOverlappingOwnership}
		}
	}
	source := d.source.Bytes()
	targetLen := len(source)
	for _, edit := range prepared {
		targetLen = targetLen - edit.oldSpan.Len() + len(edit.replacement)
		if targetLen > d.limits.MaxSourceBytes {
			return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "target-bytes"}
		}
	}
	rendered := make([]byte, 0, targetLen)
	cursor := 0
	for _, edit := range prepared {
		rendered = append(rendered, source[cursor:edit.oldSpan.StartByte()]...)
		rendered = append(rendered, edit.replacement...)
		cursor = edit.oldSpan.EndByte()
	}
	rendered = append(rendered, source[cursor:]...)
	newDocument, formationFailure := Parse(rendered, d.profile, d.limits)
	if formationFailure != nil {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	structural := false
	for index := range tx.operations {
		if isStructuralOperation(&tx.operations[index]) {
			structural = true
			break
		}
	}
	if structural {
		if failure := d.validateStructuralCandidate(newDocument, tx); failure != nil {
			return nil, failure
		}
	} else {
		if failure := d.validateCandidate(newDocument, tx); failure != nil {
			return nil, failure
		}
	}
	delta := 0
	sourceEdits := make([]SourceEdit, 0, len(prepared))
	mappings := make([]NodeMapping, 0, len(tx.operations))
	mappedOld := make(map[document.NodeRef]bool, len(prepared))
	for _, edit := range prepared {
		newStart := edit.oldSpan.StartByte() + delta
		newEnd := newStart + len(edit.replacement)
		newSpan, err := newDocument.authority.Span(newStart, newEnd)
		if err != nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		sourceEdits = append(sourceEdits, SourceEdit{
			OldSpan: edit.oldSpan, NewSpan: newSpan,
			Replacement: append([]byte(nil), edit.replacement...),
		})
		if edit.mappingPlan != mappingPlanNone && !mappedOld[edit.mappingOld] {
			mappedOld[edit.mappingOld] = true
			mapping := NodeMapping{Old: edit.mappingOld, Status: NodeMappingUnmapped}
			switch edit.mappingPlan {
			case mappingPlanNode:
				mapping.Status = NodeMappingReplaced
				newNode := newDocument.nodeRef(edit.mappingIndex)
				mapping.New = &newNode
			case mappingPlanAnchor:
				if newDocument.native.nodes[edit.mappingIndex].hasAnchor {
					mapping.Status = NodeMappingReplaced
					anchorRef := newDocument.authority.NodeRef(uint64(edit.mappingIndex),
						document.RoleYamlAnchorDefinition)
					mapping.New = &anchorRef
				} else {
					mapping.Reason = strPtr("reparsed-node-not-uniquely-located")
				}
			case mappingPlanAlias:
				if edit.mappingIndex < len(newDocument.native.aliases) {
					mapping.Status = NodeMappingReplaced
					alias := newDocument.native.aliases[edit.mappingIndex]
					aliasRef := newDocument.authority.NodeRef(alias.identity, document.RoleYamlAlias)
					mapping.New = &aliasRef
				} else {
					mapping.Reason = strPtr("reparsed-node-not-uniquely-located")
				}
			case mappingPlanRemoved:
				mapping.Status = NodeMappingDeleted
				reason := "association-removed-by-declared-operation"
				mapping.Reason = &reason
			}
			mappings = append(mappings, mapping)
		}
		delta += len(edit.replacement) - edit.oldSpan.Len()
	}
	changeSet := ChangeSet{
		oldSnapshot: d.SnapshotIdentity(),
		newSnapshot: newDocument.SnapshotIdentity(),
		sourceEdits: sourceEdits,
		mappings:    mappings,
		diagnostics: diagnostics,
	}
	patchLimits := editSourcePatchLimits(d.limits, len(sourceEdits))
	sourcePatch, err := document.NewSourcePatch(d.source,
		editSourcePatchReplacements(source, sourceEdits),
		editOperationMetadata(tx), patchLimits)
	if err != nil {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	proof, err := CreateUntouchedByteProof(d.source, newDocument.source,
		sourcePatch.Replacements())
	if err != nil {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return &EditCommit{
		Document:       newDocument,
		ChangeSet:      changeSet,
		SourcePatch:    sourcePatch,
		UntouchedProof: proof,
	}, nil
}

// isStructuralOperation reports whether one operation mutates a container
// (edit.rs is_structural_operation).
func isStructuralOperation(operation *EditOperation) bool {
	switch operation.Kind {
	case EditOperationInsertMappingEntry, EditOperationRemoveMappingEntry,
		EditOperationInsertSequenceElement, EditOperationRemoveSequenceElement,
		EditOperationInsertAlias:
		return true
	}
	return false
}

// validateDependencies checks duplicate targets and the one-structural-
// mutation-per-container rule (edit.rs validate_dependencies).
func (d *Document) validateDependencies(tx *EditTransaction) *EditFailure {
	targets := make(map[document.NodeRef]bool, len(tx.operations))
	containers := make(map[int]bool)
	for index := range tx.operations {
		operation := &tx.operations[index]
		var target document.NodeRef
		switch operation.Kind {
		case EditOperationReplaceScalar:
			target = operation.Scalar.Target
		case EditOperationRenameAnchor, EditOperationRemoveMappingEntry,
			EditOperationRemoveSequenceElement:
			target = operation.Target
		case EditOperationInsertMappingEntry, EditOperationInsertSequenceElement,
			EditOperationInsertAlias:
			target = operation.Mapping
		}
		if targets[target] {
			return &EditFailure{Kind: EditFailureDuplicateTarget}
		}
		targets[target] = true
		if isStructuralOperation(operation) {
			var container int
			var failure *EditFailure
			switch operation.Kind {
			case EditOperationInsertMappingEntry:
				container, failure = d.resolveNode(operation.Mapping, document.RoleYamlNode)
			case EditOperationInsertSequenceElement, EditOperationInsertAlias:
				container, failure = d.resolveNode(operation.Mapping, document.RoleYamlNode)
			case EditOperationRemoveMappingEntry:
				container, _, failure = d.resolveMappingEntry(operation.Target)
			case EditOperationRemoveSequenceElement:
				container, _, failure = d.resolveSequenceItem(operation.Target)
			}
			if failure != nil {
				return failure
			}
			if containers[container] {
				return &EditFailure{Kind: EditFailureStructuralContainerConflict}
			}
			containers[container] = true
		}
	}
	return nil
}

// resolveNode resolves one target node identity (edit.rs resolve_node).
func (d *Document) resolveNode(target document.NodeRef,
	role document.NodeRole) (int, *EditFailure) {
	if target.Snapshot() != d.SnapshotIdentity() {
		return 0, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if target.Role() != role {
		return 0, &EditFailure{Kind: EditFailureWrongRole}
	}
	index := int(target.Index())
	if index < 0 || index >= len(d.native.nodes) {
		return 0, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	if role == document.RoleYamlAnchorDefinition && !d.native.nodes[index].hasAnchor {
		return 0, &EditFailure{Kind: EditFailureWrongRole}
	}
	return index, nil
}

// resolveMappingEntry resolves one mapping entry target to its container
// and ordinal (edit.rs resolve_mapping_entry).
func (d *Document) resolveMappingEntry(target document.NodeRef) (int, int, *EditFailure) {
	if target.Snapshot() != d.SnapshotIdentity() {
		return 0, 0, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if target.Role() != document.RoleYamlMappingEntry {
		return 0, 0, &EditFailure{Kind: EditFailureWrongRole}
	}
	identity := target.Index()
	for container := range d.native.nodes {
		content := &d.native.nodes[container].content
		if content.kind != contentMapping {
			continue
		}
		for ordinal, entry := range content.entries {
			if entry.identity == identity {
				return container, ordinal, nil
			}
		}
	}
	return 0, 0, &EditFailure{Kind: EditFailureTargetNotFound}
}

// resolveSequenceItem resolves one sequence element target to its
// container and ordinal (edit.rs resolve_sequence_item).
func (d *Document) resolveSequenceItem(target document.NodeRef) (int, int, *EditFailure) {
	if target.Snapshot() != d.SnapshotIdentity() {
		return 0, 0, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if target.Role() != document.RoleYamlSequenceElement {
		return 0, 0, &EditFailure{Kind: EditFailureWrongRole}
	}
	identity := target.Index()
	for container := range d.native.nodes {
		content := &d.native.nodes[container].content
		if content.kind != contentSequence {
			continue
		}
		for ordinal, item := range content.items {
			if item.identity == identity {
				return container, ordinal, nil
			}
		}
	}
	return 0, 0, &EditFailure{Kind: EditFailureTargetNotFound}
}

// prepareOperation dispatches one operation to its byte-level preparation.
func (d *Document) prepareOperation(operation *EditOperation,
	diagnostics *[]*protocol.Diagnostic) ([]preparedEdit, *EditFailure) {
	switch operation.Kind {
	case EditOperationReplaceScalar:
		return d.prepareScalar(&operation.Scalar, diagnostics)
	case EditOperationRenameAnchor:
		return d.prepareAnchorRename(operation.Target, operation.Name)
	case EditOperationInsertMappingEntry:
		return d.prepareMappingInsertion(operation)
	case EditOperationRemoveMappingEntry:
		return d.prepareMappingRemoval(operation.Target)
	case EditOperationInsertSequenceElement:
		return d.prepareSequenceInsertion(operation)
	case EditOperationRemoveSequenceElement:
		return d.prepareSequenceRemoval(operation.Target)
	case EditOperationInsertAlias:
		return d.prepareAliasInsertion(operation)
	}
	return nil, &EditFailure{Kind: EditFailureWrongRole}
}

// scalarLiteralSpan returns the exact editable literal span of one scalar
// node (edit.rs scalar_literal_span).
func (d *Document) scalarLiteralSpan(index int) (document.Span, bool) {
	node := &d.native.nodes[index]
	if node.content.kind != contentScalar {
		return document.Span{}, false
	}
	style := node.content.scalar.style
	var pieceKind YamlSyntaxKind
	switch style {
	case ScalarStylePlain:
		pieceKind = SyntaxKindPlainScalar
	case ScalarStyleSingleQuoted:
		pieceKind = SyntaxKindSingleQuotedScalar
	case ScalarStyleDoubleQuoted:
		pieceKind = SyntaxKindDoubleQuotedScalar
	case ScalarStyleLiteral:
		pieceKind = SyntaxKindLiteralBlockHeader
	case ScalarStyleFolded:
		pieceKind = SyntaxKindFoldedBlockHeader
	}
	piece, ok := d.pieceWithin(node.span, pieceKind)
	if !ok {
		return document.Span{}, false
	}
	if style == ScalarStyleLiteral || style == ScalarStyleFolded {
		if content, ok := d.pieceWithin(node.span, SyntaxKindBlockScalarContent); ok &&
			content.StartByte() >= piece.EndByte() && content.EndByte() <= node.span.EndByte() {
			span, err := d.authority.Span(piece.StartByte(), content.EndByte())
			if err != nil {
				return document.Span{}, false
			}
			return span, true
		}
	}
	return piece, true
}

// pieceWithin returns the first piece of one kind within a raw span.
func (d *Document) pieceWithin(span document.Span, kind YamlSyntaxKind) (document.Span, bool) {
	pieces := d.index.Pieces()
	for _, piece := range pieces {
		if piece.span.StartByte() >= span.StartByte() && piece.span.EndByte() <= span.EndByte() &&
			d.kinds[pieceIndex(d, piece)] == kind {
			return piece.span, true
		}
	}
	return document.Span{}, false
}

// syntaxBetween finds one piece of one kind between two raw offsets; last
// selects the last match instead of the first.
func (d *Document) syntaxBetween(kind YamlSyntaxKind, start, end int,
	last bool) (document.Span, bool) {
	pieces := d.index.Pieces()
	found := document.Span{}
	ok := false
	for _, piece := range pieces {
		if piece.span.StartByte() >= start && piece.span.EndByte() <= end &&
			d.kinds[pieceIndex(d, piece)] == kind {
			found = piece.span
			ok = true
			if !last {
				return found, true
			}
		}
	}
	return found, ok
}

// pieceIndex resolves the kind index of one piece by its start byte.
func pieceIndex(d *Document, piece StructuralPiece) int {
	pieces := d.index.Pieces()
	for index, candidate := range pieces {
		if candidate.span.StartByte() == piece.span.StartByte() &&
			candidate.span.EndByte() == piece.span.EndByte() {
			return index
		}
	}
	return 0
}

// tagSpan returns the exact Tag piece inside one node span, when present.
func (d *Document) tagSpan(node document.Span) (document.Span, bool) {
	return d.pieceWithin(node, SyntaxKindTag)
}

// prepareScalar prepares one scalar replacement (edit.rs prepare_scalar).
func (d *Document) prepareScalar(replacement *ScalarReplacement,
	diagnostics *[]*protocol.Diagnostic) ([]preparedEdit, *EditFailure) {
	index, failure := d.resolveNode(replacement.Target, document.RoleYamlNode)
	if failure != nil {
		return nil, failure
	}
	node := &d.native.nodes[index]
	if node.content.kind != contentScalar {
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	literalSpan, ok := d.scalarLiteralSpan(index)
	if !ok {
		return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	if replacement.Kind == ScalarReplacementLiteral {
		if failure := d.validateLiteral(replacement.Literal); failure != nil {
			return nil, failure
		}
		return []preparedEdit{{
			oldSpan: literalSpan, replacement: append([]byte(nil), replacement.Literal...),
			mappingOld: replacement.Target, mappingPlan: mappingPlanNode, mappingIndex: index,
		}}, nil
	}
	value := replacement.Value
	switch value.(type) {
	case *core.Array, *core.Object, *core.EntryMapping:
		return nil, &EditFailure{Kind: EditFailureUnsupportedSemanticValue,
			ValueKind: value.Kind()}
	}
	if replacement.Policy == RepresentationPolicyExactLiteral {
		return nil, &EditFailure{Kind: EditFailureExactLiteralRequiresLiteralOperation}
	}
	canonical, materializationFailure := d.canonicalScalarFragment(value)
	if materializationFailure != nil {
		if materializationFailure.Kind == MaterializationUnrepresentable {
			return nil, &EditFailure{Kind: EditFailureUnsupportedSemanticValue,
				ValueKind: value.Kind()}
		}
		if materializationFailure.Kind == MaterializationResourceLimit {
			return nil, &EditFailure{Kind: EditFailureResourceLimit,
				LimitName: materializationFailure.LimitName}
		}
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	preserved := d.preservedLiteral(node, &canonical, value, replacement.Policy)
	switch replacement.Policy {
	case RepresentationPolicyPreserveCompatible:
		if preserved == "" {
			return nil, &EditFailure{Kind: EditFailureRepresentationIncompatible}
		}
		encoded, failure := d.encodeFragment(preserved)
		if failure != nil {
			return nil, failure
		}
		return []preparedEdit{{
			oldSpan: literalSpan, replacement: encoded,
			mappingOld: replacement.Target, mappingPlan: mappingPlanNode, mappingIndex: index,
		}}, nil
	case RepresentationPolicyCanonicalForProfile:
		return d.canonicalScalarEdits(index, replacement.Target, literalSpan, canonical, diagnostics)
	default: // PreserveElseCanonical
		if preserved != "" {
			encoded, failure := d.encodeFragment(preserved)
			if failure != nil {
				return nil, failure
			}
			return []preparedEdit{{
				oldSpan: literalSpan, replacement: encoded,
				mappingOld: replacement.Target, mappingPlan: mappingPlanNode, mappingIndex: index,
			}}, nil
		}
		d.pushFallbackDiagnostic(diagnostics, literalSpan)
		return d.canonicalScalarEdits(index, replacement.Target, literalSpan, canonical, diagnostics)
	}
}

// canonicalScalarFragment materializes one scalar value into the frozen
// `--- !!tag "literal"\n` form (edit.rs canonical_scalar_fragment).
func (d *Document) canonicalScalarFragment(value core.Value) (canonicalScalar, *MaterializationFailure) {
	request := document.NewMaterializationRequest(d.profile.ID(),
		document.NewMaterializationStyleId("yaml.canonical-flow", 1)).
		WithLimits(editMaterializationLimits(d.limits))
	result := MaterializeValue(value, request)
	if result.Failed != nil {
		return canonicalScalar{}, &result.Failed.Failure
	}
	text, _ := result.Complete.Document.Source().DecodedText()
	withoutPrefix, ok := strings.CutPrefix(text, "--- ")
	if !ok {
		return canonicalScalar{}, &MaterializationFailure{Kind: MaterializationFormationFailed}
	}
	withoutSuffix, ok := strings.CutSuffix(withoutPrefix, "\n")
	if !ok {
		return canonicalScalar{}, &MaterializationFailure{Kind: MaterializationFormationFailed}
	}
	tag, literal, found := strings.Cut(withoutSuffix, " ")
	if !found {
		return canonicalScalar{}, &MaterializationFailure{Kind: MaterializationFormationFailed}
	}
	return canonicalScalar{tag: tag, literal: literal}, nil
}

// canonicalScalar carries the frozen `!!tag` and quoted literal forms.
type canonicalScalar struct {
	tag     string
	literal string
}

// editMaterializationLimits maps the parse limits onto the edit
// materialization limits (edit.rs edit_materialization_limits).
func editMaterializationLimits(limits document.ParseLimits) document.MaterializationLimits {
	return document.MaterializationLimits{
		MaxInputNodes:        limits.MaxNodeCount,
		MaxOutputBytes:       limits.MaxSourceBytes,
		MaxDepth:             limits.MaxNestingDepth,
		MaxReportEntries:     limits.MaxDiagnostics,
		MaxProvenanceEntries: limits.MaxNodeCount * 4,
	}
}

// preservedLiteral retains the target category, style, and tag when the
// new value is compatible (edit.rs preserved_literal).
func (d *Document) preservedLiteral(node *nativeNode, canonical *canonicalScalar,
	value core.Value, policy RepresentationPolicy) string {
	oldKind := node.content.scalar.kind
	oldStyle := node.content.scalar.style
	oldTag := node.tag
	if oldKind != yamlKindOf(value) {
		return ""
	}
	shorthand, ok := shorthandTagURI(canonical.tag)
	if !ok || oldTag != shorthand {
		return ""
	}
	decoded, ok := d.decodeCanonicalLiteral(canonical.literal)
	if !ok {
		return ""
	}
	switch oldStyle {
	case ScalarStyleDoubleQuoted:
		return canonical.literal
	case ScalarStyleSingleQuoted:
		if strings.ContainsAny(decoded, "\n\r") {
			return ""
		}
		return "'" + strings.ReplaceAll(decoded, "'", "''") + "'"
	case ScalarStylePlain:
		source := decoded
		if nodeTagSpan, hasTag := d.tagSpan(node.span); hasTag {
			_ = nodeTagSpan
			source = canonical.tag + " " + decoded
		}
		document, failure := Parse([]byte(source), d.profile, document.DefaultParseLimits())
		if failure != nil {
			return ""
		}
		doc, _ := document.Document(0)
		scalar, ok := doc.Root().Scalar()
		if !ok || scalar.Kind() != oldKind || scalar.Canonical() != canonicalLiteralCanonical(d, canonical) {
			return ""
		}
		return decoded
	}
	return ""
}

// canonicalLiteralCanonical re-parses the canonical literal to its
// canonical content (the fragment's reparse contract).
func canonicalLiteralCanonical(d *Document, canonical *canonicalScalar) string {
	document, failure := Parse([]byte("--- "+canonical.tag+" "+canonical.literal+"\n"),
		d.profile, document.DefaultParseLimits())
	if failure != nil {
		return ""
	}
	doc, _ := document.Document(0)
	scalar, ok := doc.Root().Scalar()
	if !ok {
		return ""
	}
	return scalar.Canonical()
}

// decodeCanonicalLiteral parses one canonical quoted literal alone and
// returns its decoded content.
func (d *Document) decodeCanonicalLiteral(literal string) (string, bool) {
	document, failure := Parse([]byte("--- !!str "+literal+"\n"), Yaml12CoreV1,
		document.DefaultParseLimits())
	if failure != nil {
		return "", false
	}
	doc, _ := document.Document(0)
	scalar, ok := doc.Root().Scalar()
	if !ok {
		return "", false
	}
	return scalar.Decoded(), true
}

// yamlKindOf maps one PortableValue kind onto the YAML scalar category
// (edit.rs yaml_kind).
func yamlKindOf(value core.Value) YamlScalarKind {
	switch value.(type) {
	case core.Null:
		return ScalarKindNull
	case core.Boolean:
		return ScalarKindBoolean
	case core.Integer:
		return ScalarKindInteger
	case core.Decimal, core.BinaryFloat64:
		return ScalarKindFloat
	case core.String:
		return ScalarKindString
	case core.Bytes:
		return ScalarKindBinary
	case core.Date, core.OffsetDateTime:
		return ScalarKindTimestamp
	}
	return ScalarKindCustom
}

// shorthandTagURI resolves one frozen `!!` shorthand to its URI
// (edit.rs shorthand_tag_uri).
func shorthandTagURI(tag string) (string, bool) {
	switch tag {
	case "!!null":
		return tagNull, true
	case "!!bool":
		return tagBool, true
	case "!!int":
		return tagInt, true
	case "!!float":
		return tagFloat, true
	case "!!str":
		return tagStr, true
	case "!!timestamp":
		return tagTimestamp, true
	case "!!binary":
		return tagBinary, true
	}
	return "", false
}

// canonicalScalarEdits produces the canonical fallback spelling edits
// (edit.rs canonical_scalar_edits).
func (d *Document) canonicalScalarEdits(index int, target document.NodeRef,
	literalSpan document.Span, canonical canonicalScalar,
	diagnostics *[]*protocol.Diagnostic) ([]preparedEdit, *EditFailure) {
	nodeSpan := d.native.nodes[index].span
	if tagSpan, ok := d.pieceWithin(nodeSpan, SyntaxKindTag); ok {
		tagText, failure := d.encodeFragment(canonical.tag)
		if failure != nil {
			return nil, failure
		}
		literalText, failure := d.encodeFragment(canonical.literal)
		if failure != nil {
			return nil, failure
		}
		return []preparedEdit{
			{oldSpan: tagSpan, replacement: tagText},
			{oldSpan: literalSpan, replacement: literalText,
				mappingOld: target, mappingPlan: mappingPlanNode, mappingIndex: index},
		}, nil
	}
	text, failure := d.encodeFragment(canonical.tag + " " + canonical.literal)
	if failure != nil {
		return nil, failure
	}
	return []preparedEdit{{
		oldSpan: literalSpan, replacement: text,
		mappingOld: target, mappingPlan: mappingPlanNode, mappingIndex: index,
	}}, nil
}

// pushFallbackDiagnostic records the explicit canonical fallback
// (edit.rs push_fallback_diagnostic).
func (d *Document) pushFallbackDiagnostic(diagnostics *[]*protocol.Diagnostic,
	span document.Span) {
	diagnostic, err := protocol.NewDiagnostic("yaml.edit.canonical-fallback@1",
		protocol.CategoryEdit, protocol.SeverityInfo,
		&protocol.SourceLocation{StartByte: uint64(span.StartByte()), EndByte: uint64(span.EndByte())},
		nil, nil, nil, nil, 0, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		panic("yaml: unregistered edit diagnostic " + "yaml.edit.canonical-fallback@1")
	}
	*diagnostics = append(*diagnostics, diagnostic)
}

// validateLiteral checks one exact candidate literal (edit.rs
// validate_literal).
func (d *Document) validateLiteral(literal []byte) *EditFailure {
	if len(literal) == 0 {
		return &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	if d.source.EncodingFacts().Selected().Kind() != document.EncodingUtf8 &&
		d.source.EncodingFacts().Selected().Kind() != document.EncodingUtf16Le &&
		d.source.EncodingFacts().Selected().Kind() != document.EncodingUtf16Be {
		return &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	document, failure := Parse(literal, d.profile, d.limits)
	if failure != nil {
		return &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	if document.DocumentCount() != 1 {
		return &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	doc, _ := document.Document(0)
	if doc.Root().Kind() != NodeKindScalar {
		return &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	if _, anchored := doc.Root().Anchor(); anchored {
		return &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	for _, kind := range document.LosslessSyntaxKinds() {
		switch kind {
		case SyntaxKindTag, SyntaxKindAnchor, SyntaxKindAlias, SyntaxKindDirective,
			SyntaxKindDocumentStart, SyntaxKindDocumentEnd, SyntaxKindComment,
			SyntaxKindErrorRegion:
			return &EditFailure{Kind: EditFailureInvalidLiteral}
		}
	}
	return nil
}

// prepareAnchorRename prepares one anchor rename with its dependent alias
// updates (edit.rs prepare_anchor_rename).
func (d *Document) prepareAnchorRename(target document.NodeRef,
	name string) ([]preparedEdit, *EditFailure) {
	index, failure := d.resolveNode(target, document.RoleYamlAnchorDefinition)
	if failure != nil {
		return nil, failure
	}
	if failure := d.validateAnchorName(name); failure != nil {
		return nil, failure
	}
	node := &d.native.nodes[index]
	oldName := node.anchor
	if !node.hasAnchor {
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	anchorText, failure := d.encodeFragment("&" + name)
	if failure != nil {
		return nil, failure
	}
	edits := []preparedEdit{{
		oldSpan: node.anchorSpan, replacement: anchorText,
		mappingOld: target, mappingPlan: mappingPlanAnchor, mappingIndex: index,
	}}
	for ordinal := range d.native.aliases {
		alias := &d.native.aliases[ordinal]
		if alias.target == index && alias.name == oldName {
			aliasText, failure := d.encodeFragment("*" + name)
			if failure != nil {
				return nil, failure
			}
			aliasRef := d.authority.NodeRef(alias.identity, document.RoleYamlAlias)
			edits = append(edits, preparedEdit{
				oldSpan: alias.span, replacement: aliasText,
				mappingOld: aliasRef, mappingPlan: mappingPlanAlias, mappingIndex: ordinal,
			})
		}
	}
	return edits, nil
}

// validateAnchorName checks one new anchor name (edit.rs
// validate_anchor_name).
func (d *Document) validateAnchorName(name string) *EditFailure {
	if name == "" || len(name) > d.limits.MaxSourceBytes {
		return &EditFailure{Kind: EditFailureInvalidAnchorName}
	}
	limits := d.limits
	limits.MaxNestingDepth = 2
	limits.MaxTokenCount = 32
	limits.MaxNodeCount = 8
	document, failure := Parse([]byte("--- &"+name+" !!str \"x\"\n"), d.profile, limits)
	if failure != nil {
		return &EditFailure{Kind: EditFailureInvalidAnchorName}
	}
	doc, _ := document.Document(0)
	anchor, ok := doc.Root().Anchor()
	if !ok || anchor != name {
		return &EditFailure{Kind: EditFailureInvalidAnchorName}
	}
	return nil
}

// encodeFragment encodes one replacement text into the base encoding
// (edit.rs encode_fragment).
func (d *Document) encodeFragment(text string) ([]byte, *EditFailure) {
	switch d.source.EncodingFacts().Selected().Kind() {
	case document.EncodingUtf8:
		if len(text) > d.limits.MaxSourceBytes {
			return nil, &EditFailure{Kind: EditFailureResourceLimit,
				LimitName: "replacement-bytes"}
		}
		return []byte(text), nil
	case document.EncodingUtf16Le, document.EncodingUtf16Be:
		units := 0
		for _, character := range text {
			if character >= 0x10000 {
				units += 2
			} else {
				units++
			}
		}
		if units*2 > d.limits.MaxSourceBytes {
			return nil, &EditFailure{Kind: EditFailureResourceLimit,
				LimitName: "replacement-bytes"}
		}
		output := make([]byte, 0, units*2)
		for _, character := range text {
			if character >= 0x10000 {
				value := character - 0x10000
				output = appendUTF16(output, uint16(0xD800+value>>10),
					d.source.EncodingFacts().Selected().Kind() == document.EncodingUtf16Le)
				output = appendUTF16(output, uint16(0xDC00+value&0x3FF),
					d.source.EncodingFacts().Selected().Kind() == document.EncodingUtf16Le)
				continue
			}
			output = appendUTF16(output, uint16(character),
				d.source.EncodingFacts().Selected().Kind() == document.EncodingUtf16Le)
		}
		return output, nil
	}
	return nil, &EditFailure{Kind: EditFailureInvalidLiteral}
}

// prepareMappingInsertion prepares one mapping entry insertion
// (edit.rs prepare_mapping_insertion).
func (d *Document) prepareMappingInsertion(operation *EditOperation) ([]preparedEdit, *EditFailure) {
	index, failure := d.resolveNode(operation.Mapping, document.RoleYamlNode)
	if failure != nil {
		return nil, failure
	}
	node := &d.native.nodes[index]
	if node.content.kind != contentMapping {
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	spans := d.mappingEntrySpans(index)
	ordinal, failure := d.collectionPlacement(index, spans, operation.Placement)
	if failure != nil {
		return nil, failure
	}
	keyFragment, keyFailure := d.canonicalValueFragment(operation.Key)
	if keyFailure != nil {
		if keyFailure.Kind == MaterializationUnrepresentable {
			return nil, &EditFailure{Kind: EditFailureUnsupportedInsertedValue,
				ValueKind: operation.Key.Kind()}
		}
		if keyFailure.Kind == MaterializationResourceLimit {
			return nil, &EditFailure{Kind: EditFailureResourceLimit,
				LimitName: keyFailure.LimitName}
		}
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	valueFragment, valueFailure := d.canonicalValueFragment(operation.Value)
	if valueFailure != nil {
		if valueFailure.Kind == MaterializationUnrepresentable {
			return nil, &EditFailure{Kind: EditFailureUnsupportedInsertedValue,
				ValueKind: operation.Value.Kind()}
		}
		if valueFailure.Kind == MaterializationResourceLimit {
			return nil, &EditFailure{Kind: EditFailureResourceLimit,
				LimitName: valueFailure.LimitName}
		}
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	fragment := "? " + keyFragment + " : " + valueFragment
	blockLines := []string{"? " + keyFragment, ": " + valueFragment}
	return d.prepareCollectionInsertion(index, spans, ordinal, fragment, blockLines,
		SyntaxKindFlowMappingStart, SyntaxKindFlowMappingEnd)
}

// canonicalValueFragment materializes one full value fragment in the
// frozen canonical form.
func (d *Document) canonicalValueFragment(value core.Value) (string, *MaterializationFailure) {
	canonical, failure := d.canonicalScalarFragment(value)
	if failure != nil {
		return "", failure
	}
	return canonical.tag + " " + canonical.literal, nil
}

// collectionPlacement resolves one placement into a container ordinal
// (edit.rs mapping_placement / sequence_placement).
func (d *Document) collectionPlacement(container int, spans []document.Span,
	placement AssociationPlacement) (int, *EditFailure) {
	switch placement.Kind {
	case "Start":
		return 0, nil
	case "End":
		return len(spans), nil
	case "Before", "After":
		targetContainer, ordinal, failure := d.resolveAssociation(placement.Anchor)
		if failure != nil {
			return 0, failure
		}
		if targetContainer != container {
			return 0, &EditFailure{Kind: EditFailureInvalidPlacement}
		}
		if placement.Kind == "After" {
			ordinal++
		}
		return ordinal, nil
	}
	return 0, &EditFailure{Kind: EditFailureInvalidPlacement}
}

// resolveAssociation resolves one placement anchor association.
func (d *Document) resolveAssociation(target document.NodeRef) (int, int, *EditFailure) {
	switch target.Role() {
	case document.RoleYamlMappingEntry:
		return d.resolveMappingEntry(target)
	case document.RoleYamlSequenceElement:
		return d.resolveSequenceItem(target)
	}
	return 0, 0, &EditFailure{Kind: EditFailureInvalidPlacement}
}

// mappingEntrySpans returns the ordered association spans of one mapping.
func (d *Document) mappingEntrySpans(index int) []document.Span {
	content := &d.native.nodes[index].content
	spans := make([]document.Span, 0, len(content.entries))
	for _, entry := range content.entries {
		spans = append(spans, entry.span)
	}
	return spans
}

// sequenceItemSpans returns the ordered element spans of one sequence.
func (d *Document) sequenceItemSpans(index int) []document.Span {
	content := &d.native.nodes[index].content
	spans := make([]document.Span, 0, len(content.items))
	for _, item := range content.items {
		spans = append(spans, item.span)
	}
	return spans
}

// prepareCollectionInsertion prepares one flow or block insertion
// (edit.rs prepare_collection_insertion).
func (d *Document) prepareCollectionInsertion(container int, spans []document.Span,
	ordinal int, flowFragment string, blockLines []string,
	flowStart, flowEnd YamlSyntaxKind) ([]preparedEdit, *EditFailure) {
	insertion, failure := d.collectionInsertionPoint(container, spans, ordinal, flowStart, flowEnd)
	if failure != nil {
		return nil, failure
	}
	return d.prepareCollectionInsertionAt(container, spans, ordinal, flowFragment,
		blockLines, flowStart, insertion)
}

// collectionInsertionPoint computes the exact insertion byte
// (edit.rs collection_insertion_point).
func (d *Document) collectionInsertionPoint(container int, spans []document.Span,
	ordinal int, flowStart, flowEnd YamlSyntaxKind) (int, *EditFailure) {
	if ordinal > len(spans) {
		return 0, &EditFailure{Kind: EditFailureInvalidPlacement}
	}
	if d.collectionIsFlow(container, flowStart) {
		if ordinal < len(spans) {
			return spans[ordinal].StartByte(), nil
		}
		if len(spans) > 0 {
			return spans[len(spans)-1].EndByte(), nil
		}
		nodeSpan := d.native.nodes[container].span
		if end, ok := d.syntaxBetween(flowEnd, nodeSpan.StartByte(), nodeSpan.EndByte(), true); ok {
			return end.StartByte(), nil
		}
		return 0, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	if ordinal < len(spans) {
		owned, ok := d.blockOwnedSpan(spans[ordinal])
		if !ok {
			return 0, &EditFailure{Kind: EditFailureIncompleteTarget}
		}
		return owned.StartByte(), nil
	}
	if len(spans) > 0 {
		owned, ok := d.blockOwnedSpan(spans[len(spans)-1])
		if !ok {
			return 0, &EditFailure{Kind: EditFailureIncompleteTarget}
		}
		return owned.EndByte(), nil
	}
	return 0, &EditFailure{Kind: EditFailureIncompleteTarget}
}

// collectionIsFlow reports whether one collection is a flow collection.
func (d *Document) collectionIsFlow(container int, flowStart YamlSyntaxKind) bool {
	nodeSpan := d.native.nodes[container].span
	_, ok := d.syntaxBetween(flowStart, nodeSpan.StartByte(), nodeSpan.EndByte(), false)
	return ok
}

// prepareCollectionInsertionAt emits the separator-aware insertion text
// (edit.rs prepare_collection_insertion_at).
func (d *Document) prepareCollectionInsertionAt(container int, spans []document.Span,
	ordinal int, flowFragment string, blockLines []string, flowStart YamlSyntaxKind,
	insertion int) ([]preparedEdit, *EditFailure) {
	if d.collectionIsFlow(container, flowStart) {
		var text string
		switch {
		case len(spans) == 0:
			text = flowFragment
		case ordinal < len(spans):
			text = flowFragment + ", "
		default:
			text = ", " + flowFragment
		}
		encoded, failure := d.encodeFragment(text)
		if failure != nil {
			return nil, failure
		}
		span, err := d.authority.Span(insertion, insertion)
		if err != nil {
			return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
		}
		return []preparedEdit{{oldSpan: span, replacement: encoded,
			mappingOld: d.nodeRef(container), mappingPlan: mappingPlanNode,
			mappingIndex: container}}, nil
	}
	var reference document.Span
	if ordinal < len(spans) {
		reference = spans[ordinal]
	} else if len(spans) > 0 {
		reference = spans[len(spans)-1]
	} else {
		return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	owned, ok := d.blockOwnedSpan(reference)
	if !ok {
		return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	indent := d.lineIndentRaw(owned.StartByte())
	newline := d.nearestNewline(insertion)
	suffixNewline := ordinal < len(spans) || d.rawEndsWithBreak(owned)
	var builder strings.Builder
	if ordinal == len(spans) && !suffixNewline {
		builder.WriteString(newline)
	}
	for index, line := range blockLines {
		for count := 0; count < indent; count++ {
			builder.WriteString(" ")
		}
		builder.WriteString(line)
		if index+1 < len(blockLines) || suffixNewline {
			builder.WriteString(newline)
		}
	}
	encoded, failure := d.encodeFragment(builder.String())
	if failure != nil {
		return nil, failure
	}
	span, err := d.authority.Span(insertion, insertion)
	if err != nil {
		return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	return []preparedEdit{{oldSpan: span, replacement: encoded,
		mappingOld: d.nodeRef(container), mappingPlan: mappingPlanNode,
		mappingIndex: container}}, nil
}

// blockOwnedSpan expands one association span to its whole owned line
// (edit.rs block_owned_span).
func (d *Document) blockOwnedSpan(span document.Span) (document.Span, bool) {
	start, ok := d.lineStartRaw(span.StartByte())
	if !ok {
		return document.Span{}, false
	}
	end := span.EndByte()
	lineStartOfEnd, ok := d.lineStartRaw(span.EndByte())
	if !ok {
		return document.Span{}, false
	}
	if lineStartOfEnd == span.EndByte() && span.EndByte() > start {
		// The occurrence already ends at a line boundary.
	} else {
		lineEnd, ok := d.lineEndRaw(span.EndByte())
		if !ok {
			return document.Span{}, false
		}
		end = lineEnd
	}
	span, err := d.authority.Span(start, end)
	if err != nil {
		return document.Span{}, false
	}
	return span, true
}

// lineStartRaw returns the raw byte of the line start containing one raw
// offset.
func (d *Document) lineStartRaw(offset int) (int, bool) {
	return d.rawLineBoundary(offset, true)
}

// lineEndRaw returns the raw byte just after the line break ending one
// raw offset's line.
func (d *Document) lineEndRaw(offset int) (int, bool) {
	return d.rawLineBoundary(offset, false)
}

// rawLineBoundary resolves the raw line start or end of one raw offset.
func (d *Document) rawLineBoundary(offset int, start bool) (int, bool) {
	position, err := d.source.DecodedPosition(offset)
	if err != nil {
		return 0, false
	}
	text, _ := d.source.DecodedText()
	decoded := position.DecodedUTF8Byte
	if start {
		for decoded > 0 {
			character := text[decoded-1]
			if character == '\r' || character == '\n' {
				break
			}
			decoded--
		}
	} else {
		for decoded < len(text) {
			character := text[decoded]
			if character == '\r' || character == '\n' {
				decoded++
				if decoded < len(text) && text[decoded-1] == '\r' && text[decoded] == '\n' {
					decoded++
				}
				break
			}
			decoded++
		}
	}
	raw, err := d.source.RawByteAt(document.NewUtf8ByteOffset(decoded))
	if err != nil {
		return 0, false
	}
	return raw, true
}

// lineIndentRaw returns the leading space count of the line containing one
// raw offset.
func (d *Document) lineIndentRaw(offset int) int {
	position, err := d.source.DecodedPosition(offset)
	if err != nil {
		return 0
	}
	text, _ := d.source.DecodedText()
	decoded := position.DecodedUTF8Byte
	for decoded > 0 {
		character := text[decoded-1]
		if character == '\r' || character == '\n' {
			break
		}
		decoded--
	}
	count := 0
	for decoded+count < len(text) && text[decoded+count] == ' ' {
		count++
	}
	return count
}

// nearestNewline returns the decoded text of the Newline piece closest to
// one raw offset (edit.rs nearest_newline).
func (d *Document) nearestNewline(offset int) string {
	pieces := d.index.Pieces()
	best := ""
	bestDistance := int(^uint(0) >> 1)
	found := false
	for index, piece := range pieces {
		if d.kinds[index] != SyntaxKindNewline {
			continue
		}
		distance := piece.span.StartByte() - offset
		if distance < 0 {
			distance = -distance
		}
		if !found || distance < bestDistance {
			bestDistance = distance
			best = d.rawSlice(piece.span)
			found = true
		}
	}
	if !found {
		return "\n"
	}
	return best
}

// rawSlice returns the exact raw bytes of one span.
func (d *Document) rawSlice(span document.Span) string {
	source := d.source.Bytes()
	return string(source[span.StartByte():span.EndByte()])
}

// rawEndsWithBreak reports whether one raw span's decoded text ends with a
// line break.
func (d *Document) rawEndsWithBreak(span document.Span) bool {
	text := d.rawSlice(span)
	return strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\r")
}

// prepareMappingRemoval prepares one mapping entry removal
// (edit.rs prepare_mapping_removal).
func (d *Document) prepareMappingRemoval(target document.NodeRef) ([]preparedEdit, *EditFailure) {
	container, ordinal, failure := d.resolveMappingEntry(target)
	if failure != nil {
		return nil, failure
	}
	spans := d.mappingEntrySpans(container)
	owned, failure := d.collectionRemovalSpan(container, spans, ordinal,
		SyntaxKindFlowMappingStart, SyntaxKindFlowMappingEnd)
	if failure != nil {
		return nil, failure
	}
	content := &d.native.nodes[container].content
	entry := content.entries[ordinal]
	pairs := [][2]interface{}{
		{entry.key, entry.keyAlias},
		{entry.value, entry.valueAlias},
	}
	var ownedNodes []int
	if failure := d.validateRemovalDependencies(owned, pairs, &ownedNodes); failure != nil {
		return nil, failure
	}
	var replacement []byte
	if len(spans) == 1 && !d.collectionIsFlow(container, SyntaxKindFlowMappingStart) {
		text, ok := d.emptyBlockReplacement(owned, spans[ordinal], "{}")
		if !ok {
			return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
		}
		replacement = []byte(text)
	}
	return []preparedEdit{{
		oldSpan: owned, replacement: replacement,
		mappingOld: target, mappingPlan: mappingPlanRemoved,
	}}, nil
}

// prepareSequenceRemoval prepares one sequence element removal
// (edit.rs prepare_sequence_removal).
func (d *Document) prepareSequenceRemoval(target document.NodeRef) ([]preparedEdit, *EditFailure) {
	container, ordinal, failure := d.resolveSequenceItem(target)
	if failure != nil {
		return nil, failure
	}
	spans := d.sequenceItemSpans(container)
	owned, failure := d.collectionRemovalSpan(container, spans, ordinal,
		SyntaxKindFlowSequenceStart, SyntaxKindFlowSequenceEnd)
	if failure != nil {
		return nil, failure
	}
	content := &d.native.nodes[container].content
	item := content.items[ordinal]
	pairs := [][2]interface{}{{item.node, item.alias}}
	var ownedNodes []int
	if failure := d.validateRemovalDependencies(owned, pairs, &ownedNodes); failure != nil {
		return nil, failure
	}
	var replacement []byte
	if len(spans) == 1 && !d.collectionIsFlow(container, SyntaxKindFlowSequenceStart) {
		text, ok := d.emptyBlockReplacement(owned, spans[ordinal], "[]")
		if !ok {
			return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
		}
		replacement = []byte(text)
	}
	return []preparedEdit{{
		oldSpan: owned, replacement: replacement,
		mappingOld: target, mappingPlan: mappingPlanRemoved,
	}}, nil
}

// collectionRemovalSpan computes the exact owned removal region
// (edit.rs collection_removal_span).
func (d *Document) collectionRemovalSpan(container int, spans []document.Span,
	ordinal int, flowStart, flowEnd YamlSyntaxKind) (document.Span, *EditFailure) {
	if ordinal >= len(spans) {
		return document.Span{}, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	target := spans[ordinal]
	if !d.collectionIsFlow(container, flowStart) {
		owned, ok := d.blockOwnedSpan(target)
		if !ok {
			return document.Span{}, &EditFailure{Kind: EditFailureIncompleteTarget}
		}
		return owned, nil
	}
	if len(spans) == 1 {
		return target, nil
	}
	if ordinal+1 < len(spans) {
		next := spans[ordinal+1]
		comma, ok := d.syntaxBetween(SyntaxKindFlowEntry, target.EndByte(), next.StartByte(), false)
		if !ok {
			return document.Span{}, &EditFailure{Kind: EditFailureIncompleteTarget}
		}
		span, err := d.authority.Span(target.StartByte(), next.StartByte())
		if err != nil {
			return document.Span{}, &EditFailure{Kind: EditFailureIncompleteTarget}
		}
		_ = comma
		return span, nil
	}
	previous := spans[ordinal-1]
	comma, ok := d.syntaxBetween(SyntaxKindFlowEntry, previous.EndByte(), target.StartByte(), true)
	if !ok {
		return document.Span{}, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	span, err := d.authority.Span(comma.StartByte(), target.EndByte())
	if err != nil {
		return document.Span{}, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	return span, nil
}

// validateRemovalDependencies checks the anchor-dependency rule for one
// removal (edit.rs validate_removal_dependencies).
func (d *Document) validateRemovalDependencies(owned document.Span,
	pairs [][2]interface{}, ownedNodes *[]int) *EditFailure {
	collected := make(map[int]bool)
	for _, root := range d.native.documents {
		d.collectOwnedNodes(root.root, collected)
	}
	for _, alias := range d.native.aliases {
		if collected[alias.target] && (alias.span.StartByte() < owned.StartByte() ||
			alias.span.EndByte() > owned.EndByte()) {
			return &EditFailure{Kind: EditFailureAnchorDependency}
		}
	}
	_ = pairs
	_ = ownedNodes
	return nil
}

// collectOwnedNodes gathers the nodes owned by one root, never recursing
// through aliased edges (edit.rs collect_owned_nodes).
func (d *Document) collectOwnedNodes(index int, collected map[int]bool) {
	if collected[index] {
		return
	}
	collected[index] = true
	node := &d.native.nodes[index]
	switch node.content.kind {
	case contentSequence:
		for _, item := range node.content.items {
			if item.alias == nil {
				d.collectOwnedNodes(item.node, collected)
			}
		}
	case contentMapping:
		for _, entry := range node.content.entries {
			if entry.keyAlias == nil {
				d.collectOwnedNodes(entry.key, collected)
			}
			if entry.valueAlias == nil {
				d.collectOwnedNodes(entry.value, collected)
			}
		}
	}
}

// emptyBlockReplacement renders the empty `{}` / `[]` line replacement
// preserving the trailing comment (edit.rs empty_block_replacement).
func (d *Document) emptyBlockReplacement(owned document.Span, occurrence document.Span,
	empty string) (string, bool) {
	indent := d.lineIndentRaw(owned.StartByte())
	var tail string
	if occurrence.EndByte() < owned.EndByte() {
		// The trailing comment region after the occurrence is owned by the
		// line and preserved.
		tail = d.rawSlice(owned)[occurrence.EndByte()-owned.StartByte():]
	} else {
		text := d.rawSlice(owned)
		switch {
		case strings.HasSuffix(text, "\r\n"):
			tail = "\r\n"
		case strings.HasSuffix(text, "\n"):
			tail = "\n"
		case strings.HasSuffix(text, "\r"):
			tail = "\r"
		}
	}
	var builder strings.Builder
	for count := 0; count < indent; count++ {
		builder.WriteString(" ")
	}
	builder.WriteString(empty)
	builder.WriteString(tail)
	return builder.String(), true
}

// prepareSequenceInsertion prepares one sequence element insertion
// (edit.rs prepare_sequence_insertion).
func (d *Document) prepareSequenceInsertion(operation *EditOperation) ([]preparedEdit, *EditFailure) {
	index, failure := d.resolveNode(operation.Mapping, document.RoleYamlNode)
	if failure != nil {
		return nil, failure
	}
	node := &d.native.nodes[index]
	if node.content.kind != contentSequence {
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	spans := d.sequenceItemSpans(index)
	ordinal, failure := d.collectionPlacement(index, spans, operation.Placement)
	if failure != nil {
		return nil, failure
	}
	fragment, fragmentFailure := d.canonicalValueFragment(operation.Value)
	if fragmentFailure != nil {
		if fragmentFailure.Kind == MaterializationUnrepresentable {
			return nil, &EditFailure{Kind: EditFailureUnsupportedInsertedValue,
				ValueKind: operation.Value.Kind()}
		}
		if fragmentFailure.Kind == MaterializationResourceLimit {
			return nil, &EditFailure{Kind: EditFailureResourceLimit,
				LimitName: fragmentFailure.LimitName}
		}
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return d.prepareCollectionInsertion(index, spans, ordinal, fragment,
		[]string{"- " + fragment}, SyntaxKindFlowSequenceStart, SyntaxKindFlowSequenceEnd)
}

// prepareAliasInsertion prepares one alias insertion (edit.rs
// prepare_alias_insertion).
func (d *Document) prepareAliasInsertion(operation *EditOperation) ([]preparedEdit, *EditFailure) {
	sequence, failure := d.resolveNode(operation.Mapping, document.RoleYamlNode)
	if failure != nil {
		return nil, failure
	}
	sequenceNode := &d.native.nodes[sequence]
	if sequenceNode.content.kind != contentSequence {
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	anchorIndex, failure := d.resolveNode(operation.Anchor, document.RoleYamlAnchorDefinition)
	if failure != nil {
		return nil, failure
	}
	anchorNode := &d.native.nodes[anchorIndex]
	if !anchorNode.hasAnchor {
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	spans := d.sequenceItemSpans(sequence)
	ordinal, failure := d.collectionPlacement(sequence, spans, operation.Placement)
	if failure != nil {
		return nil, failure
	}
	insertion, failure := d.collectionInsertionPoint(sequence, spans, ordinal,
		SyntaxKindFlowSequenceStart, SyntaxKindFlowSequenceEnd)
	if failure != nil {
		return nil, failure
	}
	if failure := d.validateVisibleAnchor(sequence, anchorIndex, insertion); failure != nil {
		return nil, failure
	}
	name := anchorNode.anchor
	return d.prepareCollectionInsertionAt(sequence, spans, ordinal, "*"+name,
		[]string{"- *" + name}, SyntaxKindFlowSequenceStart, insertion)
}

// validateVisibleAnchor checks the earlier-visible-anchor rule (edit.rs
// validate_visible_anchor).
func (d *Document) validateVisibleAnchor(sequence, anchorIndex, insertion int) *EditFailure {
	anchorNode := &d.native.nodes[anchorIndex]
	anchorSpan := anchorNode.anchorSpan
	sequenceSpan := d.native.nodes[sequence].span
	var documentSpan document.Span
	for index := range d.native.documents {
		doc := &d.native.documents[index]
		if sequenceSpan.StartByte() >= doc.span.StartByte() &&
			sequenceSpan.EndByte() <= doc.span.EndByte() {
			documentSpan = doc.span
			break
		}
	}
	if documentSpan.Snapshot() == 0 {
		return &EditFailure{Kind: EditFailureAnchorNotVisible}
	}
	if anchorSpan.EndByte() > insertion ||
		anchorSpan.StartByte() < documentSpan.StartByte() ||
		anchorSpan.EndByte() > documentSpan.EndByte() {
		return &EditFailure{Kind: EditFailureAnchorNotVisible}
	}
	name := anchorNode.anchor
	lastIndex := -1
	lastEnd := -1
	for index := range d.native.nodes {
		node := &d.native.nodes[index]
		if node.hasAnchor && node.anchor == name &&
			node.anchorSpan.EndByte() <= insertion &&
			node.anchorSpan.StartByte() >= documentSpan.StartByte() &&
			node.anchorSpan.EndByte() <= documentSpan.EndByte() {
			if node.anchorSpan.EndByte() > lastEnd {
				lastEnd = node.anchorSpan.EndByte()
				lastIndex = index
			}
		}
	}
	if lastIndex != anchorIndex {
		return &EditFailure{Kind: EditFailureAnchorNotVisible}
	}
	return nil
}

// validateCandidate is the non-structural reparse validation (edit.rs
// validate_candidate).
func (d *Document) validateCandidate(newDocument *Document, tx *EditTransaction) *EditFailure {
	var scalarTargets map[int]bool
	var renameTargets map[int]string
	for index := range tx.operations {
		operation := &tx.operations[index]
		switch operation.Kind {
		case EditOperationReplaceScalar:
			if scalarTargets == nil {
				scalarTargets = make(map[int]bool)
			}
			scalarTargets[int(operation.Scalar.Target.Index())] = true
		case EditOperationRenameAnchor:
			if renameTargets == nil {
				renameTargets = make(map[int]string)
			}
			renameTargets[int(operation.Target.Index())] = operation.Name
		}
	}
	if len(newDocument.native.documents) != len(d.native.documents) ||
		len(newDocument.native.nodes) != len(d.native.nodes) ||
		len(newDocument.native.aliases) != len(d.native.aliases) {
		return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	for ordinal := range d.native.documents {
		if d.native.documents[ordinal].root != newDocument.native.documents[ordinal].root {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
	}
	for index := range d.native.nodes {
		old := &d.native.nodes[index]
		new := &newDocument.native.nodes[index]
		if newName, renamed := renameTargets[index]; renamed {
			if !new.hasAnchor || new.anchor != newName {
				return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
			}
		} else if new.hasAnchor != old.hasAnchor ||
			(old.hasAnchor && new.anchor != old.anchor) {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		if !sameTopology(old, new) {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		if !scalarTargets[index] {
			if old.tag != new.tag || !sameScalarSemantics(old, new) {
				return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
			}
		}
	}
	for ordinal := range d.native.aliases {
		old := &d.native.aliases[ordinal]
		new := &newDocument.native.aliases[ordinal]
		if old.target != new.target {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		if newName, renamed := renameTargets[old.target]; renamed {
			if new.name != newName {
				return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
			}
		} else if new.name != old.name {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
	}
	return nil
}

// sameTopology compares the structural shape of two nodes.
func sameTopology(old, new *nativeNode) bool {
	if old.content.kind != new.content.kind {
		return false
	}
	switch old.content.kind {
	case contentSequence:
		if len(old.content.items) != len(new.content.items) {
			return false
		}
		for index := range old.content.items {
			if old.content.items[index].node != new.content.items[index].node ||
				(old.content.items[index].alias == nil) != (new.content.items[index].alias == nil) {
				return false
			}
		}
	case contentMapping:
		if len(old.content.entries) != len(new.content.entries) {
			return false
		}
		for index := range old.content.entries {
			if old.content.entries[index].key != new.content.entries[index].key ||
				old.content.entries[index].value != new.content.entries[index].value ||
				(old.content.entries[index].keyAlias == nil) != (new.content.entries[index].keyAlias == nil) ||
				(old.content.entries[index].valueAlias == nil) != (new.content.entries[index].valueAlias == nil) {
				return false
			}
		}
	}
	return true
}

// sameScalarSemantics compares the canonical scalar semantics of two
// nodes.
func sameScalarSemantics(old, new *nativeNode) bool {
	if old.content.kind != contentScalar || new.content.kind != contentScalar {
		return true
	}
	return old.content.scalar.canonical == new.content.scalar.canonical &&
		old.content.scalar.kind == new.content.scalar.kind
}

// validateStructuralCandidate is the structural reparse validation with
// the representation-graph isomorphism (edit.rs validate_structural_candidate).
func (d *Document) validateStructuralCandidate(newDocument *Document,
	tx *EditTransaction) *EditFailure {
	expected, failure := d.replayModel(tx)
	if failure != nil {
		return failure
	}
	candidate := modelFromDocument(newDocument)
	if failure := compareModels(expected, candidate, false); failure != nil {
		return failure
	}
	return nil
}

// validationModel is the expected representation model of one edited
// document.
type validationModel struct {
	nodes []validationNode
	roots []int
}

type validationNode struct {
	tag     string
	anchor  string
	content validationContent
}

type validationContent struct {
	kind       int
	scalarKind YamlScalarKind
	canonical  string
	items      []validationEdge
	entries    []validationEntry
}

type validationEdge struct {
	target int
	alias  *validationAlias
}

type validationAlias struct {
	name        string
	sourceAlias *int
}

type validationEntry struct {
	key   validationEdge
	value validationEdge
}

// modelFromDocument builds the representation model of one document.
func modelFromDocument(doc *Document) *validationModel {
	model := &validationModel{}
	model.nodes = make([]validationNode, len(doc.native.nodes))
	for index := range doc.native.nodes {
		node := &doc.native.nodes[index]
		content := validationContent{kind: node.content.kind}
		switch node.content.kind {
		case contentScalar:
			content.scalarKind = node.content.scalar.kind
			content.canonical = node.content.scalar.canonical
		case contentSequence:
			for _, item := range node.content.items {
				content.items = append(content.items, validationEdge{
					target: item.node, alias: modelAlias(doc, item.alias),
				})
			}
		case contentMapping:
			for _, entry := range node.content.entries {
				content.entries = append(content.entries, validationEntry{
					key:   validationEdge{target: entry.key, alias: modelAlias(doc, entry.keyAlias)},
					value: validationEdge{target: entry.value, alias: modelAlias(doc, entry.valueAlias)},
				})
			}
		}
		anchor := ""
		if node.hasAnchor {
			anchor = node.anchor
		}
		model.nodes[index] = validationNode{tag: node.tag, anchor: anchor, content: content}
	}
	for _, doc := range doc.native.documents {
		model.roots = append(model.roots, doc.root)
	}
	return model
}

func modelAlias(doc *Document, ordinal *int) *validationAlias {
	if ordinal == nil {
		return nil
	}
	alias := &doc.native.aliases[*ordinal]
	value := *ordinal
	return &validationAlias{name: alias.name, sourceAlias: &value}
}

// replayModel applies the transaction operations to the base model
// (edit.rs ValidationModel replay).
func (d *Document) replayModel(tx *EditTransaction) (*validationModel, *EditFailure) {
	model := modelFromDocument(d)
	for index := range tx.operations {
		operation := &tx.operations[index]
		switch operation.Kind {
		case EditOperationReplaceScalar:
			index := int(operation.Scalar.Target.Index())
			if model.nodes[index].content.kind != contentScalar {
				return nil, &EditFailure{Kind: EditFailureWrongRole}
			}
			if operation.Scalar.Kind == ScalarReplacementLiteral {
				// Any scalar content is accepted for literal replacements.
				model.nodes[index].content.scalarKind = ScalarKindCustom
				model.nodes[index].content.canonical = "*wildcard*"
				model.nodes[index].tag = tagStr
				continue
			}
			imported, failure := d.valueModel(operation.Scalar.Value)
			if failure != nil {
				return nil, failure
			}
			if len(imported.nodes) == 0 {
				return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
			}
			root := imported.roots[0]
			model.nodes[index].tag = imported.nodes[root].tag
			model.nodes[index].content = imported.nodes[root].content
		case EditOperationRenameAnchor:
			index := int(operation.Target.Index())
			if model.nodes[index].anchor == "" {
				return nil, &EditFailure{Kind: EditFailureWrongRole}
			}
			oldName := model.nodes[index].anchor
			model.nodes[index].anchor = operation.Name
			for nodeIndex := range model.nodes {
				content := &model.nodes[nodeIndex].content
				for itemIndex := range content.items {
					renameModelAlias(&content.items[itemIndex], index, oldName, operation.Name)
				}
				for entryIndex := range content.entries {
					renameModelAlias(&content.entries[entryIndex].key, index, oldName, operation.Name)
					renameModelAlias(&content.entries[entryIndex].value, index, oldName, operation.Name)
				}
			}
		case EditOperationInsertMappingEntry:
			container := int(operation.Mapping.Index())
			if model.nodes[container].content.kind != contentMapping {
				return nil, &EditFailure{Kind: EditFailureWrongRole}
			}
			spans := d.mappingEntrySpans(container)
			ordinal, failure := d.collectionPlacement(container, spans, operation.Placement)
			if failure != nil {
				return nil, failure
			}
			keyModel, failure := d.valueModel(operation.Key)
			if failure != nil {
				return nil, failure
			}
			valueModel, failure := d.valueModel(operation.Value)
			if failure != nil {
				return nil, failure
			}
			keyRoot := keyModel.roots[0]
			valueRoot := valueModel.roots[0]
			keyOffset := len(model.nodes)
			valueOffset := keyOffset + len(keyModel.nodes)
			model = mergeModel(model, keyModel, keyOffset)
			model = mergeModel(model, valueModel, valueOffset)
			entry := validationEntry{
				key:   validationEdge{target: keyOffset + keyRoot},
				value: validationEdge{target: valueOffset + valueRoot},
			}
			content := &model.nodes[container].content
			content.entries = insertValidationEntry(content.entries, ordinal, entry)
		case EditOperationRemoveMappingEntry:
			container, ordinal, failure := d.resolveMappingEntry(operation.Target)
			if failure != nil {
				return nil, failure
			}
			content := &model.nodes[container].content
			content.entries = removeValidationEntry(content.entries, ordinal)
		case EditOperationInsertSequenceElement:
			container := int(operation.Mapping.Index())
			if model.nodes[container].content.kind != contentSequence {
				return nil, &EditFailure{Kind: EditFailureWrongRole}
			}
			spans := d.sequenceItemSpans(container)
			ordinal, failure := d.collectionPlacement(container, spans, operation.Placement)
			if failure != nil {
				return nil, failure
			}
			valueModel, failure := d.valueModel(operation.Value)
			if failure != nil {
				return nil, failure
			}
			valueRoot := valueModel.roots[0]
			valueOffset := len(model.nodes)
			model = mergeModel(model, valueModel, valueOffset)
			content := &model.nodes[container].content
			content.items = insertValidationEdge(content.items, ordinal,
				validationEdge{target: valueOffset + valueRoot})
		case EditOperationRemoveSequenceElement:
			container, ordinal, failure := d.resolveSequenceItem(operation.Target)
			if failure != nil {
				return nil, failure
			}
			content := &model.nodes[container].content
			content.items = removeValidationEdge(content.items, ordinal)
		case EditOperationInsertAlias:
			container := int(operation.Mapping.Index())
			if model.nodes[container].content.kind != contentSequence {
				return nil, &EditFailure{Kind: EditFailureWrongRole}
			}
			anchorIndex := int(operation.Anchor.Index())
			anchorName := model.nodes[anchorIndex].anchor
			if anchorName == "" {
				return nil, &EditFailure{Kind: EditFailureWrongRole}
			}
			spans := d.sequenceItemSpans(container)
			ordinal, failure := d.collectionPlacement(container, spans, operation.Placement)
			if failure != nil {
				return nil, failure
			}
			content := &model.nodes[container].content
			content.items = insertValidationEdge(content.items, ordinal, validationEdge{
				target: anchorIndex,
				alias:  &validationAlias{name: anchorName},
			})
		}
	}
	return model, nil
}

// renameModelAlias updates one dependent alias name.
func renameModelAlias(edge *validationEdge, target int, oldName, newName string) {
	if edge.alias != nil && edge.target == target && edge.alias.name == oldName {
		edge.alias.name = newName
	}
}

// mergeModel appends one imported model with a node offset.
func mergeModel(model *validationModel, imported *validationModel,
	offset int) *validationModel {
	combined := &validationModel{
		nodes: make([]validationNode, 0, len(model.nodes)+len(imported.nodes)),
		roots: model.roots,
	}
	combined.nodes = append(combined.nodes, model.nodes...)
	for index := range imported.nodes {
		node := imported.nodes[index]
		node.content = offsetValidationContent(node.content, offset)
		combined.nodes = append(combined.nodes, node)
	}
	return combined
}

func offsetValidationContent(content validationContent, offset int) validationContent {
	for index := range content.items {
		content.items[index].target += offset
	}
	for index := range content.entries {
		content.entries[index].key.target += offset
		content.entries[index].value.target += offset
	}
	return content
}

func insertValidationEdge(items []validationEdge, ordinal int,
	edge validationEdge) []validationEdge {
	items = append(items, validationEdge{})
	copy(items[ordinal+1:], items[ordinal:])
	items[ordinal] = edge
	return items
}

func removeValidationEdge(items []validationEdge, ordinal int) []validationEdge {
	return append(items[:ordinal], items[ordinal+1:]...)
}

func insertValidationEntry(entries []validationEntry, ordinal int,
	entry validationEntry) []validationEntry {
	entries = append(entries, validationEntry{})
	copy(entries[ordinal+1:], entries[ordinal:])
	entries[ordinal] = entry
	return entries
}

func removeValidationEntry(entries []validationEntry, ordinal int) []validationEntry {
	return append(entries[:ordinal], entries[ordinal+1:]...)
}

// valueModel builds the representation model of one materialized value
// (edit.rs validation_model_for_value).
func (d *Document) valueModel(value core.Value) (*validationModel, *EditFailure) {
	request := document.NewMaterializationRequest(d.profile.ID(),
		document.NewMaterializationStyleId("yaml.canonical-flow", 1)).
		WithLimits(editMaterializationLimits(d.limits))
	result := MaterializeValue(value, request)
	if result.Failed != nil {
		if result.Failed.Failure.Kind == MaterializationUnrepresentable {
			return nil, &EditFailure{Kind: EditFailureUnsupportedInsertedValue,
				ValueKind: value.Kind()}
		}
		if result.Failed.Failure.Kind == MaterializationResourceLimit {
			return nil, &EditFailure{Kind: EditFailureResourceLimit,
				LimitName: result.Failed.Failure.LimitName}
		}
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return modelFromDocument(result.Complete.Document), nil
}

// compareModels verifies the representation-graph isomorphism between the
// expected and candidate models (edit.rs ValidationModel::compare).
func compareModels(expected, candidate *validationModel,
	scalarWildcard bool) *EditFailure {
	if len(expected.roots) != len(candidate.roots) {
		return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	state := &compareState{
		nodePairs:  make(map[int]int),
		actualUsed: make(map[int]bool),
		expected:   expected,
		candidate:  candidate,
	}
	for index := range expected.roots {
		if failure := state.compareNode(expected.roots[index], candidate.roots[index],
			scalarWildcard); failure != nil {
			return failure
		}
	}
	// The cardinality check covers exactly the reachable nodes; nodes made
	// unreachable by a declared removal do not participate.
	if len(state.nodePairs) != reachableCount(expected) ||
		len(state.actualUsed) != reachableCount(candidate) {
		return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return nil
}

// reachableCount counts the nodes reachable from the roots without
// repeating visits.
func reachableCount(model *validationModel) int {
	seen := make(map[int]bool)
	var walk func(index int)
	walk = func(index int) {
		if seen[index] {
			return
		}
		seen[index] = true
		content := &model.nodes[index].content
		for _, item := range content.items {
			walk(item.target)
		}
		for _, entry := range content.entries {
			walk(entry.key.target)
			walk(entry.value.target)
		}
	}
	for _, root := range model.roots {
		walk(root)
	}
	return len(seen)
}

type compareState struct {
	nodePairs  map[int]int
	actualUsed map[int]bool
	expected   *validationModel
	candidate  *validationModel
}

func (s *compareState) compareNode(expectedIndex, actualIndex int,
	scalarWildcard bool) *EditFailure {
	if mapped, ok := s.nodePairs[expectedIndex]; ok {
		if mapped != actualIndex {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		return nil
	}
	if s.actualUsed[actualIndex] {
		return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	s.nodePairs[expectedIndex] = actualIndex
	s.actualUsed[actualIndex] = true
	expected := &s.expected.nodes[expectedIndex]
	actual := &s.candidate.nodes[actualIndex]
	if expected.anchor != actual.anchor {
		return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	if expected.tag != actual.tag {
		return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	if expected.content.kind != actual.content.kind {
		return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	switch expected.content.kind {
	case contentScalar:
		if actual.content.kind != contentScalar {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		if !scalarWildcard && (expected.content.scalarKind != actual.content.scalarKind ||
			expected.content.canonical != actual.content.canonical) {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
	case contentSequence:
		if len(expected.content.items) != len(actual.content.items) {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		for index := range expected.content.items {
			if failure := s.compareEdge(&expected.content.items[index],
				&actual.content.items[index]); failure != nil {
				return failure
			}
		}
	case contentMapping:
		if len(expected.content.entries) != len(actual.content.entries) {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		for index := range expected.content.entries {
			if failure := s.compareEdge(&expected.content.entries[index].key,
				&actual.content.entries[index].key); failure != nil {
				return failure
			}
			if failure := s.compareEdge(&expected.content.entries[index].value,
				&actual.content.entries[index].value); failure != nil {
				return failure
			}
		}
	}
	return nil
}

func (s *compareState) compareEdge(expected, actual *validationEdge) *EditFailure {
	switch {
	case expected.alias == nil && actual.alias == nil:
		return s.compareNode(expected.target, actual.target, false)
	case expected.alias != nil && actual.alias != nil:
		if expected.alias.name != actual.alias.name {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		return s.compareNode(expected.target, actual.target, false)
	}
	return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
}

// editSourcePatchReplacements converts the source edits into patch
// replacements.
func editSourcePatchReplacements(source []byte,
	edits []SourceEdit) []document.SourceReplacement {
	replacements := make([]document.SourceReplacement, 0, len(edits))
	for _, edit := range edits {
		replacements = append(replacements, document.NewSourceReplacement(
			edit.OldSpan.StartByte(), edit.OldSpan.EndByte(),
			source[edit.OldSpan.StartByte():edit.OldSpan.EndByte()], edit.Replacement))
	}
	return replacements
}

// editSourcePatchLimits maps the parse limits onto the patch limits.
func editSourcePatchLimits(parseLimits document.ParseLimits,
	operationCount int) document.SourcePatchLimits {
	return document.SourcePatchLimits{
		Source: document.SourceLimits{
			MaxRawBytes:         parseLimits.MaxSourceBytes,
			MaxDecodedUTF8Bytes: parseLimits.MaxSourceBytes * 2,
			MaxDecodedScalars:   parseLimits.MaxSourceBytes,
		},
		MaxReplacements: operationCount,
		MaxPatchBytes:   parseLimits.MaxSourceBytes * 2,
	}
}

// editOperationMetadata records the frozen operation identities for the
// patch provenance.
func editOperationMetadata(tx *EditTransaction) map[string]string {
	metadata := make(map[string]string, len(tx.operations))
	for index := range tx.operations {
		operation := &tx.operations[index]
		id := "yaml.edit.replace-scalar-semantic@1"
		switch operation.Kind {
		case EditOperationReplaceScalar:
			if operation.Scalar.Kind == ScalarReplacementLiteral {
				id = "yaml.edit.replace-scalar-literal@1"
			}
		case EditOperationRenameAnchor:
			id = "yaml.edit.rename-anchor@1"
		case EditOperationInsertMappingEntry:
			id = "yaml.edit.insert-mapping-entry@1"
		case EditOperationRemoveMappingEntry:
			id = "yaml.edit.remove-mapping-entry@1"
		case EditOperationInsertSequenceElement:
			id = "yaml.edit.insert-sequence-element@1"
		case EditOperationRemoveSequenceElement:
			id = "yaml.edit.remove-sequence-element@1"
		case EditOperationInsertAlias:
			id = "yaml.edit.insert-alias@1"
		}
		metadata[fmt.Sprintf("operation.%d", index)] = id
	}
	return metadata
}

func strPtr(text string) *string { return &text }
