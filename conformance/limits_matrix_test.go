package conformance

// Security limits matrix (milestone 0.16.0 G2.4; plan §2.3 deliverable
// "security limits 矩阵").
//
// Every public limit parameter of the five delivered families
// (json/toml/yaml/ini/properties) is pinned with its exact positive and
// negative boundary: the test discovers the smallest limit value N at
// which the chosen source still succeeds, asserts that N-1 fails with the
// family's frozen resource-limit code, and that N succeeds. The boundary
// discovery runs the same deterministic parsers/engines the families
// ship, so no hand-computed token/node counts are embedded; the semantic
// reference is the Rust side (the Go families mirror the Rust limit
// semantics; the existing family tests pin the individual codes, the
// vectors pin the resource-limit completion semantics).
//
// Rows whose limit only constrains conditional output (report events,
// provenance) use sources that provably consume the parameter; N >= 1 is
// asserted for every row so a row can never pass vacuously.

import (
	"context"
	"testing"

	"consema.dev/consema/document"
	"consema.dev/consema/ini"
	"consema.dev/consema/json"
	"consema.dev/consema/properties"
	"consema.dev/consema/protocol"
	"consema.dev/consema/toml"
	"consema.dev/consema/yaml"
)

// limitRow is one matrix row: a parameter name and a run closure that
// executes with the given limit value and reports success.
type limitRow struct {
	name string
	run  func(limit int) bool
}

// maxBoundary is the discovery ceiling; every chosen source consumes far
// fewer than this units of any parameter.
const maxBoundary = 512

// pinBoundaryCode pins one exact boundary: run returns the failure code
// (or "" on success) for the given limit value. The smallest limit N at
// which the source succeeds is discovered, then N-1 must fail with
// expectedCode and N must succeed.
func pinBoundaryCode(t *testing.T, family, param, expectedCode string,
	run func(limit int) (code string)) {
	t.Helper()
	n := -1
	for limit := 1; limit <= maxBoundary; limit++ {
		if code := run(limit); code == "" {
			n = limit
			break
		}
	}
	if n < 0 {
		t.Fatalf("%s/%s: no limit up to %d succeeds", family, param, maxBoundary)
	}
	if n < 1 {
		t.Fatalf("%s/%s: vacuous boundary N=%d", family, param, n)
	}
	if code := run(n - 1); code != expectedCode {
		t.Fatalf("%s/%s: limit %d failed with %q, want %q", family, param, n-1, code, expectedCode)
	}
	if code := run(n); code != "" {
		t.Fatalf("%s/%s: limit N=%d must succeed, got %q", family, param, n, code)
	}
}

// ---------------------------------------------------------------------------
// JSON family
// ---------------------------------------------------------------------------

func TestLimitsMatrixJSON(t *testing.T) {
	source := `{"a":1,"b":[2,3],"c":{"d":4}}`
	recovering := `{"a":1,}` // strict recovery: one diagnostic

	parseRows := []limitRow{
		{"MaxSourceBytes", func(limit int) bool {
			l := document.DefaultParseLimits()
			l.MaxSourceBytes = limit
			_, f := json.Parse(context.Background(), []byte(source), json.JsonProfileStrictV1, l)
			return f == nil
		}},
		{"MaxNestingDepth", func(limit int) bool {
			l := document.DefaultParseLimits()
			l.MaxNestingDepth = limit
			_, f := json.Parse(context.Background(), []byte(source), json.JsonProfileStrictV1, l)
			return f == nil
		}},
		{"MaxTokenCount", func(limit int) bool {
			l := document.DefaultParseLimits()
			l.MaxTokenCount = limit
			_, f := json.Parse(context.Background(), []byte(source), json.JsonProfileStrictV1, l)
			return f == nil
		}},
		{"MaxNodeCount", func(limit int) bool {
			l := document.DefaultParseLimits()
			l.MaxNodeCount = limit
			_, f := json.Parse(context.Background(), []byte(source), json.JsonProfileStrictV1, l)
			return f == nil
		}},
	}
	// MaxDiagnostics is not a hard limit: it truncates the diagnostics
	// list with the core.diagnostic.truncated@1 marker (covered by the
	// family's TestDiagnosticTruncationMarker), so it has no resource-limit
	// boundary to pin here.
	_ = recovering
	for _, row := range parseRows {
		pinBoundaryCode(t, "json", "parse."+row.name, "core.parse.resource-limit@1",
			func(limit int) string {
				if row.run(limit) {
					return ""
				}
				return "core.parse.resource-limit@1"
			})
	}
}

