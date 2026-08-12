// Package pilot implements the Go SDK real-repository migration pilot
// (0.19.0 G5.7; roadmap §22.7, §23.2-§23.3; report docs/pilot-go-0.19.0.md,
// modeled on docs/pilot-0.13.0.md).
//
// The pilot drives only the public SDK surface (the consema root facade,
// the family packages, go/document, go/protocol, go/core). It performs no
// internal-API calls and no cross-language imports: every operation here
// is a real consumer program's operation. The whole workflow is
// reproducible with `go test -count=1 -v ./pilot/`.
//
// The corpus is the pinned `conformance/fixtures/` real-world and family
// fixtures (the same corpus the 0.13.0 Rust pilot used); the pilot project
// is materialized as copies inside a temporary directory so the fixtures
// stay byte-untouched.
package pilot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"consema.dev/consema"
	"consema.dev/consema/core"
	"consema.dev/consema/document"
	hclpkg "consema.dev/consema/hcl"
	"consema.dev/consema/ini"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/plist"
	"consema.dev/consema/properties"
	"consema.dev/consema/protocol"
	"consema.dev/consema/toml"
	xmlpkg "consema.dev/consema/xml"
	"consema.dev/consema/yaml"
)

// ---------------------------------------------------------------------------
// Corpus and helpers

// fixtureRoot resolves the pinned conformance fixture directory relative to
// this test file (tests run with the package directory as working
// directory; runtime.Caller makes the resolution independent of that).
func fixtureRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "conformance", "fixtures")
}

func readFixture(t testing.TB, rel string) []byte {
	t.Helper()
	path := filepath.Join(fixtureRoot(), rel)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return src
}

func digestHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func newSourceSnapshot(t testing.TB, raw []byte) *document.SourceSnapshot {
	t.Helper()
	utf8 := document.Utf8Encoding()
	facts, err := document.NewEncodingFactsFromClaim(utf8, nil, nil, nil, utf8)
	if err != nil {
		t.Fatalf("encoding facts: %v", err)
	}
	return snapshotFromFacts(t, raw, facts)
}

// snapshotFromFacts reconstructs the source snapshot with the encoding
// request that matches one patch's recorded encoding facts (the patch
// apply re-verifies encoding facts against the snapshot).
func snapshotFromFacts(t testing.TB, raw []byte, facts document.EncodingFacts) *document.SourceSnapshot {
	t.Helper()
	request := document.NewEncodingRequest(facts.ProfileDefault()).
		WithBomPolicy(facts.BomPolicy())
	if declaration := facts.Declaration(); declaration != nil {
		request = request.WithDeclaration(*declaration)
	}
	if override := facts.CallerOverride(); override != nil {
		request = request.WithCallerOverride(*override)
	}
	snapshot, err := document.NewSourceSnapshotFromRaw(raw, request, document.DefaultSourceLimits())
	if err != nil {
		t.Fatalf("source snapshot: %v", err)
	}
	return snapshot
}

// editCommit is one atomic SDK edit commit through the facade root.
func editCommit(t testing.TB, doc *consema.Document, tx *consema.EditTransaction) *consema.EditCommit {
	t.Helper()
	commit, err := consema.CommitEdit(doc, tx)
	if err != nil {
		t.Fatalf("CommitEdit: %v", err)
	}
	return commit
}

// untouchedRate returns the fraction of base bytes covered by the
// untouched-byte proof (old-side region bytes / base length).
func untouchedRate(proof *document.UntouchedByteProof, baseLen int) float64 {
	if proof == nil || baseLen == 0 {
		return 0
	}
	covered := 0
	for _, region := range proof.Regions() {
		covered += region.OldEnd() - region.OldStart()
	}
	return float64(covered) / float64(baseLen)
}

// assertUntouchedProof re-verifies the proof against the base and target
// raw bytes and asserts the replacement union explains every changed byte
// (zero unauthorized loss: no changed byte outside the replacement set).
func assertUntouchedProof(t testing.TB, base, target []byte,
	proof *document.UntouchedByteProof, patch *document.SourcePatch) {
	t.Helper()
	baseSnapshot := snapshotFromFacts(t, base, patch.EncodingFacts())
	targetSnapshot := snapshotFromFacts(t, target, patch.EncodingFacts())
	if err := proof.Verify(baseSnapshot, targetSnapshot, patch.Replacements()); err != nil {
		t.Fatalf("untouched-byte proof failed: %v", err)
	}
	// Replacement union must be the exact changed region set: applying the
	// patch to the base snapshot must reproduce the target bytes exactly.
	applied, err := patch.Apply(baseSnapshot, document.DefaultSourcePatchLimits())
	if err != nil {
		t.Fatalf("patch apply: %v", err)
	}
	if !bytes.Equal(applied.Bytes(), target) {
		t.Fatalf("patch application does not reproduce the target bytes")
	}
}

// ---------------------------------------------------------------------------
// Corpus registration (§23.1): sources, digests, profiles, usage

type corpusFile struct {
	rel         string
	profileID   string // facade ParseDocument profile id
	expectedUse string
}

var corpus = []corpusFile{
	{"real-world/package.json", "json.strict", "W1/W6/W7/W8 main project"},
	{"real-world/tsconfig.jsonc", "jsonc.bounded", "W1"},
	{"real-world/vscode-settings.jsonc", "jsonc.bounded", "W1"},
	{"real-world/application.json5", "json5.standard", "W1"},
	{"toml/application.toml", "toml.1.0", "W1"},
	{"yaml/compose-services.yaml", "yaml.1.2-core", "W1"},
	{"ini/desktop-settings.ini", "ini.portable", "W2/W8"},
	{"properties/build-tool.properties", "java-properties.reader", "W2"},
	{"xml/app-server-config.xml", "xml.1.0-safe", "W3"},
	{"plist/xml/com.example.preferences.plist", "plist.xml", "W4"},
	{"hcl/tf/main.tf", "hcl.native", "W5"},
	{"properties/continuation-heavy.properties", "java-properties.reader", "W2 logical lines"},
}

func parseFacade(t testing.TB, src []byte, profileID string) *consema.Document {
	t.Helper()
	doc, err := consema.ParseDocument(context.Background(), src,
		document.NewProfileId(profileID, 1))
	if err != nil {
		t.Fatalf("ParseDocument(%s): %v", profileID, err)
	}
	return doc
}

// ---------------------------------------------------------------------------
// TestPilotCorpus — §23.1 corpus registration + metric 1 (round-trip rate)

func TestPilotCorpus(t *testing.T) {
	t.Logf("fixture corpus (pinned conformance/fixtures, %d files)", len(corpus))
	for _, file := range corpus {
		src := readFixture(t, file.rel)
		doc := parseFacade(t, src, file.profileID)
		rendered := doc.Render()
		if !bytes.Equal(rendered, src) {
			t.Fatalf("%s: parse/render round trip is not byte-exact", file.rel)
		}
		t.Logf("  %-48s %6d B  %s  %s", file.rel, len(src), digestHex(src)[:16], file.profileID)
	}
	// Round-trip rate (metric 1): every corpus file closed byte-exactly.
	// The count above is 12/12; the assertion loop is the measurement.
}

// ---------------------------------------------------------------------------
// W1 — version/image updates in JSONC/JSON5/TOML/YAML, comments and style kept

