package yaml

import (
	"context"
	"sort"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the YAML native and lossless-syntax query domains
// (consema-yaml query.rs). Native matches carry snapshot-bound roles and
// exact raw spans in presentation order; syntax matches are source-order
// pieces. Definitions validate domain, operator, argument types, and role
// composition before execution (RFC 0007 §9).

// YamlMatchKind is the closed native match variant.
type YamlMatchKind string

// The seven frozen native match variants.
const (
	// YamlMatchStream is a complete serialization stream.
	YamlMatchStream YamlMatchKind = "Stream"
	// YamlMatchDocument is one independent document.
	YamlMatchDocument YamlMatchKind = "Document"
	// YamlMatchNode is one representation node.
	YamlMatchNode YamlMatchKind = "Node"
	// YamlMatchMappingEntry is one ordered mapping association.
	YamlMatchMappingEntry YamlMatchKind = "MappingEntry"
	// YamlMatchSequenceElement is one ordered sequence association.
	YamlMatchSequenceElement YamlMatchKind = "SequenceElement"
	// YamlMatchAnchorDefinition is one anchor definition occurrence.
	YamlMatchAnchorDefinition YamlMatchKind = "AnchorDefinition"
	// YamlMatchAliasOccurrence is one alias serialization occurrence.
	YamlMatchAliasOccurrence YamlMatchKind = "AliasOccurrence"
)

// YamlMatch is one snapshot-bound YAML native semantic query match
// (query.rs:9-41).
type YamlMatch struct {
	// Kind is the match variant.
	Kind YamlMatchKind
	// Stream is the stream identity of Stream matches.
	Stream document.NodeRef
	// StreamSpan is the raw stream span.
	StreamSpan document.Span
	// DocumentCount is the document count of Stream matches.
	DocumentCount int
	// Ordinal is the zero-based ordinal of Document, MappingEntry,
	// SequenceElement, and AliasOccurrence matches.
	Ordinal int
	// Document is the document identity of Document matches.
	Document document.NodeRef
	// Root is the root node identity of Document matches.
	Root document.NodeRef
	// Span is the exact raw span.
	Span document.Span
	// Node is the node identity of Node, MappingEntry, SequenceElement,
	// AnchorDefinition, and AliasOccurrence matches.
	Node document.NodeRef
	// KindName is the native node kind of Node matches.
	KindName YamlNodeKind
	// Tag is the resolved tag of Node matches.
	Tag string
	// ScalarKind is the scalar category of scalar Node matches.
	ScalarKind *YamlScalarKind
	// Canonical is the canonical scalar content of scalar Node matches.
	Canonical *string
	// Anchor is the defining anchor name of Node and AnchorDefinition
	// matches.
	Anchor string
	// Entry is the association identity of MappingEntry matches.
	Entry document.NodeRef
	// Key is the key node identity of MappingEntry matches.
	Key document.NodeRef
	// Value is the value node identity of MappingEntry matches.
	Value document.NodeRef
	// Element is the association identity of SequenceElement matches.
	Element document.NodeRef
	// Definition is the anchor-definition identity of AnchorDefinition
	// matches.
	Definition document.NodeRef
	// Alias is the alias occurrence identity of AliasOccurrence matches.
	Alias document.NodeRef
	// Target is the alias target node identity of AliasOccurrence matches.
	Target document.NodeRef
	// Name is the alias name of AliasOccurrence matches.
	Name string
	// identity is the process-local identity used for
	// core.distinct-by-identity and structure-order-merge.
	identity document.NodeRef
}

// NodeRef returns the primary match identity.
func (m *YamlMatch) NodeRef() document.NodeRef { return m.identity }

// YamlSyntaxMatch is one snapshot-bound YAML lossless syntax query match
// (query.rs:53-86).
type YamlSyntaxMatch struct {
	node    document.NodeRef
	span    document.Span
	kind    YamlSyntaxKind
	ordinal int
}

// NodeRef returns the process-local syntax-piece identity.
func (m YamlSyntaxMatch) NodeRef() document.NodeRef { return m.node }

// Span returns the exact raw source span.
func (m YamlSyntaxMatch) Span() document.Span { return m.span }

// Kind returns the format-specific lossless kind.
func (m YamlSyntaxMatch) Kind() YamlSyntaxKind { return m.kind }

// Ordinal returns the zero-based source-order position.
func (m YamlSyntaxMatch) Ordinal() int { return m.ordinal }

// ExecuteYamlQuery executes a validated YAML native semantic query against
// one immutable snapshot (query.rs:88-127). The context is used for
// cancellation and deadlines only. Steps and result counts are bounded by
// limits; exceeding either is core.query.resource-limit@1.
func ExecuteYamlQuery(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) ([]YamlMatch, *protocol.QueryFailure) {
	domain := executable.Definition().Domain()
	if domain.ID() != "yaml.native-semantic-query" || domain.Version() != 1 {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	context := &yamlQueryContext{ctx: ctx, document: doc, limits: limits}
	if failure := context.step(1); failure != nil {
		return nil, failure
	}
	input := []YamlMatch{{
		Kind: YamlMatchStream, Stream: doc.StreamNodeRef(), StreamSpan: doc.StreamSpan(),
		DocumentCount: doc.DocumentCount(),
		identity:      doc.StreamNodeRef(),
	}}
	matches, failure := executeQueryExpression(ctx, executable.Definition().Expression(), input, context)
	if failure != nil {
		return nil, failure
	}
	matches, failure = applyYamlSelection(matches, executable.Definition().Selection())
	if failure != nil {
		return nil, failure
	}
	return matches, nil
}

// ExecuteYamlSyntaxQuery executes a validated YAML lossless syntax query
// against every source piece in raw order (query.rs:129-169).
func ExecuteYamlSyntaxQuery(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) ([]YamlSyntaxMatch, *protocol.QueryFailure) {
	domain := executable.Definition().Domain()
	if domain.ID() != "yaml.lossless-syntax-query" || domain.Version() != 1 {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	context := &yamlQueryContext{ctx: ctx, document: doc, limits: limits}
	pieces := doc.index.Pieces()
	if failure := context.step(len(pieces)); failure != nil {
		return nil, failure
	}
	kinds := doc.kinds
	input := make([]YamlSyntaxMatch, 0, len(pieces))
	for ordinal, piece := range pieces {
		input = append(input, YamlSyntaxMatch{
			node:    doc.authority.NodeRef(uint64(ordinal), document.RoleYamlSyntaxPiece),
			span:    piece.Span(),
			kind:    kinds[ordinal],
			ordinal: ordinal,
		})
	}
	matches, failure := executeSyntaxExpression(ctx, executable.Definition().Expression(), input, context)
	if failure != nil {
		return nil, failure
	}
	matches, failure = applySyntaxSelection(matches, executable.Definition().Selection())
	if failure != nil {
		return nil, failure
	}
	return matches, nil
}

type yamlQueryContext struct {
	ctx      context.Context
	document *Document
	limits   protocol.QueryLimits
	steps    int
}

func (c *yamlQueryContext) step(results int) *protocol.QueryFailure {
	select {
	case <-c.ctx.Done():
		return &protocol.QueryFailure{Kind: protocol.FailureCancelled}
	default:
	}
	c.steps++
	if c.steps > c.limits.MaxSteps || results > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	return nil
}

func checkCancelled(ctx context.Context) *protocol.QueryFailure {
	select {
	case <-ctx.Done():
		return &protocol.QueryFailure{Kind: protocol.FailureCancelled}
	default:
		return nil
	}
}

// executeQueryExpression evaluates one native expression against the input
// matches (query.rs:213-254).
func executeQueryExpression(ctx context.Context, expression *protocol.QueryExpression,
	input []YamlMatch, context *yamlQueryContext) ([]YamlMatch, *protocol.QueryFailure) {
	switch expression.Kind {
	case protocol.ExpressionInput:
		return input, nil
	case protocol.ExpressionApply:
		inner, failure := executeQueryExpression(ctx, expression.Input, input, context)
		if failure != nil {
			return nil, failure
		}
		return applyQueryOperator(ctx, expression.Operator, inner, context)
	case protocol.ExpressionConcat:
		var output []YamlMatch
		for _, branch := range expression.Branches {
			matches, failure := executeQueryExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			output = append(output, matches...)
			if failure := context.step(len(output)); failure != nil {
				return nil, failure
			}
		}
		return output, nil
	case protocol.ExpressionStructureOrderMerge:
		var output []YamlMatch
		for _, branch := range expression.Branches {
			matches, failure := executeQueryExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			output = append(output, matches...)
		}
		sort.SliceStable(output, func(i, j int) bool {
			left := output[i].Span
			right := output[j].Span
			if left.StartByte() != right.StartByte() {
				return left.StartByte() < right.StartByte()
			}
			if left.EndByte() != right.EndByte() {
				return left.EndByte() < right.EndByte()
			}
			leftRole := roleOrder(output[i].identity.Role())
			rightRole := roleOrder(output[j].identity.Role())
			if leftRole != rightRole {
				return leftRole < rightRole
			}
			return output[i].identity.Index() < output[j].identity.Index()
		})
		if failure := context.step(len(output)); failure != nil {
			return nil, failure
		}
		return output, nil
	}
	return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
}

// roleOrder is the frozen structure-order-merge role precedence (query.rs
// role_order).
func roleOrder(role document.NodeRole) uint8 {
	switch role {
	case document.RoleYamlStream:
		return 0
	case document.RoleYamlDocument:
		return 1
	case document.RoleYamlMappingEntry, document.RoleYamlSequenceElement:
		return 2
	case document.RoleYamlAnchorDefinition:
		return 3
	case document.RoleYamlAlias:
		return 4
	case document.RoleYamlNode:
		return 5
	}
	return 6
}

// applyQueryOperator evaluates one native operator (query.rs:341-469).
func applyQueryOperator(ctx context.Context, operator *protocol.OperatorCall,
	input []YamlMatch, context *yamlQueryContext) ([]YamlMatch, *protocol.QueryFailure) {
	if failure := checkCancelled(ctx); failure != nil {
		return nil, failure
	}
	var output []YamlMatch
	switch operator.ID() {
	case "yaml.documents":
		for _, match := range input {
			if match.Kind != YamlMatchStream {
				continue
			}
			for ordinal := range context.document.native.documents {
				doc := &context.document.native.documents[ordinal]
				output = append(output, YamlMatch{
					Kind: YamlMatchDocument, Ordinal: ordinal,
					Document: context.document.authority.NodeRef(uint64(ordinal),
						document.RoleYamlDocument),
					Root: context.document.nodeRef(doc.root),
					Span: doc.span,
					identity: context.document.authority.NodeRef(uint64(ordinal),
						document.RoleYamlDocument),
				})
			}
		}
	case "yaml.document-root":
		for _, match := range input {
			if match.Kind != YamlMatchDocument {
				continue
			}
			output = append(output, context.nodeMatch(int(match.Root.Index())))
		}
	case "yaml.where-node-kind":
		expected, ok := stringArgument(operator, "kind")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "kind"}
		}
		for _, match := range input {
			if match.Kind == YamlMatchNode && string(match.KindName) == expected {
				output = append(output, match)
			}
		}
	case "yaml.where-tag":
		expected, ok := stringArgument(operator, "tag")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "tag"}
		}
		for _, match := range input {
			if match.Kind == YamlMatchNode && match.Tag == expected {
				output = append(output, match)
			}
		}
	case "yaml.scalar-canonical-equals":
		expected, ok := stringArgument(operator, "canonical")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "canonical"}
		}
		for _, match := range input {
			if match.Kind == YamlMatchNode && match.Canonical != nil &&
				*match.Canonical == expected {
				output = append(output, match)
			}
		}
	case "yaml.try-sequence-elements":
		for _, match := range input {
			if match.Kind != YamlMatchNode {
				continue
			}
			index := int(match.Node.Index())
			content := &context.document.native.nodes[index].content
			if content.kind != contentSequence {
				continue
			}
			for ordinal, item := range content.items {
				output = append(output, YamlMatch{
					Kind: YamlMatchSequenceElement, Ordinal: ordinal,
					Element: context.document.authority.NodeRef(item.identity,
						document.RoleYamlSequenceElement),
					Node: context.document.nodeRef(item.node),
					Span: item.span,
					identity: context.document.authority.NodeRef(item.identity,
						document.RoleYamlSequenceElement),
				})
			}
		}
	case "yaml.sequence-element-node":
		for _, match := range input {
			if match.Kind != YamlMatchSequenceElement {
				continue
			}
			output = append(output, context.nodeMatch(int(match.Node.Index())))
		}
	case "yaml.try-mapping-entries":
		for _, match := range input {
			if match.Kind != YamlMatchNode {
				continue
			}
			index := int(match.Node.Index())
			content := &context.document.native.nodes[index].content
			if content.kind != contentMapping {
				continue
			}
			for ordinal, entry := range content.entries {
				output = append(output, YamlMatch{
					Kind: YamlMatchMappingEntry, Ordinal: ordinal,
					Entry: context.document.authority.NodeRef(entry.identity,
						document.RoleYamlMappingEntry),
					Key:   context.document.nodeRef(entry.key),
					Value: context.document.nodeRef(entry.value),
					Span:  entry.span,
					identity: context.document.authority.NodeRef(entry.identity,
						document.RoleYamlMappingEntry),
				})
			}
		}
	case "yaml.mapping-entry-key", "yaml.mapping-entry-value":
		takeKey := operator.ID() == "yaml.mapping-entry-key"
		for _, match := range input {
			if match.Kind != YamlMatchMappingEntry {
				continue
			}
			if takeKey {
				output = append(output, context.nodeMatch(int(match.Key.Index())))
			} else {
				output = append(output, context.nodeMatch(int(match.Value.Index())))
			}
		}
	case "yaml.anchor-definition":
		for _, match := range input {
			if match.Kind != YamlMatchNode {
				continue
			}
			index := int(match.Node.Index())
			node := &context.document.native.nodes[index]
			if !node.hasAnchor {
				continue
			}
			definition := context.document.authority.NodeRef(uint64(index),
				document.RoleYamlAnchorDefinition)
			output = append(output, YamlMatch{
				Kind:       YamlMatchAnchorDefinition,
				Name:       node.anchor,
				Definition: definition,
				Node:       match.Node,
				Span:       node.anchorSpan,
				identity:   definition,
			})
		}
	case "yaml.anchor-node":
		for _, match := range input {
			if match.Kind != YamlMatchAnchorDefinition {
				continue
			}
			output = append(output, context.nodeMatch(int(match.Node.Index())))
		}
	case "yaml.alias-occurrences":
		for _, match := range input {
			if match.Kind != YamlMatchStream {
				continue
			}
			for ordinal, alias := range context.document.native.aliases {
				aliasRef := context.document.authority.NodeRef(alias.identity,
					document.RoleYamlAlias)
				output = append(output, YamlMatch{
					Kind: YamlMatchAliasOccurrence, Ordinal: ordinal,
					Alias:    aliasRef,
					Target:   context.document.nodeRef(alias.target),
					Name:     alias.name,
					Span:     alias.span,
					identity: aliasRef,
				})
			}
		}
	case "yaml.alias-target":
		for _, match := range input {
			if match.Kind != YamlMatchAliasOccurrence {
				continue
			}
			output = append(output, context.nodeMatch(int(match.Target.Index())))
		}
	case "core.take":
		count, ok := integerArgument(operator, "count")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "count"}
		}
		if count < len(input) {
			output = input[:count]
		} else {
			output = input
		}
	case "core.distinct-by-identity":
		seen := make(map[document.NodeRef]bool, len(input))
		for _, match := range input {
			if !seen[match.identity] {
				seen[match.identity] = true
				output = append(output, match)
			}
		}
	default:
		return nil, &protocol.QueryFailure{Kind: protocol.FailureUnknownOperator,
			Operator: operator.ID(), Version: operator.Version()}
	}
	if failure := context.step(len(output)); failure != nil {
		return nil, failure
	}
	return output, nil
}

