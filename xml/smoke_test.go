package xml

import (
	"context"
	"testing"

	"consema.dev/consema/document"
)

func parseSmoke(t *testing.T, source string) *Document {
	t.Helper()
	doc, failure := Parse(context.Background(), []byte(source), XmlProfileSafeV1,
		XmlEncodingProfileDefault(), DefaultXmlParseLimits())
	if failure != nil {
		t.Fatalf("parse %q: %v", source, failure)
	}
	return doc
}

func dumpPieces(t *testing.T, source string) string {
	t.Helper()
	doc := parseSmoke(t, source)
	pieces := doc.LosslessStructuralIndex().Pieces()
	kinds := doc.LosslessSyntaxKinds()
	out := ""
	for i, piece := range pieces {
		start := piece.Span().StartByte()
		end := piece.Span().EndByte()
		out += kinds[i].AsStr() + "[" + itoa(start) + "," + itoa(end) + ")\n"
	}
	return out
}

func TestSmokePiecesBasic(t *testing.T) {
	cases := map[string]string{
		`<root a="1">t</root>`: `tag-open[0,1)
local-name[1,5)
whitespace[5,6)
attribute-name[6,7)
equals[7,8)
quote[8,9)
attribute-value[9,10)
quote[10,11)
tag-close[11,12)
text[12,13)
end-tag-open[13,15)
local-name[15,19)
tag-close[19,20)
`,
		`<root a="1"/>`: `tag-open[0,1)
local-name[1,5)
whitespace[5,6)
attribute-name[6,7)
equals[7,8)
quote[8,9)
attribute-value[9,10)
quote[10,11)
empty-element-close[11,13)
`,
		`<root>&lt;</root>`: `tag-open[0,1)
local-name[1,5)
tag-close[5,6)
entity-reference[6,10)
end-tag-open[10,12)
local-name[12,16)
tag-close[16,17)
`,
		`<root>a &lt; b &amp; c &#65;</root>`: `tag-open[0,1)
local-name[1,5)
tag-close[5,6)
text[6,8)
entity-reference[8,12)
text[12,15)
entity-reference[15,20)
text[20,23)
character-reference[23,28)
end-tag-open[28,30)
local-name[30,34)
tag-close[34,35)
`,
		`<?xml version="1.0"?><root/>`: `declaration-open[0,5)
whitespace[5,6)
declaration-name[6,13)
whitespace[13,15)
declaration-value[15,18)
whitespace[18,19)
declaration-close[19,21)
tag-open[21,22)
local-name[22,26)
empty-element-close[26,28)
`,
		`<!DOCTYPE root [<!ENTITY greeting "hello">]><root>&greeting;</root>`: `doctype-open[0,9)
whitespace[9,10)
doctype-name[10,14)
whitespace[14,16)
dtd-markup[16,42)
doctype-close[42,44)
tag-open[44,45)
local-name[45,49)
tag-close[49,50)
entity-reference[50,60)
end-tag-open[60,62)
local-name[62,66)
tag-close[66,67)
`,
		`<p:root xmlns:p="urn:one"><p:child xmlns:q="urn:two" q:attr="x"/></p:root>`: `tag-open[0,1)
prefix[1,2)
colon[2,3)
local-name[3,7)
whitespace[7,8)
namespace-declaration[8,15)
equals[15,16)
quote[16,17)
attribute-value[17,24)
quote[24,25)
tag-close[25,26)
tag-open[26,27)
prefix[27,28)
colon[28,29)
local-name[29,34)
whitespace[34,35)
namespace-declaration[35,42)
equals[42,43)
quote[43,44)
attribute-value[44,51)
quote[51,52)
whitespace[52,53)
attribute-name[53,59)
equals[59,60)
quote[60,61)
attribute-value[61,62)
quote[62,63)
empty-element-close[63,65)
end-tag-open[65,67)
prefix[67,68)
colon[68,69)
local-name[69,73)
tag-close[73,74)
`,
	}
	for source, expected := range cases {
		if actual := dumpPieces(t, source); actual != expected {
			t.Errorf("pieces for %q:\n%s\n--- expected ---\n%s", source, actual, expected)
		}
	}
}

