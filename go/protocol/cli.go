package protocol

import (
	"crypto/sha256"
	"strings"

	"consema.dev/consema/core"
)

// This file implements the CLI machine-protocol payloads of RFC 0015
// §4/§8/§9 (consema-rs/consema-protocol/src/cli.rs): the core.cli-output@1
// envelope, the core.batch-plan@1 manifest, and the core.batch-result@1
// manifest. Every decoder re-validates the cross constraints (closed
// command and exit-class sets, payload-schema/command consistency,
// redaction consistency, digest equality, per-status presence rules)
// instead of trusting the schema discriminator.
//
// # Bytes note
//
// The core.source-patch@2 record nested in a planned batch-plan entry
// carries Bytes leaves (replacement original/replacement content). The
// fifteen-kind Go value model expresses Bytes (RFC 0016 §4.1), and both
// codec paths carry replacement records with full byte fidelity, exactly as
// the Rust value-level codec does (source.rs:328-357):
//
//   - FromJSON/ToJSON operate on the strict canonical JSON tree: replacement
//     bytes travel as hex, exactly as the Rust transport carries them. This
//     is the primary machine transport of RFC 0015 §3.2 and the path the
//     shared cli-v1 vectors exercise.
//   - FromValue/ToValue (and the PVCE transports built on them) carry the
//     same Bytes leaves through the value model; the Rust and Go encodings
//     are byte-identical on both transports.
//
// The plan manifest's other fields, the CLI envelope, and the batch result
// carry no Bytes leaves and round-trip through all transports.

// CliCommand is one of the eleven formal CLI commands (RFC 0015 §6.1).
type CliCommand string

// The eleven frozen commands.
const (
	CommandInspect      CliCommand = "inspect"
	CommandCapabilities CliCommand = "capabilities"
	CommandQuery        CliCommand = "query"
	CommandProject      CliCommand = "project"
	CommandMaterialize  CliCommand = "materialize"
	CommandConvert      CliCommand = "convert"
	CommandEdit         CliCommand = "edit"
	CommandPlan         CliCommand = "plan"
	CommandApply        CliCommand = "apply"
	CommandConformance  CliCommand = "conformance"
	CommandExplain      CliCommand = "explain"
)

// Name returns the canonical `command` envelope name.
func (c CliCommand) Name() string { return string(c) }

// ParseCliCommand parses one canonical command name into the closed command
// set.
func ParseCliCommand(name string) (CliCommand, bool) {
	switch CliCommand(name) {
	case CommandInspect, CommandCapabilities, CommandQuery, CommandProject,
		CommandMaterialize, CommandConvert, CommandEdit, CommandPlan,
		CommandApply, CommandConformance, CommandExplain:
		return CliCommand(name), true
	}
	return "", false
}

// PayloadSchemas returns the payload schemas the command may carry
// (RFC 0015 §6.1 table; cli.rs:92-115).
func (c CliCommand) PayloadSchemas() []string {
	switch c {
	case CommandInspect:
		return []string{"cli.inspect@1"}
	case CommandCapabilities:
		return []string{"cli.capabilities@1"}
	case CommandQuery:
		return []string{"core.query-result@1", "core.ini-query-result@1",
			"core.java-properties-query-result@1", "core.yaml-query-result@1",
			"core.graph-query-result@1"}
	case CommandProject:
		return []string{"core.projection-result@1"}
	case CommandMaterialize:
		return []string{"core.materialization-result@2"}
	case CommandConvert:
		return []string{"cli.convert@1"}
	case CommandEdit:
		return []string{"cli.edit@1"}
	case CommandPlan:
		return []string{"core.batch-plan@1"}
	case CommandApply:
		return []string{"core.batch-result@1"}
	case CommandConformance:
		return []string{"cli.conformance@1"}
	case CommandExplain:
		return []string{"cli.explain@1"}
	}
	return nil
}

// Redaction carries the envelope redaction facts (RFC 0015 §11.3;
// cli.rs:117-147).
type Redaction struct {
	redacted bool
	count    uint64
}

// NewRedaction validates the `redacted == (count > 0)` invariant.
func NewRedaction(redacted bool, count uint64) (*Redaction, error) {
	if redacted != (count > 0) {
		return nil, invalid("$.redaction", "redacted must equal (count > 0)")
	}
	return &Redaction{redacted: redacted, count: count}, nil
}

// Redacted reports whether any value was replaced by the `$REDACTED$`
// placeholder.
func (r *Redaction) Redacted() bool { return r.redacted }

// Count returns the number of values replaced in this output.
func (r *Redaction) Count() uint64 { return r.count }

// ContentDigest is the stable SHA-256 identity of exact raw source bytes
// (consema-document/src/source.rs:16-50). The document milestone (G1.1)
// owns the full source model; this is the wire-form digest used by the CLI
// records.
type ContentDigest struct {
	bytes [32]byte
}

// DigestOf computes the digest of exact raw bytes.
func DigestOf(data []byte) ContentDigest {
	return ContentDigest{bytes: sha256.Sum256(data)}
}

// ContentDigestFromBytes constructs a digest from an already decoded 32-byte
// record.
func ContentDigestFromBytes(bytes [32]byte) ContentDigest {
	return ContentDigest{bytes: bytes}
}

// Algorithm returns the digest algorithm identifier frozen by the v1 source
// contract.
func (d ContentDigest) Algorithm() string { return "sha256" }

// Bytes returns the exact 32 digest bytes.
func (d ContentDigest) Bytes() [32]byte { return d.bytes }

// Hex returns the lowercase hexadecimal representation.
func (d ContentDigest) Hex() string {
	const hexDigits = "0123456789abcdef"
	text := make([]byte, 0, 64)
	for _, byte := range d.bytes {
		text = append(text, hexDigits[byte>>4], hexDigits[byte&0x0f])
	}
	return string(text)
}

// Equal reports whether two digests are identical.
func (d ContentDigest) Equal(other ContentDigest) bool { return d.bytes == other.bytes }

// FormatOperationId is a stable format-operation contract identity
// (consema-document operation_registry.rs:10-29).
type FormatOperationId struct {
	id      string
	version uint32
}

// NewFormatOperationId creates an operation identifier.
func NewFormatOperationId(id string, version uint32) *FormatOperationId {
	return &FormatOperationId{id: id, version: version}
}

// ID returns the operation identifier.
func (o *FormatOperationId) ID() string { return o.id }

// Version returns the operation contract version.
func (o *FormatOperationId) Version() uint32 { return o.version }

// EditOperationSummary is one safe, content-free summary of a declared edit
// operation (consema-document edit_plan.rs:35-75). Summary values must not
// contain raw edited values.
type EditOperationSummary struct {
	// Operation is the exact immutable operation ID/version.
	Operation *FormatOperationId
	// Summary is the stable sorted safe summary fields.
	Summary map[string]string
}

// NewEditOperationSummary validates a bounded summary.
func NewEditOperationSummary(operation *FormatOperationId, summary map[string]string) (*EditOperationSummary, error) {
	if len(summary) > 64 {
		return nil, invalid("$.files[].operations", "invalid operation summary")
	}
	for name, value := range summary {
		if !validSummaryName(name) || value == "" || len(value) > 1024 {
			return nil, invalid("$.files[].operations", "invalid operation summary")
		}
	}
	return &EditOperationSummary{Operation: operation, Summary: summary}, nil
}

func validSummaryName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for index := 0; index < len(name); index++ {
		byte := name[index]
		if !(byte >= 'a' && byte <= 'z') && !(byte >= '0' && byte <= '9') && byte != '_' {
			return false
		}
	}
	return true
}

// SourceEncoding is the wire form of one core.source-encoding@1 record
// (protocol/src/source.rs:497-514): the kind spelling and the optional
// Windows code page.
type SourceEncoding struct {
	// Kind is the frozen kind spelling: Binary, Utf8, Utf16Le, Utf16Be,
	// Latin1, or WindowsCodePage.
	Kind string
	// WindowsCodePage is the numeric code page of WindowsCodePage records.
	WindowsCodePage *uint32
}

