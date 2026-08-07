package protocol

import (
	"consema.dev/consema/core"
)

// validateRegisteredPayload dispatches the registered contracts whose Go
// message types exist in this package to their full record decoders,
// mirroring crates/consema-protocol/src/payload.rs. Contracts whose record
// types ship with later milestones are accepted at the schema-discriminator
// level (the NewProtocolMessage envelope check); the full validation lands
// with the owning milestone (documented reachable-code difference, see the
// NewProtocolMessage doc comment).
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
	case "core.capability-declaration@1":
		declaration := &CapabilityDeclaration{}
		_, err := declaration.FromValue(payload)
		return err
	case "core.cli-output@1":
		message := &CliOutputMessage{}
		_, err := message.FromValueWithRegistry(payload, errorRegistry)
		return err
	case "core.diagnostic@1":
		diagnostic := &Diagnostic{}
		_, err := diagnostic.FromValue(payload, errorRegistry)
		return err
	case "core.error-code-registry@1":
		return ValidateErrorCodeManifestValue(payload)
	case "core.profile-descriptor@1":
		descriptor := &ProfileDescriptor{}
		_, err := descriptor.FromValue(payload)
		return err
	case "core.query-definition@1":
		definition := &QueryDefinition{}
		_, failure := definition.FromProtocolValue(payload)
		if failure != nil {
			return invalid("$.payload", "invalid query definition: "+failure.Error())
		}
		return nil
	case "core.registry-manifest@1":
		manifest := &RegistryManifest{}
		_, err := manifest.FromValue(payload)
		return err
	default:
		// The remaining registered contracts validate at the envelope level
		// until their owning milestone ships the record type (NewProtocolMessage).
		return nil
	}
}
