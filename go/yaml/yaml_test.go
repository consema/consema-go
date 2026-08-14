package yaml

import (
	"math/big"
	"strings"
	"testing"

	"consema.dev/consema/document"
)

func mustParse(t *testing.T, source string, profile YamlProfile) *Document {
	t.Helper()
	doc, failure := Parse([]byte(source), profile, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("parse %q failed: %s", source, failure.Code())
	}
	return doc
}

func mustFail(t *testing.T, source string, profile YamlProfile, code string) *FormationFailure {
	t.Helper()
	_, failure := Parse([]byte(source), profile, document.DefaultParseLimits())
	if failure == nil {
		t.Fatalf("parse %q unexpectedly succeeded", source)
	}
	if failure.Code() != code {
		t.Fatalf("parse %q: code %s, want %s", source, failure.Code(), code)
	}
	return failure
}

func rootScalar(t *testing.T, doc *Document) YamlScalar {
	t.Helper()
	yamlDoc, ok := doc.Document(0)
	if !ok {
		t.Fatalf("document 0 missing")
	}
	scalar, ok := yamlDoc.Root().Scalar()
	if !ok {
		t.Fatalf("root is not a scalar")
	}
	return scalar
}

// TestProfileScalarResolution pins the frozen profile resolution of the
// YAML 1.1-only keyword forms (RFC 0007 §5/§6; native.rs tests).
func TestProfileScalarResolution(t *testing.T) {
	cases := []struct {
		source    string
		profile   YamlProfile
		kind      YamlScalarKind
		canonical string
	}{
		{"~", Yaml12CoreV1, ScalarKindNull, ""},
		{"null", Yaml12CoreV1, ScalarKindNull, ""},
		{"Null", Yaml12CoreV1, ScalarKindNull, ""},
		{"NULL", Yaml12CoreV1, ScalarKindNull, ""},
		{"true", Yaml12CoreV1, ScalarKindBoolean, "true"},
		{"True", Yaml12CoreV1, ScalarKindBoolean, "true"},
		{"TRUE", Yaml12CoreV1, ScalarKindBoolean, "true"},
		{"false", Yaml12CoreV1, ScalarKindBoolean, "false"},
		{"False", Yaml12CoreV1, ScalarKindBoolean, "false"},
		{"FALSE", Yaml12CoreV1, ScalarKindBoolean, "false"},
		{"yes", Yaml12CoreV1, ScalarKindString, "yes"},
		{"on", Yaml12CoreV1, ScalarKindString, "on"},
		{".inf", Yaml12CoreV1, ScalarKindFloat, ".inf"},
		{"-.inf", Yaml12CoreV1, ScalarKindFloat, "-.inf"},
		{".nan", Yaml12CoreV1, ScalarKindFloat, ".nan"},
		{"0x1F", Yaml12CoreV1, ScalarKindInteger, "31"},
		{"0o17", Yaml12CoreV1, ScalarKindInteger, "15"},
		{"017", Yaml12CoreV1, ScalarKindInteger, "17"},
		{"1_000", Yaml12CoreV1, ScalarKindString, "1_000"},
		{"1.5", Yaml12CoreV1, ScalarKindFloat, "15e-1"},
		{"1e3", Yaml12CoreV1, ScalarKindFloat, "1e3"},
		{"yes", Yaml11CompatV1, ScalarKindBoolean, "true"},
		{"on", Yaml11CompatV1, ScalarKindBoolean, "true"},
		{"no", Yaml11CompatV1, ScalarKindBoolean, "false"},
		{"off", Yaml11CompatV1, ScalarKindBoolean, "false"},
		{"y", Yaml11CompatV1, ScalarKindBoolean, "true"},
		{"N", Yaml11CompatV1, ScalarKindBoolean, "false"},
		{"017", Yaml11CompatV1, ScalarKindInteger, "15"},
		{"0b101", Yaml11CompatV1, ScalarKindInteger, "5"},
		{"0o17", Yaml11CompatV1, ScalarKindString, "0o17"},
		{"1:02:03", Yaml11CompatV1, ScalarKindInteger, "3723"},
		{"1_000", Yaml11CompatV1, ScalarKindInteger, "1000"},
		{"2001-12-15", Yaml11CompatV1, ScalarKindTimestamp, "2001-12-15"},
		{"2001-12-15T02:59:43Z", Yaml11CompatV1, ScalarKindTimestamp,
			"2001-12-15T02:59:43Z"},
	}
	for _, test := range cases {
		scalar := rootScalar(t, mustParse(t, test.source, test.profile))
		if scalar.Kind() != test.kind || scalar.Canonical() != test.canonical {
			t.Errorf("%q under %s: kind=%s canonical=%q; want %s %q",
				test.source, test.profile, scalar.Kind(), scalar.Canonical(),
				test.kind, test.canonical)
		}
	}
}

