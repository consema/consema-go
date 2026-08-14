package main

// The shared strict request-input contract and failure algebra of the
// request commands (mirror of the Rust bin's query_cmd.rs shared parts;
// RFC 0015 §3.2/§4.2/§5).
//
// All four request commands (query/project/materialize/convert) and the
// edit/plan commands consume a request document passed via
// `--request-file <path>` or stdin, accepted as canonical tagged JSON or
// PVCE/1 (distinguished by the leading `PVCE` magic, RFC 0015 §3.2) and
// strictly decoded. For query/project/materialize the request is the
// CLI-local `cli.request@1` wrapper that carries the source (a path or
// inline hex bytes), the profile, and the operation payload; convert uses
// its own `cli.convert-request@1` record and edit/plan use
// `cli.edit-request@1` (module docs of their command files).
//
// ```text
// cli.request@1 (CLI-local, not registered; strict exact-fields decode):
//   schema   String   "cli.request@1" (first field)
//   source   { kind: "path", path: String } | { kind: "bytes", bytes: String }
//   profile  { id: String, version: Integer } | absent
//   payload  Object   the operation payload (schema checked per command)
// ```
//
// Every error code maps through protocol.ClassifyErrorCode (RFC 0015 §5.2)
// — the CLI never invents classes.

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	consema "consema.dev/consema"
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

// requestSchema is the CLI-local request wrapper schema shared by
// query/project/materialize.
const requestSchema = "cli.request@1"

// portableQueryDomain is the only query domain the CLI wires (native
// domains need caller-externalized node locators, which the facade does
// not expose; G121, adversarial audit 2026-08-13 — milestone phrasing
// removed).
const portableQueryDomain = "core.portable-value-query"

// fallbackRegisteredCode is the registered fallback code for format-local
// diagnostics that the semantic-model registry does not carry (the envelope
// and the failed completions can only carry registered codes; the stderr
// line keeps the true format code).
const fallbackRegisteredCode = "core.source.invalid-sequence@1"

// FlowError is one frozen failure of a request-command flow. Code is always
// a registered stable code (format-owned codes are passed through
// unchanged); the exit class is derived exclusively through
// protocol.ClassifyErrorCode (RFC 0015 §5.2). Envelope-class failures carry
// the command record in its failed form when constructible (payload), plus
// diagnostics; usage-class failures (cli.usage.*) never produce an envelope
// (RFC 0015 §4.2).
type FlowError struct {
	// Code is the stable registered diagnostic code.
	Code string
	// Message is the deterministic human message (stderr line).
	Message string
	// Diagnostics are the ordered envelope diagnostics.
	Diagnostics []*protocol.Diagnostic
	// Payload is the failed-form payload record; nil falls back to the
	// minimal `{schema}` record.
	Payload core.Value
}

// newFlowError creates a data-class failure with one registry-bound
// diagnostic.
func newFlowError(code, message string) *FlowError {
	return &FlowError{
		Code:        code,
		Message:     message,
		Diagnostics: []*protocol.Diagnostic{diagnosticFor(code)},
	}
}

// usageFlowError creates a usage-class failure (never an envelope, RFC 0015
// §4.2).
func usageFlowError(code, message string) *FlowError {
	return &FlowError{Code: code, Message: message}
}

// withPayload attaches the failed-form payload record of the envelope.
func (e *FlowError) withPayload(payload core.Value) *FlowError {
	e.Payload = payload
	return e
}

// withDiagnostics replaces the envelope diagnostics (the first document
// diagnostic determines the code; the full ordered set is carried in the
// envelope).
func (e *FlowError) withDiagnostics(diagnostics []*protocol.Diagnostic) *FlowError {
	e.Diagnostics = diagnostics
	return e
}

// ioReadFlowError creates a read failure (cli.data.io@1): the human stderr
// line carries the full OS error text, while the envelope diagnostic
// message carries only the stable error kind (RFC 0015 §3.3 — locale-
// dependent OS text never enters machine output).
func ioReadFlowError(code, phrase string, err error) *FlowError {
	message := phrase + ": " + err.Error()
	diagnostic := &protocol.Diagnostic{
		Code:       registeredCode(code),
		Category:   registryCategory(registeredCode(code)),
		Severity:   protocol.SeverityError,
		Arguments:  map[string]string{"message": phrase + ": " + errorKind(err)},
		Occurrence: 0,
	}
	return &FlowError{
		Code:        code,
		Message:     message,
		Diagnostics: []*protocol.Diagnostic{diagnostic},
	}
}

