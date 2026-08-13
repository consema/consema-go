package conformance

// The convert face of the shared suites: the four `core.conversion-report@1`
// cases of `consema.operations.conformance@1` (operations.v1.convert-*)
// and the three `core.conversion@1` cases of
// `consema.json-family.conformance@2` (json5.convert.*), driven through
// the root package Convert* composition (0.15.0 G1.4; consema-rs/
// consema-conformance/src/operations_v1.rs convert_* and json_family_v2.rs
// conversion_case). The shared runner files dispatch these case IDs to
// the exported handler below:
//
//   - operations_v1.go default branch (the four operations.v1.convert-*
//     case IDs);
//   - json_family_v2.go "convert" action (the three json5.convert.* case
//     IDs).
//
// All seven cases execute (the documented-skips era ended with the 0.15.0
// G1.4 root-package Convert* composition; G054, adversarial audit
// 2026-08-13). Those shared files are edited by the milestone coordinator;
// this file is the root-package face implementation they call. Every
// handler is data-driven: the vector input and expected facts drive the
// execution,
// and no expectation literal lives here.

import (
	"context"
	"fmt"
	"strings"

	"consema.dev/consema"
	"consema.dev/consema/document"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/toml"
)

// RunOperationsConvertFace executes one convert case of the shared
// suites (core.conversion@1 / core.conversion-report@1). The case IDs
// are dispatched internally; any other case ID is reported as a failure
// so the runner never silently ignores a published case.
func RunOperationsConvertFace(vector *caseData, report *SuiteReport) {
	switch vector.ID {
	case "operations.v1.convert-json-to-toml-exact",
		"operations.v1.convert-toml-to-json-exact",
		"operations.v1.convert-duplicate-json-to-toml-fails",
		"operations.v1.convert-transformed-report":
		runOperationsConvertCase(vector, report)
	case "json5.convert.finite-to-strict",
		"json5.convert.nonfinite-to-strict-fails",
		"json5.convert.strict-to-json5":
		runJSONFamilyConvertCase(vector, report)
	default:
		report.Failed = append(report.Failed, CaseFailure{
			ID:      vector.ID,
			Message: "runner does not recognize published convert case",
		})
	}
}

// runOperationsConvertCase dispatches the four operations-v1 convert
// cases (operations_v1.rs convert_json_toml, convert_toml_json,
// convert_duplicate_failure, convert_transformed).
func runOperationsConvertCase(vector *caseData, report *SuiteReport) {
	switch vector.ID {
	case "operations.v1.convert-json-to-toml-exact":
		runConvertJSONToTOML(vector, report)
	case "operations.v1.convert-toml-to-json-exact":
		runConvertTOMLToJSON(vector, report)
	case "operations.v1.convert-duplicate-json-to-toml-fails":
		runConvertDuplicateJSONFailure(vector, report)
	case "operations.v1.convert-transformed-report":
		runConvertTransformedReport(vector, report)
	default:
		report.Failed = append(report.Failed, CaseFailure{
			ID:      vector.ID,
			Message: "runner does not recognize published operations convert case",
		})
	}
}

