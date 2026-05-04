package xxd

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
	file string

	cols      int
	groupSize int
	plain     bool
	revert    bool
	skip      int64
	limit     int64
	limitSet  bool
	upper     bool

	printHelp    bool
	printVersion bool
}

const defaultCols = 16
const defaultGroup = 2

func run(args []string) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "xxd: %v\n", err)
		return 1
	}
	if o.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if o.printVersion {
		fmt.Println("xxd (bag) -- bag drop-in")
		return 0
	}
	if o.cols == 0 {
		o.cols = defaultCols
	}
	if o.groupSize == 0 {
		o.groupSize = defaultGroup
	}

	r, closer, err := openInput(o.file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "xxd: %s: %v\n", displayName(o.file), err)
		return 1
	}
	defer closer()

	bw := bufio.NewWriter(os.Stdout)
	defer bw.Flush()

	if o.revert {
		return doRevert(bw, r, o.plain)
	}

	if o.skip > 0 {
		if _, err := io.CopyN(io.Discard, r, o.skip); err != nil {
			if !errors.Is(err, io.EOF) {
				fmt.Fprintf(os.Stderr, "xxd: %v\n", err)
				return 1
			}
			return 0
		}
	}

	var src io.Reader = r
	if o.limitSet {
		src = io.LimitReader(r, o.limit)
	}

	if o.plain {
		return doPlain(bw, src, o.cols, o.upper)
	}
	return doDump(bw, src, o.cols, o.groupSize, o.skip, o.upper)
}

func doDump(w *bufio.Writer, r io.Reader, cols, group int, baseOffset int64, upper bool) int {
	hexFmt := "%02x"
	if upper {
		hexFmt = "%02X"
	}
	buf := make([]byte, cols)
	offset := baseOffset
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			fmt.Fprintf(w, "%08x:", offset)
			// Hex columns
			width := 0 // chars written for hex part (after ": ")
			for i := 0; i < cols; i++ {
				if i%group == 0 {
					w.WriteByte(' ')
					width++
				}
				if i < n {
					fmt.Fprintf(w, hexFmt, buf[i])
				} else {
					w.WriteString("  ")
				}
				width += 2
			}
			// Pad to a fixed column for the ASCII section.
			fullWidth := cols*2 + (cols+group-1)/group
			for width < fullWidth {
				w.WriteByte(' ')
				width++
			}
			w.WriteString("  ")
			for i := 0; i < n; i++ {
				w.WriteByte(printable(buf[i]))
			}
			w.WriteByte('\n')
			offset += int64(n)
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return 0
			}
			fmt.Fprintf(os.Stderr, "xxd: %v\n", err)
			return 1
		}
	}
}

func doPlain(w *bufio.Writer, r io.Reader, cols int, upper bool) int {
	hexFmt := "%02x"
	if upper {
		hexFmt = "%02X"
	}
	col := 0
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			for i := 0; i < n; i++ {
				if col == cols {
					w.WriteByte('\n')
					col = 0
				}
				fmt.Fprintf(w, hexFmt, buf[i])
				col++
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if col > 0 {
					w.WriteByte('\n')
				}
				return 0
			}
			fmt.Fprintf(os.Stderr, "xxd: %v\n", err)
			return 1
		}
	}
}

func doRevert(w *bufio.Writer, r io.Reader, plain bool) int {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			out := decodeRevertLine(line, plain)
			if _, werr := w.Write(out); werr != nil {
				fmt.Fprintf(os.Stderr, "xxd: %v\n", werr)
				return 1
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0
			}
			fmt.Fprintf(os.Stderr, "xxd: %v\n", err)
			return 1
		}
	}
}

