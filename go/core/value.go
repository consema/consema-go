// Package core implements the language-neutral Consema value model for Go:
// the closed fifteen-kind PortableValue (配置内容统一处理标准与 Rust 参考实现.md
// §10; consema-rs/consema-core/src/value.rs: Null, Boolean, Integer,
// Decimal, BinaryFloat32, BinaryFloat64, String, Bytes, Date, Time,
// LocalDateTime, OffsetDateTime, Sequence, Object, EntryMapping), strict
// equality and deterministic hashing, and the PVCE/1 canonical byte codec
// (RFC 0016 §4.2). The wire format is reimplemented from the Rust reference
// codec (consema-rs/consema-pvce/src/lib.rs) and its canonical bytes are pinned by
// golden tests in this package.
//
// The fifteen kinds map exactly as follows:
//
//	PortableValue kind   Go type
//	Object               *core.Object (ordered []Entry, unique keys)
//	Sequence             *core.Array  (ordered []Value)
//	String               core.String
//	Integer              core.Integer (wraps *big.Int)
//	Decimal              core.Decimal (canonical coefficient × 10^exponent)
//	BinaryFloat32        core.BinaryFloat32 (exact IEEE-754 binary32 bits)
//	BinaryFloat64        core.BinaryFloat64 (exact IEEE-754 binary64 bits)
//	Bytes                core.Bytes (octet sequence)
//	Date                 core.Date (proleptic Gregorian, astronomical years)
//	Time                 core.Time (wall clock, exact fractional second)
//	LocalDateTime        core.LocalDateTime (Date + Time)
//	OffsetDateTime       core.OffsetDateTime (LocalDateTime + UTC offset)
//	Boolean              core.Boolean
//	Null                 core.Null (singleton)
//
// Value is a closed interface: only the fifteen types above implement it, so
// exhaustive switches over the kinds never meet unknown kinds (RFC 0016 §4.1:
// "no default that silently accepts unknown kinds"). Object keys are unique:
// the constructors reject duplicate keys (the RFC 0002 object contract),
// mirroring the Rust ObjectBuilder uniqueness invariant. EntryMapping
// associations allow arbitrary keys and duplicates, mirroring the Rust
// EntryMappingBuilder (consema-rs/consema-core/src/value.rs).
//
// The package is standard-library only (https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §1.3);
// it imports no third-party code.
package core

import (
	"fmt"
)

// Kind identifies one of the fifteen closed PortableValue kinds mapped to Go
// by RFC 0016 §4.1.
type Kind uint8

// The fifteen PortableValue kinds. Kinds 0-7 carry the original RFC 0016
// §4.1 eight-kind numbering unchanged; kinds 8-14 are the seven additional
// contract kinds in the language-neutral registry order of
// 配置内容统一处理标准与 Rust 参考实现.md §10 (the Rust PortableValueKind
// order, consema-rs/consema-core/src/value.rs).
const (
	KindObject Kind = iota
	KindArray
	KindString
	KindInteger
	KindDecimal
	KindBinaryFloat64
	KindBoolean
	KindNull
	KindBinaryFloat32
	KindBytes
	KindDate
	KindTime
	KindLocalDateTime
	KindOffsetDateTime
	KindEntryMapping
)

// String returns the kind name: "Object", "Array", "String", "Integer",
// "Decimal", "BinaryFloat64", "Boolean", "Null", "BinaryFloat32", "Bytes",
// "Date", "Time", "LocalDateTime", "OffsetDateTime", or "EntryMapping".
func (k Kind) String() string {
	switch k {
	case KindObject:
		return "Object"
	case KindArray:
		return "Array"
	case KindString:
		return "String"
	case KindInteger:
		return "Integer"
	case KindDecimal:
		return "Decimal"
	case KindBinaryFloat64:
		return "BinaryFloat64"
	case KindBoolean:
		return "Boolean"
	case KindNull:
		return "Null"
	case KindBinaryFloat32:
		return "BinaryFloat32"
	case KindBytes:
		return "Bytes"
	case KindDate:
		return "Date"
	case KindTime:
		return "Time"
	case KindLocalDateTime:
		return "LocalDateTime"
	case KindOffsetDateTime:
		return "OffsetDateTime"
	case KindEntryMapping:
		return "EntryMapping"
	}
	return fmt.Sprintf("Kind(%d)", uint8(k))
}

