package main

// `consema inspect`: read-only file-facts reporting (RFC 0015 §6.1/§7).
// Inspect reads exactly one file and reports source-level facts (size,
// SHA-256 digest, BOM facts, symlink/junction facts) plus the detection
// facts of detect.go (markers, candidate profiles, ambiguity) — the file is
// **not parsed for content** unless `--profile` is explicit, in which case
// the `parse` field carries `cli.parse-facts@1` (formation status,
// diagnostics, structure counts). Detection facts never produce a
// conclusion (hard gate 2): ambiguity is reported as a first-class success
// result (exit 0; RFC 0015 §7.2 rule 3), while a file that cannot be read
// is a data error (cli.data.io@1, exit 2) and a read that exceeds the CLI
// byte budget is a limit error (cli.limit.file-size@1, exit 3; RFC 0015
// §12). No side effects.

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	consema "consema.dev/consema"
	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// runInspect runs one `consema inspect` invocation and returns the frozen
// exit code.
func runInspect(parsed *ParsedArgs, stdout, stderr io.Writer) uint8 {
	path := parsed.positionals[0]
	budget := uint64(protocol.DefaultProtocolLimits().MaxBytes)
	if parsed.maxBytes != nil {
		budget = *parsed.maxBytes
	}

	// Metadata facts first: the symlink/junction fact is reported even when
	// the read itself fails.
	symlink := isSymlinkPath(path)

	bytes, fullyRead, ioError := readCappedBytes(path, budget)
	if ioError != nil {
		size := uint64(0)
		if info, err := os.Stat(path); err == nil {
			size = uint64(info.Size())
		}
		return emitInspectFailure(parsed, path, symlink, size, nil,
			[]*protocol.Diagnostic{cliDiagnostic("cli.data.io@1", protocol.CategoryEncoding,
				"cannot read '"+path+"': "+stableIOKind(ioError))},
			[]string{fmt.Sprintf("cannot read '%s': %v (code cli.data.io@1)", path, ioError)},
			"cli.data.io@1", stdout, stderr)
	}

	if !fullyRead {
		message := fmt.Sprintf("'%s' exceeds the CLI read budget of %d bytes (RFC 0015 §12); "+
			"raise it with --max-bytes", path, budget)
		return emitInspectFailure(parsed, path, symlink, uint64(len(bytes)), nil,
			[]*protocol.Diagnostic{cliDiagnostic("cli.limit.file-size@1",
				protocol.CategoryResource, message)},
			[]string{message + " (code cli.limit.file-size@1)"}, "cli.limit.file-size@1",
			stdout, stderr)
	}

	facts := detect(bytes, true)
	var parse core.Value
	if parsed.profile != nil {
		outcome := parseFactsValue(*parsed.profile, path, bytes, stderr)
		switch outcome.kind {
		case parseOutcomeFacts:
			parse = outcome.value
		case parseOutcomeFatal:
			return emitInspectFailure(parsed, path, symlink, facts.Size, facts.Digest,
				outcome.diagnostics, outcome.stderrLines, "", stdout, stderr)
		case parseOutcomeUsage:
			return protocol.ClassifyErrorCode("cli.usage.invalid-format@1").ExitCode()
		}
	}

	payload := inspectPayload(path, &facts, symlink, parse)
	envelope, err := protocol.NewCliOutputMessage(protocol.CommandInspect,
		protocol.ExitSuccess, productVersion, payload, nil, noRedaction())
	if err != nil {
		return internalFailure("inspect", "inspect envelope: "+err.Error(), stderr)
	}
	var writeErr error
	if parsed.json {
		writeErr = emitEnvelope(envelope, parsed.pretty, stdout)
	} else {
		writeErr = writeInspectReport(path, &facts, symlink, parse, stdout)
	}
	if writeErr != nil {
		return internalFailure("inspect", writeErr.Error(), stderr)
	}
	return protocol.ExitSuccess.ExitCode()
}

