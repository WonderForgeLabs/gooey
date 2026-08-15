// gitui: a lazygit-lite built entirely on the exec pack — three panes
// over a real git repository, and not one git invocation in Go code.
//
// The pitch is the exec pack's own sentence: untrusted markup never
// names a binary. Everything this app DOES is declared in gitui.gooey
// as {{sys:Run `name` …}} expressions; what this file contributes is
// the capability grant — an itemized allowlist of four commands
// (git-status, git-log, git-diff, git-branch), each a fixed binary
// plus a baked argv prefix. Markup can invoke exactly those four, with
// exactly the arguments their registrations allow, and nothing else.
// There is no shell anywhere: a Command is an argv, the selected path
// crosses into git-diff as one argv element, and nothing is ever
// re-parsed by /bin/sh. Doctrine: handlers/exec/README.md and
// docs/specs/2026-08-10-exec-pack.md.
//
//	cd apps/gitui && go run .
//
// The commands run in the app's working directory, so launched from
// anywhere inside this repository, gitui browses gooey itself —
// including the uncommitted edit you are making to it right now.
//
// # Layout
//
// Left column: an ItemsView of changed files (git status --short,
// parsed in this viewmodel) over an ItemsView of recent commits
// (git log in an NDJSON --pretty=format:, parsed likewise). Right: a
// diff pane that follows the file selection — the files view's
// SelectionChanged runs {{sys:Run `git-diff` .SelectedPath | into
// .Diff}}, and .SelectedPath is a handle read at invoke time, so the
// command the markup declared once follows the cursor forever. The
// StatusBar shows the branch and the counts (format.Count). `r`
// refreshes all three lists; the diff pane re-reads when the cursor
// moves.
//
// # The <Refresh> element
//
// `r` must run three sys:Run commands, and a KeyBinding takes exactly
// one. Rather than spend three gestures, this app registers a local
// custom element (the kanban LogPanel move):
//
//	<Refresh Gesture="r" Status="{{sys:Run `git-status` | into .StatusRaw}}" …/>
//
// Its builder resolves each attribute with parent.Command — handler
// expressions resolve on a custom element's attributes exactly the way
// Click does — and returns a *gooey.KeyBinding whose Command runs all
// three. The returned component is non-visual, so the framework hangs
// it off the root Grid: root scope, live no matter what has focus. The
// builder also posts one initial run through the Dispatcher, which is
// how the panes are full before anyone presses anything.
//
// # Honest limits
//
//   - The diff is worktree-vs-index (`git diff -- path`), so an
//     untracked or staged-only file shows an empty pane; the pane says
//     so. A fifth registration (`diff --cached`) would show the index
//     side — the allowlist is the place to widen, never the markup.
//   - git's --pretty=format: has no JSON escaping, so a commit subject
//     containing a double quote breaks its own NDJSON line; the parser
//     skips such lines rather than invent data.
//   - Failures arrive as "ERROR: …" strings in the same properties
//     (the v1 delivery contract); the parsers treat those as empty
//     lists and the diff pane shows the string as-is.
//   - Long diffs clip to the pane. Windowed scroll-back is ItemsView
//     territory, not a Text's.
//
// GIF: docs-and-demos workflow.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/format"
	exechandlers "github.com/WonderForgeLabs/gooey/handlers/exec"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// fileRow is one `git status --short` line: the two-column XY mark and
// the path.
type fileRow struct {
	Mark string
	Path string
}

// commitRow is one NDJSON line of the git-log registration's
// --pretty=format:. The tags are the format's keys.
type commitRow struct {
	Hash    string `json:"h"`
	Subject string `json:"s"`
	Author  string `json:"a"`
}

var (
	stagedStyle    = render.Style{Fg: render.RGB(120, 200, 140)}
	unstagedStyle  = render.Style{Fg: render.RGB(255, 170, 60)}
	untrackedStyle = render.Style{Fg: render.RGB(140, 140, 150)}
)

