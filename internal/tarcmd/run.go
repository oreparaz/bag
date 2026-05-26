package tarcmd

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/oreparaz/bag/internal/compress"
	"github.com/oreparaz/bag/internal/safefs"
)

type action int

const (
	actNone action = iota
	actCreate
	actExtract
	actList
)

type options struct {
	action       action
	file         string // empty or "-" means stdio
	verbose      bool
	chdir        string
	autoCompress bool

	format compress.Format

	excludes []string
	preserve bool
	deref    bool
	strip    int

	files []string

	printHelp    bool
	printVersion bool
}

func run(args []string) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tar: %v\n", err)
		return 2
	}
	if o.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if o.printVersion {
		fmt.Println("tar (bag) -- bag drop-in")
		return 0
	}
	if o.action == actNone {
		fmt.Fprintln(os.Stderr, "tar: You must specify one of '-Acdtrux', '--delete' or '--test-label' options")
		return 64
	}
	if o.autoCompress && o.format == compress.FormatNone && o.file != "" {
		o.format = compress.FormatFromExt(o.file)
	}

	if err := dispatch(o); err != nil {
		fmt.Fprintf(os.Stderr, "tar: %v\n", err)
		return 2
	}
	return 0
}

func dispatch(o *options) error {
	switch o.action {
	case actCreate:
		return doCreate(o)
	case actExtract:
		return doExtractOrList(o, false)
	case actList:
		return doExtractOrList(o, true)
	}
	return errors.New("no action")
}

func doCreate(o *options) error {
	if len(o.files) == 0 {
		return errors.New("no input files")
	}
	out, closer, err := openOut(o.file)
	if err != nil {
		return err
	}
	// On any error path we still try to close everything, but we report
	// the first error. On the success path we close in order — tar trailer,
	// compression trailer, output file — and surface any failure so a
	// truncated archive doesn't get reported as success (e.g. ENOSPC at
	// trailer-flush time).
	cw, err := compress.NewWriter(o.format, out, 0)
	if err != nil {
		closer()
		return err
	}
	tw := tar.NewWriter(cw)

	closeAll := func(prevErr error) error {
		err := prevErr
		if cerr := tw.Close(); err == nil {
			err = cerr
		}
		if cerr := cw.Close(); err == nil {
			err = cerr
		}
		if cerr := closer(); err == nil {
			err = cerr
		}
		return err
	}

	if o.chdir != "" {
		old, err := os.Getwd()
		if err != nil {
			return closeAll(err)
		}
		if err := os.Chdir(o.chdir); err != nil {
			return closeAll(err)
		}
		defer os.Chdir(old)
	}

	for _, root := range o.files {
		if err := walkAndAdd(tw, root, o); err != nil {
			return closeAll(err)
		}
	}
	return closeAll(nil)
}

func walkAndAdd(tw *tar.Writer, root string, o *options) error {
	statFn := os.Lstat
	if o.deref {
		statFn = os.Stat
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if isExcluded(path, o.excludes) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if o.deref {
			info, err = statFn(path)
			if err != nil {
				return err
			}
		}
		var link string
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		// Preserve relative path layout. tar's convention is forward slashes.
		hdr.Name = filepath.ToSlash(path)
		if info.IsDir() && !strings.HasSuffix(hdr.Name, "/") {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, f); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
		if o.verbose {
			fmt.Fprintln(os.Stderr, hdr.Name)
		}
		return nil
	})
}

func doExtractOrList(o *options, listOnly bool) error {
	in, closer, err := openIn(o.file)
	if err != nil {
		return err
	}
	defer closer()

	src, err := wrapDecompress(in, o)
	if err != nil {
		return err
	}
	if rc, ok := src.(io.Closer); ok {
		defer rc.Close()
	}

	if o.chdir != "" {
		old, err := os.Getwd()
		if err != nil {
			return err
		}
		if err := os.Chdir(o.chdir); err != nil {
			return err
		}
		defer os.Chdir(old)
	}

	tr := tar.NewReader(src)
	for {
		hdr, err := tr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		name := stripComponents(hdr.Name, o.strip)
		if name == "" {
			continue
		}
		if isExcluded(name, o.excludes) {
			continue
		}
		if listOnly {
			if o.verbose {
				fmt.Println(verboseLine(hdr, name))
			} else {
				fmt.Println(name)
			}
			continue
		}
		if err := extractEntry(tr, hdr, name, o); err != nil {
			return err
		}
		if o.verbose {
			fmt.Fprintln(os.Stderr, name)
		}
	}
}

