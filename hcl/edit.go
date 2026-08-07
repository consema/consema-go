package hcl

import (
	"math/big"
	"strconv"
	"strings"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the typed HCL edit operations and the atomic
// commit/dry-run pipeline (RFC 0014 §10; RFC 0016 §5.3). An edit replaces
// only the bytes its operations own; every other byte is covered by the
// untouched-byte proof, and a failure never modifies the base document.
//
// Both profiles publish the same snapshot-bound operations, typed per
// profile: `hcl.native@1` publishes all six structural operations and
// `hcl.tfvars@1` publishes the four attribute operations only (RFC 0014
// §5, §10).

// BodyPathStep is one root-relative body path step (RFC 0014 §4.2, §10).
//
// A step selects one block occurrence by exact type, exact label sequence,
// and 0-based source position among the blocks with the same type and
// labels; the selected block's nested body is the next level.
type BodyPathStep struct {
	blockType  string
	labels     []string
	occurrence int
}

// NewBodyPathStep creates one block step.
func NewBodyPathStep(blockType string, labels []string, occurrence int) BodyPathStep {
	return BodyPathStep{blockType: blockType, labels: labels, occurrence: occurrence}
}

// BlockType returns the exact block type of the step.
func (s BodyPathStep) BlockType() string { return s.blockType }

// Labels returns the exact label sequence of the step. The returned slice
// is a copy.
func (s BodyPathStep) Labels() []string { return append([]string(nil), s.labels...) }

// Occurrence returns the 0-based source position among the blocks with the
// same type and labels.
func (s BodyPathStep) Occurrence() int { return s.occurrence }

// BodyPath is a root-relative path to one body (RFC 0014 §10).
//
// The empty path denotes the root body. A step that meets an attribute
// instead of a block is a role failure; a step that does not exist in the
// current document state is a missing-target failure.
type BodyPath struct {
	steps []BodyPathStep
}

// BodyPathRoot is the root body path.
func BodyPathRoot() BodyPath { return BodyPath{} }

// NewBodyPath creates a path from ordered steps.
func NewBodyPath(steps []BodyPathStep) BodyPath {
	return BodyPath{steps: steps}
}

// Segments returns the ordered path steps. The returned slice is a copy.
func (p BodyPath) Segments() []BodyPathStep { return append([]BodyPathStep(nil), p.steps...) }

// Child creates a child path without modifying this path.
func (p BodyPath) Child(step BodyPathStep) BodyPath {
	steps := append([]BodyPathStep(nil), p.steps...)
	steps = append(steps, step)
	return BodyPath{steps: steps}
}

// Equal reports path equality.
func (p BodyPath) Equal(other BodyPath) bool {
	if len(p.steps) != len(other.steps) {
		return false
	}
	for i := range p.steps {
		left := p.steps[i]
		right := other.steps[i]
		if left.blockType != right.blockType || left.occurrence != right.occurrence ||
			len(left.labels) != len(right.labels) {
			return false
		}
		for j := range left.labels {
			if left.labels[j] != right.labels[j] {
				return false
			}
		}
	}
	return true
}

// EditNodeRef is one exact body item address (RFC 0014 §10).
//
// An attribute is addressed by owning body and name — unique per body in a
// Complete document (RFC 0014 §3). A block is addressed by owning body,
// type, exact label sequence, and occurrence, because blocks with the same
// type and labels may repeat (RFC 0014 §6).
type EditNodeRef struct {
	// Body is the owning body path.
	Body BodyPath
	// Name is the exact attribute name (Attribute).
	Name string
	// BlockType is the exact block type (Block).
	BlockType string
	// Labels is the exact label sequence (Block).
	Labels []string
	// Occurrence is the 0-based source position among the
	// equal-type-and-labels blocks (Block).
	Occurrence int
	// IsBlock distinguishes the attribute and block forms.
	IsBlock bool
}

// BodyPathOf returns the owning body path of the addressed item.
func (n EditNodeRef) BodyPathOf() BodyPath { return n.Body }

// BodyPlacement is the attribute insertion placement inside one body (RFC
// 0014 §10).
type BodyPlacement uint8

// The three frozen placements.
const (
	// BodyPlacementFirst inserts before the body's first item (or at the
	// body content start of an empty body).
	BodyPlacementFirst BodyPlacement = iota
	// BodyPlacementLast inserts after the body's last item's terminating
	// line (or at the body content end of an empty body).
	BodyPlacementLast
	// BodyPlacementAfter inserts immediately after the item addressed by
	// one exact EditNodeRef of the same body.
	BodyPlacementAfter
)

// EditValue is one typed literal-complete HCL value supplied to an edit
// (RFC 0014 §10).
//
// Values are typed native facts, never raw markup and never unevaluated
// expression text. Numbers are canonical-decimal facts: an integer renders
// its exact decimal spelling, a real renders the canonical decimal of its
// shortest-round-trip spelling (non-finite reals are refused with
// `hcl.edit.unrepresentable@1`). Strings render with minimal deterministic
// escapes. Tuples and objects are ordered with duplicate object keys
// preserved, never collapsed (RFC 0014 §4.6). The Expression variant
// exists so that derived-expression insertion is refused explicitly with
// `hcl.edit.unrepresentable@1`; no commit ever renders it (RFC 0014 §10,
// §14).
type EditValue struct {
	// Tag is the closed value category.
	Tag EditValueTag
	// Integer is the signed integer payload.
	Integer int64
	// Real is the IEEE 754 real payload; must be finite.
	Real float64
	// Text is the exact string content.
	Text string
	// Boolean is the boolean payload.
	Boolean bool
	// Elements are the ordered tuple elements.
	Elements []EditValue
	// Entries are the ordered object entries; duplicate keys are preserved.
	Entries []EditObjectEntry
	// Kind is the expression kind spelling of an Expression value.
	Kind string
}

// EditValueTag is the closed edit value category.
type EditValueTag uint8

// The eight frozen value categories.
const (
	// EditValueInteger is a signed 64-bit integer.
	EditValueInteger EditValueTag = iota
	// EditValueReal is a finite IEEE 754 real.
	EditValueReal
	// EditValueString is exact string content.
	EditValueString
	// EditValueBoolean is a boolean.
	EditValueBoolean
	// EditValueNull is null.
	EditValueNull
	// EditValueTuple is an ordered tuple of literal values.
	EditValueTuple
	// EditValueObject is an ordered object of literal entries.
	EditValueObject
	// EditValueExpression is a derived expression: refused by every commit
	// with `hcl.edit.unrepresentable@1`.
	EditValueExpression
)

// KindName returns the stable value-kind spelling for summaries.
func (v *EditValue) KindName() string {
	switch v.Tag {
	case EditValueInteger:
		return "integer"
	case EditValueReal:
		return "real"
	case EditValueString:
		return "string"
	case EditValueBoolean:
		return "boolean"
	case EditValueNull:
		return "null"
	case EditValueTuple:
		return "tuple"
	case EditValueObject:
		return "object"
	case EditValueExpression:
		return "expression"
	}
	return "integer"
}

// EditValueIntegerV creates an integer value.
func EditValueIntegerV(value int64) EditValue {
	return EditValue{Tag: EditValueInteger, Integer: value}
}

// EditValueRealV creates a real value; it must be finite.
func EditValueRealV(value float64) EditValue {
	return EditValue{Tag: EditValueReal, Real: value}
}

// EditValueStringV creates a string value.
func EditValueStringV(text string) EditValue {
	return EditValue{Tag: EditValueString, Text: text}
}

// EditValueBooleanV creates a boolean value.
func EditValueBooleanV(value bool) EditValue {
	return EditValue{Tag: EditValueBoolean, Boolean: value}
}

// EditValueNullV creates a null value.
func EditValueNullV() EditValue { return EditValue{Tag: EditValueNull} }

// EditValueTupleV creates an ordered tuple value.
func EditValueTupleV(elements []EditValue) EditValue {
	return EditValue{Tag: EditValueTuple, Elements: elements}
}

// EditValueObjectV creates an ordered object value.
func EditValueObjectV(entries []EditObjectEntry) EditValue {
	return EditValue{Tag: EditValueObject, Entries: entries}
}

// EditValueExpressionV creates a derived-expression value that every
// commit refuses.
func EditValueExpressionV(kind, text string) EditValue {
	return EditValue{Tag: EditValueExpression, Kind: kind, Text: text}
}

// EditObjectEntry is one ordered object entry of an edit value.
type EditObjectEntry struct {
	// Key is the literal key.
	Key EditKey
	// Value is the literal value.
	Value EditValue
}

// EditKey is one object-constructor literal key (RFC 0014 §4.6, §8.1).
//
// The bare forms are an identifier, a number literal, and a quoted literal
// string; the parenthesized-expression key form is not part of the edit
// surface. An identifier key spelled `for` is refused, because the
// for-expression interpretation has priority in an object constructor
// (RFC 0014 §4.6).
type EditKey struct {
	// Tag is the closed key category.
	Tag EditKeyTag
	// Text is the identifier or string text.
	Text string
	// Number is the number-literal key.
	Number int64
}

// EditKeyTag is the closed edit key category.
type EditKeyTag uint8

// The three frozen key categories.
const (
	// EditKeyIdentifier is a bare identifier key.
	EditKeyIdentifier EditKeyTag = iota
	// EditKeyNumber is a bare number-literal key.
	EditKeyNumber
	// EditKeyString is a bare quoted-literal-string key.
	EditKeyString
)

// EditKeyIdentifierV creates a bare identifier key.
func EditKeyIdentifierV(text string) EditKey { return EditKey{Tag: EditKeyIdentifier, Text: text} }

// EditKeyNumberV creates a bare number-literal key.
func EditKeyNumberV(value int64) EditKey { return EditKey{Tag: EditKeyNumber, Number: value} }

// EditKeyStringV creates a bare quoted-literal-string key.
func EditKeyStringV(text string) EditKey { return EditKey{Tag: EditKeyString, Text: text} }

// EditOperationKind is the closed HCL edit operation category (RFC 0014
// §10).
type EditOperationKind uint8

// The six frozen operation categories.
const (
	// EditOperationSetAttributeValue replaces the target attribute's
	// expression span with the canonical rendering of one typed
	// literal-complete value.
	EditOperationSetAttributeValue EditOperationKind = iota
	// EditOperationInsertAttribute adds one attribute to a target body at
	// a position anchor.
	EditOperationInsertAttribute
	// EditOperationRemoveAttribute removes one attribute's name, equals,
	// expression, and owned trivia.
	EditOperationRemoveAttribute
	// EditOperationRenameAttribute changes one attribute name, preserving
	// its expression.
	EditOperationRenameAttribute
	// EditOperationInsertBlock adds one block (type, labels, and a nested
	// body whose attributes are typed literal-complete values) to a target
	// body.
	EditOperationInsertBlock
	// EditOperationRemoveBlock removes one block by exact type, labels,
	// and occurrence.
	EditOperationRemoveBlock
)

// EditOperation is one snapshot-bound HCL structural operation (RFC 0014
// §10).
//
// Every body path, name, and occurrence refers to the document state as of
// the operation's own application: operations of one transaction apply
// sequentially, so a later operation may target content an earlier
// insertion created.
type EditOperation struct {
	// Kind is the closed operation category.
	Kind EditOperationKind
	// Body is the owning body of the target (all operations).
	Body BodyPath
	// Attribute is the exact target attribute name (Set, Remove, Rename).
	Attribute string
	// Value is the new typed literal value (Set, Insert attribute).
	Value EditValue
	// Name is the new attribute name (Insert, Rename).
	Name string
	// Placement is the explicit placement inside the body (Insert).
	Placement BodyPlacement
	// Anchor is the exact same-body item anchor of PlacementAfter.
	Anchor *EditNodeRef
	// BlockType is the new or target block type (InsertBlock, RemoveBlock).
	BlockType string
	// Labels is the new or target label sequence (InsertBlock, RemoveBlock).
	Labels []string
	// Attributes are the ordered nested attributes of the new block
	// (InsertBlock).
	Attributes []EditBlockAttribute
	// Occurrence is the 0-based source position among the equal-type-and-
	// labels blocks (RemoveBlock).
	Occurrence int
}

// EditBlockAttribute is one ordered nested attribute of an inserted block.
type EditBlockAttribute struct {
	// Name is the attribute name.
	Name string
	// Value is the typed literal value.
	Value EditValue
}

// EditTransaction is the immutable snapshot-bound transaction; every
// operation resolves against one base snapshot (RFC 0014 §10).
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

// EditTransactionBuilder is a builder that is not a committed edit (RFC
// 0014 §10).
type EditTransactionBuilder struct {
	base       document.SnapshotIdentity
	operations []EditOperation
}

// NewEditTransactionBuilder binds a new transaction to one immutable base
// document.
func NewEditTransactionBuilder(doc *Document) *EditTransactionBuilder {
	return &EditTransactionBuilder{base: doc.SnapshotIdentity()}
}

// SetAttributeValue adds one attribute value replacement.
func (b *EditTransactionBuilder) SetAttributeValue(body BodyPath, attribute string,
	value EditValue) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationSetAttributeValue, Body: body, Attribute: attribute, Value: value,
	})
	return b
}

