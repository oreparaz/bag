package wc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

type options struct {
	files []string

	lines   bool
	words   bool
	bytes_  bool
	chars   bool
	maxLine bool

	printHelp    bool
	printVersion bool
}

type counts struct {
	lines   int64
	words   int64
	bytes_  int64
	chars   int64
	maxLine int64
}

func (c *counts) add(o counts) {
	c.lines += o.lines
	c.words += o.words
	c.bytes_ += o.bytes_
	c.chars += o.chars
	if o.maxLine > c.maxLine {
		c.maxLine = o.maxLine
	}
}

func run(args []string) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wc: %v\n", err)
		return 1
	}
	if o.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if o.printVersion {
		fmt.Println("wc (bag) -- bag drop-in")
		return 0
	}

	// If no count selectors given, default is -l -w -c.
	defaultMode := !o.lines && !o.words && !o.bytes_ && !o.chars && !o.maxLine

	files := o.files
	if len(files) == 0 {
		files = []string{"-"}
	}

	results := make([]struct {
		name string
		c    counts
		err  error
	}, len(files))

	for i, name := range files {
		c, err := count(name)
		results[i].name = name
		results[i].c = c
		results[i].err = err
	}

	bw := bufio.NewWriter(os.Stdout)
	defer bw.Flush()

	// Width = max width of any number we'll print, across all rows
	// including the total. GNU wc uses a per-column min-width 7 for
	// stdin-only input and adapts otherwise. We follow.
	widths := computeWidths(results, o, defaultMode)

	exit := 0
	var total counts
	multipleFiles := len(files) > 1
	for _, r := range results {
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "wc: %s: %v\n", r.name, r.err)
			exit = 1
			continue
		}
		total.add(r.c)
		writeRow(bw, r.c, displayName(r.name), o, defaultMode, widths)
	}
	if multipleFiles && exit == 0 {
		writeRow(bw, total, "total", o, defaultMode, widths)
	}
	return exit
}

func count(name string) (counts, error) {
	var c counts
	r, closer, err := openInput(name)
	if err != nil {
		return c, err
	}
	defer closer()

	utf8Locale := isUTF8Locale()

	br := bufio.NewReaderSize(r, 64*1024)
	inWord := false
	var lineLen int64

	buf := make([]byte, 32*1024)
	for {
		n, err := br.Read(buf)
		if n > 0 {
			c.bytes_ += int64(n)
			if utf8Locale {
				c.chars += int64(utf8.RuneCount(buf[:n]))
			} else {
				// Under the C / POSIX locale, GNU wc treats -m the same as -c.
				c.chars += int64(n)
			}
			for _, b := range buf[:n] {
				if b == '\n' {
					c.lines++
					if lineLen > c.maxLine {
						c.maxLine = lineLen
					}
					lineLen = 0
				} else {
					lineLen++
				}
				if isWS(b) {
					inWord = false
				} else if !inWord {
					inWord = true
					c.words++
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if lineLen > c.maxLine {
					c.maxLine = lineLen
				}
				return c, nil
			}
			return c, err
		}
	}
}

// isUTF8Locale reports whether the active LC_ALL / LC_CTYPE / LANG selects
// a UTF-8 character set. Under the C / POSIX locale, GNU wc -m falls back
// to counting bytes; we match that behavior.
func isUTF8Locale() bool {
	for _, v := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if val := os.Getenv(v); val != "" {
			u := strings.ToUpper(val)
			if strings.Contains(u, "UTF-8") || strings.Contains(u, "UTF8") {
				return true
			}
			// Some non-UTF-8 explicit setting.
			return false
		}
	}
	return false
}

func isWS(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

// computeWidths matches GNU wc: width is the number of decimal digits in
// the largest value that will actually be printed (across all selected
// columns and rows including the total row if any). When any input is
// stdin (size unknown a priori), the width is at least 7 — GNU wc uses
// that as a lookahead margin.
func computeWidths(results []struct {
	name string
	c    counts
	err  error
}, o *options, defaultMode bool) int {
	maxN := int64(0)
	check := func(v int64) {
		if v > maxN {
			maxN = v
		}
	}
	var total counts
	multi := len(results) > 1
	hasStdin := false
	for _, r := range results {
		if r.name == "-" {
			hasStdin = true
		}
		total.add(r.c)
		if defaultMode || o.lines {
			check(r.c.lines)
		}
		if defaultMode || o.words {
			check(r.c.words)
		}
		if defaultMode || o.bytes_ {
			check(r.c.bytes_)
		}
		if o.chars {
			check(r.c.chars)
		}
		if o.maxLine {
			check(r.c.maxLine)
		}
	}
	if multi {
		if defaultMode || o.lines {
			check(total.lines)
		}
		if defaultMode || o.words {
			check(total.words)
		}
		if defaultMode || o.bytes_ {
			check(total.bytes_)
		}
		if o.chars {
			check(total.chars)
		}
		if o.maxLine {
			check(total.maxLine)
		}
	}
	w := len(strconv.FormatInt(maxN, 10))
	if hasStdin && w < 7 {
		w = 7
	}
	if w < 1 {
		w = 1
	}
	return w
}

func writeRow(w *bufio.Writer, c counts, name string, o *options, defaultMode bool, width int) {
	first := true
	emit := func(v int64) {
		if !first {
			w.WriteByte(' ')
		}
		fmt.Fprintf(w, "%*d", width, v)
		first = false
	}
	if defaultMode || o.lines {
		emit(c.lines)
	}
	if defaultMode || o.words {
		emit(c.words)
	}
	if defaultMode || o.bytes_ {
		emit(c.bytes_)
	}
	if o.chars {
		emit(c.chars)
	}
	if o.maxLine {
		emit(c.maxLine)
	}
	if name != "" {
		w.WriteByte(' ')
		w.WriteString(name)
	}
	w.WriteByte('\n')
}

func openInput(name string) (io.Reader, func(), error) {
	if name == "-" {
		return os.Stdin, func() {}, nil
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { f.Close() }, nil
}

func displayName(name string) string {
	if name == "-" {
		return ""
	}
	return name
}

func parseArgs(args []string) (*options, error) {
	o := &options{}
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "" || a == "-" || a[0] != '-' {
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
			switch a[2:] {
			case "lines":
				o.lines = true
			case "words":
				o.words = true
			case "bytes":
				o.bytes_ = true
			case "chars":
				o.chars = true
			case "max-line-length":
				o.maxLine = true
			case "help":
				o.printHelp = true
			case "version":
				o.printVersion = true
			default:
				return nil, fmt.Errorf("unrecognized option '%s'", a)
			}
			i++
			continue
		}
		for j := 1; j < len(a); j++ {
			switch a[j] {
			case 'l':
				o.lines = true
			case 'w':
				o.words = true
			case 'c':
				o.bytes_ = true
			case 'm':
				o.chars = true
			case 'L':
				o.maxLine = true
			default:
				return nil, fmt.Errorf("unknown option -%c", a[j])
			}
		}
		i++
	}
	return o, nil
}

func printHelp(w io.Writer) {
	const help = `Usage: wc [OPTION]... [FILE]...
Print newline, word, and byte counts for each FILE, and a total line if
more than one FILE is specified.  With no FILE, or when FILE is -, read
standard input.

  -c, --bytes              print the byte counts
  -m, --chars              print the character counts (UTF-8)
  -l, --lines              print the newline counts
  -L, --max-line-length    print the maximum display width
  -w, --words              print the word counts
      --help               display this help and exit
      --version            output version information and exit
`
	io.WriteString(w, help)
}

