package plist

// This file implements the snapshot-bound plist structural edit surface
// (consema-plist edit.rs; RFC 0013 §11). Both profiles publish the same six
// versioned operations: set-value, insert-dict-entry, remove-dict-entry,
// rename-dict-key, insert-array-element, and remove-array-element. Targets
// are addressed by root-relative EditPath steps; values are supplied as
// typed native facts (EditValue), never raw markup or raw bytes.
//
// XML edits are byte-level like RFC 0012: each operation replaces only
// operation-owned spans of the raw source, keeps every untouched byte,
// reparses the target, and verifies the promised plist semantics. Binary
// edits are structural: `set-value` rewrites the target object's marker and
// payload, `insert`/`remove` rewrite the owning container's reference
// block, and the offset table and trailer are regenerated whenever sizes
// change. All offset, size, and reference arithmetic is checked before any
// output exists (hard gate 4).
//
// Operations apply sequentially against the evolving document state: an
// index or occurrence refers to the state as of the operation's own
// application. Commit returns the new Document, a complete ChangeSet, an
// UntouchedByteProof, and a replayable SourcePatch; a failure never
// modifies the base document.

import (
	"math"
	"sort"
	"strings"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// EditPathStep is one root-relative path step (RFC 0013 §11). A DictKey
// step selects one physical dictionary association by exact key content and
// occurrence; an ArrayIndex step selects one array element by its 0-based
// position.
type EditPathStep struct {
	// Kind is "DictKey" or "ArrayIndex".
	Kind string
	// Key is the exact key content of DictKey steps.
	Key PlistKey
	// Occurrence is the 0-based source position among the equal keys.
	Occurrence int
	// Index is the 0-based array position of ArrayIndex steps.
	Index int
}

// NewEditPathStepDictKey creates one dictionary association step.
func NewEditPathStepDictKey(key PlistKey, occurrence int) EditPathStep {
	return EditPathStep{Kind: "DictKey", Key: key, Occurrence: occurrence}
}

// NewEditPathStepArrayIndex creates one array element step.
func NewEditPathStepArrayIndex(index int) EditPathStep {
	return EditPathStep{Kind: "ArrayIndex", Index: index}
}

// EditPath is a root-relative path to one value or container (RFC 0013
// §11). The empty path denotes the root value. A path step that meets a
// container of the wrong kind is a role failure; a step that does not exist
// in the current document state is a missing-target failure.
type EditPath struct {
	steps []EditPathStep
}

// NewEditPathRoot returns the root path.
func NewEditPathRoot() EditPath { return EditPath{} }

// NewEditPath creates a path from ordered steps.
func NewEditPath(steps []EditPathStep) EditPath {
	return EditPath{steps: append([]EditPathStep(nil), steps...)}
}

// Segments returns the ordered path steps. The returned slice must not be
// modified.
func (p EditPath) Segments() []EditPathStep { return p.steps }

// Child creates a child path without modifying this path.
func (p EditPath) Child(step EditPathStep) EditPath {
	steps := append([]EditPathStep(nil), p.steps...)
	steps = append(steps, step)
	return EditPath{steps: steps}
}

// DictPlacement is the dictionary entry insertion placement inside one
// dictionary (RFC 0013 §11).
type DictPlacement uint8

// The three frozen placements.
const (
	// DictPlacementEnd appends before the closing `</dict>` (or wraps a
	// self-closing `<dict/>`).
	DictPlacementEnd DictPlacement = iota
	// DictPlacementBefore inserts immediately before the entry at the given
	// 0-based source position.
	DictPlacementBefore
	// DictPlacementAfter inserts immediately after the entry at the given
	// 0-based source position.
	DictPlacementAfter
)

// NewDictPlacementEnd returns the End placement.
func NewDictPlacementEnd() DictPlacement { return DictPlacementEnd }

// NewDictPlacementBefore returns a Before placement.
func NewDictPlacementBefore(position int) DictPlacement { return DictPlacementBefore }

// NewDictPlacementAfter returns an After placement.
func NewDictPlacementAfter(position int) DictPlacement { return DictPlacementAfter }

// EditValueKind is the closed native kind of one typed edit value.
type EditValueKind uint8

// The seven frozen edit value kinds.
const (
	EditValueString EditValueKind = iota
	EditValueInteger
	EditValueReal
	EditValueBoolean
	EditValueDate
	EditValueData
	EditValueUID
)

// EditValue is one typed native plist value supplied to an edit (RFC 0013
// §11). Values are typed native facts, never raw markup or raw bytes.
type EditValue struct {
	kind    EditValueKind
	str     PlistString
	integer PlistInteger
	real    PlistReal
	boolean PlistBoolean
	date    PlistDate
	data    PlistData
	uid     PlistUID
}

// NewEditValueString creates a string edit value.
func NewEditValueString(value PlistString) EditValue {
	return EditValue{kind: EditValueString, str: value}
}

// NewEditValueInteger creates an integer edit value.
func NewEditValueInteger(value PlistInteger) EditValue {
	return EditValue{kind: EditValueInteger, integer: value}
}

// NewEditValueReal creates a real edit value.
func NewEditValueReal(value PlistReal) EditValue {
	return EditValue{kind: EditValueReal, real: value}
}

// NewEditValueBoolean creates a boolean edit value.
func NewEditValueBoolean(value PlistBoolean) EditValue {
	return EditValue{kind: EditValueBoolean, boolean: value}
}

// NewEditValueDate creates a date edit value.
func NewEditValueDate(value PlistDate) EditValue {
	return EditValue{kind: EditValueDate, date: value}
}

// NewEditValueData creates a data edit value.
func NewEditValueData(value PlistData) EditValue {
	return EditValue{kind: EditValueData, data: value}
}

// NewEditValueUID creates a UID edit value (binary profile only).
func NewEditValueUID(value PlistUID) EditValue {
	return EditValue{kind: EditValueUID, uid: value}
}

// Kind returns the closed native kind of this value.
func (v EditValue) Kind() EditValueKind { return v.kind }

// EditOperationKind is the closed plist edit operation category (RFC 0013
// §11).
type EditOperationKind uint8

// The six frozen operation categories.
const (
	EditOperationSetValue EditOperationKind = iota
	EditOperationInsertDictEntry
	EditOperationRemoveDictEntry
	EditOperationRenameDictKey
	EditOperationInsertArrayElement
	EditOperationRemoveArrayElement
)

// EditOperation is one snapshot-bound plist structural operation (RFC 0013
// §11). The path, key, occurrence, index, and placement of every operation
// refer to the document state as of the operation's own application.
type EditOperation struct {
	// Kind is the closed operation category.
	Kind EditOperationKind
	// Path is the target path.
	Path EditPath
	// Value is the typed new value (SetValue, InsertDictEntry,
	// InsertArrayElement).
	Value EditValue
	// Key is the new or renamed key content (InsertDictEntry, RenameDictKey)
	// or the key to remove (RemoveDictEntry).
	Key PlistKey
	// From is the key content to rename.
	From PlistKey
	// Occurrence is the 0-based position among equal keys.
	Occurrence int
	// Placement is the dictionary entry placement (InsertDictEntry).
	Placement DictPlacement
	// PlacementPosition is the Before/After position.
	PlacementPosition int
	// Index is the array insertion or removal position.
	Index int
}

// EditTransaction is the immutable snapshot-bound transaction.
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
// (RFC 0013 §11).
type EditTransactionBuilder struct {
	base       document.SnapshotIdentity
	operations []EditOperation
}

// NewEditTransactionBuilder binds a new transaction to one immutable base
// document.
func NewEditTransactionBuilder(document *Document) *EditTransactionBuilder {
	return &EditTransactionBuilder{base: document.SnapshotIdentity()}
}

// SetValue adds a value replacement.
func (b *EditTransactionBuilder) SetValue(path EditPath, value EditValue) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationSetValue, Path: path, Value: value})
	return b
}

// InsertDictEntry adds one dictionary association insertion.
func (b *EditTransactionBuilder) InsertDictEntry(path EditPath, key PlistKey,
	value EditValue, placement DictPlacement, position int) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationInsertDictEntry, Path: path, Key: key, Value: value,
		Placement: placement, PlacementPosition: position})
	return b
}

// RemoveDictEntry adds one dictionary association removal.
func (b *EditTransactionBuilder) RemoveDictEntry(path EditPath, key PlistKey,
	occurrence int) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRemoveDictEntry, Path: path, Key: key, Occurrence: occurrence})
	return b
}

