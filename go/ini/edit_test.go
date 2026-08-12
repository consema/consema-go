package ini

import (
	"testing"

	"consema.dev/consema/document"
)

func TestPortableSemanticEditIsAtomicAndReplayable(t *testing.T) {
	doc := parseText(t, PortableV1, "; before\n[s]\nk=old\n; after\n")
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticValue(doc.Entries()[0].NodeRef(), "new value",
		RepresentationPolicyCanonicalForProfile)
	transaction := builder.Build()
	plan, failure := doc.DryRun(transaction, "memory:portable")
	if failure != nil {
		t.Fatalf("dry run failed: %s", failure.Code())
	}
	commit, failure := doc.Commit(transaction)
	if failure != nil {
		t.Fatalf("commit failed: %s", failure.Code())
	}
	if string(commit.Document.Render()) != "; before\n[s]\nk=new value\n; after\n" {
		t.Fatalf("edit render differed: %q", commit.Document.Render())
	}
	replayed, err := commit.SourcePatch.Apply(doc.Source(), document.DefaultSourcePatchLimits())
	if err != nil {
		t.Fatalf("patch replay failed: %v", err)
	}
	if string(replayed.Bytes()) != string(commit.Document.Render()) {
		t.Fatalf("patch replay bytes differed")
	}
	if err := commit.UntouchedProof.Verify(doc.Source(), commit.Document.Source(),
		commit.SourcePatch.Replacements()); err != nil {
		t.Fatalf("proof verification failed: %v", err)
	}
	if plan.SourceID() != "memory:portable" {
		t.Fatalf("plan source id differed")
	}
}

func TestWindowsPreservesQuotesAndFallsBackForUnquotedWhitespace(t *testing.T) {
	doc := parseText(t, WindowsV1, "[S]\r\na='old'\r\nb=plain\r\n")
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticValue(doc.Entries()[0].NodeRef(), " new ",
		RepresentationPolicyPreserveCompatible)
	builder.SemanticValue(doc.Entries()[1].NodeRef(), " spaced ",
		RepresentationPolicyPreserveElseCanonical)
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("commit failed: %s", failure.Code())
	}
	if string(commit.Document.Render()) != "[S]\r\na=' new '\r\nb=\" spaced \"\r\n" {
		t.Fatalf("windows edit render differed: %q", commit.Document.Render())
	}
	diagnostics := commit.ChangeSet.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != "ini.edit.canonical-fallback@1" {
		t.Fatalf("canonical fallback diagnostic differed")
	}
}

func TestPythonPreservesMultilineTriviaAndCanonicalizesShapeChanges(t *testing.T) {
	source := "[S]\nkey : first  \n\tsecond\t\n\n\tthird\nnext=x\n"
	doc := parseText(t, PythonConfigParserV1, source)
	preserve := NewEditTransactionBuilder(doc)
	preserve.SemanticValue(doc.Entries()[0].NodeRef(), "one\ntwo\n\nthree",
		RepresentationPolicyPreserveCompatible)
	commit, failure := doc.Commit(preserve.Build())
	if failure != nil {
		t.Fatalf("preserve commit failed: %s", failure.Code())
	}
	if string(commit.Document.Render()) != "[S]\nkey : one  \n\ttwo\t\n\n\tthree\nnext=x\n" {
		t.Fatalf("preserved multiline render differed: %q", commit.Document.Render())
	}
	if commit.Document.Entries()[0].Value() != "one\ntwo\n\nthree" {
		t.Fatalf("preserved multiline value differed")
	}

	fallback := NewEditTransactionBuilder(doc)
	fallback.SemanticValue(doc.Entries()[0].NodeRef(), "single",
		RepresentationPolicyPreserveElseCanonical)
	commit, failure = doc.Commit(fallback.Build())
	if failure != nil {
		t.Fatalf("fallback commit failed: %s", failure.Code())
	}
	if commit.Document.Entries()[0].Value() != "single" ||
		len(commit.ChangeSet.Diagnostics()) != 1 {
		t.Fatalf("fallback facts differed")
	}
}

