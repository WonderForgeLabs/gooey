package main

import (
	"os"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// TestTheShippedPageBuildsAgainstTheRealContext covers the axis markup's
// TestEveryGooeyFileInTheRepoHasValidAttributes states it cannot, and is
// the file markup's TestEveryModuleShippingAPageBuildsOneInATest requires
// this module to own.
//
// The corpus test walks every .gooey in the repo checking attribute NAMES
// against the vocabulary, with an empty Context, because it has no app's
// bindings. So it catches an undeclared attribute and cannot catch
// Items="{{.TodoItms}}" — a binding naming a value no viewmodel supplies.
// That resolves only here, and without this test the first thing to find
// a broken page is a user starting the app.
//
// Nothing here binds a port or launches the worker companion: the MCP and
// gRPC listeners are wired up in main AFTER the page exists, which is
// exactly what makes ui.context() testable.
func TestTheShippedPageBuildsAgainstTheRealContext(t *testing.T) {
	u := &ui{}
	ctx := u.context()

	dir := "."
	// Mirrors main: agent-authored markup arriving through swap_markup
	// builds from BYTES and has no source FS, so Includes is where an
	// <Image Src> resolves from. Setting it here keeps the test building
	// the same context main does rather than a near-miss.
	ctx.Includes = os.DirFS(dir)

	content := markup.Page(os.DirFS(dir), "kanban.gooey", ctx)
	u.app = gooey.NewApp(content)
	ctx.Dispatcher = u.app.Dispatcher()

	root, err := content.Build()
	if err != nil {
		t.Fatalf("kanban.gooey does not build against the context main gives it: %v", err)
	}
	if root == nil {
		t.Fatal("kanban.gooey built a nil tree")
	}

	// Building proves every binding RESOLVED; composing proves the tree
	// also lays out and paints, which is a separate failure — a page can
	// resolve every name and still panic in Measure.
	c := gooey.NewComposer(root, 120, 40)
	t.Cleanup(c.Close)
	if _, painted := c.Frame(); painted < 2 {
		t.Fatalf("the first frame painted %d components; the page is not composing", painted)
	}
}
