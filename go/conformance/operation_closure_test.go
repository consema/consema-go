package conformance

// Operation closure verification (milestone 0.16.0 G2.4; plan §2.3
// deliverable "全操作补齐（query/projection/materialization/edit 闭包）").
//
// The five delivered families publish frozen per-profile operation
// registries (RFC 0015 §6.2; consema-document operation_registry.rs:234):
// json 8, toml 7, yaml 8, ini 8, java-properties 5. This test verifies
// that every registered operation ID is actually executable through the
// family's public edit API: each row parses a small document, applies the
// operation through the EditTransactionBuilder, commits, and asserts a
// Completed commit with at least one source edit. An operation that could
// not be mapped to a builder invocation would fail here (unreachable).
//
// The convert composition face (root package ConvertJSON/ConvertTOML and
// the yaml/ini/properties/xml/plist/hcl faces, all delivered by 0.18.0
// G4.2) is checked separately: the json<->toml face is exercised here and
// in convert_face_test.go; the full eight-family convert composition is
// verified by the conformance convert cases and the cross-format
// conversion tests (G054, adversarial audit 2026-08-13 — the
// "not yet published / planned for 0.18.0" note was stale).

import (
	"context"
	"math/big"
	"strconv"
	"strings"
	"testing"

	consema "consema.dev/consema"
	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/ini"
	"consema.dev/consema/json"
	"consema.dev/consema/properties"
	"consema.dev/consema/toml"
	"consema.dev/consema/yaml"
)

func TestJSONOperationClosure(t *testing.T) {
	registry := json.FormatOperationRegistryFor(json.JsonProfileStrictV1)
	ids := make([]string, 0, len(registry.Operations()))
	for _, operation := range registry.Operations() {
		ids = append(ids, operation.ID())
	}
	assertOperationIDs(t, ids, []string{
		"json.edit.insert-member@1", "json.edit.remove-member@1",
		"json.edit.move-member@1", "json.edit.rename-member@1",
		"json.edit.insert-array-element@1", "json.edit.remove-array-element@1",
		"json.edit.replace-scalar-semantic@1", "json.edit.replace-scalar-literal@1",
	})

	object := jsonParseForClosure(t, `{"a":1,"b":2}`)
	array := jsonParseForClosure(t, `[1,2,3]`)
	runJSONEdit(t, object, "json.edit.insert-member@1", func(b *json.EditTransactionBuilder) {
		b.InsertMember(object.Root().NodeRef(), "c", core.NewInteger(newClosureInt(3)), json.PlacementAtEnd())
	})
	runJSONEdit(t, object, "json.edit.remove-member@1", func(b *json.EditTransactionBuilder) {
		b.RemoveMember(jsonMemberRef(t, object, 1))
	})
	runJSONEdit(t, object, "json.edit.move-member@1", func(b *json.EditTransactionBuilder) {
		b.MoveMember(jsonMemberRef(t, object, 0), json.PlacementAtEnd())
	})
	runJSONEdit(t, object, "json.edit.rename-member@1", func(b *json.EditTransactionBuilder) {
		b.RenameMember(jsonMemberRef(t, object, 0), "z")
	})
	runJSONEdit(t, array, "json.edit.insert-array-element@1", func(b *json.EditTransactionBuilder) {
		b.InsertArrayElement(array.Root().NodeRef(), core.Boolean(true), json.PlacementAtEnd())
	})
	runJSONEdit(t, array, "json.edit.remove-array-element@1", func(b *json.EditTransactionBuilder) {
		b.RemoveArrayElement(jsonArrayElementRef(t, array, 1))
	})
	runJSONEdit(t, object, "json.edit.replace-scalar-semantic@1", func(b *json.EditTransactionBuilder) {
		b.SemanticScalar(jsonMemberValueRef(t, object, 0), core.NewInteger(newClosureInt(5)),
			json.RepresentationPolicyPreserveCompatible)
	})
	runJSONEdit(t, object, "json.edit.replace-scalar-literal@1", func(b *json.EditTransactionBuilder) {
		b.LiteralScalar(jsonMemberValueRef(t, object, 0), []byte("7"))
	})
}