// TestQuotedKeywordsAreStrings pins the quoted-always-wins rule for both
// profiles (native.rs quoted tests).
func TestQuotedKeywordsAreStrings(t *testing.T) {
	keywords := []string{"", "~", "null", "true", "yes", "on", "017", "1:02:03", "1_000",
		"2001-12-15", ".inf", ".nan", "0x1F", "0o17", "1e3", "1.5"}
	for _, profile := range []YamlProfile{Yaml12CoreV1, Yaml11CompatV1} {
		for _, keyword := range keywords {
			for _, quote := range []string{"'", "\""} {
				scalar := rootScalar(t, mustParse(t, quote+keyword+quote, profile))
				if scalar.Kind() != ScalarKindString {
					t.Errorf("%s %q under %s: kind=%s, want String",
						quote, keyword, profile, scalar.Kind())
				}
				if scalar.Decoded() != keyword {
					t.Errorf("%s %q: decoded %q", quote, keyword, scalar.Decoded())
				}
			}
		}
	}
}

// TestBlockScalarsAreStrings pins the block-style string rule and the
// chomping modes (native.rs block tests).
func TestBlockScalarsAreStrings(t *testing.T) {
	doc := mustParse(t, "a: |\n  ~\nb: >\n  null\nc: |-\n  x\nd: |+\n  y\n\n", Yaml12CoreV1)
	yamlDoc, _ := doc.Document(0)
	root := yamlDoc.Root()
	values := []struct {
		decoded string
		kind    YamlScalarKind
		style   YamlScalarStyle
	}{
		{"~\n", ScalarKindString, ScalarStyleLiteral},
		{"null\n", ScalarKindString, ScalarStyleFolded},
		{"x", ScalarKindString, ScalarStyleLiteral},
		{"y\n\n", ScalarKindString, ScalarStyleLiteral},
	}
	for ordinal, expected := range values {
		entry, _ := root.MappingEntry(ordinal)
		scalar, _ := entry.Value().Scalar()
		if scalar.Decoded() != expected.decoded || scalar.Kind() != expected.kind ||
			scalar.Style() != expected.style {
			t.Errorf("entry %d: decoded=%q kind=%s style=%s; want %q %s %s",
				ordinal, scalar.Decoded(), scalar.Kind(), scalar.Style(),
				expected.decoded, expected.kind, expected.style)
		}
	}
}

// TestFoldedScalarFolding pins the folded line-break rules.
func TestFoldedScalarFolding(t *testing.T) {
	doc := mustParse(t, "a: >\n  one\n  two\n\n  three\n", Yaml12CoreV1)
	yamlDoc, _ := doc.Document(0)
	entry, _ := yamlDoc.Root().MappingEntry(0)
	scalar, _ := entry.Value().Scalar()
	if scalar.Decoded() != "one two\n\nthree\n" {
		t.Errorf("folded decoded %q", scalar.Decoded())
	}
}

