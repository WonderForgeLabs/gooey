package fshandlers_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/WonderForgeLabs/gooey"
	fshandlers "github.com/WonderForgeLabs/gooey/handlers/fs"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
)

// pageRead is the canonical read page; the other pages vary only the
// expression under test.
const pageRead = `<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:fs="gooey.dev/handlers/fs">
  <VStack>
    <Button Name="go" Content="go" Click="{{fs:Read .Path | into .Out}}"/>
    <Text>{{.Out}}</Text>
  </VStack>
</Gooey>`

func pageWith(expr string) string {
	return strings.Replace(pageRead, "{{fs:Read .Path | into .Out}}", expr, 1)
}

type harness struct {
	t    *testing.T
	path *prop.Property[string]
	text *prop.Property[string]
	out  *prop.Property[string]
	disp *gooey.Dispatcher
	comp *gooey.Composer
}

// build registers p as THE fs grant (tests run sequentially; each test
// installs its own root), loads src, and returns the driving pieces.
func build(t *testing.T, p *fshandlers.Provider, src string) *harness {
	t.Helper()
	markup.RegisterHandlers(fshandlers.URI, p)
	t.Cleanup(func() { p.Close() })
	h := &harness{
		t:    t,
		path: prop.NewSource(""),
		text: prop.NewSource(""),
		out:  prop.NewSource("(nothing yet)"),
		disp: gooey.NewDispatcher(),
	}
	ctx := &markup.Context{
		Values:     map[string]any{"Path": h.path, "Text": h.text, "Out": h.out},
		Dispatcher: h.disp,
	}
	w, err := markup.Load(fstest.MapFS{"page.gooey": {Data: []byte(src)}}, "page.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	h.comp = gooey.NewComposer(w, 60, 6)
	return h
}

// clickAndSettle presses the focused button and pumps the dispatcher
// until the async completion arrives — the loop's job, done by hand.
func (h *harness) clickAndSettle() {
	h.t.Helper()
	if !h.comp.HandleKey(input.Named(input.KeyEnter)) {
		h.t.Fatal("enter did not reach the focused button")
	}
	deadline := time.After(5 * time.Second)
	select {
	case <-h.disp.Wake():
		if n := h.disp.Drain(); n == 0 {
			h.t.Fatal("woke with nothing to drain")
		}
	case <-deadline:
		h.t.Fatal("handler never delivered a result")
	}
}

func TestReadDeliversFileIntoTarget(t *testing.T) {
	fsys := fstest.MapFS{"docs/hello.txt": {Data: []byte("hello from the fs")}}
	h := build(t, fshandlers.New(fsys), pageRead)
	h.path.Set("docs/hello.txt")
	h.clickAndSettle()
	if got := h.out.Get(); got != "hello from the fs" {
		t.Fatalf("out=%q, want the file contents", got)
	}
}

// The result reaches the screen through the ordinary graph: the Text
// bound to {{.Out}} repaints because it read the property the handler
// Set, with nothing in the provider knowing a Text exists.
func TestReadResultRepaintsTheBoundComponent(t *testing.T) {
	fsys := fstest.MapFS{"a.txt": {Data: []byte("PAYLOAD")}}
	h := build(t, fshandlers.New(fsys), pageRead)
	h.path.Set("a.txt")
	h.comp.Frame() // paint everything once

	h.clickAndSettle()
	frame, painted := h.comp.Frame()
	if painted != 1 {
		t.Fatalf("repainted %d components, want exactly the bound Text", painted)
	}
	if out := frameString(frame, 60, 6); !strings.Contains(out, "PAYLOAD") {
		t.Fatalf("result never reached the screen:\n%s", out)
	}
}

// The argument is a handle, not a snapshot: whatever .Path holds when
// the button is pressed is what gets read.
func TestPathArgumentIsReadAtInvokeTime(t *testing.T) {
	fsys := fstest.MapFS{
		"first.txt":  {Data: []byte("one")},
		"second.txt": {Data: []byte("two")},
	}
	h := build(t, fshandlers.New(fsys), pageRead)
	h.path.Set("first.txt")
	h.clickAndSettle()
	if got := h.out.Get(); got != "one" {
		t.Fatalf("out=%q, want %q", got, "one")
	}
	h.path.Set("second.txt")
	h.clickAndSettle()
	if got := h.out.Get(); got != "two" {
		t.Fatalf("out=%q after re-pointing .Path, want %q", got, "two")
	}
}

// Every shape of escape is rejected with the same typed text: the
// grant's extent is the fs.FS, and no path may name outside it.
func TestEscapeAttemptsAreRejected(t *testing.T) {
	fsys := fstest.MapFS{"ok.txt": {Data: []byte("fine")}}
	cases := map[string]string{
		"parent traversal":   "../secret",
		"embedded traversal": "a/../../b",
		"absolute path":      "/etc/passwd",
		"rooted current":     "./ok.txt",
		"empty path":         "",
		"trailing slash":     "docs/",
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			h := build(t, fshandlers.New(fsys), pageRead)
			h.path.Set(path)
			h.clickAndSettle()
			got := h.out.Get()
			if !strings.HasPrefix(got, "ERROR:") || !strings.Contains(got, "not a valid path") {
				t.Fatalf("out=%q, want a not-a-valid-path ERROR", got)
			}
		})
	}
}

