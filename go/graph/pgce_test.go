package graph

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// ---------------------------------------------------------------------------
// Golden byte vectors.
//
// The vectors below are transcribed byte-for-byte from the Rust PGCE/1
// encoder's in-code pins:
//
//   - consema-rs/consema-graph/src/pgce.rs
//     (scalar_byte_vector_is_frozen)
//   - consema-rs/consema-graph/src/pgce.rs
//     (empty_graph_byte_vector_is_frozen)
//
// They are also the shared conformance vector expectations
// (conformance/vectors/portable-graph-v1.json: pgce.empty-vector and
// pgce.scalar-vector).
//
// The Rust side is the authority for the bytes (roadmap §16.1 hard gate:
// "Rust 与 Go 的 PVCE/PGCE bytes 完全一致"); any change to these constants
// must land in both languages together.
// ---------------------------------------------------------------------------

// TestPGCEGoldenBytes pins the two Rust frozen byte vectors and decodes them
// back.
func TestPGCEGoldenBytes(t *testing.T) {
	// encode(empty graph) == hex 50474345010000 (pgce.rs)
	empty := mustBuild(t, NewBuilder(DefaultLimits()))
	emptyBytes, err := EncodePGCE(empty)
	if err != nil {
		t.Fatal(err)
	}
	wantEmpty, err := hex.DecodeString("50474345010000")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(emptyBytes, wantEmpty) {
		t.Errorf("EncodePGCE(empty) = %x, want %x (Rust pin pgce.rs)", emptyBytes, wantEmpty)
	}

	// encode(scalar "x" tagged tag:yaml.org,2002:str) ==
	// hex 504743450101010020157461673a79616d6c2e6f72672c323030323a7374720178
	// (pgce.rs)
	b := NewBuilder(DefaultLimits())
	root := mustReserve(t, b)
	if err := b.DefineScalar(root, tagStr, "x"); err != nil {
		t.Fatal(err)
	}
	if err := b.PushRoot(root); err != nil {
		t.Fatal(err)
	}
	scalar := mustBuild(t, b)
	scalarBytes, err := EncodePGCE(scalar)
	if err != nil {
		t.Fatal(err)
	}
	wantScalar, err := hex.DecodeString("504743450101010020157461673a79616d6c2e6f72672c323030323a7374720178")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(scalarBytes, wantScalar) {
		t.Errorf("EncodePGCE(scalar x) = %x, want %x (Rust pin pgce.rs)", scalarBytes, wantScalar)
	}

	// The golden bytes decode back to Equal graphs, byte-stably.
	limits := DefaultPGCELimits()
	if got, err := DecodePGCE(wantEmpty, limits); err != nil || !Equal(got, empty) {
		t.Errorf("DecodePGCE(golden empty) = (%v, %v), want (empty, nil)", got, err)
	}
	gotScalar, err := DecodePGCE(wantScalar, limits)
	if err != nil || !Equal(gotScalar, scalar) {
		t.Errorf("DecodePGCE(golden scalar) = (%v, %v), want equal scalar graph", gotScalar, err)
	}
	reEncoded, err := EncodePGCE(gotScalar)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reEncoded, wantScalar) {
		t.Errorf("re-encode of decoded scalar = %x, want %x", reEncoded, wantScalar)
	}
}

