package properties

import (
	"testing"

	"consema.dev/consema/document"
)

func editReader(t *testing.T, source string) *Document {
	t.Helper()
	return queryReader(t, source)
}

func commitOne(t *testing.T, doc *Document,
	operation func(*EditTransactionBuilder)) *EditCommit {
	t.Helper()
	builder := NewEditTransactionBuilder(doc)
	operation(builder)
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("commit failed: %s", failure.Name())
	}
	return commit
}

func TestSemanticValuePreservesDirectStyleAndFallsBackOnlyWhenRequired(t *testing.T) {
	direct := editReader(t, "a=one\n")
	directCommit := commitOne(t, direct, func(builder *EditTransactionBuilder) {
		builder.SemanticValue(direct.properties[0].node,
			NewJavaStringFromUnicode("two words"))
	})
	if string(directCommit.Document.Render()) != "a=two words\n" {
		t.Fatalf("render %q", string(directCommit.Document.Render()))
	}
	if len(directCommit.ChangeSet.Diagnostics()) != 0 {
		t.Fatalf("unexpected fallback diagnostic")
	}

	escaped := editReader(t, "a=one\\ value\n")
	fallbackCommit := commitOne(t, escaped, func(builder *EditTransactionBuilder) {
		builder.SemanticValue(escaped.properties[0].node,
			NewJavaStringFromUnicode("next value"))
	})
	if string(fallbackCommit.Document.Render()) != "a=next value\n" {
		t.Fatalf("render %q", string(fallbackCommit.Document.Render()))
	}
	diagnostics := fallbackCommit.ChangeSet.Diagnostics()
	if len(diagnostics) != 1 ||
		diagnostics[0].Code != "java-properties.edit.canonical-fallback@1" {
		t.Fatalf("fallback diagnostics %+v", diagnostics)
	}
}

func TestSemanticValuePreservesExactUnpairedJavaUnitsWithUnicodeEscapes(t *testing.T) {
	doc := editReader(t, "a=x\n")
	exact := NewJavaStringFromCodeUnits([]uint16{0xD800})
	commit := commitOne(t, doc, func(builder *EditTransactionBuilder) {
		builder.SemanticValue(doc.properties[0].node, exact)
	})
	if string(commit.Document.Render()) != "a=\\uD800\n" {
		t.Fatalf("render %q", string(commit.Document.Render()))
	}
	if !commit.Document.properties[0].value.Equal(exact) {
		t.Fatalf("value not preserved")
	}
}

func TestLiteralValueRequiresOneExactValueOwnershipInterval(t *testing.T) {
	doc := editReader(t, "a=one\nb=two\n")
	commit := commitOne(t, doc, func(builder *EditTransactionBuilder) {
		builder.LiteralValue(doc.properties[0].node, []byte("raw\\ value"))
	})
	if string(commit.Document.Render()) != "a=raw\\ value\nb=two\n" {
		t.Fatalf("render %q", string(commit.Document.Render()))
	}
	value, _ := commit.Document.properties[0].value.ToUnicode()
	if value != "raw value" {
		t.Fatalf("value %q", value)
	}
	for _, invalid := range [][]byte{[]byte(" leading"), []byte("line\nbreak"), []byte("tail\\")} {
		builder := NewEditTransactionBuilder(doc)
		builder.LiteralValue(doc.properties[0].node, invalid)
		if _, failure := doc.Commit(builder.Build()); failure == nil ||
			failure.Kind != EditFailureInvalidLiteral {
			t.Fatalf("invalid literal %q accepted: %v", string(invalid), failure)
		}
	}
}

