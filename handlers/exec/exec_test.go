package exechandlers_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/WonderForgeLabs/gooey"
	exechandlers "github.com/WonderForgeLabs/gooey/handlers/exec"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
)

// helper is the fixture binary, built once by TestMain — portable
// process behavior without guessing which OS binaries exist.
var helper string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "execpack")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	helper = filepath.Join(dir, "helper")
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}
	build := exec.Command("go", "build", "-o", helper, "./testdata/helper")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building test helper: %s\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// register builds a provider over cmds and grants the namespace,
// replacing whatever the previous test granted.
func register(t *testing.T, cmds []exechandlers.Command, opts ...exechandlers.Option) {
	t.Helper()
	p, err := exechandlers.New(cmds, opts...)
	if err != nil {
		t.Fatal(err)
	}
	markup.RegisterHandlers(exechandlers.URI, p)
}

// page wraps one handler expression in the standard test document: a
// button carrying it, and a Text bound to .Out.
func page(expr string) string {
	return `<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:sys="gooey.dev/handlers/exec">
  <VStack>
    <Button Name="go" Content="go" Click="` + expr + `"/>
    <Text>{{.Out}}</Text>
  </VStack>
</Gooey>`
}

type harness struct {
	t    *testing.T
	out  *prop.Property[string]
	disp *gooey.Dispatcher
	comp *gooey.Composer
}

