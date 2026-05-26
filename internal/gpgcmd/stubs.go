package gpgcmd

import (
	"io"
	"os"
)

// signKind is the flavour requested by --sign / --detach-sign /
// --clearsign. Defined here so options.go and the dispatcher can
// reference it without a circular import.
type signKind int

const (
	signInline signKind = iota
	signDetached
	signClear
)

// errStream returns the stderr writer (overridable in tests via the
// `stderr` variable in decrypt.go).
func errStream() io.Writer { return os.Stderr }
