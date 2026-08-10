// How-to: forms — rules declared as <Validate> behaviors in markup,
// inline error rows, and a submit button that enables itself.
//
//	cd docs/learn/examples/howto-forms && go run .
//
// Walkthrough: docs/learn/howto/howto-forms.md
package main

import (
	"context"
	"os"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func main() {
	var app *gooey.App

	// The fields are ordinary sources. The RULES live in the markup:
	// each <Validate> behavior materializes a validator computed against
	// its field and publishes it in the context (NameErr, EmailErr,
	// TagErr — derived from the Text binding paths).
	name := prop.NewSource("")
	email := prop.NewSource("")
	tag := prop.NewSource("")

	status := prop.NewSource("fill the form — submit enables when it is valid")

	ctx := &markup.Context{
		Values: map[string]any{
			"Status": status,
			"Name":   name,
			"Email":  email,
			"Tag":    tag,
			"Quit":   gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
			"err":    {Fg: render.RGB(235, 90, 85)},
		},
	}

	// The submit gate reads the PUBLISHED error properties. The lookup
	// happens inside the computed, at evaluation — the properties do not
	// exist until the page loads, and a hot reload republishes fresh
	// ones. (A viewmodel that owns its validators in Go gates with
	// validate.All instead — see the how-to.)
	canSubmit := prop.NewComputed(func() bool {
		for _, k := range []string{"NameErr", "EmailErr", "TagErr"} {
			p, ok := ctx.Values[k].(*prop.Property[string])
			if !ok || p.Get() != "" {
				return false
			}
		}
		return true
	})
	ctx.Values["Submit"] = gooey.NewCommand(func() {
		status.Set("saved: " + name.Get() + " <" + email.Get() + ">")
	}).When(canSubmit)

	app = gooey.NewApp(markup.Page(os.DirFS("."), "app.gooey", ctx))
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
