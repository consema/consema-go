package plist

// This file implements the shared representation-independent native plist
// value model (consema-plist native.rs; RFC 0013 §6). Values live in an
// arena: containers refer to their children by index, so shared identity
// from the binary object table is preserved — one source object referenced
// by several arrays or dictionaries is one native node with multiple owners.
// The arena is acyclic; cycles are rejected at formation and again by build.
//
// Equality is strict and content-based: scalars compare exactly (a real
// compares its exact bit pattern, so NaN payloads and signed zero are
// distinct), and two documents compare equal when their reachable value
// graphs are equal, independent of arena indices, sharing patterns, or
// unreachable objects. This is the equality the materialization reparse
// closure (RFC 0013 §10.3) and the cross-representation round trip (RFC
// 0013 §7) use.

import (
	"math"
	"strings"
	"unicode/utf16"
)

// plistEpochOffsetUnix is the exact seconds between the Unix epoch
// (`1970-01-01T00:00:00Z`) and the plist epoch (`2001-01-01T00:00:00Z`),
// the origin of every PlistDate value (RFC 0013 §5.5).
const plistEpochOffsetUnix = 978_307_200.0

// PlistString is exact plist string content as immutable UTF-16 code units
// with a bounded surrogate-pairing status (consema-plist native.rs
// PlistString; RFC 0013 §6). XML sources can only produce well-formed
// Unicode; binary sources may produce unpaired surrogates, which are
// preserved exactly and block conversion to XML and ordinary Unicode
// projection.
type PlistString struct {
	codeUnits []uint16
	status    PlistStringStatus
}

// NewPlistStringFromCodeUnits creates exact string content and computes the
// surrogate well-formedness status.
func NewPlistStringFromCodeUnits(codeUnits []uint16) PlistString {
	units := append([]uint16(nil), codeUnits...)
	return PlistString{codeUnits: units, status: classifyString(units)}
}

// NewPlistStringFromUnicode converts one valid Unicode scalar string to its
// exact UTF-16 units.
func NewPlistStringFromUnicode(value string) PlistString {
	return NewPlistStringFromCodeUnits(utf16.Encode([]rune(value)))
}

// CodeUnits returns the exact ordered UTF-16 code units. The returned slice
// must not be modified.
func (s PlistString) CodeUnits() []uint16 { return s.codeUnits }

// UTF16BEBytes returns the canonical BOM-free big-endian UTF-16BE bytes.
func (s PlistString) UTF16BEBytes() []byte {
	out := make([]byte, 0, len(s.codeUnits)*2)
	for _, unit := range s.codeUnits {
		out = append(out, byte(unit>>8), byte(unit))
	}
	return out
}

// Status returns the exact surrogate pairing status.
func (s PlistString) Status() PlistStringStatus { return s.status }

// ToUnicode converts only well-formed content to a Unicode string.
func (s PlistString) ToUnicode() (string, error) {
	if s.status == PlistStringUnpairedSurrogate {
		return "", &PlistStringConversionError{}
	}
	return string(utf16.Decode(s.codeUnits)), nil
}

// Equal reports whether two strings hold identical code units.
func (s PlistString) Equal(other PlistString) bool {
	if len(s.codeUnits) != len(other.codeUnits) {
		return false
	}
	for index := range s.codeUnits {
		if s.codeUnits[index] != other.codeUnits[index] {
			return false
		}
	}
	return true
}

// PlistStringConversionError is an exact plist string that cannot enter a
// Unicode-only host string.
type PlistStringConversionError struct{}

// Error implements error.
func (e *PlistStringConversionError) Error() string {
	return "plist string contains an unpaired surrogate"
}

// Code returns the registered invalid-input code (RFC 0016 §6).
func (e *PlistStringConversionError) Code() string { return "core.protocol.invalid-value@1" }

// PlistKey is the string key identity of one dictionary association
// (consema-plist native.rs PlistKey; RFC 0013 §4.4, §5.9). Each physical
// association keeps its own key identity; duplicate keys are preserved as
// ordered native facts.
type PlistKey struct {
	string PlistString
}

// NewPlistKeyFromString creates a key from exact plist string content.
func NewPlistKeyFromString(string PlistString) PlistKey { return PlistKey{string: string} }

// NewPlistKeyFromUnicode creates a key from one valid Unicode scalar string.
func NewPlistKeyFromUnicode(value string) PlistKey {
	return NewPlistKeyFromString(NewPlistStringFromUnicode(value))
}

