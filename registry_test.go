package consema

// The registry-face acceptance tests (crates/consema/src/lib.rs registry
// tests: registry_lists_eight_families_and_sixteen_profiles,
// registry_query_domains_are_sorted_and_unique,
// registry_parse_document_round_trips_every_profile,
// registry_family_ids_match_parsed_backend_documents).

import (
	"context"
	"testing"

	"consema.dev/consema/document"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/protocol"
	"consema.dev/consema/toml"
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
	// Every implemented profile resolves an operation registry; the
	// remaining ids are declared capability facts only.
	for _, profile := range profiles {
		id := profile.Profile().ID()
		if _, ok := OperationRegistryFor(profile.Profile()); ok {
			continue
		}
		switch id {
		case "yaml.1.2-core", "yaml.1.1-compat", "ini.portable", "ini.windows",
			"ini.python-configparser", "java-properties.reader",
			"java-properties.latin1", "xml.1.0-safe", "plist.xml", "plist.binary",
			"hcl.native", "hcl.tfvars":
			// Documented: the operation surfaces land with 0.16.0-0.18.0.
		default:
			t.Fatalf("profile %s must resolve an operation registry", id)
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

	// Not-yet-implemented and unknown profiles do not resolve.
	if _, ok := OperationRegistryFor(document.NewProfileId("yaml.1.2-core", 1)); ok {
		t.Fatalf("yaml registry must not resolve before 0.16.0")
	}
	if _, ok := OperationRegistryFor(document.NewProfileId("example.unknown", 1)); ok {
		t.Fatalf("unknown registry must not resolve")
	}
}

func TestRegistryFamilyIDsMatchParsedBackendDocuments(t *testing.T) {
	// Drift guard (Rust facade R-8): the enumerated family ids must equal
	// the FormatFamily() facts of the backend documents themselves, and the
	// enumerated json/toml profile ids must equal the backend profile IDs.
	jsonDoc, failure := ParseJSON(context.Background(), []byte(`{}`),
		jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("JSON sample: %s", failure.Error())
	}
	jsonBackend, _ := jsonDoc.AsJSON()
	for _, family := range Families() {
		if family.ID() == "json" {
			if family != jsonBackend.FormatFamily() {
				t.Fatalf("family json must match the backend FormatFamily()")
			}
		}
	}
	for _, profile := range Profiles() {
		if profile.Family().ID() == "json" {
			found := false
			for _, backend := range []document.ProfileId{
				jsonpkg.JsonProfileStrictV1.ID(),
				jsonpkg.JsonProfileJsoncBoundedV1.ID(),
				jsonpkg.JsonProfileJson5StandardV1.ID(),
			} {
				if backend == profile.Profile() {
					found = true
				}
			}
			if !found {
				t.Fatalf("json profile %s not derived from the backend package",
					profile.Profile().ID())
			}
		}
	}

	tomlDoc, tomlFailure := ParseTOML([]byte("value = 1\n"), toml.Toml10V1,
		document.DefaultParseLimits())
	if tomlFailure != nil {
		t.Fatalf("TOML sample: %s", tomlFailure.Error())
	}
	tomlBackend, _ := tomlDoc.AsTOML()
	for _, family := range Families() {
		if family.ID() == "toml" && family != tomlBackend.FormatFamily() {
			t.Fatalf("family toml must match the backend FormatFamily()")
		}
	}
	for _, profile := range Profiles() {
		if profile.Family().ID() == "toml" && profile.Profile() != toml.Toml10V1.ID() {
			t.Fatalf("toml profile must match the backend profile ID")
		}
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
	// Unknown and not-yet-implemented profile ids fail with the typed
	// ProfileError carrying the frozen code.
	for _, id := range []string{"example.unknown", "yaml.1.2-core"} {
		_, err := ParseDocument(context.Background(), []byte("x"),
			document.NewProfileId(id, 1))
		profileError, ok := err.(*ProfileError)
		if !ok {
			t.Fatalf("%s must fail with ProfileError", id)
		}
		if profileError.Code() != "core.source.encoding-conflict@1" {
			t.Fatalf("%s failure code differs", id)
		}
	}
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
