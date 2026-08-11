package markup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
)

// These tests run REAL child processes declared in REAL markup, and what
// they assert is lifetime and hygiene: the child is up once the
// composition is live, it is gone — with its own children — once the
// composition closes, its output never reaches the terminal, and every
// mistake in the declaration is a load error rather than a surprise.

func needSh(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
}

// alive asks the kernel whether a pid exists, which is what signal 0 is
// for (companion_test.go in the root package does the same).
func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func waitForDeath(pid int) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func waitForFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) > 0 {
			return strings.TrimSpace(string(b))
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the companion never wrote %s", path)
	return ""
}

// companionPage builds src and returns the composition plus the named
// companion component, so a test can drive the lifetime the Composer owns.
func companionPage(t *testing.T, src string, ctx *Context) (*gooey.Composer, *gooey.Dispatcher, *components.Companion) {
	t.Helper()
	if ctx.Values == nil {
		ctx.Values = map[string]any{}
	}
	w, err := Build([]byte(src), ctx)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	c, err := Find[*components.Companion](ctx, "worker")
	if err != nil {
		t.Fatal(err)
	}
	comp := gooey.NewComposer(w, 20, 3)
	d := gooey.NewDispatcher()
	comp.Start(d)
	t.Cleanup(comp.Close)
	return comp, d, c
}

// The whole point, end to end: a page declares a service, the service is
// running once the composition is live, and it is gone — along with the
// grandchild it backgrounded — once the composition closes. The
// grandchild is the discriminating half: signalling the child alone leaves
// it orphaned and holding whatever it holds, which is the bug
// CompanionCmd's process group exists to prevent and which this element
// inherits by using it.
func TestCompanionStartsWithTheCompositionAndStopsWithIt(t *testing.T) {
	needSh(t)
	dir := t.TempDir()
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Companion Name="worker" Path="sh" KillDelay="200ms">
	      <Companion.Args>
	        <Arg>-c</Arg>
	        <Arg>echo $$ > ` + dir + `/child.pid; sleep 300 &amp; echo $! > ` + dir + `/grand.pid; wait</Arg>
	      </Companion.Args>
	    </Companion>
	    <Text>ui</Text>
	  </VStack>
	</Gooey>`
	comp, _, _ := companionPage(t, src, &Context{})

	child := atoi(t, waitForFile(t, filepath.Join(dir, "child.pid")))
	grand := atoi(t, waitForFile(t, filepath.Join(dir, "grand.pid")))
	if !alive(child) || !alive(grand) {
		t.Fatalf("the companion (pid %d) or its grandchild (pid %d) was never running", child, grand)
	}

	comp.Close()

	for _, p := range []struct {
		pid  int
		what string
	}{{child, "companion child"}, {grand, "backgrounded grandchild"}} {
		if !waitForDeath(p.pid) {
			t.Errorf("the %s (pid %d) outlived the composition", p.what, p.pid)
		}
	}
}

// Close WAITS. "Stopped" has to mean the process is gone by the time the
// composition is closed, not that somebody asked it to go — teardown runs
// this on the UI goroutine precisely so the app cannot outrace it.
func TestCompanionCloseWaitsForTheChild(t *testing.T) {
	needSh(t)
	dir := t.TempDir()
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Companion Name="worker" Path="sh" KillDelay="200ms">
	      <Companion.Args>
	        <Arg>-c</Arg>
	        <Arg>echo $$ > ` + dir + `/child.pid; sleep 300</Arg>
	      </Companion.Args>
	    </Companion>
	    <Text>ui</Text>
	  </VStack>
	</Gooey>`
	comp, _, c := companionPage(t, src, &Context{})
	child := atoi(t, waitForFile(t, filepath.Join(dir, "child.pid")))

	comp.Close()
	if alive(child) {
		t.Errorf("the child (pid %d) was still running when Close returned", child)
	}
	if c.Leaked() {
		t.Error("the tripwire fired on a child that stopped when asked")
	}
}

