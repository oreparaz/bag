package cat

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type options struct {
	files []string

	number         bool // -n
	numberNonBlank bool // -b
	squeezeBlank   bool // -s
	showEnds       bool // -E
	showTabs       bool // -T
	showNonprint   bool // -v

	printHelp    bool
	printVersion bool
}

func run(args []string) int {
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cat: %v\n", err)
		fmt.Fprintln(os.Stderr, "Try 'cat --help' for more information.")
		return 1
	}
	if opts.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if opts.printVersion {
		fmt.Println("cat (bag) -- bag drop-in")
		return 0
	}

	if opts.numberNonBlank {
		// -b overrides -n in real cat.
		opts.number = false
	}

	files := opts.files
	if len(files) == 0 {
		files = []string{"-"}
	}

	st := &state{opts: opts, out: bufio.NewWriter(os.Stdout)}
	defer st.out.Flush()

	exit := 0
	for _, f := range files {
		if err := st.copyFile(f); err != nil {
			fmt.Fprintf(os.Stderr, "cat: %s: %v\n", f, err)
			exit = 1
		}
	}
	return exit
}

type state struct {
	opts        *options
	out         *bufio.Writer
	lineNo      int
	prevBlank   bool
	atLineStart bool
	startedLine bool // whether current logical line emitted a number prefix yet
}

func (s *state) copyFile(name string) error {
	var r io.ReadCloser
	if name == "-" {
		r = io.NopCloser(os.Stdin)
	} else {
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		r = f
	}
	defer r.Close()

	// Fast path: no flags that mutate output. Stream raw bytes.
	if !s.opts.number && !s.opts.numberNonBlank && !s.opts.squeezeBlank &&
		!s.opts.showEnds && !s.opts.showTabs && !s.opts.showNonprint {
		_, err := io.Copy(s.out, r)
		return err
	}

	br := bufio.NewReader(r)
	// We need line-aware processing for numbering, squeeze, and showEnds.
	// Read until '\n' (or EOF). Other rendering happens per-byte.
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			isBlank := isBlankLine(line)
			if s.opts.squeezeBlank && isBlank && s.prevBlank {
				continue
			}
			s.prevBlank = isBlank

			needsNumber := s.opts.number || (s.opts.numberNonBlank && !isBlank)
			if needsNumber {
				s.lineNo++
				fmt.Fprintf(s.out, "%6d\t", s.lineNo)
			}
			s.writeLineBytes(line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// writeLineBytes renders one input line (which may include a trailing newline)
// applying -E/-T/-v transformations.
func (s *state) writeLineBytes(line []byte) {
	for i := 0; i < len(line); i++ {
		b := line[i]
		switch {
		case b == '\n':
			if s.opts.showEnds {
				s.out.WriteByte('$')
			}
			s.out.WriteByte('\n')
		case b == '\t':
			if s.opts.showTabs {
				s.out.WriteString("^I")
			} else {
				s.out.WriteByte('\t')
			}
		case s.opts.showNonprint:
			renderNonprint(s.out, b)
		default:
			s.out.WriteByte(b)
		}
	}
}

// renderNonprint writes b in caret + meta notation, matching coreutils cat -v.
//
// Rules (only relevant when showNonprint is on):
//   - 0..31    -> ^@ ^A ... ^_   (skipping \t and \n which are handled above)
//   - 127      -> ^?
//   - 128..255 -> M- + recurse into the same scheme on (b - 128)
//   - 32..126  -> as-is
func renderNonprint(w *bufio.Writer, b byte) {
	switch {
	case b < 32:
		w.WriteByte('^')
		w.WriteByte(b + '@')
	case b == 127:
		w.WriteString("^?")
	case b >= 128:
		w.WriteString("M-")
		renderNonprint(w, b-128)
	default:
		w.WriteByte(b)
	}
}

func isBlankLine(line []byte) bool {
	if len(line) == 0 {
		return true
	}
	if len(line) == 1 && line[0] == '\n' {
		return true
	}
	return false
}

// parseArgs is GNU-cat compatible:
//   - long flags --name (no '=' values needed; cat takes no args to its flags)
//   - clustered shorts -ETs
//   - "-" means stdin
//   - "--" ends flag parsing
func parseArgs(args []string) (*options, error) {
	o := &options{}
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "" || a == "-" {
			o.files = append(o.files, a)
			i++
			continue
		}
		if a[0] != '-' {
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
			if err := applyLong(o, a[2:]); err != nil {
				return nil, err
			}
			i++
			continue
		}
		// short cluster
		for j := 1; j < len(a); j++ {
			if err := applyShort(o, a[j]); err != nil {
				return nil, err
			}
		}
		i++
	}
	return o, nil
}

func applyLong(o *options, name string) error {
	switch name {
	case "number":
		o.number = true
	case "number-nonblank":
		o.numberNonBlank = true
	case "squeeze-blank":
		o.squeezeBlank = true
	case "show-ends":
		o.showEnds = true
	case "show-tabs":
		o.showTabs = true
	case "show-nonprinting":
		o.showNonprint = true
	case "show-all":
		o.showEnds = true
		o.showTabs = true
		o.showNonprint = true
	case "help":
		o.printHelp = true
	case "version":
		o.printVersion = true
	default:
		return fmt.Errorf("unrecognized option '--%s'", name)
	}
	return nil
}

func applyShort(o *options, c byte) error {
	switch c {
	case 'n':
		o.number = true
	case 'b':
		o.numberNonBlank = true
	case 's':
		o.squeezeBlank = true
	case 'E':
		o.showEnds = true
	case 'T':
		o.showTabs = true
	case 'v':
		o.showNonprint = true
	case 'A':
		o.showEnds = true
		o.showTabs = true
		o.showNonprint = true
	case 'e':
		o.showEnds = true
		o.showNonprint = true
	case 't':
		o.showTabs = true
		o.showNonprint = true
	case 'u':
		// POSIX: unbuffered output. Accepted; we flush at exit anyway.
	default:
		return fmt.Errorf("invalid option -- '%c'", c)
	}
	return nil
}

func printHelp(w io.Writer) {
	const help = `Usage: cat [OPTION]... [FILE]...
Concatenate FILE(s) to standard output.
With no FILE, or when FILE is -, read standard input.

  -A, --show-all           equivalent to -vET
  -b, --number-nonblank    number nonempty output lines, overrides -n
  -e                       equivalent to -vE
  -E, --show-ends          display $ at end of each line
  -n, --number             number all output lines
  -s, --squeeze-blank      suppress repeated empty output lines
  -t                       equivalent to -vT
  -T, --show-tabs          display TAB characters as ^I
  -u                       (ignored)
  -v, --show-nonprinting   use ^ and M- notation, except for LFD and TAB
      --help               display this help and exit
      --version            output version information and exit
`
	io.WriteString(w, help)
}
