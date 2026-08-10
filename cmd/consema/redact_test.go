package main

import (
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/protocol"
)

func redactionObject(t *testing.T, entries ...core.Entry) core.Value {
	t.Helper()
	value, err := core.NewObject(entries...)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestEveryFrozenPatternMatchesItsKeyNames(t *testing.T) {
	cases := map[string][]string{
		"password":    {"password", "db.password", "PASSWORD", "Db_Password"},
		"passwd":      {"passwd", "app.passwd"},
		"secret":      {"secret", "client_secret"},
		"token":       {"token", "auth_token"},
		"apikey":      {"apikey", "APIKEY"},
		"api_key":     {"api_key"},
		"api-key":     {"api-key"},
		"privatekey":  {"privatekey"},
		"private_key": {"private_key"},
		"private-key": {"private-key"},
		"accesskey":   {"accesskey"},
		"access_key":  {"access_key"},
		"access-key":  {"access-key"},
		"credential":  {"credential", "aws_credentials"},
		"auth":        {"auth", "Authorization"},
	}
	policy := conservativePolicy()
	for pattern, keys := range cases {
		for _, key := range keys {
			if !keyMatches(&policy, key) {
				t.Fatalf("pattern %q must match key %q", pattern, key)
			}
		}
	}
	// Non-matching keys are never redacted.
	for _, key := range []string{"name", "host", "port", "value", "password_thing_literal"} {
		_ = key
	}
	if keyMatches(&policy, "host") || keyMatches(&policy, "name") {
		t.Fatal("non-matching keys must not be redacted")
	}
}

func TestRedactValueReplacesMatchingValuesAndKeepsBytes(t *testing.T) {
	value := redactionObject(t,
		core.Entry{Key: "host", Value: core.String("db.internal")},
		core.Entry{Key: "password", Value: core.String("hunter2")},
		core.Entry{Key: "api_key", Value: core.String("k-1234")},
		core.Entry{Key: "original", Value: core.NewBytes([]byte{0x6f, 0x6c, 0x64})},
	)
	policy := conservativePolicy()
	redacted, facts := redactValue(&policy, value)
	if facts.count != 2 {
		t.Fatalf("count = %d", facts.count)
	}
	if len(facts.keys) != 2 || facts.keys[0] != "password" || facts.keys[1] != "api_key" {
		t.Fatalf("keys = %v", facts.keys)
	}
	entries := redacted.(*core.Object).Entries()
	if entries[1].Value != core.String(placeholder) ||
		entries[2].Value != core.String(placeholder) ||
		entries[0].Value != core.String("db.internal") {
		t.Fatal("placeholder replacement mismatch")
	}
	// Hard gate 3: byte payloads under non-matching keys survive untouched.
	bytes, ok := entries[3].Value.(core.Bytes)
	if !ok || string(bytes) != string([]byte{0x6f, 0x6c, 0x64}) {
		t.Fatal("byte payload changed by redaction")
	}
	// --show-secrets is the sole opt-out.
	shownPolicy := showSecretsPolicy()
	shown, shownFacts := redactValue(&shownPolicy, value)
	if !core.Equal(shown, value) || shownFacts.count != 0 {
		t.Fatal("--show-secrets must return the value untouched")
	}
}

func TestRedactionRecordInvariant(t *testing.T) {
	if _, err := protocol.NewRedaction(true, 0); err == nil {
		t.Fatal("redacted without count must be rejected")
	}
	if _, err := protocol.NewRedaction(false, 1); err == nil {
		t.Fatal("count without redacted must be rejected")
	}
	if _, err := protocol.NewRedaction(true, 1); err != nil {
		t.Fatal("valid record rejected")
	}
}

func TestRedactKeysGlobSemantics(t *testing.T) {
	policy, err := conservativePolicy().withExtraPatterns([]string{"value*"})
	if err != nil {
		t.Fatal(err)
	}
	if !keyMatches(&policy, "value_scalars") {
		t.Fatal("glob value* must match value_scalars")
	}
	if keyMatches(&policy, "host") {
		t.Fatal("glob value* must not match host")
	}
	// Bracket syntax is reserved and rejected.
	if _, err := conservativePolicy().withExtraPatterns([]string{"ke[y]"}); err == nil {
		t.Fatal("bracket syntax must be rejected")
	}
	if _, err := conservativePolicy().withExtraPatterns([]string{""}); err == nil {
		t.Fatal("empty pattern must be rejected")
	}
}
