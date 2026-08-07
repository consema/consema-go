package yaml

import (
	"context"
	"math/big"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/protocol"
)

func yamlExecutable(t *testing.T, domain *protocol.QueryDomain,
	expression *protocol.QueryExpression) *protocol.ExecutableQuery {
	t.Helper()
	definition := protocol.NewQueryDefinition(domain).WithExpression(expression)
	validated, failure := definition.Validate()
	if failure != nil {
		t.Fatalf("validation: %s", failure.Code())
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	bound, failure := validated.Bind(capabilities)
	if failure != nil {
		t.Fatalf("binding: %s", failure.Code())
	}
	return bound
}

func runNativeQuery(t *testing.T, doc *Document,
	expression *protocol.QueryExpression) []YamlMatch {
	t.Helper()
	executable := yamlExecutable(t, protocol.DomainYAMLNativeV1(), expression)
	matches, failure := ExecuteYamlQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("query: %s", failure.Code())
	}
	return matches
}

// TestQueryWhereOperators pins the node-kind, tag, and canonical filters.
func TestQueryWhereOperators(t *testing.T) {
	doc := mustParse(t, "a: 1\nb: true\nc: [x]\n", Yaml12CoreV1)
	base := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("yaml.documents", 1)).
		Then(protocol.NewOperatorCall("yaml.document-root", 1)).
		Then(protocol.NewOperatorCall("yaml.try-mapping-entries", 1)).
		Then(protocol.NewOperatorCall("yaml.mapping-entry-value", 1))
	expression := base.Then(protocol.NewOperatorCall("yaml.where-node-kind", 1).
		WithArgument("kind", core.String("Scalar")))
	matches := runNativeQuery(t, doc, expression)
	if len(matches) != 2 {
		t.Fatalf("where-node-kind: %d", len(matches))
	}
	expression = base.Then(protocol.NewOperatorCall("yaml.where-tag", 1).
		WithArgument("tag", core.String("tag:yaml.org,2002:bool")))
	matches = runNativeQuery(t, doc, expression)
	if len(matches) != 1 {
		t.Fatalf("where-tag: %d", len(matches))
	}
	expression = base.Then(protocol.NewOperatorCall("yaml.scalar-canonical-equals", 1).
		WithArgument("canonical", core.String("true")))
	matches = runNativeQuery(t, doc, expression)
	if len(matches) != 1 {
		t.Fatalf("canonical-equals: %d", len(matches))
	}
}

// TestQueryAnchorOperators pins the anchor-definition and anchor-node
// round trip.
func TestQueryAnchorOperators(t *testing.T) {
	doc := mustParse(t, "&a [one]\n", Yaml12CoreV1)
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("yaml.documents", 1)).
		Then(protocol.NewOperatorCall("yaml.document-root", 1)).
		Then(protocol.NewOperatorCall("yaml.anchor-definition", 1))
	matches := runNativeQuery(t, doc, expression)
	if len(matches) != 1 {
		t.Fatalf("anchor-definition: %d", len(matches))
	}
	if matches[0].Kind != YamlMatchAnchorDefinition || matches[0].Name != "a" {
		t.Fatalf("anchor match %v", matches[0])
	}
	expression = expression.Then(protocol.NewOperatorCall("yaml.anchor-node", 1))
	matches = runNativeQuery(t, doc, expression)
	if len(matches) != 1 || matches[0].Kind != YamlMatchNode {
		t.Fatalf("anchor-node: %d", len(matches))
	}
}

// TestQuerySequenceAndMappingNavigation pins the association operators in
// presentation order.
func TestQuerySequenceAndMappingNavigation(t *testing.T) {
	doc := mustParse(t, "{a: [1, 2], b: x}\n", Yaml12CoreV1)
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("yaml.documents", 1)).
		Then(protocol.NewOperatorCall("yaml.document-root", 1)).
		Then(protocol.NewOperatorCall("yaml.try-mapping-entries", 1)).
		Then(protocol.NewOperatorCall("yaml.mapping-entry-value", 1)).
		Then(protocol.NewOperatorCall("yaml.try-sequence-elements", 1))
	matches := runNativeQuery(t, doc, expression)
	if len(matches) != 2 {
		t.Fatalf("sequence elements: %d", len(matches))
	}
	if matches[0].Ordinal != 0 || matches[1].Ordinal != 1 {
		t.Fatalf("presentation order lost")
	}
	expression = expression.Then(protocol.NewOperatorCall("yaml.sequence-element-node", 1))
	matches = runNativeQuery(t, doc, expression)
	if len(matches) != 2 {
		t.Fatalf("element nodes: %d", len(matches))
	}
	expression = (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("yaml.documents", 1)).
		Then(protocol.NewOperatorCall("yaml.document-root", 1)).
		Then(protocol.NewOperatorCall("yaml.try-mapping-entries", 1)).
		Then(protocol.NewOperatorCall("yaml.mapping-entry-key", 1))
	matches = runNativeQuery(t, doc, expression)
	if len(matches) != 2 {
		t.Fatalf("entry keys: %d", len(matches))
	}
	// The try operators skip non-matching nodes silently.
	expression = (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("yaml.documents", 1)).
		Then(protocol.NewOperatorCall("yaml.document-root", 1)).
		Then(protocol.NewOperatorCall("yaml.try-sequence-elements", 1))
	matches = runNativeQuery(t, doc, expression)
	if len(matches) != 0 {
		t.Fatalf("mapping must not yield sequence elements: %d", len(matches))
	}
}

