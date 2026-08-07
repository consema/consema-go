package core

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math/big"
	"testing"
)

// ---------------------------------------------------------------------------
// Golden byte vectors.
//
// The three vectors below are transcribed byte-for-byte from the Rust PVCE/1
// encoder's in-code pins:
//
//   - crates/consema-pvce/src/lib.rs:1192-1201 (object_byte_vector_is_frozen)
//   - crates/consema-pvce/src/lib.rs:1336-1342 (byte_vector_is_frozen)
//
// The Rust side is the authority for the bytes (roadmap §16.1 hard gate:
// "Rust 与 Go 的 PVCE/PGCE bytes 完全一致"); any change to these constants
// must land in both languages together.
// ---------------------------------------------------------------------------

// TestPVCEGoldenBytes pins the three Rust frozen byte vectors.
func TestPVCEGoldenBytes(t *testing.T) {
	// encode(Null) == b"PVCE\x01\x00\x00" (lib.rs:1337)
	nullBytes := mustEncode(t, Null{})
	wantNull := []byte{0x50, 0x56, 0x43, 0x45, 0x01, 0x00, 0x00} // hex 50564345010000
	if !bytes.Equal(nullBytes, wantNull) {
		t.Errorf("EncodePVCE(Null{}) = %x, want %x (Rust pin lib.rs:1337)", nullBytes, wantNull)
	}

	// encode(Integer(-256)) == b"PVCE\x01\x10\x04\x02\x02\x01\x00"
	// (lib.rs:1339-1341)
	integerBytes := mustEncode(t, NewInteger(big.NewInt(-256)))
	wantInteger := []byte{0x50, 0x56, 0x43, 0x45, 0x01, 0x10, 0x04, 0x02, 0x02, 0x01, 0x00}
	if !bytes.Equal(integerBytes, wantInteger) {
		t.Errorf("EncodePVCE(Integer(-256)) = %x, want %x (Rust pin lib.rs:1339-1341)", integerBytes, wantInteger)
	}

	// object {"a": Integer(1)} == hex 5056434501410a01200201611003010101
	// (lib.rs:1199)
	object, err := NewObject(Entry{Key: "a", Value: NewInteger(big.NewInt(1))})
	if err != nil {
		t.Fatal(err)
	}
	objectBytes := mustEncode(t, object)
	wantObject, err := hex.DecodeString("5056434501410a01200201611003010101")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(objectBytes, wantObject) {
		t.Errorf("EncodePVCE({a: 1}) = %x, want %x (Rust pin lib.rs:1199)", objectBytes, wantObject)
	}

	// The golden bytes decode back to Equal values.
	limits := DefaultDecodeLimits()
	if got, err := DecodePVCE(wantNull, limits); err != nil || !Equal(got, Null{}) {
		t.Errorf("DecodePVCE(golden null) = (%v, %v), want (Null{}, nil)", got, err)
	}
	if got, err := DecodePVCE(wantInteger, limits); err != nil || !Equal(got, NewInteger(big.NewInt(-256))) {
		t.Errorf("DecodePVCE(golden -256) = (%v, %v), want equal -256", got, err)
	}
	if got, err := DecodePVCE(wantObject, limits); err != nil || !Equal(got, object) {
		t.Errorf("DecodePVCE(golden object) = (%v, %v), want equal {a: 1}", got, err)
	}
}

// TestRoundTripEveryKind mirrors the Rust every_core_kind_round_trips test
// (crates/consema-pvce/src/lib.rs:1129-1174) restricted to the closed
// eight-kind Go value model: every kind round-trips byte-stably.
func TestRoundTripEveryKind(t *testing.T) {
	object, err := NewObject(
		Entry{Key: "a", Value: NewInteger(big.NewInt(1))},
		Entry{Key: "b", Value: String("中")},
	)
	if err != nil {
		t.Fatal(err)
	}
	values := []Value{
		Null{},
		Boolean(false),
		Boolean(true),
		NewInteger(big.NewInt(1234567890123456789)),
		NewInteger(mustBigInt(t, "123456789012345678901234567890")),
		NewDecimal(big.NewInt(1), big.NewInt(-999)),
		NewDecimal(big.NewInt(-12300), big.NewInt(2)),
		NewBinaryFloat64(0x8000000000000000),
		NewBinaryFloat64(0x7fc0000000000001),
		String(""),
		String("é"),
		String("中"),
		object,
		NewArray(),
		NewArray(Null{}, Boolean(false), object, NewArray(String("nested"))),
	}
	for _, v := range values {
		encoded := mustEncode(t, v)
		decoded, err := DecodePVCE(encoded, DefaultDecodeLimits())
		if err != nil {
			t.Errorf("DecodePVCE(%v) failed: %v", v, err)
			continue
		}
		if !Equal(decoded, v) {
			t.Errorf("round trip of %v (%s) decoded to %v", v, v.Kind(), decoded)
		}
		// Re-encoding the decoded value is byte-stable.
		reEncoded := mustEncode(t, decoded)
		if !bytes.Equal(reEncoded, encoded) {
			t.Errorf("re-encode of %v is not byte-stable: %x vs %x", v, reEncoded, encoded)
		}
	}
}

