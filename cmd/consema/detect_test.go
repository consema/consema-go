package main

import (
	"testing"
)

func candidateIDs(facts *DetectFacts) []string {
	ids := make([]string, 0, len(facts.Candidates))
	for _, candidate := range facts.Candidates {
		ids = append(ids, candidate.Profile.ID())
	}
	return ids
}

func TestBplist00HeaderIsAPlistBinaryFact(t *testing.T) {
	facts := detect([]byte("bplist00\x5f\x78"), true)
	if len(facts.Markers) != 1 || facts.Markers[0] != "bplist00 header" {
		t.Fatalf("markers = %v", facts.Markers)
	}
	if ids := candidateIDs(&facts); len(ids) != 1 || ids[0] != "plist.binary" {
		t.Fatalf("candidates = %v", ids)
	}
	if facts.Ambiguous {
		t.Fatal("single candidate must not be ambiguous")
	}
	if facts.Candidates[0].Reason != "leading bplist00 header bytes" {
		t.Fatalf("reason = %q", facts.Candidates[0].Reason)
	}
}

func TestXMLDeclarationAmbiguousBetweenXMLAndPlist(t *testing.T) {
	facts := detect([]byte("<?xml version=\"1.0\"?><a/>"), true)
	if ids := candidateIDs(&facts); len(ids) != 2 || ids[0] != "plist.xml" || ids[1] != "xml.1.0-safe" {
		t.Fatalf("candidates = %v", ids)
	}
	if !facts.Ambiguous {
		t.Fatal("must be ambiguous")
	}
	if len(facts.AmbiguityReasons) != 1 ||
		facts.AmbiguityReasons[0] != "XML declaration is consistent with format families: plist, xml" {
		t.Fatalf("reasons = %v", facts.AmbiguityReasons)
	}
	// A plist root element without a declaration resolves to plist only.
	facts = detect([]byte("<plist version=\"1.0\"><string>x</string></plist>"), true)
	if ids := candidateIDs(&facts); len(ids) != 1 || ids[0] != "plist.xml" {
		t.Fatalf("plist root candidates = %v", ids)
	}
	if facts.Ambiguous {
		t.Fatal("plist root must not be ambiguous")
	}
}

func TestLeadingBraceAndBracketAreJSONFamilyFacts(t *testing.T) {
	facts := detect([]byte("{\"a\": 1}"), true)
	if facts.Markers[0] != "first non-whitespace '{'" {
		t.Fatalf("markers = %v", facts.Markers)
	}
	ids := candidateIDs(&facts)
	if len(ids) != 3 || ids[0] != "json.strict" || ids[1] != "json5.standard" || ids[2] != "jsonc.bounded" {
		t.Fatalf("candidates = %v", ids)
	}
	if !facts.Ambiguous {
		t.Fatal("must be ambiguous")
	}
	if facts.AmbiguityReasons[0] !=
		"first non-whitespace '{' is consistent with multiple profiles of the json family" {
		t.Fatalf("reasons = %v", facts.AmbiguityReasons)
	}
	// A leading JSON array stays a '[' fact, not a section header.
	facts = detect([]byte("[1, 2]"), true)
	if facts.Markers[0] != "first non-whitespace '['" {
		t.Fatalf("array markers = %v", facts.Markers)
	}
	// Leading whitespace and a UTF-8 BOM do not change the judgment.
	facts = detect([]byte("\xef\xbb\xbf\n  { \"a\": 1 }"), true)
	if facts.Markers[0] != "first non-whitespace '{'" {
		t.Fatalf("bom markers = %v", facts.Markers)
	}
}

func TestSectionLineAmbiguousBetweenINIAndTOML(t *testing.T) {
	facts := detect([]byte("[section]\nvalue=1\n"), true)
	ids := candidateIDs(&facts)
	expected := []string{"ini.portable", "ini.python-configparser", "ini.windows", "toml.1.0"}
	if len(ids) != len(expected) {
		t.Fatalf("candidates = %v", ids)
	}
	for index := range expected {
		if ids[index] != expected[index] {
			t.Fatalf("candidates = %v", ids)
		}
	}
	if !facts.Ambiguous {
		t.Fatal("must be ambiguous")
	}
}

func TestKeyValueLineAmbiguousBetweenINIAndProperties(t *testing.T) {
	facts := detect([]byte("name=api\nport=8080\n"), true)
	ids := candidateIDs(&facts)
	expected := []string{"ini.portable", "ini.python-configparser", "ini.windows",
		"java-properties.latin1", "java-properties.reader"}
	if len(ids) != len(expected) {
		t.Fatalf("candidates = %v", ids)
	}
	for index := range expected {
		if ids[index] != expected[index] {
			t.Fatalf("candidates = %v", ids)
		}
	}
}

func TestSpacedAssignmentIsTheTOMLHCLShape(t *testing.T) {
	facts := detect([]byte("a = 1\n"), true)
	ids := candidateIDs(&facts)
	expected := []string{"hcl.native", "hcl.tfvars", "toml.1.0"}
	if len(ids) != len(expected) {
		t.Fatalf("candidates = %v", ids)
	}
	for index := range expected {
		if ids[index] != expected[index] {
			t.Fatalf("candidates = %v", ids)
		}
	}
}

func TestYAMLMarkersResolveToTheYAMLFamily(t *testing.T) {
	facts := detect([]byte("name: catalog\nport: 8080\n"), true)
	if facts.Markers[0] != "key: value line" {
		t.Fatalf("markers = %v", facts.Markers)
	}
	ids := candidateIDs(&facts)
	if len(ids) != 2 || ids[0] != "yaml.1.1-compat" || ids[1] != "yaml.1.2-core" {
		t.Fatalf("candidates = %v", ids)
	}
	facts = detect([]byte("%YAML 1.2\n---\nvalue: 1\n"), true)
	if facts.Markers[0] != "%YAML directive" {
		t.Fatalf("markers = %v", facts.Markers)
	}
	if len(candidateIDs(&facts)) != 2 {
		t.Fatalf("candidates = %v", candidateIDs(&facts))
	}
}

func TestUnknownContentHasNoMarker(t *testing.T) {
	facts := detect([]byte("# just a comment\n"), true)
	if len(facts.Markers) != 0 || len(facts.Candidates) != 0 || facts.Ambiguous {
		t.Fatalf("facts = %+v", facts)
	}
	facts = detect([]byte(""), true)
	if len(facts.Markers) != 0 || len(facts.Candidates) != 0 {
		t.Fatalf("empty facts = %+v", facts)
	}
}

func TestByteFactsAndBOMFactsAreDeterministic(t *testing.T) {
	bytes := []byte("\xef\xbb\xbf{\"a\":1}")
	facts := detect(bytes, true)
	if facts.Size != uint64(len(bytes)) {
		t.Fatalf("size = %d", facts.Size)
	}
	if facts.BOM != "Utf8" {
		t.Fatalf("bom = %q", facts.BOM)
	}
	if facts.Digest == nil {
		t.Fatal("fully read files carry the digest")
	}
	if detectBOM([]byte("\xff\xfe\x7b\x00")) != "Utf16Le" {
		t.Fatal("Utf16Le BOM")
	}
	if detectBOM([]byte("\xfe\xff\x00\x7b")) != "Utf16Be" {
		t.Fatal("Utf16Be BOM")
	}
	if detectBOM([]byte("plain")) != "" {
		t.Fatal("no BOM")
	}
	// A capped read never reports a partial digest.
	capped := detect([]byte("\xef\xbb\xbf{\"a\":1}"), false)
	if capped.Digest != nil {
		t.Fatal("capped reads must not report a digest")
	}
}
