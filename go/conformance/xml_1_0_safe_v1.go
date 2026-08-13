package conformance

// The `consema.xml-1-0-safe.conformance@1` suite runner, mirroring the
// XML driver of consema-rs/crates/consema-conformance/src/xml_v1.rs. The suite's
// capability surface landed with milestone 0.17.0 G3.1 (go/xml); every
// case is data-driven: the vector input and expected facts drive the
// execution, and no expectation literal lives here.

import (
	"context"
	"fmt"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
	"consema.dev/consema/xml"
)

// runXml10SafeV1 executes the frozen 34-case XML suite.
func runXml10SafeV1(_ *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		var failure string
		switch vector.Capability {
		case "xml.formation@1":
			failure = runXMLFormationCase(vector)
		case "xml.syntax-query@1":
			failure = runXMLSyntaxQueryCase(vector)
		case "xml.native-query@1":
			failure = runXMLNativeQueryCase(vector)
		case "xml.projection@1":
			failure = runXMLProjectionCase(vector)
		case "xml.materialization@1":
			failure = runXMLMaterializationCase(vector)
		case "xml.edit@1":
			failure = runXMLEditCase(vector)
		case "xml.limit@1":
			failure = runXMLFormationCase(vector)
		default:
			failure = "runner does not recognize published capability " + vector.Capability
		}
		if failure != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: failure})
		} else {
			report.Passed = append(report.Passed, vector.ID)
		}
	}
	return report
}

// xmlFormDocument parses the case source under the frozen profile and the
// case-specific limits (xml_v1.rs form_document, parse_limits).
func xmlFormDocument(vector *caseData) (*xml.Document, string) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		return nil, "missing input.source"
	}
	var bytes []byte
	if encoding, ok := stringField(vector.Input, "encoding"); ok && encoding == "utf16le-bom" {
		bytes = append(bytes, 0xFF, 0xFE)
		for _, unit := range utf16EncodeXML(source) {
			bytes = append(bytes, byte(unit), byte(unit>>8))
		}
	} else {
		bytes = []byte(source)
	}
	limits := xml.DefaultXmlParseLimits()
	if ratio, ok := integerField(vector.Input, "amplification_ratio"); ok {
		limits.MaxEntityAmplificationRatio = ratio
	}
	if items, ok := integerField(vector.Input, "max_mixed_content_items"); ok {
		limits.MaxMixedContentItems = int(items)
	}
	doc, failure := xml.Parse(context.Background(), bytes, xml.XmlProfileSafeV1,
		xml.XmlEncodingProfileDefault(), limits)
	if failure != nil {
		return nil, failure.Code()
	}
	return doc, ""
}

// utf16EncodeXML encodes one string into UTF-16 code units.
func utf16EncodeXML(text string) []uint16 {
	var units []uint16
	for _, r := range text {
		if r >= 0x10000 {
			value := r - 0x10000
			units = append(units, uint16(0xD800+value>>10), uint16(0xDC00+value&0x3FF))
		} else {
			units = append(units, uint16(r))
		}
	}
	return units
}

// runXMLFormationCase runs one `xml.formation@1` case (xml_v1.rs
// run_formation); the limit cases delegate here.
func runXMLFormationCase(vector *caseData) string {
	doc, failure := xmlFormDocument(vector)
	if failure != "" {
		return "formation: " + failure
	}
	status, ok := stringField(vector.Expected, "status")
	if !ok {
		return "missing expected.status"
	}
	actualStatus := doc.FormationStatus().String()
	if actualStatus != status {
		return fmt.Sprintf("status %s != %s", actualStatus, status)
	}
	if status == "Complete" {
		if render, ok := stringField(vector.Expected, "render"); ok {
			if string(doc.Render()) != render {
				return fmt.Sprintf("render %q != %q", doc.Render(), render)
			}
		}
		if hex, ok := stringField(vector.Expected, "render_hex"); ok {
			actual := ""
			for _, byte := range doc.Render() {
				actual += fmt.Sprintf("%02x", byte)
			}
			if actual != hex {
				return fmt.Sprintf("render_hex %s != %s", actual, hex)
			}
		}
	}
	if diagnostic, ok := stringField(vector.Expected, "diagnostic"); ok {
		found := false
		for _, item := range doc.Diagnostics() {
			if item.Code == diagnostic {
				found = true
				break
			}
		}
		if !found {
			return fmt.Sprintf("diagnostic %s not found", diagnostic)
		}
	}
	return ""
}

