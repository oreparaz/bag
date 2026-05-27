package cp

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
	"time"
)

type options struct {
	recursive   bool
	preserve    bool
	interactive bool
	force       bool
	noClobber   bool
	verbose     bool
	dereference bool
	noDeref     bool
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
			case "recursive":
				o.recursive = true
			case "preserve":
				o.preserve = true
			case "interactive":
				o.interactive = true
			case "force":
				o.force = true
			case "no-clobber":
				o.noClobber = true
			case "verbose":
				o.verbose = true
			case "dereference":
				o.dereference = true
			case "no-dereference":
				o.noDeref = true
			case "archive":
				o.recursive = true
				o.preserve = true
				o.noDeref = true
			default:
				// Ignore unknown long flags.
			}
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" && len(a) > 1 {
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'r', 'R':
					o.recursive = true
				case 'p':
					o.preserve = true
				case 'i':
					o.interactive = true
				case 'f':
					o.force = true
				case 'n':
					o.noClobber = true
				case 'v':
					o.verbose = true
				case 'L':
					o.dereference = true
				case 'P':
					o.noDeref = true
				case 'a':
					o.recursive = true
					o.preserve = true
					o.noDeref = true
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
		fmt.Fprintf(os.Stderr, "cp: %v\n", err)
		return 1
	}
	srcs, dst := rest[:len(rest)-1], rest[len(rest)-1]

	dstInfo, dstErr := os.Stat(dst)
	dstIsDir := dstErr == nil && dstInfo.IsDir()

	if len(srcs) > 1 && !dstIsDir {
		fmt.Fprintf(os.Stderr, "cp: target '%s' is not a directory\n", dst)
		return 1
	}

	exit := 0
	for _, src := range srcs {
		target := dst
		if dstIsDir {
			target = filepath.Join(dst, filepath.Base(src))
		}
		if err := copyAny(src, target, o); err != nil {
			fmt.Fprintf(os.Stderr, "cp: %v\n", err)
			exit = 1
		}
	}
	return exit
}

// copyAny picks the right copy strategy (file, symlink, dir) for src.
func copyAny(src, dst string, o *options) error {
	info, err := lstatOrStat(src, o)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return copySymlink(src, dst, info, o)
	case info.IsDir():
		if !o.recursive {
			return fmt.Errorf("-r not specified; omitting directory '%s'", src)
		}
		return copyDir(src, dst, info, o)
	default:
		return copyFile(src, dst, info, o)
	}
}

// lstatOrStat returns the source's info, following symlinks unless the
// caller explicitly asked to keep them (via -P / -a).
func lstatOrStat(p string, o *options) (fs.FileInfo, error) {
	if o.noDeref {
		return os.Lstat(p)
	}
	return os.Stat(p)
}

func copyFile(src, dst string, srcInfo fs.FileInfo, o *options) error {
	if exists, err := pathExists(dst); err != nil {
		return err
	} else if exists {
		if o.noClobber {
			return nil
		}
		if !o.force && o.interactive {
			if !confirmOverwrite(dst) {
				return nil
			}
		}
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	mode := fs.FileMode(0o644)
	if o.preserve {
		mode = srcInfo.Mode().Perm()
	}
	// If we may overwrite an existing read-only file, the Create call
	// will fail with EACCES — force-mode in real cp first unlinks.
	if o.force {
		_ = os.Remove(dst)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
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
	if o.preserve {
		_ = os.Chtimes(dst, time.Now(), srcInfo.ModTime())
		// Best-effort chown — only root can change owner across uids.
		if st, ok := srcInfo.Sys().(*syscall.Stat_t); ok {
			_ = os.Chown(dst, int(st.Uid), int(st.Gid))
		}
	}
	if o.verbose {
		fmt.Fprintf(os.Stdout, "'%s' -> '%s'\n", src, dst)
	}
	return nil
}

func copySymlink(src, dst string, info fs.FileInfo, o *options) error {
	target, err := os.Readlink(src)
	if err != nil {
		return err
	}
	// If --dereference was passed, the caller wanted the symlink target
	// followed. Defer to copyFile/copyDir against the actual target.
	if o.dereference || !o.noDeref {
		// Fall back to follow: stat through the symlink.
		realInfo, err := os.Stat(src)
		if err != nil {
			return err
		}
		if realInfo.IsDir() {
			if !o.recursive {
				return fmt.Errorf("-r not specified; omitting symlink-to-directory '%s'", src)
			}
			return copyDir(src, dst, realInfo, o)
		}
		return copyFile(src, dst, realInfo, o)
	}
	// Reproduce the symlink.
	if exists, err := pathExists(dst); err != nil {
		return err
	} else if exists {
		if o.noClobber {
			return nil
		}
		if o.force {
			_ = os.Remove(dst)
		}
	}
	if err := os.Symlink(target, dst); err != nil {
		return err
	}
	if o.verbose {
		fmt.Fprintf(os.Stdout, "'%s' -> '%s'\n", src, dst)
	}
	return nil
}

func copyDir(src, dst string, srcInfo fs.FileInfo, o *options) error {
	mode := srcInfo.Mode().Perm() | 0o700 // ensure traversable
	if err := os.MkdirAll(dst, mode); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if err := copyAny(s, d, o); err != nil {
			// Match gnu: report and continue.
			fmt.Fprintf(os.Stderr, "cp: %v\n", err)
		}
	}
	if o.preserve {
		_ = os.Chmod(dst, srcInfo.Mode().Perm())
		_ = os.Chtimes(dst, time.Now(), srcInfo.ModTime())
	}
	return nil
}

func pathExists(p string) (bool, error) {
	_, err := os.Lstat(p)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func confirmOverwrite(dst string) bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer tty.Close()
	fmt.Fprintf(tty, "cp: overwrite '%s'? ", dst)
	line, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y")
}
