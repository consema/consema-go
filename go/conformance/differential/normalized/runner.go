// Package normalized implements the Go side of the cross-language
// normalized-result differential harness (milestone 0.15.0 G1.5;
// docs/go-implementation-plan.md §4.4 and §2.2; roadmap §16.2 line 1488 and
// §11.2 lines 849-861).
//
// The harness compares the language-neutral normalized results of the same
// data-driven input set (`cases.json`, this directory) executed by the Rust
// SDK (consema-rs/crates/consema-conformance/examples/emit_normalized_results.rs) and
// by this package. Go never imports or calls Rust (RFC 0016 §1.1 cgo ban):
// the Rust side emits one `<case-id>.txt` evidence file per case, and the
// Go test computes the same normalized facts and compares them field by
// field. Since milestone 0.19.0 G5.2 the comparison is bidirectional
// (roadmap §16.6 line 1548; docs/go-implementation-plan.md §2.6): the Go
// side also emits its evidence files for the same input set, and the Rust
// example's consume mode (--consume) reads them and compares them with its
// own results field by field. Orchestration:
// scripts/go-verify-normalized-differential.ps1.
//
// The compared facts are exactly the language-neutral behavior surface of
// roadmap §11.2: parse formation, diagnostic code/order (never text), query
// count/identity/order, projection/materialization reports, edit result
// bytes or failure codes, and resource-limit completion semantics. The
// output vocabulary is defined here and mirrored verbatim by the Rust
// example; it contains no Rust internal type names. Error texts never
// participate in the comparison.
package normalized

import (
	"context"
	"encoding/hex"
	stdjson "encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/ini"
	"consema.dev/consema/json"
	"consema.dev/consema/properties"
	"consema.dev/consema/protocol"
	"consema.dev/consema/toml"
	"consema.dev/consema/yaml"
)

// CaseFileManifest is the frozen manifest id of the differential input set.
const CaseFileManifest = "consema.differential.normalized@1"

// RustDirEnv names the directory of the Rust evidence files.
const RustDirEnv = "CONSEMA_DIFFERENTIAL_NORMALIZED_RUST_DIR"

// GoDirEnv names the directory where the Go emitter writes its own
// evidence files (the reverse direction of the bidirectional differential,
// milestone 0.19.0 G5.2; consumed by the Rust example's --consume mode).
const GoDirEnv = "CONSEMA_DIFFERENTIAL_NORMALIZED_GO_DIR"

// ---------------------------------------------------------------------------
// Case file schema (data-driven; shared with the Rust example)
// ---------------------------------------------------------------------------

// fileCase is one entry of cases.json.
type fileCase struct {
	ID               string               `json:"id"`
	Kind             string               `json:"kind"` // "document" or "source"
	Format           string               `json:"format,omitempty"`
	Profile          string               `json:"profile,omitempty"`
	Source           string               `json:"source,omitempty"`
	ForeignSource    string               `json:"foreign_source,omitempty"`
	ForeignSourceHex string               `json:"foreign_source_hex,omitempty"`
	ParseLimits      *parseLimitsDesc     `json:"parse_limits,omitempty"`
	Steps            []stepDesc           `json:"steps,omitempty"`
	Input            *sourceInputDesc     `json:"input,omitempty"`
	Request          *encodingRequestDesc `json:"request,omitempty"`
	Positions        []int                `json:"positions,omitempty"`
	Patch            *patchDesc           `json:"patch,omitempty"`
}

// parseLimitsDesc overrides the frozen parse limits.
type parseLimitsDesc struct {
	MaxSourceBytes  *int `json:"max_source_bytes,omitempty"`
	MaxNestingDepth *int `json:"max_nesting_depth,omitempty"`
	MaxTokenCount   *int `json:"max_token_count,omitempty"`
	MaxNodeCount    *int `json:"max_node_count,omitempty"`
	MaxDiagnostics  *int `json:"max_diagnostics,omitempty"`
}

// stepDesc is one document-face step.
type stepDesc struct {
	Op string `json:"op"`

	// query-native / query-syntax
	Domain        string       `json:"domain,omitempty"`
	DomainVersion int          `json:"domain_version,omitempty"`
	Filters       []filterDesc `json:"filters,omitempty"`
	Combine       string       `json:"combine,omitempty"`
	Selection     string       `json:"selection,omitempty"`
	QueryLimits   *queryLimits `json:"query_limits,omitempty"`

	// project
	Target          string `json:"target,omitempty"`
	DuplicatePolicy string `json:"duplicate_policy,omitempty"`

	// materialize
	Input         string             `json:"input,omitempty"`
	ValueJSON     string             `json:"value_json,omitempty"`
	EntryMapping  *entryMappingDesc  `json:"entry_mapping,omitempty"`
	TargetProfile string             `json:"target_profile,omitempty"`
	Style         string             `json:"style,omitempty"`
	Newline       string             `json:"newline,omitempty"`
	MatLimits     *materializeLimits `json:"limits,omitempty"`

	// edit
	Operations []editOpDesc `json:"operations,omitempty"`
}

// filterDesc is one query filter: an operator call on the current input.
type filterDesc struct {
	Operator string             `json:"operator"`
	Argument stdjson.RawMessage `json:"argument,omitempty"`
}

// queryLimits overrides the frozen query limits.
type queryLimits struct {
	MaxResults *int `json:"max_results,omitempty"`
	MaxSteps   *int `json:"max_steps,omitempty"`
}

// entryMappingDesc is a direct EntryMapping input for materialization; the
// key and value are canonical transport JSON envelopes (RFC 0015 §3.2).
type entryMappingDesc struct {
	KeyJSON   string `json:"key_json"`
	ValueJSON string `json:"value_json"`
}

// materializeLimits overrides the frozen materialization limits.
type materializeLimits struct {
	MaxOutputBytes       *int `json:"max_output_bytes,omitempty"`
	MaxInputNodes        *int `json:"max_input_nodes,omitempty"`
	MaxDepth             *int `json:"max_depth,omitempty"`
	MaxProvenanceEntries *int `json:"max_provenance_entries,omitempty"`
}

// editOpDesc is one edit operation of one transaction.
type editOpDesc struct {
	Operation  string         `json:"operation"`
	Target     *targetDesc    `json:"target"`
	Value      *valueDesc     `json:"value,omitempty"`
	LiteralHex string         `json:"literal_hex,omitempty"`
	Name       string         `json:"name,omitempty"`
	Policy     string         `json:"policy,omitempty"`
	Placement  *placementDesc `json:"placement,omitempty"`
}

// targetDesc identifies one structural node of the current document.
type targetDesc struct {
	// Kind: root | member | member-value | member-key | entry | entry-item |
	// entry-key | array-element | array-element-value | array-element-item.
	Kind    string `json:"kind"`
	Ordinal int    `json:"ordinal,omitempty"`
	Foreign bool   `json:"foreign,omitempty"`
}

// valueDesc is a scalar PortableValue descriptor for edit values.
type valueDesc struct {
	Null     *bool  `json:"null,omitempty"`
	Boolean  *bool  `json:"boolean,omitempty"`
	Integer  string `json:"integer,omitempty"`
	Decimal  string `json:"decimal,omitempty"`
	String   string `json:"string,omitempty"`
	Binary64 string `json:"binary64,omitempty"`
}

// placementDesc is one association placement.
type placementDesc struct {
	At            string `json:"at,omitempty"` // "start" | "end"
	BeforeOrdinal *int   `json:"before_ordinal,omitempty"`
	AfterOrdinal  *int   `json:"after_ordinal,omitempty"`
}

// sourceInputDesc is the raw input of a source-face case.
type sourceInputDesc struct {
	RawHex string `json:"raw_hex,omitempty"`
	Source string `json:"source,omitempty"`
}

// encodingRequestDesc is the source encoding-resolution request.
type encodingRequestDesc struct {
	ProfileDefault string `json:"profile_default,omitempty"`
	Declaration    string `json:"declaration,omitempty"`
	CallerOverride string `json:"caller_override,omitempty"`
	BomPolicy      string `json:"bom_policy,omitempty"`
}

// patchDesc is one optional SourcePatch application.
type patchDesc struct {
	Replacements []patchReplacementDesc `json:"replacements"`
	ApplyTo      string                 `json:"apply_to"` // "base" | "tampered"
}

// patchReplacementDesc is one raw-byte replacement.
type patchReplacementDesc struct {
	OldStart       int    `json:"old_start"`
	OldEnd         int    `json:"old_end"`
	ReplacementHex string `json:"replacement_hex"`
}

// ---------------------------------------------------------------------------
// Normalized fact emission
// ---------------------------------------------------------------------------

// facts is the ordered key=value fact set of one case. The key set is
// fixed: every document case emits exactly the same keys in the same order,
// so a missing or extra key is itself a differential failure.
type facts struct {
	lines []string
}

func (f *facts) set(key, value string) {
	f.lines = append(f.lines, key+"="+value)
}

// escape renders one text value for the evidence file: JSON string escaping
// (short escapes for the JSON whitespace set, \u00xx lowercase hex for the
// other control characters, everything else passed through as UTF-8). The
// Rust example implements the identical function.
func escape(text string) string {
	var output strings.Builder
	for index := 0; index < len(text); {
		character, size := utf8.DecodeRuneInString(text[index:])
		if character != utf8.RuneError || text[index:index+size] == "�" {
			switch character {
			case '"':
				output.WriteString(`\"`)
			case '\\':
				output.WriteString(`\\`)
			case '\b':
				output.WriteString(`\b`)
			case '\f':
				output.WriteString(`\f`)
			case '\n':
				output.WriteString(`\n`)
			case '\r':
				output.WriteString(`\r`)
			case '\t':
				output.WriteString(`\t`)
			default:
				if character < 0x20 {
					output.WriteString(fmt.Sprintf(`\u%04x`, character))
				} else {
					output.WriteRune(character)
				}
			}
			index += size
			continue
		}
		// One invalid UTF-8 sequence: emit a single U+FFFD for the longest
		// invalid run (the standard lossy semantics, mirrored byte for byte by
		// Rust's from_utf8_lossy). DecodeRuneInString already groups truncated
		// sequences (size 2/3 at the end of the string); an invalid starter
		// byte's following continuation bytes cannot begin a new sequence, so
		// they belong to the same run, unless the full expected width is
		// present, in which case the sequence is complete-but-invalid and each
		// byte is its own error (both sides emit per-byte replacements).
		output.WriteString("�")
		index += size
		if size == 1 {
			starter := text[index-1]
			if 0xC2 <= starter && starter <= 0xF4 {
				width := 2
				if starter >= 0xE0 {
					width = 3
				}
				if starter >= 0xF0 {
					width = 4
				}
				continuations := 0
				for index+continuations < len(text) &&
					0x80 <= text[index+continuations] && text[index+continuations] <= 0xBF {
					continuations++
				}
				if continuations < width-1 {
					index += continuations
				}
			}
		}
	}
	return output.String()
}

// join renders one ordered list into the `|`-joined fact vocabulary.
func join(items []string) string { return strings.Join(items, "|") }

// ---------------------------------------------------------------------------
// Document face
// ---------------------------------------------------------------------------

// docState is the execution state of one document case.
type docState struct {
	doc           *json.Document
	tomlDoc       *toml.Document
	yamlDoc       *yaml.Document
	iniDoc        *ini.Document
	propertiesDoc *properties.Document

	foreign           *json.Document
	foreignToml       *toml.Document
	foreignYaml       *yaml.Document
	foreignIni        *ini.Document
	foreignProperties *properties.Document
	iniLimits         ini.IniParseLimits
	propertiesLimits  properties.PropertiesParseLimits

	format           string
	profile          interface{}
	foreignSource    string
	foreignSourceHex string
	parseLimits      document.ParseLimits

	// parse facts
	fatalCode       string
	formation       string
	diagnosticCodes string
	rootKind        string
	native          string

	// step run flags (each key set is emitted exactly once)
	queryNativeRun bool
	querySyntaxRun bool
	projectRun     bool
	materializeRun bool
	editRun        bool

	// projection result
	value     core.Value
	projected bool
}

// documentParsed reports whether the parse step succeeded.
func (s *docState) documentParsed() bool {
	return s.doc != nil || s.tomlDoc != nil || s.yamlDoc != nil ||
		s.iniDoc != nil || s.propertiesDoc != nil
}

