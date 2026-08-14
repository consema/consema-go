package yaml

import (
	"fmt"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// YamlProfile is the frozen YAML language profile (consema-yaml lib.rs:
// 54-61). The unexported field makes the set closed.
type YamlProfile struct {
	name string
}

// Yaml12CoreV1 is YAML 1.2.2 presentation grammar with the Core schema.
var Yaml12CoreV1 = YamlProfile{name: "yaml.1.2-core"}

// Yaml11CompatV1 is the safe YAML 1.2-compatible presentation with frozen
// YAML 1.1 scalar resolution.
var Yaml11CompatV1 = YamlProfile{name: "yaml.1.1-compat"}

// ID returns the immutable profile identifier.
func (p YamlProfile) ID() document.ProfileId {
	return document.NewProfileId(p.name, 1)
}

// String returns the stable profile spelling.
func (p YamlProfile) String() string { return p.name }

// acceptedVersion is the exact %YAML directive version the profile accepts.
func (p YamlProfile) acceptedVersion() string {
	if p == Yaml11CompatV1 {
		return "1.1"
	}
	return "1.2"
}

// YamlSyntaxKind is the closed YAML lossless presentation-piece
// classification (consema-yaml lib.rs). The stable query and
// protocol spellings are the exact `as_str` names.
type YamlSyntaxKind string

// The 25 frozen YAML syntax kinds.
const (
	// SyntaxKindBom is a Unicode byte-order mark retained in the decoded
	// stream.
	SyntaxKindBom YamlSyntaxKind = "Bom"
	// SyntaxKindWhitespace is horizontal separation.
	SyntaxKindWhitespace YamlSyntaxKind = "Whitespace"
	// SyntaxKindNewline is LF, CRLF, or a bare CR line break.
	SyntaxKindNewline YamlSyntaxKind = "Newline"
	// SyntaxKindComment is a comment excluding its line break.
	SyntaxKindComment YamlSyntaxKind = "Comment"
	// SyntaxKindDirective is a `%YAML`, `%TAG`, or reserved directive line.
	SyntaxKindDirective YamlSyntaxKind = "Directive"
	// SyntaxKindDocumentStart is `---` at the start of a line.
	SyntaxKindDocumentStart YamlSyntaxKind = "DocumentStart"
	// SyntaxKindDocumentEnd is `...` at the start of a line.
	SyntaxKindDocumentEnd YamlSyntaxKind = "DocumentEnd"
	// SyntaxKindSequenceEntry is a block sequence `-` indicator.
	SyntaxKindSequenceEntry YamlSyntaxKind = "SequenceEntry"
	// SyntaxKindExplicitKey is an explicit mapping key `?` indicator.
	SyntaxKindExplicitKey YamlSyntaxKind = "ExplicitKey"
	// SyntaxKindMappingValue is a mapping value `:` indicator.
	SyntaxKindMappingValue YamlSyntaxKind = "MappingValue"
	// SyntaxKindFlowSequenceStart is `[`.
	SyntaxKindFlowSequenceStart YamlSyntaxKind = "FlowSequenceStart"
	// SyntaxKindFlowSequenceEnd is `]`.
	SyntaxKindFlowSequenceEnd YamlSyntaxKind = "FlowSequenceEnd"
	// SyntaxKindFlowMappingStart is `{`.
	SyntaxKindFlowMappingStart YamlSyntaxKind = "FlowMappingStart"
	// SyntaxKindFlowMappingEnd is `}`.
	SyntaxKindFlowMappingEnd YamlSyntaxKind = "FlowMappingEnd"
	// SyntaxKindFlowEntry is a flow `,` separator.
	SyntaxKindFlowEntry YamlSyntaxKind = "FlowEntry"
	// SyntaxKindAnchor is an anchor spelling beginning with `&`.
	SyntaxKindAnchor YamlSyntaxKind = "Anchor"
	// SyntaxKindAlias is an alias spelling beginning with `*`.
	SyntaxKindAlias YamlSyntaxKind = "Alias"
	// SyntaxKindTag is a tag spelling beginning with `!`.
	SyntaxKindTag YamlSyntaxKind = "Tag"
	// SyntaxKindPlainScalar is a plain scalar presentation fragment.
	SyntaxKindPlainScalar YamlSyntaxKind = "PlainScalar"
	// SyntaxKindSingleQuotedScalar is a complete single-quoted scalar
	// presentation.
	SyntaxKindSingleQuotedScalar YamlSyntaxKind = "SingleQuotedScalar"
	// SyntaxKindDoubleQuotedScalar is a complete double-quoted scalar
	// presentation.
	SyntaxKindDoubleQuotedScalar YamlSyntaxKind = "DoubleQuotedScalar"
	// SyntaxKindLiteralBlockHeader is a literal block-scalar header beginning
	// with `|`.
	SyntaxKindLiteralBlockHeader YamlSyntaxKind = "LiteralBlockHeader"
	// SyntaxKindFoldedBlockHeader is a folded block-scalar header beginning
	// with `>`.
	SyntaxKindFoldedBlockHeader YamlSyntaxKind = "FoldedBlockHeader"
	// SyntaxKindBlockScalarContent is the exact indented block-scalar content
	// region.
	SyntaxKindBlockScalarContent YamlSyntaxKind = "BlockScalarContent"
	// SyntaxKindErrorRegion is bytes retained after bounded syntax recovery.
	SyntaxKindErrorRegion YamlSyntaxKind = "ErrorRegion"
)

// AsStr returns the stable query and protocol name.
func (k YamlSyntaxKind) AsStr() string { return string(k) }

// YamlSyntaxKindFromName resolves one exact stable kind name.
func YamlSyntaxKindFromName(name string) (YamlSyntaxKind, bool) {
	switch name {
	case "Bom":
		return SyntaxKindBom, true
	case "Whitespace":
		return SyntaxKindWhitespace, true
	case "Newline":
		return SyntaxKindNewline, true
	case "Comment":
		return SyntaxKindComment, true
	case "Directive":
		return SyntaxKindDirective, true
	case "DocumentStart":
		return SyntaxKindDocumentStart, true
	case "DocumentEnd":
		return SyntaxKindDocumentEnd, true
	case "SequenceEntry":
		return SyntaxKindSequenceEntry, true
	case "ExplicitKey":
		return SyntaxKindExplicitKey, true
	case "MappingValue":
		return SyntaxKindMappingValue, true
	case "FlowSequenceStart":
		return SyntaxKindFlowSequenceStart, true
	case "FlowSequenceEnd":
		return SyntaxKindFlowSequenceEnd, true
	case "FlowMappingStart":
		return SyntaxKindFlowMappingStart, true
	case "FlowMappingEnd":
		return SyntaxKindFlowMappingEnd, true
	case "FlowEntry":
		return SyntaxKindFlowEntry, true
	case "Anchor":
		return SyntaxKindAnchor, true
	case "Alias":
		return SyntaxKindAlias, true
	case "Tag":
		return SyntaxKindTag, true
	case "PlainScalar":
		return SyntaxKindPlainScalar, true
	case "SingleQuotedScalar":
		return SyntaxKindSingleQuotedScalar, true
	case "DoubleQuotedScalar":
		return SyntaxKindDoubleQuotedScalar, true
	case "LiteralBlockHeader":
		return SyntaxKindLiteralBlockHeader, true
	case "FoldedBlockHeader":
		return SyntaxKindFoldedBlockHeader, true
	case "BlockScalarContent":
		return SyntaxKindBlockScalarContent, true
	case "ErrorRegion":
		return SyntaxKindErrorRegion, true
	}
	return "", false
}

// IsTrivia reports whether the kind is a trivia piece class.
func (k YamlSyntaxKind) IsTrivia() bool {
	switch k {
	case SyntaxKindBom, SyntaxKindWhitespace, SyntaxKindNewline, SyntaxKindComment:
		return true
	}
	return false
}

// YamlNodeKind is the YAML native representation node kind (consema-yaml
// lib.rs).
type YamlNodeKind string

// The three frozen native node kinds.
const (
	// NodeKindScalar is a tagged scalar.
	NodeKindScalar YamlNodeKind = "Scalar"
	// NodeKindSequence is an ordered sequence association.
	NodeKindSequence YamlNodeKind = "Sequence"
	// NodeKindMapping is an ordered arbitrary key/value association.
	NodeKindMapping YamlNodeKind = "Mapping"
)

// String returns the stable kind name.
func (k YamlNodeKind) String() string { return string(k) }

// YamlScalarStyle is the exact scalar presentation style (consema-yaml
// lib.rs).
type YamlScalarStyle uint8

// The five frozen scalar styles.
const (
	// ScalarStylePlain is plain style.
	ScalarStylePlain YamlScalarStyle = iota
	// ScalarStyleSingleQuoted is single-quoted style.
	ScalarStyleSingleQuoted
	// ScalarStyleDoubleQuoted is double-quoted style.
	ScalarStyleDoubleQuoted
	// ScalarStyleLiteral is literal block style.
	ScalarStyleLiteral
	// ScalarStyleFolded is folded block style.
	ScalarStyleFolded
)

// String returns the stable style name.
func (s YamlScalarStyle) String() string {
	switch s {
	case ScalarStylePlain:
		return "Plain"
	case ScalarStyleSingleQuoted:
		return "SingleQuoted"
	case ScalarStyleDoubleQuoted:
		return "DoubleQuoted"
	case ScalarStyleLiteral:
		return "Literal"
	case ScalarStyleFolded:
		return "Folded"
	}
	return fmt.Sprintf("YamlScalarStyle(%d)", uint8(s))
}

// YamlScalarKind is the resolved native scalar semantic category
// (consema-yaml lib.rs).
type YamlScalarKind uint8

// The nine frozen scalar categories.
const (
	// ScalarKindNull is null.
	ScalarKindNull YamlScalarKind = iota
	// ScalarKindBoolean is boolean.
	ScalarKindBoolean
	// ScalarKindInteger is an arbitrary-precision integer.
	ScalarKindInteger
	// ScalarKindFloat is an exact decimal or frozen non-finite float
	// spelling.
	ScalarKindFloat
	// ScalarKindString is string.
	ScalarKindString
	// ScalarKindTimestamp is a YAML 1.1-compatible timestamp.
	ScalarKindTimestamp
	// ScalarKindBinary is a validated YAML binary scalar.
	ScalarKindBinary
	// ScalarKindCustom is a scalar carrying an uninterpreted custom tag.
	ScalarKindCustom
	// ScalarKindTagged is a scalar carrying a retained standard tag without
	// a core tree lowering.
	ScalarKindTagged
)

// String returns the stable category name.
func (k YamlScalarKind) String() string {
	switch k {
	case ScalarKindNull:
		return "Null"
	case ScalarKindBoolean:
		return "Boolean"
	case ScalarKindInteger:
		return "Integer"
	case ScalarKindFloat:
		return "Float"
	case ScalarKindString:
		return "String"
	case ScalarKindTimestamp:
		return "Timestamp"
	case ScalarKindBinary:
		return "Binary"
	case ScalarKindCustom:
		return "Custom"
	case ScalarKindTagged:
		return "Tagged"
	}
	return fmt.Sprintf("YamlScalarKind(%d)", uint8(k))
}

// FormationFailure is the fatal formation failure before any complete
// Document exists (consema-document lib.rs). It implements error
// and the RFC 0016 §6 Code() contract with the first diagnostic's
// registered code.
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
		return "yaml: formation failed"
	}
	return "yaml: formation failed: " + f.diagnostics[0].Code
}

