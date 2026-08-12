package ini

import (
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
)

func TestExactProjectionPreservesDuplicateSectionsAndEntries(t *testing.T) {
	doc := parseText(t, WindowsV1, "[Main]\r\nName=one\r\nname=two\r\n[main]\r\nOther=three\r\n")
	projected := doc.Project(BestExactEntryMappingV1())
	if projected.Complete == nil {
		t.Fatalf("exact projection failed: %s", projected.Failed.Diagnostics[0].Code)
	}
	result := projected.Complete
	if result.Fidelity != FidelityExact || len(result.Report.Events()) != 0 {
		t.Fatalf("fidelity or report facts differed")
	}
	sections, ok := result.Value.(*core.EntryMapping)
	if !ok || sections.Len() != 2 {
		t.Fatalf("outer mapping facts differed")
	}
	first := sections.Entries()[0]
	firstKey, _ := first.Key.(core.String)
	second := sections.Entries()[1]
	secondKey, _ := second.Key.(core.String)
	if string(firstKey) != "Main" || string(secondKey) != "main" {
		t.Fatalf("section keys differed")
	}
	entries, ok := first.Value.(*core.EntryMapping)
	if !ok || entries.Len() != 2 {
		t.Fatalf("inner mapping facts differed")
	}
	entryKeys := []string{}
	for _, entry := range entries.Entries() {
		key, _ := entry.Key.(core.String)
		entryKeys = append(entryKeys, string(key))
	}
	if entryKeys[0] != "Name" || entryKeys[1] != "name" {
		t.Fatalf("entry keys differed")
	}
	foundAssociation := false
	for _, item := range result.Provenance.Entries() {
		if item.Projected.Kind == "Association" {
			foundAssociation = true
			break
		}
	}
	if !foundAssociation {
		t.Fatalf("association provenance missing")
	}

	python := parseText(t, PythonConfigParserV1, "[DEFAULT]\nbase=1\n[s]\nvalue=2\n")
	projected = python.Project(BestExactEntryMappingV1())
	if projected.Complete == nil {
		t.Fatalf("python projection failed")
	}
	foundDefault := false
	for _, item := range projected.Complete.Provenance.Entries() {
		for _, origin := range item.Origins {
			if origin.Node.Role() == document.RoleIniDefaultSection {
				foundDefault = true
				break
			}
		}
	}
	if !foundDefault {
		t.Fatalf("default-section provenance missing")
	}
}

func TestObjectProjectionRejectsThenExplicitlyReportsProfileCollisions(t *testing.T) {
	doc := parseText(t, WindowsV1, "[Main]\r\nName=one\r\nname=two\r\n[main]\r\nOther=three\r\n")
	rejected := doc.Project(RequireObjectV1(ComparisonProfileEquivalent, CollisionPolicyReject))
	if rejected.Complete != nil {
		t.Fatalf("Reject policy unexpectedly completed")
	}
	if rejected.Failed.Diagnostics[0].Code != "ini.projection.collision@1" {
		t.Fatalf("collision code differed")
	}

	first := doc.Project(RequireObjectV1(ComparisonProfileEquivalent, CollisionPolicyFirst))
	if first.Complete == nil {
		t.Fatalf("First policy failed")
	}
	if first.Complete.Fidelity != FidelityTransformed ||
		len(first.Complete.Report.Events()) != 2 {
		t.Fatalf("First policy fidelity/events differed")
	}
	section, entryKey, entryValue, ok := objectTriplet(first.Complete.Value)
	if !ok || section != "Main" || entryKey != "Name" || entryValue != "one" {
		t.Fatalf("First policy content differed")
	}

	last := doc.Project(RequireObjectV1(ComparisonProfileEquivalent, CollisionPolicyLast))
	if last.Complete == nil {
		t.Fatalf("Last policy failed")
	}
	section, entryKey, entryValue, ok = objectTriplet(last.Complete.Value)
	if !ok || section != "main" || entryKey != "Other" || entryValue != "three" {
		t.Fatalf("Last policy content differed")
	}

	original := doc.Project(RequireObjectV1(ComparisonOriginalExact, CollisionPolicyReject))
	if original.Complete == nil || original.Complete.Fidelity != FidelityExact {
		t.Fatalf("case-distinct Object projection differed")
	}
	if original.Complete.Value.(*core.Object).Len() != 2 {
		t.Fatalf("case-distinct section count differed")
	}
}

