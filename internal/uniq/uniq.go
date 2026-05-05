// Package uniq implements the bag drop-in replacement for GNU uniq.
//
// 80% target:
//
//	-c       prefix lines by count
//	-d       only duplicates
//	-u       only unique
//	-i       ignore case
//	-f N     skip first N fields when comparing
//	-s N     skip first N characters when comparing (after -f)
//	-w N     compare at most N characters
//
// Like real uniq, this only collapses *adjacent* duplicates. Sort first
// for global de-duplication.
package uniq

func Main(args []string) int { return run(args) }
