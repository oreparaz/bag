// Package sed implements a small, useful subset of GNU sed.
//
// 80% target:
//
//	s/REGEX/REPL/FLAGS    substitution; flags g, i, Nth, p
//	d                     delete the pattern space
//	p                     print the pattern space
//	q                     quit
//
// Addresses:
//
//	N        single line N
//	$        last line
//	N,M      range
//	/RE/     regex match
//	/RE1/,/RE2/   range from RE1 to RE2 inclusive
//
// CLI:
//
//	-n      suppress default print
//	-e SCR  append script (repeatable)
//	-f F    read script from file
//	-E      ERE (RE2 is already ERE-flavored)
//	-i[EXT] in-place editing
//	--      end of options
//
// Commands deliberately unsupported (deferred):
//
//	a, i, c (append/insert/change)
//	r, w, R, W (file I/O)
//	y (transliteration)
//	b, t, : (labels and branches)
//	h, H, g, G (hold space)
//	{...} (grouping)
//
// Patterns compile through RE2.
package sed

func Main(args []string) int { return run(args) }
