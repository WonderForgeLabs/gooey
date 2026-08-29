package main

// The menus, and the two things about them that were specified rather
// than chosen.
//
// # One state rendered twice, never two
//
// The requirement:
//
//	View → Code → [x] Built in       ctrl+b
//	              [ ] $EDITOR (nvim) alt+e
//
// "the accelerators must reflect and toggle the same state the check
// shows". The tempting implementation keeps a bool per item and has each
// command set its own and clear the other's — and it is wrong twice over.
// Two bools can disagree (both checked, neither checked), and neither one
// is what the KEY writes unless every writer remembers both.
//
// So there is ONE source property, `ed.codeView`, holding which viewer is
// selected, and each item's Checked is a COMPUTED over it:
//
//	builtinChecked = codeView.Get() == codeBuiltin
//	editorChecked  = codeView.Get() == codeExternal
//
// A computed is the read-only projection — prop.Property.Settable() is
// false for one — so nothing can write a check directly even by mistake;
// the only way to move a box is to move the state it is a view of. The
// menu item's Command and the KeyBinding are then literally the same
// gooey.Action value, handed to both, so there is no second code path for
// the key to drift down.
//
// The check is read inside MenuBar.drawDropdown, which runs inside the
// dropdown's own paint node, so this is also the damage story: toggling
// the viewer while the menu is open repaints the dropdown alone, and
// toggling it while the menu is closed repaints nothing.
//
// # $EDITOR is RESOLVED, not printed
//
// An item reading "$EDITOR" tells you nothing about what will open. The
// label carries the resolved program — "$EDITOR (nvim)" — and reads
// "$EDITOR (unset)" when the variable is empty, which is the honest
// rendering of a thing that cannot run. The item is also DISABLED in that
// case (gooey.NewCommand(...).When(...)), so the menu dims it rather than
// offering a key that fails.
//
// Resolution happens ONCE, at menu build. $EDITOR changing under a
// running process is not a thing that happens, and re-resolving per frame
// would put an os.Getenv in the paint path.
//
// # There are no submenus
//
// components.Menu is one level: Menu → []MenuItem, and buildMenuBar
// rejects anything but <MenuItem> inside a <Menu>. The specified
// "View → Code → ..." is therefore flattened into a labelled group inside
// View, separated by rules. Saying so is better than pretending a nesting
// that would have to be invented in the framework first.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Which viewer the Code region uses. One state, two renderings.
const (
	codeBuiltin = iota
	codeExternal
)

// Which thing the editor region is showing. The region swap: Code is not
// a panel beside the designer, it is the SAME region showing something
// else, so this selects between two children of one dock pane rather than
// adding a pane.
const (
	regionDesign = iota
	regionCode
)

// resolveEditor answers what $EDITOR will actually run, and it is the
// whole point of the label.
//
// It returns the BASENAME for the label — "nvim" out of
// "/usr/local/bin/nvim -u NONE" — and the argv for running it. $EDITOR
// commonly carries flags, so it is split on whitespace rather than
// treated as a path; a program whose own path contains a space is
// misresolved by that, and by every shell that reads $EDITOR the same
// way.
//
// The empty string for unset is deliberate and is what makes the label
// read as unset rather than as a program called "".
func resolveEditor() (label string, argv []string) {
	raw := strings.TrimSpace(os.Getenv("EDITOR"))
	if raw == "" {
		return "", nil
	}
	argv = strings.Fields(raw)
	label = filepath.Base(argv[0])
	// Reported as unresolvable rather than as present: an $EDITOR naming
	// a program that is not installed is exactly the case the resolved
	// label exists to expose, and claiming it is there would be worse
	// than printing "$EDITOR".
	if _, err := exec.LookPath(argv[0]); err != nil {
		return label + ", not found", argv
	}
	return label, argv
}

// editorItemText is the menu label. "$EDITOR (nvim)" when it resolves,
// "$EDITOR (unset)" when it does not.
func editorItemText(label string) string {
	if label == "" {
		return "$EDITO_R (unset)"
	}
	return "$EDITO_R (" + label + ")"
}

