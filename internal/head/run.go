package head

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
	files []string

	// mode
	byBytes  bool
	count    int64 // positive: first N; negative: all but the last (-N)
	countSet bool  // true once -n / -c parsed (so -n 0 differs from default)

	quiet   bool
	verbose bool

	printHelp    bool
	printVersion bool
}

const defaultLines int64 = 10

func run(args []string) int {
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "head: %v\n", err)
		return 1
	}
	if opts.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if opts.printVersion {
		fmt.Println("head (bag) -- bag drop-in")
		return 0
	}
	if !opts.countSet {
		opts.count = defaultLines
	}

	files := opts.files
	if len(files) == 0 {
		files = []string{"-"}
	}

	// Header policy: with multiple files, print headers unless -q.
	// With one file, never print headers unless -v.
	printHeaders := false
	switch {
	case opts.verbose:
		printHeaders = true
	case opts.quiet:
		printHeaders = false
	default:
		printHeaders = len(files) > 1
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	exit := 0
	for i, f := range files {
		if printHeaders {
			if i > 0 {
				out.WriteByte('\n')
			}
			fmt.Fprintf(out, "==> %s <==\n", displayName(f))
		}
		if err := emit(out, f, opts); err != nil {
			fmt.Fprintf(os.Stderr, "head: %s: %v\n", displayName(f), err)
			exit = 1
		}
	}
	return exit
}

// emit copies the head of name to w according to opts.
func emit(w *bufio.Writer, name string, o *options) error {
	r, closer, err := openInput(name)
	if err != nil {
		return err
	}
	defer closer()

	if o.byBytes {
		return emitBytes(w, r, o.count)
	}
	return emitLines(w, r, o.count)
}

