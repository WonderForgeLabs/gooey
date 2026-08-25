package components

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
)

// ---------------------------------------------------------------------
// change detection — the pure half
//
// scan takes an fs.FS and a map, so every claim about WHAT a change is
// can be pinned without a goroutine, a ticker or a dispatcher. The
// tests below are the contract; the ones further down are the lifetime.
// ---------------------------------------------------------------------

// stamps are forced rather than observed. Two writes a millisecond apart
// differ in mtime on ext4 and tmpfs and do not on a filesystem with
// one-second resolution, and a test that is right on the machine that
// wrote it is the kind of flake nobody can reproduce.
func write(t *testing.T, dir, name, content string, mod time.Time) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
}

var (
	t1 = time.Unix(1_700_000_000, 0)
	t2 = time.Unix(1_700_000_100, 0)
	t3 = time.Unix(1_700_000_200, 0)
)

func TestScanBaselineReportsNothing(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "one", t1)
	write(t, dir, "sub/b.txt", "two", t1)

	last := map[string]uint64{}
	if hit, ok := scan(os.DirFS(dir), []string{"a.txt", "sub"}, last); ok {
		t.Fatalf("the baseline scan reported %q as a change; nothing has happened yet", hit)
	}
	if len(last) != 2 {
		t.Fatalf("baseline recorded %d stamps, want 2 — a path with no stamp fires on the next poll", len(last))
	}
}

func TestScanReportsAFileEdit(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "one", t1)
	fsys := os.DirFS(dir)
	last := map[string]uint64{}
	scan(fsys, []string{"a.txt"}, last)

	write(t, dir, "a.txt", "two", t2)
	hit, ok := scan(fsys, []string{"a.txt"}, last)
	if !ok || hit != "a.txt" {
		t.Fatalf("edit reported (%q,%v), want (\"a.txt\",true)", hit, ok)
	}
	// And settles: an unchanged file does not keep firing.
	if hit, ok := scan(fsys, []string{"a.txt"}, last); ok {
		t.Fatalf("second scan reported %q with nothing changed", hit)
	}
}

// The case the issue is actually about, and the one ModTime gets wrong.
//
// The assertion has two halves on purpose. Without the second, this test
// would pass just as well against a watcher that compared the
// directory's own ModTime — it would only mean the OS happened to touch
// it — and would therefore prove nothing about the mechanism.
func TestScanReportsAnEditToAFileInsideAWatchedDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "pages/a.txt", "one", t1)
	fsys := os.DirFS(dir)
	last := map[string]uint64{}
	scan(fsys, []string{"pages"}, last)

	before, err := os.Stat(filepath.Join(dir, "pages"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "pages/a.txt", "one more", t2)
	after, err := os.Stat(filepath.Join(dir, "pages"))
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Skipf("this filesystem moves a directory's mtime on an in-place edit (%v -> %v), so the discriminating half of this test cannot run here",
			before.ModTime(), after.ModTime())
	}

	hit, ok := scan(fsys, []string{"pages"}, last)
	if !ok || hit != "pages" {
		t.Fatalf("an edit inside a watched directory reported (%q,%v), want (\"pages\",true) — the directory's own mtime did not move, so ModTime comparison would have missed it", hit, ok)
	}
}

func TestScanReportsEntriesAddedAndRemovedFromAWatchedDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "pages/a.txt", "one", t1)
	fsys := os.DirFS(dir)
	last := map[string]uint64{}
	scan(fsys, []string{"pages"}, last)

	write(t, dir, "pages/b.txt", "two", t1)
	if _, ok := scan(fsys, []string{"pages"}, last); !ok {
		t.Fatal("adding an entry to a watched directory did not report")
	}
	if err := os.Remove(filepath.Join(dir, "pages", "b.txt")); err != nil {
		t.Fatal(err)
	}
	if _, ok := scan(fsys, []string{"pages"}, last); !ok {
		t.Fatal("removing an entry from a watched directory did not report")
	}
}

