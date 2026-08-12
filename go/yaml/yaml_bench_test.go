package yaml

// ---------------------------------------------------------------------------
// Benchmark baseline (milestone 0.19.0 G5.4; docs/go-implementation-plan.md
// §2.6). See go/json/json_bench_test.go for the shared discipline.
// ---------------------------------------------------------------------------

import (
	"bytes"
	"testing"

	"consema.dev/consema/document"
)

// parseFixture is a representative YAML 1.2 document (~1.5 KiB).
var parseFixture = []byte(`# consema.yaml
name: consema-service
version: 0.19.0
replicas: 3
enabled: true
ratio: 0.875

server:
  host: 0.0.0.0
  port: 8080
  tls: true
  cert: /etc/consema/tls.crt
  key: /etc/consema/tls.key
  timeouts:
    read: 30s
    write: 10s
    idle: 90s

features: &features
  fuzz: true
  bench: true
  security_matrix: true

logging:
  level: info
  format: json
  sample_rate: 0.1
  outputs: [stdout, /var/log/consema.log]

metrics:
  enabled: true
  port: 9090
  path: /metrics
  histogram_buckets: [0.001, 0.01, 0.1, 1.0, 10.0]
  labels:
    env: production
    region: eu-west-1
    team: platform

deployments:
  - strategy: rolling-update
    max_surge: 25%
    max_unavailable: 0
    pods:
      - name: web
        replicas: 3
        resources: {cpu: 500m, memory: 128Mi}
      - name: worker
        replicas: 2
        resources: {cpu: "1", memory: 512Mi}

features_copy: *features

annotation: 中文 配置 🦀 emoji
`)

// benchSink keeps benchmark results alive so the compiler cannot elide the
// measured work.
var benchSink []byte

// BenchmarkParse measures one full YAML 1.2 parse of the fixture under the
// production default limits.
func BenchmarkParse(b *testing.B) {
	b.SetBytes(int64(len(parseFixture)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		doc, failure := Parse(parseFixture, Yaml12CoreV1, document.DefaultParseLimits())
		if failure != nil {
			b.Fatalf("parse: %v", failure)
		}
		benchSink = doc.Render()
	}
}

// BenchmarkRender measures the lossless render closure.
func BenchmarkRender(b *testing.B) {
	doc, failure := Parse(parseFixture, Yaml12CoreV1, document.DefaultParseLimits())
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
