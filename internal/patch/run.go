package patch

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type options struct {
	strip     int
	input     string
	reverse   bool
	forward   bool
	output    string
	dryRun    bool
}

func parseArgs(argv []string) (*options, error) {
	o := &options{strip: 1}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
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
			case "strip":
				if !hasEq {
					if i+1 >= len(argv) {
						return nil, errors.New("--strip requires an argument")
					}
					i++
					val = argv[i]
				}
				n, err := strconv.Atoi(val)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("invalid --strip: %q", val)
				}
				o.strip = n
			case "input":
				if !hasEq {
					if i+1 >= len(argv) {
						return nil, errors.New("--input requires an argument")
					}
					i++
					val = argv[i]
				}
				o.input = val
			case "reverse":
				o.reverse = true
			case "forward":
				o.forward = true
			case "output":
				if !hasEq {
					if i+1 >= len(argv) {
						return nil, errors.New("--output requires an argument")
					}
					i++
					val = argv[i]
				}
				o.output = val
			case "dry-run":
				o.dryRun = true
			default:
				// Ignore unknown long flags.
			}
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" && len(a) > 1 && isFlagCluster(a) {
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'p':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-p requires an argument")
						}
						i++
						val = argv[i]
					}
					n, err := strconv.Atoi(val)
					if err != nil {
						return nil, fmt.Errorf("invalid -p: %q", val)
					}
					o.strip = n
				case 'i':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-i requires an argument")
						}
						i++
						val = argv[i]
					}
					o.input = val
				case 'R':
					o.reverse = true
				case 'N':
					o.forward = true
				case 'o':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-o requires an argument")
						}
						i++
						val = argv[i]
					}
					o.output = val
				default:
				}
			}
			continue
		}
	}
	return o, nil
}

func isFlagCluster(s string) bool {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case 'p', 'i', 'R', 'N', 'o':
		default:
			return false
		}
	}
	return true
}

func run(argv []string) int {
	o, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "patch: %v\n", err)
		return 1
	}
	var src io.Reader = os.Stdin
	if o.input != "" {
		f, err := os.Open(o.input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "patch: %v\n", err)
			return 1
		}
		defer f.Close()
		src = f
	}
	files, err := parsePatch(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "patch: %v\n", err)
		return 1
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "patch: no patch hunks found")
		return 1
	}
	exit := 0
	for _, f := range files {
		if err := applyFile(f, o); err != nil {
			fmt.Fprintf(os.Stderr, "patch: %v\n", err)
			exit = 1
		}
	}
	return exit
}

// filePatch is the parsed unified-diff record for a single file.
type filePatch struct {
	oldPath string
	newPath string
	hunks   []patchHunk
}

type patchHunk struct {
	oldStart int
	newStart int
	lines    []hunkLine
}

type hunkLine struct {
	kind byte // ' ', '-', '+', '\\' (no-newline marker)
	text string
}

func parsePatch(r io.Reader) ([]*filePatch, error) {
	br := bufio.NewReader(r)
	var files []*filePatch
	var current *filePatch
	var hunk *patchHunk
	for {
		line, err := br.ReadString('\n')
		if len(line) == 0 && err != nil {
			break
		}
		// Strip newline at the parse level; we re-add when applying.
		// (We track the no-newline marker explicitly.)
		raw := strings.TrimSuffix(line, "\n")
		switch {
		case strings.HasPrefix(raw, "--- "):
			if current != nil {
				files = append(files, current)
			}
			current = &filePatch{oldPath: strings.SplitN(raw[4:], "\t", 2)[0]}
			hunk = nil
		case strings.HasPrefix(raw, "+++ "):
			if current != nil {
				current.newPath = strings.SplitN(raw[4:], "\t", 2)[0]
			}
		case strings.HasPrefix(raw, "@@"):
			h, err := parseHunkHeader(raw)
			if err != nil {
				return nil, fmt.Errorf("bad hunk header %q: %w", raw, err)
			}
			hunk = h
			if current != nil {
				current.hunks = append(current.hunks, *hunk)
			}
		case raw == `\ No newline at end of file`:
			if hunk != nil && len(current.hunks) > 0 {
				h := &current.hunks[len(current.hunks)-1]
				if len(h.lines) > 0 {
					last := &h.lines[len(h.lines)-1]
					// Mark this line as no-newline by appending a sentinel
					// — we tolerate the marker without strict tracking.
					_ = last
				}
			}
		default:
			if hunk != nil && len(raw) > 0 && (raw[0] == ' ' || raw[0] == '+' || raw[0] == '-') {
				h := &current.hunks[len(current.hunks)-1]
				h.lines = append(h.lines, hunkLine{kind: raw[0], text: raw[1:]})
			}
		}
		if err == io.EOF {
			break
		}
	}
	if current != nil {
		files = append(files, current)
	}
	return files, nil
}

