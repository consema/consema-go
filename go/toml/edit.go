package toml

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// AssociationPlacement is the explicit association placement of one
// insertion operation (document.AssociationPlacement; consema-document
// AssociationPlacement; RFC 0004).
type AssociationPlacement = document.AssociationPlacement

// PlacementStart inserts at the container start.
func PlacementStart() AssociationPlacement { return document.PlacementAtStart() }

// PlacementEnd inserts at the container end.
func PlacementEnd() AssociationPlacement { return document.PlacementAtEnd() }

// PlacementBefore inserts immediately before one exact association.
func PlacementBefore(anchor document.NodeRef) AssociationPlacement {
	return document.BeforeAnchor(anchor)
}

// PlacementAfter inserts immediately after one exact association.
func PlacementAfter(anchor document.NodeRef) AssociationPlacement {
	return document.AfterAnchor(anchor)
}

// RepresentationPolicy is the explicit semantic scalar representation
// policy (consema-toml edit.rs:15-26).
type RepresentationPolicy uint8

// The four frozen representation policies.
const (
	// RepresentationExactLiteral requires an exact literal operation.
	RepresentationExactLiteral RepresentationPolicy = iota
	// RepresentationPreserveCompatible retains the target native scalar
	// category.
	RepresentationPreserveCompatible
	// RepresentationCanonicalForProfile uses the frozen deterministic TOML
	// 1.0 scalar representation.
	RepresentationCanonicalForProfile
	// RepresentationPreserveElseCanonical preserves the category when
	// compatible and otherwise reports a canonical fallback.
	RepresentationPreserveElseCanonical
)

// ScalarReplacement is one scalar operation bound to a transaction base
// snapshot (consema-toml edit.rs:28-55).
type ScalarReplacement struct {
	// Target is the exact TOML item target.
	Target document.NodeRef
	// Value is the new complete core scalar of Semantic replacements.
	Value core.Value
	// Policy is the representation contract of Semantic replacements.
	Policy RepresentationPolicy
	// Literal is the exact candidate scalar bytes of Literal
	// replacements.
	Literal []byte
	// IsLiteral reports a Literal replacement.
	IsLiteral bool
}

// EditOperation is one typed TOML edit operation bound to an immutable
// base snapshot (consema-toml edit.rs:57-99).
type EditOperation struct {
	// Kind is the operation variant name.
	Kind string
	// Scalar is the ReplaceScalar operation.
	Scalar ScalarReplacement
	// Table is the exact table item target of InsertEntry operations.
	Table document.NodeRef
	// Key is the decoded direct key segment of InsertEntry and RenameEntry
	// operations.
	Key string
	// Value is the complete inserted value of InsertEntry and
	// InsertArrayElement operations.
	Value core.Value
	// Placement is the explicit association placement of InsertEntry and
	// InsertArrayElement operations.
	Placement AssociationPlacement
	// Target is the exact entry or array-element identity of
	// RemoveEntry, RenameEntry, and RemoveArrayElement operations.
	Target document.NodeRef
	// Array is the exact Array item target of InsertArrayElement
	// operations.
	Array document.NodeRef
}

// EditTransaction is the immutable transaction; every operation resolves
// against one base snapshot (consema-toml edit.rs:101-120).
type EditTransaction struct {
	base       document.SnapshotIdentity
	operations []EditOperation
}

// BaseSnapshot returns the base snapshot identity.
func (t *EditTransaction) BaseSnapshot() document.SnapshotIdentity { return t.base }

// Operations returns the ordered declared operations.
func (t *EditTransaction) Operations() []EditOperation {
	return append([]EditOperation(nil), t.operations...)
}

// EditTransactionBuilder incrementally binds operations to one immutable
// base document (consema-toml edit.rs:122-227).
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
func (b *EditTransactionBuilder) SemanticScalar(target document.NodeRef,
	value core.Value, policy RepresentationPolicy) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind:   "ReplaceScalar",
		Scalar: ScalarReplacement{Target: target, Value: value, Policy: policy},
	})
	return b
}

// LiteralScalar adds an exact TOML scalar literal replacement.
func (b *EditTransactionBuilder) LiteralScalar(target document.NodeRef,
	literal []byte) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: "ReplaceScalar",
		Scalar: ScalarReplacement{Target: target, Literal: append([]byte(nil), literal...),
			IsLiteral: true},
	})
	return b
}

// InsertEntry adds one direct TOML table entry insertion.
func (b *EditTransactionBuilder) InsertEntry(table document.NodeRef, key string,
	value core.Value, placement AssociationPlacement) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: "InsertEntry", Table: table, Key: key, Value: value, Placement: placement,
	})
	return b
}

// RemoveEntry adds one exact TOML table entry removal.
func (b *EditTransactionBuilder) RemoveEntry(target document.NodeRef) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{Kind: "RemoveEntry", Target: target})
	return b
}

// RenameEntry adds one exact TOML direct key rename.
func (b *EditTransactionBuilder) RenameEntry(target document.NodeRef,
	key string) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{Kind: "RenameEntry", Target: target, Key: key})
	return b
}

// InsertArrayElement adds one TOML array element insertion.
func (b *EditTransactionBuilder) InsertArrayElement(array document.NodeRef,
	value core.Value, placement AssociationPlacement) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: "InsertArrayElement", Array: array, Value: value, Placement: placement,
	})
	return b
}

// RemoveArrayElement adds one exact TOML array element removal.
func (b *EditTransactionBuilder) RemoveArrayElement(target document.NodeRef) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{Kind: "RemoveArrayElement", Target: target})
	return b
}

// Build completes the immutable request; target validation occurs
// atomically at commit.
func (b *EditTransactionBuilder) Build() *EditTransaction {
	return &EditTransaction{base: b.base, operations: append([]EditOperation(nil), b.operations...)}
}

// EditCommit is the atomic edit success (consema-toml edit.rs:230-240).
type EditCommit struct {
	// Document is the new immutable document.
	Document *Document
	// ChangeSet is the complete old-to-new change facts.
	ChangeSet ChangeSet
	// SourcePatch is the portable exact raw-byte application fact.
	SourcePatch *document.SourcePatch
	// UntouchedProof is the verifiable evidence for every byte outside the
	// replacement set.
	UntouchedProof UntouchedByteProof
}

// ChangeSet is the complete old-to-new change facts
// (document.ChangeSet; consema-document change_set.rs).
type ChangeSet = document.ChangeSet

// SourceEdit is one raw-byte source edit (document.SourceEdit;
// consema-document source_patch.rs SourceEdit).
type SourceEdit = document.SourceEdit

// NodeMappingStatus is the closed node-mapping status
// (protocol.NodeMappingStatus; the six frozen values of the shared
// contract).
type NodeMappingStatus = protocol.NodeMappingStatus

