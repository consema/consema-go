package document

import (
	"crypto/sha256"
	"encoding/hex"
)

// ContentDigest is the stable SHA-256 identity of exact raw source bytes
// (document source.rs).
type ContentDigest struct {
	bytes [32]byte
}

// DigestOf computes the digest of exact raw bytes.
func DigestOf(data []byte) ContentDigest {
	return ContentDigest{bytes: sha256.Sum256(data)}
}

// ContentDigestFromBytes constructs a digest value from an already decoded
// 32-byte record.
func ContentDigestFromBytes(bytes [32]byte) ContentDigest {
	return ContentDigest{bytes: bytes}
}

// Algorithm returns the digest algorithm identifier frozen by the v1
// source contract.
func (d ContentDigest) Algorithm() string { return "sha256" }

// Bytes returns the exact 32 digest bytes.
func (d ContentDigest) Bytes() [32]byte { return d.bytes }

// Hex returns the lowercase hexadecimal representation.
func (d ContentDigest) Hex() string {
	return hex.EncodeToString(d.bytes[:])
}

// Equal reports whether two digests are identical.
func (d ContentDigest) Equal(other ContentDigest) bool {
	return d.bytes == other.bytes
}