// String returns the exact key string content.
func (k PlistKey) String() PlistString { return k.string }

// CodeUnits returns the exact ordered UTF-16 code units.
func (k PlistKey) CodeUnits() []uint16 { return k.string.CodeUnits() }

// Status returns the exact surrogate pairing status.
func (k PlistKey) Status() PlistStringStatus { return k.string.Status() }

// ToUnicode converts only well-formed keys to a Unicode string.
func (k PlistKey) ToUnicode() (string, error) { return k.string.ToUnicode() }

// Equal reports whether two keys hold identical code units.
func (k PlistKey) Equal(other PlistKey) bool { return k.string.Equal(other.string) }

// PlistInteger is an exact signed 64-bit plist integer (consema-plist
// native.rs PlistInteger; RFC 0013 §4.5, §5.3, §6).
type PlistInteger struct{ value int64 }

// NewPlistInteger wraps an exact signed 64-bit value.
func NewPlistInteger(value int64) PlistInteger { return PlistInteger{value: value} }

// Value returns the exact value.
func (i PlistInteger) Value() int64 { return i.value }

// PlistReal is an exact IEEE 754 real with its source width fact
// (consema-plist native.rs PlistReal; RFC 0013 §4.6, §5.5). Equality and
// hashing follow the bit pattern, so distinct NaN payloads and signed zero
// are distinct values.
type PlistReal struct {
	bits  uint64
	width RealWidth
}

// NewPlistRealDouble creates a Float64 real from an exact double.
func NewPlistRealDouble(value float64) PlistReal {
	return PlistReal{bits: math.Float64bits(value), width: RealWidthFloat64}
}

// NewPlistRealSingle creates a Float32 real from an exact single.
func NewPlistRealSingle(value float32) PlistReal {
	return PlistReal{bits: uint64(math.Float32bits(value)), width: RealWidthFloat32}
}

// NewPlistRealFromBits creates a real from the exact source-width bit
// pattern; this is the parser path (`0x22`/`0x23` payloads). For Float32
// only the low 32 bits are retained.
func NewPlistRealFromBits(width RealWidth, bits uint64) PlistReal {
	if width == RealWidthFloat32 {
		bits &= 0xFFFF_FFFF
	}
	return PlistReal{bits: bits, width: width}
}

// Bits returns the exact source-width bit pattern.
func (r PlistReal) Bits() uint64 { return r.bits }

// Width returns the source width fact.
func (r PlistReal) Width() RealWidth { return r.width }

// AsFloat64 returns the exact double-converted value (RFC 0013 §5.5).
func (r PlistReal) AsFloat64() float64 {
	if r.width == RealWidthFloat32 {
		return float64(math.Float32frombits(uint32(r.bits)))
	}
	return math.Float64frombits(r.bits)
}

// Equal reports whether two reals carry the identical bit pattern and
// width.
func (r PlistReal) Equal(other PlistReal) bool {
	return r.bits == other.bits && r.width == other.width
}

// PlistBoolean is one plist boolean value (consema-plist native.rs
// PlistBoolean).
type PlistBoolean struct{ value bool }

// NewPlistBoolean creates a boolean value.
func NewPlistBoolean(value bool) PlistBoolean { return PlistBoolean{value: value} }

// Value returns the exact value.
func (b PlistBoolean) Value() bool { return b.value }

// PlistDate is exact double seconds since the plist epoch (consema-plist
// native.rs PlistDate; RFC 0013 §4.7, §5.5). Construction rejects
// non-finite payloads; equality is bit-exact, so signed zero is distinct
// from zero.
type PlistDate struct{ seconds float64 }

// NewPlistDateFromSeconds creates a date from exact seconds since the plist
// epoch; ok is false for non-finite payloads.
func NewPlistDateFromSeconds(seconds float64) (PlistDate, bool) {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return PlistDate{}, false
	}
	return PlistDate{seconds: seconds}, true
}

// Seconds returns the exact double seconds since `2001-01-01T00:00:00Z`.
func (d PlistDate) Seconds() float64 { return d.seconds }

// Equal reports whether two dates hold the identical bit pattern.
func (d PlistDate) Equal(other PlistDate) bool {
	return math.Float64bits(d.seconds) == math.Float64bits(other.seconds)
}

