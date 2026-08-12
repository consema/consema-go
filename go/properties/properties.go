package properties

import (
	"fmt"
	"unicode/utf16"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file defines the frozen Java Properties profile set, the exact
// Java UTF-16 string value, the lossless syntax kinds, and the parse
// limits (consema-properties lib.rs; RFC 0010 §3-§4, §14). The two
// profiles share one line/separator/continuation/escape grammar; they
// differ only in the source contract (explicit character decoding versus
// byte-exact Latin-1).

// PropertiesProfile is the frozen Java Properties formation profile
// (lib.rs:33-50). The unexported field makes the set closed.
type PropertiesProfile struct {
	name string
}

// PropertiesReaderV1 is the character-source profile corresponding to
// `Properties.load(Reader)`.
var PropertiesReaderV1 = PropertiesProfile{name: "java-properties.reader"}

// PropertiesLatin1V1 is the one-byte ISO-8859-1 profile corresponding to
// `Properties.load(InputStream)`.
var PropertiesLatin1V1 = PropertiesProfile{name: "java-properties.latin1"}

// ID returns the immutable profile identifier.
func (p PropertiesProfile) ID() document.ProfileId {
	return document.NewProfileId(p.name, 1)
}

// String returns the stable profile spelling.
func (p PropertiesProfile) String() string { return p.name }

// PropertiesEncodingSelection is the explicit source contract; no
// extension, locale, or platform default is consulted (lib.rs:52-59;
// RFC 0010 §3).
type PropertiesEncodingSelection struct {
	reader *document.SourceEncoding
}

// ReaderEncodingSelection selects Reader input decoded through one exact
// published text encoding.
func ReaderEncodingSelection(encoding document.SourceEncoding) PropertiesEncodingSelection {
	selected := encoding
	return PropertiesEncodingSelection{reader: &selected}
}

// Latin1EncodingSelection selects InputStream-compatible one-byte
// ISO-8859-1 mapping with marker bytes as content.
func Latin1EncodingSelection() PropertiesEncodingSelection {
	return PropertiesEncodingSelection{}
}

// IsLatin1 reports whether the selection is the byte-exact Latin-1
// contract.
func (s PropertiesEncodingSelection) IsLatin1() bool { return s.reader == nil }

// ReaderEncoding returns the explicitly selected Reader encoding.
func (s PropertiesEncodingSelection) ReaderEncoding() (document.SourceEncoding, bool) {
	if s.reader == nil {
		return document.SourceEncoding{}, false
	}
	return *s.reader, true
}

// JavaStringStatus is the surrogate well-formedness classification of one
// exact Java string (lib.rs:124-131).
type JavaStringStatus string

// The two frozen statuses.
const (
	// JavaStringWellFormedUnicode means every surrogate participates in
	// one adjacent high/low pair.
	JavaStringWellFormedUnicode JavaStringStatus = "WellFormedUnicode"
	// JavaStringUnpairedSurrogate means at least one surrogate is unpaired.
	JavaStringUnpairedSurrogate JavaStringStatus = "UnpairedSurrogate"
)

// JavaString is exact Java string content represented as immutable UTF-16
// code units (lib.rs:133-194; RFC 0010 §4). An unpaired surrogate is
// valid native content and is never replaced or rejected silently.
type JavaString struct {
	units  []uint16
	status JavaStringStatus
}

// NewJavaStringFromCodeUnits creates exact Java content and computes
// surrogate well-formedness.
func NewJavaStringFromCodeUnits(units []uint16) JavaString {
	status := classifyJavaString(units)
	return JavaString{units: append([]uint16(nil), units...), status: status}
}

// NewJavaStringFromUnicode converts one valid Unicode scalar string to its
// exact UTF-16 units.
func NewJavaStringFromUnicode(value string) JavaString {
	return NewJavaStringFromCodeUnits(utf16.Encode([]rune(value)))
}

// CodeUnits returns the exact ordered Java UTF-16 code units. The returned
// slice is a copy; the string is logically immutable.
func (s JavaString) CodeUnits() []uint16 {
	return append([]uint16(nil), s.units...)
}

// Utf16beBytes returns the canonical BOM-free big-endian `UTF16BE/1`
// bytes (RFC 0010 §4).
func (s JavaString) Utf16beBytes() []byte {
	output := make([]byte, 0, len(s.units)*2)
	for _, unit := range s.units {
		output = append(output, byte(unit>>8), byte(unit))
	}
	return output
}

// Status returns the exact surrogate pairing status.
func (s JavaString) Status() JavaStringStatus { return s.status }

// ToUnicode converts only well-formed Java content to a Go string.
func (s JavaString) ToUnicode() (string, error) {
	if s.status != JavaStringWellFormedUnicode {
		return "", JavaStringConversionError{}
	}
	return string(utf16.Decode(s.units)), nil
}

// Equal reports whether two Java strings have identical exact code units.
func (s JavaString) Equal(other JavaString) bool {
	if len(s.units) != len(other.units) {
		return false
	}
	for index := range s.units {
		if s.units[index] != other.units[index] {
			return false
		}
	}
	return true
}

// classifyJavaString computes the surrogate well-formedness status
// (lib.rs:814-830).
func classifyJavaString(units []uint16) JavaStringStatus {
	for index := 0; index < len(units); index++ {
		unit := units[index]
		switch {
		case unit >= 0xD800 && unit <= 0xDBFF:
			if index+1 < len(units) {
				next := units[index+1]
				if next >= 0xDC00 && next <= 0xDFFF {
					index++
					continue
				}
			}
			return JavaStringUnpairedSurrogate
		case unit >= 0xDC00 && unit <= 0xDFFF:
			return JavaStringUnpairedSurrogate
		}
	}
	return JavaStringWellFormedUnicode
}

// JavaStringConversionError reports that an exact Java string contains an
// unpaired surrogate and cannot enter a Unicode-only host string
// (lib.rs:196-206). It implements error.
type JavaStringConversionError struct{}

// Error implements error.
func (JavaStringConversionError) Error() string {
	return "Java UTF-16 string contains an unpaired surrogate"
}

// Code returns the registered code for the conversion boundary (RFC 0016 §6).
func (JavaStringConversionError) Code() string {
	return "java-properties.java-string.invalid-wire@1"
}

// PropertiesSyntaxKind is the closed lossless Properties syntax category
// (lib.rs:209-274). The stable query and protocol spellings are the exact
// `as_str` names.
type PropertiesSyntaxKind string

// The twelve frozen syntax kinds.
const (
	// SyntaxKindBom is a Unicode byte-order mark recognized by the Reader
	// source contract.
	SyntaxKindBom PropertiesSyntaxKind = "Bom"
	// SyntaxKindWhitespace is a space, tab, or form feed.
	SyntaxKindWhitespace PropertiesSyntaxKind = "Whitespace"
	// SyntaxKindLineBreak is LF, CR, or CRLF.
	SyntaxKindLineBreak PropertiesSyntaxKind = "LineBreak"
	// SyntaxKindCommentMarker is `#` or `!` starting a comment natural
	// line.
	SyntaxKindCommentMarker PropertiesSyntaxKind = "CommentMarker"
	// SyntaxKindCommentText is the comment payload.
	SyntaxKindCommentText PropertiesSyntaxKind = "CommentText"
	// SyntaxKindKey is raw property key content.
	SyntaxKindKey PropertiesSyntaxKind = "Key"
	// SyntaxKindSeparator is the whitespace and optional `=` or `:` between
	// key and value.
	SyntaxKindSeparator PropertiesSyntaxKind = "Separator"
	// SyntaxKindValue is raw property element content.
	SyntaxKindValue PropertiesSyntaxKind = "Value"
	// SyntaxKindEscapeMarker is the backslash beginning a normal escape.
	SyntaxKindEscapeMarker PropertiesSyntaxKind = "EscapeMarker"
	// SyntaxKindEscapeBody is a named, Unicode, or dropped-backslash escape
	// body.
	SyntaxKindEscapeBody PropertiesSyntaxKind = "EscapeBody"
	// SyntaxKindContinuationMarker is a backslash consumed by natural-line
	// continuation.
	SyntaxKindContinuationMarker PropertiesSyntaxKind = "ContinuationMarker"
	// SyntaxKindErrorRegion is malformed source retained through recovery.
	SyntaxKindErrorRegion PropertiesSyntaxKind = "ErrorRegion"
)

// AsStr returns the stable query and protocol name.
func (k PropertiesSyntaxKind) AsStr() string { return string(k) }

// PropertiesSyntaxKindFromName resolves one exact stable kind name.
func PropertiesSyntaxKindFromName(name string) (PropertiesSyntaxKind, bool) {
	switch name {
	case "Bom":
		return SyntaxKindBom, true
	case "Whitespace":
		return SyntaxKindWhitespace, true
	case "LineBreak":
		return SyntaxKindLineBreak, true
	case "CommentMarker":
		return SyntaxKindCommentMarker, true
	case "CommentText":
		return SyntaxKindCommentText, true
	case "Key":
		return SyntaxKindKey, true
	case "Separator":
		return SyntaxKindSeparator, true
	case "Value":
		return SyntaxKindValue, true
	case "EscapeMarker":
		return SyntaxKindEscapeMarker, true
	case "EscapeBody":
		return SyntaxKindEscapeBody, true
	case "ContinuationMarker":
		return SyntaxKindContinuationMarker, true
	case "ErrorRegion":
		return SyntaxKindErrorRegion, true
	}
	return "", false
}

// IsTrivia reports whether the kind is a trivia piece class (the
// structural_kind mapping, parser.rs:1002-1017).
func (k PropertiesSyntaxKind) IsTrivia() bool {
	switch k {
	case SyntaxKindWhitespace, SyntaxKindLineBreak, SyntaxKindCommentMarker,
		SyntaxKindCommentText:
		return true
	}
	return false
}

// PropertiesValueState is the semantic empty/present state with exact
// separator provenance (lib.rs:276-285; RFC 0010 §6).
type PropertiesValueState string

// The three frozen value states.
const (
	// ValueStateImplicitEmpty means no separator followed the key.
	ValueStateImplicitEmpty PropertiesValueState = "ImplicitEmpty"
	// ValueStateExplicitEmpty means a separator was present but the element
	// is empty.
	ValueStateExplicitEmpty PropertiesValueState = "ExplicitEmpty"
	// ValueStatePresent means the decoded element contains at least one
	// UTF-16 code unit.
	ValueStatePresent PropertiesValueState = "Present"
)

// PropertiesLogicalLineKind is the kind of one logical Properties record
// (lib.rs:287-294).
type PropertiesLogicalLineKind string

// The two frozen logical-line kinds.
const (
	// LogicalLineProperty is one completely formed property occurrence.
	LogicalLineProperty PropertiesLogicalLineKind = "Property"
	// LogicalLineError is one recovered malformed logical line.
	LogicalLineError PropertiesLogicalLineKind = "Error"
)

// PropertiesEscapeKind is the kind of one retained escape occurrence
// (lib.rs:295-307; RFC 0010 §7).
type PropertiesEscapeKind string

// The four frozen escape kinds.
const (
	// EscapeKindNamed is `\t`, `\n`, `\r`, or `\f`.
	EscapeKindNamed PropertiesEscapeKind = "Named"
	// EscapeKindBackslash is `\\`.
	EscapeKindBackslash PropertiesEscapeKind = "Backslash"
	// EscapeKindUnicode is the exact lowercase-`u` four-hex-digit escape.
	EscapeKindUnicode PropertiesEscapeKind = "Unicode"
	// EscapeKindDroppedBackslash is a backslash removed before another
	// source character.
	EscapeKindDroppedBackslash PropertiesEscapeKind = "DroppedBackslash"
)

// PropertiesParseLimits are the Java Properties parse and recovery limits
// (lib.rs:61-122; RFC 0010 §14).
type PropertiesParseLimits struct {
	// Common source, node, piece, and diagnostic limits.
	Common document.ParseLimits
	// MaxDecodedUTF8Bytes is the maximum decoded UTF-8 bytes in the source
	// snapshot.
	MaxDecodedUTF8Bytes int
	// MaxDecodedScalars is the maximum decoded Unicode scalars and
	// coordinate steps.
	MaxDecodedScalars int
	// MaxNaturalLines is the maximum natural source lines.
	MaxNaturalLines int
	// MaxNaturalLineBytes is the maximum raw bytes in one natural line.
	MaxNaturalLineBytes int
	// MaxNaturalLineScalars is the maximum decoded scalars in one natural
	// line.
	MaxNaturalLineScalars int
	// MaxLogicalLines is the maximum logical property or error lines.
	MaxLogicalLines int
	// MaxLogicalLineNaturalLines is the maximum natural-line constituents
	// in one logical line.
	MaxLogicalLineNaturalLines int
	// MaxLogicalLineScalars is the maximum decoded source scalars assembled
	// into one logical line.
	MaxLogicalLineScalars int
	// MaxProperties is the maximum property occurrences.
	MaxProperties int
	// MaxComments is the maximum comment occurrences.
	MaxComments int
	// MaxEscapes is the maximum escape occurrences.
	MaxEscapes int
	// MaxUnicodeEscapes is the maximum Unicode escape occurrences.
	MaxUnicodeEscapes int
	// MaxJavaCodeUnitsPerString is the maximum Java UTF-16 code units in
	// one key or value.
	MaxJavaCodeUnitsPerString int
	// MaxTotalJavaCodeUnits is the maximum Java UTF-16 code units across
	// the document.
	MaxTotalJavaCodeUnits int
	// MaxDuplicateGroupMembers is the maximum members in one duplicate-key
	// group.
	MaxDuplicateGroupMembers int
	// MaxRecoveryRegions is the maximum recovered error lines.
	MaxRecoveryRegions int
}

// DefaultPropertiesParseLimits returns the frozen defaults (lib.rs:100-122).
func DefaultPropertiesParseLimits() PropertiesParseLimits {
	return PropertiesParseLimits{
		Common:                     document.DefaultParseLimits(),
		MaxDecodedUTF8Bytes:        128 << 20,
		MaxDecodedScalars:          64 << 20,
		MaxNaturalLines:            2_000_000,
		MaxNaturalLineBytes:        4 << 20,
		MaxNaturalLineScalars:      2 << 20,
		MaxLogicalLines:            2_000_000,
		MaxLogicalLineNaturalLines: 100_000,
		MaxLogicalLineScalars:      16 << 20,
		MaxProperties:              2_000_000,
		MaxComments:                2_000_000,
		MaxEscapes:                 8_000_000,
		MaxUnicodeEscapes:          8_000_000,
		MaxJavaCodeUnitsPerString:  16 << 20,
		MaxTotalJavaCodeUnits:      64 << 20,
		MaxDuplicateGroupMembers:   1_000_000,
		MaxRecoveryRegions:         100_000,
	}
}

// FormationFailure is the fatal formation failure before any complete
// Document exists (consema-document lib.rs FatalFormationFailure). It
// implements error and the RFC 0016 §6 Code() contract with the first
// diagnostic's registered code.
type FormationFailure struct {
	diagnostics []*protocol.Diagnostic
}

// Diagnostics returns the ordered diagnostics explaining the failure.
func (f *FormationFailure) Diagnostics() []*protocol.Diagnostic {
	return append([]*protocol.Diagnostic(nil), f.diagnostics...)
}

// Error implements error; the text is human presentation only (RFC 0016 §6).
func (f *FormationFailure) Error() string {
	if len(f.diagnostics) == 0 {
		return "properties: formation failed"
	}
	return "properties: formation failed: " + f.diagnostics[0].Code
}

// Code returns the registered code of the first diagnostic.
func (f *FormationFailure) Code() string {
	if len(f.diagnostics) == 0 {
		return "core.parse.resource-limit@1"
	}
	return f.diagnostics[0].Code
}

// newFormationFailure builds a single-diagnostic fatal failure against the
// frozen registry.
func newFormationFailure(code string, category protocol.DiagnosticCategory,
	spanStart, spanEnd int, arguments map[string]string) *FormationFailure {
	var primary *protocol.SourceLocation
	if spanStart >= 0 {
		primary = &protocol.SourceLocation{StartByte: uint64(spanStart), EndByte: uint64(spanEnd)}
	}
	diagnostic, err := protocol.NewDiagnostic(code, category, protocol.SeverityError, primary,
		nil, arguments, nil, nil, 0, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		// All formation codes are registered; an unregistered code is an
		// internal invariant violation and must never produce a document.
		panic("properties: unregistered formation code " + code)
	}
	return &FormationFailure{diagnostics: []*protocol.Diagnostic{diagnostic}}
}

// resourceLimitFailure builds the frozen core.parse.resource-limit@1
// diagnostic (consema-document lib.rs FatalFormationFailure::resource_limit).
func resourceLimitFailure(name string, observed, limit int) *FormationFailure {
	return newFormationFailure("core.parse.resource-limit@1", protocol.CategoryResource,
		-1, -1, map[string]string{
			"limit":    fmt.Sprint(limit),
			"name":     name,
			"observed": fmt.Sprint(observed),
		})
}

// profileEncodingFailure builds the frozen
// java-properties.source.profile-encoding@1 fatal failure (parser.rs:83-91).
func profileEncodingFailure() *FormationFailure {
	return newFormationFailure("java-properties.source.profile-encoding@1",
		protocol.CategoryEncoding, -1, -1, nil)
}

// sourceFailure maps one source snapshot construction failure onto the
// frozen Properties formation codes.
func sourceFailure(err error) *FormationFailure {
	if sourceError, ok := err.(*document.SourceError); ok {
		switch sourceError.Kind {
		case document.SourceErrorInvalidSequence:
			return newFormationFailure("core.source.invalid-sequence@1",
				protocol.CategoryLexical, sourceError.ByteOffset, sourceError.ByteOffset, nil)
		case document.SourceErrorEncodingConflict:
			return newFormationFailure("core.source.encoding-conflict@1",
				protocol.CategoryEncoding, -1, -1, nil)
		case document.SourceErrorUnsupportedBom:
			return newFormationFailure("core.source.unsupported-bom@1",
				protocol.CategoryEncoding, -1, -1, nil)
		case document.SourceErrorResourceLimit:
			return resourceLimitFailure(sourceError.Name, sourceError.Observed, sourceError.Limit)
		}
	}
	return resourceLimitFailure("source-snapshot", 1, 1)
}

// StructuralPieceKind is the lossless class of one structural piece
// (document.StructuralPieceKind; consema-document lib.rs:415-422).
type StructuralPieceKind = document.StructuralPieceKind

// The three frozen piece classes.
const (
	// PieceToken is a lexical token.
	PieceToken = document.PieceToken
	// PieceTrivia is whitespace, newline, comment, or profile trivia.
	PieceTrivia = document.PieceTrivia
	// PieceErrorRegion is bytes not accepted as token or trivia.
	PieceErrorRegion = document.PieceErrorRegion
)

// StructuralPiece is one source byte interval and its lossless class
// (document.StructuralPiece).
type StructuralPiece = document.StructuralPiece

// LosslessStructuralIndex is the exhaustive ordered token/trivia coverage
// of one source (document.LosslessStructuralIndex). The index validates
// exact byte coverage and snapshot binding at construction.
type LosslessStructuralIndex = document.LosslessStructuralIndex

// NewLosslessStructuralIndex validates exact raw-byte coverage of the
// source and snapshot binding.
func NewLosslessStructuralIndex(identity document.SnapshotIdentity, sourceLen int,
	pieces []StructuralPiece) (*LosslessStructuralIndex, error) {
	return document.NewLosslessStructuralIndex(identity, sourceLen, pieces)
}
