package graph

import (
	"bytes"
)

// This file reimplements the PGCE/1 wire format from the Rust reference
// codec, consema-rs/crates/consema-graph/src/pgce.rs (RFC 0006 §5; RFC 0016 §4.1:
// 144-146):
//
//   - stream magic is the ASCII octets "PGCE" (pgce.rs:12);
//   - version is minimal unsigned LEB128 1 (pgce.rs:14);
//   - node records are 0x20 Scalar, 0x40 Sequence, 0x41 Mapping
//     (pgce.rs:16-18);
//   - all unsigned lengths/counts/IDs are minimal unsigned LEB128
//     (pgce.rs:398-419).
//
// The encoder applies the canonical numbering of RFC 0006 §4 first, so
// isomorphic graphs have byte-identical PGCE. The decoder is strict: it
// rejects every non-canonical form listed in RFC 0006 §5, including any
// stream whose re-encoding differs from the input (defense-in-depth).

// magicPGCE is the PGCE/1 stream magic (ASCII "PGCE",
// consema-rs/crates/consema-graph/src/pgce.rs:12).
var magicPGCE = [4]byte{'P', 'G', 'C', 'E'}

// pgceVersion is the frozen PGCE/1 version
// (consema-rs/crates/consema-graph/src/pgce.rs:14).
const pgceVersion = 1

// Node record octets (consema-rs/crates/consema-graph/src/pgce.rs:16-18).
const (
	nodeScalar   byte = 0x20
	nodeSequence byte = 0x40
	nodeMapping  byte = 0x41
)

// PGCELimits are the bounded PGCE/1 encode/decode resource limits (RFC 0006
// §6; the Rust PgceLimits, consema-rs/crates/consema-graph/src/pgce.rs:21-54). The zero
// value rejects every stream; use DefaultPGCELimits.
type PGCELimits struct {
	// MaxStreamBytes is the maximum complete PGCE stream bytes.
	MaxStreamBytes int
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
	// MaxTraversalDepth is the maximum canonical first-visit traversal
	// depth.
	MaxTraversalDepth int
}

// DefaultPGCELimits returns the frozen defaults (64 MiB stream, 1,000,000
// roots, 1,000,000 nodes, 2,000,000 edges, 1,000,000 container entries,
// 1 MiB tag, 64 MiB scalar, depth 256; consema-rs/crates/consema-graph/src/pgce.rs:
// 41-54).
func DefaultPGCELimits() PGCELimits {
	return PGCELimits{
		MaxStreamBytes:      64 << 20,
		MaxRoots:            1_000_000,
		MaxNodes:            1_000_000,
		MaxEdges:            2_000_000,
		MaxContainerEntries: 1_000_000,
		MaxTagBytes:         1 << 20,
		MaxScalarBytes:      64 << 20,
		MaxTraversalDepth:   256,
	}
}

// graphLimits is the construction-limits subset of the codec limits (the
// Rust PgceLimits::graph_limits, consema-rs/crates/consema-graph/src/pgce.rs:56-68).
func (l PGCELimits) graphLimits() Limits {
	return Limits{
		MaxRoots:            l.MaxRoots,
		MaxNodes:            l.MaxNodes,
		MaxEdges:            l.MaxEdges,
		MaxContainerEntries: l.MaxContainerEntries,
		MaxTagBytes:         l.MaxTagBytes,
		MaxScalarBytes:      l.MaxScalarBytes,
		MaxTraversalDepth:   l.MaxTraversalDepth,
	}
}

// EncodePGCE encodes one graph as a complete canonical PGCE/1 stream with
// the default bounded policy (RFC 0016 §4.2). The bytes are byte-identical
// to the Rust codec's output (consema-rs/crates/consema-graph/src/pgce.rs:219-221); a
// nil graph returns an ErrInvalidValue error.
func EncodePGCE(g *Graph) ([]byte, error) {
	return EncodePGCEBounded(g, DefaultPGCELimits())
}

