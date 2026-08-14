package yaml

import (
	"math/big"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/graph"
	"consema.dev/consema/protocol"
)

// This file implements the YAML projections (consema-yaml projection.rs):
// the exact best-exact-graph projection preserving tags, sharing, and
// cycles, and the best-exact-value projection with explicit sharing, tag,
// and mapping policies. Projection never expands aliases implicitly and
// never invents application semantics.

// GraphProjectionLimits are the resource bounds of one graph projection
// (projection.rs).
type GraphProjectionLimits struct {
	// Graph are the PortableGraph construction and traversal limits.
	Graph graph.Limits
	// MaxProvenanceEntries is the maximum projected-location plus origin
	// records.
	MaxProvenanceEntries int
}

// DefaultGraphProjectionLimits returns the frozen defaults.
func DefaultGraphProjectionLimits() GraphProjectionLimits {
	return GraphProjectionLimits{
		Graph:                graph.DefaultLimits(),
		MaxProvenanceEntries: 2_000_000,
	}
}

// GraphProjectionRequest is the immutable `yaml.projection.best-exact-graph@1`
// request (projection.rs).
type GraphProjectionRequest struct {
	limits GraphProjectionLimits
}

// BestExactGraphV1 returns the frozen exact graph request with default
// limits.
func BestExactGraphV1() GraphProjectionRequest {
	return GraphProjectionRequest{limits: DefaultGraphProjectionLimits()}
}

// WithLimits replaces the immutable resource limits.
func (r GraphProjectionRequest) WithLimits(limits GraphProjectionLimits) GraphProjectionRequest {
	r.limits = limits
	return r
}

// Limits returns the resource limits.
func (r GraphProjectionRequest) Limits() GraphProjectionLimits { return r.limits }

// GraphProjectedLocation is one projected graph location
// (projection.rs).
type GraphProjectedLocation struct {
	// Kind is "Root", "Node", "SequenceElement", "MappingKey", or
	// "MappingValue".
	Kind string
	// Ordinal is the ordered root or association ordinal.
	Ordinal uint64
	// Parent is the parent graph node of association locations.
	Parent graph.NodeID
}

// ProvenanceRelation classifies one provenance origin (projection.rs:
// 95-105).
type ProvenanceRelation uint8

// The four frozen provenance relations.
const (
	// ProvenanceDirect is a direct native semantic origin.
	ProvenanceDirect ProvenanceRelation = iota
	// ProvenanceReference is an alias edge referring to a shared
	// representation node.
	ProvenanceReference
	// ProvenanceExpanded is an alias edge explicitly duplicated into a
	// PortableValue tree.
	ProvenanceExpanded
	// ProvenanceTagStripped is a tag explicitly removed by policy.
	ProvenanceTagStripped
)

// String returns the stable relation name.
func (r ProvenanceRelation) String() string {
	switch r {
	case ProvenanceDirect:
		return "Direct"
	case ProvenanceReference:
		return "Reference"
	case ProvenanceExpanded:
		return "Expanded"
	case ProvenanceTagStripped:
		return "TagStripped"
	}
	return "ProvenanceRelation"
}

// SourceOrigin is one exact source origin of a projected location
// (projection.rs).
type SourceOrigin struct {
	// Snapshot is the source snapshot identity.
	Snapshot document.SnapshotIdentity
	// Node is the exact structural identity.
	Node document.NodeRef
	// Span is the exact raw source range.
	Span document.Span
	// Relation is the provenance relation.
	Relation ProvenanceRelation
}

// GraphProvenanceEntry is one projected location with its origins
// (projection.rs).
type GraphProvenanceEntry struct {
	// Projected is the graph location.
	Projected GraphProjectedLocation
	// Origins are the ordered origins.
	Origins []SourceOrigin
}

// GraphProvenanceMap is the immutable graph provenance map.
type GraphProvenanceMap struct {
	entries []GraphProvenanceEntry
}

// Entries returns the deterministic entries in root/node/association
// construction order.
func (m GraphProvenanceMap) Entries() []GraphProvenanceEntry {
	return append([]GraphProvenanceEntry(nil), m.entries...)
}

// CompleteGraphProjection is the complete graph projection result
// (projection.rs).
type CompleteGraphProjection struct {
	// Graph is the exact PortableGraph.
	Graph *graph.Graph
	// Provenance maps every graph location to its source origins.
	Provenance GraphProvenanceMap
}

// GraphProjectionFailureKind classifies one graph projection failure.
type GraphProjectionFailureKind uint8

// The stable graph projection failure classes.
const (
	// GraphProjectionUnsupportedTag: a custom tag has no published graph
	// canonical semantics.
	GraphProjectionUnsupportedTag GraphProjectionFailureKind = iota
	// GraphProjectionGraph: graph construction failed atomically.
	GraphProjectionGraph
	// GraphProjectionProvenanceLimit: the provenance resource limit was
	// exceeded atomically.
	GraphProjectionProvenanceLimit
)

// GraphProjectionFailure is the typed graph projection failure
// (projection.rs). It implements error and the Code() contract.
type GraphProjectionFailure struct {
	// Kind identifies the failure.
	Kind GraphProjectionFailureKind
	// Tag is the offending tag of UnsupportedTag.
	Tag string
	// GraphError is the underlying graph construction error.
	GraphError *graph.GraphError
}

// Error implements error; the text is human presentation only.
func (f *GraphProjectionFailure) Error() string {
	switch f.Kind {
	case GraphProjectionUnsupportedTag:
		return "yaml: unsupported graph tag " + f.Tag
	case GraphProjectionGraph:
		return "yaml: graph projection failed: " + f.GraphError.Error()
	case GraphProjectionProvenanceLimit:
		return "yaml: graph projection provenance limit reached"
	}
	return "yaml: graph projection failed"
}