func TestSmokeStatuses(t *testing.T) {
	cases := []struct {
		source     string
		status     string
		diagnostic string
	}{
		{`<p:root/>`, "Recovered", "xml.namespace.unbound-prefix@1"},
		{`<root>&unknown;</root>`, "Recovered", "xml.entity.unknown@1"},
		{`<!DOCTYPE root SYSTEM "http://evil.example/x.dtd"><root/>`, "Recovered", "xml.dtd.external-subset@1"},
		{`<?xml version="1.0"?><!-- nothing -->`, "Recovered", "xml.tree.missing-root@1"},
		{`<root xmlns:p="urn:u" xmlns:q="urn:u" p:a="1" q:a="2"/>`, "Recovered", "xml.namespace.duplicate-attribute@1"},
		{`<!DOCTYPE root [<!-- <!ELEMENT not-a-decl> -->]><root/>`, "Complete", ""},
		{`<root>line1
line2</root>`, "Complete", ""},
		{`<root xmlns="urn:app" version="1"><child/></root>`, "Complete", ""},
		{`<root>a<child/>b<![CDATA[c]]><!--d--><?pi e?>f</root>`, "Complete", ""},
		{`<root a="1"><child>t</child></root>`, "Complete", ""},
	}
	for _, test := range cases {
		doc := parseSmoke(t, test.source)
		if doc.FormationStatus().String() != test.status {
			t.Errorf("status for %q: got %s want %s", test.source, doc.FormationStatus(), test.status)
		}
		if test.diagnostic != "" {
			found := false
			for _, diagnostic := range doc.Diagnostics() {
				if diagnostic.Code == test.diagnostic {
					found = true
				}
			}
			if !found {
				codes := []string{}
				for _, diagnostic := range doc.Diagnostics() {
					codes = append(codes, diagnostic.Code)
				}
				t.Errorf("diagnostic %s not found for %q (got %v)", test.diagnostic, test.source, codes)
			}
		} else if len(doc.Diagnostics()) != 0 {
			codes := []string{}
			for _, diagnostic := range doc.Diagnostics() {
				codes = append(codes, diagnostic.Code)
			}
			t.Errorf("unexpected diagnostics for %q: %v", test.source, codes)
		}
	}
}

func TestSmokeRenderRoundTrip(t *testing.T) {
	cases := []string{
		`<root a="1"><child>t</child></root>`,
		`<root xmlns="urn:app" version="1"><child/></root>`,
		`<p:root xmlns:p="urn:one"><p:child xmlns:q="urn:two" q:attr="x"/></p:root>`,
		`<root>a &lt; b &amp; c &#65;</root>`,
		`<!DOCTYPE root [<!ENTITY greeting "hello">]><root>&greeting;</root>`,
		`<root>a<child/>b<![CDATA[c]]><!--d--><?pi e?>f</root>`,
		`<root>line1
line2</root>`,
	}
	for _, source := range cases {
		doc := parseSmoke(t, source)
		if string(doc.Render()) != source {
			t.Errorf("render mismatch for %q: got %q", source, doc.Render())
		}
	}
}

func TestSmokeTextSemantic(t *testing.T) {
	doc := parseSmoke(t, "<root>line1\r\nline2</root>")
	root := doc.Root()
	for _, child := range root.Children() {
		if text := child.Text(); text != nil {
			if semantic := TextSemantic(text); semantic != "line1\nline2" {
				t.Errorf("semantic: got %q", semantic)
			}
		}
	}
}

func TestSmokeUtf16(t *testing.T) {
	var bytes []byte
	bytes = append(bytes, 0xFF, 0xFE)
	for _, unit := range utf16Encode("<root>中文</root>") {
		bytes = append(bytes, byte(unit), byte(unit>>8))
	}
	doc, failure := Parse(context.Background(), bytes, XmlProfileSafeV1,
		XmlEncodingProfileDefault(), DefaultXmlParseLimits())
	if failure != nil {
		t.Fatalf("utf16 parse: %v", failure)
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("utf16 status: %v", doc.FormationStatus())
	}
	if string(doc.Render()) != string(bytes) {
		t.Fatalf("utf16 render mismatch")
	}
	root := doc.Root()
	if root == nil || root.Expanded() == nil || root.Expanded().Local != "root" {
		t.Fatalf("utf16 root mismatch")
	}
}

func TestTypedErrorCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"namespace-unbound-prefix",
			&NamespaceError{Kind: NamespaceErrorUnboundPrefix, Prefix: "p"},
			"xml.namespace.unbound-prefix@1"},
		{"namespace-reserved-prefix",
			&NamespaceError{Kind: NamespaceErrorReservedPrefix, Prefix: "xmlns"},
			"xml.namespace.reserved-prefix@1"},
		{"namespace-xml-rebinding",
			&NamespaceError{Kind: NamespaceErrorIllegalXmlRebinding, Prefix: "xml", URI: "urn:evil"},
			"xml.namespace.xml-rebinding@1"},
		{"namespace-default-xmlns",
			&NamespaceError{Kind: NamespaceErrorIllegalDefaultXmlns, URI: "http://www.w3.org/2000/xmlns/"},
			"xml.namespace.default-xmlns@1"},
		{"replacement-markup",
			&ReplacementError{Kind: ReplacementErrorContainsMarkup},
			"xml.entity.markup@1"},
		{"replacement-illegal-character",
			&ReplacementError{Kind: ReplacementErrorIllegalCharacter, Scalar: 0x01},
			"xml.entity.illegal-character@1"},
	}
	for _, testCase := range cases {
		typed, ok := testCase.err.(interface{ Code() string })
		if !ok {
			t.Errorf("%s: error type must implement Code()", testCase.name)
			continue
		}
		if got := typed.Code(); got != testCase.want {
			t.Errorf("%s: Code() = %q, want %q", testCase.name, got, testCase.want)
		}
	}
}
