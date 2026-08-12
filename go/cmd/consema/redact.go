package main

// Secret detection and presentation redaction (RFC 0015 §11; mirror of the
// Rust bin's redact.rs).
//
// Redaction is **presentation-only** (RFC 0015 §11.1; hard gate 3): it
// affects stderr diagnostics, human output, and the stdout envelope
// presentation, and it **never removes the byte preconditions required to
// apply a SourcePatch**. That boundary is enforced by construction: every
// entry point of this module takes a presentation value (core.Value tree or
// key/value strings) and never accepts a patch, a snapshot, or raw file
// bytes.
//
// Detection is the frozen v1 key-name pattern set of RFC 0015 §11.2 —
// `(?i)(password|passwd|secret|token|api[_-]?key|private[_-]?key|access[_-]?key|credential|auth)`
// — matched case-insensitively, whole or as a substring of key names, plus
// explicit `--redact-keys` globs. Value-shape inference is off and v1
// provides no switch to enable it; the false-positive direction is "redact
// more rather than miss a secret".
//
// A hit value is replaced by the string placeholder ($REDACTED$);
// showSecretsPolicy is the sole opt-out and disables matching entirely (RFC
// 0015 §11.4).

import (
	"strings"

	"consema.dev/consema/core"
)

// placeholder is the frozen presentation placeholder of RFC 0015 §11.3.
const placeholder = "$REDACTED$"

// frozenPatterns is the frozen v1 key-name pattern set of RFC 0015 §11.2
// expanded into its exact needle set (the `[_-]?` alternation is expanded
// into its three literal forms). Matching is case-insensitive substring
// containment against key names.
var frozenPatterns = []string{
	"password",
	"passwd",
	"secret",
	"token",
	"apikey",
	"api_key",
	"api-key",
	"privatekey",
	"private_key",
	"private-key",
	"accesskey",
	"access_key",
	"access-key",
	"credential",
	"auth",
}

// redactPatternError is one frozen usage-class failure: an invalid
// `--redact-keys` pattern (RFC 0015 §11.2, code
// `cli.usage.redaction-pattern@1`, exit 1).
type redactPatternError struct {
	message string
}

// Code returns the frozen cli.usage.* code of the failure.
func (e *redactPatternError) Code() string { return "cli.usage.redaction-pattern@1" }

// Error implements error.
func (e *redactPatternError) Error() string { return e.message }

// glob is one compiled `--redact-keys` glob (RFC 0015 §11.2). Grammar: `*`
// matches any run (including the empty run), `?` matches exactly one
// character, every other character matches itself. Matching is
// case-insensitive like the frozen patterns. `[` and `]` are rejected as
// reserved-but-unimplemented syntax (a bracket class would silently match
// nothing as a literal, so v1 refuses it instead).
type glob struct {
	text string
}

func compileGlob(text string) (*glob, *redactPatternError) {
	if text == "" {
		return nil, &redactPatternError{"--redact-keys pattern must not be empty"}
	}
	if strings.ContainsAny(text, "[]") {
		return nil, &redactPatternError{
			"--redact-keys pattern '" + text + "' uses '[' or ']' bracket syntax, " +
				"which is not supported by v1 redaction globs",
		}
	}
	return &glob{text: text}, nil
}

// matches tests one whole lowercased key name against this glob (classic
// glob DP; `*` is lazy, `?` consumes exactly one char, literals must
// equal).
func (g *glob) matches(loweredKey string) bool {
	pattern := []rune(strings.ToLower(g.text))
	text := []rune(loweredKey)
	matched := make([][]bool, len(pattern)+1)
	for i := range matched {
		matched[i] = make([]bool, len(text)+1)
	}
	matched[0][0] = true
	for i, patternChar := range pattern {
		if patternChar == '*' {
			matched[i+1][0] = matched[i][0]
		}
		for j := 0; j <= len(text); j++ {
			if !matched[i][j] {
				continue
			}
			switch patternChar {
			case '*':
				matched[i+1][j] = true
				if j < len(text) {
					matched[i][j+1] = true
				}
			case '?':
				if j < len(text) {
					matched[i+1][j+1] = true
				}
			default:
				if j < len(text) && text[j] == patternChar {
					matched[i+1][j+1] = true
				}
			}
		}
	}
	return matched[len(pattern)][len(text)]
}

// redactPolicy is one compiled redaction policy: the frozen patterns plus
// explicit `--redact-keys` globs, and the `--show-secrets` sole opt-out.
type redactPolicy struct {
	showSecrets bool
	extra       []*glob
}

// conservativePolicy is the conservative default: redaction on, frozen v1
// patterns only (RFC 0015 §11.2; the false-positive direction is "redact
// more").
func conservativePolicy() redactPolicy {
	return redactPolicy{showSecrets: false}
}

