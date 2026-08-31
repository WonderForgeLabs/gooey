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
//
// \b ON THE LEFT, so `HintGesture="…"`, `DefaultGesture: "…"` and a doc
// comment sampling `Gesture: "ctrl+j"` in a non-test .go file are not
// swept as if they were shipped bindings. Nothing in the tree trips it
// today, so this is latent rather than live — but the failure it would
// produce is a red sweep naming a binding that does not exist, in a test
// whose value is that red means something specific. The boundary costs
// nothing, gestureAttrSingle already has \s* boundaries on the other
// side, and apps/soundboard/board_test.go:41 already writes the bounded
// form of the same idea. Added in review of #428.
var gestureAttr = regexp.MustCompile(`\bGesture(?:="|:\s*"|\s*=\s*")([^"]*)"`)

// AND THE MARKUP FORM HAS A SECOND QUOTE STYLE. The loader is
// encoding/xml (markup/markup.go), whose tokenizer erases the quote
// character before e.Attr is read — so `Gesture='ctrl+j'` is a perfectly
// loadable binding, and a sweep that requires `"` on both sides cannot
// see it. No file in the tree writes one today, which is exactly why it
// was invisible: the same shape as the brace-skip hole the previous
// round closed, a blind spot in the sweep shaped like the thing being
// swept for. Found in review of #428.
//
// A SECOND PATTERN rather than an alternation inside the first, because
// RE2 has no backreferences: one regexp cannot say "the closing quote
// matches the opening one", and two capture groups cannot be told apart
// from a legitimately empty value without dropping to submatch INDEXES.
// Two patterns and a union is the version somebody can read.
//
// Go has no single-quoted string, so this form is markup-only and the
// `:` and bare-`=` spellings are deliberately absent from it.
var gestureAttrSingle = regexp.MustCompile(`Gesture\s*=\s*'([^']*)'`)

// boundGestures is every gesture written in src, in any spelling the two
// patterns above cover. The sweep and the matcher test both go through
// here, so the test cannot pass against a sweep that reads something
// else.
func boundGestures(src string) []string {
	var out []string
	for _, re := range []*regexp.Regexp{gestureAttr, gestureAttrSingle} {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			out = append(out, m[1])
		}
	}
	return out
}

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
			// testdata/ IS EXCLUDED FOR THE SAME REASON _test.go is, and
			// deciding it now is the point. The exclusion below reasons
			// that a test may bind a deliberately invalid gesture to
			// exercise the error path; a load-error FIXTURE is that same
			// test with its markup in a file, and go's own convention
			// already says testdata is not shipped code. There are no
			// .gooey fixtures under testdata today, so this changes the
			// swept count by zero — which is exactly why it had to be
			// decided in the comment rather than discovered by the first
			// fixture that binds ctrl+j on purpose and fails this test
			// with a message that reads like a real shipped defect.
			// Found in review of #428.
			if name := d.Name(); p != root &&
				(strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata") {
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
		for _, g := range boundGestures(string(b)) {
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
	//
	// WITHIN STRIKING DISTANCE OF THE REAL NUMBER, which is 264 at the
	// time of writing. At 50 a walk that lost 80% of the tree also passed
	// silently — and losing most of the tree is the likelier regression
	// than losing all of it: someone tightens the prune, or WalkDir's
	// root moves. 200 catches that while staying far enough below 264
	// that adding or removing bindings does not turn this into a count
	// maintained in prose. Raised in review of #428.
	if len(sites) < 200 {
		t.Fatalf("the sweep found only %d bound gestures; the tree binds far more "+
			"(264 when this floor was set), so the walk or the pattern lost most "+
			"of the tree and a green result means nothing",
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
		// SINGLE-QUOTED MARKUP, which encoding/xml accepts and the sweep
		// could not see. Nothing in the tree writes one, so this case is
		// the only thing that can fail — which is the point.
		{"single-quoted attribute", `<KeyBinding Gesture='ctrl+j' Command="{{.Down}}"/>`, "ctrl+j"},
		{"single-quoted, spaced", `<KeyBinding Gesture = 'alt+k'/>`, "alt+k"},
		// A double-quoted value may contain an apostrophe and vice versa;
		// neither pattern may steal the other's delimiter.
		{"apostrophe inside double quotes", `<KeyBinding Gesture="ctrl+'"/>`, "ctrl+'"},
		{"double quote inside single quotes", `<KeyBinding Gesture='ctrl+"'/>`, `ctrl+"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := boundGestures(tc.src)
			if len(got) != 1 {
				t.Fatalf("the matcher sees %d gestures in %s, want exactly 1: %q",
					len(got), tc.name, got)
			}
			if got[0] != tc.want {
				t.Errorf("matched %q, want %q", got[0], tc.want)
			}
			if strings.Contains(got[0], "{{") {
				t.Errorf("matched a template expression %q, which is not a "+
					"static gesture", got[0])
			}
		})
	}

	// And the skip still drops a bound template expression, which is the
	// one thing it is for — in BOTH quote styles, since either is a
	// loadable way to write one.
	for _, src := range []string{
		`<KeyBinding Gesture="{{.Key}}"/>`,
		`<KeyBinding Gesture='{{.Key}}'/>`,
	} {
		got := boundGestures(src)
		if len(got) != 1 || !strings.Contains(got[0], "{{") {
			t.Fatalf("a bound Gesture in %s no longer looks like a template "+
				"expression (%q), so the sweep would try to parse one", src, got)
		}
	}
}
