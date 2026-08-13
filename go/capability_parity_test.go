package consema

// Capability parity hard gate (roadmap §16.5 line 1539; plan §2.5 G4.4):
// the Go mandatory capability set equals the Rust Feature-Complete
// Manifest capability set — docs/fc-manifest-0.13.0.json digests.
// capability_set: "8 families / 16 profiles / 21 query domains / 16
// operation registries / 187 error codes" — with no "Rust-only" mandatory
// behavior.
//
// Every expected fact below is transcribed from the Rust published
// surface (consema-rs/crates/consema/src/lib.rs registry module and the
// `consema capabilities` CLI payload of
// consema-rs/crates/consema/src/bin/consema/capabilities.rs for the inventory, and
// consema-rs/crates/consema-*/src/operation_registry.rs for the per-profile
// operation id lists), and the Go facts are derived from the registry
// surface of this package and its families — nothing is re-declared, so
// a drift on either side fails here (the Rust facade's own drift-guard
// tests assert the same facts on the Rust side).

import (
	"errors"
	"sort"
	"testing"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// TestCapabilityParityInventory pins the five-number capability inventory
// of the Feature-Complete Manifest: 8 families, 16 profiles, 21 query
// domains, 16 operation registries (one per profile), and 187 error
// codes (derived from the Go protocol v7 error-code registry).
func TestCapabilityParityInventory(t *testing.T) {
	families := Families()
	if len(families) != 8 {
		t.Fatalf("families %d != 8 (manifest capability set)", len(families))
	}
	for index := 1; index < len(families); index++ {
		if families[index-1].ID() >= families[index].ID() {
			t.Fatalf("families not strictly sorted by id")
		}
	}
	profiles := Profiles()
	if len(profiles) != 16 {
		t.Fatalf("profiles %d != 16 (manifest capability set)", len(profiles))
	}
	for index := 1; index < len(profiles); index++ {
		if profiles[index-1].Profile().ID() > profiles[index].Profile().ID() {
			t.Fatalf("profiles not sorted by id")
		}
	}
	domains := QueryDomains()
	if len(domains) != 21 {
		t.Fatalf("query domains %d != 21 (manifest capability set)", len(domains))
	}
	for index := 1; index < len(domains); index++ {
		if domains[index-1].ID() > domains[index].ID() {
			t.Fatalf("query domains not sorted by id")
		}
	}
	// Every facade profile publishes exactly one operation registry
	// (cli.capabilities@1 `operations`: one
	// core.format-operation-registry@1 record per profile).
	for _, entry := range profiles {
		if registry, ok := OperationRegistryFor(entry.Profile()); !ok || registry == nil {
			t.Fatalf("profile %s must resolve an operation registry", entry.Profile().ID())
		}
	}
	// The 187 error codes derive from the Go protocol v7 registry, the
	// same semantic-model version the Rust facade enumerates
	// (ErrorCodeRegistry::v7(); capabilities.rs error_codes()).
	codes := protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7).Codes()
	if len(codes) != 187 {
		t.Fatalf("error codes %d != 187 (manifest capability set)", len(codes))
	}
	for index := 1; index < len(codes); index++ {
		if codes[index-1].Code >= codes[index].Code {
			t.Fatalf("error codes not strictly sorted")
		}
	}
}

// TestCapabilityParityFamilies pins the exact eight family ids against
// the Rust facade inventory (lib.rs registry::format_families and the
// cli.capabilities@1 `families` payload).
func TestCapabilityParityFamilies(t *testing.T) {
	expected := []string{
		"hcl@1",
		"ini@1",
		"java-properties@1",
		"json@1",
		"plist@1",
		"toml@1",
		"xml@1",
		"yaml@1",
	}
	actual := make([]string, 0, len(expected))
	for _, family := range Families() {
		actual = append(actual, family.ID()+"@"+itoaParity(family.Version()))
	}
	if !equalStrings(actual, expected) {
		t.Fatalf("families mismatch:\n  got:      %v\n  expected: %v", actual, expected)
	}
}

