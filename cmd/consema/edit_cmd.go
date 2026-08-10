package main

// `consema edit` (single-file dry-run) and the shared `cli.edit-request@1`
// request vocabulary consumed by both the edit and the plan commands (RFC
// 0015 §6.1 edit row; mirror of the Rust bin's edit_cmd.rs).
//
// The edit command is **dry-run only**: parse the source file under the
// `--profile` selection → build the format's typed EditTransaction through
// the facade public API → dry_run → emit the `cli.edit@1` payload record
// embedding the `core.edit-plan@1` record (whose `core.source-patch@2`
// carries the exact replacement preconditions and the target digest) and
// the `core.change-set@1` summary. `--write` is refused as a usage error
// (the commit path is not wired for this command; `apply` is the batch
// write command).
//
// The request arrives via `--request-file` or stdin as canonical tagged
// JSON or PVCE/1 (RFC 0015 §3.2), strictly decoded; the source file is the
// command's positional (exactly one path), and the profile is the
// `--profile` selection.
//
// ```text
// cli.edit-request@1 (CLI-local, strict exact-fields decode):
//   schema      String   "cli.edit-request@1" (first field)
//   operations  [{
//     operation  { id: String, version: Integer }   must be published by the
//                                                   selected profile's facade
//                                                   operation registry
//     target     { kind: "document" | "section" | "entry", ... }
//     arguments  { ... }                            exact per-operation fields
//   }]
// ```
//
// The wired operation vocabulary is the frozen INI family surface:
// `ini.edit.replace-semantic-value@1` (arguments `value`,
// `representation_policy`), `ini.edit.replace-literal-value@1` (`literal`,
// lowercase hex), `ini.edit.remove-entry@1`, `ini.edit.rename-entry@1`
// (`key`), `ini.edit.insert-section@1` (`name`, `placement`),
// `ini.edit.remove-section@1`, `ini.edit.rename-section@1` (`name`), and
// `ini.edit.insert-entry@1` (`key`, `value`, `placement`). Every operation
// id/version is validated against the facade's per-profile operation
// registry (hard gate 1: the CLI declares no format knowledge of its own).
//
// Failure classes: recovered base documents are data errors carrying the
// format's own parse diagnostics (RFC 0015 §5.1); operations the request
// vocabulary cannot express are data errors (cli.data.invalid-request@1);
// dry-run validation failures keep the format's stable core.edit.* codes,
// which classify as precondition (RFC 0015 §5.1 edit-conflict row).
//
// Redaction: the human view renders each operation's target locator and
// value-bearing arguments through the presentation policy (RFC 0015 §11);
// the machine payload is never redacted (RFC 0015 §8.3; hard gate 3).

import (
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	consema "consema.dev/consema"
	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/ini"
	"consema.dev/consema/protocol"
)

// editRequestSchema is the CLI-local edit-request schema shared by edit and
// plan.
const editRequestSchema = "cli.edit-request@1"

// EditRequestInput is one fully decoded strict edit request
// (cli.edit-request@1).
type EditRequestInput struct {
	// Profile is the resolved source profile.
	Profile document.ProfileId
	// Operations are the ordered operation requests.
	Operations []OperationSpec
}

// OperationSpec is one decoded operation request (id/version validated
// against the facade operation registry of the selected profile).
type OperationSpec struct {
	// ID is the exact operation id (e.g. `ini.edit.replace-semantic-value`).
	ID string
	// Version is the exact operation version.
	Version uint32
	// Target is the CLI-local stable target locator.
	Target TargetLocator
	// Kind is the typed per-operation arguments.
	Kind OperationKind
}

// TargetLocator is the CLI-local stable target locator (RFC 0015 §8.2
// `operations` target).
type TargetLocator struct {
	// Kind is "document", "section", or "entry".
	Kind string
	// Section is the decoded section name of entry targets (empty for the
	// default section; only for entry targets).
	Section *string
	// Key is the decoded entry key of entry targets.
	Key string
	// Name is the decoded section name of section targets.
	Name string
	// Occurrence is the 0-based occurrence ordinal.
	Occurrence uint64
}

// OperationKind is the typed per-operation arguments of the wired INI
// vocabulary.
type OperationKind struct {
	// Name is the frozen operation id.
	Name string
	// Value is the new stored string value of replace-semantic-value and
	// insert-entry.
	Value string
	// Literal is the exact replacement bytes of replace-literal-value.
	Literal []byte
	// Key is the new decoded key of rename-entry / insert-entry.
	Key string
	// SectionName is the new decoded section name of insert-section /
	// rename-section.
	SectionName string
	// Policy is the representation policy name of replace-semantic-value.
	Policy string
	// Placement is "start" or "end" of insert-section / insert-entry.
	Placement string
}

// FilePlanFailure is one per-file dry-run failure (never aborts a plan
// batch; RFC 0015 §8.2).
type FilePlanFailure struct {
	// Code is the stable diagnostic code.
	Code string
	// Message is the deterministic human message (stderr line).
	Message string
	// Diagnostics are the ordered registry-bound envelope/manifest
	// diagnostics.
	Diagnostics []*protocol.Diagnostic
}

func (f *FilePlanFailure) intoFlowError() *FlowError {
	flowError := newFlowError(f.Code, f.Message)
	if len(f.Diagnostics) > 0 {
		flowError = flowError.withDiagnostics(f.Diagnostics)
	}
	return flowError
}

