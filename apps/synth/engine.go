package main

// The audio engine: a polyphonic synthesiser that writes raw PCM to a
// pipe and hands the UI a picture of what it just played.
//
// # The rule that shapes everything here
//
// Properties are UI-goroutine-confined and unlocked. The audio goroutine
// runs at 48 kHz and cannot stop to be polite, so it may not Get or Set
// anything — not once. What it does instead is write its output into a
// plain mutex-guarded snapshot, and a Startable on the UI side reads that
// snapshot on a frame clock and does the one Set.
//
// That split is why the visualiser is honest and cheap at the same time:
// the picture is made of real samples, and the property graph sees one
// change per frame rather than 48000.
//
// # Why a pipe to pacat and not a sound library
//
// The doctrine is that a dependency of this weight gets a nested module or
// does not happen — it is the transitive graph that decides, not a count of
// `require` lines. A synthesiser that needs PortAudio is a different
// project. `pacat` takes raw interleaved little-endian 16-bit stereo on
// stdin, which is exactly what a mixer produces, so the whole audio backend
// is one exec and one io.Writer.
//
//	pacat --rate=48000 --channels=2 --format=s16le --latency-msec=30
//
// --latency-msec matters. Without it pacat buffers so far ahead that a
// keypress lands a second later, which for an instrument is the same as
// being broken.

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os/exec"
	"sync"
)

const (
	SampleRate = 48000
	Block      = 512 // frames per write — about 10.7 ms
	Voices     = 12
	ScopeLen   = 1024
	Bands      = 28
)

// Wave is the oscillator shape.
type Wave int

const (
	Saw Wave = iota
	Square
	Triangle
	Sine
	WaveCount
)

func (w Wave) String() string {
	switch w {
	case Saw:
		return "saw"
	case Square:
		return "square"
	case Triangle:
		return "triangle"
	}
	return "sine"
}

// voice is one note. phase is kept in turns (0..1) rather than radians
// so every waveform is a cheap function of the fractional part.
type voice struct {
	on    bool
	note  int
	freq  float64
	phase float64
	env   float64
	// stage: 0 attack, 1 decay/sustain, 2 release
	stage int
	// blocks counts how long the note has been held, for the fixed
	// lifetime that stands in for the key-up event a terminal never
	// sends.
	blocks int
}

// Snapshot is what the visualiser gets: the last block of mono samples
// and a band magnitude spectrum. It is a value, copied out under the
// lock, so the UI never reads memory the audio goroutine is writing.
type Snapshot struct {
	Scope    [ScopeLen]float64
	Spectrum [Bands]float64
	Peak     float64
	Active   int
}

type Engine struct {
	mu sync.Mutex

	voices [Voices]voice
	wave   Wave
	cutoff float64 // 0..1
	res    float64 // 0..1
	gain   float64

	// one-pole-per-stage lowpass state, and the delay line for depth
	lp1, lp2 float64
	delay    []float64
	dpos     int

	scope       [ScopeLen]float64
	spos        int
	spectrum    [Bands]float64
	peak        float64
	activeCount int

	// started is drained by nothing today; it exists so a future
	// arpeggiator has a record of note order. Kept because removing it
	// would also remove the reason NoteOn takes the note at all.
	started []int

	out  io.WriteCloser
	cmd  *exec.Cmd
	err  error
	stop chan struct{}
	done chan struct{}
}

func NewEngine() *Engine {
	return &Engine{
		wave:   Saw,
		cutoff: 0.55,
		res:    0.25,
		gain:   0.22,
		delay:  make([]float64, SampleRate/3),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
}

// Start launches pacat and the render loop. It reports a failure rather
// than returning an error because a machine with no sound server should
// still show the instrument — silent, and saying so.
func (e *Engine) Start() {
	cmd := exec.Command("pacat",
		"--rate=48000", "--channels=2", "--format=s16le",
		"--latency-msec=30", "--process-time-msec=10",
		"--stream-name=gooey-synth")
	in, err := cmd.StdinPipe()
	if err != nil {
		e.setErr(err)
		close(e.done)
		return
	}
	if err := cmd.Start(); err != nil {
		e.setErr(fmt.Errorf("pacat: %w (no audio server?)", err))
		close(e.done)
		return
	}
	e.out, e.cmd = in, cmd

	go e.run()
}

// Close stops the engine and joins it.
//
// The ORDER is the whole content of this function. run() spends nearly
// all of its time blocked in a write to pacat's stdin — that write is
// what paces the loop — so signalling `stop` does not wake it. Joining
// first and closing the pipe afterwards, which is what this did, meant
// the join waited for a write that only returns when pacat consumes;
// with a healthy sound server that is ten milliseconds, and with a
// pacat that has stalled or lost its sink it is forever. Close runs on
// the UI goroutine, so "forever" there is the whole app hung on exit
// with the terminal already restored — a process you have to kill.
//
// So: signal, then CLOSE THE PIPE, which makes the parked write return
// ErrClosed immediately (os.File is safe to Close concurrently with a
// Write in flight), and only then join. The join is still a real
// barrier — Close ⇒ no further writes — it just cannot outlast the
// pipe any more.
func (e *Engine) Close() {
	close(e.stop)
	if e.out != nil {
		_ = e.out.Close() // wakes a write parked in run()
	}
	<-e.done
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
		_ = e.cmd.Wait()
	}
}

