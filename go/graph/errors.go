package graph

import (
	"errors"
	"fmt"
)

// GraphErrorKind enumerates the stable graph construction failures (the Rust
// GraphBuildError, consema-rs/consema-graph/src/lib.rs:192-218). Every kind maps
// to one frozen registered code (see Code); the error text itself is human
// presentation only (RFC 0016 §6, roadmap §16.1: "Go error text 不参与规范比
// 较").
type GraphErrorKind uint8

const (
	// ErrGraphResourceLimit: a configured resource bound was exceeded;
	// Field names the limit ("graph-nodes", "graph-roots", "graph-edges",
	// "container-entries", "tag-bytes", "scalar-bytes",
	// "traversal-depth").
	ErrGraphResourceLimit GraphErrorKind = iota
	// ErrGraphSizeOverflow: a count or index exceeded the host
	// representation. Go int arithmetic cannot overflow for any graph
	// constructible under the limits, so this kind is retained for API
	// parity with the Rust error surface and never fires in practice.
	ErrGraphSizeOverflow
	// ErrGraphUnknownNode: a graph-local node ID was not reserved by this
	// builder.
	ErrGraphUnknownNode
	// ErrGraphWrongGraph: a node ID belonged to a different builder or
	// completed graph.
	ErrGraphWrongGraph
	// ErrGraphDuplicateDefinition: one reserved node was defined more than
	// once.
	ErrGraphDuplicateDefinition
	// ErrGraphUndefinedNode: one reserved node had no definition at build
	// time.
	ErrGraphUndefinedNode
	// ErrGraphUnreachableNode: a defined node was not reachable from any
	// root.
	ErrGraphUnreachableNode
	// ErrGraphInvalidTag: a tag was empty or contained ASCII control or
	// whitespace.
	ErrGraphInvalidTag
	// ErrGraphInvalidUTF8: a tag or a scalar's canonical content was not
	// valid UTF-8. The Rust side cannot construct such graphs at all (its
	// Arc<str> invariant, consema-rs/consema-graph/src/lib.rs:94-157); Go must
	// validate explicitly at the builder boundary, and the PGCE decoder
	// maps this kind back to the codec's ErrInvalidUTF8.
	ErrGraphInvalidUTF8
)

// The frozen registered codes, transcribed from the Rust StableFailure
// mapping in consema-rs/consema-graph/src/lib.rs:228-242
// (graph_build_error_code).
const (
	codeGraphResourceLimit = "core.graph.resource-limit@1"
	codeGraphInvalid       = "core.graph.invalid@1"
)

// GraphError is the typed graph construction failure. It implements error
// and the RFC 0016 §6 Code() contract: the stable code is always the
// registered code, so cross-language error-code parity holds.
type GraphError struct {
	// Kind identifies the failure.
	Kind GraphErrorKind
	// Field names the resource-limit field (see ErrGraphResourceLimit);
	// empty otherwise.
	Field string
	// Observed is the observed amount for ErrGraphResourceLimit.
	Observed int
	// Limit is the configured maximum for ErrGraphResourceLimit.
	Limit int
	// ID is the offending graph-local node ID for the node-specific
	// failures; zero otherwise.
	ID NodeID
}

// Error implements error.
func (e *GraphError) Error() string {
	switch e.Kind {
	case ErrGraphResourceLimit:
		return fmt.Sprintf("graph: resource limit %s: observed %d, limit %d", e.Field, e.Observed, e.Limit)
	case ErrGraphSizeOverflow:
		return "graph: size overflow"
	case ErrGraphUnknownNode:
		return fmt.Sprintf("graph: node %d was not reserved by this builder", e.ID.index)
	case ErrGraphWrongGraph:
		return "graph: node ID belongs to a different builder or completed graph"
	case ErrGraphDuplicateDefinition:
		return fmt.Sprintf("graph: node %d defined more than once", e.ID.index)
	case ErrGraphUndefinedNode:
		return fmt.Sprintf("graph: node %d had no definition at build time", e.ID.index)
	case ErrGraphUnreachableNode:
		return fmt.Sprintf("graph: node %d is not reachable from any root", e.ID.index)
	case ErrGraphInvalidTag:
		return "graph: tag is empty or contains ASCII control or whitespace"
	case ErrGraphInvalidUTF8:
		return "graph: tag or scalar content is not valid UTF-8"
	}
	return fmt.Sprintf("graph: error kind %d", uint8(e.Kind))
}

