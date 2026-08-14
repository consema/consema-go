package ini

import (
	"context"
	"sort"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the INI native and lossless-syntax query domains
// (consema-ini query.rs; RFC 0009 §9). Native matches carry snapshot-bound
// roles in source order; syntax matches are source-order pieces whose text
// comparison uses the decoded scalar text, not the raw encoding bytes.
// Definitions validate domain, operator, argument types, and role
// composition before execution; steps and results are bounded by limits.

// IniMatchKind is the closed native match variant (query.rs).
type IniMatchKind string

// The five frozen native match variants.
const (
	// IniMatchDocument is a complete INI document.
	IniMatchDocument IniMatchKind = "Document"
	// IniMatchSection is one distinct section occurrence.
	IniMatchSection IniMatchKind = "Section"
	// IniMatchEntry is one distinct entry occurrence.
	IniMatchEntry IniMatchKind = "Entry"
	// IniMatchPhysicalLine is one exact physical source line.
	IniMatchPhysicalLine IniMatchKind = "PhysicalLine"
	// IniMatchLogicalLine is one logical INI record.
	IniMatchLogicalLine IniMatchKind = "LogicalLine"
)

// IniMatch is one owned snapshot-bound INI native semantic query match
// (query.rs).
type IniMatch struct {
	// Kind is the match variant.
	Kind IniMatchKind
	// Node is the primary match identity: the document root, section,
	// entry, physical-line, or logical-line NodeRef.
	Node document.NodeRef
	// Ordinal is the zero-based source-order ordinal of Section, Entry,
	// PhysicalLine, and LogicalLine matches.
	Ordinal int
	// Span is the exact raw span of PhysicalLine matches.
	Span document.Span
	// Name is the original section name of Section matches.
	Name string
	// ComparisonName is the profile comparison name of Section matches.
	ComparisonName string
	// IsDefault is the Python default-section fact of Section matches.
	IsDefault bool
	// DuplicateGroup is the duplicate/case-equivalence group of Section
	// and Entry matches.
	DuplicateGroup *uint32
	// Section is the owning section occurrence of Entry matches.
	Section document.NodeRef
	// Key is the original key spelling of Entry matches.
	Key string
	// ComparisonKey is the profile comparison key of Entry matches.
	ComparisonKey string
	// ValueState is the missing/empty/present fact of Entry matches.
	ValueState IniValueState
	// LogicalKind is the logical record kind of LogicalLine matches.
	LogicalKind IniLogicalLineKind
}

// IniSyntaxMatch is one owned snapshot-bound INI lossless syntax query
// match (query.rs).
type IniSyntaxMatch struct {
	node    document.NodeRef
	span    document.Span
	kind    IniSyntaxKind
	ordinal int
}

// NodeRef returns the process-local syntax-piece identity.
func (m IniSyntaxMatch) NodeRef() document.NodeRef { return m.node }

// Span returns the exact raw source span.
func (m IniSyntaxMatch) Span() document.Span { return m.span }

// Kind returns the format-specific lossless kind.
func (m IniSyntaxMatch) Kind() IniSyntaxKind { return m.kind }

// Ordinal returns the zero-based source-order position.
func (m IniSyntaxMatch) Ordinal() int { return m.ordinal }

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

func checkCancelled(ctx context.Context) *protocol.QueryFailure {
	select {
	case <-ctx.Done():
		return &protocol.QueryFailure{Kind: protocol.FailureCancelled}
	default:
		return nil
	}
}

func (c *queryContext) push(output *[]IniMatch, match IniMatch) *protocol.QueryFailure {
	observed := len(*output) + 1
	if observed > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	*output = append(*output, match)
	return nil
}

func (c *queryContext) appendMatches(output *[]IniMatch, values []IniMatch) *protocol.QueryFailure {
	observed := len(*output) + len(values)
	if observed > c.limits.MaxResults {
		return &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
	}
	*output = append(*output, values...)
	return nil
}

func (c *queryContext) sectionMatch(ordinal int) IniMatch {
	section := c.document.sections[ordinal]
	return IniMatch{
		Kind: IniMatchSection, Node: section.node, Ordinal: ordinal,
		Name: section.name, ComparisonName: section.comparisonName,
		IsDefault: section.isDefault, DuplicateGroup: section.duplicateGroup,
	}
}

func (c *queryContext) entryMatch(ordinal int) IniMatch {
	entry := c.document.entries[ordinal]
	return IniMatch{
		Kind: IniMatchEntry, Node: entry.node, Ordinal: ordinal,
		Section: entry.section, Key: entry.key, ComparisonKey: entry.comparisonKey,
		ValueState: entry.state, DuplicateGroup: entry.duplicateGroup,
	}
}

// ExecuteIniQuery executes a validated INI native semantic query against
// one immutable snapshot (query.rs). The context is used for
// cancellation and deadlines only. Steps and result counts are bounded by
// limits; exceeding either is core.query.resource-limit@1.
func ExecuteIniQuery(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) ([]IniMatch, *protocol.QueryFailure) {
	domain := executable.Definition().Domain()
	if domain.ID() != "ini.native-semantic-query" || domain.Version() != 1 {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	context := &queryContext{ctx: ctx, document: doc, limits: limits}
	if failure := context.step(1); failure != nil {
		return nil, failure
	}
	input := []IniMatch{{Kind: IniMatchDocument, Node: doc.rootNode}}
	matches, failure := executeQueryExpression(ctx, executable.Definition().Expression(),
		input, context)
	if failure != nil {
		return nil, failure
	}
	matches, failure = applyIniSelection(matches, executable.Definition().Selection())
	if failure != nil {
		return nil, failure
	}
	return matches, nil
}

// ExecuteIniQueryCursor executes an INI native query and exposes the
// complete result through a cancellable ordered cursor
// (query.rs; OrderedQueryCursor). The cursor yields the precomputed
// standard-order matches until the context is cancelled or the result is
// exhausted; the terminal state is Cancelled, Completed, or Failed.
func ExecuteIniQueryCursor(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) (*IniQueryCursor, *protocol.QueryFailure) {
	matches, failure := ExecuteIniQuery(ctx, executable, doc, limits)
	if failure != nil {
		return nil, failure
	}
	return &IniQueryCursor{ctx: ctx, remaining: matches, terminal: ""}, nil
}

// IniQueryCursor is the ordered cancellable native-result cursor.
type IniQueryCursor struct {
	ctx       context.Context
	remaining []IniMatch
	terminal  string
}

// Next yields the next standard-order match; the cursor stops with
// Cancelled when the context is cancelled and with Completed when the
// result is exhausted.
func (c *IniQueryCursor) Next() (IniMatch, bool) {
	if c.terminal != "" {
		return IniMatch{}, false
	}
	select {
	case <-c.ctx.Done():
		c.terminal = "Cancelled"
		return IniMatch{}, false
	default:
	}
	if len(c.remaining) == 0 {
		c.terminal = "Completed"
		return IniMatch{}, false
	}
	match := c.remaining[0]
	c.remaining = c.remaining[1:]
	return match, true
}

// TerminalState returns "Cancelled", "Completed", or "Failed" once the
// cursor is closed; empty while the cursor is still open.
func (c *IniQueryCursor) TerminalState() string { return c.terminal }

// ExecuteIniSyntaxQuery executes a validated INI lossless syntax query
// against every source piece in raw order (query.rs). Text
// comparison uses the decoded scalar text of the exact piece span, so
// UTF-8, UTF-16LE, and code-page sources behave identically.
func ExecuteIniSyntaxQuery(ctx context.Context, executable *protocol.ExecutableQuery,
	doc *Document, limits protocol.QueryLimits) ([]IniSyntaxMatch, *protocol.QueryFailure) {
	domain := executable.Definition().Domain()
	if domain.ID() != "ini.lossless-syntax-query" || domain.Version() != 1 {
		return nil, protocol.QueryFailureDomainMismatch(domain)
	}
	context := &queryContext{ctx: ctx, document: doc, limits: limits}
	pieces := doc.index.Pieces()
	if failure := context.step(len(pieces)); failure != nil {
		return nil, failure
	}
	input := make([]IniSyntaxMatch, 0, len(pieces))
	for ordinal, piece := range pieces {
		input = append(input, IniSyntaxMatch{
			node:    doc.authority.NodeRef(uint64(ordinal), document.RoleIniSyntaxPiece),
			span:    piece.Span(),
			kind:    doc.kinds[ordinal],
			ordinal: ordinal,
		})
	}
	matches, failure := executeSyntaxExpression(ctx, executable.Definition().Expression(),
		input, context)
	if failure != nil {
		return nil, failure
	}
	matches, failure = applySyntaxSelection(matches, executable.Definition().Selection())
	if failure != nil {
		return nil, failure
	}
	return matches, nil
}

// executeQueryExpression evaluates one native expression against the input
// matches (query.rs).
func executeQueryExpression(ctx context.Context, expression *protocol.QueryExpression,
	input []IniMatch, context *queryContext) ([]IniMatch, *protocol.QueryFailure) {
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
		var output []IniMatch
		for _, branch := range expression.Branches {
			matches, failure := executeQueryExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			if failure := context.appendMatches(&output, matches); failure != nil {
				return nil, failure
			}
			if failure := context.step(len(output)); failure != nil {
				return nil, failure
			}
		}
		return output, nil
	case protocol.ExpressionStructureOrderMerge:
		var output []IniMatch
		for _, branch := range expression.Branches {
			matches, failure := executeQueryExpression(ctx, branch, input, context)
			if failure != nil {
				return nil, failure
			}
			if failure := context.appendMatches(&output, matches); failure != nil {
				return nil, failure
			}
		}
		sort.SliceStable(output, func(i, j int) bool {
			left, right := nativeSourceOrder(context.document, &output[i]),
				nativeSourceOrder(context.document, &output[j])
			if left != right {
				return left < right
			}
			return output[i].Ordinal < output[j].Ordinal
		})
		if failure := context.step(len(output)); failure != nil {
			return nil, failure
		}
		return output, nil
	}
	return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
}

// executeSyntaxExpression evaluates one syntax expression (query.rs:
// 334-368).
func executeSyntaxExpression(ctx context.Context, expression *protocol.QueryExpression,
	input []IniSyntaxMatch, context *queryContext) ([]IniSyntaxMatch, *protocol.QueryFailure) {
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
		var output []IniSyntaxMatch
		for _, branch := range expression.Branches {
			matches, failure := executeSyntaxExpression(ctx, branch, input, context)
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
		var output []IniSyntaxMatch
		for _, branch := range expression.Branches {
			matches, failure := executeSyntaxExpression(ctx, branch, input, context)
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

// applyQueryOperator evaluates one native operator (query.rs).
func applyQueryOperator(ctx context.Context, operator *protocol.OperatorCall,
	input []IniMatch, context *queryContext) ([]IniMatch, *protocol.QueryFailure) {
	if failure := checkCancelled(ctx); failure != nil {
		return nil, failure
	}
	var output []IniMatch
	switch operator.ID() {
	case "ini.document-sections":
		for _, match := range input {
			if match.Kind == IniMatchDocument {
				for ordinal := range context.document.sections {
					if failure := context.push(&output, context.sectionMatch(ordinal)); failure != nil {
						return nil, failure
					}
				}
			}
		}
	case "ini.section-entries":
		for _, match := range input {
			if match.Kind == IniMatchSection {
				for ordinal, entry := range context.document.entries {
					if entry.section == match.Node {
						if failure := context.push(&output, context.entryMatch(ordinal)); failure != nil {
							return nil, failure
						}
					}
				}
			}
		}
	case "ini.all-entries":
		for _, match := range input {
			if match.Kind == IniMatchDocument {
				for ordinal := range context.document.entries {
					if failure := context.push(&output, context.entryMatch(ordinal)); failure != nil {
						return nil, failure
					}
				}
			}
		}
	case "ini.entry-section":
		for _, match := range input {
			if match.Kind == IniMatchEntry {
				ordinal := -1
				for index := range context.document.sections {
					if context.document.sections[index].node == match.Section {
						ordinal = index
						break
					}
				}
				if ordinal >= 0 {
					if failure := context.push(&output, context.sectionMatch(ordinal)); failure != nil {
						return nil, failure
					}
				}
			}
		}
	case "ini.section-name-equals":
		expected, ok := stringArgument(operator, "name")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Argument: "name"}
		}
		comparison, ok := stringArgument(operator, "comparison")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Argument: "comparison"}
		}
		equivalent := sectionComparison(context.document.profile, expected)
		for _, match := range input {
			if match.Kind != IniMatchSection {
				continue
			}
			matched := false
			if comparison == "OriginalExact" {
				matched = match.Name == expected
			} else {
				matched = match.ComparisonName == equivalent
			}
			if matched {
				if failure := context.push(&output, match); failure != nil {
					return nil, failure
				}
			}
		}
	case "ini.entry-key-equals":
		expected, ok := stringArgument(operator, "key")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Argument: "key"}
		}
		comparison, ok := stringArgument(operator, "comparison")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Argument: "comparison"}
		}
		equivalent := keyComparison(context.document.profile, expected)
		for _, match := range input {
			if match.Kind != IniMatchEntry {
				continue
			}
			matched := false
			if comparison == "OriginalExact" {
				matched = match.Key == expected
			} else {
				matched = match.ComparisonKey == equivalent
			}
			if matched {
				if failure := context.push(&output, match); failure != nil {
					return nil, failure
				}
			}
		}
	case "ini.entry-value-state-is":
		state, ok := stringArgument(operator, "state")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Argument: "state"}
		}
		for _, match := range input {
			if match.Kind == IniMatchEntry && match.ValueState.AsStr() == state {
				if failure := context.push(&output, match); failure != nil {
					return nil, failure
				}
			}
		}
	case "ini.duplicate-group":
		for _, match := range input {
			if match.Kind == IniMatchSection && match.DuplicateGroup != nil {
				for ordinal, section := range context.document.sections {
					if equalGroup(section.duplicateGroup, match.DuplicateGroup) {
						if failure := context.push(&output, context.sectionMatch(ordinal)); failure != nil {
							return nil, failure
						}
					}
				}
			}
			if match.Kind == IniMatchEntry && match.DuplicateGroup != nil {
				for ordinal, entry := range context.document.entries {
					if equalGroup(entry.duplicateGroup, match.DuplicateGroup) {
						if failure := context.push(&output, context.entryMatch(ordinal)); failure != nil {
							return nil, failure
						}
					}
				}
			}
		}
	case "ini.physical-lines":
		for _, match := range input {
			if match.Kind == IniMatchDocument {
				for ordinal, line := range context.document.physicalLines {
					if failure := context.push(&output, IniMatch{
						Kind: IniMatchPhysicalLine, Node: line.node, Ordinal: ordinal,
						Span: line.span,
					}); failure != nil {
						return nil, failure
					}
				}
			}
		}
	case "ini.logical-lines":
		for _, match := range input {
			if match.Kind == IniMatchDocument {
				for ordinal, line := range context.document.logicalLines {
					if failure := context.push(&output, IniMatch{
						Kind: IniMatchLogicalLine, Node: line.node, Ordinal: ordinal,
						LogicalKind: line.kind,
					}); failure != nil {
						return nil, failure
					}
				}
			}
		}
	case "core.take":
		count, ok := integerArgument(operator, "count")
		if !ok || count < 0 {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Argument: "count"}
		}
		taken := 0
		for _, match := range input {
			if taken >= count {
				break
			}
			if failure := context.push(&output, match); failure != nil {
				return nil, failure
			}
			taken++
		}
	case "core.distinct-by-identity":
		seen := map[document.NodeRef]bool{}
		for _, match := range input {
			if seen[match.Node] {
				continue
			}
			seen[match.Node] = true
			if failure := context.push(&output, match); failure != nil {
				return nil, failure
			}
		}
	default:
		return nil, &protocol.QueryFailure{Kind: protocol.FailureUnknownOperator,
			Operator: operator.ID()}
	}
	if failure := context.step(len(output)); failure != nil {
		return nil, failure
	}
	return output, nil
}

