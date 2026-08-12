package main

// `consema capabilities`: the facade capability inventory (RFC 0015 §6.1/
// §6.2). The machine payload is the frozen `cli.capabilities@1` record: the
// eight format families, the sixteen profiles, the query-domain inventory,
// the per-profile `core.format-operation-registry@1` records, and every
// `ErrorCodeRegistry` v7 code (strictly sorted). Every fact derives from
// the facade public API through the root package's registry — nothing is
// redeclared. Read-only, no side effects.

import (
	"fmt"
	"io"
	"strings"

	consema "consema.dev/consema"
	"consema.dev/consema/core"
	"consema.dev/consema/protocol"
)

// runCapabilities runs one `consema capabilities` invocation and returns
// the frozen exit code (always 0: the inventory is the complete result).
func runCapabilities(parsed *ParsedArgs, stdout, stderr io.Writer) uint8 {
	payload := capabilitiesPayload()
	envelope, err := protocol.NewCliOutputMessage(protocol.CommandCapabilities,
		protocol.ExitSuccess, productVersion, payload, nil, noRedaction())
	if err != nil {
		return internalFailure("capabilities", "capabilities envelope: "+err.Error(), stderr)
	}
	var writeErr error
	if parsed.json {
		writeErr = emitEnvelope(envelope, parsed.pretty, stdout)
	} else {
		writeErr = writeCapabilitiesReport(stdout)
	}
	if writeErr != nil {
		return internalFailure("capabilities", writeErr.Error(), stderr)
	}
	return protocol.ExitSuccess.ExitCode()
}

// capabilitiesPayload builds the frozen `cli.capabilities@1` payload record
// (RFC 0015 §6.2).
func capabilitiesPayload() core.Value {
	families := consema.Families()
	profiles := profileEntries()
	domains := consema.QueryDomains()

	familyValues := make([]core.Value, 0, len(families))
	for _, family := range families {
		familyValues = append(familyValues, referenceValue(family.ID(), family.Version()))
	}

	profileValues := make([]core.Value, 0, len(profiles))
	for _, entry := range profiles {
		profileValues = append(profileValues,
			referenceValue(entry.Profile.ID(), entry.Profile.Version()))
	}

	domainValues := make([]core.Value, 0, len(domains))
	for _, domain := range domains {
		domainValues = append(domainValues,
			referenceValue(domain.ID(), domain.Version()))
	}

	operationValues := make([]core.Value, 0, len(profiles))
	for _, entry := range profiles {
		registry, ok := consema.OperationRegistryFor(entry.Profile)
		if !ok {
			continue
		}
		operationValues = append(operationValues, operationRegistryValue(registry))
	}

	codeValues := make([]core.Value, 0, 187)
	for _, code := range errorCodes() {
		codeValues = append(codeValues, core.String(code))
	}

	payload, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("cli.capabilities@1")},
		core.Entry{Key: "families", Value: core.NewArray(familyValues...)},
		core.Entry{Key: "profiles", Value: core.NewArray(profileValues...)},
		core.Entry{Key: "query_domains", Value: core.NewArray(domainValues...)},
		core.Entry{Key: "operations", Value: core.NewArray(operationValues...)},
		core.Entry{Key: "error_codes", Value: core.NewArray(codeValues...)},
	)
	return payload
}

// operationRegistryValue builds one `core.format-operation-registry@1`
// record (RFC 0015 §6.2 `operations`) from the facade operation registry.
func operationRegistryValue(registry *consema.OperationRegistry) core.Value {
	operations := registry.Operations()
	operationValues := make([]core.Value, 0, len(operations))
	for _, descriptor := range operations {
		id, idVersion := splitVersionedID(descriptor.ID())
		targetRole, targetVersion := splitVersionedID(descriptor.TargetRole())
		arguments := make([]core.Value, 0, len(descriptor.Arguments()))
		for _, argument := range descriptor.Arguments() {
			record, _ := core.NewObject(
				core.Entry{Key: "name", Value: core.String(argument.Name)},
				core.Entry{Key: "kind", Value: core.String(argument.Kind)},
				core.Entry{Key: "required", Value: core.Boolean(argument.Required)},
			)
			arguments = append(arguments, record)
		}
		record, _ := core.NewObject(
			core.Entry{Key: "operation", Value: referenceValue(id, idVersion)},
			core.Entry{Key: "target_role", Value: referenceValue(targetRole, targetVersion)},
			core.Entry{Key: "arguments", Value: core.NewArray(arguments...)},
			core.Entry{Key: "support", Value: core.String(descriptor.Support())},
		)
		operationValues = append(operationValues, record)
	}
	value, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.format-operation-registry@1")},
		core.Entry{Key: "profile", Value: referenceValue(
			registry.Profile().ID(), registry.Profile().Version())},
		core.Entry{Key: "operations", Value: core.NewArray(operationValues...)},
	)
	return value
}

// splitVersionedID splits one "namespace@version" spelling at its last "@"
// into the id and version parts.
func splitVersionedID(text string) (string, uint32) {
	index := strings.LastIndex(text, "@")
	if index < 0 {
		return text, 1
	}
	var version uint32
	_, err := fmt.Sscanf(text[index+1:], "%d", &version)
	if err != nil {
		return text, 1
	}
	return text[:index], version
}

// writeCapabilitiesReport writes the deterministic human capability
// inventory (same facade facts as the machine payload).
func writeCapabilitiesReport(stdout io.Writer) error {
	families := consema.Families()
	profiles := profileEntries()
	domains := consema.QueryDomains()
	codes := errorCodes()

	var report strings.Builder
	fmt.Fprintf(&report, "consema capabilities\n")
	fmt.Fprintf(&report, "  families (%d):\n", len(families))
	for _, family := range families {
		fmt.Fprintf(&report, "    %s@%d\n", family.ID(), family.Version())
	}
	fmt.Fprintf(&report, "  profiles (%d):\n", len(profiles))
	for _, entry := range profiles {
		fmt.Fprintf(&report, "    %s@%d (family %s)\n",
			entry.Profile.ID(), entry.Profile.Version(), entry.FamilyID)
	}
	fmt.Fprintf(&report, "  query domains (%d):\n", len(domains))
	for _, domain := range domains {
		fmt.Fprintf(&report, "    %s@%d\n", domain.ID(), domain.Version())
	}
	fmt.Fprintf(&report, "  operations (%d registries):\n", len(profiles))
	for _, entry := range profiles {
		registry, ok := consema.OperationRegistryFor(entry.Profile)
		if !ok {
			continue
		}
		operations := registry.Operations()
		ids := make([]string, 0, len(operations))
		for _, operation := range operations {
			ids = append(ids, operation.ID())
		}
		fmt.Fprintf(&report, "    %s@%d: %s\n",
			entry.Profile.ID(), entry.Profile.Version(), strings.Join(ids, ", "))
	}
	fmt.Fprintf(&report, "  error codes (%d):\n", len(codes))
	fmt.Fprintf(&report, "    %s\n", strings.Join(codes, ", "))
	_, err := io.WriteString(stdout, report.String())
	return err
}

// noRedaction returns the always-present, always-empty v7 redaction record
// (commands that carry no secret-shaped values).
func noRedaction() *protocol.Redaction {
	redaction, _ := protocol.NewRedaction(false, 0)
	return redaction
}
