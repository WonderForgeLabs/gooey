// Package encoderfixture is a FIXTURE, not code: it exists to be parsed
// by graphics/opaque_test.go's encoder walk. Nothing in the non-test
// build imports it — `go list -deps ./...` does not name it — and the
// one importer is that test file, which needs Pointed as a VALUE and
// not only as source text.
//
// It holds the types graphics/ cannot: two that satisfy Encoder wrongly,
// which the walk has to refuse, four that satisfy it by embedding, which
// it has to accept, and one that satisfies it ON THE POINTER, where the
// walk's answer and the table's answer have to be made to agree. The
// first two rules are unobservable through graphics/ itself because
// stating them needs a type that gets them wrong; the third is
// unobservable because stating it needs a real encoder nobody ships. It is
// under internal/ so that no consumer can reach these types, and in a
// package of its own so they stay out of the walk over ".".
//
// IT USED TO LIVE UNDER testdata/, AND THAT WAS THE DEFECT. The go tool
// ignores a testdata directory, so the file compiled as part of nothing
// — which is what a fixture wants until you notice what it costs. Every
// claim this file makes is a fact about GO'S TYPE SYSTEM: that Wrapped,
// Boxed and Chained really do satisfy Encoder, that Partial and Shaped
// really do not. Under testdata nothing checked any of them. parser.
// ParseDir catches a syntax error and cannot catch a semantic one, and
// the only reader of the file was the very walk it exists to verify — so
// a fixture wrong about embedding in the SAME DIRECTION as the walk was
// green, and TestTheEncoderWalkNeedsTheWHOLEInterface reduced to "the
// walk agrees with what its author believed". Which is the two-hand-
// lists finding of round six, one level down.
//
// Here it is compiled by go vet ./... and go test ./..., and the
// compile-time assertions below say the positive half outright. Raised
// in review of #474.
package encoderfixture

import "image"

// THE POSITIVE HALF, ASSERTED BY THE COMPILER. These lines are the
// ground truth the walk is checked against; if embedding does not work
// the way this file claims, the package does not build.
//
// Partial and Shaped deliberately have no line here, and the omission is
// the point: "does not implement" is the half the compiler cannot state
// — a missing assertion is not an assertion — and it is exactly the half
// the walk exists for. graphics/opaque_test.go:26 already splits
// `var _ OpaqueEncoder = Sixel{}` the same way.
var (
	_ Encoder = Full{}
	_ Encoder = Derived{}
	_ Encoder = Chained{}
	_ Encoder = Wrapped{}
	_ Encoder = Boxed{}
	_ Encoder = &Pointed{}
)

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

// Wrapped embeds THE INTERFACE, which is a satisfier no walk keyed on
// declarations can see: it declares nothing and embeds nothing that
// declares anything. `struct{ Encoder }` is the delegating-decorator
// shape — the one you reach for to wrap an encoder with logging or a
// size cap — so it is not a corner. The walk was silent on it in review
// of #474.
type Wrapped struct{ Encoder }

// Named is an INTERFACE embedding the interface, and Boxed satisfies
// Encoder through it. A walk that only reads struct fields sees neither
// the embed nor the type it makes an encoder of.
type Named interface{ Encoder }

// Boxed embeds an interface that embeds Encoder.
type Boxed struct{ Named }

// Partial declares Encode and not Name, so it is not an Encoder — the
// case a walk matching the method name alone counts wrongly.
type Partial struct{}

func (Partial) Encode(out *[]byte, img image.Image, cols, rows, cellW, cellH int) error {
	return nil
}

// Pointed declares both methods ON THE POINTER, which is the ordinary
// shape for an encoder holding a palette cache or a reusable scratch
// buffer. It is here because it is the one satisfier graphics/ itself
// cannot hold: the walk reports the name with the pointer STRIPPED
// (receiverName), while only &Pointed{} can be assigned to an
// Encoder-typed field. Until review of #474 measured it, the table in
// opaque_test.go demanded both spellings at once and had no satisfying
// state — a row keyed "Pointed" whose value printed as
// "*graphics.Pointed" was rejected, and the value that printed without
// the star did not compile.
//
// Note the assertion above is on &Pointed{}, not Pointed{}. That is not
// a style choice: Pointed{} does not implement Encoder, so writing it
// there would not build.
type Pointed struct{ scratch []byte }

func (p *Pointed) Name() string { return "pointed" }

func (p *Pointed) Encode(out *[]byte, img image.Image, cols, rows, cellW, cellH int) error {
	return nil
}

// Shaped declares both names and the WRONG Encode signature.
type Shaped struct{}

func (Shaped) Name() string { return "shaped" }

func (Shaped) Encode(out *[]byte) error { return nil }
