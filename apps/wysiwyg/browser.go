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
	"strconv"
	"strings"
	"unicode"

	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/render"
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

const (
	fuzzyRun      = 3 // per step of a consecutive run
	fuzzyBoundary = 8 // once, at the start of a run that begins a segment
	fuzzyGap      = 3 // per character skipped between hits
)

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
		w.err = "showing the first " + strconv.Itoa(maxWorkspaceFiles) + " files; narrow the folder"
	}
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
		"Name": shortPath(p, browserNameCols),
		"Path": p,
	}
}

// browserNameCols is the budget shortPath shortens a row's Name to, in
// CELLS. It was written as a bare 30 at the call above, which is the
// spelling that let shortPath measure in runes without the disagreement
// being visible from either end.
//
// IT IS THE DECLARED PANE, NOT THE LIVE ONE, and the first version of
// this comment called it "the explorer column's budget" as though it
// tracked the column. It does not: the number is fitted to
// <DockPane Id="explorer" … Size="34"> in wysiwyg.gooey less its chrome,
// and dockModel.Resize (dock.go) moves that width at run time. Drag the
// splitter narrower and the row is 30 columns inside a Text arranged in
// fewer, so the composer's clip takes the TAIL — which is the half
// shortPath exists to keep, and is #523's symptom reproduced with
// nothing but ASCII.
//
// Pre-existing: the bare 30 had it too, and shortening against the
// ARRANGED width means moving the call out of fileRow, which builds a
// row's values before anything is laid out. TRACKED AS [#528], not as
// this paragraph — a live defect whose only record is a comment in a
// file nobody opens unless they are already editing shortPath spends
// the attention that would have caught it, which is CLAUDE.md's "A red
// suite is yours" argument applied to prose. The issue carries the
// acceptance criterion: the fixing commit deletes this constant and this
// comment, because a budget that follows the pane has no constant to
// name. Raised in review of #524.
//
// [#528]: https://github.com/WonderForgeLabs/gooey/issues/528
const browserNameCols = 30

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
// Measured in COLUMNS. Every comparison here was a rune count, so a
// workspace holding `apps/世界/世界.gooey` reported a name that fits a
// budget it overruns by the number of wide glyphs in it — and the clip
// that then bounds it takes the TAIL, which is the exact part the
// paragraph above exists to keep. A file system supplies these names,
// so this is the one caller in the app that cannot choose its own
// characters.
func shortPath(p string, w int) string {
	if render.StringWidth(p) <= w {
		return p
	}
	segs := strings.Split(p, "/")
	out := segs[len(segs)-1]
	for i := len(segs) - 2; i >= 0; i-- {
		next := segs[i] + "/" + out
		// TWO COLUMNS RESERVED, because "…/" is what elide renders in
		// front of what this loop keeps. It reserved one, which was not a
		// bounds bug — elide bounds the result — but it produced a format
		// that alternated with the budget: `shortPath("aa/bbbb/cc", 9)`
		// gave `…/bbbb/cc` and `…bbbb/cc` at 8, and the second reads as
		// "a character was cut out of bbbb" when what actually went was
		// the whole leading `aa/`. Reserving two also makes this
		// condition and elide's first branch the SAME predicate — but
		// only from the SECOND iteration on, where `out` is the previous
		// `next` and has already passed this test.
		//
		// WHICH ARM THE FIRST ITERATION REACHES IS THE LAST SEGMENT'S
		// WIDTH, not this condition. `out` is then that segment straight
		// out of Split with nothing having measured it, so it takes
		// elide's "…/" arm whenever the segment fits in w-2 and the cut
		// arm only when it does not:
		//
		//	shortPath("aa/bbbb/cc", 7) = "…/cc"   first iteration, FIRST arm
		//	shortPath("aa/bbbb/cc", 3) = "…cc"    first iteration, CUT arm
		//
		// An earlier version of this comment said the first iteration and
		// the return below BOTH reach the cut arm. Only the second does
		// unconditionally — and that return is reached only when the path
		// holds no "/" at all: any separator makes this loop run, and its
		// last pass has `next == p`, which cannot satisfy this condition
		// because `render.StringWidth(p) > w` is already established
		// above. The cut arm is live either way, which is what the wide
		// half of TestShortPathStillKeepsTheTail exercises; it is not
		// dead code.
		//
		// AND THE FORMAT IT BUYS HAS A FLOOR. "…/" + the last segment is
		// what says leading SEGMENTS went, and it is the answer only
		// while that string fits in w. Below it there is no room for the
		// separator and the row degrades to elide's cut arm —
		// shortPath("aa/bbbb/cc", 3) = "…cc", the only answer that fits
		// three columns, and the one the format exists to avoid.
		// TestAShortenedPathSaysWhatItDropped runs down to that boundary
		// and asserts both sides of it. Found in review of #524, twice.
		if render.StringWidth(next)+2 > w {
			return elide(out, w)
		}
		out = next
	}
	return elide(out, w)
}

