package json

import (
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements JSON projection to the PortableValue model
// (consema-rs/consema-json/src/projection.rs; RFC 0016 §5.2). The conservative
// default policy is exact-or-reject: nothing is invented, and a failed
// attempt never contains a partial value.

// ProjectionTarget is a versioned projection target contract
// (projection.rs:14-24).
type ProjectionTarget uint8

// The four frozen targets.
const (
	// ProjectionTargetProjectAsObjectV1 requires every JSON object to
	// become a unique-key PortableValue Object.
	ProjectionTargetProjectAsObjectV1 ProjectionTarget = iota
	// ProjectionTargetProjectAsEntryMappingV1 requires every JSON object to
	// become an ordered EntryMapping.
	ProjectionTargetProjectAsEntryMappingV1
	// ProjectionTargetBestExactCoreV1 is the frozen exact-first core
	// selection algorithm.
	ProjectionTargetBestExactCoreV1
	// ProjectionTargetJson5BestExactCoreV1 is the JSON5 exact-first core
	// selection including frozen non-finite binary64 values.
	ProjectionTargetJson5BestExactCoreV1
)

// DuplicateKeyPolicy is the explicit duplicate member policy
// (projection.rs:26-35).
type DuplicateKeyPolicy uint8

// The three frozen duplicate policies.
const (
	// DuplicateKeyPolicyReject preserves nothing by guessing; it fails when
	// Object cannot represent duplicates.
	DuplicateKeyPolicyReject DuplicateKeyPolicy = iota
	// DuplicateKeyPolicyFirstWins retains the first member and reports every
	// collapsed later member.
	DuplicateKeyPolicyFirstWins
	// DuplicateKeyPolicyLastWins retains the last member and reports every
	// collapsed earlier member.
	DuplicateKeyPolicyLastWins
)

// ProjectionPolicyScope is the scope supported by projection policy rules
// (projection.rs:37-44).
type ProjectionPolicyScope struct {
	// Global applies to all applicable native objects.
	Global bool
	// Node is the exact snapshot-bound object NodeRef of an exact-node
	// scope.
	Node document.NodeRef
}

// ProjectionLimits are the projection resource limits
// (projection.rs:146-168).
type ProjectionLimits struct {
	// MaxValueNodes is the maximum produced PortableValue nodes.
	MaxValueNodes int
	// MaxReportEntries is the maximum report events.
	MaxReportEntries int
	// MaxProvenanceEntries is the maximum provenance locations.
	MaxProvenanceEntries int
	// MaxDepth is the maximum recursion depth.
	MaxDepth int
}

// DefaultProjectionLimits returns the frozen defaults (1M value nodes,
// 100k report entries, 2M provenance entries, depth 256).
func DefaultProjectionLimits() ProjectionLimits {
	return ProjectionLimits{
		MaxValueNodes:        1_000_000,
		MaxReportEntries:     100_000,
		MaxProvenanceEntries: 2_000_000,
		MaxDepth:             256,
	}
}

// ProjectionRequest is the immutable versioned projection request
// (projection.rs:52-72).
type ProjectionRequest struct {
	target         ProjectionTarget
	duplicateRules []duplicateRule
	limits         ProjectionLimits
}

// Target returns the target contract.
func (r *ProjectionRequest) Target() ProjectionTarget { return r.target }

// Limits returns the projection resource limits.
func (r *ProjectionRequest) Limits() ProjectionLimits { return r.limits }

// duplicateRule is one policy rule.
type duplicateRule struct {
	scope  ProjectionPolicyScope
	policy DuplicateKeyPolicy
}

// ProjectionRequestBuilder builds an immutable request and rejects
// conflicting equal-precedence rules (projection.rs:74-144).
type ProjectionRequestBuilder struct {
	target         ProjectionTarget
	duplicateRules []duplicateRule
	limits         ProjectionLimits
}

// NewProjectionRequestBuilder starts with exact-or-reject behavior.
func NewProjectionRequestBuilder(target ProjectionTarget) *ProjectionRequestBuilder {
	return &ProjectionRequestBuilder{
		target: target,
		duplicateRules: []duplicateRule{{
			scope:  ProjectionPolicyScope{Global: true},
			policy: DuplicateKeyPolicyReject,
		}},
		limits: DefaultProjectionLimits(),
	}
}

// GlobalDuplicatePolicy replaces the global duplicate policy.
func (b *ProjectionRequestBuilder) GlobalDuplicatePolicy(policy DuplicateKeyPolicy) *ProjectionRequestBuilder {
	retained := b.duplicateRules[:0]
	for _, rule := range b.duplicateRules {
		if !rule.scope.Global {
			retained = append(retained, rule)
		}
	}
	retained = append(retained, duplicateRule{
		scope:  ProjectionPolicyScope{Global: true},
		policy: policy,
	})
	b.duplicateRules = retained
	return b
}

// ExactNodeDuplicatePolicy adds an exact-node override.
func (b *ProjectionRequestBuilder) ExactNodeDuplicatePolicy(node document.NodeRef,
	policy DuplicateKeyPolicy) *ProjectionRequestBuilder {
	b.duplicateRules = append(b.duplicateRules, duplicateRule{
		scope:  ProjectionPolicyScope{Node: node},
		policy: policy,
	})
	return b
}

// Limits sets the immutable resource limits.
func (b *ProjectionRequestBuilder) Limits(limits ProjectionLimits) *ProjectionRequestBuilder {
	b.limits = limits
	return b
}

// Build validates rule precedence and completes the request
// (projection.rs:130-143).
func (b *ProjectionRequestBuilder) Build() (*ProjectionRequest, *ProjectionFailure) {
	for index, left := range b.duplicateRules {
		for _, right := range b.duplicateRules[index+1:] {
			if sameScope(left.scope, right.scope) && left.policy != right.policy {
				return nil, &ProjectionFailure{Kind: ProjectionFailureConflictingPolicyRules}
			}
		}
	}
	return &ProjectionRequest{
		target:         b.target,
		duplicateRules: append([]duplicateRule(nil), b.duplicateRules...),
		limits:         b.limits,
	}, nil
}

// sameScope reports whether two scopes are identical.
func sameScope(left, right ProjectionPolicyScope) bool {
	if left.Global || right.Global {
		return left.Global && right.Global
	}
	return left.Node == right.Node
}

// Fidelity is the projection fidelity classification (projection.rs:171-179).
type Fidelity uint8

// The three frozen fidelity classes.
const (
	// FidelityExact means the target directly and completely represents the
	// covered native semantics.
	FidelityExact Fidelity = iota
	// FidelityTransformed means complete semantics survive an explicit
	// reversible re-encoding.
	FidelityTransformed
	// FidelityLossy means at least one source fact cannot be recovered.
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

// ProjectedLocation is a projected value or association location
// (projection.rs:181-188).
type ProjectedLocation struct {
	// IsAssociation reports whether the location is an association.
	IsAssociation bool
	// Path is the portable value location of Value locations.
	Path protocol.ValuePath
	// Association is the portable association location of Association
	// locations.
	Association protocol.AssociationLocation
}

// ProvenanceRelation is the source-to-projection relation
// (projection.rs:190-204).
type ProvenanceRelation string

// The five frozen relations.
const (
	// ProvenanceRelationDirect is a direct native semantic origin.
	ProvenanceRelationDirect ProvenanceRelation = "Direct"
	// ProvenanceRelationDerived is derived without a one-to-one literal
	// origin.
	ProvenanceRelationDerived ProvenanceRelation = "Derived"
	// ProvenanceRelationExpanded is a reference expansion origin.
	ProvenanceRelationExpanded ProvenanceRelation = "Expanded"
	// ProvenanceRelationMerged means multiple sources were merged.
	ProvenanceRelationMerged ProvenanceRelation = "Merged"
	// ProvenanceRelationGenerated has no source origin.
	ProvenanceRelationGenerated ProvenanceRelation = "Generated"
)

// SourceOrigin is one exact source origin (projection.rs:205-217).
type SourceOrigin struct {
	// Snapshot is the source document snapshot.
	Snapshot document.SnapshotIdentity
	// Node is the exact structural identity.
	Node document.NodeRef
	// Span is the exact source range.
	Span document.Span
	// Relation is the source relation.
	Relation ProvenanceRelation
}

// ProvenanceEntry is one many-valued provenance mapping entry
// (projection.rs:219-225).
type ProvenanceEntry struct {
	// Projected is the projected value or association.
	Projected ProjectedLocation
	// Origins are the zero or more source origins.
	Origins []SourceOrigin
}

// ProvenanceMap is the immutable multi-map from projected locations to
// source origins (projection.rs:227-239).
type ProvenanceMap struct {
	entries []ProvenanceEntry
}

// Entries returns the deterministically generated entries. The returned
// slice is a copy.
func (m *ProvenanceMap) Entries() []ProvenanceEntry {
	return append([]ProvenanceEntry(nil), m.entries...)
}

// ProjectionEventKind is the machine-readable projection event category
// (projection.rs:241-257).
type ProjectionEventKind string

// The six frozen event categories.
const (
	// ProjectionEventStructureReencoded means an object was reversibly
	// represented as an EntryMapping.
	ProjectionEventStructureReencoded ProjectionEventKind = "StructureReencoded"
	// ProjectionEventTypeMapped means a native/core type mapping was
	// explicit.
	ProjectionEventTypeMapped ProjectionEventKind = "TypeMapped"
	// ProjectionEventDuplicateCollapsed means a duplicate member was
	// collapsed.
	ProjectionEventDuplicateCollapsed ProjectionEventKind = "DuplicateCollapsed"
	// ProjectionEventKeyStringified means a key was stringified (not
	// authorized by JSON v1 policies).
	ProjectionEventKeyStringified ProjectionEventKind = "KeyStringified"
	// ProjectionEventValueRounded means a value was rounded (not authorized
	// by JSON v1 policies).
	ProjectionEventValueRounded ProjectionEventKind = "ValueRounded"
	// ProjectionEventFieldDropped means a field was dropped.
	ProjectionEventFieldDropped ProjectionEventKind = "FieldDropped"
)

// ProjectionEvent is one structured projection report event
// (projection.rs:259-277).
type ProjectionEvent struct {
	// Kind is the stable event kind.
	Kind ProjectionEventKind
	// Policy is the policy rule that authorized it.
	Policy *DuplicateKeyPolicy
	// Source is the exact source identity.
	Source document.NodeRef
	// Projected is the result location when one exists.
	Projected *ProjectedLocation
	// OldCategory is the stable old semantic category.
	OldCategory string
	// NewCategory is the stable new semantic category.
	NewCategory string
	// Reversible reports whether the source fact can be recovered from
	// output plus contract.
	Reversible bool
	// Loss is the fidelity impact.
	Loss Fidelity
}

// ProjectionReport is the complete ordered projection report
// (projection.rs:279-291).
type ProjectionReport struct {
	events []ProjectionEvent
}

// Events returns the events in source/operation order. The returned slice
// is a copy.
func (r *ProjectionReport) Events() []ProjectionEvent {
	return append([]ProjectionEvent(nil), r.events...)
}

// CompleteProjection is the complete successful projection; its value is
// never partial (projection.rs:293-305).
type CompleteProjection struct {
	// Value is the complete immutable value.
	Value core.Value
	// Fidelity is the worst fidelity of the whole operation.
	Fidelity Fidelity
	// Report is the machine-readable transformation/loss report.
	Report ProjectionReport
	// Provenance is the basic value and association provenance.
	Provenance ProvenanceMap
}

// FailedProjectionAttempt is a failed attempt without a partial
// PortableValue (projection.rs:307-315).
type FailedProjectionAttempt struct {
	// Diagnostics are the ordered operation diagnostics.
	Diagnostics []*protocol.Diagnostic
	// Report holds the events discovered before the failed completion
	// check.
	Report ProjectionReport
	// PartialAnalysis holds the stable path descriptions of locally
	// analyzed regions.
	PartialAnalysis []string
}

// ProjectionResult is the sealed projection outcome: exactly one of
// Complete or Failed is set (projection.rs:317-324).
type ProjectionResult struct {
	// Complete is the successful outcome.
	Complete *CompleteProjection
	// Failed is the failed attempt.
	Failed *FailedProjectionAttempt
}

// ProjectionFailureKind is the stable projection failure category
// (projection.rs:327-355).
type ProjectionFailureKind uint8

// The closed projection failure categories.
const (
	// ProjectionFailureRecoveredDocument: recovered documents cannot
	// publish partial semantic values.
	ProjectionFailureRecoveredDocument ProjectionFailureKind = iota
	// ProjectionFailureConflictingPolicyRules: equal-precedence rules
	// conflict.
	ProjectionFailureConflictingPolicyRules
	// ProjectionFailureWrongSnapshotPolicy: an exact NodeRef scope belongs
	// to another snapshot or role.
	ProjectionFailureWrongSnapshotPolicy
	// ProjectionFailureInvalidPolicyTarget: an exact NodeRef scope does not
	// identify an Object value.
	ProjectionFailureInvalidPolicyTarget
	// ProjectionFailureTargetNotApplicable: the root does not satisfy an
	// explicitly requested mapping target.
	ProjectionFailureTargetNotApplicable
	// ProjectionFailureDuplicateKeys: a duplicate member cannot enter an
	// Object under Reject.
	ProjectionFailureDuplicateKeys
	// ProjectionFailureSemanticUnavailable: native semantics are locally
	// unavailable.
	ProjectionFailureSemanticUnavailable
	// ProjectionFailureResourceLimit: a declared resource limit was
	// reached; output is not truncated to success.
	ProjectionFailureResourceLimit
)

// ProjectionFailure is the typed projection failure. It implements error
// and the RFC 0016 §6 Code() contract with the frozen registered codes.
type ProjectionFailure struct {
	// Kind identifies the failure.
	Kind ProjectionFailureKind
	// Node is the offending Object or value node.
	Node document.NodeRef
	// Name is the duplicated decoded name (DuplicateKeys).
	Name string
	// Reason is the local unavailability reason (SemanticUnavailable).
	Reason SemanticUnavailable
	// LimitName is the stable limit name of a ResourceLimit.
	LimitName string
}

// Error implements error.
func (e *ProjectionFailure) Error() string {
	switch e.Kind {
	case ProjectionFailureRecoveredDocument:
		return "json: recovered documents cannot enter projection"
	case ProjectionFailureConflictingPolicyRules:
		return "json: projection policy rules conflict"
	case ProjectionFailureWrongSnapshotPolicy:
		return "json: projection policy uses another snapshot"
	case ProjectionFailureInvalidPolicyTarget:
		return "json: projection policy target is not an Object value"
	case ProjectionFailureTargetNotApplicable:
		return "json: projection target does not apply to the root"
	case ProjectionFailureDuplicateKeys:
		return "json: duplicate key " + e.Name + " cannot enter an Object under Reject"
	case ProjectionFailureSemanticUnavailable:
		return "json: native semantics unavailable: " + e.Reason.String()
	case ProjectionFailureResourceLimit:
		return "json: projection limit " + e.LimitName + " reached"
	}
	return "json: projection failure"
}

// Code returns the frozen registered code for the failure
// (projection.rs:754-765).
func (e *ProjectionFailure) Code() string {
	switch e.Kind {
	case ProjectionFailureRecoveredDocument:
		return "json.projection.incomplete-document@1"
	case ProjectionFailureConflictingPolicyRules:
		return "core.projection.conflicting-policy@1"
	case ProjectionFailureWrongSnapshotPolicy:
		return "core.projection.wrong-snapshot-policy@1"
	case ProjectionFailureInvalidPolicyTarget:
		return "core.projection.invalid-policy-target@1"
	case ProjectionFailureTargetNotApplicable:
		return "core.projection.target-not-applicable@1"
	case ProjectionFailureDuplicateKeys:
		return "json.projection.duplicate-keys@1"
	case ProjectionFailureSemanticUnavailable:
		return "json.projection.semantic-unavailable@1"
	case ProjectionFailureResourceLimit:
		return "core.projection.resource-limit@1"
	}
	return "core.projection.resource-limit@1"
}

// Project applies an immutable request; a failure never contains a
// partial value (projection.rs:357-430).
func (d *Document) Project(request *ProjectionRequest) ProjectionResult {
	if d.formationStatus != document.FormationStatusComplete {
		return failedProjection(&ProjectionFailure{Kind: ProjectionFailureRecoveredDocument},
			ProjectionReport{}, nil)
	}
	if (request.target == ProjectionTargetJson5BestExactCoreV1 && !d.profile.isJSON5()) ||
		(request.target == ProjectionTargetBestExactCoreV1 && d.profile.isJSON5()) {
		return failedProjection(&ProjectionFailure{Kind: ProjectionFailureTargetNotApplicable},
			ProjectionReport{}, nil)
	}
	for _, rule := range request.duplicateRules {
		if rule.scope.Global {
			continue
		}
		if rule.scope.Node.Snapshot() != d.SnapshotIdentity() {
			return failedProjection(&ProjectionFailure{Kind: ProjectionFailureWrongSnapshotPolicy},
				ProjectionReport{}, nil)
		}
		index, err := d.validateRef(rule.scope.Node, []document.NodeRole{document.RoleValue})
		if err != nil {
			return failedProjection(&ProjectionFailure{Kind: ProjectionFailureInvalidPolicyTarget},
				ProjectionReport{}, nil)
		}
		if d.valueEntity(index).kind.tag != internalObject {
			return failedProjection(&ProjectionFailure{Kind: ProjectionFailureInvalidPolicyTarget},
				ProjectionReport{}, nil)
		}
	}
	rootKind := d.Root().Kind()
	if (request.target == ProjectionTargetProjectAsObjectV1 ||
		request.target == ProjectionTargetProjectAsEntryMappingV1) &&
		!(rootKind.IsAvailable() && rootKind.Value() == JsonValueKindObject) {
		return failedProjection(&ProjectionFailure{Kind: ProjectionFailureTargetNotApplicable},
			ProjectionReport{}, nil)
	}
	context := &projectionContext{
		document: d,
		request:  request,
	}
	value, failure := context.projectValue(d.Root(), protocol.RootValuePath(), 0)
	if failure != nil {
		return failedProjection(failure, context.report, context.partialAnalysis)
	}
	return ProjectionResult{Complete: &CompleteProjection{
		Value:      value,
		Fidelity:   context.fidelity,
		Report:     context.report,
		Provenance: context.provenance,
	}}
}

// failedProjection builds the failed attempt with its diagnostic
// (projection.rs:728-752).
func failedProjection(failure *ProjectionFailure, report ProjectionReport,
	analysis []string) ProjectionResult {
	diagnostic, err := protocol.NewDiagnostic(failure.Code(),
		protocol.CategoryProjection, protocol.SeverityError, nil, nil,
		map[string]string{"failure": failure.Error()}, nil, nil, 0, errorRegistry())
	if err != nil {
		diagnostic = &protocol.Diagnostic{Code: failure.Code(),
			Category: protocol.CategoryProjection, Severity: protocol.SeverityError,
			Arguments: map[string]string{"failure": failure.Error()}}
	}
	return ProjectionResult{Failed: &FailedProjectionAttempt{
		Diagnostics:     []*protocol.Diagnostic{diagnostic},
		Report:          report,
		PartialAnalysis: analysis,
	}}
}

// projectionContext is the state of one projection (projection.rs:432-441).
type projectionContext struct {
	document        *Document
	request         *ProjectionRequest
	report          ProjectionReport
	provenance      ProvenanceMap
	fidelity        Fidelity
	valueNodes      int
	partialAnalysis []string
}

// projectValue projects one value node (projection.rs:443-499).
func (c *projectionContext) projectValue(value JsonValue, path protocol.ValuePath,
	depth int) (core.Value, *ProjectionFailure) {
	if depth > c.request.limits.MaxDepth {
		return nil, &ProjectionFailure{Kind: ProjectionFailureResourceLimit, LimitName: "projection-depth"}
	}
	c.valueNodes++
	if c.valueNodes > c.request.limits.MaxValueNodes {
		return nil, &ProjectionFailure{Kind: ProjectionFailureResourceLimit, LimitName: "projected-value-nodes"}
	}
	c.partialAnalysis = append(c.partialAnalysis, pathDescription(path)+":Projectable")
	if failure := c.addOrigin(ProjectedLocation{Path: path}, value.NodeRef(), value.Span()); failure != nil {
		return nil, failure
	}
	kind := &c.document.valueEntity(value.index).kind
	switch kind.tag {
	case internalNull:
		return core.NullValue(), nil
	case internalBoolean:
		return core.Boolean(kind.boolean), nil
	case internalInteger:
		return core.NewInteger(kind.integer), nil
	case internalDecimal:
		return kind.decimal, nil
	case internalBinaryFloat64:
		return kind.binary64, nil
	case internalString:
		return core.String(kind.stringText), nil
	case internalArray:
		items := make([]core.Value, 0, len(kind.array))
		for ordinal, index := range kind.array {
			element := JsonArrayElement{document: c.document, index: index}
			projected, failure := c.projectValue(element.Value(),
				path.Child(protocol.ValuePathSegment{Kind: "SequenceElement", Index: uint64(ordinal)}),
				depth+1)
			if failure != nil {
				return nil, failure
			}
			items = append(items, projected)
		}
		return core.NewArray(items...), nil
	case internalObject:
		members := make([]JsonObjectMember, 0, len(kind.object))
		for _, index := range kind.object {
			members = append(members, JsonObjectMember{document: c.document, index: index})
		}
		return c.projectObject(value, members, path, depth)
	case internalUnavailable:
		return nil, &ProjectionFailure{Kind: ProjectionFailureSemanticUnavailable,
			Node: value.NodeRef(), Reason: kind.unavail}
	}
	return nil, &ProjectionFailure{Kind: ProjectionFailureSemanticUnavailable,
		Node: value.NodeRef(), Reason: SemanticUnavailableMissing}
}

// projectObject projects one object value (projection.rs:501-639).
func (c *projectionContext) projectObject(object JsonValue, members []JsonObjectMember,
	path protocol.ValuePath, depth int) (core.Value, *ProjectionFailure) {
	names := make([]string, 0, len(members))
	for _, member := range members {
		availability := member.Name()
		if !availability.IsAvailable() {
			return nil, &ProjectionFailure{Kind: ProjectionFailureSemanticUnavailable,
				Node: member.KeyNodeRef(), Reason: availability.Reason()}
		}
		names = append(names, *availability.Value())
	}
	hasDuplicates := false
	{
		seen := make(map[string]bool, len(names))
		for _, name := range names {
			if seen[name] {
				hasDuplicates = true
				break
			}
			seen[name] = true
		}
	}
	useMapping := false
	switch c.request.target {
	case ProjectionTargetProjectAsEntryMappingV1:
		useMapping = true
	case ProjectionTargetBestExactCoreV1, ProjectionTargetJson5BestExactCoreV1:
		useMapping = hasDuplicates
	}
	if useMapping {
		if c.request.target != ProjectionTargetProjectAsObjectV1 {
			c.fidelity = maxFidelity(c.fidelity, FidelityTransformed)
			projected := ProjectedLocation{Path: path}
			if failure := c.pushEvent(ProjectionEvent{
				Kind:        ProjectionEventStructureReencoded,
				Source:      object.NodeRef(),
				Projected:   &projected,
				OldCategory: "JsonObject",
				NewCategory: "EntryMapping",
				Reversible:  true,
				Loss:        FidelityTransformed,
			}); failure != nil {
				return nil, failure
			}
		}
		builder := core.NewEntryMappingBuilder()
		for ordinal, member := range members {
			keyPath := path.Child(protocol.ValuePathSegment{Kind: "EntryKey", Index: uint64(ordinal)})
			valuePath := path.Child(protocol.ValuePathSegment{Kind: "EntryValue", Index: uint64(ordinal)})
			association := protocol.NewAssociationLocation(path, uint64(ordinal),
				protocol.AssociationRoleEntryMappingItem)
			if failure := c.addOrigin(ProjectedLocation{IsAssociation: true, Association: association},
				member.NodeRef(), member.Span()); failure != nil {
				return nil, failure
			}
			keyEntity := c.document.entity(member.index).member
			keySpan := c.document.span(keyEntity.key)
			if failure := c.addOrigin(ProjectedLocation{Path: keyPath},
				member.KeyNodeRef(), keySpan); failure != nil {
				return nil, failure
			}
			projected, failure := c.projectValue(member.Value(), valuePath, depth+1)
			if failure != nil {
				return nil, failure
			}
			if err := builder.Push(core.String(names[ordinal]), projected); err != nil {
				return nil, &ProjectionFailure{Kind: ProjectionFailureResourceLimit,
					LimitName: "projected-value-nodes"}
			}
		}
		return builder.Build(), nil
	}

	policy := c.duplicatePolicy(object.NodeRef())
	retained, failure := selectMembers(members, names, policy, object.NodeRef())
	if failure != nil {
		return nil, failure
	}
	if len(retained) != len(members) {
		c.fidelity = FidelityLossy
	}
	retainedSet := make(map[int]bool, len(retained))
	projectedOrdinals := make(map[int]int, len(retained))
	for projectedOrdinal, source := range retained {
		retainedSet[source] = true
		projectedOrdinals[source] = projectedOrdinal
	}
	for sourceOrdinal, member := range members {
		if retainedSet[sourceOrdinal] {
			continue
		}
		name := names[sourceOrdinal]
		retainedSource := -1
		for _, index := range retained {
			if names[index] == name {
				retainedSource = index
				break
			}
		}
		location := protocol.NewAssociationLocation(path, uint64(projectedOrdinals[retainedSource]),
			protocol.AssociationRoleObjectEntry)
		projected := ProjectedLocation{IsAssociation: true, Association: location}
		if failure := c.pushEvent(ProjectionEvent{
			Kind:        ProjectionEventDuplicateCollapsed,
			Policy:      &policy,
			Source:      member.NodeRef(),
			Projected:   &projected,
			OldCategory: "JsonObjectMember",
			NewCategory: "Collapsed",
			Reversible:  false,
			Loss:        FidelityLossy,
		}); failure != nil {
			return nil, failure
		}
	}
	entries := make([]core.Entry, 0, len(retained))
	for projectedOrdinal, sourceOrdinal := range retained {
		member := members[sourceOrdinal]
		name := names[sourceOrdinal]
		valuePath := path.Child(protocol.ValuePathSegment{Kind: "ObjectValue", Key: name})
		entryLocation := protocol.NewAssociationLocation(path, uint64(projectedOrdinal),
			protocol.AssociationRoleObjectEntry)
		if failure := c.addOrigin(ProjectedLocation{IsAssociation: true, Association: entryLocation},
			member.NodeRef(), member.Span()); failure != nil {
			return nil, failure
		}
		keyLocation := protocol.NewAssociationLocation(path, uint64(projectedOrdinal),
			protocol.AssociationRoleObjectKey)
		keyEntity := c.document.entity(member.index).member
		keySpan := c.document.span(keyEntity.key)
		if failure := c.addOrigin(ProjectedLocation{IsAssociation: true, Association: keyLocation},
			member.KeyNodeRef(), keySpan); failure != nil {
			return nil, failure
		}
		value, failure := c.projectValue(member.Value(), valuePath, depth+1)
		if failure != nil {
			return nil, failure
		}
		entries = append(entries, core.Entry{Key: name, Value: value})
	}
	objectValue, err := core.NewObject(entries...)
	if err != nil {
		return nil, &ProjectionFailure{Kind: ProjectionFailureResourceLimit,
			LimitName: "projected-value-nodes"}
	}
	return objectValue, nil
}

// duplicatePolicy resolves the effective policy for one object node
// (projection.rs:641-657).
func (c *projectionContext) duplicatePolicy(node document.NodeRef) DuplicateKeyPolicy {
	for _, rule := range c.request.duplicateRules {
		if !rule.scope.Global && rule.scope.Node == node {
			return rule.policy
		}
	}
	for _, rule := range c.request.duplicateRules {
		if rule.scope.Global {
			return rule.policy
		}
	}
	return DuplicateKeyPolicyReject
}

// addOrigin records one provenance mapping (projection.rs:659-678).
func (c *projectionContext) addOrigin(projected ProjectedLocation, node document.NodeRef,
	span document.Span) *ProjectionFailure {
	if len(c.provenance.entries) >= c.request.limits.MaxProvenanceEntries {
		return &ProjectionFailure{Kind: ProjectionFailureResourceLimit,
			LimitName: "provenance-entries"}
	}
	c.provenance.entries = append(c.provenance.entries, ProvenanceEntry{
		Projected: projected,
		Origins: []SourceOrigin{{
			Snapshot: c.document.SnapshotIdentity(),
			Node:     node,
			Span:     span,
			Relation: ProvenanceRelationDirect,
		}},
	})
	return nil
}

// pushEvent records one report event (projection.rs:680-688).
func (c *projectionContext) pushEvent(event ProjectionEvent) *ProjectionFailure {
	if len(c.report.events) >= c.request.limits.MaxReportEntries {
		return &ProjectionFailure{Kind: ProjectionFailureResourceLimit,
			LimitName: "projection-report-entries"}
	}
	c.report.events = append(c.report.events, event)
	return nil
}

// selectMembers selects the retained members under one policy
// (projection.rs:691-726).
func selectMembers(members []JsonObjectMember, names []string, policy DuplicateKeyPolicy,
	node document.NodeRef) ([]int, *ProjectionFailure) {
	counts := make(map[string]int, len(names))
	for _, name := range names {
		counts[name]++
	}
	if policy == DuplicateKeyPolicyReject {
		for _, name := range names {
			if counts[name] > 1 {
				return nil, &ProjectionFailure{Kind: ProjectionFailureDuplicateKeys,
					Node: node, Name: name}
			}
		}
	}
	switch policy {
	case DuplicateKeyPolicyReject, DuplicateKeyPolicyFirstWins:
		seen := make(map[string]bool, len(names))
		retained := make([]int, 0, len(names))
		for index, name := range names {
			if seen[name] {
				continue
			}
			seen[name] = true
			retained = append(retained, index)
		}
		return retained, nil
	case DuplicateKeyPolicyLastWins:
		seen := make(map[string]bool, len(names))
		retained := make([]int, 0, len(names))
		for index := len(names) - 1; index >= 0; index-- {
			if seen[names[index]] {
				continue
			}
			seen[names[index]] = true
			retained = append(retained, index)
		}
		for left, right := 0, len(retained)-1; left < right; left, right = left+1, right-1 {
			retained[left], retained[right] = retained[right], retained[left]
		}
		return retained, nil
	}
	return nil, &ProjectionFailure{Kind: ProjectionFailureConflictingPolicyRules}
}

// maxFidelity returns the worse fidelity.
func maxFidelity(left, right Fidelity) Fidelity {
	if left > right {
		return left
	}
	return right
}

// pathDescription renders one value path deterministically for the
// partial analysis records.
func pathDescription(path protocol.ValuePath) string {
	segments := path.Segments()
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		switch segment.Kind {
		case "ObjectValue":
			parts = append(parts, "ObjectValue(\""+segment.Key+"\")")
		case "SequenceElement", "EntryKey", "EntryValue":
			parts = append(parts, segment.Kind+"("+uint64String(segment.Index)+")")
		default:
			parts = append(parts, segment.Kind)
		}
	}
	return "Path([" + strings.Join(parts, ", ") + "])"
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
