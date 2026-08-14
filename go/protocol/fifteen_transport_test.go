package protocol

import (
	"math/big"
	"testing"

	"consema.dev/consema/core"
)

// ---------------------------------------------------------------------------
// Canonical JSON golden vectors for the seven additional kinds.
//
// The vectors below are byte-identical to the Rust canonical JSON encoder
// output (consema-rs/consema-protocol/src/value_transport.rs), generated with
// the reference encoder itself for the exact values of the Rust
// every_core_kind_round_trips_through_both_transports test
// (value_transport.rs).
// ---------------------------------------------------------------------------

func TestFifteenKindJSONGoldenVectors(t *testing.T) {
	date, err := core.NewDate(big.NewInt(-44), 3, 15)
	if err != nil {
		t.Fatal(err)
	}
	time, err := core.NewTime(12, 34, 56, core.NewDecimal(big.NewInt(125), big.NewInt(-3)))
	if err != nil {
		t.Fatal(err)
	}
	local := core.NewLocalDateTime(date, time)
	offset, err := core.NewOffsetDateTime(local, -90)
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := core.NewEntryMapping(core.EntryMappingEntry{Key: core.String("k"), Value: core.NullValue()})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		value core.Value
		want  string
	}{
		{"binary float32", core.NewBinaryFloat32(0x7fc00001),
			`{"schema":"core.portable-value-json@1","value":{"type":"BinaryFloat32","bits":"7fc00001"}}`},
		{"bytes", core.NewBytes([]byte{0, 1, 0xfe, 0xff}),
			`{"schema":"core.portable-value-json@1","value":{"type":"Bytes","hex":"0001feff"}}`},
		{"date", date,
			`{"schema":"core.portable-value-json@1","value":{"type":"Date","year":"-44","month":"3","day":"15"}}`},
		{"time", time,
			`{"schema":"core.portable-value-json@1","value":{"type":"Time","hour":"12","minute":"34","second":"56","fraction":{"type":"Decimal","coefficient":"125","exponent":"-3"}}}`},
		{"local date time", local,
			`{"schema":"core.portable-value-json@1","value":{"type":"LocalDateTime","date":{"type":"Date","year":"-44","month":"3","day":"15"},"time":{"type":"Time","hour":"12","minute":"34","second":"56","fraction":{"type":"Decimal","coefficient":"125","exponent":"-3"}}}}`},
		{"offset date time", offset,
			`{"schema":"core.portable-value-json@1","value":{"type":"OffsetDateTime","local":{"type":"LocalDateTime","date":{"type":"Date","year":"-44","month":"3","day":"15"},"time":{"type":"Time","hour":"12","minute":"34","second":"56","fraction":{"type":"Decimal","coefficient":"125","exponent":"-3"}}},"offset_seconds":"-90"}}`},
		{"entry mapping", mapping,
			`{"schema":"core.portable-value-json@1","value":{"type":"EntryMapping","entries":[{"key":{"type":"String","value":"k"},"value":{"type":"Null"}}]}}`},
	}
	limits := DefaultProtocolLimits()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			encoded, err := EncodeJSON(c.value, limits)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != c.want {
				t.Fatalf("EncodeJSON = %q, want %q (Rust pin)", encoded, c.want)
			}
			decoded, err := DecodeJSON([]byte(c.want), limits)
			if err != nil {
				t.Fatalf("DecodeJSON(golden) failed: %v", err)
			}
			if !core.Equal(decoded, c.value) {
				t.Errorf("DecodeJSON(golden) = %v, want equal %v", decoded, c.value)
			}
		})
	}
}

