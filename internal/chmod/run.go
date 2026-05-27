package chmod

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type options struct {
	recursive bool
	verbose   bool
	changes   bool
	silent    bool
	reference string
}

func parseArgs(argv []string) (*options, string, []string, error) {
	o := &options{}
	var rest []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			rest = append(rest, argv[i+1:]...)
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
			switch name {
			case "recursive":
				o.recursive = true
			case "verbose":
				o.verbose = true
			case "changes":
				o.changes = true
			case "silent", "quiet":
				o.silent = true
			case "reference":
				if !hasEq {
					if i+1 >= len(argv) {
						return nil, "", nil, errors.New("--reference requires an argument")
					}
					i++
					val = argv[i]
				}
				o.reference = val
			default:
				// Ignore unknown long flags.
			}
			continue
		}
		// Short flags that DON'T look like a mode (mode forms like
		// "-rwx" or "-w" can collide with a short-flag cluster). We
		// distinguish: a leading dash followed by digits or a known
		// short-flag char is an option; a dash followed by a symbolic
		// mode-perm char without a matching short flag is the mode.
		if strings.HasPrefix(a, "-") && a != "-" && len(a) > 1 && isShortFlagCluster(a) {
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'R':
					o.recursive = true
				case 'v':
					o.verbose = true
				case 'c':
					o.changes = true
				case 'f':
					o.silent = true
				default:
					// Silently ignore unknown short flags.
				}
			}
			continue
		}
		rest = append(rest, a)
	}

	if o.reference != "" {
		if len(rest) < 1 {
			return nil, "", nil, errors.New("missing file operand")
		}
		return o, "", rest, nil
	}
	if len(rest) < 2 {
		return nil, "", nil, errors.New("missing operand: chmod MODE FILE...")
	}
	return o, rest[0], rest[1:], nil
}

// isShortFlagCluster reports whether s ("-Rv", "-c", "-f") is a recognized
// chmod option cluster — as opposed to a leading-dash mode spec like
// "-w" or "-rwx" (which removes those perms from `a`).
//
// Heuristic: every character in s[1:] must be one of the known short
// flags (R, v, c, f, H — H for the BSD --no-dereference no-op).
func isShortFlagCluster(s string) bool {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case 'R', 'v', 'c', 'f', 'H':
		default:
			return false
		}
	}
	return true
}

func run(argv []string) int {
	o, modeSpec, paths, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chmod: %v\n", err)
		return 1
	}

	// Resolve the mode-change function once. Either it's a closure that
	// reads --reference's mode, or a closure that applies modeSpec to
	// the file's current mode.
	var apply func(info fs.FileInfo) fs.FileMode
	if o.reference != "" {
		refInfo, err := os.Stat(o.reference)
		if err != nil {
			fmt.Fprintf(os.Stderr, "chmod: failed to get attributes of '%s': %v\n", o.reference, err)
			return 1
		}
		target := refInfo.Mode().Perm()
		apply = func(fs.FileInfo) fs.FileMode { return target }
	} else {
		modeApply, err := compileMode(modeSpec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "chmod: %v\n", err)
			return 1
		}
		apply = modeApply
	}

	exit := 0
	for _, p := range paths {
		if err := apply1(p, apply, o); err != nil {
			if !o.silent {
				fmt.Fprintf(os.Stderr, "chmod: %v\n", err)
			}
			exit = 1
		}
	}
	return exit
}

func apply1(p string, apply func(fs.FileInfo) fs.FileMode, o *options) error {
	info, err := os.Lstat(p)
	if err != nil {
		return err
	}
	// chmod follows symlinks: change the target's mode, not the link.
	// (Real chmod does the same — Linux symlinks ignore their own mode bits.)
	if info.Mode()&fs.ModeSymlink != 0 && !o.recursive {
		info, err = os.Stat(p)
		if err != nil {
			return err
		}
	}
	// Post-order: recurse FIRST so children inherit perms before the
	// parent's traverse bits potentially get stripped. (gnu does the
	// equivalent with fts_open holding a dirfd.)
	if o.recursive && info.IsDir() {
		entries, err := os.ReadDir(p)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := apply1(filepath.Join(p, e.Name()), apply, o); err != nil {
				if !o.silent {
					fmt.Fprintf(os.Stderr, "chmod: %v\n", err)
				}
			}
		}
	}
	return changeMode(p, info, apply, o)
}

