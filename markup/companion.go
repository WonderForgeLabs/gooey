package markup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
)

// <Companion>: a child-process service declared in the page that needs
// it, instead of in the Go that loads the page. The design record is
// docs/specs/2026-08-10-markup-companions.md; the process machinery is
// gooey.CompanionCmd's, unchanged.
//
// TWO THINGS ABOUT THIS ELEMENT ARE UNUSUAL AND DELIBERATE.
//
// It is a CAPABILITY, not a widget. Markup can now name a binary, and
// markup reaches this framework through paths an app does not fully
// control — a file a watcher reloads, and swap_markup / patch_markup from
// any MCP client. That means an app serving MCP and allowing companions
// gives its clients arbitrary command execution, which is past the
// "an MCP client can do anything the keyboard can" posture recorded in
// docs/specs/2026-08-10-mcp-server.md. The perimeter is that the MCP
// server is opt-in and unauthenticated, bound wherever the host asks
// (loopback by default, not restricted to it); the off switch is
// GOOEY_MARKUP_COMPANIONS (see companionsAllowed). This was decided
// deliberately: a capability honored on one build path and refused on
// another is two languages sharing a syntax.
//
// It is STRICT about its attributes, following <x:Property> rather than
// the visual elements. A misspelled Dir= that silently ran the child in
// the wrong directory, or a misspelled Log= that silently sent its output
// to the null device, are both worse than a load failure.

// EnvCompanions is the environment variable that disables
// markup-declared companions. Unset or empty means enabled — the default
// a framework feature needs to be usable.
const EnvCompanions = "GOOEY_MARKUP_COMPANIONS"

