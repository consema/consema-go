package json_test

// Runnable SDK example for the JSON family: parse → native query →
// projection → materialization → structural edit (plan §2.5 G4.4; the
// example mirrors the chain the conformance vectors exercise, without
// any test-only shortcuts). Run with `go test ./json/`; the example is
// also visible in `go doc consema.dev/consema/json`.

import (
	"context"
	"fmt"
	"math/big"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/protocol"
)

// Example walks one JSON configuration through the full SDK chain.
func Example() {
	// Parse under the strict profile with the frozen default limits.
	source := []byte(`{"service":{"port":8080,"enabled":true},"tags":["a","b"]}`)
	doc, formationFailure := jsonpkg.Parse(context.Background(), source,
		jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
	if formationFailure != nil {
		panic(formationFailure)
	}

	// Query the native model: the value of the nested "port" member.
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("json.try-object-members", 1)).
		Then(protocol.NewOperatorCall("json.member-name-equals", 1).
			WithArgument("name", core.String("service"))).
		Then(protocol.NewOperatorCall("json.member-value", 1)).
		Then(protocol.NewOperatorCall("json.try-object-members", 1)).
		Then(protocol.NewOperatorCall("json.member-name-equals", 1).
			WithArgument("name", core.String("port"))).
		Then(protocol.NewOperatorCall("json.member-value", 1))
	definition := protocol.NewQueryDefinition(protocol.DomainJSONNativeV1()).
		WithExpression(expression)
	validated, validationFailure := definition.Validate()
	if validationFailure != nil {
		panic(validationFailure)
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, bindingFailure := validated.Bind(capabilities)
	if bindingFailure != nil {
		panic(bindingFailure)
	}
	matches, queryFailure := jsonpkg.ExecuteJSONQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if queryFailure != nil {
		panic(queryFailure)
	}
	for _, match := range matches {
		if match.Kind == jsonpkg.JsonMatchValue {
			fmt.Printf("port = %s\n", match.ValueKind)
		}
	}

	// Project the whole document to the best-exact portable value.
	request, buildFailure := jsonpkg.NewProjectionRequestBuilder(
		jsonpkg.ProjectionTargetBestExactCoreV1).Build()
	if buildFailure != nil {
		panic(buildFailure)
	}
	projected := doc.Project(request)
	if projected.Failed != nil {
		panic(projected.Failed.Diagnostics)
	}

	// Materialize the projected value as canonical compact JSON.
	materialized := jsonpkg.Materialize(projected.Complete.Value,
		document.NewMaterializationRequest(
			document.NewProfileId("json.strict", 1),
			document.NewMaterializationStyleId("json.canonical-compact", 1)).
			WithNewline(document.NewlineNone))
	if materialized.Failed != nil {
		panic(materialized.Failed.Failure)
	}
	fmt.Printf("materialized: %s\n", materialized.Complete.Document.Render())

	// Edit the parsed document: insert a top-level "version" member.
	builder := jsonpkg.NewEditTransactionBuilder(doc)
	builder.InsertMember(doc.Root().NodeRef(), "version",
		core.NewInteger(big.NewInt(1)), jsonpkg.PlacementAtEnd())
	commit, editFailure := doc.Commit(builder.Build())
	if editFailure != nil {
		panic(editFailure)
	}
	fmt.Printf("edited: %s\n", commit.Document.Render())

	// Output:
	// port = Integer
	// materialized: {"service":{"port":8080,"enabled":true},"tags":["a","b"]}
	// edited: {"service":{"port":8080,"enabled":true},"tags":["a","b"],"version":1}
}
