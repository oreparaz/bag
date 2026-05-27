// Package rm implements the bag drop-in for GNU rm.
//
// Supported flags:
//
//	-r, -R, --recursive   recurse into directories
//	-f, --force           never prompt; ignore missing files
//	-i                    prompt before every removal (yN, from /dev/tty)
//	-v, --verbose         print a line per removed entry
//	-d, --dir             remove empty directories (like rmdir)
//	--no-preserve-root    allow removing '/' (DANGEROUS; we still require
//	                      -r as a paired safety)
//
// By default rm refuses to remove '/' — matches gnu rm's
// --preserve-root default. There is no --interactive=once / -I
// (interactive once for >3 files or recursive) — use -i if you want
// per-file prompts.
package rm

func Main(args []string) int { return run(args) }
