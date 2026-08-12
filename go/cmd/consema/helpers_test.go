package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/protocol"
)

// protocolLimits returns the frozen default protocol limits.
func protocolLimits() protocol.ProtocolLimits {
	return protocol.DefaultProtocolLimits()
}

// cliOutputMessageType aliases the envelope type for the test decoders.
type cliOutputMessageType = protocol.CliOutputMessage

// Protocol exit-class aliases for the tests.
type protocolExitClass = protocol.ExitClass

const (
	protocolExitSuccess      = protocol.ExitSuccess
	protocolExitUsage        = protocol.ExitUsage
	protocolExitData         = protocol.ExitData
	protocolExitLimit        = protocol.ExitLimit
	protocolExitPrecondition = protocol.ExitPrecondition
	protocolExitInternal     = protocol.ExitInternal
)

// Core value type aliases for the tests.
type coreObject = core.Object
type coreString = core.String
type objectType = core.Object
type stringType = core.String
type boolType = core.Boolean
type arrayType = core.Array

// sampleEnvelope builds one deterministic conformance envelope.
func sampleEnvelope(t *testing.T) *protocol.CliOutputMessage {
	t.Helper()
	payload, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("cli.conformance@1")},
		core.Entry{Key: "suite", Value: core.String(conformanceSuite)},
		core.Entry{Key: "passed", Value: core.NewArray(
			core.String("cli.envelope@1"), core.String("cli.exit-code@1"))},
		core.Entry{Key: "failed", Value: core.NewArray()},
	)
	envelope, err := protocol.NewCliOutputMessage(protocol.CommandConformance,
		protocol.ExitSuccess, productVersion, payload, nil, noRedaction())
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}

// runCLIUnit dispatches one invocation against in-memory writers and
// returns the frozen exit code plus the stdout/stderr bytes.
func runCLIUnit(args ...string) (uint8, []byte, []byte) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runMain(args, &stdout, &stderr)
	return code, stdout.Bytes(), stderr.Bytes()
}

var testDirCounter atomic.Uint64

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

// newTestDir creates one isolated scratch directory, removed on cleanup.
func newTestDir(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(os.TempDir(),
		"consema-cli-"+name+"-"+itoa(os.Getpid())+"-"+itoa(int(testDirCounter.Add(1))))
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(path)
	})
	return path
}

// writeTestFile writes one fixture file and returns its spelling.
func writeTestFile(t *testing.T, dir, name string, bytes []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// encodeEditRequest builds the canonical bytes of one cli.edit-request@1
// wrapper with a single replace-semantic-value operation.
func encodeEditRequest(t *testing.T, section, key, value string) []byte {
	t.Helper()
	reference, err := core.NewObject(
		core.Entry{Key: "id", Value: core.String("ini.edit.replace-semantic-value")},
		core.Entry{Key: "version", Value: coreInteger(1)},
	)
	if err != nil {
		t.Fatal(err)
	}
	var sectionValue core.Value = core.NullValue()
	if section != "" {
		sectionValue = core.String(section)
	}
	target, err := core.NewObject(
		core.Entry{Key: "kind", Value: core.String("entry")},
		core.Entry{Key: "section", Value: sectionValue},
		core.Entry{Key: "key", Value: core.String(key)},
		core.Entry{Key: "occurrence", Value: coreInteger(0)},
	)
	if err != nil {
		t.Fatal(err)
	}
	arguments, err := core.NewObject(
		core.Entry{Key: "value", Value: core.String(value)},
		core.Entry{Key: "representation_policy", Value: core.String("preserve-compatible")},
	)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := core.NewObject(
		core.Entry{Key: "operation", Value: reference},
		core.Entry{Key: "target", Value: target},
		core.Entry{Key: "arguments", Value: arguments},
	)
	if err != nil {
		t.Fatal(err)
	}
	wrapper, err := core.NewObject(
		core.Entry{Key: "schema", Value: core.String(editRequestSchema)},
		core.Entry{Key: "operations", Value: core.NewArray(operation)},
	)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := protocol.EncodeJSON(wrapper, protocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	return bytes
}

// iniSource is the standard INI test fixture.
func iniSource() []byte {
	return []byte("[db]\nport=8080\npassword=hunter2\n")
}

// iniTarget is the expected target after the fixture edit (port -> 9090).
func iniTarget() []byte {
	return []byte("[db]\nport=9090\npassword=hunter2\n")
}

// runRequestCommand dispatches one request command against already-read
// request bytes (no stdin).
func runRequestCommand(t *testing.T, args []string, request []byte) (uint8, []byte, []byte) {
	t.Helper()
	parsed, err := parseArgs(args)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var code uint8
	switch parsed.command {
	case protocol.CommandQuery:
		code = runQueryWithRequest(parsed, request, &stdout, &stderr)
	case protocol.CommandProject:
		code = runProjectWithRequest(parsed, request, &stdout, &stderr)
	case protocol.CommandMaterialize:
		code = runMaterializeWithRequest(parsed, request, &stdout, &stderr)
	case protocol.CommandConvert:
		code = runConvertWithRequest(parsed, request, &stdout, &stderr)
	case protocol.CommandEdit:
		policy, policyErr := compileRedactPolicy(parsed)
		if policyErr != nil {
			code = emitFailure(protocol.CommandEdit, parsed, policyErr, nil, &stdout, &stderr)
			break
		}
		code = runEditWithRequest(parsed, request, policy, &stdout, &stderr)
	case protocol.CommandPlan:
		policy, policyErr := compileRedactPolicy(parsed)
		if policyErr != nil {
			code = emitFailure(protocol.CommandPlan, parsed, policyErr, nil, &stdout, &stderr)
			break
		}
		code = runPlanWithRequest(parsed, request, policy, &stdout, &stderr)
	default:
		t.Fatalf("not a request command: %s", parsed.command)
	}
	return code, stdout.Bytes(), stderr.Bytes()
}

// envelopeOf decodes one --json stdout buffer as the envelope.
func envelopeOf(t *testing.T, stdout []byte) *protocol.CliOutputMessage {
	t.Helper()
	if len(stdout) == 0 || stdout[len(stdout)-1] != '\n' {
		t.Fatalf("envelope line must end in one LF: %q", stdout)
	}
	if bytes.Contains(stdout[:len(stdout)-1], []byte("\n")) {
		t.Fatalf("exactly one line expected: %q", stdout)
	}
	decoded := &protocol.CliOutputMessage{}
	decoded, err := decoded.FromJSON(stdout[:len(stdout)-1], protocolLimits())
	if err != nil {
		t.Fatalf("byte-valid envelope: %v", err)
	}
	return decoded
}

// stderrText renders the stderr bytes as text.
func stderrText(stderr []byte) string {
	return string(stderr)
}

// coreInteger builds one non-negative integer value.
func coreInteger(value uint64) core.Value {
	return integerValueOf(value)
}

var _ io.Writer
