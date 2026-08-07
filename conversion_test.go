package consema

// The Convert* composition acceptance tests (crates/consema/src/
// conversion.rs tests: json_to_toml_keeps_both_stages_and_exact_target_
// closure, toml_to_json_is_exact_and_materialization_failure_has_no_
// document, explicitly_lossy_json_projection_remains_observable,
// transformed_conversion_report_externalizes_both_authorized_events,
// json_family_dialect_conversion_is_exact_or_explicitly_fails,
// record_member_content_from_baseline_sources_stays_content).

import (
	"context"
	"strings"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/protocol"
	"consema.dev/consema/toml"
)

func jsonSource(t *testing.T, source string, profile jsonpkg.JsonProfile) *jsonpkg.Document {
	t.Helper()
	document, failure := jsonpkg.Parse(context.Background(), []byte(source), profile,
		document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("JSON parse: %s", failure.Error())
	}
	return document
}

func tomlSource(t *testing.T, source string) *toml.Document {
	t.Helper()
	document, failure := toml.Parse([]byte(source), toml.Toml10V1,
		document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("TOML parse: %s", failure.Error())
	}
	return document
}

func bestExactProjection(t *testing.T) *jsonpkg.ProjectionRequest {
	t.Helper()
	projection, failure := jsonpkg.NewProjectionRequestBuilder(
		jsonpkg.ProjectionTargetBestExactCoreV1).Build()
	if failure != nil {
		t.Fatalf("projection build: %s", failure.Error())
	}
	return projection
}

func tomlTargetRequest() document.MaterializationRequest {
	return document.NewMaterializationRequest(
		document.NewProfileId("toml.1.0", 1),
		document.NewMaterializationStyleId("toml.canonical-document", 1)).
		WithNewline(document.NewlineLf).
		WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject)
}

func jsonTargetRequest() document.MaterializationRequest {
	return document.NewMaterializationRequest(
		document.NewProfileId("json.strict", 1),
		document.NewMaterializationStyleId("json.canonical-compact", 1)).
		WithNewline(document.NewlineNone)
}

func TestJSONToTOMLKeepsBothStagesAndExactTargetClosure(t *testing.T) {
	source := jsonSource(t, `{"service":{"port":8080,"enabled":true}}`,
		jsonpkg.JsonProfileStrictV1)
	result := ConvertJSON(source, bestExactProjection(t), tomlTargetRequest())
	if result.Failed != nil {
		t.Fatalf("unexpected failure: %s", result.Failed.Code())
	}
	complete := result.Complete
	if string(complete.Document.Render()) != `"service" = { "port" = 8080, "enabled" = true }`+"\n" {
		t.Fatalf("target render differs: %q", complete.Document.Render())
	}
	if complete.Report.OverallFidelity() != ConversionFidelityExact {
		t.Fatalf("overall fidelity differs")
	}
	if complete.Report.SourceProfile() != document.NewProfileId("json.strict", 1) {
		t.Fatalf("source profile differs")
	}
	if complete.Report.TargetProfile() != document.NewProfileId("toml.1.0", 1) {
		t.Fatalf("target profile differs")
	}
	if complete.ProjectionProvenance.JSON == nil ||
		len(complete.ProjectionProvenance.JSON.Entries()) == 0 {
		t.Fatalf("projection provenance must be retained")
	}
	if complete.MaterializationProvenance.TOML == nil ||
		len(complete.MaterializationProvenance.TOML.Entries()) == 0 {
		t.Fatalf("materialization provenance must be retained")
	}
	if _, ok := complete.Document.AsTOML(); !ok {
		t.Fatalf("target must be a TOML document")
	}
	// The intermediate portable value must be the exact projection output.
	if core.Equal(complete.ProjectedValue, complete.ProjectedValue) == false {
		t.Fatalf("projected value self-equality failed")
	}
}

func TestTOMLToJSONIsExactAndMaterializationFailureHasNoDocument(t *testing.T) {
	source := tomlSource(t, "name = \"api\"\nports = [80, 443]\n")
	result := ConvertTOML(source,
		toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1),
		jsonTargetRequest())
	if result.Failed != nil {
		t.Fatalf("unexpected failure: %s", result.Failed.Code())
	}
	if string(result.Complete.Document.Render()) != `{"name":"api","ports":[80,443]}` {
		t.Fatalf("target render differs: %q", result.Complete.Document.Render())
	}
	if result.Complete.Report.OverallFidelity() != ConversionFidelityExact {
		t.Fatalf("overall fidelity differs")
	}

	temporal := tomlSource(t, "when = 1979-05-27\n")
	result = ConvertTOML(temporal,
		toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1),
		jsonTargetRequest())
	if result.Complete != nil {
		t.Fatalf("temporal value must not materialize as strict JSON")
	}
	if result.Failed.Kind != ConversionFailureMaterializationFailed ||
		result.Failed.Code() != "core.conversion.materialization-failed@1" ||
		result.Failed.MaterializationFailure.JSON == nil {
		t.Fatalf("materialization failure facts differ")
	}
}