func errorKind(err error) string {
	// The stable io error kind spelling (the Rust io::ErrorKind debug
	// spelling; the envelope message is a stable fact, never OS locale
	// text).
	return "io: " + err.Error()
}

// exitClass returns the frozen exit class of the failure (RFC 0015 §5.2
// pure mapping).
func (e *FlowError) exitClass() protocol.ExitClass {
	return protocol.ClassifyErrorCode(e.Code)
}

// exitCode returns the frozen process exit code.
func (e *FlowError) exitCode() uint8 {
	return e.exitClass().ExitCode()
}

// registryCategory resolves the registry descriptor category of one code.
func registryCategory(code string) protocol.DiagnosticCategory {
	descriptor := protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7).Descriptor(code)
	if descriptor == nil {
		return protocol.CategorySemantic
	}
	return descriptor.Category
}

// registeredCode returns the code itself when it is registered in v7, else
// the registered fallback (the envelope can only carry registry-bound
// codes, RFC 0015 §4.3; the XML/plist/HCL format-local parse families are
// not part of the semantic-model registry).
func registeredCode(code string) string {
	if protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7).Contains(code) {
		return code
	}
	return fallbackRegisteredCode
}

// diagnosticFor builds one registry-bound diagnostic for a code (the core
// Diagnostic carries no free-text message; the human message travels on
// stderr, the code/category facts travel in the envelope).
func diagnosticFor(code string) *protocol.Diagnostic {
	diagnostic, err := protocol.NewDiagnostic(registeredCode(code),
		registryCategory(registeredCode(code)), protocol.SeverityError, nil, nil,
		nil, nil, nil, 0, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		return &protocol.Diagnostic{
			Code:     registeredCode(code),
			Category: registryCategory(registeredCode(code)),
			Severity: protocol.SeverityError,
		}
	}
	return diagnostic
}

// readRequestBytes reads the strict request input: `--request-file` content
// or stdin. The request-input size cap is cli.limit.manifest-size@1 (RFC
// 0015 §12); a file that cannot be read is cli.data.io@1.
func readRequestBytes(parsed *ParsedArgs) ([]byte, *FlowError) {
	cap := uint64(protocol.DefaultProtocolLimits().MaxBytes)
	if parsed.maxBytes != nil {
		cap = *parsed.maxBytes
	}
	if parsed.requestFile != nil {
		bytes, err := readCapped(*parsed.requestFile, cap)
		if err == errOverLimit {
			return nil, newFlowError("cli.limit.manifest-size@1",
				fmt.Sprintf("request file '%s' exceeds the %d-byte input cap", *parsed.requestFile, cap))
		}
		if err != nil {
			return nil, ioReadFlowError("cli.data.io@1",
				"cannot read request file '"+*parsed.requestFile+"'", err)
		}
		return bytes, nil
	}
	buffer, err := io.ReadAll(io.LimitReader(os.Stdin, int64(cap)+1))
	if err != nil {
		return nil, ioReadFlowError("cli.data.io@1", "cannot read request from stdin", err)
	}
	if uint64(len(buffer)) > cap {
		return nil, newFlowError("cli.limit.manifest-size@1",
			fmt.Sprintf("request input from stdin exceeds the %d-byte input cap", cap))
	}
	return buffer, nil
}

var errOverLimit = fmt.Errorf("read exceeds the byte cap")

