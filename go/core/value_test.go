package core

import (
	"errors"
	"math"
	"math/big"
	"testing"
)

// Compile-time closure of the Value interface: exactly the fifteen RFC 0016
// §4.1 kinds implement it.
var (
	_ Value = Null{}
	_ Value = Boolean(false)
	_ Value = String("")
	_ Value = Integer{}
	_ Value = Decimal{}
	_ Value = BinaryFloat32(0)
	_ Value = BinaryFloat64(0)
	_ Value = Bytes(nil)
	_ Value = Date{}
	_ Value = Time{}
	_ Value = LocalDateTime{}
	_ Value = OffsetDateTime{}
	_ Value = (*Object)(nil)
	_ Value = (*Array)(nil)
	_ Value = (*EntryMapping)(nil)
)

// TestKindMapping pins the RFC 0016 §4.1 table: each of the fifteen
// PortableValue kinds maps to exactly one Go type and one Kind value.
func TestKindMapping(t *testing.T) {
	cases := []struct {
		value Value
		kind  Kind
		name  string
	}{
		{&Object{}, KindObject, "Object"},
		{&Array{}, KindArray, "Array"},
		{String(""), KindString, "String"},
		{NewInteger(big.NewInt(0)), KindInteger, "Integer"},
		{NewDecimal(big.NewInt(0), big.NewInt(0)), KindDecimal, "Decimal"},
		{NewBinaryFloat32(0), KindBinaryFloat32, "BinaryFloat32"},
		{NewBinaryFloat64(0), KindBinaryFloat64, "BinaryFloat64"},
		{NewBytes(nil), KindBytes, "Bytes"},
		{mustDate(t, 2024, 1, 1), KindDate, "Date"},
		{mustTime(t, 0, 0, 0, 0, 0), KindTime, "Time"},
		{NewLocalDateTime(mustDate(t, 2024, 1, 1), mustTime(t, 0, 0, 0, 0, 0)), KindLocalDateTime, "LocalDateTime"},
		{mustOffset(t, 0, 2024, 1, 1, 0, 0, 0), KindOffsetDateTime, "OffsetDateTime"},
		{Boolean(false), KindBoolean, "Boolean"},
		{Null{}, KindNull, "Null"},
		{&EntryMapping{}, KindEntryMapping, "EntryMapping"},
	}
	for _, c := range cases {
		if got := c.value.Kind(); got != c.kind {
			t.Errorf("%T.Kind() = %v, want %v", c.value, got, c.kind)
		}
		if got := c.kind.String(); got != c.name {
			t.Errorf("Kind(%d).String() = %q, want %q", c.kind, got, c.name)
		}
	}
	if NullValue().Kind() != KindNull {
		t.Error("NullValue() is not KindNull")
	}
}

// TestObjectPreservesInsertionOrder pins the ordered-object contract (RFC
// 0016 §4.1; RFC 0002): entry order is a language-neutral fact.
func TestObjectPreservesInsertionOrder(t *testing.T) {
	builder := NewObjectBuilder()
	mustInsert(t, builder, "z", NewInteger(big.NewInt(1)))
	mustInsert(t, builder, "a", Null{})
	mustInsert(t, builder, "m", Boolean(true))
	object := builder.Build()
	want := []string{"z", "a", "m"}
	entries := object.Entries()
	if len(entries) != len(want) {
		t.Fatalf("Entries() length = %d, want %d", len(entries), len(want))
	}
	for i, entry := range entries {
		if entry.Key != want[i] {
			t.Errorf("entry %d key = %q, want %q", i, entry.Key, want[i])
		}
	}
	if v, ok := object.Get("a"); !ok || !Equal(v, Null{}) {
		t.Errorf("Get(\"a\") = (%v, %v), want (Null{}, true)", v, ok)
	}
	if v, ok := object.Get("missing"); ok {
		t.Errorf("Get(\"missing\") = (%v, %v), want missing", v, ok)
	}
}

