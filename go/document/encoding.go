package document

import "fmt"

// WindowsCodePage is one deterministic Windows code page admitted by
// source contract v2 (document source.rs). Only the published
// numbers resolve; the host locale never participates.
type WindowsCodePage uint16

// WindowsCodePageFromNumber resolves one numeric code page only when
// source contract v2 publishes it.
func WindowsCodePageFromNumber(number uint16) (WindowsCodePage, bool) {
	switch number {
	case 874, 932, 936, 949, 950, 1250, 1251, 1252, 1253, 1254, 1255, 1256, 1257, 1258, 65001:
		return WindowsCodePage(number), true
	}
	return 0, false
}

// Number returns the canonical numeric code-page identity.
func (p WindowsCodePage) Number() uint16 { return uint16(p) }

// Name returns the stable wire identifier ("windows-1252").
func (p WindowsCodePage) Name() string { return "windows-" + fmt.Sprint(uint16(p)) }

// SourceEncodingKind identifies the closed set of source encodings
// (document source.rs).
type SourceEncodingKind uint8

// The closed source encoding kinds.
const (
	EncodingBinary SourceEncodingKind = iota
	EncodingUtf8
	EncodingUtf16Le
	EncodingUtf16Be
	EncodingLatin1
	EncodingWindowsCodePage
)

// SourceEncoding is one closed source encoding. Values are constructed by
// the encoding constructors; the set is Binary, Utf8, Utf16Le, Utf16Be,
// Latin1, and WindowsCodePage.
type SourceEncoding struct {
	kind     SourceEncodingKind
	codePage WindowsCodePage
}

// BinaryEncoding is the opaque-bytes encoding without a decoded-text view.
func BinaryEncoding() SourceEncoding { return SourceEncoding{kind: EncodingBinary} }

// Utf8Encoding is Unicode UTF-8.
func Utf8Encoding() SourceEncoding { return SourceEncoding{kind: EncodingUtf8} }

// Utf16LeEncoding is Unicode UTF-16 with little-endian code units.
func Utf16LeEncoding() SourceEncoding { return SourceEncoding{kind: EncodingUtf16Le} }

// Utf16BeEncoding is Unicode UTF-16 with big-endian code units.
func Utf16BeEncoding() SourceEncoding { return SourceEncoding{kind: EncodingUtf16Be} }

// Latin1Encoding is the ISO-8859-1 byte-to-scalar mapping.
func Latin1Encoding() SourceEncoding { return SourceEncoding{kind: EncodingLatin1} }

// WindowsCodePageEncoding is an explicit Windows code page; it is never
// resolved from the host locale.
func WindowsCodePageEncoding(page WindowsCodePage) SourceEncoding {
	return SourceEncoding{kind: EncodingWindowsCodePage, codePage: page}
}

// Kind returns the encoding kind.
func (e SourceEncoding) Kind() SourceEncodingKind { return e.kind }

// WindowsCodePage returns the code page of a WindowsCodePage encoding.
func (e SourceEncoding) WindowsCodePage() (WindowsCodePage, bool) {
	return e.codePage, e.kind == EncodingWindowsCodePage
}

// AsStr returns the stable wire identifier ("utf-8", "windows-1252").
func (e SourceEncoding) AsStr() string {
	switch e.kind {
	case EncodingBinary:
		return "binary"
	case EncodingUtf8:
		return "utf-8"
	case EncodingUtf16Le:
		return "utf-16le"
	case EncodingUtf16Be:
		return "utf-16be"
	case EncodingLatin1:
		return "latin-1"
	case EncodingWindowsCodePage:
		return e.codePage.Name()
	}
	return "unknown"
}

// IsText reports whether the encoding has a decoded-text view.
func (e SourceEncoding) IsText() bool { return e.kind != EncodingBinary }

// Equal reports whether two encodings are identical.
func (e SourceEncoding) Equal(other SourceEncoding) bool {
	return e.kind == other.kind && e.codePage == other.codePage
}

// BomPolicy selects whether leading marker-shaped bytes participate in
// Unicode BOM resolution (document source.rs).
type BomPolicy string

// The two frozen BOM interpretation policies.
const (
	// BomPolicyDetectUnicode detects UTF-8/UTF-16 BOMs using the frozen
	// source-v1 rule.
	BomPolicyDetectUnicode BomPolicy = "DetectUnicode"
	// BomPolicyTreatAsContent decodes all bytes as content under the
	// explicitly selected encoding.
	BomPolicyTreatAsContent BomPolicy = "TreatAsContent"
)

// BomKind is one recognized Unicode byte-order mark (document
// source.rs).
type BomKind string

// The three recognized BOMs.
const (
	BomUtf8    BomKind = "Utf8"
	BomUtf16Le BomKind = "Utf16Le"
	BomUtf16Be BomKind = "Utf16Be"
)

