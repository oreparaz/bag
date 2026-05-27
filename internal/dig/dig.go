// Package dig implements bag's drop-in for the dig DNS-lookup tool.
//
// Supported types: A, AAAA, CNAME, MX, NS, PTR, SRV, TXT, SOA.
//
// Form:
//
//	dig [@SERVER] [+short] [+noall] [+answer] NAME [TYPE]
//
// Flags:
//
//	@SERVER       resolve via SERVER (host or host:port) instead of /etc/resolv.conf
//	+short        only print answer values (one per line)
//	+noall        suppress default sections (used with +answer)
//	+answer       print the ANSWER section (with +noall)
//	-x ADDR       reverse-lookup PTR for ADDR
//	-t TYPE       explicit type
//	-p PORT       custom server port (default 53)
//
// We use golang.org/x/net/dns/dnsmessage for parsing — it's already
// in bag's transitive dep set via go-crypto, no new dep.
package dig

func Main(args []string) int { return run(args) }
