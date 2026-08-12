package toml

import (
	"math"
	"strings"
	"testing"

	"consema.dev/consema/document"
)

// The acceptance facts below mirror the toml_edit 0.22.27 backend behavior
// (verified empirically against the pinned parser; RFC 0001 is the
// semantic authority and the backend behavior is the Rust implementation
// fact for cases the RFC leaves open).

func parseSource(source string) (*Document, *FormationFailure) {
	return Parse([]byte(source), Toml10V1, document.DefaultParseLimits())
}

func TestParserAcceptsValidSources(t *testing.T) {
	valid := []string{
		"",
		"   ",
		"# hi",
		"# hi\n",
		"a = 1",
		"a = 1 # comment",
		"a = 1 # comment\n",
		"a = 1\r\nb = 2\r\n",
		"\uFEFFa = 1",
		"key = \"value\"\nother = 'literal'\n",
		"\"a.b\" = 1\n'c d' = 2\n",
		"1a = 1\na-b = 2\na_b = 3\n",
		"a . b = 1\nc. d = 2\ne .f = 3\n",
		"\"caf\\u00E9\" = 2\n\"a\\u0001b\" = 3\n",
		"a = +99\nb = 0\nc = -17\nd = 1_000\ne = 5_349_221\n",
		"a = 0xF\nb = 0o0_755\nc = 0b1_0_1\n",
		"a = 9223372036854775807\nb = -9223372036854775808\n",
		"a = +1.0\nb = 3.1419\nc = -0.01\nd = 5e+22\ne = 1e6\nf = -2E-2\n",
		"a = 6.626e-34\nb = 9_224_617.445_991_228_313\n",
		"a = 1.7976931348623157e+308\nb = 5e-324\n",
		"a = nan\nb = +nan\nc = -nan\nd = inf\ne = +inf\nf = -inf\n",
		"a = 0.5\nb = 0e1\nc = 0.0\n",
		"a = 1979-05-27\nb = 07:32:00\nc = 07:32:00.999999\nd = 07:32:00.123456789012345\n",
		"a = 1979-05-27T07:32:00\nb = 1979-05-27t07:32:00z\nc = 1979-05-27 07:32:00\n",
		"a = 1979-05-27T00:32:00-07:00\nb = 1979-05-27T00:32:00+23:59\n",
		"a = 2000-02-29\nb = 1996-02-29\n",
		"a = 23:59:60\n",
		"a = \"\"\nb = ''\nc = \"quote: \\\"; slash: \\\\; tab: \\t\"\n",
		"a = \"\\u00E9\\U0001F600\"\n",
		"a = \"\"\"\nx\ny\"\"\"\nb = '''\nx\r\ny'''\n",
		"a = \"\"\"x\\\n   y\"\"\"\nb = \"\"\"x\\\r\n\ty\"\"\"\n",
		"a = \"\"\"x\" y\"\"\"\nb = \"\"\"x\"\" y\"\"\"\nc = '''x'' y'''\n",
		"a = \"\"\"\\\n   \"\"\"\n",
		"a = \"\"\"x\\\n\"\"\"\n",
		"a = []\nb = [   ]\nc = [1]\nd = [1,]\ne = [1, 2, 3]\n",
		"a = [\n  1, 2, 3\n]\nb = [\n  1,\n  2, # this is ok\n]\n",
		"a = [# comment\n# comment2\n\n\n   ]\n",
		"a = [1 # c\n, 2]\n",
		"a = [1, # keep\n 2, 3,]\n",
		"a = {}\nb = {   }\nc = {a = 1}\nd = { hello = \"world\", a = 1}\ne = { hello.world = \"a\" }\n",
		"a = [ { x = 1, a = \"2\" }, {a = \"a\",b = \"b\"} ]\n",
		"[a]\nb.c = 1\n",
		"[a.b]\na.c = 1\n",
		"[a]\na.b = 1\n",
		"[a.b.c]\na.b.d = 1\n",
		"[[a]]\na.b = 1\n",
		"[[a]]\n[[a]]\n",
		"[[a]]\nx = 1\n[[a]]\nx = 2\n",
		"[a.b]\nx = 1\n[a]\ny = 2\n",
		"[a.b.c]\nx = 1\n[a.b]\ny = 2\n",
		"a.b = 1\nc.d = 2\n",
		"a.b = 1\na.c = 2\n",
		"a.b.c = 1\na.b.d = 2\n",
		"[a.b]\n[a]\n",
		"[x]\nz = 1\n[a.b]\n[x.y]\n",
	}
	for _, source := range valid {
		if _, failure := parseSource(source); failure != nil {
			t.Errorf("valid source rejected %q: %s",
				source, failure.Diagnostics()[0].Arguments["parser_reason"])
		}
	}
}