// readCapped reads at most cap+1 bytes of one file.
func readCapped(path string, cap uint64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	buffer, err := io.ReadAll(io.LimitReader(file, int64(cap)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(buffer)) > cap {
		return nil, errOverLimit
	}
	return buffer, nil
}

// RequestInput is one fully decoded strict request (cli.request@1).
type RequestInput struct {
	// SourceLabel is the user-supplied source label (the path spelling
	// verbatim, or "inline").
	SourceLabel string
	// Source is the exact source bytes.
	Source []byte
	// Profile is the resolved source profile.
	Profile document.ProfileId
	// Payload is the operation payload (schema revalidated per command).
	Payload core.Value
}

// decodeRequest strictly decodes one `cli.request@1` wrapper and resolves
// its source. The transport is chosen by magic (`PVCE` prefix -> PVCE/1,
// otherwise strict canonical JSON, RFC 0015 §3.2). Unknown, reordered, or
// missing wrapper fields, non-canonical representations, and malformed
// inline bytes are all rejected (cli.data.invalid-request@1); transport
// ResourceLimit is a limit-class failure (exit 3).
func decodeRequest(bytes []byte, parsed *ParsedArgs,
	payloadSchema string) (*RequestInput, *FlowError) {
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
			"cli.request@1 must be an Object")
	}
	entries := object.Entries()
	if len(entries) != 3 && len(entries) != 4 {
		return nil, newFlowError("cli.data.invalid-request@1",
			"cli.request@1 requires exactly schema/source[/profile]/payload")
	}
	if entries[0].Key != "schema" || entries[0].Value != core.String(requestSchema) {
		return nil, newFlowError("cli.data.invalid-request@1",
			"schema must be the first field with value \"cli.request@1\"")
	}
	if entries[1].Key != "source" {
		return nil, newFlowError("cli.data.invalid-request@1",
			"source must be the second field")
	}
	sourceLabel, source, decodeErr := decodeSource(entries[1].Value, parsed)
	if decodeErr != nil {
		return nil, decodeErr
	}
	var profileEntry core.Value
	var payloadEntry core.Value
	if len(entries) == 3 {
		if entries[2].Key != "payload" {
			return nil, newFlowError("cli.data.invalid-request@1",
				"payload must follow the source")
		}
		payloadEntry = entries[2].Value
	} else {
		if entries[2].Key != "profile" {
			return nil, newFlowError("cli.data.invalid-request@1",
				"profile must follow the source")
		}
		if entries[3].Key != "payload" {
			return nil, newFlowError("cli.data.invalid-request@1",
				"payload must be the last field")
		}
		profileEntry = entries[2].Value
		payloadEntry = entries[3].Value
	}
	profile, resolveErr := resolveProfile(*parsed.profile)
	if resolveErr != nil {
		return nil, resolveErr
	}
	if profileEntry != nil {
		if validateErr := validateRequestProfile(profileEntry, &profile); validateErr != nil {
			return nil, validateErr
		}
	}
	if !payloadSchemaMatches(payloadEntry, payloadSchema) {
		return nil, newFlowError("cli.data.invalid-request@1",
			"payload schema must be \""+payloadSchema+"\"")
	}
	return &RequestInput{
		SourceLabel: sourceLabel,
		Source:      source,
		Profile:     profile,
		Payload:     payloadEntry,
	}, nil
}

func payloadSchemaMatches(payload core.Value, schema string) bool {
	object, ok := payload.(*core.Object)
	if !ok {
		return false
	}
	entries := object.Entries()
	if len(entries) == 0 {
		return false
	}
	text, ok := entries[0].Value.(core.String)
	return ok && string(text) == schema
}

// decodeSource decodes the `source` member: a path (read with the CLI read
// cap) or inline lowercase-hex bytes.
func decodeSource(value core.Value, parsed *ParsedArgs) (string, []byte, *FlowError) {
	object, ok := value.(*core.Object)
	if !ok {
		return "", nil, newFlowError("cli.data.invalid-request@1",
			"source must be an Object")
	}
	entries := object.Entries()
	if len(entries) != 2 || entries[0].Key != "kind" {
		return "", nil, newFlowError("cli.data.invalid-request@1",
			"source requires exactly kind and one value field")
	}
	kind, ok := entries[0].Value.(core.String)
	if !ok {
		return "", nil, newFlowError("cli.data.invalid-request@1",
			"source kind must be a String")
	}
	switch string(kind) {
	case "path":
		if entries[1].Key != "path" {
			return "", nil, newFlowError("cli.data.invalid-request@1",
				"path sources require the path field")
		}
		pathText, ok := entries[1].Value.(core.String)
		if !ok || string(pathText) == "" {
			return "", nil, newFlowError("cli.data.invalid-request@1",
				"source path must be non-empty")
		}
		cap := uint64(protocol.DefaultProtocolLimits().MaxBytes)
		if parsed.maxBytes != nil {
			cap = *parsed.maxBytes
		}
		bytes, err := readSourceCapped(string(pathText), cap)
		if err != nil {
			return "", nil, err
		}
		return string(pathText), bytes, nil
	case "bytes":
		if entries[1].Key != "bytes" {
			return "", nil, newFlowError("cli.data.invalid-request@1",
				"bytes sources require the bytes field")
		}
		hexText, ok := entries[1].Value.(core.String)
		if !ok {
			return "", nil, newFlowError("cli.data.invalid-request@1",
				"inline bytes must be a String")
		}
		bytes, ok := decodeHex(string(hexText))
		if !ok {
			return "", nil, newFlowError("cli.data.invalid-request@1",
				"inline bytes must be even-length lowercase hex")
		}
		return "inline", bytes, nil
	}
	return "", nil, newFlowError("cli.data.invalid-request@1",
		"source kind must be \"path\" or \"bytes\"")
}

