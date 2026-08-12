package ini

import (
	"sort"
	"strings"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the typed INI edit operations and the atomic commit
// pipeline (consema-ini edit.rs; RFC 0009 §12). An edit replaces only the
// bytes its operations own; every other byte is covered by the
// untouched-byte proof, and a failure never modifies the base document.
// The eight operations are shared by all three profiles but their
// delimiter, quote, continuation, comment ownership, case-collision, and
// encoding behavior is profile-specific.

// RepresentationPolicy is the explicit semantic value representation
// policy (edit.rs:15-27).
type RepresentationPolicy uint8

// The four frozen policies.
const (
	// RepresentationPolicyExactLiteral requires the caller to use a
	// literal value replacement; semantic replacement rejects it.
	RepresentationPolicyExactLiteral RepresentationPolicy = iota
	// RepresentationPolicyPreserveCompatible retains the target's
	// compatible quote or multiline representation or fails.
	RepresentationPolicyPreserveCompatible
	// RepresentationPolicyCanonicalForProfile uses the selected profile's
	// frozen canonical value representation.
	RepresentationPolicyCanonicalForProfile
	// RepresentationPolicyPreserveElseCanonical preserves when compatible,
	// otherwise reports a canonical fallback.
	RepresentationPolicyPreserveElseCanonical
)

// ValueReplacementKind is the closed value replacement category
// (edit.rs:29-47).
type ValueReplacementKind uint8

// The two frozen categories.
const (
	// ValueReplacementSemantic replaces the stored string under an
	// explicit representation policy.
	ValueReplacementSemantic ValueReplacementKind = iota
	// ValueReplacementLiteral replaces the exact profile-specific value
	// representation bytes.
	ValueReplacementLiteral
)

// ValueReplacement is one INI value replacement bound to the transaction's
// base snapshot (edit.rs:29-47).
type ValueReplacement struct {
	// Kind is the closed replacement category.
	Kind ValueReplacementKind
	// Target is the exact INI entry target.
	Target document.NodeRef
	// Value is the new stored string value (Semantic).
	Value string
	// Policy is the representation contract (Semantic).
	Policy RepresentationPolicy
	// Literal is the exact raw bytes in the base document's selected
	// source encoding (Literal).
	Literal []byte
}

// EditOperationKind is the closed INI edit operation category
// (edit.rs:58-107).
type EditOperationKind uint8

// The seven frozen operation categories.
const (
	// EditOperationReplaceValue replaces one exact entry's value.
	EditOperationReplaceValue EditOperationKind = iota
	// EditOperationInsertSection inserts one new section occurrence.
	EditOperationInsertSection
	// EditOperationRemoveSection removes one exact section and all entries
	// owned by that occurrence.
	EditOperationRemoveSection
	// EditOperationRenameSection replaces one exact section name.
	EditOperationRenameSection
	// EditOperationInsertEntry inserts one new entry into an exact section
	// occurrence.
	EditOperationInsertEntry
	// EditOperationRemoveEntry removes one exact entry occurrence.
	EditOperationRemoveEntry
	// EditOperationRenameEntry replaces one exact entry key.
	EditOperationRenameEntry
)

// EditOperation is one typed INI edit operation bound to an immutable base
// snapshot (edit.rs:58-107). Only the fields of the declared Kind are
// meaningful.
type EditOperation struct {
	// Kind is the closed operation category.
	Kind EditOperationKind
	// Replacement is the value replacement (ReplaceValue).
	Replacement *ValueReplacement
	// Target is the exact ordinary or default-section/entry target
	// (RemoveSection, RenameSection, RemoveEntry, RenameEntry).
	Target document.NodeRef
	// Document is the exact INI document target (InsertSection).
	Document document.NodeRef
	// Section is the exact ordinary or default-section container
	// (InsertEntry).
	Section document.NodeRef
	// Name is the decoded section name (InsertSection, RenameSection).
	Name string
	// Key is the decoded entry key (InsertEntry, RenameEntry).
	Key string
	// Value is the stored string value (InsertEntry).
	Value string
	// Placement is the explicit association placement (InsertSection,
	// InsertEntry).
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
// against one base snapshot (edit.rs:109-128).
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
// (edit.rs:130-243).
type EditTransactionBuilder struct {
	base       document.SnapshotIdentity
	operations []EditOperation
}

// NewEditTransactionBuilder binds a new transaction to one immutable base
// document.
func NewEditTransactionBuilder(doc *Document) *EditTransactionBuilder {
	return &EditTransactionBuilder{base: doc.SnapshotIdentity()}
}

// SemanticValue adds one semantic stored-value replacement.
func (b *EditTransactionBuilder) SemanticValue(target document.NodeRef, value string,
	policy RepresentationPolicy) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationReplaceValue,
		Replacement: &ValueReplacement{Kind: ValueReplacementSemantic, Target: target,
			Value: value, Policy: policy},
	})
	return b
}

// LiteralValue adds one exact raw value-representation replacement.
func (b *EditTransactionBuilder) LiteralValue(target document.NodeRef,
	literal []byte) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationReplaceValue,
		Replacement: &ValueReplacement{Kind: ValueReplacementLiteral, Target: target,
			Literal: append([]byte(nil), literal...)},
	})
	return b
}

// InsertSection adds one canonical section insertion.
func (b *EditTransactionBuilder) InsertSection(documentRef document.NodeRef, name string,
	placement AssociationPlacement) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationInsertSection, Document: documentRef, Name: name,
		Placement: placement,
	})
	return b
}

// RemoveSection adds one exact section removal, including that occurrence's
// owned entries.
func (b *EditTransactionBuilder) RemoveSection(target document.NodeRef) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRemoveSection, Target: target,
	})
	return b
}

// RenameSection adds one exact section-name replacement.
func (b *EditTransactionBuilder) RenameSection(target document.NodeRef,
	name string) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRenameSection, Target: target, Name: name,
	})
	return b
}

// InsertEntry adds one canonical entry insertion.
func (b *EditTransactionBuilder) InsertEntry(section document.NodeRef, key, value string,
	placement AssociationPlacement) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationInsertEntry, Section: section, Key: key, Value: value,
		Placement: placement,
	})
	return b
}

// RemoveEntry adds one exact entry removal.
func (b *EditTransactionBuilder) RemoveEntry(target document.NodeRef) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRemoveEntry, Target: target,
	})
	return b
}

// RenameEntry adds one exact entry-key replacement.
func (b *EditTransactionBuilder) RenameEntry(target document.NodeRef,
	key string) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRenameEntry, Target: target, Key: key,
	})
	return b
}

// Build completes the immutable request; target validation happens
// atomically at commit (edit.rs:235-242).
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

// EditFailureKind is the stable edit validation or commit failure category
// (edit.rs:258-303).
type EditFailureKind uint8