// A literal escape does not even load: the path is known at build time,
// so it fails at build time.
func TestLiteralEscapeIsALoadError(t *testing.T) {
	markup.RegisterHandlers(fshandlers.URI, fshandlers.New(fstest.MapFS{}))
	ctx := &markup.Context{
		Values:     map[string]any{"Out": prop.NewSource("")},
		Dispatcher: gooey.NewDispatcher(),
	}
	src := pageWith("{{fs:Read `../etc/passwd` | into .Out}}")
	_, err := markup.Load(fstest.MapFS{"page.gooey": {Data: []byte(src)}}, "page.gooey", ctx)
	if err == nil || !strings.Contains(err.Error(), "not a valid path") {
		t.Fatalf("err=%v, want a load error naming the invalid path", err)
	}
}

func TestReadSizeCap(t *testing.T) {
	fsys := fstest.MapFS{
		"small.txt": {Data: []byte("12345678")},
		"big.txt":   {Data: []byte("123456789")},
	}
	h := build(t, fshandlers.New(fsys, fshandlers.WithMaxRead(8)), pageRead)

	h.path.Set("small.txt")
	h.clickAndSettle()
	if got := h.out.Get(); got != "12345678" {
		t.Fatalf("out=%q, want the at-cap file delivered whole", got)
	}

	h.path.Set("big.txt")
	h.clickAndSettle()
	got := h.out.Get()
	if !strings.HasPrefix(got, "ERROR:") || !strings.Contains(got, "8-byte cap") {
		t.Fatalf("out=%q, want an over-cap ERROR naming the cap", got)
	}
}

func TestListDeliversEntriesJSON(t *testing.T) {
	mod := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	fsys := fstest.MapFS{
		"docs/a.txt":     {Data: []byte("aaaa"), ModTime: mod},
		"docs/sub/b.txt": {Data: []byte("b")},
	}
	h := build(t, fshandlers.New(fsys), pageWith("{{fs:List .Path | into .Out}}"))
	h.path.Set("docs")
	h.clickAndSettle()

	var entries []struct {
		Name    string `json:"name"`
		Size    int64  `json:"size"`
		Dir     bool   `json:"dir"`
		ModTime string `json:"modTime"`
	}
	if err := json.Unmarshal([]byte(h.out.Get()), &entries); err != nil {
		t.Fatalf("out=%q is not JSON: %v", h.out.Get(), err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(entries), entries)
	}
	if e := entries[0]; e.Name != "a.txt" || e.Size != 4 || e.Dir || e.ModTime != "2026-08-10T12:00:00Z" {
		t.Fatalf("a.txt entry wrong: %+v", e)
	}
	if e := entries[1]; e.Name != "sub" || !e.Dir || e.ModTime != "" {
		t.Fatalf("sub entry wrong (zero mod time must render empty): %+v", e)
	}
}

