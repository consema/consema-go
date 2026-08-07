package document

import (
	"fmt"
	"sync/atomic"
)

// snapshotIdentityCounter allocates process-local snapshot identities; 0
// is never assigned (document lib.rs:39, 62-65).
var snapshotIdentityCounter uint64

// SnapshotIdentity is the opaque identity of exactly one immutable
// document snapshot (document lib.rs:41-51). Identities are process-local
// and stable for the lifetime of the process.
type SnapshotIdentity uint64

// AsU64 returns the stable process-local representation used by protocol
// diagnostics.
func (i SnapshotIdentity) AsU64() uint64 { return uint64(i) }

// allocateSnapshotIdentity issues a fresh process-local identity.
func allocateSnapshotIdentity() SnapshotIdentity {
	return SnapshotIdentity(atomic.AddUint64(&snapshotIdentityCounter, 1))
}

// NodeRole is the semantic role of one document structural identity
// (document lib.rs:112-251).
type NodeRole string

// The closed structural role set, mirroring consema-document's roles.
const (
	// RoleSyntaxNode is a format syntax node.
	RoleSyntaxNode NodeRole = "SyntaxNode"
	// RoleToken is a lexical token.
	RoleToken NodeRole = "Token"
	// RoleObjectMember is a JSON object member association.
	RoleObjectMember NodeRole = "ObjectMember"
	// RoleObjectKey is a JSON object key.
	RoleObjectKey NodeRole = "ObjectKey"
	// RoleArrayElement is a JSON array element association.
	RoleArrayElement NodeRole = "ArrayElement"
	// RoleValue is a complete semantic value syntax.
	RoleValue NodeRole = "Value"
	// RoleTomlItem is a TOML native semantic item, including tables and
	// array-of-tables.
	RoleTomlItem NodeRole = "TomlItem"
	// RoleTomlEntry is a TOML table or inline-table key-to-item association.
	RoleTomlEntry NodeRole = "TomlEntry"
	// RoleTomlKey is a TOML decoded key segment with source identity.
	RoleTomlKey NodeRole = "TomlKey"
	// RoleTomlArrayElement is a TOML array or array-of-tables element
	// association.
	RoleTomlArrayElement NodeRole = "TomlArrayElement"
	// RoleBinaryRegion is a format-owned region in an opaque binary
	// document.
	RoleBinaryRegion NodeRole = "BinaryRegion"
	// RoleJsonSyntaxPiece is one JSON lossless syntax piece.
	RoleJsonSyntaxPiece NodeRole = "JsonSyntaxPiece"
	// RoleTomlSyntaxPiece is one TOML lossless syntax piece.
	RoleTomlSyntaxPiece NodeRole = "TomlSyntaxPiece"
	// RoleYamlStream is a complete YAML serialization stream.
	RoleYamlStream NodeRole = "YamlStream"
	// RoleYamlDocument is one independent YAML document in a stream.
	RoleYamlDocument NodeRole = "YamlDocument"
	// RoleYamlNode is a YAML representation node.
	RoleYamlNode NodeRole = "YamlNode"
	// RoleYamlSequenceElement is a YAML ordered sequence association.
	RoleYamlSequenceElement NodeRole = "YamlSequenceElement"
	// RoleYamlMappingEntry is a YAML ordered mapping association.
	RoleYamlMappingEntry NodeRole = "YamlMappingEntry"
	// RoleYamlAlias is a YAML alias serialization occurrence.
	RoleYamlAlias NodeRole = "YamlAlias"
	// RoleYamlAnchorDefinition is a YAML anchor definition occurrence.
	RoleYamlAnchorDefinition NodeRole = "YamlAnchorDefinition"
	// RoleYamlSyntaxPiece is one YAML lossless syntax piece.
	RoleYamlSyntaxPiece NodeRole = "YamlSyntaxPiece"
	// RoleIniDocument is a complete INI document.
	RoleIniDocument NodeRole = "IniDocument"
	// RoleIniPhysicalLine is one physical INI source line.
	RoleIniPhysicalLine NodeRole = "IniPhysicalLine"
	// RoleIniLogicalLine is one logical INI record.
	RoleIniLogicalLine NodeRole = "IniLogicalLine"
	// RoleIniSection is one ordinary INI section occurrence.
	RoleIniSection NodeRole = "IniSection"
	// RoleIniDefaultSection is one Python ConfigParser default-section
	// occurrence.
	RoleIniDefaultSection NodeRole = "IniDefaultSection"
	// RoleIniEntry is one INI entry occurrence.
	RoleIniEntry NodeRole = "IniEntry"
	// RoleIniErrorLine is one recovered INI error line.
	RoleIniErrorLine NodeRole = "IniErrorLine"
	// RoleIniSyntaxPiece is one INI lossless syntax piece.
	RoleIniSyntaxPiece NodeRole = "IniSyntaxPiece"
	// RolePropertiesDocument is a complete Java Properties document.
	RolePropertiesDocument NodeRole = "PropertiesDocument"
	// RolePropertiesNaturalLine is one Java Properties natural source line.
	RolePropertiesNaturalLine NodeRole = "PropertiesNaturalLine"
	// RolePropertiesLogicalLine is one Java Properties logical line.
	RolePropertiesLogicalLine NodeRole = "PropertiesLogicalLine"
	// RolePropertiesProperty is one Java Properties property occurrence.
	RolePropertiesProperty NodeRole = "PropertiesProperty"
	// RolePropertiesComment is one Java Properties comment occurrence.
	RolePropertiesComment NodeRole = "PropertiesComment"
	// RolePropertiesEscape is one Java Properties escape occurrence.
	RolePropertiesEscape NodeRole = "PropertiesEscape"
	// RolePropertiesErrorLine is one recovered Java Properties error line.
	RolePropertiesErrorLine NodeRole = "PropertiesErrorLine"
	// RolePropertiesSyntaxPiece is one Java Properties lossless syntax
	// piece.
	RolePropertiesSyntaxPiece NodeRole = "PropertiesSyntaxPiece"
	// RoleXmlDocument is a complete XML document.
	RoleXmlDocument NodeRole = "XmlDocument"
	// RoleXmlDeclaration is an XML declaration.
	RoleXmlDeclaration NodeRole = "XmlDeclaration"
	// RoleXmlDoctype is an XML internal-only DOCTYPE occurrence.
	RoleXmlDoctype NodeRole = "XmlDoctype"
	// RoleXmlElement is an XML element occurrence.
	RoleXmlElement NodeRole = "XmlElement"
	// RoleXmlAttribute is an XML attribute association.
	RoleXmlAttribute NodeRole = "XmlAttribute"
	// RoleXmlNamespaceBinding is an XML namespace declaration association.
	RoleXmlNamespaceBinding NodeRole = "XmlNamespaceBinding"
	// RoleXmlText is an XML text occurrence.
	RoleXmlText NodeRole = "XmlText"
	// RoleXmlCdata is an XML CDATA occurrence.
	RoleXmlCdata NodeRole = "XmlCdata"
	// RoleXmlComment is an XML comment occurrence.
	RoleXmlComment NodeRole = "XmlComment"
	// RoleXmlProcessingInstruction is an XML processing instruction.
	RoleXmlProcessingInstruction NodeRole = "XmlProcessingInstruction"
	// RoleXmlEntityReference is an XML entity reference occurrence.
	RoleXmlEntityReference NodeRole = "XmlEntityReference"
	// RoleXmlErrorRegion is one recovered XML error region.
	RoleXmlErrorRegion NodeRole = "XmlErrorRegion"
	// RoleXmlSyntaxPiece is one XML lossless syntax piece.
	RoleXmlSyntaxPiece NodeRole = "XmlSyntaxPiece"
	// RolePlistDocument is a complete plist document (native-domain root
	// handle, RFC 0013 §8.1).
	RolePlistDocument NodeRole = "PlistDocument"
	// RolePlistDictEntry is one plist dictionary key/value association.
	RolePlistDictEntry NodeRole = "PlistDictEntry"
	// RolePlistKey is one plist string key identity.
	RolePlistKey NodeRole = "PlistKey"
	// RolePlistArrayElement is one plist array element association.
	RolePlistArrayElement NodeRole = "PlistArrayElement"
	// RolePlistValue is one native plist value node.
	RolePlistValue NodeRole = "PlistValue"
	// RolePlistSyntaxPiece is one plist XML lossless syntax piece.
	RolePlistSyntaxPiece NodeRole = "PlistSyntaxPiece"
	// RoleHclDocument is a complete HCL document (native-domain root
	// handle, RFC 0014 §7.1).
	RoleHclDocument NodeRole = "HclDocument"
	// RoleHclBody is one HCL body: an ordered container of attributes and
	// blocks.
	RoleHclBody NodeRole = "HclBody"
	// RoleHclAttribute is one HCL attribute occurrence.
	RoleHclAttribute NodeRole = "HclAttribute"
	// RoleHclBlock is one HCL block occurrence.
	RoleHclBlock NodeRole = "HclBlock"
	// RoleHclBlockLabel is one HCL block label with its quote/naked fact.
	RoleHclBlockLabel NodeRole = "HclBlockLabel"
	// RoleHclExpression is one HCL expression AST node.
	RoleHclExpression NodeRole = "HclExpression"
	// RoleHclTemplatePart is one ordered HCL template part.
	RoleHclTemplatePart NodeRole = "HclTemplatePart"
	// RoleHclErrorRegion is one recovered HCL error region.
	RoleHclErrorRegion NodeRole = "HclErrorRegion"
	// RoleHclSyntaxPiece is one HCL lossless syntax piece.
	RoleHclSyntaxPiece NodeRole = "HclSyntaxPiece"
)

