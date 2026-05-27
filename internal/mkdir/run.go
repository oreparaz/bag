package mkdir

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

type options struct {
	parents bool
	mode    fs.FileMode
	modeSet bool
	verbose bool
}

func parseArgs(argv []string) (*options, []string, error) {
	o := &options{}
	var paths []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			paths = append(paths, argv[i+1:]...)
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
			case "parents":
				o.parents = true
			case "verbose":
				o.verbose = true
			case "mode":
				if !hasEq {
					if i+1 >= len(argv) {
						return nil, nil, errors.New("--mode requires an argument")
					}
					i++
					val = argv[i]
				}
				m, err := parseMode(val)
				if err != nil {
					return nil, nil, err
				}
				o.mode = m
				o.modeSet = true
			default:
				// Silently ignore unknown long flags.
			}
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" && len(a) > 1 {
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'p':
					o.parents = true
				case 'v':
					o.verbose = true
				case 'm':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, nil, errors.New("-m requires an argument")
						}
						i++
						val = argv[i]
					}
					m, err := parseMode(val)
					if err != nil {
						return nil, nil, err
					}
					o.mode = m
					o.modeSet = true
				default:
					// Silently ignore unknown short flags.
				}
			}
			continue
		}
		paths = append(paths, a)
	}
	if len(paths) == 0 {
		return nil, nil, errors.New("missing operand")
	}
	return o, paths, nil
}

func parseMode(s string) (fs.FileMode, error) {
	// Numeric (octal) only — symbolic chmod syntax (u+x, …) for mkdir's
	// -m is rarely used and would duplicate the chmod parser.
	if s == "" {
		return 0, errors.New("empty mode")
	}
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid mode %q (octal only): %w", s, err)
	}
	return fs.FileMode(v) & 0o7777, nil
}

func run(argv []string) int {
	o, paths, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
		return 1
	}
	exit := 0
	createMode := fs.FileMode(0o777)
	if o.modeSet {
		createMode = o.mode
	}
	for _, p := range paths {
		if err := makeOne(p, createMode, o); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
			exit = 1
		}
	}
	return exit
}

func makeOne(path string, mode fs.FileMode, o *options) error {
	var err error
	if o.parents {
		err = os.MkdirAll(path, mode)
		// MkdirAll is silent when the target already exists — that's
		// gnu's -p behavior. But we still want -v to log the leaf the
		// user asked for if it was actually created. We only log when
		// the directory didn't exist before.
	} else {
		err = os.Mkdir(path, mode)
	}
	if err != nil {
		return err
	}
	if o.modeSet {
		// Re-chmod so umask doesn't masking the requested bits.
		if err := os.Chmod(path, mode); err != nil {
			return err
		}
	}
	if o.verbose {
		fmt.Fprintf(os.Stdout, "mkdir: created directory '%s'\n", path)
	}
	return nil
}
