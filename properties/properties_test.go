package properties

import (
	"testing"

	"consema.dev/consema/document"
)

func TestJavaStringPreservesExactUnpairedCodeUnits(t *testing.T) {
	exact := NewJavaStringFromCodeUnits([]uint16{0x0041, 0xD800, 0x0042})
	units := exact.CodeUnits()
	if len(units) != 3 || units[0] != 0x0041 || units[1] != 0xD800 || units[2] != 0x0042 {
		t.Fatalf("code units %v", units)
	}
	be := exact.Utf16beBytes()
	if len(be) != 6 || string(be) != string([]byte{0x00, 0x41, 0xD8, 0x00, 0x00, 0x42}) {
		t.Fatalf("utf16be bytes %x", be)
	}
	if exact.Status() != JavaStringUnpairedSurrogate {
		t.Fatalf("status %s", exact.Status())
	}
	if _, err := exact.ToUnicode(); err == nil {
		t.Fatalf("unpaired surrogate converted")
	}

	scalar := NewJavaStringFromUnicode("😀")
	scalarUnits := scalar.CodeUnits()
	if len(scalarUnits) != 2 || scalarUnits[0] != 0xD83D || scalarUnits[1] != 0xDE00 {
		t.Fatalf("scalar units %v", scalarUnits)
	}
	text, err := scalar.ToUnicode()
	if err != nil || text != "😀" {
		t.Fatalf("scalar conversion %q %v", text, err)
	}
	if scalar.Status() != JavaStringWellFormedUnicode {
		t.Fatalf("scalar status %s", scalar.Status())
	}
}

func TestJavaStringEqualityUsesExactUnits(t *testing.T) {
	left := NewJavaStringFromUnicode("a")
	right := NewJavaStringFromCodeUnits([]uint16{0x0061})
	if !left.Equal(right) {
		t.Fatalf("exact units must be equal")
	}
	if left.Equal(NewJavaStringFromUnicode("b")) {
		t.Fatalf("different content must differ")
	}
}

func TestProfileAndEncodingSelectionMustMatch(t *testing.T) {
	_, failure := parse([]byte("k=v"), PropertiesLatin1V1,
		ReaderEncodingSelection(document.Utf8Encoding()), DefaultPropertiesParseLimits())
	if failure == nil {
		t.Fatalf("Latin-1 profile accepted Reader selection")
	}
	if failure.Code() != "java-properties.source.profile-encoding@1" {
		t.Fatalf("code %s", failure.Code())
	}
	_, failure = parse([]byte("k=v"), PropertiesReaderV1,
		ReaderEncodingSelection(document.BinaryEncoding()), DefaultPropertiesParseLimits())
	if failure == nil || failure.Code() != "java-properties.source.profile-encoding@1" {
		t.Fatalf("Reader profile accepted Binary selection: %v", failure)
	}
}

