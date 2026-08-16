package demomain

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/WonderForgeLabs/gooey/graphics"
)

// TestMarkupDirFindsEachStart pins the three ways a demo gets started.
// The third is the one that cannot be checked by eye: a refactor that
// drops the executable fallback still compiles, still passes every test
// that runs from the module root, and only fails once somebody runs the
// built binary from somewhere else.
func TestMarkupDirFindsEachStart(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cmd", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(dir string) {
		if err := os.WriteFile(filepath.Join(dir, "demo.gooey"), []byte("<Text/>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "cmd", "demo"))

	t.Run("module root", func(t *testing.T) {
		t.Chdir(root)
		if got, want := MarkupDir("demo", "demo.gooey"), filepath.Join("cmd", "demo"); got != want {
			t.Errorf("MarkupDir = %q, want %q", got, want)
		}
	})

	t.Run("demo directory", func(t *testing.T) {
		t.Chdir(filepath.Join(root, "cmd", "demo"))
		if got := MarkupDir("demo", "demo.gooey"); got != "." {
			t.Errorf("MarkupDir = %q, want %q", got, ".")
		}
	})

	// Nowhere near the source: the answer has to be the directory the
	// running binary sits in, which under `go test` is the test binary's.
	t.Run("executable directory", func(t *testing.T) {
		t.Chdir(t.TempDir())
		exe, err := os.Executable()
		if err != nil {
			t.Skip("no os.Executable on this platform:", err)
		}
		if got, want := MarkupDir("demo", "demo.gooey"), filepath.Dir(exe); got != want {
			t.Errorf("MarkupDir = %q, want %q", got, want)
		}
	})

	// A page name that is not the demo name — cmd/cards loads
	// dashboard.gooey — resolves off the page, not off the directory.
	// Asserting the exact answer, not merely "not cmd/demo": a lookup
	// that stopped consulting the page at all would return cmd/demo and
	// fail either way, but one that returned "." would slip past the
	// weaker check while sending every demo to the wrong root.
	t.Run("page unlike name", func(t *testing.T) {
		t.Chdir(root)
		exe, err := os.Executable()
		if err != nil {
			t.Skip("no os.Executable on this platform:", err)
		}
		if got, want := MarkupDir("demo", "dashboard.gooey"), filepath.Dir(exe); got != want {
			t.Errorf("MarkupDir = %q, want %q (cmd/demo holds no dashboard.gooey)", got, want)
		}
	})
}

// TestEncoderForAliases pins the reconciliation: "halfblock" (cmd/pixels,
// cmd/typeahead) and "cells" (cmd/toolkit) are the same forced cell tier,
// and both stay spellable.
func TestEncoderForAliases(t *testing.T) {
	for _, tc := range []struct {
		mode   string
		enc    graphics.Encoder
		forced bool
		bad    bool
	}{
		{mode: "", enc: nil, forced: false},
		{mode: "kitty", enc: graphics.Kitty{}, forced: true},
		{mode: "sixel", enc: graphics.Sixel{}, forced: true},
		{mode: "iterm2", enc: graphics.ITerm2{}, forced: true},
		{mode: "halfblock", enc: nil, forced: true},
		{mode: "cells", enc: nil, forced: true},
		{mode: "nonsense", bad: true},
	} {
		enc, forced, err := EncoderFor(tc.mode)
		if tc.bad {
			if err == nil {
				t.Errorf("EncoderFor(%q): want an error", tc.mode)
			}
			continue
		}
		if err != nil {
			t.Errorf("EncoderFor(%q): %v", tc.mode, err)
			continue
		}
		if enc != tc.enc || forced != tc.forced {
			t.Errorf("EncoderFor(%q) = %v, %v; want %v, %v", tc.mode, enc, forced, tc.enc, tc.forced)
		}
	}
}

// TestGraphicsOptionsCount is a shape check, not a behaviour one: the
// probe path is one option and the forced path is two, and the second of
// those is the assumed cell size without which sixel emits an empty image.
func TestGraphicsOptionsCount(t *testing.T) {
	if got := len(GraphicsOptions(nil, false)); got != 1 {
		t.Errorf("unforced: %d options, want 1 (the probe)", got)
	}
	if got := len(GraphicsOptions(graphics.Sixel{}, true)); got != 2 {
		t.Errorf("forced: %d options, want 2 (encoder + caps)", got)
	}
}
