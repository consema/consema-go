// Command consema-conformance is the Go conformance runner CLI
// (https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §4.3; RFC 0016 §7). It reads the shared
// language-neutral vectors from explicit repository paths, verifies the
// aggregate vector digest against the Feature-Complete Manifest, executes
// every suite, prints a human report, and emits the machine-readable result
// as an RFC 0015 envelope (CliOutputMessage, command "conformance",
// payload cli.conformance@1). Exit codes follow RFC 0015 §5: 0 success, 1
// usage, 2 data (a non-conformant run), 5 internal.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"consema.dev/consema/conformance"
	"consema.dev/consema/core"
	"consema.dev/consema/protocol"
)

// productVersion pins the RFC 0015 §3.3 product_version string of this CLI's
// envelopes. Version policy (G5.6 decision, mirrored from cmd/consema/
// version.go:15): the Go CLIs report the product release-train version, and
// this CLI ships as part of the 1.0.0-rc.1 milestone, so the conformance CLI
// reports "1.0.0-rc.1" like the main CLI. CI (check-version-consistency)
// asserts this declaration stays in sync with the README "Version:" line.
var productVersion = "1.0.0-rc.1"

func main() {
	os.Exit(run())
}

func run() int {
	return runWithArgs(os.Args[1:])
}

// runWithArgs runs the CLI with explicit arguments (testable; G141,
// adversarial audit 2026-08-13). Flag syntax errors exit 1 (RFC 0015 §5.1
// usage class) — the old flag.ExitOnError default exited 2. Per RFC 0015
// §4.2, usage errors are rejected before command execution and carry no
// machine envelope; they surface only as the process exit code.
func runWithArgs(args []string) int {
	flags := flag.NewFlagSet("consema-conformance", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	vectorsDir := flags.String("vectors", "", "repository conformance/vectors directory (required)")
	fixturesDir := flags.String("fixtures", "", "repository conformance/fixtures directory (required)")
	manifestPath := flags.String("manifest", "", "Feature-Complete Manifest path (default: <vectors>/../../docs/fc-manifest-0.13.0.json)")
	quiet := flags.Bool("quiet", false, "suppress the human report; emit only the machine envelope")
	if err := flags.Parse(args); err != nil {
		// flag already printed the error and usage to stderr.
		return int(protocol.ExitUsage.ExitCode())
	}

	if *vectorsDir == "" || *fixturesDir == "" {
		fmt.Fprintln(os.Stderr, "consema-conformance: -vectors and -fixtures are required")
		flags.Usage()
		return int(protocol.ExitUsage.ExitCode())
	}
	manifest := *manifestPath
	if manifest == "" {
		manifest = conformance.DefaultManifestPath(*vectorsDir)
	}
	runner := &conformance.Runner{
		VectorsDir:   *vectorsDir,
		FixturesDir:  *fixturesDir,
		ManifestPath: manifest,
	}
	report, err := runner.Run()
	if err != nil {
		// The failure path keeps the envelope contract: every invocation
		// emits exactly one RFC 0015 machine envelope. An aborted run
		// (vectors, fixtures, or manifest unreadable) reports
		// exit_class=data with the runner error as a cli.data.io@1
		// diagnostic; stderr keeps the human-readable line.
		fmt.Fprintf(os.Stderr, "consema-conformance: %v\n", err)
		envelope, envelopeErr := failureEnvelope(err)
		if envelopeErr == nil {
			var jsonBytes []byte
			jsonBytes, envelopeErr = envelope.ToJSON(protocol.DefaultProtocolLimits())
			if envelopeErr == nil {
				fmt.Println(string(jsonBytes))
			}
		}
		if envelopeErr != nil {
			fmt.Fprintf(os.Stderr, "consema-conformance: machine envelope: %v\n", envelopeErr)
			return int(protocol.ExitInternal.ExitCode())
		}
		return int(protocol.ExitData.ExitCode())
	}
	if !*quiet {
		printHumanReport(report)
	}
	envelope, err := machineEnvelope(report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "consema-conformance: machine envelope: %v\n", err)
		return int(protocol.ExitInternal.ExitCode())
	}
	jsonBytes, err := envelope.ToJSON(protocol.DefaultProtocolLimits())
	if err != nil {
		fmt.Fprintf(os.Stderr, "consema-conformance: machine envelope encode: %v\n", err)
		return int(protocol.ExitInternal.ExitCode())
	}
	fmt.Println(string(jsonBytes))
	if report.Conformant() {
		return int(protocol.ExitSuccess.ExitCode())
	}
	return int(protocol.ExitData.ExitCode())
}

