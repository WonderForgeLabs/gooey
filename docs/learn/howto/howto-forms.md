# How to validate a form

Runnable example: [`docs/learn/examples/howto-forms`](../examples/howto-forms/) — three fields, inline errors, a floating marker, and a submit button that enables itself.

There is no validation framework to learn: **a validator is a computed
property**. It reads the field's source and yields an error string —
empty means valid. The input reads it for its invalid visual, an
ordinary `<Text>` shows it, and form-level validity is a property the
submit command's `CanExecute` reads. XAML needs three mechanisms for
this (ValidationRules, `INotifyDataErrorInfo`, ErrorTemplates); here
they collapse into the property graph.

## Declare rules in markup

The primary form keeps the whole story in the page: a `<Validate>`
behavior on the input, an inline error `<Text>` under it.

```xml
<TextBox Prompt="name: " Text="{{.Name}}">
  <TextBox.Behaviors>
    <Validate Required="true" MinLen="3"/>
  </TextBox.Behaviors>
</TextBox>
<Text Style="err">{{.NameErr}}</Text>
```

The behavior builds the validator against the bound `Text` source and
**publishes** it in the context — here as `NameErr`, derived from
`Text="{{.Name}}"`; say `Into=".SomethingElse"` to name it yourself.
The `TextBox` gets its `Error` handle wired automatically, so invalid
input paints red and underlined (replace that with a named
`InvalidStyle="…"`). Rules run in a fixed order — `Required`, then
`MinLen`/`MaxLen`, then `Pattern` — and the first failure is the
message, so a person fixing the field sees one problem at a time.

The error row is just a bound `Text`: while the field is valid the
message is `""` and the row is blank. To reclaim the row entirely, bind
its `Visibility` to a has-error bool (`validate.Has(err)` in the
viewmodel): bool bindings map true→`Visible`, false→`Collapsed`.

## Or build validators in code

The behavior calls the same constructors your viewmodel can:

```go
nameErr := validate.Field(name,
    validate.Required(""),            // "" = the stock message
    validate.Len(3, 0, ""),           // at least 3 runes, no upper bound
)
```

Any `func(T) string` is a rule, so the escape hatch is a closure — and
a rule that reads *another* property subscribes to it, which is all
cross-field validation is:

```go
confirmErr := validate.Field(confirm, func(s string) string {
    if s != password.Get() {
        return "passwords differ"
    }
    return ""
})
```

Bind the result yourself: `<TextBox Text="{{.Name}}" Error="{{.NameErr}}"/>`.

## Custom rules for markup

Registration is the grant, like `Components` and `Handlers` — the rule
body stays in code, the page keeps the vocabulary:

```go
ctx.Rules = map[string]markup.RuleFunc{
    "Email": func(arg string) (validate.Rule[string], error) {
        return validate.Pattern(`^[^@\s]+@[^@\s]+$`, "not an email"), nil
    },
}
```

```xml
<Validate Required="true" Email="true"/>
```

The constructor runs at load with the attribute's literal and may
reject it — a bad argument fails the load, not the keystroke.

## Gate the submit button

Form-level validity is an aggregate over the field errors:

```go
canSubmit := validate.All(nameErr, emailErr)
submit    := gooey.NewCommand(save).When(canSubmit)
```

The button asks the command while painting, so the flip repaints
exactly the button — and `All` is value-stabilized: a keystroke that
does not change validity never touches it.

When the validators live in markup, the published properties do not
exist until the page loads, so look them up inside the computed, at
evaluation time (the runnable example does this):

```go
canSubmit := prop.NewComputed(func() bool {
    for _, k := range []string{"NameErr", "EmailErr"} {
        p, ok := ctx.Values[k].(*prop.Property[string])
        if !ok || p.Get() != "" {
            return false
        }
    }
    return true
})
```

## Float the message when there is no room

Dense layouts — grids, toolbars — may have no row to give an error. A
`<ValidationMarker/>` attachment shows the message in the page's
`AdornmentLayer` instead, anchored under the field, flipping above at
the screen edge:

```xml
<TextBox Text="{{.Tag}}" Error="{{.TagErr}}">
  <ValidationMarker/>
</TextBox>
…
<AdornmentLayer/>   <!-- last child of the root -->
```

The marker adopts its host's `Error` handle (bind `Error="…"` on the
marker to override), never intercepts the pointer, and a page without a
layer simply degrades to inline-only display.

## What repaints

Every reader subscribed by reading, so the damage is exactly the
surfaces showing the field's state: editing a field repaints the
`TextBox` and its error display; the validity flip additionally
repaints the submit button, once. The contract is pinned by
`TestValidationLoopDamage` in `components/validation_test.go`.