// readCappedBytes reads at most budget bytes; fullyRead is false when the
// file exceeds the budget (the buffer holds exactly budget bytes then).
func readCappedBytes(path string, budget uint64) ([]byte, bool, error) {
	bytes, err := readCapped(path, budget)
	if err == errOverLimit {
		return bytes, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return bytes, true, nil
}

// stableIOKind is the stable error-kind spelling for the envelope message
// (RFC 0015 §3.3: the envelope message carries only the stable kind; the
// locale-dependent OS error text is the human stderr line).
func stableIOKind(err error) string {
	if os.IsNotExist(err) {
		return "NotFound"
	}
	if os.IsPermission(err) {
		return "PermissionDenied"
	}
	return "Other"
}

// inspectFailureOutcome shapes the parse-facts failure outcome.
type inspectParseOutcome struct {
	kind        int
	value       core.Value
	diagnostics []*protocol.Diagnostic
	stderrLines []string
}

const (
	parseOutcomeFacts = iota
	parseOutcomeFatal
	parseOutcomeUsage
)

// parseFactsValue parses the file under the explicit --profile and
// assembles the `cli.parse-facts@1` record (RFC 0015 §7.1).
func parseFactsValue(profileID, path string, bytes []byte,
	stderr io.Writer) inspectParseOutcome {
	entry := profileByID(profileID)
	if entry == nil {
		fmt.Fprintf(stderr,
			"consema: error: invalid --profile value '%s': not a facade profile "+
				"(code cli.usage.invalid-format@1)\n", profileID)
		return inspectParseOutcome{kind: parseOutcomeUsage}
	}
	doc, failure := consema.ParseDocument(context.Background(), bytes, entry.Profile)
	if failure != nil {
		diagnostics := make([]*protocol.Diagnostic, 0)
		for _, diagnostic := range diagnosticsOf(failure) {
			diagnostics = append(diagnostics, bindParseDiagnostic(diagnostic, path))
		}
		if len(diagnostics) == 0 {
			trueCode := failureCodeOf(failure)
			message := "format-local code " + trueCode
			diagnostic, _ := protocol.NewDiagnostic(registeredCode(trueCode),
				registryCategory(registeredCode(trueCode)), protocol.SeverityError, nil, nil,
				map[string]string{"message": message}, nil, nil, 0,
				protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
			diagnostics = append(diagnostics, bindParseDiagnostic(diagnostic, path))
		}
		stderrLines := make([]string, 0, len(diagnostics))
		for _, diagnostic := range diagnostics {
			stderrLines = append(stderrLines,
				fmt.Sprintf("%s (code %s)", diagnosticMessage(diagnostic), diagnostic.Code))
		}
		return inspectParseOutcome{kind: parseOutcomeFatal,
			diagnostics: diagnostics, stderrLines: stderrLines}
	}

	formation := "Complete"
	if doc.FormationStatus() == document.FormationStatusRecovered {
		formation = "Recovered"
	}
	diagnosticValues := make([]core.Value, 0)
	for _, diagnostic := range doc.Diagnostics() {
		bound := bindParseDiagnostic(diagnostic, path)
		value, _ := bound.ToValue()
		diagnosticValues = append(diagnosticValues, value)
	}
	counts := structureCounts(doc)
	countEntries := make([]core.Entry, 0, len(counts))
	for _, key := range sortedKeys(counts) {
		countEntries = append(countEntries,
			core.Entry{Key: key, Value: integerValueOf(counts[key])})
	}
	structure, _ := core.NewObject(countEntries...)
	payload, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("cli.parse-facts@1")},
		core.Entry{Key: "profile", Value: referenceValue(entry.Profile.ID(), entry.Profile.Version())},
		core.Entry{Key: "formation_status", Value: core.String(formation)},
		core.Entry{Key: "diagnostics", Value: core.NewArray(diagnosticValues...)},
		core.Entry{Key: "structure_counts", Value: structure},
	)
	return inspectParseOutcome{kind: parseOutcomeFacts, value: payload}
}

func failureCodeOf(failure error) string {
	if coded, ok := failure.(interface{ Code() string }); ok {
		return coded.Code()
	}
	return "core.source.invalid-utf8@1"
}