// TestObjectRejectsDuplicateKeys pins the constructor error of RFC 0016
// §4.1 ("Objects reject duplicate keys at construction time").
func TestObjectRejectsDuplicateKeys(t *testing.T) {
	builder := NewObjectBuilder()
	mustInsert(t, builder, "k", Null{})
	err := builder.Insert("k", String("x"))
	var dup *DuplicateKeyError
	if !errors.As(err, &dup) {
		t.Fatalf("Insert duplicate = %v, want *DuplicateKeyError", err)
	}
	if dup.Key != "k" {
		t.Errorf("DuplicateKeyError.Key = %q, want %q", dup.Key, "k")
	}
	if dup.Code() != "core.pvce.duplicate-object-key@1" {
		t.Errorf("DuplicateKeyError.Code() = %q", dup.Code())
	}
	if _, err := NewObject(Entry{"a", Null{}}, Entry{"a", Null{}}); err == nil {
		t.Error("NewObject with duplicate keys succeeded, want error")
	}
	if _, err := NewObject(Entry{"a", Null{}}, Entry{"b", Null{}}); err != nil {
		t.Errorf("NewObject with unique keys failed: %v", err)
	}
}

// TestNewObjectCopiesInput pins the logical-immutability contract: mutating
// the caller's slice after construction does not change the object.
func TestNewObjectCopiesInput(t *testing.T) {
	entries := []Entry{{Key: "a", Value: Null{}}}
	object, err := NewObject(entries...)
	if err != nil {
		t.Fatal(err)
	}
	entries[0] = Entry{Key: "mutated", Value: String("x")}
	if object.Len() != 1 || object.Entries()[0].Key != "a" {
		t.Error("NewObject retained the caller's slice")
	}
}

func mustInsert(t *testing.T, b *ObjectBuilder, key string, value Value) {
	t.Helper()
	if err := b.Insert(key, value); err != nil {
		t.Fatalf("Insert(%q) failed: %v", key, err)
	}
}

// TestArrayPreservesOrder pins the ordered-array contract (RFC 0016 §4.1).
func TestArrayPreservesOrder(t *testing.T) {
	array := NewArray(String("b"), String("a"), Null{})
	want := []Value{String("b"), String("a"), Null{}}
	items := array.Items()
	if len(items) != len(want) {
		t.Fatalf("Items() length = %d, want %d", len(items), len(want))
	}
	for i, item := range items {
		if !Equal(item, want[i]) {
			t.Errorf("item %d = %v, want %v", i, item, want[i])
		}
	}
	if !Equal(array.At(1), String("a")) {
		t.Error("At(1) mismatch")
	}
	if array.Len() != 3 {
		t.Errorf("Len() = %d, want 3", array.Len())
	}
}

// TestNewArrayCopiesInput pins the logical-immutability contract.
func TestNewArrayCopiesInput(t *testing.T) {
	items := []Value{String("a"), Null{}}
	array := NewArray(items...)
	items[0] = String("mutated")
	if !Equal(array.At(0), String("a")) {
		t.Error("NewArray retained the caller's slice")
	}
}

// TestNewArrayPanicsOnNil pins the nil-item precondition.
func TestNewArrayPanicsOnNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewArray(nil) did not panic")
		}
	}()
	NewArray(Null{}, nil)
}

// TestObjectBuilderRejectsNilValue pins the nil-value guard of Insert.
func TestObjectBuilderRejectsNilValue(t *testing.T) {
	builder := NewObjectBuilder()
	err := builder.Insert("k", nil)
	if !IsPVCEError(err, ErrInvalidValue) {
		t.Errorf("Insert(nil) = %v, want ErrInvalidValue", err)
	}
	if builder.Len() != 0 {
		t.Error("failed Insert changed the builder")
	}
}

// TestIntegerCanonicalForm pins the BigInteger wrapper (RFC 0016 §4.1): zero
// is sign 0 with an empty magnitude; copy semantics on both sides.
func TestIntegerCanonicalForm(t *testing.T) {
	i := NewInteger(big.NewInt(-256))
	if i.Signum() != -1 {
		t.Errorf("Signum() = %d, want -1", i.Signum())
	}
	if i.Int().Cmp(big.NewInt(-256)) != 0 {
		t.Errorf("Int() = %v, want -256", i.Int())
	}
	if len(i.magnitude()) != 2 || i.magnitude()[0] != 0x01 || i.magnitude()[1] != 0x00 {
		t.Errorf("magnitude() = % x, want 01 00", i.magnitude())
	}
	zero := NewInteger(nil)
	if zero.Signum() != 0 || len(zero.magnitude()) != 0 {
		t.Errorf("zero = sign %d magnitude % x", zero.Signum(), zero.magnitude())
	}
	// Construction copies the input.
	input := big.NewInt(5)
	wrapped := NewInteger(input)
	input.SetInt64(99)
	if wrapped.Int().Cmp(big.NewInt(5)) != 0 {
		t.Error("NewInteger retained the caller's big.Int")
	}
	// Accessors return copies.
	out := wrapped.Int()
	out.SetInt64(1)
	if wrapped.Int().Cmp(big.NewInt(5)) != 0 {
		t.Error("Int() exposed the wrapped big.Int")
	}
	// The zero value behaves as zero.
	var zeroValue Integer
	if zeroValue.Signum() != 0 || zeroValue.Int().Sign() != 0 {
		t.Error("zero-value Integer is not zero")
	}
}