// EncodingFacts is the source-patch@2 encoding facts record
// (protocol/src/source.rs:598-631). The semantic consistency checks of the
// facts (BOM policy, code-page registration, selected-encoding
// reconciliation) belong to the document milestone; this package validates
// the record structure and carries the facts.
type EncodingFacts struct {
	// ProfileDefault is the encoding assumed without declarations.
	ProfileDefault *SourceEncoding
	// BomPolicy is "DetectUnicode" or "TreatAsContent".
	BomPolicy string
	// Bom is the detected byte-order mark: "Utf8", "Utf16Le", or
	// "Utf16Be"; nil when none.
	Bom *string
	// Declaration is the encoding declared by the source; nil when none.
	Declaration *SourceEncoding
	// CallerOverride is the encoding requested by the caller; nil when none.
	CallerOverride *SourceEncoding
	// Selected is the encoding actually selected.
	Selected *SourceEncoding
}

// SourceReplacement is one structural replacement of a wire source patch
// (consema-document source_patch.rs:31-90).
type SourceReplacement struct {
	// OldStart is the inclusive start offset of the replaced range.
	OldStart uint64
	// OldEnd is the exclusive end offset of the replaced range.
	OldEnd uint64
	// Original is the exact original bytes of the replaced range.
	Original []byte
	// Replacement is the exact new bytes.
	Replacement []byte
	// RedactOriginal reports whether the original is a redaction-sensitive
	// value.
	RedactOriginal bool
	// RedactReplacement reports whether the replacement is a
	// redaction-sensitive value.
	RedactReplacement bool
}

// SourcePatch is the wire form of a source patch (core.source-patch@2
// record; protocol/src/source.rs:222-238, 323-371). The document milestone
// (G1.1) owns the applied patch type; this package carries the transferable
// verification facts so the batch-plan record can round-trip them.
type SourcePatch struct {
	// BaseDigest is the digest of the exact base bytes the patch applies to.
	BaseDigest ContentDigest
	// TargetDigest is the digest of the exact target bytes after applying.
	TargetDigest ContentDigest
	// Encoding is the encoding facts of the patch.
	Encoding EncodingFacts
	// Replacements are the ordered structural replacements.
	Replacements []SourceReplacement
	// Metadata is the deterministic sorted metadata map.
	Metadata map[string]string
}

// BatchPlanFileStatus is one file-level status in a core.batch-plan@1
// manifest (RFC 0015 §8.2; cli.rs:366-374).
type BatchPlanFileStatus string

const (
	// PlanStatusPlanned: the file planned successfully; profile,
	// source_digest, operations, and source_patch are present.
	PlanStatusPlanned BatchPlanFileStatus = "planned"
	// PlanStatusFailed: the file failed to plan; failure_code and
	// diagnostics are present.
	PlanStatusFailed BatchPlanFileStatus = "failed"
)

// BatchPlanFileEntry is one file entry of a core.batch-plan@1 manifest
// (cli.rs:376-541).
type BatchPlanFileEntry struct {
	path         string
	status       BatchPlanFileStatus
	profile      *ProfileReference
	sourceDigest *ContentDigest
	operations   []*EditOperationSummary
	sourcePatch  *SourcePatch
	failureCode  *string
	diagnostics  []*Diagnostic
}

// NewBatchPlanFileEntry validates the per-status presence rules and the
// `source_digest == source_patch.base_digest` cross constraint
// (cli.rs:389-492).
func NewBatchPlanFileEntry(path string, status BatchPlanFileStatus,
	profile *ProfileReference, sourceDigest *ContentDigest,
	operations []*EditOperationSummary, sourcePatch *SourcePatch,
	failureCode *string, diagnostics []*Diagnostic,
	registry ErrorCodeRegistry) (*BatchPlanFileEntry, error) {
	if path == "" || len(path) > 1024 {
		return nil, invalid("$.files[].path", "invalid path")
	}
	for index, operation := range operations {
		if _, err := NewEditOperationSummary(operation.Operation, operation.Summary); err != nil {
			return nil, invalid("$.files[].operations["+uint32String(uint32(index))+"]", err.Error())
		}
	}
	switch status {
	case PlanStatusPlanned:
		if profile == nil || sourceDigest == nil || operations == nil || sourcePatch == nil {
			return nil, invalid("$.files[]",
				"planned entries require profile, source_digest, operations, and source_patch")
		}
		if failureCode != nil || diagnostics != nil {
			return nil, invalid("$.files[]",
				"planned entries cannot carry failure_code or diagnostics")
		}
		if !sourceDigest.Equal(sourcePatch.BaseDigest) {
			return nil, invalid("$.files[].source_digest",
				"source_digest must equal source_patch.base_digest")
		}
	case PlanStatusFailed:
		if profile != nil || sourceDigest != nil || operations != nil || sourcePatch != nil {
			return nil, invalid("$.files[]", "failed entries cannot carry planning facts")
		}
		if failureCode == nil || *failureCode == "" {
			return nil, invalid("$.files[].failure_code",
				"failed entries require a failure_code")
		}
		if diagnostics == nil {
			return nil, invalid("$.files[].diagnostics",
				"failed entries require a diagnostics sequence")
		}
	}
	for index, diagnostic := range diagnostics {
		// The registry binding check mirrors the Rust
		// DiagnosticMessage::from_value_with_registry re-validation
		// (cli.rs:470-481); both codecs carry fix replacement bytes, so the
		// binding check is the code/category rule directly.
		if err := validateDiagnosticCode(diagnostic.Code, diagnostic.Category, registry); err != nil {
			return nil, &ProtocolError{
				Kind:   err.Kind,
				Path:   "$.files[].diagnostics[" + uint32String(uint32(index)) + "]",
				Detail: err.Error(),
			}
		}
	}
	return &BatchPlanFileEntry{
		path:         path,
		status:       status,
		profile:      profile,
		sourceDigest: sourceDigest,
		operations:   operations,
		sourcePatch:  sourcePatch,
		failureCode:  failureCode,
		diagnostics:  diagnostics,
	}, nil
}

// Path returns the user-given path spelling.
func (e *BatchPlanFileEntry) Path() string { return e.path }

// Status returns the per-file plan status.
func (e *BatchPlanFileEntry) Status() BatchPlanFileStatus { return e.status }

// Profile returns the profile of a planned file.
func (e *BatchPlanFileEntry) Profile() *ProfileReference { return e.profile }

// SourceDigest returns the base digest of a planned file; it equals
// source_patch.base_digest.
func (e *BatchPlanFileEntry) SourceDigest() *ContentDigest { return e.sourceDigest }

// Operations returns the ordered operation summaries of a planned file.
func (e *BatchPlanFileEntry) Operations() []*EditOperationSummary {
	return append([]*EditOperationSummary(nil), e.operations...)
}

// SourcePatch returns the verifiable source patch of a planned file.
func (e *BatchPlanFileEntry) SourcePatch() *SourcePatch { return e.sourcePatch }

// FailureCode returns the failure code of a failed file.
func (e *BatchPlanFileEntry) FailureCode() *string { return e.failureCode }

// Diagnostics returns the diagnostics of a failed file.
func (e *BatchPlanFileEntry) Diagnostics() []*Diagnostic {
	return append([]*Diagnostic(nil), e.diagnostics...)
}

// BatchPlanMessage is the full core.batch-plan@1 manifest (RFC 0015 §8;
// cli.rs:543-641).
type BatchPlanMessage struct {
	productVersion string
	files          []*BatchPlanFileEntry
}

// NewBatchPlanMessage validates the manifest fields and every file entry.
func NewBatchPlanMessage(productVersion string, files []*BatchPlanFileEntry) (*BatchPlanMessage, error) {
	return newBatchPlanMessageWithRegistry(productVersion, files, NewErrorCodeRegistry(ErrorRegistryV7))
}