func internalPlanFailure(path, message string) *FilePlanFailure {
	flowError := newFlowError("cli.internal.unclassified@1", path+": "+message)
	return &FilePlanFailure{
		Code:        flowError.Code,
		Message:     flowError.Message,
		Diagnostics: flowError.Diagnostics,
	}
}

func planFailureFromFlowError(error *FlowError) *FilePlanFailure {
	return &FilePlanFailure{
		Code:        error.Code,
		Message:     error.Message,
		Diagnostics: error.Diagnostics,
	}
}

// PreparedEdit is one prepared per-file edit: the parsed document alive
// plus the validated transaction.
type PreparedEdit struct {
	// Document is the parsed facade document.
	Document *consema.Document
	// Transaction is the typed INI edit transaction.
	Transaction *ini.EditTransaction
	// InIDocument is the typed INI document adapter.
	InIDocument *ini.Document
}

// prepareEdit reads, parses, complete-gates, and builds the typed INI
// transaction for one file (the shared edit/plan per-file pipeline).
func prepareEdit(input *EditRequestInput, path string,
	parsed *ParsedArgs) (*PreparedEdit, *FilePlanFailure) {
	cap := uint64(protocol.DefaultProtocolLimits().MaxBytes)
	if parsed.maxBytes != nil {
		cap = *parsed.maxBytes
	}
	source, err := readSourceCapped(path, cap)
	if err != nil {
		return nil, planFailureFromFlowError(err)
	}
	document, err := parseDocument(source, &input.Profile)
	if err != nil {
		return nil, planFailureFromFlowError(err)
	}
	if err := requireComplete(document, path); err != nil {
		return nil, planFailureFromFlowError(err)
	}
	iniDocument, ok := document.AsINI()
	if !ok {
		return nil, internalPlanFailure(path, "the parsed document is not an INI document")
	}
	builder := ini.NewEditTransactionBuilder(iniDocument)
	for _, operation := range input.Operations {
		if applyErr := applyOperation(builder, iniDocument, &operation); applyErr != nil {
			return nil, applyErr
		}
	}
	return &PreparedEdit{
		Document:    document,
		Transaction: builder.Build(),
		InIDocument: iniDocument,
	}, nil
}

// dryRunPlan dry-runs one prepared edit (the SDK's own dry-run/commit-
// equivalence contract: the plan's replacements and target digest are
// exactly what a future commit would write).
func dryRunPlan(prepared *PreparedEdit, path string) (*document.EditPlan, *FilePlanFailure) {
	plan, failure := prepared.InIDocument.DryRun(prepared.Transaction, path)
	if failure != nil {
		return nil, editPlanFailure(path, failure)
	}
	return plan, nil
}

// applyOperation applies one operation request to the transaction builder.
// Target resolution failures are core.edit.target-not-found@1 — the SDK's
// own stable code for exactly this condition.
func applyOperation(builder *ini.EditTransactionBuilder,
	iniDocument *ini.Document, operation *OperationSpec) *FilePlanFailure {
	switch operation.Kind.Name {
	case "ini.edit.replace-semantic-value":
		target, err := resolveEntryTarget(iniDocument, &operation.Target)
		if err != nil {
			return err
		}
		builder.SemanticValue(target, operation.Kind.Value,
			representationPolicy(operation.Kind.Policy))
	case "ini.edit.replace-literal-value":
		target, err := resolveEntryTarget(iniDocument, &operation.Target)
		if err != nil {
			return err
		}
		builder.LiteralValue(target, operation.Kind.Literal)
	case "ini.edit.remove-entry":
		target, err := resolveEntryTarget(iniDocument, &operation.Target)
		if err != nil {
			return err
		}
		builder.RemoveEntry(target)
	case "ini.edit.rename-entry":
		target, err := resolveEntryTarget(iniDocument, &operation.Target)
		if err != nil {
			return err
		}
		builder.RenameEntry(target, operation.Kind.Key)
	case "ini.edit.insert-section":
		builder.InsertSection(iniDocument.NodeRef(), operation.Kind.SectionName,
			associationPlacement(operation.Kind.Placement))
	case "ini.edit.remove-section":
		target, err := resolveSectionTarget(iniDocument, &operation.Target)
		if err != nil {
			return err
		}
		builder.RemoveSection(target)
	case "ini.edit.rename-section":
		target, err := resolveSectionTarget(iniDocument, &operation.Target)
		if err != nil {
			return err
		}
		builder.RenameSection(target, operation.Kind.SectionName)
	case "ini.edit.insert-entry":
		target, err := resolveSectionTarget(iniDocument, &operation.Target)
		if err != nil {
			return err
		}
		builder.InsertEntry(target, operation.Kind.Key, operation.Kind.Value,
			associationPlacement(operation.Kind.Placement))
	}
	return nil
}

func representationPolicy(name string) ini.RepresentationPolicy {
	switch name {
	case "exact-literal":
		return ini.RepresentationPolicyExactLiteral
	case "preserve-compatible":
		return ini.RepresentationPolicyPreserveCompatible
	case "canonical-for-profile":
		return ini.RepresentationPolicyCanonicalForProfile
	case "preserve-else-canonical":
		return ini.RepresentationPolicyPreserveElseCanonical
	}
	return ini.RepresentationPolicyPreserveCompatible
}

