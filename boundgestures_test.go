package gooey

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/input"
)

// gestureAttr matches a Gesture the tree actually binds, in either of the
// two places one can be written: a markup attribute and a Go struct
// field. A bound gesture is the only kind worth sweeping — the parser's
// own tests own the vocabulary, and this owns the CONSUMERS.
var gestureAttr = regexp.MustCompile(`Gesture(?:="|: *")([^"]*)"`)

// TestEveryBoundGestureCanActuallyBeProduced is the sweep #427's
// acceptance asks for, and it is the assertion that makes the parser's
// new strictness safe rather than merely correct.
//
// A gesture no terminal can send used to load cleanly and never fire —
// no error, no warning, nothing at runtime to tell it apart from a key
// you did not press. ParseGesture rejects those now, which turns a
// silent nothing into a LOAD ERROR. That trade is only worth making if
// nothing in the tree is standing on one, and when this was written two
// things were: apps/wysiwyg bound ctrl+j and ctrl+h — the bytes 0x0a and
// 0x08, which the decoder reads as enter and backspace — so half its
// hjkl move cluster had never worked.
//
// The sweep runs over the WHOLE TREE rather than one module, because a
// dead binding in an app is exactly as invisible as one in the
// framework, and apps/* are vetted but not tested in CI.
//
// A template expression is skipped, not parsed: `Gesture="{{.Key}}"` is
// resolved at build time from a binding, and there is nothing static
// here to check.
func TestEveryBoundGestureCanActuallyBeProduced(t *testing.T) {
	type site struct{ file, gesture string }
	var sites []site

	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Dot-directories at EVERY depth, not just the top: .claude/
		// worktrees hold whole other checkouts of this repo, and reading
		// one would sweep somebody else's branch. vendor/ is third-party.
		if d.IsDir() {
			if name := d.Name(); p != root && (strings.HasPrefix(name, ".") || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case filepath.Ext(p) == ".gooey":
		// _test.go IS EXCLUDED, and not for convenience: several tests
		// bind a deliberately invalid gesture to exercise the error path
		// ("ctrl+nope", "wat+z", "ctrl+"), and one regexp literal in
		// apps/soundboard happens to contain the word. A test standing on
		// a dead gesture is also a different problem — it goes red on its
		// own the moment the parser refuses it, which is how the ctrl++
		// case in input_test.go surfaced. What has no other alarm is a
		// SHIPPED binding.
		case filepath.Ext(p) == ".go" && !strings.HasSuffix(p, "_test.go"):
		default:
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		for _, m := range gestureAttr.FindAllStringSubmatch(string(b), -1) {
			g := m[1]
			if g == "" || strings.Contains(g, "{{") || strings.Contains(g, "{") {
				continue
			}
			sites = append(sites, site{rel, g})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// A floor, because a walk that found nothing would pass silently and
	// this test's whole value is coverage.
	if len(sites) < 50 {
		t.Fatalf("the sweep found only %d bound gestures; the tree binds far more, "+
			"so the walk or the pattern is wrong and a green result means nothing",
			len(sites))
	}

	seen := map[string]bool{}
	for _, s := range sites {
		if _, err := input.ParseGesture(s.gesture); err != nil {
			if key := s.file + " " + s.gesture; !seen[key] {
				seen[key] = true
				t.Errorf("%s binds %q, which ParseGesture refuses: %v", s.file, s.gesture, err)
			}
		}
	}
	t.Logf("swept %d bound gestures across the tree", len(sites))
}