// The closed edit failure categories.
const (
	// EditFailureRecoveredDocument: edits are forbidden on a recovered
	// document.
	EditFailureRecoveredDocument EditFailureKind = iota
	// EditFailureWrongSnapshot: the transaction or target belongs to
	// another snapshot.
	EditFailureWrongSnapshot
	// EditFailureWrongRole: the target is not an INI entry/section.
	EditFailureWrongRole
	// EditFailureDuplicateTarget: more than one operation names the same
	// exact target.
	EditFailureDuplicateTarget
	// EditFailureOverlappingOwnership: prepared value ownership intervals
	// overlap.
	EditFailureOverlappingOwnership
	// EditFailureAncestorDescendantConflict: one operation edits an
	// ancestor/descendant region of another operation.
	EditFailureAncestorDescendantConflict
	// EditFailurePlacementAnchorRemoved: an insertion anchor is removed by
	// the same transaction.
	EditFailurePlacementAnchorRemoved
	// EditFailureTargetNotFound: a target or placement anchor does not
	// exist in its declared container.
	EditFailureTargetNotFound
	// EditFailureInvalidPlacement: a valid entry anchor belongs to another
	// section container.
	EditFailureInvalidPlacement
	// EditFailureInvalidName: a section name is invalid under the selected
	// profile.
	EditFailureInvalidName
	// EditFailureNameCollision: a strict profile would become ambiguous
	// after insertion or rename.
	EditFailureNameCollision
	// EditFailureInvalidKey: an entry key is invalid under the selected
	// profile.
	EditFailureInvalidKey
	// EditFailureDuplicateKey: a strict profile would contain an exact
	// duplicate key.
	EditFailureDuplicateKey
	// EditFailureKeyCollision: Python optionxform makes two distinctly
	// spelled keys equivalent.
	EditFailureKeyCollision
	// EditFailureRepresentationIncompatible: PreserveCompatible cannot
	// retain the target representation.
	EditFailureRepresentationIncompatible
	// EditFailureExactLiteralRequiresLiteralOperation: ExactLiteral was
	// requested without literal bytes.
	EditFailureExactLiteralRequiresLiteralOperation
	// EditFailureUnrepresentableValue: the semantic string cannot be
	// represented by the selected profile.
	EditFailureUnrepresentableValue
	// EditFailureEncodingUnrepresentable: the replacement cannot be
	// encoded exactly in the source encoding.
	EditFailureEncodingUnrepresentable
	// EditFailureInvalidLiteral: literal bytes do not form exactly one
	// value at the target.
	EditFailureInvalidLiteral
	// EditFailureResourceLimit: a configured edit or output bound was
	// exceeded.
	EditFailureResourceLimit
	// EditFailureNewDocumentFormationFailed: replacement bytes could not
	// form one complete document under the original contract.
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
		return "ini: edits are forbidden on a recovered document"
	case EditFailureWrongSnapshot:
		return "ini: edit transaction or target belongs to another snapshot"
	case EditFailureWrongRole:
		return "ini: edit target has the wrong structural role"
	case EditFailureDuplicateTarget:
		return "ini: more than one operation names the same target"
	case EditFailureOverlappingOwnership:
		return "ini: prepared source ownership intervals overlap"
	case EditFailureAncestorDescendantConflict:
		return "ini: an edit targets an ancestor/descendant region"
	case EditFailurePlacementAnchorRemoved:
		return "ini: an insertion anchor is removed by the same transaction"
	case EditFailureTargetNotFound:
		return "ini: edit target or placement anchor was not found"
	case EditFailureInvalidPlacement:
		return "ini: edit placement anchor is invalid"
	case EditFailureInvalidName:
		return "ini: section name is invalid for the selected profile"
	case EditFailureNameCollision:
		return "ini: section insertion or rename would collide"
	case EditFailureInvalidKey:
		return "ini: entry key is invalid for the selected profile"
	case EditFailureDuplicateKey:
		return "ini: entry insertion or rename would duplicate a key"
	case EditFailureKeyCollision:
		return "ini: entry keys would become profile-equivalent"
	case EditFailureRepresentationIncompatible:
		return "ini: representation policy cannot preserve the target"
	case EditFailureExactLiteralRequiresLiteralOperation:
		return "ini: exact literal policy requires a literal operation"
	case EditFailureUnrepresentableValue:
		return "ini: semantic value is not representable by the profile"
	case EditFailureEncodingUnrepresentable:
		return "ini: replacement is not encodable in the source encoding"
	case EditFailureInvalidLiteral:
		return "ini: edit literal is invalid for the target profile"
	case EditFailureResourceLimit:
		return "ini: edit limit " + e.LimitName + " was reached"
	case EditFailureNewDocumentFormationFailed:
		return "ini: replacement document could not be formed"
	}
	return "ini: edit failure"
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
	case EditFailureAncestorDescendantConflict:
		return "AncestorDescendantConflict"
	case EditFailurePlacementAnchorRemoved:
		return "PlacementAnchorRemoved"
	case EditFailureTargetNotFound:
		return "TargetNotFound"
	case EditFailureInvalidPlacement:
		return "InvalidPlacement"
	case EditFailureInvalidName:
		return "InvalidName"
	case EditFailureNameCollision:
		return "NameCollision"
	case EditFailureInvalidKey:
		return "InvalidKey"
	case EditFailureDuplicateKey:
		return "DuplicateKey"
	case EditFailureKeyCollision:
		return "KeyCollision"
	case EditFailureRepresentationIncompatible:
		return "RepresentationIncompatible"
	case EditFailureExactLiteralRequiresLiteralOperation:
		return "ExactLiteralRequiresLiteralOperation"
	case EditFailureUnrepresentableValue:
		return "UnrepresentableValue"
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

// Code returns the frozen registered code for the failure (edit.rs:
// 1754-1779).
func (e *EditFailure) Code() string {
	switch e.Kind {
	case EditFailureRecoveredDocument:
		return "core.edit.incomplete-target@1"
	case EditFailureWrongSnapshot:
		return "core.edit.wrong-snapshot@1"
	case EditFailureWrongRole:
		return "core.edit.wrong-role@1"
	case EditFailureDuplicateTarget, EditFailureOverlappingOwnership,
		EditFailureAncestorDescendantConflict, EditFailurePlacementAnchorRemoved:
		return "core.edit.conflicting-edits@1"
	case EditFailureTargetNotFound:
		return "core.edit.target-not-found@1"
	case EditFailureInvalidPlacement:
		return "ini.edit.invalid-placement@1"
	case EditFailureInvalidName, EditFailureInvalidKey:
		return "ini.edit.invalid-name@1"
	case EditFailureNameCollision, EditFailureDuplicateKey:
		return "core.edit.duplicate-key@1"
	case EditFailureKeyCollision:
		return "ini.edit.case-collision@1"
	case EditFailureRepresentationIncompatible, EditFailureEncodingUnrepresentable:
		return "core.edit.representation-incompatible@1"
	case EditFailureExactLiteralRequiresLiteralOperation:
		return "core.edit.exact-literal-requires-literal@1"
	case EditFailureUnrepresentableValue:
		return "core.edit.unsupported-value@1"
	case EditFailureInvalidLiteral:
		return "core.edit.invalid-literal@1"
	case EditFailureResourceLimit:
		return "core.edit.resource-limit@1"
	case EditFailureNewDocumentFormationFailed:
		return "core.edit.formation-failed@1"
	}
	return "core.edit.conflicting-edits@1"
}

// preparedEdit is one byte-level replacement with its optional node
// mapping plan (edit.rs PreparedEdit/PlannedMapping/MappingPlan).
type preparedEdit struct {
	oldSpan     document.Span
	replacement []byte
	mappings    []plannedMapping
	mergeable   bool
}

type plannedMapping struct {
	old  document.NodeRef
	plan mappingPlan
}

// mappingPlanKind is the closed node-mapping plan.
type mappingPlanKind uint8

const (
	mappingPlanReplacedValue mappingPlanKind = iota
	mappingPlanReplacedSection
	mappingPlanReplacedEntry
	mappingPlanSectionAfterEntryInsertion
	mappingPlanDeleted
	mappingPlanUnmapped
)

