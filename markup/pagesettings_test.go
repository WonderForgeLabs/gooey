package markup

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestPageSettingsFromGooeyRoot(t *testing.T) {
	fsys := fstest.MapFS{
		"ok.gooey":   {Data: []byte(`<Gooey Graphics="sixel"><Text>hi</Text></Gooey>`)},
		"none.gooey": {Data: []byte(`<Gooey><Text>hi</Text></Gooey>`)},
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
