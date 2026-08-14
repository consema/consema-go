package graph

import (
	"hash/fnv"
)

// layout is the canonical numbering of one graph (RFC 0006 §4): order lists
// original node indices in deterministic depth-first pre-order, and
// canonicalIDs maps each original index to its canonical ID.
type layout struct {
	order        []int
	canonicalIDs []int
}

// layout computes the canonical numbering of a completed graph. Completed
// graphs always traverse cleanly (Build validated reachability and depth),
// so an error here is an internal invariant violation.
func (g *Graph) layout() layout {
	order, canonicalIDs, err := canonicalOrder(g.nodes, g.roots, -1)
	if err != nil {
		panic("graph: completed graph traversal failed")
	}
	return layout{order: order, canonicalIDs: canonicalIDs}
}

// canonicalOrder assigns canonical IDs by deterministic depth-first
// pre-order (RFC 0006 §4): visit roots in root order; when a node is first
// encountered assign the next ID; for a sequence visit items in order; for a
// mapping visit each association in order, key before value; an already
// assigned node is a reference and is not traversed again. maxDepth < 0
// disables the first-visit depth limit. It returns the original indices in
// visit order and the canonical ID of every original index.
func canonicalOrder(nodes []node, roots []NodeID, maxDepth int) ([]int, []int, error) {
	order := make([]int, 0, len(nodes))
	canonicalIDs := make([]int, len(nodes))
	visited := make([]bool, len(nodes))
	stack := make([]struct{ index, depth int }, 0, len(nodes))
	// Push in reverse so the first root pops first (the Rust
	// canonical_order, consema-rs/consema-graph/src/lib.rs).
	for i := len(roots) - 1; i >= 0; i-- {
		stack = append(stack, struct{ index, depth int }{roots[i].index, 0})
	}
	next := 0
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[top.index] {
			continue
		}
		if maxDepth >= 0 && top.depth > maxDepth {
			return nil, nil, &GraphError{
				Kind:     ErrGraphResourceLimit,
				Field:    "traversal-depth",
				Observed: top.depth,
				Limit:    maxDepth,
			}
		}
		visited[top.index] = true
		order = append(order, top.index)
		canonicalIDs[top.index] = next
		next++
		n := &nodes[top.index]
		childDepth := top.depth + 1
		switch n.kind {
		case KindSequence:
			// Push reversed so items pop in stored order (the Rust
			// outgoing_reverse, lib.rs).
			for i := len(n.items) - 1; i >= 0; i-- {
				stack = append(stack, struct{ index, depth int }{n.items[i].index, childDepth})
			}
		case KindMapping:
			// Push value then key per association, all reversed, so
			// associations pop in stored order with key before value.
			for i := len(n.entries) - 1; i >= 0; i-- {
				stack = append(stack, struct{ index, depth int }{n.entries[i].Value.index, childDepth})
				stack = append(stack, struct{ index, depth int }{n.entries[i].Key.index, childDepth})
			}
		}
	}
	return order, canonicalIDs, nil
}

// Equal reports strict PortableGraph equality (RFC 0006 §4): two graphs are
// equal when there is a root-preserving ordered graph isomorphism preserving
// root order, node kind, exact resolved tag, exact canonical scalar content,
// sequence edge order, mapping association order (including duplicates),
// key/value edge roles, and shared-reference and cycle topology. Builder
// numbering is not semantic: graphs built with different local IDs compare
// equal when their canonical numbering and content match (RFC 0006 §4).
//
// Equal is total: it never panics, it never compares pointer addresses, and
// it never recurses through edges, so shared and cyclic graphs are safe
// (RFC 0006 §4: "Consema computes this without recursive expansion").
// Equal(nil, nil) is true; Equal(nil, x) is false for any non-nil x.
func Equal(a, b *Graph) bool {
	if a == nil || b == nil {
		return a == b
	}
	if len(a.roots) != len(b.roots) || len(a.nodes) != len(b.nodes) || a.edges != b.edges {
		return false
	}
	left := a.layout()
	right := b.layout()
	for i := range a.roots {
		if left.canonicalIDs[a.roots[i].index] != right.canonicalIDs[b.roots[i].index] {
			return false
		}
	}
	for i := range left.order {
		if !canonicalNodeEqual(
			&a.nodes[left.order[i]], left.canonicalIDs,
			&b.nodes[right.order[i]], right.canonicalIDs,
		) {
			return false
		}
	}
	return true
}

// canonicalNodeEqual compares two nodes under their canonical ID mappings
// (the Rust canonical_node_eq, consema-rs/consema-graph/src/lib.rs).
func canonicalNodeEqual(left *node, leftIDs []int, right *node, rightIDs []int) bool {
	if left.kind != right.kind || left.tag != right.tag {
		return false
	}
	switch left.kind {
	case KindScalar:
		return left.scalar == right.scalar
	case KindSequence:
		if len(left.items) != len(right.items) {
			return false
		}
		for i := range left.items {
			if leftIDs[left.items[i].index] != rightIDs[right.items[i].index] {
				return false
			}
		}
		return true
	case KindMapping:
		if len(left.entries) != len(right.entries) {
			return false
		}
		for i := range left.entries {
			if leftIDs[left.entries[i].Key.index] != rightIDs[right.entries[i].Key.index] ||
				leftIDs[left.entries[i].Value.index] != rightIDs[right.entries[i].Value.index] {
				return false
			}
		}
		return true
	}
	return false
}

// Hash returns a deterministic 64-bit hash consistent with Equal (RFC 0006
// §4): equal graphs always hash equal. It is defined as FNV-1a over the
// canonical PGCE/1 encoding of the graph, so Equal(a, b) holds exactly when
// the encoded bytes of a and b are identical (see EncodePGCE); the hash is
// therefore identity-order-sensitive and cycle-safe. Hash(nil) is 0.
func Hash(g *Graph) uint64 {
	bytes, err := EncodePGCE(g)
	if err != nil {
		return 0
	}
	hasher := fnv.New64a()
	_, _ = hasher.Write(bytes)
	return hasher.Sum64()
}