// A path that does not exist yet and a path deleted and recreated are
// the two cases that separate a working watcher from a demo. Both fall
// out of absence being a state rather than an error.
func TestScanTreatsAbsenceAsAState(t *testing.T) {
	dir := t.TempDir()
	fsys := os.DirFS(dir)
	last := map[string]uint64{}
	if _, ok := scan(fsys, []string{"later.txt"}, last); ok {
		t.Fatal("baselining a path that does not exist reported a change")
	}
	if _, ok := scan(fsys, []string{"later.txt"}, last); ok {
		t.Fatal("a path that still does not exist reported a change")
	}

	write(t, dir, "later.txt", "here", t1)
	if _, ok := scan(fsys, []string{"later.txt"}, last); !ok {
		t.Fatal("a watched path appearing did not report")
	}
	if err := os.Remove(filepath.Join(dir, "later.txt")); err != nil {
		t.Fatal(err)
	}
	if _, ok := scan(fsys, []string{"later.txt"}, last); !ok {
		t.Fatal("a watched path being deleted did not report")
	}
	write(t, dir, "later.txt", "back", t2)
	if _, ok := scan(fsys, []string{"later.txt"}, last); !ok {
		t.Fatal("a watched path being recreated did not report")
	}
}

// THE COALESCING PIN.
//
// Three files change inside one poll window and that is one hit, and —
// the half that actually bites — the NEXT poll is silent. WatchAll
// breaks out of its loop after the first change, which passes the first
// assertion and fails the second: the two files it never restamped fire
// again, one per poll, so one save of three files becomes three hits.
func TestScanCoalescesEveryChangeInOnePollIntoOneHit(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		write(t, dir, n, "one", t1)
	}
	fsys := os.DirFS(dir)
	paths := []string{"a.txt", "b.txt", "c.txt"}
	last := map[string]uint64{}
	scan(fsys, paths, last)

	for _, n := range paths {
		write(t, dir, n, "two", t2)
	}
	hit, ok := scan(fsys, paths, last)
	if !ok {
		t.Fatal("three files changed and the poll reported nothing")
	}
	if hit != "a.txt" {
		t.Errorf("hit = %q, want the first changed path in Paths order", hit)
	}
	if hit, ok := scan(fsys, paths, last); ok {
		t.Fatalf("the poll after a three-file change reported %q — the paths past the first were not restamped, so one save becomes one hit per file", hit)
	}
}

// A rename-and-replace save, staged the way an editor does it: the temp
// file lands, then it is renamed onto the target. Inside one window
// those are two states and only the endpoints are compared, so it is
// one hit — which is the reason this component polls rather than
// counting events.
func TestScanSeesARenameAndReplaceSaveAsOneHit(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "pages/note.md", "one", t1)
	fsys := os.DirFS(dir)
	last := map[string]uint64{}
	scan(fsys, []string{"pages"}, last)

	// Everything an editor does between two polls.
	write(t, dir, "pages/.note.md.swp", "scratch", t2)
	write(t, dir, "pages/note.md.tmp", "one edited", t2)
	if err := os.Rename(filepath.Join(dir, "pages", "note.md.tmp"), filepath.Join(dir, "pages", "note.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "pages", ".note.md.swp")); err != nil {
		t.Fatal(err)
	}

	if _, ok := scan(fsys, []string{"pages"}, last); !ok {
		t.Fatal("a save did not report")
	}
	if hit, ok := scan(fsys, []string{"pages"}, last); ok {
		t.Fatalf("the poll after one save reported %q as a second hit", hit)
	}
}

