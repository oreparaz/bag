// Package mkdir implements the bag drop-in for GNU mkdir.
//
// Supported flags:
//
//	-p, --parents       create intermediate directories as needed,
//	                    silently no-op if the leaf already exists
//	-m MODE, --mode     set the mode on the created directories
//	                    (octal; symbolic forms NOT implemented)
//	-v, --verbose       print a line per directory created
//
// Without -m, mkdir uses 0o777 modulated by the process umask
// (matches GNU). With -m the umask is ignored and the requested
// permission is set directly via chmod after creation, so the bits
// match what the user asked for.
package mkdir

func Main(args []string) int { return run(args) }
