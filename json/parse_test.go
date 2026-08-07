package json

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"consema.dev/consema/document"
)

func parseForTest(t *testing.T, source string, profile JsonProfile) *Document {
	t.Helper()
	doc, failure := Parse(context.Background(), []byte(source), profile, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("parse(%q) failed: %v", source, failure)
	}
	return doc
}

func mustForm(t *testing.T, doc *Document, status document.FormationStatus) {
	t.Helper()
	if doc.formationStatus != status {
		t.Fatalf("formation %s != %s", doc.formationStatus, status)
	}
}

func hasDiagnostic(doc *Document, code string) bool {
	for _, diagnostic := range doc.diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func TestStrictExactRoundtrip(t *testing.T) {
	for _, source := range []string{
		` {"a" : [1, 2]} `,
		`{"n":null,"b":true,"i":123,"d":1.25,"s":"x","a":[1],"o":{"k":"v"}}`,
		`"\u0041\uD83D\uDE00"`,
	} {
		doc := parseForTest(t, source, JsonProfileStrictV1)
		mustForm(t, doc, document.FormationStatusComplete)
		if got := string(doc.Render()); got != source {
			t.Fatalf("render %q != %q", got, source)
		}
	}
}

func TestJSONCCommentsAndTrailingComma(t *testing.T) {
	doc := parseForTest(t, "{/*x*/\"a\":1,}", JsonProfileJsoncBoundedV1)
	mustForm(t, doc, document.FormationStatusComplete)
	if got := string(doc.Render()); got != "{/*x*/\"a\":1,}" {
		t.Fatalf("render %q", got)
	}
}

func TestStrictRejectsJSONCSurface(t *testing.T) {
	doc := parseForTest(t, "// note\n{\"a\":1,}", JsonProfileStrictV1)
	mustForm(t, doc, document.FormationStatusRecovered)
	for _, code := range []string{"json.strict.comment-not-allowed@1", "json.strict.trailing-comma@1"} {
		if !hasDiagnostic(doc, code) {
			t.Errorf("missing diagnostic %s", code)
		}
	}
}

func TestStrictLeadingBomWarning(t *testing.T) {
	doc := parseForTest(t, "\xef\xbb\xbf{}", JsonProfileStrictV1)
	mustForm(t, doc, document.FormationStatusComplete)
	if !hasDiagnostic(doc, "json.strict.leading-bom@1") {
		t.Error("missing json.strict.leading-bom@1")
	}
	jsonc := parseForTest(t, "\xef\xbb\xbf{}", JsonProfileJsoncBoundedV1)
	mustForm(t, jsonc, document.FormationStatusComplete)
	if hasDiagnostic(jsonc, "json.strict.leading-bom@1") {
		t.Error("jsonc must not warn about a BOM")
	}
}

func TestRecoveryMissingClose(t *testing.T) {
	doc := parseForTest(t, "{\"a\":1", JsonProfileStrictV1)
	mustForm(t, doc, document.FormationStatusRecovered)
	if !hasDiagnostic(doc, "json.syntax.missing-object-close@1") {
		t.Error("missing missing-object-close diagnostic")
	}
	kind := doc.Root().Kind()
	if !kind.IsAvailable() || kind.Value() != JsonValueKindObject {
		t.Errorf("root kind %v", kind)
	}
}

func TestRecoveryMissingArrayClose(t *testing.T) {
	doc := parseForTest(t, "[1,2", JsonProfileStrictV1)
	mustForm(t, doc, document.FormationStatusRecovered)
	if !hasDiagnostic(doc, "json.syntax.missing-array-close@1") {
		t.Error("missing missing-array-close diagnostic")
	}
}

func TestDuplicateMembers(t *testing.T) {
	doc := parseForTest(t, "{\"a\":1,\"a\":2}", JsonProfileStrictV1)
	mustForm(t, doc, document.FormationStatusComplete)
	members := doc.Root().ObjectMembers()
	if !members.IsAvailable() || len(members.Value()) != 2 {
		t.Fatalf("members %v", members)
	}
	if members.Value()[0].NodeRef() == members.Value()[1].NodeRef() {
		t.Error("duplicate members must have distinct identities")
	}
	if !hasDiagnostic(doc, "json.object.duplicate-member@1") {
		t.Error("missing duplicate-member diagnostic")
	}
	for _, diagnostic := range doc.diagnostics {
		if diagnostic.Code == "json.object.duplicate-member@1" {
			if diagnostic.Arguments["name"] != "a" {
				t.Errorf("duplicate diagnostic arguments %v", diagnostic.Arguments)
			}
			if len(diagnostic.Related) != 1 || diagnostic.Related[0].Role != "first-member" {
				t.Errorf("duplicate diagnostic related %v", diagnostic.Related)
			}
		}
	}
}

func TestLosslessByteCoverage(t *testing.T) {
	source := " \n// c\n[1,] "
	doc := parseForTest(t, source, JsonProfileJsoncBoundedV1)
	pieces := doc.LosslessStructuralIndex().Pieces()
	next := 0
	for _, piece := range pieces {
		if piece.Span().StartByte() != next {
			t.Fatalf("gap or overlap at %d", next)
		}
		next = piece.Span().EndByte()
	}
	if next != len(source) {
		t.Fatalf("covered %d != %d", next, len(source))
	}
	if len(pieces) != len(doc.LosslessSyntaxKinds()) {
		t.Fatal("piece and kind counts differ")
	}
}

func TestLosslessSyntaxKindsJSONC(t *testing.T) {
	source := "\xef\xbb\xbf // line\n{\"x\":true,/* block */\"y\":null}"
	doc := parseForTest(t, source, JsonProfileJsoncBoundedV1)
	kinds := doc.LosslessSyntaxKinds()
	expected := []JsonSyntaxKind{
		JsonSyntaxKindBom, JsonSyntaxKindWhitespace, JsonSyntaxKindLineComment,
		JsonSyntaxKindWhitespace, JsonSyntaxKindLeftBrace, JsonSyntaxKindString,
		JsonSyntaxKindColon, JsonSyntaxKindTrue, JsonSyntaxKindComma,
		JsonSyntaxKindBlockComment, JsonSyntaxKindString, JsonSyntaxKindColon,
		JsonSyntaxKindNull, JsonSyntaxKindRightBrace,
	}
	if len(kinds) != len(expected) {
		t.Fatalf("kind count %d != %d: %v", len(kinds), len(expected), kinds)
	}
	for index := range expected {
		if kinds[index] != expected[index] {
			t.Errorf("kind %d = %s != %s", index, kinds[index], expected[index])
		}
	}
}

func TestJSON5FullSurface(t *testing.T) {
	source := "\ufeff{ // lead\nunquoted:'value',\\u0061:.5,hex:+0X10,trail:1.,exp:1.e+2,truth:true,nil:null,inf:-Infinity,nan:+NaN,}"
	doc := parseForTest(t, source, JsonProfileJson5StandardV1)
	mustForm(t, doc, document.FormationStatusComplete)
	members := doc.Root().ObjectMembers()
	if !members.IsAvailable() {
		t.Fatal("object members unavailable")
	}
	expectedNames := []string{"unquoted", "a", "hex", "trail", "exp", "truth", "nil", "inf", "nan"}
	expectedKinds := []JsonValueKind{
		JsonValueKindString, JsonValueKindDecimal, JsonValueKindInteger, JsonValueKindDecimal,
		JsonValueKindDecimal, JsonValueKindBoolean, JsonValueKindNull,
		JsonValueKindBinaryFloat64, JsonValueKindBinaryFloat64,
	}
	actual := members.Value()
	if len(actual) != len(expectedNames) {
		t.Fatalf("member count %d != %d", len(actual), len(expectedNames))
	}
	for index, member := range actual {
		if !member.Name().IsAvailable() || *member.Name().Value() != expectedNames[index] {
			t.Errorf("member %d name %v", index, member.Name())
		}
		kind := member.Value().Kind()
		if !kind.IsAvailable() || kind.Value() != expectedKinds[index] {
			t.Errorf("member %d kind %v", index, kind)
		}
	}
	contains := func(kind JsonSyntaxKind) bool {
		for _, item := range doc.LosslessSyntaxKinds() {
			if item == kind {
				return true
			}
		}
		return false
	}
	for _, kind := range []JsonSyntaxKind{JsonSyntaxKindBom, JsonSyntaxKindLineComment, JsonSyntaxKindIdentifier} {
		if !contains(kind) {
			t.Errorf("missing syntax kind %s", kind)
		}
	}
}

func TestJSON5Identifiers(t *testing.T) {
	source := "{$_:1,while:2,true:3,π:4,\\u0061:5,a\u200c:6,a\u200d:7}"
	doc := parseForTest(t, source, JsonProfileJson5StandardV1)
	mustForm(t, doc, document.FormationStatusComplete)
	members := doc.Root().ObjectMembers()
	if !members.IsAvailable() {
		t.Fatal("members unavailable")
	}
	expected := []string{"$_", "while", "true", "π", "a", "a\u200c", "a\u200d"}
	actual := members.Value()
	if len(actual) != len(expected) {
		t.Fatalf("member count %d != %d", len(actual), len(expected))
	}
	for index, member := range actual {
		if !member.Name().IsAvailable() || *member.Name().Value() != expected[index] {
			t.Errorf("member %d name %v != %q", index, member.Name(), expected[index])
		}
	}
}

func TestJSON5StringExtensions(t *testing.T) {
	source := "['single','\\x41','\\v','\\0','\\q','line\\\nnext','\\uD83D\\uDE00']"
	doc := parseForTest(t, source, JsonProfileJson5StandardV1)
	mustForm(t, doc, document.FormationStatusComplete)
	elements := doc.Root().ArrayElements()
	if !elements.IsAvailable() {
		t.Fatal("elements unavailable")
	}
	expected := []string{"single", "A", "\v", "\x00", "q", "linenext", "😀"}
	actual := elements.Value()
	if len(actual) != len(expected) {
		t.Fatalf("element count %d != %d", len(actual), len(expected))
	}
	for index, element := range actual {
		value := element.Value().AsString()
		if !value.IsAvailable() || *value.Value() != expected[index] {
			t.Errorf("element %d = %v != %q", index, value, expected[index])
		}
	}
}

func TestJSON5ExtendedWhitespaceAndComments(t *testing.T) {
	source := " \u00a0\u1680// line\u2028[1,/* block */2,]\u3000"
	doc := parseForTest(t, source, JsonProfileJson5StandardV1)
	mustForm(t, doc, document.FormationStatusComplete)
	kinds := doc.LosslessSyntaxKinds()
	seen := map[JsonSyntaxKind]bool{}
	for _, kind := range kinds {
		seen[kind] = true
	}
	for _, kind := range []JsonSyntaxKind{JsonSyntaxKindWhitespace, JsonSyntaxKindLineComment, JsonSyntaxKindBlockComment} {
		if !seen[kind] {
			t.Errorf("missing kind %s", kind)
		}
	}
}

func TestJSON5UnescapedSeparatorWarning(t *testing.T) {
	source := "'a\u2028b'"
	doc := parseForTest(t, source, JsonProfileJson5StandardV1)
	mustForm(t, doc, document.FormationStatusComplete)
	if !hasDiagnostic(doc, "json5.string.unescaped-line-separator@1") {
		t.Error("missing unescaped-line-separator diagnostic")
	}
}

func TestJSON5Rejections(t *testing.T) {
	cases := []struct {
		source string
		code   string
	}{
		{"{\\u0030bad:1}", "json5.syntax.invalid-identifier@1"},
		{"01", "json.syntax.invalid-number@1"},
		{"0x", "json.syntax.invalid-number@1"},
		{"'\\1'", "json.syntax.invalid-string-escape@1"},
		{"'\\uD800'", "json.syntax.invalid-string-escape@1"},
		{"1/* open", "json.syntax.unterminated-block-comment@1"},
		{`{a\u0021:1}`, "json5.syntax.invalid-identifier@1"},
	}
	for _, test := range cases {
		doc := parseForTest(t, test.source, JsonProfileJson5StandardV1)
		mustForm(t, doc, document.FormationStatusRecovered)
		if !hasDiagnostic(doc, test.code) {
			t.Errorf("%q: missing diagnostic %s (got %v)", test.source, test.code, codes(doc))
		}
	}
}

func codes(doc *Document) []string {
	output := make([]string, 0, len(doc.diagnostics))
	for _, diagnostic := range doc.diagnostics {
		output = append(output, diagnostic.Code)
	}
	return output
}

func TestJSON5Numbers(t *testing.T) {
	cases := []struct {
		source string
		kind   JsonValueKind
		bits   uint64
		text   string
	}{
		{"+Infinity", JsonValueKindBinaryFloat64, 0x7ff0_0000_0000_0000, ""},
		{"-NaN", JsonValueKindBinaryFloat64, 0xfff8_0000_0000_0000, ""},
		{"0xFFFFFFFFFFFFFFFFFFFFFFFF", JsonValueKindInteger, 0, "79228162514264337593543950335"},
	}
	for _, test := range cases {
		doc := parseForTest(t, test.source, JsonProfileJson5StandardV1)
		mustForm(t, doc, document.FormationStatusComplete)
		kind := doc.Root().Kind()
		if !kind.IsAvailable() || kind.Value() != test.kind {
			t.Errorf("%q kind %v != %v", test.source, kind, test.kind)
		}
		switch test.kind {
		case JsonValueKindBinaryFloat64:
			binary := doc.Root().AsBinaryFloat64()
			if !binary.IsAvailable() || binary.Value().Bits() != test.bits {
				t.Errorf("%q bits %v != %x", test.source, binary, test.bits)
			}
		case JsonValueKindInteger:
			integer := doc.Root().AsInteger()
			if !integer.IsAvailable() || integer.Value().String() != test.text {
				t.Errorf("%q integer %v != %q", test.source, integer, test.text)
			}
		}
	}
}

func TestJSON5LeadingTrailingExact(t *testing.T) {
	doc := parseForTest(t, "[.5,1.,1.e2]", JsonProfileJson5StandardV1)
	mustForm(t, doc, document.FormationStatusComplete)
	elements := doc.Root().ArrayElements()
	if !elements.IsAvailable() {
		t.Fatal("elements unavailable")
	}
	expected := [][2]string{{"5", "-1"}, {"1", "0"}, {"1", "2"}}
	actual := elements.Value()
	for index, element := range actual {
		decimal := element.Value().AsDecimal()
		if !decimal.IsAvailable() {
			t.Fatalf("element %d is not a decimal", index)
		}
		if decimal.Value().Coefficient().String() != expected[index][0] ||
			decimal.Value().Exponent().String() != expected[index][1] {
			t.Errorf("element %d = %s e %s != %v",
				index, decimal.Value().Coefficient(), decimal.Value().Exponent(), expected[index])
		}
	}
}

func TestStrictNumberSemantics(t *testing.T) {
	doc := parseForTest(t, "{\"d\":1.25,\"i\":-256}", JsonProfileStrictV1)
	members := doc.Root().ObjectMembers()
	if !members.IsAvailable() {
		t.Fatal("members unavailable")
	}
	decimal := members.Value()[0].Value().AsDecimal()
	if !decimal.IsAvailable() || decimal.Value().Coefficient().String() != "125" ||
		decimal.Value().Exponent().String() != "-2" {
		t.Errorf("decimal %v", decimal)
	}
	integer := members.Value()[1].Value().AsInteger()
	if !integer.IsAvailable() || integer.Value().String() != "-256" {
		t.Errorf("integer %v", integer)
	}
}

func TestInvalidUTF8IsFatal(t *testing.T) {
	_, failure := Parse(context.Background(), []byte{0xc3, 0x28}, JsonProfileStrictV1,
		document.DefaultParseLimits())
	if failure == nil || failure.Kind != FormationFailureSource {
		t.Fatalf("failure %v", failure)
	}
	if failure.Code() != "core.source.invalid-sequence@1" {
		t.Errorf("code %s", failure.Code())
	}
}

func TestParseLimits(t *testing.T) {
	limits := document.DefaultParseLimits()
	limits.MaxTokenCount = 2
	_, failure := Parse(context.Background(), []byte("[1,2]"), JsonProfileStrictV1, limits)
	if failure == nil || failure.Kind != FormationFailureResourceLimit ||
		failure.Name != "token-count" {
		t.Fatalf("token failure %v", failure)
	}

	limits = document.DefaultParseLimits()
	limits.MaxNestingDepth = 2
	_, failure = Parse(context.Background(), []byte("[[[[0]]]]"), JsonProfileJson5StandardV1, limits)
	if failure == nil || failure.Kind != FormationFailureResourceLimit ||
		failure.Name != "nesting-depth" {
		t.Fatalf("depth failure %v", failure)
	}

	limits = document.DefaultParseLimits()
	limits.MaxSourceBytes = 3
	_, failure = Parse(context.Background(), []byte("[1,2]"), JsonProfileStrictV1, limits)
	if failure == nil || failure.Kind != FormationFailureResourceLimit ||
		failure.Name != "source-bytes" {
		t.Fatalf("source failure %v", failure)
	}

	limits = document.DefaultParseLimits()
	limits.MaxNodeCount = 2
	_, failure = Parse(context.Background(), []byte("{\"a\":1}"), JsonProfileStrictV1, limits)
	if failure == nil || failure.Kind != FormationFailureResourceLimit ||
		failure.Name != "node-count" {
		t.Fatalf("node failure %v", failure)
	}
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, failure := Parse(ctx, []byte("[1,2,3]"), JsonProfileStrictV1, document.DefaultParseLimits())
	if failure == nil || failure.Kind != FormationFailureCancelled {
		t.Fatalf("failure %v", failure)
	}
}

func TestEmptySourceRecovery(t *testing.T) {
	doc := parseForTest(t, "", JsonProfileStrictV1)
	mustForm(t, doc, document.FormationStatusRecovered)
	if !hasDiagnostic(doc, "json.syntax.missing-value@1") {
		t.Error("missing missing-value diagnostic")
	}
}

func TestDiagnosticOrdering(t *testing.T) {
	// Duplicate member + trailing comma in one strict object: the semantic
	// duplicate diagnostic must sort before the conformance trailing-comma
	// diagnostic by start byte.
	doc := parseForTest(t, "{\"a\":1,\"a\":2,}", JsonProfileStrictV1)
	mustForm(t, doc, document.FormationStatusRecovered)
	duplicate := -1
	trailing := -1
	for index, diagnostic := range doc.diagnostics {
		switch diagnostic.Code {
		case "json.object.duplicate-member@1":
			duplicate = index
		case "json.strict.trailing-comma@1":
			trailing = index
		}
	}
	if duplicate < 0 || trailing < 0 || duplicate >= trailing {
		t.Fatalf("diagnostic order: %v", codes(doc))
	}
}

// json5CorpusCase is one record of the pinned upstream JSON5 reference
// corpus.
type json5CorpusCase struct {
	ID     string   `json:"id"`
	Source string   `json:"source"`
	Diag   []string `json:"diagnostic_contains"`
}

// TestJSON5ReferenceCorpus pins the upstream JSON5 v2.2.3 corpus: every
// valid case forms a Complete byte-exact document, every invalid case
// forms a Recovered document with diagnostics. The corpus file is the
// pinned fixture (conformance/corpora/json5-v2.2.3.json); the fixture is
// read-only and no expectations are copied into this test.
func TestJSON5ReferenceCorpus(t *testing.T) {
	path := filepath.Join("..", "..", "conformance", "corpora", "json5-v2.2.3.json")
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Suite   string            `json:"suite"`
		Valid   []json5CorpusCase `json:"valid"`
		Invalid []json5CorpusCase `json:"invalid"`
	}
	if err := json.Unmarshal(bytes, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.Suite != "consema.json5.reference-corpus@1" {
		t.Fatalf("corpus suite %q", corpus.Suite)
	}
	for _, test := range corpus.Valid {
		doc, failure := Parse(context.Background(), []byte(test.Source),
			JsonProfileJson5StandardV1, document.DefaultParseLimits())
		if failure != nil {
			t.Errorf("valid %s: parse failed: %v", test.ID, failure)
			continue
		}
		if string(doc.Render()) != test.Source {
			t.Errorf("valid %s: render differs", test.ID)
		}
		if doc.formationStatus != document.FormationStatusComplete {
			t.Errorf("valid %s: formation %s", test.ID, doc.formationStatus)
		}
		for _, code := range test.Diag {
			if !hasDiagnostic(doc, code) {
				t.Errorf("valid %s: missing diagnostic %s", test.ID, code)
			}
		}
	}
	for _, test := range corpus.Invalid {
		doc, failure := Parse(context.Background(), []byte(test.Source),
			JsonProfileJson5StandardV1, document.DefaultParseLimits())
		if failure != nil {
			t.Errorf("invalid %s: parse failed fatally: %v", test.ID, failure)
			continue
		}
		if string(doc.Render()) != test.Source {
			t.Errorf("invalid %s: render differs", test.ID)
		}
		if doc.formationStatus != document.FormationStatusRecovered {
			t.Errorf("invalid %s: formation %s", test.ID, doc.formationStatus)
		}
		if len(doc.diagnostics) == 0 {
			t.Errorf("invalid %s: no diagnostics", test.ID)
		}
	}
}

func TestRecoveredRegionSemantics(t *testing.T) {
	// A recovered string value with an invalid escape keeps its literal
	// span but loses native meaning.
	doc := parseForTest(t, "\"\\q\"", JsonProfileStrictV1)
	mustForm(t, doc, document.FormationStatusRecovered)
	if !hasDiagnostic(doc, "json.syntax.invalid-string-escape@1") {
		t.Error("missing invalid-string-escape")
	}
	kind := doc.Root().Kind()
	if kind.IsAvailable() {
		t.Errorf("root kind must be unavailable, got %v", kind)
	}
	availability := doc.Root().Kind()
	if availability.IsAvailable() || availability.Reason() != SemanticUnavailableInvalidLiteral {
		t.Errorf("unavailability reason %v", availability.Reason())
	}
}

func TestTrailingContentRecovery(t *testing.T) {
	doc := parseForTest(t, "{} {} ", JsonProfileStrictV1)
	mustForm(t, doc, document.FormationStatusRecovered)
	if !hasDiagnostic(doc, "json.syntax.trailing-content@1") {
		t.Error("missing trailing-content diagnostic")
	}
}

func TestProtocolDiagnosticValidation(t *testing.T) {
	// Every diagnostic the parser emits must be registry-valid; the
	// construction panics otherwise, so a broad parse is the check.
	parseForTest(t, "\xef\xbb\xbf // c\n{\"a\":1,\"a\":2,} ", JsonProfileStrictV1)
}

func TestDiagnosticTruncationMarker(t *testing.T) {
	limits := document.DefaultParseLimits()
	limits.MaxDiagnostics = 2
	doc, failure := Parse(context.Background(), []byte("[1,2,3,4,5]"),
		JsonProfileStrictV1, limits)
	if failure != nil {
		t.Fatal(failure)
	}
	// The truncation marker requires more than two diagnostics; a valid
	// array emits none, so construct a source with many recoveries.
	doc, failure = Parse(context.Background(), []byte("{{"),
		JsonProfileStrictV1, limits)
	if failure != nil {
		t.Fatal(failure)
	}
	if !hasDiagnostic(doc, "core.diagnostic.truncated@1") {
		t.Errorf("missing truncation marker: %v", codes(doc))
	}
}

func TestJSON5NumberGrammar(t *testing.T) {
	for _, valid := range []string{"+1", "1.", ".5", "1.e2", "0xdecaf", "-0X1", "Infinity", "+NaN"} {
		if !validJSON5Number(valid) {
			t.Errorf("%q must be valid", valid)
		}
	}
	for _, invalid := range []string{"01", ".", "0x", "1e", "0b1", "1_0", "+true"} {
		if validJSON5Number(invalid) {
			t.Errorf("%q must be invalid", invalid)
		}
	}
}

func TestStrictNumberGrammar(t *testing.T) {
	for _, valid := range []string{"0", "-0", "1.25", "1e2", "-1.2E-3"} {
		if !validJSONNumber(valid) {
			t.Errorf("%q must be valid", valid)
		}
	}
	for _, invalid := range []string{"01", "+1", "1.", "1e", "--1"} {
		if validJSONNumber(invalid) {
			t.Errorf("%q must be invalid", invalid)
		}
	}
}

func TestStringDecoder(t *testing.T) {
	if _, ok := decodeJSONString(`"\uD800"`, JsonProfileStrictV1); ok {
		t.Error("isolated surrogate must be rejected")
	}
	decoded, ok := decodeJSONString(`"\uD83D\uDE00"`, JsonProfileStrictV1)
	if !ok || decoded.value != "😀" {
		t.Errorf("surrogate pair decode %v %v", decoded, ok)
	}
	decoded, ok = decodeJSONString(`'single\x20\v\0\q\
line'`, JsonProfileJson5StandardV1)
	if !ok || decoded.value != "single \v\x00qline" {
		t.Errorf("json5 string decode %q %v", decoded.value, ok)
	}
	if decoded.hasUnescapedLineSeparator {
		t.Error("escaped continuation must not warn")
	}
}

// TestJSON5PackageFixture pins the upstream JSON5 v2.2.3 package fixture
// (conformance/fixtures/json5/package-json5-v2.2.3.json5): it forms a
// Complete byte-exact document and projects exactly. The fixture is
// read-only and referenced, never copied.
func TestJSON5PackageFixture(t *testing.T) {
	path := filepath.Join("..", "..", "conformance", "fixtures", "json5", "package-json5-v2.2.3.json5")
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, failure := Parse(context.Background(), bytes, JsonProfileJson5StandardV1,
		document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("fixture parse failed: %v", failure)
	}
	if doc.formationStatus != document.FormationStatusComplete {
		t.Fatalf("fixture formation %s", doc.formationStatus)
	}
	if string(doc.Render()) != string(bytes) {
		t.Fatal("fixture render differs")
	}
	request, buildFailure := NewProjectionRequestBuilder(ProjectionTargetJson5BestExactCoreV1).Build()
	if buildFailure != nil {
		t.Fatal(buildFailure)
	}
	if result := doc.Project(request); result.Failed != nil {
		t.Fatalf("fixture projection failed: %v", result.Failed.Diagnostics)
	}
}

func TestJsonSyntaxKindNames(t *testing.T) {
	for _, name := range []string{"Bom", "Whitespace", "LineComment", "BlockComment",
		"LeftBrace", "RightBrace", "LeftBracket", "RightBracket", "Colon", "Comma",
		"String", "Identifier", "Number", "True", "False", "Null", "ErrorRegion"} {
		kind, ok := JsonSyntaxKindFromName(name)
		if !ok || kind.AsStr() != name {
			t.Errorf("kind name %q", name)
		}
	}
	if _, ok := JsonSyntaxKindFromName("number"); ok {
		t.Error("lowercase kind must not resolve")
	}
}

func TestFormatFamilyAndProfile(t *testing.T) {
	doc := parseForTest(t, "{}", JsonProfileJson5StandardV1)
	if doc.FormatFamily().ID() != "json" || doc.FormatFamily().Version() != 1 {
		t.Errorf("family %v", doc.FormatFamily())
	}
	if doc.Profile().ID() != "json5.standard" || doc.Profile().Version() != 1 {
		t.Errorf("profile %v", doc.Profile())
	}
	if doc.SnapshotIdentity() == 0 {
		t.Error("snapshot identity must be non-zero")
	}
}

func TestAccessErrors(t *testing.T) {
	first := parseForTest(t, "{\"a\":1}", JsonProfileStrictV1)
	second := parseForTest(t, "{\"a\":1}", JsonProfileStrictV1)
	foreign := first.Root().NodeRef()
	index, err := second.validateRef(foreign, []document.NodeRole{document.RoleValue})
	if err == nil || index != 0 {
		t.Errorf("foreign ref must fail, got %d %v", index, err)
	}
	if accessError, ok := err.(*JsonAccessError); !ok || accessError.Kind != JsonAccessErrorWrongSnapshot {
		t.Errorf("access error %v", err)
	}
}

func TestSemanticUnavailableRecovery(t *testing.T) {
	// `{"a` forms a recovered document whose object root is complete at
	// the node level (edit.rs:2652-2687 regression).
	for _, profile := range []JsonProfile{JsonProfileStrictV1, JsonProfileJsoncBoundedV1,
		JsonProfileJson5StandardV1} {
		doc := parseForTest(t, "{\"a", profile)
		mustForm(t, doc, document.FormationStatusRecovered)
		if !hasDiagnostic(doc, "json.syntax.missing-object-close@1") {
			t.Errorf("%v: missing missing-object-close", profile)
		}
	}
}

func TestSyntaxKindsNeverGap(t *testing.T) {
	// The syntax kinds and pieces must align exactly (lib.rs:705-738).
	source := "\xef\xbb\xbf // line\n{\"x\":true,/* block */\"y\":null}"
	doc := parseForTest(t, source, JsonProfileJsoncBoundedV1)
	if len(doc.LosslessSyntaxKinds()) != len(doc.LosslessStructuralIndex().Pieces()) {
		t.Fatal("kind and piece counts differ")
	}
	if !strings.Contains(string(doc.Render()), "true") {
		t.Fatal("render sanity")
	}
}
