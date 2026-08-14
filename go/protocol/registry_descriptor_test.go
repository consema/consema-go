package protocol

import (
	"testing"

	"consema.dev/consema/core"
)

func TestProfileDescriptorIsNormalizedAndStrict(t *testing.T) {
	// registry.rs.
	descriptor, err := NewProfileDescriptor(
		"toml", 1, "toml.1-0", 1, nil,
		[]string{"toml.datetime", "toml.array-table"},
		[]*CapabilityId{NewCapabilityId("core.document.exact-roundtrip", 1)},
	)
	if err != nil {
		t.Fatal(err)
	}
	value, err := descriptor.ToValue()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := (&ProfileDescriptor{}).FromValue(value)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ProfileID() != "toml.1-0" || decoded.FormatFamilyID() != "toml" {
		t.Error("profile identity wrong")
	}
	// The differences are sorted (registry.rs).
	if decoded.Differences()[0] != "toml.array-table" {
		t.Error("differences not sorted")
	}
	// Duplicate differences are rejected.
	_, err = NewProfileDescriptor("toml", 1, "toml.1-0", 1, nil,
		[]string{"toml.datetime", "toml.datetime"},
		[]*CapabilityId{NewCapabilityId("core.document.exact-roundtrip", 1)})
	if err == nil {
		t.Error("duplicate differences accepted")
	}
	// Zero versions are rejected.
	_, err = NewProfileDescriptor("toml", 0, "toml.1-0", 1, nil, nil, nil)
	if err == nil {
		t.Error("zero family version accepted")
	}
}

func TestCapabilityCrossFieldInvariantsAreEnforced(t *testing.T) {
	// registry.rs.
	capability := NewCapabilityId("core.query.ordered-results", 1)
	// Conditional support requires preconditions.
	_, err := NewCapabilityDeclaration(capability,
		ImplementationSupport{Kind: SupportConditional}, VerificationUnverified, nil)
	if err == nil {
		t.Error("empty conditional support accepted")
	}
	// Verified requires a suite ID.
	suite := "consema.conformance"
	_, err = NewCapabilityDeclaration(capability,
		ImplementationSupport{Kind: SupportConformant}, VerificationVerified, nil)
	if err == nil {
		t.Error("verified without suite accepted")
	}
	// Only Verified may name a suite.
	_, err = NewCapabilityDeclaration(capability,
		ImplementationSupport{Kind: SupportConformant}, VerificationSelfDeclared, &suite)
	if err == nil {
		t.Error("self-declared with suite accepted")
	}
	// Precondition keys must be unique.
	_, err = NewCapabilityDeclaration(capability,
		ImplementationSupport{Kind: SupportConditional, Preconditions: []Precondition{
			{Key: "profile", Value: "toml.1-0"}, {Key: "profile", Value: "yaml.1-2"},
		}}, VerificationUnverified, nil)
	if err == nil {
		t.Error("duplicate precondition keys accepted")
	}
	// A valid declaration round-trips.
	declaration, err := NewCapabilityDeclaration(capability,
		ImplementationSupport{Kind: SupportConformant}, VerificationVerified, &suite)
	if err != nil {
		t.Fatal(err)
	}
	value, err := declaration.ToValue()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := (&CapabilityDeclaration{}).FromValue(value)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Capability().Namespace() != "core.query.ordered-results" ||
		decoded.Verification() != VerificationVerified {
		t.Error("capability declaration facts wrong")
	}
	if decoded.SuiteID() == nil || *decoded.SuiteID() != suite {
		t.Error("suite ID lost")
	}
	// The wire support/preconditions combination is closed.
	badSupport, err := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.capability-declaration@1")},
		core.Entry{Key: "capability_id", Value: core.String("core.query.ordered-results")},
		core.Entry{Key: "capability_version", Value: integerValue(1)},
		core.Entry{Key: "support", Value: core.String("Conformant")},
		core.Entry{Key: "preconditions", Value: stringMapObjectMust(map[string]string{"x": "y"})},
		core.Entry{Key: "verification", Value: core.String("Unverified")},
		core.Entry{Key: "suite_id", Value: core.NullValue()},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&CapabilityDeclaration{}).FromValue(badSupport)
	if err == nil {
		t.Error("conformant support with preconditions accepted")
	}
}

func stringMapObjectMust(values map[string]string) core.Value {
	value, err := stringMapObject(values)
	if err != nil {
		panic(err)
	}
	return value
}

func TestRegistryManifestRoundTripsAndIsCurrent(t *testing.T) {
	// registry_manifest.rs.
	manifest, err := NewRegistryManifest(7, NewContractRegistry(RegistryV7), NewErrorCodeRegistry(ErrorRegistryV7))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Contracts()) != 41 || len(manifest.ErrorCodes()) != 187 {
		t.Fatal("manifest counts wrong")
	}
	if !manifest.IsCurrent() {
		t.Error("v7 manifest is not current")
	}
	value, err := manifest.ToValue()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := (&RegistryManifest{}).FromValue(value)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.IsCurrent() {
		t.Error("decoded manifest is not current")
	}
	if decoded.SemanticModel().ID() != "core.semantic-model" || decoded.SemanticModel().Version() != 7 {
		t.Error("semantic-model identity wrong")
	}
	// Every earlier version decodes and is not current.
	for _, version := range []ErrorRegistryVersion{ErrorRegistryV1, ErrorRegistryV2, ErrorRegistryV3, ErrorRegistryV4, ErrorRegistryV5, ErrorRegistryV6} {
		manifest, err := NewRegistryManifest(uint32(version)+1,
			NewContractRegistry(ContractRegistryVersion(version)),
			NewErrorCodeRegistry(version))
		if err != nil {
			t.Fatal(err)
		}
		value, err := manifest.ToValue()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := (&RegistryManifest{}).FromValue(value)
		if err != nil {
			t.Fatalf("v%d manifest decode: %v", version+1, err)
		}
		if decoded.IsCurrent() {
			t.Errorf("v%d manifest claims current", version+1)
		}
	}
	// Unsorted records are rejected.
	contracts := manifest.Contracts()
	swapped := append([]ContractManifestEntry(nil), contracts...)
	swapped[0], swapped[1] = swapped[1], swapped[0]
	_, err = ValidateRegistryManifest(manifest.SemanticModel(), swapped, manifest.ErrorCodes())
	if err == nil {
		t.Error("unsorted contract records accepted")
	}
	// Unversioned codes are rejected.
	codes := manifest.ErrorCodes()
	codes[0].Code = "core.diagnostic"
	_, err = ValidateRegistryManifest(manifest.SemanticModel(), manifest.Contracts(), codes)
	if err == nil {
		t.Error("unversioned error code accepted")
	}
}
