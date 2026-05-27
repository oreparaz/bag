// Package cp implements the bag drop-in for GNU cp.
//
// Supported flags:
//
//	-r, -R, --recursive    recurse into directories
//	-p, --preserve         preserve mode, mtime and (best-effort) ownership
//	-i, --interactive      prompt before overwriting (yN, from /dev/tty)
//	-f, --force            never prompt; remove the destination if needed
//	-n, --no-clobber       skip destinations that already exist
//	-v, --verbose          print "src -> dst" per file copied
//	-L, --dereference      follow symlinks (default for non-recursive copy)
//	-P, --no-dereference   copy symlinks as symlinks (default for -r)
//	-a, --archive          equivalent to -dpR (preserve symlinks + meta + recurse)
//
// The destination semantics match cp:
//
//	cp src dst-file        single file copy with target name
//	cp src dst-dir/        copy into directory using basename(src)
//	cp src1 src2 ... DIR   multi-source; last arg must be an existing dir
//
// We open src with O_NOFOLLOW only when the user asked for -P / -a (so
// the system cp's default "follow leaf symlinks" still works). Cross-
// filesystem copy is supported transparently — it's just bytes.
package cp

func Main(args []string) int { return run(args) }
