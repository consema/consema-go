// Module consema: the Go implementation of the language-neutral Consema
// contracts (RFC 0016 §3.1; docs/go-implementation-plan.md §0.2). One module
// carries the SDK and the future Go CLI, mirroring consema-rs/crates/consema's facade +
// [[bin]] structure.
//
// Minimum Go version is 1.24 — the empirical floor, not the plan's
// suggestion: docs/go-implementation-plan.md §1.3 ("go.mod 最低版本：0.14.0
// 冻结...建议 go 1.26") suggested go 1.26, but verified 2026-08-12 that
// gofmt/vet/build/test/race all pass on go1.24.13 and go1.25.12, so the
// directive is lowered to 1.24 to make the CI matrix legs (1.24.x/1.25.x)
// genuine under GOTOOLCHAIN=auto (ci-go.yml go-matrix job). Stdlib-only
// policy: zero third-party dependencies (plan §1.3; RFC 0016 §10).
module consema.dev/consema

go 1.24