func extractEntry(tr *tar.Reader, hdr *tar.Header, name string, o *options) error {
	// Refuse absolute or .. components — security: tar entries from untrusted
	// archives must not write outside the cwd.
	if err := safefs.RefusePathTraversal(name); err != nil {
		return fmt.Errorf("refusing extraction outside output dir: %q", hdr.Name)
	}
	// The extraction root is cwd (-C dir was already applied via Chdir).
	// We use "." rather than the absolute cwd so safefs's intermediate
	// symlink check stays inside the user-chosen tree — components above
	// cwd (e.g. /var on macOS, which is itself a system symlink) are
	// outside the attacker's control and shouldn't fail the check.
	const root = "."
	// Refuse to write through any pre-existing symlink inside the
	// extraction tree — closes the "extract symlink dir, then file
	// through it" attack.
	if err := safefs.EnsureNoSymlinkInPath(root, name); err != nil {
		return err
	}
	switch hdr.Typeflag {
	case tar.TypeDir:
		return safefs.MkdirAllNoSymlinkLeaf(root, name, hdr.FileInfo().Mode().Perm())
	case tar.TypeReg, tar.TypeRegA:
		if err := safefs.MkdirAllNoSymlinkLeaf(root, filepath.Dir(name), 0o755); err != nil {
			return err
		}
		// O_NOFOLLOW: refuse to overwrite a symlink leaf. O_TRUNC: allow
		// overwriting an existing regular file (matches real tar default).
		f, err := safefs.CreateTrunc(name, hdr.FileInfo().Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return err
		}
		// CreateTrunc's mode argument is masked by the process umask. With
		// -p the user wants the recorded permissions verbatim, so chmod
		// after creation to bypass umask.
		if o.preserve {
			if err := f.Chmod(hdr.FileInfo().Mode().Perm()); err != nil {
				f.Close()
				return err
			}
		}
		f.Close()
		if o.preserve {
			return os.Chtimes(name, time.Now(), hdr.ModTime)
		}
		return nil
	case tar.TypeSymlink:
		if err := safefs.MkdirAllNoSymlinkLeaf(root, filepath.Dir(name), 0o755); err != nil {
			return err
		}
		// We don't validate hdr.Linkname here — symlink targets can legally
		// be absolute or contain ".." (Linux allows it). The danger is
		// only when we'd write *through* the symlink, which is blocked by
		// EnsureNoSymlinkInPath when subsequent entries arrive.
		_ = os.Remove(name)
		return os.Symlink(hdr.Linkname, name)
	case tar.TypeLink:
		// Hardlinks DO need their target validated: os.Link follows the
		// target literally (relative to the cwd, since hardlinks don't
		// "have" a path-resolution layer). A malicious archive with
		// linkname='../../etc/passwd' would otherwise hard-link our
		// extraction into the system's password file.
		linkTarget := stripComponents(hdr.Linkname, o.strip)
		if err := safefs.RefusePathTraversal(linkTarget); err != nil {
			return fmt.Errorf("refusing hardlink to unsafe target %q", hdr.Linkname)
		}
		if err := safefs.MkdirAllNoSymlinkLeaf(root, filepath.Dir(name), 0o755); err != nil {
			return err
		}
		_ = os.Remove(name)
		return os.Link(linkTarget, name)
	default:
		// Skip unsupported types: char/block devices, FIFOs, etc.
		return nil
	}
}

func stripComponents(name string, n int) string {
	if n <= 0 {
		return name
	}
	parts := strings.Split(name, "/")
	if len(parts) <= n {
		return ""
	}
	return strings.Join(parts[n:], "/")
}

func wrapDecompress(in io.Reader, o *options) (io.Reader, error) {
	if o.format == compress.FormatNone {
		// Auto-detect from magic bytes if not specified.
		f, wrapped, err := compress.FormatFromMagic(in)
		if err != nil {
			return nil, err
		}
		o.format = f
		in = wrapped
	}
	if o.format == compress.FormatNone {
		return in, nil
	}
	return compress.NewReader(o.format, in)
}

func openIn(name string) (io.Reader, func(), error) {
	if name == "" || name == "-" {
		return os.Stdin, func() {}, nil
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { f.Close() }, nil
}

func openOut(name string) (io.Writer, func() error, error) {
	if name == "" || name == "-" {
		return os.Stdout, func() error { return nil }, nil
	}
	f, err := os.Create(name)
	if err != nil {
		return nil, nil, err
	}
	return f, f.Close, nil
}

// isExcluded reports whether name matches any exclude pattern. GNU tar
// applies an exclude pattern to the full member name AND to every suffix
// starting after each '/'. So `--exclude=*.tmp` against `src/skip.tmp`
// matches via the post-slash suffix `skip.tmp`.
func isExcluded(name string, patterns []string) bool {
	slashName := filepath.ToSlash(name)
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, slashName); ok {
			return true
		}
		// Try every '/'-suffix.
		s := slashName
		for {
			i := strings.IndexByte(s, '/')
			if i < 0 {
				break
			}
			s = s[i+1:]
			if s == "" {
				break
			}
			if ok, _ := filepath.Match(p, s); ok {
				return true
			}
		}
	}
	return false
}

