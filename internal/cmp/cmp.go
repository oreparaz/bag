// Package cmp implements bag's drop-in for GNU cmp (byte-level
// file comparison).
//
// Supported:
//
//	cmp FILE1 FILE2          differ → print "FILE1 FILE2 differ: byte
//	                         N, line M"; same → silent
//	-s, --silent             no output, exit code only
//	-l, --verbose            list every differing byte
//	-b, --print-bytes        show the differing byte values
//	-n N, --bytes N          compare at most N bytes
//	-i N, --ignore-initial   skip first N bytes of both files
//
// Exit codes match GNU: 0 same, 1 differ, 2 error.
package cmp

func Main(args []string) int { return run(args) }
