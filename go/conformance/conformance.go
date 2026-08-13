// Package conformance implements the Go conformance runner over the shared
// language-neutral vectors (RFC 0016 §7; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
// §4). One runner file per suite family mirrors
// consema-rs/consema-conformance/src/lib.rs:3-25; every runner validates the
// suite identifier, rejects duplicate case IDs, asserts the frozen case
// count, dispatches cases by capability, and rejects unknown cases. The
// vector files themselves are the authority for content — the runner
// embeds no vector copy, while it pins the frozen per-suite case counts
// and verifies the aggregate digest (conformance/README.md rule 4 requires
// every suite to assert its case count; G146, adversarial audit
// 2026-08-13 — "holds no expectation literals" overstated the in-runner
// count pins; the go:embed boundary would create a second authority
// source).
//
// Cases whose capability is not implemented by the current Go milestone
// are documented skips (never silent; RFC 0016 §7): the skip record names
// the capability and the reason, and the count still participates in the
// suite-level case-count assertion.
package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"consema.dev/consema/core"
)

// SkipRecord is one documented skip: the case was not executed because its
// capability is not implemented by this Go milestone.
type SkipRecord struct {
	// ID is the stable case identifier.
	ID string
	// Capability is the declared mandatory capability.
	Capability string
	// Reason explains why the capability is not implemented.
	Reason string
}

// CaseFailure is one failed case with its message.
type CaseFailure struct {
	// ID is the stable case identifier.
	ID string
	// Message is the failure description.
	Message string
}

// SuiteReport is the run report of one vector suite, mirroring the Rust
// ConformanceReport shape plus the documented skips.
type SuiteReport struct {
	// Suite is the suite identifier read from the vector file.
	Suite string
	// SemanticModel is the semantic-model identifier read from the vector
	// file, when the suite carries one.
	SemanticModel string
	// ExpectedCases is the frozen case count assertion.
	ExpectedCases int
	// Passed are the stable passing case IDs.
	Passed []string
	// Skipped are the documented skips (success; never silent).
	Skipped []SkipRecord
	// Failed are the stable case IDs and failure descriptions.
	Failed []CaseFailure
}

// CountAsserted reports whether the frozen case count matched the vector
// file (conformance/README.md rule 4).
func (s *SuiteReport) CountAsserted() bool {
	return s.ExpectedCases == len(s.Passed)+len(s.Skipped)+len(s.Failed)
}

// Conformant reports whether every executed case passed and the count
// assertion held (documented skips count as success).
func (s *SuiteReport) Conformant() bool {
	return len(s.Failed) == 0 && s.CountAsserted()
}

// Runner executes the shared vector suites from explicit repository paths.
type Runner struct {
	// VectorsDir is the repository `conformance/vectors` directory.
	VectorsDir string
	// FixturesDir is the repository `conformance/fixtures` directory.
	FixturesDir string
	// ManifestPath is the Feature-Complete Manifest whose conformance_suite
	// record pins the aggregate digest.
	ManifestPath string
}

// DigestResult is the aggregate vector digest verification
// (https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §4.5; fc-manifest conformance_suite).
type DigestResult struct {
	// OK reports whether the computed aggregate matches the manifest.
	OK bool
	// Computed is the computed aggregate sha256.
	Computed string
	// Recorded is the manifest-recorded aggregate sha256.
	Recorded string
	// Suites is the counted vector file count.
	Suites int
	// Cases is the counted total case count.
	Cases int
}

// RunReport is the complete conformance run result.
type RunReport struct {
	// Digest is the aggregate digest verification result.
	Digest DigestResult
	// Suites are the per-suite reports in vector inventory order.
	Suites []*SuiteReport
	// Total is the total case count across suites.
	Total int
	// Passed is the total passing case count.
	Passed int
	// Skipped is the total documented skip count.
	Skipped int
	// Failed is the total failing case count.
	Failed int
}

// Conformant reports whether every applicable case passed, every count
// assertion held, and the aggregate digest matched the manifest.
func (r *RunReport) Conformant() bool {
	if !r.Digest.OK {
		return false
	}
	for _, suite := range r.Suites {
		if !suite.Conformant() {
			return false
		}
	}
	return true
}

