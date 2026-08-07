package hcl

import (
	"context"
	"testing"

	"consema.dev/consema/document"
)

// parseForms one document under the native profile.
func parseForms(t *testing.T, source string, profile HclProfile) *Document {
	t.Helper()
	doc, failure := Parse(context.Background(), []byte(source), profile,
		HclEncodingSelectionProfileDefault(), DefaultHclParseLimits())
	if failure != nil {
		t.Fatalf("fatal formation for %q: %s", source, failure.Code)
	}
	return doc
}

func codes(doc *Document) []string {
	var out []string
	for _, diagnostic := range doc.Diagnostics() {
		out = append(out, diagnostic.Code)
	}
	return out
}

func TestProfileIDsAreStable(t *testing.T) {
	if HclProfileNativeV1.ID() != document.NewProfileId("hcl.native", 1) {
		t.Fatal("native profile id mismatch")
	}
	if HclProfileTfvarsV1.ID() != document.NewProfileId("hcl.tfvars", 1) {
		t.Fatal("tfvars profile id mismatch")
	}
}

func TestSyntaxKindClosedSetHasExactlyThirtyKinds(t *testing.T) {
	seen := make(map[HclSyntaxKind]bool)
	for kind := HclSyntaxKindWhitespace; kind <= HclSyntaxKindErrorRegion; kind++ {
		spelling := kind.AsStr()
		resolved, ok := HclSyntaxKindFromName(spelling)
		if !ok || resolved != kind {
			t.Fatalf("kind %d does not round-trip its spelling %q", kind, spelling)
		}
		if seen[kind] {
			t.Fatalf("kind %d repeated", kind)
		}
		seen[kind] = true
	}
	if len(seen) != 30 {
		t.Fatalf("kind set has %d kinds, want 30", len(seen))
	}
	if _, ok := HclSyntaxKindFromName("Bom"); ok {
		t.Fatal("Bom kind must not exist")
	}
}

func TestParseLimitsDefaultsAreFrozen(t *testing.T) {
	limits := DefaultHclParseLimits()
	if limits.Common != document.DefaultParseLimits() {
		t.Fatal("common limits differ from the document defaults")
	}
	if limits.MaxDecodedUTF8Bytes != 128*1024*1024 || limits.MaxDecodedScalars != 64*1024*1024 {
		t.Fatal("decoded limits drift")
	}
	if limits.MaxBodyDepth != 128 || limits.MaxExpressionDepth != 24 {
		t.Fatal("depth limits drift")
	}
	if limits.MaxNumberDigits != 100_000 {
		t.Fatal("number digit limit drift")
	}
}

func TestCanonicalDecimalFoldsSpellingsToCanonicalForm(t *testing.T) {
	for spelling, canonical := range map[string]string{
		"1.50":      "1.5",
		"1.5":       "1.5",
		"15e-1":     "1.5",
		"15E-1":     "1.5",
		"1e3":       "1000",
		"007":       "7",
		"0":         "0",
		"0.0":       "0",
		"1.0":       "1",
		"0.5":       "0.5",
		"1.2300":    "1.23",
		"1e-3":      "0.001",
		"1.5e2":     "150",
		"100e-2":    "1",
		"0e5":       "0",
		"1e30":      "1000000000000000000000000000000",
		"123.456e3": "123456",
	} {
		got, ok := CanonicalDecimal(spelling)
		if !ok || got != canonical {
			t.Errorf("canonical decimal %q = %q, %v; want %q", spelling, got, ok, canonical)
		}
	}
}

func TestCanonicalDecimalRejectsInvalidSpellings(t *testing.T) {
	for _, spelling := range []string{
		"", "abc", "1.2.3", "1e", "1e+", "e3", ".5", "5.", "+1", "-1",
		"1_000", "0x10", "0b10", "0o7", "1 ", " 1", "1e1e1",
		"1e99999999999999999999", "1e-9223372036854775808",
	} {
		if canonical, ok := CanonicalDecimal(spelling); ok {
			t.Errorf("canonical decimal %q = %q; want rejection", spelling, canonical)
		}
	}
}

