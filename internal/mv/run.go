package mv

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type options struct {
	force       bool
	interactive bool
	noClobber   bool
	verbose     bool
}

func parseArgs(argv []string) (*options, []string, error) {
	o := &options{}
	var rest []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			rest = append(rest, argv[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "--") {
			switch a[2:] {
			case "force":
				o.force = true
			case "interactive":
				o.interactive = true
			case "no-clobber":
				o.noClobber = true
			case "verbose":
				o.verbose = true
			default:
				// Ignore unknown long flags.
			}
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" && len(a) > 1 {
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'f':
					o.force = true
				case 'i':
					o.interactive = true
				case 'n':
					o.noClobber = true
				case 'v':
					o.verbose = true
				default:
					// Silently ignore unknown short flags.
				}
			}
			continue
		}
		rest = append(rest, a)
	}
	if len(rest) < 2 {
		return nil, nil, errors.New("missing file operand")
	}
	return o, rest, nil
}

func run(argv []string) int {
	o, rest, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mv: %v\n", err)
		return 1
	}
	srcs, dst := rest[:len(rest)-1], rest[len(rest)-1]

	dstInfo, dstErr := os.Stat(dst)
	dstIsDir := dstErr == nil && dstInfo.IsDir()
	if len(srcs) > 1 && !dstIsDir {
		fmt.Fprintf(os.Stderr, "mv: target '%s' is not a directory\n", dst)
		return 1
	}

	exit := 0
	for _, src := range srcs {
		target := dst
		if dstIsDir {
			target = filepath.Join(dst, filepath.Base(src))
		}
		if err := moveOne(src, target, o); err != nil {
			fmt.Fprintf(os.Stderr, "mv: %v\n", err)
			exit = 1
		}
	}
	return exit
}

func moveOne(src, dst string, o *options) error {
	if _, err := os.Lstat(src); err != nil {
		return err
	}
	if _, err := os.Lstat(dst); err == nil {
		// Destination exists.
		if o.noClobber {
			return nil
		}
		if !o.force && o.interactive {
			if !confirmOverwrite(dst) {
				return nil
			}
		}
		if o.force {
			// Force-mode removes the destination first so a read-only
			// or symlinked target is replaced rather than written through.
			_ = os.RemoveAll(dst)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.Rename(src, dst); err == nil {
		if o.verbose {
			fmt.Fprintf(os.Stdout, "renamed '%s' -> '%s'\n", src, dst)
		}
		return nil
	} else if !isCrossDevice(err) {
		return err
	}

	// EXDEV fallback: copy then remove.
	if err := copyAcrossDevices(src, dst); err != nil {
		return err
	}
	if err := os.RemoveAll(src); err != nil {
		return err
	}
	if o.verbose {
		fmt.Fprintf(os.Stdout, "renamed '%s' -> '%s'\n", src, dst)
	}
	return nil
}

func isCrossDevice(err error) bool {
	var le *os.LinkError
	if errors.As(err, &le) {
		if errno, ok := le.Err.(syscall.Errno); ok && errno == syscall.EXDEV {
			return true
		}
	}
	return false
}

// copyAcrossDevices does the minimum needed to relocate a single entry
// (file / symlink / directory tree) across filesystems. Modes and
// mtimes are preserved; ownership is best-effort.
func copyAcrossDevices(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case info.IsDir():
		if err := os.MkdirAll(dst, info.Mode().Perm()|0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyAcrossDevices(
				filepath.Join(src, e.Name()),
				filepath.Join(dst, e.Name()),
			); err != nil {
				return err
			}
		}
		_ = os.Chmod(dst, info.Mode().Perm())
		return nil
	default:
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		_ = os.Chtimes(dst, info.ModTime(), info.ModTime())
		return nil
	}
}

func confirmOverwrite(dst string) bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer tty.Close()
	fmt.Fprintf(tty, "mv: overwrite '%s'? ", dst)
	line, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y")
}
