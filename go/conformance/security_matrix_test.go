package conformance

// Security matrix (milestone 0.19.0 G5.4; docs/go-implementation-plan.md
// 搂2.6 deliverable "full corpus銆乫uzz銆乥enchmark 鍜?security matrix";
// roadmap 搂22.4:1908 "XML/YAML/HCL/binary plist 涓撻」 threat tests 閫氳繃").
//
// Extends the 0.16.0 limits matrix (limits_matrix_test.go, five families)
// to the recovery-capable families shipped in 0.16.0-0.18.0 (xml/plist/hcl)
// and mirrors the Rust adversarial surface (consema-rs/crates/consema-conformance/
// tests/{xml,plist,hcl,yaml}_hardening.rs; SECURITY.md:16,32-36). Every
// public limit parameter is pinned with its exact positive/negative
// boundary (N-1 fails with the family's frozen code, N succeeds), and the
// family-specific threat tests run against the production default limits.
//
// Rows whose parameter is provably unconsumed by the family's parse are
// documented in comments and deliberately not pinned (the same discipline
// as the INI MaxNestingDepth row of limits_matrix_test.go): xml parse does
// not consume Common.MaxTokenCount (its piece budget is the lossless
// tokenizer, and MaxRecoveryRegions is a diagnostics budget that silently
// drops further diagnostics 鈥?the Rust-aligned shape), plist XML parse
// does not consume Common.MaxTokenCount/Common.MaxNodeCount (its piece
// budget is MaxSyntaxPieces), and HCL parse does not consume
// Common.MaxNodeCount/Common.MaxNestingDepth (HCL owns body/expression
// depth and per-body counts).

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"consema.dev/consema/document"
	"consema.dev/consema/graph"
	"consema.dev/consema/hcl"
	"consema.dev/consema/plist"
	"consema.dev/consema/xml"
	"consema.dev/consema/yaml"
)

// ---------------------------------------------------------------------------
// XML family
// ---------------------------------------------------------------------------

func xmlLimitCode(failure *xml.FormationFailure) string {
	if failure == nil {
		return ""
	}
	return failure.Code()
}

func TestSecurityLimitsMatrixXML(t *testing.T) {
	rows := []struct {
		name         string
		source       string
		expectedCode string
		set          func(*xml.XmlParseLimits, int)
	}{
		{"Common.MaxSourceBytes", "<root/>", "core.source.resource-limit@1",
			func(l *xml.XmlParseLimits, limit int) { l.Common.MaxSourceBytes = limit }},
		{"Common.MaxNestingDepth", "<a><b><c><d/></c></b></a>", "xml.limit.depth@1",
			func(l *xml.XmlParseLimits, limit int) { l.Common.MaxNestingDepth = limit }},
		{"Common.MaxNodeCount", "<a><b/><c/></a>", "xml.limit.node@1",
			func(l *xml.XmlParseLimits, limit int) { l.Common.MaxNodeCount = limit }},
		{"MaxElementCount", "<root><a/><b/><c/><d/><e/></root>", "xml.limit.element@1",
			func(l *xml.XmlParseLimits, limit int) { l.MaxElementCount = limit }},
		{"MaxAttributeCount", `<root a="1" b="2" c="3"/>`, "xml.limit.attribute@1",
			func(l *xml.XmlParseLimits, limit int) { l.MaxAttributeCount = limit }},
		{"MaxAttributeValueLength", `<root a="1234567890"/>`, "xml.limit.attribute-value@1",
			func(l *xml.XmlParseLimits, limit int) { l.MaxAttributeValueLength = limit }},
		{"MaxCommentLength", "<root><!-- 1234567890 --></root>", "xml.limit.comment@1",
			func(l *xml.XmlParseLimits, limit int) { l.MaxCommentLength = limit }},
		{"MaxTextLength", "<root>1234567890</root>", "xml.limit.text@1",
			func(l *xml.XmlParseLimits, limit int) { l.MaxTextLength = limit }},
		{"MaxCdataLength", "<root><![CDATA[1234567890]]></root>", "xml.limit.cdata@1",
			func(l *xml.XmlParseLimits, limit int) { l.MaxCdataLength = limit }},
		{"MaxPiLength", "<root><?pi 1234567890?></root>", "xml.limit.pi@1",
			func(l *xml.XmlParseLimits, limit int) { l.MaxPiLength = limit }},
		{"MaxNamespaceURILength", `<root xmlns:p="urn:verylongnamespace"><p:x/></root>`,
			"xml.limit.namespace-uri@1",
			func(l *xml.XmlParseLimits, limit int) { l.MaxNamespaceURILength = limit }},
		{"MaxQNameLength", "<root><abcdefghij/></root>", "xml.limit.qname@1",
			func(l *xml.XmlParseLimits, limit int) { l.MaxQNameLength = limit }},
		{"MaxDtdBytes", `<!DOCTYPE root [<!ENTITY e "x">]><root/>`, "xml.limit.dtd@1",
			func(l *xml.XmlParseLimits, limit int) { l.MaxDtdBytes = limit }},
	}
	for _, row := range rows {
		pinBoundaryCode(t, "xml", "parse."+row.name, row.expectedCode,
			func(limit int) string {
				limits := xml.DefaultXmlParseLimits()
				row.set(&limits, limit)
				_, failure := xml.Parse(context.Background(), []byte(row.source),
					xml.XmlProfileSafeV1, xml.XmlEncodingProfileDefault(), limits)
				return xmlLimitCode(failure)
			})
	}
}

