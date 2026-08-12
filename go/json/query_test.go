package json

import (
	"context"
	"math/big"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// capabilities returns the frozen ordered-results capability set used by
// query binding.
func capabilities() *protocol.CapabilitySet {
	set := protocol.NewCapabilitySet()
	set.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	return set
}

// bindJSONQuery validates and binds one JSON-domain query.
func bindJSONQuery(t *testing.T, domain *protocol.QueryDomain,
	expression *protocol.QueryExpression) *protocol.ExecutableQuery {
	t.Helper()
	definition := protocol.NewQueryDefinition(domain).WithExpression(expression)
	validated, failure := definition.Validate()
	if failure != nil {
		t.Fatalf("validate: %v", failure)
	}
	executable, failure := validated.Bind(capabilities())
	if failure != nil {
		t.Fatalf("bind: %v", failure)
	}
	return executable
}

func TestNativeQueryDuplicateOrder(t *testing.T) {
	doc := parseForTest(t, `{"a":1,"a":2,"b":3}`, JsonProfileStrictV1)
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("json.try-object-members", 1)).
		Then(protocol.NewOperatorCall("json.member-name-equals", 1).
			WithArgument("name", core.String("a")))
	executable := bindJSONQuery(t, protocol.DomainJSONNativeV1(), expression)
	matches, failure := ExecuteJSONQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("execute: %v", failure)
	}
	if len(matches) != 2 {
		t.Fatalf("match count %d", len(matches))
	}
	for index, match := range matches {
		if match.Kind != JsonMatchObjectMember || match.Ordinal != index {
			t.Errorf("match %d %+v", index, match)
		}
	}
	if matches[0].Identity() == matches[1].Identity() {
		t.Error("duplicate members must keep distinct identities")
	}
	if matches[0].Name == nil || *matches[0].Name != "a" {
		t.Errorf("match name %v", matches[0].Name)
	}
}

func TestNativeQueryMemberValueAndArrayElements(t *testing.T) {
	doc := parseForTest(t, `{"list":[1,2,3]}`, JsonProfileStrictV1)
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("json.try-object-members", 1)).
		Then(protocol.NewOperatorCall("json.member-value", 1)).
		Then(protocol.NewOperatorCall("json.try-array-elements", 1)).
		Then(protocol.NewOperatorCall("json.array-element-value", 1)).
		Then(protocol.NewOperatorCall("core.take", 1).WithArgument("count", core.NewInteger(big.NewInt(2))))
	executable := bindJSONQuery(t, protocol.DomainJSONNativeV1(), expression)
	matches, failure := ExecuteJSONQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("execute: %v", failure)
	}
	if len(matches) != 2 {
		t.Fatalf("match count %d", len(matches))
	}
	for index, match := range matches {
		if match.Kind != JsonMatchValue || match.ValueKind == nil ||
			*match.ValueKind != JsonValueKindInteger {
			t.Errorf("match %d %+v", index, match)
		}
	}
}

func TestNativeQueryRootLimit(t *testing.T) {
	doc := parseForTest(t, `{"a":1}`, JsonProfileStrictV1)
	executable := bindJSONQuery(t, protocol.DomainJSONNativeV1(),
		&protocol.QueryExpression{Kind: protocol.ExpressionInput})
	limits := protocol.DefaultQueryLimits()
	limits.MaxResults = 0
	_, failure := ExecuteJSONQuery(context.Background(), executable, doc, limits)
	if failure == nil || failure.Kind != protocol.FailureResourceLimit {
		t.Fatalf("failure %v", failure)
	}
}

func TestNativeQueryCancellation(t *testing.T) {
	doc := parseForTest(t, `{"a":1}`, JsonProfileStrictV1)
	executable := bindJSONQuery(t, protocol.DomainJSONNativeV1(),
		&protocol.QueryExpression{Kind: protocol.ExpressionInput})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, failure := ExecuteJSONQuery(ctx, executable, doc, protocol.DefaultQueryLimits())
	if failure == nil || failure.Kind != protocol.FailureCancelled {
		t.Fatalf("failure %v", failure)
	}
}

