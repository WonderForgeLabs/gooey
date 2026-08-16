// Package demomain holds the two pieces of scaffolding every cmd/ demo
// used to repeat, and neither of which is teaching anything about the
// framework: finding the directory a demo's markup lives in, and turning
// a -mode flag into a graphics encoder.
//
// The bar for hiding code from a demo is that reading it inline teaches
// the reader nothing they came for. Both of these clear it. "Stat a path,
// else fall back to the executable's directory" is an answer to "where
// did the shell start me", not to anything about fs.FS as a loading seam
// — the seam is markup.Page's parameter, and that is still spelled out at
// every call site. Likewise "-mode=sixel means graphics.Sixel{}" is flag
// parsing; what the demos are actually showing is what the App does with
// a forced encoder, which stays visible in the Option list.
//
// It is deliberately NOT a place for the App loop, the page context, or
// anything a demo exists to demonstrate. If a helper here would ever make
// a demo shorter by making it less legible, it does not belong here.
package demomain

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/term"
)

// MarkupDir returns the directory holding demo cmd/<name>'s markup, where
// page is the file that must be in it (usually "<name>.gooey", but
// cmd/cards loads "dashboard.gooey"). Three ways a demo gets started,
// three candidates, in the order that keeps each one unambiguous:
//
//  1. cmd/<name> — `go run ./cmd/<name>` from the module root, the way
//     every demo is documented;
//  2. the current directory — `cd cmd/<name> && go run .`;
//  3. the executable's own directory — a binary built with `go build -o`
//     and run from anywhere else, which is the only case the first two
//     cannot reach and the whole reason this is not a constant.
//
// A miss on all three returns the executable's directory anyway, so the
// caller's markup.Load reports a missing page rather than this reporting
// a missing directory.
func MarkupDir(name, page string) string {
	if dir := filepath.Join("cmd", name); exists(dir, page) {
		return dir
	}
	if exists(".", page) {
		return "."
	}
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}

// MarkupFS is MarkupDir as an fs.FS, which is the form markup.Load,
// markup.Page and markup.Watch all take.
func MarkupFS(name, page string) fs.FS { return os.DirFS(MarkupDir(name, page)) }

func exists(dir, page string) bool {
	_, err := os.Stat(filepath.Join(dir, page))
	return err == nil
}

// EncoderFor resolves a -mode flag value to a graphics encoder. forced
// says whether the demo should pin the protocol instead of probing for
// it; the empty mode is the only one that leaves the decision to the
// terminal's capabilities.
//
// A nil encoder means two different things depending on forced, which is
// why there are two results and not one. Unforced, nil means "nobody has
// decided yet, go probe". Forced, nil means the cell tier was chosen on
// purpose — no pixel plane, everything drawn in halfblocks and box runes
// — which is a real answer, and the one you want when checking that a
// demo still reads on a terminal with no graphics protocol at all.
//
// That last mode has two names. cmd/pixels and cmd/typeahead document it
// as "halfblock" (what Image falls back to), cmd/toolkit as "cells" (what
// its pixel chrome falls back to), and both spellings were shipped in
// those demos' package docs and --help output. Neither name is wrong for
// the demo that chose it, so both are accepted here rather than breaking
// either one's documented flag.
func EncoderFor(mode string) (enc graphics.Encoder, forced bool, err error) {
	switch mode {
	case "":
		return nil, false, nil // capabilities decide
	case "kitty":
		return graphics.Kitty{}, true, nil
	case "sixel":
		return graphics.Sixel{}, true, nil
	case "iterm2":
		return graphics.ITerm2{}, true, nil
	case "halfblock", "cells":
		return nil, true, nil
	}
	return nil, false, fmt.Errorf("unknown -mode %q: want kitty, sixel, iterm2, or halfblock (also spelled cells)", mode)
}

// GraphicsOptions returns the App options implied by an EncoderFor
// result. Callers append their own — cmd/pixels adds WithoutMouse — but
// the graphics half is identical in every demo that has the flag.
//
// The assumed cell size is the part worth knowing. Forcing a protocol
// skips the probe, and the probe is the only thing that can measure a
// cell in pixels; sixel scales its output by that size, and a zero CellW
// emits an empty image while Image skips the halfblock path entirely — a
// black screen with no error. So a forced mode assumes a common 10x20.
func GraphicsOptions(enc graphics.Encoder, forced bool) []gooey.Option {
	if !forced {
		return []gooey.Option{gooey.WithCapabilityProbe()}
	}
	return []gooey.Option{
		gooey.WithGraphics(enc),
		gooey.WithCaps(term.Caps{CellW: 10, CellH: 20, Color: term.DetectColorDepth()}),
	}
}
