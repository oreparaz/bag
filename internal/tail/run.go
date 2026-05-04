package tail

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

	byBytes  bool
	count    int64 // see fromStart
	fromStart bool // count from beginning ("+N" form)
	countSet bool

	quiet   bool
	verbose bool

	printHelp    bool
	printVersion bool
}

const defaultLines int64 = 10

func run(args []string) int {
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tail: %v\n", err)
		return 1
	}
	if opts.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if opts.printVersion {
		fmt.Println("tail (bag) -- bag drop-in")
		return 0
	}
	if !opts.countSet {
		opts.count = defaultLines
	}

	files := opts.files
	if len(files) == 0 {
		files = []string{"-"}
	}

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
			fmt.Fprintf(os.Stderr, "tail: %s: %v\n", displayName(f), err)
			exit = 1
		}
	}
	return exit
}

func emit(w *bufio.Writer, name string, o *options) error {
	r, closer, err := openInput(name)
	if err != nil {
		return err
	}
	defer closer()

	if o.byBytes {
		if o.fromStart {
			return emitFromByte(w, r, o.count)
		}
		return emitLastBytes(w, r, o.count)
	}
	if o.fromStart {
		return emitFromLine(w, r, o.count)
	}
	return emitLastLines(w, r, o.count)
}

func emitLastLines(w *bufio.Writer, r io.Reader, count int64) error {
	if count <= 0 {
		// tail -n 0 prints nothing.
		_, _ = io.Copy(io.Discard, r)
		return nil
	}
	br := bufio.NewReader(r)
	ring := make([][]byte, 0, count)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			cp := make([]byte, len(line))
			copy(cp, line)
			if int64(len(ring)) == count {
				ring = ring[1:]
			}
			ring = append(ring, cp)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}
			break
		}
	}
	for _, l := range ring {
		w.Write(l)
	}
	return nil
}

func emitFromLine(w *bufio.Writer, r io.Reader, n int64) error {
	if n < 1 {
		n = 1
	}
	br := bufio.NewReader(r)
	var line int64 = 0
	for {
		buf, err := br.ReadBytes('\n')
		if len(buf) > 0 {
			line++
			if line >= n {
				w.Write(buf)
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

func emitLastBytes(w *bufio.Writer, r io.Reader, count int64) error {
	if count <= 0 {
		_, _ = io.Copy(io.Discard, r)
		return nil
	}
	// Stream into a ring buffer of size count. Memory use is bounded by
	// the user's request, not file size.
	want := int(count)
	pending := make([]byte, 0, want)
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			if len(pending) > want {
				pending = pending[len(pending)-want:]
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}
			break
		}
	}
	_, err := w.Write(pending)
	return err
}

func emitFromByte(w *bufio.Writer, r io.Reader, n int64) error {
	if n < 1 {
		n = 1
	}
	skip := n - 1
	if skip > 0 {
		if _, err := io.CopyN(io.Discard, r, skip); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
	_, err := io.Copy(w, r)
	return err
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
				n, fromStart, err := parseTailCount(arg)
				if err != nil {
					return nil, err
				}
				o.count = n
				o.fromStart = fromStart
				o.byBytes = c == 'c'
				o.countSet = true
				j = len(a)
			case 'q':
				o.quiet = true
				j++
			case 'v':
				o.verbose = true
				j++
			case 'f':
				return nil, errors.New("-f follow mode not yet supported")
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
		n, fs, err := parseTailCount(v)
		if err != nil {
			return err
		}
		o.count = n
		o.fromStart = fs
		o.byBytes = false
		o.countSet = true
	case "bytes":
		v, err := next()
		if err != nil {
			return err
		}
		n, fs, err := parseTailCount(v)
		if err != nil {
			return err
		}
		o.count = n
		o.fromStart = fs
		o.byBytes = true
		o.countSet = true
	case "quiet", "silent":
		o.quiet = true
	case "verbose":
		o.verbose = true
	case "follow":
		return errors.New("--follow not yet supported")
	case "help":
		o.printHelp = true
	case "version":
		o.printVersion = true
	default:
		return fmt.Errorf("unrecognized option '--%s'", name)
	}
	return nil
}

// parseTailCount understands "+N" (start from N) and "[-]N" (last N).
func parseTailCount(s string) (n int64, fromStart bool, err error) {
	if s == "" {
		return 0, false, errors.New("empty count")
	}
	if s[0] == '+' {
		fromStart = true
		s = s[1:]
	}
	mult := int64(1)
	if len(s) > 0 {
		switch s[len(s)-1] {
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
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("invalid count: %v", err)
	}
	if v < 0 {
		// "-N" same as "N" for tail (last N), but keep the sign sane.
		v = -v
	}
	return v * mult, fromStart, nil
}

func printHelp(w io.Writer) {
	const help = `Usage: tail [OPTION]... [FILE]...
Print the last 10 lines of each FILE to standard output.
With more than one FILE, precede each with a header giving the file name.

With no FILE, or when FILE is -, read standard input.

  -c, --bytes=[+]NUM       output the last NUM bytes; or use -c +NUM to
                             output starting with byte NUM of each file
  -n, --lines=[+]NUM       output the last NUM lines, instead of the last 10;
                             or use -n +NUM to output starting with line NUM
  -q, --quiet, --silent    never output headers giving file names
  -v, --verbose            always output headers giving file names
      --help               display this help and exit
      --version            output version information and exit

Note: --follow / -f is intentionally not supported by this tool.
`
	io.WriteString(w, help)
}
