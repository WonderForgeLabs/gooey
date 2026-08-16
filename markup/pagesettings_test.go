package markup

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestPageSettingsFromGooeyRoot(t *testing.T) {
	fsys := fstest.MapFS{
		"ok.gooey":      {Data: []byte(`<Gooey Graphics="sixel"><Text>hi</Text></Gooey>`)},
		"none.gooey":    {Data: []byte(`<Gooey><Text>hi</Text></Gooey>`)},
		"badmode.gooey": {Data: []byte(`<Gooey Graphics="pigeons"><Text>hi</Text></Gooey>`)},
		"badattr.gooey": {Data: []byte(`<Gooey Graphcis="sixel"><Text>hi</Text></Gooey>`)},
	}
	s, err := ReadPageSettings(fsys, "ok.gooey")
	if err != nil || s.Graphics != "sixel" {
		t.Fatalf("ok: %+v %v", s, err)
	}
	if s, err := ReadPageSettings(fsys, "none.gooey"); err != nil || s.Graphics != "" {
		t.Errorf("absent should mean let capabilities decide: %+v %v", s, err)
	}
	if _, err := ReadPageSettings(fsys, "badmode.gooey"); err == nil || !strings.Contains(err.Error(), "unknown mode") {
		t.Errorf("bad mode = %v", err)
	}
	// A typo'd attribute must fail rather than silently do nothing.
	if _, err := ReadPageSettings(fsys, "badattr.gooey"); err == nil || !strings.Contains(err.Error(), "no attribute") {
		t.Errorf("typo'd attr = %v", err)
	}
}

// Every name in gooeyAttrs must actually be READ by readGooeyAttrs.
//
// The two ways a declared vocabulary can be wrong are not symmetric.
// UNDER-declaration is loud: an attribute the code reads but the table
// omits is refused at load, and the first document using it fails. OVER-
// declaration is silent: a name the table permits but nothing reads is
// accepted and ignored — which is precisely the silent-drop defect the
// table exists to prevent, reintroduced through the table.
//
// Nothing else would catch it, so this does: setting a declared
// attribute to a deliberately absurd value must produce an error. An
// attribute genuinely accepting any string would need a different proof
// and should say so here rather than being quietly exempted.
func TestEveryDeclaredGooeyAttrIsRead(t *testing.T) {
	for name := range gooeyAttrs {
		src := `<Gooey ` + name + `="::definitely not a valid value::"><Text>x</Text></Gooey>`
		fsys := fstest.MapFS{"p.gooey": {Data: []byte(src)}}
		if _, err := ReadPageSettings(fsys, "p.gooey"); err == nil {
			t.Errorf("<Gooey %s=...> accepted an absurd value: the attribute is "+
				"declared in gooeyAttrs but nothing reads or validates it, so a "+
				"document setting it is silently ignored", name)
		}
	}
}