func build(t *testing.T, src string, extra map[string]any) *harness {
	t.Helper()
	h := &harness{
		t:    t,
		out:  prop.NewSource("(nothing yet)"),
		disp: gooey.NewDispatcher(),
	}
	values := map[string]any{"Out": h.out}
	for k, v := range extra {
		values[k] = v
	}
	ctx := &markup.Context{Values: values, Dispatcher: h.disp}
	w, err := markup.Load(fstest.MapFS{"page.gooey": {Data: []byte(src)}}, "page.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	h.comp = gooey.NewComposer(w, 60, 6)
	return h
}

// clickAndSettle presses the focused button and pumps the dispatcher
// until the async completion arrives — the loop's job, done by hand.
func (h *harness) clickAndSettle() {
	h.t.Helper()
	if !h.comp.HandleKey(input.Named(input.KeyEnter)) {
		h.t.Fatal("enter did not reach the focused button")
	}
	deadline := time.After(15 * time.Second)
	select {
	case <-h.disp.Wake():
		if n := h.disp.Drain(); n == 0 {
			h.t.Fatal("woke with nothing to drain")
		}
	case <-deadline:
		h.t.Fatal("handler never delivered a result")
	}
}

func TestRunDeliversStdout(t *testing.T) {
	register(t, []exechandlers.Command{
		{Name: "greet", Path: helper, Args: []string{"out", "hello", "world"}},
	})
	h := build(t, page("{{sys:Run `greet` | into .Out}}"), nil)
	h.clickAndSettle()
	if got := h.out.Get(); got != "hello world\n" {
		t.Fatalf("out=%q, want the helper's stdout", got)
	}
}

// The arguments are handles, not snapshots: whatever .Name holds when
// the button is clicked is what the child receives in its argv.
func TestArgumentsAreReadAtInvokeTime(t *testing.T) {
	register(t, []exechandlers.Command{
		{Name: "echo", Path: helper, Args: []string{"out"}, ArgPolicy: exechandlers.ArgsAny},
	})
	name := prop.NewSource("first")
	h := build(t, page("{{sys:Run `echo` .Name | into .Out}}"), map[string]any{"Name": name})

	h.clickAndSettle()
	if got := h.out.Get(); got != "first\n" {
		t.Fatalf("out=%q, want %q", got, "first\n")
	}
	name.Set("second")
	h.clickAndSettle()
	if got := h.out.Get(); got != "second\n" {
		t.Fatalf("out=%q after re-pointing .Name, want %q", got, "second\n")
	}
}

func TestCaptureModes(t *testing.T) {
	register(t, []exechandlers.Command{
		{Name: "warn", Path: helper, Args: []string{"err", "warned"}, Capture: exechandlers.CaptureStderr},
		{Name: "mix", Path: helper, Args: []string{"mix"}, Capture: exechandlers.CaptureCombined},
		{Name: "mix-both", Path: helper, Args: []string{"mix"}, Capture: exechandlers.CaptureBoth},
		{Name: "seven", Path: helper, Args: []string{"exit", "7"}, Capture: exechandlers.CaptureExitCode},
	})
	t.Run("stderr", func(t *testing.T) {
		h := build(t, page("{{sys:Run `warn` | into .Out}}"), nil)
		h.clickAndSettle()
		if got := h.out.Get(); got != "warned\n" {
			t.Fatalf("out=%q, want the stderr stream", got)
		}
	})
	t.Run("combined interleaves in write order", func(t *testing.T) {
		h := build(t, page("{{sys:Run `mix` | into .Out}}"), nil)
		h.clickAndSettle()
		if got := h.out.Get(); got != "OUT\nERR\n" {
			t.Fatalf("out=%q, want both streams through one pipe", got)
		}
	})
	t.Run("both is tagged JSON with the exit code as data", func(t *testing.T) {
		h := build(t, page("{{sys:Run `mix-both` | into .Out}}"), nil)
		h.clickAndSettle()
		var got struct {
			Exit   int    `json:"exit"`
			Stdout string `json:"stdout"`
			Stderr string `json:"stderr"`
		}
		if err := json.Unmarshal([]byte(h.out.Get()), &got); err != nil {
			t.Fatalf("delivery is not JSON: %v\n%s", err, h.out.Get())
		}
		if got.Exit != 0 || got.Stdout != "OUT\n" || got.Stderr != "ERR\n" {
			t.Fatalf("both=%+v, want tagged streams and exit 0", got)
		}
	})
	t.Run("exit-code", func(t *testing.T) {
		h := build(t, page("{{sys:Run `seven` | into .Out}}"), nil)
		h.clickAndSettle()
		if got := h.out.Get(); got != "7" {
			t.Fatalf("out=%q, want the decimal exit code", got)
		}
	})
}

func TestPerCallCaptureOverridesRegistration(t *testing.T) {
	register(t, []exechandlers.Command{
		{Name: "mix", Path: helper, Args: []string{"mix"}}, // default: stdout
	})
	h := build(t, page("{{sys:Run `mix` `capture=stderr` | into .Out}}"), nil)
	h.clickAndSettle()
	if got := h.out.Get(); got != "ERR\n" {
		t.Fatalf("out=%q, want the stderr stream via the capture= option", got)
	}
}

// Stream captures treat a non-zero exit as a failure: an ERROR delivery
// naming the exit and quoting stderr's first line.
func TestNonZeroExitIsAnError(t *testing.T) {
	register(t, []exechandlers.Command{
		{Name: "boom", Path: helper, Args: []string{"fail", "3"}},
	})
	h := build(t, page("{{sys:Run `boom` | into .Out}}"), nil)
	h.clickAndSettle()
	got := h.out.Get()
	if !strings.HasPrefix(got, "ERROR:") || !strings.Contains(got, "exit status 3") || !strings.Contains(got, "boom") {
		t.Fatalf("out=%q, want an ERROR naming exit 3 and quoting stderr", got)
	}
}

// exit-code capture makes the same failure data instead.
func TestExitCodeCaptureTreatsFailureAsData(t *testing.T) {
	register(t, []exechandlers.Command{
		{Name: "boom", Path: helper, Args: []string{"fail", "3"}, Capture: exechandlers.CaptureExitCode},
	})
	h := build(t, page("{{sys:Run `boom` | into .Out}}"), nil)
	h.clickAndSettle()
	if got := h.out.Get(); got != "3" {
		t.Fatalf("out=%q, want the exit code as data", got)
	}
}

func TestJqExtraction(t *testing.T) {
	register(t, []exechandlers.Command{
		{Name: "items", Path: helper, Args: []string{"json"}, Jq: ".items[].name"},
		{Name: "doc", Path: helper, Args: []string{"json"}},
		{Name: "plain", Path: helper, Args: []string{"out", "not json"}, Jq: "."},
	})
	t.Run("registration jq, multiple results join with newlines", func(t *testing.T) {
		h := build(t, page("{{sys:Run `items` | into .Out}}"), nil)
		h.clickAndSettle()
		if got := h.out.Get(); got != "alpha\nbeta" {
			t.Fatalf("out=%q, want the extracted names", got)
		}
	})
	t.Run("per-call jq= option", func(t *testing.T) {
		h := build(t, page("{{sys:Run `doc` `jq=.ok` | into .Out}}"), nil)
		h.clickAndSettle()
		if got := h.out.Get(); got != "true" {
			t.Fatalf("out=%q, want the extracted boolean", got)
		}
	})
	t.Run("per-call jq overrides registration jq", func(t *testing.T) {
		h := build(t, page("{{sys:Run `items` `jq=.ok` | into .Out}}"), nil)
		h.clickAndSettle()
		if got := h.out.Get(); got != "true" {
			t.Fatalf("out=%q, want the override's result", got)
		}
	})
	t.Run("non-string results are re-marshaled", func(t *testing.T) {
		h := build(t, page("{{sys:Run `doc` `jq=.items[0]` | into .Out}}"), nil)
		h.clickAndSettle()
		got := h.out.Get()
		if !strings.Contains(got, `"name":"alpha"`) {
			t.Fatalf("out=%q, want the first item as JSON", got)
		}
	})
	t.Run("non-JSON input is an ERROR delivery", func(t *testing.T) {
		h := build(t, page("{{sys:Run `plain` | into .Out}}"), nil)
		h.clickAndSettle()
		got := h.out.Get()
		if !strings.HasPrefix(got, "ERROR:") || !strings.Contains(got, "not JSON") {
			t.Fatalf("out=%q, want a jq-input ERROR", got)
		}
	})
}

// capture=both composes with jq: the tagged object is the jq input, so
// `.stderr` extracts the stream failure diagnostics live in.
func TestBothComposesWithJq(t *testing.T) {
	register(t, []exechandlers.Command{
		{Name: "mix", Path: helper, Args: []string{"mix"}, Capture: exechandlers.CaptureBoth, Jq: ".stderr"},
	})
	h := build(t, page("{{sys:Run `mix` | into .Out}}"), nil)
	h.clickAndSettle()
	if got := h.out.Get(); got != "ERR\n" {
		t.Fatalf("out=%q, want the stderr member of the tagged object", got)
	}
}

// The child starts from an EMPTY environment: nothing leaks unless the
// registration names it (PassEnv) or supplies it (Env).
func TestEnvironmentIsScrubbed(t *testing.T) {
	t.Setenv("EXEC_PACK_SECRET", "hunter2")
	t.Setenv("EXEC_PACK_KEEP", "kept")
	register(t, []exechandlers.Command{
		{Name: "env", Path: helper, Args: []string{"env"},
			Env:     []string{"INJECTED=yes"},
			PassEnv: []string{"EXEC_PACK_KEEP", "EXEC_PACK_ABSENT"}},
	})
	h := build(t, page("{{sys:Run `env` | into .Out}}"), nil)
	h.clickAndSettle()
	got := h.out.Get()
	if !strings.Contains(got, "INJECTED=yes") {
		t.Fatalf("explicit Env entry missing:\n%s", got)
	}
	if !strings.Contains(got, "EXEC_PACK_KEEP=kept") {
		t.Fatalf("PassEnv entry missing:\n%s", got)
	}
	if strings.Contains(got, "EXEC_PACK_SECRET") {
		t.Fatalf("parent environment leaked into the child:\n%s", got)
	}
	if strings.Contains(got, "EXEC_PACK_ABSENT") {
		t.Fatalf("PassEnv invented a variable the parent does not have:\n%s", got)
	}
}

func TestTimeoutKillsTheProcess(t *testing.T) {
	register(t, []exechandlers.Command{
		{Name: "slow", Path: helper, Args: []string{"sleep", "30000"}, Timeout: 200 * time.Millisecond},
	}, exechandlers.WithKillDelay(300*time.Millisecond))
	h := build(t, page("{{sys:Run `slow` | into .Out}}"), nil)
	start := time.Now()
	h.clickAndSettle()
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("delivery took %s; the process was not killed", elapsed)
	}
	got := h.out.Get()
	if !strings.HasPrefix(got, "ERROR:") || !strings.Contains(got, "timed out after 200ms") {
		t.Fatalf("out=%q, want a timeout ERROR naming the budget", got)
	}
}