// applySyntaxOperator evaluates one syntax operator (query.rs).
func applySyntaxOperator(ctx context.Context, operator *protocol.OperatorCall,
	input []IniSyntaxMatch, context *queryContext) ([]IniSyntaxMatch, *protocol.QueryFailure) {
	if failure := checkCancelled(ctx); failure != nil {
		return nil, failure
	}
	var output []IniSyntaxMatch
	switch operator.ID() {
	case "ini.syntax-kind-is":
		kindName, ok := stringArgument(operator, "kind")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Argument: "kind"}
		}
		expected, ok := IniSyntaxKindFromName(kindName)
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Argument: "kind"}
		}
		for _, match := range input {
			if match.kind == expected {
				output = append(output, match)
				if len(output) > context.limits.MaxResults {
					return nil, &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
				}
			}
		}
	case "ini.syntax-text-equals":
		expected, ok := stringArgument(operator, "text")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Argument: "text"}
		}
		for _, match := range input {
			text, ok := context.document.decodedTextOf(match.span)
			if ok && text == expected {
				output = append(output, match)
				if len(output) > context.limits.MaxResults {
					return nil, &protocol.QueryFailure{Kind: protocol.FailureResourceLimit}
				}
			}
		}
	case "core.take":
		count, ok := integerArgument(operator, "count")
		if !ok || count < 0 {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Argument: "count"}
		}
		taken := 0
		for _, match := range input {
			if taken >= count {
				break
			}
			output = append(output, match)
			taken++
		}
	case "core.distinct-by-identity":
		seen := map[document.NodeRef]bool{}
		for _, match := range input {
			if seen[match.node] {
				continue
			}
			seen[match.node] = true
			output = append(output, match)
		}
	default:
		return nil, &protocol.QueryFailure{Kind: protocol.FailureUnknownOperator,
			Operator: operator.ID()}
	}
	if failure := context.step(len(output)); failure != nil {
		return nil, failure
	}
	return output, nil
}

