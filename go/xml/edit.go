package xml

// This file implements the typed XML edit operations and the atomic
// commit/dry-run pipeline (RFC 0012 §11; consema-rs/crates/consema-xml/src/edit.rs;
// RFC 0016 §5.3). V1 publishes eight versioned operations. Each operation
// targets one exact NodeRef. Placement uses one exact parent and an
// optional sibling/attribute anchor. Duplicate expanded attributes,
// invalid namespace bindings, unbound prefixes, reserved-prefix misuse,
// ancestor/self placement, stale snapshots, overlapping replacements, and
// operations that would break mixed-content or document-root invariants
// fail before commit. An edit replaces only the bytes its operations own;
// every other byte is covered by the untouched-byte proof, and a failure
// never modifies the base document.

import (
	"context"
	"sort"
	"strings"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// NameFacts is a validated element or attribute name for structural
// operations (edit.rs:58-89). The prefix must already be bound to
// `namespace` in the target's in-scope scope; the edit never guesses or
// fabricates namespace declarations.
type NameFacts struct {
	// Prefix is the prefix spelling; nil is an unprefixed name.
	Prefix *string
	// Local is the local name.
	Local string
	// Namespace is the namespace URI the prefix must resolve to; nil
	// forbids a prefix.
	Namespace *string
}

// NewNameFacts creates name facts from an already validated prefix/local
// pair.
func NewNameFacts(prefix *string, local string, namespace *string) NameFacts {
	return NameFacts{Prefix: prefix, Local: local, Namespace: namespace}
}

func (n *NameFacts) spelling() string {
	if n.Prefix != nil {
		return *n.Prefix + ":" + n.Local
	}
	return n.Local
}

// AttributePlacementKind is the closed attribute insertion placement
// inside one start tag (edit.rs:91-100).
type AttributePlacementKind uint8

// The three frozen attribute placements.
const (
	// AttributePlacementBeforeKind inserts immediately before one anchor
	// attribute.
	AttributePlacementBeforeKind AttributePlacementKind = iota
	// AttributePlacementAfterKind inserts immediately after one anchor
	// attribute.
	AttributePlacementAfterKind
	// AttributePlacementEndKind appends before the closing `>` or `/>`.
	AttributePlacementEndKind
)

// AttributePlacement is the attribute insertion placement inside one start
// tag.
type AttributePlacement struct {
	// Kind is the closed placement category.
	Kind AttributePlacementKind
	// Anchor is the exact anchor attribute of Before/After placements.
	Anchor document.NodeRef
}

// AttributePlacementBefore inserts immediately before one anchor attribute.
func AttributePlacementBefore(anchor document.NodeRef) AttributePlacement {
	return AttributePlacement{Kind: AttributePlacementBeforeKind, Anchor: anchor}
}

// AttributePlacementAfter inserts immediately after one anchor attribute.
func AttributePlacementAfter(anchor document.NodeRef) AttributePlacement {
	return AttributePlacement{Kind: AttributePlacementAfterKind, Anchor: anchor}
}

// AttributePlacementEnd appends before the closing `>` or `/>`.
func AttributePlacementEnd() AttributePlacement {
	return AttributePlacement{Kind: AttributePlacementEndKind}
}

// ContentPlacementKind is the closed content insertion placement inside
// one element (edit.rs:102-111).
type ContentPlacementKind uint8

// The three frozen content placements.
const (
	// ContentPlacementBeforeKind inserts immediately before one anchor
	// content item.
	ContentPlacementBeforeKind ContentPlacementKind = iota
	// ContentPlacementAfterKind inserts immediately after one anchor content
	// item.
	ContentPlacementAfterKind
	// ContentPlacementEndKind appends before the end tag (or after the
	// empty-element tag).
	ContentPlacementEndKind
)

// ContentPlacement is the content insertion placement inside one element.
type ContentPlacement struct {
	// Kind is the closed placement category.
	Kind ContentPlacementKind
	// Anchor is the exact anchor content item of Before/After placements.
	Anchor document.NodeRef
}

// ContentPlacementBefore inserts immediately before one anchor content
// item.
func ContentPlacementBefore(anchor document.NodeRef) ContentPlacement {
	return ContentPlacement{Kind: ContentPlacementBeforeKind, Anchor: anchor}
}

// ContentPlacementAfter inserts immediately after one anchor content item.
func ContentPlacementAfter(anchor document.NodeRef) ContentPlacement {
	return ContentPlacement{Kind: ContentPlacementAfterKind, Anchor: anchor}
}

// ContentPlacementEnd appends before the end tag (or after the
// empty-element tag).
func ContentPlacementEnd() ContentPlacement {
	return ContentPlacement{Kind: ContentPlacementEndKind}
}

// EditOperationKind is the closed XML edit operation category
// (edit.rs:112-176).
type EditOperationKind uint8

// The eight frozen operation categories.
const (
	// EditOperationReplaceText replaces one text occurrence with new
	// escaped literal content.
	EditOperationReplaceText EditOperationKind = iota
	// EditOperationInsertAttribute inserts one attribute association into
	// an element start tag.
	EditOperationInsertAttribute
	// EditOperationRemoveAttribute removes one attribute association
	// including its leading whitespace.
	EditOperationRemoveAttribute
	// EditOperationRenameAttribute renames one attribute name, preserving
	// its value.
	EditOperationRenameAttribute
	// EditOperationSetAttributeValue replaces one attribute value with new
	// escaped content.
	EditOperationSetAttributeValue
	// EditOperationInsertElement inserts one element into a parent's mixed
	// content.
	EditOperationInsertElement
	// EditOperationRemoveElement removes one element subtree including its
	// leading whitespace.
	EditOperationRemoveElement
	// EditOperationRenameElement renames one element in both its start and
	// end tags.
	EditOperationRenameElement
)

// EditOperation is one typed XML edit operation bound to an immutable base
// snapshot (edit.rs:112-176). Only the fields of the declared Kind are
// meaningful.
type EditOperation struct {
	// Kind is the closed operation category.
	Kind EditOperationKind
	// Target is the exact target NodeRef (ReplaceText, RemoveAttribute,
	// RenameAttribute, SetAttributeValue, RemoveElement, RenameElement).
	Target document.NodeRef
	// Text is the new literal character data (ReplaceText).
	Text string
	// Name is the validated name facts (InsertAttribute, RenameAttribute,
	// InsertElement, RenameElement).
	Name NameFacts
	// Value is the semantic attribute value (InsertAttribute,
	// SetAttributeValue).
	Value string
	// AttributePlacement is the attribute insertion placement
	// (InsertAttribute).
	AttributePlacement AttributePlacement
	// Content is the optional literal text content of the inserted element;
	// nil writes an empty element (InsertElement).
	Content *string
	// ContentPlacement is the content insertion placement (InsertElement).
	ContentPlacement ContentPlacement
}

// EditTransaction is the immutable transaction; every operation resolves
// against one base snapshot (edit.rs:178-197).
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
// (edit.rs:199-304).
type EditTransactionBuilder struct {
	base       document.SnapshotIdentity
	operations []EditOperation
}

// NewEditTransactionBuilder binds a new transaction to one immutable base
// document.
func NewEditTransactionBuilder(document *Document) *EditTransactionBuilder {
	return &EditTransactionBuilder{base: document.SnapshotIdentity()}
}

// ReplaceText replaces one text occurrence with new literal content.
func (b *EditTransactionBuilder) ReplaceText(target document.NodeRef, text string) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationReplaceText, Target: target, Text: text,
	})
	return b
}

