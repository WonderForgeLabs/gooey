package main

// The kit: eight sounds, synthesised once at startup into ordinary
// float slices.
//
// No sample files. Not to be clever — an example that needs a folder of
// WAVs is an example that half the people who clone it cannot run, and
// the root module's whole doctrine is that a thing which needs assets or
// an SDK is a different project. Every sound here is twenty lines of
// arithmetic, which is also roughly what the drum machines these are
// impersonating were doing.

import "math"

// Sound is a mono buffer at SampleRate.
type Sound struct {
	Name    string
	Key     rune
	Samples []float64
}

func kit() []Sound {
	return []Sound{
		{"kick", '1', kick()},
		{"snare", '2', snare()},
		{"hat", '3', hat(0.06, 0.9)},
		{"open hat", '4', hat(0.30, 0.5)},
		{"clap", '5', clap()},
		{"tom", '6', tom(140)},
		{"cowbell", '7', cowbell()},
		{"zap", '8', zap()},
	}
}

// kick is a sine whose pitch falls fast. The pitch envelope is the whole
// sound — hold the frequency constant and it is a beep.
func kick() []float64 {
	n := int(0.42 * SampleRate)
	out := make([]float64, n)
	phase := 0.0
	for i := range out {
		t := float64(i) / SampleRate
		f := 48 + 110*math.Exp(-t*38)
		phase += f / SampleRate
		amp := math.Exp(-t * 7)
		out[i] = math.Sin(2*math.Pi*phase) * amp
	}
	return out
}

// snare is noise plus two tuned tones. The tones are what make it a
// snare rather than a shaker.
func snare() []float64 {
	n := int(0.28 * SampleRate)
	out := make([]float64, n)
	for i := range out {
		t := float64(i) / SampleRate
		noise := (hash01(i*7919+13)*2 - 1) * math.Exp(-t*26)
		body := (math.Sin(2*math.Pi*182*t) + 0.7*math.Sin(2*math.Pi*331*t)) * math.Exp(-t*34) * 0.5
		out[i] = clampF(noise*0.85+body, -1, 1)
	}
	return out
}

// hat is filtered noise. The one-pole highpass is what turns hiss into
// metal.
func hat(dur, decay float64) []float64 {
	n := int(dur * SampleRate)
	out := make([]float64, n)
	prev, prevIn := 0.0, 0.0
	for i := range out {
		t := float64(i) / SampleRate
		in := hash01(i*104729+7)*2 - 1
		hp := 0.92 * (prev + in - prevIn)
		prev, prevIn = hp, in
		out[i] = hp * math.Exp(-t/(dur*decay)) * 0.6
	}
	return out
}

// clap is four short noise bursts a few milliseconds apart, which is
// exactly how the real machines did it and why it sounds like a room.
func clap() []float64 {
	n := int(0.34 * SampleRate)
	out := make([]float64, n)
	bursts := []float64{0, 0.010, 0.021, 0.033}
	for i := range out {
		t := float64(i) / SampleRate
		v := 0.0
		for _, b := range bursts {
			if t >= b {
				v += (hash01(i*15486+int(b*1e5))*2 - 1) * math.Exp(-(t-b)*95)
			}
		}
		v += (hash01(i*3181+5)*2 - 1) * math.Exp(-t*11) * 0.35
		out[i] = clampF(v*0.5, -1, 1)
	}
	return out
}

func tom(f float64) []float64 {
	n := int(0.35 * SampleRate)
	out := make([]float64, n)
	phase := 0.0
	for i := range out {
		t := float64(i) / SampleRate
		phase += (f + f*0.6*math.Exp(-t*22)) / SampleRate
		out[i] = math.Sin(2*math.Pi*phase) * math.Exp(-t*9) * 0.8
	}
	return out
}

// cowbell is two square waves a fifth-ish apart through a short
// envelope. It is here because a drum machine without one is not a drum
// machine.
func cowbell() []float64 {
	n := int(0.30 * SampleRate)
	out := make([]float64, n)
	for i := range out {
		t := float64(i) / SampleRate
		a := sq(540 * t)
		b := sq(800 * t)
		out[i] = (a + b) * 0.25 * math.Exp(-t*13)
	}
	return out
}

func zap() []float64 {
	n := int(0.22 * SampleRate)
	out := make([]float64, n)
	phase := 0.0
	for i := range out {
		t := float64(i) / SampleRate
		phase += (1800*math.Exp(-t*18) + 120) / SampleRate
		out[i] = sawAt(phase) * math.Exp(-t*15) * 0.5
	}
	return out
}

func sq(turns float64) float64 {
	if math.Mod(turns, 1) < 0.5 {
		return 1
	}
	return -1
}

func sawAt(turns float64) float64 { return 2*math.Mod(turns, 1) - 1 }

// hash01 is a deterministic pseudo-random in [0,1). math/rand would do
// and would make every run of the demo sound subtly different, which is
// the one thing a demo you intend to record cannot afford.
func hash01(n int) float64 {
	h := uint32(n)*2654435761 + 12345
	h ^= h >> 15
	h *= 2246822519
	h ^= h >> 13
	return float64(h%100000) / 100000
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