// The other half of the tripwire: a child that ignores the polite signal
// and outlives StopTimeout is ABANDONED, not waited on forever, and
// Leaked() records that it was. This is components.Companion's mirror of
// App.CompanionLeaked, and the only evidence anyone gets that a service
// ignored its cancelled context.
//
// StopTimeout is deliberately SHORTER than KillDelay here, which is the
// inversion of what a real app wants (the spec's default pair is 10s
// over 5s, so the child is SIGKILLed by its companion rather than
// abandoned by the app). Inverting it is what makes the give-up path
// reachable in a test that finishes in milliseconds.
func TestCompanionLeakedWhenTheChildOutlivesStopTimeout(t *testing.T) {
	needSh(t)
	dir := t.TempDir()
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Companion Name="worker" Path="sh" KillDelay="30s" StopTimeout="200ms">
	      <Companion.Args>
	        <Arg>-c</Arg>
	        <Arg>trap "" TERM; echo $$ > ` + dir + `/child.pid; while :; do sleep 1; done</Arg>
	      </Companion.Args>
	    </Companion>
	    <Text>ui</Text>
	  </VStack>
	</Gooey>`
	comp, _, c := companionPage(t, src, &Context{})
	child := atoi(t, waitForFile(t, filepath.Join(dir, "child.pid")))
	// The test asks for the abandoned path on purpose, so nothing else
	// will reap this child — SIGKILL the group once the assertions are in.
	t.Cleanup(func() { _ = syscall.Kill(-child, syscall.SIGKILL) })

	done := make(chan struct{})
	go func() { comp.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close hung on a child that ignores SIGTERM; StopTimeout did not bound the wait")
	}

	if !c.Leaked() {
		t.Error("the tripwire did not fire for a child that outlived StopTimeout")
	}
	if !alive(child) {
		t.Errorf("the child (pid %d) died, so this exercised the cooperative path, not the leak", child)
	}
}

// Arguments, environment and working directory all arrive, and an
// argument containing a SPACE survives — which is the whole reason
// <Companion.Args> is a list of elements instead of one space-joined
// attribute.
func TestCompanionDeliversArgsEnvAndDir(t *testing.T) {
	needSh(t)
	dir := t.TempDir()
	sub := filepath.Join(dir, "work")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Companion Name="worker" Path="sh" Dir="work">
	      <Companion.Args>
	        <Arg>-c</Arg>
	        <Arg>printf '%s\n%s\n%s\n' "$1" "$GOOEY_TEST_VAR" "$PWD" > out.txt</Arg>
	        <Arg>sh</Arg>
	        <Arg>two words</Arg>
	      </Companion.Args>
	      <Companion.Env>
	        <Var Name="GOOEY_TEST_VAR" Value="{{.Endpoint}}"/>
	      </Companion.Env>
	    </Companion>
	    <Text>ui</Text>
	  </VStack>
	</Gooey>`
	ctx := &Context{Dir: dir, Values: map[string]any{"Endpoint": prop.NewSource("http://127.0.0.1:9/mcp")}}
	companionPage(t, src, ctx)

	got := waitForFile(t, filepath.Join(sub, "out.txt"))
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("the child wrote %q, want three lines", got)
	}
	if lines[0] != "two words" {
		t.Errorf("argv[1] = %q, want %q — an argument with a space did not survive", lines[0], "two words")
	}
	if lines[1] != "http://127.0.0.1:9/mcp" {
		t.Errorf("env value = %q, want the bound endpoint", lines[1])
	}
	// The child's cwd is the resolved Dir, and Dir was document-relative.
	if resolved, err := filepath.EvalSymlinks(sub); err == nil {
		if actual, err := filepath.EvalSymlinks(lines[2]); err == nil && actual != resolved {
			t.Errorf("child cwd = %q, want %q", lines[2], resolved)
		}
	}
}

// The inherited environment reaches the child unless CleanEnv says
// otherwise — what exec.Cmd does with a nil Env, and what the worker this
// element was built for needs (an API key exported in the launching
// shell).
func TestCompanionEnvironmentInheritsUnlessCleaned(t *testing.T) {
	needSh(t)
	t.Setenv("GOOEY_INHERITED_MARKER", "inherited")
	page := func(clean string) string {
		return `<Gooey xmlns="wonderforge.io/gooey/2026">
		  <VStack>
		    <Companion Name="worker" Path="sh" ` + clean + `>
		      <Companion.Args>
		        <Arg>-c</Arg>
		        <Arg>printf '[%s]' "$GOOEY_INHERITED_MARKER" > out.txt</Arg>
		      </Companion.Args>
		    </Companion>
		    <Text>ui</Text>
		  </VStack>
		</Gooey>`
	}
	for _, tc := range []struct{ name, clean, want string }{
		{"default inherits", "", "[inherited]"},
		{"CleanEnv scrubs", `CleanEnv="true"`, "[]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ctx := &Context{Dir: dir}
			src := strings.Replace(page(tc.clean), `Path="sh"`, `Path="sh" Dir="`+dir+`"`, 1)
			companionPage(t, src, ctx)
			if got := waitForFile(t, filepath.Join(dir, "out.txt")); got != tc.want {
				t.Errorf("child saw %s, want %s", got, tc.want)
			}
		})
	}
}

