package markup

import (
	"github.com/WonderForgeLabs/gooey"
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
// # Why a COMPUTED, and why the Get is inside the post
//
// prop.OnInvalidate on a SOURCE never fires — invalidation is what a
// computed does to its dependents, and a source has no upstream to be
// invalidated by. BoundText returns a computed for a bound attribute and
// a SOURCE for a literal one, so `allow` is only observable at all
// because the builder refuses AllowError on a literal Allow. Observing
// `allow` directly would work identically under that restriction and a
// mutation swapping the two is silent — errC is observed because it is
// the handle that survives if the restriction is ever relaxed, not
// because the other one is broken today.
//
// The re-read is the second half, and it is the one that is easy to drop.
// A computed invalidates ONCE and then stays dirty until something reads
// it — so a hook that does not re-evaluate fires on the first bad set and
// is deaf to every set after it. Reading inside the posted closure does
// both jobs at once: it re-validates the computed so the next
// invalidation arms, and it runs the Set on the UI goroutine at Drain
// rather than inside the invalidation that woke it.
func armAllowError(allow, sink *prop.Property[string], d *gooey.Dispatcher) {
	errC := prop.NewComputed(func() string {
		// The Get is unconditional and its result is used on both paths,
		// so this reads `allow` on every evaluation. A Get behind an
		// early return would drop out of the dependency set on the frames
		// where it did not run.
		if _, err := gooey.ParseAllow(allow.Get()); err != nil {
			return err.Error()
		}
		return ""
	})
	errC.OnInvalidate(func() {
		d.Post(func() { sink.Set(errC.Get()) })
	})
	// The priming read. Two jobs, both required: it publishes the state a
	// page loaded with an already-bad set is in — a page that has to wait
	// for a CHANGE before it can be told anything is a page that cannot
	// report the failure it started with — and it is what evaluates the
	// computed the first time, so the hook above has something to be
	// invalidated from.
	sink.Set(errC.Get())
}
