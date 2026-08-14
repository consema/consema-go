package main

// `consema apply`: the batch write command (RFC 0015 §9; mirror of the Rust
// bin's apply.rs).
//
// `apply` consumes one plan manifest (the positional path; strictly decoded
// as `core.batch-plan@1`) and, per file, executes the frozen six-step flow
// of RFC 0015 §9.3:
//
// ```text
// 1. re-read the file, recompute the digest, compare against the plan's
//    source_digest              ← stale → skipped-stale
//                                  (core.source.patch-base-mismatch@1), no write
// 2. verify each replacement's original-bytes precondition (SourcePatch
//    semantics; the SDK re-checks the base digest, encoding facts, and every
//    original byte at its offset)
// 3. mark this file pending and persist the result manifest (pending
//    first — a crash here leaves the file untouched and truthfully pending)
// 4. atomically write the target file (fsio: same-directory temp + atomic
//    replace, symlink/read-only/directory policy enforced)
// 5. read back and verify the target digest
// 6. mark this file completed and persist the result manifest
// ```
//
// Before the fresh flow, every file is branched through the RFC 0015 §9.4
// recovery three-way rule on the **current disk bytes**: digest ==
// `source_digest` → execute the full flow; digest == `target_digest` →
// already effective, mark completed and skip; any other digest →
// `skipped-stale` (exit 4).
//
// The result manifest (`core.batch-result@1`) is persisted **before any
// write** in an all-pending state and **after each file** in its new state,
// through the fsio atomic engine; the default path is `{plan-file}.result.json`
// (RFC 0015 §8.3), overridden by `--output`.
//
// Exit codes: any `failed` or `skipped-stale` file → exit 4 (RFC 0015 §5.2);
// all `completed` → 0. CLI-layer failures classify normally: usage 1,
// plan-manifest decode 2, limits 3, result-manifest write 4, interruption 4.
//
// Interruption (RFC 0015 §5.4/§9.4): the graceful-shutdown sequence is
// reachable through the documented injection point
// `CONSEMA_APPLY_INTERRUPT_AFTER=<n>` (0-based file index), which fires
// after the pending manifest of file `n` is persisted (step 3) and before
// its target write (step 4); a real SIGINT/SIGTERM is handled at the same
// code point through the stdlib os/signal handler. The sequence writes no
// further bytes to stdout (RFC 0015 §4.2), emits
// `cli.interrupted.signal@1` on stderr, and exits 4, leaving the in-flight
// file pending in the on-disk manifest.
//
// Failure injection: `CONSEMA_APPLY_WRITE_FAILURE=permission|io` makes the
// **first** atomic target write fail with the named `cli.write.*` error.
// All other failure injections (stale digest, tampered patch, read-only,
// symlink/junction, directory target) use real filesystem states.

import (
	"fmt"
	"io"
	"strings"

	consema "consema.dev/consema"
	"consema.dev/consema/protocol"
)

// STALE_CODE is the frozen stale-digest failure code of RFC 0015 §9.3 step
// 1/§9.4.
const staleCode = "core.source.patch-base-mismatch@1"

// interruptedCode is the frozen interruption code of RFC 0015 §13.1.
const interruptedCode = "cli.interrupted.signal@1"

// The documented process-level injection seam (RFC 0015 §5.4).
const (
	interruptAfterEnv = "CONSEMA_APPLY_INTERRUPT_AFTER"
	writeFailureEnv   = "CONSEMA_APPLY_WRITE_FAILURE"
)

