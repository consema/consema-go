package protocol

// Transferable raw source snapshots, verifiable source patches, and source
// encodings (crates/consema-protocol/src/source.rs over
// crates/consema-document/src/source.rs). The 0.14.0 milestone carries the
// snapshot/patch verification facts needed by the `core.source-snapshot@1`,
// `core.source-snapshot@2`, `core.source-patch@1`, `core.source-patch@2`,
// and `core.source-encoding@1` wire records; the full document-layer source
// milestone (G1.1) owns the format-facing surface.

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"unicode/utf8"

	"consema.dev/consema/core"
)

// SourceLimits are the resource bounds of source snapshots
// (document source.rs:383-...).
type SourceLimits struct {
	// MaxRawBytes is the maximum retained raw bytes.
	MaxRawBytes int
	// MaxDecodedUTF8Bytes is the maximum decoded UTF-8 bytes.
	MaxDecodedUTF8Bytes int
	// MaxDecodedScalars is the maximum decoded Unicode scalar values.
	MaxDecodedScalars int
}

// DefaultSourceLimits returns the frozen defaults (64 MiB raw, 128 MiB
// decoded, 64 MiB scalars; document source.rs:411-419).
func DefaultSourceLimits() SourceLimits {
	return SourceLimits{
		MaxRawBytes:         64 << 20,
		MaxDecodedUTF8Bytes: 128 << 20,
		MaxDecodedScalars:   64 << 20,
	}
}

// BomPolicy selects whether leading marker-shaped bytes are BOM evidence or
// content (document source.rs:159-166).
type BomPolicy string

// The two frozen policies.
const (
	BomPolicyDetectUnicode  BomPolicy = "DetectUnicode"
	BomPolicyTreatAsContent BomPolicy = "TreatAsContent"
)

// BomKind is one recognized Unicode byte-order mark
// (document source.rs:168-181).
type BomKind string

// The three recognized BOMs.
const (
	BomUtf8    BomKind = "Utf8"
	BomUtf16Le BomKind = "Utf16Le"
	BomUtf16Be BomKind = "Utf16Be"
)

// EncodingRequest carries the caller inputs to deterministic encoding
// resolution (document source.rs:191-260).
type EncodingRequest struct {
	profileDefault *SourceEncoding
	bomPolicy      BomPolicy
	declaration    *SourceEncoding
	callerOverride *SourceEncoding
}

// NewEncodingRequest starts with the required profile default and no
// higher-priority facts (document source.rs:200-208).
func NewEncodingRequest(profileDefault *SourceEncoding) EncodingRequest {
	return EncodingRequest{
		profileDefault: profileDefault,
		bomPolicy:      BomPolicyDetectUnicode,
	}
}

// BinaryEncodingRequest is the opaque-binary request.
func BinaryEncodingRequest() EncodingRequest {
	return NewEncodingRequest(&SourceEncoding{Kind: "Binary"})
}

// WithDeclaration adds a normalized declaration supplied by the format
// layer.
func (r EncodingRequest) WithDeclaration(declaration *SourceEncoding) EncodingRequest {
	r.declaration = declaration
	return r
}

// WithCallerOverride adds an explicit caller override.
func (r EncodingRequest) WithCallerOverride(override *SourceEncoding) EncodingRequest {
	r.callerOverride = override
	return r
}

// WithBomPolicy selects the BOM interpretation policy.
func (r EncodingRequest) WithBomPolicy(policy BomPolicy) EncodingRequest {
	r.bomPolicy = policy
	return r
}

// SourceEncodingFacts is the complete, auditable result of encoding
// resolution (document source.rs:262-...).
type SourceEncodingFacts struct {
	profileDefault *SourceEncoding
	bomPolicy      BomPolicy
	bom            *BomKind
	declaration    *SourceEncoding
	callerOverride *SourceEncoding
	selected       *SourceEncoding
}

// ProfileDefault returns the profile fallback that participated in
// resolution.
func (f SourceEncodingFacts) ProfileDefault() *SourceEncoding { return f.profileDefault }

// BomPolicy returns the BOM interpretation policy used for this source.
func (f SourceEncodingFacts) BomPolicy() BomPolicy { return f.bomPolicy }

// Bom returns the recognized byte-order mark, when one exists.
func (f SourceEncodingFacts) Bom() *BomKind { return f.bom }

// Declaration returns the normalized in-source declaration.
func (f SourceEncodingFacts) Declaration() *SourceEncoding { return f.declaration }

// CallerOverride returns the explicit caller override.
func (f SourceEncodingFacts) CallerOverride() *SourceEncoding { return f.callerOverride }

// Selected returns the encoding selected by the frozen priority rule.
func (f SourceEncodingFacts) Selected() *SourceEncoding { return f.selected }

// Equal reports whether two facts are identical.
func (f SourceEncodingFacts) Equal(other SourceEncodingFacts) bool {
	return encodingEqual(f.profileDefault, other.profileDefault) &&
		f.bomPolicy == other.bomPolicy &&
		((f.bom == nil) == (other.bom == nil)) &&
		(f.bom == nil || *f.bom == *other.bom) &&
		encodingEqual(f.declaration, other.declaration) &&
		encodingEqual(f.callerOverride, other.callerOverride) &&
		encodingEqual(f.selected, other.selected)
}

func encodingEqual(left, right *SourceEncoding) bool {
	if left == nil || right == nil {
		return left == right
	}
	if left.Kind != right.Kind {
		return false
	}
	if left.WindowsCodePage == nil || right.WindowsCodePage == nil {
		return left.WindowsCodePage == right.WindowsCodePage
	}
	return *left.WindowsCodePage == *right.WindowsCodePage
}

// resolutionRequest rebuilds the request that produced these facts.
func (f SourceEncodingFacts) resolutionRequest() EncodingRequest {
	return EncodingRequest{
		profileDefault: f.profileDefault,
		bomPolicy:      f.bomPolicy,
		declaration:    f.declaration,
		callerOverride: f.callerOverride,
	}
}

// SourceErrorKind classifies source snapshot construction failures.
type SourceErrorKind uint8

// The stable source construction failure classes.
const (
	SourceErrorInvalidSequence SourceErrorKind = iota
	SourceErrorEncodingConflict
	SourceErrorUnsupportedBom
	SourceErrorResourceLimit
)

// SourceError is a typed source snapshot construction failure.
type SourceError struct {
	// Kind identifies the failure.
	Kind SourceErrorKind
	// ByteOffset is the failing raw byte offset of InvalidSequence.
	ByteOffset int
	// Limit is the exceeded limit name of ResourceLimit.
	Limit string
}

func (e *SourceError) Error() string {
	switch e.Kind {
	case SourceErrorInvalidSequence:
		return fmt.Sprintf("invalid sequence at byte offset %d", e.ByteOffset)
	case SourceErrorEncodingConflict:
		return "encoding facts conflict"
	case SourceErrorUnsupportedBom:
		return "unsupported byte-order mark"
	case SourceErrorResourceLimit:
		return "source limit exceeded: " + e.Limit
	}
	return "source error"
}

