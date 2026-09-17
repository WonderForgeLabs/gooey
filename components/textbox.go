package components

import (
	"strings"
	"unicode"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// TextBox is a single-line editor: a focus stop that owns printable
// runes and the editing keys while focused. Text is a shared property
// handle, so the viewmodel and the component edit the same value — the
// same two-way arrangement Checkbox uses for its bool.
//
// Promoted from cmd/finder's query line, which drove editing from the
// app's main loop. The framework version does its own key handling and
// adds a cursor: the caret is a source property, so moving it is
// ordinary paint damage and repaints only this component.
//
// Editing is mid-string, not append-only. The caret moves by character,
// by word (ctrl+arrow) and to either end; shift extends a selection from
// an anchor; typing, backspace and delete apply to the selection when
// there is one. Cut, copy and paste use a process-local kill buffer
// shared by every TextBox in the app — see KillBuffer, and note that
// this is NOT the system clipboard.
//
// The mouse places the caret, drags to select, and selects a word on
// double click. The drag is the pointer-capture machinery's first
// consumer: a press captures, so motion past the field's own bounds
// still arrives here and the selection keeps tracking.
//
// Changed, if set, runs after every edit. It exists because an edit
// usually invalidates something derived — finder resets its selection to
// the top whenever the query changes — and a command is a cheaper way to
// say that than making the caller watch the text property.
type TextBox struct {
	gooey.Base
	gooey.FocusState
	gooey.HoverState
	Text        *prop.Property[string]
	Prompt      *prop.Property[string]       // optional prefix, e.g. "> "
	Style       *prop.Property[render.Style] // the edited text
	AccentStyle *prop.Property[render.Style] // prompt and caret
	Changed     gooey.Action

	// Error is the field's validation state: empty means valid, anything
	// else flips the text into the invalid visual (InvalidStyle, or red +
	// underline) — the MAUI ValidationBehavior arrangement, a style flip
	// on the input itself. Typically a validate.Field computed shared
	// with the error Text (and optionally a ValidationMarker), but any
	// string property works. Render reads it, so an error flip is
	// ordinary paint damage on this one component — and this read is
	// exactly where a future style system's :invalid pseudo-class would
	// look (see the styles-and-resources spec).
	Error        *prop.Property[string]
	InvalidStyle *prop.Property[render.Style] // replaces the default invalid visual

	caret  *prop.Property[int]
	anchor *prop.Property[int] // selection anchor; noAnchor when there is none

	// scroll is the first visible rune index. It is derived state, not a
	// property: Render recomputes it from the caret and the width it is
	// already reading, so anything that can move it has dirtied this
	// paint node anyway and a property here would only add a second way
	// to say the same thing.
	scroll int
}

// noAnchor is the anchor value meaning "no selection". A real anchor is
// an index into the text, which is never negative.
const noAnchor = -1

func (t *TextBox) value() []rune { return []rune(getStr(t.Text)) }

func (t *TextBox) caretProp() *prop.Property[int] {
	if t.caret == nil {
		t.caret = prop.NewSource(0)
	}
	return t.caret
}

func (t *TextBox) anchorProp() *prop.Property[int] {
	if t.anchor == nil {
		t.anchor = prop.NewSource(noAnchor)
	}
	return t.anchor
}

// Caret is the insertion index, clamped on read: the bound text can
// change underneath the component (a viewmodel reset, a hot reload) and the
// caret must never point past the end.
func (t *TextBox) Caret() int { return clamp(t.caretProp().Get(), 0, len(t.value())) }

// SetCaret moves the insertion point and drops any selection.
func (t *TextBox) SetCaret(i int) {
	t.setCaret(i)
	t.clearSelection()
}

// setCaret and setAnchor compare before they Set. prop.Set does not —
// it invalidates every dependent unconditionally — so an unguarded write
// would repaint this component on every keystroke that did not actually
// move anything, left arrow at column zero included.
func (t *TextBox) setCaret(i int) { setInt(t.caretProp(), clamp(i, 0, len(t.value()))) }

func (t *TextBox) setAnchor(i int) { setInt(t.anchorProp(), i) }

func setInt(p *prop.Property[int], v int) {
	if p.Get() != v {
		p.Set(v)
	}
}

// Selection is the selected range as [lo, hi), and whether there is one.
// An anchor equal to the caret is not a selection — that is what a plain
// click leaves behind, ready for a shift+arrow to extend from.
func (t *TextBox) Selection() (lo, hi int, ok bool) {
	a := t.anchorProp().Get()
	if a == noAnchor {
		return 0, 0, false
	}
	n := len(t.value())
	a, c := clamp(a, 0, n), t.Caret()
	if a == c {
		return 0, 0, false
	}
	if a > c {
		a, c = c, a
	}
	return a, c, true
}

func (t *TextBox) clearSelection() { t.setAnchor(noAnchor) }

// anchorHere starts a selection at the caret if one is not already
// running. Every shift+movement calls it before moving, which is what
// makes the first shifted key define the anchor and the rest extend it.
func (t *TextBox) anchorHere() {
	if t.anchorProp().Get() == noAnchor {
		t.setAnchor(t.Caret())
	}
}

func (t *TextBox) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: avail.W, H: min(1, avail.H)}
}