// readSourceCapped reads one source file with the CLI byte cap (RFC 0015
// §12: over-cap is cli.limit.file-size@1, unreadable is cli.data.io@1).
func readSourceCapped(path string, cap uint64) ([]byte, *FlowError) {
	bytes, err := readCapped(path, cap)
	if err == errOverLimit {
		return nil, newFlowError("cli.limit.file-size@1",
			fmt.Sprintf("source file '%s' exceeds the %d-byte read cap", path, cap))
	}
	if err != nil {
		return nil, ioReadFlowError("cli.data.io@1",
			"cannot read source file '"+path+"'", err)
	}
	return bytes, nil
}

// decodeHex decodes even-length lowercase hex into bytes.
func decodeHex(text string) ([]byte, bool) {
	if len(text)%2 != 0 {
		return nil, false
	}
	for index := 0; index < len(text); index++ {
		byte := text[index]
		if !(byte >= '0' && byte <= '9') && !(byte >= 'a' && byte <= 'f') {
			return nil, false
		}
	}
	bytes, err := hex.DecodeString(text)
	if err != nil {
		return nil, false
	}
	return bytes, true
}

// validateRequestProfile validates the optional request profile against the
// CLI --profile selection and the registry version.
func validateRequestProfile(requested core.Value,
	resolved *document.ProfileId) *FlowError {
	object, ok := requested.(*core.Object)
	if !ok {
		return newFlowError("cli.data.invalid-request@1",
			"profile must be an Object")
	}
	entries := object.Entries()
	if len(entries) != 2 || entries[0].Key != "id" || entries[1].Key != "version" {
		return newFlowError("cli.data.invalid-request@1",
			"profile requires exactly {id, version}")
	}
	id, ok := entries[0].Value.(core.String)
	if !ok {
		return newFlowError("cli.data.invalid-request@1",
			"profile id must be a String")
	}
	if string(id) != resolved.ID() {
		return newFlowError("cli.data.invalid-request@1",
			fmt.Sprintf("request profile '%s' contradicts the --profile selection '%s'",
				string(id), resolved.ID()))
	}
	version, ok := unsignedValue(entries[1].Value)
	if !ok {
		return newFlowError("cli.data.invalid-request@1",
			"profile version must be an Integer")
	}
	if version != uint64(resolved.Version()) {
		return newFlowError("cli.data.invalid-request@1",
			fmt.Sprintf("request profile version %d is not the published %s@%d",
				version, resolved.ID(), resolved.Version()))
	}
	return nil
}

// unsignedValue extracts one non-negative uint64 from an Integer value.
func unsignedValue(value core.Value) (uint64, bool) {
	integer, ok := value.(core.Integer)
	if !ok {
		return 0, false
	}
	intValue := integer.Int()
	if intValue.Sign() < 0 {
		return 0, false
	}
	return intValue.Uint64(), true
}

// resolveProfile resolves one profile id to its registry ProfileId through
// the facade profile enums (the CLI declares no profile knowledge of its
// own). An unknown id is a usage-class failure (--format invalid, RFC 0015
// §5.1).
func resolveProfile(id string) (document.ProfileId, *FlowError) {
	entry := profileByID(id)
	if entry == nil {
		return document.ProfileId{}, usageFlowError("cli.usage.invalid-format@1",
			"unknown profile '"+id+"'")
	}
	return entry.Profile, nil
}

// parseDocument parses the resolved source bytes under the exact profile
// (facade only).
func parseDocument(source []byte, profile *document.ProfileId) (*consema.Document, *FlowError) {
	document, err := consema.ParseDocument(context.Background(), source, *profile)
	if err != nil {
		return nil, formationFlowError(err)
	}
	return document, nil
}

// formationFlowError maps a fatal formation failure to a data-class failure
// carrying the format's own stable diagnostic codes.
func formationFlowError(failure error) *FlowError {
	code := "core.source.invalid-utf8@1"
	if coded, ok := failure.(interface{ Code() string }); ok {
		code = coded.Code()
	}
	diagnostics := diagnosticsOf(failure)
	message := "source failed formation (" + code + ")"
	flowError := newFlowError(code, message)
	if len(diagnostics) > 0 {
		flowError = flowError.withDiagnostics(diagnostics)
	}
	return flowError
}

