package properties

import (
	"sort"
	"strings"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the typed Java Properties edit operations and the
// atomic commit pipeline (consema-properties edit.rs; RFC 0010 §13). Both
// profiles publish the same five independently validated operations; an
// edit replaces only the bytes its operations own, every other byte is
// covered by the untouched-byte proof, and a failure never modifies the
// base document.

// EditOperationKind is the closed Properties edit operation category
// (edit.rs).
type EditOperationKind uint8

// The five frozen operation categories.
const (
	// EditOperationReplaceSemanticValue replaces one property's semantic
	// Java UTF-16 value.
	EditOperationReplaceSemanticValue EditOperationKind = iota
	// EditOperationReplaceLiteralValue replaces one property's exact raw
	// value literal.
	EditOperationReplaceLiteralValue
	// EditOperationInsertProperty inserts one canonical property
	// occurrence.
	EditOperationInsertProperty
	// EditOperationRemoveProperty removes one exact property occurrence and
	// all its natural lines.
	EditOperationRemoveProperty
	// EditOperationRenameProperty replaces one exact property's semantic
	// Java UTF-16 key.
	EditOperationRenameProperty
)

// EditOperation is one typed Java Properties edit operation bound to an
// immutable base snapshot (edit.rs). Only the fields of the declared
// Kind are meaningful.
type EditOperation struct {
	// Kind is the closed operation category.
	Kind EditOperationKind
	// Target is the exact property target (ReplaceSemanticValue,
	// ReplaceLiteralValue, RemoveProperty, RenameProperty).
	Target document.NodeRef
	// Value is the exact Java string (ReplaceSemanticValue, RenameProperty
	// key).
	Value JavaString
	// Literal is the raw literal bytes in the base document's selected
	// source encoding (ReplaceLiteralValue).
	Literal []byte
	// Document is the exact Properties document target (InsertProperty).
	Document document.NodeRef
	// Key is the exact Java UTF-16 key (InsertProperty).
	Key JavaString
	// Placement is the association placement (InsertProperty).
	Placement AssociationPlacement
}

// AssociationPlacement is the explicit association placement of one
// structural edit (document.AssociationPlacement; edit.rs
// AssociationPlacement).
type AssociationPlacement = document.AssociationPlacement

// PlacementStart is the start placement.
func PlacementStart() AssociationPlacement { return document.PlacementAtStart() }

// PlacementEnd is the end placement.
func PlacementEnd() AssociationPlacement { return document.PlacementAtEnd() }

// PlacementBefore places the new association before one exact association.
func PlacementBefore(anchor document.NodeRef) AssociationPlacement {
	return document.BeforeAnchor(anchor)
}

// PlacementAfter places the new association after one exact association.
func PlacementAfter(anchor document.NodeRef) AssociationPlacement {
	return document.AfterAnchor(anchor)
}

// EditTransaction is the immutable transaction; every operation resolves
// against one base snapshot (edit.rs).
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
// (edit.rs).
type EditTransactionBuilder struct {
	base       document.SnapshotIdentity
	operations []EditOperation
}

// NewEditTransactionBuilder binds a new transaction to one immutable base
// document.
func NewEditTransactionBuilder(document *Document) *EditTransactionBuilder {
	return &EditTransactionBuilder{base: document.SnapshotIdentity()}
}

// SemanticValue adds one semantic Java-string value replacement.
func (b *EditTransactionBuilder) SemanticValue(target document.NodeRef,
	value JavaString) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationReplaceSemanticValue, Target: target, Value: value,
	})
	return b
}

// LiteralValue adds one exact raw value-literal replacement.
func (b *EditTransactionBuilder) LiteralValue(target document.NodeRef,
	literal []byte) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationReplaceLiteralValue, Target: target,
		Literal: append([]byte(nil), literal...),
	})
	return b
}

// InsertProperty adds one canonical property insertion.
func (b *EditTransactionBuilder) InsertProperty(document document.NodeRef, key, value JavaString,
	placement AssociationPlacement) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationInsertProperty, Document: document, Key: key, Value: value,
		Placement: placement,
	})
	return b
}

// RemoveProperty adds one exact property removal.
func (b *EditTransactionBuilder) RemoveProperty(target document.NodeRef) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRemoveProperty, Target: target,
	})
	return b
}

// RenameProperty adds one semantic Java-string property rename.
func (b *EditTransactionBuilder) RenameProperty(target document.NodeRef,
	key JavaString) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRenameProperty, Target: target, Value: key,
	})
	return b
}

// Build completes the immutable request; target validation happens
// atomically at commit (edit.rs).
func (b *EditTransactionBuilder) Build() *EditTransaction {
	return &EditTransaction{
		base:       b.base,
		operations: append([]EditOperation(nil), b.operations...),
	}
}

// EditCommit is the atomic edit success (edit.rs).
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

// EditFailureKind is the stable edit validation or commit failure category
// (edit.rs).
type EditFailureKind uint8

