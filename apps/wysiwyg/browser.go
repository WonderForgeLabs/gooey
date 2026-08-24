package main

// The workspace: a DIRECTORY you open, browse and edit in, rather than
// one document the binary happened to start with.
//
// # fs.FS is the seam, and it is already the framework's
//
// CLAUDE.md states it as an invariant: os.DirFS + watcher in development
// and embed.FS in a release build are the SAME code path, and that is why
// every markup load in this repo takes an fs.FS rather than a path. A
// workspace is that invariant used for what it was for — os.DirFS(root)
// and nothing downstream needs to know whether the root is a directory,
// a zip, or a test's fstest.MapFS. `scan` and `read` take fs.FS only.
//
// WRITING IS DELIBERATELY NOT ON THAT SEAM, and the asymmetry is the
// point rather than an oversight. fs.FS is a READ interface; there is no
// fs.WriteFile, and inventing one here would be a parallel path exactly
// where the coordinator asked for none. So the workspace carries the
// read seam AND, separately, an optional `dir` — the real directory it
// came from, empty when it did not come from one. Save is available when
// and only when `dir` is non-empty, which makes "this workspace cannot
// be written to" a fact the UI can show rather than an error you find
// out about by pressing the key.
//
// # There is no directory watcher, and that is a decision
//
// gooey's hot reload works because Content.Watch reports only THAT the
// source changed and never the new tree: the rebuild has to happen on
// the UI goroutine, and a watcher runs on its own. A workspace watcher
// makes that mistake much easier to make — the obvious implementation
// walks the directory on the polling goroutine and hands back a fresh
// file list, and a file list is nearly a tree.
//
// It would also inherit the baseline race: markup.Watch takes its
// baseline INSIDE its own goroutine, so a write that races the launch is
// swallowed. Opening a folder and immediately watching it is precisely
// that shape.
//
// So refresh is EXPLICIT — Project → Refresh — and the file list is a
// snapshot with a visible age. A stale list you can see and refresh is
// honest; a watcher that silently misses the first write is not.

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/WonderForgeLabs/gooey/components"
)

// maxWorkspaceFiles caps the scan. A workspace is somebody's home
// directory sooner or later, and an unbounded walk is a hang with no
// error — the editor would simply never draw its next frame.
const maxWorkspaceFiles = 4000

// workspace is an opened directory.
type workspace struct {
	// fsys is the READ seam and is always set once a workspace is open.
	fsys fs.FS
	// dir is the real directory behind fsys, or "" when there is not one
	// (a test's MapFS, an embed). Save exists exactly when this does.
	dir   string
	label string
	files []string
	// err is why the last open or scan failed, shown rather than logged.
	err string
}

// fuzzyMatch is a case-insensitive subsequence match with a score and the
// matched positions.
//
// # Why this is written here and not imported
//
// The repo already has one, at cmd/finder/main.go's `fuzzy`. It is in
// `package main`, so it is not importable by anything, and cmd/finder is
// in the ROOT module while this is a nested one — two reasons, either of
// which is sufficient. This is a genuine duplicate and it should not stay
// one: the fix is to lift the matcher into a library package that both
// call. That is a root-module change touching a demo another agent owns,
// so it is reported rather than done here.
//
// Two things are FIXED relative to that copy rather than reproduced:
//
//   - It indexes RUNES, not bytes. The original does strings.ToLower and
//     then indexes the byte slice, while its caller walks the string with
//     `for i, r := range` — which yields byte offsets, so the two agree
//     only for ASCII and mis-highlight anything else.
//   - The segment-start bonus counts `/` as well as the punctuation the
//     original had, which matters here because the corpus is paths and
//     the thing a user is aiming at is almost always a path segment.
//
// # Scoring, and why the obvious version ranks backwards
//
// The first version here scored +1 per hit, +streak for a run, and +4 for
// a hit at a segment start. It ranked "m_a_i_n_x.go" ABOVE "main.go" for
// the query "main", which is the single worst thing a fuzzy list can do.
// The arithmetic is worth keeping because the mistake is not obvious:
// every underscore is a segment break, so the scattered path collected
// FOUR boundary bonuses (16) while the contiguous one collected one (4),
// and no plausible per-run bonus closes a gap that large.
//
// Three changes fix it, and each is doing separate work:
//
//   - The boundary bonus applies ONLY to the first character of a run.
//     Rewarding it per character is what let a string of one-character
//     runs out-earn a real match; a run's boundary is a property of the
//     run.
//   - Consecutive characters are weighted heavily (3 per step), because
//     contiguity is the strongest evidence that this is the thing the
//     user meant.
//   - Skipped characters BETWEEN hits cost 3 each. This is the one that
//     actually does it: without a gap penalty, spreading a match out is
//     free, and free is why the scattered path won.
//
// Then a mild length penalty so a short path beats a long one that
// contains it.
//
// TestFuzzyPrefersContiguousAndSegmentStarts pins all three orderings,
// and it is the test that caught the inversion.
const (
	fuzzyRun      = 3 // per step of a consecutive run
	fuzzyBoundary = 8 // once, at the start of a run that begins a segment
	fuzzyGap      = 3 // per character skipped between hits
)