// TestFifteenKindsRoundTripBothTransports mirrors the Rust
// every_core_kind_round_trips_through_both_transports test
// (consema-rs/consema-protocol/src/value_transport.rs): a value
// exercising every kind round-trips through the canonical JSON transport and
// the PVCE transport.
func TestFifteenKindsRoundTripBothTransports(t *testing.T) {
	date, err := core.NewDate(big.NewInt(-44), 3, 15)
	if err != nil {
		t.Fatal(err)
	}
	fraction := core.NewDecimal(big.NewInt(125), big.NewInt(-3))
	time, err := core.NewTime(12, 34, 56, fraction)
	if err != nil {
		t.Fatal(err)
	}
	local := core.NewLocalDateTime(date, time)
	offset, err := core.NewOffsetDateTime(local, -90)
	if err != nil {
		t.Fatal(err)
	}
	object, err := core.NewObject(core.Entry{Key: "x", Value: core.Boolean(true)})
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := core.NewEntryMapping(core.EntryMappingEntry{Key: core.String("k"), Value: core.NullValue()})
	if err != nil {
		t.Fatal(err)
	}
	value := core.NewArray(
		core.NullValue(),
		core.Boolean(false),
		core.NewInteger(mustBigInt(t, "12345678901234567890")),
		core.NewDecimal(big.NewInt(123), big.NewInt(-2)),
		core.NewBinaryFloat32(0x7fc00001),
		core.NewBinaryFloat64(0x8000000000000000),
		core.String("quote \" slash \\ newline\n 世界"),
		core.NewBytes([]byte{0, 1, 0xfe, 0xff}),
		date,
		time,
		local,
		offset,
		core.NewArray(core.String("nested")),
		object,
		mapping,
	)
	limits := DefaultProtocolLimits()
	jsonBytes, err := EncodeJSON(value, limits)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeJSON(jsonBytes, limits)
	if err != nil {
		t.Fatal(err)
	}
	if !core.Equal(decoded, value) {
		t.Error("JSON round-trip changed the value")
	}
	pvceBytes, err := EncodePVCE(value, limits)
	if err != nil {
		t.Fatal(err)
	}
	decodedPVCE, err := DecodePVCE(pvceBytes, limits)
	if err != nil {
		t.Fatal(err)
	}
	if !core.Equal(decodedPVCE, value) {
		t.Error("PVCE round-trip changed the value")
	}
	// The v1.json projection.best-exact-duplicate-mapping case carries an
	// EntryMapping value through the protocol transports (conformance/
	// vectors/v1.json:89-93): duplicate keys and arbitrary key values are
	// preserved exactly.
	duplicates, err := core.NewEntryMapping(
		core.EntryMappingEntry{Key: core.String("a"), Value: core.NewInteger(big.NewInt(1))},
		core.EntryMappingEntry{Key: core.String("a"), Value: core.NewInteger(big.NewInt(2))},
		core.EntryMappingEntry{Key: core.NewInteger(big.NewInt(7)), Value: core.NullValue()},
	)
	if err != nil {
		t.Fatal(err)
	}
	for name, transport := range map[string]func(core.Value) (core.Value, error){
		"json": func(v core.Value) (core.Value, error) {
			bytes, err := EncodeJSON(v, limits)
			if err != nil {
				return nil, err
			}
			return DecodeJSON(bytes, limits)
		},
		"pvce": func(v core.Value) (core.Value, error) {
			bytes, err := EncodePVCE(v, limits)
			if err != nil {
				return nil, err
			}
			return DecodePVCE(bytes, limits)
		},
	} {
		round, err := transport(duplicates)
		if err != nil {
			t.Fatalf("%s entry-mapping transport failed: %v", name, err)
		}
		if !core.Equal(round, duplicates) {
			t.Errorf("%s entry-mapping round trip changed the value", name)
		}
	}
}

