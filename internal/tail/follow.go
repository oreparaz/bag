package tail

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// tracked is a single file being followed.
type tracked struct {
	path     string
	f        *os.File
	offset   int64
	gone     bool
	inum     uint64 // for rotation detection
	lastHead bool
}

// followAll polls each file every opts.pollEvery and emits any newly-appended
// bytes to w. Stops on SIGINT / SIGTERM.
//
// On file truncation we emit a notice and re-seek to 0. On file disappearance:
//   - -f (follow): stop tracking the file
//   - -F (follow + retry): keep retrying until it returns
//
// stdin ("-") is followed via a blocking io.Copy — pipes never go away the
// way regular files do.
func followAll(w io.Writer, files []string, opts *options, printHeaders bool) int {
	tracks := make([]*tracked, 0, len(files))
	currentHead := -1
	for _, path := range files {
		t := &tracked{path: path}
		if path == "-" {
			// Stdin: just stream forever.
			if printHeaders {
				if currentHead >= 0 {
					fmt.Fprintln(w)
				}
				fmt.Fprintf(w, "==> %s <==\n", displayName(path))
				currentHead = len(tracks)
			}
			_, _ = io.Copy(w, os.Stdin)
			return 0
		}
		f, err := os.Open(path)
		if err != nil {
			if !opts.followRetry {
				fmt.Fprintf(os.Stderr, "tail: cannot open %q for following\n", path)
				continue
			}
			t.gone = true
		} else {
			fi, _ := f.Stat()
			t.f = f
			t.offset = fi.Size()
			t.inum = inum(fi)
			// Seek to end so we only emit subsequent appends. The initial
			// "last 10 lines" content has already been written by the
			// non-follow emit pass in run.go.
			_, _ = f.Seek(t.offset, io.SeekStart)
		}
		tracks = append(tracks, t)
	}

	if len(tracks) == 0 {
		return 1
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)

	tick := time.NewTicker(opts.pollEvery)
	defer tick.Stop()

	switchHead := func(idx int) {
		if !printHeaders {
			return
		}
		if idx == currentHead {
			return
		}
		if currentHead >= 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "==> %s <==\n", displayName(tracks[idx].path))
		currentHead = idx
	}

	for {
		select {
		case <-stop:
			return 0
		case <-tick.C:
		}
		for i, t := range tracks {
			if err := pollOne(w, t, opts, func() { switchHead(i) }); err != nil {
				fmt.Fprintf(os.Stderr, "tail: %s: %v\n", t.path, err)
			}
		}
	}
}

func pollOne(w io.Writer, t *tracked, opts *options, beforeWrite func()) error {
	fi, err := os.Stat(t.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if !opts.followRetry {
				if t.f != nil {
					t.f.Close()
					t.f = nil
				}
				return nil
			}
			if !t.gone {
				t.gone = true
				fmt.Fprintf(os.Stderr, "tail: %s: file disappeared\n", t.path)
				if t.f != nil {
					t.f.Close()
					t.f = nil
				}
			}
			return nil
		}
		return err
	}

	// Reopen if the file was missing previously, or if its inode changed
	// (rotation), or if it shrank (truncation).
	if t.f == nil {
		f, err := os.Open(t.path)
		if err != nil {
			return err
		}
		t.f = f
		t.offset = 0
		t.inum = inum(fi)
		t.gone = false
		fmt.Fprintf(os.Stderr, "tail: %s: opened\n", t.path)
	}
	if newInum := inum(fi); newInum != t.inum && newInum != 0 {
		t.f.Close()
		f, err := os.Open(t.path)
		if err != nil {
			return err
		}
		t.f = f
		t.offset = 0
		t.inum = newInum
		fmt.Fprintf(os.Stderr, "tail: %s: rotated\n", t.path)
	}
	if fi.Size() < t.offset {
		t.f.Seek(0, io.SeekStart)
		t.offset = 0
		fmt.Fprintf(os.Stderr, "tail: %s: truncated\n", t.path)
	}
	if fi.Size() == t.offset {
		return nil
	}

	beforeWrite()
	n, err := io.Copy(w, t.f)
	t.offset += n
	if err != nil {
		return err
	}
	return nil
}

// inum extracts the inode number from a FileInfo on Unix. Returns 0 on
// platforms or filesystems where it isn't available.
func inum(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return 0
}