// companionsAllowed reports whether <Companion> may be built.
//
// It FAILS CLOSED on anything it cannot read as true: a security switch
// that a typo silently turns back on is not a switch, and
// GOOEY_MARKUP_COMPANIONS=of is a plausible typo.
func companionsAllowed() bool {
	raw, ok := os.LookupEnv(EnvCompanions)
	if !ok {
		return true
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	on, err := strconv.ParseBool(raw)
	return err == nil && on
}

// companionAttrs is the element's whole attribute vocabulary. Layout
// attributes are absent on purpose: a non-visual element has no bounds to
// place, so Grid.Row on a <Companion> could only be a misunderstanding.
var companionAttrs = map[string]bool{
	"Name": true, "Path": true, "Dir": true, "Log": true,
	"KillDelay": true, "StopTimeout": true, "CleanEnv": true,
	"Error": true, "Exited": true,
}

// buildCompanion assembles the element. Everything that can be checked
// without launching anything is checked HERE, at load time: the binary
// resolves, the working directory exists, the log's directory exists, the
// durations parse. For the initial page that is before the terminal is
// touched, and for a swap it is before the tree is committed — which is
// where the App tier's grace window would have caught the same failures,
// reached without a window.
func buildCompanion(e Element, ctx *Context) (gooey.Component, error) {
	if !companionsAllowed() {
		return nil, fmt.Errorf("markup: <Companion Name=%q>: markup-declared companions are disabled by %s=%q; this element starts a child process, and the capability is switched off in this environment",
			e.Attrs["Name"], EnvCompanions, os.Getenv(EnvCompanions))
	}
	if err := checkCompanionAttrs(e); err != nil {
		return nil, err
	}
	if len(e.Children) > 0 {
		return nil, fmt.Errorf("markup: <Companion> takes no children; its arguments go in <Companion.Args> and its environment in <Companion.Env>")
	}

	name := strings.TrimSpace(e.Attrs["Name"])
	if name == "" {
		return nil, fmt.Errorf(`markup: <Companion> needs a Name (e.g. Name="worker") — it is what errors call the service`)
	}
	// CleanEnv goes through the house bool reader rather than a == "true"
	// comparison, and an unreadable value is a load error. It is a
	// security switch of the same kind as GOOEY_MARKUP_COMPANIONS:
	// CleanEnv="1" silently meaning "inherit" would hand a child named by
	// the document every API key and token in the launching shell.
	clean, err := optBool(e, "CleanEnv")
	if err != nil {
		return nil, err
	}
	c := &components.Companion{Name: name, CleanEnv: clean}

	if c.Dir, err = companionDir(e, ctx, name); err != nil {
		return nil, err
	}
	if c.Path, err = companionPath(e, ctx, name); err != nil {
		return nil, err
	}
	if c.Log, err = companionLog(e, ctx, name); err != nil {
		return nil, err
	}
	if c.KillDelay, err = optDuration(e, "KillDelay"); err != nil {
		return nil, err
	}
	if c.StopTimeout, err = optDuration(e, "StopTimeout"); err != nil {
		return nil, err
	}
	if c.Args, err = companionArgs(e, ctx); err != nil {
		return nil, err
	}
	if c.Env, err = companionEnv(e, ctx); err != nil {
		return nil, err
	}
	if suppliedAttr(e, "Error") {
		if c.Error, err = Bound[string](e, ctx, "Error"); err != nil {
			return nil, err
		}
	}
	if c.Exited, err = ctx.Command(e.Attrs["Exited"]); err != nil {
		return nil, fmt.Errorf("markup: <Companion Exited=%q>: %w", e.Attrs["Exited"], err)
	}
	return c, nil
}

func checkCompanionAttrs(e Element) error {
	var unknown []string
	for k := range e.Attrs {
		if !companionAttrs[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	known := make([]string, 0, len(companionAttrs))
	for k := range companionAttrs {
		known = append(known, k)
	}
	sort.Strings(known)
	return fmt.Errorf("markup: <Companion %s=%q>: no such attribute; <Companion> takes %s",
		unknown[0], e.Attrs[unknown[0]], strings.Join(known, ", "))
}

// companionPath resolves the executable. A bare name goes through
// exec.LookPath so a binary that is not installed is a LOAD error naming
// it, rather than a start failure behind a screen that is already up. A
// pathful one is resolved against the document's directory and made
// absolute: exec.Cmd resolves a relative Path against Dir, so leaving it
// relative would silently mean two different files depending on whether
// Dir was also set.
func companionPath(e Element, ctx *Context, name string) (string, error) {
	raw := strings.TrimSpace(e.Attrs["Path"])
	if raw == "" {
		return "", fmt.Errorf(`markup: <Companion Name=%q> needs a Path (the executable, e.g. Path="python3")`, name)
	}
	if !strings.ContainsAny(raw, `/\`) {
		p, err := exec.LookPath(raw)
		if err != nil {
			return "", fmt.Errorf("markup: <Companion Name=%q Path=%q>: %w", name, raw, err)
		}
		// LookPath joins the name onto whichever PATH entry matched, so an
		// empty entry ("PATH=/usr/bin::/bin") or a relative one
		// ("PATH=…:./tools") yields "./python3" — which exec.Cmd would then
		// resolve against Dir, the exact hazard the pathful branch below
		// takes filepath.Abs to avoid. Go's own execerrdot guard usually
		// turns that into an error first, but it is a GODEBUG anyone can
		// switch off, and this element's contract is that Path is absolute
		// by the time the component holds it.
		return absPath(p, name, raw)
	}
	p, err := absPath(ctx.hostPath(raw), name, raw)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("markup: <Companion Name=%q Path=%q>: %w", name, raw, err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("markup: <Companion Name=%q Path=%q>: %s is a directory", name, raw, p)
	}
	return p, nil
}

// absPath makes a resolved executable absolute, naming the element the
// way every other companion error does. Both branches of companionPath
// go through it: an absolute Path is the element's invariant, not a
// property of the branch that happened to produce it.
func absPath(p, name, raw string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("markup: <Companion Name=%q Path=%q>: %w", name, raw, err)
	}
	return abs, nil
}

// companionDir resolves the working directory against the document's own
// directory — see Context.Dir. A path in a configuration file that means
// something different depending on where the binary was launched from is
// a bug generator.
func companionDir(e Element, ctx *Context, name string) (string, error) {
	raw := strings.TrimSpace(e.Attrs["Dir"])
	if raw == "" {
		return "", nil
	}
	p := ctx.hostPath(raw)
	st, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("markup: <Companion Name=%q Dir=%q>: %w", name, raw, err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("markup: <Companion Name=%q Dir=%q>: %s is not a directory", name, raw, p)
	}
	return p, nil
}

// companionLog resolves the output file, document-relative like Dir. The
// file itself is opened (and truncated) when the child starts, not here:
// a document that fails to load must not have destroyed a log on its way
// out. Its DIRECTORY is checked now, because "no such directory" is the
// mistake worth catching early — and so is the path ITSELF already being
// a directory, which the parent-directory check alone would let through
// to an EISDIR at child start, behind a screen that is already up.
//
// Both checks are stats. Neither creates nor truncates anything, which is
// what keeps the "a failed load never destroys a log" invariant intact.
func companionLog(e Element, ctx *Context, name string) (string, error) {
	raw := strings.TrimSpace(e.Attrs["Log"])
	if raw == "" {
		return "", nil // CompanionCmd's default: os.DevNull
	}
	p := ctx.hostPath(raw)
	dir := filepath.Dir(p)
	st, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("markup: <Companion Name=%q Log=%q>: %w", name, raw, err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("markup: <Companion Name=%q Log=%q>: %s is not a directory", name, raw, dir)
	}
	// A missing log is the normal case (it is created at start), so only
	// an existing path that is a directory is a complaint.
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		return "", fmt.Errorf("markup: <Companion Name=%q Log=%q>: %s is a directory", name, raw, p)
	}
	return p, nil
}

// hostPath resolves one host-side path against the document's directory.
// An absolute path is left alone; everything else is joined onto
// Context.Dir, which is empty (the process's working directory) for a
// document built from bytes.
func (ctx *Context) hostPath(p string) string {
	if filepath.IsAbs(p) || ctx.Dir == "" {
		return filepath.Clean(p)
	}
	return filepath.Join(ctx.Dir, p)
}

// companionArgs reads <Companion.Args>. One <Arg> per argument, document
// order preserved: a single space-joined attribute is lossy the moment an
// argument contains a space, and XML attributes have already spent both
// quote characters, so there is no escape hatch to reach for. The text of
// each <Arg> is an ordinary text binding — resolved to a handle here,
// read once when the child starts.
func companionArgs(e Element, ctx *Context) ([]*prop.Property[string], error) {
	slot, ok := e.Props["Args"]
	if !ok {
		return nil, nil
	}
	var args []*prop.Property[string]
	for _, c := range slot.Children {
		if c.Name != "Arg" {
			return nil, fmt.Errorf("markup: <Companion.Args> children must be <Arg> elements, got <%s>", c.Name)
		}
		if len(c.Attrs) > 0 {
			return nil, fmt.Errorf("markup: <Arg> takes no attributes; the argument is its text (<Arg>--flag</Arg>)")
		}
		if len(c.Children) > 0 {
			return nil, fmt.Errorf("markup: <Arg> takes no children")
		}
		// bodyText, not TrimSpace: <Arg> is the other element whose
		// content is its body, and an argv token is exactly the kind of
		// literal a loader must not quietly rewrite. Sharing the rule is
		// also what keeps there from being two of them.
		h, err := literalOrBound(bodyText(c.Text), ctx)
		if err != nil {
			return nil, err
		}
		args = append(args, h)
	}
	return args, nil
}

// companionEnv reads <Companion.Env>. Names are literal — an environment
// variable's name is part of the contract between two programs, and
// binding it would make the contract unreadable — while values bind like
// any other text attribute.
func companionEnv(e Element, ctx *Context) ([]components.EnvVar, error) {
	slot, ok := e.Props["Env"]
	if !ok {
		return nil, nil
	}
	var env []components.EnvVar
	for _, c := range slot.Children {
		if c.Name != "Var" {
			return nil, fmt.Errorf("markup: <Companion.Env> children must be <Var> elements, got <%s>", c.Name)
		}
		for k := range c.Attrs {
			if k != "Name" && k != "Value" {
				return nil, fmt.Errorf("markup: <Var %s=%q>: no such attribute; <Var> takes Name and Value", k, c.Attrs[k])
			}
		}
		name := strings.TrimSpace(c.Attrs["Name"])
		if name == "" {
			return nil, fmt.Errorf(`markup: <Var> needs a Name (e.g. <Var Name="GOOEY_MCP_URL" Value="{{.Url}}"/>)`)
		}
		if strings.ContainsRune(name, '=') {
			return nil, fmt.Errorf("markup: <Var Name=%q>: an environment variable name cannot contain '='", name)
		}
		value, err := BoundText(c, ctx, "Value")
		if err != nil {
			return nil, err
		}
		env = append(env, components.EnvVar{Name: name, Value: value})
	}
	return env, nil
}
