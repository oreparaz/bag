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
	op    byte    // 's', 'd', 'p', 'q', 'y', '{', ':', 'b', 't', 'T',
	//                'n', 'N', 'D', 'P', 'h', 'H', 'g', 'G', 'x', '='
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

	// ':', 'b', 't', 'T': label name (target for branches, definition for ':').
	label string
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

	prog, err := compileProgram(cmds)
	if err != nil {
		return err
	}
	rs := &runner{
		prog:         prog,
		lines:        all,
		out:          out,
		printDefault: !o.suppress,
		rangeOpen:    make([]bool, len(prog.insns)),
	}
	rs.runAll()

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


func matchAddr(a address, line int, isLast bool, content string, lastRE *regexp.Regexp) bool {
	switch a.kind {
	case addrLine:
		return a.line == line
	case addrLast:
		return isLast
	case addrRegex:
		re := a.re
		if re == nil {
			// `//` — fall back to most recent regex per GNU sed.
			re = lastRE
			if re == nil {
				return false
			}
		}
		return re.MatchString(content)
	}
	return false
}

// countAllCmds reports the total number of commands across the
// (potentially nested) tree, so the range-state vector is large
// enough to hold one slot per command. Each top-level command and
// each child gets its own slot, assigned in pre-order.
// instruction is a single executable step in the compiled program.
type instruction struct {
	cmd    command
	target int // for b/t/T: index of target label, or -1 to branch to end
	skipTo int // for '{': index past the matching block end
}

type program struct {
	insns []instruction
}

// compileProgram flattens the AST into a linear instruction list with
// labels resolved to PC indices and `{}` blocks resolved to a skip-target.
// This makes branching (b/t/T) and block-skipping a single PC assignment
// during execution.
func compileProgram(cmds []command) (*program, error) {
	p := &program{}
	labels := map[string]int{}
	type pending struct {
		from  int
		label string
	}
	var pendings []pending

	var walk func(cs []command) error
	walk = func(cs []command) error {
		for _, c := range cs {
			ix := len(p.insns)
			p.insns = append(p.insns, instruction{cmd: c, target: -1, skipTo: -1})
			switch c.op {
			case '{':
				if err := walk(c.children); err != nil {
					return err
				}
				p.insns[ix].skipTo = len(p.insns)
			case ':':
				if _, dup := labels[c.label]; dup {
					return fmt.Errorf("sed: duplicate label %q", c.label)
				}
				labels[c.label] = ix
			case 'b', 't', 'T':
				if c.label != "" {
					pendings = append(pendings, pending{from: ix, label: c.label})
				}
				// empty label keeps target == -1 (branch to end of script).
			}
		}
		return nil
	}
	if err := walk(cmds); err != nil {
		return nil, err
	}
	for _, pn := range pendings {
		tgt, ok := labels[pn.label]
		if !ok {
			return nil, fmt.Errorf("sed: undefined label %q", pn.label)
		}
		p.insns[pn.from].target = tgt
	}
	return p, nil
}

// runner executes a compiled program over a sequence of input lines,
// supporting GNU sed's pattern/hold spaces and branching.
type runner struct {
	prog  *program
	lines []string

	// lineIx is the index of the NEXT line to read into pattern space.
	lineIx int

	// pattern space + hold space.
	ps string
	hs string

	// subSuccess tracks whether the last `s` command on the current cycle
	// produced a substitution (cleared by reading input or by `t`/`T`).
	subSuccess bool

	// lastRE is the most recently used (address or `s`) regex. An empty
	// pattern in `s` or `/RE/` falls back to this — matches GNU sed.
	lastRE *regexp.Regexp

	out          *bufio.Writer
	printDefault bool

	// rangeOpen tracks whether each command's addr1..addr2 range is open.
	rangeOpen []bool

	// per-cycle:
	lineNo  int
	isLast  bool
	deleted bool
	quit    bool
	// appended is the text queued by `a` commands for emission AFTER
	// auto-print at end of cycle.
	appended []string
}

// readLine pulls the next line into the pattern space. Returns false if
// input was exhausted.
func (r *runner) readLine() bool {
	if r.lineIx >= len(r.lines) {
		return false
	}
	r.ps = r.lines[r.lineIx]
	r.lineIx++
	r.lineNo = r.lineIx
	r.isLast = r.lineIx >= len(r.lines)
	r.subSuccess = false
	return true
}