// Run executes every shared vector suite and verifies the aggregate digest.
func (r *Runner) Run() (*RunReport, error) {
	digest, err := r.VerifyVectorsDigest()
	if err != nil {
		return nil, err
	}
	suites := make([]*SuiteReport, 0, len(allSuites))
	for _, definition := range allSuites {
		suites = append(suites, r.runSuite(definition))
	}
	report := &RunReport{Digest: digest, Suites: suites}
	for _, suite := range suites {
		report.Total += len(suite.Passed) + len(suite.Skipped) + len(suite.Failed)
		report.Passed += len(suite.Passed)
		report.Skipped += len(suite.Skipped)
		report.Failed += len(suite.Failed)
	}
	return report, nil
}

// suiteDefinition describes one frozen vector suite.
type suiteDefinition struct {
	// File is the vector file basename.
	File string
	// SuiteID is the frozen suite identifier.
	SuiteID string
	// SemanticModel is the required semantic-model identifier; empty when
	// the suite carries none.
	SemanticModel string
	// ExpectedCases is the frozen case count (fc-manifest 519 inventory).
	ExpectedCases int
	// Run executes the suite.
	Run func(*Runner, *suiteData) *SuiteReport
}

// allSuites is the frozen 18-suite inventory in the fc-manifest order
// (https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §4.2 table; case counts re-pinned by the
// digest check against the manifest).
var allSuites = []suiteDefinition{
	{File: "v1.json", SuiteID: "consema.conformance@1", ExpectedCases: 30, Run: runV1},
	{File: "toml-v1.json", SuiteID: "consema.toml.conformance@1", ExpectedCases: 18, Run: runTomlV1},
	{File: "protocol-v1.json", SuiteID: "consema.protocol.conformance@1", ExpectedCases: 32, Run: runProtocolV1},
	{File: "source-v1.json", SuiteID: "consema.source.conformance@1", ExpectedCases: 28, Run: runSourceV1},
	{File: "syntax-query-v1.json", SuiteID: "consema.syntax-query.conformance@1", ExpectedCases: 19, Run: runSyntaxQueryV1},
	{File: "protocol-v2.json", SuiteID: "consema.protocol.conformance@2", SemanticModel: "core.semantic-model@2", ExpectedCases: 11, Run: runProtocolV2},
	{File: "operations-v1.json", SuiteID: "consema.operations.conformance@1", ExpectedCases: 35, Run: runOperationsV1},
	{File: "json-family-v2.json", SuiteID: "consema.json-family.conformance@2", ExpectedCases: 33, Run: runJsonFamilyV2},
	{File: "portable-graph-v1.json", SuiteID: "consema.portable-graph.conformance@1", ExpectedCases: 10, Run: runPortableGraphV1},
	{File: "semantic-model-v5.json", SuiteID: "consema.semantic-model-v5.conformance@1", SemanticModel: "core.semantic-model@5", ExpectedCases: 22, Run: runSemanticModelV5},
	{File: "yaml-v1.json", SuiteID: "consema.yaml.conformance@1", ExpectedCases: 31, Run: runYamlV1},
	{File: "semantic-model-v6.json", SuiteID: "consema.semantic-model-v6.conformance@1", SemanticModel: "core.semantic-model@6", ExpectedCases: 25, Run: runSemanticModelV6},
	{File: "ini-v1.json", SuiteID: "consema.ini.conformance@1", ExpectedCases: 20, Run: runIniV1},
	{File: "java-properties-v1.json", SuiteID: "consema.java-properties.conformance@1", ExpectedCases: 25, Run: runJavaPropertiesV1},
	{File: "xml-1-0-safe-v1.json", SuiteID: "consema.xml-1-0-safe.conformance@1", ExpectedCases: 34, Run: runXml10SafeV1},
	{File: "plist-v1.json", SuiteID: "consema.plist.conformance@1", ExpectedCases: 49, Run: runPlistV1},
	{File: "hcl-v1.json", SuiteID: "consema.hcl.conformance@1", ExpectedCases: 57, Run: runHclV1},
	{File: "cli-v1.json", SuiteID: "consema.cli.conformance@1", ExpectedCases: 40, Run: runCLIV1},
}

// caseData is one loaded vector case.
type caseData struct {
	// ID is the stable case identifier.
	ID string
	// Capability is the declared mandatory capability.
	Capability string
	// Contract is the declared contract of construction-based suites
	// (protocol-v1); empty otherwise.
	Contract string
	// Input is the operation input facts.
	Input core.Value
	// Expected is the public expectation facts.
	Expected core.Value
	// Index is the zero-based case ordinal in the vector file.
	Index int
}