func TestLiteralAndSnapshotFailuresAreExplicit(t *testing.T) {
	doc := parseText(t, PortableV1, "[s]\nk=old\n")
	other := parseText(t, PortableV1, "[s]\nk=x\n")
	wrong := NewEditTransactionBuilder(doc)
	wrong.LiteralValue(other.Entries()[0].NodeRef(), []byte("new"))
	if _, failure := doc.Commit(wrong.Build()); failure == nil ||
		failure.Code() != "core.edit.wrong-snapshot@1" {
		t.Fatalf("wrong snapshot code differed: %v", failure)
	}

	invalid := NewEditTransactionBuilder(doc)
	invalid.LiteralValue(doc.Entries()[0].NodeRef(), []byte("x\n[y]\nq=z"))
	if _, failure := doc.Commit(invalid.Build()); failure == nil ||
		failure.Code() != "core.edit.invalid-literal@1" {
		t.Fatalf("invalid literal code differed: %v", failure)
	}

	recovered := parseText(t, PortableV1, "[s]\nbroken\n")
	transaction := NewEditTransactionBuilder(recovered).Build()
	if _, failure := recovered.Commit(transaction); failure == nil ||
		failure.Code() != "core.edit.incomplete-target@1" {
		t.Fatalf("recovered edit code differed: %v", failure)
	}
}

func TestEveryEditFailureMapsToAFrozenCode(t *testing.T) {
	cases := []struct {
		failure EditFailure
		code    string
	}{
		{EditFailure{Kind: EditFailureRecoveredDocument}, "core.edit.incomplete-target@1"},
		{EditFailure{Kind: EditFailureWrongSnapshot}, "core.edit.wrong-snapshot@1"},
		{EditFailure{Kind: EditFailureWrongRole}, "core.edit.wrong-role@1"},
		{EditFailure{Kind: EditFailureDuplicateTarget}, "core.edit.conflicting-edits@1"},
		{EditFailure{Kind: EditFailureOverlappingOwnership}, "core.edit.conflicting-edits@1"},
		{EditFailure{Kind: EditFailureAncestorDescendantConflict}, "core.edit.conflicting-edits@1"},
		{EditFailure{Kind: EditFailurePlacementAnchorRemoved}, "core.edit.conflicting-edits@1"},
		{EditFailure{Kind: EditFailureTargetNotFound}, "core.edit.target-not-found@1"},
		{EditFailure{Kind: EditFailureInvalidPlacement}, "ini.edit.invalid-placement@1"},
		{EditFailure{Kind: EditFailureInvalidName}, "ini.edit.invalid-name@1"},
		{EditFailure{Kind: EditFailureNameCollision}, "core.edit.duplicate-key@1"},
		{EditFailure{Kind: EditFailureInvalidKey}, "ini.edit.invalid-name@1"},
		{EditFailure{Kind: EditFailureDuplicateKey}, "core.edit.duplicate-key@1"},
		{EditFailure{Kind: EditFailureKeyCollision}, "ini.edit.case-collision@1"},
		{EditFailure{Kind: EditFailureRepresentationIncompatible},
			"core.edit.representation-incompatible@1"},
		{EditFailure{Kind: EditFailureExactLiteralRequiresLiteralOperation},
			"core.edit.exact-literal-requires-literal@1"},
		{EditFailure{Kind: EditFailureUnrepresentableValue}, "core.edit.unsupported-value@1"},
		{EditFailure{Kind: EditFailureEncodingUnrepresentable},
			"core.edit.representation-incompatible@1"},
		{EditFailure{Kind: EditFailureInvalidLiteral}, "core.edit.invalid-literal@1"},
		{EditFailure{Kind: EditFailureResourceLimit, LimitName: "test"},
			"core.edit.resource-limit@1"},
		{EditFailure{Kind: EditFailureNewDocumentFormationFailed},
			"core.edit.formation-failed@1"},
	}
	for _, item := range cases {
		if item.failure.Code() != item.code {
			t.Fatalf("%s: code %s, want %s", item.failure.Name(), item.failure.Code(), item.code)
		}
	}
}

