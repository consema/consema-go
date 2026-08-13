package document

import (
	"fmt"
	"sort"
	"unicode/utf8"
)

// checkpointStride is the scalar count between full position checkpoints
// (document source.rs:13). Coordinate conversion scans at most one stride
// past a checkpoint instead of the whole source.
const checkpointStride = 256

// SourceLimits are the resource bounds applied while a source snapshot is
// constructed (document source.rs:382-409).
type SourceLimits struct {
	// MaxRawBytes is the maximum retained raw bytes.
	MaxRawBytes int
	// MaxDecodedUTF8Bytes is the maximum decoded UTF-8 bytes.
	MaxDecodedUTF8Bytes int
	// MaxDecodedScalars is the maximum decoded Unicode scalar values.
	MaxDecodedScalars int
}

// DefaultSourceLimits returns the frozen defaults (64 MiB raw, 128 MiB
// decoded, 64 MiB scalars).
func DefaultSourceLimits() SourceLimits {
	return SourceLimits{
		MaxRawBytes:         64 << 20,
		MaxDecodedUTF8Bytes: 128 << 20,
		MaxDecodedScalars:   64 << 20,
	}
}

// UnboundedSourceLimits returns the compatibility limits for already
// bounded format parsers.
func UnboundedSourceLimits() SourceLimits {
	return SourceLimits{
		MaxRawBytes:         int(^uint(0) >> 1),
		MaxDecodedUTF8Bytes: int(^uint(0) >> 1),
		MaxDecodedScalars:   int(^uint(0) >> 1),
	}
}

// SourceErrorKind classifies a stable source construction failure
// (document source.rs:669-708).
type SourceErrorKind uint8

// The stable source construction failure classes.
const (
	// SourceErrorInvalidUtf8 is the compatibility error returned by
	// NewSourceSnapshotFromUTF8.
	SourceErrorInvalidUtf8 SourceErrorKind = iota
	// SourceErrorInvalidSequence: raw bytes are not a valid sequence in
	// the selected encoding.
	SourceErrorInvalidSequence
	// SourceErrorEncodingConflict: BOM, declaration, and caller inputs made
	// contradictory assertions.
	SourceErrorEncodingConflict
	// SourceErrorUnsupportedBom: a UTF-32 byte-order mark is recognized but
	// unsupported by v1.
	SourceErrorUnsupportedBom
	// SourceErrorResourceLimit: a configured construction bound was
	// exceeded.
	SourceErrorResourceLimit
	// SourceErrorOffsetOverflow: coordinate arithmetic exceeded the host
	// representation.
	SourceErrorOffsetOverflow
)

// SourceError is a typed source snapshot construction failure. It
// implements error and the RFC 0016 §6 Code() contract.
type SourceError struct {
	// Kind identifies the failure.
	Kind SourceErrorKind
	// Encoding is the selected encoding of InvalidSequence.
	Encoding SourceEncoding
	// ByteOffset is the first byte at which a valid sequence could not be
	// formed (InvalidSequence), or the prefix length that was valid UTF-8
	// (InvalidUtf8).
	ByteOffset int
	// Bom is the BOM-derived encoding of an EncodingConflict.
	Bom *BomKind
	// Declaration is the declaration-derived encoding of an
	// EncodingConflict.
	Declaration *SourceEncoding
	// CallerOverride is the caller-selected encoding of an
	// EncodingConflict.
	CallerOverride *SourceEncoding
	// UnsupportedBom identifies the unsupported marker.
	UnsupportedBom UnsupportedBomKind
	// Name is the stable limit name of a ResourceLimit.
	Name string
	// Observed is the observed amount of a ResourceLimit.
	Observed int
	// Limit is the configured maximum of a ResourceLimit.
	Limit int
}

// Error implements error.
func (e *SourceError) Error() string {
	switch e.Kind {
	case SourceErrorInvalidUtf8:
		return fmt.Sprintf("invalid UTF-8 after byte %d", e.ByteOffset)
	case SourceErrorInvalidSequence:
		return fmt.Sprintf("invalid %s sequence at byte offset %d", e.Encoding.AsStr(), e.ByteOffset)
	case SourceErrorEncodingConflict:
		return "encoding facts conflict"
	case SourceErrorUnsupportedBom:
		return fmt.Sprintf("unsupported byte-order mark %s", e.UnsupportedBom)
	case SourceErrorResourceLimit:
		return fmt.Sprintf("source limit %s exceeded: observed %d, limit %d", e.Name, e.Observed, e.Limit)
	case SourceErrorOffsetOverflow:
		return "coordinate arithmetic overflow"
	}
	return "source error"
}

