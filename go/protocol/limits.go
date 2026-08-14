package protocol

// ProtocolLimits are the resource limits shared by the canonical JSON and
// PVCE/1 protocol transports, mirroring the Rust ProtocolLimits
// (consema-rs/consema-protocol/src/limits.rs). The zero value rejects every
// operation; use DefaultProtocolLimits.
type ProtocolLimits struct {
	// MaxBytes is the maximum encoded transport bytes.
	MaxBytes int
	// MaxDepth is the maximum nested PortableValue depth.
	MaxDepth int
	// MaxNodes is the maximum total PortableValue nodes.
	MaxNodes int
	// MaxContainerEntries is the maximum entries in one container.
	MaxContainerEntries int
	// MaxBlobBytes is the maximum one String, Bytes, key, or identifier
	// payload.
	MaxBlobBytes int
	// MaxIntegerBytes is the maximum magnitude bytes for an arbitrary
	// integer.
	MaxIntegerBytes int
}

// DefaultProtocolLimits returns the frozen defaults (Rust
// consema-rs/consema-protocol/src/limits.rs).
func DefaultProtocolLimits() ProtocolLimits {
	return ProtocolLimits{
		MaxBytes:            64 << 20, // 64 MiB
		MaxDepth:            256,
		MaxNodes:            1_000_000,
		MaxContainerEntries: 1_000_000,
		MaxBlobBytes:        64 << 20, // 64 MiB
		MaxIntegerBytes:     1024,
	}
}
