// Package curl implements the bag drop-in replacement for GNU curl.
package curl

// Main is the entry point. It returns the process exit code.
// It must not call os.Exit so the multicall harness can propagate the code.
func Main(args []string) int {
	return run(args)
}
