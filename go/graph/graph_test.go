package graph

import (
	"testing"
)

// Standard YAML resolution tags, as used by the Rust graph tests.
const (
	tagStr = "tag:yaml.org,2002:str"
	tagSeq = "tag:yaml.org,2002:seq"
	tagMap = "tag:yaml.org,2002:map"
)

func mustReserve(t *testing.T, b *Builder) NodeID {
	t.Helper()
	id, err := b.ReserveNode()
	if err != nil {
		t.Fatalf("ReserveNode failed: %v", err)
	}
	return id
}

func mustBuild(t *testing.T, b *Builder) *Graph {
	t.Helper()
	g, err := b.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	return g
}

// TestScalarGraphIsImmutableAndInspectable mirrors the Rust
// scalar_graph_is_immutable_and_inspectable test
// (consema-rs/crates/consema-graph/src/lib.rs:699-715).
func TestScalarGraphIsImmutableAndInspectable(t *testing.T) {
	b := NewBuilder(DefaultLimits())
	root := mustReserve(t, b)
	if err := b.DefineScalar(root, tagStr, "catalog"); err != nil {
		t.Fatal(err)
	}
	if err := b.PushRoot(root); err != nil {
		t.Fatal(err)
	}
	g := mustBuild(t, b)
	if got := g.Roots(); len(got) != 1 || got[0] != root {
		t.Errorf("roots = %v, want [%v]", got, root)
	}
	if got := g.NodeCount(); got != 1 {
		t.Errorf("node_count = %d, want 1", got)
	}
	if got := g.EdgeCount(); got != 0 {
		t.Errorf("edge_count = %d, want 0", got)
	}
	n, ok := g.Node(root)
	if !ok {
		t.Fatalf("Node(%v) not found", root)
	}
	if n.Kind() != KindScalar {
		t.Errorf("kind = %v, want Scalar", n.Kind())
	}
	if n.Tag() != tagStr {
		t.Errorf("tag = %q, want %q", n.Tag(), tagStr)
	}
	if content, ok := n.ScalarContent(); !ok || content != "catalog" {
		t.Errorf("scalar content = (%q, %v), want (\"catalog\", true)", content, ok)
	}
	if _, ok := n.SequenceItems(); ok {
		t.Error("scalar node reports sequence items")
	}
	if _, ok := n.MappingEntries(); ok {
		t.Error("scalar node reports mapping entries")
	}
}

