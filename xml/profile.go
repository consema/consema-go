package xml

import "consema.dev/consema/document"

// XmlProfile is a frozen XML formation profile (consema-xml lib.rs:54-67).
type XmlProfile uint8

// The one frozen XML profile.
const (
	// XmlProfileSafeV1 is the namespace-aware, side-effect-free XML 1.0
	// Profile with the safe DTD subset (RFC 0012).
	XmlProfileSafeV1 XmlProfile = iota
)

// ID returns the stable profile identifier.
func (p XmlProfile) ID() document.ProfileId {
	return document.NewProfileId("xml.1.0-safe", 1)
}

// XmlEncodingSelectionKind is the closed document-entity encoding
// selection mode (consema-xml lib.rs:69-79).
type XmlEncodingSelectionKind uint8

// The two frozen selection modes.
const (
	// XmlEncodingProfileDefaultKind applies only the frozen profile default
	// and BOM rules.
	XmlEncodingProfileDefaultKind XmlEncodingSelectionKind = iota
	// XmlEncodingExplicitKind uses one caller-selected document-entity
	// encoding.
	XmlEncodingExplicitKind
)

// XmlEncodingSelection is an explicit document-entity encoding selection
// (consema-xml lib.rs:69-79). No-BOM source defaults to UTF-8. An explicit
// caller choice is evidence, not permission to contradict a BOM or a
// declaration.
type XmlEncodingSelection struct {
	kind     XmlEncodingSelectionKind
	encoding document.SourceEncoding
}

// XmlEncodingProfileDefault selects the frozen profile default and BOM
// rules.
func XmlEncodingProfileDefault() XmlEncodingSelection {
	return XmlEncodingSelection{kind: XmlEncodingProfileDefaultKind}
}

// XmlEncodingExplicit selects one caller-selected document-entity encoding
// (UTF-8, UTF-16LE, or UTF-16BE); other encodings are v1 Profile
// exclusions.
func XmlEncodingExplicit(encoding document.SourceEncoding) XmlEncodingSelection {
	return XmlEncodingSelection{kind: XmlEncodingExplicitKind, encoding: encoding}
}

// Kind returns the closed selection mode.
func (s XmlEncodingSelection) Kind() XmlEncodingSelectionKind { return s.kind }

// Encoding returns the caller-selected encoding of an explicit selection.
func (s XmlEncodingSelection) Encoding() document.SourceEncoding { return s.encoding }

// XmlParseLimits are the XML-specific formation, entity, and recovery
// limits (RFC 0012 §12; consema-xml lib.rs:81-128).
type XmlParseLimits struct {
	// Common is the common source, node, piece, nesting, and diagnostic
	// limits.
	Common document.ParseLimits
	// MaxDecodedUTF8Bytes is the maximum decoded UTF-8 bytes.
	MaxDecodedUTF8Bytes int
	// MaxDecodedScalars is the maximum decoded Unicode scalars and
	// coordinate steps.
	MaxDecodedScalars int
	// MaxElementCount is the maximum elements in the native tree.
	MaxElementCount int
	// MaxAttributeCount is the maximum attributes per element.
	MaxAttributeCount int
	// MaxNamespaceDeclarationCount is the maximum namespace declarations per
	// element.
	MaxNamespaceDeclarationCount int
	// MaxMixedContentItems is the maximum child content items per element.
	MaxMixedContentItems int
	// MaxQNameLength is the maximum QName bytes (prefix, local, and full
	// spelling).
	MaxQNameLength int
	// MaxNamespaceURILength is the maximum namespace URI bytes.
	MaxNamespaceURILength int
	// MaxAttributeValueLength is the maximum attribute-value decoded bytes.
	MaxAttributeValueLength int
	// MaxCommentLength is the maximum comment decoded bytes.
	MaxCommentLength int
	// MaxPiLength is the maximum processing-instruction content decoded
	// bytes.
	MaxPiLength int
	// MaxCdataLength is the maximum CDATA content decoded bytes.
	MaxCdataLength int
	// MaxTextLength is the maximum text content decoded bytes.
	MaxTextLength int
	// MaxDtdBytes is the maximum DTD subset raw bytes.
	MaxDtdBytes int
	// MaxEntityDeclarations is the maximum entity declarations.
	MaxEntityDeclarations int
	// MaxEntityReferences is the maximum entity references.
	MaxEntityReferences int
	// MaxEntityExpansionDepth is the maximum reference expansion depth.
	MaxEntityExpansionDepth int
	// MaxExpandedEntityBytes is the maximum expanded bytes across the whole
	// document.
	MaxExpandedEntityBytes int
	// MaxExpandedEntityScalars is the maximum expanded scalars across the
	// whole document.
	MaxExpandedEntityScalars int
	// MaxEntityAmplificationRatio is the maximum expanded/declared byte
	// amplification ratio.
	MaxEntityAmplificationRatio uint64
	// MaxRecoveryRegions is the maximum recovery error regions.
	MaxRecoveryRegions int
}