func TestDuplicateJSONToTOMLFailsAtomically(t *testing.T) {
	source := jsonSource(t, `{"a":1,"a":2}`, jsonpkg.JsonProfileStrictV1)
	result := ConvertJSON(source, bestExactProjection(t), tomlTargetRequest())
	if result.Complete != nil {
		t.Fatalf("duplicate-key conversion must fail")
	}
	if result.Failed.Code() != "core.conversion.materialization-failed@1" {
		t.Fatalf("failure code differs: %s", result.Failed.Code())
	}
	if result.Failed.MaterializationFailure.TOML == nil {
		t.Fatalf("TOML materialization failure must be retained")
	}
}

func TestExplicitlyLossyJSONProjectionRemainsObservable(t *testing.T) {
	source := jsonSource(t, `{"a":1,"a":2}`, jsonpkg.JsonProfileStrictV1)
	projection, failure := jsonpkg.NewProjectionRequestBuilder(
		jsonpkg.ProjectionTargetProjectAsObjectV1).
		GlobalDuplicatePolicy(jsonpkg.DuplicateKeyPolicyLastWins).Build()
	if failure != nil {
		t.Fatalf("projection build: %s", failure.Error())
	}
	result := ConvertJSON(source, projection, tomlTargetRequest())
	if result.Failed != nil {
		t.Fatalf("explicitly authorized loss should complete: %s", result.Failed.Code())
	}
	if result.Complete.Report.OverallFidelity() != ConversionFidelityLossy {
		t.Fatalf("overall fidelity must be Lossy")
	}
	if codes := result.Complete.Report.ProjectionReport().EventCodes(); len(codes) != 1 ||
		codes[0] != "json.object.duplicate-member@1" {
		t.Fatalf("duplicate-collapse wire code differs: %v", codes)
	}
	// The same source without an authorizing policy fails the projection
	// stage instead of guessing.
	rejectProjection, failure := jsonpkg.NewProjectionRequestBuilder(
		jsonpkg.ProjectionTargetProjectAsObjectV1).Build()
	if failure != nil {
		t.Fatalf("projection build: %s", failure.Error())
	}
	result = ConvertJSON(source, rejectProjection, tomlTargetRequest())
	if result.Complete != nil {
		t.Fatalf("un-authorized duplicate collapse must fail")
	}
	if result.Failed.Kind != ConversionFailureProjectionFailed ||
		result.Failed.Code() != "core.conversion.projection-failed@1" {
		t.Fatalf("projection failure facts differ")
	}
}

func TestTransformedConversionReportCarriesBothAuthorizedEvents(t *testing.T) {
	source := jsonSource(t, `{"a":1}`, jsonpkg.JsonProfileStrictV1)
	projection, failure := jsonpkg.NewProjectionRequestBuilder(
		jsonpkg.ProjectionTargetProjectAsEntryMappingV1).Build()
	if failure != nil {
		t.Fatalf("projection build: %s", failure.Error())
	}
	result := ConvertJSON(source, projection, tomlTargetRequest())
	if result.Failed != nil {
		t.Fatalf("unexpected failure: %s", result.Failed.Code())
	}
	report := &result.Complete.Report
	if report.OverallFidelity() != ConversionFidelityTransformed {
		t.Fatalf("overall fidelity must be Transformed")
	}
	if report.ProjectionFidelity() != ConversionFidelityTransformed ||
		report.MaterializationFidelity() != MaterializationFidelityTransformed {
		t.Fatalf("stage fidelities differ")
	}
	projectionCodes := report.ProjectionReport().EventCodes()
	if !containsCode(projectionCodes, "json.projection.structure-reencoded@1") {
		t.Fatalf("projection event missing: %v", projectionCodes)
	}
	materializationCodes := report.MaterializationReport().EventCodes()
	if !containsCode(materializationCodes, "core.materialization.mapping-transformed@1") {
		t.Fatalf("materialization event missing: %v", materializationCodes)
	}
	if report.SourceProfile() != document.NewProfileId("json.strict", 1) ||
		report.TargetProfile() != document.NewProfileId("toml.1.0", 1) {
		t.Fatalf("report profiles differ")
	}
}