func TestTOMLOperationClosure(t *testing.T) {
	registry := toml.NewFormatOperationRegistry(toml.Toml10V1)
	ids := make([]string, 0, len(registry.Operations()))
	for _, operation := range registry.Operations() {
		ids = append(ids, operation.ID.ID()+"@"+uint64ClosureString(operation.ID.Version()))
	}
	assertOperationIDs(t, ids, []string{
		"toml.edit.insert-entry@1", "toml.edit.remove-entry@1",
		"toml.edit.rename-entry@1", "toml.edit.insert-array-element@1",
		"toml.edit.remove-array-element@1", "toml.edit.replace-scalar-semantic@1",
		"toml.edit.replace-scalar-literal@1",
	})

	table := tomlParseForClosure(t, "a = 1\nb = 2\n")
	values := tomlParseForClosure(t, "values = [1, 3]\n")
	array := tomlParseForClosure(t, "values = [1, 2, 3]\n")
	runTomlEdit(t, table, "toml.edit.insert-entry@1", func(b *toml.EditTransactionBuilder) {
		b.InsertEntry(table.Root().NodeRef(), "c", core.NewInteger(newClosureInt(3)), toml.PlacementEnd())
	})
	runTomlEdit(t, table, "toml.edit.remove-entry@1", func(b *toml.EditTransactionBuilder) {
		b.RemoveEntry(tomlEntryRef(t, table, 0))
	})
	runTomlEdit(t, table, "toml.edit.rename-entry@1", func(b *toml.EditTransactionBuilder) {
		b.RenameEntry(tomlEntryRef(t, table, 0), "z")
	})
	runTomlEdit(t, values, "toml.edit.insert-array-element@1", func(b *toml.EditTransactionBuilder) {
		b.InsertArrayElement(tomlEntryItemRef(t, values, 0), core.NewInteger(newClosureInt(2)), toml.PlacementEnd())
	})
	runTomlEdit(t, array, "toml.edit.remove-array-element@1", func(b *toml.EditTransactionBuilder) {
		b.RemoveArrayElement(tomlArrayElementRef(t, array, 1))
	})
	runTomlEdit(t, table, "toml.edit.replace-scalar-semantic@1", func(b *toml.EditTransactionBuilder) {
		b.SemanticScalar(tomlEntryItemRef(t, table, 0), core.NewInteger(newClosureInt(5)),
			toml.RepresentationPreserveCompatible)
	})
	runTomlEdit(t, table, "toml.edit.replace-scalar-literal@1", func(b *toml.EditTransactionBuilder) {
		b.LiteralScalar(tomlEntryItemRef(t, table, 0), []byte("0x2A"))
	})
}