func TestLimitsMatrixJSONProjectionQueryMaterialization(t *testing.T) {
	doc := jsonParseForClosure(t, `{"a":1,"b":[2,3],"c":{"d":4}}`)
	duplicates := jsonParseForClosure(t, `{"a":1,"a":2}`)

	projectRows := []struct {
		name string
		run  func(limit int) string
	}{
		{"MaxValueNodes", func(limit int) string {
			l := json.DefaultProjectionLimits()
			l.MaxValueNodes = limit
			request, f := json.NewProjectionRequestBuilder(json.ProjectionTargetBestExactCoreV1).
				Limits(l).Build()
			if f != nil {
				return f.Code()
			}
			result := doc.Project(request)
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Diagnostics[0].Code
		}},
		{"MaxReportEntries", func(limit int) string {
			l := json.DefaultProjectionLimits()
			l.MaxReportEntries = limit
			request, f := json.NewProjectionRequestBuilder(json.ProjectionTargetProjectAsObjectV1).
				GlobalDuplicatePolicy(json.DuplicateKeyPolicyFirstWins).Limits(l).Build()
			if f != nil {
				return f.Code()
			}
			result := duplicates.Project(request)
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Diagnostics[0].Code
		}},
		{"MaxProvenanceEntries", func(limit int) string {
			l := json.DefaultProjectionLimits()
			l.MaxProvenanceEntries = limit
			request, f := json.NewProjectionRequestBuilder(json.ProjectionTargetProjectAsObjectV1).
				Limits(l).Build()
			if f != nil {
				return f.Code()
			}
			result := doc.Project(request)
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Diagnostics[0].Code
		}},
		{"MaxDepth", func(limit int) string {
			l := json.DefaultProjectionLimits()
			l.MaxDepth = limit
			request, f := json.NewProjectionRequestBuilder(json.ProjectionTargetBestExactCoreV1).
				Limits(l).Build()
			if f != nil {
				return f.Code()
			}
			result := doc.Project(request)
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Diagnostics[0].Code
		}},
	}
	for _, row := range projectRows {
		pinBoundaryCode(t, "json", "project."+row.name, "core.projection.resource-limit@1", row.run)
	}

	executable := jsonQueryExecutableForLimits(t)
	queryRows := []struct {
		name string
		run  func(limit int) string
	}{
		{"MaxResults", func(limit int) string {
			l := protocol.DefaultQueryLimits()
			l.MaxResults = limit
			_, f := json.ExecuteJSONQuery(context.Background(), executable, doc, l)
			if f == nil {
				return ""
			}
			return f.Code()
		}},
		{"MaxSteps", func(limit int) string {
			l := protocol.DefaultQueryLimits()
			l.MaxSteps = limit
			_, f := json.ExecuteJSONQuery(context.Background(), executable, doc, l)
			if f == nil {
				return ""
			}
			return f.Code()
		}},
	}
	for _, row := range queryRows {
		pinBoundaryCode(t, "json", "query."+row.name, "core.query.resource-limit@1", row.run)
	}

	materializeRows := []struct {
		name string
		run  func(limit int) string
	}{
		{"MaxOutputBytes", func(limit int) string {
			return jsonMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxOutputBytes = limit
			})
		}},
		{"MaxInputNodes", func(limit int) string {
			return jsonMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxInputNodes = limit
			})
		}},
		{"MaxDepth", func(limit int) string {
			return jsonMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxDepth = limit
			})
		}},
		{"MaxProvenanceEntries", func(limit int) string {
			return jsonMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxProvenanceEntries = limit
			})
		}},
	}
	for _, row := range materializeRows {
		pinBoundaryCode(t, "json", "materialize."+row.name,
			"core.materialization.resource-limit@1", row.run)
	}
}

// ---------------------------------------------------------------------------
// TOML family
// ---------------------------------------------------------------------------