func associationPlacement(name string) document.AssociationPlacement {
	if name == "start" {
		return document.PlacementAtStart()
	}
	return document.PlacementAtEnd()
}

// resolveEntryTarget resolves the occurrence-th entry with the key in the
// section.
func resolveEntryTarget(iniDocument *ini.Document,
	target *TargetLocator) (document.NodeRef, *FilePlanFailure) {
	if target.Kind != "entry" {
		return document.NodeRef{}, internalPlanFailure("locator",
			"an entry operation requires an entry target")
	}
	seen := uint64(0)
	for _, entry := range iniDocument.Entries() {
		section, ok := iniDocument.Section(entry.Section())
		if !ok {
			return document.NodeRef{}, internalPlanFailure("locator",
				"an entry references an unresolvable section")
		}
		inSection := false
		if target.Section == nil {
			inSection = section.IsDefault()
		} else {
			inSection = section.Name() == *target.Section
		}
		if inSection && entry.Key() == target.Key {
			if seen == target.Occurrence {
				return entry.NodeRef(), nil
			}
			seen++
		}
	}
	return document.NodeRef{}, targetNotFound(target)
}

// resolveSectionTarget resolves the occurrence-th section with the name.
func resolveSectionTarget(iniDocument *ini.Document,
	target *TargetLocator) (document.NodeRef, *FilePlanFailure) {
	if target.Kind != "section" {
		return document.NodeRef{}, internalPlanFailure("locator",
			"a section operation requires a section target")
	}
	seen := uint64(0)
	for _, section := range iniDocument.Sections() {
		if section.Name() == target.Name {
			if seen == target.Occurrence {
				return section.NodeRef(), nil
			}
			seen++
		}
	}
	return document.NodeRef{}, targetNotFound(target)
}

func targetNotFound(target *TargetLocator) *FilePlanFailure {
	flowError := newFlowError("core.edit.target-not-found@1",
		"edit target '"+targetLocatorText(target)+"' does not exist in the source")
	return planFailureFromFlowError(flowError)
}

func targetLocatorText(target *TargetLocator) string {
	switch target.Kind {
	case "document":
		return "document"
	case "section":
		return fmt.Sprintf("section '%s'#%d", target.Name, target.Occurrence)
	case "entry":
		section := "(default)"
		if target.Section != nil {
			section = *target.Section
		}
		return fmt.Sprintf("entry '%s':'%s'#%d", section, target.Key, target.Occurrence)
	}
	return "unknown"
}

// editPlanFailure maps one format-owned edit failure to its stable code
// (RFC 0015 §5.2: format-layer codes pass through unchanged).
func editPlanFailure(path string, failure *ini.EditFailure) *FilePlanFailure {
	flowError := stableFailureFlowError(failure,
		"edit dry-run failed for '"+path+"'")
	return planFailureFromFlowError(flowError)
}

// decodeEditRequest strictly decodes one `cli.edit-request@1` and validates
// the whole operation vocabulary against the facade registry of the
// selected profile.
func decodeEditRequest(bytes []byte, parsed *ParsedArgs) (*EditRequestInput, *FlowError) {
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
			"cli.edit-request@1 must be an Object")
	}
	entries := object.Entries()
	if len(entries) != 2 || entries[0].Key != "schema" ||
		entries[0].Value != core.String(editRequestSchema) {
		return nil, newFlowError("cli.data.invalid-request@1",
			"schema must be the first field with value \"cli.edit-request@1\"")
	}
	if entries[1].Key != "operations" {
		return nil, newFlowError("cli.data.invalid-request@1",
			"operations must be the second field")
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
	if family != "ini" {
		return nil, newFlowError("cli.data.invalid-request@1",
			fmt.Sprintf("the %s edit surface is not wired in this build: the request "+
				"vocabulary maps the ini family only (the facade exposes typed edit "+
				"transactions for %s, but no operation request mapping yet)",
				family, family))
	}
	operationValues, ok := entries[1].Value.(*core.Array)
	if !ok {
		return nil, newFlowError("cli.data.invalid-request@1",
			"operations must be a Sequence")
	}
	operations := make([]OperationSpec, 0, len(operationValues.Items()))
	for index, item := range operationValues.Items() {
		operation, decodeErr := decodeOperation(item, &profile, index)
		if decodeErr != nil {
			return nil, decodeErr
		}
		operations = append(operations, *operation)
	}
	return &EditRequestInput{Profile: profile, Operations: operations}, nil
}

// decodeOperation decodes one operation request: exact fields
// [operation, target, arguments], id/version validated against the facade
// operation registry, typed arguments per operation.
func decodeOperation(value core.Value, profile *document.ProfileId,
	index int) (*OperationSpec, *FlowError) {
	path := fmt.Sprintf("operations[%d]", index)
	object, ok := value.(*core.Object)
	if !ok {
		return nil, invalidRequest(path + " must be an Object")
	}
	entries := object.Entries()
	if len(entries) != 3 || entries[0].Key != "operation" ||
		entries[1].Key != "target" || entries[2].Key != "arguments" {
		return nil, invalidRequest(path + " requires exactly operation/target/arguments in order")
	}
	id, version, err := decodeReference(entries[0].Value, path+".operation")
	if err != nil {
		return nil, err
	}
	if err := validateRegistryOperation(profile, id, version, path); err != nil {
		return nil, err
	}
	target, err := decodeTarget(entries[1].Value, path+".target")
	if err != nil {
		return nil, err
	}
	kind, err := decodeArguments(id, entries[2].Value, path+".arguments")
	if err != nil {
		return nil, err
	}
	return &OperationSpec{ID: id, Version: version, Target: *target, Kind: *kind}, nil
}