// Code returns the frozen registered code for the failure (document
// source.rs source_error mapping).
func (e *SourceError) Code() string {
	switch e.Kind {
	case SourceErrorInvalidUtf8, SourceErrorInvalidSequence:
		return "core.source.invalid-sequence@1"
	case SourceErrorEncodingConflict:
		return "core.source.encoding-conflict@1"
	case SourceErrorUnsupportedBom:
		return "core.source.unsupported-bom@1"
	case SourceErrorResourceLimit, SourceErrorOffsetOverflow:
		return "core.source.resource-limit@1"
	}
	return "core.source.invalid-sequence@1"
}

// DecodedPosition is one exact boundary expressed in every supported
// coordinate system (document source.rs:411-423).
type DecodedPosition struct {
	// RawByte is the offset in retained raw source bytes.
	RawByte int
	// DecodedUTF8Byte is the offset in the UTF-8 representation of decoded
	// text.
	DecodedUTF8Byte int
	// UnicodeScalarOffset is the number of decoded Unicode scalar values.
	UnicodeScalarOffset int
	// UTF16CodeUnitOffset is the number of UTF-16 code units in decoded
	// text.
	UTF16CodeUnitOffset int
}

// DecodedOffsetKind identifies one decoded coordinate system (document
// source.rs:425-434).
type DecodedOffsetKind uint8

// The three decoded coordinate systems.
const (
	// DecodedOffsetUTF8Byte is a UTF-8 byte offset in decoded text.
	DecodedOffsetUTF8Byte DecodedOffsetKind = iota
	// DecodedOffsetUnicodeScalar is a Unicode scalar offset in decoded
	// text.
	DecodedOffsetUnicodeScalar
	// DecodedOffsetUTF16CodeUnit is a UTF-16 code-unit offset in decoded
	// text.
	DecodedOffsetUTF16CodeUnit
)

// DecodedOffset is a decoded coordinate to resolve back to an exact
// raw-byte boundary.
type DecodedOffset struct {
	kind  DecodedOffsetKind
	value int
}

// NewUtf8ByteOffset is a UTF-8 byte offset in decoded text.
func NewUtf8ByteOffset(value int) DecodedOffset {
	return DecodedOffset{kind: DecodedOffsetUTF8Byte, value: value}
}

// NewUnicodeScalarOffset is a Unicode scalar offset in decoded text.
func NewUnicodeScalarOffset(value int) DecodedOffset {
	return DecodedOffset{kind: DecodedOffsetUnicodeScalar, value: value}
}

// NewUtf16CodeUnitOffset is a UTF-16 code-unit offset in decoded text.
func NewUtf16CodeUnitOffset(value int) DecodedOffset {
	return DecodedOffset{kind: DecodedOffsetUTF16CodeUnit, value: value}
}

// Kind returns the coordinate system.
func (o DecodedOffset) Kind() DecodedOffsetKind { return o.kind }

// Value returns the requested offset in the coordinate system.
func (o DecodedOffset) Value() int { return o.value }

// SourceSnapshot is immutable ownership of exact raw bytes plus explicitly
// derived text facts (document source.rs:476-666). The raw bytes, the
// SHA-256 content digest, the resolved encoding facts, and the decoded
// text are fixed at construction; coordinate conversion never re-validates
// the source.
type SourceSnapshot struct {
	bytes       []byte
	digest      ContentDigest
	encoding    EncodingFacts
	text        string
	hasText     bool
	widths      []byte
	checkpoints []DecodedPosition
	terminal    DecodedPosition
	identity    SnapshotIdentity
}

