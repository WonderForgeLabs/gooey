package main

import (
	"testing"

	"github.com/WonderForgeLabs/gooey/input"
)

// Wrapping the article is the expensive thing this pane does, and before
// the cache it ran on EVERY input event the focused pane received:
// HandleKey and HandleMouse both call extent() at the top, before the
// switch that decides whether the key was a scroll key at all, and
// extent() calls lines(), which re-wraps the body from scratch. A key
// that fell straight through to `return false` still paid for a full
// re-wrap, and a gesture that did scroll paid twice — once in the
// handler, once in the Render that followed.
//
// Slice identity is the instrument throughout: `&a[0] != &b[0]` is the
// only way to see that no second wrap happened, because a re-wrap
// produces an EQUAL slice, and equality cannot tell the two apart.

// TestARepeatedWrapIsReusedNotRedone is the must-say-yes case. A cache
// that never hits is indistinguishable from no cache, and every other
// test here would still pass.
func TestARepeatedWrapIsReusedNotRedone(t *testing.T) {
	body, _, _ := pane(t, article())

	a := body.lines(paneW)
	b := body.lines(paneW)
	if len(a) == 0 {
		t.Fatal("the article wrapped to nothing; the fixture is wrong, not the cache")
	}
	if &a[0] != &b[0] {
		t.Errorf("two lines(%d) calls for one story returned different backing arrays: "+
			"the wrap ran twice", paneW)
	}
}

// TestANonScrollKeyDoesNotRewrapTheArticle is the finding itself.
func TestANonScrollKeyDoesNotRewrapTheArticle(t *testing.T) {
	body, _, _ := pane(t, article())
	before := body.lines(paneW)

	// 'z' is not in the pane's gesture vocabulary: HandleKey falls
	// through its switch and returns false. It must not have re-wrapped
	// on the way there.
	if body.HandleKey(input.Rune('z')) {
		t.Fatal("'z' was handled; pick a key the pane really ignores")
	}
	after := body.lines(paneW)
	if &before[0] != &after[0] {
		t.Error("a key the pane does not even handle re-wrapped the whole article")
	}
}

// TestTheWrapCacheDoesNotCostThePaneItsStorySubscription is the one that
// matters, and it is deliberately awkward.
//
// lines() reads the story through w.story.Get(), and per this repo's
// call-site rule that Get IS the pane's subscription when it runs inside
// Render. So the Get has to sit ABOVE the cache check. Put it below and
// the dependency stops being recorded on exactly the frames that hit —
// and nothing goes red, because the FIRST Render always misses and
// subscribes.
//
// So the scroll below is load-bearing, not scene-setting. It forces a
// repaint whose lines() call HITS, which is the frame on which a
// misplaced Get would quietly drop the story from this paint node's
// dependency set. Only then is the story replaced.
func TestTheWrapCacheDoesNotCostThePaneItsStorySubscription(t *testing.T) {
	body, _, c := pane(t, article())

	if !body.HandleKey(input.Rune('j')) {
		t.Fatal("'j' did not scroll; the fixture is not long enough to move")
	}
	if _, painted := c.Frame(); painted == 0 {
		t.Fatal("scrolling repainted nothing, so the cache was never exercised " +
			"on a Render — this test cannot see what it exists to see")
	}

	replacement := article()
	replacement.Title = "a replacement title"
	body.story.Set(replacement)

	_, painted := c.Frame()
	if painted == 0 {
		t.Fatal("the story changed and the pane did not repaint: the wrap cache " +
			"returned before reaching w.story.Get(), so that Get stopped being " +
			"recorded as a dependency on cache-hit frames and the pane went deaf")
	}
}

// TestANewStoryIsWrappedFresh is the cache's must-say-no: a changed
// story must not be served the previous article's lines.
func TestANewStoryIsWrappedFresh(t *testing.T) {
	body, _, _ := pane(t, article())
	first := body.lines(paneW)

	replacement := article()
	replacement.Title = "a replacement title"
	body.story.Set(replacement)

	after := body.lines(paneW)
	if &first[0] == &after[0] {
		t.Fatal("a new story reused the previous story's wrapped lines")
	}
	if after[0].text == first[0].text {
		t.Errorf("the first line still reads %q after the title changed", after[0].text)
	}
}
