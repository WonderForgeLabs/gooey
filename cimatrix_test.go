package gooey

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ci.yml stopped emitting one matrix leg per module and started PACKING
// the tiered modules into one leg per tier. Two pieces of that are
// load-bearing and were, until these tests, checked only by a human having
// run them once:
//
//   - the `group_by(.mode)` matrix build, and the `covered` sum that
//     replaced `legs == ${#mods[@]}`. Counting legs was a real coverage
//     check while there was a leg per module; under packing `legs` is 3
//     whether the tiers hold every module or one, so the sum is now the
//     only thing standing between a broken walk and a green run.
//
//   - the leg loop's fails-file idiom, which is what still makes a failure
//     name its own module now that the CHECK name cannot. An `exit 1` in
//     the loop body would look tidier, pass every run that is green, and
//     silently throw away every failure after the first.
//
// Both are the shape of #207: a mechanism that goes green while covering
// less than it claims. So neither is asserted in prose here — the tests
// extract ci.yml's own code and run it.

// jqMatrixProgram pulls the `jq` program that builds the matrix out of the
// `matrix=$(jq -Rnc '…' < "$tmp/modules-tiered")` assignment. The `-Rnc`
// flags are part of the match: they are what make the program read raw
// lines from stdin, which is how it is exercised below.
var jqMatrixProgram = regexp.MustCompile(`(?s)matrix=\$\(jq -Rnc '(.*?)'\s*<`)

// coveredExpr pulls the sum that replaced the leg count. Pinning the
// expression itself (rather than just its effect) is what makes a revert
// to `legs` visible: that would still be a valid shell line producing a
// number, and every green run would stay green.
var coveredExpr = regexp.MustCompile(`covered=\$\(jq '([^']*)' <<<"\$matrix"\)`)

// runBlockOf returns the dedented body of the `run: |` block belonging to
// the step with the given `name:`. ci.yml is read as text rather than
// parsed as YAML on purpose — the root module's go.mod has exactly two
// direct requirements and adding a YAML parser to it is a doctrine change,
// not a test convenience.
func runBlockOf(t *testing.T, body, stepName string) string {
	t.Helper()

	lines := strings.Split(body, "\n")
	start := -1
	for i, l := range lines {
		if strings.Contains(l, "- name: "+stepName) {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s has no step named %q. If it was renamed, this test has to be "+
			"renamed with it — a step this test cannot find is a step it is not "+
			"checking, and it would report that as a pass.", ciWorkflow, stepName)
	}

	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "run: |" {
			continue
		}
		indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
		var out []string
		for j := i + 1; j < len(lines); j++ {
			l := lines[j]
			if strings.TrimSpace(l) == "" {
				out = append(out, "")
				continue
			}
			if len(l)-len(strings.TrimLeft(l, " ")) <= indent {
				break
			}
			out = append(out, strings.TrimPrefix(l, strings.Repeat(" ", indent+2)))
		}
		return strings.Join(out, "\n")
	}
	t.Fatalf("step %q in %s has no `run: |` block", stepName, ciWorkflow)
	return ""
}

