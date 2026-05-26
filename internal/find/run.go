package find

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func run(args []string) int {
	roots, exprArgs, err := splitRoots(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "find: %v\n", err)
		return 1
	}
	if len(roots) == 0 {
		roots = []string{"."}
	}

	expr, hasAction, err := parseExpr(exprArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "find: %v\n", err)
		return 1
	}

	exit := 0
	for _, root := range roots {
		wc, err := walk2(root, expr, hasAction)
		if err != nil {
			fmt.Fprintf(os.Stderr, "find: %v\n", err)
			exit = 1
		}
		if wc != nil && wc.hadActionErr {
			exit = 1
		}
	}
	return exit
}

// walk2 wraps walk and returns the populated walkCtx so callers can
// observe action errors (e.g. -delete or -exec failures).
func walk2(root string, expr *node, hasAction bool) (*walkCtx, error) {
	wc := &walkCtx{
		root:       root,
		expr:       expr,
		hasAction:  hasAction,
		depthFirst: hasDeleteIn(expr),
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return wc, err
	}
	return wc, walkInner(wc, root, 0, rootInfo)
}

// walk drives a custom filesystem walk so we can honor -prune and
// -mindepth / -maxdepth without filepath.Walk's restrictions.
//
// When the expression contains a -delete action we walk depth-first
// (children before parents) so that emptying a directory before
// removing it actually works. GNU find implies -depth with -delete.
type walkCtx struct {
	root       string
	expr       *node
	hasAction  bool
	depthFirst bool
	// hadActionErr is set when a -delete or -exec action fails. It does
	// not abort the walk (matching GNU find), but it forces a non-zero
	// process exit so scripts can detect failures.
	hadActionErr bool
}

func hasDeleteIn(n *node) bool {
	if n == nil {
		return false
	}
	if n.op == "delete" {
		return true
	}
	return hasDeleteIn(n.left) || hasDeleteIn(n.right)
}

func walkInner(wc *walkCtx, p string, depth int, info os.FileInfo) error {
	if wc.depthFirst {
		return walkDepthFirst(wc, p, depth, info)
	}
	return walkPreOrder(wc, p, depth, info)
}

func walkPreOrder(wc *walkCtx, p string, depth int, info os.FileInfo) error {
	ent := entry{path: p, info: info, depth: depth, wc: wc}

	matched, prune := evalExpr(wc.expr, &ent)
	if matched && !wc.hasAction && !ent.printed {
		fmt.Println(p)
	}
	if prune {
		return nil
	}
	if info.IsDir() {
		for _, child := range readChildren(p) {
			ci, err := os.Lstat(child)
			if err != nil {
				fmt.Fprintf(os.Stderr, "find: %s: %v\n", child, err)
				continue
			}
			if err := walkInner(wc, child, depth+1, ci); err != nil {
				return err
			}
		}
	}
	return nil
}

// walkDepthFirst visits children before evaluating the parent. -prune is
// effectively ignored here because the directory's children have already
// been processed; that matches GNU find's documented behavior with -depth.
func walkDepthFirst(wc *walkCtx, p string, depth int, info os.FileInfo) error {
	if info.IsDir() {
		for _, child := range readChildren(p) {
			ci, err := os.Lstat(child)
			if err != nil {
				fmt.Fprintf(os.Stderr, "find: %s: %v\n", child, err)
				continue
			}
			if err := walkInner(wc, child, depth+1, ci); err != nil {
				return err
			}
		}
	}
	ent := entry{path: p, info: info, depth: depth, wc: wc}
	matched, _ := evalExpr(wc.expr, &ent)
	if matched && !wc.hasAction && !ent.printed {
		fmt.Println(p)
	}
	return nil
}