func TestYAMLOperationClosure(t *testing.T) {
	registry := yaml.NewFormatOperationRegistry(yaml.Yaml12CoreV1)
	ids := make([]string, 0, len(registry.Operations()))
	for _, operation := range registry.Operations() {
		ids = append(ids, operation.ID.ID()+"@"+uint64ClosureString(operation.ID.Version()))
	}
	assertOperationIDs(t, ids, []string{
		"yaml.edit.insert-alias@1", "yaml.edit.insert-mapping-entry@1",
		"yaml.edit.insert-sequence-element@1", "yaml.edit.remove-mapping-entry@1",
		"yaml.edit.remove-sequence-element@1", "yaml.edit.rename-anchor@1",
		"yaml.edit.replace-scalar-literal@1", "yaml.edit.replace-scalar-semantic@1",
	})

	// Removals run on an alias-free document: the Go anchor-dependency
	// check collects every document-owned node (a known Go/Rust corner
	// divergence surfaced by the differential harness, G2.4 finding), so a
	// document containing any alias rejects unrelated removals. The anchor
	// rows (insert-alias, rename-anchor) run on a dedicated anchored
	// document where the anchor precedes the sequence (the anchor is
	// visible at the insertion point; yaml.edit.anchor-not-visible@1
	// otherwise).
	plain := yamlParseForClosure(t, "b: [one, two]\na: 1\n")
	yamlDoc, _ := plain.Document(0)
	root := yamlDoc.Root()
	sequence, _ := root.MappingEntry(0)
	runYamlEdit(t, plain, "yaml.edit.insert-mapping-entry@1", func(b *yaml.EditTransactionBuilder) {
		b.InsertMappingEntry(root.NodeRef(), core.String("d"), core.Boolean(true), yaml.PlacementEnd())
	})
	runYamlEdit(t, plain, "yaml.edit.insert-sequence-element@1", func(b *yaml.EditTransactionBuilder) {
		b.InsertSequenceElement(sequence.Value().NodeRef(), core.String("three"), yaml.PlacementEnd())
	})
	runYamlEdit(t, plain, "yaml.edit.remove-mapping-entry@1", func(b *yaml.EditTransactionBuilder) {
		b.RemoveMappingEntry(yamlMappingEntryRef(t, plain, 1))
	})
	runYamlEdit(t, plain, "yaml.edit.remove-sequence-element@1", func(b *yaml.EditTransactionBuilder) {
		b.RemoveSequenceElement(yamlSequenceElementRef(t, plain, 0, 0))
	})
	runYamlEdit(t, plain, "yaml.edit.replace-scalar-literal@1", func(b *yaml.EditTransactionBuilder) {
		b.LiteralScalar(yamlMappingValueRef(t, plain, 1), []byte("7"))
	})
	runYamlEdit(t, plain, "yaml.edit.replace-scalar-semantic@1", func(b *yaml.EditTransactionBuilder) {
		b.SemanticScalar(yamlMappingValueRef(t, plain, 1), core.NewInteger(newClosureInt(5)),
			yaml.RepresentationPolicyPreserveCompatible)
	})

	anchored := yamlParseForClosure(t, "c: &x three\nb: [one, two]\n")
	anchoredDoc, _ := anchored.Document(0)
	anchoredRoot := anchoredDoc.Root()
	anchorEntry, _ := anchoredRoot.MappingEntry(0)
	anchor, _ := anchorEntry.Value().AnchorNodeRef()
	anchoredSequence, _ := anchoredRoot.MappingEntry(1)
	runYamlEdit(t, anchored, "yaml.edit.insert-alias@1", func(b *yaml.EditTransactionBuilder) {
		b.InsertAlias(anchoredSequence.Value().NodeRef(), anchor, yaml.PlacementEnd())
	})
	runYamlEdit(t, anchored, "yaml.edit.rename-anchor@1", func(b *yaml.EditTransactionBuilder) {
		b.RenameAnchor(anchor, "y")
	})
}

func TestIniOperationClosure(t *testing.T) {
	registry := ini.NewFormatOperationRegistry(ini.PortableV1)
	ids := make([]string, 0, len(registry.Operations()))
	for _, operation := range registry.Operations() {
		ids = append(ids, operation.ID.ID()+"@"+uint64ClosureString(operation.ID.Version()))
	}
	// The INI registry publishes its descriptors in canonical operation-ID
	// order (alphabetical; operation_registry.go sorts by ID).
	assertOperationIDs(t, ids, []string{
		"ini.edit.insert-entry@1", "ini.edit.insert-section@1",
		"ini.edit.remove-entry@1", "ini.edit.remove-section@1",
		"ini.edit.rename-entry@1", "ini.edit.rename-section@1",
		"ini.edit.replace-literal-value@1", "ini.edit.replace-semantic-value@1",
	})

	single := iniParseForClosure(t, "[s]\na=1\n")
	twoSections := iniParseForClosure(t, "[s]\na=1\n[t]\nb=2\n")
	runIniEdit(t, single, "ini.edit.insert-section@1", func(b *ini.EditTransactionBuilder) {
		b.InsertSection(single.NodeRef(), "t", ini.PlacementEnd())
	})
	runIniEdit(t, twoSections, "ini.edit.remove-section@1", func(b *ini.EditTransactionBuilder) {
		b.RemoveSection(iniSectionRef(t, twoSections, 1))
	})
	runIniEdit(t, twoSections, "ini.edit.rename-section@1", func(b *ini.EditTransactionBuilder) {
		b.RenameSection(iniSectionRef(t, twoSections, 0), "u")
	})
	runIniEdit(t, single, "ini.edit.insert-entry@1", func(b *ini.EditTransactionBuilder) {
		b.InsertEntry(iniSectionRef(t, single, 0), "b", "two", ini.PlacementEnd())
	})
	runIniEdit(t, single, "ini.edit.remove-entry@1", func(b *ini.EditTransactionBuilder) {
		b.RemoveEntry(iniEntryRef(t, single, 0))
	})
	runIniEdit(t, single, "ini.edit.rename-entry@1", func(b *ini.EditTransactionBuilder) {
		b.RenameEntry(iniEntryRef(t, single, 0), "z")
	})
	runIniEdit(t, single, "ini.edit.replace-semantic-value@1", func(b *ini.EditTransactionBuilder) {
		b.SemanticValue(iniEntryRef(t, single, 0), "5", ini.RepresentationPolicyPreserveCompatible)
	})
	runIniEdit(t, single, "ini.edit.replace-literal-value@1", func(b *ini.EditTransactionBuilder) {
		b.LiteralValue(iniEntryRef(t, single, 0), []byte("7"))
	})
}

