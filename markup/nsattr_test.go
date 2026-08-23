package markup

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/render"
)

// A namespaced attribute must be rejected, not flattened into its local
// name. Before this check, Attrs dropped the namespace, so zz:Style and
// Style were indistinguishable — a prefixed attribute that happened to
// collide with a known name was silently applied as if the author had
// written it bare (issue #351).

func TestNamespacedAttributeIsALoadError(t *testing.T) {
	ctx := &Context{Styles: map[string]render.Style{"accent": {Fg: render.RGB(255, 0, 0)}}}
	cases := []struct {
		name, src, want string
	}{
		{
			// The collision case: namespaced Style lands in the
			// vocabulary and used to be applied as bare Style.
			name: "xml:Style collision",
			src:  `<Gooey><Text Name="t" xml:Style="accent">hi</Text></Gooey>`,
			want: "namespaced attributes are not supported",
		},
		{
			name: "undeclared prefix collision",
			src:  `<Gooey><Text Name="t" zz:Style="accent">hi</Text></Gooey>`,
			want: "namespaced attributes are not supported",
		},
		{
			// xml:lang happened to fail before (unknown attribute) but
			// only by accident; it now fails for the right reason and
			// the error names what the author wrote.
			name: "xml:lang",
			src:  `<Gooey><Text Name="t" xml:lang="en">hi</Text></Gooey>`,
			want: `Text xml:lang="en"`,
		},
		{
			name: "declared namespace prefix",
			src:  `<Gooey xmlns:ns="wonderforge.io/gooey/x"><Text Name="t" ns:Style="accent">hi</Text></Gooey>`,
			want: "namespaced attributes are not supported",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Build([]byte(c.src), ctx)
			if err == nil {
				t.Fatalf("loaded cleanly; the namespaced attribute would have been honoured under a name the author did not write")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not contain %q", err, c.want)
			}
		})
	}
}

// TestNamespaceDeclarationsStillLoad guards the other direction: xmlns
// declarations are the attributes this check must keep ignoring.
func TestNamespaceDeclarationsStillLoad(t *testing.T) {
	src := `<Gooey xmlns="wonderforge.io/gooey/2026" xmlns:x="wonderforge.io/gooey/x"><Text Name="t">hi</Text></Gooey>`
	if _, err := Build([]byte(src), &Context{}); err != nil {
		t.Fatalf("namespace declarations must still load, got: %v", err)
	}
}