// diagnosticsOf extracts the ordered bound diagnostics of a typed failure.
func diagnosticsOf(failure error) []*protocol.Diagnostic {
	if withDiagnostics, ok := failure.(interface{ Diagnostics() []*protocol.Diagnostic }); ok {
		return withDiagnostics.Diagnostics()
	}
	return nil
}

// bindParseDiagnostic binds one core parse diagnostic to the semantic-model
// v7 registry (mirror of the Rust bind_parse_diagnostic). Diagnostics
// carrying format-local codes (the XML/plist/HCL parse families are not
// registry members) or a category that contradicts the registry descriptor
// cannot enter the envelope as-is (RFC 0015 §4.3). They bind under the
// registered fallback code with the registry's own category; the true
// format code is preserved on stderr through the bound `message` argument.
func bindParseDiagnostic(diagnostic *protocol.Diagnostic, path string) *protocol.Diagnostic {
	registry := protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7)
	if registry.Contains(diagnostic.Code) {
		bound, err := protocol.NewDiagnostic(diagnostic.Code,
			registryCategory(diagnostic.Code), diagnostic.Severity,
			bindPrimary(diagnostic.Primary, path), diagnostic.Related,
			diagnostic.Arguments, diagnostic.Notes, diagnostic.Fixes,
			diagnostic.Occurrence, registry)
		if err == nil {
			return bound
		}
		return bindPrimaryDiagnostic(diagnostic, path)
	}
	fallback := registeredCode(diagnostic.Code)
	arguments := map[string]string{}
	for name, value := range diagnostic.Arguments {
		arguments[name] = value
	}
	if fallback != diagnostic.Code {
		arguments["message"] = "format-local code " + diagnostic.Code
	}
	bound, err := protocol.NewDiagnostic(fallback, registryCategory(fallback),
		protocol.SeverityError, bindPrimary(diagnostic.Primary, path),
		diagnostic.Related, arguments, diagnostic.Notes, diagnostic.Fixes,
		diagnostic.Occurrence, registry)
	if err != nil {
		return diagnosticFor(fallback)
	}
	return bound
}

// bindPrimary binds the caller-stable source label into one primary
// location (RFC 0015 §3.3: diagnostics carry process-local snapshot
// locations that must be externalized to caller-stable locators on the
// wire; the user-supplied path spelling is that locator).
func bindPrimary(primary *protocol.SourceLocation, path string) *protocol.SourceLocation {
	if primary == nil {
		return nil
	}
	copy := *primary
	copy.SourceID = path
	return &copy
}

// bindPrimaryDiagnostic copies one diagnostic with its primary location
// bound to the caller-stable source label.
func bindPrimaryDiagnostic(diagnostic *protocol.Diagnostic, path string) *protocol.Diagnostic {
	copy := *diagnostic
	copy.Primary = bindPrimary(diagnostic.Primary, path)
	return &copy
}

// requireComplete rejects Recovered documents for parse-class operations
// (RFC 0015 §5.1: `consema query` exits 2 on input that cannot form a
// Complete document; Recovered documents never project).
func requireComplete(doc *consema.Document, sourceLabel string) *FlowError {
	if doc.FormationStatus() == document.FormationStatusComplete {
		return nil
	}
	diagnostics := doc.Diagnostics()
	code := "core.source.invalid-utf8@1"
	if len(diagnostics) > 0 {
		code = diagnostics[0].Code
	}
	flowError := newFlowError(code,
		"source '"+sourceLabel+"' is Recovered; the operation requires a Complete document")
	if len(diagnostics) > 0 {
		bound := make([]*protocol.Diagnostic, 0, len(diagnostics))
		for _, diagnostic := range diagnostics {
			bound = append(bound, bindParseDiagnostic(diagnostic, sourceLabel))
		}
		flowError = flowError.withDiagnostics(bound)
	}
	return flowError
}

// protocolFlowError maps a request transport failure per RFC 0015 §3.2: a
// request that fails strict decode is a data error with
// cli.data.invalid-request@1, except decode ResourceLimit, which is a limit
// error (exit 3).
func protocolFlowError(err error) *FlowError {
	if protocolError, ok := err.(*protocol.ProtocolError); ok &&
		protocolError.Kind == protocol.KindResourceLimit {
		return newFlowError("core.protocol.resource-limit@1",
			"request transport decode exceeded a protocol limit: "+err.Error())
	}
	return newFlowError("cli.data.invalid-request@1",
		"request transport decode failed: "+err.Error())
}