// NewBatchPlanMessageWithRegistry validates the manifest under one explicit
// semantic-model registry.
func NewBatchPlanMessageWithRegistry(productVersion string, files []*BatchPlanFileEntry,
	registry ErrorCodeRegistry) (*BatchPlanMessage, error) {
	return newBatchPlanMessageWithRegistry(productVersion, files, registry)
}

func newBatchPlanMessageWithRegistry(productVersion string, files []*BatchPlanFileEntry,
	registry ErrorCodeRegistry) (*BatchPlanMessage, error) {
	if productVersion == "" {
		return nil, invalid("$.product_version", "product_version cannot be empty")
	}
	for index, entry := range files {
		if _, err := revalidatePlanEntry(entry, index, registry); err != nil {
			return nil, err
		}
	}
	return &BatchPlanMessage{productVersion: productVersion, files: files}, nil
}

// ProductVersion returns the release version string of the producing CLI.
func (m *BatchPlanMessage) ProductVersion() string { return m.productVersion }

// Files returns the file entries in command-line argument order.
func (m *BatchPlanMessage) Files() []*BatchPlanFileEntry {
	return append([]*BatchPlanFileEntry(nil), m.files...)
}

// ToValue encodes the fixed core.batch-plan@1 schema as a PortableValue
// tree. Source-patch replacement bytes travel as Bytes leaves with full
// fidelity, byte-identical to the Rust value-level encoding.
func (m *BatchPlanMessage) ToValue() (core.Value, error) {
	files := make([]core.Value, 0, len(m.files))
	for index, entry := range m.files {
		value, err := planEntryValue(entry, index)
		if err != nil {
			return nil, err
		}
		files = append(files, value)
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.batch-plan@1")},
		core.Entry{Key: "product_version", Value: core.String(m.productVersion)},
		core.Entry{Key: "command", Value: core.String("plan")},
		core.Entry{Key: "files", Value: core.NewArray(files...)},
	)
}

// FromValue strictly decodes core.batch-plan@1 under the semantic-model v7
// error registry (cli.rs:610-640).
func (m *BatchPlanMessage) FromValue(value core.Value) (*BatchPlanMessage, error) {
	return m.fromValueWithRegistry(value, NewErrorCodeRegistry(ErrorRegistryV7), DefaultSourcePatchLimits())
}

// FromValueWithRegistry strictly decodes the manifest and re-verifies every
// cross constraint under one explicit registry.
func (m *BatchPlanMessage) FromValueWithRegistry(value core.Value, registry ErrorCodeRegistry) (*BatchPlanMessage, error) {
	return m.fromValueWithRegistry(value, registry, DefaultSourcePatchLimits())
}

// FromValueWithRegistryAndPatchLimits strictly decodes the manifest under
// one explicit registry and one explicit source-patch replacement budget
// (the cli.rs from_value_with_registry patch_limits parameter).
func (m *BatchPlanMessage) FromValueWithRegistryAndPatchLimits(value core.Value,
	registry ErrorCodeRegistry, patchLimits SourcePatchLimits) (*BatchPlanMessage, error) {
	return m.fromValueWithRegistry(value, registry, patchLimits)
}

func (m *BatchPlanMessage) fromValueWithRegistry(value core.Value, registry ErrorCodeRegistry,
	patchLimits SourcePatchLimits) (*BatchPlanMessage, error) {
	fields, err := schemaFields(value, "core.batch-plan@1",
		[]string{"schema", "product_version", "command", "files"}, "$")
	if err != nil {
		return nil, err
	}
	command, err := stringOf(fields[2], "$.command")
	if err != nil {
		return nil, err
	}
	if command != "plan" {
		return nil, invalid("$.command", "expected command \"plan\"")
	}
	fileValues, err := sequenceOf(fields[3], "$.files")
	if err != nil {
		return nil, err
	}
	files := make([]*BatchPlanFileEntry, 0, len(fileValues))
	for index, item := range fileValues {
		entry, err := parsePlanEntry(item, index, registry, patchLimits)
		if err != nil {
			return nil, err
		}
		files = append(files, entry)
	}
	productVersion, err := stringOf(fields[1], "$.product_version")
	if err != nil {
		return nil, err
	}
	return newBatchPlanMessageWithRegistry(productVersion, files, registry)
}

// ToJSON encodes the manifest as canonical core.portable-value-json@1 bytes
// (the full-fidelity path; replacement bytes travel as hex).
func (m *BatchPlanMessage) ToJSON(limits ProtocolLimits) ([]byte, error) {
	node, err := planMessageNode(m)
	if err != nil {
		return nil, err
	}
	return encodeJSONTree(node, limits)
}

// FromJSON strictly decodes canonical core.portable-value-json@1 bytes and
// re-validates the manifest (the full-fidelity path). Valid but
// non-canonical bytes are rejected with KindNonCanonicalJson before any
// record error is reported, mirroring the Rust transport ordering.
func (m *BatchPlanMessage) FromJSON(bytes []byte, limits ProtocolLimits) (*BatchPlanMessage, error) {
	document, err := parseJSONDocument(bytes, limits)
	if err != nil {
		return nil, err
	}
	if err := ensureCanonical(document, bytes, limits); err != nil {
		return nil, err
	}
	fields, err := jsonObjectExact(document, []string{"schema", "value"}, "$")
	if err != nil {
		return nil, err
	}
	schema, err := jsonStringOf(fields[0], "$.schema")
	if err != nil {
		return nil, err
	}
	if schema != PortableValueJSONSchema {
		return nil, protocolError(KindSchemaMismatch, "$.schema", "unexpected transport schema")
	}
	return parsePlanMessageNode(fields[1], NewErrorCodeRegistry(ErrorRegistryV7))
}

// ToPVCE encodes the manifest as canonical PVCE/1, carrying source-patch
// replacement bytes as Bytes leaves with full fidelity.
func (m *BatchPlanMessage) ToPVCE(limits ProtocolLimits) ([]byte, error) {
	value, err := m.ToValue()
	if err != nil {
		return nil, err
	}
	return EncodePVCE(value, limits)
}

// FromPVCE decodes canonical PVCE/1 and re-validates the manifest.
func (m *BatchPlanMessage) FromPVCE(bytes []byte, limits ProtocolLimits) (*BatchPlanMessage, error) {
	value, err := DecodePVCE(bytes, limits)
	if err != nil {
		return nil, err
	}
	return m.FromValue(value)
}

// BatchResultFileStatus is one file-level status in a core.batch-result@1
// manifest (RFC 0015 §9.2; cli.rs:643-655).
type BatchResultFileStatus string

const (
	// ResultStatusCompleted: the file was rewritten and its target digest
	// was verified.
	ResultStatusCompleted BatchResultFileStatus = "completed"
	// ResultStatusFailed: the file failed; failure_code is present.
	ResultStatusFailed BatchResultFileStatus = "failed"
	// ResultStatusPending: the file was pending when the manifest was
	// written (interruption).
	ResultStatusPending BatchResultFileStatus = "pending"
	// ResultStatusSkippedStale: the current bytes no longer match the
	// planned base digest.
	ResultStatusSkippedStale BatchResultFileStatus = "skipped-stale"
)

// BatchResultFileEntry is one result entry of a core.batch-result@1
// manifest (cli.rs:656-743).
type BatchResultFileEntry struct {
	path         string
	status       BatchResultFileStatus
	failureCode  *string
	targetDigest *ContentDigest
	redacted     bool
}

// NewBatchResultFileEntry validates the per-status presence rules and the
// closed status set (cli.rs:666-712).
func NewBatchResultFileEntry(path string, status BatchResultFileStatus,
	failureCode *string, targetDigest *ContentDigest, redacted bool) (*BatchResultFileEntry, error) {
	if path == "" || len(path) > 1024 {
		return nil, invalid("$.files[].path", "invalid path")
	}
	switch status {
	case ResultStatusCompleted:
		if failureCode != nil || targetDigest == nil {
			return nil, invalid("$.files[]",
				"completed entries require a target_digest and no failure_code")
		}
	case ResultStatusFailed, ResultStatusSkippedStale:
		if failureCode == nil || *failureCode == "" || targetDigest != nil {
			return nil, invalid("$.files[]",
				"failed or skipped-stale entries require a failure_code and no target_digest")
		}
	case ResultStatusPending:
		if failureCode != nil || targetDigest != nil {
			return nil, invalid("$.files[]",
				"pending entries cannot carry failure_code or target_digest")
		}
	default:
		return nil, invalid("$.files[].status", "unknown result file status")
	}
	return &BatchResultFileEntry{
		path:         path,
		status:       status,
		failureCode:  failureCode,
		targetDigest: targetDigest,
		redacted:     redacted,
	}, nil
}