// decodeReference decodes the {id, version} operation reference.
func decodeReference(value core.Value, path string) (string, uint32, *FlowError) {
	object, ok := value.(*core.Object)
	if !ok {
		return "", 0, invalidRequest(path + " must be an Object")
	}
	entries := object.Entries()
	if len(entries) != 2 || entries[0].Key != "id" || entries[1].Key != "version" {
		return "", 0, invalidRequest(path + " requires exactly {id, version}")
	}
	id, ok := entries[0].Value.(core.String)
	if !ok {
		return "", 0, invalidRequest(path + ".id must be a String")
	}
	version, ok := unsignedValue(entries[1].Value)
	if !ok || version == 0 {
		return "", 0, invalidRequest(path + ".version must be a positive Integer")
	}
	return string(id), uint32(version), nil
}

// validateRegistryOperation validates the operation id/version against the
// profile's facade operation registry (RFC 0015 hard gate 1: the CLI's only
// knowledge of operations comes from the facade).
func validateRegistryOperation(profile *document.ProfileId, id string,
	version uint32, path string) *FlowError {
	registry, ok := consema.OperationRegistryFor(*profile)
	if !ok {
		return invalidRequest("profile '" + profile.ID() + "' publishes no operation registry")
	}
	published := false
	for _, descriptor := range registry.Operations() {
		descriptorID, descriptorVersion := splitVersionedID(descriptor.ID())
		if descriptorID == id && descriptorVersion == version {
			published = true
			break
		}
	}
	if !published {
		return invalidRequest(fmt.Sprintf("%s: operation '%s@%d' is not published by profile '%s'",
			path, id, version, profile.ID()))
	}
	return nil
}

// decodeTarget decodes the target locator (exact fields per kind).
func decodeTarget(value core.Value, path string) (*TargetLocator, *FlowError) {
	object, ok := value.(*core.Object)
	if !ok {
		return nil, invalidRequest(path + " must be an Object")
	}
	entries := object.Entries()
	if len(entries) == 0 || entries[0].Key != "kind" {
		return nil, invalidRequest(path + " requires kind as the first field")
	}
	kind, ok := entries[0].Value.(core.String)
	if !ok {
		return nil, invalidRequest(path + ".kind must be a String")
	}
	switch string(kind) {
	case "document":
		if len(entries) != 1 {
			return nil, invalidRequest(path + " document targets carry no further fields")
		}
		return &TargetLocator{Kind: "document"}, nil
	case "section":
		if len(entries) != 3 || entries[1].Key != "name" || entries[2].Key != "occurrence" {
			return nil, invalidRequest(path + " section targets require exactly name/occurrence")
		}
		name, err := stringField(entries[1].Value, path+".name")
		if err != nil {
			return nil, err
		}
		occurrence, err := occurrenceField(entries[2].Value, path+".occurrence")
		if err != nil {
			return nil, err
		}
		return &TargetLocator{Kind: "section", Name: name, Occurrence: occurrence}, nil
	case "entry":
		if len(entries) != 4 || entries[1].Key != "section" ||
			entries[2].Key != "key" || entries[3].Key != "occurrence" {
			return nil, invalidRequest(path + " entry targets require exactly section/key/occurrence")
		}
		var section *string
		if _, isNull := entries[1].Value.(core.Null); !isNull {
			text, err := stringField(entries[1].Value, path+".section")
			if err != nil {
				return nil, err
			}
			section = &text
		}
		key, err := stringField(entries[2].Value, path+".key")
		if err != nil {
			return nil, err
		}
		occurrence, err := occurrenceField(entries[3].Value, path+".occurrence")
		if err != nil {
			return nil, err
		}
		return &TargetLocator{Kind: "entry", Section: section, Key: key,
			Occurrence: occurrence}, nil
	}
	return nil, invalidRequest(path + ".kind must be \"document\", \"section\", or \"entry\"")
}

func occurrenceField(value core.Value, path string) (uint64, *FlowError) {
	occurrence, ok := unsignedValue(value)
	if !ok {
		return 0, invalidRequest(path + " must be a non-negative Integer")
	}
	return occurrence, nil
}

func stringField(value core.Value, path string) (string, *FlowError) {
	text, ok := value.(core.String)
	if !ok {
		return "", invalidRequest(path + " must be a String")
	}
	return string(text), nil
}

