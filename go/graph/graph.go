// Package graph implements the language-neutral Consema PortableGraph model
// for Go (RFC 0006; RFC 0016 §4.1:144-146): immutable rooted, directed,
// ordered, tagged graphs with graph-local node identity, sharing and cycles,
// strict equality and deterministic hashing under canonical node numbering,
// and the PGCE/1 canonical byte codec.
//
// The model mirrors the Rust reference crate (consema-rs/consema-graph): a graph
// is independent from the closed fifteen-kind PortableValue model — its
// scalar nodes carry resolved tag identifiers and canonical content strings
// (RFC 0006 §2), the graph layer introduces no value kinds of its own, and
// the package imports nothing else in the module
// (https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §0.2: core and graph are independently
// implementable). The PGCE/1 wire format is reimplemented from
// consema-rs/consema-graph/src/pgce.rs, and its canonical bytes are pinned by
// golden tests in this package (consema-rs/consema-graph/src/pgce.rs:664-686).
//
// The package is standard-library only (https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §1.3).
package graph

import (
	"fmt"
	"sync/atomic"
	"unicode/utf8"
)

// nextGraphIdentity assigns one unique identity per Builder (the Rust
// NEXT_GRAPH counter, consema-rs/consema-graph/src/lib.rs:22). The zero value
// never identifies a builder: identities start at 1, so a zero NodeID is
// invalid for every builder.
var nextGraphIdentity atomic.Uint64

// NodeKind is the stable node kind in PortableGraph@1 (RFC 0006 §2).
type NodeKind uint8

// The three stable node kinds, in the RFC 0006 §2 order.
const (
	// KindScalar is a tagged canonical scalar content node.
	KindScalar NodeKind = iota
	// KindSequence is an ordered node-reference node.
	KindSequence
	// KindMapping is an ordered key/value graph-association node.
	KindMapping
)

// String returns the kind name: "Scalar", "Sequence", or "Mapping".
func (k NodeKind) String() string {
	switch k {
	case KindScalar:
		return "Scalar"
	case KindSequence:
		return "Sequence"
	case KindMapping:
		return "Mapping"
	}
	return fmt.Sprintf("NodeKind(%d)", uint8(k))
}

// NodeID is a graph-local node identity assigned by a Builder (RFC 0006 §2:
// "GraphNodeId is meaningful only inside one immutable graph"). IDs are valid
// only for the graph built by that builder, and their numeric values are not
// part of strict graph equality or canonical encoding (RFC 0006 §4), so IDs
// must never be compared across builders or graphs. The zero value is
// invalid.
type NodeID struct {
	graph uint64
	index int
}

// AsUint64 returns the builder-local numeric representation (the Rust
// GraphNodeId::as_u64, consema-rs/consema-graph/src/lib.rs:40-43).
func (id NodeID) AsUint64() uint64 { return uint64(id.index) }

// MappingEntry is one ordered mapping association with arbitrary graph-node
// key and value (RFC 0006 §2: "mapping keys are arbitrary graph nodes").
// Duplicate associations and association order are value semantics.
type MappingEntry struct {
	// Key is the arbitrary key node.
	Key NodeID
	// Value is the value node.
	Value NodeID
}

// node is one immutable tagged graph node (the Rust GraphNode,
// consema-rs/consema-graph/src/lib.rs:94-157). The kind discriminates which
// content field is meaningful.
type node struct {
	tag     string
	kind    NodeKind
	scalar  string
	items   []NodeID
	entries []MappingEntry
}

// Node is one immutable tagged graph node as returned by Graph.Node. A
// scalar node carries the producer's canonical content for its tag; the
// graph layer treats it as an exact UTF-8 string (RFC 0006 §2).
type Node struct {
	tag     string
	kind    NodeKind
	scalar  string
	items   []NodeID
	entries []MappingEntry
}

// Tag returns the resolved non-empty tag identifier.
func (n *Node) Tag() string { return n.tag }

// Kind returns the stable node kind.
func (n *Node) Kind() NodeKind { return n.kind }

