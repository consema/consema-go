package json

import (
	"context"
	"sort"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements JSON native and lossless syntax query execution
// (consema-rs/consema-json/src/query.rs). Execution mirrors the Rust engine:
// deterministic operator application, result and step budgets, and
// cancellation through the caller's context.

// JsonMatchKind is the closed JSON native match category.
type JsonMatchKind uint8

// The three frozen native match categories.
const (
	// JsonMatchValue is a JSON native value.
	JsonMatchValue JsonMatchKind = iota
	// JsonMatchObjectMember is an ordered object member with duplicate
	// identity preserved.
	JsonMatchObjectMember
	// JsonMatchArrayElement is an ordered array element.
	JsonMatchArrayElement
)

// JsonMatch is one snapshot-bound JSON native semantic query match
// (consema-json query.rs).
type JsonMatch struct {
	// Kind is the closed match category.
	Kind JsonMatchKind
	// Node is the exact value identity (Value), member association
	// identity (ObjectMember), or element association identity
	// (ArrayElement).
	Node document.NodeRef
	// ValueKind is the native category of Value matches; nil when locally
	// unavailable.
	ValueKind *JsonValueKind
	// Ordinal is the zero-based member or element ordinal.
	Ordinal int
	// Name is the decoded member name when available.
	Name *string
	// Key is the member key identity (ObjectMember).
	Key document.NodeRef
	// Value is the member or element value identity.
	Value document.NodeRef
}

// Identity returns the exact match identity (consema-json query.rs).
func (m JsonMatch) Identity() document.NodeRef { return m.Node }

// JsonSyntaxMatch is one snapshot-bound JSON lossless syntax query match
// (consema-json query.rs).
type JsonSyntaxMatch struct {
	node    document.NodeRef
	span    document.Span
	kind    JsonSyntaxKind
	ordinal int
}

// NodeRef returns the process-local syntax-piece identity.
func (m JsonSyntaxMatch) NodeRef() document.NodeRef { return m.node }

// Span returns the exact raw source span.
func (m JsonSyntaxMatch) Span() document.Span { return m.span }

// Kind returns the format-specific lossless kind.
func (m JsonSyntaxMatch) Kind() JsonSyntaxKind { return m.kind }

// Ordinal returns the zero-based source-order position.
func (m JsonSyntaxMatch) Ordinal() int { return m.ordinal }

// ExecuteJSONQuery executes a validated JSON native semantic query against
// one immutable snapshot (consema-json query.rs). The executable
// must have been validated and bound against the capabilities; the
// domain/profile version contract is rechecked here (JSON5 documents
// require the v2 domain). ctx carries cancellation only.
func ExecuteJSONQuery(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) ([]JsonMatch, *protocol.QueryFailure) {
	domain := executable.Definition().Domain()
	if domain.ID() != "json.native-semantic-query" || (domain.Version() != 1 && domain.Version() != 2) ||
		(doc.profile.isJSON5() && domain.Version() != 2) {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	context := &queryContext{document: doc, limits: limits}
	// The root is the first standard result; it must not bypass result
	// limits (query.rs).
	if failure := context.step(ctx, 1); failure != nil {
		return nil, failure
	}
	root := doc.Root()
	var kind *JsonValueKind
	if availability := root.Kind(); availability.IsAvailable() {
		value := availability.Value()
		kind = &value
	}
	input := []JsonMatch{{
		Kind:      JsonMatchValue,
		Node:      root.NodeRef(),
		ValueKind: kind,
	}}
	matches, failure := executeExpression(ctx, executable.Definition().Expression(), input, context)
	if failure != nil {
		return nil, failure
	}
	return applySelection(matches, executable.Definition().Selection())
}

// ExecuteJSONQueryCursor executes a validated JSON native query and
// exposes the complete ordered result through a cancellable cursor. The
// cursor yields the buffered matches in order; cancellation is checked
// before each yield and after exhaustion.
func ExecuteJSONQueryCursor(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) (*JsonQueryCursor, *protocol.QueryFailure) {
	matches, failure := ExecuteJSONQuery(ctx, executable, doc, limits)
	if failure != nil {
		return nil, failure
	}
	return &JsonQueryCursor{ctx: ctx, matches: matches}, nil
}

// JsonQueryCursor is the ordered native query result cursor.
type JsonQueryCursor struct {
	ctx     context.Context
	matches []JsonMatch
}

// NextMatch yields the next match in source order. After exhaustion it
// returns nil; a cancellation after buffering returns a Cancelled
// failure.
func (c *JsonQueryCursor) NextMatch() (JsonMatch, *protocol.QueryFailure) {
	if len(c.matches) == 0 {
		if c.ctx != nil && c.ctx.Err() != nil {
			return JsonMatch{}, &protocol.QueryFailure{Kind: protocol.FailureCancelled}
		}
		return JsonMatch{}, nil
	}
	match := c.matches[0]
	c.matches = c.matches[1:]
	return match, nil
}

// ExecuteJSONSyntaxQuery executes a validated JSON lossless syntax query
// against every source piece in raw order (consema-json query.rs).
func ExecuteJSONSyntaxQuery(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) ([]JsonSyntaxMatch, *protocol.QueryFailure) {
	domain := executable.Definition().Domain()
	if domain.ID() != "json.lossless-syntax-query" || (domain.Version() != 1 && domain.Version() != 2) ||
		(doc.profile.isJSON5() && domain.Version() != 2) {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	context := &queryContext{document: doc, limits: limits}
	pieces := doc.structuralIndex.Pieces()
	if failure := context.step(ctx, len(pieces)); failure != nil {
		return nil, failure
	}
	input := make([]JsonSyntaxMatch, 0, len(pieces))
	for ordinal, piece := range pieces {
		input = append(input, JsonSyntaxMatch{
			node:    doc.authority.NodeRef(uint64(ordinal), document.RoleJsonSyntaxPiece),
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

// ExecuteJSONSyntaxQueryCursor executes a validated JSON syntax query and
// exposes the complete ordered result through a cancellable cursor.
func ExecuteJSONSyntaxQueryCursor(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) (*JsonSyntaxQueryCursor, *protocol.QueryFailure) {
	matches, failure := ExecuteJSONSyntaxQuery(ctx, executable, doc, limits)
	if failure != nil {
		return nil, failure
	}
	return &JsonSyntaxQueryCursor{ctx: ctx, matches: matches}, nil
}

// JsonSyntaxQueryCursor is the ordered syntax query result cursor.
type JsonSyntaxQueryCursor struct {
	ctx     context.Context
	matches []JsonSyntaxMatch
}

// NextMatch yields the next match in source order; nil after exhaustion.
func (c *JsonSyntaxQueryCursor) NextMatch() (JsonSyntaxMatch, *protocol.QueryFailure) {
	if len(c.matches) == 0 {
		if c.ctx != nil && c.ctx.Err() != nil {
			return JsonSyntaxMatch{}, &protocol.QueryFailure{Kind: protocol.FailureCancelled}
		}
		return JsonSyntaxMatch{}, nil
	}
	match := c.matches[0]
	c.matches = c.matches[1:]
	return match, nil
}

// queryContext is the execution state of one query (query.rs).
type queryContext struct {
	document *Document
	limits   protocol.QueryLimits
	steps    int
}

// step applies the step and result budgets and the cancellation check
// (query.rs).
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

// executeExpression evaluates one native expression (query.rs).
func executeExpression(ctx context.Context, expression *protocol.QueryExpression,
	input []JsonMatch, context *queryContext) ([]JsonMatch, *protocol.QueryFailure) {
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
		var output []JsonMatch
		for _, branch := range expression.Branches {
			values, failure := executeExpression(ctx, branch, input, context)
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
		var output []JsonMatch
		for _, branch := range expression.Branches {
			values, failure := executeExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			output = append(output, values...)
		}
		sort.SliceStable(output, func(i, j int) bool {
			left := context.document.span(resolveMatchIndex(context.document, output[i].Identity()))
			right := context.document.span(resolveMatchIndex(context.document, output[j].Identity()))
			if left.StartByte() != right.StartByte() {
				return left.StartByte() < right.StartByte()
			}
			if left.EndByte() != right.EndByte() {
				return left.EndByte() < right.EndByte()
			}
			return output[i].Identity().Index() < output[j].Identity().Index()
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
	input []JsonSyntaxMatch, context *queryContext) ([]JsonSyntaxMatch, *protocol.QueryFailure) {
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
		var output []JsonSyntaxMatch
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
		var output []JsonSyntaxMatch
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

// applyOperator applies one native operator (query.rs).
func applyOperator(ctx context.Context, operator *protocol.OperatorCall, input []JsonMatch,
	context *queryContext) ([]JsonMatch, *protocol.QueryFailure) {
	var output []JsonMatch
	switch operator.ID() {
	case "json.try-object-members":
		for _, item := range input {
			if item.Kind != JsonMatchValue {
				continue
			}
			index := resolveMatchIndex(context.document, item.Node)
			kind := &context.document.valueEntity(index).kind
			if kind.tag != internalObject {
				continue
			}
			for _, memberIndex := range kind.object {
				member := JsonObjectMember{document: context.document, index: memberIndex}
				var name *string
				if availability := member.Name(); availability.IsAvailable() {
					name = availability.Value()
				}
				output = append(output, JsonMatch{
					Kind:    JsonMatchObjectMember,
					Node:    member.NodeRef(),
					Ordinal: member.Ordinal(),
					Name:    name,
					Key:     member.KeyNodeRef(),
					Value:   member.ValueNodeRef(),
				})
			}
		}
	case "json.member-name-equals":
		expected := coreStringArgument(operator, "name")
		for _, item := range input {
			if item.Kind == JsonMatchObjectMember && item.Name != nil && *item.Name == expected {
				output = append(output, item)
			}
		}
	case "json.member-value":
		for _, item := range input {
			if item.Kind != JsonMatchObjectMember {
				continue
			}
			output = append(output, context.valueMatch(ctx, resolveMatchIndex(context.document, item.Value)))
		}
	case "json.try-array-elements":
		for _, item := range input {
			if item.Kind != JsonMatchValue {
				continue
			}
			index := resolveMatchIndex(context.document, item.Node)
			kind := &context.document.valueEntity(index).kind
			if kind.tag != internalArray {
				continue
			}
			for _, elementIndex := range kind.array {
				element := JsonArrayElement{document: context.document, index: elementIndex}
				output = append(output, JsonMatch{
					Kind:    JsonMatchArrayElement,
					Node:    element.NodeRef(),
					Ordinal: element.Ordinal(),
					Value:   element.ValueNodeRef(),
				})
			}
		}
	case "json.array-element-value":
		for _, item := range input {
			if item.Kind != JsonMatchArrayElement {
				continue
			}
			output = append(output, context.valueMatch(ctx, resolveMatchIndex(context.document, item.Value)))
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
			if seen[item.Identity()] {
				continue
			}
			seen[item.Identity()] = true
			output = append(output, item)
		}
	}
	if failure := context.step(ctx, len(output)); failure != nil {
		return nil, failure
	}
	return output, nil
}

// applySyntaxOperator applies one syntax operator (query.rs).
func applySyntaxOperator(ctx context.Context, operator *protocol.OperatorCall, input []JsonSyntaxMatch,
	context *queryContext) ([]JsonSyntaxMatch, *protocol.QueryFailure) {
	var output []JsonSyntaxMatch
	switch operator.ID() {
	case "json.syntax-kind-is":
		expected := coreStringArgument(operator, "kind")
		for _, item := range input {
			if item.kind.AsStr() == expected {
				output = append(output, item)
			}
		}
	case "json.syntax-text-equals":
		expected := coreStringArgument(operator, "text")
		text, _ := context.document.source.DecodedText()
		for _, item := range input {
			if text[item.span.StartByte():item.span.EndByte()] == expected {
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
	}
	if failure := context.step(ctx, len(output)); failure != nil {
		return nil, failure
	}
	return output, nil
}

// valueMatch builds the Value match for one value entity index.
func (c *queryContext) valueMatch(ctx context.Context, index int) JsonMatch {
	value := JsonValue{document: c.document, index: index}
	var kind *JsonValueKind
	if availability := value.Kind(); availability.IsAvailable() {
		valueKind := availability.Value()
		kind = &valueKind
	}
	return JsonMatch{Kind: JsonMatchValue, Node: value.NodeRef(), ValueKind: kind}
}

// resolveMatchIndex resolves one bound match identity to its entity index;
// validation guaranteed the identity belongs to this snapshot.
func resolveMatchIndex(document *Document, node document.NodeRef) int {
	return int(node.Index())
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
// (query.rs).
func applySelection(values []JsonMatch, selection protocol.QuerySelection) ([]JsonMatch, *protocol.QueryFailure) {
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
func applySyntaxSelection(values []JsonSyntaxMatch, selection protocol.QuerySelection) ([]JsonSyntaxMatch, *protocol.QueryFailure) {
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