// Value is the closed interface over the fifteen PortableValue kinds (RFC
// 0016 §4.1; 配置内容统一处理标准与 Rust 参考实现.md §10). Only the fifteen
// kinds in this package implement it; the unexported method prevents
// implementations outside the package.
//
// A Value must not be nil, and a typed-nil pointer (*Object, *Array, or
// *EntryMapping) is invalid input to the codec. Equal and Hash are total:
// they handle nil values without panicking.
type Value interface {
	// Kind reports the closed kind of the value.
	Kind() Kind
	isValue()
}

// Null is the singleton Null kind. All Null values are equal and encode to
// the same PVCE/1 bytes.
type Null struct{}

// NullValue returns the Null singleton as a Value.
func NullValue() Value { return Null{} }

// Kind implements Value.
func (Null) Kind() Kind { return KindNull }

func (Null) isValue() {}

// Boolean is a two-valued PortableValue Boolean.
type Boolean bool

// Kind implements Value.
func (Boolean) Kind() Kind { return KindBoolean }

func (Boolean) isValue() {}

// String is a PortableValue string: an immutable Unicode scalar sequence
// (valid UTF-8 when encoded).
type String string

// Kind implements Value.
func (String) Kind() Kind { return KindString }

func (String) isValue() {}

// Entry is one ordered object entry: a unique key and its value.
type Entry struct {
	// Key is the object key.
	Key string
	// Value is the entry value; it must not be nil.
	Value Value
}

// Object is an ordered unique-key object (RFC 0016 §4.1; RFC 0002 object
// contract). Entry order is a language-neutral fact: Equal, Hash, and the
// PVCE/1 codec all depend on it. Completed objects are logically immutable.
type Object struct {
	entries []Entry
}

// NewObject constructs an object from the given entries in order, rejecting
// duplicate keys. It copies the entries and never retains the caller's
// slice.
func NewObject(entries ...Entry) (*Object, error) {
	builder := NewObjectBuilder()
	for _, entry := range entries {
		if err := builder.Insert(entry.Key, entry.Value); err != nil {
			return nil, err
		}
	}
	return builder.Build(), nil
}

// Len reports the number of entries.
func (o *Object) Len() int { return len(o.entries) }

// Entries returns a copy of the ordered entries.
func (o *Object) Entries() []Entry {
	return append([]Entry(nil), o.entries...)
}

// Get returns the value stored under key, if present. Equal, Hash, and
// encoding always use the stored entry order.
func (o *Object) Get(key string) (Value, bool) {
	for _, entry := range o.entries {
		if entry.Key == key {
			return entry.Value, true
		}
	}
	return nil, false
}

// Kind implements Value.
func (o *Object) Kind() Kind { return KindObject }

func (o *Object) isValue() {}

// ObjectBuilder incrementally constructs an Object, rejecting duplicate keys
// at construction time (the RFC 0002 object contract, mirroring the Rust
// ObjectBuilder uniqueness invariant).
type ObjectBuilder struct {
	entries []Entry
	keys    map[string]struct{}
}

// NewObjectBuilder returns an empty builder.
func NewObjectBuilder() *ObjectBuilder {
	return &ObjectBuilder{keys: make(map[string]struct{})}
}

// Insert appends one entry, returning a *DuplicateKeyError if key is already
// present. value must not be nil; a nil value returns a *PVCEError with
// ErrInvalidValue.
func (b *ObjectBuilder) Insert(key string, value Value) error {
	if value == nil {
		return &PVCEError{Kind: ErrInvalidValue}
	}
	if b.keys == nil {
		b.keys = make(map[string]struct{})
	}
	if _, exists := b.keys[key]; exists {
		return &DuplicateKeyError{Key: key}
	}
	b.keys[key] = struct{}{}
	b.entries = append(b.entries, Entry{Key: key, Value: value})
	return nil
}

// Len reports the number of entries inserted so far.
func (b *ObjectBuilder) Len() int { return len(b.entries) }

// Build returns the completed object. The builder must not be used after
// Build.
func (b *ObjectBuilder) Build() *Object {
	return &Object{entries: append([]Entry(nil), b.entries...)}
}

// Array is an ordered value sequence (RFC 0016 §4.1). Item order is a
// language-neutral fact: Equal, Hash, and the PVCE/1 codec all depend on it.
// Completed arrays are logically immutable.
type Array struct {
	items []Value
}

// NewArray constructs an array with the given items in order. Each item must
// not be nil; a nil item panics.
func NewArray(items ...Value) *Array {
	copied := make([]Value, len(items))
	for i, item := range items {
		if item == nil {
			panic("core: NewArray: nil Value item")
		}
		copied[i] = item
	}
	return &Array{items: copied}
}

