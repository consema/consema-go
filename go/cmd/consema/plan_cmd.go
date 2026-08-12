package main

// `consema plan`: the read-only multi-file batch planner (RFC 0015 §8;
// mirror of the Rust bin's plan.rs).
//
// Per file, in command-line argument order: read the raw bytes → parse
// under the `--profile` selection → dry-run the `cli.edit-request@1`
// operations (the shared edit_cmd pipeline) → aggregate one
// `core.batch-plan@1` manifest whose file entries are `planned` (with
// profile, source_digest, operation summaries, and the embedded
// `core.source-patch@2`) or `failed` (with the stable failure code and
// diagnostics). **A per-file failure never fails the batch**: the manifest
// is the complete result and carries the failed entry truthfully, so
// `plan` exits 0 even when some files failed to plan (RFC 0015 §5.2).
//
// The plan manifest is an **artifact, not a write authorization**: `plan`
// never writes any target file; the manifest record goes to stdout (the
// `--json` envelope payload line) or, with `--output`, to that path via the
// fsio atomic engine (RFC 0015 §8.3 — the same `core.batch-plan@1` record,
// without envelope wrapping, byte-identical to the envelope payload).
//
// The manifest record itself is **never redacted** (RFC 0015 §8.3; hard
// gate 3). The human view renders each file's operations through the shared
// per-item redaction of edit_cmd, and a deterministic redaction notice goes
// to stderr when any value was replaced.
//
// Resource limits (RFC 0015 §12): the batch file-count cap (`--max-files`,
// default 1000) is cli.limit.batch-count@1; the manifest-size cap travels
// through the transport limits and is cli.limit.manifest-size@1. The
// per-file read cap (`--max-bytes`, default 64 MiB) makes that file a
// `failed` entry (cli.limit.file-size@1).

import (
	"fmt"
	"io"

	consema "consema.dev/consema"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// defaultMaxFiles is the frozen batch file-count cap of RFC 0015 §12.
const defaultMaxFiles uint64 = 1000

// runPlan runs `consema plan` (request from --request-file or stdin; files
// are the positionals).
func runPlan(parsed *ParsedArgs, stdout, stderr io.Writer) uint8 {
	policy, policyErr := compileRedactPolicy(parsed)
	if policyErr != nil {
		return emitFailure(protocol.CommandPlan, parsed, policyErr, nil, stdout, stderr)
	}
	request, err := readRequestBytes(parsed)
	if err != nil {
		return emitFailure(protocol.CommandPlan, parsed, err, nil, stdout, stderr)
	}
	return runPlanWithRequest(parsed, request, policy, stdout, stderr)
}

// runPlanWithRequest runs `consema plan` against already-read request bytes.
func runPlanWithRequest(parsed *ParsedArgs, request []byte,
	policy *redactPolicy, stdout, stderr io.Writer) uint8 {
	input, err := decodeEditRequest(request, parsed)
	if err != nil {
		return emitFailure(protocol.CommandPlan, parsed, err, nil, stdout, stderr)
	}
	cap := defaultMaxFiles
	if parsed.maxFiles != nil {
		cap = *parsed.maxFiles
	}
	if uint64(len(parsed.positionals)) > cap {
		error := newFlowError("cli.limit.batch-count@1",
			fmt.Sprintf("batch of %d files exceeds the %d-file cap (--max-files)",
				len(parsed.positionals), cap))
		return emitFailure(protocol.CommandPlan, parsed, error, nil, stdout, stderr)
	}
	planner := consema.NewBatchPlanner(productVersion)
	renderItems := make([]PlanRenderItem, 0, len(parsed.positionals))
	for _, path := range parsed.positionals {
		// The shared per-file pipeline: read → parse → complete-gate →
		// typed transaction → dry-run. Any per-file failure becomes a
		// `failed` manifest entry; the batch never aborts (RFC 0015 §8.2).
		outcome, failure := prepareEdit(input, path, parsed)
		var plan *document.EditPlan
		if failure == nil {
			plan, failure = dryRunPlan(outcome, path)
		}
		if failure != nil {
			entryErr := planner.AddFailed(path, failure.Code, failure.Diagnostics)
			if entryErr != nil {
				return emitFailure(protocol.CommandPlan, parsed,
					newFlowError("cli.internal.unclassified@1",
						"plan entry construction failed: "+entryErr.Error()),
					nil, stdout, stderr)
			}
			renderItems = append(renderItems, PlanRenderItem{
				Path: path, Planned: false, FailureCode: failure.Code})
			fmt.Fprintf(stderr, "consema: error: plan: %s: %s (code %s)\n",
				path, failure.Message, failure.Code)
			continue
		}
		entryErr := planner.AddPlanned(path, plan)
		if entryErr != nil {
			return emitFailure(protocol.CommandPlan, parsed,
				newFlowError("cli.internal.unclassified@1",
					"plan entry construction failed: "+entryErr.Error()),
				nil, stdout, stderr)
		}
		renderItems = append(renderItems, planRenderItemFromPlan(input, plan, path, policy))
	}
	manifest, buildErr := planner.Build()
	if buildErr != nil {
		return internalFailure("plan",
			"batch-plan construction failed: "+buildErr.Error(), stderr)
	}
	// Encode once: the same bytes go to the envelope payload and, with
	// --output, to the manifest file (RFC 0015 §8.3).
	bytes, err := encodeManifest(manifest)
	if err != nil {
		return emitFailure(protocol.CommandPlan, parsed, err, nil, stdout, stderr)
	}
	if parsed.output != nil {
		if writeErr := persistManifest(*parsed.output, bytes); writeErr != nil {
			return emitFailure(protocol.CommandPlan, parsed, writeErr, nil, stdout, stderr)
		}
	}
	value, encodeErr := manifest.ToValue()
	if encodeErr != nil {
		return internalFailure("plan", "batch-plan encoding failed: "+encodeErr.Error(), stderr)
	}
	if parsed.json {
		if emitErr := emitCommandEnvelope(protocol.CommandPlan, protocol.ExitSuccess,
			value, nil, parsed, nil, stdout); emitErr != nil {
			return internalFailure("plan", emitErr.Error(), stderr)
		}
		return protocol.ExitSuccess.ExitCode()
	}
	redacted, writeErr := writePlanReport(renderItems, stdout)
	if writeErr != nil {
		return internalFailure("plan", writeErr.Error(), stderr)
	}
	redactionNotice("plan", redacted, stderr)
	return protocol.ExitSuccess.ExitCode()
}

// planRenderItemFromPlan builds the human plan-view item of one planned
// file directly from the dry-run plan (the same plan facts the manifest
// entry carries; RFC 0015 §2.4).
func planRenderItemFromPlan(input *EditRequestInput, plan *document.EditPlan,
	path string, policy *redactPolicy) PlanRenderItem {
	operationLines := make([]string, 0, len(input.Operations))
	redacted := uint64(0)
	for _, operation := range input.Operations {
		line, count := operationLine(policy, &operation)
		redacted += count
		operationLines = append(operationLines, line)
	}
	patch := plan.SourcePatch()
	return PlanRenderItem{
		Path:           path,
		Planned:        true,
		OperationLines: operationLines,
		BaseDigest:     patch.BaseDigest().Hex(),
		TargetDigest:   plan.TargetDigest().Hex(),
		Replacements:   len(plan.Replacements()),
		Redacted:       redacted,
	}
}
