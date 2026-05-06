package ssh

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"strconv"
	"strings"
)

// options is the parsed argv.
type options struct {
	user    string
	host    string
	port    int
	command []string

	identityFile string

	// Override known_hosts location (testing).
	knownHostsPath string

	// Skip host key check (-o StrictHostKeyChecking=no). Useful for
	// throwaway test boxes; never on by default.
	insecure bool

	// Force PTY allocation when running a command (-t).
	forceTTY bool

	// Disable PTY entirely even for shells (-T).
	noTTY bool

	verbose bool

	printHelp    bool
	printVersion bool
}

func run(args []string) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ssh: %v\n", err)
		return 255
	}
	if o.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if o.printVersion {
		fmt.Println("ssh (bag) -- bag drop-in")
		return 0
	}
	if o.host == "" {
		fmt.Fprintln(os.Stderr, "ssh: missing host")
		return 255
	}

	if o.port == 0 {
		o.port = 22
	}
	if o.user == "" {
		if u, err := user.Current(); err == nil {
			o.user = u.Username
		} else {
			o.user = "root"
		}
	}

	if err := connectAndRun(o); err != nil {
		// Surface the underlying message; pass through exit codes from
		// remote commands when we have one.
		var ec *exitCodeError
		if errors.As(err, &ec) {
			return ec.code
		}
		fmt.Fprintf(os.Stderr, "ssh: %v\n", err)
		return 255
	}
	return 0
}

// exitCodeError carries a remote-command exit code through to the
// process exit. We use a typed error so the dispatcher can pull the
// code out without parsing a string.
type exitCodeError struct {
	code int
	msg  string
}

func (e *exitCodeError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return fmt.Sprintf("remote exit %d", e.code)
}

// parseArgs accepts the subset of openssh flags bag's ssh supports.
//
//	ssh [-p PORT] [-i IDENT] [-l USER] [-t] [-T] [-v]
//	    [-o StrictHostKeyChecking=no]
//	    [-o UserKnownHostsFile=PATH]
//	    [USER@]HOST [COMMAND...]
func parseArgs(args []string) (*options, error) {
	o := &options{}
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "":
			i++
		case a == "--help" || a == "-h":
			o.printHelp = true
			i++
		case a == "--version":
			o.printVersion = true
			i++
		case a == "-v":
			o.verbose = true
			i++
		case a == "-t":
			o.forceTTY = true
			i++
		case a == "-T":
			o.noTTY = true
			i++
		case a == "-p":
			if i+1 >= len(args) {
				return nil, errors.New("-p requires a port")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 1 || n > 65535 {
				return nil, fmt.Errorf("invalid port %q", args[i+1])
			}
			o.port = n
			i += 2
		case strings.HasPrefix(a, "-p"):
			n, err := strconv.Atoi(a[2:])
			if err != nil || n < 1 || n > 65535 {
				return nil, fmt.Errorf("invalid port %q", a[2:])
			}
			o.port = n
			i++
		case a == "-l":
			if i+1 >= len(args) {
				return nil, errors.New("-l requires a username")
			}
			o.user = args[i+1]
			i += 2
		case a == "-i":
			if i+1 >= len(args) {
				return nil, errors.New("-i requires a path")
			}
			o.identityFile = args[i+1]
			i += 2
		case a == "-o":
			if i+1 >= len(args) {
				return nil, errors.New("-o requires KEY=VALUE")
			}
			if err := applyOption(o, args[i+1]); err != nil {
				return nil, err
			}
			i += 2
		default:
			if o.host == "" {
				h := a
				if at := strings.IndexByte(h, '@'); at >= 0 {
					o.user = h[:at]
					h = h[at+1:]
				}
				o.host = h
				i++
				// Everything after the host is the remote command.
				o.command = append([]string{}, args[i:]...)
				return o, nil
			}
			i++
		}
	}
	return o, nil
}

// applyOption handles -o KEY=VALUE for the small set we recognise.
func applyOption(o *options, kv string) error {
	parts := strings.SplitN(kv, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("-o needs KEY=VALUE, got %q", kv)
	}
	key := strings.ToLower(strings.TrimSpace(parts[0]))
	val := strings.TrimSpace(parts[1])
	switch key {
	case "stricthostkeychecking":
		switch strings.ToLower(val) {
		case "no", "off", "false":
			o.insecure = true
		case "yes", "on", "true", "ask":
			o.insecure = false
		default:
			return fmt.Errorf("invalid StrictHostKeyChecking=%q", val)
		}
	case "userknownhostsfile":
		o.knownHostsPath = val
	default:
		// Unknown option: ignore (matches openssh's lenience).
	}
	return nil
}

func printHelp(w io.Writer) {
	const help = `Usage: ssh [-p PORT] [-i IDENT] [-l USER] [-t] [-T] [-v]
           [-o KEY=VALUE]... [USER@]HOST [COMMAND...]

Connect to a remote host over SSH. With no COMMAND, opens an
interactive shell. With a COMMAND, runs it remotely and exits with
its status.

  -p PORT           connect to PORT (default 22)
  -l USER           log in as USER
  -i IDENT          private key file (default ~/.ssh/id_ed25519,
                    id_ecdsa, id_rsa)
  -t                force PTY allocation for the command
  -T                disable PTY entirely
  -v                verbose
  -o KEY=VALUE      recognised KEYs:
                      StrictHostKeyChecking={yes|no}
                      UserKnownHostsFile=PATH
      --help        display this help
      --version     display version
`
	io.WriteString(w, help)
}