// You changed the list; you do not need to be told. A path joining a
// bound Paths list has its current state recorded and reports nothing,
// which is the same rule the baseline scan follows.
func TestScanBaselinesAPathThatJoinsTheListSilently(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "one", t1)
	write(t, dir, "b.txt", "two", t1)
	fsys := os.DirFS(dir)
	last := map[string]uint64{}
	scan(fsys, []string{"a.txt"}, last)

	if hit, ok := scan(fsys, []string{"a.txt", "b.txt"}, last); ok {
		t.Fatalf("adding %q to the list reported it as a change", hit)
	}
	// It is watched from then on, though.
	write(t, dir, "b.txt", "two edited", t2)
	if hit, ok := scan(fsys, []string{"a.txt", "b.txt"}, last); !ok || hit != "b.txt" {
		t.Fatalf("edit after joining reported (%q,%v), want (\"b.txt\",true)", hit, ok)
	}
}

// And a path that LEAVES loses its stamp, so re-adding it baselines
// afresh rather than firing on however much it moved while nobody was
// watching.
func TestScanDropsAPathThatLeavesTheList(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "one", t1)
	write(t, dir, "b.txt", "two", t1)
	fsys := os.DirFS(dir)
	last := map[string]uint64{}
	scan(fsys, []string{"a.txt", "b.txt"}, last)

	scan(fsys, []string{"a.txt"}, last)
	if _, still := last["b.txt"]; still {
		t.Fatal("a path that left the list kept its stamp")
	}
	write(t, dir, "b.txt", "changed while unwatched", t2)
	if hit, ok := scan(fsys, []string{"a.txt", "b.txt"}, last); ok {
		t.Fatalf("re-adding a path reported %q — a change made while it was off the list was replayed", hit)
	}
}

// The three fingerprint domains are distinguishable from each other,
// not merely from themselves: a file replaced by a directory of the
// same name is a change even if nothing else about it is comparable.
func TestFingerprintDistinguishesAbsentFromFileFromDirectory(t *testing.T) {
	dir := t.TempDir()
	fsys := os.DirFS(dir)
	absent := fingerprint(fsys, "x")
	write(t, dir, "x", "content", t1)
	asFile := fingerprint(fsys, "x")
	if err := os.Remove(filepath.Join(dir, "x")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "x/inner.txt", "content", t1)
	asDir := fingerprint(fsys, "x")

	if absent == asFile || asFile == asDir || absent == asDir {
		t.Fatalf("absent=%d file=%d dir=%d — two states fold the same, so moving between them is invisible", absent, asFile, asDir)
	}
}

func TestScanTreatsADuplicatePathAsOnePath(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "one", t1)
	fsys := os.DirFS(dir)
	last := map[string]uint64{}
	scan(fsys, []string{"a.txt", "a.txt"}, last)
	if len(last) != 1 {
		t.Fatalf("a duplicated path recorded %d stamps", len(last))
	}
	write(t, dir, "a.txt", "two", t2)
	if _, ok := scan(fsys, []string{"a.txt", "a.txt"}, last); !ok {
		t.Fatal("a duplicated path stopped reporting")
	}
	if hit, ok := scan(fsys, []string{"a.txt", "a.txt"}, last); ok {
		t.Fatalf("a duplicated path fired twice: second poll reported %q", hit)
	}
}

// ---------------------------------------------------------------------
// lifetime
// ---------------------------------------------------------------------

// sink stands in for the UI goroutine, counting what was posted and —
// the whole point — whether anything arrived after stop returned.
//
// It RUNS the closure, unlike the root package's equivalent, because
// this watcher's poll loop asks the loop for its path list and parks
// until the answer comes back. A sink that swallowed the closure would
// park the goroutine on its first tick, and every barrier assertion
// below would be measuring a goroutine that had already stopped posting
// for reasons that have nothing to do with stop.
type sink struct {
	// slow widens the window a post occupies. Without it these tests
	// measure luck: a post is a few instructions, so a stop that failed
	// to join usually returns before the in-flight post lands and the
	// assertion passes against the bug.
	slow time.Duration

	mu     sync.Mutex
	sealed bool
	n      int
	after  int
}

