package main

// `consema explain`: authoritative single-item explanations (RFC 0015
// §6.1/§6.2; mirror of the Rust bin's explain.rs).
//
// The machine payload is the frozen `cli.explain@1` record: `kind` in
// {contract, error-code, profile, capability}, the explained id and
// version, and the per-kind record (contract → id/version/stability;
// error-code → the `ErrorCodeDescriptor` fields; profile → the
// `core.profile-descriptor@1` record). The id may be given with an explicit
// kind (`consema explain error-code cli.data.io@1`) or bare
// (`consema explain cli.data.io@1`), in which case the kind is inferred by
// lookup order: error-code → contract → profile. Every record derives from
// the v7 registries and the facade profile inventory — nothing is
// redeclared.
//
// The capability kind is reserved by RFC 0015 §6.2 but this SDK build
// publishes no capability-declaration registry: an explicit capability
// lookup is a data error (exit 2) instead of an invented declaration.

import (
	"fmt"
	"io"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/protocol"
)

// explainKind is one explainable record kind (RFC 0015 §6.2 closed set).
type explainKind int

const (
	kindContract explainKind = iota
	kindErrorCode
	kindProfile
	kindCapability
)

func (k explainKind) name() string {
	switch k {
	case kindContract:
		return "contract"
	case kindErrorCode:
		return "error-code"
	case kindProfile:
		return "profile"
	case kindCapability:
		return "capability"
	}
	return "contract"
}

func parseExplainKind(name string) (explainKind, bool) {
	switch name {
	case "contract":
		return kindContract, true
	case "error-code":
		return kindErrorCode, true
	case "profile":
		return kindProfile, true
	case "capability":
		return kindCapability, true
	}
	return kindContract, false
}

// runExplain runs one `consema explain` invocation and returns the frozen
// exit code.
func runExplain(parsed *ParsedArgs, stdout, stderr io.Writer) uint8 {
	positionals := parsed.positionals
	var kind explainKind
	var id string
	if parsedKind, ok := parseExplainKind(positionals[0]); ok {
		if len(positionals) < 2 {
			// A kind without its id is a usage failure: no envelope,
			// stderr diagnostic, exit 1 (RFC 0015 §4.2).
			fmt.Fprintf(stderr,
				"consema: error: missing required argument: an id after the kind "+
					"(code cli.usage.missing-required@1)\n")
			return protocol.ClassifyErrorCode("cli.usage.missing-required@1").ExitCode()
		}
		kind = parsedKind
		id = positionals[1]
	} else {
		id = positionals[0]
		inferred, ok := inferExplainKind(id)
		if !ok {
			return emitExplainFailure(parsed, "error-code", id, idVersionOrZero(id),
				[]*protocol.Diagnostic{explainDiagnostic(
					fmt.Sprintf("explain: '%s' is not a registered v7 error code, v7 "+
						"contract, or facade profile", id))},
				stdout, stderr)
		}
		kind = inferred
	}

	record, version, err := explainRecord(kind, id)
	if err != nil {
		if kind == kindCapability {
			return emitExplainFailure(parsed, kind.name(), id, idVersionOrZero(id),
				[]*protocol.Diagnostic{explainDiagnostic(
					"explain: capability declarations are not published by this SDK build " +
						"(RFC 0015 §6.2 reserves the kind; the SDK has no " +
						"capability-declaration registry)")},
				stdout, stderr)
		}
		return emitExplainFailure(parsed, kind.name(), id, idVersionOrZero(id),
			[]*protocol.Diagnostic{explainDiagnostic(err.Error())}, stdout, stderr)
	}

	payload, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("cli.explain@1")},
		core.Entry{Key: "kind", Value: core.String(kind.name())},
		core.Entry{Key: "id", Value: core.String(id)},
		core.Entry{Key: "version", Value: integerValueOf(uint64(version))},
		core.Entry{Key: "record", Value: record},
	)
	envelope, err := protocol.NewCliOutputMessage(protocol.CommandExplain,
		protocol.ExitSuccess, productVersion, payload, nil, noRedaction())
	if err != nil {
		return internalFailure("explain", "explain envelope: "+err.Error(), stderr)
	}
	var writeErr error
	if parsed.json {
		writeErr = emitEnvelope(envelope, parsed.pretty, stdout)
	} else {
		writeErr = writeExplainReport(kind, id, version, record, stdout)
	}
	if writeErr != nil {
		return internalFailure("explain", writeErr.Error(), stderr)
	}
	return protocol.ExitSuccess.ExitCode()
}

