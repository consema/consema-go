package plist

import "consema.dev/consema/document"

// This file defines the frozen `plist.xml@1` / `plist.binary@1` profile
// identities, the explicit source-encoding selection, the plist formation
// limits (RFC 0013 §12), and the closed syntax-kind and native-kind
// vocabularies of the family.

// PlistProfile is a frozen plist formation profile (consema-plist lib.rs;
// RFC 0013 §1). The profile is selected by the caller before formation;
// neither the `bplist00` magic number nor a `.plist` extension selects
// semantics.
type PlistProfile uint8

// The two frozen plist profiles.
const (
	// PlistProfileXmlV1 is the plist value vocabulary expressed as XML 1.0
	// (RFC 0013 §4).
	PlistProfileXmlV1 PlistProfile = iota
	// PlistProfileBinaryV1 is the binary object-table representation
	// (RFC 0013 §5).
	PlistProfileBinaryV1
)

// ID returns the immutable profile identifier.
func (p PlistProfile) ID() document.ProfileId {
	switch p {
	case PlistProfileXmlV1:
		return document.NewProfileId("plist.xml", 1)
	case PlistProfileBinaryV1:
		return document.NewProfileId("plist.binary", 1)
	}
	return document.NewProfileId("plist.xml", 1)
}

// PlistEncodingSelection is the explicit source-encoding selection
// (consema-plist lib.rs PlistEncodingSelection; RFC 0013 §2). For the XML
// profile the selection follows the RFC 0012 source contract: no-BOM source
// defaults to UTF-8, and an explicit caller choice is evidence, not
// permission to contradict a BOM or a declaration. The binary profile has no
// text encoding and no BOM; only the profile default and an explicit Binary
// selection are consistent with it. An inconsistent selection is a fatal
// source-contract conflict at formation.
type PlistEncodingSelection struct {
	explicit *document.SourceEncoding
}

// PlistEncodingProfileDefault applies only the frozen profile default and
// BOM rules.
func PlistEncodingProfileDefault() PlistEncodingSelection {
	return PlistEncodingSelection{}
}

// PlistEncodingExplicit uses one caller-selected document-entity encoding
// (XML profile) or `document.BinaryEncoding()` (binary profile).
func PlistEncodingExplicit(encoding document.SourceEncoding) PlistEncodingSelection {
	return PlistEncodingSelection{explicit: &encoding}
}

// Explicit returns the caller-selected encoding, when one exists.
func (s PlistEncodingSelection) Explicit() *document.SourceEncoding {
	if s.explicit == nil {
		return nil
	}
	encoding := *s.explicit
	return &encoding
}

// PlistParseLimits bounds plist formation, structure, recovery, and
// conversion (consema-plist lib.rs PlistParseLimits; RFC 0013 §12). Every
// limit failure is a fatal formation failure or an atomic operation failure;
// a limit failure never masquerades as an empty tree, truncated data, a
// shortened query, a partial target, or a successful edit (hard gate 4).
type PlistParseLimits struct {
	// Common holds the shared source, node, nesting, token, and diagnostic
	// limits.
	Common document.ParseLimits
	// MaxDecodedUTF8Bytes bounds the decoded UTF-8 bytes (XML profile).
	MaxDecodedUTF8Bytes int
	// MaxDecodedScalars bounds the decoded Unicode scalars (XML profile).
	MaxDecodedScalars int
	// MaxObjectCount bounds native objects: binary object-table entries and
	// native arena nodes.
	MaxObjectCount int
	// MaxContainerDepth bounds the container nesting depth of the native
	// value graph.
	MaxContainerDepth int
	// MaxDictEntries bounds the associations of one dictionary.
	MaxDictEntries int
	// MaxArrayElements bounds the elements of one array.
	MaxArrayElements int
	// MaxDuplicateKeyGroupMembers bounds one duplicate-key group.
	MaxDuplicateKeyGroupMembers int
	// MaxStringCodeUnits bounds one string or key.
	MaxStringCodeUnits int
	// MaxDataBytes bounds one data value.
	MaxDataBytes int
	// MaxUIDCount bounds the UID values of one document.
	MaxUIDCount int
	// MaxExtendedSizeIntegers bounds extended-size integer objects (binary
	// profile).
	MaxExtendedSizeIntegers int
	// MaxExtendedSizeValue bounds one extended-size magnitude (binary
	// profile).
	MaxExtendedSizeValue int
	// MaxOffsetIntSize bounds the `offsetIntSize` width (binary profile).
	MaxOffsetIntSize int
	// MaxObjectRefSize bounds the `objectRefSize` width (binary profile).
	MaxObjectRefSize int
	// MaxOffsetTableBytes bounds the offset-table bytes (binary profile).
	MaxOffsetTableBytes int
	// MaxSyntaxPieces bounds the XML lossless syntax pieces.
	MaxSyntaxPieces int
	// MaxBinaryFacts bounds the binary object/offset/trailer facts.
	MaxBinaryFacts int
	// MaxConversionNodes bounds cross-representation conversion nodes.
	MaxConversionNodes int
	// MaxReportEvents bounds conversion, projection, or edit report events.
	MaxReportEvents int
	// MaxRecoveryRegions bounds recovery regions.
	MaxRecoveryRegions int
}