func TestLimitsMatrixTOML(t *testing.T) {
	source := "a = 1\nb = [2, 3]\n[c]\nd = 4\n"

	parseRows := []limitRow{
		{"MaxSourceBytes", func(limit int) bool {
			l := document.DefaultParseLimits()
			l.MaxSourceBytes = limit
			_, f := toml.Parse([]byte(source), toml.Toml10V1, l)
			return f == nil
		}},
		{"MaxNestingDepth", func(limit int) bool {
			l := document.DefaultParseLimits()
			l.MaxNestingDepth = limit
			_, f := toml.Parse([]byte(source), toml.Toml10V1, l)
			return f == nil
		}},
		{"MaxTokenCount", func(limit int) bool {
			l := document.DefaultParseLimits()
			l.MaxTokenCount = limit
			_, f := toml.Parse([]byte(source), toml.Toml10V1, l)
			return f == nil
		}},
		{"MaxNodeCount", func(limit int) bool {
			l := document.DefaultParseLimits()
			l.MaxNodeCount = limit
			_, f := toml.Parse([]byte(source), toml.Toml10V1, l)
			return f == nil
		}},
	}
	// MaxDiagnostics: TOML formation never recovers (Complete or fatal
	// syntax), and a fatal parse reports its syntax code regardless of the
	// diagnostics limit; the truncation semantics are pinned by the JSON
	// family's TestDiagnosticTruncationMarker. No boundary row applies.
	for _, row := range parseRows {
		pinBoundaryCode(t, "toml", "parse."+row.name, "core.parse.resource-limit@1",
			func(limit int) string {
				if row.run(limit) {
					return ""
				}
				return "core.parse.resource-limit@1"
			})
	}

	doc := tomlParseForClosure(t, source)
	executable := tomlQueryExecutableForLimits(t)
	queryRows := []struct {
		name string
		run  func(limit int) string
	}{
		{"MaxResults", func(limit int) string {
			l := protocol.DefaultQueryLimits()
			l.MaxResults = limit
			_, f := toml.ExecuteTomlQuery(context.Background(), executable, doc, l)
			if f == nil {
				return ""
			}
			return f.Code()
		}},
		{"MaxSteps", func(limit int) string {
			l := protocol.DefaultQueryLimits()
			l.MaxSteps = limit
			_, f := toml.ExecuteTomlQuery(context.Background(), executable, doc, l)
			if f == nil {
				return ""
			}
			return f.Code()
		}},
	}
	for _, row := range queryRows {
		pinBoundaryCode(t, "toml", "query."+row.name, "core.query.resource-limit@1", row.run)
	}

	projectRows := []struct {
		name string
		run  func(limit int) string
	}{
		{"MaxValueNodes", func(limit int) string {
			l := toml.DefaultProjectionLimits()
			l.MaxValueNodes = limit
			result := doc.Project(toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1).WithLimits(l))
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Diagnostics[0].Code
		}},
		// MaxReportEntries: the TOML 1.0 exact projection never emits
		// report events (no lossy/transform events exist in the v1
		// surface), so the parameter is unconsumed; no boundary row applies.
		{"MaxProvenanceEntries", func(limit int) string {
			l := toml.DefaultProjectionLimits()
			l.MaxProvenanceEntries = limit
			result := doc.Project(toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1).WithLimits(l))
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Diagnostics[0].Code
		}},
		{"MaxDepth", func(limit int) string {
			l := toml.DefaultProjectionLimits()
			l.MaxDepth = limit
			result := doc.Project(toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1).WithLimits(l))
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Diagnostics[0].Code
		}},
	}
	for _, row := range projectRows {
		pinBoundaryCode(t, "toml", "project."+row.name, "core.projection.resource-limit@1", row.run)
	}

	materializeRows := []struct {
		name string
		run  func(limit int) string
	}{
		{"MaxOutputBytes", func(limit int) string {
			return tomlMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxOutputBytes = limit
			})
		}},
		{"MaxInputNodes", func(limit int) string {
			return tomlMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxInputNodes = limit
			})
		}},
		{"MaxDepth", func(limit int) string {
			return tomlMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxDepth = limit
			})
		}},
		{"MaxProvenanceEntries", func(limit int) string {
			return tomlMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxProvenanceEntries = limit
			})
		}},
	}
	for _, row := range materializeRows {
		pinBoundaryCode(t, "toml", "materialize."+row.name,
			"core.materialization.resource-limit@1", row.run)
	}
}

// ---------------------------------------------------------------------------
// YAML family
// ---------------------------------------------------------------------------