func (t *TextBox) Render(f *gooey.Frame) {
	// Read the error before any early return: the read is what
	// subscribes this paint node, and a Get hidden behind a bounds check
	// would silently drop the dependency (the Get-order rule).
	errMsg := getStr(t.Error)
	b := t.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	accent := getSty(t.AccentStyle)
	prompt := clipCols(getStr(t.Prompt), b.W)
	runes := t.value()
	caret := t.Caret()
	lo, hi, selected := t.Selection()

	x := b.X
	if prompt != "" {
		f.Cells.SetString(x, b.Y, prompt, accent)
		x += render.StringWidth(prompt)
	}
	avail := b.X + b.W - x
	if avail <= 0 {
		return
	}
	// Scroll horizontally so the caret stays visible in a field narrower
	// than its content — on both sides, which is what mid-string editing
	// needs: walking left off the window has to pull it back, not just
	// walking right off the end.
	t.scroll = scrollFor(runes, t.scroll, caret, avail)

	textSty := getSty(t.Style)
	if errMsg != "" {
		// The terminal's error convention: red, underlined text. An
		// InvalidStyle handle replaces it wholesale for apps with their
		// own palette — the Style/AccentStyle pattern, one more knob.
		if t.InvalidStyle != nil {
			textSty = t.InvalidStyle.Get()
		} else {
			textSty.Fg = errorRed
			textSty.Underline = true
		}
	}
	// ONE GRAPHEME CLUSTER, ITS OWN COLUMNS. `x++` was the whole of
	// #519: a wide glyph occupies two columns and SetCell places both,
	// so advancing one put the next rune on the continuation cell the
	// previous glyph had just claimed — healSeam then blanked the
	// orphaned lead and the character vanished. "世界" rendered as
	// " 界", and with the caret at the end as "  █": a TextBox silently
	// deleted every wide glyph in its own value. The stop is on the
	// glyph's FULL width too, for the same reason SetCell answers a
	// half-glyph at the clip edge with a space — a lead written without
	// room for its tail displaces the rest of the row.
	//
	// AND THE UNIT IS THE CLUSTER, NOT THE RUNE, which the first fix for
	// #519 got wrong in a way that was worse than the bug. It walked
	// runes and folded the zero-width ones into the cell in front
	// afterwards — but a cluster's width is NOT the sum of its runes'
	// widths, so folding could WIDEN the lead: StringWidth("⚠️") is 2
	// where StringWidth("⚠") is 1. x had already advanced by 1, the
	// fold's SetCell wrote a Continuation into the column the next rune
	// was about to take, healSeam blanked the orphan, and "⚠️x" painted
	// as " x" — #519's own symptom, reintroduced by its fix, for every
	// VS16 emoji a user can type. The same arithmetic put a ZWJ family
	// in seven columns here and three in a Text, on the same frame,
	// which render.Displaced cannot see because each cell is
	// individually consistent.
	//
	// render.EachCluster hands over the cluster AND its width from one
	// segmentation, which is what SetString, ClipCols and StringWidth
	// all use and what makes this loop agree with them. It is walked
	// from t.scroll rather than from 0, and stops at the field's right
	// edge, so the SEGMENTATION is proportional to the field.
	//
	// THE SLICE IS NOT, and the sentence this replaces claimed the cost
	// stays proportional to the field full stop. `string(runes[t.scroll:])`
	// copies the whole tail on every paint — measured in review of #521
	// at 0.93 ms per call on a 100,000-rune value, out of a 1.14 ms
	// compose, so it is the dominant term of that frame. Nothing like
	// the 1.4 s this PR removed, and stated rather than fixed because
	// the bound cannot be computed cheaply AND correctly: deciding how
	// many runes can fill `avail` columns needs a rune-width walk, and a
	// rune sum is neither an upper nor a lower bound on its clusters' —
	// render.StringWidth("⚠️") is 2 against a rune sum of 1, and a
	// four-person ZWJ family is 2 against a rune sum of 8. Guessing
	// short drops glyphs off the right of the field, which is #519
	// again. The next reader should not rely on a bound that is not
	// there.
	//
	// A window that starts in the middle of a cluster would re-segment
	// from there and split it, and that is not only cosmetic: indexAt
	// segments from the START of the value, so the two would disagree
	// about which character is in the first column and a click there
	// would answer with a rune off-screen to the left. scrollFor no
	// longer produces such a start — every clamp in it answers with a
	// cluster boundary, which
	// TestTheScrollWindowAlwaysOpensOnAClusterBoundary pins over a grid
	// rather than a fixture. Setting t.scroll by hand still can. Raised
	// in the review of #521, which found the click half of it.
	idx := t.scroll
	render.EachCluster(string(runes[t.scroll:]), func(cluster string, _, _, w int) bool {
		i := idx
		n := len([]rune(cluster))
		idx += n
		if w == 0 {
			// A zero-width cluster owns no column. It can only be a mark
			// with nothing in front of it to decorate — scrollFor snaps
			// the window back over those, so reaching one here means the
			// value itself opens with one. Skipping it is right, and the
			// caret cannot be lost with it: the caret arm below fires on
			// the cluster CONTAINING it, and a caret inside this one has
			// no cell to reverse either way.
			return true
		}
		if x+w > b.X+b.W {
			return false
		}
		st := textSty
		switch {
		case selected && i < hi && i+n > lo:
			// OVERLAP, the same containment test the caret arm below
			// makes, and for the same reason. Testing the cluster's
			// FIRST rune left a selection that covers only a combining
			// mark showing nothing at all — and `selected` suppresses
			// the caret arm, so the field displayed neither: measured on
			// decomposed "éx" with the selection [1,2), reversed cells
			// none, against the ASCII control "ex" reversing its x.
			// Half a cluster is not a thing a cell can show; reversing
			// the whole glyph is the only answer it has. Raised in
			// review of #521.
			st.Reverse = true
		case !selected && t.IsFocused() && caret >= i && caret < i+n:
			// THE WHOLE CLUSTER, and caret >= i rather than caret == i.
			// The caret sits ON the character it precedes, and a caret
			// that has been moved into the middle of a cluster — an
			// arrow key steps by rune — still belongs to the one glyph
			// on screen. Reversing the cluster is the only answer a
			// single cell can give; testing caret == i left the caret
			// invisible for one keypress per combining mark in the
			// value. Found in the review of #521.
			st.Reverse = true
		}
		c := render.Cell{Rune: []rune(cluster)[0], Style: st}
		if n > 1 {
			c.Cluster = cluster
		}
		f.Cells.SetCell(x, b.Y, c)
		x += w
		return true
	})
	if t.IsFocused() && !selected && caret >= len(runes) && x < b.X+b.W {
		f.Cells.Set(x, b.Y, '█', accent)
	}
}

