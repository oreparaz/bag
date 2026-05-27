// Package seq implements bag's drop-in for GNU seq.
//
// Forms:
//
//	seq LAST                     # 1, 2, ..., LAST
//	seq FIRST LAST               # FIRST, FIRST+1, ..., LAST
//	seq FIRST INCREMENT LAST     # FIRST, FIRST+INC, ..., LAST
//
// Flags:
//
//	-s, --separator STRING       use STRING between numbers (default newline)
//	-w, --equal-width            pad numeric width with leading zeros
//	-f, --format FORMAT          printf %f / %e / %g format
//
// We use float64 throughout, matching gnu's behavior for fractional
// increments. Integer-looking outputs are printed without a decimal
// point.
package seq

func Main(args []string) int { return run(args) }
