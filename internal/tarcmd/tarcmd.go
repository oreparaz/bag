// Package tarcmd implements the bag drop-in replacement for GNU tar.
//
// 80% target:
//
//	-c, --create              create
//	-x, --extract, --get      extract
//	-t, --list                list contents
//	-f, --file FILE           archive file ("-" for stdio)
//	-v, --verbose             verbose
//	-C, --directory DIR       chdir before action
//	-z, --gzip                gzip
//	-j, --bzip2               bzip2
//	-J, --xz                  xz
//	    --zstd                zstd
//	-a, --auto-compress       infer compression from -f extension
//	    --exclude PATTERN     exclude (filepath.Match-style)
//	-p, --preserve-permissions  honor stored modes (default for root)
//	-h, --dereference         follow symlinks during -c
//	    --strip-components=N  strip N leading path components on extract
package tarcmd

func Main(args []string) int { return run(args) }