// nativeSourceOrder returns the source-order key of one native match
// (query.rs).
func nativeSourceOrder(document *Document, match *IniMatch) int {
	switch match.Kind {
	case IniMatchDocument:
		return 0
	case IniMatchSection:
		section, ok := document.Section(match.Node)
		if !ok {
			return match.Ordinal
		}
		return section.span.StartByte()
	case IniMatchEntry:
		entry, ok := document.Entry(match.Node)
		if !ok {
			return match.Ordinal
		}
		return entry.span.StartByte()
	case IniMatchPhysicalLine:
		return match.Span.StartByte()
	case IniMatchLogicalLine:
		logical, ok := document.LogicalLine(match.Node)
		if !ok {
			return match.Ordinal
		}
		lines := logical.PhysicalLines()
		if len(lines) == 0 {
			return match.Ordinal
		}
		if physical, ok := document.PhysicalLine(lines[0]); ok {
			return physical.span.StartByte()
		}
		return match.Ordinal
	}
	return match.Ordinal
}

// sectionComparison applies the profile comparison rule to one name
// (query.rs).
func sectionComparison(profile IniProfile, name string) string {
	if profile.isWindows() {
		return stringsToLowerASCII(name)
	}
	return name
}

// keyComparison applies the profile comparison rule to one key
// (query.rs).
func keyComparison(profile IniProfile, key string) string {
	switch {
	case profile.isWindows():
		return stringsToLowerASCII(key)
	case profile.isPython():
		return optionxform(key)
	}
	return key
}

