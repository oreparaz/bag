package ssh

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// runSession either executes the user-supplied command (no PTY by
// default) or opens an interactive shell (PTY + raw mode locally).
func runSession(client *xssh.Client, o *options) error {
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	wantPTY := wantPTY(o)
	if wantPTY {
		if err := requestPTY(sess); err != nil {
			return err
		}
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return err
	}

	// If we asked for a PTY, the local terminal needs to be in raw
	// mode so keystrokes pass through unprocessed.
	var restore func()
	if wantPTY {
		fd := int(os.Stdin.Fd())
		if term.IsTerminal(fd) {
			st, err := term.MakeRaw(fd)
			if err == nil {
				restore = func() { _ = term.Restore(fd, st) }
			}
		}
	}
	if restore != nil {
		defer restore()
	}

	// Start the remote endpoint.
	if len(o.command) > 0 {
		cmd := strings.Join(quoteArgs(o.command), " ")
		if err := sess.Start(cmd); err != nil {
			return err
		}
	} else {
		if err := sess.Shell(); err != nil {
			return err
		}
	}

	// Bidirectional pumps. We can't just exec.Cmd-style wait — we
	// need to make sure stdin doesn't block forever after the remote
	// exits. The pipe Close in defer (above, via sess.Close) handles
	// that.
	done := make(chan struct{}, 3)
	go func() { _, _ = io.Copy(stdin, os.Stdin); _ = stdin.Close(); done <- struct{}{} }()
	go func() { _, _ = io.Copy(os.Stdout, stdout); done <- struct{}{} }()
	go func() { _, _ = io.Copy(os.Stderr, stderr); done <- struct{}{} }()

	err = sess.Wait()
	// Drain stdout/stderr — but stdin may still be blocked on a TTY
	// read. We don't wait for it.
	<-done
	<-done

	if err != nil {
		var ee *xssh.ExitError
		if errors.As(err, &ee) {
			return &exitCodeError{code: ee.ExitStatus(), msg: ee.Error()}
		}
		return err
	}
	return nil
}

// wantPTY: openssh allocates a PTY iff we're running an interactive
// shell (no command given) OR the user passed -t. -T disables.
func wantPTY(o *options) bool {
	if o.noTTY {
		return false
	}
	if o.forceTTY {
		return true
	}
	return len(o.command) == 0
}

// requestPTY asks the server for a PTY of the local terminal's size.
func requestPTY(sess *xssh.Session) error {
	fd := int(os.Stdin.Fd())
	w, h := 80, 24
	if term.IsTerminal(fd) {
		if cw, ch, err := term.GetSize(fd); err == nil {
			w, h = cw, ch
		}
	}
	modes := xssh.TerminalModes{
		xssh.ECHO:          1,
		xssh.TTY_OP_ISPEED: 14400,
		xssh.TTY_OP_OSPEED: 14400,
	}
	termType := os.Getenv("TERM")
	if termType == "" {
		termType = "xterm-256color"
	}
	return sess.RequestPty(termType, h, w, modes)
}

// quoteArgs surrounds each arg with single quotes, escaping embedded
// single quotes the POSIX-shell-safe way (' → '\''). The remote shell
// will reassemble.
func quoteArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return out
}

func stderrW() io.Writer {
	if w, _ := os.Stderr.Stat(); w != nil {
		return os.Stderr
	}
	return io.Discard
}

// silence unused-import quibbles in some build configurations.
var _ = fmt.Errorf
