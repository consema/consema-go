package yaml

import (
	"testing"

	"consema.dev/consema/document"
)

func parseSmoke(t *testing.T, source string, profile YamlProfile) *Document {
	t.Helper()
	doc, failure := Parse([]byte(source), profile, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("parse %q failed: %s (%s)", source, failure.Code(), failure.Error())
	}
	return doc
}

func TestSmokeScalars(t *testing.T) {
	doc := parseSmoke(t, "[yes, 017, 0o17, 1:02:03, 2001-12-15]", Yaml12CoreV1)
	root, ok := doc.Document(0)
	if !ok {
		t.Fatal("no document")
	}
	length, ok := root.Root().SequenceLen()
	if !ok || length != 5 {
		t.Fatalf("sequence len = %d, %v", length, ok)
	}
	for ordinal := 0; ordinal < 5; ordinal++ {
		item, ok := root.Root().SequenceItem(ordinal)
		if !ok {
			t.Fatalf("item %d missing", ordinal)
		}
		scalar, ok := item.Node().Scalar()
		if !ok {
			t.Fatalf("item %d not scalar", ordinal)
		}
		t.Logf("item %d: kind=%s decoded=%q canonical=%q", ordinal,
			scalar.Kind(), scalar.Decoded(), scalar.Canonical())
	}
}

func TestSmokeMultilinePlain(t *testing.T) {
	doc := parseSmoke(t, "---\nk:#foo\n &a !t s\n", Yaml12CoreV1)
	root, _ := doc.Document(0)
	scalar, ok := root.Root().Scalar()
	if !ok {
		t.Fatalf("root not scalar: %v", root.Root().Kind())
	}
	if scalar.Decoded() != "k:#foo &a !t s" {
		t.Fatalf("decoded = %q", scalar.Decoded())
	}
	if doc.AliasCount() != 0 {
		t.Fatalf("alias count = %d", doc.AliasCount())
	}
}

func TestSmokeAnchorAlias(t *testing.T) {
	doc := parseSmoke(t, "&root [one, *root]\n", Yaml12CoreV1)
	root, _ := doc.Document(0)
	node := root.Root()
	anchor, ok := node.Anchor()
	if !ok || anchor != "root" {
		t.Fatalf("anchor = %q %v", anchor, ok)
	}
	length, _ := node.SequenceLen()
	if length != 2 {
		t.Fatalf("len = %d", length)
	}
	item, _ := node.SequenceItem(1)
	alias, ok := item.Alias()
	if !ok || alias.Name() != "root" {
		t.Fatalf("alias = %v", ok)
	}
	if alias.Target().NodeRef() != node.NodeRef() {
		t.Fatalf("alias target mismatch")
	}
}

