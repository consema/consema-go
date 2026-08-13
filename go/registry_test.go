package consema

// The registry-face acceptance tests (consema-rs/consema/src/lib.rs registry
// tests: registry_lists_eight_families_and_sixteen_profiles,
// registry_query_domains_are_sorted_and_unique,
// registry_parse_document_round_trips_every_profile,
// registry_family_ids_match_parsed_backend_documents).

import (
	"context"
	"testing"

	"consema.dev/consema/document"
	hclpkg "consema.dev/consema/hcl"
	"consema.dev/consema/ini"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/plist"
	"consema.dev/consema/properties"
	"consema.dev/consema/protocol"
	"consema.dev/consema/toml"
	xmlpkg "consema.dev/consema/xml"
	yamlpkg "consema.dev/consema/yaml"
)

func TestRegistryListsEightFamiliesAndSixteenProfiles(t *testing.T) {
	families := Families()
	if len(families) != 8 {
		t.Fatalf("eight format families, got %d", len(families))
	}
	for index := 1; index < len(families); index++ {
		if families[index-1].ID() >= families[index].ID() {
			t.Fatalf("families not sorted by id")
		}
	}
	profiles := Profiles()
	if len(profiles) != 16 {
		t.Fatalf("sixteen profiles across the families, got %d", len(profiles))
	}
	for index := 1; index < len(profiles); index++ {
		left, right := profiles[index-1], profiles[index]
		if left.Profile().ID() > right.Profile().ID() ||
			(left.Profile().ID() == right.Profile().ID() &&
				left.Profile().Version() >= right.Profile().Version()) {
			t.Fatalf("profiles not sorted by (id, version)")
		}
	}
	expected := []string{
		"hcl.native", "hcl.tfvars", "ini.portable", "ini.python-configparser",
		"ini.windows", "java-properties.latin1", "java-properties.reader",
		"json.strict", "json5.standard", "jsonc.bounded", "plist.binary",
		"plist.xml", "toml.1.0", "xml.1.0-safe", "yaml.1.1-compat",
		"yaml.1.2-core",
	}
	for index, profile := range profiles {
		if profile.Profile().ID() != expected[index] {
			t.Fatalf("profile inventory[%d] = %s, want %s", index,
				profile.Profile().ID(), expected[index])
		}
	}
	// Every one of the sixteen profiles resolves an operation registry
	// (RFC 0015 §6.2 `operations`; 16 operation registries in the
	// capability set).
	for _, profile := range profiles {
		if _, ok := OperationRegistryFor(profile.Profile()); !ok {
			t.Fatalf("profile %s must resolve an operation registry",
				profile.Profile().ID())
		}
	}
}

func TestRegistryQueryDomainsAreSortedAndUnique(t *testing.T) {
	domains := QueryDomains()
	if len(domains) != 21 {
		t.Fatalf("query-domain constructor inventory: got %d, want 21", len(domains))
	}
	for index := 1; index < len(domains); index++ {
		left, right := domains[index-1], domains[index]
		if left.ID() > right.ID() ||
			(left.ID() == right.ID() && left.Version() >= right.Version()) {
			t.Fatalf("domains not sorted and unique by (id, version)")
		}
	}
	found := func(id string) bool {
		for _, domain := range domains {
			if domain.ID() == id {
				return true
			}
		}
		return false
	}
	if !found("core.portable-value-query") {
		t.Fatalf("core.portable-value-query missing")
	}
	if !found("hcl.native-semantic-query") {
		t.Fatalf("hcl.native-semantic-query missing")
	}
	if !found("plist.binary-structure-query") {
		t.Fatalf("plist.binary-structure-query missing")
	}
}