// nodeMatch builds the Node variant for one native node index (query.rs
// node_match).
func (c *yamlQueryContext) nodeMatch(index int) YamlMatch {
	node := &c.document.native.nodes[index]
	match := YamlMatch{
		Kind:     YamlMatchNode,
		Node:     c.document.nodeRef(index),
		KindName: node.kindName(),
		Tag:      node.tag,
		Span:     node.span,
		identity: c.document.nodeRef(index),
	}
	if node.hasAnchor {
		match.Anchor = node.anchor
	}
	if node.content.kind == contentScalar {
		kind := node.content.scalar.kind
		canonical := node.content.scalar.canonical
		match.ScalarKind = &kind
		match.Canonical = &canonical
	}
	return match
}

func (n *nativeNode) kindName() YamlNodeKind {
	switch n.content.kind {
	case contentScalar:
		return NodeKindScalar
	case contentSequence:
		return NodeKindSequence
	default:
		return NodeKindMapping
	}
}

// executeSyntaxExpression evaluates one lossless-syntax expression
// (query.rs:256-288).
func executeSyntaxExpression(ctx context.Context, expression *protocol.QueryExpression,
	input []YamlSyntaxMatch, context *yamlQueryContext) ([]YamlSyntaxMatch, *protocol.QueryFailure) {
	switch expression.Kind {
	case protocol.ExpressionInput:
		return input, nil
	case protocol.ExpressionApply:
		inner, failure := executeSyntaxExpression(ctx, expression.Input, input, context)
		if failure != nil {
			return nil, failure
		}
		return applySyntaxOperator(ctx, expression.Operator, inner, context)
	case protocol.ExpressionConcat:
		var output []YamlSyntaxMatch
		for _, branch := range expression.Branches {
			matches, failure := executeSyntaxExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			output = append(output, matches...)
			if failure := context.step(len(output)); failure != nil {
				return nil, failure
			}
		}
		return output, nil
	case protocol.ExpressionStructureOrderMerge:
		var output []YamlSyntaxMatch
		for _, branch := range expression.Branches {
			matches, failure := executeSyntaxExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			output = append(output, matches...)
		}
		sort.SliceStable(output, func(i, j int) bool {
			return output[i].ordinal < output[j].ordinal
		})
		if failure := context.step(len(output)); failure != nil {
			return nil, failure
		}
		return output, nil
	}
	return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
}