// elide answers in ONE OF THREE SHAPES, and which one is the difference
// between "there is more path above this", "this name itself was cut",
// and "nothing of it fits at all":
//
//	"…/" + s          when s fits in w-2 — the common path, and what
//	                  every ordinary row in the explorer gets
//	"…" + a tail of s when it does not, keeping AT MOST the last w-1
//	                  columns — the walk stops at a cluster boundary, so
//	                  it comes up short whenever no cluster begins exactly
//	                  there: elide("世世世世世", 6) = "…世世", five of six
//	"…" alone         when even the trailing CLUSTER is wider than w-1,
//	                  so no tail fits beside the ellipsis — and, below
//	                  w == 2, whatever ClipCols can lay of it
//
// The third is the one a caller is likeliest to be surprised by:
// elide("世", 2) answers "…", discarding a string that fits the budget.
// Reaching elide at all means leading segments were dropped, so the
// ellipsis is the part that has to survive; a lone glyph where a path
// was is a worse answer than a mark saying a path was cut. The reason
// used to be written only inside the body, where this doc's reader does
// not look.
//
// Clipping from the left would keep the leading characters of one long
// name instead, which is the answer this whole function rejects for a
// list of paths. The doc named one arm, then two, while the fix for that
// added a third in the same commit (#524's review, twice).
func elide(s string, w int) string {
	if render.StringWidth(s) <= w-2 {
		return "…/" + s
	}
	if w <= 1 {
		return render.ClipCols("…", w)
	}
	drop := render.StringWidth(s) - (w - 1)
	cut, room := 0, false
	// EachCluster stops where the callback says so, so cut lands on the
	// first cluster starting at or past drop. WHEN THERE IS NO SUCH
	// CLUSTER the walk runs to the end and leaves cut on the last one,
	// whose start column is below drop — and the result overran w by the
	// difference: elide("世", 2) answered "…世", three columns, for a
	// string that already fitted in two. `room` is the discriminator.
	render.EachCluster(s, func(_ string, off, col, _ int) bool {
		cut, room = off, col >= drop
		return !room
	})
	if !room {
		// The trailing cluster alone is wider than w-1, so nothing of s
		// fits beside the ellipsis. The ellipsis alone is the honest
		// answer and the only one inside the budget.
		return render.ClipCols("…", w)
	}
	return "…" + s[cut:]
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
	// COMPUTED HERE, ASSIGNED WHERE THE DOCUMENT IS REPLACED. A file
	// with no envelope must not inherit the last one's Graphics, so the
	// zero value has to reach ed.envAttrs — but clearing the field up
	// here did it on the REFUSED path too. The len(n.Kids) != 1 return
	// below leaves ed.root.Kids, ed.sel and ed.openPath pointing at the
	// document that is still open, and does not re-run ed.rebuild, so
	// clicking the wrong file in the browser stripped the open
	// document's Graphics and default xmlns while the CODE tab went on
	// showing them and the next save wrote a bare <Gooey>. The field
	// moves with ed.root.Kids now, and no partial path can separate the
	// two — TestEnvAttrsIsAssignedWhereTheDocumentIs checks that from the
	// AST rather than leaving it to three comments. Raised in review of
	// #501.
	//
	// A FILE WHOSE ROOT IS A DECLARATION IS REFUSED HERE, because nodeOf
	// deliberately lets a root-position x-namespaced element through for
	// the PASTE path (see there) and this is the one caller that would
	// then write it back. Measured before the guard: the file opened with
	// "unknown element <Property>", was wrapped in a <Gooey>, and ctrl+s
	// wrote `<Property Name="T" … xmlns:p="…"/>` — the prefix gone from
	// the author's file, under a build error. saveOpenFile is not gated
	// on the build and canSave gates on openPath, which the open had set.
	// Raised in review of #522.
	//
	// n.Elem != "Gooey" because the envelope is the envelope whatever
	// namespace it resolves in: a document whose DEFAULT xmlns is the x
	// namespace puts <Gooey> itself in it, and that file has an answer of
	// its own.
	//
	// THAT COVERS THE DEFAULT-xmlns SHAPE AND ONLY IT, which this
	// paragraph did not say. A root <x:Gooey> with unprefixed children
	// reaches neither refusal — splitDecls files the children as kids,
	// so alienDecls is silent — and the file opened green and was saved
	// back as plain <Gooey>. The answer is upstream now: nodeOf's
	// root-position exemption asks the element name, so these bytes
	// never reach a document at all. Raised in review of #522.
	//
	// WHICH IS THE ALIEN REFUSAL, NOT THE ROOT COUNT. This paragraph said
	// the root count, and the alien arm was added after it and returns
	// first: with the default xmlns on <Gooey>, splitDecls files every
	// child into decls, alienDecls fires, and the editor answers
	// "<x:Canvas> is an unknown language element; the
	// wonderforge.io/gooey/x namespace declares <x:Property> only" —
	// which is markup.Build's own sentence for the same bytes, the
	// standard this file holds its other refusals to. The behaviour was
	// right and the stated reason pointed at a branch that cannot fire,
	// in a file whose comments other arms reason from. Raised in review
	// of #522.
	//
	// AND THE ELEMENT NAME DECIDES WHICH REFUSAL, which this guard did
	// not ask: it tested only the namespace, so <x:Foo/> as a whole file
	// was called "a dependency property declaration, not a document" —
	// and the advice that follows is then actively wrong, because an
	// author who moves it under a <Gooey> root lands on the editor's own
	// alien refusal, which
	// TestAnXNamespacedElementThatIsNotPropertyGetsMarkupsOwnAnswer
	// pins. bareDeclWhy asks n.Elem != "Property" first for exactly this
	// reason; the open path is the one site added after alienDecls and
	// alienDeclMsg were written for it. Raised in review of #522.
	if n.Space == markup.XNamespace && n.Elem != "Gooey" {
		prefix, bound := declBinding(n.Attrs)
		if n.Elem != "Property" {
			// THE FILE-LEVEL TAIL IS ADDED HERE, not inside
			// alienDeclMsg, because that message is shared with the
			// envelope-CHILD call sites where "no document to show" is
			// simply false. Without it this arm said only "that is not
			// a language element", which reads as a fault to fix in
			// place — and the fix it prescribes, writing
			// <prefix:Property>, is refused three lines up by the arm
			// below as a whole file. That is the same
			// sending-them-at-a-wall-we-know-about this branch split
			// the alien case out to stop, one arm over and in the
			// other direction. Raised in review of #522.
			ed.status.Set("✗ " + rel + ": " +
				alienDeclMsg([]*node{n}, prefix, bound) +
				". A file whose whole content is one has no document to show")
			return
		}
		prefix = declBindingOr(n.Attrs, declFallbackPrefix)
		ed.status.Set("✗ " + rel + ": <" + prefix + ":" + n.Elem + "> is a " +
			"dependency property declaration, not a document. A declaration " +
			"belongs among the children of a <Gooey> root, where it defines " +
			"that control's public surface; a file whose whole content is one " +
			"has no document to show")
		return
	}
	// AND THE UNPREFIXED SPELLING, which is splitDecls' THIRD arm and the
	// one the whole partition exists to diagnose — the author who wrote
	// <Property> meaning <x:Property>. The guard above is keyed on the
	// namespace, so that file fell through it entirely: it opened with
	// "markup: unknown element <Property>", set ed.openPath, and ctrl+s
	// then wrote the editor's synthesised document — the declaration
	// wrapped in a <Gooey> — over the author's file.
	//
	// THE MESSAGE WAS ALSO ABOUT A DOCUMENT THE AUTHOR DID NOT WRITE.
	// markup.Build on those bytes answers "root element must be <Gooey>,
	// got <Property>"; "unknown element" only arises once the editor has
	// put the declaration under its surface, and splitDecls' own doc
	// names that string as the one the author must not be shown.
	//
	// The other three routes to these bytes were already covered — the
	// prefixed file by the arm above, a paste by bareDeclWhy's third arm,
	// and one inside a <Gooey> by splitDecls' bare arm — which is what
	// made this the odd path out rather than a gap in the idea. Raised in
	// review of #522.
	//
	// IT DESCRIBES RATHER THAN PRESCRIBES, which is what separates this
	// arm from bareDeclMsg and is why it no longer calls it.
	// bareDeclMsg is markup's sentence for a declaration found AMONG AN
	// ENVELOPE'S CHILDREN, where a <Gooey> root exists to carry the
	// xmlns:x its remedy names. Here there is none: an author who
	// followed it in order — write <x:Property>, add xmlns:x to the
	// <Gooey> root element — had no <Gooey> root to add it to, and on
	// the next open landed on the prefixed arm fifty lines up. That is
	// the same sending-them-at-a-wall-we-know-about this branch split
	// the alien case out to stop, a third time. The prefixed arm above
	// already describes where a declaration belongs instead of
	// prescribing an edit; this one now matches it, and keeps
	// bareDeclMsg's DIAGNOSIS — that <Property> is the spelling written
	// without its namespace — which is the half that was always true
	// here. Raised in review of #522.
	if n.Elem == "Property" {
		ed.status.Set("✗ " + rel + ": <Property> is a dependency property " +
			"declaration written without its namespace; the spelling is " +
			"<x:Property>, under xmlns:x=\"" + markup.XNamespace + "\". A " +
			"declaration belongs among the children of a <Gooey> root, where " +
			"it defines that control's public surface; a file whose whole " +
			"content is one has no document to show")
		return
	}
	var env map[string]string
	// nodeOf returns the OUTERMOST element, which for a saved document is
	// the <Gooey> envelope. The editor's document is what is inside it —
	// the surface Canvas holds one child and that child is the user's
	// root. Unwrapping here rather than in nodeOf keeps nodeOf usable for
	// the seed strings, which have no envelope.
	var decls []*node
	if n.Elem == "Gooey" {
		// DECLARATIONS FIRST, because they are not root elements and
		// counting them as such refused a well-formed document. markup
		// hands the whole <Gooey> to splitDeclarations
		// (markup/property.go), which partitions its children and only
		// then requires one visual kid; this editor counted n.Kids and
		// told the author a file markup loads has "2 root elements" —
		// advice whose only reading is to delete the declaration. #517.
		var kids, bare []*node
		decls, kids, bare = splitDecls(n)
		n.Kids = kids
		// THE MISNAMESPACED DECLARATION IS ITS OWN FAULT, and it is
		// reported before the count because the count cannot see it.
		// An unprefixed <Property> is a declaration the author forgot
		// to namespace, and neither route reached markup's advice: with
		// a content root beside it the editor called it a second root,
		// and alone it was unwrapped and built inside the surface,
		// where markup answers "unknown element <Property>". Both were
		// measured. Raised in review of #522.
		if len(bare) > 0 {
			ed.status.Set("✗ " + rel + ": " + bareDeclMsg(len(bare)))
			return
		}
		// AND AN x-NAMESPACED ELEMENT THAT IS NOT Property IS ITS OWN
		// FAULT TOO. splitDecls keys on the namespace, so <x:Foo> lands
		// in decls, and the count message below then named it
		// <x:Property> — an element this file does not contain — and
		// called a thing markup rejects outright a declaration. markup's
		// own answer is "unknown language element", and it is the one
		// the author can act on. Raised in review of #522.
		if alien := alienDecls(decls); len(alien) > 0 {
			// THE BINDING TRAVELS WITH THE PREFIX. Discarding the bool
			// named declBinding's minted spelling in a file that
			// contains no such prefix — the same defect the count
			// branch below states at length, which this arm had
			// stopped one short of. Raised in review of #522.
			prefix, bound := declBinding(n.Attrs)
			ed.status.Set("✗ " + rel + ": " + alienDeclMsg(alien, prefix, bound))
			return
		}
		if len(n.Kids) != 1 {
			// NAMING THE DECLARATIONS SEPARATELY, so the count the
			// author is given is the one they can act on. "found 2" for
			// a root and a declaration sent them looking for a second
			// root that was never there.
			msg := "✗ " + rel + ": a <Gooey> document needs exactly one root element, found " +
				strconv.Itoa(len(n.Kids))
			if len(decls) > 0 {
				// THE AUTHOR'S OWN PREFIX, not "x:". The message exists
				// to tell them which of their elements is being counted
				// separately, and #522 is the change that made a p:
				// document round-trip as p: — so naming x: here sends
				// the reader looking for elements their file does not
				// contain. n.Attrs is in hand, which is where the
				// binding lives. Raised in review of #522.
				//
				// THE WHOLE TAIL AGREES, not just the verb. It read
				// "its 1 <p:Property> declaration is not root elements"
				// — the noun carried the verb and the trailing literal
				// stayed plural. Nothing went red over it because
				// nothing asserted this branch at all; the tests here
				// now do.
				//
				// AND AN UNBOUND PREFIX IS NOT WRITTEN. declBinding
				// falls back to "x" when the envelope binds nothing,
				// which is exactly the document whose declaration is
				// named by its own default xmlns — a file containing no
				// x: anywhere. Naming <x:Property> there sends the
				// author looking for an element they never wrote.
				// Raised in review of #522.
				//
				// EVERY ONE OF THEM IS SPELLED FROM ITS OWN BINDING,
				// through declElemName, because a document may bind
				// more than one prefix to the x namespace and XML
				// scoping puts the binding wherever the author wrote
				// it. This read decls[0] and printed that spelling with
				// len(decls), so a file holding one <p:Property> and
				// one <q:Property> was told it held "2 <p:Property>
				// declarations" — a count of elements it does not
				// contain, in the branch whose own comment claimed to
				// be about one element. The rounds that got here are in
				// docs/specs/2026-08-10-markup-declared-properties.md.
				// Raised in review of #522.
				elems := make([]string, len(decls))
				same := true
				for i, d := range decls {
					elems[i] = declElemName(d, n.Attrs)
					same = same && elems[i] == elems[0]
				}
				noun := " declarations are not root elements"
				if len(decls) == 1 {
					noun = " declaration is not a root element"
				}
				if same {
					msg += " (its " + strconv.Itoa(len(decls)) + " " + elems[0] + noun + ")"
				} else {
					// THE COUNT STAYS, and the list is what it counted.
					// Dropping the prefix on disagreement was the other
					// candidate and names <Property>, which bareDeclMsg
					// defines as the missing-namespace typo.
					msg += " (its " + strconv.Itoa(len(decls)) + " declarations — " +
						strings.Join(elems, ", ") + " — are not root elements)"
				}
			}
			ed.status.Set(msg)
			return
		}
		// THE ENVELOPE'S NAMESPACE DECLARATIONS COME DOWN WITH IT, and
		// the rule for doing that is carryDeclarations (main.go) rather
		// than a loop here: paste unwraps an envelope too, and this was
		// the only one of the two that carried anything.
		moved := carryDeclarations(n, n.Kids[0])
		// AND EVERYTHING ELSE ON THE ENVELOPE STAYS ON THE ENVELOPE.
		// Only the prefixed declarations move down; a plain xmlns and a
		// Graphics both belong where the author wrote them, and
		// markup.parse skips a plain xmlns outright, so moving it bought
		// nothing but a diff on the first save of every existing file.
		env = envelopeAttrs(n, moved)
		n = n.Kids[0]
	}
	ed.root.Kids = []*node{n}
	ed.envAttrs = env
	ed.envDecls = decls
	ed.sel = n
	ed.openPath.Set(rel)
	// A NEW DOCUMENT STARTS WITH NO PAST. Without this the previous
	// file's snapshots stay in the stack and ctrl+z restores ITS tree
	// under THIS file's envelope, with openPath still naming this file —
	// see history.reset for the measurement. Raised in review of #501.
	// selPath is read AFTER ed.sel is set above: the baseline has to name
	// the selection the open just made, or the first undo drops it.
	sel, hasSel := ed.selPath()
	ed.history().reset(ed.root, sel, hasSel)
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
	src := envelopeHead(ed.envAttrs, ed.envDecls) + ed.doc().markup("  ") + "</Gooey>\n"
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
		ed.status.Set("✓ " + ws.label + " (" + strconv.Itoa(len(ws.files)) + " files)")
	}
	ed.wsLabel.Set(ws.label)
	// NO envAttrs CLEAR HERE. openWorkspaceFile is the only site that
	// assigns ed.root.Kids and it assigns ed.envAttrs beside it on the
	// same path, nil included — so clearing the field here separated the
	// two, which is the invariant that fix established. The document
	// stays on the canvas across a folder change, and the next rebuild
	// wrote a bare <Gooey> for a document nobody had edited.
	// TestEnvAttrsIsAssignedWhereTheDocumentIs is the guard; this
	// sentence is the reason. Raised in review of #501.
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
	ed.status.Set("✓ " + ed.ws.label + " (" + strconv.Itoa(len(ed.ws.files)) + " files)")
	ed.wsRev.Set(ed.wsRev.Get() + 1)
}

// There is deliberately no recent-folders list here.
//
// One was written — a capped, session-only ring fed from setWorkspace —
// and removed in review, because nothing read it: no File menu item and
// no other surface consumed it, so it was state the editor maintained
// and could not show. Its comment claimed it was "how many folders the
// File menu offers", which sent a reader looking for a menu that has
// never existed.
//
// The reason it stayed unfinished is worth keeping, because it is the
// part that has not changed. Offering recent folders means persisting
// them, and the two obvious homes are both wrong: beside the user's
// .gooey files, where the workspace scan above would pick the file up and
// offer it as a document to edit, or in a dotfile this project has never
// agreed on. Design state — which file is open, the dock layout, which
// panes are pinned — has no home in this editor today, and inventing one
// silently is how a format nobody chose becomes a format nobody can
// change. Decide that first; the list is a few lines once it is decided.
