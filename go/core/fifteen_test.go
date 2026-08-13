package core

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math"
	"math/big"
	"testing"
)

// ---------------------------------------------------------------------------
// Golden byte vectors for the seven additional kinds.
//
// The vectors below were transcribed byte-for-byte from the Rust PVCE/1
// encoder, generated with the reference codec itself (consema-rs/consema-pvce)
// for the exact values of the Rust every_core_kind_round_trips test
// (consema-rs/consema-pvce/src/lib.rs:1129-1174): BinaryFloat32(0x7fc0_0001),
// Bytes([0, 255]), Date(-12345-02-28), Time(23:59:58.125),
// LocalDateTime(-12345-02-28T23:59:58.125),
// OffsetDateTime(-12345-02-28T23:59:58.125-23:00), and the entry mapping
// {true: null}. The Rust side is the authority for the bytes (roadmap §16.1
// hard gate: "Rust 与 Go 的 PVCE/PGCE bytes 完全一致").
// ---------------------------------------------------------------------------

func mustDate(t *testing.T, year int64, month, day uint8) Date {
	t.Helper()
	date, err := NewDate(big.NewInt(year), month, day)
	if err != nil {
		t.Fatalf("NewDate(%d, %d, %d) failed: %v", year, month, day, err)
	}
	return date
}

func mustTime(t *testing.T, hour, minute, second uint8, coefficient, exponent int64) Time {
	t.Helper()
	time, err := NewTime(hour, minute, second, NewDecimal(big.NewInt(coefficient), big.NewInt(exponent)))
	if err != nil {
		t.Fatalf("NewTime(%d, %d, %d, %d×10^%d) failed: %v", hour, minute, second, coefficient, exponent, err)
	}
	return time
}

// TestPVCEFifteenKindGoldenBytes pins the Rust encoder's bytes for every
// additional kind, individually and inside one sequence.
func TestPVCEFifteenKindGoldenBytes(t *testing.T) {
	date := mustDate(t, -12345, 2, 28)
	time := mustTime(t, 23, 59, 58, 125, -3)
	local := NewLocalDateTime(date, time)
	offset, err := NewOffsetDateTime(local, -23*60*60)
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := NewEntryMapping(EntryMappingEntry{Key: Boolean(true), Value: Null{}})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		value Value
		want  string
	}{
		{"binary float32", NewBinaryFloat32(0x7fc00001), "505643450112047fc00001"},
		{"bytes", NewBytes([]byte{0, 255}), "505643450121030200ff"},
		{"empty bytes", NewBytes(nil), "5056434501210100"},
		{"date", date, "505643450130070402023039021c"},
		{"leap date", mustDate(t, 2000, 2, 29), "5056434501300704010207d0021d"},
		{"time", time, "5056434501310c173b3a080301017d03020103"},
		{"zero time", mustTime(t, 0, 0, 0, 0, 0), "5056434501310a00000006020000020000"},
		{"local date time", local, "50564345013215070402023039021c0c173b3a080301017d03020103"},
		{"offset date time", offset, "5056434501331b070402023039021c0c173b3a080301017d03020103050203014370"},
		{"zero offset", mustOffset(t, 0, 0, 1, 1, 0, 0, 0), "505643450133140502000001010a00000006020000020000020000"},
		{"entry mapping", mapping, "505643450142050102000000"},
		{"all seven in a sequence", NewArray(
			NewBinaryFloat32(0x7fc00001),
			NewBytes([]byte{0, 255}),
			date,
			time,
			local,
			offset,
			mapping,
		), "5056434501405e0712047fc0000121030200ff30070402023039021c310c173b3a080301017d030201033215070402023039021c0c173b3a080301017d03020103331b070402023039021c0c173b3a080301017d0302010305020301437042050102000000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			encoded := mustEncode(t, c.value)
			want, err := hex.DecodeString(c.want)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(encoded, want) {
				t.Fatalf("EncodePVCE = %x, want %x (Rust pin)", encoded, want)
			}
			// The golden bytes decode back to Equal values.
			decoded, err := DecodePVCE(encoded, DefaultDecodeLimits())
			if err != nil {
				t.Fatalf("DecodePVCE(golden) failed: %v", err)
			}
			if !Equal(decoded, c.value) {
				t.Errorf("DecodePVCE(golden) = %v, want equal %v", decoded, c.value)
			}
			// Re-encoding the decoded value is byte-stable.
			if reEncoded := mustEncode(t, decoded); !bytes.Equal(reEncoded, encoded) {
				t.Errorf("re-encode of decoded value is not byte-stable")
			}
		})
	}
}

