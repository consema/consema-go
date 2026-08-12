package json

// ---------------------------------------------------------------------------
// Benchmark baseline (milestone 0.19.0 G5.4; docs/go-implementation-plan.md
// §2.6). Simple Go-side parse/render baselines in the spirit of the Rust
// BENCHMARKS (docs/BENCHMARKS-0.13.0.md): one representative fixture per
// family, production default limits, `go test -bench` runnable. The
// numbers are recorded in go/README.md "Benchmark baseline"; no budget is
// frozen (that is a Rust-side discipline).
// ---------------------------------------------------------------------------

import (
	"bytes"
	"context"
	"testing"

	"consema.dev/consema/document"
)

// parseFixture is a representative strict JSON document (~2 KiB).
var parseFixture = []byte(`{
  "name": "consema-service",
  "version": "0.19.0",
  "replicas": 3,
  "enabled": true,
  "ratio": 0.875,
  "endpoints": ["/health", "/metrics", "/api/v1", "/api/v2"],
  "limits": {"cpu": "500m", "memory": "128Mi", "ephemeral-storage": "1Gi"},
  "labels": {"app.kubernetes.io/name": "consema", "env": "production"},
  "config": {
    "feature-flags": {"fuzz": true, "bench": true, "security-matrix": true},
    "timeouts": {"connect": "5s", "read": "30s", "write": "10s", "idle": "90s"},
    "retry": {"max-attempts": 5, "backoff": "exponential", "jitter": 0.1},
    "tls": {"min-version": "1.2", "cert-path": "/etc/consema/tls.crt", "key-path": "/etc/consema/tls.key"}
  },
  "observability": {
    "logs": {"level": "info", "format": "json", "sample-rate": 0.1},
    "metrics": {"port": 9090, "path": "/metrics", "histogram-buckets": [0.001, 0.01, 0.1, 1, 10]},
    "traces": {"enabled": true, "endpoint": "http://otel-collector:4317", "sampling": "parent-based"}
  },
  "deployment": {
    "strategy": "rolling-update",
    "max-surge": "25%",
    "max-unavailable": 0,
    "pod-anti-affinity": {"preferred-during-scheduling": [{"weight": 100, "label-selector": {"match-labels": {"app": "consema"}}}]}
  },
  "annotation": "中文 配置 🦀 emoji"
}`)

// benchSink keeps benchmark results alive so the compiler cannot elide the
// measured work.
var benchSink []byte

// BenchmarkParse measures one full strict-JSON parse of the fixture under
// the production default limits.
func BenchmarkParse(b *testing.B) {
	b.SetBytes(int64(len(parseFixture)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		doc, failure := Parse(context.Background(), parseFixture,
			JsonProfileStrictV1, document.DefaultParseLimits())
		if failure != nil {
			b.Fatalf("parse: %v", failure)
		}
		benchSink = doc.Render()
	}
}

// BenchmarkRender measures the lossless render closure: rendering the
// parsed document and verifying the bytes equal the source.
func BenchmarkRender(b *testing.B) {
	doc, failure := Parse(context.Background(), parseFixture,
		JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		b.Fatalf("parse: %v", failure)
	}
	b.SetBytes(int64(len(parseFixture)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchSink = doc.Render()
		if !bytes.Equal(benchSink, parseFixture) {
			b.Fatal("render closure violated")
		}
	}
}
