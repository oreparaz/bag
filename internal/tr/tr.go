// Package tr implements bag's drop-in for GNU tr.
//
// Forms:
//
//	tr SET1 SET2     translate bytes in SET1 to the corresponding byte in SET2
//	tr -d SET1       delete bytes in SET1
//	tr -s SET1       squeeze adjacent repeats of bytes in SET1
//	tr -d SET1 -c    use the complement of SET1
//
// Sets accept these escape forms:
//
//	a-z, A-Z, 0-9          ranges
//	\\, \a, \b, \f, \n,    standard C escapes
//	\r, \t, \v
//	\OCT (1-3 octal digits)
//	[:alpha:] [:upper:] [:lower:] [:digit:] [:space:] [:blank:]
//	[:punct:] [:cntrl:] [:print:] [:graph:] [:xdigit:] [:alnum:]
//
// Streaming, byte-oriented: tr is happily binary-faithful.
package tr

func Main(args []string) int { return run(args) }