// EncodePGCEBounded encodes one complete canonical PGCE/1 stream after exact
// size measurement (the Rust encode_pgce_bounded,
// consema-rs/crates/consema-graph/src/pgce.rs:224-275). It never truncates: exceeding
// any limit returns a resource-limit error with no partial output (RFC 0006
// §6).
func EncodePGCEBounded(g *Graph, limits PGCELimits) ([]byte, error) {
	if g == nil {
		return nil, &PGCEError{Kind: ErrInvalidValue}
	}
	if err := validateGraphLimits(g, limits); err != nil {
		return nil, err
	}
	layout := g.layout()
	size, err := measure(g, layout.canonicalIDs, layout.order, limits)
	if err != nil {
		return nil, err
	}
	if err := checkEncodeLimit("stream-bytes", size, limits.MaxStreamBytes); err != nil {
		return nil, err
	}
	out := make([]byte, 0, size)
	out = append(out, magicPGCE[:]...)
	out = appendVarint(out, pgceVersion)
	out = appendVarint(out, uint64(len(g.roots)))
	out = appendVarint(out, uint64(len(g.nodes)))
	for _, root := range g.roots {
		out = appendVarint(out, uint64(layout.canonicalIDs[root.index]))
	}
	for _, index := range layout.order {
		n := &g.nodes[index]
		switch n.kind {
		case KindScalar:
			out = append(out, nodeScalar)
			out = appendBlob(out, []byte(n.tag))
			out = appendBlob(out, []byte(n.scalar))
		case KindSequence:
			out = append(out, nodeSequence)
			out = appendBlob(out, []byte(n.tag))
			out = appendVarint(out, uint64(len(n.items)))
			for _, item := range n.items {
				out = appendVarint(out, uint64(layout.canonicalIDs[item.index]))
			}
		case KindMapping:
			out = append(out, nodeMapping)
			out = appendBlob(out, []byte(n.tag))
			out = appendVarint(out, uint64(len(n.entries)))
			for _, entry := range n.entries {
				out = appendVarint(out, uint64(layout.canonicalIDs[entry.Key.index]))
				out = appendVarint(out, uint64(layout.canonicalIDs[entry.Value.index]))
			}
		}
	}
	return out, nil
}

// validateGraphLimits checks the whole-graph limits and the traversal depth
// before any encoding work (the Rust validate_graph_limits,
// consema-rs/crates/consema-graph/src/pgce.rs:277-284).
func validateGraphLimits(g *Graph, limits PGCELimits) error {
	if err := checkEncodeLimit("graph-roots", len(g.roots), limits.MaxRoots); err != nil {
		return err
	}
	if err := checkEncodeLimit("graph-nodes", len(g.nodes), limits.MaxNodes); err != nil {
		return err
	}
	if err := checkEncodeLimit("graph-edges", g.edges, limits.MaxEdges); err != nil {
		return err
	}
	// A completed graph only reaches the resource-limit path here.
	_, _, err := canonicalOrder(g.nodes, g.roots, limits.MaxTraversalDepth)
	if err != nil {
		return mapBuildToEncode(err)
	}
	return nil
}

// measure computes the exact encoded size of one graph under canonical
// numbering, enforcing the per-node limits (the Rust measure,
// consema-rs/crates/consema-graph/src/pgce.rs:286-339). Go int arithmetic cannot
// overflow for any graph whose sizes pass the checks above (all blobs and
// counts are bounded by the limits), so the Rust SizeOverflow paths are
// unreachable here.
func measure(g *Graph, canonicalIDs, order []int, limits PGCELimits) (int, error) {
	size := len(magicPGCE)
	size += varintSize(pgceVersion)
	size += varintSize(uint64(len(g.roots)))
	size += varintSize(uint64(len(g.nodes)))
	for _, root := range g.roots {
		size += varintSize(uint64(canonicalIDs[root.index]))
	}
	for _, index := range order {
		n := &g.nodes[index]
		if err := checkEncodeLimit("tag-bytes", len(n.tag), limits.MaxTagBytes); err != nil {
			return 0, err
		}
		size += 1
		size += blobSize(len(n.tag))
		switch n.kind {
		case KindScalar:
			if err := checkEncodeLimit("scalar-bytes", len(n.scalar), limits.MaxScalarBytes); err != nil {
				return 0, err
			}
			size += blobSize(len(n.scalar))
		case KindSequence:
			if err := checkEncodeLimit("container-entries", len(n.items), limits.MaxContainerEntries); err != nil {
				return 0, err
			}
			size += varintSize(uint64(len(n.items)))
			for _, item := range n.items {
				size += varintSize(uint64(canonicalIDs[item.index]))
			}
		case KindMapping:
			if err := checkEncodeLimit("container-entries", len(n.entries), limits.MaxContainerEntries); err != nil {
				return 0, err
			}
			size += varintSize(uint64(len(n.entries)))
			for _, entry := range n.entries {
				size += varintSize(uint64(canonicalIDs[entry.Key.index]))
				size += varintSize(uint64(canonicalIDs[entry.Value.index]))
			}
		}
	}
	return size, nil
}

