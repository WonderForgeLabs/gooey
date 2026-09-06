package markup

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
)

// armAllowError publishes a bound Allow's parse failure into a property
// the page owns — the channel #424 was filed for.
//
// components.Frozen already fails CLOSED on a set it cannot parse, and
// already records why in AllowError(). Nothing read it. A Go host could;
// a page could not, so the only symptom of a typo in a BOUND Allow was a
// subtree that had quietly stopped responding to everything. A literal
// Allow is a load error naming the attribute, and the bound case is the
// one half of the same mistake that shipped silent.
//
// # Why this lives in markup and not in components.Frozen
//
// The obvious fix — have FrozenAllow publish alongside its parse — is a
// UI-goroutine-confinement violation wearing a small diff. FrozenAllow
// is called from FocusManager.frozenHostFor on every routed event,
// including motion, and it is called from INSIDE the Composer's
// evaluation of the freeze observer. A Set from there mutates the
// property graph mid-evaluation, on the routing hot path, once per
// pointer sample. Nothing in the framework would catch it; the tests
// would stay green.
//
// So the publication is arranged out here, where the page is being
// built, and components.Frozen is untouched.
//
// # The accessor is the record, not a second parse
//
// The computed asks f.FrozenAllow() and then f.AllowError() rather than
// re-deriving the message with its own ParseAllow. Two independent parses
// of one string are two answers to one question that agree only by
// coincidence — the day FrozenAllow wraps its error with context, a
// private parse here would silently keep publishing the bare one.
//
// Consulting the accessor also subscribes identically and costs nothing
// extra: FrozenAllow's Allow.Get() is UNCONDITIONAL (frozen.go:114, above
// the cache check), so the dependency edge is recorded whether or not the
// parse cache hits, and a hit skips the parse entirely.
//
// What it publishes is the PARSE, not the seal. gooey.frozenAllow
// (component.go:242) deliberately calls FrozenAllow() before Frozen(), so
// an unparseable set reports itself even while Active is false and
// nothing is sealed. That is the right way round — the message is "this
// set did not parse", which is true regardless of whether the subtree is
// currently frozen — but it means the attribute is misread as "why it
// sealed".
//
// # Why the Get is inside the post, and why both Sets compare
//
// A computed invalidates ONCE and then stays dirty until something reads
// it — so a hook that does not re-evaluate fires on the first bad set and
// is deaf to every set after it. Reading inside the posted closure does
// both jobs at once: it re-validates the computed so the next
// invalidation arms, and it runs the Set on the UI goroutine at Drain
// rather than inside the invalidation that woke it.
//
// prop.Set does not compare (prop/prop.go:101), and Allow changes far more
// often than it breaks: every benign edit — "Focus" to "Hover", both
// parseable, message unchanged at "" — would otherwise invalidate every
// dependent of the sink and repaint the error label for nothing. The
// guard is validate.go:99-102's shape, and it goes on BOTH Sets because
// the priming one has the same property on a reload. The re-read that
// re-arms errC still happens either way; only the publication is skipped.
func armAllowError(f *components.Frozen, sink *prop.Property[string], d *gooey.Dispatcher) {
	errC := prop.NewComputed(func() string {
		// FrozenAllow is called for its parse and its cache write; the
		// error it recorded is then read back. Both calls are
		// unconditional, so neither drops out of the dependency set.
		f.FrozenAllow()
		if err := f.AllowError(); err != nil {
			return err.Error()
		}
		return ""
	})
	publish := func() {
		if v := errC.Get(); v != sink.Get() {
			sink.Set(v)
		}
	}
	errC.OnInvalidate(func() { d.Post(publish) })
	// The priming read. Two jobs, both required: it publishes the state a
	// page loaded with an already-bad set is in — a page that has to wait
	// for a CHANGE before it can be told anything is a page that cannot
	// report the failure it started with — and it is what evaluates the
	// computed the first time, so the hook above has something to be
	// invalidated from.
	publish()
}
