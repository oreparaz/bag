package grep

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type options struct {
	patterns []string
	files    []string

	ignoreCase   bool
	invert       bool
	count        bool
	listMatch    bool
	listNoMatch  bool
	lineNumber   bool
	withFilename bool // -H
	noFilename   bool // -h
	recursive    bool
	extended     bool
	fixed        bool
	wordRegexp   bool
	lineRegexp   bool

	after   int
	before  int
	context int

	includes    []string
	excludes    []string
	excludeDirs []string

	quiet     bool
	noMsgs    bool

	patternFromCmd bool // pattern provided via positional arg

	printHelp    bool
	printVersion bool
}

func run(args []string) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "grep: %v\n", err)
		return 2
	}
	if o.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if o.printVersion {
		fmt.Println("grep (bag) -- bag drop-in")
		return 0
	}
	if len(o.patterns) == 0 {
		fmt.Fprintln(os.Stderr, "grep: no pattern")
		return 2
	}

	if o.context > 0 {
		if o.before == 0 {
			o.before = o.context
		}
		if o.after == 0 {
			o.after = o.context
		}
	}

	pattern, err := buildPattern(o)
	if err != nil {
		fmt.Fprintf(os.Stderr, "grep: %v\n", err)
		return 2
	}

	files := o.files
	if len(files) == 0 {
		files = []string{"-"}
	}
	multi := len(files) > 1 || o.recursive
	showFilename := o.withFilename
	if !o.noFilename && multi {
		showFilename = true
	}
	if o.noFilename {
		showFilename = false
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	matched := false
	hadError := false
	for _, f := range files {
		m, err := processOne(out, f, pattern, o, showFilename)
		if err != nil {
			hadError = true
			if !o.noMsgs {
				fmt.Fprintf(os.Stderr, "grep: %s: %v\n", f, err)
			}
		}
		if m {
			matched = true
			if o.quiet {
				return 0
			}
		}
	}
	if hadError {
		return 2
	}
	if matched {
		return 0
	}
	return 1
}

func buildPattern(o *options) (*regexp.Regexp, error) {
	if len(o.patterns) == 0 {
		return nil, errors.New("no pattern")
	}
	parts := make([]string, 0, len(o.patterns))
	for _, p := range o.patterns {
		// Each -e may itself contain newlines; treat each line as an
		// alternative (matches GNU grep semantics). An empty alternative
		// is preserved — GNU grep matches every line when given `""`.
		for _, line := range strings.Split(p, "\n") {
			esc := line
			if o.fixed {
				esc = regexp.QuoteMeta(line)
			} else if !o.extended {
				// Default mode is BRE. Translate to RE2 so `\(` `\)` are
				// groups, `(` `)` are literal, and `*+?` at the start of
				// a pattern are literal (GNU grep behaviour). This is what
				// lets autoconf's `grep '*+'` tests not blow up.
				esc = breToRE2(esc)
			}
			if o.wordRegexp {
				esc = `\b(?:` + esc + `)\b`
			}
			if o.lineRegexp {
				esc = `\A(?:` + esc + `)\z`
			}
			parts = append(parts, esc)
		}
	}
	if len(parts) == 0 {
		return nil, errors.New("no pattern")
	}
	final := "(?:" + strings.Join(parts, ")|(?:") + ")"
	if o.ignoreCase {
		final = "(?i)" + final
	}
	return regexp.Compile(final)
}