// applySyntaxOperator evaluates one lossless-syntax operator
// (query.rs:290-339).
func applySyntaxOperator(ctx context.Context, operator *protocol.OperatorCall,
	input []YamlSyntaxMatch, context *yamlQueryContext) ([]YamlSyntaxMatch, *protocol.QueryFailure) {
	if failure := checkCancelled(ctx); failure != nil {
		return nil, failure
	}
	var output []YamlSyntaxMatch
	switch operator.ID() {
	case "yaml.syntax-kind-is":
		expected, ok := stringArgument(operator, "kind")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "kind"}
		}
		kind, valid := YamlSyntaxKindFromName(expected)
		if !valid {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "kind"}
		}
		for _, match := range input {
			if match.kind == kind {
				output = append(output, match)
			}
		}
	case "yaml.syntax-text-equals":
		expected, ok := stringArgument(operator, "text")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "text"}
		}
		encoded := encodeText(expected, context.document.source.EncodingFacts().Selected())
		source := context.document.source.Bytes()
		for _, match := range input {
			span := match.span
			if span.EndByte() <= len(source) &&
				string(source[span.StartByte():span.EndByte()]) == encoded {
				output = append(output, match)
			}
		}
	case "core.take":
		count, ok := integerArgument(operator, "count")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "count"}
		}
		if count < len(input) {
			output = input[:count]
		} else {
			output = input
		}
	case "core.distinct-by-identity":
		seen := make(map[document.NodeRef]bool, len(input))
		for _, match := range input {
			if !seen[match.node] {
				seen[match.node] = true
				output = append(output, match)
			}
		}
	default:
		return nil, &protocol.QueryFailure{Kind: protocol.FailureUnknownOperator,
			Operator: operator.ID(), Version: operator.Version()}
	}
	if failure := context.step(len(output)); failure != nil {
		return nil, failure
	}
	return output, nil
}