// The node-mapping statuses published by the TOML edit surface.
const (
	// NodeMappingReplaced maps the old node to a reparsed result node.
	NodeMappingReplaced = protocol.MappingReplaced
	// NodeMappingDeleted reports the old node was deleted.
	NodeMappingDeleted = protocol.MappingDeleted
	// NodeMappingUnmapped reports the old node has no result identity.
	NodeMappingUnmapped = protocol.MappingUnmapped
)

// NodeMapping is one old-to-new node identity fact (document.NodeMapping).
type NodeMapping = document.NodeMapping

// EditFailureKind classifies a stable edit validation or commit failure
// (consema-toml edit.rs:242-279).
type EditFailureKind uint8

// The stable edit failure classes.
const (
	EditFailureWrongSnapshot EditFailureKind = iota
	EditFailureWrongRole
	EditFailureUnsupportedSemanticValue
	EditFailureInvalidLiteral
	EditFailureRepresentationIncompatible
	EditFailureExactLiteralRequiresLiteralOperation
	EditFailureConflictingEdits
	EditFailureDuplicateTarget
	EditFailureOverlappingOwnership
	EditFailureAncestorDescendantConflict
	EditFailurePlacementAnchorRemoved
	EditFailureTargetNotFound
	EditFailureDuplicateKey
	EditFailureUnsupportedOperation
	EditFailureUnrepresentableValue
	EditFailureResourceLimit
	EditFailureNewDocumentFormationFailed
)

// EditFailure is the typed edit validation or commit failure. It
// implements error and the RFC 0016 §6 Code() contract.
type EditFailure struct {
	// Kind identifies the failure.
	Kind EditFailureKind
	// ValueKind is the offending PortableValue kind name of
	// UnsupportedSemanticValue and UnrepresentableValue failures.
	ValueKind string
	// LimitName is the stable limit name of ResourceLimit failures.
	LimitName string
}

// Code returns the frozen registered code (consema-toml edit.rs:1308-1331).
func (f *EditFailure) Code() string {
	switch f.Kind {
	case EditFailureWrongSnapshot:
		return "core.edit.wrong-snapshot@1"
	case EditFailureWrongRole:
		return "core.edit.wrong-role@1"
	case EditFailureUnsupportedSemanticValue, EditFailureUnrepresentableValue:
		return "core.edit.unsupported-value@1"
	case EditFailureInvalidLiteral:
		return "core.edit.invalid-literal@1"
	case EditFailureRepresentationIncompatible:
		return "core.edit.representation-incompatible@1"
	case EditFailureExactLiteralRequiresLiteralOperation:
		return "core.edit.exact-literal-requires-literal@1"
	case EditFailureConflictingEdits, EditFailureDuplicateTarget,
		EditFailureOverlappingOwnership, EditFailureAncestorDescendantConflict,
		EditFailurePlacementAnchorRemoved:
		return "core.edit.conflicting-edits@1"
	case EditFailureTargetNotFound:
		return "core.edit.target-not-found@1"
	case EditFailureDuplicateKey:
		return "core.edit.duplicate-key@1"
	case EditFailureUnsupportedOperation:
		return "core.edit.operation-unsupported@1"
	case EditFailureResourceLimit:
		return "core.edit.resource-limit@1"
	case EditFailureNewDocumentFormationFailed:
		return "core.edit.formation-failed@1"
	}
	return "core.edit.precondition-failed@1"
}

// Error implements error; the text is human presentation only.
func (f *EditFailure) Error() string {
	return "toml: " + f.Name()
}

// Name returns the stable failure name mirrored from the Rust EditFailure
// variant (the vector failure facts compare these names).
func (f *EditFailure) Name() string {
	switch f.Kind {
	case EditFailureWrongSnapshot:
		return "WrongSnapshot"
	case EditFailureWrongRole:
		return "WrongRole"
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
	case EditFailureTargetNotFound:
		return "TargetNotFound"
	case EditFailureDuplicateKey:
		return "DuplicateKey"
	case EditFailureUnsupportedOperation:
		return "UnsupportedOperation"
	case EditFailureUnrepresentableValue:
		return "UnrepresentableValue"
	case EditFailureResourceLimit:
		return "ResourceLimit"
	case EditFailureNewDocumentFormationFailed:
		return "NewDocumentFormationFailed"
	}
	return "EditFailure"
}

