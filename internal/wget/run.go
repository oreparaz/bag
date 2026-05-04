package wget

import (
	"fmt"
	"os"
)

// run is the wget entry point. Full implementation arrives in a later
// commit; this stub keeps the multicall binary compiling.
func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "wget: missing URL")
		return 1
	}
	fmt.Fprintln(os.Stderr, "wget: not yet implemented in this build")
	return 1
}
