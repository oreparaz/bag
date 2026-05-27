// Package patch implements bag's drop-in for GNU patch (unified-diff
// input only).
//
// Supported:
//
//	patch < FILE.PATCH        apply hunks from stdin (or -i FILE)
//	-p N, --strip=N           strip N leading path components from headers
//	-i FILE, --input          read the patch from FILE instead of stdin
//	-R, --reverse             apply the patch in reverse
//	-N, --forward             skip already-applied hunks (no-op for now)
//	-o FILE, --output=FILE    write result to FILE instead of overwriting
//	--dry-run                 verify hunks without modifying files
//
// We only accept unified-diff format ("--- / +++ / @@"). Context-format
// (ed-script / normal) patches are intentionally out of scope.
package patch

func Main(args []string) int { return run(args) }