// PlistData is exact plist data bytes (consema-plist native.rs PlistData;
// RFC 0013 §6). Data is exact bytes in the native layer; base64 exists only
// as `plist.xml@1` representation text.
type PlistData struct{ bytes []byte }

// NewPlistDataFromBytes creates data from exact bytes.
func NewPlistDataFromBytes(bytes []byte) PlistData {
	return PlistData{bytes: append([]byte(nil), bytes...)}
}

// Bytes returns the exact bytes. The returned slice must not be modified.
func (d PlistData) Bytes() []byte { return d.bytes }

// Equal reports whether two data values hold identical bytes.
func (d PlistData) Equal(other PlistData) bool {
	if len(d.bytes) != len(other.bytes) {
		return false
	}
	for index := range d.bytes {
		if d.bytes[index] != other.bytes[index] {
			return false
		}
	}
	return true
}

// PlistUID is an unsigned 32-bit UID value (binary profile only)
// (consema-plist native.rs PlistUid; RFC 0013 §5.8, §6). A UID is a value
// whose reference meaning belongs to an application layer such as
// NSKeyedArchiver; Consema preserves the value but never resolves it.
type PlistUID struct{ value uint32 }

// NewPlistUID wraps an exact unsigned 32-bit value.
func NewPlistUID(value uint32) PlistUID { return PlistUID{value: value} }

// Value returns the exact value.
func (u PlistUID) Value() uint32 { return u.value }

// PlistValueRef is an arena reference to one native value node
// (consema-plist native.rs PlistValueRef). A reference is valid only within
// the arena that issued it. The same source object referenced several times
// is the same reference (shared identity).
type PlistValueRef struct{ index int }

// NewPlistValueRefFromIndex creates a reference from an arena-relative
// ordinal. References to not-yet-added nodes are permitted so that the
// binary parser can build containers with forward object-table references;
// build rejects any reference outside the final arena.
func NewPlistValueRefFromIndex(index int) PlistValueRef { return PlistValueRef{index: index} }

// Index returns the arena-relative ordinal.
func (r PlistValueRef) Index() int { return r.index }

// PlistDictEntry is one ordered dictionary association: key identity and
// value reference (consema-plist native.rs PlistDictEntry; RFC 0013 §4.4,
// §5.9).
type PlistDictEntry struct {
	key   PlistKey
	value PlistValueRef
}

// NewPlistDictEntry creates one association.
func NewPlistDictEntry(key PlistKey, value PlistValueRef) PlistDictEntry {
	return PlistDictEntry{key: key, value: value}
}

// Key returns the string key identity.
func (e PlistDictEntry) Key() PlistKey { return e.key }

// Value returns the value reference within the owning arena.
func (e PlistDictEntry) Value() PlistValueRef { return e.value }

// PlistDict is an ordered plist dictionary value (consema-plist native.rs
// PlistDict; RFC 0013 §6). A dictionary preserves physical key/value
// association order and duplicate occurrences.
type PlistDict struct{ entries []PlistDictEntry }

// NewPlistDictFromEntries creates a dictionary from its ordered
// associations.
func NewPlistDictFromEntries(entries []PlistDictEntry) PlistDict {
	return PlistDict{entries: append([]PlistDictEntry(nil), entries...)}
}

// Entries returns the ordered associations. The returned slice must not be
// modified.
func (d PlistDict) Entries() []PlistDictEntry { return d.entries }

// Len returns the number of associations.
func (d PlistDict) Len() int { return len(d.entries) }

// IsEmpty reports whether the dictionary has no associations.
func (d PlistDict) IsEmpty() bool { return len(d.entries) == 0 }

// PositionsOfKey returns the source-ordered positions of every association
// whose key equals `key`.
func (d PlistDict) PositionsOfKey(key PlistKey) []int {
	var positions []int
	for position, entry := range d.entries {
		if entry.key.Equal(key) {
			positions = append(positions, position)
		}
	}
	return positions
}

// PlistArray is an ordered plist array value (consema-plist native.rs
// PlistArray; RFC 0013 §6).
type PlistArray struct{ elements []PlistValueRef }

// NewPlistArrayFromElements creates an array from its ordered element
// references.
func NewPlistArrayFromElements(elements []PlistValueRef) PlistArray {
	return PlistArray{elements: append([]PlistValueRef(nil), elements...)}
}

// Elements returns the ordered element references. The returned slice must
// not be modified.
func (a PlistArray) Elements() []PlistValueRef { return a.elements }

