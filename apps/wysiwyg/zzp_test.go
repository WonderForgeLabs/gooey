package main

import (
	"testing"

	"github.com/WonderForgeLabs/gooey/markup"
)

func TestZZP(t *testing.T) {
	env := map[string]string{"xmlns:p": markup.XNamespace}
	d := &node{Elem: "Property", Attrs: map[string]string{
		"xmlns": markup.XNamespace, "xmlns:p": "urn:other", "Name": "T"}}
	p, b := declPrefix(env, []*node{d})
	t.Logf("declPrefix=%q bound=%v spentElsewhere=%v", p, b, spentElsewhere("p", []*node{d}))
}
