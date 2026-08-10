package main

// CLI-side thin enumeration over the facade registry (mirror of the Rust
// bin's registry.rs; RFC 0015 §6.2). Every format fact of the CLI derives
// from the facade public API — nothing is redeclared here: the profile
// inventory, the query domains, and the per-profile operation registries
// come from the root package's registry surface. This module adapts that
// enumeration into the deterministic shapes the CLI commands consume and
// exposes the v7 contract/error-code registries the explain command reads.

import (
	"math/big"
	"strconv"

	consema "consema.dev/consema"
	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// ProfileEntry is one profile of the facade inventory with its owning
// format family.
type ProfileEntry struct {
	// FamilyID is the format family namespace of the profile.
	FamilyID string
	// FamilyVersion is the format family version.
	FamilyVersion uint32
	// Profile is the profile itself.
	Profile document.ProfileId
}

// profileEntries returns the full facade profile inventory, sorted by
// profile id (RFC 0015 §6.2 `profiles`).
func profileEntries() []ProfileEntry {
	profiles := consema.Profiles()
	entries := make([]ProfileEntry, 0, len(profiles))
	for _, entry := range profiles {
		entries = append(entries, ProfileEntry{
			FamilyID:      entry.Family().ID(),
			FamilyVersion: entry.Family().Version(),
			Profile:       entry.Profile(),
		})
	}
	return entries
}

// profileByID resolves one bare profile id ("ini.portable") from the facade
// inventory.
func profileByID(id string) *ProfileEntry {
	for _, entry := range profileEntries() {
		if entry.Profile.ID() == id {
			entry := entry
			return &entry
		}
	}
	return nil
}

// parseVersionedID parses one "namespace.id@N" reference into its (id,
// version) parts. A malformed reference (no `@`, a zero version, a
// non-numeric version) yields ok=false.
func parseVersionedID(text string) (string, uint32, bool) {
	at := -1
	for index := len(text) - 1; index >= 0; index-- {
		if text[index] == '@' {
			at = index
			break
		}
	}
	if at < 0 {
		return "", 0, false
	}
	id := text[:at]
	versionText := text[at+1:]
	version, err := strconv.ParseUint(versionText, 10, 32)
	if err != nil || version == 0 || id == "" {
		return "", 0, false
	}
	return id, uint32(version), true
}

// referenceValue builds the {id, version} reference value (the `profile`
// shape of `cli.inspect@1` candidates and `cli.explain@1`).
func referenceValue(id string, version uint32) core.Value {
	value, _ := core.NewObject(
		core.Entry{Key: "id", Value: core.String(id)},
		core.Entry{Key: "version", Value: integerValueOf(uint64(version))},
	)
	return value
}

// integerValueOf builds one non-negative integer value (CLI counts are
// capped by the CLI budgets).
func integerValueOf(value uint64) core.Value {
	return core.NewInteger(new(big.Int).SetUint64(value))
}

// errorCodes returns every ErrorCodeRegistry v7 code as a string; the
// registry itself is strictly sorted, so the returned order is
// deterministic (RFC 0015 §6.2 `error_codes`).
func errorCodes() []string {
	registry := protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7)
	descriptors := registry.Codes()
	codes := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		codes = append(codes, descriptor.Code)
	}
	return codes
}

// contractDescriptors returns the v7 contract descriptors in registry order
// (strictly sorted by contract id; RFC 0015 §13.2).
func contractDescriptors() []protocol.ContractDescriptor {
	return protocol.NewContractRegistry(protocol.RegistryV7).Contracts()
}
