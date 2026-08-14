package plist

// This file implements the plist projection targets and explicit mapping
// policies (consema-plist projection.rs; RFC 0013 §9). The default exact
// target is the versioned `plist.value-tree@1` record: one root value,
// ordered dictionary associations (string key + value), ordered array
// elements, and typed leaves — integer (signed 64-bit), real (exact IEEE
// 754 double bits), boolean, date (exact double seconds plus the fixed
// `2001-01-01T00:00:00Z` epoch constant), data (exact bytes), and string.
// There is no key sorting, date formatting, or JSON convention invention,
// and date, data, and integer never degrade through strings (hard gate 3).
//
// The explicit secondary target is `plist.projection.require-object@1`: a
// unique-key PortableValue Object over one dictionary, admitted only when
// every value is a string/integer/real/boolean and the chosen collision
// policy has no collision. UID values are never disguised as integers;
// under the explicit IncludeUid policy they project into a typed UID
// member, and otherwise fail the projection atomically. Unpaired-surrogate
// strings fail ordinary projection atomically.
//
// Projection is atomic: a recovered source, an unpaired-surrogate string,
// an unrepresentable leaf, or a resource limit returns no partial value,
// provenance, or report.

import (
	"math/big"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// The fixed XML spelling of the plist epoch, the origin of every PlistDate
// value (RFC 0013 §5.5, §9).
const plistEpochSpelling = "2001-01-01T00:00:00Z"

// ProjectionTarget is a versioned plist projection target contract.
type ProjectionTarget uint8

// The two frozen targets.
const (
	// ProjectionTargetValueTreeV1 is the exact `plist.value-tree@1` record
	// projection.
	ProjectionTargetValueTreeV1 ProjectionTarget = iota
	// ProjectionTargetRequireObjectV1 is the explicit-policy unique-key
	// Object projection over one dictionary.
	ProjectionTargetRequireObjectV1
)

// UIDPolicy is the UID handling of the value-tree target (RFC 0013 §9).
type UIDPolicy uint8

// The two frozen UID policies.
const (
	// UIDPolicyExclude: UID values fail the projection atomically.
	UIDPolicyExclude UIDPolicy = iota
	// UIDPolicyInclude: UID values project into a typed UID member and are
	// never disguised as integers.
	UIDPolicyInclude
)

// CollisionPolicy is the duplicate-key handling of the require-object
// target (RFC 0013 §9).
type CollisionPolicy uint8

// The three frozen collision policies.
const (
	// CollisionPolicyReject: the projection fails when any key repeats.
	CollisionPolicyReject CollisionPolicy = iota
	// CollisionPolicyFirst: the first occurrence in association order is
	// retained.
	CollisionPolicyFirst
	// CollisionPolicyLast: the last occurrence is retained.
	CollisionPolicyLast
)

// ProjectionLimits are the plist projection resource limits.
type ProjectionLimits struct {
	// MaxSourceNodes bounds the inspected native value nodes.
	MaxSourceNodes int
	// MaxValueNodes bounds the produced PortableValue nodes.
	MaxValueNodes int
	// MaxReportEntries bounds the report events.
	MaxReportEntries int
	// MaxProvenanceUnits bounds the projected locations plus source
	// origins.
	MaxProvenanceUnits int
}

// DefaultProjectionLimits returns the frozen defaults.
func DefaultProjectionLimits() ProjectionLimits {
	return ProjectionLimits{
		MaxSourceNodes:     2_000_000,
		MaxValueNodes:      2_000_000,
		MaxReportEntries:   100_000,
		MaxProvenanceUnits: 4_000_000,
	}
}

// ProjectionRequest is the explicit plist projection request; every policy
// is mandatory (RFC 0013 §9).
type ProjectionRequest struct {
	target    ProjectionTarget
	uid       UIDPolicy
	collision CollisionPolicy
	limits    ProjectionLimits
}

// NewProjectionRequestValueTree builds the exact `plist.value-tree@1`
// record request for the complete document.
func NewProjectionRequestValueTree() ProjectionRequest {
	return ProjectionRequest{
		target: ProjectionTargetValueTreeV1, uid: UIDPolicyExclude,
		collision: CollisionPolicyReject, limits: DefaultProjectionLimits(),
	}
}

// NewProjectionRequestValueTreeWithUID builds the exact value-tree request
// with an explicit UID policy.
func NewProjectionRequestValueTreeWithUID(policy UIDPolicy) ProjectionRequest {
	return ProjectionRequest{
		target: ProjectionTargetValueTreeV1, uid: policy,
		collision: CollisionPolicyReject, limits: DefaultProjectionLimits(),
	}
}

// NewProjectionRequestRequireObject builds the explicit require-object
// request with one duplicate-key loss policy.
func NewProjectionRequestRequireObject(collision CollisionPolicy) ProjectionRequest {
	return ProjectionRequest{
		target: ProjectionTargetRequireObjectV1, uid: UIDPolicyExclude,
		collision: collision, limits: DefaultProjectionLimits(),
	}
}

// WithLimits applies explicit resource limits to this request.
func (r ProjectionRequest) WithLimits(limits ProjectionLimits) ProjectionRequest {
	r.limits = limits
	return r
}

// Target returns the projection target.
func (r ProjectionRequest) Target() ProjectionTarget { return r.target }

// UIDPolicy returns the UID policy consumed by the value-tree target.
func (r ProjectionRequest) UIDPolicy() UIDPolicy { return r.uid }

// Collision returns the collision policy consumed by the require-object
// target.
func (r ProjectionRequest) Collision() CollisionPolicy { return r.collision }

// Limits returns the projection resource limits.
func (r ProjectionRequest) Limits() ProjectionLimits { return r.limits }

// Fidelity is the projection fidelity classification.
type Fidelity uint8

// The three frozen fidelity classes.
const (
	// FidelityExact: the target directly represents every native
	// association.
	FidelityExact Fidelity = iota
	// FidelityTransformed: an explicit reported policy transformed
	// associations.
	FidelityTransformed
	// FidelityLossy: source facts were irreversibly omitted.
	FidelityLossy
)

// String returns the stable fidelity name.
func (f Fidelity) String() string {
	switch f {
	case FidelityExact:
		return "Exact"
	case FidelityTransformed:
		return "Transformed"
	case FidelityLossy:
		return "Lossy"
	}
	return "Exact"
}

// ProjectedLocation is a projected value or association location.
type ProjectedLocation struct {
	// IsAssociation reports whether the location is an association.
	IsAssociation bool
	// Path is the portable value location of Value locations.
	Path protocol.ValuePath
	// Association is the portable association location of Association
	// locations.
	Association protocol.AssociationLocation
}

// ProvenanceRelation is the source-to-projection relation.
type ProvenanceRelation string

// The four frozen plist relations.
const (
	// ProvenanceRelationDirect is a direct native semantic origin.
	ProvenanceRelationDirect ProvenanceRelation = "Direct"
	// ProvenanceRelationDerived is a container value derived from a source
	// record.
	ProvenanceRelationDerived ProvenanceRelation = "Derived"
	// ProvenanceRelationCollapsed is a discarded occurrence related to the
	// retained projected occurrence.
	ProvenanceRelationCollapsed ProvenanceRelation = "Collapsed"
	// ProvenanceRelationReferenceDerived is semantic content derived from
	// reference resolution.
	ProvenanceRelationReferenceDerived ProvenanceRelation = "ReferenceDerived"
)

// SourceOrigin is one exact source origin.
type SourceOrigin struct {
	// Snapshot is the source document snapshot.
	Snapshot document.SnapshotIdentity
	// Node is the exact structural identity.
	Node document.NodeRef
	// Span is the exact raw source range.
	Span document.Span
	// Relation is the source relation.
	Relation ProvenanceRelation
}

// ProvenanceEntry is one many-valued provenance mapping entry.
type ProvenanceEntry struct {
	// Projected is the projected value or association.
	Projected ProjectedLocation
	// Origins are the ordered source origins.
	Origins []SourceOrigin
}

// ProvenanceMap is the immutable many-valued provenance mapping.
type ProvenanceMap struct {
	entries []ProvenanceEntry
}

// Entries returns the deterministically ordered projected locations and
// origins. The returned slice is a copy.
func (m *ProvenanceMap) Entries() []ProvenanceEntry {
	return append([]ProvenanceEntry(nil), m.entries...)
}

// ProjectionEventKind is the projection report category.
type ProjectionEventKind uint8

// The frozen event kind.
const (
	// ProjectionEventAssociationDiscarded: one duplicate-key association
	// was discarded under a First or Last collision policy.
	ProjectionEventAssociationDiscarded ProjectionEventKind = iota
)

// ProjectionEvent is one explicit transformation event.
type ProjectionEvent struct {
	// Kind is the stable event kind.
	Kind ProjectionEventKind
	// Discarded is the discarded source occurrence.
	Discarded document.NodeRef
	// Impact is the fidelity impact.
	Impact Fidelity
}

// ProjectionReport is the complete ordered projection report.
type ProjectionReport struct {
	events []ProjectionEvent
}

// Events returns the events in deterministic association order. The
// returned slice is a copy.
func (r *ProjectionReport) Events() []ProjectionEvent {
	return append([]ProjectionEvent(nil), r.events...)
}

// CompleteProjection is the complete successful projection.
type CompleteProjection struct {
	// Value is the complete immutable projected value.
	Value core.Value
	// Fidelity is the worst operation fidelity.
	Fidelity Fidelity
	// Report is the structured transformation report.
	Report ProjectionReport
	// Provenance is the value and association provenance.
	Provenance ProvenanceMap
}

// FailedProjectionAttempt is the failed projection attempt without a
// partial value.
type FailedProjectionAttempt struct {
	// Diagnostics are the stable ordered diagnostics.
	Diagnostics []*protocol.Diagnostic
	// Report is empty: failed projections publish no partial transformation
	// result.
	Report ProjectionReport
}

// ProjectionResult is the sealed projection outcome: exactly one of
// Complete or Failed is set.
type ProjectionResult struct {
	// Complete is the successful outcome.
	Complete *CompleteProjection
	// Failed is the failed attempt.
	Failed *FailedProjectionAttempt
}

// ProjectionFailureKind classifies one stable plist projection failure.
type ProjectionFailureKind uint8

// The closed projection failure classes.
const (
	// ProjectionFailureIncompleteDocument: recovered documents, or
	// documents without a provable native value, cannot publish partial
	// semantic values.
	ProjectionFailureIncompleteDocument ProjectionFailureKind = iota
	// ProjectionFailureUnpairedSurrogate: an unpaired-surrogate string or
	// key cannot enter ordinary Unicode projection.
	ProjectionFailureUnpairedSurrogate
	// ProjectionFailureCollision: a duplicate key collided under Reject.
	ProjectionFailureCollision
	// ProjectionFailureUnrepresentable: a native fact the target cannot
	// represent (require-object admission or a UID under Exclude).
	ProjectionFailureUnrepresentable
	// ProjectionFailureResourceLimit: a declared projection resource limit
	// was reached.
	ProjectionFailureResourceLimit
	// ProjectionFailureCoreInvariant: a PortableValue construction
	// invariant failed.
	ProjectionFailureCoreInvariant
)

// ProjectionFailure is the typed plist projection failure. It implements
// error and the RFC 0016 §6 Code() contract.
type ProjectionFailure struct {
	// Kind identifies the failure.
	Kind ProjectionFailureKind
	// Key is the colliding entry key of Collision.
	Key string
	// Fact is the blocking native fact of Unrepresentable.
	Fact string
	// LimitName is the stable limit name of ResourceLimit.
	LimitName string
}

// Error implements error.
func (e *ProjectionFailure) Error() string {
	switch e.Kind {
	case ProjectionFailureIncompleteDocument:
		return "plist: projection requires a complete document with a provable root value"
	case ProjectionFailureUnpairedSurrogate:
		return "plist: unpaired surrogate cannot enter ordinary Unicode projection"
	case ProjectionFailureCollision:
		return "plist: duplicate key collided under Reject: " + e.Key
	case ProjectionFailureUnrepresentable:
		return "plist: native fact is not representable by the target: " + e.Fact
	case ProjectionFailureResourceLimit:
		return "plist: projection limit " + e.LimitName + " reached"
	case ProjectionFailureCoreInvariant:
		return "plist: projection core invariant failed"
	}
	return "plist: projection failure"
}

// Code returns the frozen registered code for the failure.
func (e *ProjectionFailure) Code() string {
	switch e.Kind {
	case ProjectionFailureIncompleteDocument:
		return "plist.projection.incomplete-document@1"
	case ProjectionFailureUnpairedSurrogate:
		return "plist.projection.unpaired-surrogate@1"
	case ProjectionFailureCollision:
		return "plist.projection.collision@1"
	case ProjectionFailureUnrepresentable:
		return "plist.projection.unrepresentable@1"
	case ProjectionFailureResourceLimit:
		return "plist.projection.resource-limit@1"
	case ProjectionFailureCoreInvariant:
		return "plist.projection.core-invariant@1"
	}
	return "plist.projection.incomplete-document@1"
}

// formationStatusComplete is the document-package formation status value
// (aliased here to keep the projection condition readable).
var formationStatusComplete = document.FormationStatusComplete

// Project projects one complete plist document under one explicit target
// and policy contract (RFC 0013 §9). The projection is atomic (hard gate
// 3).
func Project(document *Document, request ProjectionRequest) ProjectionResult {
	if document.status != formationStatusComplete || document.native == nil {
		return failedProjection(&ProjectionFailure{Kind: ProjectionFailureIncompleteDocument})
	}
	span, err := document.authority.Span(0, document.source.Len())
	if err != nil {
		return failedProjection(&ProjectionFailure{Kind: ProjectionFailureCoreInvariant})
	}
	context := &projectionContext{
		document: document,
		limits:   request.limits,
		span:     span,
	}
	var value core.Value
	var fidelity Fidelity
	var failure *ProjectionFailure
	if request.target == ProjectionTargetValueTreeV1 {
		value, fidelity, failure = context.projectValueTree(request.uid)
	} else {
		value, fidelity, failure = context.projectRequireObject(request.collision)
	}
	if failure != nil {
		return failedProjection(failure)
	}
	return ProjectionResult{Complete: &CompleteProjection{
		Value: value, Fidelity: fidelity,
		Report:     ProjectionReport{events: context.report},
		Provenance: ProvenanceMap{entries: context.provenance},
	}}
}

// failedProjection builds the failed attempt with the stable diagnostic.
func failedProjection(failure *ProjectionFailure) ProjectionResult {
	arguments := map[string]string{"failure": failure.Error()}
	switch failure.Kind {
	case ProjectionFailureCollision:
		arguments["key"] = failure.Key
	case ProjectionFailureUnrepresentable:
		arguments["fact"] = failure.Fact
	case ProjectionFailureResourceLimit:
		arguments["limit"] = failure.LimitName
	}
	diagnostic := newDiagnostic(failure.Code(), protocol.CategoryProjection,
		protocol.SeverityError, nil, arguments, 0)
	return ProjectionResult{Failed: &FailedProjectionAttempt{
		Diagnostics: []*protocol.Diagnostic{diagnostic},
	}}
}

// projectionContext is the state of one projection.
type projectionContext struct {
	document    *Document
	limits      ProjectionLimits
	sourceNodes int
	valueNodes  int
	report      []ProjectionEvent
	provenance  []ProvenanceEntry
	span        document.Span
}

func (c *projectionContext) step() *ProjectionFailure {
	c.sourceNodes++
	if c.sourceNodes > c.limits.MaxSourceNodes {
		return &ProjectionFailure{Kind: ProjectionFailureResourceLimit, LimitName: "max_source_nodes"}
	}
	return nil
}

func (c *projectionContext) reserveValue(count int) *ProjectionFailure {
	c.valueNodes += count
	if c.valueNodes > c.limits.MaxValueNodes {
		return &ProjectionFailure{Kind: ProjectionFailureResourceLimit, LimitName: "max_value_nodes"}
	}
	return nil
}

func (c *projectionContext) event(kind ProjectionEventKind, discarded document.NodeRef,
	impact Fidelity) *ProjectionFailure {
	if len(c.report)+1 > c.limits.MaxReportEntries {
		return &ProjectionFailure{Kind: ProjectionFailureResourceLimit, LimitName: "max_report_entries"}
	}
	c.report = append(c.report, ProjectionEvent{Kind: kind, Discarded: discarded, Impact: impact})
	return nil
}

func (c *projectionContext) origin(projected ProjectedLocation, node document.NodeRef,
	relation ProvenanceRelation) *ProjectionFailure {
	if len(c.provenance)+1 > c.limits.MaxProvenanceUnits {
		return &ProjectionFailure{Kind: ProjectionFailureResourceLimit, LimitName: "max_provenance_units"}
	}
	c.provenance = append(c.provenance, ProvenanceEntry{
		Projected: projected,
		Origins: []SourceOrigin{{
			Snapshot: c.document.SnapshotIdentity(), Node: node,
			Span: c.span, Relation: relation,
		}},
	})
	return nil
}

func (c *projectionContext) valueNodeRef(node PlistValueRef) document.NodeRef {
	return c.document.nodeRef(node.index, document.RolePlistValue)
}

func (c *projectionContext) entryNodeRef(container PlistValueRef) document.NodeRef {
	return c.document.nodeRef(container.index, document.RolePlistDictEntry)
}

func (c *projectionContext) keyNodeRef(container PlistValueRef) document.NodeRef {
	return c.document.nodeRef(container.index, document.RolePlistKey)
}

func (c *projectionContext) elementNodeRef(container PlistValueRef) document.NodeRef {
	return c.document.nodeRef(container.index, document.RolePlistArrayElement)
}

// projectValueTree builds the exact `plist.value-tree@1` record for the
// document root (projection.rs).
func (c *projectionContext) projectValueTree(uidPolicy UIDPolicy) (core.Value, Fidelity, *ProjectionFailure) {
	native := c.document.native
	rootPath := protocol.RootValuePath().Child(protocol.ValuePathSegment{
		Kind: "ObjectValue", Key: "root"})
	rootValue, failure := c.valueOf(native, native.Root(), rootPath, uidPolicy)
	if failure != nil {
		return nil, FidelityExact, failure
	}
	if failure := c.reserveValue(1); failure != nil {
		return nil, FidelityExact, failure
	}
	record, err := core.NewObject(
		core.Entry{Key: "record", Value: core.String("plist.value-tree@1")},
		core.Entry{Key: "root", Value: rootValue},
	)
	if err != nil {
		return nil, FidelityExact, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
	}
	return record, FidelityExact, nil
}

// valueOf maps one value recursively; `path` is the location of this value
// inside the projected record.
func (c *projectionContext) valueOf(native *PlistDocument, node PlistValueRef, path protocol.ValuePath,
	uidPolicy UIDPolicy) (core.Value, *ProjectionFailure) {
	if failure := c.step(); failure != nil {
		return nil, failure
	}
	if failure := c.reserveValue(1); failure != nil {
		return nil, failure
	}
	value, ok := native.Get(node)
	if !ok {
		return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
	}
	var projected core.Value
	switch value.kind {
	case PlistValueKindDict:
		builder := core.NewEntryMappingBuilder()
		for ordinal, entry := range value.dict.entries {
			keyText, err := entry.key.ToUnicode()
			if err != nil {
				return nil, &ProjectionFailure{Kind: ProjectionFailureUnpairedSurrogate}
			}
			if failure := c.origin(ProjectedLocation{
				IsAssociation: true,
				Association: protocol.NewAssociationLocation(path, uint64(ordinal),
					protocol.AssociationRoleEntryMappingItem),
			}, c.entryNodeRef(node), ProvenanceRelationDirect); failure != nil {
				return nil, failure
			}
			if failure := c.origin(ProjectedLocation{
				Path: path.Child(protocol.ValuePathSegment{Kind: "EntryKey", Index: uint64(ordinal)}),
			}, c.keyNodeRef(node), ProvenanceRelationDirect); failure != nil {
				return nil, failure
			}
			entryPath := path.Child(protocol.ValuePathSegment{Kind: "EntryValue", Index: uint64(ordinal)})
			child, failure := c.valueOf(native, entry.value, entryPath, uidPolicy)
			if failure != nil {
				return nil, failure
			}
			if err := builder.Push(core.String(keyText), child); err != nil {
				return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
			}
		}
		projected = builder.Build()
	case PlistValueKindArray:
		items := make([]core.Value, 0, len(value.array.elements))
		for ordinal, element := range value.array.elements {
			if failure := c.origin(ProjectedLocation{
				Path: path.Child(protocol.ValuePathSegment{Kind: "SequenceElement", Index: uint64(ordinal)}),
			}, c.elementNodeRef(node), ProvenanceRelationDirect); failure != nil {
				return nil, failure
			}
			elementPath := path.Child(protocol.ValuePathSegment{Kind: "SequenceElement", Index: uint64(ordinal)})
			child, failure := c.valueOf(native, element, elementPath, uidPolicy)
			if failure != nil {
				return nil, failure
			}
			items = append(items, child)
		}
		projected = core.NewArray(items...)
	case PlistValueKindString:
		text, err := value.str.ToUnicode()
		if err != nil {
			return nil, &ProjectionFailure{Kind: ProjectionFailureUnpairedSurrogate}
		}
		projected = core.String(text)
	case PlistValueKindInteger:
		projected = core.NewInteger(big.NewInt(value.integer.value))
	case PlistValueKindReal:
		projected = core.NewBinaryFloat64(bitsOf(value.real.AsFloat64()))
	case PlistValueKindBoolean:
		projected = core.Boolean(value.boolean.value)
	case PlistValueKindDate:
		date, err := core.NewObject(
			core.Entry{Key: "epoch", Value: core.String(plistEpochSpelling)},
			core.Entry{Key: "seconds", Value: core.NewBinaryFloat64(bitsOf(value.date.seconds))},
		)
		if err != nil {
			return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
		}
		projected = date
	case PlistValueKindData:
		projected = core.NewBytes(value.data.Bytes())
	case PlistValueKindUid:
		if uidPolicy == UIDPolicyExclude {
			return nil, &ProjectionFailure{Kind: ProjectionFailureUnrepresentable, Fact: "uid"}
		}
		uid, err := core.NewObject(core.Entry{
			Key: "uid", Value: core.NewInteger(big.NewInt(int64(value.uid.value))),
		})
		if err != nil {
			return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
		}
		projected = uid
	}
	if failure := c.origin(ProjectedLocation{Path: path}, c.valueNodeRef(node),
		ProvenanceRelationDirect); failure != nil {
		return nil, failure
	}
	return projected, nil
}

// retainedOccurrence is one retained association of the require-object
// target.
type retainedOccurrence struct {
	value     core.Value
	entry     document.NodeRef
	valueNode document.NodeRef
	key       string
}

// projectRequireObject builds the unique-key Object over the document root
// dictionary under one explicit collision policy (RFC 0013 §9).
func (c *projectionContext) projectRequireObject(collision CollisionPolicy) (core.Value, Fidelity, *ProjectionFailure) {
	native := c.document.native
	root := native.Root()
	if failure := c.step(); failure != nil {
		return nil, FidelityExact, failure
	}
	if failure := c.reserveValue(1); failure != nil {
		return nil, FidelityExact, failure
	}
	rootValue, _ := native.Get(root)
	dict, isDict := rootValue.AsDict()
	if !isDict {
		return nil, FidelityExact, &ProjectionFailure{
			Kind: ProjectionFailureUnrepresentable, Fact: "root-not-dict"}
	}
	seen := map[string]int{}
	var retained []*retainedOccurrence
	var discards [][]*retainedOccurrence
	fidelity := FidelityExact
	for _, entry := range dict.Entries() {
		keyText, err := entry.key.ToUnicode()
		if err != nil {
			return nil, FidelityExact, &ProjectionFailure{Kind: ProjectionFailureUnpairedSurrogate}
		}
		valueNode, _ := native.Get(entry.value)
		if failure := c.step(); failure != nil {
			return nil, FidelityExact, failure
		}
		if failure := c.reserveValue(1); failure != nil {
			return nil, FidelityExact, failure
		}
		var scalar core.Value
		switch valueNode.kind {
		case PlistValueKindString:
			text, err := valueNode.str.ToUnicode()
			if err != nil {
				return nil, FidelityExact, &ProjectionFailure{Kind: ProjectionFailureUnpairedSurrogate}
			}
			scalar = core.String(text)
		case PlistValueKindInteger:
			scalar = core.NewInteger(big.NewInt(valueNode.integer.value))
		case PlistValueKindReal:
			scalar = core.NewBinaryFloat64(bitsOf(valueNode.real.AsFloat64()))
		case PlistValueKindBoolean:
			scalar = core.Boolean(valueNode.boolean.value)
		case PlistValueKindDate:
			return nil, FidelityExact, &ProjectionFailure{
				Kind: ProjectionFailureUnrepresentable, Fact: "date"}
		case PlistValueKindData:
			return nil, FidelityExact, &ProjectionFailure{
				Kind: ProjectionFailureUnrepresentable, Fact: "data"}
		case PlistValueKindUid:
			return nil, FidelityExact, &ProjectionFailure{
				Kind: ProjectionFailureUnrepresentable, Fact: "uid"}
		case PlistValueKindDict:
			return nil, FidelityExact, &ProjectionFailure{
				Kind: ProjectionFailureUnrepresentable, Fact: "dict"}
		case PlistValueKindArray:
			return nil, FidelityExact, &ProjectionFailure{
				Kind: ProjectionFailureUnrepresentable, Fact: "array"}
		}
		entryRef := c.entryNodeRef(root)
		valueRef := c.valueNodeRef(entry.value)
		occurrence := &retainedOccurrence{value: scalar, entry: entryRef,
			valueNode: valueRef, key: keyText}
		if position, exists := seen[keyText]; !exists {
			seen[keyText] = len(retained)
			retained = append(retained, occurrence)
			discards = append(discards, nil)
		} else {
			switch collision {
			case CollisionPolicyReject:
				return nil, FidelityExact, &ProjectionFailure{
					Kind: ProjectionFailureCollision, Key: keyText}
			case CollisionPolicyFirst:
				fidelity = FidelityTransformed
				if failure := c.event(ProjectionEventAssociationDiscarded, entryRef,
					FidelityTransformed); failure != nil {
					return nil, FidelityExact, failure
				}
				discards[position] = append(discards[position], occurrence)
			case CollisionPolicyLast:
				fidelity = FidelityTransformed
				previous := retained[position]
				if failure := c.event(ProjectionEventAssociationDiscarded, previous.entry,
					FidelityTransformed); failure != nil {
					return nil, FidelityExact, failure
				}
				discards[position] = append(discards[position], previous)
				retained[position] = occurrence
			}
		}
	}
	// Provenance follows the final retained object: every retained
	// association and key carries a Direct origin on its winning
	// occurrence, and every discarded association keeps a Collapsed origin
	// (RFC 0013 §9).
	builder := core.NewObjectBuilder()
	for position, occurrence := range retained {
		if occurrence == nil {
			continue
		}
		if err := builder.Insert(occurrence.key, occurrence.value); err != nil {
			return nil, FidelityExact, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
		}
		location := protocol.NewAssociationLocation(protocol.RootValuePath(), uint64(position),
			protocol.AssociationRoleObjectEntry)
		if failure := c.origin(ProjectedLocation{IsAssociation: true, Association: location},
			occurrence.entry, ProvenanceRelationDirect); failure != nil {
			return nil, FidelityExact, failure
		}
		keyLocation := protocol.NewAssociationLocation(protocol.RootValuePath(), uint64(position),
			protocol.AssociationRoleObjectKey)
		if failure := c.origin(ProjectedLocation{IsAssociation: true, Association: keyLocation},
			c.keyNodeRef(root), ProvenanceRelationDirect); failure != nil {
			return nil, FidelityExact, failure
		}
		if failure := c.origin(ProjectedLocation{
			Path: protocol.RootValuePath().Child(protocol.ValuePathSegment{
				Kind: "ObjectValue", Key: occurrence.key}),
		}, occurrence.valueNode, ProvenanceRelationDirect); failure != nil {
			return nil, FidelityExact, failure
		}
		for _, discarded := range discards[position] {
			if failure := c.origin(ProjectedLocation{IsAssociation: true, Association: location},
				discarded.entry, ProvenanceRelationCollapsed); failure != nil {
				return nil, FidelityExact, failure
			}
			if failure := c.origin(ProjectedLocation{
				Path: protocol.RootValuePath().Child(protocol.ValuePathSegment{
					Kind: "ObjectValue", Key: discarded.key}),
			}, discarded.valueNode, ProvenanceRelationCollapsed); failure != nil {
				return nil, FidelityExact, failure
			}
		}
	}
	return builder.Build(), fidelity, nil
}

// bitsOf returns the exact IEEE-754 binary64 bit pattern of one double.
func bitsOf(value float64) uint64 {
	return float64Bits(value)
}
