// Package normalized implements the Go side of the cross-language
// normalized-result differential harness (milestone 0.15.0 G1.5;
// docs/go-implementation-plan.md §4.4 and §2.2; roadmap §16.2 line 1488 and
// §11.2 lines 849-861).
//
// The harness compares the language-neutral normalized results of the same
// data-driven input set (`cases.json`, this directory) executed by the Rust
// SDK (crates/consema-conformance/examples/emit_normalized_results.rs) and
// by this Go package. Go never imports or calls Rust (RFC 0016 §1.1 cgo
// ban): the Rust side emits one `<case-id>.txt` evidence file per case, and
// the Go test computes the same normalized facts and compares them field by
// field. Orchestration: scripts/go-verify-normalized-differential.ps1.
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
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/json"
	"consema.dev/consema/protocol"
	"consema.dev/consema/toml"
)

// CaseFileManifest is the frozen manifest id of the differential input set.
const CaseFileManifest = "consema.differential.normalized@1"

// MinCaseCount is the task's lower bound for the input set ("至少 40 个
// case"); the integrity test fails if the checked-in file drops below it.
const MinCaseCount = 40

// RustDirEnv names the directory of the Rust evidence files.
const RustDirEnv = "CONSEMA_DIFFERENTIAL_NORMALIZED_RUST_DIR"

// ---------------------------------------------------------------------------
// Case file schema (data-driven; shared with the Rust example)
// ---------------------------------------------------------------------------

// fileCase is one entry of cases.json.
type fileCase struct {
	ID            string               `json:"id"`
	Kind          string               `json:"kind"` // "document" or "source"
	Format        string               `json:"format,omitempty"`
	Profile       string               `json:"profile,omitempty"`
	Source        string               `json:"source,omitempty"`
	ForeignSource string               `json:"foreign_source,omitempty"`
	ParseLimits   *parseLimitsDesc     `json:"parse_limits,omitempty"`
	Steps         []stepDesc           `json:"steps,omitempty"`
	Input         *sourceInputDesc     `json:"input,omitempty"`
	Request       *encodingRequestDesc `json:"request,omitempty"`
	Positions     []int                `json:"positions,omitempty"`
	Patch         *patchDesc           `json:"patch,omitempty"`
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
	Domain        string         `json:"domain,omitempty"`
	DomainVersion int            `json:"domain_version,omitempty"`
	Filters       []filterDesc   `json:"filters,omitempty"`
	Combine       string         `json:"combine,omitempty"`
	Selection     string         `json:"selection,omitempty"`
	QueryLimits   *queryLimits   `json:"limits,omitempty"`

	// project
	Target         string `json:"target,omitempty"`
	DuplicatePolicy string `json:"duplicate_policy,omitempty"`

	// materialize
	Input         string               `json:"input,omitempty"`
	ValueJSON     string               `json:"value_json,omitempty"`
	EntryMapping  *entryMappingDesc    `json:"entry_mapping,omitempty"`
	TargetProfile string               `json:"target_profile,omitempty"`
	Style         string               `json:"style,omitempty"`
	Newline       string               `json:"newline,omitempty"`
	MatLimits     *materializeLimits   `json:"limits,omitempty"`

	// edit
	Operations []editOpDesc `json:"operations,omitempty"`
}

// filterDesc is one query filter: an operator call on the current input.
type filterDesc struct {
	Operator string          `json:"operator"`
	Argument json.RawMessage `json:"argument,omitempty"`
}

// queryLimits overrides the frozen query limits.
type queryLimits struct {
	MaxResults *int `json:"max_results,omitempty"`
	MaxSteps   *int `json:"max_steps,omitempty"`
}

// entryMappingDesc is a direct EntryMapping input for materialization.
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
	Operation string       `json:"operation"`
	Target    *targetDesc  `json:"target"`
	Value     *valueDesc   `json:"value,omitempty"`
	LiteralHex string     `json:"literal_hex,omitempty"`
	Name       string      `json:"name,omitempty"`
	Policy     string      `json:"policy,omitempty"`
	Placement  *placementDesc `json:"placement,omitempty"`
}

