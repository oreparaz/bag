// Package ag implements the bag drop-in replacement for The Silver
// Searcher (ag) — a recursive code-search tool with sane defaults.
//
// What you get:
//
//   - Recursive by default. `ag PATTERN` searches the current directory
//     tree without -r.
//   - Smart-case. Lowercase pattern → case-insensitive. Any uppercase
//     in the pattern → case-sensitive. Override with -i / -s.
//   - .gitignore honor. Patterns from .gitignore and .ignore at the
//     search root are skipped. Disable with -U / --no-ignore.
//   - Hidden file skip. .git/, .hg/, .svn/ and any dotfile are skipped
//     unless --hidden.
//   - Binary file skip. Files containing a NUL byte in the first 8 KiB
//     are treated as binary and skipped (override with -a / --all-types).
//   - File-grouped output on a TTY: filename on its own line, then
//     N:matched-line for each hit. Off-TTY (piped) output uses the
//     classic grep format file:N:line.
//   - RE2 regex (no PCRE backrefs / lookarounds — DoS-safe).
//
// Flags supported:
//
//	-i / --ignore-case
//	-s / --case-sensitive
//	-Q / --literal           treat PATTERN as a literal string
//	-w / --word-regexp
//	-v / --invert-match
//	-l / --files-with-matches
//	-L / --files-without-matches
//	-c / --count
//	-A / --after  N
//	-B / --before N
//	-C / --context N
//	-G / --file-search-regex REGEX  filter filenames
//	--ignore PATTERN          add an ignore pattern
//	-U / --no-ignore          do not honor .gitignore / .ignore
//	--hidden                  search hidden files / dirs
//	-a / --all-types          search binary files too
//	--depth N                 max recursion depth
//	-0 / --null               separate filenames with NUL (with -l / -L)
//	-H / --filename           always show filename
//	     --no-filename        never show filename
//	     --nogroup            disable file-grouped output
//	     --group              force file-grouped output
package ag

func Main(args []string) int { return run(args) }
