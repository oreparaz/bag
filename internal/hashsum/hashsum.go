// Package hashsum implements bag's drop-in for GNU sha256sum,
// sha512sum, sha1sum and md5sum. One package, four entry points;
// the algorithm is selected by the invoked binary name (argv[0])
// or by the explicit MainAs("sha256sum", args) call from the
// multicall dispatch.
//
// Supported flags (matching GNU coreutils):
//
//	-c, --check          read a checksum file and verify each line
//	-b, --binary         show "*FILE" in default-format output
//	-t, --text           show " FILE" (default on Unix anyway)
//	--tag                emit BSD-style "ALGO (FILE) = HEX"
//	--quiet              with -c, only print failures
//	--status             with -c, print nothing, exit 0/1
//	--strict             with -c, fail on any malformed line
//	--ignore-missing     with -c, skip lines whose FILE is missing
//	-z, --zero           use NUL line terminators (output AND -c input)
//
// Reading the file is a streaming hash so multi-GB inputs are fine
// in bounded memory. Both standalone hex digest and BSD-tag formats
// are accepted by -c.
package hashsum

// MainAs is the multicall-friendly entry. The first argument is the
// algorithm name as the user invoked it ("sha256sum" / "sha512sum" /
// "sha1sum" / "md5sum"); remaining args follow normal flag parsing.
func MainAs(name string, args []string) int {
	return run(name, args)
}
