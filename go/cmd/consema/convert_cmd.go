package main

// `consema convert`: the two-stage cross-format conversion, driven entirely
// by the facade's `Convert*` composition (conversion.go — projection plus
// materialization with the record-consumption gate and reparse closure
// already enforced by the root package).
//
// The request is the CLI-local `cli.convert-request@1` record (not
// registered; envelope payloads only per RFC 0015 §6.2), passed via
// `--request-file` or stdin:
//
// ```text
// cli.convert-request@1:
//   schema:                "cli.convert-request@1"
//   projection_request:    core.projection-request@1 record
//   materialization_request: core.materialization-request@2 record
// ```
//
// The source is the positional path and the profile is the mandatory
// `--profile`; the request carries only the two-stage operation specifics.
//
// The machine payload is `cli.convert@1` = `{schema, report:
// core.conversion-report@1, target: core.source-snapshot@2}` (RFC 0015
// §6.1), externalized from the facade's conversion report and the target
// document's verified snapshot. Conversion failures are atomic (no target
// document, no partial bytes) and surface as data errors carrying
// `core.conversion.*@1` codes; in human mode the target bytes are written
// to stdout. This build never writes files: `--output` is refused as a
// usage error.

import (
	"fmt"
	"io"

	consema "consema.dev/consema"
	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// convertRequestSchema is the CLI-local two-stage request record of the
// convert command.
const convertRequestSchema = "cli.convert-request@1"

// ConvertRequest is one strictly decoded two-stage convert request.
type ConvertRequest struct {
	// ProjectionRequest is the typed projection request of the source
	// format family.
	ProjectionRequest WireProjectionRequest
	// MaterializationRequest is the typed materialization request of the
	// target profile.
	MaterializationRequest document.MaterializationRequest
}

// runConvert runs `consema convert` (request from --request-file or stdin;
// source path is the positional).
func runConvert(parsed *ParsedArgs, stdout, stderr io.Writer) uint8 {
	if parsed.output != nil {
		error := usageFlowError("cli.usage.invalid-argument@1",
			"flag '--output' is not available in this build: convert writes only to "+
				"stdout (file writing lands with the CLI fsio milestone)")
		return emitFailure(protocol.CommandConvert, parsed, error, nil, stdout, stderr)
	}
	request, err := readRequestBytes(parsed)
	if err != nil {
		return emitFailure(protocol.CommandConvert, parsed, err, nil, stdout, stderr)
	}
	return runConvertWithRequest(parsed, request, stdout, stderr)
}

// runConvertWithRequest runs `consema convert` against already-read request
// bytes.
func runConvertWithRequest(parsed *ParsedArgs, request []byte,
	stdout, stderr io.Writer) uint8 {
	if parsed.output != nil {
		error := usageFlowError("cli.usage.invalid-argument@1",
			"flag '--output' is not available in this build: convert writes only to "+
				"stdout (file writing lands with the CLI fsio milestone)")
		return emitFailure(protocol.CommandConvert, parsed, error, nil, stdout, stderr)
	}
	// The presentation redaction policy of RFC 0015 §11 (an invalid
	// `--redact-keys` pattern is a usage failure, like plan/apply/edit).
	policy, policyErr := compileRedactPolicy(parsed)
	if policyErr != nil {
		return emitFailure(protocol.CommandConvert, parsed, policyErr, nil, stdout, stderr)
	}
	convertRequest, err := decodeConvertRequest(request, parsed)
	if err != nil {
		return emitFailure(protocol.CommandConvert, parsed, err, policy, stdout, stderr)
	}
	sourcePath := parsed.positionals[0]
	payload, targetBytes, err := executeConvert(sourcePath, parsed, convertRequest)
	if err != nil {
		return emitFailure(protocol.CommandConvert, parsed, err, policy, stdout, stderr)
	}
	if parsed.json {
		if emitErr := emitCommandEnvelope(protocol.CommandConvert, protocol.ExitSuccess,
			payload, nil, parsed, policy, stdout); emitErr != nil {
			return internalFailure("convert", emitErr.Error(), stderr)
		}
		return protocol.ExitSuccess.ExitCode()
	}
	if _, writeErr := stdout.Write(targetBytes); writeErr != nil {
		return internalFailure("convert", writeErr.Error(), stderr)
	}
	return protocol.ExitSuccess.ExitCode()
}

// decodeConvertRequest strictly decodes the `cli.convert-request@1` record:
// exact fields, `projection_request` strictly decoded as
// `core.projection-request@1` and mapped to the typed per-format request of
// the source family, `materialization_request` strictly decoded as
// `core.materialization-request@2`.
func decodeConvertRequest(bytes []byte, parsed *ParsedArgs) (*ConvertRequest, *FlowError) {
	limits := protocol.DefaultProtocolLimits()
	var value core.Value
	var err error
	if len(bytes) >= 4 && string(bytes[:4]) == "PVCE" {
		value, err = protocol.DecodePVCE(bytes, limits)
	} else {
		value, err = protocol.DecodeJSON(bytes, limits)
	}
	if err != nil {
		return nil, protocolFlowError(err)
	}
	object, ok := value.(*core.Object)
	if !ok {
		return nil, newFlowError("cli.data.invalid-request@1",
			"cli.convert-request@1 must be an Object")
	}
	entries := object.Entries()
	if len(entries) != 3 || entries[0].Key != "schema" ||
		entries[0].Value != core.String(convertRequestSchema) ||
		entries[1].Key != "projection_request" ||
		entries[2].Key != "materialization_request" {
		return nil, newFlowError("cli.data.invalid-request@1",
			"cli.convert-request@1 requires exactly schema/projection_request/"+
				"materialization_request in order")
	}
	if !payloadSchemaMatches(entries[1].Value, "core.projection-request@1") {
		return nil, newFlowError("cli.data.invalid-request@1",
			"projection_request must be a core.projection-request@1 record")
	}
	if !payloadSchemaMatches(entries[2].Value, "core.materialization-request@2") {
		return nil, newFlowError("cli.data.invalid-request@1",
			"materialization_request must be a core.materialization-request@2 record")
	}
	profile, flowErr := resolveProfile(*parsed.profile)
	if flowErr != nil {
		return nil, flowErr
	}
	family := formatFamily(profile.ID())
	if family == "" {
		return nil, newFlowError("cli.data.invalid-request@1",
			"profile '"+profile.ID()+"' has no format family")
	}
	projectionMessage, err := (&protocol.ProjectionRequestMessage{}).FromValue(entries[1].Value)
	if err != nil {
		return nil, protocolFlowError(err)
	}
	projectionRequest, flowErr := wireProjectionRequest(family, projectionMessage)
	if flowErr != nil {
		return nil, flowErr
	}
	materializationMessage, err := (&protocol.MaterializationRequestMessageV2{}).FromValue(entries[2].Value)
	if err != nil {
		return nil, protocolFlowError(err)
	}
	return &ConvertRequest{
		ProjectionRequest:      projectionRequest,
		MaterializationRequest: wireRequestToDocument(materializationMessage.Request()),
	}, nil
}

// executeConvert executes one conversion through the facade composition,
// returning the `cli.convert@1` payload and the rendered target bytes.
func executeConvert(sourcePath string, parsed *ParsedArgs,
	request *ConvertRequest) (core.Value, []byte, *FlowError) {
	cap := uint64(protocol.DefaultProtocolLimits().MaxBytes)
	if parsed.maxBytes != nil {
		cap = *parsed.maxBytes
	}
	source, err := readSourceCapped(sourcePath, cap)
	if err != nil {
		return nil, nil, err
	}
	profile, flowErr := resolveProfile(*parsed.profile)
	if flowErr != nil {
		return nil, nil, flowErr
	}
	document, flowErr := parseDocument(source, &profile)
	if flowErr != nil {
		return nil, nil, flowErr
	}
	if flowErr := requireComplete(document, sourcePath); flowErr != nil {
		return nil, nil, flowErr
	}
	result, flowErr := convertDocument(document, request, profile.ID())
	if flowErr != nil {
		return nil, nil, flowErr
	}
	if result.Failed != nil {
		code := result.Failed.Code()
		payload := convertFailurePayload()
		flowError := newFlowError(code, "conversion failed atomically ("+code+")")
		flowError = flowError.withPayload(payload)
		flowError = flowError.withDiagnostics(convertFailureDiagnostics(result.Failed, sourcePath))
		return nil, nil, flowError
	}
	complete := result.Complete
	report, reportErr := conversionReportValue(complete)
	if reportErr != nil {
		return nil, nil, reportErr
	}
	targetSnapshot := targetSnapshotOf(complete.Document, &request.MaterializationRequest)
	if targetSnapshot == nil {
		return nil, nil, newFlowError("core.materialization.unsupported-profile@1",
			fmt.Sprintf("target profile '%s' is not materializable",
				request.MaterializationRequest.TargetProfile().ID()))
	}
	snapshotMessage := protocol.NewSourceSnapshotMessageV2FromSnapshot(targetSnapshot)
	snapshotValue, snapshotErr := snapshotMessage.ToValue()
	if snapshotErr != nil {
		return nil, nil, newFlowError("core.protocol.invalid-value@1",
			"target snapshot encoding failed: "+snapshotErr.Error())
	}
	payload, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("cli.convert@1")},
		core.Entry{Key: "report", Value: report},
		core.Entry{Key: "target", Value: snapshotValue},
	)
	return payload, complete.Document.Render(), nil
}

