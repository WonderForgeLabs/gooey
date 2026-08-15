// intro: gooey, from nothing.
package main

import (
	"context"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
)

func main() {
	app := gooey.NewApp(gooey.Tree(&components.Text{}))
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
