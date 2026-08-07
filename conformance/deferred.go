package conformance

// The deferred suite runners. These suites' capabilities land with the
// format milestones (0.15.0-0.18.0); every case is a documented skip that
// names the capability and the reason (RFC 0016 §7: documented skip =
// success, never silent). The case-count assertion still applies: the
// frozen 508-case inventory is pinned per suite by the framework, and the
// aggregate digest check re-pins the whole inventory against the manifest.

// runDeferred executes one suite whose capability surface is not yet
// implemented by the Go SDK.
func runDeferred(_ *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		report.Skipped = append(report.Skipped, SkipRecord{
			ID:         vector.ID,
			Capability: vector.Capability,
			Reason:     deferredReason(data.Suite, vector.Capability),
		})
	}
	return report
}

// deferredReason names the milestone that owns the capability family.
func deferredReason(suite, capability string) string {
	switch suite {
	case "consema.toml.conformance@1":
		return "TOML family surface lands with 0.15.0 (G1.3)"
	case "consema.source.conformance@1":
		return "document source surface lands with 0.15.0 (G1.1)"
	case "consema.syntax-query.conformance@1":
		return "syntax-query family surface lands with 0.15.0+"
	case "consema.operations.conformance@1":
		return "operations surface lands with 0.15.0-0.18.0 (G1.2-G4.x)"
	case "consema.json-family.conformance@2":
		return "JSON family surface lands with 0.15.0 (G1.2)"
	case "consema.yaml.conformance@1":
		return "YAML family surface lands with 0.16.0 (G2.1)"
	case "consema.ini.conformance@1":
		return "INI family surface lands with 0.16.0 (G2.2)"
	case "consema.java-properties.conformance@1":
		return "Java Properties family surface lands with 0.16.0 (G2.3)"
	case "consema.xml-1-0-safe.conformance@1":
		return "XML family surface lands with 0.17.0 (G3.1)"
	case "consema.plist.conformance@1":
		return "plist family surface lands with 0.17.0 (G3.2)"
	case "consema.hcl.conformance@1":
		return "HCL family surface lands with 0.18.0 (G4.1)"
	}
	return "capability not implemented by this Go milestone: " + capability
}
