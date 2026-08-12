package protocol

// Minimal portable-value and portable-graph query execution for the
// 0.14.0 milestone. The Go query types pin the definition surface
// (query.go); this file adds the execution of the operator subset exercised
// by the shared conformance vectors: the bare `Input` root match on
// portable values and the graph operators of `core.portable-graph-query@1`
// (graph.reachable-nodes, graph.try-sequence-elements,
// graph.sequence-element-node, core.distinct-by-identity, core.take).
// Family-domain execution lands with the owning format milestones.

import (
	"strconv"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/graph"
)

// QueryLimits are the resource bounds of query execution (the Rust
// QueryLimits; consema-core/src/query.rs).
type QueryLimits struct {
	// MaxResults is the maximum standard result matches.
	MaxResults int
	// MaxSteps is the maximum engine steps.
	MaxSteps int
}

// DefaultQueryLimits returns the frozen defaults (1,000,000 results,
// 10,000,000 steps).
func DefaultQueryLimits() QueryLimits {
	return QueryLimits{MaxResults: 1_000_000, MaxSteps: 10_000_000}
}

// PortableMatch is one executed portable-domain match.
type PortableMatch struct {
	// Path is the match value path.
	Path ValuePath
	// Value is the match value.
	Value core.Value
}

// GraphMatch is one executed graph-domain match.
type GraphMatch struct {
	// Kind is "Node", "SequenceElement", or "MappingEntry".
	Kind string
	// Node is the graph-local node ID of Node matches.
	Node graph.NodeID
	// Parent is the graph-local parent ID of association matches.
	Parent graph.NodeID
	// Ordinal is the zero-based association ordinal.
	Ordinal uint64
	// Key is the graph-local key node ID of MappingEntry matches.
	Key graph.NodeID
	// Value is the graph-local value node ID of MappingEntry matches.
	Value graph.NodeID
}

// graphMatchIdentity is the distinct-by-identity key of one graph match
// (the Rust GraphMatchIdentity, crates/consema-graph/src/query.rs:78-83):
// Node matches key by node ID; SequenceElement and MappingEntry matches key
// by the (parent, ordinal) association location.
type graphMatchIdentity struct {
	kind    string
	node    graph.NodeID
	parent  graph.NodeID
	ordinal uint64
}

// identity returns the distinct-by-identity key of one match.
func (m GraphMatch) identity() graphMatchIdentity {
	switch m.Kind {
	case "Node":
		return graphMatchIdentity{kind: "Node", node: m.Node}
	case "SequenceElement":
		return graphMatchIdentity{kind: "SequenceElement", parent: m.Parent, ordinal: m.Ordinal}
	case "MappingEntry":
		return graphMatchIdentity{kind: "MappingEntry", parent: m.Parent, ordinal: m.Ordinal}
	}
	return graphMatchIdentity{kind: "Node", node: m.Node}
}

// ExecutePortable executes the validated query against one portable value
// and returns the ordered matches. The portable-value domain of the shared
// vectors exercises the bare Input expression (the root match).
func (e *ExecutableQuery) ExecutePortable(value core.Value, limits QueryLimits) ([]PortableMatch, *QueryFailure) {
	if !e.validated.definition.domain.Equal(DomainPortableValueV1()) {
		return nil, QueryFailureDomainMismatch(e.validated.definition.domain)
	}
	context := &portableContext{value: value, limits: limits}
	matches, failure := evaluatePortable(e.validated.definition.expression, context)
	if failure != nil {
		return nil, failure
	}
	return applySelection(matches, e.validated.definition.selection, context)
}

type portableContext struct {
	value   core.Value
	limits  QueryLimits
	results int
	steps   int
}

func (c *portableContext) step() *QueryFailure {
	c.steps++
	if c.steps > c.limits.MaxSteps {
		return &QueryFailure{Kind: FailureResourceLimit}
	}
	return nil
}

func (c *portableContext) checkResults() *QueryFailure {
	c.results++
	if c.results > c.limits.MaxResults {
		return &QueryFailure{Kind: FailureResourceLimit}
	}
	return nil
}

