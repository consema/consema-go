package consema

// This file implements the batch-plan and batch-result composition
// (RFC 0015 §8-§9; RFC 0004 §14, §16; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §2.5
// G4.3): the root package closes the `core.batch-plan@1` /
// `core.batch-result@1` records over the shared edit artifacts.
//
// One dry-run EditPlan becomes a planned batch-plan file entry (profile,
// source digest == patch base digest, safe operation summaries, exact
// SourcePatch — RFC 0015 §8.2), and apply revalidates the base digest and
// every original-byte precondition before producing the per-file
// batch-result outcome (RFC 0015 §9.3 steps 1-2; the failure codes
// `core.source.patch-base-mismatch@1` / `core.source.patch-original-mismatch@1`
// / `core.source.patch-target-mismatch@1`). The atomic file write and
// read-back verification stay with the 0.19.0 Go CLI fsio layer; this
// package is pure library composition over caller-supplied bytes.

import (
	"strings"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// BatchPlanner accumulates per-file planned and failed entries of one
// `core.batch-plan@1` manifest (RFC 0015 §8.2: `plan` is read-only; a
// failing file does not fail the batch). Entries keep the command-line
// argument order of the caller.
type BatchPlanner struct {
	productVersion string
	entries        []*protocol.BatchPlanFileEntry
}

// NewBatchPlanner starts a batch plan with the producing product version.
func NewBatchPlanner(productVersion string) *BatchPlanner {
	return &BatchPlanner{productVersion: productVersion}
}

// AddPlanned appends one planned file entry derived from a completed
// dry-run plan (RFC 0015 §8.2: profile, source_digest == base_digest,
// ordered safe operation summaries, and the exact source patch).
func (p *BatchPlanner) AddPlanned(path string, plan *document.EditPlan) error {
	entry, err := PlanFileEntryFromEditPlan(path, plan)
	if err != nil {
		return err
	}
	p.entries = append(p.entries, entry)
	return nil
}

// AddFailed appends one failed file entry (RFC 0015 §8.2: failure_code and
// diagnostics present, no planning facts).
func (p *BatchPlanner) AddFailed(path, failureCode string,
	diagnostics []*protocol.Diagnostic) error {
	entry, err := protocol.NewBatchPlanFileEntry(path, protocol.PlanStatusFailed,
		nil, nil, nil, nil, &failureCode, diagnostics,
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		return err
	}
	p.entries = append(p.entries, entry)
	return nil
}

// Build closes the manifest; the plan is an artifact and never authorizes
// a write (RFC 0015 §8.1).
func (p *BatchPlanner) Build() (*protocol.BatchPlanMessage, error) {
	return protocol.NewBatchPlanMessage(p.productVersion, p.entries)
}

// PlanFileEntryFromEditPlan derives the planned batch-plan file entry from
// one transferable dry-run plan (RFC 0015 §8.2 presence rules: when the
// status is planned, profile, source_digest, operations, and source_patch
// are all present; source_digest equals source_patch.base_digest).
func PlanFileEntryFromEditPlan(path string,
	plan *document.EditPlan) (*protocol.BatchPlanFileEntry, error) {
	profile := plan.TargetProfile()
	profileReference, err := protocol.NewProfileReference(profile.ID(), profile.Version())
	if err != nil {
		return nil, err
	}
	patch := documentPatchToProtocol(plan.SourcePatch())
	sourceDigest := patch.BaseDigest
	entry, err := protocol.NewBatchPlanFileEntry(path, protocol.PlanStatusPlanned,
		profileReference, &sourceDigest, plan.Operations(), patch, nil, nil,
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// ApplyPlanFile revalidates and applies one planned plan file entry against
// the current bytes (RFC 0015 §9.3 pre-write steps 1-2 plus the target
// digest verification of step 5; the caller supplies the file bytes — the
// CLI fsio layer owns file I/O):
//
//  1. the current bytes' digest must equal the plan's source_digest;
//     otherwise the outcome is skipped-stale with
//     `core.source.patch-base-mismatch@1` and nothing is applied;
//  2. every replacement's original-bytes precondition and the patch's
//     encoding facts must hold; a mismatch fails the file with the
//     registered patch failure code (RFC 0015 §5.2: precondition class);
//  3. on success the outcome carries the verified target digest.
//
// The result entry is assembled with the frozen v1 redaction pattern
// (RFC 0015 §11.2): `redacted` reports whether the file's edit operations
// contain at least one key name matching a redaction pattern; the true
// precondition bytes are unchanged by any redaction setting (hard gate 3).
func ApplyPlanFile(entry *protocol.BatchPlanFileEntry, currentBytes []byte,
	limits protocol.SourcePatchLimits) (*protocol.BatchResultFileEntry, error) {
	if entry.Status() != protocol.PlanStatusPlanned || entry.SourcePatch() == nil {
		return nil, &BatchApplyError{Path: entry.Path(),
			Reason: "cannot apply a non-planned plan entry"}
	}
	path := entry.Path()
	currentDigest := protocol.DigestOf(currentBytes)
	if !currentDigest.Equal(*entry.SourceDigest()) {
		code := "core.source.patch-base-mismatch@1"
		result, err := protocol.NewBatchResultFileEntry(path, protocol.ResultStatusSkippedStale,
			&code, nil, false)
		if err != nil {
			return nil, err
		}
		return result, nil
	}
	patch := entry.SourcePatch()
	request := encodingRequestOf(patch.Encoding)
	base, err := protocol.NewSourceSnapshotFromRaw(currentBytes, request, limits.Source)
	if err != nil {
		code := sourceFailureCode(err)
		result, err := protocol.NewBatchResultFileEntry(path, protocol.ResultStatusFailed,
			&code, nil, false)
		if err != nil {
			return nil, err
		}
		return result, nil
	}
	_, err = protocol.ApplySourcePatch(patch, base, limits)
	if err != nil {
		code := sourceFailureCode(err)
		result, err := protocol.NewBatchResultFileEntry(path, protocol.ResultStatusFailed,
			&code, nil, false)
		if err != nil {
			return nil, err
		}
		return result, nil
	}
	targetDigest := patch.TargetDigest
	redacted := PlanFileRedacted(entry)
	result, err := protocol.NewBatchResultFileEntry(path, protocol.ResultStatusCompleted,
		nil, &targetDigest, redacted)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// PlanFileRedacted reports whether the plan file's edit operations contain
// at least one key name matching a redaction pattern (RFC 0015 §9.2
// `redacted` fact; the frozen v1 key-name pattern set of RFC 0015 §11.2).
// The flag is presentation-adjacent audit metadata: patch application
// bytes are unchanged by any redaction setting (RFC 0015 hard gate 3).
func PlanFileRedacted(entry *protocol.BatchPlanFileEntry) bool {
	for _, operation := range entry.Operations() {
		for _, value := range operation.Summary {
			if KeyMatchesRedactionPattern(value) {
				return true
			}
		}
	}
	return false
}

// redactionPatterns is the frozen v1 key-name pattern set of RFC 0015
// §11.2, expanded to the case-insensitive substring literals of the
// regex `(?i)(password|passwd|secret|token|api[_-]?key|private[_-]?key|
// access[_-]?key|credential|auth)`.
var redactionPatterns = []string{
	"password", "passwd", "secret", "token",
	"apikey", "api_key", "api-key",
	"privatekey", "private_key", "private-key",
	"accesskey", "access_key", "access-key",
	"credential", "auth",
}

// KeyMatchesRedactionPattern reports whether one key name matches the
// frozen v1 redaction key-name pattern set (RFC 0015 §11.2: case-
// insensitive, matched as a substring).
func KeyMatchesRedactionPattern(name string) bool {
	lower := strings.ToLower(name)
	for _, pattern := range redactionPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// BatchApplyError is the typed failure of applying one plan entry: the
// entry is not a planned entry (RFC 0015 §8.3: naked operations and
// non-plan files are rejected). Per-file precondition failures are not
// errors — they become failed or skipped-stale result entries with their
// frozen failure codes (RFC 0015 §9.2).
type BatchApplyError struct {
	// Path is the plan entry path.
	Path string
	// Reason explains the rejection.
	Reason string
}

// Error implements error; the text is human presentation only (RFC 0016
// §6).
func (e *BatchApplyError) Error() string {
	return "consema: batch apply rejected " + e.Path + ": " + e.Reason
}

// Code returns the registered precondition-failed code (RFC 0015 §5.2
// precondition class).
func (e *BatchApplyError) Code() string { return "core.edit.precondition-failed@1" }

// sourceFailureCode maps one protocol source/patch application failure onto
// its frozen registered code (RFC 0015 §5.2 precondition class; the
// source_error code mapping of records_source.go).
func sourceFailureCode(err error) string {
	if coded, ok := err.(interface{ Code() string }); ok {
		return coded.Code()
	}
	return "core.protocol.invalid-value@1"
}

// documentPatchToProtocol converts one document-layer source patch into
// the wire-form patch record (RFC 0015 §8.2 source_patch; the exact bytes
// remain present as precondition facts).
func documentPatchToProtocol(patch *document.SourcePatch) *protocol.SourcePatch {
	documentReplacements := patch.Replacements()
	replacements := make([]protocol.SourceReplacement, 0, len(documentReplacements))
	for _, replacement := range documentReplacements {
		replacements = append(replacements, protocol.SourceReplacement{
			OldStart:          uint64(replacement.OldStart()),
			OldEnd:            uint64(replacement.OldEnd()),
			Original:          replacement.Original(),
			Replacement:       replacement.Replacement(),
			RedactOriginal:    replacement.RedactOriginal(),
			RedactReplacement: replacement.RedactReplacement(),
		})
	}
	return &protocol.SourcePatch{
		BaseDigest:   digestFromDocument(patch.BaseDigest()),
		TargetDigest: digestFromDocument(patch.TargetDigest()),
		Encoding:     encodingFactsToProtocol(patch.EncodingFacts()),
		Replacements: replacements,
		Metadata:     patch.Metadata(),
	}
}

// digestFromDocument carries one document-layer digest into the wire-form
// digest (the identical SHA-256 fact).
func digestFromDocument(digest document.ContentDigest) protocol.ContentDigest {
	return protocol.ContentDigestFromBytes(digest.Bytes())
}

// encodingFactsToProtocol carries one document-layer encoding-facts record
// into the wire-form record (protocol source.rs).
func encodingFactsToProtocol(facts document.EncodingFacts) protocol.EncodingFacts {
	return protocol.EncodingFacts{
		ProfileDefault: encodingToProtocolPtr(facts.ProfileDefault()),
		BomPolicy:      string(facts.BomPolicy()),
		Bom:            bomKindToProtocol(facts.Bom()),
		Declaration:    encodingToProtocolPtrRef(facts.Declaration()),
		CallerOverride: encodingToProtocolPtrRef(facts.CallerOverride()),
		Selected:       encodingToProtocolPtr(facts.Selected()),
	}
}

func encodingToProtocolPtr(encoding document.SourceEncoding) *protocol.SourceEncoding {
	return &protocol.SourceEncoding{Kind: encodingKindToProtocol(encoding.Kind()),
		WindowsCodePage: codePageToProtocol(encoding)}
}

// encodingKindToProtocol maps one document-layer encoding kind onto the
// wire-form kind spelling (protocol source.rs: "Binary", "Utf8",
// "Utf16Le", "Utf16Be", "Latin1", "WindowsCodePage").
func encodingKindToProtocol(kind document.SourceEncodingKind) string {
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

func encodingToProtocolPtrRef(encoding *document.SourceEncoding) *protocol.SourceEncoding {
	if encoding == nil {
		return nil
	}
	return encodingToProtocolPtr(*encoding)
}

func codePageToProtocol(encoding document.SourceEncoding) *uint32 {
	page, ok := encoding.WindowsCodePage()
	if !ok {
		return nil
	}
	number := uint32(page)
	return &number
}

func bomKindToProtocol(bom *document.BomKind) *string {
	if bom == nil {
		return nil
	}
	text := string(*bom)
	return &text
}

// encodingRequestOf rebuilds the encoding-resolution request from the
// wire-form encoding facts of one patch (the resolution request that
// produced those facts; protocol source.rs factsToRequestV2).
func encodingRequestOf(facts protocol.EncodingFacts) protocol.EncodingRequest {
	request := protocol.NewEncodingRequest(facts.ProfileDefault)
	if facts.Declaration != nil {
		request = request.WithDeclaration(facts.Declaration)
	}
	if facts.CallerOverride != nil {
		request = request.WithCallerOverride(facts.CallerOverride)
	}
	switch facts.BomPolicy {
	case string(protocol.BomPolicyTreatAsContent):
		request = request.WithBomPolicy(protocol.BomPolicyTreatAsContent)
	default:
		request = request.WithBomPolicy(protocol.BomPolicyDetectUnicode)
	}
	return request
}