// ScalarContent returns the canonical scalar content when this is a scalar
// node.
func (n *Node) ScalarContent() (string, bool) {
	if n.kind != KindScalar {
		return "", false
	}
	return n.scalar, true
}

// SequenceItems returns the ordered item references when this is a sequence
// node.
func (n *Node) SequenceItems() ([]NodeID, bool) {
	if n.kind != KindSequence {
		return nil, false
	}
	return append([]NodeID(nil), n.items...), true
}

// MappingEntries returns the ordered associations when this is a mapping
// node.
func (n *Node) MappingEntries() ([]MappingEntry, bool) {
	if n.kind != KindMapping {
		return nil, false
	}
	return append([]MappingEntry(nil), n.entries...), true
}

// Limits are the resource bounds for graph construction and traversal (RFC
// 0006 §6; the Rust GraphLimits, consema-rs/consema-graph/src/lib.rs:159-190).
// The zero value rejects every reservation and root; use DefaultLimits.
type Limits struct {
	// MaxRoots is the maximum ordered roots.
	MaxRoots int
	// MaxNodes is the maximum graph nodes.
	MaxNodes int
	// MaxEdges is the maximum sequence-item plus mapping key/value edges.
	MaxEdges int
	// MaxContainerEntries is the maximum items or associations in one
	// container.
	MaxContainerEntries int
	// MaxTagBytes is the maximum UTF-8 bytes in one tag identifier.
	MaxTagBytes int
	// MaxScalarBytes is the maximum UTF-8 bytes in one scalar's canonical
	// content.
	MaxScalarBytes int
	// MaxTraversalDepth is the maximum first-visit traversal depth (the
	// active traversal path, not alias expansion count; RFC 0006 §6).
	MaxTraversalDepth int
}

// DefaultLimits returns the frozen defaults, mirroring the Rust GraphLimits
// default (consema-rs/consema-graph/src/lib.rs:178-190).
func DefaultLimits() Limits {
	return Limits{
		MaxRoots:            1_000_000,
		MaxNodes:            1_000_000,
		MaxEdges:            2_000_000,
		MaxContainerEntries: 1_000_000,
		MaxTagBytes:         1 << 20,
		MaxScalarBytes:      64 << 20,
		MaxTraversalDepth:   256,
	}
}

// Graph is an immutable rooted, directed, ordered, tagged graph value
// (PortableGraph; RFC 0006 §2). One graph contains zero or more ordered
// roots and one closed set of reachable nodes; an empty graph represents an
// empty stream of roots, not a null scalar (RFC 0006 §2). Completed graphs
// are logically immutable and safe for concurrent reads.
type Graph struct {
	identity uint64
	roots    []NodeID
	nodes    []node
	edges    int
}

// PortableGraph is the RFC 0006 contract name of the immutable graph value;
// it aliases Graph so the API freezes the same vocabulary across languages
// (RFC 0006 §2; the TS `export type PortableGraph = Graph` and Kotlin
// `typealias PortableGraph = Graph` counterparts).
type PortableGraph = Graph

// Roots returns a copy of the ordered roots. An empty slice represents an
// empty root stream (RFC 0006 §2).
func (g *Graph) Roots() []NodeID {
	return append([]NodeID(nil), g.roots...)
}

// NodeCount returns the number of reachable graph nodes.
func (g *Graph) NodeCount() int { return len(g.nodes) }

// EdgeCount returns the number of sequence-item plus mapping key/value
// edges.
func (g *Graph) EdgeCount() int { return g.edges }

// Node resolves one graph-local node ID. It returns false when the ID
// belongs to a different builder or completed graph, or is out of range.
// The returned Node is a snapshot copy; mutating it never affects the graph.
func (g *Graph) Node(id NodeID) (*Node, bool) {
	if id.graph != g.identity {
		return nil, false
	}
	if id.index < 0 || id.index >= len(g.nodes) {
		return nil, false
	}
	n := g.nodes[id.index]
	return &Node{
		tag:     n.tag,
		kind:    n.kind,
		scalar:  n.scalar,
		items:   append([]NodeID(nil), n.items...),
		entries: append([]MappingEntry(nil), n.entries...),
	}, true
}