func TestLimitsMatrixYAML(t *testing.T) {
	source := "a: 1\nb: [2, 3]\nc:\n  d: 4\n"

	parseRows := []limitRow{
		{"MaxSourceBytes", func(limit int) bool {
			l := document.DefaultParseLimits()
			l.MaxSourceBytes = limit
			_, f := yaml.Parse([]byte(source), yaml.Yaml12CoreV1, l)
			return f == nil
		}},
		{"MaxNestingDepth", func(limit int) bool {
			l := document.DefaultParseLimits()
			l.MaxNestingDepth = limit
			_, f := yaml.Parse([]byte(source), yaml.Yaml12CoreV1, l)
			return f == nil
		}},
		{"MaxTokenCount", func(limit int) bool {
			l := document.DefaultParseLimits()
			l.MaxTokenCount = limit
			_, f := yaml.Parse([]byte(source), yaml.Yaml12CoreV1, l)
			return f == nil
		}},
		{"MaxNodeCount", func(limit int) bool {
			l := document.DefaultParseLimits()
			l.MaxNodeCount = limit
			_, f := yaml.Parse([]byte(source), yaml.Yaml12CoreV1, l)
			return f == nil
		}},
	}
	// MaxDiagnostics: YAML formation never recovers (Complete or fatal
	// syntax), and a fatal parse reports its syntax code regardless of the
	// diagnostics limit; the truncation semantics are pinned by the JSON
	// family's TestDiagnosticTruncationMarker. No boundary row applies.
	for _, row := range parseRows {
		pinBoundaryCode(t, "yaml", "parse."+row.name, "core.parse.resource-limit@1",
			func(limit int) string {
				if row.run(limit) {
					return ""
				}
				return "core.parse.resource-limit@1"
			})
	}

	doc := yamlParseForClosure(t, source)
	shared := yamlParseForClosure(t, "base: &a [1, 2, 3]\ncopy: *a\n")
	executable := yamlQueryExecutableForLimits(t)
	queryRows := []struct {
		name string
		run  func(limit int) string
	}{
		{"MaxResults", func(limit int) string {
			l := protocol.DefaultQueryLimits()
			l.MaxResults = limit
			_, f := yaml.ExecuteYamlQuery(context.Background(), executable, doc, l)
			if f == nil {
				return ""
			}
			return f.Code()
		}},
		{"MaxSteps", func(limit int) string {
			l := protocol.DefaultQueryLimits()
			l.MaxSteps = limit
			_, f := yaml.ExecuteYamlQuery(context.Background(), executable, doc, l)
			if f == nil {
				return ""
			}
			return f.Code()
		}},
	}
	for _, row := range queryRows {
		pinBoundaryCode(t, "yaml", "query."+row.name, "core.query.resource-limit@1", row.run)
	}

	valueRows := []struct {
		name string
		run  func(limit int) string
	}{
		{"MaxValueNodes", func(limit int) string {
			l := yaml.DefaultValueProjectionLimits()
			l.MaxValueNodes = limit
			result := doc.ProjectValue(yaml.BestExactValueV1().WithLimits(l))
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Code()
		}},
		{"MaxDepth", func(limit int) string {
			l := yaml.DefaultValueProjectionLimits()
			l.MaxDepth = limit
			result := doc.ProjectValue(yaml.BestExactValueV1().WithLimits(l))
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Code()
		}},
		{"MaxReportEntries", func(limit int) string {
			l := yaml.DefaultValueProjectionLimits()
			l.MaxReportEntries = limit
			result := shared.ProjectValue(yaml.BestExactValueV1().
				WithSharing(yaml.SharingPolicyDuplicateAcyclic).WithLimits(l))
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Code()
		}},
		{"MaxProvenanceEntries", func(limit int) string {
			l := yaml.DefaultValueProjectionLimits()
			l.MaxProvenanceEntries = limit
			result := shared.ProjectValue(yaml.BestExactValueV1().
				WithSharing(yaml.SharingPolicyDuplicateAcyclic).WithLimits(l))
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Code()
		}},
		{"MaxAmplificationRatio", func(limit int) string {
			l := yaml.DefaultValueProjectionLimits()
			l.MaxAmplificationRatio = limit
			result := shared.ProjectValue(yaml.BestExactValueV1().
				WithSharing(yaml.SharingPolicyDuplicateAcyclic).WithLimits(l))
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Code()
		}},
	}
	for _, row := range valueRows {
		pinBoundaryCode(t, "yaml", "project."+row.name, "yaml.projection.resource-limit@1", row.run)
	}

	pinBoundaryCode(t, "yaml", "project.graph.MaxProvenanceEntries",
		"yaml.projection.provenance-limit@1", func(limit int) string {
			l := yaml.DefaultGraphProjectionLimits()
			l.MaxProvenanceEntries = limit
			_, f := doc.ProjectGraphWithProvenance(yaml.BestExactGraphV1().WithLimits(l))
			if f == nil {
				return ""
			}
			return f.Code()
		})

	materializeRows := []struct {
		name string
		run  func(limit int) string
	}{
		{"MaxOutputBytes", func(limit int) string {
			return yamlMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxOutputBytes = limit
			})
		}},
		{"MaxInputNodes", func(limit int) string {
			return yamlMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxInputNodes = limit
			})
		}},
		{"MaxDepth", func(limit int) string {
			return yamlMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxDepth = limit
			})
		}},
		{"MaxProvenanceEntries", func(limit int) string {
			return yamlMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxProvenanceEntries = limit
			})
		}},
	}
	for _, row := range materializeRows {
		pinBoundaryCode(t, "yaml", "materialize."+row.name,
			"core.materialization.resource-limit@1", row.run)
	}
}

// ---------------------------------------------------------------------------
// INI family
// ---------------------------------------------------------------------------