// Encoding returns the encoding asserted by this marker.
func (k BomKind) Encoding() SourceEncoding {
	switch k {
	case BomUtf8:
		return Utf8Encoding()
	case BomUtf16Le:
		return Utf16LeEncoding()
	case BomUtf16Be:
		return Utf16BeEncoding()
	}
	return BinaryEncoding()
}

// UnsupportedBomKind is one recognized but unsupported Unicode marker
// (document source.rs).
type UnsupportedBomKind string

// The two unsupported UTF-32 markers.
const (
	UnsupportedBomUtf32Le UnsupportedBomKind = "Utf32Le"
	UnsupportedBomUtf32Be UnsupportedBomKind = "Utf32Be"
)

// EncodingRequest carries the caller inputs to deterministic encoding
// resolution (document source.rs). The frozen priority rule
// resolves conflicts in the order caller override, declaration, BOM,
// profile default.
type EncodingRequest struct {
	profileDefault SourceEncoding
	bomPolicy      BomPolicy
	declaration    *SourceEncoding
	callerOverride *SourceEncoding
}

// NewEncodingRequest starts with the required profile default and no
// higher-priority facts.
func NewEncodingRequest(profileDefault SourceEncoding) EncodingRequest {
	return EncodingRequest{
		profileDefault: profileDefault,
		bomPolicy:      BomPolicyDetectUnicode,
	}
}

// BinaryEncodingRequest is the opaque-binary request.
func BinaryEncodingRequest() EncodingRequest {
	return NewEncodingRequest(BinaryEncoding())
}

// WithDeclaration adds a normalized declaration supplied by the format
// layer.
func (r EncodingRequest) WithDeclaration(declaration SourceEncoding) EncodingRequest {
	r.declaration = &declaration
	return r
}

// WithCallerOverride adds an explicit caller override.
func (r EncodingRequest) WithCallerOverride(override SourceEncoding) EncodingRequest {
	r.callerOverride = &override
	return r
}

// WithBomPolicy selects whether leading marker-shaped bytes are BOM
// evidence or content.
func (r EncodingRequest) WithBomPolicy(policy BomPolicy) EncodingRequest {
	r.bomPolicy = policy
	return r
}

// ProfileDefault returns the profile fallback.
func (r EncodingRequest) ProfileDefault() SourceEncoding { return r.profileDefault }

// BomPolicy returns the BOM interpretation policy.
func (r EncodingRequest) BomPolicy() BomPolicy { return r.bomPolicy }

// Declaration returns the normalized in-source declaration, when one
// exists.
func (r EncodingRequest) Declaration() *SourceEncoding { return r.declaration }

// CallerOverride returns the explicit caller choice, when one exists.
func (r EncodingRequest) CallerOverride() *SourceEncoding { return r.callerOverride }

// EncodingFacts is the complete, auditable result of encoding resolution
// (document source.rs).
type EncodingFacts struct {
	profileDefault SourceEncoding
	bomPolicy      BomPolicy
	bom            *BomKind
	declaration    *SourceEncoding
	callerOverride *SourceEncoding
	selected       SourceEncoding
}

// NewEncodingFactsFromClaim validates a structurally complete
// encoding-facts claim. It proves resolution consistency only; a source
// decoder must still verify that the claimed BOM is present in the raw
// bytes.
func NewEncodingFactsFromClaim(profileDefault SourceEncoding, bom *BomKind,
	declaration, callerOverride *SourceEncoding, selected SourceEncoding) (EncodingFacts, error) {
	return NewEncodingFactsFromClaimWithBomPolicy(profileDefault, BomPolicyDetectUnicode,
		bom, declaration, callerOverride, selected)
}

// NewEncodingFactsFromClaimWithBomPolicy validates a source-v2 claim
// including explicit BOM interpretation.
func NewEncodingFactsFromClaimWithBomPolicy(profileDefault SourceEncoding,
	bomPolicy BomPolicy, bom *BomKind, declaration, callerOverride *SourceEncoding,
	selected SourceEncoding) (EncodingFacts, error) {
	if bomPolicy == BomPolicyTreatAsContent && bom != nil {
		return EncodingFacts{}, encodingConflictError(bom, declaration, callerOverride)
	}
	request := EncodingRequest{
		profileDefault: profileDefault,
		bomPolicy:      bomPolicy,
		declaration:    declaration,
		callerOverride: callerOverride,
	}
	resolved, err := resolveAssertions(request, bom)
	if err != nil {
		return EncodingFacts{}, err
	}
	if !resolved.selected.Equal(selected) {
		return EncodingFacts{}, encodingConflictError(bom, declaration, callerOverride)
	}
	return resolved, nil
}

// ProfileDefault returns the profile fallback that participated in
// resolution.
func (f EncodingFacts) ProfileDefault() SourceEncoding { return f.profileDefault }

// BomPolicy returns the BOM interpretation policy used for this source.
func (f EncodingFacts) BomPolicy() BomPolicy { return f.bomPolicy }

