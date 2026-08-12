package main

// `consema materialize`: parse the source under the request profile, project
// it under the source format's default exact projection, and materialize the
// projected value under the request's `core.materialization-request@2`
// record (RFC 0015 §6.1).
//
// The machine payload is `core.materialization-result@2`: Complete embeds
// the verified target `core.source-snapshot@2` plus fidelity, report, and
// provenance; Failed carries the stable failure, report, and analyzed input
// paths (RFC 0015 §6.1). In human mode the materialized bytes are written to
// stdout (the command's result data); under `--json` the bytes live inside
// the envelope snapshot and stdout carries exactly the one envelope line
// (RFC 0015 §3.3).
//
// This build never writes files: `--output` is refused as a usage error
// until the CLI wires file output for this command (hard gate 4). The
// facade's record-consumption gate is re-checked here: a record-format
// source projects a versioned internal record that only its owning family's
// materializer consumes, and presenting it to a foreign family would be an
// internal record dump.

import (
	"fmt"
	"io"

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
	yamlpkg "consema.dev/consema/yaml"
)

// runMaterialize runs `consema materialize` (request from --request-file or
// stdin).
func runMaterialize(parsed *ParsedArgs, stdout, stderr io.Writer) uint8 {
	if parsed.output != nil {
		error := usageFlowError("cli.usage.invalid-argument@1",
			"flag '--output' is not available in this build: materialize writes only to "+
				"stdout (file writing lands with the CLI fsio milestone)")
		return emitFailure(protocol.CommandMaterialize, parsed, error, nil, stdout, stderr)
	}
	request, err := readRequestBytes(parsed)
	if err != nil {
		return emitFailure(protocol.CommandMaterialize, parsed, err, nil, stdout, stderr)
	}
	return runMaterializeWithRequest(parsed, request, stdout, stderr)
}

// runMaterializeWithRequest runs `consema materialize` against already-read
// request bytes.
func runMaterializeWithRequest(parsed *ParsedArgs, request []byte,
	stdout, stderr io.Writer) uint8 {
	if parsed.output != nil {
		error := usageFlowError("cli.usage.invalid-argument@1",
			"flag '--output' is not available in this build: materialize writes only to "+
				"stdout (file writing lands with the CLI fsio milestone)")
		return emitFailure(protocol.CommandMaterialize, parsed, error, nil, stdout, stderr)
	}
	// The presentation redaction policy of RFC 0015 §11 (an invalid
	// `--redact-keys` pattern is a usage failure, like plan/apply/edit).
	policy, policyErr := compileRedactPolicy(parsed)
	if policyErr != nil {
		return emitFailure(protocol.CommandMaterialize, parsed, policyErr, nil, stdout, stderr)
	}
	input, err := decodeRequest(request, parsed, "core.materialization-request@2")
	if err != nil {
		return emitFailure(protocol.CommandMaterialize, parsed, err, policy, stdout, stderr)
	}
	payload, targetBytes, err := executeMaterialize(input)
	if err != nil {
		return emitFailure(protocol.CommandMaterialize, parsed, err, policy, stdout, stderr)
	}
	if parsed.json {
		if emitErr := emitCommandEnvelope(protocol.CommandMaterialize, protocol.ExitSuccess,
			payload, nil, parsed, policy, stdout); emitErr != nil {
			return internalFailure("materialize", emitErr.Error(), stderr)
		}
		return protocol.ExitSuccess.ExitCode()
	}
	if _, writeErr := stdout.Write(targetBytes); writeErr != nil {
		return internalFailure("materialize", writeErr.Error(), stderr)
	}
	return protocol.ExitSuccess.ExitCode()
}

// materializeOutcome is one materialization outcome with the facts the
// payload record needs.
type materializeOutcome struct {
	// TargetSnapshot is the verified immutable target source snapshot.
	TargetSnapshot *document.SourceSnapshot
	// Fidelity is the whole-operation semantic fidelity ("Exact" or
	// "Transformed").
	Fidelity string
	// Report holds the ordered materialization events.
	Report []*protocol.Diagnostic
	// Rendered are the rendered target bytes (human result data).
	Rendered []byte
}