func evaluatePortable(expression *QueryExpression, context *portableContext) ([]PortableMatch, *QueryFailure) {
	switch expression.Kind {
	case ExpressionInput:
		if failure := context.step(); failure != nil {
			return nil, failure
		}
		match := PortableMatch{Path: RootValuePath(), Value: context.value}
		if failure := context.checkResults(); failure != nil {
			return nil, failure
		}
		return []PortableMatch{match}, nil
	case ExpressionApply:
		input, failure := evaluatePortable(expression.Input, context)
		if failure != nil {
			return nil, failure
		}
		return applyPortableOperator(expression.Operator, input, context)
	case ExpressionConcat:
		var output []PortableMatch
		for _, branch := range expression.Branches {
			values, failure := evaluatePortable(branch, context)
			if failure != nil {
				return nil, failure
			}
			output = append(output, values...)
		}
		return output, nil
	case ExpressionStructureOrderMerge:
		return nil, &QueryFailure{Kind: FailureTargetUnavailable}
	}
	return nil, &QueryFailure{Kind: FailureTargetUnavailable}
}

func applyPortableOperator(operator *OperatorCall, input []PortableMatch,
	context *portableContext) ([]PortableMatch, *QueryFailure) {
	switch operator.id {
	case "core.take":
		count := int(coreIntegerOf(operator.arguments["count"]))
		if count > len(input) {
			count = len(input)
		}
		output := make([]PortableMatch, 0, count)
		for _, match := range input[:count] {
			if failure := context.checkResults(); failure != nil {
				return nil, failure
			}
			output = append(output, match)
		}
		return output, nil
	case "core.distinct-by-identity":
		var output []PortableMatch
		seen := make(map[string]bool)
		for _, match := range input {
			if failure := context.step(); failure != nil {
				return nil, failure
			}
			key := identityOfPortable(match)
			if seen[key] {
				continue
			}
			seen[key] = true
			if failure := context.checkResults(); failure != nil {
				return nil, failure
			}
			output = append(output, match)
		}
		return output, nil
	default:
		// The shared vectors exercise only the bare Input expression of the
		// portable-value domain; deeper operators land with the format
		// milestones.
		return nil, &QueryFailure{Kind: FailureTargetUnavailable}
	}
}

func coreIntegerOf(value core.Value) uint64 {
	integer, ok := value.(core.Integer)
	if !ok {
		return 0
	}
	return integer.Int().Uint64()
}

func identityOfPortable(match PortableMatch) string {
	// The distinct-by-identity key of one portable match is its full value
	// path (the Rust PortableIdentity::Value(path),
	// crates/consema-core/src/query.rs:2248-2262), not a structural proxy:
	// every segment contributes its kind and its key or ordinal, so two
	// matches dedupe exactly when their paths are identical.
	var key strings.Builder
	for _, segment := range match.Path.Segments() {
		key.WriteString(segment.Kind)
		key.WriteByte(':')
		content := segment.Key
		if segment.Kind != "ObjectValue" {
			content = strconv.FormatUint(segment.Index, 10)
		}
		key.WriteString(strconv.Itoa(len(content)))
		key.WriteByte(':')
		key.WriteString(content)
		key.WriteByte(';')
	}
	return key.String()
}

// ExecuteGraph executes the validated query against one graph and returns
// the ordered matches with graph-local node IDs.
func (e *ExecutableQuery) ExecuteGraph(g *graph.Graph, limits QueryLimits) ([]GraphMatch, *QueryFailure) {
	if !e.validated.definition.domain.Equal(DomainPortableGraphV1()) {
		return nil, QueryFailureDomainMismatch(e.validated.definition.domain)
	}
	context := &graphContext{graph: g, limits: limits}
	matches, failure := evaluateGraph(e.validated.definition.expression, context)
	if failure != nil {
		return nil, failure
	}
	return applyGraphSelection(matches, e.validated.definition.selection, context)
}

type graphContext struct {
	graph   *graph.Graph
	limits  QueryLimits
	results int
	steps   int
}

func (c *graphContext) step() *QueryFailure {
	c.steps++
	if c.steps > c.limits.MaxSteps {
		return &QueryFailure{Kind: FailureResourceLimit}
	}
	return nil
}

func (c *graphContext) checkResults() *QueryFailure {
	c.results++
	if c.results > c.limits.MaxResults {
		return &QueryFailure{Kind: FailureResourceLimit}
	}
	return nil
}

