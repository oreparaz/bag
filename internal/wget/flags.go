package wget

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type options struct {
	URLs []string

	OutputDocument   string // -O
	OutputDocumentSet bool
	LogFile          string // -o
	AppendLog        string // -a
	DirPrefix        string // -P
	InputFile        string // -i
	Quiet            bool
	NonVerbose       bool
	ServerResponse   bool // -S
	NoCheckCert      bool
	CACertFile       string
	UserAgent        string
	Referer          string
	Headers          []string
	User             string
	Password         string
	Tries            int
	Wait             time.Duration
	WaitRetry        time.Duration
	Timeout          time.Duration
	ConnectTimeout   time.Duration
	ReadTimeout      time.Duration
	DNSTimeout       time.Duration
	Continue         bool
	NoClobber        bool
	MaxRedirect      int
	Recursive        bool
	Level            int
	NoParent         bool
	NoDirectories    bool
	ContentDisposition bool
	IPv4Only         bool
	IPv6Only         bool
	Proxy            string
	NoProxy          bool // --proxy=off / --proxy=no

	Method   string // --method
	PostData string // --post-data
	PostDataSet bool

	printHelp    bool
	printVersion bool
}

const defaultMaxRedirect = 20
const defaultTries = 20
const defaultLevel = 5

// parseArgs parses wget-style argv.
//
// Quirks vs. curl:
//   - No clustered booleans (-sS in curl, but wget treats -nv as one option).
//   - --name=value or --name value
//   - -O- glues output
func parseArgs(args []string) (*options, error) {
	o := &options{
		Tries:       defaultTries,
		MaxRedirect: defaultMaxRedirect,
		Level:       defaultLevel,
	}

	i := 0
	for i < len(args) {
		a := args[i]
		if a == "" || a[0] != '-' || a == "-" {
			o.URLs = append(o.URLs, a)
			i++
			continue
		}
		if a == "--" {
			for _, u := range args[i+1:] {
				o.URLs = append(o.URLs, u)
			}
			break
		}

		// Long form
		if strings.HasPrefix(a, "--") {
			name := a[2:]
			val := ""
			hasEq := false
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				val = name[eq+1:]
				name = name[:eq]
				hasEq = true
			}
			next := func() (string, error) {
				if hasEq {
					return val, nil
				}
				if i+1 >= len(args) {
					return "", fmt.Errorf("option --%s requires an argument", name)
				}
				i++
				return args[i], nil
			}
			if err := applyLong(o, name, next); err != nil {
				return nil, err
			}
			i++
			continue
		}

		// Short form. Some shorts are multi-letter (e.g. -nv, -nc, -np, -nd).
		short := a[1:]
		if multi, ok := multiShorts()[short]; ok {
			if err := applyMulti(o, multi); err != nil {
				return nil, err
			}
			i++
			continue
		}

		// Single-letter short, possibly with glued arg.
		c := short[0]
		rest := short[1:]
		needsArg, err := shortNeedsArg(c)
		if err != nil {
			return nil, err
		}
		if needsArg {
			if rest != "" {
				if err := applyShort(o, c, rest); err != nil {
					return nil, err
				}
			} else {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("option -%c requires an argument", c)
				}
				if err := applyShort(o, c, args[i+1]); err != nil {
					return nil, err
				}
				i++
			}
		} else {
			if err := applyShort(o, c, ""); err != nil {
				return nil, err
			}
			// Allow bundled booleans (-Sd etc.) — rare for wget but supported.
			for _, bc := range []byte(rest) {
				if err := applyShort(o, bc, ""); err != nil {
					return nil, err
				}
			}
		}
		i++
	}

	if err := validate(o); err != nil {
		return nil, err
	}
	return o, nil
}

func multiShorts() map[string]string {
	return map[string]string{
		"nv": "no-verbose",
		"nc": "no-clobber",
		"nd": "no-directories",
		"np": "no-parent",
		"nH": "no-host-directories",
	}
}

func applyMulti(o *options, longName string) error {
	return applyLong(o, longName, func() (string, error) {
		return "", fmt.Errorf("option --%s does not take an argument", longName)
	})
}