func (s *sink) post(fn func()) {
	if s.slow > 0 {
		time.Sleep(s.slow)
	}
	s.mu.Lock()
	s.n++
	if s.sealed {
		s.after++
	}
	s.mu.Unlock()
	fn()
}

func (s *sink) seal() {
	s.mu.Lock()
	s.sealed = true
	s.mu.Unlock()
}

func (s *sink) counts() (n, after int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n, s.after
}

// drainFor pumps the dispatcher for d, which is how a test says "let
// everything in flight land" to a component whose poll goroutine parks
// waiting for the loop to answer it.
//
// A single Drain is not that. Enabled is read at FIRE time, and fire
// time is drain time — so a hit generated by a poll while the watcher
// was disabled sits in the queue until something drains it, and a test
// that flips Enabled in between delivers it. That is the component's
// contract working (it is Timer's, exactly), and it is only visible if
// the test pumps the queue empty before it touches the property.
func drainFor(disp *gooey.Dispatcher, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		disp.Drain()
		time.Sleep(time.Millisecond)
	}
	disp.Drain()
}

// THE BARRIER PIN. close(done) alone lets a poll that already won its
// select post after Close, and the flake it causes is the one CLAUDE.md
// names as a trap. Repeated, because a single stop-then-check passes
// against a signal-only stop most of the time — which is exactly why
// that bug survives review.
func TestFileWatcherStopIsABarrierNotASignal(t *testing.T) {
	dir := t.TempDir()
	for range 20 {
		s := &sink{slow: 300 * time.Microsecond}
		w := &FileWatcher{
			FS:       os.DirFS(dir),
			Paths:    Strs(nil),
			Interval: 100 * time.Microsecond,
			Changed:  gooey.Command(func() {}),
		}
		stop := w.Start(s.post)
		time.Sleep(2 * time.Millisecond)
		stop()
		s.seal()
		// Give anything that outlived stop a generous chance to be seen.
		time.Sleep(3 * time.Millisecond)
		if _, after := s.counts(); after != 0 {
			t.Fatalf("%d posts arrived after stop returned — stop signalled but did not join", after)
		}
	}
}

// Zero paths is legal and inert: it starts, does nothing, and stops
// cleanly. It still POLLS, because a bound list can fill in later —
// what it must never do is run Changed.
func TestFileWatcherWithZeroPathsStartsDoesNothingAndStopsCleanly(t *testing.T) {
	s := &sink{}
	hits := 0
	w := &FileWatcher{
		FS:       os.DirFS(t.TempDir()),
		Paths:    Strs(nil),
		Interval: time.Millisecond,
		Changed:  gooey.Command(func() { hits++ }),
	}
	stop := w.Start(s.post)
	waitFor(t, "the watcher to poll at all", func() bool { n, _ := s.counts(); return n >= 3 })
	stop()
	if hits != 0 {
		t.Errorf("a watcher with no paths ran Changed %d times", hits)
	}
}

// A watcher missing any of its three prerequisites declines to start:
// no goroutine, and therefore no posts at all. Asserted as an effect
// over many intervals rather than as a goroutine count, which is a
// clock too coarse to say anything.
func TestFileWatcherDeclinesToStartWithoutFSPathsOrChanged(t *testing.T) {
	fsys := os.DirFS(t.TempDir())
	for name, w := range map[string]*FileWatcher{
		"no FS":      {Paths: Strs([]string{"a"}), Changed: gooey.Command(func() {}), Interval: time.Millisecond},
		"no Paths":   {FS: fsys, Changed: gooey.Command(func() {}), Interval: time.Millisecond},
		"no Changed": {FS: fsys, Paths: Strs([]string{"a"}), Interval: time.Millisecond},
	} {
		s := &sink{}
		stop := w.Start(s.post)
		time.Sleep(20 * time.Millisecond) // twenty intervals
		stop()
		if n, _ := s.counts(); n != 0 {
			t.Errorf("%s: an inert watcher posted %d funcs", name, n)
		}
	}
}

