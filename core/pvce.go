package core

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"unicode/utf8"
)

// This file reimplements the PVCE/1 wire format from the Rust reference
// codec, crates/consema-pvce/src/lib.rs:
//
//   - stream magic is the ASCII octets "PVCE" (lib.rs:23);
//   - version is minimal unsigned LEB128 1 (lib.rs:25);
//   - integer sign octets are 0 (zero), 1 (positive), 2 (negative)
//     (lib.rs:9-12);
//   - all unsigned lengths/counts/tags are minimal unsigned LEB128
//     (lib.rs:11, 616-628).
//
// The Go codec covers the closed fifteen-kind core model (RFC 0016 §4.1).
// Only the extended record tag 0x7f (ExtendedValue) remains outside the Go
// value model and fails with ErrUnknownCoreTag (see PVCEErrorKind).

// magicPVCE is the PVCE/1 stream magic (ASCII "PVCE").
var magicPVCE = [4]byte{'P', 'V', 'C', 'E'}

// streamVersion is the frozen PVCE/1 version (crates/consema-pvce/src/lib.rs:25).
const streamVersion = 1

// Record tags (crates/consema-pvce/src/lib.rs:27-43). The Rust codec also
// defines tag 0x7f (Extended); the Go value model has no ExtendedValue type,
// so extended records are rejected as ErrUnknownCoreTag.
const (
	tagNull           uint64 = 0x00
	tagFalse          uint64 = 0x01
	tagTrue           uint64 = 0x02
	tagInteger        uint64 = 0x10
	tagDecimal        uint64 = 0x11
	tagFloat32        uint64 = 0x12
	tagFloat64        uint64 = 0x13
	tagString         uint64 = 0x20
	tagBytes          uint64 = 0x21
	tagDate           uint64 = 0x30
	tagTime           uint64 = 0x31
	tagLocalDateTime  uint64 = 0x32
	tagOffsetDateTime uint64 = 0x33
	tagSequence       uint64 = 0x40
	tagObject         uint64 = 0x41
	tagEntryMapping   uint64 = 0x42
)

// Default resource limits, mirroring the Rust defaults
// (crates/consema-pvce/src/lib.rs:71-82, 127-138).
const (
	defaultMaxBytes            = 64 << 20 // 64 MiB
	defaultMaxDepth            = 256
	defaultMaxNodes            = 1_000_000
	defaultMaxContainerEntries = 1_000_000
	defaultMaxIntegerBytes     = 1 << 20  // 1 MiB
	defaultMaxBlobBytes        = 64 << 20 // 64 MiB
)

// DecodeLimits are the strict PVCE/1 decoder resource limits, mirroring the
// Rust DecodeLimits (crates/consema-pvce/src/lib.rs:56-82). The zero value
// rejects every stream; use DefaultDecodeLimits.
type DecodeLimits struct {
	// MaxBytes is the maximum complete stream bytes.
	MaxBytes int
	// MaxDepth is the maximum nested container depth.
	MaxDepth int
	// MaxNodes is the maximum total core records.
	MaxNodes int
	// MaxContainerEntries is the maximum entries in one container.
	MaxContainerEntries int
	// MaxIntegerBytes is the maximum arbitrary integer magnitude bytes.
	MaxIntegerBytes int
	// MaxBlobBytes is the maximum String or Bytes payload bytes (the Rust
	// "Maximum String, Bytes, or extension payload bytes", lib.rs:67).
	MaxBlobBytes int
}

// DefaultDecodeLimits returns the frozen defaults (64 MiB stream, depth 256,
// 1,000,000 nodes, 1,000,000 container entries, 1 MiB integer magnitude,
// 64 MiB blob).
func DefaultDecodeLimits() DecodeLimits {
	return DecodeLimits{
		MaxBytes:            defaultMaxBytes,
		MaxDepth:            defaultMaxDepth,
		MaxNodes:            defaultMaxNodes,
		MaxContainerEntries: defaultMaxContainerEntries,
		MaxIntegerBytes:     defaultMaxIntegerBytes,
		MaxBlobBytes:        defaultMaxBlobBytes,
	}
}

// EncodeLimits are the bounded PVCE/1 encoder resource limits, mirroring the
// Rust EncodeLimits (crates/consema-pvce/src/lib.rs:111-138). The zero value
// rejects every value; use DefaultEncodeLimits.
type EncodeLimits struct {
	// MaxBytes is the maximum complete stream bytes.
	MaxBytes int
	// MaxDepth is the maximum nested container depth.
	MaxDepth int
	// MaxNodes is the maximum total core records.
	MaxNodes int
	// MaxContainerEntries is the maximum entries in one container.
	MaxContainerEntries int
	// MaxIntegerBytes is the maximum arbitrary integer magnitude bytes.
	MaxIntegerBytes int
	// MaxBlobBytes is the maximum String or Bytes payload bytes (the Rust
	// "Maximum String, Bytes, or extension payload bytes", lib.rs:123).
	MaxBlobBytes int
}