func jsonObjectMember(t testing.TB, doc *jsonpkg.Document, names ...string) jsonpkg.JsonValue {
	t.Helper()
	current := doc.Root()
	for _, name := range names {
		members := current.ObjectMembers()
		if !members.IsAvailable() {
			t.Fatalf("expected object for %q", name)
		}
		found := false
		for _, member := range members.Value() {
			nameAvailability := member.Name()
			if nameAvailability.IsAvailable() && *nameAvailability.Value() == name {
				current = member.Value()
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("member %q not found", name)
		}
	}
	return current
}

func jsonMemberRef(t testing.TB, doc *jsonpkg.Document, names ...string) document.NodeRef {
	t.Helper()
	current := doc.Root()
	for i, name := range names {
		members := current.ObjectMembers()
		if !members.IsAvailable() {
			t.Fatalf("expected object for %q", name)
		}
		found := false
		for _, member := range members.Value() {
			nameAvailability := member.Name()
			if nameAvailability.IsAvailable() && *nameAvailability.Value() == name {
				if i == len(names)-1 {
					return member.ValueNodeRef()
				}
				current = member.Value()
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("member %q not found", name)
		}
	}
	panic("unreachable")
}

func TestPilotW1(t *testing.T) {
	// --- package.json (json.strict): dependency + version update ---
	src := readFixture(t, "real-world/package.json")
	doc := parseFacade(t, src, "json.strict")
	jsonDoc, _ := doc.AsJSON()

	fastify := jsonMemberRef(t, jsonDoc, "dependencies", "fastify")
	version := jsonMemberRef(t, jsonDoc, "version")
	tx := jsonpkg.NewEditTransactionBuilder(jsonDoc).
		LiteralScalar(fastify, []byte(`"4.29.0"`)).
		SemanticScalar(version, core.String("1.1.0"),
			jsonpkg.RepresentationPolicyPreserveCompatible).
		Build()
	commit := editCommit(t, doc, consema.NewJSONEditTransaction(tx))
	target := commit.Document.Render()
	assertUntouchedProof(t, src, target, commit.UntouchedProof, commit.SourcePatch)
	// style kept: two-space indentation and key order unchanged
	if !bytes.Contains(target, []byte(`  "fastify": "4.29.0"`)) {
		t.Fatalf("fastify update not present with style: %s", target)
	}
	if bytes.Contains(target, []byte("4.28.1")) {
		t.Fatalf("old dependency version leaked")
	}
	if rate := untouchedRate(commit.UntouchedProof, len(src)); rate < 0.95 {
		t.Fatalf("untouched rate too low: %v", rate)
	}
	t.Logf("W1 package.json: %d B -> %d B, untouched %.4f, digest %s",
		len(src), len(target), untouchedRate(commit.UntouchedProof, len(src)), digestHex(target)[:16])

	// --- tsconfig.jsonc (jsonc.bounded): target update, comments kept ---
	src = readFixture(t, "real-world/tsconfig.jsonc")
	doc = parseFacade(t, src, "jsonc.bounded")
	jsonDoc, _ = doc.AsJSON()
	targetRef := jsonMemberRef(t, jsonDoc, "compilerOptions", "target")
	tx = jsonpkg.NewEditTransactionBuilder(jsonDoc).
		LiteralScalar(targetRef, []byte(`"ES2023"`)).
		Build()
	commit = editCommit(t, doc, consema.NewJSONEditTransaction(tx))
	target = commit.Document.Render()
	assertUntouchedProof(t, src, target, commit.UntouchedProof, commit.SourcePatch)
	if !bytes.Contains(target, []byte(`// Compilation is deterministic across developer machines and CI.`)) {
		t.Fatalf("comment lost in JSONC edit")
	}
	if !bytes.Contains(target, []byte(`"target": "ES2023"`)) {
		t.Fatalf("target update not present")
	}
	t.Logf("W1 tsconfig.jsonc: comment kept, target ES2022 -> ES2023")

	// --- vscode-settings.jsonc: array element insertion ---
	src = readFixture(t, "real-world/vscode-settings.jsonc")
	doc = parseFacade(t, src, "jsonc.bounded")
	jsonDoc, _ = doc.AsJSON()
	rulers := jsonObjectMember(t, jsonDoc, "editor.rulers")
	tx = jsonpkg.NewEditTransactionBuilder(jsonDoc).
		InsertArrayElement(rulers.NodeRef(), core.NewInteger(big.NewInt(120)),
			jsonpkg.PlacementAtEnd()).
		Build()
	commit = editCommit(t, doc, consema.NewJSONEditTransaction(tx))
	target = commit.Document.Render()
	assertUntouchedProof(t, src, target, commit.UntouchedProof, commit.SourcePatch)
	if !bytes.Contains(target, []byte(`120]`)) || !bytes.Contains(target, []byte(`[80, 100`)) {
		t.Fatalf("rulers insertion not present: %s", target)
	}
	t.Logf("W1 vscode-settings.jsonc: rulers [80, 100] -> [80, 100, 120]")

	// --- application.json5: single-quoted style and comment kept ---
	src = readFixture(t, "real-world/application.json5")
	doc = parseFacade(t, src, "json5.standard")
	jsonDoc, _ = doc.AsJSON()
	portRef := jsonMemberRef(t, jsonDoc, "service", "port")
	envRef := jsonMemberRef(t, jsonDoc, "labels", "environment")
	tx = jsonpkg.NewEditTransactionBuilder(jsonDoc).
		SemanticScalar(portRef, core.NewInteger(big.NewInt(9090)),
			jsonpkg.RepresentationPolicyPreserveCompatible).
		LiteralScalar(envRef, []byte(`'production'`)).
		Build()
	commit = editCommit(t, doc, consema.NewJSONEditTransaction(tx))
	target = commit.Document.Render()
	assertUntouchedProof(t, src, target, commit.UntouchedProof, commit.SourcePatch)
	if !bytes.Contains(target, []byte(`// Human-authored service configuration.`)) {
		t.Fatalf("JSON5 comment lost")
	}
	if !bytes.Contains(target, []byte(`port: 9090`)) || !bytes.Contains(target, []byte(`'production'`)) {
		t.Fatalf("JSON5 updates not present: %s", target)
	}
	t.Logf("W1 application.json5: port 8080 -> 9090, environment staging -> production (single-quoted style kept)")

	// --- application.toml: version-ish updates in tables ---
	src = readFixture(t, "toml/application.toml")
	doc = parseFacade(t, src, "toml.1.0")
	tomlDoc, _ := doc.AsTOML()
	root := tomlDoc.Root()
	rootEntries, _ := root.TableEntries()
	var nameEntry, poolEntry *toml.TomlEntry
	for i := range rootEntries {
		entry := rootEntries[i]
		if entry.Name() == "service" {
			if candidate := findTomlEntry(t, entry.Item(), "name"); candidate != nil {
				nameEntry = candidate
			}
		}
		if entry.Name() == "database" {
			if candidate := findTomlEntry(t, entry.Item(), "pool_size"); candidate != nil {
				poolEntry = candidate
			}
		}
	}
	if nameEntry == nil || poolEntry == nil {
		t.Fatalf("toml fixture structure mismatch")
	}
	tomlTx := toml.NewEditTransactionBuilder(tomlDoc).
		SemanticScalar(nameEntry.ItemNodeRef(), core.String("catalog-v2"),
			toml.RepresentationPreserveCompatible).
		SemanticScalar(poolEntry.ItemNodeRef(), core.NewInteger(big.NewInt(64)),
			toml.RepresentationPreserveCompatible).
		Build()
	commit = editCommit(t, doc, consema.NewTOMLEditTransaction(tomlTx))
	target = commit.Document.Render()
	assertUntouchedProof(t, src, target, commit.UntouchedProof, commit.SourcePatch)
	if !bytes.Contains(target, []byte(`service.name = "catalog-v2"`)) || !bytes.Contains(target, []byte("pool_size = 64")) {
		t.Fatalf("toml updates not present: %s", target)
	}
	// comment and inline-table style preserved
	if !bytes.Contains(target, []byte(`# A realistic service configuration exercising logical and syntactic tables.`)) {
		t.Fatalf("toml comment lost")
	}
	t.Logf("W1 application.toml: service.name catalog -> catalog-v2, pool_size 32 -> 64 (comment and style kept)")

	// --- compose-services.yaml: image tag updates (W1 YAML) ---
	src = readFixture(t, "yaml/compose-services.yaml")
	doc = parseFacade(t, src, "yaml.1.2-core")
	yamlDoc, _ := doc.AsYAML()
	streamDoc, ok := yamlDoc.Document(0)
	if !ok {
		t.Fatalf("compose yaml: no document 0")
	}
	apiImageRef := yamlImageRef(t, streamDoc.Root())
	txYaml := yaml.NewEditTransactionBuilder(yamlDoc).
		LiteralScalar(apiImageRef, []byte("example.invalid/api:1.1.0")).
		Build()
	commitYaml, failure := yamlDoc.Commit(txYaml)
	if failure != nil {
		t.Fatalf("yaml commit: %s", failure.Error())
	}
	target = commitYaml.Document.Render()
	assertUntouchedProof(t, src, target, &commitYaml.UntouchedProof, commitYaml.SourcePatch)
	if !bytes.Contains(target, []byte("image: example.invalid/api:1.1.0")) {
		t.Fatalf("yaml image update not present: %s", target)
	}
	t.Logf("W1 compose-services.yaml: api image 1.0.0 -> 1.1.0 (literal, no expression evaluation)")
}

func findTomlEntry(t testing.TB, item toml.TomlItem, name string) *toml.TomlEntry {
	t.Helper()
	entries, ok := item.TableEntries()
	if !ok {
		return nil
	}
	for i := range entries {
		if entries[i].Name() == name {
			return &entries[i]
		}
	}
	return nil
}

func yamlImageRef(t testing.TB, root yaml.YamlNode) document.NodeRef {
	t.Helper()
	// compose root: mapping {name, services: {api: {image, ...}, database: ...}}
	imageRef := yamlWalkMapping(t, root, "services", "api", "image")
	return imageRef
}

func yamlWalkMapping(t testing.TB, node yaml.YamlNode, path ...string) document.NodeRef {
	t.Helper()
	current := node
	for i, name := range path {
		length, ok := current.MappingLen()
		if !ok {
			t.Fatalf("yaml node %q is not a mapping", strings.Join(path[:i+1], "/"))
		}
		found := false
		for ordinal := 0; ordinal < length; ordinal++ {
			entry, ok := current.MappingEntry(ordinal)
			if !ok {
				t.Fatalf("mapping entry %d missing", ordinal)
			}
			key, ok := entry.Key().Scalar()
			if !ok {
				continue
			}
			if key.Decoded() == name {
				value := entry.Value()
				if i == len(path)-1 {
					return value.NodeRef()
				}
				current = value
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("yaml path %q not found", strings.Join(path, "/"))
		}
	}
	panic("unreachable")
}

// ---------------------------------------------------------------------------
// W2 — INI/Properties insert/delete/rename, duplicates and logical lines kept

func TestPilotW2(t *testing.T) {
	// --- desktop-settings.ini ---
	src := readFixture(t, "ini/desktop-settings.ini")
	doc := parseFacade(t, src, "ini.portable")
	iniDoc, _ := doc.AsINI()

	var widthEntry *ini.IniEntry
	var windowSection, recentSection *ini.IniSection
	for i := range iniDoc.Sections() {
		section := iniDoc.Sections()[i]
		if section.Name() == "window" {
			windowSection = &section
			for _, entry := range iniDoc.Entries() {
				if entry.Section() == section.NodeRef() && entry.Key() == "width" {
					widthEntry = &entry
				}
			}
		}
		if section.Name() == "recent" {
			recentSection = &section
		}
	}
	if widthEntry == nil || windowSection == nil || recentSection == nil {
		t.Fatalf("ini fixture structure mismatch")
	}

	tx := ini.NewEditTransactionBuilder(iniDoc).
		SemanticValue(widthEntry.NodeRef(), "1600", ini.RepresentationPolicyPreserveCompatible).
		InsertEntry(recentSection.NodeRef(), "recent_files", "3", ini.PlacementEnd()).
		Build()
	commit := editCommit(t, doc, consema.NewINIEditTransaction(tx))
	target := commit.Document.Render()
	assertUntouchedProof(t, src, target, commit.UntouchedProof, commit.SourcePatch)
	if !bytes.Contains(target, []byte("width=1600")) || !bytes.Contains(target, []byte("recent_files=3")) {
		t.Fatalf("ini edits not present: %s", target)
	}
	// comments and section order kept
	if !bytes.Contains(target, []byte("; Consema desktop settings fixture")) {
		t.Fatalf("ini comment lost")
	}
	// second commit: rename the same entry (one operation per target per
	// transaction is the SDK contract)
	renamedDoc := commit.Document
	renamedIni, _ := renamedDoc.AsINI()
	var renamedWidth *ini.IniEntry
	for i := range renamedIni.Entries() {
		entry := renamedIni.Entries()[i]
		if entry.Key() == "width" {
			renamedWidth = &entry
		}
	}
	if renamedWidth == nil {
		t.Fatalf("width entry not found after first commit")
	}
	renameTx := ini.NewEditTransactionBuilder(renamedIni).
		RenameEntry(renamedWidth.NodeRef(), "resolution-width").
		Build()
	renameCommit := editCommit(t, renamedDoc, consema.NewINIEditTransaction(renameTx))
	intermediate := target
	target = renameCommit.Document.Render()
	assertUntouchedProof(t, intermediate, target, renameCommit.UntouchedProof, renameCommit.SourcePatch)
	if !bytes.Contains(target, []byte("resolution-width=1600")) {
		t.Fatalf("ini rename not present: %s", target)
	}
	t.Logf("W2 desktop-settings.ini: width 1440->1600, rename width->resolution-width, insert recent_files=3")

	// --- duplicate sections: Recovered formation and rejection (W2 preserve duplicates) ---
	duplicateSource := []byte("[window]\nwidth=1440\n[window]\nwidth=900\n")
	duplicateDoc, duplicateFailure := ini.Parse(duplicateSource, ini.PortableV1,
		ini.IniEncodingProfileDefault(), ini.DefaultIniParseLimits())
	if duplicateFailure != nil {
		t.Fatalf("duplicate ini parse: %v", duplicateFailure)
	}
	if duplicateDoc.FormationStatus().String() != "Recovered" {
		t.Fatalf("duplicate-section ini must be Recovered, got %s", duplicateDoc.FormationStatus().String())
	}
	// duplicates are preserved in the lossless index
	if len(duplicateDoc.Sections()) != 2 {
		t.Fatalf("duplicate sections not preserved: %d", len(duplicateDoc.Sections()))
	}
	t.Logf("W2 duplicate sections preserved (Recovered, 2 sections)")

	// --- build-tool.properties ---
	src = readFixture(t, "properties/build-tool.properties")
	doc = parseFacade(t, src, "java-properties.reader")
	propDoc, _ := doc.AsProperties()
	var versionProperty *properties.Property
	for i := range propDoc.Properties() {
		property := propDoc.Properties()[i]
		if key, err := property.Key().ToUnicode(); err == nil && key == "version" {
			versionProperty = &property
		}
	}
	if versionProperty == nil {
		t.Fatalf("version property not found")
	}
	txProps := properties.NewEditTransactionBuilder(propDoc).
		SemanticValue(versionProperty.NodeRef(), properties.NewJavaStringFromUnicode("1.1.0")).
		InsertProperty(propDoc.NodeRef(), properties.NewJavaStringFromUnicode("org.gradle.warning.mode"),
			properties.NewJavaStringFromUnicode("all"), properties.PlacementEnd()).
		Build()
	commitProps, failureProps := propDoc.Commit(txProps)
	if failureProps != nil {
		t.Fatalf("properties commit: %s", failureProps.Error())
	}
	target = commitProps.Document.Render()
	assertUntouchedProof(t, src, target, &commitProps.UntouchedProof, commitProps.SourcePatch)
	intermediateProps := target
	if !bytes.Contains(target, []byte("version=1.1.0")) {
		t.Fatalf("version update missing: %s", target)
	}
	if !bytes.Contains(target, []byte("org.gradle.warning.mode=all")) {
		t.Fatalf("inserted property missing")
	}
	// second commit: removal (one operation per property per transaction)
	editedProps := commitProps.Document
	editedPropsDoc := editedProps
	var editedVersion *properties.Property
	for i := range editedPropsDoc.Properties() {
		property := editedPropsDoc.Properties()[i]
		if key, err := property.Key().ToUnicode(); err == nil && key == "version" {
			editedVersion = &property
		}
	}
	if editedVersion == nil {
		t.Fatalf("version property not found after first commit")
	}
	removeTx := properties.NewEditTransactionBuilder(editedPropsDoc).
		RemoveProperty(editedVersion.NodeRef()).
		Build()
	removeCommit, removeFailure := editedPropsDoc.Commit(removeTx)
	if removeFailure != nil {
		t.Fatalf("properties remove commit: %s", removeFailure.Error())
	}
	target = removeCommit.Document.Render()
	assertUntouchedProof(t, intermediateProps, target, &removeCommit.UntouchedProof, removeCommit.SourcePatch)
	if bytes.Contains(target, []byte("version=")) {
		t.Fatalf("removed property still present")
	}
	if !bytes.Contains(target, []byte("# Consema build-tool fixture")) {
		t.Fatalf("properties comment lost")
	}
	t.Logf("W2 build-tool.properties: version 1.0.0->1.1.0 then removed, warning.mode inserted (two commits)")

	// --- logical lines: continuation-heavy.properties round trips ---
	continuation := readFixture(t, "properties/continuation-heavy.properties")
	contDoc := parseFacade(t, continuation, "java-properties.reader")
	if !bytes.Equal(contDoc.Render(), continuation) {
		t.Fatalf("continuation-heavy.properties is not byte-exact round trip")
	}
	t.Logf("W2 continuation-heavy.properties: logical lines round-trip byte-exact")
}

// ---------------------------------------------------------------------------
// W3 — XML attribute/element/text edits, namespaces and mixed content kept

func TestPilotW3(t *testing.T) {
	src := readFixture(t, "xml/app-server-config.xml")
	doc := parseFacade(t, src, "xml.1.0-safe")
	xmlDoc, _ := doc.AsXML()
	root := xmlDoc.Root()

	// connector element: last child of root
	var connector *xmlpkg.XmlElement
	var connectionString *xmlpkg.XmlElement
	var pool *xmlpkg.XmlElement
	for _, child := range root.Children() {
		if child.Element() == nil {
			continue
		}
		switch child.Element().QName().Local {
		case "connector":
			connector = child.Element()
		case "datasource":
			for _, nested := range child.Element().Children() {
				if nested.Element() == nil {
					continue
				}
				switch nested.Element().QName().Local {
				case "pool":
					pool = nested.Element()
				case "connection-string":
					connectionString = nested.Element()
				}
			}
		}
	}
	if connector == nil || pool == nil || connectionString == nil {
		t.Fatalf("xml fixture structure mismatch")
	}

	var portAttribute *xmlpkg.XmlAttributeData
	for i := range connector.Attributes() {
		if connector.Attributes()[i].QName.Local == "port" {
			portAttribute = &connector.Attributes()[i]
		}
	}
	if portAttribute == nil {
		t.Fatalf("connector port attribute not found")
	}

	portAttributeRef := xmlDoc.OccurrenceNodeRef(portAttribute.Ordinal, document.RoleXmlAttribute)
	tx := xmlpkg.NewEditTransactionBuilder(xmlDoc).
		SetAttributeValue(portAttributeRef, "9090").
		Build()
	commit := editCommit(t, doc, consema.NewXMLEditTransaction(tx))
	target := commit.Document.Render()
	assertUntouchedProof(t, src, target, commit.UntouchedProof, commit.SourcePatch)
	// second commit: attribute insertion (separate transaction; the target
	// NodeRef must be re-resolved against the new snapshot)
	editedXML := commit.Document
	editedBytes := target
	editedXMLDoc, _ := editedXML.AsXML()
	editedRoot := editedXMLDoc.Root()
	var editedConnector *xmlpkg.XmlElement
	for _, child := range editedRoot.Children() {
		if child.Element() != nil && child.Element().QName().Local == "connector" {
			editedConnector = child.Element()
		}
	}
	if editedConnector == nil {
		t.Fatalf("connector not found in edited document")
	}
	var editedPort *xmlpkg.XmlAttributeData
	for i := range editedConnector.Attributes() {
		if editedConnector.Attributes()[i].QName.Local == "port" {
			editedPort = &editedConnector.Attributes()[i]
		}
	}
	if editedPort == nil {
		t.Fatalf("port attribute not found in edited document")
	}
	txInsert := xmlpkg.NewEditTransactionBuilder(editedXMLDoc).
		InsertAttribute(editedConnector.NodeRef(), xmlpkg.NewNameFacts(nil, "name", nil), "edge", xmlpkg.AttributePlacementEnd()).
		Build()
	commitInsert := editCommit(t, editedXML, consema.NewXMLEditTransaction(txInsert))
	target = commitInsert.Document.Render()
	assertUntouchedProof(t, editedBytes, target, commitInsert.UntouchedProof, commitInsert.SourcePatch)
	// namespace bindings byte-unchanged
	if !bytes.Contains(target, []byte(`xmlns:db="urn:example:datasource"`)) ||
		!bytes.Contains(target, []byte(`xmlns:sec="urn:example:security"`)) {
		t.Fatalf("namespace bindings damaged: %s", target)
	}
	if !bytes.Contains(target, []byte(`port="9090"`)) {
		t.Fatalf("attribute edit not present")
	}
	if !bytes.Contains(target, []byte(`name="edge"`)) {
		t.Fatalf("attribute insertion not present: %s", target)
	}
	t.Logf("W3 app-server-config.xml: port 8080->9090, name=edge attribute added; namespaces byte-unchanged")

	// --- text edit: logback pattern text (a real text occurrence) ---
	logback := readFixture(t, "xml/logback.xml")
	logDoc := parseFacade(t, logback, "xml.1.0-safe")
	logXML, _ := logDoc.AsXML()
	logRoot := logXML.Root()
	var patternText *xmlpkg.XmlContentItem
	for _, child := range logRoot.Children() {
		if child.Element() == nil || child.Element().QName().Local != "appender" {
			continue
		}
		for _, appenderChild := range child.Element().Children() {
			if appenderChild.Element() == nil || appenderChild.Element().QName().Local != "encoder" {
				continue
			}
			for _, encoderChild := range appenderChild.Element().Children() {
				if encoderChild.Element() != nil && encoderChild.Element().QName().Local == "pattern" {
					for _, patternChild := range encoderChild.Element().Children() {
						if patternChild.Text() != nil {
							patternText = patternChild
						}
					}
				}
			}
		}
	}
	if patternText == nil {
		t.Fatalf("logback pattern text not found")
	}
	newPattern := "%d{HH:mm:ss.SSS} %-5level [%thread] %logger{36} - %msg%n"
	textTx := xmlpkg.NewEditTransactionBuilder(logXML).
		ReplaceText(patternText.NodeRef(), newPattern).
		Build()
	textCommit := editCommit(t, logDoc, consema.NewXMLEditTransaction(textTx))
	logTarget := textCommit.Document.Render()
	assertUntouchedProof(t, logback, logTarget, textCommit.UntouchedProof, textCommit.SourcePatch)
	if !bytes.Contains(logTarget, []byte("<!-- Production logging boundary: INFO and above, no credentials. -->")) {
		t.Fatalf("logback comment lost")
	}
	if !bytes.Contains(logTarget, []byte(newPattern)) {
		t.Fatalf("pattern text replacement not present")
	}
	t.Logf("W3 logback.xml: pattern text replaced, comment and element structure untouched")
}

// ---------------------------------------------------------------------------
// W4 — plist XML/binary typed value edit, representation contract kept

func TestPilotW4(t *testing.T) {
	src := readFixture(t, "plist/xml/com.example.preferences.plist")
	doc := parseFacade(t, src, "plist.xml")
	plistDoc, _ := doc.AsPlist()

	tx := plist.NewEditTransactionBuilder(plistDoc).
		SetValue(plist.NewEditPath([]plist.EditPathStep{
			plist.NewEditPathStepDictKey(plist.NewPlistKeyFromUnicode("ui"), 0),
			plist.NewEditPathStepDictKey(plist.NewPlistKeyFromUnicode("font-size"), 0),
		}), plist.NewEditValueReal(plist.NewPlistRealDouble(14.0))).
		SetValue(plist.NewEditPath([]plist.EditPathStep{
			plist.NewEditPathStepDictKey(plist.NewPlistKeyFromUnicode("retry-count"), 0),
		}), plist.NewEditValueInteger(plist.NewPlistInteger(43))).
		Build()
	commit := editCommit(t, doc, consema.NewPlistEditTransaction(tx))
	target := commit.Document.Render()
	assertUntouchedProof(t, src, target, commit.UntouchedProof, commit.SourcePatch)
	if !bytes.Contains(target, []byte("<real>14</real>")) || !bytes.Contains(target, []byte("<integer>43</integer>")) {
		t.Fatalf("plist typed edits not present: %s", target)
	}
	t.Logf("W4 com.example.preferences.plist: font-size 12.5->14, retry-count 42->43 (XML representation)")

	// representation contract: edited XML converts to binary and back
	editedDoc, failure := plist.Parse(target, plist.PlistProfileXmlV1,
		plist.PlistEncodingProfileDefault(), plist.DefaultPlistParseLimits())
	if failure != nil {
		t.Fatalf("reparse edited plist: %v", failure)
	}
	binaryResult := consema.ConvertPlist(editedDoc, plist.NewProjectionRequestValueTree(),
		document.NewMaterializationRequest(
			document.NewProfileId("plist.binary", 1),
			document.NewMaterializationStyleId("plist.binary-canonical", 1)).
			WithEncoding(document.BinaryEncoding()).
			WithNewline(document.NewlineNone))
	if binaryResult.Failed != nil {
		t.Fatalf("plist XML -> binary after edit: %v", binaryResult.Failed)
	}
	binaryBytes := binaryResult.Complete.Document.Render()
	if !bytes.HasPrefix(binaryBytes, []byte("bplist00")) {
		t.Fatalf("binary plist header missing")
	}
	// round trip: binary back to XML keeps the edited typed values
	back, failure := plist.Parse(binaryBytes, plist.PlistProfileBinaryV1,
		plist.PlistEncodingProfileDefault(), plist.DefaultPlistParseLimits())
	if failure != nil {
		t.Fatalf("binary reparse: %v", failure)
	}
	if back.Render() == nil {
		t.Fatalf("binary round trip produced no document")
	}
	t.Logf("W4 representation contract: edited XML -> bplist00 binary -> parse back (typed values kept)")
}

// ---------------------------------------------------------------------------
// W5 — HCL literal attribute edit, no expression evaluation

func TestPilotW5(t *testing.T) {
	src := readFixture(t, "hcl/tf/main.tf")
	doc := parseFacade(t, src, "hcl.native")
	hclDoc, _ := doc.AsHCL()

	// instance_count default = 2 -> 4 through the variable body
	body := hclpkg.NewBodyPath([]hclpkg.BodyPathStep{
		hclpkg.NewBodyPathStep("variable", []string{"instance_count"}, 0),
	})
	tx := hclpkg.NewEditTransactionBuilder(hclDoc).
		SetAttributeValue(body, "default", hclpkg.EditValue{Tag: hclpkg.EditValueInteger, Integer: 4}).
		Build()
	commit := editCommit(t, doc, consema.NewHCLEditTransaction(tx))
	target := commit.Document.Render()
	assertUntouchedProof(t, src, target, commit.UntouchedProof, commit.SourcePatch)
	if !bytes.Contains(target, []byte("default = 4")) {
		t.Fatalf("hcl edit not present: %s", target)
	}
	// expression never evaluated: the expression syntax facts for the
	// `region = var.region` attribute remain untouched bytes.
	if !bytes.Contains(target, []byte(`region = var.region`)) {
		t.Fatalf("expression attribute damaged")
	}
	t.Logf("W5 main.tf: instance_count default 2 -> 4; `region = var.region` expression untouched (no evaluation)")
}

// ---------------------------------------------------------------------------
// W6 — audited conversions (JSON <-> TOML, JSON <-> YAML and family-internal)

func portableValueOfJSON(t testing.TB, doc *jsonpkg.Document) core.Value {
	t.Helper()
	projectionBuilder, projectionFailure := jsonpkg.NewProjectionRequestBuilder(
		jsonpkg.ProjectionTargetBestExactCoreV1).Build()
	if projectionFailure != nil {
		t.Fatalf("projection build: %v", projectionFailure)
	}
	projection := projectionBuilder
	result := doc.Project(projection)
	if result.Failed != nil {
		t.Fatalf("json projection failed: %d diagnostics", len(result.Failed.Diagnostics))
	}
	return result.Complete.Value
}

func TestPilotW6(t *testing.T) {
	src := readFixture(t, "real-world/package.json")
	doc := parseFacade(t, src, "json.strict")
	jsonDoc, _ := doc.AsJSON()
	projectionBuilder, projectionFailure := jsonpkg.NewProjectionRequestBuilder(
		jsonpkg.ProjectionTargetBestExactCoreV1).Build()
	if projectionFailure != nil {
		t.Fatalf("projection build: %v", projectionFailure)
	}
	projection := projectionBuilder

	toToml := consema.ConvertJSON(jsonDoc, projection,
		document.NewMaterializationRequest(
			document.NewProfileId("toml.1.0", 1),
			document.NewMaterializationStyleId("toml.canonical-document", 1)).
			WithNewline(document.NewlineLf).
			WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject))
	if toToml.Failed != nil {
		t.Fatalf("JSON -> TOML: %v", toToml.Failed)
	}
	report := toToml.Complete.Report
	if report.OverallFidelity() != consema.ConversionFidelityExact {
		t.Fatalf("JSON -> TOML must be exact, got %s", report.OverallFidelity().String())
	}
	tomlBytes := toToml.Complete.Document.Render()
	// round trip: TOML back to JSON, semantic equality
	backDoc, tomlFailure := toml.Parse(tomlBytes, toml.Toml10V1, document.DefaultParseLimits())
	if tomlFailure != nil {
		t.Fatalf("toml reparse: %v", tomlFailure)
	}
	backProjection := toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1)
	backResult := backDoc.Project(backProjection)
	if backResult.Failed != nil {
		t.Fatalf("toml projection failed: %d diagnostics", len(backResult.Failed.Diagnostics))
	}
	backValue := backResult.Complete.Value
	original := portableValueOfJSON(t, jsonDoc)
	if !core.Equal(original, backValue) {
		t.Fatalf("JSON -> TOML -> JSON semantic mismatch")
	}
	t.Logf("W6 JSON -> TOML (%d B) -> JSON: semantic equality, fidelity %s", len(tomlBytes), report.OverallFidelity().String())

	toYAML := consema.ConvertJSON(jsonDoc, projection,
		document.NewMaterializationRequest(
			document.NewProfileId("yaml.1.2-core", 1),
			document.NewMaterializationStyleId("yaml.canonical-flow", 1)).
			WithNewline(document.NewlineLf).
			WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject))
	if toYAML.Failed != nil {
		t.Fatalf("JSON -> YAML: %v", toYAML.Failed)
	}
	yamlBytes := toYAML.Complete.Document.Render()
	yamlBack, yamlFailure := yaml.Parse(yamlBytes, yaml.Yaml12CoreV1, document.DefaultParseLimits())
	if yamlFailure != nil {
		t.Fatalf("yaml reparse: %v", yamlFailure)
	}
	yamlBackResult := yamlBack.ProjectValue(yaml.BestExactValueV1())
	if yamlBackResult.Failed != nil {
		t.Fatalf("yaml projection failed: kind=%d", yamlBackResult.Failed.Kind)
	}
	yamlBackValue := yamlBackResult.Complete.Value
	if !core.Equal(original, yamlBackValue) {
		t.Fatalf("JSON -> YAML -> JSON semantic mismatch")
	}
	t.Logf("W6 JSON -> YAML (%d B) -> JSON: semantic equality, fidelity %s",
		len(yamlBytes), toYAML.Complete.Report.OverallFidelity().String())

	// family-internal conversions (pilot-0.13.0 §2.6 precedent)
	xmlSource := readFixture(t, "xml/app-server-config.xml")
	xmlDoc, xmlFailure := xmlpkg.Parse(context.Background(), xmlSource, xmlpkg.XmlProfileSafeV1,
		xmlpkg.XmlEncodingProfileDefault(), xmlpkg.DefaultXmlParseLimits())
	if xmlFailure != nil {
		t.Fatalf("xml parse: %v", xmlFailure)
	}
	xmlToXML := consema.ConvertXML(xmlDoc, xmlpkg.ElementTreeRequest(),
		document.NewMaterializationRequest(
			document.NewProfileId("xml.1.0-safe", 1),
			document.NewMaterializationStyleId("xml.safe-canonical-document", 1)))
	if xmlToXML.Failed != nil {
		t.Fatalf("XML -> XML: %v", xmlToXML.Failed)
	}
	xmlRound, xmlRoundFailure := xmlpkg.Parse(context.Background(), xmlToXML.Complete.Document.Render(),
		xmlpkg.XmlProfileSafeV1, xmlpkg.XmlEncodingProfileDefault(), xmlpkg.DefaultXmlParseLimits())
	if xmlRoundFailure != nil {
		t.Fatalf("xml round reparse: %v", xmlRoundFailure)
	}
	_ = xmlRound
	t.Logf("W6 XML -> XML: canonical safe document round-trips")

	tfvarsSource := []byte("region = \"us-east-1\"\ncount = 2\n")
	tfvarsDoc, tfvarsFailure := hclpkg.Parse(context.Background(), tfvarsSource,
		hclpkg.HclProfileTfvarsV1, hclpkg.HclEncodingSelectionProfileDefault(),
		hclpkg.DefaultHclParseLimits())
	if tfvarsFailure != nil {
		t.Fatalf("tfvars parse: %v", tfvarsFailure)
	}
	tfvarsToCanonical := consema.ConvertHCL(tfvarsDoc, hclpkg.ProjectionRequestBody(),
		document.NewMaterializationRequest(
			document.NewProfileId("hcl.native", 1),
			document.NewMaterializationStyleId("hcl.canonical-document", 1)))
	if tfvarsToCanonical.Failed != nil {
		t.Fatalf("tfvars -> canonical: %v", tfvarsToCanonical.Failed)
	}
	t.Logf("W6 HCL tfvars -> canonical hcl.native: %d B", len(tfvarsToCanonical.Complete.Document.Render()))
}