func TestInsertionsHonorAllPropertyRelativePlacementsAndDuplicates(t *testing.T) {
	source := "# head\na=1\n# middle\nb=2"
	cases := []struct {
		placement func(*Document) AssociationPlacement
		expected  string
	}{
		{func(*Document) AssociationPlacement { return PlacementStart() },
			"# head\nx=0\na=1\n# middle\nb=2"},
		{func(doc *Document) AssociationPlacement {
			return PlacementBefore(doc.properties[1].node)
		}, "# head\na=1\n# middle\nx=0\nb=2"},
		{func(doc *Document) AssociationPlacement {
			return PlacementAfter(doc.properties[0].node)
		}, "# head\na=1\nx=0\n# middle\nb=2"},
		{func(*Document) AssociationPlacement { return PlacementEnd() },
			"# head\na=1\n# middle\nb=2\nx=0\n"},
	}
	for _, vector := range cases {
		doc := editReader(t, source)
		commit := commitOne(t, doc, func(builder *EditTransactionBuilder) {
			builder.InsertProperty(doc.NodeRef(), NewJavaStringFromUnicode("x"),
				NewJavaStringFromUnicode("0"), vector.placement(doc))
		})
		if string(commit.Document.Render()) != vector.expected {
			t.Fatalf("render %q, want %q", string(commit.Document.Render()), vector.expected)
		}
	}

	duplicate := editReader(t, "a=1\na=2\n")
	commit := commitOne(t, duplicate, func(builder *EditTransactionBuilder) {
		builder.InsertProperty(duplicate.NodeRef(), NewJavaStringFromUnicode("a"),
			NewJavaStringFromUnicode("3"), PlacementEnd())
	})
	if len(commit.Document.properties) != 3 {
		t.Fatalf("properties %d", len(commit.Document.properties))
	}
	for _, property := range commit.Document.properties {
		key, _ := property.key.ToUnicode()
		if key != "a" {
			t.Fatalf("key %q", key)
		}
	}
}

func TestRemovalOwnsAllContinuationLinesButNotAdjacentComments(t *testing.T) {
	doc := editReader(t, "# before\nkey=first\\\n  second\n# after\nnext=v\n")
	commit := commitOne(t, doc, func(builder *EditTransactionBuilder) {
		builder.RemoveProperty(doc.properties[0].node)
	})
	if string(commit.Document.Render()) != "# before\n# after\nnext=v\n" {
		t.Fatalf("render %q", string(commit.Document.Render()))
	}
	if len(commit.Document.comments) != 2 || len(commit.Document.properties) != 1 {
		t.Fatalf("counts")
	}
}

func TestRenameReplacesTheCompleteContinuedKeyOwnership(t *testing.T) {
	doc := editReader(t, "old\\\n key=value\n")
	commit := commitOne(t, doc, func(builder *EditTransactionBuilder) {
		builder.RenameProperty(doc.properties[0].node, NewJavaStringFromUnicode("new key"))
	})
	if string(commit.Document.Render()) != "new\\ key=value\n" {
		t.Fatalf("render %q", string(commit.Document.Render()))
	}
	key, _ := commit.Document.properties[0].key.ToUnicode()
	if key != "new key" {
		t.Fatalf("key %q", key)
	}
}

func TestTransactionConflictsFailBeforeAnyDocumentIsPublished(t *testing.T) {
	doc := editReader(t, "a=1\nb=2\n")
	first := doc.properties[0].node

	duplicate := NewEditTransactionBuilder(doc)
	duplicate.SemanticValue(first, NewJavaStringFromUnicode("x"))
	duplicate.RenameProperty(first, NewJavaStringFromUnicode("renamed"))
	if _, failure := doc.Commit(duplicate.Build()); failure == nil ||
		failure.Kind != EditFailureDuplicateTarget {
		t.Fatalf("duplicate target accepted: %v", failure)
	}

	removedAnchor := NewEditTransactionBuilder(doc)
	removedAnchor.RemoveProperty(first)
	removedAnchor.InsertProperty(doc.NodeRef(), NewJavaStringFromUnicode("x"),
		NewJavaStringFromUnicode("0"), PlacementAfter(first))
	if _, failure := doc.Commit(removedAnchor.Build()); failure == nil ||
		failure.Kind != EditFailurePlacementAnchorRemoved {
		t.Fatalf("removed anchor accepted: %v", failure)
	}

	sharedBoundary := NewEditTransactionBuilder(doc)
	sharedBoundary.InsertProperty(doc.NodeRef(), NewJavaStringFromUnicode("x"),
		NewJavaStringFromUnicode("0"), PlacementStart())
	sharedBoundary.InsertProperty(doc.NodeRef(), NewJavaStringFromUnicode("y"),
		NewJavaStringFromUnicode("0"), PlacementBefore(first))
	if _, failure := doc.Commit(sharedBoundary.Build()); failure == nil ||
		failure.Kind != EditFailureOverlappingOwnership {
		t.Fatalf("shared boundary accepted: %v", failure)
	}
	if string(doc.Render()) != "a=1\nb=2\n" {
		t.Fatalf("base mutated")
	}
}

