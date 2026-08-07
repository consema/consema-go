package ini

import (
	"testing"

	"consema.dev/consema/document"
)

// parseText forms one UTF-8 source under one profile with the default
// selection and limits.
func parseText(t *testing.T, profile IniProfile, source string) *Document {
	t.Helper()
	doc, failure := Parse([]byte(source), profile, IniEncodingProfileDefault(),
		DefaultIniParseLimits())
	if failure != nil {
		t.Fatalf("Parse(%s) failed: %s", profile, failure.Diagnostics()[0].Code)
	}
	return doc
}

// parseFailure forms one source and returns the fatal failure.
func parseFailure(t *testing.T, profile IniProfile, source []byte,
	selection IniEncodingSelection) *FormationFailure {
	t.Helper()
	_, failure := Parse(source, profile, selection, DefaultIniParseLimits())
	if failure == nil {
		t.Fatalf("Parse(%s) unexpectedly succeeded", profile)
	}
	return failure
}

func utf16leBOM(text string) []byte {
	output := []byte{0xFF, 0xFE}
	for _, unit := range stringToUTF16(text) {
		output = append(output, byte(unit), byte(unit>>8))
	}
	return output
}

func stringToUTF16(text string) []uint16 {
	var units []uint16
	for _, character := range text {
		if character >= 0x10000 {
			value := character - 0x10000
			units = append(units, uint16(0xD800+value>>10), uint16(0xDC00+value&0x3FF))
			continue
		}
		units = append(units, uint16(character))
	}
	return units
}

// assertExactCoverage checks the exhaustive piece coverage of one
// non-empty source.
func assertExactCoverage(t *testing.T, doc *Document) {
	t.Helper()
	pieces := doc.LosslessStructuralIndex().Pieces()
	kinds := doc.LosslessSyntaxKinds()
	if len(pieces) != len(kinds) {
		t.Fatalf("piece/kind count mismatch: %d != %d", len(pieces), len(kinds))
	}
	if len(pieces) == 0 {
		if doc.Source().Len() != 0 {
			t.Fatalf("empty coverage for non-empty source")
		}
		return
	}
	if pieces[0].Span().StartByte() != 0 ||
		pieces[len(pieces)-1].Span().EndByte() != doc.Source().Len() {
		t.Fatalf("coverage does not span the source")
	}
	for index := 1; index < len(pieces); index++ {
		if pieces[index-1].Span().EndByte() != pieces[index].Span().StartByte() {
			t.Fatalf("coverage gap at piece %d", index)
		}
	}
}

func TestPortableProfileIsLosslessAndKeepsEmptyDistinct(t *testing.T) {
	source := "; heading\r\n[core]\r\nname=value\nempty="
	doc := parseText(t, PortableV1, source)
	if string(doc.Render()) != source {
		t.Fatalf("render is not byte-identical")
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("formation %s, want Complete", doc.FormationStatus())
	}
	if len(doc.PhysicalLines()) != 4 || len(doc.LogicalLines()) != 3 {
		t.Fatalf("line counts differed")
	}
	sections := doc.Sections()
	if len(sections) != 1 || sections[0].Name() != "core" {
		t.Fatalf("section facts differed")
	}
	entries := doc.Entries()
	if len(entries) != 2 || entries[0].Value() != "value" ||
		entries[0].ValueState() != ValueStatePresent ||
		entries[1].Value() != "" || entries[1].ValueState() != ValueStateEmpty {
		t.Fatalf("entry facts differed")
	}
	assertExactCoverage(t, doc)
}

