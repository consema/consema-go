package main

// This file pins the product version string of the Go CLI (RFC 0015 §3.3:
// the release version string, without git hashes or build metadata; the
// `core.cli-output@1` decoder revalidates the MAJOR.MINOR.PATCH shape).
//
// Version policy (G5.6 decision, recorded here): the Rust CLI derives
// `PRODUCT_VERSION` from `CARGO_PKG_VERSION` (the workspace version). The Go
// module's version follows the product release train (RFC 0016 §9: the Go
// module version tracks the product releases), and this CLI ships as part of
// the 0.19.0 milestone, so the Go CLI reports "0.19.0". The constant is
// overridable at build time through `-ldflags "-X main.productVersion=..."`
// for release packaging; the envelope requires a semantic-version-shaped
// string either way.
var productVersion = "0.19.0"
