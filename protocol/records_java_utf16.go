package protocol

// Exact Java UTF-16 code-unit strings: the `core.java-utf16-string@1` record
// (crates/consema-protocol/src/java_utf16.rs). Java strings are transported
// as canonical big-endian UTF-16 units; byte and unit forms are
// cross-verified on decode.

import (
	"consema.dev/consema/core"
)

// JavaUnicodeStatus reports whether an exact Java string is also
// well-formed Unicode (java_utf16.rs:17-24).
type JavaUnicodeStatus string

// The two frozen statuses.
const (
	JavaWellFormedUnicode JavaUnicodeStatus = "WellFormedUnicode"
	JavaUnpairedSurrogate JavaUnicodeStatus = "UnpairedSurrogate"
)

// String returns the canonical status spelling.
func (s JavaUnicodeStatus) String() string { return string(s) }

// JavaUtf16String is the exact Java string content transported as canonical
// big-endian UTF-16 units (java_utf16.rs:27-36).
type JavaUtf16String struct {
	codeUnits     []uint16
	bytes         []byte
	unicodeStatus JavaUnicodeStatus
}

// NewJavaUtf16String builds an exact string while enforcing the same limits
// as wire decoding (java_utf16.rs:41-66).
func NewJavaUtf16String(codeUnits []uint16, limits ProtocolLimits) (*JavaUtf16String, error) {
	if len(codeUnits) > int(limits.MaxContainerEntries) {
		return nil, resource("$.code_units", "code-unit count exceeds the configured container limit")
	}
	byteLen := len(codeUnits) * 2
	if byteLen > int(limits.MaxBlobBytes) {
		return nil, resource("$.bytes", "UTF-16 bytes exceed the configured blob limit")
	}
	bytes := make([]byte, 0, byteLen)
	for _, unit := range codeUnits {
		bytes = append(bytes, byte(unit>>8), byte(unit))
	}
	return &JavaUtf16String{
		codeUnits:     append([]uint16(nil), codeUnits...),
		bytes:         bytes,
		unicodeStatus: classifyJavaUTF16(codeUnits),
	}, nil
}

// CodeUnits returns the exact ordered UTF-16 code units.
func (s *JavaUtf16String) CodeUnits() []uint16 { return append([]uint16(nil), s.codeUnits...) }

// Bytes returns the same units as BOM-free, big-endian bytes.
func (s *JavaUtf16String) Bytes() []byte { return append([]byte(nil), s.bytes...) }

// UnicodeStatus returns the recomputed well-formedness classification.
func (s *JavaUtf16String) UnicodeStatus() JavaUnicodeStatus { return s.unicodeStatus }

// ToValue encodes `core.java-utf16-string@1` in canonical field order
// (java_utf16.rs:64-82).
func (s *JavaUtf16String) ToValue() (core.Value, error) {
	units := make([]core.Value, 0, len(s.codeUnits))
	for _, unit := range s.codeUnits {
		units = append(units, core.String(uppercaseHex4(unit)))
	}
	array := core.NewArray(units...)
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.java-utf16-string@1")},
		core.Entry{Key: "encoding", Value: core.String("UTF16BE/1")},
		core.Entry{Key: "code_units", Value: array},
		core.Entry{Key: "bytes", Value: core.NewBytes(s.bytes)},
		core.Entry{Key: "unicode_status", Value: core.String(string(s.unicodeStatus))},
	)
}

