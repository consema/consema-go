package main

// The fsio write-policy tests (mirror of the Rust bin's fsio.rs tests,
// fsio.rs:1140-1203): the atomic-write engine refuses symlink/junction
// targets AND intermediate path components by default
// (cli.write.symlink-policy@1), names the offending component, never writes
// through the link, and leaves no temporary-file residue. Windows skips the
// symlink probes because creating a true symlink needs privileges (the
// Rust-side junction probe stands in there; the walk itself is exercised on
// every platform through the Windows reparse-point check).
//
// The system-temp carve-out is not probed directly: the scratch dirs of
// these tests live under the temp root, so a refusal of user components
// strictly inside the temp tree proves the carve-out boundary (Rust
// fsio.rs:557-607).

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"consema.dev/consema/protocol"
)

func TestWriteAtomicRefusesSymlinkTargetsAndComponentsByDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs privileges on Windows (the Rust junction probe covers it)")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(real, "app.conf")
	if err := os.WriteFile(app, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(dir, "link-dir")
	if err := os.Symlink(real, linkDir); err != nil {
		t.Fatal(err)
	}

	// A symlink as the final component of the write path.
	linkFile := filepath.Join(dir, "link-file")
	if err := os.Symlink(app, linkFile); err != nil {
		t.Fatal(err)
	}
	_, err := writeAtomic(linkFile, []byte("new"), defaultWriteOptions())
	if err == nil {
		t.Fatal("write through a final-component symlink must fail")
	}
	writeError := err
	if writeError.Code != "cli.write.symlink-policy@1" || writeError.Target != linkFile {
		t.Fatalf("error = %v", writeError)
	}

	// A symlink as an intermediate component (write through the link dir):
	// the offending component is named, the real file is untouched.
	through := filepath.Join(linkDir, "other.conf")
	_, err = writeAtomic(through, []byte("new"), defaultWriteOptions())
	if err == nil {
		t.Fatal("write through an intermediate symlink must fail")
	}
	writeError = err
	if writeError.Code != "cli.write.symlink-policy@1" || writeError.Target != linkDir {
		t.Fatalf("error = %v", writeError)
	}

	// Nothing was written through the link.
	got, readErr := os.ReadFile(app)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "old" {
		t.Fatalf("real file = %q", got)
	}
	if _, err := os.Stat(filepath.Join(real, "other.conf")); !os.IsNotExist(err) {
		t.Fatalf("other.conf must not exist: %v", err)
	}
	assertNoTempResidue(t, real)
}

func TestWriteAtomicFollowSymlinksResolvesAndWritesTheRealFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs privileges on Windows")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	realFile := filepath.Join(real, "app.conf")
	if err := os.WriteFile(realFile, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(dir, "link-dir")
	if err := os.Symlink(real, linkDir); err != nil {
		t.Fatal(err)
	}
	options := writeOptions{followSymlinks: true}
	digest, writeErr := writeAtomic(filepath.Join(linkDir, "app.conf"), []byte("new content"), options)
	if writeErr != nil {
		t.Fatalf("authorized write through the link: %v", writeErr)
	}
	if !digest.Equal(protocol.DigestOf([]byte("new content"))) {
		t.Fatalf("digest = %v", digest)
	}
	got, readErr := os.ReadFile(realFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "new content" {
		t.Fatalf("real file = %q", got)
	}
	if info, lstatErr := os.Lstat(linkDir); lstatErr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link entry must stay a symlink: %v %v", info, lstatErr)
	}
	assertNoTempResidue(t, real)
}

func TestWriteAtomicMissingParentFailsWithIoClassification(t *testing.T) {
	// A missing intermediate prefix is skipped by the walk (the
	// temporary-file creation surfaces the real failure); the parent dir
	// does not exist, so the write fails with cli.write.io@1.
	dir := t.TempDir()
	target := filepath.Join(dir, "missing", "app.conf")
	_, err := writeAtomic(target, []byte("x"), defaultWriteOptions())
	if err == nil {
		t.Fatal("missing parent must fail")
	}
	if err.Code != "cli.write.io@1" {
		t.Fatalf("error = %v", err)
	}
}

func assertNoTempResidue(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if len(name) >= len(".consema-") && name[len(name)-4:] == ".tmp" {
			t.Fatalf("temp residue left behind: %s", name)
		}
	}
}
