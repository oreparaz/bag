package sed

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type options struct {
	scripts []string
	files   []string

	suppress bool   // -n
	extended bool   // -E
	inPlace  bool   // -i
	backupExt string // -iEXT

	printHelp    bool
	printVersion bool
}

type addrKind int

const (
	addrNone addrKind = iota
	addrLine          // numeric line
	addrLast          // $
	addrRegex         // /RE/
)

type address struct {
	kind addrKind
	line int
	re   *regexp.Regexp
}

type command struct {
	op    byte    // 's', 'd', 'p', 'q'
	addr1 address
	addr2 address // zero kind = single
	negate bool

	// substitution-specific:
	subRE     *regexp.Regexp
	subRepl   string
	subFlags  subFlags
}

type subFlags struct {
	global  bool
	icase   bool
	print   bool
	nth     int // 0 = first match (default)
}

// state tracks per-file machinery (matched ranges, last-line detection).
type state struct {
	rangeOpen []bool // indexed by command position; true while inside addr1..addr2
}

func run(args []string) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sed: %v\n", err)
		return 1
	}
	if o.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if o.printVersion {
		fmt.Println("sed (bag) -- bag drop-in")
		return 0
	}
	if len(o.scripts) == 0 {
		fmt.Fprintln(os.Stderr, "sed: no script given")
		return 1
	}

	cmds, err := parseScripts(o.scripts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sed: %v\n", err)
		return 1
	}

	files := o.files
	if len(files) == 0 {
		files = []string{"-"}
	}

	exit := 0
	for _, f := range files {
		if err := processOne(f, cmds, o); err != nil {
			fmt.Fprintf(os.Stderr, "sed: %s: %v\n", f, err)
			exit = 1
		}
	}
	return exit
}

func processOne(name string, cmds []command, o *options) error {
	r, closer, err := openIn(name)
	if err != nil {
		return err
	}
	defer closer()

	// In-place output goes to a temp file, then renamed.
	var out *bufio.Writer
	var tmpPath string
	var origPath string
	cleanup := func() {}
	if o.inPlace && name != "-" {
		dir := "."
		if i := strings.LastIndexByte(name, '/'); i >= 0 {
			dir = name[:i]
		}
		f, err := os.CreateTemp(dir, ".sed-")
		if err != nil {
			return err
		}
		out = bufio.NewWriter(f)
		tmpPath = f.Name()
		origPath = name
		cleanup = func() {
			out.Flush()
			f.Close()
		}
	} else {
		out = bufio.NewWriter(os.Stdout)
		cleanup = func() { out.Flush() }
	}
	defer cleanup()

	// First pass: read all lines so we know which is "last" for $-addresses.
	all, err := readAllLines(r)
	if err != nil {
		return err
	}

	st := &state{rangeOpen: make([]bool, len(cmds))}
	total := len(all)
	for i, line := range all {
		ln := i + 1
		isLast := ln == total
		ps := line // pattern space; commands may modify
		printDefault := !o.suppress
		quit := false
		deleted := false
		for ci, c := range cmds {
			if !addressMatches(c, ln, isLast, ps, ci, st) {
				continue
			}
			switch c.op {
			case 's':
				ps = applySub(ps, c)
				if c.subFlags.print {
					out.WriteString(ps)
					out.WriteByte('\n')
				}
			case 'd':
				deleted = true
			case 'p':
				out.WriteString(ps)
				out.WriteByte('\n')
			case 'q':
				if printDefault && !deleted {
					out.WriteString(ps)
					out.WriteByte('\n')
				}
				quit = true
			}
			if deleted || quit {
				break
			}
		}
		if !deleted && printDefault && !quit {
			out.WriteString(ps)
			out.WriteByte('\n')
		}
		if quit {
			break
		}
	}

	// Backup + rename for in-place.
	if o.inPlace && tmpPath != "" {
		out.Flush()
		if o.backupExt != "" {
			if err := os.Rename(origPath, origPath+o.backupExt); err != nil {
				return err
			}
		}
		if err := os.Rename(tmpPath, origPath); err != nil {
			return err
		}
		_ = os.Chmod(origPath, 0o644)
	}
	return nil
}