// TestCapabilityParityProfiles pins the exact sixteen (family, profile)
// pairs against the Rust facade inventory (lib.rs registry::profiles and
// the cli.capabilities@1 `profiles` payload).
func TestCapabilityParityProfiles(t *testing.T) {
	expected := []struct {
		family  string
		profile string
	}{
		{"hcl", "hcl.native@1"},
		{"hcl", "hcl.tfvars@1"},
		{"ini", "ini.portable@1"},
		{"ini", "ini.python-configparser@1"},
		{"ini", "ini.windows@1"},
		{"java-properties", "java-properties.latin1@1"},
		{"java-properties", "java-properties.reader@1"},
		{"json", "json.strict@1"},
		{"json", "json5.standard@1"},
		{"json", "jsonc.bounded@1"},
		{"plist", "plist.binary@1"},
		{"plist", "plist.xml@1"},
		{"toml", "toml.1.0@1"},
		{"xml", "xml.1.0-safe@1"},
		{"yaml", "yaml.1.1-compat@1"},
		{"yaml", "yaml.1.2-core@1"},
	}
	actual := make([]struct {
		family  string
		profile string
	}, 0, len(expected))
	for _, entry := range Profiles() {
		actual = append(actual, struct {
			family  string
			profile string
		}{
			family:  entry.Family().ID(),
			profile: entry.Profile().ID() + "@" + itoaParity(entry.Profile().Version()),
		})
	}
	if len(actual) != len(expected) {
		t.Fatalf("profiles count %d != %d", len(actual), len(expected))
	}
	for index := range expected {
		if actual[index] != expected[index] {
			t.Fatalf("profiles[%d] mismatch: got %+v expected %+v",
				index, actual[index], expected[index])
		}
	}
}

// TestCapabilityParityQueryDomains pins the exact twenty-one query
// domains against the Rust facade inventory (lib.rs
// registry::query_domains and the cli.capabilities@1 `query_domains`
// payload; consema-core query.rs domain ids).
func TestCapabilityParityQueryDomains(t *testing.T) {
	// The facade publishes the domains sorted by id (lib.rs
	// registry::query_domains sorts before returning; the
	// cli.capabilities@1 `query_domains` payload is the sorted
	// inventory).
	expected := []string{
		"core.portable-graph-query@1",
		"core.portable-value-query@1",
		"hcl.lossless-syntax-query@1",
		"hcl.native-semantic-query@1",
		"ini.lossless-syntax-query@1",
		"ini.native-semantic-query@1",
		"java-properties.lossless-syntax-query@1",
		"java-properties.native-semantic-query@1",
		"json.lossless-syntax-query@1",
		"json.lossless-syntax-query@2",
		"json.native-semantic-query@1",
		"json.native-semantic-query@2",
		"plist.binary-structure-query@1",
		"plist.lossless-syntax-query@1",
		"plist.native-semantic-query@1",
		"toml.lossless-syntax-query@1",
		"toml.native-semantic-query@1",
		"xml.lossless-syntax-query@1",
		"xml.native-semantic-query@1",
		"yaml.lossless-syntax-query@1",
		"yaml.native-semantic-query@1",
	}
	actual := make([]string, 0, len(expected))
	for _, domain := range QueryDomains() {
		actual = append(actual, domain.ID()+"@"+itoaParity(domain.Version()))
	}
	if !equalStrings(actual, expected) {
		t.Fatalf("query domains mismatch:\n  got:      %v\n  expected: %v", actual, expected)
	}
}

