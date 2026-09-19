package components

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// THIS FILE IS WHAT SURVIVED THE EXPIRY, and the split is the point.
//
// It held two guards over docs/learn/concepts/overlays.md. The first,
// TestTheHostsThisPageCallsPositionDependentStillAre, was EXPIRING by
// design: it read the hosts the page called position-dependent and
// failed the moment either adopted gooey.Overlay. #439 is that moment,
// so it is gone, along with the three paragraphs its failure message
// named — which is exactly what it was written to ask for.
//
// The second does not expire, and deleting it with its neighbour would
// have been the easy mistake. "The names this page calls lifted really
// are lifted" is a claim about the CURRENT tree whatever the current
// tree is: it was wrong before #439 (the page named the Tooltip popup)
// and it can be wrong again the next time a host is added or a marker
// comes off. Raised in review of #455, kept through #439.

// TestTheHostsThisPageCallsLiftedActuallyAre reads the lifted names out
// of the page and checks each one against the type system.
//
// The page listed "the `Tooltip` popup" among the lifted surfaces for
// two rounds, one sentence after saying the opposite — and the guard
// that existed could not see it, because it read only the names on the
// UNLIFTED side. A reader who believed the lifted claim moves the
// AdornmentLayer and loses their tips behind the page.
//
// RESOLVED THROUGH THE SURFACE where a host has one: MenuBar and Popup
// do not implement the marker themselves — popupSurface does — so asking
// the host would assert the opposite of the page about both and pass for
// the wrong reason.
func TestTheHostsThisPageCallsLiftedActuallyAre(t *testing.T) {
	names := liftedHostsNamed(t, "../docs/learn/concepts/overlays.md")
	if len(names) == 0 {
		t.Fatal("the overlays concept page no longer names any host as lifted, so " +
			"this guard is checking nothing")
	}
	t.Logf("the page calls these hosts lifted: %v", names)
	for _, name := range names {
		surf, ok := liftedSurfaceByName[name]
		if !ok {
			t.Errorf("the overlays concept page calls %q lifted and this test cannot "+
				"resolve that name to the value whose type answers. Add it to "+
				"liftedSurfaceByName — a name the guard cannot check is a claim "+
				"nothing is keeping true", name)
			continue
		}
		if _, isOverlay := surf().(gooey.Overlay); !isOverlay {
			t.Errorf("the page says %s is lifted, but the value that would carry the "+
				"marker does not implement gooey.Overlay — so it paints in the "+
				"ordinary layer and its position in document order decides where it "+
				"lands. Either the page is wrong, or the marker came off a type that "+
				"still needs it", name)
		}
	}

	// A TOOLTIP IS THE NEGATIVE, and it is asserted rather than left to
	// the list's silence: the page can only be wrong about a name it
	// mentions, and the failure mode here was mentioning one that should
	// not be there. If a Tooltip ever becomes lifted on its own account
	// this fails and the paragraph about tipPopup comes out with it.
	var tip any = &Tooltip{}
	if _, isOverlay := tip.(gooey.Overlay); isOverlay {
		t.Error("Tooltip now implements gooey.Overlay. The page says its tip is " +
			"lifted by the AdornmentLayer that hosts it rather than on its own " +
			"account; correct that paragraph and this arm together")
	}

	// AND SO IS A MENUBAR, for the same reason and a different mistake.
	// The page listed the BAR among the lifted surfaces for two rounds,
	// one file away from the README saying the bar is not one and its
	// dropdown is — and the arm above could not see it, because
	// liftedSurfaceByName resolves "MenuBar" THROUGH popupSurface and so
	// answers the question the page is not asking. The distinction is
	// the useful half: Grid.Row places the bar and nothing places the
	// dropdown. Raised in review of #456.
	var bar any = &MenuBar{}
	if _, isOverlay := bar.(gooey.Overlay); isOverlay {
		t.Error("MenuBar now implements gooey.Overlay itself. The page says the bar " +
			"is an ordinary component placed by Grid.Row and that what is lifted " +
			"is the dropdown it opens; correct that paragraph and this arm together")
	}
	for _, name := range names {
		if name == "MenuBar" {
			t.Error("the overlays concept page calls MenuBar lifted. It is not — " +
				"popupSurface is, which is what its dropdown is built from — and " +
				"the arm above passes anyway because liftedSurfaceByName resolves " +
				"the name through that surface. The bar's own position is a layout " +
				"decision and the page has to say so")
		}
	}
}

