package main

// `consema project` and the wire-to-typed projection request mapping shared
// with the convert command (mirror of the Rust bin's project_cmd.rs).
//
// The request is the strict `cli.request@1` wrapper of flow.go with payload
// `core.projection-request@1`. The generic wire request (target contract +
// default policy + rules + limits) is mapped onto the typed per-format
// projection requests of the facade (hard gate 1: the CLI never
// re-implements projection semantics, it only selects published targets and
// policies). The conservative default policy
// (`core.projection.exact-or-reject@1`, no rules, no limits) is the SDK's
// own default, never invented (roadmap §10 line 818).
//
// The machine payload is the `core.projection-result@1` record (RFC 0015
// §6.1): value, fidelity, report, and provenance. The report/provenance
// externalization is wired for the baseline formats whose reports are
// closed (JSON structured events, TOML diagnostics); the other formats are
// rejected with an explicit data error rather than emitting an incomplete
// record.

import (
	"fmt"
	"io"
	"sort"

	"consema.dev/consema/core"
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

// exactOrRejectContract is the conservative default policy contract of the
// wire request.
const exactOrRejectContract = "core.projection.exact-or-reject"

// wireProjectionRequest maps the wire projection request onto the typed
// per-format request. The target contract selects one published format
// projection target; the default policy must be the conservative
// exact-or-reject default; rules and named limits are not mapped and are
// rejected as data errors instead of being silently ignored (no half-
// success, roadmap §3.4).
func wireProjectionRequest(sourceFamily string,
	message *protocol.ProjectionRequestMessage) (WireProjectionRequest, *FlowError) {
	target := message.Target()
	family := sourceFamily
	switch sourceFamily {
	case "json", "toml", "ini", "properties", "yaml", "xml", "plist", "hcl":
	default:
		return WireProjectionRequest{}, newFlowError("cli.data.invalid-request@1",
			fmt.Sprintf("projection target '%s' does not apply to family '%s'",
				target.ID(), sourceFamily))
	}
	// The java-properties family's published projection targets are
	// namespaced `java-properties.projection.*` (RFC 0010) while its wire
	// family name is "properties"; the family-prefix check needs the special
	// case or every java-properties target is rejected.
	prefix := family + ".projection."
	if family == "properties" {
		prefix = "java-properties.projection."
	}
	if !hasStringPrefix(target.ID(), prefix) {
		return WireProjectionRequest{}, newFlowError("cli.data.invalid-request@1",
			fmt.Sprintf("projection target '%s' does not belong to the '%s' format family",
				target.ID(), family))
	}
	if err := validateDefaultPolicy(message.DefaultPolicy()); err != nil {
		return WireProjectionRequest{}, err
	}
	if len(message.Rules()) != 0 {
		return WireProjectionRequest{}, newFlowError("cli.data.invalid-request@1",
			"projection rules are not mapped by this build (targets and the default policy "+
				"only); refusing instead of silently ignoring them")
	}
	if len(message.Limits()) != 0 {
		return WireProjectionRequest{}, newFlowError("cli.data.invalid-request@1",
			"projection named limits are not mapped by this build (format-owned limits "+
				"only); refusing instead of silently ignoring them")
	}
	return buildWireRequest(family, target.ID(), target.Version())
}

func hasStringPrefix(text, prefix string) bool {
	return len(text) >= len(prefix) && text[:len(prefix)] == prefix
}

// validateDefaultPolicy requires the conservative exact-or-reject contract
// with no arguments; any other policy is rejected (the CLI never invents
// loss authorization, RFC 0015 §2.2).
func validateDefaultPolicy(policy protocol.ProjectionPolicy) *FlowError {
	if policy.Contract().ID() == exactOrRejectContract &&
		policy.Contract().Version() == 1 && len(policy.Arguments()) == 0 {
		return nil
	}
	return newFlowError("cli.data.invalid-request@1",
		fmt.Sprintf("projection policy '%s@%d' is not mapped by this build (only the "+
			"conservative exact-or-reject default is wired)",
			policy.Contract().ID(), policy.Contract().Version()))
}

// buildWireRequest constructs one typed per-format request from the
// published target table.
func buildWireRequest(family, targetID string,
	version uint32) (WireProjectionRequest, *FlowError) {
	request := WireProjectionRequest{}
	switch {
	case family == "json" && targetID == "json.projection.project-as-object" && version == 1:
		built, failure := jsonpkg.NewProjectionRequestBuilder(
			jsonpkg.ProjectionTargetProjectAsObjectV1).Build()
		if failure != nil {
			return request, stableFailureFlowError(failure, "JSON projection request is invalid")
		}
		request.json = built
	case family == "json" && targetID == "json.projection.project-as-entry-mapping" && version == 1:
		built, failure := jsonpkg.NewProjectionRequestBuilder(
			jsonpkg.ProjectionTargetProjectAsEntryMappingV1).Build()
		if failure != nil {
			return request, stableFailureFlowError(failure, "JSON projection request is invalid")
		}
		request.json = built
	case family == "json" && targetID == "json.projection.best-exact-core" && version == 1:
		built, failure := jsonpkg.NewProjectionRequestBuilder(
			jsonpkg.ProjectionTargetBestExactCoreV1).Build()
		if failure != nil {
			return request, stableFailureFlowError(failure, "JSON projection request is invalid")
		}
		request.json = built
	case family == "json" && targetID == "json.projection.json5-best-exact-core" && version == 1:
		built, failure := jsonpkg.NewProjectionRequestBuilder(
			jsonpkg.ProjectionTargetJson5BestExactCoreV1).Build()
		if failure != nil {
			return request, stableFailureFlowError(failure, "JSON projection request is invalid")
		}
		request.json = built
	case family == "toml" && targetID == "toml.projection.best-exact-core" && version == 1:
		built := toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1)
		request.toml = &built
	case family == "ini" && targetID == "ini.projection.best-exact-entry-mapping" && version == 1:
		built := iniBestExactEntryMapping()
		request.ini = &built
	case family == "ini" && targetID == "ini.projection.require-object" && version == 1:
		built := iniRequireObject()
		request.ini = &built
	case family == "properties" && targetID == "java-properties.projection.best-exact-entry-mapping" && version == 1:
		built := propertiesBestExactEntryMapping()
		request.properties = &built
	case family == "properties" && targetID == "java-properties.projection.require-object" && version == 1:
		built := propertiesRequireObject()
		request.properties = &built
	case family == "yaml" && targetID == "yaml.projection.best-exact-value" && version == 1:
		built := yamlBestExactValue()
		request.yaml = &built
	case family == "xml" && targetID == "xml.projection.element-tree" && version == 1:
		built := xmlElementTree()
		request.xml = &built
	case family == "plist" && targetID == "plist.projection.value-tree" && version == 1:
		built := plistValueTree()
		request.plist = &built
	case family == "hcl" && targetID == "hcl.projection.body" && version == 1:
		built := hclBody()
		request.hcl = &built
	default:
		return request, newFlowError("cli.data.invalid-request@1",
			fmt.Sprintf("projection target '%s'@%d is not published by this build",
				targetID, version))
	}
	return request, nil
}

