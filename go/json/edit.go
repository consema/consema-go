package json

import (
	"context"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the typed JSON edit operations and the atomic
// commit/dry-run pipeline (consema-rs/crates/consema-json/src/edit.rs; RFC 0016
// §5.3). An edit replaces only the bytes its operations own; every other
// byte is covered by the untouched-byte proof, and a failure never
// modifies the base document.

// RepresentationPolicy is the explicit semantic scalar representation
// policy (edit.rs:17-28).
type RepresentationPolicy uint8

// The four frozen policies.
const (
	// RepresentationPolicyExactLiteral requires the caller to use a
	// literal scalar replacement; semantic replacement rejects it.
	RepresentationPolicyExactLiteral RepresentationPolicy = iota
	// RepresentationPolicyPreserveCompatible preserves the target's
	// compatible native scalar category or fails.
	RepresentationPolicyPreserveCompatible
	// RepresentationPolicyCanonicalForProfile uses deterministic
	// profile-canonical JSON literal syntax.
	RepresentationPolicyCanonicalForProfile
	// RepresentationPolicyPreserveElseCanonical tries category
	// preservation, then explicitly reports a canonical fallback.
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

// EditOperationKind is the closed JSON edit operation category
// (edit.rs:59-108).
type EditOperationKind uint8

// The seven frozen operation categories.
const (
	// EditOperationReplaceScalar replaces an existing scalar semantically
	// or literally.
	EditOperationReplaceScalar EditOperationKind = iota
	// EditOperationInsertMember inserts one complete member into an Object
	// value.
	EditOperationInsertMember
	// EditOperationRemoveMember removes one exact member identity.
	EditOperationRemoveMember
	// EditOperationMoveMember moves one exact member within its current
	// Object.
	EditOperationMoveMember
	// EditOperationRenameMember replaces only one exact member's key
	// literal.
	EditOperationRenameMember
	// EditOperationInsertArrayElement inserts one complete element into an
	// Array value.
	EditOperationInsertArrayElement
	// EditOperationRemoveArrayElement removes one exact array element
	// identity.
	EditOperationRemoveArrayElement
)

// EditOperation is one typed JSON edit operation bound to an immutable
// base snapshot (edit.rs:59-108). Only the fields of the declared Kind
// are meaningful.
type EditOperation struct {
	// Kind is the closed operation category.
	Kind EditOperationKind
	// Target is the exact target NodeRef (ReplaceScalar, RemoveMember,
	// MoveMember, RenameMember, RemoveArrayElement).
	Target document.NodeRef
	// Scalar is the scalar replacement (ReplaceScalar).
	Scalar ScalarReplacement
	// Object is the exact Object value target (InsertMember).
	Object document.NodeRef
	// Array is the exact Array value target (InsertArrayElement).
	Array document.NodeRef
	// Name is the decoded member name (InsertMember, RenameMember).
	Name string
	// Value is the complete inserted value (InsertMember,
	// InsertArrayElement).
	Value core.Value
	// Placement is the explicit association placement (InsertMember,
	// MoveMember, InsertArrayElement).
	Placement AssociationPlacement
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

// InsertMember adds one JSON Object member insertion.
func (b *EditTransactionBuilder) InsertMember(object document.NodeRef, name string,
	value core.Value, placement AssociationPlacement) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationInsertMember, Object: object, Name: name, Value: value,
		Placement: placement,
	})
	return b
}

// RemoveMember adds one exact JSON Object member removal.
func (b *EditTransactionBuilder) RemoveMember(target document.NodeRef) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{Kind: EditOperationRemoveMember, Target: target})
	return b
}

// MoveMember adds one exact same-Object member move.
func (b *EditTransactionBuilder) MoveMember(target document.NodeRef,
	placement AssociationPlacement) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationMoveMember, Target: target, Placement: placement,
	})
	return b
}

// RenameMember adds one exact JSON Object member rename.
func (b *EditTransactionBuilder) RenameMember(target document.NodeRef,
	name string) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRenameMember, Target: target, Name: name,
	})
	return b
}

// InsertArrayElement adds one JSON Array element insertion.
func (b *EditTransactionBuilder) InsertArrayElement(array document.NodeRef, value core.Value,
	placement AssociationPlacement) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationInsertArrayElement, Array: array, Value: value, Placement: placement,
	})
	return b
}

// RemoveArrayElement adds one exact JSON Array element removal.
func (b *EditTransactionBuilder) RemoveArrayElement(target document.NodeRef) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRemoveArrayElement, Target: target,
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
	UntouchedProof *UntouchedByteProof
}

// EditFailureKind is the stable edit validation or commit failure
// category (edit.rs:258-299).
type EditFailureKind uint8

// The closed edit failure categories.
const (
	// EditFailureRecoveredDocument: edits are forbidden on a recovered
	// document.
	EditFailureRecoveredDocument EditFailureKind = iota
	// EditFailureWrongSnapshot: the transaction or target belongs to
	// another snapshot.
	EditFailureWrongSnapshot
	// EditFailureWrongRole: the target role is not a scalar value or
	// object key.
	EditFailureWrongRole
	// EditFailureIncompleteTarget: the target is not a complete literal
	// syntax node.
	EditFailureIncompleteTarget
	// EditFailureSemanticUnavailable: the target native semantics are
	// unavailable.
	EditFailureSemanticUnavailable
	// EditFailureUnsupportedSemanticValue: the public value cannot be
	// represented as a JSON scalar.
	EditFailureUnsupportedSemanticValue
	// EditFailureInvalidLiteral: the exact candidate is not one complete
	// legal scalar literal for the profile.
	EditFailureInvalidLiteral
	// EditFailureRepresentationIncompatible: PreserveCompatible could not
	// retain the scalar category.
	EditFailureRepresentationIncompatible
	// EditFailureExactLiteralRequiresLiteralOperation: ExactLiteral was
	// incorrectly requested without literal bytes.
	EditFailureExactLiteralRequiresLiteralOperation
	// EditFailureConflictingEdits: two source edits overlap or target the
	// same scalar.
	EditFailureConflictingEdits
	// EditFailureDuplicateTarget: more than one operation names the same
	// exact destructive target.
	EditFailureDuplicateTarget
	// EditFailureOverlappingOwnership: prepared source ownership intervals
	// overlap or reuse one insertion point.
	EditFailureOverlappingOwnership
	// EditFailureAncestorDescendantConflict: one transaction edits an
	// association and one of its owned descendants.
	EditFailureAncestorDescendantConflict
	// EditFailurePlacementAnchorRemoved: an insertion anchor is removed by
	// the same transaction.
	EditFailurePlacementAnchorRemoved
	// EditFailurePlacementAnchorModified: a move target or anchor is
	// modified by another operation in the transaction.
	EditFailurePlacementAnchorModified
	// EditFailureTargetNotFound: a target or placement anchor is not
	// present in its declared container.
	EditFailureTargetNotFound
	// EditFailureUnrepresentableValue: a structural value cannot be
	// represented by the JSON target profile.
	EditFailureUnrepresentableValue
	// EditFailureResourceLimit: a configured edit or output bound was
	// exceeded.
	EditFailureResourceLimit
	// EditFailureNewDocumentFormationFailed: the replacement document could
	// not be formed under the original limits.
	EditFailureNewDocumentFormationFailed
)

// EditFailure is the typed edit failure. It implements error and the
// RFC 0016 §6 Code() contract with the frozen registered codes
// (edit.rs:1269-1324).
type EditFailure struct {
	// Kind identifies the failure.
	Kind EditFailureKind
	// ValueKind is the offending PortableValue kind of
	// UnsupportedSemanticValue / UnrepresentableValue.
	ValueKind core.Kind
	// LimitName is the stable limit name of a ResourceLimit.
	LimitName string
}

