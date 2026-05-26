package scp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// runUpload connects to o.dst.host and pushes o.srcs into o.dst.path.
//
// We exec `scp -t [-r] [-p] [-d] DEST` on the remote, then drive the
// classic protocol: send a C-record per file, receive an ack, send
// bytes, send the trailing NUL, receive an ack. -r adds D/E records
// per directory.
func runUpload(o *options) error {
	for _, s := range o.srcs {
		if s.isRemote() {
			return errors.New("upload: all sources must be local")
		}
	}

	client, err := connectFor(o.dst, o)
	if err != nil {
		return err
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	stdin, err := sess.StdinPipe()
	if err != nil {
		return err
	}
	stdoutPipe, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	stdout := bufio.NewReader(stdoutPipe)
	sess.Stderr = os.Stderr

	cmd := buildRemoteCmd("-t", o)
	if err := sess.Start(cmd); err != nil {
		return err
	}

	if err := readAck(stdout); err != nil {
		return fmt.Errorf("scp: server rejected: %w", err)
	}

	for _, s := range o.srcs {
		if err := uploadOne(s.path, stdin, stdout, o); err != nil {
			stdin.Close()
			_ = sess.Wait()
			return err
		}
	}
	stdin.Close()

	if err := sess.Wait(); err != nil {
		return err
	}
	return nil
}

func buildRemoteCmd(mode string, o *options) string {
	flags := mode
	if o.recursive {
		flags += "r"
	}
	if o.preserve {
		flags += "p"
	}
	if o.quiet {
		flags += "q"
	}
	if mode == "-t" {
		flags += "d" // accept destination being a directory
	}
	return "scp " + flags + " " + shellQuote(targetPath(o, mode))
}

func targetPath(o *options, mode string) string {
	if mode == "-t" {
		// `host:` (no path) means "the user's home directory", which
		// every other scp implementation expresses as ".". Sending an
		// empty path makes the remote scp -t error out.
		if o.dst.path == "" {
			return "."
		}
		return o.dst.path
	}
	// -f: there's only one source for now (multi-source download is
	// handled by spawning N sessions, one per source — each gets its
	// own targetPath).
	return o.srcs[0].path
}

// shellQuote wraps in single quotes, escaping inner singles.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// uploadOne sends one local path. Recurses into directories when -r.
func uploadOne(localPath string, w io.Writer, r *bufio.Reader, o *options) error {
	info, err := os.Lstat(localPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if !o.recursive {
			return fmt.Errorf("%s: is a directory (use -r)", localPath)
		}
		return uploadDir(localPath, info, w, r, o)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// Resolve the target — scp follows symlinks for sources.
		info, err = os.Stat(localPath)
		if err != nil {
			return err
		}
		// A symlink-to-directory must recurse; otherwise we'd send an
		// empty C-record claiming the symlink target's size and silently
		// drop the directory contents.
		if info.IsDir() {
			if !o.recursive {
				return fmt.Errorf("%s: is a directory (symlink target; use -r)", localPath)
			}
			return uploadDir(localPath, info, w, r, o)
		}
	}
	return uploadFile(localPath, info, w, r, o)
}

func uploadDir(dir string, info os.FileInfo, w io.Writer, r *bufio.Reader, o *options) error {
	if o.preserve {
		if err := sendTimes(info, w, r); err != nil {
			return err
		}
	}
	mode := info.Mode().Perm()
	if _, err := fmt.Fprintf(w, "D%04o 0 %s\n", mode, filepath.Base(dir)); err != nil {
		return err
	}
	if err := readAck(r); err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, de := range entries {
		child := filepath.Join(dir, de.Name())
		if err := uploadOne(child, w, r, o); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprint(w, "E\n"); err != nil {
		return err
	}
	return readAck(r)
}

func uploadFile(path string, info os.FileInfo, w io.Writer, r *bufio.Reader, o *options) error {
	if o.preserve {
		if err := sendTimes(info, w, r); err != nil {
			return err
		}
	}
	// Reject filenames the SCP wire protocol cannot represent. A file
	// containing \n / \r / NUL would split the C-record across lines and
	// the next file's record could be parsed as data on the receiving
	// side, leaving an attacker-controlled file on the remote.
	base := filepath.Base(path)
	if strings.ContainsAny(base, "\n\r\x00") {
		return fmt.Errorf("scp: refusing filename with newline or NUL: %q", base)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Re-stat the open file descriptor instead of trusting the prior
	// Lstat result. Otherwise a race between Lstat and Open lets the file
	// shrink/grow between the size we advertise in the C-record and the
	// bytes we actually send, desyncing the protocol.
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	mode := fi.Mode().Perm()
	size := fi.Size()
	if _, err := fmt.Fprintf(w, "C%04o %d %s\n", mode, size, base); err != nil {
		return err
	}
	if err := readAck(r); err != nil {
		return err
	}
	// CopyN guarantees we write exactly `size` bytes regardless of any
	// post-stat changes; a short read is reported as an error rather
	// than silently sending less.
	if _, err := io.CopyN(w, f, size); err != nil {
		return err
	}
	// End-of-file marker.
	if _, err := w.Write([]byte{0}); err != nil {
		return err
	}
	return readAck(r)
}

// sendTimes emits a T record (mtime + atime) before the next entry. If
// the remote refuses, we surface its message.
func sendTimes(info os.FileInfo, w io.Writer, r *bufio.Reader) error {
	mtime := info.ModTime().Unix()
	if _, err := fmt.Fprintf(w, "T%d 0 %d 0\n", mtime, mtime); err != nil {
		return err
	}
	return readAck(r)
}

// readAck consumes one byte from r. \x00 is OK; \x01/\x02 are warning
// (continue) and error (abort) respectively, followed by a message line.
func readAck(r *bufio.Reader) error {
	b, err := r.ReadByte()
	if err != nil {
		return err
	}
	switch b {
	case 0:
		return nil
	case 1, 2:
		msg, _ := r.ReadString('\n')
		return fmt.Errorf("remote: %s", strings.TrimRight(msg, "\n"))
	}
	return fmt.Errorf("scp: unexpected ack byte %d", b)
}

// silence import grumbles for files that may be reused.
var _ = ssh.Dial
