package main

// Deck is the running presentation: the script, where we are in it, and
// the handles the markup binds to.
//
// The division of labour is the one the framework asks for. Go holds
// VALUES — which beat, how many seconds, what the CPU is doing. The
// markup composes them into what you read. There is no fmt.Sprintf in
// the view layer and no markup assembled by concatenation anywhere,
// because bindText already interpolates literals and paths into a single
// computed with the right damage (markup/markup.go:1018), and doing it
// in Go would only be a second, worse implementation of that.

import (
	"io/fs"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/control"
	"github.com/WonderForgeLabs/gooey/prop"
)

type Deck struct {
	dir    fs.FS
	beats  []Beat
	player *Player
	app    *gooey.App
	svc    *control.Service

	sampler Sampler

	// --- source state ---
	idx      *prop.Property[int]
	elapsed  *prop.Property[int]
	wrapAt   *prop.Property[int]
	auto     *prop.Property[bool]
	prompter *prop.Property[bool]
	status   *prop.Property[string]

	// rev is bumped when the script is re-read. Everything derived reads
	// it through beat(), which is what makes a reload repaint: markup.Page
	// watches deck.gooey, but nothing watches NARRATION.md, and a beat
	// table that silently went stale is the worst bug a teleprompter has.
	rev *prop.Property[int]

	// --- the live readout behind the top island on beat 1.2 ---
	cpu     *prop.Property[int]
	mem     *prop.Property[int]
	load    *prop.Property[[]float64]
	procs   *prop.Property[[]Proc]
	sysline *prop.Property[string]

	// The counter Include on beat 2.5, edited on the left of that slide
	// and running on the right. source is the file's text, kept only for
	// the prompter and for anyone reading the deck without a vim.
	count  *prop.Property[int]
	source *prop.Property[string]

	// counterStamp is the last-seen size+mtime of counter.gooey. The
	// slide hosts a real vim editing that file, and a save has to reach
	// the running pane — which a save cannot do on its own: markup.Page
	// watches deck.gooey, and the right-hand pane was not loaded by the
	// page, it was patched into the Stage slot. So the file is polled on
	// the tick the deck already has, and a change forces a re-patch,
	// which re-resolves the Include from disk.
	counterStamp string

	// live is live.gooey, read once. It is the right-hand pane of that
	// slide on its own, so the re-patch can leave the editor alone.
	live string

	// staged is the markup currently in the Stage slot, kept so that
	// moving between two plain beats does not pay for a patch that
	// changes nothing.
	staged string
	plain  string

	// The receipt, beat 3.7. painted is the real damage count of the
	// frame a click caused — App.PaintedLastFrame, not a number this
	// file makes up.
	//
	// wantPaint is what keeps it from being an infinite loop. AfterFrame
	// runs after every frame, and Setting a property from there
	// schedules ANOTHER frame (app.go:282), so publishing on every frame
	// would repaint forever and the number would be its own cause. The
	// flag is armed by the click and cleared by the first hook that
	// fires after it, so exactly one Set happens per click and the
	// number describes the frame the click actually produced.
	painted   *prop.Property[int]
	wantPaint bool
}

// Bump is the counter's click. It arms the receipt before changing the
// count so that the frame this Set schedules is the frame the hook
// reports on.
func (d *Deck) Bump() {
	d.wantPaint = true
	d.count.Set(d.count.Get() + 1)
}

// publishPainted is the AfterFrame hook. Registered in main.go, because
// the hook has to outlive any one slide and the app is what owns it.
func (d *Deck) publishPainted() {
	if !d.wantPaint {
		return
	}
	d.wantPaint = false
	d.painted.Set(d.app.PaintedLastFrame())
}

func NewDeck(dir fs.FS, beats []Beat, player *Player, wrap, start int) (*Deck, error) {
	plain, err := fs.ReadFile(dir, "stage.gooey")
	if err != nil {
		return nil, err
	}
	counter, err := fs.ReadFile(dir, "counter.gooey")
	if err != nil {
		return nil, err
	}
	live, err := fs.ReadFile(dir, "live.gooey")
	if err != nil {
		return nil, err
	}
	return &Deck{
		live:     string(live),
		dir:      dir,
		beats:    beats,
		player:   player,
		plain:    string(plain),
		staged:   string(plain), // what deck.gooey already declares
		idx:      prop.NewSource(clamp(start, 0, len(beats)-1)),
		elapsed:  prop.NewSource(0),
		wrapAt:   prop.NewSource(wrap),
		auto:     prop.NewSource(false),
		prompter: prop.NewSource(false),
		status:   prop.NewSource(summarizeMissing(player.Missing(beats), len(beats))),
		rev:      prop.NewSource(0),
		cpu:      prop.NewSource(0),
		mem:      prop.NewSource(0),
		load:     prop.NewSource([]float64(nil)),
		procs:    prop.NewSource([]Proc(nil)),
		sysline:  prop.NewSource(""),
		count:    prop.NewSource(0),
		painted:  prop.NewSource(0),
		source:   prop.NewSource(string(counter)),
	}, nil
}

