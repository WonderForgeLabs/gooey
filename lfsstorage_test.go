package gooey

// The other half of the LFS contract, and the half that was missing.
//
// TestEveryCheckoutFetchesLFS proves a job will FETCH the objects. It says
// nothing about whether the objects exist — and adding `*.gif` to
// .gitattributes without renormalizing left thirty files matching an LFS
// pattern while still committed as raw bytes. That is not a cosmetic
// mismatch: the clean filter converts the working-tree file to a pointer
// before comparing it to the stored blob, so pointer-vs-bytes reads as
// MODIFIED in every clone, forever, for a file nobody edited. CI's
// `git diff --exit-code` went red on thirty untouched binaries.
//
// It shipped because the check that was run — `git status` in an existing
// worktree — cannot observe it. Git caches stat information, so the filter
// never re-ran on files already checked out; a fresh clone has no cache and
// sees the truth. The verification was performed in the one environment
// blind to the failure.
//
// So the invariant is checked against the INDEX rather than against the
// working tree, which is the same question a fresh clone asks.

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// The pointer magic, per the LFS spec. A stored blob that does not start
// with this is raw content, whatever the attribute says.
const lfsPointerPrefix = "version https://git-lfs.github.com/spec/v1"

func TestEveryLFSTrackedFileIsStoredAsAPointer(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	// `git ls-files` with the eol/attr flags does not report filters, so
	// ask check-attr about every tracked file in one pass. -z on both ends
	// because paths may contain anything.
	tracked, err := exec.Command("git", "ls-files", "-z").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	paths := splitZ(tracked)
	if len(paths) == 0 {
		t.Fatal("git ls-files returned nothing — this test would pass vacuously, " +
			"which is the same defect it exists to catch")
	}

	attr := exec.Command("git", "check-attr", "--stdin", "-z", "filter")
	attr.Stdin = bytes.NewReader(tracked)
	out, err := attr.Output()
	if err != nil {
		t.Fatalf("git check-attr: %v", err)
	}
	// check-attr -z emits a flat NUL-separated stream of (path, attr, value).
	fields := splitZ(out)
	if len(fields)%3 != 0 {
		t.Fatalf("check-attr returned %d fields, not a multiple of 3", len(fields))
	}

	var lfsPaths []string
	for i := 0; i+2 < len(fields); i += 3 {
		if fields[i+1] == "filter" && fields[i+2] == "lfs" {
			lfsPaths = append(lfsPaths, fields[i])
		}
	}
	if len(lfsPaths) == 0 {
		t.Fatal("no tracked file matches an LFS pattern — .gitattributes lists " +
			"*.wav, *.gif and *.png, so either they all vanished or this test " +
			"stopped asking the question")
	}

	var bad []string
	for _, p := range lfsPaths {
		// The STAGED blob, not the file on disk. The file on disk is
		// supposed to be real content; the blob is what a clone receives.
		blob, err := exec.Command("git", "cat-file", "-p", ":"+p).Output()
		if err != nil {
			t.Errorf("%s: cat-file: %v", p, err)
			continue
		}
		if !strings.HasPrefix(string(blob), lfsPointerPrefix) {
			bad = append(bad, p)
		}
	}
	if len(bad) > 0 {
		shown := bad
		if len(shown) > 8 {
			shown = shown[:8]
		}
		t.Fatalf("%d of %d LFS-tracked files are stored as raw bytes, not pointers:\n"+
			"    %s%s\n"+
			"    Every clone will report these MODIFIED forever, because the clean\n"+
			"    filter makes a pointer out of the working-tree file and compares it\n"+
			"    to a blob that is not one. `git status` in an existing worktree does\n"+
			"    NOT show this — the stat cache skips the filter.\n"+
			"    Fix: git add --renormalize -- <paths> and commit.",
			len(bad), len(lfsPaths), strings.Join(shown, "\n    "),
			map[bool]string{true: "\n    …", false: ""}[len(bad) > len(shown)])
	}
	t.Logf("%d LFS-tracked files, all stored as pointers", len(lfsPaths))
}

func splitZ(b []byte) []string {
	s := strings.TrimSuffix(string(b), "\x00")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x00")
}