// DefaultEncodeLimits returns the frozen defaults (identical to
// DefaultDecodeLimits).
func DefaultEncodeLimits() EncodeLimits {
	return EncodeLimits{
		MaxBytes:            defaultMaxBytes,
		MaxDepth:            defaultMaxDepth,
		MaxNodes:            defaultMaxNodes,
		MaxContainerEntries: defaultMaxContainerEntries,
		MaxIntegerBytes:     defaultMaxIntegerBytes,
		MaxBlobBytes:        defaultMaxBlobBytes,
	}
}

// EncodePVCE encodes one value as a complete canonical PVCE/1 stream (RFC
// 0016 §4.2). The bytes are byte-identical to the Rust codec's output
// (crates/consema-pvce/src/lib.rs); the encoder emits only canonical forms.
// The error is always nil for a valid non-nil value; the error slot is
// reserved by the frozen API shape (RFC 0016 §4.2) and reports an invalid
// nil value.
func EncodePVCE(v Value) ([]byte, error) {
	if v == nil {
		return nil, &PVCEError{Kind: ErrInvalidValue}
	}
	out := make([]byte, 0, 32)
	out = append(out, magicPVCE[:]...)
	out = appendVarint(out, streamVersion)
	var err error
	out, err = encodeRecord(out, v)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// EncodePVCEBounded encodes one value after measuring it against explicit
// resource limits (the Rust encode_bounded; crates/consema-pvce/src/lib.rs:
// 150-156). It never truncates: exceeding any limit returns a
// resource-limit error with no partial output.
func EncodePVCEBounded(v Value, limits EncodeLimits) ([]byte, error) {
	if v == nil {
		return nil, &PVCEError{Kind: ErrInvalidValue}
	}
	sizer := &sizer{limits: limits}
	record, err := sizer.recordSize(v, 0)
	if err != nil {
		return nil, err
	}
	total := len(magicPVCE) + 1 + record
	if total > limits.MaxBytes {
		return nil, resourceLimit("stream-bytes")
	}
	return EncodePVCE(v)
}

// DecodePVCE strictly decodes one canonical PVCE/1 stream (RFC 0016 §4.2),
// mirroring the Rust decode (crates/consema-pvce/src/lib.rs:104-108, 404-426,
// 725-833). The decoder covers the closed fifteen-kind core model; only
// extended (0x7f) records fail with ErrUnknownCoreTag. Non-canonical input
// fails with the matching PVCEError kind.
func DecodePVCE(stream []byte, limits DecodeLimits) (Value, error) {
	if len(stream) > limits.MaxBytes {
		return nil, resourceLimit("stream-bytes")
	}
	r := &reader{bytes: stream, limits: limits}
	magic, err := r.take(len(magicPVCE))
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(magic, magicPVCE[:]) {
		return nil, &PVCEError{Kind: ErrInvalidMagic}
	}
	version, err := r.varint()
	if err != nil {
		return nil, err
	}
	if version != streamVersion {
		return nil, &PVCEError{Kind: ErrUnsupportedVersion, Value: version}
	}
	tag, payload, err := r.record()
	if err != nil {
		return nil, err
	}
	value, err := r.decodeRecord(tag, payload, 0)
	if err != nil {
		return nil, err
	}
	if r.offset != len(r.bytes) {
		return nil, &PVCEError{Kind: ErrTrailingBytes}
	}
	return value, nil
}

// encodeRecord writes one tag-length-prefixed record (the Rust write_record,
// crates/consema-pvce/src/lib.rs:610-614).
func encodeRecord(out []byte, v Value) ([]byte, error) {
	tag, payload, err := encodePayload(v)
	if err != nil {
		return nil, err
	}
	out = appendVarint(out, tag)
	out = appendVarint(out, uint64(len(payload)))
	out = append(out, payload...)
	return out, nil
}

// encodePayload builds the payload of one record and returns its tag.
func encodePayload(v Value) (uint64, []byte, error) {
	var tag uint64
	var payload []byte
	switch val := v.(type) {
	case Null:
		tag = tagNull
	case Boolean:
		if bool(val) {
			tag = tagTrue
		} else {
			tag = tagFalse
		}
	case String:
		tag = tagString
		payload = appendBlob(payload, []byte(val))
	case Integer:
		tag = tagInteger
		payload = appendIntegerPayload(payload, val.safeValue())
	case Decimal:
		tag = tagDecimal
		payload = appendIntegerField(payload, val.safeCoefficient())
		payload = appendIntegerField(payload, val.safeExponent())
	case BinaryFloat32:
		tag = tagFloat32
		var octets [4]byte
		binary.BigEndian.PutUint32(octets[:], uint32(val))
		payload = append(payload, octets[:]...)
	case BinaryFloat64:
		tag = tagFloat64
		var octets [8]byte
		binary.BigEndian.PutUint64(octets[:], uint64(val))
		payload = append(payload, octets[:]...)
	case Bytes:
		tag = tagBytes
		payload = appendBlob(payload, []byte(val))
	case Date:
		// The zero value is not a valid calendar date (temporal.go); it is
		// invalid input to the codec (the typed-nil analog, RFC 0016 §4.1).
		if !dateValid(val) {
			return 0, nil, &PVCEError{Kind: ErrInvalidValue}
		}
		tag = tagDate
		payload = appendIntegerField(payload, val.year.safeValue())
		payload = append(payload, val.month, val.day)
	case Time:
		if !timeValid(val) {
			return 0, nil, &PVCEError{Kind: ErrInvalidValue}
		}
		tag = tagTime
		payload = append(payload, val.hour, val.minute, val.second)
		payload = appendDecimalField(payload, val.fractionalSecond)
	case LocalDateTime:
		if !localValid(val) {
			return 0, nil, &PVCEError{Kind: ErrInvalidValue}
		}
		tag = tagLocalDateTime
		payload = appendDateField(payload, val.date)
		payload = appendTimeField(payload, val.time)
	case OffsetDateTime:
		if !localValid(val.local) {
			return 0, nil, &PVCEError{Kind: ErrInvalidValue}
		}
		tag = tagOffsetDateTime
		payload = appendDateField(payload, val.local.date)
		payload = appendTimeField(payload, val.local.time)
		payload = appendIntegerField(payload, big.NewInt(int64(val.offsetSeconds)))
	case *Array:
		if val == nil {
			return 0, nil, &PVCEError{Kind: ErrInvalidValue}
		}
		tag = tagSequence
		payload = appendVarint(payload, uint64(len(val.items)))
		for _, item := range val.items {
			var err error
			payload, err = encodeRecord(payload, item)
			if err != nil {
				return 0, nil, err
			}
		}
	case *Object:
		if val == nil {
			return 0, nil, &PVCEError{Kind: ErrInvalidValue}
		}
		tag = tagObject
		payload = appendVarint(payload, uint64(len(val.entries)))
		for _, entry := range val.entries {
			var err error
			payload, err = encodeRecord(payload, String(entry.Key))
			if err != nil {
				return 0, nil, err
			}
			payload, err = encodeRecord(payload, entry.Value)
			if err != nil {
				return 0, nil, err
			}
		}
	case *EntryMapping:
		if val == nil {
			return 0, nil, &PVCEError{Kind: ErrInvalidValue}
		}
		tag = tagEntryMapping
		payload = appendVarint(payload, uint64(len(val.entries)))
		for _, entry := range val.entries {
			var err error
			payload, err = encodeRecord(payload, entry.Key)
			if err != nil {
				return 0, nil, err
			}
			payload, err = encodeRecord(payload, entry.Value)
			if err != nil {
				return 0, nil, err
			}
		}
	default:
		return 0, nil, &PVCEError{Kind: ErrInvalidValue}
	}
	return tag, payload, nil
}

// appendIntegerPayload writes the sign octet, the magnitude length varint,
// and the minimal big-endian magnitude (the Rust encode_integer_payload,
// crates/consema-pvce/src/lib.rs:545-554).
func appendIntegerPayload(out []byte, value *big.Int) []byte {
	switch value.Sign() {
	case -1:
		out = append(out, 2)
	case 0:
		out = append(out, 0)
	default:
		out = append(out, 1)
	}
	magnitude := value.Bytes()
	out = appendVarint(out, uint64(len(magnitude)))
	return append(out, magnitude...)
}

// appendIntegerField writes a length-prefixed integer payload (the Rust
// encode_integer_field, crates/consema-pvce/src/lib.rs:556-561).
func appendIntegerField(out []byte, value *big.Int) []byte {
	field := appendIntegerPayload(nil, value)
	out = appendVarint(out, uint64(len(field)))
	return append(out, field...)
}

// appendDecimalField writes a length-prefixed decimal payload (the Rust
// encode_decimal_field, crates/consema-pvce/src/lib.rs:568-573).
func appendDecimalField(out []byte, value Decimal) []byte {
	field := appendDecimalPayload(nil, value)
	out = appendVarint(out, uint64(len(field)))
	return append(out, field...)
}

// appendDecimalPayload writes the two length-prefixed integer fields of a
// decimal (the Rust encode_decimal_payload, lib.rs:563-566).
func appendDecimalPayload(out []byte, value Decimal) []byte {
	out = appendIntegerField(out, value.safeCoefficient())
	out = appendIntegerField(out, value.safeExponent())
	return out
}

// appendDateField writes a length-prefixed date payload (the Rust
// encode_date_field, crates/consema-pvce/src/lib.rs:586-591).
func appendDateField(out []byte, value Date) []byte {
	field := appendDatePayload(nil, value)
	out = appendVarint(out, uint64(len(field)))
	return append(out, field...)
}

// appendDatePayload writes the year field and the month/day octets (the Rust
// encode_date_payload, lib.rs:580-584).
func appendDatePayload(out []byte, value Date) []byte {
	out = appendIntegerField(out, value.year.safeValue())
	return append(out, value.month, value.day)
}

// appendTimeField writes a length-prefixed time payload (the Rust
// encode_time_field, crates/consema-pvce/src/lib.rs:598-603).
func appendTimeField(out []byte, value Time) []byte {
	field := appendTimePayload(nil, value)
	out = appendVarint(out, uint64(len(field)))
	return append(out, field...)
}

// appendTimePayload writes the hour/minute/second octets and the fractional
// second field (the Rust encode_time_payload, lib.rs:593-596).
func appendTimePayload(out []byte, value Time) []byte {
	out = append(out, value.hour, value.minute, value.second)
	return appendDecimalField(out, value.fractionalSecond)
}

// appendBlob writes a length-prefixed byte string (the Rust encode_blob,
// crates/consema-pvce/src/lib.rs:575-578).
func appendBlob(out []byte, value []byte) []byte {
	out = appendVarint(out, uint64(len(value)))
	return append(out, value...)
}

// appendVarint writes the minimal unsigned LEB128 encoding of value (the
// Rust write_varint, crates/consema-pvce/src/lib.rs:616-628).
func appendVarint(out []byte, value uint64) []byte {
	for {
		octet := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			octet |= 0x80
		}
		out = append(out, octet)
		if value == 0 {
			return out
		}
	}
}