// Error implements error.
func (e *EditFailure) Error() string {
	switch e.Kind {
	case EditFailureRecoveredDocument:
		return "json: edits are forbidden on a recovered document"
	case EditFailureWrongSnapshot:
		return "json: edit transaction or target belongs to another snapshot"
	case EditFailureWrongRole:
		return "json: edit target has the wrong structural role"
	case EditFailureIncompleteTarget:
		return "json: edit target is not a complete literal syntax node"
	case EditFailureSemanticUnavailable:
		return "json: edit target native semantics are unavailable"
	case EditFailureUnsupportedSemanticValue:
		return "json: portable kind " + e.ValueKind.String() + " is not a JSON scalar"
	case EditFailureInvalidLiteral:
		return "json: edit literal is invalid for the target profile"
	case EditFailureRepresentationIncompatible:
		return "json: representation policy cannot preserve the target category"
	case EditFailureExactLiteralRequiresLiteralOperation:
		return "json: exact literal policy requires a literal operation"
	case EditFailureConflictingEdits:
		return "json: edit operations have conflicting source ownership"
	case EditFailureDuplicateTarget:
		return "json: more than one operation names the same destructive target"
	case EditFailureOverlappingOwnership:
		return "json: prepared source ownership intervals overlap"
	case EditFailureAncestorDescendantConflict:
		return "json: an edit targets an association and one of its owned descendants"
	case EditFailurePlacementAnchorRemoved:
		return "json: an insertion anchor is removed by the same transaction"
	case EditFailurePlacementAnchorModified:
		return "json: a move target or anchor is modified by the same transaction"
	case EditFailureTargetNotFound:
		return "json: edit target or placement anchor was not found"
	case EditFailureUnrepresentableValue:
		return "json: portable kind " + e.ValueKind.String() + " is not representable by the profile"
	case EditFailureResourceLimit:
		return "json: edit limit " + e.LimitName + " reached"
	case EditFailureNewDocumentFormationFailed:
		return "json: replacement document could not be formed"
	}
	return "json: edit failure"
}

// Name returns the stable failure name (the Rust variant spelling used
// by the conformance vectors).
func (e *EditFailure) Name() string {
	switch e.Kind {
	case EditFailureRecoveredDocument:
		return "RecoveredDocument"
	case EditFailureWrongSnapshot:
		return "WrongSnapshot"
	case EditFailureWrongRole:
		return "WrongRole"
	case EditFailureIncompleteTarget:
		return "IncompleteTarget"
	case EditFailureSemanticUnavailable:
		return "SemanticUnavailable"
	case EditFailureUnsupportedSemanticValue:
		return "UnsupportedSemanticValue"
	case EditFailureInvalidLiteral:
		return "InvalidLiteral"
	case EditFailureRepresentationIncompatible:
		return "RepresentationIncompatible"
	case EditFailureExactLiteralRequiresLiteralOperation:
		return "ExactLiteralRequiresLiteralOperation"
	case EditFailureConflictingEdits:
		return "ConflictingEdits"
	case EditFailureDuplicateTarget:
		return "DuplicateTarget"
	case EditFailureOverlappingOwnership:
		return "OverlappingOwnership"
	case EditFailureAncestorDescendantConflict:
		return "AncestorDescendantConflict"
	case EditFailurePlacementAnchorRemoved:
		return "PlacementAnchorRemoved"
	case EditFailurePlacementAnchorModified:
		return "PlacementAnchorModified"
	case EditFailureTargetNotFound:
		return "TargetNotFound"
	case EditFailureUnrepresentableValue:
		return "UnrepresentableValue"
	case EditFailureResourceLimit:
		return "ResourceLimit"
	case EditFailureNewDocumentFormationFailed:
		return "NewDocumentFormationFailed"
	}
	return "EditFailure"
}

// Code returns the frozen registered code for the failure
// (edit.rs:1299-1323).
func (e *EditFailure) Code() string {
	switch e.Kind {
	case EditFailureRecoveredDocument, EditFailureIncompleteTarget:
		return "core.edit.incomplete-target@1"
	case EditFailureWrongSnapshot:
		return "core.edit.wrong-snapshot@1"
	case EditFailureWrongRole:
		return "core.edit.wrong-role@1"
	case EditFailureSemanticUnavailable:
		return "core.edit.semantic-unavailable@1"
	case EditFailureUnsupportedSemanticValue, EditFailureUnrepresentableValue:
		return "core.edit.unsupported-value@1"
	case EditFailureInvalidLiteral:
		return "core.edit.invalid-literal@1"
	case EditFailureRepresentationIncompatible:
		return "core.edit.representation-incompatible@1"
	case EditFailureExactLiteralRequiresLiteralOperation:
		return "core.edit.exact-literal-requires-literal@1"
	case EditFailureConflictingEdits, EditFailureDuplicateTarget, EditFailureOverlappingOwnership,
		EditFailureAncestorDescendantConflict, EditFailurePlacementAnchorRemoved,
		EditFailurePlacementAnchorModified:
		return "core.edit.conflicting-edits@1"
	case EditFailureTargetNotFound:
		return "core.edit.target-not-found@1"
	case EditFailureResourceLimit:
		return "core.edit.resource-limit@1"
	case EditFailureNewDocumentFormationFailed:
		return "core.edit.formation-failed@1"
	}
	return "core.edit.conflicting-edits@1"
}