// stableFailureFlowError maps one typed failure with a Code() contract to
// its data-class failure.
func stableFailureFlowError(failure error, message string) *FlowError {
	code := "core.protocol.invalid-value@1"
	if coded, ok := failure.(interface{ Code() string }); ok {
		code = coded.Code()
	}
	return newFlowError(code, message)
}

// emitCommandEnvelope emits one validated `core.cli-output@1` envelope line
// (RFC 0015 §4). Without --json this writes nothing and returns success, so
// envelope-class failure paths leave stdout at zero bytes while stderr
// keeps the human diagnostic and the caller keeps the classified exit code.
//
// Presentation redaction (RFC 0015 §11.1/§11.3): with a non-nil policy the
// payload is redacted through redactValue and the envelope's `redaction`
// facts are built from the real replacement count (`redacted == (count > 0)`
// by construction); `--show-secrets` disables matching inside the policy
// (RFC 0015 §11.4). With a nil policy the payload is emitted untouched with
// zero redaction facts — the plan/apply manifest payload exemption of RFC
// 0015 §8.3/§11.4 (their records and files are never redacted).
func emitCommandEnvelope(command protocol.CliCommand, exitClass protocol.ExitClass,
	payload core.Value, diagnostics []*protocol.Diagnostic,
	parsed *ParsedArgs, policy *redactPolicy, stdout io.Writer) error {
	if !parsed.json {
		// RFC 0015 §3.3: without --json, stdout carries only command-result
		// data; the envelope is never written in human mode.
		return nil
	}
	var redaction *protocol.Redaction
	var err error
	if policy == nil {
		redaction, err = protocol.NewRedaction(false, 0)
	} else {
		redacted, facts := redactValue(policy, payload)
		payload = redacted
		redaction, err = protocol.NewRedaction(facts.count > 0, facts.count)
	}
	if err != nil {
		return fmt.Errorf("%s envelope construction failed: %s", command.Name(), err)
	}
	envelope, err := protocol.NewCliOutputMessage(command, exitClass,
		productVersion, payload, diagnostics, redaction)
	if err != nil {
		return fmt.Errorf("%s envelope construction failed: %s", command.Name(), err)
	}
	return emitEnvelope(envelope, parsed.pretty, stdout)
}

// minimalRecord writes the minimal `{schema}` failure record of one command
// (the envelope carries the failure in its diagnostics; typed decoders
// reject the partial record, which is the truthful statement that no
// complete result exists).
func minimalRecord(command protocol.CliCommand) core.Value {
	schemas := command.PayloadSchemas()
	if len(schemas) == 0 {
		return nil
	}
	value, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String(schemas[0])},
	)
	return value
}

// emitFailure emits the failure path of one request command: usage-class
// failures write only a stderr line (no envelope, RFC 0015 §4.2);
// envelope-class failures write the envelope with the failed record form
// plus diagnostics, then the stderr line, and exit with the classified
// code. The envelope is written only under --json; in human mode the
// failure writes zero stdout bytes (RFC 0015 §3.3).
func emitFailure(command protocol.CliCommand, parsed *ParsedArgs,
	error *FlowError, policy *redactPolicy, stdout io.Writer, stderr io.Writer) uint8 {
	fmt.Fprintf(stderr, "consema: error: %s: %s (code %s)\n",
		command.Name(), error.Message, error.Code)
	if error.exitClass() == protocol.ExitUsage {
		return error.exitCode()
	}
	payload := error.Payload
	if payload == nil {
		payload = minimalRecord(command)
	}
	if err := emitCommandEnvelope(command, error.exitClass(), payload,
		error.Diagnostics, parsed, policy, stdout); err != nil {
		return internalFailure(command.Name(), err.Error(), stderr)
	}
	return error.exitCode()
}

// internalFailure reports an unclassified internal failure on stderr and
// returns exit 5 (RFC 0015 §5.1 internal row).
func internalFailure(command, message string, stderr io.Writer) uint8 {
	fmt.Fprintf(stderr, "consema: error: %s: %s (code cli.internal.unclassified@1)\n",
		command, message)
	return protocol.ClassifyErrorCode("cli.internal.unclassified@1").ExitCode()
}

// formatFamily returns the format family of one profile id (mirrors the
// facade's conversion composition; the family decides the
// projection/materialization dispatch).
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

// publishedRecord returns the record family behind one published record
// envelope id (the materialize/convert commands re-check the facade's
// record-consumption gate).
func publishedRecord(value core.Value) (string, bool) {
	object, ok := value.(*core.Object)
	if !ok {
		return "", false
	}
	for _, entry := range object.Entries() {
		if entry.Key == "record" {
			text, ok := entry.Value.(core.String)
			if !ok {
				return "", false
			}
			switch string(text) {
			case "xml.element-tree@1":
				return "xml", true
			case "plist.value-tree@1":
				return "plist", true
			case "hcl.body@1":
				return "hcl", true
			}
			return "", false
		}
	}
	return "", false
}