// encodeText encodes one argument into the selected source encoding for
// raw-byte comparison (query.rs encoded_text).
func encodeText(text string, encoding document.SourceEncoding) string {
	switch encoding.Kind() {
	case document.EncodingUtf8:
		return text
	case document.EncodingUtf16Le:
		var output []byte
		for _, character := range text {
			if character >= 0x10000 {
				value := character - 0x10000
				output = appendUTF16(output, uint16(0xD800+value>>10), true)
				output = appendUTF16(output, uint16(0xDC00+value&0x3FF), true)
				continue
			}
			output = appendUTF16(output, uint16(character), true)
		}
		return string(output)
	case document.EncodingUtf16Be:
		var output []byte
		for _, character := range text {
			if character >= 0x10000 {
				value := character - 0x10000
				output = appendUTF16(output, uint16(0xD800+value>>10), false)
				output = appendUTF16(output, uint16(0xDC00+value&0x3FF), false)
				continue
			}
			output = appendUTF16(output, uint16(character), false)
		}
		return string(output)
	}
	return ""
}

func stringArgument(operator *protocol.OperatorCall, name string) (string, bool) {
	value, ok := operator.Arguments()[name]
	if !ok {
		return "", false
	}
	text, ok := value.(core.String)
	if !ok {
		return "", false
	}
	return string(text), true
}

