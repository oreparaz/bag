// Package xargs implements bag's drop-in for GNU xargs.
//
// Supported flags:
//
//	-0, --null              input items separated by NUL (paired with find -print0)
//	-d DELIM, --delimiter   use DELIM instead of whitespace
//	-n N, --max-args N      pass at most N items per command invocation
//	-L N                    use up to N input lines per invocation
//	-I REPLACE, --replace   place each item by replacing REPLACE in the cmd
//	-r, --no-run-if-empty   skip the command when no input (default in gnu)
//	-t, --verbose           print the command before running it
//	-p, --interactive       prompt before running each command (yN, /dev/tty)
//	-a FILE                 read items from FILE instead of stdin
//	-s N, --max-chars N     never exceed N chars total in the spawned argv
//
// Default behavior: split stdin on whitespace, group items, run the
// command for each group. If no command is given, /bin/echo is used.
package xargs

func Main(args []string) int { return run(args) }