// WireProjectionRequest is the typed per-format projection request of the
// request commands.
type WireProjectionRequest struct {
	json       *jsonpkg.ProjectionRequest
	toml       *toml.ProjectionRequest
	ini        *ini.ProjectionRequest
	properties *properties.ProjectionRequest
	yaml       *yamlpkg.ValueProjectionRequest
	xml        *xmlpkg.ProjectionRequest
	plist      *plist.ProjectionRequest
	hcl        *hclpkg.ProjectionRequest
}

// defaultProjectionRequest returns the default exact projection request of
// each format family. These are the SDK's conservative exact defaults
// (duplicates rejected, no authorized loss); the request commands never
// invent policies (roadmap §10——G050，对抗审计 2026-08-14：改引节锚，行号删除).
func defaultProjectionRequest(family string) (WireProjectionRequest, *FlowError) {
	switch family {
	case "json":
		request, failure := jsonpkg.NewProjectionRequestBuilder(
			jsonpkg.ProjectionTargetBestExactCoreV1).Build()
		if failure != nil {
			return WireProjectionRequest{}, stableFailureFlowError(failure,
				"JSON default projection request is invalid")
		}
		return WireProjectionRequest{json: request}, nil
	case "toml":
		request := toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1)
		return WireProjectionRequest{toml: &request}, nil
	case "ini":
		request := ini.BestExactEntryMappingV1()
		return WireProjectionRequest{ini: &request}, nil
	case "properties":
		request := properties.BestExactEntryMapping()
		return WireProjectionRequest{properties: &request}, nil
	case "yaml":
		request := yamlpkg.BestExactValueV1()
		return WireProjectionRequest{yaml: &request}, nil
	case "xml":
		request := xmlpkg.ElementTreeRequest()
		return WireProjectionRequest{xml: &request}, nil
	case "plist":
		request := plist.NewProjectionRequestValueTree()
		return WireProjectionRequest{plist: &request}, nil
	case "hcl":
		request := hclpkg.ProjectionRequestBody()
		return WireProjectionRequest{hcl: &request}, nil
	}
	return WireProjectionRequest{}, newFlowError("cli.data.invalid-request@1",
		"no default projection for format family '"+family+"'")
}

// projectValue projects one document under its format request, returning
// the value. Format projection failures keep the format's own stable codes.
func projectValue(document *consema.Document,
	request WireProjectionRequest) (core.Value, *FlowError) {
	switch {
	case request.json != nil:
		jsonDocument, ok := document.AsJSON()
		if !ok {
			return nil, formatMismatch("json")
		}
		result := jsonDocument.Project(request.json)
		if result.Complete != nil {
			return result.Complete.Value, nil
		}
		return nil, projectionFailedSimple(result.Failed.Diagnostics,
			"JSON projection failed")
	case request.toml != nil:
		tomlDocument, ok := document.AsTOML()
		if !ok {
			return nil, formatMismatch("toml")
		}
		result := tomlDocument.Project(*request.toml)
		if result.Complete != nil {
			return result.Complete.Value, nil
		}
		return nil, projectionFailedSimple(result.Failed.Diagnostics,
			"TOML projection failed")
	case request.ini != nil:
		iniDocument, ok := document.AsINI()
		if !ok {
			return nil, formatMismatch("ini")
		}
		result := iniDocument.Project(*request.ini)
		if result.Complete != nil {
			return result.Complete.Value, nil
		}
		return nil, projectionFailedSimple(result.Failed.Diagnostics,
			"INI projection failed")
	case request.properties != nil:
		propertiesDocument, ok := document.AsProperties()
		if !ok {
			return nil, formatMismatch("java-properties")
		}
		result := propertiesDocument.Project(*request.properties)
		if result.Complete != nil {
			return result.Complete.Value, nil
		}
		return nil, projectionFailedSimple(result.Failed.Diagnostics,
			"Properties projection failed")
	case request.yaml != nil:
		yamlDocument, ok := document.AsYAML()
		if !ok {
			return nil, formatMismatch("yaml")
		}
		result := yamlDocument.ProjectValue(*request.yaml)
		if result.Complete != nil {
			return result.Complete.Value, nil
		}
		return nil, stableFailureFlowError(result.Failed,
			"YAML value projection failed")
	case request.xml != nil:
		xmlDocument, ok := document.AsXML()
		if !ok {
			return nil, formatMismatch("xml")
		}
		result := xmlDocument.Project(*request.xml)
		if result.Complete != nil {
			return result.Complete.Value, nil
		}
		return nil, projectionFailedSimple(result.Failed.Diagnostics,
			"XML projection failed")
	case request.plist != nil:
		plistDocument, ok := document.AsPlist()
		if !ok {
			return nil, formatMismatch("plist")
		}
		result := plist.Project(plistDocument, *request.plist)
		if result.Complete != nil {
			return result.Complete.Value, nil
		}
		return nil, projectionFailedSimple(result.Failed.Diagnostics,
			"plist projection failed")
	case request.hcl != nil:
		hclDocument, ok := document.AsHCL()
		if !ok {
			return nil, formatMismatch("hcl")
		}
		result := hclDocument.Project(*request.hcl)
		if result.Complete != nil {
			return result.Complete.Value, nil
		}
		return nil, projectionFailedSimple(result.Failed.Diagnostics,
			"HCL projection failed")
	}
	return nil, newFlowError("cli.data.invalid-request@1",
		"no projection request wired for the source family")
}