// TestCIMatrixPackingPartitionsEveryModule runs ci.yml's own jq program
// over synthetic tiered input. The input deliberately does NOT contain
// every tier: one leg per NON-EMPTY tier is the actual invariant, and
// "three legs" is only today's count.
func TestCIMatrixPackingPartitionsEveryModule(t *testing.T) {
	m := jqMatrixProgram.FindStringSubmatch(readFileString(t, ciWorkflow))
	if m == nil {
		t.Fatalf("%s must build its matrix with a `jq -Rnc '…'` program that this "+
			"test can extract and run. Without that this test would be asserting "+
			"a copy of the logic rather than the logic.", ciWorkflow)
	}

	// Two tiers, not three, and a module count that no arithmetic accident
	// reproduces: 5 modules over 2 modes.
	tiered := strings.Join([]string{
		".\ttest",
		"apps/one\tvet",
		"apps/two\tvet",
		"imagefmt/svg\ttest",
		"apps/three\tvet",
	}, "\n") + "\n"

	cmd := exec.Command("jq", "-Rnc", m[1])
	cmd.Stdin = strings.NewReader(tiered)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running ci.yml's own matrix program: %v", err)
	}

	legs := parseLegs(t, string(out))

	if len(legs) != 2 {
		t.Errorf("5 modules across 2 tiers packed into %d leg(s), want 2 — one per "+
			"NON-EMPTY tier. Got: %v", len(legs), legs)
	}

	// The property the `covered` sum exists to enforce: the legs partition
	// the input. Not "cover" — partition, so a module cannot be silently
	// dropped OR silently built twice.
	var got []string
	total := 0
	for _, l := range legs {
		total += l.count
		mods := strings.Fields(l.modules)
		if len(mods) != l.count {
			t.Errorf("leg %q reports count=%d but lists %d module(s) (%q). The "+
				"`covered` sum trusts that count, so a count that disagrees with "+
				"its own list would clear the check while a module went unbuilt.",
				l.mode, l.count, len(mods), l.modules)
		}
		got = append(got, mods...)
	}
	if total != 5 {
		t.Errorf("per-leg counts sum to %d, want 5. This sum IS the coverage "+
			"check that replaced `legs == modules`; if it can disagree with the "+
			"input, discovery can drop a module and CI stays green (#207).", total)
	}

	want := []string{".", "apps/one", "apps/three", "apps/two", "imagefmt/svg"}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the legs do not partition the input:\n  got:  %v\n  want: %v", got, want)
	}
}

// TestCICoveredSumsCountsRatherThanCountingLegs pins the fix for the
// coverage check specifically, because a revert to the old expression is
// the one edit here that cannot fail a normal run: `legs` and `covered`
// are both numbers, both compared against the module count, and they
// agreed for as long as there was a leg per module.
func TestCICoveredSumsCountsRatherThanCountingLegs(t *testing.T) {
	body := readFileString(t, ciWorkflow)

	m := coveredExpr.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("%s must derive its coverage number by SUMMING the per-leg "+
			"counts. Under packing, the number of legs is a constant with respect "+
			"to how many modules the walk found, so comparing it against the "+
			"module count checks nothing.", ciWorkflow)
	}
	if !strings.Contains(m[1], "add") || !strings.Contains(m[1], ".count") {
		t.Errorf("the coverage expression is %q, which does not sum the per-leg "+
			"`.count` fields.", m[1])
	}

	// And it must be `covered`, not `legs`, that the floor compares.
	if !strings.Contains(body, `if [ "$covered" != "${#mods[@]}" ]; then`) {
		t.Errorf("%s no longer compares the summed coverage against the discovered "+
			"module count. That comparison is the whole check.", ciWorkflow)
	}
}