// readChildren returns the child paths of dir, hand-built so the
// user-supplied root prefix is preserved (GNU find emits './x' for
// `find .`, never 'x').
func readChildren(p string) []string {
	entries, err := os.ReadDir(p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "find: %s: %v\n", p, err)
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, de := range entries {
		var child string
		switch {
		case p == "":
			child = de.Name()
		case p == "/":
			child = "/" + de.Name()
		case p[len(p)-1] == '/':
			child = p + de.Name()
		default:
			child = p + "/" + de.Name()
		}
		out = append(out, child)
	}
	return out
}

// entry captures everything per-file the predicates need.
type entry struct {
	path    string
	info    os.FileInfo
	depth   int
	printed bool      // set by -print action so default-print isn't doubled
	wc      *walkCtx  // back-pointer so actions can record errors
}

// node is the parsed expression AST.
type node struct {
	op  string
	str string
	num int64
	tnode timeBucket
	args []string

	left, right *node

	// Cached state where applicable.
	pat string

	// For -newer: the mtime to compare against.
	newerMtime time.Time
}

type timeBucket struct {
	op   rune  // '+', '-', '='
	n    int64 // value (days for time, units for size)
	unit int64 // for -size: bytes per unit. 0 means already in bytes.
}

// evalExpr returns (matched, prune). prune is set when -prune fires for
// this entry — caller should not descend.
func evalExpr(n *node, e *entry) (matched, prune bool) {
	if n == nil {
		return true, false
	}
	switch n.op {
	case "AND":
		l, p1 := evalExpr(n.left, e)
		if !l {
			return false, p1
		}
		r, p2 := evalExpr(n.right, e)
		return l && r, p1 || p2
	case "OR":
		l, p1 := evalExpr(n.left, e)
		if l {
			return true, p1
		}
		r, p2 := evalExpr(n.right, e)
		return r, p1 || p2
	case "NOT":
		l, p := evalExpr(n.left, e)
		return !l, p
	case "TRUE":
		return true, false
	case "name":
		ok, _ := filepath.Match(n.pat, filepath.Base(e.path))
		return ok, false
	case "iname":
		ok, _ := filepath.Match(strings.ToLower(n.pat), strings.ToLower(filepath.Base(e.path)))
		return ok, false
	case "path":
		ok, _ := filepath.Match(n.pat, e.path)
		return ok, false
	case "ipath":
		ok, _ := filepath.Match(strings.ToLower(n.pat), strings.ToLower(e.path))
		return ok, false
	case "type":
		return matchType(e.info, n.str), false
	case "size":
		return matchSize(e.info.Size(), n.tnode, n.str), false
	case "mtime":
		return matchTime(e.info.ModTime(), n.tnode), false
	case "newer":
		return e.info.ModTime().After(n.newerMtime), false
	case "mindepth":
		return e.depth >= int(n.num), false
	case "maxdepth":
		return e.depth <= int(n.num), false
	case "empty":
		return isEmpty(e), false
	case "prune":
		// -prune always returns true; signals caller to skip descent.
		return true, true
	case "print":
		fmt.Println(e.path)
		e.printed = true
		return true, false
	case "print0":
		fmt.Print(e.path + "\x00")
		e.printed = true
		return true, false
	case "printf":
		fmt.Print(formatPrintf(n.str, e))
		e.printed = true
		return true, false
	case "delete":
		// GNU find requires -depth with -delete; we silently set it.
		err := os.Remove(e.path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "find: %s: %v\n", e.path, err)
			if e.wc != nil {
				e.wc.hadActionErr = true
			}
			return false, false
		}
		e.printed = true // suppresses default print
		return true, false
	case "exec":
		ok := runExec(n.args, e.path)
		if !ok && e.wc != nil {
			e.wc.hadActionErr = true
		}
		return ok, false
	}
	return true, false
}

func matchType(info os.FileInfo, t string) bool {
	mode := info.Mode()
	switch t {
	case "f":
		return mode.IsRegular()
	case "d":
		return mode.IsDir()
	case "l":
		return mode&os.ModeSymlink != 0
	case "p":
		return mode&os.ModeNamedPipe != 0
	case "c":
		return mode&(os.ModeDevice|os.ModeCharDevice) == os.ModeDevice|os.ModeCharDevice
	case "b":
		return mode&os.ModeDevice != 0 && mode&os.ModeCharDevice == 0
	case "s":
		return mode&os.ModeSocket != 0
	}
	return false
}