func TestSnapshotRoleRecoveryAndResourceContractsAreEnforced(t *testing.T) {
	doc := editReader(t, "a=1\n")
	other := editReader(t, "a=1\n")
	wrongSnapshot := NewEditTransactionBuilder(doc)
	wrongSnapshot.SemanticValue(other.properties[0].node, NewJavaStringFromUnicode("x"))
	if _, failure := doc.Commit(wrongSnapshot.Build()); failure == nil ||
		failure.Kind != EditFailureWrongSnapshot {
		t.Fatalf("wrong snapshot accepted: %v", failure)
	}
	wrongRole := NewEditTransactionBuilder(doc)
	wrongRole.SemanticValue(doc.NodeRef(), NewJavaStringFromUnicode("x"))
	if _, failure := doc.Commit(wrongRole.Build()); failure == nil ||
		failure.Kind != EditFailureWrongRole {
		t.Fatalf("wrong role accepted: %v", failure)
	}
	recovered := editReader(t, "bad=\\u12G4\n")
	transaction := NewEditTransactionBuilder(recovered).Build()
	if _, failure := recovered.Commit(transaction); failure == nil ||
		failure.Kind != EditFailureRecoveredDocument {
		t.Fatalf("recovered accepted: %v", failure)
	}
	if _, failure := recovered.Commit(transaction); failure != nil &&
		failure.Code() != "core.edit.incomplete-target@1" {
		t.Fatalf("recovered code %s", failure.Code())
	}
}

func TestSelectedEncodingIsPreservedAndUnrepresentableReaderTextFails(t *testing.T) {
	var utf16 []byte
	utf16 = append(utf16, 0xFF, 0xFE)
	for _, unit := range utf16Units("a=one\r\n") {
		utf16 = append(utf16, byte(unit), byte(unit>>8))
	}
	doc, failure := ParseReader(utf16, document.Utf16LeEncoding(), DefaultPropertiesParseLimits())
	if failure != nil {
		t.Fatalf("formation failed: %s", failure.Code())
	}
	commit := commitOne(t, doc, func(builder *EditTransactionBuilder) {
		builder.SemanticValue(doc.properties[0].node, NewJavaStringFromUnicode("新"))
	})
	if !commit.Document.source.EncodingFacts().Equal(doc.source.EncodingFacts()) {
		t.Fatalf("encoding facts changed")
	}
	value, _ := commit.Document.properties[0].value.ToUnicode()
	if value != "新" {
		t.Fatalf("value %q", value)
	}

	cpPage, _ := document.WindowsCodePageFromNumber(1252)
	windows, failure := ParseReader([]byte("a=x\n"),
		document.WindowsCodePageEncoding(cpPage), DefaultPropertiesParseLimits())
	if failure != nil {
		t.Fatalf("formation failed: %s", failure.Code())
	}
	builder := NewEditTransactionBuilder(windows)
	builder.SemanticValue(windows.properties[0].node, NewJavaStringFromUnicode("中"))
	if _, failure := windows.Commit(builder.Build()); failure == nil ||
		failure.Kind != EditFailureEncodingUnrepresentable {
		t.Fatalf("unrepresentable accepted: %v", failure)
	}
}