// A child that writes to the inherited stdout paints over the UI's bottom
// rows in raw mode, and those bytes are not ours to repair. So the default
// is the null device, and this is the discriminator: the child reports
// where its own fd 1 and fd 2 actually point. Make the default inherit the
// app's stdout and both halves name a pipe or a tty instead.
//
// The `exec 3>&1 4>&2` is load-bearing, and it cost a false failure to
// learn: both `$(…)` command substitution and `> out.txt` REPLACE fd 1 for
// the process doing the asking, so a naive `$(readlink /proc/self/fd/1)`
// reports the substitution's pipe no matter what the parent handed over.
// The dups are taken before any redirection and are what the companion
// actually passed down.
func TestCompanionOutputDefaultsToDevNull(t *testing.T) {
	needSh(t)
	if _, err := os.Stat("/proc/self/fd"); err != nil {
		t.Skip("no /proc/self/fd to ask where the child's output went")
	}
	dir := t.TempDir()
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Companion Name="worker" Path="sh" Dir="` + dir + `">
	      <Companion.Args>
	        <Arg>-c</Arg>
	        <Arg>exec 3>&amp;1 4>&amp;2; printf '%s %s' "$(readlink /proc/self/fd/3)" "$(readlink /proc/self/fd/4)" > out.txt</Arg>
	      </Companion.Args>
	    </Companion>
	    <Text>ui</Text>
	  </VStack>
	</Gooey>`
	companionPage(t, src, &Context{})

	got := waitForFile(t, filepath.Join(dir, "out.txt"))
	if got != os.DevNull+" "+os.DevNull {
		t.Errorf("the child's stdout/stderr were %q, want both %q — a companion must never inherit the terminal the UI is drawing on", got, os.DevNull)
	}
}

// Log is the only way to keep the output, and it is a file path — there is
// deliberately no spelling for "inherit".
func TestCompanionLogCapturesOutput(t *testing.T) {
	needSh(t)
	dir := t.TempDir()
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Companion Name="worker" Path="sh" Log="worker.log">
	      <Companion.Args>
	        <Arg>-c</Arg>
	        <Arg>echo out; echo err 1>&amp;2</Arg>
	      </Companion.Args>
	    </Companion>
	    <Text>ui</Text>
	  </VStack>
	</Gooey>`
	companionPage(t, src, &Context{Dir: dir})

	got := waitForFile(t, filepath.Join(dir, "worker.log"))
	for _, want := range []string{"out", "err"} {
		if !strings.Contains(got, want) {
			t.Errorf("the log holds %q, which is missing %q", got, want)
		}
	}
}

// A child that exits without being asked to reports into the property
// graph — where a running UI can render it — and runs Exited, which is how
// a page reproduces the App tier's "a dead service takes the app with it".
func TestCompanionUnaskedExitReportsAndRunsExited(t *testing.T) {
	needSh(t)
	msg := prop.NewSource("")
	exited := 0
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Companion Name="worker" Path="sh" Error="{{.Err}}" Exited="{{.Died}}">
	      <Companion.Args>
	        <Arg>-c</Arg>
	        <Arg>exit 3</Arg>
	      </Companion.Args>
	    </Companion>
	    <Text>ui</Text>
	  </VStack>
	</Gooey>`
	_, d, c := companionPage(t, src, &Context{Values: map[string]any{
		"Err":  msg,
		"Died": gooey.Command(func() { exited++ }),
	}})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && exited == 0 {
		d.Drain()
		time.Sleep(5 * time.Millisecond)
	}
	if exited != 1 {
		t.Fatalf("Exited ran %d times, want 1", exited)
	}
	if got := msg.Get(); !strings.Contains(got, `companion "worker" stopped while the app was running`) {
		t.Errorf("Error = %q, which does not read like a CompanionError", got)
	}
	var ce *gooey.CompanionError
	if !asCompanionError(c.Err(), &ce) || ce.Phase != gooey.PhaseRun {
		t.Errorf("Err() = %v, want a run-phase *gooey.CompanionError", c.Err())
	}
}

// A stop we asked for is not a failure: swapping a page must not fire the
// outgoing companion's Exited, or a page that binds Exited to Quit would
// kill the app every time an agent swapped markup.
func TestCompanionRequestedStopDoesNotRunExited(t *testing.T) {
	needSh(t)
	dir := t.TempDir()
	exited := 0
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Companion Name="worker" Path="sh" Exited="{{.Died}}" KillDelay="200ms">
	      <Companion.Args>
	        <Arg>-c</Arg>
	        <Arg>echo $$ > ` + dir + `/child.pid; sleep 300</Arg>
	      </Companion.Args>
	    </Companion>
	    <Text>ui</Text>
	  </VStack>
	</Gooey>`
	comp, d, _ := companionPage(t, src, &Context{Values: map[string]any{
		"Died": gooey.Command(func() { exited++ }),
	}})
	waitForFile(t, filepath.Join(dir, "child.pid"))

	comp.Close()
	time.Sleep(50 * time.Millisecond)
	d.Drain()
	if exited != 0 {
		t.Errorf("Exited ran %d times for a stop the composition asked for", exited)
	}
}

