package grpc

import (
	"testing"

	"github.com/WonderForgeLabs/gooey/control"
)

// TestAnImageWithNoBytesRoundTripsToNoImage — encode and decode must be
// inverses, and for KindImage they were not.
//
// valueToProto emits ImageBytesOf(v.Image) for KindImage, which is empty in
// two ordinary cases: the value carries no image at all, and the image was
// built in-process so it never had source bytes to keep. Both came back
// through valueFromProto, were handed to the decoder anyway, and produced
// InvalidArgument "image bytes did not decode" followed by a list of the
// host's formats — a malformed-file answer about a value this same server
// had just serialized a moment earlier.
//
// This is an INTERNAL test (package grpc) because the converters are
// unexported and the round trip is exactly the pair being asserted; going
// through the RPC surface would test the transport as well and would not
// pin the asymmetry to the two functions that had it.
func TestAnImageWithNoBytesRoundTripsToNoImage(t *testing.T) {
	tv := valueToProto(control.ImageValue(nil))
	if tv == nil {
		t.Fatal("a KindImage value did not encode at all")
	}
	got, err := valueFromProto(tv)
	if err != nil {
		t.Fatalf("an image with no source bytes failed to decode: %v\n"+
			"encode and decode must be inverses, and this value came straight "+
			"out of valueToProto", err)
	}
	if got.Kind != control.KindImage {
		t.Errorf("kind = %v, want KindImage", got.Kind)
	}
	if got.Image != nil {
		t.Errorf("image = %v, want nil — there were no bytes to build one from", got.Image)
	}
}

// TestGenuinelyBadImageBytesAreStillRejected is the discrimination half.
//
// The fix above makes the EMPTY case legal, and an over-broad version of it
// — treating any decode failure as "no picture" — would swallow real
// corruption and hand the app a blank image with no error. Non-empty bytes
// that decode to nothing must still be an error.
func TestGenuinelyBadImageBytesAreStillRejected(t *testing.T) {
	tv := valueToProto(control.DecodedImageValue(nil, []byte("this is not an image")))
	if tv == nil {
		t.Fatal("did not encode")
	}
	if _, err := valueFromProto(tv); err == nil {
		t.Error("undecodable bytes were accepted; only the EMPTY case means " +
			"\"no picture\", and corruption must still be reported")
	}
}
