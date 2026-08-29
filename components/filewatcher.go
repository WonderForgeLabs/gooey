package components

import (
	"hash"
	"hash/fnv"
	"io/fs"
	"strconv"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
)

// DefaultWatchInterval is the poll period a FileWatcher uses when its
// Interval is left at zero. It is markup.Watch's 300ms, which is the
// number the framework's own hot reload has always used.
const DefaultWatchInterval = 300 * time.Millisecond

// FileWatcher is Timer's other half: a non-visual element that runs a
// Command when a file or a directory changes. Like KeyBinding and Timer
// it lives in the tree as an attachment — declared where it belongs,
// never measured, arranged, or painted.
//
//	<FileWatcher Paths="{{.Sources}}" Changed="{{.Reload}}" Path="{{.Hit}}"/>
//
// Five hand-rolled versions of this existed before it (markup.Watch,
// markup.WatchAll, the browser's watchKey, the intro deck's counter
// stamp, and a shell script), and no two agreed on what a change is.
// What follows is what this one promises, because a watcher whose
// contract is vague is a watcher you re-implement.
//
// # A DIRECTORY IS NOT WATCHED BY ITS ModTime
//
// A directory's own mtime moves when an entry is added or removed and
// NOT when one is edited, so watching a directory by ModTime silently
// misses every edit — the exact failure a watcher exists to prevent, and
// the reason cmd/browser stopped using ModTime. Worse, and measured
// rather than assumed: run an add and a remove back to back and the
// directory's mtime does not move for THOSE either, because they land
// inside one timestamp tick. So the ModTime route misses the edit
// always, and misses the add and the remove whenever anything happens
// quickly — which is what a save burst is. So a path that resolves
// to a directory is fingerprinted from its ENTRIES: every name, size and
// mtime folded into one number. A path that resolves to a file is
// fingerprinted from its own size and mtime. A path that resolves to
// nothing folds as a distinct absent state, which is what makes "watch a
// file that does not exist yet" and "watch a file that was deleted and
// recreated" both ordinary changes rather than special cases.
//
// The walk is one level deep. Watching a directory reports changes to
// its direct entries, including edits to the files in it; to follow a
// subtree, name its directories. A recursive walk of a source tree three
// times a second is a cost this cannot impose on every page that wants
// to notice a save.
//
// # ONE HIT PER POLL
//
// Coalescing is by STATE COMPARISON over the poll window, not by event
// counting: however many watched paths changed however many times
// between two polls, Changed runs once. That is the structural advantage
// of polling, and it is why an editor's rename-and-replace save is
// normally one hit rather than three — the temp file's arrival and its
// rename onto the target are two states inside one window, and only the
// endpoints are compared. The residual is honest: a poll that lands
// inside the rename window sees the intermediate state, and that save
// produces two hits. At the default interval that window is on the order
// of a millisecond in three hundred.
//
// Every path's fingerprint is refreshed on the poll that reports a
// change, including the paths after the one reported. Stopping at the
// first (which is what WatchAll does, break and all) would leave the
// others looking changed on the NEXT poll and turn one save of three
// files into three hits.
//
// # THE BASELINE IS TAKEN AT Start, ON THE UI GOROUTINE
//
// markup.Watch takes its baseline inside its own goroutine, so a write
// that races the launch is swallowed with nothing to say it was. Here
// the first scan is synchronous inside Start, which the Composer calls
// on the UI goroutine: everything true of the filesystem when Start
// returns is the baseline, and everything after it is a change. That is
// a line an author can reason about — and it is the same rule for a path
// that joins a bound Paths list later, whose current state is recorded
// silently rather than reported as a change. You changed the list; you
// do not need to be told.
//
// # CONFINEMENT
//
// The poll goroutine never touches the property graph. It cannot even
// read Paths: it asks the UI goroutine for the list by posting a
// closure, and scans whatever comes back. Enabled is read at fire time,
// on the loop, exactly as Timer reads it — a nil Enabled means always.
// Because Enabled gates the HIT and not the poll, a change that happens
// while disabled advances the baseline and is dropped; re-enabling
// resumes and does not replay.
//
// Lifetime is owned by the Composer. Hot reload builds a new tree and
// the old Composer's Close stops its watchers, which is what keeps a
// replaced tree from polling on behalf of a viewmodel nobody is showing.
type FileWatcher struct {
	gooey.Base

	// FS is what the paths are resolved against — os.DirFS in dev,
	// embed.FS in release, and the loader cannot tell the difference.
	// This is the same seam markup.Load uses, and it is load-bearing:
	// embed.FS reports a constant zero ModTime for every file, so a
	// watcher over one is a natural no-op and the same page works in
	// both tiers. A nil FS is a watcher declining to start.
	FS fs.FS

	// Paths is the set to watch, read on the UI goroutine only. An
	// empty list is legal and inert — a page may bind a list that has
	// not been filled in yet.
	Paths *prop.Property[[]string]

	// Interval is the poll period; zero means DefaultWatchInterval.
	Interval time.Duration

	// Changed runs on the UI goroutine after a poll saw a change. Nil is
	// a watcher declining to start.
	Changed gooey.Action

	// Path receives the path that caused this hit, Set immediately
	// before Changed runs and on the same goroutine, so the handler's
	// Get sees it. Without it every consumer re-stats all N paths to
	// find out which one fired, which is what WatchAll's callers do.
	// When one poll saw several paths change, this is the first of them
	// in Paths order.
	Path *prop.Property[string]

	// Enabled is read at fire time, on the loop. Nil means always.
	// Binding it to the same property a checkbox toggles pauses the
	// watcher with nothing torn down.
	Enabled *prop.Property[bool]
}