func mustOffset(t *testing.T, offset int64, year int64, month, day, hour, minute, second uint8) OffsetDateTime {
	t.Helper()
	return mustOffsetFraction(t, offset, year, month, day, hour, minute, second, 0, 0)
}

func mustOffsetFraction(t *testing.T, offset int64, year int64, month, day, hour, minute, second uint8, coefficient, exponent int64) OffsetDateTime {
	t.Helper()
	date := mustDate(t, year, month, day)
	time := mustTime(t, hour, minute, second, coefficient, exponent)
	value, err := NewOffsetDateTime(NewLocalDateTime(date, time), int32(offset))
	if err != nil {
		t.Fatalf("NewOffsetDateTime(%d, ...) failed: %v", offset, err)
	}
	return value
}

// TestNewKindsRoundTrip mirrors the Rust every_core_kind_round_trips values
// (consema-rs/consema-pvce/src/lib.rs:1129-1174) for the additional kinds:
// every value round-trips byte-stably.
func TestNewKindsRoundTrip(t *testing.T) {
	date := mustDate(t, -12345, 2, 28)
	time := mustTime(t, 23, 59, 58, 125, -3)
	local := NewLocalDateTime(date, time)
	offset, err := NewOffsetDateTime(local, -23*60*60)
	if err != nil {
		t.Fatal(err)
	}
	emptyMapping, err := NewEntryMapping()
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := NewEntryMapping(
		EntryMappingEntry{Key: Boolean(true), Value: Null{}},
		EntryMappingEntry{Key: String("dup"), Value: String("x")},
		EntryMappingEntry{Key: String("dup"), Value: NewInteger(big.NewInt(1))},
		EntryMappingEntry{Key: NewArray(Null{}), Value: NewBytes([]byte{0xfe})},
	)
	if err != nil {
		t.Fatal(err)
	}
	values := []Value{
		NewBinaryFloat32(0),
		NewBinaryFloat32(0x7fc00001),
		NewBinaryFloat32(0x80000000),
		NewBytes(nil),
		NewBytes([]byte{0, 255}),
		NewBytes([]byte("中")),
		mustDate(t, -400, 2, 29),
		mustDate(t, 2000, 2, 29),
		mustDate(t, 0, 1, 1),
		mustDate(t, 999999999999999999, 12, 31),
		mustTime(t, 0, 0, 0, 0, 0),
		mustTime(t, 23, 59, 59, 999, -3),
		mustTime(t, 12, 0, 0, 1, -10),
		time,
		local,
		offset,
		mustOffset(t, 86399, 2024, 2, 29, 0, 0, 0),
		emptyMapping,
		mapping,
		NewArray(NewArray(NewBinaryFloat32(1), NewBytes([]byte{1, 2})), mustOffset(t, -90, -1, 12, 31, 1, 2, 3)),
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
		reEncoded := mustEncode(t, decoded)
		if !bytes.Equal(reEncoded, encoded) {
			t.Errorf("re-encode of %v is not byte-stable: %x vs %x", v, reEncoded, encoded)
		}
	}
}

