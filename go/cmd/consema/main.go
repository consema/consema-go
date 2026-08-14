// The official `consema` CLI binary (Go implementation).
//
// Entry point, command dispatch, exit-code wiring, and stdout/stderr
// separation (mirror of the Rust bin's main.rs). The binary is stdlib-only.
// Command implementations reach format semantics through the root package
// and the family packages' public APIs — flow.go imports the eight family
// packages directly, a documented deviation from RFC 0015 §2.3 hard gate 1
// (the Rust bin reaches semantics only through the facade; the Go facade
// does not expose every format entry point, so the CLI calls the family
// packages' public APIs; G055, adversarial audit 2026-08-13). All 11
// formal commands are wired.
//
// Exit-code wiring: every error path maps through
// protocol.ClassifyErrorCode (RFC 0015 §5); the process exits with the
// classified code, never a hand-picked number. Under `--json`, stdout
// carries exactly one line of canonical `core.cli-output@1` envelope and
// nothing else; all diagnostics and progress go to stderr (RFC 0015 §3.3).
//
// Interruption (RFC 0015 §5.4): SIGINT/SIGTERM trigger graceful shutdown.
// The apply command's state machine handles the signal at the exact code
// point (after the pending manifest, before the target write) through the
// documented `CONSEMA_APPLY_INTERRUPT_AFTER` injection seam and the real
// os/signal handler; all other commands exit 4 with
// `cli.interrupted.signal@1` on stderr and no further stdout bytes.
package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"consema.dev/consema/protocol"
)

// interruptRequested is set by the signal handler; the apply state machine
// polls it at the interruption code point (RFC 0015 §5.4).
var interruptRequested = make(chan struct{}, 1)

// applyActive reports whether the apply state machine is running; while it
// is, the signal handler defers the graceful shutdown to the state machine
// (the pending manifest must be written first, RFC 0015 §9.3 step 3). The
// flag is an atomic.Bool — the signal-handling goroutine reads it while
// the apply state machine writes it (wave-4 2026-08-15, ENTRY 49: a plain
// bool was a data race; the Rust reference uses an AtomicBool at the same
// position, consema-rs main.rs).
var applyActive atomic.Bool

func main() {
	os.Exit(int(runMain(os.Args[1:], os.Stdout, os.Stderr)))
}

// runMain runs one parsed invocation against the given writers and returns
// the frozen process exit code. Both writers are injected for testability.
func runMain(args []string, stdout, stderr io.Writer) uint8 {
	// The deterministic stand-in for OS signals on platforms where the
	// stdlib cannot install a handler: the apply command polls the channel
	// AND the documented env seam at the interruption code point.
	installSignalHandler()
	if err := collectArgs(args); err != nil {
		writeUsageError(err, stderr)
		return usageExitCode(err)
	}
	parsed, err := parseArgs(args)
	if err != nil {
		writeUsageError(err, stderr)
		return usageExitCode(err)
	}
	if parsed.help {
		fmt.Fprintf(stdout, "consema %s — deterministic multi-format configuration tool\n",
			productVersion)
		io.WriteString(stdout, helpText)
		return protocol.ExitSuccess.ExitCode()
	}
	if parsed.version {
		fmt.Fprintf(stdout, "%s\n", productVersion)
		return protocol.ExitSuccess.ExitCode()
	}
	command := parsed.command
	switch command {
	case protocol.CommandCapabilities:
		return runCapabilities(parsed, stdout, stderr)
	case protocol.CommandConformance:
		return runConformance(parsed, stdout, stderr)
	case protocol.CommandExplain:
		return runExplain(parsed, stdout, stderr)
	case protocol.CommandInspect:
		return runInspect(parsed, stdout, stderr)
	case protocol.CommandQuery:
		return runQuery(parsed, stdout, stderr)
	case protocol.CommandProject:
		return runProject(parsed, stdout, stderr)
	case protocol.CommandMaterialize:
		return runMaterialize(parsed, stdout, stderr)
	case protocol.CommandConvert:
		return runConvert(parsed, stdout, stderr)
	case protocol.CommandEdit:
		return runEdit(parsed, stdout, stderr)
	case protocol.CommandPlan:
		return runPlan(parsed, stdout, stderr)
	case protocol.CommandApply:
		return runApply(parsed, stdout, stderr)
	}
	// parseArgs rejects a missing command unless help/version was requested;
	// keep the path closed defensively.
	error := missingCommandError()
	writeUsageError(error, stderr)
	return usageExitCode(error)
}

// usageExitCode returns the frozen exit code for one usage-class parse
// failure (RFC 0015 §5.2: cli.usage.* -> 1).
func usageExitCode(error *ParseError) uint8 {
	return protocol.ClassifyErrorCode(error.Code()).ExitCode()
}

// writeUsageError writes one deterministic stderr diagnostic line for a
// usage failure.
func writeUsageError(error *ParseError, stderr io.Writer) {
	fmt.Fprintf(stderr, "consema: error: %s (code %s)\n", error.Error(), error.Code())
}

// installSignalHandler installs the RFC 0015 §5.4 graceful-shutdown signal
// handling: SIGINT/SIGTERM set the interrupt channel. The apply state
// machine polls it at the interruption code point (after the pending
// manifest, before the target write) and defers the exit to itself; every
// other command has no state to preserve, so the handler writes the
// interruption diagnostic on stderr and exits 4 without further stdout
// bytes (RFC 0015 §4.2: interruption never produces an envelope).
func installSignalHandler() {
	channel := make(chan os.Signal, 1)
	signal.Notify(channel, os.Interrupt, syscall.SIGTERM)
	go func() {
		for range channel {
			select {
			case interruptRequested <- struct{}{}:
			default:
			}
			if applyActive.Load() {
				// The apply state machine owns the graceful shutdown: the
				// pending manifest is written first (RFC 0015 §9.3 step 3),
				// then the exit happens at the interruption code point.
				continue
			}
			fmt.Fprintln(os.Stderr,
				"consema: error: interrupted by SIGINT/SIGTERM (code cli.interrupted.signal@1)")
			os.Exit(int(protocol.ClassifyErrorCode("cli.interrupted.signal@1").ExitCode()))
		}
	}()
}

// pollInterrupt reports whether a signal was requested at the interruption
// code point (RFC 0015 §5.4); the apply state machine checks it after the
// pending manifest and before the target write.
func pollInterrupt() bool {
	select {
	case <-interruptRequested:
		return true
	default:
		return false
	}
}