func TestSyntaxQueryKindTextOrder(t *testing.T) {
	doc := parseForTest(t, "// note\n{\"a\":1,\"b\":2}", JsonProfileJsoncBoundedV1)
	comments := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("json.syntax-kind-is", 1).
			WithArgument("kind", core.String("LineComment")))
	commas := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("json.syntax-text-equals", 1).
			WithArgument("text", core.String(",")))
	expression := &protocol.QueryExpression{Kind: protocol.ExpressionStructureOrderMerge,
		Branches: []*protocol.QueryExpression{comments, commas}}
	executable := bindJSONQuery(t, protocol.DomainJSONLosslessSyntaxV1(), expression)
	matches, failure := ExecuteJSONSyntaxQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("execute: %v", failure)
	}
	if len(matches) != 2 {
		t.Fatalf("match count %d", len(matches))
	}
	text, _ := doc.source.DecodedText()
	if matches[0].Kind() != JsonSyntaxKindLineComment ||
		text[matches[0].Span().StartByte():matches[0].Span().EndByte()] != "// note" ||
		matches[0].Ordinal() != 0 {
		t.Errorf("match 0 %+v", matches[0])
	}
	if matches[1].Kind() != JsonSyntaxKindComma ||
		matches[1].Ordinal() != 6 {
		t.Errorf("match 1 %+v", matches[1])
	}
	if matches[0].NodeRef().Role() != document.RoleJsonSyntaxPiece {
		t.Errorf("role %v", matches[0].NodeRef().Role())
	}
	if !(matches[0].Ordinal() < matches[1].Ordinal()) {
		t.Error("order")
	}
}

func TestSyntaxQuerySelections(t *testing.T) {
	doc := parseForTest(t, "[1,2,1]", JsonProfileStrictV1)
	filter := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("json.syntax-kind-is", 1).
			WithArgument("kind", core.String("Number")))
	executable := bindJSONQuery(t, protocol.DomainJSONLosslessSyntaxV1(), filter)
	first := protocol.NewQueryDefinition(protocol.DomainJSONLosslessSyntaxV1()).
		WithExpression(filter).WithSelection(protocol.SelectionFirst)
	validated, failure := first.Validate()
	if failure != nil {
		t.Fatal(failure)
	}
	bound, failure := validated.Bind(capabilities())
	if failure != nil {
		t.Fatal(failure)
	}
	matches, failure := ExecuteJSONSyntaxQuery(context.Background(), bound, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatal(failure)
	}
	if len(matches) != 1 || matches[0].Ordinal() != 1 {
		t.Fatalf("first selection %+v", matches)
	}
	_ = executable
	last := protocol.NewQueryDefinition(protocol.DomainJSONLosslessSyntaxV1()).
		WithExpression(filter).WithSelection(protocol.SelectionLast)
	validated, failure = last.Validate()
	if failure != nil {
		t.Fatal(failure)
	}
	bound, failure = validated.Bind(capabilities())
	if failure != nil {
		t.Fatal(failure)
	}
	matches, failure = ExecuteJSONSyntaxQuery(context.Background(), bound, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatal(failure)
	}
	if len(matches) != 1 || matches[0].Ordinal() != 5 {
		t.Fatalf("last selection %+v", matches)
	}
}

func TestSyntaxQueryResultLimit(t *testing.T) {
	doc := parseForTest(t, "[1]", JsonProfileStrictV1)
	executable := bindJSONQuery(t, protocol.DomainJSONLosslessSyntaxV1(),
		&protocol.QueryExpression{Kind: protocol.ExpressionInput})
	limits := protocol.DefaultQueryLimits()
	limits.MaxResults = 0
	_, failure := ExecuteJSONSyntaxQuery(context.Background(), executable, doc, limits)
	if failure == nil || failure.Kind != protocol.FailureResourceLimit {
		t.Fatalf("failure %v", failure)
	}
}

func TestSyntaxQueryCancelled(t *testing.T) {
	doc := parseForTest(t, "[1]", JsonProfileStrictV1)
	executable := bindJSONQuery(t, protocol.DomainJSONLosslessSyntaxV1(),
		&protocol.QueryExpression{Kind: protocol.ExpressionInput})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, failure := ExecuteJSONSyntaxQuery(ctx, executable, doc, protocol.DefaultQueryLimits())
	if failure == nil || failure.Kind != protocol.FailureCancelled {
		t.Fatalf("failure %v", failure)
	}
}

