package main

// Deterministic zero-dependency argument parsing for the `consema` binary
// (mirror of the Rust bin's args.rs; implementation plan §5.1 — clap and
// its transitive dependencies are rejected under the dependency policy).
// The surface is the 11 frozen commands of RFC 0015 §6.1 plus a small
// closed flag set. Every rejection is a frozen usage-class failure: unknown
// commands and abbreviations, unknown flags, duplicate flags, missing or
// invalid values, and missing required arguments (RFC 0015 §5.1 usage row).
// There is no prefix guessing, no shell semantics, and no ambiguity:
// identical argument vectors always parse identically.
//
// Flag conventions (deterministic): long flags only; values via
// `--flag value` or `--flag=value`; `--` ends flag parsing and makes every
// following token a positional (the only way to pass dash-prefixed
// positionals); a flag value must be the next token and must not itself
// look like a flag (dash-prefixed values require the `=` form). Non-UTF-8
// arguments are rejected as usage errors (the wire path needs UTF-8
// spellings). Command-specific flags (`--request-file`, `--write`) are only
// valid after their owning command; global flags are valid anywhere.
// `--help` and `--version` skip post-scan semantic validation (positional
// counts, required flags, `--pretty` without `--json`), but scan-time
// errors such as an unknown command still fire.

import (
	"strconv"
	"unicode/utf8"

	"consema.dev/consema/protocol"
)

// ParsedArgs is a fully parsed invocation, still missing nothing the
// dispatcher needs.
type ParsedArgs struct {
	// Command is the recognized RFC 0015 §6.1 command; empty only with
	// --help/--version.
	command protocol.CliCommand
	// Positionals are the positional arguments in command-line order.
	positionals []string
	// JSON emits the machine envelope as one canonical JSON line on stdout.
	json bool
	// Pretty indents the envelope JSON (only valid together with --json).
	pretty bool
	// Profile is the explicit profile selection (--profile / --format).
	profile *string
	// Output is the result or manifest write target (--output).
	output *string
	// RequestFile is the strict request input path (--request-file).
	requestFile *string
	// Help prints the usage text instead of running anything.
	help bool
	// Version prints the product version instead of running anything.
	version bool
	// MaxBytes is the CLI-layer per-file read budget in bytes.
	maxBytes *uint64
	// MaxFiles is the CLI-layer batch file-count budget.
	maxFiles *uint64
	// ShowSecrets disables presentation-layer redaction.
	showSecrets bool
	// RedactKeys carries extra key-name redaction patterns.
	redactKeys *string
	// Write authorizes an edit commit (--write; edit only).
	write bool
}

func emptyParsedArgs() ParsedArgs {
	return ParsedArgs{}
}

// ParseError is one frozen usage-class parse failure (RFC 0015 §5.1 usage
// row).
type ParseError struct {
	// Code is the frozen cli.usage.* code of the failure.
	code string
	// Message is the deterministic human diagnostic.
	message string
}

// Code returns the frozen cli.usage.* code (RFC 0015 §13.1).
func (e *ParseError) Code() string { return e.code }

// Error implements error.
func (e *ParseError) Error() string { return e.message }

func usageError(code, message string) *ParseError {
	return &ParseError{code: code, message: message}
}

// ParseError constructors (frozen codes; args.rs code()).
func missingCommandError() *ParseError {
	return usageError("cli.usage.missing-required@1", "missing command (run 'consema --help')")
}

func unknownCommandError(name string) *ParseError {
	return usageError("cli.usage.unknown-command@1", "unknown command '"+name+"'")
}

func unknownArgumentError(flag string) *ParseError {
	return usageError("cli.usage.unknown-argument@1", "unknown argument '"+flag+"'")
}