// targetDesc identifies one structural node of the current document.
type targetDesc struct {
	Kind    string `json:"kind"` // root | member | member-value | member-key |
	// entry | entry-item | array-element | array-element-value |
	// array-element-item
	Ordinal int  `json:"ordinal,omitempty"`
	Foreign bool `json:"foreign,omitempty"`
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
	At             string `json:"at,omitempty"` // "start" | "end"
	BeforeOrdinal  *int   `json:"before_ordinal,omitempty"`
	AfterOrdinal   *int   `json:"after_ordinal,omitempty"`
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
	OldStart      int    `json:"old_start"`
	OldEnd        int    `json:"old_end"`
	ReplacementHex string `json:"replacement_hex"`
}

// ---------------------------------------------------------------------------
// Normalized fact emission
// ---------------------------------------------------------------------------

// facts is the ordered key=value fact set of one case. The key set is fixed:
// every document case emits exactly the same keys in the same order, so a
// missing or extra key is itself a differential failure.
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
	for _, character := range text {
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
	}
	return output.String()
}

// join renders one ordered list into the `|`-joined fact vocabulary.
func join(items []string) string { return strings.Join(items, "|") }

// ---------------------------------------------------------------------------
// Document face
// ---------------------------------------------------------------------------

// runDocumentCase executes one document-face case and returns its facts.
func runDocumentCase(c *fileCase) ([]string, error) {
	profile, profileName, err := parseDocumentProfile(c)
	if err != nil {
		return nil, err
	}
	limits := document.DefaultParseLimits()
	applyParseLimits(&limits, c.ParseLimits)

	facts := &facts{}
	state := &docState{}

	// --- parse ---
	parseDoc, failure := parseDocumentSource(c, profile, limits)
	if failure != nil {
		facts.set("parse.formation", "Fatal")
		facts.set("parse.fatal_code", formationFailureCode(failure))
		facts.set("parse.diagnostic_codes", "")
		facts.set("parse.root_kind", "")
		facts.set("parse.native", "")
		emitBlocked(facts)
		return facts.lines, nil
	}
	state.doc = parseDoc
	state.profile = profileName
	facts.set("parse.formation", parseDoc.FormationStatus().String())
	facts.set("parse.fatal_code", "")
	facts.set("parse.diagnostic_codes", diagnosticCodes(parseDoc.Diagnostics()))
	facts.set("parse.root_kind", documentRootKind(parseDoc))
	facts.set("parse.native", documentNativeSummary(parseDoc))

	// --- query steps ---
	for _, step := range c.Steps {
		switch step.Op {
		case "parse":
			// already handled
		case "query-native":
			runNativeQuery(facts, state, &step)
		case "query-syntax":
			runSyntaxQuery(facts, state, &step)
		case "project":
			runProject(facts, state, &step)
		case "materialize":
			runMaterialize(facts, state, &step)
		case "edit":
			runEdit(facts, state, &step)
		default:
			return nil, fmt.Errorf("case %s: unknown step op %q", c.ID, step.Op)
		}
	}
	return facts.lines, nil
}

// emitBlocked fills the fixed fact keys after a fatal parse; every dependent
// step reports Blocked on both sides.
func emitBlocked(facts *facts) {
	for _, key := range []string{
		"query.native.status", "query.native.failure", "query.native.count", "query.native.matches",
		"query.syntax.status", "query.syntax.failure", "query.syntax.count", "query.syntax.matches",
		"project.status", "project.failure", "project.fidelity", "project.value_kind",
		"project.report", "project.provenance_entries",
		"materialize.status", "materialize.failure", "materialize.output", "materialize.fidelity",
		"edit.status", "edit.failure", "edit.output", "edit.source_edit_count",
	} {
		facts.set(key, "")
	}
	// The dependency rule is expressed on the live keys below; the blocked
	// keys are filled there too, so this helper only runs on the fatal path.
	facts.set("query.native.status", "Blocked")
	facts.set("query.syntax.status", "Blocked")
	facts.set("project.status", "Blocked")
	facts.set("materialize.status", "Blocked")
	facts.set("edit.status", "Blocked")
}

// docState is the execution state of one document case.
type docState struct {
	doc       *json.Document
	tomlDoc   *toml.Document
	profile   string
	foreign   *json.Document
	foreignToml *toml.Document
	value     core.Value
	projected bool
}