// verboseLine renders a -tv "ls -l"-ish line for one entry.
func verboseLine(hdr *tar.Header, name string) string {
	mode := hdr.FileInfo().Mode()
	return fmt.Sprintf("%s %d/%d %d %s %s",
		mode.String(),
		hdr.Uid, hdr.Gid,
		hdr.Size,
		hdr.ModTime.UTC().Format("2006-01-02 15:04"),
		name,
	)
}

func parseArgs(args []string) (*options, error) {
	o := &options{}
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "" {
			i++
			continue
		}
		if a == "-" {
			o.files = append(o.files, a)
			i++
			continue
		}
		if a[0] != '-' {
			// Heuristic: tar's first non-flag positional in BSD form is the
			// "key": e.g. `tar xvf foo.tar`. We don't accept that form to
			// avoid ambiguity with input files.
			o.files = append(o.files, a)
			i++
			continue
		}
		if a == "--" {
			for _, f := range args[i+1:] {
				o.files = append(o.files, f)
			}
			break
		}
		if strings.HasPrefix(a, "--") {
			name := a[2:]
			val := ""
			hasEq := false
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				val = name[eq+1:]
				name = name[:eq]
				hasEq = true
			}
			next := func() (string, error) {
				if hasEq {
					return val, nil
				}
				if i+1 >= len(args) {
					return "", fmt.Errorf("option --%s requires an argument", name)
				}
				i++
				return args[i], nil
			}
			if err := applyLong(o, name, next); err != nil {
				return nil, err
			}
			i++
			continue
		}
		// Short cluster.  -C, -f, --strip-components take an arg; the
		// tar convention allows the arg in the next argv after the cluster.
		// We support -fNAME glued, -f NAME unglued, and clusters like -czvf.
		j := 1
		for j < len(a) {
			c := a[j]
			switch c {
			case 'c':
				o.action = actCreate
				j++
			case 'x':
				o.action = actExtract
				j++
			case 't':
				o.action = actList
				j++
			case 'v':
				o.verbose = true
				j++
			case 'z':
				o.format = compress.FormatGzip
				j++
			case 'j':
				o.format = compress.FormatBzip2
				j++
			case 'J':
				o.format = compress.FormatXZ
				j++
			case 'a':
				o.autoCompress = true
				j++
			case 'p':
				o.preserve = true
				j++
			case 'h':
				o.deref = true
				j++
			case 'f':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, errors.New("-f requires an argument")
				}
				o.file = arg
				j = len(a)
			case 'C':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, errors.New("-C requires an argument")
				}
				o.chdir = arg
				j = len(a)
			default:
				return nil, fmt.Errorf("unknown option -%c", c)
			}
		}
		i++
	}
	return o, nil
}

// pickArg returns the rest of the cluster as the arg (e.g. "-fNAME") or the
// next argv element (e.g. "-f NAME"). Increments i in the latter case.
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

func applyLong(o *options, name string, next func() (string, error)) error {
	switch name {
	case "create":
		o.action = actCreate
	case "extract", "get":
		o.action = actExtract
	case "list":
		o.action = actList
	case "verbose":
		o.verbose = true
	case "file":
		v, err := next()
		if err != nil {
			return err
		}
		o.file = v
	case "directory":
		v, err := next()
		if err != nil {
			return err
		}
		o.chdir = v
	case "gzip":
		o.format = compress.FormatGzip
	case "bzip2":
		o.format = compress.FormatBzip2
	case "xz":
		o.format = compress.FormatXZ
	case "zstd":
		o.format = compress.FormatZstd
	case "auto-compress":
		o.autoCompress = true
	case "exclude":
		v, err := next()
		if err != nil {
			return err
		}
		o.excludes = append(o.excludes, v)
	case "preserve-permissions":
		o.preserve = true
	case "dereference":
		o.deref = true
	case "strip-components":
		v, err := next()
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid --strip-components: %v", err)
		}
		o.strip = n
	case "help":
		o.printHelp = true
	case "version":
		o.printVersion = true
	default:
		return fmt.Errorf("unrecognized option --%s", name)
	}
	return nil
}

func printHelp(w io.Writer) {
	const help = `Usage: tar [OPTION...] [FILE]...
Examples:
  tar -cf archive.tar foo bar    create archive.tar from foo and bar
  tar -tvf archive.tar           list contents verbosely
  tar -xf archive.tar            extract files

Action selectors:
  -c, --create
  -x, --extract, --get
  -t, --list

Common options:
  -f, --file=ARCHIVE         archive file ("-" for stdin/stdout)
  -v, --verbose
  -C, --directory=DIR        chdir before action
  -z, --gzip
  -j, --bzip2
  -J, --xz
      --zstd
  -a, --auto-compress        infer compression from -f extension
      --exclude=PATTERN
  -p, --preserve-permissions
  -h, --dereference
      --strip-components=N
      --help
      --version
`
	io.WriteString(w, help)
}
