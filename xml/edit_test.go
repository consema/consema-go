package xml

import (
	"context"
	"testing"

	"consema.dev/consema/document"
)

func editParse(t *testing.T, source string) *Document {
	t.Helper()
	doc, failure := Parse(context.Background(), []byte(source), XmlProfileSafeV1,
		XmlEncodingProfileDefault(), DefaultXmlParseLimits())
	if failure != nil {
		t.Fatalf("parse %q: %v", source, failure)
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("edit base %q must be complete", source)
	}
	return doc
}

func editCommit(t *testing.T, doc *Document, tx *EditTransaction) *EditCommit {
	t.Helper()
	commit, failure := doc.Commit(tx)
	if failure != nil {
		t.Fatalf("commit: %v", failure)
	}
	// The committed document must be complete and the patch must replay.
	if commit.Document.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("committed document is not complete")
	}
	return commit
}

func elementRef(t *testing.T, doc *Document, name string, ordinal uint64) document.NodeRef {
	t.Helper()
	occurrence := uint64(0)
	for index, content := range doc.Nodes() {
		if content.Kind != XmlContentElement {
			continue
		}
		if content.Element.QName.Local == name {
			if occurrence == ordinal {
				return doc.OccurrenceNodeRef(uint64(index), document.RoleXmlElement)
			}
			occurrence++
		}
	}
	t.Fatalf("element %s occurrence %d not found", name, ordinal)
	return document.NodeRef{}
}

func attributeRef(t *testing.T, doc *Document, name string, ordinal uint64) document.NodeRef {
	t.Helper()
	occurrence := uint64(0)
	for _, content := range doc.Nodes() {
		if content.Kind != XmlContentElement {
			continue
		}
		for _, attribute := range content.Element.Attributes {
			if attribute.QName.Local == name {
				if occurrence == ordinal {
					return doc.OccurrenceNodeRef(attribute.Ordinal, document.RoleXmlAttribute)
				}
				occurrence++
			}
		}
	}
	t.Fatalf("attribute %s occurrence %d not found", name, ordinal)
	return document.NodeRef{}
}

func textRef(t *testing.T, doc *Document, ordinal uint64) document.NodeRef {
	t.Helper()
	occurrence := uint64(0)
	for _, content := range doc.Nodes() {
		if content.Kind != XmlContentText {
			continue
		}
		if occurrence == ordinal {
			return doc.OccurrenceNodeRef(content.Text.Ordinal, document.RoleXmlText)
		}
		occurrence++
	}
	t.Fatalf("text occurrence %d not found", ordinal)
	return document.NodeRef{}
}

func TestEditReplaceTextEscapes(t *testing.T) {
	doc := editParse(t, "<root>a &lt; b</root>")
	builder := NewEditTransactionBuilder(doc)
	builder.ReplaceText(textRef(t, doc, 0), "x < y & z")
	commit := editCommit(t, doc, builder.Build())
	if got := string(commit.Document.Render()); got != "<root>x &lt; y &amp; z</root>" {
		t.Errorf("render %q", got)
	}
}

func TestEditInsertAndRemoveAttribute(t *testing.T) {
	doc := editParse(t, `<root a="1" b="2"/>`)
	root := elementRef(t, doc, "root", 0)
	b := attributeRef(t, doc, "b", 0)
	builder := NewEditTransactionBuilder(doc)
	builder.InsertAttribute(root, NewNameFacts(nil, "c", nil), "3", AttributePlacementBefore(b))
	commit := editCommit(t, doc, builder.Build())
	if got := string(commit.Document.Render()); got != `<root a="1" c="3" b="2"/>` {
		t.Errorf("before render %q", got)
	}

	doc = commit.Document
	root = elementRef(t, doc, "root", 0)
	builder = NewEditTransactionBuilder(doc)
	builder.InsertAttribute(root, NewNameFacts(nil, "d", nil), "4", AttributePlacementEnd())
	commit = editCommit(t, doc, builder.Build())
	if got := string(commit.Document.Render()); got != `<root a="1" c="3" b="2" d="4"/>` {
		t.Errorf("end render %q", got)
	}

	doc = commit.Document
	d := attributeRef(t, doc, "d", 0)
	builder = NewEditTransactionBuilder(doc)
	builder.RemoveAttribute(d)
	commit = editCommit(t, doc, builder.Build())
	if got := string(commit.Document.Render()); got != `<root a="1" c="3" b="2"/>` {
		t.Errorf("remove render %q", got)
	}
}

func TestEditSetAndRenameAttribute(t *testing.T) {
	doc := editParse(t, `<root a="1"/>`)
	a := attributeRef(t, doc, "a", 0)
	builder := NewEditTransactionBuilder(doc)
	builder.SetAttributeValue(a, "v & w")
	commit := editCommit(t, doc, builder.Build())
	if got := string(commit.Document.Render()); got != `<root a="v &amp; w"/>` {
		t.Errorf("set render %q", got)
	}

	doc = commit.Document
	a = attributeRef(t, doc, "a", 0)
	builder = NewEditTransactionBuilder(doc)
	builder.RenameAttribute(a, NewNameFacts(nil, "renamed", nil))
	commit = editCommit(t, doc, builder.Build())
	if got := string(commit.Document.Render()); got != `<root renamed="v &amp; w"/>` {
		t.Errorf("rename render %q", got)
	}
}

