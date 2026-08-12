// Package palette is the editor's element palette, as a markup-only
// control.
//
// It has no code-behind on purpose. The pane is a pure function of
// editor state — a list, a selection index and an activation command in,
// a rendered list out — so there is nothing for a setup func to own.
// Three of the editor's four panes are like this; the preview is the one
// that is not, because it HOSTS rather than displays.
//
// The .gooey file next to this one is the whole control.
package palette

import (
	"io/fs"

	"github.com/WonderForgeLabs/gooey/markup"
)

// File is the pane's markup, relative to the editor root.
const File = "components/palette/palette.gooey"

// Builder registers the pane as <Palette Items= Sel= Activate=/>.
//
// It takes the FS rather than embedding one, which is the whole point:
// the editor hands it os.DirFS(root), so editing the .gooey on disk hot
// reloads the running pane. An embed.FS would compile the markup into
// the binary and make every pane edit a rebuild — the same fs.FS seam
// that lets a release build swap in embed.FS without touching this line.
//
// markup.Include rather than markup.UserControl: with <x:Property>
// declarations present an Include already gets a type-checked surface,
// and the difference between the tiers is only whether a code-behind
// runs. Adding an empty one would claim behavior that is not there.
func Builder(fsys fs.FS) markup.Builder { return markup.Include(fsys, File) }