func flagNotAllowedError(flag string, command protocol.CliCommand, hasCommand bool) *ParseError {
	if hasCommand {
		return usageError("cli.usage.unknown-argument@1",
			"flag '--"+flag+"' is not allowed for command '"+command.Name()+"'")
	}
	return usageError("cli.usage.unknown-argument@1",
		"flag '--"+flag+"' is not valid before the command")
}

func missingFlagValueError(flag string) *ParseError {
	return usageError("cli.usage.invalid-argument@1", "flag '--"+flag+"' requires a value")
}

func emptyFlagValueError(flag string) *ParseError {
	code := "cli.usage.invalid-argument@1"
	if flag == "profile" || flag == "format" {
		code = "cli.usage.invalid-format@1"
	}
	return usageError(code, "flag '--"+flag+"' received an empty value")
}

func invalidFlagValueError(flag, value string) *ParseError {
	return usageError("cli.usage.invalid-argument@1",
		"flag '--"+flag+"' received invalid value '"+value+"'")
}

func missingRequiredError(what string) *ParseError {
	return usageError("cli.usage.missing-required@1", "missing required argument: "+what)
}

func unexpectedArgumentError(argument string) *ParseError {
	return usageError("cli.usage.invalid-argument@1", "unexpected argument '"+argument+"'")
}

func duplicateFlagError(flag string) *ParseError {
	return usageError("cli.usage.invalid-argument@1", "duplicate flag '--"+flag+"'")
}

func conflictingProfileError() *ParseError {
	return usageError("cli.usage.invalid-argument@1", "conflicting --profile and --format values")
}

func prettyWithoutJSONError() *ParseError {
	return usageError("cli.usage.invalid-argument@1", "flag '--pretty' requires '--json'")
}

func nonUTF8ArgumentError() *ParseError {
	return usageError("cli.usage.invalid-argument@1", "argument is not valid UTF-8")
}

// helpText is the static usage text (the version line is prepended by
// main.go).
const helpText = `Usage:
  consema [global options] <command> [args...]
  consema --help | --version

Commands (RFC 0015 §6.1):
  inspect        file facts (bytes/digest/encoding facts/candidate profiles)
  capabilities   facade capability inventory
  query          native/lossless query (request via --request-file or stdin)
  project        explicit projection request
  materialize    explicit materialization request
  convert        two-phase cross-format conversion
  edit           single-file structural edit (dry-run only)
  plan           batch plan manifest (read-only)
  apply          batch apply from a prior plan manifest; env injection seam
                 CONSEMA_APPLY_INTERRUPT_AFTER / CONSEMA_APPLY_WRITE_FAILURE
                 (documented in RFC 0015 §5.4; testing/CI only)
  conformance    embedded protocol self-check subset
  explain        authoritative contract/error-code/profile explanation

Global options:
  --json              emit the core.cli-output@1 machine envelope on stdout
  --pretty            indent the envelope JSON (requires --json)
  --profile <id>      explicit profile selection (required for parse-class
                      commands); --format is an alias
  --output <path>     result or manifest write target
  --request-file <path>  strict request input (query/project/materialize/
                         convert/edit/plan)
  --max-bytes <n>     CLI-layer per-file read budget in bytes
  --max-files <n>     CLI-layer batch file-count budget
  --redact-keys <glob>  extra redaction key-name patterns
  --show-secrets      reveal secret values (sole presentation opt-out)
  --help              print this help and exit 0
  --version           print the product version and exit 0

Exit codes (RFC 0015 §5.1): 0 success, 1 usage, 2 data, 3 limit,
4 precondition, 5 internal; 6-255 reserved.
`