// `--` ends option parsing, so an argument that happens to start with
// "capture=" can still be passed.
func TestEndOfOptionsMarker(t *testing.T) {
	register(t, []exechandlers.Command{
		{Name: "args", Path: helper, Args: []string{"args"}, ArgPolicy: exechandlers.ArgsAny},
	})
	h := build(t, page("{{sys:Run `args` `--` `capture=stderr` | into .Out}}"), nil)
	h.clickAndSettle()
	if got := h.out.Get(); got != "capture=stderr\n" {
		t.Fatalf("out=%q, want the literal passed as an argument", got)
	}
}

// The result reaches the screen through the ordinary graph: exactly the
// bound Text repaints, with nothing in the provider knowing it exists.
func TestResultRepaintsTheBoundText(t *testing.T) {
	register(t, []exechandlers.Command{
		{Name: "greet", Path: helper, Args: []string{"raw", "PAYLOAD"}},
	})
	h := build(t, page("{{sys:Run `greet` | into .Out}}"), nil)
	h.comp.Frame() // paint everything once
	h.clickAndSettle()
	frame, painted := h.comp.Frame()
	if painted != 1 {
		t.Fatalf("repainted %d components, want exactly the bound Text", painted)
	}
	if out := frameString(frame, 60, 6); !strings.Contains(out, "PAYLOAD") {
		t.Fatalf("result never reached the screen:\n%s", out)
	}
}