// breToRE2 converts a POSIX Basic Regular Expression into something
// RE2 accepts. Same rules as sed's BRE translator:
//   \( \) \| \+ \? \{ \}   group / alt / quantifier metachars
//   ( ) | + ? { }          literal
//   * at the start of a pattern (or after `(` or `|`) is literal
//   \< \>  -> \b
// The translator is intentionally pragmatic — it handles the patterns
// autoconf and shell scripts actually use, not every POSIX edge case.
func breToRE2(pat string) string {
	var out strings.Builder
	out.Grow(len(pat))
	inClass := false
	// atStart is true when a `*` `+` `?` here would be invalid in
	// strict POSIX (no preceding atom) — GNU grep treats these as
	// literal at start-of-pattern and after `(` or `|`.
	atStart := true
	for i := 0; i < len(pat); i++ {
		c := pat[i]
		if inClass {
			out.WriteByte(c)
			if c == ']' {
				inClass = false
			}
			continue
		}
		if c == '[' {
			out.WriteByte(c)
			inClass = true
			if i+1 < len(pat) && pat[i+1] == ']' {
				out.WriteByte(']')
				i++
			} else if i+2 < len(pat) && pat[i+1] == '^' && pat[i+2] == ']' {
				out.WriteByte('^')
				out.WriteByte(']')
				i += 2
			}
			atStart = false
			continue
		}
		if c == '\\' && i+1 < len(pat) {
			n := pat[i+1]
			switch n {
			case '(', ')', '|':
				out.WriteByte(n)
				i++
				if n == '(' || n == '|' {
					atStart = true
				} else {
					atStart = false
				}
				continue
			case '+', '?', '{', '}':
				out.WriteByte(n)
				i++
				atStart = false
				continue
			case '<', '>':
				out.WriteString(`\b`)
				i++
				atStart = false
				continue
			default:
				out.WriteByte(c)
				out.WriteByte(n)
				i++
				atStart = false
				continue
			}
		}
		switch c {
		case '(', ')', '|', '+', '?', '{', '}':
			out.WriteByte('\\')
			out.WriteByte(c)
			atStart = false
		case '*':
			// `*` at start (or right after `(` or `|`) is literal in
			// GNU BRE; elsewhere it's a quantifier.
			if atStart {
				out.WriteString(`\*`)
			} else {
				out.WriteByte('*')
			}
			atStart = false
		case '$':
			// BRE anchor only at end; literal elsewhere.
			if i == len(pat)-1 {
				out.WriteByte('$')
			} else {
				out.WriteString(`\$`)
			}
			atStart = false
		case '^':
			// BRE anchor only at start; literal elsewhere.
			if i == 0 {
				out.WriteByte('^')
			} else {
				out.WriteString(`\^`)
			}
			atStart = false
		default:
			out.WriteByte(c)
			atStart = false
		}
	}
	return out.String()
}

func processOne(out *bufio.Writer, name string, re *regexp.Regexp, o *options, showFilename bool) (bool, error) {
	if name == "-" {
		return scan(out, "(standard input)", os.Stdin, re, o, showFilename), nil
	}
	info, err := os.Stat(name)
	if err != nil {
		return false, err
	}
	if info.IsDir() {
		if !o.recursive {
			return false, fmt.Errorf("Is a directory")
		}
		return scanDir(out, name, re, o, showFilename), nil
	}
	f, err := os.Open(name)
	if err != nil {
		return false, err
	}
	defer f.Close()
	return scan(out, name, f, re, o, showFilename), nil
}

func scanDir(out *bufio.Writer, root string, re *regexp.Regexp, o *options, showFilename bool) bool {
	matched := false
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		base := filepath.Base(p)
		if d.IsDir() {
			if dirExcluded(base, o.excludeDirs) {
				return filepath.SkipDir
			}
			return nil
		}
		if !pathIncluded(base, o.includes, o.excludes) {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			if !o.noMsgs {
				fmt.Fprintf(os.Stderr, "grep: %s: %v\n", p, err)
			}
			return nil
		}
		defer f.Close()
		if scan(out, p, f, re, o, showFilename) {
			matched = true
		}
		return nil
	})
	return matched
}

