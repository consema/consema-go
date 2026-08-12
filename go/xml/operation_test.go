package xml

import (
	"context"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

func TestProjectionElementTree(t *testing.T) {
	doc := editParse(t, `<root a="1"><child>t</child></root>`)
	result := doc.Project(ElementTreeRequest())
	if result.Failed != nil {
		t.Fatalf("projection failed: %v", result.Failed.Diagnostics[0].Code)
	}
	record, ok := result.Complete.Value.(*core.Object)
	if !ok {
		t.Fatalf("record must be an object")
	}
	if got := objectString(record, "record"); got != "xml.element-tree@1" {
		t.Errorf("record %q", got)
	}
	root, _ := record.Get("root")
	rootObject := root.(*core.Object)
	expanded, _ := rootObject.Get("expanded-name")
	name := expanded.(*core.Object)
	if got := objectString(name, "local"); got != "root" {
		t.Errorf("root local %q", got)
	}
	attributes, _ := rootObject.Get("attributes")
	first := attributes.(*core.Array).Items()[0].(*core.Object)
	if got := objectString(first, "value"); got != "1" {
		t.Errorf("attribute value %q", got)
	}
	if result.Complete.Fidelity != FidelityExact {
		t.Errorf("fidelity %v", result.Complete.Fidelity)
	}
	if len(result.Complete.Provenance.Entries()) == 0 {
		t.Errorf("missing provenance")
	}
}

func TestProjectionRecoveredNeverProjects(t *testing.T) {
	doc, failure := Parse(context.Background(), []byte("<p:root/>"), XmlProfileSafeV1,
		XmlEncodingProfileDefault(), DefaultXmlParseLimits())
	if failure != nil {
		t.Fatalf("parse: %v", failure)
	}
	result := doc.Project(ElementTreeRequest())
	if result.Complete != nil {
		t.Fatalf("recovered projection must fail")
	}
	if len(result.Failed.Diagnostics) == 0 ||
		result.Failed.Diagnostics[0].Code != "xml.projection.recovered-document@1" {
		t.Errorf("wrong failure code")
	}
}

func TestMaterializationCanonical(t *testing.T) {
	record := materializationRecord(t, func(builder *core.ObjectBuilder) {
		builder.Insert("record", core.String("xml.element-tree@1"))
		root := core.NewObjectBuilder()
		name := core.NewObjectBuilder()
		name.Insert("namespace", core.NullValue())
		name.Insert("local", core.String("root"))
		root.Insert("expanded-name", name.Build())
		attributes := core.NewObjectBuilder()
		attrName := core.NewObjectBuilder()
		attrName.Insert("namespace", core.NullValue())
		attrName.Insert("local", core.String("a"))
		attributes.Insert("expanded-name", attrName.Build())
		attributes.Insert("value", core.String("1"))
		root.Insert("attributes", core.NewArray(attributes.Build()))
		content := core.NewObjectBuilder()
		content.Insert("kind", core.String("text"))
		content.Insert("fragments", core.NewArray(textFragment(t, "literal", "t")))
		root.Insert("content", core.NewArray(content.Build()))
		builder.Insert("root", root.Build())
	})
	request := document.NewMaterializationRequest(
		document.NewProfileId("xml.1.0-safe", 1),
		document.NewMaterializationStyleId("xml.safe-canonical-document", 1),
	)
	result := Materialize(record, request)
	if result.Failed != nil {
		t.Fatalf("materialize: %v", result.Failed.Failure.Name())
	}
	if got := string(result.Complete.Document.Render()); got != "<root a=\"1\">t</root>\n" {
		t.Errorf("render %q", got)
	}
}

func TestMaterializationEscapesAndRejects(t *testing.T) {
	record := materializationRecord(t, func(builder *core.ObjectBuilder) {
		builder.Insert("record", core.String("xml.element-tree@1"))
		root := core.NewObjectBuilder()
		name := core.NewObjectBuilder()
		name.Insert("namespace", core.NullValue())
		name.Insert("local", core.String("root"))
		root.Insert("expanded-name", name.Build())
		content := core.NewObjectBuilder()
		content.Insert("kind", core.String("text"))
		content.Insert("fragments", core.NewArray(textFragment(t, "literal", "a < b & c")))
		root.Insert("content", core.NewArray(content.Build()))
		builder.Insert("root", root.Build())
	})
	request := document.NewMaterializationRequest(
		document.NewProfileId("xml.1.0-safe", 1),
		document.NewMaterializationStyleId("xml.safe-canonical-document", 1),
	)
	result := Materialize(record, request)
	if result.Failed != nil {
		t.Fatalf("materialize: %v", result.Failed.Failure.Name())
	}
	if got := string(result.Complete.Document.Render()); got != "<root>a &lt; b &amp; c</root>\n" {
		t.Errorf("render %q", got)
	}

	// An unknown record id is rejected as invalid-record.
	record = materializationRecord(t, func(builder *core.ObjectBuilder) {
		builder.Insert("record", core.String("xml.something-else@1"))
		root := core.NewObjectBuilder()
		name := core.NewObjectBuilder()
		name.Insert("namespace", core.NullValue())
		name.Insert("local", core.String("root"))
		root.Insert("expanded-name", name.Build())
		builder.Insert("root", root.Build())
	})
	result = Materialize(record, request)
	if result.Complete != nil {
		t.Fatalf("invalid record must fail")
	}
	if result.Failed.Failure.Name() != "invalid-record" {
		t.Errorf("failure %s", result.Failed.Failure.Name())
	}
}

func materializationRecord(t *testing.T, build func(*core.ObjectBuilder)) core.Value {
	t.Helper()
	builder := core.NewObjectBuilder()
	build(builder)
	return builder.Build()
}

func textFragment(t *testing.T, kind, text string) core.Value {
	t.Helper()
	fragment := core.NewObjectBuilder()
	fragment.Insert("kind", core.String(kind))
	if kind == "literal" {
		fragment.Insert("text", core.String(text))
	}
	return fragment.Build()
}

func objectString(object *core.Object, name string) string {
	value, ok := object.Get(name)
	if !ok {
		return ""
	}
	text, ok := value.(core.String)
	if !ok {
		return ""
	}
	return string(text)
}

func TestNativeQuery(t *testing.T) {
	doc := editParse(t, `<root a="1"><child>t</child></root>`)
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("xml.document-root", 1)).
		Then(protocol.NewOperatorCall("xml.element-attributes", 1))
	executable, failure := bindTestQuery(protocol.DomainXMLNativeV1(), expression)
	if failure != nil {
		t.Fatalf("bind: %v", failure)
	}
	matches, queryFailure := ExecuteXMLQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if queryFailure != nil {
		t.Fatalf("execute: %v", queryFailure)
	}
	if len(matches) != 1 {
		t.Fatalf("match count %d", len(matches))
	}
	if matches[0].Kind != XmlMatchAttribute || matches[0].Local != "a" || matches[0].Value != "1" {
		t.Errorf("match facts %+v", matches[0])
	}
}

