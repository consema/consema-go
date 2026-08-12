package protocol

import (
	"testing"
)

// The frozen per-version error-code counts (error_registry.rs:1717-1723).
var errorVersionCounts = map[ErrorRegistryVersion]int{
	ErrorRegistryV1: 55,
	ErrorRegistryV2: 62,
	ErrorRegistryV3: 90,
	ErrorRegistryV4: 92,
	ErrorRegistryV5: 132,
	ErrorRegistryV6: 166,
	ErrorRegistryV7: 187,
}

func allErrorVersions() []ErrorRegistryVersion {
	return []ErrorRegistryVersion{ErrorRegistryV1, ErrorRegistryV2, ErrorRegistryV3, ErrorRegistryV4, ErrorRegistryV5, ErrorRegistryV6, ErrorRegistryV7}
}

func TestErrorRegistryCountsAndSortedness(t *testing.T) {
	for _, version := range allErrorVersions() {
		registry := NewErrorCodeRegistry(version)
		codes := registry.Codes()
		if len(codes) != errorVersionCounts[version] {
			t.Errorf("v%d error count = %d, want %d", version+1, len(codes), errorVersionCounts[version])
		}
		for index := 1; index < len(codes); index++ {
			if codes[index-1].Code >= codes[index].Code {
				t.Errorf("v%d error codes not strictly sorted at %d: %s then %s",
					version+1, index, codes[index-1].Code, codes[index].Code)
			}
		}
	}
}

func TestErrorRegistrySupersetsAndBoundaries(t *testing.T) {
	// v5 ⊂ v6 ⊂ v7 (error_registry.rs:1726-1774).
	for _, descriptor := range NewErrorCodeRegistry(ErrorRegistryV5).Codes() {
		if !NewErrorCodeRegistry(ErrorRegistryV6).Contains(descriptor.Code) {
			t.Errorf("v6 lost %s", descriptor.Code)
		}
	}
	for _, descriptor := range NewErrorCodeRegistry(ErrorRegistryV6).Codes() {
		if !NewErrorCodeRegistry(ErrorRegistryV7).Contains(descriptor.Code) {
			t.Errorf("v7 lost %s", descriptor.Code)
		}
	}
	// The 0.13.0 registration is pinned to v7 (audit finding F3):
	// json.projection.incomplete-document@1 is absent from v6 and present in
	// v7 with the Projection category and 0.13.0 introduction
	// (error_registry.rs:1795-1822).
	if NewErrorCodeRegistry(ErrorRegistryV6).Contains("json.projection.incomplete-document@1") {
		t.Error("v6 contains json.projection.incomplete-document@1")
	}
	descriptor := NewErrorCodeRegistry(ErrorRegistryV7).Descriptor("json.projection.incomplete-document@1")
	if descriptor == nil {
		t.Fatal("v7 lost json.projection.incomplete-document@1")
	}
	if descriptor.Category != CategoryProjection || descriptor.Introduced != "0.13.0" {
		t.Errorf("json.projection.incomplete-document@1 facts wrong: %+v", descriptor)
	}
	// The CLI family is v7-only.
	if NewErrorCodeRegistry(ErrorRegistryV6).Contains("cli.data.io@1") {
		t.Error("v6 contains cli.data.io@1")
	}
	for _, code := range []string{"cli.data.io@1", "cli.write.target-is-directory@1"} {
		if !NewErrorCodeRegistry(ErrorRegistryV7).Contains(code) {
			t.Errorf("v7 lost %s", code)
		}
	}
	// Version boundaries (error_registry.rs:1757-1774).
	checks := []struct {
		version ErrorRegistryVersion
		code    string
		expect  bool
	}{
		{ErrorRegistryV1, "core.source.patch-base-mismatch@1", false},
		{ErrorRegistryV2, "core.source.patch-base-mismatch@1", true},
		{ErrorRegistryV2, "core.materialization.unrepresentable@1", false},
		{ErrorRegistryV3, "core.materialization.unrepresentable@1", true},
		{ErrorRegistryV3, "json5.syntax.invalid-identifier@1", false},
		{ErrorRegistryV4, "json5.syntax.invalid-identifier@1", true},
		{ErrorRegistryV4, "json5.string.unescaped-line-separator@1", true},
		{ErrorRegistryV4, "yaml.parse.syntax@1", false},
		{ErrorRegistryV5, "yaml.parse.syntax@1", true},
		{ErrorRegistryV5, "core.pgce.non-canonical@1", true},
		{ErrorRegistryV5, "ini.profile.encoding@1", false},
		{ErrorRegistryV6, "ini.profile.encoding@1", true},
		{ErrorRegistryV6, "cli.data.io@1", false},
		{ErrorRegistryV7, "cli.data.io@1", true},
	}
	for _, check := range checks {
		got := NewErrorCodeRegistry(check.version).Contains(check.code)
		if got != check.expect {
			t.Errorf("v%d contains(%s) = %v, want %v", check.version+1, check.code, got, check.expect)
		}
	}
}