// TestCapabilityParityOperationSets pins the per-profile operation id
// lists against the Rust family registries
// (consema-rs/crates/consema-<family>/src/operation_registry.rs): json 8, toml 7,
// yaml 8, ini 8, properties 5, xml 8, plist 6, hcl.native 6,
// hcl.tfvars 4. The comparison is exact in both directions (no missing
// Rust operation, no extra Go operation), which is the
// no-"Rust-only"-mandatory-behavior gate of roadmap §16.5 line 1539.
func TestCapabilityParityOperationSets(t *testing.T) {
	// The frozen (profile, operation id set, count) surface of the Rust
	// registries. ExistingTypedCapability operations (json/toml/yaml/ini
	// replace-scalar/replace-value pairs) are part of the published
	// surface and must carry the same support class in Go.
	type operationCase struct {
		profile  string
		count    int
		ids      []string
		typedIDs []string
	}
	cases := []operationCase{
		{
			profile: "json.strict", count: 8,
			ids: []string{"json.edit.insert-array-element@1", "json.edit.insert-member@1",
				"json.edit.move-member@1", "json.edit.remove-array-element@1",
				"json.edit.remove-member@1", "json.edit.rename-member@1",
				"json.edit.replace-scalar-literal@1", "json.edit.replace-scalar-semantic@1"},
			typedIDs: []string{"json.edit.replace-scalar-literal@1",
				"json.edit.replace-scalar-semantic@1"},
		},
		{
			profile: "jsonc.bounded", count: 8,
			ids: []string{"json.edit.insert-array-element@1", "json.edit.insert-member@1",
				"json.edit.move-member@1", "json.edit.remove-array-element@1",
				"json.edit.remove-member@1", "json.edit.rename-member@1",
				"json.edit.replace-scalar-literal@1", "json.edit.replace-scalar-semantic@1"},
			typedIDs: []string{"json.edit.replace-scalar-literal@1",
				"json.edit.replace-scalar-semantic@1"},
		},
		{
			profile: "json5.standard", count: 8,
			ids: []string{"json.edit.insert-array-element@1", "json.edit.insert-member@1",
				"json.edit.move-member@1", "json.edit.remove-array-element@1",
				"json.edit.remove-member@1", "json.edit.rename-member@1",
				"json.edit.replace-scalar-literal@1", "json.edit.replace-scalar-semantic@1"},
			typedIDs: []string{"json.edit.replace-scalar-literal@1",
				"json.edit.replace-scalar-semantic@1"},
		},
		{
			profile: "toml.1.0", count: 7,
			ids: []string{"toml.edit.insert-array-element@1", "toml.edit.insert-entry@1",
				"toml.edit.remove-array-element@1", "toml.edit.remove-entry@1",
				"toml.edit.rename-entry@1", "toml.edit.replace-scalar-literal@1",
				"toml.edit.replace-scalar-semantic@1"},
			typedIDs: []string{"toml.edit.replace-scalar-literal@1",
				"toml.edit.replace-scalar-semantic@1"},
		},
		{
			profile: "yaml.1.2-core", count: 8,
			ids: []string{"yaml.edit.insert-alias@1", "yaml.edit.insert-mapping-entry@1",
				"yaml.edit.insert-sequence-element@1", "yaml.edit.remove-mapping-entry@1",
				"yaml.edit.remove-sequence-element@1", "yaml.edit.rename-anchor@1",
				"yaml.edit.replace-scalar-literal@1", "yaml.edit.replace-scalar-semantic@1"},
			typedIDs: []string{"yaml.edit.replace-scalar-literal@1",
				"yaml.edit.replace-scalar-semantic@1"},
		},
		{
			profile: "yaml.1.1-compat", count: 8,
			ids: []string{"yaml.edit.insert-alias@1", "yaml.edit.insert-mapping-entry@1",
				"yaml.edit.insert-sequence-element@1", "yaml.edit.remove-mapping-entry@1",
				"yaml.edit.remove-sequence-element@1", "yaml.edit.rename-anchor@1",
				"yaml.edit.replace-scalar-literal@1", "yaml.edit.replace-scalar-semantic@1"},
			typedIDs: []string{"yaml.edit.replace-scalar-literal@1",
				"yaml.edit.replace-scalar-semantic@1"},
		},
		{
			profile: "ini.portable", count: 8,
			ids: []string{"ini.edit.insert-entry@1", "ini.edit.insert-section@1",
				"ini.edit.remove-entry@1", "ini.edit.remove-section@1",
				"ini.edit.rename-entry@1", "ini.edit.rename-section@1",
				"ini.edit.replace-literal-value@1", "ini.edit.replace-semantic-value@1"},
			typedIDs: []string{"ini.edit.replace-literal-value@1",
				"ini.edit.replace-semantic-value@1"},
		},
		{
			profile: "ini.windows", count: 8,
			ids: []string{"ini.edit.insert-entry@1", "ini.edit.insert-section@1",
				"ini.edit.remove-entry@1", "ini.edit.remove-section@1",
				"ini.edit.rename-entry@1", "ini.edit.rename-section@1",
				"ini.edit.replace-literal-value@1", "ini.edit.replace-semantic-value@1"},
			typedIDs: []string{"ini.edit.replace-literal-value@1",
				"ini.edit.replace-semantic-value@1"},
		},
		{
			profile: "ini.python-configparser", count: 8,
			ids: []string{"ini.edit.insert-entry@1", "ini.edit.insert-section@1",
				"ini.edit.remove-entry@1", "ini.edit.remove-section@1",
				"ini.edit.rename-entry@1", "ini.edit.rename-section@1",
				"ini.edit.replace-literal-value@1", "ini.edit.replace-semantic-value@1"},
			typedIDs: []string{"ini.edit.replace-literal-value@1",
				"ini.edit.replace-semantic-value@1"},
		},
		{
			profile: "java-properties.reader", count: 5,
			ids: []string{"java-properties.edit.insert-property@1",
				"java-properties.edit.remove-property@1",
				"java-properties.edit.rename-property@1",
				"java-properties.edit.replace-literal-value@1",
				"java-properties.edit.replace-semantic-value@1"},
		},
		{
			profile: "java-properties.latin1", count: 5,
			ids: []string{"java-properties.edit.insert-property@1",
				"java-properties.edit.remove-property@1",
				"java-properties.edit.rename-property@1",
				"java-properties.edit.replace-literal-value@1",
				"java-properties.edit.replace-semantic-value@1"},
		},
		{
			profile: "xml.1.0-safe", count: 8,
			ids: []string{"xml.edit.insert-attribute@1", "xml.edit.insert-element@1",
				"xml.edit.remove-attribute@1", "xml.edit.remove-element@1",
				"xml.edit.rename-attribute@1", "xml.edit.rename-element@1",
				"xml.edit.replace-text@1", "xml.edit.set-attribute-value@1"},
		},
		{
			profile: "plist.xml", count: 6,
			ids: []string{"plist.edit.insert-array-element@1", "plist.edit.insert-dict-entry@1",
				"plist.edit.remove-array-element@1", "plist.edit.remove-dict-entry@1",
				"plist.edit.rename-dict-key@1", "plist.edit.set-value@1"},
		},
		{
			profile: "plist.binary", count: 6,
			ids: []string{"plist.edit.insert-array-element@1", "plist.edit.insert-dict-entry@1",
				"plist.edit.remove-array-element@1", "plist.edit.remove-dict-entry@1",
				"plist.edit.rename-dict-key@1", "plist.edit.set-value@1"},
		},
		{
			profile: "hcl.native", count: 6,
			ids: []string{"hcl.edit.insert-attribute@1", "hcl.edit.insert-block@1",
				"hcl.edit.remove-attribute@1", "hcl.edit.remove-block@1",
				"hcl.edit.rename-attribute@1", "hcl.edit.set-attribute-value@1"},
		},
		{
			profile: "hcl.tfvars", count: 4,
			ids: []string{"hcl.edit.insert-attribute@1", "hcl.edit.remove-attribute@1",
				"hcl.edit.rename-attribute@1", "hcl.edit.set-attribute-value@1"},
		},
	}
	if len(cases) != 16 {
		t.Fatalf("parity case count %d != 16 profiles", len(cases))
	}
	for _, testCase := range cases {
		registry, ok := OperationRegistryFor(document.NewProfileId(testCase.profile, 1))
		if !ok {
			t.Fatalf("profile %s must resolve an operation registry", testCase.profile)
		}
		operations := registry.Operations()
		if len(operations) != testCase.count {
			t.Fatalf("%s operations %d != %d", testCase.profile, len(operations), testCase.count)
		}
		ids := make([]string, 0, len(operations))
		for _, operation := range operations {
			ids = append(ids, operation.ID())
		}
		sort.Strings(ids)
		expected := append([]string(nil), testCase.ids...)
		sort.Strings(expected)
		if !equalStrings(ids, expected) {
			t.Fatalf("%s operation ids mismatch:\n  got:      %v\n  expected: %v",
				testCase.profile, ids, expected)
		}
		// The support class of every operation matches the Rust published
		// registry: the two replace-scalar/replace-value operations of
		// json/toml/yaml/ini are ExistingTypedCapability, everything else
		// is Supported.
		for _, operation := range operations {
			expectedSupport := "Supported"
			for _, typed := range testCase.typedIDs {
				if operation.ID() == typed {
					expectedSupport = "ExistingTypedCapability"
				}
			}
			if operation.Support() != expectedSupport {
				t.Fatalf("%s operation %s support %q != %q", testCase.profile,
					operation.ID(), operation.Support(), expectedSupport)
			}
		}
	}
}

