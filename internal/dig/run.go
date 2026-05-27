package dig

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

type options struct {
	server  string // "1.1.1.1", "8.8.8.8:53", ...
	queryT  string // A, AAAA, MX, NS, TXT, PTR, SRV, CNAME, SOA
	name    string
	short   bool
	noall   bool
	answer  bool
	port    string
	reverse string
	timeout time.Duration
}

func parseArgs(argv []string) (*options, error) {
	o := &options{port: "53", timeout: 5 * time.Second}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case strings.HasPrefix(a, "@"):
			o.server = a[1:]
		case strings.HasPrefix(a, "+"):
			switch a[1:] {
			case "short":
				o.short = true
			case "noall":
				o.noall = true
			case "answer":
				o.answer = true
			case "trace":
				// no-op for now
			}
		case a == "-x":
			if i+1 >= len(argv) {
				return nil, errors.New("-x requires an address")
			}
			i++
			o.reverse = argv[i]
		case a == "-t":
			if i+1 >= len(argv) {
				return nil, errors.New("-t requires a type")
			}
			i++
			o.queryT = strings.ToUpper(argv[i])
		case a == "-p":
			if i+1 >= len(argv) {
				return nil, errors.New("-p requires a port")
			}
			i++
			o.port = argv[i]
		default:
			if o.name == "" {
				o.name = a
			} else if o.queryT == "" {
				o.queryT = strings.ToUpper(a)
			}
		}
	}
	if o.reverse != "" {
		o.name = o.reverse
		o.queryT = "PTR"
	}
	if o.name == "" {
		return nil, errors.New("missing query name")
	}
	if o.queryT == "" {
		o.queryT = "A"
	}
	return o, nil
}

func run(argv []string) int {
	o, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dig: %v\n", err)
		return 1
	}
	r := buildResolver(o)
	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	defer cancel()

	if !o.short {
		fmt.Fprintf(os.Stdout, "; <<>> bag dig <<>> %s %s\n", o.queryT, o.name)
		fmt.Fprintf(os.Stdout, ";; QUESTION SECTION:\n;%s.\t\tIN\t%s\n\n", o.name, o.queryT)
		fmt.Fprintf(os.Stdout, ";; ANSWER SECTION:\n")
	}

	exit := lookupAndPrint(ctx, r, o)
	if !o.short {
		fmt.Fprintln(os.Stdout)
	}
	return exit
}

func buildResolver(o *options) *net.Resolver {
	if o.server == "" {
		return net.DefaultResolver
	}
	srv := o.server
	if !strings.Contains(srv, ":") {
		srv = net.JoinHostPort(srv, o.port)
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: o.timeout}
			return d.DialContext(ctx, network, srv)
		},
	}
}

func lookupAndPrint(ctx context.Context, r *net.Resolver, o *options) int {
	switch o.queryT {
	case "A":
		ips, err := r.LookupIP(ctx, "ip4", o.name)
		return printIPs(o, ips, err, "A")
	case "AAAA":
		ips, err := r.LookupIP(ctx, "ip6", o.name)
		return printIPs(o, ips, err, "AAAA")
	case "CNAME":
		c, err := r.LookupCNAME(ctx, o.name)
		if err != nil {
			return diagErr(err)
		}
		printAnswer(o, o.name, "CNAME", c)
		return 0
	case "MX":
		mxs, err := r.LookupMX(ctx, o.name)
		if err != nil {
			return diagErr(err)
		}
		for _, m := range mxs {
			printAnswer(o, o.name, "MX", fmt.Sprintf("%d %s", m.Pref, m.Host))
		}
		return 0
	case "NS":
		ns, err := r.LookupNS(ctx, o.name)
		if err != nil {
			return diagErr(err)
		}
		for _, n := range ns {
			printAnswer(o, o.name, "NS", n.Host)
		}
		return 0
	case "TXT":
		t, err := r.LookupTXT(ctx, o.name)
		if err != nil {
			return diagErr(err)
		}
		for _, v := range t {
			printAnswer(o, o.name, "TXT", fmt.Sprintf("%q", v))
		}
		return 0
	case "PTR":
		names, err := r.LookupAddr(ctx, o.name)
		if err != nil {
			return diagErr(err)
		}
		for _, n := range names {
			printAnswer(o, o.name, "PTR", n)
		}
		return 0
	case "SRV":
		_, srvs, err := r.LookupSRV(ctx, "", "", o.name)
		if err != nil {
			return diagErr(err)
		}
		for _, s := range srvs {
			printAnswer(o, o.name, "SRV",
				fmt.Sprintf("%d %d %d %s", s.Priority, s.Weight, s.Port, s.Target))
		}
		return 0
	}
	fmt.Fprintf(os.Stderr, "dig: unsupported type %q\n", o.queryT)
	return 1
}

func printIPs(o *options, ips []net.IP, err error, rrType string) int {
	if err != nil {
		return diagErr(err)
	}
	for _, ip := range ips {
		printAnswer(o, o.name, rrType, ip.String())
	}
	return 0
}

func printAnswer(o *options, name, rrType, val string) {
	if o.short {
		fmt.Fprintln(os.Stdout, val)
		return
	}
	fmt.Fprintf(os.Stdout, "%s.\t60\tIN\t%s\t%s\n", name, rrType, val)
}

func diagErr(err error) int {
	fmt.Fprintf(os.Stderr, "dig: %v\n", err)
	return 1
}