// convertFailurePayload builds the failed form of the CLI-local record:
// report and target are null (there is no target document by construction);
// the envelope diagnostics carry the atomic failure.
func convertFailurePayload() core.Value {
	payload, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("cli.convert@1")},
		core.Entry{Key: "report", Value: core.NullValue()},
		core.Entry{Key: "target", Value: core.NullValue()},
	)
	return payload
}

// convertFailureDiagnostics binds the atomic conversion failure facts to
// envelope diagnostics.
func convertFailureDiagnostics(failure *consema.ConversionFailure,
	sourcePath string) []*protocol.Diagnostic {
	diagnostics := make([]*protocol.Diagnostic, 0)
	if failure.Kind == consema.ConversionFailureProjectionFailed {
		diagnostics = append(diagnostics, failure.ProjectionDiagnostics...)
	}
	if len(diagnostics) == 0 {
		diagnostics = append(diagnostics, diagnosticFor(failure.Code()))
	}
	return diagnostics
}

// convertDocument dispatches the facade `Convert*` composition by source
// family. The record-consumption gate and the reparse closure are inside
// the root package; the CLI only selects the family and the typed requests.
func convertDocument(document *consema.Document, request *ConvertRequest,
	sourceProfileID string) (*consema.ConversionResult, *FlowError) {
	materialization := request.MaterializationRequest
	switch sourceProfileID {
	case "json.strict", "jsonc.bounded", "json5.standard":
		if request.ProjectionRequest.json == nil {
			return nil, projectionProfileMismatch(sourceProfileID)
		}
		jsonDocument, ok := document.AsJSON()
		if !ok {
			return nil, formatMismatch("json")
		}
		result := consema.ConvertJSON(jsonDocument, request.ProjectionRequest.json, materialization)
		return &result, nil
	case "toml.1.0":
		if request.ProjectionRequest.toml == nil {
			return nil, projectionProfileMismatch(sourceProfileID)
		}
		tomlDocument, ok := document.AsTOML()
		if !ok {
			return nil, formatMismatch("toml")
		}
		result := consema.ConvertTOML(tomlDocument, *request.ProjectionRequest.toml, materialization)
		return &result, nil
	case "ini.portable", "ini.windows", "ini.python-configparser":
		if request.ProjectionRequest.ini == nil {
			return nil, projectionProfileMismatch(sourceProfileID)
		}
		iniDocument, ok := document.AsINI()
		if !ok {
			return nil, formatMismatch("ini")
		}
		result := consema.ConvertINI(iniDocument, *request.ProjectionRequest.ini, materialization)
		return &result, nil
	case "java-properties.reader", "java-properties.latin1":
		if request.ProjectionRequest.properties == nil {
			return nil, projectionProfileMismatch(sourceProfileID)
		}
		propertiesDocument, ok := document.AsProperties()
		if !ok {
			return nil, formatMismatch("java-properties")
		}
		result := consema.ConvertProperties(propertiesDocument,
			*request.ProjectionRequest.properties, materialization)
		return &result, nil
	case "yaml.1.2-core", "yaml.1.1-compat":
		if request.ProjectionRequest.yaml == nil {
			return nil, projectionProfileMismatch(sourceProfileID)
		}
		yamlDocument, ok := document.AsYAML()
		if !ok {
			return nil, formatMismatch("yaml")
		}
		result := consema.ConvertYAML(yamlDocument, *request.ProjectionRequest.yaml, materialization)
		return &result, nil
	case "xml.1.0-safe":
		if request.ProjectionRequest.xml == nil {
			return nil, projectionProfileMismatch(sourceProfileID)
		}
		xmlDocument, ok := document.AsXML()
		if !ok {
			return nil, formatMismatch("xml")
		}
		result := consema.ConvertXML(xmlDocument, *request.ProjectionRequest.xml, materialization)
		return &result, nil
	case "plist.xml", "plist.binary":
		if request.ProjectionRequest.plist == nil {
			return nil, projectionProfileMismatch(sourceProfileID)
		}
		plistDocument, ok := document.AsPlist()
		if !ok {
			return nil, formatMismatch("plist")
		}
		result := consema.ConvertPlist(plistDocument, *request.ProjectionRequest.plist, materialization)
		return &result, nil
	case "hcl.native", "hcl.tfvars":
		if request.ProjectionRequest.hcl == nil {
			return nil, projectionProfileMismatch(sourceProfileID)
		}
		hclDocument, ok := document.AsHCL()
		if !ok {
			return nil, formatMismatch("hcl")
		}
		result := consema.ConvertHCL(hclDocument, *request.ProjectionRequest.hcl, materialization)
		return &result, nil
	}
	return nil, newFlowError("cli.data.invalid-request@1",
		"profile '"+sourceProfileID+"' has no format family")
}

