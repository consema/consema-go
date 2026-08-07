package conformance

import "strings"

// The `consema.syntax-query.conformance@1` suite runner. The JSON face
// (syntax.json.*) executes since 0.15.0 G1.2 (go/json) and the TOML face
// (syntax.toml.*) since 0.15.0 G1.3 (go/toml); the cursor-terminal cases
// (syntax.cursor.*) stay documented skips until the protocol-layer cursor
// capability lands (RFC 0016 §7: documented skip = success, never silent).
func runSyntaxQueryV1(_ *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		switch {
		case strings.HasPrefix(vector.ID, "syntax.json."):
			RunSyntaxQueryJSONFace(vector, report)
		case strings.HasPrefix(vector.ID, "syntax.toml."):
			RunSyntaxQueryTomlFace(vector, report)
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
