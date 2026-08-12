package ini

import (
	"testing"
)

// TestEveryIniProfilePublishesTheSameFrozenEightOperationSurface pins the
// shared operation registry across all three profiles (consema-ini
// operation_registry.rs:105-137).
func TestEveryIniProfilePublishesTheSameFrozenEightOperationSurface(t *testing.T) {
	expected := []string{
		"ini.edit.insert-entry@1",
		"ini.edit.insert-section@1",
		"ini.edit.remove-entry@1",
		"ini.edit.remove-section@1",
		"ini.edit.rename-entry@1",
		"ini.edit.rename-section@1",
		"ini.edit.replace-literal-value@1",
		"ini.edit.replace-semantic-value@1",
	}
	for _, profile := range []IniProfile{PortableV1, WindowsV1, PythonConfigParserV1} {
		registry := NewFormatOperationRegistry(profile)
		operations := make([]string, 0, len(registry.Operations()))
		direct := 0
		for _, descriptor := range registry.Operations() {
			operations = append(operations,
				descriptor.ID.ID()+"@"+u32String(descriptor.ID.Version()))
			if descriptor.Support == OperationSupportSupported {
				direct++
			}
		}
		if !stringSliceEqual(operations, expected) {
			t.Fatalf("%s: operation surface differed", profile)
		}
		if direct != 6 {
			t.Fatalf("%s: direct structural count %d, want 6", profile, direct)
		}
	}
}

func stringSliceEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
