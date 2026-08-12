package properties_test

// Runnable SDK example for the Java Properties family: parse → native
// query → projection → materialization → structural edit (plan §2.5
// G4.4). Run with `go test ./properties/`; also visible in
// `go doc consema.dev/consema/properties`.

import (
	"context"
	"fmt"

	"math/big"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/properties"
	"consema.dev/consema/protocol"
)

// Example walks one properties configuration through the full SDK chain.
func Example() {
	// Parse under the explicit Reader profile with UTF-8.
	source := []byte("service.name = api\nservice.port = 8080\nservice.enabled = true\n")
	doc, formationFailure := properties.ParseReader(source, document.Utf8Encoding(),
		properties.DefaultPropertiesParseLimits())
	if formationFailure != nil {
		panic(formationFailure)
	}

	// Query the native model: the "service.port" property. Keys match on
	// their exact UTF-16BE/1 byte form (RFC 0010).
	portKey := utf16BEBytes("service.port")
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("properties.document-properties", 1)).
		Then(protocol.NewOperatorCall("properties.property-key-equals", 1).
			WithArgument("key", core.NewBytes(portKey))).
		Then(protocol.NewOperatorCall("core.take", 1).
			WithArgument("count", core.NewInteger(big.NewInt(1))))
	definition := protocol.NewQueryDefinition(protocol.DomainJavaPropertiesNativeV1()).
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
	matches, queryFailure := properties.ExecutePropertiesQuery(context.Background(),
		executable, doc, protocol.DefaultQueryLimits())
	if queryFailure != nil {
		panic(queryFailure)
	}
	fmt.Printf("properties matched: %d\n", len(matches))

	// Project the document to the best-exact portable value.
	projected := doc.Project(properties.BestExactEntryMapping())
	if projected.Failed != nil {
		panic(projected.Failed.Diagnostics)
	}

	// Materialize the projected value as the canonical Reader document.
	materialized := properties.Materialize(projected.Complete.Value,
		document.NewMaterializationRequest(
			document.NewProfileId("java-properties.reader", 1),
			document.NewMaterializationStyleId("java-properties.reader-canonical", 1)).
			WithNewline(document.NewlineLf))
	if materialized.Failed != nil {
		panic(materialized.Failed.Failure)
	}
	fmt.Printf("materialized: %s", materialized.Complete.Document.Render())

	// Edit the parsed document: replace the "service.port" value.
	var portProperty properties.Property
	portFound := false
	for _, candidate := range doc.Properties() {
		key, keyError := candidate.Key().ToUnicode()
		if keyError != nil {
			panic(keyError)
		}
		if key == "service.port" {
			portProperty = candidate
			portFound = true
		}
	}
	if !portFound {
		panic("service.port property missing")
	}
	builder := properties.NewEditTransactionBuilder(doc)
	builder.SemanticValue(portProperty.NodeRef(),
		properties.NewJavaStringFromUnicode("9090"))
	commit, editFailure := doc.Commit(builder.Build())
	if editFailure != nil {
		panic(editFailure)
	}
	fmt.Printf("edited: %s", commit.Document.Render())

	// Output:
	// properties matched: 1
	// materialized: service.name=api
	// service.port=8080
	// service.enabled=true
	// edited: service.name = api
	// service.port = 9090
	// service.enabled = true
	//
}

// utf16BEBytes encodes one ASCII key as its exact UTF-16BE/1 byte form
// (RFC 0010 §7).
func utf16BEBytes(text string) []byte {
	bytes := make([]byte, 0, len(text)*2)
	for _, rune := range text {
		unit := uint16(rune)
		bytes = append(bytes, byte(unit>>8), byte(unit))
	}
	return bytes
}