func TestStatDeliversOneEntry(t *testing.T) {
	mod := time.Date(2026, 8, 10, 9, 30, 0, 0, time.UTC)
	fsys := fstest.MapFS{"spec.md": {Data: []byte("hello"), ModTime: mod}}
	h := build(t, fshandlers.New(fsys), pageWith("{{fs:Stat .Path | into .Out}}"))
	h.path.Set("spec.md")
	h.clickAndSettle()

	var e struct {
		Name    string `json:"name"`
		Size    int64  `json:"size"`
		Dir     bool   `json:"dir"`
		ModTime string `json:"modTime"`
	}
	if err := json.Unmarshal([]byte(h.out.Get()), &e); err != nil {
		t.Fatalf("out=%q is not JSON: %v", h.out.Get(), err)
	}
	if e.Name != "spec.md" || e.Size != 5 || e.Dir || e.ModTime != "2026-08-10T09:30:00Z" {
		t.Fatalf("entry wrong: %+v", e)
	}
}

func TestGlobDeliversPathsJSON(t *testing.T) {
	fsys := fstest.MapFS{
		"a.md":     {Data: []byte("x")},
		"b.md":     {Data: []byte("x")},
		"c.txt":    {Data: []byte("x")},
		"sub/d.md": {Data: []byte("x")},
	}
	h := build(t, fshandlers.New(fsys), pageWith("{{fs:Glob .Path | into .Out}}"))

	h.path.Set("*.md")
	h.clickAndSettle()
	var paths []string
	if err := json.Unmarshal([]byte(h.out.Get()), &paths); err != nil {
		t.Fatalf("out=%q is not JSON: %v", h.out.Get(), err)
	}
	if len(paths) != 2 || paths[0] != "a.md" || paths[1] != "b.md" {
		t.Fatalf("paths=%v, want [a.md b.md]", paths)
	}

	// No matches is an empty JSON array, never null.
	h.path.Set("*.go")
	h.clickAndSettle()
	if got := h.out.Get(); got != "[]" {
		t.Fatalf("out=%q, want [] for no matches", got)
	}
}

