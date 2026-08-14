package protocol

import (
	"testing"
)

func TestExitClassTableIsExhaustiveAndClosed(t *testing.T) {
	// RFC 0015 §5.1 classification table; codes 6-255 stay reserved.
	table := []struct {
		class ExitClass
		code  uint8
		name  string
	}{
		{ExitSuccess, 0, "success"},
		{ExitUsage, 1, "usage"},
		{ExitData, 2, "data"},
		{ExitLimit, 3, "limit"},
		{ExitPrecondition, 4, "precondition"},
		{ExitInternal, 5, "internal"},
	}
	for _, row := range table {
		if Classify(row.class) != row.code {
			t.Errorf("classify(%v) = %d, want %d", row.class, Classify(row.class), row.code)
		}
		if row.class.ExitCode() != row.code {
			t.Errorf("exit code of %v = %d, want %d", row.class, row.class.ExitCode(), row.code)
		}
		if row.class.Name() != row.name {
			t.Errorf("name of %v = %s, want %s", row.class, row.class.Name(), row.name)
		}
	}
	// Envelope names round-trip through the closed set.
	for _, row := range table {
		parsed, ok := ParseExitClass(row.name)
		if !ok || parsed != row.class {
			t.Errorf("parse(%s) = %v/%v, want %v", row.name, parsed, ok, row.class)
		}
	}
	if _, ok := ParseExitClass("unknown"); ok {
		t.Error("unknown exit class accepted")
	}
}

func TestUsageFamilyClassifiesOne(t *testing.T) {
	for _, code := range []string{
		"cli.usage.invalid-argument@1",
		"cli.usage.invalid-format@1",
		"cli.usage.missing-plan@1",
		"cli.usage.missing-required@1",
		"cli.usage.redaction-pattern@1",
		"cli.usage.unknown-argument@1",
		"cli.usage.unknown-command@1",
	} {
		if ClassifyErrorCode(code) != ExitUsage {
			t.Errorf("classify(%s) != usage", code)
		}
	}
	if ClassifyErrorCode("cli.usage.invalid-format@1").ExitCode() != 1 {
		t.Error("usage exit code != 1")
	}
}

func TestDataFamilyAndFormationFailuresClassifyTwo(t *testing.T) {
	// FatalFormationFailure (including core.source.invalid-utf8@1) -> 2.
	for _, code := range []string{
		"cli.data.invalid-request@1",
		"cli.data.io@1",
		"cli.detection.ambiguous@1",
		"core.source.invalid-utf8@1",
		"core.source.code-page-required@1",
		"core.protocol.invalid-json@1",
		"core.protocol.schema-mismatch@1",
		"core.query.invalid-argument@1",
	} {
		if ClassifyErrorCode(code) != ExitData {
			t.Errorf("classify(%s) != data", code)
		}
	}
	if ClassifyErrorCode("cli.data.invalid-request@1").ExitCode() != 2 {
		t.Error("data exit code != 2")
	}
}

func TestEveryResourceLimitClassifiesThree(t *testing.T) {
	// Any *-resource-limit@1, core or format-local, -> 3; the CLI-layer
	// cli.limit.* family lands here as well.
	for _, code := range []string{
		"cli.limit.batch-count@1",
		"cli.limit.file-size@1",
		"cli.limit.manifest-size@1",
		"core.protocol.resource-limit@1",
		"core.parse.resource-limit@1",
		"core.query.resource-limit@1",
		"ini.parse.resource-limit@1",
		"xml.parse.resource-limit@1",
	} {
		if ClassifyErrorCode(code) != ExitLimit {
			t.Errorf("classify(%s) != limit", code)
		}
	}
	if ClassifyErrorCode("cli.limit.file-size@1").ExitCode() != 3 {
		t.Error("limit exit code != 3")
	}
}

func TestPreconditionFamilyClassifiesFour(t *testing.T) {
	// stale digest, original-bytes mismatch, write I/O, edit conflicts.
	for _, code := range []string{
		"core.source.patch-base-mismatch@1",
		"core.source.patch-original-mismatch@1",
		"core.source.patch-target-mismatch@1",
		"core.edit.precondition-failed@1",
		"cli.write.io@1",
		"cli.write.permission@1",
		"cli.write.read-only@1",
		"cli.write.symlink-policy@1",
		"cli.write.target-is-directory@1",
		"cli.interrupted.signal@1",
	} {
		if ClassifyErrorCode(code) != ExitPrecondition {
			t.Errorf("classify(%s) != precondition", code)
		}
	}
	for _, code := range []string{
		"core.source.patch-base-mismatch@1", "cli.write.io@1",
		"core.source.patch-original-mismatch@1", "cli.interrupted.signal@1",
	} {
		if ClassifyErrorCode(code).ExitCode() != 4 {
			t.Errorf("%s exit code != 4", code)
		}
	}
}

func TestInternalFamilyClassifiesFive(t *testing.T) {
	if ClassifyErrorCode("cli.internal.unclassified@1") != ExitInternal {
		t.Error("cli.internal.unclassified@1 != internal")
	}
	if ClassifyErrorCode("cli.internal.unclassified@1").ExitCode() != 5 {
		t.Error("internal exit code != 5")
	}
}

func TestUnlistedFormatCodesPassThroughAsData(t *testing.T) {
	// Format-layer codes are never rewritten and never invent new classes:
	// an unlisted code means the operation did not produce a complete
	// result -> data (2).
	for _, code := range []string{
		"ini.parse.malformed-line@1",
		"json.parse.invalid-json@1",
		"yaml.parse.syntax@1",
		"json.projection.incomplete-document@1",
		"example.unknown-code@1",
		"",
	} {
		if ClassifyErrorCode(code) != ExitData {
			t.Errorf("classify(%q) != data", code)
		}
	}
}

func TestEveryRegisteredCLICodeClassifiesPerRFCTable(t *testing.T) {
	// Every cli.* code registered in v7 maps per the RFC 0015 §5.2 family
	// table (exit_class.rs).
	registry := NewErrorCodeRegistry(ErrorRegistryV7)
	for _, descriptor := range registry.Codes() {
		code := descriptor.Code
		if !hasPrefix(code, "cli.") {
			continue
		}
		var expected ExitClass
		switch {
		case hasPrefix(code, "cli.usage."):
			expected = ExitUsage
		case hasPrefix(code, "cli.data.") || hasPrefix(code, "cli.detection."):
			expected = ExitData
		case hasPrefix(code, "cli.limit."):
			expected = ExitLimit
		case hasPrefix(code, "cli.write.") || hasPrefix(code, "cli.interrupted."):
			expected = ExitPrecondition
		case hasPrefix(code, "cli.internal."):
			expected = ExitInternal
		default:
			t.Fatalf("cli code %s matches no frozen family", code)
		}
		if got := ClassifyErrorCode(code); got != expected {
			t.Errorf("classify(%s) = %v, want %v", code, got, expected)
		}
	}
}
