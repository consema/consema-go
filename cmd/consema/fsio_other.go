//go:build !windows

package main

// Non-Windows platforms have no reparse-point attribute beyond symlinks,
// which os.Lstat already reports.

func isReparsePoint(path string) bool { return false }
