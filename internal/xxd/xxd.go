// Package xxd implements the bag drop-in replacement for xxd.
//
// 80% target:
//
//	default                hex+ASCII dump, 16 bytes/row, group of 2
//	-c, --cols=N           bytes per row
//	-g, --groupsize=N      bytes per hex group
//	-p, --plain            plain hex dump (postscript-style)
//	-r, --revert           reverse: hex back to binary
//	-s OFF                 skip OFF bytes at start (decimal; '-OFF' from EOF
//	                        is intentionally not supported — see FUTURE.md)
//	-l, --len=N            stop after N bytes
//	-u                     uppercase hex
//	-                      read stdin
package xxd

func Main(args []string) int { return run(args) }