func projectionProfileMismatch(profileID string) *FlowError {
	return newFlowError("cli.data.invalid-request@1",
		fmt.Sprintf("projection target does not match source profile '%s'", profileID))
}

// conversionReportValue externalizes the complete two-stage report as the
// `core.conversion-report@1` record (mirror of the facade's own
// protocol_report externalization; the CLI assembles the record from the
// facade's public report facts).
func conversionReportValue(complete *consema.CompleteConversion) (core.Value, *FlowError) {
	report := complete.Report
	var projectionReport core.Value
	switch {
	case report.ProjectionReport().JSON != nil:
		message, flowErr := jsonReportMessage(report.ProjectionReport().JSON,
			complete.ProjectionProvenance.JSON, "source")
		if flowErr != nil {
			return nil, flowErr
		}
		var valueErr error
		projectionReport, valueErr = message.ToValue()
		if valueErr != nil {
			return nil, newFlowError("cli.data.invalid-request@1",
				"projection report externalization failed: "+valueErr.Error())
		}
	case report.ProjectionReport().Properties != nil:
		if len(report.ProjectionReport().Properties.Events()) != 0 {
			return nil, newFlowError("cli.data.invalid-request@1",
				"$.projection_report: projection report and provenance variants do not "+
					"match the source profile")
		}
		message, _ := protocol.NewProjectionReportMessageWithRegistry(nil,
			protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
		projectionReport, _ = message.ToValue()
	case report.ProjectionReport().TOML != nil ||
		report.ProjectionReport().YAML != nil ||
		report.ProjectionReport().INI != nil ||
		report.ProjectionReport().XML != nil ||
		report.ProjectionReport().Plist != nil ||
		report.ProjectionReport().HCL != nil:
		if projectionEventsNonEmpty(report.ProjectionReport()) {
			return nil, newFlowError("cli.data.invalid-request@1",
				"$.projection_report: projection report and provenance variants do not "+
					"match the source profile")
		}
		message, _ := protocol.NewProjectionReportMessageWithRegistry(nil,
			protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
		projectionReport, _ = message.ToValue()
	default:
		return nil, newFlowError("cli.data.invalid-request@1",
			"$.projection_report: projection report and provenance variants do not "+
				"match the source profile")
	}
	materializationEvents := materializationReportEvents(report.MaterializationReport())
	materializationMessage, err := protocol.NewMaterializationReportMessageWithRegistry(
		materializationEvents, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		return nil, newFlowError("cli.data.invalid-request@1",
			"materialization report externalization failed: "+err.Error())
	}
	materializationReport, _ := materializationMessage.ToValue()
	value, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.conversion-report@1")},
		core.Entry{Key: "source_profile", Value: referenceValue(
			report.SourceProfile().ID(), report.SourceProfile().Version())},
		core.Entry{Key: "target_profile", Value: referenceValue(
			report.TargetProfile().ID(), report.TargetProfile().Version())},
		core.Entry{Key: "projection_fidelity", Value: core.String(
			report.ProjectionFidelity().String())},
		core.Entry{Key: "projection_report", Value: projectionReport},
		core.Entry{Key: "materialization_fidelity", Value: core.String(
			report.MaterializationFidelity().String())},
		core.Entry{Key: "materialization_report", Value: materializationReport},
		core.Entry{Key: "overall_fidelity", Value: core.String(
			report.OverallFidelity().String())},
	)
	return value, nil
}

// projectionEventsNonEmpty reports whether the conversion projection report
// of a non-JSON/Properties family carries events (such reports cannot be
// externalized).
func projectionEventsNonEmpty(report consema.ConversionProjectionReport) bool {
	switch {
	case report.TOML != nil:
		return len(report.TOML.Events()) != 0
	case report.YAML != nil:
		return len(report.YAML.Events()) != 0
	case report.INI != nil:
		return len(report.INI.Events()) != 0
	case report.XML != nil:
		return len(report.XML.Events()) != 0
	case report.Plist != nil:
		return len(report.Plist.Events()) != 0
	case report.HCL != nil:
		return len(report.HCL.Events()) != 0
	}
	return false
}

// materializationReportEvents extracts the ordered materialization report
// events of the conversion report (all family reports carry protocol
// diagnostics).
func materializationReportEvents(report consema.ConversionMaterializationReport) []*protocol.Diagnostic {
	switch {
	case report.JSON != nil:
		return report.JSON.Events()
	case report.TOML != nil:
		return report.TOML.Events()
	case report.YAML != nil:
		return report.YAML.Events()
	case report.INI != nil:
		return report.INI.Events()
	case report.Properties != nil:
		return report.Properties.Events()
	case report.XML != nil:
		return report.XML.Events()
	case report.Plist != nil:
		return report.Plist.Events()
	case report.HCL != nil:
		return report.HCL.Events()
	}
	return nil
}

// targetSnapshotOf returns the verified target snapshot of a complete
// conversion (all eight format documents expose their immutable source
// through the facade adapters).
func targetSnapshotOf(doc *consema.Document,
	request *document.MaterializationRequest) *protocol.SourceSnapshot {
	family := formatFamily(request.TargetProfile().ID())
	var source *document.SourceSnapshot
	switch family {
	case "json":
		jsonDocument, ok := doc.AsJSON()
		if !ok {
			return nil
		}
		source = jsonDocument.Source()
	case "toml":
		tomlDocument, ok := doc.AsTOML()
		if !ok {
			return nil
		}
		source = tomlDocument.Source()
	case "ini":
		iniDocument, ok := doc.AsINI()
		if !ok {
			return nil
		}
		source = iniDocument.Source()
	case "properties":
		propertiesDocument, ok := doc.AsProperties()
		if !ok {
			return nil
		}
		source = propertiesDocument.Source()
	case "yaml":
		yamlDocument, ok := doc.AsYAML()
		if !ok {
			return nil
		}
		source = yamlDocument.Source()
	case "xml":
		xmlDocument, ok := doc.AsXML()
		if !ok {
			return nil
		}
		source = xmlDocument.Source()
	case "plist":
		plistDocument, ok := doc.AsPlist()
		if !ok {
			return nil
		}
		source = plistDocument.Source()
	case "hcl":
		hclDocument, ok := doc.AsHCL()
		if !ok {
			return nil
		}
		source = hclDocument.Source()
	default:
		return nil
	}
	snapshot, err := protocol.NewSourceSnapshotFromRaw(source.Bytes(),
		documentEncodingRequest(source), protocol.DefaultSourceLimits())
	if err != nil {
		return nil
	}
	return snapshot
}