// Code returns the frozen registered code.
func (f *GraphProjectionFailure) Code() string {
	switch f.Kind {
	case GraphProjectionUnsupportedTag:
		return "yaml.projection.unsupported-tag@1"
	case GraphProjectionProvenanceLimit:
		return "yaml.projection.provenance-limit@1"
	case GraphProjectionGraph:
		if f.GraphError != nil && (f.GraphError.Kind == graph.ErrGraphResourceLimit ||
			f.GraphError.Kind == graph.ErrGraphSizeOverflow) {
			return "yaml.projection.resource-limit@1"
		}
		return "yaml.projection.graph-invalid@1"
	}
	return "yaml.projection.graph-invalid@1"
}

// ProjectGraph projects all document roots to one exact PortableGraph
// (consema-yaml lib.rs). Unknown/custom tags fail instead of being
// treated as application constructors or untyped strings; frozen standard
// repository tags remain exact tagged graph nodes. Aliases compose to
// shared graph identity and are never expanded.
func (d *Document) ProjectGraph() (*graph.Graph, error) {
	return d.ProjectGraphBounded(graph.DefaultLimits())
}

// ProjectGraphBounded projects all document roots with caller-supplied
// graph resource limits; the provenance bound keeps its frozen default.
func (d *Document) ProjectGraphBounded(limits graph.Limits) (*graph.Graph, error) {
	projected, failure := d.ProjectGraphWithProvenance(
		BestExactGraphV1().WithLimits(GraphProjectionLimits{
			Graph:                limits,
			MaxProvenanceEntries: DefaultGraphProjectionLimits().MaxProvenanceEntries,
		}))
	if failure != nil {
		return nil, failure
	}
	return projected.Graph, nil
}

// ProjectGraphWithProvenance projects all document roots and their
// complete provenance (projection.rs).
func (d *Document) ProjectGraphWithProvenance(
	request GraphProjectionRequest) (*CompleteGraphProjection, *GraphProjectionFailure) {
	builder := graph.NewBuilder(request.limits.Graph)
	ids := make([]graph.NodeID, len(d.native.nodes))
	for index := range d.native.nodes {
		id, err := builder.ReserveNode()
		if err != nil {
			return nil, &GraphProjectionFailure{Kind: GraphProjectionGraph, GraphError: asGraphError(err)}
		}
		ids[index] = id
	}
	for index := range d.native.nodes {
		node := &d.native.nodes[index]
		if !standardGraphTags[node.tag] {
			return nil, &GraphProjectionFailure{Kind: GraphProjectionUnsupportedTag, Tag: node.tag}
		}
		var err error
		switch node.content.kind {
		case contentScalar:
			err = builder.DefineScalar(ids[index], node.tag, node.content.scalar.canonical)
		case contentSequence:
			items := make([]graph.NodeID, 0, len(node.content.items))
			for _, item := range node.content.items {
				items = append(items, ids[item.node])
			}
			err = builder.DefineSequence(ids[index], node.tag, items)
		case contentMapping:
			entries := make([]graph.MappingEntry, 0, len(node.content.entries))
			for _, entry := range node.content.entries {
				entries = append(entries, graph.MappingEntry{Key: ids[entry.key], Value: ids[entry.value]})
			}
			err = builder.DefineMapping(ids[index], node.tag, entries)
		}
		if err != nil {
			return nil, &GraphProjectionFailure{Kind: GraphProjectionGraph, GraphError: asGraphError(err)}
		}
	}
	for _, document := range d.native.documents {
		if err := builder.PushRoot(ids[document.root]); err != nil {
			return nil, &GraphProjectionFailure{Kind: GraphProjectionGraph, GraphError: asGraphError(err)}
		}
	}
	graphValue, err := builder.Build()
	if err != nil {
		return nil, &GraphProjectionFailure{Kind: GraphProjectionGraph, GraphError: asGraphError(err)}
	}
	provenance, failure := d.buildGraphProvenance(graphValue, ids, request.limits.MaxProvenanceEntries)
	if failure != nil {
		return nil, failure
	}
	return &CompleteGraphProjection{Graph: graphValue, Provenance: provenance}, nil
}

