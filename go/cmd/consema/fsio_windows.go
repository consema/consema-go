//go:build windows

package main

// Windows-only reparse-point fact (mirror of the Rust bin's
// `is_symlink_fact` FILE_ATTRIBUTE_REPARSE_POINT check): junctions and
// other reparse points are reported as symlink facts for the write policy
// (RFC 0015 §10 symlink row).

import "syscall"

// isReparsePoint reports whether the path carries the Windows
// FILE_ATTRIBUTE_REPARSE_POINT attribute (junctions and mount points; Go's
// Lstat only reports true symlinks).
func isReparsePoint(path string) bool {
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attributes, err := syscall.GetFileAttributes(pointer)
	if err != nil {
		return false
	}
	return attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