// runDocumentCase executes one document-face case and returns its ordered
// normalized facts.
func runDocumentCase(c *fileCase) ([]string, error) {
	profile, err := parseDocumentProfile(c)
	if err != nil {
		return nil, err
	}
	state := &docState{
		format:           c.Format,
		profile:          profile,
		foreignSource:    c.ForeignSource,
		foreignSourceHex: c.ForeignSourceHex,
		parseLimits:      document.DefaultParseLimits(),
		iniLimits:        ini.DefaultIniParseLimits(),
		propertiesLimits: properties.DefaultPropertiesParseLimits(),
	}
	applyParseLimits(&state.parseLimits, c.ParseLimits)
	applyParseLimitsState(state, c.ParseLimits)

	facts := &facts{}
	if !parseIntoState(state, c, profile, state.parseLimits) {
		facts.set("parse.formation", "Fatal")
		facts.set("parse.fatal_code", state.fatalCode)
		facts.set("parse.diagnostic_codes", "")
		facts.set("parse.root_kind", "")
		facts.set("parse.native", "")
		emitStepFacts(facts, state, stepDesc{})
		return facts.lines, nil
	}
	facts.set("parse.formation", state.formation)
	facts.set("parse.fatal_code", "")
	facts.set("parse.diagnostic_codes", state.diagnosticCodes)
	facts.set("parse.root_kind", state.rootKind)
	facts.set("parse.native", state.native)

	for _, step := range c.Steps {
		switch step.Op {
		case "parse":
			// already handled
		case "query-native", "query-syntax", "project", "materialize", "edit":
			emitStepFacts(facts, state, step)
		default:
			return nil, fmt.Errorf("case %s: unknown step op %q", c.ID, step.Op)
		}
	}
	// Every group's key set is emitted exactly once: groups whose step is
	// absent from the case report Blocked here, in the fixed order.
	emitStepFacts(facts, state, stepDesc{})
	return facts.lines, nil
}

// emitStepFacts emits the fixed fact keys for every dependent step. A step
// that is not declared in the case, or whose dependency failed, reports
// Blocked; each key set is emitted exactly once.
func emitStepFacts(facts *facts, state *docState, step stepDesc) {
	switch step.Op {
	case "query-native":
		emitNativeQuery(facts, state, &step)
	case "query-syntax":
		emitSyntaxQuery(facts, state, &step)
	case "project":
		emitProject(facts, state, &step)
	case "materialize":
		emitMaterialize(facts, state, &step)
	case "edit":
		emitEdit(facts, state, &step)
	default:
		emitNativeQuery(facts, state, nil)
		emitSyntaxQuery(facts, state, nil)
		emitProject(facts, state, nil)
		emitMaterialize(facts, state, nil)
		emitEdit(facts, state, nil)
	}
}

// parseIntoState parses the case source and fills the parse facts.
func parseIntoState(state *docState, c *fileCase, profile interface{}, limits document.ParseLimits) bool {
	switch c.Format {
	case "json":
		var p json.JsonProfile
		switch profile {
		case json.JsonProfileStrictV1:
			p = json.JsonProfileStrictV1
		case json.JsonProfileJsoncBoundedV1:
			p = json.JsonProfileJsoncBoundedV1
		case json.JsonProfileJson5StandardV1:
			p = json.JsonProfileJson5StandardV1
		}
		doc, failure := json.Parse(context.Background(), []byte(c.Source), p, limits)
		if failure != nil {
			state.fatalCode = failure.Code()
			return false
		}
		state.doc = doc
		state.formation = doc.FormationStatus().String()
		state.diagnosticCodes = diagnosticCodes(doc.Diagnostics())
		state.rootKind = jsonRootKind(doc)
		state.native = jsonNativeValue(doc.Root(), 0)
		return true
	case "toml":
		doc, failure := toml.Parse([]byte(c.Source), toml.Toml10V1, limits)
		if failure != nil {
			state.fatalCode = failure.Code()
			return false
		}
		state.tomlDoc = doc
		state.formation = doc.FormationStatus().String()
		state.diagnosticCodes = diagnosticCodes(doc.Diagnostics())
		state.rootKind = doc.Root().Kind().String()
		state.native = tomlNativeItem(doc.Root(), 0)
		return true
	case "yaml":
		var p yaml.YamlProfile
		switch profile {
		case yaml.Yaml12CoreV1:
			p = yaml.Yaml12CoreV1
		case yaml.Yaml11CompatV1:
			p = yaml.Yaml11CompatV1
		}
		doc, failure := yaml.Parse([]byte(c.Source), p, limits)
		if failure != nil {
			state.fatalCode = failure.Code()
			return false
		}
		state.yamlDoc = doc
		state.formation = doc.FormationStatus().String()
		state.diagnosticCodes = diagnosticCodes(doc.Diagnostics())
		state.rootKind = yamlRootKind(doc)
		state.native = yamlNativeSummary(doc)
		return true
	case "ini":
		var p ini.IniProfile
		switch profile {
		case ini.PortableV1:
			p = ini.PortableV1
		case ini.WindowsV1:
			p = ini.WindowsV1
		case ini.PythonConfigParserV1:
			p = ini.PythonConfigParserV1
		}
		doc, failure := ini.Parse([]byte(c.Source), p, ini.IniEncodingProfileDefault(),
			state.iniLimits)
		if failure != nil {
			state.fatalCode = failure.Code()
			return false
		}
		state.iniDoc = doc
		state.formation = doc.FormationStatus().String()
		state.diagnosticCodes = diagnosticCodes(doc.Diagnostics())
		state.rootKind = "Document"
		state.native = fmt.Sprintf("sections=%d entries=%d", len(doc.Sections()), len(doc.Entries()))
		return true
	case "properties":
		doc, failure := properties.ParseReader([]byte(c.Source), document.Utf8Encoding(),
			state.propertiesLimits)
		if failure != nil {
			state.fatalCode = failure.Code()
			return false
		}
		state.propertiesDoc = doc
		state.formation = doc.FormationStatus().String()
		state.diagnosticCodes = diagnosticCodes(doc.Diagnostics())
		state.rootKind = "Document"
		state.native = fmt.Sprintf("properties=%d comments=%d",
			len(doc.Properties()), len(doc.Comments()))
		return true
	}
	return false
}

// yamlRootKind renders the document-0 root node kind fact of a YAML stream.
func yamlRootKind(doc *yaml.Document) string {
	if doc.DocumentCount() == 0 {
		return "EmptyStream"
	}
	yamlDoc, ok := doc.Document(0)
	if !ok {
		return "EmptyStream"
	}
	return yamlDoc.Root().Kind().String()
}

// yamlNativeSummary renders the stream-level native facts: the document
// count and the alias occurrence count (the graph/alias face of the
// language-neutral surface, roadmap §11.2).
func yamlNativeSummary(doc *yaml.Document) string {
	return fmt.Sprintf("docs=%d aliases=%d", doc.DocumentCount(), doc.AliasCount())
}

// parseDocumentProfile resolves the case profile.
func parseDocumentProfile(c *fileCase) (interface{}, error) {
	switch c.Format {
	case "json":
		switch c.Profile {
		case "json.strict@1":
			return json.JsonProfileStrictV1, nil
		case "jsonc.bounded@1":
			return json.JsonProfileJsoncBoundedV1, nil
		case "json5.standard@1":
			return json.JsonProfileJson5StandardV1, nil
		}
	case "toml":
		if c.Profile == "toml.1.0@1" {
			return toml.Toml10V1, nil
		}
	case "yaml":
		switch c.Profile {
		case "yaml.1.2-core@1":
			return yaml.Yaml12CoreV1, nil
		case "yaml.1.1-compat@1":
			return yaml.Yaml11CompatV1, nil
		}
	case "ini":
		switch c.Profile {
		case "ini.portable@1":
			return ini.PortableV1, nil
		case "ini.windows@1":
			return ini.WindowsV1, nil
		case "ini.python-configparser@1":
			return ini.PythonConfigParserV1, nil
		}
	case "properties":
		switch c.Profile {
		case "java-properties.reader@1":
			return properties.PropertiesReaderV1, nil
		case "java-properties.latin1@1":
			return properties.PropertiesLatin1V1, nil
		}
	}
	return nil, fmt.Errorf("case %s: unknown format/profile %q/%q", c.ID, c.Format, c.Profile)
}

// applyParseLimits applies the descriptor overrides. The descriptor only
// carries the shared document.ParseLimits fields; the INI and Properties
// family limits (their Common members) inherit the same overrides so the
// descriptor keeps its existing shape.
func applyParseLimits(limits *document.ParseLimits, desc *parseLimitsDesc) {
	if desc == nil {
		return
	}
	if desc.MaxSourceBytes != nil {
		limits.MaxSourceBytes = *desc.MaxSourceBytes
	}
	if desc.MaxNestingDepth != nil {
		limits.MaxNestingDepth = *desc.MaxNestingDepth
	}
	if desc.MaxTokenCount != nil {
		limits.MaxTokenCount = *desc.MaxTokenCount
	}
	if desc.MaxNodeCount != nil {
		limits.MaxNodeCount = *desc.MaxNodeCount
	}
	if desc.MaxDiagnostics != nil {
		limits.MaxDiagnostics = *desc.MaxDiagnostics
	}
}

// applyParseLimitsState applies the shared descriptor overrides to the
// family-specific limits of the current state.
func applyParseLimitsState(state *docState, desc *parseLimitsDesc) {
	state.iniLimits.Common = state.parseLimits
	state.propertiesLimits.Common = state.parseLimits
	applyParseLimits(&state.iniLimits.Common, desc)
	applyParseLimits(&state.propertiesLimits.Common, desc)
}

// diagnosticCodes renders the ordered diagnostic codes.
func diagnosticCodes(diagnostics []*protocol.Diagnostic) string {
	codes := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		codes = append(codes, diagnostic.Code)
	}
	return join(codes)
}

// jsonRootKind renders the root native kind fact.
func jsonRootKind(doc *json.Document) string {
	kind := doc.Root().Kind()
	if !kind.IsAvailable() {
		return "Unavailable:" + kind.Reason().String()
	}
	return kind.Value().String()
}

