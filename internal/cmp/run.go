package cmp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type options struct {
	silent     bool
	verbose    bool
	printBytes bool
	maxBytes   int64
	skip       int64
	files      []string
}

func parseArgs(argv []string) (*options, error) {
	o := &options{}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			o.files = append(o.files, argv[i+1:]...)
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
			switch name {
			case "silent", "quiet":
				o.silent = true
			case "verbose":
				o.verbose = true
			case "print-bytes":
				o.printBytes = true
			case "bytes":
				if !hasEq {
					if i+1 >= len(argv) {
						return nil, errors.New("--bytes requires an argument")
					}
					i++
					val = argv[i]
				}
				n, err := strconv.ParseInt(val, 10, 64)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("invalid --bytes: %q", val)
				}
				o.maxBytes = n
			case "ignore-initial":
				if !hasEq {
					if i+1 >= len(argv) {
						return nil, errors.New("--ignore-initial requires an argument")
					}
					i++
					val = argv[i]
				}
				n, err := strconv.ParseInt(val, 10, 64)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("invalid --ignore-initial: %q", val)
				}
				o.skip = n
			default:
				// Ignore unknown long flags.
			}
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" && len(a) > 1 && isFlagCluster(a) {
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 's':
					o.silent = true
				case 'l':
					o.verbose = true
				case 'b':
					o.printBytes = true
				case 'n':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-n requires an argument")
						}
						i++
						val = argv[i]
					}
					n, err := strconv.ParseInt(val, 10, 64)
					if err != nil {
						return nil, fmt.Errorf("invalid -n: %q", val)
					}
					o.maxBytes = n
				case 'i':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-i requires an argument")
						}
						i++
						val = argv[i]
					}
					n, err := strconv.ParseInt(val, 10, 64)
					if err != nil {
						return nil, fmt.Errorf("invalid -i: %q", val)
					}
					o.skip = n
				default:
					// Ignore unknown short flags.
				}
			}
			continue
		}
		o.files = append(o.files, a)
	}
	if len(o.files) < 2 {
		return nil, errors.New("missing operand: cmp FILE1 FILE2")
	}
	return o, nil
}

func isFlagCluster(s string) bool {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case 's', 'l', 'b', 'n', 'i':
		default:
			return false
		}
	}
	return true
}

func run(argv []string) int {
	o, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmp: %v\n", err)
		return 2
	}
	a, err := openOrStdin(o.files[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmp: %v\n", err)
		return 2
	}
	defer a.Close()
	b, err := openOrStdin(o.files[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmp: %v\n", err)
		return 2
	}
	defer b.Close()

	br := bufio.NewReader(a)
	cr := bufio.NewReader(b)
	for n := int64(0); n < o.skip; n++ {
		_, e1 := br.ReadByte()
		_, e2 := cr.ReadByte()
		if e1 != nil || e2 != nil {
			break
		}
	}

	var byteN, lineN int64 = 1, 1
	differ := false
	for {
		if o.maxBytes > 0 && byteN > o.maxBytes {
			break
		}
		bA, errA := br.ReadByte()
		bB, errB := cr.ReadByte()
		if errA == io.EOF && errB == io.EOF {
			break
		}
		if errA == io.EOF {
			if !o.silent {
				fmt.Fprintf(os.Stderr, "cmp: EOF on %s after byte %d\n", o.files[0], byteN-1)
			}
			return 1
		}
		if errB == io.EOF {
			if !o.silent {
				fmt.Fprintf(os.Stderr, "cmp: EOF on %s after byte %d\n", o.files[1], byteN-1)
			}
			return 1
		}
		if bA != bB {
			differ = true
			if o.silent {
				return 1
			}
			if o.verbose {
				if o.printBytes {
					fmt.Fprintf(os.Stdout, "%d %o %s %o %s\n",
						byteN, bA, octalEscape(bA), bB, octalEscape(bB))
				} else {
					fmt.Fprintf(os.Stdout, "%d %o %o\n", byteN, bA, bB)
				}
				// Continue scanning all differences with -l.
			} else {
				if o.printBytes {
					fmt.Fprintf(os.Stdout, "%s %s differ: byte %d, line %d is %o %s %o %s\n",
						o.files[0], o.files[1], byteN, lineN, bA, octalEscape(bA), bB, octalEscape(bB))
				} else {
					fmt.Fprintf(os.Stdout, "%s %s differ: byte %d, line %d\n",
						o.files[0], o.files[1], byteN, lineN)
				}
				return 1
			}
		}
		if bA == '\n' {
			lineN++
		}
		byteN++
	}
	if differ {
		return 1
	}
	return 0
}

func openOrStdin(p string) (io.ReadCloser, error) {
	if p == "-" {
		return io.NopCloser(os.Stdin), nil
	}
	return os.Open(p)
}

// octalEscape renders a byte using gnu cmp's printed-byte form
// (printable char as-is; control bytes as e.g. M-^@ — we keep it
// simple and use the c-style \\OOO octal escape).
func octalEscape(b byte) string {
	if b >= 0x20 && b < 0x7f {
		return string(b)
	}
	return fmt.Sprintf("\\%03o", b)
}
