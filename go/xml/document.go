package xml

import (
	"strings"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the immutable namespace-aware native XML tree
// (RFC 0012 §4-7; consema-rs/crates/consema-xml/src/document.rs). The Document
// retains prolog order, one document element, epilog order, and every
// exact source span. An XML element is not a JSON Object: attributes and
// child elements are never merged into one map, mixed content keeps its
// source order, and namespace prefixes stay source spelling rather than
// expanded names.

// QNameFacts is one lexical QName with its source-derived facts
// (document.rs:96-110).
type QNameFacts struct {
	// Prefix is the original prefix spelling, when present.
	Prefix *string
	// Local is the local name.
	Local string
	// Span is the complete QName span.
	Span document.Span
	// PrefixSpan is the prefix span, when present.
	PrefixSpan *document.Span
	// LocalSpan is the local-name span.
	LocalSpan document.Span
}

// QName returns the lexical QName of these facts.
func (f *QNameFacts) QName() QName {
	return QName{Prefix: f.Prefix, Local: f.Local}
}

// ReferenceFragmentKind is the closed text or attribute-value fragment
// category (RFC 0012 §6).
type ReferenceFragmentKind uint8

// The four frozen fragment categories.
const (
	// ReferenceFragmentLiteral is literal character data.
	ReferenceFragmentLiteral ReferenceFragmentKind = iota
	// ReferenceFragmentCharacterReference is a decimal or hexadecimal
	// character reference.
	ReferenceFragmentCharacterReference
	// ReferenceFragmentPredefinedEntity is one of the five predefined entity
	// references.
	ReferenceFragmentPredefinedEntity
	// ReferenceFragmentGeneralEntity is an admitted internal general entity
	// reference.
	ReferenceFragmentGeneralEntity
)

// ReferenceFragment is one ordered text or attribute-value fragment
// (document.rs:135-172).
type ReferenceFragment struct {
	// Kind is the closed fragment category.
	Kind ReferenceFragmentKind
	// Span is the exact source span.
	Span document.Span
	// Text is the decoded literal text of Literal fragments.
	Text string
	// Resolved is the resolved XML character (CharacterReference), the
	// replacement character data (PredefinedEntity/GeneralEntity), or empty.
	Resolved string
	// ResolvedChar is the resolved character of CharacterReference
	// fragments.
	ResolvedChar rune
	// Name is the entity or reference name of PredefinedEntity/GeneralEntity
	// fragments.
	Name string
	// DeclarationSpan is the span of the declaring `<!ENTITY …>` of
	// GeneralEntity fragments.
	DeclarationSpan document.Span
}

// XmlNamespaceBindingData is one XML namespace declaration association
// (document.rs:174-187).
type XmlNamespaceBindingData struct {
	// Ordinal is the document-wide binding ordinal for stable identity.
	Ordinal uint64
	// Span is the `xmlns="…"` or `xmlns:p="…"` span.
	Span document.Span
	// Prefix is the bound prefix; nil is the default namespace.
	Prefix *string
	// URISpan is the namespace URI value span.
	URISpan document.Span
	// URI is the namespace URI.
	URI string
}

// XmlAttributeData is one XML attribute association (document.rs:189-211).
type XmlAttributeData struct {
	// Ordinal is the document-wide attribute ordinal for stable identity.
	Ordinal uint64
	// Span is the whole attribute span.
	Span document.Span
	// QName is the lexical QName facts.
	QName QNameFacts
	// Expanded is the resolved expanded name; nil when a namespace error
	// kept the name unprovable.
	Expanded *ExpandedName
	// NamespaceError is the namespace resolution failure, when the name
	// could not be proven.
	NamespaceError *NamespaceError
	// SingleQuote reports whether the value used single quotes.
	SingleQuote bool
	// ValueSpan is the exact value span between the quotes; empty for an
	// empty value.
	ValueSpan document.Span
	// Fragments are the ordered raw value fragments.
	Fragments []ReferenceFragment
	// NormalizedValue is the XML 1.0 CDATA-normalized semantic value.
	NormalizedValue string
}

// XmlTextData is one text occurrence with ordered fragments
// (document.rs:213-222).
type XmlTextData struct {
	// Ordinal is the document-wide text ordinal for stable identity.
	Ordinal uint64
	// Span is the exact source span.
	Span document.Span
	// Fragments are the ordered fragments; adjacent literals are not merged
	// across markup.
	Fragments []ReferenceFragment
}

// XmlCdataData is one CDATA occurrence (document.rs:224-235).
type XmlCdataData struct {
	// Ordinal is the document-wide ordinal for stable identity.
	Ordinal uint64
	// Span is the `![CDATA[…]]>` span.
	Span document.Span
	// TextSpan is the content text span.
	TextSpan document.Span
	// Text is the content text; never entity-expanded.
	Text string
}

// XmlCommentData is one comment occurrence (document.rs:237-248).
type XmlCommentData struct {
	// Ordinal is the document-wide ordinal for stable identity.
	Ordinal uint64
	// Span is the `<!--…-->` span.
	Span document.Span
	// TextSpan is the content text span.
	TextSpan document.Span
	// Text is the content text; never entity-expanded.
	Text string
}

// XmlPiData is one processing instruction (document.rs:250-263).
type XmlPiData struct {
	// Ordinal is the document-wide ordinal for stable identity.
	Ordinal uint64
	// Span is the `<?…?>` span.
	Span document.Span
	// TargetSpan is the target span.
	TargetSpan document.Span
	// Target is the target; cannot compare case-insensitively equal to
	// `xml`.
	Target string
	// Content is the content span and text, when present; never
	// entity-expanded.
	Content *XmlPiContent
}

// XmlPiContent is one PI content span/text pair.
type XmlPiContent struct {
	// Span is the content span.
	Span document.Span
	// Text is the content text.
	Text string
}

// XmlErrorRegionData is one recovered error region (document.rs:265-272).
type XmlErrorRegionData struct {
	// Ordinal is the document-wide ordinal for stable identity.
	Ordinal uint64
	// Span is the recovered error span.
	Span document.Span
}

// XmlElementData is one element occurrence (document.rs:274-296).
type XmlElementData struct {
	// Index is the arena index for stable identity.
	Index int
	// Span is the full start-tag span, or the whole empty-element span.
	Span document.Span
	// QName is the lexical QName facts.
	QName QNameFacts
	// Expanded is the resolved expanded name; nil when a namespace error
	// kept the name unprovable.
	Expanded *ExpandedName
	// NamespaceError is the namespace resolution failure, when the name
	// could not be proven.
	NamespaceError *NamespaceError
	// Scope is the immutable ancestry-derived in-scope namespace chain.
	Scope NamespaceScope
	// Namespaces are the ordered namespace declarations on this element.
	Namespaces []XmlNamespaceBindingData
	// Attributes are the ordered attributes, excluding namespace
	// declarations.
	Attributes []XmlAttributeData
	// Children are the ordered child content arena indices; never sorted by
	// type.
	Children []int
}

// XmlContentKind is the closed child content category (document.rs:299-313).
type XmlContentKind uint8

// The six frozen content categories.
const (
	// XmlContentElement is a child element.
	XmlContentElement XmlContentKind = iota
	// XmlContentText is a text occurrence.
	XmlContentText
	// XmlContentCdata is a CDATA occurrence.
	XmlContentCdata
	// XmlContentComment is a comment occurrence.
	XmlContentComment
	// XmlContentProcessingInstruction is a processing instruction.
	XmlContentProcessingInstruction
	// XmlContentErrorRegion is a recovered error region.
	XmlContentErrorRegion
)

// XmlContent is one child content occurrence (document.rs:299-313).
type XmlContent struct {
	// Kind is the closed content category.
	Kind XmlContentKind
	// Element is the element data of Element content.
	Element *XmlElementData
	// Text is the text data of Text content.
	Text *XmlTextData
	// Cdata is the CDATA data of Cdata content.
	Cdata *XmlCdataData
	// Comment is the comment data of Comment content.
	Comment *XmlCommentData
	// ProcessingInstruction is the PI data of ProcessingInstruction
	// content.
	ProcessingInstruction *XmlPiData
	// ErrorRegion is the error-region data of ErrorRegion content.
	ErrorRegion *XmlErrorRegionData
}

// Span returns the exact source span of this occurrence.
func (c *XmlContent) Span() document.Span {
	switch c.Kind {
	case XmlContentElement:
		return c.Element.Span
	case XmlContentText:
		return c.Text.Span
	case XmlContentCdata:
		return c.Cdata.Span
	case XmlContentComment:
		return c.Comment.Span
	case XmlContentProcessingInstruction:
		return c.ProcessingInstruction.Span
	default:
		return c.ErrorRegion.Span
	}
}

// XmlPrologItemKind is the closed prolog or epilog occurrence category
// (document.rs:330-345).
type XmlPrologItemKind uint8

// The six frozen prolog categories.
const (
	// XmlPrologItemDeclaration is the XML declaration, only in the prolog.
	XmlPrologItemDeclaration XmlPrologItemKind = iota
	// XmlPrologItemDoctype is a DOCTYPE occurrence, only in the prolog.
	XmlPrologItemDoctype
	// XmlPrologItemProcessingInstruction is a processing instruction.
	XmlPrologItemProcessingInstruction
	// XmlPrologItemComment is a comment.
	XmlPrologItemComment
	// XmlPrologItemBom is a byte-order mark trivia.
	XmlPrologItemBom
	// XmlPrologItemWhitespace is whitespace trivia.
	XmlPrologItemWhitespace
)

// XmlPrologItem is one prolog or epilog occurrence (document.rs:330-345).
type XmlPrologItem struct {
	// Kind is the closed prolog category.
	Kind XmlPrologItemKind
	// Declaration is the declaration data of Declaration items.
	Declaration *XmlDeclarationData
	// Doctype is the DOCTYPE data of Doctype items.
	Doctype *XmlDoctypeData
	// ProcessingInstruction is the PI data of ProcessingInstruction items.
	ProcessingInstruction *XmlPiData
	// Comment is the comment data of Comment items.
	Comment *XmlCommentData
	// Span is the exact source span of Bom and Whitespace trivia.
	Span document.Span
}

// XmlDeclarationData is the XML declaration facts (document.rs:347-360).
type XmlDeclarationData struct {
	// Span is the `<?xml …?>` span.
	Span document.Span
	// VersionSpan is the version pseudo-attribute span.
	VersionSpan document.Span
	// Version is the version; exactly `1.0`.
	Version string
	// Encoding is the optional encoding pseudo-attribute span and value.
	Encoding *XmlEncodingFact
	// Standalone is the optional standalone pseudo-attribute span and value.
	Standalone *XmlStandaloneFact
}

// XmlEncodingFact is one declaration encoding span/value pair.
type XmlEncodingFact struct {
	// Span is the encoding value span.
	Span document.Span
	// Value is the declared encoding value.
	Value string
}

// XmlStandaloneFact is one declaration standalone span/value pair.
type XmlStandaloneFact struct {
	// Span is the standalone value span.
	Span document.Span
	// Value is the declared standalone boolean.
	Value bool
}

// EntityDeclarationData is one admitted internal general entity
// declaration (document.rs:362-374).
type EntityDeclarationData struct {
	// Span is the `<!ENTITY …>` span.
	Span document.Span
	// Name is the entity name.
	Name string
	// ReplacementSpan is the replacement value span.
	ReplacementSpan document.Span
	// Replacement is the raw replacement text.
	Replacement string
}

// XmlDoctypeData is the DOCTYPE facts (document.rs:376-386).
type XmlDoctypeData struct {
	// Span is the `<!DOCTYPE …>` span.
	Span document.Span
	// Name is the root-name QName facts.
	Name QNameFacts
	// Entities are the ordered admitted internal general entity
	// declarations.
	Entities []EntityDeclarationData
	// Recovered reports whether an excluded external/validation construct
	// forced recovery.
	Recovered bool
}

// Document is an opaque immutable `xml.1.0-safe@1` document snapshot
// (consema-xml document.rs:388-406). Completed documents are logically
// immutable and safe for concurrent reads.
type Document struct {
	authority   document.DocumentAuthority
	source      *document.SourceSnapshot
	status      document.FormationStatus
	declaration *XmlDeclarationData
	doctype     *XmlDoctypeData
	prolog      []XmlPrologItem
	root        *int
	epilog      []XmlPrologItem
	syntax      *document.LosslessStructuralIndex
	syntaxKinds []XmlSyntaxKind
	diagnostics []*protocol.Diagnostic
	nodes       []XmlContent
	parentOf    []*int
	parseLimits XmlParseLimits
}

// SnapshotIdentity returns the snapshot identity to which every NodeRef
// and Span belongs.
func (d *Document) SnapshotIdentity() document.SnapshotIdentity {
	return d.authority.Identity()
}

// Source returns the exact immutable source snapshot.
func (d *Document) Source() *document.SourceSnapshot { return d.source }

// Render returns the exact current source bytes. The returned slice is a
// copy; the raw bytes are owned by the immutable source snapshot.
func (d *Document) Render() []byte { return d.source.Bytes() }

// FormationStatus returns whether recovery structure was required.
func (d *Document) FormationStatus() document.FormationStatus { return d.status }

// Diagnostics returns the deterministically ordered document diagnostics.
// The returned slice must not be modified.
func (d *Document) Diagnostics() []*protocol.Diagnostic { return d.diagnostics }

// LosslessStructuralIndex returns the exhaustive piece coverage.
func (d *Document) LosslessStructuralIndex() *document.LosslessStructuralIndex {
	return d.syntax
}

// LosslessSyntaxKinds returns the format-specific kind for every
// structural piece, in the same source order. The returned slice must not
// be modified.
func (d *Document) LosslessSyntaxKinds() []XmlSyntaxKind { return d.syntaxKinds }

// Declaration returns the XML declaration, when present.
func (d *Document) Declaration() *XmlDeclarationData { return d.declaration }

// Doctype returns the DOCTYPE occurrence, when present.
func (d *Document) Doctype() *XmlDoctypeData { return d.doctype }

// Prolog returns the ordered prolog items before the document element.
// The returned slice must not be modified.
func (d *Document) Prolog() []XmlPrologItem { return d.prolog }

// Epilog returns the ordered epilog items after the document element.
// The returned slice must not be modified.
func (d *Document) Epilog() []XmlPrologItem { return d.epilog }

// Root returns the one document element, when formation proved it.
func (d *Document) Root() *XmlElement { return d.elementHandle(d.root) }

// Nodes returns all arena nodes; child content of every element is
// reachable here. The returned slice must not be modified.
func (d *Document) Nodes() []XmlContent { return d.nodes }

// FormatFamily returns the XML format family contract.
func (d *Document) FormatFamily() document.FormatFamilyId {
	return document.NewFormatFamilyId("xml", 1)
}

// Profile returns the exact language profile.
func (d *Document) Profile() document.ProfileId {
	return document.NewProfileId("xml.1.0-safe", 1)
}

// NodeRef returns the snapshot-bound document handle.
func (d *Document) NodeRef() document.NodeRef {
	return d.authority.NodeRef(0, document.RoleXmlDocument)
}

// OccurrenceNodeRef returns the snapshot-bound identity of one
// ordinal-scoped occurrence.
func (d *Document) OccurrenceNodeRef(ordinal uint64, role document.NodeRole) document.NodeRef {
	return d.authority.NodeRef(ordinal, role)
}

// ParseLimits returns the parse limits this document was formed under.
func (d *Document) ParseLimits() XmlParseLimits { return d.parseLimits }

// elementHandle returns the borrowed element handle of one arena index.
func (d *Document) elementHandle(index *int) *XmlElement {
	if index == nil {
		return nil
	}
	return &XmlElement{owner: d, index: *index}
}

// contentItemHandle returns the borrowed content handle of one arena index.
func (d *Document) contentItemHandle(index int) *XmlContentItem {
	return &XmlContentItem{owner: d, index: index}
}

// parentOf returns the parent element arena index of one arena node; nil
// for the root element and for orphaned content.
func (d *Document) parentIndex(index int) *int {
	if index < len(d.parentOf) {
		return d.parentOf[index]
	}
	return nil
}

// nodeRef issues the typed node handle for one content arena index.
func (d *Document) nodeRef(index int, role document.NodeRole) document.NodeRef {
	return d.authority.NodeRef(uint64(index), role)
}

// XmlDocument is a snapshot-bound view of the whole document
// (document.rs:571-609).
type XmlDocument struct {
	owner *Document
}

// NewXmlDocument creates a view from the owned document.
func NewXmlDocument(owner *Document) XmlDocument {
	return XmlDocument{owner: owner}
}

// NodeRef returns the snapshot-bound document identity.
func (x XmlDocument) NodeRef() document.NodeRef { return x.owner.NodeRef() }

// Span returns the exact raw document span.
func (x XmlDocument) Span() document.Span {
	span, err := x.owner.authority.Span(0, x.owner.source.Len())
	if err != nil {
		span, _ = x.owner.authority.Span(0, 0)
	}
	return span
}

// Root returns the document element.
func (x XmlDocument) Root() *XmlElement { return x.owner.Root() }

// Status returns the formation status.
func (x XmlDocument) Status() document.FormationStatus { return x.owner.status }

// XmlElement is a snapshot-bound element handle (document.rs:612-679).
type XmlElement struct {
	owner *Document
	index int
}

// NodeRef returns the snapshot-bound stable identity.
func (e *XmlElement) NodeRef() document.NodeRef {
	return e.owner.nodeRef(e.index, document.RoleXmlElement)
}

// Span returns the full start-tag or empty-element span.
func (e *XmlElement) Span() document.Span { return e.data().Span }

// QName returns the lexical QName facts.
func (e *XmlElement) QName() *QNameFacts { return &e.data().QName }

// Expanded returns the resolved expanded name, when the namespace binding
// could be proven.
func (e *XmlElement) Expanded() *ExpandedName { return e.data().Expanded }

// NamespaceBindings returns the ordered namespace declarations on this
// element.
func (e *XmlElement) NamespaceBindings() []XmlNamespaceBindingData {
	return e.data().Namespaces
}

// Attributes returns the ordered attributes, excluding namespace
// declarations.
func (e *XmlElement) Attributes() []XmlAttributeData { return e.data().Attributes }

// Children returns the ordered child content occurrences; mixed-content
// order is retained.
func (e *XmlElement) Children() []*XmlContentItem {
	data := e.data()
	children := make([]*XmlContentItem, 0, len(data.Children))
	for _, index := range data.Children {
		children = append(children, e.owner.contentItemHandle(index))
	}
	return children
}

// IsEmpty reports whether the element has no child content.
func (e *XmlElement) IsEmpty() bool { return len(e.data().Children) == 0 }

// data returns the element arena data; the handle always points at element
// arena data.
func (e *XmlElement) data() *XmlElementData {
	content := &e.owner.nodes[e.index]
	if content.Kind != XmlContentElement {
		return nil
	}
	return content.Element
}

// XmlContentItem is one borrowed child content occurrence
// (document.rs:681-763).
type XmlContentItem struct {
	owner *Document
	index int
}

// NodeRef returns the snapshot-bound stable identity.
func (c *XmlContentItem) NodeRef() document.NodeRef {
	role := document.RoleXmlElement
	switch c.owner.nodes[c.index].Kind {
	case XmlContentText:
		role = document.RoleXmlText
	case XmlContentCdata:
		role = document.RoleXmlCdata
	case XmlContentComment:
		role = document.RoleXmlComment
	case XmlContentProcessingInstruction:
		role = document.RoleXmlProcessingInstruction
	case XmlContentErrorRegion:
		role = document.RoleXmlErrorRegion
	}
	return c.owner.nodeRef(c.index, role)
}

// Span returns the exact source span.
func (c *XmlContentItem) Span() document.Span { return c.owner.nodes[c.index].Span() }

// Element returns the element content, when this is an element occurrence.
func (c *XmlContentItem) Element() *XmlElement {
	if c.owner.nodes[c.index].Kind != XmlContentElement {
		return nil
	}
	return &XmlElement{owner: c.owner, index: c.index}
}

// Text returns the text occurrence data, when this is a text occurrence.
func (c *XmlContentItem) Text() *XmlTextData { return c.owner.nodes[c.index].Text }

// Cdata returns the CDATA occurrence data, when present.
func (c *XmlContentItem) Cdata() *XmlCdataData { return c.owner.nodes[c.index].Cdata }

// Comment returns the comment occurrence data, when present.
func (c *XmlContentItem) Comment() *XmlCommentData { return c.owner.nodes[c.index].Comment }

// ProcessingInstruction returns the processing-instruction data, when
// present.
func (c *XmlContentItem) ProcessingInstruction() *XmlPiData {
	return c.owner.nodes[c.index].ProcessingInstruction
}

// TextSemantic is the semantic concatenation of one text occurrence after
// XML line-end normalization to LF (RFC 0012 §6; document.rs:766-799).
func TextSemantic(text *XmlTextData) string {
	var builder strings.Builder
	for _, fragment := range text.Fragments {
		switch fragment.Kind {
		case ReferenceFragmentLiteral:
			pushNormalized(&builder, fragment.Text)
		case ReferenceFragmentCharacterReference:
			builder.WriteRune(fragment.ResolvedChar)
		case ReferenceFragmentPredefinedEntity, ReferenceFragmentGeneralEntity:
			pushNormalized(&builder, fragment.Resolved)
		}
	}
	return builder.String()
}

// pushNormalized applies XML 1.0 line-end normalization: CRLF and CR
// become LF.
func pushNormalized(out *strings.Builder, text string) {
	runes := []rune(text)
	for index := 0; index < len(runes); index++ {
		c := runes[index]
		if c == '\r' {
			out.WriteRune('\n')
			if index+1 < len(runes) && runes[index+1] == '\n' {
				index++
			}
			continue
		}
		out.WriteRune(c)
	}
}
