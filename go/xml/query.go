package xml

// This file implements XML native and lossless syntax query execution
// (RFC 0012 §8; consema-rs/crates/consema-xml/src/query.rs). Native order is document
// order. Element attributes and namespace declarations preserve their
// respective source orders; child content preserves mixed-content order.
// Descendant traversal is bounded pre-order. No query resolves a URI,
// evaluates XPath, loads a schema, or expands application data.

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// XmlMatchKind is the closed XML native match category.
type XmlMatchKind uint8

// The thirteen frozen native match categories (query.rs:31-165).
const (
	// XmlMatchDocument is the complete XML document.
	XmlMatchDocument XmlMatchKind = iota
	// XmlMatchDeclaration is the XML declaration.
	XmlMatchDeclaration
	// XmlMatchDoctype is the DOCTYPE occurrence.
	XmlMatchDoctype
	// XmlMatchPrologItem is one prolog or epilog occurrence.
	XmlMatchPrologItem
	// XmlMatchElement is one element occurrence.
	XmlMatchElement
	// XmlMatchAttribute is one attribute association.
	XmlMatchAttribute
	// XmlMatchNamespaceBinding is one namespace binding association.
	XmlMatchNamespaceBinding
	// XmlMatchText is one text occurrence.
	XmlMatchText
	// XmlMatchCdata is one CDATA occurrence.
	XmlMatchCdata
	// XmlMatchComment is one comment occurrence.
	XmlMatchComment
	// XmlMatchProcessingInstruction is one processing instruction.
	XmlMatchProcessingInstruction
	// XmlMatchReference is one reference occurrence inside text.
	XmlMatchReference
	// XmlMatchErrorRegion is one recovered error region.
	XmlMatchErrorRegion
)

// XmlReferenceKind is one XML reference occurrence kind (query.rs:20-29).
type XmlReferenceKind uint8

// The three frozen reference kinds.
const (
	// XmlReferenceKindCharacter is a decimal or hexadecimal character
	// reference.
	XmlReferenceKindCharacter XmlReferenceKind = iota
	// XmlReferenceKindPredefined is one of the five predefined entity
	// references.
	XmlReferenceKindPredefined
	// XmlReferenceKindGeneral is an admitted internal general entity
	// reference.
	XmlReferenceKindGeneral
)

// String returns the stable reference-kind name.
func (k XmlReferenceKind) String() string {
	switch k {
	case XmlReferenceKindCharacter:
		return "Character"
	case XmlReferenceKindPredefined:
		return "Predefined"
	case XmlReferenceKindGeneral:
		return "General"
	}
	return "Character"
}

// XmlMatch is one snapshot-bound XML native semantic query match
// (query.rs:31-165). Only the fields of the declared Kind are meaningful.
type XmlMatch struct {
	// Kind is the closed match category.
	Kind XmlMatchKind
	// Node is the exact match identity.
	Node document.NodeRef
	// Parent is the owning element identity, when present.
	Parent *document.NodeRef
	// Prefix is the original prefix spelling, when present.
	Prefix *string
	// Local is the local name (Element, Attribute, Doctype name).
	Local string
	// Namespace is the resolved namespace URI, when provable.
	Namespace *string
	// NamespaceError reports whether a namespace error kept the name
	// unprovable (Element).
	NamespaceError bool
	// Version is the declared version (Declaration).
	Version string
	// Encoding is the declared encoding, when present (Declaration).
	Encoding *string
	// Standalone is the declared standalone, when present (Declaration).
	Standalone *bool
	// KindText is the prolog-item kind: `processing-instruction` or
	// `comment` (PrologItem).
	KindText string
	// Element is the owning element (Attribute, NamespaceBinding).
	Element document.NodeRef
	// Value is the CDATA-normalized semantic attribute value (Attribute).
	Value string
	// URI is the namespace URI (NamespaceBinding).
	URI string
	// Semantic is the line-end-normalized semantic content (Text).
	Semantic string
	// Text is the content text (Cdata, Comment).
	Text string
	// Target is the PI target (ProcessingInstruction).
	Target string
	// Content is the PI content, when present (ProcessingInstruction).
	Content *string
	// TextNode is the owning text occurrence (Reference).
	TextNode document.NodeRef
	// ReferenceKind is the reference kind (Reference).
	ReferenceKind XmlReferenceKind
	// Name is the entity or reference name (Reference).
	Name string
	// Resolved is the fully resolved character data (Reference).
	Resolved string
	// Span is the exact recovered span (ErrorRegion).
	Span document.Span
}

// Identity returns the exact match identity (query.rs:167-185).
func (m *XmlMatch) Identity() document.NodeRef { return m.Node }

// XmlSyntaxMatch is one snapshot-bound XML lossless syntax query match
// (query.rs:187-220).
type XmlSyntaxMatch struct {
	node    document.NodeRef
	span    document.Span
	kind    XmlSyntaxKind
	ordinal int
}