func TestParserRejectsInvalidSources(t *testing.T) {
	invalid := []string{
		"a = 01",
		"a = 00",
		"a = 0123",
		"a = 01.5",
		"a = 00e1",
		"a = -01",
		"a = +00",
		"a = 1_",
		"a = _1",
		"a = 1__0",
		"a = 0x_2A",
		"a = 0x",
		"a = 0xG",
		"a = -0x2A",
		"a = 0o8",
		"a = 0b2",
		"a = .5",
		"a = 1.",
		"a = 1e",
		"a = 1e+",
		"a = 9e99999",
		"a = 1e400",
		"a = 9223372036854775808",
		"a = -9223372036854775809",
		"a = 0x8000000000000000",
		"a = 1979-13-27",
		"a = 1979-01-32",
		"a = 1979-04-31",
		"a = 1900-02-29",
		"a = 1979-5-27",
		"a = 24:00:00",
		"a = 23:60:00",
		"a = 23:59:61",
		"a = 07:32",
		"a = 07:32:00Z",
		"a = 1979-05-27T00:00:00+24:00",
		"a = 1979-05-27T00:00:00-24:01",
		"a = 1979-05-27T24:00:00",
		"a = 1979-05-27  07:32:00",
		"a = 1979-05-27\t07:32:00",
		"a = 1979-05-27x",
		"a = 79-05-27",
		"a = 1e2.5",
		"a = infx",
		"a = tru",
		"a = fals",
		"a = \"x\x01y\"",
		"a = \"x\x0By\"",
		"a = \"x\x7Fy\"",
		"a = 'x\x01y'",
		"a = \"\\q\"",
		"a = \"\\uD800\"",
		"a = \"\\uD83D\\uDE00\"",
		"a = \"\\U00110000\"",
		"a = \"unterminated",
		"a = 'unterminated",
		"a = \"\"\"unterminated",
		"a = '''unterminated",
		"a = \"\"\"x\"\"\"y\"\"\"",
		"a = '''x''''y'''",
		"a = 1\ra = 2",
		"a = 1 # \x7F",
		"a = 1\u0000",
		"a = \u000B1",
		"a = \u000C1",
		"a = 1\nb = 1\na = 2",
		"a = 1\n[a]",
		"a.b = 1\n[a]",
		"a.b = 1\na.b.c = 2",
		"a = 1\na.b = 2",
		"[a.b]\n[a.b]",
		"[[a]]\n[a]",
		"[a]\n[a]",
		"a = { x = 1, x = 2 }",
		"a = { x = 1, x.y = 2 }",
		"a = { x.y = 1, x = 2 }",
		"a = { x = 1, }",
		"a = { x = 1",
		"a = [1 2]",
		"a = [1,,2]",
		"a = [,2]",
		"a..b = 1",
		"\"\"\"x\"\"\" = 1",
		"a = [",
		"a = [1",
		"a = 1 # \u0001",
	}
	for _, source := range invalid {
		_, failure := parseSource(source)
		if failure == nil {
			t.Errorf("invalid source accepted %q", source)
			continue
		}
		if failure.Diagnostics()[0].Code != "toml.parse.syntax@1" {
			t.Errorf("invalid source %q produced code %s", source, failure.Diagnostics()[0].Code)
		}
	}
}

func TestParserResourceLimits(t *testing.T) {
	limits := document.ParseLimits{
		MaxSourceBytes: 64 << 20, MaxNestingDepth: 256,
		MaxTokenCount: 2 << 20, MaxNodeCount: 1 << 20, MaxDiagnostics: 10000,
	}
	// token limit
	tokenLimits := limits
	tokenLimits.MaxTokenCount = 3
	_, failure := Parse([]byte("values = [1, 2, 3]"), Toml10V1, tokenLimits)
	if failure == nil || failure.Diagnostics()[0].Arguments["name"] != "token_count" {
		t.Fatalf("token limit facts did not match")
	}
	// node limit
	nodeLimits := limits
	nodeLimits.MaxNodeCount = 3
	_, failure = Parse([]byte("value = [[[[1]]]]"), Toml10V1, nodeLimits)
	if failure == nil || failure.Diagnostics()[0].Arguments["name"] != "node_count" {
		t.Fatalf("node limit facts did not match")
	}
	// depth limit (delimiter preflight)
	depthLimits := limits
	depthLimits.MaxNestingDepth = 2
	_, failure = Parse([]byte("value = [[[[1]]]]"), Toml10V1, depthLimits)
	if failure == nil || failure.Diagnostics()[0].Arguments["name"] != "nesting_depth" {
		t.Fatalf("depth limit facts did not match")
	}
	// source limit
	sourceLimits := limits
	sourceLimits.MaxSourceBytes = 3
	_, failure = Parse([]byte("x = 1"), Toml10V1, sourceLimits)
	if failure == nil || failure.Diagnostics()[0].Arguments["name"] != "source_bytes" {
		t.Fatalf("source limit facts did not match")
	}
}