// runAll drives the input loop: read a line, execute the program, auto-
// print, flush any queued `a` text, repeat.
func (r *runner) runAll() {
	for !r.quit && r.readLine() {
		r.deleted = false
		r.appended = r.appended[:0]
		r.execProgram()
		if !r.deleted && r.printDefault {
			r.out.WriteString(r.ps)
			r.out.WriteByte('\n')
		}
		for _, t := range r.appended {
			r.out.WriteString(t)
			r.out.WriteByte('\n')
		}
	}
}

// execProgram runs the program once over the current pattern space.
func (r *runner) execProgram() {
	pc := 0
	for pc < len(r.prog.insns) {
		ins := &r.prog.insns[pc]
		c := ins.cmd
		if !r.addressMatchesIdx(c, pc) {
			if c.op == '{' {
				pc = ins.skipTo
				continue
			}
			pc++
			continue
		}
		switch c.op {
		case 's':
			re := c.subRE
			if re == nil {
				re = r.lastRE
			}
			ps2, m := applySub(r.ps, c, re)
			r.ps = ps2
			if m {
				r.subSuccess = true
				r.noteRE(re)
				if c.subFlags.print {
					r.out.WriteString(r.ps)
					r.out.WriteByte('\n')
				}
			}
		case 'y':
			r.ps = applyY(r.ps, c.yMap)
		case 'd':
			r.deleted = true
			return
		case 'D':
			i := strings.IndexByte(r.ps, '\n')
			if i < 0 {
				r.deleted = true
				return
			}
			r.ps = r.ps[i+1:]
			r.subSuccess = false
			pc = 0
			continue
		case 'p':
			r.out.WriteString(r.ps)
			r.out.WriteByte('\n')
		case 'P':
			i := strings.IndexByte(r.ps, '\n')
			if i < 0 {
				r.out.WriteString(r.ps)
			} else {
				r.out.WriteString(r.ps[:i])
			}
			r.out.WriteByte('\n')
		case 'q':
			r.quit = true
			return
		case 'n':
			// Print current ps (unless -n), then read next line.
			if r.printDefault && !r.deleted {
				r.out.WriteString(r.ps)
				r.out.WriteByte('\n')
			}
			if !r.readLine() {
				r.quit = true
				return
			}
		case 'N':
			if r.lineIx >= len(r.lines) {
				// GNU sed: no more input — exit normal cycle (auto-print
				// fires from runAll).
				return
			}
			r.ps += "\n" + r.lines[r.lineIx]
			r.lineIx++
			r.lineNo = r.lineIx
			r.isLast = r.lineIx >= len(r.lines)
		case 'h':
			r.hs = r.ps
		case 'H':
			if r.hs == "" {
				r.hs = "\n" + r.ps
			} else {
				r.hs = r.hs + "\n" + r.ps
			}
		case 'g':
			r.ps = r.hs
		case 'G':
			if r.ps == "" {
				r.ps = "\n" + r.hs
			} else {
				r.ps = r.ps + "\n" + r.hs
			}
		case 'x':
			r.ps, r.hs = r.hs, r.ps
		case '=':
			fmt.Fprintln(r.out, r.lineNo)
		case 'a':
			r.appended = append(r.appended, c.label)
		case 'r':
			// Read FILE and append its contents after auto-print.
			data, err := os.ReadFile(c.label)
			if err == nil {
				s := string(data)
				// Drop a single trailing \n so the appended block
				// doesn't add an extra blank line — GNU sed behavior.
				s = strings.TrimSuffix(s, "\n")
				r.appended = append(r.appended, s)
			}
			// Missing file is silently ignored, per GNU sed.
		case 'i':
			r.out.WriteString(c.label)
			r.out.WriteByte('\n')
		case 'c':
			// Change: delete pattern space, output text at end of cycle
			// (only on the last line of a matched range). Simplified
			// implementation: output the text on every matching line and
			// delete the pattern space. (Matches GNU when no range.)
			r.appended = append(r.appended, c.label)
			r.deleted = true
			return
		case ':':
			// no-op: label marker.
		case 'b':
			if ins.target < 0 {
				return
			}
			pc = ins.target + 1
			continue
		case 't':
			if r.subSuccess {
				r.subSuccess = false
				if ins.target < 0 {
					return
				}
				pc = ins.target + 1
				continue
			}
		case 'T':
			if !r.subSuccess {
				if ins.target < 0 {
					return
				}
				pc = ins.target + 1
				continue
			}
			r.subSuccess = false
		case '{':
			// Block-start: address matched; fall through into children.
		}
		pc++
	}
}

// addressMatchesIdx is the address gate using a flat PC index for range
// state. Threaded with lastRE so empty `//` addresses can fall back to
// the most recent regex.
func (r *runner) addressMatchesIdx(c command, pc int) bool {
	matched := r.addrMatchesFlat(c, pc)
	if c.negate {
		return !matched
	}
	return matched
}