// Code returns the frozen registered code for the failure (RFC 0016 §6;
// consema-rs/consema-graph/src/lib.rs:228-242).
func (e *GraphError) Code() string {
	switch e.Kind {
	case ErrGraphResourceLimit, ErrGraphSizeOverflow:
		return codeGraphResourceLimit
	case ErrGraphInvalidUTF8:
		return codeGraphInvalid
	}
	return codeGraphInvalid
}

// IsGraphError reports whether err is (or wraps) a *GraphError of the given
// kind.
func IsGraphError(err error, kind GraphErrorKind) bool {
	var target *GraphError
	if !errors.As(err, &target) {
		return false
	}
	return target.Kind == kind
}

// PGCEErrorKind enumerates the strict PGCE/1 failures of the encoder and
// decoder (the Rust PgceEncodeError and PgceDecodeError,
// consema-rs/consema-graph/src/pgce.rs:70-152). Every kind maps to one frozen
// registered code (see Code); the error text itself is human presentation
// only (RFC 0016 §6).
type PGCEErrorKind uint8

const (
	// ErrInvalidMagic: the stream magic did not match "PGCE".
	ErrInvalidMagic PGCEErrorKind = iota
	// ErrUnsupportedVersion: the encoding version is not 1.
	ErrUnsupportedVersion
	// ErrUnexpectedEnd: input ended inside a required field.
	ErrUnexpectedEnd
	// ErrNonMinimalVarint: a varint was not the shortest representation
	// of its value.
	ErrNonMinimalVarint
	// ErrVarintOverflow: a varint or host-size conversion overflowed.
	ErrVarintOverflow
	// ErrUnknownNodeKind: a node record octet is not assigned by PGCE/1.
	ErrUnknownNodeKind
	// ErrInvalidUTF8: a length-delimited string was not UTF-8.
	ErrInvalidUTF8
	// ErrInvalidTag: a tag was empty or contained ASCII control or
	// whitespace.
	ErrInvalidTag
	// ErrReferenceOutOfRange: a root or edge referenced a node outside
	// node_count; Value carries the offending reference.
	ErrReferenceOutOfRange
	// ErrNonCanonicalNodeOrder: wire IDs were not assigned in canonical
	// first-discovery order.
	ErrNonCanonicalNodeOrder
	// ErrTrailingBytes: bytes followed the one complete graph.
	ErrTrailingBytes
	// ErrInvalidGraph: a structurally decoded graph violated graph
	// construction invariants; Cause carries the *GraphError.
	ErrInvalidGraph
	// ErrNonCanonicalEncoding: re-encoding produced different bytes (RFC
	// 0006 §5 defense-in-depth rule).
	ErrNonCanonicalEncoding
	// ErrResourceLimit: a declared resource limit was reached; Field
	// names the limit ("stream-bytes", "graph-roots", "graph-nodes",
	// "graph-edges", "container-entries", "tag-bytes", "scalar-bytes",
	// "traversal-depth").
	ErrResourceLimit
	// ErrInvalidValue: a nil graph was passed to the encoder (the
	// Go-side analog of the core invalid-value failure).
	ErrInvalidValue
)

// The frozen registered codes, transcribed from the Rust StableFailure
// mapping in consema-rs/consema-graph/src/pgce.rs:162-216
// (pgce_decode_error_code / pgce_encode_error_code).
const (
	codePGCEInvalid            = "core.pgce.invalid@1"
	codePGCEResourceLimit      = "core.pgce.resource-limit@1"
	codePGCENonCanonical       = "core.pgce.non-canonical@1"
	codePGCEUnsupportedVersion = "core.pgce.unsupported-version@1"
)

