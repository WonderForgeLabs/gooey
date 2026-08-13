package settings_test

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/settings"
)

// A setting has to be indistinguishable from a viewmodel field at the
// markup boundary, because that is the whole claim: no settings-specific
// binding mechanism, no adapter, no snapshot. The page below is the
// browser's three settings, spelled the way a page spells anything else.
const page = `<Gooey xmlns="wonderforge.io/gooey/2026">
  <VStack>
    <Text>{{.LastSource}}</Text>
    <Checkbox Checked="{{.KeepRecording}}" Label="keep recording"/>
    <Checkbox Checked="{{.AutoRestart}}" Label="auto restart app"/>
  </VStack>
</Gooey>`

func settingsContext(t *testing.T, doc string) (*settings.Store, *markup.Context) {
	t.Helper()
	_, s := open(t, doc)
	return s, &markup.Context{Values: map[string]any{
		"LastSource":    mustValue(t, s, keySource, ""),
		"KeepRecording": mustValue(t, s, keyRecord, false),
		"AutoRestart":   mustValue(t, s, keyRestart, false),
	}}
}

func TestMarkupBindsSettingsLikeAnyOtherHandle(t *testing.T) {
	s, ctx := settingsContext(t, `{"browser.lastSource":"origin/main","browser.autoRestartApp":true}`)
	root, err := markup.Build([]byte(page), ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c := gooey.NewComposer(root, 40, 6)
	f, _ := c.Frame()
	if got := row(f.Cells, 0); got != "origin/main" {
		t.Fatalf("row 0 = %q, want the stored setting rendered", got)
	}
	if got := row(f.Cells, 2); !strings.Contains(got, "auto restart app") || !strings.Contains(got, "x") {
		t.Fatalf("row 2 = %q, want a checked box for the stored true", got)
	}

	// The damage pin, through markup: changing one setting repaints one
	// component, exactly as changing one viewmodel handle would.
	src, _ := s.Raw(keySource)
	if string(src) != `"origin/main"` {
		t.Fatalf("Raw = %s", src)
	}
	ctx.Values["LastSource"].(interface{ Set(string) }).Set("feat/settings")
	f, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("changing a bound setting painted %d components, want exactly 1", painted)
	}
	if got := row(f.Cells, 0); got != "feat/settings" {
		t.Fatalf("row 0 = %q after the Set", got)
	}
}

// The discrimination half: a setting is type-checked at load like any
// other handle. Binding the bool setting where a string is wanted is a
// load error naming the type, not a surprise at paint time.
func TestMarkupTypeChecksASettingHandle(t *testing.T) {
	_, ctx := settingsContext(t, "")
	_, err := markup.Build([]byte(`<Gooey xmlns="wonderforge.io/gooey/2026">`+
		`<Checkbox Checked="{{.LastSource}}"/></Gooey>`), ctx)
	wantErr(t, err, "prop.Property[bool]")

	_, err = markup.Build([]byte(`<Gooey xmlns="wonderforge.io/gooey/2026">`+
		`<Text>{{.NoSuchSetting}}</Text></Gooey>`), ctx)
	wantErr(t, err, "NoSuchSetting")
}