// setCodeView picks the viewer AND swaps the region to it. Picking a
// viewer from a menu while looking at the designer should show you the
// thing you picked; leaving the region where it was would make the menu
// item look inert.
func (ed *editor) setCodeView(which int) {
	ed.codeView.Set(which)
	ed.region.Set(regionCode)
	if which == codeExternal {
		ed.launchEditor()
	}
}

// launchEditor hands the terminal to $EDITOR and takes it back.
//
// App.Suspend is what makes this correct rather than a screen-corrupting
// hack: it releases the terminal, joins the input decoder (a reader left
// parked on the tty would eat the child's keystrokes), shields the
// interrupt the tty driver sends to the whole foreground process group,
// and repaints from the retained buffer on the way back. Spawning a child
// on the shared tty without it is the "clean cells never repaint the
// garbage" failure — the child's output lands in cells the composer
// believes are already correct, so nothing ever repaints over it.
//
// SAVE FIRST. The child edits the file on disk; the editor's document is
// in memory. Without the save the child opens a stale file and the user's
// designer edits are silently discarded by whatever the child writes.
func (ed *editor) launchEditor() {
	_, argv := resolveEditor()
	rel := ed.openPath.Get()
	if len(argv) == 0 || rel == "" || ed.ws == nil || ed.ws.dir == "" || ed.app == nil {
		return
	}
	if err := ed.saveOpenFile(); err != nil {
		return
	}
	full := filepath.Join(ed.ws.dir, filepath.FromSlash(rel))
	err := ed.app.Suspend(func() error {
		cmd := exec.Command(argv[0], append(argv[1:], full)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	})
	if err != nil {
		ed.status.Set("✗ " + argv[0] + ": " + err.Error())
		return
	}
	// The child may have rewritten the file; the in-memory document is
	// now the stale copy. Re-reading is the only way the two agree.
	ed.openWorkspaceFile(rel)
}

// swapRegion is the Design ⇄ Code toggle. One property, and both the
// menu items and ctrl+\ write it.
func (ed *editor) swapRegion() {
	if ed.region.Get() == regionCode {
		ed.region.Set(regionDesign)
		return
	}
	ed.region.Set(regionCode)
}

// menuValues is every binding the <MenuBar> and the dock KeyBindings
// need, merged into the editor's context.
//
// EVERY ACTION IS CREATED ONCE and shared between its menu item and its
// KeyBinding. That is what makes "the accelerator toggles the same state
// the check shows" true by construction rather than by review: there is
// one gooey.Action value, and both the <MenuItem Command="..."> and the
// <KeyBinding Command="..."> resolve the same name to it.
func (ed *editor) menuValues() map[string]any {
	label, _ := resolveEditor()

	// The check states: computeds over the ONE source. Read-only by
	// construction — a computed is not Settable — so a check cannot be
	// written behind the state's back.
	ed.builtinChecked = prop.NewComputed(func() bool { return ed.codeView.Get() == codeBuiltin })
	ed.editorChecked = prop.NewComputed(func() bool { return ed.codeView.Get() == codeExternal })
	ed.designChecked = prop.NewComputed(func() bool { return ed.region.Get() == regionDesign })
	ed.codeChecked = prop.NewComputed(func() bool { return ed.region.Get() == regionCode })

	// $EDITOR is available only when it resolves AND there is a file for
	// it to open. Both reads are HOISTED above nothing — there is no
	// branch here — but the openPath read has to happen on every
	// evaluation for the item to un-dim when a file is opened, which is
	// why the condition is a computed rather than a captured bool.
	hasEditor := prop.NewComputed(func() bool {
		return label != "" && !strings.HasSuffix(label, "not found") &&
			ed.openPath.Get() != "" && ed.ws != nil && ed.ws.dir != ""
	})
	canSave := prop.NewComputed(func() bool { return ed.canSave() })
	hasWorkspace := prop.NewComputed(func() bool { return ed.wsLabel.Get() != "" })

	v := map[string]any{
		// Region and code view.
		"Region":         ed.region,
		"CodeView":       ed.codeView,
		"BuiltinChecked": ed.builtinChecked,
		"EditorChecked":  ed.editorChecked,
		"DesignChecked":  ed.designChecked,
		"CodeChecked":    ed.codeChecked,
		"EditorItemText": editorItemText(label),
		"ShowDesign":     gooey.Command(func() { ed.region.Set(regionDesign) }),
		"ShowCode":       gooey.Command(func() { ed.region.Set(regionCode) }),
		"SwapRegion":     gooey.Command(func() { ed.swapRegion() }),
		"UseBuiltin":     gooey.Command(func() { ed.setCodeView(codeBuiltin) }),
		"UseEditor":      gooey.NewCommand(func() { ed.setCodeView(codeExternal) }).When(hasEditor),

		// Workspace.
		"WsLabel":     ed.wsLabel,
		"WsQuery":     ed.wsQuery,
		"WsSel":       ed.wsSel,
		"WsFiles":     ed.wsFiles,
		"OpenPath":    ed.openPath,
		"OpenFolder":  gooey.Command(func() { ed.promptFolder() }),
		"OpenFile":    gooey.Command(func() { ed.openSelectedFile() }),
		"CommitPath":  gooey.Command(func() { ed.setWorkspace(ed.wsPath.Get()) }),
		"WsPath":      ed.wsPath,
		"Refresh":     gooey.NewCommand(func() { ed.refreshWorkspace() }).When(hasWorkspace),
		"Save":        gooey.NewCommand(func() { _ = ed.saveOpenFile() }).When(canSave),
		"CloseFolder": gooey.NewCommand(func() { ed.closeWorkspace() }).When(hasWorkspace),

		// Dock. Every one of these is reachable from the View menu AND
		// from a KeyBinding, because a pointer-only dock cannot be
		// verified at all: mouse reports do not survive a recording pty.
		"DockLeft":     gooey.Command(func() { ed.dock.MoveActive(dockLeft) }),
		"DockRight":    gooey.Command(func() { ed.dock.MoveActive(dockRight) }),
		"DockCenter":   gooey.Command(func() { ed.dock.MoveActive(dockCenter) }),
		"DockBottom":   gooey.Command(func() { ed.dock.MoveActive(dockBottom) }),
		"NextPane":     gooey.Command(func() { ed.dock.Cycle(1) }),
		"PrevPane":     gooey.Command(func() { ed.dock.Cycle(-1) }),
		"PaneUp":       gooey.Command(func() { ed.dock.Reorder(-1) }),
		"PaneDown":     gooey.Command(func() { ed.dock.Reorder(1) }),
		"Grow":         gooey.Command(func() { ed.dock.Resize(1) }),
		"Shrink":       gooey.Command(func() { ed.dock.Resize(-1) }),
		"TogglePane":   gooey.Command(func() { ed.dock.ToggleHidden(ed.dock.Active()) }),
		"CollapsePane": gooey.Command(func() { ed.dock.ToggleCollapsed(ed.dock.Active()) }),
		"PinPane":      gooey.Command(func() { ed.dock.TogglePinned(ed.dock.Active()) }),
		"HideUnpinned": gooey.Command(func() { ed.dock.HideUnpinned() }),
		"ShowAllPanes": gooey.Command(func() { ed.dock.ShowAll() }),
	}
	return v
}

// promptFolder puts the keyboard in the folder box. ctrl+o has to land
// somewhere a user can type, and this editor has no modal dialog — the
// box is a permanent row in the Explorer pane, so "open folder" is
// "focus the box" rather than "summon a window".
func (ed *editor) promptFolder() {
	if ed.app == nil || ed.pathBox == nil {
		return
	}
	if c := ed.app.Composer(); c != nil {
		c.Focus().SetFocus(ed.pathBox)
	}
}

// openSelectedFile is Enter on the file list.
func (ed *editor) openSelectedFile() {
	if ed.ws == nil {
		return
	}
	files := ed.ws.ranked(ed.wsQuery.Get())
	i := ed.wsSel.Get()
	if i < 0 || i >= len(files) {
		return
	}
	ed.openWorkspaceFile(files[i])
}

func (ed *editor) closeWorkspace() {
	ed.ws = nil
	ed.wsLabel.Set("")
	ed.openPath.Set("")
	ed.wsQuery.Set("")
	ed.wsRev.Set(ed.wsRev.Get() + 1)
	ed.status.Set("no folder open")
}
