// Package exechandlers is the sys: handler namespace: local commands
// from markup, behind an explicit allowlist.
//
//	<Gooey xmlns:sys="gooey.dev/handlers/exec">
//	  <Button Content="status" Click="{{sys:Run `git-status` | into .Out}}"/>
//
// The host app grants the capability by registering the provider — and
// unlike net, the grant here is itemized. Registration names every
// command markup may run:
//
//	p, err := exechandlers.New(
//	        exechandlers.Command{Name: "git-status", Path: "git",
//	                Args: []string{"status", "--short"}},
//	)
//	markup.RegisterHandlers(exechandlers.URI, p)
//
// Untrusted markup NEVER names a binary. The first argument to sys:Run
// must be a backtick literal naming a registered Command, checked at
// load time; the registered set IS the API surface. There is no shell
// anywhere in this package: a Command is an argv, markup arguments are
// appended as argv elements, and nothing is ever re-parsed by /bin/sh.
//
// Process hygiene follows the companions record: the child starts in
// its own process group (Unix), never gets a tty (stdio is pipes and
// the null device), runs against a scrubbed environment (empty plus
// the registration's explicit entries and pass-throughs), and a
// timeout kills the whole group — SIGTERM, a grace window, SIGKILL.
//
// v1 shape, matching net: the captured output is delivered to the
// `into` target as a string; failures are delivered to the same target
// as an "ERROR: …" string. Which stream is captured is the capture
// enum (stdout, stderr, combined, both, exit-code), a registration
// default overridable per call with a `capture=…` option literal.
// Structured extraction is provider-side jq (gojq, pure Go): a
// registration default or a per-call `jq=…` option literal, compiled
// at load time and applied to the delivered text. The pipeline-grammar
// v2 mapping — capture as multiple `into` results, jq as a converter
// stage — is recorded in docs/specs/2026-08-10-exec-pack.md.
package exechandlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/itchyny/gojq"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// URI is the namespace URI markup declares to reach this provider. The
// conventional prefix is sys: — <Gooey xmlns:sys="gooey.dev/handlers/exec">.
const URI = "gooey.dev/handlers/exec"

// The provider's function names — what markup writes after the prefix.
const (
	// NameRun invokes one allowlisted command:
	// {{sys:Run `name` [options] [args…] | into .Target}}.
	NameRun = "Run"
)

// AllNames lists every function the provider serves, in documentation
// order. The set is the markup-facing API of the namespace.
func AllNames() []string {
	return []string{
		NameRun,
	}
}

// Defaults, overridable per Provider (Options) and per Command.
const (
	// DefaultTimeout bounds one invocation, including the kill
	// escalation, unless the registration says otherwise.
	DefaultTimeout = 30 * time.Second
	// DefaultKillDelay is the grace between SIGTERM and SIGKILL when a
	// timeout fires.
	DefaultKillDelay = 2 * time.Second
	// DefaultMaxOutput caps the bytes kept from each captured stream —
	// the same runaway-output argument as net's DefaultMaxBody. The
	// child is drained past the cap (so it never blocks on a full
	// pipe), but only the first cap bytes are delivered.
	DefaultMaxOutput = 1 << 20
)

// Capture selects what one invocation delivers into its target. It is
// OS-agnostic by construction: streams and exit codes are the portable
// vocabulary of processes, and nothing here names a signal or a shell.
type Capture int

const (
	// CaptureStdout delivers standard output (the default). A non-zero
	// exit is an "ERROR: …" delivery instead.
	CaptureStdout Capture = iota
	// CaptureStderr delivers standard error; stdout is discarded.
	// A non-zero exit is an "ERROR: …" delivery instead.
	CaptureStderr
	// CaptureCombined delivers both streams interleaved through one
	// pipe, in write order — the 2>&1 shape without a shell. A non-zero
	// exit is an "ERROR: …" delivery instead.
	CaptureCombined
	// CaptureBoth delivers a JSON object keeping the streams tagged and
	// separate: {"exit":N,"stdout":"…","stderr":"…"}. The exit code is
	// data here, never an ERROR string — the object always arrives.
	// This is the structured form jq extraction composes with.
	CaptureBoth
	// CaptureExitCode delivers the exit code in decimal ("0", "1", …).
	// Like CaptureBoth, a non-zero exit is data, not an error.
	CaptureExitCode
)

