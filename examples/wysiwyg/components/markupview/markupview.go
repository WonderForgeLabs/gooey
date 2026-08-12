// Package markupview is the editor's markup-output pane: the document
// outline and the source the editor would write, as a markup-only
// control.
//
// The package is markupview and not markup because every file that
// registers it also imports github.com/WonderForgeLabs/gooey/markup.
package markupview

import (
	"io/fs"

	"github.com/WonderForgeLabs/gooey/markup"
)

// File is the pane's markup, relative to the editor root.
const File = "components/markupview/markupview.gooey"

// Builder registers the pane as <MarkupView Tree= Source=/>. The FS comes
// from the host so the markup hot reloads from disk.
func Builder(fsys fs.FS) markup.Builder { return markup.Include(fsys, File) }
