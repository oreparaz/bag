// Package cat implements the bag drop-in replacement for GNU cat.
//
// 80% of cat is "concatenate files (or stdin) to stdout". Beyond that:
//
//	-n, --number             number all output lines
//	-b, --number-nonblank    number only non-empty lines (overrides -n)
//	-s, --squeeze-blank      collapse repeated empty lines into one
//	-E, --show-ends          mark line ends with '$'
//	-T, --show-tabs          render tabs as '^I'
//	-v, --show-nonprinting   render non-printing bytes as caret/meta
//	-A, --show-all           = -vET
//	-e                       = -vE
//	-t                       = -vT
//	-u                       no-op (matches POSIX cat for unbuffered I/O)
//
// We do not currently implement '--help' / '--version' with the exact
// GNU output strings; we ship a short --help and a brief version line.
package cat

// Main is the entry point. Returns the process exit code.
func Main(args []string) int {
	return run(args)
}