// varintSize returns the encoded length of value as a minimal unsigned
// LEB128 (the Rust const varint_size, crates/consema-pvce/src/lib.rs:370-377).
func varintSize(value uint64) int {
	size := 1
	for value >= 0x80 {
		value >>= 7
		size++
	}
	return size
}

// reader is the strict streaming decoder over one PVCE/1 stream or payload
// (the Rust Reader, crates/consema-pvce/src/lib.rs:630-723).
type reader struct {
	bytes  []byte
	offset int
	limits DecodeLimits
	nodes  int
}

// take consumes n octets (the Rust Reader::take, lib.rs:648-659).
func (r *reader) take(n int) ([]byte, error) {
	if n < 0 || r.offset+n > len(r.bytes) {
		return nil, &PVCEError{Kind: ErrUnexpectedEnd}
	}
	value := r.bytes[r.offset : r.offset+n]
	r.offset += n
	return value, nil
}

// octet consumes one octet (the Rust Reader::octet, lib.rs:661-663).
func (r *reader) octet() (byte, error) {
	value, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return value[0], nil
}

// varint reads one unsigned varint, rejecting non-minimal encodings and
// 64-bit overflow (the Rust Reader::varint, lib.rs:665-683).
func (r *reader) varint() (uint64, error) {
	start := r.offset
	var value uint64
	for shift := 0; shift <= 63; shift += 7 {
		octet, err := r.octet()
		if err != nil {
			return 0, err
		}
		low := uint64(octet & 0x7f)
		if shift == 63 && low > 1 {
			return 0, &PVCEError{Kind: ErrVarintOverflow}
		}
		value |= low << shift
		if octet&0x80 == 0 {
			if r.offset-start > 1 && low == 0 {
				return 0, &PVCEError{Kind: ErrNonCanonicalVarint}
			}
			return value, nil
		}
	}
	return 0, &PVCEError{Kind: ErrVarintOverflow}
}