func pathIncluded(name string, includes, excludes []string) bool {
	if len(includes) > 0 {
		ok := false
		for _, g := range includes {
			if matched, _ := filepath.Match(g, name); matched {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, g := range excludes {
		if matched, _ := filepath.Match(g, name); matched {
			return false
		}
	}
	return true
}

func dirExcluded(name string, excludes []string) bool {
	for _, g := range excludes {
		if matched, _ := filepath.Match(g, name); matched {
			return true
		}
	}
	return false
}

// scan emits matches for one source. Returns whether any match was found.
//
// We keep a small ring buffer of up to opts.before lines so context-prefix
// is available when a match arrives.
func scan(out *bufio.Writer, label string, r io.Reader, re *regexp.Regexp, o *options, showFilename bool) bool {
	br := bufio.NewReaderSize(r, 64*1024)
	var matched bool
	var matches int64
	var lineNo int

	type ringItem struct {
		num  int
		line []byte
	}
	ring := make([]ringItem, 0, o.before)
	pendingAfter := 0
	lastEmitted := 0

	emit := func(num int, line []byte, sep byte) {
		if showFilename {
			out.WriteString(label)
			out.WriteByte(sep)
		}
		if o.lineNumber {
			out.WriteString(strconv.Itoa(num))
			out.WriteByte(sep)
		}
		out.Write(line)
		if len(line) == 0 || line[len(line)-1] != '\n' {
			out.WriteByte('\n')
		}
	}

	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			lineNo++
			lineNoNL := bytes.TrimRight(line, "\n")
			isMatch := re.Match(lineNoNL)
			if o.invert {
				isMatch = !isMatch
			}
			if isMatch {
				matched = true
				matches++
				if !o.count && !o.listMatch && !o.listNoMatch && !o.quiet {
					// Emit GNU-style "--" separator between non-overlapping
					// context groups. The next group starts either at the
					// oldest pre-context line in the ring (if any), or at
					// the match itself when before-context is 0.
					nextStart := lineNo
					if o.before > 0 && len(ring) > 0 && ring[0].num > lastEmitted {
						nextStart = ring[0].num
					}
					if (o.before > 0 || o.after > 0) && lastEmitted > 0 && nextStart > lastEmitted+1 {
						out.WriteString("--\n")
					}
					if o.before > 0 {
						for _, ri := range ring {
							if ri.num <= lastEmitted {
								continue
							}
							emit(ri.num, ri.line, '-')
						}
						ring = ring[:0]
					}
					emit(lineNo, line, ':')
					lastEmitted = lineNo
					pendingAfter = o.after
				}
			} else if pendingAfter > 0 && !o.count && !o.listMatch && !o.listNoMatch && !o.quiet {
				emit(lineNo, line, '-')
				lastEmitted = lineNo
				pendingAfter--
			} else if o.before > 0 {
				if len(ring) == cap(ring) && cap(ring) > 0 {
					ring = ring[1:]
				}
				cp := make([]byte, len(line))
				copy(cp, line)
				ring = append(ring, ringItem{num: lineNo, line: cp})
			}

			if o.quiet && matched {
				return true
			}
		}
		if err != nil {
			break
		}
	}

	if o.count {
		if showFilename {
			out.WriteString(label)
			out.WriteByte(':')
		}
		out.WriteString(strconv.FormatInt(matches, 10))
		out.WriteByte('\n')
	}
	if o.listMatch && matched {
		out.WriteString(label)
		out.WriteByte('\n')
	}
	if o.listNoMatch && !matched {
		out.WriteString(label)
		out.WriteByte('\n')
	}
	return matched
}

func parseArgs(args []string) (*options, error) {
	o := &options{}
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "" || (a[0] != '-') || a == "-" {
			o.consumePositional(a)
			i++
			continue
		}
		if a == "--" {
			for _, f := range args[i+1:] {
				o.consumePositional(f)
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
			case 'i':
				o.ignoreCase = true
				j++
			case 'v':
				o.invert = true
				j++
			case 'c':
				o.count = true
				j++
			case 'l':
				o.listMatch = true
				j++
			case 'L':
				o.listNoMatch = true
				j++
			case 'n':
				o.lineNumber = true
				j++
			case 'H':
				o.withFilename = true
				j++
			case 'h':
				o.noFilename = true
				j++
			case 'r', 'R':
				o.recursive = true
				j++
			case 'E':
				o.extended = true
				j++
			case 'F':
				o.fixed = true
				j++
			case 'w':
				o.wordRegexp = true
				j++
			case 'x':
				o.lineRegexp = true
				j++
			case 'q':
				o.quiet = true
				j++
			case 's':
				o.noMsgs = true
				j++
			case 'A', 'B', 'C':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, fmt.Errorf("-%c requires an argument", c)
				}
				n, err := strconv.Atoi(arg)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("invalid -%c value %q", c, arg)
				}
				switch c {
				case 'A':
					o.after = n
				case 'B':
					o.before = n
				case 'C':
					o.context = n
				}
				j = len(a)
			case 'e':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, errors.New("-e requires an argument")
				}
				o.patterns = append(o.patterns, arg)
				j = len(a)
			case 'f':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, errors.New("-f requires an argument")
				}
				body, err := os.ReadFile(arg)
				if err != nil {
					return nil, err
				}
				o.patterns = append(o.patterns, string(body))
				j = len(a)
			case 'V':
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

