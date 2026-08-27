package envhandlers_test

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	envhandlers "github.com/WonderForgeLabs/gooey/handlers/env"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// row is the row as a terminal would read it, trailing blanks trimmed.
// The readback itself is render.RowText, which is where the
// continuation markers get skipped: building the string here cell by
// cell rendered them as literal runes, so no fixture in this package
// could hold a wide glyph and be asserted on.
func row(b *render.Buffer, y int) string {
	return strings.TrimRight(render.RowText(b, y), " ")
}

// grant registers a provider for one test and revokes both halves
// afterwards, so a test can never leak a capability into the next one.
func grant(t *testing.T, p *envhandlers.Provider, writable bool) {
	t.Helper()
	markup.RegisterValues(envhandlers.URI, p)
	if writable {
		markup.RegisterHandlers(envhandlers.URI, p)
	}
	t.Cleanup(func() {
		markup.RegisterValues(envhandlers.URI, nil)
		markup.RegisterHandlers(envhandlers.URI, nil)
	})
}

func load(t *testing.T, body string, vals map[string]any) (gooey.Component, error) {
	t.Helper()
	src := `<Gooey xmlns:env="` + envhandlers.URI + `">` + body + `</Gooey>`
	if vals == nil {
		vals = map[string]any{}
	}
	return markup.Build([]byte(src), &markup.Context{Values: vals, Dispatcher: gooey.NewDispatcher()})
}

func TestGetReadsAGrantedVariable(t *testing.T) {
	p := envhandlers.New("USER").Configure(envhandlers.WithEnviron(map[string]string{"USER": "ada"}))
	grant(t, p, false)

	w, err := load(t, "<Text>hi {{env:Get `USER`}}</Text>", nil)
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 20, 1)
	c.Frame()
	if got := row(c.Cells(), 0); got != "hi ada" {
		t.Fatalf("row = %q, want %q", got, "hi ada")
	}
}

// A granted variable that is not set reads empty, and the optional
// second argument is how a page says what to show instead.
func TestGetFallsBackWhenUnset(t *testing.T) {
	p := envhandlers.New("EDITOR").Configure(envhandlers.WithEnviron(map[string]string{}))
	grant(t, p, false)

	w, err := load(t, "<Text>{{env:Get `EDITOR` `(none)`}}</Text>", nil)
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 20, 1)
	c.Frame()
	if got := row(c.Cells(), 0); got != "(none)" {
		t.Fatalf("row = %q, want %q", got, "(none)")
	}
}

// The hoisted-Get rule, pinned. The fallback may be a bound path, and
// while the variable is set that path's Get must STILL run, or the
// component goes deaf to the fallback and never repaints when the
// fallback is the thing that changed.
func TestBoundFallbackStaysSubscribedWhileUnused(t *testing.T) {
	p := envhandlers.New("EDITOR").Configure(envhandlers.WithEnviron(map[string]string{"EDITOR": "vi"}))
	grant(t, p, false)
	fb := prop.NewSource("(none)")

	w, err := load(t, "<Text>{{env:Get `EDITOR` .Fallback}}</Text>", map[string]any{"Fallback": fb})
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 20, 1)
	c.Frame()
	if got := row(c.Cells(), 0); got != "vi" {
		t.Fatalf("row = %q, want vi", got)
	}

	// The variable is set, so the fallback is not being displayed — but
	// the dependency must exist all the same.
	fb.Set("(unset)")
	_, painted := c.Frame()
	if painted != 1 {
		t.Errorf("changing the unused fallback painted %d components, want 1 — the Get was not hoisted", painted)
	}
}

// The writable grant is the reactive one: env:Set writes the process
// environment AND the source property, so a Text bound to env:Get
// updates through the ordinary graph, repainting exactly itself.
func TestSetUpdatesReadersAndPaintsOnlyThem(t *testing.T) {
	p := envhandlers.NewWritable("EDITOR").Configure(envhandlers.WithEnviron(map[string]string{"EDITOR": "vi"}))
	grant(t, p, true)

	src := `<VStack>
	  <Text>{{env:Get ` + "`EDITOR`" + `}}</Text>
	  <Text>static</Text>
	  <Button Content="vim" Click="{{env:Set ` + "`EDITOR` `vim`" + `}}"/>
	</VStack>`
	w, err := load(t, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 24, 3)
	c.Frame()
	if got := row(c.Cells(), 0); got != "vi" {
		t.Fatalf("row 0 = %q, want vi", got)
	}

	c.Focus().SetFocus(c.Focus().Order()[0])
	c.HandleKey(input.Named(input.KeyEnter))

	_, painted := c.Frame()
	if got := row(c.Cells(), 0); got != "vim" {
		t.Fatalf("row 0 = %q after env:Set, want vim", got)
	}
	// Exactly the reader. The static Text does not repaint, and neither
	// does the Button — the Composer had already focused it on the first
	// frame, so SetFocus was a no-op and the only change in the graph was
	// the source env:Set wrote.
	if painted != 1 {
		t.Errorf("env:Set painted %d components, want 1 (the reader alone)", painted)
	}
}

func TestUnsetClearsTheSource(t *testing.T) {
	p := envhandlers.NewWritable("EDITOR").Configure(envhandlers.WithEnviron(map[string]string{"EDITOR": "vi"}))
	grant(t, p, true)

	src := `<VStack>
	  <Text>[{{env:Get ` + "`EDITOR`" + `}}]</Text>
	  <Button Content="clear" Click="{{env:Unset ` + "`EDITOR`" + `}}"/>
	</VStack>`
	w, err := load(t, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 24, 2)
	c.Frame()
	c.Focus().SetFocus(c.Focus().Order()[0])
	c.HandleKey(input.Named(input.KeyEnter))
	c.Frame()
	if got := row(c.Cells(), 0); got != "[]" {
		t.Fatalf("row 0 = %q after env:Unset, want []", got)
	}
}