// NewSourceSnapshotFromRaw constructs a source from raw bytes using
// explicit resolution inputs and limits (document source.rs:488-550).
func NewSourceSnapshotFromRaw(bytes []byte, request EncodingRequest, limits SourceLimits) (*SourceSnapshot, error) {
	if len(bytes) > limits.MaxRawBytes {
		return nil, &SourceError{Kind: SourceErrorResourceLimit, Name: "raw-bytes",
			Observed: len(bytes), Limit: limits.MaxRawBytes}
	}
	encoding, err := resolveEncoding(bytes, request)
	if err != nil {
		return nil, err
	}
	digest := DigestOf(bytes)
	snapshot := &SourceSnapshot{
		bytes:    append([]byte(nil), bytes...),
		digest:   digest,
		encoding: encoding,
		identity: allocateSnapshotIdentity(),
	}
	selected := encoding.selected
	switch selected.kind {
	case EncodingBinary:
		// No decoded-text view.
	case EncodingUtf8:
		if !utf8.Valid(bytes) {
			return nil, &SourceError{Kind: SourceErrorInvalidSequence, Encoding: selected,
				ByteOffset: invalidUTF8Offset(bytes)}
		}
		snapshot.text = string(bytes)
		snapshot.hasText = true
	case EncodingUtf16Le, EncodingUtf16Be:
		text, err := decodeUTF16(bytes, selected.kind == EncodingUtf16Le, limits)
		if err != nil {
			return nil, err
		}
		snapshot.text = text
		snapshot.hasText = true
	case EncodingLatin1:
		text, err := decodeLatin1(bytes, limits)
		if err != nil {
			return nil, err
		}
		snapshot.text = text
		snapshot.hasText = true
	case EncodingWindowsCodePage:
		text, widths, err := decodeWindowsCodePage(bytes, selected.codePage, limits)
		if err != nil {
			return nil, err
		}
		snapshot.text = text
		snapshot.hasText = true
		snapshot.widths = widths
	default:
		return nil, &SourceError{Kind: SourceErrorInvalidSequence, Encoding: selected}
	}
	if snapshot.hasText {
		if err := snapshot.buildIndex(limits); err != nil {
			return nil, err
		}
	}
	return snapshot, nil
}

// NewSourceSnapshotFromUTF8 is the compatibility constructor for exact
// UTF-8 sources (document source.rs:552-568).
func NewSourceSnapshotFromUTF8(bytes []byte) (*SourceSnapshot, error) {
	override := Utf8Encoding()
	snapshot, err := NewSourceSnapshotFromRaw(bytes,
		NewEncodingRequest(override).WithCallerOverride(override),
		UnboundedSourceLimits())
	if err != nil {
		if sourceError, ok := err.(*SourceError); ok && sourceError.Kind == SourceErrorInvalidSequence {
			return nil, &SourceError{Kind: SourceErrorInvalidUtf8, ByteOffset: sourceError.ByteOffset}
		}
		return nil, err
	}
	return snapshot, nil
}

// NewSourceSnapshotFromBinary constructs an opaque binary source without
// decoding or BOM interpretation (document source.rs:570-576).
func NewSourceSnapshotFromBinary(bytes []byte, limits SourceLimits) (*SourceSnapshot, error) {
	return NewSourceSnapshotFromRaw(bytes, BinaryEncodingRequest(), limits)
}

// Bytes returns the exact retained source bytes.
func (s *SourceSnapshot) Bytes() []byte { return append([]byte(nil), s.bytes...) }

// Digest returns the stable SHA-256 identity of the exact retained bytes.
func (s *SourceSnapshot) Digest() ContentDigest { return s.digest }

// EncodingFacts returns the complete encoding-resolution facts.
func (s *SourceSnapshot) EncodingFacts() EncodingFacts { return s.encoding }

// DecodedText returns the decoded text, or false for an opaque binary
// source. The text is fully validated exactly once at construction; each
// call returns the stored view in O(1).
func (s *SourceSnapshot) DecodedText() (string, bool) { return s.text, s.hasText }

// Len returns the source byte length.
func (s *SourceSnapshot) Len() int { return len(s.bytes) }

// IsEmpty reports whether the source is empty.
func (s *SourceSnapshot) IsEmpty() bool { return len(s.bytes) == 0 }

// Identity returns the process-local snapshot identity; two snapshots of
// the same bytes always have distinct identities.
func (s *SourceSnapshot) Identity() SnapshotIdentity { return s.identity }

