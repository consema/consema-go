package protocol

import (
	"errors"
	"fmt"
)

// ProtocolErrorKind enumerates the strict protocol failures shared by the
// canonical JSON and PVCE/1 transports and the fixed-field record decoders.
// The kinds mirror the Rust ProtocolErrorKind
// (crates/consema-protocol/src/error.rs); each kind maps to one frozen
// registered code (see Code).
type ProtocolErrorKind uint8

const (
	// KindInvalidJson: the transport document is not valid JSON.
	KindInvalidJson ProtocolErrorKind = iota
	// KindNonCanonicalJson: the document is valid JSON but not the canonical
	// byte form.
	KindNonCanonicalJson
	// KindInvalidPvce: the PVCE/1 stream is structurally invalid, or carries
	// a record with no representation in the closed fifteen-kind Go model
	// (only the extended 0x7f record).
	KindInvalidPvce
	// KindUnknownContract: the contract ID or version is not registered.
	KindUnknownContract
	// KindSchemaMismatch: the schema discriminator or field order does not
	// match the fixed record schema.
	KindSchemaMismatch
	// KindUnknownField: a fixed schema contains an undeclared field.
	KindUnknownField
	// KindMissingField: a required field is absent.
	KindMissingField
	// KindWrongType: a field has the wrong value type.
	KindWrongType
	// KindInvalidValue: a field value violates its invariant.
	KindInvalidValue
	// KindResourceLimit: a protocol resource limit was reached.
	KindResourceLimit
	// KindProcessLocalHandle: a process-local handle cannot cross the wire.
	KindProcessLocalHandle
)

// The frozen registered codes, transcribed from the Rust ProtocolErrorKind
// mapping (crates/consema-protocol/src/error.rs; the error-code registry
// v1 pins "core.protocol.invalid-value@1" etc.).
const (
	codeInvalidJson        = "core.protocol.invalid-json@1"
	codeNonCanonicalJson   = "core.protocol.non-canonical-json@1"
	codeInvalidPvce        = "core.protocol.invalid-pvce@1"
	codeUnknownContract    = "core.protocol.unknown-contract@1"
	codeSchemaMismatch     = "core.protocol.schema-mismatch@1"
	codeUnknownField       = "core.protocol.unknown-field@1"
	codeMissingField       = "core.protocol.missing-field@1"
	codeWrongType          = "core.protocol.wrong-type@1"
	codeInvalidValue       = "core.protocol.invalid-value@1"
	codeResourceLimit      = "core.protocol.resource-limit@1"
	codeProcessLocalHandle = "core.protocol.process-local-handle@1"
)

// ProtocolError is the typed protocol failure (transport or record level).
// It implements error and the RFC 0016 §6 Code() contract. Path names the
// failing JSON-pointer-ish location ("$.files[0].source_digest"), mirroring
// the Rust error paths so that the shared vectors' error_path facts match.
type ProtocolError struct {
	// Kind identifies the failure.
	Kind ProtocolErrorKind
	// Path is the failing record location, e.g. "$.files[0].source_digest".
	Path string
	// Detail is the human-facing explanation; never part of conformance
	// comparison.
	Detail string
}

// Error implements error.
func (e *ProtocolError) Error() string {
	return fmt.Sprintf("protocol: %s at %s: %s", e.Code(), e.Path, e.Detail)
}

// Code returns the frozen registered code for the failure (RFC 0016 §6;
// crates/consema-protocol/src/error.rs).
func (e *ProtocolError) Code() string {
	switch e.Kind {
	case KindInvalidJson:
		return codeInvalidJson
	case KindNonCanonicalJson:
		return codeNonCanonicalJson
	case KindInvalidPvce:
		return codeInvalidPvce
	case KindUnknownContract:
		return codeUnknownContract
	case KindSchemaMismatch:
		return codeSchemaMismatch
	case KindUnknownField:
		return codeUnknownField
	case KindMissingField:
		return codeMissingField
	case KindWrongType:
		return codeWrongType
	case KindInvalidValue:
		return codeInvalidValue
	case KindResourceLimit:
		return codeResourceLimit
	case KindProcessLocalHandle:
		return codeProcessLocalHandle
	}
	return codeInvalidValue
}

// IsProtocolError reports whether err is (or wraps) a *ProtocolError of the
// given kind.
func IsProtocolError(err error, kind ProtocolErrorKind) bool {
	var target *ProtocolError
	if !errors.As(err, &target) {
		return false
	}
	return target.Kind == kind
}

// invalid builds the InvalidValue protocol error (crate::schema::invalid).
func invalid(path, detail string) *ProtocolError {
	return &ProtocolError{Kind: KindInvalidValue, Path: path, Detail: detail}
}

// resource builds the ResourceLimit protocol error.
func resource(path, detail string) *ProtocolError {
	return &ProtocolError{Kind: KindResourceLimit, Path: path, Detail: detail}
}

// protocolError builds an error with an explicit kind.
func protocolError(kind ProtocolErrorKind, path, detail string) *ProtocolError {
	return &ProtocolError{Kind: kind, Path: path, Detail: detail}
}

// asProtocolError unwraps a *ProtocolError from any error value; non
// protocol errors degrade to an InvalidValue marker carrying the original
// text.
func asProtocolError(err error) *ProtocolError {
	var target *ProtocolError
	if errors.As(err, &target) {
		return target
	}
	return &ProtocolError{Kind: KindInvalidValue, Path: "$", Detail: err.Error()}
}
