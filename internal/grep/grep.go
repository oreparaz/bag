// Package grep implements the bag drop-in replacement for GNU grep.
//
// Patterns compile through Go's RE2 engine — linear-time, memory-safe.
// PCRE features (backrefs, lookarounds) are not supported, by design.
//
// 80% target:
//
//	-i  --ignore-case
//	-v  --invert-match
//	-c  --count
//	-l  --files-with-matches
//	-L  --files-without-match
//	-n  --line-number
//	-H  --with-filename     (default for multi-file)
//	-h  --no-filename       (default for single-file / stdin)
//	-r/-R --recursive
//	-E  --extended-regexp   (RE2 is already ~ERE)
//	-F  --fixed-strings     (literal match, multi-line per -e splits)
//	-w  --word-regexp
//	-x  --line-regexp
//	-A NUM   --after-context
//	-B NUM   --before-context
//	-C NUM   --context
//	-e PAT   add pattern (repeatable)
//	-f FILE  read patterns from FILE
//	--include GLOB / --exclude GLOB / --exclude-dir GLOB
//	-q  --quiet
//	-s  --no-messages
//
// Exit codes: 0 if any match, 1 if no match, 2 on error.
package grep

func Main(args []string) int { return run(args) }
