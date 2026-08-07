package properties

import (
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
)

func projectReader(t *testing.T, source string) *Document {
	t.Helper()
	return queryReader(t, source)
}

func TestExactProjectionPreservesDuplicatesAndFragmentedOrigins(t *testing.T) {
	doc := projectReader(t, "a\\ key=one\\\n two\\u0021\na\\ key=last\n")
	result := doc.Project(BestExactEntryMapping())
	if result.Complete == nil {
		t.Fatalf("exact projection failed")
	}
	if result.Complete.Fidelity != FidelityExact {
		t.Fatalf("fidelity %s", result.Complete.Fidelity)
	}
	if len(result.Complete.Report.Events()) != 0 {
		t.Fatalf("events %d", len(result.Complete.Report.Events()))
	}
	mapping := result.Complete.Value.(*core.EntryMapping)
	if mapping.Len() != 2 {
		t.Fatalf("entries %d", mapping.Len())
	}
	first := mapping.Entries()[0]
	if string(first.Key.(core.String)) != "a key" ||
		string(first.Value.(core.String)) != "onetwo!" {
		t.Fatalf("first entry")
	}
	if !vectorRelationPresent(result.Complete, ProvenanceEscapeDerived) {
		t.Fatalf("escape provenance missing")
	}
	twoFragments := false
	for _, entry := range result.Complete.Provenance.Entries() {
		count := 0
		for _, origin := range entry.Origins {
			if origin.Relation == ProvenanceValueFragment {
				count++
			}
		}
		if count == 2 {
			twoFragments = true
		}
	}
	if !twoFragments {
		t.Fatalf("two value fragments missing")
	}
}

func TestObjectProjectionRequiresOrExplicitlyCollapsesDuplicates(t *testing.T) {
	doc := projectReader(t, "a=first\nb=middle\na=last\n")
	unique := doc.Project(RequireObject(DuplicatePolicyRequireUnique))
	if unique.Complete != nil {
		t.Fatalf("unique accepted duplicates")
	}
	if unique.Failed.Diagnostics[0].Code != "core.projection.target-not-applicable@1" {
		t.Fatalf("code %s", unique.Failed.Diagnostics[0].Code)
	}
	first := doc.Project(RequireObject(DuplicatePolicyFirstWins))
	if first.Complete == nil || first.Complete.Fidelity != FidelityLossy {
		t.Fatalf("first wins")
	}
	events := first.Complete.Report.Events()
	if len(events) != 1 || events[0].Code != "java-properties.projection.duplicate-collapsed@1" ||
		events[0].Impact != FidelityLossy {
		t.Fatalf("events %+v", events)
	}
	firstObject := first.Complete.Value.(*core.Object)
	firstEntries := firstObject.Entries()
	if len(firstEntries) != 2 || firstEntries[0].Key != "a" ||
		string(firstEntries[0].Value.(core.String)) != "first" {
		t.Fatalf("first entries")
	}
	if !vectorRelationPresent(first.Complete, ProvenanceCollapsed) {
		t.Fatalf("collapsed provenance missing")
	}
	last := doc.Project(RequireObject(DuplicatePolicyLastWinsJdkTable))
	if last.Complete == nil {
		t.Fatalf("last wins")
	}
	lastObject := last.Complete.Value.(*core.Object)
	lastEntries := lastObject.Entries()
	if len(lastEntries) != 2 || lastEntries[0].Key != "b" ||
		lastEntries[1].Key != "a" ||
		string(lastEntries[1].Value.(core.String)) != "last" {
		t.Fatalf("last entries")
	}
}

func TestUnpairedSurrogatesAndRecoveryFailAtomically(t *testing.T) {
	unpaired := projectReader(t, "a=ok\nb=\\uD800")
	unpairedResult := unpaired.Project(BestExactEntryMapping())
	if unpairedResult.Complete != nil {
		t.Fatalf("unpaired completed")
	}
	diagnostic := unpairedResult.Failed.Diagnostics[0]
	if diagnostic.Code != "java-properties.projection.unpaired-surrogate@1" {
		t.Fatalf("code %s", diagnostic.Code)
	}
	if diagnostic.Arguments["component"] != "value" ||
		diagnostic.Arguments["reason"] != "unpaired-surrogate" {
		t.Fatalf("arguments %v", diagnostic.Arguments)
	}
	if diagnostic.Primary == nil || diagnostic.Primary.StartByte != 5 {
		t.Fatalf("primary %+v", diagnostic.Primary)
	}
	if len(unpairedResult.Failed.Report.Events()) != 0 {
		t.Fatalf("partial report")
	}

	recovered := projectReader(t, "good=ok\nbad=\\u12G4")
	recoveredResult := recovered.Project(BestExactEntryMapping())
	if recoveredResult.Complete != nil {
		t.Fatalf("recovered completed")
	}
	if recoveredResult.Failed.Diagnostics[0].Code !=
		"java-properties.projection.incomplete-document@1" {
		t.Fatalf("code %s", recoveredResult.Failed.Diagnostics[0].Code)
	}
}

