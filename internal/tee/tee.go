// Package tee implements the bag drop-in replacement for GNU tee.
//
// 80% target:
//
//	-a, --append            append to files instead of truncating
//	-i, --ignore-interrupts ignore SIGINT
//	-                       (no special meaning; tee writes to stdout always)
package tee

func Main(args []string) int { return run(args) }