// InsertAttribute inserts one attribute with explicit placement.
func (b *EditTransactionBuilder) InsertAttribute(target document.NodeRef, name NameFacts,
	value string, placement AttributePlacement) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationInsertAttribute, Target: target, Name: name, Value: value,
		AttributePlacement: placement,
	})
	return b
}

// RemoveAttribute removes one attribute association.
func (b *EditTransactionBuilder) RemoveAttribute(target document.NodeRef) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRemoveAttribute, Target: target,
	})
	return b
}

// RenameAttribute renames one attribute.
func (b *EditTransactionBuilder) RenameAttribute(target document.NodeRef, name NameFacts) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRenameAttribute, Target: target, Name: name,
	})
	return b
}

// SetAttributeValue replaces one attribute value.
func (b *EditTransactionBuilder) SetAttributeValue(target document.NodeRef, value string) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationSetAttributeValue, Target: target, Value: value,
	})
	return b
}

// InsertElement inserts one element into a parent's mixed content.
func (b *EditTransactionBuilder) InsertElement(target document.NodeRef, name NameFacts,
	content *string, placement ContentPlacement) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationInsertElement, Target: target, Name: name, Content: content,
		ContentPlacement: placement,
	})
	return b
}

// RemoveElement removes one element subtree.
func (b *EditTransactionBuilder) RemoveElement(target document.NodeRef) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRemoveElement, Target: target,
	})
	return b
}

// RenameElement renames one element.
func (b *EditTransactionBuilder) RenameElement(target document.NodeRef, name NameFacts) *EditTransactionBuilder {
	b.operations = append(b.operations, EditOperation{
		Kind: EditOperationRenameElement, Target: target, Name: name,
	})
	return b
}

// Build completes the immutable request; target validation happens
// atomically at commit.
func (b *EditTransactionBuilder) Build() *EditTransaction {
	return &EditTransaction{
		base:       b.base,
		operations: append([]EditOperation(nil), b.operations...),
	}
}

// EditCommit is the atomic edit success (edit.rs:306-317).
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
// (edit.rs:319-360).
type EditFailureKind uint8

// The closed edit failure categories.
const (
	// EditFailureWrongSnapshot: the transaction or target belongs to another
	// snapshot.
	EditFailureWrongSnapshot EditFailureKind = iota
	// EditFailureWrongRole: the target role is not the operation's expected
	// XML role.
	EditFailureWrongRole
	// EditFailureTargetNotFound: the target or anchor NodeRef does not exist
	// in this snapshot.
	EditFailureTargetNotFound
	// EditFailureIncompleteTarget: the base document is not `Complete`, so
	// no target can be edited.
	EditFailureIncompleteTarget
	// EditFailureInvalidQName: the name facts violate XML QName grammar.
	EditFailureInvalidQName
	// EditFailureUnboundPrefix: a prefixed name has no in-scope binding to
	// its promised namespace.
	EditFailureUnboundPrefix
	// EditFailureReservedPrefix: a reserved prefix or namespace was used as
	// an ordinary name.
	EditFailureReservedPrefix
	// EditFailureDuplicateExpandedAttribute: the renamed or inserted
	// attribute duplicates an expanded name.
	EditFailureDuplicateExpandedAttribute
	// EditFailureCannotRemoveRoot: the document element cannot be removed.
	EditFailureCannotRemoveRoot
	// EditFailureAncestorPlacement: an insertion targets the element itself
	// or one of its descendants.
	EditFailureAncestorPlacement
	// EditFailureConflictingEdits: two operations target the same exact
	// occurrence.
	EditFailureConflictingEdits
	// EditFailureOverlappingOwnership: two operations own the same exact
	// source interval.
	EditFailureOverlappingOwnership
	// EditFailureAncestorDescendantConflict: one operation edits an element
	// and another edits an owned descendant.
	EditFailureAncestorDescendantConflict
	// EditFailurePlacementAnchorModified: an insertion anchor is modified by
	// another operation in the transaction.
	EditFailurePlacementAnchorModified
	// EditFailureResourceLimit: a configured edit or output bound was
	// exceeded.
	EditFailureResourceLimit
	// EditFailureNewDocumentFormationFailed: the replacement document could
	// not be formed under the original limits.
	EditFailureNewDocumentFormationFailed
)

// EditFailure is the typed edit failure. It implements error and the
// RFC 0016 §6 Code() contract with the frozen registered codes.
type EditFailure struct {
	// Kind identifies the failure.
	Kind EditFailureKind
	// Prefix is the offending prefix spelling of UnboundPrefix /
	// ReservedPrefix.
	Prefix string
	// LimitName is the stable limit name of a ResourceLimit.
	LimitName string
}

