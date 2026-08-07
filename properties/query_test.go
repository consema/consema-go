package properties

import (
	"context"
	"math/big"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

func queryReader(t *testing.T, source string) *Document {
	t.Helper()
	doc, failure := ParseReader([]byte(source), document.Utf8Encoding(),
		DefaultPropertiesParseLimits())
	if failure != nil {
		t.Fatalf("formation failed: %s", failure.Code())
	}
	return doc
}

func nativeExecutable(t *testing.T, expression *protocol.QueryExpression) *protocol.ExecutableQuery {
	t.Helper()
	definition := protocol.NewQueryDefinition(protocol.DomainJavaPropertiesNativeV1()).
		WithExpression(expression)
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

func TestNativeQueryPreservesExactKeysDuplicatesAndEscapeOwnership(t *testing.T) {
	doc := queryReader(t, "a\\ key=one\\u0021\na\\ key=two\nempty\n")
	matches := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.document-properties", 1)).
		Then(protocol.NewOperatorCall("properties.property-key-equals", 1).
			WithArgument("key", core.NewBytes([]byte{0, 'a', 0, ' ', 0, 'k', 0, 'e', 0, 'y'}))).
		Then(protocol.NewOperatorCall("core.take", 1).
			WithArgument("count", core.NewInteger(big.NewInt(1)))).
		Then(protocol.NewOperatorCall("properties.duplicate-group", 1))
	result, failure := ExecutePropertiesQuery(context.Background(),
		nativeExecutable(t, matches), doc, protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("query: %s", failure.Code())
	}
	if len(result) != 2 {
		t.Fatalf("matches %d", len(result))
	}
	for _, match := range result {
		if match.Kind != PropertiesMatchProperty || match.DuplicateGroup == nil {
			t.Fatalf("match %+v", match)
		}
	}

	escapes := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.document-properties", 1)).
		Then(protocol.NewOperatorCall("core.take", 1).
			WithArgument("count", core.NewInteger(big.NewInt(1)))).
		Then(protocol.NewOperatorCall("properties.property-escapes", 1))
	escapeResult, failure := ExecutePropertiesQuery(context.Background(),
		nativeExecutable(t, escapes), doc, protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("query: %s", failure.Code())
	}
	if len(escapeResult) != 2 {
		t.Fatalf("escape matches %d", len(escapeResult))
	}
	for _, match := range escapeResult {
		if match.Kind != PropertiesMatchEscape {
			t.Fatalf("escape match kind %s", match.Kind)
		}
	}
}

func TestLogicalQueryReturnsExactNaturalLineConstituents(t *testing.T) {
	doc := queryReader(t, "k=one\\\r\n two\n")
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.logical-lines", 1)).
		Then(protocol.NewOperatorCall("properties.logical-line-natural-lines", 1))
	result, failure := ExecutePropertiesQuery(context.Background(),
		nativeExecutable(t, expression), doc, protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("query: %s", failure.Code())
	}
	if len(result) != 2 ||
		result[0].Kind != PropertiesMatchNaturalLine || result[0].Ordinal != 0 ||
		result[1].Kind != PropertiesMatchNaturalLine || result[1].Ordinal != 1 {
		t.Fatalf("matches %+v", result)
	}
}

func TestSyntaxQuerySupportsTextRawBytesAndUtf16be(t *testing.T) {
	doc := queryReader(t, "π=值\n")
	text := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.syntax-text-equals", 1).
			WithArgument("text", core.String("值")))
	raw := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.syntax-raw-bytes-equals", 1).
			WithArgument("bytes", core.NewBytes([]byte("π"))))
	utf16 := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.syntax-utf16be-equals", 1).
			WithArgument("code_units", core.NewBytes([]byte{0x50, 0x3C})))
	merge := &protocol.QueryExpression{Kind: protocol.ExpressionStructureOrderMerge,
		Branches: []*protocol.QueryExpression{text, raw, utf16}}
	definition := protocol.NewQueryDefinition(protocol.DomainJavaPropertiesLosslessSyntaxV1()).
		WithExpression(merge)
	validated, failure := definition.Validate()
	if failure != nil {
		t.Fatalf("validation: %s", failure.Code())
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, failure := validated.Bind(capabilities)
	if failure != nil {
		t.Fatalf("binding: %s", failure.Code())
	}
	result, failure := ExecutePropertiesSyntaxQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("query: %s", failure.Code())
	}
	if len(result) != 3 {
		t.Fatalf("matches %d", len(result))
	}
	if result[0].kind != SyntaxKindKey || result[1].kind != SyntaxKindValue ||
		result[2].kind != SyntaxKindValue {
		t.Fatalf("kinds %v %v %v", result[0].kind, result[1].kind, result[2].kind)
	}
	if result[0].node.Role() != document.RolePropertiesSyntaxPiece {
		t.Fatalf("role %s", result[0].node.Role())
	}
}

