// reader is the first multi-UserControl gooey app: a three-pane
// RSS/Atom reader. The shell (reader.gooey) instantiates three
// UserControls — FeedList, StoryList, ReaderPane — each a .gooey file
// with its own context; data crosses control boundaries only through
// attribute bindings resolved in the page context. All four markup
// files hot-reload.
//
// Input is the framework's: panes are focus stops, j/k/↑/↓ are handled
// by whichever pane has focus, and enter/q/esc/a are <KeyBinding>s
// declared in markup and bound to viewmodel commands. The add-feed
// prompt is a <TextBox> in the same markup, with its rules declared as a
// <Validate> behavior — the run loop below routes events and never reads
// one.
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
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/cmd/internal/demomain"
	"github.com/WonderForgeLabs/gooey/components"
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
	// The two halves of the bottom row. Browsing/Prompting drive the
	// Visibility swap in markup; Fetching is the only part of the old
	// status string that was ever dynamic, and it is now a section of a
	// <StatusBar> rather than a suffix on a sentence built in Go.
	browsing := prop.NewComputed(func() bool { return !inputMode.Get() })
	fetching := prop.NewComputed(func() string {
		loading := 0
		for _, f := range feeds.Get() {
			if f.Loading {
				loading++
			}
		}
		if loading == 0 {
			return ""
		}
		return fmt.Sprintf("fetching %d…", loading)
	})

	// --- markup: page context + UserControl registrations ---
	fsys := demomain.MarkupFS("reader", "reader.gooey")

	// ONE context for the life of the app, rebound on every reload. It
	// has to be one: <Validate> publishes the field's error handle INTO
	// this map at load time (.DraftErr below), so a context discarded per
	// load would take the handle the commit reads with it.
	var comp *gooey.Composer
	var readerBody gooey.Component
	quit := false
	ctx := &markup.Context{
		Values: map[string]any{
			"Feeds": feeds, "SelFeed": selFeed,
			"Stories": stories, "SelStory": selStory, "Read": read,
			"Current": current,
			"Draft":   draft, "Browsing": browsing, "Prompting": inputMode,
			"Fetching": fetching,
		},
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

	// closePrompt hands focus back to whichever pane the prompt took it
	// from. PreviouslyFocused is exactly that component — focus moves
	// record it — and it reports nil once a reload has replaced the tree,
	// which is the case worth not guessing at.
	// Focus first, then collapse: a focus stop inside a Collapsed subtree
	// is unreachable, so moving focus out while the field is still on
	// screen is what keeps the caret somewhere that exists.
	closePrompt := func() {
		if prev := comp.Focus().PreviouslyFocused(); prev != nil {
			comp.Focus().SetFocus(prev)
		}
		inputMode.Set(false)
		draft.Set("")
	}
	// draftErr reads the handle <Validate> published. The lookup is
	// inside the command rather than resolved once, because the property
	// does not exist until the page has loaded and a hot reload publishes
	// a fresh one — the same rule docs/learn/examples/howto-forms follows.
	draftErr := func() string {
		p, ok := ctx.Values["DraftErr"].(*prop.Property[string])
		if !ok {
			// Only reachable if the markup lost its <Validate>; refusing
			// beats adding a URL nothing checked.
			return "no validator"
		}
		return p.Get()
	}

	var addFeed func(string)
	ctx.Values["Quit"] = gooey.Command(func() { quit = true })
	ctx.Values["ResetStory"] = gooey.Command(func() { selStory.Set(0) })
	ctx.Values["StayPut"] = gooey.Command(func() {})
	ctx.Values["AddFeed"] = gooey.Command(func() {
		inputMode.Set(true)
		// Focus AFTER the field is visible — the other half of the rule in
		// closePrompt: SetFocus on a Collapsed component would put the
		// caret somewhere nothing is drawn.
		if box, err := markup.Find[*components.TextBox](ctx, "AddFeed"); err == nil {
			comp.Focus().SetFocus(box)
		}
	})
	ctx.Values["CancelFeed"] = gooey.Command(closePrompt)
	ctx.Values["CommitFeed"] = gooey.Command(func() {
		if draftErr() == "" {
			addFeed(strings.TrimSpace(draft.Get()))
		}
		closePrompt()
	})
	ctx.Values["OpenStory"] = gooey.Command(func() {
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
	})

	tree, err := markup.Load(fsys, "reader.gooey", ctx)
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

	// The watcher reports only THAT a file changed. Building the
	// replacement tree resolves bindings and creates computeds, which is
	// touching the property graph, and the watcher runs on its own
	// goroutine — so the rebuild happens below, on the UI loop.
	reloads := make(chan struct{}, 1)
	stopWatch := markup.WatchAll(fsys,
		[]string{"reader.gooey", "feedlist.gooey", "storylist.gooey", "readerpane.gooey"},
		func() {
			select {
			case reloads <- struct{}{}:
			default: // one pending rebuild is enough
			}
		})
	defer stopWatch()

	reload := func() {
		// Names address components, and a rebuild makes new ones: the old
		// map would answer <Validate>'s and AddFeed's lookups with
		// components that are no longer on screen.
		ctx.Named = map[string]gooey.Component{}
		if w, err := markup.Load(fsys, "reader.gooey", ctx); err == nil {
			attach(w) // a bad edit keeps the running tree
		}
	}

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
	addFeed = func(u string) {
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

	// dispatch is the root of the input chain, and it is now only
	// routing: the add-feed prompt is a focus stop like any other, so
	// there is no keyboard mode here to check.
	dispatch := func(ev input.Event) {
		if ev.IsMouse() {
			comp.HandleMouse(ev.Mouse)
			return
		}
		comp.HandleKey(ev.Key)
	}

	for !quit {
		if needsFrame {
			comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case <-reloads:
			reload()
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

func shortHost(u string) string {
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		return strings.TrimPrefix(p.Host, "www.")
	}
	return u
}