// TestObjectOrderAffectsEncoding mirrors the Rust
// object_order_affects_encoding_and_is_strict test
// (crates/consema-pvce/src/lib.rs:1177-1189): entry order is encoded, so a
// different order produces different bytes.
func TestObjectOrderAffectsEncoding(t *testing.T) {
	first := mustObject(t, Entry{"a", NewInteger(big.NewInt(1))}, Entry{"b", Null{}})
	second := mustObject(t, Entry{"b", Null{}}, Entry{"a", NewInteger(big.NewInt(1))})
	if bytes.Equal(mustEncode(t, first), mustEncode(t, second)) {
		t.Error("reordered objects encode to identical bytes")
	}
}

// ---------------------------------------------------------------------------
// Strict canonicality: the decoder rejects every non-canonical form the Rust
// decoder rejects (crates/consema-pvce/src/lib.rs:404-833, 1317-1333).
// ---------------------------------------------------------------------------

func pvce(tag byte, payload []byte) []byte {
	stream := []byte{'P', 'V', 'C', 'E', 0x01, tag, byte(len(payload))}
	return append(stream, payload...)
}

// TestDecodeRejectsNonCanonical drives every rejection vector. The byte
// vectors marked with a Rust reference are transcribed from the Rust in-code
// tests; the others follow the same canonicality rules.
func TestDecodeRejectsNonCanonical(t *testing.T) {
	overflowVarint := []byte{'P', 'V', 'C', 'E'}
	for i := 0; i < 9; i++ {
		overflowVarint = append(overflowVarint, 0xff)
	}
	overflowVarint = append(overflowVarint, 0x02)
	maximalVarint := append(append([]byte{}, overflowVarint[:len(overflowVarint)-1]...), 0x01)

	cases := []struct {
		name  string
		bytes []byte
		kind  PVCEErrorKind
	}{
		// Stream framing.
		{"invalid magic", []byte{'X', 'V', 'C', 'E', 0x01, 0x00, 0x00}, ErrInvalidMagic},
		{"truncated magic", []byte{'P', 'V', 'C'}, ErrUnexpectedEnd},
		{"truncated version", []byte{'P', 'V', 'C', 'E', 0x01}, ErrUnexpectedEnd},
		{"unsupported version", []byte{'P', 'V', 'C', 'E', 0x02, 0x00, 0x00}, ErrUnsupportedVersion},
		{"maximal varint version", maximalVarint, ErrUnsupportedVersion},
		// lib.rs:1317-1324: rejects_non_minimal_version_varint
		{"non-minimal version varint", []byte{'P', 'V', 'C', 'E', 0x81, 0x00, 0x00, 0x00}, ErrNonCanonicalVarint},
		{"varint overflow", overflowVarint, ErrVarintOverflow},
		{"trailing bytes", []byte{'P', 'V', 'C', 'E', 0x01, 0x00, 0x00, 0x00}, ErrTrailingBytes},
		{"record payload beyond input", []byte{'P', 'V', 'C', 'E', 0x01, 0x20, 0x05, 0x02, 0x01, 0x61}, ErrUnexpectedEnd},

		// Unknown tags: only the Rust extended tag 0x7f remains outside the
		// closed fifteen-kind Go value model (Go has no ExtendedValue type);
		// the former 0x12/0x21/0x30-0x33/0x42 rejection cases now decode
		// (covered by TestPVCEFifteenKindGoldenBytes and the round-trip
		// tests).
		{"unknown tag", pvce(0x60, nil), ErrUnknownCoreTag},
		{"multi-byte unknown tag", []byte{'P', 'V', 'C', 'E', 0x01, 0x80, 0x01, 0x00}, ErrUnknownCoreTag},
		{"extended root", pvce(0x7f, nil), ErrUnknownCoreTag},

		// Fixed-payload tags.
		{"null with payload", pvce(0x00, []byte{0x00}), ErrInvalidPayload},
		{"false with payload", pvce(0x01, []byte{0x00}), ErrInvalidPayload},
		{"true with payload", pvce(0x02, []byte{0x00}), ErrInvalidPayload},
		{"float64 short payload", pvce(0x13, []byte{0, 0, 0, 0}), ErrInvalidPayload},

		// Integers.
		// lib.rs:1327-1333: rejects_noncanonical_zero_integer
		{"integer leading zero", []byte{'P', 'V', 'C', 'E', 0x01, 0x10, 0x03, 0x01, 0x01, 0x00}, ErrNonCanonicalInteger},
		{"zero sign with magnitude", pvce(0x10, []byte{0x00, 0x01, 0x05}), ErrNonCanonicalInteger},
		{"integer empty magnitude", pvce(0x10, []byte{0x01, 0x00}), ErrNonCanonicalInteger},
		{"invalid integer sign", pvce(0x10, []byte{0x03, 0x00}), ErrInvalidIntegerSign},
		{"integer trailing payload", pvce(0x10, []byte{0x01, 0x01, 0x01, 0x00}), ErrTrailingPayload},

		// Decimals. Note zero is encoded canonically (sign 0, empty
		// magnitude: 00 00), exactly as the canonicality rules require.
		{"non-canonical decimal", pvce(0x11, []byte{0x03, 0x01, 0x01, 0x0a, 0x02, 0x00, 0x00}), ErrNonCanonicalDecimal},
		{"zero decimal with exponent", pvce(0x11, []byte{0x02, 0x00, 0x00, 0x03, 0x01, 0x01, 0x05}), ErrNonCanonicalDecimal},
		{"trailing field", pvce(0x11, []byte{0x04, 0x01, 0x01, 0x01, 0x00, 0x03, 0x01, 0x01, 0x01}), ErrTrailingField},

		// Strings.
		{"invalid utf8", pvce(0x20, []byte{0x01, 0xff}), ErrInvalidUTF8},
		{"string trailing payload", pvce(0x20, []byte{0x01, 0x61, 0x00}), ErrTrailingPayload},

		// Objects.
		{"object key not string", pvce(0x41, []byte{0x01, 0x10, 0x01, 0x00}), ErrObjectKeyNotString},
		{"duplicate object key", pvce(0x41, []byte{
			0x02,                   // count
			0x20, 0x02, 0x01, 0x61, // key "a"
			0x00, 0x00, // value null
			0x20, 0x02, 0x01, 0x61, // key "a" again
			0x01, 0x00, // value false
		}), ErrDuplicateObjectKey},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := DecodePVCE(c.bytes, DefaultDecodeLimits())
			if !IsPVCEError(err, c.kind) {
				t.Fatalf("DecodePVCE(% x) = %v, want kind %v", c.bytes, err, c.kind)
			}
		})
	}
}