func TestFileWatcherDeliversHitsThroughTheDispatcher(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "one", t1)
	d := gooey.NewDispatcher()
	hits := 0
	hit := prop.NewSource("")
	var sawPath string
	w := &FileWatcher{
		FS:       os.DirFS(dir),
		Paths:    Strs([]string{"a.txt"}),
		Path:     hit,
		Interval: time.Millisecond,
		Changed:  gooey.Command(func() { hits++; sawPath = hit.Get() }),
	}
	stop := w.Start(d.Post)
	defer stop()

	write(t, dir, "a.txt", "two", t2)
	waitFor(t, "a hit", func() bool {
		d.Drain()
		return hits > 0
	})
	// Path is Set BEFORE Changed runs and on the same goroutine, so the
	// handler's own Get sees the path that caused this hit. Without that
	// every consumer re-stats all N paths to find out which one fired.
	if sawPath != "a.txt" {
		t.Errorf("the handler read Path = %q, want %q", sawPath, "a.txt")
	}
}

// Nothing runs on the poll goroutine: the hit is posted, not called.
func TestFileWatcherRunsChangedOnlyOnTheLoop(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "one", t1)
	d := gooey.NewDispatcher()
	hits := 0
	w := &FileWatcher{
		FS:       os.DirFS(dir),
		Paths:    Strs([]string{"a.txt"}),
		Interval: time.Millisecond,
		Changed:  gooey.Command(func() { hits++ }),
	}
	stop := w.Start(d.Post)
	defer stop()

	write(t, dir, "a.txt", "two", t2)
	waitFor(t, "something to be posted", func() bool { return d.Pending() > 0 })
	if hits != 0 {
		t.Fatalf("Changed ran %d times before the loop drained — it must not run on the poll goroutine", hits)
	}
}

// Enabled gates the HIT, not the poll, exactly as it does on Timer. So a
// change made while disabled advances the baseline and is dropped, and
// re-enabling resumes without replaying it.
func TestFileWatcherEnabledFalseDropsTheHitAndDoesNotReplay(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "one", t1)
	d := gooey.NewDispatcher()
	hits := 0
	enabled := prop.NewSource(false)
	hit := prop.NewSource("")
	w := &FileWatcher{
		FS:       os.DirFS(dir),
		Paths:    Strs([]string{"a.txt"}),
		Path:     hit,
		Enabled:  enabled,
		Interval: time.Millisecond,
		Changed:  gooey.Command(func() { hits++ }),
	}
	stop := w.Start(d.Post)
	defer stop()

	write(t, dir, "a.txt", "two", t2)
	drainFor(d, 40*time.Millisecond) // forty polls, every one of them drained
	if hits != 0 {
		t.Fatalf("a disabled watcher fired %d times", hits)
	}
	// Path is not moved either: prop.Set does not compare, so setting it
	// for a hit that will not happen repaints every reader for nothing.
	if got := hit.Get(); got != "" {
		t.Errorf("a disabled watcher set Path = %q", got)
	}

	// Re-enabling resumes with nothing torn down, and does NOT replay
	// the edit that happened while it was off.
	enabled.Set(true)
	drainFor(d, 40*time.Millisecond)
	if hits != 0 {
		t.Fatalf("re-enabling replayed %d change(s) made while disabled", hits)
	}
	write(t, dir, "a.txt", "three", t3)
	waitFor(t, "a hit after re-enabling", func() bool {
		d.Drain()
		return hits > 0
	})
}