// DecodedPosition resolves one raw byte offset only when it is a decoded
// scalar boundary (document source.rs:623-641).
func (s *SourceSnapshot) DecodedPosition(rawByte int) (DecodedPosition, error) {
	if rawByte < 0 || rawByte > len(s.bytes) {
		return DecodedPosition{}, &LocationError{Kind: LocationOutOfBounds}
	}
	if !s.hasText {
		return DecodedPosition{}, &LocationError{Kind: LocationNoDecodedText}
	}
	checkpoint := lastCheckpoint(s.checkpoints, func(position DecodedPosition) bool {
		return position.RawByte <= rawByte
	})
	return s.scanToRaw(checkpoint, rawByte)
}

// RawByteAt resolves one decoded offset only when it denotes a scalar
// boundary (document source.rs:644-665).
func (s *SourceSnapshot) RawByteAt(offset DecodedOffset) (int, error) {
	if !s.hasText {
		return 0, &LocationError{Kind: LocationNoDecodedText}
	}
	target := offset.value
	if target > offsetComponent(s.terminal, offset.kind) {
		return 0, &LocationError{Kind: LocationOutOfBounds}
	}
	checkpoint := lastCheckpoint(s.checkpoints, func(position DecodedPosition) bool {
		return offsetComponent(position, offset.kind) <= target
	})
	return s.scanToDecoded(checkpoint, offset)
}

// scanToRaw scans from one checkpoint until the requested raw byte is an
// exact scalar boundary.
func (s *SourceSnapshot) scanToRaw(position DecodedPosition, requested int) (DecodedPosition, error) {
	if position.RawByte == requested {
		return position, nil
	}
	for _, r := range s.text[position.DecodedUTF8Byte:] {
		width := s.scalarRawWidth(position.UnicodeScalarOffset, r)
		position.RawByte += width
		position.DecodedUTF8Byte += utf8.RuneLen(r)
		position.UnicodeScalarOffset++
		position.UTF16CodeUnitOffset += utf16Len(r)
		if position.RawByte == requested {
			return position, nil
		}
		if position.RawByte > requested {
			return DecodedPosition{}, &LocationError{Kind: LocationNotDecodedBoundary}
		}
	}
	return DecodedPosition{}, &LocationError{Kind: LocationOutOfBounds}
}

// scanToDecoded scans from one checkpoint until the requested decoded
// offset denotes a scalar boundary.
func (s *SourceSnapshot) scanToDecoded(position DecodedPosition, requested DecodedOffset) (int, error) {
	target := requested.value
	if offsetComponent(position, requested.kind) == target {
		return position.RawByte, nil
	}
	for _, r := range s.text[position.DecodedUTF8Byte:] {
		width := s.scalarRawWidth(position.UnicodeScalarOffset, r)
		position.RawByte += width
		position.DecodedUTF8Byte += utf8.RuneLen(r)
		position.UnicodeScalarOffset++
		position.UTF16CodeUnitOffset += utf16Len(r)
		observed := offsetComponent(position, requested.kind)
		if observed == target {
			return position.RawByte, nil
		}
		if observed > target {
			return 0, &LocationError{Kind: LocationDecodedOffsetNotBoundary}
		}
	}
	return 0, &LocationError{Kind: LocationOutOfBounds}
}

// scalarRawWidth returns the raw byte width of one decoded scalar: from
// the recorded widths for Windows code pages, else derivable from the
// scalar value.
func (s *SourceSnapshot) scalarRawWidth(scalarOffset int, r rune) int {
	if s.widths != nil {
		return int(s.widths[scalarOffset])
	}
	switch s.encoding.selected.kind {
	case EncodingUtf16Le, EncodingUtf16Be:
		if r >= 0x10000 {
			return 4
		}
		return 2
	case EncodingLatin1:
		return 1
	default: // EncodingUtf8 and 65001 decode.
		return utf8.RuneLen(r)
	}
}

