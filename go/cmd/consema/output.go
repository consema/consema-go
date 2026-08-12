package main

// stdout/stderr separation and machine-envelope emission (RFC 0015 §3.3).
// Under `--json`, stdout carries exactly one line of canonical envelope
// JSON ending in one LF (0x0A) and nothing else; `--json --pretty` applies
// the self-written deterministic whitespace indenter first (pure formatting
// of canonical bytes, no parse or reorder). Human result data is rendered
// by each command and written through the same writer; diagnostics always
// go to stderr through main.go.

import (
	"io"

	"consema.dev/consema/protocol"
)

// emitEnvelope writes exactly one canonical `core.cli-output@1` line ending
// in one LF. With pretty the canonical bytes first pass through
// indentCanonicalJSON; the canonical semantics are unchanged (only
// whitespace is inserted outside strings). The envelope may carry any exit
// class except usage — usage failures never reach this writer (RFC 0015
// §4.2).
func emitEnvelope(envelope *protocol.CliOutputMessage, pretty bool,
	out io.Writer) error {
	bytes, err := envelope.ToJSON(protocol.DefaultProtocolLimits())
	if err != nil {
		return &emitError{"envelope encoding failed: " + err.Error()}
	}
	if pretty {
		indented, err := indentCanonicalJSON(bytes)
		if err != nil {
			return &emitError{"envelope indentation failed: " + err.Error()}
		}
		return writeLine(indented, out)
	}
	return writeLine(bytes, out)
}

// emitError is the envelope emission failure carrying a human stderr
// message.
type emitError struct {
	message string
}

func (e *emitError) Error() string { return e.message }

func writeLine(bytes []byte, out io.Writer) error {
	if _, err := out.Write(bytes); err != nil {
		return &emitError{"stdout write failed: " + err.Error()}
	}
	if _, err := out.Write([]byte("\n")); err != nil {
		return &emitError{"stdout write failed: " + err.Error()}
	}
	return nil
}

// indentCanonicalJSON deterministically indents canonical tagged JSON bytes.
// The input is the byte output of protocol.EncodeJSON (RFC 0015 §3.1),
// which contains no whitespace at all. This function inserts `\n` and
// two-space indentation outside string literals and copies every other byte
// — including string contents and escapes — verbatim. It never parses,
// reorders, or re-encodes the value, so the canonical semantics are
// byte-for-byte unchanged up to the inserted whitespace. A malformed
// (unterminated-string) input is rejected instead of mangled.
func indentCanonicalJSON(input []byte) ([]byte, error) {
	out := make([]byte, 0, len(input)+len(input)/4)
	depth := 0
	atLineStart := true
	index := 0
	for index < len(input) {
		switch input[index] {
		case '"':
			if atLineStart {
				out = pushIndent(out, depth)
				atLineStart = false
			}
			start := index
			index++
			terminated := false
			for index < len(input) {
				if input[index] == '\\' {
					index += 2
				} else if input[index] == '"' {
					index++
					terminated = true
					break
				} else {
					index++
				}
			}
			if !terminated {
				return nil, &emitError{"unterminated string in canonical JSON"}
			}
			out = append(out, input[start:index]...)
		case '{', '[':
			if atLineStart {
				out = pushIndent(out, depth)
			}
			out = append(out, input[index])
			depth++
			out = append(out, '\n')
			atLineStart = true
			index++
		case '}', ']':
			if depth > 0 {
				depth--
			}
			if !atLineStart {
				out = append(out, '\n')
			}
			out = pushIndent(out, depth)
			out = append(out, input[index])
			atLineStart = false
			index++
		case ',':
			out = append(out, ',')
			out = append(out, '\n')
			atLineStart = true
			index++
		case ':':
			out = append(out, ':', ' ')
			index++
		case '\n':
			// Only reachable when re-indenting already-indented bytes
			// (canonical transport bytes contain no whitespace). Skipping
			// it at line starts keeps the indenter idempotent.
			if !atLineStart {
				out = append(out, '\n')
				atLineStart = true
			}
			index++
		case ' ', '\t', '\r':
			// Input whitespace outside strings is pure structure, which the
			// arms above regenerate; skipping it keeps re-indentation
			// idempotent.
			index++
		default:
			if atLineStart {
				out = pushIndent(out, depth)
				atLineStart = false
			}
			out = append(out, input[index])
			index++
		}
	}
	return out, nil
}

func pushIndent(out []byte, depth int) []byte {
	for level := 0; level < depth; level++ {
		out = append(out, ' ', ' ')
	}
	return out
}

// collapseWhitespace removes whitespace outside strings (the test-only
// inverse of the indenter).
func collapseWhitespace(input []byte) []byte {
	out := make([]byte, 0, len(input))
	inString := false
	index := 0
	for index < len(input) {
		switch input[index] {
		case '"':
			inString = !inString
			out = append(out, '"')
			index++
		case '\\':
			if inString {
				out = append(out, input[index])
				if index+1 < len(input) {
					out = append(out, input[index+1])
					index += 2
				} else {
					index++
				}
			} else {
				out = append(out, input[index])
				index++
			}
		case ' ', '\n', '\r', '\t':
			if !inString {
				index++
			} else {
				out = append(out, input[index])
				index++
			}
		default:
			out = append(out, input[index])
			index++
		}
	}
	return out
}