func TestEditInsertElementIntoEmpty(t *testing.T) {
	doc := editParse(t, `<root/>`)
	root := elementRef(t, doc, "root", 0)
	builder := NewEditTransactionBuilder(doc)
	builder.InsertElement(root, NewNameFacts(nil, "x", nil), stringPtr("c"), ContentPlacementEnd())
	commit := editCommit(t, doc, builder.Build())
	if got := string(commit.Document.Render()); got != "<root><x>c</x></root>" {
		t.Errorf("insert into empty render %q", got)
	}

	doc = editParse(t, "<root><a/></root>")
	root = elementRef(t, doc, "root", 0)
	a := elementRef(t, doc, "a", 0)
	builder = NewEditTransactionBuilder(doc)
	builder.InsertElement(root, NewNameFacts(nil, "x", nil), nil, ContentPlacementBefore(a))
	commit = editCommit(t, doc, builder.Build())
	if got := string(commit.Document.Render()); got != "<root><x/><a/></root>" {
		t.Errorf("insert before render %q", got)
	}
}

func TestEditRemoveAndRenameElement(t *testing.T) {
	doc := editParse(t, "<root><a/><b/></root>")
	a := elementRef(t, doc, "a", 0)
	builder := NewEditTransactionBuilder(doc)
	builder.RemoveElement(a)
	commit := editCommit(t, doc, builder.Build())
	if got := string(commit.Document.Render()); got != "<root><b/></root>" {
		t.Errorf("remove render %q", got)
	}

	doc = editParse(t, "<old><child>t</child></old>")
	old := elementRef(t, doc, "old", 0)
	builder = NewEditTransactionBuilder(doc)
	builder.RenameElement(old, NewNameFacts(nil, "new", nil))
	commit = editCommit(t, doc, builder.Build())
	if got := string(commit.Document.Render()); got != "<new><child>t</child></new>" {
		t.Errorf("rename render %q", got)
	}
}

func TestEditCannotRemoveRoot(t *testing.T) {
	doc := editParse(t, "<root/>")
	root := elementRef(t, doc, "root", 0)
	builder := NewEditTransactionBuilder(doc)
	builder.RemoveElement(root)
	_, failure := doc.Commit(builder.Build())
	if failure == nil || failure.Kind != EditFailureCannotRemoveRoot {
		t.Errorf("expected CannotRemoveRoot, got %v", failure)
	}
}

func TestEditRejectsRecoveredBase(t *testing.T) {
	doc, failure := Parse(context.Background(), []byte("<p:root/>"), XmlProfileSafeV1,
		XmlEncodingProfileDefault(), DefaultXmlParseLimits())
	if failure != nil {
		t.Fatalf("parse: %v", failure)
	}
	builder := NewEditTransactionBuilder(doc)
	builder.RenameElement(elementRef(t, doc, "root", 0), NewNameFacts(nil, "x", nil))
	_, editFailure := doc.Commit(builder.Build())
	if editFailure == nil || editFailure.Kind != EditFailureIncompleteTarget {
		t.Errorf("expected IncompleteTarget, got %v", editFailure)
	}
}

func TestEditDryRunMatchesCommit(t *testing.T) {
	doc := editParse(t, `<root a="1"/>`)
	root := elementRef(t, doc, "root", 0)
	builder := NewEditTransactionBuilder(doc)
	builder.InsertAttribute(root, NewNameFacts(nil, "b", nil), "2", AttributePlacementEnd())
	tx := builder.Build()
	plan, failure := doc.DryRun(tx, "test-source")
	if failure != nil {
		t.Fatalf("dry run: %v", failure)
	}
	commit, editFailure := doc.Commit(tx)
	if editFailure != nil {
		t.Fatalf("commit: %v", editFailure)
	}
	if string(commit.Document.Render()) != `<root a="1" b="2"/>` {
		t.Errorf("render %q", commit.Document.Render())
	}
	if len(plan.Replacements()) != len(commit.SourcePatch.Replacements()) {
		t.Errorf("plan/commit replacement counts differ")
	}
	replay, err := commit.SourcePatch.Apply(doc.Source(), document.DefaultSourcePatchLimits())
	if err != nil {
		t.Fatalf("patch replay: %v", err)
	}
	if string(replay.Bytes()) != string(commit.Document.Render()) {
		t.Errorf("patch replay mismatch")
	}
	if failure := commit.UntouchedProof.Verify(doc.Source(), commit.Document.Source(),
		commit.SourcePatch.Replacements()); failure != nil {
		t.Errorf("untouched proof: %v", failure)
	}
}

func TestEditUtf16(t *testing.T) {
	var bytes []byte
	bytes = append(bytes, 0xFF, 0xFE)
	for _, unit := range utf16Encode(`<root a="1"/>`) {
		bytes = append(bytes, byte(unit), byte(unit>>8))
	}
	doc, failure := Parse(context.Background(), bytes, XmlProfileSafeV1,
		XmlEncodingProfileDefault(), DefaultXmlParseLimits())
	if failure != nil {
		t.Fatalf("parse: %v", failure)
	}
	a := attributeRef(t, doc, "a", 0)
	builder := NewEditTransactionBuilder(doc)
	builder.SetAttributeValue(a, "v & w")
	commit, editFailure := doc.Commit(builder.Build())
	if editFailure != nil {
		t.Fatalf("commit: %v", editFailure)
	}
	var expected []byte
	expected = append(expected, 0xFF, 0xFE)
	for _, unit := range utf16Encode(`<root a="v &amp; w"/>`) {
		expected = append(expected, byte(unit), byte(unit>>8))
	}
	if string(commit.Document.Render()) != string(expected) {
		t.Errorf("utf16 render mismatch")
	}
}
