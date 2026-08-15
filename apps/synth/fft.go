package main

// The analyser: a radix-2 FFT and the log-spaced band grouping that
// turns it into the bars everyone recognises.
//
// Thirty lines rather than a dependency. The root module has two direct
// requirements and this is an example, so a DSP library here would be a
// doctrine change to draw twenty-eight rectangles.
//
// # Why the bands are logarithmic
//
// A linear spectrum puts eight of its ten octaves in the right-hand
// quarter of the display, so music looks like a wall on the left and
// nothing anywhere else. Every visualiser ever shipped groups bins
// geometrically instead, which is also how hearing works.

import "math"

// analyse windows the scope, transforms it, and folds the magnitudes
// into Bands. Called with the lock held, from the audio goroutine.
func (e *Engine) analyse() {
	const n = 512
	re := make([]float64, n)
	im := make([]float64, n)

	// Hann window over the most recent n samples of the ring. Without a
	// window every band leaks into its neighbours and the display turns
	// into a single wobbling lump.
	for i := 0; i < n; i++ {
		s := e.scope[(e.spos+ScopeLen-n+i)%ScopeLen]
		re[i] = s * (0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1)))
	}
	fft(re, im)

	// Bin k covers k*SampleRate/n Hz. Bands are geometric from 40 Hz to
	// 16 kHz, which is the range a small speaker can be expected to have
	// an opinion about.
	const lo, hi = 40.0, 16000.0
	binHz := float64(SampleRate) / n
	for b := 0; b < Bands; b++ {
		f0 := lo * math.Pow(hi/lo, float64(b)/Bands)
		f1 := lo * math.Pow(hi/lo, float64(b+1)/Bands)
		k0, k1 := int(f0/binHz), int(f1/binHz)
		if k1 <= k0 {
			k1 = k0 + 1
		}
		if k1 > n/2 {
			k1 = n / 2
		}
		sum := 0.0
		for k := k0; k < k1; k++ {
			sum += math.Hypot(re[k], im[k])
		}
		mag := sum / float64(k1-k0)

		// dB, normalised to something that fills the display without
		// clipping, then smoothed with a fast rise and a slow fall.
		db := 20 * math.Log10(mag+1e-9)
		v := clampF((db+62)/56, 0, 1)
		if v > e.spectrum[b] {
			e.spectrum[b] = v
		} else {
			e.spectrum[b] += (v - e.spectrum[b]) * 0.25
		}
	}
}

// fft is an in-place iterative Cooley-Tukey transform. len(re) must be a
// power of two; it is called with a constant, so there is no check to
// get wrong at runtime.
func fft(re, im []float64) {
	n := len(re)

	// bit-reversal permutation
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j |= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}

	for length := 2; length <= n; length <<= 1 {
		ang := -2 * math.Pi / float64(length)
		wr, wi := math.Cos(ang), math.Sin(ang)
		for i := 0; i < n; i += length {
			cr, ci := 1.0, 0.0
			for j := 0; j < length/2; j++ {
				ur, ui := re[i+j], im[i+j]
				vr := re[i+j+length/2]*cr - im[i+j+length/2]*ci
				vi := re[i+j+length/2]*ci + im[i+j+length/2]*cr
				re[i+j], im[i+j] = ur+vr, ui+vi
				re[i+j+length/2], im[i+j+length/2] = ur-vr, ui-vi
				cr, ci = cr*wr-ci*wi, cr*wi+ci*wr
			}
		}
	}
}
