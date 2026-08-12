package hcl

// Recovery-negative matrix transcribed from the Rust reference parser
// (consema-hcl parser.rs; RFC 0014 §3). Unclosed bodies, invalid token
// sequences, and truncated expressions classify as Recovered with the
// frozen `hcl.parse.*@1` codes; every error region carries the same code
// as its diagnostic; and every proven item survives the recovery.

import (
	"testing"

	"consema.dev/consema/document"
)

// assertRecoveredWith asserts the document is Recovered and carries the
// diagnostic code.
func assertRecoveredWith(t *testing.T, doc *Document, code string) {
	t.Helper()
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("expected Recovered, got %s", doc.FormationStatus())
	}
	if !containsCode(codes(doc), code) {
		t.Fatalf("diagnostic %s missing: %v", code, codes(doc))
	}
}

// assertRegion asserts exactly one error region with the code and span.
func assertRegion(t *testing.T, doc *Document, code string, start, end int) {
	t.Helper()
	regions := doc.ErrorRegions()
	if len(regions) != 1 {
		t.Fatalf("error regions %d, want 1", len(regions))
	}
	if regions[0].Code() != code {
		t.Fatalf("region code %s, want %s", regions[0].Code(), code)
	}
	if regions[0].Span().StartByte() != start || regions[0].Span().EndByte() != end {
		t.Fatalf("region span [%d,%d), want [%d,%d)", regions[0].Span().StartByte(),
			regions[0].Span().EndByte(), start, end)
	}
}

// attributeNames lists the names of the attribute items of the root body.
func attributeNames(doc *Document) []string {
	var names []string
	for _, item := range doc.Document().Body().Items() {
		if attribute := item.AsAttribute(); attribute != nil {
			names = append(names, attribute.Name())
		}
	}
	return names
}

// TestUnclosedBodyIsRecoveredToEOF pins the unclosed-block recovery:
// the whole block is one `hcl.parse.block@1` error region to end of file
// and no native item survives (parser.rs
// unclosed_block_is_recovered_with_region_to_eof).
func TestUnclosedBodyIsRecoveredToEOF(t *testing.T) {
	doc := parseForms(t, "x {\n  a = 1\n", HclProfileNativeV1)
	assertRecoveredWith(t, doc, "hcl.parse.block@1")
	if !doc.Document().Body().IsEmpty() {
		t.Fatalf("the failed block must enter no native item")
	}
	assertRegion(t, doc, "hcl.parse.block@1", 0, 12)
}

// TestExpressionTruncationRecovery pins truncated-expression recovery:
// the region ends at end of line, or at the matching close of an open
// bracket, or at end of file when no close exists (parser.rs
// missing_expression_is_an_attribute_failure,
// incomplete_expression_region_ends_at_end_of_line,
// unterminated_bracket_extends_to_the_matching_close,
// unterminated_bracket_without_close_extends_to_end_of_file).
func TestExpressionTruncationRecovery(t *testing.T) {
	// Missing expression: region is the attribute line, next item survives.
	doc := parseForms(t, "a =\nb = 2\n", HclProfileNativeV1)
	assertRecoveredWith(t, doc, "hcl.parse.expression@1")
	if names := attributeNames(doc); len(names) != 1 || names[0] != "b" {
		t.Fatalf("attributes %v, want [b]", names)
	}
	assertRegion(t, doc, "hcl.parse.expression@1", 0, 3)

	// Truncated binary expression: region ends at end of line.
	doc = parseForms(t, "a = 1 +\nb = 2\n", HclProfileNativeV1)
	assertRecoveredWith(t, doc, "hcl.parse.expression@1")
	if names := attributeNames(doc); len(names) != 1 || names[0] != "b" {
		t.Fatalf("attributes %v, want [b]", names)
	}
	assertRegion(t, doc, "hcl.parse.expression@1", 0, 7)

	// Unterminated bracket: region extends to the matching close across
	// line ends.
	doc = parseForms(t, "a = [1, 2\nb = 3]\nc = 4\n", HclProfileNativeV1)
	assertRecoveredWith(t, doc, "hcl.parse.expression@1")
	if names := attributeNames(doc); len(names) != 1 || names[0] != "c" {
		t.Fatalf("attributes %v, want [c]", names)
	}
	assertRegion(t, doc, "hcl.parse.expression@1", 0, 16)

	// Unterminated bracket without a close: region to end of file, no
	// native item survives.
	doc = parseForms(t, "a = [1, 2\nb = 3\n", HclProfileNativeV1)
	assertRecoveredWith(t, doc, "hcl.parse.expression@1")
	if !doc.Document().Body().IsEmpty() {
		t.Fatalf("region to EOF must leave an empty body")
	}
	assertRegion(t, doc, "hcl.parse.expression@1", 0, 16)
}

