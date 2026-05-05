// Package cmpressor implements the shared CLI for the bag compression
// front-ends (gzip / bzip2 / xz / zstd, plus their *cat / un* aliases).
//
// All four tools share 90% of the surface: -d decompress, -c stdout,
// -k keep input, -f force, -v verbose, -1..-9 levels, "-" or no file
// for stdin. We parse the flags once, route through internal/compress
// for the actual codec, then handle the file plumbing uniformly.
//
// The dispatch table in main.go registers gzip / gunzip / zcat (and the
// equivalents for bzip2, xz, zstd) as separate Tool entries that each
// call MainAs with the appropriate name and default format. This way
// argv[0]-based symlink dispatch works transparently.
package cmpressor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/oreparaz/bag/internal/compress"
)

// Tool selects the format and the default mode based on the program name.
type Tool struct {
	// Name is what the user typed on the command line (without the bag-
	// prefix). E.g. "gzip", "gunzip", "zcat".
	Name string
	// Format is the compression format this binary handles.
	Format compress.Format
	// DefaultDecompress: the binary defaults to decompression (the un*
	// and *cat aliases).
	DefaultDecompress bool
	// AlwaysStdout: this binary always writes to stdout (the *cat aliases).
	AlwaysStdout bool
}

func (t Tool) defaultExt() string { return t.Format.Extension() }

// Main runs the tool. Returns the process exit code.
func Main(t Tool, args []string) int {
	o, err := parseArgs(t, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", t.Name, err)
		return 1
	}
	if o.printHelp {
		printHelp(os.Stdout, t)
		return 0
	}
	if o.printVersion {
		fmt.Printf("%s (bag) -- bag drop-in\n", t.Name)
		return 0
	}

	files := o.files
	if len(files) == 0 {
		files = []string{"-"}
	}

	exit := 0
	for _, f := range files {
		if err := process(t, o, f); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s: %v\n", t.Name, displayName(f), err)
			exit = 1
		}
	}
	return exit
}

type options struct {
	files []string

	decompress bool
	stdout     bool // -c
	keep       bool // -k
	force      bool // -f
	verbose    bool // -v
	level      int  // 0 = default

	test bool // -t (decompress, discard output, just check)

	printHelp    bool
	printVersion bool
}

func parseArgs(t Tool, args []string) (*options, error) {
	o := &options{
		decompress: t.DefaultDecompress,
		stdout:     t.AlwaysStdout,
	}
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "" || a == "-" || a[0] != '-' {
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
			switch a[2:] {
			case "decompress", "uncompress":
				o.decompress = true
			case "stdout", "to-stdout":
				o.stdout = true
			case "keep":
				o.keep = true
			case "force":
				o.force = true
			case "verbose":
				o.verbose = true
			case "test":
				o.test = true
				o.decompress = true
			case "fast":
				o.level = 1
			case "best":
				o.level = 9
			case "rm":
				// zstd-specific: like default (don't keep). We already remove
				// unless -k is set; accept for parity.
			case "help":
				o.printHelp = true
			case "version":
				o.printVersion = true
			case "no-name":
				// gzip-specific; we don't write the original name anyway
			default:
				return nil, fmt.Errorf("unrecognized option '%s'", a)
			}
			i++
			continue
		}
		// Short cluster: -d, -c, -k, -f, -v, -t, -1..-9, -h, -V
		for j := 1; j < len(a); j++ {
			c := a[j]
			switch {
			case c == 'd':
				o.decompress = true
			case c == 'c':
				o.stdout = true
			case c == 'k':
				o.keep = true
			case c == 'f':
				o.force = true
			case c == 'v':
				o.verbose = true
			case c == 't':
				o.test = true
				o.decompress = true
			case c == 'n':
				// gzip --no-name; no-op for us
			case c == 'N':
				// gzip --name; no-op for us (we never persist original name)
			case c == 'q':
				// quiet — accepted; we don't print anything informational
			case c >= '1' && c <= '9':
				o.level = int(c - '0')
			case c == 'h':
				o.printHelp = true
			case c == 'V':
				o.printVersion = true
			default:
				return nil, fmt.Errorf("unknown option -%c", c)
			}
		}
		i++
	}
	return o, nil
}

func process(t Tool, o *options, in string) error {
	if o.decompress {
		return doDecompress(t, o, in)
	}
	return doCompress(t, o, in)
}

func doCompress(t Tool, o *options, in string) error {
	src, srcCloser, err := openIn(in)
	if err != nil {
		return err
	}
	defer srcCloser()

	out, outCloser, outPath, err := openCompressOut(t, o, in)
	if err != nil {
		return err
	}
	// outCloser closes the output; we also need to close the compressor.
	w, err := compress.NewWriter(t.Format, out, o.level)
	if err != nil {
		outCloser()
		return err
	}
	if _, err := io.Copy(w, src); err != nil {
		w.Close()
		outCloser()
		return err
	}
	if err := w.Close(); err != nil {
		outCloser()
		return err
	}
	if err := outCloser(); err != nil {
		return err
	}

	if !o.stdout && !o.keep && in != "-" {
		// gzip-style: remove original after successful compression.
		_ = os.Remove(in)
	}
	if o.verbose && outPath != "" {
		fmt.Fprintf(os.Stderr, "%s -> %s\n", in, outPath)
	}
	return nil
}

