package main

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/cmd/internal/demomain"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// The reader is four markup files and three UserControl setups, and every
// binding in them resolves at LOAD time — an unknown name is an error
// there, not a surprise on click. Nothing tested that, so adding an
// attribute to <ReaderPane> could have broken the page with the whole
// suite still green.
//
// This loads the real page against a context carrying the same names
// main() publishes. It is a key-complete map on purpose: a name valued
// with an empty property must still EXIST, because the resolver checks
// presence, not usefulness.
func TestReaderPageLoads(t *testing.T) {
	feeds := prop.NewSource([]*Feed{{Title: "example", URL: "https://example.test/f"}})
	stories := prop.NewComputed(func() []Story { return feeds.Get()[0].Stories })
	read := prop.NewSource(map[string]bool{})

	var body gooey.Component
	ctx := &markup.Context{
		Values: map[string]any{
			"Feeds":         feeds,
			"SelFeed":       prop.NewSource(0),
			"Stories":       stories,
			"SelStory":      prop.NewSource(0),
			"Read":          read,
			"Current":       prop.NewComputed(func() *Story { return nil }),
			"ArticleScroll": prop.NewSource(0),
			"Draft":         prop.NewSource(""),
			"Browsing":      prop.NewComputed(func() bool { return true }),
			"Prompting":     prop.NewSource(false),
			"Fetching":      prop.NewComputed(func() string { return "" }),
			"Quit":          gooey.Command(func() {}),
			"ResetStory":    gooey.Command(func() {}),
			"StayPut":       gooey.Command(func() {}),
			"AddFeed":       gooey.Command(func() {}),
			"CancelFeed":    gooey.Command(func() {}),
			"CommitFeed":    gooey.Command(func() {}),
			"OpenStory":     gooey.Command(func() {}),
		},
		Styles: map[string]render.Style{"panel": {}, "dim": dim},
	}
	fsys := demomain.MarkupFS("reader", "reader.gooey")
	ctx.Components = map[string]markup.Builder{
		"FeedList":   feedListControl(fsys),
		"StoryList":  storyListControl(fsys),
		"ReaderPane": readerPaneControl(fsys, func(w gooey.Component) { body = w }),
	}

	tree, err := markup.Load(fsys, "reader.gooey", ctx)
	if err != nil {
		t.Fatalf("the reader page no longer loads: %v", err)
	}
	if body == nil {
		t.Fatal("the reader pane never reported its article body; the page cannot move focus into it")
	}
	pane, ok := body.(*articleBody)
	if !ok {
		t.Fatalf("the reader pane's body is %T, want *articleBody", body)
	}
	// The point of the whole test: the Scroll attribute crossed the
	// UserControl boundary and landed on the pane's scroll model. Without
	// it the pane would load fine and never scroll.
	if pane.scroll.Offset == nil {
		t.Fatal("the article pane has no scroll offset; the Scroll attribute did not cross the control boundary")
	}
	if pane.scroll.Offset != ctx.Values["ArticleScroll"] {
		t.Fatal("the article pane bound some other property than the page's ArticleScroll")
	}
	gooey.NewComposer(tree, 80, 24).Frame()
}
