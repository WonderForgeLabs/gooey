package components

import (
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// TypeAhead is Windows Explorer's type-ahead find, attached to a list.
//
//	<ItemsView Items="{{.Rows}}" Selected="{{.Sel}}">
//	  <TypeAhead Key="Title" Search="{{.Typed}}" NoMatch="{{.Missed}}"/>
//	  <ItemsView.ItemTemplate>…</ItemsView.ItemTemplate>
//	</ItemsView>
//
// You type; the selection jumps to the first item whose value under Key
// begins with what you have typed, in the collection's current order,
// wrapping at the end. Nothing is hidden — this MOVES the selection, it
// does not filter. That is what makes "any movement resets it" coherent:
// a filter would make rows reappear underneath the user mid-gesture, and
// "take me where I'd expect" is undefined when the expected row is one of
// the hidden ones.
//
// Matching is case-insensitive PREFIX, not fuzzy. gooey has a good fuzzy
// matcher (cmd/finder) and ItemsView already transports matched-rune
// positions, so subsequence matching would have been cheap; it was
// rejected because "the first match in the current sort order" is not a
// thing subsequence matching has. Matches scatter, and which one is first
// depends on scoring rather than on where the user is looking.
//
// The mode is entered IMPLICITLY — there is no arming key, because there
// is none in Explorer. Two consequences follow, and both are deliberate:
//
//   - The idle Timeout is load-bearing rather than a nicety. Without it
//     the buffer grows forever and the feature is unusable the moment the
//     user forgets they typed something. `a`, pause, `b` lands on the
//     first `b`, not on `ab`.
//   - A list with a TypeAhead loses j/k navigation, since j now types j
//     (ItemsView binds both as movement keys). The trade is opt-in per
//     list and visible in the markup: declaring this element is a list
//     saying it has Explorer semantics.
//
// Repeating ONE letter cycles rather than searching for the repetition:
// `aaa` steps through successive items beginning with `a`.
//
// Failure is state, not sound. The request said "it beeps"; a terminal
// bell cannot be delivered from here — input dispatch has no route to the
// output stream, Frame.Flush owns the writer — and render.Screen eats
// 0x07 anyway, so a bell would be invisible to gooey's own tests. NoMatch
// is a bindable bool instead: the page decides what a miss looks like,
// which keeps the signal visible on every terminal and keeps this
// attachment non-visual.
type TypeAhead struct {
	gooey.Base

	// Key names the projected item value to match against — the same key
	// the row template binds. Required: a projection is a map[string]any
	// and, with no reflection anywhere, nothing else can say which entry
	// is the label.
	Key string
	// Search is the live buffer, exposed so a page can show it. Explorer
	// displays nothing and survives on muscle memory; a TUI mode that
	// silently changes what every keystroke does is a UI misrepresenting
	// itself, and binding this into a status bar costs one <Text>.
	Search *prop.Property[string]
	// NoMatch reports that the last keystroke matched nothing. The
	// selection is left where it was.
	NoMatch *prop.Property[bool]
	// Timeout is how long the buffer survives idle. Zero means
	// DefaultTypeAheadTimeout.
	Timeout time.Duration
	// Now is the clock the timeout is measured against. Nil means
	// time.Now; tests inject a fake so expiry is advanced rather than
	// slept — ItemsView.Now and FocusManager.Now are the precedent.
	Now func() time.Time

	host *ItemsView
	buf  []rune
	// at is when the buffer last changed. Plain fields, not properties:
	// a keystroke's bookkeeping must not be damage.
	at time.Time
	// last is the index this attachment last selected, or -1. It is how
	// movement by ANY other method is detected — see HandleKey.
	last int
}

// DefaultTypeAheadTimeout is Explorer's idle reset, near enough.
const DefaultTypeAheadTimeout = time.Second

func (t *TypeAhead) Measure(gooey.Size) gooey.Size { return gooey.Size{} }
func (t *TypeAhead) Render(*gooey.Frame)           {}
func (t *TypeAhead) NonVisual() bool               { return true }

// SetHost receives the component this is attached to (gooey.Hosted). A
// host that is not a list leaves the attachment inert; markup rejects
// that arrangement at load time instead of letting it happen quietly.
func (t *TypeAhead) SetHost(w gooey.Component) {
	if v, ok := w.(*ItemsView); ok {
		t.host = v
	}
	t.last = -1
}

func (t *TypeAhead) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

func (t *TypeAhead) timeout() time.Duration {
	if t.Timeout > 0 {
		return t.Timeout
	}
	return DefaultTypeAheadTimeout
}

// Start runs the idle clock. Lifetime belongs to the Composer, exactly as
// it does for Timer: started when the composition goes live, stopped by
// Composer.Close, which covers hot reload, teardown and suspend.
//
// The goroutine never touches the property graph — it posts expire, which
// runs later on the UI goroutine and is ordinary code there.
func (t *TypeAhead) Start(post func(func())) func() {
	if post == nil {
		return func() {}
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		tk := time.NewTicker(t.tick())
		defer tk.Stop()
		for {
			select {
			case <-done:
				return
			case <-tk.C:
				post(t.expire)
			}
		}
	}()
	// Joining makes stop a barrier: a tick that already won the select
	// posts before stop returns, so Close ⇒ no further posts, ever.
	// Signalling alone lets one land afterwards.
	return func() { close(done); <-stopped }
}

// tick is the expiry poll. Quarter of the timeout keeps the worst-case
// overshoot to 25% of it, which is well under what anyone can feel.
func (t *TypeAhead) tick() time.Duration {
	return max(t.timeout()/4, 10*time.Millisecond)
}

// expire clears a buffer that has gone idle. Runs on the UI goroutine,
// having been posted there.
func (t *TypeAhead) expire() {
	if len(t.buf) == 0 || t.now().Sub(t.at) < t.timeout() {
		return
	}
	t.reset()
}

// HandleKey is reached through the attachment seam in FocusManager.
// Dispatch, which offers it the event BEFORE the host — the ordering this
// component exists to use, since ItemsView would otherwise consume j and
// k as movement before a search could ever see them.
func (t *TypeAhead) HandleKey(ev input.KeyEvent) bool {
	if t.host == nil || t.host.Selected == nil {
		return false
	}
	if navigates(ev) {
		// Decline: the buffer resets, and the host still performs the
		// movement. Down at the end of a list does not change Selected,
		// so this eager clear is not covered by the drift check below.
		t.reset()
		return false
	}
	if !searchable(ev, len(t.buf)) {
		return false
	}
	// Movement by any OTHER method — a click, the wheel, a viewmodel
	// write — shows up as the selection having drifted from where this
	// attachment last put it. One comparison covers every method,
	// including ones that do not exist yet, and covering it here rather
	// than eagerly is not an approximation: the buffer has no observable
	// effect except on the next keystroke, which is now.
	// Guarded on a non-empty buffer, which is also what makes the zero
	// value safe: with nothing typed there is no search to invalidate.
	if len(t.buf) > 0 && t.last >= 0 && t.host.Selected.Get() != t.last {
		t.reset()
	}
	if len(t.buf) > 0 && t.now().Sub(t.at) >= t.timeout() {
		t.reset() // idle across a keystroke, in case no tick landed
	}

	t.buf = append(t.buf, ev.Rune)
	t.at = t.now()
	t.publish()

	i, ok := t.match()
	if !ok {
		// The selection stays put and the character stays in the buffer,
		// so continuing to type keeps missing. That is Explorer: a typo
		// is escaped by pausing, not by the buffer quietly forgiving it.
		setBool(t.NoMatch, true)
		return true
	}
	setBool(t.NoMatch, false)
	t.host.selectIndex(i, t.host.count())
	t.last = t.host.Selected.Get()
	return true
}

// match finds the item to select, or reports that nothing matches.
//
// Where the search STARTS is the whole of Explorer's feel:
//
//   - A buffer that just GREW (the second character of "ap") starts at the
//     current selection, so refining a search keeps the item you already
//     landed on when it still matches.
//   - A fresh single character, or the same letter repeated, starts at the
//     item AFTER the selection and wraps. That is what makes a lone letter
//     jump to the next match rather than sitting still, and what makes
//     `aaa` cycle through the a's instead of hunting for a literal "aaa".
func (t *TypeAhead) match() (int, bool) {
	n := t.host.count()
	if n == 0 || len(t.buf) == 0 {
		return 0, false
	}
	src := t.host.source()
	if src == nil {
		return 0, false
	}
	q := strings.ToLower(string(t.buf))
	start := clamp(t.host.Selected.Get(), 0, n-1)
	if cycling(t.buf) {
		q = strings.ToLower(string(t.buf[:1]))
		start++
	} else if len(t.buf) == 1 {
		start++
	}
	for k := 0; k < n; k++ {
		i := ((start+k)%n + n) % n
		vals := src.At(i)
		if vals == nil {
			continue
		}
		if s, ok := vals[t.Key].(string); ok && strings.HasPrefix(strings.ToLower(s), q) {
			return i, true
		}
	}
	return 0, false
}

// reset drops the buffer. The Sets are compared, because prop.Set does
// not: an uncompared write here would cost a frame on every declined
// arrow key.
func (t *TypeAhead) reset() {
	t.buf, t.last = t.buf[:0], -1
	t.publish()
	setBool(t.NoMatch, false)
}

func (t *TypeAhead) publish() {
	if t.Search == nil {
		return
	}
	if s := string(t.buf); s != t.Search.Get() {
		t.Search.Set(s)
	}
}

func setBool(p *prop.Property[bool], v bool) {
	if p != nil && p.Get() != v {
		p.Set(v)
	}
}

// cycling reports a buffer that is one letter repeated — `aaa`, which
// Explorer reads as "next item starting with a" rather than as a search
// for three a's.
func cycling(buf []rune) bool {
	if len(buf) < 2 {
		return false
	}
	for _, r := range buf[1:] {
		if r != buf[0] {
			return false
		}
	}
	return true
}

// searchable reports whether ev is a character the search should consume.
//
// Modified keys are never search input: ctrl+s belongs to whatever bound
// it. Space is search input only once the buffer has something in it — a
// leading space matches nothing anyone meant, and lists commonly give
// space to activation, so an empty buffer hands it back.
func searchable(ev input.KeyEvent, buffered int) bool {
	if ev.Key != input.KeyRune || ev.Mods != 0 {
		return false
	}
	return ev.Rune != ' ' || buffered > 0
}

// navigates reports the keys that reset the search. Enter and tab are
// here because leaving the item or the control ends the gesture as surely
// as moving within it does; esc is the explicit way out.
func navigates(ev input.KeyEvent) bool {
	if ev.Key == input.KeyRune {
		return false
	}
	switch ev.Key {
	case input.KeyUp, input.KeyDown, input.KeyLeft, input.KeyRight,
		input.KeyHome, input.KeyEnd, input.KeyPageUp, input.KeyPageDown,
		input.KeyEnter, input.KeyTab, input.KeyEsc, input.KeyBackspace:
		return true
	}
	return false
}
