package consema

// This file implements the audited projection-to-materialization
// composition (crates/consema/src/conversion.rs; RFC 0016 §3.2 line 108:
// convert lives in the root package only).
//
// Every Convert* function composes one format-owned projection and the
// requested target materializer, retaining the intermediate portable
// value, both provenance directions, and the two-stage report. The
// composition never invents a cross-format convention: the projection
// target, the materialization request, the mapping policy, and the
// representability policy are explicit caller choices
// (document.MaterializationRequest defaults: UTF-8, LF, Object-only
// MappingPolicyRequireObject, RepresentabilityPolicyExactOnly).
//
// Loss discipline: a projection that contains explicitly irreversible
// loss fails atomically with ConversionFailureUnauthorizedLoss unless
// every lossy event carries an explicit authorizing policy rule
// (conversion.rs convert_json); a failure never returns a partial target
// document.
//
// Baseline families (JSON, TOML, YAML, INI, Java Properties) project
// plain portable values that convert to every target family under the
// target's representability rules. The record families (XML, plist, HCL)
// project versioned internal records (`xml.element-tree@1`,
// `plist.value-tree@1`, `hcl.body@1`; RFC 0012 §9, RFC 0013 §9, RFC 0014
// §8.2) that only their owning format family's materializer consumes: the
// record-consumption gate fails a conversion atomically with the shared
// invalid-request vocabulary, no target document and no partial bytes,
// whenever the record's owning family is not the target profile's family.
// Same-family directions (for example `plist.xml` to `plist.binary`, or
// `hcl.native` to `hcl.tfvars`) pass the gate and the owning materializer
// consumes the record under its own validation and closure. The gate keys
// on the record-publishing projection, never on value shape alone: a
// baseline source never projects an envelope, so a `"record"` member in
// JSON/TOML/YAML/INI/Properties content is content (`{"record":"my-app"}`
// remains ordinary JSON), and the explicit non-record projection targets
// of the record formats (XML SimpleEntryMappingV1 and TextContentV1,
// plist RequireObjectV1) publish plain portable values that convert like
// any baseline projection.