func formatMismatch(format string) *FlowError {
	return newFlowError("cli.internal.unclassified@1",
		"the parsed document is not a "+format+" document (facade adapter mismatch)")
}

// projectionFailedSimple fails a projection attempt with the first
// diagnostic's stable code (the minimal failed form; the project command
// uses the full failed-record form of project_cmd.go).
func projectionFailedSimple(diagnostics []*protocol.Diagnostic,
	fallback string) *FlowError {
	code := "core.projection.target-not-applicable@1"
	if len(diagnostics) > 0 {
		code = diagnostics[0].Code
	}
	return newFlowError(code, fallback)
}

// renderPath produces the compact deterministic path spelling (`$`,
// `.key`, `[0]`).
func renderPath(path protocol.ValuePath) string {
	text := "$"
	for _, segment := range path.Segments() {
		switch segment.Kind {
		case "ObjectValue":
			text += "." + segment.Key
		case "SequenceElement":
			text += fmt.Sprintf("[%d]", segment.Index)
		case "EntryKey":
			text += fmt.Sprintf("[key %d]", segment.Index)
		case "EntryValue":
			text += fmt.Sprintf("[value %d]", segment.Index)
		}
	}
	return text
}

// renderValue is the deterministic human rendering of a core.Value
// (single-line, stable).
func renderValue(value core.Value) string {
	switch typed := value.(type) {
	case core.Null:
		return "null"
	case core.Boolean:
		if bool(typed) {
			return "true"
		}
		return "false"
	case core.Integer:
		return typed.String()
	case core.Decimal:
		return typed.String()
	case core.BinaryFloat64:
		return fmt.Sprintf("%v", typed.Float64())
	case core.String:
		return "\"" + escapeText(string(typed)) + "\""
	case core.Bytes:
		return "b\"" + hex.EncodeToString([]byte(typed)) + "\""
	case *core.Array:
		items := typed.Items()
		parts := make([]string, 0, len(items))
		for _, item := range items {
			parts = append(parts, renderValue(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *core.Object:
		entries := typed.Entries()
		parts := make([]string, 0, len(entries))
		for _, entry := range entries {
			parts = append(parts, escapeText(entry.Key)+": "+renderValue(entry.Value))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *core.EntryMapping:
		entries := typed.Entries()
		parts := make([]string, 0, len(entries))
		for _, entry := range entries {
			parts = append(parts, renderValue(entry.Key)+": "+renderValue(entry.Value))
		}
		return "{" + strings.Join(parts, ", ") + ":}"
	default:
		return fmt.Sprintf("%v", value)
	}
}

// escapeText escapes one text for the human view (quotes, backslashes,
// controls).
func escapeText(text string) string {
	var builder strings.Builder
	for _, ch := range text {
		switch ch {
		case '"':
			builder.WriteString("\\\"")
		case '\\':
			builder.WriteString("\\\\")
		case '\n':
			builder.WriteString("\\n")
		case '\r':
			builder.WriteString("\\r")
		case '\t':
			builder.WriteString("\\t")
		default:
			if ch < 0x20 {
				fmt.Fprintf(&builder, "\\u{%x}", uint32(ch))
			} else {
				builder.WriteRune(ch)
			}
		}
	}
	return builder.String()
}