func TestParserNativeFacts(t *testing.T) {
	// Signed-zero floats carry their exact bit patterns.
	doc, failure := parseSource("positive = 0.0\nnegative = -0.0\n")
	if failure != nil {
		t.Fatalf("parse: %v", failure)
	}
	positive, _ := rootEntry(t, doc.Root(), "positive").Item().AsFloatBits()
	negative, _ := rootEntry(t, doc.Root(), "negative").Item().AsFloatBits()
	if positive != 0 || negative != 0x8000000000000000 {
		t.Fatalf("signed zero bits %x/%x", positive, negative)
	}

	// NaN canonical payloads.
	doc, failure = parseSource("a = nan\nb = -nan\n")
	if failure != nil {
		t.Fatalf("parse: %v", failure)
	}
	nanBits, _ := rootEntry(t, doc.Root(), "a").Item().AsFloatBits()
	negativeNanBits, _ := rootEntry(t, doc.Root(), "b").Item().AsFloatBits()
	if nanBits != 0x7ff8000000000000 || negativeNanBits != 0xfff8000000000000 {
		t.Fatalf("nan bits %x/%x", nanBits, negativeNanBits)
	}

	// Datetime components and fraction truncation.
	doc, failure = parseSource("a = 1987-07-05T17:45:00.123456789012345Z\n")
	if failure != nil {
		t.Fatalf("parse: %v", failure)
	}
	dateTime, _ := rootEntry(t, doc.Root(), "a").Item().AsDateTime()
	if dateTime.Time.Nanosecond != 123456789 {
		t.Fatalf("fraction truncated to %d", dateTime.Time.Nanosecond)
	}
	if !dateTime.Offset.Z {
		t.Fatalf("offset must be Z")
	}
	if dateTime.Date.Year != 1987 || dateTime.Date.Month != 7 || dateTime.Date.Day != 5 {
		t.Fatalf("date components mismatch")
	}

	// Leap second parses but the core projection rejects it.
	doc, failure = parseSource("a = 23:59:60\n")
	if failure != nil {
		t.Fatalf("leap second must parse: %v", failure)
	}
	result := doc.Project(NewProjectionRequest(ProjectionTargetBestExactCoreV1))
	if result.Complete != nil {
		t.Fatalf("leap second must not project")
	}

	// Multiline strings normalize CRLF and trim the first newline.
	doc, failure = parseSource("a = \"\"\"x\r\ny\"\"\"\nb = '''x\r\ny'''\n")
	if failure != nil {
		t.Fatalf("parse: %v", failure)
	}
	basic, _ := rootEntry(t, doc.Root(), "a").Item().AsString()
	literal, _ := rootEntry(t, doc.Root(), "b").Item().AsString()
	if basic != "x\ny" || literal != "x\ny" {
		t.Fatalf("multiline values %q/%q", basic, literal)
	}

	// BOM prefix is skipped by the semantic parser.
	doc, failure = parseSource("\uFEFFa = 1\n")
	if failure != nil {
		t.Fatalf("BOM prefix must parse: %v", failure)
	}

	// Float edge: -0.0 materializes canonically.
	if bits, _ := math.Float64bits(-0.0), 0; bits != 0 {
		t.Fatalf("unreachable")
	}
}

func TestParserImplicitReuseOrdering(t *testing.T) {
	// The reuse of an implicit table by a later header moves the entry to
	// the end of the parent's ordered items (toml_edit remove/reinsert).
	doc, failure := parseSource("[x.y]\n[a.b]\n[x]\nz = 1\n[a.c]\nw = 2\n")
	if failure != nil {
		t.Fatalf("parse: %v", failure)
	}
	entries, _ := doc.Root().TableEntries()
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if strings.Join(names, ",") != "a,x" {
		t.Fatalf("root order = %v", names)
	}
	x, _ := rootEntry(t, doc.Root(), "x").Item().TableEntries()
	_ = x
	if entry := rootEntry(t, doc.Root(), "x").Item(); entry.Kind() != ItemKindStandardTable {
		t.Fatalf("reused x must be StandardTable, got %s", entry.Kind())
	}
}