// TestNewKindTemporalDecodeRejections drives the canonicality and validation
// rejections of the additional kinds, mirroring the Rust decode branches
// (consema-rs/consema-pvce/src/lib.rs:736-774, 895-940) and the
// map_build_error mapping (lib.rs:971-979).
func TestNewKindTemporalDecodeRejections(t *testing.T) {
	cases := []struct {
		name  string
		bytes []byte
		kind  PVCEErrorKind
	}{
		// Fixed-payload tags.
		{"float32 short payload", pvce(0x12, []byte{0x7f, 0xc0}), ErrInvalidPayload},
		{"float32 long payload", pvce(0x12, []byte{1, 2, 3, 4, 5}), ErrInvalidPayload},
		// Date validation (DecodeError::InvalidTemporal).
		{"date month zero", pvce(0x30, []byte{0x02, 0x00, 0x00, 0x00, 0x01}), ErrInvalidTemporal},
		{"date month thirteen", pvce(0x30, []byte{0x02, 0x00, 0x00, 0x0d, 0x01}), ErrInvalidTemporal},
		{"date day zero", pvce(0x30, []byte{0x02, 0x00, 0x00, 0x01, 0x00}), ErrInvalidTemporal},
		{"date day too large", pvce(0x30, []byte{0x02, 0x00, 0x00, 0x02, 0x1f}), ErrInvalidTemporal},
		{"date non-leap february 29", pvce(0x30, []byte{0x03, 0x01, 0x01, 0x64, 0x02, 0x1d}), ErrInvalidTemporal},
		// The Rust negative_year_leap_rule test
		// (consema-rs/consema-core/src/value.rs:1183-1186): year -400 is a leap
		// year (covered by the round-trip tests), year -100 is not.
		{"date non-leap negative year february 29", pvce(0x30, []byte{0x03, 0x02, 0x01, 0x64, 0x02, 0x1d}), ErrInvalidTemporal},
		// Time validation (DecodeError::InvalidTemporal).
		{"time hour 24", pvce(0x31, []byte{0x18, 0x00, 0x00, 0x06, 0x02, 0x00, 0x00, 0x02, 0x00, 0x00}), ErrInvalidTemporal},
		{"time minute 60", pvce(0x31, []byte{0x00, 0x3c, 0x00, 0x06, 0x02, 0x00, 0x00, 0x02, 0x00, 0x00}), ErrInvalidTemporal},
		{"time second 60", pvce(0x31, []byte{0x00, 0x00, 0x3c, 0x06, 0x02, 0x00, 0x00, 0x02, 0x00, 0x00}), ErrInvalidTemporal},
		// Fraction 1×10^0 is not below one: coefficient field (length 3:
		// sign 1, magnitude length 1, octet 1), exponent field zero.
		{"time fraction not below one", pvce(0x31, []byte{0x00, 0x00, 0x00, 0x07, 0x03, 0x01, 0x01, 0x01, 0x02, 0x00, 0x00}), ErrInvalidTemporal},
		// Fraction -1×10^0 is negative.
		{"time fraction negative", pvce(0x31, []byte{0x00, 0x00, 0x00, 0x07, 0x03, 0x02, 0x01, 0x01, 0x02, 0x00, 0x00}), ErrInvalidTemporal},
		// Offset validation (DecodeError::InvalidTemporal). The local part is
		// -400-02-29T00:00:00 (a leap year, so it decodes cleanly and only
		// the offset can fail).
		{"offset exactly 24 hours", pvce(0x33, []byte{
			0x07, 0x04, 0x02, 0x02, 0x01, 0x90, 0x02, 0x1d, // date field: year -400, month 2, day 29
			0x0a, 0x00, 0x00, 0x00, 0x06, 0x02, 0x00, 0x00, 0x02, 0x00, 0x00, // time field
			0x05, 0x01, 0x03, 0x01, 0x51, 0x80, // offset +86400
		}), ErrInvalidTemporal},
		{"offset below negative 24 hours", pvce(0x33, []byte{
			0x07, 0x04, 0x02, 0x02, 0x01, 0x90, 0x02, 0x1d,
			0x0a, 0x00, 0x00, 0x00, 0x06, 0x02, 0x00, 0x00, 0x02, 0x00, 0x00,
			0x05, 0x02, 0x03, 0x01, 0x51, 0x81, // offset -86401
		}), ErrInvalidTemporal},
		{"offset outside int32", pvce(0x33, []byte{
			0x07, 0x04, 0x02, 0x02, 0x01, 0x90, 0x02, 0x1d,
			0x0a, 0x00, 0x00, 0x00, 0x06, 0x02, 0x00, 0x00, 0x02, 0x00, 0x00,
			0x07, 0x01, 0x05, 0x01, 0x00, 0x00, 0x00, 0x00, // offset +2^32
		}), ErrInvalidTemporal},
		// Trailing bytes inside temporal fields.
		{"date year field trailing", pvce(0x30, []byte{0x03, 0x00, 0x00, 0x00, 0x01, 0x01}), ErrTrailingField},
		{"time fraction field trailing", pvce(0x31, []byte{0x00, 0x00, 0x00, 0x07, 0x02, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00}), ErrTrailingField},
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

// TestDateNegativeYearLeapRule mirrors the Rust
// negative_year_leap_rule_uses_absolute_remainders test
// (consema-rs/consema-core/src/value.rs:1183-1186): the leap rule operates on the
// absolute magnitude of the year, so -400 is a leap year and -100 is not.
func TestDateNegativeYearLeapRule(t *testing.T) {
	if _, err := NewDate(big.NewInt(-400), 2, 29); err != nil {
		t.Errorf("NewDate(-400, 2, 29) = %v, want a leap year", err)
	}
	if _, err := NewDate(big.NewInt(-100), 2, 29); err == nil {
		t.Error("NewDate(-100, 2, 29) succeeded, want an error")
	}
	if _, err := NewDate(big.NewInt(2000), 2, 29); err != nil {
		t.Errorf("NewDate(2000, 2, 29) = %v, want a leap year", err)
	}
	if _, err := NewDate(big.NewInt(1900), 2, 29); err == nil {
		t.Error("NewDate(1900, 2, 29) succeeded, want an error")
	}
	if _, err := NewDate(nil, 1, 1); err != nil {
		t.Errorf("NewDate(nil, 1, 1) = %v, want year zero accepted", err)
	}
}

// TestTimeFractionValidation pins the is_fraction rule (the Rust
// Decimal::is_fraction, consema-rs/consema-core/src/value.rs:337-352): the
// fractional second must be an exact finite decimal in [0, 1).
func TestTimeFractionValidation(t *testing.T) {
	valid := []struct {
		coefficient, exponent int64
	}{
		{0, 0}, {0, 5}, {125, -3}, {1, -1}, {999, -3}, {1, -10}, {7, -1},
	}
	for _, c := range valid {
		if _, err := NewTime(0, 0, 0, NewDecimal(big.NewInt(c.coefficient), big.NewInt(c.exponent))); err != nil {
			t.Errorf("fraction %d×10^%d rejected: %v", c.coefficient, c.exponent, err)
		}
	}
	invalid := []struct {
		coefficient, exponent int64
	}{
		{1, 0}, {125, -2}, {10, -1}, {-1, -1}, {-125, -3}, {1, 1},
	}
	for _, c := range invalid {
		if _, err := NewTime(0, 0, 0, NewDecimal(big.NewInt(c.coefficient), big.NewInt(c.exponent))); err == nil {
			t.Errorf("fraction %d×10^%d accepted, want rejection", c.coefficient, c.exponent)
		}
	}
}

// TestOffsetDateTimeValidation pins the 24-hour offset bound (the Rust
// OffsetDateTime::new, consema-rs/consema-core/src/value.rs:553-563).
func TestOffsetDateTimeValidation(t *testing.T) {
	local := NewLocalDateTime(mustDate(t, 2024, 1, 1), mustTime(t, 0, 0, 0, 0, 0))
	for _, offset := range []int32{0, 86399, -86399, -90} {
		if _, err := NewOffsetDateTime(local, offset); err != nil {
			t.Errorf("offset %d rejected: %v", offset, err)
		}
	}
	for _, offset := range []int32{86400, -86400, 1 << 30} {
		if _, err := NewOffsetDateTime(local, offset); err == nil {
			t.Errorf("offset %d accepted, want rejection", offset)
		}
	}
}

// TestBinaryFloat32Bits pins the exact-bit-pattern contract: -0.0, NaN
// payloads, and the like are preserved bit-exactly, and BinaryFloat32 never
// equals BinaryFloat64.
func TestBinaryFloat32Bits(t *testing.T) {
	negativeZero := uint32(0x80000000)
	b := NewBinaryFloat32(negativeZero)
	if b.Bits() != negativeZero {
		t.Errorf("Bits() = %#x, want %#x", b.Bits(), negativeZero)
	}
	if f := b.Float32(); !math.Signbit(float64(f)) || f != 0 {
		t.Errorf("Float32() = %v, want -0.0", f)
	}
	nanBits := uint32(0x7fc00001)
	nan := NewBinaryFloat32(nanBits)
	if !math.IsNaN(float64(nan.Float32())) || nan.Bits() != nanBits {
		t.Error("NaN payload was not preserved bit-exactly")
	}
	if got := FromFloat32(3.5).Bits(); got != math.Float32bits(3.5) {
		t.Errorf("FromFloat32(3.5).Bits() = %#x", got)
	}
	if Equal(NewBinaryFloat32(0), NewBinaryFloat64(0)) {
		t.Error("BinaryFloat32 and BinaryFloat64 must never be equal")
	}
}

// TestBytesSemantics pins the octet-sequence contract: Bytes copies on
// construction and access, and Bytes is never a String.
func TestBytesSemantics(t *testing.T) {
	original := []byte{0, 1, 0xfe, 0xff}
	value := NewBytes(original)
	original[0] = 0x99
	if !bytes.Equal(value.Content(), []byte{0, 1, 0xfe, 0xff}) {
		t.Error("NewBytes retained the caller's slice")
	}
	out := value.Content()
	out[0] = 0x55
	if !bytes.Equal(value.Content(), []byte{0, 1, 0xfe, 0xff}) {
		t.Error("Content exposed the wrapped slice")
	}
	if Equal(NewBytes([]byte("abc")), String("abc")) {
		t.Error("Bytes and String with the same content must not be equal")
	}
	if !Equal(NewBytes(nil), NewBytes([]byte{})) {
		t.Error("nil and empty Bytes must be equal (both are zero octets)")
	}
}

// TestEntryMappingSemantics pins the arbitrary-key duplicate-allowed
// contract (配置内容统一处理标准与 Rust 参考实现.md §10.9): duplicates and
// order are value semantics, and nil entries are rejected.
func TestEntryMappingSemantics(t *testing.T) {
	builder := NewEntryMappingBuilder()
	if err := builder.Push(String("k"), Null{}); err != nil {
		t.Fatal(err)
	}
	if err := builder.Push(String("k"), Boolean(true)); err != nil {
		t.Fatal(err)
	}
	if err := builder.Push(Null{}, NewInteger(big.NewInt(1))); err != nil {
		t.Fatal(err)
	}
	if builder.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", builder.Len())
	}
	mapping := builder.Build()
	entries := mapping.Entries()
	if len(entries) != 3 {
		t.Fatalf("Entries() length = %d, want 3", len(entries))
	}
	if !Equal(entries[0].Key, String("k")) || !Equal(entries[2].Key, Null{}) {
		t.Errorf("entries not preserved in order: %v", entries)
	}
	// Duplicate keys are value semantics, not errors.
	if err := builder.Push(String("k"), Null{}); err != nil {
		t.Errorf("duplicate key rejected: %v", err)
	}
	// Nil entries are rejected with the typed error.
	if err := builder.Push(nil, Null{}); !IsPVCEError(err, ErrInvalidValue) {
		t.Errorf("Push(nil, ...) = %v, want ErrInvalidValue", err)
	}
	if err := builder.Push(String("k"), nil); !IsPVCEError(err, ErrInvalidValue) {
		t.Errorf("Push(..., nil) = %v, want ErrInvalidValue", err)
	}
	if _, err := NewEntryMapping(EntryMappingEntry{Key: nil, Value: Null{}}); err == nil {
		t.Error("NewEntryMapping with nil key succeeded, want error")
	}
}

// TestNewKindEqualMatrix pins strict equality for the additional kinds:
// kind identity plus canonical content, order- and duplicate-sensitive for
// entry mappings.
func TestNewKindEqualMatrix(t *testing.T) {
	date := mustDate(t, 2024, 2, 29)
	time := mustTime(t, 12, 34, 56, 125, -3)
	local := NewLocalDateTime(date, time)
	offset, _ := NewOffsetDateTime(local, -90)
	mappingA, _ := NewEntryMapping(
		EntryMappingEntry{Key: String("k"), Value: Null{}},
		EntryMappingEntry{Key: String("k"), Value: Boolean(true)},
	)
	mappingB, _ := NewEntryMapping(
		EntryMappingEntry{Key: String("k"), Value: Boolean(true)},
		EntryMappingEntry{Key: String("k"), Value: Null{}},
	)
	cases := []struct {
		name string
		a, b Value
		want bool
	}{
		{"float32 bits", NewBinaryFloat32(0x7fc00001), NewBinaryFloat32(0x7fc00001), true},
		{"float32 differs", NewBinaryFloat32(0), NewBinaryFloat32(1), false},
		{"float32 sign of zero", NewBinaryFloat32(0), NewBinaryFloat32(0x80000000), false},
		{"bytes equal", NewBytes([]byte{1, 2}), NewBytes([]byte{1, 2}), true},
		{"bytes differs", NewBytes([]byte{1, 2}), NewBytes([]byte{1, 3}), false},
		{"bytes empty vs null kind", NewBytes(nil), Null{}, false},
		{"date equal", date, mustDate(t, 2024, 2, 29), true},
		{"date differs", date, mustDate(t, 2024, 2, 28), false},
		{"date year differs", date, mustDate(t, 2023, 2, 28), false},
		{"time equal", time, mustTime(t, 12, 34, 56, 125, -3), true},
		{"time fraction differs", time, mustTime(t, 12, 34, 56, 126, -3), false},
		{"time second differs", time, mustTime(t, 12, 34, 55, 125, -3), false},
		{"local equal", local, NewLocalDateTime(mustDate(t, 2024, 2, 29), time), true},
		{"local date differs", local, NewLocalDateTime(mustDate(t, 2024, 2, 28), time), false},
		{"offset equal", offset, mustOffsetFraction(t, -90, 2024, 2, 29, 12, 34, 56, 125, -3), true},
		{"offset differs", offset, mustOffsetFraction(t, -89, 2024, 2, 29, 12, 34, 56, 125, -3), false},
		{"entry mapping same order", mappingA, mustEntryMapping(t, String("k"), Null{}, String("k"), Boolean(true)), true},
		{"entry mapping reordered", mappingA, mappingB, false},
		{"entry mapping different values", mappingA, mustEntryMapping(t, String("k"), Null{}, String("k"), Null{}), false},
		{"entry mapping vs object", mappingA, mustObject(t, Entry{"k", Null{}}), false},
	}
	for _, c := range cases {
		if got := Equal(c.a, c.b); got != c.want {
			t.Errorf("%s: Equal = %v, want %v", c.name, got, c.want)
		}
		if got := Equal(c.b, c.a); got != c.want {
			t.Errorf("%s (symmetric): Equal = %v, want %v", c.name, got, c.want)
		}
	}
}

func mustEntryMapping(t *testing.T, entries ...Value) *EntryMapping {
	t.Helper()
	pairs := make([]EntryMappingEntry, 0, len(entries)/2)
	for i := 0; i+1 < len(entries); i += 2 {
		pairs = append(pairs, EntryMappingEntry{Key: entries[i], Value: entries[i+1]})
	}
	mapping, err := NewEntryMapping(pairs...)
	if err != nil {
		t.Fatalf("NewEntryMapping(%v) failed: %v", entries, err)
	}
	return mapping
}

// TestNewKindHashConsistency pins the RFC 0016 §4.1 contract for the
// additional kinds: Equal values always hash equal and Equal(a, b) holds
// exactly when the canonical PVCE/1 bytes are identical.
func TestNewKindHashConsistency(t *testing.T) {
	values := []Value{
		NewBinaryFloat32(0x7fc00001),
		NewBinaryFloat32(0x80000000),
		NewBytes([]byte{0, 255}),
		NewBytes([]byte{0, 1}),
		mustDate(t, -12345, 2, 28),
		mustDate(t, -12345, 2, 27),
		mustTime(t, 23, 59, 58, 125, -3),
		mustTime(t, 23, 59, 58, 126, -3),
		NewLocalDateTime(mustDate(t, 2024, 2, 29), mustTime(t, 1, 2, 3, 0, 0)),
		mustOffset(t, -90, 2024, 2, 29, 1, 2, 3),
		mustOffset(t, 90, 2024, 2, 29, 1, 2, 3),
		mustEntryMapping(t, String("k"), Null{}),
		mustEntryMapping(t, Null{}, String("k")),
	}
	for _, a := range values {
		encodedA := mustEncode(t, a)
		for _, b := range values {
			equal := Equal(a, b)
			byteEqual := bytes.Equal(encodedA, mustEncode(t, b))
			if equal != byteEqual {
				t.Errorf("Equal(%v, %v) = %v but bytes equal = %v", a, b, equal, byteEqual)
			}
			if equal && Hash(a) != Hash(b) {
				t.Errorf("Equal values %v and %v hash differently", a, b)
			}
		}
	}
	if Hash(NewBytes(nil)) != Hash(NewBytes([]byte{})) {
		t.Error("nil and empty Bytes hash differently")
	}
}

// TestNewKindDecodeLimits pins the additional kinds' resource-limit fields
// (the Rust decode limits, consema-rs/consema-pvce/src/lib.rs:848-940). The
// outer "decimal-field"/"date-field"/"time-field" caps are defensive
// (max_integer_bytes×2+32 / +32 / ×2+64) and, exactly as in the Rust
// decoder, a canonical value that passes the inner integer-bytes limit can
// never exceed them, so the effective limit is always the integer one.
func TestNewKindDecodeLimits(t *testing.T) {
	date := mustDate(t, 2024, 2, 29)
	time := mustTime(t, 12, 34, 56, 125, -3)
	cases := []struct {
		name   string
		value  Value
		field  string
		adjust func(*DecodeLimits)
	}{
		{"bytes blob", NewBytes([]byte{1, 2, 3}), "blob-bytes", func(l *DecodeLimits) { l.MaxBlobBytes = 2 }},
		{"date year integer", date, "integer-bytes", func(l *DecodeLimits) { l.MaxIntegerBytes = 1 }},
		{"time fraction integer", time, "integer-bytes", func(l *DecodeLimits) { l.MaxIntegerBytes = 0 }},
		{"local year integer", NewLocalDateTime(date, time), "integer-bytes", func(l *DecodeLimits) { l.MaxIntegerBytes = 0 }},
		{"offset year integer", mustOffset(t, -90, 2024, 2, 29, 12, 34, 56), "integer-bytes", func(l *DecodeLimits) { l.MaxIntegerBytes = 0 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			encoded := mustEncode(t, c.value)
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
			if target.Field != c.field {
				t.Errorf("resource-limit field = %q, want %q", target.Field, c.field)
			}
		})
	}
}

// TestNewKindEncodeBoundedLimits pins the additional kinds' encode-time
// limits through the bounded encoder.
func TestNewKindEncodeBoundedLimits(t *testing.T) {
	date := mustDate(t, 2024, 2, 29)
	time := mustTime(t, 12, 34, 56, 125, -3)
	offset, _ := NewOffsetDateTime(NewLocalDateTime(date, time), -90)
	cases := []struct {
		name   string
		value  Value
		field  string
		adjust func(*EncodeLimits)
	}{
		{"bytes blob", NewBytes([]byte{1, 2, 3}), "blob-bytes", func(l *EncodeLimits) { l.MaxBlobBytes = 2 }},
		{"date year integer", date, "integer-bytes", func(l *EncodeLimits) { l.MaxIntegerBytes = 1 }},
		{"time fraction integer", time, "integer-bytes", func(l *EncodeLimits) { l.MaxIntegerBytes = 0 }},
		{"offset integer", offset, "integer-bytes", func(l *EncodeLimits) { l.MaxIntegerBytes = 0 }},
		{"entry mapping container", mustEntryMapping(t, String("k"), Null{}, String("k"), Null{}), "container-entries", func(l *EncodeLimits) { l.MaxContainerEntries = 1 }},
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
			if target.Field != c.field {
				t.Errorf("resource-limit field = %q, want %q", target.Field, c.field)
			}
		})
	}
}