// materializeFailure is the stable failure form of one materialization
// attempt.
type materializeFailure struct {
	// WireFailure is the failed-form failure record (the
	// core.materialization-failure@1-style record of the Failed outcome).
	WireFailure core.Value
	// Code is the frozen registered failure code.
	Code string
	// Report holds the events discovered before the failure.
	Report []*protocol.Diagnostic
	// AnalyzedInputPaths are the stable input paths analyzed before the
	// failure.
	AnalyzedInputPaths []protocol.ValuePath
}

// executeMaterialize executes one materialization: parse -> default
// projection -> materialize. Returns the `core.materialization-result@2`
// payload and the rendered target bytes.
func executeMaterialize(input *RequestInput) (core.Value, []byte, *FlowError) {
	message, err := (&protocol.MaterializationRequestMessageV2{}).FromValue(input.Payload)
	if err != nil {
		return nil, nil, protocolFlowError(err)
	}
	request := message.Request()
	sourceFamily := formatFamily(input.Profile.ID())
	if sourceFamily == "" {
		return nil, nil, newFlowError("cli.data.invalid-request@1",
			"profile '"+input.Profile.ID()+"' has no format family")
	}
	doc, flowErr := parseDocument(input.Source, &input.Profile)
	if flowErr != nil {
		return nil, nil, flowErr
	}
	if flowErr := requireComplete(doc, input.SourceLabel); flowErr != nil {
		return nil, nil, flowErr
	}
	value, flowErr := projectValue(doc, mustDefaultProjection(sourceFamily))
	if flowErr != nil {
		return nil, nil, flowErr
	}
	// The record-consumption gate: a record-format source projects a
	// versioned internal record consumed only by its owning family's
	// materializer.
	targetProfile := request.TargetProfile()
	targetFamily := formatFamily(targetProfile.ID())
	if targetFamily == "" {
		return nil, nil, newFlowError("cli.data.invalid-request@1",
			fmt.Sprintf("target profile '%s' has no format family",
				targetProfile.ID()))
	}
	if recordFamily, isRecord := publishedRecord(value); isRecord {
		if recordFamily != targetFamily {
			failure := materializeFailure{
				WireFailure: failureRecordValue("InvalidRequest",
					"core.materialization.invalid-request@1",
					"the projected value is a versioned internal record that only its "+
						"owning format family's materializer consumes"),
				Code: "core.materialization.invalid-request@1",
			}
			return nil, nil, failedMaterializationFlowError(request, failure,
				"materialization failed", input.SourceLabel)
		}
	}
	targetRequest := wireRequestToDocument(request)
	sourceLabel := input.SourceLabel
	outcome, failure := materializeValue(value, targetRequest, targetFamily)
	if failure != nil {
		return nil, nil, failedMaterializationFlowError(request, *failure,
			"materialization failed", sourceLabel)
	}
	// The target snapshot is rebuilt from the verified rendered bytes under
	// the target document's encoding facts (the protocol SourceSnapshot
	// wire record; RFC 0015 §3.3 — snapshot identities never reach the
	// wire).
	protocolSnapshot, err := protocol.NewSourceSnapshotFromRaw(outcome.Rendered,
		documentEncodingRequest(outcome.TargetSnapshot),
		protocol.DefaultSourceLimits())
	if err != nil {
		return nil, nil, newFlowError("core.protocol.invalid-value@1",
			"target snapshot encoding failed: "+err.Error())
	}
	report, err := protocol.NewMaterializationReportMessageWithRegistry(outcome.Report,
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		return nil, nil, newFlowError("core.protocol.invalid-value@1",
			"materialization report externalization failed: "+err.Error())
	}
	provenance, err := protocol.NewMaterializationProvenanceMapMessage(nil)
	if err != nil {
		return nil, nil, newFlowError("core.protocol.invalid-value@1",
			"materialization provenance externalization failed: "+err.Error())
	}
	payload, err := protocol.NewMaterializationResultMessageV2Complete(
		targetProfile, sourceLabel, protocolSnapshot, outcome.Fidelity, report, provenance)
	if err != nil {
		return nil, nil, newFlowError("core.protocol.invalid-value@1",
			"materialization result encoding failed: "+err.Error())
	}
	payloadValue, err := payload.ToValue()
	if err != nil {
		return nil, nil, newFlowError("core.protocol.invalid-value@1",
			"materialization result encoding failed: "+err.Error())
	}
	return payloadValue, outcome.Rendered, nil
}