// Error implements error.
func (e *EditFailure) Error() string {
	switch e.Kind {
	case EditFailureWrongSnapshot:
		return "xml: edit transaction or target belongs to another snapshot"
	case EditFailureWrongRole:
		return "xml: edit target has the wrong structural role"
	case EditFailureTargetNotFound:
		return "xml: edit target or placement anchor was not found"
	case EditFailureIncompleteTarget:
		return "xml: edits are forbidden on a recovered document"
	case EditFailureInvalidQName:
		return "xml: edit name violates XML QName grammar"
	case EditFailureUnboundPrefix:
		return "xml: edit prefix " + e.Prefix + " has no in-scope binding to its promised namespace"
	case EditFailureReservedPrefix:
		return "xml: edit prefix " + e.Prefix + " is reserved"
	case EditFailureDuplicateExpandedAttribute:
		return "xml: edit would duplicate an expanded attribute"
	case EditFailureCannotRemoveRoot:
		return "xml: the document element cannot be removed"
	case EditFailureAncestorPlacement:
		return "xml: an insertion targets the element itself or one of its descendants"
	case EditFailureConflictingEdits:
		return "xml: more than one operation targets the same exact occurrence"
	case EditFailureOverlappingOwnership:
		return "xml: prepared source ownership intervals overlap"
	case EditFailureAncestorDescendantConflict:
		return "xml: an edit targets an element and one of its owned descendants"
	case EditFailurePlacementAnchorModified:
		return "xml: an insertion anchor is modified by the same transaction"
	case EditFailureResourceLimit:
		return "xml: edit limit " + e.LimitName + " reached"
	case EditFailureNewDocumentFormationFailed:
		return "xml: replacement document could not be formed"
	}
	return "xml: edit failure"
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
	case EditFailureInvalidQName:
		return "InvalidQName"
	case EditFailureUnboundPrefix:
		return "UnboundPrefix"
	case EditFailureReservedPrefix:
		return "ReservedPrefix"
	case EditFailureDuplicateExpandedAttribute:
		return "DuplicateExpandedAttribute"
	case EditFailureCannotRemoveRoot:
		return "CannotRemoveRoot"
	case EditFailureAncestorPlacement:
		return "AncestorPlacement"
	case EditFailureConflictingEdits:
		return "ConflictingEdits"
	case EditFailureOverlappingOwnership:
		return "OverlappingOwnership"
	case EditFailureAncestorDescendantConflict:
		return "AncestorDescendantConflict"
	case EditFailurePlacementAnchorModified:
		return "PlacementAnchorModified"
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
	case EditFailureInvalidQName:
		return "core.edit.invalid-qname@1"
	case EditFailureUnboundPrefix:
		return "core.edit.unbound-prefix@1"
	case EditFailureReservedPrefix:
		return "core.edit.reserved-prefix@1"
	case EditFailureDuplicateExpandedAttribute:
		return "core.edit.duplicate-expanded-attribute@1"
	case EditFailureCannotRemoveRoot:
		return "core.edit.cannot-remove-root@1"
	case EditFailureAncestorPlacement:
		return "core.edit.ancestor-placement@1"
	case EditFailureConflictingEdits, EditFailureOverlappingOwnership,
		EditFailureAncestorDescendantConflict, EditFailurePlacementAnchorModified:
		return "core.edit.conflicting-edits@1"
	case EditFailureResourceLimit:
		return "core.edit.resource-limit@1"
	case EditFailureNewDocumentFormationFailed:
		return "core.edit.formation-failed@1"
	}
	return "core.edit.conflicting-edits@1"
}

