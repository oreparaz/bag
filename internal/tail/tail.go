// Package tail implements the bag drop-in replacement for GNU tail.
//
// 80% target:
//
//	-n, --lines=[+]N    last N lines (or, with '+N', from line N onward)
//	-c, --bytes=[+]N    last N bytes (or, with '+N', from byte N)
//	-q, --quiet         never print headers
//	-v, --verbose       always print headers
//
// -f follow mode is intentionally deferred (see FUTURE.md).
package tail

func Main(args []string) int { return run(args) }