// The off switch. Markup can name a binary, and markup arrives through
// paths an app does not fully control (a watched file, swap_markup from
// any MCP client), so a deployment needs one lever that takes the
// capability away — see docs/specs/2026-08-10-markup-companions.md.
//
// This is the discriminator for that lever: delete the companionsAllowed
// check and every subtest below fails.
func TestCompanionsDisabledByEnvironment(t *testing.T) {
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Companion Name="worker" Path="sh"/>
	    <Text>ui</Text>
	  </VStack>
	</Gooey>`
	// Fail closed: only unset, empty, or a recognizable true value leaves
	// the capability on. "of" is a plausible typo for "off", and a switch
	// a typo silently turns back on is not a switch.
	for _, v := range []string{"0", "f", "false", "FALSE", "off", "of", "yes please"} {
		t.Run("disabled by "+v, func(t *testing.T) {
			t.Setenv(EnvCompanions, v)
			_, err := Build([]byte(src), &Context{})
			if err == nil {
				t.Fatalf("%s=%q built a <Companion> anyway", EnvCompanions, v)
			}
			for _, want := range []string{EnvCompanions, "disabled", "child process"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
	for _, v := range []string{"", "1", "true", "TRUE"} {
		t.Run("enabled by "+strconv.Quote(v), func(t *testing.T) {
			needSh(t)
			t.Setenv(EnvCompanions, v)
			if _, err := Build([]byte(src), &Context{}); err != nil {
				t.Fatalf("%s=%q refused a <Companion>: %v", EnvCompanions, v, err)
			}
		})
	}
	t.Run("enabled when unset", func(t *testing.T) {
		needSh(t)
		os.Unsetenv(EnvCompanions)
		if _, err := Build([]byte(src), &Context{}); err != nil {
			t.Fatalf("a <Companion> was refused with %s unset: %v", EnvCompanions, err)
		}
	})
}

// The deliberate decision recorded in the spec: a companion is honored on
// EVERY build path. markup.Build is the entry point control.SwapMarkup and
// control.PatchMarkup use, so a page pushed by an MCP client starts its
// service exactly like a page loaded from disk. If that ever needs to
// change, it changes in one place — and this test says so.
func TestCompanionIsHonoredOnTheSwapBuildPath(t *testing.T) {
	needSh(t)
	dir := t.TempDir()
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Companion Name="worker" Path="sh" Dir="` + dir + `">
	      <Companion.Args>
	        <Arg>-c</Arg>
	        <Arg>echo swapped > out.txt</Arg>
	      </Companion.Args>
	    </Companion>
	    <Text>ui</Text>
	  </VStack>
	</Gooey>`
	// Build, not Load: no file, no fsys — the shape a swapped document has.
	companionPage(t, src, &Context{})
	if got := waitForFile(t, filepath.Join(dir, "out.txt")); got != "swapped" {
		t.Errorf("the swapped-in companion wrote %q", got)
	}
}

