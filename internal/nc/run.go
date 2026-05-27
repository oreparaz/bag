package nc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type options struct {
	listen  bool
	udp     bool
	scan    bool
	verbose bool
	timeout time.Duration
	srcPort string
	noDNS   bool
	keep    bool
	host    string
	port    string
}

func parseArgs(argv []string) (*options, error) {
	o := &options{}
	var positional []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			positional = append(positional, argv[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "--") {
			switch a[2:] {
			case "listen":
				o.listen = true
			case "verbose":
				o.verbose = true
			default:
				// Ignore unknown long flags.
			}
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" && len(a) > 1 && isFlagCluster(a) {
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'l':
					o.listen = true
				case 'u':
					o.udp = true
				case 'z':
					o.scan = true
				case 'v':
					o.verbose = true
				case 'n':
					o.noDNS = true
				case 'k':
					o.keep = true
				case 'w':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-w requires seconds")
						}
						i++
						val = argv[i]
					}
					n, err := strconv.Atoi(val)
					if err != nil {
						return nil, fmt.Errorf("invalid -w: %q", val)
					}
					o.timeout = time.Duration(n) * time.Second
				case 'p':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-p requires a port")
						}
						i++
						val = argv[i]
					}
					o.srcPort = val
				default:
					// Ignore unknown short flags.
				}
			}
			continue
		}
		positional = append(positional, a)
	}
	switch len(positional) {
	case 1:
		o.port = positional[0]
	case 2:
		o.host = positional[0]
		o.port = positional[1]
	case 0:
		// listen mode may omit host but needs port; report below.
	}
	if o.port == "" {
		return nil, errors.New("port required")
	}
	return o, nil
}

func isFlagCluster(s string) bool {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case 'l', 'u', 'z', 'v', 'n', 'k', 'w', 'p':
		default:
			return false
		}
	}
	return true
}

func run(argv []string) int {
	o, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nc: %v\n", err)
		return 1
	}
	switch {
	case o.scan:
		return doScan(o)
	case o.listen:
		return doListen(o)
	}
	return doConnect(o)
}

func network(o *options) string {
	if o.udp {
		return "udp"
	}
	return "tcp"
}

// doConnect makes a single outbound connection and pumps stdin→conn /
// conn→stdout until either side closes.
func doConnect(o *options) int {
	target := net.JoinHostPort(o.host, o.port)
	d := net.Dialer{Timeout: o.timeout}
	if o.srcPort != "" {
		d.LocalAddr = &net.TCPAddr{Port: portInt(o.srcPort)}
	}
	conn, err := d.Dial(network(o), target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nc: %v\n", err)
		return 1
	}
	defer conn.Close()
	if o.verbose {
		fmt.Fprintf(os.Stderr, "Connection to %s succeeded\n", target)
	}
	return pump(conn, o)
}

func doListen(o *options) int {
	addr := net.JoinHostPort(o.host, o.port)
	if o.udp {
		pc, err := net.ListenPacket("udp", addr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "nc: %v\n", err)
			return 1
		}
		defer pc.Close()
		return pumpUDPListen(pc, o)
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nc: %v\n", err)
		return 1
	}
	defer l.Close()
	for {
		c, err := l.Accept()
		if err != nil {
			fmt.Fprintf(os.Stderr, "nc: %v\n", err)
			return 1
		}
		if o.verbose {
			fmt.Fprintf(os.Stderr, "Connection accepted from %s\n", c.RemoteAddr())
		}
		exit := pump(c, o)
		c.Close()
		if !o.keep {
			return exit
		}
	}
}

// pump bidirectionally copies between os.Stdin/os.Stdout and conn.
// When the stdin side EOFs, we half-close the write side of conn so
// the peer sees end-of-stream and closes back; that wakes our
// stdout-side io.Copy with EOF. Both ends are drained before
// returning, so the caller sees all bytes the peer sent.
func pump(conn net.Conn, o *options) int {
	if o.timeout > 0 {
		conn.SetDeadline(time.Now().Add(o.timeout))
	}
	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		_, _ = io.Copy(os.Stdout, conn)
	}()
	_, _ = io.Copy(conn, os.Stdin)
	// Half-close the write side so the peer's read returns EOF.
	type closeWriter interface{ CloseWrite() error }
	if cw, ok := conn.(closeWriter); ok {
		cw.CloseWrite()
	} else {
		conn.Close()
	}
	<-stdoutDone
	return 0
}

func pumpUDPListen(pc net.PacketConn, o *options) int {
	buf := make([]byte, 64*1024)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "nc: %v\n", err)
			return 1
		}
		_, _ = os.Stdout.Write(buf[:n])
		_ = addr
		if !o.keep {
			return 0
		}
	}
}

// doScan walks a single port or a "lo-hi" range, attempting a TCP
// connect on each. Successful opens are reported (always; -v adds
// per-port "Connection refused" for failures).
func doScan(o *options) int {
	lo, hi, err := splitRange(o.port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nc: %v\n", err)
		return 1
	}
	for p := lo; p <= hi; p++ {
		target := net.JoinHostPort(o.host, strconv.Itoa(p))
		d := net.Dialer{Timeout: o.timeout}
		if d.Timeout == 0 {
			d.Timeout = 2 * time.Second
		}
		conn, err := d.Dial(network(o), target)
		if err == nil {
			conn.Close()
			fmt.Fprintf(os.Stdout, "Connection to %s port %d/%s succeeded\n",
				o.host, p, network(o))
		} else if o.verbose {
			fmt.Fprintf(os.Stderr, "nc: %s port %d: %v\n", o.host, p, err)
		}
	}
	return 0
}

func splitRange(s string) (int, int, error) {
	if i := strings.Index(s, "-"); i > 0 {
		lo, err := strconv.Atoi(s[:i])
		if err != nil {
			return 0, 0, err
		}
		hi, err := strconv.Atoi(s[i+1:])
		if err != nil {
			return 0, 0, err
		}
		return lo, hi, nil
	}
	p, err := strconv.Atoi(s)
	return p, p, err
}

func portInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