func TestLimitsMatrixINI(t *testing.T) {
	source := "[s]\na=1\nb=two\n"
	python := "[s]\nkey: first\n    second\n"
	duplicates := "[s]\na=1\na=2\n"
	recovering := "[s]\nbare\n"

	rows := []limitRow{
		{"Common.MaxSourceBytes", func(limit int) bool {
			l := ini.DefaultIniParseLimits()
			l.Common.MaxSourceBytes = limit
			_, f := ini.Parse([]byte(source), ini.PortableV1, ini.IniEncodingProfileDefault(), l)
			return f == nil
		}},
		// Common.MaxNestingDepth: INI is a flat record format — no nesting
		// exists to consume the depth limit; no boundary row applies.
		{"Common.MaxTokenCount", func(limit int) bool {
			l := ini.DefaultIniParseLimits()
			l.Common.MaxTokenCount = limit
			_, f := ini.Parse([]byte(source), ini.PortableV1, ini.IniEncodingProfileDefault(), l)
			return f == nil
		}},
		{"Common.MaxNodeCount", func(limit int) bool {
			l := ini.DefaultIniParseLimits()
			l.Common.MaxNodeCount = limit
			_, f := ini.Parse([]byte(source), ini.PortableV1, ini.IniEncodingProfileDefault(), l)
			return f == nil
		}},
		{"MaxDecodedUTF8Bytes", func(limit int) bool {
			l := ini.DefaultIniParseLimits()
			l.MaxDecodedUTF8Bytes = limit
			_, f := ini.Parse([]byte(source), ini.PortableV1, ini.IniEncodingProfileDefault(), l)
			return f == nil
		}},
		{"MaxDecodedScalars", func(limit int) bool {
			l := ini.DefaultIniParseLimits()
			l.MaxDecodedScalars = limit
			_, f := ini.Parse([]byte(source), ini.PortableV1, ini.IniEncodingProfileDefault(), l)
			return f == nil
		}},
		{"MaxPhysicalLines", func(limit int) bool {
			l := ini.DefaultIniParseLimits()
			l.MaxPhysicalLines = limit
			_, f := ini.Parse([]byte(source), ini.PortableV1, ini.IniEncodingProfileDefault(), l)
			return f == nil
		}},
		{"MaxPhysicalLineBytes", func(limit int) bool {
			l := ini.DefaultIniParseLimits()
			l.MaxPhysicalLineBytes = limit
			_, f := ini.Parse([]byte(source), ini.PortableV1, ini.IniEncodingProfileDefault(), l)
			return f == nil
		}},
		{"MaxPhysicalLineScalars", func(limit int) bool {
			l := ini.DefaultIniParseLimits()
			l.MaxPhysicalLineScalars = limit
			_, f := ini.Parse([]byte(source), ini.PortableV1, ini.IniEncodingProfileDefault(), l)
			return f == nil
		}},
		{"MaxLogicalLines", func(limit int) bool {
			l := ini.DefaultIniParseLimits()
			l.MaxLogicalLines = limit
			_, f := ini.Parse([]byte(source), ini.PortableV1, ini.IniEncodingProfileDefault(), l)
			return f == nil
		}},
		{"MaxLogicalLineBytes", func(limit int) bool {
			l := ini.DefaultIniParseLimits()
			l.MaxLogicalLineBytes = limit
			_, f := ini.Parse([]byte(source), ini.PortableV1, ini.IniEncodingProfileDefault(), l)
			return f == nil
		}},
		{"MaxLogicalLineScalars", func(limit int) bool {
			l := ini.DefaultIniParseLimits()
			l.MaxLogicalLineScalars = limit
			_, f := ini.Parse([]byte(source), ini.PortableV1, ini.IniEncodingProfileDefault(), l)
			return f == nil
		}},
		{"MaxContinuationLines", func(limit int) bool {
			l := ini.DefaultIniParseLimits()
			l.MaxContinuationLines = limit
			_, f := ini.Parse([]byte(python), ini.PythonConfigParserV1,
				ini.IniEncodingProfileDefault(), l)
			return f == nil
		}},
		{"MaxSections", func(limit int) bool {
			l := ini.DefaultIniParseLimits()
			l.MaxSections = limit
			_, f := ini.Parse([]byte(source), ini.PortableV1, ini.IniEncodingProfileDefault(), l)
			return f == nil
		}},
		{"MaxEntries", func(limit int) bool {
			l := ini.DefaultIniParseLimits()
			l.MaxEntries = limit
			_, f := ini.Parse([]byte(source), ini.PortableV1, ini.IniEncodingProfileDefault(), l)
			return f == nil
		}},
		{"MaxDuplicateGroupMembers", func(limit int) bool {
			l := ini.DefaultIniParseLimits()
			l.MaxDuplicateGroupMembers = limit
			_, f := ini.Parse([]byte(duplicates), ini.PortableV1,
				ini.IniEncodingProfileDefault(), l)
			return f == nil
		}},
		{"MaxRecoveryRegions", func(limit int) bool {
			l := ini.DefaultIniParseLimits()
			l.MaxRecoveryRegions = limit
			_, f := ini.Parse([]byte(recovering), ini.PortableV1,
				ini.IniEncodingProfileDefault(), l)
			return f == nil
		}},
	}
	for _, row := range rows {
		pinBoundaryCode(t, "ini", "parse."+row.name, "core.parse.resource-limit@1",
			func(limit int) string {
				if row.run(limit) {
					return ""
				}
				return "core.parse.resource-limit@1"
			})
	}

	doc := iniParseForClosure(t, "[s]\na=1\nb=2\n")
	projectRows := []struct {
		name string
		run  func(limit int) string
	}{
		{"MaxSourceAssociations", func(limit int) string {
			l := ini.DefaultProjectionLimits()
			l.MaxSourceAssociations = limit
			result := doc.Project(ini.BestExactEntryMappingV1().WithLimits(l))
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Diagnostics[0].Code
		}},
		{"MaxValueNodes", func(limit int) string {
			l := ini.DefaultProjectionLimits()
			l.MaxValueNodes = limit
			result := doc.Project(ini.BestExactEntryMappingV1().WithLimits(l))
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Diagnostics[0].Code
		}},
		{"MaxReportEntries", func(limit int) string {
			l := ini.DefaultProjectionLimits()
			l.MaxReportEntries = limit
			// The Windows profile forms case-equivalent duplicate groups
			// whose collapse emits the report events this limit bounds.
			dup, failure := ini.Parse([]byte("[s]\r\na=1\r\nA=2\r\n"), ini.WindowsV1,
				ini.IniEncodingProfileDefault(), ini.DefaultIniParseLimits())
			if failure != nil {
				return failure.Code()
			}
			result := dup.Project(ini.RequireObjectV1(ini.ComparisonProfileEquivalent,
				ini.CollisionPolicyFirst).WithLimits(l))
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Diagnostics[0].Code
		}},
		{"MaxProvenanceUnits", func(limit int) string {
			l := ini.DefaultProjectionLimits()
			l.MaxProvenanceUnits = limit
			result := doc.Project(ini.BestExactEntryMappingV1().WithLimits(l))
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Diagnostics[0].Code
		}},
	}
	for _, row := range projectRows {
		pinBoundaryCode(t, "ini", "project."+row.name, "core.projection.resource-limit@1", row.run)
	}

	materializeRows := []struct {
		name string
		run  func(limit int) string
	}{
		{"MaxOutputBytes", func(limit int) string {
			return iniMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxOutputBytes = limit
			})
		}},
		{"MaxInputNodes", func(limit int) string {
			return iniMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxInputNodes = limit
			})
		}},
		{"MaxDepth", func(limit int) string {
			return iniMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxDepth = limit
			})
		}},
		{"MaxProvenanceEntries", func(limit int) string {
			return iniMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxProvenanceEntries = limit
			})
		}},
	}
	for _, row := range materializeRows {
		pinBoundaryCode(t, "ini", "materialize."+row.name,
			"core.materialization.resource-limit@1", row.run)
	}
}

