package yaml

import (
	"math/big"
	"testing"

	"consema.dev/consema/core"
)

func editDoc(t *testing.T, source string) *Document {
	t.Helper()
	return mustParse(t, source, Yaml12CoreV1)
}

func intValue(t *testing.T, text string) core.Integer {
	t.Helper()
	number := new(big.Int)
	if _, ok := number.SetString(text, 10); !ok {
		t.Fatalf("invalid integer %q", text)
	}
	return core.NewInteger(number)
}

// TestEditScalarPreserveCompatible pins the style-and-category-preserving
// semantic replacement (edit.rs preserved_literal).
func TestEditScalarPreserveCompatible(t *testing.T) {
	doc := editDoc(t, "# keep\na: 1\nb: two\n")
	root, _ := doc.Document(0)
	entry, _ := root.Root().MappingEntry(0)
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticScalar(entry.Value().NodeRef(), intValue(t, "2"),
		RepresentationPolicyPreserveCompatible)
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("edit failed: %s", failure.Name())
	}
	expected := "# keep\na: 2\nb: two\n"
	if string(commit.Document.Render()) != expected {
		t.Fatalf("render %q, want %q", string(commit.Document.Render()), expected)
	}
	if len(commit.ChangeSet.SourceEdits()) != 1 {
		t.Fatalf("source edit count %d", len(commit.ChangeSet.SourceEdits()))
	}
	if failure := commit.UntouchedProof.Verify(doc.source, commit.Document.source,
		commit.SourcePatch.Replacements()); failure != nil {
		t.Fatalf("untouched proof failed: %v", failure)
	}
}

// TestEditScalarRepresentationIncompatible pins the
// PreserveCompatible rejection when the category changes.
func TestEditScalarRepresentationIncompatible(t *testing.T) {
	doc := editDoc(t, "a: one\n")
	root, _ := doc.Document(0)
	entry, _ := root.Root().MappingEntry(0)
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticScalar(entry.Value().NodeRef(), core.Boolean(true),
		RepresentationPolicyPreserveCompatible)
	_, failure := doc.Commit(builder.Build())
	if failure == nil || failure.Name() != "RepresentationIncompatible" {
		t.Fatalf("failure %v", failure)
	}
	if string(doc.Render()) != "a: one\n" {
		t.Fatalf("base source changed after failure")
	}
}

// TestEditScalarCanonicalFallback pins the explicit canonical fallback
// with the anchor properties preserved (edit.rs test).
func TestEditScalarCanonicalFallback(t *testing.T) {
	doc := editDoc(t, "value: &x plain\ncopy: *x\n")
	root, _ := doc.Document(0)
	entry, _ := root.Root().MappingEntry(0)
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticScalar(entry.Value().NodeRef(), core.Boolean(true),
		RepresentationPolicyPreserveElseCanonical)
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("edit failed: %s", failure.Name())
	}
	expected := "value: &x !!bool \"true\"\ncopy: *x\n"
	if string(commit.Document.Render()) != expected {
		t.Fatalf("render %q, want %q", string(commit.Document.Render()), expected)
	}
	diagnostics := commit.ChangeSet.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != "yaml.edit.canonical-fallback@1" {
		t.Fatalf("fallback diagnostic missing")
	}
	// The shared anchored node changed; the alias observes the new value.
	alias, _ := commit.Document.Alias(0)
	scalar, _ := alias.Target().Scalar()
	if scalar.Canonical() != "true" {
		t.Fatalf("alias target canonical %q", scalar.Canonical())
	}
}

// TestEditScalarLiteral pins the exact literal replacement (edit.rs test).
func TestEditScalarLiteral(t *testing.T) {
	doc := editDoc(t, "a: \"old\"\n")
	root, _ := doc.Document(0)
	entry, _ := root.Root().MappingEntry(0)
	builder := NewEditTransactionBuilder(doc)
	builder.LiteralScalar(entry.Value().NodeRef(), []byte("'new'"))
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("edit failed: %s", failure.Name())
	}
	if string(commit.Document.Render()) != "a: 'new'\n" {
		t.Fatalf("render %q", string(commit.Document.Render()))
	}
}

// TestEditScalarLiteralRejectsNonScalar pins the literal validation.
func TestEditScalarLiteralRejectsNonScalar(t *testing.T) {
	doc := editDoc(t, "a: one\n")
	root, _ := doc.Document(0)
	entry, _ := root.Root().MappingEntry(0)
	for _, literal := range []string{"[not, scalar]", "&a x", "!!int x", "# comment"} {
		builder := NewEditTransactionBuilder(doc)
		builder.LiteralScalar(entry.Value().NodeRef(), []byte(literal))
		_, failure := doc.Commit(builder.Build())
		if failure == nil || failure.Name() != "InvalidLiteral" {
			t.Fatalf("literal %q failure %v", literal, failure)
		}
	}
}