// TestInvalidTokenSequencesAreItemFailures pins the item-level recovery
// of token sequences that can start neither an attribute nor a block
// header (parser.rs bare_identifier_before_equals_is_an_item_error,
// invalid_body_item_becomes_an_error_region,
// orphan_closing_delimiter_is_consumed_with_a_diagnostic).
func TestInvalidTokenSequencesAreItemFailures(t *testing.T) {
	// `= 1` cannot start an item.
	doc := parseForms(t, "= 1\nb = 2\n", HclProfileNativeV1)
	assertRecoveredWith(t, doc, "hcl.parse.item@1")
	if names := attributeNames(doc); len(names) != 1 || names[0] != "b" {
		t.Fatalf("attributes %v, want [b]", names)
	}
	assertRegion(t, doc, "hcl.parse.item@1", 0, 3)

	// Bare identifier before junk.
	doc = parseForms(t, "a 1\nb = 2\n", HclProfileNativeV1)
	assertRecoveredWith(t, doc, "hcl.parse.item@1")
	if names := attributeNames(doc); len(names) != 1 || names[0] != "b" {
		t.Fatalf("attributes %v, want [b]", names)
	}

	// Orphan closing delimiter is consumed with a diagnostic.
	doc = parseForms(t, "a = 1\n}\nb = 2\n", HclProfileNativeV1)
	assertRecoveredWith(t, doc, "hcl.parse.item@1")
	if names := attributeNames(doc); len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("attributes %v, want [a b]", names)
	}
}

// TestMissingNewlineRecovery pins the newline recovery: the attribute or
// block is proven and survives while the rest of the line is consumed
// (parser.rs missing_newline_after_attribute_survives_and_eats_the_line,
// missing_newline_inside_block_preserves_the_closing_brace).
func TestMissingNewlineRecovery(t *testing.T) {
	doc := parseForms(t, "a = 1 b = 2\nc = 3\n", HclProfileNativeV1)
	assertRecoveredWith(t, doc, "hcl.parse.newline@1")
	if names := attributeNames(doc); len(names) != 2 || names[0] != "a" || names[1] != "c" {
		t.Fatalf("attributes %v, want [a c]", names)
	}
	if len(doc.ErrorRegions()) != 0 {
		t.Fatalf("newline recovery must not emit error regions")
	}

	doc = parseForms(t, "x {\n  a = 1 }\ny = 2\n", HclProfileNativeV1)
	assertRecoveredWith(t, doc, "hcl.parse.newline@1")
	if len(doc.Document().Body().Items()) != 2 {
		t.Fatalf("block and attribute must survive")
	}
	if names := attributeNames(doc); len(names) != 1 || names[0] != "y" {
		t.Fatalf("attributes %v, want [y]", names)
	}
}

// TestUnterminatedStringRecovery pins the lexer recovery: the unterminated
// string kills its item and the next item survives (parser.rs
// unterminated_quoted_string_kills_the_item_and_the_next_survives).
func TestUnterminatedStringRecovery(t *testing.T) {
	doc := parseForms(t, "a = \"open\nb = 2\n", HclProfileNativeV1)
	assertRecoveredWith(t, doc, "hcl.parse.unterminated-string@1")
	if names := attributeNames(doc); len(names) != 1 || names[0] != "b" {
		t.Fatalf("attributes %v, want [b]", names)
	}
}

// TestUnterminatedHeredocRecovery pins the heredoc lexer recovery: the
// region extends to end of file, so the rest of the source belongs to it
// (parser.rs unterminated_heredoc_kills_the_item_and_the_next_survives).
func TestUnterminatedHeredocRecovery(t *testing.T) {
	doc := parseForms(t, "a = <<EOT\ncontent\nb = 2\n", HclProfileNativeV1)
	assertRecoveredWith(t, doc, "hcl.parse.unterminated-heredoc@1")
	if !doc.Document().Body().IsEmpty() {
		t.Fatalf("heredoc region to EOF must leave an empty body")
	}
}

// TestOneLineBlockBrokenContent pins one-line block recovery: a broken
// attribute keeps the (empty) block, invalid content and a missing close
// keep the block failed (parser.rs
// one_line_block_with_broken_attribute_keeps_the_block,
// one_line_block_with_invalid_content_is_recovered,
// one_line_block_missing_close_is_recovered).
func TestOneLineBlockBrokenContent(t *testing.T) {
	doc := parseForms(t, "b { a }\n", HclProfileNativeV1)
	assertRecoveredWith(t, doc, "hcl.parse.attribute@1")
	if len(doc.Document().Body().Items()) != 1 || doc.Document().Body().Items()[0].AsBlock() == nil {
		t.Fatalf("the block must survive with an empty body")
	}

	doc = parseForms(t, "b { = 1 }\n", HclProfileNativeV1)
	assertRecoveredWith(t, doc, "hcl.parse.block@1")
	if len(doc.Document().Body().Items()) != 1 || doc.Document().Body().Items()[0].AsBlock() == nil {
		t.Fatalf("the block must survive with an empty body")
	}

	doc = parseForms(t, "b { a = 1\n", HclProfileNativeV1)
	assertRecoveredWith(t, doc, "hcl.parse.block@1")
	if !doc.Document().Body().IsEmpty() {
		t.Fatalf("one-line block missing close must leave an empty body")
	}
}
