package conformance

import (
	"testing"
)

// TestG43FlippedCasesPass data-drives the G4.3 flipped faces directly
// against the shared vector files (https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §2.5:
// protocol.change-set.actual-edit-roundtrip, the v1 portable query
// execution cases, and the syntax.cursor.* terminal cases). The faces run
// through their handlers here so the flipped cases are executed and pinned
// before the shared runner files merge the dispatch (the merge hook points
// are reported to the main agent); the expectation facts come from the
// vector files, never from literals in this test.
func TestG43FlippedCasesPass(t *testing.T) {
	runner := repositoryRunner(t)
	cases := []struct {
		file string
		id   string
		run  func(*caseData, *SuiteReport)
	}{
		{file: "protocol-v1.json", id: "protocol.change-set.actual-edit-roundtrip",
			run: RunProtocolV1ChangeSetEditFace},
		{file: "v1.json", id: "query.root-result-limit",
			run: RunV1PortableQueryFace},
		{file: "v1.json", id: "query.cursor-failure-terminal",
			run: RunV1PortableQueryFace},
		{file: "syntax-query-v1.json", id: "syntax.cursor.completed",
			run: RunSyntaxCursorFace},
		{file: "syntax-query-v1.json", id: "syntax.cursor.cancelled",
			run: RunSyntaxCursorFace},
		{file: "syntax-query-v1.json", id: "syntax.cursor.failed",
			run: RunSyntaxCursorFace},
	}
	for _, item := range cases {
		t.Run(item.id, func(t *testing.T) {
			data, message := runner.loadSuite(suiteDefinition{
				File: item.file, SuiteID: "probe", ExpectedCases: 0,
			})
			if message != "" {
				t.Fatalf("vector load failed: %s", message)
			}
			for index := range data.Cases {
				vector := &data.Cases[index]
				if vector.ID != item.id {
					continue
				}
				report := &SuiteReport{}
				item.run(vector, report)
				if len(report.Failed) != 0 {
					t.Fatalf("case %s failed: %s", item.id, report.Failed[0].Message)
				}
				if len(report.Passed) != 1 || report.Passed[0] != item.id {
					t.Fatalf("case %s did not pass", item.id)
				}
				return
			}
			t.Fatalf("case %s not found in %s", item.id, item.file)
		})
	}
}

// TestG43FlippedCaseInventory pins the flipped case IDs against the vector
// files (a vector rename must fail this test).
func TestG43FlippedCaseInventory(t *testing.T) {
	files := map[string][]string{
		"protocol-v1.json": {"protocol.change-set.actual-edit-roundtrip"},
		"v1.json":          {"query.root-result-limit", "query.cursor-failure-terminal"},
		"syntax-query-v1.json": {"syntax.cursor.completed", "syntax.cursor.cancelled",
			"syntax.cursor.failed"},
	}
	for file, ids := range files {
		data, message := (repositoryRunner(t)).loadSuite(suiteDefinition{
			File: file, SuiteID: "probe", ExpectedCases: 0,
		})
		if message != "" {
			t.Fatalf("%s: %s", file, message)
		}
		seen := make(map[string]bool, len(ids))
		for _, vector := range data.Cases {
			for _, id := range ids {
				if vector.ID == id {
					seen[id] = true
				}
			}
		}
		for _, id := range ids {
			if !seen[id] {
				t.Errorf("%s: flipped case %s missing from the vector file", file, id)
			}
		}
	}
}