// The closed edit failure categories.
const (
	// EditFailureRecoveredDocument: edits are forbidden on a recovered
	// document.
	EditFailureRecoveredDocument EditFailureKind = iota
	// EditFailureWrongSnapshot: the transaction or target belongs to
	// another snapshot.
	EditFailureWrongSnapshot
	// EditFailureWrongRole: the target has the wrong structural role.
	EditFailureWrongRole
	// EditFailureDuplicateTarget: more than one operation names the same
	// exact property.
	EditFailureDuplicateTarget
	// EditFailureOverlappingOwnership: prepared source ownership intervals
	// overlap or share an insertion point.
	EditFailureOverlappingOwnership
	// EditFailureInvalidPlacement: the placement is invalid or names an
	// unavailable anchor.
	EditFailureInvalidPlacement
	// EditFailurePlacementAnchorRemoved: an insertion anchor is removed by
	// this transaction.
	EditFailurePlacementAnchorRemoved
	// EditFailureTargetNotFound: a target no longer exists in the base
	// snapshot.
	EditFailureTargetNotFound
	// EditFailureEncodingUnrepresentable: a semantic Java string cannot be
	// represented by the selected source encoding.
	EditFailureEncodingUnrepresentable
	// EditFailureInvalidLiteral: the literal bytes do not form exactly one
	// raw value element.
	EditFailureInvalidLiteral
	// EditFailureResourceLimit: a configured edit or output bound was
	// exceeded.
	EditFailureResourceLimit
	// EditFailureNewDocumentFormationFailed: the replacement bytes did not
	// close through exact reparse and semantic verification.
	EditFailureNewDocumentFormationFailed
)

// EditFailure is the typed edit failure. It implements error and the
// RFC 0016 §6 Code() contract with the frozen registered codes.
type EditFailure struct {
	// Kind identifies the failure.
	Kind EditFailureKind
	// LimitName is the stable limit name of a ResourceLimit.
	LimitName string
}

// Error implements error; the text is human presentation only.
func (e *EditFailure) Error() string {
	switch e.Kind {
	case EditFailureRecoveredDocument:
		return "properties: edits are forbidden on a recovered document"
	case EditFailureWrongSnapshot:
		return "properties: edit transaction or target belongs to another snapshot"
	case EditFailureWrongRole:
		return "properties: edit target has the wrong structural role"
	case EditFailureDuplicateTarget:
		return "properties: more than one operation names the same property"
	case EditFailureOverlappingOwnership:
		return "properties: prepared source ownership intervals overlap"
	case EditFailureInvalidPlacement:
		return "properties: edit placement is invalid"
	case EditFailurePlacementAnchorRemoved:
		return "properties: an insertion anchor is removed by this transaction"
	case EditFailureTargetNotFound:
		return "properties: edit target was not found"
	case EditFailureEncodingUnrepresentable:
		return "properties: edit value cannot be represented by the source encoding"
	case EditFailureInvalidLiteral:
		return "properties: edit literal does not form one exact value element"
	case EditFailureResourceLimit:
		return "properties: edit limit " + e.LimitName + " reached"
	case EditFailureNewDocumentFormationFailed:
		return "properties: replacement document could not be formed"
	}
	return "properties: edit failure"
}

// Name returns the stable failure name (the Rust variant spelling).
func (e *EditFailure) Name() string {
	switch e.Kind {
	case EditFailureRecoveredDocument:
		return "RecoveredDocument"
	case EditFailureWrongSnapshot:
		return "WrongSnapshot"
	case EditFailureWrongRole:
		return "WrongRole"
	case EditFailureDuplicateTarget:
		return "DuplicateTarget"
	case EditFailureOverlappingOwnership:
		return "OverlappingOwnership"
	case EditFailureInvalidPlacement:
		return "InvalidPlacement"
	case EditFailurePlacementAnchorRemoved:
		return "PlacementAnchorRemoved"
	case EditFailureTargetNotFound:
		return "TargetNotFound"
	case EditFailureEncodingUnrepresentable:
		return "EncodingUnrepresentable"
	case EditFailureInvalidLiteral:
		return "InvalidLiteral"
	case EditFailureResourceLimit:
		return "ResourceLimit"
	case EditFailureNewDocumentFormationFailed:
		return "NewDocumentFormationFailed"
	}
	return "EditFailure"
}

// Code returns the frozen registered code for the failure (edit.rs).
func (e *EditFailure) Code() string {
	switch e.Kind {
	case EditFailureRecoveredDocument:
		return "core.edit.incomplete-target@1"
	case EditFailureWrongSnapshot:
		return "core.edit.wrong-snapshot@1"
	case EditFailureWrongRole:
		return "core.edit.wrong-role@1"
	case EditFailureDuplicateTarget, EditFailureOverlappingOwnership,
		EditFailurePlacementAnchorRemoved:
		return "core.edit.conflicting-edits@1"
	case EditFailureInvalidPlacement:
		return "java-properties.edit.invalid-placement@1"
	case EditFailureTargetNotFound:
		return "core.edit.target-not-found@1"
	case EditFailureEncodingUnrepresentable:
		return "core.edit.representation-incompatible@1"
	case EditFailureInvalidLiteral:
		return "core.edit.invalid-literal@1"
	case EditFailureResourceLimit:
		return "core.edit.resource-limit@1"
	case EditFailureNewDocumentFormationFailed:
		return "core.edit.formation-failed@1"
	}
	return "core.edit.conflicting-edits@1"
}