func (w *FileWatcher) Measure(gooey.Size) gooey.Size { return gooey.Size{} }
func (w *FileWatcher) Render(*gooey.Frame)           {}
func (w *FileWatcher) NonVisual() bool               { return true }

// Start polls until the returned stop func is called. A nil FS, a nil
// Changed or a nil Paths handle makes it inert rather than an error: a
// watcher with nothing to do is a legal thing to declare while building
// a page, the same courtesy Timer extends to a missing Tick.
//
// A Paths handle that is merely EMPTY still starts, because a bound list
// can fill in later; the goroutine then asks, scans nothing, and posts
// nothing.
//
// # Why this does not use gooey.Every
//
// Every posts fn onto the UI goroutine on a tick, and fn is where the
// work happens. That is the wrong shape here twice over: the whole point
// of a watcher is to keep filesystem I/O — a ReadDir over a directory
// that may be large, cold, or on a network mount — OFF the render loop,
// and Every would post a closure per tick forever where this posts one
// per actual change. Delays is a group of one-shot deadlines and fits
// less well still. So this hand-rolls its channels, like Companion.Start
// and for the same class of reason, and pays the debt Every exists to
// collect: the stop func CLOSES AND JOINS, so Close ⇒ no further posts
// is a fact and not a probability.
func (w *FileWatcher) Start(post func(func())) func() {
	if post == nil || w.FS == nil || w.Changed == nil || w.Paths == nil {
		return func() {}
	}
	every := w.Interval
	if every <= 0 {
		every = DefaultWatchInterval
	}

	// The baseline, synchronously, on the caller's goroutine — which is
	// the UI goroutine, because the Composer starts its Startables
	// there. This is the one property read outside a posted closure and
	// it is legal for exactly that reason.
	last := map[string]uint64{}
	scan(w.FS, w.paths(), last)

	done := make(chan struct{})
	stopped := make(chan struct{})
	// Buffered so the closure below never blocks the UI goroutine, and
	// so a reply that arrives after stop has nowhere to jam.
	reply := make(chan []string, 1)
	fsys := w.FS

	go func() {
		defer close(stopped)
		tk := time.NewTicker(every)
		defer tk.Stop()
		for {
			select {
			case <-done:
				return
			case <-tk.C:
			}
			// Ask the loop for the path list. This goroutine may not
			// read Paths itself, and it may not block waiting for an
			// answer either: Composer.Close runs on the UI goroutine, so
			// a join against a goroutine parked on a post the loop will
			// never drain is a deadlock. done wins that select.
			post(func() {
				select {
				case reply <- w.paths():
				default:
				}
			})
			var paths []string
			select {
			case <-done:
				return
			case paths = <-reply:
			}
			hit, ok := scan(fsys, paths, last)
			if !ok {
				continue
			}
			post(func() { w.fire(hit) })
		}
	}()
	return func() { close(done); <-stopped }
}