// TestEditAnchorRenameUpdatesDependentAliases pins the one-transaction
// alias updates with shadowing (edit.rs test).
func TestEditAnchorRenameUpdatesDependentAliases(t *testing.T) {
	doc := editDoc(t, "first: &x [one]\ncopy: *x\nother: &x [two]\ncopy2: *x\n")
	root, _ := doc.Document(0)
	first, _ := root.Root().MappingEntry(0)
	anchorRef, _ := first.Value().AnchorNodeRef()
	builder := NewEditTransactionBuilder(doc)
	builder.RenameAnchor(anchorRef, "renamed")
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("edit failed: %s", failure.Name())
	}
	expected := "first: &renamed [one]\ncopy: *renamed\nother: &x [two]\ncopy2: *x\n"
	if string(commit.Document.Render()) != expected {
		t.Fatalf("render %q, want %q", string(commit.Document.Render()), expected)
	}
}

// TestEditInsertFlowPins the separator-aware flow insertions
// (edit.rs tests).
func TestEditInsertFlow(t *testing.T) {
	doc := editDoc(t, "seq: [one, two]\nmap: {a: 1}\n")
	root, _ := doc.Document(0)
	sequenceEntry, _ := root.Root().MappingEntry(0)
	mappingEntry, _ := root.Root().MappingEntry(1)
	sequence := sequenceEntry.Value()
	mapping := mappingEntry.Value()
	second, _ := sequence.SequenceItem(1)
	builder := NewEditTransactionBuilder(doc)
	builder.InsertSequenceElement(sequence.NodeRef(), core.Boolean(true),
		PlacementBefore(second.NodeRef()))
	builder.InsertMappingEntry(mapping.NodeRef(), core.String("b"), intValue(t, "2"),
		PlacementEnd())
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("edit failed: %s", failure.Name())
	}
	expected := "seq: [one, !!bool \"true\", two]\nmap: {a: 1, ? !!str \"b\" : !!int \"2\"}\n"
	if string(commit.Document.Render()) != expected {
		t.Fatalf("render %q, want %q", string(commit.Document.Render()), expected)
	}
}

// TestEditInsertBlockPins the block insertion with comments and CRLF
// (edit.rs test).
func TestEditInsertBlock(t *testing.T) {
	source := "root:\r\n  - one # keep-one\r\n  - two"
	doc := editDoc(t, source)
	root, _ := doc.Document(0)
	entry, _ := root.Root().MappingEntry(0)
	sequence := entry.Value()
	first, _ := sequence.SequenceItem(0)
	builder := NewEditTransactionBuilder(doc)
	builder.InsertSequenceElement(sequence.NodeRef(), core.String("inserted"),
		PlacementAfter(first.NodeRef()))
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("edit failed: %s", failure.Name())
	}
	expected := "root:\r\n  - one # keep-one\r\n  - !!str \"inserted\"\r\n  - two"
	if string(commit.Document.Render()) != expected {
		t.Fatalf("render %q, want %q", string(commit.Document.Render()), expected)
	}
}

// TestEditRemoveSequenceElement pins the block removal with the comment
// preserved (edit.rs test).
func TestEditRemoveSequenceElement(t *testing.T) {
	doc := editDoc(t, "seq:\n  - one # keep\n  - two\n")
	root, _ := doc.Document(0)
	entry, _ := root.Root().MappingEntry(0)
	sequence := entry.Value()
	first, _ := sequence.SequenceItem(0)
	builder := NewEditTransactionBuilder(doc)
	builder.RemoveSequenceElement(first.NodeRef())
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("edit failed: %s", failure.Name())
	}
	expected := "seq:\n  - two\n"
	if string(commit.Document.Render()) != expected {
		t.Fatalf("render %q, want %q", string(commit.Document.Render()), expected)
	}
}

// TestEditRemoveLastBlockElement pins the reversible empty replacement.
func TestEditRemoveLastBlockElement(t *testing.T) {
	doc := editDoc(t, "seq:\n  - one # keep\n")
	root, _ := doc.Document(0)
	entry, _ := root.Root().MappingEntry(0)
	sequence := entry.Value()
	first, _ := sequence.SequenceItem(0)
	builder := NewEditTransactionBuilder(doc)
	builder.RemoveSequenceElement(first.NodeRef())
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("edit failed: %s", failure.Name())
	}
	expected := "seq:\n  [] # keep\n"
	if string(commit.Document.Render()) != expected {
		t.Fatalf("render %q, want %q", string(commit.Document.Render()), expected)
	}
}

// TestEditAnchorDependency pins the removal rejection when a dependent
// alias remains (vector edit.anchor-dependency).
func TestEditAnchorDependency(t *testing.T) {
	doc := editDoc(t, "seq:\n  - &x one\ncopy: *x\n")
	root, _ := doc.Document(0)
	entry, _ := root.Root().MappingEntry(0)
	sequence := entry.Value()
	target, _ := sequence.SequenceItem(0)
	builder := NewEditTransactionBuilder(doc)
	builder.RemoveSequenceElement(target.NodeRef())
	_, failure := doc.Commit(builder.Build())
	if failure == nil || failure.Code() != "yaml.edit.anchor-dependency@1" {
		t.Fatalf("failure %v", failure)
	}
}