// TestDecimalCanonicalization pins the Decimal normalization (RFC 0016 §4.1;
// mirroring the Rust Decimal::new, crates/consema-core/src/value.rs:277-292).
func TestDecimalCanonicalization(t *testing.T) {
	cases := []struct {
		coefficient, exponent int64
		wantCoefficient       int64
		wantExponent          int64
	}{
		{10, 0, 1, 1},
		{100, -2, 1, 0},
		{-12300, 2, -123, 4},
		{0, 5, 0, 0},
		{0, -5, 0, 0},
		{7, -3, 7, -3},
	}
	for _, c := range cases {
		d := NewDecimal(big.NewInt(c.coefficient), big.NewInt(c.exponent))
		if got := d.Coefficient().Int64(); got != c.wantCoefficient {
			t.Errorf("NewDecimal(%d, %d) coefficient = %d, want %d",
				c.coefficient, c.exponent, got, c.wantCoefficient)
		}
		if got := d.Exponent().Int64(); got != c.wantExponent {
			t.Errorf("NewDecimal(%d, %d) exponent = %d, want %d",
				c.coefficient, c.exponent, got, c.wantExponent)
		}
	}
	zero := NewDecimal(nil, nil)
	if zero.Coefficient().Sign() != 0 || zero.Exponent().Sign() != 0 {
		t.Error("NewDecimal(nil, nil) is not 0 × 10^0")
	}
	// Construction copies the inputs.
	coefficient := big.NewInt(50)
	d := NewDecimal(coefficient, big.NewInt(0))
	coefficient.SetInt64(999)
	if d.Coefficient().Cmp(big.NewInt(5)) != 0 || d.Exponent().Cmp(big.NewInt(1)) != 0 {
		t.Error("NewDecimal retained the caller's big.Ints")
	}
	// Accessors return copies.
	out := d.Coefficient()
	out.SetInt64(1)
	if d.Coefficient().Cmp(big.NewInt(5)) != 0 {
		t.Error("Coefficient() exposed the wrapped big.Int")
	}
	// The zero value behaves as zero.
	var zeroValue Decimal
	if zeroValue.Coefficient().Sign() != 0 || zeroValue.Exponent().Sign() != 0 {
		t.Error("zero-value Decimal is not 0 × 10^0")
	}
}

// TestBinaryFloat64Bits pins the exact-bit-pattern contract (RFC 0016 §4.1):
// -0.0, NaN payloads, and the like are preserved bit-exactly.
func TestBinaryFloat64Bits(t *testing.T) {
	negativeZero := uint64(0x8000000000000000)
	b := NewBinaryFloat64(negativeZero)
	if b.Bits() != negativeZero {
		t.Errorf("Bits() = %#x, want %#x", b.Bits(), negativeZero)
	}
	if f := b.Float64(); !math.Signbit(f) || f != 0 {
		t.Errorf("Float64() = %v, want -0.0", f)
	}
	nanBits := uint64(0x7ff8000000000001)
	nan := NewBinaryFloat64(nanBits)
	if !math.IsNaN(nan.Float64()) || nan.Bits() != nanBits {
		t.Error("NaN payload was not preserved bit-exactly")
	}
	if got := FromFloat64(3.5).Bits(); got != math.Float64bits(3.5) {
		t.Errorf("FromFloat64(3.5).Bits() = %#x", got)
	}
	if got := NewBinaryFloat64(0).Bits(); got != 0 {
		t.Errorf("zero bits = %#x", got)
	}
}