func matchSize(size int64, t timeBucket, _ string) bool {
	// GNU find rounds size *up* to the next unit before comparing, so
	// `-size 1k` matches files of 1..1024 bytes (not just exactly 1024).
	got := size
	want := t.n
	if t.unit > 0 {
		got = (size + t.unit - 1) / t.unit
		want = t.n / t.unit
	}
	switch t.op {
	case '+':
		return got > want
	case '-':
		return got < want
	}
	return got == want
}

func matchTime(mtime time.Time, t timeBucket) bool {
	days := int64(time.Since(mtime).Hours() / 24)
	switch t.op {
	case '+':
		return days > t.n
	case '-':
		return days < t.n
	}
	return days == t.n
}

func isEmpty(e *entry) bool {
	if e.info.IsDir() {
		f, err := os.Open(e.path)
		if err != nil {
			return false
		}
		defer f.Close()
		_, err = f.Readdirnames(1)
		if errors.Is(err, fs.ErrInvalid) {
			return false
		}
		return err != nil
	}
	// GNU find -empty matches only regular files and directories. A
	// 0-length pipe / socket / device / symlink-to-"" should not match.
	if !e.info.Mode().IsRegular() {
		return false
	}
	return e.info.Size() == 0
}

// formatPrintf renders a -printf format string against the current
// entry. Implements the format specifiers and escapes that show up in
// the kernel's build scripts and other common usages:
//
//	%p  full pathname
//	%P  pathname relative to the start point (root stripped)
//	%f  basename
//	%h  leading directories of %p
//	%H  the starting-point under which the file was found
//	%s  size in bytes
//	%m  permission bits as octal (no leading zeros)
//	%M  symbolic file mode (e.g. "drwxr-xr-x") — best effort
//	%y  type letter (f, d, l, p, c, b, s)
//	%TY %Tm %Td  mtime year/month/day, %T@ epoch seconds
//	%%  literal percent
//	\n \t \0 \\  escape sequences
//
// Unrecognised %X is preserved verbatim so we don't lose data.
func formatPrintf(fmtstr string, e *entry) string {
	var b strings.Builder
	for i := 0; i < len(fmtstr); i++ {
		c := fmtstr[i]
		if c == '\\' && i+1 < len(fmtstr) {
			switch fmtstr[i+1] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '0':
				b.WriteByte(0)
			case '\\':
				b.WriteByte('\\')
			default:
				b.WriteByte(fmtstr[i+1])
			}
			i++
			continue
		}
		if c != '%' {
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(fmtstr) {
			b.WriteByte('%')
			continue
		}
		switch fmtstr[i+1] {
		case '%':
			b.WriteByte('%')
			i++
		case 'p':
			b.WriteString(e.path)
			i++
		case 'P':
			rel := e.path
			if e.wc != nil {
				root := e.wc.root
				if strings.HasPrefix(rel, root) {
					rel = strings.TrimPrefix(rel, root)
					rel = strings.TrimPrefix(rel, "/")
				}
			}
			b.WriteString(rel)
			i++
		case 'f':
			b.WriteString(filepath.Base(e.path))
			i++
		case 'h':
			b.WriteString(filepath.Dir(e.path))
			i++
		case 'H':
			if e.wc != nil {
				b.WriteString(e.wc.root)
			} else {
				b.WriteString(e.path)
			}
			i++
		case 's':
			fmt.Fprintf(&b, "%d", e.info.Size())
			i++
		case 'm':
			fmt.Fprintf(&b, "%o", e.info.Mode().Perm())
			i++
		case 'M':
			b.WriteString(e.info.Mode().String())
			i++
		case 'y':
			b.WriteByte(typeLetter(e.info.Mode()))
			i++
		case 'T':
			// Two-char specifier: %Tc.
			if i+2 < len(fmtstr) {
				b.WriteString(formatTime(e.info.ModTime(), fmtstr[i+2]))
				i += 2
			} else {
				b.WriteByte('%')
			}
		default:
			b.WriteByte('%')
			b.WriteByte(fmtstr[i+1])
			i++
		}
	}
	return b.String()
}