// TestAliasComposition pins shared identity and cycles without expansion
// (native.rs aliases_compose test).
func TestAliasComposition(t *testing.T) {
	doc := mustParse(t, "&self [*self]\n", Yaml12CoreV1)
	yamlDoc, _ := doc.Document(0)
	root := yamlDoc.Root()
	anchor, ok := root.Anchor()
	if !ok || anchor != "self" {
		t.Fatalf("anchor %q %v", anchor, ok)
	}
	length, _ := root.SequenceLen()
	if length != 1 {
		t.Fatalf("sequence len %d", length)
	}
	item, _ := root.SequenceItem(0)
	if item.Node().NodeRef() != root.NodeRef() {
		t.Fatalf("self alias does not share identity")
	}
	if doc.AliasCount() != 1 {
		t.Fatalf("alias count %d", doc.AliasCount())
	}
	alias, _ := doc.Alias(0)
	if alias.Name() != "self" || alias.Target().NodeRef() != root.NodeRef() {
		t.Fatalf("alias facts")
	}
	projected, err := doc.ProjectGraph()
	if err != nil {
		t.Fatalf("graph projection failed: %v", err)
	}
	rootID := projected.Roots()[0]
	rootNode, ok := projected.Node(rootID)
	if !ok {
		t.Fatalf("root node missing")
	}
	items, _ := rootNode.SequenceItems()
	if len(items) != 1 || items[0] != rootID {
		t.Fatalf("graph cycle lost: %v", items)
	}
}

// TestAnchorSpans pins the exact raw anchor and alias lexeme spans.
func TestAnchorSpans(t *testing.T) {
	source := []byte("---\nroot: &node [one, *node]\n")
	doc := mustParse(t, string(source), Yaml12CoreV1)
	yamlDoc, _ := doc.Document(0)
	entry, _ := yamlDoc.Root().MappingEntry(0)
	sequence := entry.Value()
	anchorSpan, ok := sequence.AnchorSpan()
	if !ok || string(source[anchorSpan.StartByte():anchorSpan.EndByte()]) != "&node" {
		t.Fatalf("anchor span %v", anchorSpan)
	}
	alias, _ := doc.Alias(0)
	aliasSpan := alias.Span()
	if string(source[aliasSpan.StartByte():aliasSpan.EndByte()]) != "*node" {
		t.Fatalf("alias span %v", aliasSpan)
	}
	element, _ := sequence.SequenceItem(1)
	if element.Span() != aliasSpan {
		t.Fatalf("element span does not equal the alias span")
	}
	definition, ok := sequence.AnchorNodeRef()
	if !ok || definition.Role() != document.RoleYamlAnchorDefinition {
		t.Fatalf("anchor definition role")
	}
}

// TestCustomTagsAreNeverConstructed pins the no-custom-constructor safety
// boundary (RFC 0007 §5).
func TestCustomTagsAreNeverConstructed(t *testing.T) {
	doc := mustParse(t, "!application/object payload\n", Yaml12CoreV1)
	yamlDoc, _ := doc.Document(0)
	if yamlDoc.Root().Tag() != "!application/object" {
		t.Fatalf("custom tag %q", yamlDoc.Root().Tag())
	}
	scalar := rootScalar(t, doc)
	if scalar.Kind() != ScalarKindCustom {
		t.Fatalf("kind %s, want Custom", scalar.Kind())
	}
	_, err := doc.ProjectGraph()
	if err == nil {
		t.Fatalf("custom tag must fail graph projection")
	}
}

// TestExplicitTagValidation pins the explicit standard-tag grammar
// checks (lib.rs tests).
func TestExplicitTagValidation(t *testing.T) {
	mustFail(t, "!!int nope\n", Yaml12CoreV1, "yaml.scalar.invalid-explicit-tag@1")
	mustFail(t, "!!seq {a: b}\n", Yaml12CoreV1, "yaml.tag.kind-mismatch@1")
	mustFail(t, "!!float \"1\"\n", Yaml12CoreV1, "yaml.scalar.invalid-explicit-tag@1")
	mustFail(t, "!!set [a]\n", Yaml11CompatV1, "yaml.tag.kind-mismatch@1")
	doc := mustParse(t, "a: !!str 123\n", Yaml12CoreV1)
	yamlDoc, _ := doc.Document(0)
	entry, _ := yamlDoc.Root().MappingEntry(0)
	scalar, _ := entry.Value().Scalar()
	if scalar.Kind() != ScalarKindString || scalar.Canonical() != "123" {
		t.Fatalf("explicit !!str: %s %q", scalar.Kind(), scalar.Canonical())
	}
}

