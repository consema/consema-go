package document

// ParseLimits are the parse resource limits; exceeding one is a fatal
// formation failure (document lib.rs:614-639).
type ParseLimits struct {
	// MaxSourceBytes is the maximum source bytes.
	MaxSourceBytes int
	// MaxNestingDepth is the maximum syntax nesting.
	MaxNestingDepth int
	// MaxTokenCount is the maximum tokens plus trivia/error regions.
	MaxTokenCount int
	// MaxNodeCount is the maximum format syntax nodes.
	MaxNodeCount int
	// MaxDiagnostics is the maximum diagnostics before an explicit
	// truncation marker.
	MaxDiagnostics int
}

// DefaultParseLimits returns the frozen defaults (64 MiB source, depth
// 256, 2M tokens, 1M nodes, 10k diagnostics).
func DefaultParseLimits() ParseLimits {
	return ParseLimits{
		MaxSourceBytes:  64 << 20,
		MaxNestingDepth: 256,
		MaxTokenCount:   2_000_000,
		MaxNodeCount:    1_000_000,
		MaxDiagnostics:  10_000,
	}
}

// MaterializationLimits are the resource limits for one complete
// materialization (materialization.rs:80-105).
type MaterializationLimits struct {
	// MaxInputNodes is the maximum input PortableValue nodes visited.
	MaxInputNodes int
	// MaxOutputBytes is the maximum raw output bytes.
	MaxOutputBytes int
	// MaxDepth is the maximum recursive container depth.
	MaxDepth int
	// MaxReportEntries is the maximum structured report events.
	MaxReportEntries int
	// MaxProvenanceEntries is the maximum provenance entries and origins
	// combined.
	MaxProvenanceEntries int
}

// DefaultMaterializationLimits returns the frozen defaults (1M input
// nodes, 64 MiB output, depth 256, 100k report entries, 2M provenance
// entries).
func DefaultMaterializationLimits() MaterializationLimits {
	return MaterializationLimits{
		MaxInputNodes:        1_000_000,
		MaxOutputBytes:       64 << 20,
		MaxDepth:             256,
		MaxReportEntries:     100_000,
		MaxProvenanceEntries: 2_000_000,
	}
}