func evaluateGraph(expression *QueryExpression, context *graphContext) ([]GraphMatch, *QueryFailure) {
	switch expression.Kind {
	case ExpressionInput:
		roots := context.graph.Roots()
		output := make([]GraphMatch, 0, len(roots))
		for _, root := range roots {
			if failure := context.step(); failure != nil {
				return nil, failure
			}
			if failure := context.checkResults(); failure != nil {
				return nil, failure
			}
			output = append(output, GraphMatch{Kind: "Node", Node: root})
		}
		return output, nil
	case ExpressionApply:
		input, failure := evaluateGraph(expression.Input, context)
		if failure != nil {
			return nil, failure
		}
		return applyGraphOperator(expression.Operator, input, context)
	case ExpressionConcat:
		var output []GraphMatch
		for _, branch := range expression.Branches {
			values, failure := evaluateGraph(branch, context)
			if failure != nil {
				return nil, failure
			}
			output = append(output, values...)
		}
		return output, nil
	case ExpressionStructureOrderMerge:
		return nil, &QueryFailure{Kind: FailureTargetUnavailable}
	}
	return nil, &QueryFailure{Kind: FailureTargetUnavailable}
}

func applyGraphOperator(operator *OperatorCall, input []GraphMatch,
	context *graphContext) ([]GraphMatch, *QueryFailure) {
	var output []GraphMatch
	switch operator.id {
	case "core.take":
		count := int(coreIntegerOf(operator.arguments["count"]))
		if count > len(input) {
			count = len(input)
		}
		output = make([]GraphMatch, 0, count)
		for _, match := range input[:count] {
			if failure := context.checkResults(); failure != nil {
				return nil, failure
			}
			output = append(output, match)
		}
		return output, nil
	case "core.distinct-by-identity":
		seen := make(map[graphMatchIdentity]bool)
		for _, match := range input {
			if failure := context.step(); failure != nil {
				return nil, failure
			}
			if seen[match.identity()] {
				continue
			}
			seen[match.identity()] = true
			if failure := context.checkResults(); failure != nil {
				return nil, failure
			}
			output = append(output, match)
		}
		return output, nil
	case "graph.reachable-nodes":
		seen := make(map[graph.NodeID]bool)
		for _, match := range input {
			if match.Kind != "Node" {
				return nil, &QueryFailure{Kind: FailureTargetUnavailable}
			}
			stack := []graph.NodeID{match.Node}
			for len(stack) > 0 {
				node := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if failure := context.step(); failure != nil {
					return nil, failure
				}
				if seen[node] {
					continue
				}
				seen[node] = true
				if failure := context.checkResults(); failure != nil {
					return nil, failure
				}
				output = append(output, GraphMatch{Kind: "Node", Node: node})
				stack = append(stack, outgoingNodes(context.graph, node)...)
			}
		}
		return output, nil
	case "graph.try-sequence-elements":
		for _, match := range input {
			if failure := context.step(); failure != nil {
				return nil, failure
			}
			if match.Kind != "Node" {
				return nil, &QueryFailure{Kind: FailureTargetUnavailable}
			}
			node, ok := context.graph.Node(match.Node)
			if !ok {
				return nil, &QueryFailure{Kind: FailureTargetUnavailable}
			}
			items, ok := node.SequenceItems()
			if !ok {
				continue
			}
			for ordinal, item := range items {
				if failure := context.checkResults(); failure != nil {
					return nil, failure
				}
				output = append(output, GraphMatch{
					Kind: "SequenceElement", Parent: match.Node,
					Ordinal: uint64(ordinal), Node: item,
				})
			}
		}
		return output, nil
	case "graph.sequence-element-node":
		for _, match := range input {
			if failure := context.step(); failure != nil {
				return nil, failure
			}
			if match.Kind != "SequenceElement" {
				return nil, &QueryFailure{Kind: FailureTargetUnavailable}
			}
			if failure := context.checkResults(); failure != nil {
				return nil, failure
			}
			output = append(output, GraphMatch{Kind: "Node", Node: match.Node})
		}
		return output, nil
	case "graph.try-mapping-entries":
		for _, match := range input {
			if failure := context.step(); failure != nil {
				return nil, failure
			}
			if match.Kind != "Node" {
				return nil, &QueryFailure{Kind: FailureTargetUnavailable}
			}
			node, ok := context.graph.Node(match.Node)
			if !ok {
				return nil, &QueryFailure{Kind: FailureTargetUnavailable}
			}
			entries, ok := node.MappingEntries()
			if !ok {
				continue
			}
			for ordinal, entry := range entries {
				if failure := context.checkResults(); failure != nil {
					return nil, failure
				}
				output = append(output, GraphMatch{
					Kind: "MappingEntry", Parent: match.Node,
					Ordinal: uint64(ordinal), Key: entry.Key, Value: entry.Value,
				})
			}
		}
		return output, nil
	case "graph.mapping-entry-key", "graph.mapping-entry-value":
		for _, match := range input {
			if failure := context.step(); failure != nil {
				return nil, failure
			}
			if match.Kind != "MappingEntry" {
				return nil, &QueryFailure{Kind: FailureTargetUnavailable}
			}
			if failure := context.checkResults(); failure != nil {
				return nil, failure
			}
			node := match.Value
			if operator.id == "graph.mapping-entry-key" {
				node = match.Key
			}
			output = append(output, GraphMatch{Kind: "Node", Node: node})
		}
		return output, nil
	default:
		return nil, &QueryFailure{Kind: FailureTargetUnavailable}
	}
}

