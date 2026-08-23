package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The deck hosts real programs, and each is its own module — `go run
// ../scene` cannot cross that boundary, so the binary has to exist before
// the take. NARRATION.md carried the build commands as a five-line shell
// block for a human to read and run, which is the deck's guest list kept
// somewhere other than the deck.
//
// It rotted the way those do, and not subtly: on the machine where this
// test was written, ALL FOUR guests were missing, so beats 1.5, 1.6 and
// 1.7 would each have opened showing a red island instead of the app the
// beat exists to show. The list was also wrong in the other direction —
// it built `intro`, which beat 3.2's watchgo.sh compiles itself.
//
// guests.sh derives the set from the deck's own `Cmd=` attributes. This
// test derives it AGAIN, independently, in Go, and requires the two to
// agree — the same two-mechanism check ci.yml's discovery gets.

var cmdAttr = regexp.MustCompile(`Cmd="([^"]*)"`)

// guestToken matches the two shapes a prebuilt guest is invoked by:
// `../name/name` and `./name`. The directory and the binary share a name
// because `go build -o name .` is what produced it.
var guestToken = regexp.MustCompile(`^(?:\.\./([A-Za-z0-9_-]+)/([A-Za-z0-9_-]+)|\./([A-Za-z0-9_-]+))$`)

// guestsFromNarration reads the deck's content and returns the sibling
// MODULES it runs as prebuilt binaries.
//
// `./watchgo.sh` is excluded by the shape of the path, not by the go.mod
// check: guestToken's name class has no `.`, so the token never matches.
// The go.mod check is a guard for an extension-less non-module token (say
// `./run`) that the path shape would accept. Neither is a denylist — that
// would be the thing this test exists to delete.
func guestsFromNarration(t *testing.T) []string {
	t.Helper()

	b, err := os.ReadFile("NARRATION.md")
	if err != nil {
		t.Fatalf("reading the deck's content: %v", err)
	}
	seen := map[string]bool{}
	for _, m := range cmdAttr.FindAllStringSubmatch(string(b), -1) {
		for _, tok := range strings.Fields(m[1]) {
			g := guestToken.FindStringSubmatch(tok)
			if g == nil {
				continue
			}
			name := g[3]
			if g[1] != "" {
				// ../dir/bin: only a guest when the two agree, which is
				// what `go build -o <dir> .` inside <dir> produces.
				if g[1] != g[2] {
					continue
				}
				name = g[1]
			}
			if _, err := os.Stat(filepath.Join("..", name, "go.mod")); err != nil {
				continue
			}
			seen[name] = true
		}
	}
	out := make([]string, 0, len(seen))
	for g := range seen {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

// TestGuestScriptBuildsEveryGuestTheDeckRuns is the two-mechanism check.
// It RUNS guests.sh rather than reading it: a script that is read is a
// script whose sed program is being reviewed, not one whose answer is.
func TestGuestScriptBuildsEveryGuestTheDeckRuns(t *testing.T) {
	want := guestsFromNarration(t)
	if len(want) == 0 {
		t.Fatal("no guests found in NARRATION.md. Either the deck stopped " +
			"hosting real programs — in which case delete guests.sh — or this " +
			"scan is broken and would report every future breakage as a pass.")
	}

	out, err := exec.Command("sh", "guests.sh").CombinedOutput()
	if err != nil {
		t.Fatalf("guests.sh failed, so at least one guest the deck runs does "+
			"not build:\n%s", out)
	}
	var got []string
	for _, line := range strings.Split(string(out), "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "ok  "); ok {
			got = append(got, strings.TrimSpace(name))
		}
	}
	sort.Strings(got)

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("guests.sh builds %v; the deck runs %v.\nThe script derives its "+
			"list from NARRATION.md and so does this test — a disagreement means "+
			"one of the two derivations is wrong, and the deck is the authority.",
			got, want)
	}
}

// TestTheDeckDoesNotCarryASecondGuestList is the anti-regression half.
//
// Restoring the prose block would not fail the test above — it would sit
// beside guests.sh agreeing with it, until the day somebody adds a beat
// and updates only one of them. That day is what this catches, and the
// only moment it is cheap to catch it is now.
func TestTheDeckDoesNotCarryASecondGuestList(t *testing.T) {
	b, err := os.ReadFile("NARRATION.md")
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	if !strings.Contains(body, "guests.sh") {
		t.Error("NARRATION.md never mentions guests.sh, so nothing tells a " +
			"presenter how to build the guests before a take")
	}
	// `go build -o` anywhere in the deck's content is a hand-written build
	// instruction, which is precisely the enumeration guests.sh replaced.
	for i, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "go build -o") {
			t.Errorf("NARRATION.md:%d hand-writes a build command:\n\t%s\n"+
				"That is a second guest list. guests.sh derives the set from the "+
				"Cmd= attributes; a copy here goes stale the first time a beat "+
				"gains or loses a guest.", i+1, strings.TrimSpace(line))
		}
	}
}