// explainRecord resolves one explainable id into its per-kind record.
func explainRecord(kind explainKind, id string) (core.Value, uint32, error) {
	switch kind {
	case kindContract:
		return explainContract(id)
	case kindErrorCode:
		return explainErrorCode(id)
	case kindProfile:
		return explainProfile(id)
	case kindCapability:
		return nil, 0, fmt.Errorf("capability declarations are not published")
	}
	return nil, 0, fmt.Errorf("unknown kind")
}

// inferExplainKind infers the kind of a bare id by deterministic lookup
// order: error-code, then contract, then profile.
func inferExplainKind(id string) (explainKind, bool) {
	if _, _, err := explainErrorCode(id); err == nil {
		return kindErrorCode, true
	}
	if _, _, err := explainContract(id); err == nil {
		return kindContract, true
	}
	if _, _, err := explainProfile(id); err == nil {
		return kindProfile, true
	}
	return kindContract, false
}

// explainContract resolves one `core.cli-output@1`-style contract reference.
func explainContract(id string) (core.Value, uint32, error) {
	contractID, version, ok := parseVersionedID(id)
	if !ok {
		return nil, 0, fmt.Errorf("explain: '%s' must carry a @version suffix (contract ids)", id)
	}
	registry := protocol.NewContractRegistry(protocol.RegistryV7)
	contract, contractErr := protocol.NewContractId(contractID, version)
	if contractErr != nil {
		return nil, 0, fmt.Errorf("explain: no v7 contract '%s'", id)
	}
	descriptor := registry.Descriptor(contract)
	if descriptor == nil {
		return nil, 0, fmt.Errorf("explain: no v7 contract '%s'", id)
	}
	record, _ := core.NewObject(
		core.Entry{Key: "id", Value: core.String(descriptor.ID)},
		core.Entry{Key: "version", Value: integerValueOf(uint64(descriptor.Version))},
		core.Entry{Key: "stability", Value: core.String(descriptor.Stability.String())},
	)
	return record, version, nil
}

// explainErrorCode resolves one `cli.data.io@1`-style error-code reference.
func explainErrorCode(id string) (core.Value, uint32, error) {
	_, version, ok := parseVersionedID(id)
	if !ok {
		return nil, 0, fmt.Errorf("explain: '%s' must carry a @version suffix (error-code ids)", id)
	}
	registry := protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7)
	descriptor := registry.Descriptor(id)
	if descriptor == nil {
		return nil, 0, fmt.Errorf("explain: no v7 error code '%s'", id)
	}
	record, _ := core.NewObject(
		core.Entry{Key: "code", Value: core.String(descriptor.Code)},
		core.Entry{Key: "category", Value: core.String(descriptor.Category.String())},
		core.Entry{Key: "introduced", Value: core.String(descriptor.Introduced)},
		core.Entry{Key: "description", Value: core.String(descriptor.Description)},
	)
	return record, version, nil
}

// explainProfile resolves one `ini.portable@1`-style facade profile
// reference into the `core.profile-descriptor@1` record (RFC 0015 §6.2
// profile row). The descriptor carries only the facts the facade publishes:
// family and profile ids/versions.
func explainProfile(id string) (core.Value, uint32, error) {
	profileID, version, ok := parseVersionedID(id)
	if !ok {
		return nil, 0, fmt.Errorf("explain: '%s' must carry a @version suffix (profile ids)", id)
	}
	entry := profileByID(profileID)
	if entry == nil || entry.Profile.Version() != version {
		return nil, 0, fmt.Errorf("explain: no facade profile '%s'", id)
	}
	record, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.profile-descriptor@1")},
		core.Entry{Key: "format_family_id", Value: core.String(entry.FamilyID)},
		core.Entry{Key: "format_family_version", Value: integerValueOf(uint64(entry.FamilyVersion))},
		core.Entry{Key: "profile_id", Value: core.String(entry.Profile.ID())},
		core.Entry{Key: "profile_version", Value: integerValueOf(uint64(entry.Profile.Version()))},
		core.Entry{Key: "encoding", Value: core.NullValue()},
		core.Entry{Key: "differences", Value: core.NewArray()},
		core.Entry{Key: "required_capabilities", Value: core.NewArray()},
	)
	return record, version, nil
}