func TestNumberEqualityIsCanonicalDecimalEquality(t *testing.T) {
	first, _ := HclNumberFromSpelling(document.Span{}, "1.50", 100_000)
	second, _ := HclNumberFromSpelling(document.Span{}, "1.5", 100_000)
	third, _ := HclNumberFromSpelling(document.Span{}, "15e-1", 100_000)
	if !first.Equal(&second) || !first.Equal(&third) {
		t.Fatal("equal canonical decimals must be equal")
	}
}

func TestRenderIsByteExactForCompleteAndRecovered(t *testing.T) {
	for _, source := range []string{
		"a = 1\n",
		"a = 1\nb {\n  c = 2\n}\n",
		"# comment\na = \"${b.c}\"\n",
	} {
		doc := parseForms(t, source, HclProfileNativeV1)
		if string(doc.Render()) != source {
			t.Fatalf("render %q != source %q", string(doc.Render()), source)
		}
	}
	recovered := parseForms(t, "a = 1\rb = 2\n", HclProfileNativeV1)
	if recovered.FormationStatus() != document.FormationStatusRecovered {
		t.Fatal("lone CR must be Recovered")
	}
	if !containsCode(codes(recovered), "hcl.parse.lone-cr@1") {
		t.Fatalf("lone CR diagnostic missing: %v", codes(recovered))
	}
}

func containsCode(haystack []string, needle string) bool {
	for _, code := range haystack {
		if code == needle {
			return true
		}
	}
	return false
}

func TestBomIsRecoveredWithByteOrderMarkCode(t *testing.T) {
	source := "\uFEFFa = 1\n"
	for _, profile := range []HclProfile{HclProfileNativeV1, HclProfileTfvarsV1} {
		doc := parseForms(t, source, profile)
		if doc.FormationStatus() != document.FormationStatusRecovered {
			t.Fatal("BOM must be Recovered")
		}
		if !containsCode(codes(doc), "hcl.parse.byte-order-mark@1") {
			t.Fatalf("BOM diagnostic missing: %v", codes(doc))
		}
		if string(doc.Render()) != source {
			t.Fatal("BOM bytes must stay content")
		}
	}
}

func TestInvalidUTF8IsAFatalFormationFailure(t *testing.T) {
	for _, profile := range []HclProfile{HclProfileNativeV1, HclProfileTfvarsV1} {
		_, failure := Parse(context.Background(), []byte("a = \xFF\n"), profile,
			HclEncodingSelectionProfileDefault(), DefaultHclParseLimits())
		if failure == nil || failure.DiagnosticCode() != "hcl.parse.invalid-utf8@1" {
			t.Fatalf("invalid UTF-8 must be fatal with hcl.parse.invalid-utf8@1, got %v", failure)
		}
	}
}

func TestTfvarsTopLevelBlockIsRecoveredAndPreserved(t *testing.T) {
	source := "a = 1\nb {\n  c = 2\n}\n"
	doc := parseForms(t, source, HclProfileTfvarsV1)
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatal("tfvars top-level block must be Recovered")
	}
	if !containsCode(codes(doc), "hcl.tfvars.block-not-allowed@1") {
		t.Fatalf("tfvars block diagnostic missing: %v", codes(doc))
	}
	if len(doc.Document().Body().Items()) != 2 {
		t.Fatal("the rejected block stays a native item")
	}
	if string(doc.Render()) != source {
		t.Fatal("render must stay byte-exact")
	}
	if len(doc.ErrorRegions()) != 0 {
		t.Fatal("the gate emits diagnostics, never error regions")
	}
}

func TestDuplicateAttributeIsRecovered(t *testing.T) {
	doc := parseForms(t, "a = 1\na = 2\n", HclProfileNativeV1)
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatal("duplicate attribute must be Recovered")
	}
	if !containsCode(codes(doc), "hcl.parse.duplicate-attribute@1") {
		t.Fatalf("duplicate attribute diagnostic missing: %v", codes(doc))
	}
	if len(doc.Document().Body().Items()) != 1 {
		t.Fatal("the duplicate never enters the native model")
	}
}