// applyInjections is the documented process-level injection seam; an
// absent, malformed, or out-of-range value disables the injection, and no
// other command reads the environment. The environment read itself
// (fromEnv) is build-tag gated: dev/test builds (the default) keep the
// seam for the e2e tests, while a production binary built with `-tags
// release` compiles it out entirely — a stray environment variable can
// never inject a fake write failure or interruption into a release binary
// (G114, adversarial audit 2026-08-14, rs G045-aligned; see
// apply_injections_dev.go / apply_injections_release.go).
type applyInjections struct {
	// InterruptAfter fires the graceful-shutdown sequence after the pending
	// manifest of this 0-based file index.
	interruptAfter *int
	// WriteFailure fails the first atomic target write with these (code,
	// message) facts.
	writeFailure *writeFailureInjection
	// Fired reports whether the injected write failure was consumed.
	writeFailureFired bool
}

type writeFailureInjection struct {
	code    string
	message string
}

// takeWriteFailure returns the injected write failure, consumed exactly
// once (the first atomic target write of the run).
func (i *applyInjections) takeWriteFailure() *writeFailureInjection {
	if i.writeFailureFired || i.writeFailure == nil {
		return nil
	}
	i.writeFailureFired = true
	return i.writeFailure
}

// EntryState is the per-file result state (the mutable working form of a
// `core.batch-result@1` entry).
type entryState struct {
	// Path is the user-supplied path spelling, verbatim (RFC 0015 §3.3).
	Path string
	// Status is the current per-file status.
	Status protocol.BatchResultFileStatus
	// FailureCode is the frozen failure code of failed/skipped-stale files.
	FailureCode *string
	// TargetDigest is the verified target digest of completed files.
	TargetDigest *protocol.ContentDigest
	// Redacted reports whether the file's edit operations match a
	// redaction key pattern.
	Redacted bool
}

// batchOutcome is the outcome of one apply run.
type batchOutcome struct {
	// Entries are the per-file result states in plan order.
	Entries []entryState
	// Interrupted reports whether the run was interrupted (the pending
	// manifest stays on disk).
	Interrupted bool
}

// runApply runs `consema apply` (the plan manifest is the single
// positional; the result manifest defaults to `{plan-file}.result.json`,
// overridden by `--output`).
func runApply(parsed *ParsedArgs, stdout, stderr io.Writer) uint8 {
	policy, err := compileRedactPolicy(parsed)
	if err != nil {
		return emitFailure(protocol.CommandApply, parsed, err, nil, stdout, stderr)
	}
	planPath := parsed.positionals[0]
	cap := uint64(protocol.DefaultProtocolLimits().MaxBytes)
	if parsed.maxBytes != nil {
		cap = *parsed.maxBytes
	}
	planBytes, err := readPlanFile(planPath, cap)
	if err != nil {
		return emitFailure(protocol.CommandApply, parsed, err, nil, stdout, stderr)
	}
	plan, err := decodePlanManifest(planBytes)
	if err != nil {
		return emitFailure(protocol.CommandApply, parsed, err, nil, stdout, stderr)
	}
	fileCap := defaultMaxFiles
	if parsed.maxFiles != nil {
		fileCap = *parsed.maxFiles
	}
	if uint64(len(plan.Files())) > fileCap {
		error := newFlowError("cli.limit.batch-count@1",
			fmt.Sprintf("plan batch of %d files exceeds the %d-file cap (--max-files)",
				len(plan.Files()), fileCap))
		return emitFailure(protocol.CommandApply, parsed, error, nil, stdout, stderr)
	}
	resultPath := planPath + ".result.json"
	if parsed.output != nil {
		resultPath = *parsed.output
	}
	injections := applyInjections{}
	injections.fromEnv()
	applyActive = true
	outcome, err := runBatch(plan, resultPath, cap, policy, &injections, stderr)
	applyActive = false
	if err != nil {
		return emitFailure(protocol.CommandApply, parsed, err, nil, stdout, stderr)
	}
	if outcome.Interrupted {
		// RFC 0015 §5.4/§4.2: after interruption stdout receives no further
		// bytes; the stderr line was already written by the state machine,
		// and the pending manifest stays on disk.
		return protocol.ClassifyErrorCode(interruptedCode).ExitCode()
	}
	exitClass := protocol.ExitSuccess
	for _, entry := range outcome.Entries {
		if entry.Status == protocol.ResultStatusFailed ||
			entry.Status == protocol.ResultStatusSkippedStale {
			exitClass = protocol.ExitPrecondition
			break
		}
	}
	message, err := resultMessage(outcome.Entries)
	if err != nil {
		return emitFailure(protocol.CommandApply, parsed, err, nil, stdout, stderr)
	}
	if parsed.json {
		value, valueErr := message.ToValue()
		if valueErr != nil {
			return internalFailure("apply", valueErr.Error(), stderr)
		}
		if emitErr := emitCommandEnvelope(protocol.CommandApply, exitClass,
			value, nil, parsed, nil, stdout); emitErr != nil {
			return internalFailure("apply", emitErr.Error(), stderr)
		}
		return exitClass.ExitCode()
	}
	if writeErr := writeApplyReport(outcome.Entries, stdout); writeErr != nil {
		return internalFailure("apply", writeErr.Error(), stderr)
	}
	return exitClass.ExitCode()
}