// markStyle colors a status mark the lazygit way: untracked dim,
// anything with a worktree-side change orange, staged-only green.
func markStyle(mark string) render.Style {
	switch {
	case mark == "??":
		return untrackedStyle
	case len(mark) == 2 && mark[1] != ' ':
		return unstagedStyle
	default:
		return stagedStyle
	}
}

// parseStatus turns `git status --short` output into rows. A delivery
// that is an "ERROR: …" string (v1's failure contract) parses as no
// rows rather than as one garbage row. Renames print "R  old -> new";
// the diff wants the new name. Paths git chose to quote (core.quotePath
// escapes non-ASCII) pass through still quoted — honest, not clever.
func parseStatus(raw string) []fileRow {
	if strings.HasPrefix(raw, "ERROR:") {
		return nil
	}
	var rows []fileRow
	for _, line := range strings.Split(raw, "\n") {
		if len(line) < 4 {
			continue
		}
		mark, path := line[:2], line[3:]
		if i := strings.LastIndex(path, " -> "); i >= 0 {
			path = path[i+4:]
		}
		rows = append(rows, fileRow{Mark: mark, Path: path})
	}
	return rows
}

// parseLog turns the git-log registration's NDJSON into rows. Each
// line SHOULD be one JSON object, but --pretty=format: has no JSON
// escaping, so a subject containing `"` breaks its own line — those
// are skipped, per the package doc's honest-limits list.
func parseLog(raw string) []commitRow {
	if strings.HasPrefix(raw, "ERROR:") {
		return nil
	}
	var rows []commitRow
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var c commitRow
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue
		}
		rows = append(rows, c)
	}
	return rows
}

func projectFile(f fileRow) map[string]any {
	return map[string]any{"Mark": f.Mark, "Path": f.Path, "MarkStyle": markStyle(f.Mark)}
}

func projectCommit(c commitRow) map[string]any {
	return map[string]any{"Hash": c.Hash, "Subject": c.Subject, "Author": c.Author}
}

