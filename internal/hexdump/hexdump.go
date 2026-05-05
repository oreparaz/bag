// Package hexdump implements the bag drop-in replacement for hexdump (BSD).
//
// 80% target — the format selectors users actually reach for:
//
//	default   "hex+ASCII" 8-byte words: "0000000 6865 6c6c 6f0a"
//	-C        canonical: "00000000  68 65 6c 6c 6f 0a  |hello.|"
//	-b        one-byte octal
//	-c        one-byte char
//	-d        two-byte decimal
//	-o        two-byte octal
//	-x        two-byte hex (this is the default)
//	-n N      stop after N input bytes
//	-s N      skip first N bytes
//	-v        do not collapse identical adjacent rows (default collapses
//	          and prints "*")
//	-      read stdin (or file argument)
package hexdump

func Main(args []string) int { return run(args) }