// structureCounts derives the format-owned stable structure-count keys (RFC
// 0015 §7.1 `structure_counts`) from the facade typed adapters only.
func structureCounts(document *consema.Document) map[string]uint64 {
	counts := map[string]uint64{}
	if iniDocument, ok := document.AsINI(); ok {
		counts["ini.sections"] = uint64(len(iniDocument.Sections()))
		counts["ini.entries"] = uint64(len(iniDocument.Entries()))
		counts["ini.error_lines"] = uint64(len(iniDocument.ErrorLines()))
	} else if propertiesDocument, ok := document.AsProperties(); ok {
		counts["java-properties.entries"] = uint64(len(propertiesDocument.Properties()))
	} else if jsonDocument, ok := document.AsJSON(); ok {
		root := jsonDocument.Root()
		members := root.ObjectMembers()
		if members.IsAvailable() {
			counts["json.object_members"] = uint64(len(members.Value()))
		} else {
			elements := root.ArrayElements()
			if elements.IsAvailable() {
				counts["json.array_elements"] = uint64(len(elements.Value()))
			} else if kind := root.Kind(); kind.IsAvailable() {
				// A semantically available root that is neither an object
				// nor an array is a scalar root (RFC 0015 §7.1
				// structure_counts; the Rust Available(None) case).
				counts["json.scalar_root"] = 1
			}
		}
	} else if tomlDocument, ok := document.AsTOML(); ok {
		if entries, isTable := tomlDocument.Root().TableEntries(); isTable {
			counts["toml.entries"] = uint64(len(entries))
		}
	} else if yamlDocument, ok := document.AsYAML(); ok {
		counts["yaml.documents"] = uint64(yamlDocument.DocumentCount())
	} else if xmlDocument, ok := document.AsXML(); ok {
		counts["xml.nodes"] = uint64(len(xmlDocument.Nodes()))
	} else if plistDocument, ok := document.AsPlist(); ok {
		if native := plistDocument.NativeDocument(); native != nil {
			counts["plist.nodes"] = uint64(native.NodeCount())
		}
	} else if hclDocument, ok := document.AsHCL(); ok {
		counts["hcl.body_items"] = uint64(len(hclDocument.Document().Body().Items()))
	}
	return counts
}

// inspectPayload builds the frozen `cli.inspect@1` payload record (RFC 0015
// §7.1).
func inspectPayload(path string, facts *DetectFacts, symlink bool,
	parse core.Value) core.Value {
	var digestValue core.Value = core.NullValue()
	if facts.Digest != nil {
		digestValue = digestRecord(*facts.Digest)
	}
	var bom core.Value = core.NullValue()
	if facts.BOM != "" {
		bom = core.String(facts.BOM)
	}
	markerValues := make([]core.Value, 0, len(facts.Markers))
	for _, marker := range facts.Markers {
		markerValues = append(markerValues, core.String(marker))
	}
	candidateValues := make([]core.Value, 0, len(facts.Candidates))
	for _, candidate := range facts.Candidates {
		entry, _ := core.NewObject(
			core.Entry{Key: "profile", Value: referenceValue(
				candidate.Profile.ID(), candidate.Profile.Version())},
			core.Entry{Key: "reason", Value: core.String(candidate.Reason)},
		)
		candidateValues = append(candidateValues, entry)
	}
	ambiguityValues := make([]core.Value, 0, len(facts.AmbiguityReasons))
	for _, reason := range facts.AmbiguityReasons {
		ambiguityValues = append(ambiguityValues, core.String(reason))
	}
	parseValue := core.NullValue()
	if parse != nil {
		parseValue = parse
	}
	bytesObject, _ := core.NewObject(
		core.Entry{Key: "size", Value: integerValueOf(facts.Size)},
		core.Entry{Key: "digest", Value: digestValue},
	)
	payload, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("cli.inspect@1")},
		core.Entry{Key: "path", Value: core.String(path)},
		core.Entry{Key: "bytes", Value: bytesObject},
		core.Entry{Key: "bom", Value: bom},
		core.Entry{Key: "symlink", Value: core.Boolean(symlink)},
		core.Entry{Key: "markers", Value: core.NewArray(markerValues...)},
		core.Entry{Key: "candidates", Value: core.NewArray(candidateValues...)},
		core.Entry{Key: "ambiguous", Value: core.Boolean(facts.Ambiguous)},
		core.Entry{Key: "ambiguity_reasons", Value: core.NewArray(ambiguityValues...)},
		core.Entry{Key: "parse", Value: parseValue},
	)
	return payload
}