func shortNeedsArg(c byte) (bool, error) {
	switch c {
	case 'O', 'o', 'a', 'P', 'i', 't', 'T', 'l', 'A', 'R', 'U', 'e', 'D', 'F', 'I', 'X':
		return true, nil
	case 'q', 'v', 'd', 'c', 'N', 'r', 'p', 'k', 'K', 'm', 'S', 'x', 'b', 'h', 'V', '4', '6', 'B', 'E', 'H', 'L', 'Q':
		return false, nil
	}
	return false, fmt.Errorf("unknown short option -%c", c)
}

func applyShort(o *options, c byte, arg string) error {
	switch c {
	case 'O':
		o.OutputDocument = arg
		o.OutputDocumentSet = true
	case 'o':
		o.LogFile = arg
	case 'a':
		o.AppendLog = arg
	case 'P':
		o.DirPrefix = arg
	case 'i':
		o.InputFile = arg
	case 't':
		n, err := strconv.Atoi(arg)
		if err != nil {
			return fmt.Errorf("invalid -t: %v", err)
		}
		o.Tries = n
	case 'T':
		d, err := parseSeconds(arg)
		if err != nil {
			return fmt.Errorf("invalid -T: %v", err)
		}
		o.Timeout = d
	case 'l':
		n, err := strconv.Atoi(arg)
		if err != nil {
			return fmt.Errorf("invalid -l: %v", err)
		}
		o.Level = n
	case 'U':
		o.UserAgent = arg
	case 'q':
		o.Quiet = true
	case 'v':
		o.NonVerbose = false
	case 'd':
		// debug — we just keep verbose default
	case 'c':
		o.Continue = true
	case 'r':
		o.Recursive = true
	case 'S':
		o.ServerResponse = true
	case 'N':
		// timestamping — not implemented; documented as a no-op for now
	case '4':
		o.IPv4Only = true
	case '6':
		o.IPv6Only = true
	case 'h':
		o.printHelp = true
	case 'V':
		o.printVersion = true
	case 'p', 'k', 'K', 'm', 'b', 'B', 'E', 'H', 'L', 'Q', 'A', 'R', 'D', 'F', 'I', 'X', 'e', 'x':
		// recognized but not implemented; do not error
	}
	return nil
}