// FromValue strictly decodes and canonically re-verifies one exact Java
// string (java_utf16.rs:84-146).
func (s *JavaUtf16String) FromValue(value core.Value, limits ProtocolLimits) (*JavaUtf16String, error) {
	fields, err := schemaFields(value, "core.java-utf16-string@1",
		[]string{"schema", "encoding", "code_units", "bytes", "unicode_status"}, "$")
	if err != nil {
		return nil, err
	}
	encoding, err := stringOf(fields[1], "$.encoding")
	if err != nil {
		return nil, err
	}
	if encoding != "UTF16BE/1" {
		return nil, invalid("$.encoding", "expected exact encoding UTF16BE/1")
	}
	encodedUnits, err := sequenceOf(fields[2], "$.code_units")
	if err != nil {
		return nil, err
	}
	if len(encodedUnits) > int(limits.MaxContainerEntries) {
		return nil, resource("$.code_units", "code-unit count exceeds the configured container limit")
	}
	bytes, ok := fields[3].(core.Bytes)
	if !ok {
		return nil, protocolError(KindWrongType, "$.bytes", "expected Bytes")
	}
	if len(bytes) > int(limits.MaxBlobBytes) {
		return nil, resource("$.bytes", "UTF-16 bytes exceed the configured blob limit")
	}
	if len(bytes)%2 != 0 {
		return nil, invalid("$.bytes", "UTF-16 byte length must be even")
	}
	expectedBytes := len(encodedUnits) * 2
	if len(bytes) != expectedBytes {
		return nil, invalid("$.bytes", "byte count does not equal two bytes per code unit")
	}
	codeUnits := make([]uint16, 0, len(encodedUnits))
	for index, encoded := range encodedUnits {
		path := "$.code_units[" + uint32String(uint32(index)) + "]"
		text, err := stringOf(encoded, path)
		if err != nil {
			return nil, err
		}
		unit, ok := parseJavaUnit(text)
		if !ok {
			return nil, invalid(path, "code unit must be exactly four uppercase hexadecimal digits")
		}
		offset := index * 2
		if byte(unit>>8) != bytes[offset] || byte(unit) != bytes[offset+1] {
			return nil, invalid(path, "code unit and byte representation differ")
		}
		codeUnits = append(codeUnits, unit)
	}
	statusText, err := stringOf(fields[4], "$.unicode_status")
	if err != nil {
		return nil, err
	}
	var status JavaUnicodeStatus
	switch JavaUnicodeStatus(statusText) {
	case JavaWellFormedUnicode, JavaUnpairedSurrogate:
		status = JavaUnicodeStatus(statusText)
	default:
		return nil, invalid("$.unicode_status", "unknown Java Unicode status")
	}
	decoded, err := NewJavaUtf16String(codeUnits, limits)
	if err != nil {
		return nil, err
	}
	if decoded.unicodeStatus != status {
		return nil, invalid("$.unicode_status", "status does not match exact surrogate pairing")
	}
	canonical, err := decoded.ToValue()
	if err != nil {
		return nil, err
	}
	if !core.Equal(canonical, value) {
		return nil, invalid("$", "Java UTF-16 string is not canonically encoded")
	}
	return decoded, nil
}

// parseJavaUnit accepts exactly four uppercase hexadecimal digits
// (java_utf16.rs:152-162).
func parseJavaUnit(value string) (uint16, bool) {
	if len(value) != 4 {
		return 0, false
	}
	var unit uint16
	for index := 0; index < 4; index++ {
		byte := value[index]
		var digit uint16
		switch {
		case byte >= '0' && byte <= '9':
			digit = uint16(byte - '0')
		case byte >= 'A' && byte <= 'F':
			digit = uint16(byte-'A') + 10
		default:
			return 0, false
		}
		unit = unit<<4 | digit
	}
	return unit, true
}

// uppercaseHex4 formats one code unit as exactly four uppercase hex digits.
func uppercaseHex4(unit uint16) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{
		digits[unit>>12&0xF], digits[unit>>8&0xF], digits[unit>>4&0xF], digits[unit&0xF],
	})
}

// classifyJavaUTF16 recomputes the surrogate-pair classification
// (java_utf16.rs:164-176).
func classifyJavaUTF16(units []uint16) JavaUnicodeStatus {
	for index := 0; index < len(units); {
		unit := units[index]
		switch {
		case unit >= 0xD800 && unit <= 0xDBFF:
			if index+1 < len(units) && units[index+1] >= 0xDC00 && units[index+1] <= 0xDFFF {
				index += 2
				continue
			}
			return JavaUnpairedSurrogate
		case unit >= 0xDC00 && unit <= 0xDFFF:
			return JavaUnpairedSurrogate
		default:
			index++
		}
	}
	return JavaWellFormedUnicode
}