// Two bindings to one variable share a source, so one write updates
// both. That is a consequence of caching per name, and it is the
// behaviour a page expects.
func TestTwoReadersShareOneSource(t *testing.T) {
	p := envhandlers.NewWritable("K").Configure(envhandlers.WithEnviron(map[string]string{"K": "a"}))
	grant(t, p, true)

	src := `<VStack>
	  <Text>{{env:Get ` + "`K`" + `}}</Text>
	  <Text>{{env:Get ` + "`K`" + `}}</Text>
	  <Button Content="b" Click="{{env:Set ` + "`K` `b`" + `}}"/>
	</VStack>`
	w, err := load(t, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 20, 3)
	c.Frame()
	c.Focus().SetFocus(c.Focus().Order()[0])
	c.HandleKey(input.Named(input.KeyEnter))
	c.Frame()
	if row(c.Cells(), 0) != "b" || row(c.Cells(), 1) != "b" {
		t.Fatalf("rows = %q,%q after Set, want b,b", row(c.Cells(), 0), row(c.Cells(), 1))
	}
}

func TestNamesRendersTheGrant(t *testing.T) {
	grant(t, envhandlers.New("USER", "HOME", "TERM"), false)
	w, err := load(t, "<Text>{{env:Names}}</Text>", nil)
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 40, 1)
	c.Frame()
	if got := row(c.Cells(), 0); got != "HOME, TERM, USER" {
		t.Fatalf("row = %q, want the sorted grant", got)
	}
}

// Every one of these is a LOAD error with a message that names the rule
// it broke. err != nil would pass for all of them and prove nothing.
func TestEnvLoadErrors(t *testing.T) {
	cases := []struct {
		name     string
		writable bool
		grantee  []string
		body     string
		want     string
	}{
		{"name outside the grant", false, []string{"HOME"},
			"<Text>{{env:Get `AWS_SECRET_ACCESS_KEY`}}</Text>",
			`"AWS_SECRET_ACCESS_KEY" is not in this host's environment grant; granted: HOME`},
		{"empty grant says so", false, nil,
			"<Text>{{env:Get `HOME`}}</Text>",
			"the host called envhandlers.New() with no names"},
		{"bound variable name", false, []string{"HOME"},
			"<Text>{{env:Get .Which}}</Text>",
			"takes a backtick literal variable name"},
		{"wrong arity", false, []string{"HOME"},
			"<Text>{{env:Get `HOME` `a` `b`}}</Text>",
			"Get takes the variable name and an optional fallback, got 3"},
		{"unknown value function", false, []string{"HOME"},
			"<Text>{{env:Frobnicate `HOME`}}</Text>",
			`unknown function "Frobnicate"; env reads: Get, Names`},
		{"Names takes no arguments", false, []string{"HOME"},
			"<Text>{{env:Names `x`}}</Text>",
			"Names takes no arguments, got 1"},
		{"write on a read-only grant", true, []string{"HOME"},
			"<Button Content=\"x\" Click=\"{{env:Set `HOME` `/tmp`}}\"/>",
			"needs a writable grant"},
		{"Set in a value position", false, []string{"HOME"},
			"<Text>{{env:Set `HOME` `/tmp`}}</Text>",
			"Set is an effect, not a value"},
		{"Get on an event attribute", true, []string{"HOME"},
			"<Button Content=\"x\" Click=\"{{env:Get `HOME`}}\"/>",
			"Get is a value, not an effect"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// tc.writable selects which registration the page gets, not
			// which provider: the "write on a read-only grant" case
			// registers a READ-ONLY provider on the handler side, which
			// is exactly the misconfiguration it is testing.
			p := envhandlers.New(tc.grantee...).Configure(envhandlers.WithEnviron(map[string]string{}))
			grant(t, p, tc.writable)
			_, err := load(t, tc.body, map[string]any{"Which": prop.NewSource("HOME")})
			if err == nil {
				t.Fatalf("%s loaded clean; expected an error mentioning %q", tc.body, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// The refusal that matters most: an ungranted variable is a load error
// even though the process really has it set. The grant, not the
// environment, decides what markup can see.
func TestUngrantedVariableIsRefusedEvenWhenPresent(t *testing.T) {
	p := envhandlers.New("HOME").Configure(envhandlers.WithEnviron(map[string]string{
		"HOME": "/home/ada", "SECRET": "hunter2",
	}))
	grant(t, p, false)
	_, err := load(t, "<Text>{{env:Get `SECRET`}}</Text>", nil)
	if err == nil {
		t.Fatal("an ungranted variable that is set in the environment was readable")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("the refusal leaked the value: %v", err)
	}
	if !strings.Contains(err.Error(), "not in this host's environment grant") {
		t.Fatalf("error %q does not name the grant", err)
	}
}

func TestGrantedIsACopy(t *testing.T) {
	p := envhandlers.New("B", "A")
	g := p.Granted()
	if len(g) != 2 || g[0] != "A" || g[1] != "B" {
		t.Fatalf("Granted()=%v, want sorted [A B]", g)
	}
	g[0] = "MUTATED"
	if p.Granted()[0] != "A" {
		t.Fatal("Granted() handed out the provider's own slice")
	}
}

func TestAllNamesCoversBothHalves(t *testing.T) {
	got := strings.Join(envhandlers.AllNames(), ",")
	if got != "Get,Names,Set,Unset" {
		t.Fatalf("AllNames()=%q", got)
	}
}
