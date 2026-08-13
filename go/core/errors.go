package core

import (
	"errors"
	"fmt"
)

// PVCEErrorKind enumerates the strict PVCE/1 failures of the encoder and
// decoder. Every kind maps to one frozen registered code (see Code); the
// error text itself is human presentation only (RFC 0016 §6, roadmap §16.1:
// "Go error text 不参与规范比较").
//
// The Rust decoder defines two additional failure kinds that cannot arise in
// the closed fifteen-kind Go value model: NestedExtendedValue
// ("core.pvce.nested-extended@1") and ExpectedCoreValue
// ("core.pvce.expected-core@1"). Go has no ExtendedValue type at all, so
// extended (0x7f) and nested extended records fail as ErrUnknownCoreTag; a
// nested 0x7f record inside a container differs from the Rust
// NestedExtendedValue classification (documented reachable-code difference,
// doc.go).
type PVCEErrorKind uint8

const (
	// ErrInvalidMagic: the stream magic did not match "PVCE".
	ErrInvalidMagic PVCEErrorKind = iota
	// ErrUnsupportedVersion: the encoding version is not 1.
	ErrUnsupportedVersion
	// ErrUnexpectedEnd: input ended inside a required field.
	ErrUnexpectedEnd
	// ErrTrailingBytes: bytes followed the root record.
	ErrTrailingBytes
	// ErrTrailingPayload: bytes followed a fully decoded record payload.
	ErrTrailingPayload
	// ErrTrailingField: bytes followed a fully decoded nested field.
	ErrTrailingField
	// ErrNonCanonicalVarint: an unsigned varint was not shortest-form.
	ErrNonCanonicalVarint
	// ErrVarintOverflow: an unsigned varint exceeded 64 bits.
	ErrVarintOverflow
	// ErrLengthOverflow: a length did not fit the host address space.
	ErrLengthOverflow
	// ErrResourceLimit: a declared resource limit was reached; Field names
	// the limit ("stream-bytes", "nesting-depth", "value-nodes",
	// "container-entries", "integer-bytes", "blob-bytes", "record-bytes",
	// "integer-field", "decimal-field").
	ErrResourceLimit
	// ErrUnknownCoreTag: the record tag has no representation in the closed
	// fifteen-kind Go value model (RFC 0016 §4.1). The Rust codec
	// additionally accepts tag 0x7f (ExtendedValue); Go has no ExtendedValue
	// type, so the Go decoder rejects extended roots and any nested 0x7f
	// record here.
	ErrUnknownCoreTag
	// ErrInvalidPayload: a fixed-size payload did not match its tag.
	ErrInvalidPayload
	// ErrInvalidIntegerSign: the integer sign octet is not in the v1
	// registry (0 zero, 1 positive, 2 negative).
	ErrInvalidIntegerSign
	// ErrNonCanonicalInteger: the integer representation was not the unique
	// canonical form (zero sign with magnitude, or a leading zero octet).
	ErrNonCanonicalInteger
	// ErrNonCanonicalDecimal: the decimal coefficient/exponent pair was not
	// normalized (coefficient has trailing decimal zeros, or a zero
	// coefficient has a non-zero exponent).
	ErrNonCanonicalDecimal
	// ErrInvalidUTF8: string bytes were not valid UTF-8.
	ErrInvalidUTF8
	// ErrObjectKeyNotString: an object key record was not a String record.
	ErrObjectKeyNotString
	// ErrDuplicateObjectKey: an object contained a duplicate key.
	ErrDuplicateObjectKey
	// ErrInvalidValue: a nil or otherwise invalid Value was passed to the
	// codec (the Go-side analog of the Rust invalid-value failure).
	ErrInvalidValue
	// ErrInvalidTemporal: date, time, or offset fields were outside the
	// supported ranges (the Rust DecodeError::InvalidTemporal and the
	// ValueBuildError::InvalidDate/InvalidTime/InvalidOffset construction
	// failures, consema-rs/crates/consema-pvce/src/lib.rs:971-979).
	ErrInvalidTemporal
)

// The frozen registered codes, transcribed from the Rust StableFailure
// mapping in consema-rs/crates/consema-pvce/src/lib.rs:1062-1087.
const (
	codeInvalidMagic        = "core.pvce.invalid-magic@1"
	codeUnsupportedVersion  = "core.pvce.unsupported-version@1"
	codeUnexpectedEnd       = "core.pvce.unexpected-end@1"
	codeTrailingBytes       = "core.pvce.trailing-bytes@1"
	codeTrailingPayload     = "core.pvce.trailing-payload@1"
	codeTrailingField       = "core.pvce.trailing-field@1"
	codeNonCanonicalVarint  = "core.pvce.non-canonical-varint@1"
	codeVarintOverflow      = "core.pvce.varint-overflow@1"
	codeLengthOverflow      = "core.pvce.length-overflow@1"
	codeResourceLimit       = "core.pvce.resource-limit@1"
	codeUnknownTag          = "core.pvce.unknown-tag@1"
	codeInvalidPayload      = "core.pvce.invalid-payload@1"
	codeInvalidIntegerSign  = "core.pvce.invalid-integer-sign@1"
	codeNonCanonicalInteger = "core.pvce.non-canonical-integer@1"
	codeNonCanonicalDecimal = "core.pvce.non-canonical-decimal@1"
	codeInvalidUTF8         = "core.pvce.invalid-utf8@1"
	codeObjectKeyNotString  = "core.pvce.object-key-not-string@1"
	codeDuplicateObjectKey  = "core.pvce.duplicate-object-key@1"
	codeInvalidValue        = "core.pvce.invalid-value@1"
	codeInvalidTemporal     = "core.pvce.invalid-temporal@1"
)

