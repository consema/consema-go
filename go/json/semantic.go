package json

// Regional semantic availability and the native JSON value categories
// (consema-json lib.rs:289-340).

// SemanticUnavailable is the stable reason that a region has no native
// semantic value (consema-json lib.rs:309-319).
type SemanticUnavailable uint8

// The four frozen unavailability reasons.
const (
	// SemanticUnavailableMissing means the parser inserted a zero-width
	// missing value.
	SemanticUnavailableMissing SemanticUnavailable = iota
	// SemanticUnavailableErrorRegion means source bytes occupy an explicit
	// error region.
	SemanticUnavailableErrorRegion
	// SemanticUnavailableInvalidLiteral means literal syntax was complete
	// but its decoded meaning was invalid.
	SemanticUnavailableInvalidLiteral
	// SemanticUnavailableChildUnavailable means a child prevents complete
	// container semantics.
	SemanticUnavailableChildUnavailable
)

// String returns the stable reason name (the Rust variant name).
func (u SemanticUnavailable) String() string {
	switch u {
	case SemanticUnavailableMissing:
		return "Missing"
	case SemanticUnavailableErrorRegion:
		return "ErrorRegion"
	case SemanticUnavailableInvalidLiteral:
		return "InvalidLiteral"
	case SemanticUnavailableChildUnavailable:
		return "ChildUnavailable"
	}
	return "ChildUnavailable"
}

// SemanticAvailability is one region's availability verdict
// (consema-json lib.rs:289-306): either complete native meaning or a stable
// unavailability reason. The zero value is an unavailable region with
// reason Missing; use Available to construct an available verdict.
type SemanticAvailability[T any] struct {
	available bool
	value     T
	reason    SemanticUnavailable
}

// Available returns the complete-native-meaning verdict.
func Available[T any](value T) SemanticAvailability[T] {
	return SemanticAvailability[T]{available: true, value: value}
}

// Unavailable returns the stable unavailability verdict.
func Unavailable[T any](reason SemanticUnavailable) SemanticAvailability[T] {
	return SemanticAvailability[T]{reason: reason}
}

// IsAvailable reports whether native meaning is complete.
func (a SemanticAvailability[T]) IsAvailable() bool { return a.available }

// Value returns the native value; valid only when IsAvailable is true.
func (a SemanticAvailability[T]) Value() T { return a.value }

// Reason returns the unavailability reason; meaningful only when
// IsAvailable is false.
func (a SemanticAvailability[T]) Reason() SemanticUnavailable { return a.reason }

// JsonValueKind is the native JSON value category, preserving
// integer-form versus decimal-form numbers (consema-json lib.rs:321-340).
type JsonValueKind uint8

// The eight frozen native categories.
const (
	// JsonValueKindNull is JSON null.
	JsonValueKindNull JsonValueKind = iota
	// JsonValueKindBoolean is a boolean.
	JsonValueKindBoolean
	// JsonValueKindInteger is a number without a decimal point or exponent.
	JsonValueKindInteger
	// JsonValueKindDecimal is a number with a decimal point or exponent.
	JsonValueKindDecimal
	// JsonValueKindBinaryFloat64 is an exact frozen IEEE-754 binary64 datum
	// for a JSON5 non-finite literal.
	JsonValueKindBinaryFloat64
	// JsonValueKindString is a decoded string.
	JsonValueKindString
	// JsonValueKindArray is an ordered array.
	JsonValueKindArray
	// JsonValueKindObject is an ordered object with duplicate member
	// preservation.
	JsonValueKindObject
)

// String returns the stable kind name (the Rust variant spelling used by
// the conformance vectors).
func (k JsonValueKind) String() string {
	switch k {
	case JsonValueKindNull:
		return "Null"
	case JsonValueKindBoolean:
		return "Boolean"
	case JsonValueKindInteger:
		return "Integer"
	case JsonValueKindDecimal:
		return "Decimal"
	case JsonValueKindBinaryFloat64:
		return "BinaryFloat64"
	case JsonValueKindString:
		return "String"
	case JsonValueKindArray:
		return "Array"
	case JsonValueKindObject:
		return "Object"
	}
	return "Object"
}

// JsonAccessErrorKind is a stable typed JSON access failure
// (consema-json lib.rs:612-621).
type JsonAccessErrorKind uint8

// The three frozen access failures.
const (
	// JsonAccessErrorWrongSnapshot: a NodeRef belongs to another snapshot.
	JsonAccessErrorWrongSnapshot JsonAccessErrorKind = iota
	// JsonAccessErrorWrongRole: a NodeRef role cannot be used by this
	// operation.
	JsonAccessErrorWrongRole
	// JsonAccessErrorUnknownNode: an index is not present in this snapshot.
	JsonAccessErrorUnknownNode
)

// JsonAccessError is a stable typed JSON access failure. It implements
// error; the registered code is the generic invalid-input code (RFC 0016
// §6) because access failures are invalid handle facts.
type JsonAccessError struct {
	// Kind identifies the failure.
	Kind JsonAccessErrorKind
}

// Error implements error.
func (e *JsonAccessError) Error() string {
	switch e.Kind {
	case JsonAccessErrorWrongSnapshot:
		return "json: NodeRef belongs to another snapshot"
	case JsonAccessErrorWrongRole:
		return "json: NodeRef role cannot be used by this operation"
	case JsonAccessErrorUnknownNode:
		return "json: index is not present in this snapshot"
	}
	return "json: access error"
}

// Code returns the registered invalid-input code (RFC 0016 §6).
func (e *JsonAccessError) Code() string { return "core.protocol.invalid-value@1" }