// InsertAttribute adds one attribute insertion.
func (b *EditTransactionBuilder) InsertAttribute(body BodyPath, name string,
	value EditValue, placement BodyPlacement) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationInsertAttribute, Body: body, Name: name, Value: value, Placement: placement,
	})
	return b
}

// InsertAttributeAfter adds one attribute insertion after an exact same-
// body item.
func (b *EditTransactionBuilder) InsertAttributeAfter(body BodyPath, name string,
	value EditValue, anchor EditNodeRef) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationInsertAttribute, Body: body, Name: name, Value: value,
		Placement: BodyPlacementAfter, Anchor: &anchor,
	})
	return b
}

// RemoveAttribute adds one attribute removal.
func (b *EditTransactionBuilder) RemoveAttribute(body BodyPath, attribute string) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRemoveAttribute, Body: body, Attribute: attribute,
	})
	return b
}

// RenameAttribute adds one attribute rename.
func (b *EditTransactionBuilder) RenameAttribute(body BodyPath, attribute,
	name string) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRenameAttribute, Body: body, Attribute: attribute, Name: name,
	})
	return b
}

// InsertBlock adds one block insertion.
func (b *EditTransactionBuilder) InsertBlock(body BodyPath, blockType string, labels []string,
	attributes []EditBlockAttribute, placement BodyPlacement) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationInsertBlock, Body: body, BlockType: blockType, Labels: labels,
		Attributes: attributes, Placement: placement,
	})
	return b
}

// RemoveBlock adds one block removal.
func (b *EditTransactionBuilder) RemoveBlock(body BodyPath, blockType string, labels []string,
	occurrence int) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRemoveBlock, Body: body, BlockType: blockType, Labels: labels,
		Occurrence: occurrence,
	})
	return b
}

// Build completes the immutable request; target validation happens
// atomically at commit (RFC 0014 §10).
func (b *EditTransactionBuilder) Build() *EditTransaction {
	return &EditTransaction{
		base:       b.base,
		operations: append([]EditOperation(nil), b.operations...),
	}
}

// EditCommit is the atomic edit success (RFC 0014 §10).
type EditCommit struct {
	// Document is the new immutable document.
	Document *Document
	// ChangeSet carries the complete old-to-new change facts.
	ChangeSet document.ChangeSet
	// SourcePatch carries the portable exact raw-byte application facts.
	SourcePatch *document.SourcePatch
	// UntouchedProof carries verifiable evidence for every byte outside
	// the replacement set.
	UntouchedProof *document.UntouchedByteProof
}

// EditFailureKind is the stable edit validation or commit failure
// category (RFC 0014 §10).
type EditFailureKind uint8

// The closed edit failure categories.
const (
	// EditFailureWrongSnapshot: the transaction or target belongs to
	// another snapshot.
	EditFailureWrongSnapshot EditFailureKind = iota
	// EditFailureWrongRole: a body path step meets an attribute instead of
	// a block.
	EditFailureWrongRole
	// EditFailureIncompleteTarget: the base document is not Complete, or a
	// target, occurrence, or placement anchor does not exist in the
	// current document state.
	EditFailureIncompleteTarget
	// EditFailureDuplicateAttribute: an insertion or rename would create a
	// second attribute with the same name in one body.
	EditFailureDuplicateAttribute
	// EditFailureBlockInTfvars: a block operation was declared under the
	// `hcl.tfvars@1` profile.
	EditFailureBlockInTfvars
	// EditFailureConflictingEdits: two operations map to the same exact
	// base position.
	EditFailureConflictingEdits
	// EditFailureOverlappingOwnership: one operation's source span
	// contains bytes an earlier operation of the same transaction
	// replaced.
	EditFailureOverlappingOwnership
	// EditFailureUnrepresentableValue: a typed value or name cannot be
	// expressed as literal-complete HCL; the payload names the blocking
	// native fact.
	EditFailureUnrepresentableValue
	// EditFailureResourceLimit: a configured edit or output bound was
	// exceeded.
	EditFailureResourceLimit
	// EditFailureNewDocumentFormationFailed: the replacement document could
	// not be formed under the original limits, or the reparsed target does
	// not carry the promised semantics.
	EditFailureNewDocumentFormationFailed
)

// EditFailure is the typed edit failure. It implements error and the
// RFC 0016 §6 Code() contract with the frozen registered codes (RFC 0014
// §10).
type EditFailure struct {
	// Kind identifies the failure.
	Kind EditFailureKind
	// Fact is the blocking native fact name of UnrepresentableValue.
	Fact string
	// LimitName is the stable limit name of a ResourceLimit.
	LimitName string
}