// decodeArguments decodes the exact per-operation argument fields.
func decodeArguments(id string, value core.Value, path string) (*OperationKind, *FlowError) {
	object, ok := value.(*core.Object)
	if !ok {
		return nil, invalidRequest(path + " must be an Object")
	}
	entries := object.Entries()
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Key)
	}
	namesMatch := func(expected ...string) bool {
		if len(names) != len(expected) {
			return false
		}
		for index, name := range expected {
			if names[index] != name {
				return false
			}
		}
		return true
	}
	switch id {
	case "ini.edit.replace-semantic-value":
		if !namesMatch("value", "representation_policy") {
			return nil, invalidRequest(path + " requires exactly value/representation_policy")
		}
		value, err := stringField(entries[0].Value, path+".value")
		if err != nil {
			return nil, err
		}
		policy, err := stringField(entries[1].Value, path+".representation_policy")
		if err != nil {
			return nil, err
		}
		switch policy {
		case "exact-literal", "preserve-compatible", "canonical-for-profile",
			"preserve-else-canonical":
		default:
			return nil, invalidRequest(path + ".representation_policy must be exact-literal, " +
				"preserve-compatible, canonical-for-profile, or preserve-else-canonical")
		}
		return &OperationKind{Name: id, Value: value, Policy: policy}, nil
	case "ini.edit.replace-literal-value":
		if !namesMatch("literal") {
			return nil, invalidRequest(path + " requires exactly literal")
		}
		hexText, err := stringField(entries[0].Value, path+".literal")
		if err != nil {
			return nil, err
		}
		literal, ok := decodeHex(hexText)
		if !ok {
			return nil, invalidRequest(path + ".literal must be even-length lowercase hex")
		}
		return &OperationKind{Name: id, Literal: literal}, nil
	case "ini.edit.remove-entry":
		if !namesMatch() {
			return nil, invalidRequest(path + " carries no arguments")
		}
		return &OperationKind{Name: id}, nil
	case "ini.edit.rename-entry":
		if !namesMatch("key") {
			return nil, invalidRequest(path + " requires exactly key")
		}
		key, err := stringField(entries[0].Value, path+".key")
		if err != nil {
			return nil, err
		}
		return &OperationKind{Name: id, Key: key}, nil
	case "ini.edit.insert-section":
		if !namesMatch("name", "placement") {
			return nil, invalidRequest(path + " requires exactly name/placement")
		}
		name, err := stringField(entries[0].Value, path+".name")
		if err != nil {
			return nil, err
		}
		placement, err := placementField(entries[1].Value, path+".placement")
		if err != nil {
			return nil, err
		}
		return &OperationKind{Name: id, SectionName: name, Placement: placement}, nil
	case "ini.edit.remove-section":
		if !namesMatch() {
			return nil, invalidRequest(path + " carries no arguments")
		}
		return &OperationKind{Name: id}, nil
	case "ini.edit.rename-section":
		if !namesMatch("name") {
			return nil, invalidRequest(path + " requires exactly name")
		}
		name, err := stringField(entries[0].Value, path+".name")
		if err != nil {
			return nil, err
		}
		return &OperationKind{Name: id, SectionName: name}, nil
	case "ini.edit.insert-entry":
		if !namesMatch("key", "value", "placement") {
			return nil, invalidRequest(path + " requires exactly key/value/placement")
		}
		key, err := stringField(entries[0].Value, path+".key")
		if err != nil {
			return nil, err
		}
		value, err := stringField(entries[1].Value, path+".value")
		if err != nil {
			return nil, err
		}
		placement, err := placementField(entries[2].Value, path+".placement")
		if err != nil {
			return nil, err
		}
		return &OperationKind{Name: id, Key: key, Value: value, Placement: placement}, nil
	}
	return nil, invalidRequest("operation '" + id + "' is not wired in the request vocabulary")
}

// placementField decodes the closed placement set {start, end} (anchor
// placement is not wired).
func placementField(value core.Value, path string) (string, *FlowError) {
	text, ok := value.(core.String)
	if !ok {
		return "", invalidRequest(path + " must be a String")
	}
	switch string(text) {
	case "start", "end":
		return string(text), nil
	}
	return "", invalidRequest(path + " must be \"start\" or \"end\" (anchor placement " +
		"is not wired in this build)")
}

func invalidRequest(message string) *FlowError {
	return newFlowError("cli.data.invalid-request@1", message)
}

// redactPolicy compiles the presentation redaction policy from the parsed
// arguments (RFC 0015 §11.2: an invalid --redact-keys pattern is a usage
// error, cli.usage.redaction-pattern@1, exit 1).
func compileRedactPolicy(parsed *ParsedArgs) (*redactPolicy, *FlowError) {
	policy := conservativePolicy()
	if parsed.showSecrets {
		policy = showSecretsPolicy()
	}
	if parsed.redactKeys != nil {
		var err *redactPatternError
		policy, err = policy.withExtraPatterns([]string{*parsed.redactKeys})
		if err != nil {
			return nil, usageFlowError(err.Code(), err.Error())
		}
	}
	return &policy, nil
}

// runEdit runs `consema edit` (dry-run only; the source file is the
// positional, the operation request arrives via --request-file or stdin).
func runEdit(parsed *ParsedArgs, stdout, stderr io.Writer) uint8 {
	if parsed.write {
		error := usageFlowError("cli.usage.invalid-argument@1",
			"flag '--write' is not available in this build: edit is dry-run only "+
				"(the batch commit path is the apply command)")
		return emitFailure(protocol.CommandEdit, parsed, error, stdout, stderr)
	}
	if parsed.output != nil {
		error := usageFlowError("cli.usage.invalid-argument@1",
			"flag '--output' is not available for edit: the dry-run result is "+
				"emitted to stdout only")
		return emitFailure(protocol.CommandEdit, parsed, error, stdout, stderr)
	}
	policy, err := compileRedactPolicy(parsed)
	if err != nil {
		return emitFailure(protocol.CommandEdit, parsed, err, stdout, stderr)
	}
	request, err := readRequestBytes(parsed)
	if err != nil {
		return emitFailure(protocol.CommandEdit, parsed, err, stdout, stderr)
	}
	return runEditWithRequest(parsed, request, policy, stdout, stderr)
}

