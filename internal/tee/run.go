package tee

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

type options struct {
	files []string

	append          bool
	ignoreInterrupts bool

	printHelp    bool
	printVersion bool
}

func run(args []string) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tee: %v\n", err)
		return 1
	}
	if o.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if o.printVersion {
		fmt.Println("tee (bag) -- bag drop-in")
		return 0
	}

	if o.ignoreInterrupts {
		// Ignore SIGINT so a Ctrl-C in the pipeline doesn't kill us before
		// our writes flush. Real tee uses sigaction(SIGINT, SIG_IGN).
		signal.Ignore(syscall.SIGINT)
	}

	// Open all output files. On any open error we still continue with the
	// successful ones (matches GNU tee's default behavior).
	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if o.append {
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	writers := []io.Writer{os.Stdout}
	closers := []io.Closer{}
	exit := 0
	for _, name := range o.files {
		f, err := os.OpenFile(name, flag, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tee: %s: %v\n", name, err)
			exit = 1
			continue
		}
		writers = append(writers, f)
		closers = append(closers, f)
	}
	mw := io.MultiWriter(writers...)

	if _, err := io.Copy(mw, os.Stdin); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(os.Stderr, "tee: %v\n", err)
		exit = 1
	}
	for _, c := range closers {
		_ = c.Close()
	}
	return exit
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
			case "append":
				o.append = true
			case "ignore-interrupts":
				o.ignoreInterrupts = true
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
			case 'a':
				o.append = true
			case 'i':
				o.ignoreInterrupts = true
			default:
				return nil, fmt.Errorf("unknown option -%c", a[j])
			}
		}
		i++
	}
	return o, nil
}

func printHelp(w io.Writer) {
	const help = `Usage: tee [OPTION]... [FILE]...
Copy standard input to each FILE, and also to standard output.

  -a, --append              append to the given FILEs, do not overwrite
  -i, --ignore-interrupts   ignore interrupt signals
      --help                display this help and exit
      --version             output version information and exit
`
	io.WriteString(w, help)
}
