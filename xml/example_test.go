package xml_test

// Runnable SDK example for the XML family: parse → native query →
// element-tree projection → materialization → structural edit (plan
// §2.5 G4.4). Run with `go test ./xml/`; also visible in
// `go doc consema.dev/consema/xml`.

import (
	"context"
	"fmt"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
	xmlpkg "consema.dev/consema/xml"
)

// Example walks one XML configuration through the full SDK chain.
func Example() {
	// Parse under the XML 1.0 safe profile with the profile-default
	// encoding.
	source := []byte(`<service name="api" port="8080"><enabled>true</enabled></service>`)
	doc, formationFailure := xmlpkg.Parse(context.Background(), source,
		xmlpkg.XmlProfileSafeV1, xmlpkg.XmlEncodingProfileDefault(),
		xmlpkg.DefaultXmlParseLimits())
	if formationFailure != nil {
		panic(formationFailure)
	}

	// Query the native model: the attributes of the root element.
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("xml.document-root", 1)).
		Then(protocol.NewOperatorCall("xml.element-attributes", 1))
	definition := protocol.NewQueryDefinition(protocol.DomainXMLNativeV1()).
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
	matches, queryFailure := xmlpkg.ExecuteXMLQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if queryFailure != nil {
		panic(queryFailure)
	}
	for _, match := range matches {
		if match.Kind == xmlpkg.XmlMatchAttribute {
			fmt.Printf("attribute: %s=%s\n", match.Local, match.Value)
		}
	}

	// Project the document to the exact element-tree record.
	projected := doc.Project(xmlpkg.ElementTreeRequest())
	if projected.Failed != nil {
		panic(projected.Failed.Diagnostics)
	}

	// Materialize the record as the canonical safe document.
	materialized := xmlpkg.Materialize(projected.Complete.Value,
		document.NewMaterializationRequest(
			document.NewProfileId("xml.1.0-safe", 1),
			document.NewMaterializationStyleId("xml.safe-canonical-document", 1)))
	if materialized.Failed != nil {
		panic(materialized.Failed.Failure)
	}
	fmt.Printf("materialized: %s", materialized.Complete.Document.Render())

	// Edit the parsed document: replace the "port" attribute value.
	var portOrdinal uint64
	portFound := false
	for _, attribute := range doc.Root().Attributes() {
		if attribute.QName.Local == "port" {
			portOrdinal = attribute.Ordinal
			portFound = true
		}
	}
	if !portFound {
		panic("port attribute missing")
	}
	builder := xmlpkg.NewEditTransactionBuilder(doc)
	builder.SetAttributeValue(
		doc.OccurrenceNodeRef(portOrdinal, document.RoleXmlAttribute), "9090")
	commit, editFailure := doc.Commit(builder.Build())
	if editFailure != nil {
		panic(editFailure)
	}
	fmt.Printf("edited: %s\n", commit.Document.Render())

	// Output:
	// attribute: name=api
	// attribute: port=8080
	// materialized: <service name="api" port="8080"><enabled>true</enabled></service>
	// edited: <service name="api" port="9090"><enabled>true</enabled></service>
}