func main() {
	// --- the capability grant: the whole API surface markup gets ---
	//
	// Four names, each pinned to the `git` the PATH resolves at startup
	// plus a baked argv prefix. Only git-diff accepts an argument from
	// markup (ArgsAny, capped at one), and its baked `--` means that
	// argument can only ever be a pathspec — never an option, whatever
	// the document sends. The child's environment starts EMPTY (the
	// pack's scrub-by-default), which these subcommands don't mind; a
	// command that needed the user's git config would name HOME in
	// PassEnv, in this registration, in Go — not in markup.
	provider, err := exechandlers.New([]exechandlers.Command{
		{Name: "git-status", Path: "git", Args: []string{"status", "--short"}},
		{Name: "git-log", Path: "git", Args: []string{
			"log", "-n", "40", `--pretty=format:{"h":"%h","s":"%s","a":"%an"}`}},
		{Name: "git-diff", Path: "git", Args: []string{"diff", "--no-color", "--"},
			ArgPolicy: exechandlers.ArgsAny, MaxArgs: 1},
		{Name: "git-branch", Path: "git", Args: []string{"branch", "--show-current"}},
	})
	if err != nil {
		gooey.Exit(err)
	}
	markup.RegisterHandlers(exechandlers.URI, provider)

	// --- viewmodel: raw deliveries in, parsed projections out ---
	//
	// The three `into` targets are plain string sources; everything the
	// panes show is a computed over them, so a delivery repaints exactly
	// the panes whose parse actually changed shape.
	statusRaw := prop.NewSource("")
	logRaw := prop.NewSource("")
	branchRaw := prop.NewSource("")
	diff := prop.NewSource("")

	files := prop.NewComputed(func() []fileRow { return parseStatus(statusRaw.Get()) })
	commits := prop.NewComputed(func() []commitRow { return parseLog(logRaw.Get()) })

	fileSel := prop.NewSource(0)
	commitSel := prop.NewSource(0)

	// selectedPath is what {{sys:Run `git-diff` .SelectedPath …}} reads
	// at invoke time — a handle, not a captured value, so one markup
	// expression follows the cursor for the app's lifetime.
	selectedPath := prop.NewComputed(func() string {
		rows := files.Get()
		i := fileSel.Get()
		if i < 0 || i >= len(rows) {
			return ""
		}
		return rows[i].Path
	})

	diffView := prop.NewComputed(func() string {
		d := diff.Get()
		if strings.TrimSpace(d) == "" {
			return "move the selection in changes — the worktree diff lands here\n\n" +
				"(untracked and staged-only files have no worktree diff, so this\n" +
				"pane stays empty for those; see the package doc's honest limits)"
		}
		return d
	})

	branch := prop.NewComputed(func() string {
		b := strings.TrimSpace(branchRaw.Get())
		if b == "" || strings.HasPrefix(b, "ERROR:") {
			return "(detached)"
		}
		return b
	})

	changedCount := prop.NewComputed(func() int { return len(files.Get()) })
	commitCount := prop.NewComputed(func() int { return len(commits.Get()) })

	var app *gooey.App

	ctx := &markup.Context{
		Values: map[string]any{
			// into targets — sys:Run deliveries land here.
			"StatusRaw": statusRaw,
			"LogRaw":    logRaw,
			"BranchRaw": branchRaw,
			"Diff":      diff,

			// the panes' bindings.
			"FileItems":    components.Items(files, projectFile),
			"FileSel":      fileSel,
			"SelectedPath": selectedPath,
			"DiffView":     diffView,
			"CommitItems":  components.Items(commits, projectCommit),
			"CommitSel":    commitSel,

			// the StatusBar's: branch plus format.Count counts.
			"Branch":       branch,
			"ChangedCount": format.Count(changedCount),
			"CommitCount":  format.Count(commitCount),

			"Quit": gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
		Components: map[string]markup.Builder{
			// Refresh is a demo-local element, not a framework one: one
			// gesture, several handler expressions. parent.Command
			// resolves each attribute exactly the way Click resolves —
			// including {{sys:…}} handler expressions — and the returned
			// KeyBinding is non-visual, so the builder machinery hangs
			// it off the parent (here the root Grid: root scope, live
			// regardless of focus). It also posts one initial run, which
			// executes on the UI loop after the page is built — that is
			// why the panes are populated before any key is pressed.
			"Refresh": func(e markup.Element, parent *markup.Context) (gooey.Component, error) {
				gesture, err := input.ParseGesture(e.Attrs["Gesture"])
				if err != nil {
					return nil, fmt.Errorf("gitui: <Refresh Gesture=%q>: %w", e.Attrs["Gesture"], err)
				}
				var cmds []gooey.Action
				for _, name := range []string{"Status", "Log", "Branch"} {
					raw, ok := e.Attrs[name]
					if !ok {
						return nil, fmt.Errorf("gitui: <Refresh> needs a %s attribute", name)
					}
					cmd, err := parent.Command(raw)
					if err != nil {
						return nil, err
					}
					cmds = append(cmds, cmd)
				}
				all := gooey.Command(func() {
					for _, c := range cmds {
						c.Run()
					}
				})
				app.Post(all.Run)
				return &gooey.KeyBinding{Gesture: gesture, Command: all}, nil
			},
		},
	}

	// Find the markup beside the source under `go run .`, beside the
	// executable otherwise — the kanban split. The git commands
	// themselves run in the CWD (registration Dir is ""), which is what
	// makes this app browse whatever repository it is launched inside.
	dir := "."
	if _, err := os.Stat(filepath.Join(dir, "gitui.gooey")); err != nil {
		exe, _ := os.Executable()
		dir = filepath.Dir(exe)
	}

	app = gooey.NewApp(markup.Page(os.DirFS(dir), "gitui.gooey", ctx))
	// Deliveries are Set on the UI goroutine — the Dispatcher is how
	// they get there, and a document using handler namespaces fails to
	// load without one. The App drains its own, so hand that one over.
	ctx.Dispatcher = app.Dispatcher()

	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