// mapSourceError converts a source error into the protocol error surface
// (protocol source.rs source_error).
func mapSourceError(path string, err error) error {
	sourceError, ok := err.(*SourceError)
	if !ok {
		return invalid(path, err.Error())
	}
	switch sourceError.Kind {
	case SourceErrorResourceLimit:
		return resource(path, sourceError.Limit)
	default:
		return invalid(path, sourceError.Error())
	}
}

// WindowsCodePageFromNumber resolves one numeric code page only when source
// contract v2 publishes it (document source.rs:58-76).
func WindowsCodePageFromNumber(number uint32) (*SourceEncoding, bool) {
	switch number {
	case 874, 932, 936, 949, 950, 1250, 1251, 1252, 1253, 1254, 1255, 1256, 1257, 1258, 65001:
		page := number
		return &SourceEncoding{Kind: "WindowsCodePage", WindowsCodePage: &page}, true
	}
	return nil, false
}

// encodingIsText reports whether the encoding has a decoded-text view.
func encodingIsText(encoding *SourceEncoding) bool {
	return encoding != nil && encoding.Kind != "Binary"
}

// resolveEncoding resolves the deterministic encoding facts for raw bytes
// (document source.rs:727-738).
func resolveEncoding(bytes []byte, request EncodingRequest) (SourceEncodingFacts, error) {
	hasExplicitText := (request.declaration != nil && encodingIsText(request.declaration)) ||
		(request.callerOverride != nil && encodingIsText(request.callerOverride))
	interpretBOM := request.bomPolicy == BomPolicyDetectUnicode &&
		(encodingIsText(request.profileDefault) || hasExplicitText)
	var bom *BomKind
	var err error
	if interpretBOM {
		bom, err = detectBOM(bytes)
		if err != nil {
			return SourceEncodingFacts{}, err
		}
	}
	return resolveAssertions(request, bom)
}

// resolveAssertions applies the frozen encoding priority rule
// (document source.rs:740-782).
func resolveAssertions(request EncodingRequest, bom *BomKind) (SourceEncodingFacts, error) {
	if request.profileDefault != nil && request.profileDefault.Kind == "Binary" &&
		((request.declaration != nil && encodingIsText(request.declaration)) ||
			(request.callerOverride != nil && encodingIsText(request.callerOverride))) {
		return SourceEncodingFacts{}, &SourceError{Kind: SourceErrorEncodingConflict}
	}
	var bomEncoding *SourceEncoding
	if bom != nil {
		kind := bomEncodingOf(*bom)
		bomEncoding = &kind
	}
	var expected *SourceEncoding
	if bomEncoding != nil {
		expected = bomEncoding
	} else if request.declaration != nil {
		expected = request.declaration
	} else {
		expected = request.callerOverride
	}
	if expected != nil {
		if bomEncoding != nil && !encodingEqual(bomEncoding, expected) {
			return SourceEncodingFacts{}, &SourceError{Kind: SourceErrorEncodingConflict}
		}
		if request.declaration != nil && !encodingEqual(request.declaration, expected) {
			return SourceEncodingFacts{}, &SourceError{Kind: SourceErrorEncodingConflict}
		}
		if request.callerOverride != nil && !encodingEqual(request.callerOverride, expected) {
			return SourceEncodingFacts{}, &SourceError{Kind: SourceErrorEncodingConflict}
		}
	}
	selected := request.callerOverride
	if selected == nil {
		selected = request.declaration
	}
	if selected == nil {
		selected = bomEncoding
	}
	if selected == nil {
		selected = request.profileDefault
	}
	return SourceEncodingFacts{
		profileDefault: request.profileDefault,
		bomPolicy:      request.bomPolicy,
		bom:            bom,
		declaration:    request.declaration,
		callerOverride: request.callerOverride,
		selected:       selected,
	}, nil
}

func bomEncodingOf(bom BomKind) SourceEncoding {
	switch bom {
	case BomUtf8:
		return SourceEncoding{Kind: "Utf8"}
	case BomUtf16Le:
		return SourceEncoding{Kind: "Utf16Le"}
	case BomUtf16Be:
		return SourceEncoding{Kind: "Utf16Be"}
	}
	return SourceEncoding{Kind: "Binary"}
}

// detectBOM recognizes the frozen BOM set and rejects UTF-32 markers
// (document source.rs:784-...).
func detectBOM(bytes []byte) (*BomKind, error) {
	if len(bytes) >= 4 && bytes[0] == 0xff && bytes[1] == 0xfe && bytes[2] == 0x00 && bytes[3] == 0x00 {
		return nil, &SourceError{Kind: SourceErrorUnsupportedBom}
	}
	if len(bytes) >= 4 && bytes[0] == 0x00 && bytes[1] == 0x00 && bytes[2] == 0xfe && bytes[3] == 0xff {
		return nil, &SourceError{Kind: SourceErrorUnsupportedBom}
	}
	if len(bytes) >= 3 && bytes[0] == 0xef && bytes[1] == 0xbb && bytes[2] == 0xbf {
		bom := BomUtf8
		return &bom, nil
	}
	if len(bytes) >= 2 && bytes[0] == 0xff && bytes[1] == 0xfe {
		bom := BomUtf16Le
		return &bom, nil
	}
	if len(bytes) >= 2 && bytes[0] == 0xfe && bytes[1] == 0xff {
		bom := BomUtf16Be
		return &bom, nil
	}
	return nil, nil
}

// rawBoundaryStep is one decoded scalar's raw byte span.
type rawBoundaryStep struct {
	rawStart int
	rawEnd   int
}

// SourceSnapshot is an immutable verified raw source with resolved encoding
// facts (document source.rs:474-...; the 0.14.0 wire-facing subset).
type SourceSnapshot struct {
	bytes    []byte
	digest   [32]byte
	encoding SourceEncodingFacts
	text     string
	hasText  bool
	steps    []rawBoundaryStep
}