// wireRequestToDocument maps one strict wire materialization request onto
// the document-layer request (RFC 0016 §5.2; the wire spellings are the
// document spellings).
func wireRequestToDocument(request *protocol.MaterializationRequest) document.MaterializationRequest {
	targetProfile := request.TargetProfile()
	converted := document.NewMaterializationRequest(
		document.NewProfileId(targetProfile.ID(), targetProfile.Version()),
		document.NewMaterializationStyleId(request.StyleID(), request.StyleVersion()))
	if encoding := request.Encoding(); encoding != nil {
		converted = converted.WithEncoding(wireEncodingToDocument(*encoding))
	}
	converted = converted.WithNewline(document.NewlinePolicy(request.Newline()))
	converted = converted.WithMappingPolicy(document.MappingPolicy(request.MappingPolicy()))
	converted = converted.WithLimits(document.MaterializationLimits{
		MaxInputNodes:        request.Limits().MaxInputNodes,
		MaxOutputBytes:       request.Limits().MaxOutputBytes,
		MaxDepth:             request.Limits().MaxDepth,
		MaxReportEntries:     request.Limits().MaxReportEntries,
		MaxProvenanceEntries: request.Limits().MaxProvenanceEntries,
	})
	return converted
}

func wireEncodingToDocument(encoding protocol.SourceEncoding) document.SourceEncoding {
	switch encoding.Kind {
	case "Binary":
		return document.BinaryEncoding()
	case "Utf8":
		return document.Utf8Encoding()
	case "Utf16Le":
		return document.Utf16LeEncoding()
	case "Utf16Be":
		return document.Utf16BeEncoding()
	case "Latin1":
		return document.Latin1Encoding()
	}
	return document.Utf8Encoding()
}

// documentEncodingRequest rebuilds the encoding-resolution request from one
// target snapshot's encoding facts (the resolution request that produced
// those facts).
func documentEncodingRequest(snapshot *document.SourceSnapshot) protocol.EncodingRequest {
	facts := snapshot.EncodingFacts()
	request := protocol.NewEncodingRequest(documentEncodingToProtocol(facts.ProfileDefault()))
	if declaration := facts.Declaration(); declaration != nil {
		request = request.WithDeclaration(documentEncodingToProtocol(*declaration))
	}
	if override := facts.CallerOverride(); override != nil {
		request = request.WithCallerOverride(documentEncodingToProtocol(*override))
	}
	switch facts.BomPolicy() {
	case document.BomPolicyTreatAsContent:
		request = request.WithBomPolicy(protocol.BomPolicyTreatAsContent)
	default:
		request = request.WithBomPolicy(protocol.BomPolicyDetectUnicode)
	}
	return request
}

func documentEncodingToProtocol(encoding document.SourceEncoding) *protocol.SourceEncoding {
	var codePage *uint32
	if page, ok := encoding.WindowsCodePage(); ok {
		number := uint32(page)
		codePage = &number
	}
	return &protocol.SourceEncoding{Kind: encodingKindToWire(encoding.Kind()),
		WindowsCodePage: codePage}
}

func encodingKindToWire(kind document.SourceEncodingKind) string {
	switch kind {
	case document.EncodingBinary:
		return "Binary"
	case document.EncodingUtf8:
		return "Utf8"
	case document.EncodingUtf16Le:
		return "Utf16Le"
	case document.EncodingUtf16Be:
		return "Utf16Be"
	case document.EncodingLatin1:
		return "Latin1"
	case document.EncodingWindowsCodePage:
		return "WindowsCodePage"
	}
	return "Binary"
}

