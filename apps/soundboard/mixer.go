package main

// The mixer: eight channels, a sequencer, and one stereo stream out.
//
// This is a real mixer, in the sense that matters: eight voices are
// summed in Go, per-channel gain and pan are applied in Go, and what
// reaches the sound server is one interleaved buffer. Nothing is
// "playing a file" — there is a single output stream and everything that
// makes a noise is inside it.
//
// # Where the goroutine boundary is
//
// The audio goroutine runs at 48 kHz and never touches the property
// graph. It owns the playheads, the step clock and the meters; the UI
// reads all of that as a Snapshot value, once a frame, and does one Set.
// The same shape as apps/synth, for the same reason: properties are
// unlocked and UI-confined, and 48000 Sets a second would be a bug even
// if it were legal.

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
	Block      = 512
	Steps      = 16
	Polyphony  = 24
	ScopeLen   = 512
)

// play is one sounding copy of a pad. Sounds overlap — hitting the same
// pad twice does not cut the first hit off, because a drum machine that
// did that would sound wrong on every roll.
type play struct {
	ch  int
	pos int
}

type Channel struct {
	Gain  float64
	Pan   float64 // -1 left, +1 right
	Mute  bool
	Steps [Steps]bool
	Meter float64
}

type Snapshot struct {
	Step     int
	Playing  bool
	Channels [8]Channel
	Scope    [ScopeLen]float64
	Peak     float64
}

type Mixer struct {
	mu sync.Mutex

	kit  []Sound
	ch   [8]Channel
	live [Polyphony]play
	n    int

	playing bool
	bpm     int
	step    int
	acc     int // samples until the next step
	master  float64

	scope [ScopeLen]float64
	spos  int
	peak  float64

	out  io.WriteCloser
	cmd  *exec.Cmd
	err  error
	stop chan struct{}
	done chan struct{}
}

func NewMixer(bpm int) *Mixer {
	m := &Mixer{
		kit:    kit(),
		bpm:    bpm,
		master: 0.8,
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	for i := range m.live {
		m.live[i].ch = -1
	}
	for i := range m.ch {
		m.ch[i].Gain = 0.8
		m.ch[i].Pan = float64(i-4) * 0.12
	}
	// A pattern that is already interesting on the first frame. An empty
	// grid is a correct starting state and a terrible demo.
	m.ch[0].Steps = [Steps]bool{true, false, false, false, false, false, true, false, true, false, false, false, false, false, false, false}
	m.ch[1].Steps = [Steps]bool{false, false, false, false, true, false, false, false, false, false, false, false, true, false, false, true}
	m.ch[2].Steps = [Steps]bool{true, false, true, false, true, false, true, false, true, false, true, false, true, false, true, false}
	m.ch[3].Steps = [Steps]bool{false, false, false, false, false, false, false, false, false, false, true, false, false, false, false, false}
	m.acc = m.stepSamples()
	return m
}

func (m *Mixer) stepSamples() int {
	// Sixteenth notes: four per beat.
	return SampleRate * 60 / (m.bpm * 4)
}

func (m *Mixer) Kit() []Sound { return m.kit }

func (m *Mixer) Start() {
	cmd := exec.Command("pacat",
		"--rate=48000", "--channels=2", "--format=s16le",
		"--latency-msec=30", "--process-time-msec=10",
		"--stream-name=gooey-soundboard")
	in, err := cmd.StdinPipe()
	if err != nil {
		m.setErr(err)
		close(m.done)
		return
	}
	if err := cmd.Start(); err != nil {
		m.setErr(fmt.Errorf("pacat: %w (no audio server?)", err))
		close(m.done)
		return
	}
	m.out, m.cmd = in, cmd
	go m.run()
}

// Close signals, wakes the writer, THEN joins.
//
// run() is parked in a write to pacat's stdin almost all of the time —
// that write is what paces the sequencer — so `stop` on its own does not
// reach it. Joining before closing the pipe made the join wait on a
// write that only returns when pacat consumes, which is a hang on the UI
// goroutine if pacat has stalled. Closing the pipe first makes that
// write return ErrClosed at once; os.File is safe to Close with a Write
// in flight, and the join after it is still a real barrier.
func (m *Mixer) Close() {
	close(m.stop)
	if m.out != nil {
		_ = m.out.Close() // wakes a write parked in run()
	}
	<-m.done
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		_ = m.cmd.Wait()
	}
}

func (m *Mixer) setErr(e error) { m.mu.Lock(); m.err = e; m.mu.Unlock() }
func (m *Mixer) Err() error     { m.mu.Lock(); defer m.mu.Unlock(); return m.err }

