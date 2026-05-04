package curl

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// dataKind tracks how a -d / --data-* chunk should be encoded.
type dataKind int

const (
	dataASCII     dataKind = iota // -d / --data: strip newlines/CRs from @file
	dataBinary                    // --data-binary: keep file bytes as-is
	dataURLEncode                 // --data-urlencode: encode each chunk
	dataRaw                       // --data-raw: never @file, always literal
)

// dataChunk is a single data argument. Multiple chunks join with '&'.
type dataChunk struct {
	kind  dataKind
	value string // either literal, "@filename", or "name=@filename" for urlencode
}

// formField is one -F multipart entry.
type formField struct {
	name  string
	value string // raw value or "@path"
	// Form fields support more forms (=name@path, =<path, type=...,
	// filename=...) but the 80% target is plain "key=value" or "key=@path".
}

// options holds parsed curl flags. Field naming mirrors curl's long names.
type options struct {
	URLs []string

	Method         string
	Headers        []string
	Data           []dataChunk
	Forms          []formField
	OutputPaths    []string // one per URL; "" means stdout, "-" also stdout
	RemoteName     []bool   // one per URL; -O
	FollowRedirect bool
	MaxRedirs      int
	Insecure       bool
	UserAgent      string
	Referer        string
	BasicAuth      string // "user:pass"
	Silent         bool
	ShowError      bool
	IncludeHeaders bool
	HeadOnly       bool
	Verbose        bool
	MaxTime        time.Duration
	ConnectTimeout time.Duration
	CookieIn       string
	CookieJar      string
	Compressed     bool
	Retry          int
	RetryDelay     time.Duration
	retryDelaySet  bool
	RetryMaxTime   time.Duration
	WriteOut       string
	Proxy          string
	NoProxy        string
	IPv4Only       bool
	IPv6Only       bool
	HTTPVersion    int
	FailOnError    bool
	GlobOff        bool
	CACertFile     string
	Range          string
	Continue       int64 // -C
	ContinueAuto   bool

	Get           bool // -G: -d data goes to query string
	CreateDirs    bool // --create-dirs
	remoteNameAll bool // --remote-name-all
	printVersion  bool
	printHelp     bool

	// Body configuration is derived from the above:
	hasData bool
	hasForm bool
}

const defaultMaxRedirs = 50

// parseArgs is curl's argv parser.
//
// Supported forms:
//   -X GET                long form arg in next argv
//   -XGET                 short flag glued to its arg
//   -sS                   bundled boolean shorts
//   --header "X: Y"       long form in next argv
//   --header=X: Y         long form with '='
//
// Unknown flags return an error (curl exit code 2).
func parseArgs(args []string) (*options, error) {
	opts := &options{
		Method:         "",
		FollowRedirect: false,
		MaxRedirs:      defaultMaxRedirs,
		HTTPVersion:    0,
	}

	i := 0
	for i < len(args) {
		a := args[i]

		// Bare URL
		if a == "" || (a[0] != '-') || a == "-" {
			opts.URLs = append(opts.URLs, a)
			i++
			continue
		}
		if a == "--" {
			for _, u := range args[i+1:] {
				opts.URLs = append(opts.URLs, u)
			}
			break
		}

		// Long flag --name or --name=value
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
			if err := applyLong(opts, name, next, hasEq, val); err != nil {
				return nil, err
			}
			i++
			continue
		}

		// Short flag (possibly bundled, possibly with glued arg)
		// Walk one rune at a time. The first short that takes an arg
		// consumes the rest of the cluster, or the next argv if empty.
		j := 1
		consumed := false
		for j < len(a) && !consumed {
			c := a[j]
			arg := ""
			needsArg, err := shortNeedsArg(c)
			if err != nil {
				return nil, err
			}
			if needsArg {
				if j+1 < len(a) {
					arg = a[j+1:]
				} else {
					if i+1 >= len(args) {
						return nil, fmt.Errorf("option -%c requires an argument", c)
					}
					i++
					arg = args[i]
				}
				consumed = true
			}
			if err := applyShort(opts, c, arg); err != nil {
				return nil, err
			}
			j++
		}
		i++
	}

	if err := validate(opts); err != nil {
		return nil, err
	}
	return opts, nil
}

func shortNeedsArg(c byte) (bool, error) {
	switch c {
	case 'A', 'b', 'c', 'd', 'e', 'F', 'H', 'o', 'u', 'w', 'X', 'x', 'm', 'T', 'r', 'C', 't':
		return true, nil
	case 'G', 'I', 'i', 'k', 'L', 'O', 's', 'S', 'v', 'V', 'f', 'h', 'j', 'n', 'q', 'g', '4', '6', '#':
		return false, nil
	}
	return false, fmt.Errorf("unknown short option -%c", c)
}

