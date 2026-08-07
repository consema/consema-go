package toml

import (
	"context"
	"sort"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// TomlMatch is one snapshot-bound TOML native semantic query match
// (consema-toml query.rs:9-41).
type TomlMatch struct {
	// Kind is the match variant: Item, Entry, or ArrayElement.
	Kind string
	// Node is the item identity of Item matches.
	Node document.NodeRef
	// KindName is the native item category of Item matches.
	KindName TomlItemKind
	// Ordinal is the zero-based direct ordinal of Entry and ArrayElement
	// matches.
	Ordinal int
	// Name is the decoded direct key segment of Entry matches.
	Name string
	// Key is the key identity of Entry matches.
	Key document.NodeRef
	// Item is the associated item identity of Entry and ArrayElement
	// matches.
	Item document.NodeRef
	// Entry is the association identity of Entry matches.
	Entry document.NodeRef
	// Element is the association identity of ArrayElement matches.
	Element document.NodeRef
	// identity is the process-local identity used for
	// core.distinct-by-identity and structure-order-merge.
	identity document.NodeRef
}

// TomlSyntaxMatch is one snapshot-bound TOML lossless syntax query match
// (consema-toml query.rs:53-86).
type TomlSyntaxMatch struct {
	node    document.NodeRef
	span    document.Span
	kind    TomlSyntaxKind
	ordinal int
}

// NodeRef returns the process-local syntax-piece identity.
func (m TomlSyntaxMatch) NodeRef() document.NodeRef { return m.node }

// Span returns the exact raw source span.
func (m TomlSyntaxMatch) Span() document.Span { return m.span }

// Kind returns the format-specific lossless kind.
func (m TomlSyntaxMatch) Kind() TomlSyntaxKind { return m.kind }

// Ordinal returns the zero-based source-order position.
func (m TomlSyntaxMatch) Ordinal() int { return m.ordinal }

// ExecuteTomlQuery executes a validated TOML native semantic query against
// one immutable snapshot (consema-toml query.rs:88-127). The context is
// used for cancellation and deadlines only. Steps and result counts are
// bounded by limits; exceeding either is core.query.resource-limit@1.
func ExecuteTomlQuery(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) ([]TomlMatch, *protocol.QueryFailure) {
	domain := executable.Definition().Domain()
	if domain.ID() != "toml.native-semantic-query" || domain.Version() != 1 {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	context := &queryContext{ctx: ctx, document: doc, limits: limits}
	if failure := context.step(0); failure != nil {
		return nil, failure
	}
	input := []TomlMatch{{
		Kind: "Item", Node: doc.nodeRef(doc.root, document.RoleTomlItem),
		KindName: ItemKindRootTable, identity: doc.nodeRef(doc.root, document.RoleTomlItem),
	}}
	matches, failure := executeQueryExpression(ctx, executable.Definition().Expression(), input, context)
	if failure != nil {
		return nil, failure
	}
	matches, failure = applyTomlSelection(matches, executable.Definition().Selection())
	if failure != nil {
		return nil, failure
	}
	return matches, nil
}

// ExecuteTomlSyntaxQuery executes a validated TOML lossless syntax query
// against every source piece in raw order (consema-toml query.rs:129-169).
func ExecuteTomlSyntaxQuery(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) ([]TomlSyntaxMatch, *protocol.QueryFailure) {
	domain := executable.Definition().Domain()
	if domain.ID() != "toml.lossless-syntax-query" || domain.Version() != 1 {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	context := &queryContext{ctx: ctx, document: doc, limits: limits}
	pieces := doc.index.Pieces()
	if failure := context.step(len(pieces)); failure != nil {
		return nil, failure
	}
	kinds := doc.kinds
	input := make([]TomlSyntaxMatch, 0, len(pieces))
	for ordinal, piece := range pieces {
		input = append(input, TomlSyntaxMatch{
			node:    doc.nodeRef(ordinal, document.RoleTomlSyntaxPiece),
			span:    piece.span,
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

type queryContext struct {
	ctx      context.Context
	document *Document
	limits   protocol.QueryLimits
	steps    int
}

func (c *queryContext) step(results int) *protocol.QueryFailure {
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

// executeQueryExpression evaluates one native expression against the input
// matches (consema-toml query.rs:213-254).
func executeQueryExpression(ctx context.Context, expression *protocol.QueryExpression,
	input []TomlMatch, context *queryContext) ([]TomlMatch, *protocol.QueryFailure) {
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
		var output []TomlMatch
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
		var output []TomlMatch
		for _, branch := range expression.Branches {
			matches, failure := executeQueryExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			output = append(output, matches...)
		}
		sort.SliceStable(output, func(i, j int) bool {
			left := context.document.entitySpanOf(output[i].identity)
			right := context.document.entitySpanOf(output[j].identity)
			if left.StartByte() != right.StartByte() {
				return left.StartByte() < right.StartByte()
			}
			if left.EndByte() != right.EndByte() {
				return left.EndByte() < right.EndByte()
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

func (d *Document) entitySpanOf(node document.NodeRef) document.Span {
	index := int(node.Index())
	if index >= 0 && index < len(d.entities) {
		return d.entities[index].span
	}
	return d.authoritySpan(node)
}

func (d *Document) authoritySpan(node document.NodeRef) document.Span {
	// Syntax-piece matches carry ordinal identities beyond the entity
	// list; their spans are the corresponding pieces.
	if node.Role() == document.RoleTomlSyntaxPiece {
		ordinal := int(node.Index())
		pieces := d.index.Pieces()
		if ordinal >= 0 && ordinal < len(pieces) {
			return pieces[ordinal].span
		}
	}
	span, err := d.authority.Span(0, 0)
	if err != nil {
		panic("toml: authority span")
	}
	return span
}

// applyQueryOperator evaluates one native operator (consema-toml
// query.rs:341-469).
func applyQueryOperator(ctx context.Context, operator *protocol.OperatorCall,
	input []TomlMatch, context *queryContext) ([]TomlMatch, *protocol.QueryFailure) {
	if failure := checkCancelled(ctx); failure != nil {
		return nil, failure
	}
	var output []TomlMatch
	switch operator.ID() {
	case "toml.try-table-entries":
		for _, match := range input {
			if match.Kind != "Item" {
				continue
			}
			index := int(match.Node.Index())
			item := &context.document.entities[index].item
			var entries []int
			switch item.kind {
			case itemTable:
				entries = item.entries
			case itemInlineTable:
				entries = item.entries
			default:
				continue
			}
			for _, entryIndex := range entries {
				entry := context.document.entities[entryIndex].entry
				keyEntity := &context.document.entities[entry.key].key
				output = append(output, TomlMatch{
					Kind: "Entry", Ordinal: entry.ordinal, Name: keyEntity.name,
					Key:      context.document.nodeRef(entry.key, document.RoleTomlKey),
					Item:     context.document.nodeRef(entry.item, document.RoleTomlItem),
					Entry:    context.document.nodeRef(entryIndex, document.RoleTomlEntry),
					identity: context.document.nodeRef(entryIndex, document.RoleTomlEntry),
				})
			}
		}
	case "toml.entry-name-equals":
		expected, ok := stringArgument(operator, "name")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "name"}
		}
		for _, match := range input {
			if match.Kind == "Entry" && match.Name == expected {
				output = append(output, match)
			}
		}
	case "toml.entry-item":
		for _, match := range input {
			if match.Kind != "Entry" {
				continue
			}
			index := int(match.Item.Index())
			item := &context.document.entities[index].item
			output = append(output, TomlMatch{
				Kind: "Item", Node: match.Item, KindName: item.publicKind(),
				identity: match.Item,
			})
		}
	case "toml.try-array-elements":
		for _, match := range input {
			if match.Kind != "Item" {
				continue
			}
			index := int(match.Node.Index())
			item := &context.document.entities[index].item
			var elements []int
			switch item.kind {
			case itemArray:
				elements = item.elements
			case itemArrayOfTables:
				elements = item.elements
			default:
				continue
			}
			for _, elementIndex := range elements {
				element := context.document.entities[elementIndex].element
				output = append(output, TomlMatch{
					Kind: "ArrayElement", Ordinal: element.ordinal,
					Element:  context.document.nodeRef(elementIndex, document.RoleTomlArrayElement),
					Item:     context.document.nodeRef(element.item, document.RoleTomlItem),
					identity: context.document.nodeRef(elementIndex, document.RoleTomlArrayElement),
				})
			}
		}
	case "toml.array-element-item":
		for _, match := range input {
			if match.Kind != "ArrayElement" {
				continue
			}
			index := int(match.Item.Index())
			item := &context.document.entities[index].item
			output = append(output, TomlMatch{
				Kind: "Item", Node: match.Item, KindName: item.publicKind(),
				identity: match.Item,
			})
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

// executeSyntaxExpression evaluates one lossless-syntax expression
// (consema-toml query.rs:256-288).
func executeSyntaxExpression(ctx context.Context, expression *protocol.QueryExpression,
	input []TomlSyntaxMatch, context *queryContext) ([]TomlSyntaxMatch, *protocol.QueryFailure) {
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
		var output []TomlSyntaxMatch
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
		var output []TomlSyntaxMatch
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
// (consema-toml query.rs:290-339).
func applySyntaxOperator(ctx context.Context, operator *protocol.OperatorCall,
	input []TomlSyntaxMatch, context *queryContext) ([]TomlSyntaxMatch, *protocol.QueryFailure) {
	if failure := checkCancelled(ctx); failure != nil {
		return nil, failure
	}
	var output []TomlSyntaxMatch
	switch operator.ID() {
	case "toml.syntax-kind-is":
		expected, ok := stringArgument(operator, "kind")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "kind"}
		}
		kind, valid := TomlSyntaxKindFromName(expected)
		if !valid {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "kind"}
		}
		for _, match := range input {
			if match.kind == kind {
				output = append(output, match)
			}
		}
	case "toml.syntax-text-equals":
		expected, ok := stringArgument(operator, "text")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: operator.ID(), Argument: "text"}
		}
		source := context.document.sourceBytes()
		for _, match := range input {
			text := source[match.span.StartByte():match.span.EndByte()]
			if string(text) == expected {
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

func (d *Document) sourceBytes() []byte { return d.source.Bytes() }

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

func checkCancelled(ctx context.Context) *protocol.QueryFailure {
	select {
	case <-ctx.Done():
		return &protocol.QueryFailure{Kind: protocol.FailureCancelled}
	default:
		return nil
	}
}

// applyTomlSelection applies the definition selection (consema-toml
// query.rs:471-488).
func applyTomlSelection(matches []TomlMatch, selection protocol.QuerySelection) ([]TomlMatch, *protocol.QueryFailure) {
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

func applySyntaxSelection(matches []TomlSyntaxMatch, selection protocol.QuerySelection) ([]TomlSyntaxMatch, *protocol.QueryFailure) {
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
