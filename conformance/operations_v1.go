package conformance

// The `consema.operations.conformance@1` suite runner. The suite's capability
// surface lands with a later format milestone; every case is a documented
// skip that names the capability and the reason (RFC 0016 §7: documented
// skip = success, never silent), and the frozen case-count assertion still
// applies.
func runOperationsV1(_ *Runner, data *suiteData) *SuiteReport {
	return runDeferred(nil, data)
}
