package ag

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
	pattern string
	roots   []string

	ignoreCase    bool
	caseSensitive bool
	literal       bool
	wordRegexp    bool
	invert        bool
	listMatch     bool
	listNoMatch   bool
	count         bool

	after  int
	before int

	fileRegex string
	addIgnore []string
	noIgnore  bool
	hidden    bool
	allTypes  bool

	depth int

	null         bool
	withFile     bool
	noFilename   bool
	groupForce   *bool
	patternFromE bool

	printHelp    bool
	printVersion bool
}

func run(args []string) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ag: %v\n", err)
		return 2
	}
	if o.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if o.printVersion {
		fmt.Println("ag (bag) -- bag drop-in")
		return 0
	}
	if o.pattern == "" {
		fmt.Fprintln(os.Stderr, "ag: no pattern")
		return 2
	}

	re, err := buildPattern(o)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ag: %v\n", err)
		return 2
	}

	var fileRE *regexp.Regexp
	if o.fileRegex != "" {
		fileRE, err = regexp.Compile(o.fileRegex)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ag: -G: %v\n", err)
			return 2
		}
	}

	roots := o.roots
	if len(roots) == 0 {
		roots = []string{"."}
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	grouped := isatty(os.Stdout) || (o.groupForce != nil && *o.groupForce)
	if o.groupForce != nil {
		grouped = *o.groupForce
	}

	state := &state{
		out:     out,
		opts:    o,
		re:      re,
		fileRE:  fileRE,
		grouped: grouped,
	}

	for _, root := range roots {
		state.walk(root)
	}
	if state.matched {
		return 0
	}
	return 1
}

// state carries everything walkers / scanners need.
type state struct {
	out     *bufio.Writer
	opts    *options
	re      *regexp.Regexp
	fileRE  *regexp.Regexp
	grouped bool

	matched bool

	// per-root .gitignore / .ignore patterns
	rootIgnores []ignorePattern
}

func buildPattern(o *options) (*regexp.Regexp, error) {
	pat := o.pattern
	if o.literal {
		pat = regexp.QuoteMeta(pat)
	}
	if o.wordRegexp {
		pat = `\b(?:` + pat + `)\b`
	}
	icase := o.ignoreCase
	if !o.caseSensitive && !o.ignoreCase {
		// Smart-case: case-insensitive iff pattern has no uppercase.
		icase = !hasUpper(o.pattern)
	}
	if icase {
		pat = "(?i)" + pat
	}
	return regexp.Compile(pat)
}

func hasUpper(s string) bool {
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

// walk descends into root, applying ignore rules.
func (s *state) walk(root string) {
	// Pre-load root's .gitignore / .ignore patterns.
	s.rootIgnores = s.rootIgnores[:0]
	if !s.opts.noIgnore {
		s.rootIgnores = append(s.rootIgnores, loadIgnoreFile(filepath.Join(root, ".gitignore"))...)
		s.rootIgnores = append(s.rootIgnores, loadIgnoreFile(filepath.Join(root, ".ignore"))...)
	}

	rootInfo, err := os.Lstat(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ag: %s: %v\n", root, err)
		return
	}
	if rootInfo.IsDir() {
		s.walkDir(root, root, 0)
	} else {
		s.scanFile(root)
	}
}

func (s *state) walkDir(root, dir string, depth int) {
	if s.opts.depth > 0 && depth > s.opts.depth {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ag: %s: %v\n", dir, err)
		return
	}
	for _, de := range entries {
		name := de.Name()
		full := filepath.Join(dir, name)
		rel, _ := filepath.Rel(root, full)
		if !s.opts.hidden && isHidden(name) {
			continue
		}
		if !s.opts.noIgnore && isDefaultSkipDir(name) && de.IsDir() {
			continue
		}
		if s.shouldIgnore(rel, de.IsDir()) {
			continue
		}
		if de.IsDir() {
			s.walkDir(root, full, depth+1)
			continue
		}
		// File filter via -G.
		if s.fileRE != nil && !s.fileRE.MatchString(full) {
			continue
		}
		s.scanFile(full)
	}
}

func (s *state) shouldIgnore(rel string, isDir bool) bool {
	if rel == "." || rel == "" {
		return false
	}
	for _, p := range s.opts.addIgnore {
		if matchIgnore(p, rel, isDir) {
			return true
		}
	}
	for _, p := range s.rootIgnores {
		if p.matches(rel, isDir) {
			return true
		}
	}
	return false
}

func isHidden(name string) bool {
	return len(name) > 0 && name[0] == '.' && name != "." && name != ".."
}

func isDefaultSkipDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".bzr":
		return true
	}
	return false
}