func TestSyntaxQuery(t *testing.T) {
	doc := editParse(t, `<root a="1">t</root>`)
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("xml.syntax-kind-is", 1).
			WithArgument("kind", core.String("local-name")))
	executable, failure := bindTestQuery(protocol.DomainXMLLosslessSyntaxV1(), expression)
	if failure != nil {
		t.Fatalf("bind: %v", failure)
	}
	matches, queryFailure := ExecuteXMLSyntaxQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if queryFailure != nil {
		t.Fatalf("execute: %v", queryFailure)
	}
	if len(matches) != 2 {
		t.Fatalf("match count %d", len(matches))
	}
	if matches[0].Kind() != XmlSyntaxKindLocalName || matches[0].Ordinal() != 1 ||
		matches[1].Ordinal() != 11 {
		t.Errorf("match facts %d %d", matches[0].Ordinal(), matches[1].Ordinal())
	}
}

func bindTestQuery(domain *protocol.QueryDomain,
	expression *protocol.QueryExpression) (*protocol.ExecutableQuery, *protocol.QueryFailure) {
	validated, failure := protocol.NewQueryDefinition(domain).
		WithExpression(expression).WithSelection(protocol.SelectionAll).Validate()
	if failure != nil {
		return nil, failure
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	return validated.Bind(capabilities)
}

func TestOperationRegistrySurface(t *testing.T) {
	registry := FormatOperationRegistryFor(XmlProfileSafeV1)
	operations := registry.Operations()
	if len(operations) != 8 {
		t.Fatalf("operation count %d", len(operations))
	}
	expected := []string{
		"xml.edit.insert-attribute@1",
		"xml.edit.insert-element@1",
		"xml.edit.remove-attribute@1",
		"xml.edit.remove-element@1",
		"xml.edit.rename-attribute@1",
		"xml.edit.rename-element@1",
		"xml.edit.replace-text@1",
		"xml.edit.set-attribute-value@1",
	}
	for index, operation := range operations {
		if operation.ID() != expected[index] {
			t.Errorf("operation %d: %s != %s", index, operation.ID(), expected[index])
		}
		if operation.Support() != "Supported" {
			t.Errorf("operation %s support %s", operation.ID(), operation.Support())
		}
	}
}

func TestEntityLimitsAmplification(t *testing.T) {
	source := `<!DOCTYPE root [<!ENTITY a "xxxxxxxxxxxxxxxxxxxx">]><root>&a;&a;&a;&a;&a;&a;</root>`
	limits := DefaultXmlParseLimits()
	limits.MaxEntityAmplificationRatio = 2
	doc, failure := Parse(context.Background(), []byte(source), XmlProfileSafeV1,
		XmlEncodingProfileDefault(), limits)
	if failure != nil {
		t.Fatalf("parse: %v", failure)
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("status %v", doc.FormationStatus())
	}
	count := 0
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == "xml.entity.amplification@1" {
			count++
		}
	}
	if count != 4 {
		t.Errorf("amplification diagnostics %d != 4", count)
	}
}