// suiteData is one loaded vector suite.
type suiteData struct {
	// Suite is the suite identifier from the vector file.
	Suite string
	// SemanticModel is the semantic-model identifier, when present.
	SemanticModel string
	// Cases are the loaded cases in file order.
	Cases []caseData
}

// runSuite loads and runs one vector suite with the fixed validations:
// suite identifier, semantic-model identifier, case-ID uniqueness, case
// count, capability dispatch, and unknown-case rejection.
func (r *Runner) runSuite(definition suiteDefinition) *SuiteReport {
	data, loadError := r.loadSuite(definition)
	if loadError != "" {
		return &SuiteReport{
			Suite:         definition.SuiteID,
			ExpectedCases: definition.ExpectedCases,
			Failed:        []CaseFailure{{ID: "suite.parse", Message: loadError}},
		}
	}
	report := &SuiteReport{
		Suite:         data.Suite,
		SemanticModel: data.SemanticModel,
		ExpectedCases: definition.ExpectedCases,
	}
	if data.Suite != definition.SuiteID ||
		(definition.SemanticModel != "" && data.SemanticModel != definition.SemanticModel) {
		report.Failed = append(report.Failed, CaseFailure{
			ID:      "suite.schema",
			Message: "unexpected suite or semantic-model identifier",
		})
		return report
	}
	seen := make(map[string]bool, len(data.Cases))
	for _, vector := range data.Cases {
		if seen[vector.ID] {
			report.Failed = append(report.Failed, CaseFailure{
				ID:      vector.ID,
				Message: "duplicate case id",
			})
			continue
		}
		seen[vector.ID] = true
	}
	if definition.ExpectedCases != len(data.Cases) {
		report.Failed = append(report.Failed, CaseFailure{
			ID: "suite.count",
			Message: fmt.Sprintf("case count changed: expected %d, found %d",
				definition.ExpectedCases, len(data.Cases)),
		})
		// The count assertion fails the suite; the cases still run so the
		// report carries the full case detail.
	}
	definition.Run(r, data).mergeInto(report)
	return report
}

func (s *SuiteReport) mergeInto(target *SuiteReport) {
	target.Passed = append(target.Passed, s.Passed...)
	target.Skipped = append(target.Skipped, s.Skipped...)
	target.Failed = append(target.Failed, s.Failed...)
}

// loadSuite reads one vector file, decodes it as strict JSON, and extracts
// the suite facts. The vector files are plain strict JSON documents, not
// core.portable-value-json@1 transport envelopes; the protocol canonical
// decoder is not applicable here.
func (r *Runner) loadSuite(definition suiteDefinition) (*suiteData, string) {
	path := filepath.Join(r.VectorsDir, definition.File)
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err.Error()
	}
	value, err := parseVectorJSON(bytes)
	if err != nil {
		return nil, "vector file is not strict JSON: " + err.Error()
	}
	root, ok := value.(*core.Object)
	if !ok {
		return nil, "vector root must be an Object"
	}
	data := &suiteData{}
	if suiteValue, ok := root.Get("suite"); ok {
		text, ok := suiteValue.(core.String)
		if !ok {
			return nil, "suite field must be a String"
		}
		data.Suite = string(text)
	}
	if modelValue, ok := root.Get("semantic_model"); ok {
		text, ok := modelValue.(core.String)
		if !ok {
			return nil, "semantic_model field must be a String"
		}
		data.SemanticModel = string(text)
	}
	casesValue, ok := root.Get("cases")
	if !ok {
		return nil, "cases field is absent"
	}
	cases, ok := casesValue.(*core.Array)
	if !ok {
		return nil, "cases field must be a Sequence"
	}
	for index, item := range cases.Items() {
		caseObject, ok := item.(*core.Object)
		if !ok {
			return nil, fmt.Sprintf("case %d must be an Object", index)
		}
		vector := caseData{Index: index}
		if idValue, ok := caseObject.Get("id"); ok {
			text, ok := idValue.(core.String)
			if !ok {
				return nil, fmt.Sprintf("case %d id must be a String", index)
			}
			vector.ID = string(text)
		}
		if capabilityValue, ok := caseObject.Get("capability"); ok {
			text, ok := capabilityValue.(core.String)
			if !ok {
				return nil, fmt.Sprintf("case %d capability must be a String", index)
			}
			vector.Capability = string(text)
		}
		if contractValue, ok := caseObject.Get("contract"); ok {
			text, ok := contractValue.(core.String)
			if !ok {
				return nil, fmt.Sprintf("case %d contract must be a String", index)
			}
			vector.Contract = string(text)
		}
		if inputValue, ok := caseObject.Get("input"); ok {
			vector.Input = inputValue
		}
		if expectedValue, ok := caseObject.Get("expected"); ok {
			vector.Expected = expectedValue
		}
		data.Cases = append(data.Cases, vector)
	}
	return data, ""
}