// TestNoDocCallsAHostLiftedWhenOnlyItsSurfaceIs is the guard whose
// ABSENCE let one sentence spread to five files, and the round-12 review
// reported it as "TestTheHostsThisPageCallsLiftedActuallyAre does not
// exist". That test does exist, twenty lines up. What did not exist is
// any guard over the other four files, and the distinction is the
// interesting one: its corpus is a SINGLE PAGE, chosen when the claim
// lived on a single page, and a corpus of one is how a guard passes for
// years while the thing it guards is wrong everywhere else.
//
// THE CLAIM THAT WAS WRONG: "MenuBar is a gooey.Overlay", or "MenuBar is
// lifted into the overlay layer". The type does not implement the
// marker and cannot — popupSurface does, and the MenuBar merely hosts
// one. So the BAR paints in document order like anything else, which is
// the same fact as Grid.Row still deciding where it goes. A reader who
// believed the flat claim expects the bar to float above the page.
//
// README.md, docs/demos.md, cmd/toolkit/toolkit.gooey (twice) and
// docs/learn/examples/07-app-chrome/app.gooey all said it. The last two
// are the costly ones: toolkit.gooey says it ON SCREEN in the flagship
// demo, and app.gooey is what a reader copies out of Tutorial 7.
//
// THE RULE, and it is mechanical rather than a judgement about prose: a
// host that does NOT itself implement gooey.Overlay may not be named
// within a few words of "lifted" or "gooey.Overlay" unless the sentence
// also names the thing that IS — its dropdown, its surface, its popup,
// its tip. The nouns are a short closed list because there are only four
// hosts and each has exactly one.
//
// The membership question is asked of the TYPE SYSTEM, never of a list
// here: liftedSurfaceByName already resolves a name to the value that
// carries the marker, and this asks the same names whether the HOST
// carries it. A host that adopts the marker later stops being subject to
// this rule on the day it adopts it, with no edit.
func TestNoDocCallsAHostLiftedWhenOnlyItsSurfaceIs(t *testing.T) {
	hosted := hostsWhoseSurfaceCarriesTheMarker(t)
	if len(hosted) == 0 {
		t.Fatal("every named host now implements gooey.Overlay itself, so this " +
			"guard has no subject. If that is really true, delete it with the " +
			"paragraph above — but check first, because the same reading is what " +
			"a broken resolver produces")
	}
	t.Logf("hosts whose SURFACE carries the marker, not themselves: %v", hosted)

	var problems []string
	for _, f := range overlayProseFiles(t, "..") {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		goSrc := strings.HasSuffix(f, ".go")
		for i, line := range strings.Split(string(body), "\n") {
			if goSrc && !strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			for _, host := range hosted {
				at := hostLiftedClaim(host).FindStringIndex(line)
				if at == nil {
					continue
				}
				if surfaceNoun.MatchString(nearSpan(line, at)) {
					continue
				}
				problems = append(problems, fmt.Sprintf(
					"%s:%d says %s is lifted, and %s does not implement "+
						"gooey.Overlay — the surface it hosts does. The bar "+
						"paints in document order, which is the same fact as "+
						"Grid.Row still placing it. Name the thing that IS "+
						"lifted (its dropdown, surface, popup or tip):\n\t%s",
					f, i+1, host, host, strings.TrimSpace(line)))
			}
		}
	}
	for _, p := range problems {
		t.Error(p)
	}
}

