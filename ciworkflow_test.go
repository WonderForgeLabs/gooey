package gooey

import (
	"os"
	"os/exec"
	"path"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// CLAUDE.md's verify loop is pinned to the real module set by
// claudemd_test.go. CI was not, and it enumerated: one literal
// `if [ -f <path>/go.mod ]` step per nested module, with `packs/*` the
// single exception discovered by glob. The comment on that glob predicted
// the failure exactly — "a module added without also editing this file is
// never built, and the guard makes that look like a pass" — and it
// happened twice, to `paint` (#229, #241) and to every `examples/*`
// module, both times with every job green.
//
// These tests pin the fix. `ci.yml` must DISCOVER modules with the same
// command the doc documents, that command must really reach every module,
// and the -race tier the doc describes must be the tier CI applies. A
// number written in prose is a sample; running the command is a check.

const ciWorkflow = ".github/workflows/ci.yml"

// findCmd (claudemd_test.go) pulls the `find … -name go.mod …` invocation
// out of a shell block, stopping at the first pipe. Both files must
// contain one; an enumerated list of module names is the failure mode
// these tests exist to prevent.

// raceArm captures the case-arm pattern list that selects -race, in either
// file's spelling: `race=-race` in the doc's loop, `mode='race'` in the
// workflow's tier switch.
var raceArm = regexp.MustCompile(`(?m)^\s*([a-z*/|]+)\)\s*(?:race=-race|mode='race')\s*;;`)

func readFileString(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(b)
}

// discoveryIn extracts the documented/configured discovery command from a
// file and fails if there is none, because a file with no discovery is a
// file that enumerates.
func discoveryIn(t *testing.T, name, body string) string {
	t.Helper()

	cmd := findCmd.FindString(body)
	if cmd == "" {
		t.Fatalf("%s must DISCOVER modules with a `find … -name go.mod` command "+
			"rather than naming them. A literal step per module is never run for a "+
			"module nobody added a step for, and the run still goes green — that is "+
			"how `paint` and every `examples/*` module went untested (#207).", name)
	}
	return strings.TrimSpace(cmd)
}

func TestCIWorkflowDiscoversEveryNestedModule(t *testing.T) {
	want := discoverModules(t)
	if len(want) == 0 {
		t.Fatal("no nested modules found; the walk is wrong, not the tree")
	}

	cmd := discoveryIn(t, ciWorkflow, readFileString(t, ciWorkflow))

	// POSIX only, for the reason spelled out in
	// TestCLAUDEMDVerifyLoopReachesEveryNestedModule: the point is to run
	// the command AS WRITTEN, and Windows' find.exe is an unrelated
	// text-search tool. The workflow runs on ubuntu-latest regardless.
	if runtime.GOOS == "windows" {
		t.Skip("the workflow's discovery is a POSIX `find` invocation; Windows' " +
			"find.exe is an unrelated text-search tool, so this claim cannot be " +
			"checked here. Verify under WSL or Git Bash.")
	}
	argv := shellWords(t, cmd)
	out, err := exec.Command(argv[0], argv[1:]...).Output()
	if err != nil {
		t.Fatalf("running the workflow's discovery %q: %v", cmd, err)
	}

	var got []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "./")
		if line == "" {
			continue
		}
		// The root go.mod comes out of the same walk and is a matrix leg
		// of its own; discoverModules reports nested modules only.
		if dir := path.Dir(line); dir != "." {
			got = append(got, dir)
		}
	}
	sort.Strings(got)

	if missing := missingFrom(got, want); len(missing) > 0 {
		t.Errorf("%s never builds %v.\nIts discovery reaches %d nested module(s); "+
			"the tree has %d. A module CI does not reach is untested on every green "+
			"run — this is the check that makes the count knowable instead of "+
			"asserted.", ciWorkflow, missing, len(got), len(want))
	}
	if extra := missingFrom(want, got); len(extra) > 0 {
		t.Errorf("%s's discovery reaches %v, which are not modules of this repo. "+
			"Dot-directories must stay excluded: .claude/worktrees/ holds whole "+
			"checkouts, so a walk without the exclusion finds their modules too.",
			ciWorkflow, extra)
	}
}

// TestCIWorkflowAndCLAUDEMDShareOneDiscovery is what lets both files claim
// the other's behaviour. CLAUDE.md tells a reader that running its loop
// verifies what CI verifies; that sentence is only true while the two
// commands agree, and nothing but this test notices when they stop.
func TestCIWorkflowAndCLAUDEMDShareOneDiscovery(t *testing.T) {
	ci := discoveryIn(t, ciWorkflow, readFileString(t, ciWorkflow))
	doc := discoveryIn(t, claudeMD, verifySection(t))

	if strings.Join(strings.Fields(ci), " ") != strings.Join(strings.Fields(doc), " ") {
		t.Errorf("the discovery in %s and the one in %s `## Verify` have drifted:\n"+
			"  %s: %s\n  %s: %s\nThey must be the same command, or running the doc's "+
			"loop stops meaning what the doc says it means.",
			ciWorkflow, claudeMD, ciWorkflow, ci, claudeMD, doc)
	}
}

// TestCIWorkflowRaceTierMatchesCLAUDEMD pins the other half of that claim.
// CLAUDE.md says its loop's `-race` "matches CI"; the two are separate
// case arms in separate files, so the sentence is unchecked prose unless
// something compares them.
func TestCIWorkflowRaceTierMatchesCLAUDEMD(t *testing.T) {
	arm := func(name, body string) string {
		m := raceArm.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("%s has no case arm selecting -race; the tier split must be "+
				"visible as a pattern list, not spread across per-module steps", name)
		}
		return m[1]
	}

	ci := arm(ciWorkflow, readFileString(t, ciWorkflow))
	doc := arm(claudeMD, verifySection(t))

	if ci != doc {
		t.Errorf("the -race tier has drifted: %s races %q, %s races %q. CLAUDE.md "+
			"tells readers its loop matches CI; either fix the mismatch or stop "+
			"claiming it.", ciWorkflow, ci, claudeMD, doc)
	}

	// A tier expressed as path patterns is the whole point — `handlers/*`
	// covers a handler nobody has written yet, `handlers/temporal` does
	// not. Bare module names are allowed (mcp, grpc are modules, not
	// namespaces), but a namespaced path must be a pattern.
	for _, p := range strings.Split(ci, "|") {
		if strings.Contains(p, "/") && !strings.Contains(p, "*") {
			t.Errorf("the -race tier names %q literally; a namespaced entry must be "+
				"a pattern (%s/*), or the next module under it silently drops a tier",
				p, path.Dir(p))
		}
	}
}