// pinXmlEntityBoundary pins one entity-accounting recovery boundary: the
// smallest limit N at which the source completes without an entity-limit
// diagnostic; N-1 must be Recovered with the expected `xml.entity.*@1`
// code. Entity limits are document-wide accounting that recovers (RFC
// 0013 搂12; SECURITY.md:32), never a fatal failure.
func pinXmlEntityBoundary(t *testing.T, param, expectedCode string,
	run func(limit int) (*xml.Document, *xml.FormationFailure)) {
	t.Helper()
	n := -1
	for limit := 1; limit <= maxBoundary; limit++ {
		doc, failure := run(limit)
		if failure == nil && doc.FormationStatus() == document.FormationStatusComplete &&
			!xmlHasDiagnostic(doc, "xml.entity.limit@1") &&
			!xmlHasDiagnostic(doc, "xml.entity.amplification@1") {
			n = limit
			break
		}
	}
	if n < 1 {
		t.Fatalf("xml/%s: no completing limit up to %d", param, maxBoundary)
	}
	doc, failure := run(n - 1)
	if failure != nil {
		t.Fatalf("xml/%s: limit %d failed fatally, want Recovered: %s",
			param, n-1, failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("xml/%s: limit %d status %v, want Recovered", param, n-1, doc.FormationStatus())
	}
	if !xmlHasDiagnostic(doc, expectedCode) {
		t.Fatalf("xml/%s: limit %d lacks %s", param, n-1, expectedCode)
	}
	if _, failure := run(n); failure != nil {
		t.Fatalf("xml/%s: limit N=%d must succeed, got %s", param, n, failure.Code())
	}
}

func xmlHasDiagnostic(doc *xml.Document, code string) bool {
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func TestSecurityXMLEntityRecoveryBoundaries(t *testing.T) {
	pinXmlEntityBoundary(t, "MaxEntityReferences", "xml.entity.limit@1",
		func(limit int) (*xml.Document, *xml.FormationFailure) {
			limits := xml.DefaultXmlParseLimits()
			limits.MaxEntityReferences = limit
			return xml.Parse(context.Background(),
				[]byte(`<!DOCTYPE root [<!ENTITY e "x">]><root>&e;</root>`),
				xml.XmlProfileSafeV1, xml.XmlEncodingProfileDefault(), limits)
		})
	pinXmlEntityBoundary(t, "MaxEntityDeclarations", "xml.entity.limit@1",
		func(limit int) (*xml.Document, *xml.FormationFailure) {
			limits := xml.DefaultXmlParseLimits()
			limits.MaxEntityDeclarations = limit
			return xml.Parse(context.Background(),
				[]byte(`<!DOCTYPE root [<!ENTITY e "x">]><root>&e;</root>`),
				xml.XmlProfileSafeV1, xml.XmlEncodingProfileDefault(), limits)
		})
	pinXmlEntityBoundary(t, "MaxEntityExpansionDepth", "xml.entity.limit@1",
		func(limit int) (*xml.Document, *xml.FormationFailure) {
			limits := xml.DefaultXmlParseLimits()
			limits.MaxEntityExpansionDepth = limit
			return xml.Parse(context.Background(),
				[]byte(`<!DOCTYPE root [<!ENTITY a "&b;"><!ENTITY b "x">]><root>&a;</root>`),
				xml.XmlProfileSafeV1, xml.XmlEncodingProfileDefault(), limits)
		})
	pinXmlEntityBoundary(t, "MaxExpandedEntityBytes", "xml.entity.limit@1",
		func(limit int) (*xml.Document, *xml.FormationFailure) {
			limits := xml.DefaultXmlParseLimits()
			limits.MaxExpandedEntityBytes = limit
			return xml.Parse(context.Background(),
				[]byte(`<!DOCTYPE root [<!ENTITY e "xxxxxxxxxxxxxxxx">]><root>&e;</root>`),
				xml.XmlProfileSafeV1, xml.XmlEncodingProfileDefault(), limits)
		})
}

func TestSecurityXMLEntityExpansion(t *testing.T) {
	// Classic exponential billion laughs across ten levels; the
	// document-wide amplification accounting must recover the document
	// with the frozen amplification code, never panic and never fabricate.
	var dtd strings.Builder
	dtd.WriteString(`<!DOCTYPE root [<!ENTITY e0 "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa">`)
	for index := 1; index < 10; index++ {
		fmt.Fprintf(&dtd, `<!ENTITY e%d "&e%d;&e%d;&e%d;&e%d;&e%d;&e%d;&e%d;&e%d;">`,
			index, index-1, index-1, index-1, index-1, index-1, index-1, index-1, index-1)
	}
	dtd.WriteString(`]><root>&e9;</root>`)
	source := []byte(dtd.String())
	doc, failure := xml.Parse(context.Background(), source, xml.XmlProfileSafeV1,
		xml.XmlEncodingProfileDefault(), xml.DefaultXmlParseLimits())
	if failure != nil {
		t.Fatalf("billion laughs must form a recovered document: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("billion laughs status %v, want Recovered", doc.FormationStatus())
	}
	if !xmlHasDiagnostic(doc, "xml.entity.amplification@1") {
		t.Fatalf("billion laughs must hit the document-wide amplification limit")
	}
	if !bytes.Equal(doc.Render(), source) {
		t.Fatal("billion laughs render closure violated")
	}

	// Cyclic references recover with the cycle diagnostic.
	doc, failure = xml.Parse(context.Background(),
		[]byte(`<!DOCTYPE root [<!ENTITY a "&b;"><!ENTITY b "&a;">]><root>&a;</root>`),
		xml.XmlProfileSafeV1, xml.XmlEncodingProfileDefault(), xml.DefaultXmlParseLimits())
	if failure != nil {
		t.Fatalf("cyclic entities must form a recovered document: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("cyclic entities status %v, want Recovered", doc.FormationStatus())
	}
	if !xmlHasDiagnostic(doc, "xml.entity.cyclic@1") {
		t.Fatalf("cyclic references must publish the cycle diagnostic")
	}

	// A 200-deep reference chain unwinds through the depth and cycle
	// diagnostics.
	var chain strings.Builder
	chain.WriteString("<!DOCTYPE root [")
	for index := 0; index < 200; index++ {
		fmt.Fprintf(&chain, `<!ENTITY e%d "&e%d;">`, index, index+1)
	}
	chain.WriteString(`]><root>`)
	for index := 0; index < 200; index++ {
		fmt.Fprintf(&chain, "&e%d;", index)
	}
	chain.WriteString("</root>")
	doc, failure = xml.Parse(context.Background(), []byte(chain.String()), xml.XmlProfileSafeV1,
		xml.XmlEncodingProfileDefault(), xml.DefaultXmlParseLimits())
	if failure != nil {
		t.Fatalf("deep reference chain must form a recovered document: %s", failure.Code())
	}
	if !xmlHasDiagnostic(doc, "xml.entity.limit@1") {
		t.Fatalf("reference nesting must hit the entity expansion depth limit")
	}
	if !xmlHasDiagnostic(doc, "xml.entity.cyclic@1") {
		t.Fatalf("each breached chain unwinds through the cycle diagnostic")
	}
}

// ---------------------------------------------------------------------------
// plist family (XML and binary profiles)
// ---------------------------------------------------------------------------

func plistLimitCode(failure *plist.FormationFailure) string {
	if failure == nil {
		return ""
	}
	// The frozen diagnostic code is the first diagnostic's code; the
	// failure Code() layer reports core.parse.resource-limit@1 for limit
	// failures (plist.binary.trailer/limit rows carry plist.limit.*@1).
	if len(failure.Diagnostics) > 0 {
		return failure.Diagnostics[0].Code
	}
	return failure.Code()
}

func TestSecurityLimitsMatrixPlistXML(t *testing.T) {
	rows := []struct {
		name         string
		source       string
		expectedCode string
		set          func(*plist.PlistParseLimits, int)
	}{
		{"Common.MaxSourceBytes", `<plist version="1.0"><string>ok</string></plist>`,
			"core.source.resource-limit@1",
			func(l *plist.PlistParseLimits, limit int) { l.Common.MaxSourceBytes = limit }},
		{"Common.MaxNestingDepth", `<plist version="1.0"><dict><key>a</key><dict><key>b</key><dict><key>c</key><string>x</string></dict></dict></dict></plist>`,
			"plist.limit.nesting-depth@1",
			func(l *plist.PlistParseLimits, limit int) { l.Common.MaxNestingDepth = limit }},
		{"MaxSyntaxPieces", `<plist version="1.0"><string>ok</string></plist>`,
			"plist.limit.syntax-pieces@1",
			func(l *plist.PlistParseLimits, limit int) { l.MaxSyntaxPieces = limit }},
		{"MaxObjectCount", `<plist version="1.0"><array><string>a</string><string>b</string></array></plist>`,
			"plist.limit.object-count@1",
			func(l *plist.PlistParseLimits, limit int) { l.MaxObjectCount = limit }},
		{"MaxContainerDepth", `<plist version="1.0"><dict><key>a</key><dict><key>b</key><string>x</string></dict></dict></plist>`,
			"plist.limit.container-depth@1",
			func(l *plist.PlistParseLimits, limit int) { l.MaxContainerDepth = limit }},
		{"MaxDictEntries", `<plist version="1.0"><dict><key>a</key><integer>1</integer><key>b</key><integer>2</integer></dict></plist>`,
			"plist.limit.dict-entries@1",
			func(l *plist.PlistParseLimits, limit int) { l.MaxDictEntries = limit }},
		{"MaxArrayElements", `<plist version="1.0"><array><integer>1</integer><integer>2</integer></array></plist>`,
			"plist.limit.array-elements@1",
			func(l *plist.PlistParseLimits, limit int) { l.MaxArrayElements = limit }},
		{"MaxStringCodeUnits", `<plist version="1.0"><string>abcde</string></plist>`,
			"plist.limit.string-code-units@1",
			func(l *plist.PlistParseLimits, limit int) { l.MaxStringCodeUnits = limit }},
		{"MaxDataBytes", `<plist version="1.0"><data>QUJDRA==</data></plist>`,
			"plist.limit.data-bytes@1",
			func(l *plist.PlistParseLimits, limit int) { l.MaxDataBytes = limit }},
		{"MaxRecoveryRegions", `<plist version="1.0"><string>ok</string></plist> trailing`,
			"plist.limit.recovery-regions@1",
			func(l *plist.PlistParseLimits, limit int) { l.MaxRecoveryRegions = limit }},
	}
	for _, row := range rows {
		pinBoundaryCode(t, "plist/xml", "parse."+row.name, row.expectedCode,
			func(limit int) string {
				limits := plist.DefaultPlistParseLimits()
				row.set(&limits, limit)
				_, failure := plist.Parse([]byte(row.source), plist.PlistProfileXmlV1,
					plist.PlistEncodingProfileDefault(), limits)
				return plistLimitCode(failure)
			})
	}
}

func TestSecurityLimitsMatrixPlistBinary(t *testing.T) {
	// Frozen binary corpora (plist_hardening.rs; RFC 0013 搂5).
	const threeObjects = "62706c6973743030d1010251611001080b0d000000000000010100000000000000030000000000000000000000000000000f"
	const oneUID = "62706c6973743030802a08000000000000010100000000000000010000000000000000000000000000000a"
	const oneExtendedString = "62706c69737430305f110002abcd08000000000000010100000000000000010000000000000000000000000000000e"

	rows := []struct {
		name         string
		hex          string
		expectedCode string
		set          func(*plist.PlistParseLimits, int)
	}{
		{"Common.MaxSourceBytes", threeObjects, "core.source.resource-limit@1",
			func(l *plist.PlistParseLimits, limit int) { l.Common.MaxSourceBytes = limit }},
		{"MaxObjectCount", threeObjects, "plist.limit.object-count@1",
			func(l *plist.PlistParseLimits, limit int) { l.MaxObjectCount = limit }},
		{"MaxOffsetTableBytes", threeObjects, "plist.limit.offset-table-bytes@1",
			func(l *plist.PlistParseLimits, limit int) { l.MaxOffsetTableBytes = limit }},
		{"MaxBinaryFacts", threeObjects, "plist.limit.binary-facts@1",
			func(l *plist.PlistParseLimits, limit int) { l.MaxBinaryFacts = limit }},
		{"MaxOffsetIntSize", threeObjects, "plist.limit.offset-int-size@1",
			func(l *plist.PlistParseLimits, limit int) { l.MaxOffsetIntSize = limit }},
		{"MaxObjectRefSize", threeObjects, "plist.limit.object-ref-size@1",
			func(l *plist.PlistParseLimits, limit int) { l.MaxObjectRefSize = limit }},
		{"MaxUIDCount", oneUID, "plist.limit.uid-count@1",
			func(l *plist.PlistParseLimits, limit int) { l.MaxUIDCount = limit }},
		{"MaxExtendedSizeIntegers", oneExtendedString, "plist.limit.extended-size-integers@1",
			func(l *plist.PlistParseLimits, limit int) { l.MaxExtendedSizeIntegers = limit }},
		{"MaxExtendedSizeValue", oneExtendedString, "plist.limit.extended-size-value@1",
			func(l *plist.PlistParseLimits, limit int) { l.MaxExtendedSizeValue = limit }},
	}
	for _, row := range rows {
		raw, err := decodeHexBytes(row.hex)
		if err != nil {
			t.Fatalf("%s: bad seed hex: %v", row.name, err)
		}
		pinBoundaryCode(t, "plist/binary", "parse."+row.name, row.expectedCode,
			func(limit int) string {
				limits := plist.DefaultPlistParseLimits()
				row.set(&limits, limit)
				_, failure := plist.Parse(raw, plist.PlistProfileBinaryV1,
					plist.PlistEncodingProfileDefault(), limits)
				return plistLimitCode(failure)
			})
	}
}

func TestSecurityPlistBinaryMutationTruncation(t *testing.T) {
	// The Rust binary mutation/truncation closure (plist_hardening.rs):
	// truncating or single-bit mutating any frozen binary corpus must never
	// panic, a formed document must render byte-exactly, and a Complete
	// document must always carry a native model (a limit breach must never
	// masquerade as a Complete document 鈥?the trailer-limit discard defect
	// this matrix closes).
	hexSeeds := []string{
		"62706c697374303050080000000000000101000000000000000100000000000000000000000000000009",
		"62706c6973743030d1010251611001080b0d000000000000010100000000000000030000000000000000000000000000000f",
		"62706c6973743030a3010102d103045178516b5176080c0f11130000000000000101000000000000000500000000000000000000000000000015",
		"62706c6973743030d201010203516b10011002080d0f110000000000000101000000000000000400000000000000000000000000000013",
		"62706c6973743030a2010210015162080b0d000000000000010100000000000000030000000000000000000000000000000f",
		// Recovered binary shapes (bad version strings, self-referencing
		// tables, unproven top objects).
		"62706c697374303000080000000000000101000000000000000100000000000000000000000000000009",
		"62706c697374303050500805000000000000010100000000000000020000000000000000000000000000000a",
		"62706c69737430305150080000000000000101000000000000000100000000000000000000000000000009",
	}
	for _, hexSeed := range hexSeeds {
		seed, err := decodeHexBytes(hexSeed)
		if err != nil {
			t.Fatalf("bad seed hex: %v", err)
		}
		for length := 0; length <= len(seed); length++ {
			assertBinaryParseClosure(t, seed[:length])
		}
		for index := range seed {
			for _, mask := range []byte{0x01, 0x80, 0xff} {
				mutated := append([]byte(nil), seed...)
				mutated[index] ^= mask
				assertBinaryParseClosure(t, mutated)
			}
		}
	}
}

func assertBinaryParseClosure(t *testing.T, source []byte) {
	t.Helper()
	doc, failure := plist.Parse(source, plist.PlistProfileBinaryV1,
		plist.PlistEncodingProfileDefault(), plist.DefaultPlistParseLimits())
	if failure != nil {
		return // fatal formation (incl. limit failures) is a pass
	}
	if !bytes.Equal(doc.Render(), source) {
		t.Fatalf("binary render closure violated:\ninput:  %x\noutput: %x", source, doc.Render())
	}
	if doc.FormationStatus() == document.FormationStatusComplete && doc.NativeDocument() == nil {
		t.Fatalf("Complete document without a native model (fake completion) for %x", source)
	}
	if doc.FormationStatus() == document.FormationStatusRecovered && len(doc.Diagnostics()) == 0 {
		t.Fatalf("Recovered document without diagnostics for %x", source)
	}
}

// ---------------------------------------------------------------------------
// HCL family
// ---------------------------------------------------------------------------

func hclLimitCode(failure *hcl.FormationFailure) string {
	if failure == nil {
		return ""
	}
	return failure.Code()
}

func TestSecurityLimitsMatrixHCL(t *testing.T) {
	rows := []struct {
		name         string
		source       string
		expectedCode string
		set          func(*hcl.HclParseLimits, int)
	}{
		{"Common.MaxSourceBytes", "a = 1\n", "hcl.limit.source-bytes@1",
			func(l *hcl.HclParseLimits, limit int) { l.Common.MaxSourceBytes = limit }},
		{"Common.MaxTokenCount", "a = 1\n", "hcl.limit.token-count@1",
			func(l *hcl.HclParseLimits, limit int) { l.Common.MaxTokenCount = limit }},
		{"MaxExpressionDepth.parens", "a = (((1)))\n", "hcl.limit.expression-depth@1",
			func(l *hcl.HclParseLimits, limit int) { l.MaxExpressionDepth = limit }},
		{"MaxExpressionDepth.chain", "a = 1 + 1 + 1 + 1 + 1\n", "hcl.limit.expression-depth@1",
			func(l *hcl.HclParseLimits, limit int) { l.MaxExpressionDepth = limit }},
		{"MaxBodyDepth", "a = 1\nb {\nc {\nd = 1\n}\n}\n", "hcl.limit.body-depth@1",
			func(l *hcl.HclParseLimits, limit int) { l.MaxBodyDepth = limit }},
		{"MaxAttributeCount", "a = 1\nb = 2\nc = 3\n", "hcl.limit.attribute-count@1",
			func(l *hcl.HclParseLimits, limit int) { l.MaxAttributeCount = limit }},
		{"MaxBlockCount", "a {\n}\nb {\n}\n", "hcl.limit.block-count@1",
			func(l *hcl.HclParseLimits, limit int) { l.MaxBlockCount = limit }},
		{"MaxLabelCount", "b \"x\" \"y\" {\n}\n", "hcl.limit.label-count@1",
			func(l *hcl.HclParseLimits, limit int) { l.MaxLabelCount = limit }},
		{"MaxNumberDigits", "a = 1e10\n", "hcl.limit.number-digits@1",
			func(l *hcl.HclParseLimits, limit int) { l.MaxNumberDigits = limit }},
		{"MaxTupleElements", "a = [1, 2, 3]\n", "hcl.limit.tuple-elements@1",
			func(l *hcl.HclParseLimits, limit int) { l.MaxTupleElements = limit }},
		{"MaxObjectEntries", "a = {x = 1, y = 2, z = 3}\n", "hcl.limit.object-entries@1",
			func(l *hcl.HclParseLimits, limit int) { l.MaxObjectEntries = limit }},
		{"MaxTemplateLen", "a = \"xxxxxxxxxxxxxxxxxxxxxxxxxx\"\n", "hcl.limit.template-len@1",
			func(l *hcl.HclParseLimits, limit int) { l.MaxTemplateLen = limit }},
		{"MaxHeredocBytes", "h = <<E\none\ntwo\nthree\nE\n", "hcl.limit.heredoc-bytes@1",
			func(l *hcl.HclParseLimits, limit int) { l.MaxHeredocBytes = limit }},
	}
	for _, row := range rows {
		pinBoundaryCode(t, "hcl", "parse."+row.name, row.expectedCode,
			func(limit int) string {
				limits := hcl.DefaultHclParseLimits()
				row.set(&limits, limit)
				_, failure := hcl.Parse(context.Background(), []byte(row.source),
					hcl.HclProfileNativeV1, hcl.HclEncodingSelectionProfileDefault(), limits)
				return hclLimitCode(failure)
			})
	}
}

func TestSecurityHCLAdversarialNesting(t *testing.T) {
	// The Rust adversarial-nesting corpus (hcl_hardening.rs): 2,000-level
	// parentheses, operator chains, block bodies, and template
	// interpolations must never panic 鈥?the frozen depth budgets truncate
	// before the recursion deepens, publishing the documented hcl.limit.*
	// codes.
	deepParens := "a = " + strings.Repeat("(", 2000) + "1" + strings.Repeat(")", 2000) + "\n"
	deepChain := "a = 1" + strings.Repeat(" + 1", 2000) + "\n"
	var deepBlocks strings.Builder
	deepBlocks.WriteString("a = 1\n")
	for index := 0; index < 2000; index++ {
		fmt.Fprintf(&deepBlocks, "b%d {\n", index)
	}
	deepBlocks.WriteString("x = 1\n")
	for index := 1999; index >= 0; index-- {
		fmt.Fprintf(&deepBlocks, "}\n// close b%d\n", index)
	}
	deepTemplates := `a = "` + strings.Repeat(`${"`, 2000) + "1" +
		strings.Repeat(`"}`, 2000) + `"` + "\n"

	for _, c := range []struct {
		name   string
		source string
		code   string
		limits hcl.HclParseLimits
	}{
		{"parens", deepParens, "hcl.limit.expression-depth@1", hcl.DefaultHclParseLimits()},
		{"chain", deepChain, "hcl.limit.expression-depth@1", hcl.DefaultHclParseLimits()},
		{"blocks", deepBlocks.String(), "hcl.limit.body-depth@1", hcl.DefaultHclParseLimits()},
		{"templates", deepTemplates, "hcl.limit.template-depth@1", hcl.DefaultHclParseLimits()},
	} {
		_, failure := hcl.Parse(context.Background(), []byte(c.source), hcl.HclProfileNativeV1,
			hcl.HclEncodingSelectionProfileDefault(), c.limits)
		if failure == nil {
			t.Fatalf("%s: deep nesting must be truncated by the default budgets", c.name)
		}
		if failure.Code() != c.code {
			t.Fatalf("%s: deep nesting code %s, want %s", c.name, failure.Code(), c.code)
		}
	}

	// A stack-safe configured budget truncates before the recursion
	// deepens under explicit limits too.
	tight := hcl.DefaultHclParseLimits()
	tight.MaxExpressionDepth = 24
	for _, source := range []string{deepParens, deepChain} {
		_, failure := hcl.Parse(context.Background(), []byte(source), hcl.HclProfileNativeV1,
			hcl.HclEncodingSelectionProfileDefault(), tight)
		if failure == nil || failure.Code() != "hcl.limit.expression-depth@1" {
			t.Fatalf("tight expression budget must truncate deep nesting with hcl.limit.expression-depth@1, got %v", failure)
		}
	}
	tightBody := hcl.DefaultHclParseLimits()
	tightBody.MaxBodyDepth = 8
	_, failure := hcl.Parse(context.Background(), []byte(deepBlocks.String()), hcl.HclProfileNativeV1,
		hcl.HclEncodingSelectionProfileDefault(), tightBody)
	if failure == nil || failure.Code() != "hcl.limit.body-depth@1" {
		t.Fatalf("tight body budget must truncate deep nesting with hcl.limit.body-depth@1, got %v", failure)
	}
}

// ---------------------------------------------------------------------------
// YAML family
// ---------------------------------------------------------------------------

func TestSecurityYAMLAliasBomb(t *testing.T) {
	// The Rust alias-bomb corpus (yaml_hardening.rs): a document whose
	// references encode exponential duplication in linear source. The
	// graph projection must keep the shared identity (small node count),
	// the default tree projection must reject shared identity, and a
	// bounded duplication must fail with the resource-limit code 鈥?the
	// amplification ratio and value-node limits cannot be bypassed.
	source := "base: &base [zero, one]\n" +
		"level1: &level1 [*base, *base, *base, *base]\n" +
		"level2: &level2 [*level1, *level1, *level1, *level1]\n" +
		"level3: &level3 [*level2, *level2, *level2, *level2]\n" +
		"root: [*level3, *level3, *level3, *level3]\n"
	doc, failure := yaml.Parse([]byte(source), yaml.Yaml12CoreV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("alias corpus must parse: %s", failure.Code())
	}
	if doc.AliasCount() != 16 {
		t.Fatalf("alias count %d, want 16", doc.AliasCount())
	}
	projGraph, err := doc.ProjectGraphBounded(graph.DefaultLimits())
	if err != nil {
		t.Fatalf("graph projection must not expand aliases: %v", err)
	}
	if projGraph.NodeCount() >= 32 {
		t.Fatalf("graph expanded aliases: %d nodes", projGraph.NodeCount())
	}

	defaultResult := doc.ProjectValue(yaml.BestExactValueV1())
	if defaultResult.Complete != nil || defaultResult.Failed.Code() != "yaml.projection.sharing@1" {
		t.Fatalf("default tree projection must reject shared identity, got %v", defaultResult)
	}

	limits := yaml.DefaultValueProjectionLimits()
	limits.MaxValueNodes = 32
	limits.MaxAmplificationRatio = 64
	boundedResult := doc.ProjectValue(yaml.BestExactValueV1().
		WithSharing(yaml.SharingPolicyDuplicateAcyclic).WithLimits(limits))
	if boundedResult.Complete != nil ||
		boundedResult.Failed.Code() != "yaml.projection.resource-limit@1" {
		t.Fatalf("bounded duplication must fail with yaml.projection.resource-limit@1, got %v", boundedResult)
	}
}
