package protocol

import (
	"math/big"
	"testing"

	"consema.dev/consema/core"
)

// protocolCode extracts the registered code of a protocol error for
// assertions; the error interface hides the typed error.
func protocolCode(err error) string {
	if e, ok := err.(*ProtocolError); ok {
		return e.Code()
	}
	return ""
}

func mustBigInt(t *testing.T, text string) *big.Int {
	t.Helper()
	value, ok := new(big.Int).SetString(text, 10)
	if !ok {
		t.Fatalf("invalid big integer %q", text)
	}
	return value
}

func TestCanonicalJSONNullVectorBytes(t *testing.T) {
	// protocol.json.null-vector pins the canonical bytes.
	const expected = `{"schema":"core.portable-value-json@1","value":{"type":"Null"}}`
	bytes, err := EncodeJSON(core.NullValue(), DefaultProtocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes) != expected {
		t.Errorf("null vector = %q, want %q", bytes, expected)
	}
	value, err := DecodeJSON([]byte(expected), DefaultProtocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	if value.Kind() != core.KindNull {
		t.Error("decoded null vector has wrong kind")
	}
}

func TestAllEightKindsRoundTripBothTransports(t *testing.T) {
	object, err := core.NewObject(
		core.Entry{Key: "x", Value: core.Boolean(true)},
		core.Entry{Key: "z", Value: core.NewArray(core.String("nested"), core.NullValue())},
		core.Entry{Key: "big", Value: core.NewInteger(mustBigInt(t, "12345678901234567890"))},
		core.Entry{Key: "decimal", Value: core.NewDecimal(big.NewInt(123), big.NewInt(-2))},
		core.Entry{Key: "float", Value: core.NewBinaryFloat64(0x8000000000000000)},
	)
	if err != nil {
		t.Fatal(err)
	}
	value := core.NewArray(
		core.NullValue(), core.Boolean(false), core.String("quote \" slash \\ newline\n 世界"),
		core.NewInteger(big.NewInt(-256)), object,
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
}

func TestCanonicalJSONRejectionRules(t *testing.T) {
	limits := DefaultProtocolLimits()
	// protocol.json.reject-whitespace: leading whitespace parses but is not
	// canonical.
	nullVector := `{"schema":"core.portable-value-json@1","value":{"type":"Null"}}`
	if _, err := DecodeJSON(append([]byte(" "), nullVector...), limits); err == nil || protocolCode(err) != "core.protocol.non-canonical-json@1" {
		t.Errorf("reject-whitespace: got %v", err)
	}
	// protocol.json.reject-alternate-escape: the \u003C escape decodes to <
	// but the canonical byte form emits < raw.
	alternate := "{\"schema\":\"core.portable-value-json@1\",\"value\":{\"type\":\"String\"," +
		"\"value\":\"\\u003C\"}}"
	if _, err := DecodeJSON([]byte(alternate), limits); err == nil || protocolCode(err) != "core.protocol.non-canonical-json@1" {
		t.Errorf("reject-alternate-escape: got %v", err)
	}
	// protocol.json.reject-unknown-field: a tagged record with an extra
	// field.
	unknown := `{"schema":"core.portable-value-json@1","value":{"type":"Null","extra":true}}`
	if _, err := DecodeJSON([]byte(unknown), limits); err == nil || protocolCode(err) != "core.protocol.unknown-field@1" {
		t.Errorf("reject-unknown-field: got %v", err)
	}
	// Invalid JSON syntax.
	if _, err := DecodeJSON([]byte(`{`), limits); err == nil || protocolCode(err) != "core.protocol.invalid-json@1" {
		t.Errorf("invalid json: got %v", err)
	}
	// Duplicate member names are invalid JSON.
	duplicate := `{"schema":"core.portable-value-json@1","schema":"x","value":{"type":"Null"}}`
	if _, err := DecodeJSON([]byte(duplicate), limits); err == nil || protocolCode(err) != "core.protocol.invalid-json@1" {
		t.Errorf("duplicate member: got %v", err)
	}
	// The additional kinds of the fifteen-kind contract are fully supported;
	// a Bytes vector decodes and re-encodes canonically.
	bytesValue := `{"schema":"core.portable-value-json@1","value":{"type":"Bytes","hex":"00"}}`
	decodedBytes, err := DecodeJSON([]byte(bytesValue), limits)
	if err != nil {
		t.Errorf("Bytes kind: got %v", err)
	}
	if err == nil && !core.Equal(decodedBytes, core.NewBytes([]byte{0})) {
		t.Errorf("Bytes kind decoded to %v, want [0]", decodedBytes)
	}
}

func TestCanonicalJSONResourceLimits(t *testing.T) {
	// Nesting beyond the protocol depth is a ResourceLimit.
	limits := DefaultProtocolLimits()
	limits.MaxDepth = 2
	depth := `{"schema":"core.portable-value-json@1","value":` +
		`{"type":"Sequence","items":[{"type":"Sequence","items":[{"type":"Sequence","items":[{"type":"Null"}]}]}]}}`
	if _, err := DecodeJSON([]byte(depth), limits); err == nil || protocolCode(err) != "core.protocol.resource-limit@1" {
		t.Errorf("depth limit: got %v", err)
	}
	// Oversized integer digits are a ResourceLimit.
	limits = DefaultProtocolLimits()
	limits.MaxIntegerBytes = 4
	oversized := `{"schema":"core.portable-value-json@1","value":{"type":"Integer","value":"` +
		"999999999999999" + `"}}`
	if _, err := DecodeJSON([]byte(oversized), limits); err == nil || protocolCode(err) != "core.protocol.resource-limit@1" {
		t.Errorf("integer limit: got %v", err)
	}
	// Encoding beyond max_bytes is a ResourceLimit.
	limits = DefaultProtocolLimits()
	limits.MaxBytes = 10
	if _, err := EncodeJSON(core.NullValue(), limits); err == nil || protocolCode(err) != "core.protocol.resource-limit@1" {
		t.Errorf("byte limit: got %v", err)
	}
}

func TestCanonicalJSONStringEscapingIsExact(t *testing.T) {
	// The control-character escapes match the Rust encoder
	// (value_transport.rs:137-162).
	value := core.String("a\"b\\c\bd\te\nf\fc\rd\x01e")
	bytes, err := EncodeJSON(value, DefaultProtocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	expected := "{\"schema\":\"core.portable-value-json@1\",\"value\":{\"type\":\"String\"," +
		"\"value\":\"a\\\"b\\\\c\\bd\\te\\nf\\fc\\rd\\u0001e\"}}"
	if string(bytes) != expected {
		t.Errorf("escaping = %q, want %q", bytes, expected)
	}
}

func TestPVCEErrorMapping(t *testing.T) {
	// A truncated PVCE stream maps to the invalid-pvce code.
	limits := DefaultProtocolLimits()
	if _, err := DecodePVCE([]byte("PVCE"), limits); err == nil || protocolCode(err) != "core.protocol.invalid-pvce@1" {
		t.Errorf("invalid pvce: got %v", err)
	}
	// Exceeding the byte budget is a ResourceLimit.
	limits = DefaultProtocolLimits()
	limits.MaxBytes = 2
	if _, err := DecodePVCE([]byte("PVCE"), limits); err == nil || protocolCode(err) != "core.protocol.resource-limit@1" {
		t.Errorf("pvce resource limit: got %v", err)
	}
}