// failedMaterializationFlowError builds the data-class failure of a failed
// materialization with the failed `core.materialization-result@2` record as
// payload.
func failedMaterializationFlowError(request *protocol.MaterializationRequest,
	failure materializeFailure, context, sourceLabel string) *FlowError {
	targetProfile := request.TargetProfile()
	report, reportErr := protocol.NewMaterializationReportMessageWithRegistry(failure.Report,
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	paths := make([]core.Value, 0, len(failure.AnalyzedInputPaths))
	for _, path := range failure.AnalyzedInputPaths {
		paths = append(paths, valuePathValue(path))
	}
	var reportValue core.Value = core.NullValue()
	if reportErr == nil {
		reportValue, _ = report.ToValue()
	}
	outcome, _ := core.NewObject(
		core.Entry{Key: "kind", Value: core.String("Failed")},
		core.Entry{Key: "failure", Value: failure.WireFailure},
		core.Entry{Key: "report", Value: reportValue},
		core.Entry{Key: "analyzed_input_paths", Value: core.NewArray(paths...)},
	)
	payload, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.materialization-result@2")},
		core.Entry{Key: "target_profile", Value: referenceValue(
			targetProfile.ID(), targetProfile.Version())},
		core.Entry{Key: "outcome", Value: outcome},
	)
	flowError := newFlowError(failure.Code, context+" ("+failure.Code+")")
	return flowError.withPayload(payload)
}

// valuePathValue encodes one ValuePath as the wire `{segments: [...]}`
// record (the protocol path_value encoding).
func valuePathValue(path protocol.ValuePath) core.Value {
	segments := make([]core.Value, 0, len(path.Segments()))
	for _, segment := range path.Segments() {
		var record core.Value
		switch segment.Kind {
		case "ObjectValue":
			record, _ = core.NewObject(
				core.Entry{Key: "kind", Value: core.String("ObjectValue")},
				core.Entry{Key: "key", Value: core.String(segment.Key)},
			)
		default:
			record, _ = core.NewObject(
				core.Entry{Key: "kind", Value: core.String(segment.Kind)},
				core.Entry{Key: "index", Value: integerValueOf(segment.Index)},
			)
		}
		segments = append(segments, record)
	}
	value, _ := core.NewObject(core.Entry{Key: "segments", Value: core.NewArray(segments...)})
	return value
}

// failureRecordValue builds one wire failure record (the
// core.materialization-failure@1 record shape of the Failed outcome).
func failureRecordValue(kind, code string, detail string) core.Value {
	entries := []core.Entry{
		{Key: "kind", Value: core.String(kind)},
		{Key: "code", Value: core.String(code)},
	}
	if detail != "" {
		entries = append(entries, core.Entry{Key: "detail", Value: core.String(detail)})
	}
	value, _ := core.NewObject(entries...)
	return value
}

func unrepresentableFailureValue(code string, path protocol.ValuePath,
	valueKind string) core.Value {
	value, _ := core.NewObject(
		core.Entry{Key: "kind", Value: core.String("Unrepresentable")},
		core.Entry{Key: "code", Value: core.String(code)},
		core.Entry{Key: "path", Value: valuePathValue(path)},
		core.Entry{Key: "value_kind", Value: core.String(valueKind)},
	)
	return value
}

func resourceLimitFailureValue(code, limit string) core.Value {
	value, _ := core.NewObject(
		core.Entry{Key: "kind", Value: core.String("ResourceLimit")},
		core.Entry{Key: "code", Value: core.String(code)},
		core.Entry{Key: "limit", Value: core.String(limit)},
	)
	return value
}

