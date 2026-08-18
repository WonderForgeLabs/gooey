package demomain

import (
	"os"
	"path/filepath"
	"strings"
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
		enc, forced, err := EncoderFor("-mode", tc.mode)
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

// TestGraphicsOptionsCount is a shape check, not a behaviour one: one
// option on each path.
//
// The forced path used to be two, the second a hand-carried
// `WithCaps{CellW: 10, CellH: 20}`. That is App.caps' rule, and this was
// a second copy of it with nothing holding the two equal — identical
// while both said 10x20, drift the day either moved (#322).
//
// The rule itself is NOT asserted here, and deliberately so: this package
// cannot observe it (`options` is unexported, and there is no public caps
// accessor), and it is already pinned where it lives, by
// TestPinnedProtocolGetsACellSize in graphicsopt_test.go — including the
// `{"halfblock pinned", WithGraphics(nil), 0}` case, which is why a
// forced halfblock now reports `cell 0x0` rather than a 10x20 nothing
// measured. Asserting a count here and the rule there is the split that
// keeps each claim next to the code that can be wrong about it.
func TestGraphicsOptionsCount(t *testing.T) {
	if got := len(GraphicsOptions(nil, false)); got != 1 {
		t.Errorf("unforced: %d options, want 1 (the probe)", got)
	}
	if got := len(GraphicsOptions(graphics.Sixel{}, true)); got != 1 {
		t.Errorf("forced: %d options, want 1 (the encoder; App.caps supplies the metrics)", got)
	}
}

// TestEncoderForErrorNamesTheCallersFlag pins the reason EncoderFor takes
// a flag name at all. The message hardcoded `-mode`, so cmd/colors —
// whose flag is `--graphics` — rejected a bad value by naming a flag that
// binary does not have (#322).
//
// Checked per caller spelling rather than once, because "it contains the
// string I passed" is also true of a function that ignores the argument
// and happens to hardcode the same word.
func TestEncoderForErrorNamesTheCallersFlag(t *testing.T) {
	for _, flagName := range []string{"-mode", "--graphics"} {
		_, _, err := EncoderFor(flagName, "nonsense")
		if err == nil {
			t.Fatalf("EncoderFor(%q, \"nonsense\"): want an error", flagName)
		}
		if !strings.Contains(err.Error(), flagName) {
			t.Errorf("EncoderFor(%q, ...) error %q does not name the caller's flag, "+
				"so a demo reports a flag it does not have", flagName, err)
		}
	}
	// The discriminating half: the OTHER spelling must be absent, or a
	// message naming both flags would satisfy the check above.
	_, _, err := EncoderFor("--graphics", "nonsense")
	if err != nil && strings.Contains(err.Error(), "-mode ") {
		t.Errorf("the --graphics error still mentions -mode: %v", err)
	}
}
