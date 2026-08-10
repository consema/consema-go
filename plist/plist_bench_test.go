package plist

// ---------------------------------------------------------------------------
// Benchmark baseline (milestone 0.19.0 G5.4; docs/go-implementation-plan.md
// §2.6). See go/json/json_bench_test.go for the shared discipline.
// ---------------------------------------------------------------------------

import (
	"bytes"
	"testing"
)

// parseFixture is a representative `plist.xml@1` document (~1.5 KiB).
var parseFixture = []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>name</key><string>consema-service</string>
  <key>version</key><string>0.19.0</string>
  <key>replicas</key><integer>3</integer>
  <key>enabled</key><true/>
  <key>ratio</key><real>0.875</real>
  <key>endpoints</key>
  <array>
    <string>/health</string>
    <string>/metrics</string>
    <string>/api/v1</string>
  </array>
  <key>server</key>
  <dict>
    <key>host</key><string>0.0.0.0</string>
    <key>port</key><integer>8080</integer>
    <key>tls</key><true/>
    <key>timeouts</key>
    <dict>
      <key>read</key><string>30s</string>
      <key>write</key><string>10s</string>
      <key>idle</key><string>90s</string>
    </dict>
  </dict>
  <key>logging</key>
  <dict>
    <key>level</key><string>info</string>
    <key>format</key><string>json</string>
    <key>sample_rate</key><real>0.1</real>
  </dict>
  <key>metrics</key>
  <dict>
    <key>enabled</key><true/>
    <key>port</key><integer>9090</integer>
    <key>buckets</key>
    <array>
      <real>0.001</real><real>0.01</real><real>0.1</real><real>1.0</real><real>10.0</real>
    </array>
  </dict>
  <key>payload</key><data>AQIDBAUGBwgJCg==</data>
  <key>released</key><date>2026-08-07T12:00:00Z</date>
  <key>annotation</key><string>中文 配置 🦀 emoji</string>
</dict>
</plist>
`)

// benchSink keeps benchmark results alive so the compiler cannot elide the
// measured work.
var benchSink []byte

// BenchmarkParse measures one full `plist.xml@1` parse of the fixture
// under the production default limits.
func BenchmarkParse(b *testing.B) {
	b.SetBytes(int64(len(parseFixture)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		doc, failure := Parse(parseFixture, PlistProfileXmlV1,
			PlistEncodingProfileDefault(), DefaultPlistParseLimits())
		if failure != nil {
			b.Fatalf("parse: %v", failure)
		}
		benchSink = doc.Render()
	}
}

// BenchmarkRender measures the lossless render closure.
func BenchmarkRender(b *testing.B) {
	doc, failure := Parse(parseFixture, PlistProfileXmlV1,
		PlistEncodingProfileDefault(), DefaultPlistParseLimits())
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