// scrollFor keeps caret inside a window of avail CELLS over runes,
// moving the window as little as possible. The caret may sit one past
// the last rune, so the window has to be able to show len(runes) as a
// position, and the caret itself owns the columns of the glyph it is on.
//
// IT TOOK A RUNE COUNT UNTIL #519, and the two agree exactly while every
// rune is one column wide — which every fixture in this package was.
// Over wide glyphs they do not: a window of `avail` runes is up to twice
// `avail` columns, so a field of CJK scrolled by half a field and the
// caret left the window it exists to stay inside.
//
// IT WAS ALSO O(n²) UNTIL #521's REVIEW, which is the reason the two
// conditions are gone rather than merely rewritten. Written directly —
//
//	for cur < caret && colsBetween(runes, cur, caret)+1 > avail { cur++ }
//
// — each step re-sums a tail that shrinks by one rune, so a caret at the
// end of a 10,000-rune value cost 1.4 s MEASURED, on the paint path,
// inside a prop.NewComputed on the UI goroutine: a hard freeze on End,
// on a click, on a paste, or on the first render of a prefilled field.
// windowFloor answers the same question by walking LEFT from the end of
// the span, so the cost is O(avail) — the columns that fit — rather than
// O(len). The window is the same one; only the arithmetic moved.
func scrollFor(runes []rune, cur, caret, avail int) int {
	if avail <= 0 {
		return 0
	}
	if caret < cur {
		cur = caret
	}
	if cur < 0 {
		cur = 0
	}
	// ON A CLUSTER BOUNDARY. The line above is the only one here that
	// can leave `cur` inside a cluster — an arrow key steps by rune, so
	// a caret walked backwards into the middle of one drags the window
	// in with it. A window that opens there has no lead for Render to
	// draw, so Render skips the fragment AND the caret arm with it,
	// leaving a focused field with no caret anywhere, which is what
	// TestTheCaretSurvivesAWindowThatOpensOnACombiningMark pins.
	//
	// The two clamps below cannot reintroduce that: windowFloor answers
	// with the leftmost fitting BOUNDARY, which is the property
	// TestTheWindowFloorIsTheLeftmostFittingClusterBoundary pins
	// directly. So the snap's POSITION in this function is not
	// load-bearing and the comment here used to claim it was — measured
	// in review of #521 by moving it to the end and diffing scrollFor
	// over a grid of emoji, family, CJK and decomposed values: not one
	// input changed answer. It stays first because that is where the
	// value it repairs is produced.
	cur = clusterStartAt(runes, cur)
	// Right far enough that the caret's own columns fit.
	//
	// THE SPAN ENDS AT THE CARET'S CLUSTER, NOT AT THE CARET. A caret
	// moved into the middle of one — an arrow key steps by rune — would
	// otherwise be counted twice: `reserve` is the whole cluster's
	// width, and the span runes[:caret] still holds the front of that
	// same cluster. On a four-person family in three columns the double
	// count made the window fit nothing and the floor came back as the
	// caret's own index, opening the window inside the family. Measured
	// in review of #521.
	if floor := windowFloor(runes, clusterStartAt(runes, caret), caretCols(runes, caret), avail); cur < floor {
		cur = floor
	}
	// And no further: show as much of the tail as the window holds. This
	// cannot strand the caret: caret <= len(runes), so a start that keeps
	// the whole tail plus a column inside `avail` keeps the caret's own
	// glyph inside it too.
	if tail := windowFloor(runes, len(runes), 1, avail); cur > tail {
		cur = tail
	}
	return cur
}

