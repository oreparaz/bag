package sort

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	stdsort "sort"
	"strconv"
	"strings"
	"unicode"
)

type options struct {
	files []string

	numeric    bool
	reverse    bool
	unique     bool
	icase      bool
	ignoreLead bool
	output     string
	separator  string
	keys       []keySpec

	check bool

	printHelp    bool
	printVersion bool
}

// keySpec encodes a -k POS1[,POS2][TYPE] argument.
//
//	start: 1-based field index
//	end:   1-based field index, 0 = end of line
//	startChar: char offset within start field (1-based)
//	endChar:   char offset within end field (0 = end of field)
//	flags:     overrides for this key (numeric, reverse, icase, ignoreLead)
type keySpec struct {
	start, end           int
	startChar, endChar   int
	numeric, reverse, icase, ignoreLead bool
}

func run(args []string) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sort: %v\n", err)
		return 2
	}
	if o.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if o.printVersion {
		fmt.Println("sort (bag) -- bag drop-in")
		return 0
	}

	files := o.files
	if len(files) == 0 {
		files = []string{"-"}
	}

	var lines []string
	for _, f := range files {
		ls, err := readLines(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sort: %s: %v\n", f, err)
			return 2
		}
		lines = append(lines, ls...)
	}

	if o.check {
		if isSorted(lines, o) {
			return 0
		}
		fmt.Fprintln(os.Stderr, "sort: input not sorted")
		return 1
	}

	stdsort.SliceStable(lines, func(i, j int) bool {
		return less(lines[i], lines[j], o)
	})

	if o.unique {
		lines = dedup(lines, o)
	}

	out := os.Stdout
	if o.output != "" {
		f, err := os.Create(o.output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sort: %s: %v\n", o.output, err)
			return 2
		}
		defer f.Close()
		out = f
	}
	bw := bufio.NewWriter(out)
	defer bw.Flush()
	for _, l := range lines {
		bw.WriteString(l)
		bw.WriteByte('\n')
	}
	return 0
}

func readLines(name string) ([]string, error) {
	r, closer, err := openIn(name)
	if err != nil {
		return nil, err
	}
	defer closer()
	br := bufio.NewReaderSize(r, 64*1024)
	var lines []string
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			lines = append(lines, strings.TrimSuffix(line, "\n"))
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return lines, nil
			}
			return lines, err
		}
	}
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

func less(a, b string, o *options) bool {
	if len(o.keys) > 0 {
		for _, k := range o.keys {
			ka := extractKey(a, k, o)
			kb := extractKey(b, k, o)
			cmp := keyCompare(ka, kb, k, o)
			if cmp != 0 {
				if k.reverse {
					return cmp > 0
				}
				return cmp < 0
			}
		}
		return false
	}
	cmp := plainCompare(a, b, o)
	if o.reverse {
		return cmp > 0
	}
	return cmp < 0
}

func plainCompare(a, b string, o *options) int {
	if o.ignoreLead {
		a = strings.TrimLeft(a, " \t")
		b = strings.TrimLeft(b, " \t")
	}
	if o.icase {
		a = strings.ToLower(a)
		b = strings.ToLower(b)
	}
	if o.numeric {
		return compareNumeric(a, b)
	}
	return strings.Compare(a, b)
}

func keyCompare(a, b string, k keySpec, o *options) int {
	ignoreLead := o.ignoreLead || k.ignoreLead
	if ignoreLead {
		a = strings.TrimLeft(a, " \t")
		b = strings.TrimLeft(b, " \t")
	}
	if o.icase || k.icase {
		a = strings.ToLower(a)
		b = strings.ToLower(b)
	}
	if o.numeric || k.numeric {
		return compareNumeric(a, b)
	}
	return strings.Compare(a, b)
}

// compareNumeric: best-effort numeric compare. Strips leading whitespace
// and a leading sign; treats unparseable as 0 (matches GNU sort).
func compareNumeric(a, b string) int {
	fa := parseNum(a)
	fb := parseNum(b)
	switch {
	case fa < fb:
		return -1
	case fa > fb:
		return 1
	}
	return 0
}

func parseNum(s string) float64 {
	s = strings.TrimLeft(s, " \t")
	// Find longest leading numeric prefix.
	end := 0
	if end < len(s) && (s[end] == '+' || s[end] == '-') {
		end++
	}
	for end < len(s) {
		c := s[end]
		if c >= '0' && c <= '9' {
			end++
			continue
		}
		break
	}
	if end < len(s) && s[end] == '.' {
		end++
		for end < len(s) && s[end] >= '0' && s[end] <= '9' {
			end++
		}
	}
	if end == 0 || (end == 1 && (s[0] == '+' || s[0] == '-')) {
		return 0
	}
	v, err := strconv.ParseFloat(s[:end], 64)
	if err != nil {
		return 0
	}
	return v
}

// extractKey pulls the substring of line that's covered by k.
func extractKey(line string, k keySpec, o *options) string {
	sep := o.separator
	var fields []string
	if sep == "" {
		fields = whitespaceFields(line)
	} else {
		fields = strings.Split(line, sep)
	}

	startField := k.start - 1
	if startField < 0 {
		startField = 0
	}
	endField := k.end - 1
	if k.end == 0 || endField >= len(fields) {
		endField = len(fields) - 1
	}
	if startField >= len(fields) {
		return ""
	}
	parts := fields[startField : endField+1]
	joiner := sep
	if joiner == "" {
		joiner = " "
	}
	s := strings.Join(parts, joiner)

	if k.startChar > 1 && len(s) >= k.startChar-1 {
		s = s[k.startChar-1:]
	}
	if k.endChar > 0 && len(s) > k.endChar-(k.startChar-1) {
		// Trim the tail back to endChar.
		want := k.endChar - (k.startChar - 1)
		if want < len(s) {
			s = s[:want]
		}
	}
	return s
}