func TestEveryProjectionLimitFailsWithoutPartialOutput(t *testing.T) {
	complete := projectReader(t, "a=1\n")
	for _, limits := range []ProjectionLimits{
		{MaxSourceAssociations: 0, MaxValueNodes: 4_000_001,
			MaxReportEntries: 100_000, MaxProvenanceUnits: 8_000_000},
		{MaxSourceAssociations: 2_000_000, MaxValueNodes: 1,
			MaxReportEntries: 100_000, MaxProvenanceUnits: 8_000_000},
		{MaxSourceAssociations: 2_000_000, MaxValueNodes: 4_000_001,
			MaxReportEntries: 100_000, MaxProvenanceUnits: 1},
	} {
		result := complete.Project(BestExactEntryMapping().WithLimits(limits))
		if result.Complete != nil {
			t.Fatalf("limit accepted")
		}
		if result.Failed.Diagnostics[0].Code != "core.projection.resource-limit@1" {
			t.Fatalf("code %s", result.Failed.Diagnostics[0].Code)
		}
		if len(result.Failed.Report.Events()) != 0 {
			t.Fatalf("partial report")
		}
	}
	duplicate := projectReader(t, "a=1\na=2\n")
	reportLimits := DefaultProjectionLimits()
	reportLimits.MaxReportEntries = 0
	result := duplicate.Project(RequireObject(DuplicatePolicyFirstWins).WithLimits(reportLimits))
	if result.Complete != nil {
		t.Fatalf("report limit accepted")
	}
	if result.Failed.Diagnostics[0].Code != "core.projection.resource-limit@1" {
		t.Fatalf("code %s", result.Failed.Diagnostics[0].Code)
	}
}

func TestEmptyKeysAndValuesHaveExactZeroWidthProvenanceAnchors(t *testing.T) {
	doc := projectReader(t, "=x\nempty=\nimplicit\n")
	result := doc.Project(BestExactEntryMapping())
	if result.Complete == nil {
		t.Fatalf("projection failed")
	}
	if len(result.Complete.Provenance.Entries()) != 10 {
		t.Fatalf("provenance entries %d", len(result.Complete.Provenance.Entries()))
	}
	emptyOrigins := 0
	for _, entry := range result.Complete.Provenance.Entries() {
		for _, origin := range entry.Origins {
			if origin.Span.IsEmpty() && (origin.Relation == ProvenanceKeyFragment ||
				origin.Relation == ProvenanceValueFragment) {
				emptyOrigins++
			}
		}
	}
	if emptyOrigins != 3 {
		t.Fatalf("empty origins %d", emptyOrigins)
	}
}

func TestProjectionFactsAreDeterministicAcrossRuns(t *testing.T) {
	doc := projectReader(t, "a=first\nb=middle\na=last\n")
	firstRun := doc.Project(BestExactEntryMapping())
	secondRun := doc.Project(BestExactEntryMapping())
	if !core.Equal(firstRun.Complete.Value, secondRun.Complete.Value) {
		t.Fatalf("values differ across runs")
	}
	firstProvenance := firstRun.Complete.Provenance.Entries()
	secondProvenance := secondRun.Complete.Provenance.Entries()
	if len(firstProvenance) != len(secondProvenance) {
		t.Fatalf("provenance differs across runs")
	}
	for index := range firstProvenance {
		if firstProvenance[index].Projected.Kind != secondProvenance[index].Projected.Kind {
			t.Fatalf("provenance order differs across runs")
		}
	}
}

func TestDocumentResolversRejectWrongRoles(t *testing.T) {
	doc := projectReader(t, "a=\\t\n")
	property := doc.properties[0]
	if _, err := doc.NaturalLine(property.node); err == nil {
		t.Fatalf("wrong role accepted")
	}
	resolved, err := doc.Property(property.node)
	if err != nil || resolved.node != property.node {
		t.Fatalf("property resolver")
	}
	escape := doc.escapes[0]
	resolvedEscape, err := doc.Escape(escape.node)
	if err != nil || resolvedEscape.node != escape.node {
		t.Fatalf("escape resolver")
	}
	if resolvedEscape.InKey() || resolvedEscape.Kind() != EscapeKindNamed ||
		resolvedEscape.OutputStart() != 0 || resolvedEscape.OutputEnd() != 1 {
		t.Fatalf("escape facts")
	}
	logical := doc.logicalLines[0]
	resolvedLogical, err := doc.LogicalLine(logical.node)
	if err != nil || resolvedLogical.kind != LogicalLineProperty ||
		len(resolvedLogical.naturalLines) != 1 {
		t.Fatalf("logical line resolver")
	}
	natural := doc.naturalLines[0]
	resolvedNatural, err := doc.NaturalLine(natural.node)
	if err != nil || resolvedNatural.node != natural.node {
		t.Fatalf("natural line resolver")
	}
	if _, err := doc.Property(doc.NodeRef()); err == nil {
		t.Fatalf("document role accepted as property")
	}
	_ = document.FormationStatusComplete
}
