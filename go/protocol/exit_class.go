package protocol

// This file implements the frozen CLI exit classes and the pure error
// classification (consema-rs/crates/consema-protocol/src/exit_class.rs). RFC 0015 §5
// freezes the six exit classes, their codes (0-5), and the stable mapping
// from error families to classes. ClassifyErrorCode is a pure function
// implemented once in the Go protocol layer (RFC 0016 §6: "the SDK itself
// never classifies"); the Go CLI applies the mapped code only.

// ExitClass is one of the six frozen CLI exit classes (RFC 0015 §5.1).
type ExitClass uint8

const (
	// ExitSuccess: the command completed and produced its full result. A
	// Recovered state report, an ambiguity fact report, an
	// unauthorized-loss report, and a plan manifest with per-file `failed`
	// entries are all complete results.
	ExitSuccess ExitClass = iota
	// ExitUsage: argument or syntax error (unknown command, unknown
	// argument, rejected abbreviation, missing or invalid `--format`,
	// missing `--profile` on a parse-class command, `--apply` without a
	// prior plan, invalid `--redact-keys` pattern).
	ExitUsage
	// ExitData: the operation failed on the data itself (FatalFormationFailure
	// including core.source.* diagnostics, an encoding source-contract
	// conflict, an unresolvable ambiguity, a strict request/plan decode
	// failure, or an input-file read failure).
	ExitData
	// ExitLimit: any resource budget was exceeded (SDK limits, CLI
	// file-size/batch/manifest limits, or a ResourceLimit raised while
	// decoding a request).
	ExitLimit
	// ExitPrecondition: a write precondition failed (stale base digest,
	// original-bytes mismatch, edit conflict, permission/disk failure,
	// read-only target, symlink-policy rejection, an apply item that cannot
	// continue after an interruption, or a user interrupt signal).
	ExitPrecondition
	// ExitInternal: an unclassified internal error (a bug; the diagnostic
	// template must name the command, the involved file, and the diagnostic
	// code).
	ExitInternal
)

// ExitCode returns the frozen process exit code for the class (RFC 0015
// §5.1 classification table). Codes 6-255 are reserved and never produced
// by v1.
func (e ExitClass) ExitCode() uint8 {
	switch e {
	case ExitSuccess:
		return 0
	case ExitUsage:
		return 1
	case ExitData:
		return 2
	case ExitLimit:
		return 3
	case ExitPrecondition:
		return 4
	case ExitInternal:
		return 5
	}
	return 5
}

// Name returns the canonical `exit_class` envelope name (RFC 0015 §4.1).
func (e ExitClass) Name() string {
	switch e {
	case ExitSuccess:
		return "success"
	case ExitUsage:
		return "usage"
	case ExitData:
		return "data"
	case ExitLimit:
		return "limit"
	case ExitPrecondition:
		return "precondition"
	case ExitInternal:
		return "internal"
	}
	return "internal"
}

// ParseExitClass parses one canonical envelope name into the closed class
// set.
func ParseExitClass(name string) (ExitClass, bool) {
	switch name {
	case "success":
		return ExitSuccess, true
	case "usage":
		return ExitUsage, true
	case "data":
		return ExitData, true
	case "limit":
		return ExitLimit, true
	case "precondition":
		return ExitPrecondition, true
	case "internal":
		return ExitInternal, true
	}
	return ExitSuccess, false
}

// Classify classifies one exit class into its frozen process exit code. This
// is the identity table of RFC 0015 §5.1: success -> 0, usage -> 1, data ->
// 2, limit -> 3, precondition -> 4, internal -> 5. The exit code expresses
// whether the operation produced a complete result, never the health of the
// data itself.
func Classify(exitClass ExitClass) uint8 {
	return exitClass.ExitCode()
}

// ClassifyErrorCode classifies a stable error code into its frozen exit
// class. The mapping is the exhaustive family table of RFC 0015 §5.2:
//
//   - cli.usage.* -> ExitUsage (1)
//   - cli.data.* and cli.detection.* (ambiguity) -> ExitData (2)
//   - cli.limit.* and any *-resource-limit@1 (core or format-local) ->
//     ExitLimit (3)
//   - cli.write.*, cli.interrupted.signal@1, the
//     core.source.patch-*-mismatch@1 precondition family, and core.edit.*
//     conflicts -> ExitPrecondition (4)
//   - cli.internal.unclassified@1 -> ExitInternal (5)
//   - core.protocol.* strict-decode failures -> ExitData (2), with
//     core.protocol.resource-limit@1 overridden to ExitLimit
//   - core.source.* diagnostics carried by FatalFormationFailure ->
//     ExitData (2)
//   - any code outside these frozen families -> ExitData (2): the operation
//     did not produce a complete result. Format-layer codes pass through
//     unchanged; they never invent new classes.
//
// Report-as-result outcomes (Recovered state reports, ambiguity fact
// reports, unauthorized-loss reports) classify as ExitSuccess (0) at the
// outcome level, not through error codes.
func ClassifyErrorCode(code string) ExitClass {
	if hasPrefix(code, "cli.usage.") {
		return ExitUsage
	}
	if hasPrefix(code, "cli.data.") || hasPrefix(code, "cli.detection.") {
		return ExitData
	}
	if hasPrefix(code, "cli.limit.") {
		return ExitLimit
	}
	if hasPrefix(code, "cli.write.") || hasPrefix(code, "cli.interrupted.") {
		return ExitPrecondition
	}
	if hasPrefix(code, "cli.internal.") {
		return ExitInternal
	}
	if hasSuffix(code, ".resource-limit@1") {
		return ExitLimit
	}
	if hasPrefix(code, "core.source.patch-") && hasSuffix(code, "-mismatch@1") {
		return ExitPrecondition
	}
	if hasPrefix(code, "core.edit.") {
		return ExitPrecondition
	}
	return ExitData
}

func hasPrefix(text, prefix string) bool {
	return len(text) >= len(prefix) && text[:len(prefix)] == prefix
}

func hasSuffix(text, suffix string) bool {
	return len(text) >= len(suffix) && text[len(text)-len(suffix):] == suffix
}