func TestFormationPreservesLinesContinuationsEscapesAndDuplicates(t *testing.T) {
	source := []byte("  # retained comment\\\r\nkey\\ with\\ spaces : first\\\r\n \tsecond\\u0021\ndup=first\rdup:last\nempty\nexplicit=")
	doc, failure := ParseReader(source, document.Utf8Encoding(), DefaultPropertiesParseLimits())
	if failure != nil {
		t.Fatalf("formation failed: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("status %s", doc.FormationStatus())
	}
	if string(doc.Render()) != string(source) {
		t.Fatalf("render identity")
	}
	if len(doc.naturalLines) != 7 || len(doc.logicalLines) != 5 ||
		len(doc.comments) != 1 || len(doc.properties) != 5 || len(doc.escapes) != 3 {
		t.Fatalf("counts %d/%d/%d/%d/%d", len(doc.naturalLines), len(doc.logicalLines),
			len(doc.comments), len(doc.properties), len(doc.escapes))
	}
	first := &doc.properties[0]
	key, _ := first.key.ToUnicode()
	value, _ := first.value.ToUnicode()
	if key != "key with spaces" || value != "firstsecond!" ||
		first.valueState != ValueStatePresent {
		t.Fatalf("first property %q=%q %s", key, value, first.valueState)
	}
	if len(first.keyFragments) != 1 || len(first.valueFragments) != 2 ||
		len(first.escapes) != 3 {
		t.Fatalf("fragment/escape counts %d/%d/%d", len(first.keyFragments),
			len(first.valueFragments), len(first.escapes))
	}
	if doc.properties[1].duplicateGroup == nil ||
		doc.properties[2].duplicateGroup == nil ||
		*doc.properties[1].duplicateGroup != *doc.properties[2].duplicateGroup {
		t.Fatalf("duplicate group missing")
	}
	if doc.properties[3].valueState != ValueStateImplicitEmpty ||
		doc.properties[4].valueState != ValueStateExplicitEmpty {
		t.Fatalf("empty states")
	}
	pieces := doc.index.Pieces()
	if pieces[0].Span().StartByte() != 0 ||
		pieces[len(pieces)-1].Span().EndByte() != len(source) {
		t.Fatalf("coverage")
	}
	for index := 1; index < len(pieces); index++ {
		if pieces[index-1].Span().EndByte() != pieces[index].Span().StartByte() {
			t.Fatalf("gap at %d", index)
		}
	}
}

func TestMalformedUnicodeEscapeRecoversWithoutPartialProperty(t *testing.T) {
	doc, failure := ParseReader([]byte("good=ok\nbad=\\u12G4\nafter=yes"),
		document.Utf8Encoding(), DefaultPropertiesParseLimits())
	if failure != nil {
		t.Fatalf("formation failed: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("status %s", doc.FormationStatus())
	}
	if len(doc.properties) != 2 || len(doc.errorLines) != 1 || len(doc.logicalLines) != 3 {
		t.Fatalf("counts %d/%d/%d", len(doc.properties), len(doc.errorLines),
			len(doc.logicalLines))
	}
	if doc.errorLines[0].code != "java-properties.parse.malformed-unicode-escape@1" {
		t.Fatalf("error code %s", doc.errorLines[0].code)
	}
	if len(doc.diagnostics) == 0 ||
		doc.diagnostics[0].Code != "java-properties.parse.malformed-unicode-escape@1" {
		t.Fatalf("diagnostic code")
	}
}

func TestUnicodeEscapePreservesAnUnpairedJavaSurrogate(t *testing.T) {
	doc, failure := ParseReader([]byte("key=\\uD800"), document.Utf8Encoding(),
		DefaultPropertiesParseLimits())
	if failure != nil {
		t.Fatalf("formation failed: %s", failure.Code())
	}
	value := doc.properties[0].value
	units := value.CodeUnits()
	if len(units) != 1 || units[0] != 0xD800 {
		t.Fatalf("units %v", units)
	}
	if value.Status() != JavaStringUnpairedSurrogate {
		t.Fatalf("status %s", value.Status())
	}
	if _, err := value.ToUnicode(); err == nil {
		t.Fatalf("unpaired converted")
	}
}

func TestLatin1TreatsUnicodeBomBytesAsContent(t *testing.T) {
	source := []byte{0xEF, 0xBB, 0xBF, 'k', '=', 'v'}
	doc, failure := ParseLatin1(source, DefaultPropertiesParseLimits())
	if failure != nil {
		t.Fatalf("formation failed: %s", failure.Code())
	}
	if doc.source.EncodingFacts().Bom() != nil {
		t.Fatalf("BOM detected")
	}
	key := doc.properties[0].key.CodeUnits()
	if len(key) != 4 || key[0] != 0x00EF || key[1] != 0x00BB ||
		key[2] != 0x00BF || key[3] != 0x006B {
		t.Fatalf("key units %v", key)
	}
	for _, kind := range doc.syntaxKinds {
		if kind == SyntaxKindBom {
			t.Fatalf("Bom syntax kind present")
		}
	}
}

func TestReaderHonorsAnExplicitMatchingUtf16Bom(t *testing.T) {
	source := []byte{0xFF, 0xFE, 'k', 0, '=', 0, 'v', 0}
	doc, failure := ParseReader(source, document.Utf16LeEncoding(), DefaultPropertiesParseLimits())
	if failure != nil {
		t.Fatalf("formation failed: %s", failure.Code())
	}
	if bom := doc.source.EncodingFacts().Bom(); bom == nil || *bom != document.BomUtf16Le {
		t.Fatalf("BOM %v", bom)
	}
	key, _ := doc.properties[0].key.ToUnicode()
	if key != "k" {
		t.Fatalf("key %q", key)
	}
	if string(doc.Render()) != string(source) {
		t.Fatalf("render identity")
	}
	if len(doc.syntaxKinds) == 0 || doc.syntaxKinds[0] != SyntaxKindBom {
		t.Fatalf("first kind %v", doc.syntaxKinds)
	}
}

func TestTerminalOddBackslashMatchesJdkLineReaderEofRule(t *testing.T) {
	source := append([]byte("key=value"), '\\')
	doc, failure := ParseReader(source, document.Utf8Encoding(), DefaultPropertiesParseLimits())
	if failure != nil {
		t.Fatalf("formation failed: %s", failure.Code())
	}
	value, _ := doc.properties[0].value.ToUnicode()
	if value != "value" {
		t.Fatalf("value %q", value)
	}
	if string(doc.Render()) != string(source) {
		t.Fatalf("render identity")
	}
	if len(doc.syntaxKinds) == 0 ||
		doc.syntaxKinds[len(doc.syntaxKinds)-1] != SyntaxKindContinuationMarker {
		t.Fatalf("terminal kind %v", doc.syntaxKinds)
	}
}

func TestUnicodeEscapeMayCrossAContinuationWithoutStealingItsSyntax(t *testing.T) {
	doc, failure := ParseReader([]byte("key=\\u00\\\n 41"), document.Utf8Encoding(),
		DefaultPropertiesParseLimits())
	if failure != nil {
		t.Fatalf("formation failed: %s", failure.Code())
	}
	value := doc.properties[0].value.CodeUnits()
	if len(value) != 1 || value[0] != 0x0041 {
		t.Fatalf("units %v", value)
	}
	if len(doc.properties[0].valueFragments) != 2 {
		t.Fatalf("fragments %d", len(doc.properties[0].valueFragments))
	}
	if len(doc.escapes) != 1 || doc.escapes[0].kind != EscapeKindUnicode {
		t.Fatalf("escapes")
	}
}

func TestEmptySourceHasNoPieces(t *testing.T) {
	doc, failure := ParseReader(nil, document.Utf8Encoding(), DefaultPropertiesParseLimits())
	if failure != nil {
		t.Fatalf("formation failed: %s", failure.Code())
	}
	if len(doc.index.Pieces()) != 0 || len(doc.naturalLines) != 0 {
		t.Fatalf("empty source facts")
	}
}

func TestFormatFamilyAndProfileFacts(t *testing.T) {
	doc, _ := ParseReader([]byte("a=1\n"), document.Utf8Encoding(), DefaultPropertiesParseLimits())
	if doc.FormatFamily().ID() != "java-properties" || doc.FormatFamily().Version() != 1 {
		t.Fatalf("family")
	}
	if doc.Profile().ID() != "java-properties.reader" || doc.Profile().Version() != 1 {
		t.Fatalf("profile")
	}
	if doc.SelectedProfile() != PropertiesReaderV1 {
		t.Fatalf("selected profile")
	}
	if doc.NodeRef().Role() != document.RolePropertiesDocument {
		t.Fatalf("root role")
	}
}
