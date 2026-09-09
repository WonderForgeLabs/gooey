// Package encoders is a FIXTURE, not code. It is under testdata, which
// the go tool ignores, so it compiles as part of nothing and exists only
// to be parsed by graphics/opaque_test.go's encoder walk.
//
// It holds the types graphics/ cannot: two that satisfy Encoder wrongly,
// which the walk has to refuse, and two that satisfy it by embedding,
// which it has to accept. Both rules are unobservable through graphics/
// itself, because stating them needs a type that gets them wrong.
package encoders

import "image"

// Encoder is the same SHAPE as graphics.Encoder — two methods, one of
// them carrying the signature — declared here so the walk reads its
// method set out of this package rather than importing an answer.
type Encoder interface {
	Name() string
	Encode(out *[]byte, img image.Image, cols, rows, cellW, cellH int) error
}

// Full declares both methods itself.
type Full struct{}

func (Full) Name() string { return "full" }

func (Full) Encode(out *[]byte, img image.Image, cols, rows, cellW, cellH int) error {
	return nil
}

// Derived is an encoder by EMBEDDING and declares nothing of its own.
type Derived struct{ Full }

// Chained embeds an embedder, which is what the fixed point is for.
type Chained struct{ Derived }

// Partial declares Encode and not Name, so it is not an Encoder — the
// case a walk matching the method name alone counts wrongly.
type Partial struct{}

func (Partial) Encode(out *[]byte, img image.Image, cols, rows, cellW, cellH int) error {
	return nil
}

// Shaped declares both names and the WRONG Encode signature.
type Shaped struct{}

func (Shaped) Name() string { return "shaped" }

func (Shaped) Encode(out *[]byte) error { return nil }