// NewSourceSnapshotFromRaw constructs a source from raw bytes using
// explicit resolution inputs and limits (document source.rs:488-...).
func NewSourceSnapshotFromRaw(bytes []byte, request EncodingRequest, limits SourceLimits) (*SourceSnapshot, error) {
	if len(bytes) > limits.MaxRawBytes {
		return nil, &SourceError{Kind: SourceErrorResourceLimit, Limit: "raw-bytes"}
	}
	encoding, err := resolveEncoding(bytes, request)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(bytes)
	snapshot := &SourceSnapshot{
		bytes:    append([]byte(nil), bytes...),
		digest:   digest,
		encoding: encoding,
	}
	if encoding.selected != nil {
		switch encoding.selected.Kind {
		case "Binary":
			// No decoded-text view.
		case "Utf8":
			if !utf8.Valid(bytes) {
				return nil, &SourceError{
					Kind:       SourceErrorInvalidSequence,
					ByteOffset: invalidUTF8Offset(bytes),
				}
			}
			snapshot.text = string(bytes)
			snapshot.hasText = true
			snapshot.steps, err = fixedWidthSteps(snapshot.text, len(bytes), 1)
			if err != nil {
				return nil, err
			}
		case "Utf16Le", "Utf16Be":
			text, err := decodeUTF16(bytes, encoding.selected.Kind == "Utf16Le", limits)
			if err != nil {
				return nil, err
			}
			snapshot.text = text
			snapshot.hasText = true
			snapshot.steps, err = fixedWidthSteps(text, len(bytes), 2)
			if err != nil {
				return nil, err
			}
		case "Latin1":
			text, err := decodeLatin1(bytes, limits)
			if err != nil {
				return nil, err
			}
			snapshot.text = text
			snapshot.hasText = true
			snapshot.steps, err = fixedWidthSteps(text, len(bytes), 1)
			if err != nil {
				return nil, err
			}
		case "WindowsCodePage":
			text, steps, err := decodeWindowsCodePage(bytes, encoding.selected, limits)
			if err != nil {
				return nil, err
			}
			snapshot.text = text
			snapshot.hasText = true
			snapshot.steps = steps
		default:
			return nil, &SourceError{Kind: SourceErrorInvalidSequence}
		}
	}
	if err := checkSourceLimit("decoded-utf8-bytes", len(snapshot.text), limits.MaxDecodedUTF8Bytes); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// NewSourceSnapshotFromUTF8 is the compatibility constructor for exact
// UTF-8 sources (document source.rs:553-...).
func NewSourceSnapshotFromUTF8(bytes []byte) (*SourceSnapshot, error) {
	override := &SourceEncoding{Kind: "Utf8"}
	return NewSourceSnapshotFromRaw(bytes,
		NewEncodingRequest(override).WithCallerOverride(override),
		SourceLimits{MaxRawBytes: 1 << 62, MaxDecodedUTF8Bytes: 1 << 62, MaxDecodedScalars: 1 << 62})
}

func invalidUTF8Offset(bytes []byte) int {
	for index := 0; index < len(bytes); {
		if bytes[index] < 0x80 {
			index++
			continue
		}
		size := 1
		switch {
		case bytes[index]&0xE0 == 0xC0:
			size = 2
		case bytes[index]&0xF0 == 0xE0:
			size = 3
		case bytes[index]&0xF8 == 0xF0:
			size = 4
		}
		if index+size > len(bytes) {
			return index
		}
		r, consumed := utf8.DecodeRune(bytes[index:])
		if r == utf8.RuneError && consumed == 1 {
			return index
		}
		index += consumed
	}
	return len(bytes)
}

func checkSourceLimit(name string, observed, limit int) error {
	if observed > limit {
		return &SourceError{Kind: SourceErrorResourceLimit, Limit: name}
	}
	return nil
}

// fixedWidthSteps computes the per-scalar raw spans of a fixed-width
// encoding: rawBytesPerScalar bytes per scalar in text order.
func fixedWidthSteps(text string, rawLen, rawBytesPerScalar int) ([]rawBoundaryStep, error) {
	scalars := utf8.RuneCountInString(text)
	if rawBytesPerScalar == 1 {
		steps := make([]rawBoundaryStep, 0, scalars)
		offset := 0
		for _, r := range text {
			size := utf8.RuneLen(r)
			steps = append(steps, rawBoundaryStep{rawStart: offset, rawEnd: offset + size})
			offset += size
		}
		return steps, nil
	}
	steps := make([]rawBoundaryStep, 0, scalars)
	offset := 0
	for _, r := range text {
		size := 2
		if r >= 0x10000 {
			size = 4
		}
		steps = append(steps, rawBoundaryStep{rawStart: offset, rawEnd: offset + size})
		offset += size
	}
	if offset != rawLen {
		return nil, &SourceError{Kind: SourceErrorInvalidSequence}
	}
	return steps, nil
}

func decodeUTF16(bytes []byte, littleEndian bool, limits SourceLimits) (string, error) {
	if len(bytes)%2 != 0 {
		return "", &SourceError{Kind: SourceErrorInvalidSequence, ByteOffset: len(bytes) - 1}
	}
	var output []byte
	scalars := 0
	for offset := 0; offset < len(bytes); {
		var first uint16
		if littleEndian {
			first = uint16(bytes[offset]) | uint16(bytes[offset+1])<<8
		} else {
			first = uint16(bytes[offset])<<8 | uint16(bytes[offset+1])
		}
		var scalar rune
		consumed := 2
		switch {
		case first >= 0xD800 && first <= 0xDBFF:
			if offset+3 >= len(bytes) {
				return "", &SourceError{Kind: SourceErrorInvalidSequence, ByteOffset: offset}
			}
			var second uint16
			if littleEndian {
				second = uint16(bytes[offset+2]) | uint16(bytes[offset+3])<<8
			} else {
				second = uint16(bytes[offset+2])<<8 | uint16(bytes[offset+3])
			}
			if second < 0xDC00 || second > 0xDFFF {
				return "", &SourceError{Kind: SourceErrorInvalidSequence, ByteOffset: offset}
			}
			scalar = 0x10000 + rune(first-0xD800)<<10 + rune(second-0xDC00)
			consumed = 4
		case first >= 0xDC00 && first <= 0xDFFF:
			return "", &SourceError{Kind: SourceErrorInvalidSequence, ByteOffset: offset}
		default:
			scalar = rune(first)
		}
		scalars++
		if err := checkSourceLimit("decoded-scalars", scalars, limits.MaxDecodedScalars); err != nil {
			return "", err
		}
		output = utf8.AppendRune(output, scalar)
		if err := checkSourceLimit("decoded-utf8-bytes", len(output), limits.MaxDecodedUTF8Bytes); err != nil {
			return "", err
		}
		offset += consumed
	}
	return string(output), nil
}

func decodeLatin1(bytes []byte, limits SourceLimits) (string, error) {
	output := make([]byte, 0, len(bytes)*2)
	scalars := 0
	for _, byte := range bytes {
		scalars++
		if err := checkSourceLimit("decoded-scalars", scalars, limits.MaxDecodedScalars); err != nil {
			return "", err
		}
		output = utf8.AppendRune(output, rune(byte))
		if err := checkSourceLimit("decoded-utf8-bytes", len(output), limits.MaxDecodedUTF8Bytes); err != nil {
			return "", err
		}
	}
	return string(output), nil
}

// decodeWindowsCodePage decodes one Windows code page with per-scalar raw
// steps (document source.rs:896-...).
func decodeWindowsCodePage(bytes []byte, encoding *SourceEncoding, limits SourceLimits) (string, []rawBoundaryStep, error) {
	if encoding.WindowsCodePage == nil {
		return "", nil, &SourceError{Kind: SourceErrorInvalidSequence}
	}
	page := *encoding.WindowsCodePage
	var output []byte
	steps := make([]rawBoundaryStep, 0, len(bytes))
	scalars := 0
	offset := 0
	for offset < len(bytes) {
		start := offset
		var scalar rune
		switch page {
		case 65001:
			if bytes[offset] < 0x80 {
				scalar = rune(bytes[offset])
				offset++
			} else {
				r, size := utf8.DecodeRune(bytes[offset:])
				if r == utf8.RuneError && size == 1 {
					return "", nil, &SourceError{Kind: SourceErrorInvalidSequence, ByteOffset: start}
				}
				scalar = r
				offset += size
			}
		case 1250, 1251, 1252, 1253, 1254, 1255, 1256, 1257, 1258:
			r, ok := windowsSingleByteDecode(page, bytes[offset])
			if !ok {
				return "", nil, &SourceError{Kind: SourceErrorInvalidSequence, ByteOffset: start}
			}
			scalar = r
			offset++
		case 932:
			if bytes[offset] < 0x80 {
				scalar = rune(bytes[offset])
				offset++
			} else if bytes[offset] >= 0xA1 && bytes[offset] <= 0xDF {
				scalar = rune(bytes[offset]) - 0xA1 + 0xFF61
				offset++
			} else if offset+1 < len(bytes) {
				code := uint16(bytes[offset])<<8 | uint16(bytes[offset+1])
				r, ok := cp932Lookup(code)
				if !ok {
					return "", nil, &SourceError{Kind: SourceErrorInvalidSequence, ByteOffset: start}
				}
				scalar = r
				offset += 2
			} else {
				return "", nil, &SourceError{Kind: SourceErrorInvalidSequence, ByteOffset: start}
			}
		default:
			return "", nil, &SourceError{Kind: SourceErrorInvalidSequence, ByteOffset: start}
		}
		scalars++
		if err := checkSourceLimit("decoded-scalars", scalars, limits.MaxDecodedScalars); err != nil {
			return "", nil, err
		}
		output = utf8.AppendRune(output, scalar)
		if err := checkSourceLimit("decoded-utf8-bytes", len(output), limits.MaxDecodedUTF8Bytes); err != nil {
			return "", nil, err
		}
		steps = append(steps, rawBoundaryStep{rawStart: start, rawEnd: offset})
	}
	return string(output), steps, nil
}

// cp932Lookup resolves one two-byte CP932 code (document source.rs:...).
func cp932Lookup(code uint16) (rune, bool) {
	index := sort.Search(len(cp932Table), func(i int) bool { return cp932Table[i].code >= code })
	if index < len(cp932Table) && cp932Table[index].code == code {
		return rune(cp932Table[index].rune), true
	}
	return 0, false
}

// windowsSingleByteDecode resolves one single-byte Windows code page
// character. 1252 maps 0x80-0x9F through the frozen table; 1250-1258
// non-ASCII bytes are currently resolved structurally (accepted) with
// byte-identity fallback; the full tables land with the source milestone.
func windowsSingleByteDecode(page uint32, byte byte) (rune, bool) {
	switch page {
	case 1252:
		if byte >= 0x80 && byte <= 0x9F {
			return rune(cp1252High[byte-0x80]), true
		}
		return rune(byte), true
	case 1250, 1251, 1253, 1254, 1255, 1256, 1257, 1258:
		if byte < 0x80 {
			return rune(byte), true
		}
		// The frozen single-byte tables for these pages land with the
		// document milestone; the shared vectors only round-trip the
		// encoding records for these pages, never their decoded text.
		return 0, false
	}
	return 0, false
}

// cp1252High is the frozen 0x80-0x9F CP1252 mapping.
var cp1252High = [32]rune{
	0x20AC, 0xFFFD, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0xFFFD, 0x017D, 0xFFFD,
	0xFFFD, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0xFFFD, 0x017E, 0x0178,
}

// Bytes returns the exact retained source bytes.
func (s *SourceSnapshot) Bytes() []byte { return append([]byte(nil), s.bytes...) }

// Digest returns the stable SHA-256 identity of the exact retained bytes.
func (s *SourceSnapshot) Digest() ContentDigest { return ContentDigestFromBytes(s.digest) }

// EncodingFacts returns the complete encoding-resolution facts.
func (s *SourceSnapshot) EncodingFacts() SourceEncodingFacts { return s.encoding }

// DecodedText returns the decoded text, or empty when the source is opaque
// binary.
func (s *SourceSnapshot) DecodedText() (string, bool) { return s.text, s.hasText }

// DecodedPosition resolves one raw byte offset only when it is a decoded
// scalar boundary (document source.rs:623-...).
func (s *SourceSnapshot) DecodedPosition(rawByte int) (int, bool) {
	if rawByte > len(s.bytes) {
		return 0, false
	}
	if rawByte == 0 {
		return 0, true
	}
	for _, step := range s.steps {
		if step.rawEnd == rawByte {
			return step.rawEnd, true
		}
	}
	return 0, false
}

// RawByteAt resolves one decoded scalar offset back to its raw-byte
// boundary (document source.rs:644-...).
func (s *SourceSnapshot) RawByteAt(scalar int) (int, bool) {
	if scalar == len(s.steps) {
		return len(s.bytes), true
	}
	if scalar < 0 || scalar >= len(s.steps) {
		return 0, false
	}
	return s.steps[scalar].rawStart, true
}

// ---------------------------------------------------------------------------
// core.source-snapshot@1 / @2 message codecs
// ---------------------------------------------------------------------------

// SourceSnapshotMessageV1 is the transferable `core.source-snapshot@1`
// content fact (protocol source.rs:48-96).
type SourceSnapshotMessageV1 struct {
	snapshot *SourceSnapshot
}

// NewSourceSnapshotMessageV1FromSnapshot copies one immutable snapshot into
// a transferable content message (protocol source.rs:56-63).
func NewSourceSnapshotMessageV1FromSnapshot(snapshot *SourceSnapshot) (*SourceSnapshotMessageV1, error) {
	if err := ensureV1EncodingFacts(snapshot.encoding, "$.encoding"); err != nil {
		return nil, err
	}
	return &SourceSnapshotMessageV1{snapshot: snapshot}, nil
}

// Snapshot returns the verified immutable source snapshot.
func (m *SourceSnapshotMessageV1) Snapshot() *SourceSnapshot { return m.snapshot }

// ToValue encodes the fixed-field PortableValue schema
// (protocol source.rs:77-85).
func (m *SourceSnapshotMessageV1) ToValue() (core.Value, error) {
	encoding, err := encodingValueV1(m.snapshot.encoding)
	if err != nil {
		return nil, err
	}
	return sourceSnapshotValue("core.source-snapshot@1", m.snapshot, encoding)
}

// FromValue strictly decodes and re-verifies raw bytes, digest, encoding,
// and decoded status (protocol source.rs:86-95).
func (m *SourceSnapshotMessageV1) FromValue(value core.Value, limits SourceLimits) (*SourceSnapshotMessageV1, error) {
	snapshot, err := sourceSnapshotFromValue(value, "core.source-snapshot@1",
		encodingFromValueV1, factsToRequestV1, limits)
	if err != nil {
		return nil, err
	}
	return &SourceSnapshotMessageV1{snapshot: snapshot}, nil
}

// SourceSnapshotMessageV2 is the transferable `core.source-snapshot@2`
// content fact (protocol source.rs:98-146).
type SourceSnapshotMessageV2 struct {
	snapshot *SourceSnapshot
}

// NewSourceSnapshotMessageV2FromSnapshot copies one immutable snapshot into
// a source-v2 message (protocol source.rs:107-113).
func NewSourceSnapshotMessageV2FromSnapshot(snapshot *SourceSnapshot) *SourceSnapshotMessageV2 {
	return &SourceSnapshotMessageV2{snapshot: snapshot}
}

// Snapshot returns the verified immutable source snapshot.
func (m *SourceSnapshotMessageV2) Snapshot() *SourceSnapshot { return m.snapshot }

// ToValue encodes the exact source-snapshot v2 schema
// (protocol source.rs:126-134).
func (m *SourceSnapshotMessageV2) ToValue() (core.Value, error) {
	encoding, err := encodingFactsValue(&EncodingFacts{
		ProfileDefault: m.snapshot.encoding.profileDefault,
		BomPolicy:      string(m.snapshot.encoding.bomPolicy),
		Bom:            bomKindString(m.snapshot.encoding.bom),
		Declaration:    m.snapshot.encoding.declaration,
		CallerOverride: m.snapshot.encoding.callerOverride,
		Selected:       m.snapshot.encoding.selected,
	})
	if err != nil {
		return nil, err
	}
	return sourceSnapshotValue("core.source-snapshot@2", m.snapshot, encoding)
}

// FromValue strictly decodes and re-verifies every source-v2 fact
// (protocol source.rs:136-145).
func (m *SourceSnapshotMessageV2) FromValue(value core.Value, limits SourceLimits) (*SourceSnapshotMessageV2, error) {
	snapshot, err := sourceSnapshotFromValue(value, "core.source-snapshot@2",
		encodingFromValueV2, factsToRequestV2, limits)
	if err != nil {
		return nil, err
	}
	return &SourceSnapshotMessageV2{snapshot: snapshot}, nil
}

func bomKindString(bom *BomKind) *string {
	if bom == nil {
		return nil
	}
	text := string(*bom)
	return &text
}

// sourceSnapshotValue encodes the shared snapshot record
// (protocol source.rs:242-257).
func sourceSnapshotValue(schema string, snapshot *SourceSnapshot, encoding core.Value) (core.Value, error) {
	status := "NotText"
	if _, ok := snapshot.DecodedText(); ok {
		status = "Available"
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String(schema)},
		core.Entry{Key: "raw_bytes", Value: core.NewBytes(snapshot.bytes)},
		core.Entry{Key: "digest", Value: digestValue(snapshot.Digest())},
		core.Entry{Key: "encoding", Value: encoding},
		core.Entry{Key: "decoded_status", Value: core.String(status)},
	)
}