// runEditWithRequest runs `consema edit` against already-read request bytes.
func runEditWithRequest(parsed *ParsedArgs, request []byte,
	policy *redactPolicy, stdout, stderr io.Writer) uint8 {
	if parsed.write {
		error := usageFlowError("cli.usage.invalid-argument@1",
			"flag '--write' is not available in this build: edit is dry-run only "+
				"(the batch commit path is the apply command)")
		return emitFailure(protocol.CommandEdit, parsed, error, stdout, stderr)
	}
	if parsed.output != nil {
		error := usageFlowError("cli.usage.invalid-argument@1",
			"flag '--output' is not available for edit: the dry-run result is "+
				"emitted to stdout only")
		return emitFailure(protocol.CommandEdit, parsed, error, stdout, stderr)
	}
	input, decodeErr := decodeEditRequest(request, parsed)
	if decodeErr != nil {
		return emitFailure(protocol.CommandEdit, parsed, decodeErr, stdout, stderr)
	}
	path := parsed.positionals[0]
	prepared, prepareErr := prepareEdit(input, path, parsed)
	if prepareErr != nil {
		return emitFailure(protocol.CommandEdit, parsed, prepareErr.intoFlowError(), stdout, stderr)
	}
	plan, planErr := dryRunPlan(prepared, path)
	if planErr != nil {
		return emitFailure(protocol.CommandEdit, parsed, planErr.intoFlowError(), stdout, stderr)
	}
	planValue, err := editPlanValue(plan)
	if err != nil {
		return internalFailure("edit", "edit-plan encoding failed: "+err.Error(), stderr)
	}
	changeSet, changeSetErr := changeSetValue(prepared, path)
	if changeSetErr != nil {
		return emitFailure(protocol.CommandEdit, parsed, changeSetErr.intoFlowError(), stdout, stderr)
	}
	changeSetValue, err := changeSet.ToValue()
	if err != nil {
		return internalFailure("edit", "change-set encoding failed: "+err.Error(), stderr)
	}
	payload := cliEditPayload(planValue, changeSetValue)
	if parsed.json {
		if emitErr := emitCommandEnvelope(protocol.CommandEdit, protocol.ExitSuccess,
			payload, nil, parsed, stdout); emitErr != nil {
			return internalFailure("edit", emitErr.Error(), stderr)
		}
		return protocol.ExitSuccess.ExitCode()
	}
	redactedCount, writeErr := writeEditReport(input, plan, path, policy, stdout)
	if writeErr != nil {
		return internalFailure("edit", writeErr.Error(), stderr)
	}
	redactionNotice("edit", redactedCount, stderr)
	return protocol.ExitSuccess.ExitCode()
}

// digestFromDocumentDigest carries one document-layer digest into the
// wire-form digest (the identical SHA-256 fact).
func digestFromDocumentDigest(digest document.ContentDigest) protocol.ContentDigest {
	return protocol.ContentDigestFromBytes(digest.Bytes())
}

// editPlanValue encodes the `core.edit-plan@1` record from one transferable
// dry-run plan (RFC 0015 §6.1 edit row; the wire field order of the Rust
// protocol operation.rs to_value).
func editPlanValue(plan *document.EditPlan) (core.Value, error) {
	operations := make([]core.Value, 0, len(plan.Operations()))
	for _, operation := range plan.Operations() {
		summary := make([]core.Entry, 0, len(operation.Summary))
		for name, value := range operation.Summary {
			summary = append(summary, core.Entry{Key: name, Value: core.String(value)})
		}
		summaryObject, err := core.NewObject(summary...)
		if err != nil {
			return nil, err
		}
		record, err := core.NewObject(
			core.Entry{Key: "operation", Value: referenceValue(
				operation.Operation.ID(), operation.Operation.Version())},
			core.Entry{Key: "summary", Value: summaryObject},
		)
		if err != nil {
			return nil, err
		}
		operations = append(operations, record)
	}
	replacements := make([]core.Value, 0, len(plan.Replacements()))
	for _, replacement := range plan.Replacements() {
		record, err := core.NewObject(
			core.Entry{Key: "old_start", Value: integerValueOf(uint64(replacement.OldStart()))},
			core.Entry{Key: "old_end", Value: integerValueOf(uint64(replacement.OldEnd()))},
			core.Entry{Key: "original", Value: core.NewBytes(replacement.Original())},
			core.Entry{Key: "replacement", Value: core.NewBytes(replacement.Replacement())},
			core.Entry{Key: "redact_original", Value: core.Boolean(replacement.RedactOriginal())},
			core.Entry{Key: "redact_replacement", Value: core.Boolean(replacement.RedactReplacement())},
		)
		if err != nil {
			return nil, err
		}
		replacements = append(replacements, record)
	}
	report := make([]core.Value, 0, len(plan.Diagnostics()))
	for _, diagnostic := range plan.Diagnostics() {
		value, err := diagnostic.ToValue()
		if err != nil {
			return nil, err
		}
		report = append(report, value)
	}
	baseDigest := digestFromDocumentDigest(plan.SourcePatch().BaseDigest())
	profile := plan.TargetProfile()
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.edit-plan@1")},
		core.Entry{Key: "source_id", Value: core.String(plan.SourceID())},
		core.Entry{Key: "base_digest", Value: digestRecord(baseDigest)},
		core.Entry{Key: "profile", Value: referenceValue(profile.ID(), profile.Version())},
		core.Entry{Key: "operations", Value: core.NewArray(operations...)},
		core.Entry{Key: "replacements", Value: core.NewArray(replacements...)},
		core.Entry{Key: "target_digest", Value: digestRecord(digestFromDocumentDigest(plan.TargetDigest()))},
		core.Entry{Key: "report", Value: core.NewArray(report...)},
	)
}