// TestStandardRepositoryTagsSurviveGraphProjection pins the retained
// repository tags (lib.rs test).
func TestStandardRepositoryTagsSurviveGraphProjection(t *testing.T) {
	doc := mustParse(t, "set: !!set {a: null}\nbinary: !!binary SGVsbG8=\ntime: !!timestamp 2001-12-15\n",
		Yaml11CompatV1)
	projected, err := doc.ProjectGraph()
	if err != nil {
		t.Fatalf("graph projection failed: %v", err)
	}
	foundSet, foundBinary := false, false
	for _, id := range projected.Nodes() {
		node, _ := projected.Node(id)
		if node.Tag() == "tag:yaml.org,2002:set" {
			foundSet = true
		}
		if node.Tag() == "tag:yaml.org,2002:binary" {
			foundBinary = true
		}
	}
	if !foundSet || !foundBinary {
		t.Fatalf("repository tags lost: set=%v binary=%v", foundSet, foundBinary)
	}
}

// TestTagDirectiveResolution pins the %TAG handle resolution.
func TestTagDirectiveResolution(t *testing.T) {
	doc := mustParse(t, "%TAG !e! tag:example.com,2026:\n---\n!e!thing value\n", Yaml12CoreV1)
	yamlDoc, _ := doc.Document(0)
	if yamlDoc.Root().Tag() != "tag:example.com,2026:thing" {
		t.Fatalf("resolved tag %q", yamlDoc.Root().Tag())
	}
	mustFail(t, "!e!thing value\n", Yaml12CoreV1, "yaml.parse.syntax@1")
	mustFail(t, "%TAG !! tag:example.com,2026:\n---\nx\n", Yaml12CoreV1,
		"yaml.parse.syntax@1")
	mustFail(t, "%TAG !e! a\n%TAG !e! b\n---\nx\n", Yaml12CoreV1, "yaml.parse.syntax@1")
}

// TestDuplicateAnchorsResolveToMostRecent pins the most-recent-preceding
// rule (RFC 0007 §8).
func TestDuplicateAnchorsResolveToMostRecent(t *testing.T) {
	doc := mustParse(t, "first: &x one\nsecond: &x two\ncopy: *x\n", Yaml12CoreV1)
	yamlDoc, _ := doc.Document(0)
	alias, _ := doc.Alias(0)
	second, _ := yamlDoc.Root().MappingEntry(1)
	if alias.Target().NodeRef() != second.Value().NodeRef() {
		t.Fatalf("alias does not resolve to the most recent anchor")
	}
}

// TestCrossDocumentAnchorIsolation pins the per-document anchor scope.
func TestCrossDocumentAnchorIsolation(t *testing.T) {
	mustFail(t, "---\n&x one\n---\n*x\n", Yaml12CoreV1, "yaml.parse.syntax@1")
}

// TestEmptyDocumentComposesNullScalar pins RFC 0007 §8.
func TestEmptyDocumentComposesNullScalar(t *testing.T) {
	doc := mustParse(t, "---\n---\n", Yaml12CoreV1)
	if doc.DocumentCount() != 2 {
		t.Fatalf("document count %d", doc.DocumentCount())
	}
	first, _ := doc.Document(0)
	scalar, ok := first.Root().Scalar()
	if !ok || scalar.Kind() != ScalarKindNull || scalar.Canonical() != "" {
		t.Fatalf("empty document root %v %q", scalar.Kind(), scalar.Canonical())
	}
}

// TestDuplicateMappingKeysSurvive pins the ordered arbitrary-key mapping
// facts (RFC 0007 §7).
func TestDuplicateMappingKeysSurvive(t *testing.T) {
	doc := mustParse(t, "? [a, b]\n: one\n? [a, b]\n: two\n", Yaml12CoreV1)
	yamlDoc, _ := doc.Document(0)
	length, _ := yamlDoc.Root().MappingLen()
	if length != 2 {
		t.Fatalf("mapping len %d", length)
	}
	first, _ := yamlDoc.Root().MappingEntry(0)
	second, _ := yamlDoc.Root().MappingEntry(1)
	if first.Key().NodeRef() == second.Key().NodeRef() {
		t.Fatalf("duplicate keys must keep distinct identities")
	}
	projected, err := doc.ProjectGraph()
	if err != nil {
		t.Fatalf("graph projection failed: %v", err)
	}
	if projected.NodeCount() != 9 {
		t.Fatalf("graph node count %d, want 9", projected.NodeCount())
	}
}

