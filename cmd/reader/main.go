// reader is the first multi-UserControl gooey app: a three-pane
// RSS/Atom reader. The shell (reader.gooey) instantiates three
// UserControls — FeedList, StoryList, ReaderPane — each a .gooey file
// with its own context; data crosses control boundaries only through
// attribute bindings resolved in the page context. All four markup
// files hot-reload.
//
// Input is the framework's: panes are focus stops, j/k/↑/↓ are handled
// by whichever pane has focus, and enter/q/esc/a are <KeyBinding>s
// declared in markup and bound to viewmodel commands.
//
//	tab       cycle pane focus          j/k or ↑/↓  move in focused pane
//	enter     open story (marks read)   a           add feed URL (writes feeds.opml)
//	q         quit
//
// The mouse is additive, never required: click a pane to focus it, click
// a row to select it, click it again to open it, wheel to move.
package main

import (
	_ "embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

//go:embed default.opml
var defaultOPML []byte

const opmlPath = "feeds.opml"

func main() {
	// --- feed list from OPML (write embedded default on first run) ---
	opml, err := os.ReadFile(opmlPath)
	if err != nil {
		opml = defaultOPML
		os.WriteFile(opmlPath, defaultOPML, 0o644)
	}
	urls, err := parseOPML(opml)
	if err != nil || len(urls) == 0 {
		fmt.Fprintln(os.Stderr, "bad feeds.opml:", err)
		os.Exit(1)
	}

	// --- page viewmodel ---
	initial := make([]*Feed, len(urls))
	for i, u := range urls {
		initial[i] = &Feed{Title: shortHost(u), URL: u, Loading: true}
	}
	feeds := prop.NewSource(initial)
	selFeed := prop.NewSource(0)
	selStory := prop.NewSource(0)
	read := prop.NewSource(map[string]bool{})
	inputMode := prop.NewSource(false)
	draft := prop.NewSource("")
	opened := prop.NewSource("") // link of the story opened in the reader

	stories := prop.NewComputed(func() []Story {
		fs := feeds.Get()
		if len(fs) == 0 {
			return nil
		}
		return fs[clampIdx(selFeed.Get(), len(fs))].Stories
	})
	current := prop.NewComputed(func() *Story {
		link := opened.Get()
		for _, s := range stories.Get() {
			if s.Link == link {
				return &s
			}
		}
		return nil
	})
	status := prop.NewComputed(func() string {
		if inputMode.Get() {
			return "add feed url: " + draft.Get() + "█   (enter=add  esc=cancel)"
		}
		loading := 0
		for _, f := range feeds.Get() {
			if f.Loading {
				loading++
			}
		}
		s := "tab: pane   j/k: move   enter: open   a: add feed   q: quit"
		if loading > 0 {
			s += fmt.Sprintf("   fetching %d…", loading)
		}
		return s
	})

	// --- commands: the delegates markup events bind to ---
	var comp *gooey.Composer
	var readerBody gooey.Component
	quit := false
	commands := map[string]any{
		"Quit":       gooey.Command(func() { quit = true }),
		"AddFeed":    gooey.Command(func() { inputMode.Set(true) }),
		"ResetStory": gooey.Command(func() { selStory.Set(0) }),
		"OpenStory": gooey.Command(func() {
			ss := stories.Get()
			if len(ss) == 0 {
				return
			}
			s := ss[clampIdx(selStory.Get(), len(ss))]
			m := map[string]bool{}
			for k, v := range read.Get() {
				m[k] = v
			}
			m[s.Link] = true
			read.Set(m)
			opened.Set(s.Link)
			comp.Focus().SetFocus(readerBody)
		}),
	}

	// --- markup: page context + UserControl registrations ---
	dir := "cmd/reader"
	if _, err := os.Stat(filepath.Join(dir, "reader.gooey")); err != nil {
		exe, _ := os.Executable()
		dir = filepath.Dir(exe)
	}
	fsys := os.DirFS(dir)

	pageCtx := func() *markup.Context {
		vals := map[string]any{
			"Feeds": feeds, "SelFeed": selFeed,
			"Stories": stories, "SelStory": selStory, "Read": read,
			"Current": current, "Status": status,
		}
		for k, v := range commands {
			vals[k] = v
		}
		return &markup.Context{
			Values: vals,
			Styles: map[string]render.Style{
				"panel": {Fg: render.RGB(120, 90, 220)},
				"dim":   dim,
			},
			Components: map[string]markup.Builder{
				"FeedList":   feedListControl(fsys),
				"StoryList":  storyListControl(fsys),
				"ReaderPane": readerPaneControl(fsys, func(w gooey.Component) { readerBody = w }),
			},
		}
	}
	tree, err := markup.Load(fsys, "reader.gooey", pageCtx())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	screen, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "no tty:", err)
		os.Exit(1)
	}
	cols, rows := screen.Size()

	needsFrame := true
	attach := func(w gooey.Component) {
		// A reload rebuilds the components, so focus is restored by position
		// in the traversal rather than by identity.
		at := 0
		if comp != nil {
			for i, o := range comp.Focus().Order() {
				if o == comp.Focus().Focused() {
					at = i
				}
			}
		}
		comp = gooey.NewComposer(w, cols, rows)
		if order := comp.Focus().Order(); at < len(order) {
			comp.Focus().SetFocus(order[at])
		}
		comp.OnInvalidate(func() { needsFrame = true })
		needsFrame = true
	}
	attach(tree)

	swaps := make(chan gooey.Component, 1)
	stopWatch := markup.WatchAll(fsys,
		[]string{"reader.gooey", "feedlist.gooey", "storylist.gooey", "readerpane.gooey"},
		func() {
			if w, err := markup.Load(fsys, "reader.gooey", pageCtx()); err == nil {
				swaps <- w
			}
		})
	defer stopWatch()

	// --- fetch: goroutine per feed, results applied on the UI loop ---
	type fetched struct {
		idx  int
		feed *Feed
	}
	results := make(chan fetched, len(urls)+4)
	for i, u := range urls {
		go func(i int, u string) { results <- fetched{i, fetchFeed(u)} }(i, u)
	}
	// An index past the current list marks a newly added feed.
	addFeed := func(u string) {
		idx := len(feeds.Get()) + 1_000_000
		go func() { results <- fetched{idx, fetchFeed(u)} }()
	}

	if err := screen.Raw(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer screen.Restore()
	screen.EnableMouse()

	events := make(chan input.Event, 64)
	go term.DecodeEvents(screen, events)

	// dispatch is the root of the input chain. The add-feed prompt is a
	// modal capture of the KEYBOARD only: while it is up every key is
	// text and never reaches the tree, but pointer events still route
	// normally, so hover and focus stay live.
	dispatch := func(ev input.Event) {
		if ev.IsMouse() {
			comp.HandleMouse(ev.Mouse)
			return
		}
		k := ev.Key
		if !inputMode.Get() {
			comp.HandleKey(k)
			return
		}
		switch {
		case k == input.Named(input.KeyEnter):
			if u := strings.TrimSpace(draft.Get()); validURL(u) {
				addFeed(u)
			}
			inputMode.Set(false)
			draft.Set("")
		case k == input.Named(input.KeyEsc) || k == ctrlC:
			inputMode.Set(false)
			draft.Set("")
		case k == input.Named(input.KeyBackspace):
			if s := draft.Get(); s != "" {
				draft.Set(s[:len(s)-1])
			}
		case k.Key == input.KeyRune && k.Mods == 0:
			draft.Set(draft.Get() + string(k.Rune))
		}
	}

	for !quit {
		if needsFrame {
			comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case w := <-swaps:
			attach(w)
		case r := <-results:
			fs := append([]*Feed(nil), feeds.Get()...)
			if r.idx < len(fs) {
				fs[r.idx] = r.feed
			} else { // freshly added feed
				fs = append(fs, r.feed)
				writeOPML(opmlPath, fs)
			}
			feeds.Set(fs)
		case ev := <-events:
			// Any-motion tracking reports every cell the pointer crosses;
			// only the latest position matters, so a queued burst of moves
			// collapses into the last one. Anything else pulled out of the
			// queue is dispatched in order.
		coalesce:
			for ev.IsMove() {
				select {
				case next := <-events:
					if !next.IsMove() {
						dispatch(ev)
					}
					ev = next
				default:
					break coalesce
				}
			}
			dispatch(ev)
		}
	}
}

var ctrlC = input.KeyEvent{Key: input.KeyRune, Rune: 'c', Mods: input.ModCtrl}

func shortHost(u string) string {
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		return strings.TrimPrefix(p.Host, "www.")
	}
	return u
}

func validURL(u string) bool {
	p, err := url.Parse(u)
	return err == nil && (p.Scheme == "http" || p.Scheme == "https") && p.Host != ""
}