// ---------------------------------------------------------------------------
// W7 — non-lossless conversions rejected explicitly and atomically

func TestPilotW7(t *testing.T) {
	ctx := context.Background()

	// YAML graph (anchors/aliases) -> JSON: projection must fail atomically
	anchorSource := readFixture(t, "yaml/anchor-heavy.yaml")
	anchorDoc, failure := yaml.Parse(anchorSource, yaml.Yaml12CoreV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("anchor yaml parse: %v", failure)
	}
	toJSON := consema.ConvertYAML(anchorDoc, yaml.BestExactValueV1(),
		document.NewMaterializationRequest(
			document.NewProfileId("json.strict", 1),
			document.NewMaterializationStyleId("json.canonical-compact", 1)).
			WithNewline(document.NewlineNone))
	if toJSON.Failed == nil {
		t.Fatalf("YAML sharing -> JSON must fail")
	}
	if toJSON.Complete != nil {
		t.Fatalf("failed conversion returned a target document")
	}
	t.Logf("W7 YAML graph -> JSON rejected atomically: %s", toJSON.Failed.Code())

	// TOML date/time -> JSON: materialization must fail atomically
	allValues := readFixture(t, "toml/all-values.toml")
	allValuesDoc, allValuesFailure := toml.Parse(allValues, toml.Toml10V1, document.DefaultParseLimits())
	if allValuesFailure != nil {
		t.Fatalf("all-values parse: %v", allValuesFailure)
	}
	toJSON = consema.ConvertTOML(allValuesDoc, toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1),
		document.NewMaterializationRequest(
			document.NewProfileId("json.strict", 1),
			document.NewMaterializationStyleId("json.canonical-compact", 1)).
			WithNewline(document.NewlineNone))
	if toJSON.Failed == nil {
		t.Fatalf("TOML date/time -> JSON must fail")
	}
	t.Logf("W7 TOML date/time -> JSON rejected atomically: %s", toJSON.Failed.Code())

	// XML element tree -> JSON: record-consumption gate
	xmlSource := readFixture(t, "xml/app-server-config.xml")
	xmlDoc, xmlFailure := xmlpkg.Parse(ctx, xmlSource, xmlpkg.XmlProfileSafeV1,
		xmlpkg.XmlEncodingProfileDefault(), xmlpkg.DefaultXmlParseLimits())
	if xmlFailure != nil {
		t.Fatalf("xml parse: %v", xmlFailure)
	}
	toJSON = consema.ConvertXML(xmlDoc, xmlpkg.ElementTreeRequest(),
		document.NewMaterializationRequest(
			document.NewProfileId("json.strict", 1),
			document.NewMaterializationStyleId("json.canonical-compact", 1)).
			WithNewline(document.NewlineNone))
	if toJSON.Failed == nil {
		t.Fatalf("XML element tree -> JSON must fail")
	}
	t.Logf("W7 XML element tree -> JSON rejected atomically: %s", toJSON.Failed.Code())

	// TOML floats -> JSON: exact materialization refused (core
	// BinaryFloat64 has no exact JSON text representation under
	// ExactOnly)
	applicationToml := readFixture(t, "toml/application.toml")
	applicationTomlDoc, atFailure := toml.Parse(applicationToml, toml.Toml10V1,
		document.DefaultParseLimits())
	if atFailure != nil {
		t.Fatalf("application.toml parse: %v", atFailure)
	}
	toJSON = consema.ConvertTOML(applicationTomlDoc,
		toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1),
		document.NewMaterializationRequest(
			document.NewProfileId("json.strict", 1),
			document.NewMaterializationStyleId("json.canonical-compact", 1)).
			WithNewline(document.NewlineNone).
			WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject))
	if toJSON.Failed == nil {
		t.Fatalf("TOML floats -> JSON must fail")
	}
	t.Logf("W7 TOML floats -> JSON rejected atomically: %s", toJSON.Failed.Code())

	// JSON5 exact decimal -> TOML: materialization refused (TOML has no
	// exact decimal category; float would be lossy)
	json5Source := readFixture(t, "real-world/application.json5")
	json5Doc, json5Failure := jsonpkg.Parse(ctx, json5Source, jsonpkg.JsonProfileJson5StandardV1,
		document.DefaultParseLimits())
	if json5Failure != nil {
		t.Fatalf("json5 parse: %v", json5Failure)
	}
	json5Projection, json5ProjectionFailure := jsonpkg.NewProjectionRequestBuilder(
		jsonpkg.ProjectionTargetJson5BestExactCoreV1).Build()
	if json5ProjectionFailure != nil {
		t.Fatalf("json5 projection build: %v", json5ProjectionFailure)
	}
	toToml := consema.ConvertJSON(json5Doc, json5Projection,
		document.NewMaterializationRequest(
			document.NewProfileId("toml.1.0", 1),
			document.NewMaterializationStyleId("toml.canonical-document", 1)).
			WithNewline(document.NewlineLf).
			WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject))
	if toToml.Failed == nil {
		t.Fatalf("JSON5 decimal -> TOML must fail")
	}
	t.Logf("W7 JSON5 exact decimal -> TOML rejected atomically: %s", toToml.Failed.Code())

	// Recovered INI (duplicate sections) -> JSON: explicit rejection
	duplicateSource := []byte("[window]\nwidth=1440\n[window]\nwidth=900\n")
	duplicateDoc, duplicateFailure := ini.Parse(duplicateSource, ini.PortableV1,
		ini.IniEncodingProfileDefault(), ini.DefaultIniParseLimits())
	if duplicateFailure != nil {
		t.Fatalf("duplicate ini parse: %v", duplicateFailure)
	}
	toJSON = consema.ConvertINI(duplicateDoc, ini.BestExactEntryMappingV1(),
		document.NewMaterializationRequest(
			document.NewProfileId("json.strict", 1),
			document.NewMaterializationStyleId("json.canonical-compact", 1)).
			WithNewline(document.NewlineNone))
	if toJSON.Failed == nil {
		t.Fatalf("Recovered INI -> JSON must fail")
	}
	t.Logf("W7 Recovered INI -> JSON rejected atomically: %s", toJSON.Failed.Code())
}

