// Package base64cmd implements the bag drop-in replacement for GNU base64.
//
// The package is named base64cmd to avoid clashing with the stdlib's
// "encoding/base64" import path. The binary is exposed as "base64".
//
// 80% target:
//
//	-d, --decode             decode rather than encode
//	-w, --wrap=COLS          wrap output to COLS columns (default 76; 0 disables)
//	-i, --ignore-garbage     when decoding, skip non-alphabet bytes
//	-                        read stdin
package base64cmd

func Main(args []string) int { return run(args) }
