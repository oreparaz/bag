package scp

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"strconv"
	"strings"

	"github.com/oreparaz/bag/internal/sshconn"
	"golang.org/x/crypto/ssh"
)

type options struct {
	srcs []endpoint
	dst  endpoint

	recursive   bool
	preserve    bool
	quiet       bool
	port        int
	identityKey string
	knownHosts  string
	insecure    bool
	verbose     bool

	printHelp    bool
	printVersion bool
}

// endpoint is one path argument: either local (host=="") or
// [user@]host:path.
type endpoint struct {
	user string
	host string
	path string
}

func (e endpoint) isRemote() bool { return e.host != "" }

func (e endpoint) String() string {
	if !e.isRemote() {
		return e.path
	}
	if e.user != "" {
		return e.user + "@" + e.host + ":" + e.path
	}
	return e.host + ":" + e.path
}

// parseEndpoint splits arg into endpoint pieces. Heuristic for "is this
// remote?": presence of a colon, with the part before the colon NOT
// containing a slash (so /a:b stays local). Matches scp's behavior.
//
// IPv6 literals are written as `[host]:path` (or `user@[host]:path`); the
// brackets disambiguate the colons in the address from the colon that
// separates host from path.
func parseEndpoint(arg string) endpoint {
	user := ""
	rest := arg
	if at := strings.IndexByte(rest, '@'); at >= 0 {
		// Be careful: an '@' could appear inside a path. Treat it as the
		// user separator only when no slash precedes it.
		if !strings.ContainsAny(rest[:at], "/\\") {
			user = rest[:at]
			rest = rest[at+1:]
		}
	}
	if strings.HasPrefix(rest, "[") {
		// [ipv6]:path form.
		end := strings.IndexByte(rest, ']')
		if end > 0 && end+1 < len(rest) && rest[end+1] == ':' {
			host := rest[1:end]
			path := rest[end+2:]
			return endpoint{user: user, host: host, path: path}
		}
		// Bracket without proper closing → treat as local path.
		return endpoint{path: arg}
	}
	colon := strings.IndexByte(rest, ':')
	if colon < 0 {
		// No colon → local path; user@... without colon is also local.
		return endpoint{path: arg}
	}
	hostPart := rest[:colon]
	if strings.ContainsAny(hostPart, "/\\") {
		return endpoint{path: arg}
	}
	pathPart := rest[colon+1:]
	return endpoint{user: user, host: hostPart, path: pathPart}
}

func run(args []string) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scp: %v\n", err)
		return 1
	}
	if o.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if o.printVersion {
		fmt.Println("scp (bag) -- bag drop-in")
		return 0
	}
	if len(o.srcs) == 0 {
		fmt.Fprintln(os.Stderr, "scp: missing source")
		return 1
	}

	if err := dispatch(o); err != nil {
		fmt.Fprintf(os.Stderr, "scp: %v\n", err)
		return 1
	}
	return 0
}

// dispatch decides whether this is an upload or a download and runs it.
// Mixed-direction (some local, some remote sources) is rejected as it
// would require two SSH connections — not in our 80%.
func dispatch(o *options) error {
	allRemote := true
	allLocal := true
	for _, s := range o.srcs {
		if s.isRemote() {
			allLocal = false
		} else {
			allRemote = false
		}
	}

	switch {
	case allLocal && o.dst.isRemote():
		return runUpload(o)
	case allRemote && !o.dst.isRemote():
		return runDownload(o)
	case allLocal && !o.dst.isRemote():
		return errors.New("local-to-local copy: use cp, not scp")
	case allRemote && o.dst.isRemote():
		return errors.New("remote-to-remote copy is not supported in this build")
	default:
		return errors.New("cannot mix local and remote sources in one command")
	}
}

// connectFor opens the ssh connection for endpoint e.
func connectFor(e endpoint, o *options) (*ssh.Client, error) {
	user := e.user
	if user == "" {
		if u, err := getCurrentUser(); err == nil {
			user = u
		} else {
			user = "root"
		}
	}
	port := o.port
	if port == 0 {
		port = 22
	}
	return sshconn.Dial(sshconn.Options{
		User:           user,
		Host:           e.host,
		Port:           port,
		IdentityFile:   o.identityKey,
		KnownHostsPath: o.knownHosts,
		Insecure:       o.insecure,
		Verbose:        o.verbose,
	})
}

func getCurrentUser() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

func parseArgs(args []string) (*options, error) {
	o := &options{}
	var positional []string
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
		case a == "-r":
			o.recursive = true
			i++
		case a == "-p":
			o.preserve = true
			i++
		case a == "-q":
			o.quiet = true
			i++
		case a == "-v":
			o.verbose = true
			i++
		case a == "-P":
			if i+1 >= len(args) {
				return nil, errors.New("-P requires a port")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 1 || n > 65535 {
				return nil, fmt.Errorf("invalid port %q", args[i+1])
			}
			o.port = n
			i += 2
		case strings.HasPrefix(a, "-P"):
			n, err := strconv.Atoi(a[2:])
			if err != nil || n < 1 || n > 65535 {
				return nil, fmt.Errorf("invalid port %q", a[2:])
			}
			o.port = n
			i++
		case a == "-i":
			if i+1 >= len(args) {
				return nil, errors.New("-i requires a path")
			}
			o.identityKey = args[i+1]
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
			positional = append(positional, a)
			i++
		}
	}

	if o.printHelp || o.printVersion {
		return o, nil
	}
	if len(positional) < 2 {
		return nil, errors.New("usage: scp [opts] SRC ... DST")
	}
	for _, p := range positional[:len(positional)-1] {
		o.srcs = append(o.srcs, parseEndpoint(p))
	}
	o.dst = parseEndpoint(positional[len(positional)-1])
	return o, nil
}

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
		o.knownHosts = val
	}
	return nil
}

func printHelp(w io.Writer) {
	const help = `Usage: scp [OPTS] SRC ... DST
Copy files between local and remote (over SSH).

Endpoints are local paths or [USER@]HOST:PATH.

  -r            recurse into directories
  -p            preserve modification times (and modes)
  -q            quiet mode
  -P PORT       SSH port (default 22)
  -i IDENT      identity file
  -o KEY=VALUE  StrictHostKeyChecking, UserKnownHostsFile
      --help    display this help
      --version display version
`
	io.WriteString(w, help)
}