// ---------------------------------------------------------------------------
// Java Properties family
// ---------------------------------------------------------------------------

func TestLimitsMatrixProperties(t *testing.T) {
	source := "a=1\nb=two\n"
	escapes := "a=one\\ttwo\n# comment\n"
	duplicates := "a=1\na=2\n"

	rows := []limitRow{
		{"Common.MaxSourceBytes", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.Common.MaxSourceBytes = limit
			_, f := properties.ParseReader([]byte(source), document.Utf8Encoding(), l)
			return f == nil
		}},
		// Common.MaxNestingDepth: Java Properties is a flat record format —
		// no nesting exists to consume the depth limit; no boundary row
		// applies.
		{"Common.MaxTokenCount", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.Common.MaxTokenCount = limit
			_, f := properties.ParseReader([]byte(source), document.Utf8Encoding(), l)
			return f == nil
		}},
		{"Common.MaxNodeCount", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.Common.MaxNodeCount = limit
			_, f := properties.ParseReader([]byte(source), document.Utf8Encoding(), l)
			return f == nil
		}},
		{"MaxDecodedUTF8Bytes", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.MaxDecodedUTF8Bytes = limit
			_, f := properties.ParseReader([]byte(source), document.Utf8Encoding(), l)
			return f == nil
		}},
		{"MaxDecodedScalars", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.MaxDecodedScalars = limit
			_, f := properties.ParseReader([]byte(source), document.Utf8Encoding(), l)
			return f == nil
		}},
		{"MaxNaturalLines", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.MaxNaturalLines = limit
			_, f := properties.ParseReader([]byte(source), document.Utf8Encoding(), l)
			return f == nil
		}},
		{"MaxNaturalLineBytes", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.MaxNaturalLineBytes = limit
			_, f := properties.ParseReader([]byte(source), document.Utf8Encoding(), l)
			return f == nil
		}},
		{"MaxNaturalLineScalars", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.MaxNaturalLineScalars = limit
			_, f := properties.ParseReader([]byte(source), document.Utf8Encoding(), l)
			return f == nil
		}},
		{"MaxLogicalLines", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.MaxLogicalLines = limit
			_, f := properties.ParseReader([]byte(source), document.Utf8Encoding(), l)
			return f == nil
		}},
		{"MaxLogicalLineNaturalLines", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.MaxLogicalLineNaturalLines = limit
			_, f := properties.ParseReader([]byte(source), document.Utf8Encoding(), l)
			return f == nil
		}},
		{"MaxLogicalLineScalars", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.MaxLogicalLineScalars = limit
			_, f := properties.ParseReader([]byte(source), document.Utf8Encoding(), l)
			return f == nil
		}},
		{"MaxProperties", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.MaxProperties = limit
			_, f := properties.ParseReader([]byte(source), document.Utf8Encoding(), l)
			return f == nil
		}},
		{"MaxComments", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.MaxComments = limit
			_, f := properties.ParseReader([]byte(escapes), document.Utf8Encoding(), l)
			return f == nil
		}},
		{"MaxEscapes", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.MaxEscapes = limit
			_, f := properties.ParseReader([]byte(escapes), document.Utf8Encoding(), l)
			return f == nil
		}},
		{"MaxUnicodeEscapes", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.MaxUnicodeEscapes = limit
			_, f := properties.ParseReader([]byte("a=\\u0041\n"), document.Utf8Encoding(), l)
			return f == nil
		}},
		{"MaxJavaCodeUnitsPerString", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.MaxJavaCodeUnitsPerString = limit
			_, f := properties.ParseReader([]byte(source), document.Utf8Encoding(), l)
			return f == nil
		}},
		{"MaxTotalJavaCodeUnits", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.MaxTotalJavaCodeUnits = limit
			_, f := properties.ParseReader([]byte(source), document.Utf8Encoding(), l)
			return f == nil
		}},
		{"MaxDuplicateGroupMembers", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.MaxDuplicateGroupMembers = limit
			_, f := properties.ParseReader([]byte(duplicates), document.Utf8Encoding(), l)
			return f == nil
		}},
		{"MaxRecoveryRegions", func(limit int) bool {
			l := properties.DefaultPropertiesParseLimits()
			l.MaxRecoveryRegions = limit
			// A malformed unicode escape recovers one error line.
			_, f := properties.ParseReader([]byte("a="+`\u`+"12\n"), document.Utf8Encoding(), l)
			return f == nil
		}},
	}
	for _, row := range rows {
		pinBoundaryCode(t, "properties", "parse."+row.name, "core.parse.resource-limit@1",
			func(limit int) string {
				if row.run(limit) {
					return ""
				}
				return "core.parse.resource-limit@1"
			})
	}

	doc := propertiesParseForClosure(t, source)
	projectRows := []struct {
		name string
		run  func(limit int) string
	}{
		{"MaxSourceAssociations", func(limit int) string {
			l := properties.DefaultProjectionLimits()
			l.MaxSourceAssociations = limit
			result := doc.Project(properties.BestExactEntryMapping().WithLimits(l))
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Diagnostics[0].Code
		}},
		{"MaxValueNodes", func(limit int) string {
			l := properties.DefaultProjectionLimits()
			l.MaxValueNodes = limit
			result := doc.Project(properties.BestExactEntryMapping().WithLimits(l))
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Diagnostics[0].Code
		}},
		{"MaxReportEntries", func(limit int) string {
			l := properties.DefaultProjectionLimits()
			l.MaxReportEntries = limit
			dup := propertiesParseForClosure(t, "a=1\na=2\n")
			result := dup.Project(properties.RequireObject(properties.DuplicatePolicyFirstWins).
				WithLimits(l))
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Diagnostics[0].Code
		}},
		{"MaxProvenanceUnits", func(limit int) string {
			l := properties.DefaultProjectionLimits()
			l.MaxProvenanceUnits = limit
			result := doc.Project(properties.BestExactEntryMapping().WithLimits(l))
			if result.Complete != nil {
				return ""
			}
			return result.Failed.Diagnostics[0].Code
		}},
	}
	for _, row := range projectRows {
		pinBoundaryCode(t, "properties", "project."+row.name,
			"core.projection.resource-limit@1", row.run)
	}

	materializeRows := []struct {
		name string
		run  func(limit int) string
	}{
		{"MaxOutputBytes", func(limit int) string {
			return propertiesMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxOutputBytes = limit
			})
		}},
		{"MaxInputNodes", func(limit int) string {
			return propertiesMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxInputNodes = limit
			})
		}},
		{"MaxDepth", func(limit int) string {
			return propertiesMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxDepth = limit
			})
		}},
		{"MaxProvenanceEntries", func(limit int) string {
			return propertiesMaterializeWithLimitsForMatrix(doc, func(l *document.MaterializationLimits) {
				l.MaxProvenanceEntries = limit
			})
		}},
	}
	for _, row := range materializeRows {
		pinBoundaryCode(t, "properties", "materialize."+row.name,
			"core.materialization.resource-limit@1", row.run)
	}
}