// parseDocumentSource parses the case source with the selected profile.
func parseDocumentSource(c *fileCase, profile interface{}, limits document.ParseLimits) (interface{}, interface{}) {
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
			return nil, failure
		}
		return doc, nil
	case "toml":
		doc, failure := toml.Parse([]byte(c.Source), toml.TomlProfile10V1(), limits)
		if failure != nil {
			return nil, failure
		}
		return doc, nil
	}
	return nil, nil
}

// formationFailureCode extracts the stable code of a fatal formation.
func formationFailureCode(failure interface{}) string {
	switch f := failure.(type) {
	case *json.FormationFailure:
		return f.Code()
	case *toml.FormationFailure:
		return f.Code()
	}
	return ""
}

// parseDocumentProfile resolves the case profile.
func parseDocumentProfile(c *fileCase) (interface{}, string, error) {
	switch c.Format {
	case "json":
		switch c.Profile {
		case "json.strict@1":
			return json.JsonProfileStrictV1, "json.strict@1", nil
		case "jsonc.bounded@1":
			return json.JsonProfileJsoncBoundedV1, "jsonc.bounded@1", nil
		case "json5.standard@1":
			return json.JsonProfileJson5StandardV1, "json5.standard@1", nil
		}
	case "toml":
		if c.Profile == "toml.1.0@1" {
			return toml.TomlProfile10V1(), "toml.1.0@1", nil
		}
	}
	return nil, "", fmt.Errorf("case %s: unknown format/profile %q/%q", c.ID, c.Format, c.Profile)
}

// applyParseLimits applies the descriptor overrides.
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

// diagnosticCodes renders the ordered diagnostic codes.
func diagnosticCodes(diagnostics []*protocol.Diagnostic) string {
	codes := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		codes = append(codes, diagnostic.Code)
	}
	return join(codes)
}

// documentRootKind renders the root native kind fact.
func documentRootKind(doc interface{}) string {
	switch d := doc.(type) {
	case *json.Document:
		kind := d.Root().Kind()
		if !kind.IsAvailable() {
			return "Unavailable:" + kind.Reason().String()
		}
		return kind.Value().String()
	case *toml.Document:
		return d.Root().Kind().String()
	}
	return ""
}