// buildIndex computes the checkpoint table and terminal position and
// enforces the decoded-text limits (document source.rs:1016-1067).
func (s *SourceSnapshot) buildIndex(limits SourceLimits) error {
	if len(s.text) > limits.MaxDecodedUTF8Bytes {
		return &SourceError{Kind: SourceErrorResourceLimit, Name: "decoded-utf8-bytes",
			Observed: len(s.text), Limit: limits.MaxDecodedUTF8Bytes}
	}
	checkpoints := make([]DecodedPosition, 0, len(s.text)/checkpointStride+2)
	position := DecodedPosition{}
	checkpoints = append(checkpoints, position)
	scalars := 0
	for _, r := range s.text {
		width := s.scalarRawWidth(scalars, r)
		position.RawByte += width
		position.DecodedUTF8Byte += utf8.RuneLen(r)
		position.UnicodeScalarOffset++
		position.UTF16CodeUnitOffset += utf16Len(r)
		scalars++
		if scalars > limits.MaxDecodedScalars {
			return &SourceError{Kind: SourceErrorResourceLimit, Name: "decoded-scalars",
				Observed: scalars, Limit: limits.MaxDecodedScalars}
		}
		if scalars%checkpointStride == 0 {
			checkpoints = append(checkpoints, position)
		}
	}
	if s.widths != nil && len(s.widths) != scalars {
		return &SourceError{Kind: SourceErrorOffsetOverflow}
	}
	if checkpoints[len(checkpoints)-1] != position {
		checkpoints = append(checkpoints, position)
	}
	s.checkpoints = checkpoints
	s.terminal = position
	return nil
}

// lastCheckpoint returns the last checkpoint satisfying the predicate; the
// zero checkpoint always satisfies predicates over non-negative targets
// (document source.rs:1082-1088).
func lastCheckpoint(checkpoints []DecodedPosition, predicate func(DecodedPosition) bool) DecodedPosition {
	index := sort.Search(len(checkpoints), func(i int) bool { return !predicate(checkpoints[i]) })
	if index == 0 {
		return checkpoints[0]
	}
	return checkpoints[index-1]
}

func offsetComponent(position DecodedPosition, kind DecodedOffsetKind) int {
	switch kind {
	case DecodedOffsetUTF8Byte:
		return position.DecodedUTF8Byte
	case DecodedOffsetUnicodeScalar:
		return position.UnicodeScalarOffset
	case DecodedOffsetUTF16CodeUnit:
		return position.UTF16CodeUnitOffset
	}
	return 0
}

func utf16Len(r rune) int {
	if r >= 0x10000 {
		return 2
	}
	return 1
}

func invalidUTF8Offset(bytes []byte) int {
	for index := 0; index < len(bytes); {
		if bytes[index] < 0x80 {
			index++
			continue
		}
		r, consumed := utf8.DecodeRune(bytes[index:])
		if r == utf8.RuneError && consumed == 1 {
			return index
		}
		index += consumed
	}
	return len(bytes)
}

// decodeUTF16 decodes one UTF-16 byte stream strictly
// (document source.rs:806-869).
func decodeUTF16(bytes []byte, littleEndian bool, limits SourceLimits) (string, error) {
	if len(bytes)%2 != 0 {
		encoding := Utf16BeEncoding()
		if littleEndian {
			encoding = Utf16LeEncoding()
		}
		return "", &SourceError{Kind: SourceErrorInvalidSequence, Encoding: encoding,
			ByteOffset: len(bytes) - 1}
	}
	var output []byte
	scalars := 0
	for offset := 0; offset < len(bytes); {
		first := readU16(bytes, offset, littleEndian)
		var scalar rune
		consumed := 2
		switch {
		case first >= 0xD800 && first <= 0xDBFF:
			if offset+3 >= len(bytes) {
				return "", invalidSequenceAt(littleEndian, offset)
			}
			second := readU16(bytes, offset+2, littleEndian)
			if second < 0xDC00 || second > 0xDFFF {
				return "", invalidSequenceAt(littleEndian, offset)
			}
			scalar = 0x10000 + rune(first-0xD800)<<10 + rune(second-0xDC00)
			consumed = 4
		case first >= 0xDC00 && first <= 0xDFFF:
			return "", invalidSequenceAt(littleEndian, offset)
		default:
			scalar = rune(first)
		}
		scalars++
		if err := checkDecodedLimits(output, scalars, utf8.RuneLen(scalar), limits); err != nil {
			return "", err
		}
		output = utf8.AppendRune(output, scalar)
		offset += consumed
	}
	return string(output), nil
}

