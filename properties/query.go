package properties

import (
	"context"
	"sort"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the Java Properties native and lossless-syntax
// query domains (consema-properties query.rs; RFC 0010 §10). Native
// matches carry snapshot-bound roles and exact raw spans in source order;
// syntax matches are source-order pieces. Key matching takes exact UTF-16
// code units encoded as `UTF16BE/1` and never normalizes Unicode or case;
// duplicate matches remain distinct and ordered.

// PropertiesMatchKind is the closed native match variant (query.rs:13-74).
type PropertiesMatchKind string

// The five frozen native match variants.
const (
	// PropertiesMatchDocument is the complete Properties document.
	PropertiesMatchDocument PropertiesMatchKind = "Document"
	// PropertiesMatchProperty is one duplicate-preserving property
	// association.
	PropertiesMatchProperty PropertiesMatchKind = "Property"
	// PropertiesMatchNaturalLine is one exact natural source line.
	PropertiesMatchNaturalLine PropertiesMatchKind = "NaturalLine"
	// PropertiesMatchLogicalLine is one property or recovered-error logical
	// line.
	PropertiesMatchLogicalLine PropertiesMatchKind = "LogicalLine"
	// PropertiesMatchEscape is one retained property escape.
	PropertiesMatchEscape PropertiesMatchKind = "Escape"
)

// PropertiesMatch is one snapshot-bound Java Properties native semantic
// query match (query.rs:13-74). Only the fields of the declared Kind are
// meaningful.
type PropertiesMatch struct {
	// Kind is the match variant.
	Kind PropertiesMatchKind
	// Ordinal is the zero-based source-order ordinal of Property,
	// NaturalLine, LogicalLine, and Escape matches.
	Ordinal int
	// Node is the match identity.
	Node document.NodeRef
	// LogicalLine is the owning logical-line identity of Property matches.
	LogicalLine document.NodeRef
	// Key is the exact Java UTF-16 key of Property matches.
	Key JavaString
	// Value is the exact Java UTF-16 value of Property matches.
	Value JavaString
	// ValueState is the value state of Property matches.
	ValueState PropertiesValueState
	// DuplicateGroup is the exact-key duplicate group of Property matches.
	DuplicateGroup *uint32
	// Span is the exact raw range of NaturalLine, LogicalLine, and Escape
	// matches, and the property range of Property matches.
	Span document.Span
	// InKey reports whether the output belongs to the property key of
	// Escape matches.
	InKey bool
	// EscapeKind is the escape behavior of Escape matches.
	EscapeKind PropertiesEscapeKind
	// OutputStart is the half-open Java UTF-16 output start of Escape
	// matches.
	OutputStart int
	// OutputEnd is the exclusive Java UTF-16 output boundary of Escape
	// matches.
	OutputEnd int
	// identity is the process-local identity used for
	// core.distinct-by-identity and structure-order-merge.
	identity document.NodeRef
}

// NodeRef returns the primary match identity.
func (m *PropertiesMatch) NodeRef() document.NodeRef { return m.identity }

// PropertiesSyntaxMatch is one snapshot-bound Java Properties lossless
// syntax query match (query.rs:88-121).
type PropertiesSyntaxMatch struct {
	node    document.NodeRef
	span    document.Span
	kind    PropertiesSyntaxKind
	ordinal int
}

// NodeRef returns the process-local syntax-piece identity.
func (m PropertiesSyntaxMatch) NodeRef() document.NodeRef { return m.node }

// Span returns the exact raw source span.
func (m PropertiesSyntaxMatch) Span() document.Span { return m.span }

// Kind returns the format-specific lossless kind.
func (m PropertiesSyntaxMatch) Kind() PropertiesSyntaxKind { return m.kind }

// Ordinal returns the zero-based source-order position.
func (m PropertiesSyntaxMatch) Ordinal() int { return m.ordinal }

// QueryTerminalState is the terminal state of an ordered query cursor
// (query.rs: the Rust QueryTerminalState).
type QueryTerminalState string

// The three frozen terminal states.
const (
	// QueryTerminalCompleted means every match was yielded.
	QueryTerminalCompleted QueryTerminalState = "Completed"
	// QueryTerminalCancelled means yielding stopped at cancellation.
	QueryTerminalCancelled QueryTerminalState = "Cancelled"
	// QueryTerminalFailed means execution failed before a result existed.
	QueryTerminalFailed QueryTerminalState = "Failed"
)

// ExecutePropertiesQuery executes a validated Properties native semantic
// query against one immutable snapshot (query.rs:124-150). The context is
// used for cancellation and deadlines only. Steps and result counts are
// bounded by limits; exceeding either is core.query.resource-limit@1.
func ExecutePropertiesQuery(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) ([]PropertiesMatch, *protocol.QueryFailure) {
	domain := executable.Definition().Domain()
	if domain.ID() != "java-properties.native-semantic-query" || domain.Version() != 1 {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	context := &propertiesQueryContext{ctx: ctx, document: doc, limits: limits}
	if failure := context.step(1); failure != nil {
		return nil, failure
	}
	input := []PropertiesMatch{{
		Kind: PropertiesMatchDocument, Node: doc.NodeRef(),
		identity: doc.NodeRef(),
	}}
	matches, failure := executePropertiesExpression(ctx, executable.Definition().Expression(),
		input, context)
	if failure != nil {
		return nil, failure
	}
	matches, failure = applyPropertiesSelection(matches, executable.Definition().Selection())
	if failure != nil {
		return nil, failure
	}
	return matches, nil
}

// ExecutePropertiesQueryCursor executes a validated Properties native
// query and exposes the complete result through an ordered cursor with
// cancellation (query.rs:152-164).
func ExecutePropertiesQueryCursor(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) (*PropertiesQueryCursor, *protocol.QueryFailure) {
	matches, failure := ExecutePropertiesQuery(ctx, executable, doc, limits)
	if failure != nil {
		return nil, failure
	}
	return &PropertiesQueryCursor{ctx: ctx, matches: matches}, nil
}

// ExecutePropertiesSyntaxQuery executes a validated Properties lossless
// syntax query in raw source order (query.rs:166-211).
func ExecutePropertiesSyntaxQuery(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) ([]PropertiesSyntaxMatch, *protocol.QueryFailure) {
	domain := executable.Definition().Domain()
	if domain.ID() != "java-properties.lossless-syntax-query" || domain.Version() != 1 {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	context := &propertiesQueryContext{ctx: ctx, document: doc, limits: limits}
	pieces := doc.index.Pieces()
	if failure := context.step(len(pieces)); failure != nil {
		return nil, failure
	}
	input := make([]PropertiesSyntaxMatch, 0, len(pieces))
	for ordinal, piece := range pieces {
		input = append(input, PropertiesSyntaxMatch{
			node:    doc.authority.NodeRef(uint64(ordinal), document.RolePropertiesSyntaxPiece),
			span:    piece.span,
			kind:    doc.syntaxKinds[ordinal],
			ordinal: ordinal,
		})
	}
	matches, failure := executePropertiesSyntaxExpression(ctx,
		executable.Definition().Expression(), input, context)
	if failure != nil {
		return nil, failure
	}
	matches, failure = applySyntaxSelection(matches, executable.Definition().Selection())
	if failure != nil {
		return nil, failure
	}
	return matches, nil
}

// ExecutePropertiesSyntaxQueryCursor executes a validated Properties
// syntax query through an ordered cursor with cancellation (query.rs:
// 213-225).
func ExecutePropertiesSyntaxQueryCursor(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) (*PropertiesSyntaxQueryCursor, *protocol.QueryFailure) {
	matches, failure := ExecutePropertiesSyntaxQuery(ctx, executable, doc, limits)
	if failure != nil {
		return nil, failure
	}
	return &PropertiesSyntaxQueryCursor{ctx: ctx, matches: matches}, nil
}

// PropertiesQueryCursor is the ordered native match cursor (query.rs:152-164).
type PropertiesQueryCursor struct {
	ctx      context.Context
	matches  []PropertiesMatch
	next     int
	terminal *QueryTerminalState
}

// Next yields the next match in result order, or nil when exhausted or
// cancelled.
func (c *PropertiesQueryCursor) Next() *PropertiesMatch {
	if c.terminal != nil {
		return nil
	}
	if c.ctx != nil {
		select {
		case <-c.ctx.Done():
			terminal := QueryTerminalCancelled
			c.terminal = &terminal
			return nil
		default:
		}
	}
	if c.next >= len(c.matches) {
		terminal := QueryTerminalCompleted
		c.terminal = &terminal
		return nil
	}
	match := c.matches[c.next]
	c.next++
	return &match
}

// TerminalState returns the terminal state once yielding stopped.
func (c *PropertiesQueryCursor) TerminalState() *QueryTerminalState { return c.terminal }

// PropertiesSyntaxQueryCursor is the ordered syntax match cursor.
type PropertiesSyntaxQueryCursor struct {
	ctx      context.Context
	matches  []PropertiesSyntaxMatch
	next     int
	terminal *QueryTerminalState
}

// Next yields the next syntax match in result order, or nil when exhausted
// or cancelled.
func (c *PropertiesSyntaxQueryCursor) Next() *PropertiesSyntaxMatch {
	if c.terminal != nil {
		return nil
	}
	if c.ctx != nil {
		select {
		case <-c.ctx.Done():
			terminal := QueryTerminalCancelled
			c.terminal = &terminal
			return nil
		default:
		}
	}
	if c.next >= len(c.matches) {
		terminal := QueryTerminalCompleted
		c.terminal = &terminal
		return nil
	}
	match := c.matches[c.next]
	c.next++
	return &match
}

// TerminalState returns the terminal state once yielding stopped.
func (c *PropertiesSyntaxQueryCursor) TerminalState() *QueryTerminalState { return c.terminal }

type propertiesQueryContext struct {
	ctx      context.Context
	document *Document
	limits   protocol.QueryLimits
	steps    int
}

func (c *propertiesQueryContext) step(results int) *protocol.QueryFailure {
	if c.ctx != nil {
		select {
		case <-c.ctx.Done():
			return &protocol.QueryFailure{Kind: protocol.FailureCancelled}
		default:
		}
	}
	c.steps++
	if c.steps > c.limits.MaxSteps || results > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	return nil
}

func checkCancelled(ctx context.Context) *protocol.QueryFailure {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return &protocol.QueryFailure{Kind: protocol.FailureCancelled}
	default:
		return nil
	}
}

func (c *propertiesQueryContext) push(output *[]PropertiesMatch, value PropertiesMatch) *protocol.QueryFailure {
	if len(*output)+1 > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	*output = append(*output, value)
	return nil
}

func (c *propertiesQueryContext) append(output *[]PropertiesMatch, values []PropertiesMatch) *protocol.QueryFailure {
	if len(*output)+len(values) > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	*output = append(*output, values...)
	return nil
}

func (c *propertiesQueryContext) propertyMatch(ordinal int) PropertiesMatch {
	property := &c.document.properties[ordinal]
	return PropertiesMatch{
		Kind:           PropertiesMatchProperty,
		Ordinal:        ordinal,
		Node:           property.node,
		LogicalLine:    property.logicalLine,
		Key:            property.key,
		Value:          property.value,
		ValueState:     property.valueState,
		DuplicateGroup: property.duplicateGroup,
		Span:           property.span,
		identity:       property.node,
	}
}

func (c *propertiesQueryContext) naturalLineMatch(ordinal int) PropertiesMatch {
	line := &c.document.naturalLines[ordinal]
	return PropertiesMatch{
		Kind:     PropertiesMatchNaturalLine,
		Ordinal:  ordinal,
		Node:     line.node,
		Span:     line.span,
		identity: line.node,
	}
}

func (c *propertiesQueryContext) logicalLineMatch(ordinal int) PropertiesMatch {
	line := &c.document.logicalLines[ordinal]
	return PropertiesMatch{
		Kind:     PropertiesMatchLogicalLine,
		Ordinal:  ordinal,
		Node:     line.node,
		identity: line.node,
	}
}

func (c *propertiesQueryContext) escapeMatch(ordinal int) PropertiesMatch {
	escape := &c.document.escapes[ordinal]
	return PropertiesMatch{
		Kind:        PropertiesMatchEscape,
		Ordinal:     ordinal,
		Node:        escape.node,
		Span:        escape.span,
		InKey:       escape.inKey,
		EscapeKind:  escape.kind,
		OutputStart: escape.outputStart,
		OutputEnd:   escape.outputEnd,
		identity:    escape.node,
	}
}

// logicalLineSpan resolves the raw span of one logical-line match for
// structure-order merging (query.rs:622-632).
func (c *propertiesQueryContext) logicalLineSpan(node document.NodeRef) int {
	for _, line := range c.document.logicalLines {
		if line.node != node {
			continue
		}
		if len(line.naturalLines) > 0 {
			for _, natural := range c.document.naturalLines {
				if natural.node == line.naturalLines[0] {
					return natural.span.StartByte()
				}
			}
		}
		return 0
	}
	return 0
}

func executePropertiesExpression(ctx context.Context, expression *protocol.QueryExpression,
	input []PropertiesMatch, context *propertiesQueryContext) ([]PropertiesMatch, *protocol.QueryFailure) {
	switch expression.Kind {
	case protocol.ExpressionInput:
		return input, nil
	case protocol.ExpressionApply:
		inner, failure := executePropertiesExpression(ctx, expression.Input, input, context)
		if failure != nil {
			return nil, failure
		}
		return applyPropertiesOperator(ctx, expression.Operator, inner, context)
	case protocol.ExpressionConcat:
		var output []PropertiesMatch
		for _, branch := range expression.Branches {
			matches, failure := executePropertiesExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			if failure := context.append(&output, matches); failure != nil {
				return nil, failure
			}
			if failure := context.step(len(output)); failure != nil {
				return nil, failure
			}
		}
		return output, nil
	case protocol.ExpressionStructureOrderMerge:
		var output []PropertiesMatch
		for _, branch := range expression.Branches {
			matches, failure := executePropertiesExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			if failure := context.append(&output, matches); failure != nil {
				return nil, failure
			}
		}
		sort.SliceStable(output, func(i, j int) bool {
			leftStart, leftOrdinal := propertiesSourceOrder(context, &output[i])
			rightStart, rightOrdinal := propertiesSourceOrder(context, &output[j])
			if leftStart != rightStart {
				return leftStart < rightStart
			}
			return leftOrdinal < rightOrdinal
		})
		if failure := context.step(len(output)); failure != nil {
			return nil, failure
		}
		return output, nil
	}
	return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
}

// propertiesSourceOrder resolves the deterministic source-order key of one
// native match (query.rs:609-634).
func propertiesSourceOrder(context *propertiesQueryContext, match *PropertiesMatch) (int, int) {
	switch match.Kind {
	case PropertiesMatchDocument:
		return 0, 0
	case PropertiesMatchProperty:
		return match.Span.StartByte(), match.Ordinal
	case PropertiesMatchNaturalLine, PropertiesMatchEscape:
		return match.Span.StartByte(), match.Ordinal
	case PropertiesMatchLogicalLine:
		return context.logicalLineSpan(match.Node), match.Ordinal
	}
	return 0, 0
}

// applyPropertiesOperator evaluates one native operator (query.rs:398-532).
func applyPropertiesOperator(ctx context.Context, operator *protocol.OperatorCall,
	input []PropertiesMatch, context *propertiesQueryContext) ([]PropertiesMatch, *protocol.QueryFailure) {
	if failure := checkCancelled(ctx); failure != nil {
		return nil, failure
	}
	var output []PropertiesMatch
	switch operator.ID() {
	case "properties.document-properties":
		for _, item := range input {
			if item.Kind != PropertiesMatchDocument {
				continue
			}
			for ordinal := range context.document.properties {
				if failure := context.push(&output, context.propertyMatch(ordinal)); failure != nil {
					return nil, failure
				}
			}
		}
	case "properties.natural-lines":
		for _, item := range input {
			if item.Kind != PropertiesMatchDocument {
				continue
			}
			for ordinal := range context.document.naturalLines {
				if failure := context.push(&output, context.naturalLineMatch(ordinal)); failure != nil {
					return nil, failure
				}
			}
		}
	case "properties.logical-lines":
		for _, item := range input {
			if item.Kind != PropertiesMatchDocument {
				continue
			}
			for ordinal := range context.document.logicalLines {
				if failure := context.push(&output, context.logicalLineMatch(ordinal)); failure != nil {
					return nil, failure
				}
			}
		}
	case "properties.logical-line-natural-lines":
		for _, item := range input {
			if item.Kind != PropertiesMatchLogicalLine {
				continue
			}
			for _, logical := range context.document.logicalLines {
				if logical.node != item.Node {
					continue
				}
				for _, natural := range logical.naturalLines {
					for ordinal, candidate := range context.document.naturalLines {
						if candidate.node == natural {
							if failure := context.push(&output,
								context.naturalLineMatch(ordinal)); failure != nil {
								return nil, failure
							}
							break
						}
					}
				}
			}
		}
	case "properties.property-key-equals":
		expected, ok := bytesArgument(operator, "key")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "key"}
		}
		for _, item := range input {
			if item.Kind == PropertiesMatchProperty && javaStringEqualsUTF16BE(&item.Key, expected) {
				if failure := context.push(&output, item); failure != nil {
					return nil, failure
				}
			}
		}
	case "properties.property-value-state-is":
		expected, ok := stringArgument(operator, "state")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "state"}
		}
		var state PropertiesValueState
		switch expected {
		case "ImplicitEmpty":
			state = ValueStateImplicitEmpty
		case "ExplicitEmpty":
			state = ValueStateExplicitEmpty
		case "Present":
			state = ValueStatePresent
		default:
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "state"}
		}
		for _, item := range input {
			if item.Kind == PropertiesMatchProperty && item.ValueState == state {
				if failure := context.push(&output, item); failure != nil {
					return nil, failure
				}
			}
		}
	case "properties.property-escapes":
		for _, item := range input {
			if item.Kind != PropertiesMatchProperty {
				continue
			}
			for ordinal, escape := range context.document.escapes {
				if escape.property == item.Node {
					if failure := context.push(&output, context.escapeMatch(ordinal)); failure != nil {
						return nil, failure
					}
				}
			}
		}
	case "properties.duplicate-group":
		for _, item := range input {
			if item.Kind != PropertiesMatchProperty || item.DuplicateGroup == nil {
				continue
			}
			for ordinal, property := range context.document.properties {
				if property.duplicateGroup != nil && *property.duplicateGroup == *item.DuplicateGroup {
					if failure := context.push(&output, context.propertyMatch(ordinal)); failure != nil {
						return nil, failure
					}
				}
			}
		}
	case "core.take":
		count, ok := integerArgument(operator, "count")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "count"}
		}
		if count < len(input) {
			input = input[:count]
		}
		for _, item := range input {
			if failure := context.push(&output, item); failure != nil {
				return nil, failure
			}
		}
	case "core.distinct-by-identity":
		seen := make(map[document.NodeRef]bool, len(input))
		for _, item := range input {
			if seen[item.identity] {
				continue
			}
			seen[item.identity] = true
			if failure := context.push(&output, item); failure != nil {
				return nil, failure
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

func executePropertiesSyntaxExpression(ctx context.Context, expression *protocol.QueryExpression,
	input []PropertiesSyntaxMatch, context *propertiesQueryContext) ([]PropertiesSyntaxMatch, *protocol.QueryFailure) {
	switch expression.Kind {
	case protocol.ExpressionInput:
		return input, nil
	case protocol.ExpressionApply:
		inner, failure := executePropertiesSyntaxExpression(ctx, expression.Input, input, context)
		if failure != nil {
			return nil, failure
		}
		return applySyntaxOperator(ctx, expression.Operator, inner, context)
	case protocol.ExpressionConcat:
		var output []PropertiesSyntaxMatch
		for _, branch := range expression.Branches {
			matches, failure := executePropertiesSyntaxExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			output = append(output, matches...)
			if len(output) > context.limits.MaxResults {
				return nil, &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
			}
			if failure := context.step(len(output)); failure != nil {
				return nil, failure
			}
		}
		return output, nil
	case protocol.ExpressionStructureOrderMerge:
		var output []PropertiesSyntaxMatch
		for _, branch := range expression.Branches {
			matches, failure := executePropertiesSyntaxExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			output = append(output, matches...)
			if len(output) > context.limits.MaxResults {
				return nil, &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
			}
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

// applySyntaxOperator evaluates one lossless-syntax operator (query.rs:
// 534-607).
func applySyntaxOperator(ctx context.Context, operator *protocol.OperatorCall,
	input []PropertiesSyntaxMatch, context *propertiesQueryContext) ([]PropertiesSyntaxMatch, *protocol.QueryFailure) {
	if failure := checkCancelled(ctx); failure != nil {
		return nil, failure
	}
	var output []PropertiesSyntaxMatch
	switch operator.ID() {
	case "properties.syntax-kind-is":
		expected, ok := stringArgument(operator, "kind")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "kind"}
		}
		kind, valid := PropertiesSyntaxKindFromName(expected)
		if !valid {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "kind"}
		}
		for _, item := range input {
			if item.kind == kind {
				output = append(output, item)
			}
		}
	case "properties.syntax-text-equals":
		expected, ok := stringArgument(operator, "text")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "text"}
		}
		text, ok := context.document.source.DecodedText()
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureTargetUnavailable}
		}
		for _, item := range input {
			if decodedSpanText(context.document, item.span, text) == expected {
				output = append(output, item)
			}
		}
	case "properties.syntax-raw-bytes-equals":
		expected, ok := bytesArgument(operator, "bytes")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "bytes"}
		}
		raw := context.document.source.Bytes()
		for _, item := range input {
			span := item.span
			if span.EndByte() <= len(raw) &&
				string(raw[span.StartByte():span.EndByte()]) == string(expected) {
				output = append(output, item)
			}
		}
	case "properties.syntax-utf16be-equals":
		expected, ok := bytesArgument(operator, "code_units")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "code_units"}
		}
		text, ok := context.document.source.DecodedText()
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureTargetUnavailable}
		}
		for _, item := range input {
			if unicodeTextEqualsUTF16BE(decodedSpanText(context.document, item.span, text), expected) {
				output = append(output, item)
			}
		}
	case "core.take":
		count, ok := integerArgument(operator, "count")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "count"}
		}
		if count < len(input) {
			input = input[:count]
		}
		output = append(output, input...)
	case "core.distinct-by-identity":
		seen := make(map[document.NodeRef]bool, len(input))
		for _, item := range input {
			if seen[item.node] {
				continue
			}
			seen[item.node] = true
			output = append(output, item)
		}
	default:
		return nil, &protocol.QueryFailure{Kind: protocol.FailureUnknownOperator,
			Operator: operator.ID(), Version: operator.Version()}
	}
	if len(output) > context.limits.MaxResults {
		return nil, &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	if failure := context.step(len(output)); failure != nil {
		return nil, failure
	}
	return output, nil
}