// Nodes returns the builder-local IDs in builder order. Numeric ID order is
// not value semantics (the Rust PortableGraph::nodes,
// consema-rs/consema-graph/src/lib.rs:507-519).
func (g *Graph) Nodes() []NodeID {
	ids := make([]NodeID, len(g.nodes))
	for i := range g.nodes {
		ids[i] = NodeID{graph: g.identity, index: i}
	}
	return ids
}

// Builder is the mutable reservation/definition lifecycle for one immutable
// Graph (RFC 0006 §3): reserve node identities, define each exactly once as
// a scalar, sequence, or mapping, add ordered roots, then Build validates
// all references, reachability, and traversal depth before freezing the
// graph. A reserved identity cannot be inspected as a completed node, and
// build failure returns no partial graph (RFC 0006 §3).
type Builder struct {
	identity uint64
	nodes    []*node // nil entries are reserved but undefined
	roots    []NodeID
	edges    int
	limits   Limits
}

// NewBuilder creates an empty builder with explicit resource limits.
func NewBuilder(limits Limits) *Builder {
	return &Builder{identity: nextGraphIdentity.Add(1), limits: limits}
}

// ReserveNode reserves one graph-local identity for later exact definition.
func (b *Builder) ReserveNode() (NodeID, error) {
	observed := len(b.nodes) + 1
	if err := b.checkLimit("graph-nodes", observed, b.limits.MaxNodes); err != nil {
		return NodeID{}, err
	}
	id := NodeID{graph: b.identity, index: len(b.nodes)}
	b.nodes = append(b.nodes, nil)
	return id, nil
}

// PushRoot appends one ordered graph root.
func (b *Builder) PushRoot(id NodeID) error {
	if _, err := b.requireReserved(id); err != nil {
		return err
	}
	observed := len(b.roots) + 1
	if err := b.checkLimit("graph-roots", observed, b.limits.MaxRoots); err != nil {
		return err
	}
	b.roots = append(b.roots, id)
	return nil
}

// DefineScalar defines one reserved scalar node exactly once, with a
// resolved tag and the producer's canonical content (RFC 0006 §2). Both the
// tag and the canonical content must be valid UTF-8 (the Rust Arc<str>
// invariant, consema-rs/consema-graph/src/lib.rs:94-157: such a graph cannot
// even be constructed there); invalid UTF-8 returns a *GraphError with
// ErrGraphInvalidUTF8.
func (b *Builder) DefineScalar(id NodeID, tag, canonicalContent string) error {
	if err := b.validateTag(tag); err != nil {
		return err
	}
	if !utf8.ValidString(canonicalContent) {
		return &GraphError{Kind: ErrGraphInvalidUTF8}
	}
	if err := b.checkLimit("scalar-bytes", len(canonicalContent), b.limits.MaxScalarBytes); err != nil {
		return err
	}
	return b.define(id, &node{tag: tag, kind: KindScalar, scalar: canonicalContent}, 0)
}

// DefineSequence defines one reserved ordered sequence node exactly once.
// The items are copied; the caller's slice is never retained.
func (b *Builder) DefineSequence(id NodeID, tag string, items []NodeID) error {
	if err := b.validateTag(tag); err != nil {
		return err
	}
	if err := b.checkLimit("container-entries", len(items), b.limits.MaxContainerEntries); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := b.requireReserved(item); err != nil {
			return err
		}
	}
	return b.define(id, &node{tag: tag, kind: KindSequence, items: append([]NodeID(nil), items...)}, len(items))
}

// DefineMapping defines one reserved ordered mapping node exactly once. The
// entries are copied; the caller's slice is never retained.
func (b *Builder) DefineMapping(id NodeID, tag string, entries []MappingEntry) error {
	if err := b.validateTag(tag); err != nil {
		return err
	}
	if err := b.checkLimit("container-entries", len(entries), b.limits.MaxContainerEntries); err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := b.requireReserved(entry.Key); err != nil {
			return err
		}
		if _, err := b.requireReserved(entry.Value); err != nil {
			return err
		}
	}
	// A mapping association contributes a key and a value edge; the
	// container limit above bounds this product, so it cannot overflow.
	return b.define(id, &node{tag: tag, kind: KindMapping, entries: append([]MappingEntry(nil), entries...)}, len(entries)*2)
}