// Error implements error.
func (e *EditFailure) Error() string {
	switch e.Kind {
	case EditFailureWrongSnapshot:
		return "hcl: edit transaction or target belongs to another snapshot"
	case EditFailureWrongRole:
		return "hcl: edit target has the wrong structural role"
	case EditFailureIncompleteTarget:
		return "hcl: edit target or placement anchor was not found"
	case EditFailureDuplicateAttribute:
		return "hcl: edit would create a duplicate attribute"
	case EditFailureBlockInTfvars:
		return "hcl: block operations are not published by the tfvars profile"
	case EditFailureConflictingEdits:
		return "hcl: edit operations have conflicting source ownership"
	case EditFailureOverlappingOwnership:
		return "hcl: prepared source ownership intervals overlap"
	case EditFailureUnrepresentableValue:
		return "hcl: edit value cannot be expressed as literal-complete HCL: " + e.Fact
	case EditFailureResourceLimit:
		return "hcl: edit limit " + e.LimitName + " reached"
	case EditFailureNewDocumentFormationFailed:
		return "hcl: replacement document could not be formed"
	}
	return "hcl: edit failure"
}

// Name returns the stable failure name (the Rust variant spelling used by
// the conformance vectors).
func (e *EditFailure) Name() string {
	switch e.Kind {
	case EditFailureWrongSnapshot:
		return "WrongSnapshot"
	case EditFailureWrongRole:
		return "WrongRole"
	case EditFailureIncompleteTarget:
		return "IncompleteTarget"
	case EditFailureDuplicateAttribute:
		return "DuplicateAttribute"
	case EditFailureBlockInTfvars:
		return "BlockInTfvars"
	case EditFailureConflictingEdits:
		return "ConflictingEdits"
	case EditFailureOverlappingOwnership:
		return "OverlappingOwnership"
	case EditFailureUnrepresentableValue:
		return "UnrepresentableValue"
	case EditFailureResourceLimit:
		return "ResourceLimit"
	case EditFailureNewDocumentFormationFailed:
		return "NewDocumentFormationFailed"
	}
	return "EditFailure"
}

// Code returns the frozen registered code for the failure (RFC 0014 §10).
func (e *EditFailure) Code() string {
	switch e.Kind {
	case EditFailureWrongSnapshot:
		return "core.edit.wrong-snapshot@1"
	case EditFailureWrongRole:
		return "core.edit.wrong-role@1"
	case EditFailureIncompleteTarget:
		return "core.edit.incomplete-target@1"
	case EditFailureDuplicateAttribute:
		return "hcl.edit.duplicate-attribute@1"
	case EditFailureBlockInTfvars:
		return "hcl.edit.block-in-tfvars@1"
	case EditFailureConflictingEdits, EditFailureOverlappingOwnership:
		return "core.edit.conflicting-edits@1"
	case EditFailureUnrepresentableValue:
		return "hcl.edit.unrepresentable@1"
	case EditFailureResourceLimit:
		return "core.edit.resource-limit@1"
	case EditFailureNewDocumentFormationFailed:
		return "core.edit.formation-failed@1"
	}
	return "core.edit.conflicting-edits@1"
}

// Commit atomically commits structural operations (RFC 0014 §10). On
// failure the base document remains unchanged.
func (d *Document) Commit(tx *EditTransaction) (*EditCommit, *EditFailure) {
	return d.commitImpl(tx, d.limits)
}