// materializeValue dispatches the per-format materializer (facade
// re-exports; the CLI only selects the family, never implements
// materialization).
func materializeValue(value core.Value, request document.MaterializationRequest,
	family string) (*materializeOutcome, *materializeFailure) {
	switch family {
	case "json":
		result := jsonpkg.Materialize(value, request)
		if result.Failed != nil {
			failure := jsonFailureRecord(result.Failed.Failure)
			failure.Report = result.Failed.Report.Events()
			failure.AnalyzedInputPaths = result.Failed.AnalyzedInputPaths
			return nil, failure
		}
		return completeOutcome(result.Complete.Document.Source(),
			result.Complete.Document.Render(), jsonFidelityWire(result.Complete.Fidelity),
			result.Complete.Report.Events()), nil
	case "toml":
		result := toml.Materialize(value, request)
		if result.Failed != nil {
			failure := tomlFailureRecord(&result.Failed.Failure)
			failure.Report = result.Failed.Report.Events()
			failure.AnalyzedInputPaths = result.Failed.AnalyzedInputPaths
			return nil, failure
		}
		return completeOutcome(result.Complete.Document.Source(),
			result.Complete.Document.Render(), string(result.Complete.Fidelity),
			result.Complete.Report.Events()), nil
	case "ini":
		result := ini.Materialize(value, request)
		if result.Failed != nil {
			failure := iniFailureRecord(&result.Failed.Failure)
			failure.Report = result.Failed.Report.Events()
			failure.AnalyzedInputPaths = result.Failed.AnalyzedInputPaths
			return nil, failure
		}
		return completeOutcome(result.Complete.Document.Source(),
			result.Complete.Document.Render(), string(result.Complete.Fidelity),
			result.Complete.Report.Events()), nil
	case "properties":
		result := properties.Materialize(value, request)
		if result.Failed != nil {
			failure := propertiesFailureRecord(&result.Failed.Failure)
			failure.Report = result.Failed.Report.Events()
			failure.AnalyzedInputPaths = result.Failed.AnalyzedInputPaths
			return nil, failure
		}
		return completeOutcome(result.Complete.Document.Source(),
			result.Complete.Document.Render(), string(result.Complete.Fidelity),
			result.Complete.Report.Events()), nil
	case "yaml":
		result := yamlpkg.MaterializeValue(value, request)
		if result.Failed != nil {
			failure := yamlFailureRecord(&result.Failed.Failure)
			failure.Report = result.Failed.Report.Events()
			failure.AnalyzedInputPaths = result.Failed.AnalyzedInputPaths
			return nil, failure
		}
		return completeOutcome(result.Complete.Document.Source(),
			result.Complete.Document.Render(), string(result.Complete.Fidelity),
			result.Complete.Report.Events()), nil
	case "xml":
		result := xmlpkg.Materialize(value, request)
		if result.Failed != nil {
			failure := xmlFailureRecord(result.Failed.Failure)
			failure.Report = result.Failed.Report.Events()
			failure.AnalyzedInputPaths = result.Failed.AnalyzedInputPaths
			return nil, failure
		}
		return completeOutcome(result.Complete.Document.Source(),
			result.Complete.Document.Render(), xmlFidelityWire(result.Complete.Fidelity),
			result.Complete.Report.Events()), nil
	case "plist":
		result := plist.Materialize(value, request)
		if result.Failed != nil {
			failure := plistFailureRecord(result.Failed.Failure)
			failure.Report = result.Failed.Report.Events()
			failure.AnalyzedInputPaths = result.Failed.AnalyzedInputPaths
			return nil, failure
		}
		return completeOutcome(result.Complete.Document.Source(),
			result.Complete.Document.Render(), plistFidelityWire(result.Complete.Fidelity),
			result.Complete.Report.Events()), nil
	case "hcl":
		result := hclpkg.Materialize(value, request)
		if result.Failed != nil {
			failure := hclFailureRecord(result.Failed.Failure)
			failure.Report = result.Failed.Report.Events()
			failure.AnalyzedInputPaths = result.Failed.AnalyzedInputPaths
			return nil, failure
		}
		return completeOutcome(result.Complete.Document.Source(),
			result.Complete.Document.Render(), hclFidelityWire(result.Complete.Fidelity),
			result.Complete.Report.Events()), nil
	}
	return nil, &materializeFailure{
		WireFailure: failureRecordValue("UnsupportedProfile",
			"core.materialization.unsupported-profile@1", ""),
		Code: "core.materialization.unsupported-profile@1",
	}
}