// outgoingNodes returns the ordered outgoing children of one node in
// canonical first-visit order (sequence items, then mapping keys then
// values).
func outgoingNodes(g *graph.Graph, id graph.NodeID) []graph.NodeID {
	node, ok := g.Node(id)
	if !ok {
		return nil
	}
	var outgoing []graph.NodeID
	switch node.Kind() {
	case graph.KindSequence:
		items, _ := node.SequenceItems()
		outgoing = append(outgoing, items...)
	case graph.KindMapping:
		entries, _ := node.MappingEntries()
		for _, entry := range entries {
			outgoing = append(outgoing, entry.Key, entry.Value)
		}
	}
	// The Rust engine pops a LIFO stack seeded with the reversed outgoing
	// edges, so the emitted order is the forward edge order.
	reversed := make([]graph.NodeID, 0, len(outgoing))
	for index := len(outgoing) - 1; index >= 0; index-- {
		reversed = append(reversed, outgoing[index])
	}
	return reversed
}

func applySelection(matches []PortableMatch, selection QuerySelection,
	context *portableContext) ([]PortableMatch, *QueryFailure) {
	switch selection {
	case SelectionAll, "":
		return matches, nil
	case SelectionFirst:
		if len(matches) > 0 {
			return matches[:1], nil
		}
		return nil, nil
	case SelectionLast:
		if len(matches) > 0 {
			return matches[len(matches)-1:], nil
		}
		return nil, nil
	case SelectionZeroOrOne:
		if len(matches) > 1 {
			return nil, &QueryFailure{Kind: FailureCardinalityViolation}
		}
		return matches, nil
	case SelectionRequireOne:
		if len(matches) != 1 {
			return nil, &QueryFailure{Kind: FailureCardinalityViolation}
		}
		return matches, nil
	}
	return nil, &QueryFailure{Kind: FailureInvalidArgument}
}

func applyGraphSelection(matches []GraphMatch, selection QuerySelection,
	context *graphContext) ([]GraphMatch, *QueryFailure) {
	switch selection {
	case SelectionAll, "":
		return matches, nil
	case SelectionFirst:
		if len(matches) > 0 {
			return matches[:1], nil
		}
		return nil, nil
	case SelectionLast:
		if len(matches) > 0 {
			return matches[len(matches)-1:], nil
		}
		return nil, nil
	case SelectionZeroOrOne:
		if len(matches) > 1 {
			return nil, &QueryFailure{Kind: FailureCardinalityViolation}
		}
		return matches, nil
	case SelectionRequireOne:
		if len(matches) != 1 {
			return nil, &QueryFailure{Kind: FailureCardinalityViolation}
		}
		return matches, nil
	}
	return nil, &QueryFailure{Kind: FailureInvalidArgument}
}
