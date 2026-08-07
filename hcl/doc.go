// Package hcl implements lossless `hcl.native@1` and `hcl.tfvars@1`
// documents under the RFC 0014 boundary.
//
// The two profiles share one syntax system — the HCL Native Syntax as
// frozen by HashiCorp's `hclsyntax/spec.md` — and one native semantic
// model: body, attribute, block, label, expression, and template facts
// (RFC 0014 §6). This differs from the plist family (RFC 0013), where two
// profiles own disjoint syntax systems over one value model: here the two
// profiles own one grammar, and `hcl.tfvars@1` is `hcl.native@1` under one
// structural restriction — the top level of a tfvars document admits
// attributes only, never blocks (RFC 0014 §5).
//
// The profile is selected by the caller before formation. Neither the `.tf`
// nor the `.tfvars` extension ever selects a profile, representation, or
// encoding, and Terraform's `.tfvars.json` convention (JSON-based HCL) is
// an explicit v1 exclusion (RFC 0014 §1, §14).
//
// Both profiles are formation-only documents: Consema parses, preserves,
// and queries HCL syntax and structure but never evaluates it. Variables,
// function calls, template interpolation and directives, and
// for-expressions are native content with exact source identity; no
// evaluator exists anywhere in parse, query, projection, materialization,
// or edit (RFC 0014 §1, hard gate 1).
//
// The source contract (RFC 0014 §2) is Unicode text in UTF-8 without a
// byte-order mark: a BOM is Recovered with `hcl.parse.byte-order-mark@1`,
// invalid UTF-8 is a fatal formation failure, a lone CR is never a
// newline, and the encoding is always UTF-8, always selected before
// formation.
//
// The feature surface is complete: the native semantic model with its
// exact-span double preservation, the self-owned tokenizer with the
// 30-kind lossless piece assembly (RFC 0014 §7.2), the body/expression
// grammar with recovery semantics, the profile layer, and the query
// (RFC 0014 §7), projection (RFC 0014 §8), materialization (RFC 0014 §9),
// and edit (RFC 0014 §10) operation surfaces. The caller-side explicit
// encoding selection (HclEncodingSelection) is part of the frozen surface:
// Parse rejects any non-UTF-8 selection before formation with the fatal
// `hcl.parse.encoding@1` source-contract diagnostic (RFC 0014 §2).
package hcl