func TestPolicyConflictsAndDuplicateTargetsFailBeforeAPatchExists(t *testing.T) {
	doc := parseText(t, WindowsV1, "[S]\r\nk=plain\r\n")
	target := doc.Entries()[0].NodeRef()
	incompatible := NewEditTransactionBuilder(doc)
	incompatible.SemanticValue(target, " spaced ", RepresentationPolicyPreserveCompatible)
	if _, failure := doc.Commit(incompatible.Build()); failure == nil ||
		failure.Kind != EditFailureRepresentationIncompatible {
		t.Fatalf("incompatible representation must fail")
	}
	exact := NewEditTransactionBuilder(doc)
	exact.SemanticValue(target, "value", RepresentationPolicyExactLiteral)
	if _, failure := doc.Commit(exact.Build()); failure == nil ||
		failure.Kind != EditFailureExactLiteralRequiresLiteralOperation {
		t.Fatalf("exact literal policy must fail")
	}
	duplicate := NewEditTransactionBuilder(doc)
	duplicate.SemanticValue(target, "one", RepresentationPolicyCanonicalForProfile)
	duplicate.LiteralValue(target, []byte("two"))
	if _, failure := doc.Commit(duplicate.Build()); failure == nil ||
		failure.Kind != EditFailureDuplicateTarget {
		t.Fatalf("duplicate target must fail")
	}
}

func TestPythonFirstLineCommentMarkersRemainLiteralContent(t *testing.T) {
	doc := parseText(t, PythonConfigParserV1, "[S]\nk=old\n")
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticValue(doc.Entries()[0].NodeRef(), "#literal ;literal",
		RepresentationPolicyCanonicalForProfile)
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("commit failed: %s", failure.Code())
	}
	if commit.Document.Entries()[0].Value() != "#literal ;literal" {
		t.Fatalf("comment markers must stay content")
	}
}

func TestSelectedUtf16AndCodePageEncodingsArePreserved(t *testing.T) {
	text := "[S]\r\nk=old\r\n"
	source := utf16leBOM(text)
	doc, failure := Parse(source, WindowsV1, IniEncodingProfileDefault(),
		DefaultIniParseLimits())
	if failure != nil {
		t.Fatalf("parse failed: %s", failure.Diagnostics()[0].Code)
	}
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticValue(doc.Entries()[0].NodeRef(), "wide",
		RepresentationPolicyCanonicalForProfile)
	commit, editFailure := doc.Commit(builder.Build())
	if editFailure != nil {
		t.Fatalf("utf16 edit failed: %s", editFailure.Code())
	}
	if commit.Document.Entries()[0].Value() != "wide" {
		t.Fatalf("utf16 value differed")
	}
	if !commit.Document.Source().EncodingFacts().Equal(doc.Source().EncodingFacts()) {
		t.Fatalf("utf16 encoding facts changed")
	}

	page, _ := document.WindowsCodePageFromNumber(1252)
	doc, failure = Parse([]byte("[S]\r\nk=old\r\n"), WindowsV1,
		IniEncodingExplicit(document.WindowsCodePageEncoding(page)), DefaultIniParseLimits())
	if failure != nil {
		t.Fatalf("code-page parse failed: %s", failure.Diagnostics()[0].Code)
	}
	builder = NewEditTransactionBuilder(doc)
	builder.SemanticValue(doc.Entries()[0].NodeRef(), "€",
		RepresentationPolicyCanonicalForProfile)
	commit, editFailure = doc.Commit(builder.Build())
	if editFailure != nil {
		t.Fatalf("code-page edit failed: %s", editFailure.Code())
	}
	if commit.Document.Entries()[0].Value() != "€" || !containsByte(commit.Document.Render(), 0x80) {
		t.Fatalf("code-page edit facts differed")
	}
}