// readPlanFile reads the plan-manifest file with the CLI byte cap (RFC 0015
// §12: the manifest-size cap is cli.limit.manifest-size@1, an unreadable
// plan is cli.data.io@1).
func readPlanFile(path string, cap uint64) ([]byte, *FlowError) {
	bytes, err := readCapped(path, cap)
	if err == errOverLimit {
		return nil, newFlowError("cli.limit.manifest-size@1",
			fmt.Sprintf("plan manifest '%s' exceeds the %d-byte input cap", path, cap))
	}
	if err != nil {
		return nil, ioReadFlowError("cli.data.io@1",
			"cannot read plan manifest '"+path+"'", err)
	}
	return bytes, nil
}

// runBatch runs the apply state machine against the real filesystem (RFC
// 0015 §9.3). Every state transition persists the result manifest
// atomically; a persistence failure aborts the whole run with its
// cli.write.* error (the batch cannot continue truthfully without a
// recovery record).
func runBatch(plan *protocol.BatchPlanMessage, resultPath string, cap uint64,
	policy *redactPolicy, injections *applyInjections,
	stderr io.Writer) (*batchOutcome, *FlowError) {
	files := plan.Files()
	entries := make([]entryState, 0, len(files))
	for _, entry := range files {
		entries = append(entries, entryState{
			Path:     entry.Path(),
			Status:   protocol.ResultStatusPending,
			Redacted: entryRedacted(entry, policy),
		})
	}
	// RFC 0015 §9.3 step 3 for the whole batch: the pending manifest is
	// persisted BEFORE any target write, so a crash at any point before a
	// file's write leaves that file truthfully pending.
	if err := persistEntries(entries, resultPath); err != nil {
		return nil, err
	}
	for index, planEntry := range files {
		if planEntry.Status() == protocol.PlanStatusFailed {
			// RFC 0015 §9.4: failed files are re-reported on every run —
			// the plan could not plan them, so apply has nothing to write.
			code := "unknown"
			if planEntry.FailureCode() != nil {
				code = *planEntry.FailureCode()
			}
			fmt.Fprintf(stderr,
				"consema: error: apply: %s: plan-time failure re-reported (code %s)\n",
				planEntry.Path(), code)
			entries[index].Status = protocol.ResultStatusFailed
			entries[index].FailureCode = &code
			entries[index].TargetDigest = nil
			if err := persistEntries(entries, resultPath); err != nil {
				return nil, err
			}
			continue
		}
		// Planned files: the RFC 0015 §9.4 three-way recovery rule branches
		// on the current disk bytes first.
		patch := planEntry.SourcePatch()
		sourceDigest := *planEntry.SourceDigest()
		targetDigest := patch.TargetDigest
		// Step 1 (RFC 0015 §9.3): re-read the file and recompute the
		// digest.
		bytes, failureCode, failureMessage := readTarget(planEntry.Path(), cap)
		if failureCode != "" {
			failEntry(&entries[index], failureCode, failureMessage, stderr)
			if err := persistEntries(entries, resultPath); err != nil {
				return nil, err
			}
			continue
		}
		digest := protocol.DigestOf(bytes)
		if digest.Equal(targetDigest) {
			// RFC 0015 §9.4: already effective — mark completed, skip (no
			// rewrite; the bytes were verified against the plan's target
			// digest just now).
			entries[index].Status = protocol.ResultStatusCompleted
			entries[index].FailureCode = nil
			entries[index].TargetDigest = &targetDigest
			if err := persistEntries(entries, resultPath); err != nil {
				return nil, err
			}
			continue
		}
		if !digest.Equal(sourceDigest) {
			// RFC 0015 §9.3 step 1/§9.4 third branch: the current bytes no
			// longer match the planned base — skipped-stale, no write at
			// all.
			failEntryStale(&entries[index],
				"the current file bytes no longer match the planned base digest; "+
					"the file was not rewritten", stderr)
			if err := persistEntries(entries, resultPath); err != nil {
				return nil, err
			}
			continue
		}
		// Step 2 (RFC 0015 §9.3): verify each replacement's original-bytes
		// precondition. The root package's ApplyPlanFile re-checks the base
		// digest, the encoding facts, and every original byte at its offset
		// (SourcePatch semantics — the CLI re-implements none of it) and
		// assembles the per-file result entry.
		result, applyErr := consema.ApplyPlanFile(planEntry, bytes,
			protocol.DefaultSourcePatchLimits())
		if applyErr != nil {
			code := "core.edit.precondition-failed@1"
			if coded, ok := applyErr.(interface{ Code() string }); ok {
				code = coded.Code()
			}
			message := "the plan's source patch does not apply to the current bytes: " +
				applyErr.Error()
			failEntry(&entries[index], code, message, stderr)
			if err := persistEntries(entries, resultPath); err != nil {
				return nil, err
			}
			continue
		}
		if result.Status() != protocol.ResultStatusCompleted {
			// The precondition verification failed (original-bytes
			// mismatch or encoding conflict): the result entry carries the
			// frozen failure code.
			entries[index].Status = result.Status()
			entries[index].FailureCode = result.FailureCode()
			entries[index].TargetDigest = nil
			failEntry(&entries[index], *result.FailureCode(),
				"the plan's source patch does not apply to the current bytes",
				stderr)
			if err := persistEntries(entries, resultPath); err != nil {
				return nil, err
			}
			continue
		}
		// The exact target bytes come from the SDK's own patch application
		// (the verification above already proved the patch applies, so this
		// re-derivation cannot fail).
		snapshot, snapshotErr := protocol.NewSourceSnapshotFromRaw(bytes,
			patchEncodingRequest(patch.Encoding), protocol.DefaultSourceLimits())
		if snapshotErr != nil {
			code := sourcePatchFailureCode(snapshotErr)
			failEntry(&entries[index], code, snapshotErr.Error(), stderr)
			if err := persistEntries(entries, resultPath); err != nil {
				return nil, err
			}
			continue
		}
		targetBytes, targetErr := protocol.ApplySourcePatch(patch, snapshot,
			protocol.DefaultSourcePatchLimits())
		if targetErr != nil {
			code := sourcePatchFailureCode(targetErr)
			failEntry(&entries[index], code, targetErr.Error(), stderr)
			if err := persistEntries(entries, resultPath); err != nil {
				return nil, err
			}
			continue
		}
		// Step 3 (RFC 0015 §9.3): mark this file pending and persist the
		// manifest BEFORE the target write.
		entries[index].Status = protocol.ResultStatusPending
		entries[index].FailureCode = nil
		entries[index].TargetDigest = nil
		if err := persistEntries(entries, resultPath); err != nil {
			return nil, err
		}
		// The documented interruption injection point: a SIGINT/SIGTERM
		// would be handled exactly here, after the pending manifest and
		// before the write (RFC 0015 §5.4; the graceful-shutdown sequence).
		if pollInterrupt() ||
			(injections.interruptAfter != nil && *injections.interruptAfter == index) {
			fmt.Fprintf(stderr,
				"consema: error: apply: interrupted by SIGINT/SIGTERM: the result "+
					"manifest keeps the in-flight file '%s' pending; re-run apply with "+
					"the same plan to resume (code %s)\n", planEntry.Path(), interruptedCode)
			return &batchOutcome{Entries: entries, Interrupted: true}, nil
		}
		// Steps 4 + 5 (RFC 0015 §9.3): atomic write with read-back target
		// digest verification, all inside fsio.
		var writeErr *WriteError
		if injected := injections.takeWriteFailure(); injected != nil {
			writeErr = &WriteError{Code: injected.code, Message: injected.message,
				Target: planEntry.Path()}
		} else {
			_, writeErr = writeAtomic(planEntry.Path(), targetBytes,
				defaultWriteOptions())
		}
		if writeErr == nil {
			// Step 6: completed — the on-disk bytes were verified by the
			// read-back to be exactly the written bytes.
			entries[index].Status = protocol.ResultStatusCompleted
			entries[index].FailureCode = nil
			verified := targetDigest
			entries[index].TargetDigest = &verified
			if err := persistEntries(entries, resultPath); err != nil {
				return nil, err
			}
		} else {
			code, message := writeFailureFacts(writeErr)
			failEntry(&entries[index], code, message, stderr)
			if err := persistEntries(entries, resultPath); err != nil {
				return nil, err
			}
		}
	}
	return &batchOutcome{Entries: entries, Interrupted: false}, nil
}

