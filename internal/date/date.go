// Package date implements bag's drop-in for GNU date.
//
// Supported:
//
//	date                     show current local time in default format
//	date +FORMAT             strftime-style format string
//	date -u, --utc           show in UTC
//	date -R, --rfc-email     RFC 5322 format
//	date -I, --iso-8601[=FMT] ISO-8601 (date / hours / minutes / seconds)
//	date -d STRING, --date   show the given date instead of now
//	date -r FILE             show FILE's mtime
//
// strftime conversions implemented: %a %A %b %B %c %d %e %F %H %I %j
// %m %M %n %N %p %r %R %s %S %t %T %u %U %V %w %W %x %X %y %Y %z %Z %%
// (the common subset; rarer ones %g %G %k %l %P fall through verbatim).
//
// Not implemented: --set (would change the system clock — out of scope),
// --debug, --resolution.
package date

func Main(args []string) int { return run(args) }
