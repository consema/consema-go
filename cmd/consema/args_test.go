package main

import (
	"testing"

	"consema.dev/consema/protocol"
)

func parseTest(t *testing.T, args ...string) (*ParsedArgs, *ParseError) {
	t.Helper()
	return parseArgs(args)
}

func TestEveryClosedSetCommandParses(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"inspect", []string{"inspect", "app.conf"}},
		{"capabilities", []string{"capabilities"}},
		{"query", []string{"query", "--profile", "ini.portable"}},
		{"project", []string{"project", "--profile", "json.strict", "--request-file", "r.json"}},
		{"materialize", []string{"materialize", "--profile", "json.strict"}},
		{"convert", []string{"convert", "src.json", "--profile", "json.strict"}},
		{"edit", []string{"edit", "app.conf", "--profile", "ini.portable", "--write"}},
		{"plan", []string{"plan", "a.conf", "b.conf", "--profile", "ini.portable",
			"--output", "plan.json"}},
		{"apply", []string{"apply", "plan.json", "--output", "result.json"}},
		{"conformance", []string{"conformance"}},
		{"explain", []string{"explain", "error-code", "cli.data.io@1"}},
	}
	for _, test := range cases {
		parsed, err := parseTest(t, test.args...)
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if parsed.command != protocol.CliCommand(test.name) {
			t.Fatalf("%s: command = %s", test.name, parsed.command)
		}
	}
}

func TestCommandNamesAreExactAndAbbreviationsRejected(t *testing.T) {
	for _, name := range []string{"ins", "inspectt", "capabil", "q", "conform", "pl", "detect", "version", ""} {
		if _, err := parseTest(t, name); err == nil || err.Code() != "cli.usage.unknown-command@1" {
			t.Fatalf("%q: expected unknown-command usage error, got %v", name, err)
		}
	}
}

func TestGlobalFlagsParseBeforeAndAfterCommand(t *testing.T) {
	parsed, err := parseTest(t, "--json", "conformance", "--pretty")
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.json || !parsed.pretty || parsed.command != protocol.CommandConformance {
		t.Fatalf("parsed = %+v", parsed)
	}
	parsed, err = parseTest(t, "--help")
	if err != nil || !parsed.help {
		t.Fatalf("--help: %v", err)
	}
	parsed, err = parseTest(t, "inspect", "app.conf", "--json")
	if err != nil || !parsed.json || len(parsed.positionals) != 1 {
		t.Fatalf("inspect --json: %v %+v", err, parsed)
	}
}

func TestHelpAndVersionSkipSemanticValidationButNotScanErrors(t *testing.T) {
	parsed, err := parseTest(t, "inspect", "--help")
	if err != nil || !parsed.help {
		t.Fatalf("inspect --help: %v", err)
	}
	parsed, err = parseTest(t, "--help", "--pretty")
	if err != nil || !parsed.help || !parsed.pretty {
		t.Fatalf("--help --pretty: %v", err)
	}
	if _, err := parseTest(t, "frobnicate", "--help"); err == nil ||
		err.Code() != "cli.usage.unknown-command@1" {
		t.Fatalf("frobnicate --help must still fire the scan error: %v", err)
	}
}

func TestMissingCommandIsUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"--json"}, {"--profile", "ini.portable"}} {
		if _, err := parseTest(t, args...); err == nil ||
			err.Code() != "cli.usage.missing-required@1" {
			t.Fatalf("%v: expected missing-required, got %v", args, err)
		}
	}
}

func TestUnknownArgumentsAndSingleDashTokensRejected(t *testing.T) {
	for _, args := range [][]string{{"--bogus"}, {"conformance", "--bogus"}, {"-x"}} {
		if _, err := parseTest(t, args...); err == nil ||
			err.Code() != "cli.usage.unknown-argument@1" {
			t.Fatalf("%v: expected unknown-argument, got %v", args, err)
		}
	}
}

func TestCommandSpecificFlagsScoped(t *testing.T) {
	parsed, err := parseTest(t, "edit", "x.conf", "--profile", "ini.portable", "--write")
	if err != nil || !parsed.write {
		t.Fatalf("edit --write: %v", err)
	}
	parsed, err = parseTest(t, "plan", "a.conf", "--profile", "ini.portable", "--request-file", "r")
	if err != nil || parsed.requestFile == nil {
		t.Fatalf("plan --request-file: %v", err)
	}
	if _, err := parseTest(t, "inspect", "x.conf", "--request-file", "r"); err == nil ||
		err.Code() != "cli.usage.unknown-argument@1" {
		t.Fatalf("inspect --request-file must be rejected: %v", err)
	}
	if _, err := parseTest(t, "query", "--write", "--profile", "x"); err == nil ||
		err.Code() != "cli.usage.unknown-argument@1" {
		t.Fatalf("query --write must be rejected: %v", err)
	}
	if _, err := parseTest(t, "--request-file", "r", "query", "--profile", "x"); err == nil ||
		err.Code() != "cli.usage.unknown-argument@1" {
		t.Fatalf("pre-command --request-file must be rejected: %v", err)
	}
}

func TestMissingAndEmptyFlagValuesAreUsage(t *testing.T) {
	for _, args := range [][]string{{"--profile"}, {"conformance", "--output"}, {"--max-bytes"}} {
		if _, err := parseTest(t, args...); err == nil ||
			err.Code() != "cli.usage.invalid-argument@1" {
			t.Fatalf("%v: expected invalid-argument, got %v", args, err)
		}
	}
	if _, err := parseTest(t, "--profile", "--json", "conformance"); err == nil ||
		err.Code() != "cli.usage.invalid-argument@1" {
		t.Fatalf("dash-prefixed value must be a missing value: %v", err)
	}
	if _, err := parseTest(t, "--profile", ""); err == nil ||
		err.Code() != "cli.usage.invalid-format@1" {
		t.Fatalf("empty --profile must be invalid-format: %v", err)
	}
}