// Len returns the number of elements.
func (a PlistArray) Len() int { return len(a.elements) }

// IsEmpty reports whether the array has no elements.
func (a PlistArray) IsEmpty() bool { return len(a.elements) == 0 }

// PlistValue is one native plist value node (consema-plist native.rs
// PlistValue; RFC 0013 §6). The kind set is closed; UID values are
// binary-only and are never reachable from an XML document.
type PlistValue struct {
	kind    PlistValueKind
	dict    *PlistDict
	array   *PlistArray
	str     PlistString
	integer PlistInteger
	real    PlistReal
	boolean PlistBoolean
	date    PlistDate
	data    PlistData
	uid     PlistUID
}

// NewPlistValueDict creates a dictionary value node.
func NewPlistValueDict(dict PlistDict) PlistValue {
	return PlistValue{kind: PlistValueKindDict, dict: &dict}
}

// NewPlistValueArray creates an array value node.
func NewPlistValueArray(array PlistArray) PlistValue {
	return PlistValue{kind: PlistValueKindArray, array: &array}
}

// NewPlistValueString creates a string value node.
func NewPlistValueString(value PlistString) PlistValue {
	return PlistValue{kind: PlistValueKindString, str: value}
}

// NewPlistValueInteger creates an integer value node.
func NewPlistValueInteger(value PlistInteger) PlistValue {
	return PlistValue{kind: PlistValueKindInteger, integer: value}
}

// NewPlistValueReal creates a real value node.
func NewPlistValueReal(value PlistReal) PlistValue {
	return PlistValue{kind: PlistValueKindReal, real: value}
}

// NewPlistValueBoolean creates a boolean value node.
func NewPlistValueBoolean(value PlistBoolean) PlistValue {
	return PlistValue{kind: PlistValueKindBoolean, boolean: value}
}

// NewPlistValueDate creates a date value node.
func NewPlistValueDate(value PlistDate) PlistValue {
	return PlistValue{kind: PlistValueKindDate, date: value}
}

// NewPlistValueData creates a data value node.
func NewPlistValueData(value PlistData) PlistValue {
	return PlistValue{kind: PlistValueKindData, data: value}
}

// NewPlistValueUID creates a UID value node (binary profile only).
func NewPlistValueUID(value PlistUID) PlistValue {
	return PlistValue{kind: PlistValueKindUid, uid: value}
}

// Kind returns the closed native kind.
func (v PlistValue) Kind() PlistValueKind { return v.kind }

// AsDict returns the dictionary view.
func (v PlistValue) AsDict() (PlistDict, bool) {
	if v.kind == PlistValueKindDict {
		return *v.dict, true
	}
	return PlistDict{}, false
}

// AsArray returns the array view.
func (v PlistValue) AsArray() (PlistArray, bool) {
	if v.kind == PlistValueKindArray {
		return *v.array, true
	}
	return PlistArray{}, false
}

// AsString returns the string view.
func (v PlistValue) AsString() (PlistString, bool) {
	if v.kind == PlistValueKindString {
		return v.str, true
	}
	return PlistString{}, false
}

// AsInteger returns the integer view.
func (v PlistValue) AsInteger() (PlistInteger, bool) {
	if v.kind == PlistValueKindInteger {
		return v.integer, true
	}
	return PlistInteger{}, false
}

// AsReal returns the real view.
func (v PlistValue) AsReal() (PlistReal, bool) {
	if v.kind == PlistValueKindReal {
		return v.real, true
	}
	return PlistReal{}, false
}

// AsBoolean returns the boolean view.
func (v PlistValue) AsBoolean() (PlistBoolean, bool) {
	if v.kind == PlistValueKindBoolean {
		return v.boolean, true
	}
	return PlistBoolean{}, false
}

// AsDate returns the date view.
func (v PlistValue) AsDate() (PlistDate, bool) {
	if v.kind == PlistValueKindDate {
		return v.date, true
	}
	return PlistDate{}, false
}

// AsData returns the data view.
func (v PlistValue) AsData() (PlistData, bool) {
	if v.kind == PlistValueKindData {
		return v.data, true
	}
	return PlistData{}, false
}

// AsUID returns the UID view.
func (v PlistValue) AsUID() (PlistUID, bool) {
	if v.kind == PlistValueKindUid {
		return v.uid, true
	}
	return PlistUID{}, false
}