func typeLetter(m os.FileMode) byte {
	switch {
	case m.IsRegular():
		return 'f'
	case m.IsDir():
		return 'd'
	case m&os.ModeSymlink != 0:
		return 'l'
	case m&os.ModeNamedPipe != 0:
		return 'p'
	case m&os.ModeCharDevice != 0:
		return 'c'
	case m&os.ModeDevice != 0:
		return 'b'
	case m&os.ModeSocket != 0:
		return 's'
	}
	return '?'
}

func formatTime(t time.Time, spec byte) string {
	switch spec {
	case '@':
		return strconv.FormatInt(t.Unix(), 10)
	case 'Y':
		return fmt.Sprintf("%04d", t.Year())
	case 'm':
		return fmt.Sprintf("%02d", int(t.Month()))
	case 'd':
		return fmt.Sprintf("%02d", t.Day())
	case 'H':
		return fmt.Sprintf("%02d", t.Hour())
	case 'M':
		return fmt.Sprintf("%02d", t.Minute())
	case 'S':
		return fmt.Sprintf("%02d", t.Second())
	}
	return string(spec)
}

func runExec(args []string, path string) bool {
	final := make([]string, len(args))
	for i, a := range args {
		final[i] = strings.ReplaceAll(a, "{}", path)
	}
	cmd := exec.Command(final[0], final[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

// splitRoots peels initial path arguments (those that don't begin with
// '-' or '!' or '(' ) and returns the rest as expression tokens.
func splitRoots(args []string) ([]string, []string, error) {
	var roots []string
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "" {
			i++
			continue
		}
		if a[0] == '-' || a == "!" || a == "(" || a == ")" {
			break
		}
		roots = append(roots, a)
		i++
	}
	return roots, args[i:], nil
}

// parser ----------------------------------------------------------------

type parser struct {
	tokens []string
	pos    int
}

// parseExpr parses the find expression and returns (root, hasAction, err).
// hasAction is true when at least one action (-print/-print0/-delete/-exec)
// is present; it controls whether the default -print is added.
func parseExpr(tokens []string) (*node, bool, error) {
	if len(tokens) == 0 {
		return nil, false, nil
	}
	p := &parser{tokens: tokens}
	expr, err := p.parseOr()
	if err != nil {
		return nil, false, err
	}
	if p.pos != len(p.tokens) {
		return nil, false, fmt.Errorf("unexpected token %q", p.tokens[p.pos])
	}
	return expr, hasActionIn(expr), nil
}

func hasActionIn(n *node) bool {
	if n == nil {
		return false
	}
	switch n.op {
	case "print", "print0", "delete", "exec":
		return true
	}
	return hasActionIn(n.left) || hasActionIn(n.right)
}

func (p *parser) peek() string {
	if p.pos >= len(p.tokens) {
		return ""
	}
	return p.tokens[p.pos]
}

func (p *parser) eat() string {
	t := p.peek()
	p.pos++
	return t
}

func (p *parser) parseOr() (*node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t != "-o" && t != "-or" {
			return left, nil
		}
		p.eat()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &node{op: "OR", left: left, right: right}
	}
}

func (p *parser) parseAnd() (*node, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t == "" || t == ")" || t == "-o" || t == "-or" {
			return left, nil
		}
		if t == "-a" || t == "-and" {
			p.eat()
		}
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &node{op: "AND", left: left, right: right}
	}
}

