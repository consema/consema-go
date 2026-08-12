package hcl

import "consema.dev/consema/document"

// This file implements the native HCL semantic model (RFC 0014 §6): the
// schema-free HCL body tree. The root document owns one body; body items
// preserve source order; attribute and block identity are per-occurrence,
// never merged. The model contains syntax, never computed values — no
// variable binding, function table, template expansion, or iteration
// exists — and no application types exist (hard gate 2).

// HclDocument is the immutable source-bound HCL document: one frozen
// source and its root body (RFC 0014 §6).
//
// All spans of the body tree are half-open raw-byte ranges of the bound
// snapshot; the exact source text of every construct is derived from its
// span.
type HclDocument struct {
	source *document.SourceSnapshot
	body   *HclBody
}

// Body returns the root body; the same body container serves nested block
// bodies.
func (d *HclDocument) Body() *HclBody { return d.body }

// Source returns the frozen source snapshot.
func (d *HclDocument) Source() *document.SourceSnapshot { return d.source }

// HclBody is the ordered body item container (RFC 0014 §6).
//
// A body holds attributes and blocks interleaved in source order; the root
// body of a document and every nested block body share this container.
type HclBody struct {
	// Items are the ordered body items.
	items []HclBodyItem
	// startByte is the span start of the owning block for a nested body,
	// and 0 for the root body; it participates in the deterministic node
	// identity scheme.
	startByte int
}

// Items returns the ordered body items, interleaving attributes and blocks
// in source order. The returned slice must not be modified.
func (b *HclBody) Items() []HclBodyItem { return b.items }

// Len returns the number of body items.
func (b *HclBody) Len() int { return len(b.items) }

// IsEmpty reports whether the body has no items.
func (b *HclBody) IsEmpty() bool { return len(b.items) == 0 }

// HclBodyItem is one body item: an attribute or a block occurrence (RFC
// 0014 §4.2, §6).
//
// Identity is per-occurrence: an attribute and a block may share a name in
// one body, blocks of the same type and labels may repeat, and every
// occurrence keeps its own spans; nothing is merged or resolved.
type HclBodyItem struct {
	attribute *HclAttribute
	block     *HclBlock
}

// AsAttribute returns the attribute view, or nil for a block item.
func (i HclBodyItem) AsAttribute() *HclAttribute { return i.attribute }

// AsBlock returns the block view, or nil for an attribute item.
func (i HclBodyItem) AsBlock() *HclBlock { return i.block }

// HclAttribute is one attribute occurrence: name, equals sign, and
// expression (RFC 0014 §4.2, §6).
//
// The expression is a first-class native role with its own exact span; the
// attribute's full source range is the union of the name, equals, and
// expression spans.
type HclAttribute struct {
	name       string
	nameSpan   document.Span
	equalsSpan document.Span
	expression *HclExpression
}

// Name returns the attribute name; keyword spellings such as `true` are
// valid names (RFC 0014 §4.1).
func (a *HclAttribute) Name() string { return a.name }

// NameSpan returns the exact span of the name identifier.
func (a *HclAttribute) NameSpan() document.Span { return a.nameSpan }

// EqualsSpan returns the exact span of the `=` equals sign.
func (a *HclAttribute) EqualsSpan() document.Span { return a.equalsSpan }

// Expression returns the value expression, unevaluated (RFC 0014 §1).
func (a *HclAttribute) Expression() *HclExpression { return a.expression }

// HclBlock is one block occurrence: type, ordered labels, and nested body
// (RFC 0014 §4.2, §6).
//
// A one-line block is the same native shape with at most one attribute and
// no nested blocks. Keyword spellings are valid block types (RFC 0014
// §4.1), and blocks of the same type and labels may repeat with
// per-occurrence identity.
type HclBlock struct {
	blockType string
	labels    []HclBlockLabel
	body      *HclBody
	span      document.Span
}

// BlockType returns the block type identifier.
func (b *HclBlock) BlockType() string { return b.blockType }

// Labels returns the ordered labels; each carries its quote/naked fact.
// The returned slice must not be modified.
func (b *HclBlock) Labels() []HclBlockLabel { return b.labels }

// Body returns the nested body (empty for a one-line block or a block with
// no items).
func (b *HclBlock) Body() *HclBody { return b.body }

// Span returns the exact span of the whole block, from the type identifier
// through the closing brace.
func (b *HclBlock) Span() document.Span { return b.span }

// HclBlockLabel is one block label with its quote/naked fact (RFC 0014
// §4.2, §6).
//
// A label is either a naked identifier or a quoted literal string without
// interpolation; the Quoted fact and the exact span preserve the source
// form.
type HclBlockLabel struct {
	text   string
	span   document.Span
	quoted bool
}

// Text returns the label text; for a quoted label this is the content
// without the quote delimiters (escapes are decoded by the parser).
func (l *HclBlockLabel) Text() string { return l.text }

// Span returns the exact span, including the quote delimiters when quoted.
func (l *HclBlockLabel) Span() document.Span { return l.span }

// Quoted reports whether the label is a quoted literal string; false for a
// naked identifier (RFC 0014 §6).
func (l *HclBlockLabel) Quoted() bool { return l.quoted }

// HclErrorRegion is one recovered HCL error region with its stable
// diagnostic code (RFC 0014 §3, §7.2).
//
// Recovery regions are deterministic boundaries: an expression region ends
// at end of line (extended by unterminated brackets/parens/braces to a
// matching close or end of line, by unterminated strings to end of line,
// and by unterminated heredocs to end of file within the heredoc size
// limit). Every error region corresponds to a `hcl.parse.*@1` diagnostic.
type HclErrorRegion struct {
	span document.Span
	code string
}

// Span returns the exact recovered region span.
func (r *HclErrorRegion) Span() document.Span { return r.span }

// Code returns the stable `hcl.parse.*@1` diagnostic code of the region.
func (r *HclErrorRegion) Code() string { return r.code }