// DryRun fully validates and plans an edit without returning a new
// Document (RFC 0014 §10).
func (d *Document) DryRun(tx *EditTransaction, sourceID string) (*document.EditPlan, *EditFailure) {
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

// commitImpl runs the sequential per-operation pipeline (RFC 0014 §10).
func (d *Document) commitImpl(tx *EditTransaction,
	limits HclParseLimits) (*EditCommit, *EditFailure) {
	if tx.base != d.SnapshotIdentity() {
		return nil, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if d.status != document.FormationStatusComplete {
		return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	if len(tx.operations) > limits.MaxReportEvents {
		return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "report-events"}
	}
	profile := d.selector()
	bytes := d.source.Bytes()
	var edits []appliedEdit
	current := d
	for _, operation := range tx.operations {
		splices, verify, failure := prepareOperation(current, &operation)
		if failure != nil {
			return nil, failure
		}
		if failure := applyStep(&edits, &bytes, limits, splices); failure != nil {
			return nil, failure
		}
		formed, formationFailure := Parse(contextBackground(), bytes, profile,
			HclEncodingSelectionProfileDefault(), limits)
		if formationFailure != nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		if formed.status != document.FormationStatusComplete {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		if failure := verifyOperation(formed, &operation, verify); failure != nil {
			return nil, failure
		}
		current = formed
	}
	finalDocument := current
	if len(tx.operations) == 0 {
		formed, formationFailure := Parse(contextBackground(), bytes, profile,
			HclEncodingSelectionProfileDefault(), limits)
		if formationFailure != nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		if formed.status != document.FormationStatusComplete {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		finalDocument = formed
	}
	return buildCommit(d, tx, finalDocument, edits)
}

// verifyData is the per-operation data the post-application verification
// needs beyond the operation itself.
type verifyData struct {
	// renameKind is the pre-operation expression kind a rename must
	// preserve; nil otherwise.
	renameKind *HclExpressionKind
}

// appliedEdit is one applied raw-byte splice, recorded for base-coordinate
// translation.
type appliedEdit struct {
	// preStart is the start of the replaced span in the state immediately
	// before this splice was applied.
	preStart int
	// preLen is the length of the replaced span in that pre-state.
	preLen int
	// replacement is the replacement bytes.
	replacement []byte
}

// unmapIn maps one position from the final state back to the base
// snapshot through the applied edits in reverse application order; a
// position inside an earlier replacement is an ownership overlap.
func unmapIn(edits []appliedEdit, pos int) (int, *EditFailure) {
	for index := len(edits) - 1; index >= 0; index-- {
		edit := &edits[index]
		if pos <= edit.preStart {
			continue
		}
		if pos < edit.preStart+len(edit.replacement) {
			baseStart, failure := unmapIn(edits[:index], edit.preStart)
			if failure != nil {
				return 0, failure
			}
			return baseStart + (pos - edit.preStart), nil
		}
		pos = pos - len(edit.replacement) + edit.preLen
	}
	return pos, nil
}

// mapIn maps one position from one pre-state to the final state through
// the applied edits in application order.
func mapIn(edits []appliedEdit, pos int) (int, *EditFailure) {
	for i := range edits {
		edit := &edits[i]
		if pos <= edit.preStart {
			continue
		}
		if pos < edit.preStart+edit.preLen {
			return 0, &EditFailure{Kind: EditFailureOverlappingOwnership}
		}
		pos = pos + len(edit.replacement) - edit.preLen
	}
	return pos, nil
}

// recordEdit records one splice and rejects two insertions that map to the
// same base position (a duplicate target). An operation whose span lies
// inside a replacement an earlier operation of this transaction wrote
// folds into that replacement: the sequential result is one combined
// splice.
func recordEdit(edits *[]appliedEdit, preStart, preLen int, replacement []byte) *EditFailure {
	if preLen == 0 && len(replacement) == 0 {
		return nil
	}
	for index := len(*edits) - 1; index >= 0; index-- {
		edit := &(*edits)[index]
		regionStart, failure := mapIn((*edits)[index+1:], edit.preStart)
		if failure != nil {
			return failure
		}
		regionEnd := regionStart + len(edit.replacement)
		// A zero-width insertion exactly at the region end is not operation
		// content of the region's owner and is recorded on its own.
		if preStart >= regionStart && preStart+preLen <= regionEnd &&
			!(preLen == 0 && preStart == regionEnd) {
			offset := preStart - regionStart
			merged := make([]byte, 0, len(edit.replacement)+len(replacement))
			merged = append(merged, edit.replacement[:offset]...)
			merged = append(merged, replacement...)
			merged = append(merged, edit.replacement[offset+preLen:]...)
			delta := len(merged) - len(edit.replacement)
			targetStart := edit.preStart
			// Only the later records whose own spans lie at or after the
			// fold target's span move in the folded coordinate system.
			for later := index + 1; later < len(*edits); later++ {
				if (*edits)[later].preStart > targetStart {
					shifted, failure := shiftPosition((*edits)[later].preStart, delta)
					if failure != nil {
						return failure
					}
					(*edits)[later].preStart = shifted
				}
			}
			edit.replacement = merged
			return nil
		}
	}
	baseStart, failure := unmapIn(*edits, preStart)
	if failure != nil {
		return failure
	}
	baseEnd, failure := unmapIn(*edits, preStart+preLen)
	if failure != nil {
		return failure
	}
	for index := range *edits {
		previous := &(*edits)[index]
		if previous.preLen == 0 && baseStart == baseEnd {
			previousBase, failure := unmapIn((*edits)[:index], previous.preStart)
			if failure != nil {
				return failure
			}
			if previousBase == baseStart {
				return &EditFailure{Kind: EditFailureConflictingEdits}
			}
		}
	}
	*edits = append(*edits, appliedEdit{
		preStart:    preStart,
		preLen:      preLen,
		replacement: replacement,
	})
	return nil
}

// shiftPosition shifts one base position by the accumulated length delta
// of earlier splices.
func shiftPosition(base, delta int) (int, *EditFailure) {
	if delta >= 0 {
		if base > int(^uint(0)>>1)-delta {
			return 0, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "target-bytes"}
		}
		return base + delta, nil
	}
	magnitude := -delta
	if base < magnitude {
		return 0, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "target-bytes"}
	}
	return base - magnitude, nil
}

// applyStep applies one step's splices: validates the target length
// against the source bound first (hard gate 4), records every splice
// against the base coordinates, then builds the new bytes in one pass.
func applyStep(edits *[]appliedEdit, bytes *[]byte, limits HclParseLimits,
	splices []appliedEdit) *EditFailure {
	targetLen := len(*bytes)
	for _, splice := range splices {
		targetLen = targetLen - splice.preLen + len(splice.replacement)
		if targetLen < 0 {
			return &EditFailure{Kind: EditFailureResourceLimit, LimitName: "target-bytes"}
		}
	}
	if targetLen > limits.Common.MaxSourceBytes {
		return &EditFailure{Kind: EditFailureResourceLimit, LimitName: "target-bytes"}
	}
	for _, splice := range splices {
		if failure := recordEdit(edits, splice.preStart, splice.preLen, splice.replacement); failure != nil {
			return failure
		}
	}
	var err error
	*bytes, err = applySplices(*bytes, splices)
	if err != nil {
		return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return nil
}

// applySplices builds the new bytes by applying the splices sequentially
// against a working buffer; every splice's pre-span is expressed in its
// own pre-state.
func applySplices(bytes []byte, splices []appliedEdit) ([]byte, error) {
	working := append([]byte(nil), bytes...)
	for _, splice := range splices {
		end := splice.preStart + splice.preLen
		if end > len(working) {
			return nil, errEditSplice
		}
		working = append(working[:splice.preStart],
			append(append([]byte(nil), splice.replacement...), working[end:]...)...)
	}
	return working, nil
}

// errEditSplice is the internal splice failure sentinel.
var errEditSplice = &editSpliceError{}

type editSpliceError struct{}

func (e *editSpliceError) Error() string { return "hcl: splice out of bounds" }

// splice builds one zero-width or full replacement splice.
func splice(preStart, preLen int, replacement []byte) appliedEdit {
	return appliedEdit{preStart: preStart, preLen: preLen, replacement: replacement}
}

// pieceIndex is the lossless piece facts of one formed document, indexed
// for boundary walks.
type pieceIndex struct {
	starts []int
	ends   []int
	kinds  []HclSyntaxKind
}

func newPieceIndex(doc *Document) *pieceIndex {
	pieces := doc.index.Pieces()
	kinds := doc.syntaxKinds
	index := &pieceIndex{
		starts: make([]int, 0, len(pieces)),
		ends:   make([]int, 0, len(pieces)),
		kinds:  make([]HclSyntaxKind, 0, len(pieces)),
	}
	for i, piece := range pieces {
		index.starts = append(index.starts, piece.Span().StartByte())
		index.ends = append(index.ends, piece.Span().EndByte())
		index.kinds = append(index.kinds, kinds[i])
	}
	return index
}

// pieceStartingAt is the index of the piece starting exactly at pos; -1
// at end of file.
func (i *pieceIndex) pieceStartingAt(pos int) int {
	low, high := 0, len(i.starts)
	for low < high {
		mid := (low + high) / 2
		if i.starts[mid] < pos {
			low = mid + 1
		} else {
			high = mid
		}
	}
	if low < len(i.starts) && i.starts[low] == pos {
		return low
	}
	return -1
}

// pieceEndingAt is the index of the piece ending exactly at pos; -1 at
// position zero.
func (i *pieceIndex) pieceEndingAt(pos int) int {
	if pos == 0 {
		return -1
	}
	low, high := 0, len(i.starts)
	for low < high {
		mid := (low + high) / 2
		if i.starts[mid] < pos {
			low = mid + 1
		} else {
			high = mid
		}
	}
	if low == 0 {
		return -1
	}
	previous := low - 1
	if i.ends[previous] == pos {
		return previous
	}
	return -1
}

// resolveBody resolves one body path against one native document; the
// empty path is the root body. It returns the target body and, for a
// nested target, the owning block of the last path step.
func resolveBody(document *HclDocument, path *BodyPath) (*HclBody, *HclBlock, *EditFailure) {
	body := document.body
	var parent *HclBlock
	for _, step := range path.steps {
		block := findBlock(body, step.blockType, step.labels, step.occurrence)
		if block == nil {
			for i := range body.items {
				if attribute := body.items[i].AsAttribute(); attribute != nil && attribute.name == step.blockType {
					return nil, nil, &EditFailure{Kind: EditFailureWrongRole}
				}
			}
			return nil, nil, &EditFailure{Kind: EditFailureIncompleteTarget}
		}
		parent = block
		body = block.body
	}
	return body, parent, nil
}

// findAttribute locates one attribute occurrence by exact name;
// attributes are unique per body in a Complete document (RFC 0014 §3).
func findAttribute(body *HclBody, name string) *HclAttribute {
	for i := range body.items {
		if attribute := body.items[i].AsAttribute(); attribute != nil && attribute.name == name {
			return attribute
		}
	}
	return nil
}

// findBlock locates one block occurrence by exact type, label sequence,
// and occurrence.
func findBlock(body *HclBody, blockType string, labels []string, occurrence int) *HclBlock {
	seen := 0
	for i := range body.items {
		block := body.items[i].AsBlock()
		if block == nil || block.blockType != blockType || !labelSequenceEqual(block.labels, labels) {
			continue
		}
		if seen == occurrence {
			return block
		}
		seen++
	}
	return nil
}

func labelSequenceEqual(labels []HclBlockLabel, expected []string) bool {
	if len(labels) != len(expected) {
		return false
	}
	for i := range labels {
		if labels[i].text != expected[i] {
			return false
		}
	}
	return true
}

// resolveNode resolves one exact item address against one native document.
func resolveNode(document *HclDocument, nodeRef *EditNodeRef) (*HclBodyItem, *EditFailure) {
	body, _, failure := resolveBody(document, &nodeRef.Body)
	if failure != nil {
		return nil, failure
	}
	if !nodeRef.IsBlock {
		for i := range body.items {
			if attribute := body.items[i].AsAttribute(); attribute != nil && attribute.name == nodeRef.Name {
				return &body.items[i], nil
			}
		}
		return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	for i := range body.items {
		block := body.items[i].AsBlock()
		if block == nil || block.blockType != nodeRef.BlockType ||
			!labelSequenceEqual(block.labels, nodeRef.Labels) {
			continue
		}
		if nodeRef.Occurrence == 0 {
			return &body.items[i], nil
		}
	}
	return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
}

// itemSpanStart is the start byte of one item's own span (the name or
// block-type identifier).
func itemSpanStart(item *HclBodyItem) int {
	if attribute := item.AsAttribute(); attribute != nil {
		return attribute.nameSpan.StartByte()
	}
	return item.AsBlock().span.StartByte()
}

// itemSpanEnd is the end byte of one item's own span (the expression end
// or the closing brace).
func itemSpanEnd(item *HclBodyItem) int {
	if attribute := item.AsAttribute(); attribute != nil {
		return attribute.expression.span.EndByte()
	}
	return item.AsBlock().span.EndByte()
}

// itemLineEnd is the end of the line that terminates the item ending at
// `from`: the end of the first LineBreak piece at or after `from` that is
// not inside an inline comment, or `from` itself when the item is
// end-of-file-terminated. Everything between `from` and the returned
// position is owned trivia.
func itemLineEnd(index *pieceIndex, from int) (int, *EditFailure) {
	pos := from
	for {
		piece := index.pieceStartingAt(pos)
		if piece < 0 {
			return pos, nil
		}
		switch index.kinds[piece] {
		case HclSyntaxKindWhitespace, HclSyntaxKindLineComment, HclSyntaxKindInlineComment:
			pos = index.ends[piece]
		case HclSyntaxKindLineBreak:
			return index.ends[piece], nil
		default:
			return 0, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
	}
}

// itemLineStart is the start of the line that begins at itemStart: the
// beginning of the whitespace run that indents the item.
func itemLineStart(index *pieceIndex, itemStart int) int {
	pos := itemStart
	for {
		piece := index.pieceEndingAt(pos)
		if piece < 0 || index.kinds[piece] != HclSyntaxKindWhitespace {
			break
		}
		pos = index.starts[piece]
	}
	return pos
}

// itemIndent is the leading whitespace run of the line that starts an
// item, used as the indentation of inserted markup.
func itemIndent(index *pieceIndex, decoded string, itemStart int) string {
	pos := itemStart
	var indent strings.Builder
	for {
		piece := index.pieceEndingAt(pos)
		if piece < 0 || index.kinds[piece] != HclSyntaxKindWhitespace {
			break
		}
		start := index.starts[piece]
		end := index.ends[piece]
		// Whitespace pieces are space or tab only (RFC 0014 §4.1).
		text := decoded[start:end]
		var run strings.Builder
		run.WriteString(text)
		indent.WriteString(run.String())
		pos = start
	}
	return reverseIndent(indent.String())
}

// reverseIndent reverses one whitespace run (the walk collects pieces
// right-to-left).
func reverseIndent(text string) string {
	bytes := []byte(text)
	for left, right := 0, len(bytes)-1; left < right; left, right = left+1, right-1 {
		bytes[left], bytes[right] = bytes[right], bytes[left]
	}
	return string(bytes)
}

// blockBracePositions are the byte positions of one block's own braces:
// the end of its opening `{` and the start of its closing `}`.
func blockBracePositions(index *pieceIndex, blockStart, blockEnd int) (int, int, *EditFailure) {
	openEnd := -1
	closeStart := -1
	for position, start := range index.starts {
		if start >= blockEnd {
			break
		}
		if start < blockStart {
			continue
		}
		switch index.kinds[position] {
		case HclSyntaxKindBraceOpen:
			if openEnd < 0 {
				openEnd = index.ends[position]
			}
		case HclSyntaxKindBraceClose:
			closeStart = start
		}
	}
	if openEnd < 0 || closeStart < 0 {
		return 0, 0, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return openEnd, closeStart, nil
}

// emptyBodyPoint is the insertion point facts of an empty target body.
func emptyBodyPoint(index *pieceIndex, doc *Document, bodyPath *BodyPath,
	parent *HclBlock) (int, string, *EditFailure) {
	if parent == nil {
		return doc.source.Len(), "", nil
	}
	_, closeStart, failure := blockBracePositions(index, parent.span.StartByte(), parent.span.EndByte())
	if failure != nil {
		return 0, "", failure
	}
	return closeStart, strings.Repeat("  ", len(bodyPath.steps)), nil
}

// insertionPoint computes the insertion point, markup indentation, and
// whether the markup needs a separating leading newline (the anchor item
// is end-of-file terminated) for one insertion placement.
func insertionPoint(index *pieceIndex, doc *Document, bodyPath *BodyPath, body *HclBody,
	parent *HclBlock, placement BodyPlacement, anchor *EditNodeRef) (int, string, bool, *EditFailure) {
	items := body.items
	switch placement {
	case BodyPlacementFirst:
		if len(items) > 0 {
			start := itemSpanStart(&items[0])
			return itemLineStart(index, start), itemIndent(index, doc.DecodedText(), start), false, nil
		}
		point, indent, failure := emptyBodyPoint(index, doc, bodyPath, parent)
		return point, indent, false, failure
	case BodyPlacementLast:
		if len(items) > 0 {
			end := itemSpanEnd(&items[len(items)-1])
			lineEnd, failure := itemLineEnd(index, end)
			if failure != nil {
				return 0, "", false, failure
			}
			eofTerminated := lineEnd == end
			return lineEnd, itemIndent(index, doc.DecodedText(), itemSpanStart(&items[len(items)-1])), eofTerminated, nil
		}
		point, indent, failure := emptyBodyPoint(index, doc, bodyPath, parent)
		return point, indent, false, failure
	case BodyPlacementAfter:
		if anchor == nil || !anchor.Body.Equal(*bodyPath) {
			return 0, "", false, &EditFailure{Kind: EditFailureIncompleteTarget}
		}
		anchorItem, failure := resolveNode(doc.document, anchor)
		if failure != nil {
			return 0, "", false, failure
		}
		end := itemSpanEnd(anchorItem)
		lineEnd, failure := itemLineEnd(index, end)
		if failure != nil {
			return 0, "", false, failure
		}
		eofTerminated := lineEnd == end
		return lineEnd, itemIndent(index, doc.DecodedText(), itemSpanStart(anchorItem)), eofTerminated, nil
	}
	return 0, "", false, &EditFailure{Kind: EditFailureIncompleteTarget}
}

// prepareOperation resolves one operation against the current state and
// computes its splices in the current state's coordinates, plus the data
// its post-application verification needs.
func prepareOperation(current *Document, operation *EditOperation) ([]appliedEdit, verifyData, *EditFailure) {
	index := newPieceIndex(current)
	document := current.document
	decoded := current.DecodedText()
	switch operation.Kind {
	case EditOperationSetAttributeValue:
		if failure := checkValue(operation.Value); failure != nil {
			return nil, verifyData{}, failure
		}
		targetBody, _, failure := resolveBody(document, &operation.Body)
		if failure != nil {
			return nil, verifyData{}, failure
		}
		attributeRef := findAttribute(targetBody, operation.Attribute)
		if attributeRef == nil {
			return nil, verifyData{}, &EditFailure{Kind: EditFailureIncompleteTarget}
		}
		indent := itemIndent(index, decoded, attributeRef.nameSpan.StartByte())
		rendered, failure := renderValue(operation.Value, indent)
		if failure != nil {
			return nil, verifyData{}, failure
		}
		start := attributeRef.expression.span.StartByte()
		end := attributeRef.expression.span.EndByte()
		return []appliedEdit{splice(start, end-start, []byte(rendered))}, verifyData{}, nil
	case EditOperationInsertAttribute:
		targetBody, parent, failure := resolveBody(document, &operation.Body)
		if failure != nil {
			return nil, verifyData{}, failure
		}
		if !isPlainIdentifier(operation.Name) {
			return nil, verifyData{}, &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: "identifier"}
		}
		if findAttribute(targetBody, operation.Name) != nil {
			return nil, verifyData{}, &EditFailure{Kind: EditFailureDuplicateAttribute}
		}
		if failure := checkValue(operation.Value); failure != nil {
			return nil, verifyData{}, failure
		}
		point, indent, leadingNewline, failure := insertionPoint(index, current, &operation.Body,
			targetBody, parent, operation.Placement, operation.Anchor)
		if failure != nil {
			return nil, verifyData{}, failure
		}
		var markup strings.Builder
		if leadingNewline {
			markup.WriteByte('\n')
		}
		markup.WriteString(indent)
		markup.WriteString(operation.Name)
		markup.WriteString(" = ")
		rendered, failure := renderValue(operation.Value, indent)
		if failure != nil {
			return nil, verifyData{}, failure
		}
		markup.WriteString(rendered)
		markup.WriteByte('\n')
		return []appliedEdit{splice(point, 0, []byte(markup.String()))}, verifyData{}, nil
	case EditOperationRemoveAttribute:
		targetBody, _, failure := resolveBody(document, &operation.Body)
		if failure != nil {
			return nil, verifyData{}, failure
		}
		attributeRef := findAttribute(targetBody, operation.Attribute)
		if attributeRef == nil {
			return nil, verifyData{}, &EditFailure{Kind: EditFailureIncompleteTarget}
		}
		// The removal owns the item's line: its leading indentation, the
		// name, equals, and expression, and the owned trivia through the
		// terminating newline.
		start := itemLineStart(index, attributeRef.nameSpan.StartByte())
		end, failure := itemLineEnd(index, attributeRef.expression.span.EndByte())
		if failure != nil {
			return nil, verifyData{}, failure
		}
		return []appliedEdit{splice(start, end-start, nil)}, verifyData{}, nil
	case EditOperationRenameAttribute:
		targetBody, _, failure := resolveBody(document, &operation.Body)
		if failure != nil {
			return nil, verifyData{}, failure
		}
		attributeRef := findAttribute(targetBody, operation.Attribute)
		if attributeRef == nil {
			return nil, verifyData{}, &EditFailure{Kind: EditFailureIncompleteTarget}
		}
		if !isPlainIdentifier(operation.Name) {
			return nil, verifyData{}, &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: "identifier"}
		}
		kind := attributeRef.expression.kind
		if operation.Attribute == operation.Name {
			return []appliedEdit{}, verifyData{renameKind: &kind}, nil
		}
		if findAttribute(targetBody, operation.Name) != nil {
			return nil, verifyData{}, &EditFailure{Kind: EditFailureDuplicateAttribute}
		}
		start := attributeRef.nameSpan.StartByte()
		end := attributeRef.nameSpan.EndByte()
		return []appliedEdit{splice(start, end-start, []byte(operation.Name))}, verifyData{renameKind: &kind}, nil
	case EditOperationInsertBlock:
		// The profile gate precedes every target check: a block operation
		// can never succeed under the tfvars profile.
		if current.selector() == HclProfileTfvarsV1 {
			return nil, verifyData{}, &EditFailure{Kind: EditFailureBlockInTfvars}
		}
		if !isPlainIdentifier(operation.BlockType) {
			return nil, verifyData{}, &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: "identifier"}
		}
		seen := make(map[string]bool)
		for _, attribute := range operation.Attributes {
			if !isPlainIdentifier(attribute.Name) {
				return nil, verifyData{}, &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: "identifier"}
			}
			if seen[attribute.Name] {
				return nil, verifyData{}, &EditFailure{Kind: EditFailureDuplicateAttribute}
			}
			seen[attribute.Name] = true
			if failure := checkValue(attribute.Value); failure != nil {
				return nil, verifyData{}, failure
			}
		}
		targetBody, parent, failure := resolveBody(document, &operation.Body)
		if failure != nil {
			return nil, verifyData{}, failure
		}
		point, indent, leadingNewline, failure := insertionPoint(index, current, &operation.Body,
			targetBody, parent, operation.Placement, operation.Anchor)
		if failure != nil {
			return nil, verifyData{}, failure
		}
		var markup strings.Builder
		if leadingNewline {
			markup.WriteByte('\n')
		}
		blockMarkup, failure := renderBlockMarkup(indent, operation.BlockType, operation.Labels,
			operation.Attributes)
		if failure != nil {
			return nil, verifyData{}, failure
		}
		markup.WriteString(blockMarkup)
		return []appliedEdit{splice(point, 0, []byte(markup.String()))}, verifyData{}, nil
	case EditOperationRemoveBlock:
		targetBody, _, failure := resolveBody(document, &operation.Body)
		if failure != nil {
			return nil, verifyData{}, failure
		}
		blockRef := findBlock(targetBody, operation.BlockType, operation.Labels, operation.Occurrence)
		if blockRef == nil {
			for i := range targetBody.items {
				if attribute := targetBody.items[i].AsAttribute(); attribute != nil &&
					attribute.name == operation.BlockType {
					return nil, verifyData{}, &EditFailure{Kind: EditFailureWrongRole}
				}
			}
			return nil, verifyData{}, &EditFailure{Kind: EditFailureIncompleteTarget}
		}
		start := itemLineStart(index, blockRef.span.StartByte())
		end, failure := itemLineEnd(index, blockRef.span.EndByte())
		if failure != nil {
			return nil, verifyData{}, failure
		}
		return []appliedEdit{splice(start, end-start, nil)}, verifyData{}, nil
	}
	return nil, verifyData{}, &EditFailure{Kind: EditFailureWrongRole}
}

// checkValue rejects one typed value that cannot be expressed as
// literal-complete HCL: a non-finite real, an object key that is not a
// bare identifier/number/string (including the reserved `for` spelling),
// or any expression value (RFC 0014 §8.1, §10, §14).
func checkValue(value EditValue) *EditFailure {
	switch value.Tag {
	case EditValueReal:
		if !isFinite(value.Real) {
			return &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: "real"}
		}
		return nil
	case EditValueInteger, EditValueBoolean, EditValueNull, EditValueString:
		return nil
	case EditValueTuple:
		for _, element := range value.Elements {
			if failure := checkValue(element); failure != nil {
				return failure
			}
		}
		return nil
	case EditValueObject:
		for _, entry := range value.Entries {
			if failure := checkKey(entry.Key); failure != nil {
				return failure
			}
			if failure := checkValue(entry.Value); failure != nil {
				return failure
			}
		}
		return nil
	case EditValueExpression:
		return &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: "expression"}
	}
	return nil
}