// TestEncodeJSONRootNodeCountedOnce pins the fix for the root double-count:
// the Rust encoder counts the root value exactly once
// (value_transport.rs), so EncodeJSON(Null{}, MaxNodes:1) succeeds,
// and a one-entry object needs two nodes.
func TestEncodeJSONRootNodeCountedOnce(t *testing.T) {
	limits := DefaultProtocolLimits()
	limits.MaxNodes = 1
	if _, err := EncodeJSON(core.NullValue(), limits); err != nil {
		t.Errorf("EncodeJSON(Null, MaxNodes=1) = %v, want success (root counted once)", err)
	}
	// A one-entry object is 2 records (object + value), so it still fails
	// at MaxNodes=1 and succeeds at MaxNodes=2.
	object, err := core.NewObject(core.Entry{Key: "a", Value: core.Boolean(true)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeJSON(object, limits); err == nil || protocolCode(err) != "core.protocol.resource-limit@1" {
		t.Errorf("EncodeJSON(1-entry object, MaxNodes=1) = %v, want resource limit", err)
	}
	limits.MaxNodes = 2
	if _, err := EncodeJSON(object, limits); err != nil {
		t.Errorf("EncodeJSON(1-entry object, MaxNodes=2) = %v, want success", err)
	}
	// DecodeJSON counts the root once too (the Rust DecodeState, same rule).
	nullVector := `{"schema":"core.portable-value-json@1","value":{"type":"Null"}}`
	limits = DefaultProtocolLimits()
	limits.MaxNodes = 1
	if _, err := DecodeJSON([]byte(nullVector), limits); err != nil {
		t.Errorf("DecodeJSON(Null, MaxNodes=1) = %v, want success", err)
	}
}

// TestCanonicalJSONNumberToken drives the bare-JSON-number parse path
// (numberToken, e.g. {"type":"Integer","value":123}): the strict parser
// forms the number token and only then rejects it as the wrong member type,
// exactly as the Rust decoder does (value_transport.rs).
func TestCanonicalJSONNumberToken(t *testing.T) {
	limits := DefaultProtocolLimits()
	// A bare number where a JSON string is required is WrongType, not a
	// syntax error.
	integerNumber := `{"schema":"core.portable-value-json@1","value":{"type":"Integer","value":123}}`
	if _, err := DecodeJSON([]byte(integerNumber), limits); err == nil || protocolCode(err) != "core.protocol.wrong-type@1" {
		t.Errorf("integer with number value: got %v", err)
	}
	// The same shape inside a sequence item and a nested decimal member.
	arrayNumber := `{"schema":"core.portable-value-json@1","value":{"type":"Sequence","items":[{"type":"Integer","value":1.5}]}}`
	if _, err := DecodeJSON([]byte(arrayNumber), limits); err == nil || protocolCode(err) != "core.protocol.wrong-type@1" {
		t.Errorf("array number item: got %v", err)
	}
	decimalNumber := `{"schema":"core.portable-value-json@1","value":{"type":"Decimal","coefficient":123,"exponent":"-2"}}`
	if _, err := DecodeJSON([]byte(decimalNumber), limits); err == nil || protocolCode(err) != "core.protocol.wrong-type@1" {
		t.Errorf("decimal coefficient number: got %v", err)
	}
	// Malformed number tokens are invalid JSON.
	for name, malformed := range map[string]string{
		"leading zero":  `{"schema":"core.portable-value-json@1","value":{"type":"Integer","value":01}}`,
		"bare sign":     `{"schema":"core.portable-value-json@1","value":{"type":"Integer","value":-}}`,
		"dangling dot":  `{"schema":"core.portable-value-json@1","value":{"type":"Integer","value":1.}}`,
		"dangling exp":  `{"schema":"core.portable-value-json@1","value":{"type":"Integer","value":1e}}`,
		"exp bare sign": `{"schema":"core.portable-value-json@1","value":{"type":"Integer","value":1e+}}`,
	} {
		if _, err := DecodeJSON([]byte(malformed), limits); err == nil || protocolCode(err) != "core.protocol.invalid-json@1" {
			t.Errorf("%s: got %v", name, err)
		}
	}
	// A well-formed number token inside the envelope decodes when the
	// surrounding text is canonical JSON but the value is still rejected as
	// the wrong type (the token path is fully covered either way).
	nullNumber := `{"schema":"core.portable-value-json@1","value":{"type":"Null","x":42}}`
	if _, err := DecodeJSON([]byte(nullNumber), limits); err == nil || protocolCode(err) != "core.protocol.unknown-field@1" {
		t.Errorf("number in unknown field: got %v", err)
	}
}

// TestUnicodeEscapeSurrogatePairs drives the surrogate-pair escape paths of
// the strict parser (stringToken/unicodeEscape).
func TestUnicodeEscapeSurrogatePairs(t *testing.T) {
	limits := DefaultProtocolLimits()
	escape := func(text string) string {
		return `{"schema":"core.portable-value-json@1","value":{"type":"String","value":` + text + `}}`
	}
	// Lone high surrogate: rejected as invalid JSON.
	if _, err := DecodeJSON([]byte(escape(`"\ud800"`)), limits); err == nil || protocolCode(err) != "core.protocol.invalid-json@1" {
		t.Errorf("lone high surrogate: got %v", err)
	}
	// Lone low surrogate.
	if _, err := DecodeJSON([]byte(escape(`"\udc00"`)), limits); err == nil || protocolCode(err) != "core.protocol.invalid-json@1" {
		t.Errorf("lone low surrogate: got %v", err)
	}
	// Truncated \u escape.
	if _, err := DecodeJSON([]byte(escape(`"\u12"`)), limits); err == nil || protocolCode(err) != "core.protocol.invalid-json@1" {
		t.Errorf("truncated \\u escape: got %v", err)
	}
	// High surrogate followed by a non-surrogate escape.
	if _, err := DecodeJSON([]byte(escape(`"\ud800A"`)), limits); err == nil || protocolCode(err) != "core.protocol.invalid-json@1" {
		t.Errorf("high surrogate with non-low partner: got %v", err)
	}
	// High surrogate followed by raw text instead of an escape.
	if _, err := DecodeJSON([]byte(escape(`"\ud800x"`)), limits); err == nil || protocolCode(err) != "core.protocol.invalid-json@1" {
		t.Errorf("high surrogate with raw partner: got %v", err)
	}
	// A valid surrogate pair (😀) decodes to the scalar, but the
	// canonical byte form emits the raw UTF-8 text, so the escaped input is
	// valid but non-canonical (the Rust re-encode canonicality check,
	// value_transport.rs).
	if _, err := DecodeJSON([]byte(escape(`"`+"\\ud83d\\ude00"+`"`)), limits); err == nil || protocolCode(err) != "core.protocol.non-canonical-json@1" {
		t.Errorf("valid surrogate pair: got %v", err)
	}
	// The same scalar transported as raw UTF-8 is the canonical form.
	raw := `{"schema":"core.portable-value-json@1","value":{"type":"String","value":"😀"}}`
	decoded, err := DecodeJSON([]byte(raw), limits)
	if err != nil {
		t.Fatal(err)
	}
	if !core.Equal(decoded, core.String("😀")) {
		t.Errorf("raw surrogate scalar decoded to %v", decoded)
	}
}

// TestNewKindJSONCanonicalityRejections drives the canonicality rules of the
// additional kinds: alternate spellings are valid JSON that decode, then
// fail the re-encode check.
func TestNewKindJSONCanonicalityRejections(t *testing.T) {
	limits := DefaultProtocolLimits()
	cases := []struct {
		name string
		json string
	}{
		{"uppercase float32 bits", `{"schema":"core.portable-value-json@1","value":{"type":"BinaryFloat32","bits":"7FC00001"}}`},
		{"uppercase float64 bits", `{"schema":"core.portable-value-json@1","value":{"type":"BinaryFloat64","bits":"ABCDEF0123456789"}}`},
		{"uppercase bytes hex", `{"schema":"core.portable-value-json@1","value":{"type":"Bytes","hex":"00FF"}}`},
		{"month with leading zero", `{"schema":"core.portable-value-json@1","value":{"type":"Date","year":"-44","month":"03","day":"15"}}`},
		{"year with leading zero", `{"schema":"core.portable-value-json@1","value":{"type":"Date","year":"-0044","month":"3","day":"15"}}`},
	}
	for _, c := range cases {
		if _, err := DecodeJSON([]byte(c.json), limits); err == nil || protocolCode(err) != "core.protocol.non-canonical-json@1" {
			t.Errorf("%s: got %v", c.name, err)
		}
	}
}

// TestNewKindJSONDecodeRejections drives the validation failures of the
// additional kinds through the JSON transport.
func TestNewKindJSONDecodeRejections(t *testing.T) {
	limits := DefaultProtocolLimits()
	cases := []struct {
		name string
		json string
		code string
	}{
		{"invalid date", `{"schema":"core.portable-value-json@1","value":{"type":"Date","year":"2023","month":"2","day":"29"}}`, "core.protocol.invalid-value@1"},
		{"invalid time", `{"schema":"core.portable-value-json@1","value":{"type":"Time","hour":"24","minute":"0","second":"0","fraction":{"type":"Decimal","coefficient":"0","exponent":"0"}}}`, "core.protocol.invalid-value@1"},
		{"fraction not decimal", `{"schema":"core.portable-value-json@1","value":{"type":"Time","hour":"0","minute":"0","second":"0","fraction":{"type":"Null"}}}`, "core.protocol.wrong-type@1"},
		{"local date wrong kind", `{"schema":"core.portable-value-json@1","value":{"type":"LocalDateTime","date":{"type":"Null"},"time":{"type":"Time","hour":"0","minute":"0","second":"0","fraction":{"type":"Decimal","coefficient":"0","exponent":"0"}}}}`, "core.protocol.wrong-type@1"},
		{"offset local wrong kind", `{"schema":"core.portable-value-json@1","value":{"type":"OffsetDateTime","local":{"type":"Date","year":"2024","month":"1","day":"1"},"offset_seconds":"0"}}`, "core.protocol.wrong-type@1"},
		{"offset outside i32", `{"schema":"core.portable-value-json@1","value":{"type":"OffsetDateTime","local":{"type":"LocalDateTime","date":{"type":"Date","year":"2024","month":"1","day":"1"},"time":{"type":"Time","hour":"0","minute":"0","second":"0","fraction":{"type":"Decimal","coefficient":"0","exponent":"0"}}},"offset_seconds":"9999999999999"}}`, "core.protocol.invalid-value@1"},
		{"month outside u8", `{"schema":"core.portable-value-json@1","value":{"type":"Date","year":"2024","month":"300","day":"1"}}`, "core.protocol.invalid-value@1"},
		{"odd byte hex", `{"schema":"core.portable-value-json@1","value":{"type":"Bytes","hex":"0"}}`, "core.protocol.invalid-value@1"},
		{"invalid byte hex", `{"schema":"core.portable-value-json@1","value":{"type":"Bytes","hex":"0g"}}`, "core.protocol.invalid-value@1"},
		{"float32 bits length", `{"schema":"core.portable-value-json@1","value":{"type":"BinaryFloat32","bits":"7fc0000"}}`, "core.protocol.invalid-value@1"},
		{"float32 bits non hex", `{"schema":"core.portable-value-json@1","value":{"type":"BinaryFloat32","bits":"zzzzzzzz"}}`, "core.protocol.invalid-value@1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := DecodeJSON([]byte(c.json), limits)
			if err == nil || protocolCode(err) != c.code {
				t.Fatalf("DecodeJSON = %v, want code %s", err, c.code)
			}
		})
	}
	// Bytes payloads beyond MaxBlobBytes are a ResourceLimit (the Rust
	// parse_hex_bytes, value_transport.rs).
	limited := DefaultProtocolLimits()
	limited.MaxBlobBytes = 2
	bigBytes := `{"schema":"core.portable-value-json@1","value":{"type":"Bytes","hex":"ffffff"}}`
	if _, err := DecodeJSON([]byte(bigBytes), limited); err == nil || protocolCode(err) != "core.protocol.resource-limit@1" {
		t.Errorf("bytes over blob limit: got %v", err)
	}
}