// TestZeroValueDateIsInvalidCodecInput pins the documented zero-value
// caveat: the zero Date (and LocalDateTime/OffsetDateTime containing it) is
// invalid input to the codec and fails with ErrInvalidValue, mirroring the
// typed-nil pointer discipline. The zero Time is a valid time.
func TestZeroValueDateIsInvalidCodecInput(t *testing.T) {
	var zeroDate Date
	if _, err := EncodePVCE(zeroDate); !IsPVCEError(err, ErrInvalidValue) {
		t.Errorf("EncodePVCE(zero Date) = %v, want ErrInvalidValue", err)
	}
	var zeroLocal LocalDateTime
	if _, err := EncodePVCE(zeroLocal); !IsPVCEError(err, ErrInvalidValue) {
		t.Errorf("EncodePVCE(zero LocalDateTime) = %v, want ErrInvalidValue", err)
	}
	zeroOffset := OffsetDateTime{local: zeroLocal}
	if _, err := EncodePVCE(zeroOffset); !IsPVCEError(err, ErrInvalidValue) {
		t.Errorf("EncodePVCE(zero OffsetDateTime) = %v, want ErrInvalidValue", err)
	}
	var zeroTime Time
	encoded, err := EncodePVCE(zeroTime)
	if err != nil {
		t.Fatalf("EncodePVCE(zero Time) failed: %v", err)
	}
	decoded, err := DecodePVCE(encoded, DefaultDecodeLimits())
	if err != nil || !Equal(decoded, zeroTime) {
		t.Errorf("zero Time round trip = (%v, %v)", decoded, err)
	}
	// Construction errors carry the frozen registered code.
	if got := (&PVCEError{Kind: ErrInvalidTemporal}).Code(); got != "core.pvce.invalid-temporal@1" {
		t.Errorf("ErrInvalidTemporal code = %q, want core.pvce.invalid-temporal@1", got)
	}
}