func (s *state) scanFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ag: %s: %v\n", path, err)
		return
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, 64*1024)

	if !s.opts.allTypes {
		// Sniff first 8 KiB for a NUL byte → treat as binary.
		head, _ := br.Peek(8 * 1024)
		if bytes.IndexByte(head, 0) >= 0 {
			return
		}
	}

	s.scanReader(path, br)
}

func (s *state) scanReader(path string, r io.Reader) {
	br := bufio.NewReaderSize(r, 64*1024)
	type ringItem struct {
		num  int
		line []byte
	}
	ring := make([]ringItem, 0, s.opts.before)
	pendingAfter := 0
	lastEmitted := 0
	lineNo := 0

	var fileMatches int64
	var matchedLines [][2]any // [num, line] when in count/list mode

	emitFileHeaderIfFirst := false
	if s.grouped && !s.opts.count && !s.opts.listMatch && !s.opts.listNoMatch {
		emitFileHeaderIfFirst = true
	}

	emit := func(num int, line []byte, sep byte) {
		if emitFileHeaderIfFirst {
			s.out.WriteString(path)
			s.out.WriteByte('\n')
			emitFileHeaderIfFirst = false
		}
		if !s.grouped {
			showFile := !s.opts.noFilename
			if s.opts.withFile {
				showFile = true
			}
			if showFile {
				s.out.WriteString(path)
				s.out.WriteByte(':')
			}
		}
		s.out.WriteString(strconv.Itoa(num))
		s.out.WriteByte(sep)
		s.out.Write(line)
		if len(line) == 0 || line[len(line)-1] != '\n' {
			s.out.WriteByte('\n')
		}
	}

	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			lineNo++
			lineNoNL := bytes.TrimRight(line, "\n")
			isMatch := s.re.Match(lineNoNL)
			if s.opts.invert {
				isMatch = !isMatch
			}
			if isMatch {
				s.matched = true
				fileMatches++
				if !s.opts.count && !s.opts.listMatch && !s.opts.listNoMatch {
					if s.opts.before > 0 {
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
					pendingAfter = s.opts.after
				}
			} else if pendingAfter > 0 && !s.opts.count && !s.opts.listMatch && !s.opts.listNoMatch {
				emit(lineNo, line, '-')
				lastEmitted = lineNo
				pendingAfter--
			} else if s.opts.before > 0 {
				if len(ring) == cap(ring) && cap(ring) > 0 {
					ring = ring[1:]
				}
				cp := make([]byte, len(line))
				copy(cp, line)
				ring = append(ring, ringItem{num: lineNo, line: cp})
			}
		}
		if err != nil {
			break
		}
	}
	_ = matchedLines

	if s.opts.count && fileMatches > 0 {
		if !s.opts.noFilename {
			s.out.WriteString(path)
			s.out.WriteByte(':')
		}
		s.out.WriteString(strconv.FormatInt(fileMatches, 10))
		s.out.WriteByte('\n')
	}
	if s.opts.listMatch && fileMatches > 0 {
		s.out.WriteString(path)
		if s.opts.null {
			s.out.WriteByte(0)
		} else {
			s.out.WriteByte('\n')
		}
	}
	if s.opts.listNoMatch && fileMatches == 0 {
		s.out.WriteString(path)
		if s.opts.null {
			s.out.WriteByte(0)
		} else {
			s.out.WriteByte('\n')
		}
	}
	// Trailing blank line between files in grouped mode.
	if s.grouped && fileMatches > 0 && !s.opts.count && !s.opts.listMatch && !s.opts.listNoMatch {
		s.out.WriteByte('\n')
	}
}