// NodeRef is an opaque handle to one structural identity in exactly one
// snapshot (document lib.rs:253-292).
type NodeRef struct {
	snapshot SnapshotIdentity
	index    uint64
	role     NodeRole
}

// Snapshot returns the owning snapshot.
func (n NodeRef) Snapshot() SnapshotIdentity { return n.snapshot }

// Role returns the structural role.
func (n NodeRef) Role() NodeRole { return n.role }

// Index returns the process-local ordinal within the owning snapshot.
func (n NodeRef) Index() uint64 { return n.index }

// Span is a half-open byte range bound to one snapshot (document
// lib.rs:294-342). Start is inclusive; end is exclusive.
type Span struct {
	snapshot  SnapshotIdentity
	startByte int
	endByte   int
}

// Snapshot returns the owning snapshot.
func (s Span) Snapshot() SnapshotIdentity { return s.snapshot }

// StartByte returns the inclusive start byte.
func (s Span) StartByte() int { return s.startByte }

// EndByte returns the exclusive end byte.
func (s Span) EndByte() int { return s.endByte }

// Len returns the byte length.
func (s Span) Len() int { return s.endByte - s.startByte }

// IsEmpty reports whether the range is an insertion point.
func (s Span) IsEmpty() bool { return s.startByte == s.endByte }