// documentNativeSummary renders the canonical native text of the root.
func documentNativeSummary(doc interface{}) string {
	switch d := doc.(type) {
	case *json.Document:
		return jsonNativeValue(d.Root(), 0)
	case *toml.Document:
		return tomlNativeItem(d.Root(), 0)
	}
	return ""
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
			name := member.Name()
			renderedName := "?"
			if name.IsAvailable() {
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
		return tomlTableSummary(entries, depth)
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

// tomlTableSummary renders one table's ordered entries.
func tomlTableSummary(entries []toml.TomlEntry, depth int) string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, `"`+escape(entry.Name())+`":`+tomlNativeItem(entry.Item(), depth+1))
	}
	return "{" + strings.Join(parts, ",") + "}"
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
		time := dateTime.Time
		text := fmt.Sprintf("time=%02d:%02d:%02d", time.Hour, time.Minute, time.Second)
		if time.Nanosecond != 0 {
			text += "." + fmt.Sprintf("%09d", time.Nanosecond)
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

// runNativeQuery executes the optional native query step.
func runNativeQuery(facts *facts, state *docState, step *stepDesc) {
	if state.doc == nil && state.tomlDoc == nil {
		facts.set("query.native.status", "Blocked")
		facts.set("query.native.failure", "")
		facts.set("query.native.count", "")
		facts.set("query.native.matches", "")
		return
	}
	domain, err := protocol.NewQueryDomain(step.Domain, uint32(step.DomainVersion)), error(nil)
	if err != nil {
		panic("unreachable")
	}
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
	ctx := context.Background()
	if state.doc != nil {
		matches, queryFailure := json.ExecuteJSONQuery(ctx, executable, state.doc, limits)
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
	matches, queryFailure := toml.ExecuteTomlQuery(ctx, executable, state.tomlDoc, limits)
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
	case toml.TomlMatchKindItem:
		return "I:" + match.ItemKind.String()
	case toml.TomlMatchKindEntry:
		return fmt.Sprintf("M:%d:%s", match.Ordinal, escape(match.Name))
	case toml.TomlMatchKindArrayElement:
		return fmt.Sprintf("E:%d", match.Ordinal)
	}
	return "?"
}

// runSyntaxQuery executes the optional syntax query step.
func runSyntaxQuery(facts *facts, state *docState, step *stepDesc) {
	if state.doc == nil && state.tomlDoc == nil {
		facts.set("query.syntax.status", "Blocked")
		facts.set("query.syntax.failure", "")
		facts.set("query.syntax.count", "")
		facts.set("query.syntax.matches", "")
		return
	}
	domain, err := protocol.NewQueryDomain(step.Domain, uint32(step.DomainVersion)), error(nil)
	if err != nil {
		panic("unreachable")
	}
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
	ctx := context.Background()
	if state.doc != nil {
		matches, queryFailure := json.ExecuteJSONSyntaxQuery(ctx, executable, state.doc, limits)
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
	matches, queryFailure := toml.ExecuteTomlSyntaxQuery(ctx, executable, state.tomlDoc, limits)
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

// buildQueryDefinition builds the executable from the declarative filters,
// mirroring the conformance runner pipeline helpers (v1_json_face.go and the
// Rust syntax_query_v1.rs definition()).
func buildQueryDefinition(step *stepDesc, domain *protocol.QueryDomain) (*protocol.ExecutableQuery, *protocol.QueryFailure) {
	format := "json"
	if strings.HasPrefix(step.Domain, "toml.") {
		format = "toml"
	}
	branches := make([]*protocol.QueryExpression, 0, len(step.Filters))
	for _, filter := range step.Filters {
		var call *protocol.OperatorCall
		switch filter.Operator {
		case "kind-is":
			call = protocol.NewOperatorCall(format+".syntax-kind-is", 1).
				WithArgument("kind", core.String(filter.argumentString()))
		case "text-equals":
			call = protocol.NewOperatorCall(format+".syntax-text-equals", 1).
				WithArgument("text", core.String(filter.argumentString()))
		case "take":
			call = protocol.NewOperatorCall("core.take", 1).
				WithArgument("count", core.NewInteger(big.NewInt(int64(filter.argumentInt()))))
		default:
			call = protocol.NewOperatorCall(filter.Operator, 1)
			if len(filter.Argument) > 0 {
				call = call.WithArgument("name", core.String(filter.argumentString()))
			}
		}
		branches = append(branches, (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).Then(call))
	}
	var expression *protocol.QueryExpression
	switch step.Combine {
	case "Single", "":
		if len(branches) == 0 {
			expression = &protocol.QueryExpression{Kind: protocol.ExpressionInput}
		} else if len(branches) == 1 {
			expression = branches[0]
		} else {
			expression = &protocol.QueryExpression{Kind: protocol.ExpressionConcat, Branches: branches}
		}
	case "StructureOrderMerge":
		expression = &protocol.QueryExpression{Kind: protocol.ExpressionStructureOrderMerge, Branches: branches}
	case "Concat":
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

// argumentString decodes a string filter argument.
func (f *filterDesc) argumentString() string {
	if len(f.Argument) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(f.Argument, &text); err == nil {
		return text
	}
	return ""
}

// argumentInt decodes an integer filter argument.
func (f *filterDesc) argumentInt() int {
	if len(f.Argument) == 0 {
		return 0
	}
	var number int64
	if err := json.Unmarshal(f.Argument, &number); err == nil {
		return int(number)
	}
	return 0
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

// runProject executes the optional projection step.
func runProject(facts *facts, state *docState, step *stepDesc) {
	if state.doc == nil && state.tomlDoc == nil {
		facts.set("project.status", "Blocked")
		facts.set("project.failure", "")
		facts.set("project.fidelity", "")
		facts.set("project.value_kind", "")
		facts.set("project.report", "")
		facts.set("project.provenance_entries", "")
		return
	}
	if state.doc != nil {
		request, buildFailure := buildJSONProjectionRequest(state, step)
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
			facts.set("project.report", projectionReportSummary(result.Failed.Report))
			facts.set("project.provenance_entries", "")
			return
		}
		state.value = result.Complete.Value
		state.projected = true
		facts.set("project.status", "Completed")
		facts.set("project.failure", "")
		facts.set("project.fidelity", result.Complete.Fidelity.String())
		facts.set("project.value_kind", neutralKindName(result.Complete.Value.Kind()))
		facts.set("project.report", projectionReportSummary(result.Complete.Report))
		facts.set("project.provenance_entries", strconv.Itoa(len(result.Complete.Provenance.Entries())))
		return
	}
	request := buildTomlProjectionRequest(step)
	result := state.tomlDoc.Project(request)
	if result.Failed != nil {
		facts.set("project.status", "Failed")
		facts.set("project.failure", result.Failed.Diagnostics[0].Code)
		facts.set("project.fidelity", "")
		facts.set("project.value_kind", "")
		facts.set("project.report", projectionReportSummary(result.Failed.Report))
		facts.set("project.provenance_entries", "")
		return
	}
	state.value = result.Complete.Value
	state.projected = true
	facts.set("project.status", "Completed")
	facts.set("project.failure", "")
	facts.set("project.fidelity", result.Complete.Fidelity.String())
	facts.set("project.value_kind", neutralKindName(result.Complete.Value.Kind()))
	facts.set("project.report", projectionReportSummary(result.Complete.Report))
	facts.set("project.provenance_entries", strconv.Itoa(len(result.Complete.Provenance.Entries())))
}

// buildJSONProjectionRequest builds the JSON projection request from the
// descriptor.
func buildJSONProjectionRequest(state *docState, step *stepDesc) (*json.ProjectionRequest, *json.ProjectionFailure) {
	var target json.ProjectionTarget
	switch step.Target {
	case "BestExactCore":
		target = json.ProjectionTargetBestExactCoreV1
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
	var target toml.ProjectionTarget
	switch step.Target {
	case "ProjectAsObject":
		target = toml.ProjectionTargetProjectAsObject
	default:
		target = toml.ProjectionTargetBestExactCore
	}
	return toml.NewProjectionRequest(target)
}

// neutralKindName maps one core kind to the language-neutral kind
// vocabulary (the array kind is "Sequence" on the PVCE surface).
func neutralKindName(kind core.Kind) string {
	if kind == core.KindArray {
		return "Sequence"
	}
	return kind.String()
}

// projectionReportSummary renders the report as ordered EventKind:count
// pairs.
func projectionReportSummary(report json.ProjectionReport) string {
	return eventSummary(report.Events())
}

func eventSummary(events []json.ProjectionEvent) string {
	order := make([]string, 0)
	counts := make(map[string]int)
	for _, event := range events {
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

// runMaterialize executes the optional materialization step.
func runMaterialize(facts *facts, state *docState, step *stepDesc) {
	var value core.Value
	switch step.Input {
	case "", "project":
		if !state.projected {
			facts.set("materialize.status", "Blocked")
			facts.set("materialize.failure", "")
			facts.set("materialize.output", "")
			facts.set("materialize.fidelity", "")
			return
		}
		value = state.value
	case "value":
		if step.EntryMapping != nil {
			key, err := protocol.DecodeJSON([]byte(step.EntryMapping.KeyJSON), protocol.DefaultProtocolLimits())
			if err != nil {
				return
			}
			mapped, err := protocol.DecodeJSON([]byte(step.EntryMapping.ValueJSON), protocol.DefaultProtocolLimits())
			if err != nil {
				return
			}
			builder := core.NewEntryMappingBuilder()
			if err := builder.Push(key, mapped); err != nil {
				return
			}
			value = builder.Build()
		} else {
			decoded, err := protocol.DecodeJSON([]byte(step.ValueJSON), protocol.DefaultProtocolLimits())
			if err != nil {
				return
			}
			value = decoded
		}
	default:
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
		emitMaterializationFacts(facts, result.Complete != nil, func() (string, string) {
			complete := result.Complete
			return string(complete.Document.Render()), complete.Fidelity.String()
		}, func() string {
			return result.Failed.Failure.Code()
		})
		return
	}
	result := toml.Materialize(value, request)
	emitMaterializationFacts(facts, result.Complete != nil, func() (string, string) {
		complete := result.Complete
		return string(complete.Document.Render()), complete.Fidelity.String()
	}, func() string {
		return result.Failed.Failure.Code()
	})
}

// emitMaterializationFacts writes the materialization fact keys.
func emitMaterializationFacts(facts *facts, completed bool, complete func() (string, string), failedCode func() string) {
	if completed {
		output, fidelity := complete()
		facts.set("materialize.status", "Completed")
		facts.set("materialize.failure", "")
		facts.set("materialize.output", escape(output))
		facts.set("materialize.fidelity", fidelity)
		return
	}
	facts.set("materialize.status", "Failed")
	facts.set("materialize.failure", failedCode())
	facts.set("materialize.output", "")
	facts.set("materialize.fidelity", "")
}

// buildMaterializationRequest builds the request from the descriptor.
func buildMaterializationRequest(step *stepDesc) (document.MaterializationRequest, bool) {
	if step.TargetProfile == "" || step.Style == "" {
		return document.MaterializationRequest{}, false
	}
	request := document.NewMaterializationRequest(
		document.NewProfileId(strings.SplitN(step.TargetProfile, "@", 2)[0], 1),
		document.NewMaterializationStyleId(strings.SplitN(step.Style, "@", 2)[0], 1),
	)
	switch step.Newline {
	case "None":
		request = request.WithNewline(document.NewlineNone)
	case "Lf":
		request = request.WithNewline(document.NewlineLf)
	case "CrLf":
		request = request.WithNewline(document.NewlineCrLf)
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

// runEdit executes the optional edit step (one atomic transaction).
func runEdit(facts *facts, state *docState, step *stepDesc) {
	if state.doc == nil && state.tomlDoc == nil {
		facts.set("edit.status", "Blocked")
		facts.set("edit.failure", "")
		facts.set("edit.output", "")
		facts.set("edit.source_edit_count", "")
		return
	}
	if state.doc != nil {
		builder := json.NewEditTransactionBuilder(state.doc)
		if !applyJSONEditOperations(builder, state, step) {
			facts.set("edit.status", "Blocked")
			facts.set("edit.failure", "")
			facts.set("edit.output", "")
			facts.set("edit.source_edit_count", "")
			return
		}
		commit, editFailure := state.doc.Commit(builder.Build())
		emitEditFacts(facts, commit != nil, func() (string, int) {
			return string(commit.Document.Render()), len(commit.ChangeSet.SourceEdits())
		}, func() string {
			return editFailure.Code()
		})
		return
	}
	builder := toml.NewEditTransactionBuilder(state.tomlDoc)
	if !applyTomlEditOperations(builder, state, step) {
		facts.set("edit.status", "Blocked")
		facts.set("edit.failure", "")
		facts.set("edit.output", "")
		facts.set("edit.source_edit_count", "")
		return
	}
	commit, editFailure := state.tomlDoc.Commit(builder.Build())
	emitEditFacts(facts, commit != nil, func() (string, int) {
		return string(commit.Document.Render()), len(commit.ChangeSet.SourceEdits())
	}, func() string {
		return editFailure.Code()
	})
}

// emitEditFacts writes the edit fact keys.
func emitEditFacts(facts *facts, completed bool, complete func() (string, int), failedCode func() string) {
	if completed {
		output, count := complete()
		facts.set("edit.status", "Completed")
		facts.set("edit.failure", "")
		facts.set("edit.output", escape(output))
		facts.set("edit.source_edit_count", strconv.Itoa(count))
		return
	}
	facts.set("edit.status", "Failed")
	facts.set("edit.failure", failedCode())
	facts.set("edit.output", "")
	facts.set("edit.source_edit_count", "")
}

// applyJSONEditOperations applies the declared operations to the builder;
// false means a target could not be resolved (harness bug surface).
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

// resolveJSONTarget resolves one target descriptor to a JSON node handle.
func resolveJSONTarget(state *docState, target *targetDesc) (document.NodeRef, bool) {
	doc := state.doc
	if target.Foreign {
		doc = state.foreign
	}
	if doc == nil {
		if target.Foreign {
			return document.NodeRef{}, false
		}
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
		doc = state.foreignToml
	}
	if doc == nil {
		return document.NodeRef{}, false
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

// jsonOrdinalAnchor resolves the anchor of the current container: for
// insert-member the members, for insert-array-element the elements.
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

// tomlOrdinalAnchor resolves the anchor of the current TOML container.
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
		return toml.RepresentationPolicyPreserveCompatible, true
	case "CanonicalForProfile":
		return toml.RepresentationPolicyCanonicalForProfile, true
	case "PreserveElseCanonical":
		return toml.RepresentationPolicyPreserveElseCanonical, true
	case "ExactLiteral":
		return toml.RepresentationPolicyExactLiteral, true
	}
	return toml.RepresentationPolicyExactLiteral, false
}

// coreValue builds one core.Value from a scalar descriptor.
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