func TestSectionInsertRenameAndRemoveHaveExactOwnership(t *testing.T) {
	source := "[one]\na=1\n; independent\n[two]\nb=2\n"
	doc := parseText(t, PortableV1, source)
	insert := NewEditTransactionBuilder(doc)
	insert.InsertSection(doc.NodeRef(), "middle", PlacementAfter(doc.Sections()[0].NodeRef()))
	commit, failure := doc.Commit(insert.Build())
	if failure != nil {
		t.Fatalf("insert failed: %s", failure.Code())
	}
	if string(commit.Document.Render()) != "[one]\na=1\n; independent\n[middle]\n[two]\nb=2\n" {
		t.Fatalf("insert render differed: %q", commit.Document.Render())
	}

	rename := NewEditTransactionBuilder(doc)
	rename.RenameSection(doc.Sections()[1].NodeRef(), "renamed")
	commit, failure = doc.Commit(rename.Build())
	if failure != nil {
		t.Fatalf("rename failed: %s", failure.Code())
	}
	if string(commit.Document.Render()) != "[one]\na=1\n; independent\n[renamed]\nb=2\n" {
		t.Fatalf("rename render differed: %q", commit.Document.Render())
	}
	if commit.ChangeSet.NodeMappings()[0].Status != NodeMappingReplaced {
		t.Fatalf("rename mapping status differed")
	}

	remove := NewEditTransactionBuilder(doc)
	remove.RemoveSection(doc.Sections()[0].NodeRef())
	commit, failure = doc.Commit(remove.Build())
	if failure != nil {
		t.Fatalf("remove failed: %s", failure.Code())
	}
	if string(commit.Document.Render()) != "; independent\n[two]\nb=2\n" {
		t.Fatalf("remove render differed: %q", commit.Document.Render())
	}
	mappings := commit.ChangeSet.NodeMappings()
	if len(mappings) != 2 {
		t.Fatalf("remove mapping count %d, want 2", len(mappings))
	}
	for _, mapping := range mappings {
		if mapping.Status != NodeMappingDeleted {
			t.Fatalf("remove mapping status differed")
		}
	}
	if err := commit.UntouchedProof.Verify(doc.Source(), commit.Document.Source(),
		commit.SourcePatch.Replacements()); err != nil {
		t.Fatalf("proof verification failed: %v", err)
	}
}

func TestSectionRemovalOwnsPythonContinuationsButNotComments(t *testing.T) {
	doc := parseText(t, PythonConfigParserV1,
		"[one]\nk=first\n  second\n\n  fourth\n# keep\n[two]\nx=y\n")
	builder := NewEditTransactionBuilder(doc)
	builder.RemoveSection(doc.Sections()[0].NodeRef())
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("remove failed: %s", failure.Code())
	}
	if string(commit.Document.Render()) != "# keep\n[two]\nx=y\n" {
		t.Fatalf("python section removal render differed: %q", commit.Document.Render())
	}
	if len(commit.Document.Entries()) != 1 {
		t.Fatalf("entry count differed")
	}
}

func TestAppendingAfterAnEofEntryIntroducesOneProfileNewline(t *testing.T) {
	doc := parseText(t, PortableV1, "[one]\na=1")
	builder := NewEditTransactionBuilder(doc)
	builder.InsertSection(doc.NodeRef(), "two", PlacementEnd())
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("append failed: %s", failure.Code())
	}
	if string(commit.Document.Render()) != "[one]\na=1\n[two]\n" {
		t.Fatalf("append render differed: %q", commit.Document.Render())
	}
}

func TestMultipleSectionInsertionsMapTheOldDocumentOnce(t *testing.T) {
	doc := parseText(t, PortableV1, "[one]\na=1\n")
	builder := NewEditTransactionBuilder(doc)
	builder.InsertSection(doc.NodeRef(), "zero", PlacementStart())
	builder.InsertSection(doc.NodeRef(), "last", PlacementEnd())
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("insert failed: %s", failure.Code())
	}
	if string(commit.Document.Render()) != "[zero]\n[one]\na=1\n[last]\n" {
		t.Fatalf("multi-insert render differed: %q", commit.Document.Render())
	}
	mappings := commit.ChangeSet.NodeMappings()
	if len(mappings) != 1 || mappings[0].Status != NodeMappingUnmapped {
		t.Fatalf("multi-insert mapping facts differed")
	}
}