// digestRecord builds the {algorithm, hex} digest record.
func digestRecord(digest protocol.ContentDigest) core.Value {
	value, _ := core.NewObject(
		core.Entry{Key: "algorithm", Value: core.String(digest.Algorithm())},
		core.Entry{Key: "hex", Value: core.String(digest.Hex())},
	)
	return value
}

// cliDiagnostic builds one frozen cli.* diagnostic for a failure envelope.
func cliDiagnostic(code string, category protocol.DiagnosticCategory,
	message string) *protocol.Diagnostic {
	diagnostic, _ := protocol.NewDiagnostic(code, category, protocol.SeverityError,
		nil, nil, map[string]string{"message": message}, nil, nil, 0,
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	return diagnostic
}

// diagnosticMessage returns the deterministic stderr message of a failure
// diagnostic.
func diagnosticMessage(diagnostic *protocol.Diagnostic) string {
	if message, ok := diagnostic.Arguments["message"]; ok && message != "" {
		return message
	}
	return "see the envelope diagnostics"
}

// emitInspectFailure emits one data/limit failure envelope for inspect (RFC
// 0015 §4.2: data-class failures carry an envelope; the payload keeps the
// facts that exist). The envelope is written only under --json; in human
// mode the failure writes zero stdout bytes and the diagnostics below are
// the failure surface (RFC 0015 §3.3). codeHint is the failure code when
// the diagnostics may be empty.
func emitInspectFailure(parsed *ParsedArgs, path string, symlink bool,
	size uint64, digest *protocol.ContentDigest,
	diagnostics []*protocol.Diagnostic, stderrLines []string, codeHint string,
	stdout, stderr io.Writer) uint8 {
	facts := DetectFacts{Size: size, Digest: digest}
	payload := inspectPayload(path, &facts, symlink, nil)
	exitClass := protocol.ExitData
	if len(diagnostics) > 0 {
		exitClass = protocol.ClassifyErrorCode(diagnostics[0].Code)
	} else if codeHint != "" {
		exitClass = protocol.ClassifyErrorCode(codeHint)
	}
	envelope, err := protocol.NewCliOutputMessage(protocol.CommandInspect,
		exitClass, productVersion, payload, diagnostics, noRedaction())
	if err != nil {
		return internalFailure("inspect", "inspect failure envelope: "+err.Error(), stderr)
	}
	var writeErr error
	if parsed.json {
		writeErr = emitEnvelope(envelope, parsed.pretty, stdout)
	}
	if writeErr != nil {
		return internalFailure("inspect", writeErr.Error(), stderr)
	}
	for _, line := range stderrLines {
		fmt.Fprintf(stderr, "consema: error: inspect: %s\n", line)
	}
	return exitClass.ExitCode()
}

// writeInspectReport writes the deterministic human inspect report; it
// draws from the same facade facts as the machine payload.
func writeInspectReport(path string, facts *DetectFacts, symlink bool,
	parse core.Value, stdout io.Writer) error {
	var report strings.Builder
	fmt.Fprintf(&report, "consema inspect %s\n", path)
	if facts.Digest != nil {
		fmt.Fprintf(&report, "  bytes: %d bytes sha256:%s\n", facts.Size, facts.Digest.Hex())
	} else {
		fmt.Fprintf(&report, "  bytes: %d bytes digest: unavailable\n", facts.Size)
	}
	bom := "none"
	switch facts.BOM {
	case "Utf8":
		bom = "utf-8"
	case "Utf16Le":
		bom = "utf-16-le"
	case "Utf16Be":
		bom = "utf-16-be"
	}
	fmt.Fprintf(&report, "  bom: %s\n", bom)
	symlinkText := "no"
	if symlink {
		symlinkText = "yes"
	}
	fmt.Fprintf(&report, "  symlink: %s\n", symlinkText)
	markers := "none"
	if len(facts.Markers) > 0 {
		markers = strings.Join(facts.Markers, ", ")
	}
	fmt.Fprintf(&report, "  markers: %s\n", markers)
	candidates := "none"
	if len(facts.Candidates) > 0 {
		parts := make([]string, 0, len(facts.Candidates))
		for _, candidate := range facts.Candidates {
			parts = append(parts, fmt.Sprintf("%s@%d (%s)",
				candidate.Profile.ID(), candidate.Profile.Version(), candidate.Reason))
		}
		candidates = strings.Join(parts, "; ")
	}
	fmt.Fprintf(&report, "  candidates: %s\n", candidates)
	ambiguous := "no"
	if facts.Ambiguous {
		ambiguous = "yes: " + strings.Join(facts.AmbiguityReasons, "; ")
	}
	fmt.Fprintf(&report, "  ambiguous: %s\n", ambiguous)
	if parse != nil {
		writeHumanParse(&report, parse)
	}
	_, err := io.WriteString(stdout, report.String())
	return err
}

// writeHumanParse appends the human parse-facts view (derived from the same
// record as the machine view).
func writeHumanParse(report *strings.Builder, parse core.Value) {
	object, ok := parse.(*core.Object)
	if !ok {
		return
	}
	var profile, formation string
	var diagnostics []string
	var counts []string
	for _, entry := range object.Entries() {
		switch entry.Key {
		case "profile":
			if profileObject, ok := entry.Value.(*core.Object); ok {
				profileEntries := profileObject.Entries()
				if len(profileEntries) >= 2 {
					id, idOK := profileEntries[0].Value.(core.String)
					version, versionOK := profileEntries[1].Value.(core.Integer)
					if idOK && versionOK {
						profile = fmt.Sprintf("%s@%s", string(id), version.String())
					}
				}
			}
		case "formation_status":
			if text, ok := entry.Value.(core.String); ok {
				formation = string(text)
			}
		case "diagnostics":
			if array, ok := entry.Value.(*core.Array); ok {
				for _, item := range array.Items() {
					code := ""
					message := ""
					if diagnosticObject, ok := item.(*core.Object); ok {
						for _, field := range diagnosticObject.Entries() {
							switch field.Key {
							case "code":
								if text, ok := field.Value.(core.String); ok {
									code = string(text)
								}
							case "arguments":
								if arguments, ok := field.Value.(*core.Object); ok {
									for _, argument := range arguments.Entries() {
										if argument.Key == "message" {
											if text, ok := argument.Value.(core.String); ok {
												message = string(text)
											}
										}
									}
								}
							}
						}
					}
					if code != "" {
						if message != "" {
							diagnostics = append(diagnostics, code+" ("+message+")")
						} else {
							diagnostics = append(diagnostics, code)
						}
					}
				}
			}
		case "structure_counts":
			if countsObject, ok := entry.Value.(*core.Object); ok {
				for _, field := range countsObject.Entries() {
					if integer, ok := field.Value.(core.Integer); ok {
						counts = append(counts, fmt.Sprintf("%s: %s", field.Key, integer.String()))
					}
				}
			}
		}
	}
	if profile != "" && formation != "" {
		fmt.Fprintf(report, "  parse (%s): %s\n", profile, formation)
	}
	if len(diagnostics) == 0 {
		report.WriteString("    diagnostics: none\n")
	} else {
		fmt.Fprintf(report, "    diagnostics: %d: %s\n", len(diagnostics),
			strings.Join(diagnostics, ", "))
	}
	if len(counts) == 0 {
		report.WriteString("    structure counts: none\n")
	} else {
		fmt.Fprintf(report, "    structure counts: %s\n", strings.Join(counts, ", "))
	}
}

// sortedKeys returns the sorted map keys (deterministic human output).
func sortedKeys(values map[string]uint64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys
}

func sortStrings(keys []string) {
	for index := 1; index < len(keys); index++ {
		for cursor := index; cursor > 0 && keys[cursor] < keys[cursor-1]; cursor-- {
			keys[cursor], keys[cursor-1] = keys[cursor-1], keys[cursor]
		}
	}
}
