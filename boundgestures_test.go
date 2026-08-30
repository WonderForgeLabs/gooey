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
// three places one can be written. A bound gesture is the only kind
// worth sweeping — the parser's own tests own the vocabulary, and this
// owns the CONSUMERS.
// The three spellings a bound gesture is written in: a markup attribute
// (`Gesture="..."`), a struct field in a literal (`Gesture: "..."`), and
// an assignment (`kb.Gesture = "..."`). The third was missing, and its
// absence was invisible because the comment claimed the set was two.
var gestureAttr = regexp.MustCompile(`Gesture(?:="|:\s*"|\s*=\s*")([^"]*)"`)

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
			// ONLY a template expression is skipped, and only on the
			// `{{` that starts one. Skipping every string containing a
			// brace — which this did — put a hole in the sweep exactly
			// where the parser is now strict: `ctrl+{` and `ctrl+}` are
			// two of the 46 refused gestures, and bare `{` and `}` are
			// perfectly good keys. A blind spot shaped like the thing
			// being looked for.
			if g == "" || strings.Contains(g, "{{") {
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

// TestTheGestureMatcherSeesEveryFormItClaims pins the regexp against the
// spellings it says it covers.
//
// Nothing in the tree uses the assignment form today, and no binding
// uses a brace key — so widening the matcher and narrowing the skip
// changed the swept count by zero, and both fixes would have been
// unfireable claims. This is the test that can fail: it feeds the
// matcher each form directly rather than waiting for the tree to grow
// one.
func TestTheGestureMatcherSeesEveryFormItClaims(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"markup attribute", `<KeyBinding Gesture="ctrl+s" Command="{{.Save}}"/>`, "ctrl+s"},
		{"struct field", `kb := gooey.KeyBinding{Gesture: "ctrl+s"}`, "ctrl+s"},
		{"assignment", `kb.Gesture = "ctrl+s"`, "ctrl+s"},
		{"spaced assignment", `kb.Gesture  =  "alt+j"`, "alt+j"},
		// A brace KEY, which the skip used to swallow along with the
		// template expressions.
		{"brace key", `<KeyBinding Gesture="ctrl+{"/>`, "ctrl+{"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := gestureAttr.FindStringSubmatch(tc.src)
			if m == nil {
				t.Fatalf("the matcher does not see %s at all: %s", tc.name, tc.src)
			}
			if m[1] != tc.want {
				t.Errorf("matched %q, want %q", m[1], tc.want)
			}
			if strings.Contains(m[1], "{{") {
				t.Errorf("matched a template expression %q, which is not a "+
					"static gesture", m[1])
			}
		})
	}

	// And the skip still drops a bound template expression, which is the
	// one thing it is for.
	m := gestureAttr.FindStringSubmatch(`<KeyBinding Gesture="{{.Key}}"/>`)
	if m == nil || !strings.Contains(m[1], "{{") {
		t.Fatal("a bound Gesture no longer looks like a template expression, so " +
			"the sweep would try to parse one")
	}
}