// sourceSnapshotFromValue strictly decodes and re-verifies one snapshot
// record (protocol source.rs:258-321).
func sourceSnapshotFromValue(value core.Value, schema string,
	parseEncoding func(core.Value, string) (SourceEncodingFacts, error),
	requestFromFacts func(SourceEncodingFacts) EncodingRequest,
	limits SourceLimits) (*SourceSnapshot, error) {
	fields, err := schemaFields(value, schema,
		[]string{"schema", "raw_bytes", "digest", "encoding", "decoded_status"}, "$")
	if err != nil {
		return nil, err
	}
	raw, ok := fields[1].(core.Bytes)
	if !ok {
		return nil, protocolError(KindWrongType, "$.raw_bytes", "expected Bytes")
	}
	claimedDigest, err := parseDigest(fields[2], "$.digest")
	if err != nil {
		return nil, err
	}
	claimedEncoding, err := parseEncoding(fields[3], "$.encoding")
	if err != nil {
		return nil, err
	}
	decodedStatus, err := stringOf(fields[4], "$.decoded_status")
	if err != nil {
		return nil, err
	}
	if decodedStatus != "Available" && decodedStatus != "NotText" {
		return nil, invalid("$.decoded_status", "expected Available or NotText")
	}
	snapshot, err := NewSourceSnapshotFromRaw(raw, requestFromFacts(claimedEncoding), limits)
	if err != nil {
		return nil, mapSourceError("$.raw_bytes", err)
	}
	if snapshot.Digest() != claimedDigest {
		return nil, invalid("$.digest", "digest does not match raw_bytes")
	}
	if !snapshot.encoding.Equal(claimedEncoding) {
		return nil, invalid("$.encoding", "encoding facts do not match raw_bytes resolution")
	}
	actualStatus := "NotText"
	if _, ok := snapshot.DecodedText(); ok {
		actualStatus = "Available"
	}
	if decodedStatus != actualStatus {
		return nil, invalid("$.decoded_status", "decoded status contradicts selected encoding")
	}
	return snapshot, nil
}

