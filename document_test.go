package consema

// The Document union acceptance tests (crates/consema/src/lib.rs tests:
// common_document_facade_is_opaque_and_typed).

import (
	"context"
	"testing"

	"consema.dev/consema/document"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/toml"
)

func TestDocumentFacadeIsOpaqueAndTyped(t *testing.T) {
	jsonDoc, failure := ParseJSON(context.Background(), []byte(`{"a":1}`),
		jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("JSON facade parse: %s", failure.Error())
	}
	if string(jsonDoc.Render()) != `{"a":1}` {
		t.Fatalf("JSON facade render differs")
	}
	if jsonDoc.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("JSON facade formation status differs")
	}
	if jsonDoc.Profile() != document.NewProfileId("json.strict", 1) {
		t.Fatalf("JSON facade profile differs")
	}
	if jsonDoc.FormatFamily() != document.NewFormatFamilyId("json", 1) {
		t.Fatalf("JSON facade family differs")
	}
	if len(jsonDoc.Diagnostics()) != 0 {
		t.Fatalf("JSON facade diagnostics must be empty")
	}
	if _, ok := jsonDoc.AsJSON(); !ok {
		t.Fatalf("JSON adapter must succeed")
	}
	if _, ok := jsonDoc.AsTOML(); ok {
		t.Fatalf("TOML adapter must fail on a JSON document")
	}

	tomlDoc, tomlFailure := ParseTOML([]byte("value = 1\n"), toml.Toml10V1,
		document.DefaultParseLimits())
	if tomlFailure != nil {
		t.Fatalf("TOML facade parse: %s", tomlFailure.Error())
	}
	if string(tomlDoc.Render()) != "value = 1\n" {
		t.Fatalf("TOML facade render differs")
	}
	if tomlDoc.Profile() != document.NewProfileId("toml.1.0", 1) {
		t.Fatalf("TOML facade profile differs")
	}
	if tomlDoc.FormatFamily() != document.NewFormatFamilyId("toml", 1) {
		t.Fatalf("TOML facade family differs")
	}
	if _, ok := tomlDoc.AsTOML(); !ok {
		t.Fatalf("TOML adapter must succeed")
	}
	if _, ok := tomlDoc.AsJSON(); ok {
		t.Fatalf("JSON adapter must fail on a TOML document")
	}

	other, failure := ParseJSON(context.Background(), []byte(`{}`),
		jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("second JSON facade parse: %s", failure.Error())
	}
	if jsonDoc.SnapshotIdentity() == other.SnapshotIdentity() {
		t.Fatalf("snapshot identities must be distinct")
	}
}

func TestDocumentFacadeFormationFailure(t *testing.T) {
	// Non-UTF-8 bytes fail the JSON input contract with the typed source
	// failure; no document exists.
	_, failure := ParseJSON(context.Background(), []byte{0xff},
		jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure == nil {
		t.Fatalf("invalid UTF-8 must fail formation")
	}
	if failure.Code() == "" {
		t.Fatalf("formation failure must carry a registered code")
	}
}
