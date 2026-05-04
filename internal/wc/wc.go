// Package wc implements the bag drop-in replacement for GNU wc.
//
// 80% target:
//
//	-l, --lines       count newlines
//	-w, --words       count whitespace-delimited words
//	-c, --bytes       count bytes
//	-m, --chars       count UTF-8 codepoints
//	-L, --max-line-length  print the length of the longest line
//
// Default with no flags: print -l, -w, -c (in that order). When multiple
// files are given, a final "total" line is printed.
package wc

func Main(args []string) int { return run(args) }
