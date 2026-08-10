package main

import (
	"bytes"
	"testing"
)

func TestIndenterInsertsOnlyWhitespaceOutsideStrings(t *testing.T) {
	envelope := sampleEnvelope(t)
	canonical, err := envelope.ToJSON(protocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	indented, err := indentCanonicalJSON(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(collapseWhitespace(indented), canonical) {
		t.Fatal("indenter must only insert whitespace outside strings")
	}
	// Deterministic and idempotent.
	again, err := indentCanonicalJSON(indented)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, indented) {
		t.Fatal("indenter must be idempotent")
	}
	// String contents are copied verbatim, escapes included.
	escaped := []byte(`{"key":"a \"b\": c"}`)
	out, err := indentCanonicalJSON(escaped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(collapseWhitespace(out), escaped) {
		t.Fatal("escaped string contents must be copied verbatim")
	}
}

func TestIndenterHandlesEmptyContainersAndPrimitives(t *testing.T) {
	cases := []struct{ input, expected string }{
		{"{}", "{\n}"},
		{"[]", "[\n]"},
		{`{"a":1}`, "{\n  \"a\": 1\n}"},
		{"[1,2]", "[\n  1,\n  2\n]"},
	}
	for _, test := range cases {
		out, err := indentCanonicalJSON([]byte(test.input))
		if err != nil {
			t.Fatalf("%s: %v", test.input, err)
		}
		if string(out) != test.expected {
			t.Fatalf("%s: got %q want %q", test.input, out, test.expected)
		}
	}
}

func TestIndenterRejectsUnterminatedStrings(t *testing.T) {
	if _, err := indentCanonicalJSON([]byte(`{"a`)); err == nil {
		t.Fatal("unterminated string must be rejected")
	}
}

func TestEmitEnvelopeWritesOneCanonicalLineEndingInLF(t *testing.T) {
	envelope := sampleEnvelope(t)
	var out bytes.Buffer
	if err := emitEnvelope(envelope, false, &out); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
		t.Fatal("envelope line must end in one LF")
	}
	if bytes.Contains(out.Bytes()[:out.Len()-1], []byte("\n")) {
		t.Fatal("exactly one line expected")
	}
	limits := protocolLimits()
	decoded := &cliOutputMessageType{}
	decoded, err := decoded.FromJSON(out.Bytes()[:out.Len()-1], limits)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Command() != envelope.Command() || decoded.ExitClass() != envelope.ExitClass() {
		t.Fatal("decoded envelope differs")
	}
}

func TestEmitEnvelopePrettyIndentsAndKeepsSemantics(t *testing.T) {
	envelope := sampleEnvelope(t)
	var compact bytes.Buffer
	var pretty bytes.Buffer
	if err := emitEnvelope(envelope, false, &compact); err != nil {
		t.Fatal(err)
	}
	if err := emitEnvelope(envelope, true, &pretty); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(compact.Bytes(), pretty.Bytes()) {
		t.Fatal("pretty output must differ from compact")
	}
	if !bytes.Equal(collapseWhitespace(pretty.Bytes()), compact.Bytes()[:compact.Len()-1]) {
		t.Fatal("collapsing the pretty line must reproduce the compact envelope bytes")
	}
}