import (
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

// ConversionFidelity is the whole-conversion semantic fidelity
// (conversion.rs:42-51). The ordering Exact < Transformed < Lossy is
// frozen: the overall fidelity is the worst fidelity across both stages.
type ConversionFidelity uint8

// The three frozen fidelity classes.
const (
	// ConversionFidelityExact means both stages retain exact portable
	// semantics.
	ConversionFidelityExact ConversionFidelity = iota
	// ConversionFidelityTransformed means at least one stage performs an
	// authorized reversible transformation.
	ConversionFidelityTransformed
	// ConversionFidelityLossy means the projection contains explicitly
	// authorized irreversible loss.
	ConversionFidelityLossy
)

// String returns the stable fidelity name.
func (f ConversionFidelity) String() string {
	switch f {
	case ConversionFidelityExact:
		return "Exact"
	case ConversionFidelityTransformed:
		return "Transformed"
	case ConversionFidelityLossy:
		return "Lossy"
	}
	return "Exact"
}

// MaterializationFidelity is the closed materialization fidelity of the
// composition stage (the family materialization fidelities map onto this
// root-level value).
type MaterializationFidelity uint8

// The two frozen fidelity classes.
const (
	// MaterializationFidelityExact means every input fact round-trips
	// through the target's exact projection contract.
	MaterializationFidelityExact MaterializationFidelity = iota
	// MaterializationFidelityTransformed means complete semantics survive
	// an explicit reversible re-encoding.
	MaterializationFidelityTransformed
)

// String returns the stable fidelity name.
func (f MaterializationFidelity) String() string {
	switch f {
	case MaterializationFidelityExact:
		return "Exact"
	case MaterializationFidelityTransformed:
		return "Transformed"
	}
	return "Exact"
}

// ConversionProjectionReport retains the complete format-owned projection
// report without flattening its facts (conversion.rs:53-72). Exactly one
// family report is set, matching the source family.
type ConversionProjectionReport struct {
	// JSON is the JSON-family projection report.
	JSON *jsonpkg.ProjectionReport
	// TOML is the TOML-family projection report.
	TOML *toml.ProjectionReport
	// YAML is the YAML-family value-projection report.
	YAML *yaml.ProjectionReport
	// INI is the INI-family projection report.
	INI *ini.ProjectionReport
	// Properties is the Java Properties-family projection report.
	Properties *properties.ProjectionReport
	// XML is the XML-family element-tree projection report.
	XML *xmlpkg.ProjectionReport
	// Plist is the plist-family value-tree projection report.
	Plist *plist.ProjectionReport
	// HCL is the HCL-family body projection report.
	HCL *hclpkg.ProjectionReport
}

// EventCodes returns the frozen semantic-model wire codes of the report
// events in source/operation order. The kind-to-code mapping is the
// frozen conversion externalization (conversion.rs
// json_projection_report_message): JSON structure-reencoded events are
// `json.projection.structure-reencoded@1`, duplicate-collapse events are
// `json.object.duplicate-member@1`, and event kinds without a frozen wire
// code (type-mapped, key-stringified, value-rounded, field-dropped) are
// omitted. TOML projections emit no events; Java Properties events carry
// their own frozen code (`java-properties.projection.duplicate-collapsed@1`).
// The YAML/INI/XML/plist/HCL event kinds have no frozen semantic-model
// wire code (the Rust externalization accepts only empty reports for
// those families) and are omitted; their full facts stay available on the
// retained family report.
func (r ConversionProjectionReport) EventCodes() []string {
	var codes []string
	if r.JSON != nil {
		for _, event := range r.JSON.Events() {
			if code := projectionEventWireCode(event.Kind); code != "" {
				codes = append(codes, code)
			}
		}
	}
	if r.TOML != nil {
		for _, event := range r.TOML.Events() {
			codes = append(codes, event.Code)
		}
	}
	if r.Properties != nil {
		for _, event := range r.Properties.Events() {
			codes = append(codes, event.Code)
		}
	}
	return codes
}

// projectionEventWireCode maps one JSON projection event kind onto its
// frozen semantic-model wire code; kinds without a frozen wire code
// return "".
func projectionEventWireCode(kind jsonpkg.ProjectionEventKind) string {
	switch kind {
	case jsonpkg.ProjectionEventStructureReencoded:
		return "json.projection.structure-reencoded@1"
	case jsonpkg.ProjectionEventDuplicateCollapsed:
		return "json.object.duplicate-member@1"
	default:
		return ""
	}
}

// ConversionProjectionProvenance retains the complete format-owned source
// provenance of the projection stage (conversion.rs:74-93). Exactly one
// family provenance is set, matching the source family.
type ConversionProjectionProvenance struct {
	// JSON is the JSON-family projection provenance.
	JSON *jsonpkg.ProvenanceMap
	// TOML is the TOML-family projection provenance.
	TOML *toml.ProvenanceMap
	// YAML is the YAML-family value-projection provenance.
	YAML *yaml.ProvenanceMap
	// INI is the INI-family projection provenance.
	INI *ini.ProvenanceMap
	// Properties is the Java Properties-family projection provenance.
	Properties *properties.ProvenanceMap
	// XML is the XML-family element-tree projection provenance.
	XML *xmlpkg.ProvenanceMap
	// Plist is the plist-family value-tree projection provenance.
	Plist *plist.ProvenanceMap
	// HCL is the HCL-family body projection provenance.
	HCL *hclpkg.ProvenanceMap
}

// ConversionMaterializationReport retains the complete format-owned
// materialization report of the target stage (conversion.rs
// MaterializationReport). Exactly one family report is set, matching the
// target family.
type ConversionMaterializationReport struct {
	// JSON is the JSON-family materialization report.
	JSON *jsonpkg.MaterializationReport
	// TOML is the TOML-family materialization report.
	TOML *toml.MaterializationReport
	// YAML is the YAML-family materialization report.
	YAML *yaml.MaterializationReport
	// INI is the INI-family materialization report.
	INI *ini.MaterializationReport
	// Properties is the Java Properties-family materialization report.
	Properties *properties.MaterializationReport
	// XML is the XML-family materialization report.
	XML *xmlpkg.MaterializationReport
	// Plist is the plist-family materialization report.
	Plist *plist.MaterializationReport
	// HCL is the HCL-family materialization report.
	HCL *hclpkg.MaterializationReport
}

// EventCodes returns the ordered materialization report event codes. The
// JSON and TOML events carry their own frozen codes; the YAML/INI/
// Properties/XML/plist/HCL events are structured diagnostics whose frozen
// codes are reported directly.
func (r ConversionMaterializationReport) EventCodes() []string {
	var codes []string
	if r.JSON != nil {
		for _, event := range r.JSON.Events() {
			codes = append(codes, event.Code)
		}
	}
	if r.TOML != nil {
		for _, event := range r.TOML.Events() {
			codes = append(codes, event.Code)
		}
	}
	if r.YAML != nil {
		for _, event := range r.YAML.Events() {
			codes = append(codes, event.Code)
		}
	}
	if r.INI != nil {
		for _, event := range r.INI.Events() {
			codes = append(codes, event.Code)
		}
	}
	if r.Properties != nil {
		for _, event := range r.Properties.Events() {
			codes = append(codes, event.Code)
		}
	}
	if r.XML != nil {
		for _, event := range r.XML.Events() {
			codes = append(codes, event.Code)
		}
	}
	if r.Plist != nil {
		for _, event := range r.Plist.Events() {
			codes = append(codes, event.Code)
		}
	}
	if r.HCL != nil {
		for _, event := range r.HCL.Events() {
			codes = append(codes, event.Code)
		}
	}
	return codes
}

// ConversionMaterializationProvenance retains the complete format-owned
// portable-value-to-target-document provenance of the target stage
// (conversion.rs MaterializationProvenanceMap). Exactly one family
// provenance is set, matching the target family. Plist targets retain no
// materialization provenance: the Go plist materializer publishes no
// provenance map (the Rust plist materializer does; documented divergence
// finding of 0.18.0 G4.2).
type ConversionMaterializationProvenance struct {
	// JSON is the JSON-family materialization provenance.
	JSON *jsonpkg.MaterializationProvenanceMap
	// TOML is the TOML-family materialization provenance.
	TOML *toml.MaterializationProvenanceMap
	// YAML is the YAML-family materialization provenance.
	YAML *yaml.MaterializationProvenanceMap
	// INI is the INI-family materialization provenance.
	INI *ini.MaterializationProvenanceMap
	// Properties is the Java Properties-family materialization provenance.
	Properties *properties.MaterializationProvenanceMap
	// XML is the XML-family materialization provenance.
	XML *xmlpkg.MaterializationProvenanceMap
	// HCL is the HCL-family materialization provenance.
	HCL *hclpkg.MaterializationProvenanceMap
}

// ConversionReport is the complete ordered report for both conversion
// stages (conversion.rs:95-149).
type ConversionReport struct {
	projectionFidelity      ConversionFidelity
	projectionReport        ConversionProjectionReport
	materializationFidelity MaterializationFidelity
	materializationReport   ConversionMaterializationReport
	overallFidelity         ConversionFidelity
	sourceProfile           document.ProfileId
	targetProfile           document.ProfileId
}

// ProjectionFidelity returns the projection-stage fidelity.
func (r *ConversionReport) ProjectionFidelity() ConversionFidelity {
	return r.projectionFidelity
}

// ProjectionReport returns the complete format-owned projection report.
func (r *ConversionReport) ProjectionReport() ConversionProjectionReport {
	return r.projectionReport
}

// MaterializationFidelity returns the materialization-stage fidelity.
func (r *ConversionReport) MaterializationFidelity() MaterializationFidelity {
	return r.materializationFidelity
}

// MaterializationReport returns the complete format-owned materialization
// report.
func (r *ConversionReport) MaterializationReport() ConversionMaterializationReport {
	return r.materializationReport
}

// OverallFidelity returns the worst fidelity across both stages.
func (r *ConversionReport) OverallFidelity() ConversionFidelity {
	return r.overallFidelity
}

// SourceProfile returns the exact source profile.
func (r *ConversionReport) SourceProfile() document.ProfileId {
	return r.sourceProfile
}

// TargetProfile returns the exact target profile.
func (r *ConversionReport) TargetProfile() document.ProfileId {
	return r.targetProfile
}

// CompleteConversion is the complete conversion result with both
// provenance directions kept distinct (conversion.rs:151-164).
type CompleteConversion struct {
	// Document is the newly materialized target document.
	Document *Document
	// ProjectedValue is the exact intermediate portable value used
	// between the two stages.
	ProjectedValue core.Value
	// ProjectionProvenance is the source-document-to-portable-value
	// provenance.
	ProjectionProvenance ConversionProjectionProvenance
	// MaterializationProvenance is the portable-value-to-target-document
	// provenance.
	MaterializationProvenance ConversionMaterializationProvenance
	// Report is the complete two-stage report.
	Report ConversionReport
}

// ConversionFailureKind classifies a conversion failure
// (conversion.rs:280-308). No failure carries a partial target document.
type ConversionFailureKind uint8

// The three frozen failure classes.
const (
	// ConversionFailureProjectionFailed: the projection did not produce a
	// complete portable value.
	ConversionFailureProjectionFailed ConversionFailureKind = iota
	// ConversionFailureMaterializationFailed: the materialization did not
	// produce target bytes or a target document.
	ConversionFailureMaterializationFailed
	// ConversionFailureUnauthorizedLoss: a lossy projection event lacked
	// an explicit authorizing source policy.
	ConversionFailureUnauthorizedLoss
)

// ConversionFailure is the typed conversion failure. It implements error
// and the RFC 0016 §6 Code() contract with the frozen registered codes.
// Exactly one payload group is set per Kind.
type ConversionFailure struct {
	// Kind identifies the failure.
	Kind ConversionFailureKind
	// ProjectionReport is the complete stage report produced before a
	// ProjectionFailed failure.
	ProjectionReport ConversionProjectionReport
	// ProjectionDiagnostics are the ordered structured failure
	// diagnostics of a ProjectionFailed failure.
	ProjectionDiagnostics []*protocol.Diagnostic
	// PartialAnalysis are the stable locally analyzed paths of a
	// ProjectionFailed failure.
	PartialAnalysis []string
	// YamlProjectionFailure is the exact YAML-family value projection
	// failure of a ProjectionFailed failure (conversion.rs
	// YamlProjectionFailed): the YAML value projection publishes no
	// partial report, so the failure is retained whole instead of being
	// flattened.
	YamlProjectionFailure *yaml.ValueProjectionFailure
	// MaterializationFailure is the stable target-family failure of a
	// MaterializationFailed failure.
	MaterializationFailure ConversionMaterializationFailure
	// MaterializationReport is the complete stage report produced before
	// a MaterializationFailed failure.
	MaterializationReport ConversionMaterializationReport
	// AnalyzedInputPaths are the stable portable paths analyzed before a
	// MaterializationFailed failure.
	AnalyzedInputPaths []protocol.ValuePath
}

// Error implements error; the text is human presentation only (RFC 0016
// §6).
func (e *ConversionFailure) Error() string {
	switch e.Kind {
	case ConversionFailureProjectionFailed:
		return "consema: conversion projection failed"
	case ConversionFailureMaterializationFailed:
		return "consema: conversion materialization failed"
	case ConversionFailureUnauthorizedLoss:
		return "consema: conversion would lose information without an authorizing policy"
	}
	return "consema: conversion failed"
}

// Code returns the frozen registered code for the failure
// (conversion.rs:310-333).
func (e *ConversionFailure) Code() string {
	switch e.Kind {
	case ConversionFailureProjectionFailed:
		return "core.conversion.projection-failed@1"
	case ConversionFailureMaterializationFailed:
		return "core.conversion.materialization-failed@1"
	case ConversionFailureUnauthorizedLoss:
		return "core.conversion.unauthorized-loss@1"
	}
	return "core.conversion.materialization-failed@1"
}

// ConversionMaterializationFailure is the stable target-family
// materialization failure of a MaterializationFailed conversion. Exactly
// one field is set.
type ConversionMaterializationFailure struct {
	// JSON is the JSON-family materialization failure.
	JSON *jsonpkg.MaterializationFailure
	// TOML is the TOML-family materialization failure.
	TOML *toml.MaterializationFailure
	// YAML is the YAML-family materialization failure.
	YAML *yaml.MaterializationFailure
	// INI is the INI-family materialization failure.
	INI *ini.MaterializationFailure
	// Properties is the Java Properties-family materialization failure.
	Properties *properties.MaterializationFailure
	// XML is the XML-family materialization failure.
	XML *xmlpkg.MaterializationFailure
	// Plist is the plist-family materialization failure.
	Plist *plist.MaterializationFailure
	// HCL is the HCL-family materialization failure.
	HCL *hclpkg.MaterializationFailure
	// UnsupportedProfile reports a target profile that is unknown or
	// belongs to no implemented family
	// (core.materialization.unsupported-profile@1).
	UnsupportedProfile bool
	// InvalidRequestReason is the frozen invalid-request reason of the
	// record-consumption gate: the projected value is one of the
	// published internal record envelopes
	// (`xml.element-tree@1`, `plist.value-tree@1`, `hcl.body@1`) and the
	// target profile belongs to a different family, so the envelope is
	// never presented as a target document
	// (core.materialization.invalid-request@1).
	InvalidRequestReason string
}

// ConversionResult is the conversion completion algebra (conversion.rs:
// 335-342). Exactly one outcome is non-nil; a failure never carries a
// partial target document.
type ConversionResult struct {
	// Complete is the complete target document and all required audit
	// artifacts.
	Complete *CompleteConversion
	// Failed is the failure without a target document or partial target
	// bytes.
	Failed *ConversionFailure
}

// ConvertJSON converts one JSON document by composing its published
// projection and the requested target materializer (conversion.rs
// convert_json). A lossy projection whose lossy events carry no explicit
// authorizing policy rule fails atomically with
// ConversionFailureUnauthorizedLoss before any materialization.
func ConvertJSON(source *jsonpkg.Document, projectionRequest *jsonpkg.ProjectionRequest,
	materializationRequest document.MaterializationRequest) ConversionResult {
	result := source.Project(projectionRequest)
	if result.Failed != nil {
		return ConversionResult{Failed: projectionFailed(result.Failed)}
	}
	projection := result.Complete
	if projection.Fidelity == jsonpkg.FidelityLossy {
		for _, event := range projection.Report.Events() {
			if event.Loss == jsonpkg.FidelityLossy && event.Policy == nil {
				return ConversionResult{Failed: &ConversionFailure{
					Kind: ConversionFailureUnauthorizedLoss,
				}}
			}
		}
	}
	return completeConversion(
		source.Profile(),
		projection.Value,
		jsonConversionFidelity(projection.Fidelity),
		ConversionProjectionReport{JSON: &projection.Report},
		ConversionProjectionProvenance{JSON: &projection.Provenance},
		materializationRequest,
	)
}

// ConvertTOML converts one TOML document by composing its published
// projection and the requested target materializer (conversion.rs
// convert_toml). TOML 1.0 exact projections never emit lossy events, so
// no unauthorized-loss gate applies.
func ConvertTOML(source *toml.Document, projectionRequest toml.ProjectionRequest,
	materializationRequest document.MaterializationRequest) ConversionResult {
	result := source.Project(projectionRequest)
	if result.Failed != nil {
		return ConversionResult{Failed: projectionFailedTOML(result.Failed)}
	}
	projection := result.Complete
	return completeConversion(
		source.Profile(),
		projection.Value,
		tomlConversionFidelity(projection.Fidelity),
		ConversionProjectionReport{TOML: &projection.Report},
		ConversionProjectionProvenance{TOML: &projection.Provenance},
		materializationRequest,
	)
}

// ConvertYAML converts one YAML stream through its explicit PortableValue
// projection (conversion.rs convert_yaml). The default request rejects
// sharing and cycles; both fail atomically with the exact YAML projection
// failure, and conversion never implicitly enables an acyclic
// duplication strategy.
func ConvertYAML(source *yaml.Document, projectionRequest yaml.ValueProjectionRequest,
	materializationRequest document.MaterializationRequest) ConversionResult {
	result := source.ProjectValue(projectionRequest)
	if result.Failed != nil {
		return ConversionResult{Failed: &ConversionFailure{
			Kind:                  ConversionFailureProjectionFailed,
			YamlProjectionFailure: result.Failed,
		}}
	}
	projection := result.Complete
	return completeConversion(
		source.Profile(),
		projection.Value,
		yamlConversionFidelity(projection.Fidelity),
		ConversionProjectionReport{YAML: &projection.Report},
		ConversionProjectionProvenance{YAML: &projection.Provenance},
		materializationRequest,
	)
}

// ConvertINI converts one INI document by composing its explicit
// projection and a target materializer (conversion.rs convert_ini).
func ConvertINI(source *ini.Document, projectionRequest ini.ProjectionRequest,
	materializationRequest document.MaterializationRequest) ConversionResult {
	result := source.Project(projectionRequest)
	if result.Failed != nil {
		return ConversionResult{Failed: projectionFailedINI(result.Failed)}
	}
	projection := result.Complete
	return completeConversion(
		source.Profile(),
		projection.Value,
		iniConversionFidelity(projection.Fidelity),
		ConversionProjectionReport{INI: &projection.Report},
		ConversionProjectionProvenance{INI: &projection.Provenance},
		materializationRequest,
	)
}

// ConvertProperties converts one Java Properties document through an
// explicit duplicate policy (conversion.rs convert_properties).
func ConvertProperties(source *properties.Document, projectionRequest properties.ProjectionRequest,
	materializationRequest document.MaterializationRequest) ConversionResult {
	result := source.Project(projectionRequest)
	if result.Failed != nil {
		return ConversionResult{Failed: projectionFailedProperties(result.Failed)}
	}
	projection := result.Complete
	return completeConversion(
		source.Profile(),
		projection.Value,
		propertiesConversionFidelity(projection.Fidelity),
		ConversionProjectionReport{Properties: &projection.Report},
		ConversionProjectionProvenance{Properties: &projection.Provenance},
		materializationRequest,
	)
}

// ConvertXML converts one XML document by composing its element-tree
// projection and a target materializer (conversion.rs convert_xml).
//
// The XML projection publishes the exact `xml.element-tree@1` record,
// which only the XML materializer family consumes; the record-consumption
// gate rejects the record atomically for every non-XML target instead of
// presenting the internal envelope as a target document. Recovered
// documents never project.
func ConvertXML(source *xmlpkg.Document, projectionRequest xmlpkg.ProjectionRequest,
	materializationRequest document.MaterializationRequest) ConversionResult {
	result := source.Project(projectionRequest)
	if result.Failed != nil {
		return ConversionResult{Failed: projectionFailedXML(result.Failed)}
	}
	projection := result.Complete
	return completeConversion(
		source.Profile(),
		projection.Value,
		xmlConversionFidelity(projection.Fidelity),
		ConversionProjectionReport{XML: &projection.Report},
		ConversionProjectionProvenance{XML: &projection.Provenance},
		materializationRequest,
	)
}

// ConvertPlist converts one Property List document by composing its
// value-tree projection and a target materializer (conversion.rs
// convert_plist).
//
// The plist projection publishes the exact `plist.value-tree@1` record,
// which only the plist materializer family consumes; the
// record-consumption gate rejects the record atomically for every
// non-plist target instead of presenting the internal envelope as a
// target document. Recovered documents never project.
func ConvertPlist(source *plist.Document, projectionRequest plist.ProjectionRequest,
	materializationRequest document.MaterializationRequest) ConversionResult {
	result := plist.Project(source, projectionRequest)
	if result.Failed != nil {
		return ConversionResult{Failed: projectionFailedPlist(result.Failed)}
	}
	projection := result.Complete
	return completeConversion(
		source.Profile(),
		projection.Value,
		plistConversionFidelity(projection.Fidelity),
		ConversionProjectionReport{Plist: &projection.Report},
		ConversionProjectionProvenance{Plist: &projection.Provenance},
		materializationRequest,
	)
}

// ConvertHCL converts one HCL document by composing its body projection
// and a target materializer (conversion.rs convert_hcl).
//
// The HCL projection publishes the exact `hcl.body@1` record, which only
// the HCL materializer family consumes; the record-consumption gate
// rejects the record atomically for every non-HCL target instead of
// presenting the internal envelope as a target document. Recovered
// documents never project.
//
// The exact body target is the default ExpressionPolicyFail: an attribute
// whose expression is derived (a variable reference, traversal, call,
// binary operation, conditional, for-expression, or any template
// containing interpolation or a directive) fails the conversion atomically
// with `hcl.projection.non-literal-expression@1`. Conversion never
// implicitly enables the ProjectExpression strategy; callers that want
// derived expressions projected as `hcl.expression@1` ExtendedValues must
// request that policy explicitly through the projection request (RFC 0014
// §8.2).
func ConvertHCL(source *hclpkg.Document, projectionRequest hclpkg.ProjectionRequest,
	materializationRequest document.MaterializationRequest) ConversionResult {
	result := source.Project(projectionRequest)
	if result.Failed != nil {
		return ConversionResult{Failed: projectionFailedHCL(result.Failed)}
	}
	projection := result.Complete
	return completeConversion(
		source.Profile(),
		projection.Value,
		hclConversionFidelity(projection.Fidelity),
		ConversionProjectionReport{HCL: &projection.Report},
		ConversionProjectionProvenance{HCL: &projection.Provenance},
		materializationRequest,
	)
}

// projectionFailed maps a JSON-family failed projection attempt onto the
// conversion failure without re-declaring the failure facts.
func projectionFailed(attempt *jsonpkg.FailedProjectionAttempt) *ConversionFailure {
	return &ConversionFailure{
		Kind:                  ConversionFailureProjectionFailed,
		ProjectionReport:      ConversionProjectionReport{JSON: &attempt.Report},
		ProjectionDiagnostics: attempt.Diagnostics,
		PartialAnalysis:       attempt.PartialAnalysis,
	}
}

// projectionFailedTOML maps a TOML-family failed projection attempt onto
// the conversion failure.
func projectionFailedTOML(attempt *toml.FailedProjectionAttempt) *ConversionFailure {
	return &ConversionFailure{
		Kind:                  ConversionFailureProjectionFailed,
		ProjectionReport:      ConversionProjectionReport{TOML: &attempt.Report},
		ProjectionDiagnostics: attempt.Diagnostics,
		PartialAnalysis:       attempt.PartialAnalysis,
	}
}

// projectionFailedINI maps an INI-family failed projection attempt onto
// the conversion failure. The INI failed attempt carries no partial
// analysis.
func projectionFailedINI(attempt *ini.FailedProjectionAttempt) *ConversionFailure {
	return &ConversionFailure{
		Kind:                  ConversionFailureProjectionFailed,
		ProjectionReport:      ConversionProjectionReport{INI: &attempt.Report},
		ProjectionDiagnostics: attempt.Diagnostics,
	}
}

// projectionFailedProperties maps a Java Properties-family failed
// projection attempt onto the conversion failure. The Properties failed
// attempt carries no partial analysis.
func projectionFailedProperties(attempt *properties.FailedProjectionAttempt) *ConversionFailure {
	return &ConversionFailure{
		Kind:                  ConversionFailureProjectionFailed,
		ProjectionReport:      ConversionProjectionReport{Properties: &attempt.Report},
		ProjectionDiagnostics: attempt.Diagnostics,
	}
}

// projectionFailedXML maps an XML-family failed projection attempt onto
// the conversion failure. The XML failed attempt carries no partial
// analysis.
func projectionFailedXML(attempt *xmlpkg.FailedProjectionAttempt) *ConversionFailure {
	return &ConversionFailure{
		Kind:                  ConversionFailureProjectionFailed,
		ProjectionReport:      ConversionProjectionReport{XML: &attempt.Report},
		ProjectionDiagnostics: attempt.Diagnostics,
	}
}

// projectionFailedPlist maps a plist-family failed projection attempt
// onto the conversion failure. The plist failed attempt carries no
// partial analysis.
func projectionFailedPlist(attempt *plist.FailedProjectionAttempt) *ConversionFailure {
	return &ConversionFailure{
		Kind:                  ConversionFailureProjectionFailed,
		ProjectionReport:      ConversionProjectionReport{Plist: &attempt.Report},
		ProjectionDiagnostics: attempt.Diagnostics,
	}
}

// projectionFailedHCL maps an HCL-family failed projection attempt onto
// the conversion failure. The HCL failed attempt carries no partial
// analysis.
func projectionFailedHCL(attempt *hclpkg.FailedProjectionAttempt) *ConversionFailure {
	return &ConversionFailure{
		Kind:                  ConversionFailureProjectionFailed,
		ProjectionReport:      ConversionProjectionReport{HCL: &attempt.Report},
		ProjectionDiagnostics: attempt.Diagnostics,
	}
}

// completeConversion runs the record-consumption gate, then the target
// materialization, and assembles the complete conversion with the
// two-stage report (conversion.rs complete_conversion).
func completeConversion(sourceProfile document.ProfileId, projectedValue core.Value,
	projectionFidelity ConversionFidelity, projectionReport ConversionProjectionReport,
	projectionProvenance ConversionProjectionProvenance,
	request document.MaterializationRequest) ConversionResult {
	if failure := validateRecordConsumption(sourceProfile, projectedValue, request); failure != nil {
		return ConversionResult{Failed: failure}
	}
	materialized, failure := materializeTarget(projectedValue, request)
	if failure != nil {
		return ConversionResult{Failed: failure}
	}
	materializationOverall := ConversionFidelityExact
	switch materialized.fidelity {
	case MaterializationFidelityTransformed:
		materializationOverall = ConversionFidelityTransformed
	}
	return ConversionResult{Complete: &CompleteConversion{
		Document:                  materialized.document,
		ProjectedValue:            projectedValue,
		ProjectionProvenance:      projectionProvenance,
		MaterializationProvenance: materialized.provenance,
		Report: ConversionReport{
			projectionFidelity:      projectionFidelity,
			projectionReport:        projectionReport,
			materializationFidelity: materialized.fidelity,
			materializationReport:   materialized.materializationReport,
			overallFidelity:         maxConversionFidelity(projectionFidelity, materializationOverall),
			sourceProfile:           sourceProfile,
			targetProfile:           request.TargetProfile(),
		},
	}}
}

// materializedTarget is the complete target-stage outcome.
type materializedTarget struct {
	document              *Document
	fidelity              MaterializationFidelity
	materializationReport ConversionMaterializationReport
	provenance            ConversionMaterializationProvenance
}

// materializeTarget dispatches the intermediate portable value to the
// materializer of the target profile's family (conversion.rs
// materialize_target). Unknown target profiles fail atomically with the
// unsupported-profile vocabulary; the target document never exists on
// failure.
func materializeTarget(value core.Value,
	request document.MaterializationRequest) (*materializedTarget, *ConversionFailure) {
	switch request.TargetProfile().ID() {
	case "json.strict", "jsonc.bounded", "json5.standard":
		result := jsonpkg.Materialize(value, request)
		if result.Complete != nil {
			return &materializedTarget{
				document: &Document{inner: documentInner{json: result.Complete.Document}},
				fidelity: jsonMaterializationFidelity(result.Complete.Fidelity),
				materializationReport: ConversionMaterializationReport{
					JSON: &result.Complete.Report,
				},
				provenance: ConversionMaterializationProvenance{
					JSON: &result.Complete.Provenance,
				},
			}, nil
		}
		return nil, materializationFailed(&ConversionMaterializationFailure{
			JSON: result.Failed.Failure,
		}, ConversionMaterializationReport{JSON: &result.Failed.Report},
			result.Failed.AnalyzedInputPaths)
	case "toml.1.0":
		result := toml.Materialize(value, request)
		if result.Complete != nil {
			return &materializedTarget{
				document: &Document{inner: documentInner{toml: result.Complete.Document}},
				fidelity: tomlMaterializationFidelity(result.Complete.Fidelity),
				materializationReport: ConversionMaterializationReport{
					TOML: &result.Complete.Report,
				},
				provenance: ConversionMaterializationProvenance{
					TOML: &result.Complete.Provenance,
				},
			}, nil
		}
		return nil, materializationFailed(&ConversionMaterializationFailure{
			TOML: &result.Failed.Failure,
		}, ConversionMaterializationReport{TOML: &result.Failed.Report},
			result.Failed.AnalyzedInputPaths)
	case "yaml.1.2-core", "yaml.1.1-compat":
		result := yaml.MaterializeValue(value, request)
		if result.Complete != nil {
			return &materializedTarget{
				document: &Document{inner: documentInner{yaml: result.Complete.Document}},
				fidelity: yamlMaterializationFidelity(result.Complete.Fidelity),
				materializationReport: ConversionMaterializationReport{
					YAML: &result.Complete.Report,
				},
				provenance: ConversionMaterializationProvenance{
					YAML: &result.Complete.Provenance,
				},
			}, nil
		}
		return nil, materializationFailed(&ConversionMaterializationFailure{
			YAML: &result.Failed.Failure,
		}, ConversionMaterializationReport{YAML: &result.Failed.Report},
			result.Failed.AnalyzedInputPaths)
	case "ini.portable", "ini.windows", "ini.python-configparser":
		result := ini.Materialize(value, request)
		if result.Complete != nil {
			return &materializedTarget{
				document: &Document{inner: documentInner{ini: result.Complete.Document}},
				fidelity: iniMaterializationFidelity(result.Complete.Fidelity),
				materializationReport: ConversionMaterializationReport{
					INI: &result.Complete.Report,
				},
				provenance: ConversionMaterializationProvenance{
					INI: &result.Complete.Provenance,
				},
			}, nil
		}
		return nil, materializationFailed(&ConversionMaterializationFailure{
			INI: &result.Failed.Failure,
		}, ConversionMaterializationReport{INI: &result.Failed.Report},
			result.Failed.AnalyzedInputPaths)
	case "java-properties.reader", "java-properties.latin1":
		result := properties.Materialize(value, request)
		if result.Complete != nil {
			return &materializedTarget{
				document: &Document{inner: documentInner{properties: result.Complete.Document}},
				fidelity: propertiesMaterializationFidelity(result.Complete.Fidelity),
				materializationReport: ConversionMaterializationReport{
					Properties: &result.Complete.Report,
				},
				provenance: ConversionMaterializationProvenance{
					Properties: &result.Complete.Provenance,
				},
			}, nil
		}
		return nil, materializationFailed(&ConversionMaterializationFailure{
			Properties: &result.Failed.Failure,
		}, ConversionMaterializationReport{Properties: &result.Failed.Report},
			result.Failed.AnalyzedInputPaths)
	case "xml.1.0-safe":
		result := xmlpkg.Materialize(value, request)
		if result.Complete != nil {
			return &materializedTarget{
				document: &Document{inner: documentInner{xml: result.Complete.Document}},
				fidelity: xmlMaterializationFidelity(result.Complete.Fidelity),
				materializationReport: ConversionMaterializationReport{
					XML: &result.Complete.Report,
				},
				provenance: ConversionMaterializationProvenance{
					XML: &result.Complete.Provenance,
				},
			}, nil
		}
		return nil, materializationFailed(&ConversionMaterializationFailure{
			XML: result.Failed.Failure,
		}, ConversionMaterializationReport{XML: &result.Failed.Report},
			result.Failed.AnalyzedInputPaths)
	case "plist.xml", "plist.binary":
		result := plist.Materialize(value, request)
		if result.Complete != nil {
			// The Go plist materializer publishes no provenance map
			// (documented divergence finding of 0.18.0 G4.2).
			return &materializedTarget{
				document: &Document{inner: documentInner{plist: result.Complete.Document}},
				fidelity: plistMaterializationFidelity(result.Complete.Fidelity),
				materializationReport: ConversionMaterializationReport{
					Plist: &result.Complete.Report,
				},
			}, nil
		}
		return nil, materializationFailed(&ConversionMaterializationFailure{
			Plist: result.Failed.Failure,
		}, ConversionMaterializationReport{Plist: &result.Failed.Report},
			result.Failed.AnalyzedInputPaths)
	case "hcl.native", "hcl.tfvars":
		result := hclpkg.Materialize(value, request)
		if result.Complete != nil {
			return &materializedTarget{
				document: &Document{inner: documentInner{hcl: result.Complete.Document}},
				fidelity: hclMaterializationFidelity(result.Complete.Fidelity),
				materializationReport: ConversionMaterializationReport{
					HCL: &result.Complete.Report,
				},
				provenance: ConversionMaterializationProvenance{
					HCL: &result.Complete.Provenance,
				},
			}, nil
		}
		return nil, materializationFailed(&ConversionMaterializationFailure{
			HCL: result.Failed.Failure,
		}, ConversionMaterializationReport{HCL: &result.Failed.Report},
			result.Failed.AnalyzedInputPaths)
	default:
		return nil, materializationFailed(&ConversionMaterializationFailure{
			UnsupportedProfile: true,
		}, ConversionMaterializationReport{}, nil)
	}
}

// materializationFailed assembles a MaterializationFailed conversion
// failure with its complete stage report.
func materializationFailed(failure *ConversionMaterializationFailure,
	report ConversionMaterializationReport, analyzed []protocol.ValuePath) *ConversionFailure {
	return &ConversionFailure{
		Kind:                   ConversionFailureMaterializationFailed,
		MaterializationFailure: *failure,
		MaterializationReport:  report,
		AnalyzedInputPaths:     analyzed,
	}
}

// Published record envelope ids produced by the record-format projections
// (RFC 0012 §9, RFC 0013 §9, RFC 0014 §8.2). `hcl.expression@1` is nested
// inside body items and is never the projected root.
const xmlElementTreeRecord = "xml.element-tree@1"
const plistValueTreeRecord = "plist.value-tree@1"
const hclBodyRecord = "hcl.body@1"

// formatFamily returns the format family of one profile id; unknown
// profiles return "".
func formatFamily(profileID string) string {
	switch profileID {
	case "json.strict", "jsonc.bounded", "json5.standard":
		return "json"
	case "toml.1.0":
		return "toml"
	case "yaml.1.2-core", "yaml.1.1-compat":
		return "yaml"
	case "ini.portable", "ini.windows", "ini.python-configparser":
		return "ini"
	case "java-properties.reader", "java-properties.latin1":
		return "properties"
	case "xml.1.0-safe":
		return "xml"
	case "plist.xml", "plist.binary":
		return "plist"
	case "hcl.native", "hcl.tfvars":
		return "hcl"
	}
	return ""
}

// publishedRecord returns one published Consema format record envelope id
// (conversion.rs published_record) when the value is an object whose
// `record` member equals a published versioned record id; any other
// object is ordinary content.
func publishedRecord(value core.Value) (string, bool) {
	object, ok := value.(*core.Object)
	if !ok {
		return "", false
	}
	record, ok := object.Get("record")
	if !ok {
		return "", false
	}
	id, ok := record.(core.String)
	if !ok {
		return "", false
	}
	switch string(id) {
	case xmlElementTreeRecord, plistValueTreeRecord, hclBodyRecord:
		return string(id), true
	}
	return "", false
}

// recordFamily returns the owning format family of one published record
// id.
func recordFamily(record string) string {
	switch record {
	case xmlElementTreeRecord:
		return "xml"
	case plistValueTreeRecord:
		return "plist"
	case hclBodyRecord:
		return "hcl"
	}
	return ""
}

// recordFamilyMessage is the exact invalid-request reason for one
// published record id.
func recordFamilyMessage(record string) string {
	switch record {
	case xmlElementTreeRecord:
		return "the projected value is the xml.element-tree@1 internal record; " +
			"only the xml family materializer consumes it"
	case plistValueTreeRecord:
		return "the projected value is the plist.value-tree@1 internal record; " +
			"only the plist family materializer consumes it"
	case hclBodyRecord:
		return "the projected value is the hcl.body@1 internal record; " +
			"only the hcl family materializer consumes it"
	}
	return "the projected value is an internal format record; " +
		"only its owning format family materializer consumes it"
}

// validateRecordConsumption is the record-consumption gate of the
// composition (conversion.rs validate_record_consumption; module docs).
//
// A record-format source (XML, plist, HCL) projects its versioned
// internal record envelope; the envelope is consumed only by the owning
// format family's materializer. When the target profile belongs to a
// different family, the conversion fails atomically with the shared
// invalid-request vocabulary instead of presenting the envelope as a
// target document. Baseline sources never project envelopes — a
// `"record"` member in their content is content — and the explicit
// non-record projection targets of the record formats publish plain
// values, so both pass the gate untouched.
func validateRecordConsumption(sourceProfile document.ProfileId, value core.Value,
	request document.MaterializationRequest) *ConversionFailure {
	sourceFamily := formatFamily(sourceProfile.ID())
	if sourceFamily != "xml" && sourceFamily != "plist" && sourceFamily != "hcl" {
		return nil
	}
	record, ok := publishedRecord(value)
	if !ok {
		return nil
	}
	if recordFamily(record) == formatFamily(request.TargetProfile().ID()) {
		return nil
	}
	return materializationFailed(&ConversionMaterializationFailure{
		InvalidRequestReason: recordFamilyMessage(record),
	}, ConversionMaterializationReport{}, nil)
}

// maxConversionFidelity returns the worse fidelity (Exact < Transformed
// < Lossy).
func maxConversionFidelity(left, right ConversionFidelity) ConversionFidelity {
	if left > right {
		return left
	}
	return right
}

func jsonConversionFidelity(fidelity jsonpkg.Fidelity) ConversionFidelity {
	switch fidelity {
	case jsonpkg.FidelityExact:
		return ConversionFidelityExact
	case jsonpkg.FidelityTransformed:
		return ConversionFidelityTransformed
	case jsonpkg.FidelityLossy:
		return ConversionFidelityLossy
	}
	return ConversionFidelityExact
}

func tomlConversionFidelity(fidelity toml.Fidelity) ConversionFidelity {
	switch fidelity {
	case toml.FidelityExact:
		return ConversionFidelityExact
	case toml.FidelityTransformed:
		return ConversionFidelityTransformed
	case toml.FidelityLossy:
		return ConversionFidelityLossy
	}
	return ConversionFidelityExact
}

func jsonMaterializationFidelity(fidelity jsonpkg.MaterializationFidelity) MaterializationFidelity {
	switch fidelity {
	case jsonpkg.MaterializationFidelityExact:
		return MaterializationFidelityExact
	case jsonpkg.MaterializationFidelityTransformed:
		return MaterializationFidelityTransformed
	}
	return MaterializationFidelityExact
}

func tomlMaterializationFidelity(fidelity toml.MaterializationFidelity) MaterializationFidelity {
	switch fidelity {
	case toml.MaterializationFidelityExact:
		return MaterializationFidelityExact
	case toml.MaterializationFidelityTransformed:
		return MaterializationFidelityTransformed
	}
	return MaterializationFidelityExact
}

func yamlConversionFidelity(fidelity yaml.Fidelity) ConversionFidelity {
	switch fidelity {
	case yaml.FidelityExact:
		return ConversionFidelityExact
	case yaml.FidelityTransformed:
		return ConversionFidelityTransformed
	case yaml.FidelityLossy:
		return ConversionFidelityLossy
	}
	return ConversionFidelityExact
}

func iniConversionFidelity(fidelity ini.Fidelity) ConversionFidelity {
	switch fidelity {
	case ini.FidelityExact:
		return ConversionFidelityExact
	case ini.FidelityTransformed:
		return ConversionFidelityTransformed
	case ini.FidelityLossy:
		return ConversionFidelityLossy
	}
	return ConversionFidelityExact
}

func propertiesConversionFidelity(fidelity properties.Fidelity) ConversionFidelity {
	switch fidelity {
	case properties.FidelityExact:
		return ConversionFidelityExact
	case properties.FidelityTransformed:
		return ConversionFidelityTransformed
	case properties.FidelityLossy:
		return ConversionFidelityLossy
	}
	return ConversionFidelityExact
}

func xmlConversionFidelity(fidelity xmlpkg.Fidelity) ConversionFidelity {
	switch fidelity {
	case xmlpkg.FidelityExact:
		return ConversionFidelityExact
	case xmlpkg.FidelityTransformed:
		return ConversionFidelityTransformed
	case xmlpkg.FidelityLossy:
		return ConversionFidelityLossy
	}
	return ConversionFidelityExact
}

func plistConversionFidelity(fidelity plist.Fidelity) ConversionFidelity {
	switch fidelity {
	case plist.FidelityExact:
		return ConversionFidelityExact
	case plist.FidelityTransformed:
		return ConversionFidelityTransformed
	case plist.FidelityLossy:
		return ConversionFidelityLossy
	}
	return ConversionFidelityExact
}

func hclConversionFidelity(fidelity hclpkg.Fidelity) ConversionFidelity {
	switch fidelity {
	case hclpkg.FidelityExact:
		return ConversionFidelityExact
	case hclpkg.FidelityTransformed:
		return ConversionFidelityTransformed
	case hclpkg.FidelityLossy:
		return ConversionFidelityLossy
	}
	return ConversionFidelityExact
}

func yamlMaterializationFidelity(fidelity yaml.MaterializationFidelity) MaterializationFidelity {
	switch fidelity {
	case yaml.MaterializationFidelityExact:
		return MaterializationFidelityExact
	case yaml.MaterializationFidelityTransformed:
		return MaterializationFidelityTransformed
	}
	return MaterializationFidelityExact
}

func iniMaterializationFidelity(fidelity ini.MaterializationFidelity) MaterializationFidelity {
	switch fidelity {
	case ini.MaterializationFidelityExact:
		return MaterializationFidelityExact
	case ini.MaterializationFidelityTransformed:
		return MaterializationFidelityTransformed
	}
	return MaterializationFidelityExact
}

func propertiesMaterializationFidelity(fidelity properties.MaterializationFidelity) MaterializationFidelity {
	switch fidelity {
	case properties.MaterializationFidelityExact:
		return MaterializationFidelityExact
	case properties.MaterializationFidelityTransformed:
		return MaterializationFidelityTransformed
	}
	return MaterializationFidelityExact
}

func xmlMaterializationFidelity(fidelity xmlpkg.MaterializationFidelity) MaterializationFidelity {
	switch fidelity {
	case xmlpkg.MaterializationFidelityExact:
		return MaterializationFidelityExact
	case xmlpkg.MaterializationFidelityTransformed:
		return MaterializationFidelityTransformed
	}
	return MaterializationFidelityExact
}

func plistMaterializationFidelity(fidelity plist.MaterializationFidelity) MaterializationFidelity {
	switch fidelity {
	case plist.MaterializationFidelityExact:
		return MaterializationFidelityExact
	case plist.MaterializationFidelityTransformed:
		return MaterializationFidelityTransformed
	}
	return MaterializationFidelityExact
}

func hclMaterializationFidelity(fidelity hclpkg.MaterializationFidelity) MaterializationFidelity {
	switch fidelity {
	case hclpkg.MaterializationFidelityExact:
		return MaterializationFidelityExact
	case hclpkg.MaterializationFidelityTransformed:
		return MaterializationFidelityTransformed
	}
	return MaterializationFidelityExact
}
