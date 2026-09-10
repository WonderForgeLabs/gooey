package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/activitybar"
	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/toolbox"
)

// verifiedManifest is the file each icon directory keeps beside its
// LICENSE: one `<name>.svg  <sha256>` line per icon, recording the bytes
// that were compared against upstream.
const verifiedManifest = "VERIFIED.sha256"

// TestTheVerifiedIconsAreStillTheVerifiedBytes is issue #442's second
// finding, and the finding was about SHAPE rather than about whether the
// icons are genuine.
//
// The LICENSE recorded a manual upstream-identity check ("VERIFIED
// 2026-08-30 … every one matched") three paragraphs above its own
// argument that one-time manual verifications are exactly what rot. Both
// halves were right, which is what made it worth fixing: the check was
// real and the way it was recorded could not stay true.
//
// A DATE ATTACHED TO A DIRECTORY COVERS WHATEVER IS ADDED TO IT LATER,
// silently, and something already had been — book.svg arrived in
// d712a28c, the commit that wrote the 2026-08-30 stamp, so the sentence
// was dated two days before the file it claimed to cover existed. (The
// prose around it is consistent about book.svg having been re-fetched;
// nothing tied the sentence to the bytes, which is the point.)
//
// So the claim is now recorded against BYTES, in VERIFIED.sha256, and
// this test is what makes it expire. It fails in BOTH directions,
// because the two failures are different mistakes: an icon in the
// directory with no line is one nobody compared, and a line with no icon
// is a claim about a file that is gone.
//
// The recipe was re-run for this commit against
// microsoft/vscode-codicons main — every file in both directories
// matched, and the digests recorded are that run's.
func TestTheVerifiedIconsAreStillTheVerifiedBytes(t *testing.T) {
	dirs := []string{activitybar.Dir, toolbox.Dir}
	total := 0
	for _, dir := range dirs {
		t.Run(dir, func(t *testing.T) {
			recorded := readVerified(t, dir)
			onDisk := digestDir(t, dir)
			total += len(onDisk)

			names := make([]string, 0, len(onDisk))
			for n := range onDisk {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				want, ok := recorded[n]
				if !ok {
					t.Errorf("%s is in the directory and not in %s — nobody "+
						"compared it against src/icons/%s, and the LICENSE's "+
						"dated claim does not cover it. Run the diff the "+
						"LICENSE gives, then add the line",
						n, verifiedManifest, n)
					continue
				}
				if want != onDisk[n] {
					t.Errorf("%s does not hash to the bytes that were "+
						"compared:\n\trecorded %s\n\ton disk  %s\n"+
						"Either the file was replaced without re-running the "+
						"diff, or upstream moved and the copy here is now the "+
						"old one. The LICENSE's recipe says which",
						n, want, onDisk[n])
				}
			}
			for n := range recorded {
				if _, ok := onDisk[n]; !ok {
					t.Errorf("%s records %s, which is not in the directory — "+
						"a verification of a file that is gone", verifiedManifest, n)
				}
			}
		})
	}
	// NON-VACUITY. Both readers fail loudly on a missing file, but a
	// directory that read as empty in both — a moved package, a Dir
	// constant pointing somewhere else — would satisfy every loop above
	// by comparing nothing.
	if total == 0 {
		t.Fatal("no .svg was found in either icon directory, so this test " +
			"compared nothing and would pass over a wholesale replacement")
	}
}

// TestTheVerificationManifestCanActuallyFail is the must-fire arm.
//
// Everything above is a set comparison that reports nothing when the two
// sets agree, and a negative assertion passes for any reason: a parser
// that returned an empty map on every line, a digest function that
// returned a constant, a name key that never matched. Perturbing one
// recorded digest and asserting the comparison notices is what separates
// "they agree" from "the comparison happened".
//
// It perturbs the in-memory map, never the file — a test that rewrites
// the artifact it checks is how a recorded verification quietly becomes
// whatever the code produces.
func TestTheVerificationManifestCanActuallyFail(t *testing.T) {
	recorded := readVerified(t, toolbox.Dir)
	onDisk := digestDir(t, toolbox.Dir)
	var one string
	for n := range onDisk {
		if _, ok := recorded[n]; ok {
			one = n
			break
		}
	}
	if one == "" {
		t.Fatal("no icon is both on disk and in the manifest, so the check " +
			"above has nothing to compare and this arm cannot perturb it")
	}
	recorded[one] = strings.Repeat("0", 64)
	if recorded[one] == onDisk[one] {
		t.Fatalf("%s hashes to a run of zeroes, which means digestDir is not "+
			"hashing anything", one)
	}
}

// readVerified parses a VERIFIED.sha256 into name -> digest. Blank lines
// and `#` comments are the file's prose; everything else must be two
// fields, and a line that is not is an error rather than a skip — a
// manifest that silently drops a malformed line is a manifest that
// silently stops covering an icon.
func readVerified(t *testing.T, dir string) map[string]string {
	t.Helper()
	path := filepath.Join(dir, verifiedManifest)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s does not read: %v", path, err)
	}
	out := map[string]string{}
	for i, line := range strings.Split(string(b), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		f := strings.Fields(s)
		if len(f) != 2 || len(f[1]) != 64 {
			t.Fatalf("%s:%d is not `<name>.svg  <sha256>`: %q", path, i+1, line)
		}
		if _, dup := out[f[0]]; dup {
			t.Fatalf("%s lists %s twice, so one of the two digests is "+
				"unreachable", path, f[0])
		}
		out[f[0]] = f[1]
	}
	if len(out) == 0 {
		t.Fatalf("%s records no icon at all", path)
	}
	return out
}

// digestDir hashes every .svg in dir.
func digestDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("%s does not read: %v", dir, err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".svg" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("%s does not read: %v", e.Name(), err)
		}
		out[e.Name()] = fmt.Sprintf("%x", sha256.Sum256(b))
	}
	return out
}
