package hcl_test

// Runnable SDK example for the HCL family: parse → native query →
// body projection → materialization → structural edit (plan §2.5
// G4.4). Run with `go test ./hcl/`; also visible in
// `go doc consema.dev/consema/hcl`.

import (
	"context"
	"fmt"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	hclpkg "consema.dev/consema/hcl"
	"consema.dev/consema/protocol"
)

// Example walks one HCL configuration through the full SDK chain.
func Example() {
	// Parse under the exact `hcl.native@1` profile.
	source := []byte("name = \"api\"\nserver {\n  port = 8080\n}\n")
	doc, formationFailure := hclpkg.Parse(context.Background(), source,
		hclpkg.HclProfileNativeV1, hclpkg.HclEncodingSelectionProfileDefault(),
		hclpkg.DefaultHclParseLimits())
	if formationFailure != nil {
		panic(formationFailure)
	}

	// Query the native model: the "name" attribute's unevaluated
	// expression text.
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("hcl.document-body", 1)).
		Then(protocol.NewOperatorCall("hcl.body-attributes", 1)).
		Then(protocol.NewOperatorCall("hcl.attribute-name-equals", 1).
			WithArgument("name", core.String("name")))
	definition := protocol.NewQueryDefinition(protocol.DomainHCLNativeV1()).
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
	matches, queryFailure := hclpkg.ExecuteHCLNativeQuery(context.Background(), executable,
		doc, protocol.DefaultQueryLimits())
	if queryFailure != nil {
		panic(queryFailure)
	}
	for _, match := range matches {
		if match.Kind == hclpkg.HclMatchAttribute {
			decoded, _ := doc.Source().DecodedText()
			fmt.Printf("attribute: %s = %s\n", match.Attribute.Name(),
				match.Attribute.Expression().Text(decoded))
		}
	}

	// Project the document to the exact body record.
	projected := doc.Project(hclpkg.ProjectionRequestBody())
	if projected.Failed != nil {
		panic(projected.Failed.Diagnostics)
	}

	// Materialize the record as the canonical HCL document.
	materialized := hclpkg.Materialize(projected.Complete.Value,
		document.NewMaterializationRequest(
			document.NewProfileId("hcl.native", 1),
			document.NewMaterializationStyleId("hcl.canonical-document", 1)))
	if materialized.Failed != nil {
		panic(materialized.Failed.Failure)
	}
	fmt.Printf("materialized: %s", materialized.Complete.Document.Render())

	// Edit the parsed document: set the "name" attribute value.
	builder := hclpkg.NewEditTransactionBuilder(doc)
	builder.SetAttributeValue(hclpkg.BodyPathRoot(), "name",
		hclpkg.EditValueStringV("gateway"))
	commit, editFailure := doc.Commit(builder.Build())
	if editFailure != nil {
		panic(editFailure)
	}
	fmt.Printf("edited: %s", commit.Document.Render())

	// Output:
	// attribute: name = "api"
	// materialized: name = "api"
	// server {
	//   port = 8080
	// }
	// edited: name = "gateway"
	// server {
	//   port = 8080
	// }
	//
}
