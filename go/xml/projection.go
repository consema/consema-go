package xml

// This file implements XML projection targets and explicit mapping
// policies (RFC 0012 §9; consema-rs/consema-xml/src/projection.rs). The exact
// default target is the versioned `xml.element-tree@1` record. There is no
// `xml-to-json-default`, automatic attribute `@` prefix, automatic text
// `#text` key, singular/plural heuristic, namespace stripping, or child
// grouping. Any authorized transformation emits report events and
// provenance.

import (
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// ProjectionTarget is a versioned XML projection target (projection.rs).
type ProjectionTarget uint8

// The three frozen targets.
const (
	// ProjectionTargetElementTreeV1 is the exact `xml.element-tree@1`
	// record projection.
	ProjectionTargetElementTreeV1 ProjectionTarget = iota
	// ProjectionTargetTextContentV1 is the always-transformed descendant
	// text content.
	ProjectionTargetTextContentV1
	// ProjectionTargetSimpleEntryMappingV1 is the explicit-policy entry
	// mapping of a selected subtree.
	ProjectionTargetSimpleEntryMappingV1
)

// TextContentInclude is the descendant text inclusion for TextContentV1
// (projection.rs).
type TextContentInclude uint8

// The two frozen inclusion modes.
const (
	// TextContentIncludeTextAndCdata includes descendant text and CDATA
	// occurrences.
	TextContentIncludeTextAndCdata TextContentInclude = iota
	// TextContentIncludeTextOnly includes descendant text only; CDATA is
	// reported as discarded.
	TextContentIncludeTextOnly
)

// AttributePolicy is the attribute handling for SimpleEntryMappingV1
// (projection.rs).
type AttributePolicy uint8

// The three frozen attribute policies.
const (
	// AttributePolicyRejectAttributes rejects the projection when any
	// attribute is present.
	AttributePolicyRejectAttributes AttributePolicy = iota
	// AttributePolicyIgnoreAttributes ignores every attribute and reports
	// each as discarded.
	AttributePolicyIgnoreAttributes
	// AttributePolicyPrefixAttributeKeys prefixes attribute keys with `@`.
	AttributePolicyPrefixAttributeKeys
)

// TextKeyPolicy is the text child handling for SimpleEntryMappingV1
// (projection.rs).
type TextKeyPolicy uint8

// The two frozen text policies.
const (
	// TextKeyPolicyRejectText rejects the projection when any non-whitespace
	// text is present.
	TextKeyPolicyRejectText TextKeyPolicy = iota
	// TextKeyPolicyIgnoreText discards text and reports it as discarded.
	TextKeyPolicyIgnoreText
)

// RepeatedChildPolicy is the repeated expanded-child-name handling for
// SimpleEntryMappingV1 (projection.rs).
type RepeatedChildPolicy uint8

// The three frozen repeated-child policies.
const (
	// RepeatedChildPolicyReject rejects every repeated expanded child name.
	RepeatedChildPolicyReject RepeatedChildPolicy = iota
	// RepeatedChildPolicyFirst retains the first occurrence in document
	// order.
	RepeatedChildPolicyFirst
	// RepeatedChildPolicyLast retains the last occurrence.
	RepeatedChildPolicyLast
)

// ExpandedNameKeyPolicy is the entry-key spelling for SimpleEntryMappingV1
// (projection.rs).
type ExpandedNameKeyPolicy uint8

// The three frozen key spellings.
const (
	// ExpandedNameKeyPolicyLocalOnly keys by the local name; namespace
	// collisions must be resolved by another policy or the projection
	// fails.
	ExpandedNameKeyPolicyLocalOnly ExpandedNameKeyPolicy = iota
	// ExpandedNameKeyPolicyPrefixedSpelling keys by the lexical
	// `prefix:local` spelling.
	ExpandedNameKeyPolicyPrefixedSpelling
	// ExpandedNameKeyPolicyUriBracketed keys by the `{uri}local` spelling;
	// an absent namespace is `{}local`.
	ExpandedNameKeyPolicyUriBracketed
)

// CollisionPolicy is the collision resolution direction shared by both
// entry policies (projection.rs).
type CollisionPolicy uint8

// The three frozen collision policies.
const (
	// CollisionPolicyReject rejects every collision.
	CollisionPolicyReject CollisionPolicy = iota
	// CollisionPolicyFirst retains the first occurrence in document order.
	CollisionPolicyFirst
	// CollisionPolicyLast retains the last occurrence.
	CollisionPolicyLast
)

// ProjectionRequest is the explicit XML projection request; every policy
// is mandatory (projection.rs).
type ProjectionRequest struct {
	target        ProjectionTarget
	subtree       *uint64
	include       TextContentInclude
	attributes    AttributePolicy
	textKey       TextKeyPolicy
	repeatedChild RepeatedChildPolicy
	keySpelling   ExpandedNameKeyPolicy
	collision     CollisionPolicy
	limits        ProjectionLimits
}

// ElementTreeRequest is the exact `xml.element-tree@1` record request for
// the document root (projection.rs).
func ElementTreeRequest() ProjectionRequest {
	return ProjectionRequest{
		target:        ProjectionTargetElementTreeV1,
		include:       TextContentIncludeTextAndCdata,
		attributes:    AttributePolicyRejectAttributes,
		textKey:       TextKeyPolicyRejectText,
		repeatedChild: RepeatedChildPolicyReject,
		keySpelling:   ExpandedNameKeyPolicyLocalOnly,
		collision:     CollisionPolicyReject,
		limits:        DefaultProjectionLimits(),
	}
}

// SimpleEntryMappingRequest is the explicit SimpleEntryMappingV1 request
// over one subtree (projection.rs).
func SimpleEntryMappingRequest(subtree document.NodeRef, attributes AttributePolicy,
	textKey TextKeyPolicy, repeatedChild RepeatedChildPolicy, keySpelling ExpandedNameKeyPolicy,
	collision CollisionPolicy) ProjectionRequest {
	index := subtree.Index()
	return ProjectionRequest{
		target:        ProjectionTargetSimpleEntryMappingV1,
		subtree:       &index,
		include:       TextContentIncludeTextAndCdata,
		attributes:    attributes,
		textKey:       textKey,
		repeatedChild: repeatedChild,
		keySpelling:   keySpelling,
		collision:     collision,
		limits:        DefaultProjectionLimits(),
	}
}

// TextContentRequest is the explicit TextContentV1 request over one
// subtree (projection.rs).
func TextContentRequest(subtree document.NodeRef, include TextContentInclude) ProjectionRequest {
	index := subtree.Index()
	return ProjectionRequest{
		target:        ProjectionTargetTextContentV1,
		subtree:       &index,
		include:       include,
		attributes:    AttributePolicyRejectAttributes,
		textKey:       TextKeyPolicyRejectText,
		repeatedChild: RepeatedChildPolicyReject,
		keySpelling:   ExpandedNameKeyPolicyLocalOnly,
		collision:     CollisionPolicyReject,
		limits:        DefaultProjectionLimits(),
	}
}

// Target returns the target contract.
func (r *ProjectionRequest) Target() ProjectionTarget { return r.target }

// Subtree returns the selected subtree identity, when the request targets
// a subtree.
func (r *ProjectionRequest) Subtree() *uint64 { return r.subtree }

// Limits returns the resource limits.
func (r *ProjectionRequest) Limits() ProjectionLimits { return r.limits }

// ProjectionLimits are the XML projection resource limits
// (projection.rs).
type ProjectionLimits struct {
	// MaxSourceNodes is the maximum inspected source nodes.
	MaxSourceNodes int
	// MaxValueNodes is the maximum produced PortableValue nodes.
	MaxValueNodes int
	// MaxReportEntries is the maximum report events.
	MaxReportEntries int
	// MaxProvenanceUnits is the maximum projected locations plus source
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

// Fidelity is the projection fidelity classification (projection.rs).
type Fidelity uint8

// The three frozen fidelity classes.
const (
	// FidelityExact means the target directly represents every native
	// association.
	FidelityExact Fidelity = iota
	// FidelityTransformed means an explicit reported policy transformed
	// associations.
	FidelityTransformed
	// FidelityLossy means source facts were irreversibly omitted without a
	// retained source relation.
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
// (projection.rs).
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
// (projection.rs).
type ProvenanceRelation uint8

// The four frozen relations.
const (
	// ProvenanceRelationDirect is a direct native semantic origin.
	ProvenanceRelationDirect ProvenanceRelation = iota
	// ProvenanceRelationDerived is a container value derived from a source
	// record.
	ProvenanceRelationDerived
	// ProvenanceRelationCollapsed is a discarded occurrence related to the
	// retained projected occurrence.
	ProvenanceRelationCollapsed
	// ProvenanceRelationReferenceDerived is semantic content derived from
	// reference resolution.
	ProvenanceRelationReferenceDerived
)

// String returns the stable relation name.
func (r ProvenanceRelation) String() string {
	switch r {
	case ProvenanceRelationDirect:
		return "Direct"
	case ProvenanceRelationDerived:
		return "Derived"
	case ProvenanceRelationCollapsed:
		return "Collapsed"
	case ProvenanceRelationReferenceDerived:
		return "ReferenceDerived"
	}
	return "Direct"
}

// SourceOrigin is one exact source origin (projection.rs).
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

// ProvenanceEntry is one many-valued provenance entry (projection.rs).
type ProvenanceEntry struct {
	// Projected is the projected value or association.
	Projected ProjectedLocation
	// Origins are the ordered source origins.
	Origins []SourceOrigin
}

// ProvenanceMap is the immutable many-valued provenance mapping
// (projection.rs).
type ProvenanceMap struct {
	entries []ProvenanceEntry
}

// Entries returns the deterministically ordered projected locations and
// origins. The returned slice is a copy.
func (m *ProvenanceMap) Entries() []ProvenanceEntry {
	return append([]ProvenanceEntry(nil), m.entries...)
}

func (m *ProvenanceMap) push(entry ProvenanceEntry, limits ProjectionLimits) *ProjectionFailure {
	if len(m.entries)+1 > limits.MaxProvenanceUnits {
		return &ProjectionFailure{Kind: ProjectionFailureResourceLimit, LimitName: "max_provenance_units"}
	}
	m.entries = append(m.entries, entry)
	return nil
}

// ProjectionEventKind is the projection report category
// (projection.rs).
type ProjectionEventKind uint8

// The nine frozen event kinds.
const (
	// ProjectionEventElementDiscarded is an element discarded by policy.
	ProjectionEventElementDiscarded ProjectionEventKind = iota
	// ProjectionEventAttributeDiscarded is an attribute discarded by policy.
	ProjectionEventAttributeDiscarded
	// ProjectionEventTextDiscarded is text discarded by policy.
	ProjectionEventTextDiscarded
	// ProjectionEventCdataDiscarded is CDATA discarded by policy.
	ProjectionEventCdataDiscarded
	// ProjectionEventCommentDiscarded is a comment discarded by policy.
	ProjectionEventCommentDiscarded
	// ProjectionEventProcessingInstructionDiscarded is a processing
	// instruction discarded by policy.
	ProjectionEventProcessingInstructionDiscarded
	// ProjectionEventReferenceCollapsed is a reference distinction collapsed
	// into resolved text.
	ProjectionEventReferenceCollapsed
	// ProjectionEventChildCollapsed is a repeated expanded child name
	// collapsed under policy.
	ProjectionEventChildCollapsed
	// ProjectionEventNamespaceCollapsed is an expanded-name namespace
	// difference collapsed by key spelling.
	ProjectionEventNamespaceCollapsed
)

// String returns the stable event-kind name.
func (k ProjectionEventKind) String() string {
	switch k {
	case ProjectionEventElementDiscarded:
		return "ElementDiscarded"
	case ProjectionEventAttributeDiscarded:
		return "AttributeDiscarded"
	case ProjectionEventTextDiscarded:
		return "TextDiscarded"
	case ProjectionEventCdataDiscarded:
		return "CdataDiscarded"
	case ProjectionEventCommentDiscarded:
		return "CommentDiscarded"
	case ProjectionEventProcessingInstructionDiscarded:
		return "ProcessingInstructionDiscarded"
	case ProjectionEventReferenceCollapsed:
		return "ReferenceCollapsed"
	case ProjectionEventChildCollapsed:
		return "ChildCollapsed"
	case ProjectionEventNamespaceCollapsed:
		return "NamespaceCollapsed"
	}
	return "ElementDiscarded"
}

// ProjectionEvent is one explicit transformation event (projection.rs).
type ProjectionEvent struct {
	// Kind is the stable event kind.
	Kind ProjectionEventKind
	// Discarded is the discarded source occurrence.
	Discarded document.NodeRef
	// Impact is the fidelity impact.
	Impact Fidelity
}

// ProjectionReport is the complete ordered projection report
// (projection.rs).
type ProjectionReport struct {
	events []ProjectionEvent
}

// Events returns the events in deterministic document order. The returned
// slice is a copy.
func (r *ProjectionReport) Events() []ProjectionEvent {
	return append([]ProjectionEvent(nil), r.events...)
}

func (r *ProjectionReport) push(event ProjectionEvent, limits ProjectionLimits) *ProjectionFailure {
	if len(r.events)+1 > limits.MaxReportEntries {
		return &ProjectionFailure{Kind: ProjectionFailureResourceLimit, LimitName: "max_report_entries"}
	}
	r.events = append(r.events, event)
	return nil
}

// CompleteProjection is the complete successful projection
// (projection.rs).
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

// FailedProjectionAttempt is a failed projection attempt without a partial
// value (projection.rs).
type FailedProjectionAttempt struct {
	// Diagnostics are the stable ordered diagnostics.
	Diagnostics []*protocol.Diagnostic
	// Report is empty: failed projections publish no partial transformation
	// result.
	Report ProjectionReport
}

// ProjectionResult is the sealed projection outcome: exactly one of
// Complete or Failed is set (projection.rs).
type ProjectionResult struct {
	// Complete is the complete success outcome.
	Complete *CompleteProjection
	// Failed is the failure with no value or provenance map.
	Failed *FailedProjectionAttempt
}

// ProjectionFailureKind classifies a stable XML projection failure
// (projection.rs).
type ProjectionFailureKind uint8

// The closed projection failure classes.
const (
	// ProjectionFailureRecoveredDocument: recovered documents cannot publish
	// partial semantic values.
	ProjectionFailureRecoveredDocument ProjectionFailureKind = iota
	// ProjectionFailureSubtreeNotElement: the selected subtree is not an
	// element.
	ProjectionFailureSubtreeNotElement
	// ProjectionFailureMappingAdmission: a simple-entry-mapping admission
	// precondition failed.
	ProjectionFailureMappingAdmission
	// ProjectionFailureCollision: an object collision under Reject.
	ProjectionFailureCollision
	// ProjectionFailureResourceLimit: a declared projection resource limit
	// was reached.
	ProjectionFailureResourceLimit
	// ProjectionFailureCoreInvariant: a PortableValue construction invariant
	// failed.
	ProjectionFailureCoreInvariant
)

// ProjectionFailure is the typed projection failure. It implements error
// and the RFC 0016 §6 Code() contract with the frozen registered codes.
type ProjectionFailure struct {
	// Kind identifies the failure.
	Kind ProjectionFailureKind
	// Child is the colliding child element of Collision.
	Child document.NodeRef
	// Key is the entry key that collided of Collision.
	Key string
	// Reason is the admission reason of MappingAdmission.
	Reason string
	// LimitName is the stable limit name of a ResourceLimit.
	LimitName string
}

// Error implements error; the text is human presentation only.
func (e *ProjectionFailure) Error() string {
	switch e.Kind {
	case ProjectionFailureRecoveredDocument:
		return "xml: projection of a recovered document is not available"
	case ProjectionFailureSubtreeNotElement:
		return "xml: projection subtree is not an element"
	case ProjectionFailureMappingAdmission:
		return "xml: entry mapping admission failed: " + e.Reason
	case ProjectionFailureCollision:
		return "xml: projection entry key collision: " + e.Key
	case ProjectionFailureResourceLimit:
		return "xml: projection limit " + e.LimitName + " reached"
	case ProjectionFailureCoreInvariant:
		return "xml: projection core invariant failed"
	}
	return "xml: projection failure"
}

// Code returns the frozen registered code for the failure
// (projection.rs).
func (e *ProjectionFailure) Code() string {
	switch e.Kind {
	case ProjectionFailureRecoveredDocument:
		return "xml.projection.recovered-document@1"
	case ProjectionFailureSubtreeNotElement:
		return "xml.projection.subtree@1"
	case ProjectionFailureMappingAdmission:
		return "xml.projection.admission@1"
	case ProjectionFailureCollision:
		return "xml.projection.collision@1"
	case ProjectionFailureResourceLimit:
		return "xml.projection.resource-limit@1"
	case ProjectionFailureCoreInvariant:
		return "xml.projection.core-invariant@1"
	}
	return "xml.projection.core-invariant@1"
}

// Project projects this snapshot under one explicit target and policy
// contract (projection.rs).
func (d *Document) Project(request ProjectionRequest) ProjectionResult {
	if d.status != document.FormationStatusComplete {
		return failedProjection(&ProjectionFailure{Kind: ProjectionFailureRecoveredDocument})
	}
	context := &projectionContext{
		document: d,
		limits:   request.limits,
	}
	var value core.Value
	var fidelity Fidelity
	var failure *ProjectionFailure
	switch request.target {
	case ProjectionTargetElementTreeV1:
		value, fidelity, failure = context.projectElementTree()
	case ProjectionTargetTextContentV1:
		value, fidelity, failure = context.projectTextContent(request)
	case ProjectionTargetSimpleEntryMappingV1:
		value, fidelity, failure = context.projectEntryMapping(request)
	}
	if failure != nil {
		return failedProjection(failure)
	}
	return ProjectionResult{Complete: &CompleteProjection{
		Value:      value,
		Fidelity:   fidelity,
		Report:     context.report,
		Provenance: context.provenance,
	}}
}

// projectionContext is the execution state of one projection.
type projectionContext struct {
	document    *Document
	limits      ProjectionLimits
	report      ProjectionReport
	provenance  ProvenanceMap
	valueNodes  int
	sourceNodes int
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
	return c.report.push(ProjectionEvent{Kind: kind, Discarded: discarded, Impact: impact}, c.limits)
}

func (c *projectionContext) origin(projected ProjectedLocation, node document.NodeRef,
	span document.Span, relation ProvenanceRelation) *ProjectionFailure {
	return c.provenance.push(ProvenanceEntry{
		Projected: projected,
		Origins: []SourceOrigin{{
			Snapshot: c.document.SnapshotIdentity(),
			Node:     node,
			Span:     span,
			Relation: relation,
		}},
	}, c.limits)
}

func (c *projectionContext) elementData(index int) *XmlElementData {
	return c.document.nodes[index].Element
}

func (c *projectionContext) elementNodeRef(index int) document.NodeRef {
	return c.document.authority.NodeRef(uint64(index), document.RoleXmlElement)
}

func (c *projectionContext) occurrenceNodeRef(ordinal uint64, role document.NodeRole) document.NodeRef {
	return c.document.authority.NodeRef(ordinal, role)
}

// itemPath is the value path of one item inside an ordered record array.
func itemPath(container protocol.ValuePath, field string, index int) protocol.ValuePath {
	return container.Child(protocol.ValuePathSegment{Kind: "ObjectValue", Key: field}).
		Child(protocol.ValuePathSegment{Kind: "SequenceElement", Index: uint64(index)})
}

// projectElementTree projects the exact `xml.element-tree@1` record for
// the document root (projection.rs).
func (c *projectionContext) projectElementTree() (core.Value, Fidelity, *ProjectionFailure) {
	root := c.document.Root()
	if root == nil {
		return nil, FidelityExact, &ProjectionFailure{Kind: ProjectionFailureMappingAdmission, Reason: "missing root"}
	}
	builder := core.NewObjectBuilder()
	if err := builder.Insert("record", core.String("xml.element-tree@1")); err != nil {
		return nil, FidelityExact, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
	}
	if declared := c.document.declaration; declared != nil {
		value, failure := declarationValue(declared)
		if failure != nil {
			return nil, FidelityExact, failure
		}
		if err := builder.Insert("declaration", value); err != nil {
			return nil, FidelityExact, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
		}
	}
	if doctype := c.document.doctype; doctype != nil && len(doctype.Entities) > 0 {
		var entityList []core.Value
		for _, entity := range doctype.Entities {
			entry := core.NewObjectBuilder()
			entry.Insert("name", core.String(entity.Name))
			entry.Insert("replacement", core.String(entity.Replacement))
			entityList = append(entityList, entry.Build())
		}
		if err := builder.Insert("entities", core.NewArray(entityList...)); err != nil {
			return nil, FidelityExact, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
		}
	}
	rootPath := protocol.RootValuePath().Child(protocol.ValuePathSegment{Kind: "ObjectValue", Key: "root"})
	rootValue, failure := c.elementValue(root.index, rootPath)
	if failure != nil {
		return nil, FidelityExact, failure
	}
	if err := builder.Insert("root", rootValue); err != nil {
		return nil, FidelityExact, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
	}
	return builder.Build(), FidelityExact, nil
}

// declarationValue builds the declaration record.
func declarationValue(declared *XmlDeclarationData) (core.Value, *ProjectionFailure) {
	builder := core.NewObjectBuilder()
	if err := builder.Insert("version", core.String(declared.Version)); err != nil {
		return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
	}
	if declared.Encoding != nil {
		if err := builder.Insert("encoding", core.String(declared.Encoding.Value)); err != nil {
			return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
		}
	}
	if declared.Standalone != nil {
		if err := builder.Insert("standalone", core.Boolean(declared.Standalone.Value)); err != nil {
			return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
		}
	}
	return builder.Build(), nil
}

// elementValue builds one recursive element record (projection.rs).
func (c *projectionContext) elementValue(index int, path protocol.ValuePath) (core.Value, *ProjectionFailure) {
	if failure := c.step(); failure != nil {
		return nil, failure
	}
	data := c.elementData(index)
	builder := core.NewObjectBuilder()
	namespace := ""
	if data.Expanded != nil && data.Expanded.Namespace != nil {
		namespace = *data.Expanded.Namespace
	}
	local := data.QName.Local
	name := core.NewObjectBuilder()
	var namespaceValue core.Value = core.NullValue()
	if data.Expanded != nil && data.Expanded.Namespace != nil {
		namespaceValue = core.String(namespace)
	}
	name.Insert("namespace", namespaceValue)
	name.Insert("local", core.String(local))
	if err := builder.Insert("expanded-name", name.Build()); err != nil {
		return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
	}
	if len(data.Namespaces) > 0 {
		var list []core.Value
		for item, binding := range data.Namespaces {
			bindingValue := core.NewObjectBuilder()
			var prefixValue core.Value = core.NullValue()
			if binding.Prefix != nil {
				prefixValue = core.String(*binding.Prefix)
			}
			bindingValue.Insert("prefix", prefixValue)
			bindingValue.Insert("uri", core.String(binding.URI))
			if failure := c.origin(projectedValue(itemPath(path, "namespaces", item)),
				c.occurrenceNodeRef(binding.Ordinal, document.RoleXmlNamespaceBinding),
				binding.Span, ProvenanceRelationDirect); failure != nil {
				return nil, failure
			}
			list = append(list, bindingValue.Build())
		}
		if err := builder.Insert("namespaces", core.NewArray(list...)); err != nil {
			return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
		}
	}
	if len(data.Attributes) > 0 {
		var list []core.Value
		for item, attribute := range data.Attributes {
			attributeValue := core.NewObjectBuilder()
			attrNamespace := ""
			if attribute.Expanded != nil && attribute.Expanded.Namespace != nil {
				attrNamespace = *attribute.Expanded.Namespace
			}
			attrName := core.NewObjectBuilder()
			var attrNamespaceValue core.Value = core.NullValue()
			if attribute.Expanded != nil && attribute.Expanded.Namespace != nil {
				attrNamespaceValue = core.String(attrNamespace)
			}
			attrName.Insert("namespace", attrNamespaceValue)
			attrName.Insert("local", core.String(attribute.QName.Local))
			attributeValue.Insert("expanded-name", attrName.Build())
			attributeValue.Insert("value", core.String(attribute.NormalizedValue))
			if failure := c.origin(projectedValue(itemPath(path, "attributes", item)),
				c.occurrenceNodeRef(attribute.Ordinal, document.RoleXmlAttribute),
				attribute.Span, ProvenanceRelationDirect); failure != nil {
				return nil, failure
			}
			list = append(list, attributeValue.Build())
		}
		if err := builder.Insert("attributes", core.NewArray(list...)); err != nil {
			return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
		}
	}
	if len(data.Children) > 0 {
		var list []core.Value
		for item, child := range data.Children {
			value, failure := c.contentValue(child, itemPath(path, "content", item))
			if failure != nil {
				return nil, failure
			}
			list = append(list, value)
		}
		if err := builder.Insert("content", core.NewArray(list...)); err != nil {
			return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
		}
	}
	if failure := c.reserveValue(1); failure != nil {
		return nil, failure
	}
	if failure := c.origin(projectedValue(path), c.elementNodeRef(index), data.Span,
		ProvenanceRelationDirect); failure != nil {
		return nil, failure
	}
	return builder.Build(), nil
}

// contentValue builds one ordered content item record
// (projection.rs).
func (c *projectionContext) contentValue(index int, path protocol.ValuePath) (core.Value, *ProjectionFailure) {
	if failure := c.step(); failure != nil {
		return nil, failure
	}
	content := &c.document.nodes[index]
	switch content.Kind {
	case XmlContentElement:
		return c.elementValue(index, path)
	case XmlContentText:
		builder := core.NewObjectBuilder()
		builder.Insert("kind", core.String("text"))
		var fragments []core.Value
		for item, fragment := range content.Text.Fragments {
			fragmentValue := core.NewObjectBuilder()
			switch fragment.Kind {
			case ReferenceFragmentLiteral:
				fragmentValue.Insert("kind", core.String("literal"))
				fragmentValue.Insert("text", core.String(fragment.Text))
			case ReferenceFragmentCharacterReference:
				fragmentValue.Insert("kind", core.String("character-reference"))
				fragmentValue.Insert("resolved", core.String(string(fragment.ResolvedChar)))
			case ReferenceFragmentPredefinedEntity:
				fragmentValue.Insert("kind", core.String("predefined-entity"))
				fragmentValue.Insert("name", core.String(fragment.Name))
				fragmentValue.Insert("resolved", core.String(fragment.Resolved))
			case ReferenceFragmentGeneralEntity:
				fragmentValue.Insert("kind", core.String("general-entity"))
				fragmentValue.Insert("name", core.String(fragment.Name))
				fragmentValue.Insert("resolved", core.String(fragment.Resolved))
			}
			if failure := c.origin(projectedValue(itemPath(path, "fragments", item)),
				c.occurrenceNodeRef(content.Text.Ordinal, document.RoleXmlEntityReference),
				fragment.Span, ProvenanceRelationReferenceDerived); failure != nil {
				return nil, failure
			}
			fragments = append(fragments, fragmentValue.Build())
		}
		builder.Insert("fragments", core.NewArray(fragments...))
		if failure := c.reserveValue(1); failure != nil {
			return nil, failure
		}
		if failure := c.origin(projectedValue(path),
			c.occurrenceNodeRef(content.Text.Ordinal, document.RoleXmlText),
			content.Text.Span, ProvenanceRelationDirect); failure != nil {
			return nil, failure
		}
		return builder.Build(), nil
	case XmlContentCdata:
		builder := core.NewObjectBuilder()
		builder.Insert("kind", core.String("cdata"))
		builder.Insert("text", core.String(content.Cdata.Text))
		if failure := c.reserveValue(1); failure != nil {
			return nil, failure
		}
		if failure := c.origin(projectedValue(path),
			c.occurrenceNodeRef(content.Cdata.Ordinal, document.RoleXmlCdata),
			content.Cdata.Span, ProvenanceRelationDirect); failure != nil {
			return nil, failure
		}
		return builder.Build(), nil
	case XmlContentComment:
		builder := core.NewObjectBuilder()
		builder.Insert("kind", core.String("comment"))
		builder.Insert("text", core.String(content.Comment.Text))
		if failure := c.reserveValue(1); failure != nil {
			return nil, failure
		}
		if failure := c.origin(projectedValue(path),
			c.occurrenceNodeRef(content.Comment.Ordinal, document.RoleXmlComment),
			content.Comment.Span, ProvenanceRelationDirect); failure != nil {
			return nil, failure
		}
		return builder.Build(), nil
	case XmlContentProcessingInstruction:
		pi := content.ProcessingInstruction
		builder := core.NewObjectBuilder()
		builder.Insert("kind", core.String("processing-instruction"))
		builder.Insert("target", core.String(pi.Target))
		if pi.Content != nil {
			builder.Insert("content", core.String(pi.Content.Text))
		}
		if failure := c.reserveValue(1); failure != nil {
			return nil, failure
		}
		if failure := c.origin(projectedValue(path),
			c.occurrenceNodeRef(pi.Ordinal, document.RoleXmlProcessingInstruction),
			pi.Span, ProvenanceRelationDirect); failure != nil {
			return nil, failure
		}
		return builder.Build(), nil
	default:
		builder := core.NewObjectBuilder()
		builder.Insert("kind", core.String("error-region"))
		if failure := c.reserveValue(1); failure != nil {
			return nil, failure
		}
		if failure := c.origin(projectedValue(path),
			c.occurrenceNodeRef(content.ErrorRegion.Ordinal, document.RoleXmlErrorRegion),
			content.ErrorRegion.Span, ProvenanceRelationDirect); failure != nil {
			return nil, failure
		}
		return builder.Build(), nil
	}
}

// projectTextContent projects the always-transformed descendant text
// content (projection.rs).
func (c *projectionContext) projectTextContent(request ProjectionRequest) (core.Value, Fidelity, *ProjectionFailure) {
	root := c.document.Root()
	if root == nil {
		return nil, FidelityExact, &ProjectionFailure{Kind: ProjectionFailureMappingAdmission, Reason: "missing root"}
	}
	start := root.index
	if request.subtree != nil {
		start = int(*request.subtree)
	}
	if c.document.nodes[start].Kind != XmlContentElement {
		return nil, FidelityExact, &ProjectionFailure{Kind: ProjectionFailureSubtreeNotElement}
	}
	var out strings.Builder
	if failure := c.collectText(start, request.include, &out); failure != nil {
		return nil, FidelityExact, failure
	}
	if failure := c.reserveValue(1); failure != nil {
		return nil, FidelityExact, failure
	}
	if failure := c.origin(projectedValue(protocol.RootValuePath()), c.elementNodeRef(start),
		c.elementData(start).Span, ProvenanceRelationDerived); failure != nil {
		return nil, FidelityExact, failure
	}
	return core.String(out.String()), FidelityTransformed, nil
}

// collectText gathers descendant text under the include policy and reports
// every discarded occurrence (projection.rs).
func (c *projectionContext) collectText(index int, include TextContentInclude,
	out *strings.Builder) *ProjectionFailure {
	data := c.elementData(index)
	for _, child := range data.Children {
		content := &c.document.nodes[child]
		switch content.Kind {
		case XmlContentElement:
			if failure := c.event(ProjectionEventElementDiscarded, c.elementNodeRef(child),
				FidelityTransformed); failure != nil {
				return failure
			}
			for _, attribute := range content.Element.Attributes {
				if failure := c.event(ProjectionEventAttributeDiscarded,
					c.occurrenceNodeRef(attribute.Ordinal, document.RoleXmlAttribute),
					FidelityTransformed); failure != nil {
					return failure
				}
			}
			if failure := c.collectText(child, include, out); failure != nil {
				return failure
			}
		case XmlContentText:
			for _, fragment := range content.Text.Fragments {
				if fragment.Kind != ReferenceFragmentLiteral {
					if failure := c.event(ProjectionEventReferenceCollapsed,
						c.occurrenceNodeRef(content.Text.Ordinal, document.RoleXmlEntityReference),
						FidelityTransformed); failure != nil {
						return failure
					}
				}
			}
			// Semantic text: line ends are normalized to LF, matching every
			// other text observation in the crate.
			out.WriteString(TextSemantic(content.Text))
		case XmlContentCdata:
			if include == TextContentIncludeTextAndCdata {
				out.WriteString(content.Cdata.Text)
			} else {
				if failure := c.event(ProjectionEventCdataDiscarded,
					c.occurrenceNodeRef(content.Cdata.Ordinal, document.RoleXmlCdata),
					FidelityTransformed); failure != nil {
					return failure
				}
			}
		case XmlContentComment:
			if failure := c.event(ProjectionEventCommentDiscarded,
				c.occurrenceNodeRef(content.Comment.Ordinal, document.RoleXmlComment),
				FidelityTransformed); failure != nil {
				return failure
			}
		case XmlContentProcessingInstruction:
			if failure := c.event(ProjectionEventProcessingInstructionDiscarded,
				c.occurrenceNodeRef(content.ProcessingInstruction.Ordinal,
					document.RoleXmlProcessingInstruction),
				FidelityTransformed); failure != nil {
				return failure
			}
		}
	}
	return nil
}

// entrySet is the ordered mapping entries with their expanded-name
// identities (projection.rs).
type entrySet struct {
	ordered []entryPair
	seen    map[string]entrySeen
}

type entryPair struct {
	key   string
	value core.Value
}

type entrySeen struct {
	ordinal  int
	expanded *ExpandedName
}

func newEntrySet() *entrySet {
	return &entrySet{seen: make(map[string]entrySeen)}
}

// projectEntryMapping projects the explicit-policy entry mapping of one
// selected subtree (projection.rs).
func (c *projectionContext) projectEntryMapping(request ProjectionRequest) (core.Value, Fidelity, *ProjectionFailure) {
	root := c.document.Root()
	if root == nil {
		return nil, FidelityExact, &ProjectionFailure{Kind: ProjectionFailureMappingAdmission, Reason: "missing root"}
	}
	start := root.index
	if request.subtree != nil {
		start = int(*request.subtree)
	}
	if c.document.nodes[start].Kind != XmlContentElement {
		return nil, FidelityExact, &ProjectionFailure{Kind: ProjectionFailureSubtreeNotElement}
	}
	entries := newEntrySet()
	if failure := c.mapChildren(start, protocol.RootValuePath(), entries, &request); failure != nil {
		return nil, FidelityExact, failure
	}
	builder := core.NewEntryMappingBuilder()
	for _, pair := range entries.ordered {
		builder.Push(core.String(pair.key), pair.value)
	}
	if failure := c.reserveValue(1); failure != nil {
		return nil, FidelityExact, failure
	}
	return builder.Build(), FidelityTransformed, nil
}

// keepPolicy is the collision resolution direction shared by both entry
// policies.
type keepPolicy uint8

const (
	keepReject keepPolicy = iota
	keepFirst
	keepLast
)

func keepFromRepeated(policy RepeatedChildPolicy) keepPolicy {
	switch policy {
	case RepeatedChildPolicyFirst:
		return keepFirst
	case RepeatedChildPolicyLast:
		return keepLast
	}
	return keepReject
}

func keepFromCollision(policy CollisionPolicy) keepPolicy {
	switch policy {
	case CollisionPolicyFirst:
		return keepFirst
	case CollisionPolicyLast:
		return keepLast
	}
	return keepReject
}

// entryOrdinal resolves the entry ordinal under the explicit request
// policies (projection.rs).
func (c *projectionContext) entryOrdinal(entries *entrySet, key string,
	candidate *ExpandedName, request *ProjectionRequest, origin document.NodeRef,
	collapse ProjectionEventKind) (int, *ProjectionFailure) {
	keepRepeated := keepFromRepeated(request.repeatedChild)
	keepCollision := keepFromCollision(request.collision)
	if seen, exists := entries.seen[key]; !exists {
		ordinal := len(entries.ordered)
		entries.seen[key] = entrySeen{ordinal: ordinal, expanded: candidate}
		return ordinal, nil
	} else {
		repeated := false
		if seen.expanded != nil && candidate != nil {
			repeated = seen.expanded.Equal(*candidate)
		}
		keep := keepRepeated
		if !repeated {
			keep = keepCollision
		}
		switch keep {
		case keepReject:
			return 0, &ProjectionFailure{Kind: ProjectionFailureCollision, Child: origin, Key: key}
		case keepFirst, keepLast:
			eventKind := collapse
			if !repeated {
				eventKind = ProjectionEventNamespaceCollapsed
			}
			if failure := c.event(eventKind, origin, FidelityTransformed); failure != nil {
				return 0, failure
			}
			return seen.ordinal, nil
		}
	}
	return 0, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
}

// commitEntry records one committed entry and its value/association
// provenance (projection.rs).
func (c *projectionContext) commitEntry(entries *entrySet, key string, value core.Value,
	ordinal int, source document.NodeRef, sourceSpan document.Span,
	container protocol.ValuePath) *ProjectionFailure {
	if ordinal < len(entries.ordered) {
		entries.ordered[ordinal] = entryPair{key: key, value: value}
	} else {
		entries.ordered = append(entries.ordered, entryPair{key: key, value: value})
	}
	if failure := c.reserveValue(1); failure != nil {
		return failure
	}
	association := protocol.NewAssociationLocation(container, uint64(ordinal),
		protocol.AssociationRoleEntryMappingItem)
	if failure := c.origin(ProjectedLocation{IsAssociation: true, Association: association},
		source, sourceSpan, ProvenanceRelationDirect); failure != nil {
		return failure
	}
	entryValue := container.Child(protocol.ValuePathSegment{Kind: "EntryValue", Index: uint64(ordinal)})
	return c.origin(projectedValue(entryValue), source, sourceSpan, ProvenanceRelationDirect)
}

// mapChildren maps one element's attributes and children into entries
// (projection.rs).
func (c *projectionContext) mapChildren(element int, container protocol.ValuePath,
	entries *entrySet, request *ProjectionRequest) *ProjectionFailure {
	data := c.elementData(element)
	if len(data.Namespaces) > 0 {
		return &ProjectionFailure{Kind: ProjectionFailureMappingAdmission,
			Reason: "namespace declarations on the mapped element"}
	}
	for _, attribute := range data.Attributes {
		origin := c.occurrenceNodeRef(attribute.Ordinal, document.RoleXmlAttribute)
		switch request.attributes {
		case AttributePolicyRejectAttributes:
			return &ProjectionFailure{Kind: ProjectionFailureMappingAdmission,
				Reason: "attributes present under RejectAttributes"}
		case AttributePolicyIgnoreAttributes:
			if failure := c.event(ProjectionEventAttributeDiscarded, origin,
				FidelityTransformed); failure != nil {
				return failure
			}
		case AttributePolicyPrefixAttributeKeys:
			key := "@" + attribute.QName.Local
			ordinal, failure := c.entryOrdinal(entries, key, nil, request, origin,
				ProjectionEventAttributeDiscarded)
			if failure != nil {
				return failure
			}
			if failure := c.commitEntry(entries, key, core.String(attribute.NormalizedValue),
				ordinal, origin, attribute.Span, container); failure != nil {
				return failure
			}
		}
	}
	for _, child := range data.Children {
		content := &c.document.nodes[child]
		switch content.Kind {
		case XmlContentElement:
			childData := content.Element
			namespace := ""
			if childData.Expanded != nil && childData.Expanded.Namespace != nil {
				namespace = *childData.Expanded.Namespace
			}
			local := childData.QName.Local
			var key string
			switch request.keySpelling {
			case ExpandedNameKeyPolicyLocalOnly:
				key = local
			case ExpandedNameKeyPolicyPrefixedSpelling:
				key = childData.QName.QName().String()
			case ExpandedNameKeyPolicyUriBracketed:
				key = "{" + namespace + "}" + local
			}
			origin := c.elementNodeRef(child)
			ordinal, failure := c.entryOrdinal(entries, key, childData.Expanded, request, origin,
				ProjectionEventChildCollapsed)
			if failure != nil {
				return failure
			}
			hasElementChildren := false
			for _, grandchild := range childData.Children {
				if c.document.nodes[grandchild].Kind == XmlContentElement {
					hasElementChildren = true
					break
				}
			}
			var childValue core.Value
			if hasElementChildren {
				nestedContainer := container.Child(protocol.ValuePathSegment{Kind: "EntryValue", Index: uint64(ordinal)})
				nested := newEntrySet()
				if failure := c.mapChildren(child, nestedContainer, nested, request); failure != nil {
					return failure
				}
				nestedBuilder := core.NewEntryMappingBuilder()
				for _, pair := range nested.ordered {
					nestedBuilder.Push(core.String(pair.key), pair.value)
				}
				childValue = nestedBuilder.Build()
			} else {
				value, failure := c.leafValue(child, request)
				if failure != nil {
					return failure
				}
				childValue = value
			}
			if failure := c.commitEntry(entries, key, childValue, ordinal, origin,
				childData.Span, container); failure != nil {
				return failure
			}
		case XmlContentText:
			switch request.textKey {
			case TextKeyPolicyRejectText:
				if strings.TrimSpace(TextSemantic(content.Text)) != "" {
					return &ProjectionFailure{Kind: ProjectionFailureMappingAdmission,
						Reason: "text content under RejectText"}
				}
			case TextKeyPolicyIgnoreText:
				if failure := c.event(ProjectionEventTextDiscarded,
					c.occurrenceNodeRef(content.Text.Ordinal, document.RoleXmlText),
					FidelityTransformed); failure != nil {
					return failure
				}
			}
		case XmlContentCdata:
			switch request.textKey {
			case TextKeyPolicyRejectText:
				return &ProjectionFailure{Kind: ProjectionFailureMappingAdmission,
					Reason: "CDATA content under RejectText"}
			case TextKeyPolicyIgnoreText:
				if failure := c.event(ProjectionEventCdataDiscarded,
					c.occurrenceNodeRef(content.Cdata.Ordinal, document.RoleXmlCdata),
					FidelityTransformed); failure != nil {
					return failure
				}
			}
		case XmlContentComment:
			if failure := c.event(ProjectionEventCommentDiscarded,
				c.occurrenceNodeRef(content.Comment.Ordinal, document.RoleXmlComment),
				FidelityTransformed); failure != nil {
				return failure
			}
		case XmlContentProcessingInstruction:
			if failure := c.event(ProjectionEventProcessingInstructionDiscarded,
				c.occurrenceNodeRef(content.ProcessingInstruction.Ordinal,
					document.RoleXmlProcessingInstruction),
				FidelityTransformed); failure != nil {
				return failure
			}
		}
	}
	return nil
}

// leafValue builds the leaf value of one element without element children
// (projection.rs).
func (c *projectionContext) leafValue(element int, request *ProjectionRequest) (core.Value, *ProjectionFailure) {
	data := c.elementData(element)
	var text strings.Builder
	for _, child := range data.Children {
		content := &c.document.nodes[child]
		switch content.Kind {
		case XmlContentText:
			text.WriteString(TextSemantic(content.Text))
		case XmlContentCdata:
			switch request.textKey {
			case TextKeyPolicyRejectText:
				return nil, &ProjectionFailure{Kind: ProjectionFailureMappingAdmission,
					Reason: "CDATA content under RejectText"}
			case TextKeyPolicyIgnoreText:
				if failure := c.event(ProjectionEventCdataDiscarded,
					c.occurrenceNodeRef(content.Cdata.Ordinal, document.RoleXmlCdata),
					FidelityTransformed); failure != nil {
					return nil, failure
				}
			}
		case XmlContentComment:
			if failure := c.event(ProjectionEventCommentDiscarded,
				c.occurrenceNodeRef(content.Comment.Ordinal, document.RoleXmlComment),
				FidelityTransformed); failure != nil {
				return nil, failure
			}
		case XmlContentProcessingInstruction:
			if failure := c.event(ProjectionEventProcessingInstructionDiscarded,
				c.occurrenceNodeRef(content.ProcessingInstruction.Ordinal,
					document.RoleXmlProcessingInstruction),
				FidelityTransformed); failure != nil {
				return nil, failure
			}
		}
	}
	return core.String(text.String()), nil
}

// failedProjection builds the failed projection result with the stable
// diagnostic (projection.rs).
func failedProjection(failure *ProjectionFailure) ProjectionResult {
	diagnostic := &protocol.Diagnostic{
		Code:      failure.Code(),
		Category:  protocol.CategoryProjection,
		Severity:  protocol.SeverityError,
		Arguments: map[string]string{"failure": failure.Error()},
	}
	return ProjectionResult{Failed: &FailedProjectionAttempt{
		Diagnostics: []*protocol.Diagnostic{diagnostic},
	}}
}

// projectedValue builds one Value projected location.
func projectedValue(path protocol.ValuePath) ProjectedLocation {
	return ProjectedLocation{Path: path}
}