// Path returns the user-given path spelling.
func (e *BatchResultFileEntry) Path() string { return e.path }

// Status returns the per-file result status.
func (e *BatchResultFileEntry) Status() BatchResultFileStatus { return e.status }

// FailureCode returns the failure code of failed or skipped-stale files.
func (e *BatchResultFileEntry) FailureCode() *string { return e.failureCode }

// TargetDigest returns the verified target digest of completed files.
func (e *BatchResultFileEntry) TargetDigest() *ContentDigest { return e.targetDigest }

// Redacted reports whether the file's edit operations matched a redaction
// key pattern.
func (e *BatchResultFileEntry) Redacted() bool { return e.redacted }

// BatchResultMessage is the full core.batch-result@1 manifest (RFC 0015 §9;
// cli.rs:745-822).
type BatchResultMessage struct {
	productVersion string
	files          []*BatchResultFileEntry
}

// NewBatchResultMessage validates the manifest fields and every result
// entry.
func NewBatchResultMessage(productVersion string, files []*BatchResultFileEntry) (*BatchResultMessage, error) {
	if productVersion == "" {
		return nil, invalid("$.product_version", "product_version cannot be empty")
	}
	return &BatchResultMessage{productVersion: productVersion, files: files}, nil
}

// ProductVersion returns the release version string of the producing CLI.
func (m *BatchResultMessage) ProductVersion() string { return m.productVersion }

// Files returns the result entries in input plan order.
func (m *BatchResultMessage) Files() []*BatchResultFileEntry {
	return append([]*BatchResultFileEntry(nil), m.files...)
}

// ToValue encodes the fixed core.batch-result@1 schema.
func (m *BatchResultMessage) ToValue() (core.Value, error) {
	files := make([]core.Value, 0, len(m.files))
	for _, entry := range m.files {
		files = append(files, resultEntryValue(entry))
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.batch-result@1")},
		core.Entry{Key: "product_version", Value: core.String(m.productVersion)},
		core.Entry{Key: "command", Value: core.String("apply")},
		core.Entry{Key: "files", Value: core.NewArray(files...)},
	)
}

// FromValue strictly decodes core.batch-result@1 (cli.rs:801-821).
func (m *BatchResultMessage) FromValue(value core.Value) (*BatchResultMessage, error) {
	fields, err := schemaFields(value, "core.batch-result@1",
		[]string{"schema", "product_version", "command", "files"}, "$")
	if err != nil {
		return nil, err
	}
	command, err := stringOf(fields[2], "$.command")
	if err != nil {
		return nil, err
	}
	if command != "apply" {
		return nil, invalid("$.command", "expected command \"apply\"")
	}
	fileValues, err := sequenceOf(fields[3], "$.files")
	if err != nil {
		return nil, err
	}
	files := make([]*BatchResultFileEntry, 0, len(fileValues))
	for index, item := range fileValues {
		entry, err := parseResultEntry(item, "$.files["+uint32String(uint32(index))+"]")
		if err != nil {
			return nil, err
		}
		files = append(files, entry)
	}
	productVersion, err := stringOf(fields[1], "$.product_version")
	if err != nil {
		return nil, err
	}
	return NewBatchResultMessage(productVersion, files)
}

// ToJSON encodes the manifest as canonical core.portable-value-json@1
// bytes.
func (m *BatchResultMessage) ToJSON(limits ProtocolLimits) ([]byte, error) {
	value, err := m.ToValue()
	if err != nil {
		return nil, err
	}
	return EncodeJSON(value, limits)
}

// FromJSON decodes canonical core.portable-value-json@1 bytes and
// re-validates the manifest.
func (m *BatchResultMessage) FromJSON(bytes []byte, limits ProtocolLimits) (*BatchResultMessage, error) {
	value, err := DecodeJSON(bytes, limits)
	if err != nil {
		return nil, err
	}
	return m.FromValue(value)
}

// ToPVCE encodes the manifest as canonical PVCE/1.
func (m *BatchResultMessage) ToPVCE(limits ProtocolLimits) ([]byte, error) {
	value, err := m.ToValue()
	if err != nil {
		return nil, err
	}
	return EncodePVCE(value, limits)
}

// FromPVCE decodes canonical PVCE/1 and re-validates the manifest.
func (m *BatchResultMessage) FromPVCE(bytes []byte, limits ProtocolLimits) (*BatchResultMessage, error) {
	value, err := DecodePVCE(bytes, limits)
	if err != nil {
		return nil, err
	}
	return m.FromValue(value)
}

// CliOutputMessage is the full core.cli-output@1 machine envelope
// (RFC 0015 §4; cli.rs:149-364).
type CliOutputMessage struct {
	command        CliCommand
	exitClass      ExitClass
	productVersion string
	payload        core.Value
	diagnostics    []*Diagnostic
	redaction      *Redaction
}

// NewCliOutputMessage validates command/exit-class closure, product-version
// shape, payload schema consistency, diagnostic registry binding, and
// redaction facts (cli.rs:160-220).
func NewCliOutputMessage(command CliCommand, exitClass ExitClass,
	productVersion string, payload core.Value, diagnostics []*Diagnostic,
	redaction *Redaction) (*CliOutputMessage, error) {
	return newCliOutputMessageWithRegistry(command, exitClass, productVersion,
		payload, diagnostics, redaction, NewErrorCodeRegistry(ErrorRegistryV7))
}

// NewCliOutputMessageWithRegistry validates the envelope under one explicit
// semantic-model registry.
func NewCliOutputMessageWithRegistry(command CliCommand, exitClass ExitClass,
	productVersion string, payload core.Value, diagnostics []*Diagnostic,
	redaction *Redaction, registry ErrorCodeRegistry) (*CliOutputMessage, error) {
	return newCliOutputMessageWithRegistry(command, exitClass, productVersion,
		payload, diagnostics, redaction, registry)
}

func newCliOutputMessageWithRegistry(command CliCommand, exitClass ExitClass,
	productVersion string, payload core.Value, diagnostics []*Diagnostic,
	redaction *Redaction, registry ErrorCodeRegistry) (*CliOutputMessage, error) {
	if !isSemanticVersion(productVersion) {
		return nil, invalid("$.product_version",
			"expected MAJOR.MINOR.PATCH[-prerelease] without leading zeros or build metadata")
	}
	if err := validatePayloadSchema(payload, command); err != nil {
		return nil, err
	}
	for index, diagnostic := range diagnostics {
		if err := validateDiagnosticCode(diagnostic.Code, diagnostic.Category, registry); err != nil {
			return nil, &ProtocolError{
				Kind:   err.Kind,
				Path:   "$.diagnostics[" + uint32String(uint32(index)) + "]",
				Detail: err.Error(),
			}
		}
	}
	return &CliOutputMessage{
		command:        command,
		exitClass:      exitClass,
		productVersion: productVersion,
		payload:        payload,
		diagnostics:    diagnostics,
		redaction:      redaction,
	}, nil
}

// Command returns the command that produced the envelope.
func (m *CliOutputMessage) Command() CliCommand { return m.command }

// ExitClass returns the frozen exit class of the operation.
func (m *CliOutputMessage) ExitClass() ExitClass { return m.exitClass }

// ProductVersion returns the release version string of the producing CLI.
func (m *CliOutputMessage) ProductVersion() string { return m.productVersion }

// Payload returns the validated command payload.
func (m *CliOutputMessage) Payload() core.Value { return m.payload }

