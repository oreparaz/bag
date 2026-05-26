package uniq

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
)

type options struct {
	files []string

	count       bool
	dupOnly     bool
	uniqOnly    bool
	icase       bool
	skipFields  int
	skipChars   int
	compareN    int
	compareNSet bool

	printHelp    bool
	printVersion bool
}

func run(args []string) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "uniq: %v\n", err)
		return 1
	}
	if o.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if o.printVersion {
		fmt.Println("uniq (bag) -- bag drop-in")
		return 0
	}
	if o.dupOnly && o.uniqOnly {
		fmt.Fprintln(os.Stderr, "uniq: -d and -u are mutually exclusive")
		return 1
	}

	if len(o.files) > 2 {
		fmt.Fprintf(os.Stderr, "uniq: extra operand %q\n", o.files[2])
		return 1
	}
	in := os.Stdin
	out := os.Stdout
	if len(o.files) >= 1 && o.files[0] != "-" {
		f, err := os.Open(o.files[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "uniq: %s: %v\n", o.files[0], err)
			return 1
		}
		defer f.Close()
		in = f
	}
	if len(o.files) >= 2 && o.files[1] != "-" {
		f, err := os.Create(o.files[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "uniq: %s: %v\n", o.files[1], err)
			return 1
		}
		defer f.Close()
		out = f
	}

	br := bufio.NewReaderSize(in, 64*1024)
	bw := bufio.NewWriter(out)
	defer bw.Flush()

	var prev string
	var prevKey string
	var prevHadNL bool
	prevCount := 0
	first := true

	flush := func() {
		if first {
			return
		}
		emit := true
		switch {
		case o.dupOnly:
			emit = prevCount > 1
		case o.uniqOnly:
			emit = prevCount == 1
		}
		if !emit {
			return
		}
		if o.count {
			fmt.Fprintf(bw, "%7d %s", prevCount, prev)
		} else {
			bw.WriteString(prev)
		}
		if !prevHadNL {
			bw.WriteByte('\n')
		}
	}

	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			hadNL := strings.HasSuffix(line, "\n")
			body := strings.TrimRight(line, "\n")
			key := compareKey(body, o)
			if first {
				prev = body
				if hadNL {
					prev += "\n"
				}
				prevKey = key
				prevCount = 1
				prevHadNL = hadNL
				first = false
			} else if key == prevKey {
				prevCount++
			} else {
				flush()
				prev = body
				if hadNL {
					prev += "\n"
				}
				prevKey = key
				prevCount = 1
				prevHadNL = hadNL
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			fmt.Fprintf(os.Stderr, "uniq: %v\n", err)
			return 1
		}
	}
	flush()
	return 0
}

func compareKey(line string, o *options) string {
	s := skipFieldsAndChars(line, o.skipFields, o.skipChars)
	if o.compareNSet && len(s) > o.compareN {
		s = s[:o.compareN]
	}
	if o.icase {
		s = strings.ToLower(s)
	}
	return s
}

func skipFieldsAndChars(s string, fields, chars int) string {
	// Skip leading whitespace + (field non-ws + ws) f times.
	for f := 0; f < fields; f++ {
		// trim leading whitespace
		i := 0
		for i < len(s) && unicode.IsSpace(rune(s[i])) {
			i++
		}
		// skip non-whitespace
		for i < len(s) && !unicode.IsSpace(rune(s[i])) {
			i++
		}
		s = s[i:]
	}
	// Skip leading whitespace before counting chars (matches GNU uniq).
	i := 0
	for i < len(s) && unicode.IsSpace(rune(s[i])) {
		i++
	}
	s = s[i:]
	if chars > len(s) {
		return ""
	}
	return s[chars:]
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
			case 'c':
				o.count = true
				j++
			case 'd':
				o.dupOnly = true
				j++
			case 'u':
				o.uniqOnly = true
				j++
			case 'i':
				o.icase = true
				j++
			case 'f', 's', 'w':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, fmt.Errorf("-%c requires an argument", c)
				}
				n, err := strconv.Atoi(arg)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("invalid -%c value %q", c, arg)
				}
				switch c {
				case 'f':
					o.skipFields = n
				case 's':
					o.skipChars = n
				case 'w':
					o.compareN = n
					o.compareNSet = true
				}
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
	case "count":
		o.count = true
	case "repeated":
		o.dupOnly = true
	case "unique":
		o.uniqOnly = true
	case "ignore-case":
		o.icase = true
	case "skip-fields":
		v, err := next()
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return errors.New("invalid --skip-fields")
		}
		o.skipFields = n
	case "skip-chars":
		v, err := next()
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return errors.New("invalid --skip-chars")
		}
		o.skipChars = n
	case "check-chars":
		v, err := next()
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return errors.New("invalid --check-chars")
		}
		o.compareN = n
		o.compareNSet = true
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
	const help = `Usage: uniq [OPTION]... [INPUT [OUTPUT]]
Filter adjacent matching lines from INPUT (or stdin).

  -c, --count           prefix lines by the number of occurrences
  -d, --repeated        only print duplicate lines
  -u, --unique          only print unique lines
  -i, --ignore-case     ignore differences in case when comparing
  -f, --skip-fields=N   skip first N fields when comparing
  -s, --skip-chars=N    skip first N characters when comparing
  -w, --check-chars=N   compare at most N characters per line
      --help            display this help
      --version         display version
`
	io.WriteString(w, help)
}