// DefaultPlistParseLimits returns the frozen defaults (consema-plist
// lib.rs:170-193).
func DefaultPlistParseLimits() PlistParseLimits {
	return PlistParseLimits{
		Common:                      document.DefaultParseLimits(),
		MaxDecodedUTF8Bytes:         128 << 20,
		MaxDecodedScalars:           64 << 20,
		MaxObjectCount:              1_000_000,
		MaxContainerDepth:           256,
		MaxDictEntries:              1_000_000,
		MaxArrayElements:            1_000_000,
		MaxDuplicateKeyGroupMembers: 1_000_000,
		MaxStringCodeUnits:          16 << 20,
		MaxDataBytes:                16 << 20,
		MaxUIDCount:                 100_000,
		MaxExtendedSizeIntegers:     10_000,
		MaxExtendedSizeValue:        1_000_000,
		MaxOffsetIntSize:            8,
		MaxObjectRefSize:            8,
		MaxOffsetTableBytes:         8 << 20,
		MaxSyntaxPieces:             2_000_000,
		MaxBinaryFacts:              2_000_000,
		MaxConversionNodes:          1_000_000,
		MaxReportEvents:             100_000,
		MaxRecoveryRegions:          100_000,
	}
}

// arenaLimits derives the native arena bounds from these parse limits.
func (l PlistParseLimits) arenaLimits() PlistArenaLimits {
	return PlistArenaLimits{MaxObjects: l.MaxObjectCount, MaxContainerDepth: l.MaxContainerDepth}
}

// PlistSyntaxKind is the closed lossless plist XML syntax-kind set
// (consema-plist parser_xml.rs PlistSyntaxKind; RFC 0013 §8.2). Every
// non-empty raw byte of a `plist.xml@1` source belongs to exactly one
// ordered structural piece with one of these kinds.
type PlistSyntaxKind uint8