// TestQueryAliasTarget pins the alias-target round trip.
func TestQueryAliasTarget(t *testing.T) {
	doc := mustParse(t, "first: &x [one]\ncopy: *x\n", Yaml12CoreV1)
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("yaml.alias-occurrences", 1)).
		Then(protocol.NewOperatorCall("yaml.alias-target", 1))
	matches := runNativeQuery(t, doc, expression)
	if len(matches) != 1 || matches[0].Kind != YamlMatchNode {
		t.Fatalf("alias target %v", matches)
	}
}

// TestQueryTakeAndDistinct pins the role-preserving operators.
func TestQueryTakeAndDistinct(t *testing.T) {
	doc := mustParse(t, "a: 1\nb: 2\nc: 3\n", Yaml12CoreV1)
	base := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("yaml.documents", 1)).
		Then(protocol.NewOperatorCall("yaml.document-root", 1)).
		Then(protocol.NewOperatorCall("yaml.try-mapping-entries", 1))
	expression := base.Then(protocol.NewOperatorCall("core.take", 1).
		WithArgument("count", core.NewInteger(big.NewInt(2))))
	matches := runNativeQuery(t, doc, expression)
	if len(matches) != 2 {
		t.Fatalf("take: %d", len(matches))
	}
	concat := &protocol.QueryExpression{Kind: protocol.ExpressionConcat, Branches: []*protocol.QueryExpression{
		base.Then(protocol.NewOperatorCall("yaml.mapping-entry-value", 1)),
		base.Then(protocol.NewOperatorCall("yaml.mapping-entry-key", 1)),
	}}
	expression = concat.Then(protocol.NewOperatorCall("core.distinct-by-identity", 1))
	matches = runNativeQuery(t, doc, expression)
	if len(matches) != 6 {
		t.Fatalf("distinct: %d", len(matches))
	}
}

// TestQuerySyntaxTextEquals pins the raw-byte text comparison.
func TestQuerySyntaxTextEquals(t *testing.T) {
	doc := mustParse(t, "a: 1 # first\nb: 2 # second\n", Yaml12CoreV1)
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("yaml.syntax-kind-is", 1).
			WithArgument("kind", core.String("Comment"))).
		Then(protocol.NewOperatorCall("yaml.syntax-text-equals", 1).
			WithArgument("text", core.String("# second")))
	executable := yamlExecutable(t, protocol.DomainYAMLLosslessSyntaxV1(), expression)
	matches, failure := ExecuteYamlSyntaxQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if failure != nil || len(matches) != 1 || matches[0].Ordinal() != 12 {
		t.Fatalf("text-equals: %v %d", failure, len(matches))
	}
}

// TestQueryDomainGate pins the execution-time domain gate: a native
// expression executed against the syntax entry point fails with the
// domain-mismatch code (defense in depth behind validation).
func TestQueryDomainGate(t *testing.T) {
	doc := mustParse(t, "a: 1\n", Yaml12CoreV1)
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("yaml.documents", 1))
	executable := yamlExecutable(t, protocol.DomainYAMLNativeV1(), expression)
	_, failure := ExecuteYamlSyntaxQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if failure == nil || failure.Code() != "core.query.domain-mismatch@1" {
		t.Fatalf("domain gate: %v", failure)
	}
}

// TestQueryValidationRejectsComposition pins the role-composition
// validation (query.rs test).
func TestQueryValidationRejectsComposition(t *testing.T) {
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("yaml.document-root", 1))
	definition := protocol.NewQueryDefinition(protocol.DomainYAMLNativeV1()).
		WithExpression(expression)
	_, failure := definition.Validate()
	if failure == nil || failure.Code() != "core.query.invalid-composition@1" {
		t.Fatalf("composition validation: %v", failure)
	}
}

// TestQuerySyntaxKindValidation pins the closed kind argument.
func TestQuerySyntaxKindValidation(t *testing.T) {
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("yaml.syntax-kind-is", 1).
			WithArgument("kind", core.String("NotAKind")))
	definition := protocol.NewQueryDefinition(protocol.DomainYAMLLosslessSyntaxV1()).
		WithExpression(expression)
	_, failure := definition.Validate()
	if failure == nil || failure.Code() != "core.query.invalid-argument@1" {
		t.Fatalf("kind validation: %v", failure)
	}
}

// TestQueryStructureOrderMerge pins the span-based merge ordering.
func TestQueryStructureOrderMerge(t *testing.T) {
	doc := mustParse(t, "a: 1\nb: 2\n", Yaml12CoreV1)
	base := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("yaml.documents", 1)).
		Then(protocol.NewOperatorCall("yaml.document-root", 1)).
		Then(protocol.NewOperatorCall("yaml.try-mapping-entries", 1))
	// The merge sorts every branch result back into presentation order.
	merge := &protocol.QueryExpression{Kind: protocol.ExpressionStructureOrderMerge, Branches: []*protocol.QueryExpression{
		base.Then(protocol.NewOperatorCall("core.take", 1).
			WithArgument("count", core.NewInteger(big.NewInt(1)))),
		base,
	}}
	matches := runNativeQuery(t, doc, merge)
	if len(matches) != 3 || matches[0].Ordinal != 0 || matches[1].Ordinal != 0 ||
		matches[2].Ordinal != 1 {
		t.Fatalf("merge order %v", matches)
	}
}