// objectField returns one named field of an Object value.
func objectField(value core.Value, name string) (core.Value, bool) {
	object, ok := value.(*core.Object)
	if !ok {
		return nil, false
	}
	return object.Get(name)
}

// stringField reads one String field.
func stringField(value core.Value, name string) (string, bool) {
	field, ok := objectField(value, name)
	if !ok {
		return "", false
	}
	text, ok := field.(core.String)
	if !ok {
		return "", false
	}
	return string(text), true
}

// booleanField reads one Boolean field.
func booleanField(value core.Value, name string) (bool, bool) {
	field, ok := objectField(value, name)
	if !ok {
		return false, false
	}
	boolean, ok := field.(core.Boolean)
	if !ok {
		return false, false
	}
	return bool(boolean), true
}

// integerField reads one Integer field.
func integerField(value core.Value, name string) (uint64, bool) {
	field, ok := objectField(value, name)
	if !ok {
		return 0, false
	}
	integer, ok := field.(core.Integer)
	if !ok {
		return 0, false
	}
	number := integer.Int()
	if number.Sign() < 0 || number.BitLen() > 64 {
		return 0, false
	}
	return number.Uint64(), true
}

// sequenceField reads one Sequence field.
func sequenceField(value core.Value, name string) ([]core.Value, bool) {
	field, ok := objectField(value, name)
	if !ok {
		return nil, false
	}
	array, ok := field.(*core.Array)
	if !ok {
		return nil, false
	}
	return array.Items(), true
}

// caseInput reads one named input field.
func caseInput(vector *caseData, name string) (core.Value, bool) {
	return objectField(vector.Input, name)
}

// caseExpected reads one named expected field.
func caseExpected(vector *caseData, name string) (core.Value, bool) {
	return objectField(vector.Expected, name)
}

// VerifyVectorsDigest computes the aggregate sha256 of the vector files and
// compares it against the Feature-Complete Manifest conformance_suite
// record (https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §4.5; fc-manifest
// conformance_suite.note): file-name byte-order sort, per-file sha256
// lowercase hex, lines `{basename}:{digest}` joined with '\n' without a
// trailing newline, then sha256 of that UTF-8 string.
func (r *Runner) VerifyVectorsDigest() (DigestResult, error) {
	entries, err := os.ReadDir(r.VectorsDir)
	if err != nil {
		return DigestResult{}, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	var builder strings.Builder
	totalCases := 0
	for index, name := range names {
		bytes, err := os.ReadFile(filepath.Join(r.VectorsDir, name))
		if err != nil {
			return DigestResult{}, err
		}
		count, err := countCases(bytes)
		if err != nil {
			return DigestResult{}, fmt.Errorf("%s: %w", name, err)
		}
		totalCases += count
		digest := sha256.Sum256(bytes)
		builder.WriteString(name)
		builder.WriteString(":")
		builder.WriteString(hex.EncodeToString(digest[:]))
		if index+1 < len(names) {
			builder.WriteString("\n")
		}
	}
	aggregate := sha256.Sum256([]byte(builder.String()))
	recorded, recordedSuites, recordedCases, err := manifestConformanceSuite(r.ManifestPath)
	if err != nil {
		return DigestResult{}, err
	}
	computed := hex.EncodeToString(aggregate[:])
	result := DigestResult{
		OK:       computed == recorded && len(names) == recordedSuites && totalCases == recordedCases,
		Computed: computed,
		Recorded: recorded,
		Suites:   len(names),
		Cases:    totalCases,
	}
	return result, nil
}

// parseVectorJSON parses one vector file as strict JSON into the core
// value model (encoding/json with exact number preservation; the vector
// files are the authority and never go through the canonical transport
// decoder).
func parseVectorJSON(bytes []byte) (core.Value, error) {
	decoder := json.NewDecoder(strings.NewReader(string(bytes)))
	decoder.UseNumber()
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, fmt.Errorf("trailing content after the root document")
	}
	return convertJSONValue(raw)
}