func changeMode(p string, info fs.FileInfo, apply func(fs.FileInfo) fs.FileMode, o *options) error {
	oldPerm := info.Mode() & 0o7777
	newPerm := apply(info) & 0o7777
	if newPerm == oldPerm {
		if o.verbose {
			fmt.Fprintf(os.Stdout, "mode of '%s' retained as %04o (%s)\n",
				p, oldPerm, permString(oldPerm))
		}
		return nil
	}
	if err := os.Chmod(p, newPerm); err != nil {
		return err
	}
	if o.verbose || o.changes {
		fmt.Fprintf(os.Stdout, "mode of '%s' changed from %04o (%s) to %04o (%s)\n",
			p, oldPerm, permString(oldPerm), newPerm, permString(newPerm))
	}
	return nil
}

// compileMode parses a mode argument into a function that yields the
// new mode given the current FileInfo. The closure form lets symbolic
// modes depend on the file's existing perms and on whether it's a dir.
func compileMode(spec string) (func(fs.FileInfo) fs.FileMode, error) {
	if spec == "" {
		return nil, errors.New("empty mode")
	}
	// Octal? All chars are digits 0-7.
	if isOctalLiteral(spec) {
		v, err := strconv.ParseUint(spec, 8, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid mode %q: %w", spec, err)
		}
		m := fs.FileMode(v) & 0o7777
		return func(fs.FileInfo) fs.FileMode { return m }, nil
	}
	// Symbolic: comma-separated clauses, applied in order.
	clauses, err := parseSymbolic(spec)
	if err != nil {
		return nil, err
	}
	return func(info fs.FileInfo) fs.FileMode {
		cur := info.Mode() & 0o7777
		isDir := info.IsDir()
		for _, c := range clauses {
			cur = c.apply(cur, isDir)
		}
		return cur
	}, nil
}

func isOctalLiteral(s string) bool {
	if len(s) == 0 || len(s) > 4 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '7' {
			return false
		}
	}
	return true
}

// symbolicClause is one of u+x, g-w, =rx, +X, etc.
type symbolicClause struct {
	who   uint32 // bitmask of which classes (u=0o7000, g=0o0700, o=0o0070, mapped to perm positions)
	op    byte   // '+', '-', '='
	perms uint32 // bitmask of perms in the user position (0o700); we'll spread to who classes
	copy  byte   // when set ('u','g','o'), take perms from that class instead of from `perms`
	xCap  bool   // X (capital) — execute only if dir or any-x already set
}