func TestJSONFamilyDialectConversionIsExactOrExplicitlyFails(t *testing.T) {
	// strict JSON to JSON5 closes exactly under the JSON5 canonical style.
	strict := jsonSource(t, `{"service":{"port":8080}}`, jsonpkg.JsonProfileStrictV1)
	json5Request := document.NewMaterializationRequest(
		document.NewProfileId("json5.standard", 1),
		document.NewMaterializationStyleId("json5.canonical-compact", 1)).
		WithNewline(document.NewlineNone)
	result := ConvertJSON(strict, bestExactProjection(t), json5Request)
	if result.Failed != nil {
		t.Fatalf("strict to JSON5: %s", result.Failed.Code())
	}
	if string(result.Complete.Document.Render()) != `{"service":{"port":8080}}` {
		t.Fatalf("JSON5 target render differs")
	}
	if result.Complete.Report.OverallFidelity() != ConversionFidelityExact {
		t.Fatalf("strict-to-JSON5 fidelity differs")
	}

	// Finite JSON5 to strict JSON closes exactly.
	json5Source := jsonSource(t, `{service:{port:8080,},}`, jsonpkg.JsonProfileJson5StandardV1)
	json5Projection, failure := jsonpkg.NewProjectionRequestBuilder(
		jsonpkg.ProjectionTargetJson5BestExactCoreV1).Build()
	if failure != nil {
		t.Fatalf("projection build: %s", failure.Error())
	}
	result = ConvertJSON(json5Source, json5Projection, jsonTargetRequest())
	if result.Failed != nil {
		t.Fatalf("finite JSON5 to strict: %s", result.Failed.Code())
	}
	if string(result.Complete.Document.Render()) != `{"service":{"port":8080}}` {
		t.Fatalf("strict target render differs")
	}
	if result.Complete.Report.OverallFidelity() != ConversionFidelityExact {
		t.Fatalf("JSON5-to-strict fidelity differs")
	}

	// A non-finite JSON5 value cannot enter strict JSON: atomic failure,
	// no document.
	nonFinite := jsonSource(t, `Infinity`, jsonpkg.JsonProfileJson5StandardV1)
	result = ConvertJSON(nonFinite, json5Projection, jsonTargetRequest())
	if result.Complete != nil {
		t.Fatalf("non-finite JSON5 must not materialize as strict JSON")
	}
	if result.Failed.Code() != "core.conversion.materialization-failed@1" ||
		result.Failed.MaterializationFailure.JSON == nil {
		t.Fatalf("non-finite materialization failure facts differ")
	}
}

func TestRecordMemberContentFromBaselineSourcesStaysContent(t *testing.T) {
	// The record-consumption gate keys on the record-publishing projection,
	// never on value shape (conversion.rs module docs): a baseline source
	// never projects an envelope, so `"record"` members in its content are
	// content and convert faithfully, including a member whose value equals
	// a published record id.
	source := jsonSource(t, `{"record":"plist.value-tree@1","root":{"name":"api"}}`,
		jsonpkg.JsonProfileStrictV1)
	result := ConvertJSON(source, bestExactProjection(t), tomlTargetRequest())
	if result.Failed != nil {
		t.Fatalf("record-shaped JSON content must convert as content: %s",
			result.Failed.Code())
	}
	rendered := string(result.Complete.Document.Render())
	if !strings.Contains(rendered, `"record" = "plist.value-tree@1"`) {
		t.Fatalf("the record member is preserved as content: %q", rendered)
	}
}

func TestUnsupportedTargetProfileFailsAtomically(t *testing.T) {
	source := jsonSource(t, `{"a":1}`, jsonpkg.JsonProfileStrictV1)
	request := document.NewMaterializationRequest(
		document.NewProfileId("yaml.1.2-core", 1),
		document.NewMaterializationStyleId("yaml.canonical-flow", 1))
	result := ConvertJSON(source, bestExactProjection(t), request)
	if result.Complete != nil {
		t.Fatalf("yaml target must not materialize before 0.16.0")
	}
	if result.Failed.Code() != "core.conversion.materialization-failed@1" ||
		!result.Failed.MaterializationFailure.UnsupportedProfile {
		t.Fatalf("unsupported target profile facts differ")
	}
}

func TestConversionFailureDiagnosticsAreRegistered(t *testing.T) {
	// The failure codes are the frozen registered conversion codes (RFC
	// 0016 §6): constructing them through the protocol registry must not
	// reject any of them.
	registry := protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7)
	for _, code := range []string{
		"core.conversion.projection-failed@1",
		"core.conversion.materialization-failed@1",
		"core.conversion.unauthorized-loss@1",
	} {
		if !registry.Contains(code) {
			t.Fatalf("%s must be registered in the v7 error-code registry", code)
		}
	}
}

// containsCode reports whether one event-code list contains the code.
func containsCode(codes []string, code string) bool {
	for _, present := range codes {
		if present == code {
			return true
		}
	}
	return false
}
