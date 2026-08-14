package protocol

// Execution-level regression tests for the query engine
// (query_exec.go). The identity semantics are pinned against the Rust
// reference: GraphMatchIdentity for association matches is the (parent,
// ordinal) location (consema-rs/consema-graph/src/query.rs), and the
// portable identity is the full value path (consema-rs/consema-core/src/
// query.rs).

import (
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/graph"
)

// mustBindGraphQuery validates and binds a portable-graph query with the
// required capability.
func mustBindGraphQuery(t *testing.T, expression *QueryExpression) *ExecutableQuery {
	t.Helper()
	definition := NewQueryDefinition(DomainPortableGraphV1()).WithExpression(expression)
	validated, failure := definition.Validate()
	if failure != nil {
		t.Fatalf("validate failed: %v", failure)
	}
	capabilities := NewCapabilitySet()
	capabilities.Insert(NewCapabilityId("core.query.ordered-results", 1))
	executable, failure := validated.Bind(capabilities)
	if failure != nil {
		t.Fatalf("bind failed: %v", failure)
	}
	return executable
}

// buildSharedItemSequence builds the graph of the published
// query.distinct-shared-identity vector: one root Sequence [1, 1] over one
// shared Scalar node (conformance/vectors/portable-graph-v1.json).
func buildSharedItemSequence(t *testing.T) *graph.Graph {
	t.Helper()
	builder := graph.NewBuilder(graph.DefaultLimits())
	sequence, err := builder.ReserveNode()
	if err != nil {
		t.Fatal(err)
	}
	scalar, err := builder.ReserveNode()
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.DefineSequence(sequence, "tag:yaml.org,2002:seq", []graph.NodeID{scalar, scalar}); err != nil {
		t.Fatal(err)
	}
	if err := builder.DefineScalar(scalar, "tag:yaml.org,2002:str", "x"); err != nil {
		t.Fatal(err)
	}
	if err := builder.PushRoot(sequence); err != nil {
		t.Fatal(err)
	}
	built, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	return built
}

func TestGraphDistinctByAssociationIdentity(t *testing.T) {
	// The minimal counterexample of the G0.4/G0.5 audit: association
	// matches key by (parent, ordinal), so distinct directly after
	// try-sequence-elements keeps both elements of the shared-node
	// sequence [1, 1] (Rust returns 2 matches; a child-node identity
	// collapses them to 1).
	g := buildSharedItemSequence(t)
	expression := (&QueryExpression{Kind: ExpressionInput}).
		Then(NewOperatorCall("graph.try-sequence-elements", 1)).
		Then(NewOperatorCall("core.distinct-by-identity", 1))
	executable := mustBindGraphQuery(t, expression)
	matches, failure := executable.ExecuteGraph(g, DefaultQueryLimits())
	if failure != nil {
		t.Fatal(failure)
	}
	if len(matches) != 2 {
		t.Fatalf("distinct after try-sequence-elements = %d matches, want 2", len(matches))
	}
	root := g.Roots()[0]
	for index, match := range matches {
		if match.Kind != "SequenceElement" || match.Parent != root || match.Ordinal != uint64(index) {
			t.Errorf("match %d = %+v, want SequenceElement parent %v ordinal %d",
				index, match, root, index)
		}
	}
}

