package yaml

// ---------------------------------------------------------------------------
// Go native fuzz targets (milestone 0.19.0 G5.4; docs/go-implementation-plan.md
// §2.6; roadmap §22.4:1903 release-candidate fuzz clean-run, §22.4:1908
// "XML/YAML/HCL/binary plist 专项 threat tests"). Discipline mirrors the
// Rust fuzz targets of 0.13.0 (docs/fuzz-evidence-0.13.0.md §2) and the Go
// core/graph/protocol targets of 0.14.0 (G0.5): resource limits are fixed at
// the production defaults, limit failures are passes, and property
// assertions detect closure violations.
//
// The alias target is the YAML-specific high-value face: the Rust alias-bomb
// corpus (yaml_hardening.rs) showed that a document can encode exponentially
// growing references in linear source; the parser must never panic, must
// bound the recorded aliases exactly to the emitted references, and must
// never invent an alias.
// ---------------------------------------------------------------------------

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"math/rand"
	"strings"
	"testing"

	"consema.dev/consema/document"
)

// FuzzParse feeds arbitrary bytes to the YAML 1.2 parser under the
// production default limits. A successful parse must render byte-exactly.
func FuzzParse(f *testing.F) {
	seeds := [][]byte{
		[]byte("a: 1\nb: [2, 3]\nc:\n  d: 4\n"),
		[]byte("---\nname: service\nitems: [one, two]\n"),
		[]byte("name: 配置\nemoji: \"🦀\"\n"),
		[]byte("root: &root [one, *root]\n"),
		[]byte("text: |-\n  first\n  second\n"),
		[]byte("[*missing]\n"),
		[]byte("[unterminated\n"),
		[]byte("{key: value\n"),
		[]byte("%YAML 1.3\n---\nvalue\n"),
		[]byte("key:\n\t- invalid\n"),
		[]byte("\xff"),
		[]byte("\xff\xfea"),
		[]byte(""),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, failure := Parse(data, Yaml12CoreV1, document.DefaultParseLimits())
		if failure != nil {
			return // fatal formation (incl. limit failures) is a pass
		}
		if !bytes.Equal(doc.Render(), data) {
			t.Fatalf("render closure violated (%d bytes, rendered %d):\ninput:  %q\noutput: %q",
				len(data), len(doc.Render()), data, doc.Render())
		}
		if doc.AliasCount() > len(data) {
			t.Fatalf("alias count %d exceeds the source bytes %d", doc.AliasCount(), len(data))
		}
	})
}

// FuzzAlias generates bounded anchor/alias YAML documents and asserts the
// alias-bomb invariant: on a successful parse the recorded alias count
// equals exactly the emitted references (the parser invents no aliases),
// and the render stays byte-exact.
func FuzzAlias(f *testing.F) {
	for _, seed := range []struct {
		seed uint64
		blob string
	}{
		{1, "base: &base [zero, one]\nlevel1: &level1 [*base, *base]\nroot: [*level1, *level1]\n"},
		{2, "self: &self [one, *self]\n"},
		{3, "a: &a {x: *a}\n"},
	} {
		f.Add(seed.seed, []byte(seed.blob))
	}
	f.Add(uint64(1), []byte("seed"))
	f.Add(uint64(0), []byte{})
	f.Add(uint64(0xdeadbeef), []byte("consema"))
	f.Add(uint64(2026), []byte("0.19.0 G5.4"))
	f.Fuzz(func(t *testing.T, seed uint64, blob []byte) {
		emitted := 0
		source := aliasSource(seed, blob, &emitted)
		doc, failure := Parse(source, Yaml12CoreV1, document.DefaultParseLimits())
		if failure != nil {
			return // deep or cyclic alias inputs may fail limits; a pass
		}
		if !bytes.Equal(doc.Render(), source) {
			t.Fatalf("alias render closure violated:\ninput:  %q\noutput: %q", source, doc.Render())
		}
		if doc.AliasCount() != emitted {
			t.Fatalf("alias count %d != emitted references %d\nsource:\n%s",
				doc.AliasCount(), emitted, source)
		}
	})
}

// aliasSource deterministically generates one bounded anchor/alias
// document: up to 6 anchors, each a sequence of 1-4 items that are either
// scalars or references to strictly earlier anchors. The generator keeps
// every anchor at depth ≤ 2 so the default limits never truncate a valid
// corpus, and counts the emitted references for the invariant.
func aliasSource(seed uint64, blob []byte, emitted *int) []byte {
	h := fnv.New64a()
	h.Write(blob)
	r := rand.New(rand.NewSource(int64(seed ^ h.Sum64())))
	var builder strings.Builder
	anchors := 1 + r.Intn(6)
	for index := 0; index < anchors; index++ {
		fmt.Fprintf(&builder, "a%d: &a%d [", index, index)
		items := 1 + r.Intn(4)
		for item := 0; item < items; item++ {
			if item > 0 {
				builder.WriteString(", ")
			}
			if index > 0 && r.Intn(3) == 0 {
				target := r.Intn(index) // strictly earlier anchor
				fmt.Fprintf(&builder, "*a%d", target)
				*emitted++
			} else {
				fmt.Fprintf(&builder, "%d", r.Intn(1000))
			}
		}
		builder.WriteString("]\n")
	}
	return []byte(builder.String())
}