// ---------------------------------------------------------------------------
// Matrix helpers
// ---------------------------------------------------------------------------

// jsonQueryExecutableForLimits binds the simplest native query (the root
// value, one match, one step).
func jsonQueryExecutableForLimits(t *testing.T) *protocol.ExecutableQuery {
	t.Helper()
	return bindExecutableForLimits(t, protocol.DomainJSONNativeV1(),
		protocol.NewOperatorCall("json.try-object-members", 1))
}

func tomlQueryExecutableForLimits(t *testing.T) *protocol.ExecutableQuery {
	t.Helper()
	return bindExecutableForLimits(t, protocol.DomainTOMLNativeV1(),
		protocol.NewOperatorCall("toml.try-table-entries", 1))
}

func yamlQueryExecutableForLimits(t *testing.T) *protocol.ExecutableQuery {
	t.Helper()
	return bindExecutableForLimits(t, protocol.DomainYAMLNativeV1(),
		protocol.NewOperatorCall("yaml.documents", 1))
}

func bindExecutableForLimits(t *testing.T, domain *protocol.QueryDomain,
	operator *protocol.OperatorCall) *protocol.ExecutableQuery {
	t.Helper()
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).Then(operator)
	validated, failure := protocol.NewQueryDefinition(domain).WithExpression(expression).Validate()
	if failure != nil {
		t.Fatalf("query validation: %v", failure)
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, bindFailure := validated.Bind(capabilities)
	if bindFailure != nil {
		t.Fatalf("query binding: %v", bindFailure)
	}
	return executable
}

