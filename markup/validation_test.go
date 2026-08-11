package markup

import (
	"fmt"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/validate"
)

// The validation surface from pure markup: the Error handle bound on
// the TextBox, the ValidationMarker as a KeyBinding-style attachment
// child (Error omitted — it adopts the host's), the layer last.
const validationPage = `<Gooey>
  <VStack>
    <TextBox Text="{{.Name}}" Error="{{.NameErr}}" InvalidStyle="bad">
      <ValidationMarker/>
    </TextBox>
    <Text>filler</Text>
    <AdornmentLayer/>
  </VStack>
</Gooey>`

func validationCtx() (*Context, *prop.Property[string], *prop.Property[string]) {
	name := prop.NewSource("")
	nameErr := validate.Field(name, validate.Required(""))
	return &Context{
		Values: map[string]any{"Name": name, "NameErr": nameErr},
		Styles: map[string]render.Style{"bad": {Bold: true}},
	}, name, nameErr
}

func TestValidationMarkupBuilds(t *testing.T) {
	ctx, _, nameErr := validationCtx()
	root, err := Build([]byte(validationPage), ctx)
	if err != nil {
		t.Fatal(err)
	}
	stack := root.(*components.VStack)
	tb, ok := stack.ChildComponents()[0].(*components.TextBox)
	if !ok {
		t.Fatalf("first child is %T, want *components.TextBox", stack.ChildComponents()[0])
	}
	if tb.Error != nameErr {
		t.Fatal("<TextBox Error=…> did not bind the viewmodel's handle")
	}
	if tb.InvalidStyle == nil || !tb.InvalidStyle.Get().Bold {
		t.Fatal("InvalidStyle did not resolve from the named styles")
	}
	var m *components.ValidationMarker
	for _, a := range attachmentsOf(tb) {
		if vm, ok := a.(*components.ValidationMarker); ok {
			m = vm
		}
	}
	if m == nil {
		t.Fatal("<ValidationMarker> did not attach to its TextBox")
	}

	// End to end: composed, the marker adopts the host's Error and the
	// message floats under the field; fixing the field clears both.
	c := gooey.NewComposer(root, 24, 4)
	c.Frame()
	if m.Error != nameErr {
		t.Fatal("the attached marker did not adopt the host's Error handle")
	}
	// The float clamps into the layer's own bounds — in this VStack the
	// layer owns the rows below the content, so the message shows there.
	if got := rowOf(c, 2); !strings.Contains(got, " required ") {
		t.Fatalf("row 2 = %q, want the floating message", got)
	}
}

func rowOf(c *gooey.Composer, y int) string {
	var sb strings.Builder
	for x := 0; x < 24; x++ {
		sb.WriteRune(c.Cells().At(x, y).Rune)
	}
	return sb.String()
}

// An explicit Error on the marker overrides adoption — the form for
// marking a component that is not a TextBox.
func TestValidationMarkerExplicitError(t *testing.T) {
	page := `<Gooey>
  <VStack>
    <TextBox Text="{{.Name}}">
      <ValidationMarker Error="{{.NameErr}}"/>
    </TextBox>
    <AdornmentLayer/>
  </VStack>
</Gooey>`
	ctx, _, nameErr := validationCtx()
	root, err := Build([]byte(page), ctx)
	if err != nil {
		t.Fatal(err)
	}
	tb := root.(*components.VStack).ChildComponents()[0].(*components.TextBox)
	m := attachmentsOf(tb)[0].(*components.ValidationMarker)
	if m.Error != nameErr {
		t.Fatal("explicit Error binding did not land on the marker")
	}
	if tb.Error != nil {
		t.Fatal("the TextBox grew an Error handle nobody bound")
	}
}