// idVersionOrZero returns the version of a failed lookup (0 when the id has
// no parseable version).
func idVersionOrZero(id string) uint32 {
	_, version, ok := parseVersionedID(id)
	if !ok {
		return 0
	}
	return version
}

// explainDiagnostic builds one frozen cli.data.invalid-request@1 diagnostic
// for a failed lookup (the nearest frozen data-class code; RFC 0015 §13.1).
func explainDiagnostic(message string) *protocol.Diagnostic {
	diagnostic, _ := protocol.NewDiagnostic("cli.data.invalid-request@1",
		protocol.CategoryEncoding, protocol.SeverityError, nil, nil,
		map[string]string{"message": message}, nil, nil, 0,
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	return diagnostic
}

// emitExplainFailure emits one data-class explain failure envelope:
// `cli.explain@1` with the given kind/id/version and an empty record, plus
// the failure diagnostic (RFC 0015 §4.2: data-class failures carry
// envelopes). The envelope is written only under `--json`; in human mode
// the failure writes zero stdout bytes and the diagnostics below are the
// failure surface (RFC 0015 §3.3).
func emitExplainFailure(parsed *ParsedArgs, kind, id string, version uint32,
	diagnostics []*protocol.Diagnostic, stdout, stderr io.Writer) uint8 {
	emptyRecord, _ := core.NewObject()
	payload, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("cli.explain@1")},
		core.Entry{Key: "kind", Value: core.String(kind)},
		core.Entry{Key: "id", Value: core.String(id)},
		core.Entry{Key: "version", Value: integerValueOf(uint64(version))},
		core.Entry{Key: "record", Value: emptyRecord},
	)
	exitClass := protocol.ExitData
	if len(diagnostics) > 0 {
		exitClass = protocol.ClassifyErrorCode(diagnostics[0].Code)
	}
	envelope, err := protocol.NewCliOutputMessage(protocol.CommandExplain,
		exitClass, productVersion, payload, diagnostics, noRedaction())
	if err != nil {
		return internalFailure("explain", "explain failure envelope: "+err.Error(), stderr)
	}
	var writeErr error
	if parsed.json {
		writeErr = emitEnvelope(envelope, parsed.pretty, stdout)
	}
	if writeErr != nil {
		return internalFailure("explain", writeErr.Error(), stderr)
	}
	for _, diagnostic := range diagnostics {
		fmt.Fprintf(stderr, "consema: error: explain: %s (code %s)\n",
			diagnosticMessage(diagnostic), diagnostic.Code)
	}
	return exitClass.ExitCode()
}

// writeExplainReport writes the deterministic human explanation; it renders
// the same record the machine payload carries (implementation plan §2.4).
func writeExplainReport(kind explainKind, id string, version uint32,
	record core.Value, stdout io.Writer) error {
	var text strings.Builder
	fmt.Fprintf(&text, "consema explain %s %s\n", kind.name(), id)
	fmt.Fprintf(&text, "  kind: %s\n", kind.name())
	fmt.Fprintf(&text, "  version: %d\n", version)
	text.WriteString("  record:\n")
	if object, ok := record.(*core.Object); ok {
		for _, entry := range object.Entries() {
			value := ""
			switch typed := entry.Value.(type) {
			case core.String:
				value = string(typed)
			case core.Integer:
				value = typed.String()
			default:
				value = renderValue(entry.Value)
			}
			fmt.Fprintf(&text, "    %s: %s\n", entry.Key, value)
		}
	}
	_, err := io.WriteString(stdout, text.String())
	return err
}