// decodeRevertLine pulls bytes out of a single xxd output line.
//
// In the default dump, each row looks like:
//
//	"00000000: 6865 6c6c 6f0a                           hello."
//
// We strip the leading "<hex>:" offset and stop at "  " (the gap before
// the ASCII column). In plain mode we just decode the line as hex.
func decodeRevertLine(line []byte, plain bool) []byte {
	// Drop trailing newline.
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) == 0 {
		return nil
	}
	if !plain {
		// Drop offset prefix "xxxx:" if present.
		if i := indexByte(line, ':'); i > 0 {
			line = line[i+1:]
		}
		// Stop at first "  " (ASCII column gap), if any.
		if i := indexDouble(line); i >= 0 {
			line = line[:i]
		}
	}
	out := make([]byte, 0, len(line)/2)
	hi := -1
	for _, b := range line {
		v, ok := hexVal(b)
		if !ok {
			hi = -1
			continue
		}
		if hi < 0 {
			hi = int(v)
		} else {
			out = append(out, byte((hi<<4)|int(v)))
			hi = -1
		}
	}
	return out
}

func indexByte(b []byte, c byte) int {
	for i := 0; i < len(b); i++ {
		if b[i] == c {
			return i
		}
	}
	return -1
}

func indexDouble(b []byte) int {
	for i := 0; i+1 < len(b); i++ {
		if b[i] == ' ' && b[i+1] == ' ' {
			return i
		}
	}
	return -1
}

func hexVal(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return 10 + b - 'a', true
	case b >= 'A' && b <= 'F':
		return 10 + b - 'A', true
	}
	return 0, false
}

func printable(b byte) byte {
	if b < 0x20 || b > 0x7e {
		return '.'
	}
	return b
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
		// xxd doesn't really do '--name', but accept '--' end-of-options.
		if a == "--" {
			if i+1 < len(args) {
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
		j := 1
		for j < len(a) {
			c := a[j]
			switch c {
			case 'p':
				o.plain = true
				j++
			case 'r':
				o.revert = true
				j++
			case 'u':
				o.upper = true
				j++
			case 'c', 'g', 's', 'l':
				arg := ""
				if j+1 < len(a) {
					arg = a[j+1:]
				} else {
					if i+1 >= len(args) {
						return nil, fmt.Errorf("-%c requires an argument", c)
					}
					i++
					arg = args[i]
				}
				if err := setCount(o, c, arg); err != nil {
					return nil, err
				}
				j = len(a)
			case 'h':
				o.printHelp = true
				j++
			case 'v':
				o.printVersion = true
				j++
			default:
				return nil, fmt.Errorf("unknown option -%c", c)
			}
		}
		i++
	}
	return o, nil
}

func setCount(o *options, c byte, s string) error {
	n, err := strconv.ParseInt(s, 0, 64)
	if err != nil {
		return fmt.Errorf("invalid -%c value %q", c, s)
	}
	switch c {
	case 'c':
		if n < 0 || n > 256 {
			return fmt.Errorf("invalid -c %d (must be 0..256)", n)
		}
		o.cols = int(n)
	case 'g':
		if n < 0 {
			return fmt.Errorf("invalid -g %d", n)
		}
		o.groupSize = int(n)
	case 's':
		if n < 0 {
			return errors.New("negative seek not supported (see FUTURE.md)")
		}
		o.skip = n
	case 'l':
		o.limit = n
		o.limitSet = true
	}
	return nil
}

func applyLong(o *options, name string, next func() (string, error)) error {
	switch name {
	case "cols":
		v, err := next()
		if err != nil {
			return err
		}
		return setCount(o, 'c', v)
	case "groupsize":
		v, err := next()
		if err != nil {
			return err
		}
		return setCount(o, 'g', v)
	case "len":
		v, err := next()
		if err != nil {
			return err
		}
		return setCount(o, 'l', v)
	case "seek":
		v, err := next()
		if err != nil {
			return err
		}
		return setCount(o, 's', v)
	case "plain":
		o.plain = true
	case "revert":
		o.revert = true
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
	const help = `Usage: xxd [options] [infile]
       xxd -r [-p] [infile]

  -c, --cols=N        format <N> octets per line. Default 16
  -g, --groupsize=N   number of octets per group in normal output. Default 2
  -p, --plain         plain hex dump (no offset, no ASCII)
  -r, --revert        reverse: hex dump to binary
  -s, --seek=OFF      skip OFF bytes at start of input
  -l, --len=N         stop after writing <N> bytes
  -u                  use uppercase hex letters
  -h, --help          show this help
      --version       print version
`
	io.WriteString(w, help)
}
