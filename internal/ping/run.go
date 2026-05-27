package ping

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

type options struct {
	host     string
	count    int
	timeout  time.Duration
	interval time.Duration
	size     int
	quiet    bool
	forceV4  bool
	forceV6  bool
}

func parseArgs(argv []string) (*options, error) {
	o := &options{
		timeout:  time.Second,
		interval: time.Second,
		size:     56,
	}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			break
		}
		switch {
		case a == "-c":
			if i+1 >= len(argv) {
				return nil, errors.New("-c requires count")
			}
			i++
			n, err := strconv.Atoi(argv[i])
			if err != nil || n < 0 {
				return nil, fmt.Errorf("invalid -c: %q", argv[i])
			}
			o.count = n
		case a == "-W":
			if i+1 >= len(argv) {
				return nil, errors.New("-W requires seconds")
			}
			i++
			n, err := strconv.Atoi(argv[i])
			if err != nil {
				return nil, fmt.Errorf("invalid -W: %q", argv[i])
			}
			o.timeout = time.Duration(n) * time.Second
		case a == "-i":
			if i+1 >= len(argv) {
				return nil, errors.New("-i requires seconds")
			}
			i++
			n, err := strconv.ParseFloat(argv[i], 64)
			if err != nil {
				return nil, fmt.Errorf("invalid -i: %q", argv[i])
			}
			o.interval = time.Duration(float64(time.Second) * n)
		case a == "-s":
			if i+1 >= len(argv) {
				return nil, errors.New("-s requires size")
			}
			i++
			n, err := strconv.Atoi(argv[i])
			if err != nil || n < 0 || n > 65500 {
				return nil, fmt.Errorf("invalid -s: %q", argv[i])
			}
			o.size = n
		case a == "-q":
			o.quiet = true
		case a == "-4":
			o.forceV4 = true
		case a == "-6":
			o.forceV6 = true
		case strings.HasPrefix(a, "-"):
			// silently ignore unknown flags
		default:
			if o.host == "" {
				o.host = a
			}
		}
	}
	if o.host == "" {
		return nil, errors.New("host required")
	}
	return o, nil
}

func run(argv []string) int {
	o, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ping: %v\n", err)
		return 2
	}
	dst, ipver, err := resolveHost(o)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ping: %v\n", err)
		return 2
	}

	conn, listenNet, proto, err := openICMPSocket(ipver)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ping: open ICMP socket: %v\n", err)
		fmt.Fprintln(os.Stderr, "ping: needs CAP_NET_RAW on Linux or unprivileged ICMP enabled via sysctl, or root on macOS")
		return 2
	}
	defer conn.Close()

	if !o.quiet {
		fmt.Fprintf(os.Stdout, "PING %s (%s) %d data bytes\n", o.host, dst.String(), o.size)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	id := os.Getpid() & 0xffff
	sent, recv := 0, 0
	var minRtt, maxRtt, sumRtt time.Duration = time.Hour, 0, 0

	stop := false
	for seq := 1; !stop; seq++ {
		if o.count > 0 && sent >= o.count {
			break
		}
		payload := make([]byte, o.size)
		for i := range payload {
			payload[i] = byte(i)
		}
		msg := &icmp.Message{
			Type: echoType(ipver),
			Code: 0,
			Body: &icmp.Echo{ID: id, Seq: seq, Data: payload},
		}
		b, err := msg.Marshal(nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ping: marshal: %v\n", err)
			return 2
		}

		writeAddr := &net.UDPAddr{IP: dst}
		// On the raw-socket path (ip4:icmp), we use an *IPAddr.
		var dstAddr net.Addr = writeAddr
		if strings.HasPrefix(listenNet, "ip") {
			dstAddr = &net.IPAddr{IP: dst}
		}
		t0 := time.Now()
		if _, err := conn.WriteTo(b, dstAddr); err != nil {
			fmt.Fprintf(os.Stderr, "ping: write: %v\n", err)
			break
		}
		sent++

		conn.SetReadDeadline(time.Now().Add(o.timeout))
		reply := make([]byte, 1500)
		n, peer, err := conn.ReadFrom(reply)
		if err != nil {
			if !o.quiet {
				fmt.Fprintf(os.Stdout, "Request timeout for icmp_seq=%d\n", seq)
			}
		} else {
			rm, perr := icmp.ParseMessage(proto, reply[:n])
			if perr != nil {
				fmt.Fprintf(os.Stderr, "ping: parse: %v\n", perr)
			} else if rm.Type == replyType(ipver) {
				if echo, ok := rm.Body.(*icmp.Echo); ok && echo.ID == id {
					rtt := time.Since(t0)
					recv++
					sumRtt += rtt
					if rtt < minRtt {
						minRtt = rtt
					}
					if rtt > maxRtt {
						maxRtt = rtt
					}
					if !o.quiet {
						fmt.Fprintf(os.Stdout, "%d bytes from %s: icmp_seq=%d time=%.2f ms\n",
							n, peer.String(), echo.Seq, float64(rtt.Microseconds())/1000.0)
					}
				}
			}
		}

		select {
		case <-sigCh:
			stop = true
		case <-time.After(o.interval):
		}
	}

	if sent > 0 {
		loss := 100.0 * float64(sent-recv) / float64(sent)
		fmt.Fprintf(os.Stdout, "\n--- %s ping statistics ---\n", o.host)
		fmt.Fprintf(os.Stdout, "%d packets transmitted, %d received, %.0f%% packet loss\n",
			sent, recv, loss)
		if recv > 0 {
			avg := sumRtt / time.Duration(recv)
			fmt.Fprintf(os.Stdout, "rtt min/avg/max = %.2f/%.2f/%.2f ms\n",
				ms(minRtt), ms(avg), ms(maxRtt))
		}
	}
	if recv == 0 {
		return 1
	}
	return 0
}

func ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000.0
}

func resolveHost(o *options) (net.IP, int, error) {
	network := "ip"
	if o.forceV4 {
		network = "ip4"
	} else if o.forceV6 {
		network = "ip6"
	}
	addr, err := net.ResolveIPAddr(network, o.host)
	if err != nil {
		return nil, 0, err
	}
	if addr.IP.To4() != nil {
		return addr.IP, 4, nil
	}
	return addr.IP, 6, nil
}

func openICMPSocket(ipver int) (*icmp.PacketConn, string, int, error) {
	// Prefer the unprivileged Linux path (udp4 / udp6).
	if ipver == 4 {
		if c, err := icmp.ListenPacket("udp4", "0.0.0.0"); err == nil {
			return c, "udp4", 1, nil // proto 1 = ICMPv4
		}
		c, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
		if err != nil {
			return nil, "", 0, err
		}
		return c, "ip4:icmp", 1, nil
	}
	if c, err := icmp.ListenPacket("udp6", "::"); err == nil {
		return c, "udp6", 58, nil
	}
	c, err := icmp.ListenPacket("ip6:ipv6-icmp", "::")
	if err != nil {
		return nil, "", 0, err
	}
	return c, "ip6:ipv6-icmp", 58, nil
}

func echoType(ipver int) icmp.Type {
	if ipver == 4 {
		return ipv4.ICMPTypeEcho
	}
	return ipv6.ICMPTypeEchoRequest
}

func replyType(ipver int) icmp.Type {
	if ipver == 4 {
		return ipv4.ICMPTypeEchoReply
	}
	return ipv6.ICMPTypeEchoReply
}