func fuzzyMatch(q, s string) (bool, int, []int) {
	qr := []rune(strings.ToLower(q))
	sr := []rune(s)
	if len(qr) == 0 {
		return true, 0, nil
	}
	hits := make([]int, 0, len(qr))
	score, streak, qi, last := 0, 0, 0, -1
	for i, r := range sr {
		if qi >= len(qr) {
			break
		}
		if unicode.ToLower(r) != qr[qi] {
			streak = 0
			continue
		}
		score += 1 + fuzzyRun*streak
		if streak == 0 {
			// Start of a run. The boundary bonus belongs to the run, not
			// to each of its characters.
			if i == 0 || isSegmentBreak(sr[i-1]) {
				score += fuzzyBoundary
			}
			// Gaps are only counted BETWEEN hits: whatever precedes the
			// first match is not a gap, it is the rest of the path, and
			// charging for it would just re-penalize length twice.
			if last >= 0 {
				score -= fuzzyGap * (i - last - 1)
			}
		}
		streak++
		last = i
		hits = append(hits, i)
		qi++
	}
	if qi < len(qr) {
		return false, 0, nil
	}
	return true, score - len(sr)/8, hits
}

func isSegmentBreak(r rune) bool {
	switch r {
	case '/', '_', '-', '.', ' ':
		return true
	}
	return false
}

// openWorkspace opens dir as the workspace. The path is resolved to an
// absolute one up front so the label the UI shows is the same string a
// later save writes into — a relative root that moves with the process's
// working directory is a save that lands somewhere the user was not told
// about.
func openWorkspace(dir string) *workspace {
	abs, err := filepath.Abs(strings.TrimSpace(dir))
	if err != nil {
		return &workspace{err: "cannot resolve " + dir + ": " + err.Error()}
	}
	st, err := os.Stat(abs)
	if err != nil {
		return &workspace{err: "cannot open " + abs + ": " + err.Error()}
	}
	if !st.IsDir() {
		return &workspace{err: abs + " is a file, not a folder"}
	}
	ws := &workspace{fsys: os.DirFS(abs), dir: abs, label: abs}
	ws.scan()
	return ws
}