// changeSetValue externalizes the `core.change-set@1` summary by re-
// committing the same validated transaction in memory (pure computation;
// the format crates prove the dry-run/commit equivalence). The locator
// closure resolves every mapped node against the old and new INI documents
// with the CLI's stable caller locators, so no process-local identity ever
// reaches the wire (RFC 0015 §3.3).
func changeSetValue(prepared *PreparedEdit, path string) (*protocol.ChangeSetMessage, *FilePlanFailure) {
	commit, failure := prepared.InIDocument.Commit(prepared.Transaction)
	if failure != nil {
		return nil, editPlanFailure(path, failure)
	}
	oldDocument := prepared.InIDocument
	newDocument := commit.Document
	locator := func(node document.NodeRef) (string, bool) {
		if locator := iniLocator(oldDocument, node); locator != "" {
			return locator, true
		}
		if locator := iniLocator(newDocument, node); locator != "" {
			return locator, true
		}
		return "", false
	}
	message, err := consema.ChangeSetMessageFromDocument(&commit.ChangeSet, path, path, locator)
	if err != nil {
		return nil, internalPlanFailure(path,
			"change-set externalization failed: "+err.Error())
	}
	return message, nil
}

// iniLocator returns the CLI-stable caller locator of one INI node (RFC
// 0015 §8.2/§3.3: caller-defined stable locators, no process-local
// handles).
func iniLocator(iniDocument *ini.Document, node document.NodeRef) string {
	if node == iniDocument.NodeRef() {
		return "document"
	}
	if section, ok := iniDocument.Section(node); ok {
		return "section:" + section.Name()
	}
	entry, ok := iniDocument.Entry(node)
	if !ok {
		return ""
	}
	section, ok := iniDocument.Section(entry.Section())
	if !ok {
		return ""
	}
	return "entry:" + section.Name() + ":" + entry.Key()
}

// cliEditPayload builds the fixed `cli.edit@1` payload record (RFC 0015
// §6.1); `committed` is always false (dry-run; --write is refused).
func cliEditPayload(plan, changeSet core.Value) core.Value {
	payload, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("cli.edit@1")},
		core.Entry{Key: "plan", Value: plan},
		core.Entry{Key: "change_set", Value: changeSet},
		core.Entry{Key: "committed", Value: core.Boolean(false)},
	)
	return payload
}

// operationLine renders the one deterministic human line of one operation
// request (target key and section names redacted; value-bearing arguments
// redact under the target entry's key name — the conservative direction of
// RFC 0015 §11.2). Returns the rendered line and the number of values
// replaced in it.
func operationLine(policy *redactPolicy, operation *OperationSpec) (string, uint64) {
	targetText, count := renderTarget(policy, &operation.Target)
	arguments := make([]string, 0, 3)
	renderValueArgument := func(argumentName, value string) string {
		redactionKey := argumentName
		if operation.Target.Kind == "entry" {
			redactionKey = operation.Target.Key
		}
		rendered := redactText(policy, redactionKey, value)
		if rendered.redacted {
			count++
		}
		return rendered.text
	}
	switch operation.Kind.Name {
	case "ini.edit.replace-semantic-value":
		arguments = append(arguments, "value="+renderValueArgument("value", operation.Kind.Value))
		arguments = append(arguments,
			"representation_policy="+operation.Kind.Policy)
	case "ini.edit.replace-literal-value":
		arguments = append(arguments,
			"literal="+renderValueArgument("literal", hex.EncodeToString(operation.Kind.Literal)))
	case "ini.edit.rename-entry":
		arguments = append(arguments, "key="+renderValueArgument("key", operation.Kind.Key))
	case "ini.edit.insert-section":
		arguments = append(arguments,
			"name="+renderValueArgument("name", operation.Kind.SectionName))
		arguments = append(arguments, "placement="+operation.Kind.Placement)
	case "ini.edit.rename-section":
		arguments = append(arguments,
			"name="+renderValueArgument("name", operation.Kind.SectionName))
	case "ini.edit.insert-entry":
		arguments = append(arguments, "key="+renderValueArgument("key", operation.Kind.Key))
		arguments = append(arguments, "value="+renderValueArgument("value", operation.Kind.Value))
		arguments = append(arguments, "placement="+operation.Kind.Placement)
	}
	line := fmt.Sprintf("%s@%d on %s", operation.ID, operation.Version, targetText)
	if len(arguments) > 0 {
		line += "  " + strings.Join(arguments, " ")
	}
	return line, count
}