// convertJSONValue converts one decoded strict-JSON value into the core
// value model.
func convertJSONValue(raw any) (core.Value, error) {
	switch item := raw.(type) {
	case nil:
		return core.NullValue(), nil
	case bool:
		return core.Boolean(item), nil
	case string:
		return core.String(item), nil
	case json.Number:
		return numberTextValue(item.String())
	case []any:
		items := make([]core.Value, 0, len(item))
		for _, element := range item {
			converted, err := convertJSONValue(element)
			if err != nil {
				return nil, err
			}
			items = append(items, converted)
		}
		return core.NewArray(items...), nil
	case map[string]any:
		keys := make([]string, 0, len(item))
		for key := range item {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		entries := make([]core.Entry, 0, len(keys))
		for _, key := range keys {
			converted, err := convertJSONValue(item[key])
			if err != nil {
				return nil, err
			}
			entries = append(entries, core.Entry{Key: key, Value: converted})
		}
		return core.NewObject(entries...)
	}
	return nil, fmt.Errorf("unsupported JSON value %T", raw)
}

// numberTextValue converts one JSON number text into the core value model:
// exact integers become Integer; non-integral spellings become the exact
// canonical Decimal (the vector README string-representation discipline
// applies to wire facts; a few deferred-suite files carry plain JSON
// floats, which the loader keeps exact).
func numberTextValue(text string) (core.Value, error) {
	if integer, ok := new(big.Int).SetString(text, 10); ok {
		return core.NewInteger(integer), nil
	}
	coefficientText := text
	exponent := big.NewInt(0)
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		exponentText := text[index+1:]
		coefficientText = text[:index]
		parsed, ok := new(big.Int).SetString(exponentText, 10)
		if !ok {
			return nil, fmt.Errorf("vector number %q is not an exact value", text)
		}
		exponent = parsed
	}
	scale := big.NewInt(0)
	if index := strings.IndexByte(coefficientText, '.'); index >= 0 {
		fraction := coefficientText[index+1:]
		coefficientText = coefficientText[:index] + fraction
		scale = big.NewInt(int64(-len(fraction)))
	}
	coefficient, ok := new(big.Int).SetString(coefficientText, 10)
	if !ok {
		return nil, fmt.Errorf("vector number %q is not an exact value", text)
	}
	exponent.Add(exponent, scale)
	return core.NewDecimal(coefficient, exponent), nil
}

// countCases counts the cases array of one vector file.
func countCases(bytes []byte) (int, error) {
	value, err := parseVectorJSON(bytes)
	if err != nil {
		return 0, fmt.Errorf("vector file is not strict JSON: %w", err)
	}
	root, ok := value.(*core.Object)
	if !ok {
		return 0, fmt.Errorf("vector root must be an Object")
	}
	cases, ok := root.Get("cases")
	if !ok {
		return 0, fmt.Errorf("cases field is absent")
	}
	array, ok := cases.(*core.Array)
	if !ok {
		return 0, fmt.Errorf("cases field must be a Sequence")
	}
	return len(array.Items()), nil
}

// manifestConformanceSuite reads the conformance_suite record of the
// Feature-Complete Manifest.
func manifestConformanceSuite(path string) (aggregate string, suites, cases int, err error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", 0, 0, err
	}
	var manifest struct {
		Digests struct {
			ConformanceSuite struct {
				Suites          int    `json:"suites"`
				Cases           int    `json:"cases"`
				AggregateSHA256 string `json:"aggregate_sha256"`
			} `json:"conformance_suite"`
		} `json:"digests"`
	}
	if err := json.Unmarshal(bytes, &manifest); err != nil {
		return "", 0, 0, fmt.Errorf("manifest is not strict JSON: %w", err)
	}
	if manifest.Digests.ConformanceSuite.AggregateSHA256 == "" {
		return "", 0, 0, fmt.Errorf("manifest conformance_suite record is absent")
	}
	return manifest.Digests.ConformanceSuite.AggregateSHA256,
		manifest.Digests.ConformanceSuite.Suites, manifest.Digests.ConformanceSuite.Cases, nil
}

// DefaultManifestPath derives the manifest path from the vectors directory
// (repository layout: conformance/vectors -> docs/fc-manifest-0.13.0.json).
func DefaultManifestPath(vectorsDir string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(vectorsDir)), "docs", "fc-manifest-0.13.0.json")
}