type mappingPlan struct {
	kind          mappingPlanKind
	expectedKey   string
	expectedValue string
	expectedName  string
	literal       bool
	reason        string
}

// Commit atomically commits all declared value replacements and
// structural operations (edit.rs:305-553). On failure the base document
// remains unchanged.
func (d *Document) Commit(tx *EditTransaction) (*EditCommit, *EditFailure) {
	if d.formationStatus != document.FormationStatusComplete {
		return nil, &EditFailure{Kind: EditFailureRecoveredDocument}
	}
	if tx.base != d.SnapshotIdentity() {
		return nil, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if len(tx.operations) > d.limits.Common.MaxNodeCount {
		return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "edit-operations"}
	}
	if failure := d.validateDependencies(tx); failure != nil {
		return nil, failure
	}
	targets := map[document.NodeRef]bool{}
	var diagnostics []*protocol.Diagnostic
	var prepared []preparedEdit
	for index := range tx.operations {
		operation := &tx.operations[index]
		if target := destructiveTarget(operation); target != nil {
			if targets[*target] {
				return nil, &EditFailure{Kind: EditFailureDuplicateTarget}
			}
			targets[*target] = true
		}
		edits, failure := d.prepareOperation(operation, &diagnostics)
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
	prepared = d.coalesceAdjacentDeletions(prepared)
	for index := 1; index < len(prepared); index++ {
		left, right := prepared[index-1], prepared[index]
		if left.oldSpan == right.oldSpan {
			return nil, &EditFailure{Kind: EditFailureOverlappingOwnership}
		}
		if left.oldSpan.EndByte() > right.oldSpan.StartByte() {
			return nil, &EditFailure{Kind: EditFailureAncestorDescendantConflict}
		}
	}
	literalOnly := len(tx.operations) > 0
	for index := range tx.operations {
		if tx.operations[index].Kind != EditOperationReplaceValue ||
			tx.operations[index].Replacement == nil ||
			tx.operations[index].Replacement.Kind != ValueReplacementLiteral {
			literalOnly = false
			break
		}
	}
	source := d.source.Bytes()
	targetLen := len(source)
	for _, edit := range prepared {
		targetLen = targetLen - edit.oldSpan.Len() + len(edit.replacement)
		if targetLen > d.limits.Common.MaxSourceBytes {
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
	newDocument, formationFailure := Parse(rendered, d.profile, originalEncodingSelection(d),
		d.limits)
	if formationFailure != nil {
		if literalOnly {
			return nil, &EditFailure{Kind: EditFailureInvalidLiteral}
		}
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	if newDocument.FormationStatus() != document.FormationStatusComplete {
		if literalOnly {
			return nil, &EditFailure{Kind: EditFailureInvalidLiteral}
		}
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	delta := 0
	sourceEdits := make([]SourceEdit, 0, len(prepared))
	mappings := make([]NodeMapping, 0, len(tx.operations))
	mappedOld := map[document.NodeRef]bool{}
	for _, edit := range prepared {
		newStart := edit.oldSpan.StartByte() + delta
		newEnd := newStart + len(edit.replacement)
		newSpan, ok := newDocument.span(newStart, newEnd)
		if !ok {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		sourceEdits = append(sourceEdits, SourceEdit{
			OldSpan: edit.oldSpan, NewSpan: newSpan,
			Replacement: append([]byte(nil), edit.replacement...),
		})
		for _, mapping := range edit.mappings {
			if mappedOld[mapping.old] {
				continue
			}
			mappedOld[mapping.old] = true
			status := NodeMappingUnmapped
			var newNode *document.NodeRef
			var reason *string
			switch mapping.plan.kind {
			case mappingPlanReplacedValue:
				found := false
				for _, entry := range newDocument.entries {
					if entry.key == mapping.plan.expectedKey {
						ownership, failure := newDocument.valueOwnership(&entry)
						if failure == nil && ownership == newSpan {
							node := entry.node
							newNode = &node
							found = true
							break
						}
					}
				}
				if !found {
					if mapping.plan.literal {
						return nil, &EditFailure{Kind: EditFailureInvalidLiteral}
					}
					return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
				}
				status = NodeMappingReplaced
			case mappingPlanReplacedSection:
				found := false
				for _, section := range newDocument.sections {
					if section.name == mapping.plan.expectedName && section.nameSpan == newSpan {
						node := section.node
						newNode = &node
						found = true
						break
					}
				}
				if !found {
					return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
				}
				status = NodeMappingReplaced
			case mappingPlanReplacedEntry:
				found := false
				for _, entry := range newDocument.entries {
					if entry.key == mapping.plan.expectedKey && entry.keySpan == newSpan {
						node := entry.node
						newNode = &node
						found = true
						break
					}
				}
				if !found {
					return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
				}
				status = NodeMappingReplaced
			case mappingPlanSectionAfterEntryInsertion:
				inserted := false
				for _, entry := range newDocument.entries {
					if entry.key == mapping.plan.expectedKey &&
						entry.value == mapping.plan.expectedValue {
						record, failure := newDocument.entryRecordSpan(&entry)
						if failure == nil && record.StartByte() >= newSpan.StartByte() &&
							record.EndByte() == newSpan.EndByte() {
							inserted = true
							break
						}
					}
				}
				if !inserted {
					return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
				}
				reasonText := "section-reparsed-after-entry-insertion"
				reason = &reasonText
			case mappingPlanDeleted:
				status = NodeMappingDeleted
			case mappingPlanUnmapped:
				reasonText := mapping.plan.reason
				reason = &reasonText
			}
			mappings = append(mappings, NodeMapping{Old: mapping.old, New: newNode,
				Status: status, Reason: reason})
		}
		delta += len(edit.replacement) - edit.oldSpan.Len()
	}
	changeSet := document.NewChangeSet(d.SnapshotIdentity(), newDocument.SnapshotIdentity(),
		sourceEdits, mappings, diagnostics)
	patchLimits := editSourcePatchLimits(d.limits, len(sourceEdits))
	patch, patchErr := document.NewSourcePatch(d.source,
		editSourcePatchReplacements(source, sourceEdits), editOperationMetadata(tx), patchLimits)
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
// Document (edit.rs:555-570).
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

// validateDependencies checks the removed-anchor and ancestor-descendant
// rules (edit.rs:863-920).
func (d *Document) validateDependencies(tx *EditTransaction) *EditFailure {
	removedSections := map[document.NodeRef]bool{}
	removedEntries := map[document.NodeRef]bool{}
	for index := range tx.operations {
		operation := &tx.operations[index]
		switch operation.Kind {
		case EditOperationRemoveSection:
			removedSections[operation.Target] = true
		case EditOperationRemoveEntry:
			removedEntries[operation.Target] = true
		}
	}
	for index := range tx.operations {
		operation := &tx.operations[index]
		switch operation.Kind {
		case EditOperationInsertSection:
			if (operation.Placement.Kind() == document.PlacementBefore ||
				operation.Placement.Kind() == document.PlacementAfter) &&
				removedSections[operation.Placement.Anchor()] {
				return &EditFailure{Kind: EditFailurePlacementAnchorRemoved}
			}
		case EditOperationInsertEntry:
			if (operation.Placement.Kind() == document.PlacementBefore ||
				operation.Placement.Kind() == document.PlacementAfter) &&
				removedEntries[operation.Placement.Anchor()] {
				return &EditFailure{Kind: EditFailurePlacementAnchorRemoved}
			}
			if removedSections[operation.Section] {
				return &EditFailure{Kind: EditFailureAncestorDescendantConflict}
			}
		case EditOperationReplaceValue:
			if entry, ok := d.Entry(operation.Replacement.Target); ok &&
				removedSections[entry.Section()] {
				return &EditFailure{Kind: EditFailureAncestorDescendantConflict}
			}
		case EditOperationRemoveEntry, EditOperationRenameEntry:
			if entry, ok := d.Entry(operation.Target); ok && removedSections[entry.Section()] {
				return &EditFailure{Kind: EditFailureAncestorDescendantConflict}
			}
		}
	}
	return nil
}

// prepareOperation dispatches one operation to its byte-level preparation
// (edit.rs:617-650).
func (d *Document) prepareOperation(operation *EditOperation,
	diagnostics *[]*protocol.Diagnostic) ([]preparedEdit, *EditFailure) {
	switch operation.Kind {
	case EditOperationReplaceValue:
		edit, failure := d.prepareValue(operation.Replacement, diagnostics)
		if failure != nil {
			return nil, failure
		}
		return []preparedEdit{*edit}, nil
	case EditOperationInsertSection:
		edit, failure := d.prepareInsertSection(operation.Document, operation.Name,
			operation.Placement)
		if failure != nil {
			return nil, failure
		}
		return []preparedEdit{*edit}, nil
	case EditOperationRemoveSection:
		return d.prepareRemoveSection(operation.Target)
	case EditOperationRenameSection:
		edit, failure := d.prepareRenameSection(operation.Target, operation.Name)
		if failure != nil {
			return nil, failure
		}
		return []preparedEdit{*edit}, nil
	case EditOperationInsertEntry:
		edit, failure := d.prepareInsertEntry(operation.Section, operation.Key,
			operation.Value, operation.Placement)
		if failure != nil {
			return nil, failure
		}
		return []preparedEdit{*edit}, nil
	case EditOperationRemoveEntry:
		return d.prepareRemoveEntry(operation.Target)
	case EditOperationRenameEntry:
		edit, failure := d.prepareRenameEntry(operation.Target, operation.Key)
		if failure != nil {
			return nil, failure
		}
		return []preparedEdit{*edit}, nil
	}
	return nil, &EditFailure{Kind: EditFailureWrongRole}
}

// prepareValue prepares one value replacement (edit.rs:572-615).
func (d *Document) prepareValue(replacement *ValueReplacement,
	diagnostics *[]*protocol.Diagnostic) (*preparedEdit, *EditFailure) {
	target := replacement.Target
	if target.Snapshot() != d.SnapshotIdentity() {
		return nil, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if target.Role() != document.RoleIniEntry {
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	ordinal := -1
	for index := range d.entries {
		if d.entries[index].node == target {
			ordinal = index
			break
		}
	}
	if ordinal < 0 {
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	entry := d.entries[ordinal]
	oldSpan, failure := d.valueOwnership(&entry)
	if failure != nil {
		return nil, failure
	}
	var replacementBytes []byte
	literal := false
	switch replacement.Kind {
	case ValueReplacementLiteral:
		if len(replacement.Literal) > d.limits.Common.MaxSourceBytes {
			return nil, &EditFailure{Kind: EditFailureResourceLimit,
				LimitName: "replacement-bytes"}
		}
		replacementBytes = append([]byte(nil), replacement.Literal...)
		literal = true
	case ValueReplacementSemantic:
		bytes, failure := d.semanticValue(&entry, replacement.Value, replacement.Policy,
			diagnostics)
		if failure != nil {
			return nil, failure
		}
		replacementBytes = bytes
	}
	return &preparedEdit{
		oldSpan: oldSpan, replacement: replacementBytes,
		mappings: []plannedMapping{{
			old: target,
			plan: mappingPlan{kind: mappingPlanReplacedValue,
				expectedKey: entry.key, literal: literal},
		}},
	}, nil
}

// prepareInsertSection prepares one canonical section insertion
// (edit.rs:652-705).
func (d *Document) prepareInsertSection(documentRef document.NodeRef, name string,
	placement AssociationPlacement) (*preparedEdit, *EditFailure) {
	if failure := d.resolveDocument(documentRef); failure != nil {
		return nil, failure
	}
	if failure := d.validateSectionName(name); failure != nil {
		return nil, failure
	}
	if failure := d.validateSectionCollision(name, nil); failure != nil {
		return nil, failure
	}
	var position int
	switch placement.Kind() {
	case document.PlacementStart:
		if len(d.sections) == 0 {
			position = d.source.Len()
		} else {
			position, _ = d.sectionLineStart(&d.sections[0])
		}
	case document.PlacementEnd:
		position = d.source.Len()
	case document.PlacementBefore, document.PlacementAfter:
		section, failure := d.resolveSection(placement.Anchor())
		if failure != nil {
			return nil, failure
		}
		if placement.Kind() == document.PlacementBefore {
			position, failure = d.sectionLineStart(section)
			if failure != nil {
				return nil, failure
			}
		} else {
			ordinal := -1
			for index := range d.sections {
				if d.sections[index].node == placement.Anchor() {
					ordinal = index
					break
				}
			}
			if ordinal < 0 {
				return nil, &EditFailure{Kind: EditFailureTargetNotFound}
			}
			if ordinal+1 < len(d.sections) {
				position, failure = d.sectionLineStart(&d.sections[ordinal+1])
				if failure != nil {
					return nil, failure
				}
			} else {
				position = d.source.Len()
			}
		}
	default:
		return nil, &EditFailure{Kind: EditFailureInvalidPlacement}
	}
	var text strings.Builder
	if position == d.source.Len() && !d.sourceEndsWithBreak() {
		text.WriteString(profileNewline(d.profile))
	}
	text.WriteString("[")
	text.WriteString(name)
	text.WriteString("]")
	text.WriteString(profileNewline(d.profile))
	encoded, failure := d.encodeValue(text.String())
	if failure != nil {
		return nil, failure
	}
	span, ok := d.span(position, position)
	if !ok {
		return nil, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return &preparedEdit{
		oldSpan: span, replacement: encoded,
		mappings: []plannedMapping{{
			old: documentRef,
			plan: mappingPlan{kind: mappingPlanUnmapped,
				reason: "document-reparsed-after-section-insertion"},
		}},
	}, nil
}

// prepareRemoveSection prepares one section removal including its owned
// entries (edit.rs:707-739).
func (d *Document) prepareRemoveSection(target document.NodeRef) ([]preparedEdit, *EditFailure) {
	section, failure := d.resolveSection(target)
	if failure != nil {
		return nil, failure
	}
	headerSpans, failure := d.logicalPhysicalSpans(section.logicalLine)
	if failure != nil {
		return nil, failure
	}
	var edits []preparedEdit
	for index, span := range headerSpans {
		edits = append(edits, deletionEdit(span, firstTarget(index, target)))
	}
	for _, entry := range d.entries {
		if entry.section != target {
			continue
		}
		spans, failure := d.logicalPhysicalSpans(entry.logicalLine)
		if failure != nil {
			return nil, failure
		}
		for index, span := range spans {
			edits = append(edits, deletionEdit(span, firstTarget(index, entry.node)))
		}
	}
	return edits, nil
}

// prepareRenameSection prepares one exact section-name replacement
// (edit.rs:741-760).
func (d *Document) prepareRenameSection(target document.NodeRef,
	name string) (*preparedEdit, *EditFailure) {
	section, failure := d.resolveSection(target)
	if failure != nil {
		return nil, failure
	}
	if failure := d.validateSectionName(name); failure != nil {
		return nil, failure
	}
	if failure := d.validateSectionCollision(name, &target); failure != nil {
		return nil, failure
	}
	encoded, failure := d.encodeValue(name)
	if failure != nil {
		return nil, failure
	}
	return &preparedEdit{
		oldSpan: section.nameSpan, replacement: encoded,
		mappings: []plannedMapping{{
			old:  target,
			plan: mappingPlan{kind: mappingPlanReplacedSection, expectedName: name},
		}},
	}, nil
}

// prepareInsertEntry prepares one canonical entry insertion
// (edit.rs:762-827).
func (d *Document) prepareInsertEntry(section document.NodeRef, key, value string,
	placement AssociationPlacement) (*preparedEdit, *EditFailure) {
	if _, failure := d.resolveSection(section); failure != nil {
		return nil, failure
	}
	if failure := d.validateEntryKey(key); failure != nil {
		return nil, failure
	}
	if failure := d.validateEntryCollision(section, key, nil); failure != nil {
		return nil, failure
	}
	if failure := validateSemanticValue(d.profile, value); failure != nil {
		return nil, failure
	}
	var entries []*IniEntry
	for index := range d.entries {
		if d.entries[index].section == section {
			entries = append(entries, &d.entries[index])
		}
	}
	var position int
	var failure *EditFailure
	switch placement.Kind() {
	case document.PlacementStart:
		if len(entries) == 0 {
			position, failure = d.sectionContentEnd(section)
		} else {
			position, failure = d.entryLineStart(entries[0])
		}
	case document.PlacementEnd:
		position, failure = d.sectionContentEnd(section)
	case document.PlacementBefore, document.PlacementAfter:
		entry, resolveFailure := d.resolveEntryInSection(placement.Anchor(), section, entries)
		if resolveFailure != nil {
			return nil, resolveFailure
		}
		if placement.Kind() == document.PlacementBefore {
			position, failure = d.entryLineStart(entry)
		} else {
			position, failure = d.entryLineEnd(entry)
		}
	default:
		return nil, &EditFailure{Kind: EditFailureInvalidPlacement}
	}
	if failure != nil {
		return nil, failure
	}
	var text strings.Builder
	if position == d.source.Len() && !d.sourceEndsWithBreak() {
		text.WriteString(profileNewline(d.profile))
	}
	canonical, failure := d.canonicalEntryText(key, value)
	if failure != nil {
		return nil, failure
	}
	text.WriteString(canonical)
	encoded, failure := d.encodeValue(text.String())
	if failure != nil {
		return nil, failure
	}
	span, ok := d.span(position, position)
	if !ok {
		return nil, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return &preparedEdit{
		oldSpan: span, replacement: encoded,
		mappings: []plannedMapping{{
			old: section,
			plan: mappingPlan{kind: mappingPlanSectionAfterEntryInsertion,
				expectedKey: key, expectedValue: value},
		}},
	}, nil
}

// prepareRemoveEntry prepares one exact entry removal (edit.rs:829-840).
func (d *Document) prepareRemoveEntry(target document.NodeRef) ([]preparedEdit, *EditFailure) {
	entry, failure := d.resolveEntry(target)
	if failure != nil {
		return nil, failure
	}
	spans, failure := d.logicalPhysicalSpans(entry.logicalLine)
	if failure != nil {
		return nil, failure
	}
	var edits []preparedEdit
	for index, span := range spans {
		edits = append(edits, deletionEdit(span, firstTarget(index, target)))
	}
	return edits, nil
}

// prepareRenameEntry prepares one exact entry-key replacement
// (edit.rs:841-861).
func (d *Document) prepareRenameEntry(target document.NodeRef,
	key string) (*preparedEdit, *EditFailure) {
	entry, failure := d.resolveEntry(target)
	if failure != nil {
		return nil, failure
	}
	if failure := d.validateEntryKey(key); failure != nil {
		return nil, failure
	}
	if failure := d.validateEntryCollision(entry.section, key, &target); failure != nil {
		return nil, failure
	}
	encoded, failure := d.encodeValue(key)
	if failure != nil {
		return nil, failure
	}
	return &preparedEdit{
		oldSpan: entry.keySpan, replacement: encoded,
		mappings: []plannedMapping{{
			old:  target,
			plan: mappingPlan{kind: mappingPlanReplacedEntry, expectedKey: key},
		}},
	}, nil
}

// semanticValue computes one semantic replacement under the policy
// (edit.rs:1228-1259).
func (d *Document) semanticValue(entry *IniEntry, value string, policy RepresentationPolicy,
	diagnostics *[]*protocol.Diagnostic) ([]byte, *EditFailure) {
	if policy == RepresentationPolicyExactLiteral {
		return nil, &EditFailure{Kind: EditFailureExactLiteralRequiresLiteralOperation}
	}
	if failure := validateSemanticValue(d.profile, value); failure != nil {
		return nil, failure
	}
	switch policy {
	case RepresentationPolicyPreserveCompatible:
		return d.preservedValue(entry, value)
	case RepresentationPolicyCanonicalForProfile:
		return d.canonicalValue(entry, value)
	default: // PreserveElseCanonical
		bytes, failure := d.preservedValue(entry, value)
		if failure == nil {
			return bytes, nil
		}
		if failure.Kind != EditFailureRepresentationIncompatible {
			return nil, failure
		}
		d.pushFallbackDiagnostic(diagnostics, entry.valueSpan)
		return d.canonicalValue(entry, value)
	}
}

// preservedValue retains the compatible quote or multiline representation
// (edit.rs:1261-1284).
func (d *Document) preservedValue(entry *IniEntry, value string) ([]byte, *EditFailure) {
	switch {
	case d.profile.isPortable():
		return d.encodeValue(value)
	case d.profile.isWindows():
		switch entry.quoteStyle {
		case QuoteStyleSingle, QuoteStyleDouble:
			quote := byte('\'')
			if entry.quoteStyle == QuoteStyleDouble {
				quote = '"'
			}
			return d.encodeValue(string([]byte{quote}) + value + string([]byte{quote}))
		case QuoteStyleNone:
			if !windowsValueNeedsQuotes(value) {
				return d.encodeValue(value)
			}
			return nil, &EditFailure{Kind: EditFailureRepresentationIncompatible}
		}
		return nil, &EditFailure{Kind: EditFailureRepresentationIncompatible}
	default:
		return d.preservedPythonValue(entry, value)
	}
}

// canonicalValue renders the frozen canonical value representation
// (edit.rs:1286-1303).
func (d *Document) canonicalValue(entry *IniEntry, value string) ([]byte, *EditFailure) {
	switch {
	case d.profile.isPortable():
		return d.encodeValue(value)
	case d.profile.isWindows():
		if windowsValueNeedsQuotes(value) {
			quote := byte('"')
			if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
				quote = '\''
			}
			return d.encodeValue(string([]byte{quote}) + value + string([]byte{quote}))
		}
		return d.encodeValue(value)
	default:
		return d.canonicalPythonValue(entry, value)
	}
}

// preservedPythonValue retains the exact multiline representation when the
// new value has the same line structure (edit.rs:1305-1385).
func (d *Document) preservedPythonValue(entry *IniEntry, value string) ([]byte, *EditFailure) {
	logical, ok := d.LogicalLine(entry.logicalLine)
	if !ok {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	physicalNodes := logical.PhysicalLines()
	newLines := strings.Split(value, "\n")
	oldLines := strings.Split(entry.value, "\n")
	if len(physicalNodes) != len(newLines) || len(oldLines) != len(newLines) {
		return nil, &EditFailure{Kind: EditFailureRepresentationIncompatible}
	}
	var output []byte
	first, failure := d.encodeValue(newLines[0])
	if failure != nil {
		return nil, failure
	}
	output = append(output, first...)
	firstPhysical, ok := d.PhysicalLine(physicalNodes[0])
	if !ok {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	output = append(output, d.rawBetween(entry.valueSpan.EndByte(),
		firstPhysical.contentSpan.EndByte())...)
	for index := 1; index < len(physicalNodes); index++ {
		previous, ok := d.PhysicalLine(physicalNodes[index-1])
		if !ok {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		if previous.lineBreakSpan == nil {
			return nil, &EditFailure{Kind: EditFailureRepresentationIncompatible}
		}
		output = append(output, d.rawBetween(previous.lineBreakSpan.StartByte(),
			previous.lineBreakSpan.EndByte())...)
		line, ok := d.PhysicalLine(physicalNodes[index])
		if !ok {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		if (len(oldLines[index]) == 0) != (len(newLines[index]) == 0) {
			return nil, &EditFailure{Kind: EditFailureRepresentationIncompatible}
		}
		if len(newLines[index]) == 0 {
			output = append(output, d.rawBetween(line.contentSpan.StartByte(),
				line.contentSpan.EndByte())...)
			continue
		}
		valuePiece, ok := d.pieceWithin(line.contentSpan, SyntaxKindEntryValue)
		if !ok {
			return nil, &EditFailure{Kind: EditFailureRepresentationIncompatible}
		}
		output = append(output, d.rawBetween(line.contentSpan.StartByte(),
			valuePiece.StartByte())...)
		encoded, failure := d.encodeValue(newLines[index])
		if failure != nil {
			return nil, failure
		}
		output = append(output, encoded...)
		output = append(output, d.rawBetween(valuePiece.EndByte(),
			line.contentSpan.EndByte())...)
	}
	return output, nil
}

// canonicalPythonValue renders the canonical multiline value with the
// original base indentation (edit.rs:1387-1430).
func (d *Document) canonicalPythonValue(entry *IniEntry, value string) ([]byte, *EditFailure) {
	logical, ok := d.LogicalLine(entry.logicalLine)
	if !ok {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	physicalNodes := logical.PhysicalLines()
	if len(physicalNodes) == 0 {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	first, ok := d.PhysicalLine(physicalNodes[0])
	if !ok {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	baseIndent := d.rawBetween(first.contentSpan.StartByte(), entry.keySpan.StartByte())
	var output []byte
	for index, line := range strings.Split(value, "\n") {
		if index > 0 {
			encoded, failure := d.encodeValue("\n")
			if failure != nil {
				return nil, failure
			}
			output = append(output, encoded...)
			output = append(output, baseIndent...)
			if len(line) > 0 {
				encoded, failure := d.encodeValue("    ")
				if failure != nil {
					return nil, failure
				}
				output = append(output, encoded...)
			}
		}
		encoded, failure := d.encodeValue(line)
		if failure != nil {
			return nil, failure
		}
		output = append(output, encoded...)
	}
	return output, nil
}

// encodeValue encodes one replacement fragment into the base source
// encoding (edit.rs:1432-1443).
func (d *Document) encodeValue(value string) ([]byte, *EditFailure) {
	bytes, failure := encodeFragment(value, d.source.EncodingFacts().Selected(),
		d.limits.Common.MaxSourceBytes)
	if failure != nil {
		switch failure.Kind {
		case MaterializationResourceLimit:
			return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: failure.LimitName}
		case MaterializationUnsupportedEncoding:
			return nil, &EditFailure{Kind: EditFailureEncodingUnrepresentable}
		default:
			return nil, &EditFailure{Kind: EditFailureUnrepresentableValue}
		}
	}
	return bytes, nil
}

// valueOwnership returns the exact value ownership span of one entry
// (edit.rs:1445-1475).
func (d *Document) valueOwnership(entry *IniEntry) (document.Span, *EditFailure) {
	var start, end int
	switch {
	case d.profile.isPortable():
		start, end = entry.valueSpan.StartByte(), entry.valueSpan.EndByte()
	case d.profile.isWindows():
		delimiter, ok := d.pieceWithin(entry.span, SyntaxKindDelimiter)
		if !ok {
			return document.Span{}, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		start, end = delimiter.EndByte(), entry.span.EndByte()
	default:
		logical, ok := d.LogicalLine(entry.logicalLine)
		if !ok {
			return document.Span{}, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		physicalNodes := logical.PhysicalLines()
		if len(physicalNodes) == 0 {
			return document.Span{}, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		last, ok := d.PhysicalLine(physicalNodes[len(physicalNodes)-1])
		if !ok {
			return document.Span{}, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		start, end = entry.valueSpan.StartByte(), last.contentSpan.EndByte()
	}
	span, ok := d.span(start, end)
	if !ok {
		return document.Span{}, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return span, nil
}

// entryRecordSpan returns the complete record span of one entry
// (edit.rs:1477-1494).
func (d *Document) entryRecordSpan(entry *IniEntry) (document.Span, *EditFailure) {
	logical, ok := d.LogicalLine(entry.logicalLine)
	if !ok {
		return document.Span{}, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	physicalNodes := logical.PhysicalLines()
	if len(physicalNodes) == 0 {
		return document.Span{}, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	first, ok := d.PhysicalLine(physicalNodes[0])
	if !ok {
		return document.Span{}, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	last, ok := d.PhysicalLine(physicalNodes[len(physicalNodes)-1])
	if !ok {
		return document.Span{}, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	span, ok := d.span(first.span.StartByte(), last.span.EndByte())
	if !ok {
		return document.Span{}, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return span, nil
}

// validateSectionName validates one new section name (edit.rs:950-969).
func (d *Document) validateSectionName(name string) *EditFailure {
	valid := false
	switch {
	case d.profile.isPortable():
		valid = len(name) > 0 && allBytes(name, isPortableName)
	case d.profile.isWindows():
		valid = len(name) > 0 && allBytes(name, isWindowsName)
	case d.profile.isPython():
		valid = len(name) > 0 && !strings.ContainsAny(name, "\x00\r\n")
	}
	if !valid {
		return &EditFailure{Kind: EditFailureInvalidName}
	}
	return nil
}

// validateSectionCollision rejects strict-profile section collisions
// (edit.rs:972-987).
func (d *Document) validateSectionCollision(name string, except *document.NodeRef) *EditFailure {
	if d.profile.isWindows() {
		return nil
	}
	for index := range d.sections {
		section := &d.sections[index]
		if except != nil && section.node == *except {
			continue
		}
		if section.name == name {
			return &EditFailure{Kind: EditFailureNameCollision}
		}
	}
	return nil
}

// validateEntryKey validates one new entry key (edit.rs:1016-1040).
func (d *Document) validateEntryKey(key string) *EditFailure {
	valid := false
	switch {
	case d.profile.isPortable():
		valid = len(key) > 0 && allBytes(key, isPortableName)
	case d.profile.isWindows():
		valid = len(key) > 0 && trimHorizontal(key) == key && allBytes(key, isWindowsName)
	case d.profile.isPython():
		valid = len(key) > 0 && trimHorizontal(key) == key &&
			!strings.ContainsAny(key, "\x00\r\n=:") &&
			(len(key) == 0 || (key[0] != '#' && key[0] != ';'))
	}
	if !valid {
		return &EditFailure{Kind: EditFailureInvalidKey}
	}
	return nil
}

// validateEntryCollision rejects strict-profile entry collisions
// (edit.rs:1042-1069).
func (d *Document) validateEntryCollision(section document.NodeRef, key string,
	except *document.NodeRef) *EditFailure {
	if d.profile.isWindows() {
		return nil
	}
	comparison := key
	if d.profile.isPython() {
		comparison = optionxform(key)
	}
	for index := range d.entries {
		entry := &d.entries[index]
		if entry.section != section {
			continue
		}
		if except != nil && entry.node == *except {
			continue
		}
		if entry.comparisonKey == comparison {
			if entry.key == key {
				return &EditFailure{Kind: EditFailureDuplicateKey}
			}
			return &EditFailure{Kind: EditFailureKeyCollision}
		}
	}
	return nil
}

// resolveDocument resolves the document root target (edit.rs:922-932).
func (d *Document) resolveDocument(target document.NodeRef) *EditFailure {
	if target.Snapshot() != d.SnapshotIdentity() {
		return &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if target.Role() != document.RoleIniDocument {
		return &EditFailure{Kind: EditFailureWrongRole}
	}
	if target != d.rootNode {
		return &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return nil
}

// resolveSection resolves one section/default-section target
// (edit.rs:934-948).
func (d *Document) resolveSection(target document.NodeRef) (*IniSection, *EditFailure) {
	if target.Snapshot() != d.SnapshotIdentity() {
		return nil, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if target.Role() != document.RoleIniSection && target.Role() != document.RoleIniDefaultSection {
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	for index := range d.sections {
		if d.sections[index].node == target {
			return &d.sections[index], nil
		}
	}
	return nil, &EditFailure{Kind: EditFailureTargetNotFound}
}

// resolveEntry resolves one entry target (edit.rs:989-1000).
func (d *Document) resolveEntry(target document.NodeRef) (*IniEntry, *EditFailure) {
	if target.Snapshot() != d.SnapshotIdentity() {
		return nil, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if target.Role() != document.RoleIniEntry {
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	for index := range d.entries {
		if d.entries[index].node == target {
			return &d.entries[index], nil
		}
	}
	return nil, &EditFailure{Kind: EditFailureTargetNotFound}
}

// resolveEntryInSection resolves one entry placement anchor within its
// declared section (edit.rs:1002-1014).
func (d *Document) resolveEntryInSection(target document.NodeRef, section document.NodeRef,
	entries []*IniEntry) (*IniEntry, *EditFailure) {
	if _, failure := d.resolveEntry(target); failure != nil {
		return nil, failure
	}
	for _, entry := range entries {
		if entry.node == target && entry.section == section {
			return entry, nil
		}
	}
	return nil, &EditFailure{Kind: EditFailureInvalidPlacement}
}

// entryLineStart returns the raw start byte of one entry's record
// (edit.rs:1071-1078).
func (d *Document) entryLineStart(entry *IniEntry) (int, *EditFailure) {
	logical, ok := d.LogicalLine(entry.logicalLine)
	if !ok {
		return 0, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	nodes := logical.PhysicalLines()
	if len(nodes) == 0 {
		return 0, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	line, ok := d.PhysicalLine(nodes[0])
	if !ok {
		return 0, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return line.span.StartByte(), nil
}

// entryLineEnd returns the raw end byte of one entry's record
// (edit.rs:1080-1087).
func (d *Document) entryLineEnd(entry *IniEntry) (int, *EditFailure) {
	logical, ok := d.LogicalLine(entry.logicalLine)
	if !ok {
		return 0, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	nodes := logical.PhysicalLines()
	if len(nodes) == 0 {
		return 0, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	line, ok := d.PhysicalLine(nodes[len(nodes)-1])
	if !ok {
		return 0, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return line.span.EndByte(), nil
}

// sectionLineStart returns the raw start byte of one section header record
// (edit.rs:1169-1176).
func (d *Document) sectionLineStart(section *IniSection) (int, *EditFailure) {
	logical, ok := d.LogicalLine(section.logicalLine)
	if !ok {
		return 0, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	nodes := logical.PhysicalLines()
	if len(nodes) == 0 {
		return 0, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	line, ok := d.PhysicalLine(nodes[0])
	if !ok {
		return 0, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return line.span.StartByte(), nil
}

// sectionContentEnd returns the raw insertion end of one section
// (edit.rs:1089-1099).
func (d *Document) sectionContentEnd(target document.NodeRef) (int, *EditFailure) {
	ordinal := -1
	for index := range d.sections {
		if d.sections[index].node == target {
			ordinal = index
			break
		}
	}
	if ordinal < 0 {
		return 0, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	if ordinal+1 < len(d.sections) {
		return d.sectionLineStart(&d.sections[ordinal+1])
	}
	return d.source.Len(), nil
}

// canonicalEntryText renders one canonical entry line with its profile
// newline (edit.rs:1101-1167).
func (d *Document) canonicalEntryText(key, value string) (string, *EditFailure) {
	continuationOverhead := 0
	if d.profile.isPython() {
		continuationOverhead = strings.Count(value, "\n") * 4
	}
	estimated := len(key) + len(value) + continuationOverhead + 8
	if estimated > d.limits.Common.MaxSourceBytes {
		return "", &EditFailure{Kind: EditFailureResourceLimit, LimitName: "replacement-bytes"}
	}
	var text strings.Builder
	text.Grow(estimated)
	text.WriteString(key)
	switch {
	case d.profile.isPortable():
		text.WriteString("=")
		text.WriteString(value)
	case d.profile.isWindows():
		text.WriteString("=")
		if windowsValueNeedsQuotes(value) {
			quote := byte('"')
			if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
				quote = '\''
			}
			text.WriteByte(quote)
			text.WriteString(value)
			text.WriteByte(quote)
		} else {
			text.WriteString(value)
		}
	default:
		text.WriteString(" =")
		for index, line := range strings.Split(value, "\n") {
			if index == 0 {
				if len(line) > 0 {
					text.WriteString(" ")
				}
			} else {
				text.WriteString("\n")
				if len(line) > 0 {
					text.WriteString("    ")
				}
			}
			text.WriteString(line)
		}
	}
	text.WriteString(profileNewline(d.profile))
	if text.Len() > d.limits.Common.MaxSourceBytes {
		return "", &EditFailure{Kind: EditFailureResourceLimit, LimitName: "replacement-bytes"}
	}
	return text.String(), nil
}

// logicalPhysicalSpans returns the ordered physical spans of one logical
// record (edit.rs:1178-1194).
func (d *Document) logicalPhysicalSpans(logical document.NodeRef) ([]document.Span, *EditFailure) {
	record, ok := d.LogicalLine(logical)
	if !ok {
		return nil, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	var spans []document.Span
	for _, node := range record.PhysicalLines() {
		line, ok := d.PhysicalLine(node)
		if !ok {
			return nil, &EditFailure{Kind: EditFailureTargetNotFound}
		}
		spans = append(spans, line.span)
	}
	return spans, nil
}

// coalesceAdjacentDeletions merges contiguous deletion intervals
// (edit.rs:1196-1226).
func (d *Document) coalesceAdjacentDeletions(edits []preparedEdit) []preparedEdit {
	var merged []preparedEdit
	for _, edit := range edits {
		if len(merged) > 0 {
			previous := &merged[len(merged)-1]
			if previous.mergeable && edit.mergeable &&
				previous.oldSpan.EndByte() == edit.oldSpan.StartByte() {
				span, ok := d.span(previous.oldSpan.StartByte(), edit.oldSpan.EndByte())
				if ok {
					previous.oldSpan = span
					previous.mappings = append(previous.mappings, edit.mappings...)
					continue
				}
			}
		}
		merged = append(merged, edit)
	}
	return merged
}

// pushFallbackDiagnostic records the explicit canonical fallback
// (edit.rs:1247-1255).
func (d *Document) pushFallbackDiagnostic(diagnostics *[]*protocol.Diagnostic,
	span document.Span) {
	diagnostic, err := protocol.NewDiagnostic("ini.edit.canonical-fallback@1",
		protocol.CategoryEdit, protocol.SeverityWarning,
		&protocol.SourceLocation{StartByte: uint64(span.StartByte()), EndByte: uint64(span.EndByte())},
		nil, nil, nil, nil, 0, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		panic("ini: unregistered edit diagnostic ini.edit.canonical-fallback@1")
	}
	*diagnostics = append(*diagnostics, diagnostic)
}

// sourceEndsWithBreak reports whether the decoded source ends with a line
// break.
func (d *Document) sourceEndsWithBreak() bool {
	text, ok := d.source.DecodedText()
	if !ok {
		return false
	}
	return strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\r")
}

// rawBetween returns the exact raw bytes of one range.
func (d *Document) rawBetween(start, end int) []byte {
	return d.source.Bytes()[start:end]
}

// validateSemanticValue checks one new semantic value (edit.rs:1518-1535).
func validateSemanticValue(profile IniProfile, value string) *EditFailure {
	valid := false
	switch {
	case profile.isPortable():
		valid = allBytes(value, isPortableValue)
	case profile.isWindows():
		valid = !strings.ContainsAny(value, "\x00\r\n")
	case profile.isPython():
		valid = !strings.ContainsAny(value, "\x00\r") && !strings.HasSuffix(value, "\n")
		if valid {
			for index, line := range strings.Split(value, "\n") {
				if trimHorizontal(line) != line {
					valid = false
					break
				}
				if index > 0 && len(line) > 0 && (line[0] == '#' || line[0] == ';') {
					valid = false
					break
				}
			}
		}
	}
	if !valid {
		return &EditFailure{Kind: EditFailureUnrepresentableValue}
	}
	return nil
}

// destructiveTarget returns the destructive target of one operation
// (edit.rs:1537-1546).
func destructiveTarget(operation *EditOperation) *document.NodeRef {
	switch operation.Kind {
	case EditOperationReplaceValue:
		return &operation.Replacement.Target
	case EditOperationRemoveSection, EditOperationRenameSection,
		EditOperationRemoveEntry, EditOperationRenameEntry:
		return &operation.Target
	}
	return nil
}

// deletionEdit builds one mergeable deletion interval (edit.rs:1548-1561).
func deletionEdit(span document.Span, target *document.NodeRef) preparedEdit {
	var mappings []plannedMapping
	if target != nil {
		mappings = append(mappings, plannedMapping{old: *target,
			plan: mappingPlan{kind: mappingPlanDeleted}})
	}
	return preparedEdit{oldSpan: span, mergeable: true, mappings: mappings}
}

// firstTarget maps one physical index to the record target mapping.
func firstTarget(index int, target document.NodeRef) *document.NodeRef {
	if index == 0 {
		return &target
	}
	return nil
}

// profileNewline returns the frozen profile newline (edit.rs:1563-1568).
func profileNewline(profile IniProfile) string {
	if profile.isWindows() {
		return "\r\n"
	}
	return "\n"
}

// originalEncodingSelection rebuilds the parse selection from the base
// encoding facts (edit.rs:1570-1575).
func originalEncodingSelection(document *Document) IniEncodingSelection {
	if override := document.source.EncodingFacts().CallerOverride(); override != nil {
		return IniEncodingExplicit(*override)
	}
	return IniEncodingProfileDefault()
}

// editSourcePatchLimits maps the parse limits onto the patch limits
// (edit.rs:1592-1602).
func editSourcePatchLimits(limits IniParseLimits, operationCount int) document.SourcePatchLimits {
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

// editOperationMetadata records the frozen operation identities for the
// patch provenance (edit.rs:1604-1627).
func editOperationMetadata(tx *EditTransaction) map[string]string {
	metadata := make(map[string]string, len(tx.operations))
	for index := range tx.operations {
		operation := &tx.operations[index]
		id := "ini.edit.replace-semantic-value@1"
		switch operation.Kind {
		case EditOperationReplaceValue:
			if operation.Replacement != nil && operation.Replacement.Kind == ValueReplacementLiteral {
				id = "ini.edit.replace-literal-value@1"
			}
		case EditOperationInsertSection:
			id = "ini.edit.insert-section@1"
		case EditOperationRemoveSection:
			id = "ini.edit.remove-section@1"
		case EditOperationRenameSection:
			id = "ini.edit.rename-section@1"
		case EditOperationInsertEntry:
			id = "ini.edit.insert-entry@1"
		case EditOperationRemoveEntry:
			id = "ini.edit.remove-entry@1"
		case EditOperationRenameEntry:
			id = "ini.edit.rename-entry@1"
		}
		metadata["operation."+itoa(index)] = id
	}
	return metadata
}

// operationSummaries builds the ordered safe operation summaries
// (edit.rs:1629-1702).
func operationSummaries(tx *EditTransaction) ([]*protocol.EditOperationSummary, *EditFailure) {
	summaries := make([]*protocol.EditOperationSummary, 0, len(tx.operations))
	for index := range tx.operations {
		operation := &tx.operations[index]
		var id string
		arguments := map[string]string{}
		switch operation.Kind {
		case EditOperationReplaceValue:
			replacement := operation.Replacement
			if replacement.Kind == ValueReplacementLiteral {
				id = "ini.edit.replace-literal-value"
				arguments["literal_bytes"] = itoa(len(replacement.Literal))
			} else {
				id = "ini.edit.replace-semantic-value"
				arguments["representation_policy"] = policyName(replacement.Policy)
				arguments["value_scalars"] = itoa(strings.Count(replacement.Value, "") - 1)
			}
		case EditOperationInsertSection:
			id = "ini.edit.insert-section"
			arguments["name_scalars"] = itoa(strings.Count(operation.Name, "") - 1)
			arguments["placement"] = placementName(operation.Placement)
		case EditOperationRemoveSection:
			id = "ini.edit.remove-section"
		case EditOperationRenameSection:
			id = "ini.edit.rename-section"
			arguments["name_scalars"] = itoa(strings.Count(operation.Name, "") - 1)
		case EditOperationInsertEntry:
			id = "ini.edit.insert-entry"
			arguments["key_scalars"] = itoa(strings.Count(operation.Key, "") - 1)
			arguments["placement"] = placementName(operation.Placement)
			arguments["value_scalars"] = itoa(strings.Count(operation.Value, "") - 1)
		case EditOperationRemoveEntry:
			id = "ini.edit.remove-entry"
		case EditOperationRenameEntry:
			id = "ini.edit.rename-entry"
			arguments["key_scalars"] = itoa(strings.Count(operation.Key, "") - 1)
		}
		summary, err := protocol.NewEditOperationSummary(
			protocol.NewFormatOperationId(id, 1), arguments)
		if err != nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

// policyName renders the stable policy spelling.
func policyName(policy RepresentationPolicy) string {
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
	return "canonical-for-profile"
}

// placementName renders the stable placement spelling.
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
