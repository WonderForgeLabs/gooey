package components

import (
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/render"
)

// A page with a toast layer: full-width content under a host that spans
// the whole composition, the way an app declares it (last child = top
// of the z-order).
func toastPage(w, h int) (*ToastHost, *Canvas, *Text) {
	content := &Text{Content: Str(strings.Repeat("#", w))}
	host := &ToastHost{}
	page := &Canvas{Children: []gooey.Component{content, host}}
	return host, page, content
}

func TestToastShowPaintsTheToastOverTheContent(t *testing.T) {
	host, page, _ := toastPage(30, 4)
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()
	if got := row(c.Cells(), 0); got != strings.Repeat("#", 30) {
		t.Fatalf("row 0 = %q", got)
	}

	host.Show("saved")
	_, painted := c.Frame()
	// The toast is a new paint node; the host's own node is clean and
	// stays clean — hosting the layer costs nothing per toast.
	if painted != 1 {
		t.Fatalf("showing a toast painted %d components, want 1 (the toast)", painted)
	}
	if got := row(c.Cells(), 0); !strings.HasSuffix(got, " saved") {
		t.Fatalf("row 0 = %q, want the toast in the top-right corner", got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d components, want 0", painted)
	}
}

// Dismissing must repaint the vacated cells FROM WHAT WAS BENEATH —
// the composer's restore pass, exercised through the Dynamic-departure
// path a toast actually takes.
func TestToastDismissRestoresWhatWasBeneath(t *testing.T) {
	host, page, _ := toastPage(30, 4)
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()
	toast := host.Show("saved")
	c.Frame()

	host.Dismiss(toast)
	_, painted := c.Frame()
	// The restore pass forces everything the vacated rect intersects:
	// the covered content leaf plus the two chrome-less containers
	// (Canvas and host) whose bounds span it — a container's forced
	// evaluation paints no cells, but it is counted, honestly.
	if painted != 3 {
		t.Fatalf("dismissing painted %d components, want 3 (restored leaf + 2 swept containers)", painted)
	}
	if got := row(c.Cells(), 0); got != strings.Repeat("#", 30) {
		t.Fatalf("row 0 after dismiss = %q — the vacated cells did not restore", got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d components, want 0", painted)
	}
}

func TestToastsStackDownTheCorner(t *testing.T) {
	host, page, _ := toastPage(30, 5)
	c := gooey.NewComposer(page, 30, 5)
	c.Frame()
	host.Show("first")
	host.Show("second")
	c.Frame()
	if got := row(c.Cells(), 0); !strings.HasSuffix(got, " first") {
		t.Fatalf("row 0 = %q, want the oldest toast", got)
	}
	if got := row(c.Cells(), 1); !strings.HasSuffix(got, " second") {
		t.Fatalf("row 1 = %q, want the newer toast under it", got)
	}
}

// Auto-dismiss is the Timer discipline: the goroutine posts, the UI
// loop dismisses, and stop closes AND joins so no post survives Close.
func TestToastAutoDismissPostsAndJoins(t *testing.T) {
	host, page, _ := toastPage(30, 4)
	d := gooey.NewDispatcher()
	c := gooey.NewComposer(page, 30, 4)
	c.Start(d)
	c.Frame()

	host.ShowFor("gone soon", time.Millisecond)
	c.Frame()
	if got := row(c.Cells(), 0); !strings.HasSuffix(got, " gone soon") {
		t.Fatalf("row 0 = %q, want the toast up", got)
	}
	waitFor(t, "the posted dismissal", func() bool { return d.Pending() > 0 })
	if len(host.toasts) != 1 {
		t.Fatal("the timer dismissed on its own goroutine — it must post instead")
	}
	d.Drain()
	if len(host.toasts) != 0 {
		t.Fatal("draining the dispatcher did not dismiss the toast")
	}
	c.Frame()
	if got := row(c.Cells(), 0); got != strings.Repeat("#", 30) {
		t.Fatalf("row 0 after auto-dismiss = %q", got)
	}

	// Close joins: after it returns, a fresh toast's timer never posts.
	c.Close()
	host.ShowFor("never", time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if d.Pending() != 0 {
		t.Fatal("a toast shown after Close still posted a dismissal")
	}
}

// A sticky toast (non-positive duration) starts no goroutine and stays.
func TestToastStickyStartsNoTimer(t *testing.T) {
	host := &ToastHost{}
	d := gooey.NewDispatcher()
	stop := host.Start(d.Post)
	host.ShowFor("stay", -1)
	time.Sleep(2 * time.Millisecond)
	if d.Pending() != 0 {
		t.Fatal("a sticky toast posted a dismissal")
	}
	stop()
	if len(host.toasts) != 1 {
		t.Fatal("the sticky toast went away")
	}
}

// The zero-value style is reverse video; a host style is applied to
// every toast it shows.
func TestToastCarriesTheHostStyle(t *testing.T) {
	st := render.Style{Fg: render.RGB(1, 2, 3), Bg: render.RGB(9, 9, 9)}
	host, page, _ := toastPage(30, 4)
	host.Style = st
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()
	host.Show("hey")
	c.Frame()
	if got := c.Cells().At(29, 0).Style; got != st {
		t.Fatalf("toast cell style = %+v, want the host style %+v", got, st)
	}
}