func invalidSequenceAt(littleEndian bool, offset int) *SourceError {
	encoding := Utf16BeEncoding()
	if littleEndian {
		encoding = Utf16LeEncoding()
	}
	return &SourceError{Kind: SourceErrorInvalidSequence, Encoding: encoding, ByteOffset: offset}
}

func readU16(bytes []byte, offset int, littleEndian bool) uint16 {
	if littleEndian {
		return uint16(bytes[offset]) | uint16(bytes[offset+1])<<8
	}
	return uint16(bytes[offset])<<8 | uint16(bytes[offset+1])
}

// decodeLatin1 decodes one ISO-8859-1 byte stream
// (document source.rs:880-894).
func decodeLatin1(bytes []byte, limits SourceLimits) (string, error) {
	var output []byte
	scalars := 0
	for _, byte := range bytes {
		scalars++
		if err := checkDecodedLimits(output, scalars, utf8.RuneLen(rune(byte)), limits); err != nil {
			return "", err
		}
		output = utf8.AppendRune(output, rune(byte))
	}
	return string(output), nil
}

// decodeWindowsCodePage decodes one frozen Windows code page strictly,
// returning the decoded text and the raw byte width of every decoded
// scalar (document source.rs:896-1014). The single-byte pages and CP932
// use the frozen Python-stdlib data shared with go/protocol; CP936,
// CP949, and CP950 are recognized but not decoded — a documented skip per
// RFC 0016 §7 (never silent: non-ASCII bytes fail loudly with
// SourceErrorInvalidSequence, exactly as go/protocol rejects them today;
// G165, adversarial audit 2026-08-13 — the Rust reference decodes these
// pages fully via encoding_rs, so CP936/CP949/CP950 sources diverge
// between implementations; disclosed in go/README.md; no conformance
// vector case touches these pages).
func decodeWindowsCodePage(bytes []byte, page WindowsCodePage, limits SourceLimits) (string, []byte, error) {
	switch page.Number() {
	case 65001:
		return decodeUTF8CodePage(bytes, limits)
	case 932:
		return decodeCP932(bytes, limits)
	case 874, 1250, 1251, 1252, 1253, 1254, 1255, 1256, 1257, 1258:
		return decodeSingleBytePage(bytes, page, limits)
	default:
		return "", nil, &SourceError{Kind: SourceErrorInvalidSequence, ByteOffset: 0}
	}
}

// decodeUTF8CodePage decodes the 65001 (UTF-8) code page strictly.
func decodeUTF8CodePage(bytes []byte, limits SourceLimits) (string, []byte, error) {
	var output []byte
	widths := make([]byte, 0, len(bytes))
	scalars := 0
	for offset := 0; offset < len(bytes); {
		start := offset
		r, size := utf8.DecodeRune(bytes[offset:])
		if r == utf8.RuneError && size == 1 {
			return "", nil, &SourceError{Kind: SourceErrorInvalidSequence, ByteOffset: start}
		}
		offset += size
		scalars++
		if err := checkDecodedLimits(output, scalars, size, limits); err != nil {
			return "", nil, err
		}
		output = utf8.AppendRune(output, r)
		widths = append(widths, byte(size))
	}
	return string(output), widths, nil
}

// decodeCP932 decodes the CP932 code page exactly as go/protocol
// decodes it: ASCII single bytes, half-width katakana 0xA1-0xDF, and the
// frozen single-scalar two-byte table.
func decodeCP932(bytes []byte, limits SourceLimits) (string, []byte, error) {
	var output []byte
	widths := make([]byte, 0, len(bytes))
	scalars := 0
	offset := 0
	for offset < len(bytes) {
		start := offset
		var scalar rune
		switch {
		case bytes[offset] < 0x80:
			scalar = rune(bytes[offset])
			offset++
		case bytes[offset] >= 0xA1 && bytes[offset] <= 0xDF:
			scalar = rune(bytes[offset]) - 0xA1 + 0xFF61
			offset++
		case offset+1 < len(bytes):
			code := uint16(bytes[offset])<<8 | uint16(bytes[offset+1])
			r, ok := cp932Lookup(code)
			if !ok {
				return "", nil, &SourceError{Kind: SourceErrorInvalidSequence, ByteOffset: start}
			}
			scalar = r
			offset += 2
		default:
			return "", nil, &SourceError{Kind: SourceErrorInvalidSequence, ByteOffset: start}
		}
		scalars++
		if err := checkDecodedLimits(output, scalars, utf8.RuneLen(scalar), limits); err != nil {
			return "", nil, err
		}
		output = utf8.AppendRune(output, scalar)
		widths = append(widths, byte(offset-start))
	}
	return string(output), widths, nil
}