// length reads one varint length and enforces the named limit (the Rust
// Reader::length, lib.rs:685-691).
func (r *reader) length(limit int, name string) (int, error) {
	value, err := r.varint()
	if err != nil {
		return 0, err
	}
	if value > uint64(limit) {
		return 0, resourceLimit(name)
	}
	return int(value), nil
}

// record reads one tag-length-prefixed record (the Rust Reader::record,
// lib.rs:693-697). The payload length is bounded by MaxBytes
// ("record-bytes"), exactly as in the Rust decoder.
func (r *reader) record() (uint64, []byte, error) {
	tag, err := r.varint()
	if err != nil {
		return 0, nil, err
	}
	n, err := r.length(r.limits.MaxBytes, "record-bytes")
	if err != nil {
		return 0, nil, err
	}
	payload, err := r.take(n)
	if err != nil {
		return 0, nil, err
	}
	return tag, payload, nil
}

// decodeRecord decodes one record whose payload is already delimited (the
// Rust decode_core_record, crates/consema-pvce/src/lib.rs:725-833): it
// enforces the depth and node limits, decodes the payload, and rejects
// trailing payload bytes.
func (r *reader) decodeRecord(tag uint64, payload []byte, depth int) (Value, error) {
	if depth > r.limits.MaxDepth {
		return nil, resourceLimit("nesting-depth")
	}
	r.nodes++
	if r.nodes > r.limits.MaxNodes {
		return nil, resourceLimit("value-nodes")
	}
	child := &reader{bytes: payload, limits: r.limits, nodes: r.nodes}
	value, err := child.decodePayload(tag, depth)
	if err != nil {
		return nil, err
	}
	if child.offset != len(child.bytes) {
		return nil, &PVCEError{Kind: ErrTrailingPayload, Value: tag}
	}
	r.nodes = child.nodes
	return value, nil
}