// RenameDictKey adds one dictionary key rename.
func (b *EditTransactionBuilder) RenameDictKey(path EditPath, from PlistKey,
	occurrence int, to PlistKey) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRenameDictKey, Path: path, From: from,
		Occurrence: occurrence, Key: to})
	return b
}

// InsertArrayElement adds one array element insertion.
func (b *EditTransactionBuilder) InsertArrayElement(path EditPath, index int,
	value EditValue) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationInsertArrayElement, Path: path, Index: index, Value: value})
	return b
}

// RemoveArrayElement adds one array element removal.
func (b *EditTransactionBuilder) RemoveArrayElement(path EditPath,
	index int) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRemoveArrayElement, Path: path, Index: index})
	return b
}

// Build completes the immutable transaction; target validation happens
// atomically at commit.
func (b *EditTransactionBuilder) Build() *EditTransaction {
	return &EditTransaction{
		base:       b.base,
		operations: append([]EditOperation(nil), b.operations...),
	}
}

// EditCommit is the atomic edit success (RFC 0013 §11).
type EditCommit struct {
	// Document is the new immutable document.
	Document *Document
	// ChangeSet carries the complete old-to-new change facts.
	ChangeSet document.ChangeSet
	// SourcePatch carries the portable exact raw-byte application facts.
	SourcePatch *document.SourcePatch
	// UntouchedProof carries verifiable evidence for every byte outside the
	// replacement set.
	UntouchedProof *document.UntouchedByteProof
}

// EditFailureKind is the stable edit validation or commit failure category
// (consema-plist edit.rs EditFailure).
type EditFailureKind uint8

// The closed edit failure categories.
const (
	// EditFailureWrongSnapshot: the transaction or target belongs to
	// another snapshot.
	EditFailureWrongSnapshot EditFailureKind = iota
	// EditFailureWrongRole: a path step meets a container of the wrong
	// kind.
	EditFailureWrongRole
	// EditFailureTargetNotFound: a path step, key occurrence, index, or
	// placement anchor does not exist.
	EditFailureTargetNotFound
	// EditFailureIncompleteTarget: the base document is not Complete with a
	// provable native value graph.
	EditFailureIncompleteTarget
	// EditFailureConflictingEdits: two operations target the same exact
	// source position or occurrence.
	EditFailureConflictingEdits
	// EditFailureOverlappingOwnership: one operation's source span contains
	// bytes an earlier operation replaced.
	EditFailureOverlappingOwnership
	// EditFailureUIDInXML: a UID value was inserted into or set on an XML
	// document.
	EditFailureUIDInXML
	// EditFailureUnrepresentableValue: a typed value or key cannot be
	// expressed in the target representation.
	EditFailureUnrepresentableValue
	// EditFailureResourceLimit: a configured edit or output bound was
	// exceeded.
	EditFailureResourceLimit
	// EditFailureNewDocumentFormationFailed: the replacement document could
	// not be formed under the original limits.
	EditFailureNewDocumentFormationFailed
)

// EditFailure is the typed edit failure. It implements error and the RFC
// 0016 §6 Code() contract.
type EditFailure struct {
	// Kind identifies the failure.
	Kind EditFailureKind
	// Fact is the blocking native fact of UnrepresentableValue.
	Fact string
	// LimitName is the stable limit name of a ResourceLimit.
	LimitName string
}

// Error implements error.
func (e *EditFailure) Error() string {
	switch e.Kind {
	case EditFailureWrongSnapshot:
		return "plist: edit transaction or target belongs to another snapshot"
	case EditFailureWrongRole:
		return "plist: edit path meets a container of the wrong kind"
	case EditFailureTargetNotFound:
		return "plist: edit target was not found"
	case EditFailureIncompleteTarget:
		return "plist: the base document is not complete with a provable native value"
	case EditFailureConflictingEdits:
		return "plist: edit operations target the same source position"
	case EditFailureOverlappingOwnership:
		return "plist: edit source ownership overlaps"
	case EditFailureUIDInXML:
		return "plist: a UID value cannot enter an XML document"
	case EditFailureUnrepresentableValue:
		return "plist: typed value is not expressible in the target representation: " + e.Fact
	case EditFailureResourceLimit:
		return "plist: edit limit " + e.LimitName + " reached"
	case EditFailureNewDocumentFormationFailed:
		return "plist: replacement document could not be formed"
	}
	return "plist: edit failure"
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
	case EditFailureConflictingEdits:
		return "ConflictingEdits"
	case EditFailureOverlappingOwnership:
		return "OverlappingOwnership"
	case EditFailureUIDInXML:
		return "UidInXml"
	case EditFailureUnrepresentableValue:
		return "UnrepresentableValue"
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
	case EditFailureConflictingEdits, EditFailureOverlappingOwnership:
		return "core.edit.conflicting-edits@1"
	case EditFailureUIDInXML:
		return "plist.edit.uid-in-xml@1"
	case EditFailureUnrepresentableValue:
		return "plist.edit.unrepresentable@1"
	case EditFailureResourceLimit:
		return "core.edit.resource-limit@1"
	case EditFailureNewDocumentFormationFailed:
		return "core.edit.formation-failed@1"
	}
	return "core.edit.conflicting-edits@1"
}

