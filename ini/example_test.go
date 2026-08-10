package ini_test

// Runnable SDK example for the INI family: parse → native query →
// projection → materialization → structural edit (plan §2.5 G4.4).
// Run with `go test ./ini/`; also visible in
// `go doc consema.dev/consema/ini`.

import (
	"context"
	"fmt"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	inipkg "consema.dev/consema/ini"
	"consema.dev/consema/protocol"
)

// Example walks one INI configuration through the full SDK chain.
func Example() {
	// Parse under the explicit portable profile.
	source := []byte("[server]\nhost=localhost\nport=8080\n")
	doc, formationFailure := inipkg.Parse(source, inipkg.PortableV1,
		inipkg.IniEncodingProfileDefault(), inipkg.DefaultIniParseLimits())
	if formationFailure != nil {
		panic(formationFailure)
	}

	// Query the native model: the entries of the "server" section.
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("ini.document-sections", 1)).
		Then(protocol.NewOperatorCall("ini.section-name-equals", 1).
			WithArgument("name", core.String("server")).
			WithArgument("comparison", core.String("OriginalExact"))).
		Then(protocol.NewOperatorCall("ini.section-entries", 1))
	definition := protocol.NewQueryDefinition(protocol.DomainININativeV1()).
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
	matches, queryFailure := inipkg.ExecuteIniQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if queryFailure != nil {
		panic(queryFailure)
	}
	for _, match := range matches {
		if match.Kind == inipkg.IniMatchEntry {
			fmt.Printf("entry: %s\n", match.Key)
		}
	}

	// Project the document to the best-exact portable value.
	projected := doc.Project(inipkg.BestExactEntryMappingV1())
	if projected.Failed != nil {
		panic(fmt.Sprintf("projection failed: %s", projected.Failed.Diagnostics[0].Code))
	}

	// Materialize the projected value as the canonical portable INI
	// document.
	materialized := inipkg.Materialize(projected.Complete.Value,
		document.NewMaterializationRequest(
			document.NewProfileId("ini.portable", 1),
			document.NewMaterializationStyleId("ini.portable-canonical", 1)))
	if materialized.Failed != nil {
		panic(materialized.Failed.Failure)
	}
	fmt.Printf("materialized: %s", materialized.Complete.Document.Render())

	// Edit the parsed document: replace the "port" value.
	serverEntries := doc.Entries()
	builder := inipkg.NewEditTransactionBuilder(doc)
	builder.SemanticValue(serverEntries[1].NodeRef(), "9090",
		inipkg.RepresentationPolicyCanonicalForProfile)
	commit, editFailure := doc.Commit(builder.Build())
	if editFailure != nil {
		panic(editFailure)
	}
	fmt.Printf("edited: %s", commit.Document.Render())

	// Output:
	// entry: host
	// entry: port
	// materialized: [server]
	// host=localhost
	// port=8080
	// edited: [server]
	// host=localhost
	// port=9090
	//
}