// Commit atomically commits scalar and structural operations. On failure
// the base document remains unchanged (edit.rs:301-451).
func (d *Document) Commit(tx *EditTransaction) (*EditCommit, *EditFailure) {
	if d.formationStatus != document.FormationStatusComplete {
		return nil, &EditFailure{Kind: EditFailureRecoveredDocument}
	}
	if tx.base != d.SnapshotIdentity() {
		return nil, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if failure := validateDependencies(tx); failure != nil {
		return nil, failure
	}
	var diagnostics []*protocol.Diagnostic
	var prepared []preparedEdit
	for _, operation := range tx.operations {
		edits, failure := d.prepareOperation(&operation, &diagnostics)
		if failure != nil {
			return nil, failure
		}
		prepared = append(prepared, edits...)
	}
	sort.Slice(prepared, func(i, j int) bool {
		if prepared[i].oldSpan.StartByte() != prepared[j].oldSpan.StartByte() {
			return prepared[i].oldSpan.StartByte() < prepared[j].oldSpan.StartByte()
		}
		return prepared[i].oldSpan.EndByte() < prepared[j].oldSpan.EndByte()
	})
	for index := 1; index < len(prepared); index++ {
		left := &prepared[index-1]
		right := &prepared[index]
		if !left.oldSpan.IsEmpty() && !right.oldSpan.IsEmpty() &&
			(left.oldSpan.EndByte() > right.oldSpan.StartByte() || left.oldSpan == right.oldSpan) {
			return nil, &EditFailure{Kind: EditFailureAncestorDescendantConflict}
		}
		if left.oldSpan == right.oldSpan ||
			(left.oldSpan.IsEmpty() && right.oldSpan.IsEmpty() &&
				left.oldSpan.StartByte() == right.oldSpan.StartByte()) {
			return nil, &EditFailure{Kind: EditFailureOverlappingOwnership}
		}
	}
	targetLength := d.source.Len()
	for _, edit := range prepared {
		targetLength = targetLength - edit.oldSpan.Len() + len(edit.replacement)
		if targetLength < 0 {
			return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "target-bytes"}
		}
	}
	if targetLength > d.parseLimits.MaxSourceBytes {
		return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "target-bytes"}
	}
	sourceText, _ := d.source.DecodedText()
	rendered := make([]byte, 0, targetLength)
	cursor := 0
	for _, edit := range prepared {
		rendered = append(rendered, sourceText[cursor:edit.oldSpan.StartByte()]...)
		rendered = append(rendered, edit.replacement...)
		cursor = edit.oldSpan.EndByte()
	}
	rendered = append(rendered, sourceText[cursor:]...)
	newDocument, formationFailure := Parse(context.Background(), rendered, d.profile, d.parseLimits)
	if formationFailure != nil {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}

	delta := 0
	var sourceEdits []SourceEdit
	var mappings []NodeMapping
	mappedOld := make(map[document.NodeRef]bool)
	for _, edit := range prepared {
		newStart := edit.oldSpan.StartByte() + delta
		newEnd := newStart + len(edit.replacement)
		newSpan, err := newDocument.authority.Span(newStart, newEnd)
		if err != nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		sourceEdits = append(sourceEdits, SourceEdit{
			OldSpan:     edit.oldSpan,
			NewSpan:     newSpan,
			Replacement: append([]byte(nil), edit.replacement...),
		})
		if edit.mapping != nil {
			old := edit.mapping.old
			if !mappedOld[old] {
				mappedOld[old] = true
				var newRef *document.NodeRef
				var status protocol.NodeMappingStatus
				var reason *string
				switch edit.mapping.plan {
				case mappingPlanReplacedLiteral:
					if index := findValueByLiteralSpan(newDocument, newStart, newEnd); index >= 0 {
						reference := newDocument.nodeRef(index, edit.mapping.role)
						newRef = &reference
						status = protocol.MappingReplaced
					} else {
						status = protocol.MappingUnmapped
						text := "reparsed-node-not-uniquely-located"
						reason = &text
					}
				case mappingPlanDeleted:
					status = protocol.MappingDeleted
				case mappingPlanUnmapped:
					status = protocol.MappingUnmapped
					text := edit.mapping.reason
					reason = &text
				}
				mappings = append(mappings, NodeMapping{
					Old: old, New: newRef, Status: status, Reason: reason,
				})
			}
		}
		delta += len(edit.replacement) - edit.oldSpan.Len()
	}
	changeSet := document.NewChangeSet(d.SnapshotIdentity(), newDocument.SnapshotIdentity(),
		sourceEdits, mappings, diagnostics)
	patchLimits := sourcePatchLimits(d.parseLimits, len(sourceEdits))
	replacements := make([]document.SourceReplacement, 0, len(sourceEdits))
	for _, edit := range sourceEdits {
		original := sourceText[edit.OldSpan.StartByte():edit.OldSpan.EndByte()]
		replacements = append(replacements, document.NewSourceReplacement(
			edit.OldSpan.StartByte(), edit.OldSpan.EndByte(),
			[]byte(original), edit.Replacement))
	}
	patch, patchErr := document.NewSourcePatch(d.source, replacements,
		operationMetadata(tx), patchLimits)
	if patchErr != nil || !patch.TargetDigest().Equal(newDocument.source.Digest()) {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	proof, proofErr := CreateUntouchedByteProof(d.source, newDocument.source, patch.Replacements())
	if proofErr != nil {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return &EditCommit{
		Document:       newDocument,
		ChangeSet:      changeSet,
		SourcePatch:    patch,
		UntouchedProof: proof,
	}, nil
}

// DryRun fully validates and plans an edit without returning a new
// Document (edit.rs:453-469).
func (d *Document) DryRun(tx *EditTransaction, sourceID string) (*EditPlan, *EditFailure) {
	commit, failure := d.Commit(tx)
	if failure != nil {
		return nil, failure
	}
	summaries, failure := operationSummaries(tx)
	if failure != nil {
		return nil, failure
	}
	plan, err := document.NewEditPlan(sourceID, d.Profile(), summaries, commit.SourcePatch,
		commit.ChangeSet.Diagnostics())
	if err != nil {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return plan, nil
}

// preparedEdit is one owned source interval of the commit.
type preparedEdit struct {
	oldSpan     document.Span
	replacement []byte
	mapping     *preparedMapping
}

// preparedMapping is one old-node mapping plan of a prepared edit.
type preparedMapping struct {
	old    document.NodeRef
	plan   mappingPlanKind
	role   document.NodeRole
	reason string
}

// mappingPlanKind is the closed mapping plan category.
type mappingPlanKind uint8

const (
	mappingPlanReplacedLiteral mappingPlanKind = iota
	mappingPlanDeleted
	mappingPlanUnmapped
)

// prepareOperation prepares one operation into owned edits
// (edit.rs:471-503).
func (d *Document) prepareOperation(operation *EditOperation,
	diagnostics *[]*protocol.Diagnostic) ([]preparedEdit, *EditFailure) {
	switch operation.Kind {
	case EditOperationReplaceScalar:
		edit, failure := d.prepareScalar(&operation.Scalar, diagnostics)
		if failure != nil {
			return nil, failure
		}
		return []preparedEdit{*edit}, nil
	case EditOperationInsertMember:
		return d.prepareInsertMember(operation.Object, operation.Name, operation.Value, operation.Placement)
	case EditOperationRemoveMember:
		edit, failure := d.prepareRemoveMember(operation.Target)
		if failure != nil {
			return nil, failure
		}
		return edit, nil
	case EditOperationMoveMember:
		return d.prepareMoveMember(operation.Target, operation.Placement)
	case EditOperationRenameMember:
		edit, failure := d.prepareRenameMember(operation.Target, operation.Name)
		if failure != nil {
			return nil, failure
		}
		return []preparedEdit{*edit}, nil
	case EditOperationInsertArrayElement:
		return d.prepareInsertArrayElement(operation.Array, operation.Value, operation.Placement)
	case EditOperationRemoveArrayElement:
		edit, failure := d.prepareRemoveArrayElement(operation.Target)
		if failure != nil {
			return nil, failure
		}
		return edit, nil
	}
	return nil, &EditFailure{Kind: EditFailureWrongRole}
}

// prepareScalar prepares one scalar replacement (edit.rs:505-556).
func (d *Document) prepareScalar(operation *ScalarReplacement,
	diagnostics *[]*protocol.Diagnostic) (*preparedEdit, *EditFailure) {
	index, failure := d.resolveTarget(operation.Target, []document.NodeRole{
		document.RoleValue, document.RoleObjectKey})
	if failure != nil {
		return nil, failure
	}
	entity := d.valueEntity(index)
	if !entity.complete || entity.literalSpan == nil {
		return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	if entity.kind.tag == internalUnavailable {
		return nil, &EditFailure{Kind: EditFailureSemanticUnavailable}
	}
	if entity.kind.tag == internalArray || entity.kind.tag == internalObject {
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	var replacement []byte
	switch operation.Kind {
	case ScalarReplacementLiteral:
		literalKind, failure := validateLiteral(operation.Literal, d.profile, d.parseLimits)
		if failure != nil {
			return nil, failure
		}
		if operation.Target.Role() == document.RoleObjectKey && literalKind != JsonValueKindString {
			return nil, &EditFailure{Kind: EditFailureInvalidLiteral}
		}
		replacement = append([]byte(nil), operation.Literal...)
	case ScalarReplacementSemantic:
		if operation.Target.Role() == document.RoleObjectKey && operation.Value.Kind() != core.KindString {
			return nil, &EditFailure{Kind: EditFailureUnsupportedSemanticValue,
				ValueKind: operation.Value.Kind()}
		}
		oldSpan := *entity.literalSpan
		sourceText, _ := d.source.DecodedText()
		oldLiteral := sourceText[oldSpan.StartByte():oldSpan.EndByte()]
		bytes, failure := semanticLiteral(operation.Value, &entity.kind, oldLiteral, d.profile,
			operation.Policy, d.authority, oldSpan, diagnostics)
		if failure != nil {
			return nil, failure
		}
		replacement = bytes
	}
	return &preparedEdit{
		oldSpan:     *entity.literalSpan,
		replacement: replacement,
		mapping: &preparedMapping{
			old:  operation.Target,
			plan: mappingPlanReplacedLiteral,
			role: operation.Target.Role(),
		},
	}, nil
}

// prepareInsertMember prepares one member insertion (edit.rs:558-595).
func (d *Document) prepareInsertMember(object document.NodeRef, name string, value core.Value,
	placement AssociationPlacement) ([]preparedEdit, *EditFailure) {
	index, failure := d.resolveTarget(object, []document.NodeRole{document.RoleValue})
	if failure != nil {
		return nil, failure
	}
	entity := d.valueEntity(index)
	if !entity.complete {
		return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	if entity.kind.tag != internalObject {
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	fragment, failure := d.fragment(core.String(name))
	if failure != nil {
		return nil, failure
	}
	fragment = append(fragment, ':')
	valueFragment, failure := d.fragment(value)
	if failure != nil {
		return nil, failure
	}
	if failure := appendFragment(&fragment, valueFragment, d.parseLimits.MaxSourceBytes); failure != nil {
		return nil, failure
	}
	edit, failure := d.prepareInsertion(object, entity.span, entity.kind.object, insertionSyntax{
		anchorRole: document.RoleObjectMember,
		open:       JsonSyntaxKindLeftBrace,
		close:      JsonSyntaxKindRightBrace,
	}, placement, fragment)
	if failure != nil {
		return nil, failure
	}
	return []preparedEdit{*edit}, nil
}

// prepareInsertArrayElement prepares one array element insertion
// (edit.rs:597-623).
func (d *Document) prepareInsertArrayElement(array document.NodeRef, value core.Value,
	placement AssociationPlacement) ([]preparedEdit, *EditFailure) {
	index, failure := d.resolveTarget(array, []document.NodeRole{document.RoleValue})
	if failure != nil {
		return nil, failure
	}
	entity := d.valueEntity(index)
	if !entity.complete {
		return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	if entity.kind.tag != internalArray {
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	fragment, failure := d.fragment(value)
	if failure != nil {
		return nil, failure
	}
	edit, failure := d.prepareInsertion(array, entity.span, entity.kind.array, insertionSyntax{
		anchorRole: document.RoleArrayElement,
		open:       JsonSyntaxKindLeftBracket,
		close:      JsonSyntaxKindRightBracket,
	}, placement, fragment)
	if failure != nil {
		return nil, failure
	}
	return []preparedEdit{*edit}, nil
}

// insertionSyntax is the container-specific insertion facts.
type insertionSyntax struct {
	anchorRole document.NodeRole
	open       JsonSyntaxKind
	close      JsonSyntaxKind
}

// prepareInsertion computes one zero-width insertion edit
// (edit.rs:625-695).
func (d *Document) prepareInsertion(container document.NodeRef, containerSpan document.Span,
	associations []int, syntax insertionSyntax, placement AssociationPlacement,
	fragment []byte) (*preparedEdit, *EditFailure) {
	var position int
	prefixComma := false
	suffixComma := false
	if len(associations) == 0 {
		switch placement.Kind() {
		case PlacementStart:
			delimiter, failure := d.delimiter(syntax.open, containerSpan, false)
			if failure != nil {
				return nil, failure
			}
			position = delimiter.EndByte()
		case PlacementEnd:
			delimiter, failure := d.delimiter(syntax.close, containerSpan, true)
			if failure != nil {
				return nil, failure
			}
			position = delimiter.StartByte()
		case PlacementBefore, PlacementAfter:
			return nil, &EditFailure{Kind: EditFailureTargetNotFound}
		}
	} else {
		switch placement.Kind() {
		case PlacementStart:
			position = d.span(associations[0]).StartByte()
			suffixComma = true
		case PlacementEnd:
			position = d.span(associations[len(associations)-1]).EndByte()
			prefixComma = true
		case PlacementBefore:
			anchor, failure := d.resolveAnchor(placement.Anchor(), syntax.anchorRole, associations)
			if failure != nil {
				return nil, failure
			}
			position = d.span(anchor).StartByte()
			suffixComma = true
		case PlacementAfter:
			anchor, failure := d.resolveAnchor(placement.Anchor(), syntax.anchorRole, associations)
			if failure != nil {
				return nil, failure
			}
			position = d.span(anchor).EndByte()
			prefixComma = true
		}
	}
	replacement := make([]byte, 0, len(fragment)+2)
	if prefixComma {
		replacement = append(replacement, ',')
	}
	replacement = append(replacement, fragment...)
	if suffixComma {
		replacement = append(replacement, ',')
	}
	span, err := d.authority.Span(position, position)
	if err != nil {
		return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	return &preparedEdit{
		oldSpan:     span,
		replacement: replacement,
		mapping: &preparedMapping{
			old:    container,
			plan:   mappingPlanUnmapped,
			reason: "container-reparsed-after-structural-insertion",
		},
	}, nil
}

// prepareRemoveMember prepares one member removal (edit.rs:697-709).
func (d *Document) prepareRemoveMember(target document.NodeRef) ([]preparedEdit, *EditFailure) {
	index, failure := d.resolveTarget(target, []document.NodeRole{document.RoleObjectMember})
	if failure != nil {
		return nil, failure
	}
	container, members, ordinal, parentFailure := d.parentObject(index)
	if parentFailure != nil {
		return nil, parentFailure
	}
	return d.prepareRemoval(target, index, members, ordinal, d.span(container).EndByte())
}

// prepareMoveMember prepares one same-Object member move
// (edit.rs:711-782).
func (d *Document) prepareMoveMember(target document.NodeRef,
	placement AssociationPlacement) ([]preparedEdit, *EditFailure) {
	index, failure := d.resolveTarget(target, []document.NodeRole{document.RoleObjectMember})
	if failure != nil {
		return nil, failure
	}
	container, members, ordinal, failure := d.parentObject(index)
	if failure != nil {
		return nil, failure
	}
	remaining := make([]int, 0, len(members)-1)
	for _, member := range members {
		if member != index {
			remaining = append(remaining, member)
		}
	}
	var destination int
	switch placement.Kind() {
	case PlacementStart:
		destination = 0
	case PlacementEnd:
		destination = len(remaining)
	case PlacementBefore, PlacementAfter:
		if placement.Anchor() == target {
			return nil, &EditFailure{Kind: EditFailurePlacementAnchorModified}
		}
		anchor, failure := d.resolveAnchor(placement.Anchor(), document.RoleObjectMember, remaining)
		if failure != nil {
			return nil, failure
		}
		destination = 0
		for position, candidate := range remaining {
			if candidate == anchor {
				destination = position
				break
			}
		}
		if placement.Kind() == PlacementAfter {
			destination++
		}
	}
	if destination == ordinal {
		return []preparedEdit{}, nil
	}
	targetSpan := d.span(index)
	sourceText, _ := d.source.DecodedText()
	fragment := []byte(sourceText[targetSpan.StartByte():targetSpan.EndByte()])
	containerRef := d.nodeRef(container, document.RoleValue)
	edits, failure := d.prepareRemoval(target, index, members, ordinal, d.span(container).EndByte())
	if failure != nil {
		return nil, failure
	}
	for editIndex := range edits {
		if edits[editIndex].mapping != nil && edits[editIndex].mapping.old == target {
			edits[editIndex].mapping = &preparedMapping{
				old: target, plan: mappingPlanUnmapped, reason: "member-reparsed-after-move",
			}
		}
	}
	insertion, failure := d.prepareInsertion(containerRef, d.span(container), remaining, insertionSyntax{
		anchorRole: document.RoleObjectMember,
		open:       JsonSyntaxKindLeftBrace,
		close:      JsonSyntaxKindRightBrace,
	}, placement, fragment)
	if failure != nil {
		return nil, failure
	}
	edits = append(edits, *insertion)
	return edits, nil
}

// prepareRemoveArrayElement prepares one array element removal
// (edit.rs:784-799).
func (d *Document) prepareRemoveArrayElement(target document.NodeRef) ([]preparedEdit, *EditFailure) {
	index, failure := d.resolveTarget(target, []document.NodeRole{document.RoleArrayElement})
	if failure != nil {
		return nil, failure
	}
	container, elements, ordinal, failure := d.parentArray(index)
	if failure != nil {
		return nil, failure
	}
	return d.prepareRemoval(target, index, elements, ordinal, d.span(container).EndByte())
}

// prepareRemoval prepares one association removal with its comma
// ownership (edit.rs:801-849).
func (d *Document) prepareRemoval(target document.NodeRef, index int, associations []int,
	ordinal int, containerEnd int) ([]preparedEdit, *EditFailure) {
	targetSpan := d.span(index)
	edits := make([]preparedEdit, 0, 2)
	comma, commaFound, failure := d.removalComma(associations, ordinal, containerEnd)
	if failure != nil {
		return nil, failure
	}
	if commaFound {
		if comma.EndByte() == targetSpan.StartByte() || comma.StartByte() == targetSpan.EndByte() {
			span, err := d.authority.Span(minInt(comma.StartByte(), targetSpan.StartByte()),
				maxInt(comma.EndByte(), targetSpan.EndByte()))
			if err != nil {
				return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
			}
			return []preparedEdit{{
				oldSpan:     span,
				replacement: nil,
				mapping:     &preparedMapping{old: target, plan: mappingPlanDeleted},
			}}, nil
		}
		edits = append(edits, preparedEdit{
			oldSpan:     targetSpan,
			replacement: nil,
			mapping:     &preparedMapping{old: target, plan: mappingPlanDeleted},
		})
		edits = append(edits, preparedEdit{oldSpan: comma, replacement: nil})
		return edits, nil
	}
	edits = append(edits, preparedEdit{
		oldSpan:     targetSpan,
		replacement: nil,
		mapping:     &preparedMapping{old: target, plan: mappingPlanDeleted},
	})
	return edits, nil
}

// prepareRenameMember prepares one member key rename (edit.rs:851-872).
func (d *Document) prepareRenameMember(target document.NodeRef, name string) (*preparedEdit, *EditFailure) {
	index, failure := d.resolveTarget(target, []document.NodeRole{document.RoleObjectMember})
	if failure != nil {
		return nil, failure
	}
	_, _, _, parentFailure := d.parentObject(index)
	if parentFailure != nil {
		return nil, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	member := d.entity(index).member
	if member == nil {
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	key := d.valueEntity(member.key)
	if key.literalSpan == nil {
		return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	fragment, failure := d.fragment(core.String(name))
	if failure != nil {
		return nil, failure
	}
	return &preparedEdit{
		oldSpan:     *key.literalSpan,
		replacement: fragment,
		mapping: &preparedMapping{
			old: target, plan: mappingPlanUnmapped, reason: "member-reparsed-after-key-rename",
		},
	}, nil
}

// resolveTarget resolves one target handle against its roles
// (edit.rs:874-887).
func (d *Document) resolveTarget(target document.NodeRef, roles []document.NodeRole) (int, *EditFailure) {
	if target.Snapshot() != d.SnapshotIdentity() {
		return 0, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	permitted := false
	for _, role := range roles {
		if target.Role() == role {
			permitted = true
			break
		}
	}
	if !permitted {
		return 0, &EditFailure{Kind: EditFailureWrongRole}
	}
	index, err := d.validateRef(target, roles)
	if err != nil {
		switch err.(*JsonAccessError).Kind {
		case JsonAccessErrorWrongSnapshot:
			return 0, &EditFailure{Kind: EditFailureWrongSnapshot}
		case JsonAccessErrorWrongRole:
			return 0, &EditFailure{Kind: EditFailureWrongRole}
		default:
			return 0, &EditFailure{Kind: EditFailureTargetNotFound}
		}
	}
	return index, nil
}

// resolveAnchor resolves one placement anchor inside its container
// (edit.rs:889-900).
func (d *Document) resolveAnchor(anchor document.NodeRef, role document.NodeRole,
	associations []int) (int, *EditFailure) {
	index, failure := d.resolveTarget(anchor, []document.NodeRole{role})
	if failure != nil {
		return 0, failure
	}
	for _, candidate := range associations {
		if candidate == index {
			return index, nil
		}
	}
	return 0, &EditFailure{Kind: EditFailureTargetNotFound}
}

// fragment renders one value as a canonical profile fragment
// (edit.rs:902-923).
func (d *Document) fragment(value core.Value) ([]byte, *EditFailure) {
	bytes, failure := canonicalFragment(value, d.profile, document.MaterializationLimits{
		MaxInputNodes:        d.parseLimits.MaxNodeCount,
		MaxOutputBytes:       d.parseLimits.MaxSourceBytes,
		MaxDepth:             d.parseLimits.MaxNestingDepth,
		MaxReportEntries:     d.parseLimits.MaxDiagnostics,
		MaxProvenanceEntries: d.parseLimits.MaxNodeCount * 4,
	})
	if failure != nil {
		switch failure.Kind {
		case MaterializationFailureUnrepresentable:
			return nil, &EditFailure{Kind: EditFailureUnrepresentableValue,
				ValueKind: failure.ValueKind}
		case MaterializationFailureResourceLimit:
			return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: failure.LimitName}
		default:
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
	}
	return bytes, nil
}

// parentObject locates the object container of one member
// (edit.rs:925-939).
func (d *Document) parentObject(member int) (int, []int, int, *EditFailure) {
	for index := range d.entities {
		item := &d.entities[index]
		if item.value != nil && item.value.kind.tag == internalObject {
			for ordinal, candidate := range item.value.kind.object {
				if candidate == member {
					return index, item.value.kind.object, ordinal, nil
				}
			}
		}
	}
	return 0, nil, 0, &EditFailure{Kind: EditFailureTargetNotFound}
}

// parentArray locates the array container of one element
// (edit.rs:941-955).
func (d *Document) parentArray(element int) (int, []int, int, *EditFailure) {
	for index := range d.entities {
		item := &d.entities[index]
		if item.value != nil && item.value.kind.tag == internalArray {
			for ordinal, candidate := range item.value.kind.array {
				if candidate == element {
					return index, item.value.kind.array, ordinal, nil
				}
			}
		}
	}
	return 0, nil, 0, &EditFailure{Kind: EditFailureTargetNotFound}
}

// removalComma finds the comma owned by one removal
// (edit.rs:957-987).
func (d *Document) removalComma(associations []int, ordinal int,
	containerEnd int) (document.Span, bool, *EditFailure) {
	current := d.span(associations[ordinal])
	followingEnd := containerEnd
	if ordinal+1 < len(associations) {
		followingEnd = d.span(associations[ordinal+1]).StartByte()
	}
	if comma, ok := d.syntaxBetween(JsonSyntaxKindComma, current.EndByte(), followingEnd, false); ok {
		return comma, true, nil
	}
	if ordinal == 0 {
		return document.Span{}, false, nil
	}
	previous := d.span(associations[ordinal-1])
	comma, ok := d.syntaxBetween(JsonSyntaxKindComma, previous.EndByte(), current.StartByte(), true)
	if !ok {
		return document.Span{}, false, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	return comma, true, nil
}

// delimiter finds the open or close delimiter of one container
// (edit.rs:989-997).
func (d *Document) delimiter(kind JsonSyntaxKind, container document.Span,
	last bool) (document.Span, *EditFailure) {
	span, ok := d.syntaxBetween(kind, container.StartByte(), container.EndByte(), last)
	if !ok {
		return document.Span{}, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	return span, nil
}

// syntaxBetween finds the first or last piece of one syntax kind within a
// range (edit.rs:999-1022).
func (d *Document) syntaxBetween(kind JsonSyntaxKind, start, end int, last bool) (document.Span, bool) {
	matches := make([]document.Span, 0, 4)
	for index, piece := range d.structuralIndex.Pieces() {
		span := piece.Span()
		if d.syntaxKinds[index] == kind && span.StartByte() >= start && span.EndByte() <= end {
			matches = append(matches, span)
		}
	}
	if len(matches) == 0 {
		return document.Span{}, false
	}
	if last {
		return matches[len(matches)-1], true
	}
	return matches[0], true
}

// validateDependencies rejects conflicting operation graphs before any
// preparation (edit.rs:1025-1078).
func validateDependencies(tx *EditTransaction) *EditFailure {
	destructive := make(map[document.NodeRef]bool)
	removed := make(map[document.NodeRef]bool)
	anchors := make([]document.NodeRef, 0, len(tx.operations))
	moveAnchors := make([]document.NodeRef, 0, len(tx.operations))
	moved := make(map[document.NodeRef]bool)
	for _, operation := range tx.operations {
		var target document.NodeRef
		hasTarget := false
		switch operation.Kind {
		case EditOperationReplaceScalar:
			target = operation.Scalar.Target
			hasTarget = true
		case EditOperationRemoveMember, EditOperationMoveMember, EditOperationRenameMember,
			EditOperationRemoveArrayElement:
			target = operation.Target
			hasTarget = true
		}
		if hasTarget {
			if destructive[target] {
				return &EditFailure{Kind: EditFailureDuplicateTarget}
			}
			destructive[target] = true
		}
		switch operation.Kind {
		case EditOperationRemoveMember, EditOperationRemoveArrayElement:
			removed[target] = true
		case EditOperationInsertMember, EditOperationInsertArrayElement, EditOperationMoveMember:
			if operation.Placement.Kind() == PlacementBefore || operation.Placement.Kind() == PlacementAfter {
				anchor := operation.Placement.Anchor()
				anchors = append(anchors, anchor)
				if operation.Kind == EditOperationMoveMember {
					moveAnchors = append(moveAnchors, anchor)
				}
			}
		}
		if operation.Kind == EditOperationMoveMember {
			moved[operation.Target] = true
		}
	}
	for _, anchor := range anchors {
		if removed[anchor] {
			return &EditFailure{Kind: EditFailurePlacementAnchorRemoved}
		}
	}
	for _, anchor := range anchors {
		if moved[anchor] {
			return &EditFailure{Kind: EditFailurePlacementAnchorModified}
		}
	}
	for _, anchor := range moveAnchors {
		if destructive[anchor] {
			return &EditFailure{Kind: EditFailurePlacementAnchorModified}
		}
	}
	return nil
}

// appendFragment appends one fragment under the byte budget
// (edit.rs:1080-1093).
func appendFragment(output *[]byte, fragment []byte, max int) *EditFailure {
	if len(*output)+len(fragment) > max {
		return &EditFailure{Kind: EditFailureResourceLimit, LimitName: "insert-fragment"}
	}
	*output = append(*output, fragment...)
	return nil
}

// sourcePatchLimits derives the patch budgets from the parse limits
// (edit.rs:1095-1108).
func sourcePatchLimits(parseLimits document.ParseLimits, operationCount int) document.SourcePatchLimits {
	return document.SourcePatchLimits{
		Source: document.SourceLimits{
			MaxRawBytes:         parseLimits.MaxSourceBytes,
			MaxDecodedUTF8Bytes: parseLimits.MaxSourceBytes,
			MaxDecodedScalars:   parseLimits.MaxSourceBytes,
		},
		MaxReplacements: operationCount,
		MaxPatchBytes:   parseLimits.MaxSourceBytes * 2,
	}
}

// operationMetadata builds the deterministic patch metadata
// (edit.rs:1110-1133).
func operationMetadata(tx *EditTransaction) map[string]string {
	metadata := make(map[string]string, len(tx.operations))
	for index, operation := range tx.operations {
		metadata["operation."+uint64String(uint64(index))] = operationID(&operation)
	}
	return metadata
}

// operationID returns the stable operation identifier with its version.
func operationID(operation *EditOperation) string {
	switch operation.Kind {
	case EditOperationReplaceScalar:
		if operation.Scalar.Kind == ScalarReplacementSemantic {
			return "json.edit.replace-scalar-semantic@1"
		}
		return "json.edit.replace-scalar-literal@1"
	case EditOperationInsertMember:
		return "json.edit.insert-member@1"
	case EditOperationRemoveMember:
		return "json.edit.remove-member@1"
	case EditOperationMoveMember:
		return "json.edit.move-member@1"
	case EditOperationRenameMember:
		return "json.edit.rename-member@1"
	case EditOperationInsertArrayElement:
		return "json.edit.insert-array-element@1"
	case EditOperationRemoveArrayElement:
		return "json.edit.remove-array-element@1"
	}
	return "json.edit.unknown@1"
}

// operationSummaries builds the safe content-free summaries
// (edit.rs:1135-1229).
func operationSummaries(tx *EditTransaction) ([]*protocol.EditOperationSummary, *EditFailure) {
	summaries := make([]*protocol.EditOperationSummary, 0, len(tx.operations))
	for _, operation := range tx.operations {
		var id string
		var targetRole string
		arguments := make(map[string]string)
		switch operation.Kind {
		case EditOperationReplaceScalar:
			targetRole = "json.scalar@1"
			if operation.Scalar.Kind == ScalarReplacementSemantic {
				id = "json.edit.replace-scalar-semantic"
				arguments["representation_policy"] = jsonPolicyName(operation.Scalar.Policy)
				arguments["value_kind"] = valueKindName(operation.Scalar.Value.Kind())
			} else {
				id = "json.edit.replace-scalar-literal"
				arguments["literal_bytes"] = uint64String(uint64(len(operation.Scalar.Literal)))
			}
		case EditOperationInsertMember:
			id = "json.edit.insert-member"
			targetRole = "json.object@1"
			arguments["name_bytes"] = uint64String(uint64(len(operation.Name)))
			arguments["placement"] = placementName(operation.Placement)
			arguments["value_kind"] = valueKindName(operation.Value.Kind())
		case EditOperationRemoveMember:
			id = "json.edit.remove-member"
			targetRole = "json.object-member@1"
		case EditOperationMoveMember:
			id = "json.edit.move-member"
			targetRole = "json.object-member@1"
			arguments["placement"] = placementName(operation.Placement)
		case EditOperationRenameMember:
			id = "json.edit.rename-member"
			targetRole = "json.object-member@1"
			arguments["name_bytes"] = uint64String(uint64(len(operation.Name)))
		case EditOperationInsertArrayElement:
			id = "json.edit.insert-array-element"
			targetRole = "json.array@1"
			arguments["placement"] = placementName(operation.Placement)
			arguments["value_kind"] = valueKindName(operation.Value.Kind())
		case EditOperationRemoveArrayElement:
			id = "json.edit.remove-array-element"
			targetRole = "json.array-element@1"
		}
		arguments["target_role"] = targetRole
		summary, err := protocol.NewEditOperationSummary(
			protocol.NewFormatOperationId(id, 1), arguments)
		if err != nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

// placementName renders one placement's stable name (edit.rs:1231-1238).
func placementName(placement AssociationPlacement) string {
	switch placement.Kind() {
	case PlacementStart:
		return "start"
	case PlacementEnd:
		return "end"
	case PlacementBefore:
		return "before"
	case PlacementAfter:
		return "after"
	}
	return "start"
}

// jsonPolicyName renders one policy's stable name (edit.rs:1240-1247).
func jsonPolicyName(policy RepresentationPolicy) string {
	switch policy {
	case RepresentationPolicyExactLiteral:
		return "exact-literal"
	case RepresentationPolicyPreserveCompatible:
		return "preserve-compatible"
	case RepresentationPolicyCanonicalForProfile:
		return "canonical-for-profile"
	case RepresentationPolicyPreserveElseCanonical:
		return "preserve-else-canonical"
	}
	return "exact-literal"
}

// valueKindName renders one PortableValue kind's stable name
// (edit.rs:1249-1267).
func valueKindName(kind core.Kind) string {
	return kind.String()
}

// semanticLiteral resolves one semantic replacement under its policy
// (edit.rs:1346-1386).
func semanticLiteral(value core.Value, old *internalValueKind, oldLiteral string,
	profile JsonProfile, policy RepresentationPolicy, authority document.DocumentAuthority,
	targetSpan document.Span, diagnostics *[]*protocol.Diagnostic) ([]byte, *EditFailure) {
	if policy == RepresentationPolicyExactLiteral {
		return nil, &EditFailure{Kind: EditFailureExactLiteralRequiresLiteralOperation}
	}
	if portableJSONKind(value, profile) == nil {
		return nil, &EditFailure{Kind: EditFailureUnsupportedSemanticValue,
			ValueKind: value.Kind()}
	}
	var preserved []byte
	if style := analyzeLexicalStyle(oldLiteral, old); style != nil {
		preserved = renderPreservingStyle(value, style)
	}
	switch policy {
	case RepresentationPolicyPreserveCompatible:
		if preserved == nil {
			return nil, &EditFailure{Kind: EditFailureRepresentationIncompatible}
		}
		return preserved, nil
	case RepresentationPolicyCanonicalForProfile:
		return canonicalLiteral(value, profile)
	case RepresentationPolicyPreserveElseCanonical:
		if preserved != nil {
			return preserved, nil
		}
		diagnostic, err := protocol.NewDiagnostic("json.edit.representation-fallback@1",
			protocol.CategoryEdit, protocol.SeverityWarning,
			diagnosticLocation(authority, targetSpan), nil, nil, nil, nil,
			uint64(len(*diagnostics)), errorRegistry())
		if err == nil {
			*diagnostics = append(*diagnostics, diagnostic)
		}
		return canonicalLiteral(value, profile)
	}
	return nil, &EditFailure{Kind: EditFailureRepresentationIncompatible}
}

// maxPreservedFractionDigits bounds a preserved fixed-fraction rendering
// (edit.rs:1388-1389).
const maxPreservedFractionDigits = 1_000_000

// jsonScalarLexicalStyle is the bounded lexical style retained by
// PreserveCompatible edits (edit.rs:1391-1440).
type jsonScalarLexicalStyle struct {
	kind           jsonScalarStyleKind
	radix          integerRadix
	explicitPlus   bool
	fractionScale  *int
	exponentMarker byte
	exponentPlus   bool
	leadingPlus    bool
	leadingPoint   bool
	hexUpperPrefix bool
	hexUpperDigits bool
	quote          rune
	escapes        map[string]string // decoded char -> escape text
}

// jsonScalarStyleKind is the closed style category.
type jsonScalarStyleKind uint8

const (
	styleNull jsonScalarStyleKind = iota
	styleBoolean
	styleInteger
	styleDecimal
	styleNonFinite
	styleString
)

// integerRadix is the frozen integer radix style.
type integerRadix uint8

const (
	radixDecimal integerRadix = iota
	radixHex
)

// analyzeLexicalStyle recovers one literal's lexical style
// (edit.rs:1442-1504).
func analyzeLexicalStyle(literal string, old *internalValueKind) *jsonScalarLexicalStyle {
	text := literal
	switch old.tag {
	case internalNull:
		return &jsonScalarLexicalStyle{kind: styleNull}
	case internalBoolean:
		return &jsonScalarLexicalStyle{kind: styleBoolean}
	case internalInteger:
		unsigned := stripSign(text)
		radix := radixDecimal
		upperPrefix := false
		upperDigits := false
		if hex, ok := strings.CutPrefix(unsigned, "0x"); ok {
			radix = radixHex
			upperDigits = strings.ContainsAny(hex, "ABCDEF")
		} else if hex, ok := strings.CutPrefix(unsigned, "0X"); ok {
			radix = radixHex
			upperPrefix = true
			upperDigits = strings.ContainsAny(hex, "ABCDEF")
		}
		return &jsonScalarLexicalStyle{kind: styleInteger, radix: radix,
			explicitPlus: strings.HasPrefix(text, "+"), hexUpperPrefix: upperPrefix,
			hexUpperDigits: upperDigits}
	case internalDecimal:
		unsigned := stripSign(text)
		exponentIndex := strings.IndexAny(unsigned, "eE")
		mantissa := unsigned
		if exponentIndex >= 0 {
			mantissa = unsigned[:exponentIndex]
		}
		var fractionScale *int
		if index := strings.IndexByte(mantissa, '.'); index >= 0 {
			scale := len(mantissa) - index - 1
			fractionScale = &scale
		}
		var exponentMarker byte
		exponentPlus := false
		if exponentIndex >= 0 {
			exponentMarker = unsigned[exponentIndex]
			if exponentIndex+1 < len(unsigned) && unsigned[exponentIndex+1] == '+' {
				exponentPlus = true
			}
		}
		return &jsonScalarLexicalStyle{kind: styleDecimal, fractionScale: fractionScale,
			exponentMarker: exponentMarker, exponentPlus: exponentPlus,
			leadingPlus:  strings.HasPrefix(text, "+"),
			leadingPoint: strings.HasPrefix(mantissa, ".")}
	case internalBinaryFloat64:
		return &jsonScalarLexicalStyle{kind: styleNonFinite,
			explicitPlus: strings.HasPrefix(text, "+")}
	case internalString:
		return analyzeStringStyle(text)
	}
	return nil
}

// stripSign removes one leading sign from a literal.
func stripSign(text string) string {
	if len(text) > 0 && (text[0] == '+' || text[0] == '-') {
		return text[1:]
	}
	return text
}

// analyzeStringStyle recovers one string literal's quote and escape style
// (edit.rs:1506-1579).
func analyzeStringStyle(literal string) *jsonScalarLexicalStyle {
	text := literal
	if len(text) < 2 {
		return nil
	}
	quote := rune(text[0])
	if (quote != '\'' && quote != '"') || rune(text[len(text)-1]) != quote {
		return nil
	}
	style := &jsonScalarLexicalStyle{kind: styleString, quote: quote,
		escapes: make(map[string]string)}
	chars := []rune(text)
	end := len(chars) - 1
	offset := 1
	for offset < end {
		character := chars[offset]
		if character != '\\' {
			offset++
			continue
		}
		escapeStart := offset
		offset++
		if offset >= end {
			return nil
		}
		escaped := chars[offset]
		offset++
		var decoded rune
		valid := true
		switch escaped {
		case '"':
			decoded = '"'
		case '\'':
			decoded = '\''
		case '\\':
			decoded = '\\'
		case '/':
			decoded = '/'
		case 'b':
			decoded = '\b'
		case 'f':
			decoded = '\f'
		case 'n':
			decoded = '\n'
		case 'r':
			decoded = '\r'
		case 't':
			decoded = '\t'
		case 'v':
			decoded = '\v'
		case '0':
			decoded = 0
		case 'x':
			if offset+2 > end {
				valid = false
				break
			}
			value, ok := hexFromRunes(chars[offset : offset+2])
			if !ok {
				valid = false
				break
			}
			decoded = rune(value)
			offset += 2
		case 'u':
			if offset+4 > end {
				valid = false
				break
			}
			first, ok := hexQuadFromRunes(chars[offset : offset+4])
			if !ok {
				valid = false
				break
			}
			offset += 4
			scalar := uint32(first)
			if first >= 0xd800 && first <= 0xdbff {
				if offset+6 > end || chars[offset] != '\\' || chars[offset+1] != 'u' {
					valid = false
					break
				}
				second, ok := hexQuadFromRunes(chars[offset+2 : offset+6])
				if !ok || second < 0xdc00 || second > 0xdfff {
					valid = false
					break
				}
				scalar = 0x1_0000 + (uint32(first)-0xd800)<<10 + (uint32(second) - 0xdc00)
				offset += 6
			}
			decoded = rune(scalar)
		case '\r':
			if offset < end && chars[offset] == '\n' {
				offset++
			}
			valid = false
		case '\n', '\u2028', '\u2029':
			valid = false
		default:
			decoded = escaped
		}
		if valid {
			style.escapes[string(decoded)] = text[escapeStart:offset]
		}
	}
	return style
}

// hexFromRunes decodes two hexadecimal runes.
func hexFromRunes(chars []rune) (byte, bool) {
	if len(chars) != 2 {
		return 0, false
	}
	first := hexDigitFromRune(chars[0])
	second := hexDigitFromRune(chars[1])
	if first < 0 || second < 0 {
		return 0, false
	}
	return byte(first)*16 + byte(second), true
}

// hexQuadFromRunes decodes four hexadecimal runes.
func hexQuadFromRunes(chars []rune) (uint16, bool) {
	if len(chars) != 4 {
		return 0, false
	}
	var value uint16
	for _, character := range chars {
		digit := hexDigitFromRune(character)
		if digit < 0 {
			return 0, false
		}
		value = value*16 + uint16(digit)
	}
	return value, true
}

// renderPreservingStyle renders one value under the recovered style
// (edit.rs:1581-1613).
func renderPreservingStyle(value core.Value, style *jsonScalarLexicalStyle) []byte {
	switch style.kind {
	case styleNull:
		if value.Kind() == core.KindNull {
			return []byte("null")
		}
	case styleBoolean:
		if value.Kind() == core.KindBoolean {
			if value.(core.Boolean) {
				return []byte("true")
			}
			return []byte("false")
		}
	case styleInteger:
		if value.Kind() == core.KindInteger {
			return renderIntegerStyle(value.(core.Integer).Int(), style)
		}
	case styleDecimal:
		if value.Kind() == core.KindDecimal || value.Kind() == core.KindInteger {
			return renderDecimalStyle(value, style)
		}
	case styleNonFinite:
		if value.Kind() == core.KindBinaryFloat64 {
			return renderNonFiniteStyle(value.(core.BinaryFloat64), style)
		}
	case styleString:
		if value.Kind() == core.KindString {
			return []byte(renderStringStyle(string(value.(core.String)), style))
		}
	}
	return nil
}

// renderIntegerStyle renders one integer under the recovered style
// (edit.rs:1615-1651).
func renderIntegerStyle(value *big.Int, style *jsonScalarLexicalStyle) []byte {
	var output strings.Builder
	if value.Sign() < 0 {
		output.WriteByte('-')
	} else if style.explicitPlus {
		output.WriteByte('+')
	}
	switch style.radix {
	case radixDecimal:
		output.WriteString(new(big.Int).Abs(value).String())
	case radixHex:
		if style.hexUpperPrefix {
			output.WriteString("0X")
		} else {
			output.WriteString("0x")
		}
		magnitude := new(big.Int).Abs(value).Bytes()
		if len(magnitude) == 0 {
			output.WriteByte('0')
		} else {
			for index, octet := range magnitude {
				if style.hexUpperDigits {
					if index == 0 {
						output.WriteString(strings.ToUpper(strconvFormat(octet, 16, 1)))
					} else {
						output.WriteString(strings.ToUpper(strconvFormat(octet, 16, 2)))
					}
				} else {
					if index == 0 {
						output.WriteString(strconvFormat(octet, 16, 1))
					} else {
						output.WriteString(strconvFormat(octet, 16, 2))
					}
				}
			}
		}
	}
	return []byte(output.String())
}

// strconvFormat formats one octet in base 16 with a minimum width.
func strconvFormat(octet byte, base int, width int) string {
	text := strconv.FormatUint(uint64(octet), base)
	for len(text) < width {
		text = "0" + text
	}
	return text
}

// renderDecimalStyle renders one decimal or integer under the recovered
// style (edit.rs:1653-1702).
func renderDecimalStyle(value core.Value, style *jsonScalarLexicalStyle) []byte {
	var coefficient *big.Int
	var exponent *big.Int
	switch value.Kind() {
	case core.KindDecimal:
		decimal := value.(core.Decimal)
		coefficient = decimal.Coefficient()
		exponent = decimal.Exponent()
	case core.KindInteger:
		coefficient = value.(core.Integer).Int()
		exponent = big.NewInt(0)
	default:
		return nil
	}
	var output string
	if style.exponentMarker != 0 {
		scale := 0
		if style.fractionScale != nil {
			scale = *style.fractionScale
		}
		var mantissa string
		if style.fractionScale != nil {
			mantissa = decimalFixedText(coefficient, scale)
		} else {
			mantissa = coefficient.String()
		}
		if style.leadingPoint {
			var ok bool
			mantissa, ok = removeLeadingZero(mantissa)
			if !ok {
				return nil
			}
		}
		exponentValue := exponent.Int64()
		if !exponent.IsInt64() {
			return nil
		}
		exponentValue += int64(scale)
		mantissa += string(rune(style.exponentMarker))
		if exponentValue >= 0 && style.exponentPlus {
			mantissa += "+"
		}
		mantissa += strconv.FormatInt(exponentValue, 10)
		output = mantissa
	} else {
		if style.fractionScale == nil {
			return nil
		}
		scale := *style.fractionScale
		shift := 0
		exponentValue := exponent.Int64()
		if !exponent.IsInt64() {
			return nil
		}
		if exponentValue >= 0 {
			shift = int(exponentValue) + scale
			if int64(shift) != exponentValue+int64(scale) {
				return nil
			}
		} else {
			negative := -exponentValue
			if negative > int64(scale) {
				return nil
			}
			shift = scale - int(negative)
		}
		if shift > maxPreservedFractionDigits {
			return nil
		}
		mantissa := new(big.Int).Mul(coefficient, pow10(shift))
		output = decimalFixedText(mantissa, scale)
		if style.leadingPoint {
			var ok bool
			output, ok = removeLeadingZero(output)
			if !ok {
				return nil
			}
		}
	}
	if style.leadingPlus && !strings.HasPrefix(output, "-") {
		output = "+" + output
	}
	return []byte(output)
}

// pow10 returns 10^n.
func pow10(power int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(power)), nil)
}

// removeLeadingZero removes the leading zero of a fixed decimal
// (edit.rs:1704-1709).
func removeLeadingZero(text string) (string, bool) {
	zero := 0
	if strings.HasPrefix(text, "-0.") {
		zero = 1
	}
	if len(text) < zero+2 || text[zero:zero+2] != "0." {
		return "", false
	}
	return text[:zero] + text[zero+1:], true
}

// renderNonFiniteStyle renders one frozen non-finite literal under the
// recovered style (edit.rs:1711-1725).
func renderNonFiniteStyle(value core.BinaryFloat64, style *jsonScalarLexicalStyle) []byte {
	switch value.Bits() {
	case 0x7ff0_0000_0000_0000:
		if style.explicitPlus {
			return []byte("+Infinity")
		}
		return []byte("Infinity")
	case 0xfff0_0000_0000_0000:
		return []byte("-Infinity")
	case 0x7ff8_0000_0000_0000:
		if style.explicitPlus {
			return []byte("+NaN")
		}
		return []byte("NaN")
	case 0xfff8_0000_0000_0000:
		return []byte("-NaN")
	}
	return nil
}

// decimalFixedText renders one coefficient at a fixed scale
// (edit.rs:1727-1739).
func decimalFixedText(mantissa *big.Int, scale int) string {
	text := mantissa.String()
	sign := ""
	digits := text
	if strings.HasPrefix(text, "-") {
		sign = "-"
		digits = text[1:]
	}
	if len(digits) <= scale {
		return sign + "0." + strings.Repeat("0", scale-len(digits)) + digits
	}
	split := len(digits) - scale
	return sign + digits[:split] + "." + digits[split:]
}

// renderStringStyle renders one string under the recovered quote and
// escape style (edit.rs:1741-1753).
func renderStringStyle(value string, style *jsonScalarLexicalStyle) string {
	var output strings.Builder
	output.WriteRune(style.quote)
	for _, character := range value {
		if escape, exists := style.escapes[string(character)]; exists {
			output.WriteString(escape)
		} else {
			pushJSONStringChar(&output, character, style.quote, false)
		}
	}
	output.WriteRune(style.quote)
	return output.String()
}

// portableJSONKind resolves one value's representable JSON category
// (edit.rs:1755-1767).
func portableJSONKind(value core.Value, profile JsonProfile) *JsonValueKind {
	switch value.Kind() {
	case core.KindNull:
		kind := JsonValueKindNull
		return &kind
	case core.KindBoolean:
		kind := JsonValueKindBoolean
		return &kind
	case core.KindInteger:
		kind := JsonValueKindInteger
		return &kind
	case core.KindDecimal:
		kind := JsonValueKindDecimal
		return &kind
	case core.KindBinaryFloat64:
		if profile.isJSON5() {
			kind := JsonValueKindBinaryFloat64
			return &kind
		}
	case core.KindString:
		kind := JsonValueKindString
		return &kind
	}
	return nil
}

// canonicalLiteral renders one value in deterministic profile-canonical
// syntax (edit.rs:1769-1795).
func canonicalLiteral(value core.Value, profile JsonProfile) ([]byte, *EditFailure) {
	var text string
	switch value.Kind() {
	case core.KindNull:
		text = "null"
	case core.KindBoolean:
		if value.(core.Boolean) {
			text = "true"
		} else {
			text = "false"
		}
	case core.KindInteger:
		text = value.(core.Integer).String()
	case core.KindDecimal:
		decimal := value.(core.Decimal)
		text = decimal.Coefficient().String() + "e" + decimal.Exponent().String()
	case core.KindBinaryFloat64:
		if !profile.isJSON5() {
			return nil, &EditFailure{Kind: EditFailureUnsupportedSemanticValue,
				ValueKind: core.KindBinaryFloat64}
		}
		spelling := renderNonFiniteStyle(value.(core.BinaryFloat64),
			&jsonScalarLexicalStyle{kind: styleNonFinite})
		if spelling == nil {
			return nil, &EditFailure{Kind: EditFailureUnsupportedSemanticValue,
				ValueKind: core.KindBinaryFloat64}
		}
		return spelling, nil
	case core.KindString:
		text = encodeJSONString(string(value.(core.String)), profile.isJSON5())
	default:
		return nil, &EditFailure{Kind: EditFailureUnsupportedSemanticValue,
			ValueKind: value.Kind()}
	}
	return []byte(text), nil
}

// encodeJSONString renders one string in canonical double-quoted syntax
// (edit.rs:1797-1805).
func encodeJSONString(value string, json5 bool) string {
	var output strings.Builder
	output.WriteByte('"')
	for _, character := range value {
		pushJSONStringChar(&output, character, '"', json5)
	}
	output.WriteByte('"')
	return output.String()
}

// pushJSONStringChar writes one escaped string character
// (edit.rs:1807-1829).
func pushJSONStringChar(output *strings.Builder, character rune, quote rune, canonicalJSON5 bool) {
	switch {
	case character == quote:
		output.WriteByte('\\')
		output.WriteRune(character)
	case character == '\\':
		output.WriteString(`\\`)
	case character == '\b':
		output.WriteString(`\b`)
	case character == '\f':
		output.WriteString(`\f`)
	case character == '\n':
		output.WriteString(`\n`)
	case character == '\r':
		output.WriteString(`\r`)
	case character == '\t':
		output.WriteString(`\t`)
	case character <= '\u001f':
		output.WriteString(escapeUpperHex(character))
	case (character == '\u2028' || character == '\u2029') && canonicalJSON5:
		output.WriteString(escapeUpperHex(character))
	default:
		output.WriteRune(character)
	}
}

// escapeUpperHex renders one \uXXXX escape with uppercase hex digits
// (the edit canonical spelling).
func escapeUpperHex(character rune) string {
	const digits = "0123456789ABCDEF"
	value := uint32(character)
	return "\\u" + string([]byte{
		digits[(value>>12)&0xf], digits[(value>>8)&0xf], digits[(value>>4)&0xf], digits[value&0xf],
	})
}

// validateLiteral verifies one exact literal candidate
// (edit.rs:1831-1862).
func validateLiteral(literal []byte, profile JsonProfile,
	limits document.ParseLimits) (JsonValueKind, *EditFailure) {
	if len(literal) == 0 || !utf8Valid(literal) {
		return JsonValueKindNull, &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	doc, formationFailure := Parse(context.Background(), literal, profile, limits)
	if formationFailure != nil {
		return JsonValueKindNull, &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	root := doc.Root()
	span := root.Span()
	kind := root.Kind()
	if doc.formationStatus != document.FormationStatusComplete ||
		span.StartByte() != 0 || span.EndByte() != len(literal) {
		return JsonValueKindNull, &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	if !kind.IsAvailable() {
		return JsonValueKindNull, &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	switch kind.Value() {
	case JsonValueKindNull, JsonValueKindBoolean, JsonValueKindInteger, JsonValueKindDecimal,
		JsonValueKindBinaryFloat64, JsonValueKindString:
		return kind.Value(), nil
	}
	return JsonValueKindNull, &EditFailure{Kind: EditFailureInvalidLiteral}
}

// utf8Valid reports whether the bytes are valid UTF-8.
func utf8Valid(bytes []byte) bool {
	return utf8.Valid(bytes)
}

// findValueByLiteralSpan locates the unique value whose literal span
// matches the new range (edit.rs:1864-1882).
func findValueByLiteralSpan(document *Document, start, end int) int {
	found := -1
	for index := range document.entities {
		item := &document.entities[index]
		if item.value != nil && item.value.literalSpan != nil &&
			item.value.literalSpan.StartByte() == start && item.value.literalSpan.EndByte() == end {
			if found >= 0 {
				return -1
			}
			found = index
		}
	}
	return found
}

// minInt returns the smaller of two integers.
func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

// maxInt returns the larger of two integers.
func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