// clusterSlack is how many runes before a rune-width estimate the
// cluster walks below re-synchronise from.
//
// A GRAPHEME BOUNDARY IS LOCAL, and segmentation started mid-cluster is
// correct from the NEXT boundary on — only the first cluster it reports
// is a fragment. So a walk that begins clusterSlack runes early and
// ignores its first cluster reads true boundaries from there, without
// segmenting the value from index 0, which is the O(len) cost on the
// paint path that #521's review measured at 1.4 s.
//
// It is also windowFloor's first step when it has to expand leftwards,
// doubling from there — so it is a starting guess in both places, never
// a cap on how far either will look.
//
// 64 is generous against what one cluster can be: the widest this
// vocabulary has is a four-person ZWJ family, seven runes. The residual
// is a SINGLE cluster longer than this, where the re-synchronising walk
// can still begin inside it and report that fragment's start as the
// cluster's — cosmetic and bounded.
//
// THAT IS NOT THE WHOLE RESIDUAL, AND THE MISSING HALF IS NOT ABOUT
// LENGTH. A regional-indicator boundary is decided by the PARITY of the
// whole run in front of it (UAX #29 GB12/GB13), not by anything local,
// so a segmenter restarted mid-run inherits the wrong parity however
// generous the lookback is. Measured before the fix below, on a value of
// 40 US flags: with the caret at rune 65 the walk reported the cluster
// starting at 65 where it starts at 64, the field painted "🇸🇺🇸🇺🇸🇺" — a
// flag sequence the value does not contain, the halves re-paired — and a
// click on column 0 answered rune 64. eachClusterFrom now walks back off
// a regional-indicator run before it starts, which is the one category
// where the premise above fails. Raised in review of #521.
const clusterSlack = 64