// Code returns the registered code of the first diagnostic.
func (f *FormationFailure) Code() string {
	if len(f.diagnostics) == 0 {
		return "yaml.parse.syntax@1"
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
		panic("yaml: unregistered formation code " + code)
	}
	return &FormationFailure{diagnostics: []*protocol.Diagnostic{diagnostic}}
}

// newSyntaxFailure builds the frozen yaml.parse.syntax@1 diagnostic at one
// decoded scalar offset.
func newSyntaxFailure(scalarOffset int) *FormationFailure {
	return newFormationFailure("yaml.parse.syntax@1", protocol.CategorySyntax,
		scalarOffset, scalarOffset, nil)
}

// newNativeFailure builds one frozen native composition failure
// (consema-yaml native.rs native_failure).
func newNativeFailure(code string) *FormationFailure {
	return newFormationFailure(code, protocol.CategorySemantic, -1, -1, nil)
}

// resourceLimitFailure builds the frozen core.parse.resource-limit@1
// diagnostic (consema-document lib.rs).
func resourceLimitFailure(name string, observed, limit int) *FormationFailure {
	return newFormationFailure("core.parse.resource-limit@1", protocol.CategoryResource,
		-1, -1, map[string]string{
			"limit":    fmt.Sprint(limit),
			"name":     name,
			"observed": fmt.Sprint(observed),
		})
}