// THE BASELINE PIN. markup.Watch takes its baseline inside its own
// goroutine, so a write racing the launch is swallowed with nothing to
// say it was. Here the first scan is synchronous inside Start, so the
// line is where an author can see it: everything true when Start
// RETURNS is the baseline, and the very next write is a change.
//
// The interval is deliberately long and the write deliberately
// immediate, and that is what makes this test able to fail. With a
// short interval a baseline taken on the goroutine's FIRST TICK would
// still be earlier than any write a test could manage, so the obvious
// version of this test passes against the bug it is named for. Here the
// first tick is 200ms away: a baseline taken there would fold this
// write in and report nothing, forever.
//
// Against the other spelling of the bug — a baseline taken at the top
// of the goroutine, which is markup.Watch's — this test is a race
// detector rather than a proof, and catches it only some of the time.
// That asymmetry IS the argument for doing it synchronously: a
// contract you can only test probabilistically is one you do not have.
func TestFileWatcherBaselinesSynchronouslyInsideStart(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "one", t1)
	d := gooey.NewDispatcher()
	hits := 0
	w := &FileWatcher{
		FS:       os.DirFS(dir),
		Paths:    Strs([]string{"a.txt"}),
		Interval: 200 * time.Millisecond,
		Changed:  gooey.Command(func() { hits++ }),
	}
	stop := w.Start(d.Post)
	defer stop()
	write(t, dir, "a.txt", "two", t2) // before the first poll can run

	waitFor(t, "a write made between Start and the first poll to report", func() bool {
		d.Drain()
		return hits > 0
	})
}

// And the other half: an unchanged file never fires, however long the
// watcher runs. Without this the test above would pass against a
// watcher that fires on every poll.
func TestFileWatcherDoesNotFireOverAnUnchangedFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "one", t1)
	d := gooey.NewDispatcher()
	hits := 0
	w := &FileWatcher{
		FS:       os.DirFS(dir),
		Paths:    Strs([]string{"a.txt"}),
		Interval: time.Millisecond,
		Changed:  gooey.Command(func() { hits++ }),
	}
	stop := w.Start(d.Post)
	defer stop()

	drainFor(d, 40*time.Millisecond)
	if hits != 0 {
		t.Fatalf("a watcher fired %d times over an unchanged file", hits)
	}
}

// Lifetime belongs to the Composer, like every other Startable: a tree
// built but never composed does not poll, and Close is what keeps a
// replaced tree from watching on behalf of a viewmodel nobody is
// showing.
func TestComposerStartsAndStopsAnAttachedFileWatcher(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "one", t1)
	d := gooey.NewDispatcher()
	hits := 0
	root := &VStack{Children: []gooey.Component{&Text{Content: Str("x")}}}
	root.Attach(&FileWatcher{
		FS:       os.DirFS(dir),
		Paths:    Strs([]string{"a.txt"}),
		Interval: time.Millisecond,
		Changed:  gooey.Command(func() { hits++ }),
	})

	comp := gooey.NewComposer(root, 20, 3)
	write(t, dir, "a.txt", "two", t2)
	time.Sleep(10 * time.Millisecond)
	if n := d.Pending(); n != 0 {
		t.Fatalf("the watcher polled before Start (%d posts) — starting is the composition's job", n)
	}

	comp.Start(d)
	write(t, dir, "a.txt", "three", t3)
	waitFor(t, "a hit after Start", func() bool {
		d.Drain()
		return hits > 0
	})

	comp.Close()
	time.Sleep(10 * time.Millisecond)
	d.Drain()
	before := hits
	write(t, dir, "a.txt", "four", t3.Add(time.Second))
	time.Sleep(30 * time.Millisecond)
	if n := d.Pending(); n != 0 {
		t.Errorf("%d posts arrived after Close — the watcher outlived its composition", n)
	}
	d.Drain()
	if hits != before {
		t.Errorf("the watcher fired %d more times after Close", hits-before)
	}
}