func TestValidationLimitsAndCursorCancellationAreExplicit(t *testing.T) {
	invalid := protocol.NewQueryDefinition(protocol.DomainJavaPropertiesNativeV1()).
		WithExpression((&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
			Then(protocol.NewOperatorCall("properties.document-properties", 1)).
			Then(protocol.NewOperatorCall("properties.property-key-equals", 1).
				WithArgument("key", core.NewBytes([]byte{0}))))
	_, failure := invalid.Validate()
	if failure == nil || failure.Argument != "key" {
		t.Fatalf("validation failure %+v", failure)
	}

	doc := queryReader(t, "a=1\nb=2\n")
	all := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.document-properties", 1))
	executable := nativeExecutable(t, all)
	limits := protocol.DefaultQueryLimits()
	limits.MaxSteps = 100
	limits.MaxResults = 1
	_, failure = ExecutePropertiesQuery(context.Background(), executable, doc, limits)
	if failure == nil || failure.Code() != "core.query.resource-limit@1" {
		t.Fatalf("limit failure %+v", failure)
	}
	cancelContext, cancel := context.WithCancel(context.Background())
	cursor, cursorFailure := ExecutePropertiesQueryCursor(cancelContext, executable, doc,
		protocol.DefaultQueryLimits())
	if cursorFailure != nil {
		t.Fatalf("cursor: %s", cursorFailure.Code())
	}
	if cursor.Next() == nil {
		t.Fatalf("first yield missing")
	}
	cancel()
	if cursor.Next() != nil {
		t.Fatalf("cancelled cursor yielded")
	}
	if state := cursor.TerminalState(); state == nil || *state != QueryTerminalCancelled {
		t.Fatalf("terminal %v", cursor.TerminalState())
	}
}

func TestSyntaxValueStateAndTakeOperators(t *testing.T) {
	doc := queryReader(t, "a=1\nempty\nexplicit=\n")
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.document-properties", 1)).
		Then(protocol.NewOperatorCall("properties.property-value-state-is", 1).
			WithArgument("state", core.String("ImplicitEmpty")))
	result, failure := ExecutePropertiesQuery(context.Background(),
		nativeExecutable(t, expression), doc, protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("query: %s", failure.Code())
	}
	if len(result) != 1 || result[0].Ordinal != 1 {
		t.Fatalf("matches %+v", result)
	}
	distinct := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.document-properties", 1)).
		Then(protocol.NewOperatorCall("properties.property-key-equals", 1).
			WithArgument("key", core.NewBytes([]byte{0, 'a'}))).
		Then(protocol.NewOperatorCall("core.distinct-by-identity", 1))
	result, failure = ExecutePropertiesQuery(context.Background(),
		nativeExecutable(t, distinct), doc, protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("query: %s", failure.Code())
	}
	if len(result) != 1 {
		t.Fatalf("distinct matches %d", len(result))
	}
}