// StructuralPieceKind is the lossless class of one structural piece
// (consema-document lib.rs).
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
// (document.StructuralPiece; consema-document lib.rs).
type StructuralPiece = document.StructuralPiece

// LosslessStructuralIndex is the exhaustive ordered token/trivia coverage
// of one source (document.LosslessStructuralIndex; consema-document
// lib.rs). The index validates exact byte coverage and snapshot
// binding at construction.
type LosslessStructuralIndex = document.LosslessStructuralIndex

// NewLosslessStructuralIndex validates exact raw-byte coverage of the
// source and snapshot binding (consema-document lib.rs).
func NewLosslessStructuralIndex(identity document.SnapshotIdentity, sourceLen int,
	pieces []StructuralPiece) (*LosslessStructuralIndex, error) {
	return document.NewLosslessStructuralIndex(identity, sourceLen, pieces)
}

// Document is the immutable exact-source YAML stream snapshot (consema-yaml
// lib.rs). Completed documents are logically immutable; concurrent
// reads are safe.
type Document struct {
	authority document.DocumentAuthority
	source    *document.SourceSnapshot
	profile   YamlProfile
	index     *LosslessStructuralIndex
	kinds     []YamlSyntaxKind
	native    nativeStream
	documents int
	limits    document.ParseLimits
}