// TestEditInsertAliasPins the earlier-visible-anchor rule.
func TestEditInsertAlias(t *testing.T) {
	doc := editDoc(t, "first: &x [one]\nseq: [two]\n")
	root, _ := doc.Document(0)
	first, _ := root.Root().MappingEntry(0)
	anchorRef, _ := first.Value().AnchorNodeRef()
	seqEntry, _ := root.Root().MappingEntry(1)
	sequence := seqEntry.Value()
	builder := NewEditTransactionBuilder(doc)
	builder.InsertAlias(sequence.NodeRef(), anchorRef, PlacementEnd())
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("edit failed: %s", failure.Name())
	}
	expected := "first: &x [one]\nseq: [two, *x]\n"
	if string(commit.Document.Render()) != expected {
		t.Fatalf("render %q, want %q", string(commit.Document.Render()), expected)
	}
}

// TestEditInsertAliasVisibility pins the AnchorNotVisible rejection for a
// shadowed anchor (edit.rs test).
func TestEditInsertAliasVisibility(t *testing.T) {
	doc := editDoc(t, "first: &x [one]\nsecond: &x [two]\nseq: [three]\n")
	root, _ := doc.Document(0)
	first, _ := root.Root().MappingEntry(0)
	anchorRef, _ := first.Value().AnchorNodeRef()
	seqEntry, _ := root.Root().MappingEntry(2)
	sequence := seqEntry.Value()
	builder := NewEditTransactionBuilder(doc)
	builder.InsertAlias(sequence.NodeRef(), anchorRef, PlacementEnd())
	_, failure := doc.Commit(builder.Build())
	if failure == nil || failure.Code() != "yaml.edit.anchor-not-visible@1" {
		t.Fatalf("failure %v", failure)
	}
}

// TestEditAliasRemovalDoesNotRemoveTarget pins RFC 0007 §12.
func TestEditAliasRemovalDoesNotRemoveTarget(t *testing.T) {
	doc := editDoc(t, "&self [one, *self]\n")
	root, _ := doc.Document(0)
	sequence := root.Root()
	aliasItem, _ := sequence.SequenceItem(1)
	builder := NewEditTransactionBuilder(doc)
	builder.RemoveSequenceElement(aliasItem.NodeRef())
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("edit failed: %s", failure.Name())
	}
	if string(commit.Document.Render()) != "&self [one]\n" {
		t.Fatalf("render %q", string(commit.Document.Render()))
	}
}

// TestEditStructuralContainerConflict pins the one-mutation-per-container
// rule (edit.rs test).
func TestEditStructuralContainerConflict(t *testing.T) {
	doc := editDoc(t, "seq: [one, two]\n")
	root, _ := doc.Document(0)
	entry, _ := root.Root().MappingEntry(0)
	sequence := entry.Value()
	first, _ := sequence.SequenceItem(0)
	second, _ := sequence.SequenceItem(1)
	builder := NewEditTransactionBuilder(doc)
	builder.RemoveSequenceElement(first.NodeRef())
	builder.RemoveSequenceElement(second.NodeRef())
	_, failure := doc.Commit(builder.Build())
	if failure == nil || failure.Code() != "yaml.edit.structural-container-conflict@1" {
		t.Fatalf("failure %v", failure)
	}
}

// TestEditWrongSnapshot pins the snapshot binding.
func TestEditWrongSnapshot(t *testing.T) {
	doc := editDoc(t, "a: 1\n")
	other := editDoc(t, "a: 2\n")
	root, _ := doc.Document(0)
	entry, _ := root.Root().MappingEntry(0)
	builder := NewEditTransactionBuilder(other)
	builder.SemanticScalar(entry.Value().NodeRef(), intValue(t, "3"),
		RepresentationPolicyCanonicalForProfile)
	_, failure := doc.Commit(builder.Build())
	if failure == nil || failure.Name() != "WrongSnapshot" {
		t.Fatalf("failure %v", failure)
	}
}

// TestEditDuplicateTarget pins the duplicate-target rejection.
func TestEditDuplicateTarget(t *testing.T) {
	doc := editDoc(t, "a: 1\n")
	root, _ := doc.Document(0)
	entry, _ := root.Root().MappingEntry(0)
	target := entry.Value().NodeRef()
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticScalar(target, intValue(t, "2"), RepresentationPolicyCanonicalForProfile)
	builder.SemanticScalar(target, intValue(t, "3"), RepresentationPolicyCanonicalForProfile)
	_, failure := doc.Commit(builder.Build())
	if failure == nil || failure.Name() != "DuplicateTarget" {
		t.Fatalf("failure %v", failure)
	}
}

// TestEditUnsupportedSemanticValue pins the container rejection.
func TestEditUnsupportedSemanticValue(t *testing.T) {
	doc := editDoc(t, "a: 1\n")
	root, _ := doc.Document(0)
	entry, _ := root.Root().MappingEntry(0)
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticScalar(entry.Value().NodeRef(), core.NewArray(core.String("x")),
		RepresentationPolicyCanonicalForProfile)
	_, failure := doc.Commit(builder.Build())
	if failure == nil || failure.Name() != "UnsupportedSemanticValue" {
		t.Fatalf("failure %v", failure)
	}
}
