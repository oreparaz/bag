package hexdump

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type format int

const (
	fmtDefault      format = iota // default 2-byte hex with single-space separator
	fmtCanonical                  // -C
	fmtOneByteOctal               // -b
	fmtOneByteChar                // -c
	fmtTwoByteDec                 // -d
	fmtTwoByteOctal               // -o
	fmtTwoByteHex                 // -x  (different padding from default!)
)

type options struct {
	files []string

	format  format
	limit   int64
	skip    int64
	verbose bool

	printHelp    bool
	printVersion bool
}

func run(args []string) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexdump: %v\n", err)
		return 1
	}
	if o.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if o.printVersion {
		fmt.Println("hexdump (bag) -- bag drop-in")
		return 0
	}

	files := o.files
	if len(files) == 0 {
		files = []string{"-"}
	}

	r, closer, err := openConcat(files)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexdump: %v\n", err)
		return 1
	}
	defer closer()

	if o.skip > 0 {
		if _, err := io.CopyN(io.Discard, r, o.skip); err != nil {
			if !errors.Is(err, io.EOF) {
				fmt.Fprintf(os.Stderr, "hexdump: %v\n", err)
				return 1
			}
			return 0
		}
	}

	bw := bufio.NewWriter(os.Stdout)
	defer bw.Flush()

	rowSize := 16
	switch o.format {
	case fmtCanonical:
		rowSize = 16
	case fmtTwoByteHex, fmtTwoByteOctal, fmtTwoByteDec:
		rowSize = 16
	case fmtOneByteOctal, fmtOneByteChar:
		rowSize = 16
	}

	src := r
	if o.limit >= 0 {
		src = io.LimitReader(r, o.limit)
	}
	if !o.verbose {
		// Wrap with a "collapse identical rows" emitter.
		return emitCollapsed(bw, src, o.skip, rowSize, o.format)
	}
	return emitVerbose(bw, src, o.skip, rowSize, o.format)
}

func emitCollapsed(w *bufio.Writer, r io.Reader, baseOffset int64, rowSize int, f format) int {
	var prev []byte
	starred := false
	offset := baseOffset
	buf := make([]byte, rowSize)
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			row := buf[:n]
			if prev != nil && bytesEqual(prev, row) && n == rowSize {
				if !starred {
					w.WriteString("*\n")
					starred = true
				}
			} else {
				w.WriteString(formatRow(offset, row, f))
				w.WriteByte('\n')
				starred = false
				prev = append(prev[:0], row...)
			}
			offset += int64(n)
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				fmt.Fprintf(w, finalOffset(f), offset)
				w.WriteByte('\n')
				return 0
			}
			fmt.Fprintf(os.Stderr, "hexdump: %v\n", err)
			return 1
		}
	}
}

func emitVerbose(w *bufio.Writer, r io.Reader, baseOffset int64, rowSize int, f format) int {
	offset := baseOffset
	buf := make([]byte, rowSize)
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			w.WriteString(formatRow(offset, buf[:n], f))
			w.WriteByte('\n')
			offset += int64(n)
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				fmt.Fprintf(w, finalOffset(f), offset)
				w.WriteByte('\n')
				return 0
			}
			fmt.Fprintf(os.Stderr, "hexdump: %v\n", err)
			return 1
		}
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func finalOffset(f format) string {
	if f == fmtCanonical {
		return "%08x"
	}
	return "%07x"
}

