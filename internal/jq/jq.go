// Package jq implements bag's drop-in for jq (JSON processor).
//
// Wraps github.com/itchyny/gojq — the pure-Go jq implementation —
// so the surface area inherits jq's full filter language: .field,
// .[], pipes, map/select/length/keys/type, recursive descent (..),
// the math/string builtins, etc.
//
// CLI:
//
//	jq FILTER [FILE ...]    read from FILE(s) or stdin, apply FILTER
//	-r, --raw-output         emit strings unquoted
//	-c, --compact-output     one line per result
//	-s, --slurp              read all input into one array first
//	-n, --null-input         do not read input; use null
//	-R, --raw-input          treat each input line as a string
//	-a, --ascii-output       escape non-ASCII as \uXXXX
//	-C, --color-output       (accepted; we don't colorize)
//	-M, --monochrome-output  (no-op for the same reason)
//	-S, --sort-keys          sort object keys in output
//	--arg NAME VALUE         define $NAME = "VALUE"
//	--argjson NAME JSON      define $NAME = JSON
//
// Exit codes: 0 last output was true; 1 last output was false/null;
// 2 usage error.
package jq

func Main(args []string) int { return run(args) }
