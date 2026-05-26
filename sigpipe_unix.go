//go:build unix

package main

import (
	"os/signal"
	"syscall"
)

func resetSIGPIPE() {
	signal.Reset(syscall.SIGPIPE)
}