func readAllLines(r io.Reader) ([]string, error) {
	var lines []string
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			// Strip trailing newline; we re-add on emit. Last line without
			// newline gets a synthetic newline on print, matching sed.
			line = strings.TrimSuffix(line, "\n")
			lines = append(lines, line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return lines, nil
			}
			return lines, err
		}
	}
}

func addressMatches(c command, line int, isLast bool, content string, ci int, st *state) bool {
	if c.addr1.kind == addrNone {
		return !c.negate
	}
	hit1 := matchAddr(c.addr1, line, isLast, content)
	if c.addr2.kind == addrNone {
		if c.negate {
			return !hit1
		}
		return hit1
	}
	// Range
	if !st.rangeOpen[ci] {
		if hit1 {
			st.rangeOpen[ci] = true
		}
	}
	in := st.rangeOpen[ci]
	if in && matchAddr(c.addr2, line, isLast, content) {
		st.rangeOpen[ci] = false
	}
	if c.negate {
		return !in
	}
	return in
}

func matchAddr(a address, line int, isLast bool, content string) bool {
	switch a.kind {
	case addrLine:
		return a.line == line
	case addrLast:
		return isLast
	case addrRegex:
		return a.re.MatchString(content)
	}
	return false
}

func applySub(line string, c command) string {
	if c.subRE == nil {
		return line
	}
	if c.subFlags.global {
		return c.subRE.ReplaceAllStringFunc(line, func(m string) string {
			return expandReplacement(c.subRepl, c.subRE, m)
		})
	}
	if c.subFlags.nth > 0 {
		n := c.subFlags.nth
		count := 0
		return c.subRE.ReplaceAllStringFunc(line, func(m string) string {
			count++
			if count != n {
				return m
			}
			return expandReplacement(c.subRepl, c.subRE, m)
		})
	}
	// First match only.
	loc := c.subRE.FindStringSubmatchIndex(line)
	if loc == nil {
		return line
	}
	return line[:loc[0]] + c.subRE.ReplaceAllString(line[loc[0]:loc[1]], c.subRepl) + line[loc[1]:]
}