func TestRegistryOperationRegistriesAreDerived(t *testing.T) {
	jsonRegistry, ok := OperationRegistryFor(document.NewProfileId("json.strict", 1))
	if !ok {
		t.Fatalf("json.strict registry must resolve")
	}
	if jsonRegistry.Profile() != document.NewProfileId("json.strict", 1) {
		t.Fatalf("json registry profile differs")
	}
	jsonOperations := jsonRegistry.Operations()
	if len(jsonOperations) != 8 {
		t.Fatalf("json operation count: got %d, want 8", len(jsonOperations))
	}
	hasMove := false
	for _, operation := range jsonOperations {
		if operation.ID() == "json.edit.move-member@1" {
			hasMove = true
			if operation.TargetRole() != "json.object-member@1" ||
				operation.Support() != "Supported" || len(operation.Arguments()) != 1 {
				t.Fatalf("move-member descriptor facts differ")
			}
		}
	}
	if !hasMove {
		t.Fatalf("json.edit.move-member@1 missing")
	}

	tomlRegistry, ok := OperationRegistryFor(document.NewProfileId("toml.1.0", 1))
	if !ok {
		t.Fatalf("toml.1.0 registry must resolve")
	}
	tomlOperations := tomlRegistry.Operations()
	if len(tomlOperations) != 7 {
		t.Fatalf("toml operation count: got %d, want 7", len(tomlOperations))
	}
	hasInsert := false
	for _, operation := range tomlOperations {
		if operation.ID() == "toml.edit.insert-entry@1" {
			hasInsert = true
		}
	}
	if !hasInsert {
		t.Fatalf("toml.edit.insert-entry@1 missing")
	}

	// The per-profile operation counts of the six remaining families are
	// the derived facts of their family registries (RFC 0015 §6.2
	// `operations`; fc-manifest capability set: 16 operation registries).
	counts := []struct {
		id    string
		count int
	}{
		{"yaml.1.2-core", 8},
		{"yaml.1.1-compat", 8},
		{"ini.portable", 8},
		{"ini.windows", 8},
		{"ini.python-configparser", 8},
		{"java-properties.reader", 5},
		{"java-properties.latin1", 5},
		{"xml.1.0-safe", 8},
		{"plist.xml", 6},
		{"plist.binary", 6},
		{"hcl.native", 6},
		{"hcl.tfvars", 4},
	}
	for _, test := range counts {
		registry, ok := OperationRegistryFor(document.NewProfileId(test.id, 1))
		if !ok {
			t.Fatalf("%s registry must resolve", test.id)
		}
		if registry.Profile() != document.NewProfileId(test.id, 1) {
			t.Fatalf("%s registry profile differs", test.id)
		}
		if operations := registry.Operations(); len(operations) != test.count {
			t.Fatalf("%s operation count: got %d, want %d", test.id,
				len(operations), test.count)
		}
	}

	// Representative descriptors of the field-shape and method-shape
	// families are derived whole, not re-declared.
	yamlRegistry, _ := OperationRegistryFor(document.NewProfileId("yaml.1.2-core", 1))
	hasInsertMapping := false
	for _, operation := range yamlRegistry.Operations() {
		if operation.ID() == "yaml.edit.insert-mapping-entry@1" {
			hasInsertMapping = true
			if operation.TargetRole() != "yaml.mapping@1" || len(operation.Arguments()) != 3 {
				t.Fatalf("yaml insert-mapping-entry descriptor facts differ")
			}
		}
	}
	if !hasInsertMapping {
		t.Fatalf("yaml.edit.insert-mapping-entry@1 missing")
	}
	propertiesRegistry, _ := OperationRegistryFor(document.NewProfileId("java-properties.reader", 1))
	hasInsertProperty := false
	for _, operation := range propertiesRegistry.Operations() {
		if operation.ID() == "java-properties.edit.insert-property@1" {
			hasInsertProperty = true
			if operation.Support() != "Supported" ||
				operation.TargetRole() != "java-properties.document@1" {
				t.Fatalf("properties insert-property descriptor facts differ")
			}
		}
	}
	if !hasInsertProperty {
		t.Fatalf("java-properties.edit.insert-property@1 missing")
	}
	hclNative, _ := OperationRegistryFor(document.NewProfileId("hcl.native", 1))
	hasInsertBlock := false
	for _, operation := range hclNative.Operations() {
		if operation.ID() == "hcl.edit.insert-block@1" {
			hasInsertBlock = true
		}
	}
	if !hasInsertBlock {
		t.Fatalf("hcl.edit.insert-block@1 missing from hcl.native")
	}
	hclTfvars, _ := OperationRegistryFor(document.NewProfileId("hcl.tfvars", 1))
	for _, operation := range hclTfvars.Operations() {
		if operation.ID() == "hcl.edit.insert-block@1" ||
			operation.ID() == "hcl.edit.remove-block@1" {
			t.Fatalf("tfvars must not publish block operations: %s", operation.ID())
		}
	}

	// Unknown profile ids do not resolve.
	if _, ok := OperationRegistryFor(document.NewProfileId("example.unknown", 1)); ok {
		t.Fatalf("unknown registry must not resolve")
	}
}