// whitespaceFields imitates GNU sort's default "blanks separate fields"
// behavior: each field starts after the run of whitespace preceding it.
func whitespaceFields(s string) []string {
	var out []string
	start := -1
	for i, r := range s {
		if unicode.IsSpace(r) {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

func dedup(lines []string, o *options) []string {
	if len(lines) == 0 {
		return lines
	}
	out := lines[:1]
	for i := 1; i < len(lines); i++ {
		if !isEqualForUnique(lines[i-1], lines[i], o) {
			out = append(out, lines[i])
		}
	}
	return out
}

func isEqualForUnique(a, b string, o *options) bool {
	// "equal" means !less(a,b) && !less(b,a) under whatever ordering is in
	// effect. plainCompare with options applied is the right thing.
	if len(o.keys) > 0 {
		for _, k := range o.keys {
			ka := extractKey(a, k, o)
			kb := extractKey(b, k, o)
			if keyCompare(ka, kb, k, o) != 0 {
				return false
			}
		}
		return true
	}
	return plainCompare(a, b, o) == 0
}

func isSorted(lines []string, o *options) bool {
	for i := 1; i < len(lines); i++ {
		if less(lines[i], lines[i-1], o) {
			return false
		}
	}
	return true
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
			case 'n':
				o.numeric = true
				j++
			case 'r':
				o.reverse = true
				j++
			case 'u':
				o.unique = true
				j++
			case 'f':
				o.icase = true
				j++
			case 'b':
				o.ignoreLead = true
				j++
			case 's':
				// already stable; accept silently
				j++
			case 'c':
				o.check = true
				j++
			case 'k':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, errors.New("-k requires an argument")
				}
				k, err := parseKey(arg)
				if err != nil {
					return nil, err
				}
				o.keys = append(o.keys, k)
				j = len(a)
			case 't':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, errors.New("-t requires an argument")
				}
				o.separator = arg
				j = len(a)
			case 'o':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, errors.New("-o requires an argument")
				}
				o.output = arg
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
	case "numeric-sort":
		o.numeric = true
	case "reverse":
		o.reverse = true
	case "unique":
		o.unique = true
	case "ignore-case":
		o.icase = true
	case "ignore-leading-blanks":
		o.ignoreLead = true
	case "stable":
		// already stable
	case "check":
		o.check = true
	case "key":
		v, err := next()
		if err != nil {
			return err
		}
		k, err := parseKey(v)
		if err != nil {
			return err
		}
		o.keys = append(o.keys, k)
	case "field-separator":
		v, err := next()
		if err != nil {
			return err
		}
		o.separator = v
	case "output":
		v, err := next()
		if err != nil {
			return err
		}
		o.output = v
	case "help":
		o.printHelp = true
	case "version":
		o.printVersion = true
	default:
		return fmt.Errorf("unrecognized option '--%s'", name)
	}
	return nil
}

// parseKey accepts "F[.C][TYPE][,F2[.C2][TYPE2]]". TYPE is one of n, r, f,
// b — flags applied to that key only.
func parseKey(s string) (keySpec, error) {
	var k keySpec
	parts := strings.SplitN(s, ",", 2)
	start, err := parseKeyPart(parts[0])
	if err != nil {
		return k, err
	}
	k.start = start.field
	k.startChar = start.ch
	applyKeyFlags(&k, start.flags)
	if len(parts) == 2 {
		end, err := parseKeyPart(parts[1])
		if err != nil {
			return k, err
		}
		k.end = end.field
		k.endChar = end.ch
		applyKeyFlags(&k, end.flags)
	}
	return k, nil
}

type keyPart struct {
	field, ch int
	flags     string
}

func parseKeyPart(s string) (keyPart, error) {
	var p keyPart
	// Split off trailing flags.
	flagsStart := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9') && c != '.' {
			flagsStart = i
			break
		}
	}
	num := s
	if flagsStart >= 0 {
		num = s[:flagsStart]
		p.flags = s[flagsStart:]
	}
	if dot := strings.IndexByte(num, '.'); dot >= 0 {
		f, err := strconv.Atoi(num[:dot])
		if err != nil {
			return p, fmt.Errorf("invalid key %q", s)
		}
		p.field = f
		c, err := strconv.Atoi(num[dot+1:])
		if err != nil {
			return p, fmt.Errorf("invalid key %q", s)
		}
		p.ch = c
		return p, nil
	}
	f, err := strconv.Atoi(num)
	if err != nil {
		return p, fmt.Errorf("invalid key %q", s)
	}
	p.field = f
	return p, nil
}

func applyKeyFlags(k *keySpec, flags string) {
	for _, f := range flags {
		switch f {
		case 'n':
			k.numeric = true
		case 'r':
			k.reverse = true
		case 'f':
			k.icase = true
		case 'b':
			k.ignoreLead = true
		}
	}
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
	const help = `Usage: sort [OPTION]... [FILE]...
Write sorted concatenation of all FILE(s) to standard output.

  -n, --numeric-sort
  -r, --reverse
  -u, --unique
  -f, --ignore-case
  -b, --ignore-leading-blanks
  -k, --key=POS1[,POS2][TYPE]   sort via a key starting at POS1, ending at POS2
  -t, --field-separator=SEP     use SEP instead of non-blank to blank transition
  -o, --output=FILE             write result to FILE instead of standard output
  -c, --check                   check whether input is sorted; exit 1 if not
  -s, --stable                  stable sort (default for bag)
      --help                    display this help
      --version                 display version
`
	io.WriteString(w, help)
}