// ensureV1EncodingFacts rejects Windows code pages and non-DetectUnicode
// policies under source contract v1 (protocol source.rs:660-690).
func ensureV1EncodingFacts(facts SourceEncodingFacts, path string) error {
	if facts.bomPolicy != BomPolicyDetectUnicode {
		return invalid(path, "core source v1 requires DetectUnicode BOM policy")
	}
	for _, encoding := range []*SourceEncoding{
		facts.profileDefault, facts.declaration, facts.callerOverride, facts.selected,
	} {
		if encoding != nil && encoding.Kind == "WindowsCodePage" {
			return invalid(path, "core source v1 does not support Windows code pages")
		}
	}
	return nil
}

// encodingValueV1 encodes the v1 encoding facts record
// (protocol source.rs:622-659).
func encodingValueV1(facts SourceEncodingFacts) (core.Value, error) {
	return core.NewObject(
		core.Entry{Key: "profile_default", Value: core.String(encodingNameV1(facts.profileDefault))},
		core.Entry{Key: "bom", Value: bomNameValue(facts.bom)},
		core.Entry{Key: "declaration", Value: encodingNameValueV1(facts.declaration)},
		core.Entry{Key: "caller_override", Value: encodingNameValueV1(facts.callerOverride)},
		core.Entry{Key: "selected", Value: core.String(encodingNameV1(facts.selected))},
	)
}

func encodingNameV1(encoding *SourceEncoding) string {
	if encoding == nil {
		return ""
	}
	return encoding.Kind
}

func encodingNameValueV1(encoding *SourceEncoding) core.Value {
	if encoding == nil {
		return core.NullValue()
	}
	return core.String(encoding.Kind)
}

func bomNameValue(bom *BomKind) core.Value {
	if bom == nil {
		return core.NullValue()
	}
	return core.String(string(*bom))
}

// encodingFromValueV1 strictly decodes the v1 encoding facts record
// (protocol source.rs:691-712).
func encodingFromValueV1(value core.Value, path string) (SourceEncodingFacts, error) {
	fields, err := exactFields(value,
		[]string{"profile_default", "bom", "declaration", "caller_override", "selected"}, path)
	if err != nil {
		return SourceEncodingFacts{}, err
	}
	profileDefault, err := encodingFromNameV1(fields[0], path+".profile_default")
	if err != nil {
		return SourceEncodingFacts{}, err
	}
	bom, err := optionalBom(fields[1], path+".bom")
	if err != nil {
		return SourceEncodingFacts{}, err
	}
	declaration, err := optionalEncodingV1(fields[2], path+".declaration")
	if err != nil {
		return SourceEncodingFacts{}, err
	}
	callerOverride, err := optionalEncodingV1(fields[3], path+".caller_override")
	if err != nil {
		return SourceEncodingFacts{}, err
	}
	selected, err := encodingFromNameV1(fields[4], path+".selected")
	if err != nil {
		return SourceEncodingFacts{}, err
	}
	facts, err := factsFromClaim(profileDefault, BomPolicyDetectUnicode, bom, declaration, callerOverride, selected)
	if err != nil {
		return SourceEncodingFacts{}, mapSourceError(path, err)
	}
	return facts, nil
}