func (o *options) consumePositional(a string) {
	if len(o.patterns) == 0 && !o.patternFromCmd {
		o.patterns = append(o.patterns, a)
		o.patternFromCmd = true
		return
	}
	o.files = append(o.files, a)
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

func applyLong(o *options, name string, next func() (string, error)) error {
	switch name {
	case "ignore-case":
		o.ignoreCase = true
	case "invert-match":
		o.invert = true
	case "count":
		o.count = true
	case "files-with-matches":
		o.listMatch = true
	case "files-without-match":
		o.listNoMatch = true
	case "line-number":
		o.lineNumber = true
	case "with-filename":
		o.withFilename = true
	case "no-filename":
		o.noFilename = true
	case "recursive":
		o.recursive = true
	case "extended-regexp":
		o.extended = true
	case "fixed-strings":
		o.fixed = true
	case "word-regexp":
		o.wordRegexp = true
	case "line-regexp":
		o.lineRegexp = true
	case "quiet", "silent":
		o.quiet = true
	case "no-messages":
		o.noMsgs = true
	case "after-context":
		v, err := next()
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return err
		}
		o.after = n
	case "before-context":
		v, err := next()
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return err
		}
		o.before = n
	case "context":
		v, err := next()
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return err
		}
		o.context = n
	case "regexp":
		v, err := next()
		if err != nil {
			return err
		}
		o.patterns = append(o.patterns, v)
	case "file":
		v, err := next()
		if err != nil {
			return err
		}
		body, err := os.ReadFile(v)
		if err != nil {
			return err
		}
		o.patterns = append(o.patterns, string(body))
	case "include":
		v, err := next()
		if err != nil {
			return err
		}
		o.includes = append(o.includes, v)
	case "exclude":
		v, err := next()
		if err != nil {
			return err
		}
		o.excludes = append(o.excludes, v)
	case "exclude-dir":
		v, err := next()
		if err != nil {
			return err
		}
		o.excludeDirs = append(o.excludeDirs, v)
	case "color", "colour":
		// recognized; we don't emit color codes
		_, _ = next()
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
	const help = `Usage: grep [OPTION]... PATTERNS [FILE]...
Search for PATTERNS in each FILE.

  -E, --extended-regexp     PATTERNS are extended regular expressions
  -F, --fixed-strings       PATTERNS are literal strings
  -e, --regexp=PATTERNS     repeatable; multi-line patterns split per line
  -f, --file=FILE           read patterns from FILE
  -i, --ignore-case
  -v, --invert-match
  -w, --word-regexp
  -x, --line-regexp
  -c, --count
  -l, --files-with-matches
  -L, --files-without-match
  -n, --line-number
  -H, --with-filename       (default for multi-file)
  -h, --no-filename         (default for single-file / stdin)
  -r, -R, --recursive
  -A NUM, --after-context=NUM
  -B NUM, --before-context=NUM
  -C NUM, --context=NUM
      --include=GLOB
      --exclude=GLOB
      --exclude-dir=GLOB
  -q, --quiet
  -s, --no-messages
      --help
      --version

Note: PATTERNS use Go's RE2 engine. PCRE features (backrefs, lookaround)
are not supported.
`
	io.WriteString(w, help)
}