// stringsToLowerASCII lowercases only ASCII bytes (the Windows profile
// comparison rule).
func stringsToLowerASCII(value string) string {
	lowered := []byte(value)
	for index := range lowered {
		if lowered[index] >= 'A' && lowered[index] <= 'Z' {
			lowered[index] += 'a' - 'A'
		}
	}
	return string(lowered)
}

// equalGroup compares two group identities by value.
func equalGroup(left, right *uint32) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

// stringArgument reads one validated String argument.
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

// integerArgument reads one validated Integer argument.
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

// applyIniSelection applies the definition selection (query.rs).
func applyIniSelection(matches []IniMatch,
	selection protocol.QuerySelection) ([]IniMatch, *protocol.QueryFailure) {
	switch selection {
	case protocol.SelectionAll:
		return matches, nil
	case protocol.SelectionFirst:
		if len(matches) > 1 {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureCardinalityViolation}
		}
		return matches, nil
	case protocol.SelectionLast:
		if len(matches) > 1 {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureCardinalityViolation}
		}
		return matches, nil
	case protocol.SelectionZeroOrOne:
		if len(matches) > 1 {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureCardinalityViolation}
		}
		return matches, nil
	case protocol.SelectionRequireOne:
		if len(matches) != 1 {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureCardinalityViolation}
		}
		return matches, nil
	}
	return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
}

// applySyntaxSelection applies the definition selection to syntax matches.
func applySyntaxSelection(matches []IniSyntaxMatch,
	selection protocol.QuerySelection) ([]IniSyntaxMatch, *protocol.QueryFailure) {
	switch selection {
	case protocol.SelectionAll:
		return matches, nil
	case protocol.SelectionFirst:
		if len(matches) > 1 {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureCardinalityViolation}
		}
		return matches, nil
	case protocol.SelectionLast:
		if len(matches) > 1 {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureCardinalityViolation}
		}
		return matches, nil
	case protocol.SelectionZeroOrOne:
		if len(matches) > 1 {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureCardinalityViolation}
		}
		return matches, nil
	case protocol.SelectionRequireOne:
		if len(matches) != 1 {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureCardinalityViolation}
		}
		return matches, nil
	}
	return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
}