func TestPortableProfileRecoversCounterexamples(t *testing.T) {
	cases := []struct {
		source string
		code   string
	}{
		{"", "ini.parse.missing-section@1"},
		{"; only\n", "ini.parse.missing-section@1"},
		{"[s]\nkey:value\n", "ini.parse.missing-delimiter@1"},
		{"[s]\nkey=é\n", "ini.parse.invalid-character@1"},
		{"[s\n", "ini.parse.malformed-section@1"},
		{"bare\n", "ini.parse.missing-delimiter@1"},
		{"a=1\n", "ini.parse.missing-section@1"},
	}
	for _, item := range cases {
		doc := parseText(t, PortableV1, item.source)
		if doc.FormationStatus() != document.FormationStatusRecovered {
			t.Fatalf("%q: formation %s, want Recovered", item.source, doc.FormationStatus())
		}
		found := false
		for _, diagnostic := range doc.Diagnostics() {
			if diagnostic.Code == item.code {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%q: diagnostic %s missing", item.source, item.code)
		}
	}
	missing := parseText(t, PortableV1, "[s]\nkey:value\n")
	if len(missing.Entries()) != 0 || len(missing.ErrorLines()) != 1 {
		t.Fatalf("recovery fabricated entries or error records")
	}
	if missing.ErrorLines()[0].Code() != "ini.parse.missing-delimiter@1" {
		t.Fatalf("error code differed")
	}

	failure := parseFailure(t, PortableV1, []byte{0xEF, 0xBB, 0xBF, '[', 's', ']', '\n'},
		IniEncodingProfileDefault())
	if failure.Code() != "ini.profile.encoding@1" {
		t.Fatalf("BOM portable failure code %s", failure.Code())
	}
}

func TestWindowsProfileAcceptsUtf16AndMarksCaseAmbiguity(t *testing.T) {
	source := utf16leBOM("[Main]\r\n Name =\" value \"\r\n[main]\r\nNAME=two")
	doc, failure := Parse(source, WindowsV1, IniEncodingProfileDefault(),
		DefaultIniParseLimits())
	if failure != nil {
		t.Fatalf("parse failed: %s", failure.Diagnostics()[0].Code)
	}
	if string(doc.Render()) != string(source) {
		t.Fatalf("render is not byte-identical")
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("formation %s, want Complete", doc.FormationStatus())
	}
	facts := doc.Source().EncodingFacts()
	if facts.Bom() == nil || *facts.Bom() != document.BomUtf16Le {
		t.Fatalf("BOM fact differed")
	}
	sections := doc.Sections()
	if len(sections) != 2 || sections[0].ComparisonName() != "main" ||
		!sameGroup(sections[0].DuplicateGroup(), sections[1].DuplicateGroup()) {
		t.Fatalf("section case-equivalence facts differed")
	}
	entries := doc.Entries()
	if entries[0].Key() != "Name" || entries[0].Value() != " value " ||
		entries[0].QuoteStyle() != QuoteStyleDouble ||
		!sameGroup(entries[0].DuplicateGroup(), entries[1].DuplicateGroup()) {
		t.Fatalf("entry facts differed")
	}
	found := false
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == "ini.formation.case-collision@1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("case-collision diagnostic missing")
	}
	assertExactCoverage(t, doc)

	bigEndian := []byte{0xFE, 0xFF, 0x00, '[', 0x00, 's', 0x00, ']'}
	parseFailure(t, WindowsV1, bigEndian, IniEncodingProfileDefault())
}

func sameGroup(left, right *uint32) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func TestWindowsProfileRequiresExplicitCodePageForNonASCII(t *testing.T) {
	page, ok := document.WindowsCodePageFromNumber(1252)
	if !ok {
		t.Fatalf("code page 1252 must be admitted")
	}
	source := []byte{'[', 's', ']', '\r', '\n', 'k', '=', 0x80}
	doc, failure := Parse(source, WindowsV1,
		IniEncodingExplicit(document.WindowsCodePageEncoding(page)), DefaultIniParseLimits())
	if failure != nil {
		t.Fatalf("parse failed: %s", failure.Diagnostics()[0].Code)
	}
	if doc.Entries()[0].Value() != "€" {
		t.Fatalf("code-page value differed")
	}
	facts := doc.Source().EncodingFacts()
	if facts.BomPolicy() != document.BomPolicyTreatAsContent {
		t.Fatalf("BOM policy differed")
	}
	override := facts.CallerOverride()
	if override == nil || !override.Equal(document.WindowsCodePageEncoding(page)) {
		t.Fatalf("caller override differed")
	}
	assertExactCoverage(t, doc)

	utf8 := []byte("[s]\nk=é")
	parseFailure(t, WindowsV1, utf8, IniEncodingProfileDefault())
}

func TestPythonProfileKeepsDefaultRawValuesAndMultilineIdentity(t *testing.T) {
	source := "[DEFAULT]\nRoot = raw%(x)s\n[Sec]\nKey: first\n    second\n\n    third\nOther = #literal ;literal"
	doc := parseText(t, PythonConfigParserV1, source)
	if doc.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("formation %s, want Complete", doc.FormationStatus())
	}
	sections := doc.Sections()
	if len(sections) != 2 || !sections[0].IsDefault() ||
		sections[0].NodeRef().Role() != document.RoleIniDefaultSection {
		t.Fatalf("default section facts differed")
	}
	entries := doc.Entries()
	if entries[0].Value() != "raw%(x)s" || entries[1].ComparisonKey() != "key" ||
		entries[1].Value() != "first\nsecond\n\nthird" || entries[2].Value() != "#literal ;literal" {
		t.Fatalf("entry facts differed")
	}
	logical, ok := doc.LogicalLine(entries[1].LogicalLine())
	if !ok || len(logical.PhysicalLines()) != 4 {
		t.Fatalf("continuation physical lines differed")
	}
	assertExactCoverage(t, doc)
}

