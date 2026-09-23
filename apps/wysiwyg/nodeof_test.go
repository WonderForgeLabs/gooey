package main

import (
	"strings"
	"testing"
)

// nodeOf's doc calls it deliberately strict: a malformed seed is a bug in
// the catalog, and it should surface as an error rather than as content
// silently dropped from the palette. None of the 28 real seeds is
// malformed, so every one of those branches is unexercised by
// TestEveryPaletteEntryLoadsAndOccupiesSpace. These pin that the
// strictness is real — a rejection that never fires reads as coverage it
// is not.
func TestNodeOfRejectsMalformedSeeds(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "namespaced attribute",
			src:  `<Text xmlns:x="urn:x" x:Width="10">hi</Text>`,
			want: "is namespaced",
		},
		{
			// THE PREFIX, in the same case one row up. The refusal
			// formatted a.Name.Local alone, so an author opening their
			// own file read `attribute "Width" is namespaced` and had
			// nothing to search for — while the comment beside it
			// claimed to give the same answer markup's own
			// namespacedAttrError gives, which formats {uri}local for
			// exactly this reason. The row above passes either way,
			// which is why this one exists. Raised in review of #501.
			name: "namespaced attribute names the uri, not just the local part",
			src:  `<Text xmlns:x="urn:x" x:Width="10">hi</Text>`,
			want: "urn:x",
		},
		{
			name: "property element whose owner is not its parent",
			src:  `<VStack><ItemsView.ItemTemplate><Text>x</Text></ItemsView.ItemTemplate></VStack>`,
			want: "inside",
		},
		{
			// The loader refuses this too ("markup: <X.Y> appears twice"),
			// and letting the map keep the second would save a document
			// without the first.
			name: "one property element written twice",
			src:  `<ItemsView><ItemsView.ItemTemplate><Text>a</Text></ItemsView.ItemTemplate><ItemsView.ItemTemplate><Text>b</Text></ItemsView.ItemTemplate></ItemsView>`,
			want: "appears twice",
		},
		{
			name: "no root element",
			src:  `   `,
			want: "no root element",
		},
		{
			// NOTE: this does NOT reach nodeOf's own "unbalanced </%s>"
			// branch. encoding/xml rejects the stray end element first
			// ("XML syntax error ... unexpected end element"), so that
			// branch is unreachable from this entry point and stays as
			// defence in depth. Asserting the decoder's message here
			// rather than nodeOf's is the honest pin — claiming the
			// branch is covered when it cannot fire is the fail-open
			// shape this whole PR is about.
			name: "unbalanced end tag (caught by the decoder, not by nodeOf)",
			src:  `<Text>hi</Text></Text>`,
			want: "markup does not parse",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := nodeOf(tc.src)
			if err == nil {
				t.Fatalf("nodeOf(%q) = %+v, want an error", tc.src, n)
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("nodeOf(%q) error = %q, want it to mention %q", tc.src, err, tc.want)
			}
			t.Logf("rejected as: %v", err)
		})
	}
}

// The success path stays pinned here too, so a change that makes nodeOf
// reject everything cannot pass the table above.
func TestNodeOfAcceptsAWellFormedSeed(t *testing.T) {
	n, err := nodeOf(`<VStack Width="10"><Text>  keep me</Text></VStack>`)
	if err != nil {
		t.Fatalf("nodeOf: %v", err)
	}
	if n.Elem != "VStack" || n.Attrs["Width"] != "10" {
		t.Fatalf("root = %+v, want VStack Width=10", n)
	}
	if len(n.Kids) != 1 || n.Kids[0].Elem != "Text" {
		t.Fatalf("kids = %+v, want one <Text>", n.Kids)
	}
	// The body rule is markup.BodyText, not TrimSpace: a one-line body is
	// verbatim, so the leading spaces survive.
	if got := n.Kids[0].Body; got != "  keep me" {
		t.Fatalf("body = %q, want %q", got, "  keep me")
	}
}