// Diagnostics returns the ordered operation diagnostics.
func (m *CliOutputMessage) Diagnostics() []*Diagnostic {
	return append([]*Diagnostic(nil), m.diagnostics...)
}

// Redaction returns the redaction facts of this output.
func (m *CliOutputMessage) Redaction() *Redaction { return m.redaction }

// ToValue encodes the fixed core.cli-output@1 envelope.
func (m *CliOutputMessage) ToValue() (core.Value, error) {
	diagnostics := make([]core.Value, 0, len(m.diagnostics))
	for _, diagnostic := range m.diagnostics {
		value, err := diagnostic.ToValue()
		if err != nil {
			return nil, err
		}
		diagnostics = append(diagnostics, value)
	}
	redactionObject, err := core.NewObject(
		core.Entry{Key: "redacted", Value: core.Boolean(m.redaction.redacted)},
		core.Entry{Key: "count", Value: integerValue(m.redaction.count)},
	)
	if err != nil {
		return nil, err
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.cli-output@1")},
		core.Entry{Key: "command", Value: core.String(m.command.Name())},
		core.Entry{Key: "exit_class", Value: core.String(m.exitClass.Name())},
		core.Entry{Key: "product_version", Value: core.String(m.productVersion)},
		core.Entry{Key: "payload", Value: m.payload},
		core.Entry{Key: "diagnostics", Value: core.NewArray(diagnostics...)},
		core.Entry{Key: "redaction", Value: redactionObject},
	)
}

// FromValue strictly decodes core.cli-output@1 under the semantic-model v7
// error registry (cli.rs:288-343).
func (m *CliOutputMessage) FromValue(value core.Value) (*CliOutputMessage, error) {
	return m.fromValueWithRegistry(value, NewErrorCodeRegistry(ErrorRegistryV7))
}

// FromValueWithRegistry strictly decodes the envelope under one explicit
// registry.
func (m *CliOutputMessage) FromValueWithRegistry(value core.Value, registry ErrorCodeRegistry) (*CliOutputMessage, error) {
	return m.fromValueWithRegistry(value, registry)
}

func (m *CliOutputMessage) fromValueWithRegistry(value core.Value, registry ErrorCodeRegistry) (*CliOutputMessage, error) {
	fields, err := schemaFields(value, "core.cli-output@1",
		[]string{"schema", "command", "exit_class", "product_version", "payload",
			"diagnostics", "redaction"}, "$")
	if err != nil {
		return nil, err
	}
	commandName, err := stringOf(fields[1], "$.command")
	if err != nil {
		return nil, err
	}
	command, ok := ParseCliCommand(commandName)
	if !ok {
		return nil, invalid("$.command", "unknown command")
	}
	exitClassName, err := stringOf(fields[2], "$.exit_class")
	if err != nil {
		return nil, err
	}
	exitClass, ok := ParseExitClass(exitClassName)
	if !ok {
		return nil, invalid("$.exit_class", "unknown exit class")
	}
	productVersion, err := stringOf(fields[3], "$.product_version")
	if err != nil {
		return nil, err
	}
	if !isSemanticVersion(productVersion) {
		return nil, invalid("$.product_version",
			"expected MAJOR.MINOR.PATCH[-prerelease] without leading zeros or build metadata")
	}
	if err := validatePayloadSchema(fields[4], command); err != nil {
		return nil, err
	}
	diagnosticValues, err := sequenceOf(fields[5], "$.diagnostics")
	if err != nil {
		return nil, err
	}
	diagnostics := make([]*Diagnostic, 0, len(diagnosticValues))
	for _, item := range diagnosticValues {
		diagnostic := &Diagnostic{}
		decoded, err := diagnostic.FromValue(item, registry)
		if err != nil {
			return nil, err
		}
		diagnostics = append(diagnostics, decoded)
	}
	redactionFields, err := exactFields(fields[6], []string{"redacted", "count"}, "$.redaction")
	if err != nil {
		return nil, err
	}
	redacted, err := booleanOf(redactionFields[0], "$.redaction.redacted")
	if err != nil {
		return nil, err
	}
	count, err := unsigned64(redactionFields[1], "$.redaction.count")
	if err != nil {
		return nil, err
	}
	redaction, err := NewRedaction(redacted, count)
	if err != nil {
		return nil, err
	}
	return newCliOutputMessageWithRegistry(command, exitClass, productVersion,
		fields[4], diagnostics, redaction, registry)
}

// ToJSON encodes the envelope through canonical tagged JSON.
func (m *CliOutputMessage) ToJSON(limits ProtocolLimits) ([]byte, error) {
	value, err := m.ToValue()
	if err != nil {
		return nil, err
	}
	return EncodeJSON(value, limits)
}

// FromJSON decodes canonical tagged JSON and re-validates the envelope.
func (m *CliOutputMessage) FromJSON(bytes []byte, limits ProtocolLimits) (*CliOutputMessage, error) {
	value, err := DecodeJSON(bytes, limits)
	if err != nil {
		return nil, err
	}
	return m.FromValue(value)
}

// ToPVCE encodes the envelope through canonical PVCE/1.
func (m *CliOutputMessage) ToPVCE(limits ProtocolLimits) ([]byte, error) {
	value, err := m.ToValue()
	if err != nil {
		return nil, err
	}
	return EncodePVCE(value, limits)
}

// FromPVCE decodes canonical PVCE/1 and re-validates the envelope.
func (m *CliOutputMessage) FromPVCE(bytes []byte, limits ProtocolLimits) (*CliOutputMessage, error) {
	value, err := DecodePVCE(bytes, limits)
	if err != nil {
		return nil, err
	}
	return m.FromValue(value)
}

// validatePayloadSchema requires the payload to be an Object whose first
// field is "schema" carrying one of the command's published schemas
// (cli.rs:824-868).
func validatePayloadSchema(payload core.Value, command CliCommand) error {
	object, ok := payload.(*core.Object)
	if !ok {
		return protocolError(KindWrongType, "$.payload", "payload must be an Object")
	}
	entries := object.Entries()
	if len(entries) == 0 {
		return protocolError(KindMissingField, "$.payload.schema", "payload schema is absent")
	}
	if entries[0].Key != "schema" {
		return protocolError(KindSchemaMismatch, "$.payload", "schema must be the first field")
	}
	schema, err := stringOf(entries[0].Value, "$.payload.schema")
	if err != nil {
		return err
	}
	if !containsString(command.PayloadSchemas(), schema) {
		return protocolError(KindSchemaMismatch, "$.payload.schema",
			"payload schema "+schema+" is not published by "+command.Name())
	}
	return nil
}

// isSemanticVersion validates the SemVer 2.0 core shape of a product version
// (RFC 0015 §3.3, 2026-08-10 revision; cli.rs:870-929): MAJOR.MINOR.PATCH
// with an optional dot-separated -prerelease suffix; numeric segments and
// numeric prerelease identifiers carry no leading zeros; build metadata ('+'
// suffix) is rejected (product_version never carries build metadata or git
// hashes).
func isSemanticVersion(version string) bool {
	if strings.Contains(version, "+") {
		return false
	}
	core := version
	hasPrerelease := false
	prerelease := ""
	if dash := strings.IndexByte(version, '-'); dash >= 0 {
		core, prerelease = version[:dash], version[dash+1:]
		hasPrerelease = true
	}
	if !numericCore(core) {
		return false
	}
	if !hasPrerelease {
		return true
	}
	if prerelease == "" {
		return false
	}
	for _, identifier := range strings.Split(prerelease, ".") {
		if !prereleaseIdentifier(identifier) {
			return false
		}
	}
	return true
}

// numericCore reports whether text is exactly three dot-separated numeric
// segments without leading zeros (the MAJOR.MINOR.PATCH core).
func numericCore(text string) bool {
	start, count := 0, 0
	for index := 0; index <= len(text); index++ {
		if index == len(text) || text[index] == '.' {
			if !numericSegment(text[start:index]) {
				return false
			}
			count++
			if count > 3 {
				return false
			}
			start = index + 1
		}
	}
	return count == 3
}