// runConvertJSONToTOML mirrors operations_v1.rs convert_json_toml: strict
// JSON projected under BestExactCore, materialized as TOML 1.0 canonical
// with the explicit unique-string-entries mapping policy.
func runConvertJSONToTOML(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	doc, failure := jsonpkg.Parse(context.Background(), []byte(source),
		jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	projection, projectionFailure := jsonpkg.NewProjectionRequestBuilder(
		jsonpkg.ProjectionTargetBestExactCoreV1).Build()
	if projectionFailure != nil {
		failJSONCase(vector, report, projectionFailure.Error())
		return
	}
	result := consema.ConvertJSON(doc, projection,
		tomlRequest(document.MappingPolicyUniqueStringEntriesToObject))
	if result.Failed != nil {
		failJSONCase(vector, report, "unexpected conversion failure: "+result.Failed.Code())
		return
	}
	output, ok := stringField(vector.Expected, "output")
	if !ok {
		failJSONCase(vector, report, "missing expected.output")
		return
	}
	fidelity, ok := stringField(vector.Expected, "overall_fidelity")
	if !ok {
		failJSONCase(vector, report, "missing expected.overall_fidelity")
		return
	}
	if string(result.Complete.Document.Render()) != output ||
		result.Complete.Report.OverallFidelity().String() != fidelity {
		failJSONCase(vector, report, "conversion output or fidelity did not match")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// runConvertTOMLToJSON mirrors operations_v1.rs convert_toml_json: TOML
// projected under BestExactCore, materialized as strict JSON canonical
// compact.
func runConvertTOMLToJSON(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	doc, message := parseTomlBytes([]byte(source))
	if message != "" {
		failJSONCase(vector, report, message)
		return
	}
	result := consema.ConvertTOML(doc,
		toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1),
		jsonCompactRequest())
	if result.Failed != nil {
		failJSONCase(vector, report, "unexpected conversion failure: "+result.Failed.Code())
		return
	}
	output, ok := stringField(vector.Expected, "output")
	if !ok {
		failJSONCase(vector, report, "missing expected.output")
		return
	}
	fidelity, ok := stringField(vector.Expected, "overall_fidelity")
	if !ok {
		failJSONCase(vector, report, "missing expected.overall_fidelity")
		return
	}
	if string(result.Complete.Document.Render()) != output ||
		result.Complete.Report.OverallFidelity().String() != fidelity {
		failJSONCase(vector, report, "conversion output or fidelity did not match")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// runConvertDuplicateJSONFailure mirrors operations_v1.rs
// convert_duplicate_failure: the duplicate-key JSON source projects as an
// EntryMapping under BestExactCore, and the TOML target under the
// unique-string-entries policy fails atomically; the conversion failure
// must carry the frozen code and no target document.
func runConvertDuplicateJSONFailure(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	doc, failure := jsonpkg.Parse(context.Background(), []byte(source),
		jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	projection, projectionFailure := jsonpkg.NewProjectionRequestBuilder(
		jsonpkg.ProjectionTargetBestExactCoreV1).Build()
	if projectionFailure != nil {
		failJSONCase(vector, report, projectionFailure.Error())
		return
	}
	result := consema.ConvertJSON(doc, projection,
		tomlRequest(document.MappingPolicyUniqueStringEntriesToObject))
	code, ok := stringField(vector.Expected, "code")
	if !ok {
		failJSONCase(vector, report, "missing expected.code")
		return
	}
	hasDocument, ok := booleanField(vector.Expected, "has_document")
	if !ok {
		failJSONCase(vector, report, "missing expected.has_document")
		return
	}
	if result.Complete != nil {
		failJSONCase(vector, report, "duplicate-key conversion unexpectedly completed")
		return
	}
	if result.Failed.Code() != code || hasDocument {
		failJSONCase(vector, report, "conversion failure facts did not match")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// runConvertTransformedReport mirrors operations_v1.rs
// convert_transformed: the explicit EntryMapping projection and the
// unique-string-entries materialization both report their authorized
// transformations, and the overall fidelity is Transformed.
func runConvertTransformedReport(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	doc, failure := jsonpkg.Parse(context.Background(), []byte(source),
		jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	projection, projectionFailure := jsonpkg.NewProjectionRequestBuilder(
		jsonpkg.ProjectionTargetProjectAsEntryMappingV1).Build()
	if projectionFailure != nil {
		failJSONCase(vector, report, projectionFailure.Error())
		return
	}
	result := consema.ConvertJSON(doc, projection,
		tomlRequest(document.MappingPolicyUniqueStringEntriesToObject))
	if result.Failed != nil {
		failJSONCase(vector, report, "unexpected conversion failure: "+result.Failed.Code())
		return
	}
	fidelity, ok := stringField(vector.Expected, "overall_fidelity")
	if !ok {
		failJSONCase(vector, report, "missing expected.overall_fidelity")
		return
	}
	projectionEvent, ok := stringField(vector.Expected, "projection_event")
	if !ok {
		failJSONCase(vector, report, "missing expected.projection_event")
		return
	}
	materializationEvent, ok := stringField(vector.Expected, "materialization_event")
	if !ok {
		failJSONCase(vector, report, "missing expected.materialization_event")
		return
	}
	if result.Complete.Report.OverallFidelity().String() != fidelity ||
		!containsCode(result.Complete.Report.ProjectionReport().EventCodes(), projectionEvent) ||
		!containsCode(result.Complete.Report.MaterializationReport().EventCodes(),
			materializationEvent) {
		failJSONCase(vector, report, "transformed conversion report facts did not match")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// runJSONFamilyConvertCase mirrors json_family_v2.rs conversion_case: the
// JSON-family dialect conversion driven by the source/target profile and
// style facts.
func runJSONFamilyConvertCase(vector *caseData, report *SuiteReport) {
	sourceProfileName, ok := stringField(vector.Input, "source_profile")
	if !ok {
		failJSONCase(vector, report, "missing input.source_profile")
		return
	}
	profile, ok := parseJSONProfile(sourceProfileName)
	if !ok {
		failJSONCase(vector, report, "unknown source profile "+sourceProfileName)
		return
	}
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	targetProfileName, ok := stringField(vector.Input, "target_profile")
	if !ok {
		failJSONCase(vector, report, "missing input.target_profile")
		return
	}
	style, ok := stringField(vector.Input, "style")
	if !ok {
		failJSONCase(vector, report, "missing input.style")
		return
	}
	doc, failure := jsonpkg.Parse(context.Background(), []byte(source), profile,
		document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	target := jsonpkg.ProjectionTargetBestExactCoreV1
	if profile == jsonpkg.JsonProfileJson5StandardV1 {
		target = jsonpkg.ProjectionTargetJson5BestExactCoreV1
	}
	projection, projectionFailure := jsonpkg.NewProjectionRequestBuilder(target).Build()
	if projectionFailure != nil {
		failJSONCase(vector, report, projectionFailure.Error())
		return
	}
	targetProfile, ok := profileIDFromName(targetProfileName)
	if !ok {
		failJSONCase(vector, report, "malformed target profile "+targetProfileName)
		return
	}
	request := document.NewMaterializationRequest(targetProfile,
		document.NewMaterializationStyleId(style, 1)).WithNewline(document.NewlineNone)
	result := consema.ConvertJSON(doc, projection, request)
	if result.Complete != nil {
		output, ok := stringField(vector.Expected, "output")
		if !ok {
			failJSONCase(vector, report, "missing expected.output")
			return
		}
		fidelity, ok := stringField(vector.Expected, "fidelity")
		if !ok {
			failJSONCase(vector, report, "missing expected.fidelity")
			return
		}
		if string(result.Complete.Document.Render()) != output ||
			result.Complete.Report.OverallFidelity().String() != fidelity {
			failJSONCase(vector, report, "conversion output or fidelity did not match")
			return
		}
		report.Passed = append(report.Passed, vector.ID)
		return
	}
	expectedFailure, ok := stringField(vector.Expected, "failure")
	if !ok {
		failJSONCase(vector, report, "missing expected.failure")
		return
	}
	actual := "unknown"
	if result.Failed.Kind == consema.ConversionFailureMaterializationFailed &&
		result.Failed.MaterializationFailure.JSON != nil {
		actual = jsonMaterializationFailureName(result.Failed.MaterializationFailure.JSON)
	}
	if actual != expectedFailure {
		failJSONCase(vector, report, fmt.Sprintf("conversion failure %s != %s",
			actual, expectedFailure))
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// jsonCompactRequest builds the frozen strict-JSON canonical-compact
// materialization request.
func jsonCompactRequest() document.MaterializationRequest {
	return document.NewMaterializationRequest(
		document.NewProfileId("json.strict", 1),
		document.NewMaterializationStyleId("json.canonical-compact", 1)).
		WithNewline(document.NewlineNone)
}

// profileIDFromName resolves a versioned profile spelling ("json.strict@1")
// onto a ProfileId.
func profileIDFromName(name string) (document.ProfileId, bool) {
	id, version, found := strings.Cut(name, "@")
	if !found || version == "" || strings.Contains(version, "@") {
		return document.ProfileId{}, false
	}
	number := uint64(0)
	for _, digit := range version {
		if digit < '0' || digit > '9' {
			return document.ProfileId{}, false
		}
		number = number*10 + uint64(digit-'0')
	}
	if number == 0 || number > 1<<31 {
		return document.ProfileId{}, false
	}
	return document.NewProfileId(id, uint32(number)), true
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
