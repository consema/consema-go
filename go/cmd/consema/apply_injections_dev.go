//go:build !release

package main

import (
	"fmt"
	"os"
)

// fromEnv reads the documented process-level injection seam from the
// environment (RFC 0015 §5.4; the e2e tests exercise it through the built
// binary). This file is the dev/test variant — a production binary built
// with `-tags release` compiles apply_injections_release.go instead, which
// never reads the environment (G114, adversarial audit 2026-08-14, rs
// G045-aligned).
func (i *applyInjections) fromEnv() {
	if value := os.Getenv(interruptAfterEnv); value != "" {
		if index, err := parseInt(value); err == nil {
			i.interruptAfter = &index
		}
	}
	switch os.Getenv(writeFailureEnv) {
	case "permission":
		i.writeFailure = &writeFailureInjection{
			code:    "cli.write.permission@1",
			message: "injected permission failure (" + writeFailureEnv + "=permission)",
		}
	case "io":
		i.writeFailure = &writeFailureInjection{
			code:    "cli.write.io@1",
			message: "injected disk-full failure (" + writeFailureEnv + "=io)",
		}
	}
}

func parseInt(text string) (int, error) {
	value := 0
	for _, ch := range text {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("not a number")
		}
		value = value*10 + int(ch-'0')
	}
	return value, nil
}
