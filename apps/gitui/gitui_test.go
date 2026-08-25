package main

import (
	"os"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// TestTheShippedPageBuildsAgainstTheRealContext is the axis
// markup's TestEveryGooeyFileInTheRepoHasValidAttributes states it cannot
// cover, and the reason markup's TestEveryModuleShippingAPageBuildsOneInATest
// requires this file to exist.
//
// The corpus test walks every .gooey in the repo and checks attribute
// NAMES against the element vocabulary, with an empty Context, because it
// has no app's bindings to build against. So it catches an undeclared
// attribute and cannot catch Branch="{{.Brunch}}" — a binding naming a
// value no viewmodel supplies. That resolves only here, against the real
// Context.Values, and without this test the first thing to discover a
// broken page is a user starting the app.
//
// The order below is main's own, and it is not incidental. The <Refresh>
// builder posts an initial run through the App while the page is BUILDING,
// so the App has to exist before Build and cannot exist before the context
// its content needs. Build first, fill in u.app, then build the page.
func TestTheShippedPageBuildsAgainstTheRealContext(t *testing.T) {
	u := &ui{}
	ctx, err := u.context()
	if err != nil {
		// exechandlers.New resolves `git` on PATH at startup. No git, no
		// grant, and the failure has nothing to do with the markup.
		t.Skipf("the capability grant would not build, so the page cannot be: %v", err)
	}

	content := markup.Page(os.DirFS("."), "gitui.gooey", ctx)
	u.app = gooey.NewApp(content)
	ctx.Dispatcher = u.app.Dispatcher()

	root, err := content.Build()
	if err != nil {
		t.Fatalf("gitui.gooey does not build against the context main gives it: %v", err)
	}
	if root == nil {
		t.Fatal("gitui.gooey built a nil tree")
	}

	// Building proves every binding RESOLVED. Composing proves the tree
	// also lays out and paints, which is a different failure — a page can
	// resolve every name and still panic in Measure. Frame() returns the
	// number of components painted; on a first frame over a page this size
	// it must be more than one, or the tree is not really there.
	c := gooey.NewComposer(root, 100, 30)
	t.Cleanup(c.Close)
	if _, painted := c.Frame(); painted < 2 {
		t.Fatalf("the first frame painted %d components; the page is not composing", painted)
	}
}