// TestQuotedTildeKeepsExactContent pins the M2-F2 regression through the
// alias path (lib.rs test).
func TestQuotedTildeKeepsExactContent(t *testing.T) {
	doc := mustParse(t, "a: &k \"~\"\nb: *k\n", Yaml12CoreV1)
	alias, _ := doc.Alias(0)
	scalar, _ := alias.Target().Scalar()
	if scalar.Kind() != ScalarKindString || scalar.Decoded() != "~" ||
		scalar.Style() != ScalarStyleDoubleQuoted {
		t.Fatalf("quoted tilde facts: %s %q %s", scalar.Kind(), scalar.Decoded(), scalar.Style())
	}
}

// TestEscapesAndMultilineQuoted pins the double-quoted escape surface and
// the quoted folding.
func TestEscapesAndMultilineQuoted(t *testing.T) {
	doc := mustParse(t, "\"a\\tb\\u0041\\x42\\U0001F600\\n\\\"\"\n", Yaml12CoreV1)
	scalar := rootScalar(t, doc)
	if scalar.Decoded() != "a\tbAB\U0001F600\n\"" {
		t.Errorf("escapes decoded %q", scalar.Decoded())
	}
	mustFail(t, "\"\\q\"\n", Yaml12CoreV1, "yaml.parse.syntax@1")
	mustFail(t, "\"\\uD800\"\n", Yaml12CoreV1, "yaml.parse.syntax@1")
	doc = mustParse(t, "'a\n\n  b'\n", Yaml12CoreV1)
	scalar = rootScalar(t, doc)
	if scalar.Decoded() != "a\nb" {
		t.Errorf("single-quoted folding %q", scalar.Decoded())
	}
	doc = mustParse(t, "\"a\\\n  b\"\n", Yaml12CoreV1)
	scalar = rootScalar(t, doc)
	if scalar.Decoded() != "ab" {
		t.Errorf("line continuation %q", scalar.Decoded())
	}
}

// TestFlowPlainScalarRules pins the flow plain-scalar stop rules.
func TestFlowPlainScalarRules(t *testing.T) {
	doc := mustParse(t, "[a:b, http://x/#part, 1, ]\n", Yaml12CoreV1)
	yamlDoc, _ := doc.Document(0)
	length, _ := yamlDoc.Root().SequenceLen()
	if length != 3 {
		t.Fatalf("sequence len %d", length)
	}
	item, _ := yamlDoc.Root().SequenceItem(0)
	scalar, _ := item.Node().Scalar()
	if scalar.Decoded() != "a:b" {
		t.Fatalf("flow plain %q", scalar.Decoded())
	}
}

// TestCompactNotation pins the compact sequence and mapping forms.
func TestCompactNotation(t *testing.T) {
	// A sequence after an implicit mapping needs an explicit document
	// marker (implicit documents cannot follow content).
	mustFail(t, "seq: [one, two]\n- key: value\n", Yaml12CoreV1,
		"yaml.parse.syntax@1")
	doc := mustParse(t, "- key: value\n  other: x\n", Yaml12CoreV1)
	yamlDoc, _ := doc.Document(0)
	item, _ := yamlDoc.Root().SequenceItem(0)
	length, _ := item.Node().MappingLen()
	if length != 2 {
		t.Fatalf("compact mapping continuation failed: %d", length)
	}
	doc = mustParse(t, "- key: value\n- - x\n", Yaml12CoreV1)
	yamlDoc, _ = doc.Document(0)
	length, _ = yamlDoc.Root().SequenceLen()
	if length != 2 {
		t.Fatalf("compact sequence item count: %d", length)
	}
	second, _ := yamlDoc.Root().SequenceItem(1)
	nested, _ := second.Node().SequenceLen()
	if nested != 1 {
		t.Fatalf("nested compact sequence: %d", nested)
	}
}

// TestCrLfRoundTrip pins the byte-exact newline preservation.
func TestCrLfRoundTrip(t *testing.T) {
	source := "a: 1\r\nb: two\r\n"
	doc := mustParse(t, source, Yaml12CoreV1)
	if string(doc.Render()) != source {
		t.Fatalf("CRLF render changed")
	}
}

