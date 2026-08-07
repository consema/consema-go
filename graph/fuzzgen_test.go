package graph

// ---------------------------------------------------------------------------
// Deterministic PortableGraph generator for the round-trip fuzz target
// (milestone 0.14.0 G0.5; docs/go-implementation-plan.md §2.1). It lives in
// package graph (test files) because it constructs graphs: any package that
// imports graph would create an import cycle with graph's own fuzz tests
// (the same reason the Rust fuzz drivers live in the per-crate fuzz
// directories, docs/fuzz-evidence-0.13.0.md §2).
//
// The same (seed, blob) input always produces the same graph, so a fuzz
// input fully determines its generated graph. Sharing and cycles are
// allowed (RFC 0006 §2), every node is reachable from the roots, and
// container sizes and depth stay far inside the defaults so every generated
// graph encodes under DefaultPGCELimits.
// ---------------------------------------------------------------------------

import (
	"hash/fnv"
	"math/rand"
	"strings"
)

// genGraph is the deterministic graph generator state.
type genGraph struct {
	r *rand.Rand
}

// newGenGraph derives the RNG state from the seed and the blob's FNV-1a
// hash.
func newGenGraph(seed uint64, blob []byte) *genGraph {
	h := fnv.New64a()
	h.Write(blob)
	return &genGraph{r: rand.New(rand.NewSource(int64(seed ^ h.Sum64())))}
}

// GenGraph deterministically generates one valid PortableGraph (RFC 0006)
// under DefaultLimits: 0..6 nodes, every node reachable from the roots,
// sharing and cycles allowed. It is exported within the test package for
// use by FuzzPGCEEncodeDecode; it is not SDK API.
func GenGraph(seed uint64, blob []byte) *Graph {
	g := newGenGraph(seed, blob)
	builder := NewBuilder(DefaultLimits())
	count := g.r.Intn(7)
	ids := make([]NodeID, count)
	for i := range ids {
		id, err := builder.ReserveNode()
		if err != nil {
			panic("graph: ReserveNode failed under default limits")
		}
		ids[i] = id
	}
	if count == 0 {
		empty, err := builder.Build()
		if err != nil {
			panic("graph: empty graph build failed")
		}
		return empty
	}
	// Node 0 is the connector: for count > 1 it is a sequence referencing
	// every node (including itself), guaranteeing reachability from the
	// root.
	if count > 1 {
		items := make([]NodeID, count)
		copy(items, ids)
		if err := builder.DefineSequence(ids[0], g.tag(), items); err != nil {
			panic("graph: DefineSequence failed under default limits")
		}
	}
	for i := 1; i < count; i++ {
		g.defineRandom(builder, ids, ids[i])
	}
	if count == 1 {
		// A single-node graph: any kind is valid, it is its own root.
		g.defineRandom(builder, ids, ids[0])
	}
	if err := builder.PushRoot(ids[0]); err != nil {
		panic("graph: PushRoot failed under default limits")
	}
	// Occasionally add a second root for multi-root variety.
	if count > 1 && g.r.Intn(4) == 0 {
		_ = builder.PushRoot(ids[1+g.r.Intn(count-1)])
	}
	built, err := builder.Build()
	if err != nil {
		panic("graph: build failed for a generator-produced graph")
	}
	return built
}

// defineRandom defines one node with a random kind. Containers reference
// random ids (any reserved id: sharing and cycles are valid graph facts).
func (g *genGraph) defineRandom(builder *Builder, ids []NodeID, id NodeID) {
	switch g.r.Intn(4) {
	case 0, 1:
		if err := builder.DefineScalar(id, g.tag(), g.utf8String(16)); err != nil {
			panic("graph: DefineScalar failed under default limits")
		}
	case 2:
		items := make([]NodeID, g.r.Intn(4))
		for i := range items {
			items[i] = ids[g.r.Intn(len(ids))]
		}
		if err := builder.DefineSequence(id, g.tag(), items); err != nil {
			panic("graph: DefineSequence failed under default limits")
		}
	default:
		entries := make([]MappingEntry, g.r.Intn(4))
		for i := range entries {
			entries[i] = MappingEntry{
				Key:   ids[g.r.Intn(len(ids))],
				Value: ids[g.r.Intn(len(ids))],
			}
		}
		if err := builder.DefineMapping(id, g.tag(), entries); err != nil {
			panic("graph: DefineMapping failed under default limits")
		}
	}
}

// tag returns one valid non-empty graph tag (RFC 0006 §2: no ASCII control,
// whitespace, or empty tags).
func (g *genGraph) tag() string {
	fixed := []string{
		"tag:yaml.org,2002:str",
		"tag:yaml.org,2002:seq",
		"tag:yaml.org,2002:map",
		"consema:test",
		"x",
	}
	tag := fixed[g.r.Intn(len(fixed))]
	if g.r.Intn(4) == 0 {
		tag += "-" + strings.Repeat("k", g.r.Intn(4))
	}
	return tag
}

// utf8String generates a valid UTF-8 scalar content string.
func (g *genGraph) utf8String(max int) string {
	var builder strings.Builder
	for count := g.r.Intn(max + 1); count > 0; count-- {
		builder.WriteRune(g.rune())
	}
	return builder.String()
}

// rune generates one valid Unicode scalar value (never a surrogate).
func (g *genGraph) rune() rune {
	switch g.r.Intn(4) {
	case 0:
		return rune(0x20 + g.r.Intn(0x5f)) // ASCII printable
	case 1:
		return rune(0x80 + g.r.Intn(0x780)) // two-byte UTF-8
	case 2:
		return rune(0x1000 + g.r.Intn(0xf000)) // three-byte UTF-8
	default:
		return '中'
	}
}
