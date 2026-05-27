package diff

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type options struct {
	contextLines    int
	recursive       bool
	newFile         bool
	brief           bool
	ignoreCase      bool
	ignoreAllSpace  bool
	ignoreBlankLine bool
	reportSame      bool
	files           []string
}

func parseArgs(argv []string) (*options, error) {
	o := &options{contextLines: 3}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			o.files = append(o.files, argv[i+1:]...)
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
			case "unified":
				if hasEq {
					if _, err := fmt.Sscanf(val, "%d", &o.contextLines); err != nil {
						return nil, fmt.Errorf("invalid context lines: %q", val)
					}
				}
			case "recursive":
				o.recursive = true
			case "new-file":
				o.newFile = true
			case "brief":
				o.brief = true
			case "ignore-case":
				o.ignoreCase = true
			case "ignore-all-space":
				o.ignoreAllSpace = true
			case "ignore-blank-lines":
				o.ignoreBlankLine = true
			case "report-identical-files":
				o.reportSame = true
			default:
				// Ignore unknown long flags.
			}
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" && len(a) > 1 && isFlagCluster(a) {
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'u':
					// already unified by default
				case 'U':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-U requires an argument")
						}
						i++
						val = argv[i]
					}
					if _, err := fmt.Sscanf(val, "%d", &o.contextLines); err != nil {
						return nil, fmt.Errorf("invalid -U: %q", val)
					}
				case 'r':
					o.recursive = true
				case 'N':
					o.newFile = true
				case 'q':
					o.brief = true
				case 'i':
					o.ignoreCase = true
				case 'w':
					o.ignoreAllSpace = true
				case 'B':
					o.ignoreBlankLine = true
				case 's':
					o.reportSame = true
				default:
					// Ignore unknown short flags.
				}
			}
			continue
		}
		o.files = append(o.files, a)
	}
	if len(o.files) < 2 {
		return nil, errors.New("missing operand: diff FILE1 FILE2")
	}
	return o, nil
}

func isFlagCluster(s string) bool {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case 'u', 'U', 'r', 'N', 'q', 'i', 'w', 'B', 's':
		default:
			return false
		}
	}
	return true
}

func run(argv []string) int {
	o, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "diff: %v\n", err)
		return 2
	}
	exit := diffAny(o.files[0], o.files[1], o)
	return exit
}

func diffAny(a, b string, o *options) int {
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	if errA != nil && !(os.IsNotExist(errA) && o.newFile) {
		fmt.Fprintf(os.Stderr, "diff: %s: %v\n", a, errA)
		return 2
	}
	if errB != nil && !(os.IsNotExist(errB) && o.newFile) {
		fmt.Fprintf(os.Stderr, "diff: %s: %v\n", b, errB)
		return 2
	}
	aIsDir := infoA != nil && infoA.IsDir()
	bIsDir := infoB != nil && infoB.IsDir()
	switch {
	case aIsDir && bIsDir:
		if !o.recursive {
			fmt.Fprintf(os.Stderr, "diff: %s and %s are directories; -r needed\n", a, b)
			return 2
		}
		return diffDirs(a, b, o)
	case aIsDir != bIsDir:
		fmt.Fprintf(os.Stderr, "diff: %s and %s differ in type (file vs directory)\n", a, b)
		return 2
	}
	return diffFiles(a, b, o)
}

func diffDirs(a, b string, o *options) int {
	aNames, _ := readDirNames(a)
	bNames, _ := readDirNames(b)
	all := uniqSorted(append(aNames, bNames...))
	exit := 0
	for _, n := range all {
		pa := filepath.Join(a, n)
		pb := filepath.Join(b, n)
		_, errA := os.Stat(pa)
		_, errB := os.Stat(pb)
		switch {
		case errA != nil && errB == nil:
			if o.newFile {
				if e := diffAny(pa, pb, o); e > exit {
					exit = e
				}
			} else {
				fmt.Fprintf(os.Stdout, "Only in %s: %s\n", b, n)
				if exit < 1 {
					exit = 1
				}
			}
		case errB != nil && errA == nil:
			if o.newFile {
				if e := diffAny(pa, pb, o); e > exit {
					exit = e
				}
			} else {
				fmt.Fprintf(os.Stdout, "Only in %s: %s\n", a, n)
				if exit < 1 {
					exit = 1
				}
			}
		case errA == nil && errB == nil:
			if e := diffAny(pa, pb, o); e > exit {
				exit = e
			}
		}
	}
	return exit
}

func readDirNames(p string) ([]string, error) {
	es, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(es))
	for _, e := range es {
		names = append(names, e.Name())
	}
	return names, nil
}

