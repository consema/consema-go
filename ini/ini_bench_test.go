package ini

// ---------------------------------------------------------------------------
// Benchmark baseline (milestone 0.19.0 G5.4; docs/go-implementation-plan.md
// §2.6). See go/json/json_bench_test.go for the shared discipline.
// ---------------------------------------------------------------------------

import (
	"bytes"
	"testing"
)

// parseFixture is a representative INI document (~1 KiB).
var parseFixture = []byte(`; consema.ini
[service]
name=consema-service
version=0.19.0
replicas=3
enabled=true
ratio=0.875

[server]
host=0.0.0.0
port=8080
tls=true
cert=/etc/consema/tls.crt
key=/etc/consema/tls.key
read_timeout=30s
write_timeout=10s
idle_timeout=90s
max_connections=10000

[logging]
level=info
format=json
sample_rate=0.1
outputs=stdout,/var/log/consema.log

[metrics]
enabled=true
port=9090
path=/metrics

[deployments]
strategy=rolling-update
max_surge=25%
max_unavailable=0

[deployments.web]
name=web
replicas=3
resources=cpu=500m,memory=128Mi

[deployments.worker]
name=worker
replicas=2
resources=cpu=1,memory=512Mi

[annotation]
text=中文 配置 🦀 emoji
`)

// benchSink keeps benchmark results alive so the compiler cannot elide the
// measured work.
var benchSink []byte

// BenchmarkParse measures one full portable INI parse of the fixture under
// the production default limits.
func BenchmarkParse(b *testing.B) {
	b.SetBytes(int64(len(parseFixture)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		doc, failure := Parse(parseFixture, PortableV1, IniEncodingProfileDefault(),
			DefaultIniParseLimits())
		if failure != nil {
			b.Fatalf("parse: %v", failure)
		}
		benchSink = doc.Render()
	}
}

// BenchmarkRender measures the lossless render closure.
func BenchmarkRender(b *testing.B) {
	doc, failure := Parse(parseFixture, PortableV1, IniEncodingProfileDefault(),
		DefaultIniParseLimits())
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
