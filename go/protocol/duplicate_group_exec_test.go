package protocol_test

// Execution-level regression for the ini.duplicate-group operator row
// (G2.4; protocol/query_validate.go). The row is input-dependent: the
// table lookup types it by the input role, and the operator then emits
// every section or entry sharing the same duplicate/case-equivalence
// group (query.rs:1056-1065; consema-ini query.rs:543-565). The Rust
// reference test (consema-rs/consema-ini/src/query.rs:778-794) asserts the
// same two-match outcome on the same case-equivalent Windows source.
//
// This file is an external test package (protocol_test) so it can execute
// the query through the consema.dev/consema/ini public API without an
// import cycle (ini imports protocol).

import (
	"context"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/ini"
	"consema.dev/consema/protocol"
)

func TestIniDuplicateGroupDefinitionValidatesAndExecutes(t *testing.T) {
	doc, failure := ini.Parse([]byte("[Main]\r\nName=one\r\nname=two\r\n[Other]\r\nempty=\r\n"),
		ini.WindowsV1, ini.IniEncodingProfileDefault(), ini.DefaultIniParseLimits())
	if failure != nil {
		t.Fatalf("parse failed: %s", failure.Diagnostics()[0].Code)
	}

	// all-entries -> entry-key-equals(OriginalExact) -> duplicate-group:
	// the group row must be reachable through the table (the G2.2 finding)
	// and must emit both case-equivalent members.
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("ini.all-entries", 1)).
		Then(protocol.NewOperatorCall("ini.entry-key-equals", 1).
			WithArgument("key", core.String("Name")).
			WithArgument("comparison", core.String("OriginalExact"))).
		Then(protocol.NewOperatorCall("ini.duplicate-group", 1))
	validated, validationFailure := protocol.NewQueryDefinition(protocol.DomainININativeV1()).
		WithExpression(expression).Validate()
	if validationFailure != nil {
		t.Fatalf("Validate failed: %v", validationFailure)
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, bindFailure := validated.Bind(capabilities)
	if bindFailure != nil {
		t.Fatalf("Bind failed: %v", bindFailure)
	}
	matches, queryFailure := ini.ExecuteIniQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if queryFailure != nil {
		t.Fatalf("ExecuteIniQuery failed: %s", queryFailure.Code())
	}
	if len(matches) != 2 {
		t.Fatalf("duplicate-group matches = %d, want 2 (both case-equivalent members)", len(matches))
	}
	for _, match := range matches {
		if match.Kind != ini.IniMatchEntry || match.DuplicateGroup == nil {
			t.Fatalf("match %+v must be an entry carrying a duplicate group", match)
		}
	}
	if matches[1].Key != "name" {
		t.Errorf("second match key = %q, want the case-equivalent member key %q", matches[1].Key, "name")
	}
}
