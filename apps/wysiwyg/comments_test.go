package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// TestCommentsSurviveTheRoundTrip is #529: nodeOf fell through its
// switch on xml.Comment, so the first save deleted every comment in the
// author's file under "✓ saved".
//
// Every position a comment can take is in the fixture, because each is
// kept by a different half of the model: above the envelope and after
// the content root (the envelope is not a node — see
// openWorkspaceFile), leading a child, trailing a container's last
// child, inside a property element, and inline after a body.
func TestCommentsSurviveTheRoundTrip(t *testing.T) {
	root := workspaceFixture(t)
	const doc = `<!-- header -->
<Gooey>
  <Gooey.Resources>
    <!-- in a slot -->
    <Style Key="panel" Bold="true"/>
  </Gooey.Resources>
  <!-- keep me -->
  <Canvas Name="Root">
    <!-- leads B -->
    <Button Name="B" Content="go"/>
    <Text Name="T">hi<!-- inline --></Text>
    <Text Name="P"><!-- before -->pre</Text>
    <!-- trails the canvas -->
  </Canvas>
  <!-- after the root -->
</Gooey>
<!-- epilog -->
`
	path := filepath.Join(root, "c.gooey")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("c.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("opening a commented document reports %q, want a build", got)
	}
	if err := ed.saveOpenFile(); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"header", "in a slot", "keep me", "leads B",
		"inline", "before", "trails the canvas", "after the root", "epilog"} {
		if !strings.Contains(string(first), "<!-- "+c+" -->") {
			t.Errorf("the save deleted <!-- %s -->:\n%s", c, first)
		}
	}
	// THE PLACEMENT THAT MATTERS: a leading comment stays above the
	// element it describes, and an inline one does not become body text.
	if !strings.Contains(string(first), "<!-- leads B -->\n    <Button") {
		t.Errorf("<!-- leads B --> no longer sits above <Button>:\n%s", first)
	}
	if !strings.Contains(string(first), "hi<!-- inline --></Text>") {
		t.Errorf("the inline comment moved out of <Text>'s body line:\n%s", first)
	}
	// THE RELOCATIONS, pinned so they are decisions rather than
	// accidents (node.Tail and openWorkspaceFile state them). A comment
	// before a body is saved after it: Body is one string with no
	// position in it. And an epilog after </Gooey> ends as the content
	// root's last line.
	if !strings.Contains(string(first), "pre<!-- before --></Text>") {
		t.Errorf("a comment before a body did not land after it, where node.Tail says:\n%s", first)
	}
	if !strings.Contains(string(first), "<!-- epilog -->\n  </Canvas>") {
		t.Errorf("the epilog did not land as the content root's last line:\n%s", first)
	}

	// Stable from the first save on: the envelope's comments move one
	// line inward once, and nothing moves again.
	ed.openWorkspaceFile("c.gooey")
	if err := ed.saveOpenFile(); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("a second round trip changed the file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}