// TestUtf16BomVariants pins the BOM policy for both UTF-16 endiannesses.
func TestUtf16BomVariants(t *testing.T) {
	le := []byte{0xff, 0xfe, 'a', 0, ':', 0, ' ', 0, '1', 0, '\n', 0}
	doc, failure := Parse(le, Yaml12CoreV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("UTF-16LE parse failed: %v", failure)
	}
	if string(doc.Render()) != string(le) {
		t.Fatalf("UTF-16LE render changed")
	}
	be := []byte{0xfe, 0xff, 0, 'a', 0, ':', 0, ' ', 0, '1', 0, '\n'}
	doc, failure = Parse(be, Yaml12CoreV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("UTF-16BE parse failed: %v", failure)
	}
	if string(doc.Render()) != string(be) {
		t.Fatalf("UTF-16BE render changed")
	}
	// No BOM means UTF-8: NUL bytes are valid UTF-8 content, and genuinely
	// invalid UTF-8 fails with the source contract code.
	doc, failure = Parse([]byte{'a', 0, ':', 0}, Yaml12CoreV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("no-BOM NUL bytes must parse as UTF-8: %v", failure)
	}
	_, failure = Parse([]byte{'a', 0xFF}, Yaml12CoreV1, document.DefaultParseLimits())
	if failure == nil || failure.Code() != "core.source.invalid-sequence@1" {
		t.Fatalf("invalid UTF-8 code: %v", failure)
	}
}

// TestLargeIntegersAreExact pins the arbitrary-precision contract.
func TestLargeIntegersAreExact(t *testing.T) {
	doc := mustParse(t, "123456789012345678901234567890\n", Yaml12CoreV1)
	scalar := rootScalar(t, doc)
	expected := new(big.Int)
	expected.SetString("123456789012345678901234567890", 10)
	result, ok := new(big.Int).SetString(scalar.Canonical(), 10)
	if !ok || result.Cmp(expected) != 0 {
		t.Fatalf("large integer canonical %q", scalar.Canonical())
	}
}

// TestNegativeAndPositiveCanonicalForms pins the sign canonicalization.
func TestNegativeAndPositiveCanonicalForms(t *testing.T) {
	cases := map[string]string{
		"-0x1F": "-31",
		"+0x1F": "31",
		"-0o17": "-15",
		"-1.5":  "-15e-1",
		"+1e3":  "1e3",
		"-0b11": "-3",
	}
	for source, canonical := range cases {
		scalar := rootScalar(t, mustParse(t, source, Yaml12CoreV1))
		if scalar.Canonical() != canonical {
			t.Errorf("%s canonical %q, want %q", source, scalar.Canonical(), canonical)
		}
	}
}

// TestBinaryScalarValidation pins the !!binary canonical rules.
func TestBinaryScalarValidation(t *testing.T) {
	doc := mustParse(t, "!!binary \"SGVs\n bG8=\"\n", Yaml12CoreV1)
	scalar := rootScalar(t, doc)
	if scalar.Kind() != ScalarKindBinary || scalar.Canonical() != "SGVsbG8=" {
		t.Fatalf("binary facts: %s %q", scalar.Kind(), scalar.Canonical())
	}
	mustFail(t, "!!binary SGVsbG9=\n", Yaml12CoreV1, "yaml.scalar.invalid-explicit-tag@1")
	mustFail(t, "!!binary =bad\n", Yaml12CoreV1, "yaml.scalar.invalid-explicit-tag@1")
}

// TestMergeTagIsRetainedWithoutExecution pins the !!merge boundary
// (RFC 0007 §5).
func TestMergeTagIsRetainedWithoutExecution(t *testing.T) {
	doc := mustParse(t, "a: !!merge x\n", Yaml12CoreV1)
	yamlDoc, _ := doc.Document(0)
	entry, _ := yamlDoc.Root().MappingEntry(0)
	if entry.Value().Tag() != "tag:yaml.org,2002:merge" {
		t.Fatalf("merge tag %q", entry.Value().Tag())
	}
	scalar, _ := entry.Value().Scalar()
	if scalar.Kind() != ScalarKindTagged {
		t.Fatalf("merge kind %s", scalar.Kind())
	}
}