// SnapshotIdentity is the snapshot identity to which every native handle
// and span belongs.
func (d *Document) SnapshotIdentity() document.SnapshotIdentity {
	return d.authority.Identity()
}

// Source returns the exact immutable source snapshot.
func (d *Document) Source() *document.SourceSnapshot { return d.source }

// Render returns the default rendering, byte-for-byte identical to the
// source (including the BOM and the original newline encoding).
func (d *Document) Render() []byte { return d.source.Bytes() }

// FormatFamily returns the YAML format family contract.
func (d *Document) FormatFamily() document.FormatFamilyId {
	return document.NewFormatFamilyId("yaml", 1)
}

// Profile returns the exact selected language profile.
func (d *Document) Profile() document.ProfileId { return d.profile.ID() }

// FormationStatus returns the formation state. YAML 0.7 forms only
// complete valid streams; syntax or composition failures return no
// Document.
func (d *Document) FormationStatus() document.FormationStatus {
	return document.FormationStatusComplete
}

// Diagnostics returns the deterministically ordered non-fatal diagnostics;
// complete YAML streams always carry none.
func (d *Document) Diagnostics() []*protocol.Diagnostic { return nil }

// LosslessStructuralIndex returns the exhaustive token/trivia byte
// coverage.
func (d *Document) LosslessStructuralIndex() *LosslessStructuralIndex {
	return d.index
}

// LosslessSyntaxKinds returns the format-specific kind for every
// structural piece, in the same source order.
func (d *Document) LosslessSyntaxKinds() []YamlSyntaxKind {
	return append([]YamlSyntaxKind(nil), d.kinds...)
}

// ParseLimits returns the resource contract used to form this snapshot and
// any edit successor.
func (d *Document) ParseLimits() document.ParseLimits { return d.limits }

// StreamNodeRef returns the snapshot-bound identity of the complete
// serialization stream.
func (d *Document) StreamNodeRef() document.NodeRef {
	return d.authority.NodeRef(0, document.RoleYamlStream)
}

// StreamSpan returns the exact raw span of the complete serialization
// stream.
func (d *Document) StreamSpan() document.Span {
	span, err := d.authority.Span(0, d.source.Len())
	if err != nil {
		panic("yaml: source length is an ordered span")
	}
	return span
}

// DocumentCount returns the number of independent YAML documents in this
// stream.
func (d *Document) DocumentCount() int { return d.documents }

// Document returns one independent YAML document by stream ordinal.
func (d *Document) Document(ordinal int) (YamlDocument, bool) {
	if ordinal < 0 || ordinal >= len(d.native.documents) {
		return YamlDocument{}, false
	}
	return YamlDocument{owner: d, ordinal: ordinal, document: &d.native.documents[ordinal]}, true
}

// AliasCount returns the number of alias serialization occurrences;
// aliases are never expanded.
func (d *Document) AliasCount() int { return len(d.native.aliases) }

// Alias returns one alias occurrence in serialization order.
func (d *Document) Alias(ordinal int) (YamlAlias, bool) {
	if ordinal < 0 || ordinal >= len(d.native.aliases) {
		return YamlAlias{}, false
	}
	return YamlAlias{owner: d, alias: &d.native.aliases[ordinal]}, true
}

