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

	// runes is value()'s memo of the last string it converted, and
	// runesFrom is that string. Derived state like scroll, for the same
	// reason: it is a cache of a pure function of Text, so nothing can
	// change it without changing Text.
	runes     []rune
	runesFrom string
	runesOK   bool
}

// noAnchor is the anchor value meaning "no selection". A real anchor is
// an index into the text, which is never negative.
const noAnchor = -1

// value is the bound text as runes, MEMOISED ON THE STRING IT CAME
// FROM — and the memo is not a micro-optimisation. A single motion
// event converts the whole value three times (indexAt, Caret, setCaret's
// clamp), which is O(len) per conversion on the input path: 100 drag
// events over a 200,000-rune value spent most of their time here, which
// is what TestADragDoesNotWalkTheWholeValue could not see while its
// fixture kept the caret at the end. Raised in review of #521.
//
// THE Get STILL RUNS ON EVERY CALL, which is the half that must not be
// optimised away: getStr is what subscribes the paint node to Text (the
// Get-order rule), so the memo may skip the []rune conversion and never
// the read. Comparing the string is also what makes this safe against a
// value that changed to the same length.
//
// The slice is not defensively copied because nothing writes through
// it: every caller that edits builds a new slice from append([]rune{},
// …). A caller that mutates in place would be editing the memo.
func (t *TextBox) value() []rune {
	s := getStr(t.Text)
	if !t.runesOK || t.runesFrom != s {
		t.runes, t.runesFrom, t.runesOK = []rune(s), s, true
	}
	return t.runes
}

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
	// Read the error AND the styles it selects between before any early
	// return: the read is what subscribes this paint node, and a Get
	// hidden behind a bounds check would silently drop the dependency
	// (the Get-order rule).
	//
	// THE OTHER TWO WERE BELOW THE avail CHECK until #521's review.
	// Nothing painted on such a frame reads either — a prompt filling
	// the field leaves only accent-styled text — so the missed
	// subscription costs no visible staleness today, and the reason to
	// hoist them anyway is that whether it does is a question about the
	// REST of this function, re-answerable by anyone who adds a line to
	// it. errMsg was hoisted for exactly this and these two were left
	// behind.
	errMsg := getStr(t.Error)
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
	// AND SO IS THE SLICE NOW. `string(runes[t.scroll:])` copied the
	// whole tail on every paint — 0.93 ms per call on a 100,000-rune
	// value, out of a 1.14 ms compose, so it was the dominant term of
	// that frame and the last O(len(value)) path in the component. The
	// paragraph this replaces argued the bound could not be computed,
	// because a rune sum is neither an upper nor a lower bound on its
	// clusters' widths — render.StringWidth("⚠️") is 2 against a rune
	// sum of 1, a four-person ZWJ family is 2 against a rune sum of 8 —
	// and guessing short drops glyphs off the right of the field, which
	// is #519 again.
	//
	// That is true of GUESSING and false of walking: spanForCols answers
	// the same question indexAt's forward walk answers, by doubling the
	// span until the walk has passed the column asked for, so it never
	// guesses short and never touches more than the window ends up
	// showing. Raised in review of #521.
	//
	// Routing through eachClusterFrom rather than render.EachCluster
	// also gives this loop the regional-indicator parity correction it
	// did not have: segmenting `runes[t.scroll:]` directly relies on
	// scrollFor never handing it an odd offset into a flag run.
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
	// PAST THE LAST CLUSTER THIS LOOP PAINTED, which is not always
	// len(runes): the full-width stop below refuses a cluster that would
	// overrun, so the loop can end with value left. The trailing caret
	// arm needs the distinction — see it for why.
	painted := t.scroll
	eachClusterFrom(runes, t.scroll, spanForCols(runes, t.scroll, avail), func(i, n, w int) bool {
		// A ZERO-WIDTH CLUSTER TAKES A COLUMN, which is what SetString
		// does with the same string and what this loop refused to do
		// until #521's review. Such a cluster is a mark with nothing in
		// front of it to decorate, so it can only be the first of the
		// span: scrollFor snaps the window back over the others, and
		// reaching one here means the VALUE opens with one.
		//
		// Skipping it was argued to be right on the grounds that the
		// caret arm fires on the containing cluster and a caret inside
		// this one has no cell to reverse either way. True of the
		// cluster, false of the field. Measured on "\u0301abc", focused,
		// caret 0: this loop painted "abc       " with NOT ONE reversed
		// cell, while render.Buffer.SetString on the same string paints
		// the mark. So the character was in the bound property and
		// absent from the screen — #519's own symptom, one Unicode
		// category over — and the user was typing into a field showing
		// no caret at all. The framework's own writer is the authority
		// on what a cluster is worth in columns; disagreeing with it is
		// how a glyph goes missing.
		cols := max(w, 1)
		if x+cols > b.X+b.W {
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
		// runes[i], NOT []rune(string(runes[i:i+n]))[0]. The round trip
		// allocated a string and then a rune slice for every painted
		// cell to recover a rune this loop already has, and the
		// single-rune clusters — every ASCII field in the repo — paid
		// for a Cluster string they then did not use. The two spellings
		// answer the same rune: []rune of a Go string normalises an
		// invalid encoding to U+FFFD, and `runes` came from exactly
		// that conversion in value(), so there is nothing left here for
		// the round trip to normalise. Raised in review of #521.
		c := render.Cell{Rune: runes[i], Style: st}
		if n > 1 {
			c.Cluster = string(runes[i : i+n])
		}
		f.Cells.SetCell(x, b.Y, c)
		x += cols
		painted = i + n
		return true
	})
	// THE CARET'S CLUSTER WAS NOT PAINTED, which is a wider condition
	// than "the caret is past the end" and was written as the narrow
	// one. The full-width stop above refuses a cluster that would
	// overrun the field — correctly, half a glyph is not drawable — and
	// when the refused cluster is the CARET'S the loop leaves nothing
	// reversed, so a focused field shows no caret at all. Measured on
	// this head, "東西南北" focused with the caret on 西:
	//
	//	avail 1                  row " "    reversed ""
	//	width 3, Prompt "> "     row ">  "  reversed ""
	//
	// The second is the one that matters: avail == 1 is reachable at an
	// ordinary field width through a two-column prompt, not only at a
	// one-column field. The per-rune loop this PR replaced wrote a
	// reversed cell here — mangled, but visible — so the narrow
	// condition made it a regression, and it falsifies
	// docs/markup-reference.md's "scrolls horizontally to keep the caret
	// visible in either direction".
	//
	// `caret >= painted` covers both: with the whole value shown,
	// painted == len(runes) and this is the old test; with the loop
	// stopped early it fires for the caret the stop just hid. Raised in
	// review of #521.
	if t.IsFocused() && !selected && caret >= painted && x < b.X+b.W {
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
// the span, so its own cost is O(avail) — the columns that fit — rather
// than O(len). The window is the same one; only the arithmetic moved.
//
// THAT IS THE FUNCTION'S OWN ARITHMETIC AND NOT THE WHOLE BILL, which
// this comment claimed until #521's review measured it: scrollFor calls
// clusterStartAt twice and caretCols once, and each of those copied the
// tail of the value into a string, so the O(len) it removed was still
// being paid three times per frame — 3.34 ms and 185 KB at 100,000
// runes with the caret mid-value. Those three are bounded at their own
// declarations now, and the claim here holds only because they are.
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

// spanReach is how many runes a walk for cols columns may need: every
// cluster is at least one column (the max(w, 1) every walk here
// applies), so cols+1 clusters carry any answer, and the trailing
// clusterSlack is the margin eachClusterFrom's truncation needs — the
// last cluster of a truncated span may itself be a fragment.
//
// `widest` IS MEASURED, NOT ASSUMED, and that is the whole point of
// this function. Both of these bounds were (cols+1)*clusterSlack until
// review of #521 round 8, on the argument that "a cluster longer than
// clusterSlack is already outside what this file promises to segment".
// That is true of clusterStartAt and clusterEndAt, whose fixed ±64
// lookback genuinely cannot find a long cluster's edges. It is FALSE of
// eachClusterFrom, which segments a 101-rune cluster perfectly well
// when it is not re-synchronising into one — and both of these walks
// start from a known boundary, so they never are.
//
// So a value whose AVERAGE cluster ran past 64 runes had both bounds
// stop short of what the window needs. Measured on
// ("a" + U+0301 x 100) x 40 — 4040 runes, 40 clusters, one column each
// — focused, caret at the end, against this file's own oracle
// (clusterBoundaries + clusterCols):
//
//	field   windowFloor   the leftmost fitting boundary
//	    3   3838          3838
//	    5   0             3535
//	   20   0             2020
//
// At five columns the field painted from rune 0 and put the caret block
// at column 4 while the caret was at rune 4040 — a caret in the WRONG
// PLACE, which is worse than the missing one the round before fixed.
// spanForCols had the same arithmetic from the other side: at 20
// columns it stopped after 1344 runes and painted 13 glyphs, 7 blank
// columns, and a 14th cluster TRUNCATED to 30 marks where the value's
// has 100 — a glyph the value does not contain.
//
// Passing the longest whole cluster the walk has actually seen makes
// the bound a function of the value's own segmentation rather than of a
// constant, and leaves the cost flat for the one shape that defeats it:
// a value that is ONE cluster reports no whole cluster at all, so
// `widest` stays at clusterSlack and the walk stops where it always
// did. Raised in review of #521.
func spanReach(cols, widest int) int {
	return (cols+1)*max(widest, clusterSlack) + clusterSlack
}

// spanForCols is an index `to` such that runes[start:to] holds at least
// cols columns, or len(runes) if the value has no more to give.
//
// IT IS THE BOUND Render's doc used to say could not be computed. The
// question is "how many runes can fill cols columns", and no arithmetic
// over rune widths answers it — but a WALK does, and the walk need not
// know the answer in advance: double the span until the clusters in it
// have passed the column asked for. indexAt's forward walk is the same
// shape for the same reason; it is not shared because that one needs the
// cluster it stopped on as it goes, and this one needs only the index.
//
// FLOORED AT ONE PER CLUSTER, matching Render's own max(w, 1) — a span
// measured with zero-width clusters counted as zero is short by one
// column per leading mark, which is the defect this round's finding 1 is
// about, one level up.
//
// THE MARGIN IS WHY IT DOES NOT RETURN `end`. eachClusterFrom segments a
// TRUNCATED string, so the last cluster it reports may itself be a
// truncation of a longer one — returning the index the count reached
// would hand Render a cluster cut in half. Requiring clusterSlack runes
// of slack past it is the same assumption the lookback already makes,
// and it degrades safely: a cluster longer than clusterSlack keeps the
// loop doubling until the span reaches the end of the value, which is
// exactly the unbounded behaviour this replaces.
func spanForCols(runes []rune, start, cols int) int {
	if start >= len(runes) {
		return len(runes)
	}
	// THE DOUBLING IS CAPPED, AND THE CAP IS A FUNCTION OF THE FIELD
	// rather than of the value. Every cluster contributes at least one
	// column (the max(w, 1) below), and a cluster longer than
	// clusterSlack is already outside what this file promises to
	// segment correctly — eachClusterFrom's own doc says it degrades
	// there — so cols clusters of clusterSlack runes is the widest span
	// any answer needs.
	//
	// WITHOUT IT ONE CLUSTER CAN BE THE WHOLE VALUE, and then `got`
	// stays at 1 however far the span reaches, so the loop doubles to
	// len(runes) and re-segments a larger prefix every round. Measured
	// on "a" + U+0301 x n in a 20-column field, per repaint, with
	// windowFloor's floor in place so this cap is the only variable —
	// AND WITH THE CARET AT RUNE 0, which is the caret position that can
	// see it at all:
	//
	//	n        capped    uncapped    ASCII
	//	 1,000   0.47ms    0.49ms      0.06ms
	//	10,000   0.71ms    3.0ms       0.06ms
	//	50,000   0.66ms    15.2ms      0.07ms
	//
	// WITH THE CARET AT THE END the same removal is invisible — 0.79ms
	// against 0.76ms at 50,000 — because the window is at the tail, so
	// `start` is within clusterSlack of len(runes) and the first round
	// returns whatever the cap says. That asymmetry is the whole reason
	// this bound and windowFloor's are separate: this one bounds the
	// walk RIGHTWARD from the window, that one the expansion LEFTWARD to
	// find it, and a fixture sitting at one end exercises one of them.
	// TestARepaintDoesNotWalkAZeroWidthRun runs both carets for that
	// reason and says so.
	//
	// THE CAP IS TWO STATEMENTS AND THE `min` IS THE LOAD-BEARING ONE.
	// This paragraph said the opposite for two rounds — that removing
	// only the `min` lands on maxSpan anyway because the doubling from
	// cols+clusterSlack reaches it exactly, "20 columns gives 84 and
	// 1344, exactly 16x" — and BOTH halves of that were wrong. maxSpan
	// is spanReach(cols, 0), which is (cols+1)*clusterSlack +
	// clusterSlack: at 20 columns 22*64 = 1408, not 1344, so the
	// sequence 84, 168, 336, 672, 1344, 2688 steps straight over it and
	// the equality exit never fires. Re-measured with only line 641
	// removed:
	//
	//	--- FAIL: TestARepaintDoesNotWalkAZeroWidthRun
	//	  caret at the end:   16.2ms over 50,000 against 645µs over 1,000 — 25.1x
	//	  caret at the start: 14.8ms over 50,000 against 447µs over 1,000 — 33.0x
	//
	// against a 4x budget. The mutation this called silent is the
	// loudest one there is, and the advice to "neuter maxSpan itself"
	// sent the next reader past the line that actually holds.
	//
	// The copy of this paragraph in indexAt is NOT the same fact any
	// more and the two have been separated. Its min-only mutation IS
	// silent — measured, the whole components suite green — because its
	// doubling starts at off+clusterSlack and off varies per click, so
	// no fixture pins that one line. See indexAt. Corrected in review of
	// #521 round 10.
	//
	// The comment at the doubling's other end claimed the work stays
	// "proportional to the runes the window ENDS UP SHOWING rather than
	// to the value"; that was true of every vocabulary but this one, and
	// the cap is what makes it true of this one too. On the UI goroutine
	// inside a paint node, and reachable by paste. Raised in review of
	// #521.
	maxSpan := spanReach(cols, 0)
	for span := cols + clusterSlack; ; span *= 2 {
		span = min(span, maxSpan)
		to := start + span
		if to >= len(runes) {
			return len(runes)
		}
		// widest TRAILS BY ONE, because the last cluster a truncated
		// span reports may be a fragment of a longer one — counting it
		// would let a truncation raise the bound that produced it.
		got, end, widest, prev := 0, start, 0, 0
		eachClusterFrom(runes, start, to, func(at, n, w int) bool {
			got += max(w, 1)
			end = at + n
			widest, prev = max(widest, prev), n
			return got <= cols
		})
		if got > cols && end+clusterSlack <= to {
			return to
		}
		if reach := spanReach(cols, widest); reach > maxSpan {
			maxSpan = reach
			continue
		}
		if span == maxSpan {
			// A cluster this file does not promise to segment is open
			// here. Returning the cap hands Render a window boundary
			// inside it — the same degradation clusterStartAt takes,
			// and bounded the same way.
			return to
		}
	}
}

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
	// flags and every pair from there is offset by one (UAX #29
	// GB12/GB13 decide the boundary by the parity of the whole run).
	// Only when `lo` landed IN a run — landing just after one is already
	// a true boundary — so this costs nothing on the values that have no
	// flags in them, which is nearly all of them.
	//
	// THE PARITY IS THE ANSWER, NOT THE RUN'S START, and that
	// distinction is worth the two lines. This moved `lo` back to the
	// run's own start, which is correct and made the SEGMENTED SPAN as
	// long as the run: measured over 5,000 flags, 100 clusterStartAt
	// calls cost 42.9 ms against 0.42 ms on ASCII of the same rune
	// count — 100x, on the paint path and again per column of a drag.
	// Any EVEN offset into the run is a true pair boundary, so dropping
	// back to the nearest one keeps the parity and leaves the span
	// bounded by clusterSlack. Finding the run's start still walks it,
	// but that walk is a rune comparison per step and segments nothing.
	// Raised in review of #521, both halves.
	//
	// WHAT THAT SCAN COSTS ON A VALUE THAT IS ALL FLAGS, because the
	// sentence above says what it is not and the one at clusterSlack
	// said the cost is nothing "on values with no flags" — which is the
	// case it is not about. The scan is unbounded IN THE RUN, and Render
	// plus scrollFor make roughly half a dozen lookbacks per frame.
	// Measured, 100,000 runes in a 40-column focused field with the
	// caret mid-value:
	//
	//	                  repaint/frame   100 drag-left events
	//	all flags            1.75 ms            74.8 ms
	//	ASCII, same runes    0.153 ms           24.1 ms
	//
	// 11.4x, and not a freeze on a value no user types. Recorded rather
	// than closed: the run's start is a pure function of the value, so
	// it could be memoised beside the cached runes, and that is a
	// change to the paint path's state rather than to this arithmetic.
	// Raised in review of #521.
	if lo > 0 && regionalIndicator(runes[lo]) {
		runStart := lo
		for runStart > 0 && regionalIndicator(runes[runStart-1]) {
			runStart--
		}
		lo -= (lo - runStart) % 2
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

// clusterEndAt is the first cluster boundary strictly after i — the
// right arrow's destination, and clusterStartAt's mirror.
func clusterEndAt(runes []rune, i int) int {
	if i < 0 {
		return 0
	}
	if i >= len(runes) {
		return len(runes)
	}
	// THE SPAN RETRIES, for clusterStartAt's reason mirrored: a fixed
	// at+clusterSlack window cannot hold a cluster longer than
	// clusterSlack, so the walk never reported one containing `i` and
	// this answered the arithmetic i+1. Measured on six clusters of 101
	// runes: clusterEndAt(65) was 66, where the cluster ends at 101.
	//
	// AN END FOUND INSIDE THE SPAN IS THE ONLY ONE TO TRUST — a cluster
	// that ends exactly at `to` may be a truncation of a longer one, the
	// same margin spanForCols requires — and the retry stops on
	// spanReach for the same reason clusterStartAt's does: a value this
	// file cannot segment must not turn a lookup into an O(len) walk.
	// Raised in review of #521.
	at := clusterStartAt(runes, i)
	end := i + 1
	for span := clusterSlack; ; span *= 2 {
		to := at + span
		found, widest, prev := false, 0, 0
		eachClusterFrom(runes, at, to, func(a, n, _ int) bool {
			widest, prev = max(widest, prev), n
			if i >= a && i < a+n {
				end, found = a+n, a+n < to
				return false
			}
			return true
		})
		if found || to >= len(runes) || span >= spanReach(1, widest) {
			return end
		}
	}
}

// clusterStartAt is the index the cluster containing i begins at.
//
// BOUNDED ON BOTH SIDES, and the right-hand bound is the half that was
// missing. eachClusterFrom copies runes[lo:to] into a string, so passing
// len(runes) here made a question about ONE cluster cost a copy of the
// whole tail: MEASURED at 1.19 ms and 57 KB per call over a
// 100,000-rune value, on the paint path inside a prop.NewComputed and
// again per column walked on a drag. That is the same O(len) shape this
// branch took out of scrollFor, relocated into the helper that replaced
// it. clusterSlack is already the lookback, and it is the lookahead for
// the same reason and with the same residual: a cluster longer than it
// is reported short, which its own doc records. Raised in review of
// #521.
func clusterStartAt(runes []rune, i int) int {
	if i <= 0 || i >= len(runes) {
		return i
	}
	// THE LOOKBACK RETRIES, because one pass cannot tell a boundary from
	// a re-synchronisation. eachClusterFrom segments from clusterSlack
	// runes before `from`, so the FIRST cluster it reports starts at
	// that origin and is a fragment whenever the origin landed inside
	// one. Answering with it returns an arithmetic index wearing a
	// boundary's name — and a single pass did exactly that for every
	// cluster longer than the lookback.
	//
	// `at > origin` is the test: the containing cluster starting
	// strictly right of the origin means at least one whole cluster was
	// walked before it, which is the assumption the re-sync rests on.
	// Found by the vocabulary entry #521's round-8 finding 3 asked for:
	// six clusters of 101 runes reddened
	// TestTheScrollWindowAlwaysOpensOnAClusterBoundary at caret 65,
	// where this answered 1 — the origin — and the window opened inside
	// the glyph.
	//
	// AND IT STOPS, which is the half that keeps the round's cost work
	// intact. Doubling the lookback until it finds a boundary is O(i) on
	// a value that is one cluster, which is the O(len) walk this whole
	// round removed from the other three walks. spanReach reads "no
	// whole cluster seen" as no evidence and floors at clusterSlack, so
	// a run this file cannot segment gives up after 192 runes and
	// returns the fragment start — the documented degradation, now
	// reached only where it is the real answer.
	at := i
	for back := clusterSlack; ; back *= 2 {
		// eachClusterFrom takes the span; the origin is clusterSlack
		// before it, and the origin is what has to move.
		from := max(i-back+clusterSlack, 0)
		origin := max(from-clusterSlack, 0)
		at = i
		found := false
		widest, prev := 0, 0
		eachClusterFrom(runes, from, i+1, func(a, n, _ int) bool {
			widest, prev = max(widest, prev), n
			if i >= a && i < a+n {
				at, found = a, true
				return false
			}
			return true
		})
		// `found` IS NOT REDUNDANT WITH `at > origin`. eachClusterFrom
		// drops its own first cluster whenever it re-synchronised, so a
		// span that is ONE such fragment reports nothing at all — and
		// `at` is still the untouched `i`, which is right of the origin
		// and passed the test. That is the exact arm this retry exists
		// for, so writing it without the flag made the whole loop a
		// no-op: measured, clusterStartAt(65) of six 101-rune clusters
		// answered 65 again.
		if (found && at > origin) || origin == 0 || back >= spanReach(1, widest) {
			return at
		}
	}
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
	// The span ends clusterSlack runes on, for the reason clusterStartAt
	// above carries: the caret's own cluster is the only one this asks
	// about, and len(runes) bought a whole-tail copy per call.
	eachClusterFrom(runes, i, i+clusterSlack, func(at, n, w int) bool {
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
	// FLOORED AT ONE COLUMN PER RUNE, the same floor Render, indexAt and
	// collect below already apply — and here it is what BOUNDS the walk
	// rather than what makes it accurate. render.RuneWidth answers 0 for
	// a combining mark, so an unfloored `w+rw > avail` never grows w
	// through a zero-width run and the loop runs to index 0, handing
	// collect a span of the whole value to copy into a string. Measured
	// on "a" + U+0301 x n in a 20-column field, per repaint:
	//
	//	n        zero-width run    ASCII of the same rune count
	//	 1,000   0.54ms            0.06ms
	//	10,000   5.4ms             0.07ms
	//	50,000   29.8ms            0.06ms
	//
	// Linear in the value, on the UI goroutine inside a paint node, and
	// reachable by paste — oneLine strips control characters and not
	// combining marks. With the floor the walk stops after at most
	// `avail` runes.
	//
	// LANDING FURTHER RIGHT IS SAFE, which is why a floor is the right
	// correction and not a lie about the width: this is a CANDIDATE, and
	// the cluster pass below expands leftwards until it can prove it has
	// gone far enough. Flooring can only make the guess narrower.
	// Raised in review of #521.
	i := end
	w := reserve
	for i > 0 {
		rw := max(render.RuneWidth(runes[i-1]), 1)
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
	var segs []seg
	total := 0
	collect := func(from int) {
		segs = segs[:0]
		total = reserve
		eachClusterFrom(runes, from, end, func(at, n, cw int) bool {
			// FLOORED AT ONE, THE WAY Render FLOORS. Render advances by
			// max(w, 1) so a value opening with a combining mark is
			// painted at all; a window that sums the raw width believes
			// it has a column Render will not give it. Measured on
			// "\u0301abc" focused with the caret at the end in a
			// four-column field: the window never scrolled, the caret
			// block's x < b.X+b.W was false, and the field showed a
			// value with no caret in it — the injury
			// TestAValueThatOpensWithACombiningMarkIsPaintedAndCarets
			// exists for, at the width where the slack runs out. seg.w
			// carries the floored figure so the drop loop below
			// subtracts what the total added. Raised in review of #521.
			cw = max(cw, 1)
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
	//
	// AND DOUBLING IS NOT ENOUGH ON ITS OWN, which the paragraph above
	// did not say and this is the correction. The exit condition is
	// `total > avail`, and a span that is ONE UNFINISHED CLUSTER
	// contributes one column however far left it reaches — so on a
	// value that is a single cluster the loop expands to rune 0,
	// re-collecting a larger span each round, and each collect copies
	// its span into a string. Profiled at 50,000 runes: 330ms of a
	// 380ms sample inside this loop's eachClusterFrom, 90ms of it
	// slicerunetostring.
	//
	// So the expansion has a FLOOR, and it is a function of the field
	// exactly like spanForCols' cap: a window shows at most `avail`
	// clusters and a cluster longer than clusterSlack is already
	// outside what this file promises to segment, so nothing correct
	// lies further left than that product. Measured per repaint on
	// "a" + U+0301 x n in a 20-column field:
	//
	//	n        before    after     ASCII of the same rune count
	//	 1,000   0.54ms    0.82ms    0.06ms
	//	10,000   5.4ms     0.78ms    0.06ms
	//	50,000   29.8ms    0.75ms    0.07ms
	//
	// FLAT, not fast: ~0.75ms whatever the length, against ~0.06ms for
	// ASCII. The residual is the value's []rune conversion, which is
	// O(len) for any vocabulary and is not this function's to remove —
	// what these two bounds buy is that the figure stops growing.
	//
	// Raised in review of #521.
	// THE REACH IS MEASURED, NOT A CONSTANT — see spanReach, which
	// carries the measurement and the defect a fixed (avail+1)*
	// clusterSlack floor produced here. Only segs[1:] count towards it:
	// segs[0] starts at `from`, which is wherever the arithmetic landed,
	// so its length says nothing about a cluster.
	//
	// GIVING UP IS A SEPARATE ANSWER FROM RUNNING OUT OF VALUE, which is
	// what `gaveUp` carries. The loop stops early only when the reach it
	// has already spanned covers what the widest whole cluster it found
	// says any answer needs — i.e. the span is essentially one
	// unfinished cluster, the one shape neither this walk nor
	// clusterStartAt can find the edges of.
	spanned, gaveUp := end-i, false
	for total <= avail && i > 0 {
		reach := spanReach(avail, widestWhole(segs))
		if spanned >= reach {
			gaveUp = true
			break
		}
		spanned = min(max(2*spanned, clusterSlack), reach)
		i = max(end-spanned, 0)
		collect(i)
	}
	// The loop below is what lands the answer: it leaves i at a cluster
	// START every time it runs.
	for k := 0; k < len(segs) && total > avail; k++ {
		total -= segs[k].w
		i = segs[k].at + segs[k].n
	}
	// AND IF THE EXPANSION STOPPED AT THE FLOOR, i IS AN ARITHMETIC
	// INDEX, NOT A BOUNDARY.
	// The paragraph above used to end "the expansion above guarantees it
	// runs unless the span already reaches rune 0", and the floor added
	// in the same commit falsified that: the expansion also stops at
	// `i == floor`, and it stops there WITHOUT overflowing whenever the
	// span is one unfinished cluster, because such a span contributes
	// one column however far left it reaches. total <= avail, the drop
	// loop does not run, and the raw product `end-(avail+1)*clusterSlack`
	// is returned mid-cluster.
	//
	// Render then opens eachClusterFrom inside that cluster and reports
	// only the skipped fragment, which is zero-width. Measured, focused,
	// caret at the end, 20 columns, on "a" + U+0301 x n:
	//
	//	n        scroll   row
	//	   100   0        "á́́…"   (the value)
	//	 1,000   0        "á́́…"   (the value)
	//	 5,000   3656     "█"     <- the field paints NOTHING but the caret
	//	50,000   48656    "█"
	//
	// THERE IS NO BOUNDARY TO SNAP TO, which is why the answer is 0 and
	// not a snap. clusterStartAt and clusterEndAt are both bounded by
	// clusterSlack on purpose, and a cluster this long is already past
	// what this file promises to segment — so neither can find its real
	// edges, and both would hand back another arithmetic index wearing a
	// boundary's name.
	//
	// Opening at 0 is the honest degradation and is what the same value
	// does at every length the floor does not reach: the glyph is
	// painted from its start, the caret is where a caret past a
	// single-cluster value can be. It costs nothing unbounded, because
	// Render's own walk from 0 is capped by spanForCols. Raised in
	// review of #521, on the floor added the round before.
	//
	// `gaveUp` IS THE WHOLE TEST, and the two narrower ones tried first
	// are both wrong. "The drop loop did not run" also covers the case
	// where NOTHING fits — reserve 2 in a 1-column window, where the
	// collect finds no cluster it can keep and `end` is the right answer
	// — and returning 0 there moved the window to the start of the value
	// on every too-narrow field
	// (TestTheWindowFloorIsTheLeftmostFittingClusterBoundary catches it
	// at value 0, end=1). `i == floor` was the second, and it was right
	// only while the floor was a constant: it also fires wherever the
	// arithmetic happens to land on the reach, including on values whose
	// real boundary is further left and findable, which is exactly the
	// defect the adaptive reach removes.
	if gaveUp {
		return 0
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
// AN ARROW STEPS BY CLUSTER, NOT BY RUNE, and that is the fourth site
// the three above have to agree with. indexAt quantises a click to a
// cluster boundary and Render reverses the whole cluster, so a caret
// between a rune and its accent is a position the screen cannot show
// and the mouse cannot reach — but the keyboard reached it, and the
// consequence is not cosmetic. Measured on decomposed "éx", focused:
//
//	caret 0 -> row "éx", reversed "é"
//	caret 1 -> row "éx", reversed "é"   <- pixel-identical; the keypress
//	                                       did nothing a user can see
//	typing 'Z' at caret 1   -> "eŹx"    (the accent moved onto the typed rune)
//	backspace at caret 1    -> "́x"      (an orphan mark leads the value)
//
// This PR made it worse before it made it better: widening the caret
// arm from caret == i to caret >= i && caret < i+n removed the one cue
// there was, so a reachable destructive position became identical to a
// safe one. A ZWJ family is the loud version — four of five presses do
// nothing visible.
//
// THE COST, STATED: a stray combining mark can no longer be deleted by
// arrowing between it and its base. It is still deletable — select it,
// or delete the whole cluster and retype the base — and the trade is
// the right way round, because reaching the split position by accident
// is what a user does and reaching it on purpose is not.
//
// Home and End are already boundaries. Word motion goes through
// wordLeft/wordRight, and this said they "move between runs of non-word
// runes and cannot stop inside one" — which is false for a base letter
// followed by a combining mark, because class() sorts an Mn into
// classPunct and the run boundary lands mid-cluster. They snap through
// snapOut now, and so does selectWord; the claim here is a consequence
// of that rather than of the class walk. Raised in review of #521.
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
			to = clusterStartAt(runes, caret-1)
		}
	case input.KeyRight:
		if word {
			to = wordRight(runes, caret)
		} else if _, hi, ok := t.Selection(); ok && !shift {
			to = hi
		} else {
			to = clusterEndAt(runes, caret)
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
	// one. The backward half is the drag-left behaviour the paragraph
	// above insists on.
	//
	// BOUNDED IN THE WALK IS NOT BOUNDED IN THE SPAN, and this comment
	// said "both are allocation-free" when neither was. Stopping the
	// callback early does not stop eachClusterFrom having already copied
	// runes[lo:to] into a string, and both halves passed the end of the
	// value as `to` — so a drag ten columns left of a 100,000-rune field
	// cost 21.9 ms and 1.6 MB per motion event, on the UI goroutine.
	// Each half bounds its own span now: the forward walk doubles from
	// off+clusterSlack, and the backward one inherits clusterStartAt's
	// and caretCols' bounds. Raised in review of #521.
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
	// A SPAN THAT GROWS, not the rest of the value, and the difference
	// is the allocation: eachClusterFrom copies runes[lo:to] into a
	// string, so `to = len(runes)` made a walk of `off` columns cost a
	// copy of the tail — 3.74 ms and 516 KB per call over a
	// 100,000-rune value with the window mid-value, measured in review
	// of #521. off+clusterSlack runes hold off+1 columns unless the span
	// is mostly zero-width clusters, which is why this doubles rather
	// than guessing once: the same shape windowFloor uses for the same
	// reason. The loop ends when the walk has passed the column asked
	// for or the span has reached the end of the value.
	//
	// AND THE DOUBLING IS CAPPED, for spanForCols' reason and by its
	// arithmetic. Every cluster below contributes at least one column
	// (the max(w, 1)), so reaching column off+1 takes at most off+1
	// clusters, and a cluster longer than clusterSlack is already
	// outside what eachClusterFrom promises to segment — so
	// (off+1)*clusterSlack runes is the widest span any answer needs.
	// Without the cap ONE cluster can be the whole value: `col` stays
	// at 1 however far the span reaches, so the loop doubles to
	// len(runes) and re-segments a larger prefix each round. Measured
	// on "a" + U+0301 x n, one click five columns into a 20-column
	// field, per call:
	//
	//	n         capped    uncapped    ASCII
	//	  1,000   11.4µs    190µs       9.8µs
	//	 10,000   11.0µs    1.90ms      15.7µs
	//	 50,000   11.2µs    8.05ms      8.4µs
	//	200,000   11.3µs    32.6ms      8.3µs
	//
	// This is the FOURTH walk of this shape on this path and the last
	// one left uncapped; spanForCols' cap and windowFloor's reach are
	// the other two bounds, and clusterStartAt's lookback the third.
	//
	// NAMED, NOT NUMBERED. This cited spanForCols:586 and
	// windowFloor:935 until review of #521 round 8, and both were
	// already off in the commit that wrote them — :586 was a line of
	// that comment's own prose and :935 a row of a measurement table.
	// Nothing in this tree resolves a line number written in a comment
	// (TestEveryCitedTestNameResolves covers test NAMES cited in
	// CLAUDE.md, which is a different claim), so they drift in silence;
	// a symbol cannot. It
	// runs once per MOUSE MOTION REPORT while a drag is live, on the UI
	// goroutine, so 32.6ms here is a third of a second of input latency
	// over ten reports.
	//
	// THE CAP IS TWO STATEMENTS, AND HERE THE `min` REALLY IS THE
	// SILENT ONE — which is no longer true of spanForCols' copy of this
	// paragraph, so the two have stopped being word for word. Removing
	// only the `min` below leaves the whole components suite green;
	// measured, as is `maxSpan := len(runes) + 1` taking
	// TestADragDoesNotWalkAZeroWidthRun red. So the cap is pinned and
	// this one line of it is not.
	//
	// The REASON is the budget, not arithmetic, and the arithmetic this
	// paragraph used to give was wrong anyway: maxSpan is
	// spanReach(budget, 0) = (budget+1)*clusterSlack + clusterSlack,
	// which is 1408 at 20 columns rather than 1344, and the doubling
	// sequence steps over it rather than landing on it. What makes the
	// mutation silent here is that `off` varies per click while
	// spanForCols' `cols` is the field width — so no fixture holds this
	// walk at one span long enough to pin the line. Corrected in review
	// of #521 round 10.
	// THE BUDGET IS THE FIELD'S, NOT THE CLICKED COLUMN'S, and that is
	// finding 2 of the same round. `off` is where the pointer is and
	// `reach` is how far the walk may go to get there; sizing the reach
	// by `off` made this walk's span STRICTLY NARROWER than the one
	// Render painted from — off < avail for every click inside the
	// field — so at the cap the two answered different clusters for the
	// same column. Measured on ("a" + U+0301 x 100) x 40 in a
	// 20-column field at scroll 0:
	//
	//	column 13 painted the cluster at rune 1313, indexAt said 808
	//	column 10 painted the cluster at rune 1010, indexAt said 606
	//	column  5 painted the cluster at rune  505, indexAt said 303
	//
	// That is the paint/click disagreement
	// TestTheScrollWindowAlwaysOpensOnAClusterBoundary's doc calls the
	// thing the whole cluster-boundary design exists to prevent,
	// reached through the cap rather than through a mid-cluster
	// t.scroll, so no grid over scrollFor could see it. max(off, avail)
	// rather than avail because a DRAG can be right of the field.
	//
	// AND NOTHING PINS THIS LINE, which is worth writing down rather
	// than leaving for the next reader to discover. The table above was
	// measured against the FIXED cap, and spanReach's adaptive one
	// closes the gap on its own: this walk stops on `col > off` long
	// before any cap whenever the clusters are findable at all, and
	// where they are not — one cluster wider than the reach — both
	// walks see that one cluster and answer its start either way.
	// Measured: reverting this to `off` leaves
	// TestAClickAnswersTheClusterItsColumnPaints green. It stays
	// because the guarantee it makes is STRUCTURAL — the click walk may
	// not be given a smaller budget than the paint walk — where the
	// other is contingent on two adaptations agreeing, and because a
	// future bound that is not adaptive would reintroduce the defect
	// with nothing red. Raised in review of #521.
	avail := t.Bounds().W - promptW
	budget := max(off, avail)

	col, last := 0, start
	capped := false
	maxSpan := spanReach(budget, 0)
	for span := off + clusterSlack; ; span *= 2 {
		span = min(span, maxSpan)
		to := start + span
		if to >= len(runes) {
			to = len(runes)
		}
		col, last = 0, start
		widest, prev := 0, 0
		eachClusterFrom(runes, start, to, func(at, n, w int) bool {
			if col > off {
				return false
			}
			last = at
			widest, prev = max(widest, prev), n
			// FLOORED, for Render's reason and windowFloor's — and note
			// the BACKWARD walk three lines up already floors, because
			// it goes through caretCols. This one did not, so indexAt
			// disagreed with itself: every click on a value opening with
			// a combining mark landed one index late and index 0 was
			// unreachable by mouse.
			col += max(w, 1)
			return true
		})
		if col > off || to == len(runes) {
			break
		}
		if reach := spanReach(budget, widest); reach > maxSpan {
			maxSpan = reach
			continue
		}
		if span == maxSpan {
			// A cluster this file does not promise to segment is open
			// at `last`, and the column asked for is somewhere inside
			// it. `last` is where it starts, which is the same
			// degradation spanForCols takes at its own cap — NOT the
			// end of the value, which is what the exit below would
			// otherwise make of `col <= off`.
			capped = true
			break
		}
	}
	// PAST THE END OF THE TEXT, which is a click in the empty part of the
	// field and puts the caret at the end. Distinguished from "stopped on
	// a cluster" by col: the walk only runs out with col <= off — and
	// from "stopped at the cap" by `capped`, because that walk did not
	// run out of value, only out of span.
	if col <= off && !capped {
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
	// BOTH EDGES ON CLUSTER BOUNDARIES, for the reason snapOut records —
	// and here it buys a second thing the arrow path does not need.
	// Render's overlap arm reverses the WHOLE cluster for a selection
	// covering half of it, which is the right answer for the paint side
	// and makes the mismatch invisible: the screen said the accent was
	// selected while the range that would replace it did not include
	// it. Measured on "a" + U+0301 + "b", double-click at caret 0 —
	// selection [0,1), reversed "á", typing Z gave "Źb". Raised in
	// review of #521.
	t.setAnchor(snapOut(runes, lo, false))
	t.setCaret(snapOut(runes, hi, true))
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

// snapOut moves i off the inside of a grapheme cluster, outward in the
// direction it was travelling — left for a leftward walk, right for a
// rightward one. A position already on a boundary is returned unchanged.
//
// IT IS THE CLASS TABLE THAT MAKES THIS NECESSARY, and the reason is
// narrow enough to be worth writing down: class() sorts a combining
// mark into classPunct, because unicode.IsSpace, IsLetter and IsDigit
// are all false for an Mn. So the class boundary the word walks stop at
// falls BETWEEN a base letter and its accent, which is the middle of
// one cluster. Measured:
//
//	"ab" + U+0301   wordLeft(3)  = 2   cluster "b́" is [1,3)
//	"a" + U+0301 + "b"
//	                wordRight(0) = 1   cluster "á" is [0,2)
//
// and from there every destructive outcome moveKey's doc enumerates for
// the arrow key is reachable through ctrl+arrow instead: typing moves
// the accent onto the typed rune, backspace deletes the base and
// reattaches the orphan mark to its neighbour.
//
// NOT IN setCaret, which would cover these and selectWord at one choke
// point and is the wrong place: setText sets caret+len(ins), where a
// pasted rune that joins the PRECEDING cluster would be pulled
// backwards by a snap. Quantising at the three producers leaves the
// edit path alone. Raised in review of #521.
func snapOut(runes []rune, i int, rightward bool) int {
	// HOISTED, because clusterStartAt is not free: it re-segments a
	// clusterSlack-rune lookback, and the leftward arm called it twice
	// for one answer. Raised in review of #521.
	at := clusterStartAt(runes, i)
	if at == i || !rightward {
		return at
	}
	return clusterEndAt(runes, i)
}

// wordLeft is the start of the word at or before i: skip whatever
// separates, then skip the run it lands in. The answer is a cluster
// boundary — see snapOut.
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
	return snapOut(runes, i, false)
}

// wordRight is the end of the word at or after i. The answer is a
// cluster boundary — see snapOut.
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
	return snapOut(runes, i, true)
}

// seg is one cluster of a collected span: where it starts, how many
// runes it holds, and how many columns it takes with the one-column
// floor Render applies already in it.
//
// PACKAGE SCOPE, not windowFloor's local, only so widestWhole can take
// it — the reach that reads it has to be a function the comment can
// explain once rather than three lines inlined in a loop.
type seg struct{ at, n, w int }

// widestWhole is the longest cluster in segs whose LEFT EDGE is a real
// boundary — every one but the first, which starts wherever the caller
// sliced. Zero when there is no such cluster, which spanReach reads as
// "no evidence" and floors at clusterSlack.
func widestWhole(segs []seg) int {
	w := 0
	for _, s := range segs[min(1, len(segs)):] {
		w = max(w, s.n)
	}
	return w
}
