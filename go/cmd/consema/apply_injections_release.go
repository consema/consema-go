//go:build release

package main

// fromEnv is compiled out of release builds: a production binary built
// with `-tags release` never reads CONSEMA_APPLY_INTERRUPT_AFTER or
// CONSEMA_APPLY_WRITE_FAILURE, so a stray environment variable can never
// inject a fake write failure or interruption (G114, adversarial audit
// 2026-08-14, rs G045-aligned — the seam exists only in dev/test builds,
// where the e2e tests exercise it). RELEASING.md §3 documents the
// `-tags release` requirement for the future goreleaser binary packaging.
func (i *applyInjections) fromEnv() {
	// Release build: no injection seam.
}
