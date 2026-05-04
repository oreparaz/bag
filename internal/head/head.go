// Package head implements the bag drop-in replacement for GNU head.
//
// 80% target:
//
//	-n, --lines=[-]N    first N lines (negative: all but the last N)
//	-c, --bytes=[-]N    first N bytes (negative: all but the last N)
//	-q, --quiet         never print "==> file <==" headers
//	-v, --verbose       always print headers
//	-                   read stdin
//	multi-file output prepends "==> NAME <==" headers by default
//
// Suffix multipliers on N: b=512, K/k=1024, M/m=1MiB, G/g=1GiB.
package head

// Main is the entry point.
func Main(args []string) int {
	return run(args)
}