func emitLines(w *bufio.Writer, r io.Reader, count int64) error {
	br := bufio.NewReader(r)
	// "-0" is encoded as int64Min and means "all but the last 0 lines"
	// — i.e., print everything.
	if count == int64Min {
		_, err := io.Copy(w, br)
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	if count >= 0 {
		// First N lines: count newlines.
		var n int64
		for n < count {
			line, err := br.ReadBytes('\n')
			if len(line) > 0 {
				w.Write(line)
				if line[len(line)-1] == '\n' {
					n++
				} else {
					return nil // EOF mid-line; we still printed what we have
				}
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
		}
		return nil
	}
	// All but the last (-count) lines: ring buffer of length |count|.
	want := -count
	ring := make([][]byte, 0, want)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			cp := make([]byte, len(line))
			copy(cp, line)
			ring = append(ring, cp)
			if int64(len(ring)) > want {
				w.Write(ring[0])
				ring = ring[1:]
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func emitBytes(w *bufio.Writer, r io.Reader, count int64) error {
	// "-0" — all but the last 0 bytes — means everything.
	if count == int64Min {
		_, err := io.Copy(w, r)
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	if count >= 0 {
		_, err := io.CopyN(w, r, count)
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	// All but last (-count) bytes. Stream through a ring buffer.
	want := int(-count)
	if want == 0 {
		_, err := io.Copy(w, r)
		return err
	}
	buf := make([]byte, 32*1024)
	pending := make([]byte, 0, want)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			if len(pending) > want {
				excess := len(pending) - want
				if _, werr := w.Write(pending[:excess]); werr != nil {
					return werr
				}
				pending = pending[excess:]
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func openInput(name string) (io.Reader, func(), error) {
	if name == "-" {
		return os.Stdin, func() {}, nil
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { f.Close() }, nil
}

func displayName(name string) string {
	if name == "-" {
		return "standard input"
	}
	return name
}

// parseArgs handles GNU-head argv. We don't accept the deprecated `head -10`
// (without -n) form, since clustered shorts like -nq overlap; users should
// use -n10 or -n 10.
func parseArgs(args []string) (*options, error) {
	o := &options{}
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "" || a == "-" {
			o.files = append(o.files, a)
			i++
			continue
		}
		if a[0] != '-' {
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
		// Short cluster. -n and -c take an arg (possibly glued).
		j := 1
		for j < len(a) {
			c := a[j]
			switch c {
			case 'n', 'c':
				arg := ""
				if j+1 < len(a) {
					arg = a[j+1:]
				} else {
					if i+1 >= len(args) {
						return nil, fmt.Errorf("option -%c requires an argument", c)
					}
					i++
					arg = args[i]
				}
				n, err := parseCount(arg)
				if err != nil {
					return nil, err
				}
				o.count = n
				o.byBytes = c == 'c'
				o.countSet = true
				j = len(a)
			case 'q':
				o.quiet = true
				j++
			case 'v':
				o.verbose = true
				j++
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
	case "lines":
		v, err := next()
		if err != nil {
			return err
		}
		n, err := parseCount(v)
		if err != nil {
			return err
		}
		o.count = n
		o.byBytes = false
		o.countSet = true
	case "bytes":
		v, err := next()
		if err != nil {
			return err
		}
		n, err := parseCount(v)
		if err != nil {
			return err
		}
		o.count = n
		o.byBytes = true
		o.countSet = true
	case "quiet", "silent":
		o.quiet = true
	case "verbose":
		o.verbose = true
	case "help":
		o.printHelp = true
	case "version":
		o.printVersion = true
	default:
		return fmt.Errorf("unrecognized option '--%s'", name)
	}
	return nil
}

// parseCount accepts integers with an optional unit suffix.
//
//	"10"       -> 10
//	"-10"      -> -10  (all but last 10)
//	"1K"       -> 1024
//	"2M"       -> 2 * 1024^2
//	"5b"       -> 5 * 512
func parseCount(s string) (int64, error) {
	if s == "" {
		return 0, errors.New("empty count")
	}
	// Detect a leading sign explicitly. ParseInt collapses "-0" to 0,
	// losing the user's intent ("all but last 0 lines" — which means
	// print everything — vs "first 0 lines" which means print nothing).
	negative := false
	if s[0] == '-' {
		negative = true
		s = s[1:]
		if s == "" {
			return 0, errors.New("empty count")
		}
	} else if s[0] == '+' {
		s = s[1:]
	}
	mult := int64(1)
	last := s[len(s)-1]
	switch last {
	case 'b':
		mult = 512
		s = s[:len(s)-1]
	case 'K', 'k':
		mult = 1 << 10
		s = s[:len(s)-1]
	case 'M', 'm':
		mult = 1 << 20
		s = s[:len(s)-1]
	case 'G', 'g':
		mult = 1 << 30
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseUint(s, 10, 63)
	if err != nil {
		return 0, fmt.Errorf("invalid count: %v", err)
	}
	// Range-check the multiplication so absurdly large input doesn't wrap.
	if mult != 1 && n > uint64(int64Max)/uint64(mult) {
		return 0, fmt.Errorf("count overflow: %s", s)
	}
	v := int64(n) * mult
	if negative {
		// Sentinel: encode "-0" as math.MinInt64 so the caller can tell
		// it apart from positive 0. emit functions check for negative
		// counts and clamp.
		if v == 0 {
			return int64Min, nil
		}
		v = -v
	}
	return v, nil
}

const (
	int64Max = int64(^uint64(0) >> 1) // math.MaxInt64
	int64Min = -int64Max - 1          // math.MinInt64
)

func printHelp(w io.Writer) {
	const help = `Usage: head [OPTION]... [FILE]...
Print the first 10 lines of each FILE to standard output.
With more than one FILE, precede each with a header giving the file name.

With no FILE, or when FILE is -, read standard input.

  -c, --bytes=[-]NUM       print the first NUM bytes; with leading '-',
                             print all but the last NUM bytes
  -n, --lines=[-]NUM       print the first NUM lines instead of the first 10;
                             with leading '-', print all but the last NUM
  -q, --quiet, --silent    never print headers giving file names
  -v, --verbose            always print headers giving file names
      --help               display this help and exit
      --version            output version information and exit
`
	io.WriteString(w, help)
}
