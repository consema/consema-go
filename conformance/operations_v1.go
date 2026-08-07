package conformance

// The `consema.operations.conformance@1` suite runner. The JSON-face cases
// (16) execute since 0.15.0 G1.2 (go/json) and the TOML-face cases (13)
// since 0.15.0 G1.3 (go/toml); the convert cases belong to the root
// package (G1.4) and the registry/protocol data cases stay documented
// skips (RFC 0016 §7: documented skip = success, never silent).
func runOperationsV1(_ *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		switch vector.ID {
		case "registry-v3", "protocol-v3-dual-transport":
			report.Skipped = append(report.Skipped, SkipRecord{
				ID:         vector.ID,
				Capability: vector.Capability,
				Reason:     "protocol registry data case (semantic-model v3); not part of the operations capability face",
			})
		case "operations.v1.materialize-json-compact",
			"operations.v1.materialize-json-pretty-crlf",
			"operations.v1.materialize-json-entry-mapping-duplicates",
			"operations.v1.materialize-json-nonstring-key-rejected",
			"operations.v1.materialize-json-float-rejected",
			"operations.v1.materialize-json-output-limit",
			"operations.v1.materialization-depth-limit",
			"operations.v1.json-object-insert",
			"operations.v1.json-object-remove-duplicate",
			"operations.v1.json-array-remove",
			"operations.v1.json-conflict-atomic",
			"operations.v1.json-dry-run-proof-patch",
			"operations.v1.json-structural-matrix",
			"operations.v1.json-conflict-matrix",
			"operations.v1.materialization-security-matrix",
			"operations.v1.untouched-proof-tamper":
			RunOperationsJSONFace(vector, report)
		case "operations.v1.operation-registry",
			"operations.v1.materialize-toml-native",
			"operations.v1.materialize-toml-explicit-mapping",
			"operations.v1.materialize-toml-implicit-mapping-rejected",
			"operations.v1.materialize-toml-null-rejected",
			"operations.v1.materialize-toml-output-limit",
			"operations.v1.toml-root-insert",
			"operations.v1.toml-inline-rename",
			"operations.v1.toml-array-remove",
			"operations.v1.toml-conflict-atomic",
			"operations.v1.toml-dry-run-proof-patch",
			"operations.v1.toml-structural-matrix",
			"operations.v1.toml-conflict-matrix":
			RunOperationsTOMLFace(vector, report)
		default:
			report.Skipped = append(report.Skipped, SkipRecord{
				ID:         vector.ID,
				Capability: vector.Capability,
				Reason:     "convert face belongs to the root package (0.15.0 G1.4)",
			})
		}
	}
	return report
}
