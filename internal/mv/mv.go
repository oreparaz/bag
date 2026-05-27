// Package mv implements the bag drop-in for GNU mv.
//
// Supported flags:
//
//	-f, --force            never prompt; remove destination if it exists
//	-i, --interactive      prompt before overwrite (yN, from /dev/tty)
//	-n, --no-clobber       skip when the destination exists
//	-v, --verbose          print "src -> dst" per move
//
// We try os.Rename first. On EXDEV (different filesystem) we fall
// back to copy-then-remove, preserving mode and mtime. The fallback
// uses internal/cp's logic via a simple inline copy — it keeps mv a
// single import-light leaf package.
package mv

func Main(args []string) int { return run(args) }
