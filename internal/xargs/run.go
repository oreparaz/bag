package xargs

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type options struct {
	delim       byte
	delimSet    bool
	nullSep     bool
	maxArgs     int
	maxLines    int
	replace     string
	noEmpty     bool
	verbose     bool
	interactive bool
	argFile     string
	maxChars    int
	cmd         []string
}

func parseArgs(argv []string) (*options, error) {
	o := &options{}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			o.cmd = append(o.cmd, argv[i+1:]...)
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
			case "null":
				o.nullSep = true
				o.delim = 0
				o.delimSet = true
			case "delimiter":
				if !hasEq {
					if i+1 >= len(argv) {
						return nil, errors.New("--delimiter requires an argument")
					}
					i++
					val = argv[i]
				}
				if len(val) != 1 {
					return nil, fmt.Errorf("delimiter must be a single byte, got %q", val)
				}
				o.delim = val[0]
				o.delimSet = true
			case "max-args":
				if !hasEq {
					if i+1 >= len(argv) {
						return nil, errors.New("--max-args requires an argument")
					}
					i++
					val = argv[i]
				}
				n, err := strconv.Atoi(val)
				if err != nil || n < 1 {
					return nil, fmt.Errorf("invalid --max-args: %q", val)
				}
				o.maxArgs = n
			case "replace":
				if !hasEq {
					val = "{}"
				}
				o.replace = val
			case "no-run-if-empty":
				o.noEmpty = true
			case "verbose":
				o.verbose = true
			case "interactive":
				o.interactive = true
			case "arg-file":
				if !hasEq {
					if i+1 >= len(argv) {
						return nil, errors.New("--arg-file requires an argument")
					}
					i++
					val = argv[i]
				}
				o.argFile = val
			case "max-chars":
				if !hasEq {
					if i+1 >= len(argv) {
						return nil, errors.New("--max-chars requires an argument")
					}
					i++
					val = argv[i]
				}
				n, err := strconv.Atoi(val)
				if err != nil || n < 1 {
					return nil, fmt.Errorf("invalid --max-chars: %q", val)
				}
				o.maxChars = n
			default:
				// Ignore unknown long flags.
			}
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" && len(a) > 1 && isFlagCluster(a) {
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case '0':
					o.nullSep = true
					o.delim = 0
					o.delimSet = true
				case 'r':
					o.noEmpty = true
				case 't':
					o.verbose = true
				case 'p':
					o.interactive = true
				case 'd':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-d requires an argument")
						}
						i++
						val = argv[i]
					}
					if len(val) != 1 {
						return nil, fmt.Errorf("delimiter must be a single byte, got %q", val)
					}
					o.delim = val[0]
					o.delimSet = true
				case 'n':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-n requires an argument")
						}
						i++
						val = argv[i]
					}
					n, err := strconv.Atoi(val)
					if err != nil || n < 1 {
						return nil, fmt.Errorf("invalid -n value: %q", val)
					}
					o.maxArgs = n
				case 'L':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-L requires an argument")
						}
						i++
						val = argv[i]
					}
					n, err := strconv.Atoi(val)
					if err != nil || n < 1 {
						return nil, fmt.Errorf("invalid -L value: %q", val)
					}
					o.maxLines = n
				case 'I':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-I requires an argument")
						}
						i++
						val = argv[i]
					}
					o.replace = val
				case 'a':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-a requires an argument")
						}
						i++
						val = argv[i]
					}
					o.argFile = val
				case 's':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-s requires an argument")
						}
						i++
						val = argv[i]
					}
					n, err := strconv.Atoi(val)
					if err != nil || n < 1 {
						return nil, fmt.Errorf("invalid -s value: %q", val)
					}
					o.maxChars = n
				default:
					// Ignore unknown short flags.
				}
			}
			continue
		}
		o.cmd = append(o.cmd, a)
	}

	if len(o.cmd) == 0 {
		o.cmd = []string{"/bin/echo"}
	}
	if o.maxChars == 0 {
		o.maxChars = 128 * 1024 // generous default; well under ARG_MAX
	}
	return o, nil
}

// isFlagCluster reports whether the argv element looks like a chain of
// known short flags (used to avoid eating "-X" when X is a command,
// rare but possible).
func isFlagCluster(s string) bool {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '0', 'r', 't', 'p', 'd', 'n', 'L', 'I', 'a', 's':
		default:
			return false
		}
	}
	return true
}