func TestEqualsFormCarriesDashPrefixedValues(t *testing.T) {
	parsed, err := parseTest(t, "conformance", "--output=-weird.json")
	if err != nil || parsed.output == nil || *parsed.output != "-weird.json" {
		t.Fatalf("--output=-weird.json: %v", err)
	}
	if _, err := parseTest(t, "conformance", "--output", "-weird.json"); err == nil {
		t.Fatal("dash-prefixed value without = must be rejected")
	}
	if _, err := parseTest(t, "--json=true", "conformance"); err == nil {
		t.Fatal("boolean flag with inline value must be rejected")
	}
}

func TestNumericFlagValuesValidated(t *testing.T) {
	if _, err := parseTest(t, "--max-bytes", "abc", "conformance"); err == nil {
		t.Fatal("non-numeric --max-bytes must be rejected")
	}
	parsed, err := parseTest(t, "--max-bytes", "0", "conformance")
	if err != nil || parsed.maxBytes == nil || *parsed.maxBytes != 0 {
		t.Fatalf("--max-bytes 0: %v", err)
	}
}

func TestDuplicateFlagsRejected(t *testing.T) {
	if _, err := parseTest(t, "--json", "--json", "conformance"); err == nil ||
		err.Code() != "cli.usage.invalid-argument@1" {
		t.Fatalf("duplicate --json: %v", err)
	}
}

func TestProfileAndFormatAliasConflictsRejected(t *testing.T) {
	if _, err := parseTest(t, "--profile", "a", "--format", "b", "inspect", "x"); err == nil ||
		err.Code() != "cli.usage.invalid-argument@1" {
		t.Fatalf("conflicting profile/format: %v", err)
	}
	parsed, err := parseTest(t, "--profile", "a", "--format", "a", "inspect", "x")
	if err != nil || parsed.profile == nil || *parsed.profile != "a" {
		t.Fatalf("same profile/format: %v", err)
	}
}

func TestPrettyRequiresJSON(t *testing.T) {
	if _, err := parseTest(t, "conformance", "--pretty"); err == nil ||
		err.Code() != "cli.usage.invalid-argument@1" {
		t.Fatalf("--pretty without --json: %v", err)
	}
}

func TestPositionalBoundsEnforced(t *testing.T) {
	if _, err := parseTest(t, "inspect"); err == nil || err.Code() != "cli.usage.missing-required@1" {
		t.Fatalf("inspect without path: %v", err)
	}
	if _, err := parseTest(t, "inspect", "a", "b"); err == nil ||
		err.Code() != "cli.usage.invalid-argument@1" {
		t.Fatalf("inspect with two paths: %v", err)
	}
	if _, err := parseTest(t, "capabilities", "x"); err == nil {
		t.Fatal("capabilities with positional must be rejected")
	}
	if _, err := parseTest(t, "plan"); err == nil || err.Code() != "cli.usage.missing-required@1" {
		t.Fatalf("plan without files: %v", err)
	}
	if _, err := parseTest(t, "apply", "a", "b"); err == nil {
		t.Fatal("apply with two positionals must be rejected")
	}
	if _, err := parseTest(t, "query", "x.json", "--profile", "p"); err == nil {
		t.Fatal("query with positional must be rejected")
	}
}

func TestParseClassCommandsDemandExplicitProfile(t *testing.T) {
	for _, args := range [][]string{
		{"query", "--request-file", "r.json"},
		{"project"},
		{"materialize"},
		{"convert", "src.json"},
		{"edit", "x.conf"},
		{"plan", "a.conf"},
	} {
		if _, err := parseTest(t, args...); err == nil ||
			err.Code() != "cli.usage.missing-required@1" {
			t.Fatalf("%v: expected missing --profile, got %v", args, err)
		}
	}
	// inspect (facts only) and apply (manifest input) need no profile.
	if _, err := parseTest(t, "inspect", "x.conf"); err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if _, err := parseTest(t, "apply", "plan.json"); err != nil {
		t.Fatalf("apply: %v", err)
	}
}

func TestDoubleDashEndsFlagParsing(t *testing.T) {
	parsed, err := parseTest(t, "inspect", "--", "-weird")
	if err != nil || len(parsed.positionals) != 1 || parsed.positionals[0] != "-weird" {
		t.Fatalf("-- -weird: %v", err)
	}
	parsed, err = parseTest(t, "inspect", "--", "--json")
	if err != nil || parsed.json {
		t.Fatalf("-- --json must be a positional: %v", err)
	}
}

func TestEveryUsageErrorMapsToFrozenCode(t *testing.T) {
	errors := []*ParseError{
		missingCommandError(),
		unknownCommandError("x"),
		unknownArgumentError("--x"),
		flagNotAllowedError("write", protocol.CommandQuery, true),
		missingFlagValueError("profile"),
		emptyFlagValueError("output"),
		invalidFlagValueError("max-bytes", "x"),
		missingRequiredError("--profile"),
		unexpectedArgumentError("x"),
		duplicateFlagError("json"),
		conflictingProfileError(),
		prettyWithoutJSONError(),
		nonUTF8ArgumentError(),
	}
	for _, error := range errors {
		if len(error.Code()) < len("cli.usage.@1") ||
			error.Code()[:len("cli.usage.")] != "cli.usage." {
			t.Fatalf("%v -> code %s is not a cli.usage.* code", error, error.Code())
		}
		if error.Error() == "" {
			t.Fatalf("%v has an empty message", error)
		}
	}
}