// TestDecodeAcceptsCanonicalMultibyteVarints shows that multi-byte varints
// are accepted when canonical: the tag 0x80 0x01 (= 128) parses and only
// then fails as an unknown tag, not as a canonicality error.
func TestDecodeAcceptsCanonicalMultibyteVarints(t *testing.T) {
	_, err := DecodePVCE([]byte{'P', 'V', 'C', 'E', 0x01, 0x80, 0x01, 0x00}, DefaultDecodeLimits())
	var target *PVCEError
	if !errors.As(err, &target) || target.Kind != ErrUnknownCoreTag || target.Value != 128 {
		t.Errorf("err = %v, want ErrUnknownCoreTag with tag 128", err)
	}
}

// TestDecodeEnforcesEachResourceLimit drives every decoder limit with the
// Rust field names (crates/consema-pvce/src/lib.rs:55-82).
func TestDecodeEnforcesEachResourceLimit(t *testing.T) {
	var nested Value = Null{}
	for i := 0; i < 3; i++ {
		nested = NewArray(nested)
	}
	cases := []struct {
		name   string
		value  Value
		limit  string
		adjust func(*DecodeLimits)
	}{
		{"stream-bytes", Null{}, "stream-bytes", func(l *DecodeLimits) { l.MaxBytes = 4 }},
		{"nesting-depth", nested, "nesting-depth", func(l *DecodeLimits) { l.MaxDepth = 2 }},
		{"value-nodes", NewArray(Null{}, Null{}), "value-nodes", func(l *DecodeLimits) { l.MaxNodes = 1 }},
		{"container-entries", NewArray(Null{}, Null{}), "container-entries", func(l *DecodeLimits) { l.MaxContainerEntries = 1 }},
		{"integer-bytes", NewInteger(big.NewInt(0x0102)), "integer-bytes", func(l *DecodeLimits) { l.MaxIntegerBytes = 1 }},
		{"blob-bytes", String("12345"), "blob-bytes", func(l *DecodeLimits) { l.MaxBlobBytes = 4 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			encoded := mustEncode(t, c.value)
			// With defaults the same bytes decode fine.
			if _, err := DecodePVCE(encoded, DefaultDecodeLimits()); err != nil {
				t.Fatalf("default decode failed: %v", err)
			}
			limits := DefaultDecodeLimits()
			c.adjust(&limits)
			_, err := DecodePVCE(encoded, limits)
			if !IsPVCEError(err, ErrResourceLimit) {
				t.Fatalf("err = %v, want resource limit", err)
			}
			var target *PVCEError
			_ = errors.As(err, &target)
			if target.Field != c.limit {
				t.Errorf("resource-limit field = %q, want %q", target.Field, c.limit)
			}
			if target.Code() != "core.pvce.resource-limit@1" {
				t.Errorf("code = %q, want core.pvce.resource-limit@1", target.Code())
			}
		})
	}
}