func TestGraphDistinctMappingEntryIdentity(t *testing.T) {
	// MappingEntry matches also key by (parent, ordinal): the duplicate
	// key/value associations of one mapping are distinct locations and
	// both survive distinct-by-identity.
	builder := graph.NewBuilder(graph.DefaultLimits())
	mapping, err := builder.ReserveNode()
	if err != nil {
		t.Fatal(err)
	}
	key, err := builder.ReserveNode()
	if err != nil {
		t.Fatal(err)
	}
	value, err := builder.ReserveNode()
	if err != nil {
		t.Fatal(err)
	}
	entries := []graph.MappingEntry{{Key: key, Value: value}, {Key: key, Value: value}}
	if err := builder.DefineMapping(mapping, "tag:yaml.org,2002:map", entries); err != nil {
		t.Fatal(err)
	}
	if err := builder.DefineScalar(key, "tag:yaml.org,2002:str", "k"); err != nil {
		t.Fatal(err)
	}
	if err := builder.DefineScalar(value, "tag:yaml.org,2002:str", "v"); err != nil {
		t.Fatal(err)
	}
	if err := builder.PushRoot(mapping); err != nil {
		t.Fatal(err)
	}
	g, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	expression := (&QueryExpression{Kind: ExpressionInput}).
		Then(NewOperatorCall("graph.try-mapping-entries", 1)).
		Then(NewOperatorCall("core.distinct-by-identity", 1))
	executable := mustBindGraphQuery(t, expression)
	matches, failure := executable.ExecuteGraph(g, DefaultQueryLimits())
	if failure != nil {
		t.Fatal(failure)
	}
	if len(matches) != 2 {
		t.Fatalf("distinct after try-mapping-entries = %d matches, want 2", len(matches))
	}
	for index, match := range matches {
		if match.Kind != "MappingEntry" || match.Parent != mapping || match.Ordinal != uint64(index) {
			t.Errorf("match %d = %+v, want MappingEntry parent %v ordinal %d",
				index, match, mapping, index)
		}
	}
}

func TestGraphDistinctNodeIdentityUnchanged(t *testing.T) {
	// The published vector query.distinct-shared-identity dedupes only
	// after sequence-element-node converts the matches to Node identity;
	// that path is unchanged by the association-identity fix and still
	// collapses the shared child node (expected builder_node_ids [1],
	// count 1).
	g := buildSharedItemSequence(t)
	expression := (&QueryExpression{Kind: ExpressionInput}).
		Then(NewOperatorCall("graph.try-sequence-elements", 1)).
		Then(NewOperatorCall("graph.sequence-element-node", 1)).
		Then(NewOperatorCall("core.distinct-by-identity", 1))
	executable := mustBindGraphQuery(t, expression)
	matches, failure := executable.ExecuteGraph(g, DefaultQueryLimits())
	if failure != nil {
		t.Fatal(failure)
	}
	if len(matches) != 1 || matches[0].Kind != "Node" || matches[0].Node.AsUint64() != 1 {
		t.Fatalf("distinct after sequence-element-node = %+v, want one Node match with builder id 1", matches)
	}
}

func TestPortableDistinctByPathIdentity(t *testing.T) {
	// The distinct-by-identity key of one portable match is its full value
	// path (PortableIdentity::Value(path), consema-rs/consema-core/src/
	// query.rs), not a path-length structural proxy: distinct
	// paths of equal length both survive.
	operator := NewOperatorCall("core.distinct-by-identity", 1)
	context := &portableContext{limits: DefaultQueryLimits()}
	input := []PortableMatch{
		{Path: RootValuePath().Child(ValuePathSegment{Kind: "SequenceElement", Index: 0}), Value: core.String("x")},
		{Path: RootValuePath().Child(ValuePathSegment{Kind: "SequenceElement", Index: 1}), Value: core.String("x")},
	}
	output, failure := applyPortableOperator(operator, input, context)
	if failure != nil {
		t.Fatal(failure)
	}
	if len(output) != 2 {
		t.Errorf("distinct on two equal-length distinct paths = %d matches, want 2", len(output))
	}
	// Same length, different segment kinds: also both survive.
	context = &portableContext{limits: DefaultQueryLimits()}
	input = []PortableMatch{
		{Path: RootValuePath().Child(ValuePathSegment{Kind: "ObjectValue", Key: "a"}), Value: core.String("x")},
		{Path: RootValuePath().Child(ValuePathSegment{Kind: "SequenceElement", Index: 0}), Value: core.String("y")},
	}
	output, failure = applyPortableOperator(operator, input, context)
	if failure != nil {
		t.Fatal(failure)
	}
	if len(output) != 2 {
		t.Errorf("distinct on equal-length different-kind paths = %d matches, want 2", len(output))
	}
	// Identical paths dedupe to one.
	context = &portableContext{limits: DefaultQueryLimits()}
	input = []PortableMatch{
		{Path: RootValuePath().Child(ValuePathSegment{Kind: "ObjectValue", Key: "a"}), Value: core.String("x")},
		{Path: RootValuePath().Child(ValuePathSegment{Kind: "ObjectValue", Key: "a"}), Value: core.String("y")},
	}
	output, failure = applyPortableOperator(operator, input, context)
	if failure != nil {
		t.Fatal(failure)
	}
	if len(output) != 1 {
		t.Errorf("distinct on identical paths = %d matches, want 1", len(output))
	}
}