// numericSegment reports whether one segment is a non-empty digit run without
// a leading zero (single "0" is allowed).
func numericSegment(segment string) bool {
	if segment == "" {
		return false
	}
	for index := 0; index < len(segment); index++ {
		if segment[index] < '0' || segment[index] > '9' {
			return false
		}
	}
	return len(segment) == 1 || segment[0] != '0'
}

// prereleaseIdentifier reports whether one SemVer prerelease identifier is
// well-formed: non-empty and ASCII alphanumeric or hyphen only; numeric
// identifiers must not carry leading zeros.
func prereleaseIdentifier(identifier string) bool {
	if identifier == "" {
		return false
	}
	numeric := true
	for index := 0; index < len(identifier); index++ {
		ch := identifier[index]
		if ch < '0' || ch > '9' {
			numeric = false
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '-') {
				return false
			}
		}
	}
	if numeric && len(identifier) > 1 && identifier[0] == '0' {
		return false
	}
	return true
}

// revalidatePlanEntry re-verifies the entry-level cross constraints of a
// manifest (cli.rs:887-938).
func revalidatePlanEntry(entry *BatchPlanFileEntry, index int, registry ErrorCodeRegistry) (*BatchPlanFileEntry, error) {
	path := "$.files[" + uint32String(uint32(index)) + "]"
	switch entry.status {
	case PlanStatusPlanned:
		if entry.profile == nil || entry.sourceDigest == nil ||
			entry.operations == nil || entry.sourcePatch == nil {
			return nil, invalid(path, "planned entries require all planning facts")
		}
		if !entry.sourceDigest.Equal(entry.sourcePatch.BaseDigest) {
			return nil, invalid(path+".source_digest",
				"source_digest must equal source_patch.base_digest")
		}
	case PlanStatusFailed:
		if entry.failureCode == nil || *entry.failureCode == "" || entry.diagnostics == nil {
			return nil, invalid(path, "failed entries require failure_code and diagnostics")
		}
	}
	for diagnosticIndex, diagnostic := range entry.diagnostics {
		if err := validateDiagnosticCode(diagnostic.Code, diagnostic.Category, registry); err != nil {
			return nil, &ProtocolError{
				Kind:   err.Kind,
				Path:   path + ".diagnostics[" + uint32String(uint32(diagnosticIndex)) + "]",
				Detail: err.Error(),
			}
		}
	}
	return entry, nil
}