// decodedSpanText resolves one syntax span into decoded UTF-8 text
// (query.rs:636-651).
func decodedSpanText(document *Document, span document.Span, text string) string {
	start, err := document.source.DecodedPosition(span.StartByte())
	if err != nil {
		return ""
	}
	end, err := document.source.DecodedPosition(span.EndByte())
	if err != nil {
		return ""
	}
	return text[start.DecodedUTF8Byte:end.DecodedUTF8Byte]
}

// javaStringEqualsUTF16BE compares exact Java code units against `UTF16BE/1`
// bytes (query.rs:653-661).
func javaStringEqualsUTF16BE(value *JavaString, expected []byte) bool {
	if len(value.units)*2 != len(expected) {
		return false
	}
	for index, unit := range value.units {
		if byte(unit>>8) != expected[index*2] || byte(unit) != expected[index*2+1] {
			return false
		}
	}
	return true
}

// unicodeTextEqualsUTF16BE compares one well-formed decoded text against
// `UTF16BE/1` bytes (query.rs:662-673).
func unicodeTextEqualsUTF16BE(value string, expected []byte) bool {
	if len(expected)%2 != 0 {
		return false
	}
	index := 0
	for _, character := range value {
		if character > 0xFFFF {
			first := character - 0x10000
			high := 0xD800 + (first >> 10)
			low := 0xDC00 + (first & 0x3FF)
			if index+4 > len(expected) ||
				byte(high>>8) != expected[index] || byte(high) != expected[index+1] ||
				byte(low>>8) != expected[index+2] || byte(low) != expected[index+3] {
				return false
			}
			index += 4
			continue
		}
		unit := uint16(character)
		if index+2 > len(expected) ||
			byte(unit>>8) != expected[index] || byte(unit) != expected[index+1] {
			return false
		}
		index += 2
	}
	return index == len(expected)
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

func bytesArgument(operator *protocol.OperatorCall, name string) ([]byte, bool) {
	value, ok := operator.Arguments()[name]
	if !ok {
		return nil, false
	}
	bytes, ok := value.(core.Bytes)
	if !ok {
		return nil, false
	}
	return []byte(bytes), true
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

func applyPropertiesSelection(matches []PropertiesMatch,
	selection protocol.QuerySelection) ([]PropertiesMatch, *protocol.QueryFailure) {
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

func applySyntaxSelection(matches []PropertiesSyntaxMatch,
	selection protocol.QuerySelection) ([]PropertiesSyntaxMatch, *protocol.QueryFailure) {
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
