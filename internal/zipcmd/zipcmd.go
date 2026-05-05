// Package zipcmd implements the bag drop-in replacements for `zip` and
// `unzip`. It uses archive/zip from the stdlib for both directions.
//
// 80% target:
//
//	zip [-r] [-q] [-0..-9] [-j] OUTPUT FILES...
//	unzip [-l] [-p] [-q] [-o] [-n] [-d DIR] [-j] ZIP [FILES...]
//
// Encryption (zipcrypto, AES-256) is intentionally deferred — see
// FUTURE.md.
package zipcmd

// MainAs dispatches to either zip or unzip based on the program name.
func MainAs(name string, args []string) int {
	switch name {
	case "unzip":
		return runUnzip(args)
	default:
		return runZip(args)
	}
}