// Commit atomically commits structural operations. On failure the base
// document remains unchanged (RFC 0013 §11).
func (d *Document) Commit(tx *EditTransaction) (*EditCommit, *EditFailure) {
	if tx.base != d.SnapshotIdentity() {
		return nil, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if d.status != document.FormationStatusComplete || d.native == nil {
		return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	if len(tx.operations) > d.limits.MaxReportEvents {
		return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "report-events"}
	}
	if d.representation == PlistRepresentationXML {
		return d.commitXML(tx)
	}
	return d.commitBinary(tx)
}

// DryRun fully validates and plans an edit without returning a new Document
// (RFC 0016 §5.3).
func (d *Document) DryRun(tx *EditTransaction, sourceID string) (*document.EditPlan, *EditFailure) {
	commit, failure := d.Commit(tx)
	if failure != nil {
		return nil, failure
	}
	summaries := make([]*protocol.EditOperationSummary, 0, len(tx.operations))
	for _, operation := range tx.operations {
		summary, err := protocol.NewEditOperationSummary(
			protocol.NewFormatOperationId(operationID(operation.Kind), 1), map[string]string{})
		if err != nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		summaries = append(summaries, summary)
	}
	plan, err := document.NewEditPlan(sourceID, d.Profile(), summaries, commit.SourcePatch,
		commit.ChangeSet.Diagnostics())
	if err != nil {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return plan, nil
}

// operationID returns the frozen operation identifier of one operation
// kind.
func operationID(kind EditOperationKind) string {
	switch kind {
	case EditOperationSetValue:
		return "plist.edit.set-value"
	case EditOperationInsertDictEntry:
		return "plist.edit.insert-dict-entry"
	case EditOperationRemoveDictEntry:
		return "plist.edit.remove-dict-entry"
	case EditOperationRenameDictKey:
		return "plist.edit.rename-dict-key"
	case EditOperationInsertArrayElement:
		return "plist.edit.insert-array-element"
	case EditOperationRemoveArrayElement:
		return "plist.edit.remove-array-element"
	}
	return "plist.edit.set-value"
}

// ---------------------------------------------------------------------------
// Splice machinery
// ---------------------------------------------------------------------------

// appliedEdit is one applied raw-byte splice, recorded for base-coordinate
// translation (edit.rs:611-622).
type appliedEdit struct {
	// preStart is the start of the replaced span in the state immediately
	// before this splice was applied.
	preStart int
	// preLen is the length of the replaced span in that pre-state.
	preLen int
	// replacement is the replacement bytes.
	replacement []byte
	// structural marks the implementation-owned structural regions (binary
	// offset table and trailer): the fold scan never merges operation
	// content into one.
	structural bool
}

// unmapIn maps one position from the final state back to the base snapshot
// through the applied edits in reverse application order; a position inside
// an earlier replacement is an ownership overlap (edit.rs:627-644).
func unmapIn(edits []appliedEdit, position int) (int, *EditFailure) {
	for index := len(edits) - 1; index >= 0; index-- {
		edit := edits[index]
		if position <= edit.preStart {
			continue
		}
		if position < edit.preStart+len(edit.replacement) {
			baseStart, failure := unmapIn(edits[:index], edit.preStart)
			if failure != nil {
				return 0, failure
			}
			return baseStart + (position - edit.preStart), nil
		}
		position = position - len(edit.replacement) + edit.preLen
	}
	return position, nil
}

// mapIn maps one position from one pre-state to the final state through
// the applied edits in application order (edit.rs:648-659).
func mapIn(edits []appliedEdit, position int) (int, *EditFailure) {
	for _, edit := range edits {
		if position <= edit.preStart {
			continue
		}
		if position < edit.preStart+edit.preLen {
			return 0, &EditFailure{Kind: EditFailureOverlappingOwnership}
		}
		position = position + len(edit.replacement) - edit.preLen
	}
	return position, nil
}

// recordEdit records one splice and rejects two insertions that map to the
// same base position (a duplicate target). An operation whose span lies
// inside a replacement an earlier operation of this transaction wrote
// (including the exact boundaries) folds into that replacement
// (edit.rs:668-728).
func recordEdit(edits *[]appliedEdit, preStart, preLen int, replacement []byte,
	structural bool) *EditFailure {
	if preLen == 0 && len(replacement) == 0 {
		return nil
	}
	for index := len(*edits) - 1; index >= 0; index-- {
		if (*edits)[index].structural {
			continue
		}
		regionStart, failure := mapIn((*edits)[index+1:], (*edits)[index].preStart)
		if failure != nil {
			return failure
		}
		regionEnd := regionStart + len((*edits)[index].replacement)
		if preStart >= regionStart && preStart+preLen <= regionEnd &&
			!(preLen == 0 && preStart == regionEnd) {
			offset := preStart - regionStart
			merged := make([]byte, 0, len((*edits)[index].replacement)+len(replacement))
			merged = append(merged, (*edits)[index].replacement[:offset]...)
			merged = append(merged, replacement...)
			merged = append(merged, (*edits)[index].replacement[offset+preLen:]...)
			delta := len(merged) - len((*edits)[index].replacement)
			targetStart := (*edits)[index].preStart
			for later := index + 1; later < len(*edits); later++ {
				if (*edits)[later].preStart > targetStart {
					(*edits)[later].preStart += delta
				}
			}
			(*edits)[index].replacement = merged
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
	for index, previous := range *edits {
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
		preStart: preStart, preLen: preLen,
		replacement: append([]byte(nil), replacement...), structural: structural})
	return nil
}

// applySplices builds the new bytes by applying the splices sequentially
// against a working buffer (edit.rs:733-746).
func applySplices(bytes []byte, splices []appliedEdit) ([]byte, *EditFailure) {
	working := append([]byte(nil), bytes...)
	for _, splice := range splices {
		end := splice.preStart + splice.preLen
		if splice.preStart < 0 || end > len(working) {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		working = append(working[:splice.preStart],
			append(append([]byte(nil), splice.replacement...), working[end:]...)...)
	}
	return working, nil
}

// applyStep validates the target length, records every splice against the
// base coordinates, then builds the new bytes in one pass (edit.rs:581-
// 607).
func applyStep(edits *[]appliedEdit, bytes []byte, limits PlistParseLimits,
	splices []appliedEdit) ([]byte, *EditFailure) {
	targetLen := len(bytes)
	for _, splice := range splices {
		targetLen = targetLen - splice.preLen + len(splice.replacement)
		if targetLen < 0 {
			return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "target-bytes"}
		}
	}
	if targetLen > limits.Common.MaxSourceBytes {
		return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "target-bytes"}
	}
	for _, splice := range splices {
		if failure := recordEdit(edits, splice.preStart, splice.preLen,
			splice.replacement, splice.structural); failure != nil {
			return nil, failure
		}
	}
	return applySplices(bytes, splices)
}

// resolvePath resolves one path against one native arena; the empty path is
// the root (edit.rs:749-769).
func resolvePath(native *PlistDocument, path *EditPath) (PlistValueRef, *EditFailure) {
	current := native.Root()
	for _, step := range path.steps {
		value, ok := native.Get(current)
		if !ok {
			return PlistValueRef{}, &EditFailure{Kind: EditFailureTargetNotFound}
		}
		switch step.Kind {
		case "DictKey":
			dict, isDict := value.AsDict()
			if !isDict {
				return PlistValueRef{}, &EditFailure{Kind: EditFailureWrongRole}
			}
			position, failure := nthKeyPosition(dict, step.Key, step.Occurrence)
			if failure != nil {
				return PlistValueRef{}, failure
			}
			current = dict.Entries()[position].Value()
		case "ArrayIndex":
			array, isArray := value.AsArray()
			if !isArray {
				return PlistValueRef{}, &EditFailure{Kind: EditFailureWrongRole}
			}
			if step.Index >= array.Len() {
				return PlistValueRef{}, &EditFailure{Kind: EditFailureTargetNotFound}
			}
			current = array.Elements()[step.Index]
		}
	}
	return current, nil
}

// nthKeyPosition returns the source position of the occurrence-th
// association with the given key (edit.rs:771-787).
func nthKeyPosition(dict PlistDict, key PlistKey, occurrence int) (int, *EditFailure) {
	seen := 0
	for position, entry := range dict.Entries() {
		if entry.Key().Equal(key) {
			if seen == occurrence {
				return position, nil
			}
			seen++
		}
	}
	return 0, &EditFailure{Kind: EditFailureTargetNotFound}
}

// ---------------------------------------------------------------------------
// XML byte-level layout and operations
// ---------------------------------------------------------------------------

// xmlKeyLayout is one key element's byte facts of a dictionary entry
// (edit.rs:826-836).
type xmlKeyLayout struct {
	// text is the text span between `<key>` and `</key>`; for a
	// self-closing `<key/>` this is the whole tag.
	textStart, textEnd int
	// element is the full key element span.
	elementStart, elementEnd int
	// selfClosing reports whether the key element is one self-closing tag.
	selfClosing bool
}

// xmlNodeLayout is one value element's byte facts, indexed by native arena
// ordinal (edit.rs:796-824).
type xmlNodeLayout struct {
	// span is the full element span `[open tag start, close tag end)`.
	spanStart, spanEnd int
	// selfClosing reports whether the element is written as one
	// self-closing tag.
	selfClosing bool
	// openEnd is the end of the open tag (containers; the first child's
	// removal start).
	openEnd int
	// closeStart is the start of the close tag (containers; the `End`
	// insertion point).
	closeStart int
	// children are the child value ordinals: dictionary entry values and
	// array elements.
	children []int
	// keyText are the per-entry key element facts of a dictionary.
	keyText []xmlKeyLayout
	// entryStarts are the per-entry full span starts including leading
	// whitespace.
	entryStarts []int
}

// editXmlFrame is the open stack frame of the layout walk.
type editXmlFrame struct {
	kind         PlistSyntaxKind
	openStart    int
	openEnd      int
	children     []int
	keyText      []xmlKeyLayout
	entryStarts  []int
	prevValueEnd int
	pendingKey   *xmlKeyLayout
}

// xmlLayout walks the lossless pieces and assigns every value element its
// byte span in arena ordinal order (edit.rs:840-978).
func xmlLayout(formed *Document) ([]xmlNodeLayout, *EditFailure) {
	source := formed.source
	pieces := formed.xmlIndex.Pieces()
	kinds := formed.xmlKinds
	var layouts []xmlNodeLayout
	var stack []editXmlFrame
	var pendingKeyOpen *struct{ start, end int }
	openFor := func(close PlistSyntaxKind) PlistSyntaxKind {
		switch close {
		case PlistSyntaxKindDictClose:
			return PlistSyntaxKindDictOpen
		case PlistSyntaxKindArrayClose:
			return PlistSyntaxKindArrayOpen
		case PlistSyntaxKindStringClose:
			return PlistSyntaxKindStringOpen
		case PlistSyntaxKindIntegerClose:
			return PlistSyntaxKindIntegerOpen
		case PlistSyntaxKindRealClose:
			return PlistSyntaxKindRealOpen
		case PlistSyntaxKindDateClose:
			return PlistSyntaxKindDateOpen
		case PlistSyntaxKindDataClose:
			return PlistSyntaxKindDataOpen
		}
		return PlistSyntaxKindErrorRegion
	}
	for index := range pieces {
		start := pieces[index].Span().StartByte()
		end := pieces[index].Span().EndByte()
		kind := kinds[index]
		pieceText := string(source.Bytes()[start:end])
		switch kind {
		case PlistSyntaxKindKeyOpen:
			if pieceText == ">" {
				if pendingKeyOpen != nil {
					pendingKeyOpen.end = end
				}
			} else {
				pendingKeyOpen = &struct{ start, end int }{start, end}
			}
		case PlistSyntaxKindKeyClose:
			var key xmlKeyLayout
			if strings.HasSuffix(pieceText, "/>") {
				if pendingKeyOpen != nil {
					key = xmlKeyLayout{textStart: pendingKeyOpen.start, textEnd: end,
						elementStart: pendingKeyOpen.start, elementEnd: end, selfClosing: true}
				} else {
					key = xmlKeyLayout{textStart: start, textEnd: end,
						elementStart: start, elementEnd: end, selfClosing: true}
				}
			} else if pendingKeyOpen != nil {
				key = xmlKeyLayout{textStart: pendingKeyOpen.end, textEnd: start,
					elementStart: pendingKeyOpen.start, elementEnd: end}
			} else {
				key = xmlKeyLayout{textStart: start, textEnd: end,
					elementStart: start, elementEnd: end, selfClosing: true}
			}
			pendingKeyOpen = nil
			if len(stack) > 0 {
				stack[len(stack)-1].pendingKey = &key
			}
		case PlistSyntaxKindDictOpen, PlistSyntaxKindArrayOpen, PlistSyntaxKindStringOpen,
			PlistSyntaxKindIntegerOpen, PlistSyntaxKindRealOpen, PlistSyntaxKindDateOpen,
			PlistSyntaxKindDataOpen:
			if pieceText == ">" {
				if len(stack) > 0 {
					stack[len(stack)-1].openEnd = end
					stack[len(stack)-1].prevValueEnd = end
				}
			} else {
				stack = append(stack, editXmlFrame{
					kind: kind, openStart: start, openEnd: end, prevValueEnd: end})
			}
		case PlistSyntaxKindDictClose, PlistSyntaxKindArrayClose, PlistSyntaxKindStringClose,
			PlistSyntaxKindIntegerClose, PlistSyntaxKindRealClose, PlistSyntaxKindDateClose,
			PlistSyntaxKindDataClose:
			if strings.HasSuffix(pieceText, "/>") {
				if len(stack) == 0 {
					return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
				}
				frame := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				layouts = append(layouts, finalizeXMLFrame(&stack, frame, end, end, true,
					len(layouts)))
			} else if len(stack) > 0 && stack[len(stack)-1].kind == openFor(kind) {
				frame := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				layouts = append(layouts, finalizeXMLFrame(&stack, frame, start, end, false,
					len(layouts)))
			} else {
				return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
			}
		case PlistSyntaxKindTrue, PlistSyntaxKindFalse:
			switch {
			case pieceText == ">":
				if len(stack) > 0 {
					stack[len(stack)-1].openEnd = end
					stack[len(stack)-1].prevValueEnd = end
				}
			case strings.HasPrefix(pieceText, "</"):
				if len(stack) == 0 {
					return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
				}
				frame := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				layouts = append(layouts, finalizeXMLFrame(&stack, frame, start, end, false,
					len(layouts)))
			case strings.HasSuffix(pieceText, "/>"):
				if len(stack) == 0 {
					return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
				}
				frame := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				layouts = append(layouts, finalizeXMLFrame(&stack, frame, end, end, true,
					len(layouts)))
			default:
				stack = append(stack, editXmlFrame{
					kind: kind, openStart: start, openEnd: end, prevValueEnd: end})
			}
		}
	}
	return layouts, nil
}

// finalizeXMLFrame assigns the next arena ordinal to one closed frame and
// updates its parent dictionary's pending entry (edit.rs:996-1024).
func finalizeXMLFrame(stack *[]editXmlFrame, frame editXmlFrame, closeStart, closeEnd int,
	selfClosing bool, ordinal int) xmlNodeLayout {
	if len(*stack) > 0 {
		parent := &(*stack)[len(*stack)-1]
		if parent.kind == PlistSyntaxKindDictOpen && parent.pendingKey != nil {
			key := *parent.pendingKey
			parent.pendingKey = nil
			parent.keyText = append(parent.keyText, key)
			parent.entryStarts = append(parent.entryStarts, parent.prevValueEnd)
		}
		parent.children = append(parent.children, ordinal)
		parent.prevValueEnd = closeEnd
	}
	return xmlNodeLayout{
		spanStart: frame.openStart, spanEnd: closeEnd, selfClosing: selfClosing,
		openEnd: frame.openEnd, closeStart: closeStart,
		children: frame.children, keyText: frame.keyText, entryStarts: frame.entryStarts,
	}
}

// prepareXMLOperation prepares one XML operation's splices against the
// current formed state (edit.rs:1042-1223).
func prepareXMLOperation(formed *Document, layout []xmlNodeLayout, operation *EditOperation,
	encoding document.SourceEncoding) ([]appliedEdit, *EditFailure) {
	switch operation.Kind {
	case EditOperationSetValue:
		if failure := checkXMLValue(&operation.Value); failure != nil {
			return nil, failure
		}
		node, failure := resolvePath(formed.native, &operation.Path)
		if failure != nil {
			return nil, failure
		}
		nodeLayout := layout[node.index]
		replacement, failure := encodeXMLElement(&operation.Value, encoding)
		if failure != nil {
			return nil, failure
		}
		return []appliedEdit{{preStart: nodeLayout.spanStart,
			preLen: nodeLayout.spanEnd - nodeLayout.spanStart, replacement: replacement}}, nil
	case EditOperationInsertDictEntry:
		if failure := checkXMLKey(&operation.Key); failure != nil {
			return nil, failure
		}
		if failure := checkXMLValue(&operation.Value); failure != nil {
			return nil, failure
		}
		dict, failure := resolvePath(formed.native, &operation.Path)
		if failure != nil {
			return nil, failure
		}
		value, ok := formed.native.Get(dict)
		if !ok {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		if _, isDict := value.AsDict(); !isDict {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		dictLayout := layout[dict.index]
		count := len(dictLayout.children)
		markup, failure := entryMarkup(&operation.Key, &operation.Value, encoding)
		if failure != nil {
			return nil, failure
		}
		var insertAt, oldLen int
		switch {
		case operation.Placement == DictPlacementEnd:
			if dictLayout.selfClosing {
				markup = append(append([]byte("<dict>"), markup...), []byte("</dict>")...)
				insertAt = dictLayout.spanStart
				oldLen = dictLayout.spanEnd - dictLayout.spanStart
			} else {
				insertAt = dictLayout.closeStart
			}
		case operation.Placement == DictPlacementBefore:
			if operation.PlacementPosition >= count {
				return nil, &EditFailure{Kind: EditFailureTargetNotFound}
			}
			insertAt = dictLayout.entryStarts[operation.PlacementPosition]
		case operation.Placement == DictPlacementAfter:
			if operation.PlacementPosition >= count {
				return nil, &EditFailure{Kind: EditFailureTargetNotFound}
			}
			insertAt = layout[dictLayout.children[operation.PlacementPosition]].spanEnd
		}
		return []appliedEdit{{preStart: insertAt, preLen: oldLen, replacement: markup}}, nil
	case EditOperationRemoveDictEntry:
		dict, failure := resolvePath(formed.native, &operation.Path)
		if failure != nil {
			return nil, failure
		}
		value, ok := formed.native.Get(dict)
		if !ok {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		dictValue, isDict := value.AsDict()
		if !isDict {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		position, failure := nthKeyPosition(dictValue, operation.Key, operation.Occurrence)
		if failure != nil {
			return nil, failure
		}
		dictLayout := layout[dict.index]
		spanStart := dictLayout.entryStarts[position]
		spanEnd := layout[dictLayout.children[position]].spanEnd
		return []appliedEdit{{preStart: spanStart, preLen: spanEnd - spanStart}}, nil
	case EditOperationRenameDictKey:
		if failure := checkXMLKey(&operation.Key); failure != nil {
			return nil, failure
		}
		dict, failure := resolvePath(formed.native, &operation.Path)
		if failure != nil {
			return nil, failure
		}
		value, ok := formed.native.Get(dict)
		if !ok {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		dictValue, isDict := value.AsDict()
		if !isDict {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		position, failure := nthKeyPosition(dictValue, operation.From, operation.Occurrence)
		if failure != nil {
			return nil, failure
		}
		dictLayout := layout[dict.index]
		keyLayout := dictLayout.keyText[position]
		var oldStart, oldLen int
		var replacement []byte
		if keyLayout.selfClosing {
			oldStart = keyLayout.elementStart
			oldLen = keyLayout.elementEnd - keyLayout.elementStart
			replacement, failure = encodeXMLKey(&operation.Key, encoding)
		} else {
			oldStart = keyLayout.textStart
			oldLen = keyLayout.textEnd - keyLayout.textStart
			replacement, failure = encodeKeyText(&operation.Key, encoding)
		}
		if failure != nil {
			return nil, failure
		}
		return []appliedEdit{{preStart: oldStart, preLen: oldLen, replacement: replacement}}, nil
	case EditOperationInsertArrayElement:
		if failure := checkXMLValue(&operation.Value); failure != nil {
			return nil, failure
		}
		array, failure := resolvePath(formed.native, &operation.Path)
		if failure != nil {
			return nil, failure
		}
		value, ok := formed.native.Get(array)
		if !ok {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		if _, isArray := value.AsArray(); !isArray {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		arrayLayout := layout[array.index]
		count := len(arrayLayout.children)
		if operation.Index > count {
			return nil, &EditFailure{Kind: EditFailureTargetNotFound}
		}
		markup, failure := encodeXMLElement(&operation.Value, encoding)
		if failure != nil {
			return nil, failure
		}
		var insertAt, oldLen int
		replacement := markup
		switch {
		case operation.Index == count:
			if arrayLayout.selfClosing {
				replacement = append(append([]byte("<array>"), markup...), []byte("</array>")...)
				insertAt = arrayLayout.spanStart
				oldLen = arrayLayout.spanEnd - arrayLayout.spanStart
			} else {
				insertAt = arrayLayout.closeStart
			}
		case operation.Index == 0:
			insertAt = arrayLayout.openEnd
		default:
			insertAt = layout[arrayLayout.children[operation.Index]].spanStart
		}
		return []appliedEdit{{preStart: insertAt, preLen: oldLen, replacement: replacement}}, nil
	case EditOperationRemoveArrayElement:
		array, failure := resolvePath(formed.native, &operation.Path)
		if failure != nil {
			return nil, failure
		}
		value, ok := formed.native.Get(array)
		if !ok {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		if _, isArray := value.AsArray(); !isArray {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		arrayLayout := layout[array.index]
		count := len(arrayLayout.children)
		if operation.Index >= count {
			return nil, &EditFailure{Kind: EditFailureTargetNotFound}
		}
		var spanStart, spanEnd int
		if operation.Index == 0 {
			spanStart = arrayLayout.openEnd
			spanEnd = layout[arrayLayout.children[0]].spanEnd
		} else {
			spanStart = layout[arrayLayout.children[operation.Index-1]].spanEnd
			spanEnd = layout[arrayLayout.children[operation.Index]].spanEnd
		}
		return []appliedEdit{{preStart: spanStart, preLen: spanEnd - spanStart}}, nil
	}
	return nil, &EditFailure{Kind: EditFailureWrongRole}
}

// entryMarkup is `<key>..</key>` plus one value element (edit.rs:1247-1255).
func entryMarkup(key *PlistKey, value *EditValue,
	encoding document.SourceEncoding) ([]byte, *EditFailure) {
	keyMarkup, failure := encodeXMLKey(key, encoding)
	if failure != nil {
		return nil, failure
	}
	valueMarkup, failure := encodeXMLElement(value, encoding)
	if failure != nil {
		return nil, failure
	}
	return append(keyMarkup, valueMarkup...), nil
}

// encodeXMLElement writes one value element as markup (edit.rs:1258-1308).
func encodeXMLElement(value *EditValue, encoding document.SourceEncoding) ([]byte, *EditFailure) {
	var text strings.Builder
	switch value.kind {
	case EditValueString:
		unicode, err := value.str.ToUnicode()
		if err != nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		text.WriteString("<string>")
		text.WriteString(escapeXMLText(unicode))
		text.WriteString("</string>")
	case EditValueInteger:
		text.WriteString("<integer>")
		text.WriteString(itoa64(value.integer.value))
		text.WriteString("</integer>")
	case EditValueReal:
		text.WriteString("<real>")
		text.WriteString(renderReal(value.real))
		text.WriteString("</real>")
	case EditValueBoolean:
		if value.boolean.value {
			text.WriteString("<true/>")
		} else {
			text.WriteString("<false/>")
		}
	case EditValueDate:
		year, month, day, hour, minute, second, dateError := wholeSecondDate(value.date.seconds)
		if dateError != 0 {
			fact := "fractional-seconds"
			if dateError == dateRangeYearOutOfRange {
				fact = "date-year-range"
			}
			return nil, &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: fact}
		}
		text.WriteString("<date>")
		text.WriteString(renderDate(year, month, day, hour, minute, second))
		text.WriteString("</date>")
	case EditValueData:
		text.WriteString("<data>")
		text.WriteString(encodeBase64(value.data.Bytes()))
		text.WriteString("</data>")
	case EditValueUID:
		return nil, &EditFailure{Kind: EditFailureUIDInXML}
	}
	return encodeText(text.String(), encoding), nil
}

// encodeXMLKey writes one key element as markup (edit.rs:1311-1321).
func encodeXMLKey(key *PlistKey, encoding document.SourceEncoding) ([]byte, *EditFailure) {
	unicode, err := key.ToUnicode()
	if err != nil {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	var text strings.Builder
	text.WriteString("<key>")
	text.WriteString(escapeXMLText(unicode))
	text.WriteString("</key>")
	return encodeText(text.String(), encoding), nil
}

// encodeKeyText writes the escaped key content only (edit.rs:1324-1333).
func encodeKeyText(key *PlistKey, encoding document.SourceEncoding) ([]byte, *EditFailure) {
	unicode, err := key.ToUnicode()
	if err != nil {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return encodeText(escapeXMLText(unicode), encoding), nil
}

// encodeText appends one decoded string under the source encoding
// (edit.rs:1336-1350).
func encodeText(text string, encoding document.SourceEncoding) []byte {
	switch encoding.Kind() {
	case document.EncodingUtf16Le:
		units := utf16Encode(text)
		out := make([]byte, 0, len(units)*2)
		for _, unit := range units {
			out = append(out, byte(unit), byte(unit>>8))
		}
		return out
	case document.EncodingUtf16Be:
		units := utf16Encode(text)
		out := make([]byte, 0, len(units)*2)
		for _, unit := range units {
			out = append(out, byte(unit>>8), byte(unit))
		}
		return out
	}
	return []byte(text)
}

// checkXMLValue validates one typed value for the XML representation
// (edit.rs:1353-1377).
func checkXMLValue(value *EditValue) *EditFailure {
	switch value.kind {
	case EditValueString:
		return checkXMLString(&value.str)
	case EditValueReal:
		if value.real.Width() == RealWidthFloat32 {
			return &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: "float32-width"}
		}
		if !realExpressible(value.real) {
			return &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: "real-nan-payload"}
		}
		return nil
	case EditValueDate:
		_, _, _, _, _, _, dateError := wholeSecondDate(value.date.seconds)
		if dateError == dateRangeFractionalSeconds {
			return &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: "fractional-seconds"}
		}
		if dateError == dateRangeYearOutOfRange {
			return &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: "date-year-range"}
		}
		return nil
	case EditValueUID:
		return &EditFailure{Kind: EditFailureUIDInXML}
	}
	return nil
}

// checkXMLKey validates one key content for the XML representation.
func checkXMLKey(key *PlistKey) *EditFailure {
	if key.Status() == PlistStringUnpairedSurrogate {
		return &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: "unpaired-surrogate"}
	}
	if !isXMLText(key.CodeUnits()) {
		return &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: "non-xml-character"}
	}
	return nil
}

// checkXMLString validates one string content for the XML representation.
func checkXMLString(string *PlistString) *EditFailure {
	if string.Status() == PlistStringUnpairedSurrogate {
		return &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: "unpaired-surrogate"}
	}
	if !isXMLText(string.CodeUnits()) {
		return &EditFailure{Kind: EditFailureUnrepresentableValue, Fact: "non-xml-character"}
	}
	return nil
}

// commitXML is the XML byte-level commit: each operation resolves against
// the current reparse, replaces only operation-owned spans of the raw
// source, and reparses after every operation (edit.rs:507-539).
func (d *Document) commitXML(tx *EditTransaction) (*EditCommit, *EditFailure) {
	selection := PlistEncodingProfileDefault()
	if override := d.source.EncodingFacts().CallerOverride(); override != nil {
		selection = PlistEncodingExplicit(*override)
	}
	bytes := d.Render()
	var edits []appliedEdit
	var stepFailure *EditFailure
	for _, operation := range tx.operations {
		formed, failure := Parse(bytes, PlistProfileXmlV1, selection, d.limits)
		if failure != nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		if formed.status != document.FormationStatusComplete || formed.native == nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		layout, layoutFailure := xmlLayout(formed)
		if layoutFailure != nil {
			return nil, layoutFailure
		}
		encoding := formed.source.EncodingFacts().Selected()
		splices, spliceFailure := prepareXMLOperation(formed, layout, &operation, encoding)
		if spliceFailure != nil {
			return nil, spliceFailure
		}
		bytes, stepFailure = applyStep(&edits, bytes, d.limits, splices)
		if stepFailure != nil {
			return nil, stepFailure
		}
	}
	finalDocument, failure := Parse(bytes, PlistProfileXmlV1, selection, d.limits)
	if failure != nil {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	if finalDocument.status != document.FormationStatusComplete {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return buildCommit(d, tx, finalDocument, edits)
}

// ---------------------------------------------------------------------------
// Binary structural operations
// ---------------------------------------------------------------------------

// binaryPlan is one binary operation's structural changes (edit.rs:1405-
// 1419).
type binaryPlan struct {
	// refs are the final flattened reference lists per object: for a
	// dictionary the key references followed by the value references, for
	// an array the element references.
	refs [][]int
	// appended are the fresh object bytes appended after the existing
	// object table, in object-index order.
	appended [][]byte
	// scalarReplaces are the direct scalar rewrites (set-value): object
	// index to new object bytes.
	scalarReplaces map[int][]byte
	// containerTouched are the container objects whose reference blocks the
	// operation rewrites.
	containerTouched []int
}

// binaryStep computes one binary operation's structural changes against the
// current formed state (edit.rs:1422-1568). All arithmetic is checked
// before any splice exists.
func binaryStep(formed *Document, operation *EditOperation,
	limits PlistParseLimits) ([]appliedEdit, *EditFailure) {
	native := formed.native
	facts := formed.binaryFacts
	plan, failure := binaryPlanFor(native, facts, operation)
	if failure != nil {
		return nil, failure
	}
	nodeCount := native.NodeCount()
	newObjectCount := nodeCount + len(plan.appended)
	if newObjectCount > limits.MaxObjectCount {
		return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "object-count"}
	}
	currentRefSize := int(facts.trailer.objectRefSize)
	newRefSize := refSizeFor(newObjectCount)
	if newRefSize > limits.MaxObjectRefSize {
		return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "object-ref-size"}
	}
	replacements := map[int][]byte{}
	for index, bytes := range plan.scalarReplaces {
		replacements[index] = bytes
	}
	for _, index := range plan.containerTouched {
		bytes, encFailure := encodeContainer(plan.refs[index], containerIsDict(native, index), newRefSize)
		if encFailure != nil {
			return nil, encFailure
		}
		replacements[index] = bytes
	}
	if newRefSize != currentRefSize {
		for index := 0; index < nodeCount; index++ {
			if containerIsDict(native, index) || containerIsArray(native, index) {
				bytes, encFailure := encodeContainer(plan.refs[index], containerIsDict(native, index),
					newRefSize)
				if encFailure != nil {
					return nil, encFailure
				}
				replacements[index] = bytes
			}
		}
	}
	// Object spans and lengths in the current state. Every splice's pre-span
	// is expressed in its own pre-state: each later splice position shifts
	// by the length deltas of the earlier splices of this step.
	newLens := make([]int, nodeCount)
	for index := 0; index < nodeCount; index++ {
		newLens[index] = facts.objects[index].span.Len()
	}
	var splices []appliedEdit
	delta := 0
	indices := make([]int, 0, len(replacements))
	for index := range replacements {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		bytes := replacements[index]
		span := facts.objects[index].span
		newLens[index] = len(bytes)
		preStart, failure := shift(span.StartByte(), delta)
		if failure != nil {
			return nil, failure
		}
		splices = append(splices, appliedEdit{preStart: preStart,
			preLen: span.Len(), replacement: bytes})
		var ok bool
		delta, ok = addLengthDelta(delta, len(bytes), span.Len())
		if !ok {
			return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "target-bytes"}
		}
	}
	// Fresh objects append after the last object.
	objectAreaEnd := int(facts.trailer.offsetTableOffset)
	var appendedBytes []byte
	for _, bytes := range plan.appended {
		appendedBytes = append(appendedBytes, bytes...)
	}
	if len(appendedBytes) > 0 {
		preStart, failure := shift(objectAreaEnd, delta)
		if failure != nil {
			return nil, failure
		}
		splices = append(splices, appliedEdit{preStart: preStart, replacement: appendedBytes})
		var ok bool
		delta, ok = addLengthDelta(delta, len(appendedBytes), 0)
		if !ok {
			return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "target-bytes"}
		}
	}
	// New object offsets and layout.
	newOffsets := make([]int, 0, newObjectCount)
	cursor := 8
	for _, length := range newLens {
		newOffsets = append(newOffsets, cursor)
		cursor += length
	}
	for _, bytes := range plan.appended {
		newOffsets = append(newOffsets, cursor)
		cursor += len(bytes)
	}
	newTableOffset := cursor
	// Offset table.
	oldTableStart, failure := shift(objectAreaEnd, delta)
	if failure != nil {
		return nil, failure
	}
	oldTableBytes := int(facts.trailer.numObjects) * int(facts.trailer.offsetIntSize)
	offsetIntSize := refSizeFor(newTableOffset)
	if offsetIntSize > limits.MaxOffsetIntSize {
		return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "offset-int-size"}
	}
	tableBytes := newObjectCount * offsetIntSize
	if tableBytes > limits.MaxOffsetTableBytes {
		return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "offset-table-bytes"}
	}
	targetLen := newTableOffset + tableBytes + 32
	if targetLen > limits.Common.MaxSourceBytes {
		return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "target-bytes"}
	}
	var table []byte
	for _, offset := range newOffsets {
		table = writeBE(table, uint64(offset), offsetIntSize)
	}
	splices = append(splices, appliedEdit{preStart: oldTableStart, preLen: oldTableBytes,
		replacement: table, structural: true})
	var ok bool
	delta, ok = addLengthDelta(delta, len(table), oldTableBytes)
	if !ok {
		return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "target-bytes"}
	}
	// Trailer: 5 unused bytes, sortVersion 0, offsetIntSize, objectRefSize,
	// numObjects, topObject, offsetTableOffset (RFC 0013 §5.10).
	oldLen := len(formed.Render())
	var trailer []byte
	trailer = append(trailer, 0, 0, 0, 0, 0)
	trailer = append(trailer, 0) // sortVersion
	trailer = append(trailer, byte(offsetIntSize))
	trailer = append(trailer, byte(newRefSize))
	trailer = writeBE(trailer, uint64(newObjectCount), 8)
	trailer = writeBE(trailer, uint64(native.Root().index), 8)
	trailer = writeBE(trailer, uint64(newTableOffset), 8)
	trailerStart, failure := shift(oldLen, delta)
	if failure != nil {
		return nil, failure
	}
	trailerStart -= 32
	if trailerStart < 0 {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	splices = append(splices, appliedEdit{preStart: trailerStart, preLen: 32,
		replacement: trailer, structural: true})
	return splices, nil
}

// shift shifts one base position by the accumulated length delta of
// earlier splices (edit.rs:1570-1581).
func shift(base, delta int) (int, *EditFailure) {
	shifted := base + delta
	if (delta > 0 && shifted < base) || (delta < 0 && shifted > base) {
		return 0, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "target-bytes"}
	}
	return shifted, nil
}