// completeOutcome assembles the common complete outcome of one family
// materialization.
func completeOutcome(snapshot *document.SourceSnapshot, rendered []byte,
	fidelity string, report []*protocol.Diagnostic) *materializeOutcome {
	return &materializeOutcome{
		TargetSnapshot: snapshot,
		Fidelity:       fidelity,
		Report:         report,
		Rendered:       rendered,
	}
}

// The per-family fidelity-to-wire spellings (the uint8 families map their
// enum, the string families carry the wire spelling already).
func jsonFidelityWire(fidelity jsonpkg.MaterializationFidelity) string {
	if fidelity == jsonpkg.MaterializationFidelityTransformed {
		return "Transformed"
	}
	return "Exact"
}

func xmlFidelityWire(fidelity xmlpkg.MaterializationFidelity) string {
	if fidelity == xmlpkg.MaterializationFidelityTransformed {
		return "Transformed"
	}
	return "Exact"
}

func plistFidelityWire(fidelity plist.MaterializationFidelity) string {
	if fidelity == plist.MaterializationFidelityTransformed {
		return "Transformed"
	}
	return "Exact"
}

func hclFidelityWire(fidelity hclpkg.MaterializationFidelity) string {
	if fidelity == hclpkg.MaterializationFidelityTransformed {
		return "Transformed"
	}
	return "Exact"
}

// simpleFailureKind maps one family failure kind ordinal onto its frozen
// wire kind spelling (all family packages transcribe the same closed enum
// from consema-document materialization.rs, in the same order).
func simpleFailureKind(kind int) string {
	names := []string{"InvalidRequest", "UnsupportedProfile", "UnsupportedStyle",
		"UnsupportedEncoding", "UnsupportedNewline", "Unrepresentable",
		"ResourceLimit", "FormationFailed"}
	if kind >= 0 && kind < len(names) {
		return names[kind]
	}
	return "FormationFailed"
}

// failureRecordFromParts assembles the materializeFailure of one family
// failure.
func failureRecordFromParts(code string,
	build func() (string, core.Value)) *materializeFailure {
	_, wire := build()
	return &materializeFailure{WireFailure: wire, Code: code}
}

// The per-family failure record converters; each maps the family failure
// kind onto the frozen wire failure record (mirror of the Rust
// MaterializationFailureMessage::from_failure). The family failure types
// all carry the same frozen kind constants.
func jsonFailureRecord(failure *jsonpkg.MaterializationFailure) *materializeFailure {
	return failureRecordFromParts(failure.Code(), func() (string, core.Value) {
		switch failure.Kind {
		case jsonpkg.MaterializationFailureInvalidRequest:
			return "InvalidRequest", failureRecordValue("InvalidRequest",
				failure.Code(), "invalid request")
		case jsonpkg.MaterializationFailureUnrepresentable:
			return "Unrepresentable", unrepresentableFailureValue(failure.Code(),
				failure.Path, failure.ValueKind.String())
		case jsonpkg.MaterializationFailureResourceLimit:
			return "ResourceLimit", resourceLimitFailureValue(failure.Code(), failure.LimitName)
		default:
			kind := simpleFailureKind(int(failure.Kind))
			return kind, failureRecordValue(kind, failure.Code(), "")
		}
	})
}

func tomlFailureRecord(failure *toml.MaterializationFailure) *materializeFailure {
	return failureRecordFromParts(failure.Code(), func() (string, core.Value) {
		switch failure.Kind {
		case toml.MaterializationUnrepresentable:
			return "Unrepresentable", unrepresentableFailureValue(failure.Code(),
				failure.Path, failure.KindName)
		case toml.MaterializationResourceLimit:
			return "ResourceLimit", resourceLimitFailureValue(failure.Code(), failure.LimitName)
		default:
			kind := simpleFailureKind(int(failure.Kind))
			return kind, failureRecordValue(kind, failure.Code(), "")
		}
	})
}