// blobSize returns the encoded size of one length-prefixed byte string (the
// Rust blob_size, consema-rs/crates/consema-graph/src/pgce.rs:348-350).
func blobSize(length int) int {
	return varintSize(uint64(length)) + length
}

// checkEncodeLimit reports ErrResourceLimit when observed exceeds limit (the
// Rust check_encode_limit, consema-rs/crates/consema-graph/src/pgce.rs:360-374).
func checkEncodeLimit(name string, observed, limit int) error {
	if observed > limit {
		return &PGCEError{Kind: ErrResourceLimit, Field: name}
	}
	return nil
}

// mapBuildToEncode maps traversal failures onto encode failures (the Rust
// map_build_to_encode, consema-rs/crates/consema-graph/src/pgce.rs:376-390). Only the
// resource-limit path can occur on a completed graph.
func mapBuildToEncode(err error) error {
	graphErr, ok := err.(*GraphError)
	if !ok {
		return &PGCEError{Kind: ErrInvalidGraph, Cause: err}
	}
	return &PGCEError{Kind: ErrResourceLimit, Field: graphErr.Field}
}

// appendBlob writes a length-prefixed byte string (the Rust write_blob,
// consema-rs/crates/consema-graph/src/pgce.rs:392-396).
func appendBlob(out []byte, value []byte) []byte {
	out = appendVarint(out, uint64(len(value)))
	return append(out, value...)
}

// appendVarint writes the minimal unsigned LEB128 encoding of value (the
// Rust write_varint, consema-rs/crates/consema-graph/src/pgce.rs:398-410).
func appendVarint(out []byte, value uint64) []byte {
	for {
		octet := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			octet |= 0x80
		}
		out = append(out, octet)
		if value == 0 {
			return out
		}
	}
}

// varintSize returns the encoded length of value as a minimal unsigned
// LEB128 (the Rust const varint_size, consema-rs/crates/consema-graph/src/pgce.rs:
// 412-419).
func varintSize(value uint64) int {
	size := 1
	for value >= 0x80 {
		value >>= 7
		size++
	}
	return size
}