// TestConformanceVectorRejections pins the negative expectations of the
// shared portable-graph-v1 vectors (conformance/vectors/portable-graph-v1.
// json): every non-canonical stream fails with no partial graph, and the
// stream-bytes limit fails atomically.
func TestConformanceVectorRejections(t *testing.T) {
	limits := DefaultPGCELimits()

	// pgce.reject-nonminimal-varint: hex 5047434581000000.
	nonminimal, _ := hex.DecodeString("5047434581000000")
	if _, err := DecodePGCE(nonminimal, limits); !IsPGCEError(err, ErrNonMinimalVarint) {
		t.Errorf("nonminimal varint err = %v, want NonMinimalVarint", err)
	}

	// pgce.reject-noncanonical-node-order: the root is node 1, violating
	// canonical first discovery.
	noncanonical, _ := hex.DecodeString("504743450101020120157461673a79616d6c2e6f72672c323030323a737472017840157461673a79616d6c2e6f72672c323030323a7365710100")
	if _, err := DecodePGCE(noncanonical, limits); !IsPGCEError(err, ErrNonCanonicalNodeOrder) {
		t.Errorf("noncanonical node order err = %v, want NonCanonicalNodeOrder", err)
	}

	// resource.pgce-stream-limit: the scalar graph is 33 bytes; a 7-byte
	// limit fails before any work, on both decode and bounded encode.
	b := NewBuilder(DefaultLimits())
	root := mustReserve(t, b)
	if err := b.DefineScalar(root, tagStr, "x"); err != nil {
		t.Fatal(err)
	}
	if err := b.PushRoot(root); err != nil {
		t.Fatal(err)
	}
	g := mustBuild(t, b)
	encoded := mustEncode(t, g)
	if len(encoded) != 33 {
		t.Fatalf("scalar stream = %d bytes, want 33", len(encoded))
	}
	limited := DefaultPGCELimits()
	limited.MaxStreamBytes = 7
	if _, err := DecodePGCE(encoded, limited); !IsPGCEError(err, ErrResourceLimit) {
		t.Errorf("stream-limited decode err = %v, want ResourceLimit", err)
	}
	if _, err := EncodePGCEBounded(g, limited); !IsPGCEError(err, ErrResourceLimit) {
		t.Errorf("stream-limited encode err = %v, want ResourceLimit", err)
	}
}

// TestIsomorphicBuilderNumberingHasIdenticalPGCE mirrors the Rust
// isomorphic_builder_numbering_has_identical_pgce test
// (consema-rs/consema-graph/src/pgce.rs): equal graphs built with
// different local IDs encode to identical bytes.
func TestIsomorphicBuilderNumberingHasIdenticalPGCE(t *testing.T) {
	build := func(sharedFirst bool) *Graph {
		b := NewBuilder(DefaultLimits())
		var root, shared NodeID
		if sharedFirst {
			shared = mustReserve(t, b)
			root = mustReserve(t, b)
		} else {
			root = mustReserve(t, b)
			shared = mustReserve(t, b)
		}
		if err := b.DefineScalar(shared, tagStr, "x"); err != nil {
			t.Fatal(err)
		}
		if err := b.DefineSequence(root, tagSeq, []NodeID{shared, shared}); err != nil {
			t.Fatal(err)
		}
		if err := b.PushRoot(root); err != nil {
			t.Fatal(err)
		}
		return mustBuild(t, b)
	}
	first, second := build(false), build(true)
	if !Equal(first, second) {
		t.Fatal("isomorphic graphs are not equal")
	}
	firstBytes := mustEncode(t, first)
	secondBytes := mustEncode(t, second)
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Errorf("isomorphic graphs encode differently:\n%x\n%x", firstBytes, secondBytes)
	}
}