func (r *runner) addrMatchesFlat(c command, pc int) bool {
	// No address → always matches.
	if c.addr1.kind == addrNone {
		return true
	}
	if c.addr2.kind == addrNone {
		hit := matchAddr(c.addr1, r.lineNo, r.isLast, r.ps, r.lastRE)
		if hit && c.addr1.kind == addrRegex {
			r.noteRE(c.addr1.re)
		}
		return hit
	}
	// Range address: open on first match, close on second.
	if r.rangeOpen[pc] {
		if matchAddr(c.addr2, r.lineNo, r.isLast, r.ps, r.lastRE) {
			r.rangeOpen[pc] = false
			if c.addr2.kind == addrRegex {
				r.noteRE(c.addr2.re)
			}
		}
		return true
	}
	if matchAddr(c.addr1, r.lineNo, r.isLast, r.ps, r.lastRE) {
		if c.addr1.kind == addrRegex {
			r.noteRE(c.addr1.re)
		}
		// If the close address is a number ≤ current line, this is a
		// single-line range (matches GNU sed).
		if c.addr2.kind == addrLine && c.addr2.line <= r.lineNo {
			return true
		}
		r.rangeOpen[pc] = true
		return true
	}
	return false
}

// noteRE records a matched regex as the "last regex" so empty `//` and
// `s//.../` references can reuse it.
func (r *runner) noteRE(re *regexp.Regexp) {
	if re != nil {
		r.lastRE = re
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
//
// re is the regex to use — caller resolves a nil c.subRE to the runner's
// lastRE before calling.
func applySub(line string, c command, re *regexp.Regexp) (string, bool) {
	if re == nil {
		return line, false
	}
	if c.subFlags.global {
		matched := false
		out := re.ReplaceAllStringFunc(line, func(m string) string {
			matched = true
			return expandReplacement(c.subRepl, re, m)
		})
		return out, matched
	}
	if c.subFlags.nth > 0 {
		n := c.subFlags.nth
		count := 0
		matched := false
		out := re.ReplaceAllStringFunc(line, func(m string) string {
			count++
			if count != n {
				return m
			}
			matched = true
			return expandReplacement(c.subRepl, re, m)
		})
		return out, matched
	}
	// First match only.
	loc := re.FindStringSubmatchIndex(line)
	if loc == nil {
		return line, false
	}
	return line[:loc[0]] + expandReplacement(c.subRepl, re, line[loc[0]:loc[1]]) + line[loc[1]:], true
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

// parseScripts joins multiple -e scripts (with a newline between each,
// matching GNU sed), splits the result on `;` and newline at the top
// level, and parses each command. Joining matters because block syntax
// like `-e '1{' -e 'cmd' -e '}'` spans multiple -e arguments.
func parseScripts(scripts []string, extended bool) ([]command, error) {
	joined := strings.Join(scripts, "\n")
	var out []command
	for _, line := range splitTopLevel(joined) {
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
	return out, nil
}

// splitTopLevel splits on ; and newline at the top level (depth 0),
// but skips delimiters that appear inside an s/// body, a y/// body,
// a `/regex/` address, or a `{ ... }` block. Returns the list of
// top-level commands.
func splitTopLevel(s string) []string {
	var out []string
	var buf strings.Builder
	braceDepth := 0
	i := 0
	// atCmdStart tracks whether the next non-whitespace byte begins a new
	// command (so a `/` there starts an address rather than being a stray
	// character).
	atCmdStart := true
	for i < len(s) {
		c := s[i]
		// Top-level separator only at depth 0.
		if braceDepth == 0 && (c == ';' || c == '\n') {
			out = append(out, buf.String())
			buf.Reset()
			i++
			atCmdStart = true
			continue
		}
		// Skip leading whitespace without changing the at-cmd-start flag.
		if (c == ' ' || c == '\t') && buf.Len() == 0 {
			i++
			continue
		}
		// Regex address `/RE/` (or `\cREc`) at command start — scan past
		// the closing delimiter, tracking `[...]` so a delimiter inside a
		// bracket expression doesn't close the regex early. Brace chars
		// inside a regex must NOT affect block depth.
		if atCmdStart && c == '/' {
			buf.WriteByte(c)
			i++
			inClass := false
			for i < len(s) {
				cc := s[i]
				if cc == '\\' && i+1 < len(s) {
					buf.WriteByte(cc)
					buf.WriteByte(s[i+1])
					i += 2
					continue
				}
				if !inClass && cc == '[' {
					inClass = true
					buf.WriteByte(cc)
					i++
					// Leading `]` or `^]` are literal class members.
					if i < len(s) && s[i] == ']' {
						buf.WriteByte(']')
						i++
					} else if i+1 < len(s) && s[i] == '^' && s[i+1] == ']' {
						buf.WriteByte('^')
						buf.WriteByte(']')
						i += 2
					}
					continue
				}
				if inClass && cc == ']' {
					inClass = false
					buf.WriteByte(cc)
					i++
					continue
				}
				if !inClass && cc == '/' {
					buf.WriteByte(cc)
					i++
					break
				}
				buf.WriteByte(cc)
				i++
			}
			atCmdStart = false
			continue
		}
		// `{` and `}` adjust block depth ONLY at the command position
		// (not inside an address — handled above). We keep them in the
		// buffer so the surrounding command body contains the whole
		// block; parseCommand recurses into it.
		if c == '{' {
			braceDepth++
			buf.WriteByte(c)
			i++
			atCmdStart = true
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
				atCmdStart = true
			}
			continue
		}
		// a/i/c with multi-line text. Recognise after we've consumed any
		// addresses (which were handled by the regex branch above). The
		// text payload ends at the first newline that isn't preceded by
		// a `\` (line-continuation in classic GNU syntax).
		if (c == 'a' || c == 'i' || c == 'c') && i+1 <= len(s) && braceDepth == 0 {
			// Disambiguate from a stray 'a' inside another command's body
			// — only accept if either the previous bufferred command is
			// empty/address-only AND the next char is `\` or whitespace.
			next := byte(' ')
			if i+1 < len(s) {
				next = s[i+1]
			}
			if next == '\\' || next == ' ' || next == '\t' || next == '\n' {
				buf.WriteByte(c)
				i++
				// Consume the rest of the body until an unescaped \n
				// outside braces. The first character may be `\` which
				// will then be followed by an embedded `\n` belonging to
				// the text.
				for i < len(s) {
					cc := s[i]
					if cc == '\\' && i+1 < len(s) {
						buf.WriteByte(cc)
						buf.WriteByte(s[i+1])
						i += 2
						continue
					}
					if cc == '\n' {
						// Unescaped newline — end of a/i/c body.
						break
					}
					buf.WriteByte(cc)
					i++
				}
				// Don't consume the newline; the outer split will do it.
				atCmdStart = false
				continue
			}
		}
		// If we hit a 's' or 'y' command, scan until we've seen 3
		// unescaped occurrences of the delimiter. Brackets inside the
		// regex are tracked so `s:[/]:X:` doesn't close on the inner /.
		if (c == 's' || c == 'y') && looksLikeSubAt(s, i) {
			delim := s[i+1]
			buf.WriteByte(c)
			i++
			delims := 0
			inClass := false
			for i < len(s) && delims < 3 {
				cc := s[i]
				if cc == '\\' && i+1 < len(s) {
					buf.WriteByte(cc)
					buf.WriteByte(s[i+1])
					i += 2
					continue
				}
				if delims == 0 { // bracket tracking only during the regex
					if !inClass && cc == '[' {
						inClass = true
					} else if inClass && cc == ']' {
						inClass = false
					}
				}
				if !inClass && cc == delim {
					delims++
				}
				buf.WriteByte(cc)
				i++
			}
			// After the 3rd delimiter, consume flags until a separator.
			for i < len(s) && s[i] != ';' && s[i] != '\n' && s[i] != '}' {
				buf.WriteByte(s[i])
				i++
			}
			atCmdStart = false
			continue
		}
		buf.WriteByte(c)
		i++
		atCmdStart = false
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
	case ':':
		// Label definition. The label is the rest of the line (whitespace
		// stripped). Labels can't have addresses in real sed, but we don't
		// enforce that.
		c.label = strings.TrimSpace(body)
		if c.label == "" {
			return c, errors.New(": requires a label name")
		}
	case 'b', 't', 'T':
		// Branch (unconditional, on-success, on-failure). Empty label
		// means branch to end of script.
		c.label = strings.TrimSpace(body)
	case 'r':
		// Read file: append its contents after the current cycle.
		// The whole rest-of-line (after stripping leading whitespace) is
		// the filename.
		c.label = strings.TrimSpace(body)
		if c.label == "" {
			return c, errors.New("r requires a filename")
		}
	case 'a', 'i', 'c':
		// Append / insert / change. Both forms accepted:
		//   a\<NL>TEXT         (classic; backslash separates command
		//                       from text, text on next line)
		//   a\TEXT             (text directly after the backslash)
		//   a TEXT             (GNU extension, space separates)
		// Multi-line continuation via trailing backslash is recognised
		// (the newline-joined trailing text becomes the payload).
		c.label = parseACIText(body)
	case 'd', 'p', 'q', 'n', 'N', 'D', 'P', 'h', 'H', 'g', 'G', 'x', '=':
		// no-op (or trailing whitespace)
	default:
		return c, fmt.Errorf("unsupported command %q", string(c.op))
	}
	return c, nil
}

// parseACIText turns the body following an a/i/c command into the
// payload text. Strips the leading `\` or space separator (and the
// newline that follows it in the classic form) and processes `\<NL>`
// continuation lines.
func parseACIText(body string) string {
	s := body
	if len(s) > 0 && (s[0] == '\\' || s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	// In the classic `a\<NL>text` form, the newline right after the
	// backslash is the command/text separator — drop it.
	s = strings.TrimPrefix(s, "\n")
	// Backslash-continuation: `\<NL>` joins lines verbatim.
	s = strings.ReplaceAll(s, "\\\n", "\n")
	// Bare trailing backslash leaves no text.
	s = strings.TrimSuffix(s, "\\")
	return s
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
	src, rest, ok := scanSubField(rest, delim, false)
	if !ok {
		return errors.New("unterminated y src")
	}
	dst, _, ok := scanSubField(rest, delim, false)
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
		case '$':
			// BRE's `$` is an anchor only at end-of-pattern (or right
			// before `\)`). Anywhere else it's literal. RE2 treats `$`
			// as an anchor in any position, so escape interior ones.
			if i == len(pat)-1 {
				out.WriteByte('$')
			} else if i+1 < len(pat) && pat[i+1] == '\\' &&
				i+2 < len(pat) && pat[i+2] == ')' {
				out.WriteByte('$')
			} else {
				out.WriteString(`\$`)
			}
		case '^':
			// Symmetric: BRE's `^` is an anchor only at start (or after
			// `\(`); elsewhere literal. RE2 treats it as start-anchor
			// anywhere; escape interior ones.
			if i == 0 {
				out.WriteByte('^')
			} else if i >= 2 && pat[i-2] == '\\' && pat[i-1] == '(' {
				out.WriteByte('^')
			} else {
				out.WriteString(`\^`)
			}
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
		// Find unescaped closing slash. Brackets `[...]` are tracked so a
		// `/` inside a character class doesn't close the regex early.
		j := 1
		inClass := false
		for j < len(s) {
			if s[j] == '\\' && j+1 < len(s) {
				j += 2
				continue
			}
			if !inClass && s[j] == '[' {
				inClass = true
				j++
				// `[]` and `[^]` allow `]` as first char.
				if j < len(s) && s[j] == ']' {
					j++
					continue
				}
				if j+1 < len(s) && s[j] == '^' && s[j+1] == ']' {
					j += 2
					continue
				}
				continue
			}
			if inClass && s[j] == ']' {
				inClass = false
				j++
				continue
			}
			if !inClass && s[j] == '/' {
				break
			}
			j++
		}
		if j >= len(s) {
			return s, address{}, errors.New("unterminated regex address")
		}
		pat := s[1:j]
		// Empty `//` means "reuse last regex"; defer compile to runtime.
		if pat == "" {
			return s[j+1:], address{kind: addrRegex, re: nil}, nil
		}
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
	// Pattern field: track bracket classes so the delim inside `[...]` is
	// literal. Replacement field: brackets carry no special meaning.
	pat, rest, ok := scanSubField(rest, delim, true)
	if !ok {
		return errors.New("unterminated s pattern")
	}
	repl, rest, ok := scanSubField(rest, delim, false)
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
	// An empty pattern means "reuse the most recent regex" — GNU sed.
	// Leave c.subRE nil so the runner picks up r.lastRE; resolved each
	// time the command runs.
	if pat != "" {
		re, err := compileSedRE(pat, extended)
		if err != nil {
			return err
		}
		if c.subFlags.icase {
			re, err = regexp.Compile("(?i)" + re.String())
			if err != nil {
				return err
			}
		}
		c.subRE = re
	}
	c.subRepl = repl
	return nil
}

// scanSubField extracts one delimited field from an s/// or y/// body.
// When trackClass is true (regex field), `[...]` is treated as a bracket
// expression so the s-delimiter inside it is literal — that's needed
// for `s:[[:space:]]*::` and similar. When false (replacement field),
// `[` and `]` are literal characters with no special meaning.
func scanSubField(s string, delim byte, trackClass bool) (string, string, bool) {
	var b strings.Builder
	i := 0
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
		if trackClass && !inClass && c == '[' {
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
		if trackClass && inClass && c == ']' {
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
