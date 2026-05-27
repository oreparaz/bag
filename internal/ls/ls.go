// Package ls implements the bag drop-in for GNU ls.
//
// We aim for the daily-driver subset:
//
//	-l                long listing (mode, links, owner, group, size, mtime, name)
//	-a, --all         include entries starting with '.'
//	-A, --almost-all  like -a but skip '.' and '..'
//	-1                one entry per line
//	-h, --human       human-readable sizes with -l (1.2K, 4.0M, …)
//	-S                sort by size (largest first)
//	-t                sort by mtime (newest first)
//	-r, --reverse     reverse sort order
//	-R, --recursive   list subdirectories recursively
//	-d, --directory   list the directory entry itself, not its contents
//	-F, --classify    append */@|= type indicators
//	-i, --inode       print inode number
//	--color           NEVER (we don't colorize); accepted+ignored for compat
//
// Sorting defaults to byte-wise alphabetical (matches LC_ALL=C). We don't
// implement -X (extension sort), -v (version sort), --group-directories-first,
// -L / -H symlink handling beyond the default, or the BSD -G/-T extensions.
//
// Output goes to stdout one entry per line by default; when stdout is a
// tty and -1 wasn't given, GNU ls switches to a column grid. We always
// emit one-per-line — scripts get a stable format and humans pipe through
// `column` if they care.
package ls

// Main is the entry point. Returns the process exit code.
func Main(args []string) int {
	return run(args)
}