// run is paced by the pipe: pacat drains at exactly real time, so this
// loop runs at exactly real time with no timer in it. The sequencer's
// clock is therefore SAMPLE-accurate rather than frame-accurate, which
// is the difference between a groove and a stumble.
func (m *Mixer) run() {
	defer close(m.done)
	buf := make([]byte, Block*4)
	l := make([]float64, Block)
	r := make([]float64, Block)

	for {
		select {
		case <-m.stop:
			return
		default:
		}

		m.mu.Lock()
		m.render(l, r)
		m.mu.Unlock()

		for i := range l {
			binary.LittleEndian.PutUint16(buf[i*4:], uint16(int16(clampF(l[i], -1, 1)*32000)))
			binary.LittleEndian.PutUint16(buf[i*4+2:], uint16(int16(clampF(r[i], -1, 1)*32000)))
		}
		if _, err := m.out.Write(buf); err != nil {
			m.setErr(err)
			return
		}
	}
}

// render fills one block, advancing the step clock inside it. Called
// with the lock held.
func (m *Mixer) render(l, r []float64) {
	var meters [8]float64
	peak := 0.0

	for i := range l {
		if m.playing {
			m.acc--
			if m.acc <= 0 {
				m.acc = m.stepSamples()
				m.step = (m.step + 1) % Steps
				for c := range m.ch {
					if m.ch[c].Steps[m.step] && !m.ch[c].Mute {
						m.trigger(c)
					}
				}
			}
		}

		var ls, rs float64
		for v := range m.live {
			p := &m.live[v]
			if p.ch < 0 {
				continue
			}
			s := m.kit[p.ch].Samples
			if p.pos >= len(s) {
				p.ch = -1
				continue
			}
			val := s[p.pos] * m.ch[p.ch].Gain
			p.pos++
			if a := math.Abs(val); a > meters[p.ch] {
				meters[p.ch] = a
			}
			// Equal-ish power pan. Linear pan makes the middle quieter
			// than either side, which is audible the moment anything
			// moves across the field.
			pan := (m.ch[p.ch].Pan + 1) / 2
			ls += val * math.Cos(pan*math.Pi/2)
			rs += val * math.Sin(pan*math.Pi/2)
		}
		ls = math.Tanh(ls * m.master)
		rs = math.Tanh(rs * m.master)
		l[i], r[i] = ls, rs

		mono := (ls + rs) / 2
		m.scope[m.spos] = mono
		m.spos = (m.spos + 1) % ScopeLen
		if a := math.Abs(mono); a > peak {
			peak = a
		}
	}

	// Fast attack, slow release, on the meters and the master peak alike.
	for c := range m.ch {
		if meters[c] > m.ch[c].Meter {
			m.ch[c].Meter = meters[c]
		} else {
			m.ch[c].Meter += (meters[c] - m.ch[c].Meter) * 0.10
		}
	}
	if peak > m.peak {
		m.peak = peak
	} else {
		m.peak += (peak - m.peak) * 0.08
	}
}

// trigger allocates a voice, stealing the oldest when full. Called with
// the lock held.
func (m *Mixer) trigger(c int) {
	slot, oldest := -1, 0
	for i := range m.live {
		if m.live[i].ch < 0 {
			slot = i
			break
		}
		if m.live[i].pos > m.live[oldest].pos {
			oldest = i
		}
	}
	if slot < 0 {
		slot = oldest
	}
	m.live[slot] = play{ch: c, pos: 0}
}

// --- the UI's surface, all under the same lock ----------------------

func (m *Mixer) Hit(c int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c >= 0 && c < len(m.kit) {
		m.trigger(c)
	}
}

func (m *Mixer) ToggleStep(c, s int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c >= 0 && c < 8 && s >= 0 && s < Steps {
		m.ch[c].Steps[s] = !m.ch[c].Steps[s]
	}
}

func (m *Mixer) ToggleMute(c int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c >= 0 && c < 8 {
		m.ch[c].Mute = !m.ch[c].Mute
	}
}

func (m *Mixer) Gain(c int, d float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c >= 0 && c < 8 {
		m.ch[c].Gain = clampF(m.ch[c].Gain+d, 0, 1.5)
	}
}

func (m *Mixer) Pan(c int, d float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c >= 0 && c < 8 {
		m.ch[c].Pan = clampF(m.ch[c].Pan+d, -1, 1)
	}
}

// TogglePlay resets the step to just before the first, so pressing play
// always starts the pattern at its beginning. Resuming mid-bar is a
// defensible design and a confusing demo.
func (m *Mixer) TogglePlay() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.playing = !m.playing
	if m.playing {
		m.step, m.acc = Steps-1, 1
	}
	return m.playing
}

func (m *Mixer) Tempo(d int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bpm = clampI(m.bpm+d, 40, 220)
	return m.bpm
}

func (m *Mixer) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for c := range m.ch {
		m.ch[c].Steps = [Steps]bool{}
	}
}

func (m *Mixer) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	var s Snapshot
	s.Step, s.Playing, s.Peak = m.step, m.playing, m.peak
	s.Channels = m.ch
	for i := 0; i < ScopeLen; i++ {
		s.Scope[i] = m.scope[(m.spos+i)%ScopeLen]
	}
	return s
}

func clampI(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}
