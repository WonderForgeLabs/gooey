package markup

import (
	"strings"
	"testing"
)

// Declarations is the read-only schema seam: the declaration block read
// as a wire schema (the control plane's GetDeclaredSchema), with nothing
// built and no context touched.

func TestDeclarationsReadsTheBlockWithoutBuilding(t *testing.T) {
	src := `<Gooey xmlns:x="wonderforge.io/gooey/x">
  <x:Property Name="Title" Type="string" Required="true"/>
  <x:Property Name="Limit" Type="int" Default="5"/>
  <Text>{{.Title}}</Text>
</Gooey>`
	// {{.Title}} would fail a BUILD against an empty context; a schema
	// read must not care.
	decls, err := Declarations([]byte(src))
	if err != nil {
		t.Fatalf("Declarations: %v", err)
	}
	if len(decls) != 2 {
		t.Fatalf("decls = %+v", decls)
	}
	if d := decls[0]; d.Name != "Title" || d.Type != "string" || !d.Required || d.Default != "" {
		t.Errorf("Title = %+v", d)
	}
	if d := decls[1]; d.Name != "Limit" || d.Type != "int" || d.Default != "5" || d.Required {
		t.Errorf("Limit = %+v", d)
	}
}

func TestDeclarationsOnAPlainDocument(t *testing.T) {
	decls, err := Declarations([]byte(`<Gooey><Text>hi</Text></Gooey>`))
	if err != nil {
		t.Fatalf("Declarations: %v", err)
	}
	if len(decls) != 0 {
		t.Errorf("a plain page declares nothing, got %+v", decls)
	}
}

func TestDeclarationsReportsTheLoadError(t *testing.T) {
	_, err := Declarations([]byte(`<Gooey xmlns:x="wonderforge.io/gooey/x"><x:Property Type="string"/><Text>x</Text></Gooey>`))
	if err == nil || !strings.Contains(err.Error(), "needs a Name") {
		t.Errorf("err = %v, want the declaration's own load error", err)
	}
}