func (e *Engine) setErr(err error) {
	e.mu.Lock()
	e.err = err
	e.mu.Unlock()
}

func (e *Engine) Err() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}

// run is the audio goroutine. It never blocks on anything but the pipe,
// and the pipe is what paces it: pacat consumes at exactly real time, so
// this loop runs at exactly real time without a timer anywhere in it.
func (e *Engine) run() {
	defer close(e.done)
	buf := make([]byte, Block*4) // 2 channels × 2 bytes
	mono := make([]float64, Block)

	for {
		select {
		case <-e.stop:
			return
		default:
		}

		e.mu.Lock()
		e.age()
		e.render(mono)
		e.mu.Unlock()

		for i, v := range mono {
			s := int16(clampF(v, -1, 1) * 32000)
			binary.LittleEndian.PutUint16(buf[i*4:], uint16(s))
			binary.LittleEndian.PutUint16(buf[i*4+2:], uint16(s))
		}
		if _, err := e.out.Write(buf); err != nil {
			e.setErr(err)
			return
		}
	}
}

// age applies the fixed note lifetime.
//
// A terminal delivers key PRESSES and nothing else — there is no key-up
// event on the wire, so nothing can tell this engine that a finger left
// a key. Every note therefore gets a length, in blocks, and releases
// itself. It is the same class of limit as "a recording pty cannot carry
// mouse reports": a property of the medium, worth naming rather than
// working around.
//
// Called with the lock held.
func (e *Engine) age() {
	for i := range e.voices {
		if !e.voices[i].on {
			continue
		}
		e.voices[i].blocks++
		if e.voices[i].blocks > noteBlocks {
			e.voices[i].on = false
			e.voices[i].stage = 2
		}
	}
}

// About 0.35 s of key-down before the release stage begins, which with
// the 0.18 s release makes a note that reads as plucked rather than as
// a beep or as a drone.
const noteBlocks = 32

// render fills one block. Called with the lock held — every field it
// touches is also touched by key handling on the UI goroutine.
func (e *Engine) render(out []float64) {
	// A resonant two-pole lowpass, the cheap Stilson/Smith shape: two
	// one-pole sections with feedback around them. Not a ladder filter,
	// but it squelches, which is the only requirement.
	f := 0.05 + 0.9*e.cutoff*e.cutoff
	q := 4.0 * e.res

	active := 0
	for i := range out {
		var sum float64
		for v := range e.voices {
			vo := &e.voices[v]
			if vo.env <= 0.0001 && vo.stage == 2 {
				vo.on = false
				continue
			}
			if !vo.on && vo.env <= 0.0001 {
				continue
			}
			sum += osc(e.wave, vo.phase) * vo.env
			vo.phase += vo.freq / SampleRate
			if vo.phase >= 1 {
				vo.phase -= 1
			}
			switch vo.stage {
			case 0:
				vo.env += 1.0 / (0.004 * SampleRate)
				if vo.env >= 1 {
					vo.env, vo.stage = 1, 1
				}
			case 1:
				// decay toward a sustain of 0.7
				vo.env += (0.7 - vo.env) / (0.25 * SampleRate)
			case 2:
				vo.env -= vo.env / (0.18 * SampleRate)
			}
			if i == 0 && vo.env > 0.001 {
				active++
			}
		}
		sum *= e.gain

		// filter
		in := sum - q*e.lp2
		e.lp1 += f * (in - e.lp1)
		e.lp2 += f * (e.lp1 - e.lp2)
		s := e.lp2

		// a short feedback delay, because a bare oscillator in a terminal
		// sounds like a test tone and the point is that it sounds like an
		// instrument
		d := e.delay[e.dpos]
		e.delay[e.dpos] = s + d*0.32
		e.dpos = (e.dpos + 1) % len(e.delay)
		s += d * 0.35

		s = math.Tanh(s * 1.6)
		out[i] = s

		e.scope[e.spos] = s
		e.spos = (e.spos + 1) % ScopeLen
	}

	e.analyse()
	e.trackPeak(out)
	e.activeCount = active
}