// showSecretsPolicy is the `--show-secrets` policy: the sole presentation
// opt-out (RFC 0015 §11.4). Matching is disabled entirely.
func showSecretsPolicy() redactPolicy {
	return redactPolicy{showSecrets: true}
}

// withExtraPatterns appends explicit `--redact-keys` glob patterns; an
// invalid pattern is a usage-class failure and the whole call fails.
func (p redactPolicy) withExtraPatterns(patterns []string) (redactPolicy, *redactPatternError) {
	for _, pattern := range patterns {
		compiled, err := compileGlob(pattern)
		if err != nil {
			return p, err
		}
		p.extra = append(p.extra, compiled)
	}
	return p, nil
}

// keyMatches is the pure key-name matcher over the frozen v1 pattern set
// plus the explicit globs (RFC 0015 §11.2: matched case-insensitively,
// whole or as a substring of key names). This is the single truth for every
// redaction decision in the bin.
func keyMatches(policy *redactPolicy, key string) bool {
	if policy.showSecrets {
		return false
	}
	lowered := strings.ToLower(key)
	for _, pattern := range frozenPatterns {
		if strings.Contains(lowered, pattern) {
			return true
		}
	}
	for _, extra := range policy.extra {
		if extra.matches(lowered) {
			return true
		}
	}
	return false
}

// redactedText is the presentation text of one redacted key/value pair.
type redactedText struct {
	text     string
	redacted bool
}

// redactText redacts one string value for human presentation when its key
// matches.
func redactText(policy *redactPolicy, key, value string) redactedText {
	if keyMatches(policy, key) {
		return redactedText{text: placeholder, redacted: true}
	}
	return redactedText{text: value, redacted: false}
}

// redactionFacts is the redaction facts of one redacted value (RFC 0015
// §11.3).
type redactionFacts struct {
	// Count is the number of values replaced with the placeholder.
	count uint64
	// Keys are the matching key names in first-seen document order,
	// deduplicated.
	keys []string
}

func (f *redactionFacts) record(key string) {
	f.count++
	for _, seen := range f.keys {
		if seen == key {
			return
		}
	}
	f.keys = append(f.keys, key)
}

// redactValue redacts one presentation value for machine output (the
// envelope payload view), returning the redacted tree plus the facts.
//
// Semantics (frozen for v1, deterministic and reproducible): under
// `--show-secrets` the value is returned untouched and the facts are zero.
// For every object entry whose key matches, the entry's value is replaced
// by the placeholder — any value type, including containers (a matching
// container key hides its whole subtree and counts as exactly one
// replacement). The key itself is never replaced. Entries whose key does
// not match are recursed into; sequences recurse per item; entry mappings
// treat string keys like object keys. All other value kinds are copied
// verbatim. The count is the number of replaced values; the matching keys
// are recorded in first-seen document order.
func redactValue(policy *redactPolicy, value core.Value) (core.Value, redactionFacts) {
	facts := redactionFacts{}
	redacted := redactNode(policy, value, &facts)
	return redacted, facts
}

func redactNode(policy *redactPolicy, value core.Value, facts *redactionFacts) core.Value {
	if policy.showSecrets {
		return value
	}
	switch typed := value.(type) {
	case *core.Object:
		entries := typed.Entries()
		out := make([]core.Entry, 0, len(entries))
		for _, entry := range entries {
			if keyMatches(policy, entry.Key) {
				facts.record(entry.Key)
				out = append(out, core.Entry{Key: entry.Key, Value: core.String(placeholder)})
			} else {
				out = append(out, core.Entry{Key: entry.Key, Value: redactNode(policy, entry.Value, facts)})
			}
		}
		object, err := core.NewObject(out...)
		if err != nil {
			return typed
		}
		return object
	case *core.Array:
		items := typed.Items()
		out := make([]core.Value, 0, len(items))
		for _, item := range items {
			out = append(out, redactNode(policy, item, facts))
		}
		return core.NewArray(out...)
	case *core.EntryMapping:
		entries := typed.Entries()
		out := make([]core.EntryMappingEntry, 0, len(entries))
		for _, entry := range entries {
			if text, ok := entry.Key.(core.String); ok && keyMatches(policy, string(text)) {
				facts.record(string(text))
				out = append(out, core.EntryMappingEntry{Key: entry.Key, Value: core.String(placeholder)})
				continue
			}
			out = append(out, core.EntryMappingEntry{Key: entry.Key, Value: redactNode(policy, entry.Value, facts)})
		}
		mapping, err := core.NewEntryMapping(out...)
		if err != nil {
			return typed
		}
		return mapping
	default:
		return value
	}
}