func TestValidationMarkupLoadErrors(t *testing.T) {
	ctx, _, _ := validationCtx()
	cases := []struct {
		name, page, want string
	}{
		{
			"Error must be a string property",
			`<Gooey><TextBox Text="{{.Name}}" Error="{{.Name.Missing}}"/></Gooey>`,
			"cannot resolve",
		},
		{
			"TextBox rejects visual children",
			`<Gooey><TextBox Text="{{.Name}}"><Text>x</Text></TextBox></Gooey>`,
			"no visual children",
		},
		{
			"ValidationMarker takes no children",
			`<Gooey><TextBox Text="{{.Name}}"><ValidationMarker><Text>x</Text></ValidationMarker></TextBox></Gooey>`,
			"takes no children",
		},
	}
	for _, c := range cases {
		if _, err := Build([]byte(c.page), ctx); err == nil {
			t.Errorf("%s: load succeeded, want an error", c.name)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not mention %q", c.name, err, c.want)
		}
	}
}

// The <Validate> behavior in MAUI's explicit slot spelling: the rules
// declared in markup materialize the same validate.Field computed the
// Go API builds, published under Into for later bindings to reach.
const behaviorPage = `<Gooey>
  <VStack>
    <TextBox Prompt="name: " Text="{{.Name}}">
      <TextBox.Behaviors>
        <Validate Required="true" MinLen="3" Into=".NameErr"/>
      </TextBox.Behaviors>
    </TextBox>
    <Text>{{.NameErr}}</Text>
  </VStack>
</Gooey>`

func TestValidateBehaviorEndToEnd(t *testing.T) {
	name := prop.NewSource("")
	ctx := &Context{Values: map[string]any{"Name": name}}
	root, err := Build([]byte(behaviorPage), ctx)
	if err != nil {
		t.Fatal(err)
	}
	tb := root.(*components.VStack).ChildComponents()[0].(*components.TextBox)
	if tb.Error == nil {
		t.Fatal("<Validate> did not wire the host's Error handle")
	}
	pub, ok := ctx.Values["NameErr"].(*prop.Property[string])
	if !ok {
		t.Fatalf("Into did not publish: Values[NameErr] is %T", ctx.Values["NameErr"])
	}
	if pub != tb.Error {
		t.Fatal("the published property and the host's Error are different handles; one engine means one computed")
	}

	// The whole loop from pure markup: the inline Text shows the message,
	// the aggregate gates, edits move both.
	gate := validate.All(pub)
	c := gooey.NewComposer(root, 24, 3)
	c.Frame()
	if got := rowOf(c, 1); !strings.Contains(got, "required") {
		t.Fatalf("row 1 = %q, want the required message inline", got)
	}
	if gate.Get() {
		t.Fatal("empty required field, gate says valid")
	}
	name.Set("ab")
	c.Frame()
	if got := rowOf(c, 1); !strings.Contains(got, "at least 3 characters") {
		t.Fatalf("row 1 = %q, want the MinLen message", got)
	}
	name.Set("abc")
	c.Frame()
	if got := strings.TrimRight(rowOf(c, 1), " "); got != "" {
		t.Fatalf("row 1 = %q, want the error row empty once valid", got)
	}
	if !gate.Get() {
		t.Fatal("valid field, gate says invalid")
	}
}

// Bare child and <X.Behaviors> are two spellings of the same slot: the
// attachments land in the same list, the wiring is identical.
func TestBehaviorsSlotEqualsBareChild(t *testing.T) {
	bare := `<Gooey><TextBox Text="{{.Name}}"><Validate Required="true" Into=".E1"/></TextBox></Gooey>`
	slot := `<Gooey><TextBox Text="{{.Name}}"><TextBox.Behaviors><Validate Required="true" Into=".E2"/></TextBox.Behaviors></TextBox></Gooey>`
	ctx := &Context{Values: map[string]any{"Name": prop.NewSource("")}}
	b1, err := Build([]byte(bare), ctx)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := Build([]byte(slot), ctx)
	if err != nil {
		t.Fatal(err)
	}
	t1, t2 := b1.(*components.TextBox), b2.(*components.TextBox)
	if len(attachmentsOf(t1)) != 1 || len(attachmentsOf(t2)) != 1 {
		t.Fatalf("attachments: bare %d, slot %d — want 1 and 1", len(attachmentsOf(t1)), len(attachmentsOf(t2)))
	}
	e1 := ctx.Values["E1"].(*prop.Property[string])
	e2 := ctx.Values["E2"].(*prop.Property[string])
	if e1.Get() != "required" || e2.Get() != "required" {
		t.Fatalf("errors %q / %q — both spellings must wire identically", e1.Get(), e2.Get())
	}
}