func (p *parser) parseNot() (*node, error) {
	t := p.peek()
	if t == "!" || t == "-not" {
		p.eat()
		x, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &node{op: "NOT", left: x}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (*node, error) {
	t := p.peek()
	if t == "(" {
		p.eat()
		x, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek() != ")" {
			return nil, errors.New("missing ')'")
		}
		p.eat()
		return x, nil
	}
	return p.parsePred()
}

func (p *parser) parsePred() (*node, error) {
	t := p.eat()
	if t == "" {
		return nil, errors.New("expression ended early")
	}
	switch t {
	case "-name", "-iname", "-path", "-ipath":
		v := p.eat()
		if v == "" {
			return nil, fmt.Errorf("%s requires an argument", t)
		}
		return &node{op: t[1:], pat: v}, nil
	case "-type":
		v := p.eat()
		if v == "" {
			return nil, errors.New("-type requires an argument")
		}
		return &node{op: "type", str: v}, nil
	case "-size":
		v := p.eat()
		bucket, err := parseSizeArg(v)
		if err != nil {
			return nil, err
		}
		return &node{op: "size", tnode: bucket}, nil
	case "-mtime":
		v := p.eat()
		bucket, err := parseTimeArg(v)
		if err != nil {
			return nil, err
		}
		return &node{op: "mtime", tnode: bucket}, nil
	case "-newer":
		v := p.eat()
		fi, err := os.Stat(v)
		if err != nil {
			return nil, err
		}
		return &node{op: "newer", newerMtime: fi.ModTime()}, nil
	case "-mindepth":
		v := p.eat()
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, err
		}
		return &node{op: "mindepth", num: n}, nil
	case "-maxdepth":
		v := p.eat()
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, err
		}
		return &node{op: "maxdepth", num: n}, nil
	case "-prune":
		return &node{op: "prune"}, nil
	case "-empty":
		return &node{op: "empty"}, nil
	case "-print":
		return &node{op: "print"}, nil
	case "-print0":
		return &node{op: "print0"}, nil
	case "-printf":
		return &node{op: "printf", str: p.eat()}, nil
	case "-delete":
		return &node{op: "delete"}, nil
	case "-exec":
		var argv []string
		for {
			tok := p.eat()
			if tok == ";" {
				break
			}
			if tok == "" {
				return nil, errors.New("-exec missing terminating ';'")
			}
			argv = append(argv, tok)
		}
		if len(argv) == 0 {
			return nil, errors.New("-exec needs at least one argument")
		}
		return &node{op: "exec", args: argv}, nil
	default:
		return nil, fmt.Errorf("unrecognized predicate %q", t)
	}
}

// parseSizeArg accepts forms like "+1M", "-100c", "5".
func parseSizeArg(s string) (timeBucket, error) {
	if s == "" {
		return timeBucket{}, errors.New("empty -size")
	}
	op := byte('=')
	if s[0] == '+' || s[0] == '-' {
		op = s[0]
		s = s[1:]
	}
	if s == "" {
		return timeBucket{}, errors.New("invalid -size")
	}
	mult := int64(512) // default unit is 512-byte blocks
	switch s[len(s)-1] {
	case 'c':
		mult = 1
		s = s[:len(s)-1]
	case 'k':
		mult = 1024
		s = s[:len(s)-1]
	case 'M':
		mult = 1024 * 1024
		s = s[:len(s)-1]
	case 'G':
		mult = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	case 'b':
		mult = 512
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return timeBucket{}, err
	}
	return timeBucket{op: rune(op), n: n * mult, unit: mult}, nil
}

func parseTimeArg(s string) (timeBucket, error) {
	if s == "" {
		return timeBucket{}, errors.New("empty -mtime")
	}
	op := byte('=')
	if s[0] == '+' || s[0] == '-' {
		op = s[0]
		s = s[1:]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return timeBucket{}, err
	}
	return timeBucket{op: rune(op), n: n}, nil
}

// silence unused var if walkCtx struct gains/loses fields in future revs.
var _ = filepath.Join