// Bom returns the recognized byte-order mark, when one exists.
func (f EncodingFacts) Bom() *BomKind { return f.bom }

// Declaration returns the normalized in-source declaration, when one
// exists.
func (f EncodingFacts) Declaration() *SourceEncoding { return f.declaration }

// CallerOverride returns the explicit caller override, when one exists.
func (f EncodingFacts) CallerOverride() *SourceEncoding { return f.callerOverride }

// Selected returns the encoding selected by the frozen priority rule.
func (f EncodingFacts) Selected() SourceEncoding { return f.selected }

// Equal reports whether two facts are identical.
func (f EncodingFacts) Equal(other EncodingFacts) bool {
	return f.profileDefault.Equal(other.profileDefault) &&
		f.bomPolicy == other.bomPolicy &&
		equalBomKind(f.bom, other.bom) &&
		equalEncodingPtr(f.declaration, other.declaration) &&
		equalEncodingPtr(f.callerOverride, other.callerOverride) &&
		f.selected.Equal(other.selected)
}

// resolutionRequest rebuilds the request that produced these facts.
func (f EncodingFacts) resolutionRequest() EncodingRequest {
	return EncodingRequest{
		profileDefault: f.profileDefault,
		bomPolicy:      f.bomPolicy,
		declaration:    f.declaration,
		callerOverride: f.callerOverride,
	}
}

func equalBomKind(left, right *BomKind) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func equalEncodingPtr(left, right *SourceEncoding) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Equal(*right)
}

// resolveEncoding resolves the deterministic encoding facts for raw bytes
// (document source.rs).
func resolveEncoding(bytes []byte, request EncodingRequest) (EncodingFacts, error) {
	hasExplicitText := (request.declaration != nil && request.declaration.IsText()) ||
		(request.callerOverride != nil && request.callerOverride.IsText())
	interpretBOM := request.bomPolicy == BomPolicyDetectUnicode &&
		(request.profileDefault.IsText() || hasExplicitText)
	var bom *BomKind
	var err error
	if interpretBOM {
		bom, err = detectBOM(bytes)
		if err != nil {
			return EncodingFacts{}, err
		}
	}
	return resolveAssertions(request, bom)
}

// resolveAssertions applies the frozen encoding priority rule (document
// source.rs).
func resolveAssertions(request EncodingRequest, bom *BomKind) (EncodingFacts, error) {
	if !request.profileDefault.IsText() &&
		((request.declaration != nil && request.declaration.IsText()) ||
			(request.callerOverride != nil && request.callerOverride.IsText())) {
		return EncodingFacts{}, encodingConflictError(bom, request.declaration, request.callerOverride)
	}
	var bomEncoding *SourceEncoding
	if bom != nil {
		encoding := bom.Encoding()
		bomEncoding = &encoding
	}
	var expected *SourceEncoding
	switch {
	case bomEncoding != nil:
		expected = bomEncoding
	case request.declaration != nil:
		expected = request.declaration
	default:
		expected = request.callerOverride
	}
	if expected != nil {
		if bomEncoding != nil && !bomEncoding.Equal(*expected) {
			return EncodingFacts{}, encodingConflictError(bom, request.declaration, request.callerOverride)
		}
		if request.declaration != nil && !request.declaration.Equal(*expected) {
			return EncodingFacts{}, encodingConflictError(bom, request.declaration, request.callerOverride)
		}
		if request.callerOverride != nil && !request.callerOverride.Equal(*expected) {
			return EncodingFacts{}, encodingConflictError(bom, request.declaration, request.callerOverride)
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
		selected = &request.profileDefault
	}
	return EncodingFacts{
		profileDefault: request.profileDefault,
		bomPolicy:      request.bomPolicy,
		bom:            bom,
		declaration:    request.declaration,
		callerOverride: request.callerOverride,
		selected:       *selected,
	}, nil
}

func encodingConflictError(bom *BomKind, declaration, callerOverride *SourceEncoding) *SourceError {
	return &SourceError{Kind: SourceErrorEncodingConflict, Bom: bom,
		Declaration: declaration, CallerOverride: callerOverride}
}

// detectBOM recognizes the frozen BOM set and rejects UTF-32 markers
// (document source.rs).
func detectBOM(bytes []byte) (*BomKind, error) {
	if len(bytes) >= 4 && bytes[0] == 0xff && bytes[1] == 0xfe && bytes[2] == 0x00 && bytes[3] == 0x00 {
		return nil, &SourceError{Kind: SourceErrorUnsupportedBom, UnsupportedBom: UnsupportedBomUtf32Le}
	}
	if len(bytes) >= 4 && bytes[0] == 0x00 && bytes[1] == 0x00 && bytes[2] == 0xfe && bytes[3] == 0xff {
		return nil, &SourceError{Kind: SourceErrorUnsupportedBom, UnsupportedBom: UnsupportedBomUtf32Be}
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