// PVCEError is the typed PVCE/1 codec failure (encode or decode). It
// implements error and the RFC 0016 §6 Code() contract: the stable code is
// always the registered code, so cross-language error-code parity holds.
type PVCEError struct {
	// Kind identifies the failure.
	Kind PVCEErrorKind
	// Field names the resource-limit field (see ErrResourceLimit); empty
	// otherwise.
	Field string
	// Value carries the offending tag or version for context; zero
	// otherwise.
	Value uint64
}

// Error implements error.
func (e *PVCEError) Error() string {
	switch e.Kind {
	case ErrInvalidMagic:
		return "core: PVCE/1 stream magic did not match \"PVCE\""
	case ErrUnsupportedVersion:
		return fmt.Sprintf("core: PVCE/1 unsupported version %d (want 1)", e.Value)
	case ErrUnexpectedEnd:
		return "core: PVCE/1 input ended inside a required field"
	case ErrTrailingBytes:
		return "core: PVCE/1 trailing bytes after the root record"
	case ErrTrailingPayload:
		return fmt.Sprintf("core: PVCE/1 trailing payload bytes after record tag 0x%x", e.Value)
	case ErrTrailingField:
		return "core: PVCE/1 trailing bytes after a nested field"
	case ErrNonCanonicalVarint:
		return "core: PVCE/1 non-canonical (non-minimal) unsigned varint"
	case ErrVarintOverflow:
		return "core: PVCE/1 unsigned varint exceeded 64 bits"
	case ErrLengthOverflow:
		return "core: PVCE/1 length overflow"
	case ErrResourceLimit:
		return "core: PVCE/1 resource limit: " + e.Field
	case ErrUnknownCoreTag:
		return fmt.Sprintf("core: PVCE/1 unknown core tag 0x%x", e.Value)
	case ErrInvalidPayload:
		return fmt.Sprintf("core: PVCE/1 invalid payload for record tag 0x%x", e.Value)
	case ErrInvalidIntegerSign:
		return fmt.Sprintf("core: PVCE/1 invalid integer sign octet %d", e.Value)
	case ErrNonCanonicalInteger:
		return "core: PVCE/1 non-canonical integer representation"
	case ErrNonCanonicalDecimal:
		return "core: PVCE/1 non-canonical decimal representation"
	case ErrInvalidUTF8:
		return "core: PVCE/1 string bytes are not valid UTF-8"
	case ErrObjectKeyNotString:
		return "core: PVCE/1 object key record is not a String record"
	case ErrDuplicateObjectKey:
		return "core: PVCE/1 object contains a duplicate key"
	case ErrInvalidValue:
		return "core: PVCE/1 invalid value"
	case ErrInvalidTemporal:
		return "core: PVCE/1 date, time, or offset fields are outside the supported ranges"
	}
	return fmt.Sprintf("core: PVCE/1 error kind %d", uint8(e.Kind))
}

// Code returns the frozen registered code for the failure (RFC 0016 §6;
// consema-rs/crates/consema-pvce/src/lib.rs:1062-1087).
func (e *PVCEError) Code() string {
	switch e.Kind {
	case ErrInvalidMagic:
		return codeInvalidMagic
	case ErrUnsupportedVersion:
		return codeUnsupportedVersion
	case ErrUnexpectedEnd:
		return codeUnexpectedEnd
	case ErrTrailingBytes:
		return codeTrailingBytes
	case ErrTrailingPayload:
		return codeTrailingPayload
	case ErrTrailingField:
		return codeTrailingField
	case ErrNonCanonicalVarint:
		return codeNonCanonicalVarint
	case ErrVarintOverflow:
		return codeVarintOverflow
	case ErrLengthOverflow:
		return codeLengthOverflow
	case ErrResourceLimit:
		return codeResourceLimit
	case ErrUnknownCoreTag:
		return codeUnknownTag
	case ErrInvalidPayload:
		return codeInvalidPayload
	case ErrInvalidIntegerSign:
		return codeInvalidIntegerSign
	case ErrNonCanonicalInteger:
		return codeNonCanonicalInteger
	case ErrNonCanonicalDecimal:
		return codeNonCanonicalDecimal
	case ErrInvalidUTF8:
		return codeInvalidUTF8
	case ErrObjectKeyNotString:
		return codeObjectKeyNotString
	case ErrDuplicateObjectKey:
		return codeDuplicateObjectKey
	case ErrInvalidValue:
		return codeInvalidValue
	case ErrInvalidTemporal:
		return codeInvalidTemporal
	}
	return codeInvalidValue
}

// IsPVCEError reports whether err is (or wraps) a *PVCEError of the given
// kind.
func IsPVCEError(err error, kind PVCEErrorKind) bool {
	var target *PVCEError
	if !errors.As(err, &target) {
		return false
	}
	return target.Kind == kind
}

// resourceLimit builds the typed resource-limit error for one field name.
func resourceLimit(field string) error {
	return &PVCEError{Kind: ErrResourceLimit, Field: field}
}

// DuplicateKeyError reports a duplicate object key at construction time (the
// RFC 0002 object contract; RFC 0016 §4.1: "Objects reject duplicate keys at
// construction time ... maps to a constructor error").
type DuplicateKeyError struct {
	// Key is the duplicated key.
	Key string
}

// Error implements error.
func (e *DuplicateKeyError) Error() string {
	return "core: duplicate object key: " + e.Key
}

// Code returns the frozen registered code "core.pvce.duplicate-object-key@1"
// (consema-rs/crates/consema-pvce/src/lib.rs:1082).
func (e *DuplicateKeyError) Code() string {
	return codeDuplicateObjectKey
}
