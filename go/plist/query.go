package plist

// This file implements the plist three-domain query execution
// (consema-plist query.rs; RFC 0013 §8). The three domains share the
// immutable-snapshot, ordered-selection, limits, and cancellation rules of
// the common query contract and differ only in what they see:
//
//   - `plist.native-semantic-query@1` queries the representation-independent
//     native value arena of both profiles. Results preserve source order;
//     the typed accessors validate the value type before returning, and a
//     type mismatch is a query failure, never a null or converted result.
//   - `plist.lossless-syntax-query@1` filters the ordered lossless pieces of
//     a `plist.xml@1` source (hard gate 1: binary documents have no syntax
//     pieces).
//   - `plist.binary-structure-query@1` exposes the binary structure facts
//     directly (hard gate 1: XML documents have none). The structure facts
//     are document-level: every operator projects its fact set once from
//     any binary-structure input match.

import (
	"context"
	"sort"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// PlistMatchKind is the closed plist native match category (RFC 0013 §8.1).
type PlistMatchKind uint8

// The five frozen native match categories.
const (
	// PlistMatchDocument is the complete plist document; the native-domain
	// root input.
	PlistMatchDocument PlistMatchKind = iota
	// PlistMatchValue is one native value node of any of the nine closed
	// kinds.
	PlistMatchValue
	// PlistMatchDictEntry is one dictionary key/value association.
	PlistMatchDictEntry
	// PlistMatchKey is one string key identity of one dictionary
	// association.
	PlistMatchKey
	// PlistMatchArrayElement is one array element association.
	PlistMatchArrayElement
)

// PlistMatch is one snapshot-bound plist native semantic query match (RFC
// 0013 §8.1). Value matches reference the arena of the queried document, so
// shared identity from the binary object table survives querying: one
// native node referenced by several containers is one match identity.
type PlistMatch struct {
	// Kind is the closed match category.
	Kind PlistMatchKind
	// Node is the exact match identity.
	Node document.NodeRef
	// Value is the arena reference of Value matches.
	Value *PlistValueRef
	// ValueKind is the closed native kind of Value matches.
	ValueKind *PlistValueKind
	// Dict is the owning dictionary arena reference of DictEntry and Key
	// matches.
	Dict *PlistValueRef
	// Position is the association or element position in source order.
	Position *int
	// Key is the exact string key identity of DictEntry and Key matches.
	Key *PlistKey
	// Array is the owning array arena reference of ArrayElement matches.
	Array *PlistValueRef
}

// Identity returns the exact match identity.
func (m PlistMatch) Identity() document.NodeRef { return m.Node }

// PlistSyntaxMatch is one snapshot-bound plist XML lossless syntax query
// match (RFC 0013 §8.2).
type PlistSyntaxMatch struct {
	node    document.NodeRef
	span    document.Span
	kind    PlistSyntaxKind
	ordinal int
}

// NodeRef returns the process-local syntax-piece identity.
func (m PlistSyntaxMatch) NodeRef() document.NodeRef { return m.node }

// Span returns the exact raw source span.
func (m PlistSyntaxMatch) Span() document.Span { return m.span }

// Kind returns the format-specific lossless kind.
func (m PlistSyntaxMatch) Kind() PlistSyntaxKind { return m.kind }

// Ordinal returns the zero-based source-order position.
func (m PlistSyntaxMatch) Ordinal() int { return m.ordinal }

// PlistBinaryMatchKind is the closed plist binary structure match category
// (RFC 0013 §8.3).
type PlistBinaryMatchKind uint8

// The six frozen binary structure match categories.
const (
	// PlistBinaryMatchStructure is the complete binary structure; the
	// binary-structure-domain root input.
	PlistBinaryMatchStructure PlistBinaryMatchKind = iota
	// PlistBinaryMatchObject is one proven object-table entry fact.
	PlistBinaryMatchObject
	// PlistBinaryMatchOffset is one validated offset-table entry fact.
	PlistBinaryMatchOffset
	// PlistBinaryMatchRef is one decoded object reference fact.
	PlistBinaryMatchRef
	// PlistBinaryMatchTrailer is the trailer field facts.
	PlistBinaryMatchTrailer
	// PlistBinaryMatchTopObject is the trailer's top object with its
	// ordered reference facts.
	PlistBinaryMatchTopObject
)

// PlistBinaryMatch is one snapshot-bound plist binary structure query match
// (RFC 0013 §8.3).
type PlistBinaryMatch struct {
	// Kind is the closed match category.
	Kind PlistBinaryMatchKind
	// Node is the exact match identity.
	Node document.NodeRef
	// Index is the object-table or fact ordinal.
	Index int
	// Offset is the marker byte offset or decoded offset-table value.
	Offset int
	// Marker is the marker byte.
	Marker byte
	// Span is the exact byte range.
	Span document.Span
	// Owner is the referencing object index of Ref matches.
	Owner int
	// Position is the reference position within the owner's block.
	Position int
	// Target is the decoded target object index of Ref matches.
	Target int
	// SortVersion is the trailer `sortVersion` byte.
	SortVersion byte
	// OffsetIntSize is the trailer `offsetIntSize` byte.
	OffsetIntSize byte
	// ObjectRefSize is the trailer `objectRefSize` byte.
	ObjectRefSize byte
	// NumObjects is the trailer `numObjects` value.
	NumObjects uint64
	// TopObject is the trailer `topObject` value.
	TopObject uint64
	// OffsetTableOffset is the trailer `offsetTableOffset` value.
	OffsetTableOffset uint64
	// Refs are the ordered `(position, target, span)` triples of
	// TopObject matches.
	Refs []BinaryObjectRefFact
}

// Identity returns the exact match identity.
func (m PlistBinaryMatch) Identity() document.NodeRef { return m.Node }

// ExecutePlistNativeQuery executes a validated plist native semantic query
// against one immutable snapshot (RFC 0013 §8.1). ctx carries cancellation
// only.
func ExecutePlistNativeQuery(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) ([]PlistMatch, *protocol.QueryFailure) {
	domain := executable.Definition().Domain()
	if domain.ID() != "plist.native-semantic-query" || domain.Version() != 1 {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	context := &plistQueryContext{document: doc, limits: limits}
	native := doc.native
	ranks := preorderRanks(native)
	context.ranks = ranks
	if failure := context.step(ctx, 1); failure != nil {
		return nil, failure
	}
	node := doc.nodeRef(0, document.RolePlistDocument)
	input := []PlistMatch{{Kind: PlistMatchDocument, Node: node}}
	matches, failure := executeNativeExpression(ctx, executable.Definition().Expression(),
		input, context)
	if failure != nil {
		return nil, failure
	}
	return applySelection(matches, executable.Definition().Selection())
}

// ExecutePlistSyntaxQuery executes a validated plist lossless syntax query
// against every source piece in raw order (RFC 0013 §8.2). A binary
// document is rejected with DomainMismatch before the first result (hard
// gate 1).
func ExecutePlistSyntaxQuery(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) ([]PlistSyntaxMatch, *protocol.QueryFailure) {
	domain := executable.Definition().Domain()
	if domain.ID() != "plist.lossless-syntax-query" || domain.Version() != 1 {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	if doc.xmlIndex == nil || doc.xmlKinds == nil {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	context := &plistSyntaxContext{document: doc, limits: limits}
	pieces := doc.xmlIndex.Pieces()
	if failure := context.step(ctx, len(pieces)); failure != nil {
		return nil, failure
	}
	input := make([]PlistSyntaxMatch, 0, len(pieces))
	for ordinal, piece := range pieces {
		input = append(input, PlistSyntaxMatch{
			node:    doc.nodeRef(ordinal, document.RolePlistSyntaxPiece),
			span:    piece.Span(),
			kind:    doc.xmlKinds[ordinal],
			ordinal: ordinal,
		})
	}
	matches, failure := executeSyntaxExpression(ctx, executable.Definition().Expression(),
		input, context)
	if failure != nil {
		return nil, failure
	}
	return applySyntaxSelection(matches, executable.Definition().Selection())
}

// ExecutePlistBinaryQuery executes a validated plist binary structure query
// (RFC 0013 §8.3). An XML document is rejected with DomainMismatch before
// the first result (hard gate 1). The structure facts are document-level:
// every operator projects its fact set once from any binary-structure input
// match.
func ExecutePlistBinaryQuery(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) ([]PlistBinaryMatch, *protocol.QueryFailure) {
	domain := executable.Definition().Domain()
	if domain.ID() != "plist.binary-structure-query" || domain.Version() != 1 {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	if doc.binaryFacts == nil {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	context := &plistBinaryContext{document: doc, facts: doc.binaryFacts, limits: limits}
	if failure := context.step(ctx, 1); failure != nil {
		return nil, failure
	}
	node := doc.nodeRef(0, document.RolePlistDocument)
	input := []PlistBinaryMatch{{Kind: PlistBinaryMatchStructure, Node: node}}
	matches, failure := executeBinaryExpression(ctx, executable.Definition().Expression(),
		input, context)
	if failure != nil {
		return nil, failure
	}
	return applyBinarySelection(matches, executable.Definition().Selection())
}

// plistQueryContext is the native-domain execution state.
type plistQueryContext struct {
	document *Document
	limits   protocol.QueryLimits
	ranks    []int
	steps    int
}

func (c *plistQueryContext) step(ctx context.Context, results int) *protocol.QueryFailure {
	if ctx != nil && ctx.Err() != nil {
		return &protocol.QueryFailure{Kind: protocol.FailureCancelled}
	}
	c.steps++
	if c.steps > c.limits.MaxSteps || results > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	return nil
}

func (c *plistQueryContext) push(output *[]PlistMatch, value PlistMatch) *protocol.QueryFailure {
	if len(*output)+1 > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	*output = append(*output, value)
	return nil
}

func (c *plistQueryContext) append(output *[]PlistMatch, values []PlistMatch) *protocol.QueryFailure {
	if len(*output)+len(values) > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	*output = append(*output, values...)
	return nil
}

// sourceOrder is the deterministic structure-order key: the pre-order rank
// of the owning node, with association position as the tiebreak
// (query.rs).
func (c *plistQueryContext) sourceOrder(item *PlistMatch) (int, int) {
	switch item.Kind {
	case PlistMatchDocument:
		return 0, 0
	case PlistMatchValue:
		return c.rank(item.Value.Index()), 0
	case PlistMatchDictEntry, PlistMatchKey:
		return c.rank(item.Dict.Index()), *item.Position + 1
	case PlistMatchArrayElement:
		return c.rank(item.Array.Index()), *item.Position + 1
	}
	return 0, 0
}

func (c *plistQueryContext) rank(index int) int {
	if index >= 0 && index < len(c.ranks) {
		return c.ranks[index]
	}
	return int(^uint(0) >> 1)
}

// plistSyntaxContext is the syntax-domain execution state.
type plistSyntaxContext struct {
	document *Document
	limits   protocol.QueryLimits
	steps    int
}

func (c *plistSyntaxContext) step(ctx context.Context, results int) *protocol.QueryFailure {
	if ctx != nil && ctx.Err() != nil {
		return &protocol.QueryFailure{Kind: protocol.FailureCancelled}
	}
	c.steps++
	if c.steps > c.limits.MaxSteps || results > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	return nil
}

func (c *plistSyntaxContext) push(output *[]PlistSyntaxMatch, value PlistSyntaxMatch) *protocol.QueryFailure {
	if len(*output)+1 > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	*output = append(*output, value)
	return nil
}

func (c *plistSyntaxContext) append(output *[]PlistSyntaxMatch, values []PlistSyntaxMatch) *protocol.QueryFailure {
	if len(*output)+len(values) > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	*output = append(*output, values...)
	return nil
}

// plistBinaryContext is the binary-structure-domain execution state.
type plistBinaryContext struct {
	document *Document
	facts    *BinaryFacts
	limits   protocol.QueryLimits
	steps    int
}

func (c *plistBinaryContext) step(ctx context.Context, results int) *protocol.QueryFailure {
	if ctx != nil && ctx.Err() != nil {
		return &protocol.QueryFailure{Kind: protocol.FailureCancelled}
	}
	c.steps++
	if c.steps > c.limits.MaxSteps || results > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	return nil
}

func (c *plistBinaryContext) push(output *[]PlistBinaryMatch, value PlistBinaryMatch) *protocol.QueryFailure {
	if len(*output)+1 > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	*output = append(*output, value)
	return nil
}

func (c *plistBinaryContext) append(output *[]PlistBinaryMatch, values []PlistBinaryMatch) *protocol.QueryFailure {
	if len(*output)+len(values) > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	*output = append(*output, values...)
	return nil
}

// factNode issues the flat identity ordinal shared by the fact arrays, so
// every fact kind has a distinct identity space (query.rs).
func (c *plistBinaryContext) factNode(index int) document.NodeRef {
	return c.document.nodeRef(index, document.RoleBinaryRegion)
}

// sourceOrder is the deterministic structure-order key over the flat fact
// identity space (query.rs).
func (c *plistBinaryContext) sourceOrder(item *PlistBinaryMatch) int {
	objects := len(c.facts.objects)
	offsets := len(c.facts.offsets)
	refs := len(c.facts.refs)
	switch item.Kind {
	case PlistBinaryMatchStructure:
		return 0
	case PlistBinaryMatchObject:
		return 1 + item.Index
	case PlistBinaryMatchOffset:
		return 1 + objects + item.Index
	case PlistBinaryMatchRef:
		return 1 + objects + offsets + item.Index
	case PlistBinaryMatchTrailer:
		return 1 + objects + offsets + refs
	case PlistBinaryMatchTopObject:
		return 1 + objects + offsets + refs + 1
	}
	return 0
}

func executeNativeExpression(ctx context.Context, expression *protocol.QueryExpression,
	input []PlistMatch, context *plistQueryContext) ([]PlistMatch, *protocol.QueryFailure) {
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
		var output []PlistMatch
		for _, branch := range expression.Branches {
			values, failure := executeNativeExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			if failure := context.append(&output, values); failure != nil {
				return nil, failure
			}
			if failure := context.step(ctx, len(output)); failure != nil {
				return nil, failure
			}
		}
		return output, nil
	case protocol.ExpressionStructureOrderMerge:
		var output []PlistMatch
		for _, branch := range expression.Branches {
			values, failure := executeNativeExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			if failure := context.append(&output, values); failure != nil {
				return nil, failure
			}
		}
		sort.SliceStable(output, func(i, j int) bool {
			leftRank, leftPosition := context.sourceOrder(&output[i])
			rightRank, rightPosition := context.sourceOrder(&output[j])
			if leftRank != rightRank {
				return leftRank < rightRank
			}
			return leftPosition < rightPosition
		})
		if failure := context.step(ctx, len(output)); failure != nil {
			return nil, failure
		}
		return output, nil
	}
	return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
}

func executeSyntaxExpression(ctx context.Context, expression *protocol.QueryExpression,
	input []PlistSyntaxMatch, context *plistSyntaxContext) ([]PlistSyntaxMatch, *protocol.QueryFailure) {
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
		var output []PlistSyntaxMatch
		for _, branch := range expression.Branches {
			values, failure := executeSyntaxExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			if failure := context.append(&output, values); failure != nil {
				return nil, failure
			}
			if failure := context.step(ctx, len(output)); failure != nil {
				return nil, failure
			}
		}
		return output, nil
	case protocol.ExpressionStructureOrderMerge:
		var output []PlistSyntaxMatch
		for _, branch := range expression.Branches {
			values, failure := executeSyntaxExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			if failure := context.append(&output, values); failure != nil {
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

func executeBinaryExpression(ctx context.Context, expression *protocol.QueryExpression,
	input []PlistBinaryMatch, context *plistBinaryContext) ([]PlistBinaryMatch, *protocol.QueryFailure) {
	switch expression.Kind {
	case protocol.ExpressionInput:
		return input, nil
	case protocol.ExpressionApply:
		values, failure := executeBinaryExpression(ctx, expression.Input, input, context)
		if failure != nil {
			return nil, failure
		}
		return applyBinaryOperator(ctx, expression.Operator, values, context)
	case protocol.ExpressionConcat:
		var output []PlistBinaryMatch
		for _, branch := range expression.Branches {
			values, failure := executeBinaryExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			if failure := context.append(&output, values); failure != nil {
				return nil, failure
			}
			if failure := context.step(ctx, len(output)); failure != nil {
				return nil, failure
			}
		}
		return output, nil
	case protocol.ExpressionStructureOrderMerge:
		var output []PlistBinaryMatch
		for _, branch := range expression.Branches {
			values, failure := executeBinaryExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			if failure := context.append(&output, values); failure != nil {
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

// applyNativeOperator applies one native operator (query.rs).
func applyNativeOperator(ctx context.Context, operator *protocol.OperatorCall,
	input []PlistMatch, context *plistQueryContext) ([]PlistMatch, *protocol.QueryFailure) {
	var output []PlistMatch
	switch operator.ID() {
	case "plist.document-root":
		if context.document.native == nil {
			break
		}
		for _, item := range input {
			if item.Kind != PlistMatchDocument {
				continue
			}
			root := context.document.native.Root()
			value, _ := context.document.native.Get(root)
			kind := value.Kind()
			reference := root
			node := context.document.nodeRef(root.index, document.RolePlistValue)
			if failure := context.push(&output, PlistMatch{
				Kind: PlistMatchValue, Node: node, Value: &reference, ValueKind: &kind,
			}); failure != nil {
				return nil, failure
			}
		}
	case "plist.dict-entries":
		if context.document.native == nil {
			break
		}
		for _, item := range input {
			if item.Kind != PlistMatchValue || item.Value == nil {
				continue
			}
			value, ok := context.document.native.Get(*item.Value)
			if !ok {
				continue
			}
			dict, isDict := value.AsDict()
			if !isDict {
				continue
			}
			for position, entry := range dict.Entries() {
				kind := valueKindOf(context.document, entry.Value())
				position := position
				key := entry.Key()
				reference := *item.Value
				node := context.document.nodeRef(position, document.RolePlistDictEntry)
				if failure := context.push(&output, PlistMatch{
					Kind: PlistMatchDictEntry, Node: node, Dict: &reference,
					Position: &position, Key: &key, Value: refPtr(entry.Value()),
					ValueKind: &kind,
				}); failure != nil {
					return nil, failure
				}
			}
		}
	case "plist.dict-entry-key":
		for _, item := range input {
			if item.Kind != PlistMatchDictEntry || item.Key == nil || item.Dict == nil ||
				item.Position == nil {
				continue
			}
			key := *item.Key
			position := *item.Position
			dict := *item.Dict
			node := context.document.nodeRef(position, document.RolePlistKey)
			if failure := context.push(&output, PlistMatch{
				Kind: PlistMatchKey, Node: node, Dict: &dict, Position: &position, Key: &key,
			}); failure != nil {
				return nil, failure
			}
		}
	case "plist.dict-entry-value":
		if context.document.native == nil {
			break
		}
		for _, item := range input {
			if item.Kind != PlistMatchDictEntry || item.Value == nil {
				continue
			}
			value := *item.Value
			kind := valueKindOf(context.document, value)
			node := context.document.nodeRef(value.index, document.RolePlistValue)
			if failure := context.push(&output, PlistMatch{
				Kind: PlistMatchValue, Node: node, Value: &value, ValueKind: &kind,
			}); failure != nil {
				return nil, failure
			}
		}
	case "plist.dict-key-equals":
		expected := coreStringArgument(operator, "key")
		expectedUnits := utf16Encode(expected)
		for _, item := range input {
			if item.Kind != PlistMatchDictEntry || item.Key == nil {
				continue
			}
			if codeUnitsEqual(item.Key.CodeUnits(), expectedUnits) {
				if failure := context.push(&output, item); failure != nil {
					return nil, failure
				}
			}
		}
	case "plist.duplicate-key-group":
		if context.document.native == nil {
			break
		}
		for _, item := range input {
			if item.Kind != PlistMatchDictEntry || item.Key == nil || item.Dict == nil {
				continue
			}
			value, ok := context.document.native.Get(*item.Dict)
			if !ok {
				continue
			}
			dict, isDict := value.AsDict()
			if !isDict {
				continue
			}
			key := *item.Key
			for position, entry := range dict.Entries() {
				if !entry.Key().Equal(key) {
					continue
				}
				kind := valueKindOf(context.document, entry.Value())
				position := position
				dictRef := *item.Dict
				node := context.document.nodeRef(position, document.RolePlistDictEntry)
				if failure := context.push(&output, PlistMatch{
					Kind: PlistMatchDictEntry, Node: node, Dict: &dictRef,
					Position: &position, Key: &key, Value: refPtr(entry.Value()),
					ValueKind: &kind,
				}); failure != nil {
					return nil, failure
				}
			}
		}
	case "plist.array-elements":
		if context.document.native == nil {
			break
		}
		for _, item := range input {
			if item.Kind != PlistMatchValue || item.Value == nil {
				continue
			}
			value, ok := context.document.native.Get(*item.Value)
			if !ok {
				continue
			}
			array, isArray := value.AsArray()
			if !isArray {
				continue
			}
			for position, element := range array.Elements() {
				kind := valueKindOf(context.document, element)
				position := position
				arrayRef := *item.Value
				node := context.document.nodeRef(position, document.RolePlistArrayElement)
				if failure := context.push(&output, PlistMatch{
					Kind: PlistMatchArrayElement, Node: node, Array: &arrayRef,
					Position: &position, Value: refPtr(element), ValueKind: &kind,
				}); failure != nil {
					return nil, failure
				}
			}
		}
	case "plist.value-type-is":
		expectedName := coreStringArgument(operator, "kind")
		expected, ok := plistKindFromName(expectedName)
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
		}
		for _, item := range input {
			if payload, payloadKind, has := valuePayload(&item); has {
				if payloadKind == expected {
					_ = payload
					if failure := context.push(&output, item); failure != nil {
						return nil, failure
					}
				}
			}
		}
	case "plist.value-as-integer":
		return typedAccessor(PlistValueKindInteger, input, context)
	case "plist.value-as-real":
		return typedAccessor(PlistValueKindReal, input, context)
	case "plist.value-as-string":
		return typedAccessor(PlistValueKindString, input, context)
	case "plist.value-as-data":
		return typedAccessor(PlistValueKindData, input, context)
	case "plist.value-as-date":
		return typedAccessor(PlistValueKindDate, input, context)
	case "plist.value-as-uid":
		return typedAccessor(PlistValueKindUid, input, context)
	case "plist.value-as-boolean-is":
		expected := coreBooleanArgument(operator, "value")
		if context.document.native == nil {
			break
		}
		for _, item := range input {
			payload, kind, has := valuePayload(&item)
			if !has {
				continue
			}
			if kind != PlistValueKindBoolean {
				return nil, &protocol.QueryFailure{Kind: protocol.FailureRequiredTypeMismatch}
			}
			value, _ := context.document.native.Get(payload)
			boolean, _ := value.AsBoolean()
			if boolean.Value() == expected {
				if failure := context.push(&output, item); failure != nil {
					return nil, failure
				}
			}
		}
	case "core.take":
		count := takeCount(operator)
		if count > len(input) {
			count = len(input)
		}
		for _, item := range input[:count] {
			if failure := context.push(&output, item); failure != nil {
				return nil, failure
			}
		}
	case "core.distinct-by-identity":
		seen := make(map[document.NodeRef]bool)
		for _, item := range input {
			if seen[item.Identity()] {
				continue
			}
			seen[item.Identity()] = true
			if failure := context.push(&output, item); failure != nil {
				return nil, failure
			}
		}
	default:
		return nil, &protocol.QueryFailure{Kind: protocol.FailureUnknownOperator}
	}
	if failure := context.step(ctx, len(output)); failure != nil {
		return nil, failure
	}
	return output, nil
}

// typedAccessor implements the typed accessors: the value type is validated
// before the match is returned; a mismatch is a query failure, never a null
// or converted result (RFC 0013 §8.1).
func typedAccessor(target PlistValueKind, input []PlistMatch,
	context *plistQueryContext) ([]PlistMatch, *protocol.QueryFailure) {
	var output []PlistMatch
	for _, item := range input {
		_, kind, has := valuePayload(&item)
		if !has {
			continue
		}
		if kind != target {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureRequiredTypeMismatch}
		}
		if failure := context.push(&output, item); failure != nil {
			return nil, failure
		}
	}
	return output, nil
}

// valuePayload is the value payload of one value-bearing match: a plain
// value or an array element association (query.rs).
func valuePayload(item *PlistMatch) (PlistValueRef, PlistValueKind, bool) {
	switch item.Kind {
	case PlistMatchValue:
		if item.Value != nil && item.ValueKind != nil {
			return *item.Value, *item.ValueKind, true
		}
	case PlistMatchArrayElement:
		if item.Value != nil && item.ValueKind != nil {
			return *item.Value, *item.ValueKind, true
		}
	}
	return PlistValueRef{}, 0, false
}

// valueKindOf resolves the closed native kind of one arena reference.
func valueKindOf(document *Document, reference PlistValueRef) PlistValueKind {
	value, ok := document.native.Get(reference)
	if !ok {
		return PlistValueKindDict
	}
	return value.Kind()
}

func refPtr(reference PlistValueRef) *PlistValueRef { return &reference }

func codeUnitsEqual(left, right []uint16) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// applySyntaxOperator applies one syntax operator (query.rs).
func applySyntaxOperator(ctx context.Context, operator *protocol.OperatorCall,
	input []PlistSyntaxMatch, context *plistSyntaxContext) ([]PlistSyntaxMatch, *protocol.QueryFailure) {
	var output []PlistSyntaxMatch
	switch operator.ID() {
	case "plist.syntax-kind-is":
		expected := coreStringArgument(operator, "kind")
		for _, item := range input {
			if item.kind.AsStr() == expected {
				if failure := context.push(&output, item); failure != nil {
					return nil, failure
				}
			}
		}
	case "plist.syntax-text-equals":
		expected := coreStringArgument(operator, "text")
		for _, item := range input {
			if decodedSpanText(context.document, item.span) == expected {
				if failure := context.push(&output, item); failure != nil {
					return nil, failure
				}
			}
		}
	case "core.take":
		count := takeCount(operator)
		if count > len(input) {
			count = len(input)
		}
		for _, item := range input[:count] {
			if failure := context.push(&output, item); failure != nil {
				return nil, failure
			}
		}
	case "core.distinct-by-identity":
		seen := make(map[document.NodeRef]bool)
		for _, item := range input {
			if seen[item.node] {
				continue
			}
			seen[item.node] = true
			if failure := context.push(&output, item); failure != nil {
				return nil, failure
			}
		}
	default:
		return nil, &protocol.QueryFailure{Kind: protocol.FailureUnknownOperator}
	}
	if failure := context.step(ctx, len(output)); failure != nil {
		return nil, failure
	}
	return output, nil
}

// decodedSpanText is the exact decoded text of one raw span, resolved
// through the source's decoded text so UTF-8 and UTF-16 sources decode
// correctly (RFC 0013 §2.1; query.rs).
func decodedSpanText(document *Document, span document.Span) string {
	decoded, ok := document.source.DecodedText()
	if !ok {
		return ""
	}
	start, err := document.source.DecodedPosition(span.StartByte())
	if err != nil {
		return string(document.source.Bytes()[span.StartByte():span.EndByte()])
	}
	end, err := document.source.DecodedPosition(span.EndByte())
	if err != nil {
		return string(document.source.Bytes()[span.StartByte():span.EndByte()])
	}
	return decoded[start.DecodedUTF8Byte:end.DecodedUTF8Byte]
}

// applyBinaryOperator applies one binary structure operator (query.rs
// 1511). The facts are document-level: every operator projects its fact set
// once, regardless of how many binary-structure matches arrive.
func applyBinaryOperator(ctx context.Context, operator *protocol.OperatorCall,
	input []PlistBinaryMatch, context *plistBinaryContext) ([]PlistBinaryMatch, *protocol.QueryFailure) {
	if len(input) == 0 {
		return nil, nil
	}
	var output []PlistBinaryMatch
	facts := context.facts
	switch operator.ID() {
	case "plist.object-table", "plist.top-object":
		if operator.ID() == "plist.top-object" {
			top := int(facts.trailer.topObject)
			var fact *BinaryObjectFact
			for index := range facts.objects {
				if facts.objects[index].index == top {
					fact = &facts.objects[index]
					break
				}
			}
			if fact != nil {
				var refs []BinaryObjectRefFact
				for _, reference := range facts.refs {
					if reference.owner == top {
						refs = append(refs, reference)
					}
				}
				if failure := context.push(&output, PlistBinaryMatch{
					Kind:  PlistBinaryMatchTopObject,
					Node:  context.factNode(len(facts.objects) + len(facts.offsets) + len(facts.refs) + 1),
					Index: fact.index, Offset: fact.offset, Marker: fact.marker,
					Span: fact.span, Refs: refs,
				}); failure != nil {
					return nil, failure
				}
			}
		} else {
			for index := range facts.objects {
				fact := facts.objects[index]
				if failure := context.push(&output, PlistBinaryMatch{
					Kind:  PlistBinaryMatchObject,
					Node:  context.factNode(index),
					Index: fact.index, Offset: fact.offset, Marker: fact.marker, Span: fact.span,
				}); failure != nil {
					return nil, failure
				}
			}
		}
	case "plist.object-offset", "plist.offset-table":
		for index := range facts.offsets {
			fact := facts.offsets[index]
			if failure := context.push(&output, PlistBinaryMatch{
				Kind:  PlistBinaryMatchOffset,
				Node:  context.factNode(len(facts.objects) + index),
				Index: fact.index, Offset: fact.offset, Span: fact.span,
			}); failure != nil {
				return nil, failure
			}
		}
	case "plist.object-refs":
		for index := range facts.refs {
			fact := facts.refs[index]
			if failure := context.push(&output, PlistBinaryMatch{
				Kind:  PlistBinaryMatchRef,
				Node:  context.factNode(len(facts.objects) + len(facts.offsets) + index),
				Index: index, Owner: fact.owner, Position: fact.position,
				Target: fact.target, Span: fact.span,
			}); failure != nil {
				return nil, failure
			}
		}
	case "plist.trailer-facts":
		trailer := facts.trailer
		if failure := context.push(&output, PlistBinaryMatch{
			Kind: PlistBinaryMatchTrailer,
			Node: context.factNode(len(facts.objects) + len(facts.offsets) + len(facts.refs)),
			Span: trailer.span, SortVersion: trailer.sortVersion,
			OffsetIntSize: trailer.offsetIntSize, ObjectRefSize: trailer.objectRefSize,
			NumObjects: trailer.numObjects, TopObject: trailer.topObject,
			OffsetTableOffset: trailer.offsetTableOffset,
		}); failure != nil {
			return nil, failure
		}
	case "core.take":
		count := takeCount(operator)
		if count > len(input) {
			count = len(input)
		}
		for _, item := range input[:count] {
			if failure := context.push(&output, item); failure != nil {
				return nil, failure
			}
		}
	case "core.distinct-by-identity":
		seen := make(map[document.NodeRef]bool)
		for _, item := range input {
			if seen[item.Identity()] {
				continue
			}
			seen[item.Identity()] = true
			if failure := context.push(&output, item); failure != nil {
				return nil, failure
			}
		}
	default:
		return nil, &protocol.QueryFailure{Kind: protocol.FailureUnknownOperator}
	}
	if failure := context.step(ctx, len(output)); failure != nil {
		return nil, failure
	}
	return output, nil
}

// preorderRanks computes the pre-order document ranks of the arena, first
// visit winning for shared nodes (query.rs).
func preorderRanks(native *PlistDocument) []int {
	if native == nil {
		return nil
	}
	nodeCount := native.NodeCount()
	ranks := make([]int, nodeCount)
	visited := make([]bool, nodeCount)
	for index := range ranks {
		ranks[index] = -1
	}
	next := 0
	stack := []PlistValueRef{native.Root()}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[node.index] {
			continue
		}
		visited[node.index] = true
		ranks[node.index] = next
		next++
		value, _ := native.Get(node)
		switch value.kind {
		case PlistValueKindDict:
			for index := len(value.dict.entries) - 1; index >= 0; index-- {
				stack = append(stack, value.dict.entries[index].value)
			}
		case PlistValueKindArray:
			for index := len(value.array.elements) - 1; index >= 0; index-- {
				stack = append(stack, value.array.elements[index])
			}
		}
	}
	return ranks
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

// coreBooleanArgument reads one validated boolean operator argument.
func coreBooleanArgument(operator *protocol.OperatorCall, name string) bool {
	value, ok := operator.Arguments()[name]
	if !ok {
		return false
	}
	boolean, ok := value.(core.Boolean)
	if !ok {
		return false
	}
	return bool(boolean)
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

// applySelection applies the native cardinality selection.
func applySelection(values []PlistMatch, selection protocol.QuerySelection) ([]PlistMatch, *protocol.QueryFailure) {
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
func applySyntaxSelection(values []PlistSyntaxMatch, selection protocol.QuerySelection) ([]PlistSyntaxMatch, *protocol.QueryFailure) {
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

// applyBinarySelection applies the binary structure cardinality selection.
func applyBinarySelection(values []PlistBinaryMatch, selection protocol.QuerySelection) ([]PlistBinaryMatch, *protocol.QueryFailure) {
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