func iniFailureRecord(failure *ini.MaterializationFailure) *materializeFailure {
	return failureRecordFromParts(failure.Code(), func() (string, core.Value) {
		switch failure.Kind {
		case ini.MaterializationUnrepresentable:
			return "Unrepresentable", unrepresentableFailureValue(failure.Code(),
				failure.Path, failure.KindName)
		case ini.MaterializationResourceLimit:
			return "ResourceLimit", resourceLimitFailureValue(failure.Code(), failure.LimitName)
		default:
			kind := simpleFailureKind(int(failure.Kind))
			return kind, failureRecordValue(kind, failure.Code(), "")
		}
	})
}

func propertiesFailureRecord(failure *properties.MaterializationFailure) *materializeFailure {
	return failureRecordFromParts(failure.Code(), func() (string, core.Value) {
		switch failure.Kind {
		case properties.MaterializationUnrepresentable:
			return "Unrepresentable", unrepresentableFailureValue(failure.Code(),
				failure.Path, failure.KindName)
		case properties.MaterializationResourceLimit:
			return "ResourceLimit", resourceLimitFailureValue(failure.Code(), failure.LimitName)
		default:
			kind := simpleFailureKind(int(failure.Kind))
			return kind, failureRecordValue(kind, failure.Code(), "")
		}
	})
}

func yamlFailureRecord(failure *yamlpkg.MaterializationFailure) *materializeFailure {
	return failureRecordFromParts(failure.Code(), func() (string, core.Value) {
		switch failure.Kind {
		case yamlpkg.MaterializationUnrepresentable:
			return "Unrepresentable", unrepresentableFailureValue(failure.Code(),
				failure.Path, failure.KindName)
		case yamlpkg.MaterializationResourceLimit:
			return "ResourceLimit", resourceLimitFailureValue(failure.Code(), failure.LimitName)
		default:
			kind := simpleFailureKind(int(failure.Kind))
			return kind, failureRecordValue(kind, failure.Code(), "")
		}
	})
}

func xmlFailureRecord(failure *xmlpkg.MaterializationFailure) *materializeFailure {
	return failureRecordFromParts(failure.Code(), func() (string, core.Value) {
		switch failure.Kind {
		case xmlpkg.MaterializationFailureUnrepresentable:
			return "Unrepresentable", unrepresentableFailureValue(failure.Code(),
				failure.Path, failure.ValueKind.String())
		case xmlpkg.MaterializationFailureResourceLimit:
			return "ResourceLimit", resourceLimitFailureValue(failure.Code(), failure.LimitName)
		default:
			kind := simpleFailureKind(int(failure.Kind))
			return kind, failureRecordValue(kind, failure.Code(), "")
		}
	})
}

func plistFailureRecord(failure *plist.MaterializationFailure) *materializeFailure {
	return failureRecordFromParts(failure.Code(), func() (string, core.Value) {
		switch failure.Kind {
		case plist.MaterializationFailureUnrepresentable:
			return "Unrepresentable", unrepresentableFailureValue(failure.Code(),
				failure.Path, failure.ValueKind.String())
		case plist.MaterializationFailureResourceLimit:
			return "ResourceLimit", resourceLimitFailureValue(failure.Code(), failure.LimitName)
		default:
			kind := simpleFailureKind(int(failure.Kind))
			return kind, failureRecordValue(kind, failure.Code(), "")
		}
	})
}

func hclFailureRecord(failure *hclpkg.MaterializationFailure) *materializeFailure {
	return failureRecordFromParts(failure.Code(), func() (string, core.Value) {
		switch failure.Kind {
		case hclpkg.MaterializationFailureUnrepresentable:
			return "Unrepresentable", unrepresentableFailureValue(failure.Code(),
				failure.Path, failure.ValueKind.String())
		case hclpkg.MaterializationFailureResourceLimit:
			return "ResourceLimit", resourceLimitFailureValue(failure.Code(), failure.LimitName)
		default:
			kind := simpleFailureKind(int(failure.Kind))
			return kind, failureRecordValue(kind, failure.Code(), "")
		}
	})
}
