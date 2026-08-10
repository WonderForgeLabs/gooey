package nethandlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/WonderForgeLabs/gooey"
	nethandlers "github.com/WonderForgeLabs/gooey/handlers/net"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
)

func init() { markup.RegisterHandlers(nethandlers.URI, nethandlers.New()) }

const page = `<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:net="gooey.dev/handlers/net">
  <VStack>
    <Button Name="fetch" Content="fetch" Click="{{net:Get .Url | into .Body}}"/>
    <Text>{{.Body}}</Text>
  </VStack>
</Gooey>`

type harness struct {
	t    *testing.T
	body *prop.Property[string]
	url  *prop.Property[string]
	disp *gooey.Dispatcher
	comp *gooey.Composer
}

// build loads the page with a live dispatcher and returns the pieces a
// test drives: the URL it fetches and the property the result lands in.
func build(t *testing.T, src string) *harness {
	t.Helper()
	h := &harness{
		t:    t,
		body: prop.NewSource("(nothing yet)"),
		url:  prop.NewSource(""),
		disp: gooey.NewDispatcher(),
	}
	ctx := &markup.Context{
		Values:     map[string]any{"Url": h.url, "Body": h.body},
		Dispatcher: h.disp,
	}
	w, err := markup.Load(fstest.MapFS{"page.gooey": {Data: []byte(src)}}, "page.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	h.comp = gooey.NewComposer(w, 60, 6)
	return h
}

// click presses the focused button and pumps the dispatcher until the
// async completion arrives — the loop's job, done by hand.
func (h *harness) clickAndSettle() {
	h.t.Helper()
	if !h.comp.HandleKey(input.Named(input.KeyEnter)) {
		h.t.Fatal("enter did not reach the focused button")
	}
	deadline := time.After(5 * time.Second)
	select {
	case <-h.disp.Wake():
		if n := h.disp.Drain(); n == 0 {
			h.t.Fatal("woke with nothing to drain")
		}
	case <-deadline:
		h.t.Fatal("handler never delivered a result")
	}
}

func TestGetDeliversBodyIntoTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello from the server"))
	}))
	defer srv.Close()

	h := build(t, page)
	h.url.Set(srv.URL)
	h.clickAndSettle()

	if got := h.body.Get(); got != "hello from the server" {
		t.Fatalf("body=%q, want the response body", got)
	}
}

// The result has to reach the screen through the ordinary graph: the
// Text bound to {{.Body}} repaints because it read the property the
// handler Set, with nothing in the provider knowing a Text exists.
func TestGetResultRepaintsTheBoundWidget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("PAYLOAD"))
	}))
	defer srv.Close()

	h := build(t, page)
	h.url.Set(srv.URL)
	h.comp.Frame() // paint everything once

	h.clickAndSettle()
	frame, painted := h.comp.Frame()
	if painted != 1 {
		t.Fatalf("repainted %d widgets, want exactly the bound Text", painted)
	}
	if out := frameString(frame, 60, 6); !strings.Contains(out, "PAYLOAD") {
		t.Fatalf("result never reached the screen:\n%s", out)
	}
}

// The argument is a handle, not a snapshot: whatever .Url holds when the
// button is clicked is what gets fetched.
func TestUrlArgumentIsReadAtInvokeTime(t *testing.T) {
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		w.Write([]byte("at " + r.URL.Path))
	}))
	defer srv.Close()

	h := build(t, page)
	h.url.Set(srv.URL + "/first")
	h.clickAndSettle()
	if got := h.body.Get(); got != "at /first" {
		t.Fatalf("body=%q, want %q", got, "at /first")
	}

	h.url.Set(srv.URL + "/second")
	h.clickAndSettle()
	if got := h.body.Get(); got != "at /second" {
		t.Fatalf("body=%q after re-pointing .Url, want %q", got, "at /second")
	}
	if len(hits) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(hits))
	}
}

func TestErrorsLandInTheSameTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusTeapot)
	}))
	defer srv.Close()

	// A server started and immediately closed leaves an address that
	// reliably refuses — unlike a low port number, which some hosts
	// silently filter into a long timeout instead.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	cases := map[string]struct{ url, want string }{
		"http status":     {srv.URL, "418"},
		"bad scheme":      {"ftp://example.invalid/x", "http and https"},
		"unreachable":     {deadURL + "/never", "ERROR:"},
		"malformed input": {"://", "ERROR:"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := build(t, page)
			h.url.Set(tc.url)
			h.clickAndSettle()
			got := h.body.Get()
			if !strings.HasPrefix(got, "ERROR:") || !strings.Contains(got, tc.want) {
				t.Fatalf("body=%q, want an ERROR containing %q", got, tc.want)
			}
		})
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]struct{ src, want string }{
		"undeclared prefix": {
			`<Gooey><Button Content="x" Click="{{net:Get .Url | into .Body}}"/></Gooey>`,
			"undeclared namespace prefix",
		},
		"unregistered uri": {
			`<Gooey xmlns:nope="gooey.dev/handlers/nope"><Button Content="x" Click="{{nope:Get .Url | into .Body}}"/></Gooey>`,
			"no registered handler provider",
		},
		"unknown function": {
			`<Gooey xmlns:net="gooey.dev/handlers/net"><Button Content="x" Click="{{net:Post .Url | into .Body}}"/></Gooey>`,
			"unknown function",
		},
		"wrong arity": {
			`<Gooey xmlns:net="gooey.dev/handlers/net"><Button Content="x" Click="{{net:Get .Url .Body | into .Body}}"/></Gooey>`,
			"takes 1 argument",
		},
		"missing target": {
			`<Gooey xmlns:net="gooey.dev/handlers/net"><Button Content="x" Click="{{net:Get .Url}}"/></Gooey>`,
			"needs a result target",
		},
		"unbound argument": {
			`<Gooey xmlns:net="gooey.dev/handlers/net"><Button Content="x" Click="{{net:Get .Missing | into .Body}}"/></Gooey>`,
			"not found in context",
		},
		"unbound target": {
			`<Gooey xmlns:net="gooey.dev/handlers/net"><Button Content="x" Click="{{net:Get .Url | into .Missing}}"/></Gooey>`,
			"not found in context",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := &markup.Context{
				Values: map[string]any{
					"Url": prop.NewSource(""), "Body": prop.NewSource(""),
				},
				Dispatcher: gooey.NewDispatcher(),
			}
			_, err := markup.Load(fstest.MapFS{"page.gooey": {Data: []byte(tc.src)}}, "page.gooey", ctx)
			if err == nil {
				t.Fatalf("expected a load error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// A document using handler namespaces without a Dispatcher cannot
// deliver anything safely, so it fails at load rather than at click.
func TestMissingDispatcherIsALoadError(t *testing.T) {
	ctx := &markup.Context{Values: map[string]any{
		"Url": prop.NewSource(""), "Body": prop.NewSource(""),
	}}
	_, err := markup.Load(fstest.MapFS{"page.gooey": {Data: []byte(page)}}, "page.gooey", ctx)
	if err == nil || !strings.Contains(err.Error(), "Dispatcher") {
		t.Fatalf("err=%v, want a complaint about the missing Dispatcher", err)
	}
}

func frameString(f *gooey.Frame, cols, rows int) string {
	var sb strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			sb.WriteRune(f.Cells.At(x, y).Rune)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
