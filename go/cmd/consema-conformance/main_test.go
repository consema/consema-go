package main

import (
	"os"
	"testing"
)

// TestUsageErrorsExitOne pins RFC 0015 §5.1 exit-code classification for
// the conformance runner CLI (G141, adversarial audit 2026-08-13): usage
// errors exit 1, never the flag package's default 2, and usage errors
// carry no machine envelope (RFC 0015 §4.2 — they surface only as the
// process exit code).
func TestUsageErrorsExitOne(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"unknown flag", []string{"-definitely-not-a-flag"}},
		{"missing required flags", nil},
		// Wave-4 (2026-08-15, ENTRY 13): extra positional arguments are a
		// usage error — the CLI previously executed the run and silently
		// ignored them.
		{"extra positional argument", []string{"-vectors", "v", "-fixtures", "f", "surplus"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := runWithArgs(tc.args)
			if code != 1 {
				t.Fatalf("runWithArgs(%v) = %d, want 1 (exit_class usage)", tc.args, code)
			}
		})
	}
}

// TestRunWithArgsStub ensures the stub keeps working when os.Args is
// irrelevant: run() delegates to runWithArgs(os.Args[1:]).
func TestRunDelegatesToRunWithArgs(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"consema-conformance", "-not-a-real-flag"}
	if code := run(); code != 1 {
		t.Fatalf("run() = %d, want 1 (usage error propagates through the os.Args path)", code)
	}
}