// expectedProperty is one final document association expected after the
// commit (edit.rs).
type expectedProperty struct {
	old            document.NodeRef
	hasOld         bool
	key            JavaString
	value          *JavaString
	literal        bool
	literalOldSpan *document.Span
	removed        bool
}

// preparedEdit is one byte-level replacement of the commit (edit.rs:
// 265-268).
type preparedEdit struct {
	oldSpan     document.Span
	replacement []byte
}

// Commit atomically commits every declared Properties operation
// (edit.rs). On failure the base document remains unchanged.
func (d *Document) Commit(tx *EditTransaction) (*EditCommit, *EditFailure) {
	if d.formationStatus != document.FormationStatusComplete {
		return nil, &EditFailure{Kind: EditFailureRecoveredDocument}
	}
	if tx.base != d.SnapshotIdentity() {
		return nil, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if len(tx.operations) > d.parseLimits.Common.MaxNodeCount {
		return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "edit-operations"}
	}
	if failure := d.validateRemovedAnchors(tx); failure != nil {
		return nil, failure
	}
	targets := make(map[document.NodeRef]bool, len(tx.operations))
	insertBoundaries := make(map[int]bool, len(tx.operations))
	var diagnostics []*protocol.Diagnostic
	var prepared []preparedEdit
	expected := make([]expectedProperty, 0, len(d.properties))
	for _, property := range d.properties {
		value := property.value
		expected = append(expected, expectedProperty{
			old:    property.node,
			hasOld: true,
			key:    property.key,
			value:  &value,
		})
	}
	insertions := make(map[int]expectedProperty, len(tx.operations))
	for index := range tx.operations {
		operation := &tx.operations[index]
		target, destructive := destructiveTarget(operation)
		if destructive {
			if targets[target] {
				return nil, &EditFailure{Kind: EditFailureDuplicateTarget}
			}
			targets[target] = true
		}
		switch operation.Kind {
		case EditOperationReplaceSemanticValue:
			ordinal, failure := d.propertyOrdinal(operation.Target)
			if failure != nil {
				return nil, failure
			}
			property := &d.properties[ordinal]
			oldSpan, failure := d.valueOwnership(property)
			if failure != nil {
				return nil, failure
			}
			var replacement []byte
			if direct := d.preserveDirectValue(property, &operation.Value); direct != nil {
				replacement = direct
			} else {
				diagnostics = append(diagnostics, d.canonicalFallbackDiagnostic(property.span))
				replacement, failure = d.canonicalFragment(&operation.Value, false)
				if failure != nil {
					return nil, failure
				}
			}
			value := operation.Value
			expected[ordinal].value = &value
			prepared = append(prepared, preparedEdit{oldSpan: oldSpan, replacement: replacement})
		case EditOperationReplaceLiteralValue:
			ordinal, failure := d.propertyOrdinal(operation.Target)
			if failure != nil {
				return nil, failure
			}
			if failure := d.validateLiteral(operation.Literal); failure != nil {
				return nil, failure
			}
			property := &d.properties[ordinal]
			oldSpan, failure := d.valueOwnership(property)
			if failure != nil {
				return nil, failure
			}
			expected[ordinal].value = nil
			expected[ordinal].literal = true
			expected[ordinal].literalOldSpan = &oldSpan
			prepared = append(prepared, preparedEdit{
				oldSpan: oldSpan, replacement: append([]byte(nil), operation.Literal...),
			})
		case EditOperationInsertProperty:
			if failure := d.validateDocumentTarget(operation.Document); failure != nil {
				return nil, failure
			}
			boundary, position, failure := d.insertionLocation(operation.Placement)
			if failure != nil {
				return nil, failure
			}
			if insertBoundaries[boundary] {
				return nil, &EditFailure{Kind: EditFailureOverlappingOwnership}
			}
			insertBoundaries[boundary] = true
			value := operation.Value
			insertions[boundary] = expectedProperty{
				key: operation.Key, value: &value,
			}
			span, err := d.authority.Span(position, position)
			if err != nil {
				return nil, &EditFailure{Kind: EditFailureInvalidPlacement}
			}
			replacement, failure := d.canonicalRecord(position, &operation.Key, &operation.Value)
			if failure != nil {
				return nil, failure
			}
			prepared = append(prepared, preparedEdit{oldSpan: span, replacement: replacement})
		case EditOperationRemoveProperty:
			ordinal, failure := d.propertyOrdinal(operation.Target)
			if failure != nil {
				return nil, failure
			}
			expected[ordinal].removed = true
			oldSpan, failure := d.recordOwnership(&d.properties[ordinal])
			if failure != nil {
				return nil, failure
			}
			prepared = append(prepared, preparedEdit{oldSpan: oldSpan, replacement: []byte{}})
		case EditOperationRenameProperty:
			ordinal, failure := d.propertyOrdinal(operation.Target)
			if failure != nil {
				return nil, failure
			}
			expected[ordinal].key = operation.Value
			oldSpan, failure := d.keyOwnership(&d.properties[ordinal])
			if failure != nil {
				return nil, failure
			}
			replacement, failure := d.canonicalFragment(&operation.Value, true)
			if failure != nil {
				return nil, failure
			}
			prepared = append(prepared, preparedEdit{oldSpan: oldSpan, replacement: replacement})
		}
	}
	sort.SliceStable(prepared, func(i, j int) bool {
		if prepared[i].oldSpan.StartByte() != prepared[j].oldSpan.StartByte() {
			return prepared[i].oldSpan.StartByte() < prepared[j].oldSpan.StartByte()
		}
		return prepared[i].oldSpan.EndByte() < prepared[j].oldSpan.EndByte()
	})
	if failure := validateNonOverlapping(prepared); failure != nil {
		return nil, failure
	}
	finalExpected := assembleExpected(expected, insertions)
	closureFailure := EditFailureNewDocumentFormationFailed
	for _, item := range finalExpected {
		if item.literal {
			closureFailure = EditFailureInvalidLiteral
			break
		}
	}
	rendered, failure := d.applyPrepared(prepared)
	if failure != nil {
		return nil, failure
	}
	newDocument, formationFailure := parse(rendered, d.profile,
		originalEncodingSelection(d), d.parseLimits)
	if formationFailure != nil {
		return nil, &EditFailure{Kind: closureFailure}
	}
	if newDocument.formationStatus != document.FormationStatusComplete {
		return nil, &EditFailure{Kind: closureFailure}
	}
	if failure := verifyExpected(newDocument, finalExpected); failure != nil {
		return nil, &EditFailure{Kind: closureFailure}
	}
	sourceEdits, failure := buildSourceEdits(newDocument, prepared)
	if failure != nil {
		return nil, failure
	}
	if failure := verifyLiteralOwnership(newDocument, finalExpected, sourceEdits); failure != nil {
		return nil, failure
	}
	mappings := buildNodeMappings(newDocument, finalExpected, tx)
	changeSet := document.NewChangeSet(d.SnapshotIdentity(), newDocument.SnapshotIdentity(),
		sourceEdits, mappings, diagnostics)
	patchLimits := editSourcePatchLimits(d.parseLimits, len(sourceEdits))
	replacements := make([]document.SourceReplacement, 0, len(sourceEdits))
	source := d.source.Bytes()
	for _, edit := range sourceEdits {
		replacements = append(replacements, document.NewSourceReplacement(
			edit.OldSpan.StartByte(), edit.OldSpan.EndByte(),
			source[edit.OldSpan.StartByte():edit.OldSpan.EndByte()], edit.Replacement))
	}
	patch, patchErr := document.NewSourcePatch(d.source, replacements,
		operationMetadata(tx), patchLimits)
	if patchErr != nil || !patch.TargetDigest().Equal(newDocument.source.Digest()) {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	proof, proofErr := CreateUntouchedByteProof(d.source, newDocument.source,
		patch.Replacements())
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
// Document (edit.rs).
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

func destructiveTarget(operation *EditOperation) (document.NodeRef, bool) {
	switch operation.Kind {
	case EditOperationReplaceSemanticValue, EditOperationReplaceLiteralValue,
		EditOperationRemoveProperty, EditOperationRenameProperty:
		return operation.Target, true
	}
	return document.NodeRef{}, false
}

func (d *Document) propertyOrdinal(target document.NodeRef) (int, *EditFailure) {
	if target.Snapshot() != d.SnapshotIdentity() {
		return 0, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if target.Role() != document.RolePropertiesProperty {
		return 0, &EditFailure{Kind: EditFailureWrongRole}
	}
	for index, property := range d.properties {
		if property.node == target {
			return index, nil
		}
	}
	return 0, &EditFailure{Kind: EditFailureTargetNotFound}
}

func (d *Document) validateDocumentTarget(target document.NodeRef) *EditFailure {
	if target.Snapshot() != d.SnapshotIdentity() {
		return &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if target.Role() != document.RolePropertiesDocument {
		return &EditFailure{Kind: EditFailureWrongRole}
	}
	if target != d.NodeRef() {
		return &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return nil
}

// validateRemovedAnchors rejects insertions anchored at a removed
// property (edit.rs).
func (d *Document) validateRemovedAnchors(tx *EditTransaction) *EditFailure {
	removed := make(map[document.NodeRef]bool, len(tx.operations))
	for index := range tx.operations {
		operation := &tx.operations[index]
		if operation.Kind == EditOperationRemoveProperty {
			removed[operation.Target] = true
		}
	}
	for index := range tx.operations {
		operation := &tx.operations[index]
		if operation.Kind != EditOperationInsertProperty {
			continue
		}
		if (operation.Placement.Kind() == document.PlacementBefore ||
			operation.Placement.Kind() == document.PlacementAfter) &&
			removed[operation.Placement.Anchor()] {
			return &EditFailure{Kind: EditFailurePlacementAnchorRemoved}
		}
	}
	return nil
}

// insertionLocation resolves one placement to its (boundary, raw
// position) pair (edit.rs).
func (d *Document) insertionLocation(placement AssociationPlacement) (int, int, *EditFailure) {
	count := len(d.properties)
	switch placement.Kind() {
	case document.PlacementStart:
		position := d.source.Len()
		if len(d.properties) > 0 {
			span, failure := d.recordOwnership(&d.properties[0])
			if failure != nil {
				return 0, 0, failure
			}
			position = span.StartByte()
		}
		return 0, position, nil
	case document.PlacementEnd:
		return count, d.source.Len(), nil
	case document.PlacementBefore:
		ordinal, failure := d.propertyOrdinal(placement.Anchor())
		if failure != nil {
			return 0, 0, failure
		}
		span, failure := d.recordOwnership(&d.properties[ordinal])
		if failure != nil {
			return 0, 0, failure
		}
		return ordinal, span.StartByte(), nil
	case document.PlacementAfter:
		ordinal, failure := d.propertyOrdinal(placement.Anchor())
		if failure != nil {
			return 0, 0, failure
		}
		span, failure := d.recordOwnership(&d.properties[ordinal])
		if failure != nil {
			return 0, 0, failure
		}
		return ordinal + 1, span.EndByte(), nil
	}
	return 0, 0, &EditFailure{Kind: EditFailureInvalidPlacement}
}

// recordOwnership spans every natural line of one property's logical line
// (edit.rs).
func (d *Document) recordOwnership(property *Property) (document.Span, *EditFailure) {
	logical, err := d.LogicalLine(property.logicalLine)
	if err != nil {
		return document.Span{}, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	if len(logical.naturalLines) == 0 {
		return document.Span{}, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	first, err := d.NaturalLine(logical.naturalLines[0])
	if err != nil {
		return document.Span{}, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	last, err := d.NaturalLine(logical.naturalLines[len(logical.naturalLines)-1])
	if err != nil {
		return document.Span{}, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	span, err := d.authority.Span(first.span.StartByte(), last.span.EndByte())
	if err != nil {
		return document.Span{}, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return span, nil
}

func (d *Document) keyOwnership(property *Property) (document.Span, *EditFailure) {
	return fragmentOwnership(d.authority, property.keyFragments, property.keyAnchor)
}

func (d *Document) valueOwnership(property *Property) (document.Span, *EditFailure) {
	return fragmentOwnership(d.authority, property.valueFragments, property.valueAnchor)
}

func fragmentOwnership(authority document.DocumentAuthority, fragments []document.Span,
	anchor document.Span) (document.Span, *EditFailure) {
	if len(fragments) == 0 {
		return anchor, nil
	}
	span, err := authority.Span(fragments[0].StartByte(), fragments[len(fragments)-1].EndByte())
	if err != nil {
		return document.Span{}, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return span, nil
}

// preserveDirectValue reuses the direct raw value spelling when the
// replacement closes exactly without re-encoding (edit.rs).
func (d *Document) preserveDirectValue(property *Property, value *JavaString) []byte {
	logical, err := d.LogicalLine(property.logicalLine)
	if err != nil {
		return nil
	}
	if len(logical.naturalLines) != 1 {
		return nil
	}
	for _, node := range property.escapes {
		escape, err := d.Escape(node)
		if err != nil {
			return nil
		}
		if !escape.inKey {
			return nil
		}
	}
	text, err := value.ToUnicode()
	if err != nil {
		return nil
	}
	if strings.HasPrefix(text, " ") || strings.HasPrefix(text, "\t") ||
		strings.HasPrefix(text, "\u000C") || strings.ContainsAny(text, "\\\r\n") {
		return nil
	}
	fragment, failure := encodeFragment(text, d.source.EncodingFacts().Selected(),
		d.parseLimits.Common.MaxSourceBytes)
	if failure != nil {
		return nil
	}
	return fragment
}

// canonicalFragment emits the canonical escaped form of one Java string
// under the base document's profile and encoding (edit.rs).
func (d *Document) canonicalFragment(value *JavaString, isKey bool) ([]byte, *EditFailure) {
	text, failure := canonicalJavaString(value, d.profile, isKey,
		d.parseLimits.Common.MaxSourceBytes)
	if failure != nil {
		return nil, failure
	}
	fragment, encodeFailure := encodeFragment(text, d.source.EncodingFacts().Selected(),
		d.parseLimits.Common.MaxSourceBytes)
	if encodeFailure != nil {
		return nil, mapEncodingFailure(encodeFailure)
	}
	return fragment, nil
}

// canonicalRecord emits one canonical `key=value` record at one raw
// position, honoring the existing newline convention (edit.rs).
func (d *Document) canonicalRecord(position int, key, value *JavaString) ([]byte, *EditFailure) {
	newline := d.newlineConvention()
	text := ""
	if position > 0 {
		boundary, failure := d.isLineBoundary(position)
		if failure != nil {
			return nil, failure
		}
		if !boundary {
			if failure := pushBounded(&text, newline, d.parseLimits.Common.MaxSourceBytes); failure != nil {
				return nil, failure
			}
		}
	}
	keyText, failure := canonicalJavaString(key, d.profile, true,
		d.parseLimits.Common.MaxSourceBytes)
	if failure != nil {
		return nil, failure
	}
	valueText, failure := canonicalJavaString(value, d.profile, false,
		d.parseLimits.Common.MaxSourceBytes)
	if failure != nil {
		return nil, failure
	}
	if failure := pushBounded(&text, keyText, d.parseLimits.Common.MaxSourceBytes); failure != nil {
		return nil, failure
	}
	if failure := pushBounded(&text, "=", d.parseLimits.Common.MaxSourceBytes); failure != nil {
		return nil, failure
	}
	if failure := pushBounded(&text, valueText, d.parseLimits.Common.MaxSourceBytes); failure != nil {
		return nil, failure
	}
	if failure := pushBounded(&text, newline, d.parseLimits.Common.MaxSourceBytes); failure != nil {
		return nil, failure
	}
	fragment, encodeFailure := encodeFragment(text, d.source.EncodingFacts().Selected(),
		d.parseLimits.Common.MaxSourceBytes)
	if encodeFailure != nil {
		return nil, mapEncodingFailure(encodeFailure)
	}
	return fragment, nil
}

// newlineConvention derives the existing newline spelling from the first
// line break of the base source (edit.rs).
func (d *Document) newlineConvention() string {
	text, ok := d.source.DecodedText()
	if !ok {
		return "\n"
	}
	for index, character := range text {
		if character == '\r' {
			if index+1 < len(text) && text[index+1] == '\n' {
				return "\r\n"
			}
			return "\r"
		}
		if character == '\n' {
			return "\n"
		}
	}
	return "\n"
}

func (d *Document) isLineBoundary(raw int) (bool, *EditFailure) {
	position, err := d.source.DecodedPosition(raw)
	if err != nil {
		return false, &EditFailure{Kind: EditFailureInvalidPlacement}
	}
	text, _ := d.source.DecodedText()
	prefix := text[:position.DecodedUTF8Byte]
	return strings.HasSuffix(prefix, "\r") || strings.HasSuffix(prefix, "\n"), nil
}

// validateLiteral requires the literal bytes to decode into exactly one
// raw value element with no line break (edit.rs).
func (d *Document) validateLiteral(literal []byte) *EditFailure {
	if len(literal) > d.parseLimits.Common.MaxSourceBytes {
		return &EditFailure{Kind: EditFailureResourceLimit, LimitName: "replacement-bytes"}
	}
	encoding := d.source.EncodingFacts().Selected()
	request := document.NewEncodingRequest(encoding).
		WithCallerOverride(encoding).
		WithBomPolicy(document.BomPolicyTreatAsContent)
	snapshot, err := document.NewSourceSnapshotFromRaw(literal, request, document.SourceLimits{
		MaxRawBytes:         d.parseLimits.Common.MaxSourceBytes,
		MaxDecodedUTF8Bytes: d.parseLimits.MaxDecodedUTF8Bytes,
		MaxDecodedScalars:   d.parseLimits.MaxDecodedScalars,
	})
	if err != nil {
		return &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	text, ok := snapshot.DecodedText()
	if !ok {
		return &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	if strings.ContainsAny(text, "\r\n") {
		return &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	return nil
}

func (d *Document) canonicalFallbackDiagnostic(span document.Span) *protocol.Diagnostic {
	diagnostic, err := protocol.NewDiagnostic("java-properties.edit.canonical-fallback@1",
		protocol.CategoryEdit, protocol.SeverityWarning,
		&protocol.SourceLocation{StartByte: uint64(span.StartByte()),
			EndByte: uint64(span.EndByte())},
		nil, nil, nil, nil, 0, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		panic("properties: unregistered edit diagnostic code")
	}
	return diagnostic
}

func validateNonOverlapping(prepared []preparedEdit) *EditFailure {
	for index := 1; index < len(prepared); index++ {
		left, right := prepared[index-1], prepared[index]
		if left.oldSpan == right.oldSpan ||
			left.oldSpan.EndByte() > right.oldSpan.StartByte() ||
			(left.oldSpan.IsEmpty() && left.oldSpan.StartByte() == right.oldSpan.StartByte()) ||
			(right.oldSpan.IsEmpty() && left.oldSpan.EndByte() == right.oldSpan.StartByte()) {
			return &EditFailure{Kind: EditFailureOverlappingOwnership}
		}
	}
	return nil
}

// assembleExpected merges the base expectations with the ordered
// insertions (edit.rs).
func assembleExpected(old []expectedProperty, insertions map[int]expectedProperty) []expectedProperty {
	output := make([]expectedProperty, 0, len(old)+len(insertions))
	for boundary := 0; boundary <= len(old); boundary++ {
		if inserted, ok := insertions[boundary]; ok {
			output = append(output, inserted)
		}
		if boundary < len(old) && !old[boundary].removed {
			output = append(output, old[boundary])
		}
	}
	return output
}

func verifyExpected(document *Document, expected []expectedProperty) *EditFailure {
	if len(document.properties) != len(expected) {
		return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	for index, property := range document.properties {
		item := &expected[index]
		if !property.key.Equal(item.key) ||
			(item.value != nil && !property.value.Equal(*item.value)) {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
	}
	return nil
}

// buildSourceEdits maps every prepared replacement into exact old/new
// spans (edit.rs).
func buildSourceEdits(newDocument *Document, prepared []preparedEdit) ([]SourceEdit, *EditFailure) {
	delta := 0
	sourceEdits := make([]SourceEdit, 0, len(prepared))
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
		delta += len(edit.replacement) - edit.oldSpan.Len()
	}
	return sourceEdits, nil
}

// verifyLiteralOwnership requires every literal replacement to own exactly
// the reparsed value interval (edit.rs).
func verifyLiteralOwnership(document *Document, expected []expectedProperty,
	sourceEdits []SourceEdit) *EditFailure {
	for ordinal, item := range expected {
		if !item.literal {
			continue
		}
		oldSpan := item.literalOldSpan
		if oldSpan == nil {
			return &EditFailure{Kind: EditFailureInvalidLiteral}
		}
		var sourceEdit *SourceEdit
		for index := range sourceEdits {
			if sourceEdits[index].OldSpan == *oldSpan {
				sourceEdit = &sourceEdits[index]
				break
			}
		}
		if sourceEdit == nil {
			return &EditFailure{Kind: EditFailureInvalidLiteral}
		}
		if ordinal >= len(document.properties) {
			return &EditFailure{Kind: EditFailureInvalidLiteral}
		}
		ownership, failure := document.valueOwnership(&document.properties[ordinal])
		if failure != nil {
			return &EditFailure{Kind: EditFailureInvalidLiteral}
		}
		if sourceEdit.NewSpan != ownership {
			return &EditFailure{Kind: EditFailureInvalidLiteral}
		}
	}
	return nil
}

// buildNodeMappings records the old-to-new property identities
// (edit.rs).
func buildNodeMappings(document *Document, expected []expectedProperty,
	tx *EditTransaction) []NodeMapping {
	var mappings []NodeMapping
	for index := range tx.operations {
		operation := &tx.operations[index]
		switch operation.Kind {
		case EditOperationRemoveProperty:
			mappings = append(mappings, NodeMapping{
				Old: operation.Target, New: nil,
				Status: protocol.MappingDeleted,
			})
		case EditOperationReplaceSemanticValue, EditOperationReplaceLiteralValue,
			EditOperationRenameProperty:
			ordinal := -1
			for position, item := range expected {
				if item.hasOld && item.old == operation.Target {
					ordinal = position
					break
				}
			}
			if ordinal < 0 {
				continue
			}
			node := document.properties[ordinal].node
			mappings = append(mappings, NodeMapping{
				Old: operation.Target, New: &node,
				Status: protocol.MappingReplaced,
			})
		case EditOperationInsertProperty:
			// Insertions receive no node mapping.
		}
	}
	return mappings
}

// canonicalJavaString renders one exact Java string into canonical escaped
// text (edit.rs; RFC 0010 §7, §12). Unpaired surrogates keep
// their exact code units through uppercase `\uXXXX` escapes.
func canonicalJavaString(value *JavaString, profile PropertiesProfile, isKey bool,
	limit int) (string, *EditFailure) {
	output := ""
	leadingValueSpace := !isKey
	units := value.units
	for index := 0; index < len(units); {
		unit := units[index]
		var scalar rune
		if unit >= 0xD800 && unit <= 0xDBFF && index+1 < len(units) {
			next := units[index+1]
			if next >= 0xDC00 && next <= 0xDFFF {
				high := uint32(unit - 0xD800)
				low := uint32(next - 0xDC00)
				index += 2
				scalar = rune(0x10000 + (high << 10) + low)
			} else {
				index++
				if failure := pushUnicodeEscape(&output, unit, limit); failure != nil {
					return "", failure
				}
				leadingValueSpace = false
				continue
			}
		} else if unit >= 0xD800 && unit <= 0xDFFF {
			index++
			if failure := pushUnicodeEscape(&output, unit, limit); failure != nil {
				return "", failure
			}
			leadingValueSpace = false
			continue
		} else {
			index++
			scalar = rune(unit)
		}
		switch {
		case scalar == ' ' && (isKey || leadingValueSpace):
			if failure := pushBounded(&output, "\\ ", limit); failure != nil {
				return "", failure
			}
		case scalar == '\t':
			if failure := pushBounded(&output, "\\t", limit); failure != nil {
				return "", failure
			}
		case scalar == '\n':
			if failure := pushBounded(&output, "\\n", limit); failure != nil {
				return "", failure
			}
		case scalar == '\r':
			if failure := pushBounded(&output, "\\r", limit); failure != nil {
				return "", failure
			}
		case scalar == 0x0C:
			if failure := pushBounded(&output, "\\f", limit); failure != nil {
				return "", failure
			}
		case scalar == '\\':
			if failure := pushBounded(&output, "\\\\", limit); failure != nil {
				return "", failure
			}
		case scalar == '#' || scalar == '!' || scalar == '=' || scalar == ':':
			if failure := pushBounded(&output, "\\", limit); failure != nil {
				return "", failure
			}
			if failure := pushBounded(&output, string(scalar), limit); failure != nil {
				return "", failure
			}
		case isControlScalar(scalar):
			if failure := pushScalarEscape(&output, scalar, limit); failure != nil {
				return "", failure
			}
		case scalar > 0x7E && profile == PropertiesLatin1V1:
			if failure := pushScalarEscape(&output, scalar, limit); failure != nil {
				return "", failure
			}
		default:
			if failure := pushBounded(&output, string(scalar), limit); failure != nil {
				return "", failure
			}
		}
		if scalar != ' ' {
			leadingValueSpace = false
		}
	}
	return output, nil
}

// pushScalarEscape emits the canonical `\uXXXX` escapes of one scalar.
func pushScalarEscape(output *string, value rune, limit int) *EditFailure {
	if value > 0xFFFF {
		first := value - 0x10000
		if failure := pushUnicodeEscape(output, uint16(0xD800+(first>>10)), limit); failure != nil {
			return failure
		}
		return pushUnicodeEscape(output, uint16(0xDC00+(first&0x3FF)), limit)
	}
	return pushUnicodeEscape(output, uint16(value), limit)
}

func pushUnicodeEscape(output *string, value uint16, limit int) *EditFailure {
	const hex = "0123456789ABCDEF"
	text := []byte{'\\', 'u',
		hex[(value>>12)&0xF], hex[(value>>8)&0xF], hex[(value>>4)&0xF], hex[value&0xF]}
	return pushBounded(output, string(text), limit)
}

func pushBounded(output *string, text string, limit int) *EditFailure {
	if len(*output)+len(text) > limit {
		return &EditFailure{Kind: EditFailureResourceLimit, LimitName: "replacement-bytes"}
	}
	*output += text
	return nil
}

// mapEncodingFailure maps one encoding failure onto the edit contract
// (edit.rs).
func mapEncodingFailure(failure *MaterializationFailure) *EditFailure {
	if failure.Kind == MaterializationResourceLimit {
		return &EditFailure{Kind: EditFailureResourceLimit, LimitName: failure.LimitName}
	}
	return &EditFailure{Kind: EditFailureEncodingUnrepresentable}
}

// originalEncodingSelection rebuilds the parse selection of an edited
// document (edit.rs).
func originalEncodingSelection(document *Document) PropertiesEncodingSelection {
	if document.profile == PropertiesReaderV1 {
		return ReaderEncodingSelection(document.source.EncodingFacts().Selected())
	}
	return Latin1EncodingSelection()
}

func editSourcePatchLimits(limits PropertiesParseLimits, operationCount int) document.SourcePatchLimits {
	return document.SourcePatchLimits{
		Source: document.SourceLimits{
			MaxRawBytes:         limits.Common.MaxSourceBytes,
			MaxDecodedUTF8Bytes: limits.MaxDecodedUTF8Bytes,
			MaxDecodedScalars:   limits.MaxDecodedScalars,
		},
		MaxReplacements: operationCount,
		MaxPatchBytes:   limits.Common.MaxSourceBytes * 2,
	}
}

// operationMetadata builds the deterministic patch audit metadata
// (edit.rs).
func operationMetadata(tx *EditTransaction) map[string]string {
	metadata := make(map[string]string, len(tx.operations))
	for index := range tx.operations {
		metadata["operation."+intString(index)] = operationID(&tx.operations[index]) + "@1"
	}
	return metadata
}

func operationSummaries(tx *EditTransaction) ([]*protocol.EditOperationSummary, *EditFailure) {
	summaries := make([]*protocol.EditOperationSummary, 0, len(tx.operations))
	for index := range tx.operations {
		operation := &tx.operations[index]
		arguments := map[string]string{}
		switch operation.Kind {
		case EditOperationReplaceSemanticValue:
			arguments["value_code_units"] = intString(len(operation.Value.units))
		case EditOperationReplaceLiteralValue:
			arguments["literal_bytes"] = intString(len(operation.Literal))
		case EditOperationInsertProperty:
			arguments["key_code_units"] = intString(len(operation.Key.units))
			arguments["value_code_units"] = intString(len(operation.Value.units))
			arguments["placement"] = placementName(operation.Placement)
		case EditOperationRemoveProperty:
			// No summary arguments.
		case EditOperationRenameProperty:
			arguments["key_code_units"] = intString(len(operation.Value.units))
		}
		summary, err := protocol.NewEditOperationSummary(
			protocol.NewFormatOperationId(operationID(operation), 1), arguments)
		if err != nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func operationID(operation *EditOperation) string {
	switch operation.Kind {
	case EditOperationReplaceSemanticValue:
		return "java-properties.edit.replace-semantic-value"
	case EditOperationReplaceLiteralValue:
		return "java-properties.edit.replace-literal-value"
	case EditOperationInsertProperty:
		return "java-properties.edit.insert-property"
	case EditOperationRemoveProperty:
		return "java-properties.edit.remove-property"
	case EditOperationRenameProperty:
		return "java-properties.edit.rename-property"
	}
	return "java-properties.edit.replace-semantic-value"
}

func placementName(placement AssociationPlacement) string {
	switch placement.Kind() {
	case document.PlacementStart:
		return "start"
	case document.PlacementEnd:
		return "end"
	case document.PlacementBefore:
		return "before"
	case document.PlacementAfter:
		return "after"
	}
	return "start"
}

// applyPrepared splices the ordered replacements into the base bytes
// (edit.rs).
func (d *Document) applyPrepared(prepared []preparedEdit) ([]byte, *EditFailure) {
	source := d.source.Bytes()
	targetLen := len(source)
	for _, edit := range prepared {
		targetLen = targetLen - edit.oldSpan.Len() + len(edit.replacement)
		if targetLen > d.parseLimits.Common.MaxSourceBytes {
			return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "target-bytes"}
		}
	}
	output := make([]byte, 0, targetLen)
	cursor := 0
	for _, edit := range prepared {
		output = append(output, source[cursor:edit.oldSpan.StartByte()]...)
		output = append(output, edit.replacement...)
		cursor = edit.oldSpan.EndByte()
	}
	output = append(output, source[cursor:]...)
	return output, nil
}