func TestSyntaxQueryInvalidKindRejectedAtValidation(t *testing.T) {
	filter := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("json.syntax-kind-is", 1).
			WithArgument("kind", core.String("number")))
	definition := protocol.NewQueryDefinition(protocol.DomainJSONLosslessSyntaxV1()).
		WithExpression(filter)
	_, failure := definition.Validate()
	if failure == nil || failure.Kind != protocol.FailureInvalidArgument {
		t.Fatalf("failure %v", failure)
	}
	if failure.Code() != "core.query.invalid-argument@1" {
		t.Errorf("code %s", failure.Code())
	}
}

func TestJSON5QueryRequiresV2(t *testing.T) {
	doc := parseForTest(t, "{key:1,true:2}", JsonProfileJson5StandardV1)
	filter := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("json.syntax-kind-is", 1).
			WithArgument("kind", core.String("Identifier")))
	// v1 is rejected on a JSON5 document (the Identifier kind cannot even
	// be validated under v1, so the rejection check uses the bare input).
	executable := bindJSONQuery(t, protocol.DomainJSONLosslessSyntaxV1(),
		&protocol.QueryExpression{Kind: protocol.ExpressionInput})
	_, failure := ExecuteJSONSyntaxQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if failure == nil || failure.Kind != protocol.FailureDomainMismatch {
		t.Fatalf("v1 failure %v", failure)
	}
	// v2 accepts and finds both identifiers.
	executable = bindJSONQuery(t, protocol.DomainJSONLosslessSyntaxV2(), filter)
	matches, failure := ExecuteJSONSyntaxQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("v2 execute: %v", failure)
	}
	text, _ := doc.source.DecodedText()
	if len(matches) != 2 {
		t.Fatalf("match count %d", len(matches))
	}
	for index, match := range matches {
		expected := "key"
		if index == 1 {
			expected = "true"
		}
		if got := text[match.Span().StartByte():match.Span().EndByte()]; got != expected {
			t.Errorf("match %d text %q != %q", index, got, expected)
		}
	}
}

func TestJSON5NativeQueryV2Binary(t *testing.T) {
	doc := parseForTest(t, "-Infinity", JsonProfileJson5StandardV1)
	executable := bindJSONQuery(t, protocol.DomainJSONNativeV2(),
		&protocol.QueryExpression{Kind: protocol.ExpressionInput})
	matches, failure := ExecuteJSONQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("execute: %v", failure)
	}
	if len(matches) != 1 || matches[0].Kind != JsonMatchValue ||
		matches[0].ValueKind == nil || *matches[0].ValueKind != JsonValueKindBinaryFloat64 {
		t.Fatalf("matches %+v", matches)
	}
	executable = bindJSONQuery(t, protocol.DomainJSONNativeV1(),
		&protocol.QueryExpression{Kind: protocol.ExpressionInput})
	_, failure = ExecuteJSONQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if failure == nil || failure.Kind != protocol.FailureDomainMismatch {
		t.Fatalf("v1 failure %v", failure)
	}
}

func TestSyntaxQueryCursor(t *testing.T) {
	doc := parseForTest(t, "[1,2]", JsonProfileStrictV1)
	filter := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("json.syntax-kind-is", 1).
			WithArgument("kind", core.String("Number")))
	executable := bindJSONQuery(t, protocol.DomainJSONLosslessSyntaxV1(), filter)
	cursor, failure := ExecuteJSONSyntaxQueryCursor(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatal(failure)
	}
	count := 0
	for {
		match, failure := cursor.NextMatch()
		if failure != nil {
			t.Fatalf("cursor failure: %v", failure)
		}
		if match.NodeRef() == (document.NodeRef{}) {
			break
		}
		count++
	}
	if count != 2 {
		t.Fatalf("cursor count %d", count)
	}
}

func TestSyntaxQueryDistinctByIdentity(t *testing.T) {
	doc := parseForTest(t, "[1,2,1]", JsonProfileStrictV1)
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("json.syntax-text-equals", 1).
			WithArgument("text", core.String("1"))).
		Then(protocol.NewOperatorCall("core.distinct-by-identity", 1))
	executable := bindJSONQuery(t, protocol.DomainJSONLosslessSyntaxV1(), expression)
	matches, failure := ExecuteJSONSyntaxQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatal(failure)
	}
	if len(matches) != 2 {
		t.Fatalf("distinct count %d", len(matches))
	}
}