// jsonNativeValue renders one JSON native value in the canonical summary
// vocabulary (mirrored by the Rust example).
func jsonNativeValue(value json.JsonValue, depth int) string {
	if depth > 64 {
		return "..."
	}
	kind := value.Kind()
	if !kind.IsAvailable() {
		return "Unavailable:" + kind.Reason().String()
	}
	switch kind.Value() {
	case json.JsonValueKindNull:
		return "null"
	case json.JsonValueKindBoolean:
		boolean := value.AsBoolean()
		return strconv.FormatBool(*boolean.Value())
	case json.JsonValueKindInteger:
		return value.AsInteger().Value().String()
	case json.JsonValueKindDecimal:
		decimal := value.AsDecimal().Value()
		return decimal.Coefficient().String() + "e" + decimal.Exponent().String()
	case json.JsonValueKindBinaryFloat64:
		number := value.AsBinaryFloat64().Value()
		return fmt.Sprintf("0x%016x", uint64(*number))
	case json.JsonValueKindString:
		text := value.AsString().Value()
		return `"` + escape(*text) + `"`
	case json.JsonValueKindArray:
		elements := value.ArrayElements()
		if !elements.IsAvailable() {
			return "Unavailable:" + elements.Reason().String()
		}
		items := elements.Value()
		parts := make([]string, 0, len(items))
		for _, element := range items {
			parts = append(parts, jsonNativeValue(element.Value(), depth+1))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case json.JsonValueKindObject:
		members := value.ObjectMembers()
		if !members.IsAvailable() {
			return "Unavailable:" + members.Reason().String()
		}
		items := members.Value()
		parts := make([]string, 0, len(items))
		for _, member := range items {
			renderedName := "?"
			if name := member.Name(); name.IsAvailable() {
				renderedName = escape(*name.Value())
			}
			parts = append(parts, `"`+renderedName+`":`+jsonNativeValue(member.Value(), depth+1))
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	return "?"
}

// tomlNativeItem renders one TOML native item in the canonical summary
// vocabulary (mirrored by the Rust example).
func tomlNativeItem(item toml.TomlItem, depth int) string {
	if depth > 64 {
		return "..."
	}
	switch item.Kind() {
	case toml.ItemKindString:
		text, _ := item.AsString()
		return `"` + escape(text) + `"`
	case toml.ItemKindInteger:
		number, _ := item.AsInteger()
		return strconv.FormatInt(number, 10)
	case toml.ItemKindFloat:
		bits, _ := item.AsFloatBits()
		return fmt.Sprintf("0x%016x", bits)
	case toml.ItemKindBoolean:
		value, _ := item.AsBoolean()
		return strconv.FormatBool(value)
	case toml.ItemKindOffsetDateTime, toml.ItemKindLocalDateTime,
		toml.ItemKindLocalDate, toml.ItemKindLocalTime:
		return tomlDateTimeSummary(item)
	case toml.ItemKindArray:
		elements, _ := item.ArrayElements()
		parts := make([]string, 0, len(elements))
		for _, element := range elements {
			parts = append(parts, tomlNativeItem(element.Item(), depth+1))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case toml.ItemKindInlineTable, toml.ItemKindRootTable, toml.ItemKindStandardTable,
		toml.ItemKindImplicitTable, toml.ItemKindDottedTable:
		entries, _ := item.TableEntries()
		parts := make([]string, 0, len(entries))
		for _, entry := range entries {
			parts = append(parts, `"`+escape(entry.Name())+`":`+tomlNativeItem(entry.Item(), depth+1))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case toml.ItemKindArrayOfTables:
		elements, _ := item.ArrayElements()
		parts := make([]string, 0, len(elements))
		for _, element := range elements {
			parts = append(parts, tomlNativeItem(element.Item(), depth+1))
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	return "?"
}

// tomlDateTimeSummary renders one TOML date/time datum canonically.
func tomlDateTimeSummary(item toml.TomlItem) string {
	dateTime, _ := item.AsDateTime()
	parts := make([]string, 0, 3)
	if dateTime.Date != nil {
		date := dateTime.Date
		parts = append(parts, fmt.Sprintf("date=%04d-%02d-%02d", date.Year, date.Month, date.Day))
	}
	if dateTime.Time != nil {
		value := dateTime.Time
		text := fmt.Sprintf("time=%02d:%02d:%02d", value.Hour, value.Minute, value.Second)
		if value.Nanosecond != 0 {
			text += "." + fmt.Sprintf("%09d", value.Nanosecond)
		}
		parts = append(parts, text)
	}
	if dateTime.Offset != nil {
		offset := dateTime.Offset
		if offset.Z {
			parts = append(parts, "offset=Z")
		} else {
			minutes := int(offset.Minutes)
			sign := "+"
			if minutes < 0 {
				sign = "-"
				minutes = -minutes
			}
			parts = append(parts, fmt.Sprintf("offset=%s%02d:%02d", sign, minutes/60, minutes%60))
		}
	}
	return "datetime(" + strings.Join(parts, ",") + ")"
}

// ---------------------------------------------------------------------------
// Query steps
// ---------------------------------------------------------------------------

// emitNativeQuery executes the optional native query step.
func emitNativeQuery(facts *facts, state *docState, step *stepDesc) {
	if state.queryNativeRun {
		return
	}
	blocked := func() {
		facts.set("query.native.status", "Blocked")
		facts.set("query.native.failure", "")
		facts.set("query.native.count", "")
		facts.set("query.native.matches", "")
	}
	if step == nil || step.Op != "query-native" || !state.documentParsed() {
		state.queryNativeRun = true
		blocked()
		return
	}
	state.queryNativeRun = true
	domain := protocol.NewQueryDomain(step.Domain, uint32(step.DomainVersion))
	executable, buildFailure := buildQueryDefinition(step, domain)
	if buildFailure != nil {
		facts.set("query.native.status", "Failed")
		facts.set("query.native.failure", buildFailure.Code())
		facts.set("query.native.count", "")
		facts.set("query.native.matches", "")
		return
	}
	limits := protocol.DefaultQueryLimits()
	applyQueryLimits(&limits, step.QueryLimits)
	if state.doc != nil {
		matches, queryFailure := json.ExecuteJSONQuery(context.Background(), executable, state.doc, limits)
		if queryFailure != nil {
			facts.set("query.native.status", "Failed")
			facts.set("query.native.failure", queryFailure.Code())
			facts.set("query.native.count", "")
			facts.set("query.native.matches", "")
			return
		}
		items := make([]string, 0, len(matches))
		for _, match := range matches {
			items = append(items, jsonNativeMatch(match))
		}
		facts.set("query.native.status", "Completed")
		facts.set("query.native.failure", "")
		facts.set("query.native.count", strconv.Itoa(len(matches)))
		facts.set("query.native.matches", join(items))
		return
	}
	if state.tomlDoc != nil {
		matches, queryFailure := toml.ExecuteTomlQuery(context.Background(), executable, state.tomlDoc, limits)
		if queryFailure != nil {
			facts.set("query.native.status", "Failed")
			facts.set("query.native.failure", queryFailure.Code())
			facts.set("query.native.count", "")
			facts.set("query.native.matches", "")
			return
		}
		items := make([]string, 0, len(matches))
		for _, match := range matches {
			items = append(items, tomlNativeMatch(match))
		}
		facts.set("query.native.status", "Completed")
		facts.set("query.native.failure", "")
		facts.set("query.native.count", strconv.Itoa(len(matches)))
		facts.set("query.native.matches", join(items))
		return
	}
	if state.yamlDoc != nil {
		yamlMatches, yamlFailure := yaml.ExecuteYamlQuery(context.Background(), executable,
			state.yamlDoc, limits)
		if yamlFailure != nil {
			facts.set("query.native.status", "Failed")
			facts.set("query.native.failure", yamlFailure.Code())
			facts.set("query.native.count", "")
			facts.set("query.native.matches", "")
			return
		}
		yamlItems := make([]string, 0, len(yamlMatches))
		for _, match := range yamlMatches {
			yamlItems = append(yamlItems, yamlNativeMatch(&match))
		}
		facts.set("query.native.status", "Completed")
		facts.set("query.native.failure", "")
		facts.set("query.native.count", strconv.Itoa(len(yamlMatches)))
		facts.set("query.native.matches", join(yamlItems))
		return
	}
	if state.iniDoc != nil {
		iniMatches, iniFailure := ini.ExecuteIniQuery(context.Background(), executable,
			state.iniDoc, limits)
		if iniFailure != nil {
			facts.set("query.native.status", "Failed")
			facts.set("query.native.failure", iniFailure.Code())
			facts.set("query.native.count", "")
			facts.set("query.native.matches", "")
			return
		}
		iniItems := make([]string, 0, len(iniMatches))
		for _, match := range iniMatches {
			iniItems = append(iniItems, iniNativeMatch(&match))
		}
		facts.set("query.native.status", "Completed")
		facts.set("query.native.failure", "")
		facts.set("query.native.count", strconv.Itoa(len(iniMatches)))
		facts.set("query.native.matches", join(iniItems))
		return
	}
	if state.propertiesDoc != nil {
		propertiesMatches, propertiesFailure := properties.ExecutePropertiesQuery(
			context.Background(), executable, state.propertiesDoc, limits)
		if propertiesFailure != nil {
			facts.set("query.native.status", "Failed")
			facts.set("query.native.failure", propertiesFailure.Code())
			facts.set("query.native.count", "")
			facts.set("query.native.matches", "")
			return
		}
		propertiesItems := make([]string, 0, len(propertiesMatches))
		for _, match := range propertiesMatches {
			propertiesItems = append(propertiesItems, propertiesNativeMatch(&match))
		}
		facts.set("query.native.status", "Completed")
		facts.set("query.native.failure", "")
		facts.set("query.native.count", strconv.Itoa(len(propertiesMatches)))
		facts.set("query.native.matches", join(propertiesItems))
	}
}

// yamlNativeMatch renders one YAML native match identity fact in the
// canonical vocabulary: KIND:identity where the identity is the ordinal of
// ordered matches, the node kind name of Node matches, and the escaped
// anchor name of AnchorDefinition matches.
func yamlNativeMatch(match *yaml.YamlMatch) string {
	switch match.Kind {
	case yaml.YamlMatchStream:
		return "Stream:0"
	case yaml.YamlMatchDocument:
		return fmt.Sprintf("Document:%d", match.Ordinal)
	case yaml.YamlMatchNode:
		return "Node:" + match.KindName.String()
	case yaml.YamlMatchMappingEntry:
		return fmt.Sprintf("MappingEntry:%d", match.Ordinal)
	case yaml.YamlMatchSequenceElement:
		return fmt.Sprintf("SequenceElement:%d", match.Ordinal)
	case yaml.YamlMatchAnchorDefinition:
		return "AnchorDefinition:" + escape(match.Anchor)
	case yaml.YamlMatchAliasOccurrence:
		return fmt.Sprintf("AliasOccurrence:%d", match.Ordinal)
	}
	return "?"
}

// iniNativeMatch renders one INI native match identity fact: KIND:ordinal.
func iniNativeMatch(match *ini.IniMatch) string {
	return string(match.Kind) + ":" + strconv.Itoa(match.Ordinal)
}

// propertiesNativeMatch renders one Properties native match identity fact:
// KIND:ordinal.
func propertiesNativeMatch(match *properties.PropertiesMatch) string {
	return string(match.Kind) + ":" + strconv.Itoa(match.Ordinal)
}

// emitSyntaxQuery executes the optional syntax query step.
func emitSyntaxQuery(facts *facts, state *docState, step *stepDesc) {
	if state.querySyntaxRun {
		return
	}
	blocked := func() {
		facts.set("query.syntax.status", "Blocked")
		facts.set("query.syntax.failure", "")
		facts.set("query.syntax.count", "")
		facts.set("query.syntax.matches", "")
	}
	if step == nil || step.Op != "query-syntax" || !state.documentParsed() {
		state.querySyntaxRun = true
		blocked()
		return
	}
	state.querySyntaxRun = true
	domain := protocol.NewQueryDomain(step.Domain, uint32(step.DomainVersion))
	executable, buildFailure := buildQueryDefinition(step, domain)
	if buildFailure != nil {
		facts.set("query.syntax.status", "Failed")
		facts.set("query.syntax.failure", buildFailure.Code())
		facts.set("query.syntax.count", "")
		facts.set("query.syntax.matches", "")
		return
	}
	limits := protocol.DefaultQueryLimits()
	applyQueryLimits(&limits, step.QueryLimits)
	if state.doc != nil {
		matches, queryFailure := json.ExecuteJSONSyntaxQuery(context.Background(), executable, state.doc, limits)
		if queryFailure != nil {
			facts.set("query.syntax.status", "Failed")
			facts.set("query.syntax.failure", queryFailure.Code())
			facts.set("query.syntax.count", "")
			facts.set("query.syntax.matches", "")
			return
		}
		items := make([]string, 0, len(matches))
		for _, match := range matches {
			items = append(items, fmt.Sprintf("%s@%d", match.Kind().AsStr(), match.Ordinal()))
		}
		facts.set("query.syntax.status", "Completed")
		facts.set("query.syntax.failure", "")
		facts.set("query.syntax.count", strconv.Itoa(len(matches)))
		facts.set("query.syntax.matches", join(items))
		return
	}
	if state.tomlDoc != nil {
		matches, queryFailure := toml.ExecuteTomlSyntaxQuery(context.Background(), executable, state.tomlDoc, limits)
		if queryFailure != nil {
			facts.set("query.syntax.status", "Failed")
			facts.set("query.syntax.failure", queryFailure.Code())
			facts.set("query.syntax.count", "")
			facts.set("query.syntax.matches", "")
			return
		}
		items := make([]string, 0, len(matches))
		for _, match := range matches {
			items = append(items, fmt.Sprintf("%s@%d", match.Kind().AsStr(), match.Ordinal()))
		}
		facts.set("query.syntax.status", "Completed")
		facts.set("query.syntax.failure", "")
		facts.set("query.syntax.count", strconv.Itoa(len(matches)))
		facts.set("query.syntax.matches", join(items))
		return
	}
	if state.yamlDoc != nil {
		matches, queryFailure := yaml.ExecuteYamlSyntaxQuery(context.Background(), executable,
			state.yamlDoc, limits)
		if queryFailure != nil {
			facts.set("query.syntax.status", "Failed")
			facts.set("query.syntax.failure", queryFailure.Code())
			facts.set("query.syntax.count", "")
			facts.set("query.syntax.matches", "")
			return
		}
		items := make([]string, 0, len(matches))
		for _, match := range matches {
			items = append(items, fmt.Sprintf("%s@%d", match.Kind().AsStr(), match.Ordinal()))
		}
		facts.set("query.syntax.status", "Completed")
		facts.set("query.syntax.failure", "")
		facts.set("query.syntax.count", strconv.Itoa(len(matches)))
		facts.set("query.syntax.matches", join(items))
		return
	}
	if state.iniDoc != nil {
		matches, queryFailure := ini.ExecuteIniSyntaxQuery(context.Background(), executable,
			state.iniDoc, limits)
		if queryFailure != nil {
			facts.set("query.syntax.status", "Failed")
			facts.set("query.syntax.failure", queryFailure.Code())
			facts.set("query.syntax.count", "")
			facts.set("query.syntax.matches", "")
			return
		}
		items := make([]string, 0, len(matches))
		for _, match := range matches {
			items = append(items, fmt.Sprintf("%s@%d", match.Kind().AsStr(), match.Ordinal()))
		}
		facts.set("query.syntax.status", "Completed")
		facts.set("query.syntax.failure", "")
		facts.set("query.syntax.count", strconv.Itoa(len(matches)))
		facts.set("query.syntax.matches", join(items))
		return
	}
	if state.propertiesDoc != nil {
		matches, queryFailure := properties.ExecutePropertiesSyntaxQuery(context.Background(),
			executable, state.propertiesDoc, limits)
		if queryFailure != nil {
			facts.set("query.syntax.status", "Failed")
			facts.set("query.syntax.failure", queryFailure.Code())
			facts.set("query.syntax.count", "")
			facts.set("query.syntax.matches", "")
			return
		}
		items := make([]string, 0, len(matches))
		for _, match := range matches {
			items = append(items, fmt.Sprintf("%s@%d", match.Kind().AsStr(), match.Ordinal()))
		}
		facts.set("query.syntax.status", "Completed")
		facts.set("query.syntax.failure", "")
		facts.set("query.syntax.count", strconv.Itoa(len(matches)))
		facts.set("query.syntax.matches", join(items))
	}
}

// jsonNativeMatch renders one JSON native match identity fact.
func jsonNativeMatch(match json.JsonMatch) string {
	switch match.Kind {
	case json.JsonMatchValue:
		kind := "?"
		if match.ValueKind != nil {
			kind = match.ValueKind.String()
		}
		return "V:" + kind
	case json.JsonMatchObjectMember:
		name := "?"
		if match.Name != nil {
			name = escape(*match.Name)
		}
		return fmt.Sprintf("M:%d:%s", match.Ordinal, name)
	case json.JsonMatchArrayElement:
		return fmt.Sprintf("E:%d", match.Ordinal)
	}
	return "?"
}

// tomlNativeMatch renders one TOML native match identity fact.
func tomlNativeMatch(match toml.TomlMatch) string {
	switch match.Kind {
	case "Item":
		return "I:" + match.KindName.String()
	case "Entry":
		return fmt.Sprintf("M:%d:%s", match.Ordinal, escape(match.Name))
	case "ArrayElement":
		return fmt.Sprintf("E:%d", match.Ordinal)
	}
	return "?"
}

// buildQueryDefinition builds the executable from the declarative filters,
// mirroring the conformance runner pipeline helpers (v1_json_face.go and the
// Rust syntax_query_v1.rs definition()).
func buildQueryDefinition(step *stepDesc, domain *protocol.QueryDomain) (*protocol.ExecutableQuery, *protocol.QueryFailure) {
	format := "json"
	switch {
	case strings.HasPrefix(step.Domain, "toml."):
		format = "toml"
	case strings.HasPrefix(step.Domain, "yaml."):
		format = "yaml"
	case strings.HasPrefix(step.Domain, "ini."):
		format = "ini"
	case strings.HasPrefix(step.Domain, "java-properties."):
		format = "properties"
	}
	calls := make([]*protocol.OperatorCall, 0, len(step.Filters))
	for _, filter := range step.Filters {
		var call *protocol.OperatorCall
		var buildFailure *protocol.QueryFailure
		switch filter.Operator {
		case "kind-is":
			call, buildFailure = argumentCall(format+".syntax-kind-is", "kind", &filter, filter.argumentString)
		case "text-equals":
			call, buildFailure = argumentCall(format+".syntax-text-equals", "text", &filter, filter.argumentString)
		case "take":
			call, buildFailure = argumentCall("core.take", "count", &filter, filter.argumentInteger)
		case "json.member-name-equals", "toml.entry-name-equals":
			call, buildFailure = argumentCall(filter.Operator, "name", &filter, filter.argumentString)
		case "yaml.where-node-kind":
			call, buildFailure = argumentCall("yaml.where-node-kind", "kind", &filter, filter.argumentString)
		case "yaml.where-tag":
			call, buildFailure = argumentCall("yaml.where-tag", "tag", &filter, filter.argumentString)
		case "yaml.scalar-canonical-equals":
			call, buildFailure = argumentCall("yaml.scalar-canonical-equals", "canonical", &filter, filter.argumentString)
		case "ini.entry-value-state-is":
			call, buildFailure = argumentCall("ini.entry-value-state-is", "state", &filter, filter.argumentString)
		case "properties.property-value-state-is":
			call, buildFailure = argumentCall("properties.property-value-state-is", "state", &filter, filter.argumentString)
		default:
			call = protocol.NewOperatorCall(filter.Operator, 1)
		}
		if buildFailure != nil {
			return nil, buildFailure
		}
		calls = append(calls, call)
	}
	var expression *protocol.QueryExpression
	switch step.Combine {
	case "Single", "":
		// A single filter chain applies every operator to the current input
		// in order (the vector pipeline semantics).
		expression = &protocol.QueryExpression{Kind: protocol.ExpressionInput}
		for _, call := range calls {
			expression = expression.Then(call)
		}
	case "StructureOrderMerge":
		branches := make([]*protocol.QueryExpression, 0, len(calls))
		for _, call := range calls {
			branches = append(branches, (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).Then(call))
		}
		expression = &protocol.QueryExpression{Kind: protocol.ExpressionStructureOrderMerge, Branches: branches}
	case "Concat":
		branches := make([]*protocol.QueryExpression, 0, len(calls))
		for _, call := range calls {
			branches = append(branches, (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).Then(call))
		}
		expression = &protocol.QueryExpression{Kind: protocol.ExpressionConcat, Branches: branches}
	default:
		return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
	}
	var selection protocol.QuerySelection
	switch step.Selection {
	case "All", "":
		selection = protocol.SelectionAll
	case "First":
		selection = protocol.SelectionFirst
	case "Last":
		selection = protocol.SelectionLast
	case "ZeroOrOne":
		selection = protocol.SelectionZeroOrOne
	case "RequireOne":
		selection = protocol.SelectionRequireOne
	default:
		return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument}
	}
	validated, failure := protocol.NewQueryDefinition(domain).
		WithExpression(expression).WithSelection(selection).Validate()
	if failure != nil {
		return nil, failure
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	return validated.Bind(capabilities)
}

// argumentString decodes a string filter argument; ok is false when the
// argument is absent or not a JSON string.
func (f *filterDesc) argumentString() (core.Value, bool) {
	if len(f.Argument) == 0 {
		return nil, false
	}
	var text string
	if err := stdjson.Unmarshal(f.Argument, &text); err != nil {
		return nil, false
	}
	return core.String(text), true
}

// argumentInteger decodes an integer filter argument; ok is false when the
// argument is absent or not a JSON integer.
func (f *filterDesc) argumentInteger() (core.Value, bool) {
	if len(f.Argument) == 0 {
		return nil, false
	}
	var number int64
	if err := stdjson.Unmarshal(f.Argument, &number); err != nil {
		return nil, false
	}
	return core.NewInteger(big.NewInt(number)), true
}

// argumentCall binds one operator argument from the filter descriptor with
// the Rust example's build semantics: a missing argument is an
// invalid-argument failure; a present but wrong-typed argument is bound
// verbatim so the definition validation reports the wrong argument kind
// (core.query.wrong-argument-type@1 on both sides).
func argumentCall(operator, name string, filter *filterDesc,
	decode func() (core.Value, bool)) (*protocol.OperatorCall, *protocol.QueryFailure) {
	call := protocol.NewOperatorCall(operator, 1)
	value, ok := decode()
	if ok {
		return call.WithArgument(name, value), nil
	}
	if len(filter.Argument) == 0 {
		return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument, Operator: operator, Argument: name}
	}
	raw, err := protocol.DecodeJSON(filter.Argument, protocol.DefaultProtocolLimits())
	if err != nil {
		return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument, Operator: operator, Argument: name}
	}
	return call.WithArgument(name, raw), nil
}

// applyQueryLimits applies the descriptor overrides.
func applyQueryLimits(limits *protocol.QueryLimits, desc *queryLimits) {
	if desc == nil {
		return
	}
	if desc.MaxResults != nil {
		limits.MaxResults = *desc.MaxResults
	}
	if desc.MaxSteps != nil {
		limits.MaxSteps = *desc.MaxSteps
	}
}

// ---------------------------------------------------------------------------
// Projection / materialization / edit steps
// ---------------------------------------------------------------------------

// emitProject executes the optional projection step.
func emitProject(facts *facts, state *docState, step *stepDesc) {
	if state.projectRun {
		return
	}
	blocked := func() {
		facts.set("project.status", "Blocked")
		facts.set("project.failure", "")
		facts.set("project.fidelity", "")
		facts.set("project.value_kind", "")
		facts.set("project.report", "")
		facts.set("project.provenance_entries", "")
	}
	if step == nil || step.Op != "project" || !state.documentParsed() {
		state.projectRun = true
		blocked()
		return
	}
	state.projectRun = true
	if state.doc != nil {
		request, buildFailure := buildJSONProjectionRequest(step)
		if buildFailure != nil {
			facts.set("project.status", "Failed")
			facts.set("project.failure", buildFailure.Code())
			facts.set("project.fidelity", "")
			facts.set("project.value_kind", "")
			facts.set("project.report", "")
			facts.set("project.provenance_entries", "")
			return
		}
		result := state.doc.Project(request)
		if result.Failed != nil {
			facts.set("project.status", "Failed")
			facts.set("project.failure", result.Failed.Diagnostics[0].Code)
			facts.set("project.fidelity", "")
			facts.set("project.value_kind", "")
			facts.set("project.report", jsonEventSummary(result.Failed.Report))
			facts.set("project.provenance_entries", "")
			return
		}
		state.value = result.Complete.Value
		state.projected = true
		facts.set("project.status", "Completed")
		facts.set("project.failure", "")
		facts.set("project.fidelity", result.Complete.Fidelity.String())
		facts.set("project.value_kind", neutralKindName(result.Complete.Value.Kind()))
		facts.set("project.report", jsonEventSummary(result.Complete.Report))
		facts.set("project.provenance_entries", strconv.Itoa(len(result.Complete.Provenance.Entries())))
		return
	}
	if state.tomlDoc != nil {
		request := buildTomlProjectionRequest(step)
		result := state.tomlDoc.Project(request)
		if result.Failed != nil {
			facts.set("project.status", "Failed")
			facts.set("project.failure", result.Failed.Diagnostics[0].Code)
			facts.set("project.fidelity", "")
			facts.set("project.value_kind", "")
			facts.set("project.report", tomlReportSummary(result.Failed.Report))
			facts.set("project.provenance_entries", "")
			return
		}
		state.value = result.Complete.Value
		state.projected = true
		facts.set("project.status", "Completed")
		facts.set("project.failure", "")
		facts.set("project.fidelity", string(result.Complete.Fidelity))
		facts.set("project.value_kind", neutralKindName(result.Complete.Value.Kind()))
		facts.set("project.report", tomlReportSummary(result.Complete.Report))
		facts.set("project.provenance_entries", strconv.Itoa(len(result.Complete.Provenance.Entries())))
		return
	}
	if state.yamlDoc != nil {
		result := state.yamlDoc.ProjectValue(yaml.BestExactValueV1())
		if result.Failed != nil {
			facts.set("project.status", "Failed")
			facts.set("project.failure", result.Failed.Code())
			facts.set("project.fidelity", "")
			facts.set("project.value_kind", "")
			facts.set("project.report", "")
			facts.set("project.provenance_entries", "")
			return
		}
		state.value = result.Complete.Value
		state.projected = true
		facts.set("project.status", "Completed")
		facts.set("project.failure", "")
		facts.set("project.fidelity", result.Complete.Fidelity.String())
		facts.set("project.value_kind", neutralKindName(result.Complete.Value.Kind()))
		facts.set("project.report", yamlEventSummary(result.Complete.Report))
		facts.set("project.provenance_entries",
			strconv.Itoa(len(result.Complete.Provenance.Entries())))
		return
	}
	if state.iniDoc != nil {
		result := state.iniDoc.Project(ini.BestExactEntryMappingV1())
		if result.Failed != nil {
			facts.set("project.status", "Failed")
			facts.set("project.failure", result.Failed.Diagnostics[0].Code)
			facts.set("project.fidelity", "")
			facts.set("project.value_kind", "")
			facts.set("project.report", iniEventSummary(result.Failed.Report))
			facts.set("project.provenance_entries", "")
			return
		}
		state.value = result.Complete.Value
		state.projected = true
		facts.set("project.status", "Completed")
		facts.set("project.failure", "")
		facts.set("project.fidelity", string(result.Complete.Fidelity))
		facts.set("project.value_kind", neutralKindName(result.Complete.Value.Kind()))
		facts.set("project.report", iniEventSummary(result.Complete.Report))
		facts.set("project.provenance_entries",
			strconv.Itoa(len(result.Complete.Provenance.Entries())))
		return
	}
	if state.propertiesDoc != nil {
		result := state.propertiesDoc.Project(properties.BestExactEntryMapping())
		if result.Failed != nil {
			facts.set("project.status", "Failed")
			facts.set("project.failure", result.Failed.Diagnostics[0].Code)
			facts.set("project.fidelity", "")
			facts.set("project.value_kind", "")
			facts.set("project.report", propertiesEventSummary(result.Failed.Report))
			facts.set("project.provenance_entries", "")
			return
		}
		state.value = result.Complete.Value
		state.projected = true
		facts.set("project.status", "Completed")
		facts.set("project.failure", "")
		facts.set("project.fidelity", result.Complete.Fidelity.String())
		facts.set("project.value_kind", neutralKindName(result.Complete.Value.Kind()))
		facts.set("project.report", propertiesEventSummary(result.Complete.Report))
		facts.set("project.provenance_entries",
			strconv.Itoa(len(result.Complete.Provenance.Entries())))
	}
}

// yamlEventSummary renders the YAML projection report as ordered
// EventKind:count pairs.
func yamlEventSummary(report yaml.ProjectionReport) string {
	order := make([]string, 0)
	counts := make(map[string]int)
	for _, event := range report.Events() {
		name := event.Kind.String()
		if _, seen := counts[name]; !seen {
			order = append(order, name)
		}
		counts[name]++
	}
	parts := make([]string, 0, len(order))
	for _, name := range order {
		parts = append(parts, fmt.Sprintf("%s:%d", name, counts[name]))
	}
	return join(parts)
}

// iniEventSummary renders the INI projection report as ordered
// EventKind:count pairs.
func iniEventSummary(report ini.ProjectionReport) string {
	order := make([]string, 0)
	counts := make(map[string]int)
	for _, event := range report.Events() {
		name := string(event.Kind)
		if _, seen := counts[name]; !seen {
			order = append(order, name)
		}
		counts[name]++
	}
	parts := make([]string, 0, len(order))
	for _, name := range order {
		parts = append(parts, fmt.Sprintf("%s:%d", name, counts[name]))
	}
	return join(parts)
}

// propertiesEventSummary renders the Properties projection report as
// ordered event-code:count pairs (the report events carry their registered
// code, java-properties.projection.duplicate-collapsed@1).
func propertiesEventSummary(report properties.ProjectionReport) string {
	order := make([]string, 0)
	counts := make(map[string]int)
	for _, event := range report.Events() {
		if _, seen := counts[event.Code]; !seen {
			order = append(order, event.Code)
		}
		counts[event.Code]++
	}
	parts := make([]string, 0, len(order))
	for _, name := range order {
		parts = append(parts, fmt.Sprintf("%s:%d", name, counts[name]))
	}
	return join(parts)
}

// buildJSONProjectionRequest builds the JSON projection request from the
// descriptor.
func buildJSONProjectionRequest(step *stepDesc) (*json.ProjectionRequest, *json.ProjectionFailure) {
	var target json.ProjectionTarget
	switch step.Target {
	case "ProjectAsObject":
		target = json.ProjectionTargetProjectAsObjectV1
	case "ProjectAsEntryMapping":
		target = json.ProjectionTargetProjectAsEntryMappingV1
	case "Json5BestExactCore":
		target = json.ProjectionTargetJson5BestExactCoreV1
	default:
		target = json.ProjectionTargetBestExactCoreV1
	}
	builder := json.NewProjectionRequestBuilder(target)
	switch step.DuplicatePolicy {
	case "FirstWins":
		builder = builder.GlobalDuplicatePolicy(json.DuplicateKeyPolicyFirstWins)
	case "LastWins":
		builder = builder.GlobalDuplicatePolicy(json.DuplicateKeyPolicyLastWins)
	}
	return builder.Build()
}

// buildTomlProjectionRequest builds the TOML projection request.
func buildTomlProjectionRequest(step *stepDesc) toml.ProjectionRequest {
	return toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1)
}

// neutralKindName maps one core kind to the language-neutral kind
// vocabulary (the array kind is "Sequence" on the PVCE surface).
func neutralKindName(kind core.Kind) string {
	if kind == core.KindArray {
		return "Sequence"
	}
	return kind.String()
}

// jsonEventSummary renders the JSON projection report as ordered
// EventKind:count pairs.
func jsonEventSummary(report json.ProjectionReport) string {
	order := make([]string, 0)
	counts := make(map[string]int)
	for _, event := range report.Events() {
		name := string(event.Kind)
		if _, seen := counts[name]; !seen {
			order = append(order, name)
		}
		counts[name]++
	}
	parts := make([]string, 0, len(order))
	for _, name := range order {
		parts = append(parts, fmt.Sprintf("%s:%d", name, counts[name]))
	}
	return join(parts)
}

// tomlReportSummary renders the TOML projection report as ordered
// diagnostic codes.
func tomlReportSummary(report toml.ProjectionReport) string {
	return diagnosticCodes(report.Events())
}

// emitMaterialize executes the optional materialization step.
func emitMaterialize(facts *facts, state *docState, step *stepDesc) {
	if state.materializeRun {
		return
	}
	blocked := func() {
		facts.set("materialize.status", "Blocked")
		facts.set("materialize.failure", "")
		facts.set("materialize.output", "")
		facts.set("materialize.fidelity", "")
	}
	if step == nil || step.Op != "materialize" || !state.documentParsed() {
		state.materializeRun = true
		blocked()
		return
	}
	state.materializeRun = true
	var value core.Value
	switch step.Input {
	case "", "project":
		if !state.projected {
			blocked()
			return
		}
		value = state.value
	case "value":
		decoded, ok := decodeMaterializeValue(step)
		if !ok {
			facts.set("materialize.status", "Failed")
			facts.set("materialize.failure", "core.protocol.invalid-value@1")
			facts.set("materialize.output", "")
			facts.set("materialize.fidelity", "")
			return
		}
		value = decoded
	default:
		facts.set("materialize.status", "Failed")
		facts.set("materialize.failure", "core.protocol.invalid-value@1")
		facts.set("materialize.output", "")
		facts.set("materialize.fidelity", "")
		return
	}
	request, ok := buildMaterializationRequest(step)
	if !ok {
		facts.set("materialize.status", "Failed")
		facts.set("materialize.failure", "core.materialization.invalid-request@1")
		facts.set("materialize.output", "")
		facts.set("materialize.fidelity", "")
		return
	}
	if state.doc != nil {
		result := json.Materialize(value, request)
		if result.Complete != nil {
			output := string(result.Complete.Document.Render())
			facts.set("materialize.status", "Completed")
			facts.set("materialize.failure", "")
			facts.set("materialize.output", escape(output))
			facts.set("materialize.fidelity", result.Complete.Fidelity.String())
			return
		}
		facts.set("materialize.status", "Failed")
		facts.set("materialize.failure", result.Failed.Failure.Code())
		facts.set("materialize.output", "")
		facts.set("materialize.fidelity", "")
		return
	}
	if state.tomlDoc != nil {
		result := toml.Materialize(value, request)
		if result.Complete != nil {
			output := string(result.Complete.Document.Render())
			facts.set("materialize.status", "Completed")
			facts.set("materialize.failure", "")
			facts.set("materialize.output", escape(output))
			facts.set("materialize.fidelity", string(result.Complete.Fidelity))
			return
		}
		facts.set("materialize.status", "Failed")
		facts.set("materialize.failure", result.Failed.Failure.Code())
		facts.set("materialize.output", "")
		facts.set("materialize.fidelity", "")
		return
	}
	if state.yamlDoc != nil {
		result := yaml.MaterializeValue(value, request)
		if result.Complete != nil {
			output := string(result.Complete.Document.Render())
			facts.set("materialize.status", "Completed")
			facts.set("materialize.failure", "")
			facts.set("materialize.output", escape(output))
			facts.set("materialize.fidelity", string(result.Complete.Fidelity))
			return
		}
		facts.set("materialize.status", "Failed")
		facts.set("materialize.failure", result.Failed.Failure.Code())
		facts.set("materialize.output", "")
		facts.set("materialize.fidelity", "")
		return
	}
	if state.iniDoc != nil {
		result := ini.Materialize(value, request)
		if result.Complete != nil {
			output := string(result.Complete.Document.Render())
			facts.set("materialize.status", "Completed")
			facts.set("materialize.failure", "")
			facts.set("materialize.output", escape(output))
			facts.set("materialize.fidelity", string(result.Complete.Fidelity))
			return
		}
		facts.set("materialize.status", "Failed")
		facts.set("materialize.failure", result.Failed.Failure.Code())
		facts.set("materialize.output", "")
		facts.set("materialize.fidelity", "")
		return
	}
	if state.propertiesDoc != nil {
		result := properties.Materialize(value, request)
		if result.Complete != nil {
			output := string(result.Complete.Document.Render())
			facts.set("materialize.status", "Completed")
			facts.set("materialize.failure", "")
			facts.set("materialize.output", escape(output))
			facts.set("materialize.fidelity", string(result.Complete.Fidelity))
			return
		}
		facts.set("materialize.status", "Failed")
		facts.set("materialize.failure", result.Failed.Failure.Code())
		facts.set("materialize.output", "")
		facts.set("materialize.fidelity", "")
	}
}

// decodeMaterializeValue decodes the materialize input descriptor through
// the canonical transport JSON decoder (RFC 0015 §3.2; the sanctioned
// cross-language byte surface beside PVCE/PGCE).
func decodeMaterializeValue(step *stepDesc) (core.Value, bool) {
	if step.EntryMapping != nil {
		key, err := protocol.DecodeJSON([]byte(step.EntryMapping.KeyJSON), protocol.DefaultProtocolLimits())
		if err != nil {
			return nil, false
		}
		value, err := protocol.DecodeJSON([]byte(step.EntryMapping.ValueJSON), protocol.DefaultProtocolLimits())
		if err != nil {
			return nil, false
		}
		builder := core.NewEntryMappingBuilder()
		if err := builder.Push(key, value); err != nil {
			return nil, false
		}
		return builder.Build(), true
	}
	decoded, err := protocol.DecodeJSON([]byte(step.ValueJSON), protocol.DefaultProtocolLimits())
	if err != nil {
		return nil, false
	}
	return decoded, true
}

// buildMaterializationRequest builds the request from the descriptor.
func buildMaterializationRequest(step *stepDesc) (document.MaterializationRequest, bool) {
	if step.TargetProfile == "" || step.Style == "" {
		return document.MaterializationRequest{}, false
	}
	targetParts := strings.SplitN(step.TargetProfile, "@", 2)
	styleParts := strings.SplitN(step.Style, "@", 2)
	request := document.NewMaterializationRequest(
		document.NewProfileId(targetParts[0], 1),
		document.NewMaterializationStyleId(styleParts[0], 1),
	)
	switch step.Newline {
	case "None":
		request = request.WithNewline(document.NewlineNone)
	case "CrLf":
		request = request.WithNewline(document.NewlineCrLf)
	default:
		request = request.WithNewline(document.NewlineLf)
	}
	if step.MatLimits != nil {
		limits := document.DefaultMaterializationLimits()
		if step.MatLimits.MaxOutputBytes != nil {
			limits.MaxOutputBytes = *step.MatLimits.MaxOutputBytes
		}
		if step.MatLimits.MaxInputNodes != nil {
			limits.MaxInputNodes = *step.MatLimits.MaxInputNodes
		}
		if step.MatLimits.MaxDepth != nil {
			limits.MaxDepth = *step.MatLimits.MaxDepth
		}
		if step.MatLimits.MaxProvenanceEntries != nil {
			limits.MaxProvenanceEntries = *step.MatLimits.MaxProvenanceEntries
		}
		request = request.WithLimits(limits)
	}
	return request, true
}

// emitEdit executes the optional edit step (one atomic transaction).
func emitEdit(facts *facts, state *docState, step *stepDesc) {
	if state.editRun {
		return
	}
	blocked := func() {
		facts.set("edit.status", "Blocked")
		facts.set("edit.failure", "")
		facts.set("edit.output", "")
		facts.set("edit.source_edit_count", "")
	}
	if step == nil || step.Op != "edit" || !state.documentParsed() {
		state.editRun = true
		blocked()
		return
	}
	state.editRun = true
	if state.doc != nil {
		if !state.ensureForeign() {
			facts.set("edit.status", "Failed")
			facts.set("edit.failure", "core.source.invalid-sequence@1")
			facts.set("edit.output", "")
			facts.set("edit.source_edit_count", "")
			return
		}
		builder := json.NewEditTransactionBuilder(state.doc)
		if !applyJSONEditOperations(builder, state, step) {
			facts.set("edit.status", "Failed")
			facts.set("edit.failure", "core.edit.target-not-found@1")
			facts.set("edit.output", "")
			facts.set("edit.source_edit_count", "")
			return
		}
		commit, editFailure := state.doc.Commit(builder.Build())
		if commit != nil {
			facts.set("edit.status", "Completed")
			facts.set("edit.failure", "")
			facts.set("edit.output", escape(string(commit.Document.Render())))
			facts.set("edit.source_edit_count", strconv.Itoa(len(commit.ChangeSet.SourceEdits())))
			return
		}
		facts.set("edit.status", "Failed")
		facts.set("edit.failure", editFailure.Code())
		facts.set("edit.output", "")
		facts.set("edit.source_edit_count", "")
		return
	}
	if state.tomlDoc != nil {
		builder := toml.NewEditTransactionBuilder(state.tomlDoc)
		if !applyTomlEditOperations(builder, state, step) {
			facts.set("edit.status", "Failed")
			facts.set("edit.failure", "core.edit.target-not-found@1")
			facts.set("edit.output", "")
			facts.set("edit.source_edit_count", "")
			return
		}
		commit, editFailure := state.tomlDoc.Commit(builder.Build())
		if commit != nil {
			facts.set("edit.status", "Completed")
			facts.set("edit.failure", "")
			facts.set("edit.output", escape(string(commit.Document.Render())))
			facts.set("edit.source_edit_count", strconv.Itoa(len(commit.ChangeSet.SourceEdits())))
			return
		}
		facts.set("edit.status", "Failed")
		facts.set("edit.failure", editFailure.Code())
		facts.set("edit.output", "")
		facts.set("edit.source_edit_count", "")
		return
	}
	if state.yamlDoc != nil {
		builder := yaml.NewEditTransactionBuilder(state.yamlDoc)
		if !applyYamlEditOperations(builder, state, step) {
			facts.set("edit.status", "Failed")
			facts.set("edit.failure", "core.edit.target-not-found@1")
			facts.set("edit.output", "")
			facts.set("edit.source_edit_count", "")
			return
		}
		commit, editFailure := state.yamlDoc.Commit(builder.Build())
		if commit != nil {
			facts.set("edit.status", "Completed")
			facts.set("edit.failure", "")
			facts.set("edit.output", escape(string(commit.Document.Render())))
			facts.set("edit.source_edit_count", strconv.Itoa(len(commit.ChangeSet.SourceEdits())))
			return
		}
		facts.set("edit.status", "Failed")
		facts.set("edit.failure", editFailure.Code())
		facts.set("edit.output", "")
		facts.set("edit.source_edit_count", "")
		return
	}
	if state.iniDoc != nil {
		builder := ini.NewEditTransactionBuilder(state.iniDoc)
		if !applyIniEditOperations(builder, state, step) {
			facts.set("edit.status", "Failed")
			facts.set("edit.failure", "core.edit.target-not-found@1")
			facts.set("edit.output", "")
			facts.set("edit.source_edit_count", "")
			return
		}
		commit, editFailure := state.iniDoc.Commit(builder.Build())
		if commit != nil {
			facts.set("edit.status", "Completed")
			facts.set("edit.failure", "")
			facts.set("edit.output", escape(string(commit.Document.Render())))
			facts.set("edit.source_edit_count", strconv.Itoa(len(commit.ChangeSet.SourceEdits())))
			return
		}
		facts.set("edit.status", "Failed")
		facts.set("edit.failure", editFailure.Code())
		facts.set("edit.output", "")
		facts.set("edit.source_edit_count", "")
		return
	}
	if state.propertiesDoc != nil {
		builder := properties.NewEditTransactionBuilder(state.propertiesDoc)
		if !applyPropertiesEditOperations(builder, state, step) {
			facts.set("edit.status", "Failed")
			facts.set("edit.failure", "core.edit.target-not-found@1")
			facts.set("edit.output", "")
			facts.set("edit.source_edit_count", "")
			return
		}
		commit, editFailure := state.propertiesDoc.Commit(builder.Build())
		if commit != nil {
			facts.set("edit.status", "Completed")
			facts.set("edit.failure", "")
			facts.set("edit.output", escape(string(commit.Document.Render())))
			facts.set("edit.source_edit_count", strconv.Itoa(len(commit.ChangeSet.SourceEdits())))
			return
		}
		facts.set("edit.status", "Failed")
		facts.set("edit.failure", editFailure.Code())
		facts.set("edit.output", "")
		facts.set("edit.source_edit_count", "")
	}
}

// ensureForeign parses the foreign source when the case declares one
// (the wrong-snapshot edit cases). The source is declared literally or as
// raw hex bytes; a declared source that fails to decode or parse reports
// edit.failure = core.source.invalid-sequence@1 (the Go-side norm that the
// Rust example mirrors).
func (s *docState) ensureForeign() bool {
	if s.foreign != nil || s.foreignToml != nil || s.foreignYaml != nil ||
		s.foreignIni != nil || s.foreignProperties != nil ||
		(s.foreignSource == "" && s.foreignSourceHex == "") {
		return true
	}
	foreignBytes := []byte(s.foreignSource)
	if s.foreignSourceHex != "" {
		decoded, err := hex.DecodeString(s.foreignSourceHex)
		if err != nil {
			return false
		}
		foreignBytes = decoded
	}
	switch s.format {
	case "json":
		var p json.JsonProfile
		switch s.profile {
		case json.JsonProfileStrictV1:
			p = json.JsonProfileStrictV1
		case json.JsonProfileJsoncBoundedV1:
			p = json.JsonProfileJsoncBoundedV1
		case json.JsonProfileJson5StandardV1:
			p = json.JsonProfileJson5StandardV1
		}
		doc, failure := json.Parse(context.Background(), foreignBytes, p, s.parseLimits)
		if failure != nil {
			return false
		}
		s.foreign = doc
		return true
	case "toml":
		doc, failure := toml.Parse(foreignBytes, toml.Toml10V1, s.parseLimits)
		if failure != nil {
			return false
		}
		s.foreignToml = doc
		return true
	case "yaml":
		var p yaml.YamlProfile
		switch s.profile {
		case yaml.Yaml12CoreV1:
			p = yaml.Yaml12CoreV1
		case yaml.Yaml11CompatV1:
			p = yaml.Yaml11CompatV1
		}
		doc, failure := yaml.Parse(foreignBytes, p, s.parseLimits)
		if failure != nil {
			return false
		}
		s.foreignYaml = doc
		return true
	case "ini":
		var p ini.IniProfile
		switch s.profile {
		case ini.PortableV1:
			p = ini.PortableV1
		case ini.WindowsV1:
			p = ini.WindowsV1
		case ini.PythonConfigParserV1:
			p = ini.PythonConfigParserV1
		}
		doc, failure := ini.Parse(foreignBytes, p, ini.IniEncodingProfileDefault(), s.iniLimits)
		if failure != nil {
			return false
		}
		s.foreignIni = doc
		return true
	case "properties":
		doc, failure := properties.ParseReader(foreignBytes, document.Utf8Encoding(),
			s.propertiesLimits)
		if failure != nil {
			return false
		}
		s.foreignProperties = doc
		return true
	}
	return false
}

// applyJSONEditOperations applies the declared operations to the builder;
// false means a descriptor could not be resolved.
func applyJSONEditOperations(builder *json.EditTransactionBuilder, state *docState, step *stepDesc) bool {
	for _, op := range step.Operations {
		switch op.Operation {
		case "semantic-scalar":
			value, ok := op.Value.coreValue()
			if !ok {
				return false
			}
			target, ok := resolveJSONTarget(state, op.Target)
			if !ok {
				return false
			}
			policy, ok := jsonRepresentationPolicy(op.Policy)
			if !ok {
				return false
			}
			builder.SemanticScalar(target, value, policy)
		case "literal-scalar":
			target, ok := resolveJSONTarget(state, op.Target)
			if !ok {
				return false
			}
			literal, err := hex.DecodeString(op.LiteralHex)
			if err != nil {
				return false
			}
			builder.LiteralScalar(target, literal)
		case "insert-member":
			container, ok := resolveJSONTarget(state, op.Target)
			if !ok {
				return false
			}
			value, ok := op.Value.coreValue()
			if !ok {
				return false
			}
			placement, ok := resolveJSONPlacement(state, op.Placement)
			if !ok {
				return false
			}
			builder.InsertMember(container, op.Name, value, placement)
		case "remove-member":
			target, ok := resolveJSONTarget(state, op.Target)
			if !ok {
				return false
			}
			builder.RemoveMember(target)
		case "rename-member":
			target, ok := resolveJSONTarget(state, op.Target)
			if !ok {
				return false
			}
			builder.RenameMember(target, op.Name)
		case "insert-array-element":
			container, ok := resolveJSONTarget(state, op.Target)
			if !ok {
				return false
			}
			value, ok := op.Value.coreValue()
			if !ok {
				return false
			}
			placement, ok := resolveJSONPlacement(state, op.Placement)
			if !ok {
				return false
			}
			builder.InsertArrayElement(container, value, placement)
		case "remove-array-element":
			target, ok := resolveJSONTarget(state, op.Target)
			if !ok {
				return false
			}
			builder.RemoveArrayElement(target)
		default:
			return false
		}
	}
	return true
}

// applyTomlEditOperations applies the declared TOML edit operations.
func applyTomlEditOperations(builder *toml.EditTransactionBuilder, state *docState, step *stepDesc) bool {
	for _, op := range step.Operations {
		switch op.Operation {
		case "semantic-scalar":
			value, ok := op.Value.coreValue()
			if !ok {
				return false
			}
			target, ok := resolveTomlTarget(state, op.Target)
			if !ok {
				return false
			}
			policy, ok := tomlRepresentationPolicy(op.Policy)
			if !ok {
				return false
			}
			builder.SemanticScalar(target, value, policy)
		case "literal-scalar":
			target, ok := resolveTomlTarget(state, op.Target)
			if !ok {
				return false
			}
			literal, err := hex.DecodeString(op.LiteralHex)
			if err != nil {
				return false
			}
			builder.LiteralScalar(target, literal)
		case "insert-entry":
			container, ok := resolveTomlTarget(state, op.Target)
			if !ok {
				return false
			}
			value, ok := op.Value.coreValue()
			if !ok {
				return false
			}
			placement, ok := resolveTomlPlacement(state, op.Placement)
			if !ok {
				return false
			}
			builder.InsertEntry(container, op.Name, value, placement)
		case "remove-entry":
			target, ok := resolveTomlTarget(state, op.Target)
			if !ok {
				return false
			}
			builder.RemoveEntry(target)
		case "rename-entry":
			target, ok := resolveTomlTarget(state, op.Target)
			if !ok {
				return false
			}
			builder.RenameEntry(target, op.Name)
		case "insert-array-element":
			container, ok := resolveTomlTarget(state, op.Target)
			if !ok {
				return false
			}
			value, ok := op.Value.coreValue()
			if !ok {
				return false
			}
			placement, ok := resolveTomlPlacement(state, op.Placement)
			if !ok {
				return false
			}
			builder.InsertArrayElement(container, value, placement)
		case "remove-array-element":
			target, ok := resolveTomlTarget(state, op.Target)
			if !ok {
				return false
			}
			builder.RemoveArrayElement(target)
		default:
			return false
		}
	}
	return true
}

// applyYamlEditOperations applies the declared YAML edit operations; false
// means a descriptor could not be resolved.
func applyYamlEditOperations(builder *yaml.EditTransactionBuilder, state *docState, step *stepDesc) bool {
	for _, op := range step.Operations {
		switch op.Operation {
		case "semantic-scalar":
			value, ok := op.Value.coreValue()
			if !ok {
				return false
			}
			target, ok := resolveYamlTarget(state, op.Target)
			if !ok {
				return false
			}
			policy, ok := yamlRepresentationPolicy(op.Policy)
			if !ok {
				return false
			}
			builder.SemanticScalar(target, value, policy)
		case "literal-scalar":
			target, ok := resolveYamlTarget(state, op.Target)
			if !ok {
				return false
			}
			literal, err := hex.DecodeString(op.LiteralHex)
			if err != nil {
				return false
			}
			builder.LiteralScalar(target, literal)
		case "rename-anchor":
			target, ok := resolveYamlTarget(state, op.Target)
			if !ok {
				return false
			}
			builder.RenameAnchor(target, op.Name)
		case "insert-mapping-entry":
			container, ok := resolveYamlTarget(state, op.Target)
			if !ok {
				return false
			}
			value, ok := op.Value.coreValue()
			if !ok {
				return false
			}
			placement, ok := resolveYamlPlacement(state, op.Placement)
			if !ok {
				return false
			}
			builder.InsertMappingEntry(container, core.String(op.Name), value, placement)
		case "remove-mapping-entry":
			target, ok := resolveYamlTarget(state, op.Target)
			if !ok {
				return false
			}
			builder.RemoveMappingEntry(target)
		case "insert-sequence-element":
			container, ok := resolveYamlTarget(state, op.Target)
			if !ok {
				return false
			}
			value, ok := op.Value.coreValue()
			if !ok {
				return false
			}
			placement, ok := resolveYamlPlacement(state, op.Placement)
			if !ok {
				return false
			}
			builder.InsertSequenceElement(container, value, placement)
		case "remove-sequence-element":
			target, ok := resolveYamlTarget(state, op.Target)
			if !ok {
				return false
			}
			builder.RemoveSequenceElement(target)
		default:
			return false
		}
	}
	return true
}

// applyIniEditOperations applies the declared INI edit operations; false
// means a descriptor could not be resolved.
func applyIniEditOperations(builder *ini.EditTransactionBuilder, state *docState, step *stepDesc) bool {
	for _, op := range step.Operations {
		switch op.Operation {
		case "semantic-value":
			target, ok := resolveIniTarget(state, op.Target)
			if !ok {
				return false
			}
			value, ok := op.Value.coreString()
			if !ok {
				return false
			}
			policy, ok := iniRepresentationPolicy(op.Policy)
			if !ok {
				return false
			}
			builder.SemanticValue(target, value, policy)
		case "literal-value":
			target, ok := resolveIniTarget(state, op.Target)
			if !ok {
				return false
			}
			literal, err := hex.DecodeString(op.LiteralHex)
			if err != nil {
				return false
			}
			builder.LiteralValue(target, literal)
		case "insert-section":
			container, ok := resolveIniTarget(state, op.Target)
			if !ok {
				return false
			}
			placement, ok := resolveIniPlacement(state, op.Placement)
			if !ok {
				return false
			}
			builder.InsertSection(container, op.Name, placement)
		case "remove-section":
			target, ok := resolveIniTarget(state, op.Target)
			if !ok {
				return false
			}
			builder.RemoveSection(target)
		case "rename-section":
			target, ok := resolveIniTarget(state, op.Target)
			if !ok {
				return false
			}
			builder.RenameSection(target, op.Name)
		case "insert-entry":
			container, ok := resolveIniTarget(state, op.Target)
			if !ok {
				return false
			}
			value, ok := op.Value.coreString()
			if !ok {
				return false
			}
			placement, ok := resolveIniPlacement(state, op.Placement)
			if !ok {
				return false
			}
			builder.InsertEntry(container, op.Name, value, placement)
		case "remove-entry":
			target, ok := resolveIniTarget(state, op.Target)
			if !ok {
				return false
			}
			builder.RemoveEntry(target)
		case "rename-entry":
			target, ok := resolveIniTarget(state, op.Target)
			if !ok {
				return false
			}
			builder.RenameEntry(target, op.Name)
		default:
			return false
		}
	}
	return true
}

// applyPropertiesEditOperations applies the declared Properties edit
// operations; false means a descriptor could not be resolved.
func applyPropertiesEditOperations(builder *properties.EditTransactionBuilder, state *docState, step *stepDesc) bool {
	for _, op := range step.Operations {
		switch op.Operation {
		case "semantic-value":
			target, ok := resolvePropertiesTarget(state, op.Target)
			if !ok {
				return false
			}
			value, ok := op.Value.coreString()
			if !ok {
				return false
			}
			builder.SemanticValue(target, properties.NewJavaStringFromUnicode(value))
		case "literal-value":
			target, ok := resolvePropertiesTarget(state, op.Target)
			if !ok {
				return false
			}
			literal, err := hex.DecodeString(op.LiteralHex)
			if err != nil {
				return false
			}
			builder.LiteralValue(target, literal)
		case "insert-property":
			container, ok := resolvePropertiesTarget(state, op.Target)
			if !ok {
				return false
			}
			value, ok := op.Value.coreString()
			if !ok {
				return false
			}
			placement, ok := resolvePropertiesPlacement(state, op.Placement)
			if !ok {
				return false
			}
			builder.InsertProperty(container, properties.NewJavaStringFromUnicode(op.Name),
				properties.NewJavaStringFromUnicode(value), placement)
		case "remove-property":
			target, ok := resolvePropertiesTarget(state, op.Target)
			if !ok {
				return false
			}
			builder.RemoveProperty(target)
		case "rename-property":
			target, ok := resolvePropertiesTarget(state, op.Target)
			if !ok {
				return false
			}
			builder.RenameProperty(target, properties.NewJavaStringFromUnicode(op.Name))
		default:
			return false
		}
	}
	return true
}

// resolveYamlTarget resolves one target descriptor to a YAML node handle.
func resolveYamlTarget(state *docState, target *targetDesc) (document.NodeRef, bool) {
	doc := state.yamlDoc
	if target.Foreign {
		if state.foreignYaml == nil {
			return document.NodeRef{}, false
		}
		doc = state.foreignYaml
	}
	yamlDoc, ok := doc.Document(0)
	if !ok {
		return document.NodeRef{}, false
	}
	root := yamlDoc.Root()
	switch target.Kind {
	case "document-root":
		return root.NodeRef(), true
	case "mapping-entry":
		entry, ok := root.MappingEntry(target.Ordinal)
		if !ok {
			return document.NodeRef{}, false
		}
		return entry.NodeRef(), true
	case "mapping-value":
		entry, ok := root.MappingEntry(target.Ordinal)
		if !ok {
			return document.NodeRef{}, false
		}
		return entry.Value().NodeRef(), true
	case "mapping-key":
		entry, ok := root.MappingEntry(target.Ordinal)
		if !ok {
			return document.NodeRef{}, false
		}
		return entry.Key().NodeRef(), true
	case "sequence-element":
		// A root-level sequence item, or the sequence under the first
		// mapping entry (the anchored nested-sequence shape).
		if item, ok := root.SequenceItem(target.Ordinal); ok {
			return item.NodeRef(), true
		}
		if entry, ok := root.MappingEntry(0); ok {
			if item, ok := entry.Value().SequenceItem(target.Ordinal); ok {
				return item.NodeRef(), true
			}
		}
		return document.NodeRef{}, false
	case "sequence-element-node":
		if item, ok := root.SequenceItem(target.Ordinal); ok {
			return item.Node().NodeRef(), true
		}
		if entry, ok := root.MappingEntry(0); ok {
			if item, ok := entry.Value().SequenceItem(target.Ordinal); ok {
				return item.Node().NodeRef(), true
			}
		}
		return document.NodeRef{}, false
	case "anchor-value":
		entry, ok := root.MappingEntry(target.Ordinal)
		if !ok {
			return document.NodeRef{}, false
		}
		anchor, ok := entry.Value().AnchorNodeRef()
		if !ok {
			return document.NodeRef{}, false
		}
		return anchor, true
	}
	return document.NodeRef{}, false
}

// resolveIniTarget resolves one target descriptor to an INI node handle.
func resolveIniTarget(state *docState, target *targetDesc) (document.NodeRef, bool) {
	doc := state.iniDoc
	if target.Foreign {
		if state.foreignIni == nil {
			return document.NodeRef{}, false
		}
		doc = state.foreignIni
	}
	switch target.Kind {
	case "document":
		return doc.NodeRef(), true
	case "section":
		sections := doc.Sections()
		if target.Ordinal >= len(sections) {
			return document.NodeRef{}, false
		}
		return sections[target.Ordinal].NodeRef(), true
	case "entry":
		entries := doc.Entries()
		if target.Ordinal >= len(entries) {
			return document.NodeRef{}, false
		}
		return entries[target.Ordinal].NodeRef(), true
	}
	return document.NodeRef{}, false
}

// resolvePropertiesTarget resolves one target descriptor to a Properties
// node handle.
func resolvePropertiesTarget(state *docState, target *targetDesc) (document.NodeRef, bool) {
	doc := state.propertiesDoc
	if target.Foreign {
		if state.foreignProperties == nil {
			return document.NodeRef{}, false
		}
		doc = state.foreignProperties
	}
	switch target.Kind {
	case "document":
		return doc.NodeRef(), true
	case "property":
		properties := doc.Properties()
		if target.Ordinal >= len(properties) {
			return document.NodeRef{}, false
		}
		return properties[target.Ordinal].NodeRef(), true
	}
	return document.NodeRef{}, false
}

// resolveYamlPlacement resolves one placement descriptor for YAML.
func resolveYamlPlacement(state *docState, placement *placementDesc) (yaml.AssociationPlacement, bool) {
	if placement == nil {
		return yaml.PlacementEnd(), true
	}
	switch placement.At {
	case "start":
		return yaml.PlacementStart(), true
	case "end":
		return yaml.PlacementEnd(), true
	}
	if placement.BeforeOrdinal != nil {
		anchor, ok := yamlOrdinalAnchor(state, *placement.BeforeOrdinal)
		if !ok {
			return yaml.AssociationPlacement{}, false
		}
		return yaml.PlacementBefore(anchor), true
	}
	if placement.AfterOrdinal != nil {
		anchor, ok := yamlOrdinalAnchor(state, *placement.AfterOrdinal)
		if !ok {
			return yaml.AssociationPlacement{}, false
		}
		return yaml.PlacementAfter(anchor), true
	}
	return yaml.PlacementEnd(), true
}

// resolveIniPlacement resolves one placement descriptor for INI.
func resolveIniPlacement(state *docState, placement *placementDesc) (ini.AssociationPlacement, bool) {
	if placement == nil {
		return ini.PlacementEnd(), true
	}
	switch placement.At {
	case "start":
		return ini.PlacementStart(), true
	case "end":
		return ini.PlacementEnd(), true
	}
	return ini.PlacementEnd(), true
}

// resolvePropertiesPlacement resolves one placement descriptor for
// Properties.
func resolvePropertiesPlacement(state *docState, placement *placementDesc) (properties.AssociationPlacement, bool) {
	if placement == nil {
		return properties.PlacementEnd(), true
	}
	switch placement.At {
	case "start":
		return properties.PlacementStart(), true
	case "end":
		return properties.PlacementEnd(), true
	}
	return properties.PlacementEnd(), true
}

// yamlOrdinalAnchor resolves the anchor of the current YAML container: the
// mapping entries for insert-mapping-entry, the sequence elements for
// insert-sequence-element.
func yamlOrdinalAnchor(state *docState, ordinal int) (document.NodeRef, bool) {
	yamlDoc, ok := state.yamlDoc.Document(0)
	if !ok {
		return document.NodeRef{}, false
	}
	root := yamlDoc.Root()
	if entry, ok := root.MappingEntry(ordinal); ok {
		return entry.NodeRef(), true
	}
	if item, ok := root.SequenceItem(ordinal); ok {
		return item.NodeRef(), true
	}
	return document.NodeRef{}, false
}

// yamlRepresentationPolicy resolves one policy name.
func yamlRepresentationPolicy(name string) (yaml.RepresentationPolicy, bool) {
	switch name {
	case "PreserveCompatible":
		return yaml.RepresentationPolicyPreserveCompatible, true
	case "CanonicalForProfile":
		return yaml.RepresentationPolicyCanonicalForProfile, true
	case "PreserveElseCanonical":
		return yaml.RepresentationPolicyPreserveElseCanonical, true
	case "ExactLiteral":
		return yaml.RepresentationPolicyExactLiteral, true
	}
	return yaml.RepresentationPolicyExactLiteral, false
}

// iniRepresentationPolicy resolves one policy name.
func iniRepresentationPolicy(name string) (ini.RepresentationPolicy, bool) {
	switch name {
	case "PreserveCompatible":
		return ini.RepresentationPolicyPreserveCompatible, true
	case "CanonicalForProfile":
		return ini.RepresentationPolicyCanonicalForProfile, true
	case "PreserveElseCanonical":
		return ini.RepresentationPolicyPreserveElseCanonical, true
	case "ExactLiteral":
		return ini.RepresentationPolicyExactLiteral, true
	}
	return ini.RepresentationPolicyExactLiteral, false
}

// resolveJSONTarget resolves one target descriptor to a JSON node handle.
func resolveJSONTarget(state *docState, target *targetDesc) (document.NodeRef, bool) {
	doc := state.doc
	if target.Foreign {
		if state.foreign == nil {
			return document.NodeRef{}, false
		}
		doc = state.foreign
	}
	root := doc.Root()
	switch target.Kind {
	case "root":
		return root.NodeRef(), true
	case "member":
		members := root.ObjectMembers()
		if !members.IsAvailable() || target.Ordinal >= len(members.Value()) {
			return document.NodeRef{}, false
		}
		return members.Value()[target.Ordinal].NodeRef(), true
	case "member-value":
		members := root.ObjectMembers()
		if !members.IsAvailable() || target.Ordinal >= len(members.Value()) {
			return document.NodeRef{}, false
		}
		return members.Value()[target.Ordinal].ValueNodeRef(), true
	case "member-key":
		members := root.ObjectMembers()
		if !members.IsAvailable() || target.Ordinal >= len(members.Value()) {
			return document.NodeRef{}, false
		}
		return members.Value()[target.Ordinal].KeyNodeRef(), true
	case "array-element":
		elements := root.ArrayElements()
		if !elements.IsAvailable() || target.Ordinal >= len(elements.Value()) {
			return document.NodeRef{}, false
		}
		return elements.Value()[target.Ordinal].NodeRef(), true
	case "array-element-value":
		elements := root.ArrayElements()
		if !elements.IsAvailable() || target.Ordinal >= len(elements.Value()) {
			return document.NodeRef{}, false
		}
		return elements.Value()[target.Ordinal].ValueNodeRef(), true
	}
	return document.NodeRef{}, false
}

// resolveTomlTarget resolves one target descriptor to a TOML node handle.
func resolveTomlTarget(state *docState, target *targetDesc) (document.NodeRef, bool) {
	doc := state.tomlDoc
	if target.Foreign {
		if state.foreignToml == nil {
			return document.NodeRef{}, false
		}
		doc = state.foreignToml
	}
	root := doc.Root()
	switch target.Kind {
	case "root":
		return root.NodeRef(), true
	case "entry":
		entries, ok := root.TableEntries()
		if !ok || target.Ordinal >= len(entries) {
			return document.NodeRef{}, false
		}
		return entries[target.Ordinal].NodeRef(), true
	case "entry-item":
		entries, ok := root.TableEntries()
		if !ok || target.Ordinal >= len(entries) {
			return document.NodeRef{}, false
		}
		return entries[target.Ordinal].ItemNodeRef(), true
	case "entry-key":
		entries, ok := root.TableEntries()
		if !ok || target.Ordinal >= len(entries) {
			return document.NodeRef{}, false
		}
		return entries[target.Ordinal].KeyNodeRef(), true
	case "array-element":
		elements, ok := root.ArrayElements()
		if !ok || target.Ordinal >= len(elements) {
			return document.NodeRef{}, false
		}
		return elements[target.Ordinal].NodeRef(), true
	case "array-element-item":
		elements, ok := root.ArrayElements()
		if !ok || target.Ordinal >= len(elements) {
			return document.NodeRef{}, false
		}
		return elements[target.Ordinal].ItemNodeRef(), true
	}
	return document.NodeRef{}, false
}

// resolveJSONPlacement resolves one placement descriptor for JSON.
func resolveJSONPlacement(state *docState, placement *placementDesc) (json.AssociationPlacement, bool) {
	if placement == nil {
		return json.PlacementAtEnd(), true
	}
	switch placement.At {
	case "start":
		return json.PlacementAtStart(), true
	case "end":
		return json.PlacementAtEnd(), true
	}
	if placement.BeforeOrdinal != nil {
		anchor, ok := jsonOrdinalAnchor(state, *placement.BeforeOrdinal)
		if !ok {
			return json.AssociationPlacement{}, false
		}
		return json.BeforeAnchor(anchor), true
	}
	if placement.AfterOrdinal != nil {
		anchor, ok := jsonOrdinalAnchor(state, *placement.AfterOrdinal)
		if !ok {
			return json.AssociationPlacement{}, false
		}
		return json.AfterAnchor(anchor), true
	}
	return json.PlacementAtEnd(), true
}

// resolveTomlPlacement resolves one placement descriptor for TOML.
func resolveTomlPlacement(state *docState, placement *placementDesc) (toml.AssociationPlacement, bool) {
	if placement == nil {
		return toml.PlacementEnd(), true
	}
	switch placement.At {
	case "start":
		return toml.PlacementStart(), true
	case "end":
		return toml.PlacementEnd(), true
	}
	if placement.BeforeOrdinal != nil {
		anchor, ok := tomlOrdinalAnchor(state, *placement.BeforeOrdinal)
		if !ok {
			return toml.AssociationPlacement{}, false
		}
		return toml.PlacementBefore(anchor), true
	}
	if placement.AfterOrdinal != nil {
		anchor, ok := tomlOrdinalAnchor(state, *placement.AfterOrdinal)
		if !ok {
			return toml.AssociationPlacement{}, false
		}
		return toml.PlacementAfter(anchor), true
	}
	return toml.PlacementEnd(), true
}

// jsonOrdinalAnchor resolves the anchor of the current JSON container: the
// members for insert-member, the elements for insert-array-element.
func jsonOrdinalAnchor(state *docState, ordinal int) (document.NodeRef, bool) {
	root := state.doc.Root()
	members := root.ObjectMembers()
	if members.IsAvailable() && ordinal < len(members.Value()) {
		return members.Value()[ordinal].NodeRef(), true
	}
	elements := root.ArrayElements()
	if elements.IsAvailable() && ordinal < len(elements.Value()) {
		return elements.Value()[ordinal].NodeRef(), true
	}
	return document.NodeRef{}, false
}

// tomlOrdinalAnchor resolves the anchor of the current TOML container: the
// root table entries for insert-entry, the array elements for
// insert-array-element.
func tomlOrdinalAnchor(state *docState, ordinal int) (document.NodeRef, bool) {
	root := state.tomlDoc.Root()
	entries, ok := root.TableEntries()
	if ok && ordinal < len(entries) {
		return entries[ordinal].NodeRef(), true
	}
	elements, ok := root.ArrayElements()
	if ok && ordinal < len(elements) {
		return elements[ordinal].NodeRef(), true
	}
	return document.NodeRef{}, false
}

// jsonRepresentationPolicy resolves one policy name.
func jsonRepresentationPolicy(name string) (json.RepresentationPolicy, bool) {
	switch name {
	case "PreserveCompatible":
		return json.RepresentationPolicyPreserveCompatible, true
	case "CanonicalForProfile":
		return json.RepresentationPolicyCanonicalForProfile, true
	case "PreserveElseCanonical":
		return json.RepresentationPolicyPreserveElseCanonical, true
	case "ExactLiteral":
		return json.RepresentationPolicyExactLiteral, true
	}
	return json.RepresentationPolicyExactLiteral, false
}

// tomlRepresentationPolicy resolves one policy name.
func tomlRepresentationPolicy(name string) (toml.RepresentationPolicy, bool) {
	switch name {
	case "PreserveCompatible":
		return toml.RepresentationPreserveCompatible, true
	case "CanonicalForProfile":
		return toml.RepresentationCanonicalForProfile, true
	case "PreserveElseCanonical":
		return toml.RepresentationPreserveElseCanonical, true
	case "ExactLiteral":
		return toml.RepresentationExactLiteral, true
	}
	return toml.RepresentationExactLiteral, false
}

// coreValue builds one core.Value from a scalar descriptor.
// coreString renders the string descriptor as Go text.
func (v *valueDesc) coreString() (string, bool) {
	if v.String == "" {
		return "", false
	}
	return v.String, true
}

func (v *valueDesc) coreValue() (core.Value, bool) {
	switch {
	case v.Null != nil:
		return core.NullValue(), true
	case v.Boolean != nil:
		return core.Boolean(*v.Boolean), true
	case v.Integer != "":
		number, ok := new(big.Int).SetString(v.Integer, 10)
		if !ok {
			return nil, false
		}
		return core.NewInteger(number), true
	case v.Decimal != "":
		decimal, err := parseDecimalNumber(v.Decimal)
		if err != nil {
			return nil, false
		}
		return decimal, true
	case v.String != "":
		return core.String(v.String), true
	case v.Binary64 != "":
		bits, err := strconv.ParseUint(strings.TrimPrefix(v.Binary64, "0x"), 16, 64)
		if err != nil {
			return nil, false
		}
		return core.NewBinaryFloat64(bits), true
	}
	return nil, false
}

// parseDecimalNumber parses one JSON-number spelling ("1.00", "10e-1")
// into its canonical coefficient × 10^exponent decimal (the conformance
// runner helper, conformance/v1.go:145).
func parseDecimalNumber(source string) (core.Decimal, error) {
	coefficientText := source
	exponent := big.NewInt(0)
	if index := strings.IndexAny(source, "eE"); index >= 0 {
		exponentText := source[index+1:]
		coefficientText = source[:index]
		parsed, ok := new(big.Int).SetString(exponentText, 10)
		if !ok {
			return core.Decimal{}, fmt.Errorf("invalid decimal exponent %q", exponentText)
		}
		exponent = parsed
	}
	scale := big.NewInt(0)
	if index := strings.IndexByte(coefficientText, '.'); index >= 0 {
		fraction := coefficientText[index+1:]
		coefficientText = coefficientText[:index] + fraction
		scale = big.NewInt(int64(-len(fraction)))
	}
	if coefficientText == "" || coefficientText == "-" || coefficientText == "+" {
		return core.Decimal{}, fmt.Errorf("invalid decimal coefficient %q", coefficientText)
	}
	coefficient, ok := new(big.Int).SetString(coefficientText, 10)
	if !ok {
		return core.Decimal{}, fmt.Errorf("invalid decimal coefficient %q", coefficientText)
	}
	exponent.Add(exponent, scale)
	return core.NewDecimal(coefficient, exponent), nil
}