// runProject runs `consema project` (request from --request-file or stdin).
func runProject(parsed *ParsedArgs, stdout, stderr io.Writer) uint8 {
	request, err := readRequestBytes(parsed)
	if err != nil {
		return emitFailure(protocol.CommandProject, parsed, err, stdout, stderr)
	}
	return runProjectWithRequest(parsed, request, stdout, stderr)
}

// runProjectWithRequest runs `consema project` against already-read request
// bytes.
func runProjectWithRequest(parsed *ParsedArgs, request []byte,
	stdout, stderr io.Writer) uint8 {
	input, err := decodeRequest(request, parsed, "core.projection-request@1")
	if err != nil {
		return emitFailure(protocol.CommandProject, parsed, err, stdout, stderr)
	}
	result, err := executeProject(input)
	if err != nil {
		return emitFailure(protocol.CommandProject, parsed, err, stdout, stderr)
	}
	if parsed.json {
		resultValue, valueErr := result.ToValue()
		if valueErr != nil {
			return internalFailure("project", valueErr.Error(), stderr)
		}
		if emitErr := emitCommandEnvelope(protocol.CommandProject, protocol.ExitSuccess,
			resultValue, nil, parsed, stdout); emitErr != nil {
			return internalFailure("project", emitErr.Error(), stderr)
		}
		return protocol.ExitSuccess.ExitCode()
	}
	value, ok := result.Value()
	if !ok {
		return internalFailure("project", "successful projection always carries a value", stderr)
	}
	if _, writeErr := fmt.Fprintf(stdout, "%s\n", renderValue(value)); writeErr != nil {
		return internalFailure("project", writeErr.Error(), stderr)
	}
	return protocol.ExitSuccess.ExitCode()
}