// Host-side paths are DOCUMENT-relative, not cwd-relative: a path in a
// configuration file that means something different depending on where
// the binary was launched from is a bug generator.
func TestCompanionPathsResolveAgainstTheDocumentDirectory(t *testing.T) {
	needSh(t)
	dir := t.TempDir()
	sub := filepath.Join(dir, "work")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Companion Name="worker" Path="sh" Dir="work" Log="work/worker.log"/>
	    <Text>ui</Text>
	  </VStack>
	</Gooey>`
	ctx := &Context{Dir: dir}
	if _, err := Build([]byte(src), ctx); err != nil {
		t.Fatal(err)
	}
	c, err := Find[*components.Companion](ctx, "worker")
	if err != nil {
		t.Fatal(err)
	}
	if c.Dir != sub {
		t.Errorf("Dir resolved to %q, want %q", c.Dir, sub)
	}
	if want := filepath.Join(sub, "worker.log"); c.Log != want {
		t.Errorf("Log resolved to %q, want %q", c.Log, want)
	}
	// A bare name still comes off the PATH, absolutely.
	if !filepath.IsAbs(c.Path) {
		t.Errorf("Path = %q, want the absolute result of exec.LookPath", c.Path)
	}
}

// Every mistake in a declaration is a LOAD error. Two thirds of these are
// the App tier's grace window arriving early: a binary that is not
// installed and a directory that does not exist are caught before the
// terminal is ever touched.
func TestCompanionMalformedDeclarationsAreLoadErrors(t *testing.T) {
	needSh(t)
	dir := t.TempDir()
	page := func(body string) string {
		return `<Gooey xmlns="wonderforge.io/gooey/2026"><VStack>` + body + `<Text>ui</Text></VStack></Gooey>`
	}
	for _, tc := range []struct{ name, src, want string }{
		{"no Name", page(`<Companion Path="sh"/>`), "needs a Name"},
		{"no Path", page(`<Companion Name="worker"/>`), "needs a Path"},
		{"unknown attribute", page(`<Companion Name="worker" Path="sh" Command="sh"/>`), "no such attribute"},
		{"layout attribute", page(`<Companion Name="worker" Path="sh" Grid.Row="1"/>`), "no such attribute"},
		{"missing binary", page(`<Companion Name="worker" Path="gooey-no-such-binary-exists"/>`), "gooey-no-such-binary-exists"},
		{"missing dir", page(`<Companion Name="worker" Path="sh" Dir="` + dir + `/nope"/>`), "nope"},
		{"dir is a file", page(`<Companion Name="worker" Path="sh" Dir="` + dir + `/f"/>`), "not a directory"},
		{"missing log dir", page(`<Companion Name="worker" Path="sh" Log="` + dir + `/nope/w.log"/>`), "nope"},
		{"bad KillDelay", page(`<Companion Name="worker" Path="sh" KillDelay="soon"/>`), "KillDelay"},
		{"negative StopTimeout", page(`<Companion Name="worker" Path="sh" StopTimeout="-2s"/>`), "must be positive"},
		{"direct child", page(`<Companion Name="worker" Path="sh"><Text>x</Text></Companion>`), "takes no children"},
		{"unknown property element", page(`<Companion Name="worker" Path="sh"><Companion.Argv><Arg>x</Arg></Companion.Argv></Companion>`), "does not accept the property element"},
		{"non-Arg in Args", page(`<Companion Name="worker" Path="sh"><Companion.Args><Text>x</Text></Companion.Args></Companion>`), "must be <Arg> elements"},
		{"attribute on Arg", page(`<Companion Name="worker" Path="sh"><Companion.Args><Arg Value="x"/></Companion.Args></Companion>`), "takes no attributes"},
		{"non-Var in Env", page(`<Companion Name="worker" Path="sh"><Companion.Env><Arg>x</Arg></Companion.Env></Companion>`), "must be <Var> elements"},
		{"Var without Name", page(`<Companion Name="worker" Path="sh"><Companion.Env><Var Value="x"/></Companion.Env></Companion>`), "needs a Name"},
		{"Var name with =", page(`<Companion Name="worker" Path="sh"><Companion.Env><Var Name="A=B" Value="x"/></Companion.Env></Companion>`), "cannot contain"},
		{"unknown Var attribute", page(`<Companion Name="worker" Path="sh"><Companion.Env><Var Name="A" Val="x"/></Companion.Env></Companion>`), "no such attribute"},
		{"Error is not a string handle", page(`<Companion Name="worker" Path="sh" Error="{{.N}}"/>`), "*prop.Property[string]"},
		{"unresolvable Exited", page(`<Companion Name="worker" Path="sh" Exited="{{.Missing}}"/>`), "not found in context"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, "f"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			ctx := &Context{Values: map[string]any{"N": prop.NewSource(1)}}
			w, err := Build([]byte(tc.src), ctx)
			if err == nil {
				stopAny(w)
				t.Fatalf("built without complaint")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// stopAny closes a composition built by a test that expected a failure,
// so a leaked expectation does not leak a process too.
func stopAny(w gooey.Component) {
	if w == nil {
		return
	}
	c := gooey.NewComposer(w, 10, 3)
	c.Start(gooey.NewDispatcher())
	c.Close()
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		t.Fatalf("not a pid: %q", s)
	}
	return n
}

// asCompanionError is errors.As without the import, kept explicit because
// the point of the assertion is that the two companion tiers report the
// SAME error type.
func asCompanionError(err error, out **gooey.CompanionError) bool {
	ce, ok := err.(*gooey.CompanionError)
	if ok {
		*out = ce
	}
	return ok
}
