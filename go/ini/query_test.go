package ini

import (
	"context"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// nativeExecutable binds one validated INI native query expression.
func nativeExecutable(t *testing.T, expression *protocol.QueryExpression) *protocol.ExecutableQuery {
	t.Helper()
	definition := protocol.NewQueryDefinition(protocol.DomainININativeV1()).
		WithExpression(expression)
	validated, failure := definition.Validate()
	if failure != nil {
		t.Fatalf("validation failed: %s", failure.Code())
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	bound, failure := validated.Bind(capabilities)
	if failure != nil {
		t.Fatalf("binding failed: %s", failure.Code())
	}
	return bound
}

func TestNativeQueryKeepsProfileEquivalenceDuplicatesAndOwnership(t *testing.T) {
	doc := parseText(t, WindowsV1, "[Main]\r\nName=one\r\nname=two\r\n[Other]\r\nempty=\r\n")
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("ini.document-sections", 1)).
		Then(protocol.NewOperatorCall("ini.section-name-equals", 1).
			WithArgument("name", core.String("MAIN")).
			WithArgument("comparison", core.String("ProfileEquivalent"))).
		Then(protocol.NewOperatorCall("ini.section-entries", 1))
	matches, failure := ExecuteIniQuery(context.Background(),
		nativeExecutable(t, expression), doc, protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("query failed: %s", failure.Code())
	}
	if len(matches) != 2 {
		t.Fatalf("match count %d, want 2", len(matches))
	}
	for _, match := range matches {
		if match.Kind != IniMatchEntry || match.DuplicateGroup == nil {
			t.Fatalf("entry or duplicate facts differed")
		}
	}

	// NOTE: ini.duplicate-group is reachable through the shared protocol
	// operator table (its input-dependent RoleAny row is typed by the
	// input role; protocol/query_validate.go) and its execution is covered
	// by the external regression in protocol/duplicate_group_exec_test.go
	// (G2.4). The direct duplicate facts asserted above complement that
	// coverage.
	exactExpression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("ini.all-entries", 1)).
		Then(protocol.NewOperatorCall("ini.entry-key-equals", 1).
			WithArgument("key", core.String("Name")).
			WithArgument("comparison", core.String("OriginalExact")))
	matches, failure = ExecuteIniQuery(context.Background(),
		nativeExecutable(t, exactExpression), doc, protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("original-exact query failed: %s", failure.Code())
	}
	if len(matches) != 1 || matches[0].Key != "Name" {
		t.Fatalf("original-exact selection differed")
	}

	emptyExpression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("ini.all-entries", 1)).
		Then(protocol.NewOperatorCall("ini.entry-value-state-is", 1).
			WithArgument("state", core.String("Empty"))).
		Then(protocol.NewOperatorCall("ini.entry-section", 1))
	matches, failure = ExecuteIniQuery(context.Background(),
		nativeExecutable(t, emptyExpression), doc, protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("entry-section query failed: %s", failure.Code())
	}
	if len(matches) != 1 || matches[0].Kind != IniMatchSection || matches[0].Name != "Other" {
		t.Fatalf("entry-section facts differed")
	}
}

func TestSyntaxQueryMatchesDecodedTextForUtf16AndKeepsOrder(t *testing.T) {
	doc, failure := Parse(utf16leBOM("[S]\r\nName=\" value \"\r\n"), WindowsV1,
		IniEncodingProfileDefault(), DefaultIniParseLimits())
	if failure != nil {
		t.Fatalf("parse failed: %s", failure.Diagnostics()[0].Code)
	}
	quote := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("ini.syntax-kind-is", 1).
			WithArgument("kind", core.String("Quote")))
	name := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("ini.syntax-text-equals", 1).
			WithArgument("text", core.String("Name")))
	expression := &protocol.QueryExpression{Kind: protocol.ExpressionStructureOrderMerge,
		Branches: []*protocol.QueryExpression{quote, name}}
	definition := protocol.NewQueryDefinition(protocol.DomainINILosslessSyntaxV1()).
		WithExpression(expression)
	validated, validationFailure := definition.Validate()
	if validationFailure != nil {
		t.Fatalf("validation failed: %s", validationFailure.Code())
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, bindingFailure := validated.Bind(capabilities)
	if bindingFailure != nil {
		t.Fatalf("binding failed: %s", bindingFailure.Code())
	}
	matches, queryFailure := ExecuteIniSyntaxQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if queryFailure != nil {
		t.Fatalf("syntax query failed: %s", queryFailure.Code())
	}
	if len(matches) != 3 {
		t.Fatalf("syntax match count %d, want 3", len(matches))
	}
	if matches[0].Kind() != SyntaxKindEntryKey ||
		matches[0].NodeRef().Role() != document.RoleIniSyntaxPiece {
		t.Fatalf("first syntax match facts differed")
	}
	if matches[1].Kind() != SyntaxKindQuote || matches[2].Kind() != SyntaxKindQuote {
		t.Fatalf("quote match facts differed")
	}
	if matches[0].Ordinal() >= matches[1].Ordinal() {
		t.Fatalf("syntax ordinals are not strictly increasing")
	}
}

