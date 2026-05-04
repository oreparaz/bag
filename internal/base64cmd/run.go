package base64cmd

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type options struct {
	file string

	decode         bool
	wrap           int
	wrapSet        bool
	ignoreGarbage  bool

	printHelp    bool
	printVersion bool
}

const defaultWrap = 76

func run(args []string) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "base64: %v\n", err)
		return 1
	}
	if o.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if o.printVersion {
		fmt.Println("base64 (bag) -- bag drop-in")
		return 0
	}
	if !o.wrapSet {
		o.wrap = defaultWrap
	}

	r, closer, err := openInput(o.file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "base64: %s: %v\n", displayName(o.file), err)
		return 1
	}
	defer closer()

	bw := bufio.NewWriter(os.Stdout)
	defer bw.Flush()

	if o.decode {
		return doDecode(bw, r, o.ignoreGarbage)
	}
	return doEncode(bw, r, o.wrap)
}

func doEncode(w *bufio.Writer, r io.Reader, wrap int) int {
	// Stream-encode in 3-byte chunks (which produce 4 base64 bytes) to keep
	// memory bounded.
	enc := base64.StdEncoding
	chunk := make([]byte, 3*4096)
	encoded := make([]byte, enc.EncodedLen(len(chunk)))

	col := 0
	flushOut := func(b []byte) error {
		if wrap == 0 {
			_, err := w.Write(b)
			return err
		}
		for len(b) > 0 {
			room := wrap - col
			if room <= 0 {
				if err := w.WriteByte('\n'); err != nil {
					return err
				}
				col = 0
				room = wrap
			}
			n := room
			if n > len(b) {
				n = len(b)
			}
			if _, err := w.Write(b[:n]); err != nil {
				return err
			}
			col += n
			b = b[n:]
		}
		return nil
	}

	for {
		n, err := io.ReadFull(r, chunk)
		switch {
		case err == nil:
			enc.Encode(encoded[:enc.EncodedLen(n)], chunk[:n])
			if err := flushOut(encoded[:enc.EncodedLen(n)]); err != nil {
				return 1
			}
		case errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF):
			if n > 0 {
				enc.Encode(encoded[:enc.EncodedLen(n)], chunk[:n])
				if err := flushOut(encoded[:enc.EncodedLen(n)]); err != nil {
					return 1
				}
			}
			// With wrap > 0, terminate the in-progress wrap line. With
			// wrap == 0, GNU base64 emits no trailing newline at all.
			if wrap > 0 && col != 0 {
				w.WriteByte('\n')
			}
			return 0
		default:
			fmt.Fprintf(os.Stderr, "base64: read: %v\n", err)
			return 1
		}
	}
}

func doDecode(w *bufio.Writer, r io.Reader, ignoreGarbage bool) int {
	br := bufio.NewReader(r)
	// Strip whitespace (and optionally garbage) into a clean byte stream
	// so the stdlib decoder can run.
	clean := &filterReader{r: br, ignoreGarbage: ignoreGarbage}
	dec := base64.NewDecoder(base64.StdEncoding, clean)
	if _, err := io.Copy(w, dec); err != nil {
		fmt.Fprintf(os.Stderr, "base64: invalid input\n")
		return 1
	}
	return 0
}

// filterReader strips characters from the input. Whitespace is always
// dropped (matches GNU base64). When ignoreGarbage is set, any byte
// outside the base64 alphabet is also dropped.
type filterReader struct {
	r             *bufio.Reader
	ignoreGarbage bool
}

func (f *filterReader) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		b, err := f.r.ReadByte()
		if err != nil {
			if n > 0 {
				return n, nil
			}
			return 0, err
		}
		if isWhitespace(b) {
			continue
		}
		if f.ignoreGarbage && !isBase64Byte(b) {
			continue
		}
		p[n] = b
		n++
	}
	return n, nil
}

func isWhitespace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

func isBase64Byte(b byte) bool {
	switch {
	case b >= 'A' && b <= 'Z':
		return true
	case b >= 'a' && b <= 'z':
		return true
	case b >= '0' && b <= '9':
		return true
	case b == '+' || b == '/' || b == '=':
		return true
	}
	return false
}

func openInput(name string) (io.Reader, func(), error) {
	if name == "" || name == "-" {
		return os.Stdin, func() {}, nil
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { f.Close() }, nil
}

func displayName(name string) string {
	if name == "" || name == "-" {
		return "standard input"
	}
	return name
}

func parseArgs(args []string) (*options, error) {
	o := &options{}
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "" || a == "-" {
			if o.file != "" {
				return nil, errors.New("extra operand")
			}
			o.file = a
			i++
			continue
		}
		if a[0] != '-' {
			if o.file != "" {
				return nil, errors.New("extra operand")
			}
			o.file = a
			i++
			continue
		}
		if a == "--" {
			if i+1 < len(args) {
				if i+2 < len(args) {
					return nil, errors.New("extra operand")
				}
				o.file = args[i+1]
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
		// Short cluster.
		j := 1
		for j < len(a) {
			c := a[j]
			switch c {
			case 'd':
				o.decode = true
				j++
			case 'i':
				o.ignoreGarbage = true
				j++
			case 'w':
				arg := ""
				if j+1 < len(a) {
					arg = a[j+1:]
				} else {
					if i+1 >= len(args) {
						return nil, errors.New("option -w requires an argument")
					}
					i++
					arg = args[i]
				}
				n, err := strconv.Atoi(arg)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("invalid wrap %q", arg)
				}
				o.wrap = n
				o.wrapSet = true
				j = len(a)
			case 'h':
				o.printHelp = true
				j++
			default:
				return nil, fmt.Errorf("unknown option -%c", c)
			}
		}
		i++
	}
	return o, nil
}

func applyLong(o *options, name string, next func() (string, error)) error {
	switch name {
	case "decode":
		o.decode = true
	case "ignore-garbage":
		o.ignoreGarbage = true
	case "wrap":
		v, err := next()
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid --wrap %q", v)
		}
		o.wrap = n
		o.wrapSet = true
	case "help":
		o.printHelp = true
	case "version":
		o.printVersion = true
	default:
		return fmt.Errorf("unrecognized option '--%s'", name)
	}
	return nil
}

func printHelp(w io.Writer) {
	const help = `Usage: base64 [OPTION]... [FILE]
Base64 encode or decode FILE, or standard input, to standard output.

With no FILE, or when FILE is -, read standard input.

  -d, --decode          decode data
  -i, --ignore-garbage  when decoding, ignore non-alphabet characters
  -w, --wrap=COLS       wrap encoded lines after COLS characters (default 76);
                        use 0 to disable line wrapping
      --help            display this help and exit
      --version         output version information and exit
`
	io.WriteString(w, help)
}