func (e *Engine) trackPeak(block []float64) {
	p := 0.0
	for _, v := range block {
		if a := math.Abs(v); a > p {
			p = a
		}
	}
	// Fast attack, slow release — a meter that falls as fast as it rises
	// is unreadable, which is why every meter ever built does this.
	if p > e.peak {
		e.peak = p
	} else {
		e.peak += (p - e.peak) * 0.08
	}
}

// Snapshot copies the picture out. It is the ONLY thing the UI reads,
// and it is a value copy on purpose: handing out the arrays would put
// the audio goroutine and the renderer in the same memory at 48 kHz.
func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	var s Snapshot
	// Unwrap the ring so the scope reads left-to-right in time order.
	for i := 0; i < ScopeLen; i++ {
		s.Scope[i] = e.scope[(e.spos+i)%ScopeLen]
	}
	s.Spectrum = e.spectrum
	s.Peak = e.peak
	s.Active = e.activeCount
	return s
}

func osc(w Wave, p float64) float64 {
	switch w {
	case Saw:
		return 2*p - 1
	case Square:
		if p < 0.5 {
			return 1
		}
		return -1
	case Triangle:
		if p < 0.5 {
			return 4*p - 1
		}
		return 3 - 4*p
	}
	return math.Sin(2 * math.Pi * p)
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// --- the surface the UI calls, all of it under the same lock --------
//
// These run on the UI goroutine and mutate state the audio goroutine
// reads every block. The mutex is the only thing between them, and it is
// held for a handful of assignments — never for a syscall, never for a
// write to the pipe.

// NoteOn steals the oldest voice when all are busy. Dropping the note
// instead would make a held chord swallow the next key, which reads as
// the instrument being broken rather than as a limit being reached.
func (e *Engine) NoteOn(note int, freq float64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	slot, oldest := -1, 0
	for i := range e.voices {
		if !e.voices[i].on && e.voices[i].env <= 0.0001 {
			slot = i
			break
		}
		if e.voices[i].env < e.voices[oldest].env {
			oldest = i
		}
	}
	if slot < 0 {
		slot = oldest
	}
	e.voices[slot] = voice{on: true, note: note, freq: freq, env: 0, stage: 0}
	e.started = append(e.started, note)
}

// Release moves a voice into its release stage. A terminal has no
// key-up, so nothing calls this from a keyboard — it is what the fixed
// note lifetime uses.
func (e *Engine) Release(note int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.voices {
		if e.voices[i].on && e.voices[i].note == note {
			e.voices[i].stage = 2
			e.voices[i].on = false
		}
	}
}

func (e *Engine) CycleWave() Wave {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.wave = (e.wave + 1) % WaveCount
	return e.wave
}

func (e *Engine) SetCutoff(v float64) {
	e.mu.Lock()
	e.cutoff = clampF(v, 0, 1)
	e.mu.Unlock()
}

func (e *Engine) SetRes(v float64) {
	e.mu.Lock()
	// Capped below 1 because the feedback term is 4*res and the filter
	// self-oscillates into a scream somewhere above that. A synth that
	// can hurt you is a design choice; this one is in a slide.
	e.res = clampF(v, 0, 0.92)
	e.mu.Unlock()
}

// Expired reports notes whose voices have finished, so the on-screen
// keyboard can stop lighting them. It drains, because a note that is
// reported twice would be deleted twice and a note never reported would
// stay lit forever.
func (e *Engine) Expired() []int {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []int
	for i := range e.voices {
		if e.voices[i].env <= 0.0001 && e.voices[i].note >= 0 && !e.voices[i].on {
			out = append(out, e.voices[i].note)
			e.voices[i].note = -1
		}
	}
	return out
}