func TestPythonDuplicatesRecoverAndHandlesAreSnapshotBound(t *testing.T) {
	doc := parseText(t, PythonConfigParserV1, "[S]\nKey=1\nkey=2\n[S]\n")
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("formation %s, want Recovered", doc.FormationStatus())
	}
	entries := doc.Entries()
	if entries[0].DuplicateGroup() == nil ||
		!sameGroup(entries[0].DuplicateGroup(), entries[1].DuplicateGroup()) {
		t.Fatalf("duplicate group facts differed")
	}
	foundCollision := false
	foundSection := false
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == "ini.formation.case-collision@1" {
			foundCollision = true
		}
		if diagnostic.Code == "ini.formation.duplicate-section@1" {
			foundSection = true
		}
	}
	if !foundCollision || !foundSection {
		t.Fatalf("recovery diagnostics missing: %v %v", foundCollision, foundSection)
	}
	other := parseText(t, PythonConfigParserV1, "[T]\nx=1\n")
	if _, ok := other.Entry(doc.Entries()[0].NodeRef()); ok {
		t.Fatalf("cross-snapshot entry resolve succeeded")
	}
}

func TestEveryFormationLimitFailsWithoutADocument(t *testing.T) {
	limits := DefaultIniParseLimits()
	limits.MaxPhysicalLines = 1
	if _, failure := Parse([]byte("[s]\nk=1\n"), PortableV1, IniEncodingProfileDefault(),
		limits); failure == nil {
		t.Fatalf("physical-lines limit unexpectedly succeeded")
	}
	limits = DefaultIniParseLimits()
	limits.MaxContinuationLines = 0
	if _, failure := Parse([]byte("[s]\nk=one\n  two\n"), PythonConfigParserV1,
		IniEncodingProfileDefault(), limits); failure == nil {
		t.Fatalf("continuation-lines limit unexpectedly succeeded")
	}
	limits = DefaultIniParseLimits()
	limits.MaxLogicalLineBytes = 8
	if _, failure := Parse([]byte("[s]\nk=one\n  two\n"), PythonConfigParserV1,
		IniEncodingProfileDefault(), limits); failure == nil {
		t.Fatalf("logical-line-bytes limit unexpectedly succeeded")
	}
}

func TestOptionxformUnicode16Spellings(t *testing.T) {
	if optionxform("Key") != "key" {
		t.Fatalf("ASCII lowercase differed")
	}
	if optionxform("İ") != "i̇" {
		t.Fatalf("U+0130 mapping differed")
	}
	if optionxform("Kẞ") != "kß" {
		t.Fatalf("Kelvin/long-s mapping differed")
	}
	if optionxform("\U00010400") != "\U00010428" {
		t.Fatalf("Deseret mapping differed")
	}
	// Unicode 17 letters remain unassigned under the frozen profile.
	for _, code := range []rune{0xA7CE, 0xA7D2, 0xA7D4} {
		character := string(code)
		if optionxform(character) != character {
			t.Fatalf("U+%04X must stay unchanged", code)
		}
	}
}

func TestWindowsDuplicateEntriesStayCompleteWithWarnings(t *testing.T) {
	doc := parseText(t, WindowsV1, "[s]\r\na=1\r\nA=2\r\n")
	if doc.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("Windows duplicates must not recover")
	}
	if len(doc.Diagnostics()) != 1 ||
		doc.Diagnostics()[0].Code != "ini.formation.case-collision@1" {
		t.Fatalf("Windows duplicate diagnostics differed")
	}
	portable := parseText(t, PortableV1, "[s]\na=1\na=2\n")
	if portable.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("portable duplicates must recover")
	}
}

func TestLineScanningHandlesEmptyAndCRLF(t *testing.T) {
	doc := parseText(t, WindowsV1, "[s]\r\n\r\nk=v\r\n")
	if len(doc.PhysicalLines()) != 3 {
		t.Fatalf("physical line count differed")
	}
	lines := doc.PhysicalLines()
	if lines[1].LineBreakSpan() == nil || lines[1].ContentSpan().Len() != 0 {
		t.Fatalf("blank line facts differed")
	}
	if lines[2].LineBreakSpan() == nil || lines[2].LineBreakSpan().Len() != 2 {
		t.Fatalf("CRLF break facts differed")
	}
	assertExactCoverage(t, doc)
}

