package main

// The fsio atomic-write engine (RFC 0015 §10; mirror of the Rust bin's
// fsio.rs). The CLI never writes a target file through any other path:
// same-directory unique temporary file with restricted permissions, atomic
// replacement per OS semantics, read-back target-digest verification, and
// the frozen write policy (symlink/junction rejection, read-only targets,
// directory targets, permission and I/O failures — all exit 4).
//
// The temporary file is named `{name}.consema-{pid}-{nonce}.tmp`; pid and
// nonce appear only in the temporary file name, never in any output record
// (RFC 0015 §3.3).

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"consema.dev/consema/protocol"
)

// writeOptions carries the frozen write-policy switches.
type writeOptions struct {
	// FollowSymlinks authorizes writing through symlink/junction targets
	// (the future --follow-symlinks flag; v1 rejects them by default, RFC
	// 0015 §10).
	followSymlinks bool
}

func defaultWriteOptions() writeOptions {
	return writeOptions{followSymlinks: false}
}

// WriteError is one frozen write-policy failure carrying the cli.write.*
// code (RFC 0015 §13.1; all exit 4).
type WriteError struct {
	// Code is the frozen cli.write.* code.
	Code string
	// Message is the deterministic human message.
	Message string
	// Target is the failing target path.
	Target string
}

// Error implements error.
func (e *WriteError) Error() string { return e.Message }

// writeAtomic atomically replaces one target file: same-directory temporary
// file + rename + read-back target-digest verification (RFC 0015 §9.3 steps
// 4-5). The returned digest is the verified digest of the on-disk bytes.
func writeAtomic(target string, bytes []byte,
	options writeOptions) (protocol.ContentDigest, *WriteError) {
	// Symlink/junction policy (RFC 0015 §10): write paths reject
	// symlink/junction targets AND every ancestor component by default;
	// --follow-symlinks authorizes explicitly. The walk uses the same raw
	// facts the inspect command reports (Lstat plus the Windows reparse-point
	// attribute), so a junction directory in the middle of the path is
	// refused, not just a final-component link (mirror of the Rust bin's
	// refuse_symlink_components; audit P1).
	if !options.followSymlinks {
		if err := refuseSymlinkComponents(target); err != nil {
			return protocol.ContentDigest{}, err
		}
	}
	// Directory targets are refused before any temp file is created.
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return protocol.ContentDigest{}, &WriteError{
			Code:    "cli.write.target-is-directory@1",
			Message: fmt.Sprintf("cannot replace '%s': the target is a directory", target),
			Target:  target,
		}
	}
	// Read-only targets are refused (RFC 0015 §10 read-only row).
	if info, err := os.Stat(target); err == nil {
		if isReadOnly(info) {
			return protocol.ContentDigest{}, &WriteError{
				Code:    "cli.write.read-only@1",
				Message: fmt.Sprintf("cannot replace '%s': the target is read-only", target),
				Target:  target,
			}
		}
	}
	directory := filepath.Dir(target)
	nonce, err := randomNonce()
	if err != nil {
		return protocol.ContentDigest{}, &WriteError{
			Code:    "cli.write.io@1",
			Message: fmt.Sprintf("cannot write '%s': random nonce failed: %v", target, err),
			Target:  target,
		}
	}
	tempPath := filepath.Join(directory,
		fmt.Sprintf("%s.consema-%d-%s.tmp", filepath.Base(target), os.Getpid(), nonce))
	file, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return protocol.ContentDigest{}, writeIOError(target, err)
	}
	cleanup := func() {
		file.Close()
		os.Remove(tempPath)
	}
	if _, err := file.Write(bytes); err != nil {
		cleanup()
		return protocol.ContentDigest{}, writeIOError(target, err)
	}
	if err := file.Sync(); err != nil {
		cleanup()
		return protocol.ContentDigest{}, writeIOError(target, err)
	}
	if err := file.Close(); err != nil {
		os.Remove(tempPath)
		return protocol.ContentDigest{}, writeIOError(target, err)
	}
	// Permissions/ownership: copy the target's existing permissions to the
	// temporary file before replacement (RFC 0015 §10).
	if info, err := os.Stat(target); err == nil {
		_ = os.Chmod(tempPath, info.Mode().Perm())
	}
	// Atomic replacement per OS semantics (POSIX rename; Windows
	// MoveFileEx REPLACE_EXISTING).
	if err := os.Rename(tempPath, target); err != nil {
		os.Remove(tempPath)
		return protocol.ContentDigest{}, writeIOError(target, err)
	}
	// Read back and verify the target digest (RFC 0015 §9.3 step 5).
	readBack, err := os.ReadFile(target)
	if err != nil {
		return protocol.ContentDigest{}, &WriteError{
			Code: "cli.write.io@1",
			Message: fmt.Sprintf(
				"read-back verification of '%s' failed after atomic replace: %v "+
					"(the file has been replaced and is not rolled back)", target, err),
			Target: target,
		}
	}
	digest := protocol.DigestOf(readBack)
	if !digest.Equal(protocol.DigestOf(bytes)) {
		return protocol.ContentDigest{}, &WriteError{
			Code: "core.source.patch-target-mismatch@1",
			Message: fmt.Sprintf(
				"read-back digest mismatch after atomic replace of '%s' "+
					"(environment diagnostic cli.write.io@1); the file has been "+
					"replaced and is not rolled back", target),
			Target: target,
		}
	}
	return digest, nil
}