// decodePayload decodes the payload of one record with the given tag.
func (r *reader) decodePayload(tag uint64, depth int) (Value, error) {
	switch tag {
	case tagNull:
		if len(r.bytes) != 0 {
			return nil, &PVCEError{Kind: ErrInvalidPayload, Value: tag}
		}
		return Null{}, nil
	case tagFalse:
		if len(r.bytes) != 0 {
			return nil, &PVCEError{Kind: ErrInvalidPayload, Value: tag}
		}
		return Boolean(false), nil
	case tagTrue:
		if len(r.bytes) != 0 {
			return nil, &PVCEError{Kind: ErrInvalidPayload, Value: tag}
		}
		return Boolean(true), nil
	case tagInteger:
		return r.decodeInteger()
	case tagDecimal:
		return r.decodeDecimal()
	case tagFloat32:
		if len(r.bytes) != 4 {
			return nil, &PVCEError{Kind: ErrInvalidPayload, Value: tag}
		}
		octets, err := r.take(4)
		if err != nil {
			return nil, err
		}
		return NewBinaryFloat32(binary.BigEndian.Uint32(octets)), nil
	case tagFloat64:
		if len(r.bytes) != 8 {
			return nil, &PVCEError{Kind: ErrInvalidPayload, Value: tag}
		}
		octets, err := r.take(8)
		if err != nil {
			return nil, err
		}
		return NewBinaryFloat64(binary.BigEndian.Uint64(octets)), nil
	case tagString:
		blob, err := r.decodeBlob()
		if err != nil {
			return nil, err
		}
		if !utf8.Valid(blob) {
			return nil, &PVCEError{Kind: ErrInvalidUTF8}
		}
		return String(blob), nil
	case tagBytes:
		blob, err := r.decodeBlob()
		if err != nil {
			return nil, err
		}
		return NewBytes(blob), nil
	case tagDate:
		date, err := r.decodeDate()
		if err != nil {
			return nil, err
		}
		return date, nil
	case tagTime:
		time, err := r.decodeTime()
		if err != nil {
			return nil, err
		}
		return time, nil
	case tagLocalDateTime:
		date, err := r.decodeDateField()
		if err != nil {
			return nil, err
		}
		time, err := r.decodeTimeField()
		if err != nil {
			return nil, err
		}
		return NewLocalDateTime(date, time), nil
	case tagOffsetDateTime:
		date, err := r.decodeDateField()
		if err != nil {
			return nil, err
		}
		time, err := r.decodeTimeField()
		if err != nil {
			return nil, err
		}
		offset, err := r.decodeIntegerField()
		if err != nil {
			return nil, err
		}
		offsetSeconds, ok := offsetToI32(offset)
		if !ok {
			return nil, &PVCEError{Kind: ErrInvalidTemporal}
		}
		value, err := NewOffsetDateTime(NewLocalDateTime(date, time), offsetSeconds)
		if err != nil {
			return nil, &PVCEError{Kind: ErrInvalidTemporal}
		}
		return value, nil
	case tagSequence:
		count, err := r.length(r.limits.MaxContainerEntries, "container-entries")
		if err != nil {
			return nil, err
		}
		items := make([]Value, 0, count)
		for i := 0; i < count; i++ {
			childTag, childPayload, err := r.record()
			if err != nil {
				return nil, err
			}
			item, err := r.decodeRecord(childTag, childPayload, depth+1)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return NewArray(items...), nil
	case tagObject:
		count, err := r.length(r.limits.MaxContainerEntries, "container-entries")
		if err != nil {
			return nil, err
		}
		builder := NewObjectBuilder()
		for i := 0; i < count; i++ {
			keyTag, keyPayload, err := r.record()
			if err != nil {
				return nil, err
			}
			if keyTag != tagString {
				return nil, &PVCEError{Kind: ErrObjectKeyNotString}
			}
			keyValue, err := r.decodeRecord(keyTag, keyPayload, depth+1)
			if err != nil {
				return nil, err
			}
			valueTag, valuePayload, err := r.record()
			if err != nil {
				return nil, err
			}
			item, err := r.decodeRecord(valueTag, valuePayload, depth+1)
			if err != nil {
				return nil, err
			}
			// Decoded key values are always non-nil Strings, so any Insert
			// failure here is a duplicate key (lib.rs:807-809).
			if err := builder.Insert(string(keyValue.(String)), item); err != nil {
				return nil, &PVCEError{Kind: ErrDuplicateObjectKey}
			}
		}
		return builder.Build(), nil
	case tagEntryMapping:
		count, err := r.length(r.limits.MaxContainerEntries, "container-entries")
		if err != nil {
			return nil, err
		}
		builder := NewEntryMappingBuilder()
		for i := 0; i < count; i++ {
			keyTag, keyPayload, err := r.record()
			if err != nil {
				return nil, err
			}
			key, err := r.decodeRecord(keyTag, keyPayload, depth+1)
			if err != nil {
				return nil, err
			}
			valueTag, valuePayload, err := r.record()
			if err != nil {
				return nil, err
			}
			value, err := r.decodeRecord(valueTag, valuePayload, depth+1)
			if err != nil {
				return nil, err
			}
			// Decoded values are never nil, so Push cannot fail (the Rust
			// EntryMappingBuilder::push, lib.rs:813-824; no key-tag
			// restriction and no deduplication).
			if err := builder.Push(key, value); err != nil {
				return nil, err
			}
		}
		return builder.Build(), nil
	default:
		// Includes the Rust extended tag 0x7f: Go has no ExtendedValue type
		// in the closed fifteen-kind value model.
		return nil, &PVCEError{Kind: ErrUnknownCoreTag, Value: tag}
	}
}

// decodeInteger decodes one integer payload (the Rust
// decode_integer_payload, crates/consema-pvce/src/lib.rs:835-846).
func (r *reader) decodeInteger() (Integer, error) {
	sign, err := r.octet()
	if err != nil {
		return Integer{}, err
	}
	n, err := r.length(r.limits.MaxIntegerBytes, "integer-bytes")
	if err != nil {
		return Integer{}, err
	}
	magnitude, err := r.take(n)
	if err != nil {
		return Integer{}, err
	}
	switch {
	case sign == 0 && len(magnitude) == 0:
		return NewInteger(nil), nil
	case sign == 0:
		return Integer{}, &PVCEError{Kind: ErrNonCanonicalInteger}
	case sign != 1 && sign != 2:
		return Integer{}, &PVCEError{Kind: ErrInvalidIntegerSign, Value: uint64(sign)}
	case len(magnitude) == 0 || magnitude[0] == 0:
		return Integer{}, &PVCEError{Kind: ErrNonCanonicalInteger}
	}
	value := new(big.Int).SetBytes(magnitude)
	if sign == 2 {
		value.Neg(value)
	}
	return NewInteger(value), nil
}

// decodeIntegerField decodes one length-prefixed integer field (the Rust
// decode_integer_field, crates/consema-pvce/src/lib.rs:848-860).
func (r *reader) decodeIntegerField() (Integer, error) {
	n, err := r.length(r.limits.MaxIntegerBytes+16, "integer-field")
	if err != nil {
		return Integer{}, err
	}
	payload, err := r.take(n)
	if err != nil {
		return Integer{}, err
	}
	field := &reader{bytes: payload, limits: r.limits, nodes: r.nodes}
	value, err := field.decodeInteger()
	if err != nil {
		return Integer{}, err
	}
	if field.offset != len(field.bytes) {
		return Integer{}, &PVCEError{Kind: ErrTrailingField}
	}
	return value, nil
}

// decodeDecimal decodes one decimal payload and rejects unnormalized
// coefficient/exponent pairs (the Rust decode_decimal_payload,
// crates/consema-pvce/src/lib.rs:862-870).
func (r *reader) decodeDecimal() (Decimal, error) {
	coefficient, err := r.decodeIntegerField()
	if err != nil {
		return Decimal{}, err
	}
	exponent, err := r.decodeIntegerField()
	if err != nil {
		return Decimal{}, err
	}
	decimal := NewDecimal(coefficient.Int(), exponent.Int())
	if decimal.Coefficient().Cmp(coefficient.Int()) != 0 ||
		decimal.Exponent().Cmp(exponent.Int()) != 0 {
		return Decimal{}, &PVCEError{Kind: ErrNonCanonicalDecimal}
	}
	return decimal, nil
}

// decodeDecimalField decodes one length-prefixed decimal field (the Rust
// decode_decimal_field, crates/consema-pvce/src/lib.rs:872-888).
func (r *reader) decodeDecimalField() (Decimal, error) {
	n, err := r.length(r.limits.MaxIntegerBytes*2+32, "decimal-field")
	if err != nil {
		return Decimal{}, err
	}
	payload, err := r.take(n)
	if err != nil {
		return Decimal{}, err
	}
	field := &reader{bytes: payload, limits: r.limits, nodes: r.nodes}
	value, err := field.decodeDecimal()
	if err != nil {
		return Decimal{}, err
	}
	if field.offset != len(field.bytes) {
		return Decimal{}, &PVCEError{Kind: ErrTrailingField}
	}
	return value, nil
}

// decodeDate decodes one date payload (the Rust decode_date_payload,
// crates/consema-pvce/src/lib.rs:895-900). Invalid calendar fields map to
// ErrInvalidTemporal (map_build_error, lib.rs:971-979).
func (r *reader) decodeDate() (Date, error) {
	year, err := r.decodeIntegerField()
	if err != nil {
		return Date{}, err
	}
	month, err := r.octet()
	if err != nil {
		return Date{}, err
	}
	day, err := r.octet()
	if err != nil {
		return Date{}, err
	}
	date, err := NewDate(year.Int(), month, day)
	if err != nil {
		return Date{}, &PVCEError{Kind: ErrInvalidTemporal}
	}
	return date, nil
}

// decodeDateField decodes one length-prefixed date field (the Rust
// decode_date_field, crates/consema-pvce/src/lib.rs:902-914).
func (r *reader) decodeDateField() (Date, error) {
	n, err := r.length(r.limits.MaxIntegerBytes+32, "date-field")
	if err != nil {
		return Date{}, err
	}
	payload, err := r.take(n)
	if err != nil {
		return Date{}, err
	}
	field := &reader{bytes: payload, limits: r.limits, nodes: r.nodes}
	value, err := field.decodeDate()
	if err != nil {
		return Date{}, err
	}
	if field.offset != len(field.bytes) {
		return Date{}, &PVCEError{Kind: ErrTrailingField}
	}
	return value, nil
}

// decodeTime decodes one time payload (the Rust decode_time_payload,
// crates/consema-pvce/src/lib.rs:916-922). Invalid fields map to
// ErrInvalidTemporal.
func (r *reader) decodeTime() (Time, error) {
	hour, err := r.octet()
	if err != nil {
		return Time{}, err
	}
	minute, err := r.octet()
	if err != nil {
		return Time{}, err
	}
	second, err := r.octet()
	if err != nil {
		return Time{}, err
	}
	fraction, err := r.decodeDecimalField()
	if err != nil {
		return Time{}, err
	}
	time, err := NewTime(hour, minute, second, fraction)
	if err != nil {
		return Time{}, &PVCEError{Kind: ErrInvalidTemporal}
	}
	return time, nil
}

// decodeTimeField decodes one length-prefixed time field (the Rust
// decode_time_field, crates/consema-pvce/src/lib.rs:924-940).
func (r *reader) decodeTimeField() (Time, error) {
	n, err := r.length(r.limits.MaxIntegerBytes*2+64, "time-field")
	if err != nil {
		return Time{}, err
	}
	payload, err := r.take(n)
	if err != nil {
		return Time{}, err
	}
	field := &reader{bytes: payload, limits: r.limits, nodes: r.nodes}
	value, err := field.decodeTime()
	if err != nil {
		return Time{}, err
	}
	if field.offset != len(field.bytes) {
		return Time{}, &PVCEError{Kind: ErrTrailingField}
	}
	return value, nil
}

// offsetToI32 converts a decoded offset integer to an int32 (the Rust
// to_i64().and_then(i32::try_from), crates/consema-pvce/src/lib.rs:768-771).
func offsetToI32(offset Integer) (int32, bool) {
	value := offset.safeValue()
	if !value.IsInt64() {
		return 0, false
	}
	int64Value := value.Int64()
	if int64Value < -1<<31 || int64Value > 1<<31-1 {
		return 0, false
	}
	return int32(int64Value), true
}

// decodeBlob decodes one length-prefixed byte string (the Rust decode_blob,
// crates/consema-pvce/src/lib.rs:890-893).
func (r *reader) decodeBlob() ([]byte, error) {
	n, err := r.length(r.limits.MaxBlobBytes, "blob-bytes")
	if err != nil {
		return nil, err
	}
	return r.take(n)
}

// sizer measures a value's canonical PVCE/1 stream size under encode limits
// without producing bytes (the Rust Sizer, crates/consema-pvce/src/lib.rs:
// 170-364).
type sizer struct {
	limits EncodeLimits
	nodes  int
}

// recordSize returns the encoded size of one record at the given depth.
func (s *sizer) recordSize(v Value, depth int) (int, error) {
	if depth > s.limits.MaxDepth {
		return 0, resourceLimit("nesting-depth")
	}
	s.nodes++
	if s.nodes > s.limits.MaxNodes {
		return 0, resourceLimit("value-nodes")
	}
	var tag uint64
	var payload int
	switch val := v.(type) {
	case Null:
		tag = tagNull
	case Boolean:
		if bool(val) {
			tag = tagTrue
		} else {
			tag = tagFalse
		}
	case String:
		tag = tagString
		n := len(val)
		if n > s.limits.MaxBlobBytes {
			return 0, resourceLimit("blob-bytes")
		}
		payload = varintSize(uint64(n)) + n
	case Integer:
		tag = tagInteger
		n := len(val.safeValue().Bytes())
		if n > s.limits.MaxIntegerBytes {
			return 0, resourceLimit("integer-bytes")
		}
		payload = 1 + varintSize(uint64(n)) + n
	case Decimal:
		tag = tagDecimal
		coefficient, err := s.integerFieldSize(val.safeCoefficient())
		if err != nil {
			return 0, err
		}
		exponent, err := s.integerFieldSize(val.safeExponent())
		if err != nil {
			return 0, err
		}
		payload = coefficient + exponent
	case BinaryFloat32:
		tag = tagFloat32
		payload = 4
	case BinaryFloat64:
		tag = tagFloat64
		payload = 8
	case Bytes:
		tag = tagBytes
		n := len(val)
		if n > s.limits.MaxBlobBytes {
			return 0, resourceLimit("blob-bytes")
		}
		payload = varintSize(uint64(n)) + n
	case Date:
		if !dateValid(val) {
			return 0, &PVCEError{Kind: ErrInvalidValue}
		}
		tag = tagDate
		year, err := s.integerFieldSize(val.year.safeValue())
		if err != nil {
			return 0, err
		}
		payload = year + 2
	case Time:
		if !timeValid(val) {
			return 0, &PVCEError{Kind: ErrInvalidValue}
		}
		tag = tagTime
		fraction, err := s.decimalFieldSize(val.fractionalSecond)
		if err != nil {
			return 0, err
		}
		payload = 3 + fraction
	case LocalDateTime:
		if !localValid(val) {
			return 0, &PVCEError{Kind: ErrInvalidValue}
		}
		tag = tagLocalDateTime
		date, err := s.dateFieldSize(val.date)
		if err != nil {
			return 0, err
		}
		time, err := s.timeFieldSize(val.time)
		if err != nil {
			return 0, err
		}
		payload = date + time
	case OffsetDateTime:
		if !localValid(val.local) {
			return 0, &PVCEError{Kind: ErrInvalidValue}
		}
		tag = tagOffsetDateTime
		date, err := s.dateFieldSize(val.local.date)
		if err != nil {
			return 0, err
		}
		time, err := s.timeFieldSize(val.local.time)
		if err != nil {
			return 0, err
		}
		offset, err := s.integerFieldSize(big.NewInt(int64(val.offsetSeconds)))
		if err != nil {
			return 0, err
		}
		payload = date + time + offset
	case *Array:
		if val == nil {
			return 0, &PVCEError{Kind: ErrInvalidValue}
		}
		tag = tagSequence
		n := len(val.items)
		if n > s.limits.MaxContainerEntries {
			return 0, resourceLimit("container-entries")
		}
		payload = varintSize(uint64(n))
		for _, item := range val.items {
			size, err := s.recordSize(item, depth+1)
			if err != nil {
				return 0, err
			}
			payload += size
		}
	case *Object:
		if val == nil {
			return 0, &PVCEError{Kind: ErrInvalidValue}
		}
		tag = tagObject
		n := len(val.entries)
		if n > s.limits.MaxContainerEntries {
			return 0, resourceLimit("container-entries")
		}
		payload = varintSize(uint64(n))
		for _, entry := range val.entries {
			// Object keys are encoded as String records and count as nodes,
			// exactly as in the Rust Sizer (lib.rs:332-341).
			keySize, err := s.recordSize(String(entry.Key), depth+1)
			if err != nil {
				return 0, err
			}
			valueSize, err := s.recordSize(entry.Value, depth+1)
			if err != nil {
				return 0, err
			}
			payload += keySize + valueSize
		}
	case *EntryMapping:
		if val == nil {
			return 0, &PVCEError{Kind: ErrInvalidValue}
		}
		tag = tagEntryMapping
		n := len(val.entries)
		if n > s.limits.MaxContainerEntries {
			return 0, resourceLimit("container-entries")
		}
		payload = varintSize(uint64(n))
		for _, entry := range val.entries {
			// Both the key and the value are records and count as nodes,
			// exactly as in the Rust Sizer (lib.rs:350-358).
			keySize, err := s.recordSize(entry.Key, depth+1)
			if err != nil {
				return 0, err
			}
			valueSize, err := s.recordSize(entry.Value, depth+1)
			if err != nil {
				return 0, err
			}
			payload += keySize + valueSize
		}
	default:
		return 0, &PVCEError{Kind: ErrInvalidValue}
	}
	return varintSize(tag) + varintSize(uint64(payload)) + payload, nil
}

// integerFieldSize returns the encoded size of one length-prefixed integer
// field (the Rust Sizer::integer_field_size, lib.rs:194-201).
func (s *sizer) integerFieldSize(value *big.Int) (int, error) {
	n := len(value.Bytes())
	if n > s.limits.MaxIntegerBytes {
		return 0, resourceLimit("integer-bytes")
	}
	payload := 1 + varintSize(uint64(n)) + n
	return varintSize(uint64(payload)) + payload, nil
}

// decimalFieldSize returns the encoded size of one length-prefixed decimal
// field (the Rust Sizer::decimal_field_size, lib.rs:203-209).
func (s *sizer) decimalFieldSize(value Decimal) (int, error) {
	coefficient, err := s.integerFieldSize(value.safeCoefficient())
	if err != nil {
		return 0, err
	}
	exponent, err := s.integerFieldSize(value.safeExponent())
	if err != nil {
		return 0, err
	}
	payload := coefficient + exponent
	return varintSize(uint64(payload)) + payload, nil
}

// dateFieldSize returns the encoded size of one length-prefixed date field
// (the Rust Sizer::date_field_size, lib.rs:211-214).
func (s *sizer) dateFieldSize(value Date) (int, error) {
	year, err := s.integerFieldSize(value.year.safeValue())
	if err != nil {
		return 0, err
	}
	payload := year + 2
	return varintSize(uint64(payload)) + payload, nil
}

// timeFieldSize returns the encoded size of one length-prefixed time field
// (the Rust Sizer::time_field_size, lib.rs:216-219).
func (s *sizer) timeFieldSize(value Time) (int, error) {
	fraction, err := s.decimalFieldSize(value.fractionalSecond)
	if err != nil {
		return 0, err
	}
	payload := 3 + fraction
	return varintSize(uint64(payload)) + payload, nil
}
