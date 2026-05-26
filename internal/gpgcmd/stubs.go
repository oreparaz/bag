package gpgcmd

import (
	"errors"
	"io"
	"os"
)

// These are stubs that will be filled in by later milestones. We
// declare them now so the dispatcher in run.go compiles before the
// real implementations land.

type signKind int

const (
	signInline signKind = iota
	signDetached
	signClear
)

func doSign(o *options, k signKind) error {
	return errors.New("sign: not implemented yet")
}
func doVerify(o *options) error { return errors.New("verify: not implemented yet") }

// errStream returns the stderr writer (overridable in tests via the
// `stderr` variable in decrypt.go).
func errStream() io.Writer { return os.Stderr }
