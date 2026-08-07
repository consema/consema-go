package ini

import (
	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the INI projections (consema-ini projection.rs;
// RFC 0009 §10). The default exact projection is the nested EntryMapping
// preserving every section and entry occurrence; an explicit RequireObject
// request may convert both levels to Objects only under an explicit
// comparison and collision policy. Recovered documents never project.

// ProjectionTarget is the versioned INI projection target
// (projection.rs:9-17).
type ProjectionTarget string

// The two frozen projection targets.
const (
	// TargetBestExactEntryMappingV1 is the exact nested EntryMapping
	// preserving every section and entry occurrence.
	TargetBestExactEntryMappingV1 ProjectionTarget = "BestExactEntryMappingV1"
	// TargetRequireObjectV1 is the nested unique-key Object under explicit
	// comparison and collision policy.
	TargetRequireObjectV1 ProjectionTarget = "RequireObjectV1"
)

// NameComparison is the explicit Object-name comparison (projection.rs:
// 19-27).
type NameComparison string

// The two frozen comparison modes.
const (
	// ComparisonOriginalExact compares retained original decoded spelling
	// exactly.
	ComparisonOriginalExact NameComparison = "OriginalExact"
	// ComparisonProfileEquivalent applies the selected profile's frozen
	// comparison rule.
	ComparisonProfileEquivalent NameComparison = "ProfileEquivalent"
)

// CollisionPolicy is the explicit Object collision behavior
// (projection.rs:28-38).
type CollisionPolicy string

// The three frozen collision policies.
const (
	// CollisionPolicyReject rejects every collision.
	CollisionPolicyReject CollisionPolicy = "Reject"
	// CollisionPolicyFirst retains the first occurrence in source order.
	CollisionPolicyFirst CollisionPolicy = "First"
	// CollisionPolicyLast retains the last occurrence while preserving
	// retained-source order.
	CollisionPolicyLast CollisionPolicy = "Last"
)

// ProjectionLimits are the INI projection resource limits (projection.rs:
// 102-124).
type ProjectionLimits struct {
	// MaxSourceAssociations bounds the inspected source section and entry
	// associations.
	MaxSourceAssociations int
	// MaxValueNodes bounds the produced PortableValue nodes.
	MaxValueNodes int
	// MaxReportEntries bounds the report events.
	MaxReportEntries int
	// MaxProvenanceUnits bounds the projected locations plus source
	// origins.
	MaxProvenanceUnits int
}

// DefaultProjectionLimits returns the frozen defaults (projection.rs:
// 118-124).
func DefaultProjectionLimits() ProjectionLimits {
	return ProjectionLimits{
		MaxSourceAssociations: 2_000_000,
		MaxValueNodes:         2_000_000,
		MaxReportEntries:      100_000,
		MaxProvenanceUnits:    4_000_000,
	}
}

// ProjectionRequest is the immutable explicit projection request
// (projection.rs:39-100).
type ProjectionRequest struct {
	target          ProjectionTarget
	comparison      NameComparison
	collisionPolicy CollisionPolicy
	limits          ProjectionLimits
}

// BestExactEntryMappingV1 returns the exact default request that preserves
// duplicate associations.
func BestExactEntryMappingV1() ProjectionRequest {
	return ProjectionRequest{
		target: TargetBestExactEntryMappingV1, comparison: ComparisonOriginalExact,
		collisionPolicy: CollisionPolicyReject, limits: DefaultProjectionLimits(),
	}
}

// RequireObjectV1 returns the explicit unique-Object request under one
// comparison and collision policy.
func RequireObjectV1(comparison NameComparison, collisionPolicy CollisionPolicy) ProjectionRequest {
	return ProjectionRequest{
		target: TargetRequireObjectV1, comparison: comparison,
		collisionPolicy: collisionPolicy, limits: DefaultProjectionLimits(),
	}
}

// WithLimits replaces the immutable resource limits.
func (r ProjectionRequest) WithLimits(limits ProjectionLimits) ProjectionRequest {
	r.limits = limits
	return r
}

// Target returns the frozen target contract.
func (r ProjectionRequest) Target() ProjectionTarget { return r.target }

// Comparison returns the explicit Object-name comparison.
func (r ProjectionRequest) Comparison() NameComparison { return r.comparison }

// CollisionPolicy returns the explicit Object collision policy.
func (r ProjectionRequest) CollisionPolicy() CollisionPolicy { return r.collisionPolicy }

// Limits returns the projection resource limits.
func (r ProjectionRequest) Limits() ProjectionLimits { return r.limits }

// Fidelity is the projection fidelity classification (projection.rs:
// 126-136).
type Fidelity string

// The three frozen fidelity levels.
const (
	// FidelityExact means the target directly represents every native
	// association.
	FidelityExact Fidelity = "Exact"
	// FidelityTransformed means an explicit reported collision policy
	// transformed associations.
	FidelityTransformed Fidelity = "Transformed"
	// FidelityLossy means source facts were irreversibly omitted.
	FidelityLossy Fidelity = "Lossy"
)

// ProjectedLocation is one projected value or association location
// (projection.rs:138-145).
type ProjectedLocation struct {
	// Value is the portable value location of Value locations.
	Value *protocol.ValuePath
	// Association is the portable association location of Association
	// locations.
	Association *protocol.AssociationLocation
	// Kind is "Value" or "Association".
	Kind string
}

// ValueLocation builds one value location.
func ValueLocation(path protocol.ValuePath) ProjectedLocation {
	return ProjectedLocation{Kind: "Value", Value: &path}
}

// AssociationLocation builds one association location.
func AssociationLocation(association protocol.AssociationLocation) ProjectedLocation {
	return ProjectedLocation{Kind: "Association", Association: &association}
}

// equal reports whether two projected locations are identical.
func (l ProjectedLocation) equal(other ProjectedLocation) bool {
	if l.Kind != other.Kind {
		return false
	}
	if l.Kind == "Value" {
		return l.Value.Equal(*other.Value)
	}
	return l.Association.Equal(*other.Association)
}

// ProvenanceRelation is the source-to-projection relation (projection.rs:
// 147-157).
type ProvenanceRelation string

// The five frozen provenance relations.
const (
	// RelationDirect is a direct native semantic origin.
	RelationDirect ProvenanceRelation = "Direct"
	// RelationDerived is a container value derived from a source record.
	RelationDerived ProvenanceRelation = "Derived"
	// RelationContinuationFragment is a more-indented Python physical-line
	// value fragment.
	RelationContinuationFragment ProvenanceRelation = "ContinuationFragment"
	// RelationQuoteDerived is semantic content derived by removing exact
	// Windows outer quotes.
	RelationQuoteDerived ProvenanceRelation = "QuoteDerived"
	// RelationCollapsed is a discarded association related to the retained
	// projected association.
	RelationCollapsed ProvenanceRelation = "Collapsed"
)

// SourceOrigin is one exact source origin (projection.rs:159-172).
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

// ProvenanceEntry is one many-valued provenance entry (projection.rs:
// 174-182).
type ProvenanceEntry struct {
	// Projected is the projected value or association.
	Projected ProjectedLocation
	// Origins are the ordered source origins.
	Origins []SourceOrigin
}

// ProvenanceMap is the immutable many-valued provenance mapping
// (projection.rs:184-196).
type ProvenanceMap struct {
	entries []ProvenanceEntry
}

// Entries returns the deterministically ordered entries. The returned
// slice is a copy.
func (m ProvenanceMap) Entries() []ProvenanceEntry {
	return append([]ProvenanceEntry(nil), m.entries...)
}

// ProjectionEventKind is the collision report category (projection.rs:
// 198-207).
type ProjectionEventKind string

// The two frozen event kinds.
const (
	// EventSectionCollisionCollapsed means a section association was
	// collapsed.
	EventSectionCollisionCollapsed ProjectionEventKind = "SectionCollisionCollapsed"
	// EventEntryCollisionCollapsed means an entry association was
	// collapsed.
	EventEntryCollisionCollapsed ProjectionEventKind = "EntryCollisionCollapsed"
)

// ProjectionEvent is one explicit Object collision event (projection.rs:
// 209-230).
type ProjectionEvent struct {
	// Kind is the stable event kind.
	Kind ProjectionEventKind
	// Policy is the policy that authorized the transformation.
	Policy CollisionPolicy
	// Comparison is the comparison mode that formed the collision class.
	Comparison NameComparison
	// Discarded is the discarded source occurrence.
	Discarded document.NodeRef
	// Retained is the retained source occurrence.
	Retained document.NodeRef
	// Projected is the association produced from the retained occurrence.
	Projected protocol.AssociationLocation
	// Impact is the fidelity impact.
	Impact Fidelity
}

// ProjectionReport is the complete ordered projection report
// (projection.rs:232-245).
type ProjectionReport struct {
	events []ProjectionEvent
}

// Events returns the events in deterministic source order. The returned
// slice is a copy.
func (r ProjectionReport) Events() []ProjectionEvent {
	return append([]ProjectionEvent(nil), r.events...)
}

// CompleteProjection is the complete successful projection (projection.rs:
// 247-262).
type CompleteProjection struct {
	// Value is the complete immutable nested mapping.
	Value core.Value
	// Fidelity is the worst operation fidelity.
	Fidelity Fidelity
	// Report is the structured collision report.
	Report ProjectionReport
	// Provenance is the value and association provenance.
	Provenance ProvenanceMap
}

// FailedProjectionAttempt is the failed projection without a partial value
// (projection.rs:264-271).
type FailedProjectionAttempt struct {
	// Diagnostics are the stable ordered diagnostics.
	Diagnostics []*protocol.Diagnostic
	// Report is empty: failed projections publish no partial
	// transformation result.
	Report ProjectionReport
}

// ProjectionResult is the projection completion algebra; exactly one
// outcome is meaningful (projection.rs:273-279).
type ProjectionResult struct {
	// Complete is the complete success outcome.
	Complete *CompleteProjection
	// Failed is the failure with no value or provenance map.
	Failed *FailedProjectionAttempt
}

// ProjectionFailureKind classifies a stable INI projection failure
// (projection.rs:282-299).
type ProjectionFailureKind uint8

// The stable projection failure classes.
const (
	// ProjectionFailureRecoveredDocument: recovered documents cannot
	// publish partial semantic values.
	ProjectionFailureRecoveredDocument ProjectionFailureKind = iota
	// ProjectionFailureCollision: an Object collision under Reject.
	ProjectionFailureCollision
	// ProjectionFailureResourceLimit: a declared projection resource limit
	// was reached.
	ProjectionFailureResourceLimit
	// ProjectionFailureCoreInvariant: a PortableValue construction
	// invariant failed.
	ProjectionFailureCoreInvariant
)

// ProjectionFailure is the typed INI projection failure. It implements
// error and the RFC 0016 §6 Code() contract with the frozen registered
// codes.
type ProjectionFailure struct {
	// Kind identifies the failure.
	Kind ProjectionFailureKind
	// Container is the colliding section or entry container (Collision).
	Container document.NodeRef
	// Name is the comparison name that collided (Collision).
	Name string
	// LimitName is the stable limit name of a ResourceLimit.
	LimitName string
}

// Error implements error; the text is human presentation only (RFC 0016 §6).
func (f *ProjectionFailure) Error() string {
	switch f.Kind {
	case ProjectionFailureRecoveredDocument:
		return "ini: recovered document cannot project"
	case ProjectionFailureCollision:
		return "ini: object projection collision for " + f.Name
	case ProjectionFailureResourceLimit:
		return "ini: projection resource limit " + f.LimitName + " was reached"
	case ProjectionFailureCoreInvariant:
		return "ini: projection core invariant failed"
	}
	return "ini: projection failed"
}

// Code returns the frozen registered code for the failure
// (projection.rs:886-893).
func (f *ProjectionFailure) Code() string {
	switch f.Kind {
	case ProjectionFailureRecoveredDocument:
		return "ini.projection.incomplete-document@1"
	case ProjectionFailureCollision:
		return "ini.projection.collision@1"
	case ProjectionFailureResourceLimit:
		return "core.projection.resource-limit@1"
	case ProjectionFailureCoreInvariant:
		return "core.projection.target-not-applicable@1"
	}
	return "core.projection.target-not-applicable@1"
}

// Project projects this snapshot under one explicit target and collision
// contract (projection.rs:301-314).
func (d *Document) Project(request ProjectionRequest) ProjectionResult {
	if d.formationStatus != document.FormationStatusComplete {
		return d.failed(&ProjectionFailure{Kind: ProjectionFailureRecoveredDocument})
	}
	total := len(d.sections) + len(d.entries)
	if total > request.limits.MaxSourceAssociations {
		return d.failed(&ProjectionFailure{Kind: ProjectionFailureResourceLimit,
			LimitName: "max_source_associations"})
	}
	if request.target == TargetBestExactEntryMappingV1 {
		return d.projectExact(request)
	}
	return d.projectObject(request)
}

type projectionContext struct {
	document        *Document
	request         ProjectionRequest
	provenance      ProvenanceMap
	provenanceUnits int
	report          ProjectionReport
	fidelity        Fidelity
}

// addOrigin records one provenance origin with the deterministic unit
// accounting (projection.rs:326-369).
func (c *projectionContext) addOrigin(projected ProjectedLocation, node document.NodeRef,
	span document.Span, relation ProvenanceRelation) *ProjectionFailure {
	newLocation := true
	for index := range c.provenance.entries {
		if c.provenance.entries[index].Projected.equal(projected) {
			newLocation = false
			break
		}
	}
	increment := 1
	if newLocation {
		increment = 2
	}
	c.provenanceUnits += increment
	if c.provenanceUnits > c.request.limits.MaxProvenanceUnits {
		return &ProjectionFailure{Kind: ProjectionFailureResourceLimit,
			LimitName: "max_provenance_units"}
	}
	origin := SourceOrigin{Snapshot: c.document.SnapshotIdentity(), Node: node,
		Span: span, Relation: relation}
	inserted := false
	for index := range c.provenance.entries {
		if c.provenance.entries[index].Projected.equal(projected) {
			entry := &c.provenance.entries[index]
			if relation == RelationDirect {
				entry.Origins = append([]SourceOrigin{origin}, entry.Origins...)
			} else {
				entry.Origins = append(entry.Origins, origin)
			}
			inserted = true
			break
		}
	}
	if !inserted {
		c.provenance.entries = append(c.provenance.entries, ProvenanceEntry{
			Projected: projected, Origins: []SourceOrigin{origin},
		})
	}
	return nil
}

// pushEvent records one explicit collision event (projection.rs:372-379).
func (c *projectionContext) pushEvent(event ProjectionEvent) *ProjectionFailure {
	if len(c.report.events) >= c.request.limits.MaxReportEntries {
		return &ProjectionFailure{Kind: ProjectionFailureResourceLimit,
			LimitName: "max_report_entries"}
	}
	c.report.events = append(c.report.events, event)
	c.fidelity = FidelityTransformed
	return nil
}

// addEntryValueOrigins records the value and continuation origins of one
// entry (projection.rs:381-425).
func (c *projectionContext) addEntryValueOrigins(projected ProjectedLocation,
	entryIndex int) *ProjectionFailure {
	entry := c.document.entries[entryIndex]
	relation := RelationDirect
	if entry.quoteStyle != QuoteStyleNone {
		relation = RelationQuoteDerived
	}
	if failure := c.addOrigin(projected, entry.node, entry.valueSpan, relation); failure != nil {
		return failure
	}
	logical, ok := c.document.LogicalLine(entry.logicalLine)
	if !ok {
		return nil
	}
	for _, physicalNode := range logical.PhysicalLines()[1:] {
		physical, ok := c.document.PhysicalLine(physicalNode)
		if !ok {
			continue
		}
		start := 0
		for index, piece := range c.document.index.pieces {
			if piece.span.EndByte() <= physical.contentSpan.StartByte() {
				start = index + 1
			}
		}
		for ordinal := start; ordinal < len(c.document.index.pieces); ordinal++ {
			piece := c.document.index.pieces[ordinal]
			if piece.span.StartByte() >= physical.contentSpan.EndByte() {
				break
			}
			if c.document.kinds[ordinal] == SyntaxKindEntryValue {
				if failure := c.addOrigin(projected, entry.node, piece.span,
					RelationContinuationFragment); failure != nil {
					return failure
				}
			}
		}
	}
	return nil
}

// projectExact builds the best-exact EntryMapping projection
// (projection.rs:428-537).
func (d *Document) projectExact(request ProjectionRequest) ProjectionResult {
	requiredNodes := len(d.sections)*2 + len(d.entries)*2 + 1
	if requiredNodes > request.limits.MaxValueNodes {
		return d.failed(&ProjectionFailure{Kind: ProjectionFailureResourceLimit,
			LimitName: "max_value_nodes"})
	}
	context := &projectionContext{document: d, request: request, fidelity: FidelityExact}
	root := protocol.RootValuePath()
	outer := core.NewEntryMappingBuilder()
	entriesBySection := d.groupEntries()
	for sectionOrdinal, section := range d.sections {
		sectionPath := root.Child(protocol.ValuePathSegment{Kind: "EntryValue",
			Index: uint64(sectionOrdinal)})
		outerAssociation := protocol.NewAssociationLocation(root, uint64(sectionOrdinal),
			protocol.AssociationRoleEntryMappingItem)
		if failure := context.addOrigin(AssociationLocation(outerAssociation), section.node,
			section.span, RelationDirect); failure != nil {
			return d.failed(failure)
		}
		if failure := context.addOrigin(ValueLocation(root.Child(protocol.ValuePathSegment{
			Kind: "EntryKey", Index: uint64(sectionOrdinal)})), section.node,
			section.nameSpan, RelationDirect); failure != nil {
			return d.failed(failure)
		}
		if failure := context.addOrigin(ValueLocation(sectionPath), section.node,
			section.span, RelationDerived); failure != nil {
			return d.failed(failure)
		}
		inner := core.NewEntryMappingBuilder()
		entryIndices := entriesBySection[section.node]
		for localOrdinal, entryIndex := range entryIndices {
			entry := d.entries[entryIndex]
			association := protocol.NewAssociationLocation(sectionPath, uint64(localOrdinal),
				protocol.AssociationRoleEntryMappingItem)
			if failure := context.addOrigin(AssociationLocation(association), entry.node,
				entry.span, RelationDirect); failure != nil {
				return d.failed(failure)
			}
			if failure := context.addOrigin(ValueLocation(sectionPath.Child(
				protocol.ValuePathSegment{Kind: "EntryKey", Index: uint64(localOrdinal)})),
				entry.node, entry.keySpan, RelationDirect); failure != nil {
				return d.failed(failure)
			}
			valuePath := sectionPath.Child(protocol.ValuePathSegment{Kind: "EntryValue",
				Index: uint64(localOrdinal)})
			if failure := context.addEntryValueOrigins(ValueLocation(valuePath),
				entryIndex); failure != nil {
				return d.failed(failure)
			}
			_ = inner.Push(core.String(entry.key), core.String(entry.value))
		}
		_ = outer.Push(core.String(section.name), inner.Build())
	}
	rootSpan, ok := d.span(0, d.source.Len())
	if !ok {
		return d.failed(&ProjectionFailure{Kind: ProjectionFailureCoreInvariant})
	}
	if failure := context.addOrigin(ValueLocation(root), d.rootNode, rootSpan,
		RelationDerived); failure != nil {
		return d.failed(failure)
	}
	return ProjectionResult{Complete: &CompleteProjection{
		Value: outer.Build(), Fidelity: context.fidelity,
		Report: context.report, Provenance: context.provenance,
	}}
}

// selectedSection is one retained Object section with its entry selection.
type selectedSection struct {
	sourceIndex     int
	allEntryIndices []int
	entryIndices    []int
}

// projectObject builds the explicit RequireObject projection
// (projection.rs:546-785).
func (d *Document) projectObject(request ProjectionRequest) ProjectionResult {
	sectionNames := make([]string, 0, len(d.sections))
	for _, section := range d.sections {
		sectionNames = append(sectionNames, comparisonName(d.profile, section.name,
			request.comparison, false))
	}
	retainedSections, failure := selectIndices(sectionNames, request.collisionPolicy, d.rootNode)
	if failure != nil {
		return d.failed(failure)
	}
	entriesBySection := d.groupEntries()
	selected := make([]selectedSection, 0, len(retainedSections))
	for _, sectionIndex := range retainedSections {
		entryIndices := entriesBySection[d.sections[sectionIndex].node]
		entryNames := make([]string, 0, len(entryIndices))
		for _, index := range entryIndices {
			entryNames = append(entryNames,
				comparisonName(d.profile, d.entries[index].key, request.comparison, true))
		}
		retainedLocal, failure := selectIndices(entryNames, request.collisionPolicy,
			d.sections[sectionIndex].node)
		if failure != nil {
			return d.failed(failure)
		}
		retained := make([]int, 0, len(retainedLocal))
		for _, local := range retainedLocal {
			retained = append(retained, entryIndices[local])
		}
		selected = append(selected, selectedSection{
			sourceIndex: sectionIndex, allEntryIndices: entryIndices, entryIndices: retained,
		})
	}
	retainedEntries := 0
	for _, section := range selected {
		retainedEntries += len(section.entryIndices)
	}
	requiredNodes := retainedEntries + len(selected) + 1
	if requiredNodes > request.limits.MaxValueNodes {
		return d.failed(&ProjectionFailure{Kind: ProjectionFailureResourceLimit,
			LimitName: "max_value_nodes"})
	}
	context := &projectionContext{document: d, request: request, fidelity: FidelityExact}
	root := protocol.RootValuePath()
	outer := core.NewObjectBuilder()
	retainedSectionByName := map[string]int{}
	projectedSectionOrdinal := map[int]int{}
	for projected, item := range selected {
		retainedSectionByName[sectionNames[item.sourceIndex]] = item.sourceIndex
		projectedSectionOrdinal[item.sourceIndex] = projected
	}
	for sourceIndex, section := range d.sections {
		retained := retainedSectionByName[sectionNames[sourceIndex]]
		if retained != sourceIndex {
			projectedOrdinal := projectedSectionOrdinal[retained]
			location := protocol.NewAssociationLocation(root, uint64(projectedOrdinal),
				protocol.AssociationRoleObjectEntry)
			if failure := context.pushEvent(ProjectionEvent{
				Kind: EventSectionCollisionCollapsed, Policy: request.collisionPolicy,
				Comparison: request.comparison, Discarded: section.node,
				Retained: d.sections[retained].node, Projected: location,
				Impact: FidelityTransformed,
			}); failure != nil {
				return d.failed(failure)
			}
			if failure := context.addOrigin(AssociationLocation(location), section.node,
				section.span, RelationCollapsed); failure != nil {
				return d.failed(failure)
			}
		}
	}
	for projectedOrdinal, item := range selected {
		section := d.sections[item.sourceIndex]
		sectionPath := root.Child(protocol.ValuePathSegment{Kind: "ObjectValue",
			Key: section.name})
		outerLocation := protocol.NewAssociationLocation(root, uint64(projectedOrdinal),
			protocol.AssociationRoleObjectEntry)
		if failure := context.addOrigin(AssociationLocation(outerLocation), section.node,
			section.span, RelationDirect); failure != nil {
			return d.failed(failure)
		}
		if failure := context.addOrigin(AssociationLocation(protocol.NewAssociationLocation(
			root, uint64(projectedOrdinal), protocol.AssociationRoleObjectKey)), section.node,
			section.nameSpan, RelationDirect); failure != nil {
			return d.failed(failure)
		}
		if failure := context.addOrigin(ValueLocation(sectionPath), section.node,
			section.span, RelationDerived); failure != nil {
			return d.failed(failure)
		}
		retainedEntrySet := map[int]bool{}
		for _, index := range item.entryIndices {
			retainedEntrySet[index] = true
		}
		retainedByName := map[string]int{}
		projectedEntryOrdinal := map[int]int{}
		for projected, index := range item.entryIndices {
			retainedByName[comparisonName(d.profile, d.entries[index].key,
				request.comparison, true)] = index
			projectedEntryOrdinal[index] = projected
		}
		for _, entryIndex := range item.allEntryIndices {
			if retainedEntrySet[entryIndex] {
				continue
			}
			entry := d.entries[entryIndex]
			name := comparisonName(d.profile, entry.key, request.comparison, true)
			retained := retainedByName[name]
			projected := projectedEntryOrdinal[retained]
			location := protocol.NewAssociationLocation(sectionPath, uint64(projected),
				protocol.AssociationRoleObjectEntry)
			if failure := context.pushEvent(ProjectionEvent{
				Kind: EventEntryCollisionCollapsed, Policy: request.collisionPolicy,
				Comparison: request.comparison, Discarded: entry.node,
				Retained: d.entries[retained].node, Projected: location,
				Impact: FidelityTransformed,
			}); failure != nil {
				return d.failed(failure)
			}
			if failure := context.addOrigin(AssociationLocation(location), entry.node,
				entry.span, RelationCollapsed); failure != nil {
				return d.failed(failure)
			}
		}
		inner := core.NewObjectBuilder()
		for projectedEntryOrdinal, entryIndex := range item.entryIndices {
			entry := d.entries[entryIndex]
			if failure := context.addOrigin(AssociationLocation(protocol.NewAssociationLocation(
				sectionPath, uint64(projectedEntryOrdinal),
				protocol.AssociationRoleObjectEntry)), entry.node, entry.span,
				RelationDirect); failure != nil {
				return d.failed(failure)
			}
			if failure := context.addOrigin(AssociationLocation(protocol.NewAssociationLocation(
				sectionPath, uint64(projectedEntryOrdinal),
				protocol.AssociationRoleObjectKey)), entry.node, entry.keySpan,
				RelationDirect); failure != nil {
				return d.failed(failure)
			}
			if failure := context.addEntryValueOrigins(ValueLocation(sectionPath.Child(
				protocol.ValuePathSegment{Kind: "ObjectValue", Key: entry.key})),
				entryIndex); failure != nil {
				return d.failed(failure)
			}
			if failure := inner.Insert(entry.key, core.String(entry.value)); failure != nil {
				return d.failed(&ProjectionFailure{Kind: ProjectionFailureCoreInvariant})
			}
		}
		if failure := outer.Insert(section.name, inner.Build()); failure != nil {
			return d.failed(&ProjectionFailure{Kind: ProjectionFailureCoreInvariant})
		}
	}
	rootSpan, ok := d.span(0, d.source.Len())
	if !ok {
		return d.failed(&ProjectionFailure{Kind: ProjectionFailureCoreInvariant})
	}
	if failure := context.addOrigin(ValueLocation(root), d.rootNode, rootSpan,
		RelationDerived); failure != nil {
		return d.failed(failure)
	}
	return ProjectionResult{Complete: &CompleteProjection{
		Value: outer.Build(), Fidelity: context.fidelity,
		Report: context.report, Provenance: context.provenance,
	}}
}

// selectIndices applies one collision policy to an ordered name list
// (projection.rs:787-821).
func selectIndices(names []string, policy CollisionPolicy,
	container document.NodeRef) ([]int, *ProjectionFailure) {
	counts := map[string]int{}
	for _, name := range names {
		counts[name]++
	}
	if policy == CollisionPolicyReject {
		for _, name := range names {
			if counts[name] > 1 {
				return nil, &ProjectionFailure{Kind: ProjectionFailureCollision,
					Container: container, Name: name}
			}
		}
	}
	switch policy {
	case CollisionPolicyReject, CollisionPolicyFirst:
		seen := map[string]bool{}
		var retained []int
		for index, name := range names {
			if seen[name] {
				continue
			}
			seen[name] = true
			retained = append(retained, index)
		}
		return retained, nil
	case CollisionPolicyLast:
		seen := map[string]bool{}
		var reversed []int
		for index := len(names) - 1; index >= 0; index-- {
			name := names[index]
			if seen[name] {
				continue
			}
			seen[name] = true
			reversed = append(reversed, index)
		}
		retained := make([]int, 0, len(reversed))
		for index := len(reversed) - 1; index >= 0; index-- {
			retained = append(retained, reversed[index])
		}
		return retained, nil
	}
	return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
}

// groupEntries groups entry indices by owning section (projection.rs:
// 823-829).
func (d *Document) groupEntries() map[document.NodeRef][]int {
	groups := map[document.NodeRef][]int{}
	for index, entry := range d.entries {
		groups[entry.section] = append(groups[entry.section], index)
	}
	return groups
}

// comparisonName applies the request comparison mode to one name
// (projection.rs:831-846).
func comparisonName(profile IniProfile, value string, comparison NameComparison,
	isKey bool) string {
	if comparison == ComparisonOriginalExact {
		return value
	}
	switch {
	case profile.isWindows():
		return stringsToLowerASCII(value)
	case profile.isPython() && isKey:
		return optionxform(value)
	}
	return value
}

// failed builds the failed projection attempt with the frozen diagnostic
// (projection.rs:852-884).
func (d *Document) failed(failure *ProjectionFailure) ProjectionResult {
	reason := "target-not-applicable"
	switch failure.Kind {
	case ProjectionFailureRecoveredDocument:
		reason = "incomplete-document"
	case ProjectionFailureCollision:
		reason = "collision"
	case ProjectionFailureResourceLimit:
		reason = "resource-limit"
	}
	arguments := map[string]string{
		"reason":  reason,
		"profile": d.profile.ID().ID() + "@" + u32String(d.profile.ID().Version()),
	}
	if failure.Kind == ProjectionFailureResourceLimit {
		arguments["limit"] = failure.LimitName
	}
	category := protocol.CategoryProjection
	if failure.Kind == ProjectionFailureResourceLimit {
		// The frozen registry pins core.projection.resource-limit@1 to the
		// Resource category.
		category = protocol.CategoryResource
	}
	diagnostic, err := protocol.NewDiagnostic(failure.Code(), category,
		protocol.SeverityError, nil, nil, arguments, nil, nil, 0,
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		panic("ini: unregistered projection code " + failure.Code())
	}
	return ProjectionResult{Failed: &FailedProjectionAttempt{
		Diagnostics: []*protocol.Diagnostic{diagnostic},
	}}
}

// u32String renders one unsigned integer without importing fmt.
func u32String(value uint32) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 12)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}