func TestPortableDistinctThroughConcatPipeline(t *testing.T) {
	// A multi-match portable pipeline (two Input branches of a Concat)
	// reaches distinct-by-identity and keeps exactly one root-path match.
	expression := (&QueryExpression{
		Kind:     ExpressionConcat,
		Branches: []*QueryExpression{{Kind: ExpressionInput}, {Kind: ExpressionInput}},
	}).Then(NewOperatorCall("core.distinct-by-identity", 1))
	definition := NewQueryDefinition(DomainPortableValueV1()).WithExpression(expression)
	validated, failure := definition.Validate()
	if failure != nil {
		t.Fatalf("validate failed: %v", failure)
	}
	capabilities := NewCapabilitySet()
	capabilities.Insert(NewCapabilityId("core.query.ordered-results", 1))
	executable, failure := validated.Bind(capabilities)
	if failure != nil {
		t.Fatalf("bind failed: %v", failure)
	}
	matches, failure := executable.ExecutePortable(core.String("x"), DefaultQueryLimits())
	if failure != nil {
		t.Fatal(failure)
	}
	if len(matches) != 1 || !matches[0].Path.Equal(RootValuePath()) {
		t.Fatalf("distinct over two root matches = %+v, want one root-path match", matches)
	}
}

// TestDefaultQueryLimitsFrozen pins the frozen default limits (wave-4 R12,
// 2026-08-15): MaxSteps is 100,000 — the Rust reference's frozen value
// (consema-core/src/query.rs DefaultQueryLimits) — not the divergent
// 10,000,000 the Go side previously shipped. The behavior test: the step
// accounting fails with FailureResourceLimit exactly when the budget is
// exhausted (a budget below the work performed fails, never a silent
// unbounded run), and the frozen default accepts the same work.
func TestDefaultQueryLimitsFrozen(t *testing.T) {
	limits := DefaultQueryLimits()
	if limits.MaxResults != 1_000_000 {
		t.Fatalf("MaxResults = %d, want 1,000,000", limits.MaxResults)
	}
	if limits.MaxSteps != 100_000 {
		t.Fatalf("MaxSteps = %d, want 100,000 (frozen default, wave-4 R12)", limits.MaxSteps)
	}
	// The bare Input expression costs exactly one step: a budget of one
	// succeeds, a budget of zero fails with FailureResourceLimit.
	oneStep := &portableContext{limits: QueryLimits{MaxResults: 1_000_000, MaxSteps: 1}}
	if failure := oneStep.step(); failure != nil {
		t.Fatalf("one step under a budget of 1 must succeed, got %v", failure)
	}
	zeroBudget := &portableContext{limits: QueryLimits{MaxResults: 1_000_000, MaxSteps: 0}}
	if failure := zeroBudget.step(); failure == nil || failure.Kind != FailureResourceLimit {
		t.Fatalf("step under a budget of 0: failure = %v, want FailureResourceLimit", failure)
	}
}