// buildGraphProvenance emits the deterministic root/node/association
// provenance entries (projection.rs).
func (d *Document) buildGraphProvenance(graphValue *graph.Graph, ids []graph.NodeID,
	maxEntries int) (GraphProvenanceMap, *GraphProjectionFailure) {
	var entries []GraphProvenanceEntry
	units := 0
	index := make(map[GraphProjectedLocation]int)
	add := func(location GraphProjectedLocation, origin SourceOrigin) *GraphProjectionFailure {
		if existing, ok := index[location]; ok {
			units++
			if units > maxEntries {
				return &GraphProjectionFailure{Kind: GraphProjectionProvenanceLimit}
			}
			entries[existing].Origins = append(entries[existing].Origins, origin)
			return nil
		}
		units += 2
		if units > maxEntries {
			return &GraphProjectionFailure{Kind: GraphProjectionProvenanceLimit}
		}
		index[location] = len(entries)
		entries = append(entries, GraphProvenanceEntry{
			Projected: location, Origins: []SourceOrigin{origin},
		})
		return nil
	}
	for ordinal, doc := range d.native.documents {
		origin := SourceOrigin{
			Snapshot: d.SnapshotIdentity(),
			Node:     d.authority.NodeRef(uint64(ordinal), document.RoleYamlDocument),
			Span:     doc.span,
			Relation: ProvenanceDirect,
		}
		if failure := add(GraphProjectedLocation{Kind: "Root", Ordinal: uint64(ordinal)}, origin); failure != nil {
			return GraphProvenanceMap{}, failure
		}
	}
	for index := range d.native.nodes {
		node := &d.native.nodes[index]
		origin := SourceOrigin{
			Snapshot: d.SnapshotIdentity(),
			Node:     d.nodeRef(index),
			Span:     node.span,
			Relation: ProvenanceDirect,
		}
		if failure := add(GraphProjectedLocation{Kind: "Node", Parent: ids[index]},
			origin); failure != nil {
			return GraphProvenanceMap{}, failure
		}
		switch node.content.kind {
		case contentSequence:
			for ordinal, item := range node.content.items {
				location := GraphProjectedLocation{
					Kind: "SequenceElement", Parent: ids[index], Ordinal: uint64(ordinal),
				}
				edgeOrigin := SourceOrigin{
					Snapshot: d.SnapshotIdentity(),
					Node:     d.authority.NodeRef(item.identity, document.RoleYamlSequenceElement),
					Span:     item.span, Relation: ProvenanceDirect,
				}
				if failure := add(location, edgeOrigin); failure != nil {
					return GraphProvenanceMap{}, failure
				}
				if item.alias != nil {
					alias := &d.native.aliases[*item.alias]
					reference := SourceOrigin{
						Snapshot: d.SnapshotIdentity(),
						Node:     d.authority.NodeRef(alias.identity, document.RoleYamlAlias),
						Span:     alias.span, Relation: ProvenanceReference,
					}
					if failure := add(location, reference); failure != nil {
						return GraphProvenanceMap{}, failure
					}
				}
			}
		case contentMapping:
			for ordinal, entry := range node.content.entries {
				for _, kind := range []string{"MappingKey", "MappingValue"} {
					location := GraphProjectedLocation{
						Kind: kind, Parent: ids[index], Ordinal: uint64(ordinal),
					}
					edgeOrigin := SourceOrigin{
						Snapshot: d.SnapshotIdentity(),
						Node:     d.authority.NodeRef(entry.identity, document.RoleYamlMappingEntry),
						Span:     entry.span, Relation: ProvenanceDirect,
					}
					if failure := add(location, edgeOrigin); failure != nil {
						return GraphProvenanceMap{}, failure
					}
				}
				aliases := []*int{entry.keyAlias, entry.valueAlias}
				for _, aliasOrdinal := range aliases {
					if aliasOrdinal != nil {
						alias := &d.native.aliases[*aliasOrdinal]
						kind := "MappingKey"
						if aliasOrdinal == entry.valueAlias {
							kind = "MappingValue"
						}
						location := GraphProjectedLocation{
							Kind: kind, Parent: ids[index], Ordinal: uint64(ordinal),
						}
						reference := SourceOrigin{
							Snapshot: d.SnapshotIdentity(),
							Node:     d.authority.NodeRef(alias.identity, document.RoleYamlAlias),
							Span:     alias.span, Relation: ProvenanceReference,
						}
						if failure := add(location, reference); failure != nil {
							return GraphProvenanceMap{}, failure
						}
					}
				}
			}
		}
	}
	return GraphProvenanceMap{entries: entries}, nil
}

func asGraphError(err error) *graph.GraphError {
	if graphError, ok := err.(*graph.GraphError); ok {
		return graphError
	}
	return &graph.GraphError{Kind: graph.ErrGraphInvalidUTF8}
}

// SharingPolicy is the explicit sharing policy of one value projection
// (projection.rs).
type SharingPolicy uint8

// The two frozen sharing policies.
const (
	// SharingPolicyReject fails when sharing or aliases occur; graph
	// identity is never silently discarded.
	SharingPolicyReject SharingPolicy = iota
	// SharingPolicyDuplicateAcyclic duplicates acyclic sharing and reports
	// every duplication; cycles still fail.
	SharingPolicyDuplicateAcyclic
)

// TagPolicy is the explicit tag policy of one value projection
// (projection.rs).
type TagPolicy uint8

// The two frozen tag policies.
const (
	// TagPolicyRequireKnownPortableTag accepts only tags with a frozen
	// exact PortableValue lowering.
	TagPolicyRequireKnownPortableTag TagPolicy = iota
	// TagPolicyStripToNodeKind removes unsupported standard and custom tags
	// and reports every removal.
	TagPolicyStripToNodeKind
)

// MappingPolicy is the explicit mapping policy of one value projection
// (projection.rs).
type MappingPolicy uint8

// The three frozen mapping policies.
const (
	// MappingPolicyBestExactObjectOrEntryMapping uses Object only for
	// unique string keys, otherwise EntryMapping.
	MappingPolicyBestExactObjectOrEntryMapping MappingPolicy = iota
	// MappingPolicyRequireObject requires every mapping to satisfy the
	// unique-string Object invariants.
	MappingPolicyRequireObject
	// MappingPolicyRequireEntryMapping preserves every mapping as an
	// ordered EntryMapping.
	MappingPolicyRequireEntryMapping
)

// ValueProjectionLimits are the resource bounds of one value projection
// (projection.rs).
type ValueProjectionLimits struct {
	// MaxValueNodes is the maximum projected native/value node visits.
	MaxValueNodes int
	// MaxDepth is the maximum recursive graph depth.
	MaxDepth int
	// MaxReportEntries is the maximum report events.
	MaxReportEntries int
	// MaxProvenanceEntries is the maximum projected-location plus origin
	// records.
	MaxProvenanceEntries int
	// MaxAmplificationRatio is the maximum output-node visits divided by
	// unique native nodes.
	MaxAmplificationRatio int
}

// DefaultValueProjectionLimits returns the frozen defaults (1M value
// nodes, depth 256, 100k report entries, 2M provenance entries, ratio 16).
func DefaultValueProjectionLimits() ValueProjectionLimits {
	return ValueProjectionLimits{
		MaxValueNodes:         1_000_000,
		MaxDepth:              256,
		MaxReportEntries:      100_000,
		MaxProvenanceEntries:  2_000_000,
		MaxAmplificationRatio: 16,
	}
}

// ValueProjectionRequest is the immutable `yaml.projection.best-exact-value@1`
// request (projection.rs).
type ValueProjectionRequest struct {
	sharing SharingPolicy
	tags    TagPolicy
	mapping MappingPolicy
	limits  ValueProjectionLimits
}

// BestExactValueV1 returns the frozen default request: one document, no
// sharing or cycles, known tags, exact-first mapping.
func BestExactValueV1() ValueProjectionRequest {
	return ValueProjectionRequest{
		sharing: SharingPolicyReject,
		tags:    TagPolicyRequireKnownPortableTag,
		mapping: MappingPolicyBestExactObjectOrEntryMapping,
		limits:  DefaultValueProjectionLimits(),
	}
}