func TestQueryValidationLimitsAndCursorCancellation(t *testing.T) {
	invalid := protocol.NewQueryDefinition(protocol.DomainININativeV1()).
		WithExpression((&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
			Then(protocol.NewOperatorCall("ini.section-name-equals", 1).
				WithArgument("name", core.String("S")).
				WithArgument("comparison", core.String("Implicit"))))
	if _, failure := invalid.Validate(); failure == nil ||
		failure.Kind != protocol.FailureInvalidOperatorComposition {
		t.Fatalf("invalid composition was not rejected")
	}
	invalidComparison := protocol.NewQueryDefinition(protocol.DomainININativeV1()).
		WithExpression((&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
			Then(protocol.NewOperatorCall("ini.document-sections", 1)).
			Then(protocol.NewOperatorCall("ini.section-name-equals", 1).
				WithArgument("name", core.String("S")).
				WithArgument("comparison", core.String("Implicit"))))
	if _, failure := invalidComparison.Validate(); failure == nil ||
		failure.Kind != protocol.FailureInvalidArgument || failure.Argument != "comparison" {
		t.Fatalf("invalid comparison argument was not rejected")
	}

	doc := parseText(t, PortableV1, "[s]\na=1\nb=2\n")
	all := nativeExecutable(t, (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("ini.all-entries", 1)))
	limits := protocol.DefaultQueryLimits()
	limits.MaxSteps = 100
	limits.MaxResults = 1
	_, failure := ExecuteIniQuery(context.Background(), all, doc, limits)
	if failure == nil || failure.Code() != "core.query.resource-limit@1" {
		t.Fatalf("query limit failure differed: %v", failure)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cursor, cursorFailure := ExecuteIniQueryCursor(ctx, all, doc, protocol.DefaultQueryLimits())
	if cursorFailure != nil {
		t.Fatalf("cursor creation failed: %s", cursorFailure.Code())
	}
	if _, ok := cursor.Next(); !ok {
		t.Fatalf("first yield missing")
	}
	cancel()
	if _, ok := cursor.Next(); ok {
		t.Fatalf("cancelled cursor yielded")
	}
	if cursor.TerminalState() != "Cancelled" {
		t.Fatalf("terminal state %q, want Cancelled", cursor.TerminalState())
	}
}

func TestNativeQueryPhysicalAndLogicalLines(t *testing.T) {
	doc := parseText(t, PythonConfigParserV1, "[s]\nkey: first\n    second\n")
	physical := nativeExecutable(t, (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("ini.physical-lines", 1)))
	matches, failure := ExecuteIniQuery(context.Background(), physical, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("physical-lines query failed: %s", failure.Code())
	}
	if len(matches) != 3 || matches[0].Kind != IniMatchPhysicalLine ||
		matches[0].Span.StartByte() != 0 {
		t.Fatalf("physical-line facts differed")
	}
	logical := nativeExecutable(t, (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("ini.logical-lines", 1)))
	matches, failure = ExecuteIniQuery(context.Background(), logical, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("logical-lines query failed: %s", failure.Code())
	}
	if len(matches) != 2 || matches[1].LogicalKind != LogicalLineEntry {
		t.Fatalf("logical-line facts differed")
	}
}