// Len reports the number of items.
func (a *Array) Len() int { return len(a.items) }

// Items returns a copy of the ordered items.
func (a *Array) Items() []Value {
	return append([]Value(nil), a.items...)
}

// At returns the item at index i.
func (a *Array) At(i int) Value { return a.items[i] }

// Kind implements Value.
func (a *Array) Kind() Kind { return KindArray }

func (a *Array) isValue() {}

// Bytes is a PortableValue octet sequence (配置内容统一处理标准与 Rust 参考实现.md
// §10.5; the Rust Bytes kind, consema-rs/consema-core/src/value.rs). No
// UTF-8, base64, or hex interpretation is ever implied; Bytes and String are
// always different kinds. Completed byte slices are logically immutable:
// NewBytes copies, and Content returns a copy.
type Bytes []byte

// NewBytes wraps a copy of the octet sequence.
func NewBytes(value []byte) Bytes {
	return Bytes(append([]byte(nil), value...))
}

// Content returns a copy of the octet sequence.
func (b Bytes) Content() []byte {
	return append([]byte(nil), b...)
}

// Kind implements Value.
func (Bytes) Kind() Kind { return KindBytes }

func (Bytes) isValue() {}

// EntryMappingEntry is one ordered entry-mapping association with arbitrary
// PortableValue key and value (配置内容统一处理标准与 Rust 参考实现.md §10.9;
// the Rust EntryMappingEntry, consema-rs/consema-core/src/value.rs).
// Duplicate associations and association order are value semantics.
type EntryMappingEntry struct {
	// Key is the arbitrary key value; it must not be nil.
	Key Value
	// Value is the association value; it must not be nil.
	Value Value
}

// EntryMapping is an ordered arbitrary-key mapping (RFC 0016 §4.1;
// 配置内容统一处理标准与 Rust 参考实现.md §10.9). Keys may be any
// PortableValue and may repeat; entry order and duplicates are language-
// neutral facts that Equal, Hash, and the PVCE/1 codec all depend on.
// Completed entry mappings are logically immutable.
type EntryMapping struct {
	entries []EntryMappingEntry
}

// NewEntryMapping constructs an entry mapping from the given associations in
// order. It copies the entries and never retains the caller's slice; a nil
// key or value returns a *PVCEError with ErrInvalidValue.
func NewEntryMapping(entries ...EntryMappingEntry) (*EntryMapping, error) {
	builder := NewEntryMappingBuilder()
	for _, entry := range entries {
		if err := builder.Push(entry.Key, entry.Value); err != nil {
			return nil, err
		}
	}
	return builder.Build(), nil
}

// Len reports the number of associations.
func (m *EntryMapping) Len() int { return len(m.entries) }

// Entries returns a copy of the ordered associations.
func (m *EntryMapping) Entries() []EntryMappingEntry {
	return append([]EntryMappingEntry(nil), m.entries...)
}

// Kind implements Value.
func (m *EntryMapping) Kind() Kind { return KindEntryMapping }

func (m *EntryMapping) isValue() {}

// EntryMappingBuilder incrementally constructs an EntryMapping. Unlike the
// ObjectBuilder there is no deduplication: arbitrary keys may repeat (the
// Rust EntryMappingBuilder::push semantics, consema-rs/consema-core/src/value.rs:
// 973-978).
type EntryMappingBuilder struct {
	entries []EntryMappingEntry
}

// NewEntryMappingBuilder returns an empty builder.
func NewEntryMappingBuilder() *EntryMappingBuilder {
	return &EntryMappingBuilder{}
}

// Push appends one association. key and value must not be nil; a nil value
// returns a *PVCEError with ErrInvalidValue.
func (b *EntryMappingBuilder) Push(key, value Value) error {
	if key == nil || value == nil {
		return &PVCEError{Kind: ErrInvalidValue}
	}
	b.entries = append(b.entries, EntryMappingEntry{Key: key, Value: value})
	return nil
}

// Len reports the number of associations inserted so far.
func (b *EntryMappingBuilder) Len() int { return len(b.entries) }

// Build returns the completed entry mapping. The builder must not be used
// after Build.
func (b *EntryMappingBuilder) Build() *EntryMapping {
	return &EntryMapping{entries: append([]EntryMappingEntry(nil), b.entries...)}
}