// checkKey rejects one object key that cannot be expressed as a bare
// literal key.
func checkKey(key EditKey) *EditFailure {
	switch key.Tag {
	case EditKeyIdentifier:
		if isPlainIdentifier(key.Text) && key.Text != "for" {
			return nil
		}
		return &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: "object-key"}
	case EditKeyNumber, EditKeyString:
		return nil
	}
	return nil
}

// isFinite reports whether one float is finite.
func isFinite(value float64) bool {
	return value == value && value <= 1.7976931348623157e308 && value >= -1.7976931348623157e308
}

// quoteEscape is the minimal deterministic quoted-template spelling of one
// string (RFC 0014 §9): `\n`, `\r`, `\t`, `\"`, `\\`, `\uNNNN` for other
// control characters, and `$${`/`%%{` so the reparse keeps `${`/`%{` as
// literal text.
func quoteEscape(text string) string {
	var out strings.Builder
	out.Grow(len(text) + 2)
	out.WriteByte('"')
	characters := []rune(text)
	for index := 0; index < len(characters); index++ {
		character := characters[index]
		switch character {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		case '$':
			if index+1 < len(characters) && characters[index+1] == '{' {
				out.WriteString("$${")
				index++
			} else {
				out.WriteRune(character)
			}
		case '%':
			if index+1 < len(characters) && characters[index+1] == '{' {
				out.WriteString("%%{")
				index++
			} else {
				out.WriteRune(character)
			}
		default:
			if character < 0x20 || character == 0x7F {
				out.WriteString(escapeUpperHex(character))
			} else {
				out.WriteRune(character)
			}
		}
	}
	out.WriteByte('"')
	return out.String()
}