func writeIOError(target string, err error) *WriteError {
	return &WriteError{
		Code:    "cli.write.io@1",
		Message: fmt.Sprintf("cannot write '%s': %v", target, err),
		Target:  target,
	}
}

// isReadOnly reports whether the target's permissions reject writing (the
// POSIX write bits absent; on Windows the read-only attribute maps to a
// read-only permission set).
func isReadOnly(info os.FileInfo) bool {
	return info.Mode().Perm()&0o222 == 0
}

// isSymlinkPath reports whether the path is a symlink or (on Windows) any
// reparse point (junction); the same fact the inspect command reports for
// the write policy (R-4).
func isSymlinkPath(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	return isReparsePoint(path)
}

// refuseSymlinkComponents refuses any symlink or junction component in the
// write path (R-4; mirror of the Rust bin's refuse_symlink_components,
// fsio.rs:548-591): the target itself and every ancestor prefix are
// inspected with Lstat plus the Windows reparse-point attribute, so a
// junction directory in the middle of the path is refused, not just a
// final-component link. A prefix that does not exist (a missing parent) is
// skipped — the temporary-file creation surfaces the real failure with its
// own classification; any other inspection failure (e.g. permission denial)
// is a refusal-worthy condition and classifies as cli.write.io@1 (Rust's
// classify: permission denial → cli.write.permission@1, everything else →
// cli.write.io@1).
//
// macOS system-temp carve-out (Rust fsio.rs:557-607): the walk stops once it
// reaches the system temp root, whose ancestors are exempt. On macOS
// os.TempDir() returns /var/folders/... and /var → /private/var is a system
// symlink sitting in every temp-dir path, so without the carve-out every
// write under the system temp tree would be refused. Components strictly
// inside the temp tree are still inspected, so user-controlled symlink and
// junction components — including the probe tests' symlinks created under
// the temp dir — are refused exactly as before; paths outside the temp root
// are walked to the filesystem root.
func refuseSymlinkComponents(target string) *WriteError {
	prefix := target
	for {
		// R-4 macOS system-temp carve-out: the walk reached the temp root —
		// every component strictly below it has already been inspected, and
		// the root and its ancestors are system-owned (macOS /var →
		// /private/var).
		if isSystemTempPrefix(prefix) {
			return nil
		}
		info, err := os.Lstat(prefix)
		switch {
		case err == nil:
			if info.Mode()&os.ModeSymlink != 0 || isReparsePoint(prefix) {
				return &WriteError{
					Code: "cli.write.symlink-policy@1",
					Message: fmt.Sprintf("refusing to write through symlink or junction target '%s' "+
						"(--follow-symlinks authorizes explicitly)", prefix),
					Target: prefix,
				}
			}
		case !os.IsNotExist(err):
			code := "cli.write.io@1"
			if os.IsPermission(err) {
				code = "cli.write.permission@1"
			}
			return &WriteError{
				Code:    code,
				Message: fmt.Sprintf("cannot inspect '%s': %v", prefix, err),
				Target:  target,
			}
		}
		parent := filepath.Dir(prefix)
		if parent == prefix {
			break
		}
		prefix = parent
	}
	return nil
}

// isSystemTempPrefix reports whether prefix is the system temp root or one
// of its ancestors — the exempt region of the R-4 walk (see
// refuseSymlinkComponents). Both spellings are compared: the raw
// os.TempDir() path (on macOS /var/folders/..., from $TMPDIR) and its
// canonical form (/private/var/folders/..., after resolving the /var →
// /private/var system symlink). Resolved once per process.
func isSystemTempPrefix(prefix string) bool {
	raw := os.TempDir()
	if strings.HasPrefix(raw, prefix) {
		return true
	}
	if resolved, err := filepath.EvalSymlinks(raw); err == nil {
		if strings.HasPrefix(resolved, prefix) {
			return true
		}
	}
	return false
}

// randomNonce returns one 16-byte hex nonce for the temporary file name.
func randomNonce() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(nonce[:]), nil
}
