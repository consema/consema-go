package yaml_test

// Runnable SDK example for the YAML family: parse → native query →
// value projection → materialization → structural edit (plan §2.5
// G4.4). Run with `go test ./yaml/`; also visible in
// `go doc consema.dev/consema/yaml`.

import (
	"context"
	"fmt"
	"math/big"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
	yamlpkg "consema.dev/consema/yaml"
)

// Example walks one YAML stream through the full SDK chain.
func Example() {
	// Parse under the explicit YAML 1.2 Core profile.
	source := []byte("service:\n  host: localhost\n  port: 8080\n  enabled: true\n")
	doc, formationFailure := yamlpkg.Parse(source, yamlpkg.Yaml12CoreV1,
		document.DefaultParseLimits())
	if formationFailure != nil {
		panic(formationFailure)
	}

	// Query the native model: the scalar values of the nested "service"
	// mapping.
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("yaml.documents", 1)).
		Then(protocol.NewOperatorCall("yaml.document-root", 1)).
		Then(protocol.NewOperatorCall("yaml.try-mapping-entries", 1)).
		Then(protocol.NewOperatorCall("yaml.mapping-entry-value", 1)).
		Then(protocol.NewOperatorCall("yaml.try-mapping-entries", 1)).
		Then(protocol.NewOperatorCall("yaml.mapping-entry-value", 1)).
		Then(protocol.NewOperatorCall("yaml.where-node-kind", 1).
			WithArgument("kind", core.String("Scalar")))
	definition := protocol.NewQueryDefinition(protocol.DomainYAMLNativeV1()).
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
	matches, queryFailure := yamlpkg.ExecuteYamlQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if queryFailure != nil {
		panic(queryFailure)
	}
	fmt.Printf("scalar values matched: %d\n", len(matches))

	// Project the stream to the best-exact portable value.
	projected := doc.ProjectValue(yamlpkg.BestExactValueV1())
	if projected.Failed != nil {
		panic(projected.Failed.Code())
	}

	// Materialize the projected value as canonical flow YAML.
	materialized := yamlpkg.MaterializeValue(projected.Complete.Value,
		document.NewMaterializationRequest(
			document.NewProfileId("yaml.1.2-core", 1),
			document.NewMaterializationStyleId("yaml.canonical-flow", 1)).
			WithNewline(document.NewlineLf).
			WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject))
	if materialized.Failed != nil {
		panic(fmt.Sprintf("%s: %s", materialized.Failed.Failure.Code(),
			materialized.Failed.Failure.Error()))
	}
	fmt.Printf("materialized: %s", materialized.Complete.Document.Render())

	// Edit the parsed document: replace the "port" scalar value.
	service, serviceFound := doc.Document(0)
	if !serviceFound {
		panic("document missing")
	}
	serviceEntry, entryFound := service.Root().MappingEntry(0)
	if !entryFound {
		panic("service entry missing")
	}
	serviceLen, serviceIsMapping := serviceEntry.Value().MappingLen()
	if !serviceIsMapping || serviceLen != 3 {
		panic("service value mapping missing")
	}
	portEntry, portFound := serviceEntry.Value().MappingEntry(1)
	if !portFound {
		panic("port entry missing")
	}
	builder := yamlpkg.NewEditTransactionBuilder(doc)
	builder.SemanticScalar(portEntry.Value().NodeRef(),
		core.NewInteger(big.NewInt(9090)), yamlpkg.RepresentationPolicyPreserveCompatible)
	commit, editFailure := doc.Commit(builder.Build())
	if editFailure != nil {
		panic(editFailure)
	}
	fmt.Printf("edited: %s", commit.Document.Render())

	// Output:
	// scalar values matched: 3
	// materialized: --- !!map {? !!str "service" : !!map {? !!str "host" : !!str "localhost", ? !!str "port" : !!int "8080", ? !!str "enabled" : !!bool "true"}}
	// edited: service:
	//   host: localhost
	//   port: 9090
	//   enabled: true
	//
}