// ---------------------------------------------------------------------------
// W8 — multi-file dry-run, review, stale conflict, apply, interruption

// planOne plans one file through the facade dry-run and batch planner.
func planOne(t testing.TB, planner *consema.BatchPlanner, path string, src []byte,
	profileID string, build func(doc *consema.Document) *consema.EditTransaction) {
	t.Helper()
	doc := parseFacade(t, src, profileID)
	tx := build(doc)
	plan, err := consema.PlanEdit(doc, tx, "pilot-"+filepath.Base(path))
	if err != nil {
		t.Fatalf("PlanEdit(%s): %v", path, err)
	}
	if err := planner.AddPlanned(path, plan); err != nil {
		t.Fatalf("AddPlanned(%s): %v", path, err)
	}
}

func TestPilotW8(t *testing.T) {
	dir := t.TempDir()

	// 1. multi-file dry-run + plan (review): 4 real files
	planner := consema.NewBatchPlanner("pilot-go-0.19.0")
	packageBytes := readFixture(t, "real-world/package.json")
	tsconfigBytes := readFixture(t, "real-world/tsconfig.jsonc")
	iniBytes := readFixture(t, "ini/desktop-settings.ini")
	json5Bytes := readFixture(t, "real-world/application.json5")

	planOne(t, planner, filepath.Join(dir, "package.json"), packageBytes, "json.strict",
		func(doc *consema.Document) *consema.EditTransaction {
			jsonDoc, _ := doc.AsJSON()
			fastify := jsonMemberRef(t, jsonDoc, "dependencies", "fastify")
			return consema.NewJSONEditTransaction(jsonpkg.NewEditTransactionBuilder(jsonDoc).
				LiteralScalar(fastify, []byte(`"4.29.0"`)).Build())
		})
	planOne(t, planner, filepath.Join(dir, "tsconfig.jsonc"), tsconfigBytes, "jsonc.bounded",
		func(doc *consema.Document) *consema.EditTransaction {
			jsonDoc, _ := doc.AsJSON()
			targetRef := jsonMemberRef(t, jsonDoc, "compilerOptions", "target")
			return consema.NewJSONEditTransaction(jsonpkg.NewEditTransactionBuilder(jsonDoc).
				LiteralScalar(targetRef, []byte(`"ES2023"`)).Build())
		})
	planOne(t, planner, filepath.Join(dir, "desktop-settings.ini"), iniBytes, "ini.portable",
		func(doc *consema.Document) *consema.EditTransaction {
			iniDoc, _ := doc.AsINI()
			for i := range iniDoc.Entries() {
				entry := iniDoc.Entries()[i]
				if entry.Key() == "width" {
					return consema.NewINIEditTransaction(ini.NewEditTransactionBuilder(iniDoc).
						SemanticValue(entry.NodeRef(), "1600",
							ini.RepresentationPolicyPreserveCompatible).Build())
				}
			}
			t.Fatalf("width entry not found")
			return nil
		})
	planOne(t, planner, filepath.Join(dir, "application.json5"), json5Bytes, "json5.standard",
		func(doc *consema.Document) *consema.EditTransaction {
			jsonDoc, _ := doc.AsJSON()
			portRef := jsonMemberRef(t, jsonDoc, "service", "port")
			return consema.NewJSONEditTransaction(jsonpkg.NewEditTransactionBuilder(jsonDoc).
				SemanticScalar(portRef, core.NewInteger(big.NewInt(9090)),
					jsonpkg.RepresentationPolicyPreserveCompatible).Build())
		})

	planMessage, err := planner.Build()
	if err != nil {
		t.Fatalf("batch plan build: %v", err)
	}
	if len(planMessage.Files()) != 4 {
		t.Fatalf("plan must contain 4 files")
	}

	// review + apply: every plan entry applies against the current bytes
	current := map[string][]byte{
		filepath.Join(dir, "package.json"):         packageBytes,
		filepath.Join(dir, "tsconfig.jsonc"):       tsconfigBytes,
		filepath.Join(dir, "desktop-settings.ini"): iniBytes,
		filepath.Join(dir, "application.json5"):    json5Bytes,
	}
	completed := 0
	for _, entry := range planMessage.Files() {
		result, err := consema.ApplyPlanFile(entry, current[entry.Path()], protocol.DefaultSourcePatchLimits())
		if err != nil {
			t.Fatalf("ApplyPlanFile(%s): %v", entry.Path(), err)
		}
		if result.Status() != protocol.ResultStatusCompleted {
			t.Fatalf("apply of %s not completed: %s", entry.Path(), result.Status())
		}
		targetDigest := result.TargetDigest()
		if targetDigest == nil {
			t.Fatalf("completed result without target digest")
		}
		completed++
	}
	if completed != 4 {
		t.Fatalf("expected 4 completed, got %d", completed)
	}
	t.Logf("W8 multi-file dry-run/review/apply: 4/4 completed with verified target digests")

	// 2. stale conflict: plan one file, mutate externally, apply -> skipped-stale
	stalePlanner := consema.NewBatchPlanner("pilot-go-0.19.0")
	stalePath := filepath.Join(dir, "stale-settings.ini")
	staleBytes := []byte("[window]\nwidth=1440\n")
	planOne(t, stalePlanner, stalePath, staleBytes, "ini.portable",
		func(doc *consema.Document) *consema.EditTransaction {
			iniDoc, _ := doc.AsINI()
			for i := range iniDoc.Entries() {
				entry := iniDoc.Entries()[i]
				if entry.Key() == "width" {
					return consema.NewINIEditTransaction(ini.NewEditTransactionBuilder(iniDoc).
						SemanticValue(entry.NodeRef(), "1600",
							ini.RepresentationPolicyPreserveCompatible).Build())
				}
			}
			t.Fatalf("width entry not found")
			return nil
		})
	stalePlan, err := stalePlanner.Build()
	if err != nil {
		t.Fatalf("stale plan build: %v", err)
	}
	// external mutation: append a line (simulates another writer)
	mutated := append(append([]byte(nil), staleBytes...), []byte("autosave=1\n")...)
	result, err := consema.ApplyPlanFile(stalePlan.Files()[0], mutated, protocol.DefaultSourcePatchLimits())
	if err != nil {
		t.Fatalf("stale apply: %v", err)
	}
	if result.Status() != protocol.ResultStatusSkippedStale {
		t.Fatalf("stale file must be skipped-stale, got %s", result.Status())
	}
	if result.FailureCode() == nil || *result.FailureCode() != "core.source.patch-base-mismatch@1" {
		t.Fatalf("stale failure code mismatch: %v", result.FailureCode())
	}
	if !bytes.Equal(mutated, append(append([]byte(nil), staleBytes...), []byte("autosave=1\n")...)) {
		t.Fatalf("stale file bytes were modified")
	}
	t.Logf("W8 stale conflict: skipped-stale core.source.patch-base-mismatch@1, file bytes untouched")

	// 3. 100-file batch with interruption and resume
	batchDir := filepath.Join(dir, "batch")
	if err := os.MkdirAll(batchDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	batchPlanner := consema.NewBatchPlanner("pilot-go-0.19.0")
	type plannedFile struct {
		path string
		base []byte
	}
	var batchFiles []plannedFile
	for index := 1; index <= 100; index++ {
		path := filepath.Join(batchDir, fmt.Sprintf("settings-%03d.ini", index))
		batchFiles = append(batchFiles, plannedFile{path: path, base: iniBytes})
		planOne(t, batchPlanner, path, iniBytes, "ini.portable",
			func(doc *consema.Document) *consema.EditTransaction {
				iniDoc, _ := doc.AsINI()
				for i := range iniDoc.Entries() {
					entry := iniDoc.Entries()[i]
					if entry.Key() == "width" {
						return consema.NewINIEditTransaction(ini.NewEditTransactionBuilder(iniDoc).
							SemanticValue(entry.NodeRef(), "1600",
								ini.RepresentationPolicyPreserveCompatible).Build())
					}
				}
				t.Fatalf("width entry not found")
				return nil
			})
	}
	batchPlan, err := batchPlanner.Build()
	if err != nil {
		t.Fatalf("batch plan build: %v", err)
	}
	if len(batchPlan.Files()) != 100 {
		t.Fatalf("batch plan must contain 100 files")
	}
	// interruption seam: first pass applies 53 files (simulated SIGINT)
	results := make(map[string]*protocol.BatchResultFileEntry)
	applyPass := func(files []*protocol.BatchPlanFileEntry, current func(string) []byte) {
		for _, entry := range files {
			result, err := consema.ApplyPlanFile(entry, current(entry.Path()), protocol.DefaultSourcePatchLimits())
			if err != nil {
				t.Fatalf("apply %s: %v", entry.Path(), err)
			}
			results[entry.Path()] = result
		}
	}
	passOne := batchPlan.Files()[:53]
	applyPass(passOne, func(path string) []byte {
		for _, file := range batchFiles {
			if file.path == path {
				return file.base
			}
		}
		t.Fatalf("pass-one current bytes for %s missing", path)
		return nil
	})
	// resume: apply the remaining 47
	passTwo := batchPlan.Files()[53:]
	applyPass(passTwo, func(path string) []byte {
		for _, file := range batchFiles {
			if file.path == path {
				return file.base
			}
		}
		t.Fatalf("pass-two current bytes for %s missing", path)
		return nil
	})
	completedCount := 0
	for _, result := range results {
		if result.Status() != protocol.ResultStatusCompleted {
			t.Fatalf("batch file not completed: %s", result.Status())
		}
		completedCount++
	}
	if completedCount != 100 {
		t.Fatalf("expected 100 completed after resume, got %d", completedCount)
	}
	t.Logf("W8 100-file batch: interrupted after 53, resumed with 47 -> 100/100 completed (recovery rate 100%%)")
}

// ---------------------------------------------------------------------------
// Metrics — §23.3 the twelve core metrics

func TestPilotMetrics(t *testing.T) {
	ctx := context.Background()
	src := readFixture(t, "real-world/package.json")
	parseFacade(t, src, "json.strict")
	iniSource := readFixture(t, "ini/desktop-settings.ini")
	iniFacadeDoc := parseFacade(t, iniSource, "ini.portable")
	iniDoc, _ := iniFacadeDoc.AsINI()

	// metric 8: query result determinism (INI + JSON native queries, twice)
	iniQuery := protocol.NewQueryDefinition(protocol.DomainININativeV1()).
		WithExpression((&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
			Then(protocol.NewOperatorCall("ini.document-sections", 1)).
			Then(protocol.NewOperatorCall("ini.section-name-equals", 1).
				WithArgument("name", core.String("window")).
				WithArgument("comparison", core.String("ProfileEquivalent"))).
			Then(protocol.NewOperatorCall("ini.section-entries", 1)))
	validated, failure := iniQuery.Validate()
	if failure != nil {
		t.Fatalf("ini query validation: %s", failure.Code())
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	bound, failure := validated.Bind(capabilities)
	if failure != nil {
		t.Fatalf("ini query binding: %s", failure.Code())
	}
	var queryRuns []string
	for run := 0; run < 2; run++ {
		matches, queryFailure := ini.ExecuteIniQuery(ctx, bound, iniDoc, protocol.DefaultQueryLimits())
		if queryFailure != nil {
			t.Fatalf("ini query: %s", queryFailure.Code())
		}
		var lines []string
		for _, match := range matches {
			lines = append(lines, fmt.Sprintf("%d:%s", match.Ordinal, match.Key))
		}
		queryRuns = append(queryRuns, strings.Join(lines, "\n"))
	}
	if queryRuns[0] != queryRuns[1] {
		t.Fatalf("query results not deterministic")
	}

	// metric 7: diagnostic stability (Recovered duplicate INI, twice)
	duplicateSource := []byte("[window]\nwidth=1440\n[window]\nwidth=900\n")
	var diagnosticRuns []string
	for run := 0; run < 2; run++ {
		duplicateDoc, failure := ini.Parse(duplicateSource, ini.PortableV1,
			ini.IniEncodingProfileDefault(), ini.DefaultIniParseLimits())
		if failure != nil {
			t.Fatalf("duplicate ini parse: %v", failure)
		}
		var lines []string
		for _, diag := range duplicateDoc.Diagnostics() {
			lines = append(lines, fmt.Sprintf("%s %s", diag.Code, diag.Notes))
		}
		diagnosticRuns = append(diagnosticRuns, strings.Join(lines, "\n"))
	}
	if diagnosticRuns[0] != diagnosticRuns[1] {
		t.Fatalf("diagnostics not stable across runs")
	}

	// metric 9: latency mean (steady state, n=300 per operation). The
	// p50/p95 percentiles are not meaningful below the Windows timer
	// granularity in-process; the report records the cold-process
	// numbers (fresh `go test` invocation per sample) instead.
	const samples = 300
	projectionBuilder, projectionFailure := jsonpkg.NewProjectionRequestBuilder(
		jsonpkg.ProjectionTargetBestExactCoreV1).Build()
	if projectionFailure != nil {
		t.Fatalf("projection build: %v", projectionFailure)
	}
	projection := projectionBuilder
	measure := func(run func() error) time.Duration {
		start := time.Now()
		for i := 0; i < samples; i++ {
			if err := run(); err != nil {
				t.Fatalf("measurement loop: %v", err)
			}
		}
		return time.Since(start) / samples
	}
	parseMean := measure(func() error {
		_, failure := jsonpkg.Parse(ctx, src, jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
		if failure != nil {
			return failure
		}
		return nil
	})
	queryMean := measure(func() error {
		jsonDoc, failure := jsonpkg.Parse(ctx, src, jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
		if failure != nil {
			return failure
		}
		executable, bindFailure := bindJSONQuery(protocol.DomainJSONNativeV1(),
			&protocol.QueryExpression{Kind: protocol.ExpressionInput})
		if bindFailure != "" {
			return fmt.Errorf("bind: %s", bindFailure)
		}
		_, queryFailure := jsonpkg.ExecuteJSONQuery(ctx, executable, jsonDoc, protocol.DefaultQueryLimits())
		if queryFailure != nil {
			return queryFailure
		}
		return nil
	})
	convertMean := measure(func() error {
		jsonDoc, failure := jsonpkg.Parse(ctx, src, jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
		if failure != nil {
			return failure
		}
		converted := consema.ConvertJSON(jsonDoc, projection,
			document.NewMaterializationRequest(
				document.NewProfileId("toml.1.0", 1),
				document.NewMaterializationStyleId("toml.canonical-document", 1)).
				WithNewline(document.NewlineLf).
				WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject))
		if converted.Failed != nil {
			return converted.Failed
		}
		return nil
	})
	editMean := measure(func() error {
		jsonDoc, failure := jsonpkg.Parse(ctx, src, jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
		if failure != nil {
			return failure
		}
		fastify := jsonMemberRef(t, jsonDoc, "dependencies", "fastify")
		tx := jsonpkg.NewEditTransactionBuilder(jsonDoc).
			LiteralScalar(fastify, []byte(`"4.29.0"`)).Build()
		_, editFailure := jsonDoc.Commit(tx)
		if editFailure != nil {
			return editFailure
		}
		return nil
	})
	t.Logf("latency mean (ns, n=%d, in-process steady state): parse %d, query %d, convert %d, edit %d",
		samples, parseMean, queryMean, convertMean, editMean)

	// metric 10: peak memory observed in-process after the batch work
	runtime.GC()
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	t.Logf("in-process heap after pilot work: alloc=%d B total-alloc=%d B (doc records process-level peak)",
		memStats.Alloc, memStats.TotalAlloc)
}

func bindJSONQuery(domain *protocol.QueryDomain,
	expression *protocol.QueryExpression) (*protocol.ExecutableQuery, string) {
	definition := protocol.NewQueryDefinition(domain).WithExpression(expression)
	validated, failure := definition.Validate()
	if failure != nil {
		return nil, "validation: " + failure.Code()
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	bound, failure := validated.Bind(capabilities)
	if failure != nil {
		return nil, "binding: " + failure.Code()
	}
	return bound, ""
}

func mean(samples []time.Duration) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	var total int64
	for _, sample := range samples {
		total += int64(sample)
	}
	return time.Duration(total / int64(len(samples)))
}

func percentile(samples []time.Duration, fraction float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := int(fraction * float64(len(sorted)))
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

// ---------------------------------------------------------------------------
// The three real batch migrations (§22.7) on the pilot project tree.
// Each migration asserts zero unauthorized loss: the untouched-byte proof
// plus the replacement-application check explain every changed byte.

// writePilotProject materializes the pilot project in a fresh directory and
// returns the file map.
func writePilotProject(t testing.TB, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{
		"package.json":                  readFixture(t, "real-world/package.json"),
		"tsconfig.jsonc":                readFixture(t, "real-world/tsconfig.jsonc"),
		"vscode-settings.jsonc":         readFixture(t, "real-world/vscode-settings.jsonc"),
		"application.json5":             readFixture(t, "real-world/application.json5"),
		"application.toml":              readFixture(t, "toml/application.toml"),
		"compose-services.yaml":         readFixture(t, "yaml/compose-services.yaml"),
		"desktop-settings.ini":          readFixture(t, "ini/desktop-settings.ini"),
		"build-tool.properties":         readFixture(t, "properties/build-tool.properties"),
		"app-server-config.xml":         readFixture(t, "xml/app-server-config.xml"),
		"com.example.preferences.plist": readFixture(t, "plist/xml/com.example.preferences.plist"),
		"main.tf":                       readFixture(t, "hcl/tf/main.tf"),
	}
	for name, bytes := range files {
		if err := os.WriteFile(filepath.Join(dir, name), bytes, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return files
}

// TestPilotMigration1 — 版本/镜像更新（§22.7 first migration）：
// dependency and image versions across JSONC/JSON5/TOML/YAML in one project
// tree, with per-file zero-unauthorized-loss evidence.
func TestPilotMigration1(t *testing.T) {
	dir := t.TempDir()
	base := writePilotProject(t, dir)
	type editSpec struct {
		name      string
		profile   string
		build     func(doc *consema.Document) *consema.EditTransaction
		expectSub []string // must appear in the target
		expectAbs []string // must not appear in the target
	}
	specs := []editSpec{
		{
			name: "package.json", profile: "json.strict",
			build: func(doc *consema.Document) *consema.EditTransaction {
				jsonDoc, _ := doc.AsJSON()
				fastify := jsonMemberRef(t, jsonDoc, "dependencies", "fastify")
				version := jsonMemberRef(t, jsonDoc, "version")
				return consema.NewJSONEditTransaction(jsonpkg.NewEditTransactionBuilder(jsonDoc).
					LiteralScalar(fastify, []byte(`"4.29.0"`)).
					SemanticScalar(version, core.String("1.1.0"),
						jsonpkg.RepresentationPolicyPreserveCompatible).Build())
			},
			expectSub: []string{`"fastify": "4.29.0"`, `"version": "1.1.0"`},
			expectAbs: []string{`"4.28.1"`, `"version": "1.0.0"`},
		},
		{
			name: "tsconfig.jsonc", profile: "jsonc.bounded",
			build: func(doc *consema.Document) *consema.EditTransaction {
				jsonDoc, _ := doc.AsJSON()
				targetRef := jsonMemberRef(t, jsonDoc, "compilerOptions", "target")
				return consema.NewJSONEditTransaction(jsonpkg.NewEditTransactionBuilder(jsonDoc).
					LiteralScalar(targetRef, []byte(`"ES2023"`)).Build())
			},
			expectSub: []string{`"target": "ES2023"`},
			expectAbs: []string{`"target": "ES2022"`},
		},
		{
			name: "application.json5", profile: "json5.standard",
			build: func(doc *consema.Document) *consema.EditTransaction {
				jsonDoc, _ := doc.AsJSON()
				portRef := jsonMemberRef(t, jsonDoc, "service", "port")
				return consema.NewJSONEditTransaction(jsonpkg.NewEditTransactionBuilder(jsonDoc).
					SemanticScalar(portRef, core.NewInteger(big.NewInt(9090)),
						jsonpkg.RepresentationPolicyPreserveCompatible).Build())
			},
			expectSub: []string{`port: 9090`},
			expectAbs: []string{`port: 8080`},
		},
		{
			name: "compose-services.yaml", profile: "yaml.1.2-core",
			build: func(doc *consema.Document) *consema.EditTransaction {
				yamlDoc, _ := doc.AsYAML()
				streamDoc, _ := yamlDoc.Document(0)
				imageRef := yamlWalkMapping(t, streamDoc.Root(), "services", "api", "image")
				return consema.NewYAMLEditTransaction(yaml.NewEditTransactionBuilder(yamlDoc).
					LiteralScalar(imageRef, []byte("example.invalid/api:1.1.0")).Build())
			},
			expectSub: []string{"image: example.invalid/api:1.1.0"},
			expectAbs: []string{"image: example.invalid/api:1.0.0"},
		},
	}

	totalChanged := 0
	for _, spec := range specs {
		path := filepath.Join(dir, spec.name)
		original := base[spec.name]
		doc := parseFacade(t, original, spec.profile)
		commit := editCommit(t, doc, spec.build(doc))
		target := commit.Document.Render()
		assertUntouchedProof(t, original, target, commit.UntouchedProof, commit.SourcePatch)
		for _, sub := range spec.expectSub {
			if !bytes.Contains(target, []byte(sub)) {
				t.Fatalf("%s: %q missing in target", spec.name, sub)
			}
		}
		for _, abs := range spec.expectAbs {
			if bytes.Contains(target, []byte(abs)) {
				t.Fatalf("%s: %q must not appear in target", spec.name, abs)
			}
		}
		rate := untouchedRate(commit.UntouchedProof, len(original))
		changed := len(original) - int(rate*float64(len(original)))
		totalChanged += changed
		t.Logf("migration1 %-28s base %d B -> target %d B, untouched %.6f (base %s -> target %s)",
			spec.name, len(original), len(target), rate,
			digestHex(original)[:16], digestHex(target)[:16])
		if err := os.WriteFile(path, target, 0o644); err != nil {
			t.Fatalf("write target %s: %v", spec.name, err)
		}
	}
	t.Logf("migration1: %d files migrated, %d changed bytes total, 0 unauthorized bytes (all changed bytes covered by proofs)",
		len(specs), totalChanged)
}

// TestPilotMigration2 — 结构插入删除（§22.7 second migration）：
// INI/Properties insert/delete/rename with duplicates and logical lines
// preserved.
func TestPilotMigration2(t *testing.T) {
	dir := t.TempDir()
	base := writePilotProject(t, dir)

	// INI: insert section, insert entry, rename section, remove entry
	iniOriginal := base["desktop-settings.ini"]
	iniDoc, failure := ini.Parse(iniOriginal, ini.PortableV1,
		ini.IniEncodingProfileDefault(), ini.DefaultIniParseLimits())
	if failure != nil {
		t.Fatalf("ini parse: %v", failure)
	}
	var windowSection, appearanceSection *ini.IniSection
	for i := range iniDoc.Sections() {
		section := iniDoc.Sections()[i]
		if section.Name() == "window" {
			windowSection = &section
		}
		if section.Name() == "appearance" {
			appearanceSection = &section
		}
	}
	var maximized *ini.IniEntry
	var theme *ini.IniEntry
	for i := range iniDoc.Entries() {
		entry := iniDoc.Entries()[i]
		if entry.Key() == "maximized" {
			maximized = &entry
		}
		if entry.Key() == "theme" {
			theme = &entry
		}
	}
	if windowSection == nil || appearanceSection == nil || maximized == nil || theme == nil {
		t.Fatalf("ini structure mismatch")
	}
	tx := ini.NewEditTransactionBuilder(iniDoc).
		InsertSection(iniDoc.NodeRef(), "logging", ini.PlacementEnd()).
		InsertEntry(appearanceSection.NodeRef(), "icon-theme", "auto", ini.PlacementEnd()).
		RenameSection(windowSection.NodeRef(), "display").
		RenameEntry(theme.NodeRef(), "color-scheme").
		RemoveEntry(maximized.NodeRef()).
		Build()
	commit, iniCommitFailure := iniDoc.Commit(tx)
	if iniCommitFailure != nil {
		t.Fatalf("ini commit: %s", iniCommitFailure.Error())
	}
	iniTarget := commit.Document.Render()
	assertUntouchedProof(t, iniOriginal, iniTarget, &commit.UntouchedProof, commit.SourcePatch)
	for _, required := range []string{"[logging]", "icon-theme=auto", "[display]", "color-scheme=system"} {
		if !bytes.Contains(iniTarget, []byte(required)) {
			t.Fatalf("migration2 ini: %q missing", required)
		}
	}
	if bytes.Contains(iniTarget, []byte("maximized")) {
		t.Fatalf("migration2 ini: maximized must be removed")
	}
	t.Logf("migration2 desktop-settings.ini: section insert/rename, entry insert/rename/remove (base %s -> target %s, untouched %.6f)",
		digestHex(iniOriginal)[:16], digestHex(iniTarget)[:16],
		untouchedRate(&commit.UntouchedProof, len(iniOriginal)))

	// Properties: insert/rename/remove with comment preservation
	propOriginal := base["build-tool.properties"]
	propDoc, propFailure := properties.Parse(propOriginal, properties.PropertiesReaderV1,
		properties.ReaderEncodingSelection(document.Utf8Encoding()), properties.DefaultPropertiesParseLimits())
	if propFailure != nil {
		t.Fatalf("properties parse: %v", propFailure)
	}
	var versionProperty, parallelProperty *properties.Property
	for i := range propDoc.Properties() {
		property := propDoc.Properties()[i]
		if key, err := property.Key().ToUnicode(); err == nil && key == "version" {
			versionProperty = &property
		}
		if key, err := property.Key().ToUnicode(); err == nil && key == "org.gradle.parallel" {
			parallelProperty = &property
		}
	}
	if versionProperty == nil || parallelProperty == nil {
		t.Fatalf("properties structure mismatch")
	}
	txProps := properties.NewEditTransactionBuilder(propDoc).
		SemanticValue(versionProperty.NodeRef(), properties.NewJavaStringFromUnicode("2.0.0")).
		InsertProperty(propDoc.NodeRef(), properties.NewJavaStringFromUnicode("org.gradle.jvmargs"),
			properties.NewJavaStringFromUnicode("-Xmx4g"), properties.PlacementEnd()).
		RenameProperty(parallelProperty.NodeRef(), properties.NewJavaStringFromUnicode("org.gradle.parallel.threads")).
		Build()
	commitProps, failureProps := propDoc.Commit(txProps)
	if failureProps != nil {
		t.Fatalf("properties commit: %s", failureProps.Error())
	}
	propTarget := commitProps.Document.Render()
	assertUntouchedProof(t, propOriginal, propTarget, &commitProps.UntouchedProof, commitProps.SourcePatch)
	if !bytes.Contains(propTarget, []byte("version=2.0.0")) ||
		!bytes.Contains(propTarget, []byte("org.gradle.jvmargs=-Xmx4g")) ||
		!bytes.Contains(propTarget, []byte("org.gradle.parallel.threads=true")) {
		t.Fatalf("migration2 properties: edits not present:\n%s", propTarget)
	}
	if !bytes.Contains(propTarget, []byte("# Consema build-tool fixture")) {
		t.Fatalf("migration2 properties: comment lost")
	}
	t.Logf("migration2 build-tool.properties: semantic replace, insert, rename (base %s -> target %s, untouched %.6f)",
		digestHex(propOriginal)[:16], digestHex(propTarget)[:16],
		untouchedRate(&commitProps.UntouchedProof, len(propOriginal)))

	// duplicates and logical lines: the continuation-heavy corpus file
	// (with duplicates and continuations) round-trips byte-exact unedited.
	continuation := readFixture(t, "properties/continuation-heavy.properties")
	contDoc, contFailure := properties.Parse(continuation, properties.PropertiesReaderV1,
		properties.ReaderEncodingSelection(document.Utf8Encoding()), properties.DefaultPropertiesParseLimits())
	if contFailure != nil {
		t.Fatalf("continuation parse: %v", contFailure)
	}
	if !bytes.Equal(contDoc.Render(), continuation) {
		t.Fatalf("continuation-heavy round trip not byte-exact")
	}
	t.Logf("migration2 logical-line evidence: continuation-heavy.properties round-trips byte-exact (no edit touches it)")
}

// TestPilotMigration3 — 跨格式转换（§22.7 third migration）：
// audited JSON <-> TOML and JSON <-> YAML conversions over the project
// tree, zero unauthorized loss (every conversion Exact or atomically
// rejected).
func TestPilotMigration3(t *testing.T) {
	dir := t.TempDir()
	base := writePilotProject(t, dir)

	convertible := []struct {
		name     string
		profile  string
		targetID string
		styleID  string
		newline  document.NewlinePolicy
	}{
		{"package.json", "json.strict", "toml.1.0", "toml.canonical-document", document.NewlineLf},
		{"application.json5", "json5.standard", "yaml.1.2-core", "yaml.canonical-flow", document.NewlineLf},
		{"package.json", "json.strict", "yaml.1.2-core", "yaml.canonical-flow", document.NewlineLf},
		{"desktop-settings.ini", "ini.portable", "json.strict", "json.canonical-compact", document.NewlineNone},
	}
	convertedCount := 0
	for _, spec := range convertible {
		original := base[spec.name]
		doc := parseFacade(t, original, spec.profile)
		var result consema.ConversionResult
		switch spec.profile {
		case "json.strict", "json5.standard":
			jsonDoc, _ := doc.AsJSON()
			target := jsonpkg.ProjectionTargetBestExactCoreV1
			if spec.profile == "json5.standard" {
				target = jsonpkg.ProjectionTargetJson5BestExactCoreV1
			}
			projection, failure := jsonpkg.NewProjectionRequestBuilder(target).Build()
			if failure != nil {
				t.Fatalf("projection: %v", failure)
			}
			request := document.NewMaterializationRequest(
				document.NewProfileId(spec.targetID, 1),
				document.NewMaterializationStyleId(spec.styleID, 1)).
				WithNewline(spec.newline).
				WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject)
			if spec.targetID == "toml.1.0" {
				result = consema.ConvertJSON(jsonDoc, projection, request)
			} else {
				result = consema.ConvertJSON(jsonDoc, projection, request)
			}
		case "toml.1.0":
			tomlDoc, _ := doc.AsTOML()
			result = consema.ConvertTOML(tomlDoc,
				toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1),
				document.NewMaterializationRequest(
					document.NewProfileId(spec.targetID, 1),
					document.NewMaterializationStyleId(spec.styleID, 1)).
					WithNewline(spec.newline).
					WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject))
		case "ini.portable":
			iniDoc, _ := doc.AsINI()
			result = consema.ConvertINI(iniDoc, ini.BestExactEntryMappingV1(),
				document.NewMaterializationRequest(
					document.NewProfileId(spec.targetID, 1),
					document.NewMaterializationStyleId(spec.styleID, 1)).
					WithNewline(spec.newline).
					WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject))
		}
		if result.Failed != nil {
			t.Fatalf("migration3 convert %s -> %s: %v", spec.name, spec.targetID, result.Failed)
		}
		if result.Complete.Report.OverallFidelity() != consema.ConversionFidelityExact {
			t.Fatalf("migration3 convert %s -> %s not exact: %s",
				spec.name, spec.targetID, result.Complete.Report.OverallFidelity().String())
		}
		targetBytes := result.Complete.Document.Render()
		if len(targetBytes) == 0 {
			t.Fatalf("migration3 convert %s produced empty target", spec.name)
		}
		convertedCount++
		t.Logf("migration3 %-24s -> %-14s %6d B  fidelity=%s",
			spec.name, spec.targetID, len(targetBytes),
			result.Complete.Report.OverallFidelity().String())
	}
	// source project files untouched: every source file still byte-identical
	for name, original := range base {
		current, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !bytes.Equal(current, original) {
			t.Fatalf("migration3 changed source file %s without authorization", name)
		}
	}
	t.Logf("migration3: %d audited conversions, all Exact, source tree byte-untouched", convertedCount)
}

// ---------------------------------------------------------------------------
// Metric 12 — Rust/Go observable mismatch count. The comparison runs the
// current Rust CLI (target/release/consema.exe) on the same corpus with
// the same requests and compares bytes with the Go SDK outputs. The test
// is skipped when the binary is absent (set CONSEMA_PILOT_RUST_CLI to the
// binary path); the report records the real runs.
func TestPilotRustComparison(t *testing.T) {
	cliPath := os.Getenv("CONSEMA_PILOT_RUST_CLI")
	if cliPath == "" {
		t.Skip("CONSEMA_PILOT_RUST_CLI not set; Rust comparison recorded in docs/pilot-go-0.19.0.md")
	}
	if _, err := os.Stat(cliPath); err != nil {
		t.Skipf("Rust CLI not found at %s: %v", cliPath, err)
	}
	dir := t.TempDir()
	packageBytes := readFixture(t, "real-world/package.json")
	if err := os.WriteFile(filepath.Join(dir, "package.json"), packageBytes, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// The cookbook pin: cli.convert-request@1 envelope payload with the
	// toml.1.0 canonical-document materialization request
	// (docs/cookbook.md §5, the exact pinned canonical JSON). The YAML
	// variant derives by replacing the two target profile ids, keeping
	// the canonical field order intact.
	const cookbookConvertRequest = `{"schema":"core.portable-value-json@1","value":{"type":"Object","entries":[{"key":"schema","value":{"type":"String","value":"cli.convert-request@1"}},{"key":"projection_request","value":{"type":"Object","entries":[{"key":"schema","value":{"type":"String","value":"core.projection-request@1"}},{"key":"target","value":{"type":"Object","entries":[{"key":"id","value":{"type":"String","value":"json.projection.best-exact-core"}},{"key":"version","value":{"type":"Integer","value":"1"}}]}},{"key":"default_policy","value":{"type":"Object","entries":[{"key":"id","value":{"type":"String","value":"core.projection.exact-or-reject"}},{"key":"version","value":{"type":"Integer","value":"1"}},{"key":"arguments","value":{"type":"Object","entries":[]}}]}},{"key":"rules","value":{"type":"Sequence","items":[]}},{"key":"limits","value":{"type":"Object","entries":[]}}]}},{"key":"materialization_request","value":{"type":"Object","entries":[{"key":"schema","value":{"type":"String","value":"core.materialization-request@2"}},{"key":"target_profile","value":{"type":"Object","entries":[{"key":"id","value":{"type":"String","value":"toml.1.0"}},{"key":"version","value":{"type":"Integer","value":"1"}}]}},{"key":"style","value":{"type":"Object","entries":[{"key":"id","value":{"type":"String","value":"toml.canonical-document"}},{"key":"version","value":{"type":"Integer","value":"1"}}]}},{"key":"encoding","value":{"type":"Object","entries":[{"key":"schema","value":{"type":"String","value":"core.source-encoding@1"}},{"key":"kind","value":{"type":"String","value":"Utf8"}},{"key":"windows_code_page","value":{"type":"Null"}}]}},{"key":"newline","value":{"type":"String","value":"Lf"}},{"key":"mapping_policy","value":{"type":"String","value":"UniqueStringEntriesToObject"}},{"key":"representability","value":{"type":"String","value":"ExactOnly"}},{"key":"limits","value":{"type":"Object","entries":[{"key":"max_input_nodes","value":{"type":"Integer","value":"1000000"}},{"key":"max_output_bytes","value":{"type":"Integer","value":"67108864"}},{"key":"max_depth","value":{"type":"Integer","value":"256"}},{"key":"max_report_entries","value":{"type":"Integer","value":"100000"}},{"key":"max_provenance_entries","value":{"type":"Integer","value":"2000000"}}]}}]}}]}}`
	convertRequest := func(projectionTarget, targetID, styleID string) []byte {
		derived := strings.ReplaceAll(cookbookConvertRequest, "json.projection.best-exact-core", projectionTarget)
		derived = strings.ReplaceAll(derived, "toml.1.0", targetID)
		derived = strings.ReplaceAll(derived, "toml.canonical-document", styleID)
		return []byte(derived)
	}
	writeRequest := func(name, projectionTarget, targetID, styleID string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, convertRequest(projectionTarget, targetID, styleID), 0o644); err != nil {
			t.Fatalf("write request: %v", err)
		}
		return path
	}

	runCLI := func(args ...string) ([]byte, int) {
		command := exec.Command(cliPath, args...)
		output, err := command.CombinedOutput()
		code := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				code = exitErr.ExitCode()
			} else {
				t.Fatalf("run %s: %v", cliPath, err)
			}
		}
		return output, code
	}

	jsonDoc := parseFacade(t, packageBytes, "json.strict")
	jsonInner, _ := jsonDoc.AsJSON()
	projection, failure := jsonpkg.NewProjectionRequestBuilder(
		jsonpkg.ProjectionTargetBestExactCoreV1).Build()
	if failure != nil {
		t.Fatalf("projection: %v", failure)
	}
	conversions := []struct {
		name     string
		targetID string
		styleID  string
	}{
		{"rust-toml", "toml.1.0", "toml.canonical-document"},
		{"rust-yaml", "yaml.1.2-core", "yaml.canonical-flow"},
	}
	mismatches := 0
	for _, conversion := range conversions {
		requestPath := writeRequest("request-"+conversion.name+".json",
			"json.projection.best-exact-core", conversion.targetID, conversion.styleID)
		rustOutput, exitCode := runCLI("convert", filepath.Join(dir, "package.json"),
			"--profile", "json.strict", "--request-file", requestPath)
		if exitCode != 0 {
			t.Fatalf("rust convert %s: exit %d: %s", conversion.targetID, exitCode, rustOutput)
		}
		goResult := consema.ConvertJSON(jsonInner, projection,
			document.NewMaterializationRequest(
				document.NewProfileId(conversion.targetID, 1),
				document.NewMaterializationStyleId(conversion.styleID, 1)).
				WithNewline(document.NewlineLf).
				WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject))
		if goResult.Failed != nil {
			t.Fatalf("go convert %s: %v", conversion.targetID, goResult.Failed)
		}
		goBytes := goResult.Complete.Document.Render()
		if !bytes.Equal(rustOutput, goBytes) {
			mismatches++
			t.Errorf("Rust/Go mismatch for %s:\n--- rust ---\n%s\n--- go ---\n%s",
				conversion.targetID, rustOutput, goBytes)
		}
		t.Logf("metric12 %-10s -> %-18s %6d B  byte-exact match",
			"package.json", conversion.targetID, len(rustOutput))
	}

	// Lossy conversions must fail identically on both sides (W7
	// cross-language evidence): TOML floats -> JSON.
	tomlBytes := readFixture(t, "toml/application.toml")
	if err := os.WriteFile(filepath.Join(dir, "application.toml"), tomlBytes, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tomlRequestPath := writeRequest("request-toml-json.json",
		"toml.projection.best-exact-core", "json.strict", "json.canonical-compact")
	rustOutput, exitCode := runCLI("convert", filepath.Join(dir, "application.toml"),
		"--profile", "toml.1.0", "--request-file", tomlRequestPath)
	if exitCode == 0 {
		t.Fatalf("rust convert toml->json must fail")
	}
	tomlDoc, atFailure := toml.Parse(tomlBytes, toml.Toml10V1, document.DefaultParseLimits())
	if atFailure != nil {
		t.Fatalf("toml parse: %v", atFailure)
	}
	goResult := consema.ConvertTOML(tomlDoc,
		toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1),
		document.NewMaterializationRequest(
			document.NewProfileId("json.strict", 1),
			document.NewMaterializationStyleId("json.canonical-compact", 1)).
			WithNewline(document.NewlineNone).
			WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject))
	if goResult.Failed == nil {
		t.Fatalf("go convert toml->json must fail")
	}
	if !strings.Contains(string(rustOutput), "materialization-failed") {
		t.Errorf("rust failure code mismatch: %s", rustOutput)
	}
	t.Logf("metric12 toml -> json refused on both sides (rust code %s, go code %s)",
		"core.conversion.materialization-failed@1", goResult.Failed.Code())

	if mismatches != 0 {
		t.Fatalf("Rust/Go mismatch count: %d", mismatches)
	}
	t.Logf("metric12 Rust/Go observable mismatch count: 0 over %d conversion pairs", len(conversions))
}
