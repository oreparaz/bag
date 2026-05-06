package scp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/oreparaz/bag/internal/safefs"
)

// runDownload pulls each remote source into the local destination.
// Each source gets its own SSH session (matches openssh's scp).
func runDownload(o *options) error {
	for _, s := range o.srcs {
		if err := downloadOne(s, o); err != nil {
			return err
		}
	}
	return nil
}

func downloadOne(src endpoint, o *options) error {
	if !src.isRemote() {
		return errors.New("download: source must be remote")
	}
	client, err := connectFor(src, o)
	if err != nil {
		return err
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	stdinPipe, err := sess.StdinPipe()
	if err != nil {
		return err
	}
	stdoutPipe, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	stdin := stdinPipe
	stdout := bufio.NewReader(stdoutPipe)
	sess.Stderr = os.Stderr

	cmd := "scp " + downloadFlags(o) + " " + shellQuote(src.path)
	if err := sess.Start(cmd); err != nil {
		return err
	}

	// Send first ack to start the flow.
	if _, err := stdin.Write([]byte{0}); err != nil {
		return err
	}

	dstRoot := o.dst.path
	if dstRoot == "" {
		dstRoot = "."
	}
	if err := receiveTree(stdout, stdin, dstRoot, o); err != nil {
		stdin.Close()
		_ = sess.Wait()
		return err
	}
	stdin.Close()
	return sess.Wait()
}

func downloadFlags(o *options) string {
	flags := "-f"
	if o.recursive {
		flags += "r"
	}
	if o.preserve {
		flags += "p"
	}
	if o.quiet {
		flags += "q"
	}
	return flags
}

// receiveTree drives the response side of the protocol. It reads
// records until EOF or an explicit error byte, dispatching on the
// first byte:
//
//	'T'  set times for the next entry (with -p)
//	'C'  regular file
//	'D'  directory begin
//	'E'  directory end
//	0x01 / 0x02  warning / error from remote
func receiveTree(r *bufio.Reader, w io.Writer, root string, o *options) error {
	rootInfo, _ := os.Stat(root)
	rootIsDir := rootInfo != nil && rootInfo.IsDir()

	// stack tracks the current directory we're descending into. When
	// the destination is NOT an existing directory, an incoming single
	// C-record should write to root verbatim — we model that by
	// starting with an empty stack.
	var stack []string
	if rootIsDir {
		stack = []string{root}
	}
	pendingTimes := timesPair{}

	for {
		first, err := r.ReadByte()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch first {
		case 1, 2:
			msg, _ := r.ReadString('\n')
			err := fmt.Errorf("remote: %s", strings.TrimRight(msg, "\n"))
			if first == 1 {
				fmt.Fprintln(os.Stderr, err)
				continue
			}
			return err
		case 'C', 'D':
			line, err := r.ReadString('\n')
			if err != nil {
				return err
			}
			line = strings.TrimRight(line, "\n")
			mode, size, name, err := parseEntryHeader(line)
			if err != nil {
				return err
			}
			var target string
			if len(stack) > 0 {
				target = filepath.Join(stack[len(stack)-1], name)
			} else {
				// Single-file write: ignore the server's name, use the
				// user-supplied dst path verbatim.
				target = root
			}
			if first == 'C' {
				if err := receiveFile(r, w, target, mode, size, pendingTimes); err != nil {
					return err
				}
				pendingTimes = timesPair{}
				continue
			}
			// Directory entry — meaningful for recursive transfers.
			parent := root
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			if err := safefs.MkdirAllNoSymlinkLeaf(parent, target, os.FileMode(mode)); err != nil {
				return err
			}
			stack = append(stack, target)
			if _, err := w.Write([]byte{0}); err != nil {
				return err
			}
			pendingTimes = timesPair{}
		case 'E':
			if _, err := r.ReadString('\n'); err != nil {
				return err
			}
			if len(stack) == 0 {
				return errors.New("scp: unexpected E record")
			}
			stack = stack[:len(stack)-1]
			if _, err := w.Write([]byte{0}); err != nil {
				return err
			}
		case 'T':
			line, err := r.ReadString('\n')
			if err != nil {
				return err
			}
			t, err := parseTimes(strings.TrimRight(line, "\n"))
			if err != nil {
				return err
			}
			pendingTimes = t
			if _, err := w.Write([]byte{0}); err != nil {
				return err
			}
		case '\n':
			// Some servers send a trailing newline at EOF; ignore.
		default:
			return fmt.Errorf("scp: unexpected record byte %q", first)
		}
	}
}

// parseEntryHeader pulls (mode, size, name) out of a line. The 'C' or
// 'D' has already been consumed by the caller.
func parseEntryHeader(line string) (mode int, size int64, name string, err error) {
	parts := strings.SplitN(line, " ", 3)
	if len(parts) != 3 {
		return 0, 0, "", fmt.Errorf("scp: malformed header %q", line)
	}
	m, perr := strconv.ParseInt(parts[0], 8, 32)
	if perr != nil {
		return 0, 0, "", fmt.Errorf("scp: bad mode %q", parts[0])
	}
	s, perr := strconv.ParseInt(parts[1], 10, 64)
	if perr != nil {
		return 0, 0, "", fmt.Errorf("scp: bad size %q", parts[1])
	}
	if err := safefs.RefusePathTraversal(parts[2]); err != nil {
		return 0, 0, "", fmt.Errorf("scp: refusing entry %q", parts[2])
	}
	return int(m), s, parts[2], nil
}

type timesPair struct{ mtime, atime int64 }

func parseTimes(line string) (timesPair, error) {
	parts := strings.Fields(line)
	if len(parts) != 4 {
		return timesPair{}, fmt.Errorf("scp: bad T record %q", line)
	}
	mtime, _ := strconv.ParseInt(parts[0], 10, 64)
	atime, _ := strconv.ParseInt(parts[2], 10, 64)
	return timesPair{mtime: mtime, atime: atime}, nil
}

// receiveFile reads size bytes into target, then expects a NUL ack.
// The destination directory must already exist.
func receiveFile(r *bufio.Reader, w io.Writer, target string, mode int, size int64, t timesPair) error {
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	out, err := safefs.CreateTrunc(target, os.FileMode(mode))
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte{0}); err != nil {
		out.Close()
		return err
	}
	if _, err := io.CopyN(out, r, size); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// End-of-file NUL.
	end, err := r.ReadByte()
	if err != nil {
		return err
	}
	if end != 0 {
		return fmt.Errorf("scp: expected NUL after file body, got %d", end)
	}
	if _, err := w.Write([]byte{0}); err != nil {
		return err
	}
	if t.mtime > 0 {
		_ = setTimes(target, t)
	}
	return nil
}
