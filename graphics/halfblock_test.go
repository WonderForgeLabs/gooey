package graphics

import (
	"image"
	"image/color"
	"testing"

	"github.com/WonderForgeLabs/gooey/render"
)

// translucent is one pixel of the fixture below, described the way a
// human would: the colour it IS, and how transparent it is. image.RGBA
// stores neither of those directly — it stores the product — so the
// fixture has to do the multiplication itself, exactly as alphaRamp does
// in sixel_test.go.
type translucent struct {
	a       uint8 // alpha
	r, g, b uint8 // the colour the pixel actually is, before premultiplication
}

// premul is what image.RGBA holds for this pixel, and therefore what
// color.Color.RGBA hands back to DrawHalfblock.
func (p translucent) premul() color.RGBA {
	return color.RGBA{
		R: uint8(uint32(p.r) * uint32(p.a) / 0xff),
		G: uint8(uint32(p.g) * uint32(p.a) / 0xff),
		B: uint8(uint32(p.b) * uint32(p.a) / 0xff),
		A: p.a,
	}
}

// blendAgainstBlack is the answer DrawHalfblock must give: the
// premultiplied channels straight through, which is precisely this
// pixel composited over black.
func (p translucent) blendAgainstBlack() render.Color {
	c := p.premul()
	return render.RGB(c.R, c.G, c.B)
}

// ownColour is the answer it must NOT give: what un-premultiplying
// recovers, i.e. the pixel at full strength as though it were opaque.
//
// This is exact, not approximate, for the alphas below. sixel.go's
// unpremul computes v*0xffff/a >> 8 on the 16-bit pair, and for a
// premultiplied byte P with alpha byte A that is P*0xffff/A >> 8; every
// column here was built so P*0xff/A lands back on the original byte with
// no remainder, so an un-premultiplying DrawHalfblock would produce
// exactly these values and the failure message can quote them.
func (p translucent) ownColour() render.Color { return render.RGB(p.r, p.g, p.b) }

// halfblockColumns is four one-cell columns, each two pixels tall, so a
// single row of halfblocks exercises Fg (the top pixel) and Bg (the
// bottom one) at the same time — and with different hues per row, a
// change that swapped them would not pass either.
//
// The image is 4x2 and it is drawn into 4 cols x 1 row, so Scale runs at
// 1:1 and is the identity. That is deliberate: this test is about the
// alpha decision alone, and resampling is pinned separately in
// scale_test.go.
//
// # Why these alphas and these hues
//
// The whole value of the test is that the two candidate answers are far
// apart, and it is easy to build a fixture where they are not. This PR
// had to repair exactly that mistake on the sixel side, where the
// fixture was color.RGBA{a, a, a, a} — grey scaled by alpha, which is
// WHITE at every alpha — so "its own colour" and "its premultiplied
// contribution" differed only in brightness and no count could separate
// them.
//
// So: saturated primaries at LOW alpha, where premultiplication bites
// hardest. A 25%-alpha pure red is stored as (64,0,0) and its own
// colour is (255,0,0): 191 of 255 apart on a channel, three quarters of
// the range. The 50% column is 127 apart. Both are far outside anything
// a rounding difference could produce, so a red run names a real
// disagreement about the decision rather than an off-by-one.
var halfblockColumns = []struct {
	name     string
	top, bot translucent
}{
	// 25% alpha: the largest disagreement, and the regime where the
	// resampled fringe of a soft edge actually lands.
	{"quarter alpha", translucent{0x40, 0xff, 0x00, 0x00}, translucent{0x40, 0x00, 0xff, 0x00}},
	// 50% alpha: the sixel encoder's keep/drop threshold, which
	// halfblock does not have — every pixel is painted here, so this
	// column is in the picture at half strength rather than in or out.
	{"half alpha", translucent{0x80, 0x00, 0xff, 0xff}, translucent{0x80, 0xff, 0xff, 0x00}},
	// Opaque: the control. Both readings agree here, so if this column
	// ever disagrees the fault is in the fixture or in Scale, not in the
	// alpha decision.
	{"opaque", translucent{0xff, 0xdc, 0x78, 0x28}, translucent{0xff, 0x46, 0x5a, 0xc8}},
	// Fully transparent. This column CANNOT tell the two answers apart
	// (un-premultiplying divides by alpha and must guard a == 0), so it
	// is not evidence for the claim above. It pins the other half of
	// "alpha is discarded": a cell has no transparent state, so an
	// invisible pixel is painted black rather than left alone. A change
	// that made DrawHalfblock skip transparent pixels would fail here,
	// and should — that is a different decision and deserves its own
	// argument.
	{"transparent", translucent{0x00, 0xff, 0x00, 0x00}, translucent{0x00, 0xff, 0xff, 0xff}},
}

func halfblockFixture() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, len(halfblockColumns), 2))
	for x, c := range halfblockColumns {
		img.SetRGBA(x, 0, c.top.premul())
		img.SetRGBA(x, 1, c.bot.premul())
	}
	return img
}

// TestATranslucentPixelIsPaintedAsItsBlendAgainstBlackNotItsOwnColour is
// the halfblock half of the alpha decision, and it exists because the
// sixel half now does the OPPOSITE and looks, from a distance, like the
// same problem solved twice differently.
//
// Sixel can decline to write a pixel, so a fringe pixel it keeps is the
// icon itself and is owed its own colour back —
// TestAKeptPixelIsDeclaredAtItsOwnColourNotItsPremultipliedOne pins that.
// A cell has a foreground and a background and nothing else; there is no
// "leave it". So a fringe pixel here is unavoidably a BLEND, and the
// premultiplied channels out of RGBA() already ARE that blend against
// black. Un-premultiplying them by analogy with sixel would restore every
// soft edge to full strength and ring the image in a bright halo.
//
// Nothing pinned that until now, while the sixel side gained two tests in
// the same change — which is the asymmetry that invites the "fix". A
// resampling Scale manufactures partial alpha along every transparency
// boundary there is, so the halo would appear on assets that never had a
// translucent pixel in them.
func TestATranslucentPixelIsPaintedAsItsBlendAgainstBlackNotItsOwnColour(t *testing.T) {
	buf := render.NewBuffer(len(halfblockColumns), 1)
	DrawHalfblock(buf, halfblockFixture(), 0, 0, len(halfblockColumns), 1)

	for x, c := range halfblockColumns {
		cell := buf.At(x, 0)
		if cell.Rune != '▀' {
			t.Fatalf("column %d (%s): rune %q, want the upper-half block", x, c.name, cell.Rune)
		}
		for _, part := range []struct {
			which string
			got   render.Color
			px    translucent
		}{
			{"Fg (top pixel)", cell.Style.Fg, c.top},
			{"Bg (bottom pixel)", cell.Style.Bg, c.bot},
		} {
			want := part.px.blendAgainstBlack()
			if part.got == want {
				continue
			}
			msg := "column %d (%s) %s = %v, want %v — the premultiplied channels " +
				"ARE the blend against black, and a cell has no transparent state to " +
				"leave the rest to"
			if part.got == part.px.ownColour() {
				msg += "\n\tthe value seen is this pixel's UN-premultiplied colour: " +
					"DrawHalfblock is dividing out the alpha, which paints every " +
					"soft edge at full strength and halos the image"
			}
			t.Errorf(msg, x, c.name, part.which, part.got, want)
		}
	}
}