// executeProject executes the request's projection, returning the complete
// or failed `core.projection-result@1` message.
func executeProject(input *RequestInput) (*protocol.ProjectionResultMessage, *FlowError) {
	family := formatFamily(input.Profile.ID())
	if family == "" {
		return nil, newFlowError("cli.data.invalid-request@1",
			"profile '"+input.Profile.ID()+"' has no format family")
	}
	if family != "json" && family != "toml" {
		return nil, newFlowError("cli.data.invalid-request@1",
			fmt.Sprintf("the project command is not wired for the '%s' family "+
				"(its report/provenance externalization is not yet implemented); "+
				"refusing instead of emitting an incomplete record", family))
	}
	message, err := (&protocol.ProjectionRequestMessage{}).FromValue(input.Payload)
	if err != nil {
		return nil, protocolFlowError(err)
	}
	request, flowErr := wireProjectionRequest(family, message)
	if flowErr != nil {
		return nil, flowErr
	}
	doc, flowErr := parseDocument(input.Source, &input.Profile)
	if flowErr != nil {
		return nil, flowErr
	}
	if flowErr := requireComplete(doc, input.SourceLabel); flowErr != nil {
		return nil, flowErr
	}
	sourceLabel := input.SourceLabel
	switch {
	case request.json != nil:
		jsonDocument, ok := doc.AsJSON()
		if !ok {
			return nil, formatMismatch("json")
		}
		result := jsonDocument.Project(request.json)
		if result.Failed != nil {
			var emptyProvenance jsonpkg.ProvenanceMap
			report, reportErr := jsonReportMessage(&result.Failed.Report,
				&emptyProvenance, sourceLabel)
			if reportErr != nil {
				return nil, reportErr
			}
			return nil, projectionAttemptFailure(result.Failed.Diagnostics,
				"JSON projection failed", report)
		}
		report, reportErr := jsonReportMessage(&result.Complete.Report,
			&result.Complete.Provenance, sourceLabel)
		if reportErr != nil {
			return nil, reportErr
		}
		provenance, provenanceErr := jsonProvenanceMessage(&result.Complete.Provenance, sourceLabel)
		if provenanceErr != nil {
			return nil, provenanceErr
		}
		completion, completionErr := protocol.NewCompletionWithRegistry(
			protocol.CompletionSuccess, 1, 1, nil, nil,
			protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
		if completionErr != nil {
			return nil, newFlowError("core.protocol.invalid-value@1",
				"completion encoding failed: "+completionErr.Error())
		}
		projectionResult, projectionErr := protocol.NewProjectionResultMessage(completion,
			result.Complete.Value, true, jsonFidelity(result.Complete.Fidelity),
			report, provenance, nil)
		if projectionErr != nil {
			return nil, newFlowError("core.protocol.invalid-value@1",
				"projection result encoding failed: "+projectionErr.Error())
		}
		return projectionResult, nil
	case request.toml != nil:
		tomlDocument, ok := doc.AsTOML()
		if !ok {
			return nil, formatMismatch("toml")
		}
		result := tomlDocument.Project(*request.toml)
		if result.Failed != nil {
			report, reportErr := tomlReportMessage(&result.Failed.Report)
			if reportErr != nil {
				return nil, reportErr
			}
			return nil, projectionAttemptFailure(result.Failed.Diagnostics,
				"TOML projection failed", report)
		}
		report, reportErr := tomlReportMessage(&result.Complete.Report)
		if reportErr != nil {
			return nil, reportErr
		}
		provenance, provenanceErr := tomlProvenanceMessage(&result.Complete.Provenance, sourceLabel)
		if provenanceErr != nil {
			return nil, provenanceErr
		}
		completion, completionErr := protocol.NewCompletionWithRegistry(
			protocol.CompletionSuccess, 1, 1, nil, nil,
			protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
		if completionErr != nil {
			return nil, newFlowError("core.protocol.invalid-value@1",
				"completion encoding failed: "+completionErr.Error())
		}
		projectionResult, projectionErr := protocol.NewProjectionResultMessage(completion,
			result.Complete.Value, true, tomlFidelity(result.Complete.Fidelity),
			report, provenance, nil)
		if projectionErr != nil {
			return nil, newFlowError("core.protocol.invalid-value@1",
				"projection result encoding failed: "+projectionErr.Error())
		}
		return projectionResult, nil
	}
	return nil, newFlowError("cli.data.invalid-request@1",
		"no projection request wired for the source family")
}

// projectionAttemptFailure fails a projection attempt with the format's
// stable code, the partial report, and the attempt diagnostics.
func projectionAttemptFailure(diagnostics []*protocol.Diagnostic, fallback string,
	report *protocol.ProjectionReportMessage) *FlowError {
	code := "core.projection.target-not-applicable@1"
	if len(diagnostics) > 0 {
		code = diagnostics[0].Code
	}
	failed, failedErr := failedProjectionRecord(code, report, diagnostics)
	flowError := newFlowError(code, fallback)
	if failedErr == nil {
		flowError = flowError.withPayload(failed)
	} else {
		flowError = flowError.withPayload(minimalRecord(protocol.CommandProject))
	}
	return flowError
}

// failedProjectionRecord builds the failed `core.projection-result@1` record
// form: completion Failed with the stable code, no value or provenance, and
// the attempt diagnostics.
func failedProjectionRecord(code string, report *protocol.ProjectionReportMessage,
	diagnostics []*protocol.Diagnostic) (core.Value, error) {
	completion, err := protocol.NewCompletionWithRegistry(protocol.CompletionFailed,
		0, 0, nil, &code, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		return nil, err
	}
	provenance, err := protocol.NewProvenanceMapMessage(nil)
	if err != nil {
		return nil, err
	}
	result, err := protocol.NewProjectionResultMessage(completion, nil, false, nil,
		report, provenance, diagnostics)
	if err != nil {
		return nil, err
	}
	return result.ToValue()
}

// jsonReportMessage externalizes one JSON projection report (mirror of the
// facade's own externalization pattern; pure fact translation).
func jsonReportMessage(report *jsonpkg.ProjectionReport,
	provenance *jsonpkg.ProvenanceMap, sourceID string) (*protocol.ProjectionReportMessage, *FlowError) {
	events := make([]protocol.ProjectionEventMessage, 0, len(report.Events()))
	for index, event := range report.Events() {
		locations := make([]protocol.SourceLocation, 0)
		for _, entry := range provenance.Entries() {
			for _, origin := range entry.Origins {
				if origin.Node != event.Source {
					continue
				}
				location := protocol.SourceLocation{
					SourceID:  sourceID,
					StartByte: uint64(origin.Span.StartByte()),
					EndByte:   uint64(origin.Span.EndByte()),
				}
				duplicate := false
				for _, existing := range locations {
					if existing.StartByte == location.StartByte &&
						existing.EndByte == location.EndByte {
						duplicate = true
						break
					}
				}
				if !duplicate {
					locations = append(locations, location)
				}
			}
		}
		if len(locations) == 0 {
			return nil, newFlowError("cli.data.invalid-request@1",
				fmt.Sprintf("$.projection_report.events[%d].source: projection event "+
					"source requires complete external provenance", index))
		}
		var projected *protocol.ProjectedLocationMessage
		if event.Projected != nil {
			location := projectedLocationMessage(*event.Projected)
			projected = &location
		}
		code := ""
		eventKind := ""
		switch event.Kind {
		case jsonpkg.ProjectionEventStructureReencoded:
			code, eventKind = "json.projection.structure-reencoded@1", "StructureReencoded"
		case jsonpkg.ProjectionEventDuplicateCollapsed:
			code, eventKind = "json.object.duplicate-member@1", "DuplicateCollapsed"
		default:
			return nil, newFlowError("cli.data.invalid-request@1",
				fmt.Sprintf("$.projection_report.events[%d].kind: event kind has no "+
					"frozen semantic-model wire code", index))
		}
		var policyRuleID *string
		if event.Policy != nil {
			rule := jsonPolicyRuleID(*event.Policy)
			policyRuleID = &rule
		}
		oldCategory := event.OldCategory
		newCategory := event.NewCategory
		loss := lossClassification(event.Loss)
		arguments := map[string]string{"event_kind": eventKind}
		events = append(events, protocol.ProjectionEventMessage{
			Code:               code,
			PolicyRuleID:       policyRuleID,
			SourceLocations:    locations,
			ProjectedLocation:  projected,
			OldCategory:        &oldCategory,
			NewCategory:        &newCategory,
			Reversible:         event.Reversible,
			LossClassification: loss,
			Arguments:          arguments,
		})
	}
	message, err := protocol.NewProjectionReportMessageWithRegistry(events,
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		return nil, newFlowError("cli.data.invalid-request@1",
			"projection report externalization failed: "+err.Error())
	}
	return message, nil
}

func jsonPolicyRuleID(policy jsonpkg.DuplicateKeyPolicy) string {
	switch policy {
	case jsonpkg.DuplicateKeyPolicyReject:
		return "json.duplicate-key.reject@1"
	case jsonpkg.DuplicateKeyPolicyFirstWins:
		return "json.duplicate-key.first-wins@1"
	case jsonpkg.DuplicateKeyPolicyLastWins:
		return "json.duplicate-key.last-wins@1"
	}
	return ""
}

func lossClassification(fidelity jsonpkg.Fidelity) protocol.LossClassification {
	switch fidelity {
	case jsonpkg.FidelityExact:
		return protocol.LossNone
	case jsonpkg.FidelityTransformed:
		return protocol.LossReversible
	case jsonpkg.FidelityLossy:
		return protocol.LossLossy
	}
	return protocol.LossNone
}

func jsonFidelity(fidelity jsonpkg.Fidelity) *protocol.ProjectionFidelity {
	switch fidelity {
	case jsonpkg.FidelityExact:
		f := protocol.FidelityExact
		return &f
	case jsonpkg.FidelityTransformed:
		f := protocol.FidelityTransformed
		return &f
	case jsonpkg.FidelityLossy:
		f := protocol.FidelityLossy
		return &f
	}
	f := protocol.FidelityExact
	return &f
}

func tomlFidelity(fidelity toml.Fidelity) *protocol.ProjectionFidelity {
	switch fidelity {
	case toml.FidelityExact:
		f := protocol.FidelityExact
		return &f
	case toml.FidelityTransformed:
		f := protocol.FidelityTransformed
		return &f
	case toml.FidelityLossy:
		f := protocol.FidelityLossy
		return &f
	}
	f := protocol.FidelityExact
	return &f
}

// projectedLocationMessage maps one JSON-family projected location onto the
// wire record.
func projectedLocationMessage(location jsonpkg.ProjectedLocation) protocol.ProjectedLocationMessage {
	if location.IsAssociation {
		return protocol.ProjectedLocationMessage{
			Kind:        "AssociationLocation",
			Association: location.Association,
		}
	}
	return protocol.ProjectedLocationMessage{Kind: "ValuePath", Path: location.Path}
}

// projectedLocationMessageTOML maps one TOML-family projected location onto
// the wire record.
func projectedLocationMessageTOML(location toml.ProjectedLocation) protocol.ProjectedLocationMessage {
	if location.Association != nil {
		return protocol.ProjectedLocationMessage{
			Kind:        "AssociationLocation",
			Association: *location.Association,
		}
	}
	return protocol.ProjectedLocationMessage{Kind: "ValuePath", Path: location.Path}
}

// jsonProvenanceMessage externalizes one JSON provenance map (sorted,
// merged, locator-free).
func jsonProvenanceMessage(provenance *jsonpkg.ProvenanceMap,
	sourceID string) (*protocol.ProvenanceMapMessage, *FlowError) {
	entries := provenance.Entries()
	messages := make([]protocol.ProvenanceEntryMessage, 0, len(entries))
	for _, entry := range entries {
		origins := make([]protocol.SourceOriginMessage, 0, len(entry.Origins))
		for _, origin := range entry.Origins {
			relation := provenanceRelation(origin.Relation)
			origins = append(origins, protocol.SourceOriginMessage{
				SourceID:  sourceID,
				StartByte: uint64(origin.Span.StartByte()),
				EndByte:   uint64(origin.Span.EndByte()),
				Relation:  relation,
			})
		}
		projected := projectedLocationMessage(entry.Projected)
		messages = append(messages, protocol.ProvenanceEntryMessage{
			Projected: projected,
			Origins:   origins,
		})
	}
	sort.Slice(messages, func(left, right int) bool {
		return messages[left].Projected.Less(messages[right].Projected)
	})
	var merged []protocol.ProvenanceEntryMessage
	for _, message := range messages {
		if len(merged) > 0 && merged[len(merged)-1].Projected.Equal(message.Projected) {
			merged[len(merged)-1].Origins = append(merged[len(merged)-1].Origins, message.Origins...)
			continue
		}
		merged = append(merged, message)
	}
	message, err := protocol.NewProvenanceMapMessage(merged)
	if err != nil {
		return nil, newFlowError("cli.data.invalid-request@1",
			"provenance externalization failed: "+err.Error())
	}
	return message, nil
}

// tomlProvenanceMessage externalizes one TOML provenance map (its relation
// set is Direct/Derived).
func tomlProvenanceMessage(provenance *toml.ProvenanceMap,
	sourceID string) (*protocol.ProvenanceMapMessage, *FlowError) {
	entries := provenance.Entries()
	messages := make([]protocol.ProvenanceEntryMessage, 0, len(entries))
	for _, entry := range entries {
		origins := make([]protocol.SourceOriginMessage, 0, len(entry.Origins))
		for _, origin := range entry.Origins {
			origins = append(origins, protocol.SourceOriginMessage{
				SourceID:  sourceID,
				StartByte: uint64(origin.Span.StartByte()),
				EndByte:   uint64(origin.Span.EndByte()),
				Relation:  tomlRelation(origin.Relation),
			})
		}
		projected := projectedLocationMessageTOML(entry.Projected)
		messages = append(messages, protocol.ProvenanceEntryMessage{
			Projected: projected,
			Origins:   origins,
		})
	}
	sort.Slice(messages, func(left, right int) bool {
		return messages[left].Projected.Less(messages[right].Projected)
	})
	var merged []protocol.ProvenanceEntryMessage
	for _, message := range messages {
		if len(merged) > 0 && merged[len(merged)-1].Projected.Equal(message.Projected) {
			merged[len(merged)-1].Origins = append(merged[len(merged)-1].Origins, message.Origins...)
			continue
		}
		merged = append(merged, message)
	}
	message, err := protocol.NewProvenanceMapMessage(merged)
	if err != nil {
		return nil, newFlowError("cli.data.invalid-request@1",
			"provenance externalization failed: "+err.Error())
	}
	return message, nil
}

// tomlReportMessage externalizes one TOML projection report: exact TOML 1.0
// projections emit no events, so a non-empty TOML report cannot be
// externalized without loss semantics and is refused.
func tomlReportMessage(report *toml.ProjectionReport) (*protocol.ProjectionReportMessage, *FlowError) {
	if len(report.Events()) == 0 {
		message, _ := protocol.NewProjectionReportMessageWithRegistry(nil,
			protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
		return message, nil
	}
	code := report.Events()[0].Code
	return nil, newFlowError(code,
		"non-empty TOML projection reports are not externalizable by this build")
}

// The published per-family projection request constructors of the wire
// target table (facade surfaces only; the CLI never invents requests).
func iniBestExactEntryMapping() ini.ProjectionRequest {
	return ini.BestExactEntryMappingV1()
}

func iniRequireObject() ini.ProjectionRequest {
	return ini.RequireObjectV1(ini.ComparisonOriginalExact, ini.CollisionPolicyReject)
}

func propertiesBestExactEntryMapping() properties.ProjectionRequest {
	return properties.BestExactEntryMapping()
}

func propertiesRequireObject() properties.ProjectionRequest {
	return properties.RequireObject(properties.DuplicatePolicyRequireUnique)
}

func yamlBestExactValue() yamlpkg.ValueProjectionRequest {
	return yamlpkg.BestExactValueV1()
}

func xmlElementTree() xmlpkg.ProjectionRequest {
	return xmlpkg.ElementTreeRequest()
}

func plistValueTree() plist.ProjectionRequest {
	return plist.NewProjectionRequestValueTree()
}

func hclBody() hclpkg.ProjectionRequest {
	return hclpkg.ProjectionRequestBody()
}

// provenanceRelation maps one JSON-family provenance relation onto the wire
// relation set.
func provenanceRelation(relation jsonpkg.ProvenanceRelation) protocol.ProvenanceRelation {
	switch relation {
	case jsonpkg.ProvenanceRelationDirect:
		return protocol.RelationDirect
	case jsonpkg.ProvenanceRelationDerived:
		return protocol.RelationDerived
	case jsonpkg.ProvenanceRelationExpanded:
		return protocol.RelationExpanded
	case jsonpkg.ProvenanceRelationMerged:
		return protocol.RelationMerged
	case jsonpkg.ProvenanceRelationGenerated:
		return protocol.RelationGenerated
	}
	return protocol.RelationDirect
}

// tomlRelation maps one TOML-family provenance relation onto the wire
// relation set (its relation set is Direct/Derived).
func tomlRelation(relation toml.ProvenanceRelation) protocol.ProvenanceRelation {
	if relation == toml.ProvenanceRelationDerived {
		return protocol.RelationDerived
	}
	return protocol.RelationDirect
}
