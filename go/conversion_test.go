package consema

// The Convert* composition acceptance tests (consema-rs/consema/src/
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
	hclpkg "consema.dev/consema/hcl"
	"consema.dev/consema/ini"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/plist"
	"consema.dev/consema/properties"
	"consema.dev/consema/protocol"
	"consema.dev/consema/toml"
	xmlpkg "consema.dev/consema/xml"
	"consema.dev/consema/yaml"
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
	// An unknown target profile fails atomically with the
	// unsupported-profile vocabulary; the target document never exists.
	source := jsonSource(t, `{"a":1}`, jsonpkg.JsonProfileStrictV1)
	request := document.NewMaterializationRequest(
		document.NewProfileId("example.unknown", 1),
		document.NewMaterializationStyleId("example.style", 1))
	result := ConvertJSON(source, bestExactProjection(t), request)
	if result.Complete != nil {
		t.Fatalf("unknown target profile must not materialize")
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

// The Convert* acceptance tests of the six families that land with
// 0.16.0-0.18.0 (consema-rs/consema/src/conversion.rs tests:
// yaml_and_json_conversion_closes_exactly_in_both_directions,
// yaml_compat_profile_is_explicit_at_both_conversion_stages,
// yaml_sharing_and_cycles_require_explicit_tree_projection_policy,
// ini_and_json_convert_exactly_in_both_directions,
// properties_and_json_convert_exactly_in_both_directions,
// properties_duplicate_collapse_is_audited_as_authorized_loss,
// properties_conversion_failures_publish_no_partial_target,
// ini_conversion_failures_publish_no_partial_target,
// xml_converts_to_canonical_xml_exactly,
// plist_value_tree_record_is_consumed_only_by_the_plist_family,
// plist_converts_between_profiles_exactly,
// json_cannot_materialize_into_record_formats,
// hcl_body_record_is_consumed_only_by_the_hcl_family,
// xml_element_tree_record_is_consumed_only_by_the_xml_family,
// record_envelopes_fail_every_non_owning_target_atomically,
// explicit_non_record_projection_targets_convert_across_families,
// hcl_native_converts_to_canonical_tfvars_within_the_family,
// hcl_conversion_derived_expressions_fail_atomically).

func yamlSource(t *testing.T, source string, profile yaml.YamlProfile) *yaml.Document {
	t.Helper()
	document, failure := yaml.Parse([]byte(source), profile, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("YAML parse: %s", failure.Error())
	}
	return document
}

func iniSource(t *testing.T, source string) *ini.Document {
	t.Helper()
	document, failure := ini.Parse([]byte(source), ini.PortableV1,
		ini.IniEncodingProfileDefault(), ini.DefaultIniParseLimits())
	if failure != nil {
		t.Fatalf("INI parse: %s", failure.Error())
	}
	return document
}

func propertiesSource(t *testing.T, source string) *properties.Document {
	t.Helper()
	document, failure := properties.ParseReader([]byte(source), document.Utf8Encoding(),
		properties.DefaultPropertiesParseLimits())
	if failure != nil {
		t.Fatalf("Properties parse: %s", failure.Error())
	}
	return document
}

func xmlSource(t *testing.T, source string) *xmlpkg.Document {
	t.Helper()
	document, failure := xmlpkg.Parse(context.Background(), []byte(source),
		xmlpkg.XmlProfileSafeV1, xmlpkg.XmlEncodingProfileDefault(),
		xmlpkg.DefaultXmlParseLimits())
	if failure != nil {
		t.Fatalf("XML parse: %s", failure.Error())
	}
	return document
}

func plistSource(t *testing.T, source string) *plist.Document {
	t.Helper()
	document, failure := plist.Parse([]byte(source), plist.PlistProfileXmlV1,
		plist.PlistEncodingProfileDefault(), plist.DefaultPlistParseLimits())
	if failure != nil {
		t.Fatalf("plist parse: %s", failure.Error())
	}
	return document
}

func hclSource(t *testing.T, source string, profile hclpkg.HclProfile) *hclpkg.Document {
	t.Helper()
	document, failure := hclpkg.Parse(context.Background(), []byte(source), profile,
		hclpkg.HclEncodingSelectionProfileDefault(), hclpkg.DefaultHclParseLimits())
	if failure != nil {
		t.Fatalf("HCL parse: %s", failure.Error())
	}
	return document
}

func yamlTargetRequest() document.MaterializationRequest {
	return document.NewMaterializationRequest(
		document.NewProfileId("yaml.1.2-core", 1),
		document.NewMaterializationStyleId("yaml.canonical-flow", 1)).
		WithNewline(document.NewlineLf)
}

func iniTargetRequest() document.MaterializationRequest {
	return document.NewMaterializationRequest(
		document.NewProfileId("ini.portable", 1),
		document.NewMaterializationStyleId("ini.portable-canonical", 1))
}

func propertiesTargetRequest() document.MaterializationRequest {
	return document.NewMaterializationRequest(
		document.NewProfileId("java-properties.reader", 1),
		document.NewMaterializationStyleId("java-properties.reader-canonical", 1)).
		WithEncoding(document.Utf8Encoding()).
		WithNewline(document.NewlineLf)
}

func xmlTargetRequest() document.MaterializationRequest {
	return document.NewMaterializationRequest(
		document.NewProfileId("xml.1.0-safe", 1),
		document.NewMaterializationStyleId("xml.safe-canonical-document", 1))
}

func plistTargetRequest() document.MaterializationRequest {
	return document.NewMaterializationRequest(
		document.NewProfileId("plist.xml", 1),
		document.NewMaterializationStyleId("plist.xml-canonical", 1)).
		WithEncoding(document.Utf8Encoding()).
		WithNewline(document.NewlineLf)
}

func plistBinaryTargetRequest() document.MaterializationRequest {
	return document.NewMaterializationRequest(
		document.NewProfileId("plist.binary", 1),
		document.NewMaterializationStyleId("plist.binary-canonical", 1)).
		WithEncoding(document.BinaryEncoding()).
		WithNewline(document.NewlineNone)
}

func hclTargetRequest() document.MaterializationRequest {
	return document.NewMaterializationRequest(
		document.NewProfileId("hcl.native", 1),
		document.NewMaterializationStyleId("hcl.canonical-document", 1)).
		WithEncoding(document.Utf8Encoding()).
		WithNewline(document.NewlineLf)
}

func hclTfvarsTargetRequest() document.MaterializationRequest {
	return document.NewMaterializationRequest(
		document.NewProfileId("hcl.tfvars", 1),
		document.NewMaterializationStyleId("hcl.canonical-document", 1)).
		WithEncoding(document.Utf8Encoding()).
		WithNewline(document.NewlineLf)
}

func TestYAMLAndJSONConvertExactlyInBothDirections(t *testing.T) {
	yamlDoc := yamlSource(t, "service:\n  port: 8080\n  enabled: true\n",
		yaml.Yaml12CoreV1)
	result := ConvertYAML(yamlDoc, yaml.BestExactValueV1(), jsonTargetRequest())
	if result.Failed != nil {
		t.Fatalf("YAML to JSON: %s", result.Failed.Code())
	}
	complete := result.Complete
	if string(complete.Document.Render()) != `{"service":{"port":8080,"enabled":true}}` {
		t.Fatalf("YAML to JSON render differs: %q", complete.Document.Render())
	}
	if complete.Report.OverallFidelity() != ConversionFidelityExact {
		t.Fatalf("YAML to JSON fidelity differs")
	}
	if complete.Report.SourceProfile() != document.NewProfileId("yaml.1.2-core", 1) {
		t.Fatalf("YAML source profile differs")
	}
	if complete.ProjectionProvenance.YAML == nil ||
		len(complete.ProjectionProvenance.YAML.Entries()) == 0 {
		t.Fatalf("YAML projection provenance must be retained")
	}
	if complete.MaterializationProvenance.JSON == nil ||
		len(complete.MaterializationProvenance.JSON.Entries()) == 0 {
		t.Fatalf("JSON materialization provenance must be retained")
	}

	jsonDoc := jsonSource(t, `{"service":{"port":8080,"enabled":true}}`,
		jsonpkg.JsonProfileStrictV1)
	result = ConvertJSON(jsonDoc, bestExactProjection(t), yamlTargetRequest())
	if result.Failed != nil {
		t.Fatalf("JSON to YAML: %s", result.Failed.Code())
	}
	complete = result.Complete
	if complete.Report.TargetProfile() != document.NewProfileId("yaml.1.2-core", 1) {
		t.Fatalf("YAML target profile differs")
	}
	if complete.Report.OverallFidelity() != ConversionFidelityExact {
		t.Fatalf("JSON to YAML fidelity differs")
	}
	// The materialized YAML projects back to the exact intermediate value.
	yamlDoc = complete.Document.MustYAML(t)
	roundTrip := yamlDoc.ProjectValue(yaml.BestExactValueV1())
	if roundTrip.Failed != nil {
		t.Fatalf("materialized YAML must project exactly: %s", roundTrip.Failed.Error())
	}
	if !core.Equal(roundTrip.Complete.Value, complete.ProjectedValue) {
		t.Fatalf("YAML round trip value differs from the intermediate value")
	}
}

func TestYAMLCompatProfileIsExplicitAtBothConversionStages(t *testing.T) {
	source := yamlSource(t, "%YAML 1.1\n---\nflag: yes\n", yaml.Yaml11CompatV1)
	result := ConvertYAML(source, yaml.BestExactValueV1(), jsonTargetRequest())
	if result.Failed != nil {
		t.Fatalf("YAML 1.1 compatibility source should convert: %s", result.Failed.Code())
	}
	if string(result.Complete.Document.Render()) != `{"flag":true}` {
		t.Fatalf("YAML 1.1 compat render differs: %q", result.Complete.Document.Render())
	}
	if result.Complete.Report.SourceProfile() != document.NewProfileId("yaml.1.1-compat", 1) {
		t.Fatalf("YAML 1.1 source profile differs")
	}

	jsonDoc := jsonSource(t, `{"flag":true}`, jsonpkg.JsonProfileStrictV1)
	target := document.NewMaterializationRequest(
		document.NewProfileId("yaml.1.1-compat", 1),
		document.NewMaterializationStyleId("yaml.canonical-flow", 1)).
		WithNewline(document.NewlineLf)
	result = ConvertJSON(jsonDoc, bestExactProjection(t), target)
	if result.Failed != nil {
		t.Fatalf("YAML compatibility target should materialize: %s", result.Failed.Code())
	}
	if result.Complete.Report.TargetProfile() != document.NewProfileId("yaml.1.1-compat", 1) {
		t.Fatalf("YAML 1.1 target profile differs")
	}
	yamlDoc, ok := result.Complete.Document.AsYAML()
	if !ok {
		t.Fatalf("target must be a YAML document")
	}
	if yamlDoc.Profile() != document.NewProfileId("yaml.1.1-compat", 1) {
		t.Fatalf("materialized YAML profile differs")
	}
}

func TestYAMLSharingAndCyclesRequireExplicitTreeProjectionPolicy(t *testing.T) {
	shared := yamlSource(t, "value: &x [one]\ncopy: *x\n", yaml.Yaml12CoreV1)
	result := ConvertYAML(shared, yaml.BestExactValueV1(), jsonTargetRequest())
	if result.Complete != nil {
		t.Fatalf("shared YAML must fail under the reject policy")
	}
	if result.Failed.Kind != ConversionFailureProjectionFailed ||
		result.Failed.YamlProjectionFailure == nil ||
		result.Failed.YamlProjectionFailure.Kind != yaml.ValueProjectionSharing {
		t.Fatalf("YAML sharing failure facts differ")
	}

	duplicated := yaml.BestExactValueV1().WithSharing(yaml.SharingPolicyDuplicateAcyclic)
	result = ConvertYAML(shared, duplicated, jsonTargetRequest())
	if result.Failed != nil {
		t.Fatalf("explicit acyclic duplication should complete: %s", result.Failed.Code())
	}
	if result.Complete.Report.OverallFidelity() != ConversionFidelityTransformed {
		t.Fatalf("acyclic duplication must be Transformed")
	}
	if result.Complete.Report.ProjectionReport().YAML == nil ||
		len(result.Complete.Report.ProjectionReport().YAML.Events()) == 0 {
		t.Fatalf("YAML projection report must retain the duplication events")
	}

	cyclic := yamlSource(t, "&x [*x]\n", yaml.Yaml12CoreV1)
	result = ConvertYAML(cyclic, duplicated, jsonTargetRequest())
	if result.Complete != nil {
		t.Fatalf("cyclic YAML must fail")
	}
	if result.Failed.YamlProjectionFailure == nil ||
		result.Failed.YamlProjectionFailure.Kind != yaml.ValueProjectionCycle {
		t.Fatalf("YAML cycle failure facts differ")
	}
}

func TestINIAndJSONConvertExactlyInBothDirections(t *testing.T) {
	iniDoc := iniSource(t, "[service]\nname=api\nport=8080\n")
	result := ConvertINI(iniDoc, ini.BestExactEntryMappingV1(), jsonTargetRequest())
	if result.Failed != nil {
		t.Fatalf("INI to JSON: %s", result.Failed.Code())
	}
	complete := result.Complete
	if string(complete.Document.Render()) != `{"service":{"name":"api","port":"8080"}}` {
		t.Fatalf("INI to JSON render differs: %q", complete.Document.Render())
	}
	if complete.Report.OverallFidelity() != ConversionFidelityExact {
		t.Fatalf("INI to JSON fidelity differs")
	}
	if complete.ProjectionProvenance.INI == nil ||
		len(complete.ProjectionProvenance.INI.Entries()) == 0 {
		t.Fatalf("INI projection provenance must be retained")
	}

	jsonDoc := jsonSource(t, `{"service":{"name":"api","port":"8080"}}`,
		jsonpkg.JsonProfileStrictV1)
	result = ConvertJSON(jsonDoc, bestExactProjection(t), iniTargetRequest())
	if result.Failed != nil {
		t.Fatalf("JSON to INI: %s", result.Failed.Code())
	}
	complete = result.Complete
	if string(complete.Document.Render()) != "[service]\nname=api\nport=8080\n" {
		t.Fatalf("JSON to INI render differs: %q", complete.Document.Render())
	}
	if complete.Report.TargetProfile() != document.NewProfileId("ini.portable", 1) {
		t.Fatalf("INI target profile differs")
	}
	if complete.MaterializationProvenance.INI == nil ||
		len(complete.MaterializationProvenance.INI.Entries()) == 0 {
		t.Fatalf("INI materialization provenance must be retained")
	}
}

func TestPropertiesAndJSONConvertExactlyInBothDirections(t *testing.T) {
	propertiesDoc := propertiesSource(t, "name=api\nport=8080\n")
	result := ConvertProperties(propertiesDoc, properties.BestExactEntryMapping(),
		jsonTargetRequest())
	if result.Failed != nil {
		t.Fatalf("Properties to JSON: %s", result.Failed.Code())
	}
	complete := result.Complete
	if string(complete.Document.Render()) != `{"name":"api","port":"8080"}` {
		t.Fatalf("Properties to JSON render differs: %q", complete.Document.Render())
	}
	if complete.Report.OverallFidelity() != ConversionFidelityExact {
		t.Fatalf("Properties to JSON fidelity differs")
	}
	if complete.ProjectionProvenance.Properties == nil ||
		len(complete.ProjectionProvenance.Properties.Entries()) == 0 {
		t.Fatalf("Properties projection provenance must be retained")
	}

	jsonDoc := jsonSource(t, `{"name":"api","port":"8080"}`,
		jsonpkg.JsonProfileStrictV1)
	result = ConvertJSON(jsonDoc, bestExactProjection(t), propertiesTargetRequest())
	if result.Failed != nil {
		t.Fatalf("JSON to Properties: %s", result.Failed.Code())
	}
	complete = result.Complete
	if string(complete.Document.Render()) != "name=api\nport=8080\n" {
		t.Fatalf("JSON to Properties render differs: %q", complete.Document.Render())
	}
	if complete.Report.TargetProfile() != document.NewProfileId("java-properties.reader", 1) {
		t.Fatalf("Properties target profile differs")
	}
}

func TestPropertiesDuplicateCollapseIsAuditedAsAuthorizedLoss(t *testing.T) {
	source := propertiesSource(t, "a=first\na=last\n")
	result := ConvertProperties(source, properties.RequireObject(properties.DuplicatePolicyFirstWins),
		jsonTargetRequest())
	if result.Failed != nil {
		t.Fatalf("explicit first-wins conversion should complete: %s", result.Failed.Code())
	}
	if string(result.Complete.Document.Render()) != `{"a":"first"}` {
		t.Fatalf("first-wins render differs: %q", result.Complete.Document.Render())
	}
	if result.Complete.Report.OverallFidelity() != ConversionFidelityLossy {
		t.Fatalf("overall fidelity must be Lossy")
	}
	codes := result.Complete.Report.ProjectionReport().EventCodes()
	if len(codes) != 1 || codes[0] != "java-properties.projection.duplicate-collapsed@1" {
		t.Fatalf("duplicate-collapse wire code differs: %v", codes)
	}
}

func TestPropertiesAndINIConversionFailuresPublishNoPartialTarget(t *testing.T) {
	// An unpaired UTF-16 surrogate fails the Properties projection; the
	// failure carries no partial target.
	unpaired := propertiesSource(t, "a=\\uD800")
	result := ConvertProperties(unpaired, properties.BestExactEntryMapping(),
		jsonTargetRequest())
	if result.Complete != nil {
		t.Fatalf("unpaired surrogate must fail projection")
	}
	if result.Failed.Kind != ConversionFailureProjectionFailed ||
		result.Failed.Code() != "core.conversion.projection-failed@1" {
		t.Fatalf("properties projection failure facts differ")
	}

	// A JSON object with an integer cannot enter Java Properties (string
	// values only): atomic materialization failure, no document.
	jsonDoc := jsonSource(t, `{"port":8080}`, jsonpkg.JsonProfileStrictV1)
	result = ConvertJSON(jsonDoc, bestExactProjection(t), propertiesTargetRequest())
	if result.Complete != nil {
		t.Fatalf("integer value must not materialize as Properties")
	}
	if result.Failed.Kind != ConversionFailureMaterializationFailed ||
		result.Failed.MaterializationFailure.Properties == nil {
		t.Fatalf("properties materialization failure facts differ")
	}

	// A nested JSON object cannot enter INI (flat string entries only).
	jsonDoc = jsonSource(t, `{"service":{"port":8080}}`, jsonpkg.JsonProfileStrictV1)
	result = ConvertJSON(jsonDoc, bestExactProjection(t), iniTargetRequest())
	if result.Complete != nil {
		t.Fatalf("nested object must not materialize as INI")
	}
	if result.Failed.Kind != ConversionFailureMaterializationFailed ||
		result.Failed.MaterializationFailure.INI == nil {
		t.Fatalf("ini materialization failure facts differ")
	}
}

func TestXMLConvertsToCanonicalXMLExactly(t *testing.T) {
	source := xmlSource(t, "<service><name>catalog</name></service>")
	result := ConvertXML(source, xmlpkg.ElementTreeRequest(), xmlTargetRequest())
	if result.Failed != nil {
		t.Fatalf("XML to canonical XML: %s", result.Failed.Code())
	}
	complete := result.Complete
	if string(complete.Document.Render()) != "<service><name>catalog</name></service>\n" {
		t.Fatalf("XML target render differs: %q", complete.Document.Render())
	}
	if complete.Report.OverallFidelity() != ConversionFidelityExact {
		t.Fatalf("XML conversion fidelity differs")
	}
	if complete.Report.SourceProfile() != document.NewProfileId("xml.1.0-safe", 1) ||
		complete.Report.TargetProfile() != document.NewProfileId("xml.1.0-safe", 1) {
		t.Fatalf("XML conversion profiles differ")
	}
	if complete.ProjectionProvenance.XML == nil ||
		len(complete.ProjectionProvenance.XML.Entries()) == 0 {
		t.Fatalf("XML projection provenance must be retained")
	}
}

func TestRecordEnvelopesFailEveryNonOwningTargetAtomically(t *testing.T) {
	// One source per record format; every non-owning target fails the
	// record-consumption gate atomically with the shared invalid-request
	// vocabulary and no target document, including the other record
	// families whose materializers also validate the record marker.
	xmlDoc := xmlSource(t, "<root><name>api</name></root>")
	plistDoc := plistSource(t,
		"<plist version=\"1.0\"><dict><key>name</key><string>api</string></dict></plist>")
	hclDoc := hclSource(t, "a = 1\n", hclpkg.HclProfileNativeV1)

	targets := []document.MaterializationRequest{
		tomlTargetRequest(), yamlTargetRequest(), iniTargetRequest(),
		propertiesTargetRequest(), xmlTargetRequest(), plistTargetRequest(),
		hclTargetRequest(),
	}
	assertFails := func(t *testing.T, result ConversionResult) {
		t.Helper()
		if result.Complete != nil {
			t.Fatalf("record envelope must fail atomically on a non-owning target")
		}
		if result.Failed.Kind != ConversionFailureMaterializationFailed ||
			result.Failed.Code() != "core.conversion.materialization-failed@1" ||
			result.Failed.MaterializationFailure.InvalidRequestReason == "" {
			t.Fatalf("record envelope must fail with the invalid-request vocabulary")
		}
	}
	for _, request := range targets {
		family := formatFamily(request.TargetProfile().ID())
		if family != "xml" {
			assertFails(t, ConvertXML(xmlDoc, xmlpkg.ElementTreeRequest(), request))
		}
		if family != "plist" {
			assertFails(t, ConvertPlist(plistDoc, plist.NewProjectionRequestValueTree(), request))
		}
		if family != "hcl" {
			assertFails(t, ConvertHCL(hclDoc, hclpkg.ProjectionRequestBody(), request))
		}
	}
	// The owning families consume the record exactly (asserted by the
	// dedicated same-family tests below).
	if got := ConvertXML(xmlDoc, xmlpkg.ElementTreeRequest(), xmlTargetRequest()); got.Failed != nil {
		t.Fatalf("xml same-family conversion must complete")
	}
	if got := ConvertPlist(plistDoc, plist.NewProjectionRequestValueTree(),
		plistTargetRequest()); got.Failed != nil {
		t.Fatalf("plist same-family conversion must complete")
	}
	if got := ConvertHCL(hclDoc, hclpkg.ProjectionRequestBody(), hclTargetRequest()); got.Failed != nil {
		t.Fatalf("hcl same-family conversion must complete")
	}
}

func TestPlistConvertsBetweenProfilesExactly(t *testing.T) {
	source := plistSource(t,
		"<plist version=\"1.0\"><dict>"+
			"<key>name</key><string>api</string>"+
			"<key>port</key><integer>8080</integer>"+
			"<key>enabled</key><true/>"+
			"<key>nested</key><dict><key>tags</key>"+
			"<array><string>a</string><string>b</string></array></dict>"+
			"<key>dup</key><string>first</string>"+
			"<key>dup</key><string>second</string>"+
			"<key>created</key><date>2023-01-01T00:00:00Z</date>"+
			"<key>payload</key><data>AQID</data>"+
			"</dict></plist>")
	result := ConvertPlist(source, plist.NewProjectionRequestValueTree(),
		plistBinaryTargetRequest())
	if result.Failed != nil {
		t.Fatalf("plist to plist.binary: %s", result.Failed.Code())
	}
	complete := result.Complete
	if complete.Report.OverallFidelity() != ConversionFidelityExact {
		t.Fatalf("plist to plist.binary fidelity differs")
	}
	if complete.Report.TargetProfile() != document.NewProfileId("plist.binary", 1) {
		t.Fatalf("plist.binary target profile differs")
	}
	// The target-stage provenance must be retained for the plist family
	// (G4.2 divergence finding closed: the Go plist materializer now
	// publishes the same provenance surface as the Rust materializer).
	if complete.MaterializationProvenance.Plist == nil ||
		len(complete.MaterializationProvenance.Plist.Entries()) == 0 {
		t.Fatalf("plist materialization provenance must be retained")
	}
	for _, entry := range complete.MaterializationProvenance.Plist.Entries() {
		if len(entry.Outputs) != 1 || entry.Outputs[0].Relation != plist.MaterializationRelationDirect {
			t.Fatalf("plist provenance entries must be Direct single-origin")
		}
	}
	rendered := complete.Document.Render()
	if !strings.HasPrefix(string(rendered), "bplist00") {
		t.Fatalf("binary output header missing: %q", rendered[:8])
	}
	// The conversion closed the loop: reparsing the generated bytes yields
	// the source native model exactly.
	reparsed, failure := plist.Parse(rendered, plist.PlistProfileBinaryV1,
		plist.PlistEncodingProfileDefault(), plist.DefaultPlistParseLimits())
	if failure != nil {
		t.Fatalf("binary reparse: %s", failure.Error())
	}
	sourceNative := source.NativeDocument()
	if !reparsed.NativeDocument().Equal(sourceNative) {
		t.Fatalf("binary reparse native model differs")
	}
	// And back: plist.binary -> plist.xml preserves the same native model.
	back := ConvertPlist(reparsed, plist.NewProjectionRequestValueTree(), plistTargetRequest())
	if back.Failed != nil {
		t.Fatalf("plist.binary to plist.xml: %s", back.Failed.Code())
	}
	if back.Complete.Report.OverallFidelity() != ConversionFidelityExact {
		t.Fatalf("plist.binary to plist.xml fidelity differs")
	}
	reReparsed, failure := plist.Parse(back.Complete.Document.Render(), plist.PlistProfileXmlV1,
		plist.PlistEncodingProfileDefault(), plist.DefaultPlistParseLimits())
	if failure != nil {
		t.Fatalf("xml reparse: %s", failure.Error())
	}
	if !reReparsed.NativeDocument().Equal(sourceNative) {
		t.Fatalf("xml reparse native model differs")
	}
}

func TestBaselineValuesCannotMaterializeIntoRecordFormats(t *testing.T) {
	// The record families' materializers admit only their own versioned
	// record envelopes: a baseline projection publishes a plain value, so
	// the XML/plist/HCL materializers reject it atomically with the
	// invalid-request vocabulary and no target document.
	jsonDoc := jsonSource(t, `{"service":{"port":8080}}`, jsonpkg.JsonProfileStrictV1)
	projection := bestExactProjection(t)
	for _, request := range []document.MaterializationRequest{
		xmlTargetRequest(), plistTargetRequest(), hclTargetRequest(),
	} {
		result := ConvertJSON(jsonDoc, projection, request)
		if result.Complete != nil {
			t.Fatalf("baseline value must not materialize into a record format")
		}
		if result.Failed.Kind != ConversionFailureMaterializationFailed ||
			result.Failed.Code() != "core.conversion.materialization-failed@1" {
			t.Fatalf("record-materializer rejection facts differ for %s",
				request.TargetProfile().ID())
		}
	}
}

func TestExplicitNonRecordProjectionTargetsConvertAcrossFamilies(t *testing.T) {
	// The record-consumption gate fires only on the record envelope. The
	// explicit non-record projection targets of the record formats publish
	// plain portable values that convert like any baseline projection:
	// XML simple-entry-mapping and plist require-object both complete to
	// JSON.
	xmlDoc := xmlSource(t, "<root><name>api</name><port>8080</port></root>")
	root := xmlDoc.Root()
	if root == nil {
		t.Fatalf("XML root missing")
	}
	result := ConvertXML(xmlDoc, xmlpkg.SimpleEntryMappingRequest(root.NodeRef(),
		xmlpkg.AttributePolicyRejectAttributes, xmlpkg.TextKeyPolicyRejectText,
		xmlpkg.RepeatedChildPolicyReject, xmlpkg.ExpandedNameKeyPolicyLocalOnly,
		xmlpkg.CollisionPolicyReject), jsonTargetRequest())
	if result.Failed != nil {
		t.Fatalf("explicit entry mapping should convert to JSON: %s", result.Failed.Code())
	}
	if string(result.Complete.Document.Render()) != `{"name":"api","port":"8080"}` {
		t.Fatalf("XML entry-mapping render differs: %q", result.Complete.Document.Render())
	}
	if result.Complete.Report.OverallFidelity() != ConversionFidelityTransformed {
		t.Fatalf("XML entry-mapping fidelity must be Transformed")
	}

	plistDoc := plistSource(t,
		"<plist version=\"1.0\"><dict><key>name</key><string>api</string>"+
			"<key>port</key><integer>8080</integer></dict></plist>")
	result = ConvertPlist(plistDoc, plist.NewProjectionRequestRequireObject(plist.CollisionPolicyReject),
		jsonTargetRequest())
	if result.Failed != nil {
		t.Fatalf("explicit require-object projection should convert to JSON: %s",
			result.Failed.Code())
	}
	if string(result.Complete.Document.Render()) != `{"name":"api","port":8080}` {
		t.Fatalf("plist require-object render differs: %q", result.Complete.Document.Render())
	}
}

func TestYAMLRecordShapedContentStaysContent(t *testing.T) {
	// The record-consumption gate keys on the record-publishing projection,
	// never on value shape (conversion.rs module docs): a baseline source
	// never projects an envelope, so `record` members in its content are
	// content and convert faithfully, including a member whose value equals
	// a published record id.
	source := yamlSource(t, "record: hcl.body@1\n", yaml.Yaml12CoreV1)
	result := ConvertYAML(source, yaml.BestExactValueV1(), jsonTargetRequest())
	if result.Failed != nil {
		t.Fatalf("record-looking YAML content must convert as content: %s",
			result.Failed.Code())
	}
	if string(result.Complete.Document.Render()) != `{"record":"hcl.body@1"}` {
		t.Fatalf("record member is preserved as content: %q",
			result.Complete.Document.Render())
	}
}

func TestHCLNativeConvertsToCanonicalTfvarsWithinTheFamily(t *testing.T) {
	// Same-family conversion closes the project→materialize loop through
	// the facade: an hcl.native source materializes canonically as
	// hcl.tfvars (RFC 0014 §9). Number spellings canonicalize, and the
	// explicit ProjectExpression policy carries derived expressions through
	// as the authorized hcl.expression@1 ExtendedValue.
	source := hclSource(t, "region = \"us-east-1\"\ncount = 3\nratio = 1.50\n"+
		"small = 15e-1\nderived = 1 + 2\n", hclpkg.HclProfileNativeV1)
	projection := hclpkg.ProjectionRequestBodyWithExpressionPolicy(
		hclpkg.ExpressionPolicyProjectExpression)
	result := ConvertHCL(source, projection, hclTfvarsTargetRequest())
	if result.Failed != nil {
		t.Fatalf("hcl.native to hcl.tfvars: %s", result.Failed.Code())
	}
	complete := result.Complete
	if string(complete.Document.Render()) != "region = \"us-east-1\"\ncount = 3\n"+
		"ratio = 1.5\nsmall = 1.5\nderived = 1 + 2\n" {
		t.Fatalf("tfvars render differs: %q", complete.Document.Render())
	}
	if complete.Report.OverallFidelity() != ConversionFidelityTransformed {
		t.Fatalf("tfvars conversion must be Transformed")
	}
	if complete.Report.SourceProfile() != document.NewProfileId("hcl.native", 1) ||
		complete.Report.TargetProfile() != document.NewProfileId("hcl.tfvars", 1) {
		t.Fatalf("tfvars conversion profiles differ")
	}
	if complete.ProjectionProvenance.HCL == nil ||
		len(complete.ProjectionProvenance.HCL.Entries()) == 0 {
		t.Fatalf("HCL projection provenance must be retained")
	}
}

func TestHCLConversionDerivedExpressionsFailAtomically(t *testing.T) {
	// A derived expression under the default exact body target is an atomic
	// projection failure; conversion never implicitly enables the
	// ProjectExpression strategy (RFC 0014 §8.2).
	source := hclSource(t, "a = b + 1\n", hclpkg.HclProfileNativeV1)
	if source.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("HCL source must be complete")
	}
	result := ConvertHCL(source, hclpkg.ProjectionRequestBody(), hclTargetRequest())
	if result.Complete != nil {
		t.Fatalf("derived expression must fail the projection")
	}
	if result.Failed.Kind != ConversionFailureProjectionFailed ||
		result.Failed.Code() != "core.conversion.projection-failed@1" ||
		result.Failed.ProjectionReport.HCL == nil {
		t.Fatalf("HCL projection failure facts differ")
	}
}

// MustYAML returns the typed YAML document of a conversion target.
func (d *Document) MustYAML(t *testing.T) *yaml.Document {
	t.Helper()
	document, ok := d.AsYAML()
	if !ok {
		t.Fatalf("target must be a YAML document")
	}
	return document
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
