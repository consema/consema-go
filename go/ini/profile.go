package ini

import (
	"consema.dev/consema/document"
)

// IniProfile is the frozen INI formation profile (consema-ini lib.rs:
// 36-44). The unexported field makes the set closed: no other profile is
// constructible.
type IniProfile struct {
	name string
}

// PortableV1 is the conservative ASCII exchange subset (RFC 0009 §5).
var PortableV1 = IniProfile{name: "ini.portable"}

// WindowsV1 is the deterministic Windows profile-string file surface
// (RFC 0009 §6).
var WindowsV1 = IniProfile{name: "ini.windows"}

// PythonConfigParserV1 is the Python 3.14 ConfigParser default formation
// surface without evaluation (RFC 0009 §7).
var PythonConfigParserV1 = IniProfile{name: "ini.python-configparser"}

// ID returns the immutable profile identifier.
func (p IniProfile) ID() document.ProfileId {
	return document.NewProfileId(p.name, 1)
}

// String returns the stable profile spelling.
func (p IniProfile) String() string { return p.name }

// isPortable reports whether this is the portable profile.
func (p IniProfile) isPortable() bool { return p == PortableV1 }

// isWindows reports whether this is the Windows profile.
func (p IniProfile) isWindows() bool { return p == WindowsV1 }

// isPython reports whether this is the Python ConfigParser profile.
func (p IniProfile) isPython() bool { return p == PythonConfigParserV1 }

// IniEncodingSelection is the explicit source-encoding selection; no host
// locale is ever consulted (consema-ini lib.rs:58-65).
type IniEncodingSelection struct {
	explicit *document.SourceEncoding
}

// IniEncodingProfileDefault applies only the selected profile's frozen
// default encoding and BOM rules.
func IniEncodingProfileDefault() IniEncodingSelection {
	return IniEncodingSelection{}
}

// IniEncodingExplicit selects one exact source encoding (UTF-8, UTF-16LE,
// Latin-1, or one registered Windows code page). The selection is validated
// against the profile before any Document exists.
func IniEncodingExplicit(encoding document.SourceEncoding) IniEncodingSelection {
	selected := encoding
	return IniEncodingSelection{explicit: &selected}
}

// Explicit returns the caller-selected encoding, when one exists.
func (s IniEncodingSelection) Explicit() *document.SourceEncoding {
	if s.explicit == nil {
		return nil
	}
	selected := *s.explicit
	return &selected
}

// IniParseLimits are the INI-specific parse and recovery resource bounds
// (consema-ini lib.rs:67-98).
type IniParseLimits struct {
	// Common holds the shared source, node, piece, nesting, and diagnostic
	// limits.
	Common document.ParseLimits
	// MaxDecodedUTF8Bytes bounds the decoded UTF-8 text.
	MaxDecodedUTF8Bytes int
	// MaxDecodedScalars bounds the decoded Unicode scalar count.
	MaxDecodedScalars int
	// MaxPhysicalLines bounds the physical source lines.
	MaxPhysicalLines int
	// MaxPhysicalLineBytes bounds the raw bytes of one physical line.
	MaxPhysicalLineBytes int
	// MaxPhysicalLineScalars bounds the decoded scalars of one physical
	// line.
	MaxPhysicalLineScalars int
	// MaxLogicalLines bounds the logical records.
	MaxLogicalLines int
	// MaxLogicalLineBytes bounds the raw bytes owned by one logical record.
	MaxLogicalLineBytes int
	// MaxLogicalLineScalars bounds the decoded scalars of one logical
	// record.
	MaxLogicalLineScalars int
	// MaxContinuationLines bounds the continuation physical lines per
	// Python entry.
	MaxContinuationLines int
	// MaxSections bounds the section occurrences.
	MaxSections int
	// MaxEntries bounds the entry occurrences.
	MaxEntries int
	// MaxDuplicateGroupMembers bounds one duplicate or case-equivalence
	// group.
	MaxDuplicateGroupMembers int
	// MaxRecoveryRegions bounds the recovered error lines.
	MaxRecoveryRegions int
}

// DefaultIniParseLimits returns the frozen defaults (consema-ini
// lib.rs:100-118).
func DefaultIniParseLimits() IniParseLimits {
	return IniParseLimits{
		Common:                   document.DefaultParseLimits(),
		MaxDecodedUTF8Bytes:      128 << 20,
		MaxDecodedScalars:        64 << 20,
		MaxPhysicalLines:         2_000_000,
		MaxPhysicalLineBytes:     4 << 20,
		MaxPhysicalLineScalars:   2_000_000,
		MaxLogicalLines:          2_000_000,
		MaxLogicalLineBytes:      16 << 20,
		MaxLogicalLineScalars:    8_000_000,
		MaxContinuationLines:     100_000,
		MaxSections:              1_000_000,
		MaxEntries:               1_000_000,
		MaxDuplicateGroupMembers: 100_000,
		MaxRecoveryRegions:       100_000,
	}
}

