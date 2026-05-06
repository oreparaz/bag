package vi

// Key is a single keystroke.
//
// We don't try to model every modifier; the editor only needs:
//
//   - printable runes (a, A, 1, !, ...)
//   - a small set of named keys (ESC, Enter, Backspace, Arrow*, PageUp/Dn, Home/End, Delete, Tab)
//   - Ctrl-letter combinations (Ctrl-R, Ctrl-C, ...)
type Key struct {
	Rune rune    // 0 if Code is set
	Code KeyCode // 0 if Rune is set
	Ctrl bool    // true for Ctrl-<letter>; Rune carries the lowercase letter
}

// KeyCode is the discriminator for non-rune keys.
type KeyCode int

const (
	KeyNone KeyCode = iota
	KeyEsc
	KeyEnter
	KeyBackspace
	KeyTab
	KeyDelete
	KeyArrowUp
	KeyArrowDown
	KeyArrowLeft
	KeyArrowRight
	KeyPageUp
	KeyPageDown
	KeyHome
	KeyEnd
)

// Rune returns a Key for a printable character.
func RuneKey(r rune) Key { return Key{Rune: r} }

// CtrlKey returns a Key for Ctrl-<letter>. letter should be lowercase.
func CtrlKey(letter rune) Key { return Key{Rune: letter, Ctrl: true} }

// CodeKey returns a Key for a named non-rune key.
func CodeKey(c KeyCode) Key { return Key{Code: c} }
