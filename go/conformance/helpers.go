package conformance

// Small runner-side helpers for the vector-driven suites. These are runner
// conveniences only; no expectation literal lives here.

import (
	"consema.dev/consema/protocol"
)

// strPtr wraps a string into a pointer.
func strPtr(text string) *string { return &text }

// mustContractId parses a frozen contract schema; the runner only passes
// registry-published schemas.
func mustContractId(schema string) *protocol.ContractId {
	contract, err := parseContractSchema(schema)
	if err != nil {
		panic(err)
	}
	return contract
}

// ptrFidelity wraps a projection fidelity into a pointer.
func ptrFidelity(fidelity protocol.ProjectionFidelity) *protocol.ProjectionFidelity {
	return &fidelity
}