// DefaultXmlParseLimits returns the frozen defaults (consema-xml
// lib.rs:130-157).
func DefaultXmlParseLimits() XmlParseLimits {
	return XmlParseLimits{
		Common:                       document.DefaultParseLimits(),
		MaxDecodedUTF8Bytes:          128 << 20,
		MaxDecodedScalars:            64 << 20,
		MaxElementCount:              1_000_000,
		MaxAttributeCount:            100_000,
		MaxNamespaceDeclarationCount: 100_000,
		MaxMixedContentItems:         2_000_000,
		MaxQNameLength:               4 << 10,
		MaxNamespaceURILength:        8 << 10,
		MaxAttributeValueLength:      4 << 20,
		MaxCommentLength:             4 << 20,
		MaxPiLength:                  4 << 20,
		MaxCdataLength:               4 << 20,
		MaxTextLength:                4 << 20,
		MaxDtdBytes:                  4 << 20,
		MaxEntityDeclarations:        10_000,
		MaxEntityReferences:          1_000_000,
		MaxEntityExpansionDepth:      100,
		MaxExpandedEntityBytes:       32 << 20,
		MaxExpandedEntityScalars:     16 << 20,
		MaxEntityAmplificationRatio:  1_000,
		MaxRecoveryRegions:           100_000,
	}
}

// EntityLimits derives the entity expansion limits from these parse limits
// (consema-xml lib.rs:159-172).
func (l XmlParseLimits) EntityLimits() EntityExpansionLimits {
	return EntityExpansionLimits{
		MaxDeclarations:       l.MaxEntityDeclarations,
		MaxReferences:         l.MaxEntityReferences,
		MaxExpansionDepth:     l.MaxEntityExpansionDepth,
		MaxExpandedBytes:      l.MaxExpandedEntityBytes,
		MaxExpandedScalars:    l.MaxExpandedEntityScalars,
		MaxAmplificationRatio: l.MaxEntityAmplificationRatio,
	}
}

// XmlSyntaxKind is the closed XML lossless syntax-piece classification
// (RFC 0012 §7; consema-xml document.rs:17-94). Format kinds align
// one-to-one with the common document.LosslessStructuralIndex pieces.
type XmlSyntaxKind uint8