func utf16Units(text string) []uint16 {
	var units []uint16
	for _, character := range text {
		if character > 0xFFFF {
			value := character - 0x10000
			units = append(units, uint16(0xD800+(value>>10)), uint16(0xDC00+(value&0x3FF)))
			continue
		}
		units = append(units, uint16(character))
	}
	return units
}

func TestPatchProofAndDryRunCloseOverTheExactCommittedBytes(t *testing.T) {
	doc := editReader(t, "a=one\nb=two\n")
	builder := NewEditTransactionBuilder(doc)
	builder.RenameProperty(doc.properties[0].node, NewJavaStringFromUnicode("first"))
	builder.SemanticValue(doc.properties[1].node, NewJavaStringFromUnicode("changed"))
	transaction := builder.Build()
	commit, failure := doc.Commit(transaction)
	if failure != nil {
		t.Fatalf("commit failed: %s", failure.Name())
	}
	replayed, replayErr := commit.SourcePatch.Apply(doc.Source(),
		document.DefaultSourcePatchLimits())
	if replayErr != nil {
		t.Fatalf("patch replay failed: %s", replayErr.Error())
	}
	if string(replayed.Bytes()) != string(commit.Document.Render()) {
		t.Fatalf("replay mismatch")
	}
	if proofErr := commit.UntouchedProof.Verify(doc.Source(), commit.Document.Source(),
		commit.SourcePatch.Replacements()); proofErr != nil {
		t.Fatalf("proof failed: %s", proofErr.Error())
	}
	plan, planFailure := doc.DryRun(transaction, "fixture.properties")
	if planFailure != nil {
		t.Fatalf("dry run failed: %s", planFailure.Name())
	}
	if len(plan.Operations()) != 2 {
		t.Fatalf("plan operations %d", len(plan.Operations()))
	}
	if plan.Operations()[0].Operation.ID() != "java-properties.edit.rename-property" {
		t.Fatalf("plan op %s", plan.Operations()[0].Operation.ID())
	}
}

func TestEmptyTransactionIsAVerifiedIdentityTransition(t *testing.T) {
	doc := editReader(t, "a=1\n")
	transaction := NewEditTransactionBuilder(doc).Build()
	commit, failure := doc.Commit(transaction)
	if failure != nil {
		t.Fatalf("commit failed: %s", failure.Name())
	}
	if string(commit.Document.Render()) != string(doc.Render()) {
		t.Fatalf("render changed")
	}
	if len(commit.SourcePatch.Replacements()) != 0 ||
		len(commit.ChangeSet.SourceEdits()) != 0 {
		t.Fatalf("identity edit produced edits")
	}
}

func TestOperationRegistryPublishesTheFrozenFiveOperationSurface(t *testing.T) {
	expected := []string{
		"java-properties.edit.insert-property@1",
		"java-properties.edit.remove-property@1",
		"java-properties.edit.rename-property@1",
		"java-properties.edit.replace-literal-value@1",
		"java-properties.edit.replace-semantic-value@1",
	}
	for _, profile := range []PropertiesProfile{PropertiesReaderV1, PropertiesLatin1V1} {
		registry := NewFormatOperationRegistry(profile)
		operations := make([]string, 0, len(registry.Operations()))
		supported := 0
		for _, descriptor := range registry.Operations() {
			operations = append(operations,
				descriptor.ID.ID()+"@"+uint64String(uint64(descriptor.ID.Version())))
			if descriptor.Support == OperationSupportSupported {
				supported++
			}
		}
		if len(operations) != 5 || supported != 5 {
			t.Fatalf("registry %v %d", operations, supported)
		}
		for index := range expected {
			if operations[index] != expected[index] {
				t.Fatalf("registry order %v", operations)
			}
		}
	}
}
