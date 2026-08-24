package main

import "testing"

// editAttr drives an attribute edit the way the KEYBOARD does: select the
// row, open the editor, type, commit.
//
// It replaces the idiom these tests were written against —
//
//	ed.editName.Set("Canvas.Left")
//	ed.editValue.Set("7")
//	ed.commitEdit()
//
// — which stopped working when the property browser turned the editing
// surface into a <ValueEditor> overlay. That overlay resolves its own
// subject when it OPENS (valueEditor.Write uses p.name, set by
// beginEdit), so a bare editName.Set now writes a name nothing reads,
// Write returns early, and no rebuild happens.
//
// THE FAILURE MODE IS WHY THIS IS A HELPER AND NOT A SED. Nothing about
// that combination fails to compile, and the edit silently does nothing —
// so each test goes on asserting, and reports the absence as whatever it
// happened to be about. One of them said "an edit in REMOTE mode recorded
// no history: rebuild returns early for the remote push", which blames
// rebuild for a mutation that never reached it. Routing every site
// through one function means the next change to the editing surface
// breaks in one place, loudly.
func editAttr(t *testing.T, ed *editor, name, value string) {
	t.Helper()
	i, _ := rowIndex(t, ed, name)
	ed.attrSel.Set(i)
	ed.beginEdit()
	ed.editValue.Set(value)
	ed.commitEdit()
}