// Commit atomically commits scalar and structural operations; a failure
// never changes the base snapshot (consema-toml edit.rs:281-430).
func (d *Document) Commit(transaction *EditTransaction) (*EditCommit, *EditFailure) {
	if transaction.base != d.SnapshotIdentity() {
		return nil, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if failure := validateDependencies(transaction); failure != nil {
		return nil, failure
	}
	var diagnostics []*protocol.Diagnostic
	prepared := make([]preparedEdit, 0, len(transaction.operations))
	for index := range transaction.operations {
		edits, failure := d.prepareOperation(&transaction.operations[index], &diagnostics)
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
	}
	if targetLen > d.limits.MaxSourceBytes {
		return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "target-bytes"}
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

	delta := 0
	sourceEdits := make([]SourceEdit, 0, len(prepared))
	mappings := make([]NodeMapping, 0, len(transaction.operations))
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
		if edit.mapping != nil && !mappedOld[edit.mapping.old] {
			mappedOld[edit.mapping.old] = true
			mapping := NodeMapping{Old: edit.mapping.old, Status: NodeMappingUnmapped}
			switch edit.mapping.plan {
			case mappingPlanReplacedLiteral:
				mapping.Status = NodeMappingReplaced
				if found, ok := findItemBySpan(newDocument, newStart, newEnd); ok {
					node := newDocument.nodeRef(found, document.RoleTomlItem)
					mapping.New = &node
				} else {
					reason := "reparsed-item-not-uniquely-located"
					mapping.Reason = &reason
				}
			case mappingPlanDeleted:
				mapping.Status = NodeMappingDeleted
			case mappingPlanUnmapped:
				reason := edit.mapping.reason
				mapping.Reason = &reason
			}
			mappings = append(mappings, mapping)
		}
		delta += len(edit.replacement) - edit.oldSpan.Len()
	}
	changeSet := document.NewChangeSet(d.SnapshotIdentity(), newDocument.SnapshotIdentity(),
		sourceEdits, mappings, diagnostics)
	patchLimits := sourcePatchLimits(d.limits, len(sourceEdits))
	sourcePatch, err := document.NewSourcePatch(d.source, sourcePatchReplacements(source, sourceEdits),
		operationMetadata(transaction), patchLimits)
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

// sourcePatchReplacements converts the source edits into patch
// replacements (original bytes included).
func sourcePatchReplacements(source []byte, edits []SourceEdit) []document.SourceReplacement {
	replacements := make([]document.SourceReplacement, 0, len(edits))
	for _, edit := range edits {
		replacements = append(replacements, document.NewSourceReplacement(
			edit.OldSpan.StartByte(), edit.OldSpan.EndByte(),
			source[edit.OldSpan.StartByte():edit.OldSpan.EndByte()], edit.Replacement))
	}
	return replacements
}

// DryRun fully validates and plans an edit without returning a new
// Document (consema-toml edit.rs:432-447).
func (d *Document) DryRun(transaction *EditTransaction,
	sourceID EditPlanSourceId) (*EditPlan, *EditFailure) {
	commit, failure := d.Commit(transaction)
	if failure != nil {
		return nil, failure
	}
	summaries, failure := operationSummaries(transaction)
	if failure != nil {
		return nil, failure
	}
	plan, err := NewEditPlan(sourceID, d.Profile(), summaries,
		commit.SourcePatch, commit.ChangeSet.Diagnostics())
	if err != nil {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return plan, nil
}

func (d *Document) prepareOperation(operation *EditOperation,
	diagnostics *[]*protocol.Diagnostic) ([]preparedEdit, *EditFailure) {
	switch operation.Kind {
	case "ReplaceScalar":
		edit, failure := d.prepareScalar(operation.Scalar, diagnostics)
		if failure != nil {
			return nil, failure
		}
		return []preparedEdit{edit}, nil
	case "InsertEntry":
		return d.prepareInsertEntry(operation.Table, operation.Key, operation.Value, operation.Placement)
	case "RemoveEntry":
		edit, failure := d.prepareRemoveEntry(operation.Target)
		if failure != nil {
			return nil, failure
		}
		return edit, nil
	case "RenameEntry":
		edit, failure := d.prepareRenameEntry(operation.Target, operation.Key)
		if failure != nil {
			return nil, failure
		}
		return []preparedEdit{edit}, nil
	case "InsertArrayElement":
		return d.prepareInsertArrayElement(operation.Array, operation.Value, operation.Placement)
	case "RemoveArrayElement":
		edit, failure := d.prepareRemoveArrayElement(operation.Target)
		if failure != nil {
			return nil, failure
		}
		return edit, nil
	}
	return nil, &EditFailure{Kind: EditFailureWrongRole}
}

type mappingPlanKind uint8

const (
	mappingPlanReplacedLiteral mappingPlanKind = iota
	mappingPlanDeleted
	mappingPlanUnmapped
)

type preparedEdit struct {
	oldSpan     document.Span
	replacement []byte
	mapping     *mappingRef
}

type mappingRef struct {
	old    document.NodeRef
	plan   mappingPlanKind
	reason string
}

func (d *Document) prepareScalar(operation ScalarReplacement,
	diagnostics *[]*protocol.Diagnostic) (preparedEdit, *EditFailure) {
	index, failure := d.resolveTarget(operation.Target, document.RoleTomlItem)
	if failure != nil {
		return preparedEdit{}, failure
	}
	oldKind := d.itemEntity(index).publicKind()
	if !isScalarKind(oldKind) {
		return preparedEdit{}, &EditFailure{Kind: EditFailureWrongRole}
	}
	var replacement []byte
	if operation.IsLiteral {
		if failure := validateExactScalar(operation.Literal); failure != nil {
			return preparedEdit{}, failure
		}
		replacement = append([]byte(nil), operation.Literal...)
	} else {
		literal, failure := semanticLiteral(operation.Value, oldKind, operation.Policy,
			d.entities[index].span, diagnostics)
		if failure != nil {
			return preparedEdit{}, failure
		}
		replacement = literal
	}
	return preparedEdit{
		oldSpan:     d.entities[index].span,
		replacement: replacement,
		mapping:     &mappingRef{old: operation.Target, plan: mappingPlanReplacedLiteral},
	}, nil
}

func (d *Document) prepareInsertEntry(table document.NodeRef, key string,
	value core.Value, placement AssociationPlacement) ([]preparedEdit, *EditFailure) {
	tableIndex, failure := d.resolveTarget(table, document.RoleTomlItem)
	if failure != nil {
		return nil, failure
	}
	item := &d.entities[tableIndex].item
	var entries []int
	switch item.kind {
	case itemTable:
		entries = item.entries
	case itemInlineTable:
		entries = item.entries
	default:
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	kind := item.publicKind()
	switch kind {
	case ItemKindRootTable, ItemKindStandardTable, ItemKindInlineTable:
	default:
		return nil, &EditFailure{Kind: EditFailureUnsupportedOperation}
	}
	for _, entryIndex := range entries {
		if d.entryName(entryIndex) == key {
			return nil, &EditFailure{Kind: EditFailureDuplicateKey}
		}
	}
	fragment := append(canonicalStringBytes(key), []byte(" = ")...)
	valueFragment, failure := d.fragment(value)
	if failure != nil {
		return nil, failure
	}
	fragment = append(fragment, valueFragment...)
	if kind == ItemKindInlineTable {
		prepared, failure := d.prepareDelimitedInsertion(tableIndex,
			d.entities[tableIndex].span, entries,
			delimitedSyntax{anchorRole: document.RoleTomlEntry,
				open: SyntaxKindLeftBrace, close: SyntaxKindRightBrace},
			placement, fragment)
		if failure != nil {
			return nil, failure
		}
		return []preparedEdit{prepared}, nil
	}
	prepared, failure := d.prepareTableLineInsertion(tableIndex, entries, placement, fragment)
	if failure != nil {
		return nil, failure
	}
	return []preparedEdit{prepared}, nil
}

func (d *Document) prepareInsertArrayElement(array document.NodeRef,
	value core.Value, placement AssociationPlacement) ([]preparedEdit, *EditFailure) {
	index, failure := d.resolveTarget(array, document.RoleTomlItem)
	if failure != nil {
		return nil, failure
	}
	item := &d.entities[index].item
	if item.kind != itemArray {
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	fragment, failure := d.fragment(value)
	if failure != nil {
		return nil, failure
	}
	prepared, failure := d.prepareDelimitedInsertion(index,
		d.entities[index].span, item.elements,
		delimitedSyntax{anchorRole: document.RoleTomlArrayElement,
			open: SyntaxKindLeftBracket, close: SyntaxKindRightBracket},
		placement, fragment)
	if failure != nil {
		return nil, failure
	}
	return []preparedEdit{prepared}, nil
}

type delimitedSyntax struct {
	anchorRole document.NodeRole
	open       TomlSyntaxKind
	close      TomlSyntaxKind
}

// prepareDelimitedInsertion computes the insertion position and comma
// framing for inline containers (consema-toml edit.rs:585-656).
func (d *Document) prepareDelimitedInsertion(containerIndex int,
	containerSpan document.Span, associations []int, syntax delimitedSyntax,
	placement AssociationPlacement, fragment []byte) (preparedEdit, *EditFailure) {
	var position int
	prefixComma := false
	suffixComma := false
	if len(associations) == 0 {
		switch placement.Kind() {
		case document.PlacementStart:
			delimiter, failure := d.delimiter(syntax.open, containerSpan, false)
			if failure != nil {
				return preparedEdit{}, failure
			}
			position = delimiter.EndByte()
		case document.PlacementEnd:
			delimiter, failure := d.delimiter(syntax.close, containerSpan, true)
			if failure != nil {
				return preparedEdit{}, failure
			}
			position = delimiter.StartByte()
		default:
			return preparedEdit{}, &EditFailure{Kind: EditFailureTargetNotFound}
		}
	} else {
		switch placement.Kind() {
		case document.PlacementStart:
			position = d.entities[associations[0]].span.StartByte()
			suffixComma = true
		case document.PlacementEnd:
			position = d.entities[associations[len(associations)-1]].span.EndByte()
			prefixComma = true
		case document.PlacementBefore:
			anchor, failure := d.resolveAnchor(placement.Anchor(), syntax.anchorRole, associations)
			if failure != nil {
				return preparedEdit{}, failure
			}
			position = d.entities[anchor].span.StartByte()
			suffixComma = true
		case document.PlacementAfter:
			anchor, failure := d.resolveAnchor(placement.Anchor(), syntax.anchorRole, associations)
			if failure != nil {
				return preparedEdit{}, failure
			}
			position = d.entities[anchor].span.EndByte()
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
	emptySpan, err := d.authority.Span(position, position)
	if err != nil {
		return preparedEdit{}, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return preparedEdit{
		oldSpan: emptySpan, replacement: replacement,
		mapping: &mappingRef{old: d.nodeRef(containerIndex, document.RoleTomlItem),
			plan:   mappingPlanUnmapped,
			reason: "container-reparsed-after-structural-insertion"},
	}, nil
}

// prepareTableLineInsertion computes the line-oriented insertion position
// for root and standard tables (consema-toml edit.rs:658-699).
func (d *Document) prepareTableLineInsertion(tableIndex int, entries []int,
	placement AssociationPlacement, fragment []byte) (preparedEdit, *EditFailure) {
	kind := d.itemEntity(tableIndex).publicKind()
	var position int
	switch placement.Kind() {
	case document.PlacementStart:
		if kind == ItemKindRootTable {
			position = 0
		} else {
			position = d.firstLineAfterHeader(d.entities[tableIndex].span)
		}
	case document.PlacementEnd:
		position = d.tableEndInsertion(entries, tableIndex)
	case document.PlacementBefore:
		anchor, failure := d.resolveAnchor(placement.Anchor(), document.RoleTomlEntry, entries)
		if failure != nil {
			return preparedEdit{}, failure
		}
		position = d.lineStart(d.entities[anchor].span.StartByte())
	case document.PlacementAfter:
		anchor, failure := d.resolveAnchor(placement.Anchor(), document.RoleTomlEntry, entries)
		if failure != nil {
			return preparedEdit{}, failure
		}
		if isTableKind(d.entryItemKind(anchor)) {
			return preparedEdit{}, &EditFailure{Kind: EditFailureUnsupportedOperation}
		}
		position = d.lineAfter(d.entities[anchor].span.EndByte())
	}
	emptySpan, err := d.authority.Span(position, position)
	if err != nil {
		return preparedEdit{}, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	replacement, failure := d.lineFragment(position, fragment)
	if failure != nil {
		return preparedEdit{}, failure
	}
	return preparedEdit{
		oldSpan: emptySpan, replacement: replacement,
		mapping: &mappingRef{old: d.nodeRef(tableIndex, document.RoleTomlItem),
			plan:   mappingPlanUnmapped,
			reason: "table-reparsed-after-entry-insertion"},
	}, nil
}

func (d *Document) prepareRemoveEntry(target document.NodeRef) ([]preparedEdit, *EditFailure) {
	index, failure := d.resolveTarget(target, document.RoleTomlEntry)
	if failure != nil {
		return nil, failure
	}
	if isTableKind(d.entryItemKind(index)) {
		return nil, &EditFailure{Kind: EditFailureUnsupportedOperation}
	}
	container, entries, ordinal, ok := d.parentTable(index)
	if !ok {
		return nil, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	switch d.itemEntity(container).publicKind() {
	case ItemKindInlineTable:
		return d.prepareDelimitedRemoval(index, entries, ordinal,
			d.entities[container].span.EndByte(), target)
	case ItemKindRootTable, ItemKindStandardTable:
		return []preparedEdit{{oldSpan: d.entities[index].span,
			mapping: &mappingRef{old: target, plan: mappingPlanDeleted}}}, nil
	default:
		return nil, &EditFailure{Kind: EditFailureUnsupportedOperation}
	}
}

func (d *Document) prepareRemoveArrayElement(target document.NodeRef) ([]preparedEdit, *EditFailure) {
	index, failure := d.resolveTarget(target, document.RoleTomlArrayElement)
	if failure != nil {
		return nil, failure
	}
	container, elements, ordinal, ok := d.parentArray(index)
	if !ok {
		return nil, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return d.prepareDelimitedRemoval(index, elements, ordinal,
		d.entities[container].span.EndByte(), target)
}

// prepareDelimitedRemoval removes one element and its adjacent comma
// (consema-toml edit.rs:743-791).
func (d *Document) prepareDelimitedRemoval(index int, associations []int,
	ordinal int, containerEnd int, target document.NodeRef) ([]preparedEdit, *EditFailure) {
	targetSpan := d.entities[index].span
	edits := make([]preparedEdit, 0, 2)
	if comma, ok, failure := d.removalComma(associations, ordinal, containerEnd); failure != nil {
		return nil, failure
	} else if ok {
		if comma.EndByte() == targetSpan.StartByte() || comma.StartByte() == targetSpan.EndByte() {
			merged, err := d.authority.Span(
				minInt(comma.StartByte(), targetSpan.StartByte()),
				maxInt(comma.EndByte(), targetSpan.EndByte()))
			if err != nil {
				return nil, &EditFailure{Kind: EditFailureTargetNotFound}
			}
			return []preparedEdit{{oldSpan: merged,
				mapping: &mappingRef{old: target, plan: mappingPlanDeleted}}}, nil
		}
		edits = append(edits, preparedEdit{
			oldSpan: targetSpan,
			mapping: &mappingRef{old: target, plan: mappingPlanDeleted},
		})
		edits = append(edits, preparedEdit{oldSpan: comma})
		return edits, nil
	}
	edits = append(edits, preparedEdit{
		oldSpan: targetSpan,
		mapping: &mappingRef{old: target, plan: mappingPlanDeleted},
	})
	return edits, nil
}

func (d *Document) prepareRenameEntry(target document.NodeRef,
	key string) (preparedEdit, *EditFailure) {
	index, failure := d.resolveTarget(target, document.RoleTomlEntry)
	if failure != nil {
		return preparedEdit{}, failure
	}
	if isTableKind(d.entryItemKind(index)) {
		return preparedEdit{}, &EditFailure{Kind: EditFailureUnsupportedOperation}
	}
	container, entries, _, ok := d.parentTable(index)
	if !ok {
		return preparedEdit{}, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	switch d.itemEntity(container).publicKind() {
	case ItemKindRootTable, ItemKindStandardTable, ItemKindInlineTable:
	default:
		return preparedEdit{}, &EditFailure{Kind: EditFailureUnsupportedOperation}
	}
	for _, candidate := range entries {
		if candidate != index && d.entryName(candidate) == key {
			return preparedEdit{}, &EditFailure{Kind: EditFailureDuplicateKey}
		}
	}
	entry := &d.entities[index].entry
	return preparedEdit{
		oldSpan:     d.entities[entry.key].span,
		replacement: canonicalStringBytes(key),
		mapping: &mappingRef{old: target,
			plan: mappingPlanUnmapped, reason: "entry-reparsed-after-key-rename"},
	}, nil
}

func (d *Document) resolveTarget(target document.NodeRef, role document.NodeRole) (int, *EditFailure) {
	if target.Snapshot() != d.SnapshotIdentity() {
		return 0, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if target.Role() != role {
		return 0, &EditFailure{Kind: EditFailureWrongRole}
	}
	index, err := d.validateRef(target, role)
	if err != nil {
		switch err.(*TomlAccessError).Kind {
		case TomlAccessWrongSnapshot:
			return 0, &EditFailure{Kind: EditFailureWrongSnapshot}
		case TomlAccessWrongRole:
			return 0, &EditFailure{Kind: EditFailureWrongRole}
		default:
			return 0, &EditFailure{Kind: EditFailureTargetNotFound}
		}
	}
	return index, nil
}

func (d *Document) resolveAnchor(anchor document.NodeRef, role document.NodeRole,
	associations []int) (int, *EditFailure) {
	index, failure := d.resolveTarget(anchor, role)
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

func (d *Document) fragment(value core.Value) ([]byte, *EditFailure) {
	limits := document.MaterializationLimits{
		MaxInputNodes:        d.limits.MaxNodeCount,
		MaxOutputBytes:       d.limits.MaxSourceBytes,
		MaxDepth:             d.limits.MaxNestingDepth,
		MaxReportEntries:     d.limits.MaxDiagnostics,
		MaxProvenanceEntries: d.limits.MaxNodeCount * 4,
	}
	fragment, failure := canonicalFragment(value, limits)
	if failure != nil {
		switch failure.Kind {
		case MaterializationUnrepresentable:
			return nil, &EditFailure{Kind: EditFailureUnrepresentableValue, ValueKind: failure.KindName}
		case MaterializationResourceLimit:
			return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: failure.LimitName}
		default:
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
	}
	return fragment, nil
}

func (d *Document) parentTable(entry int) (int, []int, int, bool) {
	for index := range d.entities {
		entity := &d.entities[index]
		if entity.kind != entityItem {
			continue
		}
		item := &entity.item
		var entries []int
		switch item.kind {
		case itemTable:
			entries = item.entries
		case itemInlineTable:
			entries = item.entries
		default:
			continue
		}
		for ordinal, candidate := range entries {
			if candidate == entry {
				return index, entries, ordinal, true
			}
		}
	}
	return 0, nil, 0, false
}

func (d *Document) parentArray(element int) (int, []int, int, bool) {
	for index := range d.entities {
		entity := &d.entities[index]
		if entity.kind != entityItem {
			continue
		}
		item := &entity.item
		if item.kind != itemArray {
			continue
		}
		for ordinal, candidate := range item.elements {
			if candidate == element {
				return index, item.elements, ordinal, true
			}
		}
	}
	return 0, nil, 0, false
}

func (d *Document) entryName(entry int) string {
	key := d.entities[entry].entry.key
	return d.entities[key].key.name
}

func (d *Document) entryItemKind(entry int) TomlItemKind {
	item := d.entities[entry].entry.item
	return d.itemEntity(item).publicKind()
}

// tableEndInsertion computes the End position of a root or standard table
// (consema-toml edit.rs:930-944).
func (d *Document) tableEndInsertion(entries []int, tableIndex int) int {
	for _, entry := range entries {
		if isTableKind(d.entryItemKind(entry)) {
			return d.lineStart(d.entities[entry].span.StartByte())
		}
	}
	if len(entries) > 0 {
		return d.lineAfter(d.entities[entries[len(entries)-1]].span.EndByte())
	}
	if d.itemEntity(tableIndex).publicKind() == ItemKindStandardTable {
		return d.firstLineAfterHeader(d.entities[tableIndex].span)
	}
	return d.entities[tableIndex].span.EndByte()
}

func (d *Document) firstLineAfterHeader(tableSpan document.Span) int {
	return d.lineAfter(tableSpan.StartByte())
}

func (d *Document) lineStart(position int) int {
	source := d.source.Bytes()
	for index := position - 1; index >= 0; index-- {
		if source[index] == '\n' {
			return index + 1
		}
	}
	return 0
}

func (d *Document) lineAfter(position int) int {
	source := d.source.Bytes()
	for index := position; index < len(source); index++ {
		if source[index] == '\n' {
			return index + 1
		}
	}
	return len(source)
}

// lineFragment wraps one line fragment with the document's newline bytes
// (consema-toml edit.rs:964-983).
func (d *Document) lineFragment(position int, fragment []byte) ([]byte, *EditFailure) {
	newline := d.newlineBytes()
	needsPrefix := position > 0 && d.source.Bytes()[position-1] != '\n'
	needsSuffix := position < len(d.source.Bytes())
	extra := len(newline)*boolToInt(needsPrefix) + len(newline)*boolToInt(needsSuffix)
	if len(fragment)+extra > d.limits.MaxSourceBytes {
		return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "insert-fragment"}
	}
	replacement := make([]byte, 0, len(fragment)+extra)
	if needsPrefix {
		replacement = append(replacement, newline...)
	}
	replacement = append(replacement, fragment...)
	if needsSuffix {
		replacement = append(replacement, newline...)
	}
	return replacement, nil
}

// newlineBytes returns the first newline piece's bytes, or LF
// (consema-toml edit.rs:985-994).
func (d *Document) newlineBytes() []byte {
	source := d.source.Bytes()
	for index, piece := range d.index.Pieces() {
		if d.kinds[index] == SyntaxKindNewline {
			return source[piece.Span().StartByte():piece.Span().EndByte()]
		}
	}
	return []byte("\n")
}

// removalComma finds the comma adjacent to the removed association
// (consema-toml edit.rs:996-1026).
func (d *Document) removalComma(associations []int, ordinal, containerEnd int) (document.Span, bool, *EditFailure) {
	current := d.entities[associations[ordinal]].span
	followingEnd := containerEnd
	if ordinal+1 < len(associations) {
		followingEnd = d.entities[associations[ordinal+1]].span.StartByte()
	}
	if comma, ok := d.syntaxBetween(SyntaxKindComma, current.EndByte(), followingEnd, false); ok {
		return comma, true, nil
	}
	if ordinal == 0 {
		return document.Span{}, false, nil
	}
	previous := d.entities[associations[ordinal-1]].span
	comma, ok := d.syntaxBetween(SyntaxKindComma, previous.EndByte(), current.StartByte(), true)
	if !ok {
		return document.Span{}, false, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return comma, true, nil
}

func (d *Document) delimiter(kind TomlSyntaxKind, container document.Span,
	last bool) (document.Span, *EditFailure) {
	span, ok := d.syntaxBetween(kind, container.StartByte(), container.EndByte(), last)
	if !ok {
		return document.Span{}, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return span, nil
}

func (d *Document) syntaxBetween(kind TomlSyntaxKind, start, end int,
	last bool) (document.Span, bool) {
	pieces := d.index.Pieces()
	var matches []document.Span
	for index, piece := range pieces {
		if d.kinds[index] != kind {
			continue
		}
		span := piece.Span()
		if span.StartByte() >= start && span.EndByte() <= end {
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

func isScalarKind(kind TomlItemKind) bool {
	switch kind {
	case ItemKindString, ItemKindInteger, ItemKindFloat, ItemKindBoolean,
		ItemKindOffsetDateTime, ItemKindLocalDateTime, ItemKindLocalDate, ItemKindLocalTime:
		return true
	}
	return false
}

func isTableKind(kind TomlItemKind) bool {
	switch kind {
	case ItemKindRootTable, ItemKindStandardTable, ItemKindImplicitTable,
		ItemKindDottedTable, ItemKindArrayOfTables:
		return true
	}
	return false
}

// validateDependencies rejects duplicate destructive targets and removed
// placement anchors (consema-toml edit.rs:1064-1100).
func validateDependencies(transaction *EditTransaction) *EditFailure {
	destructive := make(map[document.NodeRef]bool, len(transaction.operations))
	removed := make(map[document.NodeRef]bool)
	var anchors []document.NodeRef
	for index := range transaction.operations {
		operation := &transaction.operations[index]
		var target *document.NodeRef
		switch operation.Kind {
		case "ReplaceScalar":
			target = &operation.Scalar.Target
		case "RemoveEntry", "RenameEntry", "RemoveArrayElement":
			target = &operation.Target
		}
		if target != nil {
			if destructive[*target] {
				return &EditFailure{Kind: EditFailureDuplicateTarget}
			}
			destructive[*target] = true
		}
		switch operation.Kind {
		case "RemoveEntry", "RemoveArrayElement":
			removed[operation.Target] = true
		case "InsertEntry", "InsertArrayElement":
			switch operation.Placement.Kind() {
			case document.PlacementBefore, document.PlacementAfter:
				anchors = append(anchors, operation.Placement.Anchor())
			}
		}
	}
	for _, anchor := range anchors {
		if removed[anchor] {
			return &EditFailure{Kind: EditFailurePlacementAnchorRemoved}
		}
	}
	return nil
}

// validateExactScalar validates that the literal is exactly one complete
// TOML 1.0 scalar (consema-toml edit.rs:1379-1413).
func validateExactScalar(literal []byte) *EditFailure {
	if !isValidUTF8(literal) {
		return &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	prefix := "_ = "
	source := append([]byte(prefix), literal...)
	document, formationFailure := Parse(source, Toml10V1, document.DefaultParseLimits())
	if formationFailure != nil {
		return &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	entries, _ := document.Root().TableEntries()
	if len(entries) != 1 {
		return &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	item := entries[0].Item()
	if !isScalarKind(item.Kind()) {
		return &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	span := item.Span()
	if span.StartByte() != len(prefix) || span.EndByte() != len(prefix)+len(literal) {
		return &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	return nil
}

func isValidUTF8(bytes []byte) bool {
	for index := 0; index < len(bytes); {
		c := bytes[index]
		switch {
		case c < 0x80:
			index++
		case c < 0xC0:
			return false
		case c < 0xE0:
			if index+1 >= len(bytes) || bytes[index+1]&0xC0 != 0x80 {
				return false
			}
			index += 2
		case c < 0xF0:
			if index+2 >= len(bytes) || bytes[index+1]&0xC0 != 0x80 || bytes[index+2]&0xC0 != 0x80 {
				return false
			}
			index += 3
		case c < 0xF8:
			if index+3 >= len(bytes) || bytes[index+1]&0xC0 != 0x80 ||
				bytes[index+2]&0xC0 != 0x80 || bytes[index+3]&0xC0 != 0x80 {
				return false
			}
			index += 4
		default:
			return false
		}
	}
	return true
}

// semanticLiteral renders the canonical literal under the representation
// policy (consema-toml edit.rs:1415-1456).
func semanticLiteral(value core.Value, oldKind TomlItemKind, policy RepresentationPolicy,
	targetSpan document.Span, diagnostics *[]*protocol.Diagnostic) ([]byte, *EditFailure) {
	if policy == RepresentationExactLiteral {
		return nil, &EditFailure{Kind: EditFailureExactLiteralRequiresLiteralOperation}
	}
	newKind, ok := portableTomlKind(value)
	if !ok {
		return nil, &EditFailure{Kind: EditFailureUnsupportedSemanticValue, ValueKind: kindNameOf(value)}
	}
	compatible := oldKind == newKind
	switch policy {
	case RepresentationPreserveCompatible:
		if !compatible {
			return nil, &EditFailure{Kind: EditFailureRepresentationIncompatible}
		}
	case RepresentationPreserveElseCanonical:
		if !compatible {
			diagnostic, err := protocol.NewDiagnostic("toml.edit.representation-fallback@1",
				protocol.CategoryEdit, protocol.SeverityWarning,
				&protocol.SourceLocation{StartByte: uint64(targetSpan.StartByte()),
					EndByte: uint64(targetSpan.EndByte())},
				nil, map[string]string{
					"old_kind": string(oldKind),
					"new_kind": string(newKind),
				}, nil, nil, uint64(len(*diagnostics)), protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
			if err != nil {
				return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
			}
			*diagnostics = append(*diagnostics, diagnostic)
		}
	}
	literal, failure := canonicalLiteral(value)
	if failure != nil {
		return nil, failure
	}
	validated, validation := validateExactScalarKind([]byte(literal))
	if validation != nil {
		return nil, validation
	}
	if validated != newKind {
		return nil, &EditFailure{Kind: EditFailureUnsupportedSemanticValue, ValueKind: kindNameOf(value)}
	}
	return []byte(literal), nil
}

// validateExactScalarKind validates the literal and returns its kind.
func validateExactScalarKind(literal []byte) (TomlItemKind, *EditFailure) {
	if failure := validateExactScalar(literal); failure != nil {
		return "", failure
	}
	prefix := "_ = "
	source := append([]byte(prefix), literal...)
	document, formationFailure := Parse(source, Toml10V1, document.DefaultParseLimits())
	if formationFailure != nil {
		return "", &EditFailure{Kind: EditFailureInvalidLiteral}
	}
	entries, _ := document.Root().TableEntries()
	return entries[0].Item().Kind(), nil
}

func portableTomlKind(value core.Value) (TomlItemKind, bool) {
	switch value.(type) {
	case core.String:
		return ItemKindString, true
	case core.Integer:
		return ItemKindInteger, true
	case core.BinaryFloat64:
		return ItemKindFloat, true
	case core.Boolean:
		return ItemKindBoolean, true
	case core.Date:
		return ItemKindLocalDate, true
	case core.Time:
		return ItemKindLocalTime, true
	case core.LocalDateTime:
		return ItemKindLocalDateTime, true
	case core.OffsetDateTime:
		return ItemKindOffsetDateTime, true
	}
	return "", false
}

// canonicalLiteral renders the frozen canonical TOML scalar literal
// (consema-toml edit.rs:1472-1514).
func canonicalLiteral(value core.Value) (string, *EditFailure) {
	unrepresentable := func() (string, *EditFailure) {
		return "", &EditFailure{Kind: EditFailureUnsupportedSemanticValue, ValueKind: kindNameOf(value)}
	}
	switch typed := value.(type) {
	case core.String:
		return canonicalString(string(typed)), nil
	case core.Integer:
		number := typed.Int()
		if !number.IsInt64() {
			return unrepresentable()
		}
		return number.String(), nil
	case core.BinaryFloat64:
		return canonicalFloat(uint64(typed))
	case core.Boolean:
		if typed {
			return "true", nil
		}
		return "false", nil
	case core.Date:
		return canonicalDate(typed)
	case core.Time:
		return canonicalTime(typed)
	case core.LocalDateTime:
		date, failure := canonicalDate(typed.Date())
		if failure != nil {
			return "", failure
		}
		time, failure := canonicalTime(typed.Time())
		if failure != nil {
			return "", failure
		}
		return date + "T" + time, nil
	case core.OffsetDateTime:
		local, failure := canonicalLocalDateTime(typed.Local())
		if failure != nil {
			return "", failure
		}
		seconds := typed.OffsetSeconds()
		if seconds == 0 {
			return local + "Z", nil
		}
		if seconds%60 != 0 {
			return unrepresentable()
		}
		minutes := seconds / 60
		if minutes < -24*60 || minutes >= 24*60 {
			return unrepresentable()
		}
		sign := byte('+')
		if minutes < 0 {
			sign = '-'
			minutes = -minutes
		}
		return fmt.Sprintf("%s%c%02d:%02d", local, sign, minutes/60, minutes%60), nil
	}
	return unrepresentable()
}

func canonicalLocalDateTime(value core.LocalDateTime) (string, *EditFailure) {
	date, failure := canonicalDate(value.Date())
	if failure != nil {
		return "", failure
	}
	time, failure := canonicalTime(value.Time())
	if failure != nil {
		return "", failure
	}
	return date + "T" + time, nil
}

// canonicalString renders the canonical quoted string (consema-toml
// edit.rs:1516-1537).
func canonicalString(value string) string {
	var output strings.Builder
	output.WriteByte('"')
	for _, character := range value {
		switch character {
		case '\b':
			output.WriteString(`\b`)
		case '\t':
			output.WriteString(`\t`)
		case '\n':
			output.WriteString(`\n`)
		case '\f':
			output.WriteString(`\f`)
		case '\r':
			output.WriteString(`\r`)
		case '"':
			output.WriteString(`\"`)
		case '\\':
			output.WriteString(`\\`)
		default:
			if character <= 0x1F || character == 0x7F {
				output.WriteString(fmt.Sprintf("\\u%04X", character))
				continue
			}
			output.WriteRune(character)
		}
	}
	output.WriteByte('"')
	return output.String()
}

func canonicalStringBytes(value string) []byte {
	return []byte(canonicalString(value))
}

// canonicalFloat renders the canonical float (consema-toml edit.rs:1539-1560).
func canonicalFloat(bits uint64) (string, *EditFailure) {
	float := math.Float64frombits(bits)
	if math.IsNaN(float) {
		switch bits {
		case 0x7ff8000000000000:
			return "nan", nil
		case 0xfff8000000000000:
			return "-nan", nil
		}
		return "", &EditFailure{Kind: EditFailureUnsupportedSemanticValue, ValueKind: "BinaryFloat64"}
	}
	if math.IsInf(float, 1) {
		return "inf", nil
	}
	if math.IsInf(float, -1) {
		return "-inf", nil
	}
	output := strconv.FormatFloat(float, 'f', -1, 64)
	if !strings.ContainsAny(output, ".eE") {
		output += ".0"
	}
	return output, nil
}

// canonicalDate renders the canonical local date (consema-toml
// edit.rs:1562-1568).
func canonicalDate(value core.Date) (string, *EditFailure) {
	year := value.Year().Int()
	if !year.IsInt64() {
		return "", &EditFailure{Kind: EditFailureUnsupportedSemanticValue, ValueKind: "Date"}
	}
	yearNumber := year.Int64()
	if yearNumber < 0 || yearNumber > 9999 {
		return "", &EditFailure{Kind: EditFailureUnsupportedSemanticValue, ValueKind: "Date"}
	}
	return fmt.Sprintf("%04d-%02d-%02d", yearNumber, value.Month(), value.Day()), nil
}

// canonicalTime renders the canonical local time (consema-toml
// edit.rs:1570-1587).
func canonicalTime(value core.Time) (string, *EditFailure) {
	nanoseconds, ok := exactNanoseconds(value.FractionalSecond())
	if !ok {
		return "", &EditFailure{Kind: EditFailureUnsupportedSemanticValue, ValueKind: "Time"}
	}
	output := fmt.Sprintf("%02d:%02d:%02d", value.Hour(), value.Minute(), value.Second())
	if nanoseconds != 0 {
		fraction := fmt.Sprintf("%09d", nanoseconds)
		fraction = strings.TrimRight(fraction, "0")
		output += "." + fraction
	}
	return output, nil
}

// findItemBySpan locates the unique item with the exact span
// (consema-toml edit.rs:1638-1651).
func findItemBySpan(document *Document, start, end int) (int, bool) {
	var matches []int
	for index := range document.entities {
		entity := &document.entities[index]
		if entity.kind != entityItem {
			continue
		}
		if entity.span.StartByte() == start && entity.span.EndByte() == end {
			matches = append(matches, index)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return 0, false
}

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

func operationMetadata(transaction *EditTransaction) map[string]string {
	metadata := make(map[string]string, len(transaction.operations))
	for index := range transaction.operations {
		operation := &transaction.operations[index]
		var id string
		switch operation.Kind {
		case "ReplaceScalar":
			if operation.Scalar.IsLiteral {
				id = "toml.edit.replace-scalar-literal@1"
			} else {
				id = "toml.edit.replace-scalar-semantic@1"
			}
		case "InsertEntry":
			id = "toml.edit.insert-entry@1"
		case "RemoveEntry":
			id = "toml.edit.remove-entry@1"
		case "RenameEntry":
			id = "toml.edit.rename-entry@1"
		case "InsertArrayElement":
			id = "toml.edit.insert-array-element@1"
		case "RemoveArrayElement":
			id = "toml.edit.remove-array-element@1"
		}
		metadata[fmt.Sprintf("operation.%d", index)] = id
	}
	return metadata
}

// operationSummaries builds the safe content-free operation summaries
// (consema-toml edit.rs:1156-1240).
func operationSummaries(transaction *EditTransaction) ([]*protocol.EditOperationSummary, *EditFailure) {
	summaries := make([]*protocol.EditOperationSummary, 0, len(transaction.operations))
	for index := range transaction.operations {
		operation := &transaction.operations[index]
		var id string
		var targetRole string
		arguments := map[string]string{}
		switch operation.Kind {
		case "ReplaceScalar":
			if operation.Scalar.IsLiteral {
				id = "toml.edit.replace-scalar-literal"
				targetRole = "toml.scalar-item@1"
				arguments["literal_bytes"] = strconv.Itoa(len(operation.Scalar.Literal))
			} else {
				id = "toml.edit.replace-scalar-semantic"
				targetRole = "toml.scalar-item@1"
				arguments["representation_policy"] = tomlPolicyName(operation.Scalar.Policy)
				arguments["value_kind"] = valueKindName(operation.Scalar.Value)
			}
		case "InsertEntry":
			id = "toml.edit.insert-entry"
			targetRole = "toml.table-item@1"
			arguments["key_bytes"] = strconv.Itoa(len(operation.Key))
			arguments["placement"] = placementName(operation.Placement)
			arguments["value_kind"] = valueKindName(operation.Value)
		case "RemoveEntry":
			id = "toml.edit.remove-entry"
			targetRole = "toml.entry@1"
		case "RenameEntry":
			id = "toml.edit.rename-entry"
			targetRole = "toml.entry@1"
			arguments["key_bytes"] = strconv.Itoa(len(operation.Key))
		case "InsertArrayElement":
			id = "toml.edit.insert-array-element"
			targetRole = "toml.array-item@1"
			arguments["placement"] = placementName(operation.Placement)
			arguments["value_kind"] = valueKindName(operation.Value)
		case "RemoveArrayElement":
			id = "toml.edit.remove-array-element"
			targetRole = "toml.array-element@1"
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
	return "end"
}

func tomlPolicyName(policy RepresentationPolicy) string {
	switch policy {
	case RepresentationExactLiteral:
		return "exact-literal"
	case RepresentationPreserveCompatible:
		return "preserve-compatible"
	case RepresentationCanonicalForProfile:
		return "canonical-for-profile"
	case RepresentationPreserveElseCanonical:
		return "preserve-else-canonical"
	}
	return "canonical-for-profile"
}

func valueKindName(value core.Value) string {
	return strings.ToLower(kindNameOf(value))
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// EditPlanSourceId is the caller-stable source identity of a transferable
// edit plan (document.EditPlanSourceId; consema-document edit_plan.rs:14-26).
type EditPlanSourceId = document.EditPlanSourceId

// NewEditPlanSourceId validates one non-empty bounded external source
// identity.
func NewEditPlanSourceId(value string) (*EditPlanSourceId, error) {
	return document.NewEditPlanSourceId(value)
}

// EditPlan is the fully validated dry-run plan; possessing it does not
// authorize a write (document.EditPlan; consema-document edit_plan.rs:47-97).
type EditPlan = document.EditPlan

// NewEditPlan closes a plan only when its ordered operation metadata
// matches its exact patch.
func NewEditPlan(sourceID EditPlanSourceId, profile document.ProfileId,
	operations []*protocol.EditOperationSummary, patch *document.SourcePatch,
	report []*protocol.Diagnostic) (*EditPlan, error) {
	return document.NewEditPlan(sourceID.String(), profile, operations, patch, report)
}

// UntouchedByteRegion is one maximal unchanged raw-byte interval mapped
// across two source snapshots (document.UntouchedByteRegion;
// untouched_proof.rs:7-59).
type UntouchedByteRegion = document.UntouchedByteRegion

// NewUntouchedByteRegion creates one region fact; the enclosing proof
// validates length and ordering.
func NewUntouchedByteRegion(oldStart, oldEnd, newStart, newEnd int) UntouchedByteRegion {
	return document.NewUntouchedByteRegion(oldStart, oldEnd, newStart, newEnd)
}

// UntouchedByteProofErrorKind classifies a proof construction or
// verification failure (document.UntouchedByteProofErrorKind;
// untouched_proof.rs:135-178).
type UntouchedByteProofErrorKind = document.UntouchedByteProofErrorKind

// The stable proof failure classes.
const (
	ProofErrorEncodingMismatch   = document.ProofErrorEncodingMismatch
	ProofErrorInvalidReplacement = document.ProofErrorInvalidReplacement
	ProofErrorReplacementOrder   = document.ProofErrorReplacementOrder
	ProofErrorDuplicateInsertion = document.ProofErrorDuplicateInsertion
	ProofErrorOriginalMismatch   = document.ProofErrorOriginalMismatch
	ProofErrorTargetMismatch     = document.ProofErrorTargetMismatch
	ProofErrorCoordinateOverflow = document.ProofErrorCoordinateOverflow
	ProofErrorInvalidRegion      = document.ProofErrorInvalidRegion
	ProofErrorDigestMismatch     = document.ProofErrorDigestMismatch
	ProofErrorProofMismatch      = document.ProofErrorProofMismatch
)

// UntouchedByteProofError is a proof construction or verification
// failure (document.UntouchedByteProofError).
type UntouchedByteProofError = document.UntouchedByteProofError

// UntouchedByteProof is the immutable evidence for every byte outside one
// exact replacement plan (document.UntouchedByteProof;
// untouched_proof.rs:61-132).
type UntouchedByteProof = document.UntouchedByteProof

// CreateUntouchedByteProof creates a proof only when the replacements
// exactly produce the supplied target snapshot.
func CreateUntouchedByteProof(base, target *document.SourceSnapshot,
	replacements []document.SourceReplacement) (UntouchedByteProof, error) {
	return document.CreateUntouchedByteProof(base, target, replacements)
}
