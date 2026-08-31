// Package input is the terminal-independent key vocabulary: decoded key
// events, the byte decoder that produces them, and the gesture syntax
// markup uses to name them.
//
// It exists so the import graph stays a line rather than a cycle: term
// reads bytes and produces input.KeyEvent; gooey routes input.KeyEvent
// through the component tree. Neither has to know about the other.
package input

import "strings"

// Key is a named key. KeyRune means "a printable character" and the
// event's Rune field carries it.
type Key uint8

const (
	KeyRune Key = iota
	KeyEnter
	KeyTab
	KeyEsc
	KeyBackspace
	KeyDelete
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
)

// Mods are the modifier keys the terminal reports. Terminals cannot
// report shift for printable characters — they send the shifted rune
// instead — so ModShift only ever appears on named keys.
type Mods uint8

const (
	ModShift Mods = 1 << iota
	ModAlt
	ModCtrl
)

// KeyEvent is one decoded keystroke. It is comparable, so gestures match
// events with ==.
type KeyEvent struct {
	Key  Key
	Rune rune
	Mods Mods
}

// Rune builds a plain printable-character event.
func Rune(r rune) KeyEvent { return KeyEvent{Key: KeyRune, Rune: r} }

// Named builds a plain named-key event.
func Named(k Key) KeyEvent { return KeyEvent{Key: k} }

func (e KeyEvent) Has(m Mods) bool { return e.Mods&m != 0 }

// String renders the event in gesture syntax, so String and ParseGesture
// round-trip FOR ANY EVENT A DECODER PRODUCES.
//
// The qualifier arrived with #427 and is not decoration. ParseGesture now
// refuses the ctrl gestures no terminal can send, while String will still
// render one from a hand-built value: KeyEvent{Key: KeyRune, Rune: 'j',
// Mods: ModCtrl}.String() is "ctrl+j", which ParseGesture rejects. Every
// caller in the tree starts from a decoded event — grpc/convert.go's
// inputEventToProto and apps/wysiwyg/properties.go both do — so the
// narrower claim is the one they need, and it is true. Narrowed in
// review of #428.
func (e KeyEvent) String() string {
	var sb strings.Builder
	if e.Mods&ModCtrl != 0 {
		sb.WriteString("ctrl+")
	}
	if e.Mods&ModAlt != 0 {
		sb.WriteString("alt+")
	}
	if e.Mods&ModShift != 0 {
		sb.WriteString("shift+")
	}
	switch {
	case e.Key != KeyRune:
		sb.WriteString(keyName(e.Key))
	case e.Rune == ' ':
		sb.WriteString("space")
	default:
		sb.WriteRune(e.Rune)
	}
	return sb.String()
}

var keyNames = []struct {
	k    Key
	name string
}{
	{KeyEnter, "enter"},
	{KeyTab, "tab"},
	{KeyEsc, "esc"},
	{KeyBackspace, "backspace"},
	{KeyDelete, "delete"},
	{KeyUp, "up"},
	{KeyDown, "down"},
	{KeyLeft, "left"},
	{KeyRight, "right"},
	{KeyHome, "home"},
	{KeyEnd, "end"},
	{KeyPageUp, "pageup"},
	{KeyPageDown, "pagedown"},
}

func keyName(k Key) string {
	for _, n := range keyNames {
		if n.k == k {
			return n.name
		}
	}
	return "unknown"
}
