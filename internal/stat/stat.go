// Package stat implements bag's drop-in for GNU stat.
//
// Supported:
//
//	-c FORMAT, --format=FMT   custom format string with %a %A %b %d %D %f
//	                          %F %g %G %h %i %m %n %N %o %s %t %T %u %U
//	                          %w %W %x %X %y %Y %z %Z
//	-L, --dereference         follow symlinks
//	-t, --terse               terse one-line output
//	-f, --file-system         show filesystem stat (uses statfs)
//
// Default human-readable output matches gnu stat's "File: / Size: /
// Device: / Access: / Modify: / Change: / Birth:" block (best-effort
// — birth time is platform-dependent and falls back to "-").
//
// Filesystem (`stat -f`) is Linux-only; on macOS we emit a best-effort
// subset using the Statfs syscall.
package stat

func Main(args []string) int { return run(args) }