// planEntryValue encodes one plan entry as a PortableValue tree
// (cli.rs:940-1020). Source-patch replacement records travel as Bytes
// leaves with full fidelity.
func planEntryValue(entry *BatchPlanFileEntry, index int) (core.Value, error) {
	var profile core.Value = core.NullValue()
	if entry.profile != nil {
		profile = referenceValue(entry.profile.id, entry.profile.version)
	}
	var sourceDigest core.Value = core.NullValue()
	if entry.sourceDigest != nil {
		sourceDigest = digestValue(*entry.sourceDigest)
	}
	var operations core.Value = core.NullValue()
	if entry.operations != nil {
		items := make([]core.Value, 0, len(entry.operations))
		for _, operation := range entry.operations {
			summary := make(map[string]string, len(operation.Summary))
			for name, value := range operation.Summary {
				summary[name] = value
			}
			summaryObject, err := stringMapObject(summary)
			if err != nil {
				return nil, err
			}
			record, err := core.NewObject(
				core.Entry{Key: "operation", Value: referenceValue(operation.Operation.id, operation.Operation.version)},
				core.Entry{Key: "summary", Value: summaryObject},
			)
			if err != nil {
				return nil, err
			}
			items = append(items, record)
		}
		operations = core.NewArray(items...)
	}
	var sourcePatch core.Value = core.NullValue()
	if entry.sourcePatch != nil {
		encoded, err := sourcePatchValue(entry.sourcePatch)
		if err != nil {
			protocolError := asProtocolError(err)
			return nil, &ProtocolError{
				Kind: protocolError.Kind, Path: "$.files[" + uint32String(uint32(index)) + "].source_patch",
				Detail: protocolError.Detail,
			}
		}
		sourcePatch = encoded
	}
	var failureCode core.Value = core.NullValue()
	if entry.failureCode != nil {
		failureCode = core.String(*entry.failureCode)
	}
	var diagnostics core.Value = core.NullValue()
	if entry.diagnostics != nil {
		items := make([]core.Value, 0, len(entry.diagnostics))
		for _, diagnostic := range entry.diagnostics {
			value, err := diagnostic.ToValue()
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
		diagnostics = core.NewArray(items...)
	}
	return core.NewObject(
		core.Entry{Key: "path", Value: core.String(entry.path)},
		core.Entry{Key: "status", Value: core.String(string(entry.status))},
		core.Entry{Key: "profile", Value: profile},
		core.Entry{Key: "source_digest", Value: sourceDigest},
		core.Entry{Key: "operations", Value: operations},
		core.Entry{Key: "source_patch", Value: sourcePatch},
		core.Entry{Key: "failure_code", Value: failureCode},
		core.Entry{Key: "diagnostics", Value: diagnostics},
	)
}

// parsePlanEntry strictly decodes one plan entry at the value level
// (cli.rs:1022-1135).
func parsePlanEntry(value core.Value, index int, registry ErrorCodeRegistry,
	patchLimits SourcePatchLimits) (*BatchPlanFileEntry, error) {
	path := "$.files[" + uint32String(uint32(index)) + "]"
	fields, err := exactFields(value,
		[]string{"path", "status", "profile", "source_digest", "operations",
			"source_patch", "failure_code", "diagnostics"}, path)
	if err != nil {
		return nil, err
	}
	statusName, err := stringOf(fields[1], path+".status")
	if err != nil {
		return nil, err
	}
	var status BatchPlanFileStatus
	switch statusName {
	case "planned":
		status = PlanStatusPlanned
	case "failed":
		status = PlanStatusFailed
	default:
		return nil, invalid(path+".status", "unknown plan file status")
	}
	var profile *ProfileReference
	var sourceDigest *ContentDigest
	var operations []*EditOperationSummary
	var sourcePatch *SourcePatch
	var failureCode *string
	var diagnostics []*Diagnostic
	switch status {
	case PlanStatusPlanned:
		profile, err = parseProfile(fields[2], path+".profile")
		if err != nil {
			return nil, err
		}
		digest, err := parseDigest(fields[3], path+".source_digest")
		if err != nil {
			return nil, err
		}
		sourceDigest = &digest
		operationValues, err := sequenceOf(fields[4], path+".operations")
		if err != nil {
			return nil, err
		}
		operations = make([]*EditOperationSummary, 0, len(operationValues))
		for operationIndex, item := range operationValues {
			operation, err := parseOperationSummary(item,
				path+".operations["+uint32String(uint32(operationIndex))+"]")
			if err != nil {
				return nil, err
			}
			operations = append(operations, operation)
		}
		patch, err := parseSourcePatchValue(fields[5], path+".source_patch")
		if err != nil {
			return nil, err
		}
		if len(patch.Replacements) > patchLimits.MaxReplacements {
			return nil, resource(path+".source_patch.replacements",
				"replacement count exceeds configured limit")
		}
		sourcePatch = patch
		if _, isNull := fields[6].(core.Null); !isNull {
			return nil, invalid(path, "planned entries cannot carry failure_code or diagnostics")
		}
		if _, isNull := fields[7].(core.Null); !isNull {
			return nil, invalid(path, "planned entries cannot carry failure_code or diagnostics")
		}
	case PlanStatusFailed:
		if _, isNull := fields[2].(core.Null); !isNull {
			return nil, invalid(path, "failed entries cannot carry planning facts")
		}
		if _, isNull := fields[3].(core.Null); !isNull {
			return nil, invalid(path, "failed entries cannot carry planning facts")
		}
		if _, isNull := fields[4].(core.Null); !isNull {
			return nil, invalid(path, "failed entries cannot carry planning facts")
		}
		if _, isNull := fields[5].(core.Null); !isNull {
			return nil, invalid(path, "failed entries cannot carry planning facts")
		}
		code, err := stringOf(fields[6], path+".failure_code")
		if err != nil {
			return nil, err
		}
		if code == "" {
			return nil, invalid(path+".failure_code", "failure_code cannot be empty")
		}
		failureCode = &code
		diagnosticValues, err := sequenceOf(fields[7], path+".diagnostics")
		if err != nil {
			return nil, err
		}
		diagnostics = make([]*Diagnostic, 0, len(diagnosticValues))
		for _, item := range diagnosticValues {
			diagnostic := &Diagnostic{}
			decoded, err := diagnostic.FromValue(item, registry)
			if err != nil {
				return nil, err
			}
			diagnostics = append(diagnostics, decoded)
		}
	}
	pathText, err := stringOf(fields[0], path+".path")
	if err != nil {
		return nil, err
	}
	return NewBatchPlanFileEntry(pathText, status, profile, sourceDigest, operations,
		sourcePatch, failureCode, diagnostics, registry)
}

// resultEntryValue encodes one result entry (cli.rs:1137-1164).
func resultEntryValue(entry *BatchResultFileEntry) core.Value {
	var failureCode core.Value = core.NullValue()
	if entry.failureCode != nil {
		failureCode = core.String(*entry.failureCode)
	}
	var targetDigest core.Value = core.NullValue()
	if entry.targetDigest != nil {
		targetDigest = digestValue(*entry.targetDigest)
	}
	return valueObject(
		core.Entry{Key: "path", Value: core.String(entry.path)},
		core.Entry{Key: "status", Value: core.String(string(entry.status))},
		core.Entry{Key: "failure_code", Value: failureCode},
		core.Entry{Key: "target_digest", Value: targetDigest},
		core.Entry{Key: "redacted", Value: core.Boolean(entry.redacted)},
	)
}

// parseResultEntry strictly decodes one result entry (cli.rs:1166-1210).
func parseResultEntry(value core.Value, path string) (*BatchResultFileEntry, error) {
	fields, err := exactFields(value,
		[]string{"path", "status", "failure_code", "target_digest", "redacted"}, path)
	if err != nil {
		return nil, err
	}
	statusName, err := stringOf(fields[1], path+".status")
	if err != nil {
		return nil, err
	}
	var status BatchResultFileStatus
	switch statusName {
	case "completed":
		status = ResultStatusCompleted
	case "failed":
		status = ResultStatusFailed
	case "pending":
		status = ResultStatusPending
	case "skipped-stale":
		status = ResultStatusSkippedStale
	default:
		return nil, invalid(path+".status", "unknown result file status")
	}
	var failureCode *string
	if _, isNull := fields[2].(core.Null); !isNull {
		code, err := stringOf(fields[2], path+".failure_code")
		if err != nil {
			return nil, err
		}
		failureCode = &code
	}
	var targetDigest *ContentDigest
	if _, isNull := fields[3].(core.Null); !isNull {
		digest, err := parseDigest(fields[3], path+".target_digest")
		if err != nil {
			return nil, err
		}
		targetDigest = &digest
	}
	redacted, err := booleanOf(fields[4], path+".redacted")
	if err != nil {
		return nil, err
	}
	pathText, err := stringOf(fields[0], path+".path")
	if err != nil {
		return nil, err
	}
	return NewBatchResultFileEntry(pathText, status, failureCode, targetDigest, redacted)
}

// digestValue encodes one digest record (cli.rs:1222-1227).
func digestValue(digest ContentDigest) core.Value {
	return valueObject(
		core.Entry{Key: "algorithm", Value: core.String(digest.Algorithm())},
		core.Entry{Key: "hex", Value: core.String(digest.Hex())},
	)
}

// parseDigest strictly decodes one sha256 digest record
// (cli.rs:1229-1248).
func parseDigest(value core.Value, path string) (ContentDigest, error) {
	fields, err := exactFields(value, []string{"algorithm", "hex"}, path)
	if err != nil {
		return ContentDigest{}, err
	}
	algorithm, err := stringOf(fields[0], path+".algorithm")
	if err != nil {
		return ContentDigest{}, err
	}
	if algorithm != "sha256" {
		return ContentDigest{}, invalid(path, "expected sha256")
	}
	hex, err := stringOf(fields[1], path+".hex")
	if err != nil {
		return ContentDigest{}, err
	}
	if len(hex) != 64 || !isLowercaseHex(hex) {
		return ContentDigest{}, invalid(path, "invalid lowercase sha256")
	}
	var bytes [32]byte
	for index := 0; index < 32; index++ {
		high := hexDigitValue(hex[index*2])
		low := hexDigitValue(hex[index*2+1])
		bytes[index] = byte(high<<4 | low)
	}
	return ContentDigestFromBytes(bytes), nil
}

// isLowercaseHex reports whether every byte is a lowercase hexadecimal
// digit (the canonical digest spelling, cli.rs:1234-1240).
func isLowercaseHex(text string) bool {
	for index := 0; index < len(text); index++ {
		byte := text[index]
		if !(byte >= '0' && byte <= '9') && !(byte >= 'a' && byte <= 'f') {
			return false
		}
	}
	return true
}

// parseProfile strictly decodes one profile reference record
// (cli.rs:1250-1257).
func parseProfile(value core.Value, path string) (*ProfileReference, error) {
	reference, err := parseProfileReference(value, path)
	if err != nil {
		return nil, err
	}
	return reference, nil
}

// parseOperationSummary strictly decodes one operation summary record
// (cli.rs:1259-1286).
func parseOperationSummary(value core.Value, path string) (*EditOperationSummary, error) {
	fields, err := exactFields(value, []string{"operation", "summary"}, path)
	if err != nil {
		return nil, err
	}
	reference, err := exactFields(fields[0], []string{"id", "version"}, path+".operation")
	if err != nil {
		return nil, err
	}
	id, err := stringOf(reference[0], path+".operation.id")
	if err != nil {
		return nil, err
	}
	version, err := unsigned32(reference[1], path+".operation.version")
	if err != nil {
		return nil, err
	}
	summary, err := stringMapFromObject(fields[1], path+".summary")
	if err != nil {
		return nil, err
	}
	return NewEditOperationSummary(NewFormatOperationId(id, version), summary)
}

// sourcePatchValue encodes the core.source-patch@2 record at the value
// level with full replacement fidelity (protocol/src/source.rs:323-371;
// the 15-kind value model carries Bytes leaves).
func sourcePatchValue(patch *SourcePatch) (core.Value, error) {
	encoding, err := encodingFactsValue(&patch.Encoding)
	if err != nil {
		return nil, err
	}
	metadata := make(map[string]string, len(patch.Metadata))
	for name, value := range patch.Metadata {
		metadata[name] = value
	}
	metadataObject, err := stringMapObject(metadata)
	if err != nil {
		return nil, err
	}
	replacements := make([]core.Value, 0, len(patch.Replacements))
	for _, replacement := range patch.Replacements {
		record, err := core.NewObject(
			core.Entry{Key: "old_start", Value: integerValue(replacement.OldStart)},
			core.Entry{Key: "old_end", Value: integerValue(replacement.OldEnd)},
			core.Entry{Key: "original", Value: core.NewBytes(replacement.Original)},
			core.Entry{Key: "replacement", Value: core.NewBytes(replacement.Replacement)},
			core.Entry{Key: "redact_original", Value: core.Boolean(replacement.RedactOriginal)},
			core.Entry{Key: "redact_replacement", Value: core.Boolean(replacement.RedactReplacement)},
		)
		if err != nil {
			return nil, err
		}
		replacements = append(replacements, record)
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.source-patch@2")},
		core.Entry{Key: "base_digest", Value: digestValue(patch.BaseDigest)},
		core.Entry{Key: "target_digest", Value: digestValue(patch.TargetDigest)},
		core.Entry{Key: "encoding", Value: encoding},
		core.Entry{Key: "replacements", Value: core.NewArray(replacements...)},
		core.Entry{Key: "metadata", Value: metadataObject},
	)
}

// parseSourcePatchValue strictly decodes the core.source-patch@2 record at
// the value level with full replacement fidelity
// (protocol/src/source.rs:373-...).
func parseSourcePatchValue(value core.Value, path string) (*SourcePatch, error) {
	fields, err := schemaFields(value, "core.source-patch@2",
		[]string{"schema", "base_digest", "target_digest", "encoding",
			"replacements", "metadata"}, path)
	if err != nil {
		return nil, err
	}
	baseDigest, err := parseDigest(fields[1], path+".base_digest")
	if err != nil {
		return nil, err
	}
	targetDigest, err := parseDigest(fields[2], path+".target_digest")
	if err != nil {
		return nil, err
	}
	encoding, err := parseEncodingFactsValue(fields[3], path+".encoding")
	if err != nil {
		return nil, err
	}
	replacementValues, err := sequenceOf(fields[4], path+".replacements")
	if err != nil {
		return nil, err
	}
	replacements := make([]SourceReplacement, 0, len(replacementValues))
	for index, replacementValue := range replacementValues {
		replacementPath := path + ".replacements[" + uint32String(uint32(index)) + "]"
		replacementFields, err := exactFields(replacementValue,
			[]string{"old_start", "old_end", "original", "replacement",
				"redact_original", "redact_replacement"}, replacementPath)
		if err != nil {
			return nil, err
		}
		oldStart, err := unsigned64(replacementFields[0], replacementPath+".old_start")
		if err != nil {
			return nil, err
		}
		oldEnd, err := unsigned64(replacementFields[1], replacementPath+".old_end")
		if err != nil {
			return nil, err
		}
		original, ok := replacementFields[2].(core.Bytes)
		if !ok {
			return nil, protocolError(KindWrongType, replacementPath+".original", "expected Bytes")
		}
		replacement, ok := replacementFields[3].(core.Bytes)
		if !ok {
			return nil, protocolError(KindWrongType, replacementPath+".replacement", "expected Bytes")
		}
		redactOriginal, err := booleanOf(replacementFields[4], replacementPath+".redact_original")
		if err != nil {
			return nil, err
		}
		redactReplacement, err := booleanOf(replacementFields[5], replacementPath+".redact_replacement")
		if err != nil {
			return nil, err
		}
		if oldStart > oldEnd || len(original) != int(oldEnd-oldStart) {
			return nil, invalid(replacementPath, "invalid replacement range or original length")
		}
		replacements = append(replacements, SourceReplacement{
			OldStart:          oldStart,
			OldEnd:            oldEnd,
			Original:          original,
			Replacement:       replacement,
			RedactOriginal:    redactOriginal,
			RedactReplacement: redactReplacement,
		})
	}
	metadata, err := stringMapFromObject(fields[5], path+".metadata")
	if err != nil {
		return nil, err
	}
	return &SourcePatch{
		BaseDigest:   baseDigest,
		TargetDigest: targetDigest,
		Encoding:     *encoding,
		Replacements: replacements,
		Metadata:     metadata,
	}, nil
}

// encodingFactsValue encodes the source-patch@2 encoding facts record
// (protocol/src/source.rs:598-631).
func encodingFactsValue(facts *EncodingFacts) (core.Value, error) {
	profileDefault := core.NullValue()
	if facts.ProfileDefault != nil {
		profileDefault = sourceEncodingValue(facts.ProfileDefault)
	}
	bom := core.NullValue()
	if facts.Bom != nil {
		bom = core.String(*facts.Bom)
	}
	declaration := core.NullValue()
	if facts.Declaration != nil {
		declaration = sourceEncodingValue(facts.Declaration)
	}
	callerOverride := core.NullValue()
	if facts.CallerOverride != nil {
		callerOverride = sourceEncodingValue(facts.CallerOverride)
	}
	selected := core.NullValue()
	if facts.Selected != nil {
		selected = sourceEncodingValue(facts.Selected)
	}
	return core.NewObject(
		core.Entry{Key: "profile_default", Value: profileDefault},
		core.Entry{Key: "bom_policy", Value: core.String(facts.BomPolicy)},
		core.Entry{Key: "bom", Value: bom},
		core.Entry{Key: "declaration", Value: declaration},
		core.Entry{Key: "caller_override", Value: callerOverride},
		core.Entry{Key: "selected", Value: selected},
	)
}

// sourceEncodingValue encodes one core.source-encoding@1 record
// (protocol/src/source.rs:497-514).
func sourceEncodingValue(encoding *SourceEncoding) core.Value {
	var codePage core.Value = core.NullValue()
	if encoding.WindowsCodePage != nil {
		codePage = integerValue(uint64(*encoding.WindowsCodePage))
	}
	return valueObject(
		core.Entry{Key: "schema", Value: core.String("core.source-encoding@1")},
		core.Entry{Key: "kind", Value: core.String(encoding.Kind)},
		core.Entry{Key: "windows_code_page", Value: codePage},
	)
}

// parseEncodingFactsValue strictly decodes the source-patch@2 encoding
// facts record (protocol/src/source.rs:598-631). The structural facts are
// validated; the semantic reconciliation belongs to the document milestone.
func parseEncodingFactsValue(value core.Value, path string) (*EncodingFacts, error) {
	fields, err := exactFields(value, []string{"profile_default", "bom_policy", "bom",
		"declaration", "caller_override", "selected"}, path)
	if err != nil {
		return nil, err
	}
	profileDefault, err := parseSourceEncodingValue(fields[0], path+".profile_default")
	if err != nil {
		return nil, err
	}
	bomPolicy, err := stringOf(fields[1], path+".bom_policy")
	if err != nil {
		return nil, err
	}
	var bom *string
	if _, isNull := fields[2].(core.Null); !isNull {
		text, err := stringOf(fields[2], path+".bom")
		if err != nil {
			return nil, err
		}
		bom = &text
	}
	var declaration *SourceEncoding
	if _, isNull := fields[3].(core.Null); !isNull {
		declaration, err = parseSourceEncodingValue(fields[3], path+".declaration")
		if err != nil {
			return nil, err
		}
	}
	var callerOverride *SourceEncoding
	if _, isNull := fields[4].(core.Null); !isNull {
		callerOverride, err = parseSourceEncodingValue(fields[4], path+".caller_override")
		if err != nil {
			return nil, err
		}
	}
	selected, err := parseSourceEncodingValue(fields[5], path+".selected")
	if err != nil {
		return nil, err
	}
	return &EncodingFacts{
		ProfileDefault: profileDefault,
		BomPolicy:      bomPolicy,
		Bom:            bom,
		Declaration:    declaration,
		CallerOverride: callerOverride,
		Selected:       selected,
	}, nil
}

// parseSourceEncodingValue strictly decodes one core.source-encoding@1
// record (protocol/src/source.rs:497-514).
func parseSourceEncodingValue(value core.Value, path string) (*SourceEncoding, error) {
	fields, err := schemaFields(value, "core.source-encoding@1",
		[]string{"schema", "kind", "windows_code_page"}, path)
	if err != nil {
		return nil, err
	}
	kind, err := stringOf(fields[1], path+".kind")
	if err != nil {
		return nil, err
	}
	var codePage *uint32
	if _, isNull := fields[2].(core.Null); !isNull {
		number, err := unsigned32(fields[2], path+".windows_code_page")
		if err != nil {
			return nil, err
		}
		codePage = &number
	}
	switch kind {
	case "Binary", "Utf8", "Utf16Le", "Utf16Be", "Latin1":
		if codePage != nil {
			return nil, invalid(path+".windows_code_page", "non-Windows encoding requires null")
		}
	case "WindowsCodePage":
		if codePage == nil {
			return nil, invalid(path+".windows_code_page", "Windows code page requires a number")
		}
		if _, ok := WindowsCodePageFromNumber(*codePage); !ok {
			return nil, invalid(path+".windows_code_page", "unsupported Windows code page")
		}
	default:
		return nil, invalid(path+".kind", "unknown source encoding kind")
	}
	return &SourceEncoding{Kind: kind, WindowsCodePage: codePage}, nil
}
