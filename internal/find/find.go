// Package find implements the bag drop-in replacement for GNU find.
//
// 80% target — tests / actions / operators users actually use:
//
//	tests:    -name -iname -path -ipath -type -size -mtime
//	          -newer -mindepth -maxdepth -prune -empty
//	actions:  -print (default) -print0 -delete -exec ... \;
//	operators: implicit AND, -o (OR), -not / !, ( ... ) grouping
//
// Sizes accept k, M, G suffixes (and 'c' for plain bytes).
//
// Deferred (FUTURE.md):
//
//	-perm, -user, -group (numeric and named, with mode masks)
//	-regex, -iregex
//	-xtype, -follow
//	-printf with format strings
//	-fprintf, -fprint, -fprint0 (file output redirections)
//	-execdir
//	-okdir / -ok (interactive prompt)
//	-cnewer, -anewer
//	-fstype
package find

func Main(args []string) int { return run(args) }