// WithSharing replaces the sharing policy.
func (r ValueProjectionRequest) WithSharing(sharing SharingPolicy) ValueProjectionRequest {
	r.sharing = sharing
	return r
}

// WithTags replaces the tag policy.
func (r ValueProjectionRequest) WithTags(tags TagPolicy) ValueProjectionRequest {
	r.tags = tags
	return r
}

// WithMapping replaces the mapping policy.
func (r ValueProjectionRequest) WithMapping(mapping MappingPolicy) ValueProjectionRequest {
	r.mapping = mapping
	return r
}

// WithLimits replaces the immutable resource limits.
func (r ValueProjectionRequest) WithLimits(limits ValueProjectionLimits) ValueProjectionRequest {
	r.limits = limits
	return r
}

// Sharing returns the sharing policy.
func (r ValueProjectionRequest) Sharing() SharingPolicy { return r.sharing }

// Tags returns the tag policy.
func (r ValueProjectionRequest) Tags() TagPolicy { return r.tags }

// Mapping returns the mapping policy.
func (r ValueProjectionRequest) Mapping() MappingPolicy { return r.mapping }

// Limits returns the resource limits.
func (r ValueProjectionRequest) Limits() ValueProjectionLimits { return r.limits }

// Fidelity is the projection fidelity classification (projection.rs:
// 335-343). Exact < Transformed < Lossy.
type Fidelity uint8

// The three frozen fidelity classes.
const (
	// FidelityExact means the target directly represents the source
	// semantics.
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
	return "Fidelity"
}

// ProjectionEventKind classifies one projection event (projection.rs:
// 378-384).
type ProjectionEventKind uint8

// The two frozen event kinds.
const (
	// ProjectionEventSharingDuplicated reports an explicitly authorized
	// acyclic duplication.
	ProjectionEventSharingDuplicated ProjectionEventKind = iota
	// ProjectionEventTagStripped reports an explicitly removed tag.
	ProjectionEventTagStripped
)

// String returns the stable event kind name.
func (k ProjectionEventKind) String() string {
	switch k {
	case ProjectionEventSharingDuplicated:
		return "SharingDuplicated"
	case ProjectionEventTagStripped:
		return "TagStripped"
	}
	return "ProjectionEventKind"
}

// ProjectionEvent is one reported projection transformation
// (projection.rs).
type ProjectionEvent struct {
	// Kind is the event category.
	Kind ProjectionEventKind
	// Policy is the policy that authorized the event.
	Policy string
	// Source is the exact source identity.
	Source document.NodeRef
	// Projected is the projected value location.
	Projected protocol.ValuePath
	// OldCategory is the stable old semantic category.
	OldCategory string
	// NewCategory is the stable new semantic category.
	NewCategory string
	// Reversible reports whether output plus contract can recover the
	// fact.
	Reversible bool
	// Loss is the fidelity impact.
	Loss Fidelity
}

// ProjectionReport is the ordered projection transformation report
// (projection.rs).
type ProjectionReport struct {
	events []ProjectionEvent
}

// Events returns the deterministic traversal-order events.
func (r ProjectionReport) Events() []ProjectionEvent {
	return append([]ProjectionEvent(nil), r.events...)
}

// ProjectedLocation is one projected value or association location
// (projection.rs).
type ProjectedLocation struct {
	// Kind is "Value" or "Association".
	Kind string
	// Path is the value location.
	Path protocol.ValuePath
	// Association is the association location.
	Association *protocol.AssociationLocation
}

// ProvenanceEntry is one projected location with its origins
// (projection.rs).
type ProvenanceEntry struct {
	// Projected is the projected location.
	Projected ProjectedLocation
	// Origins are the ordered origins.
	Origins []SourceOrigin
}

// ProvenanceMap is the immutable value projection provenance map.
type ProvenanceMap struct {
	entries []ProvenanceEntry
}

// Entries returns the deterministic projection-order entries.
func (m ProvenanceMap) Entries() []ProvenanceEntry {
	return append([]ProvenanceEntry(nil), m.entries...)
}

// CompleteValueProjection is the complete value projection result
// (projection.rs).
type CompleteValueProjection struct {
	// Value is the complete immutable tree value.
	Value core.Value
	// Fidelity is the worst fidelity of the complete operation.
	Fidelity Fidelity
	// Report is the transformation report.
	Report ProjectionReport
	// Provenance maps every projected location to its origins.
	Provenance ProvenanceMap
}

// ValueProjectionFailureKind classifies one value projection failure
// (projection.rs).
type ValueProjectionFailureKind uint8

// The stable value projection failure classes.
const (
	// ValueProjectionDocumentCardinality: the stream does not contain
	// exactly one document.
	ValueProjectionDocumentCardinality ValueProjectionFailureKind = iota
	// ValueProjectionCycle: a node closes the active traversal cycle.
	ValueProjectionCycle
	// ValueProjectionSharing: a shared representation node was revisited
	// under the reject policy.
	ValueProjectionSharing
	// ValueProjectionUnsupportedTag: a resolved tag has no exact lowering.
	ValueProjectionUnsupportedTag
	// ValueProjectionMappingNotObject: a mapping cannot satisfy an
	// explicitly required Object policy.
	ValueProjectionMappingNotObject
	// ValueProjectionInvalidCanonicalScalar: a canonical scalar could not
	// form the promised category.
	ValueProjectionInvalidCanonicalScalar
	// ValueProjectionUnrepresentableTimestamp: a timestamp is valid but
	// outside the PortableValue temporal categories.
	ValueProjectionUnrepresentableTimestamp
	// ValueProjectionResourceLimit: a declared resource limit was reached.
	ValueProjectionResourceLimit
)

// ValueProjectionFailure is the typed value projection failure
// (projection.rs). It implements error and the Code() contract.
// A failed attempt never contains a partial value, report, or provenance.
type ValueProjectionFailure struct {
	// Kind identifies the failure.
	Kind ValueProjectionFailureKind
	// Node is the offending source node.
	Node document.NodeRef
	// Tag is the offending resolved tag.
	Tag string
	// LimitName is the stable limit name of ResourceLimit failures.
	LimitName string
}