// paths reads the bound list. UI goroutine only. The Get is outside any
// evaluation, so it is a plain read and subscribes nothing — which is
// what we want: a watcher is not a paint node and has no damage to
// declare.
func (w *FileWatcher) paths() []string {
	if w.Paths == nil {
		return nil
	}
	return w.Paths.Get()
}

// fire runs on the UI goroutine, having been posted there.
func (w *FileWatcher) fire(path string) {
	// Enabled first: prop.Set does not compare, so setting Path while
	// disabled would repaint every component reading it for a hit that
	// is not going to happen.
	if w.Enabled != nil && !w.Enabled.Get() {
		return
	}
	if w.Path != nil {
		w.Path.Set(path)
	}
	if gooey.CanExecute(w.Changed) {
		w.Changed.Run()
	}
}

// scan fingerprints every path and folds the result into last, reporting
// the FIRST path whose fingerprint moved.
//
// Two things here are load-bearing and neither is visible from the
// signature. It walks every path even after finding a change, because
// stopping early leaves the rest stale and they fire on the next poll —
// that is the coalescing. And a path with no previous entry is recorded
// SILENTLY: a path that has just joined the list, or the whole list on
// the baseline scan, has no "before" to have changed from.
//
// Pure and synchronous: it takes an fs.FS and a map, so the whole
// change-detection contract is testable without a goroutine, a ticker or
// a dispatcher anywhere near it.
func scan(fsys fs.FS, paths []string, last map[string]uint64) (string, bool) {
	var hit string
	var ok bool
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		if seen[p] {
			continue // a duplicate is one path, and one hit
		}
		seen[p] = true
		h := fingerprint(fsys, p)
		prev, known := last[p]
		last[p] = h
		if known && prev != h && !ok {
			hit, ok = p, true
		}
	}
	// Drop the paths that left the list, so re-adding one baselines it
	// afresh rather than firing on however much it moved while nobody
	// was watching.
	for p := range last {
		if !seen[p] {
			delete(last, p)
		}
	}
	return hit, ok
}

// fingerprint reduces one path's observable state to a number.
//
// The three cases are deliberately distinguishable from each other, not
// just from themselves: a path that is absent, a path that is a file and
// a path that is a directory fold under different domain tags, so a file
// replaced by a directory of the same name is a change.
func fingerprint(fsys fs.FS, path string) uint64 {
	h := fnv.New64a()
	st, err := fs.Stat(fsys, path)
	if err != nil {
		// Absent — or unreadable, or not a path this FS will accept.
		// One state, and moving out of it is a change.
		h.Write([]byte("\x00absent\x00"))
		h.Write([]byte(path))
		return h.Sum64()
	}
	if !st.IsDir() {
		h.Write([]byte("\x00file\x00"))
		fold(h, path, st.Size(), st.ModTime().UnixNano())
		return h.Sum64()
	}
	entries, err := fs.ReadDir(fsys, path)
	if err != nil {
		h.Write([]byte("\x00dirunreadable\x00"))
		h.Write([]byte(path))
		return h.Sum64()
	}
	// ReadDir sorts by filename, so the fold is order-independent for
	// the same directory contents however the OS enumerated them.
	h.Write([]byte("\x00dir\x00"))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			// A subdirectory contributes its NAME only. Its own mtime
			// moves for reasons this level does not care about, and its
			// contents are a level down, which is not walked.
			fold(h, name+"/", 0, 0)
			continue
		}
		info, err := e.Info()
		if err != nil {
			// Removed between the listing and the stat: fold the fact
			// rather than skipping, or the removal is invisible.
			fold(h, name, -1, -1)
			continue
		}
		fold(h, name, info.Size(), info.ModTime().UnixNano())
	}
	return h.Sum64()
}

// fold writes one entry into the hash, through a scratch buffer rather
// than a formatted string: this runs several times a second for as long
// as the page is up, and the alternative is one allocation per file per
// poll with no upper bound on the size of the directory.
func fold(h hash.Hash64, name string, size, mod int64) {
	buf := make([]byte, 0, len(name)+40)
	buf = append(buf, name...)
	buf = append(buf, 0)
	buf = strconv.AppendInt(buf, size, 36)
	buf = append(buf, 0)
	buf = strconv.AppendInt(buf, mod, 36)
	h.Write(buf)
}