// captureNames is the markup spelling of the enum, as it appears in a
// `capture=…` option literal.
var captureNames = map[string]Capture{
	"stdout":    CaptureStdout,
	"stderr":    CaptureStderr,
	"combined":  CaptureCombined,
	"both":      CaptureBoth,
	"exit-code": CaptureExitCode,
}

func parseCapture(s string) (Capture, error) {
	if c, ok := captureNames[s]; ok {
		return c, nil
	}
	return 0, fmt.Errorf("unknown capture mode %q; capture= takes stdout, stderr, combined, both, or exit-code", s)
}

// ArgPolicy says whether markup may append arguments to a registered
// Command's argv.
type ArgPolicy int

const (
	// ArgsNone (the default) locks the argv to the registration:
	// markup passing any argument is a load error. Secure by default —
	// a command that takes no input can't be steered.
	ArgsNone ArgPolicy = iota
	// ArgsAny lets markup append arguments after the registered Args.
	// They are argv elements only — never shell-interpreted — but their
	// values are the document's to choose, so grant this to commands
	// that are safe under arbitrary arguments. MaxArgs can cap the
	// count.
	ArgsAny
)

// Command is one allowlisted entry: a markup-visible name bound to a
// fixed binary and argv prefix. What is not in this struct, markup
// cannot ask for.
type Command struct {
	// Name is what markup writes: {{sys:Run `name` …}}. Required,
	// unique per Provider. It may not contain a backtick (it could
	// never be spelled as a literal).
	Name string
	// Path is the binary. A bare name ("git") is resolved via the PATH
	// at registration time and pinned to the absolute result, so what
	// runs later is what the host saw at startup. A path containing a
	// separator is taken as-is.
	Path string
	// Args is the baked argv prefix, always passed before any
	// markup-supplied arguments.
	Args []string
	// ArgPolicy controls markup-supplied arguments (default ArgsNone).
	ArgPolicy ArgPolicy
	// MaxArgs caps how many arguments markup may pass under ArgsAny
	// (0 = unlimited). Setting it under ArgsNone is a registration
	// error — it could only mislead.
	MaxArgs int
	// Env is the child's explicit environment ("K=V" entries). The
	// parent environment is NEVER inherited: the child starts from
	// empty, plus PassEnv, plus these.
	Env []string
	// PassEnv names parent environment variables to copy through, if
	// set. The scrub-by-default direction: secrets in the app's
	// environment do not leak into children unless named here.
	PassEnv []string
	// Dir is the working directory ("" = the app's).
	Dir string
	// Timeout overrides the provider default for this command
	// (0 = provider default).
	Timeout time.Duration
	// Capture is the default capture mode (zero value CaptureStdout),
	// overridable per call with a `capture=…` option literal.
	Capture Capture
	// Jq is an optional gojq expression applied to the delivered text
	// (which must then be JSON), compiled at registration. A per-call
	// `jq=…` option literal overrides it.
	Jq string
}

// registered is a Command plus its compiled pieces.
type registered struct {
	Command
	jq *gojq.Code
}

// Provider implements markup.HandlerProvider for the sys: namespace.
type Provider struct {
	cmds      map[string]*registered
	timeout   time.Duration
	killDelay time.Duration
	maxOutput int64
}

// Option configures a Provider.
type Option func(*Provider)

// WithDefaultTimeout sets the invocation timeout used by commands that
// do not declare their own.
func WithDefaultTimeout(d time.Duration) Option { return func(p *Provider) { p.timeout = d } }

// WithKillDelay sets the SIGTERM→SIGKILL grace used when a timeout
// fires.
func WithKillDelay(d time.Duration) Option { return func(p *Provider) { p.killDelay = d } }