func TestIdentifierMatrix(t *testing.T) {
	for _, source := range []string{"foo-bar = 1\n", "变量 = 2\n", "true = 1\n", "true { x = 1 }\n"} {
		doc := parseForms(t, source, HclProfileNativeV1)
		if doc.FormationStatus() != document.FormationStatusComplete {
			t.Fatalf("%q must be Complete, got %v", source, codes(doc))
		}
	}
	for _, source := range []string{"_foo = 1\n", "a = _bar\n"} {
		doc := parseForms(t, source, HclProfileNativeV1)
		if doc.FormationStatus() != document.FormationStatusRecovered {
			t.Fatalf("%q must be Recovered", source)
		}
		if !containsCode(codes(doc), "hcl.parse.identifier@1") {
			t.Fatalf("%q: identifier diagnostic missing: %v", source, codes(doc))
		}
	}
}

func TestHeredocClosingLineWhitespace(t *testing.T) {
	source := "plain = <<EOT\nalpha\nbeta\nEOT\n" +
		"indented = <<-EOT\n    one\n      two\n    EOT\n" +
		"notclosing = <<EOT\nEOT has content\nEOT\n" +
		"trimmed = <<EOT\ntail\nEOT  \n"
	doc := parseForms(t, source, HclProfileNativeV1)
	if doc.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("heredoc matrix must be Complete, got %v", codes(doc))
	}
}

func TestExpressionKindNamesRoundTrip(t *testing.T) {
	doc := parseForms(t, "a = 1 + foo.bar\n", HclProfileNativeV1)
	expression := doc.Document().Body().Items()[0].AsAttribute().Expression()
	if expression.Kind().Name() != HclExpressionKindNameBinary {
		t.Fatal("binary kind expected")
	}
	children := expression.Children()
	if len(children) != 2 || children[0].Kind().Name() != HclExpressionKindNameNumber ||
		children[1].Kind().Name() != HclExpressionKindNameTraversal {
		t.Fatalf("children mismatch: %d", len(children))
	}
}

func TestLiteralCompleteBoundary(t *testing.T) {
	cases := []struct {
		source  string
		literal bool
	}{
		{"a = -1\n", true},
		{"a = 1 + 2\n", false},
		{"a = {1 = \"a\"}\n", true},
		{"a = \"no interpolation\"\n", true},
		{"a = \"x${y}\"\n", false},
		{"a = <<EOT\nplain\nEOT\n", true},
		{"a = <<EOT\nhi ${x}\nEOT\n", false},
		{"a = var.name\n", false},
		{"a = (1)\n", true},
	}
	for _, test := range cases {
		doc := parseForms(t, test.source, HclProfileNativeV1)
		expression := doc.Document().Body().Items()[0].AsAttribute().Expression()
		if IsLiteralComplete(expression) != test.literal {
			t.Errorf("%q literal-complete = %v, want %v", test.source,
				IsLiteralComplete(expression), test.literal)
		}
	}
}

func TestEncodingSelectionRejectsNonUTF8(t *testing.T) {
	if _, ok := HclEncodingSelectionProfileDefault().Validate(); !ok {
		t.Fatal("profile default must validate")
	}
	if _, ok := HclEncodingSelectionExplicit(document.Utf8Encoding()).Validate(); !ok {
		t.Fatal("explicit UTF-8 must validate")
	}
	if _, ok := HclEncodingSelectionExplicit(document.Utf16LeEncoding()).Validate(); ok {
		t.Fatal("non-UTF-8 explicit selection must be rejected")
	}
}

func TestParseRejectsNonUTF8SelectionBeforeAnyByteIsRead(t *testing.T) {
	for _, profile := range []HclProfile{HclProfileNativeV1, HclProfileTfvarsV1} {
		_, failure := Parse(context.Background(), []byte("a = 1\n"), profile,
			HclEncodingSelectionExplicit(document.Utf16LeEncoding()), DefaultHclParseLimits())
		if failure == nil || failure.DiagnosticCode() != "hcl.parse.encoding@1" {
			t.Fatalf("non-UTF-8 selection must fail with hcl.parse.encoding@1, got %v", failure)
		}
	}
}
