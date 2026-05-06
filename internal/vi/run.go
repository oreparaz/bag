package vi

import (
	"fmt"
	"io"
	"os"
)

// run is the top-level dispatcher for `bag vi`.
//
// Usage:
//
//	bag vi [FILE]
//	bag vi --help
//	bag vi --version
//
// With no FILE we open an empty buffer.
func run(args []string) int {
	for _, a := range args {
		switch a {
		case "--help", "-h":
			printHelp(os.Stdout)
			return 0
		case "--version":
			fmt.Println("vi (bag) -- bag drop-in")
			return 0
		}
	}

	e := NewEditor()
	if len(args) > 0 {
		if err := e.Open(args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "vi: %v\n", err)
			return 1
		}
	}
	return runTUI(e)
}

func printHelp(w io.Writer) {
	const help = `Usage: vi [FILE]
A small modal editor in the vi family.

Modes:
  Normal   default; movement and operators
  Insert   text input until ESC
  Command  ':' commands (:w, :q, :wq, :q!)
  Search   '/' / '?' pattern entry, then n / N

Motions:  h j k l, w b e, 0 ^ $, gg G, NG (line N)
Edits:    i I a A o O, x X, dd dw D, cc cw C, yy yw Y, p P, J, r, u, Ctrl-R
Search:   / pat <CR>, ? pat <CR>, n, N
Files:    :w, :w PATH, :q, :q!, :wq

Syntax: minimal highlight for Go, Python, C/C++, JS/TS, shell.
`
	io.WriteString(w, help)
}