// nodeRef issues one node identity for a native node index.
func (d *Document) nodeRef(index int) document.NodeRef {
	return d.authority.NodeRef(uint64(index), document.RoleYamlNode)
}

// YamlDocument is one independent document in a YAML stream (consema-yaml
// lib.rs).
type YamlDocument struct {
	owner    *Document
	ordinal  int
	document *nativeDocument
}

// Ordinal returns the zero-based stream ordinal.
func (d YamlDocument) Ordinal() int { return d.ordinal }

// NodeRef returns the snapshot-bound document identity.
func (d YamlDocument) NodeRef() document.NodeRef {
	return d.owner.authority.NodeRef(uint64(d.ordinal), document.RoleYamlDocument)
}

// Span returns the backend-validated raw document presentation span.
func (d YamlDocument) Span() document.Span { return d.document.span }

// Root returns the representation root. Alias occurrences already share
// target identity.
func (d YamlDocument) Root() YamlNode {
	return YamlNode{owner: d.owner, index: d.document.root}
}

// YamlNode is the snapshot-bound YAML representation node (consema-yaml
// lib.rs).
type YamlNode struct {
	owner *Document
	index int
}

// NodeRef returns the process-local stable identity within this snapshot.
func (n YamlNode) NodeRef() document.NodeRef { return n.owner.nodeRef(n.index) }

// Span returns the exact raw representation occurrence span.
func (n YamlNode) Span() document.Span { return n.owner.native.nodes[n.index].span }

// Tag returns the resolved tag identifier.
func (n YamlNode) Tag() string { return n.owner.native.nodes[n.index].tag }

// Anchor returns the exact anchor name on the defining occurrence, when
// present.
func (n YamlNode) Anchor() (string, bool) {
	node := &n.owner.native.nodes[n.index]
	if !node.hasAnchor {
		return "", false
	}
	return node.anchor, true
}

// AnchorNodeRef returns the snapshot-bound anchor-definition identity, when
// this node defines one.
func (n YamlNode) AnchorNodeRef() (document.NodeRef, bool) {
	node := &n.owner.native.nodes[n.index]
	if !node.hasAnchor {
		return document.NodeRef{}, false
	}
	return n.owner.authority.NodeRef(uint64(n.index), document.RoleYamlAnchorDefinition), true
}

// AnchorSpan returns the exact raw `&name` span, when this node defines an
// anchor.
func (n YamlNode) AnchorSpan() (document.Span, bool) {
	node := &n.owner.native.nodes[n.index]
	if !node.hasAnchor {
		return document.Span{}, false
	}
	return node.anchorSpan, true
}

// Kind returns the native node kind.
func (n YamlNode) Kind() YamlNodeKind {
	switch n.owner.native.nodes[n.index].content.kind {
	case contentScalar:
		return NodeKindScalar
	case contentSequence:
		return NodeKindSequence
	default:
		return NodeKindMapping
	}
}

// Scalar returns the scalar facts, when this is a scalar node.
func (n YamlNode) Scalar() (YamlScalar, bool) {
	content := &n.owner.native.nodes[n.index].content
	if content.kind != contentScalar {
		return YamlScalar{}, false
	}
	return YamlScalar{scalar: &content.scalar}, true
}

// SequenceLen returns the ordered sequence association count, when this is
// a sequence node.
func (n YamlNode) SequenceLen() (int, bool) {
	content := &n.owner.native.nodes[n.index].content
	if content.kind != contentSequence {
		return 0, false
	}
	return len(content.items), true
}

// SequenceItem returns one exact sequence association, when this is a
// sequence node.
func (n YamlNode) SequenceItem(ordinal int) (YamlSequenceItem, bool) {
	content := &n.owner.native.nodes[n.index].content
	if content.kind != contentSequence || ordinal < 0 || ordinal >= len(content.items) {
		return YamlSequenceItem{}, false
	}
	return YamlSequenceItem{owner: n.owner, item: &content.items[ordinal]}, true
}

// MappingLen returns the ordered mapping association count, when this is a
// mapping node.
func (n YamlNode) MappingLen() (int, bool) {
	content := &n.owner.native.nodes[n.index].content
	if content.kind != contentMapping {
		return 0, false
	}
	return len(content.entries), true
}

// MappingEntry returns one exact arbitrary key/value association, when this
// is a mapping node.
func (n YamlNode) MappingEntry(ordinal int) (YamlMappingEntry, bool) {
	content := &n.owner.native.nodes[n.index].content
	if content.kind != contentMapping || ordinal < 0 || ordinal >= len(content.entries) {
		return YamlMappingEntry{}, false
	}
	return YamlMappingEntry{owner: n.owner, entry: &content.entries[ordinal]}, true
}

