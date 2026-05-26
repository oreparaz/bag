package zipcmd

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/oreparaz/bag/internal/safefs"
)

type unzipOptions struct {
	archive string
	files   []string

	list      bool
	pipe      bool
	quiet     bool
	overwrite bool
	never     bool
	junk      bool
	dir       string

	printHelp    bool
	printVersion bool
}

func runUnzip(args []string) int {
	o, err := parseUnzipArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unzip: %v\n", err)
		return 1
	}
	if o.printHelp {
		printUnzipHelp(os.Stdout)
		return 0
	}
	if o.printVersion {
		fmt.Println("unzip (bag) -- bag drop-in")
		return 0
	}
	if o.archive == "" {
		fmt.Fprintln(os.Stderr, "unzip: missing archive name")
		return 1
	}

	zr, closer, err := openZipReader(o.archive)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unzip: %v\n", err)
		return 1
	}
	defer closer()

	if o.list {
		return doList(zr, o)
	}
	return doExtract(zr, o)
}

type zipReadAt interface {
	io.ReaderAt
}

func openZipReader(path string) (*zip.Reader, func(), error) {
	if path == "-" {
		// Read full stdin into memory.
		buf, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, nil, err
		}
		zr, err := zip.NewReader(stickyReadAt(buf), int64(len(buf)))
		if err != nil {
			return nil, nil, err
		}
		return zr, func() {}, nil
	}
	rc, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil, err
	}
	return &rc.Reader, func() { rc.Close() }, nil
}

type stickyReadAt []byte

func (b stickyReadAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b)) {
		return 0, io.EOF
	}
	n := copy(p, b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func doList(zr *zip.Reader, o *unzipOptions) int {
	// "Long" listing format: length, date, time, name.
	fmt.Println("  Length      Date    Time    Name")
	fmt.Println("---------  ---------- -----   ----")
	var total int64
	count := 0
	for _, f := range zr.File {
		if !matchPatterns(f.Name, o.files) {
			continue
		}
		fmt.Printf("%9d  %s   %s\n", f.UncompressedSize64,
			f.Modified.UTC().Format("2006-01-02 15:04"), f.Name)
		total += int64(f.UncompressedSize64)
		count++
	}
	fmt.Println("---------                     -------")
	fmt.Printf("%9d                     %d files\n", total, count)
	return 0
}

func doExtract(zr *zip.Reader, o *unzipOptions) int {
	exit := 0
	for _, f := range zr.File {
		if !matchPatterns(f.Name, o.files) {
			continue
		}
		if err := extractOne(f, o); err != nil {
			fmt.Fprintf(os.Stderr, "unzip: %s: %v\n", f.Name, err)
			exit = 1
		}
	}
	return exit
}

func extractOne(f *zip.File, o *unzipOptions) error {
	rel := f.Name
	if o.junk {
		rel = filepath.Base(rel)
	}
	if err := safefs.RefusePathTraversal(rel); err != nil {
		return errors.New("refusing extraction outside output dir")
	}
	// The extraction root is o.dir (-d) or cwd. Components above the
	// root may legitimately be symlinks (e.g. /var on macOS) and aren't
	// attacker-controlled, so safefs only walks below root.
	root := o.dir
	if root == "" {
		root = "."
	}
	target := rel
	if o.dir != "" {
		target = filepath.Join(o.dir, rel)
	}
	if err := safefs.EnsureNoSymlinkInPath(root, target); err != nil {
		return err
	}

	if strings.HasSuffix(f.Name, "/") {
		return safefs.MkdirAllNoSymlinkLeaf(root, target, f.Mode().Perm())
	}

	if o.pipe {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		_, err = io.Copy(os.Stdout, rc)
		return err
	}

	if !o.overwrite {
		if _, err := os.Lstat(target); err == nil {
			if o.never {
				if !o.quiet {
					fmt.Fprintf(os.Stderr, "  skipping: %s\n", target)
				}
				return nil
			}
			// Default: overwrite. To match Info-ZIP exactly we'd prompt the
			// user; bag is non-interactive.
		}
	}

	if err := safefs.MkdirAllNoSymlinkLeaf(root, filepath.Dir(target), 0o755); err != nil {
		return err
	}
	mode := f.Mode()
	if f.Mode()&os.ModeSymlink != 0 {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		// Cap symlink target reads at PATH_MAX-ish. A hostile zip can
		// otherwise declare a multi-GB symlink target and OOM bag during
		// extraction.
		const maxSymlinkTarget = 4096
		body, err := io.ReadAll(io.LimitReader(rc, maxSymlinkTarget+1))
		rc.Close()
		if err != nil {
			return err
		}
		if len(body) > maxSymlinkTarget {
			return fmt.Errorf("unzip: symlink target too long for %q", f.Name)
		}
		_ = os.Remove(target)
		return os.Symlink(string(body), target)
	}
	out, err := safefs.CreateTrunc(target, mode.Perm()|0o600)
	if err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		out.Close()
		return err
	}
	defer rc.Close()
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if !o.quiet {
		fmt.Fprintf(os.Stderr, "  inflating: %s\n", target)
	}
	return nil
}

func matchPatterns(name string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}

func parseUnzipArgs(args []string) (*unzipOptions, error) {
	o := &unzipOptions{}
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "" || a[0] != '-' || a == "-" {
			if o.archive == "" {
				o.archive = a
			} else {
				o.files = append(o.files, a)
			}
			i++
			continue
		}
		if a == "--" {
			for _, f := range args[i+1:] {
				if o.archive == "" {
					o.archive = f
				} else {
					o.files = append(o.files, f)
				}
			}
			break
		}
		if strings.HasPrefix(a, "--") {
			switch a[2:] {
			case "list":
				o.list = true
			case "pipe":
				o.pipe = true
			case "quiet":
				o.quiet = true
			case "overwrite":
				o.overwrite = true
			case "never-overwrite":
				o.never = true
			case "junk-paths":
				o.junk = true
			case "help":
				o.printHelp = true
			case "version":
				o.printVersion = true
			default:
				return nil, fmt.Errorf("unrecognized option '%s'", a)
			}
			i++
			continue
		}
		for j := 1; j < len(a); j++ {
			c := a[j]
			switch c {
			case 'l':
				o.list = true
			case 'p':
				o.pipe = true
				o.quiet = true
			case 'q':
				o.quiet = true
			case 'o':
				o.overwrite = true
			case 'n':
				o.never = true
			case 'j':
				o.junk = true
			case 'd':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, errors.New("-d requires an argument")
				}
				o.dir = arg
				j = len(a)
			case 'h':
				o.printHelp = true
			default:
				return nil, fmt.Errorf("unknown option -%c", c)
			}
		}
		i++
	}
	return o, nil
}

func pickArg(rest string, i *int, args []string) (string, bool) {
	if rest != "" {
		return rest, true
	}
	if *i+1 >= len(args) {
		return "", false
	}
	*i++
	return args[*i], true
}

func printUnzipHelp(w io.Writer) {
	const help = `Usage: unzip [OPTION]... ARCHIVE [FILE]...
Extract files from a zip archive.

  -l            list archive contents
  -p            extract files to stdout (implies -q)
  -d DIR        extract into DIR
  -j            ignore stored directory paths
  -o            always overwrite without prompting
  -n            never overwrite
  -q            quiet
      --help    display this help
      --version display version
`
	io.WriteString(w, help)
}