// DecodePGCE strictly decodes one canonical PGCE/1 stream (RFC 0016 §4.2),
// mirroring the Rust decode (consema-rs/crates/consema-graph/src/pgce.rs:422-507). The
// decoder rejects every non-canonical form of RFC 0006 §5: wrong magic or
// version, non-minimal or overflowing or truncated varints, unknown node
// records, trailing bytes, invalid UTF-8, empty or invalid tags, counts or
// blobs outside limits, out-of-range references, node records not ordered by
// canonical first discovery, unreachable nodes, and any stream whose
// re-encoding differs from the input. No failure returns a partial graph
// (RFC 0006 §6).
func DecodePGCE(stream []byte, limits PGCELimits) (*Graph, error) {
	if err := checkDecodeLimit("stream-bytes", len(stream), limits.MaxStreamBytes); err != nil {
		return nil, err
	}
	d := &decoder{bytes: stream, limits: limits}
	magic, err := d.take(len(magicPGCE))
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(magic, magicPGCE[:]) {
		return nil, &PGCEError{Kind: ErrInvalidMagic}
	}
	version, err := d.varint()
	if err != nil {
		return nil, err
	}
	if version != pgceVersion {
		return nil, &PGCEError{Kind: ErrUnsupportedVersion, Value: version}
	}
	rootCount, err := d.count("graph-roots", limits.MaxRoots)
	if err != nil {
		return nil, err
	}
	nodeCount, err := d.count("graph-nodes", limits.MaxNodes)
	if err != nil {
		return nil, err
	}

	builder := NewBuilder(limits.graphLimits())
	ids := make([]NodeID, nodeCount)
	for i := 0; i < nodeCount; i++ {
		id, err := builder.ReserveNode()
		if err != nil {
			return nil, mapBuildToDecode(err)
		}
		ids[i] = id
	}

	roots := make([]NodeID, rootCount)
	for i := 0; i < rootCount; i++ {
		index, err := d.reference(nodeCount)
		if err != nil {
			return nil, err
		}
		roots[i] = ids[index]
	}
	for _, root := range roots {
		if err := builder.PushRoot(root); err != nil {
			return nil, mapBuildToDecode(err)
		}
	}

	for i := 0; i < nodeCount; i++ {
		kindOctet, err := d.byte()
		if err != nil {
			return nil, err
		}
		tag, err := d.string("tag-bytes", limits.MaxTagBytes)
		if err != nil {
			return nil, err
		}
		switch kindOctet {
		case nodeScalar:
			content, err := d.string("scalar-bytes", limits.MaxScalarBytes)
			if err != nil {
				return nil, err
			}
			if err := builder.DefineScalar(ids[i], tag, content); err != nil {
				return nil, mapBuildToDecode(err)
			}
		case nodeSequence:
			count, err := d.count("container-entries", limits.MaxContainerEntries)
			if err != nil {
				return nil, err
			}
			if err := d.addEdges(count); err != nil {
				return nil, err
			}
			items := make([]NodeID, count)
			for j := 0; j < count; j++ {
				index, err := d.reference(nodeCount)
				if err != nil {
					return nil, err
				}
				items[j] = ids[index]
			}
			if err := builder.DefineSequence(ids[i], tag, items); err != nil {
				return nil, mapBuildToDecode(err)
			}
		case nodeMapping:
			count, err := d.count("container-entries", limits.MaxContainerEntries)
			if err != nil {
				return nil, err
			}
			// A mapping association contributes a key and a value edge;
			// count is bounded by the container limit, so this product
			// cannot overflow (the Rust checked_mul,
			// consema-rs/crates/consema-graph/src/pgce.rs:477-480).
			if err := d.addEdges(count * 2); err != nil {
				return nil, err
			}
			entries := make([]MappingEntry, count)
			for j := 0; j < count; j++ {
				keyIndex, err := d.reference(nodeCount)
				if err != nil {
					return nil, err
				}
				valueIndex, err := d.reference(nodeCount)
				if err != nil {
					return nil, err
				}
				entries[j] = MappingEntry{Key: ids[keyIndex], Value: ids[valueIndex]}
			}
			if err := builder.DefineMapping(ids[i], tag, entries); err != nil {
				return nil, mapBuildToDecode(err)
			}
		default:
			return nil, &PGCEError{Kind: ErrUnknownNodeKind, Value: uint64(kindOctet)}
		}
	}
	if d.offset != len(stream) {
		return nil, &PGCEError{Kind: ErrTrailingBytes}
	}
	graph, err := builder.Build()
	if err != nil {
		return nil, mapBuildToDecode(err)
	}
	layout := graph.layout()
	for i, index := range layout.order {
		if index != i {
			return nil, &PGCEError{Kind: ErrNonCanonicalNodeOrder}
		}
	}
	encoded, err := EncodePGCEBounded(graph, limits)
	if err != nil {
		return nil, mapEncodeToDecode(err)
	}
	if !bytes.Equal(encoded, stream) {
		return nil, &PGCEError{Kind: ErrNonCanonicalEncoding}
	}
	return graph, nil
}

// decoder is the strict streaming PGCE/1 decoder (the Rust Decoder,
// consema-rs/crates/consema-graph/src/pgce.rs:509-595).
type decoder struct {
	bytes  []byte
	offset int
	limits PGCELimits
	edges  int
}

// byte consumes one octet (the Rust Decoder::byte, pgce.rs:517-524).
func (d *decoder) byte() (byte, error) {
	if d.offset >= len(d.bytes) {
		return 0, &PGCEError{Kind: ErrUnexpectedEnd}
	}
	b := d.bytes[d.offset]
	d.offset++
	return b, nil
}

// take consumes n octets (the Rust Decoder::take, pgce.rs:526-537).
func (d *decoder) take(count int) ([]byte, error) {
	if count < 0 || d.offset+count > len(d.bytes) {
		return nil, &PGCEError{Kind: ErrUnexpectedEnd}
	}
	value := d.bytes[d.offset : d.offset+count]
	d.offset += count
	return value, nil
}

// varint reads one unsigned varint, rejecting non-minimal encodings and
// 64-bit overflow (the Rust Decoder::varint, consema-rs/crates/consema-graph/src/pgce.rs:
// 539-557).
func (d *decoder) varint() (uint64, error) {
	start := d.offset
	var value uint64
	for shift := 0; shift <= 63; shift += 7 {
		octet, err := d.byte()
		if err != nil {
			return 0, err
		}
		payload := uint64(octet & 0x7f)
		if shift == 63 && payload > 1 {
			return 0, &PGCEError{Kind: ErrVarintOverflow}
		}
		value |= payload << shift
		if octet&0x80 == 0 {
			if d.offset-start != varintSize(value) {
				return 0, &PGCEError{Kind: ErrNonMinimalVarint}
			}
			return value, nil
		}
	}
	return 0, &PGCEError{Kind: ErrVarintOverflow}
}