// The closed syntax-piece kinds in source order.
const (
	// XmlSyntaxKindBom is a Unicode byte-order mark.
	XmlSyntaxKindBom XmlSyntaxKind = iota
	// XmlSyntaxKindWhitespace is horizontal whitespace.
	XmlSyntaxKindWhitespace
	// XmlSyntaxKindLineBreak is a line break.
	XmlSyntaxKindLineBreak
	// XmlSyntaxKindDeclarationOpen is the `<?xml` declaration opening.
	XmlSyntaxKindDeclarationOpen
	// XmlSyntaxKindDeclarationName is a declaration pseudo-attribute name.
	XmlSyntaxKindDeclarationName
	// XmlSyntaxKindDeclarationValue is a declaration pseudo-attribute value.
	XmlSyntaxKindDeclarationValue
	// XmlSyntaxKindDeclarationClose is the `?>` declaration closing.
	XmlSyntaxKindDeclarationClose
	// XmlSyntaxKindDoctypeOpen is the `<!DOCTYPE` opening.
	XmlSyntaxKindDoctypeOpen
	// XmlSyntaxKindDoctypeName is the DOCTYPE name.
	XmlSyntaxKindDoctypeName
	// XmlSyntaxKindDtdMarkup is admitted internal DTD subset markup.
	XmlSyntaxKindDtdMarkup
	// XmlSyntaxKindDoctypeClose is the `>` DOCTYPE closing.
	XmlSyntaxKindDoctypeClose
	// XmlSyntaxKindTagOpen is the `<` or `</` tag opening.
	XmlSyntaxKindTagOpen
	// XmlSyntaxKindTagClose is the `>` tag closing.
	XmlSyntaxKindTagClose
	// XmlSyntaxKindEmptyElementClose is the `/>` empty-element closing.
	XmlSyntaxKindEmptyElementClose
	// XmlSyntaxKindEndTagOpen is the `</` end-tag opening.
	XmlSyntaxKindEndTagOpen
	// XmlSyntaxKindPrefix is a QName prefix spelling.
	XmlSyntaxKindPrefix
	// XmlSyntaxKindLocalName is a QName local-name spelling.
	XmlSyntaxKindLocalName
	// XmlSyntaxKindColon is a QName colon.
	XmlSyntaxKindColon
	// XmlSyntaxKindAttributeName is an attribute name.
	XmlSyntaxKindAttributeName
	// XmlSyntaxKindEquals is the `=` assignment.
	XmlSyntaxKindEquals
	// XmlSyntaxKindQuote is an attribute value quote.
	XmlSyntaxKindQuote
	// XmlSyntaxKindAttributeValue is attribute value content.
	XmlSyntaxKindAttributeValue
	// XmlSyntaxKindNamespaceDeclaration is an `xmlns` or `xmlns:p`
	// declaration.
	XmlSyntaxKindNamespaceDeclaration
	// XmlSyntaxKindText is character data without markup.
	XmlSyntaxKindText
	// XmlSyntaxKindEntityReference is a general or predefined entity
	// reference.
	XmlSyntaxKindEntityReference
	// XmlSyntaxKindCharacterReference is a decimal or hexadecimal character
	// reference.
	XmlSyntaxKindCharacterReference
	// XmlSyntaxKindCdataOpen is the `<![CDATA[` opening.
	XmlSyntaxKindCdataOpen
	// XmlSyntaxKindCdataText is CDATA content.
	XmlSyntaxKindCdataText
	// XmlSyntaxKindCdataClose is the `]]>` CDATA closing.
	XmlSyntaxKindCdataClose
	// XmlSyntaxKindCommentOpen is the `<!--` comment opening.
	XmlSyntaxKindCommentOpen
	// XmlSyntaxKindCommentText is comment content.
	XmlSyntaxKindCommentText
	// XmlSyntaxKindCommentClose is the `-->` comment closing.
	XmlSyntaxKindCommentClose
	// XmlSyntaxKindProcessingInstructionOpen is the `<?` PI opening.
	XmlSyntaxKindProcessingInstructionOpen
	// XmlSyntaxKindProcessingInstructionTarget is a PI target.
	XmlSyntaxKindProcessingInstructionTarget
	// XmlSyntaxKindProcessingInstructionContent is PI content.
	XmlSyntaxKindProcessingInstructionContent
	// XmlSyntaxKindProcessingInstructionClose is the `?>` PI closing.
	XmlSyntaxKindProcessingInstructionClose
	// XmlSyntaxKindErrorRegion is a recovered error region.
	XmlSyntaxKindErrorRegion
)

// AsStr returns the stable kind name used by the lossless syntax query
// protocol (consema-xml document.rs:801-844).
func (k XmlSyntaxKind) AsStr() string {
	switch k {
	case XmlSyntaxKindBom:
		return "bom"
	case XmlSyntaxKindWhitespace:
		return "whitespace"
	case XmlSyntaxKindLineBreak:
		return "line-break"
	case XmlSyntaxKindDeclarationOpen:
		return "declaration-open"
	case XmlSyntaxKindDeclarationName:
		return "declaration-name"
	case XmlSyntaxKindDeclarationValue:
		return "declaration-value"
	case XmlSyntaxKindDeclarationClose:
		return "declaration-close"
	case XmlSyntaxKindDoctypeOpen:
		return "doctype-open"
	case XmlSyntaxKindDoctypeName:
		return "doctype-name"
	case XmlSyntaxKindDtdMarkup:
		return "dtd-markup"
	case XmlSyntaxKindDoctypeClose:
		return "doctype-close"
	case XmlSyntaxKindTagOpen:
		return "tag-open"
	case XmlSyntaxKindTagClose:
		return "tag-close"
	case XmlSyntaxKindEmptyElementClose:
		return "empty-element-close"
	case XmlSyntaxKindEndTagOpen:
		return "end-tag-open"
	case XmlSyntaxKindPrefix:
		return "prefix"
	case XmlSyntaxKindLocalName:
		return "local-name"
	case XmlSyntaxKindColon:
		return "colon"
	case XmlSyntaxKindAttributeName:
		return "attribute-name"
	case XmlSyntaxKindEquals:
		return "equals"
	case XmlSyntaxKindQuote:
		return "quote"
	case XmlSyntaxKindAttributeValue:
		return "attribute-value"
	case XmlSyntaxKindNamespaceDeclaration:
		return "namespace-declaration"
	case XmlSyntaxKindText:
		return "text"
	case XmlSyntaxKindEntityReference:
		return "entity-reference"
	case XmlSyntaxKindCharacterReference:
		return "character-reference"
	case XmlSyntaxKindCdataOpen:
		return "cdata-open"
	case XmlSyntaxKindCdataText:
		return "cdata-text"
	case XmlSyntaxKindCdataClose:
		return "cdata-close"
	case XmlSyntaxKindCommentOpen:
		return "comment-open"
	case XmlSyntaxKindCommentText:
		return "comment-text"
	case XmlSyntaxKindCommentClose:
		return "comment-close"
	case XmlSyntaxKindProcessingInstructionOpen:
		return "processing-instruction-open"
	case XmlSyntaxKindProcessingInstructionTarget:
		return "processing-instruction-target"
	case XmlSyntaxKindProcessingInstructionContent:
		return "processing-instruction-content"
	case XmlSyntaxKindProcessingInstructionClose:
		return "processing-instruction-close"
	case XmlSyntaxKindErrorRegion:
		return "error-region"
	}
	return "error-region"
}