// expandReplacement handles & and \1..\9 references.
func expandReplacement(repl string, re *regexp.Regexp, match string) string {
	subs := re.FindStringSubmatch(match)
	var b strings.Builder
	for i := 0; i < len(repl); i++ {
		c := repl[i]
		switch {
		case c == '&':
			b.WriteString(match)
		case c == '\\' && i+1 < len(repl):
			n := repl[i+1]
			switch {
			case n == '&':
				b.WriteByte('&')
			case n == '\\':
				b.WriteByte('\\')
			case n == 'n':
				b.WriteByte('\n')
			case n == 't':
				b.WriteByte('\t')
			case n >= '1' && n <= '9':
				idx := int(n - '0')
				if idx < len(subs) {
					b.WriteString(subs[idx])
				}
			default:
				b.WriteByte(n)
			}
			i++
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// parseScripts joins multiple -e scripts, splits on `;` and newline at the
// top level, and parses each part into a command.
func parseScripts(scripts []string) ([]command, error) {
	var out []command
	for _, s := range scripts {
		// Split on newline, but s/// can't contain raw newlines anyway.
		for _, line := range splitTopLevel(s) {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			c, err := parseCommand(line)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
	}
	return out, nil
}

// splitTopLevel splits on ; and newline but skips delimiters that appear
// inside an s/// body.
func splitTopLevel(s string) []string {
	var out []string
	var buf strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		if c == ';' || c == '\n' {
			out = append(out, buf.String())
			buf.Reset()
			i++
			continue
		}
		// If we hit a 's' command, scan until we've seen 3 unescaped
		// occurrences of the delimiter (the substitute's delimiter is
		// the char immediately after 's').
		if (c == 's') && (looksLikeSubAt(s, i)) {
			delim := s[i+1]
			buf.WriteByte('s')
			i++
			delims := 0
			for i < len(s) && delims < 3 {
				cc := s[i]
				if cc == '\\' && i+1 < len(s) {
					buf.WriteByte(cc)
					buf.WriteByte(s[i+1])
					i += 2
					continue
				}
				if cc == delim {
					delims++
				}
				buf.WriteByte(cc)
				i++
			}
			// After the 3rd delimiter, consume flags until a separator.
			for i < len(s) && s[i] != ';' && s[i] != '\n' {
				buf.WriteByte(s[i])
				i++
			}
			continue
		}
		buf.WriteByte(c)
		i++
	}
	if buf.Len() > 0 {
		out = append(out, buf.String())
	}
	return out
}

// looksLikeSubAt reports whether the 's' at s[i] starts a substitute
// command. Heuristic: previous chars are addresses or whitespace, and
// the next char is a non-alphanumeric delimiter.
func looksLikeSubAt(s string, i int) bool {
	if i+1 >= len(s) {
		return false
	}
	d := s[i+1]
	if d == 0 || d == ' ' || (d >= '0' && d <= '9') || (d >= 'a' && d <= 'z') || (d >= 'A' && d <= 'Z') {
		return false
	}
	// Look at preceding non-space char: if it's a letter that could be a
	// command, this 's' isn't the start of one.
	j := i - 1
	for j >= 0 && (s[j] == ' ' || s[j] == '\t') {
		j--
	}
	if j >= 0 {
		c := s[j]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

// parseCommand handles "[addr1[,addr2]] [!] CMD ARGS"
func parseCommand(s string) (command, error) {
	var c command
	s = strings.TrimSpace(s)
	if s == "" {
		return c, errors.New("empty command")
	}
	rest, a1, err := parseAddress(s)
	if err != nil {
		return c, err
	}
	c.addr1 = a1
	if strings.HasPrefix(rest, ",") {
		rest, a2, err := parseAddress(rest[1:])
		if err != nil {
			return c, err
		}
		c.addr2 = a2
		s = rest
	} else {
		s = rest
	}
	s = strings.TrimLeft(s, " \t")
	if strings.HasPrefix(s, "!") {
		c.negate = true
		s = strings.TrimLeft(s[1:], " \t")
	}
	if s == "" {
		return c, errors.New("missing command")
	}
	c.op = s[0]
	body := s[1:]
	switch c.op {
	case 's':
		if err := parseSub(&c, body); err != nil {
			return c, err
		}
	case 'd', 'p', 'q':
		// no-op (or trailing whitespace)
	default:
		return c, fmt.Errorf("unsupported command %q (only s, d, p, q implemented)", string(c.op))
	}
	return c, nil
}

// parseAddress parses "" (no address) | "N" | "$" | "/RE/".
// Returns the rest of the string after the address.
func parseAddress(s string) (string, address, error) {
	s = strings.TrimLeft(s, " \t")
	if s == "" {
		return s, address{}, nil
	}
	if s[0] == '$' {
		return s[1:], address{kind: addrLast}, nil
	}
	if s[0] >= '0' && s[0] <= '9' {
		j := 0
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		n, err := strconv.Atoi(s[:j])
		if err != nil {
			return s, address{}, err
		}
		return s[j:], address{kind: addrLine, line: n}, nil
	}
	if s[0] == '/' {
		// Find unescaped closing slash.
		j := 1
		for j < len(s) {
			if s[j] == '\\' && j+1 < len(s) {
				j += 2
				continue
			}
			if s[j] == '/' {
				break
			}
			j++
		}
		if j >= len(s) {
			return s, address{}, errors.New("unterminated regex address")
		}
		pat := s[1:j]
		re, err := regexp.Compile(pat)
		if err != nil {
			return s, address{}, err
		}
		return s[j+1:], address{kind: addrRegex, re: re}, nil
	}
	return s, address{}, nil
}

// parseSub parses /REGEX/REPL/FLAGS where the delimiter can be any single
// character (the first byte of body). Backslash escapes the delimiter.
func parseSub(c *command, body string) error {
	if body == "" {
		return errors.New("empty s command")
	}
	delim := body[0]
	rest := body[1:]
	pat, rest, ok := scanSubField(rest, delim)
	if !ok {
		return errors.New("unterminated s pattern")
	}
	repl, rest, ok := scanSubField(rest, delim)
	if !ok {
		return errors.New("unterminated s replacement")
	}
	flags := rest
	for _, f := range flags {
		switch {
		case f == 'g':
			c.subFlags.global = true
		case f == 'i', f == 'I':
			c.subFlags.icase = true
		case f == 'p':
			c.subFlags.print = true
		case f >= '0' && f <= '9':
			c.subFlags.nth = c.subFlags.nth*10 + int(f-'0')
		case f == ' ' || f == '\t':
			// ignore
		default:
			return fmt.Errorf("unsupported s flag %q", string(f))
		}
	}
	if c.subFlags.icase {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return err
	}
	c.subRE = re
	c.subRepl = repl
	return nil
}

func scanSubField(s string, delim byte) (string, string, bool) {
	var b strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			n := s[i+1]
			if n == delim {
				b.WriteByte(delim)
			} else {
				// Preserve backslash for replacement processing.
				b.WriteByte('\\')
				b.WriteByte(n)
			}
			i += 2
			continue
		}
		if c == delim {
			return b.String(), s[i+1:], true
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), "", false
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

func parseArgs(args []string) (*options, error) {
	o := &options{}
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "" || a == "-" || a[0] != '-' {
			o.consumePos(a)
			i++
			continue
		}
		if a == "--" {
			for _, f := range args[i+1:] {
				o.consumePos(f)
			}
			break
		}
		if strings.HasPrefix(a, "--") {
			switch a[2:] {
			case "quiet", "silent":
				o.suppress = true
			case "extended-regexp", "regexp-extended":
				o.extended = true
			case "in-place":
				o.inPlace = true
			case "help":
				o.printHelp = true
			case "version":
				o.printVersion = true
			case "expression":
				if i+1 >= len(args) {
					return nil, errors.New("--expression requires an argument")
				}
				i++
				o.scripts = append(o.scripts, args[i])
			case "file":
				if i+1 >= len(args) {
					return nil, errors.New("--file requires an argument")
				}
				i++
				body, err := os.ReadFile(args[i])
				if err != nil {
					return nil, err
				}
				o.scripts = append(o.scripts, string(body))
			default:
				return nil, fmt.Errorf("unrecognized option '%s'", a)
			}
			i++
			continue
		}
		// Short cluster.
		j := 1
		for j < len(a) {
			c := a[j]
			switch c {
			case 'n':
				o.suppress = true
				j++
			case 'E', 'r':
				o.extended = true
				j++
			case 'i':
				o.inPlace = true
				if j+1 < len(a) {
					o.backupExt = a[j+1:]
					j = len(a)
				} else {
					j++
				}
			case 'e':
				arg, ok := pickArg(a[j+1:], &i, args)
				if !ok {
					return nil, errors.New("-e requires an argument")
				}
				o.scripts = append(o.scripts, arg)
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
				o.scripts = append(o.scripts, string(body))
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
	if len(o.scripts) == 0 {
		o.scripts = append(o.scripts, a)
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

func printHelp(w io.Writer) {
	const help = `Usage: sed [OPTION]... {script-only-if-no-other-script} [input-file]...
A small subset of GNU sed.

  -n, --quiet, --silent     suppress automatic printing of pattern space
  -e SCRIPT                 add SCRIPT to the program (repeatable)
  -f FILE                   read program from FILE
  -E, -r                    extended regular expressions (default: RE2)
  -i[SUFFIX]                edit files in place (with optional backup suffix)
      --help                display this help and exit
      --version             output version information and exit

Supported commands: s, d, p, q  (with optional address(es))
`
	io.WriteString(w, help)
}