// NodeRef returns the process-local syntax-piece identity.
func (m *XmlSyntaxMatch) NodeRef() document.NodeRef { return m.node }

// Span returns the exact raw source span.
func (m *XmlSyntaxMatch) Span() document.Span { return m.span }

// Kind returns the format-specific lossless kind.
func (m *XmlSyntaxMatch) Kind() XmlSyntaxKind { return m.kind }

// Ordinal returns the zero-based source-order position.
func (m *XmlSyntaxMatch) Ordinal() int { return m.ordinal }

// ExecuteXMLQuery executes a validated XML native semantic query against
// one immutable snapshot (query.rs:222-269). ctx carries cancellation
// only.
func ExecuteXMLQuery(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) ([]*XmlMatch, *protocol.QueryFailure) {
	domain := executable.Definition().Domain()
	if domain.ID() != "xml.native-semantic-query" || domain.Version() != 1 {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	context := &queryContext{document: doc, limits: limits}
	if failure := context.step(ctx, 1); failure != nil {
		return nil, failure
	}
	node := doc.NodeRef()
	input := []*XmlMatch{{Kind: XmlMatchDocument, Node: node}}
	matches, failure := executeExpression(ctx, executable.Definition().Expression(), input, context)
	if failure != nil {
		return nil, failure
	}
	return applySelection(matches, executable.Definition().Selection())
}

// ExecuteXMLQueryCursor executes a validated XML native query and exposes
// the complete ordered result through a cancellable cursor.
func ExecuteXMLQueryCursor(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) (*XmlQueryCursor, *protocol.QueryFailure) {
	matches, failure := ExecuteXMLQuery(ctx, executable, doc, limits)
	if failure != nil {
		return nil, failure
	}
	return &XmlQueryCursor{ctx: ctx, matches: matches}, nil
}

// XmlQueryCursor is the ordered native query result cursor.
type XmlQueryCursor struct {
	ctx     context.Context
	matches []*XmlMatch
}

// NextMatch yields the next match in source order. After exhaustion it
// returns nil; a cancellation after buffering returns a Cancelled
// failure.
func (c *XmlQueryCursor) NextMatch() (*XmlMatch, *protocol.QueryFailure) {
	if len(c.matches) == 0 {
		if c.ctx != nil && c.ctx.Err() != nil {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureCancelled}
		}
		return nil, nil
	}
	match := c.matches[0]
	c.matches = c.matches[1:]
	return match, nil
}