// Into is optional when the Text binding is a single name: NameErr
// derives from Name. Other attachments still ride the same slot.
func TestValidateDerivedIntoAndMixedBehaviors(t *testing.T) {
	page := `<Gooey>
  <TextBox Text="{{.Name}}">
    <TextBox.Behaviors>
      <Validate Required="true"/>
      <KeyBinding Gesture="ctrl+k" Command="{{.Nop}}"/>
    </TextBox.Behaviors>
  </TextBox>
</Gooey>`
	ctx := &Context{Values: map[string]any{
		"Name": prop.NewSource(""),
		"Nop":  gooey.Command(func() {}),
	}}
	root, err := Build([]byte(page), ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ctx.Values["NameErr"].(*prop.Property[string]); !ok {
		t.Fatalf("derived Into did not publish NameErr; Values[NameErr] is %T", ctx.Values["NameErr"])
	}
	if n := len(attachmentsOf(root.(*components.TextBox))); n != 2 {
		t.Fatalf("attachments = %d, want the Validate and the KeyBinding", n)
	}
}

// ctx.Rules extends the <Validate> vocabulary: registration is the
// grant, like Components and Handlers — rule bodies stay code, the
// page keeps validation in markup. The constructor runs at load with
// the attribute literal and may reject it.
func TestValidateRegisteredRules(t *testing.T) {
	name := prop.NewSource("nope")
	ctx := &Context{
		Values: map[string]any{"Name": name},
		Rules: map[string]RuleFunc{
			"Email": func(arg string) (validate.Rule[string], error) {
				if arg != "true" {
					return nil, fmt.Errorf("want Email=\"true\"")
				}
				return validate.Pattern(`^[^@\s]+@[^@\s]+$`, "not an email"), nil
			},
		},
	}
	page := `<Gooey><TextBox Text="{{.Name}}"><Validate Email="true"/></TextBox></Gooey>`
	if _, err := Build([]byte(page), ctx); err != nil {
		t.Fatal(err)
	}
	errP := ctx.Values["NameErr"].(*prop.Property[string])
	if got := errP.Get(); got != "not an email" {
		t.Fatalf("registered rule: %q, want its message", got)
	}
	name.Set("a@b")
	if got := errP.Get(); got != "" {
		t.Fatalf("valid input: %q, want empty", got)
	}

	// A constructor rejecting its argument is a typed load error.
	bad := `<Gooey><TextBox Text="{{.Name}}"><Validate Email="yes"/></TextBox></Gooey>`
	if _, err := Build([]byte(bad), ctx); err == nil || !strings.Contains(err.Error(), `want Email="true"`) {
		t.Fatalf("rejecting constructor: err = %v, want its message in a load error", err)
	}

	// The unknown-rule error names the whole vocabulary: built-ins AND
	// registered rules.
	unk := `<Gooey><TextBox Text="{{.Name}}"><Validate Emial="true"/></TextBox></Gooey>`
	_, err := Build([]byte(unk), ctx)
	if err == nil {
		t.Fatal("unknown rule loaded")
	}
	for _, want := range []string{"Required", "MinLen", "MaxLen", "Pattern", "Into", "Email"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("unknown-rule error %q does not name %q", err, want)
		}
	}
}

