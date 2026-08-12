package core

import (
	"bytes"
	"math/big"
	"testing"
)

// TestRoundTripComplexDocument round-trips a rich document exercising all
// eight kinds, multi-byte UTF-8 object keys, an empty key, and nested
// containers, verifying byte stability across the decode/encode cycle.
func TestRoundTripComplexDocument(t *testing.T) {
	document, err := NewObject(
		Entry{Key: "", Value: NewArray()},
		Entry{Key: "中", Value: NewDecimal(big.NewInt(-123456789), big.NewInt(-30))},
		Entry{Key: "é", Value: String("consema\x00\x01")},
		Entry{Key: "ints", Value: NewArray(
			NewInteger(mustBigInt(t, "99999999999999999999999999999999999999")),
			NewInteger(mustBigInt(t, "-99999999999999999999999999999999999999")),
			NewInteger(big.NewInt(0)),
		)},
		Entry{Key: "floats", Value: NewArray(
			NewBinaryFloat64(0x0000000000000001),
			NewBinaryFloat64(0xfff0000000000001),
			NewBinaryFloat64(0x3ff0000000000000),
		)},
		Entry{Key: "flags", Value: NewArray(Boolean(true), Boolean(false), Null{})},
		Entry{Key: "nested", Value: NewArray(
			mustObject(t, Entry{"a", NewArray(NewArray(NewArray(Null{})))}),
			mustObject(t, Entry{"b", mustObject(t, Entry{"c", String("deep")})}),
		)},
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded := mustEncode(t, document)
	decoded, err := DecodePVCE(encoded, DefaultDecodeLimits())
	if err != nil {
		t.Fatalf("DecodePVCE failed: %v", err)
	}
	if !Equal(decoded, document) {
		t.Fatalf("complex document round trip changed the value")
	}
	reEncoded := mustEncode(t, decoded)
	if !bytes.Equal(reEncoded, encoded) {
		t.Error("complex document re-encode is not byte-stable")
	}
	// The keys keep their order.
	entries := decoded.(*Object).Entries()
	if entries[1].Key != "中" || entries[2].Key != "é" {
		t.Errorf("key order or content changed: %q %q", entries[1].Key, entries[2].Key)
	}
}

// TestLargeContainerRoundTrips round-trips containers large enough to need
// multi-byte count varints (200 > 127), exercising the minimal-varint rule
// on the wire.
func TestLargeContainerRoundTrips(t *testing.T) {
	items := make([]Value, 200)
	for i := range items {
		items[i] = NewInteger(big.NewInt(int64(i)))
	}
	array := NewArray(items...)
	if round, err := DecodePVCE(mustEncode(t, array), DefaultDecodeLimits()); err != nil {
		t.Fatalf("large array decode failed: %v", err)
	} else if !Equal(round, array) {
		t.Fatal("large array round trip changed the value")
	}

	builder := NewObjectBuilder()
	for i := 0; i < 200; i++ {
		key := "k" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		if err := builder.Insert(key, NewInteger(big.NewInt(int64(i)))); err != nil {
			t.Fatalf("Insert(%q) failed: %v", key, err)
		}
	}
	object := builder.Build()
	if round, err := DecodePVCE(mustEncode(t, object), DefaultDecodeLimits()); err != nil {
		t.Fatalf("large object decode failed: %v", err)
	} else if !Equal(round, object) {
		t.Fatal("large object round trip changed the value")
	}
}

// TestDecodeLimitBoundaries pins the resource-limit semantics at the exact
// boundary: a stream of exactly MaxBytes decodes, one byte more fails; a
// node count exactly at MaxNodes decodes, one record more fails; a container
// exactly at MaxContainerEntries decodes, one entry more fails.
func TestDecodeLimitBoundaries(t *testing.T) {
	// Stream bytes: String("x") is 9 bytes; MaxBytes 9 passes, 8 fails.
	stream := mustEncode(t, String("x"))
	if len(stream) != 9 {
		t.Fatalf("String(\"x\") stream = %d bytes, want 9", len(stream))
	}
	limits := DefaultDecodeLimits()
	limits.MaxBytes = 9
	if _, err := DecodePVCE(stream, limits); err != nil {
		t.Errorf("MaxBytes == stream size failed: %v", err)
	}
	limits.MaxBytes = 8
	if _, err := DecodePVCE(stream, limits); !IsPVCEError(err, ErrResourceLimit) {
		t.Errorf("MaxBytes == size-1: err = %v, want resource limit", err)
	}

	// Nodes: NewArray(Null{}) is 2 records (array + item).
	nodes := NewArray(Null{})
	limits = DefaultDecodeLimits()
	limits.MaxNodes = 2
	if _, err := DecodePVCE(mustEncode(t, nodes), limits); err != nil {
		t.Errorf("MaxNodes == record count failed: %v", err)
	}
	limits.MaxNodes = 1
	if _, err := DecodePVCE(mustEncode(t, nodes), limits); !IsPVCEError(err, ErrResourceLimit) {
		t.Errorf("MaxNodes == count-1: err = %v, want resource limit", err)
	}

	// Container entries: NewArray(Null{}, Null{}) has 2 entries.
	entries := NewArray(Null{}, Null{})
	limits = DefaultDecodeLimits()
	limits.MaxContainerEntries = 2
	if _, err := DecodePVCE(mustEncode(t, entries), limits); err != nil {
		t.Errorf("MaxContainerEntries == count failed: %v", err)
	}
	limits.MaxContainerEntries = 1
	if _, err := DecodePVCE(mustEncode(t, entries), limits); !IsPVCEError(err, ErrResourceLimit) {
		t.Errorf("MaxContainerEntries == count-1: err = %v, want resource limit", err)
	}
}

// TestEncodeBoundedBoundary pins the bounded encoder's exact-boundary
// semantics: a stream of exactly MaxBytes passes, one byte less fails; a
// node count exactly at MaxNodes passes, one less fails.
func TestEncodeBoundedBoundary(t *testing.T) {
	stream := mustEncode(t, String("x"))
	limits := DefaultEncodeLimits()
	limits.MaxBytes = len(stream)
	if _, err := EncodePVCEBounded(String("x"), limits); err != nil {
		t.Errorf("MaxBytes == stream size failed: %v", err)
	}
	limits.MaxBytes = len(stream) - 1
	if _, err := EncodePVCEBounded(String("x"), limits); !IsPVCEError(err, ErrResourceLimit) {
		t.Errorf("MaxBytes == size-1: err = %v, want resource limit", err)
	}

	// NewArray(Null{}) is 2 records.
	value := NewArray(Null{})
	limits = DefaultEncodeLimits()
	limits.MaxNodes = 2
	if _, err := EncodePVCEBounded(value, limits); err != nil {
		t.Errorf("MaxNodes == record count failed: %v", err)
	}
	limits.MaxNodes = 1
	if _, err := EncodePVCEBounded(value, limits); !IsPVCEError(err, ErrResourceLimit) {
		t.Errorf("MaxNodes == count-1: err = %v, want resource limit", err)
	}
}

// TestObjectKeysRoundTripUnicode pins the object-key contract for
// multi-byte UTF-8 and empty keys: keys are byte-exact through encode and
// decode, and uniqueness is per byte content.
func TestObjectKeysRoundTripUnicode(t *testing.T) {
	object, err := NewObject(
		Entry{Key: "中", Value: Null{}},
		Entry{Key: "é", Value: Null{}},
		Entry{Key: "", Value: Null{}},
		Entry{Key: "a\x00b", Value: Null{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePVCE(mustEncode(t, object), DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !Equal(decoded, object) {
		t.Fatal("unicode-key object round trip changed the value")
	}
	entries := decoded.(*Object).Entries()
	wantKeys := []string{"中", "é", "", "a\x00b"}
	for i, entry := range entries {
		if entry.Key != wantKeys[i] {
			t.Errorf("key %d = %q, want %q", i, entry.Key, wantKeys[i])
		}
	}
	// A byte-different key is a different key.
	other, err := NewObject(Entry{Key: "a\x00b", Value: Null{}}, Entry{Key: "a\x00c", Value: Null{}})
	if err != nil {
		t.Fatal(err)
	}
	if other.Len() != 2 {
		t.Error("byte-different keys were treated as duplicates")
	}
}

// TestDecodeDuplicateKeysDetectedAcrossContainers pins duplicate-key
// rejection for objects decoded from streams, including keys that differ in
// byte length but not content (the encoder never produces them).
func TestDecodeDuplicateKeysDetectedAcrossContainers(t *testing.T) {
	// {"a": null, "a": null} — duplicate at decode time.
	stream := pvce(0x41, []byte{
		0x02,
		0x20, 0x02, 0x01, 0x61,
		0x00, 0x00,
		0x20, 0x02, 0x01, 0x61,
		0x00, 0x00,
	})
	if _, err := DecodePVCE(stream, DefaultDecodeLimits()); !IsPVCEError(err, ErrDuplicateObjectKey) {
		t.Errorf("err = %v, want duplicate object key", err)
	}
}

// TestIntegerMagnitudeByteBoundaries pins integer encoding at magnitude
// length boundaries: 127 vs 128 bytes use 1- and 2-byte length varints.
func TestIntegerMagnitudeByteBoundaries(t *testing.T) {
	// 2^1016 - 1 is 127 bytes of 0xff.
	magnitude := make([]byte, 127)
	for i := range magnitude {
		magnitude[i] = 0xff
	}
	small := new(big.Int).SetBytes(magnitude)
	large := new(big.Int).Lsh(small, 8) // 128 bytes
	for _, value := range []Value{NewInteger(small), NewInteger(large), NewInteger(new(big.Int).Neg(large))} {
		encoded := mustEncode(t, value)
		decoded, err := DecodePVCE(encoded, DefaultDecodeLimits())
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if !Equal(decoded, value) {
			t.Fatal("integer magnitude boundary round trip changed the value")
		}
	}
}
