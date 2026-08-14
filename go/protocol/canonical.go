package protocol

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"

	"consema.dev/consema/core"
)

// This file implements the canonical tagged JSON transport
// `core.portable-value-json@1` (RFC 0015 §3.2; RFC 0016 §4.2), byte-identical
// to the Rust transport (consema-rs/consema-protocol/src/value_transport.rs).
//
// The decoder is a strict JSON parser (no comments, no trailing commas,
// duplicate members rejected, canonical string/number forms only, followed by
// a byte-exact canonicality re-encode check), mirroring the Rust
// formation + re-encode discipline. The encoder emits the exact canonical
// bytes: no whitespace, minimal string escapes, integer values as decimal
// strings.
//
// The tagged representation covers the closed fifteen-kind Go model (RFC
// 0016 §4.1), byte-identical to the Rust transport for every kind
// (value_transport.rs).

// PortableValueJSONSchema is the canonical tagged JSON transport schema.
const PortableValueJSONSchema = "core.portable-value-json@1"

// jsonNodeKind identifies one strict-JSON tree node.
type jsonNodeKind uint8

const (
	jsonNull jsonNodeKind = iota
	jsonBool
	jsonString
	jsonNumber
	jsonArray
	jsonObject
)

// jsonField is one ordered object member.
type jsonField struct {
	key   string
	value *jsonNode
}

// jsonNode is a strict-JSON parse tree. Numbers keep their raw token text;
// strings hold decoded text.
type jsonNode struct {
	kind   jsonNodeKind
	text   string      // String text or Number raw token
	truth  bool        // Bool value
	items  []*jsonNode // Array items
	fields []jsonField // Object members in source order
}

// parser is the strict JSON parser state.
type parser struct {
	bytes  []byte
	pos    int
	limits ProtocolLimits
	nodes  int
}

// parseJSONDocument strictly parses one complete JSON document
// (consema-rs/consema-protocol/src/value_transport.rs, using the strict
// JSON profile of consema-json). Any syntax error, duplicate member, or
// trailing content yields KindInvalidJson; parse-level resource bounds use
// the generous mapped limits of the Rust path.
func parseJSONDocument(bytes []byte, limits ProtocolLimits) (*jsonNode, error) {
	if len(bytes) > limits.MaxBytes {
		return nil, resource("$", "transport bytes")
	}
	p := &parser{bytes: bytes, limits: limits}
	node, err := p.value(0, "$")
	if err != nil {
		return nil, err
	}
	p.skipWhitespace()
	if p.pos != len(p.bytes) {
		return nil, protocolError(KindInvalidJson, "$", "trailing content")
	}
	return node, nil
}

