package core

import (
	"bytes"
	"hash/fnv"
)

// Equal reports strict PortableValue equality (RFC 0016 §4.1): kind identity
// plus canonical content equality. Objects compare entry-by-entry in stored
// order (keys and values); entry mappings compare association-by-association
// in stored order (keys and values, duplicates included); arrays compare
// item-by-item in stored order. Equal(nil, nil) is true; Equal(nil, x) is
// false for any non-nil x; two typed-nil pointers of the same kind compare
// equal to each other.
//
// Equal is total: it never panics and never silently accepts an unknown kind
// (the closed Value interface cannot carry one).
func Equal(a, b Value) bool {
	if a == nil || b == nil {
		return a == b
	}
	switch a := a.(type) {
	case Null:
		_, ok := b.(Null)
		return ok
	case Boolean:
		other, ok := b.(Boolean)
		return ok && a == other
	case String:
		other, ok := b.(String)
		return ok && a == other
	case Integer:
		other, ok := b.(Integer)
		return ok && a.safeValue().Cmp(other.safeValue()) == 0
	case Decimal:
		other, ok := b.(Decimal)
		return ok &&
			a.safeCoefficient().Cmp(other.safeCoefficient()) == 0 &&
			a.safeExponent().Cmp(other.safeExponent()) == 0
	case BinaryFloat32:
		other, ok := b.(BinaryFloat32)
		return ok && a == other
	case BinaryFloat64:
		other, ok := b.(BinaryFloat64)
		return ok && a == other
	case Bytes:
		other, ok := b.(Bytes)
		return ok && bytes.Equal(a, other)
	case Date:
		other, ok := b.(Date)
		return ok &&
			a.year.safeValue().Cmp(other.year.safeValue()) == 0 &&
			a.month == other.month &&
			a.day == other.day
	case Time:
		other, ok := b.(Time)
		return ok &&
			a.hour == other.hour &&
			a.minute == other.minute &&
			a.second == other.second &&
			Equal(a.fractionalSecond, other.fractionalSecond)
	case LocalDateTime:
		other, ok := b.(LocalDateTime)
		return ok && Equal(a.date, other.date) && Equal(a.time, other.time)
	case OffsetDateTime:
		other, ok := b.(OffsetDateTime)
		return ok && Equal(a.local, other.local) && a.offsetSeconds == other.offsetSeconds
	case *EntryMapping:
		other, ok := b.(*EntryMapping)
		if !ok {
			return false
		}
		if a == nil || other == nil {
			return a == other
		}
		if len(a.entries) != len(other.entries) {
			return false
		}
		for i := range a.entries {
			if !Equal(a.entries[i].Key, other.entries[i].Key) ||
				!Equal(a.entries[i].Value, other.entries[i].Value) {
				return false
			}
		}
		return true
	case *Array:
		other, ok := b.(*Array)
		if !ok {
			return false
		}
		if a == nil || other == nil {
			return a == other
		}
		if len(a.items) != len(other.items) {
			return false
		}
		for i := range a.items {
			if !Equal(a.items[i], other.items[i]) {
				return false
			}
		}
		return true
	case *Object:
		other, ok := b.(*Object)
		if !ok {
			return false
		}
		if a == nil || other == nil {
			return a == other
		}
		if len(a.entries) != len(other.entries) {
			return false
		}
		for i := range a.entries {
			if a.entries[i].Key != other.entries[i].Key ||
				!Equal(a.entries[i].Value, other.entries[i].Value) {
				return false
			}
		}
		return true
	}
	return false
}

// Hash returns a deterministic 64-bit hash consistent with Equal: equal
// values always hash equal, and the hash is order-dependent (objects and
// arrays hash by ordered content). It is defined as FNV-1a over the
// canonical PVCE/1 encoding of v, so Equal(a, b) holds exactly when the
// encoded bytes of a and b are identical (see EncodePVCE). Hash(nil) is 0.
func Hash(v Value) uint64 {
	bytes, err := EncodePVCE(v)
	if err != nil {
		return 0
	}
	hasher := fnv.New64a()
	_, _ = hasher.Write(bytes)
	return hasher.Sum64()
}
