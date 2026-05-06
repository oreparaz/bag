// Package vi is a small modal editor in the vi family. The goal is a
// useful editor that fits in a few thousand lines and is comfortable to
// read end-to-end.
//
// Modes:
//
//	Normal   default; movement and operators
//	Insert   text input until ESC
//	Command  ':' commands (:w, :q, :wq, :q!)
//	Search   '/' / '?' pattern entry, then n / N
//
// Motions:  h j k l, w b e, 0 ^ $, gg G
// Edits:    i I a A o O, x X, dd dw D, cc cw C, yy yw Y, p P, J, r, u, Ctrl-R
// Search:   / pat <CR>, ? pat <CR>, n, N
// Files:    :w, :w PATH, :q, :q!, :wq
//
// Editor state lives entirely in-memory; the terminal driver is a thin
// adapter on top so the headless test harness can drive the editor by
// calling Editor.Key directly.
//
// Syntax highlighting is intentionally minimal: language detection from
// file extension, then per-line tokenisation into keyword / string /
// comment / number / plain spans.
package vi

// Main is the bag-dispatch entry point.
func Main(args []string) int { return run(args) }
