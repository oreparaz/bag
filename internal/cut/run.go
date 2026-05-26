package cut

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type mode int

const (
	modeNone mode = iota
	modeFields
	modeChars
	modeBytes
)

type rng struct {
	lo, hi int // 1-based; hi == -1 means open-ended
}

type options struct {
	files []string
	mode  mode
	list  []rng

	delim    string
	outDelim string
	skipNoDelim bool
	complement  bool

	printHelp    bool
	printVersion bool
}

func run(args []string) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cut: %v\n", err)
		return 1
	}
	if o.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if o.printVersion {
		fmt.Println("cut (bag) -- bag drop-in")
		return 0
	}
	if o.mode == modeNone {
		fmt.Fprintln(os.Stderr, "cut: missing -b, -c, or -f")
		return 1
	}
	if o.delim == "" {
		o.delim = "\t"
	}
	if o.outDelim == "" {
		o.outDelim = o.delim
	}

	files := o.files
	if len(files) == 0 {
		files = []string{"-"}
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	exit := 0
	for _, f := range files {
		if err := processOne(out, f, o); err != nil {
			fmt.Fprintf(os.Stderr, "cut: %s: %v\n", f, err)
			exit = 1
		}
	}
	return exit
}

func processOne(out *bufio.Writer, name string, o *options) error {
	r, closer, err := openIn(name)
	if err != nil {
		return err
	}
	defer closer()
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			hadNL := strings.HasSuffix(line, "\n")
			body := strings.TrimRight(line, "\n")
			result, ok := emitLine(body, o)
			if ok {
				out.WriteString(result)
				if hadNL {
					out.WriteByte('\n')
				}
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

func emitLine(body string, o *options) (string, bool) {
	switch o.mode {
	case modeFields:
		return emitFields(body, o)
	case modeBytes:
		return emitBytes(body, o), true
	case modeChars:
		return emitChars(body, o), true
	}
	return "", false
}

func emitFields(body string, o *options) (string, bool) {
	if !strings.Contains(body, o.delim) {
		if o.skipNoDelim {
			return "", false
		}
		return body, true
	}
	fields := strings.Split(body, o.delim)
	idx := selectIndices(len(fields), o.list, o.complement)
	parts := make([]string, 0, len(idx))
	for _, i := range idx {
		if i >= 0 && i < len(fields) {
			parts = append(parts, fields[i])
		}
	}
	return strings.Join(parts, o.outDelim), true
}

func emitBytes(body string, o *options) string {
	idx := selectIndices(len(body), o.list, o.complement)
	var b strings.Builder
	for _, i := range idx {
		if i >= 0 && i < len(body) {
			b.WriteByte(body[i])
		}
	}
	return b.String()
}

// emitChars selects by Unicode code-point index (cut -c). For ASCII this
// is the same as byte indexing; for multi-byte input the GNU semantics
// is character-aware.
func emitChars(body string, o *options) string {
	runes := []rune(body)
	idx := selectIndices(len(runes), o.list, o.complement)
	var b strings.Builder
	for _, i := range idx {
		if i >= 0 && i < len(runes) {
			b.WriteRune(runes[i])
		}
	}
	return b.String()
}

// selectIndices builds a sorted list of zero-based indices selected by the
// given ranges over a sequence of length n. Honors --complement.
func selectIndices(n int, ranges []rng, complement bool) []int {
	pick := make([]bool, n)
	for _, r := range ranges {
		lo, hi := r.lo, r.hi
		if hi == -1 || hi > n {
			hi = n
		}
		if lo < 1 {
			lo = 1
		}
		for i := lo; i <= hi; i++ {
			pick[i-1] = true
		}
	}
	if complement {
		for i := range pick {
			pick[i] = !pick[i]
		}
	}
	out := make([]int, 0, n)
	for i, p := range pick {
		if p {
			out = append(out, i)
		}
	}
	return out
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

// parseList parses "1,3-5,7-" into ranges.
func parseList(s string) ([]rng, error) {
	var out []rng
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if dash := strings.IndexByte(part, '-'); dash >= 0 {
			loStr := part[:dash]
			hiStr := part[dash+1:]
			lo := 1
			hi := -1
			if loStr != "" {
				v, err := strconv.Atoi(loStr)
				if err != nil || v < 1 {
					return nil, fmt.Errorf("invalid range %q", part)
				}
				lo = v
			}
			if hiStr != "" {
				v, err := strconv.Atoi(hiStr)
				if err != nil || v < lo {
					return nil, fmt.Errorf("invalid range %q", part)
				}
				hi = v
			}
			out = append(out, rng{lo: lo, hi: hi})
		} else {
			v, err := strconv.Atoi(part)
			if err != nil || v < 1 {
				return nil, fmt.Errorf("invalid index %q", part)
			}
			out = append(out, rng{lo: v, hi: v})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("empty list")
	}
	return out, nil
}

func parseArgs(args []string) (*options, error) {
	o := &options{}
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
					return "", fmt.Errorf("--%s requires an argument", name)
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
			case 'f', 'c', 'b':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, fmt.Errorf("-%c requires an argument", c)
				}
				lst, err := parseList(arg)
				if err != nil {
					return nil, err
				}
				o.list = lst
				switch c {
				case 'f':
					o.mode = modeFields
				case 'c':
					o.mode = modeChars
				case 'b':
					o.mode = modeBytes
				}
				j = len(a)
			case 'd':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, errors.New("-d requires an argument")
				}
				if arg == "" {
					return nil, errors.New("-d requires a non-empty delimiter")
				}
				o.delim = arg
				j = len(a)
			case 's':
				o.skipNoDelim = true
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
	case "fields":
		v, err := next()
		if err != nil {
			return err
		}
		lst, err := parseList(v)
		if err != nil {
			return err
		}
		o.list = lst
		o.mode = modeFields
	case "characters":
		v, err := next()
		if err != nil {
			return err
		}
		lst, err := parseList(v)
		if err != nil {
			return err
		}
		o.list = lst
		o.mode = modeChars
	case "bytes":
		v, err := next()
		if err != nil {
			return err
		}
		lst, err := parseList(v)
		if err != nil {
			return err
		}
		o.list = lst
		o.mode = modeBytes
	case "delimiter":
		v, err := next()
		if err != nil {
			return err
		}
		if v == "" {
			return errors.New("--delimiter requires a non-empty argument")
		}
		o.delim = v
	case "output-delimiter":
		v, err := next()
		if err != nil {
			return err
		}
		o.outDelim = v
	case "only-delimited":
		o.skipNoDelim = true
	case "complement":
		o.complement = true
	case "help":
		o.printHelp = true
	case "version":
		o.printVersion = true
	default:
		return fmt.Errorf("unrecognized option '--%s'", name)
	}
	return nil
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
	const help = `Usage: cut OPTION... [FILE]...
Print selected parts of lines from each FILE to standard output.

  -b, --bytes=LIST          select these bytes
  -c, --characters=LIST     select these characters (== -b for ASCII)
  -f, --fields=LIST         select these fields
  -d, --delimiter=DELIM     field delimiter (default: TAB)
      --output-delimiter=STRING  use STRING as the output delimiter
  -s, --only-delimited      with -f, skip lines without DELIM
      --complement          complement the LIST
      --help                display this help
      --version             display version
`
	io.WriteString(w, help)
}