// escapeUpperHex renders one \uXXXX escape with uppercase hex digits (the
// edit canonical spelling).
func escapeUpperHex(character rune) string {
	const digits = "0123456789ABCDEF"
	value := uint32(character)
	return "\\u" + string([]byte{
		digits[(value>>12)&0xf], digits[(value>>8)&0xf], digits[(value>>4)&0xf], digits[value&0xf],
	})
}

// canonicalReal is the canonical decimal spelling of one finite real, by
// pure decimal string arithmetic over its shortest-round-trip spelling
// (hard gate 1): the sign is reattached to the canonical magnitude, and
// `-0` normalizes to `0`.
func canonicalReal(value float64) (string, bool) {
	if !isFinite(value) {
		return "", false
	}
	text := strconv.FormatFloat(value, 'g', -1, 64)
	if strings.HasPrefix(text, "-") {
		canonical, ok := CanonicalDecimal(text[1:])
		if !ok {
			return "", false
		}
		if canonical == "0" {
			return canonical, true
		}
		return "-" + canonical, true
	}
	return CanonicalDecimal(text)
}

// renderValue is the canonical expression text of one typed literal value
// at one base indentation; constructor continuation lines render one item
// per line at the base indentation plus two spaces (RFC 0014 §9).
func renderValue(value EditValue, indent string) (string, *EditFailure) {
	switch value.Tag {
	case EditValueInteger:
		return strconv.FormatInt(value.Integer, 10), nil
	case EditValueReal:
		canonical, ok := canonicalReal(value.Real)
		if !ok {
			return "", &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: "real"}
		}
		return canonical, nil
	case EditValueString:
		return quoteEscape(value.Text), nil
	case EditValueBoolean:
		if value.Boolean {
			return "true", nil
		}
		return "false", nil
	case EditValueNull:
		return "null", nil
	case EditValueTuple:
		if len(value.Elements) == 0 {
			return "[]", nil
		}
		inner := indent + "  "
		var out strings.Builder
		out.WriteString("[\n")
		for position, element := range value.Elements {
			if position > 0 {
				out.WriteString(",\n")
			}
			out.WriteString(inner)
			rendered, failure := renderValue(element, inner)
			if failure != nil {
				return "", failure
			}
			out.WriteString(rendered)
		}
		out.WriteByte('\n')
		out.WriteString(indent)
		out.WriteByte(']')
		return out.String(), nil
	case EditValueObject:
		if len(value.Entries) == 0 {
			return "{}", nil
		}
		inner := indent + "  "
		var out strings.Builder
		out.WriteString("{\n")
		for position, entry := range value.Entries {
			if position > 0 {
				out.WriteString(",\n")
			}
			out.WriteString(inner)
			out.WriteString(renderKey(entry.Key))
			out.WriteString(" = ")
			rendered, failure := renderValue(entry.Value, inner)
			if failure != nil {
				return "", failure
			}
			out.WriteString(rendered)
		}
		out.WriteByte('\n')
		out.WriteString(indent)
		out.WriteByte('}')
		return out.String(), nil
	case EditValueExpression:
		return "", &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: "expression"}
	}
	return "", &EditFailure{Kind: EditFailureUnrepresentableValue}
}

// renderKey is the bare spelling of one object key; validity is
// pre-checked by checkKey.
func renderKey(key EditKey) string {
	switch key.Tag {
	case EditKeyIdentifier:
		return key.Text
	case EditKeyNumber:
		return strconv.FormatInt(key.Number, 10)
	case EditKeyString:
		return quoteEscape(key.Text)
	}
	return ""
}

// renderBlockMarkup is the canonical block text at one base indentation:
// `type "label" {` header, two-space-indented nested attributes, closing
// brace, and a trailing newline; labels always render quoted (RFC 0014
// §9).
func renderBlockMarkup(indent, blockType string, labels []string,
	attributes []EditBlockAttribute) (string, *EditFailure) {
	var out strings.Builder
	out.WriteString(indent)
	out.WriteString(blockType)
	for _, label := range labels {
		out.WriteByte(' ')
		out.WriteString(quoteEscape(label))
	}
	out.WriteString(" {\n")
	inner := indent + "  "
	for _, attribute := range attributes {
		out.WriteString(inner)
		out.WriteString(attribute.Name)
		out.WriteString(" = ")
		rendered, failure := renderValue(attribute.Value, inner)
		if failure != nil {
			return "", failure
		}
		out.WriteString(rendered)
		out.WriteByte('\n')
	}
	out.WriteString(indent)
	out.WriteString("}\n")
	return out.String(), nil
}