// parseHunkHeader handles "@@ -L,N +L,N @@ ...".
func parseHunkHeader(s string) (*patchHunk, error) {
	// Trim trailing "@@" and tail context.
	rest := strings.TrimPrefix(s, "@@")
	rest = strings.TrimSpace(rest)
	end := strings.Index(rest, "@@")
	if end < 0 {
		return nil, errors.New("missing trailing @@")
	}
	rest = strings.TrimSpace(rest[:end])
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		return nil, errors.New("expected '-L,N +L,N'")
	}
	old, _, err := parseRange(parts[0])
	if err != nil {
		return nil, err
	}
	new, _, err := parseRange(parts[1])
	if err != nil {
		return nil, err
	}
	return &patchHunk{oldStart: old, newStart: new}, nil
}

func parseRange(s string) (start, length int, err error) {
	if !strings.HasPrefix(s, "-") && !strings.HasPrefix(s, "+") {
		return 0, 0, errors.New("range must start with - or +")
	}
	s = s[1:]
	length = 1
	if idx := strings.Index(s, ","); idx >= 0 {
		length, err = strconv.Atoi(s[idx+1:])
		if err != nil {
			return 0, 0, fmt.Errorf("bad range length: %w", err)
		}
		s = s[:idx]
	}
	start, err = strconv.Atoi(s)
	if err != nil {
		return 0, 0, fmt.Errorf("bad range start: %w", err)
	}
	return start, length, nil
}

func applyFile(p *filePatch, o *options) error {
	src := stripPath(p.oldPath, o.strip)
	dst := stripPath(p.newPath, o.strip)
	if o.reverse {
		src, dst = dst, src
	}
	if src == "" {
		return fmt.Errorf("could not derive source path from %q", p.oldPath)
	}
	body, err := os.ReadFile(src)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		// Allow creation (--- /dev/null).
		body = nil
	}
	lines := strings.SplitAfter(string(body), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	for _, h := range p.hunks {
		// Try the recorded position first.
		newLines, ok := applyHunkAt(lines, h, h.oldStart, o.reverse)
		if !ok {
			// Fuzz: scan nearby positions for a match.
			ok2 := false
			for delta := 1; delta <= 50 && !ok2; delta++ {
				for _, p := range []int{h.oldStart + delta, h.oldStart - delta} {
					if p < 1 {
						continue
					}
					if nl, k := applyHunkAt(lines, h, p, o.reverse); k {
						newLines = nl
						ok2 = true
						break
					}
				}
			}
			if !ok2 {
				return fmt.Errorf("hunk @ %d failed (no fuzzy match) in %s", h.oldStart, src)
			}
		}
		lines = newLines
	}

	if o.dryRun {
		return nil
	}
	// GNU patch modifies the SOURCE path in place by default; the
	// new-side path in the header is informational. -o overrides.
	out := src
	if o.output != "" {
		out = o.output
	}
	_ = dst
	return os.WriteFile(out, []byte(strings.Join(lines, "")), 0o644)
}

// applyHunkAt attempts to apply hunk h with its old-side context
// starting at 1-based line `at` in `lines`. Returns the new line
// slice and whether the context matched.
func applyHunkAt(lines []string, h patchHunk, at int, reverse bool) ([]string, bool) {
	// Build the expected old-side content and new-side content.
	var oldSide, newSide []string
	for _, l := range h.lines {
		txt := l.text
		// Restore newline since SplitAfter keeps them.
		if !strings.HasSuffix(txt, "\n") {
			txt = txt + "\n"
		}
		switch l.kind {
		case ' ':
			oldSide = append(oldSide, txt)
			newSide = append(newSide, txt)
		case '-':
			oldSide = append(oldSide, txt)
		case '+':
			newSide = append(newSide, txt)
		}
	}
	if reverse {
		oldSide, newSide = newSide, oldSide
	}

	start := at - 1 // convert to 0-based
	if start < 0 || start+len(oldSide) > len(lines) {
		return nil, false
	}
	for i, l := range oldSide {
		if lines[start+i] != l {
			return nil, false
		}
	}
	out := make([]string, 0, len(lines)-len(oldSide)+len(newSide))
	out = append(out, lines[:start]...)
	out = append(out, newSide...)
	out = append(out, lines[start+len(oldSide):]...)
	return out, true
}

// stripPath removes the first n path components from p. If n > the
// number of components, returns the leaf (gnu behavior).
func stripPath(p string, n int) string {
	if p == "/dev/null" {
		return ""
	}
	parts := strings.Split(p, "/")
	if n >= len(parts) {
		return parts[len(parts)-1]
	}
	return strings.Join(parts[n:], "/")
}