// hostsWhoseSurfaceCarriesTheMarker is the subject of the guard above,
// derived: a name whose SURFACE implements gooey.Overlay while the host
// type does not.
func hostsWhoseSurfaceCarriesTheMarker(t *testing.T) []string {
	t.Helper()
	var out []string
	for name, surf := range liftedSurfaceByName {
		if _, ok := surf().(gooey.Overlay); !ok {
			continue
		}
		host, ok := hostByName[name]
		if !ok {
			t.Fatalf("liftedSurfaceByName knows %q and hostByName does not, so "+
				"the guard cannot ask whether the HOST carries the marker. The "+
				"two tables are the same set of names by construction", name)
		}
		if _, isOverlay := host().(gooey.Overlay); !isOverlay {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// hostByName is liftedSurfaceByName's twin: the HOST value rather than
// the surface it hosts. Two tables over one set of names, and the guard
// fails rather than skips when they disagree.
var hostByName = map[string]func() any{
	"Popup":          func() any { return NewPopup(&Border{}, func(*gooey.Frame, gooey.Rect) {}) },
	"MenuBar":        func() any { return &MenuBar{} },
	"ToastHost":      func() any { return &ToastHost{} },
	"AdornmentLayer": func() any { return &AdornmentLayer{} },
}

// hostLiftedClaim matches "<Host> … lifted" or "<Host> … gooey.Overlay"
// within a short run, in either order — "the MenuBar is lifted" and "a
// gooey.Overlay, which the MenuBar is" both.
// CASE-SENSITIVE ON THE HOST NAME, and the marker needs a word boundary.
// Both were wrong in the first draft and both produced FALSE POSITIVES
// on docs/markup-reference.md's ToastHost paragraph:
//
//   - `(?i)Popup` matched the common noun in "above any open popup",
//     which is not the type;
//   - `gooey\.Overlay` without \b matched `gooey.OverlayRankToast`,
//     so a sentence naming a RANK read as a sentence claiming the
//     marker.
//
// A guard that fires on correct prose is noise, and noise is how a guard
// gets deleted.
// MEMOIZED, and that is not a micro-optimisation: the call site is
// inside files x lines x hosts, so compiling here cost 189 prose files
// times their lines times two hosted names — tens of thousands of
// regexp.MustCompile calls. Measured on this tree,
// TestNoDocCallsAHostLiftedWhenOnlyItsSurfaceIs ran in 7.45s as written
// and 0.58s memoized (the review measured 20.89s against 1.54s on its own
// runner; the ratio is the portable part), which made it the most
// expensive test in this package by a wide margin. Same defect class as the qualifiers() package
// var and the parallelised tree scan; this is the third time. Raised in
// review of #458.
//
// The lock rather than a package-level table built at init, because the
// hosts come from hostsWhoseSurfaceCarriesTheMarker, which needs a *T.
var (
	hostLiftedClaimMu    sync.Mutex
	hostLiftedClaimCache = map[string]*regexp.Regexp{}
)

func hostLiftedClaim(host string) *regexp.Regexp {
	hostLiftedClaimMu.Lock()
	defer hostLiftedClaimMu.Unlock()
	if re, ok := hostLiftedClaimCache[host]; ok {
		return re
	}
	const marker = `([Ll]ifted|LIFTED|gooey\.Overlay\b)`
	re := regexp.MustCompile(
		`(\b` + host + `\b[^.\n]{0,60}?` + marker +
			`|` + marker + `[^.\n]{0,60}?\b` + host + `\b)`)
	hostLiftedClaimCache[host] = re
	return re
}

// surfaceNoun is what makes such a sentence true again: it names the
// thing that actually carries the marker. Deliberately NOT a general
// "mentions something nearby" escape — four nouns, one per host.
//
// READ AGAINST A WINDOW, NOT THE LINE, and the first version read the
// line. That was silent on the exact site this guard exists for:
// README.md's feature table puts a whole paragraph on ONE line, and that
// paragraph contains the word "popups" in its own heading — so the
// sentence calling the MenuBar a gooey.Overlay was excused by a noun two
// thousand characters away, and restoring the wrong claim was measured
// SILENT. A qualifier that can be satisfied from anywhere on a line is
// not a qualifier in a file whose lines are paragraphs.
// A BARE "popup" IS NOT ON THIS LIST, and that was the second half of
// the same silence: README's row is headed "Menus, toasts, popups", so
// the category word sat thirty characters from the claim and excused it
// even once the window was narrowed. "popup surface" is the construction
// that names the thing carrying the marker; "popups" is a topic.
var surfaceNoun = regexp.MustCompile(`(?i)dropdown|surface|\btip\b|tipPopup`)

// nearSpan is the text around a match, the same width the claim pattern
// itself reaches: a correction belongs in the sentence, not in the file.
func nearSpan(line string, at []int) string {
	const around = 60
	lo, hi := at[0]-around, at[1]+around
	if lo < 0 {
		lo = 0
	}
	if hi > len(line) {
		hi = len(line)
	}
	return line[lo:hi]
}

// overlayProseFiles is every file that can teach somebody this, which is
// the same scope question #443 asked and the same answer it gave: "Go
// doc comments, test comments and .gooey markup, not just docs/**".
// Markdown, .gooey markup (it says it ON SCREEN), the README, and Go
// source — components/menu.go's Z-ORDER block is exactly where such a
// claim lands, and the corpus used to stop one directory short of it.
//
// Go files are scanned for their COMMENT lines only. A rule about what
// prose teaches has nothing to say about an identifier, and a line of
// code that happens to put a host name within sixty columns of the word
// "lifted" is a false report on correct code — which is how a guard gets
// deleted. _test.go is excluded because the fixtures BELOW state the
// retired claim on purpose.
//
// Dot-directories are pruned at EVERY depth, not filtered at the top:
// .claude/worktrees holds whole checkouts of this repo on a developer
// machine and neither exists in a fresh clone, so a top-anchored filter
// passes in CI and walks into somebody else's tree on yours.
func overlayProseFiles(t *testing.T, root string) []string {
	t.Helper()
	// TRACKED FILES ONLY, which is the half this walk did not get from
	// its sibling. walkDocFiles (zorderdocs_test.go) was hardened
	// against exactly this in an earlier round of the same PR, with the
	// reason written out there: a stray notes.md, a CLAUDE-old.md kept
	// beside a conflict resolution, any scratch file in the root — each
	// of them makes a guard fail naming a file that is not part of the
	// repository, in a suite whose subject IS the repository. This one
	// took the dot-prune, the vendor prune and the floor and not this,
	// and a reviewer reproduced the result with one untracked file at
	// the root: the components guard went red over a tree the root
	// guards called green. The floor comment below makes the argument —
	// a lesson learned in one place does not protect its sibling — for
	// the floor half and not for this half.
	//
	// git, not .gitignore parsing: the question is exactly "is this
	// file part of the repo", and git is the authority on it. `-C root`
	// rather than a prefix dance, so the listing comes back relative to
	// the same directory this walk reports against. A tree where the
	// command cannot run is not this guard's business, so a failure to
	// list falls back to walking everything — the floor below would
	// then be what speaks. Raised in review of #458.
	tracked := map[string]bool{}
	if out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output(); err == nil {
		for _, rel := range strings.Split(string(out), "\x00") {
			if rel != "" {
				tracked[filepath.ToSlash(filepath.Join(root, rel))] = true
			}
		}
	}
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			if d.Name() == "vendor" {
				return fs.SkipDir
			}
			return nil
		}
		if len(tracked) > 0 && !tracked[filepath.ToSlash(p)] {
			return nil
		}
		switch filepath.Ext(p) {
		case ".md", ".gooey":
			out = append(out, p)
		case ".go":
			if !strings.HasSuffix(p, "_test.go") {
				out = append(out, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	// A FLOOR, because a walk that found nothing passes every assertion
	// above it — and 40 was not that floor. The tree holds ~479 matching
	// files, so 40 also passed with docs/, components/, apps/ and cmd/
	// lost TOGETHER: the walk could go blind to four of the five places
	// the rule is taught and still call itself intact. That is the exact
	// argument this PR made about docFilesIn's floor in the sibling file,
	// and this one did not get the lesson until the review pointed out
	// that a lesson learned in one place does not protect its sibling.
	// 300 is the same guarantee actually enforced, and still leaves room
	// for the tree to shrink by a third. Raised in review of #458.
	if len(out) < 300 {
		t.Fatalf("the walk found %d prose files under %s, which is too few to be "+
			"the tree — the guard is reading almost nothing", len(out), root)
	}
	return out
}

// TestTheHostClaimGuardCatchesWhatItIsFor drives the rule against
// fixtures, because a guard checked only against a clean corpus is a
// guard nobody has seen fail.
func TestTheHostClaimGuardCatchesWhatItIsFor(t *testing.T) {
	host := "MenuBar"
	cases := []struct {
		name  string
		line  string
		fires bool
	}{
		{"the flat claim", "`MenuBar` and `ToastHost`, both `gooey.Overlay`s", true},
		{"the layer phrasing", "ToastHost, MenuBar and AdornmentLayer are lifted into the overlay layer", true},
		{"reversed order", "lifted out of document order into a layer of its own, which the MenuBar is", true},
		{"named through the dropdown", "a MenuBar's dropdown is lifted out of document order", false},
		{"named through the surface", "the surface a MenuBar hosts is a gooey.Overlay", false},
		{"a sentence away", "The MenuBar sits on the top row. Toasts are lifted.", false},
		{"about another host", "`ToastHost` is a `gooey.Overlay`", false},
		// THE README SHAPE: one line, a whole paragraph, with the
		// excusing noun far from the claim. This is the arm the
		// line-scoped version failed — measured, not imagined.
		{
			"a surface noun two hundred characters away",
			"| Menus, toasts, popups | done | `MenuBar` and `ToastHost` are both " +
				"`gooey.Overlay`s, lifted out of document order. " +
				strings.Repeat("Filler about something else entirely. ", 6) +
				"The anchored-surface lifecycle is extracted as the Go-side primitive.",
			true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at := hostLiftedClaim(host).FindStringIndex(c.line)
			got := at != nil && !surfaceNoun.MatchString(nearSpan(c.line, at))
			if got != c.fires {
				t.Errorf("reported=%v, want %v, for:\n\t%s", got, c.fires, c.line)
			}
		})
	}
}

// liftedRe captures the run of backticked names immediately before
// "**are** lifted".
//
// Anchored on the sentence rather than a line number, because a line
// number in a test is the failure mode line-referencing docs already
// has: it goes stale in silence and the guard starts reading a paragraph
// about something else.
var liftedRe = regexp.MustCompile(
	"((?:`[A-Za-z][A-Za-z0-9]*`(?:,)?(?: and)?\\s+)+)\\*\\*are\\*\\*\\s+lifted")

func liftedHostsNamed(t *testing.T, path string) []string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the overlays concept page: %v", err)
	}
	// FLATTENED, because the claim wraps across lines in a 72-column
	// document and re-wrapping it must not be a failure.
	flat := strings.Join(strings.Fields(string(body)), " ")
	m := liftedRe.FindStringSubmatch(flat)
	if m == nil {
		t.Fatalf("%s no longer contains the sentence this guard is anchored to "+
			"(\"… **are** lifted\"). Re-anchor it, or delete it with the paragraph", path)
	}
	return backticked(m[1])
}

var backtickRe = regexp.MustCompile("`([A-Za-z][A-Za-z0-9]*)`")

func backticked(s string) []string {
	var out []string
	for _, m := range backtickRe.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// liftedSurfaceByName resolves a name to the value whose type carries
// the marker — the hosted surface for the two Popup-backed hosts, the
// host itself for the two that adopted it in #439. It is a LOOKUP, not
// the list: what is at stake comes from the page, and a name missing
// here fails the test rather than being skipped.
var liftedSurfaceByName = map[string]func() any{
	"Popup": func() any {
		return NewPopup(&Border{}, func(*gooey.Frame, gooey.Rect) {}).Surface()
	},
	"MenuBar": func() any {
		return (&MenuBar{}).popup().Surface()
	},
	"ToastHost":      func() any { return &ToastHost{} },
	"AdornmentLayer": func() any { return &AdornmentLayer{} },
}