func TestSectionDependenciesNamesAndCollisionsFailAtomically(t *testing.T) {
	doc := parseText(t, PortableV1, "[one]\na=1\n[two]\nb=2\n")
	first := doc.Sections()[0].NodeRef()
	conflict := NewEditTransactionBuilder(doc)
	conflict.RemoveSection(first)
	conflict.SemanticValue(doc.Entries()[0].NodeRef(), "new",
		RepresentationPolicyCanonicalForProfile)
	if _, failure := doc.Commit(conflict.Build()); failure == nil ||
		failure.Kind != EditFailureAncestorDescendantConflict {
		t.Fatalf("ancestor-descendant conflict must fail")
	}
	removedAnchor := NewEditTransactionBuilder(doc)
	removedAnchor.RemoveSection(first)
	removedAnchor.InsertSection(doc.NodeRef(), "three", PlacementAfter(first))
	if _, failure := doc.Commit(removedAnchor.Build()); failure == nil ||
		failure.Kind != EditFailurePlacementAnchorRemoved {
		t.Fatalf("removed anchor must fail")
	}
	invalid := NewEditTransactionBuilder(doc)
	invalid.RenameSection(first, "bad name")
	if _, failure := doc.Commit(invalid.Build()); failure == nil ||
		failure.Kind != EditFailureInvalidName {
		t.Fatalf("invalid name must fail")
	}
	collision := NewEditTransactionBuilder(doc)
	collision.RenameSection(first, "two")
	if _, failure := doc.Commit(collision.Build()); failure == nil ||
		failure.Kind != EditFailureNameCollision {
		t.Fatalf("name collision must fail")
	}
	samePosition := NewEditTransactionBuilder(doc)
	samePosition.InsertSection(doc.NodeRef(), "three", PlacementEnd())
	samePosition.InsertSection(doc.NodeRef(), "four", PlacementEnd())
	if _, failure := doc.Commit(samePosition.Build()); failure == nil ||
		failure.Kind != EditFailureOverlappingOwnership {
		t.Fatalf("same-position insertions must fail")
	}
	only := parseText(t, PortableV1, "[only]\nk=v\n")
	removeOnly := NewEditTransactionBuilder(only)
	removeOnly.RemoveSection(only.Sections()[0].NodeRef())
	if _, failure := only.Commit(removeOnly.Build()); failure == nil ||
		failure.Kind != EditFailureNewDocumentFormationFailed {
		t.Fatalf("removing the only section must fail")
	}
}

func TestEntryInsertRenameAndRemovePreserveUnownedComments(t *testing.T) {
	source := "[s]\na=1\n; independent\nc=3\n[next]\nx=y\n"
	doc := parseText(t, PortableV1, source)
	section := doc.Sections()[0].NodeRef()
	insert := NewEditTransactionBuilder(doc)
	insert.InsertEntry(section, "b", "2", PlacementAfter(doc.Entries()[0].NodeRef()))
	commit, failure := doc.Commit(insert.Build())
	if failure != nil {
		t.Fatalf("insert failed: %s", failure.Code())
	}
	if string(commit.Document.Render()) != "[s]\na=1\nb=2\n; independent\nc=3\n[next]\nx=y\n" {
		t.Fatalf("entry insert render differed: %q", commit.Document.Render())
	}

	rename := NewEditTransactionBuilder(doc)
	rename.RenameEntry(doc.Entries()[1].NodeRef(), "renamed")
	commit, failure = doc.Commit(rename.Build())
	if failure != nil {
		t.Fatalf("rename failed: %s", failure.Code())
	}
	if string(commit.Document.Render()) != "[s]\na=1\n; independent\nrenamed=3\n[next]\nx=y\n" {
		t.Fatalf("entry rename render differed: %q", commit.Document.Render())
	}

	remove := NewEditTransactionBuilder(doc)
	remove.RemoveEntry(doc.Entries()[0].NodeRef())
	commit, failure = doc.Commit(remove.Build())
	if failure != nil {
		t.Fatalf("remove failed: %s", failure.Code())
	}
	if string(commit.Document.Render()) != "[s]\n; independent\nc=3\n[next]\nx=y\n" {
		t.Fatalf("entry remove render differed: %q", commit.Document.Render())
	}
}

func TestInsertedValuesUseEachProfilesCanonicalEntryRepresentation(t *testing.T) {
	windows := parseText(t, WindowsV1, "[S]\r\na=1\r\n")
	builder := NewEditTransactionBuilder(windows)
	builder.InsertEntry(windows.Sections()[0].NodeRef(), "quoted", " spaced ",
		PlacementEnd())
	transaction := builder.Build()
	plan, failure := windows.DryRun(transaction, "memory:windows-entry")
	if failure != nil {
		t.Fatalf("dry run failed: %s", failure.Code())
	}
	_ = plan
	commit, failure := windows.Commit(transaction)
	if failure != nil {
		t.Fatalf("commit failed: %s", failure.Code())
	}
	if string(commit.Document.Render()) != "[S]\r\na=1\r\nquoted=\" spaced \"\r\n" {
		t.Fatalf("windows insert render differed: %q", commit.Document.Render())
	}
	if commit.Document.Entries()[1].Value() != " spaced " {
		t.Fatalf("windows inserted value differed")
	}

	python := parseText(t, PythonConfigParserV1, "[S]\na=1\n")
	builder = NewEditTransactionBuilder(python)
	builder.InsertEntry(python.Sections()[0].NodeRef(), "multi", "first\n\nthird",
		PlacementEnd())
	commit, failure = python.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("python insert failed: %s", failure.Code())
	}
	if string(commit.Document.Render()) != "[S]\na=1\nmulti = first\n\n    third\n" {
		t.Fatalf("python insert render differed: %q", commit.Document.Render())
	}
	if commit.Document.Entries()[1].Value() != "first\n\nthird" {
		t.Fatalf("python inserted value differed")
	}
}