// The DataAnnotations vocabulary from markup: every built-in attribute
// reaches the same rule the Go constructor builds.
func TestValidateAnnotationAttributes(t *testing.T) {
	cases := []struct {
		name, attrs, in, want string
	}{
		{"email", `EmailAddress="true"`, "nope", "not a valid email address"},
		{"email valid", `EmailAddress="true"`, "a@b.co", ""},
		{"url", `Url="true"`, "example.com", "not a valid URL"},
		{"url valid", `Url="true"`, "https://example.com", ""},
		{"phone", `Phone="true"`, "555-CALL", "not a valid phone number"},
		{"phone valid", `Phone="true"`, "+1 (555) 010-9999", ""},
		{"card", `CreditCard="true"`, "4111111111111112", "not a valid card number"},
		{"card valid", `CreditCard="true"`, "4111 1111 1111 1111", ""},
		{"digits", `Digits="true"`, "12a", "digits only"},
		{"integer", `Integer="true"`, "4.2", "must be a whole number"},
		{"min value", `MinValue="18"`, "17", "must be at least 18"},
		{"max value", `MaxValue="100"`, "101", "must be at most 100"},
		{"both values", `MinValue="1" MaxValue="10"`, "11", "must be between 1 and 10"},
		{"both values valid", `MinValue="1" MaxValue="10"`, "10", ""},
		{"false does not add the rule", `EmailAddress="false"`, "nope", ""},
		{"field-level Message overrides", `EmailAddress="true" Message="e-mail, please"`, "nope", "e-mail, please"},
		// Order is fixed regardless of attribute order: presence first.
		{"required beats shape", `EmailAddress="true" Required="true"`, "", "required"},
		{"shape once present", `EmailAddress="true" Required="true"`, "x", "not a valid email address"},
	}
	for _, c := range cases {
		src := `<Gooey><TextBox Text="{{.F}}"><Validate ` + c.attrs + ` Into=".E"/></TextBox></Gooey>`
		f := prop.NewSource(c.in)
		ctx := &Context{Values: map[string]any{"F": f}}
		if _, err := Build([]byte(src), ctx); err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		got := ctx.Values["E"].(*prop.Property[string]).Get()
		if got != c.want {
			t.Errorf("%s: %q → %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// Compare names the other field; the rule's read of it is what makes
// editing the original re-validate the confirmation.
func TestValidateCompareAttribute(t *testing.T) {
	for _, spelling := range []string{`Compare=".Password"`, `Compare="{{.Password}}"`} {
		src := `<Gooey><VStack>
  <TextBox Text="{{.Password}}"/>
  <TextBox Text="{{.Confirm}}"><Validate ` + spelling + ` Message="passwords differ" Into=".ConfirmErr"/></TextBox>
</VStack></Gooey>`
		password := prop.NewSource("secret")
		confirm := prop.NewSource("secret")
		ctx := &Context{Values: map[string]any{"Password": password, "Confirm": confirm}}
		if _, err := Build([]byte(src), ctx); err != nil {
			t.Fatalf("%s: %v", spelling, err)
		}
		errP := ctx.Values["ConfirmErr"].(*prop.Property[string])
		if got := errP.Get(); got != "" {
			t.Errorf("%s: matching fields report %q", spelling, got)
		}
		password.Set("changed")
		if got := errP.Get(); got != "passwords differ" {
			t.Errorf("%s: after editing the original, %q — the cross-field read did not subscribe", spelling, got)
		}
	}
}

func TestValidateAnnotationLoadErrors(t *testing.T) {
	ctx := &Context{Values: map[string]any{
		"F":   prop.NewSource(""),
		"N":   prop.NewSource(0),
		"Nop": gooey.Command(func() {}),
	}}
	cases := []struct {
		name, page, want string
	}{
		{
			"non-bool annotation rule",
			`<Gooey><TextBox Text="{{.F}}"><Validate EmailAddress="yes"/></TextBox></Gooey>`,
			"want a bool",
		},
		{
			"non-numeric MinValue",
			`<Gooey><TextBox Text="{{.F}}"><Validate MinValue="ten"/></TextBox></Gooey>`,
			"want a number",
		},
		{
			"empty numeric range",
			`<Gooey><TextBox Text="{{.F}}"><Validate MinValue="10" MaxValue="1"/></TextBox></Gooey>`,
			"range is empty",
		},
		{
			"Compare naming nothing",
			`<Gooey><TextBox Text="{{.F}}"><Validate Compare=""/></TextBox></Gooey>`,
			"name the other field",
		},
		{
			"Compare naming a missing field",
			`<Gooey><TextBox Text="{{.F}}"><Validate Compare=".Nope"/></TextBox></Gooey>`,
			"not found",
		},
		{
			"Compare naming a wrongly typed field",
			`<Gooey><TextBox Text="{{.F}}"><Validate Compare=".N"/></TextBox></Gooey>`,
			"*prop.Property[string]",
		},
		{
			"RegularExpression is not a spelling we accept",
			`<Gooey><TextBox Text="{{.F}}"><Validate RegularExpression="^a$"/></TextBox></Gooey>`,
			"unknown rule",
		},
	}
	for _, c := range cases {
		if _, err := Build([]byte(c.page), ctx); err == nil {
			t.Errorf("%s: load succeeded, want an error", c.name)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not mention %q", c.name, err, c.want)
		}
	}
}

func TestValidateLoadErrors(t *testing.T) {
	ctx := &Context{Values: map[string]any{
		"Name": prop.NewSource(""),
		"Err":  prop.NewSource(""),
	}}
	cases := []struct {
		name, page, want string
	}{
		{
			"unknown rule attribute",
			`<Gooey><TextBox Text="{{.Name}}"><Validate Requried="true"/></TextBox></Gooey>`,
			"unknown rule",
		},
		{
			"bad pattern",
			`<Gooey><TextBox Text="{{.Name}}"><Validate Pattern="["/></TextBox></Gooey>`,
			"Pattern",
		},
		{
			"bad Required literal",
			`<Gooey><TextBox Text="{{.Name}}"><Validate Required="yep"/></TextBox></Gooey>`,
			"want a bool",
		},
		{
			"host without a text source",
			`<Gooey><VStack><Validate Required="true"/></VStack></Gooey>`,
			"does not support <Validate>",
		},
		{
			"Error attribute and Validate together",
			`<Gooey><TextBox Text="{{.Name}}" Error="{{.Err}}"><Validate Required="true"/></TextBox></Gooey>`,
			"both",
		},
		{
			"two Validates",
			`<Gooey><TextBox Text="{{.Name}}"><Validate Required="true" Into=".A"/><Validate MinLen="2" Into=".B"/></TextBox></Gooey>`,
			"one <Validate>",
		},
		{
			"visual child in the Behaviors slot",
			`<Gooey><TextBox Text="{{.Name}}"><TextBox.Behaviors><Text>x</Text></TextBox.Behaviors></TextBox></Gooey>`,
			"Behaviors",
		},
		{
			"dotted Into",
			`<Gooey><TextBox Text="{{.Name}}"><Validate Required="true" Into=".Form.Err"/></TextBox></Gooey>`,
			"single context name",
		},
	}
	for _, c := range cases {
		if _, err := Build([]byte(c.page), ctx); err == nil {
			t.Errorf("%s: load succeeded, want an error", c.name)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not mention %q", c.name, err, c.want)
		}
	}
}

// A mistyped Error binding is a load-time failure naming both types —
// the lvalue contract.
func TestValidationErrorBindingTypeChecked(t *testing.T) {
	ctx := &Context{Values: map[string]any{
		"Name": prop.NewSource(""),
		"Bad":  prop.NewSource(42),
	}}
	page := `<Gooey><TextBox Text="{{.Name}}" Error="{{.Bad}}"/></Gooey>`
	_, err := Build([]byte(page), ctx)
	if err == nil {
		t.Fatal("an int-typed Error bound without error")
	}
	if !strings.Contains(err.Error(), "*prop.Property[string]") {
		t.Fatalf("error %q does not name the wanted type", err)
	}
}