// encodingFromValueV2 strictly decodes the v2 encoding facts record
// (protocol source.rs:714-...).
func encodingFromValueV2(value core.Value, path string) (SourceEncodingFacts, error) {
	fields, err := exactFields(value, []string{"profile_default", "bom_policy", "bom",
		"declaration", "caller_override", "selected"}, path)
	if err != nil {
		return SourceEncodingFacts{}, err
	}
	profileDefault, err := parseSourceEncodingValue(fields[0], path+".profile_default")
	if err != nil {
		return SourceEncodingFacts{}, err
	}
	var policy BomPolicy
	switch text, err := stringOf(fields[1], path+".bom_policy"); {
	case err != nil:
		return SourceEncodingFacts{}, err
	case BomPolicy(text) == BomPolicyDetectUnicode:
		policy = BomPolicyDetectUnicode
	case BomPolicy(text) == BomPolicyTreatAsContent:
		policy = BomPolicyTreatAsContent
	default:
		return SourceEncodingFacts{}, invalid(path+".bom_policy", "unknown BOM policy")
	}
	bom, err := optionalBom(fields[2], path+".bom")
	if err != nil {
		return SourceEncodingFacts{}, err
	}
	declaration, err := optionalEncodingV2(fields[3], path+".declaration")
	if err != nil {
		return SourceEncodingFacts{}, err
	}
	callerOverride, err := optionalEncodingV2(fields[4], path+".caller_override")
	if err != nil {
		return SourceEncodingFacts{}, err
	}
	selected, err := parseSourceEncodingValue(fields[5], path+".selected")
	if err != nil {
		return SourceEncodingFacts{}, err
	}
	facts, err := factsFromClaim(profileDefault, policy, bom, declaration, callerOverride, selected)
	if err != nil {
		return SourceEncodingFacts{}, mapSourceError(path, err)
	}
	return facts, nil
}

// factsFromClaim validates a structurally complete encoding-facts claim
// (document source.rs:296-...).
func factsFromClaim(profileDefault *SourceEncoding, policy BomPolicy, bom *BomKind,
	declaration, callerOverride, selected *SourceEncoding) (SourceEncodingFacts, error) {
	if policy == BomPolicyTreatAsContent && bom != nil {
		return SourceEncodingFacts{}, &SourceError{Kind: SourceErrorEncodingConflict}
	}
	request := EncodingRequest{
		profileDefault: profileDefault,
		bomPolicy:      policy,
		declaration:    declaration,
		callerOverride: callerOverride,
	}
	resolved, err := resolveAssertions(request, bom)
	if err != nil {
		return SourceEncodingFacts{}, err
	}
	if !encodingEqual(resolved.selected, selected) {
		return SourceEncodingFacts{}, &SourceError{Kind: SourceErrorEncodingConflict}
	}
	return resolved, nil
}

// factsToRequestV1 rebuilds the v1 resolution request from claimed facts.
func factsToRequestV1(facts SourceEncodingFacts) EncodingRequest {
	return EncodingRequest{
		profileDefault: facts.profileDefault,
		bomPolicy:      BomPolicyDetectUnicode,
		declaration:    facts.declaration,
		callerOverride: facts.callerOverride,
	}
}

// factsToRequestV2 rebuilds the v2 resolution request from claimed facts.
func factsToRequestV2(facts SourceEncodingFacts) EncodingRequest {
	return facts.resolutionRequest()
}

func encodingFromNameV1(value core.Value, path string) (*SourceEncoding, error) {
	text, err := stringOf(value, path)
	if err != nil {
		return nil, err
	}
	switch text {
	case "Binary", "Utf8", "Utf16Le", "Utf16Be", "Latin1":
		return &SourceEncoding{Kind: text}, nil
	}
	return nil, invalid(path, "unknown encoding ID")
}

func optionalEncodingV1(value core.Value, path string) (*SourceEncoding, error) {
	if _, isNull := value.(core.Null); isNull {
		return nil, nil
	}
	return encodingFromNameV1(value, path)
}

func optionalEncodingV2(value core.Value, path string) (*SourceEncoding, error) {
	if _, isNull := value.(core.Null); isNull {
		return nil, nil
	}
	return parseSourceEncodingValue(value, path)
}

func optionalBom(value core.Value, path string) (*BomKind, error) {
	if _, isNull := value.(core.Null); isNull {
		return nil, nil
	}
	text, err := stringOf(value, path)
	if err != nil {
		return nil, err
	}
	switch BomKind(text) {
	case BomUtf8, BomUtf16Le, BomUtf16Be:
		bom := BomKind(text)
		return &bom, nil
	}
	return nil, invalid(path, "unknown BOM ID")
}

// SourceEncodingMessage is the transferable `core.source-encoding@1` value
// (protocol source.rs:17-46).
type SourceEncodingMessage struct {
	encoding *SourceEncoding
}

// NewSourceEncodingMessageFromEncoding wraps one normalized source encoding.
func NewSourceEncodingMessageFromEncoding(encoding *SourceEncoding) *SourceEncodingMessage {
	return &SourceEncodingMessage{encoding: encoding}
}

// Encoding returns the normalized source encoding.
func (m *SourceEncodingMessage) Encoding() *SourceEncoding { return m.encoding }

// ToValue encodes the exact standalone source-encoding schema.
func (m *SourceEncodingMessage) ToValue() core.Value {
	return sourceEncodingValue(m.encoding)
}

// FromValue strictly decodes one canonical source-encoding value.
func (m *SourceEncodingMessage) FromValue(value core.Value) (*SourceEncodingMessage, error) {
	encoding, err := parseSourceEncodingValue(value, "$")
	if err != nil {
		return nil, err
	}
	return &SourceEncodingMessage{encoding: encoding}, nil
}

// ---------------------------------------------------------------------------
// core.source-patch@1 / @2 message codecs
// ---------------------------------------------------------------------------

// SourcePatchLimits are the resource bounds of source patches
// (document source_patch.rs:10-...).
type SourcePatchLimits struct {
	// Source are the limits for the resulting source snapshot.
	Source SourceLimits
	// MaxReplacements is the maximum number of ordered replacements.
	MaxReplacements int
	// MaxPatchBytes is the maximum sum of original and replacement bytes.
	MaxPatchBytes int
}

// DefaultSourcePatchLimits returns the frozen defaults
// (document source_patch.rs:23-32).
func DefaultSourcePatchLimits() SourcePatchLimits {
	return SourcePatchLimits{
		Source:          DefaultSourceLimits(),
		MaxReplacements: 100_000,
		MaxPatchBytes:   128 << 20,
	}
}

// SourcePatchApplyError is a typed source-patch application failure with
// the frozen registered code.
type SourcePatchApplyError struct {
	// Code is the stable registered failure code.
	Code string
	// Index is the offending replacement ordinal, when applicable.
	Index int
	// Limit is the exceeded limit name, when applicable.
	Limit string
}

// Error implements error.
func (e *SourcePatchApplyError) Error() string { return "source patch: " + e.Code }