// patchEncodingRequest rebuilds the encoding-resolution request from the
// wire-form encoding facts of one patch (the resolution request that
// produced those facts; the same mapping the root package's batch.go uses).
func patchEncodingRequest(facts protocol.EncodingFacts) protocol.EncodingRequest {
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

// sourcePatchFailureCode maps one protocol source/patch application failure
// onto its frozen registered code.
func sourcePatchFailureCode(err error) string {
	if coded, ok := err.(interface{ Code() string }); ok {
		return coded.Code()
	}
	return "core.protocol.invalid-value@1"
}

// entryRedacted computes the per-file `redacted` fact of RFC 0015
// §9.2/§11.3: the file's edit operations (the plan manifest's operation
// summaries) contain at least one key name matching the presentation
// redaction policy.
func entryRedacted(planEntry *protocol.BatchPlanFileEntry,
	policy *redactPolicy) bool {
	for _, operation := range planEntry.Operations() {
		for name := range operation.Summary {
			if keyMatches(policy, name) {
				return true
			}
		}
	}
	return false
}

// readTarget reads one target file with the CLI byte cap (RFC 0015 §12).
// Per-file failures are (code, message) facts recorded in the batch result:
// an over-cap file is cli.limit.file-size@1, an unreadable file is
// cli.data.io@1 (RFC 0015 §5.2: file-level failures are uniformly
// unfulfilled write preconditions → exit 4).
func readTarget(path string, cap uint64) ([]byte, string, string) {
	bytes, err := readCapped(path, cap)
	if err == errOverLimit {
		return nil, "cli.limit.file-size@1",
			fmt.Sprintf("source file '%s' exceeds the %d-byte read cap", path, cap)
	}
	if err != nil {
		return nil, "cli.data.io@1",
			fmt.Sprintf("cannot read source file '%s': %v", path, err)
	}
	return bytes, "", ""
}

// writeFailureFacts normalizes one fsio write failure into the per-file
// failure facts. A read-back digest mismatch (RFC 0015 §9.3 step 5) carries
// the frozen `core.source.patch-target-mismatch@1` code with the
// `cli.write.io@1` environment diagnostic named on the stderr line; every
// other failure keeps its frozen `cli.write.*` code.
func writeFailureFacts(err *WriteError) (string, string) {
	if err.Code == "core.source.patch-target-mismatch@1" {
		return err.Code, err.Message + " (environment diagnostic cli.write.io@1)"
	}
	return err.Code, err.Message
}

// failEntry marks one entry failed and writes its deterministic stderr line.
func failEntry(entry *entryState, code, message string, stderr io.Writer) {
	entry.Status = protocol.ResultStatusFailed
	entry.FailureCode = &code
	entry.TargetDigest = nil
	fmt.Fprintf(stderr, "consema: error: apply: %s: %s (code %s)\n",
		entry.Path, message, code)
}

// failEntryStale marks one entry skipped-stale and writes its deterministic
// stderr line.
func failEntryStale(entry *entryState, message string, stderr io.Writer) {
	entry.Status = protocol.ResultStatusSkippedStale
	entry.FailureCode = stringPointer(staleCode)
	entry.TargetDigest = nil
	fmt.Fprintf(stderr, "consema: error: apply: %s: %s (code %s)\n",
		entry.Path, message, staleCode)
}

func stringPointer(text string) *string { return &text }

// resultMessage builds the transferable result message of the current state
// (the entries are constructed by the machine with the §9.2 presence rules,
// so the protocol validation cannot fail; the error is defensive).
func resultMessage(entries []entryState) (*protocol.BatchResultMessage, *FlowError) {
	files := make([]*protocol.BatchResultFileEntry, 0, len(entries))
	for _, entry := range entries {
		file, err := protocol.NewBatchResultFileEntry(entry.Path, entry.Status,
			entry.FailureCode, entry.TargetDigest, entry.Redacted)
		if err != nil {
			return nil, newFlowError("cli.internal.unclassified@1",
				"result entry construction failed: "+err.Error())
		}
		files = append(files, file)
	}
	message, err := protocol.NewBatchResultMessage(productVersion, files)
	if err != nil {
		return nil, newFlowError("cli.internal.unclassified@1",
			"batch-result construction failed: "+err.Error())
	}
	return message, nil
}

// persistEntries encodes and atomically persists the current result state
// (RFC 0015 §9.3 manifest ordering: pending before a write,
// completed/failed after).
func persistEntries(entries []entryState, resultPath string) *FlowError {
	message, err := resultMessage(entries)
	if err != nil {
		return err
	}
	bytes, err := encodeResultManifest(message)
	if err != nil {
		return err
	}
	return persistResultManifest(resultPath, bytes)
}

// writeApplyReport writes the deterministic human apply report (same
// per-file facts as the machine manifest, rendered as text).
func writeApplyReport(entries []entryState, stdout io.Writer) error {
	var text strings.Builder
	fmt.Fprintf(&text, "consema apply: %d file(s)\n", len(entries))
	for _, entry := range entries {
		switch entry.Status {
		case protocol.ResultStatusCompleted:
			digest := "?"
			if entry.TargetDigest != nil {
				digest = entry.TargetDigest.Hex()
			}
			fmt.Fprintf(&text, "  %s: completed (target sha256:%s)\n", entry.Path, digest)
		case protocol.ResultStatusFailed:
			code := "?"
			if entry.FailureCode != nil {
				code = *entry.FailureCode
			}
			fmt.Fprintf(&text, "  %s: failed %s\n", entry.Path, code)
		case protocol.ResultStatusPending:
			fmt.Fprintf(&text, "  %s: pending\n", entry.Path)
		case protocol.ResultStatusSkippedStale:
			code := "?"
			if entry.FailureCode != nil {
				code = *entry.FailureCode
			}
			fmt.Fprintf(&text, "  %s: skipped-stale %s\n", entry.Path, code)
		}
	}
	_, err := io.WriteString(stdout, text.String())
	return err
}
