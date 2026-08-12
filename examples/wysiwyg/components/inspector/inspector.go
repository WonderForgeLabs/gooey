// Package inspector is the editor's property grid, as a markup-only
// control.
//
// Like the palette it is a pure function of editor state and has no
// code-behind. Unlike the palette it OWNS THE ONLY INPUT in the editor,
// which is why its position in the tree is a contract rather than a
// preference — see the comment at the top of inspector.gooey.
package inspector

import (
	"io/fs"

	"github.com/WonderForgeLabs/gooey/markup"
)

// File is the pane's markup, relative to the editor root.
const File = "components/inspector/inspector.gooey"

// Builder registers the pane as
// <Inspector Items= Sel= Activate= EditName= EditValue= Commit= Doc=/>.
// The FS comes from the host so the markup hot reloads from disk.
func Builder(fsys fs.FS) markup.Builder { return markup.Include(fsys, File) }