// xmlFilters builds the operator chain from the vector filters
// (xml_v1.rs build_filters).
func xmlFilters(vector *caseData) (*protocol.QueryExpression, string) {
	filterValues, ok := sequenceField(vector.Input, "filters")
	if !ok {
		return nil, "missing input.filters"
	}
	expression := &protocol.QueryExpression{Kind: protocol.ExpressionInput}
	for _, filter := range filterValues {
		operator, ok := stringField(filter, "operator")
		if !ok {
			return nil, "missing filter.operator"
		}
		call := protocol.NewOperatorCall(operator, 1)
		if argument, ok := stringField(filter, "argument"); ok {
			switch operator {
			case "xml.syntax-kind-is":
				call = call.WithArgument("kind", core.String(argument))
			case "xml.syntax-text-equals":
				call = call.WithArgument("text", core.String(argument))
			default:
				call = call.WithArgument("argument", core.String(argument))
			}
		}
		expression = expression.Then(call)
	}
	return expression, ""
}

// xmlBind binds one XML-domain query against the frozen capabilities
// (xml_v1.rs capabilities).
func xmlBind(domain *protocol.QueryDomain, expression *protocol.QueryExpression) (*protocol.ExecutableQuery, string) {
	validated, failure := protocol.NewQueryDefinition(domain).
		WithExpression(expression).WithSelection(protocol.SelectionAll).Validate()
	if failure != nil {
		return nil, fmt.Sprintf("definition: %s", failure.Code())
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, failure := validated.Bind(capabilities)
	if failure != nil {
		return nil, fmt.Sprintf("bind: %s", failure.Code())
	}
	return executable, ""
}

// runXMLSyntaxQueryCase runs one `xml.syntax-query@1` case (xml_v1.rs
// run_syntax_query): kind and raw text are compared; the vector ordinals
// are informational and do not participate (exactly as the Rust runner).
func runXMLSyntaxQueryCase(vector *caseData) string {
	doc, failure := xmlFormDocument(vector)
	if failure != "" {
		return "syntax-query: " + failure
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		return "syntax-query input must form completely"
	}
	expression, failure := xmlFilters(vector)
	if failure != "" {
		return failure
	}
	executable, failure := xmlBind(protocol.DomainXMLLosslessSyntaxV1(), expression)
	if failure != "" {
		return failure
	}
	matches, queryFailure := xml.ExecuteXMLSyntaxQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if queryFailure != nil {
		return fmt.Sprintf("execute: %s", queryFailure.Code())
	}
	expectedMatches, ok := sequenceField(vector.Expected, "matches")
	if !ok {
		return "missing expected.matches"
	}
	if len(matches) != len(expectedMatches) {
		return fmt.Sprintf("match count %d != %d", len(matches), len(expectedMatches))
	}
	raw := doc.Source().Bytes()
	for index, match := range matches {
		expected := expectedMatches[index]
		kind, ok := stringField(expected, "kind")
		if !ok {
			return "missing expected match kind"
		}
		if match.Kind().AsStr() != kind {
			return fmt.Sprintf("kind %s != %s", match.Kind().AsStr(), kind)
		}
		if text, ok := stringField(expected, "text"); ok {
			span := match.Span()
			actual := string(raw[span.StartByte():span.EndByte()])
			if actual != text {
				return fmt.Sprintf("text %q != %q", actual, text)
			}
		}
	}
	return ""
}

// runXMLNativeQueryCase runs one `xml.native-query@1` case (xml_v1.rs
// run_native_query): local and value facts are compared.
func runXMLNativeQueryCase(vector *caseData) string {
	doc, failure := xmlFormDocument(vector)
	if failure != "" {
		return "native-query: " + failure
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		return "native-query input must form completely"
	}
	expression, failure := xmlFilters(vector)
	if failure != "" {
		return failure
	}
	executable, failure := xmlBind(protocol.DomainXMLNativeV1(), expression)
	if failure != "" {
		return failure
	}
	matches, queryFailure := xml.ExecuteXMLQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if queryFailure != nil {
		return fmt.Sprintf("execute: %s", queryFailure.Code())
	}
	expectedMatches, ok := sequenceField(vector.Expected, "matches")
	if !ok {
		return "missing expected.matches"
	}
	if len(matches) != len(expectedMatches) {
		return fmt.Sprintf("match count %d != %d", len(matches), len(expectedMatches))
	}
	for index, match := range matches {
		expected := expectedMatches[index]
		if local, ok := stringField(expected, "local"); ok {
			if match.Kind != xml.XmlMatchElement && match.Kind != xml.XmlMatchAttribute {
				return "unexpected match kind"
			}
			if match.Local != local {
				return fmt.Sprintf("local %s != %s", match.Local, local)
			}
		}
		if value, ok := stringField(expected, "value"); ok {
			if match.Kind != xml.XmlMatchAttribute {
				return "expected attribute match"
			}
			if match.Value != value {
				return fmt.Sprintf("value %s != %s", match.Value, value)
			}
		}
	}
	return ""
}

// runXMLProjectionCase runs one `xml.projection@1` case (xml_v1.rs
// run_projection).
func runXMLProjectionCase(vector *caseData) string {
	doc, failure := xmlFormDocument(vector)
	if failure != "" {
		return "projection: " + failure
	}
	result := doc.Project(xml.ElementTreeRequest())
	if expectedFailure, ok := stringField(vector.Expected, "failure"); ok {
		if result.Failed == nil {
			return "projection must fail"
		}
		code := ""
		if len(result.Failed.Diagnostics) > 0 {
			code = result.Failed.Diagnostics[0].Code
		}
		if code != expectedFailure {
			return fmt.Sprintf("failure code %s != %s", code, expectedFailure)
		}
		return ""
	}
	if result.Failed != nil {
		return "projection must complete"
	}
	projection := result.Complete
	record, ok := projection.Value.(*core.Object)
	if !ok {
		return "record must be an object"
	}
	if recordID, ok := stringField(vector.Expected, "record"); ok {
		if actual := objectFieldString(record, "record"); actual != recordID {
			return fmt.Sprintf("record %s != %s", actual, recordID)
		}
	}
	rootValue, ok := record.Get("root")
	if !ok {
		return "missing root"
	}
	rootObject, ok := rootValue.(*core.Object)
	if !ok {
		return "root must be an object"
	}
	if rootLocal, ok := stringField(vector.Expected, "root_local"); ok {
		expanded, ok := rootObject.Get("expanded-name")
		if !ok {
			return "missing expanded-name"
		}
		name, ok := expanded.(*core.Object)
		if !ok {
			return "missing expanded-name"
		}
		local, ok := name.Get("local")
		if !ok {
			return "missing expanded-name.local"
		}
		if string(local.(core.String)) != rootLocal {
			return fmt.Sprintf("root_local %s != %s", local, rootLocal)
		}
	}
	if rootNamespace, ok := stringField(vector.Expected, "root_namespace"); ok {
		expanded, ok := rootObject.Get("expanded-name")
		if !ok {
			return "missing expanded-name"
		}
		name, ok := expanded.(*core.Object)
		if !ok {
			return "missing expanded-name"
		}
		namespace, ok := name.Get("namespace")
		if !ok {
			return "missing expanded-name.namespace"
		}
		namespaceText, ok := namespace.(core.String)
		if !ok {
			return "missing expanded-name.namespace"
		}
		if string(namespaceText) != rootNamespace {
			return fmt.Sprintf("root_namespace %s != %s", namespaceText, rootNamespace)
		}
	}
	if attributeValue, ok := stringField(vector.Expected, "root_attribute_value"); ok {
		attributes, ok := rootObject.Get("attributes")
		if !ok {
			return "missing attributes"
		}
		array, ok := attributes.(*core.Array)
		if !ok || len(array.Items()) == 0 {
			return "missing attributes sequence"
		}
		first, ok := array.Items()[0].(*core.Object)
		if !ok {
			return "missing attribute value"
		}
		value, ok := first.Get("value")
		if !ok {
			return "missing attribute value"
		}
		if string(value.(core.String)) != attributeValue {
			return fmt.Sprintf("attribute value %s != %s", value, attributeValue)
		}
	}
	if contentKinds, ok := sequenceField(vector.Expected, "content_kinds"); ok {
		content, ok := rootObject.Get("content")
		if !ok {
			return "missing content"
		}
		array, ok := content.(*core.Array)
		if !ok {
			return "missing content sequence"
		}
		items := array.Items()
		if len(items) != len(contentKinds) {
			return fmt.Sprintf("content count %d != %d", len(items), len(contentKinds))
		}
		for index, item := range items {
			expectedKind, ok := contentKinds[index].(core.String)
			if !ok {
				return "content kind must be a string"
			}
			itemObject, ok := item.(*core.Object)
			if !ok {
				return "content item must be an object"
			}
			actualKind := ""
			if _, hasName := itemObject.Get("expanded-name"); hasName {
				actualKind = "element"
			} else if kind, ok := itemObject.Get("kind"); ok {
				actualKind = string(kind.(core.String))
			}
			if actualKind != string(expectedKind) {
				return fmt.Sprintf("kind %s != %s", actualKind, expectedKind)
			}
		}
	}
	return ""
}

// objectFieldString reads one string field of an object value.
func objectFieldString(object *core.Object, name string) string {
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

// runXMLMaterializationCase runs one `xml.materialization@1` case
// (xml_v1.rs run_materialization).
func runXMLMaterializationCase(vector *caseData) string {
	record, ok := caseInput(vector, "record")
	if !ok {
		return "missing input.record"
	}
	request := document.NewMaterializationRequest(
		document.NewProfileId("xml.1.0-safe", 1),
		document.NewMaterializationStyleId("xml.safe-canonical-document", 1),
	)
	result := xml.Materialize(record, request)
	if expectedFailure, ok := stringField(vector.Expected, "failure"); ok {
		if result.Failed == nil {
			return "materialization must fail"
		}
		actual := result.Failed.Failure.Name()
		if actual != expectedFailure {
			return fmt.Sprintf("failure %s != %s", actual, expectedFailure)
		}
		if len(result.Failed.AnalyzedInputPaths) > request.Limits().MaxInputNodes {
			return "analyzed_input_paths exceeds max_input_nodes"
		}
		return ""
	}
	if result.Failed != nil {
		return "materialization must complete"
	}
	render, ok := stringField(vector.Expected, "render")
	if !ok {
		return "missing expected.render"
	}
	actual := string(result.Complete.Document.Render())
	if actual != render {
		return fmt.Sprintf("render %q != %q", actual, render)
	}
	return ""
}

// runXMLEditCase runs one `xml.edit@1` case (xml_v1.rs run_edit).
func runXMLEditCase(vector *caseData) string {
	doc, failure := xmlFormDocument(vector)
	if failure != "" {
		return "edit: " + failure
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		return "edit input must form completely"
	}
	operations, ok := sequenceField(vector.Input, "operations")
	if !ok {
		return "missing input.operations"
	}
	builder := xml.NewEditTransactionBuilder(doc)
	for _, operation := range operations {
		op, ok := stringField(operation, "op")
		if !ok {
			return "missing op"
		}
		switch op {
		case "replace-text":
			ordinal := xmlOccurrenceField(operation, "text")
			value, ok := stringField(operation, "value")
			if !ok {
				return "missing value"
			}
			target, ok := xmlFindText(doc, ordinal)
			if !ok {
				return "text occurrence not found"
			}
			builder.ReplaceText(target, value)
		case "insert-attribute":
			elementName, ok := stringField(operation, "element")
			if !ok {
				return "missing element"
			}
			name, ok := stringField(operation, "name")
			if !ok {
				return "missing name"
			}
			value, ok := stringField(operation, "value")
			if !ok {
				return "missing value"
			}
			target, ok := xmlFindElement(doc, elementName, xmlOperationOrdinal(operation))
			if !ok {
				return "element not found"
			}
			var placement xml.AttributePlacement
			placementName, _ := stringField(operation, "placement")
			switch placementName {
			case "", "End":
				placement = xml.AttributePlacementEnd()
			case "Before":
				anchor, ok := xmlFindAnchorAttribute(doc, target, xmlAnchorName(operation))
				if !ok {
					return "anchor attribute not found"
				}
				placement = xml.AttributePlacementBefore(anchor)
			case "After":
				anchor, ok := xmlFindAnchorAttribute(doc, target, xmlAnchorName(operation))
				if !ok {
					return "anchor attribute not found"
				}
				placement = xml.AttributePlacementAfter(anchor)
			default:
				return "unknown placement " + placementName
			}
			builder.InsertAttribute(target, xml.NewNameFacts(nil, name, nil), value, placement)
		case "remove-attribute":
			name, ok := stringField(operation, "attribute")
			if !ok {
				return "missing attribute"
			}
			attribute, ok := xmlFindAttribute(doc, name, xmlOperationOrdinal(operation))
			if !ok {
				return "attribute not found"
			}
			builder.RemoveAttribute(attribute)
		case "rename-attribute":
			from, ok := stringField(operation, "attribute")
			if !ok {
				return "missing attribute"
			}
			to, ok := stringField(operation, "to")
			if !ok {
				return "missing to"
			}
			attribute, ok := xmlFindAttribute(doc, from, xmlOperationOrdinal(operation))
			if !ok {
				return "attribute not found"
			}
			builder.RenameAttribute(attribute, xml.NewNameFacts(nil, to, nil))
		case "set-attribute-value":
			name, ok := stringField(operation, "attribute")
			if !ok {
				return "missing attribute"
			}
			value, ok := stringField(operation, "value")
			if !ok {
				return "missing value"
			}
			attribute, ok := xmlFindAttribute(doc, name, xmlOperationOrdinal(operation))
			if !ok {
				return "attribute not found"
			}
			builder.SetAttributeValue(attribute, value)
		case "insert-element":
			root := doc.Root()
			if root == nil {
				return "missing root"
			}
			name, ok := stringField(operation, "name")
			if !ok {
				return "missing name"
			}
			var content *string
			if value, ok := stringField(operation, "content"); ok {
				content = &value
			}
			builder.InsertElement(root.NodeRef(), xml.NewNameFacts(nil, name, nil),
				content, xml.ContentPlacementEnd())
		case "remove-element":
			name, ok := stringField(operation, "name")
			if !ok {
				return "missing name"
			}
			target, ok := xmlFindElement(doc, name, xmlOperationOrdinal(operation))
			if !ok {
				return "element not found"
			}
			builder.RemoveElement(target)
		case "rename-element":
			from, ok := stringField(operation, "from")
			if !ok {
				return "missing from"
			}
			to, ok := stringField(operation, "to")
			if !ok {
				return "missing to"
			}
			target, ok := xmlFindElement(doc, from, xmlOperationOrdinal(operation))
			if !ok {
				return "element not found"
			}
			builder.RenameElement(target, xml.NewNameFacts(nil, to, nil))
		default:
			return "unknown edit op " + op
		}
	}
	commit, editFailure := doc.Commit(builder.Build())
	if editFailure != nil {
		return editFailure.Error()
	}
	render, ok := stringField(vector.Expected, "render")
	if !ok {
		return "missing expected.render"
	}
	actual := string(commit.Document.Render())
	if actual != render {
		return fmt.Sprintf("render %q != %q", actual, render)
	}
	return ""
}

// xmlOperationOrdinal reads the optional occurrence selector of one edit
// operation (xml_v1.rs operation_ordinal).
func xmlOperationOrdinal(operation core.Value) uint64 {
	ordinal, ok := integerField(operation, "ordinal")
	if !ok {
		return 0
	}
	return ordinal
}

// xmlOccurrenceField reads the required text occurrence selector.
func xmlOccurrenceField(operation core.Value, name string) uint64 {
	ordinal, ok := integerField(operation, name)
	if !ok {
		return 0
	}
	return ordinal
}

// xmlAnchorName reads the anchor attribute name of a Before/After
// insertion placement.
func xmlAnchorName(operation core.Value) string {
	name, ok := stringField(operation, "anchor")
	if !ok {
		return ""
	}
	return name
}

// xmlFindAttribute resolves the `ordinal`-th attribute with `name` in
// document order (xml_v1.rs find_attribute).
func xmlFindAttribute(doc *xml.Document, name string, ordinal uint64) (document.NodeRef, bool) {
	occurrence := uint64(0)
	for _, content := range doc.Nodes() {
		if content.Kind != xml.XmlContentElement {
			continue
		}
		for _, attribute := range content.Element.Attributes {
			if attribute.QName.Local == name {
				if occurrence == ordinal {
					return doc.OccurrenceNodeRef(attribute.Ordinal, document.RoleXmlAttribute), true
				}
				occurrence++
			}
		}
	}
	return document.NodeRef{}, false
}

// xmlFindElement resolves the `ordinal`-th element with `name` in document
// order (xml_v1.rs find_element).
func xmlFindElement(doc *xml.Document, name string, ordinal uint64) (document.NodeRef, bool) {
	occurrence := uint64(0)
	for index, content := range doc.Nodes() {
		if content.Kind != xml.XmlContentElement {
			continue
		}
		if content.Element.QName.Local == name {
			if occurrence == ordinal {
				return doc.OccurrenceNodeRef(uint64(index), document.RoleXmlElement), true
			}
			occurrence++
		}
	}
	return document.NodeRef{}, false
}

// xmlFindText resolves the `ordinal`-th text occurrence in document order
// (xml_v1.rs find_text).
func xmlFindText(doc *xml.Document, ordinal uint64) (document.NodeRef, bool) {
	occurrence := uint64(0)
	for _, content := range doc.Nodes() {
		if content.Kind != xml.XmlContentText {
			continue
		}
		if occurrence == ordinal {
			return doc.OccurrenceNodeRef(content.Text.Ordinal, document.RoleXmlText), true
		}
		occurrence++
	}
	return document.NodeRef{}, false
}

// xmlFindAnchorAttribute resolves one attribute anchor on exactly one
// element (xml_v1.rs find_anchor_attribute).
func xmlFindAnchorAttribute(doc *xml.Document, element document.NodeRef, name string) (document.NodeRef, bool) {
	index := int(element.Index())
	if index >= len(doc.Nodes()) || doc.Nodes()[index].Kind != xml.XmlContentElement {
		return document.NodeRef{}, false
	}
	for _, attribute := range doc.Nodes()[index].Element.Attributes {
		if attribute.QName.Local == name {
			return doc.OccurrenceNodeRef(attribute.Ordinal, document.RoleXmlAttribute), true
		}
	}
	return document.NodeRef{}, false
}