func TestEveryProtocolKindCodeIsRegisteredInV1(t *testing.T) {
	// The ProtocolErrorKind codes are all v1 registrations
	// (error_registry.rs:1775-1793).
	registry := NewErrorCodeRegistry(ErrorRegistryV1)
	for _, kind := range []ProtocolErrorKind{
		KindInvalidJson, KindNonCanonicalJson, KindInvalidPvce, KindUnknownContract,
		KindSchemaMismatch, KindUnknownField, KindMissingField, KindWrongType,
		KindInvalidValue, KindResourceLimit, KindProcessLocalHandle,
	} {
		code := (&ProtocolError{Kind: kind}).Code()
		if !registry.Contains(code) {
			t.Errorf("v1 does not register %s", code)
		}
	}
}

func TestValidateErrorCodeRegistry(t *testing.T) {
	// A code with a missing @version suffix is rejected.
	if err := validateVersionedCode("core.diagnostic", "$.code"); err == nil {
		t.Error("unversioned code accepted")
	}
	if err := validateVersionedCode("core.diagnostic@x", "$.code"); err == nil {
		t.Error("non-numeric version accepted")
	}
	if err := validateVersionedCode("core.diagnostic@0", "$.code"); err == nil {
		t.Error("zero version accepted")
	}
	if err := validateVersionedCode("core.diagnostic@1", "$.code"); err != nil {
		t.Errorf("valid versioned code rejected: %v", err)
	}
}

func TestErrorCodeManifestIsStrictlyValid(t *testing.T) {
	for _, version := range allErrorVersions() {
		value, err := ErrorCodeManifestValueForVersion(version)
		if err != nil {
			t.Fatalf("v%d manifest build: %v", version+1, err)
		}
		if err := ValidateErrorCodeManifestValue(value); err != nil {
			t.Errorf("v%d manifest invalid: %v", version+1, err)
		}
	}
	// The v7 manifest matches the current registry exactly.
	value, err := ErrorCodeManifestValue()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateErrorCodeManifestValue(value); err != nil {
		t.Errorf("current manifest invalid: %v", err)
	}
}

func TestEveryCLICodeHasExactRegistryFacts(t *testing.T) {
	// The 21 v7 registrations: 20 CLI family codes plus the 0.13.0 JSON
	// code, with the exact categories of error_registry.rs:1205-1337.
	expected := map[string]DiagnosticCategory{
		"cli.data.invalid-request@1":            CategoryEncoding,
		"cli.data.io@1":                         CategoryEncoding,
		"cli.detection.ambiguous@1":             CategorySemantic,
		"cli.internal.unclassified@1":           CategorySemantic,
		"cli.interrupted.signal@1":              CategorySemantic,
		"cli.limit.batch-count@1":               CategoryResource,
		"cli.limit.file-size@1":                 CategoryResource,
		"cli.limit.manifest-size@1":             CategoryResource,
		"cli.usage.invalid-argument@1":          CategorySyntax,
		"cli.usage.invalid-format@1":            CategorySyntax,
		"cli.usage.missing-plan@1":              CategorySyntax,
		"cli.usage.missing-required@1":          CategorySyntax,
		"cli.usage.redaction-pattern@1":         CategorySyntax,
		"cli.usage.unknown-argument@1":          CategorySyntax,
		"cli.usage.unknown-command@1":           CategorySyntax,
		"cli.write.io@1":                        CategoryEdit,
		"cli.write.permission@1":                CategoryEdit,
		"cli.write.read-only@1":                 CategoryEdit,
		"cli.write.symlink-policy@1":            CategoryEdit,
		"cli.write.target-is-directory@1":       CategoryEdit,
		"json.projection.incomplete-document@1": CategoryProjection,
	}
	registry := NewErrorCodeRegistry(ErrorRegistryV7)
	for code, category := range expected {
		descriptor := registry.Descriptor(code)
		if descriptor == nil {
			t.Errorf("v7 does not register %s", code)
			continue
		}
		if descriptor.Category != category {
			t.Errorf("%s category = %s, want %s", code, descriptor.Category, category)
		}
	}
}