// references returns the ordered direct child references of this node
// (dictionary values, then array elements).
func (v PlistValue) references() []PlistValueRef {
	switch v.kind {
	case PlistValueKindDict:
		refs := make([]PlistValueRef, 0, len(v.dict.entries))
		for _, entry := range v.dict.entries {
			refs = append(refs, entry.value)
		}
		return refs
	case PlistValueKindArray:
		return append([]PlistValueRef(nil), v.array.elements...)
	}
	return nil
}

// PlistArenaLimits bounds one native arena (consema-plist native.rs
// PlistArenaLimits).
type PlistArenaLimits struct {
	// MaxObjects bounds the native value nodes in one arena.
	MaxObjects int
	// MaxContainerDepth bounds the container nesting depth of any node.
	MaxContainerDepth int
}

// PlistArenaErrorKind classifies a native arena validation failure
// (consema-plist native.rs PlistArenaError).
type PlistArenaErrorKind uint8

// The closed arena failure classes.
const (
	// PlistArenaErrorObjectLimitExceeded: the node limit was reached.
	PlistArenaErrorObjectLimitExceeded PlistArenaErrorKind = iota
	// PlistArenaErrorReferenceOutOfBounds: a reference does not index a node.
	PlistArenaErrorReferenceOutOfBounds
	// PlistArenaErrorCycleDetected: the arena contains a reference cycle.
	PlistArenaErrorCycleDetected
	// PlistArenaErrorContainerDepthLimitExceeded: a container nests deeper
	// than the configured limit.
	PlistArenaErrorContainerDepthLimitExceeded
)

// PlistArenaError is a typed native arena validation failure.
type PlistArenaError struct {
	// Kind identifies the failure.
	Kind PlistArenaErrorKind
	// Node is the offending node of CycleDetected or
	// ContainerDepthLimitExceeded.
	Node PlistValueRef
	// Reference is the invalid reference of ReferenceOutOfBounds.
	Reference PlistValueRef
	// NodeCount is the arena size of ReferenceOutOfBounds.
	NodeCount int
	// Limit is the configured maximum of ObjectLimitExceeded or
	// ContainerDepthLimitExceeded.
	Limit int
}

// Error implements error.
func (e *PlistArenaError) Error() string {
	switch e.Kind {
	case PlistArenaErrorObjectLimitExceeded:
		return "plist arena object limit of " + itoa(e.Limit) + " exceeded"
	case PlistArenaErrorReferenceOutOfBounds:
		return "plist arena reference out of bounds"
	case PlistArenaErrorCycleDetected:
		return "plist arena contains a reference cycle"
	case PlistArenaErrorContainerDepthLimitExceeded:
		return "plist arena container depth exceeds the limit"
	}
	return "plist arena failure"
}

// Code returns the registered invalid-input code (RFC 0016 §6).
func (e *PlistArenaError) Code() string { return "core.protocol.invalid-value@1" }

// PlistDocument is the immutable native plist value arena
// (consema-plist native.rs PlistDocument; RFC 0013 §6). The arena owns every
// native value node of one document (in binary object-table order) and the
// root reference. Nodes may be referenced by several containers, preserving
// shared identity from the binary object table. Objects not reachable from
// the root may exist in the arena (binary object-table orphans); they remain
// structural facts and are excluded from structural equality.
type PlistDocument struct {
	nodes       []PlistValue
	root        PlistValueRef
	arenaLimits PlistArenaLimits
}

// Root returns the root value reference.
func (d *PlistDocument) Root() PlistValueRef { return d.root }

// RootValue returns the root value; always in bounds because build
// validated the arena.
func (d *PlistDocument) RootValue() PlistValue {
	return d.nodes[d.root.index]
}

// Get resolves one reference within this arena.
func (d *PlistDocument) Get(reference PlistValueRef) (PlistValue, bool) {
	if reference.index < 0 || reference.index >= len(d.nodes) {
		return PlistValue{}, false
	}
	return d.nodes[reference.index], true
}

// NodeCount returns the number of nodes in the arena, including
// unreachable objects.
func (d *PlistDocument) NodeCount() int { return len(d.nodes) }

// ArenaLimits returns the limits used when the document was built.
func (d *PlistDocument) ArenaLimits() PlistArenaLimits { return d.arenaLimits }

