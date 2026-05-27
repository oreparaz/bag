// Package nc implements bag's drop-in for netcat (TCP and UDP).
//
// Modes:
//
//	nc HOST PORT             connect; copy stdin↔stdout
//	nc -l [HOST] PORT        listen; accept one connection (default TCP)
//	nc -u HOST PORT          UDP connection-mode (best-effort)
//	nc -z HOST PORT[-PORT]   port scan: just open/close, report status
//
// Flags:
//
//	-l, --listen             listen mode (single-shot, accepts one client)
//	-u                       UDP instead of TCP
//	-z                       zero-IO scan mode (paired with -v for output)
//	-v, --verbose            print connection status to stderr
//	-w SECONDS               connect timeout (TCP) / I/O timeout (UDP)
//	-p PORT                  local port to bind for outbound connect
//	-n                       no DNS resolution (treat host as literal IP)
//	-k                       in -l mode, keep listening after the client closes
//
// Intentional omissions: -e (exec on connect — security risk), -c
// (exec with shell), proxy modes, sctp.
package nc

func Main(args []string) int { return run(args) }
