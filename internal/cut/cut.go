// Package cut implements the bag drop-in replacement for GNU cut.
//
// 80% target:
//
//	-d DELIM        field delimiter (default tab)
//	-f LIST         output fields LIST
//	-c LIST         output character positions LIST (1-based, byte-oriented)
//	-b LIST         output byte positions LIST (== -c for ASCII)
//	-s              with -f: skip lines without delimiter
//	--complement    invert LIST
//	--output-delimiter=STR
//
// LIST syntax: comma-separated ranges, e.g. "1,3-5,7-".
package cut

func Main(args []string) int { return run(args) }
