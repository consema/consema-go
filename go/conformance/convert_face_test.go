package conformance

// The 0.15.0 G1.4 convert-face acceptance pin. The shared runner files
// (operations_v1.go default branch, json_family_v2.go "convert" action)
// are rewired by the main agent at merge time; this test drives the
// exported face handler directly with the vector data so the G1.4
// acceptance is pinned independently of the shared-file wiring.

import (
	"testing"
)

// TestG14OperationsConvertFace drives the four convert cases of
// `consema.operations.conformance@1` through RunOperationsConvertFace.
func TestG14OperationsConvertFace(t *testing.T) {
	report := &SuiteReport{}
	for _, id := range []string{
		"operations.v1.convert-json-to-toml-exact",
		"operations.v1.convert-toml-to-json-exact",
		"operations.v1.convert-duplicate-json-to-toml-fails",
		"operations.v1.convert-transformed-report",
	} {
		RunOperationsConvertFace(faceVectorCase(t, "operations-v1.json", id), report)
	}
	if len(report.Failed) != 0 {
		t.Fatalf("operations convert face failures: %v", report.Failed)
	}
	if len(report.Passed) != 4 {
		t.Fatalf("operations convert face passed %d != 4", len(report.Passed))
	}
}

// TestG14JSONFamilyConvertFace drives the three convert cases of
// `consema.json-family.conformance@2` through RunOperationsConvertFace.
func TestG14JSONFamilyConvertFace(t *testing.T) {
	report := &SuiteReport{}
	for _, id := range []string{
		"json5.convert.finite-to-strict",
		"json5.convert.nonfinite-to-strict-fails",
		"json5.convert.strict-to-json5",
	} {
		RunOperationsConvertFace(faceVectorCase(t, "json-family-v2.json", id), report)
	}
	if len(report.Failed) != 0 {
		t.Fatalf("json-family convert face failures: %v", report.Failed)
	}
	if len(report.Passed) != 3 {
		t.Fatalf("json-family convert face passed %d != 3", len(report.Passed))
	}
}