func uniqSorted(s []string) []string {
	sort.Strings(s)
	out := s[:0]
	var last string
	for i, v := range s {
		if i == 0 || v != last {
			out = append(out, v)
		}
		last = v
	}
	return out
}

func diffFiles(a, b string, o *options) int {
	aData, errA := readOrEmpty(a, o.newFile)
	if errA != nil {
		fmt.Fprintf(os.Stderr, "diff: %s: %v\n", a, errA)
		return 2
	}
	bData, errB := readOrEmpty(b, o.newFile)
	if errB != nil {
		fmt.Fprintf(os.Stderr, "diff: %s: %v\n", b, errB)
		return 2
	}
	if isBinary(aData) || isBinary(bData) {
		if !bytes.Equal(aData, bData) {
			fmt.Fprintf(os.Stdout, "Binary files %s and %s differ\n", a, b)
			return 1
		}
		if o.reportSame {
			fmt.Fprintf(os.Stdout, "Files %s and %s are identical\n", a, b)
		}
		return 0
	}

	aLines := splitLines(aData)
	bLines := splitLines(bData)

	if o.brief {
		if equalLines(aLines, bLines, o) {
			if o.reportSame {
				fmt.Fprintf(os.Stdout, "Files %s and %s are identical\n", a, b)
			}
			return 0
		}
		fmt.Fprintf(os.Stdout, "Files %s and %s differ\n", a, b)
		return 1
	}

	keyA := keysFor(aLines, o)
	keyB := keysFor(bLines, o)
	hunks := myersHunks(keyA, keyB, o.contextLines)
	if len(hunks) == 0 {
		if o.reportSame {
			fmt.Fprintf(os.Stdout, "Files %s and %s are identical\n", a, b)
		}
		return 0
	}
	stA, _ := os.Stat(a)
	stB, _ := os.Stat(b)
	mtA := formatHeaderTime(statTime(stA))
	mtB := formatHeaderTime(statTime(stB))
	fmt.Fprintf(os.Stdout, "--- %s\t%s\n", a, mtA)
	fmt.Fprintf(os.Stdout, "+++ %s\t%s\n", b, mtB)
	for _, h := range hunks {
		printHunk(os.Stdout, h, aLines, bLines)
	}
	return 1
}

func readOrEmpty(p string, newFile bool) ([]byte, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) && newFile {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

func statTime(st fs.FileInfo) time.Time {
	if st == nil {
		return time.Now()
	}
	return st.ModTime()
}

func formatHeaderTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05.000000000 -0700")
}

func isBinary(b []byte) bool {
	// gnu's heuristic: NUL in the first ~8KB.
	limit := len(b)
	if limit > 8192 {
		limit = 8192
	}
	for i := 0; i < limit; i++ {
		if b[i] == 0 {
			return true
		}
	}
	return false
}

func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	s := string(b)
	parts := strings.SplitAfter(s, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func equalLines(a, b []string, o *options) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if keyOf(a[i], o) != keyOf(b[i], o) {
			return false
		}
	}
	return true
}

func keysFor(lines []string, o *options) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = keyOf(l, o)
	}
	return out
}

func keyOf(line string, o *options) string {
	if o.ignoreCase {
		line = strings.ToLower(line)
	}
	if o.ignoreAllSpace {
		line = strings.Join(strings.Fields(line), " ")
	}
	if o.ignoreBlankLine && strings.TrimSpace(line) == "" {
		return ""
	}
	return line
}

// hunk represents a unified-diff hunk: aStart/aLen and bStart/bLen
// are 1-based line numbers and lengths; edits are the lines (each
// prefixed with the operation kind).
type hunk struct {
	aStart, aLen int
	bStart, bLen int
	edits        []edit
}

type editKind int

const (
	editEq editKind = iota
	editDel
	editAdd
)

type edit struct {
	kind editKind
	aIdx int // 0-based index into A's lines (for eq/del)
	bIdx int // 0-based index into B's lines (for eq/add)
}

// myersHunks runs the linear-space Myers diff and groups the edit
// script into context-window-bounded hunks.
func myersHunks(a, b []string, ctx int) []hunk {
	script := myersScript(a, b)
	// Split eq runs that exceed 2*ctx — they bracket separate hunks.
	var hunks []hunk
	var cur hunk
	curEdits := 0
	pendingEq := 0
	resetCur := func() {
		cur = hunk{}
		curEdits = 0
		pendingEq = 0
	}
	resetCur()
	for _, e := range script {
		if e.kind == editEq {
			cur.edits = append(cur.edits, e)
			pendingEq++
			if pendingEq > 2*ctx+1 && curEdits > 0 {
				// flush previous hunk (drop trailing context above ctx)
				h := finalizeHunk(cur, ctx)
				hunks = append(hunks, h)
				resetCur()
				// Restart context window with the most recent ctx
				// lines from the just-emitted hunk.
				start := 0
				if len(h.edits) > ctx {
					start = len(h.edits) - ctx
				}
				cur.edits = append(cur.edits, h.edits[start:]...)
				curEdits = 0
				pendingEq = len(cur.edits)
			}
		} else {
			cur.edits = append(cur.edits, e)
			curEdits++
			pendingEq = 0
		}
	}
	if curEdits > 0 {
		hunks = append(hunks, finalizeHunk(cur, ctx))
	}
	return hunks
}