func applyShort(o *options, c byte, arg string) error {
	switch c {
	case 'A':
		o.UserAgent = arg
	case 'b':
		o.CookieIn = arg
	case 'c':
		o.CookieJar = arg
	case 'C':
		if arg == "-" {
			o.ContinueAuto = true
			return nil
		}
		n, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid -C value: %v", err)
		}
		o.Continue = n
	case 'd':
		o.Data = append(o.Data, dataChunk{kind: dataASCII, value: arg})
		o.hasData = true
	case 'e':
		o.Referer = arg
	case 'F':
		f, err := parseFormField(arg)
		if err != nil {
			return err
		}
		o.Forms = append(o.Forms, f)
		o.hasForm = true
	case 'G':
		o.Get = true
	case 'H':
		o.Headers = append(o.Headers, arg)
	case 'i':
		o.IncludeHeaders = true
	case 'I':
		o.HeadOnly = true
		if o.Method == "" {
			o.Method = "HEAD"
		}
	case 'k':
		o.Insecure = true
	case 'L':
		o.FollowRedirect = true
	case 'o':
		o.OutputPaths = append(o.OutputPaths, arg)
		o.RemoteName = append(o.RemoteName, false)
	case 'O':
		o.OutputPaths = append(o.OutputPaths, "")
		o.RemoteName = append(o.RemoteName, true)
	case 'r':
		o.Range = arg
	case 's':
		o.Silent = true
	case 'S':
		o.ShowError = true
	case 'u':
		o.BasicAuth = arg
	case 'v':
		o.Verbose = true
	case 'V':
		o.printVersion = true
	case 'w':
		o.WriteOut = arg
	case 'X':
		o.Method = strings.ToUpper(arg)
	case 'x':
		o.Proxy = arg
	case 'm':
		d, err := parseSeconds(arg)
		if err != nil {
			return fmt.Errorf("invalid -m value: %v", err)
		}
		o.MaxTime = d
	case 'f':
		o.FailOnError = true
	case 'h':
		o.printHelp = true
	case '4':
		o.IPv4Only = true
	case '6':
		o.IPv6Only = true
	case '#':
		// progress bar style; we don't render one — accept silently
	case 'j':
		// --junk-session-cookies: stored but not implemented
	case 'n':
		// .netrc — not implemented
	case 'q':
		// disable .curlrc — we don't load one anyway
	case 'g':
		o.GlobOff = true
	case 'T':
		// PUT upload — out of 80% scope
		return fmt.Errorf("option -T not supported")
	case 't':
		// telnet option — out of scope
		return fmt.Errorf("option -t not supported")
	}
	return nil
}