// parseArgs parses one argument vector into a validated invocation.
func parseArgs(args []string) (*ParsedArgs, *ParseError) {
	parsed := emptyParsedArgs()
	seen := make(map[string]bool)
	var command protocol.CliCommand
	hasCommand := false
	endOfFlags := false
	index := 0
	for index < len(args) {
		token := args[index]
		if endOfFlags {
			if err := pushPositional(&command, &hasCommand, &parsed.positionals, token); err != nil {
				return nil, err
			}
		} else if token == "--" {
			endOfFlags = true
		} else if len(token) > 2 && token[0] == '-' && token[1] == '-' {
			body := token[2:]
			name := body
			var inlineValue *string
			for split := 0; split < len(body); split++ {
				if body[split] == '=' {
					name = body[:split]
					value := body[split+1:]
					inlineValue = &value
					break
				}
			}
			if err := parseFlag(name, inlineValue, &parsed, seen, &index, args,
				command, hasCommand); err != nil {
				return nil, err
			}
		} else if len(token) > 1 && token[0] == '-' {
			// Single-dash tokens are never flags; there are no short options.
			return nil, unknownArgumentError(token)
		} else {
			if err := pushPositional(&command, &hasCommand, &parsed.positionals, token); err != nil {
				return nil, err
			}
		}
		index++
	}
	return finish(parsed, command, hasCommand)
}

func pushPositional(command *protocol.CliCommand, hasCommand *bool,
	positionals *[]string, token string) *ParseError {
	if !*hasCommand {
		parsed, ok := protocol.ParseCliCommand(token)
		if !ok {
			return unknownCommandError(token)
		}
		*command = parsed
		*hasCommand = true
	} else {
		*positionals = append(*positionals, token)
	}
	return nil
}

func parseFlag(name string, inlineValue *string, parsed *ParsedArgs,
	seen map[string]bool, index *int, args []string,
	command protocol.CliCommand, hasCommand bool) *ParseError {
	var flag string
	switch name {
	case "help", "version", "json", "pretty", "show-secrets", "write",
		"profile", "format", "output", "request-file", "redact-keys",
		"max-bytes", "max-files":
		flag = name
	default:
		return unknownArgumentError("--" + name)
	}
	if seen[flag] {
		return duplicateFlagError(flag)
	}
	seen[flag] = true
	switch flag {
	case "help":
		parsed.help = true
	case "version":
		parsed.version = true
	case "json":
		if inlineValue != nil {
			return invalidFlagValueError(flag, *inlineValue)
		}
		parsed.json = true
	case "pretty":
		if inlineValue != nil {
			return invalidFlagValueError(flag, *inlineValue)
		}
		parsed.pretty = true
	case "show-secrets":
		if inlineValue != nil {
			return invalidFlagValueError(flag, *inlineValue)
		}
		parsed.showSecrets = true
	case "write":
		if inlineValue != nil {
			return invalidFlagValueError(flag, *inlineValue)
		}
		if err := commandSpecificFlag(flag, command, hasCommand); err != nil {
			return err
		}
		parsed.write = true
	case "profile", "format":
		value, err := takeValue(args, index, flag, inlineValue)
		if err != nil {
			return err
		}
		if *value == "" {
			return emptyFlagValueError(flag)
		}
		switch {
		case parsed.profile == nil:
			parsed.profile = value
		case *parsed.profile == *value:
		default:
			return conflictingProfileError()
		}
	case "output", "request-file", "redact-keys":
		if flag == "request-file" {
			if err := commandSpecificFlag(flag, command, hasCommand); err != nil {
				return err
			}
		}
		value, err := takeValue(args, index, flag, inlineValue)
		if err != nil {
			return err
		}
		if *value == "" {
			return emptyFlagValueError(flag)
		}
		switch flag {
		case "output":
			parsed.output = value
		case "request-file":
			parsed.requestFile = value
		default:
			parsed.redactKeys = value
		}
	case "max-bytes", "max-files":
		value, err := takeValue(args, index, flag, inlineValue)
		if err != nil {
			return err
		}
		budget, parseErr := strconv.ParseUint(*value, 10, 64)
		if parseErr != nil {
			return invalidFlagValueError(flag, *value)
		}
		if flag == "max-bytes" {
			parsed.maxBytes = &budget
		} else {
			parsed.maxFiles = &budget
		}
	}
	return nil
}

