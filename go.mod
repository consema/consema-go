// Module consema: the Go implementation of the language-neutral Consema
// contracts (RFC 0016 §3.1; docs/go-implementation-plan.md §0.2). One module
// carries the SDK and the future Go CLI, mirroring crates/consema's facade +
// [[bin]] structure.
//
// Minimum Go version frozen at 0.14.0 per docs/go-implementation-plan.md
// §1.3 ("go.mod 最低版本：0.14.0 冻结...建议 go 1.26"). Stdlib-only policy:
// zero third-party dependencies (plan §1.3; RFC 0016 §10).
module consema.dev/consema

go 1.26