// TestSharingCyclesAndDuplicateArbitraryKeysAreValues mirrors the Rust
// sharing_cycles_and_duplicate_arbitrary_keys_are_values test
// (consema-rs/crates/consema-graph/src/lib.rs:717-746): a mapping whose key is a shared
// scalar, whose value is a sequence that references the mapping (cycle), and
// a second association with the same key.
func TestSharingCyclesAndDuplicateArbitraryKeysAreValues(t *testing.T) {
	b := NewBuilder(DefaultLimits())
	mapping := mustReserve(t, b)
	key := mustReserve(t, b)
	sequence := mustReserve(t, b)
	if err := b.DefineScalar(key, tagStr, "self"); err != nil {
		t.Fatal(err)
	}
	if err := b.DefineSequence(sequence, tagSeq, []NodeID{mapping, key, key}); err != nil {
		t.Fatal(err)
	}
	if err := b.DefineMapping(mapping, tagMap, []MappingEntry{
		{Key: key, Value: sequence},
		{Key: key, Value: mapping},
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.PushRoot(mapping); err != nil {
		t.Fatal(err)
	}
	g := mustBuild(t, b)
	if got := g.NodeCount(); got != 3 {
		t.Errorf("node_count = %d, want 3", got)
	}
	if got := g.EdgeCount(); got != 7 {
		t.Errorf("edge_count = %d, want 7", got)
	}
	n, ok := g.Node(mapping)
	if !ok {
		t.Fatal("mapping node not found")
	}
	entries, ok := n.MappingEntries()
	if !ok || len(entries) != 2 {
		t.Fatalf("mapping entries = (%v, %v), want 2 associations", entries, ok)
	}
	if entries[1].Value != mapping {
		t.Errorf("entries[1].value = %v, want the mapping itself (cycle)", entries[1].Value)
	}
	ids := g.Nodes()
	if len(ids) != 3 || ids[0] != mapping || ids[1] != key || ids[2] != sequence {
		t.Errorf("builder-order nodes = %v, want [mapping key sequence]", ids)
	}
}

// TestStrictEqualityIgnoresBuilderIDsButPreservesTopology mirrors the Rust
// strict_equality_ignores_builder_ids_but_preserves_topology test
// (consema-rs/crates/consema-graph/src/lib.rs:748-788): two graphs built with different
// reservation orders but identical topology are equal and hash equal; a
// graph that duplicates the shared node instead of sharing it is not.
func TestStrictEqualityIgnoresBuilderIDsButPreservesTopology(t *testing.T) {
	first := mustBuild(t, func() *Builder {
		b := NewBuilder(DefaultLimits())
		root := mustReserve(t, b)
		shared := mustReserve(t, b)
		if err := b.DefineScalar(shared, tagStr, "x"); err != nil {
			t.Fatal(err)
		}
		if err := b.DefineSequence(root, tagSeq, []NodeID{shared, shared}); err != nil {
			t.Fatal(err)
		}
		if err := b.PushRoot(root); err != nil {
			t.Fatal(err)
		}
		return b
	}())

	second := mustBuild(t, func() *Builder {
		b := NewBuilder(DefaultLimits())
		shared := mustReserve(t, b)
		root := mustReserve(t, b)
		if err := b.DefineScalar(shared, tagStr, "x"); err != nil {
			t.Fatal(err)
		}
		if err := b.DefineSequence(root, tagSeq, []NodeID{shared, shared}); err != nil {
			t.Fatal(err)
		}
		if err := b.PushRoot(root); err != nil {
			t.Fatal(err)
		}
		return b
	}())

	duplicated := mustBuild(t, func() *Builder {
		b := NewBuilder(DefaultLimits())
		root := mustReserve(t, b)
		left := mustReserve(t, b)
		right := mustReserve(t, b)
		if err := b.DefineScalar(left, tagStr, "x"); err != nil {
			t.Fatal(err)
		}
		if err := b.DefineScalar(right, tagStr, "x"); err != nil {
			t.Fatal(err)
		}
		if err := b.DefineSequence(root, tagSeq, []NodeID{left, right}); err != nil {
			t.Fatal(err)
		}
		if err := b.PushRoot(root); err != nil {
			t.Fatal(err)
		}
		return b
	}())

	if !Equal(first, second) {
		t.Error("isomorphic graphs with different builder IDs are not equal")
	}
	if Hash(first) != Hash(second) {
		t.Error("isomorphic graphs hash differently")
	}
	if Equal(first, duplicated) {
		t.Error("sharing and duplication compare equal")
	}
	if Hash(first) == Hash(duplicated) {
		t.Error("sharing and duplication hash equally")
	}
}

// TestRootAndAssociationOrderAreStrict mirrors the Rust
// root_and_association_order_are_strict test
// (consema-rs/crates/consema-graph/src/lib.rs:790-812): reversing the association order
// of a mapping is a value change.
func TestRootAndAssociationOrderAreStrict(t *testing.T) {
	makeGraph := func(reverse bool) *Graph {
		b := NewBuilder(DefaultLimits())
		mapping := mustReserve(t, b)
		a := mustReserve(t, b)
		bNode := mustReserve(t, b)
		if err := b.DefineScalar(a, tagStr, "a"); err != nil {
			t.Fatal(err)
		}
		if err := b.DefineScalar(bNode, tagStr, "b"); err != nil {
			t.Fatal(err)
		}
		var entries []MappingEntry
		if reverse {
			entries = []MappingEntry{{Key: bNode, Value: a}, {Key: a, Value: bNode}}
		} else {
			entries = []MappingEntry{{Key: a, Value: bNode}, {Key: bNode, Value: a}}
		}
		if err := b.DefineMapping(mapping, tagMap, entries); err != nil {
			t.Fatal(err)
		}
		if err := b.PushRoot(mapping); err != nil {
			t.Fatal(err)
		}
		return mustBuild(t, b)
	}
	if Equal(makeGraph(false), makeGraph(true)) {
		t.Error("reversed association order compares equal")
	}
	if Hash(makeGraph(false)) == Hash(makeGraph(true)) {
		t.Error("reversed association order hashes equally")
	}
}

// TestRootOrderIsStrict pins that root order is value semantics: the same
// nodes in a different root order produce different graphs and bytes.
func TestRootOrderIsStrict(t *testing.T) {
	makeGraph := func(swap bool) *Graph {
		b := NewBuilder(DefaultLimits())
		first := mustReserve(t, b)
		second := mustReserve(t, b)
		if err := b.DefineScalar(first, tagStr, "a"); err != nil {
			t.Fatal(err)
		}
		if err := b.DefineScalar(second, tagStr, "b"); err != nil {
			t.Fatal(err)
		}
		order := []NodeID{first, second}
		if swap {
			order = []NodeID{second, first}
		}
		for _, root := range order {
			if err := b.PushRoot(root); err != nil {
				t.Fatal(err)
			}
		}
		return mustBuild(t, b)
	}
	left, right := makeGraph(false), makeGraph(true)
	if Equal(left, right) {
		t.Error("different root order compares equal")
	}
	if Hash(left) == Hash(right) {
		t.Error("different root order hashes equally")
	}
}

// TestEmptyGraphIsAnEmptyRootStream pins the empty-graph contract (RFC 0006
// §2: "an empty graph represents an empty stream of roots, not a null
// scalar"): it builds, is equal to itself, has no roots, and hashes.
func TestEmptyGraphIsAnEmptyRootStream(t *testing.T) {
	g := mustBuild(t, NewBuilder(DefaultLimits()))
	if got := g.Roots(); len(got) != 0 {
		t.Errorf("empty graph roots = %v, want none", got)
	}
	if g.NodeCount() != 0 || g.EdgeCount() != 0 {
		t.Errorf("empty graph = (%d nodes, %d edges), want (0, 0)", g.NodeCount(), g.EdgeCount())
	}
	other := mustBuild(t, NewBuilder(DefaultLimits()))
	if !Equal(g, other) {
		t.Error("empty graphs are not equal")
	}
	if Hash(g) != Hash(other) {
		t.Error("empty graphs hash differently")
	}
}

// TestMultipleRootsShareNodes pins multi-root sharing (RFC 0006 §2: "a graph
// may have multiple roots that share nodes"): both roots reach one shared
// scalar, and the root order is preserved.
func TestMultipleRootsShareNodes(t *testing.T) {
	b := NewBuilder(DefaultLimits())
	first := mustReserve(t, b)
	second := mustReserve(t, b)
	shared := mustReserve(t, b)
	if err := b.DefineScalar(shared, tagStr, "shared"); err != nil {
		t.Fatal(err)
	}
	if err := b.DefineSequence(first, tagSeq, []NodeID{shared}); err != nil {
		t.Fatal(err)
	}
	if err := b.DefineSequence(second, tagSeq, []NodeID{shared}); err != nil {
		t.Fatal(err)
	}
	if err := b.PushRoot(first); err != nil {
		t.Fatal(err)
	}
	if err := b.PushRoot(second); err != nil {
		t.Fatal(err)
	}
	g := mustBuild(t, b)
	roots := g.Roots()
	if len(roots) != 2 || roots[0] != first || roots[1] != second {
		t.Errorf("roots = %v, want [first second]", roots)
	}
	if g.NodeCount() != 3 {
		t.Errorf("node_count = %d, want 3", g.NodeCount())
	}
	if g.EdgeCount() != 2 {
		t.Errorf("edge_count = %d, want 2", g.EdgeCount())
	}
}

// TestBuilderRejectsIncompleteUnreachableDuplicateAndInvalidTag mirrors the
// Rust builder_rejects_incomplete_unreachable_duplicate_and_invalid_tag test
// (consema-rs/crates/consema-graph/src/lib.rs:814-857).
func TestBuilderRejectsIncompleteUnreachableDuplicateAndInvalidTag(t *testing.T) {
	// Incomplete: a root without a definition.
	incomplete := NewBuilder(DefaultLimits())
	missing := mustReserve(t, incomplete)
	if err := incomplete.PushRoot(missing); err != nil {
		t.Fatal(err)
	}
	if _, err := incomplete.Build(); !IsGraphError(err, ErrGraphUndefinedNode) {
		t.Errorf("incomplete build err = %v, want UndefinedNode", err)
	}

	// Unreachable: a defined node no root reaches.
	unreachable := NewBuilder(DefaultLimits())
	root := mustReserve(t, unreachable)
	hidden := mustReserve(t, unreachable)
	if err := unreachable.DefineScalar(root, tagStr, "root"); err != nil {
		t.Fatal(err)
	}
	if err := unreachable.DefineScalar(hidden, tagStr, "hidden"); err != nil {
		t.Fatal(err)
	}
	if err := unreachable.PushRoot(root); err != nil {
		t.Fatal(err)
	}
	if _, err := unreachable.Build(); !IsGraphError(err, ErrGraphUnreachableNode) {
		t.Errorf("unreachable build err = %v, want UnreachableNode", err)
	}

	// Duplicate: one node defined twice.
	duplicate := NewBuilder(DefaultLimits())
	node := mustReserve(t, duplicate)
	if err := duplicate.DefineScalar(node, tagStr, "x"); err != nil {
		t.Fatal(err)
	}
	if err := duplicate.DefineScalar(node, tagStr, "y"); !IsGraphError(err, ErrGraphDuplicateDefinition) {
		t.Errorf("duplicate define err = %v, want DuplicateDefinition", err)
	}

	// Invalid tag: ASCII whitespace.
	invalid := NewBuilder(DefaultLimits())
	invalidNode := mustReserve(t, invalid)
	if err := invalid.DefineScalar(invalidNode, "bad tag", "x"); !IsGraphError(err, ErrGraphInvalidTag) {
		t.Errorf("invalid tag err = %v, want InvalidTag", err)
	}
	if err := invalid.DefineScalar(invalidNode, "", "x"); !IsGraphError(err, ErrGraphInvalidTag) {
		t.Errorf("empty tag err = %v, want InvalidTag", err)
	}
	if err := invalid.DefineScalar(invalidNode, "bad\x00tag", "x"); !IsGraphError(err, ErrGraphInvalidTag) {
		t.Errorf("control-char tag err = %v, want InvalidTag", err)
	}

	// Wrong graph: a foreign ID used against a different builder.
	first := NewBuilder(DefaultLimits())
	foreign := mustReserve(t, first)
	second := NewBuilder(DefaultLimits())
	if err := second.PushRoot(foreign); !IsGraphError(err, ErrGraphWrongGraph) {
		t.Errorf("foreign root err = %v, want WrongGraph", err)
	}
	if err := second.DefineScalar(foreign, tagStr, "x"); !IsGraphError(err, ErrGraphWrongGraph) {
		t.Errorf("foreign define err = %v, want WrongGraph", err)
	}

	// Unknown node: an ID within this builder's identity but outside the
	// reserved range.
	unknown := NewBuilder(DefaultLimits())
	known := mustReserve(t, unknown)
	outOfRange := NodeID{graph: known.graph, index: known.index + 100}
	if err := unknown.PushRoot(outOfRange); !IsGraphError(err, ErrGraphUnknownNode) {
		t.Errorf("unknown root err = %v, want UnknownNode", err)
	}
}

// TestLimitsFailBeforeAGraphExists mirrors the Rust
// limits_fail_before_a_graph_exists test
// (consema-rs/crates/consema-graph/src/lib.rs:859-890): node, edge, and traversal-depth
// limits fail atomically before any graph exists.
func TestLimitsFailBeforeAGraphExists(t *testing.T) {
	b := NewBuilder(Limits{
		MaxNodes:            2,
		MaxEdges:            1,
		MaxTraversalDepth:   0,
		MaxRoots:            1_000_000,
		MaxContainerEntries: 1_000_000,
		MaxTagBytes:         1_000_000,
		MaxScalarBytes:      1_000_000,
	})
	root := mustReserve(t, b)
	child := mustReserve(t, b)
	if _, err := b.ReserveNode(); !IsGraphError(err, ErrGraphResourceLimit) {
		t.Errorf("third reserve err = %v, want ResourceLimit(graph-nodes)", err)
	}
	if err := b.DefineScalar(child, tagStr, "x"); err != nil {
		t.Fatal(err)
	}
	if err := b.DefineSequence(root, tagSeq, []NodeID{child}); err != nil {
		t.Fatal(err)
	}
	if err := b.PushRoot(root); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Build(); !IsGraphError(err, ErrGraphResourceLimit) {
		t.Errorf("depth-limited build err = %v, want ResourceLimit(traversal-depth)", err)
	}

	edgeLimited := NewBuilder(Limits{
		MaxNodes:            3,
		MaxEdges:            1,
		MaxTraversalDepth:   256,
		MaxRoots:            1_000_000,
		MaxContainerEntries: 1_000_000,
		MaxTagBytes:         1_000_000,
		MaxScalarBytes:      1_000_000,
	})
	seqRoot := mustReserve(t, edgeLimited)
	first := mustReserve(t, edgeLimited)
	second := mustReserve(t, edgeLimited)
	if err := edgeLimited.DefineScalar(first, tagStr, "a"); err != nil {
		t.Fatal(err)
	}
	if err := edgeLimited.DefineScalar(second, tagStr, "b"); err != nil {
		t.Fatal(err)
	}
	if err := edgeLimited.DefineSequence(seqRoot, tagSeq, []NodeID{first, second}); !IsGraphError(err, ErrGraphResourceLimit) {
		t.Errorf("edge-limited define err = %v, want ResourceLimit(graph-edges)", err)
	}
}

// TestGraphBuildFailuresHaveStableCodes mirrors the Rust
// graph_build_failures_have_stable_v5_codes test
// (consema-rs/crates/consema-graph/src/lib.rs:892-911): construction failures carry the
// frozen "core.graph.*@1" codes.
func TestGraphBuildFailuresHaveStableCodes(t *testing.T) {
	resource := &GraphError{Kind: ErrGraphResourceLimit, Field: "graph-nodes", Observed: 2, Limit: 1}
	if got := resource.Code(); got != "core.graph.resource-limit@1" {
		t.Errorf("ResourceLimit code = %q, want core.graph.resource-limit@1", got)
	}
	if got := (&GraphError{Kind: ErrGraphSizeOverflow}).Code(); got != "core.graph.resource-limit@1" {
		t.Errorf("SizeOverflow code = %q, want core.graph.resource-limit@1", got)
	}
	if got := (&GraphError{Kind: ErrGraphInvalidTag}).Code(); got != "core.graph.invalid@1" {
		t.Errorf("InvalidTag code = %q, want core.graph.invalid@1", got)
	}
	for _, kind := range []GraphErrorKind{ErrGraphUnknownNode, ErrGraphWrongGraph, ErrGraphDuplicateDefinition, ErrGraphUndefinedNode, ErrGraphUnreachableNode} {
		if got := (&GraphError{Kind: kind}).Code(); got != "core.graph.invalid@1" {
			t.Errorf("kind %d code = %q, want core.graph.invalid@1", kind, got)
		}
	}
}

// TestNodeIdentityScoping pins node identity scoping: NodeID values are
// meaningful only inside one graph (RFC 0006 §2), and AsUint64 exposes the
// builder-local number.
func TestNodeIdentityScoping(t *testing.T) {
	b := NewBuilder(DefaultLimits())
	root := mustReserve(t, b)
	if err := b.DefineScalar(root, tagStr, "x"); err != nil {
		t.Fatal(err)
	}
	if err := b.PushRoot(root); err != nil {
		t.Fatal(err)
	}
	other := NewBuilder(DefaultLimits())
	foreign := mustReserve(t, other)

	g := mustBuild(t, b)
	if _, ok := g.Node(root); !ok {
		t.Error("own node not resolvable")
	}
	if _, ok := g.Node(foreign); ok {
		t.Error("foreign node resolved inside a different graph")
	}
	if _, ok := g.Node(NodeID{}); ok {
		t.Error("zero NodeID resolved")
	}
	if got := root.AsUint64(); got != 0 {
		t.Errorf("first reserved node AsUint64 = %d, want 0", got)
	}
	if got := foreign.AsUint64(); got != 0 {
		t.Errorf("other builder's first node AsUint64 = %d, want 0", got)
	}
}

// TestHashIsDeterministicAndCycleSafe pins that Hash is deterministic across
// calls, stable across isomorphic builder numbering, and cycle-safe
// (RFC 0006 §4: equal graphs must hash equally; no recursive expansion).
func TestHashIsDeterministicAndCycleSafe(t *testing.T) {
	b := NewBuilder(DefaultLimits())
	self := mustReserve(t, b)
	if err := b.DefineSequence(self, tagSeq, []NodeID{self}); err != nil {
		t.Fatal(err)
	}
	if err := b.PushRoot(self); err != nil {
		t.Fatal(err)
	}
	g := mustBuild(t, b)
	if Hash(g) != Hash(g) {
		t.Error("Hash is not deterministic")
	}
	if Equal(g, g) != true {
		t.Error("a graph is not equal to itself")
	}
	// Self-cycle vs. a two-node cycle through the same topology: both are
	// one-node self-references built in different orders.
	b2 := NewBuilder(DefaultLimits())
	second := mustReserve(t, b2)
	if err := b2.DefineSequence(second, tagSeq, []NodeID{second}); err != nil {
		t.Fatal(err)
	}
	if err := b2.PushRoot(second); err != nil {
		t.Fatal(err)
	}
	g2 := mustBuild(t, b2)
	if !Equal(g, g2) || Hash(g) != Hash(g2) {
		t.Error("identical self-cycles built independently differ")
	}
}