// ExecuteXMLSyntaxQuery executes a validated XML lossless syntax query
// against every source piece in raw order (query.rs:285-355).
func ExecuteXMLSyntaxQuery(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) ([]*XmlSyntaxMatch, *protocol.QueryFailure) {
	domain := executable.Definition().Domain()
	if domain.ID() != "xml.lossless-syntax-query" || domain.Version() != 1 {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	if doc.syntax == nil {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	context := &queryContext{document: doc, limits: limits}
	pieces := doc.syntax.Pieces()
	if failure := context.step(ctx, len(pieces)); failure != nil {
		return nil, failure
	}
	input := make([]*XmlSyntaxMatch, 0, len(pieces))
	for ordinal, piece := range pieces {
		input = append(input, &XmlSyntaxMatch{
			node:    doc.authority.NodeRef(uint64(ordinal), document.RoleXmlSyntaxPiece),
			span:    piece.Span(),
			kind:    doc.syntaxKinds[ordinal],
			ordinal: ordinal,
		})
	}
	matches, failure := executeSyntaxExpression(ctx, executable.Definition().Expression(), input, context)
	if failure != nil {
		return nil, failure
	}
	return applySyntaxSelection(matches, executable.Definition().Selection())
}

// ExecuteXMLSyntaxQueryCursor executes a validated XML syntax query and
// exposes the complete ordered result through a cancellable cursor.
func ExecuteXMLSyntaxQueryCursor(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) (*XmlSyntaxQueryCursor, *protocol.QueryFailure) {
	matches, failure := ExecuteXMLSyntaxQuery(ctx, executable, doc, limits)
	if failure != nil {
		return nil, failure
	}
	return &XmlSyntaxQueryCursor{ctx: ctx, matches: matches}, nil
}

// XmlSyntaxQueryCursor is the ordered syntax query result cursor.
type XmlSyntaxQueryCursor struct {
	ctx     context.Context
	matches []*XmlSyntaxMatch
}

// NextMatch yields the next match in source order; nil after exhaustion.
func (c *XmlSyntaxQueryCursor) NextMatch() (*XmlSyntaxMatch, *protocol.QueryFailure) {
	if len(c.matches) == 0 {
		if c.ctx != nil && c.ctx.Err() != nil {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureCancelled}
		}
		return nil, nil
	}
	match := c.matches[0]
	c.matches = c.matches[1:]
	return match, nil
}

// queryContext is the execution state of one query (query.rs:371-482).
type queryContext struct {
	document *Document
	limits   protocol.QueryLimits
	steps    int
}

// step applies the step and result budgets and the cancellation check.
func (c *queryContext) step(ctx context.Context, results int) *protocol.QueryFailure {
	if ctx != nil && ctx.Err() != nil {
		return &protocol.QueryFailure{Kind: protocol.FailureCancelled}
	}
	c.steps++
	if c.steps > c.limits.MaxSteps || results > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	return nil
}

// push appends one match under the result budget.
func (c *queryContext) push(output *[]*XmlMatch, value *XmlMatch) *protocol.QueryFailure {
	if len(*output)+1 > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	*output = append(*output, value)
	return nil
}

// appendValues appends matches under the result budget.
func (c *queryContext) appendValues(output *[]*XmlMatch, values []*XmlMatch) *protocol.QueryFailure {
	if len(*output)+len(values) > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	*output = append(*output, values...)
	return nil
}

// elementData returns one element's arena data; the index always points at
// element arena data.
func (c *queryContext) elementData(index int) *XmlElementData {
	return c.document.nodes[index].Element
}

// elementMatch builds the Element match for one arena index.
func (c *queryContext) elementMatch(index int) *XmlMatch {
	data := c.elementData(index)
	return &XmlMatch{
		Kind:           XmlMatchElement,
		Node:           c.nodeRef(index, document.RoleXmlElement),
		Parent:         c.parentOf(index),
		Prefix:         data.QName.Prefix,
		Local:          data.QName.Local,
		Namespace:      expandedNamespace(data.Expanded),
		NamespaceError: data.NamespaceError != nil,
	}
}

// parentOf returns the parent element handle of one arena index.
func (c *queryContext) parentOf(index int) *document.NodeRef {
	parent := c.document.parentIndex(index)
	if parent == nil {
		return nil
	}
	node := c.nodeRef(*parent, document.RoleXmlElement)
	return &node
}

// nodeRef issues the typed node handle for one arena index.
func (c *queryContext) nodeRef(index int, role document.NodeRole) document.NodeRef {
	return c.document.authority.NodeRef(uint64(index), role)
}

// prologItem builds the PrologItem match of one prolog/epilog occurrence.
func (c *queryContext) prologItem(item *XmlPrologItem) *XmlMatch {
	var node document.NodeRef
	kind := ""
	switch item.Kind {
	case XmlPrologItemProcessingInstruction:
		node = c.nodeRef(int(item.ProcessingInstruction.Ordinal), document.RoleXmlProcessingInstruction)
		kind = "processing-instruction"
	case XmlPrologItemComment:
		node = c.nodeRef(int(item.Comment.Ordinal), document.RoleXmlComment)
		kind = "comment"
	default:
		return nil
	}
	return &XmlMatch{Kind: XmlMatchPrologItem, Node: node, KindText: kind}
}

// executeExpression evaluates one native expression (query.rs:484-518).
func executeExpression(ctx context.Context, expression *protocol.QueryExpression,
	input []*XmlMatch, context *queryContext) ([]*XmlMatch, *protocol.QueryFailure) {
	switch expression.Kind {
	case protocol.ExpressionInput:
		return input, nil
	case protocol.ExpressionApply:
		values, failure := executeExpression(ctx, expression.Input, input, context)
		if failure != nil {
			return nil, failure
		}
		return applyOperator(ctx, expression.Operator, values, context)
	case protocol.ExpressionConcat:
		var output []*XmlMatch
		for _, branch := range expression.Branches {
			values, failure := executeExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			if failure := context.appendValues(&output, values); failure != nil {
				return nil, failure
			}
			if failure := context.step(ctx, len(output)); failure != nil {
				return nil, failure
			}
		}
		return output, nil
	case protocol.ExpressionStructureOrderMerge:
		var output []*XmlMatch
		for _, branch := range expression.Branches {
			values, failure := executeExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			if failure := context.appendValues(&output, values); failure != nil {
				return nil, failure
			}
		}
		sort.SliceStable(output, func(i, j int) bool {
			return matchSourceOrder(output[i]) < matchSourceOrder(output[j])
		})
		if failure := context.step(ctx, len(output)); failure != nil {
			return nil, failure
		}
		return output, nil
	}
	return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
}

// executeSyntaxExpression evaluates one syntax expression (query.rs:520-554).
func executeSyntaxExpression(ctx context.Context, expression *protocol.QueryExpression,
	input []*XmlSyntaxMatch, context *queryContext) ([]*XmlSyntaxMatch, *protocol.QueryFailure) {
	switch expression.Kind {
	case protocol.ExpressionInput:
		return input, nil
	case protocol.ExpressionApply:
		values, failure := executeSyntaxExpression(ctx, expression.Input, input, context)
		if failure != nil {
			return nil, failure
		}
		return applySyntaxOperator(ctx, expression.Operator, values, context)
	case protocol.ExpressionConcat:
		var output []*XmlSyntaxMatch
		for _, branch := range expression.Branches {
			values, failure := executeSyntaxExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			output = append(output, values...)
			if failure := context.step(ctx, len(output)); failure != nil {
				return nil, failure
			}
		}
		return output, nil
	case protocol.ExpressionStructureOrderMerge:
		var output []*XmlSyntaxMatch
		for _, branch := range expression.Branches {
			values, failure := executeSyntaxExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			output = append(output, values...)
		}
		sort.SliceStable(output, func(i, j int) bool { return output[i].ordinal < output[j].ordinal })
		if failure := context.step(ctx, len(output)); failure != nil {
			return nil, failure
		}
		return output, nil
	}
	return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
}

// matchSourceOrder is the structural identity order of one native match
// (query.rs:556-576).
func matchSourceOrder(item *XmlMatch) int {
	switch item.Kind {
	case XmlMatchDocument:
		return 0
	case XmlMatchErrorRegion:
		return item.Span.StartByte()
	default:
		return int(item.Node.Index())
	}
}

// applyOperator applies one native operator (query.rs:578-622).
func applyOperator(ctx context.Context, operator *protocol.OperatorCall, input []*XmlMatch,
	context *queryContext) ([]*XmlMatch, *protocol.QueryFailure) {
	var output []*XmlMatch
	switch operator.ID() {
	case "xml.document-root":
		documentRoot(input, context, &output)
	case "xml.document-declaration":
		documentDeclaration(input, context, &output)
	case "xml.document-doctype":
		documentDoctype(input, context, &output)
	case "xml.document-prolog", "xml.document-epilog":
		documentPrologEpilog(operator.ID(), input, context, &output)
	case "xml.element-children":
		elementChildren(input, context, &output)
	case "xml.element-child-elements":
		elementChildElements(input, context, &output)
	case "xml.element-child-text":
		elementChildText(input, context, &output)
	case "xml.element-child-cdata":
		elementChildCdata(input, context, &output)
	case "xml.element-child-comments":
		elementChildComments(input, context, &output)
	case "xml.element-child-pi":
		elementChildPi(input, context, &output)
	case "xml.element-descendants":
		elementDescendants(input, context, &output)
	case "xml.element-attributes":
		elementAttributes(input, context, &output)
	case "xml.element-namespace-bindings", "xml.element-in-scope-namespaces":
		namespaceBindings(operator.ID(), input, context, &output)
	case "xml.content-parent", "xml.attribute-element", "xml.reference-text":
		contentParent(input, context, &output)
	case "xml.text-references":
		textReferences(input, context, &output)
	case "xml.name-equals":
		nameEquals(operator, input, context, &output)
	case "xml.attribute-value-equals":
		attributeValueEquals(operator, input, context, &output)
	case "xml.pi-target-equals":
		piTargetEquals(operator, input, context, &output)
	case "xml.reference-kind-is":
		referenceKindIs(operator, input, context, &output)
	case "xml.reference-name-equals":
		referenceNameEquals(operator, input, context, &output)
	case "xml.node-kind-is":
		nodeKindIs(operator, input, context, &output)
	case "core.take":
		take(operator, input, context, &output)
	case "core.distinct-by-identity":
		distinctByIdentity(input, context, &output)
	default:
		return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
	}
	if failure := context.step(ctx, len(output)); failure != nil {
		return nil, failure
	}
	return output, nil
}

// documentRoot: the one document element, when formation proved it.
func documentRoot(input []*XmlMatch, context *queryContext, output *[]*XmlMatch) {
	root := context.document.Root()
	if root == nil {
		return
	}
	for _, item := range input {
		if item.Kind == XmlMatchDocument {
			context.push(output, context.elementMatch(root.index))
		}
	}
}

// documentDeclaration: the XML declaration, when present.
func documentDeclaration(input []*XmlMatch, context *queryContext, output *[]*XmlMatch) {
	for _, item := range input {
		if item.Kind != XmlMatchDocument {
			continue
		}
		declared := context.document.declaration
		if declared == nil {
			continue
		}
		node := context.document.authority.NodeRef(1, document.RoleXmlDeclaration)
		match := &XmlMatch{
			Kind:    XmlMatchDeclaration,
			Node:    node,
			Version: declared.Version,
		}
		if declared.Encoding != nil {
			encoding := declared.Encoding.Value
			match.Encoding = &encoding
		}
		if declared.Standalone != nil {
			standalone := declared.Standalone.Value
			match.Standalone = &standalone
		}
		context.push(output, match)
	}
}

// documentDoctype: the DOCTYPE occurrence, when present.
func documentDoctype(input []*XmlMatch, context *queryContext, output *[]*XmlMatch) {
	for _, item := range input {
		if item.Kind != XmlMatchDocument {
			continue
		}
		doctype := context.document.doctype
		if doctype == nil {
			continue
		}
		node := context.document.authority.NodeRef(2, document.RoleXmlDoctype)
		context.push(output, &XmlMatch{
			Kind:  XmlMatchDoctype,
			Node:  node,
			Local: doctype.Name.QName().String(),
		})
	}
}

// documentPrologEpilog: ordered prolog or epilog occurrences that publish
// a match.
func documentPrologEpilog(id string, input []*XmlMatch, context *queryContext, output *[]*XmlMatch) {
	var items []XmlPrologItem
	if id == "xml.document-prolog" {
		items = context.document.prolog
	} else {
		items = context.document.epilog
	}
	for _, item := range input {
		if item.Kind != XmlMatchDocument {
			continue
		}
		for index := range items {
			if match := context.prologItem(&items[index]); match != nil {
				context.push(output, match)
			}
		}
	}
}

// elementChildren: every child content occurrence, mixed order.
func elementChildren(input []*XmlMatch, context *queryContext, output *[]*XmlMatch) {
	for _, item := range input {
		if item.Kind != XmlMatchElement {
			continue
		}
		index := int(item.Node.Index())
		if index >= len(context.document.nodes) {
			continue
		}
		for _, child := range context.elementData(index).Children {
			context.push(output, context.contentMatch(child, item.Node))
		}
	}
}

// contentMatch builds the content match of one child arena index.
func (c *queryContext) contentMatch(index int, parent document.NodeRef) *XmlMatch {
	content := &c.document.nodes[index]
	switch content.Kind {
	case XmlContentElement:
		return c.elementMatch(index)
	case XmlContentText:
		return &XmlMatch{
			Kind:     XmlMatchText,
			Node:     c.nodeRef(index, document.RoleXmlText),
			Parent:   &parent,
			Semantic: TextSemantic(content.Text),
		}
	case XmlContentCdata:
		return &XmlMatch{
			Kind:   XmlMatchCdata,
			Node:   c.nodeRef(index, document.RoleXmlCdata),
			Parent: &parent,
			Text:   content.Cdata.Text,
		}
	case XmlContentComment:
		return &XmlMatch{
			Kind:   XmlMatchComment,
			Node:   c.nodeRef(index, document.RoleXmlComment),
			Parent: &parent,
			Text:   content.Comment.Text,
		}
	case XmlContentProcessingInstruction:
		pi := content.ProcessingInstruction
		var piContent *string
		if pi.Content != nil {
			text := pi.Content.Text
			piContent = &text
		}
		return &XmlMatch{
			Kind:    XmlMatchProcessingInstruction,
			Node:    c.nodeRef(index, document.RoleXmlProcessingInstruction),
			Parent:  &parent,
			Target:  pi.Target,
			Content: piContent,
		}
	default:
		return &XmlMatch{
			Kind: XmlMatchErrorRegion,
			Node: c.nodeRef(index, document.RoleXmlErrorRegion),
			Span: content.ErrorRegion.Span,
		}
	}
}

// elementChildElements: child element occurrences only.
func elementChildElements(input []*XmlMatch, context *queryContext, output *[]*XmlMatch) {
	for _, item := range input {
		if item.Kind != XmlMatchElement {
			continue
		}
		index := int(item.Node.Index())
		for _, child := range context.elementData(index).Children {
			if context.document.nodes[child].Kind == XmlContentElement {
				context.push(output, context.elementMatch(child))
			}
		}
	}
}

// elementChildText: child text occurrences only.
func elementChildText(input []*XmlMatch, context *queryContext, output *[]*XmlMatch) {
	for _, item := range input {
		if item.Kind != XmlMatchElement {
			continue
		}
		index := int(item.Node.Index())
		for _, child := range context.elementData(index).Children {
			if context.document.nodes[child].Kind == XmlContentText {
				context.push(output, context.contentMatch(child, item.Node))
			}
		}
	}
}

// elementChildCdata: child CDATA occurrences only.
func elementChildCdata(input []*XmlMatch, context *queryContext, output *[]*XmlMatch) {
	for _, item := range input {
		if item.Kind != XmlMatchElement {
			continue
		}
		index := int(item.Node.Index())
		for _, child := range context.elementData(index).Children {
			if context.document.nodes[child].Kind == XmlContentCdata {
				context.push(output, context.contentMatch(child, item.Node))
			}
		}
	}
}

// elementChildComments: child comment occurrences only.
func elementChildComments(input []*XmlMatch, context *queryContext, output *[]*XmlMatch) {
	for _, item := range input {
		if item.Kind != XmlMatchElement {
			continue
		}
		index := int(item.Node.Index())
		for _, child := range context.elementData(index).Children {
			if context.document.nodes[child].Kind == XmlContentComment {
				context.push(output, context.contentMatch(child, item.Node))
			}
		}
	}
}

// elementChildPi: child processing-instruction occurrences only.
func elementChildPi(input []*XmlMatch, context *queryContext, output *[]*XmlMatch) {
	for _, item := range input {
		if item.Kind != XmlMatchElement {
			continue
		}
		index := int(item.Node.Index())
		for _, child := range context.elementData(index).Children {
			if context.document.nodes[child].Kind == XmlContentProcessingInstruction {
				context.push(output, context.contentMatch(child, item.Node))
			}
		}
	}
}

// elementDescendants: bounded pre-order traversal with an explicit stack;
// the input element itself is never included (query.rs:879-903).
func elementDescendants(input []*XmlMatch, context *queryContext, output *[]*XmlMatch) {
	var stack []int
	for _, item := range input {
		if item.Kind != XmlMatchElement {
			continue
		}
		index := int(item.Node.Index())
		stack = append(stack, index)
		for len(stack) > 0 {
			current := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			children := context.elementData(current).Children
			for position := len(children) - 1; position >= 0; position-- {
				child := children[position]
				if context.document.nodes[child].Kind == XmlContentElement {
					stack = append(stack, child)
				}
			}
			if current != index {
				context.push(output, context.elementMatch(current))
			}
		}
	}
}

// elementAttributes: ordered attributes, excluding declarations.
func elementAttributes(input []*XmlMatch, context *queryContext, output *[]*XmlMatch) {
	for _, item := range input {
		if item.Kind != XmlMatchElement {
			continue
		}
		index := int(item.Node.Index())
		for _, attribute := range context.elementData(index).Attributes {
			context.push(output, attributeMatch(&attribute, item.Node, context))
		}
	}
}

// namespaceBindings: local declarations, or the full ancestry-derived
// chain oldest first.
func namespaceBindings(id string, input []*XmlMatch, context *queryContext, output *[]*XmlMatch) {
	for _, item := range input {
		if item.Kind != XmlMatchElement {
			continue
		}
		index := int(item.Node.Index())
		if id == "xml.element-in-scope-namespaces" {
			var chain []int
			current := &index
			for current != nil {
				chain = append(chain, *current)
				current = context.document.parentIndex(*current)
			}
			for position := len(chain) - 1; position >= 0; position-- {
				at := chain[position]
				element := context.nodeRef(at, document.RoleXmlElement)
				for _, binding := range context.elementData(at).Namespaces {
					context.push(output, namespaceBindingMatch(&binding, element, context))
				}
			}
		} else {
			for _, binding := range context.elementData(index).Namespaces {
				context.push(output, namespaceBindingMatch(&binding, item.Node, context))
			}
		}
	}
}

// namespaceBindingMatch builds one binding match on one owning element.
func namespaceBindingMatch(binding *XmlNamespaceBindingData, element document.NodeRef,
	context *queryContext) *XmlMatch {
	return &XmlMatch{
		Kind:    XmlMatchNamespaceBinding,
		Node:    context.nodeRef(int(binding.Ordinal), document.RoleXmlNamespaceBinding),
		Element: element,
		Prefix:  binding.Prefix,
		URI:     binding.URI,
	}
}

// contentParent: one step back to the owning element.
func contentParent(input []*XmlMatch, context *queryContext, output *[]*XmlMatch) {
	for _, item := range input {
		switch item.Kind {
		case XmlMatchAttribute, XmlMatchNamespaceBinding:
			context.push(output, elementFromNode(context, item.Element))
		case XmlMatchText, XmlMatchCdata, XmlMatchComment, XmlMatchProcessingInstruction,
			XmlMatchElement, XmlMatchReference:
			if item.Parent != nil {
				context.push(output, elementFromNode(context, *item.Parent))
			}
		}
	}
}

// textReferences: the ordered reference occurrences of one text.
func textReferences(input []*XmlMatch, context *queryContext, output *[]*XmlMatch) {
	for _, item := range input {
		if item.Kind != XmlMatchText {
			continue
		}
		index := int(item.Node.Index())
		content := &context.document.nodes[index]
		if content.Kind != XmlContentText {
			continue
		}
		for ordinal, fragment := range content.Text.Fragments {
			var kind XmlReferenceKind
			var name string
			var resolved string
			switch fragment.Kind {
			case ReferenceFragmentCharacterReference:
				kind = XmlReferenceKindCharacter
				name = "&#x" + strings.ToUpper(strconv.FormatUint(uint64(fragment.ResolvedChar), 16)) + ";"
				resolved = string(fragment.ResolvedChar)
			case ReferenceFragmentPredefinedEntity:
				kind = XmlReferenceKindPredefined
				name = fragment.Name
				resolved = fragment.Resolved
			case ReferenceFragmentGeneralEntity:
				kind = XmlReferenceKindGeneral
				name = fragment.Name
				resolved = fragment.Resolved
			default:
				continue
			}
			node := context.nodeRef(ordinal, document.RoleXmlEntityReference)
			context.push(output, &XmlMatch{
				Kind:          XmlMatchReference,
				Node:          node,
				TextNode:      item.Node,
				Parent:        item.Parent,
				ReferenceKind: kind,
				Name:          name,
				Resolved:      resolved,
			})
		}
	}
}

// nameEquals: original-spelling or expanded-name comparison.
func nameEquals(operator *protocol.OperatorCall, input []*XmlMatch,
	context *queryContext, output *[]*XmlMatch) {
	expectedPrefix := coreStringArgument(operator, "prefix")
	expectedLocal := coreStringArgument(operator, "local")
	expectedNamespace := coreStringArgument(operator, "namespace")
	comparison := coreStringArgument(operator, "comparison")
	for _, item := range input {
		matches := false
		switch item.Kind {
		case XmlMatchElement, XmlMatchAttribute:
			prefix := ""
			if item.Prefix != nil {
				prefix = *item.Prefix
			}
			namespace := ""
			if item.Namespace != nil {
				namespace = *item.Namespace
			}
			if comparison == "OriginalExact" {
				matches = prefix == expectedPrefix && item.Local == expectedLocal
			} else if comparison == "Expanded" &&
				(item.Kind == XmlMatchAttribute || !item.NamespaceError) {
				matches = namespace == expectedNamespace && item.Local == expectedLocal
			}
		}
		if matches {
			context.push(output, item)
		}
	}
}

// attributeValueEquals: CDATA-normalized value equality.
func attributeValueEquals(operator *protocol.OperatorCall, input []*XmlMatch,
	context *queryContext, output *[]*XmlMatch) {
	expected := coreStringArgument(operator, "value")
	for _, item := range input {
		if item.Kind == XmlMatchAttribute && item.Value == expected {
			context.push(output, item)
		}
	}
}

// piTargetEquals: processing-instruction target equality.
func piTargetEquals(operator *protocol.OperatorCall, input []*XmlMatch,
	context *queryContext, output *[]*XmlMatch) {
	expected := coreStringArgument(operator, "target")
	for _, item := range input {
		if item.Kind == XmlMatchProcessingInstruction && item.Target == expected {
			context.push(output, item)
		}
	}
}

// referenceKindIs: reference kind equality.
func referenceKindIs(operator *protocol.OperatorCall, input []*XmlMatch,
	context *queryContext, output *[]*XmlMatch) {
	expected := XmlReferenceKindCharacter
	switch coreStringArgument(operator, "kind") {
	case "Predefined":
		expected = XmlReferenceKindPredefined
	case "General":
		expected = XmlReferenceKindGeneral
	}
	for _, item := range input {
		if item.Kind == XmlMatchReference && item.ReferenceKind == expected {
			context.push(output, item)
		}
	}
}

// referenceNameEquals: reference name equality.
func referenceNameEquals(operator *protocol.OperatorCall, input []*XmlMatch,
	context *queryContext, output *[]*XmlMatch) {
	expected := coreStringArgument(operator, "name")
	for _, item := range input {
		if item.Kind == XmlMatchReference && item.Name == expected {
			context.push(output, item)
		}
	}
}

// nodeKindIs: match-kind filter over mixed output.
func nodeKindIs(operator *protocol.OperatorCall, input []*XmlMatch,
	context *queryContext, output *[]*XmlMatch) {
	expected := coreStringArgument(operator, "kind")
	for _, item := range input {
		kind := ""
		switch item.Kind {
		case XmlMatchDocument:
			kind = "document"
		case XmlMatchDeclaration:
			kind = "declaration"
		case XmlMatchDoctype:
			kind = "doctype"
		case XmlMatchPrologItem:
			kind = "prolog-item"
		case XmlMatchElement:
			kind = "element"
		case XmlMatchAttribute:
			kind = "attribute"
		case XmlMatchNamespaceBinding:
			kind = "namespace-binding"
		case XmlMatchText:
			kind = "text"
		case XmlMatchCdata:
			kind = "cdata"
		case XmlMatchComment:
			kind = "comment"
		case XmlMatchProcessingInstruction:
			kind = "processing-instruction"
		case XmlMatchReference:
			kind = "reference"
		case XmlMatchErrorRegion:
			kind = "error-region"
		}
		if kind == expected {
			context.push(output, item)
		}
	}
}

// take: the first `count` input items.
func take(operator *protocol.OperatorCall, input []*XmlMatch,
	context *queryContext, output *[]*XmlMatch) {
	count := takeCount(operator)
	if count > len(input) {
		count = len(input)
	}
	for _, item := range input[:count] {
		context.push(output, item)
	}
}

// distinctByIdentity: first occurrence of every identity.
func distinctByIdentity(input []*XmlMatch, context *queryContext, output *[]*XmlMatch) {
	seen := make(map[document.NodeRef]bool)
	for _, item := range input {
		if seen[item.Node] {
			continue
		}
		seen[item.Node] = true
		context.push(output, item)
	}
}

// elementFromNode resolves one element handle to an Element match.
func elementFromNode(context *queryContext, node document.NodeRef) *XmlMatch {
	index := int(node.Index())
	if index < len(context.document.nodes) &&
		context.document.nodes[index].Kind == XmlContentElement {
		return context.elementMatch(index)
	}
	// The root document element is addressed through the root handle.
	if root := context.document.Root(); root != nil {
		return context.elementMatch(root.index)
	}
	return &XmlMatch{Kind: XmlMatchDocument, Node: context.document.NodeRef()}
}

// attributeMatch builds one attribute match on one owning element.
func attributeMatch(attribute *XmlAttributeData, element document.NodeRef,
	context *queryContext) *XmlMatch {
	return &XmlMatch{
		Kind:      XmlMatchAttribute,
		Node:      context.nodeRef(int(attribute.Ordinal), document.RoleXmlAttribute),
		Element:   element,
		Prefix:    attribute.QName.Prefix,
		Local:     attribute.QName.Local,
		Namespace: expandedNamespace(attribute.Expanded),
		Value:     attribute.NormalizedValue,
	}
}

// expandedNamespace extracts the namespace URI of one expanded name.
func expandedNamespace(expanded *ExpandedName) *string {
	if expanded == nil || expanded.Namespace == nil {
		return nil
	}
	return expanded.Namespace
}

// applySyntaxOperator applies one syntax operator.
func applySyntaxOperator(ctx context.Context, operator *protocol.OperatorCall,
	input []*XmlSyntaxMatch, context *queryContext) ([]*XmlSyntaxMatch, *protocol.QueryFailure) {
	var output []*XmlSyntaxMatch
	switch operator.ID() {
	case "xml.syntax-kind-is":
		expected := coreStringArgument(operator, "kind")
		for _, item := range input {
			if item.kind.AsStr() == expected {
				output = append(output, item)
			}
		}
	case "xml.syntax-text-equals":
		expected := coreStringArgument(operator, "text")
		decoded, _ := context.document.source.DecodedText()
		for _, item := range input {
			if decoded[item.span.StartByte():item.span.EndByte()] == expected {
				output = append(output, item)
			}
		}
	case "core.take":
		count := takeCount(operator)
		if count > len(input) {
			count = len(input)
		}
		output = append(output, input[:count]...)
	case "core.distinct-by-identity":
		seen := make(map[document.NodeRef]bool)
		for _, item := range input {
			if seen[item.node] {
				continue
			}
			seen[item.node] = true
			output = append(output, item)
		}
	default:
		return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
	}
	if failure := context.step(ctx, len(output)); failure != nil {
		return nil, failure
	}
	return output, nil
}

// coreStringArgument reads one validated string operator argument.
func coreStringArgument(operator *protocol.OperatorCall, name string) string {
	value, ok := operator.Arguments()[name]
	if !ok {
		return ""
	}
	text, ok := value.(core.String)
	if !ok {
		return ""
	}
	return string(text)
}

// takeCount reads the validated core.take count argument.
func takeCount(operator *protocol.OperatorCall) int {
	value, ok := operator.Arguments()["count"]
	if !ok {
		return 0
	}
	integer, ok := value.(core.Integer)
	if !ok {
		return 0
	}
	number := integer.Int()
	if !number.IsInt64() || number.Sign() < 0 {
		return 0
	}
	return int(number.Int64())
}

// applySelection applies the native cardinality selection
// (query.rs:252-269).
func applySelection(values []*XmlMatch,
	selection protocol.QuerySelection) ([]*XmlMatch, *protocol.QueryFailure) {
	switch selection {
	case protocol.SelectionAll, "":
		return values, nil
	case protocol.SelectionFirst:
		if len(values) > 0 {
			return values[:1], nil
		}
		return nil, nil
	case protocol.SelectionLast:
		if len(values) > 0 {
			return values[len(values)-1:], nil
		}
		return nil, nil
	case protocol.SelectionZeroOrOne:
		if len(values) <= 1 {
			return values, nil
		}
		return nil, &protocol.QueryFailure{Kind: protocol.FailureCardinalityViolation}
	case protocol.SelectionRequireOne:
		if len(values) == 1 {
			return values, nil
		}
		return nil, &protocol.QueryFailure{Kind: protocol.FailureCardinalityViolation}
	}
	return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
}

// applySyntaxSelection applies the syntax cardinality selection.
func applySyntaxSelection(values []*XmlSyntaxMatch,
	selection protocol.QuerySelection) ([]*XmlSyntaxMatch, *protocol.QueryFailure) {
	switch selection {
	case protocol.SelectionAll, "":
		return values, nil
	case protocol.SelectionFirst:
		if len(values) > 0 {
			return values[:1], nil
		}
		return nil, nil
	case protocol.SelectionLast:
		if len(values) > 0 {
			return values[len(values)-1:], nil
		}
		return nil, nil
	case protocol.SelectionZeroOrOne:
		if len(values) <= 1 {
			return values, nil
		}
		return nil, &protocol.QueryFailure{Kind: protocol.FailureCardinalityViolation}
	case protocol.SelectionRequireOne:
		if len(values) == 1 {
			return values, nil
		}
		return nil, &protocol.QueryFailure{Kind: protocol.FailureCardinalityViolation}
	}
	return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
}
