package main

import (
	"testing"

	consema "consema.dev/consema"
	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/ini"
	"consema.dev/consema/protocol"
)

// editRequestFixture builds the canonical bytes of one cli.edit-request@1
// wrapper with a single replace-semantic-value operation (standalone, no
// *testing.T dependency).
func editRequestFixture(section, key, value string) []byte {
	request := editRequestOperation(section, key, value, "ini.edit.replace-semantic-value")
	return request
}

// editRequestOperation builds one strict edit request with the given
// operation id and replace-semantic-value-shaped arguments.
func editRequestOperation(section, key, value, operationID string) []byte {
	reference, _ := coreNewObject(
		coreEntry("id", coreString(operationID)),
		coreEntry("version", coreInteger(1)),
	)
	var sectionValue core.Value = core.NullValue()
	if section != "" {
		sectionValue = coreString(section)
	}
	target, _ := coreNewObject(
		coreEntry("kind", coreString("entry")),
		coreEntry("section", sectionValue),
		coreEntry("key", coreString(key)),
		coreEntry("occurrence", coreInteger(0)),
	)
	arguments, _ := coreNewObject(
		coreEntry("value", coreString(value)),
		coreEntry("representation_policy", coreString("preserve-compatible")),
	)
	operation, _ := coreNewObject(
		coreEntry("operation", reference),
		coreEntry("target", target),
		coreEntry("arguments", arguments),
	)
	wrapper, _ := coreNewObject(
		coreEntry("schema", coreString(editRequestSchema)),
		coreEntry("operations", coreNewArray(operation)),
	)
	bytes, err := protocol.EncodeJSON(wrapper, protocolLimits())
	if err != nil {
		panic(err)
	}
	return bytes
}

func coreEntry(key string, value core.Value) core.Entry {
	return core.Entry{Key: key, Value: value}
}

func coreNewObject(entries ...core.Entry) (core.Value, error) {
	return core.NewObject(entries...)
}

func coreNewArray(items ...core.Value) core.Value {
	return core.NewArray(items...)
}

// editRequestWithUnknownOperation builds one request whose operation id is
// not published by the ini.portable profile registry.
func editRequestWithUnknownOperation(t *testing.T) []byte {
	t.Helper()
	return editRequestOperation("db", "port", "9090", "ini.edit.set-entry-value")
}

// plannedIniEntry builds one planned batch-plan entry for the standard INI
// fixture (replace `port` bytes with `9090`); with tamper the replacement
// offsets are shifted so the original-bytes precondition fails.
func plannedIniEntry(t *testing.T, path string, tamper bool) *protocol.BatchPlanFileEntry {
	t.Helper()
	doc, failure := consema.ParseINI(iniSource(), ini.PortableV1,
		ini.IniEncodingProfileDefault(), ini.DefaultIniParseLimits())
	if failure != nil {
		t.Fatalf("parse: %v", failure)
	}
	iniDoc, ok := doc.AsINI()
	if !ok {
		t.Fatal("not an INI document")
	}
	builder := ini.NewEditTransactionBuilder(iniDoc)
	target := findEntry(t, iniDoc, "db", "port")
	builder.SemanticValue(target, "9090", ini.RepresentationPolicyPreserveCompatible)
	plan, editFailure := iniDoc.DryRun(builder.Build(), "source:one")
	if editFailure != nil {
		t.Fatalf("dry run: %v", editFailure)
	}
	entry, err := consema.PlanFileEntryFromEditPlan(path, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !tamper {
		return entry
	}
	// A structurally valid patch whose original bytes do not match the base
	// at the (shifted) offset — the RFC 0015 §9.3 step-2 precondition
	// failure.
	patch := entry.SourcePatch()
	replacements := make([]protocol.SourceReplacement, 0, len(patch.Replacements))
	for _, replacement := range patch.Replacements {
		replacements = append(replacements, protocol.SourceReplacement{
			OldStart:          replacement.OldStart + 1,
			OldEnd:            replacement.OldEnd + 1,
			Original:          replacement.Original,
			Replacement:       replacement.Replacement,
			RedactOriginal:    replacement.RedactOriginal,
			RedactReplacement: replacement.RedactReplacement,
		})
	}
	tampered := &protocol.SourcePatch{
		BaseDigest:   patch.BaseDigest,
		TargetDigest: patch.TargetDigest,
		Encoding:     patch.Encoding,
		Replacements: replacements,
		Metadata:     patch.Metadata,
	}
	sourceDigest := patch.BaseDigest
	tamperedEntry, err := protocol.NewBatchPlanFileEntry(path, protocol.PlanStatusPlanned,
		entry.Profile(), &sourceDigest, entry.Operations(), tampered, nil, nil,
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		t.Fatal(err)
	}
	return tamperedEntry
}

func findEntry(t *testing.T, doc *ini.Document, section, key string) document.NodeRef {
	t.Helper()
	for _, entry := range doc.Entries() {
		entrySection, ok := doc.Section(entry.Section())
		if !ok {
			t.Fatal("unresolvable section")
		}
		if entrySection.Name() == section && entry.Key() == key {
			return entry.NodeRef()
		}
	}
	t.Fatalf("entry %s.%s not found", section, key)
	return document.NodeRef{}
}

// applyPlanOf builds one plan manifest with planned entries for the given
// source paths (an empty path means "no entry").
func applyPlanOf(t *testing.T, a, b string, tamper bool) *protocol.BatchPlanMessage {
	t.Helper()
	var entries []*protocol.BatchPlanFileEntry
	if a != "" {
		entries = append(entries, plannedIniEntry(t, a, tamper))
	}
	if b != "" {
		entries = append(entries, plannedIniEntry(t, b, tamper))
	}
	plan, err := protocol.NewBatchPlanMessage(productVersion, entries)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