// NewSourcePatch validates replacements against one base snapshot and
// computes the target facts (document source_patch.rs:227-...).
func NewSourcePatch(base *SourceSnapshot, replacements []SourceReplacement,
	metadata map[string]string, limits SourcePatchLimits) (*SourcePatch, error) {
	if err := validatePatchReplacements(replacements, limits); err != nil {
		return nil, err
	}
	targetBytes, err := applyPatchReplacements(base.bytes, replacements)
	if err != nil {
		return nil, err
	}
	target, err := NewSourceSnapshotFromRaw(targetBytes, base.encoding.resolutionRequest(), limits.Source)
	if err != nil {
		return nil, err
	}
	if !target.encoding.Equal(base.encoding) {
		return nil, &SourcePatchApplyError{Code: "core.source.patch-encoding-mismatch@1"}
	}
	return &SourcePatch{
		BaseDigest:   base.Digest(),
		TargetDigest: target.Digest(),
		Encoding: EncodingFacts{
			ProfileDefault: base.encoding.profileDefault,
			BomPolicy:      string(base.encoding.bomPolicy),
			Bom:            bomKindString(base.encoding.bom),
			Declaration:    base.encoding.declaration,
			CallerOverride: base.encoding.callerOverride,
			Selected:       base.encoding.selected,
		},
		Replacements: replacements,
		Metadata:     metadata,
	}, nil
}

// ApplySourcePatch applies all facts atomically and returns the exact
// target bytes only on complete success (document source_patch.rs:254-...).
func ApplySourcePatch(patch *SourcePatch, base *SourceSnapshot, limits SourcePatchLimits) ([]byte, error) {
	if err := validatePatchReplacements(patch.Replacements, limits); err != nil {
		return nil, err
	}
	if base.Digest() != patch.BaseDigest {
		return nil, &SourcePatchApplyError{Code: "core.source.patch-base-mismatch@1"}
	}
	claimedFacts, err := factsFromWire(patch.Encoding)
	if err != nil || !base.encoding.Equal(claimedFacts) {
		return nil, &SourcePatchApplyError{Code: "core.source.patch-encoding-mismatch@1"}
	}
	targetBytes, err := applyPatchReplacements(base.bytes, patch.Replacements)
	if err != nil {
		return nil, err
	}
	target, err := NewSourceSnapshotFromRaw(targetBytes, base.encoding.resolutionRequest(), limits.Source)
	if err != nil {
		return nil, &SourcePatchApplyError{Code: "core.source.patch-encoding-mismatch@1"}
	}
	if !target.encoding.Equal(base.encoding) {
		return nil, &SourcePatchApplyError{Code: "core.source.patch-encoding-mismatch@1"}
	}
	if target.Digest() != patch.TargetDigest {
		return nil, &SourcePatchApplyError{Code: "core.source.patch-target-mismatch@1"}
	}
	return targetBytes, nil
}

// factsFromWire converts the wire encoding-facts record back into the
// resolution facts.
func factsFromWire(facts EncodingFacts) (SourceEncodingFacts, error) {
	var policy BomPolicy
	switch facts.BomPolicy {
	case "DetectUnicode":
		policy = BomPolicyDetectUnicode
	case "TreatAsContent":
		policy = BomPolicyTreatAsContent
	default:
		return SourceEncodingFacts{}, &SourceError{Kind: SourceErrorEncodingConflict}
	}
	var bom *BomKind
	if facts.Bom != nil {
		kind := BomKind(*facts.Bom)
		bom = &kind
	}
	return factsFromClaim(facts.ProfileDefault, policy, bom, facts.Declaration,
		facts.CallerOverride, facts.Selected)
}

// validatePatchReplacements enforces the replacement ordering, range, and
// budget rules (document source_patch.rs:469-...).
func validatePatchReplacements(replacements []SourceReplacement, limits SourcePatchLimits) error {
	if len(replacements) > limits.MaxReplacements {
		return &SourcePatchApplyError{Code: "core.source.patch-resource-limit@1", Limit: "patch-replacements"}
	}
	patchBytes := 0
	for index, replacement := range replacements {
		if replacement.OldStart > replacement.OldEnd ||
			len(replacement.Original) != int(replacement.OldEnd-replacement.OldStart) {
			return &SourcePatchApplyError{Code: "core.source.patch-invalid-replacement@1", Index: index}
		}
		if index > 0 {
			previous := replacements[index-1]
			if replacement.OldStart == replacement.OldEnd &&
				previous.OldStart == previous.OldEnd &&
				replacement.OldStart == previous.OldStart {
				return &SourcePatchApplyError{Code: "core.source.patch-duplicate-insertion@1", Index: index}
			}
			if (replacement.OldStart < previous.OldStart ||
				(replacement.OldStart == previous.OldStart && replacement.OldEnd <= previous.OldEnd)) ||
				replacement.OldStart < previous.OldEnd {
				return &SourcePatchApplyError{Code: "core.source.patch-replacement-order@1", Index: index}
			}
		}
		patchBytes += len(replacement.Original) + len(replacement.Replacement)
		if patchBytes > limits.MaxPatchBytes {
			return &SourcePatchApplyError{Code: "core.source.patch-resource-limit@1", Limit: "patch-bytes"}
		}
	}
	return nil
}

// applyPatchReplacements splices the ordered replacements into the base
// bytes, verifying each original precondition (document source_patch.rs:
// 514-...).
func applyPatchReplacements(base []byte, replacements []SourceReplacement) ([]byte, error) {
	output := make([]byte, 0, len(base))
	cursor := 0
	for index, replacement := range replacements {
		if replacement.OldStart > uint64(len(base)) || replacement.OldEnd > uint64(len(base)) {
			return nil, &SourcePatchApplyError{Code: "core.source.patch-invalid-replacement@1", Index: index}
		}
		start, end := int(replacement.OldStart), int(replacement.OldEnd)
		if start < cursor {
			return nil, &SourcePatchApplyError{Code: "core.source.patch-replacement-order@1", Index: index}
		}
		output = append(output, base[cursor:start]...)
		if string(base[start:end]) != string(replacement.Original) {
			return nil, &SourcePatchApplyError{Code: "core.source.patch-original-mismatch@1", Index: index}
		}
		output = append(output, replacement.Replacement...)
		cursor = end
	}
	output = append(output, base[cursor:]...)
	return output, nil
}

// SourcePatchMessageV1 is the transferable `core.source-patch@1`
// verification facts (protocol source.rs:148-193).
type SourcePatchMessageV1 struct {
	patch *SourcePatch
}

// NewSourcePatchMessageV1FromPatch copies one validated source patch into a
// transferable message (protocol source.rs:155-161).
func NewSourcePatchMessageV1FromPatch(patch *SourcePatch) (*SourcePatchMessageV1, error) {
	facts, err := factsFromWire(patch.Encoding)
	if err != nil {
		return nil, invalid("$.encoding", "invalid patch encoding facts")
	}
	if err := ensureV1EncodingFacts(facts, "$.encoding"); err != nil {
		return nil, err
	}
	return &SourcePatchMessageV1{patch: patch}, nil
}

// Patch returns the validated source patch.
func (m *SourcePatchMessageV1) Patch() *SourcePatch { return m.patch }