func TestEntityLimitsMixedContent(t *testing.T) {
	limits := DefaultXmlParseLimits()
	limits.MaxMixedContentItems = 1
	doc, failure := Parse(context.Background(), []byte("<root>a<child/></root>"), XmlProfileSafeV1,
		XmlEncodingProfileDefault(), limits)
	if failure != nil {
		t.Fatalf("parse: %v", failure)
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("status %v", doc.FormationStatus())
	}
	found := false
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == "xml.limit.mixed-content@1" {
			found = true
		}
	}
	if !found {
		t.Errorf("xml.limit.mixed-content@1 not found")
	}
}

func TestUtf16Bytes(t *testing.T) {
	var bytes []byte
	bytes = append(bytes, 0xFF, 0xFE)
	for _, unit := range utf16Encode("<root>中文</root>") {
		bytes = append(bytes, byte(unit), byte(unit>>8))
	}
	doc, failure := Parse(context.Background(), bytes, XmlProfileSafeV1,
		XmlEncodingProfileDefault(), DefaultXmlParseLimits())
	if failure != nil {
		t.Fatalf("parse: %v", failure)
	}
	hex := ""
	for _, b := range doc.Render() {
		hex += itoa(int(b)>>4) + itoa(int(b)&0xF)
	}
	_ = hex
	if root := doc.Root(); root == nil || root.Expanded() == nil || root.Expanded().Local != "root" {
		t.Fatalf("utf16 root mismatch")
	}
	children := doc.Root().Children()
	if len(children) != 1 || children[0].Text() == nil {
		t.Fatalf("utf16 content mismatch")
	}
	if got := TextSemantic(children[0].Text()); got != "中文" {
		t.Errorf("semantic %q", got)
	}
}
