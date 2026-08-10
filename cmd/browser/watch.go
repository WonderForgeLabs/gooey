package main

// Everything the browser shows is derived from the filesystem: the two
// group listings, the ⟨n .gooey⟩ badges, the ● / ○ / ▶ markers, the doc
// comment or README in the preview pane, and which GIF `p` would play.
// So the poll that keeps it fresh has to cover everything those are
// derived FROM, not just the directory that happens to hold the demos.
//
// The mechanism is a fingerprint rather than a modification time. A
// directory's own mtime moves when a file is added or removed but NOT
// when one is edited, which would leave a corrected doc comment invisible
// until the next restart — the exact failure a watcher is supposed to
// prevent. Folding every entry's name, size and mtime into one hash
// catches edits, additions, removals and re-recordings alike, and
// reduces to a single uint64 the poll can compare.
//
// One rescan is the whole reaction: `demos` is a computed over a
// revision source, so bumping it re-derives the list AND every pane
// bound to it, including the one on screen.

import (
	"hash"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// watchInterval is fast enough that a recording finished in another
// terminal shows up before you look for it, and slow enough that the
// stat traffic is invisible.
const watchInterval = 1500 * time.Millisecond

// watchKey fingerprints every directory the UI reads.
func watchKey(root string) uint64 {
	h := fnv.New64a()
	buf := make([]byte, 0, 256)
	for _, r := range roots {
		dir := filepath.Join(root, filepath.FromSlash(r.path))
		buf = foldDir(h, dir, "", buf)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				buf = foldDir(h, filepath.Join(dir, e.Name()), "", buf)
			}
		}
	}
	buf = foldDir(h, filepath.Join(root, recDir), "", buf)
	// The module root is watched for GIFs only. It also holds build
	// output, and rebuilding a demo binary is not a reason to rescan.
	foldDir(h, root, ".gif", buf)
	return h.Sum64()
}

// foldDir folds one directory listing into h. suffix filters entries
// (empty means all); a directory that does not exist folds as its own
// distinct state, so creating recordings/ registers as a change.
//
// buf is threaded through and reused: this runs every 1.5 s forever, and
// the alternative is a formatted string per file per tick.
func foldDir(h hash.Hash64, dir, suffix string, buf []byte) []byte {
	entries, err := os.ReadDir(dir)
	if err != nil {
		buf = append(buf[:0], "<absent>"...)
		buf = append(buf, dir...)
		h.Write(buf)
		return buf
	}
	for _, e := range entries {
		name := e.Name()
		if suffix != "" && !strings.HasSuffix(name, suffix) {
			continue
		}
		buf = append(buf[:0], name...)
		if info, err := e.Info(); err == nil {
			buf = strconv.AppendInt(buf, info.ModTime().UnixNano(), 36)
			buf = strconv.AppendInt(buf, info.Size(), 36)
		}
		h.Write(buf)
	}
	return buf
}