func TestWriteAndAppend(t *testing.T) {
	dir := t.TempDir()
	p, err := fshandlers.NewWritable(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := build(t, p, pageWith("{{fs:Write .Path .Text | into .Out}}"))
	h.path.Set("notes.txt")
	h.text.Set("first line\n")
	h.clickAndSettle()
	if got := h.out.Get(); got != "" {
		t.Fatalf("status=%q, want \"\" on success", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "notes.txt")); string(b) != "first line\n" {
		t.Fatalf("on disk: %q", b)
	}

	ha := build(t, p, pageWith("{{fs:Append .Path .Text | into .Out}}"))
	ha.path.Set("notes.txt")
	ha.text.Set("second line\n")
	ha.clickAndSettle()
	if got := ha.out.Get(); got != "" {
		t.Fatalf("status=%q, want \"\" on success", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "notes.txt")); string(b) != "first line\nsecond line\n" {
		t.Fatalf("on disk after append: %q", b)
	}
}

// The writable provider also serves the read functions, through the
// same root.
func TestWritableGrantAlsoReads(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seeded"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := fshandlers.NewWritable(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := build(t, p, pageRead)
	h.path.Set("seed.txt")
	h.clickAndSettle()
	if got := h.out.Get(); got != "seeded" {
		t.Fatalf("out=%q, want the seeded file", got)
	}
}

func TestWriteEscapeAttemptsAreRejected(t *testing.T) {
	dir := t.TempDir()
	p, err := fshandlers.NewWritable(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := build(t, p, pageWith("{{fs:Write .Path .Text | into .Out}}"))
	h.path.Set("../escape.txt")
	h.text.Set("nope")
	h.clickAndSettle()
	got := h.out.Get()
	if !strings.HasPrefix(got, "ERROR:") || !strings.Contains(got, "not a valid path") {
		t.Fatalf("status=%q, want a not-a-valid-path ERROR", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.txt")); !os.IsNotExist(err) {
		t.Fatal("the write escaped the root")
	}
}

// os.Root enforces the boundary below the path grammar too: a symlink
// pointing outside the root refuses to resolve.
func TestSymlinkEscapeIsBlocked(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	p, err := fshandlers.NewWritable(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := build(t, p, pageRead)
	h.path.Set("link.txt")
	h.clickAndSettle()
	got := h.out.Get()
	if !strings.HasPrefix(got, "ERROR:") {
		t.Fatalf("out=%q, want an ERROR — the symlink must not resolve outside the root", got)
	}
}

func TestWriteOnReadOnlyGrantIsALoadError(t *testing.T) {
	markup.RegisterHandlers(fshandlers.URI, fshandlers.New(fstest.MapFS{}))
	for _, fn := range []string{"Write", "Append"} {
		t.Run(fn, func(t *testing.T) {
			src := pageWith("{{fs:" + fn + " .Path .Text | into .Out}}")
			ctx := &markup.Context{
				Values: map[string]any{
					"Path": prop.NewSource(""), "Text": prop.NewSource(""), "Out": prop.NewSource(""),
				},
				Dispatcher: gooey.NewDispatcher(),
			}
			_, err := markup.Load(fstest.MapFS{"page.gooey": {Data: []byte(src)}}, "page.gooey", ctx)
			if err == nil || !strings.Contains(err.Error(), "writable grant") {
				t.Fatalf("err=%v, want a load error naming the missing writable grant", err)
			}
		})
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]struct{ expr, want string }{
		"unknown function": {
			"{{fs:Delete .Path | into .Out}}",
			"unknown function",
		},
		"read-only function list": {
			"{{fs:Delete .Path | into .Out}}",
			"Read, List, Stat, Glob",
		},
		"wrong arity": {
			"{{fs:Read .Path .Text | into .Out}}",
			"takes 1 argument",
		},
		"missing target": {
			"{{fs:Read .Path}}",
			"needs a result target",
		},
		"bad literal pattern": {
			"{{fs:Glob `[` | into .Out}}",
			"not a valid pattern",
		},
		"unbound argument": {
			"{{fs:Read .Missing | into .Out}}",
			"not found in context",
		},
	}
	markup.RegisterHandlers(fshandlers.URI, fshandlers.New(fstest.MapFS{}))
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := &markup.Context{
				Values: map[string]any{
					"Path": prop.NewSource(""), "Text": prop.NewSource(""), "Out": prop.NewSource(""),
				},
				Dispatcher: gooey.NewDispatcher(),
			}
			src := pageWith(tc.expr)
			_, err := markup.Load(fstest.MapFS{"page.gooey": {Data: []byte(src)}}, "page.gooey", ctx)
			if err == nil {
				t.Fatalf("expected a load error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// The arity check on writable functions names both parameters.
func TestWritableLoadErrors(t *testing.T) {
	dir := t.TempDir()
	p, err := fshandlers.NewWritable(dir)
	if err != nil {
		t.Fatal(err)
	}
	markup.RegisterHandlers(fshandlers.URI, p)
	t.Cleanup(func() { p.Close() })
	cases := map[string]struct{ expr, want string }{
		"write arity": {
			"{{fs:Write .Path | into .Out}}",
			"takes 2 arguments",
		},
		"write missing target": {
			"{{fs:Write .Path .Text}}",
			"needs a status target",
		},
		"write literal escape": {
			"{{fs:Write `../x` .Text | into .Out}}",
			"not a valid path",
		},
		"unknown function names writables": {
			"{{fs:Delete .Path | into .Out}}",
			"Write, Append",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := &markup.Context{
				Values: map[string]any{
					"Path": prop.NewSource(""), "Text": prop.NewSource(""), "Out": prop.NewSource(""),
				},
				Dispatcher: gooey.NewDispatcher(),
			}
			src := pageWith(tc.expr)
			_, err := markup.Load(fstest.MapFS{"page.gooey": {Data: []byte(src)}}, "page.gooey", ctx)
			if err == nil {
				t.Fatalf("expected a load error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// AllNames is the pack's invocable inventory as data (the
// pack-distribution record). Pinning the set means a new function
// cannot ship without appearing in it — and via the unknown-function
// error message, which derives from it.
func TestAllNamesPinsTheInventory(t *testing.T) {
	got := fshandlers.AllNames()
	want := []string{
		fshandlers.NameRead, fshandlers.NameList, fshandlers.NameStat,
		fshandlers.NameGlob, fshandlers.NameWrite, fshandlers.NameAppend,
	}
	if len(got) != len(want) {
		t.Fatalf("AllNames()=%v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("AllNames()=%v, want %v", got, want)
		}
	}
	for i, lit := range []string{"Read", "List", "Stat", "Glob", "Write", "Append"} {
		if want[i] != lit {
			t.Fatalf("constant %d is %q, want %q", i, want[i], lit)
		}
	}
}

func frameString(f *gooey.Frame, cols, rows int) string {
	var sb strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			sb.WriteRune(f.Cells.At(x, y).Rune)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