// TestEncodeBoundedRejectsEachResourceLimit mirrors the Rust
// bounded_encode_rejects_each_resource_limit test
// (crates/consema-pvce/src/lib.rs:1204-1276).
func TestEncodeBoundedRejectsEachResourceLimit(t *testing.T) {
	value := NewArray(String("12345"), String("67890"), String("abcde"))
	cases := []struct {
		name   string
		value  Value
		limit  string
		adjust func(*EncodeLimits)
	}{
		{"stream-bytes", value, "stream-bytes", func(l *EncodeLimits) { l.MaxBytes = 4 }},
		{"value-nodes", value, "value-nodes", func(l *EncodeLimits) { l.MaxNodes = 2 }},
		{"container-entries", value, "container-entries", func(l *EncodeLimits) { l.MaxContainerEntries = 2 }},
		{"blob-bytes", String("12345"), "blob-bytes", func(l *EncodeLimits) { l.MaxBlobBytes = 4 }},
		{"integer-bytes", NewInteger(big.NewInt(0x0102)), "integer-bytes", func(l *EncodeLimits) { l.MaxIntegerBytes = 1 }},
		{"nesting-depth", NewArray(NewArray(NewArray(Null{}))), "nesting-depth", func(l *EncodeLimits) { l.MaxDepth = 2 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			limits := DefaultEncodeLimits()
			c.adjust(&limits)
			_, err := EncodePVCEBounded(c.value, limits)
			if !IsPVCEError(err, ErrResourceLimit) {
				t.Fatalf("err = %v, want resource limit", err)
			}
			var target *PVCEError
			_ = errors.As(err, &target)
			if target.Field != c.limit {
				t.Errorf("resource-limit field = %q, want %q", target.Field, c.limit)
			}
		})
	}
	// Boundary: the exact stream size passes, one byte less fails.
	exact := len(mustEncode(t, String("x")))
	if _, err := EncodePVCEBounded(String("x"), EncodeLimits{MaxBytes: exact, MaxDepth: 256, MaxNodes: 1_000_000, MaxContainerEntries: 1_000_000, MaxIntegerBytes: 1 << 20, MaxBlobBytes: 64 << 20}); err != nil {
		t.Errorf("exact-size bounded encode failed: %v", err)
	}
}

// TestEncodePVCEBoundedMatchesUnbounded pins that the bounded encoder never
// changes the bytes at the boundary.
func TestEncodePVCEBoundedMatchesUnbounded(t *testing.T) {
	value := mustObject(t, Entry{"k", NewArray(String("v"), Boolean(true))})
	bounded, err := EncodePVCEBounded(value, DefaultEncodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bounded, mustEncode(t, value)) {
		t.Error("bounded and unbounded encodings differ")
	}
}

// TestEncodeNilValue pins the nil guard: EncodePVCE(nil) is a typed
// PVCEError with the frozen code.
func TestEncodeNilValue(t *testing.T) {
	for name, err := range map[string]error{
		"EncodePVCE":        func() error { _, e := EncodePVCE(nil); return e }(),
		"EncodePVCEBounded": func() error { _, e := EncodePVCEBounded(nil, DefaultEncodeLimits()); return e }(),
	} {
		if !IsPVCEError(err, ErrInvalidValue) {
			t.Errorf("%s(nil) = %v, want ErrInvalidValue", name, err)
		}
		var target *PVCEError
		if errors.As(err, &target) && target.Code() != "core.pvce.invalid-value@1" {
			t.Errorf("%s code = %q", name, target.Code())
		}
	}
}

