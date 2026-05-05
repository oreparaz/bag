// Package sort implements the bag drop-in replacement for GNU sort.
//
// Sort is byte-locale (effectively LC_COLLATE=C). GNU sort's locale-aware
// collation is intentionally not duplicated.
//
// 80% target:
//
//	-n, --numeric-sort
//	-r, --reverse
//	-u, --unique
//	-f, --ignore-case
//	-b, --ignore-leading-blanks
//	-k, --key=POS1[,POS2][TYPE]
//	-t, --field-separator=SEP
//	-o, --output=FILE
//	-c, --check
//	-s, --stable    (we are stable by default; flag accepted)
package sort

func Main(args []string) int { return run(args) }
