package control

// Value.Equal's image branch, which runs on the per-frame delta path.
//
// grpc/session.go diffs each session's `last` map against the current
// values once per frame per session, so this function is called on the
// UI goroutine at frame rate. A panic here is not one client's error —
// it takes the process, and with it every other session's UI.
//
// The hazard is specific: comparing two interface values panics when
// their dynamic type is identical and NOT comparable. SourceImage is
// exactly that — a struct embedding a []byte — and its fields are
// exported, so a caller can build one with nil Bytes without going
// through DecodedImageValue. The bytes fast path does not cover it,
// because that path requires BOTH sides to carry bytes.

import (
	"image"
	"testing"
)

func imgValue(i image.Image) Value { return Value{Kind: KindImage, Image: i} }

// The regression this guard exists for. Before it, this test panicked
// rather than failed — which is the distinction that made the bug worth
// blocking a merge over.
func TestEqualDoesNotPanicOnTwoSourceImagesWithoutBytes(t *testing.T) {
	a := imgValue(SourceImage{Image: image.NewRGBA(image.Rect(0, 0, 2, 2))})
	b := imgValue(SourceImage{Image: image.NewRGBA(image.Rect(0, 0, 2, 2))})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Equal panicked comparing two byte-less SourceImages: %v\n"+
				"this runs once per frame per session on the UI goroutine — a panic "+
				"here is every session's UI, not just the sender's", r)
		}
	}()

	// Two distinct pictures that cannot be compared: reporting "changed"
	// is the safe answer. A spurious repaint costs a frame; the other
	// direction costs the process.
	if a.Equal(b) {
		t.Error("two incomparable images reported equal — the delta path would go blind to a real change")
	}
}

func TestEqualComparesSourceImagesByTheirBytes(t *testing.T) {
	px := image.NewRGBA(image.Rect(0, 0, 1, 1))
	same := []byte{1, 2, 3}
	if !imgValue(SourceImage{Image: px, Bytes: same}).
		Equal(imgValue(SourceImage{Image: px, Bytes: []byte{1, 2, 3}})) {
		t.Error("identical source bytes reported unequal — every frame would re-send the same picture")
	}
	if imgValue(SourceImage{Image: px, Bytes: same}).
		Equal(imgValue(SourceImage{Image: px, Bytes: []byte{9}})) {
		t.Error("different source bytes reported equal")
	}
}

// The common in-process case: a picture built by the app, held as a
// pointer. Pointer identity is comparable and cheap, and keeping it is
// the reason the guard is an allowlist rather than a blanket "never
// use ==" — the frame path should not report a repaint for an image
// that genuinely did not change.
func TestEqualUsesPointerIdentityForStandardImages(t *testing.T) {
	p := image.NewRGBA(image.Rect(0, 0, 4, 4))
	if !imgValue(p).Equal(imgValue(p)) {
		t.Error("the same *image.RGBA reported unequal — every frame would repaint it")
	}
	q := image.NewRGBA(image.Rect(0, 0, 4, 4))
	if imgValue(p).Equal(imgValue(q)) {
		t.Error("two distinct *image.RGBA reported equal")
	}
	// Different concrete types must not be compared with ==, which would
	// be false anyway, but must also not panic on the way there.
	if imgValue(p).Equal(imgValue(image.NewGray(image.Rect(0, 0, 4, 4)))) {
		t.Error("different image types reported equal")
	}
}

func TestEqualHandlesNilImages(t *testing.T) {
	// A page may bind <Image Src="{{.Logo}}"> before any picture exists,
	// so nil is a legal value and must compare without a special case at
	// every call site.
	if !imgValue(nil).Equal(imgValue(nil)) {
		t.Error("two unset images reported unequal — an app with no logo would repaint forever")
	}
	if imgValue(nil).Equal(imgValue(image.NewRGBA(image.Rect(0, 0, 1, 1)))) {
		t.Error("unset and set reported equal — setting the first picture would not be delivered")
	}
	if imgValue(image.NewRGBA(image.Rect(0, 0, 1, 1))).Equal(imgValue(nil)) {
		t.Error("set and unset reported equal — clearing a picture would not be delivered")
	}
}

// A non-comparable type that is NOT SourceImage: the allowlist's default
// arm has to catch this too, or adding an image type to the framework
// reintroduces the panic.
type sliceBackedImage struct {
	image.Image
	pix []byte
}

func TestEqualIsSafeForAnUnknownNonComparableImageType(t *testing.T) {
	base := image.NewRGBA(image.Rect(0, 0, 1, 1))
	a := imgValue(sliceBackedImage{Image: base, pix: []byte{1}})
	b := imgValue(sliceBackedImage{Image: base, pix: []byte{1}})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Equal panicked on an unknown non-comparable image type: %v\n"+
				"the allowlist's default arm is what makes adding an image type safe", r)
		}
	}()
	if a.Equal(b) {
		t.Error("an incomparable custom type reported equal")
	}
}