func TestPropertiesOperationClosure(t *testing.T) {
	registry := properties.NewFormatOperationRegistry(properties.PropertiesReaderV1)
	ids := make([]string, 0, len(registry.Operations()))
	for _, operation := range registry.Operations() {
		ids = append(ids, operation.ID.ID()+"@"+uint64ClosureString(operation.ID.Version()))
	}
	assertOperationIDs(t, ids, []string{
		"java-properties.edit.insert-property@1", "java-properties.edit.remove-property@1",
		"java-properties.edit.rename-property@1", "java-properties.edit.replace-literal-value@1",
		"java-properties.edit.replace-semantic-value@1",
	})

	doc := propertiesParseForClosure(t, "a=1\nb=2\n")
	runPropertiesEdit(t, doc, "java-properties.edit.insert-property@1", func(b *properties.EditTransactionBuilder) {
		b.InsertProperty(doc.NodeRef(), properties.NewJavaStringFromUnicode("c"),
			properties.NewJavaStringFromUnicode("3"), properties.PlacementEnd())
	})
	runPropertiesEdit(t, doc, "java-properties.edit.remove-property@1", func(b *properties.EditTransactionBuilder) {
		b.RemoveProperty(propertiesPropertyRef(t, doc, 0))
	})
	runPropertiesEdit(t, doc, "java-properties.edit.rename-property@1", func(b *properties.EditTransactionBuilder) {
		b.RenameProperty(propertiesPropertyRef(t, doc, 0), properties.NewJavaStringFromUnicode("z"))
	})
	runPropertiesEdit(t, doc, "java-properties.edit.replace-literal-value@1", func(b *properties.EditTransactionBuilder) {
		b.LiteralValue(propertiesPropertyRef(t, doc, 0), []byte("7"))
	})
	runPropertiesEdit(t, doc, "java-properties.edit.replace-semantic-value@1", func(b *properties.EditTransactionBuilder) {
		b.SemanticValue(propertiesPropertyRef(t, doc, 0), properties.NewJavaStringFromUnicode("5"))
	})
}

// TestConvertCompositionFace reports the current convert composition
// surface: json<->toml only (exercised here and by convert_face_test.go);
// the yaml/ini/properties convert composition is not published by the
// root package in 0.16.0 (full-format convert composition is planned for
// 0.18.0 G4.2). The compilation of this test pins the json/toml face.
func TestConvertCompositionFace(t *testing.T) {
	source, failure := json.Parse(context.Background(), []byte(`{"a":1}`),
		json.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("json parse: %v", failure)
	}
	request, requestFailure := json.NewProjectionRequestBuilder(json.ProjectionTargetBestExactCoreV1).Build()
	if requestFailure != nil {
		t.Fatalf("projection request: %v", requestFailure)
	}
	converted := consema.ConvertJSON(source, request, document.NewMaterializationRequest(
		toml.Toml10V1.ID(), document.NewMaterializationStyleId("toml.canonical-document", 1)))
	if converted.Failed != nil {
		t.Fatalf("json->toml convert failed: %v", converted.Failed)
	}
	if !strings.Contains(string(converted.Complete.Document.Render()), "a") {
		t.Fatalf("converted render %q", converted.Complete.Document.Render())
	}
	convertedToml, ok := converted.Complete.Document.AsTOML()
	if !ok {
		t.Fatalf("json->toml convert did not produce a TOML document")
	}
	back := consema.ConvertTOML(convertedToml,
		toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1),
		document.NewMaterializationRequest(json.JsonProfileStrictV1.ID(),
			document.NewMaterializationStyleId("json.canonical-compact", 1)))
	if back.Failed != nil {
		t.Fatalf("toml->json convert failed: %v", back.Failed)
	}
	if !strings.Contains(string(back.Complete.Document.Render()), "a") {
		t.Fatalf("round-trip render %q", back.Complete.Document.Render())
	}
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

func assertOperationIDs(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("operation count = %d, want %d (%v)", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("operation %d = %q, want %q", index, got[index], want[index])
		}
	}
}