// malformedByteSentinel marks a byte that encoding_rs 0.8.35 decodes as
// Malformed in the single-byte authority tables (0xFFFF is not a real
// mapping of any of the nine pages).
const malformedByteSentinel = 0xFFFF

// decodeSingleBytePage decodes one frozen single-byte Windows code page
// exactly as document source.rs decodes it through encoding_rs 0.8.35:
// every byte resolves through the frozen authority table, C1 control
// positions decode to their U+00xx scalars, and the malformed sentinel
// (0xFFFF) fails the whole source with SourceErrorInvalidSequence at the
// byte offset, matching the Rust decode_to_string_without_replacement
// path.
func decodeSingleBytePage(bytes []byte, page WindowsCodePage, limits SourceLimits) (string, []byte, error) {
	var table [128]uint16
	switch page.Number() {
	case 874:
		table = cp874Table
	case 1250:
		table = cp1250Table
	case 1251:
		table = cp1251Table
	case 1252:
		table = cp1252Table
	case 1253:
		table = cp1253Table
	case 1254:
		table = cp1254Table
	case 1255:
		table = cp1255Table
	case 1256:
		table = cp1256Table
	case 1257:
		table = cp1257Table
	case 1258:
		table = cp1258Table
	default:
		return "", nil, &SourceError{Kind: SourceErrorInvalidSequence, ByteOffset: 0}
	}
	var output []byte
	widths := make([]byte, 0, len(bytes))
	scalars := 0
	for offset, b := range bytes {
		scalar := rune(b)
		if b >= 0x80 {
			scalar = rune(table[b-0x80])
			if scalar == malformedByteSentinel {
				// The malformed check precedes the limit checks exactly as
				// the Rust loop reports Malformed before the next limit
				// projection.
				return "", nil, &SourceError{Kind: SourceErrorInvalidSequence,
					Encoding: WindowsCodePageEncoding(page), ByteOffset: offset}
			}
		}
		scalars++
		if err := checkDecodedLimits(output, scalars, utf8.RuneLen(scalar), limits); err != nil {
			return "", nil, err
		}
		output = utf8.AppendRune(output, scalar)
		widths = append(widths, 1)
	}
	return string(output), widths, nil
}

// cp932Lookup resolves one two-byte CP932 code in the frozen sorted
// table.
func cp932Lookup(code uint16) (rune, bool) {
	index := sort.Search(len(cp932Table), func(i int) bool { return cp932Table[i].code >= code })
	if index < len(cp932Table) && cp932Table[index].code == code {
		return rune(cp932Table[index].rune), true
	}
	return 0, false
}

// checkDecodedLimits enforces the decoded scalar and UTF-8 byte budgets
// for one more scalar whose UTF-8 representation has the given length
// (document source.rs:857-864, 886-891: the projected byte count is
// checked before the scalar is appended).
func checkDecodedLimits(output []byte, scalars, nextUTF8Len int, limits SourceLimits) error {
	if scalars > limits.MaxDecodedScalars {
		return &SourceError{Kind: SourceErrorResourceLimit, Name: "decoded-scalars",
			Observed: scalars, Limit: limits.MaxDecodedScalars}
	}
	if len(output)+nextUTF8Len > limits.MaxDecodedUTF8Bytes {
		return &SourceError{Kind: SourceErrorResourceLimit, Name: "decoded-utf8-bytes",
			Observed: len(output) + nextUTF8Len, Limit: limits.MaxDecodedUTF8Bytes}
	}
	return nil
}