// beat is the current one. Reading rev here — rather than in each
// derived property — is what puts every one of them downstream of a
// script reload without any of them mentioning it.
func (d *Deck) beat() Beat {
	d.rev.Get()
	return d.beats[clamp(d.idx.Get(), 0, len(d.beats)-1)]
}

// Reload re-reads the script in place. The beat index survives, clamped,
// so editing the file mid-rehearsal does not send the presenter back to
// the title card.
func (d *Deck) Reload() string {
	next, err := ParseNarration(d.dir, "NARRATION.md")
	if err != nil {
		return "script error: " + err.Error()
	}
	if counter, err := fs.ReadFile(d.dir, "counter.gooey"); err == nil {
		d.source.Set(string(counter))
	}
	d.beats = next
	d.idx.Set(clamp(d.idx.Get(), 0, len(d.beats)-1))
	d.rev.Set(d.rev.Get() + 1)

	// The stage cache cannot survive a reload: the beat it was caching
	// may not exist any more.
	d.staged = ""
	d.Restage()

	speech, holds := Runtime(d.beats)
	return join(" · ", "reloaded",
		itoa(len(d.beats))+" beats",
		clockOf(speech)+" speech",
		clockOf(holds)+" holds")
}

// GoTo is the only place a beat change happens, so no path can move the
// deck without moving the sound and the stage with it.
//
// Ordering is load-bearing: Restage only writes status on failure, so it
// must run AFTER the audio line or a stage error is silently overwritten
// by "no audio: …". That cost half an hour once.
func (d *Deck) GoTo(n int) {
	n = clamp(n, 0, len(d.beats)-1)
	d.idx.Set(n)
	d.elapsed.Set(0)
	d.status.Set(d.player.Play(d.beats[n].ID))
	d.Restage()
}

func (d *Deck) Advance(by int) { d.GoTo(d.idx.Get() + by) }

// Tick is one second. Auto-advance runs off the same clock the presenter
// reads, so the deck can never pace itself by a number nobody can see.
func (d *Deck) Tick() {
	e := d.elapsed.Get() + 1
	d.elapsed.Set(e)
	b := d.beat()

	// Sample only while a staged slide is up: the gauges are its only
	// readers, and prop.Set does not compare, so setting them off-slide
	// would repaint an off-screen component once a second for twenty
	// minutes.
	if b.Staged() && !d.prompter.Get() {
		d.sample()
		d.syncCounter()
	}
	if d.auto.Get() && b.Dur > 0 && e >= int((b.Dur+b.Hold).Seconds()) {
		d.Advance(1)
	}
}

// syncCounter is the other half of the vim slide.
//
// The left pane is a real vim on a real pty, editing counter.gooey. When
// it writes, nothing in the framework notices: markup.Page's watcher is
// on deck.gooey, and the right-hand pane came from PatchMarkup, not from
// the page. So the file is polled — once a second, on the tick the deck
// already runs, and only while a staged slide is up.
//
// It patches Live and not Stage, and that distinction is the whole
// reason Live exists. Re-patching the Stage rebuilds the Terminal too:
// the guest is killed and vim reopens on the file, losing the cursor and
// whatever the presenter had half-typed. Live is the smallest subtree
// that contains the Include and excludes the editor.
//
// The source is live.gooey, unchanged every time. That is fine and is
// the point — what changed is the file the <Counter/> inside it resolves
// to, one level down, and a fresh build re-reads it from the same fs.FS.
func (d *Deck) syncCounter() {
	st, err := fs.Stat(d.dir, "counter.gooey")
	if err != nil {
		return
	}
	stamp := itoa(int(st.Size())) + "@" + st.ModTime().String()
	if stamp == d.counterStamp {
		return
	}
	first := d.counterStamp == ""
	d.counterStamp = stamp
	if first {
		return // the opening sample, not an edit
	}
	if b, err := fs.ReadFile(d.dir, "counter.gooey"); err == nil {
		d.source.Set(string(b))
	}
	if d.svc == nil || d.live == "" {
		return
	}
	// A failed build here is a markup error the presenter has just typed,
	// so it belongs on the status line and nowhere else: the previous
	// pane stays up, and the next :w that parses replaces it.
	if _, err := d.svc.PatchMarkup("Live", d.live); err != nil {
		d.status.Set("counter.gooey: " + err.Error())
		return
	}
	d.status.Set("counter.gooey reloaded")
}

func (d *Deck) sample() {
	s := d.sampler.Sample()
	d.cpu.Set(s.CPU)
	d.mem.Set(s.Mem)
	d.load.Set(d.sampler.History())
	d.procs.Set(s.Procs)
	d.sysline.Set(s.Head)
}

func (d *Deck) target() time.Duration { return d.beat().Dur }
