package hcl

import (
	"context"
	"sort"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the HCL native and lossless syntax query execution
// (crates/consema-hcl/src/query.rs; RFC 0014 §7). Execution mirrors the
// Rust engine: deterministic operator application, result and step
// budgets, and cancellation through the caller's context. The domain
// serves both profiles: the two profiles own the one native model, so only
// the domain identity is guarded here.

// HclMatchKind is the closed HCL native match category (RFC 0014 §7.1).
type HclMatchKind uint8

// The seven frozen native match categories.
const (
	// HclMatchBody is one HCL body: the ordered container of attributes
	// and blocks shared by the root and nested bodies.
	HclMatchBody HclMatchKind = iota
	// HclMatchAttribute is one attribute occurrence.
	HclMatchAttribute
	// HclMatchBlock is one block occurrence.
	HclMatchBlock
	// HclMatchBlockLabel is one block label with its quote/naked fact.
	HclMatchBlockLabel
	// HclMatchExpression is one expression AST node.
	HclMatchExpression
	// HclMatchTemplatePart is one ordered template part.
	HclMatchTemplatePart
	// HclMatchErrorRegion is one recovered HCL error region with its
	// stable code.
	HclMatchErrorRegion
)

// HclMatch is one snapshot-bound HCL native semantic query match (RFC 0014
// §7.1).
//
// Every match carries a snapshot-bound handle and a reference into the
// immutable native tree of the queried document: the same tree node
// reached through different operators is one match identity, and no
// identity is ever shared between distinct occurrences (RFC 0014 §6).
type HclMatch struct {
	// Kind is the closed match category.
	Kind HclMatchKind
	// Node is the exact match identity.
	Node document.NodeRef
	// Body is the native body of Body matches.
	Body *HclBody
	// Attribute is the native attribute of Attribute matches.
	Attribute *HclAttribute
	// Block is the native block of Block matches.
	Block *HclBlock
	// Label is the native label of BlockLabel matches.
	Label *HclBlockLabel
	// Expression is the native expression of Expression matches.
	Expression *HclExpression
	// Part is the native template part of TemplatePart matches.
	Part *HclTemplatePart
	// Region is the recovered region fact of ErrorRegion matches.
	Region *HclErrorRegion
	// Position is the zero-based position within the document's ordered
	// error regions (ErrorRegion matches).
	Position int
}

// Identity returns the exact match identity (RFC 0014 §7.1).
func (m HclMatch) Identity() document.NodeRef { return m.Node }

// HclSyntaxMatch is one snapshot-bound HCL lossless syntax query match
// (RFC 0014 §7.2).
type HclSyntaxMatch struct {
	node    document.NodeRef
	span    document.Span
	kind    HclSyntaxKind
	ordinal int
}

// NodeRef returns the process-local syntax-piece identity.
func (m HclSyntaxMatch) NodeRef() document.NodeRef { return m.node }

// Span returns the exact raw source span.
func (m HclSyntaxMatch) Span() document.Span { return m.span }

// Kind returns the format-specific lossless kind.
func (m HclSyntaxMatch) Kind() HclSyntaxKind { return m.kind }

// Ordinal returns the zero-based source-order position.
func (m HclSyntaxMatch) Ordinal() int { return m.ordinal }

// rankKey identifies one native tree node for the pre-order rank map. The
// (start byte, role) pair is unique per node: every construct owns a
// distinct span start, and the body/block roles distinguish a block from
// its own nested body.
type rankKey struct {
	start int
	role  document.NodeRole
}

// preorderRanks computes the pre-order document rank of every tree node
// and the total ranked tree-node count (query.rs preorder_ranks).
func preorderRanks(doc *HclDocument) (map[rankKey]int, int) {
	ranks := make(map[rankKey]int)
	next := 0
	var walkBody func(*HclBody)
	var walkExpression func(*HclExpression)
	walkExpression = func(expression *HclExpression) {
		ranks[rankKey{start: expression.span.StartByte(), role: document.RoleHclExpression}] = next
		next++
		for _, child := range expression.Children() {
			walkExpression(child)
		}
	}
	walkBody = func(body *HclBody) {
		ranks[rankKey{start: body.startByte, role: document.RoleHclBody}] = next
		next++
		for i := range body.items {
			item := &body.items[i]
			if attribute := item.AsAttribute(); attribute != nil {
				ranks[rankKey{start: attribute.nameSpan.StartByte(), role: document.RoleHclAttribute}] = next
				next++
				walkExpression(attribute.expression)
				continue
			}
			block := item.AsBlock()
			ranks[rankKey{start: block.span.StartByte(), role: document.RoleHclBlock}] = next
			next++
			for j := range block.labels {
				label := &block.labels[j]
				ranks[rankKey{start: label.span.StartByte(), role: document.RoleHclBlockLabel}] = next
				next++
			}
			walkBody(block.body)
		}
	}
	walkBody(doc.body)
	return ranks, next
}

// nativeContext is the execution state of one native query.
type nativeContext struct {
	document  *Document
	native    *HclDocument
	limits    protocol.QueryLimits
	steps     int
	ranks     map[rankKey]int
	treeNodes int
}

func (c *nativeContext) step(ctx context.Context, results int) *protocol.QueryFailure {
	if ctx != nil && ctx.Err() != nil {
		return &protocol.QueryFailure{Kind: protocol.FailureCancelled}
	}
	c.steps++
	if c.steps > c.limits.MaxSteps || results > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	return nil
}

func (c *nativeContext) push(ctx context.Context, output *[]HclMatch, value HclMatch) *protocol.QueryFailure {
	if len(*output)+1 > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	*output = append(*output, value)
	return nil
}

func (c *nativeContext) append(ctx context.Context, output *[]HclMatch, values []HclMatch) *protocol.QueryFailure {
	if len(*output)+len(values) > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	*output = append(*output, values...)
	return nil
}

func (c *nativeContext) rank(key rankKey) int {
	if rank, ok := c.ranks[key]; ok {
		return rank
	}
	return int(^uint(0) >> 1)
}

// sourceOrder is the deterministic structure-order key: the pre-order
// document rank of the node, with the document-level error regions ranked
// after every tree node in their own source order (RFC 0014 §3).
func (c *nativeContext) sourceOrder(item *HclMatch) int {
	switch item.Kind {
	case HclMatchBody:
		return c.rank(rankKey{start: item.Body.startByte, role: document.RoleHclBody})
	case HclMatchAttribute:
		return c.rank(rankKey{start: item.Attribute.nameSpan.StartByte(), role: document.RoleHclAttribute})
	case HclMatchBlock:
		return c.rank(rankKey{start: item.Block.span.StartByte(), role: document.RoleHclBlock})
	case HclMatchBlockLabel:
		return c.rank(rankKey{start: item.Label.span.StartByte(), role: document.RoleHclBlockLabel})
	case HclMatchExpression:
		return c.rank(rankKey{start: item.Expression.span.StartByte(), role: document.RoleHclExpression})
	case HclMatchTemplatePart:
		return c.rank(rankKey{start: item.Part.span.StartByte(), role: document.RoleHclTemplatePart})
	case HclMatchErrorRegion:
		return c.treeNodes + item.Position
	}
	return int(^uint(0) >> 1)
}

// ExecuteHCLNativeQuery executes a validated HCL native semantic query
// against one immutable snapshot (RFC 0014 §7.1). The domain serves both
// profiles. ctx carries cancellation only.
func ExecuteHCLNativeQuery(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) ([]HclMatch, *protocol.QueryFailure) {
	domain := executable.Definition().Domain()
	if domain.ID() != "hcl.native-semantic-query" || domain.Version() != 1 {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	ranks, treeNodes := preorderRanks(doc.document)
	context := &nativeContext{
		document:  doc,
		native:    doc.document,
		limits:    limits,
		ranks:     ranks,
		treeNodes: treeNodes,
	}
	// The root body is the first standard result; it must not bypass
	// result limits (query.rs).
	if failure := context.step(ctx, 1); failure != nil {
		return nil, failure
	}
	input := []HclMatch{{
		Kind: HclMatchBody,
		Node: doc.nodeRef(0, document.RoleHclBody),
		Body: doc.document.body,
	}}
	matches, failure := executeNativeExpression(ctx, executable.Definition().Expression(), input, context)
	if failure != nil {
		return nil, failure
	}
	return applyNativeSelection(matches, executable.Definition().Selection())
}

// ExecuteHCLNativeQueryCursor executes a validated HCL native query and
// exposes the complete ordered result through a cancellable cursor.
func ExecuteHCLNativeQueryCursor(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) (*HclQueryCursor, *protocol.QueryFailure) {
	matches, failure := ExecuteHCLNativeQuery(ctx, executable, doc, limits)
	if failure != nil {
		return nil, failure
	}
	return &HclQueryCursor{ctx: ctx, matches: matches}, nil
}

// HclQueryCursor is the ordered native query result cursor.
type HclQueryCursor struct {
	ctx     context.Context
	matches []HclMatch
}

// NextMatch yields the next match in source order. After exhaustion it
// returns nil; a cancellation after buffering returns a Cancelled
// failure.
func (c *HclQueryCursor) NextMatch() (HclMatch, *protocol.QueryFailure) {
	if len(c.matches) == 0 {
		if c.ctx != nil && c.ctx.Err() != nil {
			return HclMatch{}, &protocol.QueryFailure{Kind: protocol.FailureCancelled}
		}
		return HclMatch{}, nil
	}
	match := c.matches[0]
	c.matches = c.matches[1:]
	return match, nil
}

// ExecuteHCLSyntaxQuery executes a validated HCL lossless syntax query
// against every source piece in raw order (RFC 0014 §7.2). The lossless
// index is always present under both profiles.
func ExecuteHCLSyntaxQuery(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) ([]HclSyntaxMatch, *protocol.QueryFailure) {
	domain := executable.Definition().Domain()
	if domain.ID() != "hcl.lossless-syntax-query" || domain.Version() != 1 {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	context := &syntaxContext{document: doc, limits: limits}
	pieces := doc.index.Pieces()
	if failure := context.step(ctx, len(pieces)); failure != nil {
		return nil, failure
	}
	input := make([]HclSyntaxMatch, 0, len(pieces))
	for ordinal, piece := range pieces {
		input = append(input, HclSyntaxMatch{
			node:    doc.nodeRef(uint64(ordinal), document.RoleHclSyntaxPiece),
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

// ExecuteHCLSyntaxQueryCursor executes a validated HCL syntax query and
// exposes the complete ordered result through a cancellable cursor.
func ExecuteHCLSyntaxQueryCursor(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) (*HclSyntaxQueryCursor, *protocol.QueryFailure) {
	matches, failure := ExecuteHCLSyntaxQuery(ctx, executable, doc, limits)
	if failure != nil {
		return nil, failure
	}
	return &HclSyntaxQueryCursor{ctx: ctx, matches: matches}, nil
}

// HclSyntaxQueryCursor is the ordered syntax query result cursor.
type HclSyntaxQueryCursor struct {
	ctx     context.Context
	matches []HclSyntaxMatch
}

// NextMatch yields the next match in source order; nil after exhaustion.
func (c *HclSyntaxQueryCursor) NextMatch() (HclSyntaxMatch, *protocol.QueryFailure) {
	if len(c.matches) == 0 {
		if c.ctx != nil && c.ctx.Err() != nil {
			return HclSyntaxMatch{}, &protocol.QueryFailure{Kind: protocol.FailureCancelled}
		}
		return HclSyntaxMatch{}, nil
	}
	match := c.matches[0]
	c.matches = c.matches[1:]
	return match, nil
}

// syntaxContext is the execution state of one syntax query.
type syntaxContext struct {
	document *Document
	limits   protocol.QueryLimits
	steps    int
}

func (c *syntaxContext) step(ctx context.Context, results int) *protocol.QueryFailure {
	if ctx != nil && ctx.Err() != nil {
		return &protocol.QueryFailure{Kind: protocol.FailureCancelled}
	}
	c.steps++
	if c.steps > c.limits.MaxSteps || results > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	return nil
}

func (c *syntaxContext) push(ctx context.Context, output *[]HclSyntaxMatch, value HclSyntaxMatch) *protocol.QueryFailure {
	if len(*output)+1 > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	*output = append(*output, value)
	return nil
}

func (c *syntaxContext) append(ctx context.Context, output *[]HclSyntaxMatch, values []HclSyntaxMatch) *protocol.QueryFailure {
	if len(*output)+len(values) > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	*output = append(*output, values...)
	return nil
}

// executeNativeExpression evaluates one native expression (query.rs).
func executeNativeExpression(ctx context.Context, expression *protocol.QueryExpression,
	input []HclMatch, context *nativeContext) ([]HclMatch, *protocol.QueryFailure) {
	switch expression.Kind {
	case protocol.ExpressionInput:
		return input, nil
	case protocol.ExpressionApply:
		values, failure := executeNativeExpression(ctx, expression.Input, input, context)
		if failure != nil {
			return nil, failure
		}
		return applyNativeOperator(ctx, expression.Operator, values, context)
	case protocol.ExpressionConcat:
		var output []HclMatch
		for _, branch := range expression.Branches {
			values, failure := executeNativeExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			if failure := context.append(ctx, &output, values); failure != nil {
				return nil, failure
			}
			if failure := context.step(ctx, len(output)); failure != nil {
				return nil, failure
			}
		}
		return output, nil
	case protocol.ExpressionStructureOrderMerge:
		var output []HclMatch
		for _, branch := range expression.Branches {
			values, failure := executeNativeExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			if failure := context.append(ctx, &output, values); failure != nil {
				return nil, failure
			}
		}
		sort.SliceStable(output, func(i, j int) bool {
			return context.sourceOrder(&output[i]) < context.sourceOrder(&output[j])
		})
		if failure := context.step(ctx, len(output)); failure != nil {
			return nil, failure
		}
		return output, nil
	}
	return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
}

// executeSyntaxExpression evaluates one syntax expression (query.rs).
func executeSyntaxExpression(ctx context.Context, expression *protocol.QueryExpression,
	input []HclSyntaxMatch, context *syntaxContext) ([]HclSyntaxMatch, *protocol.QueryFailure) {
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
		var output []HclSyntaxMatch
		for _, branch := range expression.Branches {
			values, failure := executeSyntaxExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			if failure := context.append(ctx, &output, values); failure != nil {
				return nil, failure
			}
			if failure := context.step(ctx, len(output)); failure != nil {
				return nil, failure
			}
		}
		return output, nil
	case protocol.ExpressionStructureOrderMerge:
		var output []HclSyntaxMatch
		for _, branch := range expression.Branches {
			values, failure := executeSyntaxExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			if failure := context.append(ctx, &output, values); failure != nil {
				return nil, failure
			}
		}
		sort.SliceStable(output, func(i, j int) bool { return output[i].ordinal < output[j].ordinal })
		if failure := context.step(ctx, len(output)); failure != nil {
			return nil, failure
		}
		return output, nil
	}
	return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
}

// applyNativeOperator applies one native operator (query.rs operator
// table; RFC 0014 §7.1).
func applyNativeOperator(ctx context.Context, operator *protocol.OperatorCall,
	input []HclMatch, context *nativeContext) ([]HclMatch, *protocol.QueryFailure) {
	var output []HclMatch
	switch operator.ID() {
	case "hcl.document-body":
		if len(input) > 0 {
			if failure := context.push(ctx, &output, HclMatch{
				Kind: HclMatchBody,
				Node: context.document.nodeRef(0, document.RoleHclBody),
				Body: context.native.body,
			}); failure != nil {
				return nil, failure
			}
		}
	case "hcl.body-items":
		for _, item := range input {
			if item.Kind != HclMatchBody {
				continue
			}
			for i := range item.Body.items {
				bodyItem := &item.Body.items[i]
				if attribute := bodyItem.AsAttribute(); attribute != nil {
					if failure := context.push(ctx, &output, HclMatch{
						Kind:      HclMatchAttribute,
						Node:      context.document.nodeRef(uint64(context.rank(rankKey{start: attribute.nameSpan.StartByte(), role: document.RoleHclAttribute})), document.RoleHclAttribute),
						Attribute: attribute,
					}); failure != nil {
						return nil, failure
					}
				} else {
					block := bodyItem.AsBlock()
					if failure := context.push(ctx, &output, HclMatch{
						Kind:  HclMatchBlock,
						Node:  context.document.nodeRef(uint64(context.rank(rankKey{start: block.span.StartByte(), role: document.RoleHclBlock})), document.RoleHclBlock),
						Block: block,
					}); failure != nil {
						return nil, failure
					}
				}
			}
		}
	case "hcl.body-attributes":
		for _, item := range input {
			if item.Kind != HclMatchBody {
				continue
			}
			for i := range item.Body.items {
				attribute := item.Body.items[i].AsAttribute()
				if attribute == nil {
					continue
				}
				if failure := context.push(ctx, &output, HclMatch{
					Kind:      HclMatchAttribute,
					Node:      context.document.nodeRef(uint64(context.rank(rankKey{start: attribute.nameSpan.StartByte(), role: document.RoleHclAttribute})), document.RoleHclAttribute),
					Attribute: attribute,
				}); failure != nil {
					return nil, failure
				}
			}
		}
	case "hcl.body-blocks":
		for _, item := range input {
			if item.Kind != HclMatchBody {
				continue
			}
			for i := range item.Body.items {
				block := item.Body.items[i].AsBlock()
				if block == nil {
					continue
				}
				if failure := context.push(ctx, &output, HclMatch{
					Kind:  HclMatchBlock,
					Node:  context.document.nodeRef(uint64(context.rank(rankKey{start: block.span.StartByte(), role: document.RoleHclBlock})), document.RoleHclBlock),
					Block: block,
				}); failure != nil {
					return nil, failure
				}
			}
		}
	case "hcl.body-block-type-equals":
		expected := coreStringArgument(operator, "type")
		for _, item := range input {
			if item.Kind != HclMatchBody {
				continue
			}
			for i := range item.Body.items {
				block := item.Body.items[i].AsBlock()
				if block == nil || block.blockType != expected {
					continue
				}
				if failure := context.push(ctx, &output, HclMatch{
					Kind:  HclMatchBlock,
					Node:  context.document.nodeRef(uint64(context.rank(rankKey{start: block.span.StartByte(), role: document.RoleHclBlock})), document.RoleHclBlock),
					Block: block,
				}); failure != nil {
					return nil, failure
				}
			}
		}
	case "hcl.attribute-name":
		for _, item := range input {
			if item.Kind == HclMatchAttribute {
				if failure := context.push(ctx, &output, item); failure != nil {
					return nil, failure
				}
			}
		}
	case "hcl.attribute-name-equals":
		expected := coreStringArgument(operator, "name")
		for _, item := range input {
			if item.Kind == HclMatchAttribute && item.Attribute.name == expected {
				if failure := context.push(ctx, &output, item); failure != nil {
					return nil, failure
				}
			}
		}
	case "hcl.attribute-expression":
		for _, item := range input {
			if item.Kind != HclMatchAttribute {
				continue
			}
			expression := item.Attribute.expression
			if failure := context.push(ctx, &output, HclMatch{
				Kind:       HclMatchExpression,
				Node:       context.document.nodeRef(uint64(context.rank(rankKey{start: expression.span.StartByte(), role: document.RoleHclExpression})), document.RoleHclExpression),
				Expression: expression,
			}); failure != nil {
				return nil, failure
			}
		}
	case "hcl.attribute-literal-value":
		failure := attributeLiteralValue(ctx, operator, input, context, &output)
		if failure != nil {
			return nil, failure
		}
	case "hcl.block-type":
		for _, item := range input {
			if item.Kind == HclMatchBlock {
				if failure := context.push(ctx, &output, item); failure != nil {
					return nil, failure
				}
			}
		}
	case "hcl.block-type-equals":
		expected := coreStringArgument(operator, "type")
		for _, item := range input {
			if item.Kind == HclMatchBlock && item.Block.blockType == expected {
				if failure := context.push(ctx, &output, item); failure != nil {
					return nil, failure
				}
			}
		}
	case "hcl.block-labels":
		for _, item := range input {
			if item.Kind != HclMatchBlock {
				continue
			}
			for j := range item.Block.labels {
				label := &item.Block.labels[j]
				if failure := context.push(ctx, &output, HclMatch{
					Kind:  HclMatchBlockLabel,
					Node:  context.document.nodeRef(uint64(context.rank(rankKey{start: label.span.StartByte(), role: document.RoleHclBlockLabel})), document.RoleHclBlockLabel),
					Label: label,
				}); failure != nil {
					return nil, failure
				}
			}
		}
	case "hcl.block-label-equals":
		expected := coreStringArgument(operator, "label")
		for _, item := range input {
			if item.Kind == HclMatchBlockLabel && item.Label.text == expected {
				if failure := context.push(ctx, &output, item); failure != nil {
					return nil, failure
				}
			}
		}
	case "hcl.block-nested-body":
		for _, item := range input {
			if item.Kind != HclMatchBlock {
				continue
			}
			body := item.Block.body
			if failure := context.push(ctx, &output, HclMatch{
				Kind: HclMatchBody,
				Node: context.document.nodeRef(uint64(context.rank(rankKey{start: body.startByte, role: document.RoleHclBody})), document.RoleHclBody),
				Body: body,
			}); failure != nil {
				return nil, failure
			}
		}
	case "hcl.expression-kind-is":
		expectedName := coreStringArgument(operator, "kind")
		expected, ok := HclExpressionKindNameFromName(expectedName)
		for _, item := range input {
			if item.Kind != HclMatchExpression {
				continue
			}
			if ok && item.Expression.kind.Name() == expected {
				if failure := context.push(ctx, &output, item); failure != nil {
					return nil, failure
				}
			}
		}
	case "hcl.expression-is-literal":
		for _, item := range input {
			if item.Kind == HclMatchExpression && IsLiteralComplete(item.Expression) {
				if failure := context.push(ctx, &output, item); failure != nil {
					return nil, failure
				}
			}
		}
	case "hcl.expression-text":
		for _, item := range input {
			if item.Kind == HclMatchExpression {
				if failure := context.push(ctx, &output, item); failure != nil {
					return nil, failure
				}
			}
		}
	case "hcl.expression-children":
		for _, item := range input {
			if item.Kind != HclMatchExpression {
				continue
			}
			for _, child := range item.Expression.Children() {
				if failure := context.push(ctx, &output, HclMatch{
					Kind:       HclMatchExpression,
					Node:       context.document.nodeRef(uint64(context.rank(rankKey{start: child.span.StartByte(), role: document.RoleHclExpression})), document.RoleHclExpression),
					Expression: child,
				}); failure != nil {
					return nil, failure
				}
			}
		}
	case "hcl.template-parts":
		for _, item := range input {
			if item.Kind != HclMatchExpression || item.Expression.kind.tag != exprTemplate {
				continue
			}
			for j := range item.Expression.kind.parts {
				part := &item.Expression.kind.parts[j]
				if failure := context.push(ctx, &output, HclMatch{
					Kind: HclMatchTemplatePart,
					Node: context.document.nodeRef(uint64(context.rank(rankKey{start: part.span.StartByte(), role: document.RoleHclTemplatePart})), document.RoleHclTemplatePart),
					Part: part,
				}); failure != nil {
					return nil, failure
				}
			}
		}
	case "hcl.tuple-elements":
		for _, item := range input {
			if item.Kind != HclMatchExpression || item.Expression.kind.tag != exprTuple {
				continue
			}
			for _, element := range item.Expression.kind.elements {
				if failure := context.push(ctx, &output, HclMatch{
					Kind:       HclMatchExpression,
					Node:       context.document.nodeRef(uint64(context.rank(rankKey{start: element.span.StartByte(), role: document.RoleHclExpression})), document.RoleHclExpression),
					Expression: element,
				}); failure != nil {
					return nil, failure
				}
			}
		}
	case "hcl.object-entries":
		for _, item := range input {
			if item.Kind != HclMatchExpression || item.Expression.kind.tag != exprObject {
				continue
			}
			for j := range item.Expression.kind.entries {
				value := item.Expression.kind.entries[j].value
				if failure := context.push(ctx, &output, HclMatch{
					Kind:       HclMatchExpression,
					Node:       context.document.nodeRef(uint64(context.rank(rankKey{start: value.span.StartByte(), role: document.RoleHclExpression})), document.RoleHclExpression),
					Expression: value,
				}); failure != nil {
					return nil, failure
				}
			}
		}
	case "hcl.error-regions":
		if len(input) > 0 {
			for position, region := range context.document.errorRegions {
				if failure := context.push(ctx, &output, HclMatch{
					Kind:     HclMatchErrorRegion,
					Node:     context.document.nodeRef(uint64(context.treeNodes+position), document.RoleHclErrorRegion),
					Region:   &region,
					Position: position,
				}); failure != nil {
					return nil, failure
				}
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
			if seen[item.Node] {
				continue
			}
			seen[item.Node] = true
			if failure := context.push(ctx, &output, item); failure != nil {
				return nil, failure
			}
		}
	}
	if failure := context.step(ctx, len(output)); failure != nil {
		return nil, failure
	}
	return output, nil
}

// attributeLiteralValue implements `hcl.attribute-literal-value`: the
// typed literal accessor family (RFC 0014 §7.1).
//
// Each accessor (`as-string`, `as-integer`, `as-real`, `as-boolean-is`,
// `as-null-is`) validates that the expression is literal-complete and of
// the requested type before returning. A non-literal expression is
// reported as `hcl.query.non-literal@1`; a type mismatch is reported as
// `hcl.query.type-mismatch@1`. Neither is ever a null, empty, or converted
// result.
func attributeLiteralValue(ctx context.Context, operator *protocol.OperatorCall,
	input []HclMatch, context *nativeContext, output *[]HclMatch) *protocol.QueryFailure {
	accessor := coreStringArgument(operator, "accessor")
	var expected literalKindTag
	switch accessor {
	case "as-string":
		expected = literalString
	case "as-integer":
		expected = literalInteger
	case "as-real":
		expected = literalDecimal
	case "as-boolean-is":
		expected = literalBoolean
	case "as-null-is":
		expected = literalNull
	default:
		return &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument, Argument: "accessor"}
	}
	for _, item := range input {
		var expression *HclExpression
		switch item.Kind {
		case HclMatchExpression:
			expression = item.Expression
		case HclMatchAttribute:
			expression = item.Attribute.expression
		default:
			continue
		}
		value, err := LiteralValue(expression)
		if err != nil {
			return &protocol.QueryFailure{Kind: protocol.FailureTargetUnavailable}
		}
		if value.tag != expected {
			return &protocol.QueryFailure{Kind: protocol.FailureRequiredTypeMismatch}
		}
		if failure := context.push(ctx, output, item); failure != nil {
			return failure
		}
	}
	return nil
}

// applySyntaxOperator applies one syntax operator (query.rs operator
// table; RFC 0014 §7.2).
func applySyntaxOperator(ctx context.Context, operator *protocol.OperatorCall,
	input []HclSyntaxMatch, context *syntaxContext) ([]HclSyntaxMatch, *protocol.QueryFailure) {
	var output []HclSyntaxMatch
	switch operator.ID() {
	case "hcl.syntax-kind-is":
		expected, ok := HclSyntaxKindFromName(coreStringArgument(operator, "kind"))
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument, Argument: "kind"}
		}
		for _, item := range input {
			if item.kind == expected {
				if failure := context.push(ctx, &output, item); failure != nil {
					return nil, failure
				}
			}
		}
	case "hcl.syntax-text-equals":
		expected := coreStringArgument(operator, "text")
		text, _ := context.document.source.DecodedText()
		for _, item := range input {
			if text[item.span.StartByte():item.span.EndByte()] == expected {
				if failure := context.push(ctx, &output, item); failure != nil {
					return nil, failure
				}
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
			if failure := context.push(ctx, &output, item); failure != nil {
				return nil, failure
			}
		}
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

// applyNativeSelection applies the native cardinality selection.
func applyNativeSelection(values []HclMatch, selection protocol.QuerySelection) ([]HclMatch, *protocol.QueryFailure) {
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
func applySyntaxSelection(values []HclSyntaxMatch, selection protocol.QuerySelection) ([]HclSyntaxMatch, *protocol.QueryFailure) {
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