// Equal compares the reachable value graphs structurally and content-based
// (consema-plist native.rs PlistDocument PartialEq). Sharing patterns,
// arena indices, and unreachable objects do not matter; the arena is
// acyclic, so each node pair is compared at most once even under heavy
// sharing.
func (d *PlistDocument) Equal(other *PlistDocument) bool {
	if d == other {
		return true
	}
	if d == nil || other == nil || len(d.nodes) == 0 || len(other.nodes) == 0 {
		return d == other
	}
	if d.root == other.root && sameNodes(d.nodes, other.nodes) {
		return true
	}
	type pair struct{ left, right int }
	memo := make(map[pair]bool)
	stack := []pair{{d.root.index, other.root.index}}
	for len(stack) > 0 {
		item := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if memo[item] {
			continue
		}
		memo[item] = true
		left := d.nodes[item.left]
		right := other.nodes[item.right]
		switch left.kind {
		case PlistValueKindDict:
			if right.kind != PlistValueKindDict || len(left.dict.entries) != len(right.dict.entries) {
				return false
			}
			for index := range left.dict.entries {
				if !left.dict.entries[index].key.Equal(right.dict.entries[index].key) {
					return false
				}
				stack = append(stack, pair{
					left.dict.entries[index].value.index, right.dict.entries[index].value.index})
			}
		case PlistValueKindArray:
			if right.kind != PlistValueKindArray || len(left.array.elements) != len(right.array.elements) {
				return false
			}
			for index := range left.array.elements {
				stack = append(stack, pair{left.array.elements[index].index, right.array.elements[index].index})
			}
		case PlistValueKindString:
			if right.kind != PlistValueKindString || !left.str.Equal(right.str) {
				return false
			}
		case PlistValueKindInteger:
			if right.kind != PlistValueKindInteger || left.integer.value != right.integer.value {
				return false
			}
		case PlistValueKindReal:
			if right.kind != PlistValueKindReal || !left.real.Equal(right.real) {
				return false
			}
		case PlistValueKindBoolean:
			if right.kind != PlistValueKindBoolean || left.boolean.value != right.boolean.value {
				return false
			}
		case PlistValueKindDate:
			if right.kind != PlistValueKindDate || !left.date.Equal(right.date) {
				return false
			}
		case PlistValueKindData:
			if right.kind != PlistValueKindData || !left.data.Equal(right.data) {
				return false
			}
		case PlistValueKindUid:
			if right.kind != PlistValueKindUid || left.uid.value != right.uid.value {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// sameNodes reports whether the two node slices are identical storage.
func sameNodes(left, right []PlistValue) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !plistValueBitEqual(left[index], right[index]) {
			return false
		}
	}
	return true
}

// plistValueBitEqual compares two nodes by their stored payload fields
// (used only as the fast path of structural equality, where a shared arena
// is the common case; the memoized graph walk remains the authority).
func plistValueBitEqual(left, right PlistValue) bool {
	return left.kind == right.kind &&
		left.integer == right.integer && left.real == right.real &&
		left.boolean == right.boolean && left.uid == right.uid
}

// PlistDocumentBuilder builds one immutable PlistDocument arena
// (consema-plist native.rs PlistDocumentBuilder). The binary parser adds
// nodes in object-table order so that arena indices equal object indices;
// references may point forward. Build validates reference bounds,
// acyclicity, and the container depth limit, all iteratively.
type PlistDocumentBuilder struct {
	nodes  []PlistValue
	limits PlistArenaLimits
}

// NewPlistDocumentBuilder starts a builder with the default arena limits.
func NewPlistDocumentBuilder() PlistDocumentBuilder {
	return NewPlistDocumentBuilderWithLimits(PlistArenaLimits{
		MaxObjects: 1_000_000, MaxContainerDepth: 256})
}

// NewPlistDocumentBuilderWithLimits starts a builder with explicit arena
// limits.
func NewPlistDocumentBuilderWithLimits(limits PlistArenaLimits) PlistDocumentBuilder {
	return PlistDocumentBuilder{limits: limits}
}

// Limits returns the arena limits applied by this builder.
func (b *PlistDocumentBuilder) Limits() PlistArenaLimits { return b.limits }

// NodeCount returns the number of nodes added so far.
func (b *PlistDocumentBuilder) NodeCount() int { return len(b.nodes) }

// Add adds one node and returns its arena reference.
func (b *PlistDocumentBuilder) Add(value PlistValue) (PlistValueRef, error) {
	if len(b.nodes) >= b.limits.MaxObjects {
		return PlistValueRef{}, &PlistArenaError{
			Kind: PlistArenaErrorObjectLimitExceeded, Limit: b.limits.MaxObjects}
	}
	b.nodes = append(b.nodes, value)
	return PlistValueRef{index: len(b.nodes) - 1}, nil
}

// Build validates the arena and freezes it into one immutable document.
// The root must be in bounds, every reference must index an existing node,
// the reference graph must be acyclic, and no container may be nested
// deeper than `max_container_depth`.
func (b PlistDocumentBuilder) Build(root PlistValueRef) (*PlistDocument, error) {
	nodeCount := len(b.nodes)
	if root.index < 0 || root.index >= nodeCount {
		return nil, &PlistArenaError{
			Kind: PlistArenaErrorReferenceOutOfBounds, Reference: root, NodeCount: nodeCount}
	}
	indegree := make([]int, nodeCount)
	for _, node := range b.nodes {
		for _, reference := range node.references() {
			if reference.index < 0 || reference.index >= nodeCount {
				return nil, &PlistArenaError{
					Kind: PlistArenaErrorReferenceOutOfBounds, Reference: reference, NodeCount: nodeCount}
			}
			indegree[reference.index]++
		}
	}
	// Kahn's algorithm: parents before children, leaving cyclic nodes
	// unprocessed.
	queue := make([]int, 0, nodeCount)
	for index, degree := range indegree {
		if degree == 0 {
			queue = append(queue, index)
		}
	}
	order := make([]int, 0, nodeCount)
	for len(queue) > 0 {
		index := queue[0]
		queue = queue[1:]
		order = append(order, index)
		for _, reference := range b.nodes[index].references() {
			indegree[reference.index]--
			if indegree[reference.index] == 0 {
				queue = append(queue, reference.index)
			}
		}
	}
	if len(order) != nodeCount {
		processed := make([]bool, nodeCount)
		for _, index := range order {
			processed[index] = true
		}
		node := 0
		for node < nodeCount && processed[node] {
			node++
		}
		return nil, &PlistArenaError{Kind: PlistArenaErrorCycleDetected, Node: PlistValueRef{index: node}}
	}
	// Container depth over the reversed topological order, so every child's
	// depth is known before its parent.
	depth := make([]int, nodeCount)
	for position := len(order) - 1; position >= 0; position-- {
		index := order[position]
		node := b.nodes[index]
		if node.kind == PlistValueKindDict || node.kind == PlistValueKindArray {
			childDepth := 0
			for _, reference := range node.references() {
				if depth[reference.index] > childDepth {
					childDepth = depth[reference.index]
				}
			}
			containerDepth := childDepth + 1
			if containerDepth > b.limits.MaxContainerDepth {
				return nil, &PlistArenaError{
					Kind: PlistArenaErrorContainerDepthLimitExceeded,
					Node: PlistValueRef{index: index}, Limit: b.limits.MaxContainerDepth}
			}
			depth[index] = containerDepth
		}
	}
	return &PlistDocument{
		nodes:       append([]PlistValue(nil), b.nodes...),
		root:        root,
		arenaLimits: b.limits,
	}, nil
}

// classifyString computes the surrogate pairing status of one code-unit
// sequence (consema-plist native.rs classify_string).
func classifyString(units []uint16) PlistStringStatus {
	index := 0
	for index < len(units) {
		unit := units[index]
		switch {
		case unit >= 0xD800 && unit <= 0xDBFF:
			if index+1 < len(units) {
				next := units[index+1]
				if next >= 0xDC00 && next <= 0xDFFF {
					index += 2
					continue
				}
			}
			return PlistStringUnpairedSurrogate
		case unit >= 0xDC00 && unit <= 0xDFFF:
			return PlistStringUnpairedSurrogate
		default:
			index++
		}
	}
	return PlistStringWellFormedUnicode
}

// itoa formats one non-negative ordinal (shared helper).
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	negative := value < 0
	magnitude := value
	if negative {
		magnitude = -magnitude
	}
	for magnitude > 0 {
		index--
		digits[index] = byte('0' + magnitude%10)
		magnitude /= 10
	}
	if negative {
		index--
		digits[index] = '-'
	}
	return string(digits[index:])
}

// stringsJoin is the small shared join helper for diagnostic argument
// rendering.
func stringsJoin(parts []string, separator string) string {
	return strings.Join(parts, separator)
}