func integerArgument(operator *protocol.OperatorCall, name string) (int, bool) {
	value, ok := operator.Arguments()[name]
	if !ok {
		return 0, false
	}
	integer, ok := value.(core.Integer)
	if !ok {
		return 0, false
	}
	number := integer.Int()
	if !number.IsInt64() {
		return 0, false
	}
	host := number.Int64()
	if host < 0 || host > int64(^uint(0)>>1) {
		return 0, false
	}
	return int(host), true
}

// applyYamlSelection applies the definition selection (query.rs:471-488).
func applyYamlSelection(matches []YamlMatch,
	selection protocol.QuerySelection) ([]YamlMatch, *protocol.QueryFailure) {
	switch selection {
	case protocol.SelectionAll:
		return matches, nil
	case protocol.SelectionFirst:
		if len(matches) > 1 {
			matches = matches[:1]
		}
		return matches, nil
	case protocol.SelectionLast:
		if len(matches) > 0 {
			matches = matches[len(matches)-1:]
		}
		return matches, nil
	case protocol.SelectionZeroOrOne:
		if len(matches) <= 1 {
			return matches, nil
		}
	case protocol.SelectionRequireOne:
		if len(matches) == 1 {
			return matches, nil
		}
	}
	return nil, &protocol.QueryFailure{Kind: protocol.FailureCardinalityViolation}
}

func applySyntaxSelection(matches []YamlSyntaxMatch,
	selection protocol.QuerySelection) ([]YamlSyntaxMatch, *protocol.QueryFailure) {
	switch selection {
	case protocol.SelectionAll:
		return matches, nil
	case protocol.SelectionFirst:
		if len(matches) > 1 {
			matches = matches[:1]
		}
		return matches, nil
	case protocol.SelectionLast:
		if len(matches) > 0 {
			matches = matches[len(matches)-1:]
		}
		return matches, nil
	case protocol.SelectionZeroOrOne:
		if len(matches) <= 1 {
			return matches, nil
		}
	case protocol.SelectionRequireOne:
		if len(matches) == 1 {
			return matches, nil
		}
	}
	return nil, &protocol.QueryFailure{Kind: protocol.FailureCardinalityViolation}
}