func TestSmokeStyles(t *testing.T) {
	source := "--- # doc\nplain: text\nsingle: 'x'\ndouble: \"y\"\nliteral: |-\n  a\nfolded: >+\n  b\nflow: [one, {k: v}]\n...\n"
	doc := parseSmoke(t, source, Yaml12CoreV1)
	pieces := doc.LosslessStructuralIndex().Pieces()
	if len(pieces) != 48 {
		t.Fatalf("piece count = %d, want 48", len(pieces))
	}
	kinds := doc.LosslessSyntaxKinds()
	required := []YamlSyntaxKind{SyntaxKindDocumentStart, SyntaxKindComment, SyntaxKindPlainScalar,
		SyntaxKindSingleQuotedScalar, SyntaxKindDoubleQuotedScalar, SyntaxKindLiteralBlockHeader,
		SyntaxKindFoldedBlockHeader, SyntaxKindBlockScalarContent, SyntaxKindFlowSequenceStart,
		SyntaxKindFlowMappingStart, SyntaxKindDocumentEnd}
	for _, kind := range required {
		found := false
		for _, actual := range kinds {
			if actual == kind {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing kind %s", kind)
		}
	}
	if string(doc.Render()) != source {
		t.Fatalf("render mismatch")
	}
}

func TestSmokeComments(t *testing.T) {
	doc := parseSmoke(t, "a: 1 # first\nb: 2 # second\n", Yaml12CoreV1)
	kinds := doc.LosslessSyntaxKinds()
	var ordinals []int
	for index, kind := range kinds {
		if kind == SyntaxKindComment {
			ordinals = append(ordinals, index)
		}
	}
	if len(ordinals) != 2 || ordinals[0] != 5 || ordinals[1] != 12 {
		t.Fatalf("comment ordinals = %v", ordinals)
	}
}

func TestSmokeBlockScalars(t *testing.T) {
	doc := parseSmoke(t, "a: |\n  ~\nb: >\n  null\n", Yaml12CoreV1)
	root, _ := doc.Document(0)
	entry, _ := root.Root().MappingEntry(0)
	scalar, _ := entry.Value().Scalar()
	if scalar.Decoded() != "~\n" || scalar.Kind() != ScalarKindString {
		t.Fatalf("literal scalar = %q %s", scalar.Decoded(), scalar.Kind())
	}
	entry, _ = root.Root().MappingEntry(1)
	scalar, _ = entry.Value().Scalar()
	if scalar.Decoded() != "null\n" {
		t.Fatalf("folded scalar = %q", scalar.Decoded())
	}
}

func TestSmokeExplicitKey(t *testing.T) {
	doc := parseSmoke(t, "? [a, b]\n: one\nk: two\nk: three\n", Yaml12CoreV1)
	root, _ := doc.Document(0)
	length, ok := root.Root().MappingLen()
	if !ok || length != 3 {
		t.Fatalf("mapping len = %d %v", length, ok)
	}
	entry, _ := root.Root().MappingEntry(0)
	if entry.Key().Kind() != NodeKindSequence {
		t.Fatalf("key kind = %v", entry.Key().Kind())
	}
	entry, _ = root.Root().MappingEntry(2)
	scalar, _ := entry.Value().Scalar()
	if scalar.Canonical() != "three" {
		t.Fatalf("value = %q", scalar.Canonical())
	}
}

func TestSmokeEmptyStream(t *testing.T) {
	doc := parseSmoke(t, "", Yaml12CoreV1)
	if doc.DocumentCount() != 0 {
		t.Fatalf("document count = %d", doc.DocumentCount())
	}
}

func TestSmokeMultiDocument(t *testing.T) {
	doc := parseSmoke(t, "---\n&a [one, *a]\n---\n{k: v}\n", Yaml12CoreV1)
	if doc.DocumentCount() != 2 || doc.AliasCount() != 1 {
		t.Fatalf("facts: docs=%d aliases=%d", doc.DocumentCount(), doc.AliasCount())
	}
}

func TestSmokeUndefinedAlias(t *testing.T) {
	_, failure := Parse([]byte("[*missing]\n"), Yaml12CoreV1, document.DefaultParseLimits())
	if failure == nil || failure.Code() != "yaml.parse.syntax@1" {
		t.Fatalf("failure = %v", failure)
	}
}

func TestSmokeProfileVersion(t *testing.T) {
	_, failure := Parse([]byte("%YAML 1.1\n---\nyes\n"), Yaml12CoreV1, document.DefaultParseLimits())
	if failure == nil || failure.Code() != "yaml.profile.version-directive@1" {
		t.Fatalf("failure = %v", failure)
	}
	doc := parseSmoke(t, "%YAML 1.1\n---\nyes\n", Yaml11CompatV1)
	root, _ := doc.Document(0)
	scalar, _ := root.Root().Scalar()
	if scalar.Canonical() != "true" {
		t.Fatalf("canonical = %q", scalar.Canonical())
	}
}

func TestSmokeUtf16(t *testing.T) {
	source := []byte{0xff, 0xfe, 'a', 0, ':', 0, ' ', 0, '1', 0, '\n', 0}
	doc, failure := Parse(source, Yaml12CoreV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("utf16 parse failed: %v", failure)
	}
	if string(doc.Render()) != string(source) {
		t.Fatalf("utf16 render mismatch")
	}
	if doc.DocumentCount() != 1 {
		t.Fatalf("docs = %d", doc.DocumentCount())
	}
	root, _ := doc.Document(0)
	entry, _ := root.Root().MappingEntry(0)
	scalar, _ := entry.Value().Scalar()
	if scalar.Canonical() != "1" {
		t.Fatalf("value = %q", scalar.Canonical())
	}
}

func TestSmokeNesting(t *testing.T) {
	limits := document.DefaultParseLimits()
	limits.MaxNestingDepth = 1
	_, failure := Parse([]byte("[[x]]"), Yaml12CoreV1, limits)
	if failure == nil || failure.Code() != "core.parse.resource-limit@1" {
		t.Fatalf("failure = %v", failure)
	}
}