// scan walks the workspace and collects what can be opened. It is the
// only place the file list is produced, and it runs on the UI goroutine
// (from a command), which is what keeps the property writes downstream of
// it legal.
func (w *workspace) scan() {
	if w.fsys == nil {
		return
	}
	w.files = w.files[:0]
	w.err = ""
	err := fs.WalkDir(w.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// A directory we cannot read is skipped, not fatal: one
			// unreadable subtree must not cost the user the workspace.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			// Dot-directories are pruned at EVERY depth, not just the
			// top. The same trap CLAUDE.md's module discovery documents:
			// a top-anchored filter walks into .git and into a worktree
			// holding whole checkouts.
			if p != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor" || d.Name() == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		if len(w.files) >= maxWorkspaceFiles {
			return fs.SkipAll
		}
		w.files = append(w.files, p)
		return nil
	})
	if err != nil {
		w.err = "scan failed: " + err.Error()
	}
	sort.Strings(w.files)
	if len(w.files) >= maxWorkspaceFiles {
		w.err = "showing the first " + itoa(maxWorkspaceFiles) + " files; narrow the folder"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// ranked is the file list filtered and re-ranked by the query — the fzf
// shape, which the type-ahead spec (docs/specs/2026-08-11-type-ahead-search.md)
// names as the OTHER design and explicitly does not put inside
// <TypeAhead>. <TypeAhead> moves a selection by prefix and never filters;
// this filters and re-ranks. They are different controls, so this one is
// built out of an ItemsView and a query box rather than by widening the
// attachment.
//
// The sort is STABLE so an empty query leaves the list in its scanned
// (alphabetical) order rather than in whatever order equal scores happen
// to fall.
func (w *workspace) ranked(q string) []string {
	q = strings.TrimSpace(q)
	if q == "" {
		return w.files
	}
	type scored struct {
		path  string
		score int
	}
	out := make([]scored, 0, len(w.files))
	for _, f := range w.files {
		if ok, s, _ := fuzzyMatch(q, f); ok {
			out = append(out, scored{f, s})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	paths := make([]string, len(out))
	for i, s := range out {
		paths[i] = s.path
	}
	return paths
}

// browserItems is the bound list the Explorer pane's <ItemsView> shows.
//
// The rev Get is HOISTED to the top and above every branch: a dependency
// is recorded by the Get that actually RUNS, so a read placed after an
// early return drops out of the dependency set on the frames that take
// the return, and the list goes deaf with no error anywhere. There are
// two sources here — the scan revision and the query — and both are read
// before anything can bail.
func (ed *editor) browserItems() components.ItemSource {
	ed.wsRev.Get()
	q := ed.wsQuery.Get()
	if ed.ws == nil || ed.ws.fsys == nil {
		return components.ItemsOf([]string{}, fileRow)
	}
	return components.ItemsOf(ed.ws.ranked(q), fileRow)
}

// fileRow projects one path. The matched-rune positions are deliberately
// NOT projected: ItemsView does carry them (rowValue has a []int case,
// added for exactly this), but rendering them needs a custom highlight
// component in the row template, and markup has no way to spell one
// without registering a builder. The ranking is the substance and it is
// here; the highlight is a known gap, not an oversight.
func fileRow(p string) map[string]any {
	return map[string]any{
		"Name": shortPath(p, 30),
		"Path": p,
	}
}

// shortPath fits a path into w cells by dropping LEADING segments, not
// trailing characters.
//
// The first version showed path.Base alone, and a capture of the real
// editor is what showed it was wrong: a workspace holds
// `components/activitybar/activitybar.go` and `activitybar/activitybar_test.go`
// and half a dozen `main.go`, and a list of base names renders them as
// indistinguishable rows. The distinguishing part of a path is its TAIL,
// so that is the part that survives.
//
// Truncating from the right — which is what letting the cell buffer clip
// would do — keeps exactly the part every candidate shares.
func shortPath(p string, w int) string {
	if len([]rune(p)) <= w {
		return p
	}
	segs := strings.Split(p, "/")
	out := segs[len(segs)-1]
	for i := len(segs) - 2; i >= 0; i-- {
		next := segs[i] + "/" + out
		if len([]rune(next))+1 > w {
			return "…/" + out
		}
		out = next
	}
	return out
}

// openWorkspaceFile loads a document out of the workspace. It reads
// through fs.FS — the same seam markup.Load uses — and parses with the
// editor's own nodeOf, so what lands in the designer is the DOCUMENT
// MODEL and not a built component tree.
func (ed *editor) openWorkspaceFile(rel string) {
	if ed.ws == nil || ed.ws.fsys == nil {
		return
	}
	b, err := fs.ReadFile(ed.ws.fsys, rel)
	if err != nil {
		ed.status.Set("✗ " + rel + ": " + err.Error())
		return
	}
	n, err := nodeOf(string(b))
	if err != nil {
		ed.status.Set("✗ " + rel + ": " + err.Error())
		return
	}
	// nodeOf returns the OUTERMOST element, which for a saved document is
	// the <Gooey> envelope. The editor's document is what is inside it —
	// the surface Canvas holds one child and that child is the user's
	// root. Unwrapping here rather than in nodeOf keeps nodeOf usable for
	// the seed strings, which have no envelope.
	if n.Elem == "Gooey" {
		if len(n.Kids) != 1 {
			ed.status.Set("✗ " + rel + ": a <Gooey> document needs exactly one root element, found " + itoa(len(n.Kids)))
			return
		}
		n = n.Kids[0]
	}
	ed.root.Kids = []*node{n}
	ed.sel = n
	ed.openPath.Set(rel)
	ed.rebuild()
}

// saveOpenFile writes the document back.
//
// IT SERIALISES THE DOCUMENT MODEL, never the live tree, and that is the
// hazard this method exists to avoid rather than a stylistic preference.
// The component tree on screen is what markup.Build made of the document
// PLUS whatever the control plane patched into it — patch_markup replaces
// a named element's subtree in the running app, and the editor is itself
// patchable. Serialising the tree would write those patches into the
// user's file as if they had authored them. ed.doc().markup() walks the
// nodes the user edited and nothing else.
//
// Guarded by CanExecute on the command rather than by an error here: a
// Save that is not available should be visibly grey, not a keystroke that
// reports failure.
func (ed *editor) saveOpenFile() error {
	rel := ed.openPath.Get()
	if ed.ws == nil || ed.ws.dir == "" || rel == "" {
		return nil
	}
	src := "<Gooey>\n" + ed.doc().markup("  ") + "</Gooey>\n"
	full := filepath.Join(ed.ws.dir, filepath.FromSlash(rel))
	if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
		ed.status.Set("✗ save " + rel + ": " + err.Error())
		return err
	}
	ed.status.Set("✓ saved " + rel)
	return nil
}

// canSave is the command's condition, and it is a COMPUTED so the menu
// item dims and undims with no event anywhere: drawDropdown calls
// CanExecute while painting, which subscribes this to both handles.
func (ed *editor) canSave() bool {
	rel := ed.openPath.Get()
	return rel != "" && ed.ws != nil && ed.ws.dir != ""
}

// setWorkspace opens dir and republishes everything derived from it. The
// property writes are why this must stay on the UI goroutine; it is
// reached only from commands, which the dispatcher already runs there.
func (ed *editor) setWorkspace(dir string) {
	ws := openWorkspace(dir)
	ed.ws = ws
	if ws.err != "" {
		ed.status.Set("✗ " + ws.err)
	} else {
		ed.status.Set("✓ " + ws.label + " (" + itoa(len(ws.files)) + " files)")
		ed.recordRecent(ws.dir)
	}
	ed.wsLabel.Set(ws.label)
	ed.openPath.Set("")
	ed.wsQuery.Set("")
	ed.wsRev.Set(ed.wsRev.Get() + 1)
}

// refreshWorkspace re-walks the directory. Explicit, for the reason the
// file comment gives: there is no watcher, so this is the only thing that
// moves the snapshot forward.
func (ed *editor) refreshWorkspace() {
	if ed.ws == nil || ed.ws.fsys == nil {
		return
	}
	ed.ws.scan()
	ed.status.Set("✓ " + ed.ws.label + " (" + itoa(len(ed.ws.files)) + " files)")
	ed.wsRev.Set(ed.wsRev.Get() + 1)
}

// maxRecent is how many folders the File menu offers. The list is
// SESSION-ONLY and is written to no file anywhere.
//
// That is a decision, not a gap. Persisting it means inventing a place
// for editor state to live, and the two obvious places are both wrong:
// beside the user's .gooey files, where it would be picked up by the very
// workspace scan above and offered as a document to edit, or in a
// dotfile this project has never agreed on. Design state — which file is
// open, the dock layout, which panes are pinned — has no home in this
// editor today, and inventing one silently is how a format nobody chose
// becomes a format nobody can change.
const maxRecent = 5

func (ed *editor) recordRecent(dir string) {
	out := []string{dir}
	for _, d := range ed.recent {
		if d != dir && len(out) < maxRecent {
			out = append(out, d)
		}
	}
	ed.recent = out
}