// verifyOperation verifies the promised HCL semantics of one operation
// against the reparse of the state immediately after its application: the
// target resolves, a rename preserves its expression kind, a removal is
// gone, and every promised literal value equals the reparsed literal
// (numbers by canonical-decimal equality, RFC 0014 §6, §9).
func verifyOperation(formed *Document, operation *EditOperation, data verifyData) *EditFailure {
	document := formed.document
	switch operation.Kind {
	case EditOperationSetAttributeValue:
		targetBody, _, failure := resolveBody(document, &operation.Body)
		if failure != nil {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		attributeRef := findAttribute(targetBody, operation.Attribute)
		if attributeRef == nil {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		literal, err := LiteralValue(attributeRef.expression)
		if err != nil {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		if !editValueMatchesLiteral(&operation.Value, &literal) {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		return nil
	case EditOperationInsertAttribute:
		targetBody, _, failure := resolveBody(document, &operation.Body)
		if failure != nil {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		attributeRef := findAttribute(targetBody, operation.Name)
		if attributeRef == nil {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		literal, err := LiteralValue(attributeRef.expression)
		if err != nil {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		if !editValueMatchesLiteral(&operation.Value, &literal) {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		return nil
	case EditOperationRemoveAttribute:
		targetBody, _, failure := resolveBody(document, &operation.Body)
		if failure != nil {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		if findAttribute(targetBody, operation.Attribute) != nil {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		return nil
	case EditOperationRenameAttribute:
		if data.renameKind == nil {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		targetBody, _, failure := resolveBody(document, &operation.Body)
		if failure != nil {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		attributeRef := findAttribute(targetBody, operation.Name)
		if attributeRef == nil {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		if !attributeRef.expression.kind.Equal(*data.renameKind) {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		return nil
	case EditOperationInsertBlock:
		targetBody, _, failure := resolveBody(document, &operation.Body)
		if failure != nil {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		for i := range targetBody.items {
			block := targetBody.items[i].AsBlock()
			if block == nil || block.blockType != operation.BlockType ||
				!labelSequenceEqual(block.labels, operation.Labels) {
				continue
			}
			matches, failure := blockBodyMatches(block, operation.Attributes)
			if failure != nil {
				return failure
			}
			if matches {
				return nil
			}
		}
		return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	case EditOperationRemoveBlock:
		targetBody, _, failure := resolveBody(document, &operation.Body)
		if failure != nil {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		if findBlock(targetBody, operation.BlockType, operation.Labels, operation.Occurrence) != nil {
			return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		return nil
	}
	return &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
}

// blockBodyMatches reports whether one block's nested body carries exactly
// the promised attributes with the promised literal values.
func blockBodyMatches(block *HclBlock, attributes []EditBlockAttribute) (bool, *EditFailure) {
	items := block.body.items
	count := 0
	for i := range items {
		if items[i].AsAttribute() != nil {
			count++
		}
	}
	if count != len(attributes) {
		return false, nil
	}
	for _, attribute := range attributes {
		attributeRef := findAttribute(block.body, attribute.Name)
		if attributeRef == nil {
			return false, nil
		}
		literal, err := LiteralValue(attributeRef.expression)
		if err != nil {
			return false, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		if !editValueMatchesLiteral(&attribute.Value, &literal) {
			return false, nil
		}
	}
	return true, nil
}

// editValueMatchesLiteral reports whether one typed edit value equals one
// reparsed literal; numbers compare by canonical-decimal value equality
// across the integer/real kind boundary (RFC 0014 §6).
func editValueMatchesLiteral(value *EditValue, literal *HclLiteralValue) bool {
	switch value.Tag {
	case EditValueInteger:
		if literal.tag != literalInteger {
			return false
		}
		return strconv.FormatInt(value.Integer, 10) == literal.text
	case EditValueReal:
		if literal.tag != literalInteger && literal.tag != literalDecimal {
			return false
		}
		canonical, ok := canonicalReal(value.Real)
		return ok && canonical == literal.text
	case EditValueString:
		return literal.tag == literalString && value.Text == literal.text
	case EditValueBoolean:
		return literal.tag == literalBoolean && value.Boolean == literal.boolean
	case EditValueNull:
		return literal.tag == literalNull
	case EditValueTuple:
		if literal.tag != literalTuple || len(value.Elements) != len(literal.elements) {
			return false
		}
		for i := range value.Elements {
			if !editValueMatchesLiteral(&value.Elements[i], &literal.elements[i]) {
				return false
			}
		}
		return true
	case EditValueObject:
		if literal.tag != literalObject || len(value.Entries) != len(literal.entries) {
			return false
		}
		for i := range value.Entries {
			if !editKeyMatchesLiteral(&value.Entries[i].Key, &literal.entries[i].key) {
				return false
			}
			if !editValueMatchesLiteral(&value.Entries[i].Value, &literal.entries[i].value) {
				return false
			}
		}
		return true
	}
	return false
}

// editKeyMatchesLiteral reports whether one typed object key equals one
// reparsed literal key.
func editKeyMatchesLiteral(key *EditKey, literal *HclLiteralKey) bool {
	switch key.Tag {
	case EditKeyIdentifier:
		return literal.tag == literalKeyIdentifier && key.Text == literal.text
	case EditKeyNumber:
		return literal.tag == literalKeyNumber && strconv.FormatInt(key.Number, 10) == literal.text
	case EditKeyString:
		return literal.tag == literalKeyString && key.Text == literal.text
	}
	return false
}

// buildCommit builds the commit facts: ChangeSet, replayable SourcePatch,
// and the untouched-byte proof (RFC 0014 §10).
func buildCommit(base *Document, transaction *EditTransaction, finalDocument *Document,
	edits []appliedEdit) (*EditCommit, *EditFailure) {
	limits := base.limits
	if len(edits) > limits.MaxReportEvents {
		return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "report-events"}
	}
	oldAuthority := base.authority
	newAuthority := finalDocument.authority
	// The recorded edits are merged into maximal non-overlapping base runs
	// (spans that overlap or touch). Each run's replacement is the exact
	// target bytes at its new span.
	spans := make([]baseSpan, 0, len(edits))
	for index := range edits {
		oldStart, failure := unmapIn(edits[:index], edits[index].preStart)
		if failure != nil {
			return nil, failure
		}
		oldEnd, failure := unmapIn(edits[:index], edits[index].preStart+edits[index].preLen)
		if failure != nil {
			return nil, failure
		}
		delta := len(edits[index].replacement) - edits[index].preLen
		spans = append(spans, baseSpan{start: oldStart, end: oldEnd, delta: delta})
	}
	sortSpans(spans)
	var runs []baseSpan
	for _, current := range spans {
		if len(runs) > 0 {
			last := &runs[len(runs)-1]
			if current.start <= last.end {
				if current.end > last.end {
					last.end = current.end
				}
				last.delta += current.delta
				continue
			}
		}
		runs = append(runs, current)
	}
	beforeDelta := 0
	targetBytes := finalDocument.source.Bytes()
	var sourceEdits []document.SourceEdit
	for _, run := range runs {
		targetStart, failure := shiftPosition(run.start, beforeDelta)
		if failure != nil {
			return nil, failure
		}
		runLen := (run.end - run.start) + run.delta
		if runLen < 0 {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		targetEnd := targetStart + runLen
		if targetEnd > len(targetBytes) {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		oldSpan, err := oldAuthority.Span(run.start, run.end)
		if err != nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		newSpan, err := newAuthority.Span(targetStart, targetEnd)
		if err != nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		sourceEdits = append(sourceEdits, document.SourceEdit{
			OldSpan:     oldSpan,
			NewSpan:     newSpan,
			Replacement: append([]byte(nil), targetBytes[targetStart:targetEnd]...),
		})
		beforeDelta += run.delta
	}
	changeSet := document.NewChangeSet(base.SnapshotIdentity(), finalDocument.SnapshotIdentity(),
		sourceEdits, buildMappings(base, transaction, finalDocument), nil)
	sourcePatch, patchErr := document.NewSourcePatch(base.source,
		sourcePatchReplacements(base, sourceEdits),
		operationMetadata(transaction),
		sourcePatchLimits(limits, len(sourceEdits)))
	if patchErr != nil || !sourcePatch.TargetDigest().Equal(finalDocument.source.Digest()) {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	proof, proofErr := CreateUntouchedByteProof(base.source, finalDocument.source,
		sourcePatch.Replacements())
	if proofErr != nil {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return &EditCommit{
		Document:       finalDocument,
		ChangeSet:      changeSet,
		SourcePatch:    sourcePatch,
		UntouchedProof: proof,
	}, nil
}

// sourcePatchReplacements builds the ordered patch replacements from the
// source edits.
func sourcePatchReplacements(base *Document, sourceEdits []document.SourceEdit) []document.SourceReplacement {
	text, _ := base.source.DecodedText()
	replacements := make([]document.SourceReplacement, 0, len(sourceEdits))
	for _, edit := range sourceEdits {
		original := text[edit.OldSpan.StartByte():edit.OldSpan.EndByte()]
		replacements = append(replacements, document.NewSourceReplacement(
			edit.OldSpan.StartByte(), edit.OldSpan.EndByte(),
			[]byte(original), edit.Replacement))
	}
	return replacements
}

// sourcePatchLimits derives the patch budgets from the parse limits.
func sourcePatchLimits(parseLimits HclParseLimits, operationCount int) document.SourcePatchLimits {
	return document.SourcePatchLimits{
		Source: document.SourceLimits{
			MaxRawBytes:         parseLimits.Common.MaxSourceBytes,
			MaxDecodedUTF8Bytes: parseLimits.Common.MaxSourceBytes,
			MaxDecodedScalars:   parseLimits.Common.MaxSourceBytes,
		},
		MaxReplacements: operationCount,
		MaxPatchBytes:   parseLimits.Common.MaxSourceBytes * 2,
	}
}

// operationMetadata builds the deterministic patch metadata.
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
	case EditOperationSetAttributeValue:
		return "hcl.edit.set-attribute-value@1"
	case EditOperationInsertAttribute:
		return "hcl.edit.insert-attribute@1"
	case EditOperationRemoveAttribute:
		return "hcl.edit.remove-attribute@1"
	case EditOperationRenameAttribute:
		return "hcl.edit.rename-attribute@1"
	case EditOperationInsertBlock:
		return "hcl.edit.insert-block@1"
	case EditOperationRemoveBlock:
		return "hcl.edit.remove-block@1"
	}
	return "hcl.edit.unknown@1"
}

// operationSummaries builds the safe content-free summaries.
func operationSummaries(tx *EditTransaction) ([]*protocol.EditOperationSummary, *EditFailure) {
	summaries := make([]*protocol.EditOperationSummary, 0, len(tx.operations))
	for _, operation := range tx.operations {
		var id string
		var targetRole string
		arguments := make(map[string]string)
		switch operation.Kind {
		case EditOperationSetAttributeValue:
			id = "hcl.edit.set-attribute-value"
			targetRole = "hcl.attribute@1"
			arguments["value_kind"] = operation.Value.KindName()
		case EditOperationInsertAttribute:
			id = "hcl.edit.insert-attribute"
			targetRole = "hcl.body@1"
			arguments["name_bytes"] = uint64String(uint64(len(operation.Name)))
			arguments["placement"] = placementName(operation.Placement)
			arguments["value_kind"] = operation.Value.KindName()
		case EditOperationRemoveAttribute:
			id = "hcl.edit.remove-attribute"
			targetRole = "hcl.attribute@1"
		case EditOperationRenameAttribute:
			id = "hcl.edit.rename-attribute"
			targetRole = "hcl.attribute@1"
			arguments["name_bytes"] = uint64String(uint64(len(operation.Name)))
		case EditOperationInsertBlock:
			id = "hcl.edit.insert-block"
			targetRole = "hcl.body@1"
			arguments["type_bytes"] = uint64String(uint64(len(operation.BlockType)))
			arguments["label_count"] = uint64String(uint64(len(operation.Labels)))
			arguments["placement"] = placementName(operation.Placement)
		case EditOperationRemoveBlock:
			id = "hcl.edit.remove-block"
			targetRole = "hcl.block@1"
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

// placementName renders one placement's stable name.
func placementName(placement BodyPlacement) string {
	switch placement {
	case BodyPlacementFirst:
		return "start"
	case BodyPlacementLast:
		return "end"
	case BodyPlacementAfter:
		return "after"
	}
	return "start"
}

// buildMappings builds one old-to-new mapping per operation whose target
// resolves in the base snapshot; insertions carry no mapping.
func buildMappings(base *Document, transaction *EditTransaction,
	finalDocument *Document) []document.NodeMapping {
	var mappings []document.NodeMapping
	for i := range transaction.operations {
		operation := &transaction.operations[i]
		mapping := mappingFor(operation, base.document, finalDocument.document,
			base.authority, finalDocument.authority)
		if mapping != nil {
			mappings = append(mappings, *mapping)
		}
	}
	return mappings
}

// mappingFor is one mapping fact for one operation, when its target
// resolves in the base.
func mappingFor(operation *EditOperation, baseDocument, finalDocument *HclDocument,
	oldAuthority, newAuthority document.DocumentAuthority) *document.NodeMapping {
	var old document.NodeRef
	var newRef *document.NodeRef
	var status protocol.NodeMappingStatus
	var reason *string
	switch operation.Kind {
	case EditOperationSetAttributeValue:
		oldRef := resolveAttributeMapping(baseDocument, &operation.Body, operation.Attribute)
		if oldRef == nil {
			return nil
		}
		old = oldAuthority.NodeRef(oldRef.index, document.RoleHclAttribute)
		if resolved := resolveAttributeMapping(finalDocument, &operation.Body, operation.Attribute); resolved != nil {
			value := newAuthority.NodeRef(resolved.index, document.RoleHclAttribute)
			newRef = &value
			status = protocol.MappingReplaced
		} else {
			status = protocol.MappingUnmapped
			text := "reparsed-node-not-uniquely-located"
			reason = &text
		}
	case EditOperationRenameAttribute:
		oldRef := resolveAttributeMapping(baseDocument, &operation.Body, operation.Attribute)
		if oldRef == nil {
			return nil
		}
		old = oldAuthority.NodeRef(oldRef.index, document.RoleHclAttribute)
		if resolved := resolveAttributeMapping(finalDocument, &operation.Body, operation.Name); resolved != nil {
			value := newAuthority.NodeRef(resolved.index, document.RoleHclAttribute)
			newRef = &value
			status = protocol.MappingReplaced
		} else {
			status = protocol.MappingUnmapped
			text := "reparsed-node-not-uniquely-located"
			reason = &text
		}
	case EditOperationRemoveAttribute:
		oldRef := resolveAttributeMapping(baseDocument, &operation.Body, operation.Attribute)
		if oldRef == nil {
			return nil
		}
		old = oldAuthority.NodeRef(oldRef.index, document.RoleHclAttribute)
		status = protocol.MappingDeleted
	case EditOperationRemoveBlock:
		oldRef := resolveBlockMapping(baseDocument, &operation.Body, operation.BlockType,
			operation.Labels, operation.Occurrence)
		if oldRef == nil {
			return nil
		}
		old = oldAuthority.NodeRef(oldRef.index, document.RoleHclBlock)
		status = protocol.MappingDeleted
	case EditOperationInsertAttribute, EditOperationInsertBlock:
		return nil
	}
	return &document.NodeMapping{Old: old, New: newRef, Status: status, Reason: reason}
}

// documentNodeRef is one deterministic (index, role) identity of a native
// construct; the authority binding happens at mapping assembly.
type documentNodeRef struct {
	index uint64
	role  document.NodeRole
}

// resolveAttributeMapping resolves one attribute's node index (its name's
// start byte) in one document.
func resolveAttributeMapping(doc *HclDocument, body *BodyPath, attribute string) *documentNodeRef {
	targetBody, _, failure := resolveBody(doc, body)
	if failure != nil {
		return nil
	}
	attributeRef := findAttribute(targetBody, attribute)
	if attributeRef == nil {
		return nil
	}
	return &documentNodeRef{index: uint64(attributeRef.nameSpan.StartByte()),
		role: document.RoleHclAttribute}
}

// resolveBlockMapping resolves one block's node index (its span's start
// byte) in one document.
func resolveBlockMapping(doc *HclDocument, body *BodyPath, blockType string,
	labels []string, occurrence int) *documentNodeRef {
	targetBody, _, failure := resolveBody(doc, body)
	if failure != nil {
		return nil
	}
	blockRef := findBlock(targetBody, blockType, labels, occurrence)
	if blockRef == nil {
		return nil
	}
	return &documentNodeRef{index: uint64(blockRef.span.StartByte()),
		role: document.RoleHclBlock}
}

// baseSpan is one base-coordinate replacement run.
type baseSpan struct {
	start int
	end   int
	delta int
}

// sortSpans sorts one span list by start, then end.
func sortSpans(spans []baseSpan) {
	for i := 1; i < len(spans); i++ {
		for j := i; j > 0; j-- {
			if spans[j-1].start < spans[j].start ||
				(spans[j-1].start == spans[j].start && spans[j-1].end <= spans[j].end) {
				break
			}
			spans[j-1], spans[j] = spans[j], spans[j-1]
		}
	}
}

// uint64String formats one unsigned ordinal.
func uint64String(value uint64) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

// bigIntString formats one big.Int.
func bigIntString(value *big.Int) string { return value.String() }