func TestRegistryFamilyIDsMatchParsedBackendDocuments(t *testing.T) {
	// Drift guard (Rust facade R-8): the enumerated family ids must equal
	// the FormatFamily() facts of the backend documents themselves, and the
	// enumerated profile ids must equal the backend profile IDs. One sample
	// per family; every family id in the inventory must appear exactly
	// once.
	cases := []struct {
		familyID string
		profile  document.ProfileId
		source   string
	}{
		{"json", jsonpkg.JsonProfileStrictV1.ID(), `{}`},
		{"toml", toml.Toml10V1.ID(), "value = 1\n"},
		{"yaml", yamlpkg.Yaml12CoreV1.ID(), "value: 1\n"},
		{"ini", ini.PortableV1.ID(), "[section]\nvalue=1\n"},
		{"java-properties", properties.PropertiesReaderV1.ID(), "name=api\n"},
		{"xml", xmlpkg.XmlProfileSafeV1.ID(), "<a/>"},
		{"plist", plist.PlistProfileXmlV1.ID(),
			"<plist version=\"1.0\"><string>x</string></plist>"},
		{"hcl", hclpkg.HclProfileNativeV1.ID(), "a = 1\n"},
	}
	for _, test := range cases {
		parsed, err := ParseDocument(context.Background(), []byte(test.source),
			test.profile)
		if err != nil {
			t.Fatalf("%s sample must parse: %s", test.familyID, err.Error())
		}
		if parsed.FormatFamily().ID() != test.familyID {
			t.Fatalf("family %s must match the backend FormatFamily()",
				test.familyID)
		}
		if parsed.FormatFamily().Version() != 1 {
			t.Fatalf("family %s version must be 1", test.familyID)
		}
		if parsed.Profile() != test.profile {
			t.Fatalf("profile %s must round-trip through ParseDocument",
				test.profile.ID())
		}
	}
	// The enumerated family inventory matches the parsed backend facts
	// one-to-one.
	familyCount := 0
	for _, family := range Families() {
		for _, test := range cases {
			if family.ID() == test.familyID {
				familyCount++
			}
		}
	}
	if familyCount != 8 {
		t.Fatalf("family inventory drift: %d of 8 families match backends",
			familyCount)
	}
}

func TestRegistryParseDocumentRoundTripsEveryImplementedProfile(t *testing.T) {
	cases := []struct {
		id     string
		source string
	}{
		{"json.strict", `{"a":1}`},
		{"jsonc.bounded", `{"a":1,}`},
		{"json5.standard", `{a:1,}`},
		{"toml.1.0", "value = 1\n"},
		{"yaml.1.2-core", "value: 1\n"},
		{"yaml.1.1-compat", "value: 1\n"},
		{"ini.portable", "[section]\nvalue=1\n"},
		{"ini.windows", "[section]\nvalue=1\r\n"},
		{"ini.python-configparser", "[section]\nvalue=1\n"},
		{"java-properties.reader", "name=api\n"},
		{"java-properties.latin1", "name=api\n"},
		{"xml.1.0-safe", "<service><name>catalog</name></service>"},
		{"plist.xml", "<plist version=\"1.0\"><string>x</string></plist>"},
		{"plist.binary", binaryPlistSample()},
		{"hcl.native", "a = 1\n"},
		{"hcl.tfvars", "a = 1\n"},
	}
	for _, test := range cases {
		parsed, err := ParseDocument(context.Background(), []byte(test.source),
			document.NewProfileId(test.id, 1))
		if err != nil {
			t.Fatalf("%s must parse: %s", test.id, err.Error())
		}
		if parsed.Profile().ID() != test.id {
			t.Fatalf("%s profile round trip", test.id)
		}
	}
	// Unknown profile ids fail with the typed ProfileError carrying the
	// frozen code.
	_, err := ParseDocument(context.Background(), []byte("x"),
		document.NewProfileId("example.unknown", 1))
	profileError, ok := err.(*ProfileError)
	if !ok {
		t.Fatalf("unknown profile must fail with ProfileError")
	}
	if profileError.Code() != "core.source.encoding-conflict@1" {
		t.Fatalf("unknown profile failure code differs")
	}
}

// binaryPlistSample is a minimal hand-built binary plist: header, one
// ASCII string object ("x"), a one-byte offset table, and the 32-byte
// trailer (RFC 0013 §5). The offset table starts at byte 10.
func binaryPlistSample() string {
	return "bplist00\x5f\x78\x08" +
		"\x00\x00\x00\x00\x00\x00\x01\x01\x00\x00\x00\x00\x00\x00\x00\x01" +
		"\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x0a"
}

func TestRegistryDomainsComeFromProtocolConstructors(t *testing.T) {
	// The inventory aggregates the protocol package's frozen domain
	// constructors; a representative constructor must be in the inventory.
	domain := protocol.DomainTOMLNativeV1()
	found := false
	for _, present := range QueryDomains() {
		if present.Equal(domain) {
			found = true
		}
	}
	if !found {
		t.Fatalf("toml.native-semantic-query@1 missing from the inventory")
	}
}