// TestCapabilityParityNoRustOnlyMandatoryBehavior asserts that every
// mandatory capability face of the Feature-Complete Manifest exists in
// Go: every profile id parses through the single facade parse entry
// (registry.rs parse_document — the mandatory per-profile formation
// capability), every query domain is the frozen protocol constructor
// surface, and the v7 error registry carries the manifest code count
// with the manifest-pinned audit code present. Combined with the exact
// set equalities above (both directions), this closes the
// "no Rust-only mandatory behavior" gate: Go has every mandatory face
// Rust publishes and none besides.
func TestCapabilityParityNoRustOnlyMandatoryBehavior(t *testing.T) {
	// Every one of the sixteen facade profiles has a mandatory parse
	// entry through the single facade parse point: formation of an
	// empty-ish source may fail with the format's own failure, but never
	// with the facade's unknown-profile failure.
	for _, entry := range Profiles() {
		profile := entry.Profile()
		document, failure := ParseDocument(nil, []byte(" "), profile)
		if document == nil && failure == nil {
			t.Fatalf("profile %s parse entry must resolve", profile.ID())
		}
		var unknown *ProfileError
		if failure != nil && errors.As(failure, &unknown) {
			t.Fatalf("profile %s parse entry must not report an unknown profile",
				profile.ID())
		}
	}
	// The 187-code v7 registry is the manifest's error-code face; the
	// audit-registered code that brought v7 to 187 (0.13.0 audit F3) and
	// one representative registered code per published family namespace
	// are part of the mandatory surface. (plist/xml/hcl family parse
	// codes are intentionally outside the semantic-model registry on both
	// sides — the same 187-code contract.)
	registry := protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7)
	for _, code := range []string{
		"core.source.encoding-conflict@1",
		"json.projection.incomplete-document@1",
		"toml.projection.unrepresentable-datetime@1",
		"yaml.projection.sharing@1",
		"ini.projection.collision@1",
		"java-properties.projection.unpaired-surrogate@1",
	} {
		if !registry.Contains(code) {
			t.Fatalf("v7 registry must contain %s", code)
		}
	}
}

// equalStrings compares two string slices exactly.
func equalStrings(left, right []string) bool {
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

// itoaParity renders one version ordinal for the frozen inventory
// comparisons.
func itoaParity(value uint32) string {
	if value == 0 {
		return "0"
	}
	var digits [10]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
