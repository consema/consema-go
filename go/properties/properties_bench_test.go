package properties

// ---------------------------------------------------------------------------
// Benchmark baseline (milestone 0.19.0 G5.4; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
// §2.6). See go/json/json_bench_test.go for the shared discipline.
// ---------------------------------------------------------------------------

import (
	"bytes"
	"testing"

	"consema.dev/consema/document"
)

// parseFixture is a representative Java Properties document (~1 KiB).
var parseFixture = []byte(`# consema.properties
service.name=consema-service
service.version=0.19.0
service.replicas=3
service.enabled=true
service.ratio=0.875

server.host=0.0.0.0
server.port=8080
server.tls=true
server.cert=/etc/consema/tls.crt
server.key=/etc/consema/tls.key
server.read_timeout=30s
server.write_timeout=10s
server.idle_timeout=90s
server.max_connections=10000

logging.level=info
logging.format=json
logging.sample_rate=0.1
logging.outputs=stdout,/var/log/consema.log

metrics.enabled=true
metrics.port=9090
metrics.path=/metrics

deployments.strategy=rolling-update
deployments.max_surge=25%
deployments.max_unavailable=0
deployments.web.name=web
deployments.web.replicas=3
deployments.web.resources=cpu=500m,memory=128Mi
deployments.worker.name=worker
deployments.worker.replicas=2
deployments.worker.resources=cpu=1,memory=512Mi

annotation.text=中文 配置 🦀 emoji
escape.example=one\ttwo\\three
`)

// benchSink keeps benchmark results alive so the compiler cannot elide the
// measured work.
var benchSink []byte

// BenchmarkParse measures one full Java Properties read of the fixture
// under the production default limits.
func BenchmarkParse(b *testing.B) {
	b.SetBytes(int64(len(parseFixture)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		doc, failure := ParseReader(parseFixture, document.Utf8Encoding(),
			DefaultPropertiesParseLimits())
		if failure != nil {
			b.Fatalf("parse: %v", failure)
		}
		benchSink = doc.Render()
	}
}

// BenchmarkRender measures the lossless render closure.
func BenchmarkRender(b *testing.B) {
	doc, failure := ParseReader(parseFixture, document.Utf8Encoding(),
		DefaultPropertiesParseLimits())
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
