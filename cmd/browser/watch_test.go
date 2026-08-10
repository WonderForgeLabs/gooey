package main

import (
	"os"
	"path/filepath"
	"testing"
)

// tree builds a miniature module root: one demo, one tutorial example,
// and a recordings directory.
func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{
		filepath.Join("cmd", "demo"),
		filepath.Join("docs", "learn", "examples", "01-first-app"),
		recDir,
	} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, root, filepath.Join("cmd", "demo", "main.go"), "// demo\npackage main\n")
	write(t, root, filepath.Join("docs", "learn", "examples", "01-first-app", "main.go"), "package main\n")
	return root
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWatchKeyIsStableWhenNothingChanges(t *testing.T) {
	root := tree(t)
	if a, b := watchKey(root, root), watchKey(root, root); a != b {
		t.Fatalf("fingerprint is not stable: %d then %d", a, b)
	}
}

// The old watcher compared cmd/'s modification time, which does not move
// when a file inside a demo is edited — a corrected doc comment stayed
// invisible until restart. This is the case that drove the rewrite.
func TestWatchKeyNoticesAnEditedDocComment(t *testing.T) {
	root := tree(t)
	before := watchKey(root, root)
	write(t, root, filepath.Join("cmd", "demo", "main.go"), "// demo, now explained at length\npackage main\n")
	if after := watchKey(root, root); after == before {
		t.Fatal("editing a demo's main.go did not change the fingerprint")
	}
}

func TestWatchKeyNoticesTheThingsTheUIShows(t *testing.T) {
	cases := []struct {
		name string
		mut  func(root string)
	}{
		{"a new recording", func(root string) {
			write(t, root, filepath.Join(recDir, "demo.cast"), "cast")
		}},
		{"a new GIF at the module root", func(root string) {
			write(t, root, "demo.gif", "gif")
		}},
		{"a new checked-in GIF", func(root string) {
			if err := os.MkdirAll(filepath.Join(root, "docs", "media", "demos"), 0o755); err != nil {
				t.Fatal(err)
			}
			write(t, root, filepath.Join("docs", "media", "demos", "demo.gif"), "gif")
		}},
		{"a new markup file", func(root string) {
			write(t, root, filepath.Join("cmd", "demo", "app.gooey"), "<Gooey/>")
		}},
		{"a new README", func(root string) {
			write(t, root, filepath.Join("cmd", "demo", "README.md"), "# demo")
		}},
		{"a new demo", func(root string) {
			if err := os.MkdirAll(filepath.Join(root, "cmd", "other"), 0o755); err != nil {
				t.Fatal(err)
			}
			write(t, root, filepath.Join("cmd", "other", "main.go"), "package main\n")
		}},
		{"an edited tutorial example", func(root string) {
			write(t, root, filepath.Join("docs", "learn", "examples", "01-first-app", "main.go"), "package main // v2\n")
		}},
		{"a removed demo", func(root string) {
			if err := os.RemoveAll(filepath.Join(root, "cmd", "demo")); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := tree(t)
			before := watchKey(root, root)
			tc.mut(root)
			if after := watchKey(root, root); after == before {
				t.Fatalf("%s did not change the fingerprint", tc.name)
			}
		})
	}
}

// Rebuilding a demo drops a binary in the module root. That is not a
// reason to rescan, so the root is watched for GIFs only.
func TestWatchKeyIgnoresBuildOutputAtTheRoot(t *testing.T) {
	root := tree(t)
	before := watchKey(root, root)
	write(t, root, "demo", "ELF...")
	write(t, root, "go.mod", "module x\n")
	if after := watchKey(root, root); after != before {
		t.Fatal("build output at the module root triggered a rescan")
	}
}

// The poll runs forever, so its cost is a property worth pinning. It is
// dominated by the directory reads themselves; the fingerprinting adds
// one reused scratch buffer, not a string per file.
func BenchmarkWatchKey(b *testing.B) {
	root := b.TempDir()
	for i := 0; i < 12; i++ {
		dir := filepath.Join(root, "cmd", string(rune('a'+i)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		for _, f := range []string{"main.go", "app.gooey", "README.md"} {
			if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		watchKey(root, root)
	}
}

func TestWatchKeyToleratesAMissingTree(t *testing.T) {
	// A module with no recordings/ yet, and no docs/, must fingerprint
	// rather than fail — and creating recordings/ must register.
	root := t.TempDir()
	before := watchKey(root, root)
	if err := os.MkdirAll(filepath.Join(root, recDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if after := watchKey(root, root); after == before {
		t.Fatal("creating recordings/ did not change the fingerprint")
	}
}