// formatRow renders one row in the chosen format. Length of row may be
// less than the row width on the final partial read; we pad to the full
// row width to match real hexdump's trailing whitespace.
func formatRow(offset int64, row []byte, f format) string {
	if f == fmtCanonical {
		return canonicalLine(offset, row)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%07x", offset)
	const rowSize = 16
	switch f {
	case fmtOneByteOctal:
		for i := 0; i < rowSize; i++ {
			if i < len(row) {
				fmt.Fprintf(&sb, " %03o", row[i])
			} else {
				sb.WriteString("    ")
			}
		}
	case fmtOneByteChar:
		for i := 0; i < rowSize; i++ {
			if i < len(row) {
				fmt.Fprintf(&sb, "%4s", charByte(row[i]))
			} else {
				sb.WriteString("    ")
			}
		}
	case fmtTwoByteOctal:
		each2Padded(&sb, row, "  %06o", "        ")
	case fmtTwoByteDec:
		each2Padded(&sb, row, "   %05d", "        ")
	case fmtTwoByteHex:
		each2Padded(&sb, row, "    %04x", "        ")
	default: // fmtDefault
		each2Padded(&sb, row, " %04x", "     ")
	}
	return sb.String()
}

func each2Padded(sb *strings.Builder, row []byte, fmtStr, pad string) {
	groups := 8 // 16 bytes / 2 = 8 groups
	for g := 0; g < groups; g++ {
		i := g * 2
		if i+1 < len(row) {
			// little-endian (matches real hexdump on x86 / ARM little-endian)
			v := uint16(row[i]) | uint16(row[i+1])<<8
			fmt.Fprintf(sb, fmtStr, v)
		} else if i < len(row) {
			v := uint16(row[i])
			fmt.Fprintf(sb, fmtStr, v)
		} else {
			sb.WriteString(pad)
		}
	}
}

func canonicalLine(offset int64, row []byte) string {
	var sb strings.Builder
	// Real hexdump: "00000000  68 65 ... 0a              |hello.|"
	// 8-char offset, 2 spaces, then 8 bytes hex (single-space separated),
	// 2 spaces (between halves), then 8 bytes hex, 2 spaces, ASCII.
	fmt.Fprintf(&sb, "%08x ", offset)
	for i := 0; i < 16; i++ {
		if i == 8 {
			sb.WriteByte(' ')
		}
		if i < len(row) {
			fmt.Fprintf(&sb, " %02x", row[i])
		} else {
			sb.WriteString("   ")
		}
	}
	sb.WriteString("  |")
	for _, b := range row {
		if b >= 0x20 && b < 0x7f {
			sb.WriteByte(b)
		} else {
			sb.WriteByte('.')
		}
	}
	sb.WriteString("|")
	return sb.String()
}

func charByte(b byte) string {
	switch b {
	case 0:
		return `\0`
	case '\b':
		return `\b`
	case '\t':
		return `\t`
	case '\n':
		return `\n`
	case '\r':
		return `\r`
	case '\f':
		return `\f`
	}
	if b >= 0x20 && b < 0x7f {
		return string([]byte{b})
	}
	return fmt.Sprintf("%03o", b)
}

// openConcat returns a single Reader that concatenates the given files.
func openConcat(names []string) (io.Reader, func(), error) {
	readers := make([]io.Reader, 0, len(names))
	closers := make([]func(), 0, len(names))
	for _, n := range names {
		if n == "-" {
			readers = append(readers, os.Stdin)
			continue
		}
		f, err := os.Open(n)
		if err != nil {
			for _, c := range closers {
				c()
			}
			return nil, nil, err
		}
		readers = append(readers, f)
		closers = append(closers, func() { f.Close() })
	}
	return io.MultiReader(readers...), func() {
		for _, c := range closers {
			c()
		}
	}, nil
}

func parseArgs(args []string) (*options, error) {
	o := &options{format: fmtDefault, limit: -1}
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "" || a[0] != '-' || a == "-" {
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
			case "canonical":
				o.format = fmtCanonical
			case "no-squeezing":
				o.verbose = true
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
		j := 1
		for j < len(a) {
			c := a[j]
			switch c {
			case 'C':
				o.format = fmtCanonical
				j++
			case 'b':
				o.format = fmtOneByteOctal
				j++
			case 'c':
				o.format = fmtOneByteChar
				j++
			case 'd':
				o.format = fmtTwoByteDec
				j++
			case 'o':
				o.format = fmtTwoByteOctal
				j++
			case 'x':
				o.format = fmtTwoByteHex
				j++
			case 'v':
				o.verbose = true
				j++
			case 'n':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, errors.New("-n requires an argument")
				}
				v, err := strconv.ParseInt(arg, 0, 64)
				if err != nil {
					return nil, err
				}
				o.limit = v
				j = len(a)
			case 's':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, errors.New("-s requires an argument")
				}
				v, err := strconv.ParseInt(arg, 0, 64)
				if err != nil {
					return nil, err
				}
				o.skip = v
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

func printHelp(w io.Writer) {
	const help = `Usage: hexdump [OPTION]... [FILE]...
Display file contents in hexadecimal, decimal, octal, or ASCII.

  -b   one-byte octal
  -c   one-byte char
  -d   two-byte decimal
  -o   two-byte octal
  -x   two-byte hex (default)
  -C   canonical hex+ASCII

  -n N stop after N input bytes
  -s N skip the first N bytes
  -v   do not squeeze identical adjacent rows
       --help
       --version
`
	io.WriteString(w, help)
}
