package toml_test

// Runnable SDK example for the TOML family: parse → native query →
// projection → materialization → structural edit (plan §2.5 G4.4).
// Run with `go test ./toml/`; also visible in
// `go doc consema.dev/consema/toml`.

import (
	"context"
	"fmt"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
	tomlpkg "consema.dev/consema/toml"
)

// Example walks one TOML configuration through the full SDK chain.
func Example() {
	// Parse under the exact TOML 1.0 profile.
	source := []byte("title = \"service\"\n[server]\nhost = 'localhost'\nport = 8080\n")
	doc, formationFailure := tomlpkg.Parse(source, tomlpkg.Toml10V1,
		document.DefaultParseLimits())
	if formationFailure != nil {
		panic(formationFailure)
	}

	// Query the native model: the entries of the "server" table.
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("toml.try-table-entries", 1)).
		Then(protocol.NewOperatorCall("toml.entry-name-equals", 1).
			WithArgument("name", core.String("server"))).
		Then(protocol.NewOperatorCall("toml.entry-item", 1)).
		Then(protocol.NewOperatorCall("toml.try-table-entries", 1))
	definition := protocol.NewQueryDefinition(protocol.DomainTOMLNativeV1()).
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
	matches, queryFailure := tomlpkg.ExecuteTomlQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if queryFailure != nil {
		panic(queryFailure)
	}
	for _, match := range matches {
		fmt.Printf("entry: %s\n", match.Name)
	}

	// Project the document to the best-exact portable value.
	projected := doc.Project(tomlpkg.NewProjectionRequest(
		tomlpkg.ProjectionTargetBestExactCoreV1))
	if projected.Failed != nil {
		panic(projected.Failed.Diagnostics)
	}

	// Materialize the projected value as the canonical TOML document.
	materialized := tomlpkg.Materialize(projected.Complete.Value,
		document.NewMaterializationRequest(
			document.NewProfileId("toml.1.0", 1),
			document.NewMaterializationStyleId("toml.canonical-document", 1)).
			WithNewline(document.NewlineLf).
			WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject))
	if materialized.Failed != nil {
		panic(materialized.Failed.Failure)
	}
	fmt.Printf("materialized: %s", materialized.Complete.Document.Render())

	// Edit the parsed document: rename the "title" entry.
	var titleEntry tomlpkg.TomlEntry
	titleFound := false
	rootEntries, rootIsTable := doc.Root().TableEntries()
	if rootIsTable {
		for _, entry := range rootEntries {
			if entry.Name() == "title" {
				titleEntry = entry
				titleFound = true
			}
		}
	}
	if !titleFound {
		panic("title entry missing")
	}
	builder := tomlpkg.NewEditTransactionBuilder(doc)
	builder.RenameEntry(titleEntry.NodeRef(), "name")
	commit, editFailure := doc.Commit(builder.Build())
	if editFailure != nil {
		panic(editFailure)
	}
	fmt.Printf("edited: %s", commit.Document.Render())

	// Output:
	// entry: host
	// entry: port
	// materialized: "title" = "service"
	// "server" = { "host" = "localhost", "port" = 8080 }
	// edited: "name" = "service"
	// [server]
	// host = 'localhost'
	// port = 8080
	//
}
