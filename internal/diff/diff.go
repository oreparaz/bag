// Package diff implements bag's drop-in for GNU diff.
//
// Supported:
//
//	diff FILE1 FILE2          line-by-line unified-format diff
//	-u, --unified[=N]         unified context (default already; N lines)
//	-r, --recursive           diff directories recursively
//	-N, --new-file            treat absent files as empty
//	-q, --brief               only report whether files differ
//	-i, --ignore-case
//	-w, --ignore-all-space    collapse all whitespace
//	-B, --ignore-blank-lines
//	-s, --report-identical-files
//
// The diff algorithm is straightforward Myers (O(ND) time) over line
// hashes; sufficient for typical patch files. Binary files print
// "Binary files ... differ" rather than a hex diff.
//
// Exit codes (gnu): 0 same, 1 differ, 2 trouble.
package diff

func Main(args []string) int { return run(args) }