// finalizeHunk trims leading/trailing eq runs to at most ctx lines and
// computes the (1-based) start positions and lengths.
func finalizeHunk(h hunk, ctx int) hunk {
	// Trim leading eq beyond ctx.
	leadEq := 0
	for _, e := range h.edits {
		if e.kind != editEq {
			break
		}
		leadEq++
	}
	if leadEq > ctx {
		h.edits = h.edits[leadEq-ctx:]
	}
	// Trim trailing eq beyond ctx.
	trailEq := 0
	for i := len(h.edits) - 1; i >= 0; i-- {
		if h.edits[i].kind != editEq {
			break
		}
		trailEq++
	}
	if trailEq > ctx {
		h.edits = h.edits[:len(h.edits)-(trailEq-ctx)]
	}
	// Compute starting line numbers.
	aStart := 0
	bStart := 0
	for _, e := range h.edits {
		if e.kind != editAdd {
			aStart = e.aIdx + 1
			break
		}
	}
	for _, e := range h.edits {
		if e.kind != editDel {
			bStart = e.bIdx + 1
			break
		}
	}
	for _, e := range h.edits {
		switch e.kind {
		case editEq:
			h.aLen++
			h.bLen++
		case editDel:
			h.aLen++
		case editAdd:
			h.bLen++
		}
	}
	h.aStart = aStart
	h.bStart = bStart
	if h.aLen == 0 {
		h.aStart = max1(h.aStart-1, 0)
	}
	if h.bLen == 0 {
		h.bStart = max1(h.bStart-1, 0)
	}
	return h
}

func max1(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func printHunk(w *os.File, h hunk, a, b []string) {
	fmt.Fprintf(w, "@@ -%d,%d +%d,%d @@\n", h.aStart, h.aLen, h.bStart, h.bLen)
	for _, e := range h.edits {
		switch e.kind {
		case editEq:
			fmt.Fprintf(w, " %s", a[e.aIdx])
			if !strings.HasSuffix(a[e.aIdx], "\n") {
				fmt.Fprintln(w)
				fmt.Fprintln(w, `\ No newline at end of file`)
			}
		case editDel:
			fmt.Fprintf(w, "-%s", a[e.aIdx])
			if !strings.HasSuffix(a[e.aIdx], "\n") {
				fmt.Fprintln(w)
				fmt.Fprintln(w, `\ No newline at end of file`)
			}
		case editAdd:
			fmt.Fprintf(w, "+%s", b[e.bIdx])
			if !strings.HasSuffix(b[e.bIdx], "\n") {
				fmt.Fprintln(w)
				fmt.Fprintln(w, `\ No newline at end of file`)
			}
		}
	}
}

// myersScript returns the full edit script (eq/del/add) needed to
// transform a into b. Standard Myers algorithm with backtrace from a
// trace of the v arrays — readable, not the fastest.
func myersScript(a, b []string) []edit {
	n, m := len(a), len(b)
	max := n + m
	if max == 0 {
		return nil
	}
	v := make(map[int]int) // k → x
	v[1] = 0
	var trace []map[int]int
	for d := 0; d <= max; d++ {
		snap := make(map[int]int, len(v))
		for k, x := range v {
			snap[k] = x
		}
		trace = append(trace, snap)

		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[k-1] < v[k+1]) {
				x = v[k+1]
			} else {
				x = v[k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[k] = x
			if x >= n && y >= m {
				return backtrace(trace, a, b, n, m)
			}
		}
	}
	return backtrace(trace, a, b, n, m)
}

func backtrace(trace []map[int]int, a, b []string, n, m int) []edit {
	x, y := n, m
	var path []edit
	for d := len(trace) - 1; d > 0; d-- {
		v := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && v[k-1] < v[k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := v[prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			path = append(path, edit{kind: editEq, aIdx: x - 1, bIdx: y - 1})
			x--
			y--
		}
		if d > 0 {
			if x == prevX {
				path = append(path, edit{kind: editAdd, aIdx: x, bIdx: y - 1})
			} else {
				path = append(path, edit{kind: editDel, aIdx: x - 1, bIdx: y})
			}
		}
		x = prevX
		y = prevY
	}
	// Reverse to get forward order.
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}