// DocumentAuthority owns the snapshot-bound handle issuance of one
// document implementation (document lib.rs:53-110).
type DocumentAuthority struct {
	identity SnapshotIdentity
}

// NewDocumentAuthority allocates a fresh authority with its own snapshot
// identity.
func NewDocumentAuthority() DocumentAuthority {
	return DocumentAuthority{identity: allocateSnapshotIdentity()}
}

// Identity returns the snapshot identity.
func (a DocumentAuthority) Identity() SnapshotIdentity { return a.identity }

// NodeRef issues one opaque node handle with the given role.
func (a DocumentAuthority) NodeRef(index uint64, role NodeRole) NodeRef {
	return NodeRef{snapshot: a.identity, index: index, role: role}
}

// Span creates a snapshot-bound span after range validation.
func (a DocumentAuthority) Span(startByte, endByte int) (Span, error) {
	if startByte > endByte {
		return Span{}, &LocationError{Kind: LocationInvertedSpan}
	}
	return Span{snapshot: a.identity, startByte: startByte, endByte: endByte}, nil
}

// Verify reports whether a node handle belongs to this snapshot.
func (a DocumentAuthority) Verify(node NodeRef) error {
	if node.snapshot == a.identity {
		return nil
	}
	return &LocationError{Kind: LocationWrongSnapshot}
}