func TestWindowsValueOwnershipAndQuotedSpelling(t *testing.T) {
	doc := parseText(t, WindowsV1, "[S]\r\nName=\" value \"\r\nPlain=value\r\n")
	entries := doc.Entries()
	if entries[0].Value() != " value " || entries[0].QuoteStyle() != QuoteStyleDouble {
		t.Fatalf("quoted value facts differed")
	}
	if entries[1].Value() != "value" || entries[1].QuoteStyle() != QuoteStyleNone {
		t.Fatalf("plain value facts differed")
	}
	pieces := doc.LosslessStructuralIndex().Pieces()
	kinds := doc.LosslessSyntaxKinds()
	quoteCount := 0
	for index, kind := range kinds {
		if kind == SyntaxKindQuote {
			quoteCount++
			if pieces[index].Span().Len() != 1 {
				t.Fatalf("quote piece must be one byte")
			}
		}
	}
	if quoteCount != 2 {
		t.Fatalf("quote piece count %d, want 2", quoteCount)
	}
}

func TestSingleQuoteStyleIsRecognized(t *testing.T) {
	doc := parseText(t, WindowsV1, "[s]\r\nk='v'\r\n")
	if doc.Entries()[0].QuoteStyle() != QuoteStyleSingle || doc.Entries()[0].Value() != "v" {
		t.Fatalf("single quote facts differed")
	}
	// Quotes inside an otherwise unquoted value are ordinary content.
	doc = parseText(t, WindowsV1, "[s]\r\nk=ab'cd\r\n")
	if doc.Entries()[0].QuoteStyle() != QuoteStyleNone ||
		doc.Entries()[0].Value() != "ab'cd" {
		t.Fatalf("interior quote facts differed")
	}
}

func TestPythonColonDelimiterAndCommentContent(t *testing.T) {
	doc := parseText(t, PythonConfigParserV1, "[s]\nkey: value\n# comment\n; also\nk=#hash ;semi\n")
	if doc.Entries()[0].Key() != "key" || doc.Entries()[0].Value() != "value" {
		t.Fatalf("colon delimiter facts differed")
	}
	if doc.Entries()[1].Value() != "#hash ;semi" {
		t.Fatalf("comment markers inside values must stay content")
	}
	assertExactCoverage(t, doc)
}

func TestWindowsProfileTrimmedKeysAndSectionTrivia(t *testing.T) {
	doc := parseText(t, WindowsV1, "  [ S ]  \r\n  key  =  value  \r\n")
	sections := doc.Sections()
	// Whitespace inside the brackets is part of the section name; only the
	// line-edge trivia is trimmed.
	if sections[0].Name() != " S " {
		t.Fatalf("section name trimming differed: %q", sections[0].Name())
	}
	entries := doc.Entries()
	if entries[0].Key() != "key" {
		t.Fatalf("key trimming differed")
	}
	// Unquoted values keep their exact content including trailing spaces.
	if entries[0].Value() != "  value  " {
		t.Fatalf("unquoted value content differed: %q", entries[0].Value())
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("formation %s", doc.FormationStatus())
	}
}

func TestPortableDuplicateSectionAndEntryDiagnostics(t *testing.T) {
	doc := parseText(t, PortableV1, "[a]\nx=1\nx=2\n[a]\ny=3\n")
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("portable duplicates must recover")
	}
	seen := map[string]bool{}
	for _, diagnostic := range doc.Diagnostics() {
		seen[diagnostic.Code] = true
	}
	if !seen["ini.formation.duplicate-section@1"] || !seen["ini.formation.duplicate-entry@1"] {
		t.Fatalf("duplicate diagnostics missing: %v", seen)
	}
	// Entries in distinct section occurrences are never duplicate-grouped
	// under the portable profile (grouping is per owning occurrence).
	distinct := parseText(t, PortableV1, "[a]\nx=1\n[a]\nx=2\n")
	seen = map[string]bool{}
	for _, diagnostic := range distinct.Diagnostics() {
		seen[diagnostic.Code] = true
	}
	if seen["ini.formation.duplicate-entry@1"] {
		t.Fatalf("cross-occurrence entries must not form a duplicate group")
	}
}

func TestDeterministicDiagnosticOrdering(t *testing.T) {
	doc := parseText(t, PortableV1, "[s]\nbare\nbad\n")
	diagnostics := doc.Diagnostics()
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostic count %d", len(diagnostics))
	}
	locations := doc.ErrorLines()
	if diagnostics[0].Code != "ini.parse.missing-delimiter@1" ||
		diagnostics[1].Code != "ini.parse.missing-delimiter@1" {
		t.Fatalf("diagnostic order differed")
	}
	if uint64(locations[0].Span().StartByte()) != diagnostics[0].Primary.StartByte {
		t.Fatalf("diagnostic locations disagree with error records")
	}
	// The portable end-of-document missing-section fact fires only when no
	// section was proven.
	commentOnly := parseText(t, PortableV1, "; only\n")
	if len(commentOnly.Diagnostics()) != 1 ||
		commentOnly.Diagnostics()[0].Code != "ini.parse.missing-section@1" {
		t.Fatalf("missing-section diagnostic differed")
	}
}