// The closed syntax kinds in RFC 0013 §8.2 order.
const (
	PlistSyntaxKindBom PlistSyntaxKind = iota
	PlistSyntaxKindWhitespace
	PlistSyntaxKindLineBreak
	PlistSyntaxKindDeclarationOpen
	PlistSyntaxKindDeclarationName
	PlistSyntaxKindDeclarationValue
	PlistSyntaxKindDeclarationClose
	PlistSyntaxKindDoctypeOpen
	PlistSyntaxKindDoctypeBody
	PlistSyntaxKindDoctypeClose
	PlistSyntaxKindPlistOpen
	PlistSyntaxKindPlistVersionName
	PlistSyntaxKindPlistVersionValue
	PlistSyntaxKindPlistClose
	PlistSyntaxKindDictOpen
	PlistSyntaxKindDictClose
	PlistSyntaxKindKeyOpen
	PlistSyntaxKindKeyClose
	PlistSyntaxKindArrayOpen
	PlistSyntaxKindArrayClose
	PlistSyntaxKindStringOpen
	PlistSyntaxKindStringClose
	PlistSyntaxKindIntegerOpen
	PlistSyntaxKindIntegerClose
	PlistSyntaxKindRealOpen
	PlistSyntaxKindRealClose
	PlistSyntaxKindDateOpen
	PlistSyntaxKindDateClose
	PlistSyntaxKindDataOpen
	PlistSyntaxKindDataClose
	PlistSyntaxKindTrue
	PlistSyntaxKindFalse
	PlistSyntaxKindText
	PlistSyntaxKindEntityReference
	PlistSyntaxKindCharacterReference
	PlistSyntaxKindCdataOpen
	PlistSyntaxKindCdataText
	PlistSyntaxKindCdataClose
	PlistSyntaxKindCommentOpen
	PlistSyntaxKindCommentText
	PlistSyntaxKindCommentClose
	PlistSyntaxKindProcessingInstructionOpen
	PlistSyntaxKindProcessingInstructionTarget
	PlistSyntaxKindProcessingInstructionContent
	PlistSyntaxKindProcessingInstructionClose
	PlistSyntaxKindErrorRegion
)

// AsStr returns the stable query and protocol name of the kind.
func (k PlistSyntaxKind) AsStr() string {
	switch k {
	case PlistSyntaxKindBom:
		return "bom"
	case PlistSyntaxKindWhitespace:
		return "whitespace"
	case PlistSyntaxKindLineBreak:
		return "line-break"
	case PlistSyntaxKindDeclarationOpen:
		return "declaration-open"
	case PlistSyntaxKindDeclarationName:
		return "declaration-name"
	case PlistSyntaxKindDeclarationValue:
		return "declaration-value"
	case PlistSyntaxKindDeclarationClose:
		return "declaration-close"
	case PlistSyntaxKindDoctypeOpen:
		return "doctype-open"
	case PlistSyntaxKindDoctypeBody:
		return "doctype-body"
	case PlistSyntaxKindDoctypeClose:
		return "doctype-close"
	case PlistSyntaxKindPlistOpen:
		return "plist-open"
	case PlistSyntaxKindPlistVersionName:
		return "plist-version-name"
	case PlistSyntaxKindPlistVersionValue:
		return "plist-version-value"
	case PlistSyntaxKindPlistClose:
		return "plist-close"
	case PlistSyntaxKindDictOpen:
		return "dict-open"
	case PlistSyntaxKindDictClose:
		return "dict-close"
	case PlistSyntaxKindKeyOpen:
		return "key-open"
	case PlistSyntaxKindKeyClose:
		return "key-close"
	case PlistSyntaxKindArrayOpen:
		return "array-open"
	case PlistSyntaxKindArrayClose:
		return "array-close"
	case PlistSyntaxKindStringOpen:
		return "string-open"
	case PlistSyntaxKindStringClose:
		return "string-close"
	case PlistSyntaxKindIntegerOpen:
		return "integer-open"
	case PlistSyntaxKindIntegerClose:
		return "integer-close"
	case PlistSyntaxKindRealOpen:
		return "real-open"
	case PlistSyntaxKindRealClose:
		return "real-close"
	case PlistSyntaxKindDateOpen:
		return "date-open"
	case PlistSyntaxKindDateClose:
		return "date-close"
	case PlistSyntaxKindDataOpen:
		return "data-open"
	case PlistSyntaxKindDataClose:
		return "data-close"
	case PlistSyntaxKindTrue:
		return "true"
	case PlistSyntaxKindFalse:
		return "false"
	case PlistSyntaxKindText:
		return "text"
	case PlistSyntaxKindEntityReference:
		return "entity-reference"
	case PlistSyntaxKindCharacterReference:
		return "character-reference"
	case PlistSyntaxKindCdataOpen:
		return "cdata-open"
	case PlistSyntaxKindCdataText:
		return "cdata-text"
	case PlistSyntaxKindCdataClose:
		return "cdata-close"
	case PlistSyntaxKindCommentOpen:
		return "comment-open"
	case PlistSyntaxKindCommentText:
		return "comment-text"
	case PlistSyntaxKindCommentClose:
		return "comment-close"
	case PlistSyntaxKindProcessingInstructionOpen:
		return "processing-instruction-open"
	case PlistSyntaxKindProcessingInstructionTarget:
		return "processing-instruction-target"
	case PlistSyntaxKindProcessingInstructionContent:
		return "processing-instruction-content"
	case PlistSyntaxKindProcessingInstructionClose:
		return "processing-instruction-close"
	case PlistSyntaxKindErrorRegion:
		return "error-region"
	}
	return "error-region"
}