func run(argv []string) int {
	o, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "xargs: %v\n", err)
		return 1
	}

	r, err := openInput(o)
	if err != nil {
		fmt.Fprintf(os.Stderr, "xargs: %v\n", err)
		return 1
	}
	defer r.Close()

	items, err := readItems(r, o)
	if err != nil {
		fmt.Fprintf(os.Stderr, "xargs: %v\n", err)
		return 1
	}

	if len(items) == 0 && o.noEmpty {
		return 0
	}

	if o.replace != "" {
		return runReplaceMode(o, items)
	}
	return runBatchMode(o, items)
}

func openInput(o *options) (io.ReadCloser, error) {
	if o.argFile != "" {
		return os.Open(o.argFile)
	}
	return io.NopCloser(os.Stdin), nil
}

// readItems splits the input into items. With -0 the separator is NUL;
// with -d DELIM, that single byte. Otherwise whitespace (\t \n \v \f ' ').
func readItems(r io.Reader, o *options) ([]string, error) {
	if o.delimSet {
		return splitOnDelim(r, o.delim)
	}
	return splitOnWhitespace(r)
}

func splitOnDelim(r io.Reader, delim byte) ([]string, error) {
	br := bufio.NewReader(r)
	var items []string
	var cur strings.Builder
	for {
		b, err := br.ReadByte()
		if err != nil {
			if err == io.EOF {
				if cur.Len() > 0 {
					items = append(items, cur.String())
				}
				return items, nil
			}
			return nil, err
		}
		if b == delim {
			items = append(items, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(b)
	}
}

func splitOnWhitespace(r io.Reader) ([]string, error) {
	br := bufio.NewReader(r)
	var items []string
	var cur strings.Builder
	for {
		b, err := br.ReadByte()
		if err != nil {
			if err == io.EOF {
				if cur.Len() > 0 {
					items = append(items, cur.String())
				}
				return items, nil
			}
			return nil, err
		}
		if isWhitespace(b) {
			if cur.Len() > 0 {
				items = append(items, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteByte(b)
	}
}

func isWhitespace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

// runBatchMode groups items into batches of at most maxArgs and at
// most maxChars characters, then invokes the command for each batch.
// When maxArgs is 0 (the default), all items become one batch unless
// the char-budget forces splitting.
func runBatchMode(o *options, items []string) int {
	if len(items) == 0 {
		// gnu runs the command once with no extra args (unless -r).
		return spawn(o.cmd)
	}

	cmdLen := 0
	for _, a := range o.cmd {
		cmdLen += len(a) + 1
	}
	budget := o.maxChars - cmdLen
	if budget < 0 {
		budget = 0
	}

	worst := 0
	var batch []string
	flush := func() int {
		if len(batch) == 0 {
			return 0
		}
		argv := append(append([]string{}, o.cmd...), batch...)
		if o.verbose {
			fmt.Fprintln(os.Stderr, strings.Join(argv, " "))
		}
		if o.interactive && !confirm(argv) {
			batch = batch[:0]
			return 0
		}
		ex := spawn(argv)
		batch = batch[:0]
		return ex
	}

	exit := 0
	for _, it := range items {
		if o.maxArgs > 0 && len(batch) == o.maxArgs {
			if e := flush(); e != 0 {
				exit = e
			}
		}
		// Char budget.
		if worst+len(it)+1 > budget && len(batch) > 0 {
			if e := flush(); e != 0 {
				exit = e
			}
			worst = 0
		}
		batch = append(batch, it)
		worst += len(it) + 1
	}
	if e := flush(); e != 0 {
		exit = e
	}
	return exit
}

func runReplaceMode(o *options, items []string) int {
	exit := 0
	for _, it := range items {
		argv := make([]string, len(o.cmd))
		for i, a := range o.cmd {
			argv[i] = strings.ReplaceAll(a, o.replace, it)
		}
		if o.verbose {
			fmt.Fprintln(os.Stderr, strings.Join(argv, " "))
		}
		if o.interactive && !confirm(argv) {
			continue
		}
		if e := spawn(argv); e != 0 {
			exit = e
		}
	}
	return exit
}

func spawn(argv []string) int {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "xargs: %s: %v\n", argv[0], err)
		return 127
	}
	return 0
}

func confirm(argv []string) bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer tty.Close()
	fmt.Fprintf(tty, "%s ?...", strings.Join(argv, " "))
	line, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y")
}