func doDecompress(t Tool, o *options, in string) error {
	src, srcCloser, err := openIn(in)
	if err != nil {
		return err
	}
	defer srcCloser()

	r, err := compress.NewReader(t.Format, src)
	if err != nil {
		return err
	}
	defer r.Close()

	if o.test {
		_, err := io.Copy(io.Discard, r)
		return err
	}

	out, outCloser, outPath, err := openDecompressOut(t, o, in)
	if err != nil {
		return err
	}
	defer outCloser()
	if _, err := io.Copy(out, r); err != nil {
		return err
	}

	if !o.stdout && !o.keep && in != "-" {
		_ = os.Remove(in)
	}
	if o.verbose && outPath != "" {
		fmt.Fprintf(os.Stderr, "%s -> %s\n", in, outPath)
	}
	return nil
}

func openIn(name string) (io.Reader, func(), error) {
	if name == "-" {
		return os.Stdin, func() {}, nil
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { f.Close() }, nil
}

// openCompressOut returns the output for a compression run.
//
// Rules:
//   - stdout if -c, or input is stdin and -o not given.
//   - <name><ext> otherwise; refuse to overwrite without -f.
func openCompressOut(t Tool, o *options, in string) (io.Writer, func() error, string, error) {
	if o.stdout || in == "-" {
		return os.Stdout, func() error { return nil }, "", nil
	}
	out := in + t.defaultExt()
	if !o.force {
		if _, err := os.Stat(out); err == nil {
			return nil, nil, "", fmt.Errorf("%s already exists; use -f to force", out)
		}
	}
	f, err := os.Create(out)
	if err != nil {
		return nil, nil, "", err
	}
	return f, f.Close, out, nil
}

// openDecompressOut returns the output for a decompression run.
//
// Rules:
//   - stdout if -c, or input is stdin and -o not given.
//   - strip the format extension from the input name; refuse to overwrite
//     without -f.
//   - if there is no recognized extension, we fail (matches gzip).
func openDecompressOut(t Tool, o *options, in string) (io.Writer, func() error, string, error) {
	if o.stdout || in == "-" {
		return os.Stdout, func() error { return nil }, "", nil
	}
	out, err := stripExt(t, in)
	if err != nil {
		return nil, nil, "", err
	}
	if !o.force {
		if _, err := os.Stat(out); err == nil {
			return nil, nil, "", fmt.Errorf("%s already exists; use -f to force", out)
		}
	}
	f, err := os.Create(out)
	if err != nil {
		return nil, nil, "", err
	}
	return f, f.Close, out, nil
}

// stripExt removes the format-specific extension from a path. For .tgz,
// .tbz, .tbz2, .txz, .tzst we replace with .tar.
func stripExt(t Tool, path string) (string, error) {
	base := filepath.Base(path)
	dir := filepath.Dir(path)
	tarSwap := map[string]string{
		".tgz":  ".tar",
		".tbz":  ".tar",
		".tbz2": ".tar",
		".txz":  ".tar",
		".tzst": ".tar",
	}
	for k, v := range tarSwap {
		if strings.HasSuffix(base, k) {
			return filepath.Join(dir, strings.TrimSuffix(base, k)+v), nil
		}
	}
	if ext := t.defaultExt(); ext != "" && strings.HasSuffix(base, ext) {
		return filepath.Join(dir, strings.TrimSuffix(base, ext)), nil
	}
	return "", errors.New("input does not have a recognized extension; use -c to write to stdout")
}

func displayName(name string) string {
	if name == "-" {
		return "<stdin>"
	}
	return name
}

func printHelp(w io.Writer, t Tool) {
	fmt.Fprintf(w, "Usage: %s [OPTION]... [FILE]...\n", t.Name)
	fmt.Fprintln(w, "Compress or uncompress FILEs (by default, compress in-place).")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  -c, --stdout      write to standard output, keep originals")
	fmt.Fprintln(w, "  -d, --decompress  decompress")
	fmt.Fprintln(w, "  -f, --force       force, overwrite existing files")
	fmt.Fprintln(w, "  -k, --keep        keep input files")
	fmt.Fprintln(w, "  -t, --test        test compressed file integrity")
	fmt.Fprintln(w, "  -v, --verbose     verbose mode")
	fmt.Fprintln(w, "  -1 .. -9          compression level")
	fmt.Fprintln(w, "      --help        display this help and exit")
	fmt.Fprintln(w, "      --version     display version and exit")
	_ = strconv.Itoa // silence import (used by sister files in the future)
}