// regionalIndicator reports the code points whose cluster boundaries are
// decided by a PARITY rather than by their neighbours: a pair of them is
// one flag, so whether rune k joins the one before it depends on how
// many regional indicators precede it, all the way back to the start of
// the run. It is the one thing a fixed lookback cannot re-synchronise
// on, which is why eachClusterFrom treats it specially. Raised in review
// of #521.
func regionalIndicator(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

// eachClusterFrom walks the clusters of runes[from:to], calling fn with
// each cluster's start index, rune count and COLUMN width.
//
// It re-synchronises: segmentation starts clusterSlack runes before
// `from` when there is room, and the fragment that produces is skipped.
// fn returning false stops the walk.
func eachClusterFrom(runes []rune, from, to int, fn func(at, n, w int) bool) {
	if from < 0 {
		from = 0
	}
	if to > len(runes) {
		to = len(runes)
	}
	if from >= to {
		return
	}
	lo := from - clusterSlack
	if lo < 0 {
		lo = 0
	}
	// AND OFF A REGIONAL-INDICATOR RUN, which is the one boundary a
	// fixed lookback cannot re-synchronise on: start inside a run of
	// flags and every pair from there is offset by one. Walking to the
	// run's own start restores the parity. Only when `lo` landed IN a
	// run — landing just after one is already a true boundary — so this
	// costs nothing on the values that have no flags in them, which is
	// nearly all of them. Raised in review of #521.
	if lo > 0 && regionalIndicator(runes[lo]) {
		for lo > 0 && regionalIndicator(runes[lo-1]) {
			lo--
		}
	}
	idx := lo
	first := true
	render.EachCluster(string(runes[lo:to]), func(cluster string, _, _, w int) bool {
		n := len([]rune(cluster))
		at := idx
		idx += n
		// The fragment, and anything still left of the span asked for.
		if (first && lo > 0) || at+n <= from {
			first = false
			return true
		}
		first = false
		return fn(at, n, w)
	})
}

// clusterStartAt is the index the cluster containing i begins at.
func clusterStartAt(runes []rune, i int) int {
	if i <= 0 || i >= len(runes) {
		return i
	}
	at := i
	eachClusterFrom(runes, i, len(runes), func(a, n, _ int) bool {
		if i >= a && i < a+n {
			at = a
			return false
		}
		return true
	})
	return at
}

// caretCols is how many columns the caret needs at index i.
//
// ON a rune it is drawn by REVERSING the CLUSTER it is in (Render,
// above), so a caret on a wide glyph needs all of its columns —
// reserving one let Render's stop, which breaks on the glyph's full
// width, drop the glyph the caret was riding, and the user typed at a
// position with no visible caret at all. Past the last rune it is a
// block of its own, one column.
//
// THE CLUSTER, NOT THE RUNE, and the difference is not only CJK. This
// reserved render.RuneWidth(runes[i]), which is 1 for the lead of "⚠️"
// — VS16 is zero-width — while Render reverses the whole cluster and
// stops on its full width of 2. Measured: `x⚠️` with the caret on the
// emoji in two columns drew "x " and reversed nothing, the identical
// configuration TestTheCaretIsVisibleOnAWideGlyphAtTheWindowsEdge pins
// one Unicode category over. Raised in review of #521.
//
// The floor of one is for a caret inside a zero-width cluster: a
// combining mark with nothing in front of it is drawn into no column, so
// the caret takes the column after it rather than none.
func caretCols(runes []rune, i int) int {
	if i < 0 || i >= len(runes) {
		return 1
	}
	got := 1
	eachClusterFrom(runes, i, len(runes), func(at, n, w int) bool {
		if i >= at && i < at+n {
			if w > 1 {
				got = w
			}
			return false
		}
		return true
	})
	return got
}

// windowFloor is the leftmost index a window of avail columns can start
// at and still show runes[:end] with reserve columns to spare.
//
// A GUESS AND A CORRECTION, BECAUSE A RUNE SUM IS NOT A CLUSTER SUM IN
// EITHER DIRECTION. The first pass walks left from end summing rune
// widths, which is O(avail) and exact for the one-rune clusters almost
// every value is made of. The second segments that span into clusters,
// expands it left until it overflows the window, and then drops leading
// clusters until it fits.
//
// The review that asked for this offered the rune walk as a valid lower
// BOUND — rune widths "under-count a cluster and never over-count it" —
// and that is measured false: render.StringWidth("⚠️") is 2 against a
// rune sum of 1, and a four-person ZWJ family is 2 against a rune sum of
// 8. The guess can be wrong in BOTH directions, which is why the
// correction expands as well as drops. Raised in review of #521.
//
// The result is the leftmost fitting cluster boundary, exactly — not an
// approximation of one. TestTheWindowFloorIsTheLeftmostFittingClusterBoundary
// checks it against the O(len) walk that says so directly, over the
// vocabularies where runes, rune-width sums and columns all disagree.
func windowFloor(runes []rune, end, reserve, avail int) int {
	if end > len(runes) {
		end = len(runes)
	}
	if avail <= 0 || end <= 0 {
		return 0
	}
	// A CANDIDATE, NOT A BOUND. Summing rune widths left from end is
	// O(avail) and exact while every cluster is one rune, which is the
	// overwhelmingly common case — but a rune sum is neither an upper
	// nor a lower bound on its clusters' widths, so it can land either
	// side of the answer: render.StringWidth("⚠️") is 2 against a rune
	// sum of 1, and a four-person family is 2 against a rune sum of 8.
	// The cluster pass below is what makes the answer true, and it
	// expands leftwards until it can prove it has gone far enough.
	i := end
	w := reserve
	for i > 0 {
		rw := render.RuneWidth(runes[i-1])
		if w+rw > avail {
			break
		}
		w += rw
		i--
	}

	// Now in clusters, which is the unit Render advances by. Collect the
	// span, then drop whole clusters off the LEFT until it fits.
	//
	// Dropping lands the answer on a cluster BOUNDARY, and that is not a
	// bonus. A window opened inside a cluster makes Render re-segment
	// the fragment into a glyph the value does not contain, while
	// indexAt still segments from 0 — so the paint and the click stop
	// agreeing about what is in the first column.
	type seg struct{ at, n, w int }
	var segs []seg
	total := 0
	collect := func(from int) {
		segs = segs[:0]
		total = reserve
		eachClusterFrom(runes, from, end, func(at, n, cw int) bool {
			segs = append(segs, seg{at, n, cw})
			total += cw
			return true
		})
	}
	collect(i)
	// EXPAND LEFT UNTIL THE SPAN OVERFLOWS THE WINDOW, because only an
	// overflowing span is evidence that the answer is inside it. A span
	// that still fits proves only that the guess was too far right, and
	// the drop loop below moves one way. Three four-person families are
	// six columns and TWENTY-ONE runes: in a field of eight the rune
	// walk said the window starts thirteen runes in, and the first
	// family — which fitted — was scrolled off the left. Measured in
	// review of #521.
	//
	// Doubling keeps the total work proportional to the runes the window
	// ENDS UP SHOWING rather than to the value: each retry at most
	// doubles the span, and the walk stops the first time it overflows.
	// That is the honest form of the O(avail) claim this function was
	// written for — avail COLUMNS may be arbitrarily many runes, and
	// nothing cheaper can know how many.
	//
	// The doubling is a COST property and nothing here can see it: no
	// vocabulary in this repo's tests needs a second retry, so a fixed
	// step gives every one of them the same answer. It is the bound
	// against a single cluster thousands of runes long, where a fixed
	// step would re-walk the span once per 64 runes.
	for back := clusterSlack; total <= avail && i > 0; back *= 2 {
		i -= back
		if i < 0 {
			i = 0
		}
		collect(i)
	}
	// The loop below is what lands the answer: it leaves i at a cluster
	// START every time it runs, and the expansion above guarantees it
	// runs unless the span already reaches rune 0.
	for k := 0; k < len(segs) && total > avail; k++ {
		total -= segs[k].w
		i = segs[k].at + segs[k].n
	}
	return i
}

// HandleKey owns text editing while focused. Keys it does not use bubble
// on, so page gestures (enter to accept, esc to quit) keep working from
// inside the field.
func (t *TextBox) HandleKey(ev input.KeyEvent) bool {
	if t.Text == nil {
		return false
	}
	if t.moveKey(ev) {
		return true // a caret move is not an edit
	}
	if !t.editKey(ev) {
		return false
	}
	if gooey.CanExecute(t.Changed) {
		t.Changed.Run()
	}
	return true
}

// moveKey handles everything that only moves the caret or the selection.
// Shift extends from the anchor, ctrl moves by word, and the two compose.
func (t *TextBox) moveKey(ev input.KeyEvent) bool {
	// Anything carrying a modifier the box does not use — alt+left, say —
	// is somebody else's gesture and must keep bubbling.
	if ev.Mods&^(input.ModShift|input.ModCtrl) != 0 {
		return false
	}
	runes := t.value()
	caret := t.Caret()
	shift := ev.Has(input.ModShift)
	word := ev.Has(input.ModCtrl)

	var to int
	switch ev.Key {
	case input.KeyLeft:
		if word {
			to = wordLeft(runes, caret)
		} else if lo, _, ok := t.Selection(); ok && !shift {
			to = lo // an unshifted arrow collapses a selection to its edge
		} else {
			to = caret - 1
		}
	case input.KeyRight:
		if word {
			to = wordRight(runes, caret)
		} else if _, hi, ok := t.Selection(); ok && !shift {
			to = hi
		} else {
			to = caret + 1
		}
	case input.KeyHome:
		to = 0
	case input.KeyEnd:
		to = len(runes)
	default:
		return false
	}
	if shift {
		t.anchorHere()
		t.setCaret(to)
	} else {
		t.SetCaret(to)
	}
	return true
}

// editKey handles everything that changes the text. Each branch that
// touches a selection deletes it first, so "typing replaces the
// selection" is one rule rather than four.
func (t *TextBox) editKey(ev input.KeyEvent) bool {
	switch {
	case ev.Key == input.KeyRune && ev.Mods == 0:
		t.replace(ev.Rune)
	case ev == input.Named(input.KeyBackspace):
		if _, _, ok := t.Selection(); ok {
			t.deleteSelection()
			break
		}
		caret := t.Caret()
		if caret == 0 {
			return true // consumed: backspace at the start is a no-op, not a page gesture
		}
		runes := t.value()
		t.setText(append(append([]rune{}, runes[:caret-1]...), runes[caret:]...), caret-1)
	case ev == input.Named(input.KeyDelete):
		if _, _, ok := t.Selection(); ok {
			t.deleteSelection()
			break
		}
		caret, runes := t.Caret(), t.value()
		if caret >= len(runes) {
			return true
		}
		t.setText(append(append([]rune{}, runes[:caret]...), runes[caret+1:]...), caret)
	case ev == ctrlRune('x'):
		if !t.copySelection() {
			return true
		}
		t.deleteSelection()
	case ev == ctrlRune('c'):
		// Copy only claims the key when there IS something to copy. The
		// framework quit key is ctrl+c and it is checked on what bubbles
		// out of the tree, so an unconditional copy would make a focused
		// TextBox swallow every quit in the house style.
		if !t.copySelection() {
			return false
		}
		return true
	case ev == ctrlRune('v'):
		if !t.insertText(KillBuffer()) {
			return true
		}
	default:
		return false
	}
	return true
}

// insertText replaces the selection with text and puts the caret after
// it. False means nothing was inserted, which is not an error — an empty
// kill buffer and an empty paste are both "no text", and the key is
// still consumed by the caller.
//
// Extracted from the ctrl+v arm rather than restated in HandlePaste. The
// two differ only in where the text came from, and a second copy of the
// splice would be a place for the caret arithmetic to drift.
func (t *TextBox) insertText(text string) bool {
	if text == "" {
		return false
	}
	if _, _, ok := t.Selection(); ok {
		t.deleteSelection()
	}
	caret, runes := t.Caret(), t.value()
	ins := []rune(text)
	next := append(append(append([]rune{}, runes[:caret]...), ins...), runes[caret:]...)
	t.setText(next, caret+len(ins))
	return true
}

// HandlePaste inserts a bracketed paste at the caret.
//
// This exists because bracketed paste is ON by default (gooey.App), and
// the mode CHANGES what a paste into a text field looks like: without it
// the payload arrives as keystrokes and lands one rune at a time, with
// it the payload arrives as one event that nothing would otherwise
// consume. Implementing this is what keeps enabling the mode from
// silently breaking every text field in the tree.
//
// The payload's newlines and control bytes are STRIPPED, not inserted.
// A TextBox is one line — it has no way to show a second — so inserting
// a "\n" would put a rune in the value that the box cannot render and
// that a Changed handler would then hand to whatever consumes the text.
// Tabs become a space for the same reason. This is the policy
// PasteHandler's doc says a consumer is forced to have; the alternative
// is not "no policy", it is "insert control characters silently".
func (t *TextBox) HandlePaste(ev input.PasteEvent) bool {
	if t.Text == nil {
		return false
	}
	if !t.insertText(oneLine(ev.Text)) {
		// Consumed anyway. A paste of nothing into a focused text field
		// is not a gesture for an ancestor to reinterpret.
		return true
	}
	if gooey.CanExecute(t.Changed) {
		t.Changed.Run()
	}
	return true
}

// oneLine flattens a payload for a single-line field: line breaks and
// tabs become one space each, every other control character is dropped.
//
// ONE SPACE PER BREAK, AND A CRLF IS ONE BREAK. This ran per rune, and a
// CRLF is two of them, so every line break from a Windows editor, a
// browser textarea or an RDP clipboard came through as two spaces — the
// common case, not the exotic one. Nothing reports it and the user
// cannot see it: doubled spaces in a value read as whitespace they
// typed. Found in the review of #391 (issue #419).
//
// The flag is what makes it "CR LF is one break" rather than "drop CR".
// A lone carriage return is still a break and still one space, so a
// classic-Mac payload and a payload that really contains a bare CR keep
// their separators, and LF-then-CR stays the two breaks it is.
func oneLine(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevCR := false
	for _, r := range s {
		switch {
		case r == '\n':
			if !prevCR {
				b.WriteRune(' ')
			}
		case r == '\r' || r == '\t':
			b.WriteRune(' ')
		case r < 0x20 || r == 0x7f:
			// Dropped. A NUL or a DEL in a value is invisible on screen
			// and travels into whatever reads the text.
		default:
			b.WriteRune(r)
		}
		prevCR = r == '\r'
	}
	return b.String()
}

// ctrlRune is the event a terminal sends for ctrl+letter.
func ctrlRune(r rune) input.KeyEvent {
	return input.KeyEvent{Key: input.KeyRune, Rune: r, Mods: input.ModCtrl}
}

func (t *TextBox) replace(r rune) {
	if _, _, ok := t.Selection(); ok {
		t.deleteSelection()
	}
	caret, runes := t.Caret(), t.value()
	next := append(append(append([]rune{}, runes[:caret]...), r), runes[caret:]...)
	t.setText(next, caret+1)
}

func (t *TextBox) deleteSelection() {
	lo, hi, ok := t.Selection()
	if !ok {
		return
	}
	runes := t.value()
	t.setText(append(append([]rune{}, runes[:lo]...), runes[hi:]...), lo)
}

// copySelection puts the selection in the kill buffer and reports
// whether there was one.
func (t *TextBox) copySelection() bool {
	lo, hi, ok := t.Selection()
	if !ok {
		return false
	}
	SetKillBuffer(string(t.value()[lo:hi]))
	return true
}

// setText is the one place the bound property is written: text, then
// caret, then the selection dropped, in that order so the caret clamps
// against the new value rather than the old one.
func (t *TextBox) setText(runes []rune, caret int) {
	t.Text.Set(string(runes))
	t.setCaret(caret)
	t.clearSelection()
}

// ---- kill buffer ----

// killBuffer is the process-local cut/copy target. It is UI-goroutine
// state like every other, so it needs no lock.
//
// It is deliberately not the system clipboard. Reaching the terminal's
// clipboard means OSC 52, which is a write-only channel on most
// terminals (you can set it, you cannot read it back), is disabled by
// default in several, and is a genuine exfiltration vector — so it is a
// decision to make on purpose, not a side effect of adding cut and
// paste. Until then, cut and copy move text between fields of the same
// app and nowhere else.
var killBuffer string

// KillBuffer is the text the last cut or copy put aside.
func KillBuffer() string { return killBuffer }

// SetKillBuffer replaces it. Exported so an app can seed the buffer, and
// so a future clipboard integration has one place to hook.
func SetKillBuffer(s string) { killBuffer = s }

// ---- mouse ----

// HandleMouse places the caret, starts a drag selection, and selects a
// word on double click.
func (t *TextBox) HandleMouse(ev input.MouseEvent) bool {
	switch ev.Kind {
	case input.MousePress:
		// The anchor is dropped at the press even though nothing is
		// selected yet: it costs nothing while the pointer has not moved
		// and it is what a drag extends from.
		i := t.indexAt(ev.X)
		t.setCaret(i)
		t.setAnchor(i)
		return true
	case input.MouseClick:
		// Progressive selection, the convention every text field shares:
		// double selects the word under the caret, triple selects the
		// line. Tested with >= so the ceiling can rise again without
		// this silently falling back to single-click behaviour.
		switch {
		case ev.Count >= 3:
			t.selectLine()
		case ev.Count == 2:
			t.selectWord()
		}
		return true
	}
	return false
}

// HandleMouseMove extends the selection while a button is held. It only
// ever runs for this component because the press captured the pointer,
// which is also why dragging past the field's edge keeps working.
func (t *TextBox) HandleMouseMove(ev input.MouseEvent) bool {
	if ev.Button == input.ButtonNone {
		return false
	}
	t.setCaret(t.indexAt(ev.X))
	return true
}

// indexAt maps a screen column to a rune index, through the prompt and
// the current horizontal scroll.
func (t *TextBox) indexAt(x int) int {
	// COLUMNS, not runes: this offsets the caret past the prompt, so a
	// prompt holding a wide glyph put the caret one cell left of the
	// text it belongs to — the same rune-vs-column confusion clipCols
	// was renamed for, one line further on.
	promptW := render.StringWidth(clipCols(getStr(t.Prompt), t.Bounds().W))
	runes := t.value()
	// CLAMPED, because t.scroll is derived state from the LAST paint and
	// a click can arrive before the next one. A bound value that shrank
	// — a viewmodel reset, a hot reload, the scenario Caret()'s own doc
	// names and clamps on read for — left t.scroll past the end, and the
	// walk that indexed runes with it panicked out of the UI goroutine
	// and killed the process.
	//
	// The walk below no longer indexes with it — it runs from the start
	// of the value and uses `start` only to pick which cluster the
	// window opens on — so the fault is gone whether this clamps or not
	// (measured: removing the clamp leaves
	// TestAClickAfterTheValueShrankDoesNotPanic green, where the walk
	// this replaced faulted). It stays because a stale scroll must not
	// be able to choose a cluster outside the value either, and because
	// the next person to reintroduce an index here should not have to
	// rediscover it. Found in the review of #521.
	start := clamp(t.scroll, 0, len(runes))

	// COLUMNS, AND BY CLUSTER, since #519 and its review. `scroll + col`
	// is the rune at that offset only while every rune is one column
	// wide; over a wide glyph it lands one character right per glyph
	// passed, so a click in the middle of a CJK field put the caret
	// somewhere else. Walking rune widths fixed that and left a second
	// disagreement: Render paints a grapheme CLUSTER into one cell, so a
	// walk that stops between a rune and its accent answers with a caret
	// position the screen does not have. Typing there put the typed rune
	// between "e" and its accent; backspace left an orphan mark. Every
	// index this returns is a cluster boundary.
	//
	// A COLUMN LEFT OF THE TEXT WALKS LEFT, and this is the half that has
	// to keep working rather than be clamped away. Dragging past the
	// field's left edge is how a selection reaches text that has scrolled
	// off it, and HandleMouseMove's doc comment promises exactly that.
	// An intermediate version of #519 answered col < 0 with the first
	// VISIBLE rune, reasoning that an off-window index is a caret the
	// user cannot see — but the off-window index IS the autoscroll:
	// scrollFor's `if caret < cur { cur = caret }` pulls the window onto
	// it before the frame is drawn, so it was never invisible. Clamping
	// pinned the caret at the window's left edge forever and the user
	// could not mouse-select anything that had scrolled off (measured:
	// six drags to column -2 over a 6-column field left the selection at
	// [21,24) where walking gives [9,24)). Found in the review of #521.
	//
	// THE WALK IS FROM THE WINDOW, NOT FROM THE START OF THE VALUE, and
	// the comment here used to say the opposite: "a click costs one pass
	// either way — this is not the paint path". True of a click; false
	// of a DRAG, which is the caller three lines up in this same file.
	// HandleMouseMove calls this on every motion event, on the UI
	// goroutine, and motion arrives in bursts. Measured on the version
	// this replaces — a full `string(runes)` copy plus two []int with an
	// entry per cluster, per call: 202µs at 1,000 runes, 1.95ms at
	// 10,000, 27.3ms at 100,000, 51.6ms at 200,000. That is the same
	// O(len) shape this branch already took OUT of scrollFor, relocated
	// to the input path.
	//
	// Render establishes that the answer only ever needs the clusters
	// between the window and the click, so the walk is bounded by the
	// OFFSET rather than by the value: forward from the window for a
	// non-negative column, backward one cluster at a time for a negative
	// one. Both are allocation-free, and the backward half is the
	// drag-left behaviour the paragraph above insists on.
	// Raised in review of #521.
	start = clusterStartAt(runes, start)
	off := x - t.Bounds().X - promptW
	if off < 0 {
		// A column left of the window. Each step is at least one column,
		// so this is bounded by how far past the edge the pointer is.
		i := start
		for need := -off; i > 0 && need > 0; {
			p := clusterStartAt(runes, i-1)
			need -= caretCols(runes, p)
			i = p
		}
		return i
	}
	col, last := 0, start
	eachClusterFrom(runes, start, len(runes), func(at, n, w int) bool {
		if col > off {
			return false
		}
		last = at
		col += w
		return true
	})
	// PAST THE END OF THE TEXT, which is a click in the empty part of the
	// field and puts the caret at the end. Distinguished from "stopped on
	// a cluster" by col: the walk only runs out with col <= off.
	if col <= off {
		return len(runes)
	}
	return clamp(last, 0, len(runes))
}

// selectWord selects the run of like characters around the caret: a
// word, a stretch of whitespace, or a stretch of punctuation.
func (t *TextBox) selectWord() {
	runes := t.value()
	if len(runes) == 0 {
		return
	}
	i := clamp(t.Caret(), 0, len(runes)-1)
	c := class(runes[i])
	lo := i
	for lo > 0 && class(runes[lo-1]) == c {
		lo--
	}
	hi := i + 1
	for hi < len(runes) && class(runes[hi]) == c {
		hi++
	}
	t.setAnchor(lo)
	t.setCaret(hi)
}

// selectLine selects the whole value.
//
// A TextBox is one line by construction — HandlePaste flattens newlines
// to spaces precisely so a second line cannot get into the value — so
// "the line" and "everything" are the same range here, and saying
// selectLine rather than selectAll keeps the name honest about which
// concept it implements. A multi-line editor implementing this
// interface would bound it to the line around the caret and nothing
// above would change.
//
// There is deliberately NO empty-value guard here, unlike selectWord,
// and the difference is worth stating because the symmetry is
// misleading. selectWord's guard prevents a PANIC — it indexes
// runes[clamp(caret, 0, len(runes)-1)], which is runes[-1] on an empty
// value. Nothing here indexes anything: on an empty value this sets the
// anchor and the caret both to 0, and Selection() reports an anchor
// equal to the caret as "no selection" (see its doc), so the result is
// already correct. A guard would be code that cannot change an
// observable outcome, and a mutation run removing it stayed green in
// every test — which is the evidence it was not doing anything, not a
// gap in the tests.
func (t *TextBox) selectLine() {
	t.setAnchor(0)
	t.setCaret(len(t.value()))
}

// ---- word boundaries ----

// runeClass sorts a rune into the three kinds word motion cares about.
// Punctuation is its own class rather than a separator, so ctrl+left
// through "a.b" stops at the dot the way an editor does.
type runeClass uint8

const (
	classSpace runeClass = iota
	classWord
	classPunct
)

func class(r rune) runeClass {
	switch {
	case unicode.IsSpace(r):
		return classSpace
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return classWord
	}
	return classPunct
}

// wordLeft is the start of the word at or before i: skip whatever
// separates, then skip the run it lands in.
func wordLeft(runes []rune, i int) int {
	i = clamp(i, 0, len(runes))
	for i > 0 && class(runes[i-1]) == classSpace {
		i--
	}
	if i == 0 {
		return 0
	}
	c := class(runes[i-1])
	for i > 0 && class(runes[i-1]) == c {
		i--
	}
	return i
}

// wordRight is the end of the word at or after i.
func wordRight(runes []rune, i int) int {
	i = clamp(i, 0, len(runes))
	for i < len(runes) && class(runes[i]) == classSpace {
		i++
	}
	if i == len(runes) {
		return i
	}
	c := class(runes[i])
	for i < len(runes) && class(runes[i]) == c {
		i++
	}
	return i
}