func newClosureInt(value int64) *big.Int { return big.NewInt(value) }

func uint64ClosureString(value uint32) string {
	return strconv.FormatUint(uint64(value), 10)
}

func jsonParseForClosure(t *testing.T, source string) *json.Document {
	t.Helper()
	doc, failure := json.Parse(context.Background(), []byte(source), json.JsonProfileStrictV1,
		document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("json parse %q: %v", source, failure)
	}
	return doc
}

func jsonMemberRef(t *testing.T, doc *json.Document, ordinal int) document.NodeRef {
	t.Helper()
	members := doc.Root().ObjectMembers()
	if !members.IsAvailable() || ordinal >= len(members.Value()) {
		t.Fatalf("member %d unavailable", ordinal)
	}
	return members.Value()[ordinal].NodeRef()
}

func jsonMemberValueRef(t *testing.T, doc *json.Document, ordinal int) document.NodeRef {
	t.Helper()
	members := doc.Root().ObjectMembers()
	if !members.IsAvailable() || ordinal >= len(members.Value()) {
		t.Fatalf("member %d unavailable", ordinal)
	}
	return members.Value()[ordinal].ValueNodeRef()
}

func jsonArrayElementRef(t *testing.T, doc *json.Document, ordinal int) document.NodeRef {
	t.Helper()
	elements := doc.Root().ArrayElements()
	if !elements.IsAvailable() || ordinal >= len(elements.Value()) {
		t.Fatalf("element %d unavailable", ordinal)
	}
	return elements.Value()[ordinal].NodeRef()
}

func runJSONEdit(t *testing.T, doc *json.Document, id string,
	apply func(*json.EditTransactionBuilder)) {
	t.Helper()
	builder := json.NewEditTransactionBuilder(doc)
	apply(builder)
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("%s: commit failed: %s", id, failure.Code())
	}
	if commit == nil || len(commit.ChangeSet.SourceEdits()) == 0 {
		t.Fatalf("%s: commit produced no source edits", id)
	}
}

func tomlParseForClosure(t *testing.T, source string) *toml.Document {
	t.Helper()
	doc, failure := toml.Parse([]byte(source), toml.Toml10V1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("toml parse %q: %v", source, failure)
	}
	return doc
}

func tomlEntryRef(t *testing.T, doc *toml.Document, ordinal int) document.NodeRef {
	t.Helper()
	entries, ok := doc.Root().TableEntries()
	if !ok || ordinal >= len(entries) {
		t.Fatalf("entry %d unavailable", ordinal)
	}
	return entries[ordinal].NodeRef()
}

func tomlEntryItemRef(t *testing.T, doc *toml.Document, ordinal int) document.NodeRef {
	t.Helper()
	entries, ok := doc.Root().TableEntries()
	if !ok || ordinal >= len(entries) {
		t.Fatalf("entry %d unavailable", ordinal)
	}
	return entries[ordinal].ItemNodeRef()
}

func tomlArrayElementRef(t *testing.T, doc *toml.Document, ordinal int) document.NodeRef {
	t.Helper()
	entries, ok := doc.Root().TableEntries()
	if !ok || len(entries) == 0 {
		t.Fatalf("entry unavailable")
	}
	item := entries[0].Item()
	elements, ok := item.ArrayElements()
	if !ok || ordinal >= len(elements) {
		t.Fatalf("element %d unavailable", ordinal)
	}
	return elements[ordinal].NodeRef()
}

func runTomlEdit(t *testing.T, doc *toml.Document, id string,
	apply func(*toml.EditTransactionBuilder)) {
	t.Helper()
	builder := toml.NewEditTransactionBuilder(doc)
	apply(builder)
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("%s: commit failed: %s", id, failure.Code())
	}
	if commit == nil || len(commit.ChangeSet.SourceEdits()) == 0 {
		t.Fatalf("%s: commit produced no source edits", id)
	}
}

func yamlParseForClosure(t *testing.T, source string) *yaml.Document {
	t.Helper()
	doc, failure := yaml.Parse([]byte(source), yaml.Yaml12CoreV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("yaml parse %q: %v", source, failure)
	}
	return doc
}