// Everything resolvable fails at load, not at click — including every
// allowlist violation.
func TestLoadErrors(t *testing.T) {
	register(t, []exechandlers.Command{
		{Name: "greet", Path: helper, Args: []string{"out", "hi"}},
		{Name: "lim", Path: helper, Args: []string{"args"}, ArgPolicy: exechandlers.ArgsAny, MaxArgs: 1},
	})
	cases := map[string]struct{ expr, want string }{
		"unregistered command": {
			"{{sys:Run `nope` | into .Out}}",
			`command "nope" is not registered`,
		},
		"bound command name": {
			"{{sys:Run .Cmd | into .Out}}",
			"must be a `literal`",
		},
		"bad capture value": {
			"{{sys:Run `greet` `capture=nope` | into .Out}}",
			"unknown capture mode",
		},
		"duplicate capture option": {
			"{{sys:Run `greet` `capture=stderr` `capture=stdout` | into .Out}}",
			"more than one capture=",
		},
		"bad jq expression": {
			"{{sys:Run `greet` `jq=.foo[` | into .Out}}",
			"jq",
		},
		"argument under ArgsNone": {
			"{{sys:Run `greet` .Cmd | into .Out}}",
			"takes no arguments",
		},
		"too many arguments": {
			"{{sys:Run `lim` .Cmd .Cmd | into .Out}}",
			"at most 1 argument",
		},
		"unknown function": {
			"{{sys:Exec `greet` | into .Out}}",
			"unknown function",
		},
		"missing target": {
			"{{sys:Run `greet`}}",
			"needs a result target",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := &markup.Context{
				Values: map[string]any{
					"Out": prop.NewSource(""), "Cmd": prop.NewSource(""),
				},
				Dispatcher: gooey.NewDispatcher(),
			}
			_, err := markup.Load(fstest.MapFS{"page.gooey": {Data: []byte(page(tc.expr))}}, "page.gooey", ctx)
			if err == nil {
				t.Fatalf("expected a load error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// The inventory is data (the pack-distribution doctrine): the Name*
// constants and AllNames stay in lockstep with what NewCommand serves.
func TestAllNamesPinsTheInventory(t *testing.T) {
	want := []string{exechandlers.NameRun}
	got := exechandlers.AllNames()
	if len(got) != len(want) {
		t.Fatalf("AllNames()=%v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("AllNames()=%v, want %v", got, want)
		}
	}
}

// Registration problems surface at New — app startup — never later.
func TestRegistrationErrors(t *testing.T) {
	cases := map[string]struct {
		cmds []exechandlers.Command
		want string
	}{
		"empty name": {
			[]exechandlers.Command{{Path: helper}},
			"needs a Name",
		},
		"backtick in name": {
			[]exechandlers.Command{{Name: "a`b", Path: helper}},
			"backtick",
		},
		"duplicate name": {
			[]exechandlers.Command{{Name: "x", Path: helper}, {Name: "x", Path: helper}},
			"registered twice",
		},
		"empty path": {
			[]exechandlers.Command{{Name: "x"}},
			"needs a Path",
		},
		"unresolvable bare path": {
			[]exechandlers.Command{{Name: "x", Path: "definitely-not-a-real-binary-xyz"}},
			"definitely-not-a-real-binary-xyz",
		},
		"negative timeout": {
			[]exechandlers.Command{{Name: "x", Path: helper, Timeout: -time.Second}},
			"negative Timeout",
		},
		"MaxArgs under ArgsNone": {
			[]exechandlers.Command{{Name: "x", Path: helper, MaxArgs: 2}},
			"MaxArgs",
		},
		"bad jq": {
			[]exechandlers.Command{{Name: "x", Path: helper, Jq: ".foo["}},
			"jq",
		},
		"invalid capture": {
			[]exechandlers.Command{{Name: "x", Path: helper, Capture: exechandlers.Capture(42)}},
			"invalid Capture",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := exechandlers.New(tc.cmds); err == nil {
				t.Fatalf("expected a registration error mentioning %q", tc.want)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func frameString(f *gooey.Frame, cols, rows int) string {
	var sb strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			sb.WriteRune(f.Cells.At(x, y).Rune)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