// printHumanReport mirrors the Rust runner's report shape: suite id, case
// counts, skip reasons, and failures.
func printHumanReport(report *conformance.RunReport) {
	fmt.Printf("conformance vectors digest: %s (recorded %s, %d suites, %d cases)\n",
		report.Digest.Computed, report.Digest.Recorded, report.Digest.Suites, report.Digest.Cases)
	if !report.Digest.OK {
		fmt.Println("digest MISMATCH: the vector inventory differs from the Feature-Complete Manifest")
	}
	for _, suite := range report.Suites {
		fmt.Printf("suite %s: %d passed, %d skipped, %d failed (expected %d cases)\n",
			suite.Suite, len(suite.Passed), len(suite.Skipped), len(suite.Failed), suite.ExpectedCases)
		for _, skip := range suite.Skipped {
			fmt.Printf("  skip %s [%s]: %s\n", skip.ID, skip.Capability, skip.Reason)
		}
		for _, failure := range suite.Failed {
			fmt.Printf("  FAIL %s: %s\n", failure.ID, failure.Message)
		}
	}
	fmt.Printf("total: %d passed, %d skipped, %d failed\n", report.Passed, report.Skipped, report.Failed)
	if report.Conformant() {
		fmt.Println("conformant")
	} else {
		fmt.Println("NOT CONFORMANT")
	}
}

// failureEnvelope builds the RFC 0015 machine envelope of an aborted run
// (Runner.Run failed before any suite executed): command "conformance",
// exit class data, the cli.conformance@1 payload with empty per-suite
// facts, and the runner error as a cli.data.io@1 diagnostic.
func failureEnvelope(runErr error) (*protocol.CliOutputMessage, error) {
	payload, err := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("cli.conformance@1")},
		core.Entry{Key: "suite", Value: core.String("consema.conformance@1")},
		core.Entry{Key: "passed", Value: core.NewArray()},
		core.Entry{Key: "failed", Value: core.NewArray()},
	)
	if err != nil {
		return nil, err
	}
	diagnostic, err := protocol.NewDiagnostic("cli.data.io@1", protocol.CategoryEncoding,
		protocol.SeverityError, nil, nil, nil, []string{runErr.Error()}, nil, 0,
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		return nil, err
	}
	redaction, err := protocol.NewRedaction(false, 0)
	if err != nil {
		return nil, err
	}
	return protocol.NewCliOutputMessage(protocol.CommandConformance, protocol.ExitData,
		productVersion, payload, []*protocol.Diagnostic{diagnostic}, redaction)
}

// machineEnvelope builds the RFC 0015 machine envelope of the run: command
// "conformance", the classified exit class, and the cli.conformance@1
// payload with the per-suite aggregate facts.
func machineEnvelope(report *conformance.RunReport) (*protocol.CliOutputMessage, error) {
	exitClass := protocol.ExitSuccess
	if !report.Conformant() {
		exitClass = protocol.ExitData
	}
	var passed []core.Value
	var failed []core.Value
	for _, suite := range report.Suites {
		if suite.Conformant() {
			passed = append(passed, core.String(suite.Suite))
			continue
		}
		message := strings.Builder{}
		for _, failure := range suite.Failed {
			message.WriteString(failure.ID)
			message.WriteString(": ")
			message.WriteString(failure.Message)
			message.WriteString("; ")
		}
		entry, err := core.NewObject(
			core.Entry{Key: "id", Value: core.String(suite.Suite)},
			core.Entry{Key: "message", Value: core.String(strings.TrimSuffix(message.String(), "; "))},
		)
		if err != nil {
			return nil, err
		}
		failed = append(failed, entry)
	}
	payload, err := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("cli.conformance@1")},
		core.Entry{Key: "suite", Value: core.String("consema.conformance@1")},
		core.Entry{Key: "passed", Value: core.NewArray(passed...)},
		core.Entry{Key: "failed", Value: core.NewArray(failed...)},
	)
	if err != nil {
		return nil, err
	}
	redaction, err := protocol.NewRedaction(false, 0)
	if err != nil {
		return nil, err
	}
	return protocol.NewCliOutputMessage(protocol.CommandConformance, exitClass,
		productVersion, payload, nil, redaction)
}