// isatty reports whether f is connected to a terminal. Best-effort: we
// use the Mode bits, which work on Unix.
func isatty(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func parseArgs(args []string) (*options, error) {
	o := &options{}
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "" || (a[0] != '-') || a == "-" {
			o.consumePos(a)
			i++
			continue
		}
		if a == "--" {
			for _, x := range args[i+1:] {
				o.consumePos(x)
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
			case 'i':
				o.ignoreCase = true
				j++
			case 's':
				o.caseSensitive = true
				j++
			case 'Q':
				o.literal = true
				j++
			case 'w':
				o.wordRegexp = true
				j++
			case 'v':
				o.invert = true
				j++
			case 'l':
				o.listMatch = true
				j++
			case 'L':
				o.listNoMatch = true
				j++
			case 'c':
				o.count = true
				j++
			case 'H':
				o.withFile = true
				j++
			case '0':
				o.null = true
				j++
			case 'U':
				o.noIgnore = true
				j++
			case 'a':
				o.allTypes = true
				j++
			case 'A', 'B', 'C':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, fmt.Errorf("-%c requires an argument", c)
				}
				n, err := strconv.Atoi(arg)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("invalid -%c %q", c, arg)
				}
				switch c {
				case 'A':
					o.after = n
				case 'B':
					o.before = n
				case 'C':
					o.after = n
					o.before = n
				}
				j = len(a)
			case 'G':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, errors.New("-G requires an argument")
				}
				o.fileRegex = arg
				j = len(a)
			case 'e':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, errors.New("-e requires an argument")
				}
				o.pattern = arg
				o.patternFromE = true
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

func (o *options) consumePos(a string) {
	if o.pattern == "" && !o.patternFromE {
		o.pattern = a
		return
	}
	o.roots = append(o.roots, a)
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
	case "case-sensitive":
		o.caseSensitive = true
	case "literal":
		o.literal = true
	case "word-regexp":
		o.wordRegexp = true
	case "invert-match":
		o.invert = true
	case "files-with-matches":
		o.listMatch = true
	case "files-without-matches":
		o.listNoMatch = true
	case "count":
		o.count = true
	case "filename":
		o.withFile = true
	case "no-filename":
		o.noFilename = true
	case "hidden":
		o.hidden = true
	case "no-ignore":
		o.noIgnore = true
	case "all-types":
		o.allTypes = true
	case "null":
		o.null = true
	case "after":
		v, err := next()
		if err != nil {
			return err
		}
		n, _ := strconv.Atoi(v)
		o.after = n
	case "before":
		v, err := next()
		if err != nil {
			return err
		}
		n, _ := strconv.Atoi(v)
		o.before = n
	case "context":
		v, err := next()
		if err != nil {
			return err
		}
		n, _ := strconv.Atoi(v)
		o.after = n
		o.before = n
	case "depth":
		v, err := next()
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return err
		}
		o.depth = n
	case "file-search-regex":
		v, err := next()
		if err != nil {
			return err
		}
		o.fileRegex = v
	case "ignore":
		v, err := next()
		if err != nil {
			return err
		}
		o.addIgnore = append(o.addIgnore, v)
	case "group":
		yes := true
		o.groupForce = &yes
	case "nogroup":
		no := false
		o.groupForce = &no
	case "help":
		o.printHelp = true
	case "version":
		o.printVersion = true
	default:
		return fmt.Errorf("unrecognized option --%s", name)
	}
	return nil
}

func printHelp(w io.Writer) {
	const help = `Usage: ag [OPTIONS] PATTERN [PATH...]
Recursive code-search with sane defaults (RE2 backend).

Search behaviour:
  -i, --ignore-case
  -s, --case-sensitive
  -Q, --literal             treat PATTERN as a literal string
  -w, --word-regexp
  -v, --invert-match
  -A NUM, --after=NUM
  -B NUM, --before=NUM
  -C NUM, --context=NUM
  -e PATTERN

Output:
  -l, --files-with-matches
  -L, --files-without-matches
  -c, --count
  -H, --filename
      --no-filename
      --group               force file-grouped output
      --nogroup             disable grouped output
  -0, --null

File selection:
  -G, --file-search-regex=PAT
      --ignore=PATTERN      add an ignore pattern (repeatable)
  -U, --no-ignore           don't honor .gitignore / .ignore
      --hidden              search hidden files / dirs
  -a, --all-types           search binary files too
      --depth=N             max recursion depth

      --help
      --version

PATTERNS use Go's RE2 engine. PCRE features (backrefs, lookaround) are
not supported.
`
	io.WriteString(w, help)
}

// Avoid unused-import quibble if helpers move around.
var _ = fs.SkipDir
