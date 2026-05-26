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
	"unicode/utf8"
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
	op    byte    // 's', 'd', 'p', 'q', 'y', '{'
	addr1 address
	addr2 address // zero kind = single
	negate bool

	// substitution-specific:
	subRE     *regexp.Regexp
	subRepl   string
	subFlags  subFlags

	// y-specific: a rune-to-rune translation map.
	yMap map[rune]rune

	// block-specific ('{'): contained commands executed when the
	// address(es) match.
	children []command
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

	cmds, err := parseScripts(o.scripts, o.extended)
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

	// In-place output goes to a temp file, then renamed. We capture the
	// original file's mode so the rename preserves it (rather than
	// silently downgrading 0600 secrets to 0644 etc.).
	var out *bufio.Writer
	var tmpPath string
	var origPath string
	var origMode os.FileMode = 0o644
	cleanup := func() {}
	if o.inPlace && name != "-" {
		if info, err := os.Stat(name); err == nil {
			origMode = info.Mode().Perm()
		}
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

	st := &state{rangeOpen: make([]bool, countAllCmds(cmds))}
	total := len(all)
	for i, line := range all {
		ln := i + 1
		isLast := ln == total
		ps := line // pattern space; commands may modify
		printDefault := !o.suppress
		idxNext := 0
		ps2, deleted, quit := runCmds(cmds, ps, ln, isLast, st, &idxNext, out)
		ps = ps2
		// q quits after still emitting the current pattern space (matching
		// GNU sed). Auto-print fires unless -n or `d` suppressed it.
		if !deleted && printDefault {
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
		// Preserve the original file's mode by chmod'ing the temp before
		// rename. Rename is atomic on the same filesystem; doing the
		// chmod first means there's no window where the file has the
		// wrong mode.
		if err := os.Chmod(tmpPath, origMode); err != nil {
			return err
		}
		if o.backupExt != "" {
			if err := os.Rename(origPath, origPath+o.backupExt); err != nil {
				return err
			}
		}
		if err := os.Rename(tmpPath, origPath); err != nil {
			return err
		}
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

// countAllCmds reports the total number of commands across the
// (potentially nested) tree, so the range-state vector is large
// enough to hold one slot per command. Each top-level command and
// each child gets its own slot, assigned in pre-order.
func countAllCmds(cmds []command) int {
	n := 0
	for _, c := range cmds {
		n++
		if c.op == '{' {
			n += countAllCmds(c.children)
		}
	}
	return n
}

// runCmds executes a list of commands on ps. It threads a monotonically
// increasing command index (used as the key into st.rangeOpen) so each
// nested command has a stable slot for address-range state. Returns the
// (possibly mutated) pattern space and the deleted/quit signals.
func runCmds(cmds []command, ps string, ln int, isLast bool, st *state, idxNext *int, out *bufio.Writer) (string, bool, bool) {
	deleted := false
	quit := false
	for _, c := range cmds {
		ci := *idxNext
		*idxNext++
		if !addressMatches(c, ln, isLast, ps, ci, st) {
			// Even when the address skips this command, we still need
			// to advance over any nested children so subsequent
			// commands keep their stable indices.
			if c.op == '{' {
				skipCmds(c.children, idxNext)
			}
			continue
		}
		switch c.op {
		case 's':
			var didMatch bool
			ps2, m := applySub(ps, c)
			ps, didMatch = ps2, m
			if c.subFlags.print && didMatch {
				out.WriteString(ps)
				out.WriteByte('\n')
			}
		case 'y':
			ps = applyY(ps, c.yMap)
		case 'd':
			deleted = true
		case 'p':
			out.WriteString(ps)
			out.WriteByte('\n')
		case 'q':
			quit = true
		case '{':
			var d, q bool
			ps, d, q = runCmds(c.children, ps, ln, isLast, st, idxNext, out)
			if d {
				deleted = true
			}
			if q {
				quit = true
			}
		}
		if deleted || quit {
			// Drain remaining index slots so any sibling commands
			// that don't run still own the correct future slots.
			break
		}
	}
	return ps, deleted, quit
}

// skipCmds advances *idxNext past a block's children without
// executing them.
func skipCmds(cmds []command, idxNext *int) {
	for _, c := range cmds {
		*idxNext++
		if c.op == '{' {
			skipCmds(c.children, idxNext)
		}
	}
}

// applyY rewrites line by translating every rune via m. Runes not in m
// are passed through unchanged.
func applyY(line string, m map[rune]rune) string {
	if m == nil {
		return line
	}
	var b strings.Builder
	b.Grow(len(line))
	for _, r := range line {
		if t, ok := m[r]; ok {
			b.WriteRune(t)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// applySub returns the (possibly rewritten) line and whether any
// substitution was actually performed. The caller needs the latter so
// `s///p` only prints when there was a real match (matching GNU sed).
func applySub(line string, c command) (string, bool) {
	if c.subRE == nil {
		return line, false
	}
	if c.subFlags.global {
		matched := false
		out := c.subRE.ReplaceAllStringFunc(line, func(m string) string {
			matched = true
			return expandReplacement(c.subRepl, c.subRE, m)
		})
		return out, matched
	}
	if c.subFlags.nth > 0 {
		n := c.subFlags.nth
		count := 0
		matched := false
		out := c.subRE.ReplaceAllStringFunc(line, func(m string) string {
			count++
			if count != n {
				return m
			}
			matched = true
			return expandReplacement(c.subRepl, c.subRE, m)
		})
		return out, matched
	}
	// First match only.
	loc := c.subRE.FindStringSubmatchIndex(line)
	if loc == nil {
		return line, false
	}
	return line[:loc[0]] + expandReplacement(c.subRepl, c.subRE, line[loc[0]:loc[1]]) + line[loc[1]:], true
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
func parseScripts(scripts []string, extended bool) ([]command, error) {
	var out []command
	for _, s := range scripts {
		// Split on newline, but s/// can't contain raw newlines anyway.
		for _, line := range splitTopLevel(s) {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			c, err := parseCommand(line, extended)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
	}
	return out, nil
}

// splitTopLevel splits on ; and newline at the top level (depth 0),
// but skips delimiters that appear inside an s/// body, a y/// body,
// or a `{ ... }` block. Returns the list of top-level commands.
func splitTopLevel(s string) []string {
	var out []string
	var buf strings.Builder
	braceDepth := 0
	i := 0
	for i < len(s) {
		c := s[i]
		// Top-level separator only at depth 0.
		if braceDepth == 0 && (c == ';' || c == '\n') {
			out = append(out, buf.String())
			buf.Reset()
			i++
			continue
		}
		// `{` and `}` adjust depth. We keep them in the buffer so the
		// surrounding command body contains the whole block; parseCommand
		// recurses into it.
		if c == '{' {
			braceDepth++
			buf.WriteByte(c)
			i++
			continue
		}
		if c == '}' {
			if braceDepth > 0 {
				braceDepth--
			}
			buf.WriteByte(c)
			i++
			// A `}` at the new depth 0 terminates the enclosing block;
			// flush the buffer as a complete command.
			if braceDepth == 0 {
				out = append(out, buf.String())
				buf.Reset()
			}
			continue
		}
		// If we hit a 's' or 'y' command at top level, scan until we've
		// seen 3 unescaped occurrences of the delimiter.
		if (c == 's' || c == 'y') && looksLikeSubAt(s, i) {
			delim := s[i+1]
			buf.WriteByte(c)
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
			// After the 3rd delimiter, consume flags until a separator
			// (or a brace at depth 0).
			for i < len(s) && s[i] != ';' && s[i] != '\n' && s[i] != '}' {
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
func parseCommand(s string, extended bool) (command, error) {
	var c command
	s = strings.TrimSpace(s)
	if s == "" {
		return c, errors.New("empty command")
	}
	rest, a1, err := parseAddress(s, extended)
	if err != nil {
		return c, err
	}
	c.addr1 = a1
	if strings.HasPrefix(rest, ",") {
		rest, a2, err := parseAddress(rest[1:], extended)
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
		if err := parseSub(&c, body, extended); err != nil {
			return c, err
		}
	case 'y':
		if err := parseY(&c, body); err != nil {
			return c, err
		}
	case '{':
		// Block: parse the contained commands. The trailing `}` was
		// preserved by splitTopLevel; strip it before recursing.
		inner := strings.TrimSpace(body)
		if !strings.HasSuffix(inner, "}") {
			return c, errors.New("missing `}` to close block")
		}
		inner = strings.TrimSpace(inner[:len(inner)-1])
		children, err := parseScripts([]string{inner}, extended)
		if err != nil {
			return c, err
		}
		c.children = children
	case 'd', 'p', 'q':
		// no-op (or trailing whitespace)
	default:
		return c, fmt.Errorf("unsupported command %q (only s, y, d, p, q, { } implemented)", string(c.op))
	}
	return c, nil
}

// parseY parses y/SRC/DST/ — a per-character transliteration table.
// SRC and DST must have the same rune count. Backslash escapes the
// delimiter and \n / \t / \\ inside either field.
func parseY(c *command, body string) error {
	if body == "" {
		return errors.New("empty y command")
	}
	delim := body[0]
	rest := body[1:]
	src, rest, ok := scanSubField(rest, delim)
	if !ok {
		return errors.New("unterminated y src")
	}
	dst, _, ok := scanSubField(rest, delim)
	if !ok {
		return errors.New("unterminated y dst")
	}
	srcRunes := decodeYField(src)
	dstRunes := decodeYField(dst)
	if len(srcRunes) != len(dstRunes) {
		return fmt.Errorf("y: src/dst length mismatch (%d vs %d)", len(srcRunes), len(dstRunes))
	}
	m := make(map[rune]rune, len(srcRunes))
	for i, r := range srcRunes {
		m[r] = dstRunes[i]
	}
	c.yMap = m
	return nil
}

// decodeYField handles the small set of backslash escapes (\\ \n \t)
// inside a y command field. scanSubField has already stripped the
// outer delimiter and turned \DELIM into the literal delimiter.
func decodeYField(s string) []rune {
	out := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				out = append(out, '\n')
			case 't':
				out = append(out, '\t')
			case '\\':
				out = append(out, '\\')
			default:
				out = append(out, rune(s[i+1]))
			}
			i += 2
			continue
		}
		// Decode possible multi-byte rune.
		r, size := utf8.DecodeRuneInString(s[i:])
		out = append(out, r)
		i += size
	}
	return out
}

// compileSedRE compiles a sed regex into a Go RE2 regexp. When extended
// is true the pattern is already POSIX ERE, which RE2 accepts. When
// false, the pattern is POSIX BRE: backslash-prefixed (, ), {, }, |, +,
// ? are metacharacters and their unbackslashed forms are literal. We
// translate by swapping those classes' meanings; everything else
// (character classes, anchors, *, .) is BRE/ERE-shared.
//
// Also accepts a few GNU-sed extensions that real-world scripts use:
//
//	\<  \>   word boundaries (translated to \b)
//	\`  \'   buffer start / end (translated to \A / \z)
//
// The translator does not attempt full POSIX BRE compatibility — it
// handles the common cases that show up in build scripts.
func compileSedRE(pat string, extended bool) (*regexp.Regexp, error) {
	if extended {
		return regexp.Compile(pat)
	}
	var out strings.Builder
	out.Grow(len(pat))
	inClass := false
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
			// `[]` and `[^]` allow a literal `]` as first char.
			if i+1 < len(pat) && pat[i+1] == ']' {
				out.WriteByte(']')
				i++
			} else if i+2 < len(pat) && pat[i+1] == '^' && pat[i+2] == ']' {
				out.WriteByte('^')
				out.WriteByte(']')
				i += 2
			}
			continue
		}
		if c == '\\' && i+1 < len(pat) {
			n := pat[i+1]
			switch n {
			case '(', ')', '|', '+', '?', '{', '}':
				// BRE metachars: write without the backslash.
				out.WriteByte(n)
				i++
				continue
			case '<', '>':
				// GNU word boundaries — RE2 has \b but no asymmetric
				// start/end forms. Use \b for both.
				out.WriteString(`\b`)
				i++
				continue
			case '`':
				out.WriteString(`\A`)
				i++
				continue
			case '\'':
				out.WriteString(`\z`)
				i++
				continue
			default:
				// Keep the backslash escape verbatim (covers \., \*,
				// \\, \1..\9 back-references, \w/\s/\d, etc.).
				out.WriteByte(c)
				out.WriteByte(n)
				i++
				continue
			}
		}
		switch c {
		case '(', ')', '|', '+', '?', '{', '}':
			// BRE literals: escape so RE2 doesn't treat them as
			// metacharacters.
			out.WriteByte('\\')
			out.WriteByte(c)
		default:
			out.WriteByte(c)
		}
	}
	return regexp.Compile(out.String())
}

// parseAddress parses "" (no address) | "N" | "$" | "/RE/".
// Returns the rest of the string after the address.
func parseAddress(s string, extended bool) (string, address, error) {
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
		re, err := compileSedRE(pat, extended)
		if err != nil {
			return s, address{}, err
		}
		return s[j+1:], address{kind: addrRegex, re: re}, nil
	}
	return s, address{}, nil
}

// parseSub parses /REGEX/REPL/FLAGS where the delimiter can be any single
// character (the first byte of body). Backslash escapes the delimiter.
func parseSub(c *command, body string, extended bool) error {
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
	re, err := compileSedRE(pat, extended)
	if err != nil {
		return err
	}
	if c.subFlags.icase {
		// Re-compile with case-insensitive flag applied to the
		// (already translated) pattern.
		re, err = regexp.Compile("(?i)" + re.String())
		if err != nil {
			return err
		}
	}
	c.subRE = re
	c.subRepl = repl
	return nil
}

func scanSubField(s string, delim byte) (string, string, bool) {
	var b strings.Builder
	i := 0
	// inClass tracks whether we're inside a `[...]` bracket expression in
	// the pattern. The delimiter byte is literal inside a bracket
	// expression, even when it would normally end the field. This
	// matters for sed scripts like `s:^[[:space:]]*:...:` where `:` is
	// the s/// delimiter but also part of the POSIX character class.
	inClass := false
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
		if !inClass && c == '[' {
			inClass = true
			b.WriteByte(c)
			i++
			// A `]` as the first character of a class is literal (and
			// likewise `^]` for negated classes). Copy it through so we
			// don't close the class prematurely.
			if i < len(s) && s[i] == ']' {
				b.WriteByte(']')
				i++
			} else if i+1 < len(s) && s[i] == '^' && s[i+1] == ']' {
				b.WriteByte('^')
				b.WriteByte(']')
				i += 2
			}
			continue
		}
		if inClass && c == ']' {
			inClass = false
			b.WriteByte(c)
			i++
			continue
		}
		if !inClass && c == delim {
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
