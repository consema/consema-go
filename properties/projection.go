package properties

import (
	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the Java Properties projections (consema-properties
// projection.rs; RFC 0010 §11): the source-ordered best-exact EntryMapping
// and the explicit unique-Object projection under one duplicate policy.
// Any unpaired surrogate blocks the complete PortableValue projection
// atomically; no partial mapping is ever published and no code unit is
// disguised as a replacement character or Bytes.

// ProjectionTarget is the versioned Java Properties projection target
// (projection.rs:10-16).
type ProjectionTarget uint8

// The two frozen targets.
const (
	// ProjectionTargetBestExactEntryMapping is the source-ordered
	// EntryMapping preserving every association.
	ProjectionTargetBestExactEntryMapping ProjectionTarget = iota
	// ProjectionTargetRequireObject is the unique-key Object under one
	// explicit duplicate policy.
	ProjectionTargetRequireObject
)

// DuplicatePolicy is the explicit duplicate behavior of RequireObject
// (projection.rs:19-27).
type DuplicatePolicy uint8

// The three frozen duplicate policies.
const (
	// DuplicatePolicyRequireUnique rejects every duplicate key.
	DuplicatePolicyRequireUnique DuplicatePolicy = iota
	// DuplicatePolicyFirstWins retains the first occurrence in source
	// order.
	DuplicatePolicyFirstWins
	// DuplicatePolicyLastWinsJdkTable retains the last occurrence, matching
	// a newly loaded JDK Properties table.
	DuplicatePolicyLastWinsJdkTable
)

// ProjectionLimits are the Java Properties projection limits
// (projection.rs:84-106).
type ProjectionLimits struct {
	// MaxSourceAssociations is the maximum source property associations
	// inspected.
	MaxSourceAssociations int
	// MaxValueNodes is the maximum produced PortableValue nodes.
	MaxValueNodes int
	// MaxReportEntries is the maximum report events.
	MaxReportEntries int
	// MaxProvenanceUnits is the maximum projected locations plus source
	// origins.
	MaxProvenanceUnits int
}

// DefaultProjectionLimits returns the frozen defaults (2M associations,
// 4,000,001 value nodes, 100k report entries, 8M provenance units).
func DefaultProjectionLimits() ProjectionLimits {
	return ProjectionLimits{
		MaxSourceAssociations: 2_000_000,
		MaxValueNodes:         4_000_001,
		MaxReportEntries:      100_000,
		MaxProvenanceUnits:    8_000_000,
	}
}

// ProjectionRequest is the immutable explicit Properties projection
// request (projection.rs:29-82).
type ProjectionRequest struct {
	target          ProjectionTarget
	duplicatePolicy DuplicatePolicy
	limits          ProjectionLimits
}

// BestExactEntryMapping returns the exact default that preserves every
// property occurrence.
func BestExactEntryMapping() ProjectionRequest {
	return ProjectionRequest{
		target:          ProjectionTargetBestExactEntryMapping,
		duplicatePolicy: DuplicatePolicyRequireUnique,
		limits:          DefaultProjectionLimits(),
	}
}

// RequireObject returns the explicit unique Object request under one
// duplicate policy.
func RequireObject(policy DuplicatePolicy) ProjectionRequest {
	return ProjectionRequest{
		target:          ProjectionTargetRequireObject,
		duplicatePolicy: policy,
		limits:          DefaultProjectionLimits(),
	}
}

// WithLimits replaces the immutable resource limits.
func (r ProjectionRequest) WithLimits(limits ProjectionLimits) ProjectionRequest {
	r.limits = limits
	return r
}

// Target returns the frozen target contract.
func (r ProjectionRequest) Target() ProjectionTarget { return r.target }

// DuplicatePolicy returns the explicit Object duplicate policy.
func (r ProjectionRequest) DuplicatePolicy() DuplicatePolicy { return r.duplicatePolicy }

// Limits returns the projection resource limits.
func (r ProjectionRequest) Limits() ProjectionLimits { return r.limits }

// Fidelity is the projection fidelity classification
// (projection.rs:108-117). Exact < Transformed < Lossy.
type Fidelity uint8

// The three frozen fidelity classes.
const (
	// FidelityExact means the target directly represents every native
	// association.
	FidelityExact Fidelity = iota
	// FidelityTransformed means complete semantics survive an explicit
	// reversible re-encoding.
	FidelityTransformed
	// FidelityLossy means at least one source fact cannot be recovered from
	// the projected value and report.
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
	return "Fidelity"
}

// ProjectedLocation is one projected value or association location
// (projection.rs:119-127).
type ProjectedLocation struct {
	// Kind is "Value" or "Association".
	Kind string
	// Path is the value location of Value locations.
	Path protocol.ValuePath
	// Association is the association location of Association locations.
	Association *protocol.AssociationLocation
}

// ProvenanceRelation is the source-to-projection relation
// (projection.rs:128-144).
type ProvenanceRelation uint8

// The six frozen provenance relations.
const (
	// ProvenanceDirect is a direct property-association origin.
	ProvenanceDirect ProvenanceRelation = iota
	// ProvenanceDerived is a root value derived from the complete document.
	ProvenanceDerived
	// ProvenanceKeyFragment is a raw source fragment contributing to a key.
	ProvenanceKeyFragment
	// ProvenanceValueFragment is a raw source fragment contributing to a
	// value.
	ProvenanceValueFragment
	// ProvenanceEscapeDerived is an escape source spelling contributing
	// Java UTF-16 code units.
	ProvenanceEscapeDerived
	// ProvenanceCollapsed is a discarded duplicate related to the retained
	// projected association.
	ProvenanceCollapsed
)

// String returns the stable relation name.
func (r ProvenanceRelation) String() string {
	switch r {
	case ProvenanceDirect:
		return "Direct"
	case ProvenanceDerived:
		return "Derived"
	case ProvenanceKeyFragment:
		return "KeyFragment"
	case ProvenanceValueFragment:
		return "ValueFragment"
	case ProvenanceEscapeDerived:
		return "EscapeDerived"
	case ProvenanceCollapsed:
		return "Collapsed"
	}
	return "ProvenanceRelation"
}

// SourceOrigin is one exact source origin (projection.rs:146-156).
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
// 158-166).
type ProvenanceEntry struct {
	// Projected is the projected value or association.
	Projected ProjectedLocation
	// Origins are the ordered source origins.
	Origins []SourceOrigin
}

// ProvenanceMap is the immutable many-valued provenance mapping
// (projection.rs:167-179).
type ProvenanceMap struct {
	entries []ProvenanceEntry
}

// Entries returns the deterministically ordered projected locations and
// origins. The returned slice is a copy.
func (m ProvenanceMap) Entries() []ProvenanceEntry {
	return append([]ProvenanceEntry(nil), m.entries...)
}

// ProjectionEvent is one explicit duplicate-collapse event
// (projection.rs:181-196).
type ProjectionEvent struct {
	// Code is the stable event code.
	Code string
	// Policy is the policy that authorized the transformation.
	Policy DuplicatePolicy
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
// (projection.rs:198-210).
type ProjectionReport struct {
	events []ProjectionEvent
}

// Events returns the events in deterministic discarded-source order. The
// returned slice is a copy.
func (r ProjectionReport) Events() []ProjectionEvent {
	return append([]ProjectionEvent(nil), r.events...)
}

// CompleteProjection is the complete successful projection
// (projection.rs:212-223).
type CompleteProjection struct {
	// Value is the complete immutable mapping.
	Value core.Value
	// Fidelity is the worst operation fidelity.
	Fidelity Fidelity
	// Report is the structured duplicate-collapse report.
	Report ProjectionReport
	// Provenance maps the value and association locations to their source
	// origins.
	Provenance ProvenanceMap
}

// FailedProjectionAttempt is the failed projection attempt without a
// partial value (projection.rs:225-232).
type FailedProjectionAttempt struct {
	// Diagnostics are the stable ordered diagnostics.
	Diagnostics []*protocol.Diagnostic
	// Report is the empty report; failed projections publish no partial
	// transformation.
	Report ProjectionReport
}

// ProjectionResult is the projection completion algebra (projection.rs:
// 234-241). Exactly one outcome is non-nil.
type ProjectionResult struct {
	// Complete is the complete success outcome.
	Complete *CompleteProjection
	// Failed is the failure with no value or provenance map.
	Failed *FailedProjectionAttempt
}

// stringComponent is the key/value side of one Java string.
type stringComponent uint8

const (
	componentKey stringComponent = iota
	componentValue
)

// projectionFailure is the internal atomic failure classification.
type projectionFailure struct {
	kind      projectionFailureKind
	property  document.NodeRef
	component stringComponent
	retained  document.NodeRef
	duplicate document.NodeRef
	limitName string
}

type projectionFailureKind uint8

const (
	failureRecoveredDocument projectionFailureKind = iota
	failureUnpairedSurrogate
	failureDuplicateKey
	failureResourceLimit
	failureCoreInvariant
)

// Project projects this snapshot under one explicit target and duplicate
// contract (projection.rs:264-306; RFC 0010 §11). An unpaired surrogate
// or a recovered document fails atomically with a stable diagnostic and
// no partial mapping; the native Document remains Complete and queryable.
func (d *Document) Project(request ProjectionRequest) ProjectionResult {
	if d.formationStatus != document.FormationStatusComplete {
		return failedProjection(d, &projectionFailure{kind: failureRecoveredDocument})
	}
	if len(d.properties) > request.limits.MaxSourceAssociations {
		return failedProjection(d, &projectionFailure{
			kind: failureResourceLimit, limitName: "max_source_associations"})
	}
	for _, property := range d.properties {
		if property.key.status == JavaStringUnpairedSurrogate {
			return failedProjection(d, &projectionFailure{
				kind: failureUnpairedSurrogate, property: property.node, component: componentKey})
		}
		if property.value.status == JavaStringUnpairedSurrogate {
			return failedProjection(d, &projectionFailure{
				kind: failureUnpairedSurrogate, property: property.node, component: componentValue})
		}
	}
	switch request.target {
	case ProjectionTargetBestExactEntryMapping:
		complete, failure := projectExact(d, request)
		if failure != nil {
			return failedProjection(d, failure)
		}
		return ProjectionResult{Complete: complete}
	case ProjectionTargetRequireObject:
		complete, failure := projectObject(d, request)
		if failure != nil {
			return failedProjection(d, failure)
		}
		return ProjectionResult{Complete: complete}
	}
	return failedProjection(d, &projectionFailure{kind: failureCoreInvariant})
}

type projectionContext struct {
	document        *Document
	request         ProjectionRequest
	provenance      []ProvenanceEntry
	provenanceIndex map[string]int
	provenanceUnits int
	report          []ProjectionEvent
	fidelity        Fidelity
}

func newProjectionContext(document *Document, request ProjectionRequest) *projectionContext {
	return &projectionContext{
		document:        document,
		request:         request,
		provenanceIndex: make(map[string]int),
		fidelity:        FidelityExact,
	}
}

// addOrigin charges one provenance unit and appends an origin
// (projection.rs:318-362).
func (c *projectionContext) addOrigin(projected ProjectedLocation, node document.NodeRef,
	span document.Span, relation ProvenanceRelation) *projectionFailure {
	key := projectedLocationKey(projected)
	increment := 1
	if existing, ok := c.provenanceIndex[key]; ok {
		if relation == ProvenanceDirect {
			c.provenance[existing].Origins = append([]SourceOrigin{{
				Snapshot: c.document.SnapshotIdentity(), Node: node, Span: span, Relation: relation,
			}}, c.provenance[existing].Origins...)
		} else {
			c.provenance[existing].Origins = append(c.provenance[existing].Origins, SourceOrigin{
				Snapshot: c.document.SnapshotIdentity(), Node: node, Span: span, Relation: relation,
			})
		}
	} else {
		increment = 2
		c.provenanceIndex[key] = len(c.provenance)
		c.provenance = append(c.provenance, ProvenanceEntry{
			Projected: projected,
			Origins: []SourceOrigin{{
				Snapshot: c.document.SnapshotIdentity(), Node: node, Span: span, Relation: relation,
			}},
		})
	}
	c.provenanceUnits += increment
	if c.provenanceUnits > c.request.limits.MaxProvenanceUnits {
		return &projectionFailure{kind: failureResourceLimit, limitName: "max_provenance_units"}
	}
	return nil
}

// addStringOrigins records the fragment and escape origins of one key or
// value (projection.rs:364-404).
func (c *projectionContext) addStringOrigins(projected ProjectedLocation, propertyIndex int,
	component stringComponent) *projectionFailure {
	property := &c.document.properties[propertyIndex]
	fragments := property.keyFragments
	relation := ProvenanceKeyFragment
	anchor := property.keyAnchor
	if component == componentValue {
		fragments = property.valueFragments
		relation = ProvenanceValueFragment
		anchor = property.valueAnchor
	}
	if len(fragments) == 0 {
		if failure := c.addOrigin(projected, property.node, anchor, relation); failure != nil {
			return failure
		}
	} else {
		for _, span := range fragments {
			if failure := c.addOrigin(projected, property.node, span, relation); failure != nil {
				return failure
			}
		}
	}
	for _, escapeNode := range property.escapes {
		escape, err := c.document.Escape(escapeNode)
		if err != nil {
			continue
		}
		inKey := escape.inKey
		if (component == componentKey && inKey) || (component == componentValue && !inKey) {
			if failure := c.addOrigin(projected, escape.node, escape.span,
				ProvenanceEscapeDerived); failure != nil {
				return failure
			}
		}
	}
	return nil
}

// pushEvent records one collapse event and tracks the worst fidelity
// (projection.rs:406-413).
func (c *projectionContext) pushEvent(event ProjectionEvent) *projectionFailure {
	if len(c.report) >= c.request.limits.MaxReportEntries {
		return &projectionFailure{kind: failureResourceLimit, limitName: "max_report_entries"}
	}
	if event.Impact > c.fidelity {
		c.fidelity = event.Impact
	}
	c.report = append(c.report, event)
	return nil
}

func (c *projectionContext) addRootOrigin() *projectionFailure {
	span, err := c.document.authority.Span(0, c.document.source.Len())
	if err != nil {
		return &projectionFailure{kind: failureCoreInvariant}
	}
	return c.addOrigin(ProjectedLocation{Kind: "Value", Path: protocol.RootValuePath()},
		c.document.NodeRef(), span, ProvenanceDerived)
}

func projectExact(document *Document,
	request ProjectionRequest) (*CompleteProjection, *projectionFailure) {
	requiredNodes := len(document.properties)*2 + 1
	if requiredNodes > request.limits.MaxValueNodes {
		return nil, &projectionFailure{kind: failureResourceLimit, limitName: "max_value_nodes"}
	}
	context := newProjectionContext(document, request)
	root := protocol.RootValuePath()
	pairs := make([]core.EntryMappingEntry, 0, len(document.properties))
	for ordinal, property := range document.properties {
		association := protocol.NewAssociationLocation(root, uint64(ordinal),
			protocol.AssociationRoleEntryMappingItem)
		if failure := context.addOrigin(ProjectedLocation{Kind: "Association",
			Association: &association}, property.node, property.span, ProvenanceDirect); failure != nil {
			return nil, failure
		}
		keyPath := root.Child(protocol.ValuePathSegment{Kind: "EntryKey", Index: uint64(ordinal)})
		if failure := context.addStringOrigins(ProjectedLocation{Kind: "Value", Path: keyPath},
			ordinal, componentKey); failure != nil {
			return nil, failure
		}
		valuePath := root.Child(protocol.ValuePathSegment{Kind: "EntryValue", Index: uint64(ordinal)})
		if failure := context.addStringOrigins(ProjectedLocation{Kind: "Value", Path: valuePath},
			ordinal, componentValue); failure != nil {
			return nil, failure
		}
		key, _ := property.key.ToUnicode()
		value, _ := property.value.ToUnicode()
		pairs = append(pairs, core.EntryMappingEntry{Key: core.String(key), Value: core.String(value)})
	}
	if failure := context.addRootOrigin(); failure != nil {
		return nil, failure
	}
	mapping, err := core.NewEntryMapping(pairs...)
	if err != nil {
		return nil, &projectionFailure{kind: failureCoreInvariant}
	}
	return &CompleteProjection{
		Value:      mapping,
		Fidelity:   context.fidelity,
		Report:     ProjectionReport{events: context.report},
		Provenance: ProvenanceMap{entries: context.provenance},
	}, nil
}

func projectObject(document *Document,
	request ProjectionRequest) (*CompleteProjection, *projectionFailure) {
	keys := make([]string, 0, len(document.properties))
	for _, property := range document.properties {
		key, _ := property.key.ToUnicode()
		keys = append(keys, key)
	}
	retained, failure := selectIndices(document, keys, request.duplicatePolicy)
	if failure != nil {
		return nil, failure
	}
	requiredNodes := len(retained) + 1
	if requiredNodes > request.limits.MaxValueNodes {
		return nil, &projectionFailure{kind: failureResourceLimit, limitName: "max_value_nodes"}
	}
	context := newProjectionContext(document, request)
	root := protocol.RootValuePath()
	retainedSet := make(map[int]bool, len(retained))
	for _, index := range retained {
		retainedSet[index] = true
	}
	retainedByKey := make(map[string]int, len(retained))
	projectedOrdinal := make(map[int]int, len(retained))
	for projected, source := range retained {
		retainedByKey[keys[source]] = source
		projectedOrdinal[source] = projected
	}
	for sourceIndex, property := range document.properties {
		if retainedSet[sourceIndex] {
			continue
		}
		retainedIndex := retainedByKey[keys[sourceIndex]]
		location := protocol.NewAssociationLocation(root, uint64(projectedOrdinal[retainedIndex]),
			protocol.AssociationRoleObjectEntry)
		if failure := context.pushEvent(ProjectionEvent{
			Code:      "java-properties.projection.duplicate-collapsed@1",
			Policy:    request.duplicatePolicy,
			Discarded: property.node,
			Retained:  document.properties[retainedIndex].node,
			Projected: location,
			Impact:    FidelityLossy,
		}); failure != nil {
			return nil, failure
		}
		if failure := context.addOrigin(ProjectedLocation{Kind: "Association",
			Association: &location}, property.node, property.span,
			ProvenanceCollapsed); failure != nil {
			return nil, failure
		}
	}
	entries := make([]core.Entry, 0, len(retained))
	for projected, sourceIndex := range retained {
		property := &document.properties[sourceIndex]
		association := protocol.NewAssociationLocation(root, uint64(projected),
			protocol.AssociationRoleObjectEntry)
		if failure := context.addOrigin(ProjectedLocation{Kind: "Association",
			Association: &association}, property.node, property.span, ProvenanceDirect); failure != nil {
			return nil, failure
		}
		keyAssociation := protocol.NewAssociationLocation(root, uint64(projected),
			protocol.AssociationRoleObjectKey)
		if failure := context.addStringOrigins(ProjectedLocation{Kind: "Association",
			Association: &keyAssociation}, sourceIndex, componentKey); failure != nil {
			return nil, failure
		}
		valuePath := root.Child(protocol.ValuePathSegment{Kind: "ObjectValue", Key: keys[sourceIndex]})
		if failure := context.addStringOrigins(ProjectedLocation{Kind: "Value", Path: valuePath},
			sourceIndex, componentValue); failure != nil {
			return nil, failure
		}
		value, _ := property.value.ToUnicode()
		entries = append(entries, core.Entry{Key: keys[sourceIndex], Value: core.String(value)})
	}
	if failure := context.addRootOrigin(); failure != nil {
		return nil, failure
	}
	object, err := core.NewObject(entries...)
	if err != nil {
		return nil, &projectionFailure{kind: failureCoreInvariant}
	}
	return &CompleteProjection{
		Value:      object,
		Fidelity:   context.fidelity,
		Report:     ProjectionReport{events: context.report},
		Provenance: ProvenanceMap{entries: context.provenance},
	}, nil
}

// selectIndices computes the retained property ordinals under one
// duplicate policy (projection.rs:613-648).
func selectIndices(document *Document, keys []string,
	policy DuplicatePolicy) ([]int, *projectionFailure) {
	firstByKey := make(map[string]int, len(keys))
	for index, key := range keys {
		if first, ok := firstByKey[key]; ok {
			if policy == DuplicatePolicyRequireUnique {
				return nil, &projectionFailure{kind: failureDuplicateKey,
					retained:  document.properties[first].node,
					duplicate: document.properties[index].node}
			}
		} else {
			firstByKey[key] = index
		}
	}
	seen := make(map[string]bool, len(keys))
	var retained []int
	if policy == DuplicatePolicyLastWinsJdkTable {
		for index := len(keys) - 1; index >= 0; index-- {
			if seen[keys[index]] {
				continue
			}
			seen[keys[index]] = true
			retained = append(retained, index)
		}
		for left, right := 0, len(retained)-1; left < right; left, right = left+1, right-1 {
			retained[left], retained[right] = retained[right], retained[left]
		}
		return retained, nil
	}
	for index, key := range keys {
		if seen[key] {
			continue
		}
		seen[key] = true
		retained = append(retained, index)
	}
	return retained, nil
}

func failedProjection(document *Document, failure *projectionFailure) ProjectionResult {
	code := projectionFailureCode(failure)
	arguments := map[string]string{
		"reason":  projectionFailureReason(failure),
		"profile": document.profile.ID().ID() + "@" + uint32String(document.profile.ID().Version()),
	}
	var primary *protocol.SourceLocation
	switch failure.kind {
	case failureUnpairedSurrogate:
		component := "key"
		if failure.component == componentValue {
			component = "value"
		}
		arguments["component"] = component
		if ordinal, ok := propertyOrdinal(document, failure.property); ok {
			arguments["property_ordinal"] = intString(ordinal)
		}
		if property, err := document.Property(failure.property); err == nil {
			primary = &protocol.SourceLocation{
				StartByte: uint64(property.span.StartByte()),
				EndByte:   uint64(property.span.EndByte()),
			}
		}
	case failureDuplicateKey:
		if ordinal, ok := propertyOrdinal(document, failure.retained); ok {
			arguments["retained_ordinal"] = intString(ordinal)
		}
		if ordinal, ok := propertyOrdinal(document, failure.duplicate); ok {
			arguments["duplicate_ordinal"] = intString(ordinal)
		}
		if property, err := document.Property(failure.duplicate); err == nil {
			primary = &protocol.SourceLocation{
				StartByte: uint64(property.span.StartByte()),
				EndByte:   uint64(property.span.EndByte()),
			}
		}
	case failureResourceLimit:
		arguments["limit"] = failure.limitName
	}
	category := protocol.CategoryProjection
	if failure.kind == failureResourceLimit {
		// The registry pins core.projection.resource-limit@1 to the
		// resource category (error_registry.go); the diagnostic must agree.
		category = protocol.CategoryResource
	}
	diagnostic, err := protocol.NewDiagnostic(code, category,
		protocol.SeverityError, primary, nil, arguments, nil, nil, 0,
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		return ProjectionResult{Failed: &FailedProjectionAttempt{Report: ProjectionReport{}}}
	}
	return ProjectionResult{Failed: &FailedProjectionAttempt{
		Diagnostics: []*protocol.Diagnostic{diagnostic},
		Report:      ProjectionReport{},
	}}
}

func projectionFailureCode(failure *projectionFailure) string {
	switch failure.kind {
	case failureRecoveredDocument:
		return "java-properties.projection.incomplete-document@1"
	case failureUnpairedSurrogate:
		return "java-properties.projection.unpaired-surrogate@1"
	case failureDuplicateKey, failureCoreInvariant:
		return "core.projection.target-not-applicable@1"
	case failureResourceLimit:
		return "core.projection.resource-limit@1"
	}
	return "core.projection.target-not-applicable@1"
}

func projectionFailureReason(failure *projectionFailure) string {
	switch failure.kind {
	case failureRecoveredDocument:
		return "incomplete-document"
	case failureUnpairedSurrogate:
		return "unpaired-surrogate"
	case failureDuplicateKey:
		return "duplicate-key"
	case failureResourceLimit:
		return "resource-limit"
	case failureCoreInvariant:
		return "target-not-applicable"
	}
	return "target-not-applicable"
}

func propertyOrdinal(document *Document, node document.NodeRef) (int, bool) {
	for index, property := range document.properties {
		if property.node == node {
			return index, true
		}
	}
	return 0, false
}

func uint32String(value uint32) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

func intString(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}

// projectedLocationKey builds the deterministic unique key of one
// projected location.
func projectedLocationKey(location ProjectedLocation) string {
	if location.Kind == "Association" {
		key := "A" + pathKey(location.Path)
		if location.Association != nil {
			key += "|" + uint64String(location.Association.Ordinal()) + "|" +
				string(location.Association.Role())
		}
		return key
	}
	return "V" + pathKey(location.Path)
}

func pathKey(path protocol.ValuePath) string {
	key := ""
	for _, segment := range path.Segments() {
		key += segment.Kind
		key += ":"
		if segment.Kind == "ObjectValue" {
			key += segment.Key
		} else {
			key += uint64String(segment.Index)
		}
		key += ";"
	}
	return key
}

func uint64String(value uint64) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