// addLengthDelta accumulates one splice's length delta.
func addLengthDelta(delta, newLen, oldLen int) (int, bool) {
	sum := delta + newLen - oldLen
	return sum, true
}

// binaryPlanFor computes one operation's structural changes over the
// current arena (edit.rs:1595-1762).
func binaryPlanFor(native *PlistDocument, facts *BinaryFacts,
	operation *EditOperation) (*binaryPlan, *EditFailure) {
	nodeCount := native.NodeCount()
	dictCounts := make([]int, nodeCount)
	for index := 0; index < nodeCount; index++ {
		if value, ok := native.Get(PlistValueRef{index: index}); ok {
			if dict, isDict := value.AsDict(); isDict {
				dictCounts[index] = dict.Len()
			}
		}
	}
	keyRefs := make([][]int, nodeCount)
	for index := range keyRefs {
		keyRefs[index] = nil
	}
	for _, reference := range facts.refs {
		if reference.position < dictCounts[reference.owner] {
			keyRefs[reference.owner] = append(keyRefs[reference.owner], reference.target)
		}
	}
	refs := make([][]int, nodeCount)
	for index := 0; index < nodeCount; index++ {
		value, _ := native.Get(PlistValueRef{index: index})
		switch value.kind {
		case PlistValueKindDict:
			refs[index] = append([]int(nil), keyRefs[index]...)
			for _, entry := range value.dict.entries {
				refs[index] = append(refs[index], entry.value.index)
			}
		case PlistValueKindArray:
			refs[index] = make([]int, 0, len(value.array.elements))
			for _, element := range value.array.elements {
				refs[index] = append(refs[index], element.index)
			}
		default:
			refs[index] = nil
		}
	}
	switch operation.Kind {
	case EditOperationSetValue:
		target, failure := resolvePath(native, &operation.Path)
		if failure != nil {
			return nil, failure
		}
		bytes, failure := encodeBinaryValue(&operation.Value)
		if failure != nil {
			return nil, failure
		}
		return &binaryPlan{refs: refs,
			scalarReplaces: map[int][]byte{target.index: bytes}}, nil
	case EditOperationInsertDictEntry:
		dict, failure := resolvePath(native, &operation.Path)
		if failure != nil {
			return nil, failure
		}
		value, ok := native.Get(dict)
		if !ok {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		dictValue, isDict := value.AsDict()
		if !isDict {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		count := dictValue.Len()
		position := count
		switch {
		case operation.Placement == DictPlacementEnd:
			position = count
		case operation.Placement == DictPlacementBefore && operation.PlacementPosition < count:
			position = operation.PlacementPosition
		case operation.Placement == DictPlacementAfter && operation.PlacementPosition < count:
			position = operation.PlacementPosition + 1
		default:
			return nil, &EditFailure{Kind: EditFailureTargetNotFound}
		}
		keyBytes := encodeBinaryString(operation.Key.String())
		valueBytes, failure := encodeBinaryValue(&operation.Value)
		if failure != nil {
			return nil, failure
		}
		keyIndex := nodeCount
		valueIndex := nodeCount + 1
		dictRefs := refs[dict.index]
		dictRefs = append(dictRefs, 0, 0)
		copy(dictRefs[position+1:], dictRefs[position:len(dictRefs)-2])
		dictRefs[position] = keyIndex
		dictRefs[count+1+position] = valueIndex
		refs[dict.index] = dictRefs
		return &binaryPlan{refs: refs, appended: [][]byte{keyBytes, valueBytes},
			containerTouched: []int{dict.index}}, nil
	case EditOperationRemoveDictEntry:
		dict, failure := resolvePath(native, &operation.Path)
		if failure != nil {
			return nil, failure
		}
		value, ok := native.Get(dict)
		if !ok {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		dictValue, isDict := value.AsDict()
		if !isDict {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		position, failure := nthKeyPosition(dictValue, operation.Key, operation.Occurrence)
		if failure != nil {
			return nil, failure
		}
		count := dictValue.Len()
		dictRefs := refs[dict.index]
		dictRefs = append(dictRefs[:position], dictRefs[position+1:]...)
		dictRefs = append(dictRefs[:count-1+position], dictRefs[count+position:]...)
		refs[dict.index] = dictRefs
		return &binaryPlan{refs: refs, containerTouched: []int{dict.index}}, nil
	case EditOperationRenameDictKey:
		dict, failure := resolvePath(native, &operation.Path)
		if failure != nil {
			return nil, failure
		}
		value, ok := native.Get(dict)
		if !ok {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		dictValue, isDict := value.AsDict()
		if !isDict {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		position, failure := nthKeyPosition(dictValue, operation.From, operation.Occurrence)
		if failure != nil {
			return nil, failure
		}
		newKeyIndex := nodeCount
		refs[dict.index][position] = newKeyIndex
		return &binaryPlan{refs: refs,
			appended:         [][]byte{encodeBinaryString(operation.Key.String())},
			containerTouched: []int{dict.index}}, nil
	case EditOperationInsertArrayElement:
		array, failure := resolvePath(native, &operation.Path)
		if failure != nil {
			return nil, failure
		}
		value, ok := native.Get(array)
		if !ok {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		arrayValue, isArray := value.AsArray()
		if !isArray {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		count := arrayValue.Len()
		if operation.Index > count {
			return nil, &EditFailure{Kind: EditFailureTargetNotFound}
		}
		valueIndex := nodeCount
		arrayRefs := refs[array.index]
		arrayRefs = append(arrayRefs, 0)
		copy(arrayRefs[operation.Index+1:], arrayRefs[operation.Index:])
		arrayRefs[operation.Index] = valueIndex
		refs[array.index] = arrayRefs
		valueBytes, failure := encodeBinaryValue(&operation.Value)
		if failure != nil {
			return nil, failure
		}
		return &binaryPlan{refs: refs, appended: [][]byte{valueBytes},
			containerTouched: []int{array.index}}, nil
	case EditOperationRemoveArrayElement:
		array, failure := resolvePath(native, &operation.Path)
		if failure != nil {
			return nil, failure
		}
		value, ok := native.Get(array)
		if !ok {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		arrayValue, isArray := value.AsArray()
		if !isArray {
			return nil, &EditFailure{Kind: EditFailureWrongRole}
		}
		count := arrayValue.Len()
		if operation.Index >= count {
			return nil, &EditFailure{Kind: EditFailureTargetNotFound}
		}
		arrayRefs := refs[array.index]
		arrayRefs = append(arrayRefs[:operation.Index], arrayRefs[operation.Index+1:]...)
		refs[array.index] = arrayRefs
		return &binaryPlan{refs: refs, containerTouched: []int{array.index}}, nil
	}
	return nil, &EditFailure{Kind: EditFailureWrongRole}
}

// containerIsDict reports whether one object is a dictionary.
func containerIsDict(native *PlistDocument, index int) bool {
	value, ok := native.Get(PlistValueRef{index: index})
	if !ok {
		return false
	}
	_, isDict := value.AsDict()
	return isDict
}

// containerIsArray reports whether one object is an array.
func containerIsArray(native *PlistDocument, index int) bool {
	value, ok := native.Get(PlistValueRef{index: index})
	if !ok {
		return false
	}
	_, isArray := value.AsArray()
	return isArray
}

// encodeContainer encodes one container object: the sized marker and every
// reference at the given width; a dictionary writes its key references
// followed by its value references (RFC 0013 §5.9).
func encodeContainer(refs []int, isDict bool, refSize int) ([]byte, *EditFailure) {
	count := len(refs)
	if isDict {
		count = len(refs) / 2
	}
	marker := byte(0xA0)
	if isDict {
		marker = 0xD0
	}
	out := writeSizedMarker(nil, marker, count)
	for _, target := range refs {
		out = writeBE(out, uint64(target), refSize)
	}
	return out, nil
}

// encodeBinaryValue encodes one typed value as a binary object (RFC 0013
// §5).
func encodeBinaryValue(value *EditValue) ([]byte, *EditFailure) {
	switch value.kind {
	case EditValueString:
		return encodeBinaryString(value.str), nil
	case EditValueInteger:
		width := integerWidth(value.integer.value)
		out := []byte{0x10 | byte(log2Width(width))}
		return writeBE(out, uint64(value.integer.value), width), nil
	case EditValueReal:
		switch value.real.Width() {
		case RealWidthFloat64:
			out := []byte{0x23}
			return writeBE(out, value.real.Bits(), 8), nil
		case RealWidthFloat32:
			out := []byte{0x22}
			return writeBE(out, value.real.Bits(), 4), nil
		}
	case EditValueBoolean:
		if value.boolean.value {
			return []byte{0x09}, nil
		}
		return []byte{0x08}, nil
	case EditValueDate:
		out := []byte{0x33}
		return writeBE(out, math.Float64bits(value.date.seconds), 8), nil
	case EditValueData:
		out := writeSizedMarker(nil, 0x40, len(value.data.Bytes()))
		return append(out, value.data.Bytes()...), nil
	case EditValueUID:
		width := uidWidth(uint64(value.uid.value))
		out := []byte{0x80 | byte(width-1)}
		return writeBE(out, uint64(value.uid.value), width), nil
	}
	return nil, &EditFailure{Kind: EditFailureUnrepresentableValue}
}

// encodeBinaryString encodes one string object: the ASCII marker when every
// code unit is below `0x80`, else the UTF-16BE marker (RFC 0013 §5.6).
func encodeBinaryString(string PlistString) []byte {
	units := string.CodeUnits()
	allASCII := true
	for _, unit := range units {
		if unit >= 0x80 {
			allASCII = false
			break
		}
	}
	if allASCII {
		out := writeSizedMarker(nil, 0x50, len(units))
		for _, unit := range units {
			out = append(out, byte(unit))
		}
		return out
	}
	out := writeSizedMarker(nil, 0x60, len(units))
	for _, unit := range units {
		out = append(out, byte(unit>>8), byte(unit))
	}
	return out
}

// commitBinary is the binary structural commit: each operation rewrites the
// owning object bytes, appends fresh objects for new values, regenerates
// the offset table and trailer, and reparses after every operation
// (edit.rs:544-575).
func (d *Document) commitBinary(tx *EditTransaction) (*EditCommit, *EditFailure) {
	bytes := d.Render()
	var edits []appliedEdit
	var stepFailure *EditFailure
	for _, operation := range tx.operations {
		formed, failure := Parse(bytes, PlistProfileBinaryV1, PlistEncodingProfileDefault(), d.limits)
		if failure != nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		if formed.status != document.FormationStatusComplete || formed.native == nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		splices, spliceFailure := binaryStep(formed, &operation, d.limits)
		if spliceFailure != nil {
			return nil, spliceFailure
		}
		bytes, stepFailure = applyStep(&edits, bytes, d.limits, splices)
		if stepFailure != nil {
			return nil, stepFailure
		}
	}
	finalDocument, failure := Parse(bytes, PlistProfileBinaryV1, PlistEncodingProfileDefault(), d.limits)
	if failure != nil {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	if finalDocument.status != document.FormationStatusComplete {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return buildCommit(d, tx, finalDocument, edits)
}

// buildCommit builds the commit facts: ChangeSet, replayable SourcePatch,
// and the untouched-byte proof (edit.rs:1935-2033). The recorded edits are
// merged into maximal non-overlapping base runs; each run's replacement is
// the exact target bytes at its new span.
func buildCommit(base *Document, tx *EditTransaction, finalDocument *Document,
	edits []appliedEdit) (*EditCommit, *EditFailure) {
	if len(edits) > base.limits.MaxReportEvents {
		return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "report-events"}
	}
	type spanDelta struct{ start, end, delta int }
	spans := make([]spanDelta, 0, len(edits))
	for index, edit := range edits {
		oldStart, failure := unmapIn(edits[:index], edit.preStart)
		if failure != nil {
			return nil, failure
		}
		oldEnd, failure := unmapIn(edits[:index], edit.preStart+edit.preLen)
		if failure != nil {
			return nil, failure
		}
		spans = append(spans, spanDelta{start: oldStart, end: oldEnd,
			delta: len(edit.replacement) - edit.preLen})
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end < spans[j].end
	})
	var runs []spanDelta
	for _, item := range spans {
		if len(runs) > 0 {
			last := &runs[len(runs)-1]
			if item.start <= last.end {
				if item.end > last.end {
					last.end = item.end
				}
				last.delta += item.delta
				continue
			}
		}
		runs = append(runs, item)
	}
	beforeDelta := 0
	targetBytes := finalDocument.Render()
	var sourceEdits []document.SourceEdit
	for _, run := range runs {
		targetStart := run.start + beforeDelta
		runLen := (run.end - run.start) + run.delta
		targetEnd := targetStart + runLen
		if targetEnd > len(targetBytes) {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		oldSpan, err := base.authority.Span(run.start, run.end)
		if err != nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		newSpan, err := finalDocument.authority.Span(targetStart, targetEnd)
		if err != nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		sourceEdits = append(sourceEdits, document.SourceEdit{
			OldSpan: oldSpan, NewSpan: newSpan,
			Replacement: append([]byte(nil), targetBytes[targetStart:targetEnd]...),
		})
		beforeDelta += run.delta
	}
	changeSet := document.NewChangeSet(base.SnapshotIdentity(), finalDocument.SnapshotIdentity(),
		sourceEdits, buildMappings(base, tx, finalDocument), nil)
	patchLimits := sourcePatchLimits(base.limits)
	replacements := make([]document.SourceReplacement, 0, len(sourceEdits))
	for _, edit := range sourceEdits {
		original := base.source.Bytes()[edit.OldSpan.StartByte():edit.OldSpan.EndByte()]
		replacements = append(replacements, document.NewSourceReplacement(
			edit.OldSpan.StartByte(), edit.OldSpan.EndByte(), original, edit.Replacement))
	}
	patch, patchErr := document.NewSourcePatch(base.source, replacements,
		operationMetadata(tx), patchLimits)
	if patchErr != nil || !patch.TargetDigest().Equal(finalDocument.source.Digest()) {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	proof, proofErr := document.CreateUntouchedByteProof(base.source, finalDocument.source,
		patch.Replacements())
	if proofErr != nil {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return &EditCommit{
		Document: finalDocument, ChangeSet: changeSet,
		SourcePatch: patch, UntouchedProof: &proof,
	}, nil
}

// buildMappings builds one old-to-new mapping per operation whose target
// resolves in the base snapshot; insertions carry no mapping (edit.rs:2037-
// 2065).
func buildMappings(base *Document, tx *EditTransaction,
	finalDocument *Document) []document.NodeMapping {
	var mappings []document.NodeMapping
	for _, operation := range tx.operations {
		var old PlistValueRef
		var newRef *PlistValueRef
		status := protocol.MappingReplaced
		switch operation.Kind {
		case EditOperationSetValue, EditOperationRenameDictKey:
			resolved, failure := resolvePath(base.native, &operation.Path)
			if failure != nil {
				continue
			}
			old = resolved
			if final, failure := resolvePath(finalDocument.native, &operation.Path); failure == nil {
				newRef = &final
			} else {
				status = protocol.MappingUnmapped
			}
		case EditOperationRemoveDictEntry:
			container, failure := resolvePath(base.native, &operation.Path)
			if failure != nil {
				continue
			}
			value, ok := base.native.Get(container)
			if !ok {
				continue
			}
			dict, isDict := value.AsDict()
			if !isDict {
				continue
			}
			position, failure := nthKeyPosition(dict, operation.Key, operation.Occurrence)
			if failure != nil {
				continue
			}
			old = dict.Entries()[position].Value()
			status = protocol.MappingDeleted
		case EditOperationRemoveArrayElement:
			container, failure := resolvePath(base.native, &operation.Path)
			if failure != nil {
				continue
			}
			value, ok := base.native.Get(container)
			if !ok {
				continue
			}
			array, isArray := value.AsArray()
			if !isArray || operation.Index >= array.Len() {
				continue
			}
			old = array.Elements()[operation.Index]
			status = protocol.MappingDeleted
		default:
			continue
		}
		var newHandle *document.NodeRef
		if newRef != nil {
			handle := finalDocument.nodeRef(newRef.index, document.RolePlistValue)
			newHandle = &handle
		}
		var reason *string
		if status == protocol.MappingUnmapped {
			text := "reparsed-node-not-uniquely-located"
			reason = &text
		}
		mappings = append(mappings, document.NodeMapping{
			Old: base.nodeRef(old.index, document.RolePlistValue),
			New: newHandle, Status: status, Reason: reason,
		})
	}
	return mappings
}

// sourcePatchLimits derives the patch construction bounds from the parse
// limits (edit.rs:2097-2107).
func sourcePatchLimits(limits PlistParseLimits) document.SourcePatchLimits {
	patchLimits := document.DefaultSourcePatchLimits()
	patchLimits.Source = document.SourceLimits{
		MaxRawBytes:         limits.Common.MaxSourceBytes,
		MaxDecodedUTF8Bytes: limits.MaxDecodedUTF8Bytes,
		MaxDecodedScalars:   limits.MaxDecodedScalars,
	}
	patchLimits.MaxReplacements = limits.MaxReportEvents
	if patchLimits.MaxReplacements < 1 {
		patchLimits.MaxReplacements = 1
	}
	patchLimits.MaxPatchBytes = limits.Common.MaxSourceBytes * 2
	return patchLimits
}

// operationMetadata builds the deterministic audit metadata of one
// transaction.
func operationMetadata(tx *EditTransaction) map[string]string {
	metadata := map[string]string{}
	for index, operation := range tx.operations {
		metadata["operation."+itoa(index)] =
			operationID(operation.Kind) + "@1"
	}
	return metadata
}
