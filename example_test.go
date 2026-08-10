package consema

// Runnable SDK example for the facade surface: the Convert* composition
// (plan §2.5 G4.4) across every format family, keeping both provenance
// directions and the two-stage report at every hop. The portable-value
// families (JSON, TOML, YAML) convert among each other with exact scalar
// semantics; the string-constrained families (INI, Java Properties)
// convert in and out through their canonical string surface; and the
// record-publishing families (XML, plist, HCL) convert within their own
// family — their projections publish the exact element-tree/value-tree/
// body records that only their own materializer consumes. Run with
// `go test .`; also visible in `go doc consema.dev/consema`.

import (
	"context"
	"fmt"

	"consema.dev/consema/document"
	hclpkg "consema.dev/consema/hcl"
	"consema.dev/consema/ini"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/plist"
	"consema.dev/consema/properties"
	"consema.dev/consema/toml"
	xmlpkg "consema.dev/consema/xml"
	"consema.dev/consema/yaml"
)

// Example converts one configuration around the family circle.
func Example() {
	// JSON → TOML → YAML → JSON: one object of scalars around the
	// portable-value circle, closing byte-exactly.
	source := mustParseJSON(`{"name":"api","port":8080,"enabled":true}`)
	jsonProjection := mustJSONProjection()
	toToml := ConvertJSON(mustAsJSON(source), jsonProjection,
		document.NewMaterializationRequest(
			document.NewProfileId("toml.1.0", 1),
			document.NewMaterializationStyleId("toml.canonical-document", 1)).
			WithNewline(document.NewlineLf).
			WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject))
	if toToml.Failed != nil {
		panic(toToml.Failed)
	}
	fmt.Printf("JSON → TOML: %s", toToml.Complete.Document.Render())
	toYaml := ConvertTOML(mustAsTOML(toToml), toml.NewProjectionRequest(
		toml.ProjectionTargetBestExactCoreV1), document.NewMaterializationRequest(
		document.NewProfileId("yaml.1.2-core", 1),
		document.NewMaterializationStyleId("yaml.canonical-flow", 1)).
		WithNewline(document.NewlineLf).
		WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject))
	if toYaml.Failed != nil {
		panic(toYaml.Failed)
	}
	fmt.Printf("TOML → YAML: %s", toYaml.Complete.Document.Render())
	toJSON := ConvertYAML(mustAsYAML(toYaml), yaml.BestExactValueV1(),
		document.NewMaterializationRequest(
			document.NewProfileId("json.strict", 1),
			document.NewMaterializationStyleId("json.canonical-compact", 1)).
			WithNewline(document.NewlineNone))
	if toJSON.Failed != nil {
		panic(toJSON.Failed)
	}
	if string(toJSON.Complete.Document.Render()) != `{"name":"api","port":8080,"enabled":true}` {
		panic("portable-value circle must close byte-exactly")
	}
	fmt.Printf("YAML → JSON: %s\n", toJSON.Complete.Document.Render())

	// JSON → INI: the nested object becomes one section of string
	// entries under the canonical portable profile.
	toIni := ConvertJSON(mustAsJSON(
		mustParseJSON(`{"service":{"name":"api","port":"8080"}}`)),
		jsonProjection, document.NewMaterializationRequest(
			document.NewProfileId("ini.portable", 1),
			document.NewMaterializationStyleId("ini.portable-canonical", 1)))
	if toIni.Failed != nil {
		panic(toIni.Failed)
	}
	fmt.Printf("JSON → INI: %s", toIni.Complete.Document.Render())

	// INI → JSON: the same section structure projects back.
	iniToJSON := ConvertINI(mustAsINI(toIni), ini.BestExactEntryMappingV1(),
		document.NewMaterializationRequest(
			document.NewProfileId("json.strict", 1),
			document.NewMaterializationStyleId("json.canonical-compact", 1)).
			WithNewline(document.NewlineNone))
	if iniToJSON.Failed != nil {
		panic(iniToJSON.Failed)
	}
	fmt.Printf("INI → JSON: %s\n", iniToJSON.Complete.Document.Render())

	// JSON → Java Properties: flat string entries under the canonical
	// Reader profile.
	toProperties := ConvertJSON(mustAsJSON(
		mustParseJSON(`{"name":"api","port":"8080"}`)),
		jsonProjection, document.NewMaterializationRequest(
			document.NewProfileId("java-properties.reader", 1),
			document.NewMaterializationStyleId("java-properties.reader-canonical", 1)).
			WithNewline(document.NewlineLf))
	if toProperties.Failed != nil {
		panic(toProperties.Failed)
	}
	fmt.Printf("JSON → Properties: %s", toProperties.Complete.Document.Render())

	// Java Properties → JSON.
	propertiesToJSON := ConvertProperties(mustAsProperties(toProperties),
		properties.BestExactEntryMapping(), document.NewMaterializationRequest(
			document.NewProfileId("json.strict", 1),
			document.NewMaterializationStyleId("json.canonical-compact", 1)).
			WithNewline(document.NewlineNone))
	if propertiesToJSON.Failed != nil {
		panic(propertiesToJSON.Failed)
	}
	fmt.Printf("Properties → JSON: %s\n", propertiesToJSON.Complete.Document.Render())

	// XML → XML: the element-tree record materializes as the canonical
	// safe document.
	xmlDoc, xmlFailure := xmlpkg.Parse(context.Background(),
		[]byte(`<root><name>api</name></root>`), xmlpkg.XmlProfileSafeV1,
		xmlpkg.XmlEncodingProfileDefault(), xmlpkg.DefaultXmlParseLimits())
	if xmlFailure != nil {
		panic(xmlFailure)
	}
	xmlToXML := ConvertXML(xmlDoc, xmlpkg.ElementTreeRequest(),
		document.NewMaterializationRequest(
			document.NewProfileId("xml.1.0-safe", 1),
			document.NewMaterializationStyleId("xml.safe-canonical-document", 1)))
	if xmlToXML.Failed != nil {
		panic(xmlToXML.Failed)
	}
	fmt.Printf("XML → XML: %s", xmlToXML.Complete.Document.Render())

	// plist converts between its two representations through the same
	// two-stage composition.
	plistDoc, plistFailure := plist.Parse([]byte(
		"<plist version=\"1.0\"><dict><key>name</key><string>api</string></dict></plist>"),
		plist.PlistProfileXmlV1, plist.PlistEncodingProfileDefault(),
		plist.DefaultPlistParseLimits())
	if plistFailure != nil {
		panic(plistFailure)
	}
	plistToBinary := ConvertPlist(plistDoc, plist.NewProjectionRequestValueTree(),
		document.NewMaterializationRequest(
			document.NewProfileId("plist.binary", 1),
			document.NewMaterializationStyleId("plist.binary-canonical", 1)).
			WithEncoding(document.BinaryEncoding()).
			WithNewline(document.NewlineNone))
	if plistToBinary.Failed != nil {
		panic(plistToBinary.Failed)
	}
	fmt.Printf("plist.xml → plist.binary: %s\n",
		string(plistToBinary.Complete.Document.Render()[:8]))

	// HCL converts within the native profile family.
	hclDoc, hclFailure := hclpkg.Parse(context.Background(), []byte("name = \"api\"\n"),
		hclpkg.HclProfileNativeV1, hclpkg.HclEncodingSelectionProfileDefault(),
		hclpkg.DefaultHclParseLimits())
	if hclFailure != nil {
		panic(hclFailure)
	}
	hclToHcl := ConvertHCL(hclDoc, hclpkg.ProjectionRequestBody(),
		document.NewMaterializationRequest(
			document.NewProfileId("hcl.native", 1),
			document.NewMaterializationStyleId("hcl.canonical-document", 1)))
	if hclToHcl.Failed != nil {
		panic(hclToHcl.Failed)
	}
	fmt.Printf("HCL → HCL: %s", hclToHcl.Complete.Document.Render())

	// Output:
	// JSON → TOML: "name" = "api"
	// "port" = 8080
	// "enabled" = true
	// TOML → YAML: --- !!map {? !!str "name" : !!str "api", ? !!str "port" : !!int "8080", ? !!str "enabled" : !!bool "true"}
	// YAML → JSON: {"name":"api","port":8080,"enabled":true}
	// JSON → INI: [service]
	// name=api
	// port=8080
	// INI → JSON: {"service":{"name":"api","port":"8080"}}
	// JSON → Properties: name=api
	// port=8080
	// Properties → JSON: {"name":"api","port":"8080"}
	// XML → XML: <root><name>api</name></root>
	// plist.xml → plist.binary: bplist00
	// HCL → HCL: name = "api"
	//
}

