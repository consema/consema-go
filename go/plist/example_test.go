package plist_test

// Runnable SDK example for the plist family: parse → native query →
// value-tree projection → materialization → structural edit (plan §2.5
// G4.4). Run with `go test ./plist/`; also visible in
// `go doc consema.dev/consema/plist`.

import (
	"context"
	"fmt"

	"consema.dev/consema/document"
	"consema.dev/consema/plist"
	"consema.dev/consema/protocol"
)

// Example walks one property list through the full SDK chain.
func Example() {
	// Parse under the exact `plist.xml@1` profile.
	source := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>name</key><string>api</string><key>port</key><integer>8080</integer></dict></plist>`)
	doc, formationFailure := plist.Parse(source, plist.PlistProfileXmlV1,
		plist.PlistEncodingProfileDefault(), plist.DefaultPlistParseLimits())
	if formationFailure != nil {
		panic(formationFailure)
	}

	// Query the native model: the dictionary entries of the root.
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("plist.document-root", 1)).
		Then(protocol.NewOperatorCall("plist.dict-entries", 1))
	definition := protocol.NewQueryDefinition(protocol.DomainPlistNativeV1()).
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
	matches, queryFailure := plist.ExecutePlistNativeQuery(context.Background(), executable,
		doc, protocol.DefaultQueryLimits())
	if queryFailure != nil {
		panic(queryFailure)
	}
	for _, match := range matches {
		if match.Kind == plist.PlistMatchDictEntry && match.Key != nil {
			keyText, keyError := match.Key.ToUnicode()
			if keyError != nil {
				panic(keyError)
			}
			fmt.Printf("key: %s\n", keyText)
		}
	}

	// Project the document to the exact value-tree record.
	projected := plist.Project(doc, plist.NewProjectionRequestValueTree())
	if projected.Failed != nil {
		panic(projected.Failed.Diagnostics)
	}

	// Materialize the record as the canonical XML property list.
	materialized := plist.Materialize(projected.Complete.Value,
		document.NewMaterializationRequest(
			document.NewProfileId("plist.xml", 1),
			document.NewMaterializationStyleId("plist.xml-canonical", 1)).
			WithEncoding(document.Utf8Encoding()).
			WithNewline(document.NewlineLf))
	if materialized.Failed != nil {
		panic(materialized.Failed.Failure)
	}
	fmt.Printf("materialized: %s", materialized.Complete.Document.Render())

	// Edit the parsed document: set the "port" value.
	builder := plist.NewEditTransactionBuilder(doc)
	builder.SetValue(plist.NewEditPath([]plist.EditPathStep{
		plist.NewEditPathStepDictKey(plist.NewPlistKeyFromUnicode("port"), 0),
	}), plist.NewEditValueInteger(plist.NewPlistInteger(9090)))
	commit, editFailure := doc.Commit(builder.Build())
	if editFailure != nil {
		panic(editFailure)
	}
	fmt.Printf("edited: %s\n", commit.Document.Render())

	// Output:
	// key: name
	// key: port
	// materialized: <?xml version="1.0" encoding="UTF-8"?>
	// <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
	// <plist version="1.0">
	//     <dict>
	//         <key>name</key>
	//         <string>api</string>
	//         <key>port</key>
	//         <integer>8080</integer>
	//     </dict>
	// </plist>
	// edited: <?xml version="1.0" encoding="UTF-8"?>
	// <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
	// <plist version="1.0"><dict><key>name</key><string>api</string><key>port</key><integer>9090</integer></dict></plist>
}