func parseSymbolic(spec string) ([]symbolicClause, error) {
	var out []symbolicClause
	for _, raw := range strings.Split(spec, ",") {
		c, err := parseOneClause(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func parseOneClause(s string) (symbolicClause, error) {
	if s == "" {
		return symbolicClause{}, errors.New("empty clause in mode")
	}
	var c symbolicClause
	i := 0
	// who
	for i < len(s) && (s[i] == 'u' || s[i] == 'g' || s[i] == 'o' || s[i] == 'a') {
		switch s[i] {
		case 'u':
			c.who |= 0o4 // marker bit; we'll translate below
		case 'g':
			c.who |= 0o2
		case 'o':
			c.who |= 0o1
		case 'a':
			c.who |= 0o7
		}
		i++
	}
	if c.who == 0 {
		c.who = 0o7 // default = all
	}
	// op
	if i >= len(s) || (s[i] != '+' && s[i] != '-' && s[i] != '=') {
		return symbolicClause{}, fmt.Errorf("invalid mode %q: expected +, - or =", s)
	}
	c.op = s[i]
	i++
	// perms
	for i < len(s) {
		switch s[i] {
		case 'r':
			c.perms |= 0o4
		case 'w':
			c.perms |= 0o2
		case 'x':
			c.perms |= 0o1
		case 'X':
			c.xCap = true
		case 's':
			// setuid / setgid — encoded outside the rwx triplet.
			c.perms |= 0o10
		case 't':
			// sticky bit.
			c.perms |= 0o20
		case 'u', 'g', 'o':
			if c.copy != 0 || c.perms != 0 {
				return symbolicClause{}, fmt.Errorf("invalid mode %q: copy-source must stand alone", s)
			}
			c.copy = s[i]
		default:
			return symbolicClause{}, fmt.Errorf("invalid mode %q: unexpected %q", s, s[i])
		}
		i++
	}
	return c, nil
}

func (c symbolicClause) apply(cur fs.FileMode, isDir bool) fs.FileMode {
	// Resolve copy-source if used.
	var rwx uint32
	if c.copy != 0 {
		switch c.copy {
		case 'u':
			rwx = uint32(cur>>6) & 0o7
		case 'g':
			rwx = uint32(cur>>3) & 0o7
		case 'o':
			rwx = uint32(cur) & 0o7
		}
	} else {
		rwx = c.perms & 0o7
	}
	// Resolve capital X.
	if c.xCap {
		anyX := cur&0o111 != 0
		if isDir || anyX {
			rwx |= 0o1
		}
	}
	// Build the perm mask in the user-position of three classes.
	mask := fs.FileMode(0)
	if c.who&0o4 != 0 {
		mask |= fs.FileMode(rwx) << 6
	}
	if c.who&0o2 != 0 {
		mask |= fs.FileMode(rwx) << 3
	}
	if c.who&0o1 != 0 {
		mask |= fs.FileMode(rwx)
	}
	// Setuid / setgid: applied when the clause hit 's' AND 'u' or 'g'.
	if c.perms&0o10 != 0 {
		if c.who&0o4 != 0 {
			mask |= 0o4000
		}
		if c.who&0o2 != 0 {
			mask |= 0o2000
		}
	}
	// Sticky: applies regardless of who (gnu accepts "+t" / "a+t").
	if c.perms&0o20 != 0 {
		mask |= 0o1000
	}

	switch c.op {
	case '+':
		return cur | mask
	case '-':
		return cur &^ mask
	case '=':
		// = clears the perms for the named classes, then sets to mask.
		// Setuid/setgid/sticky from existing perms are preserved if
		// the clause doesn't mention them.
		clear := fs.FileMode(0)
		if c.who&0o4 != 0 {
			clear |= 0o700
		}
		if c.who&0o2 != 0 {
			clear |= 0o070
		}
		if c.who&0o1 != 0 {
			clear |= 0o007
		}
		// Special bits: cleared only when the user wrote them in the
		// clause (s / t). We use the mask bits to decide.
		if c.perms&0o10 != 0 {
			if c.who&0o4 != 0 {
				clear |= 0o4000
			}
			if c.who&0o2 != 0 {
				clear |= 0o2000
			}
		}
		if c.perms&0o20 != 0 {
			clear |= 0o1000
		}
		return (cur &^ clear) | mask
	}
	return cur
}

// permString renders the 9-char rwxrwxrwx string for the lower 9 bits
// of a mode (no setuid/setgid/sticky encoding here — kept simple for
// the verbose output).
func permString(m fs.FileMode) string {
	var b [9]byte
	for i, bit := range []fs.FileMode{0o400, 0o200, 0o100, 0o040, 0o020, 0o010, 0o004, 0o002, 0o001} {
		if m&bit != 0 {
			b[i] = "rwxrwxrwx"[i]
		} else {
			b[i] = '-'
		}
	}
	return string(b[:])
}

// silenceErrno avoids importing syscall in cases where it isn't used.
// Kept here so future error refinement (e.g. distinguishing EPERM
// from ENOENT) has a hook ready.
var _ = syscall.EPERM