// ResolveIndex resolves an index only for the authority that issued the
// handle.
func (a DocumentAuthority) ResolveIndex(node NodeRef) (uint64, error) {
	if err := a.Verify(node); err != nil {
		return 0, err
	}
	return node.index, nil
}

// LocationErrorKind classifies a span, identity, or coverage failure
// (document lib.rs:581-612).
type LocationErrorKind uint8

// The stable location failure classes.
const (
	// LocationInvertedSpan: a span start followed its end.
	LocationInvertedSpan LocationErrorKind = iota
	// LocationWrongSnapshot: a handle or span belongs to another snapshot.
	LocationWrongSnapshot
	// LocationIncompleteStructuralCoverage: pieces had a gap, overlap,
	// empty interval, or wrong final length.
	LocationIncompleteStructuralCoverage
	// LocationOutOfBounds: a requested coordinate is beyond the source or
	// decoded text.
	LocationOutOfBounds
	// LocationNoDecodedText: binary sources do not have decoded
	// coordinates.
	LocationNoDecodedText
	// LocationNotDecodedBoundary: a raw offset lies inside one encoded
	// scalar.
	LocationNotDecodedBoundary
	// LocationDecodedOffsetNotBoundary: a decoded offset lies inside one
	// scalar's UTF-8 or UTF-16 representation.
	LocationDecodedOffsetNotBoundary
	// LocationWrongRole: a structural handle has a role other than the one
	// required by its index.
	LocationWrongRole
	// LocationInvalidBinaryRegionKind: a binary region kind is empty.
	LocationInvalidBinaryRegionKind
	// LocationDuplicateStructuralIdentity: more than one structural region
	// reused the same process-local identity.
	LocationDuplicateStructuralIdentity
)

// LocationError is a span, identity, or coverage failure. It implements
// error and the RFC 0016 §6 Code() contract; the generic registered
// invalid-input code applies because these failures are invalid location
// facts.
type LocationError struct {
	// Kind identifies the failure.
	Kind LocationErrorKind
}

// Error implements error.
func (e *LocationError) Error() string { return "document location: " + e.Name() }

// Code returns the registered invalid-input code (RFC 0016 §6).
func (e *LocationError) Code() string { return "core.protocol.invalid-value@1" }

// Name returns the stable failure name mirrored from the Rust
// LocationError variant (the source-v1 vectors compare these names).
func (e *LocationError) Name() string {
	switch e.Kind {
	case LocationInvertedSpan:
		return "InvertedSpan"
	case LocationWrongSnapshot:
		return "WrongSnapshot"
	case LocationIncompleteStructuralCoverage:
		return "IncompleteStructuralCoverage"
	case LocationOutOfBounds:
		return "OutOfBounds"
	case LocationNoDecodedText:
		return "NoDecodedText"
	case LocationNotDecodedBoundary:
		return "NotDecodedBoundary"
	case LocationDecodedOffsetNotBoundary:
		return "DecodedOffsetNotBoundary"
	case LocationWrongRole:
		return "WrongRole"
	case LocationInvalidBinaryRegionKind:
		return "InvalidBinaryRegionKind"
	case LocationDuplicateStructuralIdentity:
		return "DuplicateStructuralIdentity"
	}
	return fmt.Sprintf("LocationError(%d)", e.Kind)
}