// define stores one node after checking duplicate definition and the edge
// limit (the Rust GraphBuilder::define, consema-rs/consema-graph/src/lib.rs:
// 383-401).
func (b *Builder) define(id NodeID, n *node, newEdges int) error {
	index, err := b.requireReserved(id)
	if err != nil {
		return err
	}
	if b.nodes[index] != nil {
		return &GraphError{Kind: ErrGraphDuplicateDefinition, ID: id}
	}
	b.edges += newEdges
	if err := b.checkLimit("graph-edges", b.edges, b.limits.MaxEdges); err != nil {
		return err
	}
	b.nodes[index] = n
	return nil
}

// requireReserved validates that id belongs to this builder and is within
// the reserved range (the Rust GraphBuilder::require_reserved,
// consema-rs/consema-graph/src/lib.rs:403-410).
func (b *Builder) requireReserved(id NodeID) (int, error) {
	if id.graph != b.identity {
		return 0, &GraphError{Kind: ErrGraphWrongGraph, ID: id}
	}
	if id.index < 0 || id.index >= len(b.nodes) {
		return 0, &GraphError{Kind: ErrGraphUnknownNode, ID: id}
	}
	return id.index, nil
}

// validateTag rejects empty tags, tags containing ASCII control or
// whitespace, and tags that are not valid UTF-8 (the Rust validate_tag,
// consema-rs/consema-graph/src/lib.rs:447-456 plus the Arc<str> invariant; RFC
// 0006 §2). Invalid UTF-8 returns ErrGraphInvalidUTF8, everything else
// ErrGraphInvalidTag.
func (b *Builder) validateTag(tag string) error {
	if tag == "" || hasInvalidTagChar(tag) {
		return &GraphError{Kind: ErrGraphInvalidTag}
	}
	if !utf8.ValidString(tag) {
		return &GraphError{Kind: ErrGraphInvalidUTF8}
	}
	return b.checkLimit("tag-bytes", len(tag), b.limits.MaxTagBytes)
}

// hasInvalidTagChar reports one ASCII control or whitespace character (the
// Rust is_ascii_control / is_ascii_whitespace predicates,
// consema-rs/consema-graph/src/lib.rs:450-451).
func hasInvalidTagChar(tag string) bool {
	for _, r := range tag {
		if r < 0x20 || r == 0x7f {
			return true
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\f' || r == '\r' {
			return true
		}
	}
	return false
}

// checkLimit reports ErrGraphResourceLimit when observed exceeds limit (the Rust
// check_limit, consema-rs/consema-graph/src/lib.rs:458-468).
func (b *Builder) checkLimit(name string, observed, limit int) error {
	if observed > limit {
		return &GraphError{Kind: ErrGraphResourceLimit, Field: name, Observed: observed, Limit: limit}
	}
	return nil
}

// Build validates definitions, reachability, and traversal depth, then
// freezes the graph. It returns no partial graph on failure (RFC 0006 §3).
func (b *Builder) Build() (*Graph, error) {
	nodes := make([]node, len(b.nodes))
	for index, n := range b.nodes {
		if n == nil {
			return nil, &GraphError{
				Kind: ErrGraphUndefinedNode,
				ID:   NodeID{graph: b.identity, index: index},
			}
		}
		nodes[index] = *n
	}
	order, _, err := canonicalOrder(nodes, b.roots, b.limits.MaxTraversalDepth)
	if err != nil {
		return nil, err
	}
	if len(order) != len(nodes) {
		reachable := make([]bool, len(nodes))
		for _, index := range order {
			reachable[index] = true
		}
		for index, ok := range reachable {
			if !ok {
				return nil, &GraphError{
					Kind: ErrGraphUnreachableNode,
					ID:   NodeID{graph: b.identity, index: index},
				}
			}
		}
		panic("graph: different counts imply an unreachable node")
	}
	return &Graph{
		identity: b.identity,
		roots:    append([]NodeID(nil), b.roots...),
		nodes:    nodes,
		edges:    b.edges,
	}, nil
}