// TestSharedCyclesAndArbitraryMappingKeysRoundTrip mirrors the Rust
// shared_cycles_and_arbitrary_mapping_keys_round_trip test
// (consema-rs/consema-graph/src/pgce.rs): a graph with sharing, a
// self-cycle through a mapping value, and a duplicate arbitrary key
// round-trips byte-stably.
func TestSharedCyclesAndArbitraryMappingKeysRoundTrip(t *testing.T) {
	b := NewBuilder(DefaultLimits())
	mapping := mustReserve(t, b)
	key := mustReserve(t, b)
	sequence := mustReserve(t, b)
	if err := b.DefineScalar(key, tagStr, "k"); err != nil {
		t.Fatal(err)
	}
	if err := b.DefineSequence(sequence, tagSeq, []NodeID{mapping, key}); err != nil {
		t.Fatal(err)
	}
	if err := b.DefineMapping(mapping, tagMap, []MappingEntry{
		{Key: sequence, Value: key},
		{Key: key, Value: mapping},
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.PushRoot(mapping); err != nil {
		t.Fatal(err)
	}
	g := mustBuild(t, b)
	encoded := mustEncode(t, g)
	decoded, err := DecodePGCE(encoded, DefaultPGCELimits())
	if err != nil {
		t.Fatalf("DecodePGCE failed: %v", err)
	}
	if !Equal(decoded, g) {
		t.Error("cyclic graph round trip changed the graph")
	}
	root, ok := decoded.Node(decoded.Roots()[0])
	if !ok || root.Kind() != KindMapping {
		t.Errorf("decoded root kind = %v, want Mapping", root.Kind())
	}
	if reEncoded := mustEncode(t, decoded); !bytes.Equal(reEncoded, encoded) {
		t.Error("cyclic graph re-encode is not byte-stable")
	}
}

// TestRoundTripEveryNodeKind round-trips scalar, sequence, and mapping nodes
// with multi-byte UTF-8 tag and content, pinning byte stability.
func TestRoundTripEveryNodeKind(t *testing.T) {
	b := NewBuilder(DefaultLimits())
	mapping := mustReserve(t, b)
	sequence := mustReserve(t, b)
	scalar := mustReserve(t, b)
	if err := b.DefineScalar(scalar, tagStr, "中\x00x"); err != nil {
		t.Fatal(err)
	}
	if err := b.DefineSequence(sequence, tagSeq, []NodeID{scalar, scalar}); err != nil {
		t.Fatal(err)
	}
	if err := b.DefineMapping(mapping, tagMap, []MappingEntry{
		{Key: scalar, Value: sequence},
		{Key: scalar, Value: scalar},
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.PushRoot(mapping); err != nil {
		t.Fatal(err)
	}
	g := mustBuild(t, b)
	encoded := mustEncode(t, g)
	decoded, err := DecodePGCE(encoded, DefaultPGCELimits())
	if err != nil {
		t.Fatalf("DecodePGCE failed: %v", err)
	}
	if !Equal(decoded, g) {
		t.Fatal("every-kind round trip changed the graph")
	}
	if reEncoded := mustEncode(t, decoded); !bytes.Equal(reEncoded, encoded) {
		t.Error("every-kind re-encode is not byte-stable")
	}
	// The decoded graph owns fresh node identities (RFC 0006 §2): locate
	// the scalar by walking the decoded nodes rather than reusing the
	// original builder IDs.
	var scalarNode *Node
	for _, id := range decoded.Nodes() {
		if n, ok := decoded.Node(id); ok && n.Kind() == KindScalar {
			scalarNode = n
			break
		}
	}
	if scalarNode == nil {
		t.Fatal("decoded scalar node not found")
	}
	content, ok := scalarNode.ScalarContent()
	if !ok || content != "中\x00x" {
		t.Errorf("decoded scalar content = %q, want %q", content, "中\x00x")
	}
}

// TestDecoderRejectsNonminimalVarintTrailingAndInvalidReference mirrors the
// Rust decoder_rejects_nonminimal_varint_trailing_and_invalid_reference test
// (consema-rs/consema-graph/src/pgce.rs).
func TestDecoderRejectsNonminimalVarintTrailingAndInvalidReference(t *testing.T) {
	scalar := hexScalar(t)
	limits := DefaultPGCELimits()

	nonminimal := make([]byte, 0, len(scalar)+1)
	nonminimal = append(nonminimal, scalar[:4]...)
	nonminimal = append(nonminimal, 0x81, 0x00)
	nonminimal = append(nonminimal, scalar[5:]...)
	if _, err := DecodePGCE(nonminimal, limits); !IsPGCEError(err, ErrNonMinimalVarint) {
		t.Errorf("nonminimal varint err = %v, want NonMinimalVarint", err)
	}

	trailing := append(append([]byte(nil), scalar...), 0)
	if _, err := DecodePGCE(trailing, limits); !IsPGCEError(err, ErrTrailingBytes) {
		t.Errorf("trailing bytes err = %v, want TrailingBytes", err)
	}

	invalidReference := append([]byte(nil), scalar...)
	invalidReference[7] = 1 // the root ID references node 1 of a 1-node graph
	if _, err := DecodePGCE(invalidReference, limits); !IsPGCEError(err, ErrReferenceOutOfRange) {
		t.Errorf("invalid reference err = %v, want ReferenceOutOfRange", err)
	}
}

// TestDecoderRejectsNoncanonicalNodeNumbering mirrors the Rust
// decoder_rejects_noncanonical_node_numbering test
// (consema-rs/consema-graph/src/pgce.rs).
func TestDecoderRejectsNoncanonicalNodeNumbering(t *testing.T) {
	bytes := []byte{'P', 'G', 'C', 'E',
		1, // version
		1, // roots
		2, // nodes
		1, // root is node 1, violating canonical first discovery
		nodeScalar, 21}
	bytes = append(bytes, tagStr...)
	bytes = append(bytes, 1, 'x', nodeSequence, 21)
	bytes = append(bytes, tagSeq...)
	bytes = append(bytes, 1, 0)
	if _, err := DecodePGCE(bytes, DefaultPGCELimits()); !IsPGCEError(err, ErrNonCanonicalNodeOrder) {
		t.Errorf("err = %v, want NonCanonicalNodeOrder", err)
	}
}

// TestDecoderRejectsStructuralFailures pins the remaining strict-decode
// rejection classes of RFC 0006 §5: wrong magic, unsupported version,
// unknown node record, invalid UTF-8, invalid tag, unreachable node, and
// truncated input.
func TestDecoderRejectsStructuralFailures(t *testing.T) {
	limits := DefaultPGCELimits()

	// Wrong magic.
	if _, err := DecodePGCE([]byte("PGCF\x01\x00\x00"), limits); !IsPGCEError(err, ErrInvalidMagic) {
		t.Errorf("wrong magic err = %v, want InvalidMagic", err)
	}
	// Empty input is truncated inside the magic.
	if _, err := DecodePGCE(nil, limits); !IsPGCEError(err, ErrUnexpectedEnd) {
		t.Errorf("empty input err = %v, want UnexpectedEnd", err)
	}
	// Unsupported version.
	if _, err := DecodePGCE([]byte("PGCE\x02\x00\x00"), limits); !IsPGCEError(err, ErrUnsupportedVersion) {
		t.Errorf("version 2 err = %v, want UnsupportedVersion", err)
	}
	// Unknown node record octet 0x21 (PVCE's string tag is not a PGCE
	// node record).
	if _, err := DecodePGCE([]byte("PGCE\x01\x01\x01\x00\x21\x01\x78"), limits); !IsPGCEError(err, ErrUnknownNodeKind) {
		t.Errorf("unknown kind err = %v, want UnknownNodeKind", err)
	}
	// Invalid UTF-8 scalar content.
	if _, err := DecodePGCE([]byte("PGCE\x01\x01\x01\x00\x20\x01\x78\x01\xff"), limits); !IsPGCEError(err, ErrInvalidUTF8) {
		t.Errorf("invalid utf-8 err = %v, want InvalidUTF8", err)
	}
	// Empty tag fails the resolved-tag rule (RFC 0006 §2).
	if _, err := DecodePGCE([]byte("PGCE\x01\x01\x01\x00\x20\x00\x01\x78"), limits); !IsPGCEError(err, ErrInvalidTag) {
		t.Errorf("empty tag err = %v, want InvalidTag", err)
	}
	// A defined node no root reaches fails graph construction (wrapped as
	// InvalidGraph; no partial graph escapes). Two scalar records with one
	// root referencing only the first.
	unreachable, _ := hex.DecodeString("504743450101020020017801782001790179")
	if _, err := DecodePGCE(unreachable, limits); !IsPGCEError(err, ErrInvalidGraph) {
		t.Errorf("unreachable node err = %v, want InvalidGraph", err)
	}
	// Truncated inside the node records.
	truncated := hexScalar(t)[:10]
	if _, err := DecodePGCE(truncated, limits); !IsPGCEError(err, ErrUnexpectedEnd) {
		t.Errorf("truncated stream err = %v, want UnexpectedEnd", err)
	}
}

// TestEncodeAndDecodeLimitsFailAtomically mirrors the Rust
// encode_and_decode_limits_fail_atomically test
// (consema-rs/consema-graph/src/pgce.rs): both directions fail with a
// stream-bytes resource limit and never return partial output.
func TestEncodeAndDecodeLimitsFailAtomically(t *testing.T) {
	scalar := hexScalar(t)
	limits := DefaultPGCELimits()
	limits.MaxStreamBytes = len(scalar) - 1
	if _, err := DecodePGCE(scalar, limits); !IsPGCEError(err, ErrResourceLimit) {
		t.Errorf("stream-limited decode err = %v, want ResourceLimit(stream-bytes)", err)
	}
	g, err := DecodePGCE(scalar, DefaultPGCELimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EncodePGCEBounded(g, limits); !IsPGCEError(err, ErrResourceLimit) {
		t.Errorf("stream-limited encode err = %v, want ResourceLimit(stream-bytes)", err)
	}
}

// TestCodecLimitBoundaries pins the exact-boundary semantics: a stream
// exactly at a limit decodes; one beyond fails. The decoded graph must stay
// exact at the boundary.
func TestCodecLimitBoundaries(t *testing.T) {
	scalar := hexScalar(t)
	limits := DefaultPGCELimits()
	limits.MaxStreamBytes = len(scalar)
	if _, err := DecodePGCE(scalar, limits); err != nil {
		t.Errorf("MaxStreamBytes == stream size failed: %v", err)
	}

	// Nodes: the scalar graph has 1 node.
	limits = DefaultPGCELimits()
	limits.MaxNodes = 1
	if _, err := DecodePGCE(scalar, limits); err != nil {
		t.Errorf("MaxNodes == node count failed: %v", err)
	}
	limits.MaxNodes = 0
	if _, err := DecodePGCE(scalar, limits); !IsPGCEError(err, ErrResourceLimit) {
		t.Errorf("MaxNodes == 0 err = %v, want ResourceLimit(graph-nodes)", err)
	}

	// Container entries: a 2-item sequence.
	b := NewBuilder(DefaultLimits())
	root := mustReserve(t, b)
	a := mustReserve(t, b)
	c := mustReserve(t, b)
	if err := b.DefineScalar(a, tagStr, "a"); err != nil {
		t.Fatal(err)
	}
	if err := b.DefineScalar(c, tagStr, "c"); err != nil {
		t.Fatal(err)
	}
	if err := b.DefineSequence(root, tagSeq, []NodeID{a, c}); err != nil {
		t.Fatal(err)
	}
	if err := b.PushRoot(root); err != nil {
		t.Fatal(err)
	}
	sequence := mustEncode(t, mustBuild(t, b))
	limits = DefaultPGCELimits()
	limits.MaxContainerEntries = 2
	if _, err := DecodePGCE(sequence, limits); err != nil {
		t.Errorf("MaxContainerEntries == 2 failed: %v", err)
	}
	limits.MaxContainerEntries = 1
	if _, err := DecodePGCE(sequence, limits); !IsPGCEError(err, ErrResourceLimit) {
		t.Errorf("MaxContainerEntries == 1 err = %v, want ResourceLimit(container-entries)", err)
	}
}

// TestPGCEFailuresHaveStableCodes mirrors the Rust
// pgce_failures_have_stable_v5_codes test
// (consema-rs/consema-graph/src/pgce.rs): codec failures carry the
// frozen "core.pgce.*@1" codes.
func TestPGCEFailuresHaveStableCodes(t *testing.T) {
	if got := (&PGCEError{Kind: ErrInvalidMagic}).Code(); got != "core.pgce.invalid@1" {
		t.Errorf("InvalidMagic code = %q, want core.pgce.invalid@1", got)
	}
	if got := (&PGCEError{Kind: ErrNonMinimalVarint}).Code(); got != "core.pgce.non-canonical@1" {
		t.Errorf("NonMinimalVarint code = %q, want core.pgce.non-canonical@1", got)
	}
	if got := (&PGCEError{Kind: ErrNonCanonicalNodeOrder}).Code(); got != "core.pgce.non-canonical@1" {
		t.Errorf("NonCanonicalNodeOrder code = %q, want core.pgce.non-canonical@1", got)
	}
	if got := (&PGCEError{Kind: ErrNonCanonicalEncoding}).Code(); got != "core.pgce.non-canonical@1" {
		t.Errorf("NonCanonicalEncoding code = %q, want core.pgce.non-canonical@1", got)
	}
	if got := (&PGCEError{Kind: ErrUnsupportedVersion, Value: 2}).Code(); got != "core.pgce.unsupported-version@1" {
		t.Errorf("UnsupportedVersion code = %q, want core.pgce.unsupported-version@1", got)
	}
	if got := (&PGCEError{Kind: ErrResourceLimit, Field: "stream-bytes"}).Code(); got != "core.pgce.resource-limit@1" {
		t.Errorf("ResourceLimit code = %q, want core.pgce.resource-limit@1", got)
	}
	// A wrapped construction resource limit still maps to the resource
	// code (pgce.rs); any other wrapped failure maps to invalid.
	resource := &PGCEError{Kind: ErrInvalidGraph, Cause: &GraphError{Kind: ErrGraphResourceLimit, Field: "traversal-depth"}}
	if got := resource.Code(); got != "core.pgce.resource-limit@1" {
		t.Errorf("InvalidGraph(resource) code = %q, want core.pgce.resource-limit@1", got)
	}
	size := &PGCEError{Kind: ErrInvalidGraph, Cause: &GraphError{Kind: ErrGraphSizeOverflow}}
	if got := size.Code(); got != "core.pgce.resource-limit@1" {
		t.Errorf("InvalidGraph(size) code = %q, want core.pgce.resource-limit@1", got)
	}
	invalid := &PGCEError{Kind: ErrInvalidGraph, Cause: &GraphError{Kind: ErrGraphUnreachableNode}}
	if got := invalid.Code(); got != "core.pgce.invalid@1" {
		t.Errorf("InvalidGraph(unreachable) code = %q, want core.pgce.invalid@1", got)
	}
	if got := (&PGCEError{Kind: ErrUnknownNodeKind}).Code(); got != "core.pgce.invalid@1" {
		t.Errorf("UnknownNodeKind code = %q, want core.pgce.invalid@1", got)
	}
}

// TestDecodeNilGraphEncodeErrors pins the Go-side invalid-value failure: a
// nil graph is rejected by the encoder with the typed error.
func TestDecodeNilGraphEncodeErrors(t *testing.T) {
	if _, err := EncodePGCE(nil); !IsPGCEError(err, ErrInvalidValue) {
		t.Errorf("EncodePGCE(nil) err = %v, want InvalidValue", err)
	}
	if got := (&PGCEError{Kind: ErrInvalidValue}).Code(); got != "core.pgce.invalid@1" {
		t.Errorf("InvalidValue code = %q, want core.pgce.invalid@1", got)
	}
}

// TestDecodeRejectsTraversalDepthLimit pins that a wire graph deeper than
// the traversal-depth limit fails as a resource limit (mapped through
// construction), never producing a partial graph.
func TestDecodeRejectsTraversalDepthLimit(t *testing.T) {
	// One root scalar with a deeper chain: root sequence -> scalar.
	b := NewBuilder(DefaultLimits())
	root := mustReserve(t, b)
	child := mustReserve(t, b)
	if err := b.DefineScalar(child, tagStr, "x"); err != nil {
		t.Fatal(err)
	}
	if err := b.DefineSequence(root, tagSeq, []NodeID{child}); err != nil {
		t.Fatal(err)
	}
	if err := b.PushRoot(root); err != nil {
		t.Fatal(err)
	}
	encoded := mustEncode(t, mustBuild(t, b))
	limits := DefaultPGCELimits()
	limits.MaxTraversalDepth = 0
	if _, err := DecodePGCE(encoded, limits); !IsPGCEError(err, ErrResourceLimit) {
		t.Errorf("depth-limited decode err = %v, want ResourceLimit(traversal-depth)", err)
	}
}

// mustEncode encodes g or fails the test.
func mustEncode(t *testing.T, g *Graph) []byte {
	t.Helper()
	encoded, err := EncodePGCE(g)
	if err != nil {
		t.Fatalf("EncodePGCE failed: %v", err)
	}
	return encoded
}

// hexScalar returns the canonical PGCE stream of one scalar "x" node (the
// Rust hex_scalar helper, consema-rs/consema-graph/src/pgce.rs).
func hexScalar(t *testing.T) []byte {
	t.Helper()
	b := NewBuilder(DefaultLimits())
	root := mustReserve(t, b)
	if err := b.DefineScalar(root, tagStr, "x"); err != nil {
		t.Fatal(err)
	}
	if err := b.PushRoot(root); err != nil {
		t.Fatal(err)
	}
	return mustEncode(t, mustBuild(t, b))
}
