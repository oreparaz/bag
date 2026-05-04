// Package wget implements the bag drop-in replacement for GNU wget.
package wget

// Main is the entry point. Returns the process exit code.
func Main(args []string) int {
	return run(args)
}