// TestCILegNamesEveryFailingModule is the behavioural half, and the one
// that matters: it builds a throwaway tier of three modules, breaks two of
// them in DIFFERENT ways, and runs ci.yml's actual leg script over it.
//
// A text assertion (`the loop contains "continue"`) would pass on a script
// that no longer works. This one fails if the loop ever stops early,
// because module three's name would go missing from the output.
func TestCILegNamesEveryFailingModule(t *testing.T) {
	script := runBlockOf(t, readFileString(t, ciWorkflow),
		"Vet and test every ${{ matrix.mode }} module")

	// Outside the repo, so the workspace's go.work does not adopt these.
	root := t.TempDir()
	mods := []string{"alpha", "beta", "gamma"}
	sources := map[string]string{
		// passes
		"alpha": "package alpha\n\nfunc A() int { return 1 }\n",
		// fails to compile, so `go vet` fails
		"beta": "package beta\n\nfunc B() int { return \"not an int\" }\n",
		// compiles and vets clean; its TEST fails
		"gamma": "package gamma\n\nfunc G() int { return 1 }\n",
	}
	for _, m := range mods {
		dir := filepath.Join(root, m)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		write := func(name, body string) {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("go.mod", "module example.com/"+m+"\n\ngo 1.25.6\n")
		write(m+".go", sources[m])
	}
	if err := os.WriteFile(filepath.Join(root, "gamma", "gamma_test.go"),
		[]byte("package gamma\n\nimport \"testing\"\n\nfunc TestG(t *testing.T) { t.Fatal(\"deliberate\") }\n"),
		0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", "-c", script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"MODE=test",
		"MODULES="+strings.Join(mods, " "),
		"RUNNER_TEMP="+root,
		"GITHUB_STEP_SUMMARY="+filepath.Join(root, "summary"),
		"GOFLAGS=", // the repo's -mod=vendor must not follow us here
	)
	out, err := cmd.CombinedOutput()
	got := string(out)

	if err == nil {
		t.Fatalf("the leg passed with two broken modules in it:\n%s", got)
	}

	for _, want := range []string{"beta", "gamma"} {
		if !strings.Contains(got, "::error::"+want) {
			t.Errorf("the leg failed without naming %q. Packing legs by tier gave "+
				"up the module name in the CHECK name; these annotations are what "+
				"replaced it, so a leg that stops at its first failure loses every "+
				"module after it.\n%s", want, got)
		}
	}
	if strings.Contains(got, "::error::alpha") {
		t.Errorf("the leg named alpha, which passes — a check that names everything "+
			"names nothing.\n%s", got)
	}
	// The summary line lists the whole set, not just the first.
	if !strings.Contains(got, "leg failed for: beta gamma") {
		t.Errorf("the leg's final message should list every failing module in "+
			"order; got:\n%s", got)
	}
}

type leg struct {
	mode    string
	count   int
	modules string
}

// parseLegs reads the compact JSON the matrix program emits without a JSON
// dependency: the shape is fixed by the program above, and this test is
// what would catch it changing.
func parseLegs(t *testing.T, s string) []leg {
	t.Helper()

	s = strings.TrimSpace(s)
	entry := regexp.MustCompile(`\{"mode":"([^"]*)","count":(\d+),"modules":"([^"]*)"\}`)
	ms := entry.FindAllStringSubmatch(s, -1)
	if len(ms) == 0 {
		t.Fatalf("the matrix program emitted no {mode,count,modules} object: %s\n"+
			"If its output shape changed, every consumer in ci.yml's `test` job "+
			"changed with it.", s)
	}
	var out []leg
	for _, m := range ms {
		n := 0
		for _, c := range m[2] {
			n = n*10 + int(c-'0')
		}
		out = append(out, leg{mode: m[1], count: n, modules: m[3]})
	}
	return out
}

// writeModule drops a minimal module at root/name with the given `go`
// directive, for the workflow-step tests below.
func writeModule(t *testing.T, root, name, goDirective string) {
	t.Helper()

	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(name)
	files := map[string]string{
		"go.mod":     "module example.com/" + base + "\n\ngo " + goDirective + "\n",
		base + ".go": "package " + base + "\n",
	}
	for f, body := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestCIToolchainCheckFailsOnlyTheTooNewModule pins the step that replaced
// actions/setup-go.
//
// Dropping setup-go means go.mod no longer PINS the toolchain, so this step
// is the only thing left checking it — and it is order-sensitive in a way
// that reads fine either way round: `sort -V -C` over (want, have) accepts
// an image at least as new, and the same two lines swapped accepts an image
// at most as new. Both are one line of shell, both look plausible, and the
// wrong one passes every run on a tree where the two versions are equal —
// which is every module in this repo today.
//
// So it is checked by running it against a module the image CANNOT build
// and one it can, which is the only way the direction becomes observable.
func TestCIToolchainCheckFailsOnlyTheTooNewModule(t *testing.T) {
	script := runBlockOf(t, readFileString(t, ciWorkflow),
		"Check the image's Go satisfies every module in this leg")

	root := t.TempDir()
	// Deliberately far below and far above any toolchain that could be
	// running this test, so neither arm depends on which Go the machine has.
	writeModule(t, root, "oldenough", "1.16.0")
	writeModule(t, root, "fromthefuture", "1.99.0")

	run := func(mods string) (string, error) {
		cmd := exec.Command("bash", "-c", script)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "MODE=test", "MODULES="+mods, "GOFLAGS=")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// The passing direction. Reverse the comparison and this arm fails,
	// which is what makes running it worthwhile.
	if out, err := run("oldenough"); err != nil {
		t.Errorf("a module requiring go1.16.0 was rejected by a newer toolchain — "+
			"the version comparison is inverted:\n%s", out)
	}

	// The failing direction, with a satisfiable module alongside so the test
	// also shows the step is not simply rejecting everything.
	out, err := run("oldenough fromthefuture")
	if err == nil {
		t.Fatalf("a module requiring go1.99.0 was accepted:\n%s", out)
	}
	if !strings.Contains(out, "::error::fromthefuture") {
		t.Errorf("the step failed without naming the module that caused it. It "+
			"runs per module precisely so the answer is not \"something in this "+
			"tier\":\n%s", out)
	}
	if strings.Contains(out, "::error::oldenough") {
		t.Errorf("the step named a module the toolchain can build:\n%s", out)
	}
}

// TestCIDiscoveryFloorNamesTheConditionThatFailed pins #263's split. The
// two conditions used to share one message that always said "no root
// go.mod", including in the case the guard exists to catch. A single `||`
// is an easy thing to restore and both messages are plausible English, so
// the only way to know which one fires is to fire them.
func TestCIDiscoveryFloorNamesTheConditionThatFailed(t *testing.T) {
	script := runBlockOf(t, readFileString(t, ciWorkflow), "Discover modules")

	// The step cross-checks its walk against `git ls-files`, so the fixture
	// has to be a real index, not merely files on disk.
	newRepo := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		for _, args := range [][]string{
			{"init", "-q"},
			{"config", "user.email", "ci@example.com"},
			{"config", "user.name", "ci"},
		} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		return dir
	}
	stage := func(t *testing.T, dir string) {
		t.Helper()
		cmd := exec.Command("git", "add", "-A")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git add: %v\n%s", err, out)
		}
	}
	run := func(t *testing.T, dir string) (string, error) {
		t.Helper()
		tmp := filepath.Join(dir, ".rt")
		if err := os.MkdirAll(tmp, 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("bash", "-c", script)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"RUNNER_TEMP="+tmp,
			"GITHUB_OUTPUT="+filepath.Join(dir, ".out"),
			"GITHUB_STEP_SUMMARY="+filepath.Join(dir, ".sum"),
		)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	t.Run("no root go.mod", func(t *testing.T) {
		dir := newRepo(t)
		writeModule(t, dir, "pkgs/one", "1.25.6")
		stage(t, dir)

		out, err := run(t, dir)
		if err == nil {
			t.Fatalf("discovery accepted a tree with no root go.mod:\n%s", out)
		}
		if !strings.Contains(out, "no root go.mod") {
			t.Errorf("the message does not name the missing root:\n%s", out)
		}
	})

	t.Run("root only", func(t *testing.T) {
		dir := newRepo(t)
		if err := os.WriteFile(filepath.Join(dir, "go.mod"),
			[]byte("module example.com/root\n\ngo 1.25.6\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		stage(t, dir)

		out, err := run(t, dir)
		if err == nil {
			t.Fatalf("discovery accepted a tree with only the root module:\n%s", out)
		}
		// #263 exactly: this case must NOT claim the root is missing.
		if strings.Contains(out, "no root go.mod") {
			t.Errorf("the root go.mod is present and the failure still blames its "+
				"absence — which sends whoever is debugging a broken walk looking "+
				"for a file that is right there:\n%s", out)
		}
		if !strings.Contains(out, "and nothing else") {
			t.Errorf("the message does not say the walk found nothing beyond the "+
				"root:\n%s", out)
		}
	})
}