// WithMaxOutput caps the bytes kept from each captured stream.
func WithMaxOutput(n int64) Option { return func(p *Provider) { p.maxOutput = n } }

// New builds the provider over an explicit command allowlist. Every
// registration problem — a duplicate or unspellable name, a binary the
// PATH cannot resolve, a jq expression that does not compile, a
// policy contradiction — is an error here, at startup, not at click.
func New(cmds []Command, opts ...Option) (*Provider, error) {
	p := &Provider{
		cmds:      make(map[string]*registered, len(cmds)),
		timeout:   DefaultTimeout,
		killDelay: DefaultKillDelay,
		maxOutput: DefaultMaxOutput,
	}
	for _, o := range opts {
		o(p)
	}
	for _, c := range cmds {
		if c.Name == "" {
			return nil, fmt.Errorf("exec: a Command needs a Name")
		}
		if strings.ContainsRune(c.Name, '`') {
			return nil, fmt.Errorf("exec: command name %q contains a backtick and could never be spelled as a markup literal", c.Name)
		}
		if _, dup := p.cmds[c.Name]; dup {
			return nil, fmt.Errorf("exec: command %q registered twice", c.Name)
		}
		if c.Path == "" {
			return nil, fmt.Errorf("exec: command %q needs a Path", c.Name)
		}
		if !strings.ContainsAny(c.Path, `/\`) {
			resolved, err := exec.LookPath(c.Path)
			if err != nil {
				return nil, fmt.Errorf("exec: command %q: %w", c.Name, err)
			}
			c.Path = resolved
		}
		if c.Timeout < 0 {
			return nil, fmt.Errorf("exec: command %q: negative Timeout", c.Name)
		}
		if c.MaxArgs < 0 {
			return nil, fmt.Errorf("exec: command %q: negative MaxArgs", c.Name)
		}
		if c.MaxArgs > 0 && c.ArgPolicy == ArgsNone {
			return nil, fmt.Errorf("exec: command %q: MaxArgs is set but ArgPolicy is ArgsNone — the cap would never apply", c.Name)
		}
		if c.Capture < CaptureStdout || c.Capture > CaptureExitCode {
			return nil, fmt.Errorf("exec: command %q: invalid Capture value %d", c.Name, c.Capture)
		}
		r := &registered{Command: c}
		if c.Jq != "" {
			code, err := compileJq(c.Jq)
			if err != nil {
				return nil, fmt.Errorf("exec: command %q: %w", c.Name, err)
			}
			r.jq = code
		}
		p.cmds[c.Name] = r
	}
	return p, nil
}

func compileJq(expr string) (*gojq.Code, error) {
	q, err := gojq.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("jq %q: %w", expr, err)
	}
	code, err := gojq.Compile(q)
	if err != nil {
		return nil, fmt.Errorf("jq %q: %w", expr, err)
	}
	return code, nil
}

// names lists the registered command names, sorted, for error messages.
func (p *Provider) names() []string {
	ns := make([]string, 0, len(p.cmds))
	for n := range p.cmds {
		ns = append(ns, n)
	}
	sort.Strings(ns)
	return ns
}

// NewCommand resolves one {{sys:…}} expression at load time.
func (p *Provider) NewCommand(c *markup.Call) (gooey.Command, error) {
	switch c.Fn {
	case NameRun:
		return p.runCommand(c)
	}
	return nil, fmt.Errorf("unknown function %q; exec provides: %s", c.Fn, strings.Join(AllNames(), ", "))
}

// invocation is everything one call resolved to at load time, minus the
// argument values (those are snapshots taken per click).
type invocation struct {
	reg     *registered
	capture Capture
	jq      *gojq.Code
}

func (p *Provider) runCommand(c *markup.Call) (gooey.Command, error) {
	if len(c.Args) == 0 {
		return nil, fmt.Errorf("Run takes a command name first: {{sys:Run `name` …}}")
	}
	if !c.Args[0].IsLiteral() {
		return nil, fmt.Errorf("the command name must be a `literal`, not .%s — the allowlist is checked at load time, so it cannot be a runtime value", c.Args[0].Path)
	}
	name := c.Args[0].String()
	reg, ok := p.cmds[name]
	if !ok {
		return nil, fmt.Errorf("command %q is not registered; registered commands: %v", name, p.names())
	}
	if !c.Target.Valid() {
		return nil, fmt.Errorf("Run needs a result target — add `| into .SomeProperty`")
	}

	inv := invocation{reg: reg, capture: reg.Capture, jq: reg.jq}

	// Option literals sit between the name and the arguments:
	// `capture=stderr`, `jq=.items[].name`, each at most once. A `--`
	// literal ends option parsing, so a command whose FIRST argument
	// genuinely starts with "capture=" or "jq=" can still receive it.
	// Options are literals by definition — a bound .Path is always an
	// argument — which keeps every option a load-time fact.
	i := 1
	var haveCapture, haveJq bool
scan:
	for ; i < len(c.Args); i++ {
		a := c.Args[i]
		if !a.IsLiteral() {
			break
		}
		s := a.String()
		switch {
		case s == "--":
			i++
			break scan
		case strings.HasPrefix(s, "capture="):
			if haveCapture {
				return nil, fmt.Errorf("command %q: more than one capture= option", name)
			}
			mode, err := parseCapture(strings.TrimPrefix(s, "capture="))
			if err != nil {
				return nil, fmt.Errorf("command %q: %w", name, err)
			}
			inv.capture, haveCapture = mode, true
		case strings.HasPrefix(s, "jq="):
			if haveJq {
				return nil, fmt.Errorf("command %q: more than one jq= option", name)
			}
			code, err := compileJq(strings.TrimPrefix(s, "jq="))
			if err != nil {
				return nil, fmt.Errorf("command %q: %w", name, err)
			}
			inv.jq, haveJq = code, true
		default:
			break scan
		}
	}

	args := c.Args[i:]
	switch reg.ArgPolicy {
	case ArgsNone:
		if len(args) > 0 {
			return nil, fmt.Errorf("command %q takes no arguments from markup (ArgPolicy is ArgsNone), got %d", name, len(args))
		}
	case ArgsAny:
		if reg.MaxArgs > 0 && len(args) > reg.MaxArgs {
			return nil, fmt.Errorf("command %q allows at most %d arguments, got %d", name, reg.MaxArgs, len(args))
		}
	}

	target := c.Target
	return func() {
		// Read argument handles HERE: the command runs on the UI
		// goroutine, where touching properties is legal. Values, not
		// handles, cross to the process goroutine.
		extra := make([]string, len(args))
		for j, a := range args {
			extra[j] = a.String()
		}
		go func() { target.Deliver(p.run(inv, extra)) }()
	}, nil
}

// bothResult is CaptureBoth's tagged delivery. A struct, so the key
// order is deterministic.
type bothResult struct {
	Exit   int    `json:"exit"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// run executes one invocation off the UI goroutine and renders the
// outcome as the string the target property will hold.
func (p *Provider) run(inv invocation, extra []string) string {
	reg := inv.reg
	argv := make([]string, 0, len(reg.Args)+len(extra))
	argv = append(argv, reg.Args...)
	argv = append(argv, extra...)

	cmd := exec.Command(reg.Path, argv...)
	cmd.Dir = reg.Dir
	// Scrubbed by construction: a non-nil empty slice means "no
	// environment at all" to os/exec, where nil would mean "inherit
	// everything".
	env := []string{}
	for _, k := range reg.PassEnv {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	cmd.Env = append(env, reg.Env...)

	// Stdin stays nil: os/exec gives the child the null device. The
	// child never sees a tty — its stdio is pipes and /dev/null, and
	// on Unix its own process group keeps it out of the terminal's
	// foreground group.
	outBuf := &boundedBuf{max: p.maxOutput}
	errBuf := &boundedBuf{max: p.maxOutput}
	switch inv.capture {
	case CaptureStdout, CaptureBoth, CaptureExitCode:
		cmd.Stdout, cmd.Stderr = outBuf, errBuf
	case CaptureStderr:
		cmd.Stdout, cmd.Stderr = nil, errBuf // stdout to the null device
	case CaptureCombined:
		// The same Writer value on both: os/exec then serializes the
		// writes, and the child's own write order is the interleaving.
		cmd.Stdout, cmd.Stderr = outBuf, outBuf
	}
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("ERROR: %s: %s", reg.Name, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	timeout := reg.Timeout
	if timeout <= 0 {
		timeout = p.timeout
	}
	var werr error
	select {
	case werr = <-done:
	case <-time.After(timeout):
		// The escalation from the companions record: ask the group
		// politely, wait the grace, then kill it. Always join Wait —
		// the pipes close only when the group's writers are gone.
		terminateProcess(cmd.Process)
		select {
		case <-done:
		case <-time.After(p.killDelay):
			killProcess(cmd.Process)
			<-done
		}
		return fmt.Sprintf("ERROR: %s: timed out after %s", reg.Name, timeout)
	}

	code := cmd.ProcessState.ExitCode()
	switch inv.capture {
	case CaptureExitCode:
		return p.finish(inv, strconv.Itoa(code))
	case CaptureBoth:
		b, err := json.Marshal(bothResult{Exit: code, Stdout: outBuf.String(), Stderr: errBuf.String()})
		if err != nil {
			return fmt.Sprintf("ERROR: %s: %s", reg.Name, err)
		}
		return p.finish(inv, string(b))
	}

	// Stream captures treat failure as failure: an "ERROR: …" delivery
	// carrying the exit and a stderr excerpt. A page that wants exit
	// codes or failing output as data uses capture=exit-code or
	// capture=both.
	if werr != nil {
		if code < 0 {
			return fmt.Sprintf("ERROR: %s: %s", reg.Name, werr)
		}
		msg := fmt.Sprintf("ERROR: %s: exit status %d", reg.Name, code)
		if inv.capture == CaptureStdout {
			if line := firstLine(errBuf.String()); line != "" {
				msg += ": " + line
			}
		}
		return msg
	}
	if inv.capture == CaptureStderr {
		return p.finish(inv, errBuf.String())
	}
	return p.finish(inv, outBuf.String())
}

// finish applies the invocation's jq extraction, if any.
func (p *Provider) finish(inv invocation, payload string) string {
	if inv.jq == nil {
		return payload
	}
	var v any
	if err := json.Unmarshal([]byte(payload), &v); err != nil {
		return fmt.Sprintf("ERROR: %s: jq input is not JSON: %s", inv.reg.Name, err)
	}
	iter := inv.jq.Run(v)
	var parts []string
	for {
		r, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := r.(error); isErr {
			return fmt.Sprintf("ERROR: %s: jq: %s", inv.reg.Name, err)
		}
		if s, isStr := r.(string); isStr {
			// A string result is delivered raw — the jq -r shape,
			// which is what a Text binding wants.
			parts = append(parts, s)
		} else {
			b, err := json.Marshal(r)
			if err != nil {
				return fmt.Sprintf("ERROR: %s: jq: %s", inv.reg.Name, err)
			}
			parts = append(parts, string(b))
		}
	}
	// Multiple results join with newlines, matching the jq CLI.
	return strings.Join(parts, "\n")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// boundedBuf keeps the first max bytes and keeps counting past them:
// the child is always drained, so it can never block on a full pipe,
// but a runaway stream cannot become the app's memory profile either.
type boundedBuf struct {
	buf       bytes.Buffer
	n         int64
	max       int64
	truncated bool
}

func (b *boundedBuf) Write(p []byte) (int, error) {
	if rem := b.max - b.n; rem > 0 {
		if int64(len(p)) <= rem {
			b.buf.Write(p)
		} else {
			b.buf.Write(p[:rem])
			b.truncated = true
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	b.n += int64(len(p))
	return len(p), nil
}

func (b *boundedBuf) String() string { return b.buf.String() }