// YamlScalar carries the native scalar facts with exact decoded and
// canonical content (consema-yaml lib.rs).
type YamlScalar struct {
	scalar *nativeScalar
}

// Decoded returns the decoded YAML scalar content before schema
// canonicalization.
func (s YamlScalar) Decoded() string { return s.scalar.decoded }

// Canonical returns the profile-defined canonical scalar content.
func (s YamlScalar) Canonical() string { return s.scalar.canonical }

// Kind returns the resolved scalar category.
func (s YamlScalar) Kind() YamlScalarKind { return s.scalar.kind }

// Style returns the source presentation style.
func (s YamlScalar) Style() YamlScalarStyle { return s.scalar.style }

// YamlSequenceItem is one ordered sequence association (consema-yaml
// lib.rs).
type YamlSequenceItem struct {
	owner *Document
	item  *nativeSequenceItem
}

// NodeRef returns the snapshot-bound association identity.
func (i YamlSequenceItem) NodeRef() document.NodeRef {
	return i.owner.authority.NodeRef(i.item.identity, document.RoleYamlSequenceElement)
}

// Span returns the exact raw element occurrence span, including an alias
// spelling when used.
func (i YamlSequenceItem) Span() document.Span { return i.item.span }

// Node returns the referenced representation node.
func (i YamlSequenceItem) Node() YamlNode {
	return YamlNode{owner: i.owner, index: i.item.node}
}

// Alias returns the alias occurrence that supplied this element edge, when
// present.
func (i YamlSequenceItem) Alias() (YamlAlias, bool) {
	if i.item.alias == nil {
		return YamlAlias{}, false
	}
	return YamlAlias{owner: i.owner, alias: &i.owner.native.aliases[*i.item.alias]}, true
}

// YamlMappingEntry is one ordered YAML mapping association with an
// arbitrary key node (consema-yaml lib.rs).
type YamlMappingEntry struct {
	owner *Document
	entry *nativeMappingEntry
}

// NodeRef returns the snapshot-bound association identity.
func (e YamlMappingEntry) NodeRef() document.NodeRef {
	return e.owner.authority.NodeRef(e.entry.identity, document.RoleYamlMappingEntry)
}

// Span returns the raw span from the key occurrence through the value
// occurrence.
func (e YamlMappingEntry) Span() document.Span { return e.entry.span }

// Key returns the arbitrary key node.
func (e YamlMappingEntry) Key() YamlNode {
	return YamlNode{owner: e.owner, index: e.entry.key}
}

// Value returns the value node.
func (e YamlMappingEntry) Value() YamlNode {
	return YamlNode{owner: e.owner, index: e.entry.value}
}

// KeyAlias returns the alias occurrence that supplied the key edge, when
// present.
func (e YamlMappingEntry) KeyAlias() (YamlAlias, bool) {
	if e.entry.keyAlias == nil {
		return YamlAlias{}, false
	}
	return YamlAlias{owner: e.owner, alias: &e.owner.native.aliases[*e.entry.keyAlias]}, true
}

// ValueAlias returns the alias occurrence that supplied the value edge,
// when present.
func (e YamlMappingEntry) ValueAlias() (YamlAlias, bool) {
	if e.entry.valueAlias == nil {
		return YamlAlias{}, false
	}
	return YamlAlias{owner: e.owner, alias: &e.owner.native.aliases[*e.entry.valueAlias]}, true
}

// YamlAlias is one alias serialization occurrence pointing at an existing
// representation node (consema-yaml lib.rs).
type YamlAlias struct {
	owner *Document
	alias *nativeAlias
}

// NodeRef returns the snapshot-bound occurrence identity.
func (a YamlAlias) NodeRef() document.NodeRef {
	return a.owner.authority.NodeRef(a.alias.identity, document.RoleYamlAlias)
}

// Span returns the exact raw `*name` occurrence span.
func (a YamlAlias) Span() document.Span { return a.alias.span }

// Name returns the exact alias name without `*`.
func (a YamlAlias) Name() string { return a.alias.name }

// Target returns the shared target representation node; no expansion
// occurs.
func (a YamlAlias) Target() YamlNode {
	return YamlNode{owner: a.owner, index: a.alias.target}
}