func applyLong(o *options, name string, next func() (string, error), _ bool, _ string) error {
	switch name {
	case "url":
		v, err := next()
		if err != nil {
			return err
		}
		o.URLs = append(o.URLs, v)
	case "request":
		v, err := next()
		if err != nil {
			return err
		}
		o.Method = strings.ToUpper(v)
	case "header":
		v, err := next()
		if err != nil {
			return err
		}
		o.Headers = append(o.Headers, v)
	case "data":
		v, err := next()
		if err != nil {
			return err
		}
		o.Data = append(o.Data, dataChunk{kind: dataASCII, value: v})
		o.hasData = true
	case "data-binary":
		v, err := next()
		if err != nil {
			return err
		}
		o.Data = append(o.Data, dataChunk{kind: dataBinary, value: v})
		o.hasData = true
	case "data-raw":
		v, err := next()
		if err != nil {
			return err
		}
		o.Data = append(o.Data, dataChunk{kind: dataRaw, value: v})
		o.hasData = true
	case "data-urlencode":
		v, err := next()
		if err != nil {
			return err
		}
		o.Data = append(o.Data, dataChunk{kind: dataURLEncode, value: v})
		o.hasData = true
	case "form":
		v, err := next()
		if err != nil {
			return err
		}
		f, err := parseFormField(v)
		if err != nil {
			return err
		}
		o.Forms = append(o.Forms, f)
		o.hasForm = true
	case "user-agent":
		v, err := next()
		if err != nil {
			return err
		}
		o.UserAgent = v
	case "referer":
		v, err := next()
		if err != nil {
			return err
		}
		o.Referer = v
	case "user":
		v, err := next()
		if err != nil {
			return err
		}
		o.BasicAuth = v
	case "output":
		v, err := next()
		if err != nil {
			return err
		}
		o.OutputPaths = append(o.OutputPaths, v)
		o.RemoteName = append(o.RemoteName, false)
	case "remote-name":
		o.OutputPaths = append(o.OutputPaths, "")
		o.RemoteName = append(o.RemoteName, true)
	case "remote-name-all":
		o.remoteNameAll = true
	case "location":
		o.FollowRedirect = true
	case "no-location":
		o.FollowRedirect = false
	case "max-redirs":
		v, err := next()
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid --max-redirs: %v", err)
		}
		o.MaxRedirs = n
	case "insecure":
		o.Insecure = true
	case "no-insecure":
		o.Insecure = false
	case "silent":
		o.Silent = true
	case "no-silent":
		o.Silent = false
	case "show-error":
		o.ShowError = true
	case "no-show-error":
		o.ShowError = false
	case "include":
		o.IncludeHeaders = true
	case "head":
		o.HeadOnly = true
		if o.Method == "" {
			o.Method = "HEAD"
		}
	case "verbose":
		o.Verbose = true
	case "no-verbose":
		o.Verbose = false
	case "max-time":
		v, err := next()
		if err != nil {
			return err
		}
		d, err := parseSeconds(v)
		if err != nil {
			return fmt.Errorf("invalid --max-time: %v", err)
		}
		o.MaxTime = d
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
	case "cookie":
		v, err := next()
		if err != nil {
			return err
		}
		o.CookieIn = v
	case "cookie-jar":
		v, err := next()
		if err != nil {
			return err
		}
		o.CookieJar = v
	case "compressed":
		o.Compressed = true
	case "retry":
		v, err := next()
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid --retry: %v", err)
		}
		o.Retry = n
	case "retry-delay":
		v, err := next()
		if err != nil {
			return err
		}
		d, err := parseSeconds(v)
		if err != nil {
			return fmt.Errorf("invalid --retry-delay: %v", err)
		}
		o.RetryDelay = d
		o.retryDelaySet = true
	case "retry-max-time":
		v, err := next()
		if err != nil {
			return err
		}
		d, err := parseSeconds(v)
		if err != nil {
			return fmt.Errorf("invalid --retry-max-time: %v", err)
		}
		o.RetryMaxTime = d
	case "write-out":
		v, err := next()
		if err != nil {
			return err
		}
		o.WriteOut = v
	case "proxy":
		v, err := next()
		if err != nil {
			return err
		}
		o.Proxy = v
	case "noproxy", "no-proxy":
		v, err := next()
		if err != nil {
			return err
		}
		o.NoProxy = v
	case "ipv4":
		o.IPv4Only = true
	case "ipv6":
		o.IPv6Only = true
	case "http1.0":
		o.HTTPVersion = 1
	case "http1.1":
		o.HTTPVersion = 1
	case "http2":
		o.HTTPVersion = 2
	case "fail":
		o.FailOnError = true
	case "globoff":
		o.GlobOff = true
	case "cacert":
		v, err := next()
		if err != nil {
			return err
		}
		o.CACertFile = v
	case "range":
		v, err := next()
		if err != nil {
			return err
		}
		o.Range = v
	case "continue-at":
		v, err := next()
		if err != nil {
			return err
		}
		if v == "-" {
			o.ContinueAuto = true
			return nil
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid --continue-at: %v", err)
		}
		o.Continue = n
	case "get":
		o.Get = true
	case "version":
		o.printVersion = true
	case "help":
		o.printHelp = true
	case "disable":
		// disable .curlrc — we don't load one
	case "create-dirs":
		o.CreateDirs = true
	default:
		return fmt.Errorf("unknown option --%s", name)
	}
	return nil
}

// parseSeconds accepts integer or fractional seconds.
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

// parseFormField parses a -F/--form value.
//
// Forms supported:
//
//	name=value       literal
//	name=@path       file content as a part with filename=basename(path)
//	name=<path       file content but no "filename" header
func parseFormField(s string) (formField, error) {
	eq := strings.IndexByte(s, '=')
	if eq < 0 {
		return formField{}, fmt.Errorf("invalid -F %q: missing '='", s)
	}
	return formField{name: s[:eq], value: s[eq+1:]}, nil
}

func validate(o *options) error {
	if o.printHelp || o.printVersion {
		return nil
	}
	if len(o.URLs) == 0 {
		return errors.New("no URL specified")
	}
	if o.hasData && o.hasForm {
		return errors.New("cannot mix -d/--data with -F/--form")
	}
	return nil
}
