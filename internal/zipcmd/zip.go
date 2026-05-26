package zipcmd

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type zipOptions struct {
	output  string
	files   []string
	recurse bool
	junk    bool
	quiet   bool
	level   int

	printHelp    bool
	printVersion bool
}

func runZip(args []string) int {
	o, err := parseZipArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zip: %v\n", err)
		return 1
	}
	if o.printHelp {
		printZipHelp(os.Stdout)
		return 0
	}
	if o.printVersion {
		fmt.Println("zip (bag) -- bag drop-in")
		return 0
	}
	if o.output == "" {
		fmt.Fprintln(os.Stderr, "zip: missing output archive name")
		return 1
	}
	if len(o.files) == 0 {
		fmt.Fprintln(os.Stderr, "zip: nothing to do")
		return 1
	}

	out, err := openZipOutput(o.output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zip: %v\n", err)
		return 1
	}
	zw := zip.NewWriter(out.w)

	closeAll := func(prevErr error) error {
		err := prevErr
		// zip.Writer.Close() writes the central directory; failure here means
		// the archive is unreadable, so it must be surfaced.
		if cerr := zw.Close(); err == nil {
			err = cerr
		}
		if cerr := out.close(); err == nil {
			err = cerr
		}
		return err
	}

	for _, root := range o.files {
		if err := addPath(zw, root, o); err != nil {
			closeAll(err)
			fmt.Fprintf(os.Stderr, "zip: %v\n", err)
			return 1
		}
	}
	if err := closeAll(nil); err != nil {
		fmt.Fprintf(os.Stderr, "zip: %v\n", err)
		return 1
	}
	return 0
}

type zipOut struct {
	w     io.Writer
	close func() error
}

func openZipOutput(path string) (zipOut, error) {
	if path == "-" {
		return zipOut{w: os.Stdout, close: func() error { return nil }}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return zipOut{}, err
	}
	return zipOut{w: f, close: f.Close}, nil
}

func addPath(zw *zip.Writer, root string, o *zipOptions) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return addFile(zw, root, o)
	}
	if !o.recurse {
		// zip without -r adds the directory entry only (matches Info-ZIP).
		return addDirEntry(zw, root, o, info)
	}
	return filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return addDirEntry(zw, p, o, fi)
		}
		return addFile(zw, p, o)
	})
}

func addDirEntry(zw *zip.Writer, p string, o *zipOptions, info os.FileInfo) error {
	name := nameFor(p, o)
	if !strings.HasSuffix(name, "/") {
		name += "/"
	}
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = name
	hdr.Method = zip.Store
	_, err = zw.CreateHeader(hdr)
	if !o.quiet {
		fmt.Fprintf(os.Stderr, "  adding: %s\n", name)
	}
	return err
}

func addFile(zw *zip.Writer, p string, o *zipOptions) error {
	info, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// Store symlink content as a Symlink entry.
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = nameFor(p, o)
		hdr.Method = zip.Store
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		target, err := os.Readlink(p)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(target))
		return err
	}
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = nameFor(p, o)
	if info.Size() == 0 {
		hdr.Method = zip.Store
	} else {
		hdr.Method = zip.Deflate
	}
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(w, f); err != nil {
		return err
	}
	if !o.quiet {
		fmt.Fprintf(os.Stderr, "  adding: %s\n", hdr.Name)
	}
	return nil
}

func nameFor(p string, o *zipOptions) string {
	if o.junk {
		return filepath.Base(p)
	}
	return filepath.ToSlash(p)
}

func parseZipArgs(args []string) (*zipOptions, error) {
	o := &zipOptions{}
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "" || a[0] != '-' || a == "-" {
			if o.output == "" {
				o.output = a
			} else {
				o.files = append(o.files, a)
			}
			i++
			continue
		}
		if a == "--" {
			for _, f := range args[i+1:] {
				if o.output == "" {
					o.output = f
				} else {
					o.files = append(o.files, f)
				}
			}
			break
		}
		if strings.HasPrefix(a, "--") {
			switch a[2:] {
			case "recurse-paths":
				o.recurse = true
			case "junk-paths":
				o.junk = true
			case "quiet":
				o.quiet = true
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
			switch {
			case c == 'r' || c == 'R':
				o.recurse = true
			case c == 'j':
				o.junk = true
			case c == 'q':
				o.quiet = true
			case c >= '0' && c <= '9':
				o.level = int(c - '0')
			case c == 'h':
				o.printHelp = true
			default:
				return nil, fmt.Errorf("unknown option -%c", c)
			}
		}
		i++
	}
	return o, nil
}

func printZipHelp(w io.Writer) {
	const help = `Usage: zip [OPTION]... ARCHIVE FILE...
Create a zip archive containing FILE(s).

  -r, --recurse-paths   recurse directories
  -j, --junk-paths      don't preserve directory paths in entries
  -q, --quiet           quiet mode
  -0..-9                compression level (0=store, 9=best)
      --help            display this help
      --version         display version
`
	io.WriteString(w, help)
}

// errSentinel is a placeholder for callers wanting a typed sentinel later.
var errZipNotImpl = errors.New("not implemented")

var _ = errZipNotImpl