// XmlSyntaxKindFromName resolves one exact stable kind name
// (consema-xml document.rs:846-889).
func XmlSyntaxKindFromName(name string) (XmlSyntaxKind, bool) {
	switch name {
	case "bom":
		return XmlSyntaxKindBom, true
	case "whitespace":
		return XmlSyntaxKindWhitespace, true
	case "line-break":
		return XmlSyntaxKindLineBreak, true
	case "declaration-open":
		return XmlSyntaxKindDeclarationOpen, true
	case "declaration-name":
		return XmlSyntaxKindDeclarationName, true
	case "declaration-value":
		return XmlSyntaxKindDeclarationValue, true
	case "declaration-close":
		return XmlSyntaxKindDeclarationClose, true
	case "doctype-open":
		return XmlSyntaxKindDoctypeOpen, true
	case "doctype-name":
		return XmlSyntaxKindDoctypeName, true
	case "dtd-markup":
		return XmlSyntaxKindDtdMarkup, true
	case "doctype-close":
		return XmlSyntaxKindDoctypeClose, true
	case "tag-open":
		return XmlSyntaxKindTagOpen, true
	case "tag-close":
		return XmlSyntaxKindTagClose, true
	case "empty-element-close":
		return XmlSyntaxKindEmptyElementClose, true
	case "end-tag-open":
		return XmlSyntaxKindEndTagOpen, true
	case "prefix":
		return XmlSyntaxKindPrefix, true
	case "local-name":
		return XmlSyntaxKindLocalName, true
	case "colon":
		return XmlSyntaxKindColon, true
	case "attribute-name":
		return XmlSyntaxKindAttributeName, true
	case "equals":
		return XmlSyntaxKindEquals, true
	case "quote":
		return XmlSyntaxKindQuote, true
	case "attribute-value":
		return XmlSyntaxKindAttributeValue, true
	case "namespace-declaration":
		return XmlSyntaxKindNamespaceDeclaration, true
	case "text":
		return XmlSyntaxKindText, true
	case "entity-reference":
		return XmlSyntaxKindEntityReference, true
	case "character-reference":
		return XmlSyntaxKindCharacterReference, true
	case "cdata-open":
		return XmlSyntaxKindCdataOpen, true
	case "cdata-text":
		return XmlSyntaxKindCdataText, true
	case "cdata-close":
		return XmlSyntaxKindCdataClose, true
	case "comment-open":
		return XmlSyntaxKindCommentOpen, true
	case "comment-text":
		return XmlSyntaxKindCommentText, true
	case "comment-close":
		return XmlSyntaxKindCommentClose, true
	case "processing-instruction-open":
		return XmlSyntaxKindProcessingInstructionOpen, true
	case "processing-instruction-target":
		return XmlSyntaxKindProcessingInstructionTarget, true
	case "processing-instruction-content":
		return XmlSyntaxKindProcessingInstructionContent, true
	case "processing-instruction-close":
		return XmlSyntaxKindProcessingInstructionClose, true
	case "error-region":
		return XmlSyntaxKindErrorRegion, true
	}
	return 0, false
}
