// Module consema: the Go implementation of the language-neutral Consema
// contracts (RFC 0016 §3.1; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §0.2). One module
// carries the SDK and the future Go CLI, mirroring consema-rs/consema's facade +
// [[bin]] structure.
//
// Minimum Go version is 1.26, frozen at 0.14.0 per RFC 0020 §9.2 and
// https://github.com/consema/consema/blob/main/docs/support-policy.md §2
// (https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
// §1.3 "建议 go 1.26"). A 2026-08-12 experiment lowered the directive to
// 1.24 (verified 1.24/1.25 pass), but the 2026-08-13 adversarial audit
// (G001) ruled to restore 1.26: the authoritative policy documents (RFC
// 0020 §9.2, support-policy.md, pilot-go-0.19.0.md) freeze
// `go 1.26`, and the CI go-matrix legs (1.26.0 declared minimum + 1.26.5
// matrix-pinned — R19, wave-4 2026-08-15: go.dev's current stable is
// 1.26.6, so the old "1.26.5 current stable" leg was never satisfied; the
// docs state the frozen matrix fact and the matrix upgrade is post-1.0.0
// per RFC 0020 §9.2; G031, adversarial audit 2026-08-14 — the minimum leg
// was '1.26.x', which resolved to the latest 1.26 patch and never
// exercised the declared minimum) are genuine under GOTOOLCHAIN=auto.
// Stdlib-only
// policy: zero third-party dependencies (plan §1.3; RFC 0016 §10).
module consema.dev/consema

go 1.26
