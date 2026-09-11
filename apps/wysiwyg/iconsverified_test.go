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

			for _, m := range verifiedMismatches(recorded, onDisk) {
				t.Error(m)
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

	// THE ARM AIMS THE REAL FUNCTION, and the first version did not.
	// It perturbed the map and then asserted `recorded[one] !=
	// onDisk[one]` — a run of zeroes against a real digest, which proves
	// digestDir hashes SOMETHING and proves nothing about the comparison
	// in the test above, whose loops were inline in the subtest and were
	// never called here. Invert that comparison or key it on the wrong
	// name and this arm still passed: the exact "a negative assertion
	// passes for any reason" it was written against, one level down.
	// Raised in review of #487. verifiedMismatches is now the shared
	// function both aim at, the way docsLabelCollisions already was.
	if got := verifiedMismatches(recorded, onDisk); len(got) != 0 {
		t.Fatalf("the manifest and the directory disagree BEFORE this arm "+
			"perturbs anything, so what it measures below is not the "+
			"perturbation:\n\t%s", strings.Join(got, "\n\t"))
	}
	// ALL THREE DIRECTIONS, because the baseline exercises none of them.
	// Every icon in the tree has a line and every line has an icon, so
	// the two set-difference branches never run on real data — deleting
	// either was measured SILENT with only the digest arm here. A branch
	// no arm reaches is a branch that can be removed without a test
	// noticing, which is what this whole function exists to prevent.
	for _, tc := range []struct {
		name    string
		perturb func(rec, disk map[string]string)
		wants   string
	}{
		{
			name: "a digest that no longer matches",
			perturb: func(rec, _ map[string]string) {
				rec[one] = strings.Repeat("0", 64)
			},
			wants: one,
		},
		{
			name: "an icon in the directory with no line",
			perturb: func(rec, _ map[string]string) {
				delete(rec, one)
			},
			wants: one,
		},
		{
			name: "a line for an icon that is gone",
			perturb: func(rec, _ map[string]string) {
				rec["zzz-not-an-icon.svg"] = strings.Repeat("0", 64)
			},
			wants: "zzz-not-an-icon.svg",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := make(map[string]string, len(recorded))
			for k, v := range recorded {
				rec[k] = v
			}
			tc.perturb(rec, onDisk)
			got := verifiedMismatches(rec, onDisk)
			if len(got) == 0 {
				t.Fatalf("the comparison reported nothing for %q, so that "+
					"branch of it could be deleted and every assertion in "+
					"the test above would stay green", tc.name)
			}
			if len(got) != 1 || !strings.Contains(got[0], tc.wants) {
				t.Errorf("one perturbation should report exactly one problem "+
					"naming %s, and the comparison reported %d: %s",
					tc.wants, len(got), strings.Join(got, "; "))
			}
		})
	}
}

// verifiedMismatches is the set comparison itself, extracted so the
// must-fire arm can aim it rather than aiming a restatement of it.
//
// BOTH DIRECTIONS, because the two failures are different mistakes: an
// icon in the directory with no line is one nobody compared, and a line
// with no icon is a claim about a file that is gone. Returns one message
// per problem, ordered, so a caller can report them all rather than
// stopping at the first.
func verifiedMismatches(recorded, onDisk map[string]string) []string {
	var out []string

	names := make([]string, 0, len(onDisk))
	for n := range onDisk {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		want, ok := recorded[n]
		if !ok {
			out = append(out, fmt.Sprintf("%s is in the directory and not in "+
				"%s — nobody compared it against src/icons/%s, and the "+
				"LICENSE's dated claim does not cover it. Run the diff the "+
				"LICENSE gives, then add the line", n, verifiedManifest, n))
			continue
		}
		if want != onDisk[n] {
			out = append(out, fmt.Sprintf("%s does not hash to the bytes that "+
				"were compared:\n\trecorded %s\n\ton disk  %s\nEither the "+
				"file was replaced without re-running the diff, or upstream "+
				"moved and the copy here is now the old one. The LICENSE's "+
				"recipe says which", n, want, onDisk[n]))
		}
	}

	gone := make([]string, 0, len(recorded))
	for n := range recorded {
		if _, ok := onDisk[n]; !ok {
			gone = append(gone, n)
		}
	}
	sort.Strings(gone)
	for _, n := range gone {
		out = append(out, fmt.Sprintf("%s records %s, which is not in the "+
			"directory — a verification of a file that is gone",
			verifiedManifest, n))
	}
	return out
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