// count reads one varint count, converts it to the host int, and enforces
// the named limit (the Rust Decoder::count, pgce.rs:559-564).
func (d *decoder) count(name string, limit int) (int, error) {
	value, err := d.varint()
	if err != nil {
		return 0, err
	}
	if value > uint64(^uint(0)>>1) {
		return 0, &PGCEError{Kind: ErrVarintOverflow}
	}
	count := int(value)
	if count > limit {
		return 0, &PGCEError{Kind: ErrResourceLimit, Field: name}
	}
	return count, nil
}

// reference reads one node reference and rejects out-of-range IDs (the Rust
// Decoder::reference, pgce.rs:566-574).
func (d *decoder) reference(nodeCount int) (int, error) {
	value, err := d.varint()
	if err != nil {
		return 0, err
	}
	if value > uint64(^uint(0)>>1) {
		return 0, &PGCEError{Kind: ErrReferenceOutOfRange, Value: value}
	}
	index := int(value)
	if index >= nodeCount {
		return 0, &PGCEError{Kind: ErrReferenceOutOfRange, Value: value}
	}
	return index, nil
}

// string reads one length-delimited string (the Rust Decoder::string,
// pgce.rs:576-586). UTF-8 validity is enforced by the builder layer
// (DefineScalar and validateTag return ErrGraphInvalidUTF8, mapped back to
// the codec's ErrInvalidUTF8 by mapBuildToDecode), so the invalid-UTF-8
// interception happens exactly where graph invariants are enforced.
func (d *decoder) string(name string, limit int) (string, error) {
	length, err := d.count(name, limit)
	if err != nil {
		return "", err
	}
	value, err := d.take(length)
	if err != nil {
		return "", err
	}
	return string(value), nil
}

// addEdges accumulates decoded edges under the graph-edges limit (the Rust
// Decoder::add_edges, pgce.rs:588-594). The accumulated total cannot
// overflow Go int arithmetic because every decoded count already passed the
// container limit.
func (d *decoder) addEdges(count int) error {
	d.edges += count
	if d.edges > d.limits.MaxEdges {
		return &PGCEError{Kind: ErrResourceLimit, Field: "graph-edges"}
	}
	return nil
}

// checkDecodeLimit reports ErrResourceLimit when observed exceeds limit (the
// Rust check_decode_limit, consema-rs/crates/consema-graph/src/pgce.rs:597-611).
func checkDecodeLimit(name string, observed, limit int) error {
	if observed > limit {
		return &PGCEError{Kind: ErrResourceLimit, Field: name}
	}
	return nil
}

// mapBuildToDecode maps graph construction failures onto strict decode
// failures (the Rust map_build_to_decode, consema-rs/crates/consema-graph/src/pgce.rs:
// 613-627): resource limits pass through, invalid tags surface directly, and
// every other construction failure wraps as ErrInvalidGraph.
func mapBuildToDecode(err error) error {
	graphErr, ok := err.(*GraphError)
	if !ok {
		return &PGCEError{Kind: ErrInvalidGraph, Cause: err}
	}
	switch graphErr.Kind {
	case ErrGraphResourceLimit:
		return &PGCEError{Kind: ErrResourceLimit, Field: graphErr.Field}
	case ErrGraphInvalidTag:
		return &PGCEError{Kind: ErrInvalidTag}
	case ErrGraphInvalidUTF8:
		// The builder is the single UTF-8 enforcement point; the codec maps
		// its typed error back to the strict wire failure (the Rust
		// Decoder::string InvalidUtf8, pgce.rs:576-586).
		return &PGCEError{Kind: ErrInvalidUTF8}
	}
	return &PGCEError{Kind: ErrInvalidGraph, Cause: graphErr}
}

// mapEncodeToDecode maps re-encoding failures onto strict decode failures
// (the Rust map_encode_to_decode, consema-rs/crates/consema-graph/src/pgce.rs:629-642):
// resource limits pass through; any other encode failure (unreachable here)
// reports as a varint overflow.
func mapEncodeToDecode(err error) error {
	pgceErr, ok := err.(*PGCEError)
	if !ok {
		return err
	}
	if pgceErr.Kind == ErrResourceLimit {
		return pgceErr
	}
	return &PGCEError{Kind: ErrVarintOverflow}
}