func objectTriplet(value core.Value) (string, string, string, bool) {
	sections, ok := value.(*core.Object)
	if !ok || sections.Len() == 0 {
		return "", "", "", false
	}
	section := sections.Entries()[0]
	entries, ok := section.Value.(*core.Object)
	if !ok || entries.Len() == 0 {
		return "", "", "", false
	}
	entry := entries.Entries()[0]
	text, ok := entry.Value.(core.String)
	if !ok {
		return "", "", "", false
	}
	return section.Key, entry.Key, string(text), true
}

func TestContinuationAndQuoteOriginsAreDistinct(t *testing.T) {
	python := parseText(t, PythonConfigParserV1, "[s]\nkey = first\n  second\n")
	projected := python.Project(BestExactEntryMappingV1())
	if projected.Complete == nil {
		t.Fatalf("python projection failed")
	}
	if !relationPresent(projected.Complete.Provenance, RelationContinuationFragment) {
		t.Fatalf("continuation provenance missing")
	}

	windows := parseText(t, WindowsV1, "[s]\r\nk=\" value \"\r\n")
	projected = windows.Project(BestExactEntryMappingV1())
	if projected.Complete == nil {
		t.Fatalf("windows projection failed")
	}
	if !relationPresent(projected.Complete.Provenance, RelationQuoteDerived) {
		t.Fatalf("quote provenance missing")
	}
}

func relationPresent(provenance ProvenanceMap, relation ProvenanceRelation) bool {
	for _, entry := range provenance.Entries() {
		for _, origin := range entry.Origins {
			if origin.Relation == relation {
				return true
			}
		}
	}
	return false
}

func TestRecoveredAndEachProjectionLimitFailWithoutValues(t *testing.T) {
	recovered := parseText(t, PortableV1, "[s]\nbare\n")
	projected := recovered.Project(BestExactEntryMappingV1())
	if projected.Complete != nil {
		t.Fatalf("recovered projection must fail")
	}
	diagnostic := projected.Failed.Diagnostics[0]
	if diagnostic.Code != "ini.projection.incomplete-document@1" ||
		diagnostic.Arguments["reason"] != "incomplete-document" {
		t.Fatalf("recovered projection code/args differed")
	}

	complete := parseText(t, PortableV1, "[s]\na=1\n")
	limitCases := []ProjectionLimits{
		{MaxSourceAssociations: 1, MaxValueNodes: 2_000_000, MaxReportEntries: 100_000,
			MaxProvenanceUnits: 4_000_000},
		{MaxSourceAssociations: 2_000_000, MaxValueNodes: 1, MaxReportEntries: 100_000,
			MaxProvenanceUnits: 4_000_000},
		{MaxSourceAssociations: 2_000_000, MaxValueNodes: 2_000_000, MaxReportEntries: 100_000,
			MaxProvenanceUnits: 1},
	}
	for _, limits := range limitCases {
		projected = complete.Project(BestExactEntryMappingV1().WithLimits(limits))
		if projected.Complete != nil {
			t.Fatalf("projection limit %+v must fail", limits)
		}
		if projected.Failed.Diagnostics[0].Code != "core.projection.resource-limit@1" {
			t.Fatalf("projection limit code differed")
		}
	}

	duplicate := parseText(t, WindowsV1, "[s]\r\na=1\r\nA=2\r\n")
	limits := DefaultProjectionLimits()
	limits.MaxReportEntries = 0
	projected = duplicate.Project(RequireObjectV1(ComparisonProfileEquivalent,
		CollisionPolicyFirst).WithLimits(limits))
	if projected.Complete != nil {
		t.Fatalf("report limit must fail")
	}
	if projected.Failed.Diagnostics[0].Code != "core.projection.resource-limit@1" {
		t.Fatalf("report limit code differed")
	}
}