func applyLong(o *options, name string, next func() (string, error)) error {
	switch name {
	case "output-document":
		v, err := next()
		if err != nil {
			return err
		}
		o.OutputDocument = v
		o.OutputDocumentSet = true
	case "output-file":
		v, err := next()
		if err != nil {
			return err
		}
		o.LogFile = v
	case "append-output":
		v, err := next()
		if err != nil {
			return err
		}
		o.AppendLog = v
	case "directory-prefix":
		v, err := next()
		if err != nil {
			return err
		}
		o.DirPrefix = v
	case "input-file":
		v, err := next()
		if err != nil {
			return err
		}
		o.InputFile = v
	case "quiet":
		o.Quiet = true
	case "verbose":
		o.NonVerbose = false
	case "no-verbose":
		o.NonVerbose = true
	case "show-progress", "no-show-progress":
		// Boolean — we have no progress bar but real wget scripts pass
		// these routinely; accept-and-ignore rather than failing the run.
	case "progress":
		// --progress=bar / --progress=dot — has a value. Only consume
		// the value when one was explicitly attached with `=`; otherwise
		// don't steal the URL from the argv tail.
		// (`next()` only consumes the next arg when hasEq is false; so
		// we call it without preserving the value to advance i.)
		_, _ = next()
	case "tries":
		v, err := next()
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid --tries: %v", err)
		}
		o.Tries = n
	case "timeout":
		v, err := next()
		if err != nil {
			return err
		}
		d, err := parseSeconds(v)
		if err != nil {
			return fmt.Errorf("invalid --timeout: %v", err)
		}
		o.Timeout = d
	case "connect-timeout":
		v, err := next()
		if err != nil {
			return err
		}
		d, err := parseSeconds(v)
		if err != nil {
			return fmt.Errorf("invalid --connect-timeout: %v", err)
		}
		o.ConnectTimeout = d
	case "read-timeout":
		v, err := next()
		if err != nil {
			return err
		}
		d, err := parseSeconds(v)
		if err != nil {
			return fmt.Errorf("invalid --read-timeout: %v", err)
		}
		o.ReadTimeout = d
	case "dns-timeout":
		v, err := next()
		if err != nil {
			return err
		}
		d, err := parseSeconds(v)
		if err != nil {
			return fmt.Errorf("invalid --dns-timeout: %v", err)
		}
		o.DNSTimeout = d
	case "wait":
		v, err := next()
		if err != nil {
			return err
		}
		d, err := parseSeconds(v)
		if err != nil {
			return fmt.Errorf("invalid --wait: %v", err)
		}
		o.Wait = d
	case "waitretry":
		v, err := next()
		if err != nil {
			return err
		}
		d, err := parseSeconds(v)
		if err != nil {
			return fmt.Errorf("invalid --waitretry: %v", err)
		}
		o.WaitRetry = d
	case "user-agent":
		v, err := next()
		if err != nil {
			return err
		}
		o.UserAgent = v
	case "header":
		v, err := next()
		if err != nil {
			return err
		}
		o.Headers = append(o.Headers, v)
	case "user":
		v, err := next()
		if err != nil {
			return err
		}
		o.User = v
	case "password":
		v, err := next()
		if err != nil {
			return err
		}
		o.Password = v
	case "referer":
		v, err := next()
		if err != nil {
			return err
		}
		o.Referer = v
	case "no-check-certificate":
		o.NoCheckCert = true
	case "check-certificate":
		o.NoCheckCert = false
	case "ca-certificate":
		v, err := next()
		if err != nil {
			return err
		}
		o.CACertFile = v
	case "continue":
		o.Continue = true
	case "no-clobber":
		o.NoClobber = true
	case "max-redirect":
		v, err := next()
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid --max-redirect: %v", err)
		}
		o.MaxRedirect = n
	case "recursive":
		o.Recursive = true
	case "level":
		v, err := next()
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid --level: %v", err)
		}
		o.Level = n
	case "no-parent":
		o.NoParent = true
	case "no-directories":
		o.NoDirectories = true
	case "no-host-directories":
		// recognized; no-op for our shallow recursive
	case "content-disposition":
		o.ContentDisposition = true
	case "no-content-disposition":
		o.ContentDisposition = false
	case "inet4-only":
		o.IPv4Only = true
	case "inet6-only":
		o.IPv6Only = true
	case "proxy":
		// wget --proxy=on/off; default is on. Treat any non-"off" as on.
		v, err := next()
		if err != nil {
			return err
		}
		if v == "off" || v == "no" {
			o.Proxy = ""
			o.NoProxy = true
		}
	case "execute":
		_, err := next()
		if err != nil {
			return err
		}
		// .wgetrc directive — silently ignored
	case "version":
		o.printVersion = true
	case "help":
		o.printHelp = true
	case "no-cookies", "load-cookies", "save-cookies", "keep-session-cookies":
		// cookies — not yet wired in for wget; consume the arg if applicable
		if name != "no-cookies" && name != "keep-session-cookies" {
			_, _ = next()
		}
	case "no-hsts", "no-warc-keep-log", "default-page":
		// recognized no-ops / arg-eaters
		if name == "default-page" {
			_, _ = next()
		}
	case "https-only", "no-http-keep-alive", "no-cache", "no-dns-cache":
		// recognized no-ops
	case "restrict-file-names":
		_, err := next()
		if err != nil {
			return err
		}
	case "accept", "reject", "domains", "exclude-domains", "include-directories", "exclude-directories":
		_, err := next()
		if err != nil {
			return err
		}
		// recursive filters — out of 80% scope
	case "post-data":
		v, err := next()
		if err != nil {
			return err
		}
		o.PostData = v
		o.PostDataSet = true
		if o.Method == "" {
			o.Method = "POST"
		}
	case "method":
		v, err := next()
		if err != nil {
			return err
		}
		o.Method = strings.ToUpper(v)
	default:
		return fmt.Errorf("unknown option --%s", name)
	}
	return nil
}

func parseSeconds(s string) (time.Duration, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if f < 0 {
		return 0, errors.New("negative duration")
	}
	return time.Duration(f * float64(time.Second)), nil
}

func validate(o *options) error {
	if o.printHelp || o.printVersion {
		return nil
	}
	if len(o.URLs) == 0 && o.InputFile == "" {
		return errors.New("missing URL")
	}
	if o.IPv4Only && o.IPv6Only {
		return errors.New("cannot combine --inet4-only and --inet6-only")
	}
	return nil
}