func TestRemovingAPythonMultilineEntryOwnsItsContinuationsOnly(t *testing.T) {
	doc := parseText(t, PythonConfigParserV1,
		"[S]\nmulti=first\n  second\n\n  fourth\n# keep\nnext=value\n")
	builder := NewEditTransactionBuilder(doc)
	builder.RemoveEntry(doc.Entries()[0].NodeRef())
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("remove failed: %s", failure.Code())
	}
	if string(commit.Document.Render()) != "[S]\n# keep\nnext=value\n" {
		t.Fatalf("multiline removal render differed: %q", commit.Document.Render())
	}
}

func TestEntryKeysPlacementsAndDependenciesAreValidatedBeforeRendering(t *testing.T) {
	doc := parseText(t, PythonConfigParserV1, "[S]\nKey=1\nother=2\n[T]\nx=3\n")
	section := doc.Sections()[0].NodeRef()
	collision := NewEditTransactionBuilder(doc)
	collision.RenameEntry(doc.Entries()[1].NodeRef(), "KEY")
	if _, failure := doc.Commit(collision.Build()); failure == nil ||
		failure.Kind != EditFailureKeyCollision {
		t.Fatalf("key collision must fail")
	}
	invalid := NewEditTransactionBuilder(doc)
	invalid.InsertEntry(section, "bad:key", "v", PlacementEnd())
	if _, failure := doc.Commit(invalid.Build()); failure == nil ||
		failure.Kind != EditFailureInvalidKey {
		t.Fatalf("invalid key must fail")
	}
	crossSection := NewEditTransactionBuilder(doc)
	crossSection.InsertEntry(section, "new", "v", PlacementBefore(doc.Entries()[2].NodeRef()))
	if _, failure := doc.Commit(crossSection.Build()); failure == nil ||
		failure.Kind != EditFailureInvalidPlacement {
		t.Fatalf("cross-section placement must fail")
	}
	duplicate := NewEditTransactionBuilder(doc)
	duplicate.InsertEntry(section, "Key", "v", PlacementEnd())
	if _, failure := doc.Commit(duplicate.Build()); failure == nil ||
		failure.Kind != EditFailureDuplicateKey {
		t.Fatalf("duplicate key must fail")
	}
	removedAnchor := NewEditTransactionBuilder(doc)
	removedAnchor.RemoveEntry(doc.Entries()[0].NodeRef())
	removedAnchor.InsertEntry(section, "new", "v", PlacementAfter(doc.Entries()[0].NodeRef()))
	if _, failure := doc.Commit(removedAnchor.Build()); failure == nil ||
		failure.Kind != EditFailurePlacementAnchorRemoved {
		t.Fatalf("removed entry anchor must fail")
	}
	removedSection := NewEditTransactionBuilder(doc)
	removedSection.RemoveSection(section)
	removedSection.InsertEntry(section, "new", "v", PlacementEnd())
	if _, failure := doc.Commit(removedSection.Build()); failure == nil ||
		failure.Kind != EditFailureAncestorDescendantConflict {
		t.Fatalf("insert into removed section must fail")
	}
}

func TestWindowsEntryEditsKeepOrderedCaseEquivalentOccurrences(t *testing.T) {
	doc := parseText(t, WindowsV1, "[S]\r\nKey=1\r\nother=2\r\n")
	builder := NewEditTransactionBuilder(doc)
	builder.RenameEntry(doc.Entries()[1].NodeRef(), "KEY")
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("commit failed: %s", failure.Code())
	}
	entries := commit.Document.Entries()
	if entries[0].ComparisonKey() != "key" || entries[1].ComparisonKey() != "key" ||
		!sameGroup(entries[0].DuplicateGroup(), entries[1].DuplicateGroup()) {
		t.Fatalf("windows case-equivalent facts differed")
	}
}