// mustParseJSON parses one strict JSON source through the facade entry.
func mustParseJSON(text string) *Document {
	source, failure := ParseDocument(context.Background(), []byte(text),
		document.NewProfileId("json.strict", 1))
	if failure != nil {
		panic(failure)
	}
	return source
}

// mustJSONProjection builds the best-exact JSON projection request.
func mustJSONProjection() *jsonpkg.ProjectionRequest {
	projection, failure := jsonpkg.NewProjectionRequestBuilder(
		jsonpkg.ProjectionTargetBestExactCoreV1).Build()
	if failure != nil {
		panic(failure)
	}
	return projection
}

// mustAsJSON unwraps the JSON document of one parse result.
func mustAsJSON(document *Document) *jsonpkg.Document {
	doc, ok := document.AsJSON()
	if !ok {
		panic("source must be a JSON document")
	}
	return doc
}

// mustAsTOML unwraps the TOML document of one conversion result.
func mustAsTOML(result ConversionResult) *toml.Document {
	doc, ok := result.Complete.Document.AsTOML()
	if !ok {
		panic("target must be a TOML document")
	}
	return doc
}

// mustAsYAML unwraps the YAML document of one conversion result.
func mustAsYAML(result ConversionResult) *yaml.Document {
	doc, ok := result.Complete.Document.AsYAML()
	if !ok {
		panic("target must be a YAML document")
	}
	return doc
}

// mustAsINI unwraps the INI document of one conversion result.
func mustAsINI(result ConversionResult) *ini.Document {
	doc, ok := result.Complete.Document.AsINI()
	if !ok {
		panic("target must be an INI document")
	}
	return doc
}

// mustAsProperties unwraps the Java Properties document of one conversion
// result.
func mustAsProperties(result ConversionResult) *properties.Document {
	doc, ok := result.Complete.Document.AsProperties()
	if !ok {
		panic("target must be a Java Properties document")
	}
	return doc
}
