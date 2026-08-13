package conformance

import "strings"

// The `consema.syntax-query.conformance@1` suite runner. All 19 published
// cases execute: the JSON face (syntax.json.*) since 0.15.0 G1.2 (go/json),
// the TOML face (syntax.toml.*) since 0.15.0 G1.3 (go/toml), and the
// cursor-terminal cases (syntax.cursor.*) via the root-package cursor
// primitive (g43_faces.go RunSyntaxCursorFace; G054, adversarial audit
// 2026-08-13 — the "documented skips until the protocol-layer cursor"
// header was stale).
func runSyntaxQueryV1(_ *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		switch {
		case strings.HasPrefix(vector.ID, "syntax.json."):
			RunSyntaxQueryJSONFace(vector, report)
		case strings.HasPrefix(vector.ID, "syntax.toml."):
			RunSyntaxQueryTomlFace(vector, report)
		case strings.HasPrefix(vector.ID, "syntax.cursor."):
			RunSyntaxCursorFace(vector, report)
		default:
			report.Skipped = append(report.Skipped, SkipRecord{
				ID:         vector.ID,
				Capability: vector.Capability,
				Reason:     "cursor-terminal capability is protocol-layer (core.query.ordered-results@1); lands after 0.15.0",
			})
		}
	}
	return report
}
