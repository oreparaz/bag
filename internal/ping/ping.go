// Package ping implements bag's drop-in for the ping ICMP tool.
//
// Supported:
//
//	ping [HOST]              ICMP echo until interrupted
//	-c COUNT                 stop after COUNT replies
//	-W TIMEOUT_SEC           per-reply timeout
//	-i INTERVAL_SEC          delay between echoes
//	-s SIZE                  echo payload size (bytes)
//	-q                       quiet — only print summary
//	-4 / -6                  force IPv4 / IPv6
//
// Sockets: we first try the unprivileged ICMP path on Linux
// (net.ListenPacket("udp4", "0.0.0.0:0") + icmp4 protocol, which the
// kernel exposes when /proc/sys/net/ipv4/ping_group_range covers the
// running uid). If that fails, we fall back to the privileged raw
// socket (net.ListenPacket("ip4:icmp", ...)) — requires CAP_NET_RAW.
// On macOS, both forms typically need root.
package ping

func Main(args []string) int { return run(args) }
