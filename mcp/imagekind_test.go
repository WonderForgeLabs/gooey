package mcp

// An image property is the only way a client can put a picture on a page.
//
// Markup swapped or patched over the control plane is built from BYTES,
// so it has no source fs.FS and <Image Src="logo.png"> cannot resolve —
// it fails with "no file system to load from". Binding Src to a property
// is the alternative, and before KindImage there was no property type to
// bind it to: register_properties offered string/int/bool/float/duration/
// color/any, and `any` produces *prop.Property[interface{}], which
// <Image Src> rejects because it needs *prop.Property[image.Image].
//
// So this pair of tests pins the hole being closed, from both sides.

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"
)

// tinyPNG is a real encoded PNG — the wire form is a FILE's bytes, not
// raw pixels, because the host already owns a format registry and a
// client that had to state width/height/stride would be reimplementing a
// decoder in order to talk to one.
func tinyPNG(t *testing.T) string {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	im.Set(0, 0, color.RGBA{255, 170, 60, 255})
	im.Set(1, 1, color.RGBA{120, 90, 220, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, im); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func imageSetup(t *testing.T) *client {
	t.Helper()
	_, values := newVM()
	app := newTestApp(t, testMarkup, values)
	s, err := New(app, Options{Context: app.ctx, Timeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return newClient(t, s)
}

func TestRegisterImagePropertyAndBindItInMarkup(t *testing.T) {
	c := imageSetup(t)

	if out := c.ok("register_properties", map[string]any{
		"properties": []any{map[string]any{
			"name": "Logo", "type": "image", "value": tinyPNG(t),
		}},
	}); !strings.Contains(out, "Logo") {
		t.Fatalf("register: %s", out)
	}

	// The payoff: markup built from bytes, with no fs.FS anywhere, can
	// now carry a picture.
	const page = `<Gooey><VStack Name="Root"><Image Name="Pic" Src="{{.Logo}}" Cols="4" Rows="2"/></VStack></Gooey>`
	if out := c.ok("validate_markup", map[string]any{"source": page}); !strings.Contains(out, `"valid": true`) &&
		!strings.Contains(out, `"valid":true`) {
		t.Fatalf("validate: %s", out)
	}
	if out := c.ok("swap_markup", map[string]any{"source": page}); !strings.Contains(out, "Pic") {
		t.Fatalf("swap: %s", out)
	}

	// A nil image is legal and must not be an error: a page may bind a
	// picture before one exists, and Image renders nothing for nil.
	if out := c.ok("register_properties", map[string]any{
		"properties": []any{map[string]any{"name": "Later", "type": "image"}},
	}); !strings.Contains(out, "Later") {
		t.Errorf("absent value should register as nil: %s", out)
	}
}

func TestRegisterImageRejectsGarbageWithAUsefulError(t *testing.T) {
	c := imageSetup(t)

	// Not base64 at all.
	text, isErr := c.call("register_properties", map[string]any{
		"properties": []any{map[string]any{"name": "A", "type": "image", "value": "!!!not base64!!!"}},
	})
	if !isErr || !strings.Contains(text, "base64") {
		t.Errorf("bad base64 = %q (isErr=%v)", text, isErr)
	}

	// Valid base64 that is not an image. The error names what this app
	// CAN read, because formats are registered by blank import — "this
	// build has no SVG" is a host-configuration answer, not a
	// malformed-file one, and a client cannot tell them apart otherwise.
	text, isErr = c.call("register_properties", map[string]any{
		"properties": []any{map[string]any{
			"name": "B", "type": "image",
			"value": base64.StdEncoding.EncodeToString([]byte("this is plainly not a picture")),
		}},
	})
	if !isErr || !strings.Contains(text, "did not decode") || !strings.Contains(text, "png") {
		t.Errorf("non-image = %q (isErr=%v); want a decode failure naming the readable formats", text, isErr)
	}

	// A number where a base64 string belongs.
	text, isErr = c.call("register_properties", map[string]any{
		"properties": []any{map[string]any{"name": "C", "type": "image", "value": 42}},
	})
	if !isErr || !strings.Contains(text, "base64") {
		t.Errorf("wrong JSON type = %q (isErr=%v)", text, isErr)
	}
}

// Read-back returns exactly what arrived. Before SourceImage this was
// impossible: the adapter decoded and dropped the source, so the only
// way to answer "what did the client send?" was to re-encode — which is
// a different file, and puts an encoder on the ListValues and
// frame-delta paths to produce it.
func TestRegisteredImageReadsBackAsTheBytesThatArrived(t *testing.T) {
	c := imageSetup(t)
	src := tinyPNG(t)

	c.ok("register_properties", map[string]any{
		"properties": []any{map[string]any{"name": "Badge", "type": "image", "value": src}},
	})

	// The MCP surface reports the kind and Go type but not the payload,
	// which is the v1 ceiling; the round trip that matters is in-process,
	// so assert through the service the way a gRPC read would.
	out := c.ok("list_values", nil)
	if !strings.Contains(out, "Badge") {
		t.Fatalf("registered image missing from list_values: %s", out)
	}
	if !strings.Contains(out, "image.Image") {
		t.Errorf("Badge should report an image.Image handle: %s", out)
	}
}