// takeValue returns the flag value: the inline `=` form, or the next token
// when it does not look like a flag.
func takeValue(args []string, index *int, flag string,
	inlineValue *string) (*string, *ParseError) {
	if inlineValue != nil {
		return inlineValue, nil
	}
	if *index+1 >= len(args) {
		return nil, missingFlagValueError(flag)
	}
	next := args[*index+1]
	if len(next) > 1 && next[0] == '-' {
		return nil, missingFlagValueError(flag)
	}
	*index++
	return &next, nil
}

// commandSpecificFlag rejects command-specific flags outside their owning
// commands.
func commandSpecificFlag(flag string, command protocol.CliCommand,
	hasCommand bool) *ParseError {
	switch flag {
	case "request-file":
		switch command {
		case protocol.CommandQuery, protocol.CommandProject,
			protocol.CommandMaterialize, protocol.CommandConvert,
			protocol.CommandEdit, protocol.CommandPlan:
			return nil
		}
		return flagNotAllowedError(flag, command, hasCommand)
	case "write":
		if command == protocol.CommandEdit {
			return nil
		}
		return flagNotAllowedError(flag, command, hasCommand)
	}
	return nil
}

func finish(parsed ParsedArgs, command protocol.CliCommand,
	hasCommand bool) (*ParsedArgs, *ParseError) {
	if hasCommand {
		parsed.command = command
	}
	if parsed.help || parsed.version {
		// Help and version answer before semantic validation; scan-time
		// errors (unknown command/flag) already fired above.
		return &parsed, nil
	}
	if !hasCommand {
		return nil, missingCommandError()
	}
	if parsed.pretty && !parsed.json {
		return nil, prettyWithoutJSONError()
	}
	minPositionals, maxPositionals, missingMessage := positionalBounds(command)
	if len(parsed.positionals) < minPositionals {
		return nil, missingRequiredError(missingMessage)
	}
	if maxPositionals >= 0 && len(parsed.positionals) > maxPositionals {
		return nil, unexpectedArgumentError(parsed.positionals[maxPositionals])
	}
	if parseClassCommand(command) && parsed.profile == nil {
		return nil, missingRequiredError("--profile")
	}
	return &parsed, nil
}

// positionalBounds returns (minimum, maximum, message) of each command; a
// negative maximum means unbounded.
func positionalBounds(command protocol.CliCommand) (int, int, string) {
	switch command {
	case protocol.CommandInspect, protocol.CommandEdit:
		return 1, 1, "a file path"
	case protocol.CommandCapabilities, protocol.CommandConformance,
		protocol.CommandQuery, protocol.CommandProject,
		protocol.CommandMaterialize:
		return 0, 0, ""
	case protocol.CommandConvert:
		return 1, 1, "a source file path"
	case protocol.CommandPlan:
		return 1, -1, "at least one file path"
	case protocol.CommandApply:
		return 1, 1, "a plan manifest path"
	case protocol.CommandExplain:
		return 1, 2, "an explainable id (optionally with a kind)"
	}
	return 0, 0, ""
}

// parseClassCommand reports whether the command parses source documents and
// therefore demands an explicit --profile/--format (RFC 0015 §7.2; missing =
// usage, never try-and-see).
func parseClassCommand(command protocol.CliCommand) bool {
	switch command {
	case protocol.CommandQuery, protocol.CommandProject,
		protocol.CommandMaterialize, protocol.CommandConvert,
		protocol.CommandEdit, protocol.CommandPlan:
		return true
	}
	return false
}

// collectArgs validates every process argument as UTF-8 (the Rust bin
// rejects non-UTF-8 arguments at collection; Go's os.Args are byte strings).
func collectArgs(args []string) *ParseError {
	for _, argument := range args {
		if !utf8.ValidString(argument) {
			return nonUTF8ArgumentError()
		}
	}
	return nil
}