// Commit atomically commits the structural operations. On failure the base
// document remains unchanged (edit.rs:410-570).
func (d *Document) Commit(tx *EditTransaction) (*EditCommit, *EditFailure) {
	if tx.base != d.SnapshotIdentity() {
		return nil, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	if d.status != document.FormationStatusComplete {
		return nil, &EditFailure{Kind: EditFailureIncompleteTarget}
	}
	if failure := validateDependencies(tx); failure != nil {
		return nil, failure
	}
	var prepared []preparedEdit
	for index := range tx.operations {
		edits, failure := d.prepareOperation(&tx.operations[index])
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
		if left.oldSpan == right.oldSpan ||
			(left.oldSpan.IsEmpty() && right.oldSpan.IsEmpty() &&
				left.oldSpan.StartByte() == right.oldSpan.StartByte()) {
			return nil, &EditFailure{Kind: EditFailureOverlappingOwnership}
		}
		if !left.oldSpan.IsEmpty() && !right.oldSpan.IsEmpty() &&
			left.oldSpan.EndByte() > right.oldSpan.StartByte() {
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
	if targetLength > d.parseLimits.Common.MaxSourceBytes {
		return nil, &EditFailure{Kind: EditFailureResourceLimit, LimitName: "target-bytes"}
	}
	sourceBytes := d.source.Bytes()
	rendered := make([]byte, 0, targetLength)
	cursor := 0
	for _, edit := range prepared {
		rendered = append(rendered, sourceBytes[cursor:edit.oldSpan.StartByte()]...)
		rendered = append(rendered, edit.replacement...)
		cursor = edit.oldSpan.EndByte()
	}
	rendered = append(rendered, sourceBytes[cursor:]...)
	newDocument, formationFailure := Parse(context.Background(), rendered, XmlProfileSafeV1,
		XmlEncodingProfileDefault(), d.parseLimits)
	if formationFailure != nil {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	if newDocument.status != document.FormationStatusComplete {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	delta := 0
	var sourceEdits []document.SourceEdit
	var mappings []document.NodeMapping
	mappedOld := make(map[document.NodeRef]bool)
	for _, edit := range prepared {
		newStart := edit.oldSpan.StartByte() + delta
		newEnd := newStart + len(edit.replacement)
		newSpan, err := newDocument.authority.Span(newStart, newEnd)
		if err != nil {
			return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
		}
		sourceEdits = append(sourceEdits, document.SourceEdit{
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
				if edit.mapping.plan == mappingPlanReplaced {
					if found := findNodeBySpan(newDocument, newStart, newEnd); found != nil {
						reference := *found
						newRef = &reference
						status = protocol.MappingReplaced
					} else {
						status = protocol.MappingUnmapped
						text := "reparsed-node-not-uniquely-located"
						reason = &text
					}
				} else {
					status = protocol.MappingDeleted
				}
				mappings = append(mappings, document.NodeMapping{
					Old: old, New: newRef, Status: status, Reason: reason,
				})
			}
		}
		delta += len(edit.replacement) - edit.oldSpan.Len()
	}
	changeSet := document.NewChangeSet(d.SnapshotIdentity(), newDocument.SnapshotIdentity(),
		sourceEdits, mappings, nil)
	patchLimits := sourcePatchLimits(d.parseLimits, len(sourceEdits))
	replacements := make([]document.SourceReplacement, 0, len(sourceEdits))
	for _, edit := range sourceEdits {
		original := sourceBytes[edit.OldSpan.StartByte():edit.OldSpan.EndByte()]
		replacements = append(replacements, document.NewSourceReplacement(
			edit.OldSpan.StartByte(), edit.OldSpan.EndByte(),
			[]byte(original), edit.Replacement))
	}
	patch, patchErr := document.NewSourcePatch(d.source, replacements,
		operationMetadata(tx), patchLimits)
	if patchErr != nil || !patch.TargetDigest().Equal(newDocument.source.Digest()) {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	proof, proofErr := document.CreateUntouchedByteProof(d.source, newDocument.source,
		patch.Replacements())
	if proofErr != nil {
		return nil, &EditFailure{Kind: EditFailureNewDocumentFormationFailed}
	}
	return &EditCommit{
		Document:       newDocument,
		ChangeSet:      changeSet,
		SourcePatch:    patch,
		UntouchedProof: &proof,
	}, nil
}

// DryRun fully validates and plans an edit without returning a new
// Document (edit.rs:572-588).
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

// preparedEdit is one owned source interval of the commit.
type preparedEdit struct {
	oldSpan     document.Span
	replacement []byte
	mapping     *preparedMapping
}

// preparedMapping is one old-node mapping plan of a prepared edit.
type preparedMapping struct {
	old  document.NodeRef
	plan mappingPlanKind
}

// mappingPlanKind is the closed mapping plan category.
type mappingPlanKind uint8

const (
	mappingPlanReplaced mappingPlanKind = iota
	mappingPlanDeleted
)

// validateDependencies runs the cross-operation dependency checks before
// any span is computed (edit.rs:597-641).
func validateDependencies(tx *EditTransaction) *EditFailure {
	targets := make(map[document.NodeRef]bool)
	for index := range tx.operations {
		operation := &tx.operations[index]
		var target document.NodeRef
		var anchor *document.NodeRef
		switch operation.Kind {
		case EditOperationReplaceText, EditOperationRemoveAttribute,
			EditOperationSetAttributeValue, EditOperationRemoveElement,
			EditOperationRenameAttribute, EditOperationRenameElement:
			target = operation.Target
		case EditOperationInsertAttribute:
			target = operation.Target
			if operation.AttributePlacement.Kind == AttributePlacementBeforeKind ||
				operation.AttributePlacement.Kind == AttributePlacementAfterKind {
				anchor = &operation.AttributePlacement.Anchor
			}
		case EditOperationInsertElement:
			target = operation.Target
			if operation.ContentPlacement.Kind == ContentPlacementBeforeKind ||
				operation.ContentPlacement.Kind == ContentPlacementAfterKind {
				anchor = &operation.ContentPlacement.Anchor
			}
		}
		if targets[target] {
			return &EditFailure{Kind: EditFailureConflictingEdits}
		}
		targets[target] = true
		if anchor != nil && targets[*anchor] {
			return &EditFailure{Kind: EditFailurePlacementAnchorModified}
		}
	}
	return nil
}

// prepareOperation prepares one operation into owned edits (edit.rs:745-776).
func (d *Document) prepareOperation(operation *EditOperation) ([]preparedEdit, *EditFailure) {
	switch operation.Kind {
	case EditOperationReplaceText:
		return d.prepareReplaceText(operation.Target, operation.Text)
	case EditOperationInsertAttribute:
		return d.prepareInsertAttribute(operation.Target, &operation.Name, operation.Value,
			operation.AttributePlacement)
	case EditOperationRemoveAttribute:
		return d.prepareRemoveAttribute(operation.Target)
	case EditOperationRenameAttribute:
		return d.prepareRenameAttribute(operation.Target, &operation.Name)
	case EditOperationSetAttributeValue:
		return d.prepareSetAttributeValue(operation.Target, operation.Value)
	case EditOperationInsertElement:
		return d.prepareInsertElement(operation.Target, &operation.Name, operation.Content,
			operation.ContentPlacement)
	case EditOperationRemoveElement:
		return d.prepareRemoveElement(operation.Target)
	case EditOperationRenameElement:
		return d.prepareRenameElement(operation.Target, &operation.Name)
	}
	return nil, &EditFailure{Kind: EditFailureWrongRole}
}

// charWidth is the raw bytes per decoded character under the source
// encoding (edit.rs:643-649).
func charWidth(encoding document.SourceEncoding) int {
	if encoding.Equal(document.Utf16LeEncoding()) || encoding.Equal(document.Utf16BeEncoding()) {
		return 2
	}
	return 1
}

// emptyElementTagClose reports whether the element tag ending at spanEnd is
// written with a `/>` close, probed in raw bytes (edit.rs:651-664).
func emptyElementTagClose(source []byte, spanEnd int, encoding document.SourceEncoding) bool {
	offset := spanEnd - 2*charWidth(encoding)
	if offset < 0 {
		return false
	}
	slash := offset
	if encoding.Equal(document.Utf16BeEncoding()) {
		slash = offset + 1
	}
	return slash < len(source) && source[slash] == '/'
}

// pushEncodedText appends literal text to a replacement buffer under the
// source encoding (edit.rs:666-687).
func pushEncodedText(out *[]byte, text string, encoding document.SourceEncoding) {
	switch {
	case encoding.Equal(document.Utf16LeEncoding()):
		for _, unit := range utf16Encode(text) {
			*out = append(*out, byte(unit), byte(unit>>8))
		}
	case encoding.Equal(document.Utf16BeEncoding()):
		for _, unit := range utf16Encode(text) {
			*out = append(*out, byte(unit>>8), byte(unit))
		}
	default:
		*out = append(*out, text...)
	}
}

// spellingBytes encodes one name spelling under the source encoding
// (edit.rs:696-705).
func spellingBytes(name *NameFacts, encoding document.SourceEncoding) []byte {
	var out []byte
	if name.Prefix != nil {
		pushEncodedText(&out, *name.Prefix, encoding)
		pushEncodedText(&out, ":", encoding)
	}
	pushEncodedText(&out, name.Local, encoding)
	return out
}

// qnameSpellingBytes encodes one source QName spelling under the source
// encoding (edit.rs:707-715).
func qnameSpellingBytes(qname *QNameFacts, encoding document.SourceEncoding) []byte {
	var out []byte
	if qname.Prefix != nil {
		pushEncodedText(&out, *qname.Prefix, encoding)
		pushEncodedText(&out, ":", encoding)
	}
	pushEncodedText(&out, qname.Local, encoding)
	return out
}

// escapeText escapes literal character data for text content under the
// source encoding (edit.rs:718-728).
func escapeText(text string, encoding document.SourceEncoding) []byte {
	var out []byte
	for _, c := range text {
		switch c {
		case '&':
			pushEncodedText(&out, "&amp;", encoding)
		case '<':
			pushEncodedText(&out, "&lt;", encoding)
		default:
			pushEncodedText(&out, string(c), encoding)
		}
	}
	return out
}

// escapeAttribute escapes literal text for double-quoted attribute values
// under the source encoding (edit.rs:730-743).
func escapeAttribute(text string, encoding document.SourceEncoding) []byte {
	var out []byte
	for _, c := range text {
		switch c {
		case '&':
			pushEncodedText(&out, "&amp;", encoding)
		case '<':
			pushEncodedText(&out, "&lt;", encoding)
		case '"':
			pushEncodedText(&out, "&quot;", encoding)
		default:
			pushEncodedText(&out, string(c), encoding)
		}
	}
	return out
}

// prepareReplaceText replaces one whole text occurrence (edit.rs:778-790).
func (d *Document) prepareReplaceText(target document.NodeRef, text string) ([]preparedEdit, *EditFailure) {
	textData, failure := d.textFor(target)
	if failure != nil {
		return nil, failure
	}
	encoding := d.source.EncodingFacts().Selected()
	return []preparedEdit{{
		oldSpan:     textData.Span,
		replacement: escapeText(text, encoding),
		mapping:     &preparedMapping{old: target, plan: mappingPlanReplaced},
	}}, nil
}

// prepareInsertAttribute inserts one attribute association (edit.rs:792-862).
func (d *Document) prepareInsertAttribute(target document.NodeRef, name *NameFacts,
	value string, placement AttributePlacement) ([]preparedEdit, *EditFailure) {
	element, failure := d.elementFor(target)
	if failure != nil {
		return nil, failure
	}
	if failure := validateNameFacts(name, element, true); failure != nil {
		return nil, failure
	}
	if failure := rejectDuplicateAttribute(element, name); failure != nil {
		return nil, failure
	}
	encoding := d.source.EncodingFacts().Selected()
	var insertAt int
	var replacement []byte
	switch placement.Kind {
	case AttributePlacementBeforeKind:
		anchorData, failure := d.attributeFor(placement.Anchor)
		if failure != nil {
			return nil, failure
		}
		insertAt = anchorData.Span.StartByte()
		replacement = spellingBytes(name, encoding)
		pushEncodedText(&replacement, "=\"", encoding)
		replacement = append(replacement, escapeAttribute(value, encoding)...)
		pushEncodedText(&replacement, "\" ", encoding)
	case AttributePlacementAfterKind:
		anchorData, failure := d.attributeFor(placement.Anchor)
		if failure != nil {
			return nil, failure
		}
		insertAt = anchorData.Span.EndByte()
		replacement = []byte(" ")
		replacement = append(replacement, spellingBytes(name, encoding)...)
		pushEncodedText(&replacement, "=\"", encoding)
		replacement = append(replacement, escapeAttribute(value, encoding)...)
		pushEncodedText(&replacement, "\"", encoding)
	default: // End
		emptyElement := emptyElementTagClose(d.source.Bytes(), element.Span.EndByte(), encoding)
		width := charWidth(encoding)
		insertAt = element.Span.EndByte() - width
		if emptyElement {
			insertAt -= width
		}
		replacement = []byte(" ")
		replacement = append(replacement, spellingBytes(name, encoding)...)
		pushEncodedText(&replacement, "=\"", encoding)
		replacement = append(replacement, escapeAttribute(value, encoding)...)
		pushEncodedText(&replacement, "\"", encoding)
	}
	span, err := d.authority.Span(insertAt, insertAt)
	if err != nil {
		return nil, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return []preparedEdit{{oldSpan: span, replacement: replacement}}, nil
}

// prepareRemoveAttribute removes one attribute including its leading
// whitespace (edit.rs:864-876).
func (d *Document) prepareRemoveAttribute(target document.NodeRef) ([]preparedEdit, *EditFailure) {
	attribute, failure := d.attributeFor(target)
	if failure != nil {
		return nil, failure
	}
	start := leadingWhitespaceStart(d.source.Bytes(), attribute.Span.StartByte())
	span, err := d.authority.Span(start, attribute.Span.EndByte())
	if err != nil {
		return nil, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return []preparedEdit{{
		oldSpan:     span,
		replacement: nil,
		mapping:     &preparedMapping{old: target, plan: mappingPlanDeleted},
	}}, nil
}

// prepareRenameAttribute renames one attribute name preserving its value
// (edit.rs:878-914).
func (d *Document) prepareRenameAttribute(target document.NodeRef, name *NameFacts) ([]preparedEdit, *EditFailure) {
	attribute, failure := d.attributeFor(target)
	if failure != nil {
		return nil, failure
	}
	element := d.elementOwningAttribute(attribute)
	if element == nil {
		return nil, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	if failure := validateNameFacts(name, element, true); failure != nil {
		return nil, failure
	}
	if newExpanded, failure := expandedNameForFacts(name, element); failure != nil {
		return nil, failure
	} else if newExpanded != nil {
		for _, existing := range element.Attributes {
			if existing.Ordinal == attribute.Ordinal {
				continue
			}
			if existing.Expanded != nil && existing.Expanded.Equal(*newExpanded) {
				return nil, &EditFailure{Kind: EditFailureDuplicateExpandedAttribute}
			}
		}
	}
	encoding := d.source.EncodingFacts().Selected()
	return []preparedEdit{{
		oldSpan:     attribute.QName.Span,
		replacement: spellingBytes(name, encoding),
		mapping:     &preparedMapping{old: target, plan: mappingPlanReplaced},
	}}, nil
}

// prepareSetAttributeValue replaces one attribute value (edit.rs:916-928).
func (d *Document) prepareSetAttributeValue(target document.NodeRef, value string) ([]preparedEdit, *EditFailure) {
	attribute, failure := d.attributeFor(target)
	if failure != nil {
		return nil, failure
	}
	encoding := d.source.EncodingFacts().Selected()
	return []preparedEdit{{
		oldSpan:     attribute.ValueSpan,
		replacement: escapeAttribute(value, encoding),
		mapping:     &preparedMapping{old: target, plan: mappingPlanReplaced},
	}}, nil
}

// prepareInsertElement inserts one element into a parent's mixed content
// (edit.rs:930-1007).
func (d *Document) prepareInsertElement(target document.NodeRef, name *NameFacts,
	content *string, placement ContentPlacement) ([]preparedEdit, *EditFailure) {
	element, failure := d.elementFor(target)
	if failure != nil {
		return nil, failure
	}
	if failure := validateNameFacts(name, element, false); failure != nil {
		return nil, failure
	}
	encoding := d.source.EncodingFacts().Selected()
	spelling := spellingBytes(name, encoding)
	var markup []byte
	pushEncodedText(&markup, "<", encoding)
	markup = append(markup, spelling...)
	if content != nil {
		pushEncodedText(&markup, ">", encoding)
		markup = append(markup, escapeText(*content, encoding)...)
		pushEncodedText(&markup, "</", encoding)
		markup = append(markup, spelling...)
		pushEncodedText(&markup, ">", encoding)
	} else {
		pushEncodedText(&markup, "/>", encoding)
	}
	var start, end int
	var replacement []byte
	switch placement.Kind {
	case ContentPlacementBeforeKind, ContentPlacementAfterKind:
		role, span, failure := d.contentSpanFor(placement.Anchor)
		if failure != nil {
			return nil, failure
		}
		found := false
		for _, child := range element.Children {
			if d.nodes[child].Span() == span && d.nodeRole(child) == role {
				found = true
				break
			}
		}
		if !found {
			return nil, &EditFailure{Kind: EditFailureTargetNotFound}
		}
		start = span.StartByte()
		end = span.StartByte()
		if placement.Kind == ContentPlacementAfterKind {
			start = span.EndByte()
			end = span.EndByte()
		}
		replacement = markup
	default: // End
		if len(element.Children) > 0 {
			at := d.contentExtentEnd(element.Children[len(element.Children)-1])
			start, end, replacement = at, at, markup
		} else {
			tagEnd := element.Span.EndByte()
			if emptyElementTagClose(d.source.Bytes(), tagEnd, encoding) {
				// `<root/>`: the element's own span ends after the `/>`, so
				// a zero-width insertion there would create a second root.
				// Replace the `/>` close with `>` plus the new element plus
				// a fresh `</parent-name>` close.
				var wrapped []byte
				pushEncodedText(&wrapped, ">", encoding)
				wrapped = append(wrapped, markup...)
				pushEncodedText(&wrapped, "</", encoding)
				wrapped = append(wrapped, qnameSpellingBytes(&element.QName, encoding)...)
				pushEncodedText(&wrapped, ">", encoding)
				start = tagEnd - 2*charWidth(encoding)
				end = tagEnd
				replacement = wrapped
			} else {
				// `<root></root>`: insert directly before the explicit end
				// tag.
				start = tagEnd
				end = tagEnd
				replacement = markup
			}
		}
	}
	span, err := d.authority.Span(start, end)
	if err != nil {
		return nil, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return []preparedEdit{{oldSpan: span, replacement: replacement}}, nil
}

// prepareRemoveElement removes one element subtree including its leading
// whitespace (edit.rs:1009-1030).
func (d *Document) prepareRemoveElement(target document.NodeRef) ([]preparedEdit, *EditFailure) {
	element, failure := d.elementFor(target)
	if failure != nil {
		return nil, failure
	}
	if d.root != nil && *d.root == element.Index {
		return nil, &EditFailure{Kind: EditFailureCannotRemoveRoot}
	}
	start := leadingWhitespaceStart(d.source.Bytes(), element.Span.StartByte())
	end := d.contentExtentEnd(element.Index)
	span, err := d.authority.Span(start, end)
	if err != nil {
		return nil, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return []preparedEdit{{
		oldSpan:     span,
		replacement: nil,
		mapping:     &preparedMapping{old: target, plan: mappingPlanDeleted},
	}}, nil
}

// prepareRenameElement renames one element in both its start and end tags
// (edit.rs:1032-1070).
func (d *Document) prepareRenameElement(target document.NodeRef, name *NameFacts) ([]preparedEdit, *EditFailure) {
	element, failure := d.elementFor(target)
	if failure != nil {
		return nil, failure
	}
	if failure := validateNameFacts(name, element, false); failure != nil {
		return nil, failure
	}
	encoding := d.source.EncodingFacts().Selected()
	spelling := spellingBytes(name, encoding)
	edits := []preparedEdit{{
		oldSpan:     element.QName.Span,
		replacement: append([]byte(nil), spelling...),
		mapping:     &preparedMapping{old: target, plan: mappingPlanReplaced},
	}}
	emptyElement := emptyElementTagClose(d.source.Bytes(), element.Span.EndByte(), encoding)
	if !emptyElement {
		lastChildEnd := element.Span.EndByte()
		if len(element.Children) > 0 {
			lastChildEnd = d.contentExtentEnd(element.Children[len(element.Children)-1])
		}
		width := charWidth(encoding)
		nameStart := lastChildEnd + 2*width
		endName, err := d.authority.Span(nameStart, nameStart+element.QName.Span.Len())
		if err != nil {
			return nil, &EditFailure{Kind: EditFailureTargetNotFound}
		}
		edits = append(edits, preparedEdit{oldSpan: endName, replacement: spelling})
	}
	return edits, nil
}

// elementFor resolves one element occurrence by arena index (edit.rs:1073-1087).
func (d *Document) elementFor(target document.NodeRef) (*XmlElementData, *EditFailure) {
	if target.Snapshot() != d.SnapshotIdentity() || target.Role() != document.RoleXmlElement {
		return nil, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	index := int(target.Index())
	if index >= len(d.nodes) {
		return nil, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	content := &d.nodes[index]
	if content.Kind != XmlContentElement {
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	if content.Element.Index != index {
		return nil, &EditFailure{Kind: EditFailureWrongRole}
	}
	return content.Element, nil
}

// attributeFor resolves one attribute association by ordinal
// (edit.rs:1090-1098).
func (d *Document) attributeFor(target document.NodeRef) (*XmlAttributeData, *EditFailure) {
	if target.Snapshot() != d.SnapshotIdentity() || target.Role() != document.RoleXmlAttribute {
		return nil, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	for _, content := range d.nodes {
		if content.Kind != XmlContentElement {
			continue
		}
		for _, attribute := range content.Element.Attributes {
			if attribute.Ordinal == target.Index() {
				return &attribute, nil
			}
		}
	}
	return nil, &EditFailure{Kind: EditFailureTargetNotFound}
}

// textFor resolves one text occurrence by ordinal (edit.rs:1101-1108).
func (d *Document) textFor(target document.NodeRef) (*XmlTextData, *EditFailure) {
	if target.Snapshot() != d.SnapshotIdentity() || target.Role() != document.RoleXmlText {
		return nil, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	for _, content := range d.nodes {
		if content.Kind == XmlContentText && content.Text.Ordinal == target.Index() {
			return content.Text, nil
		}
	}
	return nil, &EditFailure{Kind: EditFailureTargetNotFound}
}

// contentExtentEnd is the exact end of one content item's full extent: for
// an element child this is its closing end tag, not its start-tag end
// (edit.rs:1112-1144).
func (d *Document) contentExtentEnd(index int) int {
	content := &d.nodes[index]
	if content.Kind != XmlContentElement {
		return content.Span().EndByte()
	}
	data := content.Element
	encoding := d.source.EncodingFacts().Selected()
	width := charWidth(encoding)
	if len(data.Children) == 0 {
		if emptyElementTagClose(d.source.Bytes(), data.Span.EndByte(), encoding) {
			return data.Span.EndByte()
		}
		return data.Span.EndByte() + 2*width + data.QName.Span.Len() + width
	}
	return d.contentExtentEnd(data.Children[len(data.Children)-1]) + 2*width +
		data.QName.Span.Len() + width
}

// contentSpanFor resolves one content item span by role (edit.rs:1147-1186).
func (d *Document) contentSpanFor(target document.NodeRef) (document.NodeRole, document.Span, *EditFailure) {
	if target.Snapshot() != d.SnapshotIdentity() {
		return "", document.Span{}, &EditFailure{Kind: EditFailureWrongSnapshot}
	}
	switch target.Role() {
	case document.RoleXmlElement:
		data, failure := d.elementFor(target)
		if failure != nil {
			return "", document.Span{}, failure
		}
		return document.RoleXmlElement, data.Span, nil
	case document.RoleXmlText:
		data, failure := d.textFor(target)
		if failure != nil {
			return "", document.Span{}, failure
		}
		return document.RoleXmlText, data.Span, nil
	case document.RoleXmlCdata:
		for _, content := range d.nodes {
			if content.Kind == XmlContentCdata && content.Cdata.Ordinal == target.Index() {
				return document.RoleXmlCdata, content.Cdata.Span, nil
			}
		}
		return "", document.Span{}, &EditFailure{Kind: EditFailureTargetNotFound}
	case document.RoleXmlComment:
		for _, content := range d.nodes {
			if content.Kind == XmlContentComment && content.Comment.Ordinal == target.Index() {
				return document.RoleXmlComment, content.Comment.Span, nil
			}
		}
		return "", document.Span{}, &EditFailure{Kind: EditFailureTargetNotFound}
	case document.RoleXmlProcessingInstruction:
		for _, content := range d.nodes {
			if content.Kind == XmlContentProcessingInstruction &&
				content.ProcessingInstruction.Ordinal == target.Index() {
				return document.RoleXmlProcessingInstruction, content.ProcessingInstruction.Span, nil
			}
		}
		return "", document.Span{}, &EditFailure{Kind: EditFailureTargetNotFound}
	}
	return "", document.Span{}, &EditFailure{Kind: EditFailureWrongRole}
}

// nodeRole reports the content role of one arena index.
func (d *Document) nodeRole(index int) document.NodeRole {
	switch d.nodes[index].Kind {
	case XmlContentElement:
		return document.RoleXmlElement
	case XmlContentText:
		return document.RoleXmlText
	case XmlContentCdata:
		return document.RoleXmlCdata
	case XmlContentComment:
		return document.RoleXmlComment
	case XmlContentProcessingInstruction:
		return document.RoleXmlProcessingInstruction
	default:
		return document.RoleXmlErrorRegion
	}
}

// elementOwningAttribute finds the element owning one attribute.
func (d *Document) elementOwningAttribute(attribute *XmlAttributeData) *XmlElementData {
	for _, content := range d.nodes {
		if content.Kind != XmlContentElement {
			continue
		}
		for _, candidate := range content.Element.Attributes {
			if candidate.Ordinal == attribute.Ordinal {
				return content.Element
			}
		}
	}
	return nil
}

// validateNameFacts validates name facts against one element's in-scope
// scope (edit.rs:1189-1255).
func validateNameFacts(name *NameFacts, element *XmlElementData, attribute bool) *EditFailure {
	if name.Local == "" || strings.Contains(name.Local, ":") {
		return &EditFailure{Kind: EditFailureInvalidQName}
	}
	first := []rune(name.Local)[0]
	if (first >= '0' && first <= '9') || first == '-' {
		return &EditFailure{Kind: EditFailureInvalidQName}
	}
	switch {
	case name.Prefix == nil && name.Namespace != nil:
		if attribute {
			// An unprefixed attribute never carries a namespace.
			return &EditFailure{Kind: EditFailureUnboundPrefix, Prefix: ""}
		}
		// An unprefixed element name resolves through the default
		// namespace; it must equal the promised URI.
		defaultURI := element.Scope.lookupDefault()
		if defaultURI == nil || *defaultURI != *name.Namespace {
			return &EditFailure{Kind: EditFailureUnboundPrefix, Prefix: ""}
		}
		return nil
	case name.Prefix != nil && name.Namespace == nil:
		return &EditFailure{Kind: EditFailureUnboundPrefix, Prefix: *name.Prefix}
	case name.Prefix == nil && name.Namespace == nil:
		return nil
	default:
		prefix := *name.Prefix
		if prefix == "xmlns" {
			return &EditFailure{Kind: EditFailureReservedPrefix, Prefix: prefix}
		}
		if prefix == "xml" && *name.Namespace != XMLNamespaceURI {
			return &EditFailure{Kind: EditFailureUnboundPrefix, Prefix: prefix}
		}
		bound := ""
		for index := len(element.Scope.bindings) - 1; index >= 0; index-- {
			binding := element.Scope.bindings[index]
			if binding.Prefix != nil && *binding.Prefix == prefix {
				bound = binding.URI
				break
			}
		}
		if bound != *name.Namespace {
			return &EditFailure{Kind: EditFailureUnboundPrefix, Prefix: prefix}
		}
		return nil
	}
}

// expandedNameForFacts is the expanded name promised by name facts, when
// resolvable (edit.rs:1257-1287).
func expandedNameForFacts(name *NameFacts, element *XmlElementData) (*ExpandedName, *EditFailure) {
	if name.Namespace == nil {
		return nil, nil
	}
	if name.Prefix != nil && *name.Prefix == "xml" {
		return &ExpandedName{Namespace: stringPtr(XMLNamespaceURI), Local: name.Local}, nil
	}
	prefix := ""
	if name.Prefix != nil {
		prefix = *name.Prefix
	}
	bound := ""
	found := false
	for index := len(element.Scope.bindings) - 1; index >= 0; index-- {
		binding := element.Scope.bindings[index]
		bindingPrefix := ""
		if binding.Prefix != nil {
			bindingPrefix = *binding.Prefix
		}
		if bindingPrefix == prefix {
			bound = binding.URI
			found = true
			break
		}
	}
	if !found || bound != *name.Namespace {
		return nil, &EditFailure{Kind: EditFailureUnboundPrefix, Prefix: prefix}
	}
	return &ExpandedName{Namespace: name.Namespace, Local: name.Local}, nil
}

// rejectDuplicateAttribute rejects an attribute whose expanded name already
// exists on the element (edit.rs:1289-1306).
func rejectDuplicateAttribute(element *XmlElementData, name *NameFacts) *EditFailure {
	promised, failure := expandedNameForFacts(name, element)
	if failure != nil {
		return failure
	}
	if promised == nil {
		return nil
	}
	for _, attribute := range element.Attributes {
		if attribute.Expanded != nil && attribute.Expanded.Equal(*promised) {
			return &EditFailure{Kind: EditFailureDuplicateExpandedAttribute}
		}
	}
	return nil
}

// findNodeBySpan locates one content node by exact span (edit.rs:1309-1336).
func findNodeBySpan(document *Document, start, end int) *document.NodeRef {
	for _, content := range document.nodes {
		span := content.Span()
		if span.StartByte() != start || span.EndByte() != end {
			continue
		}
		role := document.nodeRoleNode(content)
		var ordinal uint64
		switch content.Kind {
		case XmlContentElement:
			ordinal = uint64(content.Element.Index)
		case XmlContentText:
			ordinal = content.Text.Ordinal
		case XmlContentCdata:
			ordinal = content.Cdata.Ordinal
		case XmlContentComment:
			ordinal = content.Comment.Ordinal
		case XmlContentProcessingInstruction:
			ordinal = content.ProcessingInstruction.Ordinal
		default:
			ordinal = content.ErrorRegion.Ordinal
		}
		node := document.authority.NodeRef(ordinal, role)
		return &node
	}
	return nil
}

// nodeRoleNode reports the content role of one arena node.
func (d *Document) nodeRoleNode(content XmlContent) document.NodeRole {
	switch content.Kind {
	case XmlContentElement:
		return document.RoleXmlElement
	case XmlContentText:
		return document.RoleXmlText
	case XmlContentCdata:
		return document.RoleXmlCdata
	case XmlContentComment:
		return document.RoleXmlComment
	case XmlContentProcessingInstruction:
		return document.RoleXmlProcessingInstruction
	default:
		return document.RoleXmlErrorRegion
	}
}

// leadingWhitespaceStart scans back over the leading whitespace of one
// owned span (edit.rs:1338-1344).
func leadingWhitespaceStart(source []byte, start int) int {
	cursor := start
	for cursor > 0 {
		switch source[cursor-1] {
		case ' ', '\t', '\r', '\n':
			cursor--
		default:
			return cursor
		}
	}
	return cursor
}

// sourcePatchLimits derives the patch limits from the parse limits
// (edit.rs:1346-1356).
func sourcePatchLimits(limits XmlParseLimits, operationCount int) document.SourcePatchLimits {
	maxReplacements := operationCount
	if maxReplacements < 1 {
		maxReplacements = 1
	}
	return document.SourcePatchLimits{
		Source: document.SourceLimits{
			MaxRawBytes:         limits.Common.MaxSourceBytes,
			MaxDecodedUTF8Bytes: limits.MaxDecodedUTF8Bytes,
			MaxDecodedScalars:   limits.MaxDecodedScalars,
		},
		MaxReplacements: maxReplacements,
		MaxPatchBytes:   limits.Common.MaxSourceBytes * 2,
	}
}

// operationMetadata builds the ordered operation metadata map
// (edit.rs:1358-1370).
func operationMetadata(tx *EditTransaction) map[string]string {
	metadata := make(map[string]string, len(tx.operations))
	for index, operation := range tx.operations {
		metadata["operation."+itoa(index)] = operationID(&operation)
	}
	return metadata
}

// operationID returns the stable operation identifier.
func operationID(operation *EditOperation) string {
	switch operation.Kind {
	case EditOperationReplaceText:
		return "xml.edit.replace-text@1"
	case EditOperationInsertAttribute:
		return "xml.edit.insert-attribute@1"
	case EditOperationRemoveAttribute:
		return "xml.edit.remove-attribute@1"
	case EditOperationRenameAttribute:
		return "xml.edit.rename-attribute@1"
	case EditOperationSetAttributeValue:
		return "xml.edit.set-attribute-value@1"
	case EditOperationInsertElement:
		return "xml.edit.insert-element@1"
	case EditOperationRemoveElement:
		return "xml.edit.remove-element@1"
	case EditOperationRenameElement:
		return "xml.edit.rename-element@1"
	}
	return "xml.edit.replace-text@1"
}

// operationSummaries builds the ordered safe operation summaries
// (edit.rs:1385-1435).
func operationSummaries(tx *EditTransaction) ([]*protocol.EditOperationSummary, *EditFailure) {
	var summaries []*protocol.EditOperationSummary
	for index := range tx.operations {
		operation := &tx.operations[index]
		var id string
		var arguments map[string]string
		switch operation.Kind {
		case EditOperationReplaceText:
			id = "xml.edit.replace-text"
			arguments = map[string]string{"text_bytes": itoa(len(operation.Text))}
		case EditOperationInsertAttribute:
			id = "xml.edit.insert-attribute"
			arguments = map[string]string{
				"name_bytes":  itoa(len(operation.Name.spelling())),
				"value_bytes": itoa(len(operation.Value)),
			}
		case EditOperationRemoveAttribute:
			id = "xml.edit.remove-attribute"
			arguments = map[string]string{}
		case EditOperationRenameAttribute:
			id = "xml.edit.rename-attribute"
			arguments = map[string]string{"name_bytes": itoa(len(operation.Name.spelling()))}
		case EditOperationSetAttributeValue:
			id = "xml.edit.set-attribute-value"
			arguments = map[string]string{"value_bytes": itoa(len(operation.Value))}
		case EditOperationInsertElement:
			id = "xml.edit.insert-element"
			contentBytes := 0
			if operation.Content != nil {
				contentBytes = len(*operation.Content)
			}
			arguments = map[string]string{
				"name_bytes":    itoa(len(operation.Name.spelling())),
				"content_bytes": itoa(contentBytes),
			}
		case EditOperationRemoveElement:
			id = "xml.edit.remove-element"
			arguments = map[string]string{}
		case EditOperationRenameElement:
			id = "xml.edit.rename-element"
			arguments = map[string]string{"name_bytes": itoa(len(operation.Name.spelling()))}
		}
		summary, err := protocol.NewEditOperationSummary(protocol.NewFormatOperationId(id, 1), arguments)
		if err != nil {
			return nil, &EditFailure{Kind: EditFailureInvalidQName}
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}