// TestEmptyValueIsNull pins the empty-value null facts.
func TestEmptyValueIsNull(t *testing.T) {
	doc := mustParse(t, "a:\n", Yaml12CoreV1)
	yamlDoc, _ := doc.Document(0)
	entry, _ := yamlDoc.Root().MappingEntry(0)
	scalar, _ := entry.Value().Scalar()
	if scalar.Kind() != ScalarKindNull || scalar.Decoded() != "" || scalar.Canonical() != "" {
		t.Fatalf("empty value: %s %q %q", scalar.Kind(), scalar.Decoded(), scalar.Canonical())
	}
}

// TestDocumentEndMarkerAndDirectivePositions pins the stream grammar.
func TestDocumentEndMarkerAndDirectivePositions(t *testing.T) {
	mustFail(t, "a: 1\n...\nb: 2\n", Yaml12CoreV1, "yaml.parse.syntax@1")
	doc := mustParse(t, "a: 1\n...\n---\nb: 2\n", Yaml12CoreV1)
	if doc.DocumentCount() != 2 {
		t.Fatalf("document count %d", doc.DocumentCount())
	}
	doc = mustParse(t, "%YAML 1.2\n---\na: 1\n", Yaml12CoreV1)
	if doc.DocumentCount() != 1 {
		t.Fatalf("directive document count %d", doc.DocumentCount())
	}
	mustFail(t, "%YAML 1.2\n%YAML 1.2\n---\na\n", Yaml12CoreV1, "yaml.parse.syntax@1")
}

// TestNestingLimitSurfacesResourceLimit pins the depth limit code.
func TestNestingLimitSurfacesResourceLimit(t *testing.T) {
	limits := document.DefaultParseLimits()
	limits.MaxNestingDepth = 1
	_, failure := Parse([]byte("[[x]]"), Yaml12CoreV1, limits)
	if failure == nil || failure.Code() != "core.parse.resource-limit@1" {
		t.Fatalf("nesting limit: %v", failure)
	}
	limits = document.DefaultParseLimits()
	limits.MaxTokenCount = 2
	_, failure = Parse([]byte("x"), Yaml12CoreV1, limits)
	if failure == nil || failure.Code() != "core.parse.resource-limit@1" {
		t.Fatalf("token limit: %v", failure)
	}
	limits = document.DefaultParseLimits()
	limits.MaxNodeCount = 1
	_, failure = Parse([]byte("[one, two]"), Yaml12CoreV1, limits)
	if failure == nil || failure.Code() != "core.parse.resource-limit@1" {
		t.Fatalf("node limit: %v", failure)
	}
}

// TestNumberMagnitudeLimit pins the frozen cross-language number
// magnitude bound (maxNumberMagnitudeDigits, coefficient plus exponent)
// on the plain-scalar number paths: an over-limit lexeme fails with
// core.parse.resource-limit@1 before any big.Int allocation, and an
// exactly-at-limit lexeme resolves normally.
func TestNumberMagnitudeLimit(t *testing.T) {
	over := "1e" + strings.Repeat("9", maxNumberMagnitudeDigits)
	_, failure := Parse([]byte(over+"\n"), Yaml12CoreV1, document.DefaultParseLimits())
	if failure == nil || failure.Code() != "core.parse.resource-limit@1" ||
		failure.Diagnostics()[0].Arguments["name"] != "number-magnitude-digits" {
		t.Fatalf("over-limit float failure %v", failure)
	}

	at := "1e" + strings.Repeat("9", maxNumberMagnitudeDigits-1)
	doc := mustParse(t, at+"\n", Yaml12CoreV1)
	scalar := rootScalar(t, doc)
	if scalar.Kind() != ScalarKindFloat || scalar.Canonical() != at {
		t.Fatalf("at-limit float: kind %s canonical %q", scalar.Kind(), scalar.Canonical())
	}

	// The YAML 1.1 sexagesimal-float coefficient shares the bound.
	canonical, ok, failure := parseSexagesimalFloat("0:0." + strings.Repeat("9", maxNumberMagnitudeDigits+1))
	if ok || failure == nil || failure.Code() != "core.parse.resource-limit@1" ||
		failure.Diagnostics()[0].Arguments["name"] != "number-magnitude-digits" {
		t.Fatalf("sexagesimal over-limit: ok %v failure %v canonical %q", ok, failure, canonical)
	}
}