// skipWhitespace advances past JSON whitespace.
func (p *parser) skipWhitespace() {
	for p.pos < len(p.bytes) {
		switch p.bytes[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

// value parses one JSON value at the given tree depth.
func (p *parser) value(depth int, path string) (*jsonNode, error) {
	if depth > p.limits.MaxDepth*4+8 {
		return nil, resource(path, "nesting depth")
	}
	p.nodes++
	if p.nodes > p.limits.MaxNodes*16+32 {
		return nil, resource(path, "value nodes")
	}
	p.skipWhitespace()
	if p.pos >= len(p.bytes) {
		return nil, protocolError(KindInvalidJson, "$", "expected a value")
	}
	switch p.bytes[p.pos] {
	case '{':
		return p.object(depth, path)
	case '[':
		return p.array(depth, path)
	case '"':
		text, err := p.stringToken(path)
		if err != nil {
			return nil, err
		}
		if len(text) > p.limits.MaxBlobBytes {
			return nil, resource(path, "string bytes")
		}
		return &jsonNode{kind: jsonString, text: text}, nil
	case 't':
		if p.literal("true") {
			return &jsonNode{kind: jsonBool, truth: true}, nil
		}
	case 'f':
		if p.literal("false") {
			return &jsonNode{kind: jsonBool, truth: false}, nil
		}
	case 'n':
		if p.literal("null") {
			return &jsonNode{kind: jsonNull}, nil
		}
	default:
		if p.bytes[p.pos] == '-' || (p.bytes[p.pos] >= '0' && p.bytes[p.pos] <= '9') {
			token, err := p.numberToken()
			if err != nil {
				return nil, err
			}
			return &jsonNode{kind: jsonNumber, text: token}, nil
		}
	}
	return nil, protocolError(KindInvalidJson, "$", "unexpected character")
}

// literal consumes one exact word and reports success.
func (p *parser) literal(word string) bool {
	if len(p.bytes)-p.pos < len(word) || string(p.bytes[p.pos:p.pos+len(word)]) != word {
		return false
	}
	p.pos += len(word)
	return true
}

// object parses a JSON object, rejecting duplicate member names.
func (p *parser) object(depth int, path string) (*jsonNode, error) {
	p.pos++ // '{'
	node := &jsonNode{kind: jsonObject}
	p.skipWhitespace()
	if p.pos < len(p.bytes) && p.bytes[p.pos] == '}' {
		p.pos++
		return node, nil
	}
	seen := make(map[string]struct{})
	for {
		p.skipWhitespace()
		key, err := p.stringToken(path)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[key]; exists {
			return nil, protocolError(KindInvalidJson, "$", "duplicate member name")
		}
		seen[key] = struct{}{}
		if len(key) > p.limits.MaxBlobBytes {
			return nil, resource(path, "string bytes")
		}
		p.skipWhitespace()
		if p.pos >= len(p.bytes) || p.bytes[p.pos] != ':' {
			return nil, protocolError(KindInvalidJson, "$", "expected ':'")
		}
		p.pos++
		value, err := p.value(depth+1, path)
		if err != nil {
			return nil, err
		}
		node.fields = append(node.fields, jsonField{key: key, value: value})
		p.skipWhitespace()
		if p.pos >= len(p.bytes) {
			return nil, protocolError(KindInvalidJson, "$", "unterminated object")
		}
		switch p.bytes[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return node, nil
		default:
			return nil, protocolError(KindInvalidJson, "$", "expected ',' or '}'")
		}
	}
}

// array parses a JSON array.
func (p *parser) array(depth int, path string) (*jsonNode, error) {
	p.pos++ // '['
	node := &jsonNode{kind: jsonArray}
	p.skipWhitespace()
	if p.pos < len(p.bytes) && p.bytes[p.pos] == ']' {
		p.pos++
		return node, nil
	}
	for {
		item, err := p.value(depth+1, path)
		if err != nil {
			return nil, err
		}
		node.items = append(node.items, item)
		p.skipWhitespace()
		if p.pos >= len(p.bytes) {
			return nil, protocolError(KindInvalidJson, "$", "unterminated array")
		}
		switch p.bytes[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return node, nil
		default:
			return nil, protocolError(KindInvalidJson, "$", "expected ',' or ']'")
		}
	}
}

// stringToken parses one JSON string token (with escapes and surrogate
// pairs) and returns its decoded text.
func (p *parser) stringToken(path string) (string, error) {
	p.pos++ // opening quote
	var builder strings.Builder
	for {
		if p.pos >= len(p.bytes) {
			return "", protocolError(KindInvalidJson, path, "unterminated string")
		}
		byte := p.bytes[p.pos]
		switch {
		case byte == '"':
			p.pos++
			return builder.String(), nil
		case byte == '\\':
			p.pos++
			if p.pos >= len(p.bytes) {
				return "", protocolError(KindInvalidJson, path, "unterminated escape")
			}
			switch p.bytes[p.pos] {
			case '"', '\\', '/':
				builder.WriteByte(p.bytes[p.pos])
				p.pos++
			case 'b':
				builder.WriteByte('\b')
				p.pos++
			case 'f':
				builder.WriteByte('\f')
				p.pos++
			case 'n':
				builder.WriteByte('\n')
				p.pos++
			case 'r':
				builder.WriteByte('\r')
				p.pos++
			case 't':
				builder.WriteByte('\t')
				p.pos++
			case 'u':
				p.pos++ // advance past the 'u' to the first hex digit
				rune, err := p.unicodeEscape(path)
				if err != nil {
					return "", err
				}
				builder.WriteRune(rune)
			default:
				return "", protocolError(KindInvalidJson, path, "invalid escape")
			}
		case byte < 0x20:
			return "", protocolError(KindInvalidJson, path, "raw control character")
		default:
			// Copy one full UTF-8 sequence; a partial sequence is invalid
			// JSON text and must not be silently replaced.
			start := p.pos
			p.pos++
			for p.pos < len(p.bytes) && p.bytes[p.pos]&0xc0 == 0x80 {
				p.pos++
			}
			text := p.bytes[start:p.pos]
			if !utf8.Valid(text) {
				return "", protocolError(KindInvalidJson, path, "invalid UTF-8")
			}
			builder.Write(text)
		}
	}
}

// unicodeEscape decodes one \uXXXX escape, combining surrogate pairs.
func (p *parser) unicodeEscape(path string) (rune, error) {
	value, err := p.hexQuad(path)
	if err != nil {
		return 0, err
	}
	if value >= 0xd800 && value <= 0xdbff {
		// High surrogate: require a following \uDC00-\uDFFF.
		if p.pos+1 < len(p.bytes) && p.bytes[p.pos] == '\\' && p.bytes[p.pos+1] == 'u' {
			p.pos += 2
			low, err := p.hexQuad(path)
			if err != nil {
				return 0, err
			}
			if low < 0xdc00 || low > 0xdfff {
				return 0, protocolError(KindInvalidJson, path, "invalid surrogate pair")
			}
			return 0x10000 + (rune(value)-0xd800)<<10 + (rune(low) - 0xdc00), nil
		}
		return 0, protocolError(KindInvalidJson, path, "lone high surrogate")
	}
	if value >= 0xdc00 && value <= 0xdfff {
		return 0, protocolError(KindInvalidJson, path, "lone low surrogate")
	}
	return rune(value), nil
}

// hexQuad parses exactly four hexadecimal digits.
func (p *parser) hexQuad(path string) (uint16, error) {
	if p.pos+4 > len(p.bytes) {
		return 0, protocolError(KindInvalidJson, path, "truncated \\u escape")
	}
	var value uint16
	for index := 0; index < 4; index++ {
		digit := p.bytes[p.pos+index]
		value <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			value |= uint16(digit - '0')
		case digit >= 'a' && digit <= 'f':
			value |= uint16(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			value |= uint16(digit-'A') + 10
		default:
			return 0, protocolError(KindInvalidJson, path, "invalid \\u escape")
		}
	}
	p.pos += 4
	return value, nil
}

// numberToken parses one strict JSON number token and returns its raw text.
func (p *parser) numberToken() (string, error) {
	start := p.pos
	if p.bytes[p.pos] == '-' {
		p.pos++
	}
	// Integer part.
	if p.pos >= len(p.bytes) {
		return "", protocolError(KindInvalidJson, "$", "invalid number")
	}
	if p.bytes[p.pos] == '0' {
		p.pos++
	} else if p.bytes[p.pos] >= '1' && p.bytes[p.pos] <= '9' {
		for p.pos < len(p.bytes) && p.bytes[p.pos] >= '0' && p.bytes[p.pos] <= '9' {
			p.pos++
		}
	} else {
		return "", protocolError(KindInvalidJson, "$", "invalid number")
	}
	// Fraction part.
	if p.pos < len(p.bytes) && p.bytes[p.pos] == '.' {
		p.pos++
		if p.pos >= len(p.bytes) || p.bytes[p.pos] < '0' || p.bytes[p.pos] > '9' {
			return "", protocolError(KindInvalidJson, "$", "invalid number fraction")
		}
		for p.pos < len(p.bytes) && p.bytes[p.pos] >= '0' && p.bytes[p.pos] <= '9' {
			p.pos++
		}
	}
	// Exponent part.
	if p.pos < len(p.bytes) && (p.bytes[p.pos] == 'e' || p.bytes[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.bytes) && (p.bytes[p.pos] == '+' || p.bytes[p.pos] == '-') {
			p.pos++
		}
		if p.pos >= len(p.bytes) || p.bytes[p.pos] < '0' || p.bytes[p.pos] > '9' {
			return "", protocolError(KindInvalidJson, "$", "invalid number exponent")
		}
		for p.pos < len(p.bytes) && p.bytes[p.pos] >= '0' && p.bytes[p.pos] <= '9' {
			p.pos++
		}
	}
	return string(p.bytes[start:p.pos]), nil
}

// decodeState tracks the resource counts of a value-tree decode.
type decodeState struct {
	limits ProtocolLimits
	nodes  int
}

func (s *decodeState) node(depth int, path string) error {
	if depth > s.limits.MaxDepth {
		return resource(path, "nesting depth")
	}
	s.nodes++
	if s.nodes > s.limits.MaxNodes {
		return resource(path, "value nodes")
	}
	return nil
}

func (s *decodeState) container(count int, path string) error {
	if count > s.limits.MaxContainerEntries {
		return resource(path, "container entries")
	}
	return nil
}

// EncodeJSON encodes a PortableValue as canonical `core.portable-value-json@1`
// bytes, byte-identical to the Rust encoder
// (consema-rs/consema-protocol/src/value_transport.rs).
func EncodeJSON(value core.Value, limits ProtocolLimits) ([]byte, error) {
	var builder strings.Builder
	if err := encodeTransportNode(&builder, valueToNode(value), limits); err != nil {
		return nil, err
	}
	return []byte(builder.String()), nil
}

// encodeTransportNode writes the full canonical envelope around one tagged
// value tree. The root value is counted exactly once: value itself calls
// node, matching the Rust JsonEncoder (value_transport.rs) — there
// is no separate node count for the envelope (the Rust encoder
// encode_json, value_transport.rs).
func encodeTransportNode(builder *strings.Builder, valueNode *jsonNode, limits ProtocolLimits) error {
	state := jsonEncoderState{limits: limits, output: builder}
	if err := state.push(`{"schema":"` + PortableValueJSONSchema + `","value":`); err != nil {
		return err
	}
	if err := state.value(valueNode, 0, "$.value"); err != nil {
		return err
	}
	return state.push("}")
}

// DecodeJSON strictly decodes canonical `core.portable-value-json@1` bytes
// and returns the transported PortableValue
// (consema-rs/consema-protocol/src/value_transport.rs). The record decode
// runs before the canonicality re-encode check, matching the Rust ordering
// (a resource-limit or field error is reported before a non-canonical form).
func DecodeJSON(bytes []byte, limits ProtocolLimits) (core.Value, error) {
	node, err := parseJSONDocument(bytes, limits)
	if err != nil {
		return nil, err
	}
	fields, err := jsonObjectExact(node, []string{"schema", "value"}, "$")
	if err != nil {
		return nil, err
	}
	schema, err := jsonStringOf(fields[0], "$.schema")
	if err != nil {
		return nil, err
	}
	if schema != PortableValueJSONSchema {
		return nil, protocolError(KindSchemaMismatch, "$.schema", "unexpected transport schema")
	}
	state := &decodeState{limits: limits}
	value, err := nodeToValue(fields[1], 0, "$.value", state)
	if err != nil {
		return nil, err
	}
	if err := ensureCanonical(node, bytes, limits); err != nil {
		return nil, err
	}
	return value, nil
}

// ensureCanonical re-encodes the parsed document's value and requires byte
// equality with the input (the Rust re-encode canonicality check,
// value_transport.rs). Re-encoding works on the parse tree, which
// preserves field order and decoded text; any valid-but-non-canonical form
// (whitespace, alternate escapes, reordered fields, non-minimal numbers)
// therefore differs.
func ensureCanonical(node *jsonNode, input []byte, limits ProtocolLimits) error {
	var valueNode *jsonNode
	for _, field := range node.fields {
		if field.key == "value" {
			valueNode = field.value
		}
	}
	if valueNode == nil {
		// Malformed envelope; the record decode reports the missing field.
		return nil
	}
	var builder strings.Builder
	if err := encodeTransportNode(&builder, valueNode, limits); err != nil {
		return err
	}
	if builder.String() != string(input) {
		return protocolError(KindNonCanonicalJson, "$", "input is valid but not the canonical JSON byte form")
	}
	return nil
}

// jsonEncoderState is the canonical encoder with protocol resource checks.
type jsonEncoderState struct {
	limits ProtocolLimits
	nodes  int
	output *strings.Builder
}

func (s *jsonEncoderState) push(text string) error {
	if s.output.Len()+len(text) > s.limits.MaxBytes {
		return resource("$", "transport bytes")
	}
	s.output.WriteString(text)
	return nil
}

func (s *jsonEncoderState) node(depth int, path string) error {
	if depth > s.limits.MaxDepth {
		return resource(path, "nesting depth")
	}
	s.nodes++
	if s.nodes > s.limits.MaxNodes {
		return resource(path, "value nodes")
	}
	return nil
}

func (s *jsonEncoderState) container(count int, path string) error {
	if count > s.limits.MaxContainerEntries {
		return resource(path, "container entries")
	}
	return nil
}

func (s *jsonEncoderState) quoted(value string, path string) error {
	if len(value) > s.limits.MaxBlobBytes {
		return resource(path, "string bytes")
	}
	if err := s.push(`"`); err != nil {
		return err
	}
	for _, character := range value {
		var escaped string
		switch character {
		case '"':
			escaped = `\"`
		case '\\':
			escaped = `\\`
		case '\b':
			escaped = `\b`
		case '\t':
			escaped = `\t`
		case '\n':
			escaped = `\n`
		case '\f':
			escaped = `\f`
		case '\r':
			escaped = `\r`
		default:
			if character < 0x20 {
				escaped = fmt.Sprintf(`\u%04x`, character)
			} else {
				if err := s.push(string(character)); err != nil {
					return err
				}
				continue
			}
		}
		if err := s.push(escaped); err != nil {
			return err
		}
	}
	return s.push(`"`)
}

func (s *jsonEncoderState) integer(value *big.Int, path string) error {
	if len(value.Bytes()) > s.limits.MaxIntegerBytes {
		return resource(path, "integer magnitude")
	}
	return s.quoted(value.String(), path)
}

func (s *jsonEncoderState) value(node *jsonNode, depth int, path string) error {
	if err := s.node(depth, path); err != nil {
		return err
	}
	// A tagged value is a JSON object whose first member is "type". The
	// encoder dispatches on the tagged kind and re-emits the canonical byte
	// form of the decoded value: integers and decimals are normalized to
	// their canonical decimal spellings, strings re-escape from decoded
	// text, and byte hex is lowercased (the Rust re-encode semantics,
	// value_transport.rs).
	if node.kind != jsonObject || len(node.fields) == 0 || node.fields[0].key != "type" {
		return invalid(path, "unrepresentable value")
	}
	kind := node.fields[0].value
	if kind.kind != jsonString {
		return protocolError(KindWrongType, path+".type", "expected String")
	}
	member := func(name string) *jsonNode {
		for _, field := range node.fields {
			if field.key == name {
				return field.value
			}
		}
		return nil
	}
	switch kind.text {
	case "Null":
		return s.push(`{"type":"Null"}`)
	case "Boolean":
		value := member("value")
		if value == nil || value.kind != jsonBool {
			return invalid(path, "unrepresentable value")
		}
		if value.truth {
			return s.push(`{"type":"Boolean","value":true}`)
		}
		return s.push(`{"type":"Boolean","value":false}`)
	case "String":
		text, err := jsonStringOf(member("value"), path+".value")
		if err != nil {
			return err
		}
		if err := s.push(`{"type":"String","value":`); err != nil {
			return err
		}
		if err := s.quoted(text, path); err != nil {
			return err
		}
		return s.push(`}`)
	case "Integer":
		text, err := jsonStringOf(member("value"), path+".value")
		if err != nil {
			return err
		}
		integer, ok := new(big.Int).SetString(text, 10)
		if !ok {
			return invalid(path+".value", "invalid integer")
		}
		if err := s.push(`{"type":"Integer","value":`); err != nil {
			return err
		}
		if err := s.integer(integer, path); err != nil {
			return err
		}
		return s.push(`}`)
	case "Decimal":
		coefficientText, err := jsonStringOf(member("coefficient"), path+".coefficient")
		if err != nil {
			return err
		}
		exponentText, err := jsonStringOf(member("exponent"), path+".exponent")
		if err != nil {
			return err
		}
		coefficient, ok := new(big.Int).SetString(coefficientText, 10)
		if !ok {
			return invalid(path+".coefficient", "invalid integer")
		}
		exponent, ok := new(big.Int).SetString(exponentText, 10)
		if !ok {
			return invalid(path+".exponent", "invalid integer")
		}
		if err := s.push(`{"type":"Decimal","coefficient":`); err != nil {
			return err
		}
		if err := s.integer(coefficient, path); err != nil {
			return err
		}
		if err := s.push(`,"exponent":`); err != nil {
			return err
		}
		if err := s.integer(exponent, path); err != nil {
			return err
		}
		return s.push(`}`)
	case "BinaryFloat32":
		bits, err := jsonStringOf(member("bits"), path+".bits")
		if err != nil {
			return err
		}
		// The canonical form is eight lowercase hex digits (the Rust
		// {:08x} format, value_transport.rs); any other spelling
		// fails the re-encode canonicality check.
		if err := s.push(`{"type":"BinaryFloat32","bits":`); err != nil {
			return err
		}
		if err := s.quoted(strings.ToLower(bits), path); err != nil {
			return err
		}
		return s.push(`}`)
	case "BinaryFloat64":
		bits, err := jsonStringOf(member("bits"), path+".bits")
		if err != nil {
			return err
		}
		// The canonical form is sixteen lowercase hex digits (the Rust
		// {:016x} format, value_transport.rs); any other spelling
		// fails the re-encode canonicality check.
		if err := s.push(`{"type":"BinaryFloat64","bits":`); err != nil {
			return err
		}
		if err := s.quoted(strings.ToLower(bits), path); err != nil {
			return err
		}
		return s.push(`}`)
	case "Bytes":
		// The hex is lowercased to the canonical form (the Rust encoder
		// re-emits from the decoded octets, value_transport.rs);
		// any other spelling fails the re-encode canonicality check.
		hex, err := jsonStringOf(member("hex"), path+".hex")
		if err != nil {
			return err
		}
		lower := strings.ToLower(hex)
		if err := s.push(`{"type":"Bytes","hex":`); err != nil {
			return err
		}
		if err := s.quoted(lower, path); err != nil {
			return err
		}
		return s.push(`}`)
	case "Date":
		yearText, err := jsonStringOf(member("year"), path+".year")
		if err != nil {
			return err
		}
		year, ok := new(big.Int).SetString(yearText, 10)
		if !ok {
			return invalid(path+".year", "invalid integer")
		}
		month, err := jsonParseU8(member("month"), path+".month", s.limits)
		if err != nil {
			return err
		}
		day, err := jsonParseU8(member("day"), path+".day", s.limits)
		if err != nil {
			return err
		}
		if err := s.push(`{"type":"Date","year":`); err != nil {
			return err
		}
		if err := s.integer(year, path); err != nil {
			return err
		}
		if err := s.push(`,"month":`); err != nil {
			return err
		}
		// Month and day are normalized to their canonical decimal spellings,
		// exactly as the Rust encoder formats from the parsed fields
		// (value_transport.rs).
		if err := s.quoted(strconv.FormatUint(uint64(month), 10), path); err != nil {
			return err
		}
		if err := s.push(`,"day":`); err != nil {
			return err
		}
		if err := s.quoted(strconv.FormatUint(uint64(day), 10), path); err != nil {
			return err
		}
		return s.push(`}`)
	case "Time":
		hour, err := jsonParseU8(member("hour"), path+".hour", s.limits)
		if err != nil {
			return err
		}
		minute, err := jsonParseU8(member("minute"), path+".minute", s.limits)
		if err != nil {
			return err
		}
		second, err := jsonParseU8(member("second"), path+".second", s.limits)
		if err != nil {
			return err
		}
		fraction := member("fraction")
		if fraction == nil {
			return invalid(path+".fraction", "unrepresentable value")
		}
		if err := s.push(`{"type":"Time","hour":`); err != nil {
			return err
		}
		// Hour, minute, and second are normalized to their canonical decimal
		// spellings (value_transport.rs).
		if err := s.quoted(strconv.FormatUint(uint64(hour), 10), path); err != nil {
			return err
		}
		if err := s.push(`,"minute":`); err != nil {
			return err
		}
		if err := s.quoted(strconv.FormatUint(uint64(minute), 10), path); err != nil {
			return err
		}
		if err := s.push(`,"second":`); err != nil {
			return err
		}
		if err := s.quoted(strconv.FormatUint(uint64(second), 10), path); err != nil {
			return err
		}
		if err := s.push(`,"fraction":`); err != nil {
			return err
		}
		if err := s.value(fraction, depth+1, path); err != nil {
			return err
		}
		return s.push(`}`)
	case "LocalDateTime":
		date := member("date")
		time := member("time")
		if date == nil || time == nil {
			return invalid(path, "unrepresentable value")
		}
		if err := s.push(`{"type":"LocalDateTime","date":`); err != nil {
			return err
		}
		if err := s.value(date, depth+1, path); err != nil {
			return err
		}
		if err := s.push(`,"time":`); err != nil {
			return err
		}
		if err := s.value(time, depth+1, path); err != nil {
			return err
		}
		return s.push(`}`)
	case "OffsetDateTime":
		local := member("local")
		if local == nil {
			return invalid(path, "unrepresentable value")
		}
		offset, err := jsonParseI32(member("offset_seconds"), path+".offset_seconds", s.limits)
		if err != nil {
			return err
		}
		if err := s.push(`{"type":"OffsetDateTime","local":`); err != nil {
			return err
		}
		if err := s.value(local, depth+1, path); err != nil {
			return err
		}
		if err := s.push(`,"offset_seconds":`); err != nil {
			return err
		}
		// The offset is normalized to its canonical decimal spelling
		// (value_transport.rs).
		if err := s.quoted(strconv.FormatInt(int64(offset), 10), path); err != nil {
			return err
		}
		return s.push(`}`)
	case "Sequence":
		items := member("items")
		if items == nil || items.kind != jsonArray {
			return protocolError(KindWrongType, path+".items", "expected JSON array")
		}
		if err := s.container(len(items.items), path); err != nil {
			return err
		}
		if err := s.push(`{"type":"Sequence","items":[`); err != nil {
			return err
		}
		for index, item := range items.items {
			if index != 0 {
				if err := s.push(","); err != nil {
					return err
				}
			}
			if err := s.value(item, depth+1, fmt.Sprintf("%s.items[%d]", path, index)); err != nil {
				return err
			}
		}
		return s.push(`]}`)
	case "Object":
		entries := member("entries")
		if entries == nil || entries.kind != jsonArray {
			return protocolError(KindWrongType, path+".entries", "expected JSON array")
		}
		if err := s.container(len(entries.items), path); err != nil {
			return err
		}
		if err := s.push(`{"type":"Object","entries":[`); err != nil {
			return err
		}
		for index, item := range entries.items {
			if index != 0 {
				if err := s.push(","); err != nil {
					return err
				}
			}
			entryFields, err := jsonObjectExact(item, []string{"key", "value"},
				fmt.Sprintf("%s.entries[%d]", path, index))
			if err != nil {
				return err
			}
			key, err := jsonStringOf(entryFields[0], fmt.Sprintf("%s.entries[%d].key", path, index))
			if err != nil {
				return err
			}
			if err := s.push(`{"key":`); err != nil {
				return err
			}
			if err := s.quoted(key, fmt.Sprintf("%s.entries[%d].key", path, index)); err != nil {
				return err
			}
			if err := s.push(`,"value":`); err != nil {
				return err
			}
			if err := s.value(entryFields[1], depth+1, fmt.Sprintf("%s.entries[%d].value", path, index)); err != nil {
				return err
			}
			if err := s.push(`}`); err != nil {
				return err
			}
		}
		return s.push(`]}`)
	case "EntryMapping":
		entries := member("entries")
		if entries == nil || entries.kind != jsonArray {
			return protocolError(KindWrongType, path+".entries", "expected JSON array")
		}
		if err := s.container(len(entries.items), path); err != nil {
			return err
		}
		if err := s.push(`{"type":"EntryMapping","entries":[`); err != nil {
			return err
		}
		for index, item := range entries.items {
			if index != 0 {
				if err := s.push(","); err != nil {
					return err
				}
			}
			entryFields, err := jsonObjectExact(item, []string{"key", "value"},
				fmt.Sprintf("%s.entries[%d]", path, index))
			if err != nil {
				return err
			}
			if err := s.push(`{"key":`); err != nil {
				return err
			}
			if err := s.value(entryFields[0], depth+1, fmt.Sprintf("%s.entries[%d].key", path, index)); err != nil {
				return err
			}
			if err := s.push(`,"value":`); err != nil {
				return err
			}
			if err := s.value(entryFields[1], depth+1, fmt.Sprintf("%s.entries[%d].value", path, index)); err != nil {
				return err
			}
			if err := s.push(`}`); err != nil {
				return err
			}
		}
		return s.push(`]}`)
	}
	return invalid(path+".type", "unknown value type")
}

// valueToNode converts a core value into the tagged tree form. Every value
// becomes its tagged representation (an object whose first member is
// "type"), the form the canonical encoder emits.
func valueToNode(value core.Value) *jsonNode {
	switch val := value.(type) {
	case core.Null:
		return &jsonNode{kind: jsonObject, fields: []jsonField{
			{key: "type", value: &jsonNode{kind: jsonString, text: "Null"}},
		}}
	case core.Boolean:
		return &jsonNode{kind: jsonObject, fields: []jsonField{
			{key: "type", value: &jsonNode{kind: jsonString, text: "Boolean"}},
			{key: "value", value: &jsonNode{kind: jsonBool, truth: bool(val)}},
		}}
	case core.String:
		return &jsonNode{kind: jsonObject, fields: []jsonField{
			{key: "type", value: &jsonNode{kind: jsonString, text: "String"}},
			{key: "value", value: &jsonNode{kind: jsonString, text: string(val)}},
		}}
	case core.Integer:
		return &jsonNode{kind: jsonObject, fields: []jsonField{
			{key: "type", value: &jsonNode{kind: jsonString, text: "Integer"}},
			{key: "value", value: &jsonNode{kind: jsonString, text: val.Int().String()}},
		}}
	case core.Decimal:
		return &jsonNode{kind: jsonObject, fields: []jsonField{
			{key: "type", value: &jsonNode{kind: jsonString, text: "Decimal"}},
			{key: "coefficient", value: &jsonNode{kind: jsonString, text: val.Coefficient().String()}},
			{key: "exponent", value: &jsonNode{kind: jsonString, text: val.Exponent().String()}},
		}}
	case core.BinaryFloat32:
		return &jsonNode{kind: jsonObject, fields: []jsonField{
			{key: "type", value: &jsonNode{kind: jsonString, text: "BinaryFloat32"}},
			{key: "bits", value: &jsonNode{kind: jsonString, text: fmt.Sprintf("%08x", uint32(val))}},
		}}
	case core.BinaryFloat64:
		return &jsonNode{kind: jsonObject, fields: []jsonField{
			{key: "type", value: &jsonNode{kind: jsonString, text: "BinaryFloat64"}},
			{key: "bits", value: &jsonNode{kind: jsonString, text: fmt.Sprintf("%016x", uint64(val))}},
		}}
	case core.Bytes:
		return &jsonNode{kind: jsonObject, fields: []jsonField{
			{key: "type", value: &jsonNode{kind: jsonString, text: "Bytes"}},
			{key: "hex", value: &jsonNode{kind: jsonString, text: hex.EncodeToString(val.Content())}},
		}}
	case core.Date:
		return &jsonNode{kind: jsonObject, fields: []jsonField{
			{key: "type", value: &jsonNode{kind: jsonString, text: "Date"}},
			{key: "year", value: &jsonNode{kind: jsonString, text: val.Year().Int().String()}},
			{key: "month", value: &jsonNode{kind: jsonString, text: strconv.FormatUint(uint64(val.Month()), 10)}},
			{key: "day", value: &jsonNode{kind: jsonString, text: strconv.FormatUint(uint64(val.Day()), 10)}},
		}}
	case core.Time:
		return &jsonNode{kind: jsonObject, fields: []jsonField{
			{key: "type", value: &jsonNode{kind: jsonString, text: "Time"}},
			{key: "hour", value: &jsonNode{kind: jsonString, text: strconv.FormatUint(uint64(val.Hour()), 10)}},
			{key: "minute", value: &jsonNode{kind: jsonString, text: strconv.FormatUint(uint64(val.Minute()), 10)}},
			{key: "second", value: &jsonNode{kind: jsonString, text: strconv.FormatUint(uint64(val.Second()), 10)}},
			{key: "fraction", value: valueToNode(val.FractionalSecond())},
		}}
	case core.LocalDateTime:
		return &jsonNode{kind: jsonObject, fields: []jsonField{
			{key: "type", value: &jsonNode{kind: jsonString, text: "LocalDateTime"}},
			{key: "date", value: valueToNode(val.Date())},
			{key: "time", value: valueToNode(val.Time())},
		}}
	case core.OffsetDateTime:
		return &jsonNode{kind: jsonObject, fields: []jsonField{
			{key: "type", value: &jsonNode{kind: jsonString, text: "OffsetDateTime"}},
			{key: "local", value: valueToNode(val.Local())},
			{key: "offset_seconds", value: &jsonNode{kind: jsonString, text: strconv.FormatInt(int64(val.OffsetSeconds()), 10)}},
		}}
	case *core.Array:
		items := make([]*jsonNode, 0, val.Len())
		for _, item := range val.Items() {
			items = append(items, valueToNode(item))
		}
		return &jsonNode{kind: jsonObject, fields: []jsonField{
			{key: "type", value: &jsonNode{kind: jsonString, text: "Sequence"}},
			{key: "items", value: &jsonNode{kind: jsonArray, items: items}},
		}}
	case *core.Object:
		entries := make([]*jsonNode, 0, val.Len())
		for _, entry := range val.Entries() {
			entries = append(entries, &jsonNode{kind: jsonObject, fields: []jsonField{
				{key: "key", value: &jsonNode{kind: jsonString, text: entry.Key}},
				{key: "value", value: valueToNode(entry.Value)},
			}})
		}
		return &jsonNode{kind: jsonObject, fields: []jsonField{
			{key: "type", value: &jsonNode{kind: jsonString, text: "Object"}},
			{key: "entries", value: &jsonNode{kind: jsonArray, items: entries}},
		}}
	case *core.EntryMapping:
		entries := make([]*jsonNode, 0, val.Len())
		for _, entry := range val.Entries() {
			entries = append(entries, &jsonNode{kind: jsonObject, fields: []jsonField{
				{key: "key", value: valueToNode(entry.Key)},
				{key: "value", value: valueToNode(entry.Value)},
			}})
		}
		return &jsonNode{kind: jsonObject, fields: []jsonField{
			{key: "type", value: &jsonNode{kind: jsonString, text: "EntryMapping"}},
			{key: "entries", value: &jsonNode{kind: jsonArray, items: entries}},
		}}
	}
	return &jsonNode{kind: jsonObject, fields: []jsonField{
		{key: "type", value: &jsonNode{kind: jsonString, text: "Null"}},
	}}
}

// nodeToValue converts a tagged tree node into a core value, applying the
// protocol limits and covering all fifteen kinds (the Rust decode_value,
// value_transport.rs).
func nodeToValue(node *jsonNode, depth int, path string, state *decodeState) (core.Value, error) {
	if err := state.node(depth, path); err != nil {
		return nil, err
	}
	if node.kind != jsonObject {
		return nil, protocolError(KindWrongType, path, "expected JSON object")
	}
	if len(node.fields) == 0 {
		return nil, protocolError(KindMissingField, path+".type", "missing value type")
	}
	if node.fields[0].key != "type" {
		return nil, protocolError(KindSchemaMismatch, path, "type must be the first field")
	}
	kind, err := jsonStringOf(node.fields[0].value, path+".type")
	if err != nil {
		return nil, err
	}
	switch kind {
	case "Null":
		if err := jsonObjectFieldsExact(node, []string{"type"}, path); err != nil {
			return nil, err
		}
		return core.NullValue(), nil
	case "Boolean":
		if err := jsonObjectFieldsExact(node, []string{"type", "value"}, path); err != nil {
			return nil, err
		}
		boolean, err := jsonBooleanOf(node.fields[1].value, path+".value")
		if err != nil {
			return nil, err
		}
		return core.Boolean(boolean), nil
	case "Integer":
		if err := jsonObjectFieldsExact(node, []string{"type", "value"}, path); err != nil {
			return nil, err
		}
		integer, err := jsonParseInteger(node.fields[1].value, path+".value", state.limits)
		if err != nil {
			return nil, err
		}
		return core.NewInteger(integer), nil
	case "Decimal":
		if err := jsonObjectFieldsExact(node, []string{"type", "coefficient", "exponent"}, path); err != nil {
			return nil, err
		}
		coefficient, err := jsonParseInteger(node.fields[1].value, path+".coefficient", state.limits)
		if err != nil {
			return nil, err
		}
		exponent, err := jsonParseInteger(node.fields[2].value, path+".exponent", state.limits)
		if err != nil {
			return nil, err
		}
		return core.NewDecimal(coefficient, exponent), nil
	case "BinaryFloat32":
		if err := jsonObjectFieldsExact(node, []string{"type", "bits"}, path); err != nil {
			return nil, err
		}
		bits, err := jsonParseHexUint32(node.fields[1].value, path+".bits")
		if err != nil {
			return nil, err
		}
		return core.NewBinaryFloat32(bits), nil
	case "BinaryFloat64":
		if err := jsonObjectFieldsExact(node, []string{"type", "bits"}, path); err != nil {
			return nil, err
		}
		bits, err := jsonParseHexUint64(node.fields[1].value, path+".bits")
		if err != nil {
			return nil, err
		}
		return core.NewBinaryFloat64(bits), nil
	case "Bytes":
		if err := jsonObjectFieldsExact(node, []string{"type", "hex"}, path); err != nil {
			return nil, err
		}
		hexText, err := jsonStringOf(node.fields[1].value, path+".hex")
		if err != nil {
			return nil, err
		}
		if len(hexText)%2 != 0 {
			return nil, invalid(path+".hex", "byte hex length must be even")
		}
		if len(hexText)/2 > state.limits.MaxBlobBytes {
			return nil, resource(path+".hex", "bytes")
		}
		decoded, err := hex.DecodeString(hexText)
		if err != nil {
			return nil, invalid(path+".hex", "invalid byte hex")
		}
		return core.NewBytes(decoded), nil
	case "Date":
		if err := jsonObjectFieldsExact(node, []string{"type", "year", "month", "day"}, path); err != nil {
			return nil, err
		}
		year, err := jsonParseInteger(node.fields[1].value, path+".year", state.limits)
		if err != nil {
			return nil, err
		}
		month, err := jsonParseU8(node.fields[2].value, path+".month", state.limits)
		if err != nil {
			return nil, err
		}
		day, err := jsonParseU8(node.fields[3].value, path+".day", state.limits)
		if err != nil {
			return nil, err
		}
		date, err := core.NewDate(year, month, day)
		if err != nil {
			return nil, invalid(path, "invalid date")
		}
		return date, nil
	case "Time":
		if err := jsonObjectFieldsExact(node, []string{"type", "hour", "minute", "second", "fraction"}, path); err != nil {
			return nil, err
		}
		hour, err := jsonParseU8(node.fields[1].value, path+".hour", state.limits)
		if err != nil {
			return nil, err
		}
		minute, err := jsonParseU8(node.fields[2].value, path+".minute", state.limits)
		if err != nil {
			return nil, err
		}
		second, err := jsonParseU8(node.fields[3].value, path+".second", state.limits)
		if err != nil {
			return nil, err
		}
		fractionValue, err := nodeToValue(node.fields[4].value, depth+1, path+".fraction", state)
		if err != nil {
			return nil, err
		}
		fraction, ok := fractionValue.(core.Decimal)
		if !ok {
			return nil, protocolError(KindWrongType, path+".fraction", "expected Decimal")
		}
		time, err := core.NewTime(hour, minute, second, fraction)
		if err != nil {
			return nil, invalid(path, "invalid time")
		}
		return time, nil
	case "LocalDateTime":
		if err := jsonObjectFieldsExact(node, []string{"type", "date", "time"}, path); err != nil {
			return nil, err
		}
		dateValue, err := nodeToValue(node.fields[1].value, depth+1, path+".date", state)
		if err != nil {
			return nil, err
		}
		date, ok := dateValue.(core.Date)
		if !ok {
			return nil, protocolError(KindWrongType, path+".date", "expected Date")
		}
		timeValue, err := nodeToValue(node.fields[2].value, depth+1, path+".time", state)
		if err != nil {
			return nil, err
		}
		time, ok := timeValue.(core.Time)
		if !ok {
			return nil, protocolError(KindWrongType, path+".time", "expected Time")
		}
		return core.NewLocalDateTime(date, time), nil
	case "OffsetDateTime":
		if err := jsonObjectFieldsExact(node, []string{"type", "local", "offset_seconds"}, path); err != nil {
			return nil, err
		}
		localValue, err := nodeToValue(node.fields[1].value, depth+1, path+".local", state)
		if err != nil {
			return nil, err
		}
		local, ok := localValue.(core.LocalDateTime)
		if !ok {
			return nil, protocolError(KindWrongType, path+".local", "expected LocalDateTime")
		}
		offset, err := jsonParseI32(node.fields[2].value, path+".offset_seconds", state.limits)
		if err != nil {
			return nil, err
		}
		offsetDateTime, err := core.NewOffsetDateTime(local, offset)
		if err != nil {
			return nil, invalid(path, "invalid offset date-time")
		}
		return offsetDateTime, nil
	case "String":
		if err := jsonObjectFieldsExact(node, []string{"type", "value"}, path); err != nil {
			return nil, err
		}
		text, err := jsonStringOf(node.fields[1].value, path+".value")
		if err != nil {
			return nil, err
		}
		return core.String(text), nil
	case "Sequence":
		if err := jsonObjectFieldsExact(node, []string{"type", "items"}, path); err != nil {
			return nil, err
		}
		if node.fields[1].value.kind != jsonArray {
			return nil, protocolError(KindWrongType, path+".items", "expected JSON array")
		}
		itemsNode := node.fields[1].value
		if err := state.container(len(itemsNode.items), path); err != nil {
			return nil, err
		}
		items := make([]core.Value, 0, len(itemsNode.items))
		for index, item := range itemsNode.items {
			value, err := nodeToValue(item, depth+1, fmt.Sprintf("%s.items[%d]", path, index), state)
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
		return core.NewArray(items...), nil
	case "Object":
		if err := jsonObjectFieldsExact(node, []string{"type", "entries"}, path); err != nil {
			return nil, err
		}
		if node.fields[1].value.kind != jsonArray {
			return nil, protocolError(KindWrongType, path+".entries", "expected JSON array")
		}
		entriesNode := node.fields[1].value
		if err := state.container(len(entriesNode.items), path); err != nil {
			return nil, err
		}
		builder := core.NewObjectBuilder()
		for index, item := range entriesNode.items {
			entryPath := fmt.Sprintf("%s.entries[%d]", path, index)
			entryFields, err := jsonObjectExact(item, []string{"key", "value"}, entryPath)
			if err != nil {
				return nil, err
			}
			key, err := jsonStringOf(entryFields[0], entryPath+".key")
			if err != nil {
				return nil, err
			}
			value, err := nodeToValue(entryFields[1], depth+1, entryPath+".value", state)
			if err != nil {
				return nil, err
			}
			if err := builder.Insert(key, value); err != nil {
				return nil, invalid(entryPath, "duplicate object key")
			}
		}
		return builder.Build(), nil
	case "EntryMapping":
		if err := jsonObjectFieldsExact(node, []string{"type", "entries"}, path); err != nil {
			return nil, err
		}
		if node.fields[1].value.kind != jsonArray {
			return nil, protocolError(KindWrongType, path+".entries", "expected JSON array")
		}
		entriesNode := node.fields[1].value
		if err := state.container(len(entriesNode.items), path); err != nil {
			return nil, err
		}
		builder := core.NewEntryMappingBuilder()
		for index, item := range entriesNode.items {
			entryPath := fmt.Sprintf("%s.entries[%d]", path, index)
			entryFields, err := jsonObjectExact(item, []string{"key", "value"}, entryPath)
			if err != nil {
				return nil, err
			}
			key, err := nodeToValue(entryFields[0], depth+1, entryPath+".key", state)
			if err != nil {
				return nil, err
			}
			value, err := nodeToValue(entryFields[1], depth+1, entryPath+".value", state)
			if err != nil {
				return nil, err
			}
			// Decoded values are never nil, so Push cannot fail; duplicate
			// keys are value semantics (value_transport.rs).
			if err := builder.Push(key, value); err != nil {
				return nil, err
			}
		}
		return builder.Build(), nil
	default:
		return nil, invalid(path+".type", "unknown value type")
	}
}

// jsonObjectExact returns the fields of an object node in source order,
// validating the declared name set (if any) and canonical order.
func jsonObjectExact(node *jsonNode, expected []string, path string) ([]*jsonNode, error) {
	if node.kind != jsonObject {
		return nil, protocolError(KindWrongType, path, "expected JSON object")
	}
	names := make([]string, len(node.fields))
	values := make([]*jsonNode, len(node.fields))
	for index, field := range node.fields {
		names[index] = field.key
		values[index] = field.value
	}
	if expected != nil {
		for _, name := range names {
			if !containsString(expected, name) {
				return nil, protocolError(KindUnknownField, path+"."+name, "field is not declared by the fixed schema")
			}
		}
		for _, name := range expected {
			if !containsString(names, name) {
				return nil, protocolError(KindMissingField, path+"."+name, "required field is absent")
			}
		}
		if !equalStrings(names, expected) {
			return nil, protocolError(KindSchemaMismatch, path, "fields are duplicated or not in canonical order")
		}
	}
	return values, nil
}

// jsonObjectFieldsExact validates the exact member set of a tagged value
// object (the counterpart of exact_object in value_transport.rs).
func jsonObjectFieldsExact(node *jsonNode, expected []string, path string) error {
	if node.kind != jsonObject {
		return protocolError(KindWrongType, path, "expected JSON object")
	}
	names := make([]string, len(node.fields))
	for index, field := range node.fields {
		names[index] = field.key
	}
	for _, name := range names {
		if !containsString(expected, name) {
			return protocolError(KindUnknownField, path+"."+name, "field is not declared by the fixed schema")
		}
	}
	for _, name := range expected {
		if !containsString(names, name) {
			return protocolError(KindMissingField, path+"."+name, "required field is absent")
		}
	}
	if !equalStrings(names, expected) {
		return protocolError(KindSchemaMismatch, path, "fields are duplicated or not in canonical order")
	}
	return nil
}

// jsonStringOf reads a string member value.
func jsonStringOf(node *jsonNode, path string) (string, error) {
	if node.kind != jsonString {
		return "", protocolError(KindWrongType, path, "expected JSON string")
	}
	return node.text, nil
}

// jsonBooleanOf reads a boolean member value.
func jsonBooleanOf(node *jsonNode, path string) (bool, error) {
	if node.kind != jsonBool {
		return false, protocolError(KindWrongType, path, "expected JSON boolean")
	}
	return node.truth, nil
}

// jsonParseInteger parses a decimal string into a big integer with the
// protocol integer limits (value_transport.rs).
func jsonParseInteger(node *jsonNode, path string, limits ProtocolLimits) (*big.Int, error) {
	if node.kind != jsonString {
		return nil, protocolError(KindWrongType, path, "expected JSON string")
	}
	maxDigits := limits.MaxIntegerBytes*3 + 2
	if len(node.text) > maxDigits {
		return nil, resource(path, "integer decimal digits")
	}
	integer, ok := new(big.Int).SetString(node.text, 10)
	if !ok {
		return nil, invalid(path, "invalid integer")
	}
	if len(integer.Bytes()) > limits.MaxIntegerBytes {
		return nil, resource(path, "integer magnitude")
	}
	return integer, nil
}

// jsonParseHexUint32 parses exactly eight hexadecimal digits (the Rust
// parse_hex_u32, value_transport.rs).
func jsonParseHexUint32(node *jsonNode, path string) (uint32, error) {
	if node.kind != jsonString {
		return 0, protocolError(KindWrongType, path, "expected JSON string")
	}
	if len(node.text) != 8 {
		return 0, invalid(path, "binary32 bits require 8 hexadecimal digits")
	}
	decoded, err := hex.DecodeString(node.text)
	if err != nil {
		return 0, invalid(path, "invalid binary32 bits")
	}
	var bits uint32
	for _, byte := range decoded {
		bits = bits<<8 | uint32(byte)
	}
	return bits, nil
}

// jsonParseHexUint64 parses exactly sixteen hexadecimal digits (the Rust
// parse_hex_u64, value_transport.rs).
func jsonParseHexUint64(node *jsonNode, path string) (uint64, error) {
	if node.kind != jsonString {
		return 0, protocolError(KindWrongType, path, "expected JSON string")
	}
	if len(node.text) != 16 {
		return 0, invalid(path, "binary64 bits require 16 hexadecimal digits")
	}
	decoded, err := hex.DecodeString(node.text)
	if err != nil {
		return 0, invalid(path, "invalid binary64 bits")
	}
	var bits uint64
	for _, byte := range decoded {
		bits = bits<<8 | uint64(byte)
	}
	return bits, nil
}

// jsonParseU8 parses a decimal string into a uint8 (the Rust parse_u8,
// value_transport.rs).
func jsonParseU8(node *jsonNode, path string, limits ProtocolLimits) (uint8, error) {
	integer, err := jsonParseInteger(node, path, limits)
	if err != nil {
		return 0, err
	}
	if !integer.IsInt64() {
		return 0, invalid(path, "integer is outside u8")
	}
	value := integer.Int64()
	if value < 0 || value > 255 {
		return 0, invalid(path, "integer is outside u8")
	}
	return uint8(value), nil
}

// jsonParseI32 parses a decimal string into an int32 (the Rust parse_i32,
// value_transport.rs).
func jsonParseI32(node *jsonNode, path string, limits ProtocolLimits) (int32, error) {
	integer, err := jsonParseInteger(node, path, limits)
	if err != nil {
		return 0, err
	}
	if !integer.IsInt64() {
		return 0, invalid(path, "integer is outside i32")
	}
	value := integer.Int64()
	if value < -1<<31 || value > 1<<31-1 {
		return 0, invalid(path, "integer is outside i32")
	}
	return int32(value), nil
}

// EncodePVCE encodes a PortableValue as canonical PVCE/1 under protocol
// limits (consema-rs/consema-protocol/src/value_transport.rs).
func EncodePVCE(value core.Value, limits ProtocolLimits) ([]byte, error) {
	bytes, err := core.EncodePVCEBounded(value, core.EncodeLimits{
		MaxBytes:            limits.MaxBytes,
		MaxDepth:            limits.MaxDepth,
		MaxNodes:            limits.MaxNodes,
		MaxContainerEntries: limits.MaxContainerEntries,
		MaxIntegerBytes:     limits.MaxIntegerBytes,
		MaxBlobBytes:        limits.MaxBlobBytes,
	})
	if err != nil {
		return nil, mapPVCEError(err)
	}
	return bytes, nil
}

// DecodePVCE strictly decodes canonical PVCE/1 under protocol limits
// (consema-rs/consema-protocol/src/value_transport.rs). The Go codec
// rejects records outside the closed fifteen-kind model (only the extended
// 0x7f record, via core.ErrUnknownCoreTag), reported as KindInvalidPvce.
func DecodePVCE(bytes []byte, limits ProtocolLimits) (core.Value, error) {
	value, err := core.DecodePVCE(bytes, core.DecodeLimits{
		MaxBytes:            limits.MaxBytes,
		MaxDepth:            limits.MaxDepth,
		MaxNodes:            limits.MaxNodes,
		MaxContainerEntries: limits.MaxContainerEntries,
		MaxIntegerBytes:     limits.MaxIntegerBytes,
		MaxBlobBytes:        limits.MaxBlobBytes,
	})
	if err != nil {
		return nil, mapPVCEError(err)
	}
	return value, nil
}

// mapPVCEError converts a go/core codec error into the protocol error kind
// registry (the Rust decode_pvce mapping, value_transport.rs).
func mapPVCEError(err error) *ProtocolError {
	if core.IsPVCEError(err, core.ErrResourceLimit) {
		return resource("$", err.Error())
	}
	return protocolError(KindInvalidPvce, "$", err.Error())
}