// Error implements error; the text is human presentation only.
func (f *ValueProjectionFailure) Error() string {
	switch f.Kind {
	case ValueProjectionDocumentCardinality:
		return "yaml: value projection requires exactly one document"
	case ValueProjectionCycle:
		return "yaml: value projection cycle"
	case ValueProjectionSharing:
		return "yaml: value projection sharing rejected"
	case ValueProjectionUnsupportedTag:
		return "yaml: unsupported projection tag " + f.Tag
	case ValueProjectionMappingNotObject:
		return "yaml: mapping cannot project as Object"
	case ValueProjectionInvalidCanonicalScalar:
		return "yaml: invalid canonical scalar"
	case ValueProjectionUnrepresentableTimestamp:
		return "yaml: timestamp outside PortableValue temporal categories"
	case ValueProjectionResourceLimit:
		return "yaml: projection resource limit " + f.LimitName + " reached"
	}
	return "yaml: value projection failed"
}

// Code returns the frozen registered code.
func (f *ValueProjectionFailure) Code() string {
	switch f.Kind {
	case ValueProjectionDocumentCardinality:
		return "yaml.projection.document-cardinality@1"
	case ValueProjectionCycle:
		return "yaml.projection.cycle@1"
	case ValueProjectionSharing:
		return "yaml.projection.sharing@1"
	case ValueProjectionUnsupportedTag:
		return "yaml.projection.unsupported-tag@1"
	case ValueProjectionMappingNotObject:
		return "yaml.projection.mapping-not-object@1"
	case ValueProjectionInvalidCanonicalScalar:
		return "yaml.projection.invalid-canonical-scalar@1"
	case ValueProjectionUnrepresentableTimestamp:
		return "yaml.projection.unrepresentable-timestamp@1"
	case ValueProjectionResourceLimit:
		return "yaml.projection.resource-limit@1"
	}
	return "yaml.projection.resource-limit@1"
}

// ValueProjectionResult is the value projection completion algebra
// (projection.rs). Exactly one outcome is non-nil.
type ValueProjectionResult struct {
	// Complete is the complete success outcome.
	Complete *CompleteValueProjection
	// Failed is the failed attempt with no partial value.
	Failed *ValueProjectionFailure
}

// ProjectValue projects the single document root to one exact
// PortableValue (projection.rs). The default request requires
// exactly one document, rejects sharing and cycles, requires known tags,
// and selects Object only for unique string keys.
func (d *Document) ProjectValue(request ValueProjectionRequest) ValueProjectionResult {
	if d.DocumentCount() != 1 {
		return ValueProjectionResult{Failed: &ValueProjectionFailure{
			Kind: ValueProjectionDocumentCardinality,
		}}
	}
	if request.limits.MaxAmplificationRatio == 0 {
		return ValueProjectionResult{Failed: &ValueProjectionFailure{
			Kind: ValueProjectionResourceLimit, LimitName: "max_amplification_ratio",
		}}
	}
	context := &valueProjectionContext{
		document:        d,
		request:         request,
		seen:            make(map[int]bool),
		stack:           make(map[int]bool),
		provenanceIndex: make(map[string]int),
		fidelity:        FidelityExact,
	}
	path := protocol.RootValuePath()
	value, failure := context.projectNode(d.native.documents[0].root, path, 0, nil)
	if failure != nil {
		return ValueProjectionResult{Failed: failure}
	}
	maximum := len(context.seen) * request.limits.MaxAmplificationRatio
	if context.visits > maximum {
		return ValueProjectionResult{Failed: &ValueProjectionFailure{
			Kind: ValueProjectionResourceLimit, LimitName: "max_amplification_ratio",
		}}
	}
	return ValueProjectionResult{Complete: &CompleteValueProjection{
		Value:      value,
		Fidelity:   context.fidelity,
		Report:     ProjectionReport{events: context.report},
		Provenance: ProvenanceMap{entries: context.provenance},
	}}
}

type valueProjectionContext struct {
	document        *Document
	request         ValueProjectionRequest
	seen            map[int]bool
	stack           map[int]bool
	visits          int
	report          []ProjectionEvent
	provenance      []ProvenanceEntry
	provenanceIndex map[string]int
	units           int
	fidelity        Fidelity
}

// projectedLocationKey builds the deterministic unique key of one
// projected location (the Go counterpart of the Rust derived Hash on
// ProjectedLocation).
func projectedLocationKey(location ProjectedLocation) string {
	var builder strings.Builder
	if location.Kind == "Association" {
		builder.WriteString("A")
		if location.Association != nil {
			builder.WriteString(pathKey(location.Association.Container()))
			builder.WriteString("|")
			builder.WriteString(itoaU64(location.Association.Ordinal()))
			builder.WriteString("|")
			builder.WriteString(string(location.Association.Role()))
		}
		return builder.String()
	}
	builder.WriteString("V")
	builder.WriteString(pathKey(location.Path))
	return builder.String()
}

// pathKey encodes one value path unambiguously.
func pathKey(path protocol.ValuePath) string {
	var builder strings.Builder
	for _, segment := range path.Segments() {
		builder.WriteString(segment.Kind)
		builder.WriteString(":")
		if segment.Kind == "ObjectValue" {
			builder.WriteString(segment.Key)
		} else {
			builder.WriteString(itoaU64(segment.Index))
		}
		builder.WriteString(";")
	}
	return builder.String()
}