// PlistSyntaxKindFromName resolves one exact stable kind name (RFC 0013
// §8.2).
func PlistSyntaxKindFromName(name string) (PlistSyntaxKind, bool) {
	for kind := PlistSyntaxKindBom; kind <= PlistSyntaxKindErrorRegion; kind++ {
		if kind.AsStr() == name {
			return kind, true
		}
	}
	return 0, false
}

// PlistValueKind is the closed native plist value kind (consema-plist
// native.rs PlistValueKind; RFC 0013 §6).
type PlistValueKind uint8

// The nine frozen native kinds.
const (
	PlistValueKindDict PlistValueKind = iota
	PlistValueKindArray
	PlistValueKindString
	PlistValueKindInteger
	PlistValueKindReal
	PlistValueKindBoolean
	PlistValueKindDate
	PlistValueKindData
	PlistValueKindUid
)

// AsStr returns the stable query and protocol name of the kind.
func (k PlistValueKind) AsStr() string {
	switch k {
	case PlistValueKindDict:
		return "dict"
	case PlistValueKindArray:
		return "array"
	case PlistValueKindString:
		return "string"
	case PlistValueKindInteger:
		return "integer"
	case PlistValueKindReal:
		return "real"
	case PlistValueKindBoolean:
		return "boolean"
	case PlistValueKindDate:
		return "date"
	case PlistValueKindData:
		return "data"
	case PlistValueKindUid:
		return "uid"
	}
	return "dict"
}

// plistKindFromName resolves one frozen closed kind name of
// `plist.value-type-is@1`.
func plistKindFromName(name string) (PlistValueKind, bool) {
	for kind := PlistValueKindDict; kind <= PlistValueKindUid; kind++ {
		if kind.AsStr() == name {
			return kind, true
		}
	}
	return 0, false
}

// PlistStringStatus is whether exact UTF-16 code units form Unicode scalar
// text (consema-plist native.rs PlistStringStatus; RFC 0013 §6).
type PlistStringStatus uint8

// The two frozen string statuses.
const (
	// PlistStringWellFormedUnicode: every surrogate participates in one
	// adjacent high/low pair.
	PlistStringWellFormedUnicode PlistStringStatus = iota
	// PlistStringUnpairedSurrogate: at least one surrogate is unpaired.
	PlistStringUnpairedSurrogate
)

// String returns the stable status name.
func (s PlistStringStatus) String() string {
	switch s {
	case PlistStringWellFormedUnicode:
		return "WellFormedUnicode"
	case PlistStringUnpairedSurrogate:
		return "UnpairedSurrogate"
	}
	return "WellFormedUnicode"
}

// RealWidth is the width fact of one exact IEEE 754 real payload
// (consema-plist native.rs RealWidth; RFC 0013 §5.5).
type RealWidth uint8

// The two frozen real widths.
const (
	// RealWidthFloat64 is the 8-byte IEEE 754 binary64.
	RealWidthFloat64 RealWidth = iota
	// RealWidthFloat32 is the 4-byte IEEE 754 binary32 (only binary marker
	// `0x22`).
	RealWidthFloat32
)

// String returns the stable width name.
func (w RealWidth) String() string {
	switch w {
	case RealWidthFloat64:
		return "Float64"
	case RealWidthFloat32:
		return "Float32"
	}
	return "Float64"
}
