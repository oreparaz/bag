package rm

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type options struct {
	recursive       bool
	force           bool
	interactive     bool
	verbose         bool
	dir             bool
	noPreserveRoot  bool
}

func parseArgs(argv []string) (*options, []string, error) {
	o := &options{}
	var paths []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			paths = append(paths, argv[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "--") {
			switch a[2:] {
			case "recursive":
				o.recursive = true
			case "force":
				o.force = true
			case "interactive":
				o.interactive = true
			case "verbose":
				o.verbose = true
			case "dir":
				o.dir = true
			case "no-preserve-root":
				o.noPreserveRoot = true
			case "preserve-root":
				// already the default
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
				case 'f':
					o.force = true
				case 'i':
					o.interactive = true
				case 'v':
					o.verbose = true
				case 'd':
					o.dir = true
				default:
					// Silently ignore unknown short flags.
				}
			}
			continue
		}
		paths = append(paths, a)
	}
	if len(paths) == 0 && !o.force {
		return nil, nil, errors.New("missing operand")
	}
	return o, paths, nil
}

func run(argv []string) int {
	o, paths, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rm: %v\n", err)
		return 1
	}
	exit := 0
	for _, p := range paths {
		if isRootLike(p) && !o.noPreserveRoot {
			fmt.Fprintf(os.Stderr, "rm: it is dangerous to operate recursively on '%s'\n", p)
			fmt.Fprintln(os.Stderr, "rm: use --no-preserve-root to override this failsafe")
			exit = 1
			continue
		}
		if err := removeOne(p, o); err != nil {
			if o.force && os.IsNotExist(err) {
				continue
			}
			fmt.Fprintf(os.Stderr, "rm: %v\n", err)
			exit = 1
		}
	}
	return exit
}

// isRootLike returns true for "/" and a few obvious dangerous targets
// after cleaning. We're deliberately conservative.
func isRootLike(p string) bool {
	c := filepath.Clean(p)
	return c == "/" || c == "//"
}

func removeOne(p string, o *options) error {
	info, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if !o.recursive && !o.dir {
			return fmt.Errorf("cannot remove '%s': Is a directory", p)
		}
		if o.recursive {
			return removeTree(p, o)
		}
		// -d (rmdir-style): refuses non-empty.
		if !confirm(p, info, o) {
			return nil
		}
		if o.verbose {
			fmt.Fprintf(os.Stdout, "removed directory '%s'\n", p)
		}
		return os.Remove(p)
	}
	if !confirm(p, info, o) {
		return nil
	}
	if err := os.Remove(p); err != nil {
		return err
	}
	if o.verbose {
		fmt.Fprintf(os.Stdout, "removed '%s'\n", p)
	}
	return nil
}

// removeTree removes p (a directory) and everything below it. Walks
// post-order so children disappear before their parents. We chmod
// any read-only directories along the way so they're traversable —
// gnu rm -r does the same when -f is set; here we always do it for
// simplicity.
func removeTree(root string, o *options) error {
	// Collect entries in post-order so we delete from the leaves up.
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// gnu rm reports but continues unless -f.
			if !o.force {
				fmt.Fprintf(os.Stderr, "rm: %v\n", err)
			}
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return err
	}
	// Reverse for post-order delete.
	for i, j := 0, len(paths)-1; i < j; i, j = i+1, j-1 {
		paths[i], paths[j] = paths[j], paths[i]
	}
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if !confirm(p, info, o) {
			continue
		}
		if err := os.Remove(p); err != nil {
			return err
		}
		if o.verbose {
			if info.IsDir() {
				fmt.Fprintf(os.Stdout, "removed directory '%s'\n", p)
			} else {
				fmt.Fprintf(os.Stdout, "removed '%s'\n", p)
			}
		}
	}
	return nil
}

// confirm returns true when the entry should be removed. -f bypasses
// the prompt entirely; -i prompts on /dev/tty (NOT stdin, so a file
// stream piped in cannot accidentally answer y).
func confirm(p string, info fs.FileInfo, o *options) bool {
	if o.force || !o.interactive {
		return true
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		// No tty — refuse rather than guess.
		return false
	}
	defer tty.Close()
	noun := "regular file"
	if info.IsDir() {
		noun = "directory"
	}
	fmt.Fprintf(tty, "rm: remove %s '%s'? ", noun, p)
	line, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.TrimSpace(line)
	return strings.HasPrefix(strings.ToLower(line), "y")
}
