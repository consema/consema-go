package protocol

import (
	"consema.dev/consema/core"
	"consema.dev/consema/graph"
)

// validateRegisteredPayload dispatches the registered contracts whose Go
// message types exist in this package to their full record decoders,
// mirroring consema-rs/consema-protocol/src/payload.rs. The 0.14.0 milestone
// closes the v1-v6 record surface exercised by the shared conformance
// vectors; contracts whose record types ship with later milestones are
// accepted at the schema-discriminator level (the NewProtocolMessage
// envelope check).
func validateRegisteredPayload(contract *ContractId, payload core.Value, registry ContractRegistry) error {
	errorRegistry := registry.ErrorCodeRegistry()
	switch contract.id + "@" + uint32String(contract.version) {
	case "core.batch-plan@1":
		message := &BatchPlanMessage{}
		_, err := message.FromValueWithRegistry(payload, errorRegistry)
		return err
	case "core.batch-result@1":
		message := &BatchResultMessage{}
		_, err := message.FromValue(payload)
		return err
	case "core.cancellation-request@1":
		message := &CancellationRequest{}
		_, err := message.FromValue(payload)
		return err
	case "core.capability-declaration@1":
		declaration := &CapabilityDeclaration{}
		_, err := declaration.FromValue(payload)
		return err
	case "core.change-set@1":
		message := &ChangeSetMessage{}
		_, err := message.FromValueWithRegistry(payload, errorRegistry)
		return err
	case "core.cli-output@1":
		message := &CliOutputMessage{}
		_, err := message.FromValueWithRegistry(payload, errorRegistry)
		return err
	case "core.completion@1":
		message := &Completion{}
		_, err := message.FromValueWithRegistry(payload, errorRegistry)
		return err
	case "core.diagnostic@1":
		diagnostic := &Diagnostic{}
		_, err := diagnostic.FromValue(payload, errorRegistry)
		return err
	case "core.error-code-registry@1":
		return ValidateErrorCodeManifestValue(payload)
	case "core.execution-policy@1":
		message := &ExecutionPolicy{}
		_, err := message.FromValue(payload)
		return err
	case "core.graph-projection-result@1":
		message := &GraphProjectionResultMessage{}
		_, err := message.FromValueWithRegistry(payload, graph.DefaultPGCELimits(), errorRegistry)
		return err
	case "core.graph-provenance-map@1":
		message := &GraphProvenanceMapMessage{}
		_, err := message.FromValue(payload)
		return err
	case "core.graph-query-result@1":
		message := &GraphQueryResultMessage{}
		_, err := message.FromValueWithRegistry(payload, graph.DefaultPGCELimits(), errorRegistry)
		return err
	case "core.ini-query-result@1":
		message := &IniQueryResultMessage{}
		_, err := message.FromValueWithRegistry(payload, errorRegistry)
		return err
	case "core.java-properties-query-result@1":
		message := &JavaPropertiesQueryResultMessage{}
		_, err := message.FromValueWithRegistry(payload, errorRegistry)
		return err
	case "core.java-utf16-string@1":
		message := &JavaUtf16String{}
		_, err := message.FromValue(payload, DefaultProtocolLimits())
		return err
	case "core.materialization-request@1":
		message := &MaterializationRequestMessageV1{}
		_, err := message.FromValue(payload)
		return err
	case "core.materialization-request@2":
		message := &MaterializationRequestMessageV2{}
		_, err := message.FromValue(payload)
		return err
	case "core.materialization-result@2":
		message := &MaterializationResultMessageV2{}
		_, err := message.FromValueWithRegistry(payload, errorRegistry)
		return err
	case "core.portable-graph@1":
		message := &PortableGraphMessage{}
		_, err := message.FromValue(payload, graph.DefaultPGCELimits())
		return err
	case "core.profile-descriptor@1":
		descriptor := &ProfileDescriptor{}
		_, err := descriptor.FromValue(payload)
		return err
	case "core.projection-report@1":
		message := &ProjectionReportMessage{}
		_, err := message.FromValueWithRegistry(payload, errorRegistry)
		return err
	case "core.projection-request@1":
		message := &ProjectionRequestMessage{}
		_, err := message.FromValue(payload)
		return err
	case "core.projection-result@1":
		message := &ProjectionResultMessage{}
		_, err := message.FromValueWithRegistry(payload, errorRegistry)
		return err
	case "core.provenance-map@1":
		message := &ProvenanceMapMessage{}
		_, err := message.FromValue(payload)
		return err
	case "core.query-definition@1":
		definition := &QueryDefinition{}
		_, failure := definition.FromProtocolValue(payload)
		if failure != nil {
			return invalid("$.payload", "invalid query definition: "+failure.Error())
		}
		return nil
	case "core.query-result@1":
		message := &QueryResultMessage{}
		_, err := message.FromValueWithRegistry(payload, errorRegistry)
		return err
	case "core.registry-manifest@1":
		manifest := &RegistryManifest{}
		_, err := manifest.FromValue(payload)
		return err
	case "core.source-encoding@1":
		message := &SourceEncodingMessage{}
		_, err := message.FromValue(payload)
		return err
	case "core.source-patch@1":
		message := &SourcePatchMessageV1{}
		_, err := message.FromValue(payload, DefaultSourcePatchLimits())
		return err
	case "core.source-patch@2":
		message := &SourcePatchMessageV2{}
		_, err := message.FromValue(payload, DefaultSourcePatchLimits())
		return err
	case "core.source-snapshot@1":
		message := &SourceSnapshotMessageV1{}
		_, err := message.FromValue(payload, DefaultSourceLimits())
		return err
	case "core.source-snapshot@2":
		message := &SourceSnapshotMessageV2{}
		_, err := message.FromValue(payload, DefaultSourceLimits())
		return err
	case "core.yaml-query-result@1":
		message := &YamlQueryResultMessage{}
		_, err := message.FromValueWithRegistry(payload, errorRegistry)
		return err
	default:
		// The remaining registered contracts validate at the envelope level
		// until their owning milestone ships the record type (NewProtocolMessage).
		return nil
	}
}