// PGCEError is the typed PGCE/1 codec failure (encode or decode). It
// implements error and the RFC 0016 §6 Code() contract: the stable code is
// always the registered code, so cross-language error-code parity holds.
type PGCEError struct {
	// Kind identifies the failure.
	Kind PGCEErrorKind
	// Field names the resource-limit field (see ErrResourceLimit); empty
	// otherwise.
	Field string
	// Value carries the offending version, node-kind octet, or reference
	// for context; zero otherwise.
	Value uint64
	// Cause carries the wrapped *GraphError for ErrInvalidGraph; nil
	// otherwise.
	Cause error
}

// Error implements error.
func (e *PGCEError) Error() string {
	switch e.Kind {
	case ErrInvalidMagic:
		return "graph: PGCE/1 stream magic did not match \"PGCE\""
	case ErrUnsupportedVersion:
		return fmt.Sprintf("graph: PGCE/1 unsupported version %d (want 1)", e.Value)
	case ErrUnexpectedEnd:
		return "graph: PGCE/1 input ended inside a required field"
	case ErrNonMinimalVarint:
		return "graph: PGCE/1 non-canonical (non-minimal) unsigned varint"
	case ErrVarintOverflow:
		return "graph: PGCE/1 varint or host-size conversion overflowed"
	case ErrUnknownNodeKind:
		return fmt.Sprintf("graph: PGCE/1 unknown node record octet 0x%x", e.Value)
	case ErrInvalidUTF8:
		return "graph: PGCE/1 string bytes are not valid UTF-8"
	case ErrInvalidTag:
		return "graph: PGCE/1 tag is empty or contains ASCII control or whitespace"
	case ErrReferenceOutOfRange:
		return fmt.Sprintf("graph: PGCE/1 node reference %d is outside node_count", e.Value)
	case ErrNonCanonicalNodeOrder:
		return "graph: PGCE/1 node records are not ordered by canonical first discovery"
	case ErrTrailingBytes:
		return "graph: PGCE/1 trailing bytes after the one complete graph"
	case ErrInvalidGraph:
		return fmt.Sprintf("graph: PGCE/1 invalid graph: %v", e.Cause)
	case ErrNonCanonicalEncoding:
		return "graph: PGCE/1 re-encoding produced different bytes"
	case ErrResourceLimit:
		return "graph: PGCE/1 resource limit: " + e.Field
	case ErrInvalidValue:
		return "graph: PGCE/1 invalid value"
	}
	return fmt.Sprintf("graph: PGCE/1 error kind %d", uint8(e.Kind))
}

// Code returns the frozen registered code for the failure (RFC 0016 §6;
// consema-rs/consema-graph/src/pgce.rs:162-216).
func (e *PGCEError) Code() string {
	switch e.Kind {
	case ErrResourceLimit:
		return codePGCEResourceLimit
	case ErrUnsupportedVersion:
		return codePGCEUnsupportedVersion
	case ErrNonMinimalVarint, ErrNonCanonicalNodeOrder, ErrNonCanonicalEncoding:
		return codePGCENonCanonical
	case ErrInvalidGraph:
		var graphErr *GraphError
		if errors.As(e.Cause, &graphErr) &&
			(graphErr.Kind == ErrGraphResourceLimit || graphErr.Kind == ErrGraphSizeOverflow) {
			return codePGCEResourceLimit
		}
		return codePGCEInvalid
	}
	return codePGCEInvalid
}

// Unwrap returns the wrapped graph construction failure for ErrInvalidGraph
// (nil otherwise), so errors.As can reach the *GraphError.
func (e *PGCEError) Unwrap() error { return e.Cause }

// IsPGCEError reports whether err is (or wraps) a *PGCEError of the given
// kind.
func IsPGCEError(err error, kind PGCEErrorKind) bool {
	var target *PGCEError
	if !errors.As(err, &target) {
		return false
	}
	return target.Kind == kind
}
