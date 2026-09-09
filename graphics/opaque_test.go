package graphics

import "testing"

// OpaqueEncoder's doc called a mis-classified fourth protocol "the
// failure this whole interface exists to make impossible". It was not
// impossible: the mapping is opt-in, and review of #474 measured it —
// adding OpaqueOnly() to Kitty was caught by a downstream pixel test,
// and adding it to ITerm2 left the ENTIRE root suite green.
//
// ITerm2 is the one the interface's own doc names by hand as taking the
// composited branch, and marking it alpha-less changes the picture every
// iTerm2/WezTerm/mintty user gets. A claim about which encoders answer a
// capability belongs where the capability is declared, not only in one
// consumer's pixel test — a second consumer would have to rediscover it.
var _ OpaqueEncoder = Sixel{}

// TestOnlySixelIsAlphaLess is the table half. The compile-time assertion
// above says Sixel implements it; nothing but this says the others do
// NOT, and "does not implement" is the half that cannot be asserted at
// compile time.
//
// IT IS DERIVED FROM THE ENCODER SET, not a list of three names. A
// fourth encoder added without a row here fails the count, which is what
// makes this a guard rather than a description — the same reason
// CLAUDE.md's verify loop discovers modules instead of listing them.
func TestOnlySixelIsAlphaLess(t *testing.T) {
	// Every encoder this package defines, with the answer it must give.
	// Adding one means adding a row, and that IS the review moment: the
	// question "can this protocol carry alpha?" has to be answered
	// deliberately, because the default — not implementing the interface
	// — is the composited branch, and a wrong default is silent.
	encoders := []struct {
		enc    Encoder
		opaque bool
	}{
		{Sixel{}, true},
		{Kitty{}, false},
		{ITerm2{}, false},
	}
	for _, tc := range encoders {
		_, got := tc.enc.(OpaqueEncoder)
		if got == tc.opaque {
			continue
		}
		if tc.opaque {
			t.Errorf("%s does not declare OpaqueEncoder. Its wire carries no alpha, "+
				"so a caller drawing a faint line will hand it a translucent stroke "+
				"and the line will be discarded rather than dimmed — which is #254, "+
				"exactly", tc.enc.Name())
			continue
		}
		t.Errorf("%s declares OpaqueEncoder. It transmits through png.Encode, so the "+
			"TERMINAL composites against its own background — an answer no "+
			"arithmetic in a component can improve on. Marking it alpha-less "+
			"replaces that with a guessed ground for every user of that terminal",
			tc.enc.Name())
	}

	// NON-VACUITY, and it is the half that keeps the table honest. A
	// fourth encoder with no row here is not covered by the loop above at
	// all: it would take the composited branch by default, silently,
	// which is the failure the interface is for.
	if len(encoders) != len(allEncoders()) {
		t.Errorf("this table has %d encoders and the package defines %d. The one "+
			"missing a row takes the composited branch by default — a decision "+
			"nobody made, about a protocol nobody asked",
			len(encoders), len(allEncoders()))
	}
}

// allEncoders is every Encoder implementation in this package. It is
// hand-listed for the same reason the table is: Go cannot enumerate
// implementations of an interface without reflection, which core does
// not use. The two lists exist so that adding an encoder to one and not
// the other goes red — a check the single list could not make on itself.
func allEncoders() []Encoder {
	return []Encoder{Sixel{}, Kitty{}, ITerm2{}}
}
