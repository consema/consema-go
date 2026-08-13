package core

import (
	"bytes"
	"math/big"
	"testing"
)

// TestEqualMatrix pins strict equality (RFC 0016 §4.1): kind identity plus
// canonical content equality, order-sensitive for objects and arrays.
func TestEqualMatrix(t *testing.T) {
	cases := []struct {
		name string
		a, b Value
		want bool
	}{
		{"null", Null{}, Null{}, true},
		{"boolean false", Boolean(false), Boolean(false), true},
		{"boolean true", Boolean(true), Boolean(true), true},
		{"boolean differs", Boolean(false), Boolean(true), false},
		{"string", String("x"), String("x"), true},
		{"string differs", String("x"), String("y"), false},
		{"string empty vs null", String(""), Null{}, false},
		{"integer by value", NewInteger(big.NewInt(42)), NewInteger(big.NewInt(42)), true},
		{"integer negative", NewInteger(big.NewInt(-42)), NewInteger(big.NewInt(-42)), true},
		{"integer differs", NewInteger(big.NewInt(42)), NewInteger(big.NewInt(-42)), false},
		{"integer zero", NewInteger(big.NewInt(0)), NewInteger(nil), true},
		{"decimal canonical equal", NewDecimal(big.NewInt(1), big.NewInt(0)), NewDecimal(big.NewInt(10), big.NewInt(-1)), true},
		{"decimal canonical equal big", NewDecimal(big.NewInt(12300), big.NewInt(-2)), NewDecimal(big.NewInt(123), big.NewInt(0)), true},
		{"decimal differs", NewDecimal(big.NewInt(1), big.NewInt(0)), NewDecimal(big.NewInt(2), big.NewInt(0)), false},
		{"decimal zero exponent normalized", NewDecimal(big.NewInt(0), big.NewInt(5)), NewDecimal(big.NewInt(0), big.NewInt(-5)), true},
		{"binary float bits", NewBinaryFloat64(0x8000000000000000), NewBinaryFloat64(0x8000000000000000), true},
		{"binary float differs", NewBinaryFloat64(0), NewBinaryFloat64(1), false},
		{"array same order", NewArray(String("a"), Null{}), NewArray(String("a"), Null{}), true},
		{"array reordered", NewArray(String("a"), Null{}), NewArray(Null{}, String("a")), false},
		{"array different length", NewArray(String("a")), NewArray(String("a"), Null{}), false},
		{"object same order", mustObject(t, Entry{"a", Null{}}, Entry{"b", String("x")}), mustObject(t, Entry{"a", Null{}}, Entry{"b", String("x")}), true},
		{"object reordered", mustObject(t, Entry{"a", Null{}}, Entry{"b", String("x")}), mustObject(t, Entry{"b", String("x")}, Entry{"a", Null{}}), false},
		{"object key differs", mustObject(t, Entry{"a", Null{}}), mustObject(t, Entry{"b", Null{}}), false},
		{"nested", NewArray(mustObject(t, Entry{"a", NewArray(Boolean(true))})), NewArray(mustObject(t, Entry{"a", NewArray(Boolean(true))})), true},
		{"nested differs", NewArray(mustObject(t, Entry{"a", NewArray(Boolean(true))})), NewArray(mustObject(t, Entry{"a", NewArray(Boolean(false))})), false},
	}
	for _, c := range cases {
		if got := Equal(c.a, c.b); got != c.want {
			t.Errorf("%s: Equal = %v, want %v", c.name, got, c.want)
		}
		// Strict equality is symmetric.
		if got := Equal(c.b, c.a); got != c.want {
			t.Errorf("%s (symmetric): Equal = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestEqualKindIdentity pins "kind identity plus canonical form": values of
// different kinds are never equal even when their representations coincide
// (Boolean(false) vs Null, String("") vs Null, Integer(0) vs Decimal(0),
// BinaryFloat32(0) vs BinaryFloat64(0), Bytes([]) vs String("")).
func TestEqualKindIdentity(t *testing.T) {
	values := []Value{
		Null{},
		Boolean(false),
		String(""),
		NewInteger(big.NewInt(0)),
		NewDecimal(big.NewInt(0), big.NewInt(0)),
		NewBinaryFloat32(0),
		NewBinaryFloat64(0),
		NewBytes(nil),
		NewArray(),
		&Object{},
		&EntryMapping{},
	}
	for i, a := range values {
		for j, b := range values {
			got := Equal(a, b)
			want := i == j
			if got != want {
				t.Errorf("Equal(%v, %v) = %v, want %v (kind identity)", a, b, got, want)
			}
		}
	}
}

// TestEqualNilHandling pins the total behavior of Equal on nil values.
func TestEqualNilHandling(t *testing.T) {
	if !Equal(nil, nil) {
		t.Error("Equal(nil, nil) = false, want true")
	}
	if Equal(nil, Null{}) || Equal(Null{}, nil) {
		t.Error("Equal with one nil side must be false")
	}
	if !Equal((*Object)(nil), (*Object)(nil)) {
		t.Error("Equal(typed-nil Object, typed-nil Object) = false, want true")
	}
	if Equal((*Object)(nil), (*Array)(nil)) {
		t.Error("typed-nil Object and Array must not be equal")
	}
	if Equal((*Object)(nil), &Object{}) {
		t.Error("typed-nil Object and empty Object must not be equal")
	}
}

// TestHashDeterminism pins deterministic hashing: the same value hashes the
// same across calls and across separately constructed instances.
func TestHashDeterminism(t *testing.T) {
	values := []Value{
		Null{},
		Boolean(true),
		String("中"),
		NewInteger(big.NewInt(-256)),
		NewDecimal(big.NewInt(12300), big.NewInt(-2)),
		NewBinaryFloat64(0x8000000000000000),
		NewArray(String("a"), NewArray(Null{})),
		mustObject(t, Entry{"a", NewInteger(big.NewInt(1))}, Entry{"b", NewArray(Boolean(false))}),
	}
	for _, v := range values {
		first := Hash(v)
		for i := 0; i < 5; i++ {
			if got := Hash(v); got != first {
				t.Errorf("Hash(%v) not deterministic: %#x then %#x", v, first, got)
			}
		}
	}
}

// TestHashConsistencyWithEqual pins the RFC 0016 §4.1 contract: Equal values
// always hash equal.
func TestHashConsistencyWithEqual(t *testing.T) {
	pairs := [][2]Value{
		{NewInteger(big.NewInt(42)), NewInteger(big.NewInt(42))},
		{NewDecimal(big.NewInt(1), big.NewInt(0)), NewDecimal(big.NewInt(10), big.NewInt(-1))},
		{NewArray(String("a"), Null{}), NewArray(String("a"), Null{})},
		{mustObject(t, Entry{"a", Null{}}, Entry{"b", String("x")}),
			mustObject(t, Entry{"a", Null{}}, Entry{"b", String("x")})},
		{Null{}, Null{}},
	}
	for _, pair := range pairs {
		if !Equal(pair[0], pair[1]) {
			t.Fatalf("test pair not Equal: %v vs %v", pair[0], pair[1])
		}
		if Hash(pair[0]) != Hash(pair[1]) {
			t.Errorf("Equal values hash differently: %v vs %v", pair[0], pair[1])
		}
	}
	if Hash(nil) != 0 {
		t.Error("Hash(nil) != 0")
	}
}

// TestEqualIsByteBijection pins the strongest form of the RFC 0016 §4.1
// equality contract: Equal(a, b) holds exactly when the canonical PVCE/1
// encodings of a and b are byte-identical (which is what Hash is defined
// over). Object and array order therefore affects both equality and bytes.
func TestEqualIsByteBijection(t *testing.T) {
	values := []Value{
		Null{},
		Boolean(false),
		Boolean(true),
		String(""),
		String("é"),
		String("中"),
		NewInteger(big.NewInt(0)),
		NewInteger(big.NewInt(1)),
		NewInteger(big.NewInt(-256)),
		NewInteger(mustBigInt(t, "123456789012345678901234567890")),
		NewDecimal(big.NewInt(1), big.NewInt(0)),
		NewDecimal(big.NewInt(10), big.NewInt(-1)),
		NewDecimal(big.NewInt(-12300), big.NewInt(2)),
		NewDecimal(big.NewInt(0), big.NewInt(7)),
		NewBinaryFloat64(0),
		NewBinaryFloat64(0x8000000000000000),
		NewBinaryFloat64(0x7ff8000000000001),
		NewArray(),
		NewArray(Null{}),
		NewArray(String("a"), Null{}),
		NewArray(Null{}, String("a")),
		NewArray(NewArray(NewArray(Null{}))),
		mustObject(t, Entry{"a", Null{}}),
		mustObject(t, Entry{"a", NewInteger(big.NewInt(1))}),
		mustObject(t, Entry{"b", NewInteger(big.NewInt(1))}),
		mustObject(t, Entry{"a", NewInteger(big.NewInt(1))}, Entry{"b", Null{}}),
		mustObject(t, Entry{"b", Null{}}, Entry{"a", NewInteger(big.NewInt(1))}),
		NewArray(mustObject(t, Entry{"k", NewArray(Boolean(true), String("v"))})),
	}
	for _, a := range values {
		encodedA := mustEncode(t, a)
		for _, b := range values {
			equal := Equal(a, b)
			byteEqual := bytes.Equal(encodedA, mustEncode(t, b))
			if equal != byteEqual {
				t.Errorf("Equal(%v, %v) = %v but bytes equal = %v (kinds %v/%v)",
					a, b, equal, byteEqual, a.Kind(), b.Kind())
			}
			if equal && Hash(a) != Hash(b) {
				t.Errorf("Equal values %v and %v hash differently", a, b)
			}
		}
	}
}

// TestObjectOrderAffectsEquality pins the ordered-object contract: the same
// entries in a different order are not equal (mirrors the Rust
// object_order_affects_encoding test, consema-rs/consema-pvce/src/lib.rs:1177).
func TestObjectOrderAffectsEquality(t *testing.T) {
	first := mustObject(t, Entry{"a", NewInteger(big.NewInt(1))}, Entry{"b", Null{}})
	second := mustObject(t, Entry{"b", Null{}}, Entry{"a", NewInteger(big.NewInt(1))})
	if Equal(first, second) {
		t.Error("reordered objects are equal, want not equal")
	}
	if Equal(mustObject(t, Entry{"a", NewInteger(big.NewInt(1))}), mustObject(t, Entry{"a", NewInteger(big.NewInt(1))})) != true {
		t.Error("identical objects are not equal")
	}
}

// TestArrayOrderAffectsEquality pins the ordered-array contract.
func TestArrayOrderAffectsEquality(t *testing.T) {
	if Equal(NewArray(String("a"), Null{}), NewArray(Null{}, String("a"))) {
		t.Error("reordered arrays are equal, want not equal")
	}
	if !Equal(NewArray(String("a"), Null{}), NewArray(String("a"), Null{})) {
		t.Error("identical arrays are not equal")
	}
}

func mustObject(t *testing.T, entries ...Entry) *Object {
	t.Helper()
	object, err := NewObject(entries...)
	if err != nil {
		t.Fatalf("NewObject(%v) failed: %v", entries, err)
	}
	return object
}

func mustBigInt(t *testing.T, text string) *big.Int {
	t.Helper()
	value, ok := new(big.Int).SetString(text, 10)
	if !ok {
		t.Fatalf("SetString(%q) failed", text)
	}
	return value
}

func mustEncode(t *testing.T, v Value) []byte {
	t.Helper()
	bytes, err := EncodePVCE(v)
	if err != nil {
		t.Fatalf("EncodePVCE(%v) failed: %v", v, err)
	}
	return bytes
}
