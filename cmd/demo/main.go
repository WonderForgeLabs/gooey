// demo renders a retained component tree with an image through the best
// (or forced) graphics mode.
//
//	demo                 auto-detect protocol, draw, wait for a key
//	demo --mode=sixel    force a protocol (kitty|sixel|iterm2|halfblock)
//	demo --dump          render one frame to stdout without raw mode
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

func main() {
	mode := flag.String("mode", "", "force graphics mode: kitty|sixel|iterm2|halfblock")
	dump := flag.Bool("dump", false, "render one frame to stdout (no tty control)")
	hold := flag.Duration("hold", 0, "exit after this duration instead of waiting for a key")
	flag.Parse()

	caps := term.Caps{Cols: 80, Rows: 24, CellW: 10, CellH: 20}
	var screen *term.Screen
	if !*dump {
		var err error
		screen, err = term.Open()
		if err != nil {
			fmt.Fprintln(os.Stderr, "no tty:", err)
			os.Exit(1)
		}
		if caps2, err := screen.Detect(); err == nil {
			caps = caps2
		}
	}

	selected := caps.Best()
	if *mode != "" {
		selected = *mode
	}
	var enc graphics.Encoder
	switch selected {
	case "kitty":
		enc = graphics.Kitty{}
	case "sixel":
		enc = graphics.Sixel{}
	case "iterm2":
		enc = graphics.ITerm2{}
	case "halfblock":
		enc = nil
	default:
		fmt.Fprintln(os.Stderr, "unknown mode:", selected)
		os.Exit(2)
	}

	root := buildTree(selected, caps)
	frame := gooey.Compose(root, caps, enc)

	if *dump {
		frame.Flush(os.Stdout)
		fmt.Println()
		return
	}

	if err := screen.Raw(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	frame.Flush(screen.File())
	if *hold > 0 {
		time.Sleep(*hold)
	} else {
		b := make([]byte, 1)
		screen.File().Read(b) // any key
	}
	screen.Restore()
}

func buildTree(mode string, caps term.Caps) gooey.Component {
	accent := render.Style{Fg: render.RGB(255, 170, 60), Bold: true}
	dim := render.Style{Fg: render.RGB(140, 140, 150)}

	info := &components.VStack{Children: []gooey.Component{
		&components.Text{Content: components.Str("gooey — retained visual tree POC"), Style: components.Sty(accent)},
		&components.Text{},
		&components.Text{Content: components.Str(fmt.Sprintf("graphics mode : %s", mode)), Style: components.Sty(render.Style{Bold: true})},
		&components.Text{Content: components.Str(fmt.Sprintf("kitty=%v sixel=%v iterm2=%v", caps.Kitty, caps.Sixel, caps.ITerm2)), Style: components.Sty(dim)},
		&components.Text{Content: components.Str(fmt.Sprintf("cell size     : %d×%d px", caps.CellW, caps.CellH)), Style: components.Sty(dim)},
		&components.Text{Content: components.Str(fmt.Sprintf("color depth   : %s", caps.Color)), Style: components.Sty(dim)},
		&components.Text{},
		&components.Text{Content: components.Str("tree: Border > VStack > [HStack >\n[Image, VStack > Text×N], Text]"), Style: components.Sty(dim)},
	}}

	body := &components.VStack{Gap: 1, Children: []gooey.Component{
		&components.HStack{Gap: 2, Children: []gooey.Component{
			&components.Image{Src: logo(), Cols: 24, Rows: 12},
			info,
		}},
		&components.Text{Content: components.Str("press any key to exit"), Style: components.Sty(dim)},
	}}

	return &components.Border{Child: body, Title: components.Str("gooey"), Style: components.Sty(render.Style{Fg: render.RGB(120, 90, 220)})}
}

// logo renders a plasma-ish gradient with a ring — enough color variety
// to show quantization quality differences between the protocols.
func logo() image.Image {
	const w, h = 240, 240
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx, fy := float64(x)/w, float64(y)/h
			r := uint8(90 + 120*math.Sin(fx*6)*math.Cos(fy*4) + 45)
			g := uint8(60 + 160*fy)
			b := uint8(200 - 120*fx)
			dx, dy := fx-0.5, fy-0.5
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist > 0.30 && dist < 0.38 {
				r, g, b = 255, 220, 120
			}
			img.Set(x, y, color.RGBA{r, g, b, 255})
		}
	}
	return img
}