// IniSyntaxKind is one lossless INI syntax category (consema-ini lib.rs:
// 121-152). The stable query and protocol spellings are the exact `as_str`
// names.
type IniSyntaxKind string

// The fourteen frozen INI syntax kinds.
const (
	// SyntaxKindBom is a Unicode byte-order mark.
	SyntaxKindBom IniSyntaxKind = "Bom"
	// SyntaxKindWhitespace is horizontal whitespace.
	SyntaxKindWhitespace IniSyntaxKind = "Whitespace"
	// SyntaxKindLineBreak is LF or CRLF.
	SyntaxKindLineBreak IniSyntaxKind = "LineBreak"
	// SyntaxKindCommentMarker is the prefix comment marker.
	SyntaxKindCommentMarker IniSyntaxKind = "CommentMarker"
	// SyntaxKindCommentText is the comment payload.
	SyntaxKindCommentText IniSyntaxKind = "CommentText"
	// SyntaxKindSectionOpen is the opening section bracket.
	SyntaxKindSectionOpen IniSyntaxKind = "SectionOpen"
	// SyntaxKindSectionName is the section name text.
	SyntaxKindSectionName IniSyntaxKind = "SectionName"
	// SyntaxKindSectionClose is the closing section bracket.
	SyntaxKindSectionClose IniSyntaxKind = "SectionClose"
	// SyntaxKindEntryKey is the entry key text.
	SyntaxKindEntryKey IniSyntaxKind = "EntryKey"
	// SyntaxKindDelimiter is the entry delimiter.
	SyntaxKindDelimiter IniSyntaxKind = "Delimiter"
	// SyntaxKindQuote is a value quote.
	SyntaxKindQuote IniSyntaxKind = "Quote"
	// SyntaxKindEntryValue is the entry value text.
	SyntaxKindEntryValue IniSyntaxKind = "EntryValue"
	// SyntaxKindContinuationMarker is skipped indentation on a Python
	// continuation line.
	SyntaxKindContinuationMarker IniSyntaxKind = "ContinuationMarker"
	// SyntaxKindErrorRegion is a profile-invalid or malformed source range.
	SyntaxKindErrorRegion IniSyntaxKind = "ErrorRegion"
)

// AsStr returns the stable query and protocol name.
func (k IniSyntaxKind) AsStr() string { return string(k) }

// IniSyntaxKindFromName resolves one exact stable kind name.
func IniSyntaxKindFromName(name string) (IniSyntaxKind, bool) {
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
	case "SectionOpen":
		return SyntaxKindSectionOpen, true
	case "SectionName":
		return SyntaxKindSectionName, true
	case "SectionClose":
		return SyntaxKindSectionClose, true
	case "EntryKey":
		return SyntaxKindEntryKey, true
	case "Delimiter":
		return SyntaxKindDelimiter, true
	case "Quote":
		return SyntaxKindQuote, true
	case "EntryValue":
		return SyntaxKindEntryValue, true
	case "ContinuationMarker":
		return SyntaxKindContinuationMarker, true
	case "ErrorRegion":
		return SyntaxKindErrorRegion, true
	}
	return "", false
}

// IniValueState is the native value-presence fact (consema-ini lib.rs:
// 197-206).
type IniValueState string

// The three frozen value states.
const (
	// ValueStateMissing means no delimiter/value was present; only
	// recovered error records use it in v1.
	ValueStateMissing IniValueState = "Missing"
	// ValueStateEmpty means a delimiter was present with empty semantic
	// content.
	ValueStateEmpty IniValueState = "Empty"
	// ValueStatePresent means non-empty semantic string content.
	ValueStatePresent IniValueState = "Present"
)

// AsStr returns the stable state name.
func (s IniValueState) AsStr() string { return string(s) }

// IniQuoteStyle is the profile-recognized outer quote style (consema-ini
// lib.rs:208-217).
type IniQuoteStyle string

// The three frozen quote styles.
const (
	// QuoteStyleNone means no semantic outer quotes.
	QuoteStyleNone IniQuoteStyle = "None"
	// QuoteStyleSingle is exact single quotes under the Windows profile.
	QuoteStyleSingle IniQuoteStyle = "Single"
	// QuoteStyleDouble is exact double quotes under the Windows profile.
	QuoteStyleDouble IniQuoteStyle = "Double"
)

// AsStr returns the stable style name.
func (s IniQuoteStyle) AsStr() string { return string(s) }

// IniLogicalLineKind is the kind of one logical INI record (consema-ini
// lib.rs:219-228).
type IniLogicalLineKind string

// The three frozen logical-record kinds.
const (
	// LogicalLineSection is a section header record.
	LogicalLineSection IniLogicalLineKind = "Section"
	// LogicalLineEntry is an entry and any continuation lines.
	LogicalLineEntry IniLogicalLineKind = "Entry"
	// LogicalLineError is a recovered malformed record.
	LogicalLineError IniLogicalLineKind = "Error"
)

// AsStr returns the stable kind name.
func (k IniLogicalLineKind) AsStr() string { return string(k) }