// THE DAMAGE PIN. One file change repaints exactly the components that
// read what it changed. A bounds assertion or a "the cell says X"
// assertion passes just as well when the whole tree repainted, so it
// would prove nothing about damage.
func TestOneFileChangeRepaintsOnlyTheComponentsThatReadIt(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "one", t1)
	d := gooey.NewDispatcher()

	status := prop.NewSource("idle")
	other := prop.NewSource("untouched")
	root := &VStack{Children: []gooey.Component{
		&Text{Content: status},
		&Text{Content: other},
		&Text{Content: Str("static")},
	}}
	root.Attach(&FileWatcher{
		FS:       os.DirFS(dir),
		Paths:    Strs([]string{"a.txt"}),
		Interval: time.Millisecond,
		Changed:  gooey.Command(func() { status.Set("reloaded") }),
	})

	comp := gooey.NewComposer(root, 20, 4)
	comp.Frame()
	comp.Start(d)
	defer comp.Close()

	write(t, dir, "a.txt", "two", t2)
	waitFor(t, "the hit to reach the loop", func() bool {
		d.Drain()
		return status.Get() == "reloaded"
	})

	_, painted := comp.Frame()
	if painted != 1 {
		t.Fatalf("one file change painted %d components, want exactly 1 — only the Text that reads what the handler set", painted)
	}
}

// THE SCHEDULE PIN, and it is a different question from the one above.
//
// TestOneFileChangeRepaintsOnlyTheComponentsThatReadIt calls Frame()
// itself, so it answers "which components repaint" and CANNOT answer
// "would the app have painted at all". A damage count is blind to a
// missing schedule: if the hit never fired Composer.OnInvalidate, that
// test still passes, because the explicit Frame() is the harness doing
// the thing under test. In a real app nothing would ask for a frame and
// the change would sit invisible until some unrelated event forced one —
// which for a file watcher is the whole failure.
//
// Both halves are here. A change SCHEDULES; a poll that sees nothing
// schedules NOTHING, which is the "costs nothing while idle" claim and
// the one that would go silently wrong if the watcher touched a property
// every poll.
func TestAFileChangeSchedulesAFrameAndAnIdlePollDoesNot(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "one", t1)
	d := gooey.NewDispatcher()

	status := prop.NewSource("idle")
	root := &VStack{Children: []gooey.Component{&Text{Content: status}}}
	root.Attach(&FileWatcher{
		FS:       os.DirFS(dir),
		Paths:    Strs([]string{"a.txt"}),
		Interval: time.Millisecond,
		Changed:  gooey.Command(func() { status.Set("reloaded") }),
	})

	comp := gooey.NewComposer(root, 20, 3)
	comp.Frame()
	scheduled := 0
	comp.OnInvalidate(func() { scheduled++ })
	comp.Start(d)
	defer comp.Close()

	// Idle first: many polls, no change, and the scheduler must stay
	// untouched. prop.Set does not compare, so a watcher that Set
	// anything per poll would repaint the page several times a second
	// forever and nothing else in this file would notice.
	drainFor(d, 40*time.Millisecond)
	if scheduled != 0 {
		t.Fatalf("an idle watcher asked for %d frames over ~40 polls; it must cost nothing", scheduled)
	}

	write(t, dir, "a.txt", "two", t2)
	waitFor(t, "the hit to reach the loop", func() bool {
		d.Drain()
		return status.Get() == "reloaded"
	})
	if scheduled == 0 {
		t.Fatal("the change repainted nothing's worth of schedule: OnInvalidate never fired, so App.Run would never have asked for a frame")
	}
}

