// Package chmod implements the bag drop-in for GNU chmod.
//
// Supported flags:
//
//	-R, --recursive       recurse into directories
//	-v, --verbose         print "mode of FILE changed from M to N"
//	-c, --changes         like -v but only when the mode actually changed
//	-f, --silent, --quiet suppress most error messages
//	--reference=FILE      copy mode from FILE instead of parsing a mode arg
//
// Mode argument forms accepted:
//
//	octal: 0644, 644, 4644 (setuid + 644), 7755 (setuid+setgid+sticky+755)
//	symbolic: [who][op][perms](,[who][op][perms])*
//	   who   = combination of u g o a (default a, masked by umask)
//	   op    = + | - | =
//	   perms = combination of r w x X s t  OR  one of u g o (copy from)
//
// X (capital-X) sets execute only when the entry is a directory OR
// some execute bit is already present — matches gnu's "smart x".
package chmod

func Main(args []string) int { return run(args) }
