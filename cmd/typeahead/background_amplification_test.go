package main

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
)

// A finding, not a feature — and the reason the shipped page has no
// Background on its root Grid.
//
// A template that draws its own selection has to make something appear
// and disappear per row, and the markup way to do that is
// Visibility="{{._selected}}". A Visibility flip is not free the way a
// style change is: the Composer clears the vacated rectangle and then
// force-repaints everything UNDER it (Composer.restoreUnder), because
// whatever the marker was covering has to come back.
//
// That much is bounded — seven components. The amplification is what
// comes next. One of the nodes restored under a marker in the middle of
// the screen is the ROOT, since its bounds are the screen; and a
// container that declares a Background is `covered`, which makes the
// z-ordered pass force its whole subtree to repaint above it. So a
// two-cell marker appearing on one row repaints the entire page.
//
// Seven versus forty-eight, for the same keystroke and the same visible
// result. Neither number is wrong — both follow from rules this codebase
// states out loud — but nothing warns you, and the trigger is an
// aesthetic attribute on an element nowhere near the list.
const ampPage = `<Gooey>
  <Grid Rows="1,*,1" BACKGROUND>
    <Text Grid.Row="0" Style="accent">shelf</Text>
    <ItemsView Grid.Row="1" Items="{{.Records}}" Selected="{{.Sel}}">
      <TypeAhead Key="Title" Search="{{.Typed}}" NoMatch="{{.Missed}}"/>
      <ItemsView.ItemTemplate>
        <Grid Rows="4" Cols="2,9,*">
          <Text Grid.Col="0" Style="accent" Visibility="{{._selected}}">{{.Bar}}</Text>
          <Image Grid.Col="1" Src="{{.Cover}}" Cols="8" Rows="4" HAlign="Start" VAlign="Start"/>
          <VStack Grid.Col="2">
            <Text Style="title">{{.Title}}</Text>
            <Text Style="dim">{{.Artist}}</Text>
          </VStack>
        </Grid>
      </ItemsView.ItemTemplate>
    </ItemsView>
    <Text Grid.Row="2" Style="dim">{{.Search}}</Text>
  </Grid>
</Gooey>`

// ampHop builds the page with or without the root Background and returns
// what one in-window selection hop costs.
func ampHop(t *testing.T, background string) int {
	t.Helper()
	src := strings.Replace(ampPage, "BACKGROUND", background, 1)
	m := newModel()
	root, err := markup.Load(fstest.MapFS{"amp.gooey": &fstest.MapFile{Data: []byte(src)}}, "amp.gooey", m.ctx())
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(root, cols, rows)
	c.Focus().Resync()
	c.Frame()
	c.Frame()
	if _, n := c.Frame(); n != 0 {
		t.Fatalf("not at rest: %d components still repainting", n)
	}
	c.HandleKey(input.Rune('a')) // Aftertone -> Alabaster, one row down
	_, n := c.Frame()
	if m.sel.Get() != 1 {
		t.Fatalf("selection moved to %d, want 1 — the hop must stay in the window", m.sel.Get())
	}
	return n
}

func TestABackgroundAncestorAmplifiesTheSelectionMarker(t *testing.T) {
	plain := ampHop(t, "")
	if plain != 7 {
		t.Fatalf("without a Background, a selection hop repainted %d components, want 7", plain)
	}
	filled := ampHop(t, `Background="#141420"`)
	if filled != 48 {
		t.Fatalf("with a Background, a selection hop repainted %d components, want 48", filled)
	}
	if filled <= plain {
		t.Fatalf("no amplification: %d with a background vs %d without", filled, plain)
	}
}