// renderTarget renders the deterministic human spelling of one target
// locator (key and section names pass through the redaction policy).
func renderTarget(policy *redactPolicy, target *TargetLocator) (string, uint64) {
	switch target.Kind {
	case "document":
		return "document", 0
	case "section":
		rendered := redactText(policy, target.Name, target.Name)
		return fmt.Sprintf("section '%s'%s", rendered.text,
			occurrenceSuffix(target.Occurrence)), boolToUint64(rendered.redacted)
	case "entry":
		sectionText := "(default)"
		redactedCount := uint64(0)
		if target.Section != nil {
			rendered := redactText(policy, *target.Section, *target.Section)
			sectionText = rendered.text
			redactedCount += boolToUint64(rendered.redacted)
		}
		keyRendered := redactText(policy, target.Key, target.Key)
		return fmt.Sprintf("entry '%s':'%s'%s", sectionText, keyRendered.text,
			occurrenceSuffix(target.Occurrence)), redactedCount + boolToUint64(keyRendered.redacted)
	}
	return "unknown", 0
}

func occurrenceSuffix(occurrence uint64) string {
	if occurrence == 0 {
		return ""
	}
	return fmt.Sprintf("#%d", occurrence)
}

func boolToUint64(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}

// writeEditReport writes the deterministic human edit report (same facade
// result as the machine payload). Returns the number of redacted values.
func writeEditReport(input *EditRequestInput, plan *document.EditPlan,
	path string, policy *redactPolicy, stdout io.Writer) (uint64, error) {
	var text strings.Builder
	redacted := uint64(0)
	fmt.Fprintf(&text, "edit dry-run (%s): %s\n", input.Profile.ID(), path)
	for _, operation := range input.Operations {
		line, count := operationLine(policy, &operation)
		redacted += count
		fmt.Fprintf(&text, "  %s\n", line)
	}
	patch := plan.SourcePatch()
	fmt.Fprintf(&text, "  base %s target %s replacements: %d\n",
		patch.BaseDigest().Hex(), plan.TargetDigest().Hex(), len(plan.Replacements()))
	text.WriteString("  committed: no\n")
	if _, err := io.WriteString(stdout, text.String()); err != nil {
		return 0, err
	}
	return redacted, nil
}

// redactionNotice writes the one deterministic stderr redaction notice (RFC
// 0015 §4.4: only when the human view replaced any value).
func redactionNotice(command string, count uint64, stderr io.Writer) {
	if count > 0 {
		fmt.Fprintf(stderr,
			"consema: %s: redacted %d value(s) in the human view (--show-secrets reveals)\n",
			command, count)
	}
}

// PlanRenderItem is the summary facts of one planned file for the human
// plan view.
type PlanRenderItem struct {
	// Path is the user-supplied path spelling.
	Path string
	// Planned reports whether the file planned successfully.
	Planned bool
	// OperationLines are the operation request lines (redacted) of a
	// planned file.
	OperationLines []string
	// BaseDigest is the base digest hex of a planned file.
	BaseDigest string
	// TargetDigest is the target digest hex of a planned file.
	TargetDigest string
	// Replacements is the replacement count of a planned file.
	Replacements int
	// FailureCode is the failure code of a failed file.
	FailureCode string
	// Redacted is the number of values redacted in this item's lines.
	Redacted uint64
}

// planRenderItem builds the human plan-view item of one manifest file entry
// (rendering from the same request operations and plan facts as the machine
// manifest; RFC 0015 §2.4).
func planRenderItem(input *EditRequestInput,
	entry *protocol.BatchPlanFileEntry, policy *redactPolicy) PlanRenderItem {
	switch entry.Status() {
	case protocol.PlanStatusPlanned:
		operationLines := make([]string, 0, len(input.Operations))
		redacted := uint64(0)
		for _, operation := range input.Operations {
			line, count := operationLine(policy, &operation)
			redacted += count
			operationLines = append(operationLines, line)
		}
		patch := entry.SourcePatch()
		return PlanRenderItem{
			Path:           entry.Path(),
			Planned:        true,
			OperationLines: operationLines,
			BaseDigest:     patch.BaseDigest.Hex(),
			TargetDigest:   patch.TargetDigest.Hex(),
			Replacements:   len(patch.Replacements),
			Redacted:       redacted,
		}
	case protocol.PlanStatusFailed:
		code := "unknown"
		if entry.FailureCode() != nil {
			code = *entry.FailureCode()
		}
		return PlanRenderItem{
			Path:        entry.Path(),
			Planned:     false,
			FailureCode: code,
		}
	}
	return PlanRenderItem{Path: entry.Path(), Planned: false}
}

// writePlanReport writes the deterministic human plan report (RFC 0015
// §2.4; per-item redaction). Returns the total number of redacted values.
func writePlanReport(items []PlanRenderItem, stdout io.Writer) (uint64, error) {
	var text strings.Builder
	total := uint64(0)
	fmt.Fprintf(&text, "consema plan: %d file(s)\n", len(items))
	for _, item := range items {
		total += item.Redacted
		if item.Planned {
			fmt.Fprintf(&text, "  %s: planned\n", item.Path)
			for _, line := range item.OperationLines {
				fmt.Fprintf(&text, "    %s\n", line)
			}
			fmt.Fprintf(&text, "    base sha256:%s target sha256:%s replacements: %d\n",
				item.BaseDigest, item.TargetDigest, item.Replacements)
		} else {
			fmt.Fprintf(&text, "  %s: failed %s\n", item.Path, item.FailureCode)
		}
	}
	if _, err := io.WriteString(stdout, text.String()); err != nil {
		return 0, err
	}
	return total, nil
}