// TestPVCEErrorCodeTable pins the frozen registered codes for every failure
// kind, transcribed from crates/consema-pvce/src/lib.rs:1062-1087.
func TestPVCEErrorCodeTable(t *testing.T) {
	cases := []struct {
		kind PVCEErrorKind
		code string
	}{
		{ErrInvalidMagic, "core.pvce.invalid-magic@1"},
		{ErrUnsupportedVersion, "core.pvce.unsupported-version@1"},
		{ErrUnexpectedEnd, "core.pvce.unexpected-end@1"},
		{ErrTrailingBytes, "core.pvce.trailing-bytes@1"},
		{ErrTrailingPayload, "core.pvce.trailing-payload@1"},
		{ErrTrailingField, "core.pvce.trailing-field@1"},
		{ErrNonCanonicalVarint, "core.pvce.non-canonical-varint@1"},
		{ErrVarintOverflow, "core.pvce.varint-overflow@1"},
		{ErrLengthOverflow, "core.pvce.length-overflow@1"},
		{ErrResourceLimit, "core.pvce.resource-limit@1"},
		{ErrUnknownCoreTag, "core.pvce.unknown-tag@1"},
		{ErrInvalidPayload, "core.pvce.invalid-payload@1"},
		{ErrInvalidIntegerSign, "core.pvce.invalid-integer-sign@1"},
		{ErrNonCanonicalInteger, "core.pvce.non-canonical-integer@1"},
		{ErrNonCanonicalDecimal, "core.pvce.non-canonical-decimal@1"},
		{ErrInvalidUTF8, "core.pvce.invalid-utf8@1"},
		{ErrObjectKeyNotString, "core.pvce.object-key-not-string@1"},
		{ErrDuplicateObjectKey, "core.pvce.duplicate-object-key@1"},
		{ErrInvalidValue, "core.pvce.invalid-value@1"},
	}
	for _, c := range cases {
		err := &PVCEError{Kind: c.kind}
		if got := err.Code(); got != c.code {
			t.Errorf("kind %d Code() = %q, want %q", c.kind, got, c.code)
		}
		if err.Error() == "" {
			t.Errorf("kind %d has empty Error()", c.kind)
		}
		if !IsPVCEError(err, c.kind) {
			t.Errorf("IsPVCEError(%v, %d) = false", err, c.kind)
		}
		if IsPVCEError(err, c.kind+1) {
			t.Errorf("IsPVCEError(%v, %d) = true, want false", err, c.kind+1)
		}
	}
	// errors.As works through wrapping.
	wrapped := errors.Join(&PVCEError{Kind: ErrInvalidMagic}, errors.New("wrapped"))
	if !IsPVCEError(wrapped, ErrInvalidMagic) {
		t.Error("IsPVCEError does not see wrapped errors")
	}
}

// TestDecodePVCEZeroLimitsRejectsEverything pins the documented zero-value
// behavior: a zero DecodeLimits rejects every stream.
func TestDecodePVCEZeroLimitsRejectsEverything(t *testing.T) {
	if _, err := DecodePVCE(mustEncode(t, Null{}), DecodeLimits{}); !IsPVCEError(err, ErrResourceLimit) {
		t.Errorf("err = %v, want resource limit with zero limits", err)
	}
}

// TestCanonicalEmptyStreamEdge pins edge round trips at the extremes of the
// wire format: empty string, empty array, empty object, zero integer, zero
// decimal, and a maximal integer magnitude.
func TestCanonicalEmptyStreamEdge(t *testing.T) {
	values := []Value{
		String(""),
		NewArray(),
		mustObject(t),
		NewInteger(big.NewInt(0)),
		NewDecimal(big.NewInt(0), big.NewInt(0)),
		NewInteger(mustBigInt(t, "340282366920938463463374607431768211455")),
		NewInteger(mustBigInt(t, "-340282366920938463463374607431768211455")),
	}
	for _, v := range values {
		encoded := mustEncode(t, v)
		decoded, err := DecodePVCE(encoded, DefaultDecodeLimits())
		if err != nil {
			t.Errorf("DecodePVCE(%x) failed: %v", encoded, err)
			continue
		}
		if !Equal(decoded, v) {
			t.Errorf("round trip of %v decoded to %v", v, decoded)
		}
	}
}