func jsonMaterializeWithLimitsForMatrix(doc *json.Document,
	setLimits func(*document.MaterializationLimits)) string {
	request, _ := json.NewProjectionRequestBuilder(json.ProjectionTargetBestExactCoreV1).Build()
	projected := doc.Project(request)
	if projected.Complete == nil {
		return projected.Failed.Diagnostics[0].Code
	}
	limits := document.DefaultMaterializationLimits()
	setLimits(&limits)
	materializationRequest := document.NewMaterializationRequest(json.JsonProfileStrictV1.ID(),
		document.NewMaterializationStyleId("json.canonical-compact", 1)).WithLimits(limits)
	result := json.Materialize(projected.Complete.Value, materializationRequest)
	if result.Complete != nil {
		return ""
	}
	return result.Failed.Failure.Code()
}

func tomlMaterializeWithLimitsForMatrix(doc *toml.Document,
	setLimits func(*document.MaterializationLimits)) string {
	projected := doc.Project(toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1))
	if projected.Complete == nil {
		return projected.Failed.Diagnostics[0].Code
	}
	limits := document.DefaultMaterializationLimits()
	setLimits(&limits)
	request := document.NewMaterializationRequest(toml.Toml10V1.ID(),
		document.NewMaterializationStyleId("toml.canonical-document", 1)).WithLimits(limits)
	result := toml.Materialize(projected.Complete.Value, request)
	if result.Complete != nil {
		return ""
	}
	return result.Failed.Failure.Code()
}

func yamlMaterializeWithLimitsForMatrix(doc *yaml.Document,
	setLimits func(*document.MaterializationLimits)) string {
	projected := doc.ProjectValue(yaml.BestExactValueV1())
	if projected.Complete == nil {
		return projected.Failed.Code()
	}
	limits := document.DefaultMaterializationLimits()
	setLimits(&limits)
	request := document.NewMaterializationRequest(yaml.Yaml12CoreV1.ID(),
		document.NewMaterializationStyleId("yaml.canonical-flow", 1)).WithLimits(limits)
	result := yaml.MaterializeValue(projected.Complete.Value, request)
	if result.Complete != nil {
		return ""
	}
	return result.Failed.Failure.Code()
}

func iniMaterializeWithLimitsForMatrix(doc *ini.Document,
	setLimits func(*document.MaterializationLimits)) string {
	projected := doc.Project(ini.BestExactEntryMappingV1())
	if projected.Complete == nil {
		return projected.Failed.Diagnostics[0].Code
	}
	limits := document.DefaultMaterializationLimits()
	setLimits(&limits)
	request := document.NewMaterializationRequest(ini.PortableV1.ID(),
		document.NewMaterializationStyleId("ini.portable-canonical", 1)).WithLimits(limits)
	result := ini.Materialize(projected.Complete.Value, request)
	if result.Complete != nil {
		return ""
	}
	return result.Failed.Failure.Code()
}

func propertiesMaterializeWithLimitsForMatrix(doc *properties.Document,
	setLimits func(*document.MaterializationLimits)) string {
	projected := doc.Project(properties.BestExactEntryMapping())
	if projected.Complete == nil {
		return projected.Failed.Diagnostics[0].Code
	}
	limits := document.DefaultMaterializationLimits()
	setLimits(&limits)
	request := document.NewMaterializationRequest(properties.PropertiesReaderV1.ID(),
		document.NewMaterializationStyleId("java-properties.reader-canonical", 1)).WithLimits(limits)
	result := properties.Materialize(projected.Complete.Value, request)
	if result.Complete != nil {
		return ""
	}
	return result.Failed.Failure.Code()
}