func yamlMappingEntryRef(t *testing.T, doc *yaml.Document, ordinal int) document.NodeRef {
	t.Helper()
	yamlDoc, ok := doc.Document(0)
	if !ok {
		t.Fatalf("document 0 missing")
	}
	entry, ok := yamlDoc.Root().MappingEntry(ordinal)
	if !ok {
		t.Fatalf("mapping entry %d unavailable", ordinal)
	}
	return entry.NodeRef()
}

func yamlMappingValueRef(t *testing.T, doc *yaml.Document, ordinal int) document.NodeRef {
	t.Helper()
	yamlDoc, ok := doc.Document(0)
	if !ok {
		t.Fatalf("document 0 missing")
	}
	entry, ok := yamlDoc.Root().MappingEntry(ordinal)
	if !ok {
		t.Fatalf("mapping entry %d unavailable", ordinal)
	}
	return entry.Value().NodeRef()
}

func yamlSequenceElementRef(t *testing.T, doc *yaml.Document, entryOrdinal, elementOrdinal int) document.NodeRef {
	t.Helper()
	yamlDoc, ok := doc.Document(0)
	if !ok {
		t.Fatalf("document 0 missing")
	}
	entry, ok := yamlDoc.Root().MappingEntry(entryOrdinal)
	if !ok {
		t.Fatalf("sequence mapping entry unavailable")
	}
	item, ok := entry.Value().SequenceItem(elementOrdinal)
	if !ok {
		t.Fatalf("sequence element %d unavailable", elementOrdinal)
	}
	return item.NodeRef()
}

func runYamlEdit(t *testing.T, doc *yaml.Document, id string,
	apply func(*yaml.EditTransactionBuilder)) {
	t.Helper()
	builder := yaml.NewEditTransactionBuilder(doc)
	apply(builder)
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("%s: commit failed: %s", id, failure.Code())
	}
	if commit == nil || len(commit.ChangeSet.SourceEdits()) == 0 {
		t.Fatalf("%s: commit produced no source edits", id)
	}
}

func iniParseForClosure(t *testing.T, source string) *ini.Document {
	t.Helper()
	doc, failure := ini.Parse([]byte(source), ini.PortableV1, ini.IniEncodingProfileDefault(),
		ini.DefaultIniParseLimits())
	if failure != nil {
		t.Fatalf("ini parse %q: %v", source, failure)
	}
	return doc
}

func iniSectionRef(t *testing.T, doc *ini.Document, ordinal int) document.NodeRef {
	t.Helper()
	sections := doc.Sections()
	if ordinal >= len(sections) {
		t.Fatalf("section %d unavailable", ordinal)
	}
	return sections[ordinal].NodeRef()
}

func iniEntryRef(t *testing.T, doc *ini.Document, ordinal int) document.NodeRef {
	t.Helper()
	entries := doc.Entries()
	if ordinal >= len(entries) {
		t.Fatalf("entry %d unavailable", ordinal)
	}
	return entries[ordinal].NodeRef()
}

func runIniEdit(t *testing.T, doc *ini.Document, id string,
	apply func(*ini.EditTransactionBuilder)) {
	t.Helper()
	builder := ini.NewEditTransactionBuilder(doc)
	apply(builder)
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("%s: commit failed: %s", id, failure.Code())
	}
	if commit == nil || len(commit.ChangeSet.SourceEdits()) == 0 {
		t.Fatalf("%s: commit produced no source edits", id)
	}
}

func propertiesParseForClosure(t *testing.T, source string) *properties.Document {
	t.Helper()
	doc, failure := properties.ParseReader([]byte(source), document.Utf8Encoding(),
		properties.DefaultPropertiesParseLimits())
	if failure != nil {
		t.Fatalf("properties parse %q: %v", source, failure)
	}
	return doc
}

func propertiesPropertyRef(t *testing.T, doc *properties.Document, ordinal int) document.NodeRef {
	t.Helper()
	properties := doc.Properties()
	if ordinal >= len(properties) {
		t.Fatalf("property %d unavailable", ordinal)
	}
	return properties[ordinal].NodeRef()
}

func runPropertiesEdit(t *testing.T, doc *properties.Document, id string,
	apply func(*properties.EditTransactionBuilder)) {
	t.Helper()
	builder := properties.NewEditTransactionBuilder(doc)
	apply(builder)
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("%s: commit failed: %s", id, failure.Code())
	}
	if commit == nil || len(commit.ChangeSet.SourceEdits()) == 0 {
		t.Fatalf("%s: commit produced no source edits", id)
	}
}