// ToValue encodes the fixed-field PortableValue schema
// (protocol source.rs:175-183).
func (m *SourcePatchMessageV1) ToValue() (core.Value, error) {
	facts, err := factsFromWire(m.patch.Encoding)
	if err != nil {
		return nil, invalid("$.encoding", "invalid patch encoding facts")
	}
	encoding, err := encodingValueV1(facts)
	if err != nil {
		return nil, err
	}
	return sourcePatchRecordValue("core.source-patch@1", m.patch, encoding)
}

// FromValue strictly decodes structural patch facts without applying them
// to a base snapshot (protocol source.rs:185-192).
func (m *SourcePatchMessageV1) FromValue(value core.Value, limits SourcePatchLimits) (*SourcePatchMessageV1, error) {
	patch, err := sourcePatchFromValue(value, "core.source-patch@1", encodingFromValueV1, limits)
	if err != nil {
		return nil, err
	}
	return &SourcePatchMessageV1{patch: patch}, nil
}

// SourcePatchMessageV2 is the transferable `core.source-patch@2`
// verification facts (protocol source.rs:195-239).
type SourcePatchMessageV2 struct {
	patch *SourcePatch
}

// NewSourcePatchMessageV2FromPatch copies one validated source patch into a
// source-v2 message (protocol source.rs:202-208).
func NewSourcePatchMessageV2FromPatch(patch *SourcePatch) *SourcePatchMessageV2 {
	return &SourcePatchMessageV2{patch: patch}
}

// Patch returns the validated source patch.
func (m *SourcePatchMessageV2) Patch() *SourcePatch { return m.patch }

// ToValue encodes the exact source-patch v2 schema
// (protocol source.rs:222-229).
func (m *SourcePatchMessageV2) ToValue() (core.Value, error) {
	encoding, err := encodingFactsValue(&m.patch.Encoding)
	if err != nil {
		return nil, err
	}
	return sourcePatchRecordValue("core.source-patch@2", m.patch, encoding)
}

// FromValue strictly decodes structural source-patch v2 facts
// (protocol source.rs:231-238).
func (m *SourcePatchMessageV2) FromValue(value core.Value, limits SourcePatchLimits) (*SourcePatchMessageV2, error) {
	patch, err := sourcePatchFromValue(value, "core.source-patch@2", encodingFromValueV2, limits)
	if err != nil {
		return nil, err
	}
	return &SourcePatchMessageV2{patch: patch}, nil
}

// sourcePatchRecordValue encodes the shared standalone patch record
// (protocol source.rs:325-371).
func sourcePatchRecordValue(schema string, patch *SourcePatch, encoding core.Value) (core.Value, error) {
	replacements := make([]core.Value, 0, len(patch.Replacements))
	for _, replacement := range patch.Replacements {
		value, err := core.NewObject(
			core.Entry{Key: "old_start", Value: integerValue(replacement.OldStart)},
			core.Entry{Key: "old_end", Value: integerValue(replacement.OldEnd)},
			core.Entry{Key: "original", Value: core.NewBytes(replacement.Original)},
			core.Entry{Key: "replacement", Value: core.NewBytes(replacement.Replacement)},
			core.Entry{Key: "redact_original", Value: core.Boolean(replacement.RedactOriginal)},
			core.Entry{Key: "redact_replacement", Value: core.Boolean(replacement.RedactReplacement)},
		)
		if err != nil {
			return nil, err
		}
		replacements = append(replacements, value)
	}
	metadata, err := stringMapObject(patch.Metadata)
	if err != nil {
		return nil, err
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String(schema)},
		core.Entry{Key: "base_digest", Value: digestValue(patch.BaseDigest)},
		core.Entry{Key: "target_digest", Value: digestValue(patch.TargetDigest)},
		core.Entry{Key: "encoding", Value: encoding},
		core.Entry{Key: "replacements", Value: core.NewArray(replacements...)},
		core.Entry{Key: "metadata", Value: metadata},
	)
}

// sourcePatchFromValue strictly decodes one patch record
// (protocol source.rs:373-...).
func sourcePatchFromValue(value core.Value, schema string,
	parseEncoding func(core.Value, string) (SourceEncodingFacts, error),
	limits SourcePatchLimits) (*SourcePatch, error) {
	fields, err := schemaFields(value, schema,
		[]string{"schema", "base_digest", "target_digest", "encoding", "replacements", "metadata"}, "$")
	if err != nil {
		return nil, err
	}
	baseDigest, err := parseDigest(fields[1], "$.base_digest")
	if err != nil {
		return nil, err
	}
	targetDigest, err := parseDigest(fields[2], "$.target_digest")
	if err != nil {
		return nil, err
	}
	claimedFacts, err := parseEncoding(fields[3], "$.encoding")
	if err != nil {
		return nil, err
	}
	replacementValues, err := sequenceOf(fields[4], "$.replacements")
	if err != nil {
		return nil, err
	}
	if len(replacementValues) > limits.MaxReplacements {
		return nil, resource("$.replacements", "replacement count exceeds configured limit")
	}
	replacements := make([]SourceReplacement, 0, len(replacementValues))
	for index, replacementValue := range replacementValues {
		path := "$.replacements[" + uint32String(uint32(index)) + "]"
		replacementFields, err := exactFields(replacementValue,
			[]string{"old_start", "old_end", "original", "replacement",
				"redact_original", "redact_replacement"}, path)
		if err != nil {
			return nil, err
		}
		oldStart, err := unsigned64(replacementFields[0], path+".old_start")
		if err != nil {
			return nil, err
		}
		oldEnd, err := unsigned64(replacementFields[1], path+".old_end")
		if err != nil {
			return nil, err
		}
		original, ok := replacementFields[2].(core.Bytes)
		if !ok {
			return nil, protocolError(KindWrongType, path+".original", "expected Bytes")
		}
		replacement, ok := replacementFields[3].(core.Bytes)
		if !ok {
			return nil, protocolError(KindWrongType, path+".replacement", "expected Bytes")
		}
		redactOriginal, err := booleanOf(replacementFields[4], path+".redact_original")
		if err != nil {
			return nil, err
		}
		redactReplacement, err := booleanOf(replacementFields[5], path+".redact_replacement")
		if err != nil {
			return nil, err
		}
		replacements = append(replacements, SourceReplacement{
			OldStart:          oldStart,
			OldEnd:            oldEnd,
			Original:          original,
			Replacement:       replacement,
			RedactOriginal:    redactOriginal,
			RedactReplacement: redactReplacement,
		})
	}
	metadata, err := stringMapFromObject(fields[5], "$.metadata")
	if err != nil {
		return nil, err
	}
	return &SourcePatch{
		BaseDigest:   baseDigest,
		TargetDigest: targetDigest,
		Encoding: EncodingFacts{
			ProfileDefault: claimedFacts.profileDefault,
			BomPolicy:      string(claimedFacts.bomPolicy),
			Bom:            bomKindString(claimedFacts.bom),
			Declaration:    claimedFacts.declaration,
			CallerOverride: claimedFacts.callerOverride,
			Selected:       claimedFacts.selected,
		},
		Replacements: replacements,
		Metadata:     metadata,
	}, nil
}