func itoaU64(value uint64) string {
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

func (c *valueProjectionContext) fail(kind ValueProjectionFailureKind,
	node document.NodeRef) *ValueProjectionFailure {
	return &ValueProjectionFailure{Kind: kind, Node: node}
}

func (c *valueProjectionContext) failLimit(name string) *ValueProjectionFailure {
	return &ValueProjectionFailure{Kind: ValueProjectionResourceLimit, LimitName: name}
}

// pushEvent records one transformation event and tracks the worst
// fidelity.
func (c *valueProjectionContext) pushEvent(event ProjectionEvent) *ValueProjectionFailure {
	if len(c.report)+1 > c.request.limits.MaxReportEntries {
		return c.failLimit("max_report_entries")
	}
	c.report = append(c.report, event)
	if event.Loss > c.fidelity {
		c.fidelity = event.Loss
	}
	return nil
}

// addOrigin charges one provenance unit and appends an origin.
func (c *valueProjectionContext) addOrigin(location ProjectedLocation,
	origin SourceOrigin) *ValueProjectionFailure {
	if existing, ok := c.provenanceIndex[projectedLocationKey(location)]; ok {
		c.units++
		if c.units > c.request.limits.MaxProvenanceEntries {
			return c.failLimit("max_provenance_entries")
		}
		c.provenance[existing].Origins = append(c.provenance[existing].Origins, origin)
		return nil
	}
	c.units += 2
	if c.units > c.request.limits.MaxProvenanceEntries {
		return c.failLimit("max_provenance_entries")
	}
	c.provenanceIndex[projectedLocationKey(location)] = len(c.provenance)
	c.provenance = append(c.provenance, ProvenanceEntry{
		Projected: location, Origins: []SourceOrigin{origin},
	})
	return nil
}

// projectNode projects one native node into a PortableValue
// (projection.rs).
func (c *valueProjectionContext) projectNode(index int, path protocol.ValuePath,
	depth int, incomingAlias *int) (core.Value, *ValueProjectionFailure) {
	if depth > c.request.limits.MaxDepth {
		return nil, c.failLimit("max_depth")
	}
	c.visits++
	if c.visits > c.request.limits.MaxValueNodes {
		return nil, c.failLimit("max_value_nodes")
	}
	node := &c.document.native.nodes[index]
	nodeRef := c.document.nodeRef(index)
	if c.stack[index] {
		return nil, c.fail(ValueProjectionCycle, nodeRef)
	}
	if c.seen[index] {
		if c.request.sharing == SharingPolicyReject {
			return nil, c.fail(ValueProjectionSharing, nodeRef)
		}
		source := nodeRef
		if incomingAlias != nil {
			source = c.document.aliasRef(*incomingAlias)
		}
		event := ProjectionEvent{
			Kind: ProjectionEventSharingDuplicated, Policy: "DuplicateAcyclicSharing@1",
			Source: source, Projected: path,
			OldCategory: "SharedGraphNode", NewCategory: "DuplicatedTreeValue",
			Reversible: false, Loss: FidelityTransformed,
		}
		if failure := c.pushEvent(event); failure != nil {
			return nil, failure
		}
		// Duplicate in place: continue projecting this node again.
	}
	c.seen[index] = true
	c.stack[index] = true
	defer func() { delete(c.stack, index) }()
	supported := c.isPortableTag(node)
	stripped := false
	if !supported {
		if c.request.tags == TagPolicyRequireKnownPortableTag {
			return nil, &ValueProjectionFailure{Kind: ValueProjectionUnsupportedTag,
				Node: nodeRef, Tag: node.tag}
		}
		event := ProjectionEvent{
			Kind: ProjectionEventTagStripped, Policy: "StripToNodeKind@1",
			Source: nodeRef, Projected: path,
			OldCategory: node.tag, NewCategory: c.nodeKindName(node),
			Reversible: false, Loss: FidelityLossy,
		}
		if failure := c.pushEvent(event); failure != nil {
			return nil, failure
		}
		stripped = true
	}
	var value core.Value
	var failure *ValueProjectionFailure
	switch node.content.kind {
	case contentScalar:
		value, failure = c.projectScalar(&node.content.scalar, supported, nodeRef)
	case contentSequence:
		value, failure = c.projectSequence(index, node, path, depth)
	case contentMapping:
		value, failure = c.projectMapping(index, node, path, depth)
	}
	if failure != nil {
		return nil, failure
	}
	relation := ProvenanceDirect
	if stripped {
		relation = ProvenanceTagStripped
	}
	if failure := c.addOrigin(ProjectedLocation{Kind: "Value", Path: path},
		SourceOrigin{Snapshot: c.document.SnapshotIdentity(), Node: nodeRef,
			Span: node.span, Relation: relation}); failure != nil {
		return nil, failure
	}
	if incomingAlias != nil {
		alias := &c.document.native.aliases[*incomingAlias]
		if failure := c.addOrigin(ProjectedLocation{Kind: "Value", Path: path},
			SourceOrigin{Snapshot: c.document.SnapshotIdentity(),
				Node: c.document.authority.NodeRef(alias.identity, document.RoleYamlAlias),
				Span: alias.span, Relation: ProvenanceExpanded}); failure != nil {
			return nil, failure
		}
	}
	return value, nil
}

// isPortableTag reports whether the node's tag has a frozen exact
// PortableValue lowering for its content kind (projection.rs).
func (c *valueProjectionContext) isPortableTag(node *nativeNode) bool {
	switch node.content.kind {
	case contentScalar:
		switch node.tag {
		case tagNull, tagBool, tagInt, tagFloat, tagStr, tagTimestamp, tagBinary:
			return true
		}
	case contentSequence:
		return node.tag == tagSeq
	case contentMapping:
		return node.tag == tagMap
	}
	return false
}

func (c *valueProjectionContext) nodeKindName(node *nativeNode) string {
	switch node.content.kind {
	case contentScalar:
		return "Scalar"
	case contentSequence:
		return "Sequence"
	default:
		return "Mapping"
	}
}

// projectScalar lowers one scalar node (projection.rs).
func (c *valueProjectionContext) projectScalar(scalar *nativeScalar, supported bool,
	nodeRef document.NodeRef) (core.Value, *ValueProjectionFailure) {
	if !supported {
		return core.String(scalar.decoded), nil
	}
	invalid := func() (core.Value, *ValueProjectionFailure) {
		return nil, c.fail(ValueProjectionInvalidCanonicalScalar, nodeRef)
	}
	switch scalar.kind {
	case ScalarKindNull:
		return core.NullValue(), nil
	case ScalarKindBoolean:
		switch scalar.canonical {
		case "true":
			return core.Boolean(true), nil
		case "false":
			return core.Boolean(false), nil
		}
		return invalid()
	case ScalarKindInteger:
		number, ok := new(big.Int).SetString(scalar.canonical, 10)
		if !ok {
			return invalid()
		}
		return core.NewInteger(number), nil
	case ScalarKindFloat:
		switch scalar.canonical {
		case ".inf":
			return core.NewBinaryFloat64(0x7ff0000000000000), nil
		case "-.inf":
			return core.NewBinaryFloat64(0xfff0000000000000), nil
		case ".nan":
			return core.NewBinaryFloat64(0x7ff8000000000000), nil
		}
		coefficient, exponent, ok := parseJSONDecimal(scalar.canonical)
		if !ok {
			return invalid()
		}
		return core.NewDecimal(coefficient, exponent), nil
	case ScalarKindString:
		return core.String(scalar.canonical), nil
	case ScalarKindBinary:
		decoded, ok := decodeBase64Value(scalar.canonical)
		if !ok {
			return invalid()
		}
		return core.NewBytes(decoded), nil
	case ScalarKindTimestamp:
		value, ok := projectTimestamp(scalar.canonical)
		if !ok {
			return nil, c.fail(ValueProjectionUnrepresentableTimestamp, nodeRef)
		}
		return value, nil
	default: // Custom and Tagged
		return core.String(scalar.decoded), nil
	}
}

// projectTimestamp lowers one canonical timestamp (projection.rs).
func projectTimestamp(canonical string) (core.Value, bool) {
	year, ok := new(big.Int).SetString(canonical[:4], 10)
	if !ok {
		return nil, false
	}
	month, monthOK := parseU8(canonical[5:7])
	day, dayOK := parseU8(canonical[8:10])
	if !monthOK || !dayOK {
		return nil, false
	}
	date, err := core.NewDate(year, month, day)
	if err != nil {
		return nil, false
	}
	if len(canonical) == 10 {
		return date, true
	}
	hour, hourOK := parseU8(canonical[11:13])
	minute, minuteOK := parseU8(canonical[14:16])
	second, secondOK := parseU8(canonical[17:19])
	if !hourOK || !minuteOK || !secondOK {
		return nil, false
	}
	tail := canonical[19:]
	zoneStart := -1
	for index := 0; index < len(tail); index++ {
		character := tail[index]
		if character == 'Z' || character == '+' || character == '-' {
			zoneStart = index
			break
		}
	}
	if zoneStart < 0 {
		return nil, false
	}
	var fraction core.Decimal
	if zoneStart > 0 {
		coefficient, exponent, ok := parseJSONDecimal("0" + tail[:zoneStart])
		if !ok {
			return nil, false
		}
		fraction = core.NewDecimal(coefficient, exponent)
	}
	time, err := core.NewTime(hour, minute, second, fraction)
	if err != nil {
		return nil, false
	}
	local := core.NewLocalDateTime(date, time)
	var offset int32
	zone := tail[zoneStart:]
	if zone == "Z" {
		offset = 0
	} else {
		sign := int32(1)
		if zone[0] == '-' {
			sign = -1
		}
		hours, ok := parseI32(zone[1:3])
		if !ok {
			return nil, false
		}
		minutes, ok := parseI32(zone[4:6])
		if !ok {
			return nil, false
		}
		offset = sign * (hours*3600 + minutes*60)
	}
	value, err := core.NewOffsetDateTime(local, offset)
	if err != nil {
		return nil, false
	}
	return value, true
}

func parseI32(value string) (int32, bool) {
	if !allASCIIDigits(value) {
		return 0, false
	}
	var result int64
	for index := 0; index < len(value); index++ {
		result = result*10 + int64(value[index]-'0')
	}
	if result > 2147483647 {
		return 0, false
	}
	return int32(result), true
}

// projectSequence projects one sequence node (projection.rs).
func (c *valueProjectionContext) projectSequence(index int, node *nativeNode,
	path protocol.ValuePath, depth int) (core.Value, *ValueProjectionFailure) {
	items := make([]core.Value, 0, len(node.content.items))
	for ordinal, item := range node.content.items {
		childPath := path.Child(protocol.ValuePathSegment{Kind: "SequenceElement", Index: uint64(ordinal)})
		value, failure := c.projectNode(item.node, childPath, depth+1, item.alias)
		if failure != nil {
			return nil, failure
		}
		items = append(items, value)
		if failure := c.addOrigin(ProjectedLocation{Kind: "Value", Path: childPath},
			SourceOrigin{Snapshot: c.document.SnapshotIdentity(),
				Node: c.document.authority.NodeRef(item.identity, document.RoleYamlSequenceElement),
				Span: item.span, Relation: ProvenanceDirect}); failure != nil {
			return nil, failure
		}
	}
	return core.NewArray(items...), nil
}

// projectMapping projects one mapping node (projection.rs).
func (c *valueProjectionContext) projectMapping(index int, node *nativeNode,
	path protocol.ValuePath, depth int) (core.Value, *ValueProjectionFailure) {
	names := c.objectNames(node)
	useObject := false
	switch c.request.mapping {
	case MappingPolicyRequireObject:
		if names == nil {
			return nil, c.fail(ValueProjectionMappingNotObject, c.document.nodeRef(index))
		}
		useObject = true
	case MappingPolicyRequireEntryMapping:
		useObject = false
	default: // BestExactObjectOrEntryMapping
		useObject = names != nil
	}
	if useObject {
		return c.projectObject(node, path, depth, names)
	}
	return c.projectEntryMapping(node, path, depth)
}

// projectObject projects one unique-string-key mapping as an Object
// (projection.rs).
func (c *valueProjectionContext) projectObject(node *nativeNode, path protocol.ValuePath,
	depth int, names []string) (core.Value, *ValueProjectionFailure) {
	entries := node.content.entries
	values := make([]core.Value, 0, len(entries))
	for ordinal, entry := range entries {
		keyName := names[ordinal]
		if failure := c.visitObjectKey(entry.key, entry.keyAlias, path); failure != nil {
			return nil, failure
		}
		childPath := path.Child(protocol.ValuePathSegment{Kind: "ObjectValue", Key: keyName})
		value, failure := c.projectNode(entry.value, childPath, depth+1, entry.valueAlias)
		if failure != nil {
			return nil, failure
		}
		values = append(values, value)
		entryRef := c.document.authority.NodeRef(entry.identity, document.RoleYamlMappingEntry)
		association := protocol.NewAssociationLocation(path, uint64(ordinal),
			protocol.AssociationRoleObjectEntry)
		if failure := c.addOrigin(ProjectedLocation{Kind: "Association", Association: &association},
			SourceOrigin{Snapshot: c.document.SnapshotIdentity(), Node: entryRef,
				Span: entry.span, Relation: ProvenanceDirect}); failure != nil {
			return nil, failure
		}
		keyAssociation := protocol.NewAssociationLocation(path, uint64(ordinal),
			protocol.AssociationRoleObjectKey)
		keyRef := c.document.nodeRef(entry.key)
		keyOrigin := SourceOrigin{Snapshot: c.document.SnapshotIdentity(), Node: keyRef,
			Span: c.document.native.nodes[entry.key].span, Relation: ProvenanceDirect}
		if failure := c.addOrigin(ProjectedLocation{Kind: "Association", Association: &keyAssociation},
			keyOrigin); failure != nil {
			return nil, failure
		}
		if entry.keyAlias != nil {
			alias := &c.document.native.aliases[*entry.keyAlias]
			if failure := c.addOrigin(ProjectedLocation{Kind: "Association", Association: &keyAssociation},
				SourceOrigin{Snapshot: c.document.SnapshotIdentity(),
					Node: c.document.authority.NodeRef(alias.identity, document.RoleYamlAlias),
					Span: alias.span, Relation: ProvenanceExpanded}); failure != nil {
				return nil, failure
			}
		}
	}
	object, err := core.NewObject(entriesWithNames(names, values)...)
	if err != nil {
		// The names are prevalidated unique, so the Object constructor
		// cannot reject them.
		return nil, c.failLimit("max_value_nodes")
	}
	return object, nil
}

func entriesWithNames(names []string, values []core.Value) []core.Entry {
	entries := make([]core.Entry, 0, len(names))
	for index := range names {
		entries = append(entries, core.Entry{Key: names[index], Value: values[index]})
	}
	return entries
}

// visitObjectKey charges one object key visit with the sharing rules
// (projection.rs).
func (c *valueProjectionContext) visitObjectKey(key int, keyAlias *int,
	path protocol.ValuePath) *ValueProjectionFailure {
	keyRef := c.document.nodeRef(key)
	if c.stack[key] {
		return c.fail(ValueProjectionCycle, keyRef)
	}
	if c.seen[key] {
		if c.request.sharing == SharingPolicyReject {
			return c.fail(ValueProjectionSharing, keyRef)
		}
		source := keyRef
		if keyAlias != nil {
			source = c.document.aliasRef(*keyAlias)
		}
		event := ProjectionEvent{
			Kind: ProjectionEventSharingDuplicated, Policy: "DuplicateAcyclicSharing@1",
			Source: source, Projected: path,
			OldCategory: "SharedGraphNode", NewCategory: "DuplicatedObjectKey",
			Reversible: false, Loss: FidelityTransformed,
		}
		if failure := c.pushEvent(event); failure != nil {
			return failure
		}
	}
	c.seen[key] = true
	c.visits++
	if c.visits > c.request.limits.MaxValueNodes {
		return c.failLimit("max_value_nodes")
	}
	return nil
}

// projectEntryMapping projects one mapping as an ordered EntryMapping
// (projection.rs).
func (c *valueProjectionContext) projectEntryMapping(node *nativeNode, path protocol.ValuePath,
	depth int) (core.Value, *ValueProjectionFailure) {
	entries := node.content.entries
	pairs := make([]core.EntryMappingEntry, 0, len(entries))
	for ordinal, entry := range entries {
		keyPath := path.Child(protocol.ValuePathSegment{Kind: "EntryKey", Index: uint64(ordinal)})
		key, failure := c.projectNode(entry.key, keyPath, depth+1, entry.keyAlias)
		if failure != nil {
			return nil, failure
		}
		valuePath := path.Child(protocol.ValuePathSegment{Kind: "EntryValue", Index: uint64(ordinal)})
		value, failure := c.projectNode(entry.value, valuePath, depth+1, entry.valueAlias)
		if failure != nil {
			return nil, failure
		}
		pairs = append(pairs, core.EntryMappingEntry{Key: key, Value: value})
		association := protocol.NewAssociationLocation(path, uint64(ordinal),
			protocol.AssociationRoleEntryMappingItem)
		entryRef := c.document.authority.NodeRef(entry.identity, document.RoleYamlMappingEntry)
		if failure := c.addOrigin(ProjectedLocation{Kind: "Association", Association: &association},
			SourceOrigin{Snapshot: c.document.SnapshotIdentity(), Node: entryRef,
				Span: entry.span, Relation: ProvenanceDirect}); failure != nil {
			return nil, failure
		}
	}
	mapping, err := core.NewEntryMapping(pairs...)
	if err != nil {
		return nil, c.failLimit("max_value_nodes")
	}
	return mapping, nil
}

// objectNames returns the ordered unique string keys when every mapping
// key is a distinct `!!str` scalar (projection.rs).
func (c *valueProjectionContext) objectNames(node *nativeNode) []string {
	names := make([]string, 0, len(node.content.entries))
	seen := make(map[string]bool, len(node.content.entries))
	for _, entry := range node.content.entries {
		keyNode := &c.document.native.nodes[entry.key]
		if keyNode.content.kind != contentScalar || keyNode.tag != tagStr {
			return nil
		}
		name := keyNode.content.scalar.canonical
		if seen[name] {
			return nil
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// aliasRef resolves one alias ordinal to its occurrence identity.
func (d *Document) aliasRef(ordinal int) document.NodeRef {
	alias := &d.native.aliases[ordinal]
	return d.authority.NodeRef(alias.identity, document.RoleYamlAlias)
}
