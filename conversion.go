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
// document. The JSON and TOML families are baseline formats that project
// plain portable values; the record-consumption gate of the record
// formats (xml.element-tree@1, plist.value-tree@1, hcl.body@1 — RFC 0012
// §9, RFC 0013 §9, RFC 0014 §8.2) lands with those families (0.17.0-
// 0.18.0), because no Go record-format projection exists to publish an
// envelope. Target profiles of the not-yet-implemented families fail
// atomically with the unsupported-profile vocabulary; nothing is
// invented.

import (
	"consema.dev/consema/core"
	"consema.dev/consema/document"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/protocol"
	"consema.dev/consema/toml"
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
// of JSON or TOML is set, matching the source family.
type ConversionProjectionReport struct {
	// JSON is the JSON-family projection report.
	JSON *jsonpkg.ProjectionReport
	// TOML is the TOML-family projection report.
	TOML *toml.ProjectionReport
}

// EventCodes returns the frozen semantic-model wire codes of the report
// events in source/operation order. The kind-to-code mapping is the
// frozen conversion externalization (conversion.rs
// json_projection_report_message): JSON structure-reencoded events are
// `json.projection.structure-reencoded@1`, duplicate-collapse events are
// `json.object.duplicate-member@1`, and event kinds without a frozen wire
// code (type-mapped, key-stringified, value-rounded, field-dropped) are
// omitted. TOML projections emit no events.
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
// of JSON or TOML is set, matching the source family.
type ConversionProjectionProvenance struct {
	// JSON is the JSON-family projection provenance.
	JSON *jsonpkg.ProvenanceMap
	// TOML is the TOML-family projection provenance.
	TOML *toml.ProvenanceMap
}

// ConversionMaterializationReport retains the complete format-owned
// materialization report of the target stage (conversion.rs
// MaterializationReport). Exactly one of JSON or TOML is set, matching
// the target family.
type ConversionMaterializationReport struct {
	// JSON is the JSON-family materialization report.
	JSON *jsonpkg.MaterializationReport
	// TOML is the TOML-family materialization report.
	TOML *toml.MaterializationReport
}

// EventCodes returns the ordered materialization report event codes.
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
	return codes
}

// ConversionMaterializationProvenance retains the complete format-owned
// portable-value-to-target-document provenance of the target stage
// (conversion.rs MaterializationProvenanceMap). Exactly one of JSON or
// TOML is set, matching the target family.
type ConversionMaterializationProvenance struct {
	// JSON is the JSON-family materialization provenance.
	JSON *jsonpkg.MaterializationProvenanceMap
	// TOML is the TOML-family materialization provenance.
	TOML *toml.MaterializationProvenanceMap
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
	// UnsupportedProfile reports a target profile that is unknown or
	// belongs to a family this Go milestone has not implemented
	// (core.materialization.unsupported-profile@1).
	UnsupportedProfile bool
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

// completeConversion runs the target materialization and assembles the
// complete conversion with the two-stage report (conversion.rs
// complete_conversion).
func completeConversion(sourceProfile document.ProfileId, projectedValue core.Value,
	projectionFidelity ConversionFidelity, projectionReport ConversionProjectionReport,
	projectionProvenance ConversionProjectionProvenance,
	request document.MaterializationRequest) ConversionResult {
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
// materialize_target). Target profiles of the not-yet-implemented
// families fail atomically with the unsupported-profile vocabulary; the
// target document never exists on failure.
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