// THE "LOOKS RIGHT" LEG, which neither of the two above answers.
//
// A damage count of 1 is satisfied by repainting the wrong component,
// and OnInvalidate firing is satisfied by scheduling a frame that draws
// nothing. This asserts the cells, and then FlushBytes — the damage
// guarantee made countable ON THE WIRE. An idle poll must cost zero
// bytes; the hit must cost some, and far less than a full repaint.
func TestAFileChangeReachesTheCellsAndCostsAWireUpdate(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "one", t1)
	d := gooey.NewDispatcher()

	status := prop.NewSource("idle")
	root := &VStack{Children: []gooey.Component{
		&Text{Content: status},
		&Text{Content: Str("second line")},
	}}
	root.Attach(&FileWatcher{
		FS:       os.DirFS(dir),
		Paths:    Strs([]string{"a.txt"}),
		Interval: time.Millisecond,
		Changed:  gooey.Command(func() { status.Set("reloaded") }),
	})

	comp := gooey.NewComposer(root, 20, 3)
	f, _ := comp.Frame()
	if got := strings.TrimRight(row(f.Cells, 0), " "); got != "idle" {
		t.Fatalf("opening row = %q, want %q", got, "idle")
	}
	var sink bytes.Buffer
	if err := comp.Flush(&sink); err != nil {
		t.Fatal(err)
	}
	full := comp.FlushBytes()
	if full == 0 {
		t.Fatal("the first flush wrote nothing")
	}

	comp.Start(d)
	defer comp.Close()

	// Idle: zero bytes on the wire, however many times it polls.
	drainFor(d, 30*time.Millisecond)
	comp.Frame()
	sink.Reset()
	if err := comp.Flush(&sink); err != nil {
		t.Fatal(err)
	}
	if n := comp.FlushBytes(); n != 0 {
		t.Fatalf("an idle watcher cost %d bytes on the wire; an idle app costs zero", n)
	}

	write(t, dir, "a.txt", "two", t2)
	waitFor(t, "the hit to reach the loop", func() bool {
		d.Drain()
		return status.Get() == "reloaded"
	})
	f, _ = comp.Frame()
	if got := strings.TrimRight(row(f.Cells, 0), " "); got != "reloaded" {
		t.Fatalf("row after the change = %q, want %q — the hit never reached the cells", got, "reloaded")
	}
	if got := strings.TrimRight(row(f.Cells, 1), " "); got != "second line" {
		t.Fatalf("the untouched row became %q", got)
	}
	sink.Reset()
	if err := comp.Flush(&sink); err != nil {
		t.Fatal(err)
	}
	hit := comp.FlushBytes()
	if hit == 0 {
		t.Fatal("the change cost zero bytes on the wire — nothing was sent to the terminal")
	}
	if hit >= full {
		t.Errorf("one changed row cost %d bytes against %d for the whole screen; the update is not incremental", hit, full)
	}
}

// Non-visual, like Timer: never measured, never arranged, never painted,
// and contributing no cells. A watcher that measured anything would take
// a row off the page it is attached to.
func TestFileWatcherIsNonVisual(t *testing.T) {
	w := &FileWatcher{Paths: Strs(nil)}
	if !w.NonVisual() {
		t.Error("FileWatcher must report itself non-visual")
	}
	if got := w.Measure(gooey.Size{W: 80, H: 24}); got != (gooey.Size{}) {
		t.Errorf("FileWatcher measured %+v, want zero", got)
	}

	// The page renders identically with and without one attached.
	plain := &VStack{Children: []gooey.Component{&Text{Content: Str("only line")}}}
	withW := &VStack{Children: []gooey.Component{&Text{Content: Str("only line")}}}
	withW.Attach(&FileWatcher{
		FS:      os.DirFS(t.TempDir()),
		Paths:   Strs([]string{"nothing"}),
		Changed: gooey.Command(func() {}),
	})
	a, bare := gooey.NewComposer(plain, 20, 3).Frame()
	b, armed := gooey.NewComposer(withW, 20, 3).Frame()
	for y := range 3 {
		if row(a.Cells, y) != row(b.Cells, y) {
			t.Fatalf("row %d differs with a watcher attached: %q vs %q", y, row(a.Cells, y), row(b.Cells, y))
		}
	}
	// RELATIVE, not absolute. The claim is that attaching a watcher adds
	// no paint node, and the count of the page without one is not this
	// test's business — asserting a literal here got it wrong (a VStack
	// and its Text are two nodes on a first frame, not one) and would go
	// stale the moment the page in this test gained a child.
	if armed != bare {
		t.Errorf("attaching a watcher moved the paint count from %d to %d — it is being treated as a paint node", bare, armed)
	}
}
